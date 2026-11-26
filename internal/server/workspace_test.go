package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/workspace"
)

// wsFixture is a workspace server under test with a registry in a
// temporary folder, a counting Open and an adjustable clock.
type wsFixture struct {
	t   *testing.T
	ws  *workspaceServer
	reg *workspace.Registry
	cfg string

	mu      sync.Mutex
	now     time.Time
	opens   map[string]int
	closes  map[string]int
	active  int
	maxSeen int
}

type wsOpenFunc func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error)

// newWSFixture builds a workspace server. setup may change the options
// before the server is built.
func newWSFixture(t *testing.T, open wsOpenFunc, setup func(*WorkspaceOptions)) *wsFixture {
	t.Helper()
	f := &wsFixture{t: t, now: fixedNow(), opens: map[string]int{}, closes: map[string]int{}}
	f.cfg = filepath.Join(t.TempDir(), "config", "graphvein")
	f.reg = workspace.Open(filepath.Join(f.cfg, "workspace.yaml"))
	wo := &WorkspaceOptions{
		Registry: f.reg,
		Open: func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error) {
			f.mu.Lock()
			f.opens[e.Name]++
			f.active++
			if f.active > f.maxSeen {
				f.maxSeen = f.active
			}
			f.mu.Unlock()
			defer func() {
				f.mu.Lock()
				f.active--
				f.mu.Unlock()
			}()
			wi, err := open(ctx, e, create)
			if err != nil {
				return nil, err
			}
			inner := wi.Close
			wi.Close = func() {
				f.mu.Lock()
				f.closes[e.Name]++
				f.mu.Unlock()
				if inner != nil {
					inner()
				}
			}
			return wi, nil
		},
		StoreLocation: func(folder string) StoreLocation {
			return StoreLocation{Kind: "sqlite", Path: filepath.Join(folder, sqlite.DefaultPath)}
		},
		Home:          t.TempDir(),
		CheckInterval: -1,
	}
	if setup != nil {
		setup(wo)
	}
	opt := Options{Version: "test", Assets: fstest.MapFS{"index.html": {Data: []byte("<p>t</p>")}}, Now: f.clock, Workspace: wo}
	f.ws = newWorkspaceServer(opt, testHosts)
	t.Cleanup(f.ws.shutdown)
	return f
}

func (f *wsFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *wsFixture) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *wsFixture) count(m map[string]int, name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return m[name]
}

// register adds a folder to the registry and returns its entry.
func (f *wsFixture) register(kind workspace.Kind, name, folder string) workspace.Entry {
	f.t.Helper()
	path, err := workspace.Resolve(folder)
	if err != nil {
		f.t.Fatal(err)
	}
	e, _, err := f.reg.Add(workspace.Entry{Kind: kind, Name: name, Path: path})
	if err != nil {
		f.t.Fatal(err)
	}
	return e
}

// do sends one request from a loopback peer; POSTs carry JSON and the
// session token unless header overrides them ("" deletes a header).
func (f *wsFixture) do(method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = testHost
	req.RemoteAddr = "127.0.0.1:50000"
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(tokenHeader, f.ws.token)
	}
	for k, v := range header {
		if k == "RemoteAddr" {
			req.RemoteAddr = v
			continue
		}
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	f.ws.handler.ServeHTTP(rec, req)
	return rec
}

// json sends a request, checks its status and decodes the body into v.
func (f *wsFixture) json(method, target, body string, want int, v any) {
	f.t.Helper()
	rec := f.do(method, target, body, nil)
	if rec.Code != want {
		f.t.Fatalf("%s %s: %d %s", method, target, rec.Code, rec.Body.String())
	}
	if v != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
			f.t.Fatal(err, rec.Body.String())
		}
	}
}

func (f *wsFixture) list() workspaceResponse {
	f.t.Helper()
	var out workspaceResponse
	f.json(http.MethodGet, workspacePath, "", http.StatusOK, &out)
	return out
}

func (f *wsFixture) item(id string) workspaceItem {
	f.t.Helper()
	for _, it := range f.list().Entries {
		if it.ID == id {
			return it
		}
	}
	f.t.Fatalf("no entry %s", id)
	return workspaceItem{}
}

func (f *wsFixture) isOpen(id string) bool {
	return f.ws.openIDs()[id]
}

// sqliteOpen opens an entry's index as graphvein serve does with the
// default SQLite location (<folder>/.graphvein/index.db).
func sqliteOpen(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error) {
	path := filepath.Join(e.Path, sqlite.DefaultPath)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) && !create {
		return nil, ErrNoIndex
	}
	if create {
		w, err := sqlite.Open(ctx, path)
		if err != nil {
			return nil, err
		}
		w.Close()
	}
	ro, err := sqlite.OpenRead(ctx, path)
	if err != nil {
		return nil, err
	}
	opt := Options{Store: ro, StoreDir: filepath.Dir(path),
		OpenWriter: func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) }}
	var locker store.IndexLocker
	if e.Kind == workspace.KindProject {
		p, err := config.LoadProject(e.Path)
		if err != nil {
			ro.Close()
			return nil, err
		}
		locker = sqlite.ProjectLocker(path, p.Name)
		opt.Project = &ProjectOptions{Project: p, Reindex: fakeProjectReindex}
	} else {
		locker = sqlite.IndexLocker(path)
		opt.Root, opt.Repository = e.Path, e.Name
	}
	opt.Locker = locker
	return &WorkspaceIndex{Entry: e, Options: opt, Close: func() {
		locker.Close()
		ro.Close()
	}}, nil
}

// fakeProjectReindex reports every member checkout committed and one
// link pass, without indexing.
func fakeProjectReindex(_ context.Context, p config.Project, _ bool, progress func(string, indexrun.Progress)) ProjectRun {
	out := ProjectRun{Pass: &store.ProjectLinkRun{Seq: 1}}
	for _, m := range p.Members {
		progress(m.Repository, indexrun.Progress{Phase: "parsing", Counted: true, Done: 1, Total: 2, Unit: "files"})
		out.Members = append(out.Members, ProjectMemberRun{Repository: m.Repository, Indexed: m.Checkout,
			Run: &store.Run{ID: "run-" + m.Repository, FinishedAt: "2026-11-24T10:00:00Z"}})
	}
	return out
}

// indexedFolder copies the Go fixture to a new folder and indexes it into
// its default SQLite store as repository name.
func indexedFolder(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	w, err := sqlite.Open(context.Background(), filepath.Join(root, sqlite.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	indexRepository(t, root, w, name)
	w.Close()
	return root
}

// sharedOpen opens every entry on one shared store (the backends' read
// store), as GRAPHVEIN_POSTGRES_URL does; missing names answer
// ErrNoIndex.
func sharedOpen(read store.Store, missing ...string) wsOpenFunc {
	return func(_ context.Context, e workspace.Entry, _ bool) (*WorkspaceIndex, error) {
		for _, m := range missing {
			if m == e.Name {
				return nil, ErrNoIndex
			}
		}
		if e.Name == "broken" {
			return nil, errors.New("store unreachable")
		}
		return &WorkspaceIndex{Entry: e, Options: Options{Root: e.Path, Repository: e.Name, Store: read}}, nil
	}
}

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestWorkspaceList checks WS-7 and WS-12: projects first, then
// repositories by name; the session token, the open limit, the current
// entry (the working directory's registered entry) and the suggestion
// (an unregistered working directory with an index).
func TestWorkspaceList(t *testing.T) {
	alpha, beta := t.TempDir(), t.TempDir()
	shop := t.TempDir()
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n")
	var f *wsFixture
	f = newWSFixture(t, sharedOpen(nil), func(o *WorkspaceOptions) {
		reg := workspace.Open(filepath.Join(filepath.Dir(o.Registry.Path()), "workspace.yaml"))
		for _, e := range []workspace.Entry{{Kind: workspace.KindRepository, Name: "beta", Path: beta}, {Kind: workspace.KindRepository, Name: "alpha", Path: alpha}, {Kind: workspace.KindProject, Name: "shop", Path: shop}} {
			p, _ := workspace.Resolve(e.Path)
			e.Path = p
			if _, _, err := reg.Add(e); err != nil {
				t.Fatal(err)
			}
		}
		o.WorkDir = alpha
	})
	l := f.list()
	var names []string
	for _, e := range l.Entries {
		names = append(names, e.Name)
		if !e.Registered || e.Freshness != nil || e.Job != nil {
			t.Errorf("entry %+v", e)
		}
	}
	if strings.Join(names, ",") != "shop,alpha,beta" {
		t.Fatalf("order %v", names)
	}
	if l.Token == "" || l.OpenLimit != maxOpenIndexes || l.Current == nil || *l.Current != l.Entries[1].ID || !l.Entries[1].Current || l.Entries[2].Current || l.Suggestion != nil {
		t.Fatalf("list %+v", l)
	}
	if l.Registry.Error != nil || l.Registry.Path != f.reg.Path() {
		t.Fatalf("registry %+v", l.Registry)
	}

	// An unregistered working directory holding an index is suggested.
	other := t.TempDir()
	if err := os.Mkdir(filepath.Join(other, ".graphvein"), 0o755); err != nil {
		t.Fatal(err)
	}
	g := newWSFixture(t, sharedOpen(nil), func(o *WorkspaceOptions) { o.WorkDir = other })
	gl := g.list()
	resolved, _ := workspace.Resolve(other)
	if gl.Current != nil || gl.Suggestion == nil || gl.Suggestion.Path != resolved || gl.Suggestion.Kind != workspace.KindRepository || len(gl.Entries) != 0 {
		t.Fatalf("first run %+v", gl)
	}
	// A registry that cannot be read is reported, not shown as empty.
	if err := os.MkdirAll(filepath.Dir(g.reg.Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(g.reg.Path(), []byte("version: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if gl := g.list(); gl.Registry.Error == nil {
		t.Fatalf("damaged registry %+v", gl.Registry)
	}
}

// TestWorkspaceListStates checks WS-8 and WS-10: open, not open and
// folder gone (an open index keeps "open"); project members from the
// manifest on disk, and the manifest's error instead of members when it
// does not load.
func TestWorkspaceListStates(t *testing.T) {
	backends(t, func(t *testing.T, write, read store.Store) {
		alpha, beta, gone := fixtureRoot(t), fixtureRoot(t), t.TempDir()
		indexRepository(t, alpha, write, "alpha")
		f := newWSFixture(t, sharedOpen(read), nil)
		a := f.register(workspace.KindRepository, "alpha", alpha)
		b := f.register(workspace.KindRepository, "beta", beta)
		g := f.register(workspace.KindRepository, "gone", gone)
		shop := t.TempDir()
		if err := os.Mkdir(filepath.Join(shop, "api"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n  - repository: web\n    path: web\n")
		p := f.register(workspace.KindProject, "shop", shop)
		if err := os.RemoveAll(gone); err != nil {
			t.Fatal(err)
		}
		f.json(http.MethodGet, "/api/v0/api/endpoints?ws="+url.QueryEscape(a.ID), "", http.StatusOK, nil)
		states := map[string]string{a.ID: entryOpen, b.ID: entryNotOpen, g.ID: entryFolderGone, p.ID: entryNotOpen}
		for _, it := range f.list().Entries {
			if it.State != states[it.ID] {
				t.Errorf("%s: state %s, want %s", it.Name, it.State, states[it.ID])
			}
			if it.Kind == workspace.KindRepository && (it.Members != nil || it.MembersError != nil) {
				t.Errorf("%s: members on a repository", it.Name)
			}
		}
		it := f.item(p.ID)
		if len(it.Members) != 2 || it.Members[0].Repository != "api" || !it.Members[0].Checkout || it.Members[1].Checkout || it.Members[0].Path == nil || it.MembersError != nil {
			t.Fatalf("members %+v", it)
		}
		writeManifest(t, shop, "version: 1\nname: shop\nmembers: nope\n")
		if it := f.item(p.ID); it.Members != nil || it.MembersError == nil {
			t.Fatalf("broken manifest %+v", it)
		}
	})
}

// TestWorkspaceBackgroundChecks checks WS-9 and WS-20: no freshness until
// a check finished, then the states of /api/v0/status (fresh only from a
// zero-change check of an indexed store); a gone folder is not checked;
// checks run one at a time, every entry once, then only open entries on
// the interval; a project folds its members. A status request records
// its answer as the entry's freshness at once, unless a newer one is
// recorded.
func TestWorkspaceBackgroundChecks(t *testing.T) {
	fresh, stale, unindexed := indexedFolder(t, "fresh"), indexedFolder(t, "stale"), fixtureRoot(t)
	gone := t.TempDir()
	if err := os.WriteFile(filepath.Join(stale, "main.go"), []byte("package main\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := newWSFixture(t, sqliteOpen, func(o *WorkspaceOptions) { o.CheckInterval = 20 * time.Millisecond })
	ids := map[string]string{}
	for name, dir := range map[string]string{"fresh": fresh, "stale": stale, "unindexed": unindexed, "gone": gone} {
		ids[name] = f.register(workspace.KindRepository, name, dir).ID
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	for _, it := range f.list().Entries {
		if it.Freshness != nil {
			t.Fatalf("%s has freshness before any check: %+v", it.Name, it.Freshness)
		}
	}
	// Open one entry with a status request: its answer is the entry's
	// freshness at once, before any background check ran.
	f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(ids["fresh"]), "", http.StatusOK, nil)
	if it := f.item(ids["fresh"]); it.Freshness == nil || it.Freshness.State != stateFresh || it.Freshness.CheckedAt == "" {
		t.Fatalf("status did not record freshness: %+v", it.Freshness)
	}
	if it := f.item(ids["stale"]); it.Freshness != nil {
		t.Fatalf("another entry got freshness: %+v", it.Freshness)
	}
	f.ws.start()
	deadline := time.Now().Add(10 * time.Second)
	for {
		l := f.list()
		done := 0
		for _, it := range l.Entries {
			if it.Freshness != nil {
				done++
			}
		}
		if done == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("checks did not finish: %+v", l.Entries)
		}
		time.Sleep(5 * time.Millisecond)
	}
	want := map[string]string{"fresh": stateFresh, "stale": stateStale, "unindexed": stateNotIndexed}
	first := map[string]string{}
	for name, st := range want {
		it := f.item(ids[name])
		if it.Freshness == nil || it.Freshness.State != st {
			t.Errorf("%s: %+v, want %s", name, it.Freshness, st)
		}
		first[name] = it.Freshness.CheckedAt
	}
	if it := f.item(ids["gone"]); it.Freshness != nil {
		t.Errorf("gone folder checked: %+v", it.Freshness)
	}
	if f.maxSeen > 1 {
		t.Errorf("%d checks ran at once", f.maxSeen)
	}
	// Later passes re-check only the open entry.
	f.advance(time.Minute)
	for f.item(ids["fresh"]).Freshness.CheckedAt == first["fresh"] {
		if time.Now().After(deadline) {
			t.Fatal("the open entry was not checked again")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if f.item(ids["stale"]).Freshness.CheckedAt != first["stale"] {
		t.Error("an entry that is not open was checked again")
	}
	// A status request after an edit shows the edit without waiting for
	// the next pass.
	if err := os.WriteFile(filepath.Join(fresh, "main.go"), []byte("package main\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.advance(time.Minute)
	f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(ids["fresh"]), "", http.StatusOK, nil)
	if it := f.item(ids["fresh"]); it.Freshness == nil || it.Freshness.State != stateStale {
		t.Fatalf("status after an edit: %+v", it.Freshness)
	}
	// An older answer (a background check that started before the
	// request) never replaces a newer one.
	f.ws.recordStatus(ids["fresh"], nil, "fresh", "", freshnessStatus{State: stateFresh, CheckedAt: fixedNow().UTC().Format(time.RFC3339Nano)})
	if it := f.item(ids["fresh"]); it.Freshness.State != stateStale {
		t.Fatalf("an older answer replaced a newer one: %+v", it.Freshness)
	}
	// A project member's status replaces that member's state and folds
	// the project again; a member never checked counts as unknown.
	po := &ProjectOptions{Project: config.Project{Name: "shop", Members: []config.Member{{Repository: "api"}, {Repository: "web"}}}}
	check := func(id string) entryCheck {
		f.ws.mu.Lock()
		defer f.ws.mu.Unlock()
		return f.ws.checks[id]
	}
	at := func(m int) string {
		return fixedNow().Add(time.Duration(m) * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	f.ws.recordStatus("shop~1", po, "api", "", freshnessStatus{State: stateFresh, CheckedAt: at(1)})
	if c := check("shop~1"); c.doc.State != stateUnknown || c.members["api"].State != stateFresh {
		t.Fatalf("one member checked: %+v", c)
	}
	f.ws.recordStatus("shop~1", po, "web", "", freshnessStatus{State: stateFresh, CheckedAt: at(2)})
	if c := check("shop~1"); c.doc.State != stateFresh || c.doc.CheckedAt != at(1) {
		t.Fatalf("both members fresh: %+v", c)
	}
	f.ws.recordStatus("shop~1", po, "web", "", freshnessStatus{State: stateStale, CheckedAt: at(3)})
	if c := check("shop~1"); c.doc.State != stateStale {
		t.Fatalf("a member stale: %+v", c)
	}
	for _, tc := range []struct {
		states []string
		want   string
	}{
		{[]string{stateFresh, stateFresh}, stateFresh},
		{[]string{stateFresh, stateStale, stateUnknown}, stateStale},
		{[]string{stateFresh, stateUnknown}, stateUnknown},
		{[]string{stateNotIndexed, stateNotIndexed}, stateNotIndexed},
		{[]string{stateNotIndexed, stateFresh}, stateUnknown},
		{nil, stateUnknown},
	} {
		if got := foldMemberStates(tc.states); got != tc.want {
			t.Errorf("fold %v = %s, want %s", tc.states, got, tc.want)
		}
	}
}

// TestWorkspaceDispatch checks WS-13, WS-14 and WS-15: ws selects the
// entry and never reaches the index's handlers; without it the current
// entry answers, and a server without one refuses; unknown, empty or
// repeated ws values; an entry without a store, and one whose store does
// not open.
func TestWorkspaceDispatch(t *testing.T) {
	backends(t, func(t *testing.T, write, read store.Store) {
		alpha, beta := fixtureRoot(t), fixtureRoot(t)
		indexRepository(t, alpha, write, "alpha")
		indexRepository(t, beta, write, "beta")
		current := &WorkspaceIndex{Entry: workspace.Entry{Kind: workspace.KindRepository, Name: "alpha", Path: alpha}, Options: Options{Root: alpha, Repository: "alpha", Store: read}}
		current.Entry.ID = workspace.EntryID(current.Entry.Kind, current.Entry.Path, current.Entry.Name)
		f := newWSFixture(t, sharedOpen(read, "empty"), func(o *WorkspaceOptions) { o.Current = current })
		b := f.register(workspace.KindRepository, "beta", beta)
		empty := f.register(workspace.KindRepository, "empty", t.TempDir())
		broken := f.register(workspace.KindRepository, "broken", t.TempDir())
		var st statusResponse
		f.json(http.MethodGet, "/api/v0/status", "", http.StatusOK, &st)
		if st.Repository != "alpha" {
			t.Fatalf("no ws: %s", st.Repository)
		}
		f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(b.ID), "", http.StatusOK, &st)
		if st.Repository != "beta" || st.Freshness.State != stateFresh {
			t.Fatalf("ws=beta: %+v", st)
		}
		for target, want := range map[string]string{
			"/api/v0/status?ws=nope~00000000": "unknown_entry",
			"/api/v0/status?ws=":              "bad_request",
			"/api/v0/status?ws=" + url.QueryEscape(b.ID) + "&ws=" + url.QueryEscape(b.ID): "bad_request",
			"/api/v0/status?ws=" + url.QueryEscape(empty.ID):                              "no_index",
			"/api/v0/status?ws=" + url.QueryEscape(broken.ID):                             "store_error",
			"/api/v0/nothing?ws=" + url.QueryEscape(b.ID):                                 "not_found",
		} {
			if rec := f.do(http.MethodGet, target, "", nil); errorCode(t, rec) != want {
				t.Errorf("%s: %d %s, want %s", target, rec.Code, rec.Body.String(), want)
			}
		}
		if f.count(f.opens, "beta") != 1 {
			t.Errorf("beta opened %d times", f.count(f.opens, "beta"))
		}
		// Without a current entry, a per-index request must name one.
		g := newWSFixture(t, sharedOpen(read), nil)
		if rec := g.do(http.MethodGet, "/api/v0/status", "", nil); rec.Code != http.StatusBadRequest || errorCode(t, rec) != "ws_required" {
			t.Fatalf("no current: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// TestWorkspaceLRU checks WS-16: beyond the open limit the least recently
// used idle index is closed; an index with a request in flight or a
// running job, and the command-line index, are kept.
func TestWorkspaceLRU(t *testing.T) {
	backends(t, func(t *testing.T, write, read store.Store) {
		root := fixtureRoot(t)
		indexRepository(t, root, write, "a")
		current := &WorkspaceIndex{Entry: workspace.Entry{Kind: workspace.KindRepository, Name: "pinned", Path: t.TempDir()}, Options: Options{Root: root, Repository: "a", Store: read}}
		current.Entry.ID = workspace.EntryID(current.Entry.Kind, current.Entry.Path, current.Entry.Name)
		f := newWSFixture(t, sharedOpen(read), func(o *WorkspaceOptions) { o.MaxOpen, o.Current = 3, current })
		ids := map[string]string{}
		for _, name := range []string{"a", "b", "c", "d"} {
			ids[name] = f.register(workspace.KindRepository, name, t.TempDir()).ID
		}
		get := func(name string) {
			t.Helper()
			f.json(http.MethodGet, "/api/v0/api/endpoints?ws="+url.QueryEscape(ids[name]), "", http.StatusOK, nil)
		}
		get("a")
		get("b")
		get("c")
		if f.isOpen(ids["a"]) || f.count(f.closes, "a") != 1 || !f.isOpen(ids["b"]) || !f.isOpen(ids["c"]) || !f.isOpen(current.Entry.ID) {
			t.Fatalf("after c: open %v closes %v", f.ws.openIDs(), f.closes)
		}
		// b is older than c but in use: c is closed instead.
		held, _ := f.ws.hold(ids["b"], false)
		get("d")
		if !f.isOpen(ids["b"]) || f.isOpen(ids["c"]) || f.count(f.closes, "c") != 1 {
			t.Fatalf("in use: open %v closes %v", f.ws.openIDs(), f.closes)
		}
		f.ws.release(held)
		// d runs a job: opening a closes b, the least recently used idle.
		f.ws.mu.Lock()
		d := f.ws.open[ids["d"]]
		f.ws.mu.Unlock()
		d.handler.runs.mu.Lock()
		d.handler.runs.job = &jobDoc{ID: "x", State: jobRunning}
		d.handler.runs.mu.Unlock()
		get("a")
		get("c")
		if !f.isOpen(ids["d"]) || f.count(f.closes, "d") != 0 || !f.isOpen(current.Entry.ID) {
			t.Fatalf("busy: open %v closes %v", f.ws.openIDs(), f.closes)
		}
		d.handler.runs.mu.Lock()
		d.handler.runs.job.EndedAt = newPtr("done")
		d.handler.runs.mu.Unlock()
		if len(f.ws.openIDs()) > 4 {
			t.Fatalf("open %v", f.ws.openIDs())
		}
	})
}

// TestWorkspaceCache checks WS-17 and WS-18: only the listed views are
// cached (hit after a miss, per query), status never; a commit made
// outside the server, a graphvein.yaml edit and the end of this server's
// own run all make the next answer a miss.
func TestWorkspaceCache(t *testing.T) {
	backends(t, func(t *testing.T, write, read store.Store) {
		root := fixtureRoot(t)
		indexRepository(t, root, write, "alpha")
		f := newWSFixture(t, sharedOpen(read), nil)
		a := f.register(workspace.KindRepository, "alpha", root)
		ws := "ws=" + url.QueryEscape(a.ID)
		get := func(path, want string) string {
			t.Helper()
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			rec := f.do(http.MethodGet, path+sep+ws, "", nil)
			if rec.Code != http.StatusOK || rec.Header().Get(cacheHeader) != want {
				t.Fatalf("%s: %d cache %q, want %q: %s", path, rec.Code, rec.Header().Get(cacheHeader), want, rec.Body.String())
			}
			return rec.Body.String()
		}
		first := get("/api/v0/api/endpoints", "miss")
		if again := get("/api/v0/api/endpoints", "hit"); again != first {
			t.Fatal("a hit answered differently")
		}
		get("/api/v0/api/endpoints?transport=http", "miss")
		get("/api/v0/schema/summary", "miss")
		get("/api/v0/schema/summary", "hit")
		get("/api/v0/status", "")
		get("/api/v0/status", "")
		for p := range cachedPaths {
			if strings.HasPrefix(p, "/api/v0/status") || p == "/api/v0/source" || p == "/api/v0/api/endpoint" || p == "/api/v0/api/calls" || p == "/api/v0/api/flow" {
				t.Errorf("%s must not be cached", p)
			}
		}
		// A commit outside the server (graphvein index in a terminal).
		indexRepository(t, root, write, "alpha")
		get("/api/v0/api/endpoints", "miss")
		get("/api/v0/api/endpoints", "hit")
		// A graphvein.yaml edit.
		if err := os.WriteFile(filepath.Join(root, config.File), []byte("version: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		get("/api/v0/api/endpoints", "miss")
		get("/api/v0/api/endpoints", "hit")
		// The end of this server's run of the index.
		f.ws.mu.Lock()
		oi := f.ws.open[a.ID]
		f.ws.mu.Unlock()
		if oi.handler.runs.onEnd == nil {
			t.Fatal("the runner does not invalidate the cache")
		}
		oi.handler.runs.onEnd()
		get("/api/v0/api/endpoints", "miss")
	})
}

// TestWorkspaceCacheRun checks WS-18 with a real run: a re-index started
// through the workspace server makes the next answer a miss once it
// committed.
func TestWorkspaceCacheRun(t *testing.T) {
	root := indexedFolder(t, "alpha")
	f := newWSFixture(t, sqliteOpen, nil)
	a := f.register(workspace.KindRepository, "alpha", root)
	ws := "?ws=" + url.QueryEscape(a.ID)
	if rec := f.do(http.MethodGet, "/api/v0/api/endpoints"+ws, "", nil); rec.Header().Get(cacheHeader) != "miss" {
		t.Fatal(rec.Header())
	}
	if rec := f.do(http.MethodGet, "/api/v0/api/endpoints"+ws, "", nil); rec.Header().Get(cacheHeader) != "hit" {
		t.Fatal(rec.Header())
	}
	f.json(http.MethodPost, indexRunPath+ws, "{}", http.StatusAccepted, nil)
	waitJob(t, f, a.ID, jobCommitted)
	if rec := f.do(http.MethodGet, "/api/v0/api/endpoints"+ws, "", nil); rec.Header().Get(cacheHeader) != "miss" {
		t.Fatalf("after the run: %v", rec.Header())
	}
}

// waitJob polls GET /api/v0/index/run?ws= until the job ended and the lock
// is no longer held here, and checks its state.
func waitJob(t *testing.T, f *wsFixture, id, want string) indexRunResponse {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		var doc indexRunResponse
		f.json(http.MethodGet, indexRunPath+"?ws="+url.QueryEscape(id), "", http.StatusOK, &doc)
		if doc.Job != nil && doc.Job.EndedAt != nil && doc.Lock.State != lockHeldHere {
			if doc.Job.State != want {
				t.Fatalf("job %+v, want %s", doc.Job, want)
			}
			return doc
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not end: %+v", doc)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWorkspaceCacheBounds checks WS-19: an index's cache stays within its
// limit and the server's total, evicting its least recently used answers;
// an answer over 4 MiB is never cached.
func TestWorkspaceCacheBounds(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	budget := &cacheBudget{limit: 250}
	ih := &indexHandler{opt: Options{Repository: "r", Store: s, noCheckout: true}}
	c := newAnswerCache(ih, budget)
	c.limit = 200
	for i := 0; i < 5; i++ {
		c.put(fmt.Sprintf("k%d", i), "fp", 0, http.Header{}, make([]byte, 60))
	}
	if c.size > 200 || len(c.entries) != 3 || c.entries["k0"] != nil || c.entries["k4"] == nil || budget.total != c.size {
		t.Fatalf("index limit: size %d entries %d budget %d", c.size, len(c.entries), budget.total)
	}
	other := newAnswerCache(ih, budget)
	other.put("o", "fp", 0, http.Header{}, make([]byte, 100))
	if budget.total > budget.limit || other.entries["o"] != nil {
		t.Fatalf("total limit: budget %d, other cached %v", budget.total, other.entries["o"] != nil)
	}
	c.invalidate()
	if budget.total != 0 || c.size != 0 {
		t.Fatalf("after invalidate: budget %d size %d", budget.total, c.size)
	}
	// An answer computed before an invalidation is not stored after it.
	c.put("late", "fp", 0, http.Header{}, []byte("x"))
	if c.entries["late"] != nil {
		t.Fatal("an answer of an old generation was stored")
	}
	big := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, cacheEntryLimit+1))
	})
	c.limit, budget.limit = cacheIndexLimit, cacheTotalLimit
	for _, want := range []string{"miss", "miss"} {
		rec := httptest.NewRecorder()
		c.serve(rec, httptest.NewRequest(http.MethodGet, "/api/v0/architecture", nil), big)
		if rec.Header().Get(cacheHeader) != want || rec.Body.Len() != cacheEntryLimit+1 {
			t.Fatalf("big answer: %q %d", rec.Header().Get(cacheHeader), rec.Body.Len())
		}
	}
}

// TestWorkspaceGuards checks WS-21: every workspace POST passes the guard
// of state-changing POSTs plus a loopback peer, the session token, a JSON
// content type and a strict body of at most 4 KiB; other methods are 405.
func TestWorkspaceGuards(t *testing.T) {
	f := newWSFixture(t, sharedOpen(nil), nil)
	dir := t.TempDir()
	body, _ := json.Marshal(map[string]string{"path": dir})
	for _, p := range []string{workspaceCheckPath, workspaceAddPath, workspaceRemovePath} {
		for name, tc := range map[string]struct {
			method string
			body   string
			header map[string]string
			status int
			code   string
		}{
			"missing token":    {http.MethodPost, string(body), map[string]string{tokenHeader: ""}, 403, "token_required"},
			"wrong token":      {http.MethodPost, string(body), map[string]string{tokenHeader: "nope"}, 403, "token_required"},
			"not loopback":     {http.MethodPost, string(body), map[string]string{"RemoteAddr": "192.0.2.7:4000"}, 403, "not_loopback"},
			"cross-site":       {http.MethodPost, string(body), map[string]string{"Sec-Fetch-Site": "cross-site"}, 403, "cross_site_request"},
			"same-site":        {http.MethodPost, string(body), map[string]string{"Sec-Fetch-Site": "same-site"}, 403, "cross_site_request"},
			"foreign origin":   {http.MethodPost, string(body), map[string]string{"Origin": "http://evil.example"}, 403, "cross_site_request"},
			"other own origin": {http.MethodPost, string(body), map[string]string{"Origin": "http://localhost:7420"}, 403, "cross_site_request"},
			"host":             {http.MethodPost, string(body), map[string]string{"Host": "evil.example:7420"}, 403, "host_not_allowed"},
			"content type":     {http.MethodPost, string(body), map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type"},
			"unknown field":    {http.MethodPost, `{"path":"/x","id":"y","confirm":"z","folder":1}`, nil, 400, "bad_request"},
			"duplicate key":    {http.MethodPost, `{"path":"/x","path":"/y"}`, nil, 400, "bad_request"},
			"two objects":      {http.MethodPost, `{}{}`, nil, 400, "bad_request"},
			"too large":        {http.MethodPost, `{"path":"` + strings.Repeat("a", maxWorkspaceBody) + `"}`, nil, 413, "too_large"},
			"get":              {http.MethodGet, "", nil, 405, "method_not_allowed"},
			"put":              {http.MethodPut, string(body), nil, 405, "method_not_allowed"},
		} {
			req := httptest.NewRequest(tc.method, p, strings.NewReader(tc.body))
			req.Host, req.RemoteAddr = testHost, "127.0.0.1:5000"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(tokenHeader, f.ws.token)
			for k, v := range tc.header {
				switch {
				case k == "RemoteAddr":
					req.RemoteAddr = v
				case k == "Host":
					req.Host = v
				case v == "":
					req.Header.Del(k)
				default:
					req.Header.Set(k, v)
				}
			}
			rec := httptest.NewRecorder()
			f.ws.handler.ServeHTTP(rec, req)
			if rec.Code != tc.status || errorCode(t, rec) != tc.code {
				t.Errorf("%s %s: %d %s, want %d %s", p, name, rec.Code, rec.Body.String(), tc.status, tc.code)
			}
		}
	}
	if len(f.ws.confirms) != 0 {
		t.Fatal("a refused request issued a confirm value")
	}
	// The token is served only behind the GET guard.
	if rec := f.do(http.MethodGet, workspacePath, "", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != 403 || strings.Contains(rec.Body.String(), f.ws.token) {
		t.Fatalf("cross-site list: %d", rec.Code)
	}
}

// checkPath posts a typed path to check.
func (f *wsFixture) checkPath(path string) checkResponse {
	f.t.Helper()
	body, _ := json.Marshal(map[string]string{"path": path})
	var out checkResponse
	f.json(http.MethodPost, workspaceCheckPath, string(body), http.StatusOK, &out)
	return out
}

// TestWorkspaceCheckPath checks WS-22..WS-33 and WS-D, one refusal per
// rule, in table order; a refusal never issues a confirm value.
func TestWorkspaceCheckPath(t *testing.T) {
	home := t.TempDir()
	f := newWSFixture(t, sqliteOpen, func(o *WorkspaceOptions) { o.Home = home })
	base := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		r, _ := workspace.Resolve(p)
		return r
	}
	registered := mk("registered")
	reg := f.register(workspace.KindRepository, "registered", registered)
	file := filepath.Join(mk("files"), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	project := mk("shop")
	writeManifest(t, project, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n")
	badProject := mk("badshop")
	writeManifest(t, badProject, "version: 1\nname: \n")
	named := mk("named")
	if err := os.WriteFile(filepath.Join(named, config.File), []byte("version: 1\nrepository: billing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	badConfig := mk("badconfig")
	if err := os.WriteFile(filepath.Join(badConfig, config.File), []byte("version: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(base) + string(filepath.Separator)
	for _, tc := range []struct {
		path, row, code string
	}{
		{"relative/path", "WS-22", "path_not_absolute"},
		{filepath.Join(base, "missing"), "WS-24", "path_not_found"},
		{file, "WS-25", "not_a_directory"},
		{root, "WS-26", "filesystem_root"},
		{home, "WS-27", "home_directory"},
		{f.cfg, "WS-28", "config_directory"},
		{mk("store/.graphvein/inner"), "WS-29", "inside_index_store"},
		{mk("repo/.git"), "WS-29", "inside_index_store"},
		{mk("registered/sub"), "WS-30", "inside_registered"},
		{badProject, "WS-32", "invalid_manifest"},
		{badConfig, "WS-33", "invalid_config"},
		{filepath.Join(base, "bad\x00name"), "WS-D", "path_unreadable"},
	} {
		got := f.checkPath(tc.path)
		if got.Outcome != outcomeRefused || got.Refusal == nil || got.Refusal.Row != tc.row || got.Refusal.Code != tc.code || got.Confirm != nil {
			t.Errorf("%q: %+v %+v, want %s %s", tc.path, got, got.Refusal, tc.row, tc.code)
		}
	}
	if err := os.MkdirAll(filepath.Join(f.cfg, "sub"), 0o755); err == nil {
		if got := f.checkPath(filepath.Join(f.cfg, "sub")); got.Refusal == nil || got.Refusal.Code != "config_directory" {
			t.Errorf("inside the config folder: %+v", got)
		}
	}
	if got := f.checkPath(mk("registered/sub")); got.Refusal.Entry == nil || *got.Refusal.Entry != reg.ID {
		t.Errorf("inside registered names no entry: %+v", got.Refusal)
	}
	if rec := f.do(http.MethodPost, workspaceCheckPath, `{"path":""}`, nil); rec.Code != 400 {
		t.Errorf("empty path: %d", rec.Code)
	}
	// WS-31: a registered folder is not refused and gets no confirm.
	if got := f.checkPath(registered); got.Outcome != outcomeRegistered || got.Entry == nil || *got.Entry != reg.ID || got.Confirm != nil || got.Command == nil {
		t.Errorf("registered: %+v", got)
	}
	// WS-32: a manifest makes a project with its members.
	if got := f.checkPath(project); got.Outcome != outcomeReady || *got.Kind != workspace.KindProject || *got.Name != "shop" || len(got.Members) != 1 || got.Members[0].Checkout || got.Confirm == nil || !strings.Contains(*got.Command, "--project") {
		t.Errorf("project: %+v", got)
	}
	// WS-33: graphvein.yaml names the repository; else the folder name.
	if got := f.checkPath(named); got.Outcome != outcomeReady || *got.Kind != workspace.KindRepository || *got.Name != "billing" || got.Members != nil {
		t.Errorf("named: %+v", got)
	}
	plain := mk("plain")
	if got := f.checkPath(plain); *got.Name != "plain" || got.Store == nil || got.Store.Path != filepath.Join(plain, sqlite.DefaultPath) {
		t.Errorf("plain: %+v", got)
	}
	// WS-23: a symbolic link is resolved, and the target is what is named.
	link := filepath.Join(base, "link")
	if err := os.Symlink(plain, link); err == nil {
		if got := f.checkPath(link); got.Resolved == nil || *got.Resolved != plain || got.Path != link {
			t.Errorf("symlink: %+v", got)
		}
	} else {
		t.Logf("symlinks unavailable here: %v", err)
	}
	if !strings.HasPrefix(indexCommandFor(workspace.KindRepository, "/a/it's"), "graphvein index '/a/it'\\''s'") {
		t.Errorf("quoting: %s", indexCommandFor(workspace.KindRepository, "/a/it's"))
	}
}

// TestWorkspaceCheckIndexPresence checks WS-34: present (with the last
// run) only from stored runs; absent when no store exists or it holds no
// run; unknown when the store does not open.
func TestWorkspaceCheckIndexPresence(t *testing.T) {
	indexed := indexedFolder(t, "indexed")
	bare := fixtureRoot(t)
	emptyStore := fixtureRoot(t)
	if s, err := sqlite.Open(context.Background(), filepath.Join(emptyStore, sqlite.DefaultPath)); err != nil {
		t.Fatal(err)
	} else {
		s.Close()
	}
	broken := fixtureRoot(t)
	f := newWSFixture(t, func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error) {
		if r, _ := workspace.Resolve(broken); e.Path == r {
			return nil, errors.New("disk on fire")
		}
		return sqliteOpen(ctx, e, create)
	}, nil)
	for dir, want := range map[string]string{indexed: indexPresent, bare: indexAbsent, emptyStore: indexAbsent, broken: indexUnknown} {
		got := f.checkPath(dir)
		if got.Index == nil || got.Index.State != want {
			t.Errorf("%s: %+v, want %s", dir, got.Index, want)
			continue
		}
		if (want == indexPresent) != (got.Index.LastRun != nil) || (want == indexUnknown) != (got.Index.Error != nil) {
			t.Errorf("%s: %+v", dir, got.Index)
		}
	}
	if _, err := os.Stat(filepath.Join(bare, ".graphvein")); !os.IsNotExist(err) {
		t.Fatal("check created a store")
	}
}

// TestWorkspaceConfirm checks WS-35: the confirm value is single-use,
// bound to the resolved folder, expires after 10 minutes and at most 32
// are pending; add re-checks the folder.
func TestWorkspaceConfirm(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	add := func(path, confirm string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"path": path, "confirm": confirm, "index": false})
		return f.do(http.MethodPost, workspaceAddPath, string(body), nil)
	}
	a, b := fixtureRoot(t), fixtureRoot(t)
	ca := f.checkPath(a)
	if rec := add(*ca.Resolved, "made-up"); errorCode(t, rec) != "confirm_required" {
		t.Fatalf("made-up value: %s", rec.Body.String())
	}
	cb := f.checkPath(b)
	if rec := add(*ca.Resolved, *cb.Confirm); errorCode(t, rec) != "confirm_required" {
		t.Fatalf("value of another folder: %s", rec.Body.String())
	}
	if rec := add(*cb.Resolved, *cb.Confirm); errorCode(t, rec) != "confirm_required" {
		t.Fatalf("a value is used up by a refused add: %s", rec.Body.String())
	}
	if rec := add(*ca.Resolved, *ca.Confirm); rec.Code != http.StatusOK {
		t.Fatalf("add: %d %s", rec.Code, rec.Body.String())
	}
	if rec := add(*ca.Resolved, *ca.Confirm); errorCode(t, rec) != "confirm_required" {
		t.Fatalf("reuse: %s", rec.Body.String())
	}
	cb = f.checkPath(b)
	f.advance(confirmLifetime)
	if rec := add(*cb.Resolved, *cb.Confirm); errorCode(t, rec) != "confirm_required" {
		t.Fatalf("expired: %s", rec.Body.String())
	}
	// The folder changed between check and add.
	cb = f.checkPath(b)
	writeManifest(t, b, "version: 1\nname: shop\nmembers:\n  - repository: x\n    path: x\n")
	if rec := add(*cb.Resolved, *cb.Confirm); errorCode(t, rec) != "path_changed" {
		t.Fatalf("changed: %s", rec.Body.String())
	}
	if expires, err := time.Parse(time.RFC3339, *f.checkPath(fixtureRoot(t)).Expires); err != nil || !expires.Equal(f.clock().Add(confirmLifetime).Truncate(time.Second)) {
		t.Fatalf("expiry %v %v", expires, err)
	}
	var first string
	for i := 0; i <= maxConfirms; i++ {
		c := f.checkPath(b)
		if i == 0 {
			first = *c.Confirm
		}
		f.advance(time.Second)
	}
	if len(f.ws.confirms) != maxConfirms {
		t.Fatalf("%d pending", len(f.ws.confirms))
	}
	if _, ok := f.ws.confirms[first]; ok {
		t.Fatal("the oldest value was kept beyond the cap")
	}
}

// TestWorkspaceAddRuns checks WS-11 and WS-36: add registers the checked
// folder and starts its index run (the job of GET /api/v0/index/run?ws=,
// with run-counted progress), creating the store where graphvein index
// would; the list shows the job on the entry.
func TestWorkspaceAddRuns(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	dir := fixtureRoot(t)
	c := f.checkPath(dir)
	if c.Index.State != indexAbsent {
		t.Fatalf("check %+v", c.Index)
	}
	body, _ := json.Marshal(map[string]string{"path": *c.Resolved, "confirm": *c.Confirm})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusAccepted, &out)
	if !out.Added || out.Job == nil || out.Entry.ID == "" || out.Entry.State != entryOpen {
		t.Fatalf("add %+v", out)
	}
	doc := waitJob(t, f, out.Entry.ID, jobCommitted)
	if doc.Repository != *c.Name || doc.Job.Run == nil {
		t.Fatalf("run %+v", doc)
	}
	if _, err := os.Stat(filepath.Join(*c.Resolved, sqlite.DefaultPath)); err != nil {
		t.Fatal("no store at the CLI's location:", err)
	}
	it := f.item(out.Entry.ID)
	if it.Job == nil || it.Job.State != jobCommitted || !it.Registered {
		t.Fatalf("listed %+v", it)
	}
	entries, err := f.reg.Entries()
	if err != nil || len(entries) != 1 || entries[0].Path != *c.Resolved {
		t.Fatalf("registry %+v %v", entries, err)
	}
	var st statusResponse
	f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(out.Entry.ID), "", http.StatusOK, &st)
	if st.Freshness.State != stateFresh {
		t.Fatalf("status %+v", st)
	}
}

// TestWorkspaceAddWithoutIndex checks WS-37: "index": false only
// registers; no store is created and no job runs.
func TestWorkspaceAddWithoutIndex(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	dir := fixtureRoot(t)
	c := f.checkPath(dir)
	body, _ := json.Marshal(map[string]any{"path": *c.Resolved, "confirm": *c.Confirm, "index": false})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusOK, &out)
	if out.Job != nil || out.Entry.State != entryNotOpen || !out.Added {
		t.Fatalf("add %+v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".graphvein")); !os.IsNotExist(err) {
		t.Fatal("a store was created")
	}
	if rec := f.do(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(out.Entry.ID), "", nil); errorCode(t, rec) != "no_index" {
		t.Fatalf("status: %s", rec.Body.String())
	}
}

// TestWorkspaceAddProject checks WS-38: adding a folder with a manifest
// registers the project and runs the project re-index (members, then the
// link pass), with the project job document.
func TestWorkspaceAddProject(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, dir, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n  - repository: web\n    path: web\n")
	c := f.checkPath(dir)
	body, _ := json.Marshal(map[string]string{"path": *c.Resolved, "confirm": *c.Confirm})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusAccepted, &out)
	if out.Entry.Kind != workspace.KindProject || out.Job == nil || out.Job.Project == nil {
		t.Fatalf("add %+v", out)
	}
	doc := waitJob(t, f, out.Entry.ID, jobCommitted)
	if doc.Project != "shop" || doc.Job.Project.Members[0].State != memberCommitted || doc.Job.Project.Members[1].State != memberNoCheckout {
		t.Fatalf("project job %+v", doc.Job.Project)
	}
}

// TestWorkspaceRemove checks WS-39: remove drops the registry entry only
// (the folder and its index stay), closes the open index, refuses the
// command-line entry and an entry whose run is running, and an unknown id.
func TestWorkspaceRemove(t *testing.T) {
	root := indexedFolder(t, "alpha")
	pinnedDir := indexedFolder(t, "pinned")
	pinned, err := sqliteOpen(context.Background(), workspace.Entry{Kind: workspace.KindRepository, Name: "pinned", Path: pinnedDir}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	pinned.Entry.ID = workspace.EntryID(workspace.KindRepository, pinnedDir, "pinned")
	cur := *pinned
	cur.Close = nil
	f := newWSFixture(t, sqliteOpen, func(o *WorkspaceOptions) { o.Current = &cur })
	a := f.register(workspace.KindRepository, "alpha", root)
	remove := func(id string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"id": id})
		return f.do(http.MethodPost, workspaceRemovePath, string(body), nil)
	}
	if rec := remove(cur.Entry.ID); errorCode(t, rec) != "entry_in_use" {
		t.Fatalf("pinned: %s", rec.Body.String())
	}
	f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(a.ID), "", http.StatusOK, nil)
	f.ws.mu.Lock()
	oi := f.ws.open[a.ID]
	f.ws.mu.Unlock()
	oi.handler.runs.mu.Lock()
	oi.handler.runs.job = &jobDoc{ID: "x", State: jobRunning}
	oi.handler.runs.mu.Unlock()
	if rec := remove(a.ID); errorCode(t, rec) != "index_running" {
		t.Fatalf("running: %s", rec.Body.String())
	}
	oi.handler.runs.mu.Lock()
	oi.handler.runs.job.EndedAt = newPtr("done")
	oi.handler.runs.mu.Unlock()
	if rec := remove(a.ID); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if f.isOpen(a.ID) || f.count(f.closes, "alpha") != 1 {
		t.Fatalf("not closed: %v", f.closes)
	}
	if _, err := os.Stat(filepath.Join(root, sqlite.DefaultPath)); err != nil {
		t.Fatal("remove deleted the index:", err)
	}
	if rec := remove(a.ID); errorCode(t, rec) != "unknown_entry" {
		t.Fatalf("second remove: %s", rec.Body.String())
	}
	if rec := f.do(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(a.ID), "", nil); errorCode(t, rec) != "unknown_entry" {
		t.Fatalf("removed entry still served: %s", rec.Body.String())
	}
}

// TestIndexRunProjectShape checks WS-40: under serve --project, GET
// /api/v0/index/run carries "repository" (empty) next to "project", the
// shape web/src/api/indexRun.ts reads.
func TestIndexRunProjectShape(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n")
	wi, err := sqliteOpen(context.Background(), workspace.Entry{Kind: workspace.KindProject, Name: "shop", Path: dir}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer wi.Close()
	opt := wi.Options
	opt.Version, opt.Now = "test", fixedNow
	h, runs, refs := newServerHandler(opt, testHosts)
	defer refs.shutdown()
	defer runs.stop()
	req := httptest.NewRequest(http.MethodGet, indexRunPath, nil)
	req.Host = testHost
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || rec.Code != 200 {
		t.Fatal(rec.Code, err, rec.Body.String())
	}
	if string(raw["repository"]) != `""` || string(raw["project"]) != `"shop"` {
		t.Fatalf("index run %s", rec.Body.String())
	}
}

// TestWorkspaceContractTable keeps docs/contract/claims/workspace.md's WS
// rows and workspaceRows 1:1, checks that each row's test exists and that
// every row a refusal names is in the table.
func TestWorkspaceContractTable(t *testing.T) {
	doc, err := os.ReadFile("../../docs/contract/claims/workspace.md")
	if err != nil {
		t.Fatal(err)
	}
	// The web UI's rows (WS-41..WS-69) follow "## Web UI" and are kept 1:1
	// with the UI by web/src/shell/workspace/rows.test.ts.
	if i := bytes.Index(doc, []byte("\n## Web UI")); i >= 0 {
		doc = doc[:i]
	}
	var documented, code []string
	for _, m := range regexp.MustCompile(`(?m)^\| (WS-[0-9D]+) \|`).FindAllSubmatch(doc, -1) {
		documented = append(documented, string(m[1]))
	}
	known := map[string]bool{}
	for _, r := range workspaceRows {
		code = append(code, r.ID)
		known[r.ID] = true
	}
	if strings.Join(documented, ",") != strings.Join(code, ",") {
		t.Fatalf("workspace.md rows %v\ncode rows %v", documented, code)
	}
	var sources strings.Builder
	for _, pattern := range []string{"*_test.go", "../workspace/*_test.go", "../../cmd/graphvein/*_test.go"} {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			sources.Write(data)
		}
	}
	for _, r := range workspaceRows {
		if !strings.Contains(sources.String(), "func "+r.Test+"(t *testing.T)") {
			t.Errorf("row %s names test %s, which does not exist", r.ID, r.Test)
		}
	}
	src, err := os.ReadFile("workspace_paths.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`"(WS-[0-9D]+)"`).FindAllSubmatch(src, -1) {
		if !known[string(m[1])] {
			t.Errorf("refusal row %s is not in the table", m[1])
		}
	}
}

// TestWorkspaceReportURL checks that a multi-index server hands its own
// build-time wrong-fact issue form (ADR 0089) to every index it opens:
// GET /api/v0/ui answers the same report_url with and without ws=.
func TestWorkspaceReportURL(t *testing.T) {
	const form = "https://github.com/example/project/issues/new"
	alpha, beta := t.TempDir(), t.TempDir()
	reg := workspace.Open(filepath.Join(t.TempDir(), "config", "graphvein", "workspace.yaml"))
	current := &WorkspaceIndex{Entry: workspace.Entry{Kind: workspace.KindRepository, Name: "alpha", Path: alpha}, Options: Options{Root: alpha, Repository: "alpha"}}
	current.Entry.ID = workspace.EntryID(current.Entry.Kind, current.Entry.Path, current.Entry.Name)
	b, _, err := reg.Add(workspace.Entry{Kind: workspace.KindRepository, Name: "beta", Path: beta})
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Version: "test", Assets: fstest.MapFS{"index.html": {Data: []byte("<p>t</p>")}}, Now: fixedNow, ReportURL: form,
		Workspace: &WorkspaceOptions{Registry: reg, Current: current, Open: sharedOpen(nil), Home: t.TempDir(), CheckInterval: -1,
			StoreLocation: func(folder string) StoreLocation {
				return StoreLocation{Kind: "sqlite", Path: filepath.Join(folder, sqlite.DefaultPath)}
			}}}
	ws := newWorkspaceServer(opt, testHosts)
	t.Cleanup(ws.shutdown)
	for _, target := range []string{"/api/v0/ui", "/api/v0/ui?ws=" + url.QueryEscape(b.ID)} {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Host = testHost
		req.RemoteAddr = "127.0.0.1:50000"
		rec := httptest.NewRecorder()
		ws.handler.ServeHTTP(rec, req)
		var out uiResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body.String())
		}
		if out.ReportURL == nil || *out.ReportURL != form {
			t.Errorf("%s: report_url %v", target, out.ReportURL)
		}
	}
}

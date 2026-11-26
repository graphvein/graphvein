package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/workspace"
)

// This file holds the tests of the T038 review (R3-R9 and
// the follow-ups), each built from the review's own input.

// TestWorkspaceProjectFreshnessListed checks WS-9 for projects (R3): the
// project's freshness is folded at list time over the members the manifest
// lists now. A member added since the check has no freshness, so the
// project is not checked (null), never fresh; a member found stale makes
// the project stale.
func TestWorkspaceProjectFreshnessListed(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	shop := t.TempDir()
	for _, m := range []string{"api", "web", "extra"} {
		if err := os.Mkdir(filepath.Join(shop, m), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	two := "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n  - repository: web\n    path: web\n"
	writeManifest(t, shop, two)
	p := f.register(workspace.KindProject, "shop", shop)
	at := func(m int) string {
		return fixedNow().Add(time.Duration(m) * time.Minute).UTC().Format(time.RFC3339Nano)
	}
	// The check the review saw: shop (api, web) checked fresh at start.
	f.ws.mu.Lock()
	paths := map[string]string{"api": filepath.Join(p.Path, "api"), "web": filepath.Join(p.Path, "web")}
	f.ws.checks[p.ID] = entryCheck{doc: checkDoc{State: stateFresh, CheckedAt: at(0)}, members: map[string]checkDoc{
		"api": {State: stateFresh, CheckedAt: at(0)},
		"web": {State: stateFresh, CheckedAt: at(1)},
	}, paths: paths}
	f.ws.mu.Unlock()
	if it := f.item(p.ID); it.Freshness == nil || it.Freshness.State != stateFresh || it.Freshness.CheckedAt != at(0) {
		t.Fatalf("both members fresh: %+v", it.Freshness)
	}
	// A member "extra" appended to the manifest after the check.
	writeManifest(t, shop, two+"  - repository: extra\n    path: extra\n")
	it := f.item(p.ID)
	if it.Freshness != nil {
		t.Fatalf("a member was never checked, but the project is %+v", it.Freshness)
	}
	if len(it.Members) != 3 || it.Members[2].Repository != "extra" || it.Members[2].Freshness != nil {
		t.Fatalf("members %+v", it.Members)
	}
	// Back to two members; a status request finds api stale.
	writeManifest(t, shop, two)
	f.ws.mu.Lock()
	f.ws.checks[p.ID] = entryCheck{doc: checkDoc{State: stateFresh, CheckedAt: at(0)}, members: map[string]checkDoc{
		"api": {State: stateStale, CheckedAt: at(2), ChangedCount: newPtr(1)},
		"web": {State: stateFresh, CheckedAt: at(1)},
	}, paths: paths}
	f.ws.mu.Unlock()
	if it := f.item(p.ID); it.Freshness == nil || it.Freshness.State != stateStale || it.Freshness.CheckedAt != at(1) {
		t.Fatalf("a member stale: %+v", it.Freshness)
	}
	// A manifest that does not load folds nothing.
	writeManifest(t, shop, "version: 1\nname: shop\nmembers: nope\n")
	if it := f.item(p.ID); it.Freshness != nil {
		t.Fatalf("broken manifest folded: %+v", it.Freshness)
	}
}

// TestWorkspaceProjectMemberRepointed checks WS-9 for a member whose path
// the manifest changes while the project is open (T038 re-check W1): the
// check recorded for the old path is not shown for the new one, so the
// member and the project are not checked (null), also after a background
// check of the open project (which still walks the path it opened with);
// a status request of the member reads the edited manifest and checks the
// new path, which the list then shows.
func TestWorkspaceProjectMemberRepointed(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	shop := filepath.Join(t.TempDir(), "shop")
	for _, m := range []string{"web", "web2"} {
		if err := os.CopyFS(filepath.Join(shop, m), os.DirFS("../../testdata/repos/go")); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(shop, "web2", "extra.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := sqlite.Open(context.Background(), filepath.Join(shop, sqlite.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	indexRepository(t, filepath.Join(shop, "web"), w, "web")
	w.Close()
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: web\n    path: web\n")
	p := f.register(workspace.KindProject, "shop", shop)
	status := "/api/v0/status?ws=" + url.QueryEscape(p.ID) + "&repository=web"
	f.json(http.MethodGet, status, "", http.StatusOK, nil)
	if it := f.item(p.ID); it.Freshness == nil || it.Freshness.State != stateFresh || it.Members[0].Freshness == nil {
		t.Fatalf("before the edit: %+v %+v", it.Freshness, it.Members)
	}
	// The review's input: path: web changed to path: web2 (a copy with a
	// file added) while the project is open.
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: web\n    path: web2\n")
	notChecked := func(when string) {
		t.Helper()
		it := f.item(p.ID)
		if len(it.Members) != 1 || it.Members[0].Path == nil || *it.Members[0].Path != filepath.Join(p.Path, "web2") {
			t.Fatalf("%s: members %+v", when, it.Members)
		}
		if it.Freshness != nil || it.Members[0].Freshness != nil {
			t.Fatalf("%s: the old path's check is shown for the new one: project %+v member %+v", when, it.Freshness, it.Members[0].Freshness)
		}
	}
	notChecked("after the edit")
	f.ws.checkEntry(context.Background(), p)
	notChecked("after a background check of the open project")
	var st statusResponse
	f.json(http.MethodGet, status, "", http.StatusOK, &st)
	if st.Freshness.State != stateStale {
		t.Fatalf("status of the re-pointed member walked the old folder: %+v", st.Freshness)
	}
	it := f.item(p.ID)
	if it.Members[0].Freshness == nil || it.Members[0].Freshness.State != stateStale || it.Freshness == nil || it.Freshness.State != stateStale {
		t.Fatalf("after the status of the new path: project %+v member %+v", it.Freshness, it.Members[0].Freshness)
	}
	// Every member endpoint reads an edited manifest too, not only a
	// status or project request (T038 follow-up WS-F3, integration): after
	// path: web3, the member's Settings answer comes from web3's
	// graphvein.yaml with no status or project request in between.
	if err := os.CopyFS(filepath.Join(shop, "web3"), os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "web3", "graphvein.yaml"), []byte("version: 1\nrepository: web\nui:\n  labels: technical\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: web\n    path: web3\n")
	var cfg struct {
		Keys []struct {
			Key    string `json:"key"`
			Value  any    `json:"value"`
			Origin string `json:"origin"`
		} `json:"keys"`
	}
	f.json(http.MethodGet, "/api/v0/config?ws="+url.QueryEscape(p.ID)+"&repository=web", "", http.StatusOK, &cfg)
	labels := ""
	for _, k := range cfg.Keys {
		if k.Key == "ui.labels" && k.Origin == "file" {
			labels, _ = k.Value.(string)
		}
	}
	if labels != "technical" {
		t.Fatalf("a member endpoint after the manifest edit read the old path: %+v", cfg.Keys)
	}
}

// TestWorkspaceProjectLinkChanges checks that the project link-changes
// endpoint added with the MCP link_changes tool takes ws= like every other
// per-index path: the workspace server strips it and the project's own
// handler answers (no link pass stored yet, so no comparison), and an
// unknown entry is refused.
func TestWorkspaceProjectLinkChanges(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	shop := filepath.Join(t.TempDir(), "shop")
	if err := os.CopyFS(filepath.Join(shop, "web"), os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	w, err := sqlite.Open(context.Background(), filepath.Join(shop, sqlite.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	indexRepository(t, filepath.Join(shop, "web"), w, "web")
	w.Close()
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: web\n    path: web\n")
	p := f.register(workspace.KindProject, "shop", shop)
	var got projectLinkChangesResponse
	f.json(http.MethodGet, projectLinkChangesPath+"?ws="+url.QueryEscape(p.ID), "", http.StatusOK, &got)
	if got.Project != "shop" || got.State != linkChangesNoComparison || got.Links != nil || got.LinksNote == nil {
		t.Fatalf("link changes through ws=: %+v", got)
	}
	if rec := f.do(http.MethodGet, projectLinkChangesPath+"?ws=no-such-entry", "", nil); errorCode(t, rec) != "unknown_entry" {
		t.Fatalf("unknown entry: %d %s", rec.Code, rec.Body.String())
	}
}

// TestWorkspaceMovedFolder checks WS-8, WS-11 and WS-20 for a folder moved
// while its index is open (R4, R6): the list says folder_gone (not open)
// and drops its old freshness, reports the job from memory, and neither
// the list, the run document nor a check creates anything at the old
// path.
func TestWorkspaceMovedFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not move a folder whose files are open")
	}
	f := newWSFixture(t, sqliteOpen, nil)
	r1 := indexedFolder(t, "r1")
	shop := t.TempDir()
	if err := os.Mkdir(filepath.Join(shop, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, shop, "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n")
	w, err := sqlite.Open(context.Background(), filepath.Join(shop, sqlite.DefaultPath))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	a := f.register(workspace.KindRepository, "r1", r1)
	p := f.register(workspace.KindProject, "shop", shop)
	for _, e := range []workspace.Entry{a, p} {
		f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(e.ID), "", http.StatusOK, nil)
		f.json(http.MethodGet, indexRunPath+"?ws="+url.QueryEscape(e.ID), "", http.StatusOK, nil)
		if it := f.item(e.ID); it.State != entryOpen || it.Freshness == nil {
			t.Fatalf("%s before the move: %+v", e.Name, it)
		}
	}
	moved := t.TempDir()
	for _, e := range []workspace.Entry{a, p} {
		if err := os.Rename(e.Path, filepath.Join(moved, e.Name)); err != nil {
			t.Fatal(err)
		}
	}
	nothingAt := func(when string) {
		t.Helper()
		for _, e := range []workspace.Entry{a, p} {
			if _, err := os.Stat(e.Path); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("%s: %s recreated the moved folder %s (%v)", when, e.Name, e.Path, err)
			}
		}
	}
	for _, it := range f.list().Entries {
		if it.State != entryFolderGone || it.Freshness != nil {
			t.Errorf("%s after the move: state %s freshness %+v", it.Name, it.State, it.Freshness)
		}
		for _, m := range it.Members {
			if m.Freshness != nil {
				t.Errorf("%s member %s keeps freshness %+v", it.Name, m.Repository, m.Freshness)
			}
		}
	}
	nothingAt("the list")
	// The run document still answers from the open index (the moved store
	// stays open until idle) and probes no lock folder into existence.
	f.json(http.MethodGet, indexRunPath+"?ws="+url.QueryEscape(a.ID), "", http.StatusOK, nil)
	nothingAt("the run document")
	for _, e := range []workspace.Entry{a, p} {
		f.ws.checkEntry(context.Background(), e)
		f.ws.mu.Lock()
		_, kept := f.ws.checks[e.ID]
		f.ws.mu.Unlock()
		if kept {
			t.Errorf("%s: the check before the move was kept", e.Name)
		}
	}
	nothingAt("a check")
	// Starting a run would create a store: refused for a gone folder that
	// is not open.
	f.ws.mu.Lock()
	oi := f.ws.open[a.ID]
	delete(f.ws.open, a.ID)
	f.ws.mu.Unlock()
	oi.shutdown()
	if rec := f.do(http.MethodPost, indexRunPath+"?ws="+url.QueryEscape(a.ID), "{}", nil); errorCode(t, rec) != "folder_gone" {
		t.Fatalf("run of a gone folder: %d %s", rec.Code, rec.Body.String())
	}
	nothingAt("a run request")
}

// TestWorkspaceRecheckNotOpen checks WS-20's schedule for entries that are
// not open (R5): once their check is older than notOpenRecheck a pass
// checks them again, and an entry added after the first pass is checked
// at once (R7).
func TestWorkspaceRecheckNotOpen(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, func(o *WorkspaceOptions) { o.CheckInterval = 20 * time.Millisecond })
	r1 := indexedFolder(t, "r1")
	a := f.register(workspace.KindRepository, "r1", r1)
	f.ws.start()
	wait := func(what string, ok func(workspaceItem) bool, id string) workspaceItem {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			it := f.item(id)
			if ok(it) {
				return it
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %+v", what, it)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	first := wait("first pass", func(it workspaceItem) bool { return it.Freshness != nil }, a.ID)
	if first.Freshness.State != stateFresh || first.State != entryNotOpen {
		t.Fatalf("first pass %+v", first)
	}
	// The review's input: r1 (not open) edited after the first pass.
	if err := os.WriteFile(filepath.Join(r1, "main.go"), []byte("package main\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if it := f.item(a.ID); it.Freshness.CheckedAt != first.Freshness.CheckedAt {
		t.Fatalf("rechecked before the bound: %+v", it.Freshness)
	}
	f.advance(notOpenRecheck)
	it := wait("recheck after the bound", func(it workspaceItem) bool { return it.Freshness.CheckedAt != first.Freshness.CheckedAt }, a.ID)
	if it.Freshness.State != stateStale {
		t.Fatalf("recheck %+v", it.Freshness)
	}
	// An entry added without a run is checked now, so it reads "not
	// indexed" rather than "not checked yet".
	c := f.checkPath(fixtureRoot(t))
	body, _ := json.Marshal(map[string]any{"path": *c.Resolved, "confirm": *c.Confirm, "index": false})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusOK, &out)
	added := wait("added entry", func(it workspaceItem) bool { return it.Freshness != nil }, out.Entry.ID)
	if added.Freshness.State != stateNotIndexed {
		t.Fatalf("added entry %+v", added.Freshness)
	}
}

// TestWorkspaceIndexAddedEntry checks R7 end to end: an entry added with
// "index": false says no_index with a message that does not send the user
// back to add; a check of its folder answers registered; POST
// /api/v0/index/run?ws= creates its store and runs it, after which it is
// fresh.
func TestWorkspaceIndexAddedEntry(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	dir := fixtureRoot(t)
	c := f.checkPath(dir)
	body, _ := json.Marshal(map[string]any{"path": *c.Resolved, "confirm": *c.Confirm, "index": false})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusOK, &out)
	id := out.Entry.ID
	rec := f.do(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(id), "", nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"no_index"`) || strings.Contains(rec.Body.String(), "add the folder again") || !strings.Contains(rec.Body.String(), "index/run?ws=") {
		t.Fatalf("status of an entry without a store: %d %s", rec.Code, rec.Body.String())
	}
	if again := f.checkPath(dir); again.Outcome != outcomeRegistered || again.Entry == nil || *again.Entry != id {
		t.Fatalf("check again: %+v", again)
	}
	f.json(http.MethodPost, indexRunPath+"?ws="+url.QueryEscape(id), "{}", http.StatusAccepted, nil)
	waitJob(t, f, id, jobCommitted)
	if _, err := os.Stat(filepath.Join(*c.Resolved, sqlite.DefaultPath)); err != nil {
		t.Fatal("no store after Index it:", err)
	}
	var st statusResponse
	f.json(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(id), "", http.StatusOK, &st)
	if st.Freshness.State != stateFresh {
		t.Fatalf("status after the run %+v", st)
	}
}

// TestWorkspaceAddRunRefused checks WS-36 (R9): when the run is refused
// after the entry was registered (here: another process holds the index
// lock, as a running graphvein index does), the add answers 200 with the
// entry, no job and the run's refusal; the entry is listed.
func TestWorkspaceAddRunRefused(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	dir := fixtureRoot(t)
	c := f.checkPath(dir)
	holder := sqlite.IndexLocker(filepath.Join(*c.Resolved, sqlite.DefaultPath))
	defer holder.Close()
	lease, err := holder.Acquire(context.Background(), indexrun.LockWait)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	body, _ := json.Marshal(map[string]string{"path": *c.Resolved, "confirm": *c.Confirm})
	var out addResponse
	f.json(http.MethodPost, workspaceAddPath, string(body), http.StatusOK, &out)
	if !out.Added || out.Job != nil || out.RunError == nil || out.RunError.Code != "index_locked" || out.RunError.Message == "" || out.Entry.ID == "" {
		t.Fatalf("add with the lock held: %+v %+v", out, out.RunError)
	}
	if it := f.item(out.Entry.ID); !it.Registered {
		t.Fatalf("listed %+v", it)
	}
	// An open failure is reported the same way, with the entry.
	f2 := newWSFixture(t, func(context.Context, workspace.Entry, bool) (*WorkspaceIndex, error) {
		return nil, fmt.Errorf("%w: GRAPHVEIN_SQLITE_PATH names one SQLite file", ErrSingleStore)
	}, nil)
	c2 := f2.checkPath(fixtureRoot(t))
	body, _ = json.Marshal(map[string]string{"path": *c2.Resolved, "confirm": *c2.Confirm})
	var out2 addResponse
	f2.json(http.MethodPost, workspaceAddPath, string(body), http.StatusOK, &out2)
	if out2.RunError == nil || out2.RunError.Code != "single_store" || !out2.Added {
		t.Fatalf("add with an open failure: %+v", out2)
	}
	// WS-15: the same refusal on a per-index request.
	if rec := f2.do(http.MethodGet, "/api/v0/status?ws="+url.QueryEscape(out2.Entry.ID), "", nil); errorCode(t, rec) != "single_store" {
		t.Fatalf("status: %d %s", rec.Code, rec.Body.String())
	}
}

// TestWorkspaceCheckSystemFolders checks WS-53 (R8): the operating
// system's own folders are refused as typed paths, on this machine and,
// through the rule's table, on the others.
func TestWorkspaceCheckSystemFolders(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	none := env(nil)
	for _, tc := range []struct {
		goos, path string
		getenv     func(string) string
		refused    bool
	}{
		{"linux", "/tmp", none, true},
		{"linux", "/etc", none, true},
		{"linux", "/dev", none, true},
		{"linux", "/proc", none, true},
		{"linux", "/proc/1", none, true},
		{"linux", "/sys/kernel", none, true},
		{"linux", "/var", none, true},
		{"linux", "/usr", none, true},
		{"linux", "/bin", none, true},
		{"linux", "/sbin", none, true},
		{"linux", "/boot", none, true},
		{"linux", "/lib", none, true},
		{"linux", "/lib64", none, true},
		{"linux", "/run", none, true},
		{"linux", "/var/lib", none, true},
		{"linux", "/run/media/you/drive/app", none, false},
		{"linux", "/var/lib/app", none, false},
		{"linux", "/tmp/app", none, false},
		{"linux", "/usr/src/app", none, false},
		{"linux", "/var/www/app", none, false},
		{"linux", "/home/you/src/app", none, false},
		{"darwin", "/private/etc", none, true},
		{"darwin", "/private/var/folders/x/app", none, false},
		{"windows", `C:\Windows`, none, true},
		{"windows", `c:\windows\`, none, true},
		{"windows", `C:\Program Files`, none, true},
		{"windows", `C:\Program Files (x86)`, none, true},
		{"windows", `C:\ProgramData`, none, true},
		{"windows", `D:\Windows`, env(map[string]string{"SystemDrive": "D:"}), true},
		{"windows", `E:\OS`, env(map[string]string{"WINDIR": `E:\OS`}), true},
		{"windows", `C:\Windows\src\app`, none, false},
		{"windows", `C:\src\app`, none, false},
		// W3 of the re-check: device prefixes, the local administrative
		// share and the trailing dots and spaces Windows drops.
		{"windows", `\\?\C:\Windows`, none, true},
		{"windows", `\\.\C:\Windows`, none, true},
		{"windows", `\\?\UNC\localhost\C$\Windows`, none, true},
		{"windows", `\\localhost\C$\Windows`, none, true},
		{"windows", `\\LOCALHOST\c$\windows\`, none, true},
		{"windows", `C:\Windows.`, none, true},
		{"windows", `C:\Windows. .\`, none, true},
		{"windows", `C:\Program Files \`, none, true},
		{"windows", `C:/Windows`, none, true},
		{"windows", `C:\src\..\Windows`, none, true},
		{"windows", `C:\Windows\.`, none, true},
		{"windows", `\\?\C:\Windows\src\app`, none, false},
		{"windows", `\\localhost\C$\src\app`, none, false},
		{"windows", `\\otherhost\C$\Windows`, none, false},
		{"windows", `C:\Windows.old`, none, false},
	} {
		if _, got := systemFolder(tc.path, tc.path, tc.goos, tc.getenv, nil); got != tc.refused {
			t.Errorf("%s %q: refused %v, want %v", tc.goos, tc.path, got, tc.refused)
		}
	}
	// A spelling no string rule knows (a Windows 8.3 short name; here a
	// link the resolution is assumed to have missed) is caught on disk.
	if runtime.GOOS != "windows" {
		link := filepath.Join(t.TempDir(), "etc-link")
		if err := os.Symlink("/etc", link); err == nil {
			if dir, got := systemFolder(link, link, runtime.GOOS, os.Getenv, sameFolderOnDisk); !got || dir != "/etc" {
				t.Errorf("a link to /etc compared on disk: %q %v", dir, got)
			}
			if _, got := systemFolder(link, link, runtime.GOOS, os.Getenv, nil); got {
				t.Error("the link matched as a string")
			}
		}
	} else if short := os.Getenv("WINDIR"); short != "" {
		if _, got := systemFolder(short, short, runtime.GOOS, func(string) string { return "" }, sameFolderOnDisk); !got {
			t.Errorf("%s compared on disk was not refused", short)
		}
	}
	f := newWSFixture(t, sqliteOpen, nil)
	var system string
	if runtime.GOOS == "windows" {
		system = os.Getenv("WINDIR")
	} else {
		system = "/etc"
	}
	if system == "" {
		t.Skip("no system folder known here")
	}
	got := f.checkPath(system)
	if got.Outcome != outcomeRefused || got.Refusal == nil || got.Refusal.Row != "WS-53" || got.Refusal.Code != "system_directory" || got.Confirm != nil || !strings.Contains(got.Refusal.Message, "operating system") {
		t.Fatalf("%s: %+v %+v", system, got, got.Refusal)
	}
}

// TestWorkspaceCheckContains checks WS-54 (R8): a folder that holds
// registered entries may be added (a monorepo above its parts), and the
// check names every entry it contains whose folder is there; a folder
// holding none lists none.
func TestWorkspaceCheckContains(t *testing.T) {
	f := newWSFixture(t, sqliteOpen, nil)
	sc := t.TempDir()
	for _, d := range []string{"r1", "proj", "other"} {
		if err := os.Mkdir(filepath.Join(sc, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest(t, filepath.Join(sc, "proj"), "version: 1\nname: shop\nmembers:\n  - repository: api\n    path: api\n")
	r1 := f.register(workspace.KindRepository, "r1", filepath.Join(sc, "r1"))
	shop := f.register(workspace.KindProject, "shop", filepath.Join(sc, "proj"))
	got := f.checkPath(sc)
	if got.Outcome != outcomeReady || got.Confirm == nil || len(got.Contains) != 2 {
		t.Fatalf("parent of two entries: %+v", got)
	}
	ids := map[string]bool{got.Contains[0].ID: true, got.Contains[1].ID: true}
	if !ids[r1.ID] || !ids[shop.ID] || got.Contains[0].Path == "" || got.Contains[0].Name == "" {
		t.Fatalf("contains %+v", got.Contains)
	}
	if other := f.checkPath(filepath.Join(sc, "other")); other.Outcome != outcomeReady || other.Contains == nil || len(other.Contains) != 0 {
		t.Fatalf("a folder holding none: %+v", other)
	}
	// A registered folder that is gone is no part of what the index would
	// read: it is left out.
	if err := os.RemoveAll(filepath.Join(sc, "r1")); err != nil {
		t.Fatal(err)
	}
	got = f.checkPath(sc)
	if got.Outcome != outcomeReady || len(got.Contains) != 1 || got.Contains[0].ID != shop.ID {
		t.Fatalf("after r1 was removed from disk: %+v", got.Contains)
	}
}

// TestWorkspaceWriteGoneFolder checks WS-15 for an index that is already
// open (T038 re-check W2): once its folder is moved, a run and every other
// request that may write answer 409 folder_gone, and nothing is created at
// the old path, with a store in the folder (SQLite) or a shared one
// (Postgres). Reads of the open index still answer (R4).
func TestWorkspaceWriteGoneFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not move a folder whose files are open")
	}
	backends(t, func(t *testing.T, write, read store.Store) {
		var root string
		open := sqliteOpen
		if strings.HasSuffix(t.Name(), "/postgres") {
			root = filepath.Join(t.TempDir(), "r1")
			if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
				t.Fatal(err)
			}
			indexRepository(t, root, write, "r1")
			open = func(_ context.Context, e workspace.Entry, _ bool) (*WorkspaceIndex, error) {
				return &WorkspaceIndex{Entry: e, Options: Options{Root: e.Path, Repository: e.Name, Store: read,
					OpenWriter: func(context.Context) (store.Store, error) { return nil, errors.New("no writer in this test") }}}, nil
			}
		} else {
			root = indexedFolder(t, "r1")
		}
		f := newWSFixture(t, open, nil)
		e := f.register(workspace.KindRepository, "r1", root)
		ws := "?ws=" + url.QueryEscape(e.ID)
		f.json(http.MethodGet, "/api/v0/status"+ws, "", http.StatusOK, nil)
		if !f.isOpen(e.ID) {
			t.Fatal("the index did not open")
		}
		if err := os.Rename(root, root+".moved"); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{indexRunPath, changesPinsPath} {
			rec := f.do(http.MethodPost, target+ws, "{}", nil)
			if rec.Code != http.StatusConflict || errorCode(t, rec) != "folder_gone" {
				t.Errorf("POST %s of an open index whose folder is gone: %d %s", target, rec.Code, rec.Body.String())
			}
		}
		if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("a write request recreated the moved folder (%v)", err)
		}
		if doc := f.item(e.ID); doc.State != entryFolderGone || doc.Job != nil {
			t.Fatalf("after the refused run: state %s job %+v", doc.State, doc.Job)
		}
		f.json(http.MethodGet, "/api/v0/api/endpoints"+ws, "", http.StatusOK, nil)
	})
}

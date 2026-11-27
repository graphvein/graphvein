package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

// resolverOpen opens an entry's index where the store resolver finds it,
// as serve's openWorkspaceIndex does (SQLite only): the read store, the
// index lock and the writer of a re-index at that place.
func resolverOpen(stores **storeloc.Resolver) wsOpenFunc {
	return func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error) {
		loc, err := (*stores).Resolve(e.Path, storeloc.Scope{})
		if err != nil {
			return nil, err
		}
		if !loc.Exists && !create {
			return nil, ErrNoIndex
		}
		if create {
			if err := createTestStore(ctx, loc); err != nil {
				return nil, err
			}
		}
		ro, err := sqlite.OpenRead(ctx, loc.Path)
		if err != nil {
			return nil, err
		}
		locker := sqlite.IndexLocker(loc.Path)
		opt := Options{Root: e.Path, Repository: e.Name, Store: ro, Locker: locker, StoreDir: loc.Dir(),
			OpenWriter: func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, loc.Path) }}
		return &WorkspaceIndex{Entry: e, Options: opt, Close: func() {
			locker.Close()
			ro.Close()
		}}, nil
	}
}

// storeFixture is a workspace server whose entries open where its store
// resolver finds them.
func storeFixture(t *testing.T, setup func(*WorkspaceOptions)) (*wsFixture, *storeloc.Resolver) {
	t.Helper()
	var stores *storeloc.Resolver
	f := newWSFixture(t, resolverOpen(&stores), func(o *WorkspaceOptions) {
		stores = o.Stores
		if setup != nil {
			setup(o)
		}
	})
	return f, stores
}

// indexAt indexes root as repository name into the store at loc.
func indexAt(t *testing.T, loc storeloc.Location, root, name string) {
	t.Helper()
	if err := loc.Prepare(); err != nil {
		t.Fatal(err)
	}
	w, err := sqlite.Open(context.Background(), loc.Path)
	if err != nil {
		t.Fatal(err)
	}
	indexRepository(t, root, w, name)
	w.Close()
}

// checkStore posts a typed path with a store place to check.
func (f *wsFixture) checkStore(path string, extra map[string]string) checkResponse {
	f.t.Helper()
	m := map[string]string{"path": path}
	for k, v := range extra {
		m[k] = v
	}
	body, _ := json.Marshal(m)
	var out checkResponse
	f.json(http.MethodPost, workspaceCheckPath, string(body), http.StatusOK, &out)
	return out
}

// TestWorkspaceCheckStore checks WS-71 and WS-73: a ready check names
// where the index is or would be kept and the places a new one could get;
// a folder with an index keeps it; a folder with two indexes is refused.
func TestWorkspaceCheckStore(t *testing.T) {
	f, stores := storeFixture(t, nil)
	plain := fixtureRoot(t)
	got := f.checkPath(plain)
	key := storeloc.Key(*got.Resolved)
	wantDefault := filepath.Join(stores.DataDir, "graphvein", "stores", key, storeloc.StoreFile)
	if got.Outcome != outcomeReady || got.Store == nil || *got.Store.Path != wantDefault || *got.Store.Origin != "default" || got.Store.Exists || got.Store.Size != nil || *got.Store.DirSource != "platform" {
		t.Fatalf("new folder store %+v", got.Store)
	}
	p := got.Places
	if p == nil || !p.Choosable || p.Chosen != nil || *p.Default != wantDefault || *p.Inside != filepath.Join(*got.Resolved, ".graphvein", "index.db") || p.Folder != nil {
		t.Fatalf("places %+v", p)
	}

	// A folder with a v0.1.1 store inside it: kept there; no choice.
	legacy := fixtureRoot(t)
	inside := storeloc.Location{Folder: legacy, Origin: storeloc.OriginInFolder, Path: filepath.Join(legacy, ".graphvein", "index.db")}
	indexAt(t, inside, legacy, filepath.Base(legacy))
	got = f.checkPath(legacy)
	if got.Outcome != outcomeReady || *got.Store.Origin != "in_folder" || *got.Store.Place != placeInside || !got.Store.Exists || got.Store.Size == nil || *got.Store.Size == 0 {
		t.Fatalf("legacy store %+v", got.Store)
	}
	if got.Places.Choosable || got.Places.Reason == nil || got.Index.State != indexPresent {
		t.Fatalf("legacy places %+v index %+v", got.Places, got.Index)
	}

	// WS-71: a second index in the stores folder: both are refused.
	loc := storeloc.Location{Folder: legacy, Origin: storeloc.OriginDefault, Path: filepath.Join(stores.DataDir, "graphvein", "stores", storeloc.Key(*got.Resolved), storeloc.StoreFile)}
	indexAt(t, loc, legacy, "legacy")
	got = f.checkPath(legacy)
	if got.Outcome != outcomeRefused || got.Refusal.Row != "WS-71" || got.Refusal.Code != "two_indexes" || got.Confirm != nil {
		t.Fatalf("two indexes %+v", got.Refusal)
	}
	// A registered folder reports its entry's store.
	e := f.register(workspace.KindRepository, "plain", plain)
	if got := f.checkPath(plain); got.Outcome != outcomeRegistered || got.Store == nil || *got.Store.Path != wantDefault || *got.Entry != e.ID {
		t.Fatalf("registered %+v", got)
	}
	// The golden pins the shape of a ready answer's store and places.
	ready := f.checkPath(fixtureRoot(t))
	checkStoreGolden(t, "workspace-check-store", map[string]any{"store": ready.Store, "places": ready.Places}, map[string]string{stores.DataDir: "<data>", *ready.Resolved: "<folder>", storeloc.Key(*ready.Resolved): "<name>-<h8>"})
}

// checkStoreGolden compares v with a golden after replacing the temporary
// paths it holds, longest first, with placeholders.
func checkStoreGolden(t *testing.T, name string, v any, paths map[string]string) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for {
		longest := ""
		for p := range paths {
			if strings.Contains(text, jsonText(p)) && len(p) > len(longest) {
				longest = p
			}
		}
		if longest == "" {
			break
		}
		text = strings.ReplaceAll(text, jsonText(longest), paths[longest])
	}
	text = strings.ReplaceAll(text, `\\`, "/")
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	// Entry ids hash the temporary path, and a store's size is SQLite's:
	// both are kept out of the golden.
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			for k, item := range x {
				switch {
				case k == "id":
					x[k] = "<id>"
				case k == "size" && item != nil:
					x[k] = "<bytes>"
				default:
					walk(item)
				}
			}
		}
	}
	walk(out)
	golden.Check(t, filepath.Join("testdata", name+".golden.json"), out)
}

// jsonText is p as it appears inside a JSON string.
func jsonText(p string) string {
	b, _ := json.Marshal(p)
	return string(b[1 : len(b)-1])
}

// TestWorkspaceCheckStoreChoice checks WS-74 and WS-75: the place a check
// asks for, and every refusal of a typed store folder.
func TestWorkspaceCheckStoreChoice(t *testing.T) {
	f, stores := storeFixture(t, nil)
	folder := fixtureRoot(t)
	resolved, _ := workspace.Resolve(folder)
	key := storeloc.Key(resolved)
	for name, body := range map[string]string{
		"unknown place":  `{"path":` + quoteJSON(folder) + `,"store_place":"cloud"}`,
		"dir alone":      `{"path":` + quoteJSON(folder) + `,"store_dir":"/x"}`,
		"folder no dir":  `{"path":` + quoteJSON(folder) + `,"store_place":"folder"}`,
		"inside and dir": `{"path":` + quoteJSON(folder) + `,"store_place":"inside","store_dir":"/x"}`,
	} {
		if rec := f.do(http.MethodPost, workspaceCheckPath, body, nil); rec.Code != 400 || errorCode(t, rec) != "bad_request" {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	got := f.checkStore(folder, map[string]string{"store_place": "inside"})
	if got.Outcome != outcomeReady || *got.Store.Path != filepath.Join(resolved, ".graphvein", "index.db") || *got.Store.Place != placeInside || *got.Places.Chosen != "inside" {
		t.Fatalf("inside %+v", got.Store)
	}
	dest := t.TempDir()
	got = f.checkStore(folder, map[string]string{"store_place": "folder", "store_dir": dest})
	destResolved, _ := workspace.Resolve(dest)
	want := filepath.Join(destResolved, key, storeloc.StoreFile)
	if got.Outcome != outcomeReady || *got.Store.Path != want || *got.Store.Origin != "stores_file" || *got.Places.Folder != want || got.Confirm == nil {
		t.Fatalf("folder %+v %+v", got.Store, got.Places)
	}
	got = f.checkStore(folder, map[string]string{"store_place": "default"})
	if got.Outcome != outcomeReady || *got.Store.Origin != "default" {
		t.Fatalf("default %+v", got.Store)
	}

	// WS-75: the typed folder's refusals.
	other := t.TempDir()
	f.register(workspace.KindRepository, "other", other)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	taken := t.TempDir()
	if err := os.MkdirAll(filepath.Join(taken, key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taken, key, storeloc.StoreFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := t.TempDir()
	if err := os.MkdirAll(filepath.Join(foreign, key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreign, key, storeloc.FolderFile), []byte("/somewhere/else\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.VolumeName(dest) + string(filepath.Separator)
	for dir, code := range map[string]string{
		"relative/dir":                   "not_absolute",
		root:                             "filesystem_root",
		filepath.Join(dest, ".git", "x"): "inside_git",
		filepath.Join(folder, "keep"):    "inside_folder",
		filepath.Join(other, "nested"):   "inside_registered",
		file:                             "not_a_directory",
		taken:                            "index_exists",
		foreign:                          "foreign_store",
	} {
		got := f.checkStore(folder, map[string]string{"store_place": "folder", "store_dir": dir})
		if got.Outcome != outcomeRefused || got.Refusal.Row != "WS-75" || got.Refusal.Code != code || got.Confirm != nil {
			t.Errorf("%s: %+v, want %s", dir, got.Refusal, code)
		}
	}
	// WS-74: a folder that has an index keeps it.
	loc, err := stores.Resolve(folder, storeloc.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	indexAt(t, loc, folder, "fixture")
	got = f.checkStore(folder, map[string]string{"store_place": "inside"})
	if got.Outcome != outcomeRefused || got.Refusal.Row != "WS-74" || got.Refusal.Code != "store_fixed" {
		t.Fatalf("existing index %+v", got.Refusal)
	}
	if got := f.checkStore(folder, map[string]string{"store_place": "default"}); got.Outcome != outcomeReady {
		t.Fatalf("its own place %+v", got.Refusal)
	}
	// Without CreateStore the inside place is not offered.
	g, _ := storeFixture(t, func(o *WorkspaceOptions) { o.CreateStore = nil })
	if got := g.checkStore(fixtureRoot(t), map[string]string{"store_place": "inside"}); got.Outcome != outcomeRefused || got.Refusal.Code != "place_unavailable" {
		t.Fatalf("no CreateStore %+v", got.Refusal)
	}
}

// TestWorkspaceAddStorePlace checks WS-76: the confirm value binds the
// folder and the store the check showed; the add records the chosen place
// before the run, and refuses when the store would now go elsewhere.
func TestWorkspaceAddStorePlace(t *testing.T) {
	f, stores := storeFixture(t, nil)
	add := func(c checkResponse, index bool) addResponse {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"path": *c.Resolved, "confirm": *c.Confirm, "index": index})
		var out addResponse
		want := http.StatusOK
		if index {
			want = http.StatusAccepted
		}
		f.json(http.MethodPost, workspaceAddPath, string(body), want, &out)
		return out
	}
	// A typed folder: recorded in stores.yaml, the run writes there.
	folder := fixtureRoot(t)
	dest := t.TempDir()
	c := f.checkStore(folder, map[string]string{"store_place": "folder", "store_dir": dest})
	out := add(c, true)
	waitJob(t, f, out.Entry.ID, jobCommitted)
	loc, err := stores.Resolve(folder, storeloc.Scope{})
	if err != nil || loc.Origin != storeloc.OriginMapped || loc.Path != *c.Store.Path || !loc.Exists {
		t.Fatalf("after add %+v %v", loc, err)
	}
	// Inside the folder without a run: the empty store records the place.
	in := fixtureRoot(t)
	c = f.checkStore(in, map[string]string{"store_place": "inside"})
	add(c, false)
	if loc, err := stores.Resolve(in, storeloc.Scope{}); err != nil || loc.Origin != storeloc.OriginInFolder || !loc.Exists {
		t.Fatalf("inside %+v %v", loc, err)
	}
	// The store changed place since the check: refused, nothing written.
	late := fixtureRoot(t)
	c = f.checkPath(late)
	if err := stores.SetDefaultDir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"path": *c.Resolved, "confirm": *c.Confirm})
	if rec := f.do(http.MethodPost, workspaceAddPath, string(body), nil); rec.Code != 409 || errorCode(t, rec) != "path_changed" {
		t.Fatalf("moved default: %d %s", rec.Code, rec.Body.String())
	}
	if entries, _ := f.reg.Entries(); len(entries) != 2 {
		t.Fatalf("registered %d entries", len(entries))
	}
	// A confirm value of a move is not an add's.
	value, _ := f.ws.issueConfirm(confirmRecord{op: opMove, path: *c.Resolved})
	body, _ = json.Marshal(map[string]any{"path": *c.Resolved, "confirm": value})
	if rec := f.do(http.MethodPost, workspaceAddPath, string(body), nil); rec.Code != 409 || errorCode(t, rec) != "confirm_required" {
		t.Fatalf("move's confirm: %d %s", rec.Code, rec.Body.String())
	}
}

// storeSettings reads GET /api/v0/store.
func (f *wsFixture) storeSettings() storeSettingsResponse {
	f.t.Helper()
	var out storeSettingsResponse
	f.json(http.MethodGet, storePath, "", http.StatusOK, &out)
	return out
}

// TestStoreSettings checks WS-77: Settings' Storage section without any
// index, and one row per entry with its store and whether it can move.
func TestStoreSettings(t *testing.T) {
	f, stores := storeFixture(t, nil)
	empty := f.storeSettings()
	platform := filepath.Join(stores.DataDir, "graphvein", "stores")
	if empty.TeamMode.Set || empty.SQLitePath.Set || *empty.DefaultDir.Path != platform || *empty.DefaultDir.Source != "platform" || !empty.DefaultDir.Editable || len(empty.Entries) != 0 || *empty.Registry.Path != f.reg.Path() || *empty.StoresFile.Path != stores.StoresFile {
		t.Fatalf("no index %+v", empty)
	}
	if _, err := os.Stat(platform); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the GET created the stores folder")
	}
	legacy := fixtureRoot(t)
	indexAt(t, storeloc.Location{Folder: legacy, Origin: storeloc.OriginInFolder, Path: filepath.Join(legacy, ".graphvein", "index.db")}, legacy, "legacy")
	fresh := fixtureRoot(t)
	gone := t.TempDir()
	el := f.register(workspace.KindRepository, "legacy", legacy)
	ef := f.register(workspace.KindRepository, "fresh", fresh)
	eg := f.register(workspace.KindRepository, "gone", gone)
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	got := f.storeSettings()
	rows := map[string]entryDocs{}
	for _, e := range got.Entries {
		rows[e.ID] = e
	}
	if r := rows[el.ID]; !r.Movable || *r.Store.Place != placeInside || !r.Store.Exists || r.Store.Size == nil || !r.FolderFound {
		t.Fatalf("legacy %+v %+v", r, r.Store)
	}
	if r := rows[ef.ID]; r.Movable || r.Reason == nil || !strings.Contains(*r.Reason, "no index yet") || r.Store.Exists {
		t.Fatalf("fresh %+v", r)
	}
	if r := rows[eg.ID]; r.Movable || r.FolderFound || r.Store != nil {
		t.Fatalf("gone %+v", r)
	}
	if _, err := os.Stat(platform); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the GET created the stores folder")
	}
	checkStoreGolden(t, "store-settings", got, map[string]string{stores.DataDir: "<data>", filepath.Dir(f.reg.Path()): "<config>", el.Path: "<legacy>", ef.Path: "<fresh>", eg.Path: "<gone>", storeloc.Key(ef.Path): "<name>-<h8>"})
}

// TestStoreMoveCheck checks WS-78: a move is checked for an entry with an
// index, and the confirm value binds the entry and both index files.
func TestStoreMoveCheck(t *testing.T) {
	f, stores := storeFixture(t, nil)
	check := func(body string) storeCheckResponse {
		t.Helper()
		var out storeCheckResponse
		f.json(http.MethodPost, storeCheckPath, body, http.StatusOK, &out)
		return out
	}
	if rec := f.do(http.MethodPost, storeCheckPath, `{"id":"nope~00000000","dest_dir":""}`, nil); rec.Code != 404 || errorCode(t, rec) != "unknown_entry" {
		t.Fatalf("unknown entry: %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.do(http.MethodPost, storeCheckPath, `{"id":"x","default_dir":""}`, nil); rec.Code != 400 {
		t.Fatalf("both: %d", rec.Code)
	}
	folder := fixtureRoot(t)
	e := f.register(workspace.KindRepository, "fixture", folder)
	if got := check(`{"id":` + quoteJSON(e.ID) + `,"dest_dir":""}`); got.Outcome != outcomeRefused || got.Refusal.Row != "WS-78" || got.Refusal.Code != "no_index" || got.Confirm != nil {
		t.Fatalf("no index %+v", got.Refusal)
	}
	inFolder := filepath.Join(e.Path, ".graphvein", "index.db")
	indexAt(t, storeloc.Location{Folder: e.Path, Origin: storeloc.OriginInFolder, Path: inFolder}, folder, "fixture")
	got := check(`{"id":` + quoteJSON(e.ID) + `,"dest_dir":""}`)
	want := filepath.Join(stores.DataDir, "graphvein", "stores", storeloc.Key(e.Path), storeloc.StoreFile)
	if got.Outcome != outcomeReady || got.Move.From != inFolder || got.Move.To != want || !got.Move.ToDefault || got.Move.FromPlace != placeInside || got.Move.Size == 0 || got.Confirm == nil || !strings.HasPrefix(*got.Command, "graphvein store move '") {
		t.Fatalf("ready %+v %+v", got.Move, got.Refusal)
	}
	for dir, code := range map[string]string{"rel": "not_absolute", filepath.Join(e.Path, "x"): "inside_folder"} {
		got := check(`{"id":` + quoteJSON(e.ID) + `,"dest_dir":` + quoteJSON(dir) + `}`)
		if got.Outcome != outcomeRefused || got.Refusal.Row != "WS-75" || got.Refusal.Code != code {
			t.Errorf("%s: %+v", dir, got.Refusal)
		}
	}
}

// moveBody is a POST /api/v0/store/move body.
func moveBody(id, dest, confirm string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "dest_dir": dest, "confirm": confirm})
	return string(b)
}

// TestStoreMove checks WS-79: the move closes the open index, copies,
// verifies and deletes the old files, and the next request opens the
// index at its new place; a used or foreign confirm value, a changed
// plan, a held index lock and a running move are refused.
func TestStoreMove(t *testing.T) {
	f, stores := storeFixture(t, nil)
	folder := fixtureRoot(t)
	e := f.register(workspace.KindRepository, "fixture", folder)
	inFolder := filepath.Join(e.Path, ".graphvein", "index.db")
	indexAt(t, storeloc.Location{Folder: e.Path, Origin: storeloc.OriginInFolder, Path: inFolder}, folder, "fixture")
	status := "/api/v0/status?ws=" + url.QueryEscape(e.ID)
	f.json(http.MethodGet, status, "", http.StatusOK, nil)
	if !f.isOpen(e.ID) {
		t.Fatal("not open")
	}
	checkMove := func(dest string) storeCheckResponse {
		t.Helper()
		var out storeCheckResponse
		f.json(http.MethodPost, storeCheckPath, `{"id":`+quoteJSON(e.ID)+`,"dest_dir":`+quoteJSON(dest)+`}`, http.StatusOK, &out)
		if out.Outcome != outcomeReady {
			t.Fatalf("check %+v", out.Refusal)
		}
		return out
	}
	// A held index lock (another process's run): 409 index_locked after
	// the wait, the original kept.
	c := checkMove("")
	holder := sqlite.IndexLocker(inFolder)
	lease, err := holder.Acquire(context.Background(), indexrun.LockWait)
	if err != nil {
		t.Fatal(err)
	}
	if rec := f.do(http.MethodPost, storeMovePath, moveBody(e.ID, "", *c.Confirm), nil); rec.Code != 409 || errorCode(t, rec) != "index_locked" {
		t.Fatalf("locked: %d %s", rec.Code, rec.Body.String())
	}
	lease.Release()
	holder.Close()
	if _, err := os.Stat(inFolder); err != nil {
		t.Fatal("original lost after a refused move")
	}
	// The confirm value was used up; one for another destination is
	// refused.
	if rec := f.do(http.MethodPost, storeMovePath, moveBody(e.ID, "", *c.Confirm), nil); rec.Code != 409 || errorCode(t, rec) != "confirm_required" {
		t.Fatalf("reused: %d %s", rec.Code, rec.Body.String())
	}
	c = checkMove("")
	if rec := f.do(http.MethodPost, storeMovePath, moveBody(e.ID, t.TempDir(), *c.Confirm), nil); rec.Code != 409 || errorCode(t, rec) != "confirm_required" {
		t.Fatalf("other destination: %d %s", rec.Code, rec.Body.String())
	}
	// A plan that changed since the check: refused.
	dest := t.TempDir()
	c = checkMove(dest)
	if err := os.MkdirAll(filepath.Join(dest, storeloc.Key(e.Path)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, storeloc.Key(e.Path), storeloc.StoreFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(http.MethodPost, storeMovePath, moveBody(e.ID, dest, *c.Confirm), nil); rec.Code != 409 || errorCode(t, rec) != "path_changed" {
		t.Fatalf("changed: %d %s", rec.Code, rec.Body.String())
	}
	// While a move runs, requests for the entry are refused.
	f.ws.mu.Lock()
	f.ws.moving[e.ID] = true
	f.ws.mu.Unlock()
	if rec := f.do(http.MethodGet, status, "", nil); rec.Code != 409 || errorCode(t, rec) != "store_moving" {
		t.Fatalf("during a move: %d %s", rec.Code, rec.Body.String())
	}
	f.ws.mu.Lock()
	delete(f.ws.moving, e.ID)
	f.ws.mu.Unlock()

	// The move.
	c = checkMove("")
	var out storeMoveResponse
	f.json(http.MethodPost, storeMovePath, moveBody(e.ID, "", *c.Confirm), http.StatusOK, &out)
	want := filepath.Join(stores.DataDir, "graphvein", "stores", storeloc.Key(e.Path), storeloc.StoreFile)
	if out.From != inFolder || out.To != want || out.Runs != 1 || out.Bytes == 0 || len(out.Remaining) != 0 || out.Warning != nil || out.Recorded {
		t.Fatalf("moved %+v", out)
	}
	if f.isOpen(e.ID) {
		t.Fatal("the index stayed open across the move")
	}
	if _, err := os.Stat(filepath.Join(e.Path, ".graphvein")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old store folder: %v", err)
	}
	var st statusResponse
	f.json(http.MethodGet, status, "", http.StatusOK, &st)
	if !st.Index.Indexed {
		t.Fatalf("after the move %+v", st.Index)
	}
	if row := f.storeSettings().Entries[0]; *row.Store.Path != want || *row.Store.Origin != "default" {
		t.Fatalf("settings after the move %+v", row.Store)
	}
}

// TestStoreMoveCommandLine checks WS-79 for the index serve was started
// on: its store closes through WorkspaceIndex.Close; without Close it is
// not movable.
func TestStoreMoveCommandLine(t *testing.T) {
	folder := fixtureRoot(t)
	resolved, _ := workspace.Resolve(folder)
	inFolder := filepath.Join(resolved, ".graphvein", "index.db")
	indexAt(t, storeloc.Location{Folder: resolved, Origin: storeloc.OriginInFolder, Path: inFolder}, folder, "fixture")
	var stores *storeloc.Resolver
	closed := 0
	f := newWSFixture(t, resolverOpen(&stores), func(o *WorkspaceOptions) {
		stores = o.Stores
		wi, err := resolverOpen(&stores)(context.Background(), workspace.Entry{Kind: workspace.KindRepository, Name: "fixture", Path: resolved}, false)
		if err != nil {
			t.Fatal(err)
		}
		inner := wi.Close
		wi.Close = func() { closed++; inner() }
		wi.Entry = workspace.Entry{Kind: workspace.KindRepository, Name: "fixture", Path: resolved, ID: workspace.EntryID(workspace.KindRepository, resolved, "fixture")}
		o.Current = wi
	})
	id := f.ws.ws.Current.Entry.ID
	var c storeCheckResponse
	f.json(http.MethodPost, storeCheckPath, `{"id":`+quoteJSON(id)+`,"dest_dir":""}`, http.StatusOK, &c)
	if c.Outcome != outcomeReady {
		t.Fatalf("check %+v", c.Refusal)
	}
	f.json(http.MethodPost, storeMovePath, moveBody(id, "", *c.Confirm), http.StatusOK, nil)
	if closed != 1 {
		t.Fatalf("command-line index closed %d times", closed)
	}
	var st statusResponse
	f.json(http.MethodGet, "/api/v0/status", "", http.StatusOK, &st)
	if !st.Index.Indexed {
		t.Fatalf("after the move %+v", st.Index)
	}
	// Without Close the command-line index is not movable.
	f.ws.ws.Current.Close = nil
	f.json(http.MethodPost, storeCheckPath, `{"id":`+quoteJSON(id)+`,"dest_dir":""}`, http.StatusOK, &c)
	if c.Outcome != outcomeRefused || c.Refusal.Code != "same_place" && c.Refusal.Code != "entry_in_use" {
		t.Fatalf("without Close %+v", c.Refusal)
	}
}

// TestStoreDefaultDir checks WS-80: the folder for new indexes is checked
// and changed with a confirm value; indexes that exist stay where they
// are; GRAPHVEIN_STORE_DIR decides when it is set.
func TestStoreDefaultDir(t *testing.T) {
	f, stores := storeFixture(t, nil)
	folder := fixtureRoot(t)
	e := f.register(workspace.KindRepository, "fixture", folder)
	old, err := stores.Resolve(folder, storeloc.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	indexAt(t, old, folder, "fixture")
	check := func(dir string) storeCheckResponse {
		t.Helper()
		var out storeCheckResponse
		f.json(http.MethodPost, storeCheckPath, `{"default_dir":`+quoteJSON(dir)+`}`, http.StatusOK, &out)
		return out
	}
	platform := filepath.Join(stores.DataDir, "graphvein", "stores")
	for dir, want := range map[string][2]string{
		"rel":                         {"WS-75", "not_absolute"},
		filepath.Join(e.Path, "keep"): {"WS-75", "inside_registered"},
		"":                            {"WS-80", "unchanged"},
		platform:                      {"WS-80", "unchanged"},
	} {
		if got := check(dir); got.Outcome != outcomeRefused || got.Refusal.Row != want[0] || got.Refusal.Code != want[1] {
			t.Errorf("%q: %+v", dir, got.Refusal)
		}
	}
	dest := t.TempDir()
	got := check(dest)
	destResolved, _ := workspace.Resolve(dest)
	if got.Outcome != outcomeReady || got.DefaultDir.To != destResolved || *got.DefaultDir.From != platform || got.Confirm == nil {
		t.Fatalf("ready %+v %+v", got.DefaultDir, got.Refusal)
	}
	if rec := f.do(http.MethodPost, storeDefaultPath, `{"default_dir":"/other","confirm":`+quoteJSON(*got.Confirm)+`}`, nil); rec.Code != 409 || errorCode(t, rec) != "confirm_required" {
		t.Fatalf("other dir: %d %s", rec.Code, rec.Body.String())
	}
	got = check(dest)
	f.json(http.MethodPost, storeDefaultPath, `{"default_dir":`+quoteJSON(dest)+`,"confirm":`+quoteJSON(*got.Confirm)+`}`, http.StatusOK, nil)
	s := f.storeSettings()
	if *s.DefaultDir.Path != destResolved || *s.DefaultDir.Source != "stores_file" {
		t.Fatalf("after %+v", s.DefaultDir)
	}
	// The existing index stays where it is.
	if loc, err := stores.Resolve(folder, storeloc.Scope{}); err != nil || loc.Path != old.Path || !loc.Exists {
		t.Fatalf("existing index %+v %v", loc, err)
	}
	// Back to the platform's folder.
	got = check("")
	if got.Outcome != outcomeReady || !got.DefaultDir.ToPlatform || got.DefaultDir.To != platform {
		t.Fatalf("platform %+v", got.DefaultDir)
	}
	f.json(http.MethodPost, storeDefaultPath, `{"default_dir":"","confirm":`+quoteJSON(*got.Confirm)+`}`, http.StatusOK, nil)
	if s := f.storeSettings(); *s.DefaultDir.Source != "platform" {
		t.Fatalf("restored %+v", s.DefaultDir)
	}
	// GRAPHVEIN_STORE_DIR decides: not editable.
	env := t.TempDir()
	stores.Getenv = func(k string) string {
		if k == storeloc.EnvStoreDir {
			return env
		}
		return ""
	}
	t.Setenv(storeloc.EnvStoreDir, env)
	if got := check(t.TempDir()); got.Outcome != outcomeRefused || got.Refusal.Code != "set_by_environment" {
		t.Fatalf("env %+v", got.Refusal)
	}
	if s := f.storeSettings(); s.DefaultDir.Editable || *s.DefaultDir.Source != "env" {
		t.Fatalf("env settings %+v", s.DefaultDir)
	}
}

// TestWorkspaceReindexAfterMove checks WS-81: a re-index whose lock
// reports the store moved (storeloc.ErrMoved) is refused with
// store_moved, and nothing is created.
func TestWorkspaceReindexAfterMove(t *testing.T) {
	folder := fixtureRoot(t)
	f := newWSFixture(t, func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error) {
		wi, err := sqliteOpen(ctx, e, true)
		if err != nil {
			return nil, err
		}
		wi.Options.Locker = movedLocker{wi.Options.Locker}
		return wi, nil
	}, nil)
	e := f.register(workspace.KindRepository, "fixture", folder)
	rec := f.do(http.MethodPost, "/api/v0/index/run?ws="+url.QueryEscape(e.ID), "{}", nil)
	if rec.Code != 409 || errorCode(t, rec) != "store_moved" {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}
}

// movedLocker is an index lock whose store moved away meanwhile.
type movedLocker struct{ store.IndexLocker }

func (movedLocker) Acquire(context.Context, time.Duration) (store.IndexLease, error) {
	return nil, errors.Join(storeloc.ErrMoved, errors.New("the index of x is now at y"))
}

// TestStoreTeamMode checks WS-77, WS-73 and WS-78 in team mode
// (GRAPHVEIN_POSTGRES_URL set, here through the resolver's environment):
// every store is the database, named by its variable only; nothing can be
// placed, moved or given another default folder. No store is opened.
func TestStoreTeamMode(t *testing.T) {
	const url = "postgres://user:secret@db.example/graphvein"
	f, stores := storeFixture(t, nil)
	stores.Getenv = func(k string) string {
		if k == storeloc.EnvPostgres {
			return url
		}
		return ""
	}
	folder := fixtureRoot(t)
	e := f.register(workspace.KindRepository, "fixture", folder)
	rec := f.do(http.MethodGet, storePath, "", nil)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}
	var s storeSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	row := s.Entries[0]
	if !s.TeamMode.Set || s.DefaultDir.Editable || row.Movable || row.Store.Kind != "postgres" || row.Store.Path != nil || *row.Store.Env != storeloc.EnvPostgres || *row.Store.Place != placePostgres {
		t.Fatalf("team mode %+v %+v", s, row.Store)
	}
	var c storeCheckResponse
	f.json(http.MethodPost, storeCheckPath, `{"id":`+quoteJSON(e.ID)+`,"dest_dir":""}`, http.StatusOK, &c)
	if c.Outcome != outcomeRefused || c.Refusal.Code != "team_mode" {
		t.Fatalf("move %+v", c.Refusal)
	}
	f.json(http.MethodPost, storeCheckPath, `{"default_dir":`+quoteJSON(t.TempDir())+`}`, http.StatusOK, &c)
	if c.Outcome != outcomeRefused || c.Refusal.Code != "team_mode" {
		t.Fatalf("default %+v", c.Refusal)
	}
	other := fixtureRoot(t)
	got := f.checkPath(other)
	if got.Store == nil || got.Store.Kind != "postgres" || got.Places.Choosable {
		t.Fatalf("check %+v %+v", got.Store, got.Places)
	}
	if got := f.checkStore(other, map[string]string{"store_place": "inside"}); got.Outcome != outcomeRefused || got.Refusal.Code != "team_mode" {
		t.Fatalf("place %+v", got.Refusal)
	}
}

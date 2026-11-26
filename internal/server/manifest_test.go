package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// editManifest is the projectimpact fixture's manifest with comments, so
// the tests see the writer keep them through a server edit.
const editManifest = `# users project (T022 B10 test)
version: 1
name: users
members:
  # the web client
  - repository: frontend # frontend line
    path: frontend
  - repository: backend
    path: backend
`

// manifestServer is one handler (one session token) over the fixture.
type manifestServer struct {
	t     *testing.T
	h     http.Handler
	stop  func()
	root  string
	token string
	hash  string
}

func newManifestServer(t *testing.T, s store.Store) *manifestServer {
	t.Helper()
	return newManifestServerWith(t, s, nil)
}

// newManifestServerWith is newManifestServer with options changed by
// tweak (nil: none).
func newManifestServerWith(t *testing.T, s store.Store, tweak func(*Options)) *manifestServer {
	t.Helper()
	root := copyProjectFixture(t)
	if err := os.WriteFile(filepath.Join(root, config.ProjectFile), []byte(editManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	project, err := config.LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	opt := projectOptions(project, s)
	if tweak != nil {
		tweak(&opt)
	}
	h, runs, refs := newServerHandler(opt, testHosts)
	ms := &manifestServer{t: t, h: h, root: root, stop: func() { runs.stop(); refs.shutdown() }}
	t.Cleanup(ms.stop)
	ms.refresh()
	return ms
}

// do sends one request from a loopback peer with the page's headers;
// header entries override them (an empty value removes one).
func (ms *manifestServer) do(method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	ms.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = testHost
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Origin", "http://"+testHost)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(tokenHeader, ms.token)
	}
	for k, v := range header {
		switch {
		case k == "RemoteAddr":
			req.RemoteAddr = v
		case v == "":
			req.Header.Del(k)
		default:
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	ms.h.ServeHTTP(rec, req)
	return rec
}

// refresh reads the manifest document as the page does.
func (ms *manifestServer) refresh() manifestResponse {
	ms.t.Helper()
	rec := ms.do(http.MethodGet, projectManifestPath, "", nil)
	var doc manifestResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &doc) != nil {
		ms.t.Fatalf("GET manifest: %d %s", rec.Code, rec.Body)
	}
	ms.token, ms.hash = doc.Token, doc.Hash
	return doc
}

// edit posts operations (a JSON array) as a preview, or applied with the
// preview's result hash.
func (ms *manifestServer) edit(ops string, apply bool, resultHash string, header map[string]string) *httptest.ResponseRecorder {
	ms.t.Helper()
	body := `{"base_hash":"` + ms.hash + `","operations":` + ops
	if apply {
		body += `,"apply":true,"result_hash":"` + resultHash + `"`
	}
	return ms.do(http.MethodPost, projectManifestPath, body+"}", header)
}

func (ms *manifestServer) manifest() string {
	ms.t.Helper()
	data, err := os.ReadFile(filepath.Join(ms.root, config.ProjectFile))
	if err != nil {
		ms.t.Fatal(err)
	}
	return string(data)
}

// TestManifestEditSecurity is T022 B10's security gate (ADR 0075 "Manifest
// editing from the UI"): an edit is refused without the session token,
// with a wrong one, from a foreign Origin or site, without the JSON
// content type, from a non-loopback peer, with a field naming a file, with
// a scope folder outside the member (traversal, absolute, backslash,
// symbolic link), with a stale base hash or a moved preview, and on a
// server that is not serving a project. Every refusal leaves the file as
// it was.
func TestManifestEditSecurity(t *testing.T) {
	backends(t, func(t *testing.T, _, s store.Store) {
		ms := newManifestServer(t, s)
		addScope := func(path string) string {
			return `[{"op":"add_scope","member":"backend","name":"api","path":` + quoteJSON(path) + `}]`
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(ms.root, "backend", "outside")); err != nil {
			t.Fatal(err)
		}
		ok := ms.edit(addScope("src"), false, "", nil)
		if ok.Code != http.StatusOK {
			t.Fatalf("valid preview: %d %s", ok.Code, ok.Body)
		}
		var preview manifestEditResponse
		json.Unmarshal(ok.Body.Bytes(), &preview)
		cases := []struct {
			name, ops string
			header    map[string]string
			status    int
			code      string
		}{
			{"no token", addScope("src"), map[string]string{tokenHeader: ""}, 403, "token_required"},
			{"wrong token", addScope("src"), map[string]string{tokenHeader: "guess"}, 403, "token_required"},
			{"foreign origin", addScope("src"), map[string]string{"Origin": "http://evil.example"}, 403, "cross_site_request"},
			{"cross-site fetch", addScope("src"), map[string]string{"Sec-Fetch-Site": "cross-site"}, 403, "cross_site_request"},
			{"same-site fetch", addScope("src"), map[string]string{"Sec-Fetch-Site": "same-site"}, 403, "cross_site_request"},
			{"foreign host", addScope("src"), map[string]string{"Host": "evil.example:7420"}, 403, "host_not_allowed"},
			{"form content type", addScope("src"), map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type"},
			{"remote peer", addScope("src"), map[string]string{"RemoteAddr": "192.0.2.1:4000"}, 403, "not_loopback"},
			{"file field", `[{"op":"add_scope","member":"backend","name":"api","path":"src","file":"/etc/passwd"}]`, nil, 400, "bad_request"},
			{"manifest field", `[{"op":"rename_repository","member":"backend","to":"api","manifest":"../other.yaml"}]`, nil, 400, "bad_request"},
			{"unknown op", `[{"op":"add_member","path":"/tmp/x"}]`, nil, 400, "bad_request"},
			{"path on another op", `[{"op":"remove_scope","member":"backend","name":"api","path":"src"}]`, nil, 400, "bad_request"},
			{"traversal", addScope("../frontend"), nil, 400, "path_refused"},
			{"inner traversal", addScope("src/../../frontend"), nil, 400, "path_refused"},
			{"absolute", addScope("/etc"), nil, 400, "path_refused"},
			{"backslash", addScope(`src\db`), nil, 400, "path_refused"},
			{"drive", addScope("C:/Windows"), nil, 400, "path_refused"},
			{"dot", addScope("."), nil, 400, "path_refused"},
			{"symlink out", addScope("outside"), nil, 400, "path_refused"},
			{"missing folder", addScope("nowhere"), nil, 400, "path_refused"},
			{"a file", addScope("package.json"), nil, 400, "path_refused"},
		}
		before := ms.manifest()
		for _, c := range cases {
			if c.header != nil && c.header["Host"] != "" {
				// Host is the request's own field.
				req := httptest.NewRequest(http.MethodPost, projectManifestPath, strings.NewReader("{}"))
				req.Host = c.header["Host"]
				rec := httptest.NewRecorder()
				ms.h.ServeHTTP(rec, req)
				if rec.Code != c.status || errorCode(t, rec) != c.code {
					t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body)
				}
				continue
			}
			rec := ms.edit(c.ops, false, "", c.header)
			if rec.Code != c.status || errorCode(t, rec) != c.code {
				t.Errorf("%s: %d %s, want %d %s", c.name, rec.Code, rec.Body, c.status, c.code)
			}
			// The same request applied is refused the same way.
			if rec := ms.edit(c.ops, true, preview.ResultHash, c.header); rec.Code != c.status {
				t.Errorf("%s applied: %d %s", c.name, rec.Code, rec.Body)
			}
		}
		if ms.manifest() != before {
			t.Fatal("a refused edit changed the manifest")
		}

		// The file changed on disk since the edit was prepared.
		changed := before + "# edited elsewhere\n"
		os.WriteFile(filepath.Join(ms.root, config.ProjectFile), []byte(changed), 0o644)
		if rec := ms.edit(addScope("src"), true, preview.ResultHash, nil); rec.Code != 409 || errorCode(t, rec) != "manifest_changed" {
			t.Errorf("stale base hash: %d %s", rec.Code, rec.Body)
		}
		if ms.manifest() != changed {
			t.Error("a stale edit wrote the manifest")
		}
		ms.refresh()
		// The preview of the old file no longer matches the new result.
		if rec := ms.edit(addScope("src"), true, preview.ResultHash, nil); rec.Code != 409 || errorCode(t, rec) != "preview_changed" {
			t.Errorf("moved preview: %d %s", rec.Code, rec.Body)
		}
		// apply without result_hash, and result_hash without apply.
		if rec := ms.do(http.MethodPost, projectManifestPath, `{"base_hash":"`+ms.hash+`","operations":`+addScope("src")+`,"apply":true}`, nil); rec.Code != 400 {
			t.Errorf("apply without result hash: %d %s", rec.Code, rec.Body)
		}
		if ms.manifest() != changed {
			t.Error("a refused edit wrote the manifest")
		}

		// A single-repository server has no manifest endpoint.
		single := testOptions(fixtureRoot(t), s)
		h, runs, refs := newServerHandler(single, testHosts)
		defer runs.stop()
		defer refs.shutdown()
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, projectManifestPath, strings.NewReader("{}"))
			req.Host, req.RemoteAddr = testHost, "127.0.0.1:1"
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s on a single-repository server: %d %s", method, rec.Code, rec.Body)
			}
		}
	})
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestManifestEditGoldens previews and applies edits on both backends: each
// preview's diff, validation and re-index offer is golden; an apply writes
// exactly the previewed text atomically (comments kept), the server then
// serves the new declarations without a restart (links stale until the
// link pass), an invalid result is never written, and a rename offers a
// project re-index.
func TestManifestEditGoldens(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	backends(t, func(t *testing.T, w, s store.Store) {
		ms := newManifestServer(t, s)
		doc := ms.refresh()
		if doc.File != config.ProjectFile || doc.Content != editManifest || len(doc.Members) != 2 || doc.Token == "" {
			t.Fatalf("manifest document: %+v", doc)
		}
		got := map[string]any{}
		step := func(name, ops string, apply bool) manifestEditResponse {
			t.Helper()
			rec := ms.edit(ops, false, "", nil)
			var preview manifestEditResponse
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &preview) != nil {
				t.Fatalf("%s preview: %d %s", name, rec.Code, rec.Body)
			}
			var v any
			json.Unmarshal(rec.Body.Bytes(), &v)
			got[name] = v
			if !apply {
				return preview
			}
			before := ms.manifest()
			rec = ms.edit(ops, true, preview.ResultHash, nil)
			var applied manifestEditResponse
			if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &applied) != nil || !applied.Applied {
				t.Fatalf("%s apply: %d %s", name, rec.Code, rec.Body)
			}
			after := ms.manifest()
			if manifestHash([]byte(after)) != preview.ResultHash || unifiedDiff(config.ProjectFile, before, after) != preview.Diff {
				t.Fatalf("%s: the written manifest is not the previewed one:\n%s", name, after)
			}
			for _, comment := range []string{"# users project", "# the web client", "# frontend line"} {
				if !strings.Contains(after, comment) {
					t.Errorf("%s lost %q:\n%s", name, comment, after)
				}
			}
			ms.refresh()
			return preview
		}
		step("add consumes with selector", `[{"op":"add_consume","member":"frontend","consume":{"repository":"backend","select":{"path_prefixes":["/t022b7"]}}}]`, true)
		// The server serves the new declaration at once.
		rec := ms.do(http.MethodGet, projectPath, "", nil)
		var project projectResponse
		json.Unmarshal(rec.Body.Bytes(), &project)
		if len(project.Declarations.Consumes) != 1 || project.Declarations.Consumes[0].Provider != "backend" {
			t.Errorf("declarations after the edit: %+v", project.Declarations)
		}
		step("set selector", `[{"op":"set_select","member":"frontend","target":{"repository":"backend"},"select":{"bases":["VITE_API"],"origins":["http://localhost:3100"]}}]`, true)
		step("scope and its consumer", `[{"op":"add_scope","member":"backend","name":"db","path":"src/db"},{"op":"set_select","member":"frontend","target":{"repository":"backend"},"select":{}}]`, true)
		step("default scope", `[{"op":"set_default_scope","member":"frontend","repository":"backend","value":true}]`, true)
		// Removing a scope that an entry names does not validate: shown,
		// never written.
		invalid := step("remove a named scope", `[{"op":"set_default_scope","member":"frontend","repository":"backend","value":false},{"op":"add_consume","member":"frontend","consume":{"repository":"backend","scope":"db"}},{"op":"remove_scope","member":"backend","name":"db"}]`, false)
		before := ms.manifest()
		if rec := ms.edit(`[{"op":"set_default_scope","member":"frontend","repository":"backend","value":false},{"op":"add_consume","member":"frontend","consume":{"repository":"backend","scope":"db"}},{"op":"remove_scope","member":"backend","name":"db"}]`, true, invalid.ResultHash, nil); rec.Code != http.StatusUnprocessableEntity || errorCode(t, rec) != "invalid_manifest" || ms.manifest() != before {
			t.Errorf("invalid apply: %d %s", rec.Code, rec.Body)
		}
		if rec := ms.edit(`[{"op":"remove_consume","member":"backend","target":{"repository":"frontend"}}]`, false, "", nil); rec.Code != 400 || errorCode(t, rec) != "edit_refused" {
			t.Errorf("missing entry: %d %s", rec.Code, rec.Body)
		}
		renamed := step("rename", `[{"op":"rename_repository","member":"backend","to":"api"}]`, true)
		if renamed.Reindex.Kind != reindexProject {
			t.Errorf("rename offers %+v", renamed.Reindex)
		}
		rec = ms.do(http.MethodGet, "/api/v0/status?repository=api", "", nil)
		if rec.Code != http.StatusOK {
			t.Errorf("renamed member after reload: %d %s", rec.Code, rec.Body)
		}
		rec = ms.do(http.MethodGet, projectPath, "", nil)
		json.Unmarshal(rec.Body.Bytes(), &project)
		if project.Members[1].Repository != "api" {
			t.Errorf("members after the rename: %+v", project.Members)
		}
		step("remove all", `[{"op":"remove_consume","member":"frontend","target":{"repository":"api","default_scope":true}},{"op":"remove_scope","member":"api","name":"db"},{"op":"rename_repository","member":"api","to":"backend"}]`, true)
		if ms.manifest() != editManifest {
			t.Errorf("undoing every edit does not restore the manifest:\n%s", ms.manifest())
		}
		// A member whose own graphvein.yaml names it keeps that name.
		os.WriteFile(filepath.Join(ms.root, "backend", "graphvein.yaml"), []byte("version: 1\nrepository: backend\n"), 0o644)
		refused := step("rename refused by member config", `[{"op":"rename_repository","member":"backend","to":"api"}]`, false)
		if refused.Valid {
			t.Error("rename against the member's graphvein.yaml validates")
		}
		mu.Lock()
		bodies[t.Name()] = got
		mu.Unlock()
	})
	var first map[string]any
	for _, b := range bodies {
		if first == nil {
			first = b
			continue
		}
		if !sameJSONValue(first, b) {
			t.Error("backends answer differently")
		}
	}
	if first != nil {
		golden.Check(t, filepath.Join("testdata", "project-manifest-edit.golden.json"), first)
	}
}

func sameJSONValue(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// TestManifestRenameRetire covers a rename's retirement of the old name
// where the server cannot do it (T022 B11): a project lock held by another
// process refuses the rename with nothing written, and a server without a
// writer store writes the manifest but reports that the old rows stay.
// The removal itself is TestRetireProjectMember (both backends) and
// TestServeProjectManifestEdit (a real server, both backends).
func TestManifestRenameRetire(t *testing.T) {
	const rename = `[{"op":"rename_repository","member":"backend","to":"api"}]`
	backends(t, func(t *testing.T, w, s store.Store) {
		locked := newManifestServerWith(t, s, func(opt *Options) {
			opt.Locker = fakeLocker{acquireErr: fmt.Errorf("%w (test)", store.ErrIndexLocked), state: store.LockHeldElsewhere}
			opt.OpenWriter = func(context.Context) (store.Store, error) { return w, nil }
		})
		var preview manifestEditResponse
		rec := locked.edit(rename, false, "", nil)
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &preview) != nil || preview.Retired != nil {
			t.Fatalf("preview: %d %s", rec.Code, rec.Body)
		}
		if rec := locked.edit(rename, true, preview.ResultHash, nil); rec.Code != http.StatusConflict || errorCode(t, rec) != "index_locked" || locked.manifest() != editManifest {
			t.Fatalf("rename under a held project lock: %d %s", rec.Code, rec.Body)
		}

		noWriter := newManifestServer(t, s)
		rec = noWriter.edit(rename, true, preview.ResultHash, nil)
		var applied manifestEditResponse
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &applied) != nil || !applied.Applied {
			t.Fatalf("rename without a writer: %d %s", rec.Code, rec.Body)
		}
		if len(applied.Retired) != 1 || applied.Retired[0].Repository != "backend" || applied.Retired[0].Removed || applied.Retired[0].Error == nil {
			t.Fatalf("retired without a writer = %+v", applied.Retired)
		}
	})
}

// TestManifestRenameConfirmDelete is the T022 review gate on both
// backends: a rename whose old name has a stored index made on its own (no
// stored link pass lists it) previews "delete stored index of backend (N
// nodes)" as standalone, refuses the apply without that explicit confirm
// (nothing written, nothing deleted), and writes and retires with it.
func TestManifestRenameConfirmDelete(t *testing.T) {
	const rename = `[{"op":"rename_repository","member":"backend","to":"api"}]`
	backends(t, func(t *testing.T, w, s store.Store) {
		ms := newManifestServerWith(t, s, func(opt *Options) {
			opt.Locker = heldLocker{}
			opt.OpenWriter = func(context.Context) (store.Store, error) { return w, nil }
		})
		indexRepository(t, filepath.Join(ms.root, "backend"), w, "backend")
		ms.refresh()
		var preview manifestEditResponse
		rec := ms.edit(rename, false, "", nil)
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &preview) != nil {
			t.Fatalf("preview: %d %s", rec.Code, rec.Body)
		}
		if len(preview.Retiring) != 1 {
			t.Fatalf("retiring = %+v", preview.Retiring)
		}
		r := preview.Retiring[0]
		if r.Repository != "backend" || !r.Stored || r.Nodes == 0 || !r.Standalone || len(r.Projects) != 0 || r.Confirm != fmt.Sprintf("delete stored index of backend (%d nodes)", r.Nodes) {
			t.Fatalf("retiring = %+v", r)
		}
		rec = ms.edit(rename, true, preview.ResultHash, nil)
		if rec.Code != http.StatusConflict || errorCode(t, rec) != "delete_confirmation_required" || ms.manifest() != editManifest || !strings.Contains(rec.Body.String(), "only copy") {
			t.Fatalf("apply without the confirm: %d %s", rec.Code, rec.Body)
		}
		if runs, err := s.Runs(context.Background(), "backend"); err != nil || len(runs) == 0 {
			t.Fatalf("a refused rename deleted the index: %d %v", len(runs), err)
		}
		// A confirm naming another repository is not the confirm.
		body := `{"base_hash":"` + ms.hash + `","operations":` + rename + `,"apply":true,"result_hash":"` + preview.ResultHash + `","confirm_delete":["frontend"]}`
		if rec := ms.do(http.MethodPost, projectManifestPath, body, nil); rec.Code != http.StatusConflict {
			t.Fatalf("apply with another confirm: %d %s", rec.Code, rec.Body)
		}
		body = `{"base_hash":"` + ms.hash + `","operations":` + rename + `,"apply":true,"result_hash":"` + preview.ResultHash + `","confirm_delete":["backend"]}`
		rec = ms.do(http.MethodPost, projectManifestPath, body, nil)
		var applied manifestEditResponse
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &applied) != nil || !applied.Applied || len(applied.Retired) != 1 || !applied.Retired[0].Removed {
			t.Fatalf("apply with the confirm: %d %s", rec.Code, rec.Body)
		}
		if runs, err := s.Runs(context.Background(), "backend"); err != nil || len(runs) != 0 {
			t.Fatalf("runs of backend after the confirmed rename = %d %v", len(runs), err)
		}
	})
}

// TestScopeFolderCase is T022 review: a scope folder must exist with
// exactly the declared case, component by component (a case-insensitive
// file system would resolve a wrong case and the scope would be empty).
func TestScopeFolderCase(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Apps", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := caseExact(root, "Apps/web"); !ok || got != "Apps/web" {
		t.Errorf("exact case = %q %v", got, ok)
	}
	if got, ok := caseExact(root, "apps/web"); ok || got != "Apps" {
		t.Errorf("wrong case = %q %v, want the case on disk", got, ok)
	}
	if got, ok := caseExact(root, "Apps/missing"); ok || got != "Apps" {
		t.Errorf("missing folder = %q %v", got, ok)
	}
}

// heldLocker grants the project lock at once with a lease that stays held.
type heldLocker struct{}

func (heldLocker) Acquire(context.Context, time.Duration) (store.IndexLease, error) {
	return heldLease{}, nil
}
func (heldLocker) State(context.Context) (store.LockState, error) { return store.LockFree, nil }
func (heldLocker) Close() error                                   { return nil }

type heldLease struct{}

func (heldLease) Check(context.Context) error { return nil }
func (heldLease) Release() error              { return nil }

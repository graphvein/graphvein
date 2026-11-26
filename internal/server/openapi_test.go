package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/openapi"
	"github.com/graphvein/graphvein/internal/store"
	"go.yaml.in/yaml/v3"
)

// storeRoutes writes route nodes as one run of repository fixture.
func storeRoutes(t *testing.T, w store.Store, nodes ...contract.Node) {
	t.Helper()
	ctx := context.Background()
	header := contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: index.Version}, Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z"}
	tx, err := w.BeginRun(ctx, header)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	files := map[string]bool{}
	for _, n := range nodes {
		if !files[n.File] {
			files[n.File] = true
			if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: n.File, Language: "javascript", Precision: "best_effort", ContentHash: contract.ContentHash([]byte(n.File))}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.UpsertNode(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// openapiRoute is an Express route node with the given stored paths.
func openapiRoute(method, pattern string, line int, templates ...string) contract.Node {
	paths := []any{}
	for _, tpl := range templates {
		paths = append(paths, map[string]any{"template": tpl, "conditional": false, "resolution": "literal"})
	}
	id := "route:src/app.js#" + strings.NewReplacer(" ", "%20", "/", "%2F", ":", "%3A", "@", "%40").Replace(method+" "+pattern)
	return contract.Node{Type: "node", Identity: id, Kind: "route", Level: 4, Name: method + " " + pattern, QualifiedName: method + " " + pattern, File: "src/app.js",
		Span: &contract.Span{StartLine: line, EndLine: line, EndColumn: 20}, ContentHash: contract.ContentHash([]byte(id)),
		Attributes: map[string]any{"framework": "express", "method": method, "pattern": pattern, "handler": nil, "handler_inline": true, "middleware": []any{}, "paths": paths}}
}

// explorerServer is a single-repository server built once, so its edit
// token stays the same across requests (as in graphvein serve).
type explorerServer struct {
	t     *testing.T
	h     http.Handler
	token string
}

func (es *explorerServer) do(method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	es.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = testHost
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Origin", "http://"+testHost)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(tokenHeader, es.token)
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
	es.h.ServeHTTP(rec, req)
	return rec
}

// TestOpenAPIServedAndEdited covers GET /api/v0/openapi (the same bytes
// as the generator, on both backends) and the guarded edit of
// explorer.path_values: preview, refusals, apply, and the document
// following the written value (rows OG-52, OG-90..OG-93).
func TestOpenAPIServedAndEdited(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		storeRoutes(t, w, openapiRoute("GET", "/items/:id", 3, "/api/items/{id}"), openapiRoute("POST", "/orphan", 5))
		root := t.TempDir()
		original := "# team settings\nversion: 1\ntryit:\n  environments:\n    - {name: local, label: local, base_url: \"http://127.0.0.1:8000\"}\n"
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		opt := testOptions(root, s)
		es := &explorerServer{t: t, h: NewHandler(opt, testHosts)}

		// The document is the generator's bytes.
		rec := es.do(http.MethodGet, openapiPath, "", nil)
		in, err := openapi.Read(ctx, s, "fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		in.Servers = openapi.Servers(nil, cfg.TryIt.Environments, "fixture")
		want, _ := openapi.Encode(openapi.Generate(in), openapi.FormatJSON)
		if rec.Code != http.StatusOK || rec.Body.String() != string(want) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("GET openapi: %d %s", rec.Code, rec.Body.String())
		}
		rec = es.do(http.MethodGet, openapiPath+"?format=yaml", "", nil)
		var y map[string]any
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/yaml; charset=utf-8" || yaml.Unmarshal(rec.Body.Bytes(), &y) != nil || y["openapi"] != openapi.Version {
			t.Errorf("GET openapi yaml: %d %s", rec.Code, rec.Header().Get("Content-Type"))
		}
		if rec := es.do(http.MethodGet, openapiPath+"?format=xml", "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("format=xml: %d", rec.Code)
		}
		if rec := es.do(http.MethodPost, openapiPath, "{}", nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST openapi: %d", rec.Code)
		}

		if !strings.Contains(string(want), `"url": "http://127.0.0.1:8000"`) {
			t.Errorf("configured environment not in servers")
		}
		// The values document: token, hash, the unresolved route and its key.
		var vals explorerValuesResponse
		rec = es.do(http.MethodGet, explorerValuesPath, "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &vals); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("GET path-values: %d %s", rec.Code, rec.Body.String())
		}
		es.token = vals.Token
		orphan := "route:src/app.js#POST%20%2Forphan"
		if !vals.Exists || !vals.Editable || len(vals.Unresolved) != 1 || vals.Unresolved[0].Keys[0] != orphan || len(vals.Values) != 0 {
			t.Fatalf("values %+v", vals)
		}
		edit := func(ops string, apply bool, resultHash string) string {
			body := `{"base_hash":"` + vals.Hash + `","operations":` + ops
			if apply {
				body += `,"apply":true,"result_hash":"` + resultHash + `"`
			}
			return body + "}"
		}
		set := `[{"op":"set","key":"` + orphan + `","value":"/api/orphans/{id}"}]`
		for _, c := range []struct {
			name, body string
			header     map[string]string
			status     int
			code       string
		}{
			{"no token", edit(set, false, ""), map[string]string{tokenHeader: ""}, 403, "token_required"},
			{"wrong token", edit(set, false, ""), map[string]string{tokenHeader: "guess"}, 403, "token_required"},
			{"remote peer", edit(set, false, ""), map[string]string{"RemoteAddr": "192.0.2.1:4000"}, 403, "not_loopback"},
			{"foreign origin", edit(set, false, ""), map[string]string{"Origin": "http://evil.example"}, 403, "cross_site_request"},
			{"cross site", edit(set, false, ""), map[string]string{"Sec-Fetch-Site": "cross-site"}, 403, "cross_site_request"},
			{"form post", edit(set, false, ""), map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type"},
			{"stale base", strings.Replace(edit(set, false, ""), vals.Hash, "sha256:00", 1), nil, 409, "config_changed"},
			{"bad key", edit(`[{"op":"set","key":"../x","value":"/x"}]`, false, ""), nil, 400, "invalid_value"},
			{"bad value", edit(`[{"op":"set","key":"`+orphan+`","value":"x?y"}]`, false, ""), nil, 400, "invalid_value"},
			{"unknown field", strings.Replace(edit(set, false, ""), `"base_hash"`, `"path":"/etc/x","base_hash"`, 1), nil, 400, "bad_request"},
			{"duplicate key", strings.Replace(edit(set, false, ""), `"base_hash"`, `"apply":false,"apply":false,"base_hash"`, 1), nil, 400, "bad_request"},
			{"remove missing", edit(`[{"op":"remove","key":"`+orphan+`"}]`, false, ""), nil, 400, "edit_refused"},
			{"apply unpreviewed", edit(set, true, "sha256:00"), nil, 409, "preview_changed"},
		} {
			rec := es.do(http.MethodPost, explorerValuesPath, c.body, c.header)
			var e struct{ Error struct{ Code string } }
			_ = json.Unmarshal(rec.Body.Bytes(), &e)
			if rec.Code != c.status || e.Error.Code != c.code {
				t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
			}
		}
		if data, _ := os.ReadFile(filepath.Join(root, "graphvein.yaml")); string(data) != original {
			t.Fatalf("a refused edit wrote the file:\n%s", data)
		}
		// Preview, then apply exactly the preview.
		var preview explorerEditResponse
		rec = es.do(http.MethodPost, explorerValuesPath, edit(set, false, ""), nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || rec.Code != 200 || preview.Applied || !preview.Valid || !strings.Contains(preview.Diff, "+    - key:") {
			t.Fatalf("preview: %d %s", rec.Code, rec.Body.String())
		}
		var applied explorerEditResponse
		rec = es.do(http.MethodPost, explorerValuesPath, edit(set, true, preview.ResultHash), nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &applied); err != nil || !applied.Applied || applied.ResultHash != preview.ResultHash {
			t.Fatalf("apply: %d %s", rec.Code, rec.Body.String())
		}
		data, _ := os.ReadFile(filepath.Join(root, "graphvein.yaml"))
		cfg, err = config.Load(root)
		if err != nil || !strings.HasPrefix(string(data), "# team settings\n") || len(cfg.Explorer.PathValues) != 1 || !config.RegistrationHash(cfg.Explorer.PathValues[0].Registration) {
			t.Fatalf("written file %q (%v)", data, err)
		}
		// The document follows the value: the route's operation is set by you.
		var doc openapi.Document
		rec = es.do(http.MethodGet, openapiPath, "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		item := doc.Paths["/api/orphans/{id}"]
		if item == nil || item.Post == nil || item.Post.X.Resolution != openapi.SetByYou || len(doc.X.Unresolved) != 0 || doc.X.UserValues[0].Status != openapi.UserValueUsed {
			t.Fatalf("document after the edit: %s", rec.Body.String())
		}
		// An ordinal key records its route's registration hash; an
		// ordinal key no stored route has is refused (OG-90).
		before := vals
		rec = es.do(http.MethodGet, explorerValuesPath, "", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &vals)
		ordinal := "route:src/app.js#POST%20%2Forphan%402"
		rec = es.do(http.MethodPost, explorerValuesPath, edit(`[{"op":"set","key":"`+ordinal+`","value":"/o2"}]`, false, ""), nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_value") {
			t.Errorf("unknown ordinal key: %d %s", rec.Code, rec.Body.String())
		}
		storeRoutes(t, w, openapiRoute("GET", "/items/:id", 3, "/api/items/{id}"), openapiRoute("POST", "/orphan", 5), openapiRoute("POST", "/orphan@2", 6))
		rec = es.do(http.MethodPost, explorerValuesPath, edit(`[{"op":"set","key":"`+ordinal+`","value":"/o2"}]`, false, ""), nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "registration: sha256:") {
			t.Errorf("ordinal key: %d %s", rec.Code, rec.Body.String())
		}
		// A mount key of a hop that adds no unread part takes only "/"
		// (OG-95); the edit records the hop's registration.
		passed := openapiRoute("GET", "/passed", 7)
		mountKey := "mount:src/app.js#%28module%29%20wrap%28r%29"
		passed.Attributes["unresolved_mounts"] = []any{map[string]any{"key": mountKey, "registration": "sha256:" + strings.Repeat("ab", 32), "reason": "route_mount_unresolved", "hop": "wrap(r)", "file": "src/app.js", "line": 7, "value_covers": "nothing",
			"paths": []any{map[string]any{"template": "/passed", "conditional": false, "resolution": "literal"}}}}
		storeRoutes(t, w, openapiRoute("GET", "/items/:id", 3, "/api/items/{id}"), openapiRoute("POST", "/orphan", 5), openapiRoute("POST", "/orphan@2", 6), passed)
		rec = es.do(http.MethodPost, explorerValuesPath, edit(`[{"op":"set","key":"`+mountKey+`","value":"/x"}]`, false, ""), nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "the only value is /") {
			t.Errorf("mount key adding nothing, value /x: %d %s", rec.Code, rec.Body.String())
		}
		rec = es.do(http.MethodPost, explorerValuesPath, edit(`[{"op":"set","key":"`+mountKey+`","value":"/"}]`, false, ""), nil)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "registration: sha256:abab") {
			t.Errorf("mount key adding nothing, value /: %d %s", rec.Code, rec.Body.String())
		}
		vals = before
		// The old base hash is stale now; a remove based on the new one
		// gives the original text back.
		if rec := es.do(http.MethodPost, explorerValuesPath, edit(`[{"op":"remove","key":"`+orphan+`"}]`, false, ""), nil); rec.Code != http.StatusConflict {
			t.Errorf("stale base after a write: %d", rec.Code)
		}
		rec = es.do(http.MethodGet, explorerValuesPath, "", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &vals)
		if len(vals.Values) != 1 || vals.Values[0].Operations != 1 {
			t.Errorf("values after the edit %+v", vals.Values)
		}
		remove := `[{"op":"remove","key":"` + orphan + `"}]`
		rec = es.do(http.MethodPost, explorerValuesPath, edit(remove, false, ""), nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &preview)
		rec = es.do(http.MethodPost, explorerValuesPath, edit(remove, true, preview.ResultHash), nil)
		if data, _ := os.ReadFile(filepath.Join(root, "graphvein.yaml")); rec.Code != 200 || string(data) != original {
			t.Errorf("remove: %d, file %q", rec.Code, data)
		}
	})
}

// TestExplorerValuesWithoutConfigFile: an edit creates graphvein.yaml
// (version 1 and the values only), and an invalid graphvein.yaml makes
// the document refuse rather than drop the user's values.
func TestExplorerValuesWithoutConfigFile(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		storeRoutes(t, w, openapiRoute("GET", "/x", 3))
		root := t.TempDir()
		es := &explorerServer{t: t, h: NewHandler(testOptions(root, s), testHosts)}
		var vals explorerValuesResponse
		_ = json.Unmarshal(es.do(http.MethodGet, explorerValuesPath, "", nil).Body.Bytes(), &vals)
		es.token = vals.Token
		if vals.Exists || vals.Hash != manifestHash(nil) {
			t.Fatalf("%+v", vals)
		}
		body := `{"base_hash":"` + vals.Hash + `","operations":[{"op":"set","key":"route:src/app.js#GET%20%2Fx","value":"/x"}]`
		var preview explorerEditResponse
		_ = json.Unmarshal(es.do(http.MethodPost, explorerValuesPath, body+"}", nil).Body.Bytes(), &preview)
		rec := es.do(http.MethodPost, explorerValuesPath, body+`,"apply":true,"result_hash":"`+preview.ResultHash+`"}`, nil)
		data, _ := os.ReadFile(filepath.Join(root, "graphvein.yaml"))
		if rec.Code != 200 || !strings.HasPrefix(string(data), "version: 1\nexplorer:\n") {
			t.Fatalf("%d %s / %q", rec.Code, rec.Body.String(), data)
		}
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nexplorer: {path_values: [{key: x, value: /x}]}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if rec := es.do(http.MethodGet, openapiPath, "", nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "invalid_config") {
			t.Errorf("invalid config: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// TestOpenAPIProjectMembers: a project server lists one document per
// member without a repository parameter, serves each member's document
// with one, and refuses a path value edit for a member without a
// checkout (row OG-61).
func TestOpenAPIProjectMembers(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := copyProjectFixture(t)
		project, err := config.LoadProject(root)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(root, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(root, "backend"), w, "backend")
		project.Members = append(project.Members, config.Member{Repository: "ghost", RepositorySource: config.RepositoryFromManifest})
		es := &explorerServer{t: t, h: NewHandler(projectOptions(project, s), testHosts)}
		var list openapiMembersResponse
		rec := es.do(http.MethodGet, openapiPath, "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Members) != 3 || list.Members[0].Document != openapiPath+"?repository="+list.Members[0].Repository {
			t.Fatalf("members: %d %s", rec.Code, rec.Body.String())
		}
		var doc openapi.Document
		rec = es.do(http.MethodGet, openapiPath+"?repository=backend", "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || doc.X.Repository != "backend" || len(doc.Paths) == 0 {
			t.Fatalf("backend document: %d %s", rec.Code, rec.Body.String())
		}
		var vals explorerValuesResponse
		rec = es.do(http.MethodGet, explorerValuesPath+"?repository=ghost", "", nil)
		if err := json.Unmarshal(rec.Body.Bytes(), &vals); err != nil || vals.Editable || vals.Reason == nil {
			t.Fatalf("ghost values: %d %s", rec.Code, rec.Body.String())
		}
		es.token = vals.Token
		rec = es.do(http.MethodPost, explorerValuesPath+"?repository=ghost", `{"base_hash":"","operations":[{"op":"remove","key":"route:x#y"}]}`, nil)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no_checkout") {
			t.Errorf("ghost edit: %d %s", rec.Code, rec.Body.String())
		}
	})
}

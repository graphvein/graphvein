package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// apiFixtureRoot copies testdata/fixtures/api-section to a temporary
// repository and restores the inert names of its README: the ignore file,
// and the Tauri app's files, which are inert in the repository so that the
// self-index gains no app registry (app scope is repository-wide).
func apiFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/api-section")); err != nil {
		t.Fatal(err)
	}
	renames := map[string]string{"_graphveinignore": ".graphveinignore"}
	for _, name := range []string{"app/Cargo.toml", "app/build.rs", "app/tauri.conf.json", "app/src/main.rs", "app/src/commands.rs"} {
		renames[name+".inert"] = name
	}
	for from, to := range renames {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// ingestStatuses ingests the crafted snapshot as repository "statuses".
func ingestStatuses(t *testing.T, s store.Store) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "api-statuses.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := ingest.Stream(context.Background(), f, s); err != nil {
		t.Fatal(err)
	}
	// Ingest records rejected records as diagnostics; the snapshot must
	// have none, or the goldens would silently miss cases.
	snapshot, err := s.Dump(context.Background(), "statuses")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Diagnostics) != 0 || len(snapshot.Nodes) != 39 {
		t.Fatalf("crafted snapshot: %d nodes, diagnostics %v", len(snapshot.Nodes), snapshot.Diagnostics)
	}
}

// normalizeAPI decodes an API body and replaces the run's finish time, which
// a test cannot fix; golden.CheckRoot replaces the absolute root in links.
func normalizeAPI(t *testing.T, body []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v map[string]any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err, string(body))
	}
	if idx, ok := v["index"].(map[string]any); ok {
		idx["finished_at"] = finishedAtPlaceholder
	}
	return v
}

// apiCase is one golden request.
type apiCase struct {
	name, repo, path string
}

func apiCases() []apiCase {
	items := "route:server/app.ts#GET%20%2Ft016d-items"
	return []apiCase{
		{"api-endpoints-production", "fixture", "/api/v0/api/endpoints"},
		{"api-endpoints-all", "fixture", "/api/v0/api/endpoints?scope=all"},
		{"api-endpoints-root", "fixture", "/api/v0/api/endpoints?transport=http&root=web"},
		{"api-endpoint-route", "fixture", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape(items)},
		{"api-endpoint-route-literal-origin", "fixture", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items%2F%3Aid")},
		{"api-endpoint-route-suffix", "fixture", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-api%2Ft016d-users")},
		{"api-endpoint-route-never", "fixture", "/api/v0/api/endpoint?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-never")},
		{"api-endpoint-route-ambiguous", "fixture", "/api/v0/api/endpoint?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-tie")},
		{"api-endpoint-route-test-only", "fixture", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("route:server/tests/routes.ts#GET%20%2Ft016d-test-only")},
		{"api-endpoint-command", "fixture", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("ipc_command:app/src/main.rs#main.COMMAND%20t016d_greet")},
		{"api-endpoint-action", "fixture", "/api/v0/api/endpoint?id=" + url.QueryEscape("server_action:next/app/actions.ts#ACTION%20t016dSave")},
		{"api-calls-http-gaps", "fixture", "/api/v0/api/calls?transport=http&status=ambiguous,dynamic,test_only,unmatched,wrapper"},
		{"api-calls-tauri-gaps", "fixture", "/api/v0/api/calls?transport=tauri_ipc&status=dynamic,plugin,unmatched,wrapper"},
		{"api-statuses-endpoints", "statuses", "/api/v0/api/endpoints?scope=all"},
		{"api-statuses-endpoint-route", "statuses", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("route:server/routes.ts#GET%20%2Fs")},
		{"api-statuses-endpoint-command", "statuses", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("ipc_command:app/main.rs#main.COMMAND%20s")},
	}
}

// statusesCalls lists every status the crafted snapshot holds, including
// the empty status of a call without one.
var statusesCalls = map[string]string{
	"http":      "/api/v0/api/calls?scope=all&transport=http&status=" + url.QueryEscape(",LINKED,ambiguous,dynamic,linked,test_only,unmatched,weird,wrapper"),
	"tauri_ipc": "/api/v0/api/calls?scope=all&transport=tauri_ipc&status=" + url.QueryEscape(",LINKED,acl_possible,ambiguous,dynamic,global_unlinked,linked,plugin,registry_unproven,shadow_possible,test_only,unknown_command,unmatched,weird,wrapper"),
}

// TestAPISectionGoldens checks every API section golden on both backends
// and that both backends produce the same bodies.
func TestAPISectionGoldens(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		ingestStatuses(t, w)
		empty := t.TempDir()
		options := map[string]Options{"fixture": testOptions(root, s), "statuses": testOptions(empty, s)}
		opt := options["statuses"]
		opt.Repository = "statuses"
		options["statuses"] = opt
		roots := map[string]string{"fixture": root, "statuses": empty}
		got := map[string]string{}
		check := func(name, repo, path string) any {
			rec := get(t, options[repo], http.MethodGet, path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[name] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots[repo]))
			return v
		}
		for _, tc := range apiCases() {
			v := check(tc.name, tc.repo, tc.path)
			golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, roots[tc.repo])
		}
		calls := map[string]any{}
		for transport, path := range statusesCalls {
			calls[transport] = check("api-statuses-calls-"+transport, "statuses", path)
		}
		golden.CheckRoot(t, filepath.Join("testdata", "api-statuses-calls.golden.json"), calls, empty)
		// A handler identity that is not stored is reported as missing,
		// never as resolved.
		var detail endpointResponse
		decodeInto(t, get(t, options["statuses"], http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape("ipc_command:app/main.rs#plugin.COMMAND%20p"), nil), &detail)
		if detail.Handler.State != handlerMissing || detail.Handler.Node != nil || detail.Handler.Edge != nil {
			t.Errorf("missing handler: %+v", detail.Handler)
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		for name, body := range bodies["sqlite"] {
			if bodies["postgres"][name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// TestAPISectionChecks asserts the precision rules on the fixture directly,
// so a golden rewrite cannot hide them.
func TestAPISectionChecks(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		var list endpointsResponse
		decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil), &list)
		for _, tr := range list.Transports {
			raw, _ := json.Marshal(tr.Endpoints)
			var rows []struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				var detail struct {
					Callers []struct {
						Node  contract.Node `json:"node"`
						Edges []any         `json:"edges"`
					} `json:"callers"`
					NearMisses []struct {
						Node contract.Node `json:"node"`
					} `json:"near_misses"`
				}
				decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?scope=all&id="+url.QueryEscape(row.ID), nil), &detail)
				edges, err := s.Edges(context.Background(), "fixture", []string{row.ID}, []string{map[string]string{"http": "calls_route", "tauri_ipc": "invokes_command", "server_action": "calls_action"}[tr.Transport]}, store.Incoming)
				if err != nil {
					t.Fatal(err)
				}
				stored := map[string]bool{}
				for _, e := range edges {
					stored[e.Source] = true
				}
				for _, c := range detail.Callers {
					if !stored[c.Node.Identity] || len(c.Edges) == 0 {
						t.Errorf("%s: caller %s has no stored edge to it", row.ID, c.Node.Identity)
					}
				}
				for _, m := range detail.NearMisses {
					if st := statusOf(m.Node); st != "ambiguous" && st != "test_only" || stored[m.Node.Identity] {
						t.Errorf("%s: near miss %s with status %q", row.ID, m.Node.Identity, st)
					}
				}
			}
		}
		// Every D7 case is present in the fixture.
		byTransport := map[string]transportSummary{}
		for _, tr := range list.Transports {
			byTransport[tr.Transport] = tr
		}
		httpCov, ipc := byTransport["http"], byTransport["tauri_ipc"]
		for _, st := range []string{"linked", "unmatched", "dynamic", "wrapper", "ambiguous", "test_only"} {
			if httpCov.Calls.ByStatus[st] == 0 {
				t.Errorf("fixture has no http %s call", st)
			}
		}
		for _, st := range []string{"linked", "unmatched", "dynamic", "wrapper", "plugin"} {
			if ipc.Calls.ByStatus[st] == 0 {
				t.Errorf("fixture has no tauri_ipc %s call", st)
			}
		}
		if httpCov.Calls.EdgesFromUnlinked != 1 || ipc.Calls.EdgesFromUnlinked != 1 || httpCov.Calls.LinkedWithoutEdge != 0 || httpCov.NeverCalled == 0 || ipc.NeverCalled == 0 {
			t.Errorf("coverage: http %+v never %d, ipc %+v never %d", httpCov.Calls, httpCov.NeverCalled, ipc.Calls, ipc.NeverCalled)
		}
		roots := map[string]*string{}
		for _, r := range httpCov.Roots {
			roots[r.Root] = r.Manifest
		}
		for root, manifest := range map[string]string{".": "", "scripts": "", "tools": "", "web": "web/package.json", "web/packages/ui": "web/packages/ui/package.json"} {
			got, ok := roots[root]
			if !ok || manifest == "" && got != nil || manifest != "" && (got == nil || *got != manifest) {
				t.Errorf("root %s: manifest %v, want %q (present %v)", root, got, manifest, ok)
			}
		}
		// The ignored tools/package.json is not stored, so it makes no root.
		hashes, err := s.FileHashes(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := hashes["tools/package.json"]; ok {
			t.Error("tools/package.json is stored; the fixture's ignore file did not apply")
		}
		for _, tr := range list.Transports {
			for _, n := range nodesOfKind(t, s, map[string]string{"http": "route", "tauri_ipc": "ipc_command"}[tr.Transport]) {
				if n.Kind == "ipc_command" && n.Attributes["transport"] != "tauri_ipc" {
					t.Errorf("%s: transport %v", n.Identity, n.Attributes["transport"])
				}
			}
		}
		for _, n := range nodesOfKind(t, s, "ipc_call") {
			if n.Attributes["transport"] != "tauri_ipc" {
				t.Errorf("%s: transport %v", n.Identity, n.Attributes["transport"])
			}
		}
	})
}

func nodesOfKind(t *testing.T, s store.Store, kind string) []contract.Node {
	t.Helper()
	nodes, err := s.Nodes(context.Background(), "fixture", store.Query{Kind: kind})
	if err != nil {
		t.Fatal(err)
	}
	return nodes
}

func decodeInto(t *testing.T, rec interface{ Result() *http.Response }, v any) {
	t.Helper()
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatal(err)
	}
}

func TestAPIParameters(t *testing.T) {
	opt := handlerOptions(t)
	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"/api/v0/api/endpoints?scope=prod", "bad_request", 400},
		{"/api/v0/api/endpoints?scope=", "bad_request", 400},
		{"/api/v0/api/endpoints?scope=all&scope=test", "bad_request", 400},
		{"/api/v0/api/endpoints?transport=grpc", "bad_request", 400},
		{"/api/v0/api/endpoints?transport=", "bad_request", 400},
		{"/api/v0/api/endpoints?root=a&root=b", "bad_request", 400},
		{"/api/v0/api/endpoint", "bad_request", 400},
		{"/api/v0/api/endpoint?id=", "bad_request", 400},
		{"/api/v0/api/endpoint?id=" + url.QueryEscape("route:a.ts#GET /"), "bad_request", 400},
		{"/api/v0/api/endpoint?id=" + url.QueryEscape("function:a.ts#f"), "bad_request", 400},
		{"/api/v0/api/endpoint?id=" + url.QueryEscape("route:a.ts#GET%20%2F") + "&scope=x", "bad_request", 400},
		{"/api/v0/api/endpoint?id=" + url.QueryEscape("route:a.ts#GET%20%2F"), "not_found", 404},
		{"/api/v0/api/endpoint?id=" + url.QueryEscape("ipc_command:a.rs#main.COMMAND%20x"), "not_found", 404},
		{"/api/v0/api/calls?status=dynamic", "bad_request", 400},
		{"/api/v0/api/calls?transport=all&status=dynamic", "bad_request", 400},
		{"/api/v0/api/calls?transport=http", "bad_request", 400},
		{"/api/v0/api/calls?transport=http&status=a&status=b", "bad_request", 400},
		{"/api/v0/api/calls?transport=http&status=dynamic&scope=tests", "bad_request", 400},
		{"/api/v0/api/endpoints?transport=http&scope=test&root=.", "", 200},
		{"/api/v0/api/calls?transport=http&status=", "", 200},
		{"/api/v0/api/endpoints?unknown=1", "", 200},
	} {
		rec := get(t, opt, http.MethodGet, tc.path, nil)
		if rec.Code != tc.status {
			t.Errorf("%s: status %d %s, want %d", tc.path, rec.Code, rec.Body.String(), tc.status)
			continue
		}
		if tc.code != "" && errorCode(t, rec) != tc.code {
			t.Errorf("%s: code %s", tc.path, rec.Body.String())
		}
	}
}

// changingStore returns a different run list on chosen Runs calls.
type changingStore struct {
	store.Store
	mu    sync.Mutex
	calls int
	// change reports whether the n-th Runs call (1-based) sees a new run.
	change func(n int) bool
	extra  int
}

func (c *changingStore) Runs(ctx context.Context, repo string) ([]store.Run, error) {
	c.mu.Lock()
	c.calls++
	if c.change(c.calls) {
		c.extra++
	}
	extra := c.extra
	c.mu.Unlock()
	runs, err := c.Store.Runs(ctx, repo)
	for i := 0; i < extra; i++ {
		runs = append(runs, store.Run{ID: fmt.Sprintf("new-%d", i), FinishedAt: fmt.Sprintf("2030-01-01T00:00:%02dZ", i)})
	}
	return runs, err
}

func TestAPIConsistency(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		indexRoot(t, apiFixtureRoot(t), w)
		paths := []string{"/api/v0/api/endpoints", "/api/v0/api/calls?transport=http&status=dynamic", "/api/v0/api/endpoint?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items"), "/api/v0/api/flow?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items"), "/api/v0/source?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items")}
		for _, path := range paths {
			// Every read sees a new run: one retry, then 503.
			always := &changingStore{Store: s, change: func(n int) bool { return true }}
			rec := get(t, testOptions(t.TempDir(), always), http.MethodGet, path, nil)
			if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "index_changed" || always.calls != 4 {
				t.Errorf("%s always changing: status %d, %d runs reads: %s", path, rec.Code, always.calls, rec.Body.String())
			}
			// One change during the first attempt, then stable: the retry
			// answers 200 from the second attempt's reads.
			once := &changingStore{Store: s, change: func(n int) bool { return n == 2 }}
			rec = get(t, testOptions(t.TempDir(), once), http.MethodGet, path, nil)
			if rec.Code != http.StatusOK || once.calls != 4 {
				t.Errorf("%s one change: status %d, %d runs reads", path, rec.Code, once.calls)
			}
			// No change: one attempt.
			stable := &changingStore{Store: s, change: func(n int) bool { return false }}
			if rec := get(t, testOptions(t.TempDir(), stable), http.MethodGet, path, nil); rec.Code != http.StatusOK || stable.calls != 2 {
				t.Errorf("%s stable: status %d, %d runs reads", path, rec.Code, stable.calls)
			}
		}
	})
}

// recordingStore counts reads and fails the n-th one (1-based; 0 never).
type recordingStore struct {
	store.Store
	fail  int
	reads int
}

func (r *recordingStore) next() error {
	r.reads++
	if r.reads == r.fail {
		return errInjected
	}
	return nil
}

func (r *recordingStore) Runs(ctx context.Context, repo string) ([]store.Run, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.Runs(ctx, repo)
}

func (r *recordingStore) Nodes(ctx context.Context, repo string, q store.Query) ([]contract.Node, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.Nodes(ctx, repo, q)
}

func (r *recordingStore) Edges(ctx context.Context, repo string, ids, kinds []string, dir store.Direction) ([]contract.Edge, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.Edges(ctx, repo, ids, kinds, dir)
}

func (r *recordingStore) NodesByIdentity(ctx context.Context, repo string, ids []string) ([]contract.Node, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.NodesByIdentity(ctx, repo, ids)
}

func (r *recordingStore) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.FileHashes(ctx, repo)
}

func (r *recordingStore) Diagnostics(ctx context.Context, repo string, files []string) ([]contract.Diagnostic, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.Diagnostics(ctx, repo, files)
}

func (r *recordingStore) DiagnosticsWithoutFile(ctx context.Context, repo string) ([]contract.Diagnostic, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.DiagnosticsWithoutFile(ctx, repo)
}

func (r *recordingStore) DiagnosticsWithCodes(ctx context.Context, repo string, codes, prefixes []string) ([]contract.Diagnostic, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.DiagnosticsWithCodes(ctx, repo, codes, prefixes)
}

func (r *recordingStore) DiagnosticsWithMessage(ctx context.Context, repo, code, text string) ([]contract.Diagnostic, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.DiagnosticsWithMessage(ctx, repo, code, text)
}

func (r *recordingStore) NodesWithoutModelClaims(ctx context.Context, repo string, q store.Query) ([]contract.Node, error) {
	if err := r.next(); err != nil {
		return nil, err
	}
	return r.Store.NodesWithoutModelClaims(ctx, repo, q)
}

// NodesWithoutModelClaims passes through, like Diagnostics.
func (c *changingStore) NodesWithoutModelClaims(ctx context.Context, repo string, q store.Query) ([]contract.Node, error) {
	return c.Store.NodesWithoutModelClaims(ctx, repo, q)
}

// DiagnosticsWithCodes passes through, like Diagnostics.
func (c *changingStore) DiagnosticsWithCodes(ctx context.Context, repo string, codes, prefixes []string) ([]contract.Diagnostic, error) {
	return c.Store.DiagnosticsWithCodes(ctx, repo, codes, prefixes)
}

// DiagnosticsWithMessage passes through, like Diagnostics.
func (c *changingStore) DiagnosticsWithMessage(ctx context.Context, repo, code, text string) ([]contract.Diagnostic, error) {
	return c.Store.DiagnosticsWithMessage(ctx, repo, code, text)
}

// DiagnosticsWithoutFile passes through, like Diagnostics.
func (c *changingStore) DiagnosticsWithoutFile(ctx context.Context, repo string) ([]contract.Diagnostic, error) {
	return c.Store.DiagnosticsWithoutFile(ctx, repo)
}

// Diagnostics passes through; changingStore changes only the runs, and
// overriding every read keeps the wrapper's reads explicit.
func (c *changingStore) Diagnostics(ctx context.Context, repo string, files []string) ([]contract.Diagnostic, error) {
	return c.Store.Diagnostics(ctx, repo, files)
}

// TestAPIStoreFailures fails each store read of each endpoint in turn: every
// one answers 500 store_error with the envelope.
func TestAPIStoreFailures(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		for _, path := range []string{
			"/api/v0/api/endpoints?scope=all",
			"/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items"),
			"/api/v0/api/endpoint?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-tie"),
			"/api/v0/api/endpoint?id=" + url.QueryEscape("ipc_command:app/src/main.rs#main.COMMAND%20t016d_greet"),
			"/api/v0/api/calls?transport=http&status=dynamic,unmatched",
			"/api/v0/api/flow?scope=all&id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items"),
			"/api/v0/api/flow?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-tie"),
			"/api/v0/api/flow?scope=all&id=" + url.QueryEscape("ipc_command:app/src/main.rs#main.COMMAND%20t016d_items"),
			"/api/v0/source?id=" + url.QueryEscape("route:server/app.ts#GET%20%2Ft016d-items"),
		} {
			counter := &recordingStore{Store: s}
			if rec := get(t, testOptions(root, counter), http.MethodGet, path, nil); rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d", path, rec.Code)
			}
			if counter.reads < 4 {
				t.Fatalf("%s: only %d reads", path, counter.reads)
			}
			for n := 1; n <= counter.reads; n++ {
				rec := get(t, testOptions(root, &recordingStore{Store: s, fail: n}), http.MethodGet, path, nil)
				var env errorEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusInternalServerError || env.Error.Code != "store_error" || !strings.HasSuffix(env.Error.Message, errInjected.Error()) {
					t.Errorf("%s read %d: status %d %s", path, n, rec.Code, rec.Body.String())
				}
			}
		}
	})
}

// TestAPIConfigError keeps the data when graphvein.yaml is invalid: IDE
// links are dropped and the error is reported.
func TestAPIConfigError(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nide:\n  column_unit: bytes\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		var out callsResponse
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/api/calls?transport=http&status=dynamic", nil), &out)
		if out.IDE.Available || out.IDE.Error == nil || out.Total == 0 {
			t.Fatalf("ide %+v total %d", out.IDE, out.Total)
		}
		for _, c := range out.Calls {
			if c.Node.IDE != nil || c.Node.FileState == nil || *c.Node.FileState != "unchanged" {
				t.Errorf("%s: ide %v state %v", c.Node.Identity, c.Node.IDE, c.Node.FileState)
			}
		}
		// An edited file is reported as changed, a deleted one as missing.
		if err := os.WriteFile(filepath.Join(root, "web", "src", "http.ts"), []byte("// edited\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(root, "tools", "check.ts")); err != nil {
			t.Fatal(err)
		}
		decodeInto(t, get(t, testOptions(root, s), http.MethodGet, "/api/v0/api/calls?transport=http&status=dynamic,unmatched", nil), &out)
		states := map[string]string{}
		for _, c := range out.Calls {
			states[c.Node.File] = string(*c.Node.FileState)
		}
		if states["web/src/http.ts"] != "changed" || states["tools/check.ts"] != "missing" {
			t.Fatalf("file states %v", states)
		}
	})
}

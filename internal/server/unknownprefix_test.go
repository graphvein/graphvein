package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// The unknown-prefix surfaces (T033, rows UP-29..UP-33 of
// docs/contract/claims/unknown-prefix-paths.md, decision U6 for a Next.js
// basePath not read): the routes list, the endpoint detail with its UP
// callers, the flow, the architecture chain and links, on the matcher's
// fixture (chi and net/http routes below configured prefixes) and the
// Next.js fixture (a basePath read as not literal).

const (
	upOrders = "route:server/api.go#GET%20%2Fupb%2Forders%2F%7Bid%7D"
	upInner  = "route:server/api.go#GET%20%2Fupb%2Finner%2F%7Bname%7D"
	upSearch = "route:docs/app/api/search/route.js#GET%20%2Fapi%2Fsearch"
)

// upFixtureRoot copies a fixture directory of testdata/fixtures to a
// temporary directory.
func upFixtureRoot(t *testing.T, name string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("../../testdata/fixtures", name))); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestUnknownPrefixGoldens checks every unknown-prefix surface golden on
// both backends and that both backends serve the same bodies.
func TestUnknownPrefixGoldens(t *testing.T) {
	cases := []apiCase{
		{"unknown-prefix-endpoints", "up", "/api/v0/api/endpoints?transport=http&scope=all"},
		{"unknown-prefix-endpoint", "up", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape(upOrders)},
		{"unknown-prefix-endpoint-above", "up", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape(upInner)},
		{"unknown-prefix-flow", "up", flowPath(upOrders, "&scope=all")},
		{"unknown-prefix-chain", "up", "/api/v0/architecture/chain?direction=up&id=" + url.QueryEscape(upOrders)},
		{"unknown-prefix-architecture", "up", "/api/v0/architecture?scope=all"},
		{"unknown-prefix-next-endpoints", "next", "/api/v0/api/endpoints?transport=http&scope=all"},
		{"unknown-prefix-next-endpoint", "next", "/api/v0/api/endpoint?scope=all&id=" + url.QueryEscape(upSearch)},
	}
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		roots := map[string]string{"up": upFixtureRoot(t, "unknown-prefix"), "next": upFixtureRoot(t, "nextjs-routes")}
		options := map[string]Options{}
		for repo, root := range roots {
			indexRepository(t, root, w, repo)
			opt := testOptions(root, s)
			opt.Repository = repo
			options[repo] = opt
		}
		got := map[string]string{}
		for _, tc := range cases {
			rec := get(t, options[tc.repo], http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[tc.name] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots[tc.repo]))
			golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, roots[tc.repo])
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if a, b := bodies["sqlite"], bodies["postgres"]; a != nil && b != nil {
		for name, body := range a {
			if b[name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// TestUnknownPrefixRows checks the routes list rules directly: a route
// with only unknown-prefix paths has a null path, path_count 0, the
// prefix_unresolved flag instead of unmounted, its "…" form with the
// reason, and sorts after every full path; the counts of the view are the
// same as the rows say.
func TestUnknownPrefixRows(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := upFixtureRoot(t, "unknown-prefix")
		indexRepository(t, root, w, "up")
		opt := testOptions(root, s)
		opt.Repository = "up"
		rec := get(t, opt, http.MethodGet, "/api/v0/api/endpoints?transport=http&scope=all", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d %s", rec.Code, rec.Body.String())
		}
		var v struct {
			Transports []struct {
				EndpointCount int        `json:"endpoint_count"`
				NeverCalled   int        `json:"never_called"`
				Endpoints     []routeRow `json:"endpoints"`
			} `json:"transports"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if len(v.Transports) != 1 {
			t.Fatalf("transports = %d", len(v.Transports))
		}
		tr := v.Transports[0]
		seenPrefix, never := false, 0
		for _, r := range tr.Endpoints {
			if r.CallerCount == 0 {
				never++
			}
			if r.PrefixUnknown == nil {
				if seenPrefix && r.Path != nil {
					t.Errorf("%s: a full path sorts after an unknown-prefix path", r.ID)
				}
				continue
			}
			seenPrefix = true
			if r.Path != nil || r.PathCount != 0 {
				t.Errorf("%s: path %v, path_count %d, want null and 0", r.ID, r.Path, r.PathCount)
			}
			if !contains(r.Flags, "prefix_unresolved") || contains(r.Flags, "unmounted") {
				t.Errorf("%s: flags %v", r.ID, r.Flags)
			}
			if r.PrefixUnknown.Path == "" || r.PrefixUnknown.Path[:len("…")] != "…" && r.ID != upInner {
				t.Errorf("%s: prefix path %q", r.ID, r.PrefixUnknown.Path)
			}
			if r.PrefixUnknown.ReasonText == "" {
				t.Errorf("%s: no reason text", r.ID)
			}
		}
		if tr.EndpointCount != len(tr.Endpoints) || tr.NeverCalled != never {
			t.Errorf("counts: endpoint_count %d (rows %d), never_called %d (rows %d)", tr.EndpointCount, len(tr.Endpoints), tr.NeverCalled, never)
		}
		for _, r := range tr.Endpoints {
			if r.ID == upInner && (r.PrefixUnknown == nil || r.PrefixUnknown.Path != "/outer/…/upb/inner/{name}") {
				t.Errorf("above form: %+v", r.PrefixUnknown)
			}
		}
	})
}

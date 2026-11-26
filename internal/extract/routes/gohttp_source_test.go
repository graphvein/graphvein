package routes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goSourceRoute is one route of the fixture's compilable module
// (testdata/fixtures/go-routes/source) as the source oracle reads it
// (task T023): its claims and refusals, the handler
// that should answer, and the function that registers it.
type goSourceRoute struct {
	Identity          string        `json:"identity"`
	File              string        `json:"file"`
	Owner             string        `json:"owner"`
	Framework         string        `json:"framework"`
	Method            string        `json:"method"`
	Pattern           string        `json:"pattern"`
	Host              string        `json:"host,omitempty"`
	Handler           string        `json:"handler,omitempty"`
	HandlerExpression string        `json:"handler_expression,omitempty"`
	Paths             []goPathEntry `json:"paths"`
}

// goSourceRoutes links the go-routes fixture and returns the routes of its
// source module.
func goSourceRoutes(t *testing.T) []goSourceRoute {
	t.Helper()
	l := goRun(t, goFixtureSources(t))
	owners := map[string]string{}
	for _, e := range l.edges {
		if e.Kind == "contains" {
			owners[e.Target] = e.Source
		}
	}
	var out []goSourceRoute
	for _, n := range l.nodes {
		if !strings.HasPrefix(n.File, "source/") {
			continue
		}
		r := goSourceRoute{Identity: n.Identity, File: n.File, Owner: owners[n.Identity]}
		data, err := json.Marshal(n.Attributes)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &r); err != nil {
			t.Fatal(err)
		}
		if r.Paths == nil {
			r.Paths = []goPathEntry{}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity < out[j].Identity })
	return out
}

// TestGoRoutesSourceExport writes the source module's routes as JSON into
// GRAPHVEIN_GO_ROUTES_SOURCE_OUT (source-routes.json) for the opt-in
// source oracle (scripts/oracle/go_routes, TestGoRoutesSourceOracle), which
// compiles that module with the real routers and dispatches every claimed
// path and every refused route's own pattern.
func TestGoRoutesSourceExport(t *testing.T) {
	dir := os.Getenv("GRAPHVEIN_GO_ROUTES_SOURCE_OUT")
	if dir == "" {
		t.Skip("GRAPHVEIN_GO_ROUTES_SOURCE_OUT is not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(goSourceRoutes(t), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "source-routes.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestGoRoutesSourceModule checks, in the normal suite, that the source
// module's routes exist and that the review's escaped-router forms (A1)
// have no path; the dispatch itself is the opt-in source oracle's.
func TestGoRoutesSourceModule(t *testing.T) {
	routes := goSourceRoutes(t)
	if len(routes) < 80 {
		t.Fatalf("source module routes = %d, want at least 80", len(routes))
	}
	for _, r := range routes {
		if strings.HasPrefix(r.HandlerExpression, `tg.T("R`) && len(r.Paths) > 0 {
			t.Errorf("%s: escaped router route has paths %+v", r.Identity, r.Paths)
		}
	}
}

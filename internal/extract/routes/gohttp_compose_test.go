package routes

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/testscope"
)

// goRun links sources and returns the linker, for the composition graph.
func goRun(t *testing.T, sources map[string]string) *goLinker {
	t.Helper()
	raw := map[string][]byte{}
	for f, s := range sources {
		raw[f] = []byte(s)
	}
	files := goParse(t, raw)
	golang.Link(files, golang.ModulePaths(raw))
	return runGo(files, raw, testscope.Rules{}, nil)
}

// TestGoComposeMux checks GR-C3's outer-pattern rule: a hop is claimed
// only when its outer pattern matches every request of the composed child
// pattern (method, host and path).
func TestGoComposeMux(t *testing.T) {
	for _, c := range []struct {
		pattern string
		hops    []goHop
		path    string
		ok      bool
	}{
		{"GET /users", []goHop{{outer: "/api/", prefix: "/api"}}, "/api/users", true},
		{"GET /users", []goHop{{outer: "GET /api/", prefix: "/api"}}, "/api/users", true},
		{"HEAD /users", []goHop{{outer: "GET /api/", prefix: "/api"}}, "/api/users", true},
		{"/users", []goHop{{outer: "GET /api/", prefix: "/api"}}, "", false},
		{"POST /users", []goHop{{outer: "GET /api/", prefix: "/api"}}, "", false},
		{"GET /ptr", []goHop{{outer: "/ptr/", prefix: ""}}, "", false},
		{"GET /ptr/x", []goHop{{outer: "/ptr/", prefix: ""}}, "/ptr/x", true},
		{"GET /x", []goHop{{outer: "/x", prefix: ""}}, "/x", true},
		{"GET /h", []goHop{{outer: "example.com/h/", prefix: "/h"}}, "", false},
		{"GET example.com/h", []goHop{{outer: "example.com/h/", prefix: "/h"}}, "/h/h", true},
		{"GET /s", []goHop{{outer: "/a/", prefix: "/a"}, {outer: "/b/", prefix: "/b"}}, "/a/b/s", true},
		{"GET /s", []goHop{{outer: "/a/", prefix: "/a"}, {outer: "/c/", prefix: "/b"}}, "", false},
		{"GET /{id}", []goHop{{outer: "/a/{x}/", prefix: "/a/v"}}, "/a/v/{id}", true},
		{"GET /{id}", []goHop{{outer: "/a/{x}", prefix: "/a/v"}}, "", false},
		{"GET /files/{p...}", []goHop{{outer: "/f/", prefix: "/f"}}, "/f/files/{p...}", true},
		{"GET /{$}", []goHop{{outer: "/r/", prefix: "/r"}}, "/r/{$}", true},
		// StripPrefix("api", ...) matches no request path.
		{"GET /x", []goHop{{outer: "/", prefix: "api"}}, "", false},
		// The outer mux cleans "/v2//status" and redirects.
		{"GET /status", []goHop{{outer: "/v2/", prefix: "/v2/"}}, "", false},
	} {
		path, ok := goComposeMux(c.pattern, c.hops)
		if path != c.path || ok != c.ok {
			t.Errorf("%q through %v: %q %v, want %q %v", c.pattern, c.hops, path, ok, c.path, c.ok)
		}
	}
}

// TestGoJoinPaths checks gin's joinPaths (GR-C2).
func TestGoJoinPaths(t *testing.T) {
	for _, c := range [][3]string{
		{"/", "version", "/version"},
		{"/", "", "/"},
		{"/api", "", "/api"},
		{"/api", "/users/", "/api/users/"},
		{"/v1/", "users", "/v1/users"},
		{"/a", "../b", "/b"},
		{"/a", "/", "/a/"},
	} {
		if got := goJoinPaths(c[0], c[1]); got != c[2] {
			t.Errorf("joinPaths(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

// TestGoRoutesGraph checks the composition graph exported for the oracle:
// every route node is a registration of the graph with its paths, every
// registration with a path registers on an object some root reaches, and
// the fixture's composition forms appear as edges.
func TestGoRoutesGraph(t *testing.T) {
	l := goRun(t, goFixtureSources(t))
	g := l.routeGraph()
	nodes := map[string]contract.Node{}
	for _, n := range l.nodes {
		nodes[n.Identity] = n
	}
	regs := map[string]bool{}
	for _, r := range g.Registrations {
		regs[r.Identity] = true
		if _, ok := nodes[r.Identity]; !ok {
			t.Errorf("registration %s is no route node", r.Identity)
		}
		if len(r.Paths) == 0 {
			continue
		}
		reached := false
		for _, id := range r.Objects {
			reached = reached || len(l.graph.chains[id]) > 0
		}
		if !reached {
			t.Errorf("registration %s has paths but no root reaches its objects", r.Identity)
		}
	}
	for id := range nodes {
		if !regs[id] {
			t.Errorf("route node %s is no registration of the graph", id)
		}
	}
	roots := 0
	for _, o := range g.Objects {
		if o.Root {
			roots++
		}
	}
	want := map[string]bool{"mux /api/ /api": false, "mux /direct/ ": false, "chi /nested/": false, "chi /sub": false, "gin /v1/": false, "same": false, "blocked route_prefix_unresolved": false, "blocked invalid_pattern": false, "blocked regexp_param": false}
	for _, e := range g.Edges {
		switch {
		case e.Blocked != "":
			want["blocked "+e.Blocked] = true
		case e.Kind == "mux":
			want["mux "+e.Outer+" "+e.Prefix] = true
		case e.Kind == "same":
			want["same"] = true
		default:
			want[e.Kind+" "+e.Prefix] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("no edge %s", k)
		}
	}
	if roots == 0 || len(g.Registrations) != len(l.nodes) {
		t.Errorf("%d roots, %d registrations for %d route nodes", roots, len(g.Registrations), len(l.nodes))
	}
}

// TestGoRoutesStepIsolation checks that a failure while reading one file,
// composing from one root or composing one registration's paths becomes a
// go_routes_failed warning and never stops the other files, roots or
// registrations.
func TestGoRoutesStepIsolation(t *testing.T) {
	sources := goFixtureSources(t)
	baseline := goRun(t, sources)
	withPaths := func(l *goLinker, file string) int {
		n := 0
		for _, node := range l.nodes {
			if node.File == file && len(node.Attributes["paths"].([]goPathEntry)) > 0 {
				n++
			}
		}
		return n
	}
	for _, step := range []struct{ step, file string }{
		{"index", "chiapp/router.go"},
		{"file", "chiapp/router.go"},
		{"root", "chiapp/router.go"},
		{"paths", "chiapp/router.go"},
	} {
		t.Run(step.step, func(t *testing.T) {
			goTestFail = func(s, file string) {
				if s == step.step && file == step.file {
					panic("injected failure")
				}
			}
			defer func() { goTestFail = nil }()
			l := goRun(t, sources)
			failed := 0
			for _, d := range l.diagnostics {
				if d.Code == "go_routes_failed" && strings.Contains(d.Message, "injected failure") {
					failed++
				}
			}
			if failed == 0 {
				t.Fatalf("no go_routes_failed warning")
			}
			if withPaths(l, step.file) >= withPaths(baseline, step.file) {
				t.Errorf("%s: %d routes with a path, baseline %d", step.file, withPaths(l, step.file), withPaths(baseline, step.file))
			}
			for _, other := range []string{"ginapp/engine.go", "nethttp/compose.go", "chiapp/compose.go"} {
				if got, want := withPaths(l, other), withPaths(baseline, other); got != want {
					t.Errorf("%s: %d routes with a path, want %d", other, got, want)
				}
			}
		})
	}
}

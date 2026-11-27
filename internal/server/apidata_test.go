package server

import (
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestSourceRoot(t *testing.T) {
	stored := map[string]bool{
		"package.json":                   true,
		"web/package.json":               true,
		"web/packages/ui/package.json":   true,
		"web/packages/ui/pyproject.toml": true,
		"crates/app/Cargo.toml":          true,
		"py/setup.py":                    true,
		"py/pyproject.toml":              true,
	}
	noRoot := map[string]bool{"web/package.json": true}
	for _, tc := range []struct {
		file     string
		stored   map[string]bool
		root     string
		manifest string // "" means nil
	}{
		{"web/src/a.ts", stored, "web", "web/package.json"},                                     // own ancestor directory
		{"web/package.json", stored, "web", "web/package.json"},                                 // the manifest's own directory
		{"web/packages/ui/src/b.ts", stored, "web/packages/ui", "web/packages/ui/package.json"}, // nested: deepest wins, first name in byte order
		{"crates/app/src/main.rs", stored, "crates/app", "crates/app/Cargo.toml"},
		{"py/m.py", stored, "py", "py/pyproject.toml"}, // two names: byte order picks pyproject.toml before setup.py
		{"docs/a/b.ts", stored, ".", "package.json"},   // root manifest for a file without a deeper one
		{"a.ts", stored, ".", "package.json"},
		{"docs/a/b.ts", noRoot, "docs", ""}, // no manifest ancestor: first segment
		{"a.ts", noRoot, ".", ""},           // at the root without a root manifest
		{"tools/check.ts", map[string]bool{}, "tools", ""},
		{"tools/check.ts", map[string]bool{"tools/package.json": false}, "tools", ""}, // a manifest that is not stored does not count
		{".", stored, ".", ""},                                                        // synthetic node
	} {
		root, manifest := sourceRoot(tc.file, tc.stored)
		got := ""
		if manifest != nil {
			got = *manifest
		}
		if root != tc.root || got != tc.manifest {
			t.Errorf("sourceRoot(%q) = %q, %q; want %q, %q", tc.file, root, got, tc.root, tc.manifest)
		}
	}
}

func n(kind, id, file string, attrs map[string]any) contract.Node {
	return contract.Node{Identity: id, Kind: kind, File: file, Attributes: attrs}
}

func edgeTo(source, target string) contract.Edge {
	return contract.Edge{Kind: "calls_route", Source: source, Target: target}
}

func TestCountCoverage(t *testing.T) {
	routes := []contract.Node{
		n("route", "r1", "s.ts", map[string]any{}),
		n("route", "r2", "s.ts", map[string]any{}),
		n("route", "r3", "s.ts", map[string]any{"scope": "test"}),
	}
	calls := []contract.Node{
		n("client_call", "c1", "web/a.ts", map[string]any{"status": "linked"}),
		n("client_call", "c2", "web/a.ts", map[string]any{"status": "linked"}),                       // two edges to two routes
		n("client_call", "c3", "web/a.ts", map[string]any{"status": "linked"}),                       // linked without edge
		n("client_call", "c4", "docs/b.ts", map[string]any{"status": "dynamic"}),                     // edge from a non-linked call
		n("client_call", "c5", "docs/b.ts", map[string]any{}),                                        // no status
		n("client_call", "c6", "docs/b.ts", map[string]any{"status": 3.0}),                           // non-string status
		n("client_call", "c7", "web/a.test.ts", map[string]any{"status": "linked", "scope": "test"}), // test scope
		n("client_call", "c8", "web/a.ts", map[string]any{"status": "linked", "scope": "fixture"}),   // unknown scope
		n("client_call", "c9", "web/a.ts", map[string]any{"status": "LINKED"}),
	}
	edges := []contract.Edge{edgeTo("c1", "r1"), edgeTo("c2", "r1"), edgeTo("c2", "r2"), edgeTo("c4", "r2"), edgeTo("c7", "r3"), edgeTo("c8", "r1"), edgeTo("c9", "r1"),
		edgeTo("c1", "r1"), // a duplicate edge counts its caller once
		edgeTo("c1", "x"),  // an edge to a node that is not an endpoint is ignored
	}
	roots := newRootFinder(map[string]string{"web/package.json": "h"})

	got := countCoverage(routes, calls, edges, scopeProduction, nil, roots)
	wantCalls := callCounts{Total: 7, ByStatus: map[string]int{"linked": 3, "dynamic": 1, "": 2, "LINKED": 1}, LinkedWithoutEdge: 1, EdgesFromUnlinked: 2}
	if !reflect.DeepEqual(got.Calls, wantCalls) || len(got.Endpoints) != 2 || got.NeverCalled != 0 {
		t.Fatalf("production: %+v endpoints %d never %d", got.Calls, len(got.Endpoints), got.NeverCalled)
	}
	if !reflect.DeepEqual(got.Callers, map[string]int{"r1": 3, "r2": 2}) {
		t.Fatalf("callers %v", got.Callers)
	}
	sum := 0
	for _, r := range got.Roots {
		sum += r.Total
	}
	if sum != got.Calls.Total || len(got.Roots) != 2 || got.Roots[0].Root != "docs" || got.Roots[0].Manifest != nil || got.Roots[1].Root != "web" || *got.Roots[1].Manifest != "web/package.json" {
		t.Fatalf("roots %+v", got.Roots)
	}

	all := countCoverage(routes, calls, edges, scopeAll, nil, roots)
	if all.Calls.Total != 9 || all.Calls.UnknownScope != 1 || len(all.Endpoints) != 3 || all.Callers["r3"] != 1 || all.Callers["r1"] != 4 {
		t.Fatalf("all: %+v callers %v", all.Calls, all.Callers)
	}
	test := countCoverage(routes, calls, edges, scopeTest, nil, roots)
	if test.Calls.Total != 1 || len(test.Endpoints) != 1 || test.NeverCalled != 0 || test.Calls.UnknownScope != 0 {
		t.Fatalf("test: %+v", test)
	}

	// root filters counts and callers, not the per-root breakdown.
	docs := "docs"
	filtered := countCoverage(routes, calls, edges, scopeProduction, &docs, roots)
	if filtered.Calls.Total != 3 || filtered.Calls.EdgesFromUnlinked != 1 || filtered.Calls.LinkedWithoutEdge != 0 ||
		!reflect.DeepEqual(filtered.Callers, map[string]int{"r2": 1}) || filtered.NeverCalled != 1 || len(filtered.Roots) != 2 {
		t.Fatalf("root docs: %+v callers %v never %d", filtered.Calls, filtered.Callers, filtered.NeverCalled)
	}
	unknown := "nope"
	empty := countCoverage(routes, calls, edges, scopeProduction, &unknown, roots)
	if empty.Calls.Total != 0 || len(empty.Calls.ByStatus) != 0 || empty.NeverCalled != 2 || len(empty.Callers) != 0 || len(empty.Roots) != 2 {
		t.Fatalf("unknown root: %+v", empty)
	}
}

func TestLessRoot(t *testing.T) {
	roots := []string{"-x", ".", "a", "A"}
	want := []string{".", "-x", "A", "a"}
	got := append([]string{}, roots...)
	for i := range got {
		for j := i + 1; j < len(got); j++ {
			if lessRoot(got[j], got[i]) {
				got[i], got[j] = got[j], got[i]
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v", got)
	}
}

func TestDisplayPath(t *testing.T) {
	prod := map[string]any{"template": "/p"}
	test := map[string]any{"template": "/t", "scope": "test"}
	null := map[string]any{"template": nil}
	for _, tc := range []struct {
		paths []map[string]any
		scope string
		want  any
	}{
		{nil, scopeProduction, nil},
		{[]map[string]any{test, prod}, scopeProduction, "/p"},
		{[]map[string]any{test, prod}, scopeAll, "/p"},
		{[]map[string]any{prod, test}, scopeTest, "/t"},
		{[]map[string]any{test}, scopeProduction, "/t"}, // no production entry: the first
		{[]map[string]any{prod}, scopeTest, "/p"},
		{[]map[string]any{null}, scopeAll, nil},
		// T025: a FastAPI path's served trailing slash (ADR 0019); never on the root.
		{[]map[string]any{{"template": "/api/v1/items", "trailing_slash": true}}, scopeAll, "/api/v1/items/"},
		{[]map[string]any{{"template": "/", "trailing_slash": true}}, scopeAll, "/"},
	} {
		got := displayPath(tc.paths, tc.scope)
		if tc.want == nil && got != nil || tc.want != nil && (got == nil || *got != tc.want) {
			t.Errorf("%v %s: %v", tc.paths, tc.scope, got)
		}
	}
}

// TestRoutePathTestComposedOnly is the T057 precision correction: a path
// composed only through a test-scope mount is not shown as the route's
// path in the production or all scope while the route has a production
// path below an unknown prefix; that path is shown instead (row UP-29).
func TestRoutePathTestComposedOnly(t *testing.T) {
	test := map[string]any{"template": "/api/about", "scope": "test"}
	prod := map[string]any{"template": "/api/about"}
	entry := map[string]any{"template": "/api/about", "prefix": "unresolved", "prefix_reason": "route_mount_unresolved", "hop": "mount:router.go#SetRouter%20dispatcher"}
	testEntry := map[string]any{"template": "/x/api/about", "prefix": "unresolved", "prefix_reason": "route_prefix_unresolved", "scope": "test"}
	node := func(paths, entries []any) contract.Node {
		attrs := map[string]any{"paths": paths}
		if entries != nil {
			attrs["unresolved_prefix_paths"] = entries
		}
		return contract.Node{Identity: "route:router.go#GET%20%2Fabout", Kind: "route", Attributes: attrs}
	}
	for _, tc := range []struct {
		name         string
		n            contract.Node
		scope        string
		path, prefix any
	}{
		{"production view, unknown production prefix", node([]any{test}, []any{entry}), scopeProduction, nil, "…/api/about"},
		{"all view, unknown production prefix", node([]any{test}, []any{entry}), scopeAll, nil, "…/api/about"},
		{"test view keeps the test path", node([]any{test}, []any{entry}), scopeTest, "/api/about", nil},
		{"no production entry: unchanged", node([]any{test}, nil), scopeProduction, "/api/about", nil},
		{"only a test-scope entry: unchanged", node([]any{test}, []any{testEntry}), scopeProduction, "/api/about", nil},
		{"a production full path wins", node([]any{test, prod}, []any{entry}), scopeProduction, "/api/about", nil},
	} {
		got := routePath(tc.n, tc.scope)
		if tc.path == nil && got != nil || tc.path != nil && (got == nil || *got != tc.path) {
			t.Errorf("%s: path %v, want %v", tc.name, got, tc.path)
		}
		view := routePrefix(tc.n, tc.scope)
		if tc.prefix == nil && view != nil || tc.prefix != nil && (view == nil || view.Path != tc.prefix) {
			t.Errorf("%s: prefix %+v, want %v", tc.name, view, tc.prefix)
		}
	}
}

func TestRouteSortAndFlags(t *testing.T) {
	s := func(v string) *string { return &v }
	rows := []routeRow{
		{ID: "6", Path: nil, Pattern: s("/b")},
		{ID: "5", Path: nil, Pattern: s("/a")},
		{ID: "4", Path: s("/x"), Method: s("TRACE")},
		{ID: "3", Path: s("/x"), Method: s("BREW")},
		{ID: "2", Path: s("/x"), Method: s("DELETE")},
		{ID: "1b", Path: s("/x"), Method: s("GET")},
		{ID: "1a", Path: s("/x"), Method: s("GET")},
		{ID: "0", Path: s("/a"), Method: s("POST")},
		{ID: "7", Path: s("/x"), Method: nil},
	}
	sortRoutes(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.ID)
	}
	if want := []string{"0", "1a", "1b", "2", "3", "4", "7", "5", "6"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	cmds := []commandRow{{ID: "b", Command: nil}, {ID: "a", Command: s("z")}, {ID: "c", Command: s("a")}}
	sortCommands(cmds)
	if cmds[0].ID != "c" || cmds[2].ID != "b" {
		t.Fatalf("commands %+v", cmds)
	}

	route := n("route", "r", "a.ts", map[string]any{"handler": nil, "handler_inline": false,
		"paths": []any{map[string]any{"conditional": true}, map[string]any{"scope": "test"}}})
	if got := routeFlags(route, nil); !reflect.DeepEqual(got, []string{"conditional", "handler_unresolved", "test_paths"}) {
		t.Fatalf("route flags %v", got)
	}
	inline := n("route", "r", "a.ts", map[string]any{"handler_inline": true, "paths": []any{}})
	if got := routeFlags(inline, nil); !reflect.DeepEqual(got, []string{"handler_inline", "unmounted"}) {
		t.Fatalf("inline flags %v", got)
	}
	cmd := n("ipc_command", "c", "m.rs", map[string]any{"command": nil, "registry": "unproven", "cfg_gated": true, "test_only": true, "unreachable": true})
	if got := commandFlags(cmd); !reflect.DeepEqual(got, []string{"cfg_gated", "test_only", "unproven_registry", "unreachable", "unresolved_entry"}) {
		t.Fatalf("command flags %v", got)
	}
	if got := commandFlags(n("ipc_command", "c", "m.rs", map[string]any{"command": "x", "registry": "plugin"})); !reflect.DeepEqual(got, []string{"plugin_registry"}) {
		t.Fatalf("plugin flags %v", got)
	}
}

func TestNearMiss(t *testing.T) {
	ten := []any{}
	for i := 0; i < 10; i++ {
		ten = append(ten, string(rune('a'+i)))
	}
	path := func(status string, candidates []any, count float64) map[string]any {
		return map[string]any{"status": status, "candidates": candidates, "candidate_count": count}
	}
	call := func(status string, paths ...map[string]any) contract.Node {
		items := []any{}
		for _, p := range paths {
			items = append(items, p)
		}
		return n("client_call", "c", "a.ts", map[string]any{"status": status, "paths": items})
	}
	noCount := func(status string, candidates []any) map[string]any {
		return map[string]any{"status": status, "candidates": candidates}
	}
	for _, tc := range []struct {
		name string
		t    transport
		c    contract.Node
		want nearMissResult
	}{
		{"ambiguous listed", httpTransport, call("ambiguous", path("ambiguous", []any{"r", "s"}, 2)), nearMissResult{"ambiguous_candidate", 2, true, true, false}},
		{"test only one candidate", httpTransport, call("test_only", path("test_only", []any{"r"}, 0)), nearMissResult{"test_only_candidate", 1, true, true, false}},
		{"capped and listed", httpTransport, call("ambiguous", path("ambiguous", append([]any{"r"}, ten[:9]...), 12)), nearMissResult{"ambiguous_candidate", 12, true, true, false}},
		{"capped, not listed", httpTransport, call("ambiguous", path("ambiguous", ten, 12)), nearMissResult{capped: true}},
		{"no count at the cap, listed", httpTransport, call("ambiguous", noCount("ambiguous", append([]any{"r"}, ten[:9]...))), nearMissResult{"ambiguous_candidate", 10, false, true, false}},
		{"no count at the cap, not listed", httpTransport, call("ambiguous", noCount("ambiguous", ten)), nearMissResult{capped: true}},
		{"no count below the cap, not listed", httpTransport, call("ambiguous", noCount("ambiguous", ten[:3])), nearMissResult{}},
		{"dynamic with an ambiguous entry", httpTransport, call("dynamic", path("ambiguous", []any{"r", "s"}, 2), path("dynamic", []any{}, 0)), nearMissResult{"ambiguous_candidate", 2, true, true, false}},
		{"dynamic with a capped test-only entry", httpTransport, call("dynamic", path("test_only", ten, 12)), nearMissResult{capped: true}},
		{"unmatched", httpTransport, call("unmatched", path("unmatched", []any{"r"}, 0)), nearMissResult{}},
		{"linked entry of an ambiguous call", httpTransport, call("ambiguous", path("linked", []any{"r"}, 0)), nearMissResult{}},
		{"ipc listed", tauriTransport, n("ipc_call", "c", "a.ts", map[string]any{"status": "ambiguous", "candidates": []any{"r", "s"}}), nearMissResult{"ambiguous_candidate", 2, true, true, false}},
		{"ipc listed at the cap", tauriTransport, n("ipc_call", "c", "a.ts", map[string]any{"status": "ambiguous", "candidates": append([]any{"r"}, ten[:9]...)}), nearMissResult{"ambiguous_candidate", 10, false, true, false}},
		{"ipc capped", tauriTransport, n("ipc_call", "c", "a.ts", map[string]any{"status": "test_only", "candidates": ten}), nearMissResult{capped: true}},
		{"ipc dynamic", tauriTransport, n("ipc_call", "c", "a.ts", map[string]any{"status": "dynamic", "candidates": []any{"r"}}), nearMissResult{}},
	} {
		if got := nearMiss(tc.t, tc.c, "r"); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestLinkedMatch(t *testing.T) {
	c := n("client_call", "c", "a.ts", map[string]any{"paths": []any{
		map[string]any{"status": "linked", "candidates": []any{"r"}, "match": "suffix"},
		map[string]any{"status": "linked", "candidates": []any{"r"}, "match": "exact"},
		map[string]any{"status": "linked", "candidates": []any{"s"}, "match": "exact"},
	}})
	if got := linkedMatch(c, "r"); got == nil || *got != "exact" {
		t.Fatalf("match %v", got)
	}
	if got := linkedMatch(c, "x"); got != nil {
		t.Fatalf("unrelated match %v", *got)
	}
	ambiguous := n("client_call", "c", "a.ts", map[string]any{"paths": []any{map[string]any{"status": "ambiguous", "candidates": []any{"r"}, "match": nil}}})
	if got := linkedMatch(ambiguous, "r"); got != nil {
		t.Fatalf("ambiguous match %v", *got)
	}
}

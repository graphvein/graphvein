package index_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestTestScopeUnknownPrefix indexes the test-scope-unknown-prefix fixture
// (task T057, a precision correction of test scope): a gin router whose
// production mount is unproven (the engine is passed on after mounting) and
// that a test file builds at a known prefix. A production call that only
// the test-composed path matches, while the route's production path below
// the unknown prefix may match it, is unmatched, not test_only; a call no
// production path can match (a route only a test registers, or a known
// production path below the unknown prefix that differs) stays test_only;
// a test call still links to the test-composed path.
func TestTestScopeUnknownPrefix(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/test-scope-unknown-prefix")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	const (
		about = "route:server/router/api.go#GET%20%2Fabout"
		stats = "route:server/stats/stats.go#GET%20%2Fstats"
	)
	type want struct{ status, target string }
	expected := map[string]want{
		// Only the test-composed /api/about matches; the production
		// entry …/api/about may serve the call: unmatched.
		"web/src/api.ts:2": {status: "unmatched"},
		"web/src/api.ts:6": {status: "unmatched"},
		// A route only a test file registers: test_only stays.
		"web/src/api.ts:10": {status: "test_only"},
		// The production path …/admin/stats cannot be /stats: test_only
		// stays.
		"web/src/api.ts:14": {status: "test_only"},
		// The unknown-prefix tier links the production path (UP-19).
		"web/src/api.ts:18": {status: "linked", target: stats},
		// A test call ignores scope and links the test-composed path.
		"web/src/api.test.ts:2": {status: "linked", target: about},
	}
	targets := map[string]string{}
	var projected []any
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			targets[e.Source] = e.Target
			projected = append(projected, e)
		}
	}
	seen := map[string]bool{}
	for _, n := range snapshot.Nodes {
		switch n.Kind {
		case "route":
			projected = append(projected, fmt.Sprintf("%s scope=%s paths=%s entries=%s", n.Identity, compact(t, n.Attributes["scope"]), compact(t, n.Attributes["paths"]), compact(t, n.Attributes["unresolved_prefix_paths"])))
		case "client_call":
			projected = append(projected, n)
			key := fmt.Sprintf("%s:%d", n.File, n.Span.StartLine)
			w, ok := expected[key]
			if !ok {
				t.Errorf("%s: call without an expectation (%s)", key, n.Identity)
				continue
			}
			seen[key] = true
			if n.Attributes["status"] != w.status || targets[n.Identity] != w.target {
				t.Errorf("%s: status %v target %q, want %s %q", key, n.Attributes["status"], targets[n.Identity], w.status, w.target)
			}
		}
	}
	for key := range expected {
		if !seen[key] {
			t.Errorf("%s: expected call not emitted", key)
		}
	}
	golden.Check(t, "../../testdata/fixtures/test-scope-unknown-prefix.golden.json", projected)
}

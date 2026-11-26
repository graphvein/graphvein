package index_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestClientExactLiteralPrecisionGoldenAndReplay checks T017b: with an
// unresolved base, literal-free routes are never candidates (Rule E) and a
// suffix remainder must start with a literal or a parameter matched by a call
// parameter (Rule P). An exact match needs the same anchor (Revision 1).
// Resolved-base calls keep their literal-free and parameter-first exact links.
func TestClientExactLiteralPrecisionGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/client-exact-literal-precision")); err != nil {
		t.Fatal(err)
	}
	// Inert name (README): the literal-free routes stay out of the
	// self-index, where they would match other fixtures' calls.
	if err := os.Rename(filepath.Join(root, "fallback.ts.inert"), filepath.Join(root, "fallback.ts")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/client-exact-literal-precision.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	type link struct {
		target     string
		confidence float64
	}
	links := map[string]link{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			links[e.Source] = link{e.Target, e.Confidence}
		}
	}
	const call, route = "client_call:frontend.ts#", "route:backend.ts#"
	want := map[string]link{
		call + "main.GET%20%60%24%7Bprocess.env.API%7D%2F%24%7Btenant%7D%2Ft017b-reports%60": {route + "GET%20%2Ft017b-api%2F%3Atenant%2Ft017b-reports", 0.6},
		call + "main.GET%20%60%24%7Bprocess.env.API%7D%2Ft017b-items%60":                     {route + "GET%20%2Ft017b-api%2Ft017b-items", 0.6},
		call + "main.GET%20%60%24%7Bprocess.env.API%7D%2Ft017b-users%2F7%2Ft017b-edit%60":    {route + "GET%20%2Ft017b-users%2F%3Aid%2Ft017b-edit", 0.9},
		call + "resolved.GET%20%27%2Fhealth%27":                                              {"route:fallback.ts#GET%20%2F%3Aid", 0.9},
		call + "resolved.GET%20%27%2Faccount%2Ft017b-settings%27":                            {route + "GET%20%2F%3Aorg%2Ft017b-settings", 0.9},
		call + "resolved.POST%20%27%2Fanything%27":                                           {"route:fallback.ts#ANY%20%2A", 0.9},
	}
	if len(links) != len(want) {
		t.Fatal("calls_route edges", links)
	}
	for source, target := range want {
		if links[source] != target {
			t.Error(source, links[source], "want", target)
		}
	}
	warmData, warm, writes := emit(t, root, s, opt)
	if string(data) != string(warmData) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("exact literal precision replay differs", warm, writes)
	}
}

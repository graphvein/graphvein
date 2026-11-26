package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestClientSuffixPrecisionGoldenAndReplay checks T017a: suffix remainders of
// parameters or a catch-all alone never link, literal remainders still do.
func TestClientSuffixPrecisionGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/client-suffix-precision")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/client-suffix-precision.golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
	links := map[string]string{}
	for _, e := range snapshot.Edges {
		if e.Kind == "calls_route" {
			if e.Confidence != 0.6 {
				t.Error("suffix confidence", e)
			}
			links[e.Source] = e.Target
		}
	}
	want := map[string]string{
		"client_call:frontend.ts#main.GET%20%60%24%7Bprocess.env.API%7D%2Forders%2F42%60":       "route:backend.ts#GET%20%2Fapi%2Forders%2F%3Aid",
		"client_call:frontend.ts#main.GET%20%60%24%7Bprocess.env.API%7D%2Fassets%2Flogo.png%60": "route:backend.ts#ANY%20%2Fcdn%2Fassets%2F%2A",
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
		t.Fatal("suffix precision replay differs", warm, writes)
	}
}

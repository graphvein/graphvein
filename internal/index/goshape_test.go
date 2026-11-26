package index_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// goShapeSnapshot renders the shape attribute of every route of a
// snapshot (routes.ShapeLines), read back from the store.
func goShapeSnapshot(t *testing.T, s store.Snapshot) map[string][]string {
	t.Helper()
	out := map[string][]string{"shapes": {}, "diagnostics": {}}
	var nodes []string
	byID := map[string][]string{}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		v, ok := n.Attributes[routeshape.AttributeKey]
		if !ok {
			byID[n.Identity] = []string{unescape(n.Identity) + ": no shape"}
			nodes = append(nodes, n.Identity)
			continue
		}
		shape, err := routeshape.FromAttribute(v)
		if err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		byID[n.Identity] = routes.ShapeLines(unescape(n.Identity), shape)
		nodes = append(nodes, n.Identity)
	}
	sort.Strings(nodes)
	for _, id := range nodes {
		out["shapes"] = append(out["shapes"], byID[id]...)
	}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_shape") {
			out["diagnostics"] = append(out["diagnostics"], d.File+": "+d.Code+": "+d.Message)
		}
	}
	return out
}

// TestGoShapeGoldenAndReplay indexes the go-shape fixture (T029,
// docs/contract/claims/route-shape-go.md), checks the shapes read
// back from the store against the golden, and that a warm run replays
// every file and emits the same records.
func TestGoShapeGoldenAndReplay(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/go-shape")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/go-shape.golden.json", goShapeSnapshot(t, snapshot))
	// T029 W2 review: a handler-local type that shadows a package type
	// is not resolved, so it is no model use of the package type.
	for _, e := range snapshot.Edges {
		if e.Kind == routeshape.UsesModelEdge && (strings.Contains(e.Source, "%2Fshadow")) {
			t.Errorf("shadowed local type gives a model edge %s -> %s", e.Source, e.Target)
		}
	}
	found := false
	for _, e := range snapshot.Edges {
		found = found || e.Kind == routeshape.UsesModelEdge && e.Source == "route:ginapp/routes.go#GET%20%2Fusers%2F%3Aid"
	}
	if !found {
		t.Error("GET /users/:id has no uses_model edge")
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
}

// TestGoShapeDepthOption checks that the run's shape depth (index
// Options.ShapeDepth, --shape-depth) reaches the Go shape linker: at
// depth 2 the chain of GET /deep stops with depth_cap at depth 2.
func TestGoShapeDepthOption(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/go-shape")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	opt.ShapeDepth = 2
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range snapshot.Nodes {
		if n.Kind != "route" || !strings.HasSuffix(unescape(n.Identity), "#GET /deep") {
			continue
		}
		shape, err := routeshape.FromAttribute(n.Attributes[routeshape.AttributeKey])
		if err != nil {
			t.Fatal(err)
		}
		if shape.Follow.DepthLimit != 2 || len(shape.Follow.Chains) != 1 || shape.Follow.Chains[0].Stop != routeshape.StopDepthCap || shape.Follow.Chains[0].Depth != 2 {
			t.Fatalf("follow %+v", shape.Follow)
		}
		return
	}
	t.Fatal("no GET /deep route")
}

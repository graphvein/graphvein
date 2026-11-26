package index_test

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// axumShapeSnapshot returns the shape attribute of every route of a
// snapshot, read back from the store and checked by FromAttribute, and the
// route_shape diagnostics.
func axumShapeSnapshot(t *testing.T, s store.Snapshot) map[string]any {
	t.Helper()
	shapes := map[string]any{}
	for _, n := range s.Nodes {
		if n.Kind != "route" {
			continue
		}
		v, ok := n.Attributes[routeshape.AttributeKey]
		if !ok {
			shapes[unescape(n.Identity)] = "no shape"
			continue
		}
		if _, err := routeshape.FromAttribute(v); err != nil {
			t.Fatalf("%s: %v", n.Identity, err)
		}
		shapes[unescape(n.Identity)] = v
	}
	diagnostics := []string{}
	for _, d := range s.Diagnostics {
		if strings.HasPrefix(d.Code, "route_shape") {
			diagnostics = append(diagnostics, d.File+": "+d.Code+": "+d.Message)
		}
	}
	sort.Strings(diagnostics)
	return map[string]any{"shapes": shapes, "diagnostics": diagnostics}
}

func indexAxumShape(t *testing.T, fixture string, depth int) store.Snapshot {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/"+fixture)); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	opt.ShapeDepth = depth
	data, first, _ := emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	again, warm, writes := emit(t, root, s, opt)
	if string(data) != string(again) || warm.Parsed != 0 || warm.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("warm replay", warm, writes)
	}
	return snapshot
}

// TestAxumShapeGolden indexes the Axum shape fixtures (T029,
// docs/contract/claims/route-shape-axum.md) at the default depth and at
// depth 2, checks the shapes read back from the store against the
// goldens, and that a warm run replays every file.
func TestAxumShapeGolden(t *testing.T) {
	for _, c := range []struct {
		fixture, golden string
		depth           int
	}{
		{"route-shape-axum", "route-shape-axum.golden.json", 0},
		{"route-shape-axum", "route-shape-axum.depth2.golden.json", 2},
		{"route-shape-axum03", "route-shape-axum03.golden.json", 0},
		{"route-shape-axum-versions", "route-shape-axum-versions.golden.json", 0},
	} {
		t.Run(c.golden, func(t *testing.T) {
			golden.Check(t, "../../testdata/fixtures/"+c.golden, axumShapeSnapshot(t, indexAxumShape(t, c.fixture, c.depth)))
		})
	}
}

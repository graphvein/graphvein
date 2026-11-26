package index

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestShapeDepthResolution checks which call-following depth limit a run
// uses (routeshape row SM-21): the option when set, else graphvein.yaml
// index.shape_depth, else the default; an option out of range fails the
// run before anything is scanned.
func TestShapeDepthResolution(t *testing.T) {
	s, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	run := func(yaml string, option int) (Summary, error) {
		t.Helper()
		root := t.TempDir()
		if yaml != "" {
			if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte(yaml), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		var b bytes.Buffer
		opt := Options{Repository: "fixture", GeneratedAt: "2026-11-20T00:00:00Z", ShapeDepth: option, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "wazero")}}
		return Emit(context.Background(), root, s, &b, opt)
	}
	for _, c := range []struct {
		yaml   string
		option int
		want   int
	}{
		{"", 0, routeshape.DefaultDepth},
		{"version: 1\nindex:\n  shape_depth: 9\n", 0, 9},
		{"version: 1\nindex:\n  shape_depth: 9\n", 2, 2},
	} {
		sum, err := run(c.yaml, c.option)
		if err != nil || sum.ShapeDepth != c.want {
			t.Errorf("%q option %d: depth %d, want %d (%v)", c.yaml, c.option, sum.ShapeDepth, c.want, err)
		}
	}
	if _, err := run("", routeshape.MaxDepthLimit+1); err == nil {
		t.Error("depth above the limit accepted")
	}
}

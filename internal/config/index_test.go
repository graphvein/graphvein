package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexShapeDepth(t *testing.T) {
	load := func(body string) (Config, error) {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(root)
	}
	if c, err := load(""); err != nil || c.Index.ShapeDepth != DefaultShapeDepth {
		t.Fatalf("default: %+v %v", c.Index, err)
	}
	if c, err := Load(t.TempDir()); err != nil || c.Index.ShapeDepth != DefaultShapeDepth {
		t.Fatalf("no file: %+v %v", c.Index, err)
	}
	for depth, text := range map[int]string{1: "1", 10: "10", 7: "7"} {
		c, err := load("index:\n  shape_depth: " + text + "\n")
		if err != nil || c.Index.ShapeDepth != depth {
			t.Fatalf("%d: %+v %v", depth, c.Index, err)
		}
	}
	for body, want := range map[string]string{
		"index:\n  shape_depth: 0\n":                   "line 3: index.shape_depth must be an integer from 1 to 10",
		"index:\n  shape_depth: 11\n":                  "from 1 to 10",
		"index:\n  shape_depth: \"6\"\n":               "from 1 to 10",
		"index:\n  shape_depth: 6.5\n":                 "from 1 to 10",
		"index:\n  depth: 6\n":                         "unknown index key depth",
		"index: 6\n":                                   "index must be a mapping",
		"index:\n  shape_depth: 6\n  shape_depth: 7\n": "duplicate index key shape_depth",
	} {
		if _, err := load(body); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

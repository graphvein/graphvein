package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestExpressMountsGolden indexes the express-mounts fixture (T035):
// an Express app another app or router mounts with `use` answers
// only below the mount, so its routes get the composed paths and no path
// at its own root; an app mounted at a prefix that is not a constant
// keeps no path and stores the hop, one mounted by an object the linker
// does not read keeps no path, and an app only a test mounts keeps its
// own root.
func TestExpressMountsGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/express-mounts")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/express-mounts.golden.json", goRoutesSnapshot(t, snapshot))
}

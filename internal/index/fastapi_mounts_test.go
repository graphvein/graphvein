package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestFastAPIMountsGolden indexes the fastapi-mounts fixture (T035): a
// FastAPI app a Starlette mount consumes answers only below the mount, so
// its routes get the composed paths and no path at its own root; an app
// mounted at a prefix that is not a constant, through a Mount(...) in a
// routes list or on an object the linker does not read keeps no path; an
// app only a test mounts, or one a project class named Mount holds, keeps
// its own root.
func TestFastAPIMountsGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/fastapi-mounts")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/fastapi-mounts.golden.json", goRoutesSnapshot(t, snapshot))
}

package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestUnknownPrefixMountsGolden indexes the unknown-prefix-mounts fixture
// (T033, rows UP-5..UP-10 of
// docs/contract/claims/unknown-prefix-paths.md): pathless Express,
// FastAPI and Axum routes below a prefix that is not a constant store the
// paths proven below the hop in unresolved_prefix_paths; routes with a
// path, routes below no hop, unread patterns and paths not stored below a
// hop store none.
func TestUnknownPrefixMountsGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/unknown-prefix-mounts")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/unknown-prefix-mounts.golden.json", goRoutesSnapshot(t, snapshot))
}

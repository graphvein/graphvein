package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestAxumTrailingSlashGolden indexes the axum-trailing-slash fixture
// (T029 FU-3b): the same router under four axum requirements. Trailing
// slashes of patterns and nest prefixes are kept as axum 0.6.20 to 0.8.8
// serve them; for axum 0.3 and an unproven version those paths are not
// claimed (route_trailing_slash_unproven).
func TestAxumTrailingSlashGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/axum-trailing-slash")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/axum-trailing-slash.golden.json", goRoutesSnapshot(t, snapshot))
}

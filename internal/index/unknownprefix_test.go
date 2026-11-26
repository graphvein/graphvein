package index_test

import (
	"context"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
)

// TestUnknownPrefixGoGolden indexes the unknown-prefix-go fixture (T033,
// rows UP-1..UP-4 and UP-8..UP-10 of
// docs/contract/claims/unknown-prefix-paths.md): pathless Go routes below a
// configured chi, gin or net/http prefix or an unproven hop store the
// paths proven below the hop in unresolved_prefix_paths; routes with a
// path, routes below no hop, a StripPrefix holding template syntax and a
// StripPrefix at a serving position store none.
func TestUnknownPrefixGoGolden(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/unknown-prefix-go")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	emit(t, root, s, opt)
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	golden.Check(t, "../../testdata/fixtures/unknown-prefix-go.golden.json", goRoutesSnapshot(t, snapshot))
}

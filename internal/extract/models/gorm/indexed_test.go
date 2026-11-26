package gorm_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const indexedFixture = "../../../../testdata/fixtures/gorm-models"

// TestGormIndexedFixture indexes the GORM fixture through the indexer
// (the one call in internal/index/index.go) and checks the stored graph
// against testdata/fixtures/gorm-models.golden.json, then a warm replay
// that parses nothing and emits the same stream.
func TestGormIndexedFixture(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(indexedFixture)); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-09T00:00:00Z", Parser: parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	var first []byte
	for run := 0; run < 2; run++ {
		var stream bytes.Buffer
		summary, err := index.Emit(ctx, root, s, &stream, opt)
		if err != nil {
			t.Fatal(err)
		}
		data := append([]byte(nil), stream.Bytes()...)
		if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
			t.Fatal(err)
		}
		if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0) {
			t.Fatalf("replay parsed %d files or changed the stream", summary.Parsed)
		}
		first = data
		if run == 0 {
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			golden.Check(t, indexedFixture+".golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
		}
	}
}

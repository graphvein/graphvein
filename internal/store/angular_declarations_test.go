package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularDeclarationsRoundTrip(t *testing.T) {
	for _, fixture := range []string{"angular-declarations", "angular-declarations-shadow", "angular-injects", "angular-template-matching"} {
		t.Run(fixture, func(t *testing.T) {
			backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
				root := t.TempDir()
				if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/"+fixture)); err != nil {
					t.Fatal(err)
				}
				if fixture == "angular-declarations-shadow" {
					if err := os.Rename(filepath.Join(root, "core", "_package.json"), filepath.Join(root, "core", "package.json")); err != nil {
						t.Fatal(err)
					}
				}
				opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-25T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
				var first []byte
				for run := 0; run < 2; run++ {
					var b bytes.Buffer
					summary, err := index.Emit(context.Background(), root, s, &b, opt)
					if err != nil {
						t.Fatal(err)
					}
					data := append([]byte(nil), b.Bytes()...)
					writes, err := ingest.StreamWithWrites(context.Background(), &b, s)
					if err != nil {
						t.Fatal(err)
					}
					if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0) {
						t.Fatal("round trip changed Angular graph", summary, writes)
					}
					first = data
					snapshot, err := s.Dump(context.Background(), "fixture")
					if err != nil {
						t.Fatal(err)
					}
					golden.Check(t, "../../testdata/fixtures/"+fixture+".golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
				}
			})
		})
	}
}

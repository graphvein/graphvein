package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

func TestTauriCommandSyntaxRoundTrip(t *testing.T) {
	t.Run("tauri-command-syntax", func(t *testing.T) {
		backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
			ctx := context.Background()
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/tauri-command-syntax")); err != nil {
				t.Fatal(err)
			}
			opt := index.Options{Repository: "fixture", GeneratedAt: "2026-11-01T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			var first []byte
			var original map[string]facts.TauriSyntax
			for run := 0; run < 2; run++ {
				var stream bytes.Buffer
				summary, err := index.Emit(ctx, root, s, &stream, opt)
				if err != nil {
					t.Fatal(err)
				}
				data := append([]byte(nil), stream.Bytes()...)
				writes, err := ingest.StreamWithWrites(ctx, &stream, s)
				if err != nil {
					t.Fatal(err)
				}
				if run == 1 && (!bytes.Equal(first, data) || summary.Parsed != 0 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0) {
					t.Fatal("Tauri graph replay", summary, writes)
				}
				first = data
				snapshot, err := s.Dump(ctx, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				projection := map[string]facts.TauriSyntax{}
				for _, f := range snapshot.Files {
					if f.Language != "rust" {
						continue
					}
					data, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
					if err != nil {
						t.Fatal(err)
					}
					var cached facts.Facts
					if err := json.Unmarshal(data, &cached); err != nil || !cached.Tauri.Valid() {
						t.Fatal(f.Path, err)
					}
					if cached.Tauri != nil {
						projection[f.Path] = *cached.Tauri
					}
				}
				golden.Check(t, "../../testdata/fixtures/tauri-command-syntax.golden.json", projection)
				if run == 1 && !reflect.DeepEqual(original, projection) {
					t.Fatal("stored Tauri payload changed")
				}
				original = projection
			}
		})
	})
}

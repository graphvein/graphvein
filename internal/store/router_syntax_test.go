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
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularRouterSyntaxRoundTrip(t *testing.T) {
	t.Run("angular-router-syntax", func(t *testing.T) {
		backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
			ctx := context.Background()
			root := t.TempDir()
			if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-router-syntax")); err != nil {
				t.Fatal(err)
			}
			opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-27T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
			var first []byte
			var original map[string]typescript.RouterSyntax
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
					t.Fatal("router graph replay", summary, writes)
				}
				first = data
				snapshot, err := s.Dump(ctx, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				projection := map[string]typescript.RouterSyntax{}
				for _, f := range snapshot.Files {
					if f.Language != "typescript" {
						continue
					}
					data, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
					if err != nil {
						t.Fatal(err)
					}
					var cached typescript.CachedFacts
					if err := json.Unmarshal(data, &cached); err != nil || !cached.Router.Valid() {
						t.Fatal(f.Path, err)
					}
					projection[f.Path] = *cached.Router
				}
				golden.Check(t, "../../testdata/fixtures/angular-router-syntax.golden.json", projection)
				if run == 1 && !reflect.DeepEqual(original, projection) {
					t.Fatal("stored router payload changed")
				}
				original = projection
			}
		})
	})
}

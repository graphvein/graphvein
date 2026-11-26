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
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularTemplateSyntaxCache(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-templates")); err != nil {
			t.Fatal(err)
		}
		opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-24T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
		var before []byte
		var projection map[string]any
		for run := 0; run < 2; run++ {
			var stream bytes.Buffer
			sum, err := index.Emit(ctx, root, s, &stream, opt)
			if err != nil {
				t.Fatal(err)
			}
			data := append([]byte{}, stream.Bytes()...)
			writes, err := ingest.StreamWithWrites(ctx, &stream, s)
			if err != nil {
				t.Fatal(err)
			}
			if run == 1 && (!bytes.Equal(data, before) || sum.Parsed != 0 || sum.Reused != 2 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0) {
				t.Fatal(sum, writes)
			}
			before = data
			snapshot, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]any{}
			for _, f := range snapshot.Files {
				if f.Language != "html" && f.Language != "typescript" {
					continue
				}
				payload, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
				if err != nil {
					t.Fatal(err)
				}
				if f.Language == "html" {
					var v template.Facts
					if json.Unmarshal(payload, &v) != nil || !v.Valid() {
						t.Fatal("invalid HTML cache")
					}
					got[f.Path] = v
				} else {
					var v typescript.CachedFacts
					if json.Unmarshal(payload, &v) != nil || !v.TemplateSyntax.Valid() {
						t.Fatal("invalid TS cache")
					}
					got[f.Path] = *v.TemplateSyntax
				}
			}
			golden.Check(t, "../../testdata/fixtures/angular-templates.golden.json", got)
			if run == 1 && !reflect.DeepEqual(got, projection) {
				t.Fatal("fact cache round trip changed syntax")
			}
			projection = got
		}
	})
}

package typescript_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// modelFixtures are the payload fixtures: drizzle-syntax (version 1 rows,
// T018g2), sequelize-syntax (version 2 to 4 rows, T018g3) and typeorm-syntax
// (version 5 rows, T018g6).
var modelFixtures = []string{"../../../testdata/fixtures/drizzle-syntax", "../../../testdata/fixtures/sequelize-syntax", "../../../testdata/fixtures/typeorm-syntax"}

// TestDrizzleSyntaxFixture indexes each payload fixture and checks the graph
// golden (TypeScript symbol records only: the payload adds no record), the
// cached model payload of every file (payload.golden.json; files outside the
// gate have none) and a replay that reuses every cached payload.
func TestDrizzleSyntaxFixture(t *testing.T) {
	for _, fixture := range modelFixtures {
		t.Run(filepath.Base(fixture), func(t *testing.T) { checkModelFixture(t, fixture) })
	}
}

func checkModelFixture(t *testing.T, modelFixture string) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(modelFixture)); err != nil {
		t.Fatal(err)
	}
	// The payload golden is an expectation, not a fixture input.
	if err := os.Remove(filepath.Join(root, "payload.golden.json")); err != nil && !os.IsNotExist(err) {
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
		snapshot, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			golden.Check(t, modelFixture+".golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
		}
		payload := map[string]*typescript.ModelSyntax{}
		for _, f := range snapshot.Files {
			if f.Language != "typescript" && f.Language != "javascript" {
				continue
			}
			raw, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
			if err != nil {
				t.Fatal(err)
			}
			var cached typescript.CachedFacts
			if err := json.Unmarshal(raw, &cached); err != nil || cached.Exports == nil || (cached.Models != nil && !cached.Models.Valid()) {
				t.Fatalf("%s: cached facts %v", f.Path, err)
			}
			payload[f.Path] = cached.Models
		}
		golden.Check(t, filepath.Join(modelFixture, "payload.golden.json"), payload)
	}
}

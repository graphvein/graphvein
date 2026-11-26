package python_test

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
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const modelFixture = "../../../testdata/fixtures/sqlmodel-models"

// TestSQLModelFixture indexes the model fixture and checks the graph golden
// (Python symbol records and, since T018c2, the model tables it claims), the cached model payload of every
// file (payload.golden.json) and a replay that reuses every cached payload.
func TestSQLModelFixture(t *testing.T) {
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
		payload := map[string]*facts.PythonModels{}
		for _, f := range snapshot.Files {
			if f.Language != "python" {
				continue
			}
			raw, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
			if err != nil {
				t.Fatal(err)
			}
			var cached facts.Facts
			if err := json.Unmarshal(raw, &cached); err != nil || cached.Python == nil || !cached.Python.ModelsValid() {
				t.Fatalf("%s: cached facts %v", f.Path, err)
			}
			payload[f.Path] = cached.Python.Models
		}
		golden.Check(t, filepath.Join(modelFixture, "payload.golden.json"), payload)
	}
}

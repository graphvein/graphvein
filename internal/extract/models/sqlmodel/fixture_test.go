package sqlmodel_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const linkFixture = "../../../../testdata/fixtures/sqlmodel-link"

// TestModelLinkFixture indexes the linking fixture (cross-module chains,
// foreign keys, mutation sites, a migration set with an equal table name)
// and checks its graph golden, then a warm replay that parses nothing and
// emits the same stream.
func TestModelLinkFixture(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(linkFixture)); err != nil {
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
			golden.Check(t, linkFixture+".golden.json", store.Snapshot{Nodes: snapshot.Nodes, Edges: snapshot.Edges, Diagnostics: snapshot.Diagnostics})
			checkReasonProvenance(t, snapshot.Nodes)
		}
	}
}

// checkReasonProvenance checks T018f's reason attributes on every model
// table and column: each reason has a fact key (a string, or null for a
// reason about the whole column claim) and a non-empty file.
func checkReasonProvenance(t *testing.T, nodes []contract.Node) {
	t.Helper()
	seen := 0
	for _, n := range nodes {
		if n.Attributes["source"] != "model" {
			continue
		}
		list, ok := n.Attributes["reasons"].([]any)
		if !ok {
			t.Errorf("%s: reasons %v", n.Identity, n.Attributes["reasons"])
			continue
		}
		for _, r := range list {
			m, _ := r.(map[string]any)
			f, hasFact := m["fact"]
			if _, isString := f.(string); !hasFact || f != nil && !isString {
				t.Errorf("%s: reason without a fact: %v", n.Identity, r)
			}
			if file, _ := m["file"].(string); file == "" {
				t.Errorf("%s: reason without a file: %v", n.Identity, r)
			}
			seen++
		}
	}
	if seen == 0 {
		t.Error("the fixture has no model reason")
	}
}

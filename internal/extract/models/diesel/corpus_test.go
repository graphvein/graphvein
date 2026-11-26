package diesel

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// readTree reads every file under root as retained sources
// (repository-relative paths), skipping .git and target directories.
func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	sources := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "target" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		sources[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return sources
}

// coverage counts the claim pass's records of one group as the implementation
// coverage targets state them.
type coverage struct {
	Tables, PrimaryKeys, CompleteSets                  int
	Columns, Typed, Families, Nullability, ForeignKeys int
}

func countCoverage(nodes []contract.Node, set string) coverage {
	var c coverage
	for _, n := range nodes {
		a := n.Attributes
		switch n.Kind {
		case "table":
			if a["set"] != set {
				continue
			}
			c.Tables++
			if a["primary_key"] != nil {
				c.PrimaryKeys++
			}
			if a["column_set"] == "complete" {
				c.CompleteSets++
			}
		case "column":
			if !strings.HasPrefix(n.Identity, "column:"+strings.TrimPrefix(set, "models/diesel/")+"#") {
				continue
			}
			c.Columns++
			if a["type"] != nil {
				c.Typed++
			}
			if a["type_family"] != nil {
				c.Families++
			}
			if a["nullable"] != nil {
				c.Nullability++
			}
		}
	}
	return c
}

// TestDieselCorpus runs the claim pass over the checkout that
// GRAPHVEIN_DIESEL_CORPUS names (rust-lang/crates.io at
// 4e6ac5a46387d41656746507e0b6d7b5d863cddf) and checks the implementation coverage
// targets of the T018g4 task record (internal) as amended by Revision 1
// addendum 2 (O-a: categories, whose patch removes a column, keeps names
// only) and with the type-family types (Timestamptz, Timestamp, Numeric,
// ...) claimed as families (Revision 1 O4, ADR 0065). Skipped without it.
func TestDieselCorpus(t *testing.T) {
	root := os.Getenv("GRAPHVEIN_DIESEL_CORPUS")
	if root == "" {
		t.Skip("GRAPHVEIN_DIESEL_CORPUS is not set")
	}
	nodes, edges, diags := Link(Input{Sources: readTree(t, root)})
	for _, d := range diags {
		t.Logf("diagnostic %s %s: %s", d.Severity, d.Code, d.Message)
	}
	if out := os.Getenv("GRAPHVEIN_DIESEL_CORPUS_OUT"); out != "" {
		// The records, for a comparison with the oracle's catalog dump.
		data, err := json.Marshal(map[string]any{"nodes": nodes, "edges": edges})
		if err == nil {
			err = os.WriteFile(out, data, 0o644)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	fkEdges := 0
	for _, e := range edges {
		if e.Kind == "foreign_key" {
			fkEdges++
		}
	}
	main := countCoverage(nodes, "models/diesel/crates/crates_io_database/src/schema.rs")
	worker := countCoverage(nodes, "models/diesel/crates/crates_io_worker/src/schema.rs")
	t.Logf("configured %+v; foreign_key edges %d; worker %+v", main, fkEdges, worker)
	want := coverage{Tables: 36, PrimaryKeys: 34, CompleteSets: 34, Columns: 209, Typed: corpusTyped, Families: corpusFamilies, Nullability: 198, ForeignKeys: corpusForeignKeys}
	if got := (coverage{Tables: main.Tables, PrimaryKeys: main.PrimaryKeys, CompleteSets: main.CompleteSets, Columns: main.Columns, Typed: main.Typed, Families: main.Families, Nullability: main.Nullability, ForeignKeys: fkEdges}); got != want {
		t.Errorf("configured file coverage %+v, want %+v", got, want)
	}
	if worker.Tables != 1 || worker.Columns != 7 || worker.Typed != 0 || worker.Families != 0 || worker.Nullability != 0 {
		t.Errorf("worker schema.rs coverage %+v, want 1 table, 7 columns, names only", worker)
	}
}

const (
	corpusTyped       = 164
	corpusFamilies    = 34
	corpusForeignKeys = 29
)

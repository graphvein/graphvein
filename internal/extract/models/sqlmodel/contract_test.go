package sqlmodel

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/modeltest"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/parse"
)

// modelClaimRows maps every claim-contract row (T018c1's rows and T018c2's
// linking rows, docs/extract/schema-models.md "Claim contract") to the
// function that holds its claim branch. An empty function means the row has
// no claim branch: M0 is dropped at capture, I1 and I2 are after v1.
var modelClaimRows = map[string]string{
	"C1": "class", "C1b": "class", "C1c": "class", "C2": "buildTable", "C3": "class", "C4": "class",
	"C5": "buildTable", "C6": "buildTable", "N1": "buildTable", "N2": "buildTable", "N3": "buildTable",
	"M0": "", "M1": "buildTable", "M2": "fieldColumn", "M3": "fieldColumn", "M4": "fieldColumn",
	"M5": "fieldColumn", "M6": "fieldColumn", "M7": "buildTable", "M8": "buildTable", "M9": "buildTable",
	"M10": "buildTable", "M11": "buildTable", "M12": "buildTable",
	"A1": "annotation", "A2": "annotation", "A3": "annotation", "A5": "annotation", "A1m": "annotation",
	"F1": "fieldCall", "F2": "fieldCall", "F3": "fieldCall", "F3b": "fieldCall", "F4": "fieldCall",
	"F5": "setType", "F6": "fieldCall", "F7": "fieldCall", "F7r": "fieldCall", "F8": "fieldCall", "F9": "fieldCall",
	"K1": "columnCall", "K1b": "typeExpr", "K2": "columnCall", "K3": "columnCall", "K4": "columnCall",
	"K5": "columnCall", "K6": "columnCall", "T1": "typeExpr",
	"TA1": "tableArgs", "TA2": "tableArgs", "TA3": "tableArgs", "TA-D": "tableArgs",
	"U1": "class", "U2": "buildTable", "U3": "Link", "U4": "Link", "X1": "site", "U5": "site",
	"I1": "", "I2": "", "D": "class",
	"L1": "resolve", "L2": "class", "L3": "emit", "L4": "resolveForeignKeys", "L5": "compareMigrations",
	"L6": "Link", "L7": "buildTable", "L8": "site",
}

// modelRowForms lists the forms of contract rows that name several forms in
// one row; each form's branch tags "<row>/<form>", and the fixtures must
// reach every form (Rev 3: per-form granularity of L7's P1 list).
var modelRowForms = map[string][]string{
	"L7": {"F7r", "F4", "F9", "F5", "PK", "M12"},
	// Rev 3.1: what an X1 site may change beyond the set completeness;
	// Rev 3.2: attribute assignments that may rename the table or a column.
	"X1": {"PK", "FK", "replace", "table_attr", "table_identity", "column_identity"},
	// Rev 3.3 (G-2): a module-level name bound to a T1 call chain in the
	// Column type position (the column read, its type unknown).
	"K1b": {"alias"},
}

// tableRows reads the row lines of a Markdown file's "## Claim contract"
// table (the kit's shared helper).
func tableRows(t *testing.T, path string) map[string]string {
	t.Helper()
	return modeltest.TableRows(t, path)
}

func fixtureSources(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".py") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func parseAll(t *testing.T, sources map[string]string) (map[string]facts.Facts, map[string][]byte) {
	t.Helper()
	files, raw := map[string]facts.Facts{}, map[string][]byte{}
	var paths []string
	for f, s := range sources {
		raw[f] = []byte(s)
		paths = append(paths, f)
	}
	sort.Strings(paths)
	jobs := make([]parse.Job, len(paths))
	for i, f := range paths {
		jobs[i] = parse.Job{Language: "python", Source: raw[f]}
	}
	err := parse.Consume(context.Background(), jobs, parse.Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")}, func(i int, r parse.Result) {
		if r.Err != nil || r.Root == nil {
			t.Fatalf("%s: %v", paths[i], r.Err)
		}
		files[paths[i]] = python.Extract(paths[i], raw[paths[i]], r.Root)
	})
	if err != nil {
		t.Fatal(err)
	}
	return files, raw
}

// TestModelClaimContract keeps the code 1:1 with the claim contract: the
// row ids of T018c1's table and T018c2's linking rows equal the docs table
// and modelClaimRows, the docs name each row's claim function, and linking
// both fixtures emits exactly the rows that have a claim branch.
func TestModelClaimContract(t *testing.T) {
	task := tableRows(t, "../../../../docs/contract/claims/python-models-syntax.md")
	for id, row := range tableRows(t, "../../../../docs/contract/claims/python-models-linking.md") {
		task[id] = row
	}
	docs := tableRows(t, "../../../../docs/extract/schema-models.md")
	for id := range task {
		if _, ok := docs[id]; !ok {
			t.Errorf("row %s is in the tasks but not in docs/extract/schema-models.md", id)
		}
		if _, ok := modelClaimRows[id]; !ok {
			t.Errorf("row %s has no entry in modelClaimRows", id)
		}
	}
	for id := range docs {
		if _, ok := task[id]; !ok {
			t.Errorf("row %s is only in docs/extract/schema-models.md", id)
		}
	}
	modeltest.CheckDocs(t, docs, modelClaimRows, 2)
	for id := range modelClaimRows {
		if _, ok := task[id]; !ok {
			t.Errorf("modelClaimRows has %s, which no task row defines", id)
		}
	}

	tags := modeltest.NewTags()
	claimTag = tags.Tag
	defer func() { claimTag = nil }()
	for _, dir := range []string{"../../../../testdata/fixtures/sqlmodel-models", "../../../../testdata/fixtures/sqlmodel-link"} {
		files, raw := parseAll(t, fixtureSources(t, dir))
		migration := contract.Node{Type: "node", Identity: "table:migrations/0001_init.sql#public.team", Kind: "table", Name: "team", Attributes: map[string]any{"set": "migrations", "state": "complete"}}
		Link(Input{Python: files, Resolver: python.NewModuleResolver(files, raw), Sources: raw, Migrations: []contract.Node{migration}})
	}
	modeltest.CheckTags(t, tags, modelClaimRows, modelRowForms)
}

// TestModelAssumptions keeps the assumptions every model table states
// (Revision 3.3, O1: V1, V4, V6, V7) equal to the records section of
// docs/extract/schema-models.md.
func TestModelAssumptions(t *testing.T) {
	want := []string{"V1", "V4", "V6", "V7"}
	data, err := os.ReadFile("../../../../docs/extract/schema-models.md")
	if err != nil {
		t.Fatal(err)
	}
	if quoted := `["V1", "V4", "V6", "V7"]`; !strings.Contains(string(data), quoted) {
		t.Errorf("docs/extract/schema-models.md does not list %s", quoted)
	}
	nodes, _, _ := linkSources(t, map[string]string{"m.py": "from typing import Optional\nfrom sqlmodel import Field, SQLModel\nclass A(SQLModel, table=True):\n    id: Optional[int] = Field(default=None, primary_key=True)\n"})
	tables := 0
	for _, n := range nodes {
		if n.Kind != "table" {
			continue
		}
		tables++
		got, _ := n.Attributes["assumptions"].([]string)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: assumptions %v, want %v", n.Identity, got, want)
		}
	}
	if tables != 1 {
		t.Fatalf("%d tables", tables)
	}
}

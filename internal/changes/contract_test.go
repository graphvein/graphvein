package changes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// taskRows reads the row ids of the claim contract's tables from the task
// file, in order: G, C and D rows, then the I rows.
func taskRows(t *testing.T) (rows, relations []string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "contract", "claims", "change-impact.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "## Claim contract")
	end := len(text)
	if start < 0 {
		t.Fatal("claim contract section not found in the task file")
	}
	id := regexp.MustCompile(`(?m)^\| (G[0-9]+|C[0-9]+|D|I[0-9]+|ID) \|`)
	for _, m := range id.FindAllStringSubmatch(text[start:end], -1) {
		if strings.HasPrefix(m[1], "I") {
			relations = append(relations, m[1])
		} else {
			rows = append(rows, m[1])
		}
	}
	return rows, relations
}

// TestChangeClaimContract holds the code's contract 1:1 with the task
// (T019 "Claim contract"): the row and relation tables equal the task's;
// every C and D row has code (rowCodes) and every relation has a follow,
// a not_extracted answer or is ID; every item and path of every golden
// names an existing row or relation; and every row and relation is
// exercised by at least one golden, as are Revision 1's forms and Revision
// 4's I8 forms (fact, inferred and builds wordings, a column's table
// users, a "may also be used by" entry, a changed reads_table edge on its
// owner); no golden D item has a kind a row names.
func TestChangeClaimContract(t *testing.T) {
	wantRows, wantRelations := taskRows(t)
	var gotRows, gotRelations []string
	for _, r := range Rows {
		gotRows = append(gotRows, r.ID)
	}
	for _, r := range Relations {
		gotRelations = append(gotRelations, r.ID)
	}
	if strings.Join(gotRows, " ") != strings.Join(wantRows, " ") {
		t.Errorf("rows = %v, the task's = %v", gotRows, wantRows)
	}
	if strings.Join(gotRelations, " ") != strings.Join(wantRelations, " ") {
		t.Errorf("relations = %v, the task's = %v", gotRelations, wantRelations)
	}

	// Every C and D row has code, and no code names another row.
	for _, r := range Rows {
		_, has := rowCodes[r.ID]
		code := strings.HasPrefix(r.ID, "C") || r.ID == "D"
		if code != has {
			t.Errorf("row %s: code present %t, want %t", r.ID, has, code)
		}
	}
	for id, code := range rowCodes {
		if _, ok := rowOf(id); !ok {
			t.Errorf("rowCodes names %s, which the table does not list", id)
		}
		if code.nodes == nil && code.edges == nil && code.pass == nil {
			t.Errorf("row %s has no code", id)
		}
	}
	// Every relation has a follow, a not_extracted answer or is ID.
	followed := map[string]bool{"ID": true, "I11": true}
	// I12 and I13 follow project links (T022 B7), one link kind each.
	for kind, relation := range projectHops {
		if _, ok := relationOf(relation); !ok || !projectRelation[relation] {
			t.Errorf("project link kind %s follows %s, which the table does not list as a project relation", kind, relation)
		}
		followed[relation] = true
	}
	for relation := range projectRelation {
		found := false
		for _, r := range projectHops {
			found = found || r == relation
		}
		if !found {
			t.Errorf("project relation %s follows no link kind", relation)
		}
	}
	for _, f := range follows {
		if _, ok := relationOf(f.ID); !ok {
			t.Errorf("follows names %s, which the table does not list", f.ID)
		}
		followed[f.ID] = true
	}
	for _, r := range Relations {
		if !followed[r.ID] {
			t.Errorf("relation %s has no code", r.ID)
		}
	}

	// Coverage over the goldens.
	exercised := map[string]bool{}
	revision1 := map[string]bool{}
	revision4 := map[string]bool{}
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "fixtures", "change-impact.*.golden.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no change-impact goldens: %v", err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Summary Summary `json:"summary"`
			Items   []struct {
				Row    string   `json:"row"`
				Record string   `json:"record"`
				Kind   string   `json:"kind"`
				Labels []string `json:"labels"`
				Reason *string  `json:"reason"`
				Hints  []Hint   `json:"hints"`
			} `json:"items"`
			Impacts []struct {
				Impact ImpactResult `json:"impact"`
			} `json:"impacts"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, it := range doc.Items {
			if _, ok := rowOf(it.Row); !ok {
				t.Errorf("%s: item names row %q, which the table does not list", path, it.Row)
			}
			exercised[it.Row] = true
			// Revision 1: D keeps only the forms no row names; file
			// nodes, exposes_route and alters edges are C1, C3 and C10.
			if it.Row == "D" && namedKind(it.Record, it.Kind) {
				t.Errorf("%s: D item of kind %s, which a row names", path, it.Kind)
			}
			for _, l := range it.Labels {
				revision1[l] = true
				revision4[l] = true
			}
			if it.Reason != nil {
				revision1[*it.Reason] = true
			}
			for _, h := range it.Hints {
				exercised[h.Row] = true
			}
		}
		for _, g := range doc.Summary.NotExtracted {
			if g == GroupArchitecture {
				exercised["C13"] = true
			}
		}
		for _, im := range doc.Impacts {
			for _, p := range im.Impact.Paths {
				if n := len(p.Steps); n > 0 && p.Steps[n-1].Relation == "I8" {
					revision4["I8 "+p.Wording] = true
					if a := p.Steps[n-1].Access; a != nil && a.ColumnUse != nil {
						revision4["column_use "+*a.ColumnUse] = true
					}
				}
				for _, s := range p.Steps {
					if _, ok := relationOf(s.Relation); !ok {
						t.Errorf("%s: path step names relation %q, which the table does not list", path, s.Relation)
					}
					exercised[s.Relation] = true
				}
			}
			for _, ne := range im.Impact.Unknowns.NotExtracted {
				exercised[ne.Relation] = true
			}
			if len(im.Impact.Unknowns.Untraversed) > 0 {
				exercised["ID"] = true
			}
			for _, u := range im.Impact.TableUses {
				if len(u.MayAlsoBeUsedBy) > 0 {
					revision4[u.MayAlsoWording] = true
				}
			}
		}
	}
	var missing []string
	for _, r := range Rows {
		if (strings.HasPrefix(r.ID, "C") || r.ID == "D") && !exercised[r.ID] {
			missing = append(missing, r.ID)
		}
	}
	for _, r := range Relations {
		if !exercised[r.ID] {
			missing = append(missing, r.ID)
		}
	}
	// Revision 1's forms: a route's changed handler edges, a migration's
	// changed alters edges, a link lost with its removed call.
	for _, form := range []string{LabelHandlerChanged, LabelAltersChanged, ReasonCallAbsent} {
		if !revision1[form] {
			missing = append(missing, form)
		}
	}
	for _, form := range []string{"I8 reads this table", "I8 may read this table", "I8 may write this table",
		"I8 builds a query on this table", "column_use not_extracted", "may also be used by", LabelTableAccessChanged} {
		if !revision4[form] {
			missing = append(missing, form)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("rows and relations no golden exercises: %v", missing)
	}
}

// TestAccessCoverage holds Revision 4's item 3 per source group: I8 is
// not_extracted only for tables of a source no code-to-table row covers.
func TestAccessCoverage(t *testing.T) {
	table := func(attrs map[string]any) *contract.Node {
		return &contract.Node{Kind: "table", Attributes: attrs}
	}
	cases := []struct {
		name string
		node *contract.Node
		want bool
	}{
		{"sqlx Postgres migration set (CT-S1, CT-S2)", table(map[string]any{"dialect": "postgres", "set": "db/migrations"}), true},
		{"sqlx SQLite set (no_schema_source)", table(map[string]any{"dialect": "sqlite"}), false},
		{"sqlx MySQL set (no_schema_source)", table(map[string]any{"dialect": "mysql"}), false},
		{"migration set of unknown dialect", table(map[string]any{"dialect": nil}), false},
		{"SQLModel group (CT-P1..P5)", table(map[string]any{"source": "model", "tool": "sqlmodel"}), true},
		{"Prisma group (CT-R1..R3)", table(map[string]any{"source": "model", "tool": "prisma"}), true},
		{"Drizzle group (CT-Z1..Z4)", table(map[string]any{"source": "model", "tool": "drizzle"}), true},
		{"Sequelize group (no rows yet)", table(map[string]any{"source": "model", "tool": "sequelize"}), false},
		{"no node", nil, false},
		{"a column is no table", &contract.Node{Kind: "column", Attributes: map[string]any{"dialect": "postgres"}}, false},
	}
	for _, c := range cases {
		if got := AccessCovered(c.node); got != c.want {
			t.Errorf("%s: covered %t, want %t", c.name, got, c.want)
		}
	}
}

// TestAccessWording holds Revision 4's item 1 wording.
func TestAccessWording(t *testing.T) {
	cases := []struct {
		kind, provenance, role, want string
	}{
		{"reads_table", "fact", "executes", "reads this table"},
		{"writes_table", "fact", "executes", "writes this table"},
		{"reads_table", "inferred", "executes", "may read this table"},
		{"writes_table", "inferred", "executes", "may write this table"},
		{"reads_table", "fact", "builds", "builds a query on this table"},
		{"writes_table", "inferred", "builds", "builds a query on this table"},
	}
	for _, c := range cases {
		s := Step{Kind: c.kind, Provenance: c.provenance, Access: &TableAccess{Role: c.role}}
		if got := accessWording(s); got != c.want {
			t.Errorf("%s %s %s: %q, want %q", c.kind, c.provenance, c.role, got, c.want)
		}
	}
}

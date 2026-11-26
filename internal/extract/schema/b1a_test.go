package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// T018a2 B1a (Revision 1): unit cases for the classes P1, P2, P3, P10 and
// K2. The oracle regression cases under testdata/oracle/ pin the same
// rules against recorded Postgres verdicts; these cases pin the forms the
// oracle cases do not reach (manifests, keywords, enum label edits).

const pgManifest = "[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n"

// linkSQL links one Postgres set of the given migrations and returns the
// applied state per file ("true", "false" or "null") and the diagnostics.
func linkSQL(t *testing.T, files ...string) (map[string]string, []contract.Diagnostic) {
	t.Helper()
	sources := map[string][]byte{"Cargo.toml": []byte(pgManifest)}
	for i, sql := range files {
		sources[filepath.ToSlash(filepath.Join("migrations", string(rune('1'+i))+"_m.sql"))] = []byte(sql)
	}
	nodes, _, diagnostics := Link(Input{Sources: sources})
	applied := map[string]string{}
	for _, n := range nodes {
		if n.Kind != "migration" {
			continue
		}
		switch v := n.Attributes["applied"].(type) {
		case bool:
			if v {
				applied[filepath.Base(n.File)] = "true"
			} else {
				applied[filepath.Base(n.File)] = "false"
			}
		default:
			applied[filepath.Base(n.File)] = "null"
		}
	}
	return applied, diagnostics
}

func cites(diagnostics []contract.Diagnostic, id string) bool {
	for _, d := range diagnostics {
		if strings.Contains(d.Message, precondition(id)) {
			return true
		}
	}
	return false
}

func TestB1aNames(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  string // applied state of 1_m.sql
		cite  string // precondition the possible failure cites, or ""
	}{
		{"forward reference (K1)", []string{"CREATE TABLE a (b_id int REFERENCES b);\nCREATE TABLE b (id int PRIMARY KEY);\n"}, "null", "M3.P1"},
		{"outside table (E1)", []string{"CREATE TABLE a (b_id int REFERENCES outside_table);\n"}, "true", ""},
		{"renamed away", []string{"CREATE TABLE a (id int);\nALTER TABLE a RENAME TO c;\nINSERT INTO a DEFAULT VALUES;\n"}, "null", "N1.P1"},
		{"dropped", []string{"CREATE TABLE a (id int);\nDROP TABLE a;\nCOMMENT ON TABLE a IS 'x';\n"}, "null", "N1.P1"},
		{"generated shape", []string{"CREATE TABLE a (id int);\nDROP INDEX a_id_idx;\n"}, "null", "M7.P1"},
		{"held generated name", []string{"CREATE TABLE a (id int PRIMARY KEY);\nCOMMENT ON INDEX a_pkey IS 'x';\n"}, "true", ""},
		{"index where a table is needed", []string{"CREATE TABLE a (id int PRIMARY KEY);\nSELECT * FROM a_pkey;\n"}, "null", "N1.P1"},
		{"keyword as syntax", []string{"CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCREATE TABLE language (id int);\n"}, "true", ""},
		{"keyword as a name", []string{"SELECT * FROM language;\nCREATE TABLE language (id int);\n"}, "null", "N1.P1"},
		{"function created later", []string{"CREATE TABLE a (n int DEFAULT f());\nCREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\n"}, "null", "M1.P1"},
		{"schema created later", []string{"CREATE TABLE s.a (id int);\nCREATE SCHEMA s;\n"}, "null", "M1.P1"},
		// P2: a constraint index name taken by a generated name, and the
		// generated name of a renamed table's key keeps the old name.
		{"constraint name taken", []string{"CREATE TABLE a (id int PRIMARY KEY);\nCREATE TABLE b (id int, CONSTRAINT a_pkey PRIMARY KEY (id));\n"}, "null", "M1.P2"},
		{"rename to a taken name", []string{"CREATE TABLE a (id int PRIMARY KEY);\nALTER INDEX a_pkey RENAME TO a;\n"}, "null", "M8.P2"},
		{"old generated name kept", []string{"CREATE TABLE a (id int PRIMARY KEY);\nALTER TABLE a RENAME TO b;\nDROP INDEX a_pkey;\n"}, "null", ""},
		// K2: enum labels.
		{"existing label", []string{"CREATE TYPE e AS ENUM ('x');\nALTER TYPE e ADD VALUE 'x';\n"}, "null", "R3.K2"},
		{"existing label, IF NOT EXISTS", []string{"CREATE TYPE e AS ENUM ('x');\nALTER TYPE e ADD VALUE IF NOT EXISTS 'x' BEFORE 'nosuch';\n"}, "true", ""},
		{"missing neighbour", []string{"CREATE TYPE e AS ENUM ('x');\nALTER TYPE e ADD VALUE 'y' AFTER 'nosuch';\n"}, "null", "R3.K2"},
		{"rename to an existing label", []string{"CREATE TYPE e AS ENUM ('x', 'y');\nALTER TYPE e RENAME VALUE 'x' TO 'y';\n"}, "null", "R3.K2"},
		{"labels follow a rename", []string{"CREATE TYPE e AS ENUM ('x');\nALTER TYPE e RENAME TO f;\nALTER TYPE f ADD VALUE 'x';\n"}, "null", "R3.K2"},
		{"new type uses its label", []string{"CREATE TYPE e AS ENUM ('x');\nALTER TYPE e ADD VALUE 'y';\nCREATE TABLE a (s e DEFAULT 'y');\n"}, "true", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			applied, diagnostics := linkSQL(t, tc.files...)
			if applied["1_m.sql"] != tc.want {
				t.Fatalf("applied %v, want %s; %v", applied, tc.want, diagnostics)
			}
			if tc.cite != "" && !cites(diagnostics, tc.cite) {
				t.Fatalf("no diagnostic cites %s: %v", tc.cite, diagnostics)
			}
		})
	}
	// K2: a label added to a type of an earlier migration is unsafe to use
	// in the same transaction (55P04).
	applied, diagnostics := linkSQL(t, "CREATE TYPE e AS ENUM ('x');\n", "ALTER TYPE e ADD VALUE 'y';\nCREATE TABLE a (s e DEFAULT 'y');\n")
	if applied["1_m.sql"] != "true" || applied["2_m.sql"] != "null" || !cites(diagnostics, "R3.K2") {
		t.Fatalf("unsafe label use: %v %v", applied, diagnostics)
	}
}

// TestB1aConditional checks P3: CREATE TABLE IF NOT EXISTS after an opaque
// statement states no columns, and CREATE INDEX IF NOT EXISTS over a name
// that may be held states no index; a free name keeps both.
func TestB1aConditional(t *testing.T) {
	has := map[string]bool{}
	for _, sql := range []string{
		"DO $$ BEGIN NULL; END $$;\nCREATE TABLE IF NOT EXISTS a (id int);\n",
		"CREATE TABLE b (id int);\nCREATE INDEX ON b (id);\nCREATE INDEX ON b (id);\nCREATE INDEX IF NOT EXISTS b_id_idx1 ON b (id);\nCREATE INDEX IF NOT EXISTS b_free ON b (id);\n",
	} {
		nodes, _, _ := Link(Input{Sources: map[string][]byte{"Cargo.toml": []byte(pgManifest), "migrations/1_m.sql": []byte(sql)}})
		for _, n := range nodes {
			has[n.Kind+" "+n.QualifiedName] = true
		}
	}
	if has["column a.id"] {
		t.Error("CREATE TABLE IF NOT EXISTS after an opaque statement states its columns")
	}
	if has["db_index b.b_id_idx1"] || !has["db_index b.b_free"] {
		t.Errorf("CREATE INDEX IF NOT EXISTS: %v", has)
	}
}

// TestB1aSetReading checks P10: a member the pass could read and found not
// UTF-8 makes sqlx refuse the set (applied false, no facts); a member it
// could not read (here: valid SQL left out of the sources) makes every
// applied state unknown; manifests that are not UTF-8, that sit between a
// package and its workspace root, or that a renamed inherited sqlx key
// depends on, give no records.
func TestB1aSetReading(t *testing.T) {
	for _, tc := range []struct {
		name, member string
		want         string
	}{
		{"not utf-8", "CREATE TABLE \xff (id int);\n", "false"},
		{"left out of the sources", "CREATE TABLE u (id int);\n", "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "migrations"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "migrations", "2_member.sql"), []byte(tc.member), 0o644); err != nil {
				t.Fatal(err)
			}
			sources := map[string][]byte{"Cargo.toml": []byte(pgManifest), "migrations/1_m.sql": []byte("CREATE TABLE a (id int);\n")}
			nodes, _, diagnostics := Link(Input{Sources: sources, Root: root})
			for _, n := range nodes {
				if n.Kind == "table" && tc.want == "false" {
					t.Errorf("a refused set states table %s", n.QualifiedName)
				}
				if n.Kind != "migration" {
					continue
				}
				got := "null"
				if v, ok := n.Attributes["applied"].(bool); ok {
					got = map[bool]string{true: "true", false: "false"}[v]
				}
				if got != tc.want {
					t.Errorf("applied %s, want %s", got, tc.want)
				}
			}
			if !cites(diagnostics, "Q1.P10") {
				t.Errorf("no P10 diagnostic: %v", diagnostics)
			}
		})
	}
	if m := readCargo("Cargo.toml", []byte("[package]\nname = \"\xff\"\n")); m.readable {
		t.Error("a manifest that is not UTF-8 must be unreadable")
	}
	member := readCargo("ws/crates/a/Cargo.toml", []byte("[package]\nname = \"a\"\n[dependencies]\ndb = { workspace = true }\n"))
	manifests := map[string]manifest{
		"ws/Cargo.toml":          readCargo("ws/Cargo.toml", []byte("[workspace]\n[workspace.dependencies]\ndb = { package = \"sqlx\", features = [\"postgres\"] }\n")),
		"ws/crates/Cargo.toml":   readCargo("ws/crates/Cargo.toml", []byte("[package\n")),
		"ws/crates/a/Cargo.toml": member,
	}
	root := findWorkspaceRoot(member, manifests)
	if root == nil || root.path != "ws/crates/Cargo.toml" {
		t.Fatalf("the search must stop at the unreadable manifest: %+v", root)
	}
	if dep := readSqlx(member, root); !dep.found || dep.rootUnread == "" {
		t.Fatalf("renamed inherited key behind an unreadable root: %+v", dep)
	}
	sources := map[string][]byte{"Cargo.toml": []byte("[package\n"), "a/Cargo.toml": []byte("[package]\nname = \"a\"\n[dependencies]\ndb = { workspace = true }\n"),
		"a/migrations/1_m.sql": []byte("CREATE TABLE a (id int);\n")}
	nodes, _, diagnostics := Link(Input{Sources: sources})
	if len(nodes) != 0 || !cites(diagnostics, "Q1.P10") {
		t.Fatalf("no records and a P10 diagnostic expected: %v %v", nodes, diagnostics)
	}
}

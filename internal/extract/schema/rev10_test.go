package schema

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// TestSetDirectoryUnlisted checks P2 of the T018b1 review: a set directory
// whose listing fails for a reason other than absence is reported with
// schema_member_unreadable on the manifest, and the set's tables are
// partial; with no stored member the set has no record, only that
// diagnostic (so the API lists it in sets_without_records). An absent
// directory stays silent.
func TestSetDirectoryUnlisted(t *testing.T) {
	manifest := []byte("[package]\nname = \"a\"\n[dependencies]\nsqlx = { version = \"0.8\", features = [\"postgres\"] }\n")
	member := []byte("CREATE TABLE t (id integer);\n")
	for _, tc := range []struct {
		name      string
		err       error
		member    bool
		reported  bool
		tables    int
		partial   bool
		recordSet bool
	}{
		{"denied with a member", fs.ErrPermission, true, true, 1, true, true},
		{"denied without a member", fmt.Errorf("open migrations: %w", fs.ErrPermission), false, true, 0, false, false},
		{"absent", fs.ErrNotExist, false, false, 0, false, false},
		{"absent with a member", fs.ErrNotExist, true, false, 1, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := readDir
			defer func() { readDir = saved }()
			readDir = func(string) ([]os.DirEntry, error) { return nil, tc.err }
			sources := map[string][]byte{"Cargo.toml": manifest}
			if tc.member {
				sources["migrations/1_init.sql"] = member
			}
			nodes, _, diagnostics := Link(Input{Sources: sources, Root: t.TempDir()})
			reported := 0
			for _, d := range diagnostics {
				if d.Code == "schema_member_unreadable" && d.File == "Cargo.toml" && strings.Contains(d.Message, "could not be listed") {
					reported++
				}
			}
			if (reported == 1) != tc.reported || reported > 1 {
				t.Fatalf("listing diagnostics %d: %v", reported, diagnostics)
			}
			tables, migrations := 0, 0
			for _, n := range nodes {
				switch n.Kind {
				case "table":
					tables++
					if (n.Attributes["state"] == "partial") != tc.partial {
						t.Errorf("table %s state %v", n.Identity, n.Attributes["state"])
					}
				case "migration":
					migrations++
				}
			}
			if tables != tc.tables || (migrations > 0) != tc.recordSet {
				t.Fatalf("%d tables, %d migrations: %v", tables, migrations, nodes)
			}
		})
	}
}

// TestManifestShape checks R4 P2 in cargo.go: a value discovery reads that
// has the wrong type makes the manifest unreadable (reported with
// schema_manifest_unreadable) instead of a silent "no package" or "no
// sqlx".
func TestManifestShape(t *testing.T) {
	for _, tc := range []struct {
		manifest, problem string
	}{
		{"package = \"a\"\n", "package is not a table"},
		{"[[package]]\nname = \"a\"\n", "package is not a table"},
		{"[package]\nname = \"a\"\nworkspace = true\n", "package.workspace is not a string"},
		{"workspace = 1\n", "workspace is not a table"},
		{"[workspace]\ndependencies = \"x\"\n", "workspace.dependencies is not a table"},
		{"dependencies = \"sqlx\"\n[package]\nname = \"a\"\n", "dependencies is not a table"},
		{"dev-dependencies = [\"sqlx\"]\n[package]\nname = \"a\"\n", "dev-dependencies is not a table"},
		{"target = \"x\"\n[package]\nname = \"a\"\n", "target is not a table"},
		{"[package]\nname = \"a\"\n[target]\nunix = 1\n", "target.unix is not a table"},
		{"[package]\nname = \"a\"\n[target.unix]\ndependencies = 1\n", "target.unix.dependencies is not a table"},
		{"[package]\nname = \"a\"\nworkspace = \"..\"\n[dependencies]\nsqlx = \"0.8\"\n", ""},
	} {
		m := readCargo("Cargo.toml", []byte(tc.manifest))
		if m.readable != (tc.problem == "") || m.problem != tc.problem {
			t.Errorf("%q: readable %v, problem %q", tc.manifest, m.readable, m.problem)
		}
		if tc.problem == "" {
			continue
		}
		_, _, diagnostics := Link(Input{Sources: map[string][]byte{"Cargo.toml": []byte(tc.manifest)}})
		if len(diagnostics) != 1 || diagnostics[0].Code != "schema_manifest_unreadable" || !strings.Contains(diagnostics[0].Message, tc.problem) {
			t.Errorf("%q: diagnostics %v", tc.manifest, diagnostics)
		}
	}
}

// TestSelectInto checks P3 of the T018b1 review statement by statement, with
// the outcome a Postgres 17 cluster gave for each form (created, refused
// or no INTO clause; T018b1 summary R4): every INTO that is not INSERT
// INTO, MERGE INTO or a column label is unsupported; only the leftmost
// SELECT's INTO is certain, every other one may make the migration fail.
func TestSelectInto(t *testing.T) {
	for _, tc := range []struct {
		sql      string
		class    class
		mayFail  bool
		relation string
	}{
		{"SELECT 1 INTO t;", classUnsupported, false, "t"},
		{"SELECT INTO t;", classUnsupported, false, "t"},
		{"SELECT 1 INTO t5 UNION SELECT 2;", classUnsupported, false, "t5"},
		{"SELECT 1 a INTO t FOR UPDATE;", classUnsupported, false, "t"},
		{"(SELECT 1 AS a INTO t);", classUnsupported, false, "t"},
		{"((SELECT 1 INTO t));", classUnsupported, false, "t"},
		{"(SELECT 1 INTO t) UNION SELECT 2;", classUnsupported, false, "t"},
		{"(WITH x AS (SELECT 1) SELECT * INTO t FROM x);", classUnsupported, false, "t"},
		{"WITH RECURSIVE r(a) AS (SELECT 1 UNION SELECT a+1 FROM r WHERE a<3) SELECT * INTO t FROM r;", classUnsupported, false, "t"},
		{"SELECT 1 UNION SELECT 2 INTO t;", classUnsupported, true, "t"},
		{"SELECT * FROM (SELECT 1 INTO t) s;", classUnsupported, true, "t"},
		{"WITH x AS (SELECT 1 INTO t) SELECT * FROM x;", classUnsupported, true, "t"},
		{"SELECT 1 WHERE EXISTS (SELECT 1 INTO t);", classUnsupported, true, "t"},
		{"SELECT 1 a FROM (SELECT 1) s INTO t;", classUnsupported, true, "t"},
		{"SELECT 1 a ORDER BY 1 INTO t;", classUnsupported, true, "t"},
		{"(SELECT 1 a) INTO t;", classUnsupported, true, "t"},
		{"((SELECT 1) UNION SELECT 2 INTO t);", classUnsupported, true, "t"},
		{"VALUES (1) INTO t;", classUnsupported, true, "t"},
		// T018a2 B1b (P7): text gram.y rejects is a parse error (D).
		{"SELECT 1 INTO a INTO b;", classError, false, ""},
		{"SELECT INTO t DISTINCT a FROM s;", classError, false, ""},
		{"SELECT DISTINCT INTO t a FROM s;", classError, false, ""},
		{"SELECT * FROM x AS into;", classError, false, ""},
		{"INSERT INTO k SELECT 1 INTO t;", classError, false, ""},
		{"SELECT 1 AS into;", classNoEffect, false, ""},
		{"SELECT z.into FROM (SELECT 1 AS into) z;", classNoEffect, false, ""},
		{"SELECT 'INTO t', 1 AS \"into\" /* INTO t */;", classNoEffect, false, ""},
		{"WITH x AS (INSERT INTO k VALUES (1) RETURNING *) SELECT * FROM x;", classNoEffect, false, ""},
		{"WITH x AS (MERGE INTO k USING s ON true WHEN MATCHED THEN DELETE RETURNING k.*) SELECT * FROM x;", classNoEffect, false, ""},
	} {
		src := []byte(tc.sql)
		statements, failure := lexStatements(src)
		if failure != nil || len(statements) != 1 {
			t.Fatalf("%s: lexing %v %v", tc.sql, statements, failure)
		}
		a := analyze(statements[0], src, nil, false)
		relation := ""
		if len(a.relations) > 0 {
			relation = a.relations[0].q.name
		}
		if a.class != tc.class || (a.mayFail != "") != tc.mayFail || relation != tc.relation {
			t.Errorf("%s: class %v, mayFail %q, relation %q", tc.sql, a.class, a.mayFail, relation)
		}
	}
}

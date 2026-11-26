package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestRevisionFiveCases covers the re-check of Revision 4 (Revision 5):
// Postgres refuses DROP CONSTRAINT of a missing name without IF EXISTS
// (tablecmds.c 12644-12651), and sqlx stops the set at the failed
// migration. On a complete table an unwritten name is a constraint only
// when it is the exact generated name of an unnamed one. The probe cases
// (p1-p6, p13, p21) and the uncertain-name cases fail on the Revision 4
// code. Revision 7 supersedes the exact-name rule: constraint names are
// not tracked, so every DROP CONSTRAINT without IF EXISTS may fail and
// makes all key facts of its table unknown; the expectations below follow
// Revision 7, except where T018a2 B3 (C3, contract row M5g-1) holds every
// constraint name of a complete table exactly: a name it does not hold is
// then refused (the keys stay as they were, since the statement fails) and
// IF EXISTS of such a name changes nothing.
func TestRevisionFiveCases(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// Probes of the re-check.
		{"p1 a name no constraint has may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT t_zzz;
` + later, want{"state t": "partial", "state later": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil,
			"primary_key table t": []any{"id"}, "diag schema_migration_may_fail": true}},
		{"p2 a _check name without a CHECK may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT t_b_check;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "primary_key table t": []any{"id"}}},
		{"p3 a _fkey name without foreign keys may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT t_a_fkey;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "primary_key table t": []any{"id"}}},
		{"p4 another table's _pkey name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT x_pkey;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil,
			"primary_key table t": []any{"id"}, "primary_key column t.id": true}},
		{"p5 IF EXISTS of an unknown name stays applied", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_zzz;
` + later, want{"state t": "complete", "state later": "complete", "applied 1_a.sql": true, "applied 2_b.sql": true,
			"primary_key table t": []any{"id"}}},
		{"p6 an _excl name without an EXCLUDE may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_excl;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil,
			"primary_key table t": []any{"id"}, "unique column t.a": true}},
		{"p13 the generated foreign key name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE u DROP CONSTRAINT u_tid_fkey;
` + later, want{"state u": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "status ref u.tid": "unresolved",
			"diag schema_migration_may_fail": true}},
		{"p21 a numbered name while the base name is exact may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t DROP CONSTRAINT t_pkey1;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil,
			"primary_key table t": nil, "primary_key column t.id": nil}},

		// Revision 6: the generated names of CHECK and EXCLUDE constraints
		// are never exact, so these drops may fail even where Postgres's
		// name matches.
		{"a single-column CHECK name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int CHECK (a > 0 AND a < length('x')));
ALTER TABLE t DROP CONSTRAINT t_a_check;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "primary_key table t": nil}},
		{"a table-named CHECK name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, b int, CHECK (a > b));
ALTER TABLE t DROP CONSTRAINT t_check;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"a column name of a CHECK over two columns may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, b int, CHECK (a > b));
ALTER TABLE t DROP CONSTRAINT t_a_check;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"a CHECK whose column name is also used as a type name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, "text" int, CHECK (a::text <> ''));
ALTER TABLE t DROP CONSTRAINT t_a_check;
` + later, want{"applied 1_a.sql": nil}},
		// Revision 6: the partial table no longer exempts the second drop.
		{"a second drop of a generated name on a partial table may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_key;
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT t_a_key;
`, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"an EXCLUDE name over bare columns may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, b int, EXCLUDE USING gist (a WITH =, b WITH &&));
ALTER TABLE t DROP CONSTRAINT t_a_b_excl;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"an EXCLUDE over an expression may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, EXCLUDE USING gist ((a + 1) WITH =));
ALTER TABLE t DROP CONSTRAINT t_expr_excl;
` + later, want{"applied 1_a.sql": nil}},

		// Names fixed at creation; collisions and truncation.
		{"the old table name after a rename may fail", `-- FILE: 1_a.sql
CREATE TABLE x (id int PRIMARY KEY, a int UNIQUE);
ALTER TABLE x RENAME TO t;
ALTER TABLE t RENAME COLUMN a TO b;
ALTER TABLE t DROP CONSTRAINT x_a_key;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "primary_key table t": nil}},
		{"the new table name after a rename may fail", `-- FILE: 1_a.sql
CREATE TABLE x (id int PRIMARY KEY);
ALTER TABLE x RENAME TO t;
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, want{"applied 1_a.sql": nil, "primary_key table t": nil}},
		{"an index of the generated name makes the key name uncertain", `-- FILE: 1_a.sql
CREATE TABLE x (id int);
CREATE INDEX t_pkey ON x (id);
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, want{"applied 1_a.sql": nil, "primary_key table t": nil}},
		{"an opaque statement before the table makes its generated names uncertain", `-- FILE: 1_a.sql
DO $$ BEGIN NULL; END $$;
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, want{"applied 1_a.sql": nil}},
		{"a truncated generated name is uncertain", `-- FILE: 1_a.sql
CREATE TABLE table_name_of_sixty_characters_xxxxxxxxxxxxxxxxxxxxxxxxxxxxx (id int PRIMARY KEY);
ALTER TABLE table_name_of_sixty_characters_xxxxxxxxxxxxxxxxxxxxxxxxxxxxx DROP CONSTRAINT table_name_of_sixty_characters_xxxxxxxxxxxxxxxxxxxxxxxxxxx_pkey;
` + later, want{"applied 1_a.sql": nil}},
		{"INCLUDE columns make a unique key name uncertain", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, b int, UNIQUE (a) INCLUDE (b));
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, want{"applied 1_a.sql": nil}},
		{"two unnamed foreign keys of one base name are uncertain", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (tid int REFERENCES t, FOREIGN KEY (tid) REFERENCES t);
ALTER TABLE u DROP CONSTRAINT u_tid_fkey;
` + later, want{"applied 1_a.sql": nil}},
		{"a generated unique name forgets every key and may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE, b int UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "unique column t.a": nil, "unique column t.b": nil, "unique_keys table t": nil}},

		// Other object kinds without IF EXISTS.
		{"DROP COLUMN of a missing column is refused", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t DROP COLUMN nope;
` + later, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"DROP COLUMN IF EXISTS of a missing column stays applied", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t DROP COLUMN IF EXISTS nope;
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := linkCase(t, tc.sql)
			for q, w := range tc.want {
				got := l.fact(t, q)
				if fmt.Sprint(got) != fmt.Sprint(w) || !reflect.DeepEqual(normalize(got), normalize(w)) {
					t.Errorf("%s: got %#v, want %#v", q, got, w)
				}
			}
		})
	}
}

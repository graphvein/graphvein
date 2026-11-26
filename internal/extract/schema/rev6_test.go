package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestRevisionSixCases covers the re-check of Revision 5 (Revision 6). The
// generated names of unnamed CHECK and EXCLUDE constraints are never exact:
// Postgres names them after the columns of its parsed expression or index
// (heap.c AddRelationNewConstraints, indexcmds.c ChooseIndexColumnNames),
// and keywords, SQL value functions and INCLUDE columns make those
// unverifiable from the text. A rename of a relation written with a
// generated key name, under any qualification, makes that name uncertain,
// as does any statement that may rename constraints the pass does not see.
// On a partial table too, DROP CONSTRAINT of a name the table does not
// surely hold may fail. Every may-fail case below produced applied: true
// on the Revision 5 code. Revision 7 supersedes the exact-name rule: every
// DROP CONSTRAINT without IF EXISTS may fail, so the cases that pinned an
// exact name as a real drop now expect a possible failure.
func TestRevisionSixCases(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	mayFail := want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true}
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// Postgres: t_a_b_excl (INCLUDE columns are in the index name).
		{"q1 an EXCLUDE with INCLUDE columns may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int, b int, EXCLUDE USING btree (a WITH =) INCLUDE (b));
ALTER TABLE t DROP CONSTRAINT t_a_excl;
` + later, mayFail},
		// Postgres: t_d_check (year is a keyword of EXTRACT, not the column).
		{"q2 a CHECK using EXTRACT with a year column may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, year int, d date, CHECK (extract(year from d) > 2000));
ALTER TABLE t DROP CONSTRAINT t_check;
` + later, mayFail},
		// Postgres: t_a_check (END closes the CASE, not the column).
		{"q3 a CHECK using CASE with an end column may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, "end" int, CHECK (CASE WHEN a > 0 THEN true ELSE false END));
ALTER TABLE t DROP CONSTRAINT t_check;
` + later, mayFail},
		// Postgres: t_check (current_date is the SQL value function).
		{"q4 a CHECK using current_date with a current_date column may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, "current_date" date, CHECK (current_date > '2000-01-01'));
ALTER TABLE t DROP CONSTRAINT t_current_date_check;
` + later, mayFail},
		{"a CHECK name equal to the one Postgres generates still may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int CHECK (a > 0));
ALTER TABLE t DROP CONSTRAINT t_a_check;
` + later, want{"applied 1_a.sql": nil, "primary_key table t": nil}},
		// T018a2 B3 (C3, M5g-1): a name the set wrote on a complete table
		// is held exactly, so the drop applies (it might fail under
		// Revision 7).
		{"a written CHECK name is dropped exactly", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int CONSTRAINT a_positive CHECK (a > 0));
ALTER TABLE t DROP CONSTRAINT a_positive;
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true}},

		// q14: renames of a generated key name under any qualification.
		{"q14 a qualified ALTER INDEX rename of the generated primary key name", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER INDEX public.t_pkey RENAME TO t_pk;
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT t_pkey;
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"applied 1_a.sql": true, "applied 2_b.sql": nil, "applied 3_c.sql": nil, "diag schema_migration_may_fail": true}},
		{"a qualified ALTER TABLE rename of the generated unique name", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE);
ALTER TABLE public.t_a_key RENAME TO t_a_uq;
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, mayFail},
		{"a rename of another name still may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE INDEX t_id ON t (id);
ALTER INDEX public.t_id RENAME TO t_id2;
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, mayFail},

		// Partial tables: whether an unwritten name exists is unknown, so
		// a drop without IF EXISTS may fail unless the name is an exact
		// generated name the table still holds.
		{"an unqualified ALTER INDEX rename of the generated primary key name", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER INDEX t_pkey RENAME TO t_pk;
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT t_pkey;
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "applied 3_c.sql": nil}},
		// (Revision 8: 1_a may fail too, since generated names are never
		// exact (Revision 7) and the renamed index may not exist; it was
		// applied: true before.)
		{"DROP FUNCTION CASCADE may drop the CHECK its name names", `-- FILE: 1_a.sql
CREATE FUNCTION f(int) RETURNS bool LANGUAGE sql IMMUTABLE AS 'SELECT true';
CREATE TABLE t (id int, a int CHECK (f(a)));
DROP FUNCTION f(int) CASCADE;
ALTER TABLE t DROP CONSTRAINT t_a_check;
` + later, mayFail},
		{"an unwritten name on a partial table may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
DO $$ BEGIN NULL; END $$;
ALTER TABLE t DROP CONSTRAINT t_zzz;
` + later, mayFail},
		{"IF EXISTS on a partial table stays applied", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
DO $$ BEGIN NULL; END $$;
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_zzz;
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true}},
		{"an opaque statement after the table makes its generated names uncertain", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
DO $$ BEGIN NULL; END $$;
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, mayFail},
		{"RENAME CONSTRAINT makes the old generated name uncertain", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t RENAME CONSTRAINT t_pkey TO t_pk;
ALTER TABLE t DROP CONSTRAINT t_pkey;
` + later, mayFail},
		{"generated names a table holds may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE, b int UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_key;
ALTER TABLE t DROP CONSTRAINT t_b_key;
` + later, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true}},

		// Uncertain target table: the name may mean another table, which
		// may not hold the constraint.
		{"a written name on a table search_path may not mean may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int CONSTRAINT t_a_uq UNIQUE);
SET search_path TO app;
ALTER TABLE t DROP CONSTRAINT t_a_uq;
` + later, mayFail},
		// Revision 8: without ALTER TABLE IF EXISTS the name may mean no
		// table after the search_path change (was applied: true).
		{"IF EXISTS on a table search_path may not mean may fail without ALTER TABLE IF EXISTS", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int CONSTRAINT t_a_uq UNIQUE);
SET search_path TO app;
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_a_uq;
` + later, mayFail},
		{"IF EXISTS on a table search_path may not mean stays applied", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int CONSTRAINT t_a_uq UNIQUE);
SET search_path TO app;
ALTER TABLE IF EXISTS t DROP CONSTRAINT IF EXISTS t_a_uq;
RESET search_path;
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true}},

		{"a multi-column unnamed UNIQUE name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, b int, UNIQUE (a, b));
ALTER TABLE t DROP CONSTRAINT t_a_b_key;
` + later, mayFail},
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

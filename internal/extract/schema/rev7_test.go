package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestRevisionSevenCases covers the re-check of Revision 6 (Revision 7).
// Constraint names are not tracked any more: every DROP CONSTRAINT makes
// all key facts of each table it may name unknown, and without IF EXISTS
// the migration may fail. Postgres merges equal key constraints of one
// CREATE TABLE or one ALTER TABLE subcommand (parse_utilcmd.c 2084-2140);
// the pass now does too (F1). Every case marked "today" produced a wrong
// fact on the Revision 6 code (applied: true where Postgres may refuse, or
// a UNIQUE key Postgres never created).
func TestRevisionSevenCases(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	mayFail := func(extra want) want {
		w := want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true}
		for k, v := range extra {
			w[k] = v
		}
		return w
	}
	noKey := []any{}
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// F1: merged key constraints (today: an extra UNIQUE key).
		{"f1 PRIMARY KEY UNIQUE on one column keeps only the primary key", `-- FILE: 1_a.sql
CREATE TABLE t (a int PRIMARY KEY UNIQUE);
`, want{"unique_keys table t": noKey, "primary_key table t": []any{"a"}, "state t": "complete"}},
		{"f1 a UNIQUE before an equal PRIMARY KEY is dropped", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a, b), PRIMARY KEY (a, b));
`, want{"unique_keys table t": noKey, "primary_key table t": []any{"a", "b"}}},
		{"f1 an unnamed and a named equal UNIQUE give one key with the name", `-- FILE: 1_a.sql
CREATE TABLE t (a int, UNIQUE (a), CONSTRAINT u UNIQUE (a));
`, want{"unique_keys table t": []any{map[string]any{"constraint": "u", "columns": []any{"a"}}}}},
		{"f1 a named UNIQUE equal to the primary key is merged into it", `-- FILE: 1_a.sql
CREATE TABLE t (a int PRIMARY KEY, CONSTRAINT u UNIQUE (a));
`, want{"unique_keys table t": noKey, "primary_key table t": []any{"a"}}},
		{"f1 ADD COLUMN merges its own constraints", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ADD COLUMN a int PRIMARY KEY UNIQUE;
`, want{"unique_keys table t": noKey, "primary_key table t": []any{"a"}}},
		{"f1 WITH options are not compared", `-- FILE: 1_a.sql
CREATE TABLE t (a int UNIQUE WITH (fillfactor = 70), UNIQUE (a));
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}}}},
		// ALTER TABLE transforms each subcommand alone (tablecmds.c
		// ATParseTransformCmd 5567-5592), so these are not merged.
		{"f1 separate ALTER TABLE subcommands are not merged", `-- FILE: 1_a.sql
CREATE TABLE t (a int);
ALTER TABLE t ADD UNIQUE (a), ADD PRIMARY KEY (a);
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}}, "primary_key table t": []any{"a"}}},
		// Attributes Postgres compares keep both.
		{"f1 DEFERRABLE differs", `-- FILE: 1_a.sql
CREATE TABLE t (a int PRIMARY KEY, UNIQUE (a) DEFERRABLE);
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}}}},
		{"f1 INITIALLY DEFERRED differs", `-- FILE: 1_a.sql
CREATE TABLE t (a int, UNIQUE (a) DEFERRABLE, UNIQUE (a) DEFERRABLE INITIALLY DEFERRED);
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}, map[string]any{"constraint": nil, "columns": []any{"a"}}}}},
		{"f1 NULLS NOT DISTINCT differs", `-- FILE: 1_a.sql
CREATE TABLE t (a int, UNIQUE NULLS NOT DISTINCT (a), UNIQUE (a));
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}, map[string]any{"constraint": nil, "columns": []any{"a"}}}}},
		{"f1 INCLUDE differs", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a) INCLUDE (b), UNIQUE (a));
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a"}}, map[string]any{"constraint": nil, "columns": []any{"a"}}}}},
		{"f1 equal INCLUDE lists merge", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a) INCLUDE (b), CONSTRAINT u UNIQUE (a) INCLUDE (b));
`, want{"unique_keys table t": []any{map[string]any{"constraint": "u", "columns": []any{"a"}}}}},
		{"f1 column order differs", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a, b), UNIQUE (b, a));
`, want{"unique_keys table t": []any{map[string]any{"constraint": nil, "columns": []any{"a", "b"}}, map[string]any{"constraint": nil, "columns": []any{"b", "a"}}}}},
		{"f1 an unreadable INCLUDE list makes the merge unknown", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a) INCLUDE (b + 1), UNIQUE (a));
`, want{"unique_keys table t": nil, "state t": "partial", "unique column t.a": true}},

		// F1 probes with their drops (today: applied true).
		{"p1 PRIMARY KEY UNIQUE then DROP CONSTRAINT t_a_key may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int PRIMARY KEY UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, mayFail(want{"primary_key table t": []any{"a"}, "unique_keys table t": noKey})},
		{"p1b UNIQUE then PRIMARY KEY, DROP CONSTRAINT t_a_b_key may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, b int, UNIQUE (a, b), PRIMARY KEY (a, b));
ALTER TABLE t DROP CONSTRAINT t_a_b_key;
` + later, mayFail(nil)},
		{"p2 the unnamed duplicate's generated name may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, UNIQUE (a), CONSTRAINT u UNIQUE (a));
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, mayFail(nil)},
		{"p3 ALTER TABLE ADD UNIQUE, ADD PRIMARY KEY then a drop may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int);
ALTER TABLE t ADD UNIQUE (a), ADD PRIMARY KEY (a);
ALTER TABLE t DROP CONSTRAINT t_a_key;
` + later, mayFail(nil)},

		// F2: names renamed by statements the pass does not model (today:
		// applied true).
		{"p8b RENAME CONSTRAINT of a written name then DROP may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, CONSTRAINT u UNIQUE (a));
ALTER TABLE t RENAME CONSTRAINT u TO v;
ALTER TABLE t DROP CONSTRAINT u;
` + later, mayFail(nil)},
		{"q1 a DO block may rename the written name", `-- FILE: 1_a.sql
CREATE TABLE t (a int, CONSTRAINT u UNIQUE (a));
DO $$ BEGIN EXECUTE 'ALTER TABLE t RENAME CONSTRAINT u TO v'; END $$;
ALTER TABLE t DROP CONSTRAINT u;
` + later, mayFail(nil)},
		{"q2b ALTER INDEX public.u RENAME then DROP CONSTRAINT u may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, CONSTRAINT u UNIQUE (a));
ALTER INDEX public.u RENAME TO v;
ALTER TABLE t DROP CONSTRAINT u;
` + later, mayFail(nil)},
		{"q3 RENAME CONSTRAINT of a written CHECK then DROP may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, CONSTRAINT c CHECK (a > 0));
ALTER TABLE t RENAME CONSTRAINT c TO d;
ALTER TABLE t DROP CONSTRAINT c;
` + later, mayFail(nil)},
		{"q4 RENAME CONSTRAINT of a written foreign key then DROP may fail", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
CREATE TABLE t (a int, CONSTRAINT f FOREIGN KEY (a) REFERENCES p);
ALTER TABLE t RENAME CONSTRAINT f TO g;
ALTER TABLE t DROP CONSTRAINT f;
` + later, mayFail(want{"status ref t.a": "unresolved"})},
		{"q2 ALTER INDEX u RENAME then DROP CONSTRAINT u may fail", `-- FILE: 1_a.sql
CREATE TABLE t (a int, CONSTRAINT u UNIQUE (a));
ALTER INDEX u RENAME TO v;
ALTER TABLE t DROP CONSTRAINT u;
` + later, mayFail(nil)},

		// Rule 1: any name, any form.
		// T018a2 B3 (C3, M5g-1): on a complete table the written names are
		// exact, so DROP CONSTRAINT drops exactly the constraint named and
		// keeps the other keys (Revision 7 forgot every key).
		{"a written primary key name with IF EXISTS drops exactly that key", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY, a int UNIQUE, b int);
CREATE UNIQUE INDEX t_b ON t (b);
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pk;
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "state t": "complete", "primary_key table t": noKey,
			"primary_key column t.id": false, "unique column t.a": true, "unique column t.b": true,
			"nullable column t.id": false, "index t.t_b": true}},
		{"a written foreign key name drops exactly that foreign key", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
CREATE TABLE q (id int PRIMARY KEY);
CREATE TABLE t (a int CONSTRAINT f REFERENCES p, b int REFERENCES q);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT IF EXISTS f;
`, want{"applied 2_b.sql": true, "status ref t.a": nil, "status ref t.b": "linked", "state p": "complete",
			"primary_key table p": []any{"id"}}},
		{"IF EXISTS of a name a complete table does not hold changes nothing", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY, n int);
CREATE TABLE u (tid int REFERENCES t);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_n_check;
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "applied 3_c.sql": true, "state u": "complete", "status ref u.tid": "linked"}},
		{"incoming foreign keys of the only candidate key go with CASCADE", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY);
CREATE TABLE u (tid int REFERENCES t);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pk CASCADE;
`, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "state u": "partial", "status ref u.tid": nil, "primary_key table t": noKey}},
		// Revision 8: CREATE TABLE app.t may be refused (t may be in app),
		// so 1_a may fail (2_b was applied: true before Revision 8).
		{"every table the name may mean forgets its keys", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE app.t (id int PRIMARY KEY);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pkey;
`, want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "primary_key table t": nil, "primary_key table app.t": nil}},
		{"a table the name may mean only under another qualification forgets its keys", `-- FILE: 1_a.sql
CREATE TABLE app.t (id int PRIMARY KEY);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT t_pkey;
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"applied 2_b.sql": nil, "applied 3_c.sql": nil, "primary_key table app.t": nil}},
		{"an unknown table without IF EXISTS may fail", `-- FILE: 1_a.sql
ALTER TABLE nowhere DROP CONSTRAINT c;
` + later, mayFail(nil)},
		{"ADD PRIMARY KEY after an exact drop of the key", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pk;
ALTER TABLE t ADD PRIMARY KEY (a);
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "primary_key table t": []any{"a"}}},
		{"ADD PRIMARY KEY after a drop of a generated name may be refused", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pkey;
ALTER TABLE t ADD PRIMARY KEY (a);
` + later, mayFail(want{"primary_key table t": []any{"a"}})},
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

// TestRevisionSevenMergedNames checks the names Postgres gives merged key
// constraints (parse_utilcmd.c 2120-2128): the written name of a dropped
// duplicate goes to an unnamed survivor, the primary key first. The
// primary key's name is not emitted, so this reads the state.
func TestRevisionSevenMergedNames(t *testing.T) {
	for _, tc := range []struct {
		sql       string
		pk        string // "" when the primary key is unnamed or absent
		uniques   []string
		uniqueSet int
	}{
		{"CREATE TABLE t (a int PRIMARY KEY, CONSTRAINT u UNIQUE (a))", "u", nil, 0},
		{"CREATE TABLE t (a int, CONSTRAINT u UNIQUE (a), PRIMARY KEY (a))", "u", nil, 0},
		{"CREATE TABLE t (a int CONSTRAINT p PRIMARY KEY, CONSTRAINT u UNIQUE (a))", "p", nil, 0},
		{"CREATE TABLE t (a int, UNIQUE (a), CONSTRAINT u UNIQUE (a), CONSTRAINT w UNIQUE (a))", "", []string{"u"}, 1},
		{"CREATE TABLE t (a int, CONSTRAINT w UNIQUE (a), CONSTRAINT u UNIQUE (a))", "", []string{"w"}, 1},
		// Source order, not column-then-table order, picks the survivor.
		{"CREATE TABLE t (CONSTRAINT w UNIQUE (a), a int CONSTRAINT u UNIQUE)", "", []string{"w"}, 1},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			src := []byte(tc.sql + ";\n")
			sts, failure := lexStatements(src)
			if failure != nil || len(sts) != 1 {
				t.Fatalf("lexing %q", tc.sql)
			}
			ct := analyze(sts[0], src, nil, false).create
			if ct == nil {
				t.Fatalf("not a CREATE TABLE: %q", tc.sql)
			}
			tc0 := ct.constraints
			for _, def := range ct.columns {
				tc0 = mergeConstraints(columnConstraintSet(def), tc0)
			}
			got := (&state{}).mergeIndexConstraints(&table{}, tc0)
			pk := ""
			if len(got.primaryKeys) > 0 && got.primaryKeys[0].name != nil {
				pk = *got.primaryKeys[0].name
			}
			var uniques []string
			for _, u := range got.uniques {
				if u.name != nil {
					uniques = append(uniques, *u.name)
				}
			}
			if pk != tc.pk || len(got.uniques) != tc.uniqueSet || !reflect.DeepEqual(uniques, tc.uniques) {
				t.Fatalf("primary key %q, uniques %v (%d); want %q, %v (%d)", pk, uniques, len(got.uniques), tc.pk, tc.uniques, tc.uniqueSet)
			}
		})
	}
}

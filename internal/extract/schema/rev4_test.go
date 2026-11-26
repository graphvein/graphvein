package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestRevisionFourCases covers the third review's findings (Revision 4):
// each class rule with the reviewer's probes (e3, g3, c1d, c1c, c1g, g2,
// g1, d2, e4, e6, e5, c3d, d1, e8) and further forms of the same class.
// Every probe case failed on the Revision 3 code in at least one
// expectation.
func TestRevisionFourCases(t *testing.T) {
	type want map[string]any
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// WF1: foreign keys depend on the unique index they use.
		{"e3 DROP INDEX of a unique index a foreign key uses", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE UNIQUE INDEX t_a ON t (a);
CREATE TABLE u (id int, ta int REFERENCES t (a));
DROP INDEX t_a;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "state u": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "index t.t_a": true, "status ref u.ta": "linked"}},
		{"g3 the dependency survives ALTER INDEX RENAME", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE UNIQUE INDEX t_a ON t (a);
CREATE TABLE u (id int, ta int REFERENCES t (a));
ALTER INDEX t_a RENAME TO t_a2;
DROP INDEX t_a2;
`, want{"state t": "partial", "applied 1_a.sql": nil, "index t.t_a2": true}},
		{"c1d DROP INDEX CASCADE drops the foreign key", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE UNIQUE INDEX t_a ON t (a);
CREATE TABLE u (id int, ta int REFERENCES t (a));
DROP INDEX t_a CASCADE;
`, want{"state u": "partial", "references column u.ta": nil, "index t.t_a": false, "applied 1_a.sql": true}},
		{"DROP INDEX CASCADE with another key over the columns leaves the reference unresolved", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE);
CREATE UNIQUE INDEX t_a ON t (a);
CREATE TABLE u (id int, ta int REFERENCES t (a));
DROP INDEX t_a CASCADE;
`, want{"state u": "partial", "status ref u.ta": "unresolved"}},
		{"DROP CONSTRAINT of a written unique key a foreign key uses", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int CONSTRAINT t_a_uq UNIQUE);
CREATE TABLE u (id int, ta int REFERENCES t (a));
ALTER TABLE t DROP CONSTRAINT t_a_uq;
`, want{"state t": "partial", "applied 1_a.sql": nil}},
		{"c1c DROP CONSTRAINT of a generated primary key name CASCADE", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE t DROP CONSTRAINT t_pkey CASCADE;
`, want{"state t": "partial", "state u": "partial", "status ref u.tid": "unresolved", "primary_key table t": nil, "primary_key column t.id": nil, "nullable column t.id": false}},
		{"c1g DROP CONSTRAINT of a generated primary key name without CASCADE", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE t DROP CONSTRAINT t_pkey;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "state u": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		// Revision 7: constraint names are not tracked, so even a CHECK's
		// name makes the keys unknown and the references unresolved.
		{"a check name makes the keys unknown", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int CHECK (a > 0));
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE t DROP CONSTRAINT t_a_check;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "state u": "partial", "primary_key table t": nil, "status ref u.tid": "unresolved", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},

		// WF2: the index of a key cannot be dropped, even with CASCADE.
		{"g2 DROP INDEX of a generated primary key index name", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
DROP INDEX t_pkey;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil, "status ref u.tid": "linked"}},
		{"g1 DROP INDEX CASCADE of a generated unique constraint index name", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE);
CREATE TABLE u (id int, ta int REFERENCES t (a));
DROP INDEX t_a_key CASCADE;
`, want{"state t": "partial", "applied 1_a.sql": nil}},
		{"DROP INDEX of a written constraint name", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY);
DROP INDEX t_pk CASCADE;
`, want{"state t": "partial", "applied 1_a.sql": nil}},
		{"DROP INDEX of an unwritten name: unnamed unique indexes are no longer proven", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
CREATE UNIQUE INDEX ON t (a);
CREATE TABLE u (id int, ta int REFERENCES t (a));
DROP INDEX t_a_idx CASCADE;
`, want{"state t": "partial", "state u": "partial", "status ref u.ta": "unresolved", "unique column t.a": nil}},

		// WF3: unwritten DROP CONSTRAINT names make key facts unknown.
		{"d2 DROP CONSTRAINT t_pkey keeps NOT NULL", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT t_pkey;
`, want{"primary_key table t": nil, "primary_key column t.id": nil, "nullable column t.id": false, "unique column t.id": nil}},
		{"e4 an unwritten foreign key name unresolves the table's references", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
-- FILE: 2_b.sql
ALTER TABLE u DROP CONSTRAINT IF EXISTS u_tid_fkey;
`, want{"state u": "partial", "status ref u.tid": "unresolved", "state t": "complete", "primary_key table t": []any{"id"}, "applied 1_a.sql": true, "applied 2_b.sql": true}},
		{"an unwritten unique name makes unique_keys unknown", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int UNIQUE, b int CONSTRAINT t_b_uq UNIQUE);
ALTER TABLE t DROP CONSTRAINT t_a_key;
`, want{"unique_keys table t": nil, "unique column t.a": nil}},
		{"an unwritten name on a partial table may be any constraint", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t DROP CONSTRAINT whatever;
`, want{"primary_key table t": nil, "unique_keys table t": nil}},
		{"ADD PRIMARY KEY after an unwritten DROP CONSTRAINT may be refused", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY, a int);
ALTER TABLE t DROP CONSTRAINT t_pkey;
ALTER TABLE t ADD PRIMARY KEY (a);
`, want{"applied 1_a.sql": nil, "primary_key table t": []any{"a"}}},
		{"an uncertain DROP CONSTRAINT of a written key", `-- FILE: 1_a.sql
CREATE TABLE t (id int CONSTRAINT t_pk PRIMARY KEY);
SET search_path TO app;
ALTER TABLE t DROP CONSTRAINT t_pk;
`, want{"primary_key table t": nil}},

		// WF4: the partition key.
		{"e6 DROP COLUMN of a partition key column", `-- FILE: 1_a.sql
CREATE TABLE par (id int, k date) PARTITION BY RANGE (k);
ALTER TABLE par DROP COLUMN k;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state par": "partial", "column par.k": true, "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"ALTER COLUMN TYPE of a renamed partition key column", `-- FILE: 1_a.sql
CREATE TABLE par (id int, k date) PARTITION BY LIST (k);
ALTER TABLE par RENAME COLUMN k TO kk;
ALTER TABLE par ALTER COLUMN kk TYPE timestamptz;
`, want{"state par": "partial", "type column par.kk": "date", "applied 1_a.sql": nil}},
		{"other columns of a partitioned table can be dropped", `-- FILE: 1_a.sql
CREATE TABLE par (id int, k date, x int) PARTITION BY RANGE (k);
ALTER TABLE par DROP COLUMN x;
`, want{"state par": "complete", "column par.x": false}},

		// G-a, G-b.
		{"e5 circular INHERIT", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE child (b int) INHERITS (parent);
ALTER TABLE parent INHERIT child;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state parent": "partial", "state child": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"c3d circular INHERIT through an empty child", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE child () INHERITS (parent);
ALTER TABLE parent INHERIT child;
`, want{"applied 1_a.sql": nil}},
		{"INHERIT of an unrelated table is not a refusal", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE other (id int);
ALTER TABLE other INHERIT parent;
`, want{"applied 1_a.sql": true, "state other": "partial"}},
		{"d1 ADD GENERATED AS IDENTITY on a nullable column", `-- FILE: 1_a.sql
CREATE TABLE t (id int GENERATED ALWAYS AS IDENTITY, a int);
ALTER TABLE t ALTER COLUMN a ADD GENERATED ALWAYS AS IDENTITY;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"ADD GENERATED AS IDENTITY on a NOT NULL column is not a refusal", `-- FILE: 1_a.sql
CREATE TABLE t (a int NOT NULL);
ALTER TABLE t ALTER COLUMN a ADD GENERATED BY DEFAULT AS IDENTITY;
`, want{"state t": "partial", "applied 1_a.sql": true}},

		// G-c: ALTER COLUMN TYPE of a foreign key column.
		{"e8 ALTER COLUMN TYPE of a referenced column", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE t ALTER COLUMN id TYPE text;
-- FILE: 2_b.sql
CREATE TABLE later (id int);
`, want{"state t": "partial", "state u": "partial", "status ref u.tid": "unresolved", "applied 1_a.sql": nil, "applied 2_b.sql": nil}},
		{"ALTER COLUMN TYPE of a referencing column", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE u ALTER COLUMN tid TYPE bigint;
`, want{"state t": "complete", "state u": "complete", "status ref u.tid": "linked"}},
		// C2 (B2): numeric does not reference an integer key.
		{"ALTER COLUMN TYPE of a referencing column to an unverified pair", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE u (id int, tid int REFERENCES t (id));
ALTER TABLE u ALTER COLUMN tid TYPE numeric;
`, want{"state t": "partial", "state u": "partial", "status ref u.tid": "unresolved"}},
		// Revision 8: the reference starts with equal written types, since
		// int to bigint at creation is now a possible failure itself.
		{"ALTER COLUMN TYPE to the other side's written type keeps the reference", `-- FILE: 1_a.sql
CREATE TABLE t (id bigint PRIMARY KEY);
CREATE TABLE u (id int, tid bigint REFERENCES t (id));
ALTER TABLE u ALTER COLUMN tid TYPE BIGINT;
`, want{"state t": "complete", "state u": "complete", "status ref u.tid": "linked", "applied 1_a.sql": true}},

		// G-d: tables whose columns come from elsewhere.
		{"d3 LIKE, OF and PARTITION OF give partial table nodes", `-- FILE: 1_a.sql
CREATE TYPE pair AS (a int, b int);
CREATE TABLE src (id int);
CREATE TABLE l (LIKE src);
CREATE TABLE o OF pair;
CREATE TABLE par (id int, k date) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF par FOR VALUES FROM ('2020-01-01') TO ('2021-01-01');
`, want{"state l": "partial", "state o": "partial", "state p1": "partial", "state src": "complete", "applied 1_a.sql": true}},
		{"DROP TABLE of a partitioned table drops its partitions", `-- FILE: 1_a.sql
CREATE TABLE par (id int, k date) PARTITION BY RANGE (k);
CREATE TABLE p1 PARTITION OF par FOR VALUES FROM ('2020-01-01') TO ('2021-01-01');
DROP TABLE par;
`, want{"state par": "absent", "state p1": "absent", "applied 1_a.sql": true}},
		{"DROP TABLE CASCADE drops inheriting tables", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE child () INHERITS (parent);
DROP TABLE parent CASCADE;
`, want{"state parent": "absent", "state child": "absent", "applied 1_a.sql": true}},
		{"DROP TABLE CASCADE of the parent of an ALTER TABLE INHERIT child", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE child (id int);
ALTER TABLE child INHERIT parent;
DROP TABLE parent CASCADE;
`, want{"state child": "absent", "applied 1_a.sql": true}},
		{"DROP TABLE without CASCADE of a parent of a modeled child", `-- FILE: 1_a.sql
CREATE TABLE parent (id int);
CREATE TABLE child () INHERITS (parent);
DROP TABLE parent;
`, want{"state parent": "partial", "state child": "partial", "applied 1_a.sql": nil}},
		{"DROP TYPE of a typed table's type", `-- FILE: 1_a.sql
CREATE TYPE pair AS (a int, b int);
CREATE TABLE o OF pair;
DROP TYPE pair;
`, want{"state o": "partial", "applied 1_a.sql": nil}},
		{"DROP TYPE CASCADE drops a typed table", `-- FILE: 1_a.sql
CREATE TYPE pair AS (a int, b int);
CREATE TABLE o OF pair;
DROP TYPE pair CASCADE;
`, want{"state o": "absent"}},
		{"ATTACH PARTITION ties the partition to the parent", `-- FILE: 1_a.sql
CREATE TABLE par (id int, k date) PARTITION BY RANGE (k);
CREATE TABLE p1 (id int, k date);
ALTER TABLE par ATTACH PARTITION p1 FOR VALUES FROM ('2020-01-01') TO ('2021-01-01');
DROP TABLE par;
`, want{"state p1": "partial"}},

		// Own pass: a unique index that INCLUDEs a dropped column.
		{"DROP COLUMN of an INCLUDE column of a unique index a foreign key uses", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int, note text);
CREATE UNIQUE INDEX t_a ON t (a) INCLUDE (note);
CREATE TABLE u (id int, ta int REFERENCES t (a));
ALTER TABLE t DROP COLUMN note;
`, want{"state t": "partial", "applied 1_a.sql": nil}},
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

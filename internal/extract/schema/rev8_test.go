package schema

import (
	"fmt"
	"reflect"
	"testing"
)

// TestRevisionEightCases covers the re-check of Revision 7 (Revision 8).
// Principle: when a statement's success depends on a fact the pass does
// not know, the migration may fail (WF8). Foreign keys to a partial table
// without a proven key (W1: r1, r1b, r1c, r1d, r2, r5, n3) and every
// audited handler have a case that gave applied: true on the Revision 7
// code; the cases marked "guard" hold on both and pin forms Postgres
// accepts whatever the unknown fact is (IF EXISTS and IF NOT EXISTS
// forms, a proven key, a complete table).
func TestRevisionEightCases(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	mayFail := func(extra want) want {
		w := want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true}
		for k, v := range extra {
			w[k] = v
		}
		return w
	}
	applied := want{"applied 1_a.sql": true, "applied 2_b.sql": true, "diag schema_migration_may_fail": false}
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// W1: foreign keys to a partial table without a proven key.
		{"r1 a dropped primary key, then REFERENCES p", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
ALTER TABLE p DROP CONSTRAINT IF EXISTS p_pkey;
CREATE TABLE c (pid int REFERENCES p);
` + later, mayFail(want{"status ref c.pid": "unresolved", "state c": "partial"})},
		{"r1b a dropped primary key, then REFERENCES p (id)", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
ALTER TABLE p DROP CONSTRAINT IF EXISTS p_pkey;
CREATE TABLE c (pid int REFERENCES p (id));
` + later, mayFail(want{"status ref c.pid": "unresolved"})},
		{"r1c a dropped primary key, then ADD FOREIGN KEY", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
CREATE TABLE c (pid int);
ALTER TABLE p DROP CONSTRAINT IF EXISTS p_pkey;
ALTER TABLE c ADD FOREIGN KEY (pid) REFERENCES p (id);
` + later, mayFail(want{"status ref c.pid": "unresolved"})},
		{"r1d the drop in an earlier migration", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
ALTER TABLE p DROP CONSTRAINT IF EXISTS p_pkey;
-- FILE: 2_b.sql
CREATE TABLE c (pid int REFERENCES p);
-- FILE: 3_c.sql
CREATE TABLE later (id int);
`, want{"applied 1_a.sql": true, "applied 2_b.sql": nil, "applied 3_c.sql": nil, "status ref c.pid": "unresolved"}},
		{"r2 a LIKE table, then REFERENCES", `-- FILE: 1_a.sql
CREATE TABLE p (LIKE x);
CREATE TABLE c (pid int REFERENCES p (id));
` + later, mayFail(want{"status ref c.pid": "unresolved"})},
		{"an INHERITS table, then REFERENCES", `-- FILE: 1_a.sql
CREATE TABLE base (id int PRIMARY KEY);
CREATE TABLE p (extra int) INHERITS (base);
CREATE TABLE c (pid int REFERENCES p (id));
` + later, mayFail(nil)},
		{"r5 DROP INDEX under a changed search_path, then REFERENCES", `-- FILE: 1_a.sql
CREATE TABLE public.p (id int);
CREATE UNIQUE INDEX pi ON public.p (id);
SET search_path TO s, public;
DROP INDEX pi;
CREATE TABLE public.c (pid int REFERENCES public.p (id));
` + later, mayFail(want{"status ref public.c.pid": "unresolved"})},
		{"r5b the same with DROP INDEX IF EXISTS", `-- FILE: 1_a.sql
CREATE TABLE public.p (id int);
CREATE UNIQUE INDEX pi ON public.p (id);
SET search_path TO s, public;
DROP INDEX IF EXISTS pi;
CREATE TABLE public.c (pid int REFERENCES public.p (id));
` + later, mayFail(want{"status ref public.c.pid": "unresolved"})},
		// T018a2 B3 (C3): zz is no constraint of the complete table p, so
		// IF EXISTS changes nothing and the key stays proven.
		{"n3 DROP CONSTRAINT IF EXISTS of another name, then REFERENCES", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
ALTER TABLE p DROP CONSTRAINT IF EXISTS zz;
CREATE TABLE c (pid int REFERENCES p);
` + later, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "status ref c.pid": "linked"}},
		{"a reference after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
SET search_path TO app, public;
CREATE TABLE public.c (pid int REFERENCES p);
` + later, mayFail(want{"status ref public.c.pid": "unresolved"})},
		{"guard: a proven key on a partial table links", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
ALTER TABLE p ENABLE ROW LEVEL SECURITY;
CREATE TABLE c (pid int REFERENCES p);
` + later, mergeWant(applied, want{"status ref c.pid": "linked", "state p": "partial"})},
		{"guard: a proven key after an opaque statement links", `-- FILE: 1_a.sql
CREATE TABLE p (id int PRIMARY KEY);
DO $$ BEGIN NULL; END $$;
CREATE TABLE c (pid int REFERENCES p);
` + later, mergeWant(applied, want{"status ref c.pid": "linked"})},
		{"guard: a complete table without the key is a certain refusal", `-- FILE: 1_a.sql
CREATE TABLE p (id int);
CREATE TABLE c (pid int REFERENCES p (id));
` + later, mayFail(want{"reason c Postgres refuses": true})},
		{"guard: a table the set never creates stays outside the model", `-- FILE: 1_a.sql
CREATE TABLE c (pid int REFERENCES nowhere (id));
` + later, mergeWant(applied, want{"status ref c.pid": "unresolved"})},

		// Columns that may not exist (missing).
		{"CREATE INDEX on a column a partial table may lack", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE INDEX ON t (zz);
` + later, mayFail(nil)},
		{"ALTER COLUMN of a column a partial table may lack", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ALTER COLUMN zz SET NOT NULL;
` + later, mayFail(nil)},
		{"a constraint on a column a partial table may lack", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ADD UNIQUE (zz);
` + later, mayFail(nil)},
		{"guard: DROP COLUMN IF EXISTS on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t DROP COLUMN IF EXISTS zz;
` + later, applied},

		// Keys and names a partial table may already hold.
		{"ADD PRIMARY KEY on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ADD PRIMARY KEY (id);
` + later, mayFail(want{"primary_key table t": []any{"id"}})},
		{"ADD COLUMN with a primary key on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ADD COLUMN IF NOT EXISTS k int PRIMARY KEY;
` + later, mayFail(nil)},
		{"guard: ADD PRIMARY KEY on a complete table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ADD PRIMARY KEY (id);
` + later, applied},
		{"ADD COLUMN on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ADD COLUMN a int;
` + later, mayFail(want{"column t.a": true})},
		{"guard: ADD COLUMN IF NOT EXISTS on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ADD COLUMN IF NOT EXISTS a int;
` + later, applied},
		{"RENAME COLUMN on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t RENAME COLUMN a TO b;
` + later, mayFail(want{"column t.b": true})},
		{"RENAME TO a name a table under another qualification may hold", `-- FILE: 1_a.sql
CREATE TABLE app.u (id int);
CREATE TABLE t (id int);
ALTER TABLE t RENAME TO u;
` + later, mayFail(nil)},
		{"RENAME TO a name an uncertain relation may hold", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE VIEW v AS SELECT 1 AS x;
DO $$ BEGIN NULL; END $$;
ALTER TABLE t RENAME TO v;
` + later, mayFail(nil)},
		{"SET SCHEMA to a name a table may hold", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE TABLE IF NOT EXISTS b.t (id int);
ALTER TABLE b.t SET SCHEMA app;
` + later, mayFail(nil)},
		{"ALTER INDEX RENAME TO a name a table under another qualification may hold", `-- FILE: 1_a.sql
CREATE TABLE app.u (id int);
CREATE TABLE t (id int);
CREATE INDEX ti ON t (id);
ALTER INDEX ti RENAME TO u;
` + later, mayFail(nil)},

		// Dependent facts of a partial table.
		{"ALTER COLUMN TYPE on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ALTER COLUMN a TYPE bigint;
` + later, mayFail(nil)},
		{"DROP NOT NULL on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int NOT NULL);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ALTER COLUMN a DROP NOT NULL;
` + later, mayFail(nil)},
		{"SET DEFAULT on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ALTER COLUMN a SET DEFAULT 1;
` + later, mayFail(nil)},
		{"ADD GENERATED AS IDENTITY on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int NOT NULL);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t ALTER COLUMN a ADD GENERATED ALWAYS AS IDENTITY;
` + later, mayFail(nil)},
		{"DROP COLUMN without CASCADE on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t DROP COLUMN a;
` + later, mayFail(nil)},
		{"guard: DROP COLUMN CASCADE on a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int, a int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
ALTER TABLE t DROP COLUMN a CASCADE;
` + later, mergeWant(applied, want{"column t.a": false})},
		{"DROP COLUMN that an unresolved foreign key may reference", `-- FILE: 1_a.sql
CREATE TABLE c (pid int REFERENCES q (id));
CREATE TABLE q (id int PRIMARY KEY, a int);
ALTER TABLE q DROP COLUMN id;
` + later, mayFail(nil)},
		{"DROP TABLE without CASCADE of a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
DROP TABLE t;
` + later, mayFail(want{"state t": "partial"})},
		{"guard: DROP TABLE CASCADE of a partial table", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
DROP TABLE t CASCADE;
` + later, mergeWant(applied, want{"state t": "absent"})},
		{"DROP TABLE that an unresolved foreign key may reference", `-- FILE: 1_a.sql
CREATE TABLE c (pid int REFERENCES q);
CREATE TABLE q (id int PRIMARY KEY);
DROP TABLE q;
` + later, mayFail(want{"state q": "partial"})},

		// Names that may mean another table, or none.
		{"ALTER TABLE after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
ALTER TABLE t ADD COLUMN a int;
` + later, mayFail(nil)},
		{"guard: ALTER TABLE IF EXISTS ... ADD COLUMN IF NOT EXISTS after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
ALTER TABLE IF EXISTS t ADD COLUMN IF NOT EXISTS a int;
RESET search_path;
` + later, applied},
		{"ALTER TABLE on a temporary table's name", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE TEMP TABLE t (x int);
ALTER TABLE t ADD COLUMN a int;
` + later, mayFail(nil)},
		{"ALTER TABLE of a qualification only another table has", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
ALTER TABLE public.t ADD COLUMN a int;
` + later, mayFail(nil)},
		{"guard: DROP CONSTRAINT IF EXISTS on a name two tables may hold", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
CREATE TABLE IF NOT EXISTS app.t (id int PRIMARY KEY);
-- FILE: 2_b.sql
ALTER TABLE t DROP CONSTRAINT IF EXISTS t_pkey;
`, want{"applied 1_a.sql": true, "applied 2_b.sql": true, "primary_key table t": nil}},
		{"CREATE INDEX after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
CREATE INDEX ON t (id);
` + later, mayFail(nil)},
		{"CREATE INDEX of a qualification only another table has", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ON public.t (id);
` + later, mayFail(nil)},
		{"CREATE INDEX with a name an uncertain relation may hold", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE VIEW v AS SELECT 1 AS x;
DO $$ BEGIN NULL; END $$;
CREATE INDEX v ON t (id);
` + later, mayFail(nil)},
		{"DROP TABLE after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
DROP TABLE t;
` + later, mayFail(nil)},
		{"guard: DROP TABLE IF EXISTS after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
DROP TABLE IF EXISTS t;
RESET search_path;
` + later, applied},
		{"guard: DROP TABLE of a temporary table's name", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE TEMP TABLE t (x int);
DROP TABLE t;
` + later, applied},
		{"DROP TABLE of a qualification only another table has", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
DROP TABLE public.t;
` + later, mayFail(nil)},
		{"CREATE TABLE of a name a table under another qualification may hold", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE TABLE public.t (id int);
` + later, mayFail(nil)},
		{"guard: CREATE TABLE IF NOT EXISTS of such a name", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE TABLE IF NOT EXISTS public.t (id int);
` + later, applied},
		{"CREATE TABLE again after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
CREATE TABLE t (id int);
` + later, mayFail(nil)},
		{"CREATE TABLE of a name an uncertain relation may hold", `-- FILE: 1_a.sql
CREATE VIEW v AS SELECT 1 AS x;
DO $$ BEGIN NULL; END $$;
CREATE TABLE v (id int);
` + later, mayFail(nil)},
		{"CREATE VIEW of a table's name after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
SET search_path TO app;
CREATE VIEW t AS SELECT 1 AS x;
` + later, mayFail(nil)},
		{"DROP INDEX of an unqualified table's index under another qualification", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ti ON t (id);
DROP INDEX public.ti;
` + later, mayFail(nil)},
		{"guard: the same DROP INDEX IF EXISTS", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ti ON t (id);
DROP INDEX IF EXISTS public.ti;
` + later, applied},
		{"DROP INDEX of a never-written name that may be generated", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ON t (id);
DROP INDEX t_id_idx;
` + later, mayFail(nil)},
		{"guard: the same DROP INDEX IF EXISTS of a generated name", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ON t (id);
DROP INDEX IF EXISTS t_id_idx;
` + later, applied},
		{"ALTER INDEX RENAME after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ti ON t (id);
SET search_path TO app;
ALTER INDEX ti RENAME TO tj;
` + later, mayFail(nil)},
		{"guard: ALTER INDEX IF EXISTS RENAME after a search_path change", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ti ON t (id);
SET search_path TO app;
ALTER INDEX IF EXISTS ti RENAME TO tj;
RESET search_path;
` + later, applied},
		{"ALTER INDEX RENAME of a never-written name that may be generated", `-- FILE: 1_a.sql
CREATE TABLE t (id int);
CREATE INDEX ON t (id);
ALTER INDEX t_id_idx RENAME TO tj;
` + later, mayFail(nil)},
		{"ALTER TABLE of a generated index name", `-- FILE: 1_a.sql
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t_pkey RENAME TO tk;
` + later, mayFail(nil)},

		// factsKnown: a schema rename changes the name only.
		{"guard: ADD COLUMN on a table a schema rename moved", `-- FILE: 1_a.sql
CREATE TABLE inv.items (id int);
ALTER SCHEMA inv RENAME TO stock;
ALTER TABLE stock.items ADD COLUMN qty int;
-- FILE: 2_b.sql
CREATE TABLE public.later (id int);
`, mergeWant(applied, want{"state stock.items": "partial"})},
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

// TestRevisionEightFollowUps covers the decisions on the
// Revision 8 open questions, with the same principle: (1) a new name of
// the shape of a generated name of a known unnamed object, (2) a new
// foreign key whose column types differ as written, (3) a relation or type
// name already recorded in the same namespace. Every case not marked
// "guard" gave applied: true on c752219.
func TestRevisionEightFollowUps(t *testing.T) {
	type want map[string]any
	later := "-- FILE: 2_b.sql\nCREATE TABLE later (id int);\n"
	mayFail := want{"applied 1_a.sql": nil, "applied 2_b.sql": nil, "diag schema_migration_may_fail": true}
	applied := want{"applied 1_a.sql": true, "applied 2_b.sql": true, "diag schema_migration_may_fail": false}
	for _, tc := range []struct {
		name, sql string
		want      want
	}{
		// (1) Generated-name shapes.
		{"CREATE INDEX with a collision-suffixed key name", "-- FILE: 1_a.sql\nCREATE TABLE t (a int UNIQUE);\nCREATE INDEX t_a_key1 ON t (a);\n" + later, mayFail},
		{"ADD CONSTRAINT with the generated name of an unnamed UNIQUE", "-- FILE: 1_a.sql\nCREATE TABLE t (a int UNIQUE, b int);\nALTER TABLE t ADD CONSTRAINT t_a_key UNIQUE (b);\n" + later, mayFail},
		{"ADD CONSTRAINT FOREIGN KEY with an fkey shape", "-- FILE: 1_a.sql\nCREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE t (a int REFERENCES p, b int);\nALTER TABLE t ADD CONSTRAINT t_b_fkey FOREIGN KEY (b) REFERENCES p;\n" + later, mayFail},
		{"ADD CONSTRAINT CHECK with a check shape", "-- FILE: 1_a.sql\nCREATE TABLE t (a int CHECK (a > 0));\nALTER TABLE t ADD CONSTRAINT t_a_check CHECK (a < 9);\n" + later, mayFail},
		{"CREATE TABLE with a pkey shape", "-- FILE: 1_a.sql\nCREATE TABLE t (id int PRIMARY KEY);\nCREATE TABLE t_pkey1 (id int);\n" + later, mayFail},
		{"CREATE SEQUENCE with a serial sequence shape", "-- FILE: 1_a.sql\nCREATE TABLE t (id serial);\nCREATE SEQUENCE t_id_seq1;\n" + later, mayFail},
		{"CREATE VIEW with an idx shape", "-- FILE: 1_a.sql\nCREATE TABLE t (a int);\nCREATE INDEX ON t (a);\nCREATE VIEW t_a_idx AS SELECT 1 AS x;\n" + later, mayFail},
		{"RENAME TO an idx shape", "-- FILE: 1_a.sql\nCREATE TABLE t (a int);\nCREATE INDEX ON t (a);\nCREATE TABLE u (id int);\nALTER TABLE u RENAME TO t_a_idx2;\n" + later, mayFail},
		{"a name of maximum length may be truncated", "-- FILE: 1_a.sql\nCREATE TABLE t (a int UNIQUE);\nCREATE INDEX t_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_key ON t (a);\n" + later, mayFail},
		{"guard: IF NOT EXISTS makes the shape harmless", "-- FILE: 1_a.sql\nCREATE TABLE t (a int UNIQUE);\nCREATE INDEX IF NOT EXISTS t_a_key1 ON t (a);\n" + later, applied},
		{"guard: a shape without an unnamed object", "-- FILE: 1_a.sql\nCREATE TABLE t (a int CONSTRAINT u UNIQUE);\nCREATE INDEX t_a_key ON t (a);\n" + later, applied},
		{"guard: a name of another table's shape in another schema", "-- FILE: 1_a.sql\nCREATE TABLE app.t (a int UNIQUE);\nCREATE INDEX t_a_key ON b.other (a);\n" + later, applied},
		// T018a2 B3 (P2, g1-s16467): the index takes its name in schema b,
		// so a table of that name there is refused (42P07).
		{"an index on an outside table holds its name", "-- FILE: 1_a.sql\nCREATE TABLE app.t (a int UNIQUE);\nCREATE INDEX t_a_key ON b.other (a);\nCREATE TABLE b.t_a_key (id int);\n" + later, mayFail},

		// (2) Foreign-key column types.
		{"C2 (B2): int referencing bigint", "-- FILE: 1_a.sql\nCREATE TABLE p (id bigint PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\n" + later, mergeWant(applied, want{"status ref c.pid": "linked"})},
		{"C2 (B2): varchar referencing text", "-- FILE: 1_a.sql\nCREATE TABLE p (k text UNIQUE);\nCREATE TABLE c (pk varchar(10) REFERENCES p (k));\n" + later, mergeWant(applied, want{"status ref c.pk": "linked"})},
		{"C2 (B2): ADD FOREIGN KEY with differing types", "-- FILE: 1_a.sql\nCREATE TABLE p (id bigint PRIMARY KEY);\nCREATE TABLE c (pid int);\nALTER TABLE c ADD FOREIGN KEY (pid) REFERENCES p;\n" + later, mergeWant(applied, want{"status ref c.pid": "linked"})},
		{"varchar referencing uuid", "-- FILE: 1_a.sql\nCREATE TABLE p (id uuid PRIMARY KEY);\nCREATE TABLE c (pid varchar(36) REFERENCES p);\n" + later, mergeWant(mayFail, want{"status ref c.pid": "unresolved"})},
		{"guard: integer referencing serial links", "-- FILE: 1_a.sql\nCREATE TABLE p (id serial PRIMARY KEY);\nCREATE TABLE c (pid integer REFERENCES p);\n" + later, mergeWant(applied, want{"status ref c.pid": "linked"})},
		{"guard: case and spacing do not matter", "-- FILE: 1_a.sql\nCREATE TABLE p (id UUID PRIMARY KEY);\nCREATE TABLE c (pid uuid REFERENCES p);\n" + later, mergeWant(applied, want{"status ref c.pid": "linked"})},

		// (3) Relation and type namespaces.
		{"CREATE VIEW twice", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nCREATE VIEW v AS SELECT 2 AS x;\n" + later, mayFail},
		{"CREATE SEQUENCE of a view's name", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nCREATE SEQUENCE v;\n" + later, mayFail},
		{"CREATE TYPE of a view's name (row type)", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nCREATE TYPE v AS ENUM ('a');\n" + later, mayFail},
		{"CREATE DOMAIN twice", "-- FILE: 1_a.sql\nCREATE DOMAIN d AS int;\nCREATE DOMAIN d AS int;\n" + later, mayFail},
		{"CREATE MATERIALIZED VIEW IF NOT EXISTS over a domain", "-- FILE: 1_a.sql\nCREATE DOMAIN d AS int;\nCREATE MATERIALIZED VIEW IF NOT EXISTS d AS SELECT 1 AS x;\n" + later, mayFail},
		{"CREATE OR REPLACE VIEW over a sequence", "-- FILE: 1_a.sql\nCREATE SEQUENCE v;\nCREATE OR REPLACE VIEW v AS SELECT 1 AS x;\n" + later, mayFail},
		{"CREATE OR REPLACE VIEW over a view", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nCREATE OR REPLACE VIEW v AS SELECT 1 AS x, 2 AS y;\n" + later, mayFail},
		{"CREATE VIEW of a name an opaque statement may have dropped", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nDO $$ BEGIN NULL; END $$;\nCREATE VIEW v AS SELECT 2 AS x;\n" + later, mayFail},
		{"guard: CREATE SEQUENCE IF NOT EXISTS over a view", "-- FILE: 1_a.sql\nCREATE VIEW v AS SELECT 1 AS x;\nCREATE SEQUENCE IF NOT EXISTS v;\n" + later, applied},
		{"guard: a type may share a sequence's name", "-- FILE: 1_a.sql\nCREATE SEQUENCE s;\nCREATE TYPE s AS ENUM ('a');\n" + later, applied},
		{"guard: views in two explicit schemas", "-- FILE: 1_a.sql\nCREATE VIEW app.v AS SELECT 1 AS x;\nCREATE VIEW b.v AS SELECT 1 AS x;\n" + later, applied},
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

// mergeWant returns base with extra's entries added.
func mergeWant[M ~map[string]any](base, extra M) M {
	out := M{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

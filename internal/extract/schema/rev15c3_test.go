package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.5 cases of step C3 (T018a2 U2, class P12: code effects), as
// rev15Cases, each checked live by TestRev15C3CasesPostgres.
var rev15C3Cases = []b1dCase{
	// Names code may create are possibly held (P2).
	{"do creates a table, then create", "DO $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; END $$;\nCREATE TABLE x (id int);", "null", "O1.P12", "42P07"},
	{"do creates a table, then create (protected block)", "DO $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE x (id int);", "null", "M1.P12", "42P07"},
	{"do creates an index, then create", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE INDEX x_i ON x (id)$q$; END $$;\nCREATE INDEX x_i ON x (id);", "null", "O1.P12", "42P07"},
	{"do creates an index, then create (protected block)", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE INDEX x_i ON x (id)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE INDEX x_i ON x (id);", "null", "M4.P12", "42P07"},
	{"function creates a table, select, then create", "CREATE FUNCTION mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; END $$;\nSELECT mk();\nCREATE TABLE x (id int);", "null", "O1.P12", "42P07"},
	{"function creates a table, select, then create (protected block)", "CREATE FUNCTION mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nSELECT mk();\nCREATE TABLE x (id int);", "null", "M1.P12", "42P07"},
	{"procedure creates a table, call, then create", "CREATE PROCEDURE mk() LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; END $$;\nCALL mk();\nCREATE TABLE x (id int);", "null", "O1.P12", "42P07"},
	{"procedure creates a table, call, then create (protected block)", "CREATE PROCEDURE mk() LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCALL mk();\nCREATE TABLE x (id int);", "null", "M1.P12", "42P07"},
	{"sql function creates a table", "CREATE FUNCTION mk() RETURNS void LANGUAGE sql AS $$ CREATE TABLE x (id int) $$;\nSELECT mk();\nCREATE TABLE x (id int);", "null", "N1.P1", "42P07"},
	{"nested routine call", "CREATE FUNCTION inner_mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; END $$;\nCREATE FUNCTION outer_mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN PERFORM inner_mk(); END $$;\nSELECT outer_mk();\nCREATE TABLE x (id int);", "null", "O1.P12", "42P07"},
	{"trigger function fired by an insert", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN CREATE TABLE IF NOT EXISTS made (a int); RETURN NEW; END $$;\nCREATE TRIGGER tg BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nINSERT INTO t VALUES (1);\nCREATE TABLE made (a int);", "null", "", "42P07"},
	{"do creates an enum, then create", "DO $$ BEGIN CREATE TYPE e AS ENUM ('a'); END $$;\nCREATE TYPE e AS ENUM ('a');", "null", "O1.P12", "42710"},
	{"do creates an enum, then create (protected block)", "DO $$ BEGIN CREATE TYPE e AS ENUM ('a'); EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TYPE e AS ENUM ('a');", "null", "R1.P12", "42710"},
	{"do creates an enum in an exception block", "DO $$ BEGIN CREATE TYPE e AS ENUM ('a'); EXCEPTION WHEN duplicate_object THEN NULL; END $$;\nCREATE TYPE e AS ENUM ('a');", "null", "O1.P12", "42710"},
	{"do creates a sequence, then create", "DO $$ BEGIN EXECUTE $q$CREATE SEQUENCE s$q$; END $$;\nCREATE SEQUENCE s;", "null", "O1.P12", "42P07"},
	{"do creates a sequence, then create (protected block)", "DO $$ BEGIN EXECUTE $q$CREATE SEQUENCE s$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE SEQUENCE s;", "null", "R1.P12", "42P07"},
	{"guarded type, then used", "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'e') THEN CREATE TYPE e AS ENUM ('a'); END IF; END $$;\nCREATE TABLE t (id int, s e);", "null", "", "ok"},
	{"do renames an index, then rename to its name", "CREATE TABLE x (id int);\nCREATE INDEX x_i ON x (id);\nCREATE INDEX x_k ON x (id);\nDO $$ BEGIN EXECUTE $q$ALTER INDEX x_i RENAME TO x_j$q$; END $$;\nALTER INDEX x_k RENAME TO x_j;", "null", "O1.P12", "42P07"},
	{"do renames an index, then rename to its name (protected block)", "CREATE TABLE x (id int);\nCREATE INDEX x_i ON x (id);\nCREATE INDEX x_k ON x (id);\nDO $$ BEGIN EXECUTE $q$ALTER INDEX x_i RENAME TO x_j$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER INDEX x_k RENAME TO x_j;", "null", "M8.P12", "42P07"},
	{"do creates a function, then create", "DO $$ BEGIN EXECUTE $q$CREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1'$q$; END $$;\nCREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1';", "null", "R6.P12", "42723"},
	{"do creates a function, then replace with another type", "DO $$ BEGIN EXECUTE $q$CREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1'$q$; END $$;\nCREATE OR REPLACE FUNCTION g() RETURNS text LANGUAGE sql AS 'SELECT 1::text';", "null", "R6.P3", "42P13"},
	{"do creates a role, then create", "DO $$ BEGIN CREATE ROLE gv_p12_role; END $$;\nCREATE ROLE gv_p12_role;", "null", "O1.P12", "42710"},
	{"do creates a role, then create (protected block)", "DO $$ BEGIN CREATE ROLE gv_p12_role; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE ROLE gv_p12_role;", "null", "N1.P12", "42710"},
	{"do creates a trigger, then create", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE TRIGGER t1 BEFORE INSERT ON x FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger()$q$; END $$;\nCREATE TRIGGER t1 BEFORE INSERT ON x FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger();", "null", "O1.P12", "42710"},
	{"do creates a trigger, then create (protected block)", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE TRIGGER t1 BEFORE INSERT ON x FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger()$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TRIGGER t1 BEFORE INSERT ON x FOR EACH ROW EXECUTE FUNCTION suppress_redundant_updates_trigger();", "null", "N1.P12", "42710"},
	{"do creates a policy, then create", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE POLICY p1 ON x USING (true)$q$; END $$;\nCREATE POLICY p1 ON x USING (true);", "null", "O1.P12", "42710"},
	{"do creates a policy, then create (protected block)", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$CREATE POLICY p1 ON x USING (true)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE POLICY p1 ON x USING (true);", "null", "N1.P12", "42710"},
	// Objects code may drop, rename or move are no longer held (P1).
	{"do drops a table, then alter", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$DROP TABLE x$q$; END $$;\nALTER TABLE x ADD COLUMN y int;", "null", "O1.P12", "42P01"},
	{"do drops a table, then alter (protected block)", "CREATE TABLE x (id int);\nDO $$ BEGIN EXECUTE $q$DROP TABLE x$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER TABLE x ADD COLUMN y int;", "null", "M5a.P12", "42P01"},
	{"do drops a set collation, then use", "CREATE COLLATION c (provider = icu, locale = 'und');\nDO $$ BEGIN DROP COLLATION c; END $$;\nCREATE TABLE t (a text COLLATE c);", "null", "O1.P12", "42704"},
	{"do drops a set collation, then use (protected block)", "CREATE COLLATION c (provider = icu, locale = 'und');\nDO $$ BEGIN DROP COLLATION c; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE t (a text COLLATE c);", "null", "M1.P12", "42704"},
	{"do renames a sequence, then drop", "CREATE SEQUENCE s1;\nDO $$ BEGIN ALTER SEQUENCE s1 RENAME TO s2; END $$;\nDROP SEQUENCE s1;", "null", "O1.P12", "42P01"},
	{"do renames a sequence, then drop (protected block)", "CREATE SEQUENCE s1;\nDO $$ BEGIN ALTER SEQUENCE s1 RENAME TO s2; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nDROP SEQUENCE s1;", "null", "R5.P12", "42P01"},
	{"do renames an enum label, then add it", "CREATE TYPE e AS ENUM ('a');\nDO $$ BEGIN ALTER TYPE e RENAME VALUE 'a' TO 'b'; END $$;\nALTER TYPE e ADD VALUE 'b';", "null", "O1.P12", "42710"},
	{"do renames an enum label, then add it (protected block)", "CREATE TYPE e AS ENUM ('a');\nDO $$ BEGIN ALTER TYPE e RENAME VALUE 'a' TO 'b'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER TYPE e ADD VALUE 'b';", "null", "R3.P12", "42710"},
	{"do takes an index for a key, then rename it", "CREATE TABLE t (a int NOT NULL);\nCREATE UNIQUE INDEX i ON t (a);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t ADD CONSTRAINT k PRIMARY KEY USING INDEX i$q$; END $$;\nALTER INDEX i RENAME TO z;", "null", "O1.P12", "42P01"},
	{"do takes an index for a key, then rename it (protected block)", "CREATE TABLE t (a int NOT NULL);\nCREATE UNIQUE INDEX i ON t (a);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t ADD CONSTRAINT k PRIMARY KEY USING INDEX i$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER INDEX i RENAME TO z;", "null", "M8.P12", "42P01"},
	{"do drops a key, then rename its index", "CREATE TABLE t (a int PRIMARY KEY);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t DROP CONSTRAINT t_pkey$q$; END $$;\nALTER INDEX t_pkey RENAME TO z;", "null", "O1.P12", "42P01"},
	{"do drops a key, then rename its index (protected block)", "CREATE TABLE t (a int PRIMARY KEY);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t DROP CONSTRAINT t_pkey$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER INDEX t_pkey RENAME TO z;", "null", "M8.P12", "42P01"},
	{"do drops an indexed column, then rename the index", "CREATE TABLE t (a int, b int);\nCREATE INDEX i ON t (b);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t DROP COLUMN b$q$; END $$;\nALTER INDEX i RENAME TO z;", "null", "O1.P12", "42P01"},
	{"do drops an indexed column, then rename the index (protected block)", "CREATE TABLE t (a int, b int);\nCREATE INDEX i ON t (b);\nDO $$ BEGIN EXECUTE $q$ALTER TABLE t DROP COLUMN b$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER INDEX i RENAME TO z;", "null", "M8.P12", "42P01"},
	{"do renames a column, then index it", "CREATE TABLE ct1 (a int);\nDO $$ BEGIN ALTER TABLE ct1 RENAME COLUMN a TO cx2; END $$;\nCREATE INDEX cx2 ON ct1 (a);", "null", "", "42703"},
	{"function creates a type, then a sequence of its name", "CREATE FUNCTION cr6() RETURNS void LANGUAGE plpgsql AS $$ BEGIN CREATE TYPE cx4 AS ENUM ('a'); EXCEPTION WHEN others THEN NULL; END $$;\nSELECT cr6();\nCREATE SEQUENCE cx4;", "null", "R1.P12", "42710"},
	// Class fixes the U2 gate exposed: a relation checks pg_type for its
	// name (P2, heap.c 1164-1182), LIST takes one key element (P4,
	// tablecmds.c transformPartitionSpec).
	{"sequence named as a type", "CREATE TYPE e AS ENUM ('a');\nCREATE SEQUENCE e;", "null", "", "42710"},
	{"index named as a type", "CREATE TABLE t (a int);\nCREATE TYPE e AS ENUM ('a');\nCREATE INDEX e ON t (a);", "null", "", "ok"},
	{"list partitioning over two columns", "CREATE TABLE p (id int, k int) PARTITION BY LIST (k, id);", "null", "M1.P4", "42P17"},
	{"list partitioning over one column", "CREATE TABLE p (id int, k int) PARTITION BY LIST (k);", "true", "", "ok"},
	// Held tables code names have unknown dependents (P8).
	{"do adds a foreign key, then drop the index", "CREATE TABLE p (id int NOT NULL);\nCREATE UNIQUE INDEX p_id ON p (id);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE c (pid int REFERENCES p (id))$q$; END $$;\nDROP INDEX p_id;", "null", "O1.P12", "2BP01"},
	{"do adds a foreign key, then drop the index (protected block)", "CREATE TABLE p (id int NOT NULL);\nCREATE UNIQUE INDEX p_id ON p (id);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE c (pid int REFERENCES p (id))$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nDROP INDEX p_id;", "null", "M7.P12", "2BP01"},
	{"do adds a foreign key, then drop the table", "CREATE TABLE p (id int PRIMARY KEY);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE c (pid int REFERENCES p)$q$; END $$;\nDROP TABLE p;", "null", "O1.P12", "2BP01"},
	{"do adds a foreign key, then drop the table (protected block)", "CREATE TABLE p (id int PRIMARY KEY);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE c (pid int REFERENCES p)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nDROP TABLE p;", "null", "M6.P12", "2BP01"},
	{"do creates a view, then drop the column", "CREATE TABLE x (id int, a int);\nDO $$ BEGIN EXECUTE $q$CREATE VIEW v AS SELECT a FROM x$q$; END $$;\nALTER TABLE x DROP COLUMN a;", "null", "O1.P12", "2BP01"},
	{"do creates a view, then drop the column (protected block)", "CREATE TABLE x (id int, a int);\nDO $$ BEGIN EXECUTE $q$CREATE VIEW v AS SELECT a FROM x$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER TABLE x DROP COLUMN a;", "null", "M5a.P12", "2BP01"},
	{"do creates a view, then retype the column", "CREATE TABLE x (id int, a int);\nDO $$ BEGIN EXECUTE $q$CREATE VIEW v AS SELECT a FROM x$q$; END $$;\nALTER TABLE x ALTER COLUMN a TYPE text;", "null", "O1.P12", "0A000"},
	{"do creates a view, then retype the column (protected block)", "CREATE TABLE x (id int, a int);\nDO $$ BEGIN EXECUTE $q$CREATE VIEW v AS SELECT a FROM x$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER TABLE x ALTER COLUMN a TYPE text;", "null", "M5a.P12", "0A000"},
	{"do uses a type, then drop it", "CREATE TYPE e AS ENUM ('a');\nDO $$ BEGIN EXECUTE $q$CREATE TABLE t (a e)$q$; END $$;\nDROP TYPE e;", "null", "O1.P12", "2BP01"},
	{"do uses a type, then drop it (protected block)", "CREATE TYPE e AS ENUM ('a');\nDO $$ BEGIN EXECUTE $q$CREATE TABLE t (a e)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nDROP TYPE e;", "null", "R5.P12", "2BP01"},
	{"do creates a partition, then an overlapping one", "CREATE TABLE p (id int) PARTITION BY RANGE (id);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (1) TO (10)$q$; END $$;\nCREATE TABLE p2 PARTITION OF p FOR VALUES FROM (5) TO (20);", "null", "O1.P12", "42P17"},
	{"do creates a partition, then an overlapping one (protected block)", "CREATE TABLE p (id int) PARTITION BY RANGE (id);\nDO $$ BEGIN EXECUTE $q$CREATE TABLE p1 PARTITION OF p FOR VALUES FROM (1) TO (10)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE p2 PARTITION OF p FOR VALUES FROM (5) TO (20);", "null", "M1b.P12", "42P17"},
	// Unbounded inventories.
	{"do executes a format result", "DO $$ BEGIN EXECUTE format('CREATE TABLE %I (a int)', 'q'); END $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "ok"},
	{"do changes search_path", "DO $$ BEGIN PERFORM set_config('search_path', 'pg_catalog', false); END $$;\nCREATE TABLE t (a int);", "null", "O1.P12", "42501"},
	{"do in another language", "DO LANGUAGE sql $$ SELECT 1 $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "0A000"},
	// Inventories that leave later statements alone (no loss).
	{"do grants only", "CREATE TABLE x (id int);\nDO $$ BEGIN GRANT SELECT ON x TO PUBLIC; END $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "ok"},
	{"do grants only (protected block)", "CREATE TABLE x (id int);\nDO $$ BEGIN GRANT SELECT ON x TO PUBLIC; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE y (id int);", "true", "", "ok"},
	{"do creates a table no later statement touches", "DO $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; END $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "ok"},
	{"do creates a table no later statement touches (protected block)", "DO $$ BEGIN EXECUTE $q$CREATE TABLE x (id int)$q$; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE y (id int);", "true", "", "ok"},
	{"do executes a constant", "DO $$ BEGIN EXECUTE 'CREATE TABLE q (a int)'; END $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "ok"},
	{"do executes a constant (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE q (a int)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE y (id int);", "true", "", "ok"},
	{"do raises a notice", "DO $$ DECLARE n int := 1; BEGIN IF n > 0 THEN RAISE NOTICE 'n %', n; END IF; END $$;\nCREATE TABLE y (id int);", "true", "", "ok"},
	{"function refreshes a view", "CREATE TABLE t (a int);\nCREATE MATERIALIZED VIEW mv AS SELECT a FROM t;\nCREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE $q$REFRESH MATERIALIZED VIEW mv$q$; END $$;\nSELECT f();\nCREATE TABLE y (a int);", "true", "", "ok"},
	{"function without ddl", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int := 1; BEGIN n := n + 1; RETURN n; END $$;\nSELECT f();\nCREATE TABLE x (id int);", "true", "", "ok"},
	// Revision 1.6 V1 (P14): a body outside the provable subset (a loop,
	// embedded DDL the syntax-only readers do not read) may fail where it
	// is compiled; the cases above run their DDL through EXECUTE of a
	// constant, which the subset reads and P12 analyses.
	{"function with a loop", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int := 1; BEGIN FOR i IN 1..2 LOOP n := n + i; END LOOP; RETURN n; END $$;\nSELECT f();\nCREATE TABLE x (id int);", "null", "R6.P14", "ok"},
	{"do creates a table, written directly", "DO $$ BEGIN CREATE TABLE x (id int); END $$;\nCREATE TABLE y (id int);", "null", "O1.P14", "ok"},
}

func TestRev15C3Cases(t *testing.T) {
	for _, tc := range rev15C3Cases {
		t.Run(tc.name, func(t *testing.T) {
			applied, diagnostics := linkSQL(t, tc.sql)
			if applied["1_m.sql"] != tc.want {
				t.Fatalf("applied %v, want %s; %v", applied, tc.want, diagnostics)
			}
			if tc.cite != "" && !cites(diagnostics, tc.cite) {
				t.Fatalf("no diagnostic cites %s: %v", tc.cite, diagnostics)
			}
			if tc.want == "true" && tc.pg != "ok" {
				t.Fatalf("the case claims applied, but Postgres refuses it (%s)", tc.pg)
			}
		})
	}
}

func TestRev15C3CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev15C3Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

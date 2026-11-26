package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.5 cases (T018a2, step C1: H12, U1, U7-U10): each SQL text,
// the applied state the pass must give its one migration, the precondition
// a diagnostic must cite, and Postgres 17's answer ("ok" or the SQLSTATE),
// checked live by TestRev15CasesPostgres. A case the pass claims applied
// must be one Postgres accepts.
var rev15Cases = []b1dCase{
	// H12: E1 does not cover indexes.
	{"drop an index the set never creates", "DROP INDEX nosuch_index;", "null", "M7.P1", "42704"},
	{"drop an index the set never creates if exists", "DROP INDEX IF EXISTS nosuch_index;", "true", "", "ok"},
	// U1: ADD CONSTRAINT ... USING INDEX (row M5f-u).
	{"primary key using index, renamed", "CREATE TABLE t (a int NOT NULL);\nCREATE UNIQUE INDEX t_a ON t (a);\nALTER TABLE t ADD CONSTRAINT t_pk PRIMARY KEY USING INDEX t_a;\nALTER INDEX t_pk RENAME TO t_pk2;", "true", "", "ok"},
	{"unique using index, unnamed", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a);\nALTER TABLE t ADD UNIQUE USING INDEX t_a;\nALTER TABLE t DROP CONSTRAINT t_a;", "true", "", "ok"},
	{"using index of the old name after the rename", "CREATE TABLE t (a int NOT NULL);\nCREATE UNIQUE INDEX t_a ON t (a);\nALTER TABLE t ADD CONSTRAINT t_pk PRIMARY KEY USING INDEX t_a;\nDROP INDEX t_a;", "null", "M7.P1", "42704"},
	{"using index not unique", "CREATE TABLE t (a int NOT NULL);\nCREATE INDEX t_a ON t (a);\nALTER TABLE t ADD CONSTRAINT t_pk PRIMARY KEY USING INDEX t_a;", "null", "M5f-u.P4", "42809"},
	{"using index partial", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a) WHERE a > 0;\nALTER TABLE t ADD UNIQUE USING INDEX t_a;", "null", "M5f-u.P4", "42809"},
	{"using index descending", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a DESC);\nALTER TABLE t ADD UNIQUE USING INDEX t_a;", "null", "M5f-u.P4", "42809"},
	{"using index missing", "CREATE TABLE t (a int);\nALTER TABLE t ADD UNIQUE USING INDEX t_a;", "null", "M5f-u.P1", "42704"},
	{"using index of another table", "CREATE TABLE t (a int);\nCREATE TABLE u (a int);\nCREATE UNIQUE INDEX u_a ON u (a);\nALTER TABLE t ADD UNIQUE USING INDEX u_a;", "null", "M5f-u.P4", "55000"},
	{"using index nulls not distinct primary key", "CREATE TABLE t (a int NOT NULL);\nCREATE UNIQUE INDEX t_a ON t (a) NULLS NOT DISTINCT;\nALTER TABLE t ADD PRIMARY KEY USING INDEX t_a;", "null", "M5f-u.P4", "42P16"},
	{"using index second primary key", "CREATE TABLE t (a int PRIMARY KEY, b int NOT NULL);\nCREATE UNIQUE INDEX t_b ON t (b);\nALTER TABLE t ADD PRIMARY KEY USING INDEX t_b;", "null", "M5f-u.P4", "42P16"},
	{"using index constraint name taken", "CREATE TABLE t (a int, b int, CONSTRAINT k CHECK (a > 0));\nCREATE UNIQUE INDEX t_b ON t (b);\nALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX t_b;", "null", "M5f-u.P2", "23505"},
	{"using index new name taken in pg_class", "CREATE TABLE t (a int);\nCREATE TABLE k (x int);\nCREATE UNIQUE INDEX t_a ON t (a);\nALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX t_a;", "null", "M5f-u.P2", "42P07"},
	{"using index primary key over a nullable column with rows", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a);\nINSERT INTO t VALUES (NULL);\nALTER TABLE t ADD PRIMARY KEY USING INDEX t_a;", "null", "M5f-u.P11", "23502"},
	{"using index beside another subcommand", "CREATE TABLE t (a int);\nCREATE UNIQUE INDEX t_a ON t (a);\nALTER TABLE t ADD CONSTRAINT k UNIQUE USING INDEX t_a, ADD COLUMN c int;", "null", "", "ok"},
	// U7: types, collations, operator classes and settings.
	{"unknown type", "CREATE TABLE t (a datetime);", "null", "M2.P1", "42704"},
	{"contrib type", "CREATE EXTENSION IF NOT EXISTS citext;\nCREATE TABLE t (a citext);", "true", "", "ok"},
	{"set enum type", "CREATE TYPE mood AS ENUM ('a');\nCREATE TABLE t (a mood PRIMARY KEY);", "true", "", "ok"},
	{"unknown collation", "CREATE TABLE t (a text COLLATE \"xx_YY\");", "null", "M2.P1", "42704"},
	{"built-in collation", "CREATE TABLE t (a text COLLATE \"C\");", "true", "", "ok"},
	{"index over json", "CREATE TABLE t (a json);\nCREATE INDEX t_a ON t (a);", "null", "M4.P4", "42704"},
	{"index over point", "CREATE TABLE t (a point);\nCREATE INDEX t_a ON t (a);", "null", "M4.P4", "42704"},
	{"gist index over point", "CREATE TABLE t (a point);\nCREATE INDEX t_a ON t USING gist (a);", "true", "", "ok"},
	{"gin index over jsonb", "CREATE TABLE t (a jsonb);\nCREATE INDEX t_a ON t USING gin (a);", "true", "", "ok"},
	{"btree index over varchar", "CREATE TABLE t (a varchar(10));\nCREATE INDEX t_a ON t (a);", "true", "", "ok"},
	{"unique over xml", "CREATE TABLE t (a xml UNIQUE);", "null", "M1.P4", "42704"},
	{"primary key over json", "CREATE TABLE t (a json PRIMARY KEY);", "null", "M1.P4", "42704"},
	{"unique over an int array", "CREATE TABLE t (a int[] UNIQUE);", "true", "", "ok"},
	{"set an unknown setting", "SET no_such_setting = 1;\nCREATE TABLE t (a int);", "null", "N2.P1", "42704"},
	{"set a placeholder", "SET myapp.flag = 'x';\nCREATE TABLE t (a int);", "true", "", "ok"},
	{"set a reserved prefix", "SET plpgsql.nosuch = 1;\nCREATE TABLE t (a int);", "null", "N2.P1", "ok"},
	{"set a postmaster setting", "SET shared_buffers = 1000;", "null", "N2.P4", "55P02"},
	{"set an integer to text", "SET work_mem = 'abc';", "null", "N2.P5", "22023"},
	{"set an integer out of bounds", "SET work_mem = 1;", "null", "N2.P5", "22023"},
	{"set an integer", "SET work_mem = 4096;", "true", "", "ok"},
	{"set an enum", "SET client_min_messages = warning;", "true", "", "ok"},
	{"set an enum to another word", "SET client_min_messages = loud;", "null", "N2.P5", "22023"},
	{"set a check hook value", "SET default_with_oids = true;", "null", "N2.P5", "0A000"},
	{"set time zone", "SET timezone = 'UTC';", "true", "", "ok"},
	{"reset a postmaster setting", "RESET shared_buffers;", "null", "N2.P4", "55P02"},
	// U8: keywords in name positions.
	{"reserved column name", "CREATE TABLE t (id int, order int);", "null", "M1.P7", "42601"},
	{"reserved name after a dot", "CREATE TABLE public.order (a int);", "true", "", "ok"},
	{"col_name keyword as a type name", "CREATE TYPE values AS ENUM ('a');", "true", "", "ok"},
	{"type_func_name keyword as a type name", "CREATE TYPE verbose AS ENUM ('a');", "null", "R2.P7", "42601"},
	{"type_func_name keyword as a rename target", "CREATE TABLE t (x int);\nALTER TABLE t RENAME COLUMN x TO similar;", "null", "M5a.P7", "42601"},
	{"type_func_name keyword as an index rename target", "CREATE TABLE t (x int);\nCREATE INDEX i ON t (x);\nALTER INDEX i RENAME TO inner;", "null", "M8.P7", "42601"},
	{"col_name keyword as a routine name", "CREATE FUNCTION coalesce() RETURNS int LANGUAGE sql AS 'SELECT 1';", "null", "R6.P7", "42601"},
	{"reserved keyword as a trigger name", "CREATE TABLE t (x int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER select BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();", "null", "N1.P7", "42601"},
	// U9: storage parameters.
	{"fillfactor out of bounds", "CREATE TABLE t (a int);\nALTER TABLE t SET (fillfactor = 5);", "null", "M5k-a.P5", "22023"},
	{"unknown storage parameter", "CREATE TABLE t (a int);\nALTER TABLE t SET (no_such_option = 1);", "null", "M5k-a.P5", "22023"},
	{"fillfactor in bounds", "CREATE TABLE t (a int);\nALTER TABLE t SET (fillfactor = 50);", "true", "", "ok"},
	{"storage parameter on a partitioned table", "CREATE TABLE t (a int) PARTITION BY RANGE (a);\nALTER TABLE t SET (fillfactor = 50);", "null", "M5k-a.P5", "42809"},
	{"reset an unknown storage parameter", "CREATE TABLE t (a int);\nALTER TABLE t RESET (no_such);", "true", "", "ok"},
	{"create table with storage parameters", "CREATE TABLE t (a int) WITH (fillfactor = 70, autovacuum_enabled = off);", "true", "", "ok"},
	{"index with an unknown storage parameter", "CREATE TABLE t (a int);\nCREATE INDEX i ON t (a) WITH (pages_per_range = 4);", "null", "M4.P4", "22023"},
	{"brin index storage parameter", "CREATE TABLE t (a int);\nCREATE INDEX i ON t USING brin (a) WITH (pages_per_range = 4);", "true", "", "ok"},
	{"alter index storage parameter out of bounds", "CREATE TABLE t (a int);\nCREATE INDEX i ON t (a);\nALTER INDEX i SET (fillfactor = 500);", "null", "M4.P4", "22023"},
	// U10: system column names.
	{"system column name in create table", "CREATE TABLE t (xmin int);", "null", "M1.P2", "42701"},
	{"system column name added", "CREATE TABLE t (a int);\nALTER TABLE t ADD COLUMN IF NOT EXISTS ctid int;", "null", "M5a.P2", "42701"},
	{"system column name as a rename target", "CREATE TABLE t (a int);\nALTER TABLE t RENAME COLUMN a TO tableoid;", "null", "M5h.P2", "42701"},
	{"oid is no longer a system column", "CREATE TABLE t (oid int);", "true", "", "ok"},
}

func TestRev15Cases(t *testing.T) {
	for _, tc := range rev15Cases {
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

func TestRev15CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev15Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

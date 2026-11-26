package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.5 cases of step C2 (T018a2: U4, U5, U6; U3 is checked by
// its regression cases, which need sqlx's migration boundaries): as
// rev15Cases, each checked live by TestRev15C2CasesPostgres.
var rev15C2Cases = []b1dCase{
	// U4: unique indexes on partitioned tables include the partition key.
	{"partitioned add primary key without the key", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nALTER TABLE p ADD PRIMARY KEY (id);", "null", "M5f.P4", "0A000"},
	{"partitioned add primary key with the key", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nALTER TABLE p ADD PRIMARY KEY (id, k);", "true", "", "ok"},
	{"partitioned add unique include of the key", "CREATE TABLE p (id int, k int) PARTITION BY LIST (k);\nALTER TABLE p ADD UNIQUE (id) INCLUDE (k);", "null", "M5f.P4", "0A000"},
	{"partitioned add exclude", "CREATE TABLE p (id int, k int) PARTITION BY LIST (k);\nALTER TABLE p ADD EXCLUDE USING btree (k WITH =);", "null", "M5f.P4", "ok"},
	{"partitioned unique index without the key", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nCREATE UNIQUE INDEX p_id ON p (id);", "null", "M4.P4", "0A000"},
	{"partitioned unique index with the key", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nCREATE UNIQUE INDEX p_id ON p (id, k DESC);", "true", "", "ok"},
	{"partitioned unique hash index", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nCREATE UNIQUE INDEX p_id ON p USING hash (k);", "null", "M4.P4", "0A000"},
	{"partitioned unique index with a collation", "CREATE TABLE p (id int, k text) PARTITION BY LIST (k);\nCREATE UNIQUE INDEX p_k ON p (k COLLATE \"C\");", "null", "M4.P4", "0A000"},
	{"partitioned plain index", "CREATE TABLE p (id int, k int) PARTITION BY RANGE (k);\nCREATE INDEX p_id ON p (id);", "true", "", "ok"},
	{"partitioned add column unique", "CREATE TABLE p (id int, k int) PARTITION BY LIST (k);\nALTER TABLE p ADD COLUMN u int UNIQUE;", "null", "M5a.P4", "0A000"},
	{"plain table add column unique", "CREATE TABLE p (id int, k int);\nALTER TABLE p ADD COLUMN u int UNIQUE;", "true", "", "ok"},
	{"sub-partitioned child of a parent with a key", "CREATE TABLE p (id int, k int, j int, PRIMARY KEY (id, k)) PARTITION BY RANGE (k);\nCREATE TABLE c PARTITION OF p FOR VALUES FROM (1) TO (10) PARTITION BY LIST (j);", "null", "M1b.P4", "0A000"},
	{"sub-partitioned child by a key column", "CREATE TABLE p (id int, k int, j int, PRIMARY KEY (id, k)) PARTITION BY RANGE (k);\nCREATE TABLE c PARTITION OF p FOR VALUES FROM (1) TO (10) PARTITION BY LIST (id);", "true", "", "ok"},
	{"unique index over a sub-partitioned child", "CREATE TABLE p (id int, k int, j int) PARTITION BY RANGE (k);\nCREATE TABLE c PARTITION OF p FOR VALUES FROM (1) TO (10) PARTITION BY LIST (j);\nCREATE UNIQUE INDEX p_u ON p (id, k);", "null", "M4.P4", "0A000"},
	// U5: sequence options.
	{"sequence min not below max", "CREATE SEQUENCE s MINVALUE 10 MAXVALUE 5;", "null", "R1.P4", "22023"},
	{"sequence start below min", "CREATE SEQUENCE s START 0;", "null", "R1.P4", "22023"},
	{"sequence increment zero", "CREATE SEQUENCE s INCREMENT BY 0;", "null", "R1.P4", "22023"},
	{"sequence as text", "CREATE SEQUENCE s AS text;", "null", "R1.P4", "22023"},
	{"sequence as smallint max out of range", "CREATE SEQUENCE s AS smallint MAXVALUE 40000;", "null", "R1.P4", "22023"},
	{"sequence cache zero", "CREATE SEQUENCE s CACHE 0;", "null", "R1.P4", "22023"},
	{"sequence repeated option", "CREATE SEQUENCE s START 1 START 2;", "null", "R1.P4", "42601"},
	{"sequence descending defaults", "CREATE SEQUENCE s INCREMENT -1 START -5;", "true", "", "ok"},
	{"sequence descending start above max", "CREATE SEQUENCE s INCREMENT -1 START 5;", "null", "R1.P4", "22023"},
	{"sequence restart in create", "CREATE SEQUENCE s RESTART 5;", "true", "", "ok"},
	{"sequence restart above max", "CREATE SEQUENCE s MAXVALUE 3 RESTART 5;", "null", "R1.P4", "22023"},
	{"sequence fraction", "CREATE SEQUENCE s START 1.5;", "null", "R1.P4", "22P02"},
	{"sequence every option", "CREATE SEQUENCE s AS integer INCREMENT BY 2 MINVALUE 1 MAXVALUE 100 START WITH 3 CACHE 5 CYCLE;", "true", "", "ok"},
	{"sequence owned by a missing column", "CREATE TABLE t (a int);\nCREATE SEQUENCE s OWNED BY t.zz;", "null", "R1.P4", "42703"},
	{"sequence owned by a column", "CREATE TABLE t (a int);\nCREATE SEQUENCE s OWNED BY t.a;", "true", "", "ok"},
	{"sequence owned by none", "CREATE SEQUENCE s OWNED BY NONE;", "true", "", "ok"},
	{"alter sequence max below min", "CREATE SEQUENCE s;\nALTER SEQUENCE s MAXVALUE 0;", "null", "N1.P4", "22023"},
	{"alter sequence as smallint keeps explicit max", "CREATE SEQUENCE s MAXVALUE 100000;\nALTER SEQUENCE s AS smallint;", "null", "N1.P4", "22023"},
	{"alter sequence as smallint resets the default max", "CREATE SEQUENCE s;\nALTER SEQUENCE s AS smallint;", "true", "", "ok"},
	{"alter sequence restart above max", "CREATE SEQUENCE s MAXVALUE 10;\nALTER SEQUENCE s RESTART 11;", "null", "N1.P4", "22023"},
	{"alter sequence owned by a missing column", "CREATE SEQUENCE s;\nCREATE TABLE t (a int);\nALTER SEQUENCE s OWNED BY t.zz;", "null", "N1.P4", "42703"},
	{"alter serial sequence as bigint", "CREATE TABLE t (id serial);\nALTER SEQUENCE t_id_seq AS bigint;", "true", "", "ok"},
	{"alter serial sequence max above int", "CREATE TABLE t (id serial);\nALTER SEQUENCE t_id_seq MAXVALUE 3000000000;", "null", "N1.P4", "22023"},
	{"alter serial sequence max narrows", "CREATE TABLE t (id serial);\nALTER SEQUENCE t_id_seq MAXVALUE 100;", "null", "N1.P4", "ok"},
	{"alter identity sequence owned by", "CREATE TABLE t (id int GENERATED ALWAYS AS IDENTITY, a int);\nALTER SEQUENCE t_id_seq OWNED BY t.a;", "null", "N1.P4", "0A000"},
	{"alter sequence owner", "CREATE SEQUENCE s;\nALTER SEQUENCE s OWNER TO CURRENT_USER;", "true", "", "ok"},
	// U6: CREATE OR REPLACE of routines.
	{"replace function return type", "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE OR REPLACE FUNCTION f() RETURNS text LANGUAGE sql AS 'SELECT 1::text';", "null", "R6.P3", "42P13"},
	{"replace function same signature", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nCREATE OR REPLACE FUNCTION f() RETURNS integer LANGUAGE plpgsql AS 'BEGIN RETURN 2; END';", "true", "", "ok"},
	{"replace function overload", "CREATE FUNCTION f(a int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nCREATE OR REPLACE FUNCTION f(a text) RETURNS text LANGUAGE plpgsql AS 'BEGIN RETURN a; END';", "true", "", "ok"},
	{"replace function renamed parameter", "CREATE FUNCTION f(a int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nCREATE OR REPLACE FUNCTION f(b int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';", "null", "R6.P3", "42P13"},
	{"replace function newly named parameter", "CREATE FUNCTION f(int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nCREATE OR REPLACE FUNCTION f(b int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';", "true", "", "ok"},
	{"replace function removed default", "CREATE FUNCTION f(a int DEFAULT 1) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nCREATE OR REPLACE FUNCTION f(a int) RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';", "null", "R6.P3", "42P13"},
	{"replace function with procedure", "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS 'BEGIN END';\nCREATE OR REPLACE PROCEDURE f() LANGUAGE plpgsql AS 'BEGIN END';", "null", "R6.P3", "42809"},
	{"replace function out parameters", "CREATE FUNCTION f(OUT a int, OUT b int) LANGUAGE plpgsql AS 'BEGIN a := 1; b := 2; END';\nCREATE OR REPLACE FUNCTION f(OUT a int, OUT c int) LANGUAGE plpgsql AS 'BEGIN a := 1; c := 2; END';", "null", "R6.P3", "42P13"},
	{"replace function returns table", "CREATE FUNCTION f() RETURNS TABLE (a int) LANGUAGE plpgsql AS 'BEGIN RETURN; END';\nCREATE OR REPLACE FUNCTION f() RETURNS TABLE (a int) LANGUAGE plpgsql AS 'BEGIN RETURN; END';", "true", "", "ok"},
	{"replace function returns setof", "CREATE FUNCTION f() RETURNS SETOF int LANGUAGE plpgsql AS 'BEGIN RETURN; END';\nCREATE OR REPLACE FUNCTION f() RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';", "null", "R6.P3", "42P13"},
	{"replace procedure gaining an output parameter", "CREATE PROCEDURE p(a int) LANGUAGE plpgsql AS 'BEGIN END';\nCREATE OR REPLACE PROCEDURE p(a int, OUT b int) LANGUAGE plpgsql AS 'BEGIN END';", "null", "R6.P3", "42P13"},
	{"replace function after drop", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';\nDROP FUNCTION f();\nCREATE OR REPLACE FUNCTION f() RETURNS text LANGUAGE plpgsql AS 'BEGIN RETURN 1; END';", "true", "", "ok"},
	{"replace trigger function", "CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END';\nCREATE OR REPLACE FUNCTION f() RETURNS TRIGGER LANGUAGE plpgsql AS 'BEGIN RETURN OLD; END';", "true", "", "ok"},
}

func TestRev15C2Cases(t *testing.T) {
	for _, tc := range rev15C2Cases {
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

func TestRev15C2CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev15C2Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

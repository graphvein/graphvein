package schema

import (
	"context"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.9 step U-c cases (T018a2 Y39-Y41, Y44, Y45, Y48-Y51 and the
// E6 role rules): each rule's refused, may-fail and accepted forms, as
// rev19Cases. They are checked live by TestRev19cCasesPostgres as the
// migrating role of E6 (gv_migrator, not a superuser: H23), in a
// rolled-back transaction, and by TestContractFacts. Y52 changes facts,
// so its forms are oracle histories (TestRev19cCascadePostgres).
var rev19cCases = []b1dCase{
	// Y39: the dependency record without exceptions.
	{"Y39 a trigger named like its function", "CREATE TABLE t (a int);\n" + b1dTrigger + "CREATE TRIGGER tf BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP FUNCTION tf();", "null", "R5.P8", "2BP01"},
	{"Y39 a trigger named like its function, IF EXISTS without arguments", "CREATE TABLE t (a int);\n" + b1dTrigger + "CREATE TRIGGER tf BEFORE INSERT ON t FOR EACH ROW EXECUTE PROCEDURE tf();\nDROP FUNCTION IF EXISTS tf;", "null", "R5.P8", "2BP01"},
	{"Y39 the trigger dropped first", "CREATE TABLE t (a int);\n" + b1dTrigger + "CREATE TRIGGER tf BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TRIGGER tf ON t;\nDROP FUNCTION tf();", "true", "", "ok"},
	{"Y39 a trigger on a table named like its function", "CREATE TABLE tf (a int);\n" + b1dTrigger + "CREATE TRIGGER tg BEFORE INSERT ON tf FOR EACH ROW EXECUTE FUNCTION tf();\nDROP FUNCTION tf();", "null", "R5.P8", "2BP01"},
	{"Y39 the trigger's table dropped (its ON position)", "CREATE TABLE tf (a int);\n" + b1dTrigger + "CREATE TRIGGER tg BEFORE INSERT ON tf FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TABLE tf;\nDROP FUNCTION tf();", "true", "", "ok"},
	{"Y39 a trigger dropped on another table", "CREATE TABLE t (a int);\nCREATE TABLE u (a int);\n" + b1dTrigger + "CREATE TRIGGER tf BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TRIGGER IF EXISTS tf ON u;\nDROP FUNCTION tf();", "null", "R5.P8", "2BP01"},
	{"Y39 a quoted trigger and function", "CREATE TABLE \"T\" (a int);\nCREATE FUNCTION \"touchT\"() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER \"touchT\" BEFORE UPDATE ON \"T\" FOR EACH ROW EXECUTE FUNCTION \"touchT\"();\nDROP FUNCTION \"touchT\"();", "null", "R5.P8", "2BP01"},
	{"Y39 CREATE OR REPLACE TRIGGER replaces its dependency", "CREATE TABLE t (a int);\n" + b1dTrigger + "CREATE FUNCTION tg2() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER x BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nCREATE OR REPLACE TRIGGER x BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tg2();\nDROP FUNCTION tf();", "true", "", "ok"},
	{"Y39 a domain default of the domain's name", "CREATE FUNCTION slug() RETURNS text LANGUAGE sql AS $$ SELECT 'x' $$;\nCREATE DOMAIN slug AS text DEFAULT slug();\nDROP FUNCTION slug();", "null", "R5.P8", "2BP01"},
	{"Y39 ALTER DOMAIN SET DEFAULT of the domain's name", "CREATE FUNCTION d() RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE DOMAIN d AS int;\nALTER DOMAIN d SET DEFAULT d();\nDROP FUNCTION d();", "null", "R5.P8", "2BP01"},
	{"Y39 an ATOMIC body named like its table", "CREATE TABLE cleanup (id int);\nCREATE PROCEDURE cleanup() BEGIN ATOMIC DELETE FROM cleanup; END;\nDROP TABLE cleanup;", "null", "R1b.P5", "2BP01"},
	{"Y39 an ATOMIC body named like its sequence", "CREATE SEQUENCE next_id;\nCREATE FUNCTION next_id() RETURNS bigint BEGIN ATOMIC SELECT nextval('next_id'); END;\nDROP SEQUENCE next_id;", "null", "R1b.P5", "2BP01"},
	{"Y39 a function named like the type it returns", "CREATE TYPE pair AS (a int, b int);\nCREATE FUNCTION pair(a int, b int) RETURNS pair BEGIN ATOMIC SELECT ROW(a, b)::pair; END;\nDROP TYPE pair;", "null", "R1b.P5", "2BP01"},
	{"Y39 a function named like a referenced table", "CREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\nCREATE FUNCTION p() RETURNS int LANGUAGE sql AS 'SELECT 1';\nDROP FUNCTION p();", "true", "", "ok"},
	{"Y39 a referenced table dropped after its user", "CREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\nDROP TABLE c;\nDROP TABLE p;", "true", "", "ok"},
	// Y40: owned sequences are names of their column and table.
	{"Y40 a serial sequence in an ATOMIC body, DROP TABLE", "CREATE TABLE a (id serial PRIMARY KEY);\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nDROP TABLE a;", "null", "R1b.P5", "2BP01"},
	{"Y40 an identity sequence, DROP TABLE", "CREATE TABLE a (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY);\nCREATE PROCEDURE bump() BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nDROP TABLE a;", "null", "R1b.P5", "2BP01"},
	{"Y40 a serial sequence, DROP COLUMN", "CREATE TABLE a (id serial PRIMARY KEY, n int);\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nALTER TABLE a DROP COLUMN id;", "null", "R1b.P5", "2BP01"},
	{"Y40 an identity sequence, DROP IDENTITY", "CREATE TABLE a (id int GENERATED ALWAYS AS IDENTITY, n int);\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nALTER TABLE a ALTER COLUMN id DROP IDENTITY;", "null", "M5k-b.P4", "2BP01"},
	{"Y40 the table renamed, the sequence keeps its name", "CREATE TABLE a (id serial);\nALTER TABLE a RENAME TO b;\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nDROP TABLE b;", "null", "R1b.P5", "2BP01"},
	{"Y40 a sequence OWNED BY the column", "CREATE TABLE a (id int);\nCREATE SEQUENCE s OWNED BY a.id;\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('s'); END;\nDROP TABLE a;", "null", "R1b.P5", "2BP01"},
	{"Y40 a default of another table", "CREATE TABLE a (id serial);\nCREATE TABLE u (b bigint DEFAULT nextval('a_id_seq'));\nDROP TABLE a;", "null", "M1.P5", "2BP01"},
	{"Y40 another sequence", "CREATE TABLE a (id serial);\nCREATE SEQUENCE other;\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('other'); END;\nDROP TABLE a;", "null", "R1b.P5", "ok"},
	{"Y40 CASCADE drops the routine", "CREATE TABLE a (id int GENERATED ALWAYS AS IDENTITY);\nCREATE FUNCTION nx() RETURNS bigint BEGIN ATOMIC SELECT nextval('a_id_seq'); END;\nDROP TABLE a CASCADE;\nSELECT nx();", "null", "R1b.P5", "42883"},
	// Y41: columns used by SQL-standard bodies.
	{"Y41 an INSERT without the column, DROP COLUMN", "CREATE TABLE items (id int, name text);\nCREATE PROCEDURE add_item(n text) BEGIN ATOMIC INSERT INTO items (name) VALUES (n); END;\nALTER TABLE items DROP COLUMN name;", "null", "R1b.P5", "2BP01"},
	{"Y41 ALTER COLUMN TYPE", "CREATE TABLE items (id int, name text);\nCREATE FUNCTION first_name() RETURNS text BEGIN ATOMIC SELECT name FROM items LIMIT 1; END;\nALTER TABLE items ALTER COLUMN name TYPE varchar(10);", "null", "R1b.P5", "0A000"},
	{"Y41 RETURN expr with count(*)", "CREATE TABLE items (id int, name text);\nCREATE FUNCTION n() RETURNS bigint RETURN (SELECT count(*) FROM items);\nALTER TABLE items DROP COLUMN name;", "null", "R1b.P5", "ok"},
	{"Y41 CASCADE drops the routine", "CREATE TABLE items (id int, name text);\nCREATE FUNCTION fn() RETURNS text BEGIN ATOMIC SELECT name FROM items LIMIT 1; END;\nALTER TABLE items DROP COLUMN name CASCADE;\nSELECT fn();", "null", "R1b.P5", "42883"},
	{"Y41 another table's column", "CREATE TABLE items (id int, name text);\nCREATE TABLE other (x int);\nCREATE FUNCTION fn() RETURNS int BEGIN ATOMIC SELECT x FROM other LIMIT 1; END;\nALTER TABLE items DROP COLUMN name;", "null", "R1b.P5", "ok"},
	// Y44: columns whose types the statement does not write.
	{"Y44 SELECT INTO, the source dropped", "CREATE TYPE status AS ENUM ('a', 'b');\nCREATE TABLE orders (id int PRIMARY KEY, status status NOT NULL);\nSELECT * INTO orders_copy FROM orders;\nDROP TABLE orders CASCADE;\nDROP TYPE status;", "null", "R5.P8", "2BP01"},
	{"Y44 CREATE TABLE AS of a domain", "CREATE DOMAIN pos AS int CHECK (VALUE > 0);\nCREATE TABLE o (a pos);\nCREATE TABLE c AS SELECT * FROM o;\nDROP TABLE o CASCADE;\nDROP DOMAIN pos;", "null", "R5.P8", "2BP01"},
	{"Y44 LIKE copies the type", "CREATE TYPE st AS ENUM ('a');\nCREATE TABLE o (s st);\nCREATE TABLE c (LIKE o);\nDROP TABLE o CASCADE;\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"Y44 LIKE of another type", "CREATE TYPE st AS ENUM ('a');\nCREATE TABLE o (s text);\nCREATE TABLE c (LIKE o);\nDROP TYPE st;", "true", "", "ok"},
	{"Y44 a view's column type", "CREATE TYPE st AS ENUM ('a');\nCREATE VIEW v AS SELECT 'a'::st AS s;\nDROP TYPE st;", "null", "R1b.P5", "2BP01"},
	// Y45: an entry ends only with its object.
	{"Y45 DROP IF EXISTS of the old name", "CREATE TYPE st AS ENUM ('a');\nCREATE FUNCTION g() RETURNS st LANGUAGE sql AS $$ SELECT 'a'::st $$;\nALTER FUNCTION g() RENAME TO g2;\nDROP FUNCTION IF EXISTS g();\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"Y45 DROP of the new name", "CREATE TYPE st AS ENUM ('a');\nCREATE FUNCTION g() RETURNS st LANGUAGE sql AS $$ SELECT 'a'::st $$;\nALTER FUNCTION g() RENAME TO g2;\nDROP FUNCTION g2();\nDROP TYPE st;", "true", "", "ok"},
	// Y48: statements code runs from constants.
	{"Y48 a PL/pgSQL body that does not compile", "DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1 END $b$'; END $$;", "null", "R6.P14", "42601"},
	{"Y48 a SQL body that does not parse", "DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE sql AS ''SELECT 1 +'''; END $$;", "null", "O1.P12", "42601"},
	{"Y48 a body that compiles", "DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1; END $b$'; END $$;", "true", "", "ok"},
	{"Y48 a constant DROP INDEX of a key", "CREATE TABLE ct (id int PRIMARY KEY);\nCREATE PROCEDURE cr() LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'DROP INDEX ct_pkey'; END $$;\nCALL cr();", "null", "O1.P12", "2BP01"},
	{"Y48 a protected block", "DO $$ BEGIN CREATE ROLE gv19c_p; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x: %', SQLERRM; END $$;", "true", "", "ok"},
	// Y49: static statements in DO bodies and called routines.
	{"Y49 a regclass argument in PERFORM", "CREATE TABLE users (id int);\nCREATE FUNCTION g(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nDO $$ BEGIN PERFORM g('userz'); END $$;", "null", "O1.P1", "42P01"},
	{"Y49 a column in a DO body", "CREATE TABLE t (a int);\nDO $$ BEGIN PERFORM b FROM t; END $$;", "null", "N3.P1", "42703"},
	{"Y49 a table in a called procedure", "CREATE PROCEDURE p() LANGUAGE plpgsql AS $$ BEGIN DELETE FROM gv19c_nosuch; END $$;\nCREATE TABLE gv19c_nosuch (a int);\nDROP TABLE gv19c_nosuch;\nCALL p();", "null", "", "42P01"},
	// Y50: calls of routines whose objects changed.
	{"Y50 a column renamed", "CREATE TABLE t (a int);\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT a FROM t LIMIT 1); END $$;\nALTER TABLE t RENAME COLUMN a TO b;\nSELECT f();", "null", "O1.P1", "42703"},
	{"Y50 a table dropped", "CREATE TABLE t (a int);\nCREATE FUNCTION g() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT count(*) FROM t); END $$;\nDROP TABLE t;\nSELECT g();", "null", "", "42P01"},
	// Y51: superuser-only statements (E6).
	{"Y51 CREATE EVENT TRIGGER", "CREATE FUNCTION et() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;\nCREATE EVENT TRIGGER gv19c_e ON ddl_command_end EXECUTE FUNCTION et();", "null", "O5.P4", "42501"},
	{"Y51 CREATE ROLE SUPERUSER", "CREATE ROLE gv19c_su SUPERUSER;", "null", "N1.P4", "42501"},
	{"Y51 ALTER ROLE NOSUPERUSER", "CREATE ROLE gv19c_su;\nALTER ROLE gv19c_su NOSUPERUSER;", "null", "N1.P4", "42501"},
	{"Y51 ALTER ROLE CREATEDB", "CREATE ROLE gv19c_su;\nALTER ROLE gv19c_su CREATEDB;", "true", "", "ok"},
	{"Y51 a function in language c", "CREATE FUNCTION cf(int) RETURNS int LANGUAGE c AS 'int4abs';", "null", "R6.P4", "42501"},
	{"Y51 a shell type", "CREATE TYPE gv19c_shell;", "null", "R1.P4", "42501"},
	{"Y51 CREATE CAST WITHOUT FUNCTION", "CREATE TYPE ee AS ENUM ('a');\nCREATE CAST (ee AS int) WITHOUT FUNCTION;", "null", "N1.P4", "42501"},
	{"Y51 DISABLE TRIGGER ALL with a foreign key", "CREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE c (pid int REFERENCES p);\nALTER TABLE c DISABLE TRIGGER ALL;", "null", "M5k-b.P4", "42501"},
	{"Y51 DISABLE TRIGGER USER", "CREATE TABLE p (id int PRIMARY KEY);\nALTER TABLE p DISABLE TRIGGER USER;", "true", "", "ok"},
	{"Y51 ALTER TYPE SET", "CREATE TYPE ee AS ENUM ('a');\nALTER TYPE ee SET (STORAGE = plain);", "null", "R4.P4", "42809"},
	{"E6 ALTER DOMAIN OWNER TO a role the set created", "CREATE ROLE gv19c_o;\nCREATE DOMAIN dd AS int;\nALTER DOMAIN dd OWNER TO gv19c_o;", "null", "R4.P4", "42501"},
	{"Y51 a placeholder stored for a role", "CREATE ROLE gv19c_su;\nALTER ROLE gv19c_su SET gv9.x = '1';", "null", "N2.P4", "42501"},
	{"Y51 a placeholder set for the session", "SET gv9.x = '1';", "true", "", "ok"},
	{"Y51 an untrusted extension", "CREATE EXTENSION pg_stat_statements;", "null", "O2.P4", "42501"},
	{"Y51 a trusted extension", "CREATE EXTENSION pgcrypto;", "true", "", "ok"},
	{"Y51 SET SESSION AUTHORIZATION of another role", "CREATE ROLE gv19c_su;\nSET SESSION AUTHORIZATION gv19c_su;", "null", "N2.P4", "42501"},
	// E6: what a role the set created may do.
	{"E6 CREATE CAST between built-in types", "CREATE CAST (int AS text) WITH INOUT;", "null", "N1.P4", "42501"},
	{"E6 CREATE CAST of a set type", "CREATE TYPE ee AS ENUM ('a');\nCREATE CAST (ee AS text) WITH INOUT;", "true", "", "ok"},
	{"E6 OWNER TO a role the set created", "CREATE ROLE gv19c_o;\nCREATE TABLE t (a int);\nALTER TABLE t OWNER TO gv19c_o;", "null", "M5k-b.P4", "42501"},
	{"E6 OWNER TO CURRENT_USER", "CREATE TABLE t (a int);\nALTER TABLE t OWNER TO CURRENT_USER;", "true", "", "ok"},
	{"E6 a statement under SET ROLE of a set role", "CREATE ROLE gv19c_o;\nSET ROLE gv19c_o;\nCREATE TABLE public.gv19c_t (a int);", "null", "N1.P2", "42501"},
	{"E6 SET ROLE then RESET ROLE (O3: the path stays unknown)", "CREATE ROLE gv19c_o;\nSET ROLE gv19c_o;\nRESET ROLE;\nCREATE TABLE public.gv19c_t (a int);", "null", "Q1.P13", "ok"},
}

func TestRev19cCases(t *testing.T) {
	for _, tc := range rev19cCases {
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

// TestRev19cCasesPostgres checks each case's Postgres verdict as the
// migrating role of E6 (H23), in a rolled-back transaction of the oracle
// database it owns.
func TestRev19cCasesPostgres(t *testing.T) {
	o := sharedOracle(t)
	u, err := o.migratorURL(o.name)
	if err != nil {
		t.Fatal(err)
	}
	db, err := egress.OpenPostgres(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev19cCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// y52Histories are Y52's forms (CASCADE removes columns typed by a dropped
// type): tables, composite types, enums, domains and ranges used as
// column types directly, as arrays, through domains and ranges and as
// partition keys, with renames before the drop. Each is judged against
// Postgres: the pass must claim no column Postgres dropped.
var y52Histories = []struct{ name, sql string }{
	{"a table's row type in an array, renamed", "CREATE TABLE o1 (b int);\nCREATE TABLE d4 (a o1[], c int);\nALTER TABLE o1 RENAME TO o2;\nDROP TABLE IF EXISTS o2 CASCADE;"},
	{"a composite type", "CREATE TYPE c AS (x int);\nCREATE TABLE t (a int, b c);\nDROP TYPE c CASCADE;"},
	{"an enum array, renamed", "CREATE TYPE e AS ENUM ('a');\nCREATE TABLE t (a int, b e[]);\nALTER TYPE e RENAME TO e2;\nDROP TYPE e2 CASCADE;"},
	{"a domain over an enum", "CREATE TYPE e AS ENUM ('a');\nCREATE DOMAIN d AS e;\nCREATE TABLE t (a int, b d, c d[]);\nDROP TYPE e CASCADE;"},
	{"a range and its multirange", "CREATE TYPE r AS RANGE (subtype = int4);\nCREATE TABLE t (a int, b r, c r_multirange);\nDROP TYPE r CASCADE;"},
	{"a domain dropped", "CREATE DOMAIN d AS int;\nCREATE TABLE t (a int, b d);\nDROP DOMAIN d CASCADE;"},
	{"the last column", "CREATE TYPE e AS ENUM ('a');\nCREATE TABLE t (b e);\nDROP TYPE e CASCADE;"},
	{"a partition key", "CREATE TYPE e AS ENUM ('a', 'b');\nCREATE TABLE p (a e, b int) PARTITION BY LIST (a);\nCREATE TABLE q (x int);\nDROP TYPE e CASCADE;"},
	{"a composite attribute", "CREATE TYPE e AS ENUM ('a');\nCREATE TYPE c AS (x int, y e);\nCREATE TABLE t (a int, b c);\nDROP TYPE e CASCADE;"},
	{"an index and a key on the column", "CREATE TYPE e AS ENUM ('a');\nCREATE TABLE t (a int PRIMARY KEY, b e UNIQUE);\nCREATE INDEX t_b ON t (b);\nDROP TYPE e CASCADE;"},
}

func TestRev19cCascadePostgres(t *testing.T) {
	o := sharedOracle(t)
	for _, tc := range y52Histories {
		t.Run(tc.name, func(t *testing.T) {
			h := &oracleHistory{ID: "y52", Files: []oracleFile{{Name: "1_a.sql", SQL: tc.sql}}}
			j, err := judge(context.Background(), t, o, h)
			if err != nil {
				t.Fatal(err)
			}
			if len(j.all) > 0 {
				t.Fatalf("disagreements: %v", j.all)
			}
			// DROP TYPE and DROP DOMAIN ... CASCADE are row O5 (the tables
			// become opaque); DROP TABLE ... CASCADE is modelled and keeps
			// the other columns' facts.
			t.Logf("applied %d, known facts %d of %d", j.counts.Applied, j.counts.KnownFacts, j.counts.CatalogFacts)
		})
	}
}

// TestHarnessRev19 pins H21, H22 and H23's classification with the
// recorded texts of the inventory's hidden claims.
func TestHarnessRev19(t *testing.T) {
	never := func(string) bool { return false }
	set := "create table users (id int);\ncreate function g(t regclass) returns int language plpgsql as $$ begin return 1; end $$;\ndo $$ begin perform g('userz'); end $$;\n" +
		"create view uv as select * from users;\ncreate or replace function trigger_updated_at(tablename regclass) returns void as $$ begin execute format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION set_updated_at();', tablename); end; $$ language plpgsql;\nselect trigger_updated_at('uv');\n" +
		"DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1 END $b$'; END $$;\n" +
		"DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE sql AS ''SELECT 1 +'''; END $$;\n" +
		"CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT a FROM t LIMIT 1); END $$;\n" +
		"create function ti(n text) returns void as $$ begin execute format('CREATE INDEX ON %I (a)', n); end $$ language plpgsql;\nselect ti('zz');\n"
	for _, tc := range []struct {
		name string
		oc   outcome
		want string
	}{
		{"h14", outcome{SQLState: "42P01", Message: `relation "userz" does not exist`, Where: "PL/pgSQL function inline_code_block line 1 at PERFORM", StmtText: "do $$ begin perform g('userz'); end $$;"}, ""},
		{"i07", outcome{SQLState: "42809", Message: `"uv" is a view`, Where: "SQL statement \"CREATE TRIGGER set_updated_at BEFORE UPDATE ON uv FOR EACH ROW EXECUTE FUNCTION set_updated_at();\"\nPL/pgSQL function trigger_updated_at(regclass) line 1 at EXECUTE", StmtText: "select trigger_updated_at('uv');"}, ""},
		{"p030", outcome{SQLState: "42601", Message: "syntax error at end of input", Where: "PL/pgSQL function inline_code_block line 1 at EXECUTE", StmtText: "DO $$ BEGIN EXECUTE 'CREATE FUNCTION g() RETURNS int LANGUAGE plpgsql AS $b$ BEGIN RETURN 1 END $b$'; END $$;"}, ""},
		{"p031", outcome{SQLState: "42601", Message: "syntax error at end of input", Where: "SQL statement \"CREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1 +'\"\nPL/pgSQL function inline_code_block line 1 at EXECUTE", StmtText: "DO $$ ...;"}, ""},
		{"p088", outcome{SQLState: "42703", Message: `column "a" does not exist`, Where: "PL/pgSQL function f() line 1 at RETURN", StmtText: "SELECT f();"}, ""},
		{"n04", outcome{SQLState: "42P01", Message: `relation "t" does not exist`, Where: "PL/pgSQL function g() line 1 at RETURN", StmtText: "select g();"}, ""},
		{"a %I template (g5-s2274)", outcome{SQLState: "42P01", Message: `relation "zz" does not exist`, Where: "SQL statement \"CREATE INDEX ON zz (a)\"\nPL/pgSQL function ti(text) line 1 at EXECUTE", StmtText: "select ti('zz');"}, ""},
		{"RAISE", outcome{SQLState: "P0001", Message: "x", Where: "PL/pgSQL function f() line 1 at RAISE", StmtText: "SELECT f();"}, "E3"},
		{"a class 22 error in code", outcome{SQLState: "22P02", Message: "invalid input syntax", Where: "PL/pgSQL function f() line 1 at RETURN", StmtText: "SELECT f();"}, "E3"},
		{"dynamic SQL", outcome{SQLState: "42P01", Message: `relation "zz" does not exist`, Where: "SQL statement \"SELECT * FROM zz\"\nPL/pgSQL function f() line 1 at EXECUTE", StmtText: "SELECT f();"}, "E3"},
		{"an outside routine", outcome{SQLState: "42703", Message: `column "a" does not exist`, Where: "PL/pgSQL function outside_fn() line 1 at RETURN", StmtText: "SELECT outside_fn();"}, "E3"},
		{"a superuser-only form", outcome{SQLState: "42501", Message: `permission denied to create event trigger "e"`, Detail: ""}, ""},
		{"a privilege E6 grants", outcome{SQLState: "42501", Message: "permission denied for schema public"}, "E6"},
		{"a trusted language", outcome{SQLState: "42501", Message: "permission denied for language plpgsql"}, "E6"},
		{"an untrusted language", outcome{SQLState: "42501", Message: "permission denied for language c"}, ""},
		{"the system catalog", outcome{SQLState: "42501", Message: `permission denied to create "pg_catalog.t"`, Detail: "System catalog modifications are currently disallowed."}, ""},
		{"a superuser role attribute", outcome{SQLState: "42501", Message: "permission denied to create role", Detail: `Only roles with the SUPERUSER attribute may create roles with the SUPERUSER attribute.`}, ""},
	} {
		if got := assumption(tc.oc, map[string]bool{"users": true, "uv": true, "t": true, "g": true, "f": true}, nil, never, true, nil, set); got != tc.want {
			t.Errorf("%s: assumption %q, want %q", tc.name, got, tc.want)
		}
	}
	if !preludeAsSuperuser("CREATE EXTENSION IF NOT EXISTS citext") || preludeAsSuperuser("CREATE TABLE ext_a (id int)") {
		t.Error("preludeAsSuperuser: only CREATE EXTENSION runs as the superuser")
	}
	if got := outsideRoles([]string{`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN CREATE ROLE anon; END IF; END $$`}); strings.Join(got, " ") != "gv_oracle_role anon" {
		t.Errorf("outsideRoles = %v", got)
	}
	// H22: the inventory's hidden claims, recorded as regression cases,
	// produce no claimed-hit line.
	for _, c := range readCases(t) {
		switch c.ID {
		case "review19-r4-i07-w2-helper-on-view", "review19-r3-p030-do-execute-createfunc-bad", "review19-r3-p031-do-execute-sqlfunc-bad",
			"review19-r3-h14-perform-in-do-regclass", "review19-r3-p088-plpgsql-stale-call", "review19-r4-n04-plpgsql-body-table-dropped",
			"review19-inv-x05-event-trigger", "review19-r2-i20-event-trigger":
		default:
			continue
		}
		x, err := extractTemp(c.History)
		if err != nil {
			t.Fatal(err)
		}
		j := judgeWith(c.History, x, c.Verdict)
		if hits := claimedHits(j); len(hits) > 0 {
			t.Errorf("%s: %v", c.ID, hits)
		}
		for _, o := range c.Verdict.Outcomes {
			if o.Status == "refused" && o.Assumption != "" {
				t.Errorf("%s: the refusal is an assumption hit (%s), H21-H23 judge it", c.ID, o.Assumption)
			}
		}
	}
}

// TestHarnessRev19Postgres checks H23 against Postgres: the replay runs as
// a role that is not a superuser, a superuser-only statement is judged,
// and the outside roles may own objects in the history's schema.
func TestHarnessRev19Postgres(t *testing.T) {
	o := sharedOracle(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, sql, status, assumption string
	}{
		{"not a superuser", "DO $$ BEGIN IF (SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = current_user) OR current_user <> 'gv_migrator' THEN RAISE EXCEPTION 'replay as %', current_user; END IF; END $$;", "accepted", ""},
		{"an event trigger", "CREATE FUNCTION et() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;\nCREATE EVENT TRIGGER gv19c_h ON ddl_command_end EXECUTE FUNCTION et();", "refused", ""},
		{"an outside role owns a table", "CREATE TABLE t (a int);\nALTER TABLE t OWNER TO gv_oracle_role;", "accepted", ""},
		{"a set role owns a table", "CREATE ROLE gv19c_h;\nCREATE TABLE t (a int);\nALTER TABLE t OWNER TO gv19c_h;", "refused", "E6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := o.run(ctx, &oracleHistory{ID: "h23", Files: []oracleFile{{Name: "1_a.sql", SQL: tc.sql}}})
			if err != nil {
				t.Fatal(err)
			}
			oc := v.Outcomes[0]
			if oc.Status != tc.status || oc.Assumption != tc.assumption {
				t.Fatalf("%s %s (%s %s), want %s %s", oc.Status, oc.Assumption, oc.SQLState, oc.Message, tc.status, tc.assumption)
			}
		})
	}
}

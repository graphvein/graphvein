package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.8 cases (T018a2 X1-X4), as rev17Cases, each checked live by
// TestRev18CasesPostgres. Statements a migration runs in sqlx's
// transaction; a refused case's Postgres result is its SQLSTATE.
var rev18Cases = []b1dCase{
	// X1: dependents of every kind.
	{"X1 a trigger on the function", "CREATE TABLE h (id int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tg AFTER INSERT ON h FOR EACH ROW EXECUTE PROCEDURE f();\nDROP FUNCTION f();", "null", "R5.P8", "2BP01"},
	{"X1 a trigger on the function, with IF EXISTS", "CREATE TABLE h (id int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tg AFTER INSERT ON h FOR EACH ROW EXECUTE PROCEDURE f();\nDROP FUNCTION IF EXISTS f();", "null", "R5.P8", "2BP01"},
	{"X1 the trigger dropped first", "CREATE TABLE h (id int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tg AFTER INSERT ON h FOR EACH ROW EXECUTE PROCEDURE f();\nDROP TRIGGER tg ON h;\nDROP FUNCTION f();", "true", "", "ok"},
	{"X1 the trigger's table dropped first", "CREATE TABLE h (id int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tg AFTER INSERT ON h FOR EACH ROW EXECUTE PROCEDURE f();\nDROP TABLE h;\nDROP FUNCTION f();", "true", "", "ok"},
	{"X1 a trigger created by the axum helper", "CREATE TABLE users (id int, updated_at timestamptz);\nCREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE FUNCTION trigger_updated_at(tablename regclass) RETURNS void AS $$ BEGIN EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION set_updated_at();', tablename); END; $$ LANGUAGE plpgsql;\nSELECT trigger_updated_at('users');\nDROP FUNCTION set_updated_at();", "null", "R5.P8", "2BP01"},
	{"X1 a trigger on the function after a rename", "CREATE TABLE h (id int);\nCREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tg AFTER INSERT ON h FOR EACH ROW EXECUTE PROCEDURE f();\nALTER FUNCTION f() RENAME TO g;\nDROP FUNCTION g();", "null", "R5.P8", "2BP01"},
	{"X1 an event trigger on the function", "CREATE FUNCTION et() RETURNS event_trigger LANGUAGE plpgsql AS $$ BEGIN END $$;\nCREATE EVENT TRIGGER gv18_e ON ddl_command_end EXECUTE FUNCTION et();\nDROP FUNCTION et();", "null", "", "2BP01"},
	{"X1 a function result type", "CREATE TYPE st AS ENUM ('a');\nCREATE FUNCTION f() RETURNS st LANGUAGE sql AS $$ SELECT 'a'::st $$;\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"X1 a PL/pgSQL function result type, with IF EXISTS", "CREATE TYPE st AS ENUM ('a');\nCREATE FUNCTION f() RETURNS st LANGUAGE plpgsql AS $$ BEGIN RETURN 'a'; END $$;\nDROP TYPE IF EXISTS st;", "null", "R5.P8", "2BP01"},
	{"X1 a procedure argument type", "CREATE TYPE st AS ENUM ('a');\nCREATE PROCEDURE p(s st) LANGUAGE plpgsql AS $$ BEGIN END $$;\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"X1 an OUT parameter type", "CREATE TYPE st AS ENUM ('a');\nCREATE FUNCTION f(OUT s st) LANGUAGE plpgsql AS $$ BEGIN s := 'a'; END $$;\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"X1 a composite attribute", "CREATE TYPE st AS ENUM ('a');\nCREATE TYPE pair AS (s st, n int);\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"X1 an array attribute", "CREATE TYPE st AS ENUM ('a');\nCREATE TYPE pair AS (s st[], n int);\nDROP TYPE st;", "null", "R5.P8", "2BP01"},
	{"X1 the array type's name", "CREATE TYPE st AS ENUM ('a');\nCREATE TABLE t (a _st);\nDROP TYPE st;", "null", "", "2BP01"},
	{"X1 a row type as an attribute", "CREATE TABLE t (a int);\nCREATE TYPE pair AS (x t, n int);\nDROP TABLE t;", "null", "M6.P8", "2BP01"},
	{"X1 a row type as a column after a rename", "CREATE TABLE t (a int);\nCREATE TABLE u (r t);\nALTER TABLE t RENAME TO t2;\nDROP TABLE t2;", "null", "", "2BP01"},
	{"X1 a BEGIN ATOMIC body calls the function", "CREATE FUNCTION g() RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE PROCEDURE p() BEGIN ATOMIC SELECT g(); END;\nDROP FUNCTION g();", "null", "R1b.P5", "2BP01"},
	{"X1 a BEGIN ATOMIC body writes the table", "CREATE TABLE t (a int);\nCREATE PROCEDURE p() BEGIN ATOMIC INSERT INTO t VALUES (1); END;\nDROP TABLE t;", "null", "R1b.P5", "2BP01"},
	{"X1 a PL/pgSQL body reads the table", "CREATE TABLE t (a int);\nCREATE FUNCTION f() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN RETURN (SELECT count(*) FROM t); END $$;\nDROP TABLE t;", "true", "", "ok"},
	{"X1 a domain default", "CREATE FUNCTION f() RETURNS int LANGUAGE sql IMMUTABLE AS 'SELECT 1';\nCREATE DOMAIN d AS int DEFAULT f();\nDROP FUNCTION f();", "null", "R5.P8", "2BP01"},
	{"X1 a domain CHECK", "CREATE FUNCTION f(int) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$ BEGIN RETURN true; END $$;\nCREATE DOMAIN d AS int CHECK (f(VALUE));\nDROP FUNCTION f(int);", "null", "", "2BP01"},
	{"X1 a column default after the function is renamed", "CREATE FUNCTION f() RETURNS int LANGUAGE sql IMMUTABLE AS 'SELECT 1';\nCREATE TABLE t (id int DEFAULT f(), b int);\nALTER FUNCTION f() RENAME TO g;\nDROP FUNCTION g();", "null", "R5.P8", "2BP01"},
	{"X1 a regclass default", "CREATE TABLE t (a int);\nCREATE TABLE u (r regclass DEFAULT 't'::regclass);\nDROP TABLE t;", "null", "", "2BP01"},
	{"X1 a view on the view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE VIEW w AS SELECT a FROM v;\nDROP VIEW v;", "null", "", "2BP01"},
	{"X1 statistics on an expression", "CREATE FUNCTION f(int) RETURNS int LANGUAGE plpgsql IMMUTABLE AS $$ BEGIN RETURN 1; END $$;\nCREATE TABLE t (a int, b int);\nCREATE STATISTICS s ON (f(a)), b FROM t;\nDROP FUNCTION f(int);", "null", "", "2BP01"},
	{"X1 a policy calls the function", "CREATE FUNCTION f() RETURNS boolean LANGUAGE sql STABLE AS 'SELECT true';\nCREATE TABLE t (a int);\nCREATE POLICY p ON t USING (f());\nDROP FUNCTION f();", "null", "", "2BP01"},
	{"X1 an enum swap that forgot the column", "CREATE TYPE status AS ENUM ('a', 'b');\nCREATE TABLE t (id int, s status);\nALTER TYPE status RENAME TO status_old;\nCREATE TYPE status AS ENUM ('a', 'b', 'c');\nDROP TYPE status_old;", "null", "", "2BP01"},
	{"X1 a domain renamed, then dropped", "CREATE DOMAIN email AS text;\nCREATE TABLE t (e email);\nALTER DOMAIN email RENAME TO email_old;\nDROP DOMAIN email_old;", "null", "", "2BP01"},
	{"X1 a schema an unqualified table went into", "CREATE SCHEMA s;\nSET search_path TO s;\nCREATE TABLE t (a int);\nSET search_path TO public;\nDROP SCHEMA s;", "null", "", "2BP01"},
	{"X1 a sequence copied by LIKE", "CREATE TABLE t (a serial);\nCREATE TABLE u (LIKE t INCLUDING DEFAULTS);\nALTER TABLE t ALTER COLUMN a DROP DEFAULT;\nDROP SEQUENCE t_a_seq;", "null", "", "2BP01"},
	{"X1 a function without dependents", "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1';\nDROP FUNCTION f();", "true", "", "ok"},
	{"X1 a type without dependents", "CREATE TYPE st AS ENUM ('a');\nCREATE TABLE t (a int);\nDROP TYPE st;", "true", "", "ok"},
	{"X1 a table without dependents", "CREATE TABLE t (a int);\nCREATE INDEX t_a ON t (a);\nINSERT INTO t VALUES (1);\nDROP TABLE t;", "true", "", "ok"},
	{"X1 the atuin teardown", "CREATE TABLE history (id int, user_id int);\nCREATE TABLE total_history_count_user (id bigserial PRIMARY KEY, user_id bigint, total integer);\nCREATE FUNCTION user_history_count() RETURNS trigger AS $$ BEGIN UPDATE total_history_count_user SET total = total + 1 WHERE user_id = NEW.user_id; RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE TRIGGER tg_user_history_count AFTER INSERT ON history FOR EACH ROW EXECUTE PROCEDURE user_history_count();\nDROP TRIGGER IF EXISTS tg_user_history_count ON history;\nDROP FUNCTION IF EXISTS user_history_count();\nDROP TABLE IF EXISTS total_history_count_user;", "true", "", "ok"},
	{"X1 a correct enum swap (the record keeps the old column's words)", "CREATE TYPE status AS ENUM ('a');\nCREATE TABLE t (s status);\nALTER TYPE status RENAME TO status_old;\nCREATE TYPE status AS ENUM ('a', 'b');\nALTER TABLE t ALTER COLUMN s TYPE status USING s::text::status;\nDROP TYPE status_old;", "null", "", "ok"},
	{"X1 CASCADE removes a function, which is called", "CREATE TYPE status AS ENUM ('active');\nCREATE FUNCTION default_status() RETURNS status LANGUAGE plpgsql AS $$ BEGIN RETURN 'active'; END $$;\nDROP TYPE status CASCADE;\nSELECT default_status();", "null", "N1.P1", "42883"},
	{"X1 CASCADE removes a function, which is created again", "CREATE TYPE status AS ENUM ('active');\nCREATE FUNCTION default_status() RETURNS status LANGUAGE plpgsql AS $$ BEGIN RETURN 'active'; END $$;\nDROP TYPE status CASCADE;\nCREATE FUNCTION default_status() RETURNS text LANGUAGE sql AS $$ SELECT 'x' $$;", "null", "R5.P8", "ok"},
	// X2: collation renames.
	{"X2 drop a collation renamed away", "CREATE COLLATION ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);\nALTER COLLATION ci RENAME TO ci2;\nDROP COLLATION ci;", "null", "N1.P1", "42704"},
	{"X2 comment on a collation renamed away", "CREATE COLLATION ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);\nALTER COLLATION ci RENAME TO ci2;\nCOMMENT ON COLLATION ci IS 'x';", "null", "N1.P1", "42704"},
	{"X2 a column collation renamed away", "CREATE COLLATION ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);\nALTER COLLATION ci RENAME TO ci2;\nCREATE TABLE t (a text COLLATE ci);", "null", "N1.P1", "42704"},
	{"X2 the new name", "CREATE COLLATION ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);\nALTER COLLATION ci RENAME TO ci2;\nCREATE TABLE t (a text COLLATE ci2);\nDROP TABLE t;\nDROP COLLATION ci2;", "true", "", "ok"},
	{"X2 a renamed collation a column uses", "CREATE COLLATION ci (provider = icu, locale = 'und-u-ks-level2', deterministic = false);\nCREATE TABLE u (name text COLLATE ci);\nALTER COLLATION ci RENAME TO ci2;\nDROP COLLATION ci2;", "null", "", "2BP01"},
	{"X2 an index element names a collation never created", "CREATE TABLE t (a text);\nCREATE INDEX i ON t (a COLLATE nosuch);", "null", "M2.P1", "42704"},
	{"X2 comment on a collation never created", "COMMENT ON COLLATION nosuch IS 'x';", "null", "M2.P1", "42704"},
	{"X2 an index element names a built-in collation", "CREATE TABLE t (a text);\nCREATE INDEX i ON t (a COLLATE \"C\");", "true", "", "ok"},
	// X3: relations that are not tables in table positions.
	{"X3 a foreign key to a view", "CREATE TABLE t (a int PRIMARY KEY);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE TABLE c (x int REFERENCES v (a));", "null", "M3.P1", "42809"},
	{"X3 an index on a view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE INDEX v_a ON v (a);", "null", "M4.P1", "42809"},
	{"X3 an index on a composite type", "CREATE TYPE ty AS (a int);\nCREATE INDEX ty_a ON ty (a);", "null", "M4.P1", "42809"},
	{"X3 an index on a materialized view's missing column", "CREATE TABLE t (a int);\nCREATE MATERIALIZED VIEW mv AS SELECT a FROM t;\nCREATE INDEX mv_b ON mv (b);", "null", "M4.P1", "42703"},
	{"X3 an index on a materialized view's column", "CREATE TABLE t (a int);\nCREATE MATERIALIZED VIEW mv AS SELECT a FROM t;\nCREATE INDEX mv_a ON mv (a);", "null", "M4.P1", "ok"},
	{"X3 CREATE TABLE IF NOT EXISTS skipped over a view", "CREATE TABLE t (a int);\nCREATE VIEW v AS SELECT a FROM t;\nCREATE TABLE IF NOT EXISTS v (a int, b int);\nCREATE INDEX v_b ON v (b);", "null", "M4.P1", "42809"},
	// X4: CREATE SCHEMA AUTHORIZATION.
	{"X4 a schema named after a role, used", "CREATE ROLE gv18_auth;\nCREATE SCHEMA AUTHORIZATION gv18_auth;\nCREATE TABLE gv18_auth.t (a int);", "true", "", "ok"},
	{"X4 a schema named after a role, used before", "CREATE ROLE gv18_auth;\nCREATE TABLE gv18_auth.t (a int);\nCREATE SCHEMA AUTHORIZATION gv18_auth;", "null", "", "3F000"},
	{"X4 a schema named after a role, dropped, used", "CREATE ROLE gv18_auth;\nCREATE SCHEMA AUTHORIZATION gv18_auth;\nDROP SCHEMA gv18_auth;\nCREATE TABLE gv18_auth.t (a int);", "null", "", "3F000"},
	{"X4 a schema of the current user's name, then another schema", "CREATE SCHEMA AUTHORIZATION CURRENT_USER;\nCREATE TABLE gv18_nosuch.t (a int);", "null", "R1.P7", "3F000"},
}

func TestRev18Cases(t *testing.T) {
	for _, tc := range rev18Cases {
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

func TestRev18CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev18Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

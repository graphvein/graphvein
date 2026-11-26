package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.6 cases of step D2 (T018a2 V1, V5, V6, V8), as rev16Cases,
// each checked live by TestRev16D2CasesPostgres.
var rev16D2Cases = []b1dCase{
	// V1 (P14): the provable subset is applied.
	{"V1 trigger assigns NEW.col", "CREATE TABLE t (a int, updated_at timestamptz);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION tf();", "true", "", "ok"},
	{"V1 trigger with IF, DML and a scalar subquery", "CREATE TABLE h (user_id bigint);\nCREATE TABLE c (user_id bigint, total int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$\nbegin\n\tif (TG_OP='INSERT') then\n\t\tupdate c set total = total + 1 where user_id = new.user_id;\n\t\tif not found then\n\t\t\tinsert into c(user_id, total) values (new.user_id, (select count(1) from h where user_id = new.user_id));\n\t\tend if;\n\telsif (TG_OP='DELETE') then\n\t\tdelete from c where user_id = old.user_id;\n\tend if;\n\treturn NEW;\nend;\n$$ volatile cost 100;", "true", "", "ok"},
	{"V1 IS DISTINCT FROM and a bare value function", "CREATE FUNCTION tf() RETURNS trigger AS $$\nBEGIN\n    IF (\n        NEW IS DISTINCT FROM OLD AND\n        NEW.updated_at IS NOT DISTINCT FROM OLD.updated_at\n    ) THEN\n        NEW.updated_at := current_timestamp;\n    END IF;\n    RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;", "true", "", "ok"},
	{"V1 EXECUTE format with a regclass parameter", "CREATE FUNCTION f(_tbl regclass) RETURNS VOID AS $$\nBEGIN\n    EXECUTE format('SELECT 1 FROM %s', _tbl);\nEND;\n$$ LANGUAGE plpgsql;", "true", "", "ok"},
	{"V1 DO with embedded role statements and a handler", "DO $do$\nBEGIN\n    CREATE ROLE gv16d2_r WITH BYPASSRLS;\n    GRANT ALL ON ALL TABLES IN SCHEMA public TO gv16d2_r;\n    GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO gv16d2_r;\n    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO gv16d2_r;\n    LOCK TABLE pg_catalog.pg_roles;\nEXCEPTION WHEN OTHERS THEN\n    RAISE NOTICE 'error: %', SQLERRM;\nEND\n$do$;", "true", "", "ok"},
	{"V1 DO with a guarded rename", "CREATE TABLE u (confirmed_at timestamptz);\nDO $$\nBEGIN\n  IF NOT EXISTS(SELECT *\n    FROM information_schema.columns\n    WHERE table_schema = 'public' and table_name='u' and column_name='email_confirmed_at')\n  THEN\n      ALTER TABLE \"u\" RENAME COLUMN \"confirmed_at\" TO \"email_confirmed_at\";\n  END IF;\nEND $$;", "null", "O1.P12", "ok"},
	{"V1 DO with an empty target list", "DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'gv16d2_x') THEN NULL; END IF; END $$;", "true", "", "ok"},
	{"V1 declarations, ELSIF, ELSE, RAISE", "DO $$ DECLARE n int := 2; m text DEFAULT 'a'; BEGIN IF n IS DISTINCT FROM 1 AND n > 0 THEN n := n + 1; ELSIF n = 0 THEN NULL; ELSE RAISE NOTICE 'x % y %%', n; END IF; END $$;", "true", "", "ok"},
	{"V1 OUT parameter and RETURN", "CREATE FUNCTION f(x int, OUT y int) LANGUAGE plpgsql AS $$ BEGIN y := x; RETURN; END $$;", "true", "", "ok"},
	{"V1 SETOF and RETURN", "CREATE FUNCTION f(x int) RETURNS SETOF int LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$;", "true", "", "ok"},
	{"V1 procedure", "CREATE PROCEDURE p(a int) LANGUAGE plpgsql AS $$ BEGIN PERFORM a + 1; RETURN; END $$;", "true", "", "ok"},
	{"V1 labelled block", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ <<l>> BEGIN RETURN 1; END l $$;", "true", "", "ok"},
	{"V1 SQLSTATE handler", "DO $$ BEGIN NULL; EXCEPTION WHEN SQLSTATE '22012' OR division_by_zero THEN NULL; END $$;", "true", "", "ok"},
	// V1: certain compile errors are refused.
	{"V1 trailing text after END", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END; x $$;", "null", "R6.P14", "42601"},
	{"V1 end label of an unlabelled block", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END m $$;", "null", "R6.P14", "42601"},
	{"V1 duplicate declaration", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE a int; a text; BEGIN RETURN 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 undeclared target", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN y := 1; RETURN 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 NEW outside a trigger function", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN NEW.a := 1; RETURN 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 RETURN expression in void", "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P14", "42804"},
	{"V1 RETURN expression in a procedure", "CREATE PROCEDURE p() LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 RETURN expression with OUT", "CREATE FUNCTION f(OUT y int) LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P14", "42804"},
	{"V1 RETURN expression in DO", "DO $$ BEGIN RETURN 1; END $$;", "null", "O1.P14", "42804"},
	{"V1 RETURN without expression in a scalar function", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN; END $$;", "null", "R6.P14", "42601"},
	{"V1 RAISE parameter count", "DO $$ BEGIN RAISE NOTICE 'x % %', 1; END $$;", "null", "O1.P14", "42601"},
	{"V1 unknown condition", "DO $$ BEGIN NULL; EXCEPTION WHEN no_such_condition THEN NULL; END $$;", "null", "O1.P14", "42704"},
	{"V1 bad SQLSTATE", "DO $$ BEGIN NULL; EXCEPTION WHEN SQLSTATE '2201' THEN NULL; END $$;", "null", "O1.P14", "42601"},
	{"V1 serial declared", "DO $$ DECLARE s serial; BEGIN NULL; END $$;", "null", "O1.P14", "42704"},
	{"V1 trigger function with a parameter", "CREATE FUNCTION f(a int) RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;", "null", "R6.P14", "42P13"},
	{"V1 pseudo-type result", "CREATE FUNCTION f() RETURNS cstring LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$;", "null", "R6.P14", "0A000"},
	{"V1 pseudo-type argument", "CREATE FUNCTION f(a internal) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P14", "0A000"},
	{"V1 missing semicolon", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1 END $$;", "null", "R6.P14", "42601"},
	{"V1 misspelled embedded keyword", "CREATE PROCEDURE p() LANGUAGE plpgsql AS $$ BEGIN SELEC 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 unknown declared type", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE x nosuchtype; BEGIN RETURN 1; END $$;", "null", "R6.P14", "42704"},
	{"V1 unknown result type", "CREATE FUNCTION f() RETURNS nosuchtype LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "M2.P1", "42704"},
	{"V1 a language twice", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P4", "42601"},
	{"V1 conflicting volatility", "CREATE FUNCTION f() RETURNS int LANGUAGE sql IMMUTABLE VOLATILE AS 'SELECT 1';", "null", "R6.P4", "42601"},
	{"V1 a function attribute on a procedure", "CREATE PROCEDURE p() LANGUAGE plpgsql IMMUTABLE AS $$ BEGIN NULL; END $$;", "null", "R6.P4", "42P13"},
	{"V1 ROWS on a function without a set", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql ROWS 10 AS $$ BEGIN RETURN 1; END $$;", "null", "R6.P4", "22023"},
	{"V1 options read", "CREATE FUNCTION f() RETURNS SETOF int LANGUAGE plpgsql STABLE STRICT SECURITY DEFINER PARALLEL SAFE COST 10 ROWS 5 SET work_mem = 4096 AS $$ BEGIN RETURN; END $$;", "true", "", "ok"},
	{"V1 unknown argument type of a sql function", "CREATE FUNCTION f(a nosuchtype) RETURNS int LANGUAGE sql AS 'SELECT 1';", "null", "M2.P1", "42704"},
	// V1: the expression reader's precision (scan.l, gram.y).
	{"V1 chained comparison", "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN PERFORM 1 = 1 = 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 trailing junk after a number", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1abc; END $$;", "null", "R6.P14", "42601"},
	{"V1 prefix multiplication", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN * 1; END $$;", "null", "R6.P14", "42601"},
	{"V1 operator scan.l splits", "CREATE FUNCTION f() RETURNS boolean LANGUAGE plpgsql AS $$ BEGIN RETURN 1 <-1 = 0; END $$;", "null", "R6.P14", "42601"},
	{"V1 type modifiers that are no constants", "CREATE FUNCTION f() RETURNS text LANGUAGE plpgsql AS $$ BEGIN RETURN 1::varchar(1 2); END $$;", "null", "R6.P14", "42601"},
	{"V1 a keyword-named parameter assigned", "CREATE FUNCTION f(\"user\" int) RETURNS int LANGUAGE plpgsql AS $$ BEGIN user := 1; RETURN 1; END $$;", "null", "R6.P14", "42601"},
	// P7 (found while writing V1's expression reader): the scanner refuses
	// escapes the lexer read as strings.
	{"P7 invalid Unicode escape in an escape string", "CREATE TABLE t (a int);\nCOMMENT ON TABLE t IS E'\\u12';", "null", "", "22025"},
	{"P7 invalid Unicode escape in a Unicode string", "CREATE TABLE t (a int);\nCOMMENT ON TABLE t IS U&'\\00zz';", "null", "", "42601"},
	{"P7 invalid escape inside a DO body", "CREATE TABLE t (a int);\nDO $$ BEGIN COMMENT ON TABLE t IS E'\\u12'; END $$;", "null", "O1.P14", "22025"},
	{"P7 a valid escape", "CREATE TABLE t (a int);\nCOMMENT ON TABLE t IS E'\\x41\\n\\u00e9';", "true", "", "ok"},
	// V1: outside the subset, may fail where Postgres accepts.
	{"V1 loop", "DO $$ BEGIN FOR i IN 1..2 LOOP NULL; END LOOP; END $$;", "null", "O1.P14", "ok"},
	{"V1 record declaration", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE r record; BEGIN RETURN 1; END $$;", "null", "R6.P14", "ok"},
	// V1: check_function_bodies tracked like search_path (U3).
	{"V1 bodies off: an invalid body is created", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1 END $$;\nCREATE TABLE y (a int);", "true", "", "ok"},
	{"V1 bodies off: the call compiles it", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1 END $$;\nSELECT f();", "null", "O1.P14", "42601"},
	{"V1 bodies off: a valid body is called", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT f();\nCREATE TABLE y (a int);", "true", "", "ok"},
	{"V1 bodies reset", "SET check_function_bodies = off;\nRESET check_function_bodies;\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1 END $$;", "null", "R6.P14", "42601"},
	{"V1 bodies off by set_config", "SELECT set_config('check_function_bodies', 'off', false);\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1 END $$;", "null", "N3.P5", "ok"},
	{"V1 bodies off: a sql body is not parsed", "SET check_function_bodies = off;\nCREATE FUNCTION s() RETURNS int LANGUAGE sql AS $$ SELECT a FROM nosuch $$;", "true", "", "ok"},
	{"V1 bodies off: a sql pseudo-type result is refused", "SET check_function_bodies = off;\nCREATE FUNCTION s() RETURNS trigger LANGUAGE sql AS $$ SELECT 1 $$;", "null", "R6.P14", "42P13"},
	{"V1 a set type declared, then called", "CREATE TABLE t (a int);\nCREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE x t; BEGIN RETURN 1; END $$;\nSELECT f();", "null", "O1.P14", "ok"},
	// V5: rows code writes count for P11.
	{"V5 DO inserts, then SET NOT NULL", "CREATE TABLE t (a int);\nDO $$ BEGIN INSERT INTO t VALUES (NULL); END $$;\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"V5 DO inserts duplicates, then a unique index", "CREATE TABLE t (a int);\nDO $$ BEGIN INSERT INTO t VALUES (1), (1); END $$;\nCREATE UNIQUE INDEX t_a ON t (a);", "null", "M4.P11", "23505"},
	{"V5 DO inserts, then a foreign key", "CREATE TABLE p (id int PRIMARY KEY);\nCREATE TABLE c (pid int);\nDO $$ BEGIN INSERT INTO c VALUES (5); END $$;\nALTER TABLE c ADD FOREIGN KEY (pid) REFERENCES p;", "null", "M5f.P11", "23503"},
	{"V5 EXECUTE of a constant insert", "CREATE TABLE t (a int);\nDO $$ BEGIN EXECUTE $q$INSERT INTO t VALUES (NULL)$q$; END $$;\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"V5 a function updates, then SET NOT NULL", "CREATE TABLE t (a int);\nCREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN UPDATE t SET a = NULL; END $$;\nINSERT INTO t VALUES (1);\nSELECT f();\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"V5 a rule writes another table", "CREATE TABLE t (a int);\nCREATE TABLE l (a int);\nCREATE RULE r AS ON INSERT TO t DO ALSO INSERT INTO l VALUES (NULL);\nINSERT INTO t VALUES (1);\nALTER TABLE l ALTER COLUMN a SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"V5 DO writes another table only", "CREATE TABLE t (a int);\nCREATE TABLE u (a int);\nDO $$ BEGIN INSERT INTO u VALUES (NULL); END $$;\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", "null", "N3.P1", "ok"},
	{"V5 DO writes another table only (protected block)", "CREATE TABLE t (a int);\nCREATE TABLE u (a int);\nDO $$ BEGIN INSERT INTO u VALUES (NULL); EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nALTER TABLE t ALTER COLUMN a SET NOT NULL;", "true", "", "ok"},
	// V6: "$user" in the search path.
	{"V6 a schema named like the user", "CREATE SCHEMA graphvein;\nCREATE TABLE t (a int);", "null", "O3.P13", "ok"},
	{"V6 a schema of another name", "CREATE SCHEMA gv16d2_s;\nCREATE TABLE t (a int);", "null", "O3.P13", "ok"},
	{"V6 AUTHORIZATION CURRENT_USER", "CREATE SCHEMA AUTHORIZATION CURRENT_USER;\nCREATE TABLE t (a int);", "null", "", "ok"},
	{"V6 qualified creations are unaffected", "CREATE SCHEMA gv16d2_s;\nCREATE TABLE public.t (a int);", "true", "", "ok"},
	{"V6 the schema dropped again", "CREATE SCHEMA gv16d2_s;\nDROP SCHEMA gv16d2_s;\nCREATE TABLE t (a int);", "true", "", "ok"},
	// V8: names Postgres generates with tables code creates.
	{"V8 serial sequence", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id serial)'; END $$;\nCREATE SEQUENCE x_id_seq;", "null", "O1.P12", "42P07"},
	{"V8 serial sequence (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id serial)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE SEQUENCE x_id_seq;", "null", "R1.P12", "42P07"},
	{"V8 drop the serial sequence", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id serial)'; END $$;\nDROP SEQUENCE IF EXISTS x_id_seq;", "null", "O1.P12", "2BP01"},
	{"V8 drop the serial sequence (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id serial)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nDROP SEQUENCE IF EXISTS x_id_seq;", "null", "R5.P12", "2BP01"},
	{"V8 primary key index name", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int PRIMARY KEY)'; END $$;\nCREATE TABLE t0 (a int);\nCREATE INDEX x_pkey ON t0 (a);", "null", "O1.P12", "42P07"},
	{"V8 primary key index name (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int PRIMARY KEY)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE t0 (a int);\nCREATE INDEX x_pkey ON t0 (a);", "null", "M4.P12", "42P07"},
	{"V8 identity sequence as a table name", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int GENERATED ALWAYS AS IDENTITY)'; END $$;\nCREATE TABLE x_id_seq (a int);", "null", "O1.P12", "42P07"},
	{"V8 identity sequence as a table name (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int GENERATED ALWAYS AS IDENTITY)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE x_id_seq (a int);", "null", "M1.P12", "42P07"},
	{"V8 array type name", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int)'; END $$;\nCREATE TYPE _x AS ENUM ('a');", "null", "O1.P12", "ok"},
	{"V8 array type name (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TYPE _x AS ENUM ('a');", "null", "R1.P12", "ok"},
	{"V8 another name", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int)'; END $$;\nCREATE TABLE y (id int);", "null", "O1.P12", "ok"},
	{"V8 another name (protected block)", "DO $$ BEGIN EXECUTE 'CREATE TABLE x (id int)'; EXCEPTION WHEN OTHERS THEN RAISE NOTICE 'x'; END $$;\nCREATE TABLE y (id int);", "true", "", "ok"},
}

func TestRev16D2Cases(t *testing.T) {
	for _, tc := range rev16D2Cases {
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

func TestRev16D2CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev16D2Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

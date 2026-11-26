package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// Revision 1.7 cases (T018a2 W1-W7), as rev16D2Cases, each checked live by
// TestRev17CasesPostgres. Statements a migration runs in sqlx's
// transaction; a refused case's Postgres result is its SQLSTATE.
var rev17Cases = []b1dCase{
	{"W1 drop a trigger never created", "CREATE TABLE t (a int);\nDROP TRIGGER nosuch ON t;", "null", "N1.P1", "42704"},
	{"W1 drop a held trigger", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TRIGGER tr ON t;", "true", "", "ok"},
	{"W1 drop a trigger twice", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TRIGGER tr ON t;\nDROP TRIGGER tr ON t;", "null", "N1.P1", "42704"},
	{"W1 drop a trigger held on another table", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nCREATE TABLE u (a int);\nDROP TRIGGER tr ON u;", "null", "N1.P1", "42704"},
	{"W1 drop trigger if exists", "CREATE TABLE t (a int);\nDROP TRIGGER IF EXISTS nosuch ON t;", "true", "", "ok"},
	{"W1 comment on a held trigger", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nCOMMENT ON TRIGGER tr ON t IS 'x';", "true", "", "ok"},
	{"W1 comment on a trigger after the table is renamed away", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nALTER TABLE t RENAME TO t_old;\nCREATE TABLE t (a int);\nCOMMENT ON TRIGGER tr ON t IS 'x';", "null", "N1.P1", "42704"},
	{"W1 drop a trigger after the table is created again", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TABLE t;\nCREATE TABLE t (a int);\nDROP TRIGGER tr ON t;", "null", "N1.P1", "42704"},
	{"W1 a trigger created by code", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nDO $$ BEGIN EXECUTE 'CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION tf()'; END $$;\nDROP TRIGGER tr ON t;", "null", "O1.P12", "ok"},
	{"W1 drop a held policy", "CREATE TABLE t (a int);\nCREATE POLICY p ON t USING (true);\nDROP POLICY p ON t;", "true", "", "ok"},
	{"W1 alter a held policy", "CREATE TABLE t (a int);\nCREATE POLICY p ON t USING (true);\nALTER POLICY p ON t USING (false);", "true", "", "ok"},
	{"W1 alter a policy renamed away", "CREATE TABLE t (a int);\nCREATE POLICY p ON t USING (true);\nALTER POLICY p ON t RENAME TO q;\nALTER POLICY p ON t USING (false);", "null", "N1.P1", "42704"},
	{"W1 comment on a policy never created", "CREATE TABLE t (a int);\nCOMMENT ON POLICY nosuch ON t IS 'x';", "null", "N1.P1", "42704"},
	{"W1 drop a rule", "CREATE TABLE t (a int);\nDROP RULE nosuch ON t;", "null", "N1.P1", "42704"},
	{"W1 comment on a rule", "CREATE TABLE t (a int);\nCOMMENT ON RULE nosuch ON t IS 'x';", "null", "N1.P1", "42704"},
	{"W1 the axum helper's trigger, dropped by another name", "CREATE TABLE users (id int, updated_at timestamptz);\nCREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE FUNCTION trigger_updated_at(tablename regclass) RETURNS void AS $$ BEGIN EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION set_updated_at();', tablename); END; $$ LANGUAGE plpgsql;\nSELECT trigger_updated_at('users');\nDROP TRIGGER set_updated_at_users ON users;", "null", "N1.P1", "42704"},
	{"W1 the axum helper's trigger, dropped", "CREATE TABLE users (id int, updated_at timestamptz);\nCREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE FUNCTION trigger_updated_at(tablename regclass) RETURNS void AS $$ BEGIN EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION set_updated_at();', tablename); END; $$ LANGUAGE plpgsql;\nSELECT trigger_updated_at('users');\nDROP TRIGGER set_updated_at ON users;", "true", "", "ok"},
	{"W2 a regclass argument held", "CREATE TABLE users (id int);\nCREATE FUNCTION g(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT g('users');", "true", "", "ok"},
	{"W2 a regclass argument not held", "CREATE TABLE users (id int);\nCREATE FUNCTION g(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT g('userz');", "null", "O1.P1", "42P01"},
	{"W2 a named regclass argument not held", "CREATE TABLE users (id int);\nCREATE FUNCTION g(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT g(t => 'userz');", "null", "O1.P1", "42P01"},
	{"W2 a qualified regclass argument not held", "CREATE TABLE users (id int);\nCREATE FUNCTION g(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT g('public.userz');", "null", "O1.P1", "42P01"},
	{"W2 a regclass argument of another function", "CREATE FUNCTION g2(t regclass) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT g2('nosuch');", "null", "O1.P1", "42P01"},
	{"W2 a regnamespace argument", "CREATE FUNCTION h(n regnamespace) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT h('public');", "null", "O1.P1", "ok"},
	{"W2 a text argument", "CREATE FUNCTION k(n text) RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nSELECT k('userz');", "true", "", "ok"},
	{"W2 the axum helper with a held table", "CREATE TABLE users (id int, updated_at timestamptz);\nCREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE FUNCTION trigger_updated_at(tablename regclass) RETURNS void AS $$ BEGIN EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION set_updated_at();', tablename); END; $$ LANGUAGE plpgsql;\nSELECT trigger_updated_at('users');", "true", "", "ok"},
	{"W2 the axum helper with a missing table", "CREATE TABLE users (id int, updated_at timestamptz);\nCREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\nCREATE FUNCTION trigger_updated_at(tablename regclass) RETURNS void AS $$ BEGIN EXECUTE format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON %s FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION set_updated_at();', tablename); END; $$ LANGUAGE plpgsql;\nSELECT trigger_updated_at('userz');", "null", "O1.P1", "42P01"},
	{"W3 bodies off: a sql body with a syntax error is called", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1 +';\nSELECT f();", "null", "O1.P14", "42601"},
	{"W3 bodies off: a sql body naming a missing table is called", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS bigint LANGUAGE sql AS 'SELECT count(*) FROM nosuch_tbl';\nSELECT f();", "null", "O1.P14", "42P01"},
	{"W3 bodies off: a constant sql body is called", "SET check_function_bodies = off;\nCREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1';\nSELECT f();", "true", "", "ok"},
	{"W3 bodies on: a constant sql body is called", "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1';\nSELECT f();", "true", "", "ok"},
	{"W4 alter a database", "ALTER DATABASE nosuch_db SET work_mem = '1MB';", "null", "N1.P1", "3D000"},
	{"W4 grant on a database", "GRANT CONNECT ON DATABASE nosuch_db TO PUBLIC;", "null", "N1.P1", "3D000"},
	{"W4 revoke on a database", "REVOKE ALL ON DATABASE nosuch_db FROM PUBLIC;", "null", "N1.P1", "3D000"},
	{"W4 a role setting in a database", "ALTER ROLE CURRENT_USER IN DATABASE nosuch_db SET work_mem = '1MB';", "null", "N1.P1", "3D000"},
	{"W4 comment on a database", "COMMENT ON DATABASE nosuch_db IS 'x';", "true", "", "ok"},
	{"W5 drop statistics", "DROP STATISTICS nosuch;", "null", "N1.P1", "42704"},
	{"W5 drop statistics if exists", "DROP STATISTICS IF EXISTS nosuch;", "true", "", "ok"},
	{"W5 drop a publication not held", "DROP PUBLICATION nosuch;", "null", "N1.P1", "42704"},
	{"W5 drop a held publication", "CREATE PUBLICATION gv17_pub;\nDROP PUBLICATION gv17_pub;", "true", "", "ok"},
	{"W5 drop a publication twice", "CREATE PUBLICATION gv17_pub;\nDROP PUBLICATION gv17_pub;\nDROP PUBLICATION gv17_pub;", "null", "N1.P1", "42704"},
	{"W5 drop a collation not held", "DROP COLLATION nosuch;", "null", "R5.P1", "42704"},
	{"W5 drop a cast", "DROP CAST (int AS point);", "null", "N1.P1", "42704"},
	{"W5 drop an operator", "DROP OPERATOR === (int, int);", "null", "N1.P1", "42883"},
	{"W5 drop an aggregate", "DROP AGGREGATE nosuch(int);", "null", "R5.P1", "42883"},
	{"W6 code writes NULL into an inheritance child", "CREATE TABLE p (b int);\nCREATE TABLE c () INHERITS (p);\nDO $$ BEGIN INSERT INTO c VALUES (NULL); END $$;\nALTER TABLE p ALTER COLUMN b SET NOT NULL;", "null", "", "23502"},
	{"W6 code writes NULL into a partition", "CREATE TABLE p (k int, b int) PARTITION BY LIST (k);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);\nDO $$ BEGIN INSERT INTO p1 VALUES (1, NULL); END $$;\nALTER TABLE p ALTER COLUMN b SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"W6 a top-level NULL in a partition", "CREATE TABLE p (k int, b int) PARTITION BY LIST (k);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);\nINSERT INTO p1 VALUES (1, NULL);\nALTER TABLE p ALTER COLUMN b SET NOT NULL;", "null", "M5d.P11", "23502"},
	{"W6 a top-level row in a partition", "CREATE TABLE p (k int, b int) PARTITION BY LIST (k);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);\nINSERT INTO p1 VALUES (1, 2);\nALTER TABLE p ALTER COLUMN b SET NOT NULL;", "null", "M5d.P11", "ok"},
	{"W6 an empty partition", "CREATE TABLE p (k int, b int) PARTITION BY LIST (k);\nCREATE TABLE p1 PARTITION OF p FOR VALUES IN (1);\nALTER TABLE p ALTER COLUMN b SET NOT NULL;", "true", "", "ok"},
	{"W7 a hexadecimal escape label", "CREATE TYPE e AS ENUM (E'a\\x62');\nALTER TYPE e ADD VALUE 'ab';", "null", "R3.K2", "42710"},
	{"W7 a Unicode string label", "CREATE TYPE e AS ENUM (U&'\\0061');\nALTER TYPE e ADD VALUE 'a';", "null", "R3.K2", "42710"},
	{"W7 a decoded escape label", "CREATE TYPE e AS ENUM (E'x\\ty');\nALTER TYPE e ADD VALUE E'x\\ty';", "null", "R3.K2", "42710"},
	{"W7 an escape string equal to a plain label", "CREATE TYPE e AS ENUM ('ab');\nALTER TYPE e ADD VALUE E'ab';", "null", "R3.K2", "42710"},
	{"W7 a new label as an escape string", "CREATE TYPE e AS ENUM ('a');\nALTER TYPE e ADD VALUE E'c';", "true", "", "ok"},
}

func TestRev17Cases(t *testing.T) {
	for _, tc := range rev17Cases {
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

func TestRev17CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range rev17Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

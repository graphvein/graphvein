package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// b1dCase is one Revision 1.3 case: want is the applied state of 1_m.sql;
// cite is the precondition a possible failure names; pg is what Postgres
// 17 does with the migration ("ok" or the SQLSTATE that refuses it).
type b1dCase struct {
	name string
	sql  string
	want string
	cite string
	pg   string
}

const b1dTable = "CREATE TABLE p (a int, b text, v varchar(3), n numeric(5,2), f boolean, u uuid, d date, ts timestamptz, s smallint, nn int NOT NULL DEFAULT 0, dd text DEFAULT 'x', t2 timestamp DEFAULT now());\n"

const b1dTrigger = "CREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\n"

// b1dCases are the Revision 1.3 cases (T018a2 B1d): every "ok" case is
// accepted by Postgres 17 and every other one refused with the SQLSTATE
// given; TestB1dPostgres checks that against a live server.
var b1dCases = []b1dCase{
	// INSERT values (V1-V3) on a table without keys, checks or triggers.
	{"INSERT of valid constants", b1dTable + "INSERT INTO p (a, b, v, n, f, u, d, ts, s) VALUES (1, 'x', 'abc', 123.45, true, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', '2020-02-28', '2020-01-01 10:00', -5);\n", "true", "", "ok"},
	{"INSERT of string constants for typed columns", b1dTable + "INSERT INTO p (a, n, f, d, ts) VALUES ('12', '12.5', 'yes', '2020-01-01', '2020-01-01 10:00:30');\n", "true", "", "ok"},
	{"INSERT of NULL into nullable columns and several rows", b1dTable + "INSERT INTO p (a, b) VALUES (NULL, NULL), (-5, 'ab'), (DEFAULT, DEFAULT);\n", "true", "", "ok"},
	{"INSERT DEFAULT VALUES (constant and now() defaults)", b1dTable + "INSERT INTO p DEFAULT VALUES;\n", "true", "", "ok"},
	{"INSERT without a column list", b1dTable + "INSERT INTO p VALUES (1, 'x', 'ab ');\n", "true", "", "ok"},
	{"INSERT of a cast to the column's own type", b1dTable + "INSERT INTO p (a, n) VALUES ('7'::int, -999);\n", "true", "", "ok"},
	{"INSERT RETURNING columns and constants", b1dTable + "INSERT INTO p (a) VALUES (1) RETURNING a, b AS x, 1, *;\n", "true", "", "ok"},
	{"INSERT of invalid text for int (refused)", b1dTable + "INSERT INTO p (a) VALUES ('abc');\n", "null", "N3.P5", "22P02"},
	{"INSERT of an int out of range (refused)", b1dTable + "INSERT INTO p (a) VALUES (2147483648);\n", "null", "N3.P5", "22003"},
	{"INSERT of a smallint out of range (refused)", b1dTable + "INSERT INTO p (s) VALUES (40000);\n", "null", "N3.P5", "22003"},
	{"INSERT of a string too long (refused)", b1dTable + "INSERT INTO p (v) VALUES ('abcd');\n", "null", "N3.P5", "22001"},
	{"INSERT of a numeric beyond its precision (refused)", b1dTable + "INSERT INTO p (n) VALUES (1234.5);\n", "null", "N3.P5", "22003"},
	{"INSERT of NULL into NOT NULL (refused)", b1dTable + "INSERT INTO p (nn) VALUES (NULL);\n", "null", "N3.P5", "23502"},
	{"INSERT of an invalid date (refused)", b1dTable + "INSERT INTO p (d) VALUES ('2020-02-30');\n", "null", "N3.P5", "22008"},
	{"INSERT of year zero (refused)", b1dTable + "INSERT INTO p (d) VALUES ('0000-01-01');\n", "null", "N3.P5", "22008"},
	{"INSERT of an invalid uuid (refused)", b1dTable + "INSERT INTO p (u) VALUES ('nope');\n", "null", "N3.P5", "22P02"},
	{"INSERT of a zone offset out of range (refused)", b1dTable + "INSERT INTO p (ts) VALUES ('2020-01-01 10:00:00+99');\n", "null", "N3.P5", "22009"},
	{"INSERT of a division by zero (refused)", b1dTable + "INSERT INTO p (a) VALUES (1/0);\n", "null", "N3.P5", "22012"},
	{"INSERT of an aggregate (refused)", b1dTable + "INSERT INTO p (a) VALUES (count(*));\n", "null", "N3.P5", "42803"},
	{"INSERT of an int into text (accepted, not proven)", b1dTable + "INSERT INTO p (b) VALUES (1);\n", "null", "N3.P5", "ok"},
	{"INSERT of a decimal into int (accepted, not proven)", b1dTable + "INSERT INTO p (a) VALUES (1.5);\n", "null", "N3.P5", "ok"},
	{"INSERT of a function (accepted, not proven)", b1dTable + "INSERT INTO p (d) VALUES (current_date);\n", "null", "N3.P5", "ok"},
	{"INSERT RETURNING an expression (accepted, not proven)", b1dTable + "INSERT INTO p (a) VALUES (1) RETURNING a + 1;\n", "null", "N3.P5", "ok"},
	// Constraints and defaults a new row meets.
	{"INSERT omitting a NOT NULL column without default (refused)", "CREATE TABLE q (a int, b int NOT NULL);\nINSERT INTO q (a) VALUES (1);\n", "null", "N3.P5", "23502"},
	{"INSERT breaking a CHECK (refused)", "CREATE TABLE q (a int CHECK (a > 0));\nINSERT INTO q VALUES (0);\n", "null", "N3.P5", "23514"},
	{"INSERT meeting a CHECK (accepted, not proven)", "CREATE TABLE q (a int CHECK (a > 0));\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"INSERT of a duplicate key (refused)", "CREATE TABLE q (a int UNIQUE);\nINSERT INTO q VALUES (1), (1);\n", "null", "N3.P5", "23505"},
	// B2 (V5): a single row into a table proven empty meets its keys.
	{"INSERT into an empty table with a primary key (B2: proven)", "CREATE TABLE q (a int PRIMARY KEY);\nINSERT INTO q VALUES (1);\n", "true", "", "ok"},
	{"INSERT of a missing referenced row (refused)", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r);\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "23503"},
	// B2 (V6): the first value of a fresh sequence.
	{"INSERT into an empty serial table (B2: proven)", "CREATE TABLE q (id serial, a int);\nINSERT INTO q (a) VALUES (1);\n", "true", "", "ok"},
	{"INSERT into a table with an expression index (accepted, not proven)", "CREATE TABLE q (a int);\nCREATE INDEX ON q ((a + 1));\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"INSERT into a table with a trigger (accepted, not proven)", b1dTrigger + "CREATE TABLE q (a int);\nCREATE TRIGGER tg BEFORE INSERT ON q FOR EACH ROW EXECUTE FUNCTION tf();\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"INSERT into a table without a trigger in a set with one", b1dTrigger + "CREATE TABLE q (a int);\nCREATE TABLE w (a int);\nCREATE TRIGGER tg BEFORE INSERT ON q FOR EACH ROW EXECUTE FUNCTION tf();\nINSERT INTO w VALUES (1);\n", "true", "", "ok"},
	// UPDATE.
	{"UPDATE of constants, a column copy and DEFAULT", b1dTable + "UPDATE p SET b = 'y', dd = b, nn = DEFAULT WHERE a = 1 AND b IS NOT NULL;\n", "true", "", "ok"},
	{"UPDATE WHERE with a string for int and parentheses", b1dTable + "UPDATE p SET b = NULL WHERE (a = '1' OR NOT (b <> 'x')) AND true;\n", "true", "", "ok"},
	{"UPDATE RETURNING", b1dTable + "UPDATE p SET a = 2 RETURNING a, p.b;\n", "true", "", "ok"},
	{"UPDATE of invalid text (refused)", b1dTable + "UPDATE p SET a = 'x';\n", "null", "N3.P5", "22P02"},
	{"UPDATE of an aggregate (refused)", b1dTable + "UPDATE p SET a = count(*);\n", "null", "N3.P5", "42803"},
	{"UPDATE of a window function (refused)", b1dTable + "UPDATE p SET a = row_number() OVER ();\n", "null", "N3.P5", "42P20"},
	{"UPDATE WHERE of mismatched types (refused)", b1dTable + "UPDATE p SET a = 1 WHERE b = 1;\n", "null", "N3.P5", "42883"},
	{"UPDATE WHERE an aggregate (refused)", b1dTable + "UPDATE p SET a = 1 WHERE count(*) > 0;\n", "null", "N3.P5", "42803"},
	{"UPDATE WHERE invalid text for int (refused)", b1dTable + "UPDATE p SET a = 1 WHERE a = 'x';\n", "null", "N3.P5", "22P02"},
	{"UPDATE of NULL into NOT NULL with rows (refused)", b1dTable + "INSERT INTO p (a) VALUES (1);\nUPDATE p SET nn = NULL;\n", "null", "N3.P5", "23502"},
	{"UPDATE copying a nullable column into NOT NULL (accepted, not proven)", b1dTable + "UPDATE p SET nn = a;\n", "null", "N3.P5", "ok"},
	{"UPDATE of an expression (accepted, not proven)", b1dTable + "UPDATE p SET a = a + 1;\n", "null", "N3.P5", "ok"},
	{"UPDATE of a referenced column with rows (refused)", "CREATE TABLE r (id int PRIMARY KEY, x int);\nCREATE TABLE q (a int REFERENCES r);\nINSERT INTO r VALUES (1, 0);\nINSERT INTO q VALUES (1);\nUPDATE r SET id = 2;\n", "null", "N3.P5", "23503"},
	{"UPDATE of an unreferenced column of a referenced table", "CREATE TABLE r (id int PRIMARY KEY, x int);\nCREATE TABLE q (a int REFERENCES r);\nUPDATE r SET x = 2 WHERE id = 1;\n", "true", "", "ok"},
	{"UPDATE of a key column (accepted, not proven)", "CREATE TABLE r (id int PRIMARY KEY, x int);\nUPDATE r SET id = 2;\n", "null", "N3.P5", "ok"},
	// DELETE.
	{"DELETE WHERE a proven condition", b1dTable + "DELETE FROM p WHERE a = 1 OR b IS NULL;\n", "true", "", "ok"},
	{"DELETE of a referenced row (refused)", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r);\nINSERT INTO r VALUES (1);\nINSERT INTO q VALUES (1);\nDELETE FROM r;\n", "null", "N3.P5", "23503"},
	{"DELETE from a referenced table (accepted, not proven)", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r);\nDELETE FROM r;\n", "null", "N3.P5", "ok"},
	{"DELETE from the referencing table", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r);\nDELETE FROM q WHERE a = 1;\n", "true", "", "ok"},
	{"DELETE WHERE a function (accepted, not proven)", b1dTable + "DELETE FROM p WHERE length(b) > 1;\n", "null", "N3.P5", "ok"},
	{"DELETE of an outside table (accepted, not proven)", "DELETE FROM ext;\n", "null", "N3.P5", "ok"},
	// Queries.
	{"SELECT of constants", "SELECT 1, 'x' AS y, NULL, true, -1;\n", "true", "", "ok"},
	{"VALUES of constants", "VALUES (1, 'a'), (2, 'b');\n", "true", "", "ok"},
	{"SELECT of a division by zero (refused)", "SELECT 1/0;\n", "null", "N3.P5", "22012"},
	{"SELECT of an invalid cast (refused)", "SELECT 'abc'::int;\n", "null", "N3.P5", "22P02"},
	{"VALUES of an invalid cast (refused)", "VALUES ('x'::int);\n", "null", "N3.P5", "22P02"},
	{"SELECT of a function (accepted, not proven)", "SELECT now();\n", "null", "N3.P5", "ok"},
	// N1 column references (N1.P5).
	{"CREATE TRIGGER UPDATE OF held columns", b1dTrigger + b1dTable + "CREATE TRIGGER tg BEFORE UPDATE OF a, b ON p FOR EACH ROW EXECUTE FUNCTION tf();\n", "true", "", "ok"},
	{"CREATE TRIGGER UPDATE OF a missing column (refused)", b1dTrigger + b1dTable + "CREATE TRIGGER tg BEFORE UPDATE OF nosuch ON p FOR EACH ROW EXECUTE FUNCTION tf();\n", "null", "N1.P5", "42703"},
	{"CREATE TRIGGER UPDATE OF a column twice (refused)", b1dTrigger + b1dTable + "CREATE TRIGGER tg BEFORE INSERT OR UPDATE OF a, a ON p FOR EACH ROW EXECUTE FUNCTION tf();\n", "null", "N1.P5", "42701"},
	{"CREATE TRIGGER WHEN (accepted, not proven)", b1dTrigger + b1dTable + "CREATE TRIGGER tg BEFORE UPDATE ON p FOR EACH ROW WHEN (NEW.a > 0) EXECUTE FUNCTION tf();\n", "null", "N1.P5", "ok"},
	{"CREATE TRIGGER WHEN a missing column (refused)", b1dTrigger + b1dTable + "CREATE TRIGGER tg BEFORE UPDATE ON p FOR EACH ROW WHEN (NEW.nosuch > 0) EXECUTE FUNCTION tf();\n", "null", "N1.P5", "42703"},
	{"CREATE POLICY USING (true)", b1dTable + "CREATE POLICY pol ON p USING (true);\n", "true", "", "ok"},
	{"CREATE POLICY USING a column (accepted, not proven)", b1dTable + "CREATE POLICY pol ON p USING (a > 0);\n", "null", "N1.P5", "ok"},
	{"CREATE POLICY USING a missing column (refused)", b1dTable + "CREATE POLICY pol ON p USING (nosuch > 0);\n", "null", "N1.P5", "42703"},
	{"CREATE POLICY WITH CHECK of a mismatched type (refused)", b1dTable + "CREATE POLICY pol ON p WITH CHECK (b = 1);\n", "null", "N1.P5", "42883"},
	{"GRANT with column lists", b1dTable + "GRANT SELECT (a, b), UPDATE (b) ON p TO PUBLIC;\n", "true", "", "ok"},
	{"GRANT of a missing column (refused)", b1dTable + "GRANT SELECT (nosuch) ON TABLE p TO PUBLIC;\n", "null", "N1.P5", "42703"},
	{"REVOKE of a missing column (refused)", b1dTable + "REVOKE SELECT (nosuch) ON p FROM PUBLIC;\n", "null", "N1.P5", "42703"},
}

func TestB1dValues(t *testing.T) {
	for _, tc := range b1dCases {
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

// TestB1dPostgres checks each case's pg verdict against a live server (the
// postgres CI job, or GRAPHVEIN_TEST_POSTGRES_URL): every migration runs
// in a transaction that is rolled back, with a table ext outside the set.
func TestB1dPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range b1dCases {
		t.Run(tc.name, func(t *testing.T) {
			got := runRolledBack(ctx, t, db, "CREATE TABLE ext (id int);\n"+tc.sql)
			if got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// runRolledBack runs sql as one simple-protocol query string in a schema
// of its own inside a transaction it rolls back, and returns "ok" or the
// SQLSTATE of the refusal.
func runRolledBack(ctx context.Context, t *testing.T, db *sql.DB, sqlText string) string {
	t.Helper()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	schema := fmt.Sprintf("b1d_%d", len(sqlText))
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s; SET LOCAL search_path TO %s", schema, schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		if pe, ok := readPgError(err); ok {
			return pe.code
		}
		return strings.TrimSpace(err.Error())
	}
	return "ok"
}

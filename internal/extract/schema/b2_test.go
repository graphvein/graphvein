package schema

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// b2LiteralCases pin the literal grammars (literals.go, T018a2 B2): each
// case is a string constant assigned to a column of the type, whether the
// grammar proves it, and what Postgres 17 answers ("ok" or the SQLSTATE).
// A proven case must be "ok"; the refused cases are mostly the B1d
// follow-up (a) forms that Postgres refuses, and a few forms Postgres
// accepts that the grammars leave unproven (narrower on purpose).
var b2LiteralCases = []struct {
	typ, lit string
	valid    bool
	pg       string
}{
	{"numeric", "", false, "22P02"},
	{"numeric", ".", false, "22P02"},
	{"numeric", "+", false, "22P02"},
	{"numeric", "-", false, "22P02"},
	{"numeric", "1e", false, "22P02"},
	{"numeric", "1.", true, "ok"},
	{"numeric", ".5", true, "ok"},
	{"numeric", " -12.5e3 ", true, "ok"},
	{"numeric", "1e100000", false, "ok"},
	{"numeric", "1 ", false, "22P02"},
	{"numeric(5,2)", "123.45", true, "ok"},
	{"numeric(5,2)", "1234.5", false, "22003"},
	{"int4", " 42\t", true, "ok"},
	{"int4", "42 ", false, "22P02"},
	{"int4", " 42", false, "22P02"},
	{"int4", "+7", true, "ok"},
	{"int2", "40000", false, "22003"},
	{"int8", "1_000", false, "ok"},
	{"float4", "1e39", false, "22003"},
	{"float4", "1e-46", false, "22003"},
	{"float4", "3.4e38", true, "ok"},
	{"float4", "1.5", true, "ok"},
	{"float8", "1e309", false, "22003"},
	{"float8", "1e-400", false, "22003"},
	{"float8", "0.0", true, "ok"},
	{"float8", "-Infinity", true, "ok"},
	{"float8", "0x10", false, "ok"},
	{"float8", "1 ", false, "22P02"},
	{"bool", "yes", true, "ok"},
	{"bool", " on ", true, "ok"},
	{"bool", "on ", false, "22P02"},
	{"uuid", "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", true, "ok"},
	{"uuid", " a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", false, "22P02"},
	{"date", "0000-01-01", false, "22008"},
	{"date", "0001-01-01", true, "ok"},
	{"date", "2024-02-29", true, "ok"},
	{"date", "2023-02-29", false, "22008"},
	{"date", " today ", true, "ok"},
	{"timestamptz", "2024-01-01 10:00:00+99", false, "22009"},
	{"timestamptz", "2024-01-01 10:00:00+16", false, "22009"},
	{"timestamptz", "2024-01-01 10:00:00+15:59", true, "ok"},
	{"timestamptz", "2024-01-01 10:00:00+05:60", false, "22009"},
	{"timestamptz", "2024-01-01T10:00:00.123456-0530", true, "ok"},
	{"timestamp", "0000-06-01 00:00", false, "22008"},
	{"timestamp", "2024-01-01 10:00 ", false, "22007"},
	{"interval", "1 day", true, "ok"},
	{"interval", "1 day 2 days", false, "22007"},
	{"interval", "1 week 2 days 3 hours", true, "ok"},
	{"interval", "-1.5 years", true, "ok"},
	{"jsonb", `"\u0000"`, false, "22P05"},
	{"json", `"\ud800"`, false, "ok"},
	{"jsonb", `{"a": [1, 2.5e3, null, true]}`, true, "ok"},
	{"jsonb", `1e100000`, false, "ok"},
	{"json", ` [] `, true, "ok"},
	{"text", "anything at all", true, "ok"},
	{"varchar(3)", "abcd", false, "22001"},
	{"varchar(3)", "abc", true, "ok"},
	{"int4[]", " {} ", true, "ok"},
	{"int4[]", "{} ", false, "22P02"},
	{"bytea", `\x0a1B`, true, "ok"},
	{"bytea", `\x0`, false, "22023"},
}

func TestB2Literals(t *testing.T) {
	e := &exprEnv{}
	for _, tc := range b2LiteralCases {
		if got := e.validLiteral(tc.lit, tc.typ); got != tc.valid {
			t.Errorf("validLiteral(%q, %s) = %v, want %v", tc.lit, tc.typ, got, tc.valid)
		}
		if tc.valid && tc.pg != "ok" {
			t.Errorf("case %q %s: a proven literal must be accepted by Postgres", tc.lit, tc.typ)
		}
	}
}

// literalSQL returns the statements that assign lit to a column of typ.
func literalSQL(typ, lit string) string {
	return fmt.Sprintf("CREATE TABLE lit (c %s);\nINSERT INTO lit VALUES ('%s');", typ, strings.ReplaceAll(lit, "'", "''"))
}

func TestB2LiteralsPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range b2LiteralCases {
		if got := runRolledBack(ctx, t, db, literalSQL(tc.typ, tc.lit)); got != tc.pg {
			t.Errorf("Postgres on %q %s: %s, the case says %s", tc.lit, tc.typ, got, tc.pg)
		}
	}
	// Random literals: whatever a grammar proves, Postgres must accept.
	alphabet := []string{"0", "1", "5", "9", "-", "+", ".", "e", "E", " ", "\t", " ", ":", "T", "x", "a", "f",
		"{", "}", "[", "]", `"`, ",", "\\", "u", "_", "day", "year", "s", "n", "t", "inf", "nan", "2024-02-29", "0000",
		"12:30", "+15", "+16", "99", "now", "null", "true"}
	types := []string{"int2", "int4", "int8", "numeric", "numeric(4,1)", "float4", "float8", "bool", "uuid", "date",
		"timestamp", "timestamptz", "interval", "json", "jsonb", "bytea", "varchar(4)", "int4[]"}
	r := rand.New(rand.NewPCG(18, 2))
	e := &exprEnv{}
	proven := 0
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := r.IntN(6) + 1; n > 0; n-- {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		lit := b.String()
		typ := types[r.IntN(len(types))]
		if !e.validLiteral(lit, typ) {
			continue
		}
		proven++
		if got := runRolledBack(ctx, t, db, literalSQL(typ, lit)); got != "ok" {
			t.Errorf("proven literal %q for %s refused by Postgres: %s", lit, typ, got)
		}
	}
	t.Logf("random literals proven and accepted: %d", proven)
}

// b2Cases are the B2 coverage rows (V5 empty tables, V6 fresh sequences,
// ON CONFLICT DO NOTHING arbiters, MATCH SIMPLE foreign keys with a NULL
// column), each with valid and refused variants; same shape as b1dCases.
var b2Cases = []b1dCase{
	{"single row into an empty table with keys", "CREATE TABLE q (id uuid PRIMARY KEY, name text NOT NULL UNIQUE);\nINSERT INTO q (id, name) VALUES ('ddf8994f-d522-4659-8d02-c1d479057be6', 'admin');\n", "true", "", "ok"},
	{"two rows into an empty table with a key (refused)", "CREATE TABLE q (a int PRIMARY KEY);\nINSERT INTO q VALUES (1), (1);\n", "null", "N3.P5", "23505"},
	{"second insert into a keyed table (refused)", "CREATE TABLE q (a int PRIMARY KEY);\nINSERT INTO q VALUES (1);\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "23505"},
	{"insert after an opaque statement (accepted, not proven)", "CREATE TABLE q (a int PRIMARY KEY);\nDO $$ BEGIN INSERT INTO q VALUES (1); END $$;\nINSERT INTO q VALUES (1);\n", "null", "N3.P1", "23505"},
	{"insert after an UPDATE keeps the table empty", "CREATE TABLE q (a int PRIMARY KEY, b int);\nUPDATE q SET b = 1;\nINSERT INTO q VALUES (1, 2);\n", "true", "", "ok"},
	{"insert after an insert into another table", "CREATE TABLE q (a int PRIMARY KEY);\nCREATE TABLE w (a int PRIMARY KEY);\nINSERT INTO w VALUES (1);\nINSERT INTO q VALUES (1);\n", "true", "", "ok"},
	{"insert into a table after CREATE TABLE AS of it (accepted, not proven)", "CREATE TABLE q (a int PRIMARY KEY);\nCREATE TABLE z AS SELECT 1 AS a;\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"serial default in a set that calls nextval (accepted, not proven)", "CREATE TABLE q (id serial PRIMARY KEY, a int);\nCREATE FUNCTION f() RETURNS bigint LANGUAGE plpgsql AS $$ BEGIN RETURN nextval('q_id_seq'); END $$;\nINSERT INTO q (a) VALUES (1);\n", "null", "N3.P5", "ok"},
	{"identity default into an empty table", "CREATE TABLE q (id int GENERATED ALWAYS AS IDENTITY PRIMARY KEY, a int);\nINSERT INTO q (a) VALUES (1);\n", "true", "", "ok"},
	{"identity default with DEFAULT written", "CREATE TABLE q (id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, a int);\nINSERT INTO q VALUES (DEFAULT, 1);\n", "true", "", "ok"},
	{"identity ALWAYS with a value (refused)", "CREATE TABLE q (id int GENERATED ALWAYS AS IDENTITY, a int);\nINSERT INTO q VALUES (5, 1);\n", "null", "", "428C9"},
	{"ON CONFLICT DO NOTHING on a table with rows", "CREATE TABLE q (a int PRIMARY KEY, b text UNIQUE);\nINSERT INTO q VALUES (1, 'x');\nINSERT INTO q VALUES (1, 'y') ON CONFLICT DO NOTHING;\n", "true", "", "ok"},
	{"ON CONFLICT DO NOTHING with a deferrable key (refused)", "CREATE TABLE q (a int UNIQUE DEFERRABLE INITIALLY DEFERRED);\nINSERT INTO q VALUES (1);\nINSERT INTO q VALUES (1) ON CONFLICT DO NOTHING;\n", "null", "N3.P5", "55000"},
	{"ON CONFLICT DO NOTHING with a NOT NULL violation (refused)", "CREATE TABLE q (a int PRIMARY KEY, b int NOT NULL);\nINSERT INTO q (a) VALUES (1) ON CONFLICT DO NOTHING;\n", "null", "N3.P5", "23502"},
	{"foreign key with a NULL column", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r, b int);\nINSERT INTO q VALUES (NULL, 1);\n", "true", "", "ok"},
	{"foreign key of an omitted column", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r, b int);\nINSERT INTO q (b) VALUES (1);\n", "true", "", "ok"},
	{"foreign key with a value (refused)", "CREATE TABLE r (id int PRIMARY KEY);\nCREATE TABLE q (a int REFERENCES r, b int);\nINSERT INTO q VALUES (1, 1);\n", "null", "N3.P5", "23503"},
	{"MATCH FULL foreign key with one NULL column (refused)", "CREATE TABLE r (x int, y int, PRIMARY KEY (x, y));\nCREATE TABLE q (a int, b int, FOREIGN KEY (a, b) REFERENCES r MATCH FULL);\nINSERT INTO q VALUES (NULL, 1);\n", "null", "N3.P5", "23503"},
	{"MATCH SIMPLE foreign key with one NULL column", "CREATE TABLE r (x int, y int, PRIMARY KEY (x, y));\nCREATE TABLE q (a int, b int, FOREIGN KEY (a, b) REFERENCES r MATCH SIMPLE);\nINSERT INTO q VALUES (NULL, 1);\n", "true", "", "ok"},
	{"empty table with a CHECK (accepted, not proven)", "CREATE TABLE q (a int PRIMARY KEY CHECK (a > 0));\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"empty table with an expression index (accepted, not proven)", "CREATE TABLE q (a int);\nCREATE UNIQUE INDEX ON q ((a + 1));\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"empty table after INSERT ... SELECT (refused)", "CREATE TABLE q (a int PRIMARY KEY);\nINSERT INTO q SELECT 1;\nINSERT INTO q VALUES (1);\n", "null", "N3.P1", "23505"},
	// O1t and trigger names (N1.P2).
	{"O1t call of a regclass template", b2O1t("tablename regclass", "%s") + "CREATE TABLE \"user\" (a int, updated_at timestamptz);\nSELECT trigger_updated_at('\"user\"');\n", "true", "", "ok"},
	{"O1t call of a text template with %I", b2O1t("tablename text", "%I") + "CREATE TABLE q (a int);\nSELECT trigger_updated_at('q');\n", "true", "", "ok"},
	{"O1t call twice on one table (refused)", b2O1t("tablename regclass", "%s") + "CREATE TABLE q (a int);\nSELECT trigger_updated_at('q');\nSELECT trigger_updated_at('Q');\n", "null", "N1.P2", "42710"},
	{"O1t call of a dropped table (refused)", b2O1t("tablename regclass", "%s") + "CREATE TABLE gone (a int);\nDROP TABLE gone;\nSELECT trigger_updated_at('gone');\n", "null", "", "42P01"},
	// Not modelled: an O1 call; the body's EXECUTE of a format() result
	// makes its inventory unbounded (Revision 1.5 U2, P12).
	{"O1t call of a regclass template with %I (not modelled)", b2O1t("tablename regclass", "%I") + "CREATE TABLE q (a int);\nSELECT trigger_updated_at('q');\n", "null", "O1.P12", "ok"},
	{"O1t: an INSERT later meets the trigger (accepted, not proven)", b2O1t("tablename regclass", "%s") + "CREATE TABLE q (a int);\nSELECT trigger_updated_at('q');\nINSERT INTO q VALUES (1);\n", "null", "N3.P5", "ok"},
	{"WHEN OLD IS DISTINCT FROM NEW on INSERT (refused)", b1dTrigger + "CREATE TABLE q (a int);\nCREATE TRIGGER tg BEFORE INSERT ON q FOR EACH ROW WHEN (OLD IS DISTINCT FROM NEW) EXECUTE FUNCTION tf();\n", "null", "N1.P5", "42P17"},
	{"CREATE TRIGGER twice (refused)", b1dTrigger + "CREATE TABLE q (a int);\nCREATE TRIGGER tg BEFORE INSERT ON q FOR EACH ROW EXECUTE FUNCTION tf();\nCREATE TRIGGER tg BEFORE UPDATE ON q FOR EACH ROW EXECUTE FUNCTION tf();\n", "null", "N1.P2", "42710"},
	{"CREATE TRIGGER after DROP TRIGGER", b1dTrigger + "CREATE TABLE q (a int);\nCREATE TRIGGER tg BEFORE INSERT ON q FOR EACH ROW EXECUTE FUNCTION tf();\nDROP TRIGGER tg ON q;\nCREATE TRIGGER tg BEFORE UPDATE ON q FOR EACH ROW EXECUTE FUNCTION tf();\n", "true", "", "ok"},
}

// b2O1t returns the set_updated_at trigger function and a
// trigger_updated_at function of the O1t form with the given parameter
// and format specifier.
func b2O1t(param, spec string) string {
	return "CREATE FUNCTION set_updated_at() RETURNS trigger AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END; $$ LANGUAGE plpgsql;\n" +
		"CREATE OR REPLACE FUNCTION trigger_updated_at(" + param + ") RETURNS void AS $$\nbegin\n  execute format('CREATE TRIGGER set_updated_at BEFORE UPDATE ON " + spec + " FOR EACH ROW WHEN (OLD is distinct from NEW) EXECUTE FUNCTION set_updated_at();', tablename);\nend;\n$$ LANGUAGE plpgsql;\n"
}

func TestB2Cases(t *testing.T) {
	for _, tc := range b2Cases {
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

func TestB2CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range b2Cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, "CREATE TABLE ext (id int);\n"+tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

package schema

import (
	"strings"
	"testing"
)

func statementTexts(t *testing.T, src string) ([]string, *lexFailure) {
	t.Helper()
	stmts, failure := lexStatements([]byte(src))
	var out []string
	for _, s := range stmts {
		out = append(out, src[s.start:s.end])
	}
	return out, failure
}

func TestLexStatementsP1(t *testing.T) {
	src := "-- leading; comment\n" +
		"/* outer /* inner; */ still; */ select 1;\n" +
		"select E'it\\'s; ok', 'a''b;c';\n" +
		"create function f() returns int language sql as $fn$ select 1; $fn$;\n" +
		"select $1, a$b$c from t; " +
		"select \"semi;\"\"colon\" from u;\n" +
		"CREATE FUNCTION g() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; SELECT 2; END;\n" +
		"select 3"
	got, failure := statementTexts(t, src)
	if failure != nil {
		t.Fatal(failure)
	}
	want := []string{
		"select 1;",
		"select E'it\\'s; ok', 'a''b;c';",
		"create function f() returns int language sql as $fn$ select 1; $fn$;",
		"select $1, a$b$c from t;",
		"select \"semi;\"\"colon\" from u;",
		"CREATE FUNCTION g() RETURNS int LANGUAGE sql BEGIN ATOMIC SELECT CASE WHEN true THEN 1 END; SELECT 2; END;",
		"select 3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("statements:\n%q\nwant\n%q", got, want)
	}
	stmts, _ := lexStatements([]byte(src))
	if v := stmts[1].tokens[1].value; v != "it's; ok" {
		t.Fatalf("E string value %q", v)
	}
	if v := stmts[4].tokens[1].value; v != `semi;"colon` {
		t.Fatalf("quoted identifier %q", v)
	}
	if k := stmts[3].tokens[1].kind; k != tokParam {
		t.Fatalf("$1 lexed as %d", k)
	}
	if v := stmts[3].tokens[3].value; v != "a$b$c" {
		t.Fatalf("identifier with $ lexed as %q", v)
	}
}

func TestLexFailures(t *testing.T) {
	for _, tc := range []struct{ src, reason string }{
		{"select 1; select 'open", "unterminated string"},
		{"select 1; create function f() as $$ begin", "unterminated dollar-quoted string"},
		{"select 1; /* open /* nested */", "unterminated block comment"},
		{"select 1; select \"open", "unterminated quoted identifier"},
	} {
		got, failure := statementTexts(t, tc.src)
		if failure == nil || failure.reason != tc.reason || failure.start != 10 || len(got) != 1 {
			t.Fatalf("%q: %v %+v", tc.src, got, failure)
		}
	}
}

func TestFoldIdentifierP2(t *testing.T) {
	if got := foldIdentifier("User_ÄB"); got != "user_Äb" {
		t.Fatalf("non-ASCII bytes must be kept: %q", got)
	}
	long := strings.Repeat("A", 70)
	if got := foldIdentifier(long); got != strings.Repeat("a", 63) {
		t.Fatalf("truncation: %q", got)
	}
	// 62 ASCII bytes and a two-byte character: the character does not fit.
	edge := strings.Repeat("a", 62) + "é"
	if got := truncateIdentifier(edge); got != strings.Repeat("a", 62) {
		t.Fatalf("character boundary: %q", got)
	}
	stmts, _ := lexStatements([]byte(`select "MixedCase", U&"d\0061t\+000061"`))
	if stmts[0].tokens[1].value != "MixedCase" || stmts[0].tokens[3].value != "data" {
		t.Fatalf("quoted forms: %+v", stmts[0].tokens)
	}
}

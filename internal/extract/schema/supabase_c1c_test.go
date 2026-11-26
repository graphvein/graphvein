package schema

import (
	"context"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// T024c1c cases of R1b.P5's analysed form (sqlbody.go): a LANGUAGE sql AS
// body that Postgres parse-analyses at creation, as rev16D2Cases, each
// checked live by TestSupabaseC1cCasesPostgres. A case the reader proves
// is "true" and Postgres creates it ("ok"); every other case is "null"
// (R1b.P5, may fail) whatever Postgres does.
var supabaseC1cCases = []b1dCase{
	{"EXISTS over a held table with a parameter", "CREATE TABLE files (id uuid PRIMARY KEY, sharing text NOT NULL);\nCREATE FUNCTION f(p_name text) RETURNS boolean LANGUAGE sql SECURITY DEFINER AS $$ SELECT EXISTS (SELECT 1 FROM files WHERE (id::text = (string_to_array(p_name, '/'))[2]) AND sharing <> 'private'); $$;", "true", "", "ok"},
	{"a column from a table, STABLE", "CREATE TABLE t (id int PRIMARY KEY, name text);\nCREATE FUNCTION f(p int) RETURNS text LANGUAGE sql STABLE AS $$ SELECT name FROM t WHERE id = p $$;", "true", "", "ok"},
	{"a column through the alias", "CREATE TABLE t (id int PRIMARY KEY);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t AS x WHERE x.id = p) $$;", "true", "", "ok"},
	{"a subscript of a parenthesised array", "CREATE FUNCTION f(p text) RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT (string_to_array(p, '/'))[2] $$;", "true", "", "ok"},
	{"a parameter in the output list only", "CREATE FUNCTION f(p int) RETURNS int LANGUAGE sql AS 'SELECT p';", "true", "", "ok"},
	{"a later DROP TABLE of the body's table (an AS string records no dependency)", "CREATE TABLE t (id int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t WHERE id = p) $$;\nDROP TABLE t;", "true", "", "ok"},
	{"a column shadows the parameter of its name", "CREATE TABLE files (sharing text, p_name int);\nCREATE FUNCTION f(p_name text) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM files WHERE sharing = p_name) $$;", "null", "R1b.P5", "42883"},
	{"a missing column", "CREATE TABLE files (id uuid, sharing text);\nCREATE FUNCTION f(p_name text) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM files WHERE sharing = p_name AND nosuch = 1) $$;", "null", "R1b.P5", "42703"},
	{"a missing table", "CREATE FUNCTION f(p_name text) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM nosuch WHERE p_name = 'a') $$;", "null", "R1b.P5", "42P01"},
	{"the result type differs", "CREATE TABLE files (sharing text);\nCREATE FUNCTION f(p_name text) RETURNS int LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM files WHERE sharing = p_name) $$;", "null", "R1b.P5", "42P13"},
	{"the table name after an alias", "CREATE TABLE t (id int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t AS x WHERE t.id = p) $$;", "null", "R1b.P5", "42P01"},
	{"an outer column reference", "CREATE TABLE t (id int);\nCREATE TABLE u (a int);\nCREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT id FROM t WHERE EXISTS (SELECT 1 FROM u WHERE a = id) $$;", "null", "R1b.P5", "ok"},
	{"a function outside the set and pg_catalog", "CREATE FUNCTION f(p text) RETURNS boolean LANGUAGE sql AS $$ SELECT nosuch(p) $$;", "null", "R1b.P5", "42883"},
	{"a set function named like a pg_catalog function", "CREATE FUNCTION lower(x int) RETURNS int LANGUAGE sql AS 'SELECT 1';\nCREATE FUNCTION f(p text) RETURNS text LANGUAGE sql AS $$ SELECT lower(p) $$;", "null", "R1b.P5", "ok"},
	{"a SET clause changes the search_path", "CREATE TABLE t (id int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql SET search_path = public AS $$ SELECT EXISTS (SELECT 1 FROM t WHERE id = p) $$;", "null", "R1b.P5", "42P01"},
	{"two statements", "CREATE TABLE t (id int);\nCREATE FUNCTION f(p int) RETURNS int LANGUAGE sql AS $$ SELECT 1; SELECT id FROM t WHERE id = p $$;", "null", "R1b.P5", "ok"},
	{"a polymorphic parameter", "CREATE FUNCTION f(p anyelement) RETURNS boolean LANGUAGE sql AS $$ SELECT nosuch = p $$;", "null", "R1b.P5", "ok"},
	{"after a statement naming row_security", "SET row_security = off;\nCREATE TABLE t (id int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t WHERE id = p) $$;", "null", "R1b.P5", "ok"},
	{"a table created IF NOT EXISTS over a view", "CREATE VIEW t AS SELECT 1 AS id;\nCREATE TABLE IF NOT EXISTS t (id int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t WHERE id = p) $$;", "null", "", "ok"},
	{"a join", "CREATE TABLE t (id int);\nCREATE TABLE u (a int);\nCREATE FUNCTION f(p int) RETURNS boolean LANGUAGE sql AS $$ SELECT EXISTS (SELECT 1 FROM t JOIN u ON u.a = t.id WHERE t.id = p) $$;", "null", "R1b.P5", "ok"},
}

// supabaseC1cPlatformCases are Supabase sets (SB-M8); the live check
// creates the two storage functions with the platform's signatures first
// (supabaseStorageStub). Each is "true" only when the reader proves it.
var supabaseC1cPlatformCases = []b1dCase{
	{"storage.foldername as chatbot-ui calls it", "create table files (id uuid primary key, sharing text not null default 'private');\ncreate or replace function public_file(p_name text) returns boolean language sql security definer as $$ select exists (select 1 from files where (id::text = (storage.foldername(p_name))[2]) and sharing <> 'private'); $$;", "true", "", "ok"},
	{"storage.filename", "create table files (id uuid primary key, sharing text);\ncreate function public_file(p_name text) returns boolean language sql as $$ select exists (select 1 from files where (id::text = (storage.filename(p_name))) and sharing <> 'private') $$;", "true", "", "ok"},
	{"storage.foldername of an integer", "create function f(p int) returns text language sql as $$ select (storage.foldername(p))[1] $$;", "null", "R1b.P5", "42883"},
	{"storage.foldername subscripted twice", "create function f(p text) returns text language sql as $$ select ((storage.foldername(p))[1])[1] $$;", "null", "R1b.P5", "42804"},
	{"the set creates a function of the name", "create function storage.foldername(n int) returns int language sql as 'select 1';\ncreate function f(p text) returns text language sql as $$ select (storage.foldername(p))[1] $$;", "null", "R1b.P5", "ok"},
	{"another storage function", "create function f(p text) returns text language sql as $$ select storage.extension(p) $$;", "null", "R1b.P5", "42883"},
}

// supabaseStorageStub gives the live check the platform's storage
// functions with the signatures of platformFunctions.
const supabaseStorageStub = "CREATE SCHEMA IF NOT EXISTS storage;\n" +
	"CREATE OR REPLACE FUNCTION storage.foldername(name text) RETURNS text[] LANGUAGE sql IMMUTABLE AS $$ SELECT string_to_array(name, '/') $$;\n" +
	"CREATE OR REPLACE FUNCTION storage.filename(name text) RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT name $$;\n"

func TestSupabaseC1cCases(t *testing.T) {
	for _, tc := range supabaseC1cCases {
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
	for _, tc := range supabaseC1cPlatformCases {
		t.Run("supabase/"+tc.name, func(t *testing.T) {
			l := supabaseLinked(t, map[string]string{"1_a.sql": tc.sql})
			want := any(nil)
			if tc.want == "true" {
				want = true
			}
			if got := appliedOf(l)["1_a.sql"]; got != want {
				t.Fatalf("applied %v, want %v; %v", got, want, l.diagnostics)
			}
			if tc.cite != "" && !cites(l.diagnostics, tc.cite) {
				t.Fatalf("no diagnostic cites %s: %v", tc.cite, l.diagnostics)
			}
			if tc.want == "true" && tc.pg != "ok" {
				t.Fatalf("the case claims applied, but Postgres refuses it (%s)", tc.pg)
			}
		})
	}
	// SB-M8 is Supabase's: in a sqlx set the platform functions are not
	// known, so the chatbot-ui form is not proven.
	applied, diagnostics := linkSQL(t, supabaseC1cPlatformCases[0].sql)
	if applied["1_m.sql"] != "null" || !cites(diagnostics, "R1b.P5") {
		t.Errorf("sqlx: applied %v; %v", applied, diagnostics)
	}
}

func TestSupabaseC1cCasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range supabaseC1cCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
	for _, tc := range supabaseC1cPlatformCases {
		t.Run("supabase/"+tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, supabaseStorageStub+tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

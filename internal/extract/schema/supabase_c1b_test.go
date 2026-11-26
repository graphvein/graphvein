package schema

import (
	"context"
	"slices"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
)

// T024c1b cases of the reader rows the Supabase corpora reach (R6.P14's
// PL/pgSQL subset: SELECT ... INTO, a function call in FROM, a variable
// named like an unreserved keyword; N2.P5's search_path; N1.P4's Y09
// public.f), as rev16D2Cases, each checked live by
// TestSupabaseC1bCasesPostgres.
var supabaseC1bCases = []b1dCase{
	{"SELECT INTO OUT parameters", "CREATE TABLE n (owner int, body text);\nCREATE FUNCTION f(p int, OUT total int, OUT content text) LANGUAGE plpgsql AS $$ BEGIN SELECT INTO total, content count(*)::int, max(body) FROM n WHERE n.owner = p; END $$;", "true", "", "ok"},
	{"SELECT INTO STRICT declared variables from a function", "CREATE FUNCTION g(OUT a int, OUT b text) LANGUAGE plpgsql AS $$ BEGIN a := 1; b := 'x'; END $$;\nCREATE FUNCTION f() RETURNS text LANGUAGE plpgsql AS $$ DECLARE n int; content text; BEGIN SELECT r.a, r.b INTO STRICT n, content FROM public.g() AS r; RETURN content || n::text; END $$;", "true", "", "ok"},
	{"SELECT INTO one target after the list", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int; BEGIN SELECT 1 INTO n; RETURN n; END $$;", "true", "", "ok"},
	{"SELECT without INTO compiles", "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN SELECT 1; END $$;", "true", "", "ok"},
	{"PERFORM from a function call", "CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $$ BEGIN PERFORM 1 FROM generate_series(1, 2) g; END $$;", "true", "", "ok"},
	{"SELECT INTO a composite and a scalar", "CREATE TABLE t (a int);\nCREATE FUNCTION f(INOUT r t, INOUT b int) LANGUAGE plpgsql AS $$ BEGIN SELECT INTO r, b 1, 2; END $$;", "null", "R6.P14", "42601"},
	{"SELECT INTO twice", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int; m int; BEGIN SELECT 1 INTO n INTO m; RETURN n; END $$;", "null", "R6.P14", "42601"},
	{"SELECT INTO an unknown name", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ BEGIN SELECT 1 INTO nosuch; RETURN 1; END $$;", "null", "R6.P14", "42601"},
	{"SELECT INTO inside parentheses", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int; BEGIN SELECT (SELECT 1 INTO n); RETURN n; END $$;", "null", "R6.P14", "ok"},
	{"SELECT INTO a misspelled query", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE n int; BEGIN SELECT INTO n 1 FORM x; RETURN n; END $$;", "null", "R6.P14", "42601"},
	{"a variable named like an unreserved keyword", "CREATE FUNCTION f() RETURNS text LANGUAGE plpgsql AS $$ DECLARE content text := 'a'; BEGIN content := content || 'b'; RETURN content; END $$;", "true", "", "ok"},
	{"a variable named like a reserved keyword", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql AS $$ DECLARE user int; BEGIN RETURN 1; END $$;", "null", "R6.P14", "ok"},
	{"char_length CHECKs on text columns", "CREATE TABLE t (a text CHECK (char_length(a) <= 100), b text CHECK (char_length(b) >= 3 AND char_length(b) <= 25), c text NOT NULL DEFAULT '' CHECK (char_length(c) < 1000));", "true", "", "ok"},
	{"char_length CHECK on a varchar column (overload resolution not proven)", "CREATE TABLE t (b varchar(20) CHECK (char_length(b) <= 25));", "null", "M1.P5", "ok"},
	{"char_length CHECK over an integer column", "CREATE TABLE t (a int CHECK (char_length(a) <= 100));", "null", "M1.P5", "42883"},
	{"an index named like the generated shape", "CREATE TABLE t (user_id uuid, w int);\nCREATE INDEX t_user_id_idx ON t (user_id);\nCREATE INDEX t_w_idx ON t (w);", "true", "", "ok"},
	{"an index name an unnamed index took", "CREATE TABLE t (w int);\nCREATE INDEX ON t (w);\nCREATE INDEX t_w_idx ON t (w);", "null", "", "42P07"},
	{"a routine's SET search_path", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql SECURITY DEFINER SET search_path = public AS $$ BEGIN RETURN 1; END $$;", "true", "", "ok"},
	{"a routine's SET search_path to an empty string", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql SET search_path = '' AS $$ BEGIN RETURN 1; END $$;", "true", "", "ok"},
	{"a routine's SET search_path to a missing schema", "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql SET search_path TO nosuch_schema AS $$ BEGIN RETURN 1; END $$;", "true", "", "ok"},
}

// supabaseC1bTriggerCases name public.f, which the live check's scratch
// schema (runRolledBack's search_path) cannot hold: the Supabase oracle
// replays the fixture's trigger on public.touch_profile instead
// (TestSupabaseOracle, app/20240104000000_functions.sql).
var supabaseC1bTriggerCases = []b1dCase{
	{"a trigger naming public.f for the set's f", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION public.tf();", "true", "", "ok"},
	{"a trigger naming public.f for a set f that is no trigger function", "CREATE TABLE t (a int);\nCREATE FUNCTION tf() RETURNS int LANGUAGE plpgsql AS $$ BEGIN RETURN 1; END $$;\nCREATE TRIGGER tr BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION public.tf();", "null", "N1.P4", "42P17"},
	{"a trigger naming public.f after a search_path change", "CREATE SCHEMA s;\nSET search_path = s, public;\nCREATE TABLE public.t (a int);\nCREATE FUNCTION tf() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;\nCREATE TRIGGER tr BEFORE UPDATE ON public.t FOR EACH ROW EXECUTE FUNCTION public.tf();", "null", "", "42883"},
}

func TestSupabaseC1bCases(t *testing.T) {
	for _, tc := range append(slices.Clone(supabaseC1bCases), supabaseC1bTriggerCases...) {
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

func TestSupabaseC1bCasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range supabaseC1bCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runRolledBack(ctx, t, db, tc.sql); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// platformAssumptions reads the SB-V3 / SB-V4 assumptions of the set's
// migration records.
func platformAssumptions(t *testing.T, nodes map[string]contract.Node) []string {
	t.Helper()
	var out []string
	for _, n := range nodes {
		if n.Kind != "migration" {
			continue
		}
		p, ok := n.Attributes["platform"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no platform attribute", n.Name)
		}
		for _, a := range p["assumptions"].([]any) {
			m := a.(map[string]any)
			out = append(out, m["id"].(string)+": "+m["text"].(string))
		}
		return out
	}
	return out
}

// TestSupabasePlatformExtensions checks SB-M4: in a Supabase set, CREATE
// EXTENSION of a platform extension is not superuser-only (assumption
// SB-V4, shown per extension); other untrusted extensions and sqlx sets
// keep Y51.
func TestSupabasePlatformExtensions(t *testing.T) {
	l := supabaseLinked(t, map[string]string{
		"1_a.sql": "create extension if not exists vector with schema extensions;\ncreate extension http with schema extensions;\ncreate table a (id int primary key);",
	})
	if applied := appliedOf(l); applied["1_a.sql"] != true {
		t.Fatalf("applied %v: %v", applied, l.diagnostics)
	}
	want := []string{"SB-V4: " + supabaseExtensionText("http"), "SB-V4: " + supabaseExtensionText("vector")}
	if got := platformAssumptions(t, l.nodes); !slices.Equal(got, want) {
		t.Errorf("assumptions %v, want %v", got, want)
	}
	// An untrusted extension outside the list: as before (Y51, O2.P4).
	l = supabaseLinked(t, map[string]string{"1_a.sql": "create extension postgis;\ncreate table a (id int primary key);"})
	if applied := appliedOf(l); applied["1_a.sql"] != nil {
		t.Errorf("postgis: applied %v", applied)
	}
	if got := platformAssumptions(t, l.nodes); len(got) != 0 {
		t.Errorf("postgis: assumptions %v", got)
	}
	// A sqlx set: the platform list does not apply.
	applied, diagnostics := linkSQL(t, "CREATE EXTENSION vector;\nCREATE TABLE a (id int PRIMARY KEY);")
	if applied["1_m.sql"] != "null" || !cites(diagnostics, "O2.P4") {
		t.Errorf("sqlx: applied %v; %v", applied, diagnostics)
	}
}

// TestSupabaseBareDirectory checks SB-M5: a supabase/migrations directory
// without config.toml whose entries all follow the CLI's name pattern is a
// Supabase set assuming SB-V3, with the directory as its package; any other
// entry leaves it no set.
func TestSupabaseBareDirectory(t *testing.T) {
	nodes, _, _ := Link(Input{Sources: map[string][]byte{
		"supabase/migrations/1_a.sql": []byte("create table a (id int primary key);"),
		"supabase/migrations/2_b.sql": []byte("create table b (id int primary key);"),
	}})
	byID := map[string]contract.Node{}
	tables := 0
	for _, n := range nodes {
		byID[n.Identity] = n
		switch n.Kind {
		case "migration":
			if n.Attributes["tool"] != "supabase" || n.Attributes["package"] != "supabase/migrations" || n.Attributes["applied"] != true {
				t.Errorf("%s: %v", n.Name, n.Attributes)
			}
		case "table":
			tables++
		}
	}
	if tables != 2 {
		t.Fatalf("tables %d: %v", tables, nodes)
	}
	if got := platformAssumptions(t, byID); !slices.Equal(got, []string{"SB-V3: " + supabaseBareText}) {
		t.Errorf("assumptions %v", got)
	}
	for _, extra := range []string{"README.md", "3_c.down.txt", "notes.sql"} {
		nodes, _, _ := Link(Input{Sources: map[string][]byte{
			"supabase/migrations/1_a.sql":  []byte("create table a (id int primary key);"),
			"supabase/migrations/" + extra: []byte("x"),
		}})
		if len(nodes) != 0 {
			t.Errorf("%s beside the migrations: a set %v", extra, nodes)
		}
	}
	// With config.toml it is SB-M1's set, without SB-V3.
	l := supabaseLinked(t, map[string]string{"1_a.sql": "create table a (id int primary key);"})
	if got := platformAssumptions(t, l.nodes); len(got) != 0 {
		t.Errorf("config.toml set: assumptions %v", got)
	}
	// A sqlx set's directory is never a bare Supabase set.
	nodes, _, _ = Link(Input{Sources: map[string][]byte{
		"supabase/Cargo.toml":         []byte(pgManifest),
		"supabase/migrations/1_a.sql": []byte("CREATE TABLE a (id int PRIMARY KEY);"),
	}})
	for _, n := range nodes {
		if n.Kind == "migration" && n.Attributes["tool"] != "sqlx" {
			t.Errorf("%s: tool %v", n.Name, n.Attributes["tool"])
		}
	}
}

// TestSupabasePlatformEffects checks SB-M6 and SB-M7: a platform
// extension created into a named schema and an insert into
// storage.buckets no trigger of the set fires on are no opaque
// statements, so a later CREATE TABLE IF NOT EXISTS is complete; the
// guards keep the opaque reading.
func TestSupabasePlatformEffects(t *testing.T) {
	state := func(files map[string]string, table string) any {
		t.Helper()
		l := supabaseLinked(t, files)
		for _, n := range l.nodes {
			if n.Kind == "table" && n.Name == table {
				return n.Attributes["state"]
			}
		}
		t.Fatalf("no table %s", table)
		return nil
	}
	after := "\ncreate table if not exists a (id int primary key);"
	trigger := "create function tf() returns trigger language plpgsql as $$ begin return new; end $$;\n"
	cases := []struct {
		name, sql, want string
	}{
		{"extension into extensions", "create extension if not exists vector with schema extensions;", "complete"},
		{"extension without a schema", "create extension vector;", "partial"},
		{"extension into public", "create extension http with schema public;", "partial"},
		{"extension with a version", "create extension vector with schema extensions version '0.8.7';", "partial"},
		{"the set creates an extension object name", "create function vector_dims(int) returns int language sql as 'select 1';\ncreate extension vector with schema extensions;", "partial"},
		{"the set creates an operator", "create extension vector with schema extensions;\ncreate operator === (leftarg = int, rightarg = int, function = int4eq);", "partial"},
		{"bucket insert in a set with triggers", trigger + "insert into storage.buckets (id, name) values ('b', 'b');", "complete"},
		{"bucket insert with a key to buckets", trigger + "create table k (b text references storage.buckets (id));\ninsert into storage.buckets (id, name) values ('b', 'b');", "complete"},
		{"bucket insert with a trigger on buckets", trigger + "create trigger tb before insert on storage.buckets for each row execute function tf();\ninsert into storage.buckets (id, name) values ('b', 'b');", "partial"},
		{"bucket insert with dynamic code", "create function d() returns void language plpgsql as $$ begin execute 'select 1'; end $$;\n" + trigger + "insert into storage.buckets (id, name) values ('b', 'b');", "partial"},
		{"another platform table", trigger + "insert into storage.objects (id) values (gen_random_uuid());", "partial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := state(map[string]string{"1_a.sql": tc.sql + after}, "a"); got != tc.want {
				t.Errorf("a is %v, want %s", got, tc.want)
			}
		})
	}
	l := supabaseLinked(t, map[string]string{"1_a.sql": trigger + "insert into storage.buckets (id, name) values ('b', 'b');"})
	want := []string{"SB-V5: " + supabaseInsertText("insert into storage.buckets (id, name) values ('b', 'b')")}
	if got := platformAssumptions(t, l.nodes); !slices.Equal(got, want) {
		t.Errorf("assumptions %v, want %v", got, want)
	}
}

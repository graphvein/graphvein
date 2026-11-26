package schema

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// NX-F2 cases of rows SB-M9 (row level security across data statements)
// and SB-M10 (column types of platform extensions). Each SB-M9 case is a
// Supabase set of one migration with a table t whose row level security
// the set enables; want is the rls enabled flag the pass records for t
// ("true", or "null" when unknown). TestSupabaseNXF2CasesPostgres runs
// each case on PostgreSQL 17 and reads pg_class.relrowsecurity: a known
// flag must be Postgres's.
var supabaseRLSDataCases = []struct {
	name, sql, want string
}{
	{"an UPDATE whose trigger only assigns NEW", `CREATE TABLE t (id int PRIMARY KEY, v text, at timestamptz);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN NEW.at = now(); RETURN NEW; END; $$;
CREATE TRIGGER t_touch BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION touch();
UPDATE t SET v = 'b' WHERE id = 1;`, "true"},
	{"a DELETE in a set whose trigger function writes a table", `CREATE TABLE log (id int);
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE FUNCTION audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO log (id) VALUES (NEW.id); RETURN NEW; END; $$;
CREATE TRIGGER t_audit AFTER INSERT ON log FOR EACH ROW EXECUTE FUNCTION audit();
DELETE FROM t WHERE id = 1;`, "true"},
	{"a trigger function that disables row level security", `CREATE TABLE t (id int PRIMARY KEY, v text);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION off() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ALTER TABLE t DISABLE ROW LEVEL SECURITY; RETURN NEW; END; $$;
CREATE TRIGGER u_off BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION off();
INSERT INTO u (id) VALUES (1);`, "null"},
	{"a trigger function with dynamic EXECUTE", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION dyn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'ALTER TABLE t DISABLE ROW LEVEL SECURITY'; RETURN NEW; END; $$;
CREATE TRIGGER u_dyn BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION dyn();
INSERT INTO u (id) VALUES (1);`, "null"},
	{"a trigger function that creates a policy", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION pol() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN CREATE POLICY p ON t FOR SELECT USING (true); RETURN NEW; END; $$;
CREATE TRIGGER u_pol BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION pol();
INSERT INTO u (id) VALUES (1);`, "null"},
	{"a trigger function that sets row_security", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION rs() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM set_config('row_security', 'on', true); RETURN NEW; END; $$;
CREATE TRIGGER u_rs BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION rs();
INSERT INTO u (id) VALUES (1);`, "null"},
	{"after a DO block", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END; $$;
DO $$ BEGIN PERFORM 1; END $$;
UPDATE t SET id = 2 WHERE id = 1;`, "null"},
	{"a data statement calling a set function that disables row level security", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION off() RETURNS int LANGUAGE plpgsql AS $$ BEGIN ALTER TABLE t DISABLE ROW LEVEL SECURITY; RETURN 1; END; $$;
INSERT INTO u (id) VALUES (off());`, "null"},
	{"a function in another language", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE TABLE u (id int);
CREATE FUNCTION f() RETURNS int LANGUAGE sql AS 'SELECT 1';
CREATE FUNCTION g() RETURNS trigger LANGUAGE plperl AS 'return;';
UPDATE u SET id = 2;`, "null"},
}

// supabaseVectorCases are SB-M10 cases: a Supabase set that creates the
// platform extensions in extensions (SB-M6) and then a table. want is the
// applied state of the migration ("true"; "refused" when the pass knows
// Postgres refuses the column, M2.P5, which leaves the migration's applied
// state null; "null" when not proven); pg is what PostgreSQL 17.12 with
// pgvector 0.8.7 and pgsql-http 1.7 does ("ok" or the SQLSTATE).
var supabaseVectorCases = []struct {
	name, sql, want, pg string
}{
	{"vector(384), unqualified through the platform search_path", "CREATE TABLE t (e vector(384));", "true", "ok"},
	{"vector without dimensions", "CREATE TABLE t (e vector);", "true", "ok"},
	{"qualified halfvec at its maximum", "CREATE TABLE t (e extensions.halfvec(16000));", "true", "ok"},
	{"sparsevec at its maximum", "CREATE TABLE t (e sparsevec(1000000000));", "true", "ok"},
	{"an array of vector", "CREATE TABLE t (e vector(3)[]);", "true", "ok"},
	{"an http composite type and a domain array", "CREATE TABLE t (h http_header, m http_method[]);", "true", "ok"},
	{"ADD COLUMN of vector", "CREATE TABLE t (id int);\nALTER TABLE t ADD COLUMN e vector(3);", "true", "ok"},
	{"vector(0)", "CREATE TABLE t (e vector(0));", "refused", "22023"},
	{"vector above 16000", "CREATE TABLE t (e vector(16001));", "refused", "22023"},
	{"two modifiers", "CREATE TABLE t (e vector(3,4));", "refused", "22023"},
	{"sparsevec above its maximum", "CREATE TABLE t (e sparsevec(1000000001));", "refused", "22023"},
	{"a modifier on an http type", "CREATE TABLE t (h http_header(3));", "refused", "42601"},
	{"a string modifier", "CREATE TABLE t (e vector('3'));", "null", "ok"},
	{"another schema", "CREATE TABLE t (e public.vector(3));", "null", "42704"},
	{"a search_path without the extension's schema", "SET search_path = public;\nCREATE TABLE t (e vector(3));", "null", "42704"},
	{"a set type of the name", "CREATE TYPE public.vector AS (x int);\nCREATE TABLE t (e vector);", "null", "ok"},
	{"after a DO block", "DO $$ BEGIN PERFORM 1; END $$;\nCREATE TABLE t (e vector(3));", "null", "ok"},
}

// supabaseVectorPrelude is the extensions migration of the SB-M10 cases.
const supabaseVectorPrelude = "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA extensions;\nCREATE EXTENSION http WITH SCHEMA extensions;\n"

// rlsEnabledOf returns the rls enabled flag the pass records for table t.
func rlsEnabledOf(l linked) (any, bool) {
	for _, n := range l.nodes {
		if n.Kind == "table" && n.Name == "t" {
			rls, _ := n.Attributes["rls"].(map[string]any)
			return rls["enabled"], true
		}
	}
	return nil, false
}

func TestSupabaseNXF2Cases(t *testing.T) {
	for _, tc := range supabaseRLSDataCases {
		t.Run("SB-M9/"+tc.name, func(t *testing.T) {
			l := supabaseLinked(t, map[string]string{"1_a.sql": tc.sql})
			got, ok := rlsEnabledOf(l)
			if !ok {
				t.Fatalf("no table t: %v", l.diagnostics)
			}
			if fmt.Sprint(got) != strings.Replace(tc.want, "null", "<nil>", 1) {
				t.Fatalf("rls enabled %v, want %s; %v", got, tc.want, l.diagnostics)
			}
		})
	}
	for _, tc := range supabaseVectorCases {
		t.Run("SB-M10/"+tc.name, func(t *testing.T) {
			l := supabaseLinked(t, map[string]string{"1_ext.sql": supabaseVectorPrelude, "2_t.sql": tc.sql})
			applied := appliedOf(l)
			if tc.want == "true" && applied["1_ext.sql"] != true {
				t.Fatalf("the extensions migration: %v; %v", applied, l.diagnostics)
			}
			want := map[string]string{"true": "true", "refused": "<nil>", "null": "<nil>"}[tc.want]
			if got := fmt.Sprint(applied["2_t.sql"]); got != want {
				t.Fatalf("applied %s, want %s; %v", got, tc.want, l.diagnostics)
			}
			refused := false
			for _, d := range l.diagnostics {
				refused = refused || strings.Contains(d.Message, "(Postgres refuses)"+precondition("M2.P5"))
			}
			if refused != (tc.want == "refused") {
				t.Fatalf("refusal %v, want %s; %v", refused, tc.want, l.diagnostics)
			}
			if tc.want == "true" && tc.pg != "ok" || tc.want == "refused" && tc.pg == "ok" {
				t.Fatalf("the case claims %s, but Postgres says %s", tc.want, tc.pg)
			}
		})
	}
	// Both rows are Supabase's: in a sqlx set the data statement stays
	// opaque for row level security (no rls attribute at all), and vector
	// is not a type the pass holds.
	applied, diagnostics := linkSQL(t, "CREATE EXTENSION IF NOT EXISTS vector WITH SCHEMA extensions;\nCREATE TABLE t (e vector(3));")
	if applied["1_m.sql"] == "true" {
		t.Errorf("sqlx: applied %v; %v", applied, diagnostics)
	}
}

// TestSupabaseNXF2CasesPostgres runs the cases on PostgreSQL 17. The
// SB-M10 cases need pgvector and pgsql-http on the server (the PGDG builds
// postgresql-17-pgvector 0.8.7 and postgresql-17-http 1.7); where they are
// not available the subtests are skipped.
func TestSupabaseNXF2CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range supabaseRLSDataCases {
		t.Run("SB-M9/"+tc.name, func(t *testing.T) {
			enabled := rlsLive(ctx, t, db, tc.sql)
			if tc.want == "true" && !enabled {
				t.Fatalf("the pass records rls enabled, Postgres has it disabled")
			}
		})
	}
	var available int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_available_extensions WHERE name IN ('vector', 'http')`).Scan(&available); err != nil {
		t.Fatal(err)
	}
	for _, tc := range supabaseVectorCases {
		t.Run("SB-M10/"+tc.name, func(t *testing.T) {
			if available != 2 {
				t.Skip("pgvector and pgsql-http are not installed on this server")
			}
			sqlText := `CREATE SCHEMA extensions; SET LOCAL search_path TO "$user", public, extensions;` + "\n" + supabaseVectorPrelude + tc.sql
			if got := runPlain(ctx, t, db, sqlText); got != tc.pg {
				t.Fatalf("Postgres: %s, the case says %s", got, tc.pg)
			}
		})
	}
}

// rlsLive runs a case in a rolled-back transaction in a fresh schema and
// reads relrowsecurity of its table t.
func rlsLive(ctx context.Context, t *testing.T, db *sql.DB, sqlText string) bool {
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
	if _, err := conn.ExecContext(ctx, "CREATE SCHEMA nxf2_rls; SET LOCAL search_path TO nxf2_rls"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		// plperl may not be installed: the case's claim is null then.
		t.Logf("Postgres: %v", err)
		return false
	}
	var enabled bool
	if err := conn.QueryRowContext(ctx, "SELECT relrowsecurity FROM pg_class WHERE oid = 'nxf2_rls.t'::regclass").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	return enabled
}

// runPlain runs a case in a rolled-back transaction as written and
// returns "ok" or the SQLSTATE.
func runPlain(ctx context.Context, t *testing.T, db *sql.DB, sqlText string) string {
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
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		if pe, ok := readPgError(err); ok {
			return pe.code
		}
		return strings.TrimSpace(err.Error())
	}
	return "ok"
}

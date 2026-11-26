package schema

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/graphvein/graphvein/internal/egress"
)

// T024 review cases of row SB-M9: a data statement on a relation the
// set does not hold (a pg_catalog relation among them), at top level or in
// code a data statement runs, makes the row level security flags and the
// policies of every held table unknown. Each case is a Supabase set of one
// migration with a table t whose row level security the set enables and
// on which it creates the policy p. enabled is the rls enabled flag the
// pass records for t and policies the number of policies it lists ("null"
// when unknown). TestSupabaseB5CasesPostgres runs each case on PostgreSQL
// 17 as a superuser (the platform's postgres role is not one, so there the
// catalog writes are refused): a known value must be Postgres's. unit
// cases name public, which the live run's scratch schema is not, so they
// are not run live (chatbot-ui's public-qualified inserts in its trigger
// functions are, by TestSupabaseOracle).
var supabaseB5Cases = []struct {
	name, sql, enabled, policies string
	unit                         bool
}{
	{"the review's probe: catalog writes after a trigger", `CREATE TABLE t (id int PRIMARY KEY, v text);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION f();
UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't';
DELETE FROM pg_catalog.pg_policy WHERE polname = 'p';`, "null", "null", false},
	{"an unqualified catalog UPDATE in a set without code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
UPDATE pg_class SET relrowsecurity = false WHERE relname = 't';`, "null", "null", false},
	{"a catalog DELETE in a set without code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
DELETE FROM pg_policy WHERE polname = 'p';`, "null", "null", false},
	{"a trigger function that writes a catalog", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE FUNCTION off() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't'; DELETE FROM pg_policy WHERE polname = 'p'; RETURN NEW; END $$;
CREATE TRIGGER u_off BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION off();
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"a DO block that writes a catalog", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
DO $$ BEGIN DELETE FROM pg_catalog.pg_policy WHERE polname = 'p'; END $$;`, "null", "null", false},
	{"a DO block with dynamic EXECUTE", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
DO $$ BEGIN EXECUTE 'DROP POLICY p ON ' || 't'; END $$;`, "null", "null", false},
	{"a data statement on a view in a set with code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE VIEW w AS SELECT id FROM u;
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION f();
UPDATE w SET id = 2;`, "null", "null", false},
	{"a catalog write, then ENABLE again", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
UPDATE pg_class SET relrowsecurity = false WHERE relname = 't';
ALTER TABLE t ENABLE ROW LEVEL SECURITY;`, "true", "null", false},
	{"data statements on held tables, with and without code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO t (id) VALUES (1);
CREATE TABLE u (id int);
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN DELETE FROM u WHERE id = 0; RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION f();
INSERT INTO u (id) VALUES (1);
DELETE FROM t WHERE id = 1;`, "true", "1", false},
	{"a data statement on an information_schema table", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
UPDATE information_schema.sql_features SET comments = 'x' WHERE false;`, "null", "null", false},
	{"a public-qualified target the set created unqualified", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO public.t (id) VALUES (NEW.id); RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION f();
INSERT INTO public.u (id) VALUES (1);`, "true", "1", true},
	{"a public-qualified view", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE VIEW w AS SELECT id FROM u;
UPDATE public.w SET id = 2;`, "null", "null", true},
	{"another schema's table of a held name", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE users (id int);
INSERT INTO auth.users (id) VALUES (1);`, "null", "null", true},
	// Stacked gate NX-S: SB-M9 does not read rule actions.
	{"a rule whose actions write the catalogs, in a set with code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE RULE r AS ON INSERT TO u DO ALSO (UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't'; DELETE FROM pg_catalog.pg_policy WHERE polname = 'p');
CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER tr BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION f();
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"a rule whose actions write the catalogs, in a set without code", `CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
CREATE TABLE u (id int);
CREATE RULE r AS ON INSERT TO u DO ALSO (UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't'; DELETE FROM pg_catalog.pg_policy WHERE polname = 'p');
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"a rule created before the table it disables", `CREATE TABLE u (id int);
CREATE OR REPLACE RULE r AS ON UPDATE TO u DO ALSO UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't';
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO u (id) VALUES (1);
UPDATE u SET id = 2;`, "null", "null", false},
	{"a rule that does nothing, then ENABLE again", `CREATE TABLE t (id int PRIMARY KEY);
CREATE POLICY p ON t USING (true);
CREATE RULE r AS ON DELETE TO t DO INSTEAD NOTHING;
ALTER TABLE t ENABLE ROW LEVEL SECURITY;`, "true", "null", false},
	{"a rule a DO block creates", `CREATE TABLE u (id int);
DO $$ BEGIN CREATE RULE r AS ON INSERT TO u DO ALSO UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't'; END $$;
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	// TOP-1 of 2026-11-18: code the pass cannot read (dynamic EXECUTE) may
	// create a rule or trigger, so it ends SB-M9 for the set as a rule
	// does: the flags and policies of every table, held then or created
	// later, stay unknown.
	{"a rule a DO block creates through dynamic EXECUTE, in a set without code", `CREATE TABLE u (id int);
DO $$ BEGIN EXECUTE 'CREATE RULE r AS ON INSERT TO u DO ALSO UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = ''t'''; END $$;
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"a trigger a DO block creates through dynamic EXECUTE", `CREATE TABLE u (id int);
CREATE TABLE t (id int PRIMARY KEY);
DO $$ BEGIN
EXECUTE 'CREATE FUNCTION off() RETURNS trigger LANGUAGE plpgsql AS $f$ BEGIN UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = ''t''; DELETE FROM pg_catalog.pg_policy WHERE polname = ''p''; RETURN NEW; END $f$';
EXECUTE 'CREATE TRIGGER tr BEFORE INSERT ON u FOR EACH ROW EXECUTE FUNCTION off()';
END $$;
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"a rule a routine with dynamic EXECUTE creates when a later statement runs it", `CREATE TABLE u (id int);
CREATE FUNCTION mk() RETURNS void LANGUAGE plpgsql AS $$ BEGIN EXECUTE 'CREATE RULE r AS ON INSERT TO u DO ALSO UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = ''t'''; END $$;
SELECT mk();
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);
INSERT INTO u (id) VALUES (1);`, "null", "null", false},
	{"an event trigger a DO block creates through dynamic EXECUTE, then DDL only", `CREATE TABLE u (id int);
DO $$ BEGIN
EXECUTE 'CREATE FUNCTION off() RETURNS event_trigger LANGUAGE plpgsql AS $f$ BEGIN UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = ''t''; END $f$';
EXECUTE 'CREATE EVENT TRIGGER et ON ddl_command_end EXECUTE FUNCTION off()';
END $$;
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);`, "null", "null", false},
	{"an event trigger created at top level, then DDL only", `CREATE FUNCTION off() RETURNS event_trigger LANGUAGE plpgsql AS $f$ BEGIN UPDATE pg_catalog.pg_class SET relrowsecurity = false WHERE relname = 't'; END $f$;
CREATE EVENT TRIGGER et ON ddl_command_end EXECUTE FUNCTION off();
CREATE TABLE t (id int PRIMARY KEY);
ALTER TABLE t ENABLE ROW LEVEL SECURITY;
CREATE POLICY p ON t USING (true);`, "null", "null", false},
}

// policiesOf returns the number of policies the pass lists for table t,
// "null" when unknown.
func policiesOf(l linked) string {
	for _, n := range l.nodes {
		if n.Kind == "table" && n.Name == "t" {
			list, ok := n.Attributes["policies"].([]any)
			if !ok {
				return "null"
			}
			return fmt.Sprint(len(list))
		}
	}
	return "missing"
}

func TestSupabaseB5Cases(t *testing.T) {
	for _, tc := range supabaseB5Cases {
		t.Run(tc.name, func(t *testing.T) {
			l := supabaseLinked(t, map[string]string{"1_a.sql": tc.sql})
			got, ok := rlsEnabledOf(l)
			if !ok {
				t.Fatalf("no table t: %v", l.diagnostics)
			}
			if g := fmt.Sprint(got); g != map[string]string{"null": "<nil>", "true": "true"}[tc.enabled] {
				t.Errorf("rls enabled %s, want %s; %v", g, tc.enabled, l.diagnostics)
			}
			if g := policiesOf(l); g != tc.policies {
				t.Errorf("policies %s, want %s; %v", g, tc.policies, l.diagnostics)
			}
		})
	}
}

// TestSupabaseB5CasesPostgres runs the B5 cases on PostgreSQL 17 (as the
// test cluster's superuser) and compares every known value.
func TestSupabaseB5CasesPostgres(t *testing.T) {
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, tc := range supabaseB5Cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unit {
				t.Skip("the case names public")
			}
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
				t.Fatal(err)
			}
			defer conn.ExecContext(ctx, "ROLLBACK")
			if _, err := conn.ExecContext(ctx, "CREATE SCHEMA nxg_b5; SET LOCAL search_path TO nxg_b5"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, tc.sql); err != nil {
				t.Fatalf("Postgres: %v", err)
			}
			var enabled bool
			var policies int
			if err := conn.QueryRowContext(ctx, `SELECT relrowsecurity, (SELECT count(*) FROM pg_policy WHERE polrelid = c.oid) FROM pg_class c WHERE oid = 'nxg_b5.t'::regclass`).Scan(&enabled, &policies); err != nil {
				t.Fatal(err)
			}
			t.Logf("Postgres: relrowsecurity %v, %d policies", enabled, policies)
			if tc.enabled != "null" && tc.enabled != fmt.Sprint(enabled) {
				t.Errorf("the pass records rls enabled %s, Postgres %v", tc.enabled, enabled)
			}
			if tc.policies != "null" && tc.policies != fmt.Sprint(policies) {
				t.Errorf("the pass lists %s policies, Postgres %d", tc.policies, policies)
			}
		})
	}
}

// supabaseCatalogFixture is the B5 fixture: catalog writes after a
// trigger.
const supabaseCatalogFixture = "../../../testdata/fixtures/supabase-migrations/rls-catalog/supabase/migrations/20240101000000_notes.sql"

// TestSupabaseB5Fixture checks the rls-catalog fixture: notes' rls flags
// and policies are unknown and the migration may fail (the platform's
// postgres role is no superuser; TestSupabaseOracle's non-superuser replay
// is refused with 42501, so the fixture is not one of its roots).
func TestSupabaseB5Fixture(t *testing.T) {
	checkNotesRLSUnknown(t, supabaseCatalogFixture, "a catalog write")
}

// checkNotesRLSUnknown links the one-migration fixture at path and checks
// that its migration may fail (why) and that notes' rls flags and policies
// are unknown.
func checkNotesRLSUnknown(t *testing.T, path, why string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l := supabaseLinked(t, map[string]string{"20240101000000_notes.sql": string(data)})
	applied := appliedOf(l)
	if applied["20240101000000_notes.sql"] == true {
		t.Errorf("applied %v, want not proven (%s)", applied, why)
	}
	for _, n := range l.nodes {
		if n.Kind == "table" && n.Name == "notes" {
			rls, _ := n.Attributes["rls"].(map[string]any)
			if rls["enabled"] != nil || rls["forced"] != nil || n.Attributes["policies"] != nil {
				t.Errorf("notes: rls %v, policies %v, want unknown", rls, n.Attributes["policies"])
			}
			return
		}
	}
	t.Fatalf("no table notes: %v", l.diagnostics)
}

// TestSupabaseB5FixturePostgres replays the rls-catalog fixture on
// PostgreSQL 17 as a superuser (rolled back) and as a role without
// superuser rights, as the platform's postgres role: the superuser's
// writes disable row level security and drop the policy, which the pass
// does not claim otherwise, and the other role is refused.
func TestSupabaseB5FixturePostgres(t *testing.T) {
	replayNotesFixture(t, supabaseCatalogFixture)
}

// replayNotesFixture replays the one-migration fixture at path on
// PostgreSQL 17 as a role without superuser rights (refused with 42501)
// and as a superuser (rolled back), which leaves public.notes with row
// level security off and no policy.
func replayNotesFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := egress.OpenPostgres(oracleURL(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	var super bool
	if err := conn.QueryRowContext(ctx, "SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&super); err != nil {
		t.Fatal(err)
	}
	if !super {
		t.Skip("the test role is not a superuser")
	}
	if _, err := conn.ExecContext(ctx, "CREATE ROLE nxg_b5_platform NOSUPERUSER; GRANT CREATE ON SCHEMA public TO nxg_b5_platform; SAVEPOINT s; SET LOCAL ROLE nxg_b5_platform"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, string(data))
	if pe, ok := readPgError(err); !ok || pe.code != "42501" {
		t.Errorf("a role without superuser rights: %v, want 42501", err)
	}
	if _, err := conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT s; RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, string(data)); err != nil {
		t.Fatalf("superuser: %v", err)
	}
	var enabled bool
	var policies int
	if err := conn.QueryRowContext(ctx, `SELECT relrowsecurity, (SELECT count(*) FROM pg_policy WHERE polrelid = c.oid) FROM pg_class c WHERE oid = 'public.notes'::regclass`).Scan(&enabled, &policies); err != nil {
		t.Fatal(err)
	}
	if enabled || policies != 0 {
		t.Errorf("superuser: relrowsecurity %v, %d policies; the fixture expects false and 0", enabled, policies)
	}
}

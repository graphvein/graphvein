package schema

import "testing"

// supabaseRuleFixture is the stacked-gate NX-S fixture of row SB-M9: a
// rule whose actions write pg_catalog.pg_class and pg_catalog.pg_policy
// for a table the set creates after the rule, fired by an INSERT.
const supabaseRuleFixture = "../../../testdata/fixtures/supabase-migrations/rls-rule/supabase/migrations/20240101000000_notes.sql"

// TestSupabaseRuleFixture checks the rls-rule fixture: SB-M9 does not
// read rule actions, so notes' rls flags and policies are unknown.
func TestSupabaseRuleFixture(t *testing.T) {
	checkNotesRLSUnknown(t, supabaseRuleFixture, "the rule's catalog writes")
}

// TestSupabaseRuleFixturePostgres replays the rls-rule fixture on
// PostgreSQL 17: a role without superuser rights is refused, and as a
// superuser the rule's actions disable row level security on notes and
// drop its policy, which the pass does not claim otherwise.
func TestSupabaseRuleFixturePostgres(t *testing.T) {
	replayNotesFixture(t, supabaseRuleFixture)
}

// supabaseDynamicFixture is the TOP-1 of 2026-11-18 fixture of row SB-M9: a
// DO block's dynamic EXECUTE creates the rls-rule fixture's rule, which
// the pass cannot read.
const supabaseDynamicFixture = "../../../testdata/fixtures/supabase-migrations/rls-dynamic/supabase/migrations/20240101000000_notes.sql"

// TestSupabaseDynamicFixture checks the rls-dynamic fixture: code the pass
// cannot read ends SB-M9, so notes' rls flags and policies are unknown.
func TestSupabaseDynamicFixture(t *testing.T) {
	checkNotesRLSUnknown(t, supabaseDynamicFixture, "the dynamic rule's catalog writes")
}

// TestSupabaseDynamicFixturePostgres replays the rls-dynamic fixture on
// PostgreSQL 17: a role without superuser rights is refused, and as a
// superuser the dynamically created rule disables row level security on
// notes and drops its policy.
func TestSupabaseDynamicFixturePostgres(t *testing.T) {
	replayNotesFixture(t, supabaseDynamicFixture)
}

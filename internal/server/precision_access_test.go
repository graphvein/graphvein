package server

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/precision"
)

// TestAccessLabel checks the label of a code-to-table link by its target
// table's group tool (T024c2): a link into a sqlx table earns PL-F9, one
// into a Supabase table falls to row D (the SB rows have no evidence of
// their own yet), and one whose target the head cannot read is
// scope_unknown.
func TestAccessLabel(t *testing.T) {
	migration := func(set, tool string) contract.Node {
		return contract.Node{Identity: "migration:" + set + "/1.sql#migration", Kind: "migration", Attributes: map[string]any{"set": set, "tool": tool, "dialect": "postgres"}}
	}
	table := func(set, name string) contract.Node {
		return contract.Node{Identity: "table:" + set + "/1.sql#" + name, Kind: "table", Attributes: map[string]any{"set": set}}
	}
	tables := []contract.Node{table("migrations", "users"), table("supabase/migrations", "chats")}
	markerOf := schemaMarkers(tables, []contract.Node{migration("migrations", "sqlx"), migration("supabase/migrations", "supabase")})
	nodes := map[string]contract.Node{}
	for _, n := range tables {
		nodes[n.Identity] = n
	}
	run := runOfIndexer("graphvein", index.Version)
	cases := []struct {
		name    string
		targets []string
		label   precision.Label
		row     string
		reason  string
	}{
		{"sqlx table", []string{tables[0].Identity}, precision.Precise, "PL-F9", ""},
		{"supabase table", []string{tables[1].Identity}, precision.BestEffort, precision.DefaultRow, precision.ReasonDefault},
		{"a field with both", []string{tables[0].Identity, tables[1].Identity}, precision.BestEffort, precision.DefaultRow, precision.ReasonDefault},
		{"removed table", []string{"table:gone.sql#t"}, precision.BestEffort, precision.DefaultRow, reasonScopeUnknown},
	}
	for _, c := range cases {
		got := accessLabel(run, markerOf, nodes, c.targets)
		reason := ""
		if got.Reason != nil {
			reason = *got.Reason
		}
		if got.Label != c.label || got.Row != c.row || reason != c.reason {
			t.Errorf("%s: %+v (reason %q), want %s %s %q", c.name, got, reason, c.label, c.row, c.reason)
		}
	}
	if got := accessLabel(run, nil, nil, []string{tables[0].Identity}); got.Label != precision.BestEffort {
		t.Errorf("no graph to read: %+v, want best_effort", got)
	}
}

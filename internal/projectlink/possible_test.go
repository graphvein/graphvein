package projectlink

import (
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/store"
)

// TestPossibleCallers covers View.PossibleCallers (T022 review):
// which stored outcomes may reach a route without a link.
func TestPossibleCallers(t *testing.T) {
	ref := func(repo, id string) store.ProjectNodeRef {
		return store.ProjectNodeRef{Repository: repo, Identity: id}
	}
	route := ref("express", "route:b")
	outcomes := []store.ProjectOutcome{
		// Listed among the candidates.
		{Kind: KindCallsRoute, Source: ref("web", "c1"), Status: StatusAmbiguous, Row: RowSeveralProviders, Candidates: []store.ProjectNodeRef{ref("axum", "route:b"), route}, CandidateCount: 2},
		// The same call's second alternative: one entry per source.
		{Kind: KindCallsRoute, Source: ref("web", "c1"), Template: "/x", Status: StatusAmbiguous, Row: RowSeveralProviders, Candidates: []store.ProjectNodeRef{route}, CandidateCount: 2},
		// A list cut at ten whose last listed candidate sorts before the route.
		{Kind: KindCallsRoute, Source: ref("web", "c2"), Status: StatusAmbiguous, Row: RowSeveralProviders, Candidates: []store.ProjectNodeRef{ref("axum", "route:z")}, CandidateCount: 12},
		// A complete list without the route.
		{Kind: KindCallsRoute, Source: ref("web", "c3"), Status: StatusAmbiguous, Row: RowSeveralProviders, Candidates: []store.ProjectNodeRef{ref("axum", "route:b"), ref("express", "route:c")}, CandidateCount: 2},
		// A cut list whose last listed candidate sorts after the route.
		{Kind: KindCallsRoute, Source: ref("web", "c4"), Status: StatusAmbiguous, Row: RowSeveralProviders, Candidates: []store.ProjectNodeRef{ref("express", "route:c")}, CandidateCount: 11},
		// Incomplete with the route among the indexed candidates.
		{Kind: KindCallsRoute, Source: ref("web", "c5"), Status: StatusIncomplete, Row: RowProviderUnindexed, Reason: "no stored index: ghost", Candidates: []store.ProjectNodeRef{route}},
		// Incomplete where no indexed provider matched.
		{Kind: KindCallsRoute, Source: ref("web", "c6"), Status: StatusIncomplete, Row: RowProviderUnindexed, Reason: "no stored index: ghost; " + NoIndexedCandidate},
		// Incomplete from a pass before candidates were listed: the
		// manifest decides (web has no consumes: every other member).
		{Kind: KindCallsRoute, Source: ref("web", "c7"), Status: StatusIncomplete, Row: RowProviderUnindexed, Reason: "no stored index: ghost"},
		// Older incomplete outcome of a consumer naming only axum.
		{Kind: KindCallsRoute, Source: ref("admin", "c8"), Status: StatusIncomplete, Row: RowProviderUnindexed, Reason: "no stored index: ghost"},
		// Other statuses claim nothing; the route's own repository is never listed.
		{Kind: KindCallsRoute, Source: ref("web", "c9"), Status: StatusUnmatched, Row: RowNoCandidate},
		{Kind: KindCallsRoute, Source: ref("express", "c10"), Status: StatusAmbiguous, Row: RowScopeUnresolved, Candidates: []store.ProjectNodeRef{route}, CandidateCount: 2},
	}
	project := config.Project{Name: "p", Members: []config.Member{{Repository: "web"}, {Repository: "express"}, {Repository: "axum"}, {Repository: "admin", Consumes: []config.Consume{{Repository: "axum"}}}}}
	v := View{Stored: true, Set: store.ProjectLinkSet{Outcomes: outcomes}, project: project}
	var got []string
	for _, pc := range v.PossibleCallers(route.Repository, route.Identity) {
		got = append(got, pc.Source.Identity+" "+pc.Wording)
	}
	want := []string{"c1 " + PossibleAmbiguous, "c2 " + PossibleAmbiguous, "c5 " + PossibleIncomplete, "c7 " + PossibleIncomplete}
	if len(got) != len(want) {
		t.Fatalf("possible callers = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("possible callers = %q, want %q", got, want)
		}
	}
	// A changed manifest narrows nothing: the old consumer list is unknown.
	v.ManifestChanged = true
	if n := len(v.PossibleCallers(route.Repository, route.Identity)); n != 5 {
		t.Errorf("with a changed manifest: %d possible callers, want 5", n)
	}
	if n := len((View{}).PossibleCallers(route.Repository, route.Identity)); n != 0 {
		t.Errorf("without a stored pass: %d", n)
	}
}

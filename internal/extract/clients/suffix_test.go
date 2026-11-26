package clients

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestSuffixRequiresLiteralRemainder covers T017a: a suffix remainder must
// match at least one literal call segment with a literal route segment.
func TestSuffixRequiresLiteralRemainder(t *testing.T) {
	for _, tc := range []struct {
		name, routeMethod, template string
		optional                    bool
		method, call, base          string
		status, match               string
		confidence                  float64
	}{
		{"final catch-all only", "ANY", "/files/{*}", false, "DELETE", "/nothing", "unresolved", "unmatched", "", 0},
		{"parameter only", "GET", "/users/{id}", false, "GET", "/health", "unresolved", "unmatched", "", 0},
		{"literal and parameter", "GET", "/api/users/{id}", false, "GET", "/users/42", "unresolved", "linked", "suffix", 0.6},
		{"literal and catch-all", "ANY", "/cdn/assets/{*}", false, "GET", "/assets/logo.png", "unresolved", "linked", "suffix", 0.6},
		{"optional parameter only", "GET", "/api/{id}", true, "GET", "/health", "unresolved", "unmatched", "", 0},
		{"optional omitted literal", "GET", "/api/items/{id}", true, "GET", "/items", "unresolved", "linked", "suffix", 0.6},
		// T017b Rule P: the remainder {*}/files starts with a wildcard that
		// would take the call literal x.
		{"non-final wildcard then literal", "GET", "/api/{*}/files", false, "GET", "/x/files", "unresolved", "unmatched", "", 0},
		{"non-final wildcard then other literal", "GET", "/api/{*}/files", false, "GET", "/x/other", "unresolved", "unmatched", "", 0},
		{"no literal call segment", "GET", "/api/{id}", false, "GET", "/{id}", "unresolved", "unmatched", "", 0},
		// T017b Rule E: /{id} has no literal segment.
		{"exact without literal call segment", "GET", "/{id}", false, "GET", "/{id}", "unresolved", "unmatched", "", 0},
		{"resolved base never suffix", "GET", "/api/users/{id}", false, "GET", "/users/42", "none", "unmatched", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			routes := matcher{{identity: "route", method: tc.routeMethod, framework: "express", paths: []routePath{{Template: tc.template, Optional: tc.optional}}}}
			a := map[string]any{"method": tc.method, "path_template": tc.call, "base": tc.base, "origin": nil, "candidate_count": 0}
			edges := routes.link("call", "call.ts", contract.Span{}, a)
			if a["status"] != tc.status {
				t.Fatalf("status %v, want %s (%v)", a["status"], tc.status, a)
			}
			if tc.match == "" {
				if len(edges) != 0 || a["match"] != nil {
					t.Fatal("unexpected link", edges, a)
				}
				return
			}
			if len(edges) != 1 || edges[0].Target != "route" || edges[0].Confidence != tc.confidence || a["match"] != tc.match {
				t.Fatal(edges, a)
			}
		})
	}
}

// TestSuffixRemovalSecondaryEffects checks the effects the investigation lists: removed
// score-0 suffix candidates no longer tie with each other, and no longer beat
// a score-0 exact ANY candidate on method. Since T017b that ANY candidate is
// removed too (Rule E).
func TestSuffixRemovalSecondaryEffects(t *testing.T) {
	call := func() map[string]any {
		return map[string]any{"method": "GET", "path_template": "/health", "base": "unresolved", "origin": nil, "candidate_count": 0}
	}
	tied := matcher{
		{identity: "users", method: "GET", paths: []routePath{{Template: "/users/{id}"}}},
		{identity: "orders", method: "GET", paths: []routePath{{Template: "/orders/{id}"}}},
	}
	a := call()
	if edges := tied.link("call", "call.ts", contract.Span{}, a); len(edges) != 0 || a["status"] != "unmatched" || a["candidates"] != nil {
		t.Fatal("tied score-0 suffix candidates", edges, a)
	}
	root := matcher{
		{identity: "root", method: "ANY", paths: []routePath{{Template: "/{*}"}}},
		{identity: "users", method: "GET", paths: []routePath{{Template: "/users/{id}"}}},
	}
	// T017b Rule E: the literal-free ANY /{*} root is no candidate either.
	a = call()
	if edges := root.link("call", "call.ts", contract.Span{}, a); len(edges) != 0 || a["status"] != "unmatched" || a["candidates"] != nil {
		t.Fatal("literal-free ANY root candidate", edges, a)
	}
	withTest := matcher{
		{identity: "users", method: "GET", paths: []routePath{{Template: "/users/{id}"}}},
		{identity: "test", method: "GET", paths: []routePath{{Template: "/api/health", Scope: "test"}}},
	}
	a = call()
	if edges := withTest.link("call", "call.ts", contract.Span{}, a); len(edges) != 0 || a["status"] != "test_only" || len(a["candidates"].([]string)) != 1 || a["candidates"].([]string)[0] != "test" {
		t.Fatal("test-only literal suffix candidate", edges, a)
	}
}

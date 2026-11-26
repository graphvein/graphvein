package clients

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// literalCase is one call matched against one or more routes. An empty match
// means no edge; candidates lists the expected candidates for an ambiguous
// or test-only entry (nil when the entry has none).
type literalCase struct {
	name                   string
	routes                 matcher
	method, call, base     string
	originUnresolved, test bool
	status, match, target  string
	confidence             float64
	candidates             []string
}

func runLiteralCase(t *testing.T, tc literalCase) {
	t.Helper()
	a := map[string]any{"method": tc.method, "path_template": tc.call, "base": tc.base, "origin": nil, "candidate_count": 0}
	if tc.originUnresolved {
		a["origin_resolution"] = "unresolved"
	}
	if tc.test {
		a["scope"] = "test"
	}
	edges := tc.routes.link("call", "call.ts", contract.Span{}, a)
	if a["status"] != tc.status {
		t.Fatalf("status %v, want %s (%v)", a["status"], tc.status, a)
	}
	if tc.match == "" {
		if len(edges) != 0 || a["match"] != nil {
			t.Fatal("unexpected link", edges, a)
		}
		got, _ := a["candidates"].([]string)
		if len(got) != len(tc.candidates) {
			t.Fatalf("candidates %v, want %v", got, tc.candidates)
		}
		for i := range got {
			if got[i] != tc.candidates[i] {
				t.Fatalf("candidates %v, want %v", got, tc.candidates)
			}
		}
		return
	}
	if len(edges) != 1 || edges[0].Target != tc.target || edges[0].Confidence != tc.confidence || a["match"] != tc.match {
		t.Fatal(edges, a)
	}
}

func one(method, template string, optional bool) matcher {
	return matcher{{identity: "route", method: method, framework: "express", paths: []routePath{{Template: template, Optional: optional}}}}
}

// TestExactNeedsLiteralWithUnresolvedBase covers T017b Rule E: with an
// unresolved base, a literal-free route (/{id}, /{*}, /) matches only by
// segment count, so it is never a candidate. Known bases keep such matches.
func TestExactNeedsLiteralWithUnresolvedBase(t *testing.T) {
	twoFree := matcher{
		{identity: "a", method: "GET", framework: "express", paths: []routePath{{Template: "/{id}"}}},
		{identity: "b", method: "GET", framework: "express", paths: []routePath{{Template: "/{name}"}}},
	}
	testFree := matcher{{identity: "test", method: "GET", framework: "express", paths: []routePath{{Template: "/{id}", Scope: "test"}}}}
	for _, tc := range []literalCase{
		// Rule E: unmatched.
		{name: "parameter route", routes: one("GET", "/{id}", false), method: "GET", call: "/health", base: "unresolved", status: "unmatched"},
		{name: "ANY catch-all", routes: one("ANY", "/{*}", false), method: "POST", call: "/anything", base: "unresolved", status: "unmatched"},
		{name: "root", routes: one("GET", "/", false), method: "GET", call: "/", base: "unresolved", status: "unmatched"},
		{name: "optional parameter", routes: one("GET", "/{id}", true), method: "GET", call: "/x", base: "unresolved", status: "unmatched"},
		{name: "optional parameter omitted", routes: one("GET", "/{id}", true), method: "GET", call: "/", base: "unresolved", status: "unmatched"},
		{name: "two literal-free routes", routes: twoFree, method: "GET", call: "/health", base: "unresolved", status: "unmatched"},
		{name: "test-only literal-free route", routes: testFree, method: "GET", call: "/health", base: "unresolved", status: "unmatched"},
		{name: "unresolved origin too", routes: one("GET", "/{id}", false), method: "GET", call: "/health", base: "unresolved", originUnresolved: true, status: "unmatched"},
		// Unchanged.
		{name: "resolved base parameter route", routes: one("GET", "/{id}", false), method: "GET", call: "/health", base: "none", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "resolved base ANY catch-all", routes: one("ANY", "/{*}", false), method: "POST", call: "/anything", base: "none", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "resolved base root", routes: one("GET", "/", false), method: "GET", call: "/", base: "none", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "resolved base two literal-free routes", routes: twoFree, method: "GET", call: "/health", base: "none", status: "ambiguous", candidates: []string{"a", "b"}},
		{name: "resolved base test-only route", routes: testFree, method: "GET", call: "/health", base: "none", status: "test_only", candidates: []string{"test"}},
		// T034 row LF-1: an origin nothing proves to be the server never
		// reaches a literal-free route (was exact 0.7).
		{name: "assignment-derived origin", routes: one("GET", "/{id}", false), method: "GET", call: "/health", base: "none", originUnresolved: true, status: "unmatched"},
		{name: "assignment-derived origin, literal route", routes: one("GET", "/users/{id}", false), method: "GET", call: "/users/7", base: "none", originUnresolved: true, status: "linked", match: "exact", target: "route", confidence: 0.7},
		{name: "configured base", routes: one("GET", "/{id}", false), method: "GET", call: "/health", base: "configured", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "literal route from unresolved base", routes: one("GET", "/users/{id}", false), method: "GET", call: "/users/7", base: "unresolved", status: "linked", match: "exact", target: "route", confidence: 0.9},
	} {
		t.Run(tc.name, func(t *testing.T) { runLiteralCase(t, tc) })
	}
}

// TestSuffixRemainderAnchor covers T017b Rule P: a suffix remainder starts
// with a literal, or with a parameter matched by a call parameter, so no call
// literal becomes the value of a parameter placed after the unknown base.
func TestSuffixRemainderAnchor(t *testing.T) {
	// A parameter-first remainder (score 2) used to beat a literal-first
	// remainder (score 1); without it the literal-first route wins.
	lower := matcher{
		{identity: "users", method: "GET", framework: "express", paths: []routePath{{Template: "/users/{id}/t/edit"}}},
		{identity: "profile", method: "GET", framework: "express", paths: []routePath{{Template: "/api/profile/{a}/{b}"}}},
	}
	// A parameter-first and a literal-first remainder used to tie.
	tie := matcher{
		{identity: "users", method: "GET", framework: "express", paths: []routePath{{Template: "/users/{id}/edit"}}},
		{identity: "profile", method: "GET", framework: "express", paths: []routePath{{Template: "/api/profile/{x}"}}},
	}
	for _, tc := range []literalCase{
		{name: "parameter takes call literal", routes: one("GET", "/users/{id}/edit", false), method: "GET", call: "/profile/edit", base: "unresolved", status: "unmatched"},
		{name: "call parameter on route parameter", routes: one("GET", "/api/{tenant}/reports", false), method: "GET", call: "/{tenant}/reports", base: "unresolved", status: "linked", match: "suffix", target: "route", confidence: 0.6},
		{name: "literal then parameter", routes: one("GET", "/api/users/{id}", false), method: "GET", call: "/users/42", base: "unresolved", status: "linked", match: "suffix", target: "route", confidence: 0.6},
		{name: "literal then catch-all", routes: one("ANY", "/cdn/assets/{*}", false), method: "GET", call: "/assets/logo.png", base: "unresolved", status: "linked", match: "suffix", target: "route", confidence: 0.6},
		{name: "lower literal-first remainder wins", routes: lower, method: "GET", call: "/profile/t/edit", base: "unresolved", status: "linked", match: "suffix", target: "profile", confidence: 0.6},
		{name: "former tie becomes linked", routes: tie, method: "GET", call: "/profile/edit", base: "unresolved", status: "linked", match: "suffix", target: "profile", confidence: 0.6},
	} {
		t.Run(tc.name, func(t *testing.T) { runLiteralCase(t, tc) })
	}
}

// TestExactAnchorWithUnresolvedBase covers T017b Revision 1: with an
// unresolved base, an exact match needs Rule P's anchor too. A route that
// starts with a parameter is a candidate only when a call parameter fills
// that parameter; otherwise the call's leading literal would become the
// parameter's value although the unknown base could have supplied it.
func TestExactAnchorWithUnresolvedBase(t *testing.T) {
	for _, tc := range []literalCase{
		// Revision 1: unmatched.
		{name: "parameter takes leading literal", routes: one("GET", "/{org}/settings", false), method: "GET", call: "/account/settings", base: "unresolved", status: "unmatched"},
		{name: "two parameters take literals", routes: one("GET", "/{a}/{b}/edit", false), method: "GET", call: "/profile/x/edit", base: "unresolved", status: "unmatched"},
		{name: "unresolved origin too", routes: one("GET", "/{org}/settings", false), method: "GET", call: "/account/settings", base: "unresolved", originUnresolved: true, status: "unmatched"},
		// Unchanged.
		{name: "resolved base", routes: one("GET", "/{org}/settings", false), method: "GET", call: "/account/settings", base: "none", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "configured base", routes: one("GET", "/{a}/{b}/edit", false), method: "GET", call: "/profile/x/edit", base: "configured", status: "linked", match: "exact", target: "route", confidence: 0.9},
		{name: "call parameter on route parameter", routes: one("GET", "/{org}/settings", false), method: "GET", call: "/{org}/settings", base: "unresolved", status: "linked", match: "exact", target: "route", confidence: 0.9},
		// A rejected exact match still tries the anchored suffix remainders.
		{name: "falls back to anchored remainder", routes: one("GET", "/{v}/files/{*}", false), method: "GET", call: "/files/files/a", base: "unresolved", status: "linked", match: "suffix", target: "route", confidence: 0.6},
	} {
		t.Run(tc.name, func(t *testing.T) { runLiteralCase(t, tc) })
	}
}

// TestUnresolvedBaseKnownGaps pins two behaviours that ADR 0018's T017b
// amendment lists as known gaps, so a change to either is deliberate.
func TestUnresolvedBaseKnownGaps(t *testing.T) {
	for _, tc := range []literalCase{
		// G2: a dynamic first call segment is a call parameter, so it still
		// anchors a parameter-first remainder.
		{name: "dynamic first segment anchors parameter", routes: one("GET", "/users/{id}/edit", false), method: "GET", call: "/{x}/edit", base: "unresolved", status: "linked", match: "suffix", target: "route", confidence: 0.6},
		// G3: a versioned route loses its link; clients.base_paths makes the
		// base configured and restores it.
		{name: "versioned route unmatched", routes: one("GET", "/api/{version}/users", false), method: "GET", call: "/v1/users", base: "unresolved", status: "unmatched"},
		{name: "versioned route with configured base", routes: one("GET", "/api/{version}/users", false), method: "GET", call: "/api/v1/users", base: "configured", status: "linked", match: "exact", target: "route", confidence: 0.9},
	} {
		t.Run(tc.name, func(t *testing.T) { runLiteralCase(t, tc) })
	}
}

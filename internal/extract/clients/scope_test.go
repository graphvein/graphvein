package clients

import (
	"fmt"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestScopeWinningTier(t *testing.T) {
	for _, tc := range []struct {
		name            string
		routes          matcher
		callScope, want string
		count, links    int
	}{
		{"no fallback", matcher{
			{identity: "test", method: "GET", paths: []routePath{{Template: "/items", Scope: "test"}}},
			{identity: "prod", method: "ANY", paths: []routePath{{Template: "/items"}}},
		}, "", "test_only", 0, 0},
		{"test ignores scope", matcher{
			{identity: "test", method: "GET", paths: []routePath{{Template: "/items", Scope: "test"}}},
			{identity: "prod", method: "GET", paths: []routePath{{Template: "/items"}}},
		}, "test", "ambiguous", 2, 0},
		{"same target production proof", matcher{
			{identity: "route", method: "GET", paths: []routePath{{Template: "/items", Scope: "test"}, {Template: "/items"}}},
		}, "", "linked", 0, 1},
		{"slash selection precedes scope", matcher{
			{identity: "test", method: "GET", framework: "fastapi", paths: []routePath{{Template: "/items", Scope: "test"}}},
			{identity: "prod", method: "GET", framework: "fastapi", paths: []routePath{{Template: "/items", TrailingSlash: true}}},
		}, "", "test_only", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := map[string]any{"method": "GET", "path_template": "/items", "base": "none", "scope": tc.callScope, "candidate_count": 0}
			edges := tc.routes.link("call", "call.ts", contract.Span{}, a)
			if a["status"] != tc.want || a["candidate_count"] != tc.count || len(edges) != tc.links {
				t.Fatal(a, edges)
			}
		})
	}
	var routes matcher
	for i := 0; i < 12; i++ {
		routes = append(routes, route{identity: fmt.Sprintf("test%02d", i), method: "GET", paths: []routePath{{Template: "/items", Scope: "test"}}})
	}
	a := map[string]any{"method": "GET", "path_template": "/items", "base": "none"}
	if edges := routes.link("call", "call.ts", contract.Span{}, a); len(edges) != 0 || a["candidate_count"] != 12 || len(a["candidates"].([]string)) != 10 || a["status"] != "test_only" {
		t.Fatal(a, edges)
	}
}

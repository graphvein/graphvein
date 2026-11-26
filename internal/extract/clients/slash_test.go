package clients

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestSlashTwins checks the slash rules: equal-template ties of one
// framework whose router serves the two slash forms as two routes (FastAPI
// and the Go routers) keep the route of the call's form; Express, mixed
// frameworks and different templates are unchanged; a Go router's path is
// never linked from a call of its other slash form (TOP-2 of 2026-11-18),
// while a FastAPI sole candidate still is.
func TestSlashTwins(t *testing.T) {
	sole := func(framework, template string, slash bool) matcher {
		return matcher{{identity: "route", method: "GET", framework: framework, paths: []routePath{{Template: template, TrailingSlash: slash}}}}
	}
	twins := func(framework string) matcher {
		return matcher{
			{identity: "bare", method: "GET", framework: framework, paths: []routePath{{Template: "/api/articles"}}},
			{identity: "slash", method: "GET", framework: framework, paths: []routePath{{Template: "/api/articles", TrailingSlash: true}}},
		}
	}
	for _, tc := range []struct {
		name   string
		routes matcher
		slash  bool
		want   string
		target string
		path   string // the call's path template; "/api/articles" when empty
	}{
		{"fastapi bare call", twins("fastapi"), false, "linked", "bare", ""},
		{"fastapi slash call", twins("fastapi"), true, "linked", "slash", ""},
		{"gin bare call", twins("gin"), false, "linked", "bare", ""},
		{"gin slash call", twins("gin"), true, "linked", "slash", ""},
		{"chi bare call", twins("chi"), false, "linked", "bare", ""},
		{"net/http slash call", twins("net/http"), true, "linked", "slash", ""},
		{"express stays ambiguous", twins("express"), false, "ambiguous", "", ""},
		{"mixed frameworks stay ambiguous", matcher{
			{identity: "bare", method: "GET", framework: "fastapi", paths: []routePath{{Template: "/api/articles"}}},
			{identity: "slash", method: "GET", framework: "express", paths: []routePath{{Template: "/api/articles", TrailingSlash: true}}},
		}, false, "ambiguous", "", ""},
		{"mixed Go frameworks: the chi slash route never serves the bare form", matcher{
			{identity: "bare", method: "GET", framework: "gin", paths: []routePath{{Template: "/api/articles"}}},
			{identity: "slash", method: "GET", framework: "chi", paths: []routePath{{Template: "/api/articles", TrailingSlash: true}}},
		}, false, "linked", "bare", ""},
		{"a route answering both forms keeps the tie", matcher{
			{identity: "both", method: "GET", framework: "chi", paths: []routePath{{Template: "/api/articles"}, {Template: "/api/articles", TrailingSlash: true}}},
			{identity: "bare", method: "GET", framework: "chi", paths: []routePath{{Template: "/api/articles"}}},
		}, false, "ambiguous", "", ""},
		{"fastapi sole candidate of the other form still links", matcher{
			{identity: "slash", method: "GET", framework: "fastapi", paths: []routePath{{Template: "/api/articles", TrailingSlash: true}}},
		}, false, "linked", "slash", ""},
		// TOP-2 of 2026-11-18: the Go routers never hand the other slash form
		// to the route (gin redirects, chi answers 404, net/http answers
		// 404 or redirects a subtree root), so a sole candidate of the
		// other form is no link.
		{"gin bare call to a slash route", sole("gin", "/api/articles", true), false, "unmatched", "", ""},
		{"gin slash call to a bare route", sole("gin", "/api/articles", false), true, "unmatched", "", ""},
		{"chi bare call to a slash route", sole("chi", "/api/articles", true), false, "unmatched", "", ""},
		{"chi slash call to a bare route", sole("chi", "/api/articles", false), true, "unmatched", "", ""},
		{"net/http slash call to an exact route", sole("net/http", "/api/articles", false), true, "unmatched", "", ""},
		{"net/http bare call to a {$} route", sole("net/http", "/api/articles", true), false, "unmatched", "", ""},
		{"net/http bare call to a subtree root is a redirect", sole("net/http", "/api/{*}", false), false, "unmatched", "", "/api"},
		{"net/http subtree serves a slash call below it", sole("net/http", "/api/{*}", false), true, "linked", "route", "/api/articles"},
		{"chi catch-all serves either form below it", sole("chi", "/api/{*}", false), true, "linked", "route", ""},
		{"gin root has no other form", sole("gin", "/", false), false, "linked", "route", "/"},
		{"chi bare-prefix entry of a slash route links the bare call", matcher{
			{identity: "route", method: "GET", framework: "chi", paths: []routePath{{Template: "/api/articles", TrailingSlash: true}, {Template: "/api/articles"}}},
		}, false, "linked", "route", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = "/api/articles"
			}
			a := map[string]any{"method": "GET", "path_template": path, "trailing_slash": tc.slash, "base": "none", "candidate_count": 0}
			edges := tc.routes.link("call", "call.ts", contract.Span{}, a)
			if a["status"] != tc.want {
				t.Fatalf("status %v, want %s (%v)", a["status"], tc.want, a)
			}
			if tc.target != "" && (len(edges) != 1 || edges[0].Target != tc.target) {
				t.Fatalf("edges %v, want one to %s", edges, tc.target)
			}
		})
	}
}

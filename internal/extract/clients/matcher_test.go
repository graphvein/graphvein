package clients

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

func TestExactMatcherDoesNotUseSuffixOrNormalizeMethods(t *testing.T) {
	nodes := []contract.Node{
		{Kind: "route", Identity: "route:app.py#get", Attributes: map[string]any{"method": "GET", "framework": "fastapi", "paths": []map[string]any{{"template": "/prefix/items"}}}},
		{Kind: "route", Identity: "route:app.py#any", Attributes: map[string]any{"method": "ANY", "framework": "fastapi", "paths": []map[string]any{{"template": "/any"}}}},
	}
	m := NewRouteMatcher(nil, nil, nodes)
	for _, request := range []ExactRequest{{Method: "GET", Template: "/items"}, {Method: "get", Template: "/prefix/items"}, {Method: "HEAD", Template: "/prefix/items"}} {
		if result := m.Exact(request); result.Status != "unmatched" || result.Match != "" || len(result.Candidates) != 0 {
			t.Fatal(request, result)
		}
	}
	if result := m.Exact(ExactRequest{Method: "get", Template: "/any"}); result.Status != "unmatched" || result.Match != "" || len(result.Candidates) != 0 || result.CandidateCount != 0 {
		t.Fatal(result)
	}
}

func TestExactMatcherExtensionMethodsByServer(t *testing.T) {
	for _, method := range []string{"query", "Query", "QUERy", "QUERY"} {
		for _, framework := range []string{"express", "fastapi", "axum"} {
			t.Run(framework+"/"+method, func(t *testing.T) {
				nodes := []contract.Node{{Kind: "route", Identity: "route:any", Attributes: map[string]any{
					"method": "ANY", "framework": framework, "paths": []map[string]any{{"template": "/any"}},
				}}}
				result := NewRouteMatcher(nil, nil, nodes).Exact(ExactRequest{Method: method, Template: "/any"})
				if method == "QUERY" || framework == "axum" {
					if result.Status != "linked" || result.Match != "exact" || len(result.Candidates) != 1 || result.Candidates[0] != "route:any" || result.CandidateCount != 0 {
						t.Fatal(result)
					}
				} else if result.Status != "unmatched" || result.Match != "" || len(result.Candidates) != 0 || result.CandidateCount != 0 {
					t.Fatal(result)
				}
			})
		}
	}
}

// TestExactMatcherHeadServesGet checks the HEAD rule per framework: a HEAD
// call matches a GET route of Express, Axum and net/http, never one of
// FastAPI, chi or gin (T023 decision 8: chi answers 405, gin 404).
func TestExactMatcherHeadServesGet(t *testing.T) {
	for framework, matches := range map[string]bool{"express": true, "axum": true, "net/http": true, "fastapi": false, "chi": false, "gin": false} {
		t.Run(framework, func(t *testing.T) {
			nodes := []contract.Node{{Kind: "route", Identity: "route:get", Attributes: map[string]any{
				"method": "GET", "framework": framework, "paths": []map[string]any{{"template": "/items"}},
			}}}
			result := NewRouteMatcher(nil, nil, nodes).Exact(ExactRequest{Method: "HEAD", Template: "/items"})
			if matches != (result.Status == "linked" && len(result.Candidates) == 1 && result.Candidates[0] == "route:get") {
				t.Fatal(result)
			}
		})
	}
}

// TestMatcherHostPattern checks that a net/http host-pattern route links
// only calls whose literal origin names its host (T023 review).
func TestMatcherHostPattern(t *testing.T) {
	nodes := []contract.Node{{Kind: "route", Identity: "route:h", Attributes: map[string]any{
		"method": "GET", "framework": "net/http", "host": "example.com", "paths": []map[string]any{{"template": "/h"}},
	}}}
	m := newMatcher(nodes)
	for origin, want := range map[any]bool{"https://example.com": true, "https://EXAMPLE.com:8443": true, "https://other.org": false, nil: false} {
		attrs := map[string]any{"method": "GET", "path_template": "/h", "base": "none", "origin": origin}
		if got := len(m.link("call:x", "a.ts", contract.Span{}, attrs)) == 1; got != want {
			t.Errorf("origin %v: linked %v, want %v", origin, got, want)
		}
	}
	if r := NewRouteMatcher(nil, nil, nodes).Exact(ExactRequest{Method: "GET", Template: "/h"}); r.Status == "linked" {
		t.Errorf("exact request without a host linked %v", r)
	}
}

// TestLinkPrecisionRulesInMatchers checks rows LF-1 and LF-2 of
// docs/extract/clients.md in the matchers the project link pass uses
// (T034): the stored matcher replays the in-repository rules, and the
// project matcher keeps them except for a literal origin, which a
// consumes entry has declared for the provider (rows PL-R5, PL-L4).
func TestLinkPrecisionRulesInMatchers(t *testing.T) {
	route := func(id, method, template string) contract.Node {
		return contract.Node{Kind: "route", Identity: id, Attributes: map[string]any{
			"method": method, "framework": "express", "paths": []map[string]any{{"template": template}},
		}}
	}
	nodes := []contract.Node{
		route("route:all", "GET", "/{*path}"),
		route("route:movie", "GET", "/api/genres/movie"),
		route("route:tv", "GET", "/api/genres/tv"),
	}
	type result struct {
		status     string
		candidates int
	}
	for _, tc := range []struct {
		name            string
		request         CallRequest
		stored, project result
	}{
		{"LF-1 literal origin", CallRequest{Method: "GET", Template: "/repos/x", Base: "none", Origin: "https://api.github.com"}, result{"unmatched", 0}, result{"linked", 1}},
		{"LF-1 assignment-derived origin", CallRequest{Method: "GET", Template: "/repos/x", Base: "none", OriginUnresolved: true}, result{"unmatched", 0}, result{"unmatched", 0}},
		{"LF-U no origin", CallRequest{Method: "GET", Template: "/repos/x", Base: "none"}, result{"linked", 1}, result{"linked", 1}},
		{"LF-2 parameter over literals", CallRequest{Method: "GET", Template: "/api/genres/{type}", Base: "none"}, result{"ambiguous", 3}, result{"ambiguous", 3}},
		{"LF-U literal call", CallRequest{Method: "GET", Template: "/api/genres/movie", Base: "none"}, result{"linked", 1}, result{"linked", 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, m := range []struct {
				name    string
				matcher RouteMatcher
				want    result
			}{{"stored", NewStoredRouteMatcher(nodes), tc.stored}, {"project", NewProjectRouteMatcher(nodes), tc.project}} {
				got := m.matcher.Call(tc.request)
				count := got.CandidateCount
				if got.Status == "linked" {
					count = len(got.Candidates)
				}
				if got.Status != m.want.status || count != m.want.candidates {
					t.Errorf("%s matcher: %+v, want %+v", m.name, got, m.want)
				}
			}
		})
	}
}

// TestLinkPrecisionHostPatternProvesOrigin checks that row LF-1 leaves a
// net/http host-pattern route that names the call's host: the pattern
// proves the origin is the route's.
func TestLinkPrecisionHostPatternProvesOrigin(t *testing.T) {
	nodes := []contract.Node{{Kind: "route", Identity: "route:h", Attributes: map[string]any{
		"method": "ANY", "framework": "net/http", "host": "example.com", "paths": []map[string]any{{"template": "/{*}"}},
	}}}
	m := newMatcher(nodes)
	attrs := map[string]any{"method": "GET", "path_template": "/x", "base": "none", "origin": "https://example.com"}
	if len(m.link("call:x", "a.ts", contract.Span{}, attrs)) != 1 {
		t.Fatal("host-pattern catch-all not linked", attrs)
	}
}

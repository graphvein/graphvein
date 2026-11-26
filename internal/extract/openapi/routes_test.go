package openapi

import (
	"fmt"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/testscope"
)

func TestOperationAlternativesAndCandidateCaps(t *testing.T) {
	var routes []contract.Node
	for i := 0; i < 12; i++ {
		routes = append(routes, contract.Node{Kind: "route", Identity: fmt.Sprintf("route:app.py#tie%d", i), Attributes: map[string]any{
			"method": "GET", "framework": "fastapi", "paths": []map[string]any{{"template": "/tie", "scope": "test"}},
		}})
	}
	routes = append(routes, contract.Node{Kind: "route", Identity: "route:app.py#one", Attributes: map[string]any{
		"method": "GET", "framework": "fastapi", "paths": []map[string]any{{"template": "/one"}, {"template": "/other/one"}},
	}})
	matcher := clients.NewRouteMatcher(nil, nil, routes)
	sources := map[string][]byte{"openapi.yaml": []byte(`openapi: 3.2
paths:
  /tie: {get: {}}
  /one:
    get:
      servers: [{url: 'https://example.invalid/'}, {url: /other}]
  /absent:
    get:
      servers: [{url: /}, {url: relative}]
`)}
	nodes, edges, ds := Link(sources, testscope.Rules{}, testscope.Rules{}, LinkOptions{Matcher: matcher})
	if len(ds) != 1 || ds[0].Code != "openapi_coverage" {
		t.Fatal(ds)
	}
	count := 0
	for _, e := range edges {
		if e.Kind == "documents_route" {
			count++
			if e.Target != "route:app.py#one" || e.Confidence != 0.9 {
				t.Fatal(e)
			}
		}
	}
	if count != 1 {
		t.Fatal("alternatives must deduplicate at strongest confidence", count)
	}
	for _, n := range nodes {
		if n.Kind != "api_operation" {
			continue
		}
		paths := n.Attributes["paths"].([]map[string]any)
		switch n.Attributes["path"] {
		case "/tie":
			if n.Attributes["status"] != "test_only" || paths[0]["candidate_count"] != 12 || len(paths[0]["candidates"].([]string)) != 10 || paths[0]["match"] != nil {
				t.Fatal(n)
			}
		case "/one":
			if n.Attributes["status"] != "linked" || len(paths) != 2 {
				t.Fatal(n)
			}
		case "/absent":
			if n.Attributes["status"] != "unresolved" || paths[0]["status"] != "unmatched" || paths[1]["status"] != "unresolved" {
				t.Fatal(n)
			}
		}
	}
	tests, err := testscope.Compile([]string{"openapi.yaml"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, _, _ = Link(sources, testscope.Rules{}, tests, LinkOptions{Matcher: matcher})
	for _, n := range nodes {
		if n.Kind == "api_operation" && n.Attributes["path"] == "/tie" {
			p := n.Attributes["paths"].([]map[string]any)[0]
			if n.Attributes["status"] != "ambiguous" || p["candidate_count"] != 12 || len(p["candidates"].([]string)) != 10 {
				t.Fatal(n)
			}
		}
	}
}

func TestOperationConfiguredPrefixBoundaries(t *testing.T) {
	sources := map[string][]byte{}
	for _, file := range []string{"openapi.yaml", "a/openapi.yaml", "api/openapi.yaml", "apis/openapi.yaml", "api/specific.yaml"} {
		sources[file] = []byte("openapi: 3.2\nservers: [{url: relative}]\npaths: {/x/: {get: {}}}\n")
	}
	nodes, _, _ := Link(sources, testscope.Rules{}, testscope.Rules{}, LinkOptions{BasePaths: map[string]string{".": "", "a": "/a", "api": "/api", "api/specific.yaml": "/specific"}})
	for _, n := range nodes {
		if n.Kind != "api_operation" {
			continue
		}
		want := map[string]string{"openapi.yaml": "/x", "a/openapi.yaml": "/a/x", "api/openapi.yaml": "/api/x", "apis/openapi.yaml": "/x", "api/specific.yaml": "/specific/x"}[n.File]
		p := n.Attributes["paths"].([]map[string]any)[0]
		if p["template"] != want || p["resolution"] != "configured" || p["trailing_slash"] != true || len(p["origins"].([]string)) != 0 || n.Attributes["path_count"] != 1 {
			t.Fatal(n)
		}
	}
}

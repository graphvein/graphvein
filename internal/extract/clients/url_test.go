package clients

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

func TestConfiguredBaseLongestPrefix(t *testing.T) {
	r := resolver{files: map[string]facts.Facts{}}
	q := request{library: "fetch", url: expr{Kind: "literal", Name: "items", Text: "'items'"}, method: defaultMethod()}
	bases := map[string]string{".": "/root", "src": "/api", "src/admin": "/admin"}
	for i := 0; i < 100; i++ {
		a := r.attributes("a/call.ts", 0, q, "GET", true, map[string]string{".": "/root", "a": "/api"})
		if a["path_template"] != "/api/items" {
			t.Fatal("one-character directory lost to root", a)
		}
	}
	a := r.attributes("src/admin/call.ts", 0, q, "GET", true, bases)
	if a["base"] != "configured" || a["path_template"] != "/admin/items" {
		t.Fatal(a)
	}
	q.url = expr{Kind: "literal", Name: "/resolved", Text: "'/resolved'"}
	a = r.attributes("src/admin/call.ts", 0, q, "GET", true, bases)
	if a["base"] != "none" || a["path_template"] != "/resolved" {
		t.Fatal("overrode resolved URL", a)
	}
}

func TestCompactTemplateSubstitutions(t *testing.T) {
	s := " `literal space ${ /* trivia */ item . name + 'x y' }/${ `nested ${ a }` }` // comment\n"
	if got := compact(s); got != "`literal space ${item.name+'x y'}/${`nested ${a}`}`" {
		t.Fatal(got)
	}
}

func TestOptionalParameterAndWildcardBounds(t *testing.T) {
	m := matcher{{identity: "route:a.ts#one", method: "GET", paths: []routePath{{Template: "/api/{id}/middle", Optional: true}}}}
	a := map[string]any{"method": "GET", "path_template": "/api/x", "base": "none", "origin": nil}
	if links := m.link("client_call:a.ts#call", "a.ts", contract.Span{}, a); len(links) != 0 || a["status"] != "unmatched" {
		t.Fatal(a, links)
	}
	if _, ok := pathScore([]string{"api"}, []string{"api", "{*rest}"}); ok {
		t.Fatal("wildcard matched zero remaining segments")
	}
}

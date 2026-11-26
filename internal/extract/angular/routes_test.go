package angular

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

func TestJoinRoutePath(t *testing.T) {
	for _, c := range []struct{ prefix, path, want string }{
		{"", "", "/"},
		{"", "docs", "/docs"},
		{"/docs", "", "/docs"},
		{"/docs", ":id", "/docs/:id"},
		{"/", "a//b/", "/a/b"},
		{"/Admin", "**", "/Admin/**"},
	} {
		if got := joinRoutePath(c.prefix, c.path); got != c.want {
			t.Errorf("joinRoutePath(%q, %q) = %q, want %q", c.prefix, c.path, got, c.want)
		}
	}
}

func TestRouteLabel(t *testing.T) {
	pair := func(key, kind, text string) expression {
		e := expression{Kind: "object"}
		v := expression{Kind: kind}
		v.Value.Text = text
		e.Entries = append(e.Entries, declarationProperty("pair", key, v))
		return e
	}
	// wrap puts the path value in a transparent wrapper (review 1 F10).
	wrap := func(e expression, kind string) expression {
		e.Entries[0].Value = expression{Kind: kind, Parts: []expression{e.Entries[0].Value}}
		return e
	}
	for _, c := range []struct {
		object expression
		want   string
	}{
		{pair("path", "string", "''"), `""`},
		{pair("path", "string", "'tag/:tag'"), "tag/:tag"},
		{pair("path", "template_string", "`plain`"), "plain"},
		{pair("path", "template_string", "`a${b}`"), "(dynamic)"},
		{pair("path", "template_string", "`a\\nb`"), "(dynamic)"},
		{pair("path", "template_string", "`a\nb`"), "(dynamic)"},
		{pair("path", "identifier", "SEGMENT"), "(dynamic)"},
		{pair("matcher", "identifier", "match"), "(matcher)"},
		{pair("component", "identifier", "C"), "(no path)"},
		{wrap(pair("path", "string", "'paren'"), "parenthesized_expression"), "paren"},
		{wrap(pair("path", "string", "'cast'"), "as_expression"), "cast"},
		{wrap(pair("path", "string", "'checked'"), "satisfies_expression"), "checked"},
	} {
		obj := (&router{}).newRouteObject(routeContext{}, c.object, nil)
		if obj.segment != "ROUTE "+c.want {
			t.Errorf("%v: segment %q, want ROUTE %s", c.object.Entries[0].Value.Value.Text, obj.segment, c.want)
		}
	}
}

func TestPartialKeys(t *testing.T) {
	e := expression{Kind: "object"}
	e.Entries = append(e.Entries, declarationProperty("pair", "path", expression{Kind: "string"}))
	e.Entries = append(e.Entries, declarationProperty("spread_element", "", expression{Kind: "identifier"}))
	e.Entries = append(e.Entries, declarationProperty("pair", "component", expression{Kind: "identifier"}))
	obj := (&router{}).newRouteObject(routeContext{}, e, nil)
	if !obj.partial || !obj.open || obj.keys["path"].known || !obj.keys["component"].known {
		t.Fatal("a later spread must make earlier keys unknown", obj.partial, obj.keys)
	}
}

// TestNamedMethodKeys checks review 1 F4: a method whose key name the router
// payload records makes only that key present-but-unevaluated.
func TestNamedMethodKeys(t *testing.T) {
	e := expression{Kind: "object", Span: contract.Span{StartLine: 1, EndLine: 1, EndColumn: 9}}
	e.Entries = append(e.Entries, declarationProperty("pair", "path", expression{Kind: "string"}))
	e.Entries = append(e.Entries, declarationProperty("method_definition", "", expression{}))
	r := &router{methods: map[string]string{"f.ts\x00" + spanKey(e.Span) + "\x001": "loadComponent"}}
	obj := r.newRouteObject(routeContext{file: "f.ts"}, e, nil)
	k, ok := obj.keys["loadComponent"]
	if !obj.partial || obj.open || !obj.keys["path"].known || !ok || !k.method || k.known {
		t.Fatal("a named method defines only its own key", obj.partial, obj.open, obj.keys)
	}
	unnamed := (&router{}).newRouteObject(routeContext{file: "f.ts"}, e, nil)
	if !unnamed.open || unnamed.keys["path"].known {
		t.Fatal("an unnamed method may overwrite any key", unnamed.keys)
	}
}

func declarationProperty(kind, key string, value expression) typescript.DeclarationProperty {
	return typescript.DeclarationProperty{Kind: kind, Key: key, Value: value}
}

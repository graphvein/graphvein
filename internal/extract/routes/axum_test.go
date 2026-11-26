package routes

import (
	"strings"
	"testing"
)

func TestAxumPathConversion(t *testing.T) {
	for input, want := range map[string]string{
		"//api//:id/*rest/":  "/api/{id}/{*rest}",
		"/api/{id}/{*rest}/": "/api/{id}/{*rest}",
		"/":                  "/", "/parent/": "/parent", "/A/%2F/../name:literal": "/A/%2F/../name:literal",
	} {
		if got := axumNormalize(input); got != want {
			t.Errorf("%q: %q, want %q", input, got, want)
		}
	}
}

// TestStripRustComments: a layer as written keeps code only.
func TestStripRustComments(t *testing.T) {
	for in, want := range map[string]string{
		"ServiceBuilder::new() // The other reason\n.layer(TraceLayer::new())": "ServiceBuilder::new() .layer(TraceLayer::new())",
		"a /* x /* nested */ y */ b":                 "a b",
		`f("// not a comment", '/', r#"/* raw */"#)`: `f("// not a comment", '/', r#"/* raw */"#)`,
		"g::<'a>(x) // end":                          "g::<'a>(x)",
	} {
		if got := strings.Join(strings.Fields(stripRustComments(in)), " "); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

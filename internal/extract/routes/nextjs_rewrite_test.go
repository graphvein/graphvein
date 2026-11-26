package routes_test

import (
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/extract/routes"
)

// TestNextRewrite pins each outcome of routes.NextRewrite (row NX-L3);
// TestNextRewritesOracle checks the same reading against Next.js itself.
func TestNextRewrite(t *testing.T) {
	cases := []struct {
		source, destination, call string
		slash                     bool
		outcome, path             string
	}{
		{"/old/:path*", "/api/new/:path*", "/old/a/b", false, routes.RewriteApplied, "/api/new/a/b"},
		{"/old/:path*", "/api/new/:path*", "/old", false, routes.RewriteApplied, "/api/new"},
		{"/old/:path*", "/api/new/:path*", "/new/a", false, routes.RewriteNoMatch, ""},
		{"/old/:path*", "/api/new/:path*", "/{seg}/a", false, routes.RewriteUnsure, ""},
		{"/old/:path*", "/api/new/:path*", "/OLD/a", false, routes.RewriteUnsure, ""},
		{"/old/:path*", "/api/new/:path*", "/old/a", true, routes.RewriteUnsure, ""},
		{"/items/:id", "/things/:id", "/items/{id}", false, routes.RewriteApplied, "/things/{id}"},
		{"/ext/:p*", "https://ext.example.com/:p*", "/ext/x", false, routes.RewriteExternal, ""},
		{"/a", "//host/b", "/a", false, routes.RewriteUnmapped, ""},
		{"/odd/:id", "/v-:id", "/odd/1", false, routes.RewriteUnmapped, ""},
		{"/a/:x", "/b/:y", "/a/1", false, routes.RewriteUnmapped, ""},
		{"/a/:x+", "/b/:x", "/a/1/2", false, routes.RewriteUnmapped, ""},
		{"/a/:x?", "/b/:x?", "/a", false, routes.RewriteUnmapped, ""},
		{"/a/:x?", "/b/:x?", "/a/1", false, routes.RewriteApplied, "/b/1"},
		{"/a", "/b?q=1", "/a", false, routes.RewriteApplied, "/b"},
		{"/a/:x*/:y*", "/b/:y*", "/a/1/2", false, routes.RewriteUnsure, ""},
	}
	for _, c := range cases {
		call := strings.FieldsFunc(c.call, func(r rune) bool { return r == '/' })
		path, outcome := routes.NextRewrite(c.source, c.destination, call, c.slash)
		got := ""
		if outcome == routes.RewriteApplied {
			got = "/" + strings.Join(path, "/")
		}
		if outcome != c.outcome || got != c.path && !(c.path == "" && got == "") {
			t.Errorf("%s -> %s on %s: %s %q, want %s %q", c.source, c.destination, c.call, outcome, got, c.outcome, c.path)
		}
	}
}

package routes

import (
	"strings"
	"testing"
)

// TestReadExpressPath reads path pieces under each Express major (T028,
// docs/contract/claims/express5-paths.md). It replaces the former
// TestNormalize, whose single reading ignored the major: its cases keep
// their Express 4 results, except `{name}`, which is regular-expression
// syntax in Express 4 and a group in Express 5, never a parameter.
func TestReadExpressPath(t *testing.T) {
	const refusedPath = "-"
	for _, c := range []struct {
		piece    string
		major    int
		pattern  bool
		want     string // alternatives joined by " | ", refusedPath when refused
		optional bool
		refusal  string
	}{
		{"/api/users/:id?/", 4, true, "/api/users/{id}/", true, ""},
		{"/files/*", 4, true, "/files/{*}", false, ""},
		// T028 review: a piece without a leading slash matches nothing.
		{"//api///users/:id?/", 4, true, refusedPath, false, routePatternUnmodelled},
		{"files/*", 4, true, refusedPath, false, routePatternUnmodelled},
		{"users", 5, true, refusedPath, false, routePatternUnmodelled},
		{":id", 0, true, refusedPath, false, routePatternUnmodelled},
		{"api", 4, false, refusedPath, false, routePatternUnmodelled},
		{"", 4, true, "", false, ""},
		// Literals no request path carries, empty segments.
		{"/c/é", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/c/a b", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/c/a^b", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/c/\"q\"", 5, true, refusedPath, false, routePatternUnmodelled},
		{"//dbl", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/a//b", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/", 5, true, "/", false, ""},
		{"/a/", 5, true, "/a", false, ""},
		{"/api//", 4, false, refusedPath, false, routePatternUnmodelled},
		{"/api//", 5, false, "/api", false, ""},
		// Group alternatives Express 5 does not collapse.
		{"/g/{*rest}", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/h/{:id}", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/a/{/b}", 5, true, refusedPath, false, routePatternUnmodelled},
		{"{/:id}", 5, true, refusedPath, false, routePatternUnmodelled},
		// Optional only as the final token of a pattern.
		{"/a/:id?/x/:y", 4, true, "/a/{id}/x/{y}", false, ""},
		{"/m/:org?", 4, false, "/m/{org}", false, ""},
		{"/m/:org?", 4, true, "/m/{org}", true, ""},
		{"*", 4, true, "{*}", false, ""},
		{"/{name}/{*}", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/A/%2F/../B", 4, true, "/A/%2F/../B", false, ""},
		{"/*splat", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/api/*", 4, false, refusedPath, false, routePatternUnmodelled},
		{"/ab?cd", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/:file.:ext", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/:id(\\d+)", 4, true, refusedPath, false, routePatternUnmodelled},
		{"/:1st", 4, true, "/{1st}", false, ""},
		{"/*splat", 5, true, "/{*splat}", false, ""},
		{"*splat", 5, true, "{*splat}", false, ""},
		{"/r/*splat", 5, false, refusedPath, false, routePatternUnmodelled},
		{"/a/*rest/b", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/f*rest", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/users/:id", 5, true, "/users/{id}", false, ""},
		{"/users{/:id}", 5, true, "/users | /users/{id}", false, ""},
		{"/m{/:v}", 5, false, refusedPath, false, routePatternUnmodelled},
		{"/a{/b{/c}}", 5, true, "/a | /a/b | /a/b/c", false, ""},
		{"/files{/*path}", 5, true, "/files | /files/{*path}", false, ""},
		{"/:id?", 5, true, refusedPath, false, routePatternInvalid},
		{"/*", 5, true, refusedPath, false, routePatternInvalid},
		{"/a(b)", 5, true, refusedPath, false, routePatternInvalid},
		{"/a{/b", 5, true, refusedPath, false, routePatternInvalid},
		{"/:a{:b}", 5, true, refusedPath, false, routePatternInvalid},
		{"/a\\:b", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/:from-:to", 5, true, refusedPath, false, routePatternUnmodelled},
		{"/a|b$", 5, true, "/a|b$", false, ""},
		{"/users/:id", 0, true, "/users/{id}", false, ""},
		{"/users/:id?", 0, true, refusedPath, false, routePatternUnmodelled},
		{"/*splat", 0, true, refusedPath, false, routePatternUnmodelled},
		{"*", 0, true, refusedPath, false, routePatternUnmodelled},
		{"/users{/:id}", 0, true, refusedPath, false, routePatternUnmodelled},
	} {
		got := readExpressPath(c.piece, c.major, c.pattern)
		text := strings.Join(got.alternatives, " | ")
		if got.refusal != "" {
			text = refusedPath
		}
		if text != c.want || got.optional != c.optional || got.refusal != c.refusal {
			t.Errorf("readExpressPath(%q, %d, %t) = %q optional=%t refusal=%q (%s); want %q optional=%t refusal=%q", c.piece, c.major, c.pattern, text, got.optional, got.refusal, got.reason, c.want, c.optional, c.refusal)
		}
	}
	for _, c := range []struct{ in, want string }{
		{"//api///users/{id}/", "/api/users/{id}"},
		{"////", "/"},
		{"", "/"},
		{"/A/%2F/../B", "/A/%2F/../B"},
	} {
		if got := slashes(c.in); got != c.want {
			t.Errorf("slashes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestExpressMajorRanges: ranges every version of which has one major.
func TestExpressMajorRanges(t *testing.T) {
	for _, c := range []struct {
		ranges []string
		want   int
	}{
		{[]string{"4.21.1"}, 4},
		{[]string{"^4.18.2"}, 4},
		{[]string{"~5.1.0"}, 5},
		{[]string{"5.x"}, 5},
		{[]string{"5"}, 5},
		{[]string{"5.0.0-beta.3"}, 0},
		{[]string{">=4.0.0"}, 0},
		{[]string{"^4 || ^5"}, 0},
		{[]string{"*"}, 0},
		{[]string{"latest"}, 0},
		{[]string{"workspace:*"}, 0},
		{[]string{"3.21.2"}, 0},
		{[]string{"^4.21.1", "^5.1.0"}, 0},
		{[]string{"^5.1.0", "5.1.0"}, 5},
	} {
		if got := majorOfRanges(c.ranges); got != c.want {
			t.Errorf("majorOfRanges(%q) = %d, want %d", c.ranges, got, c.want)
		}
	}
}

// TestRouteDisjoint: the earlier-route path proof (DA-E1).
func TestRouteDisjoint(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"/items", "/feed", true},
		{"/:id", "/feed", false},
		{"/items", "/ITEMS", false},
		{"/a/multi", "/optional/:id?", true},
		{"/a/*", "/a/b/c", false},
		{"*", "/other", false},
		{"/api/*", "/r/other", true},
		{"/x", "/x/y", true},
		{"/x/:id?", "/x", false},
	} {
		if got := routeDisjoint(c.a, c.b); got != c.want {
			t.Errorf("routeDisjoint(%q, %q) = %t, want %t", c.a, c.b, got, c.want)
		}
	}
}

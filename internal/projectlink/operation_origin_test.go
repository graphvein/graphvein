package projectlink_test

import (
	"testing"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/projectlink"
)

// TestOperationOriginLiteralFreeRoute is row LF-1 for OpenAPI operations
// in the project pass (T034 review): a spec of a member without routes
// whose servers name a host nothing proves to be the provider's links
// another member's routes with a literal segment, never a literal-free
// route (a catch-all), unless a consumes entry names one of its origins.
func TestOperationOriginLiteralFreeRoute(t *testing.T) {
	catchAll := routeNode("route:server.ts#GET *", "GET", "/{*}", "")
	zen := routeNode("route:server.ts#GET /zen", "GET", "/zen", "")
	user := operationNode("o:user", "GET", "/user", "unmatched", "https://api.github.com")
	zenOp := operationNode("o:zen", "GET", "/zen", "unmatched", "https://api.github.com")
	relative := operationNode("o:relative", "GET", "/user", "unmatched")
	outcome := func(r projectlink.Result, source string) (projectlink.Outcome, bool) {
		for _, o := range r.Outcomes {
			if o.Source.Identity == source {
				return o, true
			}
		}
		return projectlink.Outcome{}, false
	}
	linked := func(r projectlink.Result, source string) string {
		for _, l := range r.Set.Links {
			if l.Source.Identity == source {
				return l.Target.Identity
			}
		}
		return ""
	}

	r := projectlink.Link(projectlink.Input{Project: "p", Members: []projectlink.Member{member("spec", user, zenOp, relative), member("back", catchAll, zen)}})
	if got := linked(r, "o:user"); got != "" {
		t.Errorf("an origin nothing proves links the catch-all %s", got)
	}
	if o, ok := outcome(r, "o:user"); !ok || o.Row != projectlink.RowNoCandidate {
		t.Errorf("o:user outcome %+v, want %s", o, projectlink.RowNoCandidate)
	}
	if got := linked(r, "o:zen"); got != zen.Identity {
		t.Errorf("a route with a literal segment: linked %q, want %s", got, zen.Identity)
	}
	if got := linked(r, "o:relative"); got != catchAll.Identity {
		t.Errorf("an operation without origins (LF-U): linked %q, want %s", got, catchAll.Identity)
	}

	// A consumes entry naming the origin is the proof.
	spec := member("spec", user)
	spec.Consumes = []config.Consume{{Repository: "back", Select: config.Selector{Origins: []string{"https://api.github.com"}}}}
	r = projectlink.Link(projectlink.Input{Project: "p", Members: []projectlink.Member{spec, member("back", catchAll, zen)}})
	if got := linked(r, "o:user"); got != catchAll.Identity {
		t.Errorf("declared origin: linked %q, want %s", got, catchAll.Identity)
	}
	// An entry that names no origin is no proof.
	spec.Consumes = []config.Consume{{Repository: "back"}}
	r = projectlink.Link(projectlink.Input{Project: "p", Members: []projectlink.Member{spec, member("back", catchAll, zen)}})
	if got := linked(r, "o:user"); got != "" {
		t.Errorf("an entry without origins links the catch-all %s", got)
	}
}

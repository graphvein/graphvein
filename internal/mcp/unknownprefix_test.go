package mcp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// upOrdersRoute is the unknown-prefix fixture's chi route below a
// configured base, linked by two calls of the unknown-prefix tier (UP-V1;
// UP-V1 and UP-V2).
const upOrdersRoute = "route:server/api.go#GET%20%2Fupb%2Forders%2F%7Bid%7D"

// TestUnknownPrefixSessions is the golden of the unknown-prefix answers
// (T033 row UP-36, decision U6) on both backends: a route served below a
// prefix the code does not fix keeps path null, shows its known part with
// "…" and the reason, and every link resting on it carries its UP
// assumptions (endpoint callers, callers, impact, chain, the list of
// endpoints without a stored caller); a Next.js route whose basePath is
// not read is shown the same way.
func TestUnknownPrefixSessions(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		session := func(fixture, repo string, calls []toolCall) transcript {
			root := copyFixtureTest(t, fixture, nil)
			indexRepository(t, root, w, repo)
			return runSession(t, Options{Root: root, Repository: repo, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, root, calls)
		}
		up := session("../../testdata/fixtures/unknown-prefix", "up", []toolCall{
			{"endpoints", args("scope", "all")},
			{"endpoint", args("target", upOrdersRoute)},
			{"callers", args("target", upOrdersRoute)},
			{"impact", args("target", "client_call:web/calls.ts#full.GET%20%60%2Fv1%2Fupb%2Forders%2F%24%7Bid%7D%60", "depth", 1)},
			{"chain", args("target", upOrdersRoute, "direction", "up", "depth", 1)},
			{"unresolved", args("status", "unmatched")},
			// MC-11: the path filter matches a route without a
			// served path by its known part: the proven path below the
			// unknown prefix, or the part above it.
			{"endpoints", args("scope", "all", "path_prefix", "/upb/orders")},
			{"endpoints", args("scope", "all", "path_prefix", "/outer")},
		})
		// Step UP-F: a served path resting on RO-V2 (MC-12
		// path_assumptions, flag path_assumed) and a route whose every
		// path an earlier mount takes (flag shadowed, not unmounted).
		order := session("../../testdata/fixtures/express-mounts", "order", []toolCall{
			{"endpoints", args("path_prefix", "/z")},
			{"endpoint", args("target", "route:web/app.js#GET%20%2Fz%2Fb")},
		})
		shadow := session("../../testdata/fixtures/fastapi-mounts", "shadow", []toolCall{
			{"endpoints", args("path_prefix", "/sh/")},
		})
		next := session("../../testdata/fixtures/nextjs-routes", "next", []toolCall{
			{"endpoint", args("target", "route:docs/app/api/search/route.js#GET%20%2Fapi%2Fsearch")},
			{"callers", args("target", "route:docs/app/api/search/route.js#GET%20%2Fapi%2Fsearch")},
		})
		golden.Check(t, filepath.Join("testdata", "session-unknown-prefix.golden.json"), map[string]any{"up": up.Calls, "next": next.Calls, "order": order.Calls, "shadow": shadow.Calls})
	})
}

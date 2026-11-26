package mcp

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestLinkPathAssumptionSessions is the golden of links whose matched
// route path entry rests on a path assumption, on
// both backends: an endpoint's caller, a callers and an impact step and a
// chain edge carry the ids as route_path_assumptions (after the
// unknown-prefix tier's prefix_assumptions), with each text in the
// envelope's assumptions; a plain link carries none.
func TestLinkPathAssumptionSessions(t *testing.T) {
	const (
		zb      = "route:web/app.js#GET%20%2Fz%2Fb"
		orders  = "route:server/main.go#GET%20%2Fgp%2Forders%2Flist"
		both    = "route:server/main.go#GET%20%2Fgb%2Fitems%2Fall"
		healthR = "route:api/main.py#GET%20%2Fhealth%2Flive"
	)
	backends(t, func(t *testing.T, w, s store.Store) {
		root := copyFixtureTest(t, "../../testdata/fixtures/link-path-assumptions", nil)
		indexRepository(t, root, w, "lpa")
		session := runSession(t, Options{Root: root, Repository: "lpa", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, root, []toolCall{
			{"endpoint", args("target", zb)},
			{"callers", args("target", healthR)},
			{"callers", args("target", orders)},
			{"callers", args("target", both)},
			{"impact", args("target", zb, "depth", 1)},
			{"chain", args("target", zb, "direction", "up", "depth", 1)},
		})
		golden.Check(t, filepath.Join("testdata", "session-path-assumptions.golden.json"), session.Calls)
	})
}

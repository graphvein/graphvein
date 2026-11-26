package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

// itemRoute is the link-precision-fl route a literal-origin call links
// under an origin assumption (LF-1, calls.ts:10).
const itemRoute = "route:backend.ts#GET%20%2Ft034-items%2F%3Aid"

// copyFixtureTest copies a fixture to a temporary directory and restores
// the inert names its README lists (from -> to, slash paths).
func copyFixtureTest(t *testing.T, fixture string, renames map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	for from, to := range renames {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestLinkHonestySessions is the golden of the T034 link statuses in the
// MCP answers on both backends: calls in committed bundler output
// (status generated, ADR 0085) are counted and listed by unresolved with
// their reason, never as a missing link; a caller linked under an origin
// assumption (LF-1) carries the assumption in its own fields and in the
// envelope's assumptions; LF-2 catch-all ambiguity is an unlinked call
// with its candidates; a project link keeps its stored assumption text,
// the origin assumption included.
func TestLinkHonestySessions(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		session := func(fixture string, renames map[string]string, repo string, calls []toolCall) transcript {
			root := copyFixtureTest(t, fixture, renames)
			indexRepository(t, root, w, repo)
			return runSession(t, Options{Root: root, Repository: repo, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, root, calls)
		}
		generated := session("../../testdata/fixtures/generated-bundle-output", nil, "bundle", []toolCall{
			{"unresolved", args()},
			{"unresolved", args("status", "generated")},
			{"endpoint", args("target", "GET /t035-gb/items")},
		})
		precision := session("../../testdata/fixtures/link-precision-fl", map[string]string{"fallback.ts.inert": "fallback.ts"}, "precision", []toolCall{
			{"endpoint", args("target", itemRoute)},
			{"callers", args("target", itemRoute)},
			{"impact", args("target", itemRoute, "depth", 2)},
			{"unresolved", args("status", "ambiguous,unmatched")},
		})
		catchall := session("../../testdata/fixtures/link-precision-catchall", nil, "catchall", []toolCall{
			{"unresolved", args("status", "ambiguous")},
		})

		proot := copyFixtureTest(t, "../../testdata/fixtures/projectlink", map[string]string{
			"frontend/openapi.yaml.fixture": "frontend/openapi.yaml", "monorepo/admin/openapi.yaml.fixture": "monorepo/admin/openapi.yaml"})
		data, err := os.ReadFile(filepath.Join(proot, "scenarios", "two.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		project, err := config.ParseProject(proot, data, nil)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(proot, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(proot, "backend"), w, "backend")
		if _, _, err := projectlink.Run(t.Context(), w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		projectRun := runSession(t, Options{Project: &project, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, proot, []toolCall{
			{"endpoint", args("target", "GET /t022/users", "repository", "backend")},
			{"callers", args("target", "GET /t022/users", "repository", "backend")},
		})
		golden.Check(t, filepath.Join("testdata", "session-links.golden.json"), map[string]any{
			"generated": generated, "precision": precision.Calls, "catchall": catchall.Calls, "project": projectRun.Calls})
	})
}

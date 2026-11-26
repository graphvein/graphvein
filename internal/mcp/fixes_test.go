package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// TestReviewFixSessions is the golden of the T030 review fixes on both
// backends: target resolution (R4, R5, R6, R11, R13), every served path
// (R8), table users of an uncovered tool (R3), the shared "may also be
// used by" rule with unbound sites (R9), the table tool's include and
// users page (R14) and the paged lists of unknowns (R14).
func TestReviewFixSessions(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		mounts := t.TempDir()
		if err := os.CopyFS(mounts, os.DirFS("../../testdata/fixtures/mcp-mounts")); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, mounts, w, "mounts")
		mountsRun := runSession(t, Options{Root: mounts, Repository: "mounts", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, mounts, []toolCall{
			{"endpoints", args()},
			{"endpoints", args("path_prefix", "/v2")},
			{"endpoints", args("path_prefix", "/v3")},
			{"endpoints", args("path_prefix", "/testing")},
			{"endpoints", args("path_prefix", "/testing", "scope", "test")},
			{"endpoint", args("target", "GET /testing/items")},
			{"endpoint", args("target", "GET /testing/items", "scope", "all")},
			{"endpoint", args("target", "GET /v2/items")},
			{"endpoint", args("target", "GET /v3/items")},
			{"callers", args("target", "GET /v2/items")},
			{"source", args("target", "GET /v1/items/{item_id}", "context", 0)},
			{"chain", args("target", "GET /v2/items", "direction", "down")},
		})

		api := apiFixtureRoot(t)
		indexRepository(t, api, w, "api")
		apiRun := runSession(t, Options{Root: api, Repository: "api", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, api, []toolCall{
			{"callers", args("target", "tItems")},
			{"impact", args("target", "Items")},
			{"source", args("target", "tItems")},
			{"callers", args("target", "listItems")},
			{"chain", args("target", "server")},
			{"chain", args("target", "component:server", "direction", "down", "limit", 1)},
			{"callers", args("target", "GET /t016d-items", "limit", 1)},
			{"impact", args("target", "api_operation:api/openapi.json#GET%20%2Ft016d-items")},
			{"chain", args("target", "api_operation:api/openapi.json#GET%20%2Ft016d-items")},
			{"table", args("target", "listItems")},
		})

		gorm := t.TempDir()
		if err := os.CopyFS(gorm, os.DirFS("../../testdata/fixtures/gorm-models/pg")); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, gorm, w, "gorm")
		gormRun := runSession(t, Options{Root: gorm, Repository: "gorm", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, gorm, []toolCall{
			{"table", args("target", "Category")},
			{"table", args("target", "struct:model/names.go#Category")},
			{"table", args("target", "Category", "include", "users")},
		})

		prisma := t.TempDir()
		if err := os.CopyFS(prisma, os.DirFS("../../testdata/fixtures/table-access-prisma")); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, prisma, w, "prisma")
		prismaRun := runSession(t, Options{Root: prisma, Repository: "prisma", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, prisma, []toolCall{
			{"table", args("target", "Tag", "include", "users")},
			{"table", args("target", "Tag", "include", "users", "limit", 1, "unknowns_limit", 1)},
			{"table", args("target", "Tag", "include", "users", "unknowns_limit", 1, "unknowns_cursor", "1")},
			{"table", args("target", "Tag", "include", "schema")},
			{"table", args("target", "Tag", "include", "nothing")},
		})
		golden.Check(t, filepath.Join("testdata", "session-fixes.golden.json"), map[string]any{
			"mounts": mountsRun, "api": apiRun.Calls, "gorm": gormRun.Calls, "prisma": prismaRun.Calls})
	})
}

// callEnvelope runs one tool call in an open session and decodes its
// envelope.
func callEnvelope(t *testing.T, c *session, name string, arguments map[string]any) map[string]any {
	t.Helper()
	res := c.call("tools/call", map[string]any{"name": name, "arguments": arguments})
	env, _ := res["structuredContent"].(map[string]any)
	if env == nil {
		t.Fatalf("%s: no structuredContent in %v", name, res)
	}
	return env
}

func freshnessOfEnv(env map[string]any) (state string, reused bool, finished string) {
	f, _ := env["freshness"].(map[string]any)
	idx, _ := f["index"].(map[string]any)
	reused, _ = f["reused"].(bool)
	return str(f["state"]), reused, str(idx["finished_at"])
}

// TestFreshnessWithinReuseWindow is R1 with a reuse window longer than
// the calls (the goldens' hour would hide it): status always checks; a
// file edit inside the window is never reported fresh next to a node
// whose file_state says changed; a re-index inside the window is never
// labelled with the old run's freshness. Both backends.
func TestFreshnessWithinReuseWindow(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRepository(t, root, w, "api")
		c := startSession(t, Options{Root: root, Repository: "api", Store: s, Version: "test", FreshnessReuse: time.Minute})
		c.initialize()
		state, reused, firstRun := freshnessOfEnv(callEnvelope(t, c, "status", args()))
		if state != "fresh" || reused {
			t.Fatalf("first status: %s reused %v", state, reused)
		}
		if _, reused, _ := freshnessOfEnv(callEnvelope(t, c, "status", args())); reused {
			t.Error("status reused a check: it must always check")
		}
		if _, reused, _ := freshnessOfEnv(callEnvelope(t, c, "find", args("term", "listItems"))); !reused {
			t.Error("find inside the window did not reuse the check (the window is what the test exercises)")
		}

		// Edit a served file inside the window.
		app := filepath.Join(root, "server", "app.ts")
		src, err := os.ReadFile(app)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(app, append(src, []byte("\n// edited\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			tool string
			a    map[string]any
		}{{"source", args("target", "listItems")}, {"find", args("term", "listItems")}, {"impact", args("target", "listItems")}} {
			env := callEnvelope(t, c, tc.tool, tc.a)
			raw, _ := json.Marshal(env["results"])
			if !strings.Contains(string(raw), `"file_state":"changed"`) {
				t.Errorf("%s after the edit: no node with file_state changed: %s", tc.tool, raw)
			}
			if state, _, _ := freshnessOfEnv(env); state == "fresh" {
				t.Errorf("%s after the edit: freshness fresh next to a changed node", tc.tool)
			}
		}

		// Re-index inside the window: the next answers name the new run.
		time.Sleep(1100 * time.Millisecond) // run finish times have second resolution
		indexRepository(t, root, w, "api")
		for _, tool := range []string{"find", "status"} {
			a := args("term", "listItems")
			if tool == "status" {
				a = args()
			}
			state, _, run := freshnessOfEnv(callEnvelope(t, c, tool, a))
			if state != "fresh" || run == firstRun {
				t.Errorf("%s after a re-index: state %s, run finished %s (first run %s)", tool, state, run, firstRun)
			}
		}
		c.close()
	})
}

// TestEnvelopeFieldOrder is R14: the error, bounds and page come before
// the results in the JSON, so a client that cuts a long text keeps them.
func TestEnvelopeFieldOrder(t *testing.T) {
	e := newEnvelope("probe")
	e.Error = &ToolError{Code: "x"}
	e.finish()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	results := strings.Index(text, `"results"`)
	for _, field := range []string{`"error"`, `"bounds"`, `"page"`, `"freshness"`, `"request"`} {
		if i := strings.Index(text, field); i < 0 || i > results {
			t.Errorf("%s is not before results in %s", field, text)
		}
	}
}

// TestListPage is R14's paging of the lists unknowns carry: exact totals,
// the rest by cursor.
func TestListPage(t *testing.T) {
	e := newEnvelope("probe")
	if err := e.unknownsPaging(unknownsArgs{UnknownsLimit: 2, UnknownsCursor: "1"}); err != nil {
		t.Fatal(err)
	}
	got := e.listPage([]any{1, 2, 3, 4})
	if items := got["items"].([]any); len(items) != 2 || items[0] != 2 || got["total"] != 4 || got["more"] != 1 || got["next_cursor"] != "3" {
		t.Errorf("listPage = %v", got)
	}
	whole := newEnvelope("probe").listPage([]any{1, 2, 3})
	if len(whole["items"].([]any)) != 3 || whole["more"] != nil {
		t.Errorf("listPage without paging = %v", whole)
	}
}

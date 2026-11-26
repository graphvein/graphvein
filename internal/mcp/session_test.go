package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/store"
)

const (
	changeFixture  = "../../testdata/fixtures/change-impact"
	heroTable      = "table:models/app/models.py#Hero"
	itemsTable     = "table:db/migrations/0001_items.sql#items"
	formatRow      = "function:server/items.ts#formatRow"
	projectFixture = "../../testdata/fixtures/projectimpact"
	usersManifest  = "version: 1\nname: users\nmembers:\n  - repository: frontend\n    path: frontend\n  - repository: backend\n    path: backend\n"
	usersRoute     = "route:src/server.ts#GET%20%2Ft022b7%2Fusers"
	usersHandler   = "function:src/server.ts#listUsers"
	tieRoute       = "route:server/app.ts#GET%20%2Ft016d-tie"
)

// toolCall is one tools/call of a golden session.
type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// exchange is one recorded tools/call: the call, isError and the envelope.
type exchange struct {
	Call     toolCall `json:"call"`
	IsError  bool     `json:"is_error"`
	Envelope any      `json:"envelope"`
}

// transcript is a golden session: the initialize result, the tool list
// and every call.
type transcript struct {
	Initialize any        `json:"initialize"`
	Tools      any        `json:"tools"`
	Calls      []exchange `json:"calls"`
}

// runSession runs calls in one session and returns the transcript. It
// checks that the text block is the same JSON as structuredContent (D5,
// EncodingBoth) and normalizes what a test cannot fix: run finish times
// and the temporary root.
func runSession(t *testing.T, opt Options, root string, calls []toolCall) transcript {
	t.Helper()
	c := startSession(t, opt)
	var out transcript
	out.Initialize = c.initialize()
	out.Tools = c.call("tools/list", map[string]any{})["tools"]
	for _, call := range calls {
		res := c.call("tools/call", map[string]any{"name": call.Name, "arguments": call.Arguments})
		if _, ok := res["error"]; ok {
			t.Fatalf("%s: protocol error %v", call.Name, res["error"])
		}
		content, _ := res["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("%s: %d content blocks", call.Name, len(content))
		}
		text, _ := content[0].(map[string]any)["text"].(string)
		var fromText any
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&fromText); err != nil {
			t.Fatalf("%s: text block is not JSON: %v", call.Name, err)
		}
		if !reflect.DeepEqual(fromText, res["structuredContent"]) {
			t.Fatalf("%s: text block and structuredContent differ", call.Name)
		}
		isError, _ := res["isError"].(bool)
		out.Calls = append(out.Calls, exchange{Call: call, IsError: isError, Envelope: res["structuredContent"]})
	}
	c.close()
	return normalize(t, out, root)
}

// runIDs and commits are the run identities and git commits a test
// cannot fix; normalize numbers them in order of appearance.
var (
	runIDs  = regexp.MustCompile(`\b[A-Z2-7]{26}\b`)
	commits = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
)

// normalize replaces the temporary root, every finish time, run identity,
// git commit and change-view cursor.
func normalize(t *testing.T, v transcript, root string) transcript {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte(filepath.ToSlash(root)), []byte("{ROOT}"))
	for _, re := range []struct {
		re   *regexp.Regexp
		name string
	}{{runIDs, "run"}, {commits, "commit"}} {
		seen := map[string]string{}
		raw = re.re.ReplaceAllFunc(raw, func(m []byte) []byte {
			if _, ok := seen[string(m)]; !ok {
				seen[string(m)] = fmt.Sprintf("<%s-%d>", re.name, len(seen)+1)
			}
			return []byte(seen[string(m)])
		})
	}
	var out transcript
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "finished_at" || k == "created_at" {
					x[k] = "<" + k + ">"
					continue
				}
				if cur, ok := val.(string); ok && k == "next_cursor" && strings.Trim(cur, "0123456789") != "" {
					// A change view's cursor encodes its comparison.
					x[k] = "<cursor>"
					continue
				}
				walk(val)
			}
		case []any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	for _, c := range out.Calls {
		walk(c.Envelope)
	}
	return out
}

func args(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// TestSessionGoldens is the MCP-1 gate on both backends: one session over
// the api-section fixture (every MCP-1 tool, paging, refusals, near
// misses, ends and caps) and one over the projectimpact project (project
// links with their assumption, member freshness and the link pass). The
// transcripts are golden, and together with the MC-D unit case they reach
// every row of the claim contract.
func TestSessionGoldens(t *testing.T) {
	var mu sync.Mutex
	tags := map[string]bool{}
	rowTag = func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	}
	defer func() { rowTag = nil }()
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRepository(t, root, w, "api")
		single := runSession(t, Options{Root: root, Repository: "api", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, root, []toolCall{
			{"status", args()},
			{"find", args("term", "ambiguous")},
			{"find", args("term", "GET '/t016d-items'", "limit", 1)},
			{"find", args("term", "noSuchName")},
			{"endpoints", args("limit", 3)},
			{"endpoints", args("limit", 3, "cursor", "3")},
			{"endpoints", args("method", "POST")},
			{"endpoint", args("target", "GET /t016d-tie")},
			{"endpoint", args("target", "GET /t016d-items")},
			{"endpoint", args("target", tieRoute)},
			{"source", args("target", "ambiguous", "context", 2)},
			{"callers", args("target", tieRoute)},
			{"callers", args("target", "GET /t016d-items")},
			{"callers", args("target", "ambiguous")},
			{"callers", args("target", "ambiguous", "depth", 11)},
			{"callers", args("target", "noSuchName")},
			{"impact", args("target", "listItems", "depth", 1)},
			{"impact", args("target", "listItems")},
			{"callers", args("target", "GET /t016d-items", "scope", "all")},
			{"impact", args("target", "listItems", "scope", "all", "limit", 3)},
			{"callers", args("target", "ambiguous", "scope", "test")},
			{"link_changes", args()},
		})
		golden.Check(t, filepath.Join("testdata", "session-api.golden.json"), single)

		// The change-impact fixture: schema (a sqlx migration set and a
		// SQLModel model), code-to-table access (fact, inferred and
		// mapped, a built query, a may-also-use site), unlinked calls and
		// two runs with a pin, so the second step's tools answer from a
		// change set; the second revision is committed to git so a ref
		// base without a stored snapshot is refused (D9).
		croot := t.TempDir()
		copyTreeTest(t, changeFixture+"/rev1", croot)
		indexRepository(t, croot, w, "fixture")
		runs, err := w.ChangeRuns(t.Context(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Pin(t.Context(), "fixture", runs[len(runs)-1].Seq, "rev1", 0); err != nil {
			t.Fatal(err)
		}
		copyTreeTest(t, changeFixture+"/rev2", croot)
		gitIn(t, croot, "init", "-q")
		gitIn(t, croot, "add", "-A")
		gitIn(t, croot, "commit", "-q", "-m", "rev2")
		gitIn(t, croot, "tag", "rev2")
		indexRepository(t, croot, w, "fixture")
		schema := runSession(t, Options{Root: croot, Repository: "fixture", Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, croot, schemaCalls)
		golden.Check(t, filepath.Join("testdata", "session-schema.golden.json"), schema)

		proot := t.TempDir()
		if err := os.CopyFS(proot, os.DirFS(projectFixture)); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(proot, "frontend", "openapi.yaml.fixture"), filepath.Join(proot, "frontend", "openapi.yaml")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proot, config.ProjectFile), []byte(usersManifest), 0o644); err != nil {
			t.Fatal(err)
		}
		project, err := config.LoadProject(proot)
		if err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(proot, "frontend"), w, "frontend")
		indexRepository(t, filepath.Join(proot, "backend"), w, "backend")
		if _, _, err := projectlink.Run(t.Context(), w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		calls := []toolCall{
			{"status", args()},
			{"find", args("term", "listUsers", "repository", "backend")},
			{"endpoints", args("repository", "backend")},
			{"endpoint", args("target", usersRoute, "repository", "backend")},
			{"callers", args("target", usersRoute, "repository", "backend")},
			{"callers", args("target", "listUsers")},
			{"impact", args("target", usersHandler, "repository", "backend", "depth", 4)},
			{"source", args("target", usersHandler)},
			{"chain", args("target", usersRoute, "repository", "backend")},
			{"chain", args("target", usersRoute)},
			{"unresolved", args("repository", "frontend")},
			{"unresolved", args("repository", "backend")},
			{"overview", args("repository", "backend")},
			{"changes", args("repository", "frontend")},
			{"table", args("target", "table:src/db/schema.ts#users", "repository", "backend")},
		}
		projectRun := runSession(t, Options{Project: &project, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, proot, calls)
		// The frontend indexed again without a link pass: links stale. The
		// backend's handler body changes, so its change impact crosses
		// the stale link into the frontend.
		indexRepository(t, filepath.Join(proot, "frontend"), w, "frontend")
		server := filepath.Join(proot, "backend", "src", "server.ts")
		src, err := os.ReadFile(server)
		if err != nil {
			t.Fatal(err)
		}
		src = bytes.Replace(src, []byte("  res.json(await db.select().from(users));"), []byte("  const rows = await db.select().from(users);\n  res.json(rows);"), 1)
		if err := os.WriteFile(server, src, 0o644); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(proot, "backend"), w, "backend")
		stale := runSession(t, Options{Project: &project, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, proot, []toolCall{
			{"callers", args("target", usersRoute, "repository", "backend")},
			{"changes", args("repository", "frontend")},
			{"changes", args("repository", "backend")},
			{"change_impact", args("id", usersHandler, "repository", "backend", "depth", 4)},
			{"change_impact", args("group", "client_calls", "repository", "frontend")},
			{"change_impact", args("id", usersRoute, "repository", "backend", "base", "ref:main")},
		})
		golden.Check(t, filepath.Join("testdata", "session-project.golden.json"), map[string]any{"linked": projectRun, "stale": stale.Calls})

		// A second link pass over the re-indexed members, so the link
		// journal keeps two passes to compare (MC-32). Before it, the
		// journal kept one: the linked session's answer is no_comparison.
		before := runSession(t, Options{Project: &project, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, proot, []toolCall{
			{"link_changes", args()},
		})
		// The frontend's calls change before it: one call moves down a
		// line (its link's evidence changes), the POST becomes a PUT no
		// route serves (its link is removed) and a new call is linked.
		api := filepath.Join(proot, "frontend", "src", "api.ts")
		srcAPI := "export function listUsers() {\n  // moved down one line\n  return fetch(`${import.meta.env.VITE_API}/t022b7/users`);\n}\n" +
			"export function createUser() {\n  return fetch(`${import.meta.env.VITE_API}/t022b7/users`, { method: 'PUT' });\n}\n" +
			"export function reloadUsers() {\n  return fetch(`${import.meta.env.VITE_API}/t022b7/users`);\n}\n"
		if err := os.WriteFile(api, []byte(srcAPI), 0o644); err != nil {
			t.Fatal(err)
		}
		indexRepository(t, filepath.Join(proot, "frontend"), w, "frontend")
		if _, _, err := projectlink.Run(t.Context(), w, project, projectlink.ManifestHash(project)); err != nil {
			t.Fatal(err)
		}
		after := runSession(t, Options{Project: &project, Store: s, Version: "test", Now: fixedNow, FreshnessReuse: time.Hour}, proot, []toolCall{
			{"link_changes", args()},
			{"link_changes", args("base", "pass:1", "limit", 1)},
			{"link_changes", args("base", "pass:1", "limit", 1, "cursor", "1")},
			{"link_changes", args("base", "pass:2")},
			{"link_changes", args("base", "pass:99")},
			{"link_changes", args("base", "yesterday")},
		})
		golden.Check(t, filepath.Join("testdata", "session-link-changes.golden.json"), map[string]any{"one_pass": before.Calls, "two_passes": after.Calls})
	})
	// Row MC-D: a field and a label value the contract does not name are
	// passed on as unknowns, never dropped.
	e := newEnvelope("probe")
	node := compactNode(e, map[string]any{"identity": "function:a.go#f", "kind": "function", "novel_field": 1})
	e.label("provenance", "guessed")
	if _, ok := node["novel_field"]; ok || len(e.Unknowns) != 2 || e.Unknowns[0].Row != "MC-D" || e.Unknowns[1].Row != "MC-D" || e.Unknowns[0].Data != 1 {
		t.Errorf("MC-D: node %v unknowns %+v", node, e.Unknowns)
	}
	var missed []string
	for _, r := range claimRows {
		if !tags[r.ID] {
			missed = append(missed, r.ID)
		}
	}
	for tag := range tags {
		found := false
		for _, r := range claimRows {
			found = found || r.ID == tag
		}
		if !found {
			missed = append(missed, "unknown tag "+tag)
		}
	}
	sort.Strings(missed)
	if len(missed) > 0 {
		t.Errorf("claim rows not reached by the sessions: %v", missed)
	}
}

// schemaCalls is the change-impact session: every tool of the second step
// with its refusals, compact and full detail, and paging.
var schemaCalls = []toolCall{
	{"status", args()},
	{"overview", args()},
	{"chain", args("target", "Hero")},
	{"chain", args("target", heroTable, "direction", "up")},
	{"chain", args("target", formatRow, "direction", "up", "depth", 1, "limit", 2)},
	{"chain", args("target", "db")},
	{"chain", args("target", formatRow, "depth", 11)},
	{"table", args("target", heroTable)},
	{"table", args("target", itemsTable, "detail", "full")},
	{"changes", args("limit", 3)},
	{"changes", args("group", "model_tables", "detail", "full")},
	{"changes", args("base", "pin:rev1", "group", "migration_tables")},
	{"changes", args("base", "pin:nope")},
	{"changes", args("base", "ref:rev2..rev2")},
	{"change_impact", args("id", formatRow, "depth", 1)},
	{"change_impact", args("id", "column:models/app/models.py#Hero.age")},
	{"change_impact", args("id", formatRow, "depth", 11)},
	{"change_impact", args("id", "function:server/none.ts#nothing")},
	{"unresolved", args()},
	{"unresolved", args("transport", "tauri_ipc")},
	{"endpoints", args()},
	{"endpoint", args("target", "GET /api/items")},
	{"endpoint", args("target", "GET /api/items", "detail", "full")},
	{"callers", args("target", "formatRow")},
	{"chain", args("target", "component:db")},
}

// gitIn runs git in dir with a fixed identity, as the change fixture's own
// golden test does (no configuration is changed).
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.autocrlf=false", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestInstructionsShort keeps the server instructions within the budget a
// client pays in every session (the session goldens fix the text itself).
func TestInstructionsShort(t *testing.T) {
	if n := len(instructions); n > 600 {
		t.Errorf("server instructions are %d bytes, want at most 600", n)
	}
}

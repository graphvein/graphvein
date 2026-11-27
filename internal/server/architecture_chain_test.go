package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

// chainSynth holds the identities of the crafted snapshot "chainsynth".
type chainSynth struct {
	route, table, handler, inner, command, file, hub, page, dependency string
}

// chainSynthStream is "chainsynth", a small stack crafted so that the
// chain reaches every CH row: UI function Page renders Widget (CH8) and
// calls useItems, which calls ItemsService.readItems (CH1), whose client
// call (CH2) calls GET /items (CH3, inferred); read_items handles it (CH4),
// reads table item (CH6, inferred, through a query builder) and contains
// the nested _inner (CH7), which writes item and has no caller, and the
// nested _fmt, which reads item and read_items calls; a test
// calls read_items and read_items calls a test helper (CH10); useItems
// makes a Tauri IPC call to command greet (CH5); read_items calls an
// external and has a uses_config edge (CHD); hub has 201 callers (CH12
// fan-out). Page has no caller (CH11).
func chainSynthStream(t *testing.T) (*streamWriter, int, chainSynth) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "t026-chain"}, "repository": "chainsynth", "generated_at": "2026-11-18T00:00:00Z"})
	for _, f := range []string{"ui/package.json", "ui/src/app.ts", "api/pyproject.toml", "api/items.py", "api/models.py", "api/hub.py", "api/tests/test_items.py", "api/deps.py", "app/Cargo.toml", "app/src/main.rs"} {
		w.rec(map[string]any{"type": "file", "path": f, "language": "unknown", "precision": "best_effort", "size": 0, "content_hash": contract.ContentHash(nil)})
	}
	pkg := func(file, name, ecosystem string) {
		id := w.id("package", file, name)
		w.rec(map[string]any{"type": "node", "identity": id, "kind": "package", "level": 2, "name": name, "qualified_name": name,
			"file": file, "span": w.span(), "content_hash": contract.ContentHash(nil), "attributes": map[string]any{"name": name, "directory": filepath.ToSlash(filepath.Dir(file)), "ecosystem": ecosystem, "dependencies": []any{}}})
	}
	pkg("ui/package.json", "ui", "npm")
	pkg("api/pyproject.toml", "api", "python")
	pkg("app/Cargo.toml", "app", "cargo")
	var s chainSynth
	type e5 struct {
		kind, source, target, provenance string
		confidence                       float64
	}
	var edges []e5
	edge := func(kind, source, target, provenance string, confidence float64) {
		edges = append(edges, e5{kind, source, target, provenance, confidence})
	}
	s.file = w.node("file", "ui/src/app.ts", "ui/src/app.ts", nil)
	s.page = w.node("function", "ui/src/app.ts", "Page", nil)
	widget := w.node("function", "ui/src/app.ts", "Widget", nil)
	use := w.node("function", "ui/src/app.ts", "useItems", nil)
	read := w.node("method", "ui/src/app.ts", "ItemsService.readItems", nil)
	call := w.node("client_call", "ui/src/app.ts", "ItemsService.readItems.GET /items", map[string]any{"method": "GET", "status": "linked", "paths": []any{}})
	ipc := w.node("ipc_call", "ui/src/app.ts", "useItems.INVOKE 'greet'", map[string]any{"command": "greet", "status": "linked"})
	// GET /items declares OAuth2PasswordBearer through get_current_user
	// (AA3), which has no stored caller: CH11 names the injection.
	s.dependency = w.id("function", "api/deps.py", "get_current_user")
	s.route = w.node("route", "api/items.py", "GET /items", map[string]any{"framework": "fastapi", "method": "GET", "pattern": "/items",
		"security_dependencies": []any{map[string]any{"class": "OAuth2PasswordBearer", "scheme_name": "OAuth2PasswordBearer", "auto_error": true, "instance": "api/deps.py#oauth2",
			"chain": []any{
				map[string]any{"via": "parameter user", "file": "api/items.py", "span": nil, "target": s.dependency},
				map[string]any{"via": "parameter token", "file": "api/deps.py", "span": nil, "target": "api/deps.py#oauth2"},
			}}}})
	s.handler = w.node("function", "api/items.py", "read_items", map[string]any{"table_access": []any{map[string]any{"kind": "reads_table", "target": w.id("table", "api/models.py", "item"), "role": "builds"}}})
	s.inner = w.node("function", "api/items.py", "read_items._inner", nil)
	s.table = w.node("table", "api/models.py", "item", map[string]any{"set": "models", "tool": "sqlmodel"})
	setting := w.node("variable", "api/items.py", "SETTING", nil)
	testFn := w.node("function", "api/tests/test_items.py", "test_read", nil)
	helper := w.node("function", "api/tests/test_items.py", "make_item", nil)
	s.command = w.node("ipc_command", "app/src/main.rs", "main.COMMAND greet", map[string]any{"framework": "tauri", "command": "greet"})
	greet := w.node("function", "app/src/main.rs", "greet", nil)
	s.hub = w.node("function", "api/hub.py", "hub", nil)
	edge("renders", s.page, widget, "fact", 1)
	edge("calls", s.page, use, "fact", 1)
	edge("calls", use, read, "fact", 1)
	edge("contains", read, call, "fact", 1)
	edge("calls_route", call, s.route, "inferred", 0.9)
	edge("exposes_route", s.handler, s.route, "fact", 1)
	edge("reads_table", s.handler, s.table, "inferred", 0.9)
	edge("contains", s.handler, s.inner, "fact", 1)
	edge("writes_table", s.inner, s.table, "fact", 1)
	// A nested function with a stored caller: upstream follows the caller,
	// not the enclosing function (CH7).
	fmtFn := w.node("function", "api/items.py", "read_items._fmt", nil)
	edge("contains", s.handler, fmtFn, "fact", 1)
	edge("calls", s.handler, fmtFn, "fact", 1)
	edge("reads_table", fmtFn, s.table, "fact", 1)
	// A leaf function in a file with an unresolved_calls diagnostic: the
	// downstream chain ends there with CH11's line.
	logFn := w.node("function", "api/items.py", "read_items_log", nil)
	edge("calls", s.handler, logFn, "fact", 1)
	edge("calls", testFn, s.handler, "fact", 1)
	edge("calls", s.handler, helper, "fact", 1)
	extHash, err := contract.SyntheticHash(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ext := w.id("external", ".", "pypi:sqlmodel")
	w.rec(map[string]any{"type": "node", "identity": ext, "kind": "external", "level": 2, "name": "sqlmodel", "qualified_name": "pypi:sqlmodel",
		"file": ".", "content_hash": extHash, "attributes": map[string]any{}})
	edge("calls", s.handler, ext, "fact", 1)
	edge("uses_config", s.handler, setting, "fact", 1)
	edge("contains", use, ipc, "fact", 1)
	edge("invokes_command", ipc, s.command, "fact", 1)
	edge("exposes_command", greet, s.command, "fact", 1)
	for i := 0; i < traverse.MaxFanOut+1; i++ {
		caller := w.node("function", "api/hub.py", "caller"+strconv.Itoa(i), nil)
		edge("calls", caller, s.hub, "fact", 1)
	}
	// Created last, so the other nodes keep their crafted lines.
	w.node("function", "api/deps.py", "get_current_user", nil)
	for _, e := range edges {
		w.edge(e.kind, e.source, e.target, e.provenance, e.confidence)
	}
	w.rec(map[string]any{"type": "diagnostic", "file": "api/items.py", "severity": "info", "code": "unresolved_calls", "message": "3 calls could not be resolved without a type checker (external=1, unknown=2)"})
	w.rec(map[string]any{"type": "footer", "counts": map[string]any{"records": w.count["file"] + w.count["node"] + w.count["edge"] + 1, "file": w.count["file"], "node": w.count["node"], "edge": w.count["edge"], "diagnostic": 1}})
	return w, w.count["node"], s
}

func ingestChainStream(t *testing.T, s store.Store) chainSynth {
	t.Helper()
	w, nodes, ids := chainSynthStream(t)
	ingestStreamDiagnostics(t, s, &w.buf, "chainsynth", nodes, 1)
	return ids
}

type chainCase struct {
	name, repo, path string
}

func chainPath(id string, extra ...string) string {
	p := "/api/v0/architecture/chain?id=" + url.QueryEscape(id)
	for _, e := range extra {
		p += "&" + e
	}
	return p
}

func chainCases(s chainSynth) []chainCase {
	return []chainCase{
		{"chain-route", "chainsynth", chainPath(s.route)},
		{"chain-route-all", "chainsynth", chainPath(s.route, "scope=all")},
		{"chain-route-depth", "chainsynth", chainPath(s.route, "depth=1")},
		{"chain-table", "chainsynth", chainPath(s.table, "direction=up")},
		{"chain-handler", "chainsynth", chainPath(s.handler, "direction=down")},
		{"chain-inner", "chainsynth", chainPath(s.inner, "direction=up")},
		{"chain-command", "chainsynth", chainPath(s.command)},
		{"chain-dependency", "chainsynth", chainPath(s.dependency, "direction=up")},
		{"chain-file", "chainsynth", chainPath(s.file)},
		{"chain-component", "chainsynth", chainPath("ui")},
		{"chain-component-fixture", "fixture", chainPath("web")},
	}
}

// chainBodies serves every chain golden on both backends and returns the
// decoded bodies by backend and case.
func chainBodies(t *testing.T, tags map[string]bool, check bool) map[string]map[string]any {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	archTag = func(row string) {
		mu.Lock()
		tags[row] = true
		mu.Unlock()
	}
	t.Cleanup(func() { archTag = nil })
	backends(t, func(t *testing.T, w, s store.Store) {
		root := componentsFixtureRoot(t)
		indexRoot(t, root, w)
		ids := ingestChainStream(t, w)
		empty := t.TempDir()
		got := map[string]any{}
		for _, tc := range chainCases(ids) {
			opt := testOptions(root, s)
			base := root
			if tc.repo != "fixture" {
				opt = testOptions(empty, s)
				opt.Repository = tc.repo
				base = empty
			}
			rec := get(t, opt, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			if check {
				golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, base)
			}
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			var plain any
			if err := json.Unmarshal([]byte(golden.ReplaceRoot(string(data), filepath.ToSlash(base))), &plain); err != nil {
				t.Fatal(err)
			}
			got[tc.name] = plain
		}
		// CH12 fan-out: hub's 201 callers, not a golden (too large).
		opt := testOptions(empty, s)
		opt.Repository = "chainsynth"
		rec := get(t, opt, http.MethodGet, chainPath(ids.hub, "direction=up"), nil)
		var body chainResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Up == nil || len(body.Up.Steps) < 2 || body.Up.Members != traverse.MaxFanOut {
			t.Fatalf("hub chain: %s", rec.Body.String())
		}
		step := body.Up.Steps[1]
		if len(step.Bounds) != 1 || step.Bounds[0].Cap != traverse.CapFanOut || step.Bounds[0].Beyond != 1 || step.Map.Folded != traverse.MaxFanOut-mapPerStep {
			t.Errorf("hub chain step 1: bounds %+v, map %+v", step.Bounds, step.Map)
		}
		if !reflect.DeepEqual(body.Up.Caps, []string{traverse.CapFanOut}) || !body.Map.OpenInFlow {
			t.Errorf("hub chain: caps %v, map %+v", body.Up.Caps, body.Map)
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	return bodies
}

// TestChainGoldens checks every chain golden on both backends and that
// both backends produce the same bodies.
func TestChainGoldens(t *testing.T) {
	bodies := chainBodies(t, map[string]bool{}, true)
	if bodies["sqlite"] != nil && bodies["postgres"] != nil && !reflect.DeepEqual(bodies["sqlite"], bodies["postgres"]) {
		t.Error("SQLite and Postgres bodies differ")
	}
}

// TestChainContract keeps the chain table and the code 1:1: the task
// file's CH rows equal chainRows, every relation rule names a row, and the
// crafted snapshot reaches every row and the default CHD.
func TestChainContract(t *testing.T) {
	if got := contractSection(t, "Claim contract: chain"); !reflect.DeepEqual(got, rowIDs(chainRows)) {
		t.Errorf("task rows %v\ncode rows %v", got, rowIDs(chainRows))
	}
	known := map[string]bool{}
	for _, r := range chainRows {
		known[r.ID] = true
	}
	for _, rules := range [][]traverse.Rule{chainDown, chainUp} {
		for _, r := range rules {
			if !known[r.ID] {
				t.Errorf("rule %s %s names no row", r.ID, r.Edge)
			}
			if r.ID == "CH6" || r.ID == "CH8" {
				continue
			}
			if _, ok := chainWording[r.ID][r.Edge]; !ok {
				t.Errorf("rule %s %s has no wording", r.ID, r.Edge)
			}
		}
	}
	tags := map[string]bool{}
	chainBodies(t, tags, false)
	// CH13 is reached in project mode only (architecture_project_test.go).
	archProjectBodies(t, tags, false)
	checkReached(t, chainRows, tags)
	for _, form := range []string{"CH7/has-caller", "CH9/file", "CH9/component", "CH11/injected", "CH11/declared", "CH11/unresolved", "CH13/component"} {
		if !tags[form] {
			t.Errorf("form %s is not reached", form)
		}
	}
}

// TestChainWording checks every string the chain emits (gate: wording).
func TestChainWording(t *testing.T) {
	bodies := chainBodies(t, map[string]bool{}, false)
	var walk func(name string, v any)
	walk = func(name string, v any) {
		switch x := v.(type) {
		case string:
			if forbiddenWording.MatchString(x) {
				t.Errorf("%s: %q uses a forbidden word", name, x)
			}
		case []any:
			for _, item := range x {
				walk(name, item)
			}
		case map[string]any:
			for _, item := range x {
				walk(name, item)
			}
		}
	}
	for _, backend := range bodies {
		for name, body := range backend {
			walk(name, body)
		}
	}
}

// TestChainErrors checks the parameter errors.
func TestChainErrors(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ids := ingestChainStream(t, w)
		opt := testOptions(t.TempDir(), s)
		opt.Repository = "chainsynth"
		for path, code := range map[string]int{
			"/api/v0/architecture/chain":                           http.StatusBadRequest,
			chainPath(ids.route, "direction=sideways"):             http.StatusBadRequest,
			chainPath(ids.route, "depth=11"):                       http.StatusBadRequest,
			chainPath(ids.route, "depth=7"):                        http.StatusOK,
			chainPath(ids.route, "depth=10"):                       http.StatusOK,
			chainPath(ids.route, "depth=0"):                        http.StatusBadRequest,
			chainPath(ids.route, "scope=test"):                     http.StatusBadRequest,
			chainPath("function:api/items.py#nope"):                http.StatusNotFound,
			chainPath("nope"):                                      http.StatusNotFound,
			chainPath(ids.route, "direction=down", "depth=6"):      http.StatusOK,
			chainPath(ids.route, "direction=up", "direction=down"): http.StatusBadRequest,
		} {
			if rec := get(t, opt, http.MethodGet, path, nil); rec.Code != code {
				t.Errorf("%s: status %d, want %d (%s)", path, rec.Code, code, rec.Body.String())
			}
		}
		// T042: without depth the chain walks 6 steps while the cap it
		// reports is the maximum a request may name, 10.
		var out struct {
			Depth int `json:"depth"`
			Caps  struct {
				Depth int `json:"depth"`
			} `json:"caps"`
		}
		for _, tc := range []struct {
			path        string
			depth, caps int
		}{
			{chainPath(ids.route), traverse.ChainDefaultDepth, traverse.MaxDepth},
			{chainPath(ids.route, "depth=10"), 10, traverse.MaxDepth},
		} {
			rec := get(t, opt, http.MethodGet, tc.path, nil)
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Depth != tc.depth || out.Caps.Depth != tc.caps {
				t.Errorf("%s: depth %d caps %d (%v), want %d and %d", tc.path, out.Depth, out.Caps.Depth, err, tc.depth, tc.caps)
			}
		}
		if traverse.ChainDefaultDepth != 6 || traverse.MaxDepth != 10 {
			t.Errorf("chain default %d, maximum %d; want 6 and 10 (T042)", traverse.ChainDefaultDepth, traverse.MaxDepth)
		}
	})
}

// TestMapBudgetSingleMember checks the map budget never folds a single
// member: a one-member step past the third is drawn when its parent is,
// a step one over the per-step budget draws all, and two over fold two.
func TestMapBudgetSingleMember(t *testing.T) {
	member := func(id string, parents ...string) chainMember {
		return chainMember{ID: id, Parents: append([]string{}, parents...)}
	}
	step := func(n int, ms ...chainMember) chainStep { return chainStep{Step: n, Members: ms} }
	many := func(prefix string, n int, parent string) []chainMember {
		var out []chainMember
		for i := 0; i < n; i++ {
			out = append(out, member(fmt.Sprintf("%s%d", prefix, i), parent))
		}
		return out
	}
	up := &chainSide{Steps: []chainStep{
		step(0, member("s")),
		step(1, member("a", "s")),
		step(2, member("b", "a")),
		step(3, member("c", "b")),
		step(4, member("d", "c")), // past the third step, one member: drawn
	}}
	down := &chainSide{Steps: []chainStep{
		step(0, member("s")),
		step(1, many("six", 6, "s")...),      // one over the step budget: all drawn
		step(2, many("seven", 7, "six0")...), // two over: two fold
		step(3, member("x", "seven6")),       // its parent folded at step 2
		step(4, member("y", "seven5")),       // parent folded at step 2: folded
	}}
	out := &chainResponse{Up: up, Down: down}
	applyMapBudget(out)
	if got := up.Steps[4].Map; got.Folded != 0 || len(got.Shown) != 1 {
		t.Errorf("up step 4: %+v, want the single member drawn", got)
	}
	if got := down.Steps[1].Map; got.Folded != 0 || len(got.Shown) != 6 {
		t.Errorf("down step 1: %+v, want 6 drawn", got)
	}
	if got := down.Steps[2].Map; got.Folded != 2 {
		t.Errorf("down step 2: %+v, want 2 folded", got)
	}
	if got := down.Steps[3].Map; got.Folded != 0 {
		t.Errorf("down step 3 (within the step budget): %+v, want drawn", got)
	}
	if got := down.Steps[4].Map; got.Folded != 1 || len(got.Shown) != 0 {
		t.Errorf("down step 4: %+v, want folded (parent drawn? %v)", got, down.Steps[3].Map.Shown)
	}
	if out.Map.Folded != 3 || !out.Map.OpenInFlow {
		t.Errorf("map: %+v, want 3 folded and open_in_flow", out.Map)
	}
}

// TestChainBridgesPassesFunction checks that row CH8 follows the
// passes_function facts of T026 both ways as dashed bridges
// with the decided wording, alongside React renders and react-router
// routes_to.
func TestChainBridgesPassesFunction(t *testing.T) {
	e := contract.Edge{Kind: "passes_function", Source: "function:a.ts#Items", Target: "function:a.ts#createItem"}
	for _, tc := range []struct {
		rules []traverse.Rule
		from  string
		dir   store.Direction
	}{{chainDown, "function", store.Outgoing}, {chainUp, "function", store.Incoming}, {chainUp, "ui_route", store.Incoming}} {
		r := traverse.Match(tc.rules, tc.from, e, tc.dir, nil)
		if r == nil || r.ID != "CH8" {
			t.Errorf("%s %v: passes_function not followed by CH8 (%v)", tc.from, tc.dir, r)
		}
	}
	if got := chainWording["CH8"]["passes_function"]; got != [2]string{"passed as callback, call not modelled", "passed as callback by, call not modelled"} {
		t.Errorf("wording %v", got)
	}
}

// edgeFailingStore fails every edge read, as a broken database would.
type edgeFailingStore struct {
	store.Store
}

func (edgeFailingStore) Edges(context.Context, string, []string, []string, store.Direction) ([]contract.Edge, error) {
	return nil, errInjected
}

// TestChainNodeStoreFailure checks that a store failure while walking a
// node's chain is a 500, not an empty chain (T052: the walk's error was
// assigned to a shadowed variable and dropped).
func TestChainNodeStoreFailure(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ids := ingestChainStream(t, w)
		opt := testOptions(t.TempDir(), edgeFailingStore{s})
		opt.Repository = "chainsynth"
		for _, id := range []string{ids.handler, ids.file} {
			rec := get(t, opt, http.MethodGet, chainPath(id), nil)
			if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), errInjected.Error()) {
				t.Errorf("%s: status %d %s, want 500 with the store error", id, rec.Code, rec.Body.String())
			}
		}
	})
}

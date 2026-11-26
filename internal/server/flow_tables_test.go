package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/store"
)

// flowTables holds the identities of the crafted snapshot "flowtables".
type flowTables struct {
	route, handler, operation, local, patch, helper, inner, deep, deepInner, load string
	item, user, audit, log, secret                                                string
}

// flowTablesStream is "flowtables" (T026 AG-C3): GET /items is handled by
// read_items, which reads item and contains the nested operation (CH7,
// hop 0) and a class (not a function, not followed); operation calls
// _patch (hop 1), which may write item through a query builder and has
// two unknown sites (one narrowed to user and a table that is not stored,
// one to any table); _patch calls _helper (hop 2), which reads audit and
// contains inner, which writes log; _helper calls _deep (hop 3), which
// reads secret and item and has an unknown site, and contains an inner
// function that writes user: two tables and two functions beyond hop 2.
// The UI caller load reads secret: an upstream function's tables are not
// the handler's and are not returned.
func flowTablesStream(t *testing.T) (*streamWriter, int, flowTables) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "t026-c3"}, "repository": "flowtables", "generated_at": "2026-11-18T00:00:00Z"})
	for _, f := range []string{"api/items.py", "api/helpers.py", "api/models.py", "web/app.ts"} {
		w.rec(map[string]any{"type": "file", "path": f, "language": "unknown", "precision": "best_effort", "size": 0, "content_hash": contract.ContentHash(nil)})
	}
	var s flowTables
	table := func(name string) string {
		return w.node("table", "api/models.py", name, map[string]any{"set": "models", "tool": "sqlmodel"})
	}
	s.item, s.user, s.audit, s.log, s.secret = table("item"), table("user"), table("audit"), table("log"), table("secret")
	ghost := w.id("table", "api/models.py", "ghost")
	s.handler = w.id("function", "api/items.py", "read_items")
	s.route = w.node("route", "api/items.py", "GET /items", map[string]any{"framework": "fastapi", "method": "GET", "pattern": "/items", "handler": s.handler, "handler_inline": false,
		"paths": []any{map[string]any{"template": "/items", "conditional": false, "resolution": "literal"}}})
	w.node("function", "api/items.py", "read_items", map[string]any{"table_access": []any{map[string]any{"kind": "reads_table", "target": s.item, "role": "executes"}}})
	s.operation = w.node("function", "api/items.py", "read_items.operation", nil)
	s.local = w.node("class", "api/items.py", "read_items.Local", nil)
	s.patch = w.node("function", "api/helpers.py", "_patch", map[string]any{
		"table_access": []any{map[string]any{"kind": "writes_table", "target": s.item, "role": "builds"}},
		"table_access_unknown": []any{
			map[string]any{"row": "CT-P4", "line": 12, "span": nil, "tool": "sqlmodel", "reason": "object_class_unproven", "partial": false, "candidates": []any{s.user, ghost}},
			map[string]any{"row": "CT-P5", "line": 14, "span": nil, "tool": "sql", "reason": "sql_not_literal", "partial": true, "candidates": nil},
		}})
	s.helper = w.node("function", "api/helpers.py", "_helper", nil)
	s.inner = w.node("function", "api/helpers.py", "_helper.inner", nil)
	s.deep = w.node("function", "api/helpers.py", "_deep", map[string]any{"table_access_unknown": []any{
		map[string]any{"row": "CT-P5", "line": 30, "span": nil, "tool": "sql", "reason": "sql_not_literal", "partial": false, "candidates": nil},
	}})
	s.deepInner = w.node("function", "api/helpers.py", "_deep.inner", nil)
	s.load = w.node("function", "web/app.ts", "load", nil)
	call := w.node("client_call", "web/app.ts", "load.GET '/items'", map[string]any{"library": "fetch", "method": "GET", "status": "linked",
		"paths": []any{map[string]any{"template": "/items", "status": "linked", "match": "exact", "candidates": []any{s.route}, "candidate_count": 0}}})
	w.edge("exposes_route", s.handler, s.route, "fact", 1)
	w.edge("contains", s.load, call, "fact", 1)
	w.edge("calls_route", call, s.route, "inferred", 0.9)
	w.edge("reads_table", s.load, s.secret, "fact", 1)
	w.edge("reads_table", s.handler, s.item, "fact", 1)
	w.edge("contains", s.handler, s.operation, "fact", 1)
	w.edge("contains", s.handler, s.local, "fact", 1)
	w.edge("calls", s.operation, s.patch, "fact", 1)
	w.edge("writes_table", s.patch, s.item, "inferred", 0.9)
	w.edge("calls", s.patch, s.helper, "fact", 1)
	w.edge("reads_table", s.helper, s.audit, "fact", 1)
	w.edge("contains", s.helper, s.inner, "fact", 1)
	w.edge("writes_table", s.inner, s.log, "fact", 1)
	w.edge("calls", s.helper, s.deep, "fact", 1)
	w.edge("reads_table", s.deep, s.secret, "fact", 1)
	w.edge("reads_table", s.deep, s.item, "fact", 1)
	w.edge("contains", s.deep, s.deepInner, "fact", 1)
	w.edge("writes_table", s.deepInner, s.user, "fact", 1)
	w.rec(map[string]any{"type": "footer", "counts": map[string]any{"records": w.count["file"] + w.count["node"] + w.count["edge"], "file": w.count["file"], "node": w.count["node"], "edge": w.count["edge"], "diagnostic": 0}})
	return w, w.count["node"], s
}

func ingestFlowTables(t *testing.T, st store.Store) flowTables {
	t.Helper()
	w, nodes, ids := flowTablesStream(t)
	ingestStream(t, st, &w.buf, "flowtables", nodes)
	return ids
}

// flowTablesBody is the part of the AG-C3 fields the checks read.
type flowTablesBody struct {
	Edges []struct {
		Kind    string  `json:"kind"`
		Source  string  `json:"source"`
		Target  string  `json:"target"`
		Wording *string `json:"wording"`
	} `json:"edges"`
	MayAlsoUse   []flowMayAlsoUse `json:"may_also_use"`
	TablesBeyond *flowBeyond      `json:"tables_beyond"`
}

// TestFlowTables checks the AG-C3 rules directly (roles, hops, stored
// edges, wording, unknown sites, the hop-3 count) and the golden on both
// backends, with equal bodies.
func TestFlowTables(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]string{}
	backends(t, func(t *testing.T, w, st store.Store) {
		s := ingestFlowTables(t, w)
		options, roots := flowOptions(t, t.TempDir(), st)
		opt := options["flowtables"]
		path := flowPath(s.route, "&scope=all")
		body := getFlow(t, opt, path)
		checkStored(t, st, "flowtables", body)
		roles, hops := body.roles(), body.hops()
		want := map[string]struct {
			roles []string
			hop   int
		}{
			s.route:     {[]string{roleEndpoint}, 0},
			s.handler:   {[]string{roleHandler}, 0},
			s.operation: {[]string{roleNested}, 0},
			s.patch:     {[]string{roleCallee}, 1},
			s.helper:    {[]string{roleCallee}, 2},
			s.inner:     {[]string{roleNested}, 2},
			s.load:      {[]string{roleOwner}, 1},
			s.item:      {[]string{roleTable}, 1},
			s.audit:     {[]string{roleTable}, 2},
			s.log:       {[]string{roleTable}, 2},
		}
		for id, w := range want {
			if !reflect.DeepEqual(roles[id], w.roles) || hops[id] != w.hop {
				t.Errorf("%s: roles %v hop %d, want %v hop %d", id, roles[id], hops[id], w.roles, w.hop)
			}
		}
		// Not returned: the class (not a function), hop-3 functions, their
		// tables and the upstream function's table.
		for _, id := range []string{s.local, s.deep, s.deepInner, s.secret, s.user} {
			if roles[id] != nil {
				t.Errorf("%s returned with roles %v", id, roles[id])
			}
		}
		for _, e := range [][3]string{
			{"contains", s.handler, s.operation}, {"contains", s.helper, s.inner}, {"calls", s.operation, s.patch},
			{"reads_table", s.handler, s.item}, {"writes_table", s.patch, s.item}, {"reads_table", s.helper, s.audit}, {"writes_table", s.inner, s.log},
		} {
			if !body.hasEdge(e[0], e[1], e[2]) {
				t.Errorf("missing edge %v", e)
			}
		}
		var extra flowTablesBody
		decodeInto(t, get(t, opt, http.MethodGet, path, nil), &extra)
		words := map[[2]string]string{}
		for _, e := range extra.Edges {
			if e.Kind == "reads_table" || e.Kind == "writes_table" {
				if e.Wording == nil {
					t.Errorf("%s %s -> %s has no wording", e.Kind, e.Source, e.Target)
					continue
				}
				words[[2]string{e.Source, e.Target}] = *e.Wording
			} else if e.Wording != nil {
				t.Errorf("%s edge carries a wording", e.Kind)
			}
		}
		if words[[2]string{s.handler, s.item}] != "reads" || words[[2]string{s.patch, s.item}] != "builds a query on" || words[[2]string{s.inner, s.log}] != "writes" {
			t.Errorf("wording %v", words)
		}
		if len(extra.MayAlsoUse) != 2 || extra.MayAlsoUse[0].Accessor != s.patch || extra.MayAlsoUse[0].Hop != 1 || siteLine(extra.MayAlsoUse[0].Line) != 12 ||
			len(extra.MayAlsoUse[0].Candidates) != 2 || extra.MayAlsoUse[0].Candidates[1].Name != nil || extra.MayAlsoUse[1].Candidates != nil || !extra.MayAlsoUse[1].Partial {
			t.Errorf("may_also_use %+v", extra.MayAlsoUse)
		}
		if extra.TablesBeyond == nil || *extra.TablesBeyond != (flowBeyond{Hop: 3, Tables: 2, Functions: 2, MayAlsoUse: 1}) {
			t.Errorf("tables_beyond %+v", extra.TablesBeyond)
		}
		rec := get(t, opt, http.MethodGet, path, nil)
		v := normalizeAPI(t, rec.Body.Bytes())
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		golden.CheckRoot(t, filepath.Join("testdata", "flow-tables.golden.json"), v, roots["flowtables"])
		mu.Lock()
		bodies[filepath.Base(t.Name())] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots["flowtables"]))
		mu.Unlock()
	})
	if bodies["sqlite"] != "" && bodies["postgres"] != "" && bodies["sqlite"] != bodies["postgres"] {
		t.Error("flow-tables: SQLite and Postgres bodies differ")
	}
}

// TestFlowTablesHop2Dropped checks that a dropped hop 2 drops its tables
// and the hop-3 count, which would then count from a frontier the
// response does not show.
func TestFlowTablesHop2Dropped(t *testing.T) {
	backends(t, func(t *testing.T, w, st store.Store) {
		ingestFlowBig(t, w)
		options, _ := flowOptions(t, t.TempDir(), st)
		var extra flowTablesBody
		decodeInto(t, get(t, options["flowbig"], http.MethodGet, flowPath(bigWide, ""), nil), &extra)
		if extra.TablesBeyond != nil || len(extra.MayAlsoUse) != 0 {
			t.Errorf("wide: tables_beyond %+v, may_also_use %v", extra.TablesBeyond, extra.MayAlsoUse)
		}
	})
}

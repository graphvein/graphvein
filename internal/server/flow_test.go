package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
)

// Identities of the crafted flow snapshot (testdata/flow-graph.ndjson).
const (
	flowRoute      = "route:server/api.ts#GET%20%2Fflows%2F%3Aid"
	flowOther      = "route:server/api.ts#GET%20%2Fpublic%2F%3Aid"
	flowNever      = "route:server/api.ts#GET%20%2Fnever"
	flowReadFlow   = "function:server/api.ts#readFlow"
	flowLoadFlow   = "function:server/api.ts#loadFlow"
	flowAudit      = "function:server/api.ts#audit"
	flowStrip      = "function:lib/util.ts#strip"
	flowMakeHooks  = "function:web/hooks.ts#makeHooks"
	flowUseGetFlow = "function:web/hooks.ts#makeHooks.useGetFlow"
	flowGetFlowFn  = "function:web/hooks.ts#makeHooks.useGetFlow.getFlowFn"
	flowPage       = "function:web/pages.ts#Page"
	flowOtherFn    = "function:web/pages.ts#Other"
	flowApp        = "function:web/pages.ts#App"
	flowFlowPage   = "function:web/pages.ts#FlowPage"
	flowPlayground = "function:web/pages.ts#Playground"
	flowBoot       = "function:web/pages.ts#Boot"
	flowSettings   = "function:web/pages.ts#Settings"
	flowTestFile   = "file:web/hooks.test.ts#web%2Fhooks.test.ts"
)

// ingestStream ingests one snapshot and requires it to have no
// diagnostics and nodes nodes, so a rejected record cannot hide a case.
func ingestStream(t *testing.T, s store.Store, r interface{ Read([]byte) (int, error) }, repo string, nodes int) {
	t.Helper()
	ingestStreamDiagnostics(t, s, r, repo, nodes, 0)
}

// ingestStreamDiagnostics is ingestStream for a stream that carries
// diagnostics records of its own.
func ingestStreamDiagnostics(t *testing.T, s store.Store, r interface{ Read([]byte) (int, error) }, repo string, nodes, diagnostics int) {
	t.Helper()
	if err := ingest.Stream(context.Background(), r, s); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Dump(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Diagnostics) != diagnostics || len(snapshot.Nodes) != nodes {
		t.Fatalf("%s snapshot: %d nodes, diagnostics %v", repo, len(snapshot.Nodes), snapshot.Diagnostics)
	}
}

func ingestFlowGraph(t *testing.T, s store.Store) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "flow-graph.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ingestStream(t, s, f, "flowgraph", 25)
}

// Generated snapshot "flowbig" for the caps (too large to read as a
// golden): GET /fan's handler calls 30 functions (fan-out cap 25); GET
// /wide's handler calls 25 functions that call 25 each (625 hop-2 nodes,
// over the node cap), g00 also calls wo, the owner of /wide's one call
// (so g00 is a hop-2 caller too), and g01 calls a 26th (a hop-2 cut); GET
// /cut's handler calls 30 functions and c00 calls c25, which the cap cut;
// GET /many has 201 callers and 201 near misses.
const (
	bigFan  = "route:server/big.ts#GET%20%2Ffan"
	bigWide = "route:server/big.ts#GET%20%2Fwide"
	bigCut  = "route:server/big.ts#GET%20%2Fcut"
	bigMany = "route:server/big.ts#GET%20%2Fmany"
)

type streamWriter struct {
	t     *testing.T
	buf   bytes.Buffer
	count map[string]int
	line  int
}

func (w *streamWriter) rec(v map[string]any) {
	data, err := json.Marshal(v)
	if err != nil {
		w.t.Fatal(err)
	}
	w.buf.Write(append(data, '\n'))
	if typ := v["type"].(string); typ != "header" {
		w.count[typ]++
	}
}

func (w *streamWriter) id(kind, file, qualified string) string {
	id, err := contract.Identity(kind, file, qualified)
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *streamWriter) span() map[string]any {
	w.line++
	return map[string]any{"start_line": w.line, "start_column": 0, "end_line": w.line, "end_column": 1}
}

func (w *streamWriter) node(kind, file, qualified string, attrs map[string]any) string {
	id := w.id(kind, file, qualified)
	if attrs == nil {
		attrs = map[string]any{}
	}
	w.rec(map[string]any{"type": "node", "identity": id, "kind": kind, "level": 4, "name": qualified, "qualified_name": qualified,
		"file": file, "span": w.span(), "content_hash": contract.ContentHash(nil), "attributes": attrs})
	return id
}

func (w *streamWriter) edge(kind, source, target, provenance string, confidence float64) {
	w.rec(map[string]any{"type": "edge", "kind": kind, "source": source, "target": target, "provenance": provenance, "confidence": confidence, "evidence": []any{}})
}

func bigFlowStream(t *testing.T) (*bytes.Buffer, int) {
	w := &streamWriter{t: t, count: map[string]int{}}
	w.rec(map[string]any{"type": "header", "contract_version": 1, "indexer": map[string]any{"name": "crafted", "version": "t016f1"}, "repository": "flowbig", "generated_at": "2026-11-03T00:00:00Z"})
	for _, f := range []string{"server/big.ts", "web/big.ts"} {
		w.rec(map[string]any{"type": "file", "path": f, "language": "typescript", "precision": "precise", "size": 0, "content_hash": contract.ContentHash(nil)})
	}
	route := func(name, handler string) string {
		var h any
		if handler != "" {
			h = handler
		}
		return w.node("route", "server/big.ts", name, map[string]any{"framework": "express", "method": "GET", "handler": h, "handler_inline": handler == "", "paths": []any{}})
	}
	hf, hb, hc := w.id("function", "server/big.ts", "hf"), w.id("function", "server/big.ts", "hb"), w.id("function", "server/big.ts", "hc")
	fan, wide, cut, many, tie := route("GET /fan", hf), route("GET /wide", hb), route("GET /cut", hc), route("GET /many", ""), route("GET /many-b", "")
	var edges [][2]string
	w.node("function", "server/big.ts", "hf", nil)
	for i := 0; i < 30; i++ {
		edges = append(edges, [2]string{hf, w.node("function", "server/big.ts", fmt.Sprintf("f%02d", i), nil)})
	}
	w.node("function", "server/big.ts", "hb", nil)
	var g00 string
	for i := 0; i < 25; i++ {
		g := w.node("function", "server/big.ts", fmt.Sprintf("g%02d", i), nil)
		if i == 0 {
			g00 = g
		}
		edges = append(edges, [2]string{hb, g})
		n := 25
		if i == 1 {
			n = 26
		}
		for j := 0; j < n; j++ {
			edges = append(edges, [2]string{g, w.node("function", "server/big.ts", fmt.Sprintf("g%02d.h%02d", i, j), nil)})
		}
	}
	w.node("function", "server/big.ts", "hc", nil)
	var cs []string
	for i := 0; i < 30; i++ {
		cs = append(cs, w.node("function", "server/big.ts", fmt.Sprintf("c%02d", i), nil))
		edges = append(edges, [2]string{hc, cs[i]})
	}
	edges = append(edges, [2]string{cs[0], cs[25]})
	wo := w.node("function", "web/big.ts", "wo", nil)
	wideCall := w.node("client_call", "web/big.ts", "wo.GET /wide", map[string]any{"status": "linked",
		"paths": []any{map[string]any{"template": "/wide", "status": "linked", "match": "exact", "candidates": []any{wide}, "candidate_count": 0}}})
	edges = append(edges, [2]string{g00, wo})
	owner := w.node("function", "web/big.ts", "many", nil)
	var linked, misses []string
	for i := 0; i < 201; i++ {
		linked = append(linked, w.node("client_call", "web/big.ts", fmt.Sprintf("many.GET %03d", i), map[string]any{"status": "linked",
			"paths": []any{map[string]any{"template": "/many", "status": "linked", "match": "exact", "candidates": []any{many}, "candidate_count": 0}}}))
	}
	for i := 0; i < 201; i++ {
		misses = append(misses, w.node("client_call", "web/big.ts", fmt.Sprintf("many.TIE %03d", i), map[string]any{"status": "ambiguous",
			"paths": []any{map[string]any{"template": "/many", "status": "ambiguous", "candidates": []any{many, tie}, "candidate_count": 2}}}))
	}
	w.edge("exposes_route", hf, fan, "fact", 1)
	w.edge("exposes_route", hb, wide, "fact", 1)
	w.edge("exposes_route", hc, cut, "fact", 1)
	w.edge("contains", wo, wideCall, "fact", 1)
	w.edge("calls_route", wideCall, wide, "inferred", 0.9)
	for _, c := range append(linked, misses...) {
		w.edge("contains", owner, c, "fact", 1)
	}
	for _, c := range linked {
		w.edge("calls_route", c, many, "inferred", 0.9)
	}
	for _, e := range edges {
		w.edge("calls", e[0], e[1], "fact", 1)
	}
	nodes := w.count["node"]
	w.rec(map[string]any{"type": "footer", "counts": map[string]any{"records": w.count["file"] + w.count["node"] + w.count["edge"], "file": w.count["file"], "node": nodes, "edge": w.count["edge"], "diagnostic": 0}})
	return &w.buf, nodes
}

func ingestFlowBig(t *testing.T, s store.Store) {
	t.Helper()
	stream, nodes := bigFlowStream(t)
	ingestStream(t, s, stream, "flowbig", nodes)
}

// flowOptions are handler options per repository of the flow tests.
func flowOptions(t *testing.T, fixtureRoot string, s store.Store) (map[string]Options, map[string]string) {
	t.Helper()
	empty := t.TempDir()
	options := map[string]Options{"fixture": testOptions(fixtureRoot, s)}
	roots := map[string]string{"fixture": fixtureRoot}
	for _, repo := range []string{"flowgraph", "flowbig", "flowtables", "statuses"} {
		opt := testOptions(empty, s)
		opt.Repository = repo
		options[repo] = opt
		roots[repo] = empty
	}
	return options, roots
}

func flowPath(id, extra string) string {
	return "/api/v0/api/flow?id=" + url.QueryEscape(id) + extra
}

// TestFlowGoldens checks every flow golden on both backends and that both
// backends produce the same bodies.
func TestFlowGoldens(t *testing.T) {
	cases := []apiCase{
		{"flow-route", "fixture", flowPath("route:server/app.ts#GET%20%2Ft016d-items", "&scope=all")},
		{"flow-route-literal-origin", "fixture", flowPath("route:server/app.ts#GET%20%2Ft016d-items%2F%3Aid", "&scope=all")},
		{"flow-route-suffix", "fixture", flowPath("route:server/app.ts#GET%20%2Ft016d-api%2Ft016d-users", "&scope=all")},
		{"flow-route-never", "fixture", flowPath("route:server/app.ts#GET%20%2Ft016d-never", "")},
		{"flow-route-ambiguous", "fixture", flowPath("route:server/app.ts#GET%20%2Ft016d-tie", "")},
		{"flow-route-test-only", "fixture", flowPath("route:server/tests/routes.ts#GET%20%2Ft016d-test-only", "&scope=all")},
		{"flow-command", "fixture", flowPath("ipc_command:app/src/main.rs#main.COMMAND%20t016d_items", "&scope=all")},
		{"flow-graph", "flowgraph", flowPath(flowRoute, "&scope=all")},
	}
	var mu sync.Mutex
	bodies := map[string]map[string]string{}
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		ingestFlowGraph(t, w)
		options, roots := flowOptions(t, root, s)
		got := map[string]string{}
		for _, tc := range cases {
			rec := get(t, options[tc.repo], http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: status %d %s", tc.path, rec.Code, rec.Body.String())
			}
			v := normalizeAPI(t, rec.Body.Bytes())
			data, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			got[tc.name] = golden.ReplaceRoot(string(data), filepath.ToSlash(roots[tc.repo]))
			golden.CheckRoot(t, filepath.Join("testdata", tc.name+".golden.json"), v, roots[tc.repo])
		}
		mu.Lock()
		bodies[filepath.Base(t.Name())] = got
		mu.Unlock()
	})
	if bodies["sqlite"] != nil && bodies["postgres"] != nil {
		for name, body := range bodies["sqlite"] {
			if bodies["postgres"][name] != body {
				t.Errorf("%s: SQLite and Postgres bodies differ", name)
			}
		}
	}
}

// flowBody is the part of a flow response the checks read.
type flowBody struct {
	Handler struct {
		State string `json:"state"`
	} `json:"handler"`
	Counts flowCounts `json:"counts"`
	Nodes  []struct {
		Node struct {
			Identity   string         `json:"identity"`
			Kind       string         `json:"kind"`
			Attributes map[string]any `json:"attributes"`
		} `json:"node"`
		Roles   []string    `json:"roles"`
		Hop     int         `json:"hop"`
		Omitted flowOmitted `json:"omitted"`
	} `json:"nodes"`
	Edges []struct {
		Kind       string              `json:"kind"`
		Source     string              `json:"source"`
		Target     string              `json:"target"`
		Provenance string              `json:"provenance"`
		Confidence float64             `json:"confidence"`
		Evidence   []contract.Evidence `json:"evidence"`
		Match      *string             `json:"match"`
	} `json:"edges"`
	Candidates   []flowCandidate `json:"candidates"`
	SkippedEdges int             `json:"skipped_edges"`
	Truncated    *flowTruncated  `json:"truncated"`
}

func (b flowBody) roles() map[string][]string {
	out := map[string][]string{}
	for _, n := range b.Nodes {
		out[n.Node.Identity] = n.Roles
	}
	return out
}

func (b flowBody) hops() map[string]int {
	out := map[string]int{}
	for _, n := range b.Nodes {
		out[n.Node.Identity] = n.Hop
	}
	return out
}

func (b flowBody) withRole(role string) []string {
	var out []string
	for _, n := range b.Nodes {
		if contains(n.Roles, role) {
			out = append(out, n.Node.Identity)
		}
	}
	sort.Strings(out)
	return out
}

func (b flowBody) hasEdge(kind, source, target string) bool {
	for _, e := range b.Edges {
		if e.Kind == kind && e.Source == source && e.Target == target {
			return true
		}
	}
	return false
}

func getFlow(t *testing.T, opt Options, path string) flowBody {
	t.Helper()
	var out flowBody
	decodeInto(t, get(t, opt, http.MethodGet, path, nil), &out)
	return out
}

// checkStored asserts that every returned edge is stored exactly as
// returned and joins two returned nodes.
func checkStored(t *testing.T, s store.Store, repo string, body flowBody) {
	t.Helper()
	included := body.roles()
	for _, e := range body.Edges {
		stored, err := s.Edges(context.Background(), repo, []string{e.Source}, []string{e.Kind}, store.Outgoing)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, x := range stored {
			if x.Target == e.Target {
				found = true
				ev := e.Evidence
				if len(ev) == 0 {
					ev = nil
				}
				want := x.Evidence
				if len(want) == 0 {
					want = nil
				}
				if x.Provenance != e.Provenance || x.Confidence != e.Confidence || !reflect.DeepEqual(ev, want) {
					t.Errorf("%s %s -> %s: returned %s %v, stored %s %v", e.Kind, e.Source, e.Target, e.Provenance, e.Confidence, x.Provenance, x.Confidence)
				}
			}
		}
		if !found {
			t.Errorf("%s %s -> %s is not a stored edge", e.Kind, e.Source, e.Target)
		}
		if included[e.Source] == nil || included[e.Target] == nil {
			t.Errorf("%s %s -> %s joins a node that is not returned", e.Kind, e.Source, e.Target)
		}
	}
}

// TestFlowChecks asserts the D2 rules on the crafted snapshot directly, so
// a golden rewrite cannot hide them.
func TestFlowChecks(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestFlowGraph(t, w)
		options, _ := flowOptions(t, t.TempDir(), s)
		opt := options["flowgraph"]
		body := getFlow(t, opt, flowPath(flowRoute, "&scope=all"))
		checkStored(t, s, "flowgraph", body)
		want := map[string][]string{
			flowRoute:    {roleEndpoint},
			flowReadFlow: {roleHandler},
			"client_call:web/hooks.ts#makeHooks.useGetFlow.getFlowFn.GET%20%27%2Fflows%2F%3Aid%27": {roleCall},
			"client_call:web/pages.ts#Page.GET%20%27%2Fflows%2F1%27":                               {roleCall},
			"client_call:web/pages.ts#Page.GET%20%27%2Fflows%2F2%27":                               {roleCall},
			"client_call:web/hooks.test.ts#GET%20%27%2Fflows%2F3%27":                               {roleCall},
			flowGetFlowFn:  {roleOwner},
			flowPage:       {roleOwner, roleCallee}, // audit, a hop-1 callee, calls it
			flowTestFile:   {roleOwner},
			flowOtherFn:    {roleNearMissOwner},
			flowUseGetFlow: {roleEnclosing},
			flowOther:      {roleOtherTarget},
			"client_call:web/pages.ts#Other.GET%20%27%2Ftie%27": {roleNearMiss},
			flowLoadFlow:   {roleCallee},
			flowAudit:      {roleCallee, roleCaller},
			flowStrip:      {roleCallee},
			flowApp:        {roleCaller},
			flowFlowPage:   {roleCaller},
			flowPlayground: {roleCaller},
		}
		if got := body.roles(); !reflect.DeepEqual(got, want) {
			t.Errorf("roles:\n got %v\nwant %v", got, want)
		}
		hops := body.hops()
		for id, hop := range map[string]int{flowRoute: 0, flowReadFlow: 0, flowUseGetFlow: 1, flowAudit: 1, flowStrip: 2, flowFlowPage: 2, flowApp: 2} {
			if hops[id] != hop {
				t.Errorf("%s: hop %d, want %d", id, hops[id], hop)
			}
		}
		// The enclosing step (UQ1 B) is one stored contains edge, never a
		// calls edge, and never goes further: makeHooks and its caller
		// Boot are not included.
		if !body.hasEdge("contains", flowUseGetFlow, flowGetFlowFn) || body.hasEdge("calls", flowUseGetFlow, flowGetFlowFn) {
			t.Error("enclosing step is not the stored contains edge")
		}
		// Callers reached through it call the enclosing function, not the
		// route: their only edges target useGetFlow.
		for _, caller := range []string{flowFlowPage, flowPlayground} {
			for _, e := range body.Edges {
				if e.Source == caller && (e.Kind != "calls" || e.Target != flowUseGetFlow) {
					t.Errorf("%s: edge %s -> %s", caller, e.Kind, e.Target)
				}
			}
		}
		// Recursion, a two-node cycle, a passed-through provenance, both
		// conditional targets, the near miss as a candidate only.
		for _, e := range [][3]string{{"calls", flowLoadFlow, flowLoadFlow}, {"calls", flowStrip, flowLoadFlow}, {"calls", flowLoadFlow, flowStrip}, {"calls", flowAudit, flowPage}, {"calls", flowApp, flowPage}, {"exposes_route", flowReadFlow, flowRoute}} {
			if !body.hasEdge(e[0], e[1], e[2]) {
				t.Errorf("missing edge %v", e)
			}
		}
		for _, e := range body.Edges {
			if e.Source == flowApp && e.Provenance != "assessment" || e.Kind == "calls_route" && e.Source == "client_call:web/pages.ts#Page.GET%20%27%2Fflows%2F2%27" && (e.Match == nil || *e.Match != "suffix" || e.Confidence != 0.6) {
				t.Errorf("edge not as stored: %+v", e)
			}
			if strings.Contains(e.Source, "Other.GET") || strings.Contains(e.Target, "Other.GET") && e.Kind != "contains" {
				t.Errorf("near miss drawn as an edge: %+v", e)
			}
		}
		if len(body.Candidates) != 1 || body.Candidates[0].Endpoint != flowRoute || body.Candidates[0].Relation != "ambiguous_candidate" || body.Candidates[0].CandidateCount != 2 {
			t.Errorf("candidates %+v", body.Candidates)
		}
		if body.Counts != (flowCounts{Callers: 4, NearMisses: 1}) || body.SkippedEdges != 0 || body.Truncated != nil {
			t.Errorf("counts %+v skipped %d truncated %+v", body.Counts, body.SkippedEdges, body.Truncated)
		}
		// Production scope leaves out the test call and its file owner;
		// the other target keeps its raw test scope.
		prod := getFlow(t, opt, flowPath(flowRoute, ""))
		roles := prod.roles()
		if roles[flowTestFile] != nil || roles["client_call:web/hooks.test.ts#GET%20%27%2Fflows%2F3%27"] != nil || prod.Counts.Callers != 3 {
			t.Errorf("production: %v", roles)
		}
		for _, n := range prod.Nodes {
			if n.Node.Identity == flowOther && n.Node.Attributes["scope"] != "test" {
				t.Errorf("other target scope %v", n.Node.Attributes["scope"])
			}
		}
		// A handler that is not stored: state missing, nothing downstream.
		never := getFlow(t, opt, flowPath(flowNever, ""))
		if never.Handler.State != handlerMissing || len(never.Nodes) != 1 || len(never.Edges) != 0 {
			t.Errorf("missing handler: %+v", never)
		}
		// A stored edge whose other end is not a stored node is skipped
		// and counted. The store rejects such edges, so a wrapper adds one.
		dangling := opt
		dangling.Store = danglingStore{Store: s}
		got := getFlow(t, dangling, flowPath(flowRoute, "&scope=all"))
		if got.SkippedEdges != 1 || len(got.Nodes) != len(body.Nodes) || len(got.Edges) != len(body.Edges) {
			t.Errorf("dangling: skipped %d, %d nodes, %d edges", got.SkippedEdges, len(got.Nodes), len(got.Edges))
		}
	})
}

// danglingStore adds an outgoing calls edge from readFlow to a node that is
// not stored.
type danglingStore struct{ store.Store }

func (d danglingStore) Edges(ctx context.Context, repo string, ids, kinds []string, dir store.Direction) ([]contract.Edge, error) {
	edges, err := d.Store.Edges(ctx, repo, ids, kinds, dir)
	if err == nil && dir == store.Outgoing && contains(kinds, "calls") && contains(ids, flowReadFlow) {
		edges = append(edges, contract.Edge{Type: "edge", Kind: "calls", Source: flowReadFlow, Target: "function:server/gone.ts#ghost", Provenance: "fact", Confidence: 1})
	}
	return edges, err
}

// TestFlowCountsEqualEndpoint compares, for every endpoint of every test
// repository, the flow counts and its call and near-miss identities with
// /api/v0/api/endpoint for the same scope and root.
func TestFlowCountsEqualEndpoint(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := apiFixtureRoot(t)
		indexRoot(t, root, w)
		ingestFlowGraph(t, w)
		ingestFlowBig(t, w)
		ingestStatuses(t, w)
		options, _ := flowOptions(t, root, s)
		checked := 0
		for _, repo := range []string{"fixture", "flowgraph", "flowbig", "statuses"} {
			opt := options[repo]
			var ids []string
			for _, kind := range []string{"route", "ipc_command"} {
				nodes, err := s.Nodes(context.Background(), repo, store.Query{Kind: kind})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, identities(nodes)...)
			}
			for _, id := range ids {
				for _, extra := range []string{"", "&scope=all", "&scope=test", "&scope=all&root=web"} {
					var detail struct {
						Callers []struct {
							Node struct{ Identity string } `json:"node"`
						} `json:"callers"`
						NearMisses []struct {
							Node struct{ Identity string } `json:"node"`
						} `json:"near_misses"`
						NearMissesIncomplete int `json:"near_misses_incomplete"`
					}
					decodeInto(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoint?id="+url.QueryEscape(id)+extra, nil), &detail)
					flow := getFlow(t, opt, flowPath(id, extra))
					want := flowCounts{Callers: len(detail.Callers), NearMisses: len(detail.NearMisses), NearMissesIncomplete: detail.NearMissesIncomplete}
					if flow.Counts != want {
						t.Errorf("%s %s %s: flow counts %+v, endpoint %+v", repo, id, extra, flow.Counts, want)
					}
					var callers, misses, candidates []string
					for _, c := range detail.Callers {
						callers = append(callers, c.Node.Identity)
					}
					for _, m := range detail.NearMisses {
						misses = append(misses, m.Node.Identity)
					}
					for _, c := range flow.Candidates {
						candidates = append(candidates, c.Call)
					}
					sort.Strings(callers)
					sort.Strings(misses)
					sort.Strings(candidates)
					if flow.Truncated == nil && (!reflect.DeepEqual(flow.withRole(roleCall), callers) || !reflect.DeepEqual(flow.withRole(roleNearMiss), misses) || !reflect.DeepEqual(candidates, misses)) {
						t.Errorf("%s %s %s: flow calls %v / %v, endpoint %v / %v", repo, id, extra, flow.withRole(roleCall), flow.withRole(roleNearMiss), callers, misses)
					}
					checked++
				}
			}
		}
		if checked < 80 {
			t.Fatalf("only %d comparisons", checked)
		}
	})
}

// TestFlowCaps checks the fan-out, calls and node caps on the generated
// snapshot.
func TestFlowCaps(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ingestFlowBig(t, w)
		options, _ := flowOptions(t, t.TempDir(), s)
		opt := options["flowbig"]
		hf := "function:server/big.ts#hf"
		fan := getFlow(t, opt, flowPath(bigFan, ""))
		callees := fan.withRole(roleCallee)
		if len(callees) != 25 || callees[0] != "function:server/big.ts#f00" || callees[24] != "function:server/big.ts#f24" || fan.Truncated != nil {
			t.Errorf("fan-out: %d callees %v, truncated %+v", len(callees), callees, fan.Truncated)
		}
		for _, n := range fan.Nodes {
			if want := (flowOmitted{Callees: map[bool]int{true: 5}[n.Node.Identity == hf]}); n.Omitted != want {
				t.Errorf("%s: omitted %+v, want %+v", n.Node.Identity, n.Omitted, want)
			}
		}
		// 625 hop-2 callees exceed the node cap: hop 2 is dropped whole,
		// hop 1 stays complete. The drop also removes g00's caller role
		// (hop 2 gave it; it keeps callee from hop 1) and g01's hop-2
		// omitted callee (the cap cut g01.h25 at hop 2).
		g00, g01, wo := "function:server/big.ts#g00", "function:server/big.ts#g01", "function:web/big.ts#wo"
		wide := getFlow(t, opt, flowPath(bigWide, ""))
		// route, handler, 25 callees, the call and its owner wo; edges:
		// exposes, 25 calls, calls_route, contains and g00 -> wo.
		if len(wide.Nodes) != 29 || len(wide.withRole(roleCallee)) != 25 || len(wide.Edges) != 29 || !wide.hasEdge("calls", g00, wo) {
			t.Errorf("wide: %d nodes, %d edges", len(wide.Nodes), len(wide.Edges))
		}
		if wide.Truncated == nil || !reflect.DeepEqual(*wide.Truncated, flowTruncated{NodesOmitted: 625, EdgesOmitted: 625, Reasons: []string{truncHop2Dropped}}) {
			t.Errorf("wide truncated %+v", wide.Truncated)
		}
		if roles := wide.roles(); !reflect.DeepEqual(roles[g00], []string{roleCallee}) || !reflect.DeepEqual(roles[wo], []string{roleOwner}) || !reflect.DeepEqual(roles[g01], []string{roleCallee}) {
			t.Errorf("wide roles after the drop: g00 %v, g01 %v, wo %v", roles[g00], roles[g01], roles[wo])
		}
		for _, n := range wide.Nodes {
			if n.Hop == 2 || n.Omitted != (flowOmitted{}) {
				t.Errorf("wide: %s hop %d omitted %+v", n.Node.Identity, n.Hop, n.Omitted)
			}
		}
		// The handler's cap cut c25..c29, but c00 calls c25, so c25 is a
		// hop-2 callee and the stored hc -> c25 edge is induced: only 4
		// callees stay hidden.
		hc, c25 := "function:server/big.ts#hc", "function:server/big.ts#c25"
		cutBody := getFlow(t, opt, flowPath(bigCut, ""))
		if !cutBody.hasEdge("calls", hc, c25) || cutBody.hops()[c25] != 2 || len(cutBody.withRole(roleCallee)) != 26 || cutBody.Truncated != nil {
			t.Errorf("cut: %d callees, c25 hop %d, truncated %+v", len(cutBody.withRole(roleCallee)), cutBody.hops()[c25], cutBody.Truncated)
		}
		for _, n := range cutBody.Nodes {
			if want := (flowOmitted{Callees: map[bool]int{true: 4}[n.Node.Identity == hc]}); n.Omitted != want {
				t.Errorf("cut: %s omitted %+v, want %+v", n.Node.Identity, n.Omitted, want)
			}
		}
		checkStored(t, s, "flowbig", cutBody)
		// 201 callers and 201 near misses: the first 200 of each by site,
		// counts uncut. Hop 1 alone exceeds the node cap and stays, and
		// that is reported without adding to the omitted counts.
		many := getFlow(t, opt, flowPath(bigMany, ""))
		if many.Counts != (flowCounts{Callers: 201, NearMisses: 201}) || len(many.withRole(roleCall)) != 200 || len(many.withRole(roleNearMiss)) != 200 || len(many.Candidates) != 200 {
			t.Errorf("many: counts %+v, %d calls, %d near misses, %d candidates", many.Counts, len(many.withRole(roleCall)), len(many.withRole(roleNearMiss)), len(many.Candidates))
		}
		if many.Truncated == nil || !reflect.DeepEqual(*many.Truncated, flowTruncated{NodesOmitted: 2, EdgesOmitted: 1, Reasons: []string{truncCallsCap, truncHop1OverTotals, truncNearMissesCap}}) {
			t.Errorf("many truncated %+v", many.Truncated)
		}
		if len(many.Nodes) <= flowNodesCap {
			t.Errorf("many: %d nodes, want hop 1 over the node total", len(many.Nodes))
		}
		if contains(many.withRole(roleCall), "client_call:web/big.ts#many.GET%20200") || !contains(many.withRole(roleCall), "client_call:web/big.ts#many.GET%20199") {
			t.Error("calls cap did not keep the first 200 by site")
		}
		checkStored(t, s, "flowbig", many)
	})
}

func TestFlowParameters(t *testing.T) {
	opt := handlerOptions(t)
	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"/api/v0/api/flow", "bad_request", 400},
		{"/api/v0/api/flow?id=", "bad_request", 400},
		{"/api/v0/api/flow?id=a&id=b", "bad_request", 400},
		{flowPath("route:a.ts#GET /", ""), "bad_request", 400},
		{flowPath("function:a.ts#f", ""), "bad_request", 400},
		{flowPath("route:a.ts#GET%20%2F", "&scope=x"), "bad_request", 400},
		{flowPath("route:a.ts#GET%20%2F", "&root=a&root=b"), "bad_request", 400},
		{flowPath("route:a.ts#GET%20%2F", ""), "not_found", 404},
		{flowPath("ipc_command:a.rs#main.COMMAND%20x", "&unknown=1"), "not_found", 404},
	} {
		rec := get(t, opt, http.MethodGet, tc.path, nil)
		if rec.Code != tc.status || errorCode(t, rec) != tc.code {
			t.Errorf("%s: status %d %s, want %d %s", tc.path, rec.Code, rec.Body.String(), tc.status, tc.code)
		}
	}
}

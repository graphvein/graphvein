package server

import (
	"context"
	"net/http"
	"net/url"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds GET /api/v0/api/flow (ADR 0045, docs/cli/serve.md): the
// stored neighbourhood of one route or IPC command, one or two hops each
// way, for the Flow tab. Every edge it returns is a stored edge reported as
// stored; near misses, which have no edge, are a separate candidates list.
// Callers and near misses come from selectCalls, the code /endpoint uses,
// so the counts of both endpoints agree.

// Node roles, in the order the response sorts them (ADR 0045).
const (
	roleEndpoint    = "endpoint"
	roleHandler     = "handler"
	roleCall        = "call"
	roleOwner       = "owner"
	roleEnclosing   = "enclosing"
	roleOtherTarget = "other_target"
	roleNearMiss    = "near_miss"
	// roleNearMissOwner is the owner of a near miss, distinct from the
	// owner of a call so the UI can hide it with the near-miss toggle
	// without following edges; a node that owns both has both roles.
	roleNearMissOwner = "near_miss_owner"
	roleCallee        = "callee"
	roleCaller        = "caller"
	// roleNested is a function or method that the handler, a nested
	// function or a callee contains by a stored contains edge (chain row
	// CH7 downstream, T026 AG-C3): structure, not a call, at the hop of
	// its container.
	roleNested = "nested"
	// roleTable is a table that a handler-side function (handler, nested,
	// callee) reads or writes by a stored reads_table / writes_table edge
	// (chain row CH6), at the accessor's hop and at least 1.
	roleTable = "table"
)

var roleOrder = []string{roleEndpoint, roleHandler, roleCall, roleOwner, roleEnclosing, roleOtherTarget, roleNearMiss, roleNearMissOwner, roleCallee, roleCaller, roleNested, roleTable}

// Caps keep a response drawable; ADR 0045 records the measurements behind
// them. Order within a cap is file, span start, identity.
const (
	flowFanOutCap = 25  // calls neighbours per node and direction
	flowCallsCap  = 200 // callers, and separately near misses
	flowNodesCap  = 300 // total nodes before hop 2 is dropped
	flowEdgesCap  = 600 // total edges before hop 2 is dropped
)

// Truncation reasons.
const (
	truncCallsCap      = "calls_cap"
	truncNearMissesCap = "near_misses_cap"
	truncHop2Dropped   = "hop2_dropped"
	// truncHop1OverTotals reports that hop 1 alone exceeds the node or edge
	// total. Hop 1 is never cut, so it adds nothing to the omitted counts.
	truncHop1OverTotals = "hop1_over_totals"
)

type flowResponse struct {
	APIVersion   int             `json:"api_version"`
	Repository   string          `json:"repository"`
	Index        *lastRun        `json:"index"`
	Scope        string          `json:"scope"`
	Root         *string         `json:"root"`
	IDE          ideStatus       `json:"ide"`
	Transport    string          `json:"transport"`
	Endpoint     presentedNode   `json:"endpoint"`
	Handler      handlerInfo     `json:"handler"`
	Counts       flowCounts      `json:"counts"`
	Nodes        []flowNode      `json:"nodes"`
	Edges        []any           `json:"edges"`
	Candidates   []flowCandidate `json:"candidates"`
	SkippedEdges int             `json:"skipped_edges"`
	Truncated    *flowTruncated  `json:"truncated"`
	// Precision and CallersPrecision are /endpoint's scope labels for the
	// same endpoint. They label the route or command and call facts only;
	// a structural node keeps its file's label (/api/v0/source).
	Precision        scopeLabel `json:"precision"`
	CallersPrecision scopeLabel `json:"callers_precision"`
	// CodeCallsPrecision labels the code `calls` edges of the handler's
	// call chain (familyCodeCalls), which no file label covers.
	CodeCallsPrecision scopeLabel `json:"code_calls_precision"`
	// MayAlsoUse lists the table_access_unknown sites of the returned
	// handler-side functions (T026 AG-C3): sites the indexer recognised
	// but could not bind to a table, so a lower bound stays visible as one.
	MayAlsoUse []flowMayAlsoUse `json:"may_also_use"`
	// TablesBeyond counts the tables first reached one hop past hop 2
	// (hop 3), which are not returned; null when hop 2 was dropped or no
	// handler node is stored.
	TablesBeyond *flowBeyond `json:"tables_beyond"`
	// Project (serve --project only, HTTP routes) lists the
	// cross-repository links into the route, as /api/v0/api/endpoint does:
	// they are project records, not stored edges of this repository, so
	// they are not among Nodes and Edges (T022 B9).
	Project *projectEndpointDoc `json:"project,omitempty"`
}

// flowCounts are the figures of /api/v0/api/endpoint for the same request:
// callers and near misses before any cap.
type flowCounts struct {
	Callers              int `json:"callers"`
	NearMisses           int `json:"near_misses"`
	NearMissesIncomplete int `json:"near_misses_incomplete"`
}

// flowNode is one included node with every role it has and its smallest
// hop. Omitted counts the calls neighbours the fan-out cap left out that
// no returned edge reaches (one cut neighbour can be reached another way,
// and then its edge is induced and it is not hidden).
type flowNode struct {
	Node    presentedNode `json:"node"`
	Roles   []string      `json:"roles"`
	Hop     int           `json:"hop"`
	Omitted flowOmitted   `json:"omitted"`
}

type flowOmitted struct {
	Callers int `json:"callers"`
	Callees int `json:"callees"`
}

// flowEdge is a stored edge, verbatim.
type flowEdge struct {
	Kind       string              `json:"kind"`
	Source     string              `json:"source"`
	Target     string              `json:"target"`
	Provenance string              `json:"provenance"`
	Confidence float64             `json:"confidence"`
	Evidence   []contract.Evidence `json:"evidence"`
}

// flowRouteEdge is a stored calls_route edge with the match tier of the
// call's linked path entry for its target (null when no entry says so).
type flowRouteEdge struct {
	flowEdge
	Match *string `json:"match"`
	// ShapeDifferences (additive; T029 W4) compares what the call sends
	// with what the route reads; absent when nothing is compared.
	ShapeDifferences *shapeDifferencesDoc `json:"shape_differences,omitempty"`
	// Assumptions are the assumptions the link rests on (an origin
	// nothing proves, UP-V1, UP-V2), read from the call; absent when
	// there is none.
	Assumptions []clients.LinkAssumption `json:"assumptions,omitempty"`
}

// flowCandidate is a near miss: a recorded candidate, not an edge.
type flowCandidate struct {
	Call                string `json:"call"`
	Endpoint            string `json:"endpoint"`
	Relation            string `json:"relation"`
	CandidateCount      int    `json:"candidate_count"`
	CandidateCountExact bool   `json:"candidate_count_exact"`
}

type flowTruncated struct {
	NodesOmitted int      `json:"nodes_omitted"`
	EdgesOmitted int      `json:"edges_omitted"`
	Reasons      []string `json:"reasons"`
}

func computeFlow(h *apiHandler, q url.Values) (readFunc, error) {
	id, t, scope, root, err := endpointParams(q)
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		endpoint, ok := found[id]
		if !ok {
			return nil, &apiError{http.StatusNotFound, "not_found", "no API endpoint with identity " + id}
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		p := h.newPresenter(hashes, editor)
		out := flowResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), Scope: scope, Root: root, IDE: p.ide,
			Transport: t.Name, Endpoint: p.node(endpoint), Candidates: []flowCandidate{},
		}
		out.Precision, out.CallersPrecision = endpointLabels(runs, t, []contract.Node{endpoint})
		run, _ := precisionRun(runs)
		out.CodeCallsPrecision = codeCallsLabel(run)
		handler, handlerNode, exposes, err := handlerWithEdge(rd, p, t, endpoint)
		if err != nil {
			return nil, err
		}
		out.Handler = handler
		sel, err := selectCalls(rd, p.roots, t, id, scope, root)
		if err != nil {
			return nil, err
		}
		out.Counts = flowCounts{Callers: len(sel.callers), NearMisses: len(sel.misses), NearMissesIncomplete: sel.incomplete}
		b := &flowBuilder{rd: rd, t: t, endpoint: id, nodes: map[string]*flowEntry{}, edges: map[edgeKey]contract.Edge{}, skipped: map[edgeKey]bool{}}
		trunc := &flowTruncated{Reasons: []string{}}
		callers, misses := sel.callers, sel.misses
		if len(callers) > flowCallsCap {
			for _, c := range callers[flowCallsCap:] {
				trunc.NodesOmitted++
				trunc.EdgesOmitted += len(sel.edgesFrom[c.Identity])
			}
			callers = callers[:flowCallsCap]
			trunc.Reasons = append(trunc.Reasons, truncCallsCap)
		}
		if len(misses) > flowCallsCap {
			trunc.NodesOmitted += len(misses) - flowCallsCap
			misses = misses[:flowCallsCap]
			trunc.Reasons = append(trunc.Reasons, truncNearMissesCap)
		}
		if err := b.build(endpoint, handlerNode, exposes, callers, misses); err != nil {
			return nil, err
		}
		for _, m := range misses {
			r := sel.missOf[m.Identity]
			out.Candidates = append(out.Candidates, flowCandidate{Call: m.Identity, Endpoint: id, Relation: r.relation, CandidateCount: r.count, CandidateCountExact: r.exact})
		}
		hop2Dropped := false
		if b.overTotals() {
			if nodes, edges := b.dropHop2(); nodes > 0 {
				hop2Dropped = true
				trunc.NodesOmitted += nodes
				trunc.EdgesOmitted += edges
				trunc.Reasons = append(trunc.Reasons, truncHop2Dropped)
			}
		}
		// Hop 1 is never cut, so it may exceed the totals alone; that is
		// reported, and the returned nodes and edges show by how much.
		if b.overTotals() {
			trunc.Reasons = append(trunc.Reasons, truncHop1OverTotals)
		}
		if len(trunc.Reasons) > 0 {
			sort.Strings(trunc.Reasons)
			out.Truncated = trunc
		}
		if out.MayAlsoUse, err = b.mayAlsoUse(); err != nil {
			return nil, err
		}
		if handlerNode != nil && !hop2Dropped {
			if out.TablesBeyond, err = b.beyond(); err != nil {
				return nil, err
			}
		}
		out.SkippedEdges = len(b.skipped)
		out.Nodes = b.presentNodes(p)
		out.Edges = b.presentEdges(p)
		if h.opt.project != nil && t.Name == httpTransport.Name {
			if out.Project, err = h.opt.project.projectEndpoint(ctx, h.opt.Repository, endpoint, p, editor); err != nil {
				return nil, err
			}
		}
		return out, nil
	}, nil
}

// edgeKey is the stored edge key (kind, source, target).
type edgeKey struct{ kind, source, target string }

func keyOf(e contract.Edge) edgeKey { return edgeKey{e.Kind, e.Source, e.Target} }

// flowEntry is a node being collected. roles maps each role to the hop it
// was reached at, so dropping hop 2 also drops roles only hop 2 gave.
type flowEntry struct {
	node  contract.Node
	roles map[string]int
	hop   int
	// cut holds, by the hop of the expansion that cut them, the stored
	// calls edges the fan-out cap left out. Omitted counts are computed
	// from them only when presenting, after induction and any hop-2 drop.
	cut [3]flowCut
}

// flowCut is the stored calls edges one expansion cut, by direction.
type flowCut struct {
	callers, callees []edgeKey
}

// flowBuilder collects the nodes and stored edges of one flow response.
type flowBuilder struct {
	rd       *reader
	t        transport
	endpoint string
	nodes    map[string]*flowEntry
	edges    map[edgeKey]contract.Edge
	// skipped holds stored edges whose other end is not a stored node.
	skipped map[edgeKey]bool
	// frontier holds the hop-2 handler-side functions (callees and their
	// nested functions), whose callees are hop 3 (tables_beyond).
	frontier []string
}

func (b *flowBuilder) add(n contract.Node, role string, hop int) {
	e := b.nodes[n.Identity]
	if e == nil {
		e = &flowEntry{node: n, roles: map[string]int{}, hop: hop}
		b.nodes[n.Identity] = e
	}
	if cur, ok := e.roles[role]; !ok || hop < cur {
		e.roles[role] = hop
	}
	if hop < e.hop {
		e.hop = hop
	}
}

func (b *flowBuilder) addEdge(e contract.Edge) { b.edges[keyOf(e)] = e }

// build collects every role of D2 (ADR 0045). The hop-1 nodes are all
// added before any hop-2 node, so a node whose hop is 2 was reached only
// by a hop-2 step.
func (b *flowBuilder) build(endpoint contract.Node, handler *contract.Node, exposes *contract.Edge, callers, misses []contract.Node) error {
	b.add(endpoint, roleEndpoint, 0)
	if handler != nil {
		b.add(*handler, roleHandler, 0)
	}
	if exposes != nil {
		if handler != nil {
			b.addEdge(*exposes)
		} else {
			b.skipped[keyOf(*exposes)] = true
		}
	}
	if err := b.addCalls(callers); err != nil {
		return err
	}
	for _, m := range misses {
		b.add(m, roleNearMiss, 1)
	}
	owners, err := b.addContainers(identities(callers), roleOwner, nil)
	if err != nil {
		return err
	}
	if _, err := b.addContainers(identities(misses), roleNearMissOwner, nil); err != nil {
		return err
	}
	// Only the owners of callers lead further: a near miss is not a caller.
	callerOwners := make([]string, 0, len(owners))
	for _, owner := range owners {
		callerOwners = append(callerOwners, owner)
	}
	callerOwners = distinct(callerOwners)
	enclosing, err := b.addEnclosing(callerOwners)
	if err != nil {
		return err
	}
	// Downstream hop 0 is the handler with its nested functions (CH7);
	// hop 1 their callees with the callees' nested functions.
	var down0, down1 []string
	if handler != nil {
		nested0, err := b.addNested([]string{handler.Identity}, 0)
		if err != nil {
			return err
		}
		down0 = distinct(append([]string{handler.Identity}, nested0...))
		callees1, err := b.expand(down0, store.Outgoing, roleCallee, 1)
		if err != nil {
			return err
		}
		nested1, err := b.addNested(callees1, 1)
		if err != nil {
			return err
		}
		down1 = distinct(append(callees1, nested1...))
	}
	// Hop 2: callers of the owners and of their enclosing functions, and
	// the callees of the hop-1 functions (each function is expanded once).
	if _, err := b.expand(distinct(append(callerOwners, enclosing...)), store.Incoming, roleCaller, 2); err != nil {
		return err
	}
	expanded := setOf(down0...)
	var next []string
	for _, id := range down1 {
		if !expanded[id] {
			next = append(next, id)
			expanded[id] = true
		}
	}
	callees2, err := b.expand(next, store.Outgoing, roleCallee, 2)
	if err != nil {
		return err
	}
	nested2, err := b.addNested(callees2, 2)
	if err != nil {
		return err
	}
	for _, id := range distinct(append(callees2, nested2...)) {
		if !expanded[id] {
			b.frontier = append(b.frontier, id)
		}
	}
	if err := b.induceCalls(); err != nil {
		return err
	}
	return b.addTables()
}

// addCalls adds the callers with every stored call edge they have: the edge
// to the endpoint and, for a conditional call, its other targets.
func (b *flowBuilder) addCalls(callers []contract.Node) error {
	for _, c := range callers {
		b.add(c, roleCall, 1)
	}
	edges, err := b.rd.edges(identities(callers), b.t.CallEdge, store.Outgoing)
	if err != nil {
		return err
	}
	var others []string
	for _, e := range edges {
		if e.Target != b.endpoint {
			others = append(others, e.Target)
		}
	}
	targets, err := b.rd.byIdentity(others)
	if err != nil {
		return err
	}
	for _, e := range edges {
		switch n, ok := targets[e.Target]; {
		case e.Target == b.endpoint:
			b.addEdge(e)
		case ok:
			b.add(n, roleOtherTarget, 1)
			b.addEdge(e)
		default:
			b.skipped[keyOf(e)] = true
		}
	}
	return nil
}

// addContainers adds, for each identity, the source of its stored contains
// edge (the rule of /endpoint's owner) with role at hop 1, and that edge.
// accept, when non-nil, decides which container kinds are used. It returns
// the identity of the added container by contained identity.
func (b *flowBuilder) addContainers(ids []string, role string, accept func(contract.Node) bool) (map[string]string, error) {
	first, err := b.rd.containers(ids)
	if err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(first))
	for _, e := range first {
		sources = append(sources, e.Source)
	}
	nodes, err := b.rd.byIdentity(sources)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for target, e := range first {
		n, ok := nodes[e.Source]
		if !ok {
			b.skipped[keyOf(e)] = true
			continue
		}
		if accept != nil && !accept(n) {
			continue
		}
		b.add(n, role, 1)
		b.addEdge(e)
		out[target] = n.Identity
	}
	return out, nil
}

// addEnclosing is the one structural step of UQ1 B (ADR 0045): for the
// owner of a call, the function or method that contains the owner, by its
// stored contains edge, one step and never further. It is reported as role
// enclosing with that contains edge; its callers (hop 2) are callers of the
// enclosing function, never callers of the endpoint, and no count includes
// them.
func (b *flowBuilder) addEnclosing(owners []string) ([]string, error) {
	byOwner, err := b.addContainers(owners, roleEnclosing, func(n contract.Node) bool {
		return n.Kind == "function" || n.Kind == "method"
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(byOwner))
	for _, id := range byOwner {
		out = append(out, id)
	}
	return distinct(out), nil
}

// expand adds, for each node of ids, its stored calls neighbours in dir with
// role at hop, at most flowFanOutCap per node by site order; the rest are
// recorded in the node's cut. It returns the added neighbours.
func (b *flowBuilder) expand(ids []string, dir store.Direction, role string, hop int) ([]string, error) {
	edges, err := b.rd.edges(ids, "calls", dir)
	if err != nil {
		return nil, err
	}
	expanded := setOf(ids...)
	from := func(e contract.Edge) (self, other string) {
		if dir == store.Incoming {
			return e.Target, e.Source
		}
		return e.Source, e.Target
	}
	var others []string
	for _, e := range edges {
		if self, other := from(e); expanded[self] {
			others = append(others, other)
		}
	}
	nodes, err := b.rd.byIdentity(others)
	if err != nil {
		return nil, err
	}
	bySelf := map[string][]contract.Edge{}
	for _, e := range edges {
		self, other := from(e)
		if !expanded[self] {
			continue
		}
		if _, ok := nodes[other]; !ok {
			b.skipped[keyOf(e)] = true
			continue
		}
		bySelf[self] = append(bySelf[self], e)
	}
	var added []string
	for _, self := range distinct(ids) {
		list := bySelf[self]
		sort.Slice(list, func(i, j int) bool {
			_, a := from(list[i])
			_, c := from(list[j])
			return lessBySite(nodes[a], nodes[c])
		})
		if len(list) > flowFanOutCap {
			cut := &b.nodes[self].cut[hop]
			for _, e := range list[flowFanOutCap:] {
				if dir == store.Incoming {
					cut.callers = append(cut.callers, keyOf(e))
				} else {
					cut.callees = append(cut.callees, keyOf(e))
				}
			}
			list = list[:flowFanOutCap]
		}
		for _, e := range list {
			_, other := from(e)
			b.add(nodes[other], role, hop)
			b.addEdge(e)
			added = append(added, other)
		}
	}
	return distinct(added), nil
}

// induceCalls adds every stored calls edge between two included nodes, so
// a node reached twice shows both edges and recursion shows a self-loop.
func (b *flowBuilder) induceCalls() error {
	ids := make([]string, 0, len(b.nodes))
	for id := range b.nodes {
		ids = append(ids, id)
	}
	edges, err := b.rd.edges(distinct(ids), "calls", store.Outgoing)
	if err != nil {
		return err
	}
	for _, e := range edges {
		if b.nodes[e.Source] != nil && b.nodes[e.Target] != nil {
			b.addEdge(e)
		}
	}
	return nil
}

// dropHop2 removes every hop-2 node, the edges touching one, the roles
// only hop 2 gave and the cuts of hop-2 expansions; hop 1 stays
// complete. It returns the numbers removed.
func (b *flowBuilder) dropHop2() (nodes, edges int) {
	for id, e := range b.nodes {
		if e.hop == 2 {
			delete(b.nodes, id)
			nodes++
			continue
		}
		for role, hop := range e.roles {
			if hop == 2 {
				delete(e.roles, role)
			}
		}
		e.cut[2] = flowCut{}
	}
	for k := range b.edges {
		if b.nodes[k.source] == nil || b.nodes[k.target] == nil {
			delete(b.edges, k)
			edges++
		}
	}
	return nodes, edges
}

// overTotals reports whether the collected graph exceeds the node or edge
// total.
func (b *flowBuilder) overTotals() bool {
	return len(b.nodes) > flowNodesCap || len(b.edges) > flowEdgesCap
}

// omitted counts, per direction, the neighbours e's expansions cut that no
// returned edge reaches. A neighbour the cap cut may still be returned
// through another path, and then induceCalls returns its edge too.
func (b *flowBuilder) omitted(e *flowEntry) flowOmitted {
	hidden := func(keys []edgeKey, seen map[edgeKey]bool) {
		for _, k := range keys {
			if _, ok := b.edges[k]; !ok {
				seen[k] = true
			}
		}
	}
	callers, callees := map[edgeKey]bool{}, map[edgeKey]bool{}
	for _, c := range e.cut {
		hidden(c.callers, callers)
		hidden(c.callees, callees)
	}
	return flowOmitted{Callers: len(callers), Callees: len(callees)}
}

func roleRank(role string) int {
	for i, r := range roleOrder {
		if r == role {
			return i
		}
	}
	return len(roleOrder)
}

// presentNodes orders nodes by hop, first role, site, identity. Call and
// near-miss nodes are presented with their source root.
func (b *flowBuilder) presentNodes(p *presenter) []flowNode {
	entries := make([]*flowEntry, 0, len(b.nodes))
	for _, e := range b.nodes {
		entries = append(entries, e)
	}
	firstRank := func(e *flowEntry) int {
		best := len(roleOrder)
		for role := range e.roles {
			best = min(best, roleRank(role))
		}
		return best
	}
	sort.Slice(entries, func(i, j int) bool {
		a, c := entries[i], entries[j]
		if a.hop != c.hop {
			return a.hop < c.hop
		}
		if ra, rc := firstRank(a), firstRank(c); ra != rc {
			return ra < rc
		}
		return lessBySite(a.node, c.node)
	})
	out := make([]flowNode, 0, len(entries))
	for _, e := range entries {
		roles := make([]string, 0, len(e.roles))
		for role := range e.roles {
			roles = append(roles, role)
		}
		sort.Slice(roles, func(i, j int) bool { return roleRank(roles[i]) < roleRank(roles[j]) })
		presented := p.node(e.node)
		if hasRole(e, roleCall) || hasRole(e, roleNearMiss) {
			presented = p.call(e.node)
		}
		out = append(out, flowNode{Node: presented, Roles: roles, Hop: e.hop, Omitted: b.omitted(e)})
	}
	return out
}

func hasRole(e *flowEntry, role string) bool {
	_, ok := e.roles[role]
	return ok
}

// presentEdges orders edges by kind, source, target; http call edges carry
// the match of the calling node's linked path entry.
func (b *flowBuilder) presentEdges(p *presenter) []any {
	keys := make([]edgeKey, 0, len(b.edges))
	for k := range b.edges {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, c := keys[i], keys[j]
		if a.kind != c.kind {
			return a.kind < c.kind
		}
		if a.source != c.source {
			return a.source < c.source
		}
		return a.target < c.target
	})
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		e := b.edges[k]
		fe := flowEdge{Kind: e.Kind, Source: e.Source, Target: e.Target, Provenance: e.Provenance, Confidence: e.Confidence, Evidence: e.Evidence}
		if fe.Evidence == nil {
			fe.Evidence = []contract.Evidence{}
		}
		if b.t.Name == httpTransport.Name && e.Kind == b.t.CallEdge {
			re := flowRouteEdge{flowEdge: fe, Match: linkedMatch(b.nodes[e.Source].node, e.Target), Assumptions: linkAssumptions(b.nodes[e.Source].node, e.Target)}
			if target, ok := b.nodes[e.Target]; ok {
				re.ShapeDifferences = shapeDifferences(target.node, b.nodes[e.Source].node, presenterLinker(p), presenterLinker(p), false)
			}
			out = append(out, re)
			continue
		}
		if e.Kind == "reads_table" || e.Kind == "writes_table" {
			out = append(out, flowTableEdge{flowEdge: fe, Wording: chainAccessWording(b.nodes[e.Source].node, e, false)})
			continue
		}
		out = append(out, fe)
	}
	return out
}

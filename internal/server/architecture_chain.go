package server

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/testscope"
	"github.com/graphvein/graphvein/internal/traverse"
)

// This file holds the chain of the architecture view (T026, ADR
// 0079 option 4A): GET /api/v0/architecture/chain (docs/cli/serve.md). A
// chain starts at any node (component, file, route, handler, function or
// method, client or IPC call, IPC command, table) and follows the stored
// edges of the CH rows in docs/contract/claims/architecture-graph.md up, down
// or both, with all branches, on the breadth-first walk change impact uses
// (internal/traverse: same depth, member and fan-out caps, reported per
// step when they bind). The code tables chainRows, chainDown and chainUp
// list the rows in table order; TestChainContract keeps them 1:1.

// chainRows are the rows of the chain contract, in table order.
var chainRows = []archRow{
	{ID: "CH1"}, {ID: "CH2"}, {ID: "CH3"}, {ID: "CH4"}, {ID: "CH5"}, {ID: "CH6"},
	{ID: "CH7"}, {ID: "CH8"}, {ID: "CH9"}, {ID: "CH10"}, {ID: "CH11"}, {ID: "CH12"}, {ID: "CH13"}, {ID: "CHD"},
}

// Kinds a chain rule follows from or to.
var (
	chainFunctions = []string{"function", "method"}
	chainCalls     = []string{"client_call", "ipc_call"}
)

// chainBridges are the edge kinds of row CH8 (ADR 0056 I9's Angular
// relations the task names, React renders and react-router routes_to, and
// passes_function of T026), followed as dashed bridges.
var chainBridges = []string{"renders", "injects", "routes_to", "loads_children", "passes_function"}

func chainRule(id string, from []string, edge string, dir store.Direction, to []string) traverse.Rule {
	return traverse.Rule{ID: id, From: from, Edge: edge, Dir: dir, To: to}
}

// chainDown is the relation table of a downstream chain (towards callees,
// routes, handlers and tables), rows CH1..CH8.
var chainDown = append([]traverse.Rule{
	chainRule("CH1", chainFunctions, "calls", store.Outgoing, chainFunctions),
	chainRule("CH2", chainFunctions, "contains", store.Outgoing, chainCalls),
	chainRule("CH3", []string{"client_call"}, "calls_route", store.Outgoing, []string{"route"}),
	chainRule("CH4", []string{"route"}, "exposes_route", store.Incoming, chainFunctions),
	chainRule("CH5", []string{"ipc_call"}, "invokes_command", store.Outgoing, []string{"ipc_command"}),
	chainRule("CH5", []string{"ipc_command"}, "exposes_command", store.Incoming, chainFunctions),
	chainRule("CH6", nil, "reads_table", store.Outgoing, []string{"table"}),
	chainRule("CH6", nil, "writes_table", store.Outgoing, []string{"table"}),
	chainRule("CH7", chainFunctions, "contains", store.Outgoing, chainFunctions),
}, bridgeRules(store.Outgoing)...)

// chainUp is the relation table of an upstream chain (towards callers,
// client calls and UI code), rows CH1..CH8. CH7 upstream applies only to
// an inner function without a stored caller (chainWalk.candidates).
var chainUp = append([]traverse.Rule{
	chainRule("CH1", chainFunctions, "calls", store.Incoming, chainFunctions),
	chainRule("CH2", chainCalls, "contains", store.Incoming, chainFunctions),
	chainRule("CH3", []string{"route"}, "calls_route", store.Incoming, []string{"client_call"}),
	chainRule("CH4", chainFunctions, "exposes_route", store.Outgoing, []string{"route"}),
	chainRule("CH5", []string{"ipc_command"}, "invokes_command", store.Incoming, []string{"ipc_call"}),
	chainRule("CH5", chainFunctions, "exposes_command", store.Outgoing, []string{"ipc_command"}),
	chainRule("CH6", []string{"table"}, "reads_table", store.Incoming, nil),
	chainRule("CH6", []string{"table"}, "writes_table", store.Incoming, nil),
	chainRule("CH7", chainFunctions, "contains", store.Incoming, chainFunctions),
}, bridgeRules(store.Incoming)...)

func bridgeRules(dir store.Direction) []traverse.Rule {
	var out []traverse.Rule
	for _, kind := range chainBridges {
		out = append(out, chainRule("CH8", nil, kind, dir, nil))
	}
	return out
}

// Chain directions.
const (
	chainDirUp   = "up"
	chainDirDown = "down"
	chainDirBoth = "both"
)

// chainNotModelled is row CH11's end line (O4 (a)).
const chainNotModelled = "? not modelled: no stored caller and no resolved bridge; JSX renders and passed callbacks are followed only where the tag or value resolves to this function, other dependency injection and dynamic calls are not modelled"

// The map budget (AG16): a folding hint for the map, never a cut of the
// lists. At most mapSteps steps each way, mapPerStep members per step and
// mapTotal members in total are drawn; the rest folds into counts.
const (
	mapSteps   = 3
	mapPerStep = 5
	mapTotal   = 40
)

// chainWording is a followed edge's wording by row, edge kind and
// direction; CH6's depends on the edge (chainAccessWording).
var chainWording = map[string]map[string][2]string{
	// [down, up]
	"CH1": {"calls": {"calls", "called by"}},
	"CH2": {"contains": {"makes request", "made in"}},
	"CH3": {"calls_route": {"calls route", "called by client call"}},
	"CH4": {"exposes_route": {"handled by", "handles route"}},
	"CH5": {"invokes_command": {"invokes command", "invoked by IPC call"}, "exposes_command": {"handled by", "handles command"}},
	"CH7": {"contains": {"contains, not a call", "enclosed by, not a call"}},
	"CH8": {"renders": {"renders", "rendered by"}, "injects": {"injects", "injected by"}, "routes_to": {"routes to", "routed to by"}, "loads_children": {"loads children", "loaded by"}, "passes_function": {"passed as callback, call not modelled", "passed as callback by, call not modelled"}},
}

// chainMember is one member of a chain step.
type chainMember struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	QualifiedName string  `json:"qualified_name"`
	File          *string `json:"file"`
	Line          *int    `json:"line"`
	Component     *string `json:"component"`
	Method        *string `json:"method,omitempty"`
	Path          *string `json:"path,omitempty"`
	// FullPath is a route's composed path as served (its stored paths),
	// absent when the route has none (unmounted).
	FullPath *string `json:"full_path,omitempty"`
	// PrefixPath is a route's path whose prefix the code does not fix
	// ("…" marks it; T033 row UP-33), set only when FullPath is absent.
	PrefixPath *string `json:"prefix_path,omitempty"`
	Command    *string `json:"command,omitempty"`
	Status     *string `json:"status,omitempty"`
	// Database is the database card ("database:<set>") a table member
	// belongs to, so the map draws it inside that card; absent when the
	// table stores no set.
	Database *string `json:"database,omitempty"`
	// Repository names the other project member of a CH13 member (serve
	// --project): a node of that repository, drawn in its muted card.
	Repository *string `json:"repository,omitempty"`
	// Row is the CH row of the edge that first reached the member.
	Row string `json:"row"`
	// Tier is the weakest edge on the member's first path from the start;
	// TierStep is the step of that edge (0: every edge is a fact).
	Tier     string `json:"tier"`
	TierStep int    `json:"tier_step"`
	// Parents are the members of the previous step with an edge to this
	// one (all branches).
	Parents []string `json:"parents"`

	step       int
	confidence float64
}

// chainEdge is one followed edge between members, in chain order: From is
// the member nearer the start.
type chainEdge struct {
	From     string              `json:"from"`
	To       string              `json:"to"`
	Kind     string              `json:"kind"`
	Row      string              `json:"row"`
	Wording  string              `json:"wording"`
	Dashed   bool                `json:"dashed"`
	Tier     string              `json:"tier"`
	Count    int                 `json:"count,omitempty"`
	Evidence []contract.Evidence `json:"evidence"`
	// Access is a CH6 edge's table_access entry on its owner (tool, role,
	// operations, rows, verification, assumes), as change impact's I8
	// step carries it (T030 D7, G9); absent on every other row.
	Access *changes.TableAccess `json:"access,omitempty"`
	// Assumptions are the assumptions a CH3 (calls_route) edge's link
	// rests on, read from its client call: an origin nothing proves to
	// be the server (LF-1) and the unknown-prefix ones (UP-V1, UP-V2);
	// absent when there is none and on every other row.
	Assumptions []clients.LinkAssumption `json:"assumptions,omitempty"`
	stepOfFrom  int
}

// chainEnd is a row CH11 end line under a member.
type chainEnd struct {
	Member string `json:"member"`
	Row    string `json:"row"`
	Text   string `json:"text"`
}

// chainBound is one cap that bound (CH12), with its wording.
type chainBound struct {
	Cap    string `json:"cap"`
	Step   int    `json:"step"`
	Beyond int    `json:"beyond"`
	Text   string `json:"text"`
}

// chainStepMap is the map budget's hint for one step: the members drawn
// and how many fold into the step's count.
type chainStepMap struct {
	Shown  []string `json:"shown"`
	Folded int      `json:"folded"`
}

// chainStep is one step of one direction; step 0 holds the start.
type chainStep struct {
	Step    int           `json:"step"`
	Members []chainMember `json:"members"`
	// HiddenTest counts the members test scope kept out (CH10).
	HiddenTest int          `json:"hidden_test"`
	Bounds     []chainBound `json:"bounds"`
	Map        chainStepMap `json:"map"`
}

// chainSide is the chain in one direction.
type chainSide struct {
	Steps       []chainStep    `json:"steps"`
	Edges       []chainEdge    `json:"edges"`
	Ends        []chainEnd     `json:"ends"`
	NotFollowed map[string]int `json:"not_followed"`
	Caps        []string       `json:"caps"`
	Members     int            `json:"members"`
	HiddenTest  int            `json:"hidden_test"`
}

// chainMap is the map budget summary of the whole chain.
type chainMap struct {
	Steps      int  `json:"steps"`
	PerStep    int  `json:"per_step"`
	Total      int  `json:"total"`
	Shown      int  `json:"shown"`
	Folded     int  `json:"folded"`
	OpenInFlow bool `json:"open_in_flow"`
}

// chainCaps are the traversal caps of every chain (CH12).
type chainCaps struct {
	Depth   int `json:"depth"`
	Members int `json:"members"`
	FanOut  int `json:"fan_out"`
}

type chainStart struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Level string `json:"level"`
	Row   string `json:"row,omitempty"`
}

type chainResponse struct {
	APIVersion int        `json:"api_version"`
	Repository string     `json:"repository"`
	Index      *lastRun   `json:"index"`
	Start      chainStart `json:"start"`
	Direction  string     `json:"direction"`
	Scope      string     `json:"scope"`
	Depth      int        `json:"depth"`
	Caps       chainCaps  `json:"caps"`
	Up         *chainSide `json:"up"`
	Down       *chainSide `json:"down"`
	Map        chainMap   `json:"map"`
}

func computeArchitectureChain(h *apiHandler, q url.Values) (readFunc, error) {
	id, present, err := param(q, "id")
	if err != nil {
		return nil, err
	}
	if !present || id == "" {
		return nil, badRequest("parameter id is required: a node identity or a component directory")
	}
	direction, present, err := param(q, "direction")
	if err != nil {
		return nil, err
	}
	if !present {
		direction = chainDirBoth
	}
	if direction != chainDirUp && direction != chainDirDown && direction != chainDirBoth {
		return nil, badRequest("direction must be up, down or both, not %q", direction)
	}
	depth, err := intParam(q, "depth", 1, traverse.MaxDepth)
	if err != nil {
		return nil, err
	}
	if depth == 0 {
		depth = traverse.ChainDefaultDepth
	}
	scope, err := archScopeParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		cfg, err := h.opt.loadConfig()
		if err != nil {
			return nil, badRequest("reading graphvein.yaml: %v", err)
		}
		m, err := buildComponents(rd, cfg)
		if err != nil {
			return nil, err
		}
		out := chainResponse{Repository: h.opt.Repository, Index: indexOf(runs), Direction: direction, Scope: scope, Depth: depth,
			Caps: chainCaps{Depth: traverse.MaxDepth, Members: traverse.MaxReached, FanOut: traverse.MaxFanOut},
			Map:  chainMap{Steps: mapSteps, PerStep: mapPerStep, Total: mapTotal}}
		dirs := []string{chainDirUp, chainDirDown}
		if direction != chainDirBoth {
			dirs = []string{direction}
		}
		px, err := h.opt.project.archProject(ctx, h.opt.Repository)
		if err != nil {
			return nil, err
		}
		var sides map[string]*chainSide
		if _, _, _, perr := contract.ParseIdentity(id); perr != nil {
			c, ok := m.components[id]
			if !ok || c.Counts.Files == 0 {
				return nil, &apiError{404, "not_found", "no node or component " + id}
			}
			out.Start = chainStart{ID: id, Kind: "component", Level: "component", Row: "CH9"}
			sides, err = componentChain(rd, m, id, dirs, depth, scope, px)
		} else {
			// err stays the outer variable: the walk's error below must
			// reach the check after this branch, not a shadowed copy.
			var start map[string]contract.Node
			start, err = rd.byIdentity([]string{id})
			if err != nil {
				return nil, err
			}
			n, ok := start[id]
			if !ok {
				return nil, &apiError{404, "not_found", "no node " + id}
			}
			out.Start = chainStart{ID: id, Kind: n.Kind, Level: "node"}
			seeds := []contract.Node{n}
			if n.Kind == "file" {
				// CH9: a file's chain is the union of its nodes' chains.
				tagRow("CH9/file")
				out.Start.Level, out.Start.Row = "file", "CH9"
				seeds, err = rd.s.Nodes(rd.ctx, rd.repo, store.Query{File: n.File})
				if err != nil {
					return nil, fmt.Errorf("reading nodes of %s: %w", n.File, err)
				}
			}
			sides, err = nodeChain(rd, m, seeds, dirs, depth, scope)
		}
		if err != nil {
			return nil, err
		}
		if out.Start.Level != "component" {
			for dir, side := range sides {
				projectChainSteps(side, dir, depth, scope, px)
			}
		}
		out.Up, out.Down = sides[chainDirUp], sides[chainDirDown]
		applyMapBudget(&out)
		return out, nil
	}, nil
}

// chainWalk is the walk of one direction from the start.
type chainWalk struct {
	rd    *reader
	m     *archModel
	scope string
	dir   string
	rules []traverse.Rule
	// opposite is the other direction's table.
	opposite []traverse.Rule
	walk     *traverse.Walk
	nodes    map[string]contract.Node
	member   map[string]*chainMember
	order    []string
	edges    map[[3]string]*chainEdge
	// hidden are the test-scope nodes kept out, by first step (CH10).
	hidden      map[string]int
	notFollowed map[string]int
	ends        map[string]bool
	// incomingKinds / outgoingKinds are the edge kinds some rule of this
	// direction follows that way: CHD counts the other edges of the
	// direction's own sense, as change impact counts its untraversed edges.
	followsIn, followsOut map[string]bool
	// injected maps a function to the routes whose security_dependencies
	// chains name it; declared maps a route to its chains' wording (CH11
	// named dependency-injection steps from AA3's facts).
	injected map[string][]string
	declared map[string][]string
	// leaves are the downstream functions with nothing to follow;
	// unresolved maps those whose file stores an unresolved_calls
	// diagnostic to CH11's downstream end line.
	leaves     map[string]bool
	unresolved map[string]string
}

func newChainWalk(rd *reader, m *archModel, dir string, depth int, scope string) *chainWalk {
	w := &chainWalk{rd: rd, m: m, scope: scope, dir: dir, rules: chainDown, nodes: map[string]contract.Node{}, member: map[string]*chainMember{},
		edges: map[[3]string]*chainEdge{}, hidden: map[string]int{}, notFollowed: map[string]int{}, ends: map[string]bool{},
		followsIn: map[string]bool{}, followsOut: map[string]bool{}}
	w.opposite = chainUp
	if dir == chainDirUp {
		w.rules, w.opposite = chainUp, chainDown
	}
	for _, r := range w.rules {
		if r.Dir == store.Incoming {
			w.followsIn[r.Edge] = true
		} else {
			w.followsOut[r.Edge] = true
		}
	}
	w.walk = &traverse.Walk{Depth: depth, Visited: func(id string) bool { return w.member[id] != nil },
		Candidates: w.candidates, Seen: w.seen, Reach: w.reach}
	return w
}

// testNode is row CH10's test: a node with scope test, or in a file test
// scope classifies (configuration applied).
func (w *chainWalk) testNode(n contract.Node) bool {
	return testScoped(n, w.m.testFile[n.File])
}

// testScoped is the test-scope rule of the code views (the architecture
// chain's row CH10, its table users and the may-also-use sites): a node
// whose scope attribute is "test", or one in a file test scope classifies.
// testFile is that classification of the node's file.
func testScoped(n contract.Node, testFile bool) bool {
	return n.Attributes["scope"] == "test" || testFile
}

// TestScoped applies the code views' test-scope rule (testScoped) to one
// node with the repository's test rules (built-in names and the tests
// section of graphvein.yaml). The MCP callers and impact tools filter
// with it, so test code is classified by one rule everywhere.
func TestScoped(n contract.Node, tests testscope.Rules) bool {
	return testScoped(n, testscope.Classify(n.File, tests) == testscope.Test)
}

func (w *chainWalk) addMember(n contract.Node, step int, row string) *chainMember {
	cm := &chainMember{ID: n.Identity, Kind: n.Kind, Name: n.Name, QualifiedName: n.QualifiedName, Row: row, Tier: "fact", Parents: []string{}, step: step, confidence: 1}
	if n.File != "" && n.File != "." {
		f := n.File
		cm.File = &f
		if c := w.m.componentOfFile(f); c != "" {
			cm.Component = &c
		}
	}
	if n.Span != nil {
		line := n.Span.StartLine
		cm.Line = &line
	}
	switch n.Kind {
	case "route":
		cm.Method, cm.Path = stringAttr(n, "method"), stringAttr(n, "pattern")
		cm.FullPath = routePath(n, scopeProduction)
		cm.PrefixPath = prefixPath(n, scopeProduction)
	case "client_call":
		cm.Method, cm.Status = stringAttr(n, "method"), stringAttr(n, "status")
	case "ipc_call", "ipc_command":
		cm.Command, cm.Status = stringAttr(n, "command"), stringAttr(n, "status")
	case "table":
		if set := stringAttr(n, "set"); set != nil {
			card := "database:" + *set
			cm.Database = &card
		}
	}
	w.member[n.Identity] = cm
	w.order = append(w.order, n.Identity)
	return cm
}

// candidates reads the frontier's edges and matches them against the
// direction's rules. Test-scope ends are kept out and counted (CH10);
// unmatched edges are counted by kind (CHD); a function that ends the
// upstream chain gets CH11's end line.
func (w *chainWalk) candidates(frontier []string, d int, probe bool) ([]traverse.Candidate, error) {
	sort.Strings(frontier)
	in, err := w.rd.s.Edges(w.rd.ctx, w.rd.repo, frontier, nil, store.Incoming)
	if err != nil {
		return nil, fmt.Errorf("reading incoming edges: %w", err)
	}
	outEdges, err := w.rd.s.Edges(w.rd.ctx, w.rd.repo, frontier, nil, store.Outgoing)
	if err != nil {
		return nil, fmt.Errorf("reading outgoing edges: %w", err)
	}
	front := map[string]bool{}
	for _, id := range frontier {
		front[id] = true
	}
	// A stored caller: any incoming calls edge (CH7 upstream, CH11).
	hasCaller := map[string]bool{}
	for _, e := range in {
		if e.Kind == "calls" && front[e.Target] {
			hasCaller[e.Target] = true
		}
	}
	type pending struct {
		from string
		e    contract.Edge
		dir  store.Direction
		rule *traverse.Rule
	}
	var matched []pending
	followed := map[string]bool{}
	consider := func(from string, e contract.Edge, dir store.Direction) {
		r := traverse.Match(w.rules, traverse.Kind(from), e, dir, nil)
		if r != nil && r.ID == "CH7" && dir == store.Incoming && hasCaller[from] {
			// CH7 upstream: the inner function has a stored caller, so the
			// chain follows that caller instead of the enclosing function.
			if !probe {
				tagRow("CH7/has-caller")
			}
			return
		}
		if r == nil {
			if traverse.Match(w.opposite, traverse.Kind(from), e, dir, nil) != nil {
				// The other direction's relation (the edge back towards
				// the start): not a gap of this one.
				return
			}
			if !probe && (dir == store.Incoming && (w.dir == chainDirUp || w.followsIn[e.Kind]) || dir == store.Outgoing && (w.dir == chainDirDown || w.followsOut[e.Kind])) {
				tagRow("CHD")
				w.notFollowed[e.Kind]++
			}
			return
		}
		followed[from] = true
		matched = append(matched, pending{from, e, dir, r})
	}
	for _, e := range in {
		if front[e.Target] {
			consider(e.Target, e, store.Incoming)
		}
	}
	for _, e := range outEdges {
		if front[e.Source] {
			consider(e.Source, e, store.Outgoing)
		}
	}
	if w.dir == chainDirDown && !probe {
		for _, id := range frontier {
			if k := traverse.Kind(id); (k == "function" || k == "method") && !followed[id] {
				if w.leaves == nil {
					w.leaves = map[string]bool{}
				}
				w.leaves[id] = true
			}
		}
	}
	if w.dir == chainDirUp {
		for _, id := range frontier {
			k := traverse.Kind(id)
			if (k == "function" || k == "method") && !hasCaller[id] && !followed[id] && !w.ends[id] {
				tagRow("CH11")
				w.ends[id] = true
			}
		}
	}
	// Read the nodes at the far ends once, for test scope and the members.
	var unread []string
	for _, p := range matched {
		other := p.e.Source
		if p.dir == store.Outgoing {
			other = p.e.Target
		}
		if _, ok := w.nodes[other]; !ok {
			unread = append(unread, other)
		}
	}
	read, err := w.rd.byIdentity(unread)
	if err != nil {
		return nil, err
	}
	for id, n := range read {
		w.nodes[id] = n
	}
	var out []traverse.Candidate
	for _, p := range matched {
		other := p.e.Source
		if p.dir == store.Outgoing {
			other = p.e.Target
		}
		n, ok := w.nodes[other]
		if !ok {
			// An edge to a node the store does not hold (an external
			// indexer's dangling edge) is not followed.
			if !probe {
				tagRow("CHD/dangling")
				w.notFollowed[p.e.Kind]++
			}
			continue
		}
		if w.scope == scopeProduction && w.member[other] == nil && w.testNode(n) {
			if !probe {
				if _, seen := w.hidden[other]; !seen {
					tagRow("CH10")
					w.hidden[other] = d
				}
			}
			continue
		}
		out = append(out, traverse.Candidate{From: p.from, To: other, Edge: p.e, Dir: p.dir, Rule: p.rule})
	}
	return out, nil
}

// seen records every followed edge between members, so that the chain
// shows all branches, not only the first path to each member.
func (w *chainWalk) seen(c traverse.Candidate) {
	key := [3]string{c.From, c.To, c.Edge.Kind}
	if e, ok := w.edges[key]; ok {
		e.Count++
		return
	}
	tagRow(c.Rule.ID)
	tier, _ := tierOf(c.Edge)
	evidence := c.Edge.Evidence
	if evidence == nil {
		evidence = []contract.Evidence{}
	}
	edge := &chainEdge{From: c.From, To: c.To, Kind: c.Edge.Kind, Row: c.Rule.ID, Wording: w.wording(c), Dashed: c.Rule.ID == "CH7" || c.Rule.ID == "CH8",
		Tier: tier, Count: 1, Evidence: evidence}
	if c.Rule.ID == "CH6" {
		owner := w.nodes[c.Edge.Source]
		edge.Access = changes.AccessEntry(&owner, c.Edge.Kind, c.Edge.Target)
	}
	if c.Edge.Kind == "calls_route" {
		if call, ok := w.nodes[c.Edge.Source]; ok {
			edge.Assumptions = linkAssumptions(call, c.Edge.Target)
		}
	}
	w.edges[key] = edge
}

func (w *chainWalk) wording(c traverse.Candidate) string {
	if c.Rule.ID == "CH6" {
		owner := w.nodes[c.Edge.Source]
		return chainAccessWording(owner, c.Edge, w.dir == chainDirUp)
	}
	pair := chainWording[c.Rule.ID][c.Edge.Kind]
	if w.dir == chainDirUp {
		return pair[1]
	}
	return pair[0]
}

// chainAccessWording is row CH6's wording, ADR 0056 I8's: "reads" or
// "writes" for a fact, "may read" or "may write" when inferred, "builds a
// query on" for a query builder's entry; upstream in the passive.
func chainAccessWording(owner contract.Node, e contract.Edge, up bool) string {
	builds := false
	for _, entry := range objects(owner.Attributes["table_access"]) {
		if entry["kind"] == e.Kind && entry["target"] == e.Target && entry["role"] == "builds" {
			builds = true
		}
	}
	read := e.Kind == "reads_table"
	switch {
	case builds && up:
		return "query built by"
	case builds:
		return "builds a query on"
	case e.Provenance != "fact" && up && read:
		return "may be read by"
	case e.Provenance != "fact" && up:
		return "may be written by"
	case e.Provenance != "fact" && read:
		return "may read"
	case e.Provenance != "fact":
		return "may write"
	case up && read:
		return "read by"
	case up:
		return "written by"
	case read:
		return "reads"
	}
	return "writes"
}

func (w *chainWalk) reach(c traverse.Candidate, d int) {
	parent := w.member[c.From]
	cm := w.addMember(w.nodes[c.To], d, c.Rule.ID)
	tier, confidence := tierOf(c.Edge)
	cm.Tier, cm.TierStep, cm.confidence = parent.Tier, parent.TierStep, parent.confidence
	if confidence < parent.confidence {
		cm.Tier, cm.TierStep, cm.confidence = tier, d, confidence
	}
}

// run walks from the seeds (step 0) and returns the side.
func (w *chainWalk) run(seeds []contract.Node) (*chainSide, error) {
	var ids []string
	for _, n := range seeds {
		if w.member[n.Identity] != nil {
			continue
		}
		w.nodes[n.Identity] = n
		w.addMember(n, 0, "")
		ids = append(ids, n.Identity)
	}
	if err := w.walk.Run(ids); err != nil {
		return nil, err
	}
	if err := w.readInjections(); err != nil {
		return nil, err
	}
	if err := w.readUnresolved(); err != nil {
		return nil, err
	}
	return w.side(), nil
}

// readUnresolved gives CH11's downstream end line to each
// leaf function whose file stores an unresolved_calls diagnostic: the
// chain stops there because calls were not resolved, not because there
// are none. The diagnostic is per file, so the line says the function's
// calls may be among them.
func (w *chainWalk) readUnresolved() error {
	if len(w.leaves) == 0 {
		return nil
	}
	byFile := map[string][]string{}
	for id := range w.leaves {
		if file, ok := identityFile(id); ok {
			byFile[file] = append(byFile[file], id)
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	diags, err := w.rd.s.Diagnostics(w.rd.ctx, w.rd.repo, files)
	if err != nil {
		return fmt.Errorf("reading unresolved-call diagnostics: %w", err)
	}
	w.unresolved = map[string]string{}
	for _, d := range diags {
		if d.Code != "unresolved_calls" {
			continue
		}
		count := "some"
		var n int
		if _, err := fmt.Sscanf(d.Message, "%d calls", &n); err == nil {
			count = strconv.Itoa(n)
		}
		text := "? not modelled: no stored callee; " + d.File + " has " + count + " calls the indexer could not resolve, not followed (this function's calls may be among them)"
		for _, id := range byFile[d.File] {
			w.unresolved[id] = text
		}
	}
	return nil
}

// readInjections reads the routes with security_dependencies (AA3)
// for CH11's named dependency-injection steps: the functions their
// chains name, and each route's chains in words.
func (w *chainWalk) readInjections() error {
	routes, err := w.rd.s.Nodes(w.rd.ctx, w.rd.repo, store.Query{Kind: "route", Attribute: "security_dependencies"})
	if err != nil {
		return fmt.Errorf("reading routes with security dependencies: %w", err)
	}
	w.injected, w.declared = map[string][]string{}, map[string][]string{}
	for _, r := range routes {
		for _, scheme := range objects(r.Attributes["security_dependencies"]) {
			var names []string
			for _, step := range objects(scheme["chain"]) {
				target, _ := step["target"].(string)
				if kind, _, _, err := contract.ParseIdentity(target); err == nil && (kind == "function" || kind == "method") {
					w.injected[target] = append(w.injected[target], r.Identity)
					names = append(names, shortName(target))
					continue
				}
				if _, name, ok := strings.Cut(target, "#"); ok {
					names = append(names, name)
				}
			}
			class, _ := scheme["class"].(string)
			if len(names) > 0 {
				w.declared[r.Identity] = append(w.declared[r.Identity], strings.Join(names, " -> ")+" ("+class+")")
			}
		}
	}
	return nil
}

// shortName is an identity's qualified name, or the identity itself.
func shortName(id string) string {
	if _, _, q, err := contract.ParseIdentity(id); err == nil && q != "" {
		return q
	}
	return id
}

// chainInjectionEnd is row CH11's end line for a function FastAPI injects
// as a declared dependency (named, never a stored call).
func chainInjectionEnd(routes []string) string {
	routes = distinct(routes)
	var shown []string
	for _, id := range routes {
		if len(shown) == 5 {
			shown = append(shown, "+ "+strconv.Itoa(len(routes)-5)+" more")
			break
		}
		_, file, q, err := contract.ParseIdentity(id)
		if err != nil {
			shown = append(shown, id)
			continue
		}
		shown = append(shown, q+" in "+file)
	}
	return "? not modelled: no stored caller; FastAPI dependency injection into " + strconv.Itoa(len(routes)) + " route(s) as declared in security_dependencies (" + strings.Join(shown, ", ") + "); the injection is not a stored call"
}

// side assembles the steps, edges, end lines and caps.
func (w *chainWalk) side() *chainSide {
	s := &chainSide{Steps: []chainStep{}, Edges: []chainEdge{}, Ends: []chainEnd{}, NotFollowed: w.notFollowed, Caps: w.walk.Caps()}
	byStep := map[int][]chainMember{}
	maxStep := 0
	for _, e := range w.edges {
		from, to := w.member[e.From], w.member[e.To]
		if from == nil || to == nil {
			continue
		}
		e.stepOfFrom = from.step
		if to.step == from.step+1 {
			to.Parents = append(to.Parents, from.ID)
		}
		s.Edges = append(s.Edges, *e)
	}
	sort.Slice(s.Edges, func(i, j int) bool {
		a, b := s.Edges[i], s.Edges[j]
		if a.stepOfFrom != b.stepOfFrom {
			return a.stepOfFrom < b.stepOfFrom
		}
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
	for _, id := range w.order {
		cm := w.member[id]
		cm.Parents = distinct(cm.Parents)
		byStep[cm.step] = append(byStep[cm.step], *cm)
		if cm.step > maxStep {
			maxStep = cm.step
		}
		if cm.step > 0 {
			s.Members++
		}
	}
	hiddenBy := map[int]int{}
	for _, step := range w.hidden {
		hiddenBy[step]++
		s.HiddenTest++
		if step > maxStep {
			maxStep = step
		}
	}
	boundsBy := map[int][]chainBound{}
	for _, b := range w.walk.Bounds() {
		tagRow("CH12")
		boundsBy[b.Step] = append(boundsBy[b.Step], chainBound{Cap: b.Cap, Step: b.Step, Beyond: b.Beyond, Text: boundText(b)})
		if b.Step > maxStep && b.Cap != traverse.CapDepth {
			maxStep = b.Step
		}
	}
	for step := 0; step <= maxStep; step++ {
		members := byStep[step]
		if members == nil {
			members = []chainMember{}
		}
		sort.SliceStable(members, func(i, j int) bool { return lessMember(members[i], members[j]) })
		bounds := boundsBy[step]
		if bounds == nil {
			bounds = []chainBound{}
		}
		s.Steps = append(s.Steps, chainStep{Step: step, Members: members, HiddenTest: hiddenBy[step], Bounds: bounds, Map: chainStepMap{Shown: []string{}}})
	}
	// A depth bound sits one step past the last listed step: report it on
	// the last step, where the reader sees the chain stop.
	if b := boundsBy[w.walk.Depth+1]; len(b) > 0 && len(s.Steps) > 0 {
		last := &s.Steps[len(s.Steps)-1]
		last.Bounds = append(last.Bounds, b...)
	}
	for _, id := range w.order {
		if w.ends[id] {
			text := chainNotModelled
			if routes := w.injected[id]; len(routes) > 0 {
				tagRow("CH11/injected")
				text = chainInjectionEnd(routes)
			}
			s.Ends = append(s.Ends, chainEnd{Member: id, Row: "CH11", Text: text})
		}
		if text, ok := w.unresolved[id]; ok && w.dir == chainDirDown {
			tagRow("CH11/unresolved")
			s.Ends = append(s.Ends, chainEnd{Member: id, Row: "CH11", Text: text})
		}
		if chains := w.declared[id]; w.dir == chainDirDown && len(chains) > 0 {
			// A route's declared dependency steps: named, not followed.
			tagRow("CH11/declared")
			s.Ends = append(s.Ends, chainEnd{Member: id, Row: "CH11", Text: "? not modelled: dependencies declared, not followed as calls: " + strings.Join(distinct(chains), "; ")})
		}
	}
	return s
}

// lessMember orders a step's members: those whose first path is a fact
// first, then by tier, kind and identity.
func lessMember(a, b chainMember) bool {
	if a.confidence != b.confidence {
		return a.confidence > b.confidence
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

func boundText(b traverse.Bound) string {
	switch b.Cap {
	case traverse.CapDepth:
		return "+ " + strconv.Itoa(b.Beyond) + " beyond step " + strconv.Itoa(b.Step-1)
	case traverse.CapFanOut:
		return "+ " + strconv.Itoa(b.Beyond) + " not followed at step " + strconv.Itoa(b.Step) + ": over " + strconv.Itoa(traverse.MaxFanOut) + " from one member"
	}
	return "+ " + strconv.Itoa(b.Beyond) + " beyond the " + strconv.Itoa(traverse.MaxReached) + " member cap at step " + strconv.Itoa(b.Step)
}

// nodeChain walks each direction from the seeds.
func nodeChain(rd *reader, m *archModel, seeds []contract.Node, dirs []string, depth int, scope string) (map[string]*chainSide, error) {
	out := map[string]*chainSide{}
	for _, dir := range dirs {
		side, err := newChainWalk(rd, m, dir, depth, scope).run(seeds)
		if err != nil {
			return nil, err
		}
		out[dir] = side
	}
	return out, nil
}

// componentChain is row CH9 for a component: its CE1-CE4 links (and the
// SQL links to database cards), transitively over components, on the same
// walk and caps. Test components are not followed in the default scope
// (CH10).
func componentChain(rd *reader, m *archModel, start string, dirs []string, depth int, scope string, px *archProjectIn) (map[string]*chainSide, error) {
	tagRow("CH9/component")
	g, err := buildLinks(rd, m, scope, px)
	if err != nil {
		return nil, err
	}
	out := map[string]*chainSide{}
	for _, dir := range dirs {
		side, err := componentSide(m, g, start, dir, depth, scope)
		if err != nil {
			return nil, err
		}
		out[dir] = side
	}
	return out, nil
}

func componentSide(m *archModel, g *archGraph, start, dir string, depth int, scope string) (*chainSide, error) {
	members := map[string]*chainMember{}
	var order []string
	edges := map[[3]string]*chainEdge{}
	hidden := map[string]int{}
	add := func(id string, step int, row string) *chainMember {
		cm := &chainMember{ID: id, Kind: "component", Name: id, QualifiedName: id, Row: row, Tier: "fact", Parents: []string{}, step: step, confidence: 1}
		if c, ok := m.components[id]; ok {
			cm.Name = c.Name
			dir := c.Directory
			cm.Component = &dir
		} else if repo, ok := strings.CutPrefix(id, remoteCardPrefix); ok {
			// CH13: another member's card, reached by a CE8 link; the
			// chain ends there.
			cm.Kind, cm.Name, cm.Row = "repository", repo+" (other repository)", "CH13"
			cm.Repository = &repo
		} else {
			cm.Kind = "database"
		}
		members[id] = cm
		order = append(order, id)
		return cm
	}
	// links by component, in the direction of the walk.
	type hop struct {
		to   string
		link *archLink
		n    int
	}
	hops := map[string][]hop{}
	for _, l := range g.links {
		if l.Count > 0 {
			if dir == chainDirDown {
				hops[l.Source] = append(hops[l.Source], hop{l.Target, l, l.Count})
			} else {
				hops[l.Target] = append(hops[l.Target], hop{l.Source, l, l.Count})
			}
		}
		if l.BackCount > 0 {
			if dir == chainDirDown {
				hops[l.Target] = append(hops[l.Target], hop{l.Source, l, l.BackCount})
			} else {
				hops[l.Source] = append(hops[l.Source], hop{l.Target, l, l.BackCount})
			}
		}
	}
	add(start, 0, "")
	walk := &traverse.Walk{Depth: depth, Visited: func(id string) bool { return members[id] != nil }}
	walk.Candidates = func(frontier []string, d int, probe bool) ([]traverse.Candidate, error) {
		sort.Strings(frontier)
		var out []traverse.Candidate
		for _, from := range frontier {
			if strings.HasPrefix(from, remoteCardPrefix) {
				// CH13: the other repository is not walked.
				continue
			}
			for _, h := range hops[from] {
				if c, ok := m.components[h.to]; ok && scope == scopeProduction && c.HiddenBy != nil && *c.HiddenBy == "tests" && members[h.to] == nil {
					if !probe {
						if _, seen := hidden[h.to]; !seen {
							tagRow("CH10")
							hidden[h.to] = d
						}
					}
					continue
				}
				out = append(out, traverse.Candidate{From: from, To: h.to, Rule: &traverse.Rule{ID: h.link.Row}, Data: h})
			}
		}
		return out, nil
	}
	walk.Seen = func(c traverse.Candidate) {
		key := [3]string{c.From, c.To, ""}
		if _, ok := edges[key]; ok {
			return
		}
		h := c.Data.(hop)
		tier := h.link.Weakest
		if tier == "" {
			tier = "fact"
		}
		wording := h.link.Family + " link (" + h.link.Row + ")"
		if h.link.Row == "CE8" {
			wording = h.link.Family + " link across repositories (CE8, project link)"
		}
		edges[key] = &chainEdge{From: c.From, To: c.To, Kind: h.link.Family, Row: "CH9", Wording: wording, Tier: tier, Count: h.n, Evidence: []contract.Evidence{}}
	}
	walk.Reach = func(c traverse.Candidate, d int) {
		parent := members[c.From]
		cm := add(c.To, d, "CH9")
		cm.Tier, cm.TierStep, cm.confidence = parent.Tier, parent.TierStep, parent.confidence
		if l := c.Data.(hop).link; l.weakest < cm.confidence {
			cm.Tier, cm.TierStep, cm.confidence = l.Weakest, d, l.weakest
		}
	}
	if err := walk.Run([]string{start}); err != nil {
		return nil, err
	}
	w := &chainWalk{member: members, order: order, edges: edges, hidden: hidden, notFollowed: map[string]int{}, ends: map[string]bool{}, walk: walk}
	side := w.side()
	for _, id := range order {
		if repo, ok := strings.CutPrefix(id, remoteCardPrefix); ok {
			tagRow("CH13/component")
			side.Ends = append(side.Ends, chainEnd{Member: id, Row: "CH13", Text: continuesIn(repo, "")})
		}
	}
	return side, nil
}

// applyMapBudget marks, per step, the members the map draws (the map
// budget, AG16) and folds the rest into counts. The lists stay whole: the
// inspector and the Flow tab read every member.
func applyMapBudget(out *chainResponse) {
	shown := map[string]bool{}
	total := 0
	pick := func(st *chainStep, limit int) {
		// Members whose parent is drawn first, so drawn edges connect.
		var first, rest []string
		for _, cm := range st.Members {
			connected := st.Step == 0
			for _, p := range cm.Parents {
				if shown[p] {
					connected = true
				}
			}
			if connected {
				first = append(first, cm.ID)
			} else {
				rest = append(rest, cm.ID)
			}
		}
		for _, id := range append(first, rest...) {
			if len(st.Map.Shown) >= limit {
				break
			}
			if shown[id] {
				// Already drawn from the other direction (a cycle).
				st.Map.Shown = append(st.Map.Shown, id)
				continue
			}
			if total >= mapTotal {
				break
			}
			st.Map.Shown = append(st.Map.Shown, id)
			shown[id] = true
			total++
		}
		st.Map.Folded = len(st.Members) - len(st.Map.Shown)
	}
	sides := []*chainSide{}
	for _, s := range []*chainSide{out.Up, out.Down} {
		if s != nil {
			sides = append(sides, s)
		}
	}
	// The start is drawn once, from the first side's step 0.
	if len(sides) > 0 && len(sides[0].Steps) > 0 {
		pick(&sides[0].Steps[0], mapPerStep)
		for _, s := range sides[1:] {
			if len(s.Steps) > 0 {
				s.Steps[0].Map = sides[0].Steps[0].Map
			}
		}
	}
	for step := 1; step <= mapSteps; step++ {
		for _, s := range sides {
			if step < len(s.Steps) {
				pick(&s.Steps[step], mapPerStep)
			}
		}
	}
	// A fold never hides a single member: a step with one member left over
	// draws it instead of "+ 1 more", within the step budget always, past
	// the third step when its parent is drawn (so the line stays joined).
	for _, s := range sides {
		for i := 1; i < len(s.Steps); i++ {
			st := &s.Steps[i]
			var rest []chainMember
			for _, cm := range st.Members {
				if !contains(st.Map.Shown, cm.ID) {
					rest = append(rest, cm)
				}
			}
			if len(rest) != 1 {
				continue
			}
			joined := i <= mapSteps
			for _, p := range rest[0].Parents {
				if shown[p] {
					joined = true
				}
			}
			if !joined {
				continue
			}
			st.Map.Shown = append(st.Map.Shown, rest[0].ID)
			if !shown[rest[0].ID] {
				shown[rest[0].ID] = true
				total++
			}
		}
	}
	out.Map.Shown = total
	for _, s := range sides {
		for i := range s.Steps {
			st := &s.Steps[i]
			st.Map.Folded = len(st.Members) - len(st.Map.Shown)
			if i > 0 || s == sides[0] {
				out.Map.Folded += st.Map.Folded
			}
		}
	}
	out.Map.OpenInFlow = out.Map.Folded > 0
}

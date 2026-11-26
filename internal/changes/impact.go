package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/traverse"
)

// Traversal limits (claim contract, "Impact relation rows"): the shared
// caps of internal/traverse, which the architecture chain applies too.
const (
	DefaultDepth = 3
	MaxDepth     = traverse.MaxDepth
	MaxReached   = traverse.MaxReached
	MaxFanOut    = traverse.MaxFanOut
)

// Caps that can bind.
const (
	CapDepth   = traverse.CapDepth
	CapReached = traverse.CapReached
	CapFanOut  = traverse.CapFanOut
)

// ErrNotSeed: the item is not a seed of impact (rows C1, C14, C15, D and
// edge items start no traversal).
var ErrNotSeed = errors.New("the item starts no impact traversal")

// ErrUnknownItem: no item of the comparison has the id.
var ErrUnknownItem = errors.New("the comparison has no item with this id")

// follows is the relation table of impact (traverse.Rule: from a node of a
// kind in From, the edges of one kind in one direction, to a dependent
// whose kind is in To, nil: any kind). Every I row with a relation has its
// entries here; I11 is not extracted yet, ID is everything else
// (untraversed).
var follows = []traverse.Rule{
	rule("I1", []string{"function", "method", "class"}, "calls", store.Incoming, nil),
	rule("I2", []string{"client_call", "ipc_call", "ui_route"}, "contains", store.Incoming, []string{"function", "method", "class", "variable"}),
	rule("I3", []string{"route"}, "calls_route", store.Incoming, []string{"client_call"}),
	rule("I3", []string{"route"}, "documents_route", store.Incoming, []string{"api_operation"}),
	rule("I4", []string{"function", "method"}, "exposes_route", store.Outgoing, []string{"route"}),
	rule("I4", []string{"function", "method"}, "exposes_command", store.Outgoing, []string{"ipc_command"}),
	rule("I5", []string{"ipc_command"}, "invokes_command", store.Incoming, []string{"ipc_call"}),
	rule("I6", []string{"api_operation"}, "calls_operation", store.Incoming, []string{"client_call"}),
	// I7: a column's referencing columns; a table's referencing columns
	// are read through its columns (tableColumns); later migrations.
	rule("I7", []string{"column"}, "foreign_key", store.Incoming, []string{"column"}),
	rule("I7", []string{"table"}, "alters", store.Incoming, []string{"migration"}),
	// I8 (Revision 4): the code using a table, any owner kind; a column
	// reaches its table's users through tableUsers.
	rule("I8", []string{"table"}, "reads_table", store.Incoming, nil),
	rule("I8", []string{"table"}, "writes_table", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "renders", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "injects", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "routes_to", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "loads_children", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "uses_pipe", store.Incoming, nil),
	rule("I9", []string{"class", "file"}, "uses_template", store.Incoming, nil),
	// I14 (T026 AG-F): React's element owner and the react-router route of
	// a function component (rows JR1-JR3, RR3, RR5); class targets are I9's.
	rule("I14", []string{"function"}, "renders", store.Incoming, nil),
	rule("I14", []string{"function"}, "routes_to", store.Incoming, []string{"ui_route"}),
	// I15 (T026 AG-F): the code that passes the function as a value (rows
	// PF1-PF3, RR4, RR5) depends on its signature and behaviour as a
	// callback; the call is not modelled, so the path is inferred.
	rule("I15", []string{"function", "method"}, "passes_function", store.Incoming, nil),
	// I16 (T029 W2, rows MT-13..MT-19 of
	// docs/contract/claims/route-model-types.md): a route whose request or
	// response shape uses a changed model declaration.
	rule("I16", modelDeclarationKinds, "uses_model", store.Incoming, []string{"route"}),
	// I10: imports are followed one hop only: a file reached through an
	// import does not follow imports again.
	rule("I10", []string{"file"}, "imports", store.Incoming, []string{"file"}),
}

// modelDeclarationKinds are the node kinds a uses_model edge can point at
// (row MT-3; routeshape.DeclarationKind): model classes, structs, types
// and the module-level consts of named zod or Joi schemas.
var modelDeclarationKinds = []string{"class", "constant", "enum", "interface", "struct", "type", "type_alias", "union", "variable"}

// inferredRelations are the relations whose dependency is inferred even
// when the edge they follow is a fact.
var inferredRelations = map[string]bool{"I15": true}

func rule(id string, from []string, edge string, dir store.Direction, to []string) traverse.Rule {
	return traverse.Rule{ID: id, From: from, Edge: edge, Dir: dir, To: to}
}

// outgoingKinds are the edge kinds some relation follows outgoing.
var outgoingKinds = func() map[string]bool {
	out := map[string]bool{}
	for _, f := range follows {
		if f.Dir == store.Outgoing {
			out[f.Edge] = true
		}
	}
	return out
}()

// followOf returns the follows entry of a relation and edge kind; a
// missing entry is a programming error reported as an error, not a panic
// (R3-10).
func followOf(relation, edge string) (*traverse.Rule, error) {
	for i := range follows {
		if follows[i].ID == relation && follows[i].Edge == edge {
			return &follows[i], nil
		}
	}
	return nil, fmt.Errorf("changes: no follows entry %s %s", relation, edge)
}

func inList(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// match finds the relation that follows edge e from a node of kind, or nil.
// Imports are followed one hop only (I10).
func match(kind string, e contract.Edge, dir store.Direction, viaImport bool) *traverse.Rule {
	return traverse.Match(follows, kind, e, dir, func(r *traverse.Rule) bool { return r.ID == "I10" && viaImport })
}

// ImpactRequest asks for the traversal from one item (ID) or from every
// seed item of a group (Group).
type ImpactRequest struct {
	ID    string
	Group string
	// Depth is 1 to MaxDepth; 0 means DefaultDepth.
	Depth int
	// Project, when set, continues the traversal through the project's
	// stored links into other member repositories (relations I12, I13;
	// ADR 0075, T022 B7). Nil keeps the single-repository traversal.
	Project *ProjectScope
}

// Step is one edge of a path.
type Step struct {
	Kind       string              `json:"kind"`
	Source     string              `json:"source"`
	Target     string              `json:"target"`
	Relation   string              `json:"relation"`
	Provenance string              `json:"provenance"`
	Confidence float64             `json:"confidence"`
	Evidence   []contract.Evidence `json:"evidence"`
	// Via names the column a table's foreign key reaches it through (I7).
	Via *string `json:"via"`
	// Removed marks an edge read from the change set: a removed seed's
	// first hop.
	Removed bool `json:"removed"`
	// Access is the owner's table_access entry of an I8 step (Revision 4);
	// no other step has it.
	Access *TableAccess `json:"access,omitempty"`
	// Model is the route's use of the model declaration of an I16 step
	// (rows MT-14, MT-15); no other step has it.
	Model *ModelStep `json:"model,omitempty"`
	// SourceRepository and TargetRepository name the members the step's
	// ends belong to; set only at project scope (T022 B7), where an I12 or
	// I13 step joins two repositories through a project link.
	SourceRepository string `json:"source_repository,omitempty"`
	TargetRepository string `json:"target_repository,omitempty"`
	// Assumption is the project link's assumption (I12, I13 only).
	Assumption string `json:"assumption,omitempty"`
	// LinkAssumptions are the assumptions a calls_route step's link rests
	// on, read from its client call (clients.LinkAssumptions): an origin
	// nothing proves to be the server (LF-1) and the unknown-prefix ones
	// (UP-V1, UP-V2; T033). Absent on other steps and when there is none.
	LinkAssumptions []clients.LinkAssumption `json:"link_assumptions,omitempty"`
	// StaleReasons says why a project link step is stale (a member indexed
	// again since the link pass, or the manifest changed); empty when it
	// is fresh.
	StaleReasons []string `json:"stale_reasons,omitempty"`
}

// Path is the shortest path from a seed to one dependent.
type Path struct {
	Node     string `json:"node"`
	Kind     string `json:"kind"`
	Seed     string `json:"seed"`
	Distance int    `json:"distance"`
	Relation string `json:"relation"`
	// Claim is "depends on" for a fact path, "may depend on" otherwise;
	// Wording is the last relation's wording. Never "breaks".
	Claim      string `json:"claim"`
	Wording    string `json:"wording"`
	Provenance string `json:"provenance"`
	// Confidence is the weakest inferred step's confidence, 1 for a fact
	// path, and null for a path inferred only by a rule (an I15 step or a
	// gap, R3-10) while every step is a fact: no stored confidence
	// measures it (Revision 7, review fix).
	Confidence *float64 `json:"confidence"`
	Steps      []Step   `json:"steps"`
	// RoutePaths are a reached route's composed paths as stored on its
	// node (ADR 0013), so a reader can show the full path with router
	// prefixes rather than the path as written in the route's file. Set
	// only on route paths; an empty list means the route is not mounted.
	RoutePaths *[]RoutePath `json:"route_paths,omitempty"`
	// PrefixUnknownPaths are a reached route's paths whose prefix the
	// code does not fix (T033 rows UP-29, UP-32; a Next.js basePath not
	// read, NX-R4): never full paths, so never in RoutePaths. Set only
	// when the route has one.
	PrefixUnknownPaths *[]routes.PrefixUnknownPath `json:"prefix_unknown_paths,omitempty"`
	// RouteShadowed marks a reached route whose every composed path an
	// earlier mount takes first (route attribute shadowed_paths):
	// it is mounted, its paths withheld, so a reader says
	// "shadowed" rather than "not mounted" for its empty RoutePaths.
	RouteShadowed bool `json:"route_shadowed,omitempty"`
	// RouteWithheld marks a reached route that is a registration whose
	// path the Go linker withholds (couldn't-read row CR-44, from the
	// stored diagnostics of the route's graph): found, but another
	// pattern or route may answer its path, so a reader says "path
	// withheld" for its empty RoutePaths, never "not mounted".
	RouteWithheld bool `json:"route_withheld,omitempty"`
	// Repository is the member holding Node, set only at project scope
	// (T022 B7). Assumptions are those of the path's project links, and
	// Stale reports that one of them is stale: the path is still shown.
	Repository  string   `json:"repository,omitempty"`
	Assumptions []string `json:"assumptions,omitempty"`
	Stale       bool     `json:"stale,omitempty"`
}

// RoutePath is one composed path of a route node: its template, its
// scope ("test" for a path only test code mounts, else null) and whether
// the served path ends in a slash the normalized template drops (FastAPI,
// ADR 0019; entries differing only in it are distinct paths).
type RoutePath struct {
	Template      *string `json:"template"`
	Scope         *string `json:"scope"`
	TrailingSlash bool    `json:"trailing_slash"`
}

// NearMiss is a call whose ambiguous or test-only choice lists a changed
// route as a candidate without an edge to it (ADR 0042).
type NearMiss struct {
	Route  string `json:"route"`
	Call   string `json:"call"`
	Status string `json:"status"`
}

// Unknowns are what the traversal cannot claim (never counted as impact).
type Unknowns struct {
	BlindSpots  []query.MissingSite `json:"blind_spots"`
	NearMisses  []NearMiss          `json:"near_misses"`
	GapNodes    []string            `json:"gap_nodes"`
	Untraversed map[string]int      `json:"untraversed"`
	// NotExtracted lists relations without facts yet, with the nodes they
	// apply to (I8: tables and columns of a source no code-to-table row
	// covers) or the whole traversal (I11).
	NotExtracted []NotExtracted `json:"not_extracted"`
}

// NotExtracted is one relation answered not_extracted.
type NotExtracted struct {
	Relation string   `json:"relation"`
	Nodes    []string `json:"nodes"`
}

// ImpactResult is one traversal.
type ImpactResult struct {
	Seeds    []string `json:"seeds"`
	Depth    int      `json:"depth"`
	Paths    []Path   `json:"paths"`
	Unknowns Unknowns `json:"unknowns"`
	Caps     []string `json:"caps"`
	// TableUses answers, per table whose users I8 read, the users found
	// (a lower bound) and the owners that may also use it.
	TableUses []TableUse `json:"table_uses"`
	// Project and Members are set only at project scope (T022 B7):
	// Unknowns and TableUses above are the compared member's, Members holds
	// those of every other member the traversal reached, and Caps covers
	// all of them.
	Project string         `json:"project,omitempty"`
	Members []MemberImpact `json:"members,omitempty"`
	// PossibleDependents (project scope only, T022 review) are sources
	// of other members that may depend on a reached route without a
	// project link: never paths, never followed further.
	PossibleDependents []PossibleDependent `json:"possible_dependents,omitempty"`
}

type reached struct {
	path      Path
	viaImport bool
}

// Impact traverses outward from the requested item or group's seeds. A
// removed seed takes its first hop through the removed edges of the change
// set and continues in the graph that still holds its dependents: the
// current graph for run and pin bases, the base namespace for ref bases.
func Impact(ctx context.Context, s store.Store, c *Comparison, req ImpactRequest) (*ImpactResult, error) {
	depth := req.Depth
	if depth == 0 {
		depth = DefaultDepth
	}
	if depth < 1 || depth > MaxDepth {
		return nil, fmt.Errorf("depth %d is outside 1 to %d", depth, MaxDepth)
	}
	var seeds []*Item
	if req.ID != "" {
		i, ok := c.byID[req.ID]
		if !ok {
			return nil, ErrUnknownItem
		}
		if !c.items[i].seed || c.items[i].Record != store.RecordNode {
			return nil, ErrNotSeed
		}
		// The graph a comparison reads is fixed (G6), so an earlier
		// traversal of the same item and depth is the answer.
		c.mu.Lock()
		r, done := c.impacts[req.ID]
		c.mu.Unlock()
		if done && r.Depth == depth && req.Project == nil {
			return r, nil
		}
		seeds = append(seeds, &c.items[i])
	} else {
		for i := range c.items {
			it := &c.items[i]
			if it.Group == req.Group && it.seed && it.Record == store.RecordNode {
				seeds = append(seeds, it)
			}
		}
	}
	if req.Project != nil && c.graph[head] != c.Repository {
		return nil, ErrProjectRef
	}
	t := newTraversal(ctx, s, c, depth, nil)
	if req.Project != nil {
		t.label = c.Repository
	}
	var removed, present []*Item
	for _, it := range seeds {
		t.visited[it.Identity] = &reached{path: Path{Node: it.Identity, Kind: it.Kind, Seed: it.Identity}}
		if it.Change == Removed {
			removed = append(removed, it)
		} else {
			present = append(present, it)
		}
	}
	if err := t.walk(present, c.graph[head], false); err != nil {
		return nil, err
	}
	if err := t.walk(removed, c.graph[base], true); err != nil {
		return nil, err
	}
	var consumers []*traversal
	if req.Project != nil {
		var err error
		if consumers, err = t.followProject(req.Project); err != nil {
			return nil, err
		}
	}
	out, err := t.result(seeds)
	if err != nil {
		return nil, err
	}
	if req.Project != nil {
		if err := t.mergeProject(out, req.Project, consumers); err != nil {
			return nil, err
		}
		return out, nil
	}
	if req.ID != "" {
		c.mu.Lock()
		c.impacts[req.ID] = out
		c.mu.Unlock()
	}
	return out, nil
}

type traversal struct {
	ctx          context.Context
	s            store.Store
	c            *Comparison
	depth        int
	visited      map[string]*reached
	untraversed  map[string]int
	notExtracted map[string]map[string]bool
	// graphs records which graph each reached node was read from.
	graphs map[string]string
	// walk is the shared breadth-first walk (its caps and reached count
	// carry over from present to removed seeds, and at project scope to
	// every member's traversal, T022 B7).
	walker *traverse.Walk
	// owners are the current step's I8 owner nodes (accessOwners).
	owners map[string]*contract.Node
	// modelRoutes are the current step's I16 route nodes (modelRoutes).
	modelRoutes map[string]*contract.Node
	// label is the member repository at project scope, empty otherwise;
	// know, when set, is the knowledge of a member graph no side of the
	// comparison covers (a consumer reached through a project link).
	label string
	know  *knowledge
	// hopped are the routes whose project links were followed; missing
	// are link sources the member no longer stores.
	hopped  map[string]bool
	missing []string
	// possible are the possible dependents found at the reached routes
	// (kept on the compared member's traversal only).
	possible []PossibleDependent
	// useTables are the tables whose users I8 read, with the graph they
	// were read from; uses are the owners found per table.
	useTables map[string]string
	uses      map[string]map[string]bool
}

func (t *traversal) note(relation, node string) {
	if t.notExtracted[relation] == nil {
		t.notExtracted[relation] = map[string]bool{}
	}
	if node != "" {
		t.notExtracted[relation][node] = true
	}
}

// edgesOf reads the incident edges of ids in graph; for a removed seed's
// first hop, the removed edges of the change set instead.
func (t *traversal) edgesOf(ids []string, graph string, fromChangeSet bool, dir store.Direction) ([]contract.Edge, error) {
	if !fromChangeSet {
		return t.s.Edges(t.ctx, graph, ids, nil, dir)
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []contract.Edge
	for _, rc := range t.c.set.Changes {
		if rc.Record != store.RecordEdge || rc.Before == nil || rc.After != nil {
			continue
		}
		var e contract.Edge
		if decode(rc.Before, &e) != nil {
			continue
		}
		if dir == store.Incoming && want[e.Target] || dir == store.Outgoing && want[e.Source] {
			out = append(out, e)
		}
	}
	return out, nil
}

func (t *traversal) walk(seeds []*Item, graph string, removed bool) error {
	frontier := []string{}
	for _, it := range seeds {
		frontier = append(frontier, it.Identity)
		t.graphs[it.Identity] = graph
	}
	return t.prepare(graph, removed).Run(frontier)
}

// walkLevels continues the walk in graph from nodes already visited at
// different distances (project link sources, T022 B7): levels maps a
// distance to the nodes placed there.
func (t *traversal) walkLevels(levels map[int][]string, graph string) error {
	return t.prepare(graph, false).RunLevels(levels)
}

// prepare points the shared walk at this traversal and graph; removed
// marks a walk from removed seeds, whose first step reads the change set.
func (t *traversal) prepare(graph string, removed bool) *traverse.Walk {
	w := t.walker
	w.Visited = func(id string) bool { return t.visited[id] != nil }
	w.Candidates = func(frontier []string, d int, probe bool) ([]traverse.Candidate, error) {
		if probe {
			return t.probe(frontier, graph)
		}
		cands, err := t.candidates(frontier, graph, removed && d == 1)
		if err != nil {
			return nil, err
		}
		if t.owners, err = t.accessOwners(cands, graph); err != nil {
			return nil, err
		}
		if t.modelRoutes, err = t.modelRouteNodes(cands, graph); err != nil {
			return nil, err
		}
		out := make([]traverse.Candidate, 0, len(cands))
		for i := range cands {
			out = append(out, traverse.Candidate{From: cands[i].from, To: cands[i].other(), Edge: cands[i].edge, Dir: cands[i].dir, Rule: cands[i].f, Data: &cands[i]})
		}
		return out, nil
	}
	w.Seen = func(c traverse.Candidate) {
		cd := c.Data.(*candidate)
		if cd.f.ID == "I8" {
			if t.uses[cd.edge.Target] == nil {
				t.uses[cd.edge.Target] = map[string]bool{}
			}
			t.uses[cd.edge.Target][cd.edge.Source] = true
		}
	}
	w.Reach = func(c traverse.Candidate, d int) {
		t.reach(c.Data.(*candidate), graph, removed && d == 1, d)
	}
	return w
}

// candidates are the edges a frontier node may follow, with the relation.
type candidate struct {
	from string
	edge contract.Edge
	dir  store.Direction
	f    *traverse.Rule
	via  *string
	// columnUse marks an I8 edge reached from a column through its table.
	columnUse bool
}

// other is the node the candidate leads to.
func (cd *candidate) other() string {
	if cd.dir == store.Outgoing {
		return cd.edge.Target
	}
	return cd.edge.Source
}

func (t *traversal) candidates(frontier []string, graph string, fromChangeSet bool) ([]candidate, error) {
	sort.Strings(frontier)
	var out []candidate
	in, err := t.edgesOf(frontier, graph, fromChangeSet, store.Incoming)
	if err != nil {
		return nil, fmt.Errorf("reading incoming edges: %w", err)
	}
	outEdges, err := t.edgesOf(frontier, graph, fromChangeSet, store.Outgoing)
	if err != nil {
		return nil, fmt.Errorf("reading outgoing edges: %w", err)
	}
	front := map[string]bool{}
	for _, id := range frontier {
		front[id] = true
	}
	var columns []string
	columnOf := map[string]string{}
	add := func(from string, e contract.Edge, dir store.Direction, via *string) {
		r := t.visited[from]
		f := match(identityKind(from), e, dir, r != nil && r.viaImport)
		if f == nil {
			// ID: an edge toward a possible dependent that no relation
			// follows. Outgoing edges count only for the kinds some
			// relation follows outgoing (exposes_*): other outgoing edges
			// point at what the node depends on, not at dependents.
			if dir == store.Incoming || outgoingKinds[e.Kind] {
				t.untraversed[e.Kind]++
			}
			return
		}
		out = append(out, candidate{from: from, edge: e, dir: dir, f: f, via: via})
	}
	tableOf := map[string]string{}
	for _, e := range in {
		if front[e.Target] {
			if e.Kind == "contains" && identityKind(e.Source) == "table" && identityKind(e.Target) == "column" {
				tableOf[e.Target] = e.Source
			}
			add(e.Target, e, store.Incoming, nil)
		}
	}
	for _, e := range outEdges {
		if !front[e.Source] {
			continue
		}
		if identityKind(e.Source) == "table" && e.Kind == "contains" && identityKind(e.Target) == "column" {
			columns = append(columns, e.Target)
			columnOf[e.Target] = e.Source
		}
		add(e.Source, e, store.Outgoing, nil)
	}
	if len(columns) > 0 && !fromChangeSet {
		fks, err := t.s.Edges(t.ctx, graph, columns, []string{"foreign_key"}, store.Incoming)
		if err != nil {
			return nil, fmt.Errorf("reading foreign keys: %w", err)
		}
		for _, e := range fks {
			table, col := columnOf[e.Target], e.Target
			if identityKind(e.Source) != "column" {
				t.untraversed[e.Kind]++
				continue
			}
			f, err := followOf("I7", "foreign_key")
			if err != nil {
				return nil, err
			}
			out = append(out, candidate{from: table, edge: e, dir: store.Incoming, f: f, via: &col})
		}
	}
	users, err := t.tableUsers(frontier, tableOf, graph, fromChangeSet)
	if err != nil {
		return nil, err
	}
	return append(out, users...), nil
}

// probe lists the candidates of a frontier past the depth cap, so that the
// walk can tell whether the cap binds, leaving the traversal's counts as
// they were.
func (t *traversal) probe(frontier []string, graph string) ([]traverse.Candidate, error) {
	saved := map[string]int{}
	for k, v := range t.untraversed {
		saved[k] = v
	}
	tables := map[string]string{}
	for k, v := range t.useTables {
		tables[k] = v
	}
	cands, err := t.candidates(frontier, graph, false)
	t.untraversed, t.useTables = saved, tables
	if err != nil {
		return nil, err
	}
	out := make([]traverse.Candidate, 0, len(cands))
	for i := range cands {
		out = append(out, traverse.Candidate{From: cands[i].from, To: cands[i].other(), Edge: cands[i].edge, Dir: cands[i].dir, Rule: cands[i].f})
	}
	return out, nil
}

// reach records the path of a candidate the walk admitted at step d.
func (t *traversal) reach(cd *candidate, graph string, fromChangeSet bool, d int) {
	other := cd.other()
	parent := t.visited[cd.from].path
	step := Step{Kind: cd.edge.Kind, Source: cd.edge.Source, Target: cd.edge.Target, Relation: cd.f.ID,
		Provenance: cd.edge.Provenance, Confidence: cd.edge.Confidence, Evidence: cd.edge.Evidence, Via: cd.via, Removed: fromChangeSet}
	if step.Evidence == nil {
		step.Evidence = []contract.Evidence{}
	}
	if cd.f.ID == "I8" {
		step.Access = AccessEntry(t.owners[cd.edge.Source], cd.edge.Kind, cd.edge.Target)
		if cd.columnUse {
			step.Access.ColumnUse = ptr("not_extracted")
		}
	}
	if cd.f.ID == "I16" {
		step.Model = modelStep(t.modelRoutes[cd.edge.Source], cd.edge.Target)
	}
	step.SourceRepository, step.TargetRepository = t.label, t.label
	p := Path{Node: other, Kind: identityKind(other), Seed: parent.Seed, Distance: d, Relation: cd.f.ID,
		Steps: append(append([]Step{}, parent.Steps...), step)}
	t.visited[other] = &reached{path: p, viaImport: cd.f.ID == "I10"}
	t.graphs[other] = graph
}

func (t *traversal) result(seeds []*Item) (*ImpactResult, error) {
	out := &ImpactResult{Seeds: []string{}, Depth: t.depth, Paths: []Path{}, Caps: []string{}, TableUses: []TableUse{},
		Unknowns: Unknowns{BlindSpots: []query.MissingSite{}, NearMisses: []NearMiss{}, GapNodes: []string{}, Untraversed: t.untraversed, NotExtracted: []NotExtracted{}}}
	seedSet := map[string]bool{}
	for _, it := range seeds {
		out.Seeds = append(out.Seeds, it.Identity)
		seedSet[it.Identity] = true
	}
	sort.Strings(out.Seeds)
	// Reached nodes in gap files of the side they were read from.
	gaps := map[string]bool{}
	for id, graph := range t.graphs {
		side := head
		if graph != t.c.graph[head] {
			side = base
		}
		know := t.c.know[side]
		if t.know != nil {
			know = *t.know
		}
		if f := identityFile(id); f != "" && f != "." && !know.knows([]string{f}) {
			gaps[id] = true
			out.Unknowns.GapNodes = append(out.Unknowns.GapNodes, id)
		}
	}
	sort.Strings(out.Unknowns.GapNodes)
	for id, r := range t.visited {
		if seedSet[id] {
			continue
		}
		p := r.path
		p.Provenance = "fact"
		conf := 1.0
		for _, s := range p.Steps {
			if s.Provenance != "fact" {
				p.Provenance = "inferred"
			}
			if s.Confidence < conf {
				conf = s.Confidence
			}
		}
		stepInferred := p.Provenance == "inferred"
		// I15: passing a function proves no call, so the dependency on it
		// is inferred even though the passing itself is a fact.
		for _, s := range p.Steps {
			if inferredRelations[s.Relation] {
				p.Provenance = "inferred"
			}
		}
		// R3-10: a path through a node in a gap file is inferred: the
		// gap may hide or distort the facts it was read from.
		if gaps[p.Node] {
			p.Provenance = "inferred"
		}
		for _, s := range p.Steps {
			if gaps[s.Source] || gaps[s.Target] {
				p.Provenance = "inferred"
			}
		}
		for _, s := range p.Steps {
			if s.Assumption != "" && !inList(p.Assumptions, s.Assumption) {
				p.Assumptions = append(p.Assumptions, s.Assumption)
			}
			p.Stale = p.Stale || len(s.StaleReasons) > 0
		}
		p.Claim = "depends on"
		if p.Provenance != "fact" {
			p.Claim = "may depend on"
		}
		if p.Provenance == "fact" || stepInferred {
			p.Confidence = &conf
		}
		rel, _ := relationOf(p.Relation)
		p.Wording = rel.Wording
		if n := len(p.Steps); n > 0 {
			if w, ok := edgeWordings[p.Steps[n-1].Kind]; ok && !projectRelation[p.Steps[n-1].Relation] {
				p.Wording = w
			}
			if p.Steps[n-1].Relation == "I8" {
				p.Wording = accessWording(p.Steps[n-1])
			}
			if p.Steps[n-1].Relation == "I16" {
				p.Wording = modelWording(p.Steps[n-1])
			}
		}
		out.Paths = append(out.Paths, p)
	}
	sort.Slice(out.Paths, func(i, j int) bool {
		a, b := out.Paths[i], out.Paths[j]
		if a.Distance != b.Distance {
			return a.Distance < b.Distance
		}
		return a.Node < b.Node
	})
	if err := t.routePaths(out); err != nil {
		return nil, err
	}
	if err := t.linkAssumptions(out); err != nil {
		return nil, err
	}
	if t.walker != nil {
		out.Caps = t.walker.Caps()
	}
	if len(t.visited) > 0 {
		// I11: the components and architecture edges of every reached node.
		t.note("I11", "")
	}
	for _, r := range Relations {
		if nodes, ok := t.notExtracted[r.ID]; ok {
			ne := NotExtracted{Relation: r.ID, Nodes: []string{}}
			for n := range nodes {
				ne.Nodes = append(ne.Nodes, n)
			}
			sort.Strings(ne.Nodes)
			out.Unknowns.NotExtracted = append(out.Unknowns.NotExtracted, ne)
		}
	}
	if err := t.tableUses(out); err != nil {
		return nil, err
	}
	if err := t.blindSpots(out); err != nil {
		return nil, err
	}
	if err := t.nearMisses(out); err != nil {
		return nil, err
	}
	return out, nil
}

// markWithheld records in withheld the reached routes of graph that are
// registrations whose path the Go linker withholds (row CR-44); the
// diagnostics are read only when some route has no full path.
func (t *traversal) markWithheld(graph string, nodes []contract.Node, withheld map[string]bool) error {
	may := false
	for _, n := range nodes {
		may = may || routes.MayBeWithheld(n)
	}
	if !may {
		return nil
	}
	diagnostics, err := t.s.DiagnosticsWithCodes(t.ctx, graph, []string{routes.WithheldCode}, nil)
	if err != nil {
		return fmt.Errorf("reading withheld route registrations: %w", err)
	}
	sites := routes.WithheldSites(diagnostics)
	for _, n := range nodes {
		if routes.PathWithheld(n, sites) {
			withheld[n.Identity] = true
		}
	}
	return nil
}

// routePaths sets RoutePaths on every path that reaches a route, from the
// route node in the graph it was read from. A route the graph no longer
// holds keeps RoutePaths nil: its full path is not known.
func (t *traversal) routePaths(out *ImpactResult) error {
	byGraph := map[string][]string{}
	for _, p := range out.Paths {
		if p.Kind == "route" {
			byGraph[t.graphs[p.Node]] = append(byGraph[t.graphs[p.Node]], p.Node)
		}
	}
	found := map[string][]RoutePath{}
	prefixed := map[string][]routes.PrefixUnknownPath{}
	shadowed := map[string]bool{}
	withheldRoutes := map[string]bool{}
	for graph, ids := range byGraph {
		nodes, err := t.s.NodesByIdentity(t.ctx, graph, ids)
		if err != nil {
			return fmt.Errorf("reading reached routes: %w", err)
		}
		if err := t.markWithheld(graph, nodes, withheldRoutes); err != nil {
			return err
		}
		for _, n := range nodes {
			list := []RoutePath{}
			entries, _ := n.Attributes["paths"].([]any)
			for _, e := range entries {
				m, ok := e.(map[string]any)
				if !ok || !routes.FullPathEntry(m) {
					continue
				}
				var rp RoutePath
				if s, ok := m["template"].(string); ok {
					rp.Template = &s
				}
				if s, ok := m["scope"].(string); ok {
					rp.Scope = &s
				}
				rp.TrailingSlash = m["trailing_slash"] == true
				list = append(list, rp)
			}
			found[n.Identity] = list
			if withheld, _ := n.Attributes[routes.ShadowedAttribute].([]any); len(list) == 0 && len(withheld) > 0 {
				shadowed[n.Identity] = true
			}
			if p := routes.PrefixUnknownPaths(n.Attributes); len(p) > 0 {
				prefixed[n.Identity] = p
			}
		}
	}
	for i := range out.Paths {
		if list, ok := found[out.Paths[i].Node]; ok && out.Paths[i].Kind == "route" {
			out.Paths[i].RoutePaths = &list
		}
		if shadowed[out.Paths[i].Node] && out.Paths[i].Kind == "route" {
			out.Paths[i].RouteShadowed = true
		}
		if withheldRoutes[out.Paths[i].Node] && out.Paths[i].Kind == "route" {
			out.Paths[i].RouteWithheld = true
		}
		if list, ok := prefixed[out.Paths[i].Node]; ok && out.Paths[i].Kind == "route" {
			out.Paths[i].PrefixUnknownPaths = &list
		}
	}
	return nil
}

// linkAssumptions sets LinkAssumptions on every calls_route step of the
// paths, from the step's client call in the graph it was read from (the
// head's when the walk did not record one). A call the graph no longer
// holds leaves the step without them.
func (t *traversal) linkAssumptions(out *ImpactResult) error {
	graphOf := func(s Step) string {
		for _, id := range []string{s.Source, s.Target} {
			if g := t.graphs[id]; g != "" {
				return g
			}
		}
		return t.c.graph[head]
	}
	byGraph := map[string][]string{}
	for _, p := range out.Paths {
		for _, s := range p.Steps {
			if s.Kind == "calls_route" && s.Assumption == "" {
				byGraph[graphOf(s)] = append(byGraph[graphOf(s)], s.Source)
			}
		}
	}
	calls := map[[2]string]map[string]any{}
	for graph, ids := range byGraph {
		nodes, err := t.s.NodesByIdentity(t.ctx, graph, ids)
		if err != nil {
			return fmt.Errorf("reading the client calls of reached links: %w", err)
		}
		for _, n := range nodes {
			calls[[2]string{graph, n.Identity}] = n.Attributes
		}
	}
	for i := range out.Paths {
		for j := range out.Paths[i].Steps {
			s := &out.Paths[i].Steps[j]
			if s.Kind != "calls_route" || s.Assumption != "" {
				continue
			}
			if attrs, ok := calls[[2]string{graphOf(*s), s.Source}]; ok {
				if list := clients.LinkAssumptions(attrs, s.Target); len(list) > 0 {
					s.LinkAssumptions = list
				}
			}
		}
	}
	return nil
}

// byGraph groups the visited nodes by the graph they were read from.
func (t *traversal) byGraph() map[string][]string {
	out := map[string][]string{}
	for id, g := range t.graphs {
		out[g] = append(out[g], id)
	}
	for g := range out {
		sort.Strings(out[g])
	}
	return out
}

// blindSpots lists the unresolved call sites (ADR 0009) of every file of
// the graph whose callee's simple name matches a seed or reached function,
// method or class. Like graphvein impact it scans all file nodes: a caller
// the traversal never reached is exactly where a blind spot hides
// (Revision 3, R3-2).
func (t *traversal) blindSpots(out *ImpactResult) error {
	for graph, ids := range t.byGraph() {
		names := map[string][]string{}
		for _, id := range ids {
			kind, _, qualified, err := contract.ParseIdentity(id)
			if err != nil {
				continue
			}
			if kind == "function" || kind == "method" || kind == "class" {
				name := simpleName(qualified)
				names[name] = append(names[name], id)
			}
		}
		if len(names) == 0 {
			continue
		}
		index, err := t.c.blindIndex(t.ctx, t.s, graph)
		if err != nil {
			return err
		}
		var matched []rawSite
		for name := range names {
			matched = append(matched, index[name]...)
		}
		// Scan order first, so the stable sort below keeps the order a
		// scan of the file nodes gives among sites at the same position.
		sort.Slice(matched, func(i, j int) bool { return matched[i].ordinal < matched[j].ordinal })
		for _, m := range matched {
			data, err := json.Marshal(m.raw)
			if err != nil {
				return fmt.Errorf("reading blind spots of %s: %w", m.file, err)
			}
			var site query.MissingSite
			if err := json.Unmarshal(data, &site); err != nil {
				return fmt.Errorf("reading blind spots of %s: %w", m.file, err)
			}
			site.File = m.file
			site.Matches = append([]string{}, names[m.name]...)
			sort.Strings(site.Matches)
			out.Unknowns.BlindSpots = append(out.Unknowns.BlindSpots, site)
		}
	}
	sort.SliceStable(out.Unknowns.BlindSpots, func(i, j int) bool {
		a, b := out.Unknowns.BlindSpots[i], out.Unknowns.BlindSpots[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Span.StartLine != b.Span.StartLine {
			return a.Span.StartLine < b.Span.StartLine
		}
		return a.Span.StartColumn < b.Span.StartColumn
	})
	return nil
}

// rawSite is one stored unresolved call site with a callee name, kept
// undecoded until a traversal matches it.
type rawSite struct {
	file, name string
	ordinal    int
	raw        any
}

// indexBuild is one graph's per-comparison index: the file index or the
// I8 candidate index. Whoever asks first builds it (Prepare in the
// background, or a traversal); the others wait on done instead of reading
// the nodes again.
type indexBuild[T any] struct {
	done  chan struct{}
	index T
	err   error
}

// errBuildAbandoned tells a waiting traversal that the build it waited on
// stopped because its owner's context ended (a newer run, or the server
// stopping), so it builds the index itself with its own context.
var errBuildAbandoned = errors.New("index build abandoned")

// Prepare builds the comparison's per-graph indexes ahead of the first
// traversal, which would otherwise pay for them: the file index reads
// every file node (about 0.5 s on Langflow), the I8 candidate index adds
// every code symbol node (about 0.65 s more). The candidate index is built
// only when some item is a table or column, since only those seeds reach
// a table. The server runs Prepare in the background right after a
// comparison is computed; a traversal that starts meanwhile waits for the
// index it needs. It returns when the indexes are built or ctx ends.
func (c *Comparison) Prepare(ctx context.Context, s store.Store) error {
	tables := c.hasTableItems()
	// The head graph first: only removed seeds traverse the base graph.
	for _, graph := range []string{c.graph[head], c.graph[base]} {
		if graph == "" {
			continue
		}
		if _, err := c.fileIndex(ctx, s, graph); err != nil {
			return err
		}
		if !tables {
			continue
		}
		if _, err := c.unknownUses(ctx, s, graph); err != nil {
			return err
		}
	}
	return nil
}

// hasTableItems reports whether some item is a table or column node, the
// only seeds whose traversal reaches a table (I8 starts at a table, I7's
// foreign keys lead from a column to columns).
func (c *Comparison) hasTableItems() bool {
	for i := range c.items {
		it := &c.items[i]
		if it.Record == store.RecordNode && (it.Kind == "table" || it.Kind == "column") {
			return true
		}
	}
	return false
}

// buildOnce returns graph's index from builds, building it with read when
// no build exists yet and waiting on the running one otherwise. A failed
// build is not kept, so a later call reads again; a build abandoned by
// its owner's context is retried with the caller's. mu guards builds.
func buildOnce[T any](ctx context.Context, mu *sync.Mutex, builds map[string]*indexBuild[T], graph, what string, read func(context.Context) (T, error)) (T, error) {
	for {
		mu.Lock()
		b, ok := builds[graph]
		if !ok {
			b = &indexBuild[T]{done: make(chan struct{})}
			builds[graph] = b
			mu.Unlock()
			index, err := read(ctx)
			if err != nil {
				mu.Lock()
				delete(builds, graph)
				mu.Unlock()
				b.err = err
				if ctx.Err() != nil {
					b.err = errBuildAbandoned
				}
			}
			b.index = index
			close(b.done)
			return index, err
		}
		mu.Unlock()
		select {
		case <-b.done:
		case <-ctx.Done():
			var zero T
			return zero, fmt.Errorf("%s of %s: %w", what, graph, ctx.Err())
		}
		if errors.Is(b.err, errBuildAbandoned) {
			continue
		}
		return b.index, b.err
	}
}

// fileIndex is what a comparison keeps of one graph's file nodes, read
// once for both indexes that need them.
type fileIndex struct {
	// sites are the unresolved call sites by callee name (blind spots).
	sites map[string][]rawSite
	// unknowns are the file nodes' table_access_unknown entries (I8),
	// the start of the candidate index.
	unknowns *UnknownUses
}

// fileIndex returns graph's file index, read once per comparison:
// decoding every site of a large repository on each traversal cost about
// 0.9 s on Langflow.
func (c *Comparison) fileIndex(ctx context.Context, s store.Store, graph string) (*fileIndex, error) {
	return buildOnce(ctx, &c.mu, c.files, graph, "reading blind spots", func(ctx context.Context) (*fileIndex, error) {
		return readFileIndex(ctx, s, graph)
	})
}

// blindIndex returns graph's unresolved call sites by callee name.
func (c *Comparison) blindIndex(ctx context.Context, s store.Store, graph string) (map[string][]rawSite, error) {
	fi, err := c.fileIndex(ctx, s, graph)
	if err != nil {
		return nil, err
	}
	return fi.sites, nil
}

// readFileIndex reads graph's file nodes and indexes their unresolved call
// sites by callee name and their table_access_unknown entries by candidate
// table. Sites without a name are left out, as no seed can match them.
func readFileIndex(ctx context.Context, s store.Store, graph string) (*fileIndex, error) {
	nodes, err := s.Nodes(ctx, graph, store.Query{Kind: "file"})
	if err != nil {
		return nil, fmt.Errorf("reading blind spots of %s: %w", graph, err)
	}
	index := map[string][]rawSite{}
	unknowns := newUnknownUses()
	ordinal := 0
	for _, n := range nodes {
		unknowns.add(&n)
		raw, ok := n.Attributes["unresolved_unknown_call_sites"]
		if !ok {
			continue
		}
		list, ok := raw.([]any)
		if !ok && raw != nil {
			return nil, fmt.Errorf("reading blind spots of %s: unresolved_unknown_call_sites is not a list", n.File)
		}
		for _, item := range list {
			site, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("reading blind spots of %s: a call site is not an object", n.File)
			}
			value, present := site["name"]
			if !present || value == nil {
				continue
			}
			name, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("reading blind spots of %s: a call site name is not a string", n.File)
			}
			index[name] = append(index[name], rawSite{file: n.File, name: name, ordinal: ordinal, raw: item})
			ordinal++
		}
	}
	return &fileIndex{sites: index, unknowns: unknowns}, nil
}

func simpleName(q string) string {
	if i := indexByte(q, '@'); i >= 0 {
		q = q[:i]
	}
	for i := len(q) - 1; i >= 0; i-- {
		if q[i] == '.' {
			return q[i+1:]
		}
	}
	return q
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// nearMisses lists, for every seed or reached route, the client calls
// whose ambiguous or test-only path entries list it as a candidate (the
// ADR 0042 rule: only those statuses record candidates).
func (t *traversal) nearMisses(out *ImpactResult) error {
	for graph, ids := range t.byGraph() {
		routes := map[string]bool{}
		for _, id := range ids {
			if identityKind(id) == "route" {
				routes[id] = true
			}
		}
		if len(routes) == 0 {
			continue
		}
		calls, err := t.s.Nodes(t.ctx, graph, store.Query{Kind: "client_call"})
		if err != nil {
			return fmt.Errorf("reading near misses: %w", err)
		}
		for _, call := range calls {
			paths, _ := call.Attributes["paths"].([]any)
			for _, p := range paths {
				m, _ := p.(map[string]any)
				status, _ := m["status"].(string)
				if status != "ambiguous" && status != "test_only" {
					continue
				}
				cands, _ := m["candidates"].([]any)
				for _, cand := range cands {
					if id, ok := cand.(string); ok && routes[id] {
						out.Unknowns.NearMisses = append(out.Unknowns.NearMisses, NearMiss{Route: id, Call: call.Identity, Status: status})
					}
				}
			}
		}
	}
	sort.SliceStable(out.Unknowns.NearMisses, func(i, j int) bool {
		a, b := out.Unknowns.NearMisses[i], out.Unknowns.NearMisses[j]
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Call < b.Call
	})
	return nil
}

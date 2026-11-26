package changes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/store"
)

// rowCode is the code of one contract row: how it classifies one node or
// edge record, or a pass over the whole comparison. rowCodes holds every C
// and D row exactly once (TestChangeClaimContract), so the table in
// contract.go and the code stay 1:1.
type rowCode struct {
	nodes func(*classifier, *nodeRec)
	edges func(*classifier, *edgeRec)
	pass  func(*classifier) error
}

var rowCodes = map[string]rowCode{
	"C1":  {nodes: (*classifier).fileNode, pass: (*classifier).fileRows},
	"C2":  {nodes: (*classifier).symbol, edges: (*classifier).tableAccessEdge},
	"C3":  {nodes: (*classifier).route, edges: (*classifier).routeEdge},
	"C4":  {nodes: (*classifier).clientCall},
	"C5":  {edges: (*classifier).routeLink},
	"C6":  {nodes: (*classifier).operation, edges: (*classifier).operationEdge},
	"C7":  {nodes: (*classifier).ipc, edges: (*classifier).ipcEdge},
	"C8":  {nodes: (*classifier).migrationTable},
	"C9":  {nodes: (*classifier).migrationColumn, edges: (*classifier).foreignKey},
	"C10": {nodes: (*classifier).migration, edges: (*classifier).altersEdge},
	"C11": {nodes: (*classifier).modelRecord, edges: (*classifier).foreignKey},
	"C12": {nodes: (*classifier).uiRoute, edges: (*classifier).presence},
	"C13": {pass: (*classifier).architecture},
	"C14": {pass: (*classifier).moveHints},
	"C15": {pass: (*classifier).gapRows},
	"C16": {nodes: (*classifier).otherNode, edges: (*classifier).presence},
	"C17": {nodes: (*classifier).reactRoute, edges: (*classifier).presence},
	"C18": {nodes: (*classifier).serverAction, edges: (*classifier).actionEdge},
	"D":   {nodes: (*classifier).unclassifiedNode, edges: (*classifier).unclassifiedEdge},
}

// symbolKinds are C2's code symbol kinds.
var symbolKinds = map[string]bool{
	"function": true, "method": true, "class": true, "struct": true, "enum": true,
	"interface": true, "trait": true, "type": true, "type_alias": true, "union": true,
	"macro": true, "variable": true, "constant": true,
}

// angularEdges are C12's Angular structure edge kinds.
var angularEdges = map[string]bool{
	"routes_to": true, "loads_children": true, "renders": true, "injects": true,
	"uses_pipe": true, "uses_template": true, "declares": true,
	"scope_imports": true, "scope_exports": true,
}

// nodeRow assigns a node record to the one row that covers its form.
func nodeRow(n *contract.Node) string {
	model := n.Attributes["source"] == "model"
	switch {
	case n.Kind == "file":
		return "C1"
	case symbolKinds[n.Kind]:
		return "C2"
	case n.Kind == "route":
		return "C3"
	case n.Kind == "client_call":
		return "C4"
	case n.Kind == "api_operation":
		return "C6"
	case n.Kind == "ipc_command" || n.Kind == "ipc_call":
		return "C7"
	case n.Kind == "server_action" || n.Kind == "action_call":
		return "C18"
	case (n.Kind == "table" || n.Kind == "column") && model:
		return "C11"
	case n.Kind == "table":
		return "C8"
	case n.Kind == "column" || n.Kind == "db_index":
		return "C9"
	case n.Kind == "migration":
		return "C10"
	case n.Kind == "ui_route" && n.Attributes["framework"] == "react-router":
		return "C17"
	case n.Kind == "ui_route":
		return "C12"
	case n.Kind == "external" || n.Kind == "api_spec":
		return "C16"
	}
	return "D"
}

// edgeRow assigns an edge record to its row. A foreign key belongs to C11
// when its source column is a model column on either side, else to C9.
// exposes_route edges are reported on their route (C3) and alters edges on
// their migration (C10), Revision 1. reads_table and writes_table edges
// (T019 Revision 4) are reported on their owner's C2 item when the owner
// is a code symbol, else (a file node owner) they are C16 edges.
func (c *classifier) edgeRow(e *contract.Edge) string {
	switch {
	case tableAccessKinds[e.Kind] && symbolKinds[identityKind(e.Source)]:
		return "C2"
	case tableAccessKinds[e.Kind]:
		return "C16"
	case e.Kind == "exposes_route" || e.Kind == "uses_model":
		return "C3"
	case e.Kind == "alters":
		return "C10"
	case e.Kind == "calls_route":
		return "C5"
	case e.Kind == "documents_route" || e.Kind == "calls_operation":
		return "C6"
	case e.Kind == "exposes_command" || e.Kind == "invokes_command":
		return "C7"
	case e.Kind == "exposes_action" || e.Kind == "calls_action":
		return "C18"
	case e.Kind == "foreign_key":
		for side := base; side <= head; side++ {
			if n := c.node(side, e.Source); n != nil && n.Attributes["source"] == "model" {
				return "C11"
			}
		}
		return "C9"
	case c.reactEdge(e):
		return "C17"
	case angularEdges[e.Kind]:
		return "C12"
	case e.Kind == "imports" || e.Kind == "contains" || e.Kind == "calls" || e.Kind == "passes_function":
		return "C16"
	}
	return "D"
}

// contextEdgeKinds are the edges reported on an owner node's item
// (Revision 1) rather than as items of their own.
var contextEdgeKinds = map[string]bool{"exposes_route": true, "uses_model": true, "alters": true, "reads_table": true, "writes_table": true}

// namedKind reports whether some C row names records of this kind, so
// that only undecodable payloads of such a kind may take row D (Revision
// 1: D keeps only the forms no row names).
func namedKind(record, kind string) bool {
	switch record {
	case store.RecordFile:
		return true
	case store.RecordNode:
		return nodeRow(&contract.Node{Kind: kind}) != "D"
	case store.RecordEdge:
		return (&classifier{}).edgeRow(&contract.Edge{Kind: kind}) != "D"
	}
	return false
}

// seedRows are the rows whose items start an impact traversal.
var seedRows = map[string]bool{
	"C2": true, "C3": true, "C4": true, "C5": true, "C6": true, "C7": true, "C8": true,
	"C9": true, "C10": true, "C11": true, "C12": true, "C13": true, "C16": true, "C17": true, "C18": true,
}

type classifier struct {
	ctx context.Context
	s   store.Store
	opt Options
	cmp *Comparison

	files map[string]*fileRec
	nodes map[string]*nodeRec
	edges map[string]*edgeRec
	// graphNodes are unchanged records read from the head graph (equal on
	// both sides).
	graphNodes map[string]*contract.Node
	// parents maps a column or index identity to its table per side.
	parents [2]map[string]string
	lists   [2]map[string]string
	// nodeItems are the node items by identity, for the link rows.
	nodeItems map[string]*Item
	groups    map[string]*GroupCount
	items     []*Item
	// headPaths is the head commit's tree of a ref head, read once (G8).
	headPaths    map[string]bool
	headPathsErr error
}

func (c *classifier) group(name string) *GroupCount {
	g, ok := c.groups[name]
	if !ok {
		g = &GroupCount{Group: name, Changes: map[string]int{}, Causes: map[string]int{}, Kinds: map[string]int{}}
		c.groups[name] = g
	}
	return g
}

func (c *classifier) run() error {
	c.files, c.nodes, c.edges = map[string]*fileRec{}, map[string]*nodeRec{}, map[string]*edgeRec{}
	c.graphNodes, c.nodeItems, c.groups = map[string]*contract.Node{}, map[string]*Item{}, map[string]*GroupCount{}
	set := c.cmp.set
	var nodeOrder, edgeOrder []string
	for _, rc := range set.Changes {
		switch rc.Record {
		case store.RecordFile:
			c.files[rc.Key] = decodeFile(rc)
		case store.RecordNode:
			c.nodes[rc.Key] = decodeNode(rc)
			nodeOrder = append(nodeOrder, rc.Key)
		case store.RecordEdge:
			c.edges[rc.Key] = decodeEdge(rc)
			edgeOrder = append(edgeOrder, rc.Key)
		}
	}
	if err := c.prefetch(); err != nil {
		return err
	}
	if err := c.loadLists(); err != nil {
		return err
	}
	for _, key := range nodeOrder {
		r := c.nodes[key]
		n := r.any()
		if n != nil && (n.Level == 5 || r.side[base] != nil && r.side[base].Level == 5) {
			c.cmp.Summary.Locations++
			continue
		}
		row := "D"
		if !r.bad && n != nil {
			row = nodeRow(n)
		}
		rowCodes[row].nodes(c, r)
	}
	// Edges reported on their owner node (exposes_route, alters) go first,
	// so the link rows see the owner items they create or change.
	for _, context := range []bool{true, false} {
		for _, key := range edgeOrder {
			r := c.edges[key]
			row := "D"
			e := r.any()
			if !r.bad && e != nil {
				row = c.edgeRow(e)
			}
			if (row != "D" && contextEdgeKinds[e.Kind]) != context {
				continue
			}
			rowCodes[row].edges(c, r)
		}
	}
	for _, row := range Rows {
		if code := rowCodes[row.ID]; code.pass != nil {
			if err := code.pass(c); err != nil {
				return err
			}
		}
	}
	if err := c.markWithheld(); err != nil {
		return err
	}
	c.finish()
	return nil
}

// markWithheld sets PathWithheld on the sides of route items that are
// registrations whose path the Go linker withholds (couldn't-read row
// CR-44, ADR 0089 Revision 5), from the stored diagnostics of the graph
// holding that side: the head graph, and the base only for a ref base (a
// journal run keeps no diagnostics, so its side is never marked and reads
// "path not known").
func (c *classifier) markWithheld() error {
	graphs := map[int]string{head: c.cmp.graph[head]}
	if c.cmp.set.Base.Kind == store.SideRef {
		graphs[base] = c.cmp.graph[base]
	}
	for _, side := range []int{base, head} {
		graph, ok := graphs[side]
		if !ok {
			continue
		}
		var states []*State
		var nodes []*contract.Node
		for _, it := range c.items {
			if it.Record != "node" || it.Kind != "route" {
				continue
			}
			r := c.nodes[it.Identity]
			st := it.Before
			if side == head {
				st = it.After
			}
			if r == nil || st == nil || r.side[side] == nil || !routes.MayBeWithheld(*r.side[side]) {
				continue
			}
			states, nodes = append(states, st), append(nodes, r.side[side])
		}
		if len(states) == 0 {
			continue
		}
		diagnostics, err := c.s.DiagnosticsWithCodes(c.ctx, graph, []string{routes.WithheldCode}, nil)
		if err != nil {
			return fmt.Errorf("reading withheld route registrations: %w", err)
		}
		sites := routes.WithheldSites(diagnostics)
		for i, st := range states {
			st.PathWithheld = routes.PathWithheld(*nodes[i], sites)
		}
	}
	return nil
}

// prefetch reads, from the head graph, the unchanged nodes the rows need
// besides the change set: edge endpoints and the tables of changed columns
// and indexes.
func (c *classifier) prefetch() error {
	c.parents = [2]map[string]string{{}, {}}
	var schemaIDs []string
	for id, r := range c.nodes {
		if n := r.any(); n != nil && (n.Kind == "column" || n.Kind == "db_index") {
			schemaIDs = append(schemaIDs, id)
		}
	}
	for _, r := range c.edges {
		if e := r.any(); e != nil && e.Kind == "foreign_key" {
			schemaIDs = append(schemaIDs, e.Source, e.Target)
		}
	}
	for _, r := range c.edges {
		for side, e := range r.side {
			if e != nil && e.Kind == "contains" {
				c.parents[side][e.Target] = e.Source
			}
		}
	}
	if len(schemaIDs) > 0 {
		sort.Strings(schemaIDs)
		contains, err := c.s.Edges(c.ctx, c.cmp.graph[head], schemaIDs, []string{"contains"}, store.Incoming)
		if err != nil {
			return fmt.Errorf("reading tables of changed columns: %w", err)
		}
		for _, e := range contains {
			key := store.EdgeKey(e.Kind, e.Source, e.Target)
			if _, changed := c.edges[key]; changed {
				continue
			}
			for side := base; side <= head; side++ {
				if _, ok := c.parents[side][e.Target]; !ok {
					c.parents[side][e.Target] = e.Source
				}
			}
		}
	}
	need := map[string]bool{}
	for _, r := range c.edges {
		if e := r.any(); e != nil {
			need[e.Source], need[e.Target] = true, true
		}
	}
	for side := base; side <= head; side++ {
		for _, t := range c.parents[side] {
			need[t] = true
		}
	}
	var ids []string
	for id := range need {
		if _, changed := c.nodes[id]; !changed {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids)
	nodes, err := c.s.NodesByIdentity(c.ctx, c.cmp.graph[head], ids)
	if err != nil {
		return fmt.Errorf("reading unchanged endpoints: %w", err)
	}
	for i := range nodes {
		c.graphNodes[nodes[i].Identity] = &nodes[i]
	}
	return nil
}

// loadLists reads the table_list of each schema group for the sides that
// have a graph of their own: the head, and the base of a ref comparison. A
// run or pin base has no stored diagnostics, so its lists stay unknown.
func (c *classifier) loadLists() error {
	if c.opt.Schema == nil {
		return nil
	}
	schema := false
	for _, r := range c.nodes {
		if n := r.any(); n != nil && (n.Kind == "table" || n.Kind == "column" || n.Kind == "db_index") {
			schema = true
			break
		}
	}
	if !schema {
		return nil
	}
	sides := []int{head}
	if c.cmp.set.Base.Kind == store.SideRef {
		sides = append(sides, base)
	}
	for _, side := range sides {
		lists, err := c.opt.Schema.TableLists(c.ctx, c.s, c.cmp.graph[side])
		if err != nil {
			return fmt.Errorf("reading schema table lists: %w", err)
		}
		c.lists[side] = lists
	}
	return nil
}

// node is the payload of id on side: from the change set when id changed
// (nil when absent there), else from the head graph.
func (c *classifier) node(side int, id string) *contract.Node {
	if r, ok := c.nodes[id]; ok {
		return r.side[side]
	}
	return c.graphNodes[id]
}

// contentChanged reports whether a footprint file's content differs
// between the sides (G4).
func (c *classifier) contentChanged(footprint []string) bool {
	for _, f := range footprint {
		r, ok := c.files[f]
		if !ok {
			continue
		}
		if r.bad || (r.side[base] == nil) != (r.side[head] == nil) {
			return true
		}
		if r.side[base] != nil && r.side[base].ContentHash != r.side[head].ContentHash {
			return true
		}
	}
	return false
}

// cause applies G4 to a footprint.
func (c *classifier) cause(footprint []string) *string {
	switch {
	case c.contentChanged(footprint):
		return ptr(CauseCode)
	case c.extractorChanged():
		return ptr(CauseExtractor)
	}
	return ptr(CauseRelink)
}

func (c *classifier) extractorChanged() bool {
	return c.cmp.set.Base.Run.Indexer != c.cmp.set.Head.Run.Indexer
}

// removalWording applies G8 to records other than files: they are no
// longer in the index; only a file's absence can be proven.
func (c *classifier) removalWording(_ bool) *string {
	return ptr("no_longer_in_index")
}

// fileRemoval applies G8 to a removed file (Revision 3, R3-4): "deleted"
// only when the head commit's tree (a ref head) or the working tree (the
// current index of opt.Root) lacks the path. A path still present was
// excluded from the index (ignore rules, size limit, language): it reads
// "no_longer_in_index" with reason "excluded". Without a tree to look at
// the wording stays "no_longer_in_index" and no reason is claimed.
func (c *classifier) fileRemoval(path string) (wording, reason *string) {
	present, known := c.headHas(path)
	switch {
	case !known:
		return ptr("no_longer_in_index"), nil
	case present:
		return ptr("no_longer_in_index"), ptr(ReasonExcluded)
	}
	return ptr("deleted"), nil
}

// headHas reports whether the head's tree has path, and whether that is
// known at all.
func (c *classifier) headHas(path string) (present, known bool) {
	switch {
	case c.cmp.set.Head.Kind == store.SideRef && c.cmp.Refs.Head != "":
		if c.headPaths == nil && c.headPathsErr == nil {
			c.headPaths, c.headPathsErr = gitref.Paths(c.ctx, c.opt.Root, c.cmp.Refs.Head)
		}
		if c.headPathsErr != nil {
			return false, false
		}
		return c.headPaths[path], true
	case c.cmp.set.Head.Kind == store.SideCurrent && c.opt.Root != "":
		if !contract.ValidPath(path, false) {
			return false, false
		}
		_, err := os.Lstat(filepath.Join(c.opt.Root, filepath.FromSlash(path)))
		switch {
		case err == nil:
			return true, true
		case errors.Is(err, fs.ErrNotExist):
			return false, true
		}
	}
	return false, false
}

func ptr[T any](v T) *T { return &v }

func rawOf(r store.RecordChange) Raw {
	out := Raw{}
	if r.Before != nil {
		out.Before = r.Before
	}
	if r.After != nil {
		out.After = r.After
	}
	return out
}

// nodeSpec describes a row's user-level fields for nodes.
type nodeSpec struct {
	row, group string
	// names lists the compared fields; context fields are shown only.
	names   func(r *nodeRec) []string
	context []string
	// unknown reports a value that counts as unknown for its field.
	unknown func(name string, v any) bool
}

func fixed(names ...string) func(*nodeRec) []string {
	return func(*nodeRec) []string { return names }
}

func isNull(_ string, v any) bool { return v == nil || v == "unknown" }

// value reads a field: content_hash is the record's, completeness is the
// table's under the Schema rule, everything else an attribute.
func (c *classifier) value(n *contract.Node, name string) any {
	switch name {
	case "content_hash":
		return n.ContentHash
	case "completeness":
		return c.completeness(n)
	}
	return n.Attributes[name]
}

func (c *classifier) completeness(n *contract.Node) string {
	if n == nil || c.opt.Schema == nil {
		return "unknown"
	}
	return c.opt.Schema.TableCompleteness(*n)
}

func (c *classifier) state(n *contract.Node, spec nodeSpec, names []string) *State {
	if n == nil {
		return nil
	}
	st := &State{File: n.File, Span: n.Span, Fields: map[string]any{}}
	for _, name := range append(append([]string{}, names...), spec.context...) {
		st.Fields[name] = c.value(n, name)
	}
	return st
}

// diffFields compares the named fields of both sides.
func (c *classifier) diffFields(b, h *contract.Node, names []string, unknown func(string, any) bool) []Field {
	out := []Field{}
	for _, name := range names {
		bv, hv := c.value(b, name), c.value(h, name)
		if canonical(bv) == canonical(hv) {
			continue
		}
		change := Changed
		switch bu, hu := unknown(name, bv), unknown(name, hv); {
		case !bu && hu:
			change = UnknownNow
		case bu && !hu:
			change = KnownNow
		}
		out = append(out, Field{Name: name, Change: change, Before: bv, After: hv})
	}
	return out
}

// classifyNode applies the generic part of a row to a node record: moves
// and other changes are counted, G3 downgrades, G4 causes, G7 scope.
// It returns nil when nothing is listed.
func (c *classifier) classifyNode(r *nodeRec, spec nodeSpec) *Item {
	b, h := r.side[base], r.side[head]
	n := r.any()
	g := c.group(spec.group)
	fp := nodeFootprint(r)
	knows := [2]bool{c.cmp.know[base].knows(fp), c.cmp.know[head].knows(fp)}
	names := spec.names(r)
	it := &Item{
		Group: spec.group, Row: spec.row, Record: store.RecordNode, Identity: r.rc.Key,
		Kind: n.Kind, Name: n.QualifiedName, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
		Before: c.state(b, spec, names), After: c.state(h, spec, names),
		footprint: fp, seed: seedRows[spec.row], raw: rawOf(r.rc),
	}
	switch {
	case b != nil && h != nil:
		if projectNode(b) == projectNode(h) {
			g.Moves++
			return nil
		}
		it.Fields = c.diffFields(b, h, names, spec.unknown)
		switch {
		case len(it.Fields) > 0:
			it.Change = Changed
		case b.ContentHash != h.ContentHash:
			it.Change = BodyChanged
		default:
			g.OtherChanges++
			return nil
		}
		if !knows[base] || !knows[head] {
			it.Change, it.Reason = UnknownNow, ptr("gap")
		}
	case b != nil:
		it.Change = Removed
		it.Wording = c.removalWording(false)
		presenceG3(it, knows)
	default:
		it.Change = Added
		presenceG3(it, knows)
	}
	it.Cause = c.cause(fp)
	it.Test = scopeTest(b) || scopeTest(h)
	return it
}

// presenceG3 applies G3 symmetrically to an added or removed item
// (Revision 3, R3-7): the claim stands only when both sides know the
// fact's files. A head that does not know makes it unknown_now; a base that
// does not know (the head does) makes it known_now. Either way the item
// has reason gap and no removal wording.
func presenceG3(it *Item, knows [2]bool) {
	switch {
	case !knows[head]:
		it.Change, it.Reason, it.Wording = UnknownNow, ptr("gap"), nil
	case !knows[base]:
		it.Change, it.Reason, it.Wording = KnownNow, ptr("gap"), nil
	}
}

func scopeTest(n *contract.Node) bool { return n != nil && n.Attributes["scope"] == "test" }

func (c *classifier) add(it *Item) {
	if it == nil {
		return
	}
	c.items = append(c.items, it)
	if it.Record == store.RecordNode {
		c.nodeItems[it.Identity] = it
	}
}

// C2: code symbols compare their content hash only (body_changed).
func (c *classifier) symbol(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C2", group: GroupCode, names: fixed(), unknown: isNull}))
}

// C3: routes. A pattern or path list that becomes unresolved (no composed
// path) is unknown_now for that field.
func (c *classifier) route(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C3", group: GroupRoutes,
		names: routeNames,
		unknown: func(name string, v any) bool {
			if name == "paths" || name == routeUnknownPrefix {
				list, _ := v.([]any)
				return len(list) == 0
			}
			return isNull(name, v)
		}}))
}

// C4: client calls. A call whose status becomes dynamic is unknown_now.
func (c *classifier) clientCall(r *nodeRec) {
	it := c.classifyNode(r, nodeSpec{row: "C4", group: GroupClientCalls,
		names: fixed(callFields...),
		unknown: func(name string, v any) bool {
			return isNull(name, v) || name == "status" && v == "dynamic"
		}})
	if it != nil && len(it.Fields) > 0 && c.linkTargetGap(it.Identity) {
		// R3-8: a resolution field (paths, status) of a call whose linked
		// route lies in a gap file is downgraded like its C5 link.
		for i := range it.Fields {
			if resolutionFields[it.Fields[i].Name] && it.Fields[i].Change == Changed {
				it.Fields[i].Change = UnknownNow
			}
		}
		allUnknown := len(it.Fields) > 0
		for _, f := range it.Fields {
			allUnknown = allUnknown && f.Change == UnknownNow
		}
		if allUnknown {
			it.Change, it.Reason = UnknownNow, ptr("gap")
		}
	}
	c.add(it)
}

// resolutionFields are C4's fields that depend on the route a call links to.
var resolutionFields = map[string]bool{"paths": true, "status": true}

// linkTargetGap reports whether a calls_route target of call (on either
// side, changed or not) lies in a file either side does not know. A failed
// read counts as a gap: it only downgrades.
func (c *classifier) linkTargetGap(call string) bool {
	targets := map[string]bool{}
	for _, r := range c.edges {
		for _, e := range r.side {
			if e != nil && e.Kind == "calls_route" && e.Source == call {
				targets[e.Target] = true
			}
		}
	}
	edges, err := c.s.Edges(c.ctx, c.cmp.graph[head], []string{call}, []string{"calls_route"}, store.Outgoing)
	if err != nil {
		return true
	}
	for _, e := range edges {
		targets[e.Target] = true
	}
	for target := range targets {
		_, file, _, err := contract.ParseIdentity(target)
		if err != nil {
			return true
		}
		if !c.cmp.know[base].knows([]string{file}) || !c.cmp.know[head].knows([]string{file}) {
			return true
		}
	}
	return false
}

// routeFields are C3's user-level fields.
var routeFields = []string{"method", "pattern", "paths", "handler", "framework", "scope"}

// routeNames are the fields C3 compares for one route: routeFields, plus
// unresolved_prefix_paths (T033 row UP-31, ADR 0084) when either side
// stores it. That attribute holds the paths of a route whose prefix the
// code does not fix: gaining it reads known_now, losing it unknown_now,
// any other change changed, so a hop that changes in another function is
// never silent for the route. A route that has it on neither side keeps
// its item as before (no field that only says null on both sides).
func routeNames(r *nodeRec) []string {
	for _, n := range r.side {
		if n == nil {
			continue
		}
		if _, ok := n.Attributes[routeUnknownPrefix]; ok {
			return append(append([]string{}, routeFields[:3]...), append([]string{routeUnknownPrefix}, routeFields[3:]...)...)
		}
	}
	return routeFields
}

// routeUnknownPrefix is the route attribute of the unknown-prefix paths
// (routes.UnknownPrefixAttribute).
const routeUnknownPrefix = "unresolved_prefix_paths"

// migrationFields are C10's user-level fields.
var migrationFields = []string{"content_hash", "applied", "order"}

// callFields are C4's user-level fields: method, the URL as written, its
// composed paths (template, resolution, status, match, candidates), the
// call status and the wrapper.
var callFields = []string{"method", "url_expression", "paths", "status", "wrapper"}

// C6: OpenAPI operations.
func (c *classifier) operation(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C6", group: GroupAPI,
		names: fixed("method", "path", "operation_id", "status"), unknown: isNull}))
}

// C7: IPC commands and calls.
func (c *classifier) ipc(r *nodeRec) {
	names := fixed("command", "handler")
	if r.any().Kind == "ipc_call" {
		names = fixed("command", "status")
	}
	c.add(c.classifyNode(r, nodeSpec{row: "C7", group: GroupIPC, names: names,
		unknown: func(name string, v any) bool { return isNull(name, v) || name == "status" && v == "dynamic" }}))
}

// C18: server actions and their uses (T024b R1), as C7: an action by its
// name and handler, a use by its target action(s), form and boundary.
func (c *classifier) serverAction(r *nodeRec) {
	names := fixed("action", "handler", "form")
	if r.any().Kind == "action_call" {
		names = fixed("action", "alternatives", "form", "boundary", "status")
	}
	c.add(c.classifyNode(r, nodeSpec{row: "C18", group: GroupServerActions, names: names, unknown: isNull}))
}

// actionEdge: C18's exposes_action is presence; calls_action uses C5's
// link logic with the target action in place of method and URL.
func (c *classifier) actionEdge(r *edgeRec) {
	if r.any().Kind == "calls_action" {
		c.add(c.link(r, actionLinkSpec))
		return
	}
	c.add(c.edgeItem(r, "C18", GroupServerActions))
}

// C8: migration-defined tables. Removed only when the head set's table
// list is complete and the set has no gap; added only when the base set's
// list was complete (unknown for run and pin bases, whose diagnostics are
// not journaled).
func (c *classifier) migrationTable(r *nodeRec) {
	it := c.classifyNode(r, nodeSpec{row: "C8", group: GroupMigrationTables,
		names: fixed("name", "set", "completeness"), unknown: isNull})
	c.tableListRule(it, r, true)
	c.add(it)
}

func (c *classifier) tableListRule(it *Item, r *nodeRec, setIsDir bool) {
	if it == nil {
		return
	}
	set, _ := r.any().Attributes["set"].(string)
	switch it.Change {
	case Removed:
		complete := c.lists[head] != nil && c.lists[head][set] == "complete"
		if !complete || setIsDir && set != "" && c.cmp.know[head].gapUnder(set) {
			it.Change, it.Reason, it.Wording = UnknownNow, ptr("table_list_not_complete"), nil
		}
	case Added:
		if c.lists[base] == nil {
			it.Change, it.Reason = KnownNow, ptr("base_table_list_unknown")
		} else if c.lists[base][set] != "complete" {
			it.Change, it.Reason = KnownNow, ptr("base_table_list_not_complete")
		}
	}
}

// claimedBoth are the facts (except exists) claimed on every present side
// of a column or index record (ADR 0055): only they are compared.
func claimedBoth(r *nodeRec) []string {
	var sets []map[string]bool
	for _, n := range r.side {
		if n == nil {
			continue
		}
		set := map[string]bool{}
		claims, _ := n.Attributes["claims"].([]any)
		for _, cl := range claims {
			m, _ := cl.(map[string]any)
			if f, ok := m["fact"].(string); ok && f != "exists" {
				set[f] = true
			}
		}
		sets = append(sets, set)
	}
	out := []string{}
	if len(sets) == 0 {
		return out
	}
	for f := range sets[0] {
		all := true
		for _, s := range sets[1:] {
			all = all && s[f]
		}
		if all {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

// C9: migration-defined columns and indexes: the claimed facts, and added
// or removed only when the table is complete on both sides.
func (c *classifier) migrationColumn(r *nodeRec) {
	spec := nodeSpec{row: "C9", group: GroupMigrationTables, names: claimedBoth, unknown: isNull}
	it := c.classifyNode(r, spec)
	it = c.oneSidedClaims(r, spec, it)
	c.completeTableRule(it, r.rc.Key)
	c.add(it)
}

// oneSidedClaims (R3-10): a fact such as a default that only one side of a
// column or index claims is not compared as a change: first claimed on the
// head it is known_now, claimed only on the base unknown_now. It adds those
// fields to it, creating the item when classifyNode listed nothing.
func (c *classifier) oneSidedClaims(r *nodeRec, spec nodeSpec, it *Item) *Item {
	b, h := r.side[base], r.side[head]
	if b == nil || h == nil {
		return it
	}
	claimed := func(n *contract.Node) map[string]bool {
		set := map[string]bool{}
		claims, _ := n.Attributes["claims"].([]any)
		for _, cl := range claims {
			m, _ := cl.(map[string]any)
			if f, ok := m["fact"].(string); ok && f != "exists" {
				set[f] = true
			}
		}
		return set
	}
	cb, ch := claimed(b), claimed(h)
	var fields []Field
	for _, f := range sortedKeys(ch) {
		if !cb[f] {
			fields = append(fields, Field{Name: f, Change: KnownNow, Before: nil, After: c.value(h, f)})
		}
	}
	for _, f := range sortedKeys(cb) {
		if !ch[f] {
			fields = append(fields, Field{Name: f, Change: UnknownNow, Before: c.value(b, f), After: nil})
		}
	}
	if len(fields) == 0 {
		return it
	}
	if it == nil {
		// classifyNode counted the record as an other change.
		c.group(spec.group).OtherChanges--
		names := spec.names(r)
		it = &Item{Group: spec.group, Row: spec.row, Record: store.RecordNode, Identity: r.rc.Key,
			Kind: h.Kind, Name: h.QualifiedName, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
			Before: c.state(b, spec, names), After: c.state(h, spec, names),
			footprint: nodeFootprint(r), seed: seedRows[spec.row], raw: rawOf(r.rc)}
		it.Change = KnownNow
		it.Cause = c.cause(it.footprint)
		it.Test = scopeTest(b) || scopeTest(h)
	}
	it.Fields = append(it.Fields, fields...)
	if it.Change == KnownNow || it.Change == BodyChanged {
		it.Change = KnownNow
		for _, f := range it.Fields {
			if f.Change == UnknownNow {
				it.Change = UnknownNow
			}
		}
	}
	return it
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// completeTableRule downgrades additions and removals of a table's member
// when the table is not complete on both sides.
func (c *classifier) completeTableRule(it *Item, member string) {
	if it == nil || it.Change != Added && it.Change != Removed {
		return
	}
	for side := base; side <= head; side++ {
		// The table of a member absent on this side is the one it had on
		// the other side, read as it is on this side.
		id := c.parents[side][member]
		if id == "" {
			id = c.parents[1-side][member]
		}
		if c.completeness(c.node(side, id)) != "complete" {
			if it.Change == Added {
				it.Change, it.Reason = KnownNow, ptr("table_not_complete")
			} else {
				it.Change, it.Reason, it.Wording = UnknownNow, ptr("table_not_complete"), nil
			}
			return
		}
	}
}

// C10: migrations: content (edited), applied (the verdict) and order.
func (c *classifier) migration(r *nodeRec) {
	it := c.classifyNode(r, nodeSpec{row: "C10", group: GroupMigrations,
		names: fixed(migrationFields...), unknown: isNull})
	if it != nil {
		for _, f := range it.Fields {
			switch f.Name {
			case "content_hash":
				it.Labels = append(it.Labels, "edited")
			case "applied":
				it.Labels = append(it.Labels, "verdict_changed")
			}
		}
	}
	c.add(it)
}

// C11: model-defined tables and columns: C8 and C9 with the model group's
// table list and the table's state; tool and pins are context.
func (c *classifier) modelRecord(r *nodeRec) {
	if r.any().Kind == "table" {
		it := c.classifyNode(r, nodeSpec{row: "C11", group: GroupModelTables,
			names: fixed("name", "set", "completeness"), context: []string{"tool", "pins"}, unknown: isNull})
		c.tableListRule(it, r, false)
		c.add(it)
		return
	}
	it := c.classifyNode(r, nodeSpec{row: "C11", group: GroupModelTables, names: claimedBoth, unknown: isNull})
	c.completeTableRule(it, r.rc.Key)
	c.add(it)
}

// C12: Angular UI routes: path and target identities.
func (c *classifier) uiRoute(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C12", group: GroupAngular,
		names: fixed("path", "paths", "component", "children_load"), unknown: isNull}))
}

// C17: react-router UI routes (T026 AG-B5, docs/lang/typescript-bridges.md
// RR rows): the route keys the bridges contract records and their linked
// targets.
func (c *classifier) reactRoute(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C17", group: GroupReact,
		names: fixed("path", "index", "lazy", "open", "component", "element", "loader", "action"), unknown: isNull}))
}

// reactEdge reports whether an edge is a React bridge (C17) rather than an
// Angular structure edge (C12) or a generic one (C16): routes_to and
// passes_function from a react-router route, and renders that is not the
// Angular linker's. The Angular linker draws renders only from a component
// class to a class carrying Angular metadata (the node's "angular"
// attribute), so a renders edge from any other kind of source, or between
// classes neither of which carries that attribute on either side, is
// React's JSX element owner (rows JR1-JR3).
func (c *classifier) reactEdge(e *contract.Edge) bool {
	switch e.Kind {
	case "routes_to", "passes_function":
		return c.reactRouteNode(e.Source)
	case "renders":
		if identityKind(e.Source) != "class" {
			return true
		}
		known := false
		for side := base; side <= head; side++ {
			for _, id := range []string{e.Source, e.Target} {
				n := c.node(side, id)
				if n == nil {
					continue
				}
				known = true
				if _, ok := n.Attributes["angular"]; ok {
					return false
				}
			}
		}
		return known
	}
	return false
}

// reactRouteNode reports whether a ui_route identity is a react-router
// route on either side.
func (c *classifier) reactRouteNode(id string) bool {
	if identityKind(id) != "ui_route" {
		return false
	}
	for side := base; side <= head; side++ {
		if n := c.node(side, id); n != nil && n.Attributes["framework"] == "react-router" {
			return true
		}
	}
	return false
}

// C16: other listed nodes compare their content hash.
func (c *classifier) otherNode(r *nodeRec) {
	c.add(c.classifyNode(r, nodeSpec{row: "C16", group: GroupOther, names: fixed("content_hash"), unknown: isNull}))
}

// C1 (Revision 1): a file node is never an item of its own. Its file's
// record is the C1 item and the cause context (G4) of the items in that
// file, so the node is only counted: as a move when equal under
// projection, else as context.
func (c *classifier) fileNode(r *nodeRec) {
	g := c.group(GroupFiles)
	if b, h := r.side[base], r.side[head]; b != nil && h != nil && projectNode(b) == projectNode(h) {
		g.Moves++
		return
	}
	g.Context++
}

// routeEdge dispatches C3's edges: exposes_route (handlerEdge) and
// uses_model (modelEdge).
func (c *classifier) routeEdge(r *edgeRec) {
	if r.any().Kind == "uses_model" {
		c.modelEdge(r)
		return
	}
	c.handlerEdge(r)
}

// modelEdge (C3, T029 W2 row MT-13): a changed uses_model edge is
// reported on the route that uses the model, labelled models_changed,
// with the declarations it gained and lost in the field model_edges.
func (c *classifier) modelEdge(r *edgeRec) {
	e := r.any()
	c.contextEdge(r, e.Source, e.Target, nodeSpec{row: "C3", group: GroupRoutes, names: fixed(routeFields...)},
		LabelModelsChanged, "model_edges")
}

// handlerEdge (C3, Revision 1): a changed exposes_route edge is reported
// on the route it exposes, labelled handler_changed, with the handlers it
// gained and lost in the field handler_edges.
func (c *classifier) handlerEdge(r *edgeRec) {
	e := r.any()
	c.contextEdge(r, e.Target, e.Source, nodeSpec{row: "C3", group: GroupRoutes, names: fixed(routeFields...)},
		LabelHandlerChanged, "handler_edges")
}

// tableAccessEdge (C2, T019 Revision 4): a changed reads_table or
// writes_table edge of a code symbol is reported on the symbol's item,
// labelled table_access_changed, with the tables it gained and lost in the
// field named by the edge kind. The owner's table_access attributes are
// not compared on their own: C2 compares the content hash, and the edges
// carry the claims.
func (c *classifier) tableAccessEdge(r *edgeRec) {
	e := r.any()
	c.contextEdge(r, e.Source, e.Target, nodeSpec{row: "C2", group: GroupCode, names: fixed()},
		LabelTableAccessChanged, e.Kind)
}

// altersEdge (C10, Revision 1): a changed alters edge is reported on the
// migration it starts from, labelled alters_changed, with the tables it
// gained and lost in the field alters; a listed table it connects is
// labelled too.
func (c *classifier) altersEdge(r *edgeRec) {
	e := r.any()
	if c.contextEdge(r, e.Source, e.Target, nodeSpec{row: "C10", group: GroupMigrations, names: fixed(migrationFields...)},
		LabelAltersChanged, "alters") {
		if t := c.nodeItems[e.Target]; t != nil && c.node(base, e.Target) != nil && c.node(head, e.Target) != nil {
			addLabel(t, LabelAltersChanged)
		}
	}
}

// contextEdge reports a changed edge on the item of its owner node. Equal
// under projection it is a move; a differing confidence or provenance only
// is an other change. When the owner is absent on a side (the owner item
// is added, removed, known_now or unknown_now) the edge is part of that
// item and counted as context. Otherwise the owner's item (created as
// changed when the owner itself did not change) gets the label and the
// edge's other endpoint in the field's before and after lists; a footprint
// gap on either side makes the field unknown_now (G3). It reports whether
// the edge was put on an item with the label.
func (c *classifier) contextEdge(r *edgeRec, owner, other string, spec nodeSpec, label, field string) bool {
	b, h := r.side[base], r.side[head]
	g := c.group(spec.group)
	if b != nil && h != nil {
		if projectEdge(b) == projectEdge(h) {
			g.Moves++
		} else {
			g.OtherChanges++
		}
		return false
	}
	related := RelatedRaw{Record: store.RecordEdge, Key: r.rc.Key, Before: rawOf(r.rc).Before, After: rawOf(r.rc).After}
	it := c.nodeItems[owner]
	ob, oh := c.node(base, owner), c.node(head, owner)
	if ob == nil || oh == nil {
		g.Context++
		if it != nil {
			it.raw.Related = append(it.raw.Related, related)
		}
		return false
	}
	if it == nil {
		it = &Item{Group: spec.group, Row: spec.row, Change: Changed, Record: store.RecordNode, Identity: owner,
			Kind: oh.Kind, Name: oh.QualifiedName, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
			Before: c.state(ob, spec, spec.names(nil)), After: c.state(oh, spec, spec.names(nil)),
			footprint: sortedFiles(map[string]bool{ob.File: true, oh.File: true}), seed: seedRows[spec.row],
			raw: Raw{Before: ob, After: oh}}
		it.Test = scopeTest(ob) || scopeTest(oh)
		c.add(it)
	}
	if it.Change == BodyChanged {
		it.Change = Changed
	}
	addLabel(it, label)
	it.raw.Related = append(it.raw.Related, related)
	fp := edgeFootprint(r)
	set := map[string]bool{}
	for _, f := range append(append([]string{}, it.footprint...), fp...) {
		set[f] = true
	}
	it.footprint = sortedFiles(set)
	it.Cause = c.cause(it.footprint)
	var f *Field
	for i := range it.Fields {
		if it.Fields[i].Name == field {
			f = &it.Fields[i]
		}
	}
	if f == nil {
		it.Fields = append(it.Fields, Field{Name: field, Change: Changed, Before: []string{}, After: []string{}})
		f = &it.Fields[len(it.Fields)-1]
	}
	if b != nil {
		f.Before = sortedAppend(f.Before.([]string), other)
	} else {
		f.After = sortedAppend(f.After.([]string), other)
	}
	// G3: an edge gained on a base that does not know its footprint is
	// known_now; any other gap on either side makes the field unknown_now.
	kb, kh := c.cmp.know[base].knows(fp), c.cmp.know[head].knows(fp)
	switch {
	case !kh || !kb && b != nil:
		f.Change = UnknownNow
	case !kb && f.Change != UnknownNow:
		f.Change = KnownNow
	}
	if len(it.Fields) == 1 && it.Reason == nil && f.Change != Changed {
		it.Change, it.Reason = f.Change, ptr("gap")
	}
	return true
}

func addLabel(it *Item, label string) {
	if !containsString(it.Labels, label) {
		it.Labels = append(it.Labels, label)
	}
}

func sortedAppend(list []string, v string) []string {
	out := append(append([]string{}, list...), v)
	sort.Strings(out)
	return out
}

// D: unclassified nodes are listed with their raw payloads only.
func (c *classifier) unclassifiedNode(r *nodeRec) {
	kind := identityKind(r.rc.Key)
	name := r.rc.Key
	if n := r.any(); n != nil {
		kind, name = n.Kind, n.QualifiedName
	}
	raw := rawOf(r.rc)
	c.add(&Item{Group: GroupUnclassified, Change: Unclassified, Row: "D", Record: store.RecordNode,
		Identity: r.rc.Key, Kind: kind, Name: name, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
		Raw: &raw, raw: raw, footprint: nodeFootprint(r), undecodable: r.bad})
}

func (c *classifier) unclassifiedEdge(r *edgeRec) {
	kind, source, target, _ := store.ParseEdgeKey(r.rc.Key)
	raw := rawOf(r.rc)
	c.add(&Item{Group: GroupUnclassified, Change: Unclassified, Row: "D", Record: store.RecordEdge,
		Identity: r.rc.Key, Kind: kind, Name: source + " -> " + target, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
		Raw: &raw, raw: raw, footprint: edgeFootprint(r), undecodable: r.bad})
}

// edgeItem is the generic part of an edge row: presence with G3, moves
// counted, causes, and test scope from the source node.
func (c *classifier) edgeItem(r *edgeRec, row, group string) *Item {
	b, h := r.side[base], r.side[head]
	e := r.any()
	g := c.group(group)
	fp := edgeFootprint(r)
	it := &Item{
		Group: group, Row: row, Record: store.RecordEdge, Identity: r.rc.Key, Kind: e.Kind,
		Name: e.Source + " -> " + e.Target, Fields: []Field{}, Labels: []string{}, Hints: []Hint{},
		Before: edgeState(b), After: edgeState(h), footprint: fp, seed: seedRows[row], raw: rawOf(r.rc),
	}
	knows := [2]bool{c.cmp.know[base].knows(fp), c.cmp.know[head].knows(fp)}
	switch {
	case b != nil && h != nil:
		if projectEdge(b) == projectEdge(h) {
			g.Moves++
			return nil
		}
		g.OtherChanges++
		return nil
	case b != nil:
		it.Change, it.Wording = Removed, c.removalWording(false)
		presenceG3(it, knows)
	default:
		it.Change = Added
		presenceG3(it, knows)
	}
	it.Provenance, it.Confidence = &e.Provenance, &e.Confidence
	it.Cause = c.cause(fp)
	it.Test = scopeTest(c.node(base, e.Source)) || scopeTest(c.node(head, e.Source))
	return it
}

func edgeState(e *contract.Edge) *State {
	if e == nil {
		return nil
	}
	st := &State{Fields: map[string]any{"provenance": e.Provenance, "confidence": e.Confidence}}
	if len(e.Evidence) > 0 {
		st.File, st.Span = e.Evidence[0].File, &e.Evidence[0].Span
	}
	return st
}

var rowGroups = map[string]string{
	"C5": GroupClientCalls, "C6": GroupAPI, "C7": GroupIPC, "C9": GroupMigrationTables,
	"C11": GroupModelTables, "C12": GroupAngular, "C16": GroupOther, "C17": GroupReact, "C18": GroupServerActions,
}

// presence: C12 and C16 edges compare presence only. A structural C16
// edge (contains, imports) gained or lost with an endpoint that has a C3-C12
// item is no claim of its own: it takes that item's state and reason and is
// counted as context of the item (Revision 3, R3-3), so it never says
// removed next to an item that says unknown_now. A file node's
// reads_table or writes_table edge to a table with a C8 or C11 item is
// context of that item the same way (T019 Revision 4).
func (c *classifier) presence(r *edgeRec) {
	e := r.any()
	row := c.edgeRow(e)
	it := c.edgeItem(r, row, rowGroups[row])
	if it != nil && row == "C16" && (structuralEdges[e.Kind] || tableAccessKinds[e.Kind]) {
		for _, end := range []string{e.Target, e.Source} {
			owner := c.nodeItems[end]
			if owner == nil || !classifiedRows[owner.Row] {
				continue
			}
			owner.raw.Related = append(owner.raw.Related, RelatedRaw{Record: store.RecordEdge, Key: r.rc.Key, Before: it.raw.Before, After: it.raw.After})
			c.group(owner.Group).Context++
			return
		}
	}
	c.add(it)
}

// structuralEdges are the C16 edge kinds that only place a node in its
// file or parent; classifiedRows are the rows whose items own them.
var (
	structuralEdges = map[string]bool{"contains": true, "imports": true}
	classifiedRows  = map[string]bool{"C3": true, "C4": true, "C5": true, "C6": true, "C7": true, "C8": true,
		"C9": true, "C10": true, "C11": true, "C12": true, "C17": true, "C18": true}
)

// foreignKey: C9 and C11 foreign keys, added or removed only when the
// source column's table is complete on both sides.
func (c *classifier) foreignKey(r *edgeRec) {
	row := c.edgeRow(r.any())
	it := c.edgeItem(r, row, rowGroups[row])
	c.completeTableRule(it, r.any().Source)
	c.add(it)
}

// linkSpec parameterises C5's link logic: the call's identifying fields
// (method and URL, or the command name), the target's, and the target's row.
type linkSpec struct {
	row          string
	callFields   []string
	targetFields []string
	// static reports a head call whose identifying fields are known.
	static func(call *contract.Node) bool
}

var (
	routeLinkSpec = linkSpec{row: "C5", callFields: []string{"method", "url_expression", "url_templates"},
		targetFields: []string{"method", "pattern", "paths"},
		static:       func(n *contract.Node) bool { return n.Attributes["method"] != nil }}
	operationLinkSpec = linkSpec{row: "C6", callFields: routeLinkSpec.callFields,
		targetFields: []string{"method", "path"}, static: routeLinkSpec.static}
	actionLinkSpec = linkSpec{row: "C18", callFields: []string{"action", "alternatives"}, targetFields: []string{"action", "handler"},
		static: func(n *contract.Node) bool {
			return n.Attributes["action"] != nil || n.Attributes["alternatives"] != nil
		}}
	commandLinkSpec = linkSpec{row: "C7", callFields: []string{"command"}, targetFields: []string{"command"},
		static: func(n *contract.Node) bool { return n.Attributes["command"] != nil }}
)

// C5: calls_route links.
func (c *classifier) routeLink(r *edgeRec) { c.add(c.link(r, routeLinkSpec)) }

// operationEdge: C6's documents_route is presence; calls_operation uses
// C5's link logic.
func (c *classifier) operationEdge(r *edgeRec) {
	if r.any().Kind == "calls_operation" {
		c.add(c.link(r, operationLinkSpec))
		return
	}
	c.add(c.edgeItem(r, "C6", GroupAPI))
}

// ipcEdge: C7's exposes_command is presence; invokes_command uses C5's
// link logic with the command name.
func (c *classifier) ipcEdge(r *edgeRec) {
	if r.any().Kind == "invokes_command" {
		c.add(c.link(r, commandLinkSpec))
		return
	}
	c.add(c.edgeItem(r, "C7", GroupIPC))
}

// callValue reads a call's identifying field; url_templates are the
// composed path templates.
func callValue(n *contract.Node, name string) any {
	if name != "url_templates" {
		return n.Attributes[name]
	}
	var out []any
	list, _ := n.Attributes["paths"].([]any)
	for _, p := range list {
		if m, ok := p.(map[string]any); ok {
			out = append(out, m["template"])
		}
	}
	return out
}

func sameFields(a, b *contract.Node, names []string) bool {
	for _, name := range names {
		if canonical(callValue(a, name)) != canonical(callValue(b, name)) {
			return false
		}
	}
	return true
}

// link applies C5: link_lost only with a proven reason and a known static
// head call whose status is unmatched; otherwise link_unknown_now.
func (c *classifier) link(r *edgeRec, spec linkSpec) *Item {
	b, h := r.side[base], r.side[head]
	e := r.any()
	group := rowGroups[spec.row]
	it := c.edgeItem(r, spec.row, group)
	if it == nil {
		if b != nil && h != nil && projectEdge(b) != projectEdge(h) {
			// Undo edgeItem's other-changes count: confidence and
			// provenance are this row's fields.
			c.group(group).OtherChanges--
			it = &Item{Group: group, Row: spec.row, Record: store.RecordEdge, Identity: r.rc.Key, Kind: e.Kind,
				Name: e.Source + " -> " + e.Target, Labels: []string{}, Hints: []Hint{}, Before: edgeState(b), After: edgeState(h),
				footprint: edgeFootprint(r), seed: true, raw: rawOf(r.rc)}
			for _, f := range []struct {
				name string
				b, h any
			}{{"confidence", b.Confidence, h.Confidence}, {"provenance", b.Provenance, h.Provenance}} {
				if canonical(f.b) != canonical(f.h) {
					it.Fields = append(it.Fields, Field{Name: f.name, Change: Changed, Before: f.b, After: f.h})
				}
			}
			if len(it.Fields) == 0 {
				c.group(group).OtherChanges++
				return nil
			}
			it.Change = Changed
			it.Provenance, it.Confidence = &h.Provenance, &h.Confidence
			it.Cause = c.cause(it.footprint)
			it.Test = scopeTest(c.node(head, e.Source))
			if !c.cmp.know[base].knows(it.footprint) || !c.cmp.know[head].knows(it.footprint) {
				it.Change, it.Reason = UnknownNow, ptr("gap")
			}
		}
		return it
	}
	switch it.Change {
	case Added:
		it.Change = LinkGained
	case Removed:
		it.Wording = nil
		it.Change, it.Reason = c.lostReason(e, spec)
		if it.Reason != nil && *it.Reason == ReasonCallAbsent {
			it.Wording = ptr("call_removed")
		}
	case UnknownNow:
		it.Change = LinkUnknown
	}
	return it
}

// lostReason decides a lost link (C5's refusal conditions).
func (c *classifier) lostReason(e *contract.Edge, spec linkSpec) (string, *string) {
	unknown := func(reason string) (string, *string) { return LinkUnknown, ptr(reason) }
	call := c.node(head, e.Source)
	if call == nil {
		// Revision 1: a link whose call was removed is lost, its absence
		// proven by the removal; a call absent for another reason (a gap)
		// leaves the link unknown.
		if it := c.nodeItems[e.Source]; it != nil && it.Change == Removed {
			return LinkLost, ptr(ReasonCallAbsent)
		}
		return unknown("call_not_proven_removed")
	}
	if status, _ := call.Attributes["status"].(string); status != "unmatched" {
		return unknown("head_status_" + status)
	}
	if !spec.static(call) {
		return unknown("call_not_static")
	}
	baseCall := c.node(base, e.Source)
	callSame := baseCall != nil && sameFields(baseCall, call, spec.callFields)
	target := c.nodeItems[e.Target]
	targetSame := target == nil || target.Change == BodyChanged
	targetChanged := false
	if target != nil && target.Change == Changed {
		for _, f := range target.Fields {
			for _, name := range spec.targetFields {
				targetChanged = targetChanged || f.Name == name
			}
		}
	}
	switch {
	case callSame && target != nil && target.Change == Removed:
		return LinkLost, ptr(ReasonTargetRemoved)
	case callSame && targetChanged:
		return LinkLost, ptr(ReasonTargetChanged)
	case !callSame && baseCall != nil && targetSame && c.node(head, e.Target) != nil:
		return LinkLost, ptr(ReasonCallChanged)
	}
	return unknown("reason_not_proven")
}

// C1: file records.
func (c *classifier) fileRows() error {
	keys := make([]string, 0, len(c.files))
	for k := range c.files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	c.group(GroupFiles)
	for _, path := range keys {
		r := c.files[path]
		if r.bad {
			raw := rawOf(r.rc)
			c.add(&Item{Group: GroupUnclassified, Change: Unclassified, Row: "D", Record: store.RecordFile, Identity: path,
				Kind: "file", Name: path, Fields: []Field{}, Labels: []string{}, Hints: []Hint{}, Raw: &raw, raw: raw, footprint: []string{path}, undecodable: true})
			continue
		}
		b, h := r.side[base], r.side[head]
		it := &Item{Group: GroupFiles, Row: "C1", Record: store.RecordFile, Identity: path, Kind: "file", Name: path,
			Fields: []Field{}, Labels: []string{}, Hints: []Hint{}, Before: fileState(b), After: fileState(h),
			footprint: []string{path}, raw: rawOf(r.rc)}
		k := c.cmp.know
		switch {
		case b != nil && h != nil:
			if projectFile(b) == projectFile(h) {
				c.cmp.Summary.FileMetadataOnly++
				continue
			}
			for _, f := range []struct {
				name string
				b, h string
			}{{"content_hash", b.ContentHash, h.ContentHash}, {"language", b.Language, h.Language}, {"precision", b.Precision, h.Precision}} {
				if f.b != f.h {
					it.Fields = append(it.Fields, Field{Name: f.name, Change: Changed, Before: f.b, After: f.h})
				}
			}
			it.Change = Changed
		case b != nil:
			it.Change = Removed
			it.Wording, it.Reason = c.fileRemoval(path)
			presenceG3(it, [2]bool{!k[base].whole && !k[base].gap(path), !k[head].whole && !k[head].gap(path)})
		default:
			it.Change = Added
			presenceG3(it, [2]bool{!k[base].whole && !k[base].gap(path), !k[head].whole && !k[head].gap(path)})
		}
		it.Cause = c.cause(it.footprint)
		c.add(it)
	}
	return nil
}

func fileState(f *contract.File) *State {
	if f == nil {
		return nil
	}
	return &State{File: f.Path, Fields: map[string]any{"content_hash": f.ContentHash, "language": f.Language, "precision": f.Precision}}
}

// C13: architecture facts are not extracted yet.
func (c *classifier) architecture() error {
	c.cmp.Summary.NotExtracted = []string{GroupArchitecture}
	return nil
}

// C14: an inferred move hint pairs one removed and one added node of the
// same kind with the same content hash, or the same qualified name in
// another file, when the pairing is unique in the comparison. The items
// stay removed and added.
func (c *classifier) moveHints() error {
	var removed, added []*Item
	for _, it := range c.items {
		if it.Record != store.RecordNode || it.Row == "D" {
			continue
		}
		switch it.Change {
		case Removed:
			removed = append(removed, it)
		case Added:
			added = append(added, it)
		}
	}
	match := func(a, b *Item) string {
		na, nb := c.node(base, a.Identity), c.node(head, b.Identity)
		if na == nil || nb == nil || na.Kind != nb.Kind {
			return ""
		}
		if na.ContentHash != "" && na.ContentHash == nb.ContentHash {
			return "content_hash"
		}
		if na.QualifiedName == nb.QualifiedName && na.File != nb.File {
			return "qualified_name"
		}
		return ""
	}
	pairs := map[*Item][]*Item{}
	back := map[*Item][]*Item{}
	basis := map[[2]*Item]string{}
	for _, r := range removed {
		for _, a := range added {
			if m := match(r, a); m != "" {
				pairs[r] = append(pairs[r], a)
				back[a] = append(back[a], r)
				basis[[2]*Item{r, a}] = m
			}
		}
	}
	for _, r := range removed {
		if len(pairs[r]) != 1 || len(back[pairs[r][0]]) != 1 {
			continue
		}
		a := pairs[r][0]
		m := basis[[2]*Item{r, a}]
		r.Hints = append(r.Hints, Hint{Row: "C14", Kind: "possible_move", Other: a.Identity, Basis: m, Provenance: "inferred"})
		a.Hints = append(a.Hints, Hint{Row: "C14", Kind: "possible_move", Other: r.Identity, Basis: m, Provenance: "inferred"})
	}
	return nil
}

// C15: knowledge gaps per path: became_partial when a path is a gap of the
// head only, recovered when of the base only. A base whose gaps were never
// recorded yields no recovered items. Codes come from the diagnostics of a
// side with its own graph.
func (c *classifier) gapRows() error {
	set := c.cmp.set
	bg, hg := map[string]bool{}, map[string]bool{}
	for _, p := range set.Base.Run.Gaps {
		bg[p] = true
	}
	for _, p := range set.Head.Run.Gaps {
		hg[p] = true
	}
	var became, recovered []string
	for p := range hg {
		if !bg[p] && !set.Base.Run.GapsUnknown {
			became = append(became, p)
		}
	}
	for p := range bg {
		if !hg[p] {
			recovered = append(recovered, p)
		}
	}
	sort.Strings(became)
	sort.Strings(recovered)
	codes := func(side int, paths []string) (map[string][]string, error) {
		out := map[string][]string{}
		repo := c.cmp.graph[side]
		if len(paths) == 0 || side == base && set.Base.Kind != store.SideRef {
			return out, nil
		}
		diags, err := c.s.Diagnostics(c.ctx, repo, paths)
		if err != nil {
			return nil, fmt.Errorf("reading gap diagnostics: %w", err)
		}
		for _, d := range diags {
			if store.GapDiagnostic(d) && !containsString(out[d.File], d.Code) {
				out[d.File] = append(out[d.File], d.Code)
			}
		}
		for _, list := range out {
			sort.Strings(list)
		}
		return out, nil
	}
	hc, err := codes(head, became)
	if err != nil {
		return err
	}
	bc, err := codes(base, recovered)
	if err != nil {
		return err
	}
	for _, p := range became {
		c.add(gapItem(p, BecamePartial, nil, &State{File: p, Fields: map[string]any{"codes": codesOrNull(hc[p])}}))
	}
	for _, p := range recovered {
		c.add(gapItem(p, Recovered, &State{File: p, Fields: map[string]any{"codes": codesOrNull(bc[p])}}, nil))
	}
	return nil
}

func codesOrNull(list []string) any {
	if list == nil {
		return nil
	}
	return list
}

func gapItem(path, change string, before, after *State) *Item {
	return &Item{Group: GroupGaps, Change: change, Row: "C15", Record: "gap", Identity: path, Kind: "gap", Name: path,
		Fields: []Field{}, Labels: []string{}, Hints: []Hint{}, Before: before, After: after}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// finish sorts the items (group, change, identity), counts them per group
// and fills the summary.
func (c *classifier) finish() {
	order := map[string]int{}
	for i, g := range GroupOrder {
		order[g] = i
	}
	sort.SliceStable(c.items, func(i, j int) bool {
		a, b := c.items[i], c.items[j]
		if a.Group != b.Group {
			return order[a.Group] < order[b.Group]
		}
		if a.Change != b.Change {
			return a.Change < b.Change
		}
		if a.Identity != b.Identity {
			return a.Identity < b.Identity
		}
		return a.Record < b.Record
	})
	cmp := c.cmp
	cmp.items = make([]Item, len(c.items))
	cmp.byID = map[string]int{}
	for i, it := range c.items {
		cmp.items[i] = *it
		cmp.byID[itemID(it.Record, it.Identity)] = i
		g := c.group(it.Group)
		g.Items++
		g.Changes[it.Change]++
		if it.Cause != nil {
			g.Causes[*it.Cause]++
		}
		g.Kinds[it.Kind]++
		if it.Test {
			g.HiddenTests++
		}
	}
	s := &cmp.Summary
	s.ContractVersion = ContractVersion
	s.Base, s.Head = sideInfo(cmp.set.Base), sideInfo(cmp.set.Head)
	s.ExtractorChanged = c.extractorChanged()
	s.Partial = s.Base.Partial || s.Head.Partial
	s.Total = len(cmp.items)
	if s.NotExtracted == nil {
		s.NotExtracted = []string{}
	}
	for _, name := range GroupOrder {
		s.Groups = append(s.Groups, *c.group(name))
	}
	b, h := cmp.set.Base.Run, cmp.set.Head.Run
	if s.ExtractorChanged && b.Commit != "" && h.Commit != "" && b.Dirty != nil && !*b.Dirty && h.Dirty != nil && !*h.Dirty {
		s.ExtractorRecheck = ptr("ref:" + b.Commit + ".." + h.Commit)
	}
	cmp.impacts = map[string]*ImpactResult{}
	cmp.files = map[string]*indexBuild[*fileIndex]{}
	cmp.unknowns = map[string]*indexBuild[*UnknownUses]{}
}

// itemID is the id an item is addressed by: "<record>:<key>" for files,
// edges and gaps, the identity itself for nodes.
func itemID(record, key string) string {
	if record == store.RecordNode {
		return key
	}
	return record + ":" + key
}

// ID is the item's address for impact requests.
func (it Item) ID() string { return itemID(it.Record, it.Identity) }

// MarshalJSON adds the item's id.
func (it Item) MarshalJSON() ([]byte, error) {
	type plain Item
	return json.Marshal(struct {
		ID string `json:"id"`
		plain
	}{it.ID(), plain(it)})
}

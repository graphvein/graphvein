package server

import (
	"encoding/json"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the Flow endpoint's downstream additions of T026 AG-C3
// (ADR 0045 revision, ADR 0079): the nested functions of the handler and
// its callees (chain row CH7), the tables they read or write (row CH6),
// the sites they could not bind to a table ("may also use") and the count
// of tables first reached at hop 3. Every returned edge is still a stored
// edge returned as stored; the unknown sites and the hop-3 count are
// separate fields, never edges.

// flowNestedDepth bounds how deep nested functions are followed inside one
// container (a function in a function in a function ...).
const flowNestedDepth = 8

// flowTableEdge is a stored reads_table or writes_table edge with chain
// row CH6's wording ("reads", "may write", "builds a query on"), computed
// from the edge and its owner's table_access entry as the chain words it.
type flowTableEdge struct {
	flowEdge
	Wording string `json:"wording"`
}

// flowMayAlsoUse is one table_access_unknown site of a returned
// handler-side function: recognised, not bound to a table.
type flowMayAlsoUse struct {
	Accessor string  `json:"accessor"`
	Hop      int     `json:"hop"`
	Row      *string `json:"row"`
	Tool     *string `json:"tool"`
	Reason   *string `json:"reason"`
	Partial  bool    `json:"partial"`
	Line     *int    `json:"line"`
	// Candidates are the tables the row narrowed the site to; null means
	// any table of the tool's sources in the package.
	Candidates []flowTableRef `json:"candidates"`
}

// flowTableRef names a candidate table; Name is null when the identity is
// not a stored node.
type flowTableRef struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}

// flowBeyond counts what lies one hop past the returned graph: tables a
// hop-3 function reads or writes that are not returned, the hop-3
// functions with such an edge, and their unknown sites. It is a count, not
// a list, and never an absence: deeper hops are not read.
type flowBeyond struct {
	Hop        int `json:"hop"`
	Tables     int `json:"tables"`
	Functions  int `json:"functions"`
	MayAlsoUse int `json:"may_also_use"`
}

func isFunction(n contract.Node) bool { return n.Kind == "function" || n.Kind == "method" }

// nestedOf follows stored contains edges from ids to functions and
// methods, transitively up to flowNestedDepth. It returns the edges used
// and the nested nodes; edges to a node that is not stored are reported
// in missing.
func (b *flowBuilder) nestedOf(ids []string) (used []contract.Edge, nested map[string]contract.Node, missing []contract.Edge, err error) {
	nested = map[string]contract.Node{}
	seen := setOf(ids...)
	frontier := distinct(ids)
	for depth := 0; len(frontier) > 0 && depth < flowNestedDepth; depth++ {
		edges, err := b.rd.edges(frontier, "contains", store.Outgoing)
		if err != nil {
			return nil, nil, nil, err
		}
		targets := make([]string, 0, len(edges))
		for _, e := range edges {
			targets = append(targets, e.Target)
		}
		nodes, err := b.rd.byIdentity(targets)
		if err != nil {
			return nil, nil, nil, err
		}
		var next []string
		for _, e := range edges {
			n, ok := nodes[e.Target]
			if !ok {
				missing = append(missing, e)
				continue
			}
			if !isFunction(n) {
				continue
			}
			used = append(used, e)
			nested[n.Identity] = n
			if !seen[n.Identity] {
				seen[n.Identity] = true
				next = append(next, n.Identity)
			}
		}
		frontier = distinct(next)
	}
	return used, nested, missing, nil
}

// addNested adds the nested functions of ids (row CH7 downstream) with
// role nested at hop, and the contains edges to them. It returns the
// added identities.
func (b *flowBuilder) addNested(ids []string, hop int) ([]string, error) {
	used, nested, missing, err := b.nestedOf(ids)
	if err != nil {
		return nil, err
	}
	for _, e := range missing {
		b.skipped[keyOf(e)] = true
	}
	out := make([]string, 0, len(nested))
	for id, n := range nested {
		b.add(n, roleNested, hop)
		out = append(out, id)
	}
	for _, e := range used {
		b.addEdge(e)
	}
	return distinct(out), nil
}

// downstreamHop is the smallest hop at which a node has a handler-side
// role (handler, nested, callee); ok is false for a node with none.
func downstreamHop(e *flowEntry) (hop int, ok bool) {
	hop = 3
	for _, role := range []string{roleHandler, roleNested, roleCallee} {
		if h, has := e.roles[role]; has && h < hop {
			hop, ok = h, true
		}
	}
	return hop, ok
}

// accessors are the returned handler-side functions with their hop.
func (b *flowBuilder) accessors() map[string]int {
	out := map[string]int{}
	for id, e := range b.nodes {
		if !isFunction(e.node) {
			continue
		}
		if hop, ok := downstreamHop(e); ok {
			out[id] = hop
		}
	}
	return out
}

var tableEdgeKinds = []string{"reads_table", "writes_table"}

// addTables adds the tables the handler-side functions read or write
// (row CH6) with role table at the accessor's hop (at least 1), and the
// stored edges.
func (b *flowBuilder) addTables() error {
	acc := b.accessors()
	ids := make([]string, 0, len(acc))
	for id := range acc {
		ids = append(ids, id)
	}
	ids = distinct(ids)
	for _, kind := range tableEdgeKinds {
		edges, err := b.rd.edges(ids, kind, store.Outgoing)
		if err != nil {
			return err
		}
		targets := make([]string, 0, len(edges))
		for _, e := range edges {
			targets = append(targets, e.Target)
		}
		nodes, err := b.rd.byIdentity(targets)
		if err != nil {
			return err
		}
		for _, e := range edges {
			hop, from := acc[e.Source]
			if !from {
				continue
			}
			n, ok := nodes[e.Target]
			if !ok {
				b.skipped[keyOf(e)] = true
				continue
			}
			if n.Kind != "table" {
				continue
			}
			b.add(n, roleTable, max(hop, 1))
			b.addEdge(e)
		}
	}
	return nil
}

// mayAlsoUse lists the table_access_unknown sites of the returned
// handler-side functions, ordered by hop, accessor and line.
func (b *flowBuilder) mayAlsoUse() ([]flowMayAlsoUse, error) {
	acc := b.accessors()
	out := []flowMayAlsoUse{}
	var candidates []string
	for id, hop := range acc {
		for _, entry := range objects(b.nodes[id].node.Attributes["table_access_unknown"]) {
			m := flowMayAlsoUse{Accessor: id, Hop: hop, Row: entryString(entry, "row"), Tool: entryString(entry, "tool"), Reason: entryString(entry, "reason")}
			m.Partial, _ = entry["partial"].(bool)
			m.Line = entryInt(entry, "line")
			if list, ok := entry["candidates"].([]any); ok {
				m.Candidates = []flowTableRef{}
				for _, c := range list {
					if s, ok := c.(string); ok {
						m.Candidates = append(m.Candidates, flowTableRef{ID: s})
						candidates = append(candidates, s)
					}
				}
			}
			out = append(out, m)
		}
	}
	nodes, err := b.rd.byIdentity(candidates)
	if err != nil {
		return nil, err
	}
	for i := range out {
		for j, c := range out[i].Candidates {
			if n, ok := nodes[c.ID]; ok {
				name := n.Name
				out[i].Candidates[j].Name = &name
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, c := out[i], out[j]
		if a.Hop != c.Hop {
			return a.Hop < c.Hop
		}
		if a.Accessor != c.Accessor {
			return a.Accessor < c.Accessor
		}
		return siteLine(a.Line) < siteLine(c.Line)
	})
	return out, nil
}

func entryString(entry map[string]any, key string) *string {
	if s, ok := entry[key].(string); ok {
		return &s
	}
	return nil
}

// entryInt is a whole-number field of an attribute object (the store
// decodes numbers with UseNumber), or nil.
func entryInt(entry map[string]any, key string) *int {
	switch v := entry[key].(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			n := int(i)
			return &n
		}
	case float64:
		if v == float64(int(v)) {
			n := int(v)
			return &n
		}
	}
	return nil
}

func siteLine(l *int) int {
	if l == nil {
		return 0
	}
	return *l
}

// beyond counts the tables first reached at hop 3: the callees of the
// hop-2 functions and their nested functions, read without a fan-out cap,
// and the tables they read or write that the response does not return.
func (b *flowBuilder) beyond() (*flowBeyond, error) {
	out := &flowBeyond{Hop: 3}
	if len(b.frontier) == 0 {
		return out, nil
	}
	calls, err := b.rd.edges(distinct(b.frontier), "calls", store.Outgoing)
	if err != nil {
		return nil, err
	}
	var targets []string
	for _, e := range calls {
		if b.nodes[e.Target] == nil {
			targets = append(targets, e.Target)
		}
	}
	nodes, err := b.rd.byIdentity(targets)
	if err != nil {
		return nil, err
	}
	hop3 := map[string]contract.Node{}
	for id, n := range nodes {
		if isFunction(n) {
			hop3[id] = n
		}
	}
	ids := make([]string, 0, len(hop3))
	for id := range hop3 {
		ids = append(ids, id)
	}
	_, nested, _, err := b.nestedOf(ids)
	if err != nil {
		return nil, err
	}
	for id, n := range nested {
		if b.nodes[id] == nil {
			hop3[id] = n
		}
	}
	ids = ids[:0]
	for id := range hop3 {
		ids = append(ids, id)
	}
	ids = distinct(ids)
	tables, functions := map[string]bool{}, map[string]bool{}
	for _, kind := range tableEdgeKinds {
		edges, err := b.rd.edges(ids, kind, store.Outgoing)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if b.nodes[e.Target] != nil || hop3[e.Source].Identity == "" {
				continue
			}
			tables[e.Target] = true
			functions[e.Source] = true
		}
	}
	for _, id := range ids {
		out.MayAlsoUse += len(objects(hop3[id].Attributes["table_access_unknown"]))
	}
	out.Tables, out.Functions = len(tables), len(functions)
	return out, nil
}

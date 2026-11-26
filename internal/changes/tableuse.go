package changes

import (
	"context"
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
)

// Row I8 (T019 Revision 4) follows T018h's code-to-table edges
// (docs/extract/table-access.md, ADR 0060): from a table, the incoming
// reads_table and writes_table edges to their owner; from a column, the
// same edges of its table (the edges are table-level, so the column's own
// use is not extracted). The edge data lives on the owner node's
// table_access entry, joined by (kind, target). A table's users are a lower
// bound: owners whose table_access_unknown entries name the table are
// listed as "may also be used by", and no answer ever says "no users".

// tableAccessKinds are the edge kinds of T018h's code-to-table links.
var tableAccessKinds = map[string]bool{"reads_table": true, "writes_table": true}

// accessModelTools are the model tools some code-to-table row covers
// (CT-P SQLModel/SQLAlchemy, CT-R Prisma, CT-Z Drizzle, and the Supabase
// rows SB-T1 to SB-T4 over Supabase's generated types, T024c3). Model
// groups of other tools (Sequelize, Diesel, GORM, TypeORM) have no rows
// yet.
var accessModelTools = map[string]bool{"sqlmodel": true, "prisma": true, "drizzle": true, "supabase_types": true}

// AccessCovered reports whether a code-to-table row covers the source of a
// table (Revision 4, item 3), so that a missing edge means no use was
// found rather than that uses are not extracted. It is the one coverage
// rule: change impact's I8 and the MCP table tool (T030 review) both
// answer "users not extracted" for a table it refuses, never zero uses. Migration tables are
// sqlx's (the only migration tool the schema extractor reads); rows CT-S1
// and CT-S2 resolve against a package's Postgres sets only, so SQLite and
// MySQL sets are not covered. A model table is covered when its tool is.
func AccessCovered(table *contract.Node) bool {
	if table == nil || table.Kind != "table" {
		return false
	}
	if table.Attributes["source"] == "model" {
		tool, _ := table.Attributes["tool"].(string)
		return accessModelTools[tool]
	}
	return table.Attributes["dialect"] == "postgres"
}

// TableAccess is an I8 step's entry from the owner node's table_access
// attribute: how the owner uses the table and how far the form is proven.
type TableAccess struct {
	Tool       string   `json:"tool"`
	Role       string   `json:"role"`
	Operations []string `json:"operations"`
	Rows       []string `json:"rows"`
	// Verification is "oracle" or "mapped"; VerificationWording is how a
	// client shows it ("mapped, not verified").
	Verification        string   `json:"verification"`
	VerificationWording string   `json:"verification_wording"`
	Assumes             []string `json:"assumes"`
	// ColumnUse is "not_extracted" on a step from a column seed or
	// reached column: the edge names the column's table, not the column.
	ColumnUse *string `json:"column_use"`
}

// verificationWordings are the client wordings of the entries'
// verification values (T018h Revision 1, O6).
var verificationWordings = map[string]string{
	"oracle": "verified by an oracle",
	"mapped": "mapped, not verified",
}

// UnknownUse is one table_access_unknown entry of an owner that names a
// table among its candidates: a possible use the linker could not prove.
type UnknownUse struct {
	Owner   string `json:"owner"`
	Row     string `json:"row"`
	Line    int    `json:"line"`
	Tool    string `json:"tool"`
	Reason  string `json:"reason"`
	Partial bool   `json:"partial"`
	// Provenance is always "inferred": the use is possible, not proven.
	Provenance string `json:"provenance"`
	// Unbound is true for an entry whose candidates are null: the row
	// could not narrow the site to tables, so any table of the tool's
	// sources in the site's package may be the one (T030 review).
	Unbound bool `json:"unbound,omitempty"`
}

// TableUse is the answer for one table whose users the traversal read:
// the owners found through edges (a lower bound, worded "uses found") and
// the owners that may also use it. There is no "no users" answer.
type TableUse struct {
	Table           string       `json:"table"`
	Wording         string       `json:"wording"`
	Found           []string     `json:"found"`
	MayAlsoWording  string       `json:"may_also_wording"`
	MayAlsoBeUsedBy []UnknownUse `json:"may_also_be_used_by"`
}

// accessWording is the claim wording of a path whose last step is an I8
// edge: "builds a query on this table" for a statement built but not run
// in the owner, else reads or writes, "may" for an inferred edge.
func accessWording(s Step) string {
	if s.Access != nil && s.Access.Role == "builds" {
		return "builds a query on this table"
	}
	verb := "reads"
	if s.Kind == "writes_table" {
		verb = "writes"
	}
	if s.Provenance != "fact" {
		return "may " + verb[:len(verb)-1] + " this table"
	}
	return verb + " this table"
}

// accessEntry finds the owner's table_access entry for an edge.
func AccessEntry(owner *contract.Node, kind, target string) *TableAccess {
	out := &TableAccess{Operations: []string{}, Rows: []string{}, Assumes: []string{}, Verification: "unknown",
		VerificationWording: "no table_access entry on the owner"}
	if owner == nil {
		return out
	}
	list, _ := owner.Attributes["table_access"].([]any)
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		if m["kind"] != kind || m["target"] != target {
			continue
		}
		out.Tool, _ = m["tool"].(string)
		out.Role, _ = m["role"].(string)
		out.Operations = stringList(m["operations"])
		out.Rows = stringList(m["rows"])
		out.Assumes = stringList(m["assumes"])
		out.Verification, _ = m["verification"].(string)
		out.VerificationWording = verificationWordings[out.Verification]
		return out
	}
	return out
}

func stringList(v any) []string {
	out := []string{}
	list, _ := v.([]any)
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// nodesOf reads nodes of graph by identity; a node the graph does not hold
// (a removed seed's table or owner) is read from the change set's base
// payload instead.
func (t *traversal) nodesOf(graph string, ids []string) (map[string]*contract.Node, error) {
	out := map[string]*contract.Node{}
	if len(ids) == 0 {
		return out, nil
	}
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	nodes, err := t.s.NodesByIdentity(t.ctx, graph, sorted)
	if err != nil {
		return nil, fmt.Errorf("reading table users: %w", err)
	}
	for i := range nodes {
		out[nodes[i].Identity] = &nodes[i]
	}
	missing := map[string]bool{}
	for _, id := range sorted {
		if out[id] == nil {
			missing[id] = true
		}
	}
	if len(missing) == 0 {
		return out, nil
	}
	for _, rc := range t.c.set.Changes {
		if rc.Record != store.RecordNode || !missing[rc.Key] {
			continue
		}
		raw := rc.Before
		if raw == nil {
			raw = rc.After
		}
		var n contract.Node
		if decode(raw, &n) == nil {
			out[rc.Key] = &n
		}
	}
	return out, nil
}

// tableUsers adds the I8 candidates of a frontier: the table seeds' own
// incoming edges are matched by follows; here each table and column is
// checked against the rows' coverage (not_extracted otherwise), and a
// column gets its table's edges with column_use not_extracted. tableOf
// maps a frontier column to its table (incoming contains).
func (t *traversal) tableUsers(frontier []string, tableOf map[string]string, graph string, fromChangeSet bool) ([]candidate, error) {
	var tables []string
	seen := map[string]bool{}
	for _, id := range frontier {
		table := id
		if identityKind(id) == "column" {
			table = tableOf[id]
		} else if identityKind(id) != "table" {
			continue
		}
		if table != "" && !seen[table] {
			seen[table] = true
			tables = append(tables, table)
		}
	}
	if len(tables) == 0 {
		for _, id := range frontier {
			if identityKind(id) == "column" {
				t.note("I8", id)
			}
		}
		return nil, nil
	}
	nodes, err := t.nodesOf(graph, tables)
	if err != nil {
		return nil, err
	}
	var columnTables []string
	columnsOf := map[string][]string{}
	for _, id := range frontier {
		switch identityKind(id) {
		case "table":
			if AccessCovered(nodes[id]) {
				t.useTables[id] = graph
			} else {
				t.note("I8", id)
			}
		case "column":
			table := tableOf[id]
			if table == "" || !AccessCovered(nodes[table]) {
				t.note("I8", id)
				continue
			}
			t.useTables[table] = graph
			if columnsOf[table] == nil {
				columnTables = append(columnTables, table)
			}
			columnsOf[table] = append(columnsOf[table], id)
		}
	}
	if len(columnTables) == 0 {
		return nil, nil
	}
	sort.Strings(columnTables)
	// A removed column's table usually remains: its users are read from
	// the graph that holds the dependents, plus the edges the change set
	// removed with the table when it went too.
	edges, err := t.edgesOf(columnTables, graph, false, store.Incoming)
	if err != nil {
		return nil, fmt.Errorf("reading users of column tables: %w", err)
	}
	if fromChangeSet {
		removed, err := t.edgesOf(columnTables, graph, true, store.Incoming)
		if err != nil {
			return nil, fmt.Errorf("reading removed users of column tables: %w", err)
		}
		edges = append(edges, removed...)
	}
	var out []candidate
	for _, e := range edges {
		if !tableAccessKinds[e.Kind] {
			continue
		}
		f, err := followOf("I8", e.Kind)
		if err != nil {
			return nil, err
		}
		for _, col := range columnsOf[e.Target] {
			out = append(out, candidate{from: col, edge: e, dir: store.Incoming, f: f, columnUse: true})
		}
	}
	return out, nil
}

// accessOwners reads the owner nodes of a step's I8 candidates, for their
// table_access entries.
func (t *traversal) accessOwners(cands []candidate, graph string) (map[string]*contract.Node, error) {
	seen := map[string]bool{}
	var ids []string
	for _, cd := range cands {
		if cd.f.ID == "I8" && !seen[cd.edge.Source] && t.visited[cd.edge.Source] == nil {
			seen[cd.edge.Source] = true
			ids = append(ids, cd.edge.Source)
		}
	}
	return t.nodesOf(graph, ids)
}

// accessUnknownSymbolKinds are the code symbol kinds an owner of
// table_access_unknown entries can have (T018h W1: a code symbol or the
// file node; the file nodes' entries come with the file index).
func accessUnknownSymbolKinds() []string {
	var kinds []string
	for k := range symbolKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds
}

// UnknownUses holds a repository's table_access_unknown entries: those
// whose candidates name tables, by table, and the unbound ones (candidates
// null). MayAlsoUse is the one "may also be used by" rule over them, used
// by change impact (I8) and the MCP table tool (T030 review).
type UnknownUses struct {
	named   map[string][]UnknownUse
	unbound []UnknownUse
}

func newUnknownUses() *UnknownUses { return &UnknownUses{named: map[string][]UnknownUse{}} }

// clone copies u, so a cached file index is never changed by the symbol
// nodes added to it.
func (u *UnknownUses) clone() *UnknownUses {
	out := newUnknownUses()
	for table, list := range u.named {
		out.named[table] = append([]UnknownUse(nil), list...)
	}
	out.unbound = append([]UnknownUse(nil), u.unbound...)
	return out
}

// add adds n's table_access_unknown entries: once per candidate table they
// name, or once as unbound when the candidates are null.
func (u *UnknownUses) add(n *contract.Node) {
	list, _ := n.Attributes["table_access_unknown"].([]any)
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		e := UnknownUse{Owner: n.Identity, Provenance: "inferred"}
		e.Row, _ = m["row"].(string)
		e.Tool, _ = m["tool"].(string)
		e.Reason, _ = m["reason"].(string)
		e.Partial, _ = m["partial"].(bool)
		e.Line = intOf(m["line"])
		if _, listed := m["candidates"].([]any); !listed {
			e.Unbound = true
			u.unbound = append(u.unbound, e)
			continue
		}
		for _, table := range stringList(m["candidates"]) {
			u.named[table] = append(u.named[table], e)
		}
	}
}

// modelBoundTools are the entry tools whose rows resolve a site against
// their own model group only (CT-P SQLModel, CT-R Prisma, CT-Z Drizzle):
// an unbound entry of one of them can reach only a model table of that
// tool. An entry of any other tool (sql text over any group, sqlx over
// migration sets, supabase over generated types or a migration set, a tool
// added later) may reach any table.
var modelBoundTools = map[string]bool{"sqlmodel": true, "prisma": true, "drizzle": true}

// unboundReaches reports whether an unbound entry of tool may use table:
// over-reported rather than missed (the entry says only "a table of the
// tool's sources in the package", and the package is not on the entry).
func unboundReaches(tool string, table *contract.Node) bool {
	if !modelBoundTools[tool] {
		return true
	}
	if table == nil || table.Attributes["source"] != "model" {
		return false
	}
	t, _ := table.Attributes["tool"].(string)
	return t == tool
}

// MayAlsoUse is the "may also be used by" list of a table: the entries
// whose candidates name it and the unbound entries that may reach it
// (unboundReaches), without the owners already found, sorted by owner and
// line, named entries first for the same owner and line. table may be a
// node read from a base graph; its identity names it.
func (u *UnknownUses) MayAlsoUse(table *contract.Node, found map[string]bool) []UnknownUse {
	out := []UnknownUse{}
	if u == nil || table == nil {
		return out
	}
	for _, e := range u.named[table.Identity] {
		if !found[e.Owner] {
			out = append(out, e)
		}
	}
	for _, e := range u.unbound {
		if !found[e.Owner] && unboundReaches(e.Tool, table) {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return !a.Unbound && b.Unbound
	})
	return out
}

// unknownUses returns graph's table_access_unknown entries, read once per
// comparison and graph (the graph is fixed, G6), by Prepare in the
// background or by the first traversal that reaches a table.
func (c *Comparison) unknownUses(ctx context.Context, s store.Store, graph string) (*UnknownUses, error) {
	return buildOnce(ctx, &c.mu, c.unknowns, graph, "reading table_access_unknown entries", func(ctx context.Context) (*UnknownUses, error) {
		fi, err := c.fileIndex(ctx, s, graph)
		if err != nil {
			return nil, err
		}
		return readUnknownUses(ctx, s, graph, fi.unknowns)
	})
}

// readUnknownUses reads the table_access_unknown entries of graph's code
// symbol nodes (only the nodes that store the attribute) and adds them to
// a copy of the file nodes' entries.
func readUnknownUses(ctx context.Context, s store.Store, graph string, files *UnknownUses) (*UnknownUses, error) {
	index := files.clone()
	for _, kind := range accessUnknownSymbolKinds() {
		nodes, err := s.Nodes(ctx, graph, store.Query{Kind: kind, Attribute: "table_access_unknown"})
		if err != nil {
			return nil, fmt.Errorf("reading table_access_unknown entries: %w", err)
		}
		for i := range nodes {
			index.add(&nodes[i])
		}
	}
	return index, nil
}

// ReadUnknownUses reads repository's table_access_unknown entries (code
// symbols and file nodes that store the attribute), for a caller outside
// a comparison: the MCP table tool, whose "may also be used by" is then
// MayAlsoUse, the same rule as change impact's.
func ReadUnknownUses(ctx context.Context, s store.Store, repository string) (*UnknownUses, error) {
	files := newUnknownUses()
	nodes, err := s.Nodes(ctx, repository, store.Query{Kind: "file", Attribute: "table_access_unknown"})
	if err != nil {
		return nil, fmt.Errorf("reading table_access_unknown entries: %w", err)
	}
	for i := range nodes {
		files.add(&nodes[i])
	}
	return readUnknownUses(ctx, s, repository, files)
}

// intOf reads a JSON number decoded either as float64 or json.Number.
func intOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case interface{ Int64() (int64, error) }:
		n, _ := x.Int64()
		return int(n)
	}
	return 0
}

// tableUses builds the per-table answers of the tables whose users the
// traversal read: the owners found (whether or not their path was the
// shortest to them) and the unknown entries of other owners naming the
// table, sorted by owner and line.
func (t *traversal) tableUses(out *ImpactResult) error {
	tables := make([]string, 0, len(t.useTables))
	for table := range t.useTables {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		graph := t.useTables[table]
		index, err := t.c.unknownUses(t.ctx, t.s, graph)
		if err != nil {
			return err
		}
		nodes, err := t.nodesOf(graph, []string{table})
		if err != nil {
			return err
		}
		node := nodes[table]
		if node == nil {
			node = &contract.Node{Identity: table}
		}
		found := t.uses[table]
		out.TableUses = append(out.TableUses, TableUse{Table: table, Wording: "uses found", Found: sortedKeys(found),
			MayAlsoWording: "may also be used by", MayAlsoBeUsedBy: index.MayAlsoUse(node, found)})
	}
	return nil
}

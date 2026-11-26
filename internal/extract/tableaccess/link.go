package tableaccess

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Input is what the link pass reads: cached facts and retained sources,
// never a tree, and the schema records the schema front ends emitted.
type Input struct {
	// Rust holds the cached facts of every Rust file (the sqlx payload,
	// RustFacts.Queries, plus scopes, imports and calls).
	Rust map[string]facts.Facts
	// Python holds the cached facts of every Python file (the model
	// payload's Queries and Annotations, scopes and calls).
	Python map[string]facts.Facts
	// Resolver resolves Python names (imports, aliases, class bindings).
	Resolver *python.ModuleResolver
	// Sources are the retained sources (Cargo.toml discovery, W7; the
	// source before a Python text() call).
	Sources map[string][]byte
	// Schema are the migration-set records of internal/extract/schema;
	// SchemaEdges its edges (contains and foreign_key, which the Supabase
	// embed rule SB-T3 reads).
	Schema      []contract.Node
	SchemaEdges []contract.Edge
	// Models are the model-group records (the SQLModel tables of T018c2).
	Models []contract.Node
	// TypeScript holds the cached facts of every TypeScript and JavaScript
	// file (the ungated call cache, facts.ClientCall, with its scopes).
	TypeScript map[string]facts.Facts
	// Prisma are the Prisma group's table records (T018g1); the relation
	// fields come from the Prisma front end's accessor over Sources.
	Prisma []contract.Node
	// Drizzle are the Drizzle group's table and column records (T018g2);
	// DrizzleModels the cached TypeScript model payloads and Modules the
	// shared TypeScript module resolver, which drizzle.Discover and the
	// table const resolution read.
	Drizzle       []contract.Node
	DrizzleModels map[string]*typescript.ModelSyntax
	Modules       *typescript.ModuleResolver
	// SupabaseTypes are the table records read from Supabase's generated
	// types (T024c3, group tool supabase_types): the schema source of a
	// package without a Supabase migration set (row SB-T1).
	SupabaseTypes []contract.Node
	// Owners maps an owner identity that is not emitted to the node that
	// stands for it: a declaration of a TypeScript file tree-sitter
	// rejects (row TS-F2 of docs/lang/typescript-structure.md) to its
	// file node. The sites keep everything else.
	Owners map[string]string
	// Tag, when set, receives the row tag of every branch taken
	// ("<row>" or "<row>/<form>"); the contract tests record them.
	Tag func(string)
}

// Output is the link result: plain contract v1 edges, and the attributes
// to merge into existing owner nodes (identity -> attribute -> value).
type Output struct {
	Edges      []contract.Edge
	Attributes map[string]map[string]any
	// Diagnostics are the table_access_internal_error records of files
	// whose link step failed (R5-B3); the other files are linked.
	Diagnostics []contract.Diagnostic
}

// Access is one table_access entry: the data of one edge that contract v1
// edges cannot carry (Revision 2). Consumers join it with the edge by
// (kind, target).
type Access struct {
	Kind         string   `json:"kind"`
	Target       string   `json:"target"`
	Tool         string   `json:"tool"`
	Rows         []string `json:"rows"`
	Operations   []string `json:"operations"`
	Assumes      []string `json:"assumes,omitempty"`
	Role         string   `json:"role"`
	Verification string   `json:"verification"`
}

// Unknown is one table_access_unknown entry (W5): a site a row recognises
// but cannot bind. Candidates is nil when the site may touch any table of
// the tool's sources in the package.
type Unknown struct {
	Row        string        `json:"row"`
	Line       int           `json:"line"`
	Span       contract.Span `json:"span"`
	Tool       string        `json:"tool"`
	Reason     string        `json:"reason"`
	Partial    bool          `json:"partial"`
	Candidates []string      `json:"candidates"`
}

// Roles (Revision 1, O2).
const (
	RoleBuilds   = "builds"
	RoleExecutes = "executes"
)

// inferredConfidence is the clients linker's exact-match level, used for
// every inferred edge.
const inferredConfidence = 0.9

// site is one claimed or refused carrier site.
type site struct {
	file         string
	span         contract.Span
	owner        string
	row          string
	tool         string
	rows         []string
	fact         bool
	assumes      []string
	role         string
	verification string
	reads        map[string]map[string]bool
	writes       map[string]map[string]bool
	unknown      *Unknown
	// extra are further unknown entries of the site (a Prisma site's
	// relation_write_side and site_partial entries, Revision 3).
	extra []Unknown
	// For the oracle: the SQL text, the package's sets and the claimed
	// identities' schema-qualified names.
	sql    string
	sets   []*migrationSet
	tables map[string]string
	// builder and scope are a Drizzle builder site's query text (without
	// a trailing then, catch, execute, as or prepare) and its call scope,
	// which the toSQL oracle evaluates.
	builder string
	scope   int
	// receiver and clientSchema are a Supabase site's receiver text and
	// its client's default schema, which the postgrest-js oracle reads.
	receiver, clientSchema string
	// consts are the bounded consts a Supabase chain names (name ->
	// literal), which the oracle binds.
	consts map[string]string
	// bound is a chain whose receiver is a const builder binding (T024
	// review): the oracle replaces the binding's name at the start of
	// builder by the binding's value text (T024 review).
	bound *constBound
}

// constBound is a const binding of a Supabase query builder a chain
// starts with: its name and its value's source text.
type constBound struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// analysis is the link pass before aggregation; the oracle test reads the
// sites.
type analysis struct {
	sites       []site
	unknowns    map[string][]Unknown
	diagnostics []contract.Diagnostic
}

// Link runs every implemented row and aggregates the result (W2).
func Link(in Input) Output {
	a := analyze(in)
	a.reanchor(in.Owners)
	out := aggregate(a)
	out.Diagnostics = a.diagnostics
	return out
}

// reanchor moves sites and unknown entries of an owner that is not emitted
// to the node that stands for it (Input.Owners), so aggregate groups them
// there.
func (a *analysis) reanchor(owners map[string]string) {
	if len(owners) == 0 {
		return
	}
	for i := range a.sites {
		if to, ok := owners[a.sites[i].owner]; ok {
			a.sites[i].owner = to
		}
	}
	for owner, list := range a.unknowns {
		if to, ok := owners[owner]; ok {
			a.unknowns[to] = append(a.unknowns[to], list...)
			delete(a.unknowns, owner)
		}
	}
}

// InternalErrorCode is the diagnostic of a file whose link step failed.
const InternalErrorCode = "table_access_internal_error"

// stepHook, when set (tests only), runs at the start of every per-file
// step with the step name and the file, so a test can make one fail.
var stepHook func(step, file string)

// guard runs one per-file step in isolation (AGENTS.md, R5-B3): the step
// writes into its own analysis, merged only when it completes; a panic
// discards the file's partial result and becomes a diagnostic, and the
// other files are still linked.
func (a *analysis) guard(file, step string, fn func(*analysis)) {
	part := analysis{unknowns: map[string][]Unknown{}}
	defer func() {
		if r := recover(); r != nil {
			a.diagnostics = append(a.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: InternalErrorCode,
				Message: fmt.Sprintf("%s: table access could not be linked (%s step): %v", file, step, r)})
			return
		}
		a.sites = append(a.sites, part.sites...)
		for owner, list := range part.unknowns {
			a.unknowns[owner] = append(a.unknowns[owner], list...)
		}
	}()
	if stepHook != nil {
		stepHook(step, file)
	}
	fn(&part)
}

func (in Input) tag(t string) {
	if in.Tag != nil {
		in.Tag(t)
	}
}

func analyze(in Input) analysis {
	a := analysis{unknowns: map[string][]Unknown{}}
	sets := migrationSets(in.Schema)
	byPackage := map[string][]*migrationSet{}
	var names []string
	for name := range sets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := sets[name]
		byPackage[s.pkg] = append(byPackage[s.pkg], s)
	}
	for _, file := range facts.Paths(in.Rust) {
		f := in.Rust[file]
		if f.Rust == nil {
			continue
		}
		a.guard(file, "sqlx", func(part *analysis) {
			sqlxSites := linkSqlxFile(in, file, f, byPackage, part)
			unknownExecution(in, file, f, sqlxSites, part)
		})
	}
	models := sqlmodelTables(in.Models)
	group := sqlmodelGroup(models)
	for _, file := range facts.Paths(in.Python) {
		f := in.Python[file]
		if f.Python == nil || f.Python.Models == nil || !f.Python.ModelsValid() {
			continue
		}
		a.guard(file, "sqlmodel", func(part *analysis) {
			spans := linkPythonFile(in, file, f, models, group, part)
			unknownPythonExecution(in, f, spans, part)
		})
	}
	if groups := prismaGroups(in.Prisma, in.Sources); len(groups) > 0 {
		for _, file := range facts.Paths(in.TypeScript) {
			a.guard(file, "prisma", func(part *analysis) {
				linkPrismaFile(in, file, in.TypeScript[file], groups, part)
			})
		}
	}
	if l := newDrizzleLinker(in); l != nil {
		for _, file := range facts.Paths(in.TypeScript) {
			a.guard(file, "drizzle", func(part *analysis) {
				linkDrizzleFile(l, file, in.TypeScript[file], part)
			})
		}
	}
	sb := newSupabaseLinker(in)
	for _, file := range facts.Paths(in.TypeScript) {
		a.guard(file, "supabase", func(part *analysis) {
			linkSupabaseFile(sb, file, in.TypeScript[file], part)
		})
	}
	return a
}

// sqlxExecutions are the methods that run a sqlx query (Query, QueryAs,
// QueryScalar and the Executor trait).
var sqlxExecutions = map[string]bool{"fetch": true, "fetch_one": true, "fetch_optional": true, "fetch_all": true, "fetch_many": true, "execute": true, "execute_many": true}

func contains(outer, inner contract.Span) bool {
	before := func(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 <= c2 }
	return before(outer.StartLine, outer.StartColumn, inner.StartLine, inner.StartColumn) &&
		before(inner.EndLine, inner.EndColumn, outer.EndLine, outer.EndColumn)
}

// importsSqlx reports whether a Rust file has a use declaration of sqlx.
func importsSqlx(f facts.Facts) bool {
	for _, imp := range f.Imports {
		m := strings.TrimPrefix(imp.Module, "::")
		if m == "sqlx" || strings.HasPrefix(m, "sqlx::") {
			return true
		}
	}
	return false
}

// unknownExecution is row D's branch for executions (Revision 2): in a
// file that imports sqlx, a call of a sqlx execution method whose span
// holds no captured query site runs a statement whose table is not traced
// here; it adds an unknown record on its owner.
func unknownExecution(in Input, file string, f facts.Facts, sites []contract.Span, a *analysis) {
	if !importsSqlx(f) && len(sites) == 0 {
		return
	}
	for _, c := range f.Calls {
		if !sqlxExecutions[c.StaticName] || c.Source == "" {
			continue
		}
		traced := false
		for _, s := range sites {
			traced = traced || contains(c.Span, s)
		}
		if traced {
			continue
		}
		in.tag("D")
		a.unknowns[c.Source] = append(a.unknowns[c.Source], Unknown{Row: "D", Line: c.Span.StartLine, Span: c.Span, Tool: "sqlx", Reason: ReasonStatementNotTraced})
	}
}

// aggregate builds one edge per (kind, owner, table) with every site as
// evidence, and the owners' attributes.
func aggregate(a analysis) Output {
	type key struct{ kind, owner, target string }
	type group struct {
		evidence     []contract.Evidence
		tools, rows  map[string]bool
		ops, assumes map[string]bool
		fact         bool
		executes     bool
		oracle       bool
	}
	groups := map[key]*group{}
	add := func(kind string, s site, target string, ops map[string]bool) {
		k := key{kind, s.owner, target}
		g := groups[k]
		if g == nil {
			g = &group{tools: map[string]bool{}, rows: map[string]bool{}, ops: map[string]bool{}, assumes: map[string]bool{}, oracle: true}
			groups[k] = g
		}
		g.evidence = append(g.evidence, contract.Evidence{File: s.file, Span: s.span})
		g.tools[s.tool] = true
		for _, r := range s.rows {
			g.rows[r] = true
		}
		for op := range ops {
			g.ops[op] = true
		}
		if s.fact {
			g.fact = true
		} else {
			for _, x := range s.assumes {
				g.assumes[x] = true
			}
		}
		g.executes = g.executes || s.role == RoleExecutes
		g.oracle = g.oracle && s.verification == VerificationOracle
	}
	unknowns := map[string][]Unknown{}
	for owner, list := range a.unknowns {
		unknowns[owner] = append(unknowns[owner], list...)
	}
	for _, s := range a.sites {
		for target, ops := range s.reads {
			add("reads_table", s, target, ops)
		}
		for target, ops := range s.writes {
			add("writes_table", s, target, ops)
		}
		if s.unknown != nil {
			unknowns[s.owner] = append(unknowns[s.owner], *s.unknown)
		}
		if len(s.extra) > 0 {
			unknowns[s.owner] = append(unknowns[s.owner], s.extra...)
		}
	}
	out := Output{Attributes: map[string]map[string]any{}}
	accesses := map[string][]Access{}
	for k, g := range groups {
		sort.Slice(g.evidence, func(i, j int) bool { return evidenceLess(g.evidence[i], g.evidence[j]) })
		g.evidence = dedupEvidence(g.evidence)
		e := contract.Edge{Type: "edge", Kind: k.kind, Source: k.owner, Target: k.target, Provenance: "fact", Confidence: 1, Evidence: g.evidence}
		if !g.fact {
			e.Provenance, e.Confidence = "inferred", inferredConfidence
		}
		out.Edges = append(out.Edges, e)
		access := Access{Kind: k.kind, Target: k.target, Tool: strings.Join(sortedSet(g.tools), ","), Rows: sortedSet(g.rows), Operations: sortedSet(g.ops), Role: RoleBuilds, Verification: VerificationMapped}
		if !g.fact {
			access.Assumes = sortedSet(g.assumes)
		}
		if g.executes {
			access.Role = RoleExecutes
		}
		if g.oracle {
			access.Verification = VerificationOracle
		}
		accesses[k.owner] = append(accesses[k.owner], access)
	}
	sort.Slice(out.Edges, func(i, j int) bool {
		a, b := out.Edges[i], out.Edges[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Target < b.Target
	})
	for owner, list := range accesses {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Kind != list[j].Kind {
				return list[i].Kind < list[j].Kind
			}
			return list[i].Target < list[j].Target
		})
		attrs(out, owner)["table_access"] = list
	}
	for owner, list := range unknowns {
		sort.SliceStable(list, func(i, j int) bool {
			x, y := list[i], list[j]
			if x.Span != y.Span {
				return evidenceLess(contract.Evidence{Span: x.Span}, contract.Evidence{Span: y.Span})
			}
			if x.Row != y.Row {
				return x.Row < y.Row
			}
			return x.Reason < y.Reason
		})
		attrs(out, owner)["table_access_unknown"] = list
	}
	return out
}

func attrs(out Output, owner string) map[string]any {
	m := out.Attributes[owner]
	if m == nil {
		m = map[string]any{}
		out.Attributes[owner] = m
	}
	return m
}

func evidenceLess(a, b contract.Evidence) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	x, y := a.Span, b.Span
	if x.StartLine != y.StartLine {
		return x.StartLine < y.StartLine
	}
	if x.StartColumn != y.StartColumn {
		return x.StartColumn < y.StartColumn
	}
	if x.EndLine != y.EndLine {
		return x.EndLine < y.EndLine
	}
	return x.EndColumn < y.EndColumn
}

func dedupEvidence(list []contract.Evidence) []contract.Evidence {
	var out []contract.Evidence
	for i, e := range list {
		if i > 0 && e == list[i-1] {
			continue
		}
		out = append(out, e)
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

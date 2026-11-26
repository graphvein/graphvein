package tableaccess

import (
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/models/drizzle"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file holds rows CT-Z1 (select builders), CT-Z2 (insert, update and
// delete builders), CT-Z3 (relational queries) and CT-Z4 (sql templates
// run by execute) over the cached TypeScript calls (facts.ClientCall) and
// the Drizzle group's table records (T018g2, ADR 0059). Table consts are
// resolved through the shared TypeScript module resolver (imports,
// aliases, export * barrels, namespace members, single-assignment local
// consts, alias(t, "a")). Every Drizzle site is inferred: the database or
// transaction receiver is not proven (TA-V3b, Revision 1 O1).

// drizzleTable is one claimed Drizzle table as the link pass reads it.
type drizzleTable struct {
	identity, file, decl string
	// name and schema are the database names (schema "public" for a
	// table without pgSchema); schemaKnown is the record's schema_known.
	name, schema string
	schemaKnown  bool
	// relations maps an orm_relations entry name to its target identity
	// ("" when the target is not a claimed table).
	relations map[string]string
	// columns maps a column's property key to its database name.
	columns map[string]string
}

// qualified is the table's schema-qualified name (the oracle's spelling).
func (t *drizzleTable) qualified() string { return t.schema + "." + t.name }

// rendered is how drizzle-orm writes the table into an sql template
// (sql.js buildQueryFromSourceParams: the name, schema-qualified only for
// a table with a schema of its own).
func (t *drizzleTable) rendered() string {
	if t.schema == "" || t.schema == "public" {
		return strconv.Quote(t.name)
	}
	return strconv.Quote(t.schema) + "." + strconv.Quote(t.name)
}

// drizzleGroup is one Drizzle model group (one drizzle-kit config).
type drizzleGroup struct {
	set, dialect string
	config       *drizzle.Config
	// packages are the package directories whose sites resolve against
	// the group (W7): the config's package and the packages holding its
	// tables' files.
	packages map[string]bool
	tables   map[string]*drizzleTable
	// source is the group as a schema source for CT-Z4 (CT-Q2).
	source *migrationSet
	keys   map[string][]string
}

// drizzleLinker resolves table consts for every file of a repository.
type drizzleLinker struct {
	in Input
	// moduleFiles are the groups' schema module files (W6: not sites).
	moduleFiles map[string]bool
	groups      []*drizzleGroup
	byTable     map[string]*drizzleGroup
}

// newDrizzleLinker reads the Drizzle groups from the table and column
// records and the configs drizzle.Discover proves; nil when there is no
// claimed Drizzle table.
func newDrizzleLinker(in Input) *drizzleLinker {
	bySet := map[string]*drizzleGroup{}
	var sets []string
	tables := map[string]*drizzleTable{}
	for _, n := range in.Drizzle {
		if n.Kind != "table" || n.Attributes["tool"] != drizzle.Tool {
			continue
		}
		set, _ := n.Attributes["set"].(string)
		g := bySet[set]
		if g == nil {
			g = &drizzleGroup{set: set, packages: map[string]bool{}, tables: map[string]*drizzleTable{}, keys: map[string][]string{}}
			g.source = &migrationSet{name: set, dialect: "postgres"}
			bySet[set] = g
			sets = append(sets, set)
		}
		g.dialect, _ = n.Attributes["dialect"].(string)
		t := &drizzleTable{identity: n.Identity, file: n.File, decl: n.QualifiedName, relations: map[string]string{}, columns: map[string]string{}}
		t.name, _ = n.Attributes["name"].(string)
		t.schema, _ = n.Attributes["schema"].(string)
		t.schemaKnown, _ = n.Attributes["schema_known"].(bool)
		for _, r := range relationList(n.Attributes["orm_relations"]) {
			name, _ := r["name"].(string)
			target, _ := r["target"].(string)
			t.relations[name] = target
		}
		g.tables[n.Identity] = t
		tables[n.Identity] = t
		schema := t.schema
		if schema == "public" {
			schema = ""
		}
		g.source.tables = append(g.source.tables, table{identity: n.Identity, schema: schema, name: t.name, pathUnproven: !t.schemaKnown})
		if dir, ok := npmPackage(n.File, in.Sources); ok {
			g.packages[dir] = true
		}
	}
	if len(sets) == 0 {
		return nil
	}
	for _, n := range in.Drizzle {
		if n.Kind != "column" {
			continue
		}
		// column:<file>#<const>.<property>; the file holds dots too.
		file, rest, ok := strings.Cut(strings.TrimPrefix(n.Identity, "column:"), "#")
		decl, prop, ok2 := strings.Cut(rest, ".")
		if t := tables["table:"+file+"#"+decl]; ok && ok2 && t != nil {
			t.columns[prop] = n.Name
		}
	}
	l := &drizzleLinker{in: in, byTable: map[string]*drizzleGroup{}, moduleFiles: map[string]bool{}}
	if in.Modules != nil {
		pkgs, _ := drizzle.Discover(drizzle.Input{TypeScript: in.TypeScript, Models: in.DrizzleModels, Resolver: in.Modules, Sources: in.Sources})
		for _, p := range pkgs {
			for _, c := range p.Configs {
				if g := bySet[c.SetKey()]; g != nil && c.Proven() {
					g.config = c
					g.packages[c.Package.Dir] = true
					for _, m := range c.Modules {
						l.moduleFiles[m.File] = true
					}
					g.source.drizzleRun = drizzleRun(c)
				}
			}
		}
	}
	sort.Strings(sets)
	for _, set := range sets {
		g := bySet[set]
		sort.Slice(g.source.tables, func(i, j int) bool { return g.source.tables[i].identity < g.source.tables[j].identity })
		l.groups = append(l.groups, g)
		for id := range g.tables {
			l.byTable[id] = g
		}
	}
	return l
}

// relationList reads orm_relations as the front end builds it
// ([]map[string]any) or as JSON decodes it ([]any).
func relationList(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		return list
	case []any:
		var out []map[string]any
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// packageGroups returns the groups a file's sites resolve against (W7).
func (l *drizzleLinker) packageGroups(file string) []*drizzleGroup {
	dir, ok := npmPackage(file, l.in.Sources)
	if !ok {
		return nil
	}
	var out []*drizzleGroup
	for _, g := range l.groups {
		if g.packages[dir] {
			out = append(out, g)
		}
	}
	return out
}

// tableRef is what an expression in a table position names.
type tableRef struct {
	// table is the claimed table the expression names (nil when it names
	// none); alias is set when it is alias(t, "a").
	table *drizzleTable
	alias string
	// column is set when the expression is a member of a table const
	// (t.col), which names a column, not a table.
	column string
	// detail says why the expression names no table; form is the CT-Z1
	// refusal form it is (".as(\"x\")", "$with", "a view", "a parameter").
	detail, form string
	// structural is set when the expression builds SQL (an sql fragment,
	// a drizzle-orm operator, a query builder): rendered into a template
	// it is not a bound parameter (CT-Z4).
	structural bool
	// namespace is the file of a namespace import (import * as schema).
	namespace string
	// object is a const object literal the expression names (in
	// objectFile at objectScope), whose properties a member reads.
	object      *facts.RouteExpr
	objectFile  string
	objectScope int
}

// drizzleORMModule reports a drizzle-orm module specifier.
func drizzleORMModule(m string) bool {
	return m == "drizzle-orm" || strings.HasPrefix(m, "drizzle-orm/")
}

// viewBuilders are drizzle-orm's view builders: a view is no table.
var viewBuilders = map[string]bool{"pgView": true, "pgMaterializedView": true, "sqliteView": true, "mysqlView": true}

// binding finds the nearest binding of a name from a scope of a file and
// the scope that holds it.
func (l *drizzleLinker) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
	r := l.in.TypeScript[file].Routes
	if r == nil {
		return facts.RouteBinding{}, 0, false
	}
	for s := scope; s >= 0 && s < len(r.Scopes); s = r.Scopes[s].Parent {
		if b, ok := r.Scopes[s].Bindings[name]; ok {
			return b, s, true
		}
		if r.Scopes[s].Parent == s {
			break
		}
	}
	return facts.RouteBinding{}, 0, false
}

// resolve reads an expression of a file at a scope as a table position.
func (l *drizzleLinker) resolve(file string, scope int, e facts.RouteExpr, depth int) tableRef {
	if depth > 8 {
		return tableRef{detail: "a binding chain too deep to follow"}
	}
	if l.in.Modules == nil {
		return tableRef{detail: "no module resolver"}
	}
	switch e.Kind {
	case "identifier":
		b, at, ok := l.binding(file, scope, e.Name)
		if !ok {
			return tableRef{detail: e.Name + " is not bound in the file"}
		}
		if b.Module != "" {
			if drizzleORMModule(b.Module) {
				return tableRef{detail: e.Name + " is imported from " + b.Module, structural: true}
			}
			if b.Export == "*" {
				res := l.in.Modules.Resolve(file, b.Module)
				return tableRef{namespace: res.File, detail: e.Name + " is a namespace import"}
			}
			return l.export(l.in.Modules.ImportBinding(file, b.Module, b.Export, ""), e.Name, depth)
		}
		if b.Value.Kind == "parameter" {
			return tableRef{detail: "a parameter " + e.Name, form: "a parameter"}
		}
		if at == 0 {
			if t := l.declared(file, e.Name); t != nil {
				return tableRef{table: t}
			}
		}
		if b.Value.Kind == "" || b.Value.Kind == "function" || b.Value.Kind == "ambiguous" || b.Value.Kind == "static_class" {
			return tableRef{detail: "a binding " + e.Name + " the caches do not follow"}
		}
		if b.Value.Kind == "object" {
			v := b.Value
			return tableRef{object: &v, objectFile: file, objectScope: at, detail: e.Name + " is an object"}
		}
		r := l.resolve(file, at, b.Value, depth+1)
		if r.table == nil && r.detail != "" && !strings.HasPrefix(r.detail, e.Name) {
			r.detail = e.Name + ": " + r.detail
		}
		return r
	case "member_expression":
		if e.Object == nil {
			return tableRef{detail: "a member " + e.Text}
		}
		base := l.resolve(file, scope, *e.Object, depth+1)
		switch {
		case base.namespace != "":
			return l.export(l.in.Modules.Export(base.namespace, e.Name), e.Text, depth)
		case base.table != nil && base.column == "":
			return tableRef{table: base.table, alias: base.alias, column: e.Name, detail: e.Text + " is a column"}
		case base.structural:
			return tableRef{detail: e.Text + " is a drizzle-orm member", structural: true}
		case base.object != nil:
			// A property of a const object literal (kaneo's
			// `export const schema = { userTable, ... }`): its value.
			if v, ok := base.object.Properties[e.Name]; ok && !base.object.Spread && !base.object.Invalid {
				return l.resolve(base.objectFile, base.objectScope, v, depth+1)
			}
		}
		return tableRef{detail: "a member " + e.Text + " that names no table const"}
	case "call_expression":
		return l.resolveCall(file, scope, e, depth)
	case "binary_expression":
		// sql<T>`...`: the TypeScript grammar reads a generic tagged
		// template as (sql < T) > `...`; the template is not read (known
		// gap), but it is SQL.
		left := e
		for left.Kind == "binary_expression" && len(left.Parts) > 0 {
			left = left.Parts[0]
		}
		if left.Kind == "identifier" && left.Offset == e.Offset {
			if b, _, ok := l.binding(file, scope, left.Name); ok && b.Module == "drizzle-orm" && b.Export == "sql" {
				return tableRef{detail: "a generic sql<T> template", structural: true}
			}
		}
	}
	return tableRef{detail: "a " + strings.ReplaceAll(e.Kind, "_", " ")}
}

// resolveCall reads a call in a table position: alias(t, "a") names t; a
// view builder, a subquery (.as), a $with CTE and anything else name no
// table.
func (l *drizzleLinker) resolveCall(file string, scope int, e facts.RouteExpr, depth int) tableRef {
	recv, links := callChain(e)
	if len(links) == 0 {
		recv = e
	}
	if recv.Kind == "call_expression" && recv.Object != nil && recv.Object.Kind == "identifier" {
		if b, _, ok := l.binding(file, scope, recv.Object.Name); ok && drizzleORMModule(b.Module) && viewBuilders[b.Export] {
			return tableRef{detail: "a view " + b.Export + "(...)", form: "a view", structural: true}
		}
	}
	for _, k := range links {
		if k.name == "$with" {
			return tableRef{detail: "a $with CTE", form: "$with", structural: true}
		}
	}
	if len(links) > 0 && links[len(links)-1].name == "as" {
		return tableRef{detail: "a subquery " + strings.TrimSpace(firstLine(e.Text)), form: `.as("x")`, structural: true}
	}
	if isBuilderChain(links) {
		return tableRef{detail: "a query builder", structural: true}
	}
	if e.Object == nil {
		return tableRef{detail: "a call"}
	}
	callee := l.resolve(file, scope, *e.Object, depth+1)
	name := e.Object.Name
	if e.Object.Kind == "identifier" {
		if b, _, ok := l.binding(file, scope, name); ok && b.Module != "" && drizzleORMModule(b.Module) {
			switch {
			case b.Export == "alias" && len(e.Args) >= 1:
				r := l.resolve(file, scope, e.Args[0], depth+1)
				if r.table == nil || r.column != "" {
					return tableRef{detail: "alias of " + e.Args[0].Text + ": " + r.detail}
				}
				r.alias = "alias"
				if len(e.Args) > 1 && e.Args[1].Kind == "literal" {
					r.alias = e.Args[1].Name
				}
				return r
			}
			return tableRef{detail: "a drizzle-orm call " + b.Export, structural: true}
		}
	}
	if callee.structural {
		return tableRef{detail: "a drizzle-orm call", structural: true}
	}
	return tableRef{detail: "a call " + strings.TrimSpace(firstLine(e.Object.Text))}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " ..."
	}
	return s
}

// export follows a resolved export to the declaration it names.
func (l *drizzleLinker) export(b typescript.ExportBinding, name string, depth int) tableRef {
	switch {
	case b.Ambiguous:
		return tableRef{detail: name + " is exported by two export * barrels"}
	case b.External:
		if drizzleORMModule(b.File) {
			return tableRef{detail: name + " is imported from " + b.File, structural: true}
		}
		return tableRef{detail: name + " is imported from the package " + b.File}
	case b.Namespace:
		return tableRef{namespace: b.File, detail: name + " is a namespace"}
	case b.File == "":
		return tableRef{detail: name + " resolves to no file of the repository"}
	}
	if t := l.declared(b.File, b.Name); t != nil {
		return tableRef{table: t}
	}
	r := l.in.TypeScript[b.File].Routes
	if r == nil || len(r.Scopes) == 0 {
		return tableRef{detail: name + " names no table const"}
	}
	v, ok := r.Scopes[0].Bindings[b.Name]
	if !ok || v.Value.Kind == "" || v.Module != "" {
		return tableRef{detail: name + " names no table const"}
	}
	if v.Value.Kind == "object" {
		o := v.Value
		return tableRef{object: &o, objectFile: b.File, objectScope: 0, detail: name + " is an object"}
	}
	return l.resolve(b.File, 0, v.Value, depth+1)
}

// declared returns the claimed table a module-level const of a file is.
func (l *drizzleLinker) declared(file, name string) *drizzleTable {
	g := l.byTable["table:"+file+"#"+name]
	if g == nil {
		return nil
	}
	return g.tables["table:"+file+"#"+name]
}

// chainLink is one method call of a call chain.
type chainLink struct {
	name string
	args []facts.RouteExpr
	call facts.RouteExpr
}

// callChain splits `r.m1(...).m2(...)` into its receiver r and its method
// calls in source order.
func callChain(e facts.RouteExpr) (facts.RouteExpr, []chainLink) {
	var links []chainLink
	for e.Kind == "call_expression" && e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil {
		links = append([]chainLink{{name: e.Object.Name, args: e.Args, call: e}}, links...)
		e = *e.Object.Object
	}
	return e, links
}

// selectMethods start a select builder (CT-Z1).
var selectMethods = map[string]bool{"select": true, "selectDistinct": true, "selectDistinctOn": true}

// selectJoins are the joins of a select builder (pg-core select.js).
var selectJoins = map[string]bool{
	"leftJoin": true, "rightJoin": true, "innerJoin": true, "fullJoin": true, "crossJoin": true,
	"leftJoinLateral": true, "innerJoinLateral": true, "crossJoinLateral": true,
}

// writeMethods start a write builder (CT-Z2) and the operation each one
// records.
var writeMethods = map[string]string{"insert": "insert", "update": "update", "delete": "delete"}

// writeLinks are the builder methods that make a write chain Drizzle's
// even when its target does not resolve.
var writeLinks = map[string]bool{
	"values": true, "set": true, "where": true, "returning": true, "onConflictDoUpdate": true, "onConflictDoNothing": true,
	"from": true, "select": true, "overridingSystemValue": true,
}

// unexecutedEnds are the chain ends that build a query without running
// it (a subquery, the SQL text, a prepared statement, a dynamic builder).
var unexecutedEnds = map[string]bool{"as": true, "toSQL": true, "getSQL": true, "prepare": true, "$dynamic": true}

// isBuilderChain reports a select or write builder chain.
func isBuilderChain(links []chainLink) bool {
	_, _, ok := selectStart(links)
	if ok {
		return true
	}
	_, ok = writeStart(links)
	return ok
}

// selectStart finds the select link and its from link: links before the
// select are only with(...) (CTEs).
func selectStart(links []chainLink) (int, int, bool) {
	for i, k := range links {
		if selectMethods[k.name] {
			for j := i + 1; j < len(links); j++ {
				if links[j].name == "from" && len(links[j].args) >= 1 {
					return i, j, true
				}
			}
			return 0, 0, false
		}
		if k.name != "with" {
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// writeStart finds the insert, update or delete link (only with(...)
// before it).
func writeStart(links []chainLink) (int, bool) {
	for i, k := range links {
		if _, ok := writeMethods[k.name]; ok && len(k.args) == 1 {
			return i, true
		}
		if k.name != "with" {
			return 0, false
		}
	}
	return 0, false
}

// notTableForm reports an argument that is no Drizzle table form at all
// (a string, number, object or array literal): the call is another
// library's (knex's from("users")), not a site.
func notTableForm(e facts.RouteExpr) bool {
	switch e.Kind {
	case "literal", "string", "template_string", "number", "object", "array", "true", "false", "null", "undefined", "arrow_function", "function_expression":
		return true
	}
	return false
}

// drizzleFile is one TypeScript file's Drizzle sites in link order.
type drizzleFile struct {
	l      *drizzleLinker
	file   string
	facts  facts.Facts
	groups []*drizzleGroup
}

// chainKey identifies a call chain by its start and its number of links:
// every call of a chain starts where the chain starts.
type chainKey struct{ offset, links int }

func keyOf(e facts.RouteExpr) chainKey {
	_, links := callChain(e)
	return chainKey{e.Offset, len(links)}
}

// linkDrizzleFile reads a file's calls against the Drizzle groups of its
// package. Executions holding no site are row D's statement_not_traced
// records.
func linkDrizzleFile(l *drizzleLinker, file string, f facts.Facts, a *analysis) {
	if f.Routes == nil || len(f.Routes.Clients) == 0 {
		return
	}
	if l.moduleFiles[file] {
		// A file a schema source owns is not a site (W6): a view's
		// definition reads tables in the DDL, not in code.
		return
	}
	groups := l.packageGroups(file)
	d := drizzleFile{l: l, file: file, facts: f, groups: groups}
	receivers := map[chainKey]bool{}
	args := map[chainKey]bool{}
	for _, c := range f.Routes.Clients {
		e := c.Expression
		if e.Kind != "call_expression" {
			continue
		}
		if e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil && e.Object.Object.Kind == "call_expression" {
			receivers[keyOf(*e.Object.Object)] = true
		}
		for _, x := range e.Args {
			args[keyOf(x)] = true
		}
	}
	var sites []site
	var direct []bool
	var executions []facts.ClientCall
	for _, c := range f.Routes.Clients {
		e := c.Expression
		if e.Kind != "call_expression" {
			continue
		}
		if s, ok := d.claimDrizzleSQL(c); ok {
			sites = append(sites, s)
			direct = append(direct, false)
			continue
		}
		if d.execution(c) {
			executions = append(executions, c)
		}
		if receivers[keyOf(e)] {
			continue
		}
		for _, s := range d.site(c) {
			sites = append(sites, s)
			direct = append(direct, args[keyOf(e)] || s.role == RoleBuilds)
		}
	}
	// Role: a chain that ends without running (.as, .toSQL, .prepare,
	// $dynamic) or that is passed to another call builds, unless it sits
	// inside a site that executes (a subquery of an executed statement).
	for i := range sites {
		if !direct[i] {
			continue
		}
		sites[i].role = RoleBuilds
		for j := range sites {
			if j != i && !direct[j] && sites[j].span != sites[i].span && contains(sites[j].span, sites[i].span) {
				sites[i].role = RoleExecutes
			}
		}
	}
	// W7 (Revision 4): a table const proven to be a group's table is
	// claimed in any package. In a package without a group, a site that
	// claims nothing and an execution are kept only when the file shows
	// Drizzle (an import from drizzle-orm or a claimed site); otherwise
	// the shape may be another library's.
	evidence := len(groups) > 0 || importsDrizzle(f)
	for _, s := range sites {
		evidence = evidence || len(s.reads)+len(s.writes) > 0
	}
	for _, s := range sites {
		// <r>.query.<key>.findMany is Drizzle's shape: in a package
		// without a group it stays an unknown (Revision 4).
		if evidence || len(s.reads)+len(s.writes) > 0 || s.row == "CT-Z3" {
			a.sites = append(a.sites, s)
		}
	}
	if !evidence {
		return
	}
	for _, c := range executions {
		traced := false
		for _, s := range sites {
			traced = traced || contains(c.Span, s.span) && c.Span != s.span
		}
		if traced {
			continue
		}
		l.in.tag("D")
		a.unknowns[c.Owner] = append(a.unknowns[c.Owner], Unknown{Row: "D", Line: c.Span.StartLine, Span: c.Span, Tool: drizzle.Tool, Reason: ReasonStatementNotTraced})
	}
}

// importsDrizzle reports a file that imports a drizzle-orm module.
func importsDrizzle(f facts.Facts) bool {
	for _, imp := range f.Imports {
		if drizzleORMModule(imp.Module) {
			return true
		}
	}
	return false
}

// execution reports a call that runs statements the link pass may not
// see: a transaction over a callback, or execute(...) of an sql value
// that is not an inline template (row D).
func (d drizzleFile) execution(c facts.ClientCall) bool {
	e := c.Expression
	if e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Object == nil || len(e.Args) == 0 {
		return false
	}
	switch e.Object.Name {
	case "transaction":
		k := e.Args[0].Kind
		return k == "arrow_function" || k == "function_expression"
	case "execute":
		r := d.l.resolve(d.file, c.Scope, e.Args[0], 0)
		return r.structural
	}
	return false
}

func (d drizzleFile) newSite(c facts.ClientCall, row, tool string) site {
	return site{file: d.file, span: c.Span, owner: c.Owner, row: row, tool: tool, rows: []string{row},
		assumes: []string{"TA-V3b"}, role: RoleExecutes, verification: rowByID(row).Verification,
		reads: map[string]map[string]bool{}, writes: map[string]map[string]bool{}, tables: map[string]string{}}
}

// site classifies the outermost call of a chain: a select (CT-Z1), a write
// (CT-Z2), a relational query (CT-Z3), or no site.
func (d drizzleFile) site(c facts.ClientCall) []site {
	recv, links := callChain(c.Expression)
	if len(links) == 0 {
		return nil
	}
	if (links[0].name == "findMany" || links[0].name == "findFirst") && recv.Kind == "member_expression" && recv.Object != nil &&
		recv.Object.Kind == "member_expression" && recv.Object.Name == "query" && recv.Object.Object != nil {
		return []site{d.claimDrizzleQuery(c, recv.Name, links)}
	}
	if links[0].name == "$count" && len(links[0].args) >= 1 {
		if notTableForm(links[0].args[0]) {
			return nil
		}
		return []site{d.claimDrizzleSelect(c, links, -1, -1)}
	}
	if sel, from, ok := selectStart(links); ok {
		if notTableForm(links[from].args[0]) {
			return nil
		}
		return []site{d.claimDrizzleSelect(c, links, sel, from)}
	}
	if w, ok := writeStart(links); ok {
		if notTableForm(links[w].args[0]) {
			return nil
		}
		ref := d.l.resolve(d.file, c.Scope, links[w].args[0], 0)
		if ref.table == nil || ref.column != "" {
			drizzleChain := false
			for _, k := range links[w+1:] {
				drizzleChain = drizzleChain || writeLinks[k.name] || selectJoins[k.name]
			}
			if !drizzleChain {
				return nil
			}
		}
		return []site{d.claimDrizzleWrite(c, links, w, ref)}
	}
	return nil
}

// target records a table position of a CT-Z1 or CT-Z2 site: a resolved
// table of the package is claimed, anything else is an unresolved
// detail.
func (d drizzleFile) target(s *site, ref tableRef, at string, write bool, op string, unresolved *reasonSet) bool {
	if ref.form != "" && s.row == "CT-Z1" {
		d.l.in.tag("CT-Z1/" + ref.form)
	}
	if ref.table == nil || ref.column != "" {
		unresolved.add(at + ": " + ref.detail)
		return false
	}
	if write {
		addOp(s.writes, ref.table.identity, op)
	} else {
		addOp(s.reads, ref.table.identity, op)
	}
	s.tables[ref.table.identity] = ref.table.qualified()
	return true
}

// finish turns a builder site's unresolved positions into its unknown
// record (partial when it claimed a table), sets its verification and
// its oracle text, and keeps the builder text for the toSQL oracle.
func (d drizzleFile) finish(s *site, c facts.ClientCall, links []chainLink, unresolved reasonSet) {
	if u := unresolved.unknown(s, ReasonEntityUnresolved, len(s.reads)+len(s.writes) > 0); u != nil {
		s.unknown = u
	}
	end := len(links)
	for end > 0 && (links[end-1].name == "then" || links[end-1].name == "catch" || links[end-1].name == "finally" || links[end-1].name == "execute" || unexecutedEnds[links[end-1].name]) {
		end--
	}
	if end < len(links) && unexecutedEnds[links[len(links)-1].name] {
		s.role = RoleBuilds
	}
	if end > 0 {
		s.builder = links[end-1].call.Text
		s.scope = c.Scope
	}
	for _, id := range sortedKeys(s.tables) {
		g := d.l.byTable[id]
		if s.sets == nil {
			s.sets = []*migrationSet{g.source}
		}
		if g.dialect != "postgresql" {
			// The toSQL oracle compares pg-core builders only.
			s.verification = VerificationMapped
		}
	}
}

// claimDrizzleSelect is row CT-Z1's branch: the from table and every join
// table of a select chain, or the table of <r>.$count(t), are read.
func (d drizzleFile) claimDrizzleSelect(c facts.ClientCall, links []chainLink, sel, from int) site {
	s := d.newSite(c, "CT-Z1", drizzle.Tool)
	var unresolved reasonSet
	if sel < 0 {
		d.l.in.tag("CT-Z1/$count")
		d.target(&s, d.l.resolve(d.file, c.Scope, links[0].args[0], 0), "$count", false, "count", &unresolved)
		d.finish(&s, c, links, unresolved)
		return s
	}
	d.l.in.tag("CT-Z1/" + links[sel].name)
	d.l.in.tag("CT-Z1/.from(t)")
	d.target(&s, d.l.resolve(d.file, c.Scope, links[from].args[0], 0), ".from", false, "select", &unresolved)
	for _, k := range links[from+1:] {
		if !selectJoins[k.name] || len(k.args) == 0 {
			continue
		}
		name := strings.TrimSuffix(k.name, "Lateral")
		d.l.in.tag("CT-Z1/" + name)
		if name != k.name {
			d.l.in.tag("CT-Z1/Lateral")
		}
		d.target(&s, d.l.resolve(d.file, c.Scope, k.args[0], 0), "."+k.name, false, "select", &unresolved)
	}
	d.finish(&s, c, links, unresolved)
	return s
}

// claimDrizzleWrite is row CT-Z2's branch: the insert, update or delete
// target is written (onConflictDoUpdate: upsert); an update's from table
// is read. A join on an update chain is no listed form: its tables are
// site_partial.
func (d drizzleFile) claimDrizzleWrite(c facts.ClientCall, links []chainLink, w int, ref tableRef) site {
	s := d.newSite(c, "CT-Z2", drizzle.Tool)
	method := links[w].name
	op := writeMethods[method]
	d.l.in.tag("CT-Z2/" + method)
	var partial reasonSet
	for _, k := range links[w+1:] {
		switch {
		case k.name == "onConflictDoUpdate" && method == "insert":
			op = "upsert"
			d.l.in.tag("CT-Z2/." + k.name)
		case k.name == "select" && method == "insert":
			d.l.in.tag("CT-Z2/.select(qb)")
		case writeLinks[k.name] && k.name != "from" && k.name != "select" && k.name != "overridingSystemValue":
			d.l.in.tag("CT-Z2/." + k.name)
		}
	}
	var unresolved reasonSet
	d.target(&s, ref, method, true, op, &unresolved)
	for _, k := range links[w+1:] {
		switch {
		case k.name == "from" && method == "update" && len(k.args) > 0:
			d.l.in.tag("CT-Z2/.from(u)")
			d.target(&s, d.l.resolve(d.file, c.Scope, k.args[0], 0), ".from", false, "select", &unresolved)
		case selectJoins[k.name] && len(k.args) > 0:
			r := d.l.resolve(d.file, c.Scope, k.args[0], 0)
			if r.table != nil && r.column == "" {
				partial.add("."+k.name+" on a write chain is no listed form", r.table.identity)
			} else {
				partial.add("." + k.name + " on a write chain is no listed form")
			}
		}
	}
	d.finish(&s, c, links, unresolved)
	if u := partial.unknown(&s, ReasonSitePartial, true); u != nil {
		s.extra = append(s.extra, *u)
	}
	return s
}

// claimDrizzleQuery is row CT-Z3's branch: <r>.query.<key>.findMany(cfg)
// and findFirst read the key's table and every with relation's target.
func (d drizzleFile) claimDrizzleQuery(c facts.ClientCall, key string, links []chainLink) site {
	s := d.newSite(c, "CT-Z3", drizzle.Tool)
	d.l.in.tag("CT-Z3/" + links[0].name)
	var ids []string
	groups := 0
	for _, g := range d.groups {
		found := d.l.keyTables(g, key)
		if len(found) > 0 {
			groups++
		}
		ids = append(ids, found...)
	}
	sort.Strings(ids)
	switch {
	case len(ids) == 0:
		detail := "query key " + key + " names no table const of the schema modules of the package's Drizzle groups"
		if len(d.groups) == 0 {
			detail = "query key " + key + ": the package has no Drizzle group, whose schema binding names the key"
		}
		s.refuse(ReasonEntityUnresolved+": "+detail, false, nil)
		return s
	case len(ids) > 1:
		detail := "query key " + key + " names tables of two Drizzle groups of the package"
		if groups == 1 {
			detail = "query key " + key + " names two table consts of the group's schema modules"
		}
		s.refuse(ReasonAmbiguousGroup+": "+detail, false, ids)
		return s
	}
	t := d.l.byTable[ids[0]].tables[ids[0]]
	addOp(s.reads, t.identity, "find")
	s.tables[t.identity] = t.qualified()
	var partial reasonSet
	if len(links[0].args) > 0 {
		d.queryConfig(&s, t, links[0].args[0], "the config", &partial)
	}
	if u := partial.unknown(&s, ReasonSitePartial, true); u != nil {
		s.extra = append(s.extra, *u)
	}
	return s
}

// queryConfig reads a relational query config: each with entry that is a
// relation of the table's orm_relations reads the relation's target, and
// its own config is read for that target.
func (d drizzleFile) queryConfig(s *site, t *drizzleTable, cfg facts.RouteExpr, at string, partial *reasonSet) {
	if cfg.Kind != "object" {
		partial.add(at + " is not an object literal")
		return
	}
	if cfg.Spread {
		d.l.in.tag("CT-Z3/spread")
		partial.add("an object spread in " + at)
	}
	with, ok := cfg.Properties["with"]
	if !ok || literalFalse(with) {
		return
	}
	if with.Kind != "object" {
		partial.add(at + ".with is not an object literal")
		return
	}
	if with.Spread {
		d.l.in.tag("CT-Z3/spread")
		partial.add("an object spread in " + at + ".with")
	}
	keys := with.Keys
	if len(keys) == 0 {
		for k := range with.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	for _, k := range keys {
		v := with.Properties[k]
		if literalFalse(v) {
			continue
		}
		target, ok := t.relations[k]
		if !ok {
			d.l.in.tag("CT-Z3/orm_relations")
			partial.add(at + ".with." + k + " is not an entry of " + t.decl + "'s orm_relations")
			continue
		}
		rt := d.l.byTable[target]
		if target == "" || rt == nil {
			partial.add(at + ".with." + k + ": the relation's target is not a claimed table")
			continue
		}
		related := rt.tables[target]
		switch v.Kind {
		case "true", "object":
			d.l.in.tag("CT-Z3/with")
			addOp(s.reads, related.identity, "find")
			s.tables[related.identity] = related.qualified()
			if v.Kind == "object" {
				d.queryConfig(s, related, v, at+".with."+k, partial)
			}
		default:
			partial.add(at+".with."+k+" is not true or an object literal", related.identity)
		}
	}
}

// keyTables returns the tables a relational query key names in a group:
// the key is an export name of the group's schema modules (A12: the
// schema option is `import * as schema`).
func (l *drizzleLinker) keyTables(g *drizzleGroup, key string) []string {
	if ids, ok := g.keys[key]; ok {
		return ids
	}
	found := map[string]bool{}
	if g.config != nil && l.in.Modules != nil {
		for _, m := range g.config.Modules {
			if !m.All && !hasString(m.Names, key) {
				continue
			}
			r := l.export(l.in.Modules.Export(m.File, key), key, 0)
			if r.table != nil && r.column == "" && r.alias == "" && g.tables[r.table.identity] == r.table {
				found[r.table.identity] = true
			}
		}
	}
	ids := sortedSet(found)
	if len(ids) == 0 {
		ids = nil
	}
	g.keys[key] = ids
	return ids
}

// sqliteMethods run an sql template on a SQLite database (CT-Z4).
var sqliteMethods = map[string]bool{"run": true, "all": true, "get": true, "values": true}

// sqlTag reports whether an expression is drizzle-orm's sql tag.
func (d drizzleFile) sqlTag(scope int, e facts.RouteExpr) bool {
	if e.Kind != "identifier" {
		return false
	}
	b, _, ok := d.l.binding(d.file, scope, e.Name)
	return ok && b.Module == "drizzle-orm" && b.Export == "sql"
}

// claimDrizzleSQL is row CT-Z4's branch: <r>.execute(sql`...`) (and the
// SQLite run, all, get, values) read through the SQL reader against the
// package's Drizzle groups, a table const substitution rendered as the
// table's name, any other value substitution a bound parameter.
func (d drizzleFile) claimDrizzleSQL(c facts.ClientCall) (site, bool) {
	e := c.Expression
	if e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Object == nil || len(e.Args) != 1 {
		return site{}, false
	}
	method := e.Object.Name
	sqlite := false
	for _, g := range d.groups {
		sqlite = sqlite || g.dialect == "sqlite"
	}
	if method != "execute" && !(sqlite && sqliteMethods[method]) {
		return site{}, false
	}
	arg := e.Args[0]
	if arg.Kind != "call_expression" || arg.Object == nil {
		return site{}, false
	}
	tagged := d.sqlTag(c.Scope, *arg.Object) && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(arg.Text, arg.Object.Text)), "`")
	fragment := arg.Object.Kind == "member_expression" && arg.Object.Object != nil && d.sqlTag(c.Scope, *arg.Object.Object)
	if !tagged && !fragment {
		return site{}, false
	}
	s := d.newSite(c, "CT-Z4", "sql")
	s.assumes = []string{"TA-V1", "TA-V3b"}
	s.reads, s.writes = nil, nil
	if method == "execute" {
		d.l.in.tag("CT-Z4/.execute(")
	}
	if fragment {
		form := "sql." + arg.Object.Name
		if form != "sql.raw" && form != "sql.identifier" {
			form = "structural substitution"
		}
		d.l.in.tag("CT-Z4/" + form)
		s.refuse(ReasonSQLDynamic+": the argument is "+strings.TrimSpace(firstLine(arg.Text)), false, nil)
		return s, true
	}
	sql, detail, used := d.templateSQL(c.Scope, arg)
	if detail != "" {
		s.refuse(ReasonSQLDynamic+": "+detail, false, nil)
		return s, true
	}
	// The schema source is the package's groups; in a package without a
	// group, the groups of the table consts the template names (W7,
	// Revision 4).
	groups := d.groups
	if len(groups) == 0 {
		groups = used
	}
	var sets []*migrationSet
	seen := map[*drizzleGroup]bool{}
	for _, g := range groups {
		if g.dialect == "postgresql" && !seen[g] {
			seen[g] = true
			sets = append(sets, g.source)
		}
	}
	if len(sets) == 0 {
		detail := "the package's Drizzle groups are not postgresql (TA-V4)"
		if len(groups) == 0 {
			detail = "the package has no Drizzle group and the template names no table const"
		}
		s.refuse(ReasonNoSchemaSource+": "+detail, false, nil)
		return s, true
	}
	s.sql = sql
	s.sets = sets
	readSQLSite(d.l.in, &s, sql, sets)
	return s, true
}

// tablePositions are the keywords after which a reference is a table
// (CT-Q1): a substitution there must be a table const.
var tablePositions = map[string]bool{"from": true, "join": true, "into": true, "update": true, "using": true, "only": true, "table": true}

// templateSQL renders an sql template as drizzle-orm does for the reader:
// text cooked, a table const as its name, a column as
// "table"."column", any other value as a bound parameter $n. A
// structural substitution (an sql fragment, an operator, a query builder,
// an aliased table, a view) or a parameter in a table position is
// sql_dynamic (the detail says which).
func (d drizzleFile) templateSQL(scope int, arg facts.RouteExpr) (string, string, []*drizzleGroup) {
	var used []*drizzleGroup
	var b strings.Builder
	n := 0
	params := map[int]string{}
	for _, a := range arg.Args {
		switch a.Kind {
		case "string_fragment":
			b.WriteString(a.Text)
		case "escape_sequence":
			v, ok := cookEscape(a.Text)
			if !ok {
				return "", "an escape sequence " + a.Text + " in the template", nil
			}
			b.WriteString(v)
		case "template_substitution":
			if a.Object == nil {
				return "", "an empty substitution", nil
			}
			r := d.l.resolve(d.file, scope, *a.Object, 0)
			switch {
			case r.table != nil && r.column == "" && r.alias == "":
				d.l.in.tag("CT-Z4/table position")
				b.WriteString(r.table.rendered())
				used = append(used, d.l.byTable[r.table.identity])
			case r.table != nil && r.column != "":
				col, ok := r.table.columns[r.column]
				if !ok {
					return "", "a column " + a.Object.Text + " the records do not hold", nil
				}
				owner := r.table.rendered()
				if r.alias != "" {
					owner = strconv.Quote(r.alias)
				}
				b.WriteString(owner + "." + strconv.Quote(col))
			case r.table != nil && r.alias != "":
				d.l.in.tag("CT-Z4/structural substitution")
				return "", "an aliased table " + a.Object.Text + " in the template", nil
			case r.structural:
				form := "structural substitution"
				if o := a.Object; o.Kind == "call_expression" && o.Object != nil && o.Object.Kind == "member_expression" && o.Object.Object != nil &&
					d.sqlTag(scope, *o.Object.Object) && (o.Object.Name == "raw" || o.Object.Name == "identifier") {
					form = "sql." + o.Object.Name
				}
				d.l.in.tag("CT-Z4/" + form)
				return "", "a structural substitution " + strings.TrimSpace(firstLine(a.Object.Text)) + " (" + r.detail + ")", nil
			default:
				d.l.in.tag("CT-Z4/value position")
				n++
				params[b.Len()] = a.Object.Text
				b.WriteString("$" + strconv.Itoa(n))
			}
		case "comment":
		default:
			return "", "a template part " + a.Kind, nil
		}
	}
	sql := b.String()
	if len(params) > 0 {
		if tokens, err := lex(sql); err == nil {
			for i, t := range tokens {
				text, ok := params[t.start]
				if !ok || t.kind != tokParam || i == 0 {
					continue
				}
				if p := tokens[i-1]; p.kind == tokIdent && tablePositions[p.value] {
					d.l.in.tag("CT-Z4/table position")
					return "", "a substitution in a table position that is no table const: " + strings.TrimSpace(firstLine(text)), nil
				}
			}
		}
	}
	return sql, "", used
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// drizzleRun is a config's drizzle-kit run in the format of
// scripts/oracle/drizzle_catalog.sh (the oracle's schema source).
func drizzleRun(c *drizzle.Config) string {
	casing := c.Casing
	if casing == "" {
		casing = "-"
	}
	kit, orm := "", ""
	if c.Package.Pins != nil {
		kit, _ = c.Package.Pins["drizzle-kit"].(string)
		orm, _ = c.Package.Pins["drizzle-orm"].(string)
	}
	return strings.Join([]string{".", c.Package.Dir, c.Path, strings.Join(c.Schema, ","), c.Dialect, casing, kit, orm}, "|")
}

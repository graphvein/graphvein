package drizzle

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Table forms of row DR-T1: the builder export of each dialect module.
var tableForms = map[string]string{"pgTable": "postgresql", "sqliteTable": "sqlite", "mysqlTable": "mysql"}

// Table builders row D names (table creators, dialects drizzle-kit 0.31
// is not read for).
var unreadTableBuilders = map[string]bool{
	"pgTableCreator": true, "sqliteTableCreator": true, "mysqlTableCreator": true,
	"singlestoreTable": true, "gelTable": true,
}

// group is the claim pass of one proven config: one model group.
type group struct {
	in Input
	c  *Config
	// dialect is the dialect whose rules apply (turso: sqlite).
	dialect string
	steps   *models.Steps
	// visible holds the declarations among the exported values of the
	// group's schema modules (drizzle-kit's Object.values(exports)).
	visible map[*typescript.ModelDeclaration]bool
	order   []visibleDecl
	// skip are declarations another group also reads (not claimed).
	skip   map[*typescript.ModelDeclaration]bool
	tables []*table
}

type visibleDecl struct {
	file string
	decl *typescript.ModelDeclaration
}

func rulesDialect(d string) string {
	if d == "turso" {
		return "sqlite"
	}
	return d
}

func (g *group) tag(row string) { g.in.tag(row) }

// unread reports a whole declaration no row reads (row D) that is not a
// table of the group (relations() forms, a table of another dialect).
func (g *group) unread(file string, line int, format string, a ...any) {
	g.tag("D")
	g.steps.Diag(file, "info", codeUnread, fmt.Sprintf("%s:%d: ", file, line)+fmt.Sprintf(format, a...))
}

// tableUnread reports a declaration that may be a table of the group but
// that no row reads (row D). The message ends with the group's list
// marker, so the server's table_list for the group is may_be_incomplete
// even when the file holds no claimed table of the group (T018g2 review:
// never silent).
func (g *group) tableUnread(file string, line int, format string, a ...any) {
	g.tag("D")
	g.steps.Diag(file, "info", codeUnread, fmt.Sprintf("%s:%d: ", file, line)+fmt.Sprintf(format, a...)+ListMarker(g.c.SetKey()))
}

// failure reports a provable failure (row DR-P1): drizzle-kit or the
// database rejects the tables involved, so none of them is claimed.
func (g *group) failure(file string, line int, format string, a ...any) {
	g.tag("DR-P1")
	g.steps.Diag(file, "warning", codeImportFailure, fmt.Sprintf("%s:%d: DR-P1: ", file, line)+fmt.Sprintf(format, a...)+"; the tables involved are not claimed"+ListMarker(g.c.SetKey()))
}

// ListMarker is the suffix of every claim-pass diagnostic about a
// declaration that may be a table of the group set but is not claimed
// there. The server reads it (internal/server, modelListMarker) to put the
// diagnostic in that group's table_list reasons, whatever file it is on:
// a module without a payload or a table creator's file need not hold a
// claimed table of the group.
func ListMarker(set string) string {
	return "; the table list of model group " + set + " may be incomplete"
}

// collectVisible lists the declarations among the exported values of the
// group's modules, in module and source order, each once (a value exported
// under two names is one table, bin.cjs:16797).
func (g *group) collectVisible() {
	g.visible = map[*typescript.ModelDeclaration]bool{}
	add := func(file string, d *typescript.ModelDeclaration) {
		if !g.visible[d] {
			g.visible[d] = true
			g.order = append(g.order, visibleDecl{file, d})
		}
	}
	for _, m := range g.c.Modules {
		md := g.in.Models[m.File]
		if md == nil {
			g.noPayload(m)
			continue
		}
		vis := func(name string) bool { return m.All || containsString(m.Names, name) }
		for i := range md.Declarations {
			if d := &md.Declarations[i]; d.Exported && vis(d.Name) {
				add(m.File, d)
			}
		}
		for _, ec := range g.in.TypeScript[m.File].ExportClauses {
			if ec.Module != "" || ec.Star || ec.TypeOnly || ec.Namespace || !vis(ec.Alias) {
				continue
			}
			for i := range md.Declarations {
				if md.Declarations[i].Name == ec.Name {
					add(m.File, &md.Declarations[i])
				}
			}
		}
		if md.Default != nil && vis("default") {
			if _, _, ok := g.tableBuilder(m.File, md.Default.Value); ok {
				g.tableUnread(m.File, md.Default.Span.StartLine, "a default-exported table builder call is not read (row D)")
			}
		}
	}
}

// noPayload reports a module of the group without a model payload whose
// visible exports include a value declaration (row D, T018g2 review):
// drizzle-kit sees those values, and one may be a table built with
// builders the module imports through a local re-export (the payload gate
// G1 needs a direct drizzle-orm/<dialect>-core import, ADR 0059), so the
// group's table list may be incomplete. Functions and classes are never
// tables, and re-exports are followed by discovery (reach), so neither
// counts. The marker counts the values in the "<file>: <n> ..." form the
// server reads.
func (g *group) noPayload(m Module) {
	f, ok := g.in.TypeScript[m.File]
	if !ok {
		return
	}
	var names []string
	for alias, identity := range f.Exports {
		kind, _, _, _ := contract.ParseIdentity(identity)
		if kind == "variable" && (m.All || containsString(m.Names, alias)) {
			names = append(names, alias)
		}
	}
	if len(names) == 0 {
		return
	}
	sort.Strings(names)
	g.tag("D")
	g.steps.Diag(m.File, "info", codeUnread, fmt.Sprintf("%s: %d exported value(s) not read (%s): the module has no Drizzle model payload (it imports no drizzle-orm/<dialect>-core module; builders imported through a local re-export are not followed, row D)%s", m.File, len(names), strings.Join(names, ", "), ListMarker(g.c.SetKey())))
}

// tableBuilder classifies a declaration's value: a table builder call
// (form, schema declaration for `<s>.table`), ok false for anything else.
// why is set for a table builder no row claims.
type builderInfo struct {
	form    string
	dialect string
	schema  *visibleDecl
	why     string
	noTable string
}

func (g *group) tableBuilder(file string, e typescript.ModelExpr) (builderInfo, []link, bool) {
	base, links, ok := chain(e)
	if !ok {
		return builderInfo{}, nil, false
	}
	callee := base.Callee
	if callee.Kind == typescript.ModelMember && callee.Name == "table" && callee.Object.Kind == typescript.ModelIdentifier {
		r := g.in.lookup(file, callee.Object.Name)
		if r.Decl == nil {
			return builderInfo{}, nil, false
		}
		sb, _, ok := chain(r.Decl.Value)
		if !ok {
			return builderInfo{}, nil, false
		}
		sr := g.in.calleeRef(r.File, sb.Callee)
		switch {
		case sr.Module == "drizzle-orm/pg-core" && sr.Export == "pgSchema":
			return builderInfo{form: "<s>.table", dialect: "postgresql", schema: &visibleDecl{r.File, r.Decl}}, links, true
		case sr.Module == "drizzle-orm/mysql-core" && sr.Export == "mysqlSchema":
			return builderInfo{form: "<s>.table", dialect: "mysql", noTable: "drizzle-kit adds a MySQL table only when it has no schema (bin.cjs:16271), so mysqlSchema(...).table creates no table"}, links, true
		}
		return builderInfo{}, nil, false
	}
	r := g.in.calleeRef(file, callee)
	d, isCore := coreDialect[r.Module]
	switch {
	case isCore && tableForms[r.Export] != "":
		return builderInfo{form: r.Export, dialect: tableForms[r.Export]}, links, true
	case strings.HasPrefix(r.Module, "drizzle-orm/") && unreadTableBuilders[r.Export]:
		return builderInfo{form: r.Export, dialect: d, why: r.Export + " is not read (row D)"}, links, true
	}
	return builderInfo{}, nil, false
}

// creator is a module-level const holding a *TableCreator call.
type creator struct {
	file   string
	decl   *typescript.ModelDeclaration
	export string
}

// creatorTables finds the visible declarations built by a table creator
// (`const createTable = pgTableCreator(...)`; `export const users =
// createTable("users", {...})`, row D): built maps each creator to the
// table names its calls pass (`<non-literal>` for any other argument), so
// its marker can name them (T018g2 review); creators lists the
// creators in the order their first table is seen.
func (g *group) creatorTables() (map[*typescript.ModelDeclaration][]string, []creator) {
	built := map[*typescript.ModelDeclaration][]string{}
	var creators []creator
	for _, v := range g.order {
		base, _, ok := chain(v.decl.Value)
		if !ok || base.Callee == nil || base.Callee.Kind != typescript.ModelIdentifier {
			continue
		}
		r := g.in.lookup(v.file, base.Callee.Name)
		if r.Decl == nil || r.Decl == v.decl {
			continue
		}
		cb, _, ok := chain(r.Decl.Value)
		if !ok {
			continue
		}
		cr := g.in.calleeRef(r.File, cb.Callee)
		if !strings.HasPrefix(cr.Module, "drizzle-orm/") || !strings.HasSuffix(cr.Export, "TableCreator") || !unreadTableBuilders[cr.Export] {
			continue
		}
		name := "<non-literal>"
		if len(base.Args) > 0 {
			if s, ok := literalString(base.Args[0]); ok {
				name = strconv.Quote(s)
			}
		}
		if _, seen := built[r.Decl]; !seen {
			creators = append(creators, creator{file: r.File, decl: r.Decl, export: cr.Export})
		}
		built[r.Decl] = append(built[r.Decl], name)
	}
	return built, creators
}

// builtText names the tables a creator builds, in a marker.
func builtText(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("; it builds the table(s) %s (the database names come from the creator's function, not read)", strings.Join(names, ", "))
}

// schemaCertain says whether drizzle-kit creates the tables of schema s
// (public for a table without pgSchema) under both DR-V2 commands:
// `generate` reads every schema (serializePg without a filter,
// bin.cjs:19850), while `push` keeps only the schemas of the config's
// schemaFilter, ["public"] when it is absent (bin.cjs:10770, :82705,
// :16944). A schema outside that filter exists after generate + migrate
// but not after push, so its tables are not claimed.
func (g *group) schemaCertain(s string) (bool, string) {
	switch {
	case !g.c.SchemaFilterKnown:
		return false, "the config's schemaFilter is not literal, so whether drizzle-kit push creates the table is not known"
	case g.c.SchemaFilter == nil && s == "public":
		return true, ""
	case g.c.SchemaFilter == nil:
		return false, fmt.Sprintf("schema %q: drizzle-kit push keeps only schemaFilter's schemas (default [\"public\"], bin.cjs:10770, :16944) while generate keeps every schema (bin.cjs:19850), so whether the table exists depends on the command", s)
	case containsString(g.c.SchemaFilter, s):
		return true, ""
	}
	return false, fmt.Sprintf("schema %q is not in the config's schemaFilter %v: drizzle-kit push does not create the table (bin.cjs:16944) while generate does (bin.cjs:19850)", s, g.c.SchemaFilter)
}

// read builds the group's tables.
func (g *group) read() {
	g.collectVisible()
	built, creators := g.creatorTables()
	for _, v := range g.order {
		v := v
		info, links, ok := g.tableBuilder(v.file, v.decl.Value)
		if !ok {
			continue
		}
		if g.skip[v.decl] {
			continue
		}
		line := v.decl.Span.StartLine
		switch {
		case info.noTable != "":
			g.tag("DR-T1/" + info.form)
			g.steps.Diag(v.file, "info", codeUnread, fmt.Sprintf("%s:%d: %s: %s (row DR-T1)", v.file, line, v.decl.Name, info.noTable))
			continue
		case info.why != "":
			g.tableUnread(v.file, line, "%s: %s%s", v.decl.Name, info.why, builtText(built[v.decl]))
			delete(built, v.decl)
			continue
		case info.dialect != g.dialect:
			g.unread(v.file, line, "%s: a %s table builder in a config of dialect %s; drizzle-kit collects only that dialect's tables (DR-V4)", v.decl.Name, info.dialect, g.c.Dialect)
			continue
		case len(links) > 0:
			g.tableUnread(v.file, line, "%s: table builder call chain (.%s()) is not read (row D)", v.decl.Name, links[0].Name)
			continue
		case v.decl.Keyword != "const":
			g.tableUnread(v.file, line, "%s: a table held by %s is not read (row DR-T1 reads const declarations)", v.decl.Name, v.decl.Keyword)
			continue
		}
		var t *table
		g.steps.Guard(v.file, "table", "Drizzle table "+v.file+"#"+v.decl.Name, func() { t = g.readTable(v, info) })
		if t != nil {
			g.tables = append(g.tables, t)
		}
	}
	// A creator that is not among the visible values still builds the
	// visible tables that call it.
	for _, c := range creators {
		if names, ok := built[c.decl]; ok {
			g.tableUnread(c.file, c.decl.Span.StartLine, "%s: %s is not read (row D)%s", c.decl.Name, c.export, builtText(names))
		}
	}
	g.groupFailures()
	g.foreignKeys()
	g.relations()
}

// groupFailures applies the DR-P1 rules that span tables: two tables with
// one schema-qualified name, and on sqlite two unique indexes with one
// name (index names are per database there).
func (g *group) groupFailures() {
	failed := map[*table]bool{}
	byName := map[string][]*table{}
	for _, t := range g.tables {
		key := t.rec.Name
		if t.rec.Schema != nil {
			key = *t.rec.Schema + "." + key
		}
		byName[key] = append(byName[key], t)
	}
	for _, key := range sortedKeys(byName) {
		if list := byName[key]; len(list) > 1 {
			var names []string
			for _, t := range list {
				names = append(names, t.rec.Identity)
				failed[t] = true
			}
			g.failure(list[0].rec.File, list[0].rec.ModelLine, "%d exported tables are named %s (%s)", len(list), key, strings.Join(names, ", "))
		}
	}
	if g.dialect == "sqlite" {
		owners := map[string][]*table{}
		for _, t := range g.tables {
			for _, name := range t.sqliteIndexNames {
				owners[name] = append(owners[name], t)
			}
		}
		for _, name := range sortedKeys(owners) {
			list := owners[name]
			if len(list) < 2 {
				continue
			}
			var ids []string
			for _, t := range list {
				ids = append(ids, t.rec.Identity)
				failed[t] = true
			}
			g.failure(list[0].rec.File, list[0].rec.ModelLine, "unique index name %s is used twice in the database (%s)", name, strings.Join(ids, ", "))
		}
	}
	if len(failed) == 0 {
		return
	}
	var kept []*table
	for _, t := range g.tables {
		if !failed[t] {
			kept = append(kept, t)
		}
	}
	g.tables = kept
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// table is one table being read.
type table struct {
	g       *group
	file    string
	decl    *typescript.ModelDeclaration
	rec     *models.Table
	columns []*column
	// extra is the line of the table's third argument (extra config, row
	// DR-X1), 0 when there is none; open is set when an element of it (or
	// the whole) is not read, so the column absences it could change stay
	// unknown.
	extra int
	open  bool
	// casingDerived: a column name comes from the config's casing (DR-V3).
	casingDerived bool
	// pkColumns counts the primary key's columns: the columns with
	// .primaryKey(), or a primaryKey() element's; multiPK is set when
	// several columns call .primaryKey() (not read).
	pkColumns int
	multiPK   bool
	// The extra config's elements (row DR-X1): primary key, unique
	// constraints, foreign keys; index names (sqlite: database-wide).
	pk         *pkSpec
	uniques    []uniqueSpec
	fks        []*fkSpec
	indexNames []string
	// missing are property keys the extra config names that are not
	// columns of the table (DR-P1).
	missing []string
	// uniqueIndexes are the columns of the non-partial unique indexes
	// without expressions: a foreign key may reference them (DR-P1).
	uniqueIndexes [][]*column
	// sqliteIndexNames are the unique index names of the columns (sqlite).
	sqliteIndexNames []string
	failed           bool
}

func (t *table) reason(row, fact string, line int, format string, a ...any) {
	t.rec.Reasons = append(t.rec.Reasons, models.Reason{Row: row, Fact: models.Fact(fact), File: t.file, Line: line, Reason: fmt.Sprintf(format, a...)})
}

// readTable reads one table builder call (row DR-T1); nil when the table
// is not claimed.
func (g *group) readTable(v visibleDecl, info builderInfo) *table {
	base, _, _ := chain(v.decl.Value)
	line := v.decl.Span.StartLine
	args := base.Args
	if len(args) < 2 || len(args) > 3 {
		g.tableUnread(v.file, line, "%s: %s with %d arguments is not read (row D)", v.decl.Name, info.form, len(args))
		return nil
	}
	name, ok := literalString(args[0])
	if !ok || name == "" {
		g.tableUnread(v.file, line, "%s: the table name is not a string literal (row DR-T1: D)", v.decl.Name)
		return nil
	}
	if args[1].Kind != typescript.ModelObject {
		g.tableUnread(v.file, line, "%s: the columns are not an object literal (row DR-T1: D)", v.decl.Name)
		return nil
	}
	var schema *string
	switch {
	case info.schema != nil:
		s, why := g.pgSchemaName(*info.schema)
		if why != "" {
			if strings.HasPrefix(why, "DR-P1") {
				g.failure(v.file, line, "%s: %s", v.decl.Name, strings.TrimPrefix(why, "DR-P1 "))
			} else {
				g.tableUnread(v.file, line, "%s: %s", v.decl.Name, why)
			}
			return nil
		}
		schema = &s
	case g.dialect == "postgresql":
		public := "public"
		schema = &public
	}
	if schema != nil {
		if ok, why := g.schemaCertain(*schema); !ok {
			g.steps.Diag(v.file, "info", codeNotClaimed, fmt.Sprintf("%s:%d: %s: %s; the table is not claimed (row DR-T1)", v.file, line, v.decl.Name, why)+ListMarker(g.c.SetKey()))
			g.tag("DR-T1")
			return nil
		}
	}
	g.tag("DR-T1/" + info.form)
	t := &table{g: g, file: v.file, decl: v.decl}
	t.rec = &models.Table{
		Identity: "table:" + v.file + "#" + v.decl.Name, File: v.file, Name: name, Model: v.decl.Name, ModelLine: line,
		Span:   contract.Span{StartLine: v.decl.Span.StartLine, EndLine: v.decl.Span.EndLine},
		Schema: schema, SchemaKnown: true, ColumnsComplete: true, ConstraintsComplete: true,
		Attributes: map[string]any{"builder": info.form},
	}
	for _, e := range g.columnEntries(t, v.file, args[1], 0) {
		t.readColumn(e)
	}
	if len(args) == 3 {
		t.extra = args[2].Span.StartLine
		t.extraConfig(args[2])
	}
	t.finish()
	if t.failed {
		return nil
	}
	return t
}

// pgSchemaName reads the schema of `<s>.table` / `<s>.enum`: s must be an
// exported pgSchema("name") of the group (else DR-P1: drizzle-kit creates
// only exported schemas) with a literal name other than "public"
// (pgSchema("public") throws, drizzle-orm pg-core/schema.js).
func (g *group) pgSchemaName(s visibleDecl) (string, string) {
	base, links, _ := chain(s.decl.Value)
	name, ok := "", false
	if len(base.Args) == 1 && len(links) == 0 {
		name, ok = literalString(base.Args[0])
	}
	switch {
	case !ok:
		return "", "pgSchema " + s.decl.Name + " has no string literal name (row D)"
	case name == "public":
		return "", "pgSchema(\"public\") throws in drizzle-orm (row D)"
	case !g.visible[s.decl]:
		return "", fmt.Sprintf("DR-P1 pgSchema %s (%s) is not exported from the schema modules, so drizzle-kit does not create schema %q", s.decl.Name, s.file, name)
	}
	return name, ""
}

// entry is one column property of a table's columns object: its key, its
// value and the file and line it is written at (a spread object may be in
// another file); shared marks a builder written in a spread object, which
// every table spreading it shares.
type entry struct {
	key    string
	value  typescript.ModelExpr
	file   string
	line   int
	shared bool
}

// columnEntries lists the columns object's properties as JavaScript
// builds the object: in order, a later key replacing an earlier one's
// value in the earlier position; a spread of a module-level object
// literal (same file or imported) contributes its properties (row DR-C1).
// Anything else makes the column set partial.
func (g *group) columnEntries(t *table, file string, obj typescript.ModelExpr, depth int) []entry {
	var out []entry
	index := map[string]int{}
	put := func(e entry) {
		if i, ok := index[e.key]; ok {
			out[i] = e
			return
		}
		index[e.key] = len(out)
		out = append(out, e)
	}
	for _, en := range obj.Entries {
		switch en.Kind {
		case typescript.ModelProperty:
			put(entry{key: en.Key, value: en.Value, file: file, line: en.Span.StartLine, shared: depth > 0})
		case typescript.ModelSpreadIn:
			var r ref
			if en.Value.Kind == typescript.ModelIdentifier {
				r = g.in.lookup(file, en.Value.Name)
			}
			if r.Decl == nil || r.Decl.Value.Kind != typescript.ModelObject || depth > 8 {
				t.rec.ColumnsComplete = false
				t.rec.ConstraintsComplete = false
				t.reason("DR-C1", "column_set", en.Span.StartLine, "a spread of something other than a module-level object literal of columns is not read")
				continue
			}
			g.tag("DR-C1")
			for _, e := range g.columnEntries(t, r.File, r.Decl.Value, depth+1) {
				e.shared = true
				put(e)
			}
		default:
			g.tag("D")
			t.rec.ColumnsComplete = false
			t.rec.ConstraintsComplete = false
			t.reason("D", "column_set", en.Span.StartLine, "a %s entry of the columns object is not read", en.Kind)
		}
	}
	return out
}

// finish applies the facts that depend on the whole table: DR-P1 failures
// within it, the table's extra config (row DR-X1), the column absences
// and the assumptions. Foreign keys are resolved by the group
// (foreignKeys) once every table is read.
func (t *table) finish() {
	g := t.g
	byName := map[string]*column{}
	for _, c := range t.columns {
		if !c.rec.Unknown && c.primaryKey {
			t.pkColumns++
		}
	}
	t.multiPK = t.pkColumns > 1
	if len(t.missing) > 0 {
		g.failure(t.file, t.extra, "%s: the extra config names %s, not a column of the table (drizzle-orm reads a property of undefined)", t.decl.Name, strings.Join(t.missing, ", "))
		t.failed = true
		return
	}
	for _, c := range t.columns {
		if c.rec.Unknown {
			continue
		}
		if prev := byName[c.rec.Name]; prev != nil {
			g.failure(t.file, t.rec.ModelLine, "%s: columns %s and %s are both named %q", t.decl.Name, prev.key, c.key, c.rec.Name)
			t.failed = true
			return
		}
		byName[c.rec.Name] = c
	}
	names := map[string]string{}
	for _, c := range t.columns {
		if c.rec.Unknown || !c.unique {
			continue
		}
		name, why := c.constraintName()
		if why != "" {
			continue
		}
		if g.dialect == "postgresql" {
			name = truncateIdentifier(name)
		}
		if prev, ok := names[name]; ok {
			g.failure(t.file, t.rec.ModelLine, "%s: columns %s and %s both have unique constraint %s (drizzle-kit exits, bin.cjs:16997)", t.decl.Name, prev, c.key, name)
			t.failed = true
			return
		}
		names[name] = c.key
		if g.dialect == "sqlite" {
			t.sqliteIndexNames = append(t.sqliteIndexNames, name)
		}
	}
	for _, u := range t.uniques {
		name, ok := t.uniqueName(u)
		if !ok {
			continue
		}
		if prev, ok := names[name]; ok {
			g.failure(t.file, u.line, "%s: %s and a unique() element both have unique constraint %s (drizzle-kit exits, bin.cjs:17083-17101)", t.decl.Name, prev, name)
			t.failed = true
			return
		}
		names[name] = "unique()"
		if g.dialect == "sqlite" {
			t.sqliteIndexNames = append(t.sqliteIndexNames, name)
		}
	}
	if g.dialect == "sqlite" {
		t.sqliteIndexNames = append(t.sqliteIndexNames, t.indexNames...)
	}
	for _, c := range t.columns {
		if c.failure != "" {
			g.failure(c.file, c.line, "%s.%s: %s", t.decl.Name, c.key, c.failure)
			t.failed = true
			return
		}
	}
	t.applyExtra()
	if t.multiPK {
		t.rec.ConstraintsComplete = false
		t.rec.PrimaryKeyOpen = true
		t.reason("D", "constraint_set", t.rec.ModelLine, "%d columns call .primaryKey(); a composite key written that way is not read", t.pkColumns)
	}
	for _, c := range t.columns {
		c.finish()
	}
	assumptions := []string{"DR-V1", "DR-V2"}
	if t.casingDerived {
		assumptions = append(assumptions, "DR-V3")
	}
	assumptions = append(assumptions, "DR-V4", "DR-V5")
	if g.dialect == "mysql" {
		assumptions = append(assumptions, "DR-V6")
	}
	t.rec.Attributes["assumptions"] = assumptions
}

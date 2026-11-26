package tableaccess

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/prisma"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds rows CT-R1 (delegate calls), CT-R2 (relation fields in a
// delegate call's argument object) and CT-R3 (raw SQL) over the cached
// TypeScript calls (facts.ClientCall, docs/lang/typescript-models.md "Call
// cache fidelity fields") and the Prisma group's table records (T018g1,
// ADR 0057). Every Prisma site is inferred: the client receiver is not
// proven (TA-V3a, Revision 1 O1).

// prismaModel is one model of a Prisma group: its table identity and its
// relation fields by name.
type prismaModel struct {
	identity  string
	relations map[string]prisma.RelationField
}

// prismaGroup is one claimed Prisma schema as the link pass reads it.
type prismaGroup struct {
	set      string
	packages []string
	dialect  string
	// models are the client's models by name (an @@ignore'd model has no
	// delegate and is left out); delegates maps a delegate name (the model
	// name with its first letter lower-cased) to its model.
	models    map[string]prismaModel
	delegates map[string]string
	// joins maps "Model.field" of an implicit many-to-many side to its join
	// table's identity.
	joins map[string]string
	// tables are every table identity of the group, sorted (the candidates
	// of a dynamic delegate).
	tables []string
	// source is the group as a schema source for CT-R3 (CT-Q2).
	source *migrationSet
}

// prismaGroups reads the Prisma groups from the table records and the
// relation fields the Prisma front end parses (prisma.Relations).
func prismaGroups(nodes []contract.Node, sources map[string][]byte) []*prismaGroup {
	bySet := map[string]*prismaGroup{}
	var sets []string
	for _, n := range nodes {
		if n.Kind != "table" || n.Attributes["tool"] != prisma.Tool {
			continue
		}
		set, _ := n.Attributes["set"].(string)
		g := bySet[set]
		if g == nil {
			g = &prismaGroup{set: set, models: map[string]prismaModel{}, delegates: map[string]string{}, joins: map[string]string{}}
			g.source = &migrationSet{name: set, dialect: "postgres"}
			bySet[set] = g
			sets = append(sets, set)
		}
		g.dialect, _ = n.Attributes["dialect"].(string)
		if pins, ok := n.Attributes["pins"].(map[string]any); ok {
			g.source.prismaVersion, _ = pins["prisma"].(string)
		}
		g.source.prismaSchema = strings.TrimPrefix(set, "models/prisma/")
		g.tables = append(g.tables, n.Identity)
		t := table{identity: n.Identity}
		t.name, _ = n.Attributes["name"].(string)
		t.schema, _ = n.Attributes["schema"].(string)
		known, _ := n.Attributes["schema_known"].(bool)
		t.pathUnproven = !known
		g.source.tables = append(g.source.tables, t)
		if join, ok := n.Attributes["join_table"].(map[string]any); ok {
			ms := stringList(join["models"])
			fs := stringList(join["fields"])
			for i := 0; i < len(ms) && i < len(fs); i++ {
				g.joins[ms[i]+"."+fs[i]] = n.Identity
			}
			continue
		}
		model, _ := n.Attributes["model"].(string)
		if model == "" || n.Attributes["ignored"] == true {
			continue
		}
		g.models[model] = prismaModel{identity: n.Identity, relations: map[string]prisma.RelationField{}}
		g.delegates[lowerFirst(model)] = model
	}
	if len(sets) == 0 {
		return nil
	}
	for _, rg := range prisma.Relations(sources) {
		g := bySet[rg.Set]
		if g == nil {
			continue
		}
		g.packages = rg.Packages
		for model, fields := range rg.Models {
			m, ok := g.models[model]
			if !ok {
				continue
			}
			for _, f := range fields {
				m.relations[f.Name] = f
			}
		}
	}
	sort.Strings(sets)
	var out []*prismaGroup
	for _, set := range sets {
		g := bySet[set]
		sort.Strings(g.tables)
		sort.Slice(g.source.tables, func(i, j int) bool { return g.source.tables[i].identity < g.source.tables[j].identity })
		out = append(out, g)
	}
	return out
}

func stringList(v any) []string {
	var out []string
	switch list := v.(type) {
	case []string:
		out = append(out, list...)
	case []any:
		for _, x := range list {
			s, _ := x.(string)
			out = append(out, s)
		}
	}
	return out
}

// lowerFirst is Prisma's delegate name: the model name with its first
// letter lower-cased.
func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return strings.ToLower(string(r)) + s[n:]
}

// npmPackage returns the directory of the package.json nearest to file
// among sources ("." for the root), ok false when there is none (W7).
func npmPackage(file string, sources map[string][]byte) (string, bool) {
	dir := path.Dir(file)
	for {
		manifest := path.Join(dir, "package.json")
		if _, ok := sources[manifest]; ok {
			return dir, true
		}
		if dir == "." || dir == "/" || dir == "" {
			return "", false
		}
		dir = path.Dir(dir)
	}
}

// Delegate operations (CT-R1) and the operation each one records.
var prismaReadOps = map[string]string{
	"findUnique": "find", "findUniqueOrThrow": "find", "findFirst": "find", "findFirstOrThrow": "find", "findMany": "find",
	"count": "count", "aggregate": "aggregate", "groupBy": "aggregate",
}

var prismaWriteOps = map[string]string{
	"create": "insert", "createMany": "insert", "createManyAndReturn": "insert",
	"update": "update", "updateMany": "update", "updateManyAndReturn": "update",
	"upsert": "upsert", "delete": "delete", "deleteMany": "delete",
}

// prismaExecutions are the client methods that run statements built
// elsewhere: a $transaction over queries or a callback, and TypedSQL's
// $queryRawTyped over a .sql file the link pass does not read (row D).
var prismaExecutions = map[string]bool{"$transaction": true, "$queryRawTyped": true}

// prismaFile is one TypeScript file's Prisma sites in link order.
type prismaFile struct {
	in    Input
	file  string
	facts facts.Facts
	group *prismaGroup
}

// linkPrismaFile reads a file's calls against the Prisma group of its
// package; executions holding no site are row D's statement_not_traced
// records.
func linkPrismaFile(in Input, file string, f facts.Facts, all []*prismaGroup, a *analysis) {
	if f.Routes == nil || len(f.Routes.Clients) == 0 {
		return
	}
	dir, ok := npmPackage(file, in.Sources)
	if !ok {
		return
	}
	// Discovery names one schema per package, so a package has at most
	// one group (W7; "exactly one Prisma group" of CT-R1).
	var group *prismaGroup
	for _, g := range all {
		if hasString(g.packages, dir) {
			group = g
		}
	}
	if group == nil {
		return
	}
	l := prismaFile{in: in, file: file, facts: f, group: group}
	var spans []contract.Span
	var executions []facts.ClientCall
	for _, c := range f.Routes.Clients {
		callee, method, ok := l.callee(c)
		if !ok {
			continue
		}
		if prismaExecutions[method] {
			executions = append(executions, c)
			continue
		}
		if u, ok := l.fluent(c, callee, method); ok {
			l.in.tag("CT-R2")
			a.unknowns[c.Owner] = append(a.unknowns[c.Owner], u)
			continue
		}
		sites := l.site(c, callee, method)
		for _, s := range sites {
			spans = append(spans, s.span)
			a.sites = append(a.sites, s)
		}
	}
	for _, c := range executions {
		traced := false
		for _, s := range spans {
			traced = traced || contains(c.Span, s) && c.Span != s
		}
		if traced {
			continue
		}
		in.tag("D")
		a.unknowns[c.Owner] = append(a.unknowns[c.Owner], Unknown{Row: "D", Line: c.Span.StartLine, Span: c.Span, Tool: "prisma", Reason: ReasonStatementNotTraced})
	}
}

// fluentOps are the single-record operations whose result has Prisma's
// fluent API (prisma.user.findUnique(...).posts()).
var fluentOps = map[string]bool{
	"findUnique": true, "findUniqueOrThrow": true, "findFirst": true, "findFirstOrThrow": true,
	"create": true, "update": true, "upsert": true, "delete": true,
}

// fluent applies R5-C1: a call of a relation field on a single-record
// delegate call (or on a fluent call) reads the related table, a form the
// rows do not claim. It returns a site_partial entry naming the relation
// with the related table (and an implicit many-to-many join table) as
// candidates, null when the related model has no table. The delegate call
// itself is its own CT-R1 site.
func (l prismaFile) fluent(c facts.ClientCall, callee facts.RouteExpr, method string) (Unknown, bool) {
	if callee.Object == nil || callee.Object.Kind != "call_expression" {
		return Unknown{}, false
	}
	model, ok := l.fluentModel(*callee.Object, 0)
	if !ok {
		return Unknown{}, false
	}
	rf, ok := l.group.models[model].relations[method]
	if !ok {
		// then, catch, finally or any other method: not a relation.
		return Unknown{}, false
	}
	var candidates []string
	if t, ok := l.group.models[rf.Model]; ok {
		candidates = append(candidates, t.identity)
		if join := l.group.joins[model+"."+method]; join != "" {
			candidates = append(candidates, join)
		}
		sort.Strings(candidates)
	}
	return Unknown{Row: "CT-R2", Line: c.Span.StartLine, Span: c.Span, Tool: prisma.Tool, Partial: true, Candidates: candidates,
		Reason: ReasonSitePartial + ": fluent relation " + model + "." + method}, true
}

// fluentModel returns the model a call's single record belongs to: a
// fluent operation of a delegate of the group, or a relation field called
// on such a call.
func (l prismaFile) fluentModel(e facts.RouteExpr, depth int) (string, bool) {
	if depth > 8 || e.Kind != "call_expression" || e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Object == nil {
		return "", false
	}
	callee := *e.Object
	inner := *callee.Object
	if fluentOps[callee.Name] && inner.Kind == "member_expression" && inner.Object != nil {
		model, ok := l.group.delegates[inner.Name]
		return model, ok
	}
	if inner.Kind != "call_expression" {
		return "", false
	}
	model, ok := l.fluentModel(inner, depth+1)
	if !ok {
		return "", false
	}
	rf, ok := l.group.models[model].relations[callee.Name]
	if !ok {
		return "", false
	}
	if _, ok := l.group.models[rf.Model]; !ok {
		return "", false
	}
	return rf.Model, true
}

// callee returns a call's member callee and method name. A generic tagged
// template (prisma.$queryRaw<T> followed by a template) is not read: the
// TypeScript grammar parses it only by error recovery, so it is no site
// (known gap).
func (l prismaFile) callee(c facts.ClientCall) (facts.RouteExpr, string, bool) {
	e := c.Expression
	if e.Kind != "call_expression" || e.Object == nil {
		return facts.RouteExpr{}, "", false
	}
	o := *e.Object
	if o.Kind == "member_expression" && o.Object != nil {
		return o, o.Name, true
	}
	return facts.RouteExpr{}, "", false
}

func (l prismaFile) newSite(c facts.ClientCall, row, tool string) site {
	return site{file: l.file, span: c.Span, owner: c.Owner, row: row, tool: tool, rows: []string{row},
		assumes: []string{"TA-V3a"}, role: RoleExecutes, verification: rowByID(row).Verification,
		reads: map[string]map[string]bool{}, writes: map[string]map[string]bool{}}
}

// site classifies one call: a delegate operation (CT-R1, with its CT-R2
// relations), a raw query (CT-R3), or no site.
func (l prismaFile) site(c facts.ClientCall, callee facts.RouteExpr, method string) []site {
	switch method {
	case "$queryRaw", "$executeRaw", "$queryRawUnsafe", "$executeRawUnsafe":
		return []site{l.claimPrismaRaw(c, callee, method)}
	}
	readOp, read := prismaReadOps[method]
	writeOp, write := prismaWriteOps[method]
	if !read && !write || callee.Object == nil {
		return nil
	}
	delegate := *callee.Object
	if delegate.Kind == "subscript_expression" {
		// <r>[expr].<op>: the model is not known (CT-R1 refusal).
		l.in.tag("CT-R1/<r>[expr]")
		s := l.newSite(c, "CT-R1", prisma.Tool)
		s.refuse(ReasonEntityUnresolved+": a dynamic delegate "+delegate.Text, false, append([]string(nil), l.group.tables...))
		return []site{s}
	}
	if delegate.Kind != "member_expression" || delegate.Object == nil {
		return nil
	}
	model, ok := l.group.delegates[delegate.Name]
	if !ok {
		// Not a delegate of the package's group: no site.
		return nil
	}
	return l.claimPrismaDelegate(c, l.group, model, method, readOp, writeOp, write)
}

// claimPrismaDelegate is row CT-R1's branch: the delegate's model table is
// read or written; the argument object's relation fields are row CT-R2
// (claimPrismaRelations), a site of its own when it claims a table.
func (l prismaFile) claimPrismaDelegate(c facts.ClientCall, g *prismaGroup, model, method, readOp, writeOp string, write bool) []site {
	l.in.tag("CT-R1/" + method)
	s := l.newSite(c, "CT-R1", prisma.Tool)
	m := g.models[model]
	if write {
		addOp(s.writes, m.identity, writeOp)
	} else {
		addOp(s.reads, m.identity, readOp)
	}
	r := l.claimPrismaRelations(c, g, model, write)
	if len(r.reads)+len(r.writes) == 0 {
		s.extra = r.extra
		return []site{s}
	}
	return []site{s, r}
}

// relationWalk collects CT-R2's claims and the site's partial and
// write-side unknowns while the argument object is read.
type relationWalk struct {
	l       prismaFile
	g       *prismaGroup
	s       *site
	partial reasonSet
	side    reasonSet
}

// reasonSet is the details of one unknown entry and its candidates; open
// is set when a detail does not narrow the tables (candidates null).
type reasonSet struct {
	details    []string
	candidates map[string]bool
	open       bool
}

func (r *reasonSet) add(detail string, candidates ...string) {
	if !hasString(r.details, detail) {
		r.details = append(r.details, detail)
	}
	if len(candidates) == 0 {
		r.open = true
	}
	if r.candidates == nil {
		r.candidates = map[string]bool{}
	}
	for _, c := range candidates {
		r.candidates[c] = true
	}
}

// unknown renders the set as an entry on the site (nil when empty).
func (r reasonSet) unknown(s *site, code string, partial bool) *Unknown {
	if len(r.details) == 0 {
		return nil
	}
	var candidates []string
	if !r.open {
		candidates = sortedSet(r.candidates)
	}
	return &Unknown{Row: s.row, Line: s.span.StartLine, Span: s.span, Tool: s.tool, Reason: code + ": " + strings.Join(r.details, ", "), Partial: partial, Candidates: candidates}
}

// claimPrismaRelations is row CT-R2's branch over a CT-R1 site's argument
// object. It returns the relation site (its claims, with every unknown of
// the delegate call attached).
func (l prismaFile) claimPrismaRelations(c facts.ClientCall, g *prismaGroup, model string, write bool) site {
	s := l.newSite(c, "CT-R2", prisma.Tool)
	w := &relationWalk{l: l, g: g, s: &s}
	if len(c.Expression.Args) > 0 {
		w.args(model, c.Expression.Args[0], write)
	}
	if u := w.side.unknown(&s, ReasonRelationWriteSide, true); u != nil {
		s.extra = append(s.extra, *u)
	}
	if u := w.partial.unknown(&s, ReasonSitePartial, true); u != nil {
		s.extra = append(s.extra, *u)
	}
	if len(s.reads)+len(s.writes) > 0 {
		l.in.tag("CT-R2")
	}
	return s
}

// literalFalse reports a value that selects or includes nothing.
func literalFalse(e facts.RouteExpr) bool {
	return e.Kind == "false" || e.Kind == "null" || e.Kind == "undefined"
}

// object returns the entries of an object literal in source order; ok is
// false for anything else. A spread or an entry the reduction cannot name
// makes the object incomplete, recorded as partial.
func (w *relationWalk) object(e facts.RouteExpr, where string) ([]string, bool) {
	if e.Kind != "object" {
		return nil, false
	}
	if e.Spread {
		w.l.in.tag("CT-R2/Spread")
		w.partial.add("an object spread in " + where)
	} else if e.Invalid {
		w.partial.add("an object entry that is not a plain key in " + where)
	}
	keys := e.Keys
	if len(keys) == 0 {
		for k := range e.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	return keys, true
}

// args reads a delegate call's argument object.
func (w *relationWalk) args(model string, e facts.RouteExpr, write bool) {
	keys, ok := w.object(e, "the argument")
	if !ok {
		w.partial.add("the argument is not an object literal")
		return
	}
	for _, k := range keys {
		v := e.Properties[k]
		switch k {
		case "where":
			w.where(model, v, k)
		case "include", "select":
			w.selection(model, v, k)
		case "orderBy":
			w.orderBy(model, v)
		case "data", "create", "update":
			if write {
				w.data(model, v, k)
			}
		}
	}
}

// claim records a read or write of a relation's related table, and the
// relation's join table (implicit many-to-many) or unknown side as partial.
func (w *relationWalk) claim(model string, rf prisma.RelationField, op string, write bool) {
	target, ok := w.g.models[rf.Model]
	if !ok {
		w.partial.add("relation " + model + "." + rf.Name + " to a model with no claimed table")
		return
	}
	if write {
		addOp(w.s.writes, target.identity, op)
	} else {
		addOp(w.s.reads, target.identity, op)
	}
	switch {
	case rf.ManyToMany:
		if join := w.g.joins[model+"."+rf.Name]; join != "" {
			w.partial.add("the join table of "+model+"."+rf.Name, join)
		} else {
			w.partial.add("the join table of " + model + "." + rf.Name)
		}
	case rf.Unread && rf.List:
		w.partial.add("relation " + model + "." + rf.Name + " whose side is not read")
	}
}

// relation returns the relation field of a model named by key.
func (w *relationWalk) relation(model, key string) (prisma.RelationField, bool) {
	rf, ok := w.g.models[model].relations[key]
	return rf, ok
}

// related returns the identity of a relation's related table ("" when it
// has none), for candidates.
func (w *relationWalk) related(rf prisma.RelationField) []string {
	if t, ok := w.g.models[rf.Model]; ok {
		return []string{t.identity}
	}
	return nil
}

// selection reads an include or select object: each relation field
// selected is read, its own arguments read for the related model.
func (w *relationWalk) selection(model string, e facts.RouteExpr, key string) {
	keys, ok := w.object(e, key)
	if !ok {
		if !literalFalse(e) && e.Kind != "true" {
			w.partial.add(key + " is not an object literal")
		}
		return
	}
	for _, k := range keys {
		v := e.Properties[k]
		if k == "_count" {
			w.count(model, v)
			continue
		}
		rf, ok := w.relation(model, k)
		if !ok || literalFalse(v) {
			continue
		}
		w.l.in.tag("CT-R2/" + key)
		w.claim(model, rf, "find", false)
		switch {
		case v.Kind == "true":
		case v.Kind == "object":
			w.nestedRead(rf.Model, v, key+"."+k)
		default:
			w.partial.add(key + "." + k + " is not true or an object literal")
		}
	}
}

// nestedRead reads the arguments of an included or selected relation.
func (w *relationWalk) nestedRead(model string, e facts.RouteExpr, where string) {
	keys, _ := w.object(e, where)
	for _, k := range keys {
		v := e.Properties[k]
		switch k {
		case "where":
			w.where(model, v, where+".where")
		case "include", "select":
			w.selection(model, v, k)
		case "orderBy":
			w.orderBy(model, v)
		}
	}
}

// count reads _count in include or select: relation counts read the
// related tables, a form the row does not list (partial).
func (w *relationWalk) count(model string, e facts.RouteExpr) {
	if literalFalse(e) {
		return
	}
	var cands []string
	if e.Kind == "object" && !e.Spread && !e.Invalid {
		if sel, ok := e.Properties["select"]; ok && sel.Kind == "object" && !sel.Spread && !sel.Invalid {
			for k, v := range sel.Properties {
				if rf, ok := w.relation(model, k); ok && !literalFalse(v) {
					cands = append(cands, w.related(rf)...)
				}
			}
			if len(cands) > 0 {
				sort.Strings(cands)
				w.partial.add("_count of relations", cands...)
				return
			}
		}
	}
	for _, rf := range w.g.models[model].relations {
		cands = append(cands, w.related(rf)...)
	}
	sort.Strings(cands)
	w.partial.add("_count of relations", cands...)
}

// relationFilters are the CT-R2 relation filters in where.
var relationFilters = map[string]bool{"some": true, "every": true, "none": true, "is": true, "isNot": true}

// where reads a where filter: a relation filter (some, every, none, is,
// isNot) reads the related table, nested filters are read for it; AND, OR
// and NOT nest filters of the same model.
func (w *relationWalk) where(model string, e facts.RouteExpr, at string) {
	if e.Kind == "array" {
		for _, p := range e.Parts {
			w.where(model, p, at)
		}
		return
	}
	keys, ok := w.object(e, at)
	if !ok {
		w.partial.add(at + " is not an object literal")
		return
	}
	for _, k := range keys {
		v := e.Properties[k]
		if k == "AND" || k == "OR" || k == "NOT" {
			w.where(model, v, at+"."+k)
			continue
		}
		rf, ok := w.relation(model, k)
		if !ok {
			continue
		}
		filters, ok := w.object(v, at+"."+k)
		if !ok {
			w.partial.add(at+"."+k+" is not a relation filter object", w.related(rf)...)
			continue
		}
		for _, f := range filters {
			fv := v.Properties[f]
			if !relationFilters[f] {
				// A to-one filter without is/isNot: not a listed form.
				w.partial.add(at+"."+k+" filters the relation without some, every, none, is or isNot", w.related(rf)...)
				continue
			}
			if fv.Kind != "object" {
				// is: null and the like: whether the related table is
				// read depends on the relation's side.
				w.partial.add(at+"."+k+"."+f+" is not a filter object", w.related(rf)...)
				continue
			}
			w.l.in.tag("CT-R2/" + f)
			w.claim(model, rf, "find", false)
			w.where(rf.Model, fv, at+"."+k+"."+f)
		}
	}
}

// orderBy reads an orderBy object or list: ordering by a relation reads
// the related table, a form the row does not list (partial).
func (w *relationWalk) orderBy(model string, e facts.RouteExpr) {
	if e.Kind == "array" {
		for _, p := range e.Parts {
			w.orderBy(model, p)
		}
		return
	}
	keys, ok := w.object(e, "orderBy")
	if !ok {
		w.partial.add("orderBy is not an object literal")
		return
	}
	for _, k := range keys {
		if rf, ok := w.relation(model, k); ok {
			w.partial.add("orderBy relation "+model+"."+k, w.related(rf)...)
		}
	}
}

// nestedWrites are the CT-R2 nested writes under data and the operation
// each one records on the related table.
var nestedWrites = map[string]string{
	"create": "insert", "createMany": "insert", "connectOrCreate": "upsert", "upsert": "upsert",
	"update": "update", "updateMany": "update", "delete": "delete", "deleteMany": "delete",
}

// data reads a data object (or a list of them): each relation field holds
// nested writes on the related model.
func (w *relationWalk) data(model string, e facts.RouteExpr, at string) {
	if e.Kind == "array" {
		for _, p := range e.Parts {
			w.data(model, p, at)
		}
		return
	}
	keys, ok := w.object(e, at)
	if !ok {
		w.partial.add(at + " is not an object literal")
		return
	}
	for _, k := range keys {
		rf, ok := w.relation(model, k)
		if !ok {
			continue
		}
		v := e.Properties[k]
		ops, ok := w.object(v, at+"."+k)
		if !ok {
			w.partial.add(at + "." + k + " is not an object literal of nested writes")
			continue
		}
		for _, op := range ops {
			ov := v.Properties[op]
			switch op {
			case "connect", "disconnect", "set":
				// The table holding the key depends on the relation's side
				// (an implicit many-to-many writes the join table).
				w.l.in.tag("CT-R2/" + op)
				cands := []string{w.g.models[model].identity}
				cands = append(cands, w.related(rf)...)
				if join := w.g.joins[model+"."+rf.Name]; join != "" {
					cands = append(cands, join)
				}
				if rf.ManyToMany && w.g.joins[model+"."+rf.Name] == "" || rf.Unread {
					w.side.add(model + "." + rf.Name + " " + op)
				} else {
					w.side.add(model+"."+rf.Name+" "+op, cands...)
				}
				continue
			}
			kind, listed := nestedWrites[op]
			if !listed {
				w.partial.add(at + "." + k + "." + op + " is not a listed nested write")
				continue
			}
			if literalFalse(ov) {
				continue
			}
			w.l.in.tag("CT-R2/" + op)
			w.claim(model, rf, kind, true)
			w.nested(rf.Model, op, ov, at+"."+k+"."+op)
		}
	}
}

// nested reads the payload of a nested write for the related model: the
// data it creates or updates, and the filters it selects rows with.
func (w *relationWalk) nested(model, op string, e facts.RouteExpr, at string) {
	if e.Kind == "array" {
		for _, p := range e.Parts {
			w.nested(model, op, p, at)
		}
		return
	}
	if e.Kind == "true" {
		return
	}
	if e.Kind != "object" {
		w.partial.add(at + " is not an object literal")
		return
	}
	switch op {
	case "create":
		w.data(model, e, at)
	case "createMany":
		if d, ok := e.Properties["data"]; ok {
			w.data(model, d, at+".data")
		}
	case "connectOrCreate":
		if d, ok := e.Properties["create"]; ok {
			w.data(model, d, at+".create")
		}
	case "upsert":
		for _, k := range []string{"create", "update"} {
			if d, ok := e.Properties[k]; ok {
				w.data(model, d, at+"."+k)
			}
		}
		if f, ok := e.Properties["where"]; ok {
			w.where(model, f, at+".where")
		}
	case "update", "updateMany":
		if d, ok := e.Properties["data"]; ok {
			w.data(model, d, at+".data")
			if f, ok := e.Properties["where"]; ok {
				w.where(model, f, at+".where")
			}
			return
		}
		if op == "update" {
			w.data(model, e, at)
		} else {
			w.partial.add(at + " has no data")
		}
	case "delete", "deleteMany":
		w.where(model, e, at)
	}
}

// claimPrismaRaw is row CT-R3's branch: raw SQL text through the reader
// against the package's Prisma group (O3), every edge inferred under
// TA-V1 and TA-V3a.
func (l prismaFile) claimPrismaRaw(c facts.ClientCall, callee facts.RouteExpr, method string) site {
	l.in.tag("CT-R3/" + method)
	s := l.newSite(c, "CT-R3", "sql")
	s.assumes = []string{"TA-V1", "TA-V3a"}
	s.reads, s.writes = nil, nil
	var sql string
	if method == "$queryRaw" || method == "$executeRaw" {
		text, ok, detail := l.taggedSQL(c, callee)
		if !ok {
			s.refuse(ReasonNotLiteral+": "+detail, false, nil)
			return s
		}
		sql = text
	} else {
		args := c.Expression.Args
		if len(args) == 0 || args[0].Kind != "literal" {
			s.refuse(ReasonNotLiteral+": the first argument of "+method+" is not a string literal", false, nil)
			return s
		}
		sql = args[0].Name
	}
	g := l.group
	if g.dialect != "postgresql" {
		s.refuse(ReasonNoSchemaSource+": the Prisma group's provider is "+strconv.Quote(g.dialect)+", not postgresql (TA-V4)", false, nil)
		return s
	}
	s.sql = sql
	s.sets = []*migrationSet{g.source}
	readSQLSite(l.in, &s, sql, s.sets)
	return s
}

// taggedSQL reads a tagged template call's SQL: the cooked text with each
// substitution as a bound parameter $n (Prisma's semantics for $queryRaw
// and $executeRaw). A call with parentheses, or a substitution that is a
// Prisma.sql, Prisma.raw, Prisma.join or Prisma.empty fragment (inline, or
// a local bound to one), is not literal.
func (l prismaFile) taggedSQL(c facts.ClientCall, callee facts.RouteExpr) (string, bool, string) {
	text := c.Expression.Text
	if i := strings.Index(text, callee.Text); i >= 0 {
		text = text[i+len(callee.Text):]
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<") {
		depth := 0
		for i, r := range text {
			if r == '<' {
				depth++
			} else if r == '>' {
				depth--
				if depth == 0 {
					text = strings.TrimSpace(text[i+1:])
					break
				}
			}
		}
	}
	if !strings.HasPrefix(text, "`") {
		return "", false, callee.Name + " is called with an argument, not a tagged template"
	}
	var b strings.Builder
	n := 0
	for _, a := range c.Expression.Args {
		switch a.Kind {
		case "string_fragment":
			b.WriteString(a.Text)
		case "escape_sequence":
			v, ok := cookEscape(a.Text)
			if !ok {
				return "", false, "an escape sequence " + a.Text + " in the template"
			}
			b.WriteString(v)
		case "template_substitution":
			if a.Object == nil {
				return "", false, "an empty substitution"
			}
			if l.prismaFragment(c.Scope, *a.Object, 0) {
				l.in.tag("CT-R3/Prisma.sql")
				return "", false, "a Prisma SQL fragment " + a.Text + " in the template"
			}
			n++
			b.WriteString("$" + strconv.Itoa(n))
		case "comment":
		default:
			return "", false, "a template part " + a.Kind
		}
	}
	return b.String(), true, ""
}

// prismaFragment reports an expression that is (or a local bound to) a
// Prisma SQL fragment: rooted at Prisma (Prisma.sql, Prisma.raw,
// Prisma.join, Prisma.empty) or at a name imported from a @prisma/
// module other than the client's namespace object.
func (l prismaFile) prismaFragment(scope int, e facts.RouteExpr, depth int) bool {
	if depth > 4 {
		return true
	}
	root := e
	for (root.Kind == "call_expression" || root.Kind == "member_expression") && root.Object != nil {
		root = *root.Object
	}
	if root.Kind != "identifier" {
		return false
	}
	if root.Name == "Prisma" {
		return true
	}
	b, ok := l.binding(scope, root.Name)
	if !ok {
		return false
	}
	if strings.HasPrefix(b.Module, "@prisma/") && b.Export != "PrismaClient" {
		return true
	}
	if root.Kind == "identifier" && e.Kind == "identifier" && (b.Value.Kind == "call_expression" || b.Value.Kind == "member_expression") {
		return l.prismaFragment(scope, b.Value, depth+1)
	}
	return false
}

// binding finds the nearest binding of a name from a scope.
func (l prismaFile) binding(scope int, name string) (facts.RouteBinding, bool) {
	scopes := l.facts.Routes.Scopes
	for s := scope; s >= 0 && s < len(scopes); s = scopes[s].Parent {
		if b, ok := scopes[s].Bindings[name]; ok {
			return b, true
		}
		if scopes[s].Parent == s {
			break
		}
	}
	return facts.RouteBinding{}, false
}

// cookEscape decodes one JavaScript template escape sequence (the cooked
// string Prisma reads); ok false for one it does not decode.
func cookEscape(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '\\' {
		return "", false
	}
	switch c := raw[1]; c {
	case 'n':
		return "\n", true
	case 't':
		return "\t", true
	case 'r':
		return "\r", true
	case 'b':
		return "\b", true
	case 'f':
		return "\f", true
	case 'v':
		return "\v", true
	case '0':
		if len(raw) == 2 {
			return "\x00", true
		}
	case '\n':
		return "", true
	case '\r':
		return "", true
	case 'x', 'u':
		hex := strings.Trim(raw[2:], "{}")
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return "", false
		}
		return string(rune(v)), true
	default:
		if len(raw) == 2 {
			return raw[1:], true
		}
	}
	return "", false
}

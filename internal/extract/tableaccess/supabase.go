package tableaccess

import (
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/models/supabasetypes"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file holds the Supabase rows (T024c2, ADR 0077; rows SB-K1 and
// SB-T1..SB-T5 of docs/contract/claims/nextjs-supabase.md, copied into
// docs/extract/table-access.md "Supabase rows"): a PostgREST chain
// `<client>.from('t').<verb>(...)` on a Supabase JS client proven through
// its factory reads or writes the table of the package's Supabase
// migration set (T024c1). Receivers are proven only through the factories
// (never by name); every claim is inferred (assumption SB-V1).

// SupabaseTool is the table_access tool of the Supabase rows.
const SupabaseTool = "supabase"

// SupabaseContract is the Supabase claim contract in the task's order:
// SB-K1 (the receiver proof, reached through the chain rows), SB-T1..SB-T5
// and the table's own default branch D. TestSupabaseContract keeps it, the
// task table and the docs copy 1:1.
var SupabaseContract = []Row{
	{ID: "SB-K1", Side: SideReader, Forms: SupabaseClientForms, Branch: "supabaseClient", Unit: "T024c2", Verification: VerificationMapped},
	{ID: "SB-T1", Side: SideClaim, Tool: SupabaseTool, Assumptions: []string{"SB-V1", "SB-V6"}, Forms: SupabaseVerbForms, Branch: "claimSupabaseTable", Unit: "T024c2", Verification: VerificationOracle},
	{ID: "SB-T2", Side: SideClaim, Tool: SupabaseTool, Assumptions: []string{"SB-V1", "SB-V6"}, Forms: []string{"non-literal schema"}, Branch: "supabaseSchema", Unit: "T024c2", Verification: VerificationOracle},
	{ID: "SB-T3", Side: SideClaim, Tool: SupabaseTool, Assumptions: []string{"SB-V1"}, Forms: SupabaseEmbedForms, Branch: "claimSupabaseEmbeds", Unit: "T024c2", Verification: VerificationMapped},
	{ID: "SB-T4", Side: SideClaim, Tool: SupabaseTool, Branch: "supabaseDynamicTable", Unit: "T024c2", Verification: VerificationMapped},
	{ID: "SB-T5", Side: SideClaim, Tool: SupabaseTool, Branch: "supabaseUnprovenClient", Unit: "T024c2", Verification: VerificationMapped},
	{ID: "D", Side: SideDefault, Forms: SupabaseDefaultForms, Branch: "supabaseDefault", Unit: "T024c2"},
}

// SupabaseAssumptions are the Supabase rows' assumptions.
var SupabaseAssumptions = []string{"SB-V1"}

// SupabaseAssumptionTexts are the texts the schema API's catalogue serves
// for SupabaseAssumptions (the entry of docs/extract/table-access.md
// "Supabase rows", as AssumptionTexts).
var SupabaseAssumptionTexts = []AssumptionText{
	{ID: "SB-V1", Title: "Supabase client talks to the migrated database", Text: "The database the client talks to is the one the repository's Supabase migrations describe (as TA-V1): every SB edge is `inferred`."},
}

// SupabaseClientForms are the SB-K1 factories, proofs and refusals.
var SupabaseClientForms = []string{
	"createClient", "createBrowserClient", "createServerClient", "create*Client",
	"directly", "const binding", "imported consts", "one level of a function", "db.schema",
	"destructuring", "parameters", "let", "members",
}

// SupabaseVerbForms are the SB-T1 verbs and name forms.
var SupabaseVerbForms = []string{"select", "insert", "update", "upsert", "delete", "insert(...).select()", "bounded const", "generated types", "const builder", "builder_unread"}

// SupabaseEmbedForms are the SB-T3 embed forms and refusals.
var SupabaseEmbedForms = []string{"a:e", "nested levels", "one foreign key", "join modifier", "!hint", "...e", "zero or several relationships", "junction table", "no table of the set", "non-literal select", "relationships not resolved"}

// SupabaseDefaultForms are the D forms the Supabase pass recognises (and
// claims nothing for).
var SupabaseDefaultForms = []string{".storage.from(bucket)", ".rpc(fn)"}

// supabaseFactories are the SB-K1 factories by module.
var supabaseFactories = map[string]map[string]string{
	"@supabase/supabase-js": {"createClient": "createClient"},
	"@supabase/ssr":         {"createBrowserClient": "createBrowserClient", "createServerClient": "createServerClient"},
	"@supabase/auth-helpers-nextjs": {
		"createClientComponentClient": "create*Client", "createServerComponentClient": "create*Client",
		"createRouteHandlerClient": "create*Client", "createMiddlewareClient": "create*Client",
		"createServerActionClient": "create*Client", "createPagesBrowserClient": "create*Client",
		"createPagesServerClient": "create*Client",
	},
}

// supabaseVerbs are the PostgREST query builder verbs (SB-T1) and the
// operation each one records.
var supabaseVerbs = map[string]string{"select": "select", "insert": "insert", "update": "update", "upsert": "upsert", "delete": "delete"}

// supabaseClient is the result of SB-K1 for one receiver expression.
type supabaseClient struct {
	proven bool
	// schema is the client's default schema: public, or a literal
	// db.schema option.
	schema string
	// detail says why the receiver is no proven client.
	detail string
	forms  []string
}

// supabaseTable is one table of a Supabase set as the embed resolution
// reads it.
type supabaseTable struct {
	identity, schema, name string
}

// supabaseSet is one Supabase migration set with what SB-T3 needs.
type supabaseSet struct {
	source *migrationSet
	// complete is true when every table of the set is complete and the
	// set has no opaque statement: only then can a count of foreign keys
	// prove "exactly one relationship" (an unread statement may add a key
	// or a junction table).
	complete bool
	// keys are the foreign key edges between tables, one per referencing
	// column (a key over two columns counts twice, so it is never "one").
	keys [][2]string
	// generated is true for a set read from Supabase's generated types
	// (T024c3): its relationships are not resolved to foreign keys, so no
	// embed is proven through it.
	generated bool
}

// supabaseLinker reads the Supabase sets of a repository.
type supabaseLinker struct {
	in Input
	// sets maps an npm package directory to its Supabase sets.
	sets map[string][]*supabaseSet
	// declares caches whether a package's package.json declares a
	// Supabase JS package (the SB-T5 gate).
	declares map[string]bool
}

func newSupabaseLinker(in Input) *supabaseLinker {
	l := &supabaseLinker{in: in, sets: map[string][]*supabaseSet{}, declares: map[string]bool{}}
	all := migrationSets(in.Schema)
	var names []string
	for name, s := range all {
		if s.tool == SupabaseTool {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	state := map[string]string{}
	for _, n := range in.Schema {
		if n.Kind == "table" {
			state[n.Identity], _ = n.Attributes["state"].(string)
		}
	}
	tableOf := map[string]string{}
	for _, e := range in.SchemaEdges {
		if e.Kind == "contains" && strings.HasPrefix(e.Source, "table:") && strings.HasPrefix(e.Target, "column:") {
			tableOf[e.Target] = e.Source
		}
	}
	for _, name := range names {
		s := all[name]
		set := &supabaseSet{source: s, complete: s.opaque == 0}
		member := map[string]bool{}
		for _, t := range s.tables {
			member[t.identity] = true
			if state[t.identity] != "complete" {
				set.complete = false
			}
		}
		for _, e := range in.SchemaEdges {
			if e.Kind != "foreign_key" {
				continue
			}
			from, to := tableOf[e.Source], tableOf[e.Target]
			if member[from] && member[to] {
				set.keys = append(set.keys, [2]string{from, to})
			}
		}
		dir, ok := npmPackage(s.pkg, in.Sources)
		if !ok {
			continue
		}
		l.sets[dir] = append(l.sets[dir], set)
	}
	for _, set := range generatedSets(in.SupabaseTypes) {
		// SB-G2: a package with a Supabase migration set keeps the set (the
		// types front end emits no tables there; this guards the rule).
		if len(l.sets[set.pkg]) > 0 {
			continue
		}
		l.sets[set.pkg] = append(l.sets[set.pkg], &supabaseSet{source: set.source, generated: true})
	}
	return l
}

// generatedSet is one group of tables read from generated types with its
// package directory.
type generatedSet struct {
	pkg    string
	source *migrationSet
}

// generatedSets reads the generated-types groups (one per types file) from
// their table records; a group without a package is never a package's
// schema source.
func generatedSets(nodes []contract.Node) []generatedSet {
	bySet := map[string]*generatedSet{}
	var names []string
	for _, n := range nodes {
		if n.Kind != "table" || n.Attributes["tool"] != supabasetypes.Tool {
			continue
		}
		pkg, ok := n.Attributes["package"].(string)
		if !ok {
			continue
		}
		set, _ := n.Attributes["set"].(string)
		g := bySet[set]
		if g == nil {
			g = &generatedSet{pkg: pkg, source: &migrationSet{name: set, pkg: pkg, dialect: "postgres", tool: supabasetypes.Tool}}
			bySet[set] = g
			names = append(names, set)
		}
		t := table{identity: n.Identity}
		t.name, _ = n.Attributes["name"].(string)
		t.schema, _ = n.Attributes["schema"].(string)
		g.source.tables = append(g.source.tables, t)
	}
	sort.Strings(names)
	var out []generatedSet
	for _, name := range names {
		g := bySet[name]
		sort.Slice(g.source.tables, func(i, j int) bool { return g.source.tables[i].identity < g.source.tables[j].identity })
		out = append(out, *g)
	}
	return out
}

// packageDeclares reports whether the package.json of a package directory
// declares a Supabase JS package (dependencies or devDependencies).
func (l *supabaseLinker) packageDeclares(dir string) bool {
	if v, ok := l.declares[dir]; ok {
		return v
	}
	var manifest struct {
		Dependencies    map[string]any `json:"dependencies"`
		DevDependencies map[string]any `json:"devDependencies"`
	}
	declared := false
	if err := json.Unmarshal(l.in.Sources[path.Join(dir, "package.json")], &manifest); err == nil {
		for module := range supabaseFactories {
			_, a := manifest.Dependencies[module]
			_, b := manifest.DevDependencies[module]
			declared = declared || a || b
		}
	}
	l.declares[dir] = declared
	return declared
}

// binding finds the nearest binding of a name from a scope of a file and
// the scope that holds it.
func (l *supabaseLinker) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
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

// supabaseClient is row SB-K1's branch: it proves a receiver expression a
// Supabase client through a factory call, directly, through a const
// binding (imported consts included) or one level of a function whose
// every return is a proven client. wrapper is false inside a wrapper's
// returns (one level only).
func (l *supabaseLinker) supabaseClient(file string, scope int, e facts.RouteExpr, depth int, wrapper bool) supabaseClient {
	if depth > 8 {
		return supabaseClient{detail: "a binding chain too deep to follow"}
	}
	switch e.Kind {
	case "await_expression":
		if e.Object == nil {
			return supabaseClient{detail: "an await the caches do not keep"}
		}
		if e.Object.Kind == "call_expression" {
			return l.clientCall(file, scope, *e.Object, depth+1, wrapper, true)
		}
		return l.supabaseClient(file, scope, *e.Object, depth+1, wrapper)
	case "call_expression":
		return l.clientCall(file, scope, e, depth, wrapper, false)
	case "identifier":
		b, at, ok := l.binding(file, scope, e.Name)
		if !ok {
			return supabaseClient{detail: e.Name + " is not bound in the file"}
		}
		if b.Module != "" {
			if l.in.Modules == nil {
				return supabaseClient{detail: "no module resolver"}
			}
			x := l.in.Modules.ImportBinding(file, b.Module, b.Export, "")
			if x.External || x.File == "" || x.Namespace || x.Ambiguous {
				return supabaseClient{detail: e.Name + " is imported from " + b.Module + " and names no const of the repository"}
			}
			r := l.in.TypeScript[x.File].Routes
			if r == nil || len(r.Scopes) == 0 {
				return supabaseClient{detail: e.Name + " names no const of " + x.File}
			}
			v, ok := r.Scopes[0].Bindings[x.Name]
			if !ok || v.Module != "" || !v.ModuleConst || v.Mutable || v.Value.Kind == "" || v.Value.Invalid {
				return supabaseClient{detail: e.Name + " names no module const of " + x.File}
			}
			c := l.supabaseClient(x.File, 0, v.Value, depth+1, wrapper)
			if c.proven {
				c.forms = append(c.forms, "imported consts")
			}
			return c
		}
		switch {
		case b.Value.Kind == "parameter":
			l.in.tag("SB-K1/parameters")
			return supabaseClient{detail: "a parameter " + e.Name}
		case b.Mutable:
			l.in.tag("SB-K1/let")
			return supabaseClient{detail: "a let or var binding " + e.Name}
		case b.Value.Kind == "":
			// A destructured name, a binding a mutation cleared, an enum.
			l.in.tag("SB-K1/destructuring")
			return supabaseClient{detail: "a binding " + e.Name + " the caches do not follow (destructured or reassigned)"}
		case at == 0 && !b.ModuleConst:
			return supabaseClient{detail: "a module binding " + e.Name + " that is no const"}
		case b.Value.Kind == "function" || b.Value.Kind == "ambiguous":
			return supabaseClient{detail: e.Name + " is a function"}
		}
		c := l.supabaseClient(file, at, b.Value, depth+1, wrapper)
		if c.proven {
			c.forms = append(c.forms, "const binding")
		}
		return c
	case "member_expression":
		l.in.tag("SB-K1/members")
		return supabaseClient{detail: "a member " + firstLine(e.Text)}
	}
	return supabaseClient{detail: "a " + strings.ReplaceAll(e.Kind, "_", " ")}
}

// clientCall proves a call: a factory call, or a call of a function whose
// every return is a proven client (one level).
func (l *supabaseLinker) clientCall(file string, scope int, e facts.RouteExpr, depth int, wrapper, awaited bool) supabaseClient {
	if e.Object == nil {
		return supabaseClient{detail: "a call"}
	}
	callee := *e.Object
	if callee.Kind == "await_expression" && callee.Object != nil {
		// await f<T>(...): the grammar attaches await to a generic call's
		// callee.
		callee, awaited = *callee.Object, true
	}
	if callee.Kind != "identifier" {
		return supabaseClient{detail: "a call of " + firstLine(callee.Text)}
	}
	b, _, ok := l.binding(file, scope, callee.Name)
	if !ok {
		return supabaseClient{detail: callee.Name + " is not bound in the file"}
	}
	if b.Module != "" {
		if form, ok := supabaseFactories[b.Module][b.Export]; ok {
			return l.factory(b.Module, form, e)
		}
		if l.in.Modules == nil {
			return supabaseClient{detail: "no module resolver"}
		}
		x := l.in.Modules.ImportBinding(file, b.Module, b.Export, "")
		if x.External {
			if form, ok := supabaseFactories[x.File][x.Name]; ok {
				return l.factory(x.File, form, e)
			}
			return supabaseClient{detail: callee.Name + " is imported from the package " + x.File}
		}
		if x.File == "" || x.Namespace || x.Ambiguous {
			return supabaseClient{detail: callee.Name + " resolves to no function of the repository"}
		}
		r := l.in.TypeScript[x.File].Routes
		if r == nil || len(r.Scopes) == 0 {
			return supabaseClient{detail: callee.Name + " names no function of " + x.File}
		}
		fn, ok := r.Scopes[0].Bindings[x.Name]
		if !ok || fn.Module != "" {
			return supabaseClient{detail: callee.Name + " names no function of " + x.File}
		}
		return l.wrapperCall(x.File, fn, callee.Name, depth, wrapper, awaited)
	}
	return l.wrapperCall(file, b, callee.Name, depth, wrapper, awaited)
}

// wrapperCall proves a call of a repository function: one level, every
// return a proven client, awaited when the function is async.
func (l *supabaseLinker) wrapperCall(file string, fn facts.RouteBinding, name string, depth int, wrapper, awaited bool) supabaseClient {
	if fn.Value.Kind != "function" || fn.Mutable {
		return supabaseClient{detail: "a call of " + name + ", which is no function the caches follow"}
	}
	if !wrapper {
		return supabaseClient{detail: "a call of " + name + " inside a wrapper (one level only)"}
	}
	if len(fn.Returns) == 0 {
		return supabaseClient{detail: "a call of " + name + ", whose returns are not all read"}
	}
	if fn.Async && !awaited {
		return supabaseClient{detail: "a call of the async function " + name + " that is not awaited"}
	}
	var out supabaseClient
	for i, ret := range fn.Returns {
		c := l.supabaseClient(file, fn.ReturnScope, ret, depth+1, false)
		if !c.proven {
			return supabaseClient{detail: "a call of " + name + ": a return is no proven client (" + c.detail + ")"}
		}
		if i > 0 && c.schema != out.schema {
			return supabaseClient{detail: "a call of " + name + ": its returns name different schemas"}
		}
		out = c
	}
	out.forms = append(out.forms, "one level of a function")
	return out
}

// factory reads a factory call's options: a db.schema key gives the
// client's schema when it is a literal; an options argument the caches
// cannot read (not an object literal, a spread, a non-literal schema)
// leaves the client unproven, so its sites go to SB-T5.
func (l *supabaseLinker) factory(module, form string, e facts.RouteExpr) supabaseClient {
	c := supabaseClient{proven: true, schema: "public", forms: []string{form, "directly"}}
	options := e.Args
	if module != "@supabase/auth-helpers-nextjs" {
		if len(options) <= 2 {
			return c
		}
		options = options[2:]
	}
	for _, a := range options {
		if a.Kind != "object" || a.Spread || a.Invalid {
			return supabaseClient{detail: "a " + form + " options argument the caches do not read (" + firstLine(a.Text) + ")"}
		}
		db, ok := a.Properties["db"]
		if module == "@supabase/auth-helpers-nextjs" {
			o, has := a.Properties["options"]
			if !has {
				continue
			}
			if o.Kind != "object" || o.Spread || o.Invalid {
				return supabaseClient{detail: "a " + form + " options value the caches do not read"}
			}
			db, ok = o.Properties["db"]
		}
		if !ok {
			continue
		}
		l.in.tag("SB-K1/db.schema")
		if db.Kind != "object" || db.Spread || db.Invalid {
			return supabaseClient{detail: "a db option the caches do not read"}
		}
		s, has := db.Properties["schema"]
		if !has {
			continue
		}
		if s.Kind != "literal" {
			return supabaseClient{detail: "a db.schema option that is no literal (" + firstLine(s.Text) + ")"}
		}
		c.schema = s.Name
	}
	return c
}

// literalName reads a table, schema or function name: a string literal or
// a bounded const (a module-level const bound to a literal, in the file or
// imported from a repository module).
func (l *supabaseLinker) literalName(file string, scope int, e facts.RouteExpr) (string, string, bool) {
	switch e.Kind {
	case "literal":
		return e.Name, "", true
	case "identifier":
		b, at, ok := l.binding(file, scope, e.Name)
		if !ok {
			return "", "", false
		}
		if b.Module != "" {
			if l.in.Modules == nil {
				return "", "", false
			}
			x := l.in.Modules.ImportBinding(file, b.Module, b.Export, "")
			if x.External || x.File == "" || x.Namespace || x.Ambiguous {
				return "", "", false
			}
			r := l.in.TypeScript[x.File].Routes
			if r == nil || len(r.Scopes) == 0 {
				return "", "", false
			}
			v, ok := r.Scopes[0].Bindings[x.Name]
			if ok && v.ModuleConst && v.Constant && !v.Mutable && v.Value.Kind == "literal" {
				return v.Value.Name, "bounded const", true
			}
			return "", "", false
		}
		if at == 0 && b.ModuleConst && b.Constant && !b.Mutable && b.Value.Kind == "literal" {
			return b.Value.Name, "bounded const", true
		}
	}
	return "", "", false
}

// supabaseFile is one TypeScript file's Supabase sites.
type supabaseFile struct {
	l    *supabaseLinker
	file string
	pkg  string
	ok   bool
	sets []*supabaseSet
}

// linkSupabaseFile reads a file's call chains for SB-T1..SB-T5 and D.
func linkSupabaseFile(l *supabaseLinker, file string, f facts.Facts, a *analysis) {
	if f.Routes == nil || len(f.Routes.Clients) == 0 {
		return
	}
	d := supabaseFile{l: l, file: file}
	d.pkg, d.ok = npmPackage(file, l.in.Sources)
	if d.ok {
		d.sets = l.sets[d.pkg]
	}
	receivers := map[chainKey]bool{}
	for _, c := range f.Routes.Clients {
		e := c.Expression
		if e.Kind == "call_expression" && e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil && e.Object.Object.Kind == "call_expression" {
			receivers[keyOf(*e.Object.Object)] = true
		}
	}
	// A query builder bound to a const and used with a verb in the same
	// function (`const q = sb.from('t'); q.select()`, T024 review) is
	// read as the one chain; the binding's own chain then claims nothing.
	type outer struct {
		c     facts.ClientCall
		recv  facts.RouteExpr
		links []chainLink
		bound *constBound
	}
	var chains []outer
	followed := map[chainKey]bool{}
	for _, c := range f.Routes.Clients {
		e := c.Expression
		if e.Kind != "call_expression" || receivers[keyOf(e)] {
			continue
		}
		recv, links := callChain(e)
		o := outer{c: c, recv: recv, links: links}
		if brecv, blinks, value, ok := d.constBuilder(c, recv, links); ok {
			followed[keyOf(value)] = true
			o.recv, o.links = brecv, append(append([]chainLink(nil), blinks...), links...)
			o.bound = &constBound{Name: recv.Name, Text: value.Text}
		}
		chains = append(chains, o)
	}
	for _, o := range chains {
		d.chain(o.c, o.recv, o.links, o.bound, followed[keyOf(o.c.Expression)], a)
	}
}

// constBuilder follows a chain whose receiver is a const binding of a
// builder `<client>.from(...)` (or `.schema(...).from(...)`) without a
// verb, declared in the same function as the use, when the use starts
// with an SB-T1 verb. It returns the builder's receiver and links, and the
// binding's value. A let or var binding, an imported
// one, a parameter, or one declared outside the function is not followed
// (its value may differ at the use).
func (d supabaseFile) constBuilder(c facts.ClientCall, recv facts.RouteExpr, links []chainLink) (facts.RouteExpr, []chainLink, facts.RouteExpr, bool) {
	if recv.Kind != "identifier" || len(links) == 0 {
		return facts.RouteExpr{}, nil, facts.RouteExpr{}, false
	}
	if _, verb := supabaseVerbs[links[0].name]; !verb {
		return facts.RouteExpr{}, nil, facts.RouteExpr{}, false
	}
	b, at, ok := d.l.binding(d.file, c.Scope, recv.Name)
	// As SB-K1's const binding: a let or var is Mutable, and a binding a
	// reassignment cleared has no value.
	if !ok || b.Mutable || b.Module != "" || b.Value.Kind != "call_expression" || b.Value.Invalid || at == 0 && !b.ModuleConst {
		return facts.RouteExpr{}, nil, facts.RouteExpr{}, false
	}
	scopes := d.l.in.TypeScript[d.file].Routes.Scopes
	for s := c.Scope; s != at; s = scopes[s].Parent {
		if s < 0 || s >= len(scopes) || scopes[s].Function || scopes[s].Parent == s {
			return facts.RouteExpr{}, nil, facts.RouteExpr{}, false
		}
	}
	brecv, blinks := callChain(b.Value)
	from := -1
	for i, k := range blinks {
		if k.name == "from" && len(k.args) >= 1 {
			from = i
			break
		}
	}
	if from < 0 || from > 1 || from == 1 && blinks[0].name != "schema" || len(blinks) != from+1 {
		return facts.RouteExpr{}, nil, facts.RouteExpr{}, false
	}
	return brecv, blinks, b.Value, true
}

// chain classifies one outermost call chain.
func (d supabaseFile) chain(c facts.ClientCall, recv facts.RouteExpr, links []chainLink, bound *constBound, followed bool, a *analysis) {
	at := -1
	for i, k := range links {
		if (k.name == "from" || k.name == "rpc") && len(k.args) >= 1 {
			at = i
			break
		}
	}
	if at < 0 || at > 1 || at == 1 && links[0].name != "schema" {
		return
	}
	var schemaLink *chainLink
	if at == 1 {
		schemaLink = &links[0]
	}
	if recv.Kind == "member_expression" && recv.Name == "storage" && schemaLink == nil && links[at].name == "from" {
		// supabase.storage.from(bucket): a storage bucket, no table (D).
		if d.l.supabaseClient(d.file, c.Scope, *recv.Object, 0, true).proven {
			d.l.in.tag("D/.storage.from(bucket)")
		}
		return
	}
	verb := ""
	if links[at].name == "from" && at+1 < len(links) {
		if _, ok := supabaseVerbs[links[at+1].name]; ok {
			verb = links[at+1].name
		}
	}
	client := d.l.supabaseClient(d.file, c.Scope, recv, 0, true)
	if links[at].name == "from" && verb == "" {
		// No verb on the chain: a builder bound to a const that a verb
		// use in its function reads is claimed at that use; any other
		// (returned, passed on, stored where no use is read) is an
		// unknown, so change impact is never silent (T024 review).
		if client.proven && !followed {
			d.supabaseBuilderUnread(c, links[at], a)
		}
		return
	}
	if bound != nil {
		d.l.in.tag("SB-T1/const builder")
	}
	if links[at].name == "rpc" {
		if client.proven {
			d.supabaseDefault(c, client, schemaLink, links[at], a)
		}
		return
	}
	if !client.proven {
		d.supabaseUnprovenClient(c, client, schemaLink, links[at], a)
		return
	}
	for _, f := range client.forms {
		d.l.in.tag("SB-K1/" + f)
	}
	schema, ok := d.supabaseSchema(c, client, schemaLink, a)
	if !ok {
		return
	}
	name, form, ok := d.l.literalName(d.file, c.Scope, links[at].args[0])
	if !ok {
		d.supabaseDynamicTable(c, links[at].args[0], a)
		return
	}
	if form != "" {
		d.l.in.tag("SB-T1/" + form)
	}
	base := d.claimSupabaseTable(c, schema, name, links, at, verb, schemaLink != nil)
	base.receiver, base.clientSchema, base.bound = recv.Text, client.schema, bound
	for _, k := range links[:at+1] {
		if len(k.args) >= 1 && k.args[0].Kind == "identifier" {
			if v, form, ok := d.l.literalName(d.file, c.Scope, k.args[0]); ok && form != "" {
				if base.consts == nil {
					base.consts = map[string]string{}
				}
				base.consts[k.args[0].Name] = v
			}
		}
	}
	a.sites = append(a.sites, base)
	if embeds, ok := d.claimSupabaseEmbeds(c, base, schema, links, at+1); ok {
		a.sites = append(a.sites, embeds)
	}
}

// supabaseBuilderUnread is SB-T1's refusal for a query builder without a
// verb on its chain that no followed use reads: an unknown naming the
// table (or the table expression), candidates the package's tables.
func (d supabaseFile) supabaseBuilderUnread(c facts.ClientCall, from chainLink, a *analysis) {
	d.l.in.tag("SB-T1/builder_unread")
	name := firstLine(from.args[0].Text)
	if v, _, ok := d.l.literalName(d.file, c.Scope, from.args[0]); ok {
		name = v
	}
	d.unknown(c, "SB-T1", ReasonBuilderUnread+": "+name+" (a query builder without a verb; its uses are not read)", d.candidates(), a)
}

// newSite starts a Supabase site of a row.
func (d supabaseFile) newSite(c facts.ClientCall, row string) site {
	return site{file: d.file, span: c.Span, owner: c.Owner, row: row, tool: SupabaseTool, rows: []string{row},
		assumes: []string{"SB-V1"}, role: RoleExecutes, verification: supabaseRow(row).Verification,
		reads: map[string]map[string]bool{}, writes: map[string]map[string]bool{}, tables: map[string]string{}}
}

// unknown records an entry on the call's owner.
func (d supabaseFile) unknown(c facts.ClientCall, row, reason string, candidates []string, a *analysis) {
	a.unknowns[c.Owner] = append(a.unknowns[c.Owner], Unknown{Row: row, Line: c.Span.StartLine, Span: c.Span, Tool: SupabaseTool, Reason: reason, Candidates: candidates})
}

// supabaseSchema is row SB-T2's branch: `<client>.schema('<s>')` sets the
// schema of the chain (a literal or bounded const); a non-literal schema
// is an unknown. Without a schema link the client's default applies.
func (d supabaseFile) supabaseSchema(c facts.ClientCall, client supabaseClient, link *chainLink, a *analysis) (string, bool) {
	if link == nil {
		return client.schema, true
	}
	d.l.in.tag("SB-T2")
	if len(link.args) >= 1 {
		if s, _, ok := d.l.literalName(d.file, c.Scope, link.args[0]); ok {
			return s, true
		}
	}
	d.l.in.tag("SB-T2/non-literal schema")
	text := ""
	if len(link.args) >= 1 {
		text = firstLine(link.args[0].Text)
	}
	d.unknown(c, "SB-T2", ReasonTableDynamic+": schema "+text, d.candidates(), a)
	return "", false
}

// candidates are the identities of every table of the package's Supabase
// sets (nil when it has none: any table).
func (d supabaseFile) candidates() []string {
	var out []string
	for _, s := range d.sets {
		for _, t := range s.source.tables {
			out = append(out, t.identity)
		}
	}
	sort.Strings(out)
	return out
}

// supabaseDynamicTable is row SB-T4's branch: `.from(<non-literal>)` on a
// proven client claims nothing; the set's tables are the candidates.
func (d supabaseFile) supabaseDynamicTable(c facts.ClientCall, arg facts.RouteExpr, a *analysis) {
	d.l.in.tag("SB-T4")
	d.unknown(c, "SB-T4", ReasonTableDynamic+": "+firstLine(arg.Text), d.candidates(), a)
}

// supabaseUnprovenClient is row SB-T5's branch: the chain shape with a
// literal table name on a receiver SB-K1 does not prove, in a package that
// declares a Supabase JS package (so another library's from(...).select()
// stays invisible), is an unknown; never a claim.
func (d supabaseFile) supabaseUnprovenClient(c facts.ClientCall, client supabaseClient, schemaLink *chainLink, from chainLink, a *analysis) {
	if !d.ok || !d.l.packageDeclares(d.pkg) {
		return
	}
	name, _, ok := d.l.literalName(d.file, c.Scope, from.args[0])
	if !ok {
		return
	}
	d.l.in.tag("SB-T5")
	d.unknown(c, "SB-T5", ReasonClientUnproven+": "+name+" ("+client.detail+")", nil, a)
}

// supabaseDefault is the D branch for `.rpc(fn)` on a proven client: the
// function runs statements whose tables are not traced here, so it is a
// statement_not_traced record naming the function; no table is claimed.
func (d supabaseFile) supabaseDefault(c facts.ClientCall, client supabaseClient, schemaLink *chainLink, rpc chainLink, a *analysis) {
	d.l.in.tag("D/.rpc(fn)")
	schema := client.schema
	if schemaLink != nil {
		schema = "?"
		if len(schemaLink.args) >= 1 {
			if s, _, ok := d.l.literalName(d.file, c.Scope, schemaLink.args[0]); ok {
				schema = s
			}
		}
	}
	fn := firstLine(rpc.args[0].Text)
	if name, _, ok := d.l.literalName(d.file, c.Scope, rpc.args[0]); ok {
		fn = name
	}
	d.unknown(c, "D", ReasonStatementNotTraced+": rpc "+schema+"."+fn, nil, a)
}

// claimSupabaseTable is row SB-T1's branch (with SB-T2's schema): the
// chain's verb reads (select) or writes (insert, update, upsert, delete)
// the table `<schema>.<name>` of the package's Supabase set; a later
// .select() on a write returns the written rows and adds no read.
func (d supabaseFile) claimSupabaseTable(c facts.ClientCall, schema, name string, links []chainLink, at int, verb string, schemaLink bool) site {
	s := d.newSite(c, "SB-T1")
	if schemaLink {
		s.rows = []string{"SB-T1", "SB-T2"}
	}
	d.l.in.tag("SB-T1/" + verb)
	op := supabaseVerbs[verb]
	write := verb != "select"
	if verb == "insert" {
		for _, k := range links[at+2:] {
			if k.name == "select" {
				d.l.in.tag("SB-T1/insert(...).select()")
			}
		}
	}
	s.builder = links[len(links)-1].call.Text
	if len(d.sets) == 0 {
		s.unknown = &Unknown{Row: "SB-T1", Line: c.Span.StartLine, Span: c.Span, Tool: SupabaseTool, Reason: ReasonNoSchemaSource + ": " + schema + "." + name}
		return s
	}
	var sources []*migrationSet
	for _, set := range d.sets {
		sources = append(sources, set.source)
		if set.generated {
			// T024c3: the package's schema source is its generated types
			// (SB-G1), so the claim rests on SB-V6 instead of SB-V1.
			d.l.in.tag("SB-T1/generated types")
			s.assumes = []string{"SB-V6"}
		}
	}
	s.sets = sources
	res := resolveNames(Reading{Refs: []Ref{{Schema: schema, Name: name, Write: write, Operation: op}}}, sources)
	s.reads, s.writes = res.reads, res.writes
	s.tables = res.tables
	if len(res.unresolved) > 0 {
		u := res.unresolved[0]
		s.unknown = &Unknown{Row: "SB-T1", Line: c.Span.StartLine, Span: c.Span, Tool: SupabaseTool, Reason: u.reason, Candidates: u.candidates}
	}
	return s
}

// claimSupabaseEmbeds is row SB-T3's branch: the embedded resources of a
// literal select string on the chain (the verb, or a select after a
// write) are read where exactly one foreign key relates each to its parent
// (mapped); every other embedded name is a partial unknown.
func (d supabaseFile) claimSupabaseEmbeds(c facts.ClientCall, base site, schema string, links []chainLink, from int) (site, bool) {
	var arg *facts.RouteExpr
	for i := from; i < len(links); i++ {
		if links[i].name == "select" && len(links[i].args) >= 1 {
			arg = &links[i].args[0]
			break
		}
	}
	if arg == nil {
		return site{}, false
	}
	if arg.Kind != "literal" {
		return d.unreadSelect(c, base, *arg)
	}
	items, ok := parseSelect(arg.Name)
	if !ok || !hasEmbeds(items) {
		return site{}, false
	}
	s := d.newSite(c, "SB-T3")
	var parent string
	for id := range base.reads {
		parent = id
	}
	for id := range base.writes {
		parent = id
	}
	var set *supabaseSet
	for _, x := range d.sets {
		for _, t := range x.source.tables {
			if t.identity == parent {
				set = x
			}
		}
	}
	var unresolved reasonSet
	d.embeds(&s, set, schema, parent, items, 0, &unresolved)
	claimed := len(s.reads) > 0 || len(base.reads)+len(base.writes) > 0
	s.unknown = unresolved.unknown(&s, ReasonEmbeddedResource, claimed)
	if len(s.reads) == 0 && s.unknown == nil {
		return site{}, false
	}
	return s, true
}

// unreadSelect is SB-T3's branch for a select argument that is not a
// literal (accepted contract change, T024c3): the base table keeps its
// claim, and a partial unknown says the embedded tables were not read, with
// the package's other tables as candidates, so change impact lists the
// site under "may also be used by" (never a silent absence).
func (d supabaseFile) unreadSelect(c facts.ClientCall, base site, arg facts.RouteExpr) (site, bool) {
	d.l.in.tag("SB-T3/non-literal select")
	s := d.newSite(c, "SB-T3")
	var candidates []string
	for _, id := range d.candidates() {
		if base.reads[id] == nil && base.writes[id] == nil {
			candidates = append(candidates, id)
		}
	}
	claimed := len(base.reads)+len(base.writes) > 0
	var reasons reasonSet
	reasons.add("embedded tables not read (select argument "+firstLine(arg.Text)+" is not a literal)", candidates...)
	s.unknown = reasons.unknown(&s, ReasonEmbeddedResource, claimed)
	return s, true
}

// embeds resolves one level of embedded resources relative to parent ("" when
// the parent is not a resolved table).
func (d supabaseFile) embeds(s *site, set *supabaseSet, schema, parent string, items []selectItem, depth int, unresolved *reasonSet) {
	for _, it := range items {
		if !it.embed {
			continue
		}
		if depth > 0 {
			d.l.in.tag("SB-T3/nested levels")
		}
		if it.alias != "" {
			d.l.in.tag("SB-T3/a:e")
		}
		if it.join != "" {
			d.l.in.tag("SB-T3/join modifier")
		}
		child := ""
		switch {
		case it.spread:
			d.l.in.tag("SB-T3/...e")
			unresolved.add(it.name + " (a spread)")
		case it.hint:
			d.l.in.tag("SB-T3/!hint")
			unresolved.add(it.name + " (a !hint)")
		case parent == "" || set == nil:
			unresolved.add(it.name + " (the parent table is not resolved)")
		default:
			child = d.relation(s, set, schema, parent, it.name, unresolved)
		}
		d.embeds(s, set, schema, child, it.children, depth+1, unresolved)
	}
}

// relation resolves one embedded name under a resolved parent: a table of
// the set in the request's schema with exactly one foreign key between the
// two (either direction) and no table holding keys to both. It returns the
// embedded table's identity, "" when it claims nothing.
func (d supabaseFile) relation(s *site, set *supabaseSet, schema, parent, name string, unresolved *reasonSet) string {
	target := ""
	for _, t := range set.source.tables {
		ts := t.schema
		if ts == "" {
			if t.pathUnproven {
				continue
			}
			ts = "public"
		}
		if ts == schema && t.name == name {
			target = t.identity
		}
	}
	if target == "" {
		d.l.in.tag("SB-T3/no table of the set")
		unresolved.add(name + " (no table of the set)")
		return ""
	}
	if set.generated {
		d.l.in.tag("SB-T3/relationships not resolved")
		unresolved.add(name+" (relationships not proven: generated types name no referenced schema)", target)
		return ""
	}
	if !set.complete {
		unresolved.add(name+" (relationships not proven: the set has partial tables or opaque statements)", target)
		return ""
	}
	keys := 0
	for _, k := range set.keys {
		if k == [2]string{parent, target} || k == [2]string{target, parent} {
			keys++
		}
	}
	junction := false
	toParent, toTarget := map[string]bool{}, map[string]bool{}
	for _, k := range set.keys {
		if k[1] == parent && k[0] != parent && k[0] != target {
			toParent[k[0]] = true
		}
		if k[1] == target && k[0] != parent && k[0] != target {
			toTarget[k[0]] = true
		}
	}
	for j := range toParent {
		junction = junction || toTarget[j]
	}
	switch {
	case junction:
		d.l.in.tag("SB-T3/junction table")
		unresolved.add(name+" (a junction table joins it to its parent)", target)
		return ""
	case keys != 1 || parent == target:
		d.l.in.tag("SB-T3/zero or several relationships")
		unresolved.add(name+" ("+strconv.Itoa(keys)+" foreign keys to its parent)", target)
		return ""
	}
	d.l.in.tag("SB-T3/one foreign key")
	addOp(s.reads, target, "select")
	s.tables[target] = schema + "." + name
	return target
}

// selectItem is one item of a PostgREST select string.
type selectItem struct {
	name, alias  string
	embed        bool
	spread, hint bool
	// join is a final `!inner` or `!left` join modifier, which does not
	// change the embedded resource.
	join     string
	children []selectItem
}

func hasEmbeds(items []selectItem) bool {
	for _, it := range items {
		if it.embed {
			return true
		}
	}
	return false
}

// parseSelect reads a PostgREST select string as postgrest-js sends it
// (whitespace outside double quotes removed): comma-separated items, an
// embedded resource `[...][alias:]name[!x](items)`. Columns, casts, JSON
// paths and aggregates (`col.sum()`, `count()`) are no embeds. ok is false
// when the string does not parse (unbalanced parentheses or quotes).
func parseSelect(raw string) ([]selectItem, bool) {
	var b strings.Builder
	quoted := false
	for _, r := range raw {
		if r == '"' {
			quoted = !quoted
		}
		if !quoted && (r == ' ' || r == '\n' || r == '\t' || r == '\r') {
			continue
		}
		b.WriteRune(r)
	}
	if quoted {
		return nil, false
	}
	items, rest, ok := parseItems(b.String())
	return items, ok && rest == ""
}

// parseItems reads items up to an unmatched ')' and returns the rest.
func parseItems(s string) ([]selectItem, string, bool) {
	var items []selectItem
	for {
		head, rest, ok := itemHead(s)
		if !ok {
			return nil, "", false
		}
		if head == "" && len(items) == 0 && strings.HasPrefix(rest, ")") {
			// Empty parentheses: count(), e().
			return nil, rest, true
		}
		it := selectItem{}
		if strings.HasPrefix(rest, "(") {
			children, after, ok := parseItems(rest[1:])
			if !ok || !strings.HasPrefix(after, ")") {
				return nil, "", false
			}
			rest = after[1:]
			it = embedItem(head, children)
		} else {
			it.name = head
		}
		items = append(items, it)
		if strings.HasPrefix(rest, ",") {
			s = rest[1:]
			continue
		}
		return items, rest, true
	}
}

// itemHead reads an item up to '(', ',' or ')' outside double quotes.
func itemHead(s string) (string, string, bool) {
	quoted := false
	for i, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case !quoted && (r == '(' || r == ',' || r == ')'):
			return s[:i], s[i:], true
		}
	}
	if quoted {
		return "", "", false
	}
	return s, "", true
}

// embedItem reads the head of an item followed by parentheses: an embed,
// or an aggregate (`col.sum()`, `count()`), which embeds nothing.
func embedItem(head string, children []selectItem) selectItem {
	it := selectItem{children: children}
	h := head
	if strings.HasPrefix(h, "...") {
		it.spread = true
		h = h[3:]
	}
	if i := aliasColon(h); i >= 0 {
		it.alias, h = h[:i], h[i+1:]
	}
	if i := strings.IndexByte(h, '!'); i >= 0 {
		// PostgREST reads a final `!inner` or `!left` as the join type
		// (T024 review): the resource resolves as without it. Any other
		// name after `!` (also before such a modifier) is a hint.
		modifiers := strings.Split(h[i+1:], "!")
		if last := modifiers[len(modifiers)-1]; last == "inner" || last == "left" {
			modifiers = modifiers[:len(modifiers)-1]
			it.join = last
		}
		it.hint = len(modifiers) > 0
		h = h[:i]
	}
	if !strings.HasPrefix(h, `"`) && (strings.Contains(h, ".") || h == "count" && len(children) == 0 && !it.spread) {
		// An aggregate: col.sum(), count().
		return selectItem{name: head}
	}
	it.name = strings.Trim(h, `"`)
	it.embed = true
	return it
}

// aliasColon returns the index of an alias separator (a ':' that is not
// part of a '::' cast) outside double quotes, -1 when there is none.
func aliasColon(s string) int {
	quoted := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '"':
			quoted = !quoted
		case !quoted && s[i] == ':':
			if i+1 < len(s) && s[i+1] == ':' {
				return -1
			}
			return i
		}
	}
	return -1
}

// supabaseRow returns the Supabase contract row with the id.
func supabaseRow(id string) Row {
	for _, r := range SupabaseContract {
		if r.ID == id {
			return r
		}
	}
	return Row{ID: id}
}

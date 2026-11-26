package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file links the bridge rows of docs/lang/typescript-bridges.md (T026,
// decision O4 (b)): `renders` (a JSX element whose tag resolves
// to a repository function or class), `passes_function` (a repository
// function passed as a value to a call or a JSX attribute, the fact that it
// is passed, not that it is called) and react-router route objects
// (`ui_route` nodes with `routes_to` and `passes_function` edges). Names
// resolve like calls do: the lexical scope chain of the file's declarations,
// then the Precise import graph (ADR 0070). bridgeRows lists the rows;
// TestTypeScriptBridgesContract keeps them 1:1 with the document.

// bridgeRow is one row of the bridge claim contract.
type bridgeRow struct {
	ID, Claim string
}

// bridgeRows are the rows in document order; "D" is the single default
// branch: every form no row reads claims nothing.
var bridgeRows = []bridgeRow{
	{"JR1", "renders: tag resolves to a function or class of the same file"},
	{"JR2", "renders: tag resolves through the import graph to a function or class of another file"},
	{"JR3", "renders: tag is a module-level React.lazy binding; the imported module's default export"},
	{"PF1", "passes_function: a call argument that resolves to a function or method"},
	{"PF2", "passes_function: an object property value or array element of an argument literal"},
	{"PF3", "passes_function: a JSX attribute value that resolves to a function or method"},
	{"RR1", "ui_route: an object route reached from a proven router factory call"},
	{"RR2", "ui_route: a <Route> element whose tag is react-router's Route"},
	{"RR3", "routes_to: a route's static Component or element tags"},
	{"RR4", "passes_function: a route's static loader or action"},
	{"RR5", "routes_to / passes_function: Component, loader or action supplied by the route's lazy function"},
	{"D", "nothing"},
}

// bridgeTagHook is a test-only hook every branch reports its row to.
var bridgeTagHook func(row string)

func bridgeTag(row string) {
	if bridgeTagHook != nil {
		bridgeTagHook(row)
	}
}

// reactRouterPackages are the packages whose exports the route rows read
// (react-router-dom 7 re-exports react-router).
var reactRouterPackages = map[string]bool{"react-router": true, "react-router-dom": true}

// bridgeFile is one file's resolution context.
type bridgeFile struct {
	file      string
	f         Facts
	syn       *facts.BridgeSyntax
	functions map[string]string
	classes   map[string]string
	bindings  map[string]ExportBinding
	// packages maps a local import name to its package and exported name
	// when the import resolves to a package outside the repository.
	packages map[string][2]string
	lazy     map[string]facts.LazyComponent
}

// bridgeLinker holds the shared state of one link.
type bridgeLinker struct {
	files     map[string]*bridgeFile
	resolver  *ModuleResolver
	ambiguous map[string]bool
	withheld  map[string]string
	edges     []contract.Edge
	nodes     []contract.Node
}

// LinkBridges applies the bridge rows to the current inventory. It
// returns the react-router `ui_route` nodes and the `renders`,
// `passes_function`, `routes_to` and `contains` edges; files row TS-F2
// refuses are neither sources nor targets.
func LinkBridges(files map[string]Facts, resolver *ModuleResolver) ([]contract.Node, []contract.Edge) {
	l := &bridgeLinker{files: map[string]*bridgeFile{}, resolver: resolver, ambiguous: map[string]bool{}, withheld: RejectedOwners(files)}
	for _, f := range files {
		for _, identities := range f.Repeated {
			for _, identity := range identities {
				l.ambiguous[identity] = true
			}
		}
	}
	paths := make([]string, 0, len(files))
	for p, f := range files {
		if !Rejected(f) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		l.files[p] = l.context(p, files[p])
	}
	for _, p := range paths {
		bf := l.files[p]
		if bf.syn == nil {
			continue
		}
		for _, s := range bf.syn.Sites {
			l.site(bf, s)
		}
	}
	l.routes(paths)
	merged := map[string]contract.Edge{}
	for _, e := range l.edges {
		key := e.Kind + "\x00" + e.Source + "\x00" + e.Target
		if old, ok := merged[key]; ok {
			old.Evidence = append(old.Evidence, e.Evidence...)
			merged[key] = old
		} else {
			merged[key] = e
		}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	edges := make([]contract.Edge, 0, len(keys))
	for _, key := range keys {
		edges = append(edges, merged[key])
	}
	sort.Slice(l.nodes, func(i, j int) bool { return l.nodes[i].Identity < l.nodes[j].Identity })
	return l.nodes, edges
}

// context builds a file's declarations and import bindings.
func (l *bridgeLinker) context(file string, f Facts) *bridgeFile {
	bf := &bridgeFile{file: file, f: f, functions: map[string]string{}, classes: map[string]string{}, bindings: map[string]ExportBinding{}, packages: map[string][2]string{}, lazy: map[string]facts.LazyComponent{}}
	if f.TypeScript != nil {
		bf.syn = f.TypeScript.Bridges
	}
	for _, n := range f.Nodes {
		switch n.Kind {
		case "function", "method":
			bf.functions[n.QualifiedName] = n.Identity
		case "class":
			bf.classes[n.QualifiedName] = n.Identity
		}
	}
	for _, imp := range f.Imports {
		if imp.Module == "" || strings.HasPrefix(imp.Module, "/") {
			continue
		}
		resolution := l.resolver.Resolve(file, imp.Module)
		for local, name := range imp.Bindings {
			switch {
			case resolution.Refusal != "":
			case resolution.File != "":
				bf.bindings[local] = l.resolver.ImportBinding(file, imp.Module, name, "")
			case resolution.Package():
				bf.packages[local] = [2]string{packageName(imp.Module), name}
			}
		}
	}
	if bf.syn != nil {
		for _, z := range bf.syn.Lazy {
			bf.lazy[z.Name] = z
		}
	}
	return bf
}

// resolve returns the identity a name denotes at a site and the row that
// proves it ("" when nothing does): the scope chain of the file's
// declarations of the wanted kinds, else an import binding.
func (l *bridgeLinker) resolve(bf *bridgeFile, s facts.BridgeSite, kinds map[string]bool, lazy bool) (string, string) {
	if s.Shadowed || s.Name == "" {
		return "", ""
	}
	local := func(q string) string {
		if kinds["function"] || kinds["method"] {
			if v := bf.functions[q]; v != "" {
				return v
			}
		}
		if kinds["class"] {
			if v := bf.classes[q]; v != "" {
				return v
			}
		}
		return ""
	}
	name := s.Name
	target, row := "", ""
	if strings.HasPrefix(name, "this.") {
		if i := strings.LastIndex(s.Scope, "."); i >= 0 {
			target = local(s.Scope[:i] + strings.TrimPrefix(name, "this"))
		}
		if target == "" {
			return "", ""
		}
		row = "same-file"
	}
	for scope := s.Scope; target == ""; {
		target = local(join(scope, name))
		if target != "" {
			row = "same-file"
			break
		}
		// Any declaration of the name (a variable, an interface) at this
		// level hides outer ones only through bindings; keep walking like
		// the calls scope does.
		if scope == "" {
			break
		}
		if i := strings.LastIndex(scope, "."); i >= 0 {
			scope = scope[:i]
		} else {
			scope = ""
		}
	}
	if target == "" && lazy && !strings.Contains(name, ".") {
		if z, ok := bf.lazy[name]; ok {
			if l.reactLazy(bf, z.Callee) {
				if v := l.resolver.Resolve(bf.file, z.Module); v.File != "" {
					target, row = l.resolver.Export(v.File, "default").Identity, "lazy"
				}
			}
			if target == "" {
				return "", ""
			}
		}
	}
	if target == "" {
		base, member, _ := strings.Cut(name, ".")
		b := bf.bindings[base]
		if member != "" {
			b = l.resolver.Member(b, member)
		}
		if b.Ambiguous {
			return "", ""
		}
		target, row = b.Identity, "import"
	}
	if target == "" || l.ambiguous[target] {
		return "", ""
	}
	if _, ok := l.withheld[target]; ok {
		return "", ""
	}
	kind, _, _, err := contract.ParseIdentity(target)
	if err != nil || !kinds[kind] {
		return "", ""
	}
	return target, row
}

// reactLazy proves a lazy binding's callee is React's lazy: `lazy`
// imported from react, or `React.lazy` through react's default or
// namespace import.
func (l *bridgeLinker) reactLazy(bf *bridgeFile, callee facts.BridgeSite) bool {
	if callee.Shadowed {
		return false
	}
	base, member, _ := strings.Cut(callee.Name, ".")
	p, ok := bf.packages[base]
	if !ok || p[0] != "react" {
		return false
	}
	if member == "" {
		return p[1] == "lazy"
	}
	return member == "lazy" && (p[1] == "default" || p[1] == "*")
}

// fromPackage proves a name denotes the export of one of the packages.
func fromPackage(bf *bridgeFile, s facts.BridgeSite, packages map[string]bool, exports map[string]bool) bool {
	if s.Shadowed {
		return false
	}
	base, member, _ := strings.Cut(s.Name, ".")
	p, ok := bf.packages[base]
	if !ok || !packages[p[0]] {
		return false
	}
	if member == "" {
		return exports[p[1]]
	}
	return p[1] == "*" && exports[member]
}

var (
	renderKinds   = map[string]bool{"function": true, "class": true}
	functionKinds = map[string]bool{"function": true, "method": true}
)

// source is a site's enclosing function or method, else the file.
func (bf *bridgeFile) source(s facts.BridgeSite) string {
	if s.Source != "" {
		return s.Source
	}
	return id("file", bf.file, bf.file)
}

// site applies rows JR1-JR3 and PF1-PF3 to one site.
func (l *bridgeLinker) site(bf *bridgeFile, s facts.BridgeSite) {
	if s.Row == "jsx" {
		target, how := l.resolve(bf, s, renderKinds, true)
		switch how {
		case "same-file":
			bridgeTag("JR1")
		case "import":
			bridgeTag("JR2")
		case "lazy":
			bridgeTag("JR3")
		default:
			bridgeTag("D")
			return
		}
		l.edges = append(l.edges, facts.Edge("renders", bf.source(s), target, bf.file, s.Span))
		return
	}
	target, _ := l.resolve(bf, s, functionKinds, false)
	if target == "" {
		bridgeTag("D")
		return
	}
	switch s.Row {
	case "argument":
		bridgeTag("PF1")
	case "property":
		bridgeTag("PF2")
	case "attribute":
		bridgeTag("PF3")
	}
	l.edges = append(l.edges, facts.Edge("passes_function", bf.source(s), target, bf.file, s.Span))
}

// routeRef is a reached route candidate.
type routeRef struct {
	file  string
	index int
}

// routes applies rows RR1-RR5: candidates reached from proven router
// factory calls and proven <Route> elements become ui_route nodes.
func (l *bridgeLinker) routes(paths []string) {
	reached := map[routeRef]bool{}
	var queue []routeRef
	reach := func(file string, index int) {
		r := routeRef{file, index}
		if !reached[r] {
			reached[r] = true
			queue = append(queue, r)
		}
	}
	var value func(bf *bridgeFile, v facts.RouteValue, seen map[string]bool)
	var item func(bf *bridgeFile, it facts.RouteItem, seen map[string]bool)
	item = func(bf *bridgeFile, it facts.RouteItem, seen map[string]bool) {
		switch {
		case it.Candidate >= 0:
			reach(bf.file, it.Candidate)
		case it.Ref != nil:
			target, v, ok := l.constValue(bf, *it.Ref)
			if !ok {
				bridgeTag("D")
				return
			}
			key := target.file + "\x00" + it.Ref.Name
			if seen[key] {
				return
			}
			seen[key] = true
			if it.Spread && v.Candidate >= 0 {
				// A spread of an object is not a route item.
				bridgeTag("D")
				return
			}
			value(target, v, seen)
		default:
			bridgeTag("D")
		}
	}
	value = func(bf *bridgeFile, v facts.RouteValue, seen map[string]bool) {
		switch {
		case v.Candidate >= 0:
			reach(bf.file, v.Candidate)
		case v.Other:
			bridgeTag("D")
		default:
			for _, it := range v.Items {
				item(bf, it, seen)
			}
		}
	}
	for _, p := range paths {
		bf := l.files[p]
		if bf.syn == nil {
			continue
		}
		for _, c := range bf.syn.RouterCalls {
			if !fromPackage(bf, c.Callee, reactRouterPackages, routerFactories) {
				bridgeTag("D")
				continue
			}
			value(bf, c.Routes, map[string]bool{})
		}
		for i, r := range bf.syn.Routes {
			if r.Tag.Name == "" {
				continue
			}
			if fromPackage(bf, r.Tag, reactRouterPackages, map[string]bool{"Route": true}) {
				reach(p, i)
			} else {
				bridgeTag("D")
			}
		}
	}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		bf := l.files[r.file]
		for _, it := range bf.syn.Routes[r.index].Children {
			item(bf, it, map[string]bool{})
		}
	}
	refs := make([]routeRef, 0, len(reached))
	for r := range reached {
		refs = append(refs, r)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].file != refs[j].file {
			return refs[i].file < refs[j].file
		}
		return refs[i].index < refs[j].index
	})
	for _, r := range refs {
		bf := l.files[r.file]
		c := bf.syn.Routes[r.index]
		// A child of a proven <Route> is only a route when its own tag is
		// proven; an object literal is reached from a proven position.
		if c.Tag.Name != "" {
			bridgeTag("RR2")
		} else {
			bridgeTag("RR1")
		}
		l.route(bf, c, reached)
	}
}

// constValue resolves a route position's name to a module-level const
// route value: in the same file, or through an import binding.
func (l *bridgeLinker) constValue(bf *bridgeFile, s facts.BridgeSite) (*bridgeFile, facts.RouteValue, bool) {
	if s.Shadowed {
		return nil, facts.RouteValue{}, false
	}
	if !strings.Contains(s.Name, ".") && bf.syn != nil {
		if v, ok := bf.syn.Consts[s.Name]; ok {
			return bf, v, true
		}
	}
	base, member, _ := strings.Cut(s.Name, ".")
	b, ok := bf.bindings[base]
	if !ok {
		return nil, facts.RouteValue{}, false
	}
	if member != "" {
		b = l.resolver.Member(b, member)
	}
	if b.Ambiguous || b.Identity == "" {
		return nil, facts.RouteValue{}, false
	}
	target := l.files[b.File]
	if target == nil || target.syn == nil {
		return nil, facts.RouteValue{}, false
	}
	v, ok := target.syn.Consts[b.Name]
	return target, v, ok
}

// route emits one ui_route node with its contains edge and rows RR3-RR5.
func (l *bridgeLinker) route(bf *bridgeFile, c facts.RouteCandidate, reached map[routeRef]bool) {
	identity := id("ui_route", bf.file, c.Qualified)
	attrs := map[string]any{"framework": "react-router"}
	switch c.PathForm {
	case "literal":
		attrs["path"] = c.Path
	case "dynamic":
		attrs["path"] = nil
	case "index":
		attrs["index"] = true
	}
	span := c.Span
	present := map[string]bool{}
	for _, k := range c.Present {
		present[k] = true
	}
	owner := c.Owner
	if c.Parent >= 0 && !reached[routeRef{bf.file, c.Parent}] {
		// The literal parent is not a proven route: own the child from
		// the nearest declaration instead.
		owner = id("file", bf.file, bf.file)
	}
	l.edges = append(l.edges, facts.Edge("contains", owner, identity, bf.file, span))
	var lazy *facts.RouteLazy
	if c.Lazy != nil {
		lazy = c.Lazy
		attrs["lazy"] = lazy.Form
	}
	if c.Open {
		// A spread or computed key may supply or overwrite any key: the
		// route's targets are unknown (D).
		attrs["open"] = true
		bridgeTag("D")
		l.node(bf, identity, c, attrs)
		return
	}
	targets := map[string][]string{}
	link := func(row, kind, key, target string, evidence contract.Span) {
		bridgeTag(row)
		targets[key] = append(targets[key], target)
		l.edges = append(l.edges, facts.Edge(kind, identity, target, bf.file, evidence))
	}
	// RR3: a static Component.
	componentFromLazy := false
	if present["Component"] {
		if s, ok := c.Keys["Component"]; ok {
			if target, _ := l.resolve(bf, s, renderKinds, true); target != "" {
				link("RR3", "routes_to", "component", target, s.Span)
			} else {
				bridgeTag("D")
			}
		} else {
			bridgeTag("D")
		}
	}
	// RR5: lazy keys apply only where the route has no static value
	// (react-router loadLazyRoute: a statically defined property wins).
	if lazy != nil {
		for _, key := range []string{"Component", "loader", "action"} {
			if present[key] {
				continue
			}
			target, ok := l.lazyTarget(bf, lazy, key)
			if key == "Component" && (ok || lazy.Form != "object") {
				// The lazy function may supply a Component, which replaces
				// any element (mapRouteProperties).
				componentFromLazy = true
			}
			if !ok {
				continue
			}
			if target == "" {
				bridgeTag("D")
				continue
			}
			kind, attr := "passes_function", key
			if key == "Component" {
				kind, attr = "routes_to", "component"
			}
			link("RR5", kind, attr, target, lazy.Span)
		}
	}
	// RR3: the element's tags, unless a Component replaces it.
	if present["element"] {
		switch {
		case present["Component"] || componentFromLazy || c.ElementOther:
			bridgeTag("D")
		default:
			for _, s := range c.Element {
				if target, _ := l.resolve(bf, s, renderKinds, true); target != "" {
					link("RR3", "routes_to", "element", target, s.Span)
				} else {
					bridgeTag("D")
				}
			}
		}
	}
	// RR4: a static loader or action.
	for _, key := range []string{"loader", "action"} {
		if !present[key] {
			continue
		}
		s, ok := c.Keys[key]
		if !ok {
			bridgeTag("D")
			continue
		}
		if target, _ := l.resolve(bf, s, functionKinds, false); target != "" {
			link("RR4", "passes_function", key, target, s.Span)
		} else {
			bridgeTag("D")
		}
	}
	for _, key := range []string{"component", "element", "loader", "action"} {
		if len(targets[key]) > 0 {
			attrs[key] = distinctSorted(targets[key])
		}
	}
	l.node(bf, identity, c, attrs)
}

func (l *bridgeLinker) node(bf *bridgeFile, identity string, c facts.RouteCandidate, attrs map[string]any) {
	span := c.Span
	l.nodes = append(l.nodes, contract.Node{Type: "node", Identity: identity, Kind: "ui_route", Level: 4, Name: c.Display, QualifiedName: c.Qualified, File: bf.file, Span: &span, ContentHash: c.ContentHash, Attributes: attrs})
}

// lazyTarget returns the target a lazy function supplies for key: ok is
// false when the form does not supply the key; target is "" when it may
// but the target is not proven.
func (l *bridgeLinker) lazyTarget(bf *bridgeFile, lazy *facts.RouteLazy, key string) (string, bool) {
	kinds := functionKinds
	if key == "Component" {
		kinds = renderKinds
	}
	module, export := "", key
	switch lazy.Form {
	case "module":
		module = lazy.Module
	case "object":
		imp, ok := lazy.Props[key]
		if !ok {
			return "", false
		}
		if imp.Unknown {
			return "", true
		}
		module, export = imp.Module, imp.Export
	default:
		return "", true
	}
	v := l.resolver.Resolve(bf.file, module)
	if v.File == "" {
		return "", true
	}
	b := l.resolver.Export(v.File, export)
	if b.Identity == "" || b.Ambiguous || l.ambiguous[b.Identity] {
		if lazy.Form == "module" {
			// The module may not export this key at all.
			return "", false
		}
		return "", true
	}
	if _, ok := l.withheld[b.Identity]; ok {
		return "", true
	}
	kind, _, _, err := contract.ParseIdentity(b.Identity)
	if err != nil || !kinds[kind] {
		return "", true
	}
	return b.Identity, true
}

func distinctSorted(list []string) []string {
	sort.Strings(list)
	out := list[:0]
	for i, v := range list {
		if i == 0 || v != list[i-1] {
			out = append(out, v)
		}
	}
	return out
}

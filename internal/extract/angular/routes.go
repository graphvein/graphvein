package angular

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// routeTuple records that an NgModule's imports flatten to an external call
// leaf at a span. Router registrations are matched to it by file and full span.
// Tuples live for one relink only; they are never serialized or cached.
type routeTuple struct {
	module, file string
	span         contract.Span
}

// routeContext is the lexical context of a route value: where it physically
// lives decides which scope and source shadows resolve its identifiers.
type routeContext struct {
	file    string
	scope   int
	shadows []string
}

// registration is one proven router registration call (ADR 0036).
type registration struct {
	call  typescript.RouterCall
	file  string
	ctx   routeContext
	root  bool     // a root provideRouter call (prefix "")
	owner []string // NgModules whose providers include this registration
	// appTest is set for a production provideRouter that only test-file
	// application bootstraps reach: its chains are test-scope.
	appTest bool
}

// routeKey is one own key of a route object literal. A key that a later
// spread, computed key, method or accessor could overwrite is unknown.
type routeKey struct {
	entry  typescript.DeclarationProperty
	known  bool
	method bool // a method or accessor: present, never evaluated
}

// routeObject is one reached route object literal. Its name, owner and
// context come from where it physically lives; paths accumulate per chain.
type routeObject struct {
	file    string
	expr    expression
	ctx     routeContext
	parent  *routeObject
	keys    map[string]routeKey
	partial bool
	// open is set when a spread, computed key or unnamed method may supply
	// any key: absent keys are then unknown, not absent (A3).
	open     bool
	segment  string
	base     string // qualified name before @N suffixes
	identity string
	owner    string
	paths    map[string]bool // composed path -> reached by a production chain
	lazy     bool            // reached inside a lazily loaded routes array
	eager    bool            // reached outside any lazily loaded routes array
	children *routeLoad      // loadChildren classification, computed once
	codes    []string        // R6 codes, computed once
	checked  bool
}

// routeLoad is a classified loader or component target.
type routeLoad struct {
	status, target, reason string
	span                   contract.Span
	module                 string      // lazy NgModule identity
	array                  *expression // loaded routes array
	arrayFile, arrayKey    string      // file and file+name key of the array
	// lazyOnly marks a non-standalone component that is linked because an
	// eager reach allows it, while a lazy reach still rejects it.
	lazyOnly bool
}

// routeChain carries one composition chain: the prefix (nil once composition
// stops or when the root is unproven), its test scope and the lazy cycle guard.
type routeChain struct {
	prefix *string
	test   bool
	lazy   bool
	stack  []string
}

// routeWalk is one traversal pass over all registrations.
type routeWalk struct {
	l            *linker
	r            *router
	objects      map[string]*routeObject
	order        []*routeObject
	memo         map[string]bool
	visited      map[*registration]bool
	lazyAttached map[*registration]bool
	modules      map[string][]*registration
	reported     map[string]bool
	diagnostics  []contract.Diagnostic
}

var guardKeys = []struct{ key, attr string }{
	{"canActivate", "can_activate"},
	{"canMatch", "can_match"},
	{"canActivateChild", "can_activate_child"},
	{"canDeactivate", "can_deactivate"},
	{"canLoad", "can_load"},
}

// routes emits ui_route nodes, routing edges and diagnostics from the cached
// router payloads. It runs after declarations so roles, standalone values and
// ordered NgModule imports are final.
func (l *linker) routes(routers map[string]typescript.RouterSyntax, injections map[string]typescript.InjectionSyntax, templatesSyntax map[string]typescript.TemplateSyntax) []contract.Node {
	arrays := map[string]expression{}
	for file, r := range routers {
		for _, a := range r.Arrays {
			arrays[file+"\x00"+a.Name] = a.Value
		}
	}
	typeOnly := map[string]bool{}
	for file, s := range injections {
		for _, name := range s.TypeImports {
			typeOnly[file+"\x00"+name] = true
		}
	}
	loaders := map[string]typescript.RouterLoader{}
	for file, r := range routers {
		for _, loader := range r.Loaders {
			loaders[file+"\x00"+spanKey(loader.ValueSpan)] = loader
		}
	}
	decorated := map[string]bool{}
	for _, t := range templatesSyntax {
		for _, d := range t.Decorators {
			decorated[d.Class] = true
		}
	}
	configs := map[string]expression{}
	methods := map[string]string{}
	for file, r := range routers {
		for _, c := range r.Configs {
			configs[file+"\x00"+c.Name] = c.Value
		}
		for _, m := range r.Methods {
			methods[fmt.Sprintf("%s\x00%s\x00%d", file, spanKey(m.Object), m.Index)] = m.Key
		}
	}
	r := &router{l: l, arrays: arrays, typeOnly: typeOnly, loaders: loaders, decorated: decorated, configs: configs, methods: methods, routers: routers}
	r.app = r.applications(routers)
	regs, bootstraps := r.registrations(routers, templatesSyntax)

	// Pass 1 reaches every route object from every proven registration. It
	// decides which provideRouter calls sit inside a reached route's providers
	// (unproven, not roots) and which registrations a lazy closure attaches.
	first := r.walk(regs)
	for _, reg := range regs {
		first.registration(reg, routeChain{})
	}
	for _, obj := range first.order {
		for _, e := range obj.expr.Entries {
			if e.Key != "providers" || e.Kind != "pair" {
				continue
			}
			for _, reg := range regs {
				if reg.root && reg.file == obj.file && spanContains(e.Value.Span, reg.call.Span) {
					reg.root = false
				}
			}
		}
	}

	// Pass 2 composes paths from roots, then emits unproven registrations.
	w := r.walk(regs)
	for _, reg := range regs {
		if reg.root {
			empty := ""
			w.registration(reg, routeChain{prefix: &empty, test: r.test(reg.file) || reg.appTest})
		}
	}
	for _, b := range bootstraps {
		for _, m := range r.closure(b.module) {
			for _, reg := range w.modules[m] {
				empty := ""
				w.registration(reg, routeChain{prefix: &empty, test: r.test(b.file) || r.test(reg.file)})
			}
		}
	}
	unproven := func(reg *registration) {
		if w.registration(reg, routeChain{}) > 0 {
			w.diagnostic(reg.file, reg.call.Span, "angular_route_root_unproven", "info", "registration is not reached from a proven root; paths are not composed")
		}
	}
	for _, reg := range regs {
		if !w.visited[reg] && !first.lazyAttached[reg] {
			unproven(reg)
		}
	}
	for _, reg := range regs {
		if !w.visited[reg] {
			unproven(reg)
		}
	}
	nodes := r.emit(w)
	l.diagnostics = append(l.diagnostics, w.diagnostics...)
	return nodes
}

// router holds per-relink lookups shared by both walks.
type router struct {
	l        *linker
	arrays   map[string]expression
	typeOnly map[string]bool
	loaders  map[string]typescript.RouterLoader
	// decorated holds classes with any captured decorator, proven or not.
	decorated map[string]bool
	// configs holds module const objects with an own providers key.
	configs map[string]expression
	// methods names method entries by file, object span and entry index.
	methods map[string]string
	// app holds provideRouter call sites (file and span) proven inside the
	// providers of an application bootstrap; the value is true when a
	// production bootstrap reaches it.
	app     map[string]bool
	routers map[string]typescript.RouterSyntax
	// names holds per-file @N suffixes by object span, computed on demand.
	names map[string]map[string]int
}

type bootstrap struct {
	file, module string
}

func (r *router) walk(regs []*registration) *routeWalk {
	w := &routeWalk{l: r.l, r: r, objects: map[string]*routeObject{}, memo: map[string]bool{}, visited: map[*registration]bool{}, lazyAttached: map[*registration]bool{}, modules: map[string][]*registration{}, reported: map[string]bool{}}
	for _, reg := range regs {
		for _, m := range reg.owner {
			w.modules[m] = append(w.modules[m], reg)
		}
	}
	return w
}

func (r *router) test(file string) bool {
	return testscope.Classify(file, r.l.rules) == testscope.Test
}

// registrations proves registration and bootstrap calls. Calls whose callee,
// receiver or module identity cannot be proven are ignored, never guessed.
func (r *router) registrations(routers map[string]typescript.RouterSyntax, templatesSyntax map[string]typescript.TemplateSyntax) ([]*registration, []bootstrap) {
	l := r.l
	tuples := map[string][]string{}
	for _, t := range l.routeTuples {
		key := t.file + "\x00" + spanKey(t.span)
		tuples[key] = appendUnique(tuples[key], t.module)
	}
	var regs []*registration
	var boots []bootstrap
	files := []string{}
	for file := range routers {
		if _, ok := l.files[file]; ok {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	for _, file := range files {
		for _, c := range routers[file].Calls {
			ctx := routeContext{file: file, scope: c.Scope, shadows: c.Shadows}
			switch c.Kind {
			case "provide_router":
				b, reason := r.resolve(ctx, c.Callee)
				if reason != "" || b.Ambiguous || !b.External || b.File != "@angular/router" || b.Name != "provideRouter" {
					continue
				}
				reg := &registration{call: c, file: file, ctx: ctx, root: true}
				for _, d := range templatesSyntax[file].Decorators {
					if !spanContains(d.Span, c.Span) {
						continue
					}
					reg.root = false
					if m := l.classes[d.Class]; m != nil && m.role == "ng_module" && m.active {
						reg.owner = appendUnique(reg.owner, d.Class)
					}
				}
				if reg.root && !r.test(file) {
					// Revision 1: a production call is a root only when it is
					// proven inside the providers of an application bootstrap.
					production, ok := r.app[file+"\x00"+spanKey(c.Span)]
					reg.root, reg.appTest = ok, ok && !production
				}
				regs = append(regs, reg)
			case "for_root", "for_child":
				if c.Callee.Kind != "member_expression" || c.Callee.Object == nil {
					continue
				}
				b, reason := r.resolve(ctx, *c.Callee.Object)
				if reason != "" || b.Ambiguous || !b.External || b.File != "@angular/router" || b.Name != "RouterModule" {
					continue
				}
				owners := tuples[file+"\x00"+spanKey(c.Span)]
				if len(owners) == 0 {
					// A proven call that no NgModule imports registers nothing.
					continue
				}
				regs = append(regs, &registration{call: c, file: file, ctx: ctx, owner: owners})
			case "bootstrap_module":
				if c.Receiver == nil || c.Receiver.Kind != "call_expression" || c.ReceiverCallee == nil || c.First == nil || unwrapRoute(*c.First).Kind != "identifier" {
					continue
				}
				p, reason := r.resolve(ctx, *c.ReceiverCallee)
				platform := p.External && !p.Ambiguous && (p.File == "@angular/platform-browser-dynamic" && p.Name == "platformBrowserDynamic" || p.File == "@angular/platform-browser" && p.Name == "platformBrowser")
				if reason != "" || !platform {
					continue
				}
				m, reason := r.resolve(ctx, unwrapRoute(*c.First).Value)
				if reason != "" || m.Ambiguous || m.Identity == "" {
					continue
				}
				if d := l.classes[m.Identity]; d != nil && d.role == "ng_module" && d.active {
					boots = append(boots, bootstrap{file: file, module: m.Identity})
				}
			}
		}
	}
	return regs, boots
}

// closure is X plus the transitive local NgModule targets of its ordered
// import and export leaves: an NgModule injector imports its exports too
// (JIT core/src/render3/jit/module.ts; AOT ng_module handler injectorImports).
func (r *router) closure(module string) []string {
	out := []string{}
	seen := map[string]bool{}
	var visit func(string)
	visit = func(m string) {
		if seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
		d := r.l.classes[m]
		if d == nil {
			return
		}
		for _, list := range [][]string{d.orderedImports, d.orderedExports} {
			for _, target := range list {
				if t := r.l.classes[target]; t != nil && t.role == "ng_module" {
					visit(target)
				}
			}
		}
	}
	visit(module)
	return out
}

// resolve proves an identifier or member chain in a route context. A root
// name in the context's source shadows or imported type-only is unresolved.
func (r *router) resolve(ctx routeContext, e facts.RouteExpr) (typescript.ExportBinding, string) {
	root := e
	for root.Kind == "member_expression" && root.Object != nil {
		root = *root.Object
	}
	if root.Kind != "identifier" {
		return typescript.ExportBinding{}, "not an identifier or member chain"
	}
	for _, name := range ctx.shadows {
		if name == root.Name {
			return typescript.ExportBinding{}, "locally shadowed"
		}
	}
	if r.typeOnly[ctx.file+"\x00"+root.Name] {
		return typescript.ExportBinding{}, "type-only import"
	}
	b := r.l.target(ctx.file, ctx.scope, e)
	if b.Ambiguous {
		return b, "ambiguous export"
	}
	return b, ""
}

// registration walks one registration's first argument. It returns the number
// of route objects and unresolved entries reached directly by that list.
func (w *routeWalk) registration(reg *registration, chain routeChain) int {
	w.visited[reg] = true
	if reg.call.First == nil {
		return 0
	}
	return w.list(reg.ctx, *reg.call.First, nil, chain, map[string]bool{})
}

func (w *routeWalk) diagnostic(file string, span contract.Span, code, severity, reason string) {
	key := code + "\x00" + file + "\x00" + spanKey(span) + "\x00" + reason
	if w.reported[key] {
		return
	}
	w.reported[key] = true
	w.diagnostics = append(w.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: code, Severity: severity, Message: fmt.Sprintf("route at %s:%d:%d: %s", file, span.StartLine, span.StartColumn+1, reason)})
}

func (w *routeWalk) unresolved(file string, span contract.Span, reason string) int {
	w.diagnostic(file, span, "angular_route_entry_unresolved", "info", reason)
	return 1
}

// list reads a route list in element order.
func (w *routeWalk) list(ctx routeContext, e expression, parent *routeObject, chain routeChain, seen map[string]bool) int {
	e = unwrapRoute(e)
	switch e.Kind {
	case "array":
		n := 0
		for _, p := range e.Parts {
			p = unwrapRoute(p)
			switch {
			case p.Kind == "object":
				w.object(ctx, p, parent, chain)
				n++
			case p.Kind == "spread_element" && len(p.Parts) == 1:
				operand := unwrapRoute(p.Parts[0])
				if operand.Kind == "array" {
					n += w.list(ctx, operand, parent, chain, seen)
				} else {
					n += w.reference(ctx, operand, chain, seen)
				}
			default:
				n += w.unresolved(ctx.file, p.Span, "route entry is not an object literal")
			}
		}
		return n
	case "identifier", "member_expression":
		return w.reference(ctx, e, chain, seen)
	}
	return w.unresolved(ctx.file, e.Span, "route list is not an array literal or proven const array")
}

// reference follows a proven, unmutated module const array. Objects in a
// separately declared array are top-level for their own owner.
func (w *routeWalk) reference(ctx routeContext, e expression, chain routeChain, seen map[string]bool) int {
	b, reason := w.r.resolve(ctx, e.Value)
	if reason != "" {
		return w.unresolved(ctx.file, e.Span, reason)
	}
	value, key, ok := w.r.constArray(b)
	if !ok {
		return w.unresolved(ctx.file, e.Span, "not a proven const route array")
	}
	if w.r.mutated(key) {
		return w.unresolved(ctx.file, e.Span, "mutated const array")
	}
	if seen[key] {
		return w.unresolved(ctx.file, e.Span, "cyclic const array")
	}
	seen[key] = true
	defer delete(seen, key)
	return w.list(routeContext{file: b.File, shadows: []string{}}, value, nil, chain, seen)
}

// constArray finds a module const array (declaration constants or router
// arrays) for a binding.
func (r *router) constArray(b typescript.ExportBinding) (expression, string, bool) {
	if b.External || b.Namespace || b.File == "" || b.Name == "" {
		return expression{}, "", false
	}
	key := b.File + "\x00" + b.Name
	if v, ok := r.l.constants[key]; ok {
		return v, key, true
	}
	if v, ok := r.arrays[key]; ok {
		return v, key, true
	}
	if b.Name == "@default" {
		// `export default [...]` binds the anonymous default as @default; the
		// router payload records the same array under the name "default".
		key = b.File + "\x00default"
		if v, ok := r.arrays[key]; ok {
			return v, key, true
		}
	}
	return expression{}, "", false
}

// mutated reports a mutation of a const array key. The anonymous default
// export array is keyed "default" here, while mutations through a default
// import are recorded under the module's "@default" binding (F3).
func (r *router) mutated(key string) bool {
	if r.l.mutated[key] {
		return true
	}
	if file, ok := strings.CutSuffix(key, "\x00default"); ok {
		return r.l.mutated[file+"\x00@default"]
	}
	return false
}

func (w *routeWalk) object(ctx routeContext, e expression, parent *routeObject, chain routeChain) {
	key := ctx.file + "\x00" + spanKey(e.Span)
	obj := w.objects[key]
	if obj == nil {
		obj = w.r.newRouteObject(ctx, e, parent)
		w.objects[key] = obj
		w.order = append(w.order, obj)
	}
	prefix := "-"
	if chain.prefix != nil {
		prefix = "+" + *chain.prefix
	}
	memo := fmt.Sprintf("%s\x00%s\x00%t\x00%t\x00%s", key, prefix, chain.test, chain.lazy, strings.Join(chain.stack, "\x01"))
	if w.memo[memo] {
		return
	}
	w.memo[memo] = true
	if chain.lazy {
		obj.lazy = true
	} else {
		obj.eager = true
	}
	r := w.r
	test := chain.test || r.test(obj.file)
	var full *string
	if chain.prefix != nil {
		if p, ok := obj.composablePath(r); ok {
			joined := joinRoutePath(*chain.prefix, p)
			full = &joined
			obj.paths[joined] = obj.paths[joined] || !test
		}
	}
	if r.unloaded(obj) != "" {
		// Angular rejects the combination; it never matches children through
		// this route, so neither branch is traversed (F5).
		return
	}
	if k, ok := obj.keys["children"]; ok && k.known {
		w.list(obj.ctx, k.entry.Value, obj, routeChain{prefix: full, test: chain.test, lazy: chain.lazy, stack: chain.stack}, map[string]bool{})
	}
	k, ok := obj.keys["loadChildren"]
	if !ok || !k.known {
		return
	}
	load := w.childrenLoad(obj)
	next := routeChain{prefix: full, test: test, stack: chain.stack}
	switch {
	case load.module != "":
		guard := "module:" + load.module
		if contains(chain.stack, guard) {
			w.diagnostic(obj.file, obj.expr.Span, "angular_route_cycle", "info", "lazy module chain revisits "+load.module)
			return
		}
		next.stack = append(append([]string(nil), chain.stack...), guard)
		for _, m := range r.closure(load.module) {
			for _, reg := range w.modules[m] {
				w.lazyAttached[reg] = true
				c := next
				c.test = next.test || r.test(reg.file)
				if reg.call.Kind == "for_root" {
					w.diagnostic(reg.file, reg.call.Span, "angular_route_for_root_lazy", "warning", "RouterModule.forRoot is reached through the lazy module "+load.module+"; no path is composed from this chain")
					c.prefix = nil
				}
				w.registration(reg, c)
			}
		}
	case load.array != nil:
		guard := "array:" + load.arrayKey
		if contains(chain.stack, guard) {
			w.diagnostic(obj.file, obj.expr.Span, "angular_route_cycle", "info", "lazy routes chain revisits "+strings.ReplaceAll(load.arrayKey, "\x00", "#"))
			return
		}
		next.stack = append(append([]string(nil), chain.stack...), guard)
		next.lazy = true
		w.list(routeContext{file: load.arrayFile, shadows: []string{}}, *load.array, nil, next, map[string]bool{})
	}
}

func (r *router) newRouteObject(ctx routeContext, e expression, parent *routeObject) *routeObject {
	obj := &routeObject{file: ctx.file, expr: e, ctx: ctx, parent: parent, keys: map[string]routeKey{}, paths: map[string]bool{}}
	for i, entry := range e.Entries {
		if entry.Kind == "pair" || entry.Kind == "shorthand_property_identifier" {
			obj.keys[entry.Key] = routeKey{entry: entry, known: true}
			continue
		}
		obj.partial = true
		if name := r.methods[fmt.Sprintf("%s\x00%s\x00%d", ctx.file, spanKey(e.Span), i)]; name != "" && entry.Kind == "method_definition" {
			// A method or accessor with a known name defines only that key.
			obj.keys[name] = routeKey{entry: entry, method: true}
			continue
		}
		// A spread, computed key or unnamed method may overwrite any earlier key
		// and supply any absent one.
		obj.open = true
		for name, k := range obj.keys {
			k.known = false
			obj.keys[name] = k
		}
	}
	obj.segment = "ROUTE " + routeLabel(obj)
	return obj
}

// unloaded returns the R6 code that makes Angular reject a route whose
// children or loadChildren would otherwise be traversed, or "".
func (r *router) unloaded(obj *routeObject) string {
	for _, code := range r.invalid(obj) {
		switch code {
		case "children_with_load_children", "redirect_with_load_children", "redirect_with_children":
			return code
		}
	}
	return ""
}

// routeLabel names a route from source literals only, never from evaluation,
// so identities stay stable across edits elsewhere.
func routeLabel(obj *routeObject) string {
	k, ok := obj.keys["path"]
	if !ok {
		if _, matcher := obj.keys["matcher"]; matcher {
			return "(matcher)"
		}
		return "(no path)"
	}
	v := unwrapRoute(k.entry.Value)
	raw := ""
	switch v.Kind {
	case "string":
		if len(v.Value.Text) < 2 {
			return "(dynamic)"
		}
		raw = v.Value.Text[1 : len(v.Value.Text)-1]
	case "template_string":
		text := v.Value.Text
		if len(text) < 2 || strings.Contains(text, "${") || strings.Contains(text, `\`) {
			return "(dynamic)"
		}
		raw = text[1 : len(text)-1]
	default:
		return "(dynamic)"
	}
	if hasControl(raw) {
		return "(dynamic)"
	}
	if raw == "" {
		return `""`
	}
	return raw
}

// composablePath reports the route's own path when composition may continue.
// An open object never composes: any absent key, including outlet and
// matcher, is unknown. Named methods make only their own key unknown.
func (obj *routeObject) composablePath(r *router) (string, bool) {
	if obj.open {
		return "", false
	}
	if _, matcher := obj.keys["matcher"]; matcher {
		return "", false
	}
	if k, ok := obj.keys["outlet"]; ok {
		if !k.known {
			return "", false
		}
		kind, text, known := r.evaluate(obj.ctx, k.entry.Value)
		if !known || kind != "string" || text != "" && text != "primary" {
			return "", false
		}
	}
	k, ok := obj.keys["path"]
	if !ok || !k.known {
		return "", false
	}
	kind, text, known := r.evaluate(obj.ctx, k.entry.Value)
	if !known || kind != "string" || strings.HasPrefix(text, "/") {
		return "", false
	}
	return text, true
}

func joinRoutePath(prefix, p string) string {
	out := prefix
	if p != "" {
		out = prefix + "/" + p
	}
	if !strings.HasPrefix(out, "/") {
		out = "/" + out
	}
	for strings.Contains(out, "//") {
		out = strings.ReplaceAll(out, "//", "/")
	}
	if len(out) > 1 {
		out = strings.TrimSuffix(out, "/")
	}
	return out
}

func spanKey(s contract.Span) string {
	return fmt.Sprintf("%d:%d-%d:%d", s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
}

func spanContains(outer, inner contract.Span) bool {
	before := func(l1, c1, l2, c2 int) bool { return l1 < l2 || l1 == l2 && c1 <= c2 }
	return before(outer.StartLine, outer.StartColumn, inner.StartLine, inner.StartColumn) && before(inner.EndLine, inner.EndColumn, outer.EndLine, outer.EndColumn)
}

func unwrapRoute(e expression) expression {
	for len(e.Parts) == 1 && (e.Kind == "parenthesized_expression" || e.Kind == "as_expression" || e.Kind == "satisfies_expression") {
		e = e.Parts[0]
	}
	return e
}

func appendUnique(list []string, value string) []string {
	if contains(list, value) {
		return list
	}
	return append(list, value)
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func hasControl(s string) bool {
	for _, c := range s {
		if c < 0x20 {
			return true
		}
	}
	return false
}

// offsets converts a contract span (1-based lines, 0-based byte columns) into
// byte offsets of the normalized source.
func offsets(source []byte, s contract.Span) (int, int) {
	point := func(line, column int) int {
		at := 0
		for n := 1; n < line; n++ {
			next := bytes.IndexByte(source[at:], '\n')
			if next < 0 {
				return len(source)
			}
			at += next + 1
		}
		if at+column > len(source) {
			return len(source)
		}
		return at + column
	}
	start, end := point(s.StartLine, s.StartColumn), point(s.EndLine, s.EndColumn)
	if end < start {
		end = start
	}
	return start, end
}

// applications proves bootstrapApplication/createApplication calls and
// returns the provideRouter call sites in their options' providers (ADR 0036
// Revision 1). The options are an object literal, or an identifier or member
// chain naming an unmutated module const object; providers are read from
// array literals, nested arrays and spreads of proven unmutated const arrays.
// Anything else proves nothing.
func (r *router) applications(routers map[string]typescript.RouterSyntax) map[string]bool {
	out := map[string]bool{}
	files := []string{}
	for file := range routers {
		if _, ok := r.l.files[file]; ok {
			files = append(files, file)
		}
	}
	sort.Strings(files)
	for _, file := range files {
		for _, c := range routers[file].Calls {
			name := map[string]string{"bootstrap_application": "bootstrapApplication", "create_application": "createApplication"}[c.Kind]
			if name == "" || c.Options == nil {
				continue
			}
			ctx := routeContext{file: file, scope: c.Scope, shadows: c.Shadows}
			b, reason := r.resolve(ctx, c.Callee)
			if reason != "" || b.Ambiguous || !b.External || b.File != "@angular/platform-browser" || b.Name != name {
				continue
			}
			config, configCtx, ok := r.config(ctx, *c.Options)
			if !ok {
				continue
			}
			providers, ok := ownKey(config, "providers")
			if !ok {
				continue
			}
			production := !r.test(file)
			r.providers(configCtx, providers, map[string]bool{}, func(site string) {
				out[site] = out[site] || production
			})
		}
	}
	return out
}

// config resolves an ApplicationConfig argument to an object literal and the
// context its identifiers resolve in.
func (r *router) config(ctx routeContext, e expression) (expression, routeContext, bool) {
	e = unwrapRoute(e)
	if e.Kind == "object" {
		return e, ctx, true
	}
	if e.Kind != "identifier" && e.Kind != "member_expression" {
		return expression{}, routeContext{}, false
	}
	b, reason := r.resolve(ctx, e.Value)
	if reason != "" || b.External || b.Namespace || b.File == "" || b.Name == "" {
		return expression{}, routeContext{}, false
	}
	key := b.File + "\x00" + b.Name
	v, ok := r.configs[key]
	if !ok || r.l.mutated[key] {
		return expression{}, routeContext{}, false
	}
	return unwrapRoute(v), routeContext{file: b.File, shadows: []string{}}, true
}

// ownKey returns the value of an object's own pair key when no later spread,
// computed key or method can overwrite it. Duplicate keys: the last wins.
func ownKey(object expression, key string) (expression, bool) {
	var value expression
	found := false
	for _, entry := range object.Entries {
		switch {
		case (entry.Kind == "pair" || entry.Kind == "shorthand_property_identifier") && entry.Key == key:
			value, found = entry.Value, true
		case entry.Kind != "pair" && entry.Kind != "shorthand_property_identifier":
			found = false
		}
	}
	return value, found
}

// providers reports every call expression found as a provider element.
func (r *router) providers(ctx routeContext, e expression, seen map[string]bool, site func(string)) {
	e = unwrapRoute(e)
	switch e.Kind {
	case "array":
		for _, p := range e.Parts {
			p = unwrapRoute(p)
			spread := p.Kind == "spread_element" && len(p.Parts) == 1
			if spread {
				p = unwrapRoute(p.Parts[0])
			}
			switch p.Kind {
			case "call_expression":
				if !spread {
					site(ctx.file + "\x00" + spanKey(p.Span))
				}
			case "array", "identifier", "member_expression":
				r.providers(ctx, p, seen, site)
			}
		}
	case "identifier", "member_expression":
		b, reason := r.resolve(ctx, e.Value)
		if reason != "" {
			return
		}
		value, key, ok := r.constArray(b)
		if !ok || r.mutated(key) || seen[key] {
			return
		}
		seen[key] = true
		defer delete(seen, key)
		r.providers(routeContext{file: b.File, shadows: []string{}}, value, seen, site)
	}
}

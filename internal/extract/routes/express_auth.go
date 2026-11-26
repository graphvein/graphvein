package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file composes Express `app.use` / `router.use` middleware onto the
// routes it runs before (T026, ADR 0079 option 3A,
// docs/extract/auth.md "Express"). Express 4.21.1 (`lib/router/index.js`,
// `lib/application.js`) and Express 5.1.0 (router 2.2.0) agree on the
// model read here: `use` pushes one layer per middleware onto its router's
// stack in call order; a request walks the stack in order, so a
// middleware runs before a route's handler only when its layer was pushed
// before the route's (or before the `use` that mounted the route's
// router) and its path matches the request at a segment boundary
// (`end: false`, case-insensitive by default, trailing slash ignored). A
// mounted router or app inherits every earlier matching layer of its
// parent. The facts say which middleware the code mounts, as written; a
// known auth library factory is named "declares <library> (<detail>)",
// never "protected".

// ExpressAuthContract lists the Express rows in task-file order. Every
// form the code reads maps to exactly one row; everything else is DAD
// (an entry in mounted_middleware_unknown, or nothing).
var ExpressAuthContract = []AuthRow{
	{"DA-E1", "app.use / router.use middleware before the route on its router or mount chain"},
	{"DA-E2", "a call to a closed list of auth library factories (passport, express-jwt, express-oauth2-jwt-bearer, express-openid-connect)"},
	{"DA-E3", "use calls in a module function's body on a parameter receiving the app or router, read at its only call with that object (top level, unguarded)"},
	{"DA-E4", "route(p).all(f...) chained before a method of the same route(p): route middleware of that method"},
	{"DA-E5", "a use middleware argument that is a function of length 4 or more: an error handler, listed apart from mounted middleware"},
	{"DAD", "every other form"},
}

// expressAuthFactories is row DA-E2's closed list, keyed by resolved
// package and export (as the route resolver names an import: "default"
// for the default export or a CommonJS require, "default.x" for its
// member). Read from the packages' sources: passport 0.7.0
// (`module.exports = new Passport()`, `Authenticator.prototype.authenticate`),
// express-jwt 8.5.1 (`exports.expressjwt`) and 6.1.2 (`module.exports =
// function(options)`), express-oauth2-jwt-bearer 1.6.1 (`exports.auth`),
// express-openid-connect 2.19.2 (`module.exports = {...requiresAuth}`).
var expressAuthFactories = map[string]struct{ library, export, option string }{
	"passport#default.authenticate":               {"passport", "authenticate", ""},
	"passport#authenticate":                       {"passport", "authenticate", ""},
	"express-jwt#expressjwt":                      {"express-jwt", "expressjwt", "credentialsRequired"},
	"express-jwt#default.expressjwt":              {"express-jwt", "expressjwt", "credentialsRequired"},
	"express-jwt#default":                         {"express-jwt", "default export", "credentialsRequired"},
	"express-oauth2-jwt-bearer#auth":              {"express-oauth2-jwt-bearer", "auth", "authRequired"},
	"express-oauth2-jwt-bearer#default.auth":      {"express-oauth2-jwt-bearer", "auth", "authRequired"},
	"express-openid-connect#requiresAuth":         {"express-openid-connect", "requiresAuth", ""},
	"express-openid-connect#default.requiresAuth": {"express-openid-connect", "requiresAuth", ""},
}

// maxDerefDepth bounds the bindings DA-E2 follows from a middleware
// argument to the factory call.
const maxDerefDepth = 4

// maxExpressChains bounds the mount chains read per route; beyond it the
// route gets an unknown entry.
const maxExpressChains = 64

// expressArg is one argument of a `use` call after its path: a mounted
// router or app (mount) or a middleware expression.
type expressArg struct {
	index int
	expr  facts.RouteExpr
	mount string
}

// expressEvent is a `use` call on a proven object, or an escape: the
// object passed to another call, which may register middleware on it.
type expressEvent struct {
	file, object string
	scope        int
	span         contract.Span
	conditional  bool
	escape       string
	prefix       string
	// prefixState is "" when the path is known (prefix, "/" when absent),
	// else the reason it is not.
	prefixState string
	args        []expressArg
	// place is where a DA-E3 event enters the object's stack: the call
	// of the function whose body holds the use (via names it); exit marks
	// a use after a return or throw of that body.
	place *anchor
	via   string
	exit  bool
}

// pos is where the event enters its object's stack.
func (ev *expressEvent) pos() (string, int, contract.Span) {
	if ev.place != nil {
		return ev.place.file, ev.place.scope, ev.place.span
	}
	return ev.file, ev.scope, ev.span
}

// expressSite is one mount: event's argument arg mounts to under from.
type expressSite struct {
	from, to string
	event    *expressEvent
	arg      int
	prefix   string
}

// expressAuth holds the events, mounts and route layers of one link run.
type expressAuth struct {
	r      *resolver
	events map[string][]*expressEvent
	sites  []expressSite
	routes map[string][]*routeLayer
}

func newExpressAuth(r *resolver) *expressAuth {
	return &expressAuth{r: r, events: map[string][]*expressEvent{}, routes: map[string][]*routeLayer{}}
}

// routeSite is one route registration call in its file.
type routeSite struct {
	file string
	call facts.RouteCall
}

// chainStep places a registration in its `route(p)` chain (DA-E4):
// before are the steps chained before it, in chain order; later says a
// step is chained after it; group names the chain ("" for a plain
// registration, which is a chain of its own).
type chainStep struct {
	before []facts.RouteCall
	later  bool
	group  string
}

// chainKey identifies a registration call.
func chainKey(file string, c facts.RouteCall) string {
	return fmt.Sprintf("%s\x00%v\x00%s", file, c.Span, c.Method)
}

// routeChains groups the registrations of one `router.route(p)` builder:
// the steps share the pattern argument (the same source offset) and the
// start of their call; a step chained later ends later.
func routeChains(sites []routeSite) map[string]chainStep {
	groups := map[string][]routeSite{}
	var keys []string
	for _, s := range sites {
		if s.call.Pattern == nil {
			continue
		}
		g := fmt.Sprintf("%s\x00%d", s.file, s.call.Pattern.Offset)
		if _, ok := groups[g]; !ok {
			keys = append(keys, g)
		}
		groups[g] = append(groups[g], s)
	}
	out := map[string]chainStep{}
	for _, g := range keys {
		list := groups[g]
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i].call.Span, list[j].call.Span
			if a.EndLine != b.EndLine {
				return a.EndLine < b.EndLine
			}
			return a.EndColumn < b.EndColumn
		})
		var before []facts.RouteCall
		for i, s := range list {
			out[chainKey(s.file, s.call)] = chainStep{before: append([]facts.RouteCall(nil), before...), later: i < len(list)-1, group: g}
			before = append(before, s.call)
		}
	}
	return out
}

// routeLayer is a route registration as a layer of its router's stack:
// Express pushes it when the registration runs, and a request whose
// method and path it matches runs its handlers before any later layer.
type routeLayer struct {
	file        string
	scope       int
	span        contract.Span
	method      string
	pattern     string
	dynamic     bool
	expression  string
	group       string
	conditional bool
}

// addRouteLayer records registration c on object as a route layer.
func (x *expressAuth) addRouteLayer(file string, c facts.RouteCall, object string, step chainStep) {
	args := c.Args
	l := &routeLayer{file: file, scope: c.Scope, span: c.Span, method: strings.ToUpper(c.Method), group: step.group, conditional: c.Conditional}
	pattern := c.Pattern
	if pattern == nil && len(args) > 0 {
		p := args[0]
		pattern = &p
		args = args[1:]
	}
	switch {
	case pattern == nil:
		l.dynamic = true
	case pattern.Kind == "literal":
		l.pattern = pattern.Name
	default:
		l.dynamic, l.pattern = true, whitespace(pattern.Text)
	}
	if step.group == "" {
		l.group = chainKey(file, c)
	}
	names := make([]string, 0, len(args))
	for _, a := range args {
		names = append(names, whitespace(a.Text))
	}
	l.expression = strings.Join(names, ", ")
	x.routes[object] = append(x.routes[object], l)
}

// methodMayRun says whether a route layer of method layer runs for
// requests of a route of method route: `all` runs for every method, a
// GET layer also answers HEAD (Express Route._handles_method), and a
// route registered with `all` (ANY) receives every method.
func methodMayRun(layer, route string) bool {
	return layer == "ALL" || route == "ANY" || layer == route || route == "HEAD" && layer == "GET"
}

// routeDisjoint reports whether two route paths relative to one router
// are proven never to match the same request: Express route layers match
// the whole path (end: true) case-insensitively, a plain `:name`
// parameter matches one segment. Any other syntax may match.
//
// Segments before the first one with other syntax are anchored (a
// wildcard, an optional parameter or a group only changes what matches
// from its own segment on), so two literal segments at the same position
// before it that differ prove the paths disjoint.
func routeDisjoint(a, b string) bool {
	p, q := segments(a), segments(b)
	for i := 0; i < len(p) && i < len(q); i++ {
		if special(p[i]) || special(q[i]) {
			return false
		}
		if !plainParameter(p[i]) && !plainParameter(q[i]) && !strings.EqualFold(p[i], q[i]) {
			return true
		}
	}
	for _, seg := range append(append([]string(nil), p...), q...) {
		if special(seg) {
			return false
		}
	}
	return len(p) != len(q)
}

// arity is JavaScript's Function.length of a middleware argument when it
// is proven: an inline function, or a name bound (locally or through one
// import) to a function declaration or a const function expression. It
// returns -1 otherwise.
func (x *expressAuth) arity(file string, scope int, e facts.RouteExpr, depth int) int {
	if depth > maxDerefDepth {
		return -1
	}
	data := x.r.files[file].Routes
	if data == nil {
		return -1
	}
	switch e.Kind {
	case "arrow_function", "function_expression", "function":
		for _, sc := range data.Scopes {
			if sc.Function && sc.Start == e.Offset && sc.Start != 0 {
				return facts.FunctionLength(sc.Params)
			}
		}
	case "identifier":
		b, at, ok := x.r.binding(file, scope, e.Name)
		if !ok {
			return -1
		}
		if b.Module != "" {
			im := x.r.modules.ImportBinding(file, b.Module, b.Export, "")
			if im.External || im.Namespace || im.File == "" || im.Name == "" {
				return -1
			}
			return x.arity(im.File, 0, facts.RouteExpr{Kind: "identifier", Name: im.Name, Text: im.Name}, depth+1)
		}
		switch b.Value.Kind {
		case "function":
			if b.ReturnScope > 0 && b.ReturnScope < len(data.Scopes) {
				return facts.FunctionLength(data.Scopes[b.ReturnScope].Params)
			}
		case "arrow_function", "function_expression":
			if b.Constant || b.ModuleConst {
				return x.arity(file, at, b.Value, depth+1)
			}
		}
	}
	return -1
}

// functionScope is the nearest enclosing function scope of scope, or -1
// at the top level of the file.
func (x *expressAuth) functionScope(file string, scope int) int {
	data := x.r.files[file].Routes
	for scope > 0 && data != nil && scope < len(data.Scopes) {
		if data.Scopes[scope].Function {
			return scope
		}
		scope = data.Scopes[scope].Parent
	}
	return -1
}

// functionArg reports whether e is a function as written: an inline
// function or an identifier bound to a function declaration.
func (x *expressAuth) functionArg(file string, scope int, e facts.RouteExpr) bool {
	if e.Kind == "arrow_function" || e.Kind == "function_expression" || e.Kind == "function" {
		return true
	}
	if e.Kind == "identifier" {
		if b, _, ok := x.r.binding(file, scope, e.Name); ok && b.Module == "" && (b.Value.Kind == "function" || b.Value.Kind == "ambiguous") {
			return true
		}
	}
	return false
}

// addUse records a `use` call on object and returns its event, so the
// caller can attach the mounts it makes.
func (x *expressAuth) addUse(file string, c facts.RouteCall, object value) *expressEvent {
	ev := x.useEvent(file, c, object.key)
	x.events[object.key] = append(x.events[object.key], ev)
	return ev
}

// useEvent reads a `use` call on object: its path and arguments.
func (x *expressAuth) useEvent(file string, c facts.RouteCall, object string) *expressEvent {
	ev := &expressEvent{file: file, object: object, scope: c.Scope, span: c.Span, conditional: c.Conditional, prefix: "/"}
	start := 0
	if len(c.Args) > 1 {
		first := c.Args[0]
		v := x.r.resolve(file, c.Scope, first, 0, nil)
		switch {
		case v.kind == "app" || v.kind == "router" || x.functionArg(file, c.Scope, first):
		case v.kind == "literal" && (first.Kind == "literal" || v.constant):
			ev.prefix, start = v.name, 1
		case first.Kind == "literal" || first.Kind == "template_string" || first.Kind == "binary_expression" || first.Kind == "array" || first.Kind == "regex":
			ev.prefixState, start = "dynamic path "+whitespace(first.Text), 1
		default:
			// Express takes the first argument as the path unless it is a
			// function (or an array starting with one): a call or an
			// unresolved name may be either.
			ev.prefixState = "first argument " + whitespace(first.Text) + " may be a path"
		}
	}
	for i := start; i < len(c.Args); i++ {
		a := expressArg{index: i, expr: c.Args[i]}
		if t := x.r.resolve(file, c.Scope, c.Args[i], 0, nil); t.kind == "app" || t.kind == "router" {
			a.mount = t.key
		}
		ev.args = append(ev.args, a)
	}
	return ev
}

// addSite records that ev's argument at index arg mounts to under from.
func (x *expressAuth) addSite(from, to string, ev *expressEvent, arg int, prefix string) {
	x.sites = append(x.sites, expressSite{from, to, ev, arg, prefix})
}

// addEscapes records every call that receives a proven object as an
// argument (directly or as an object-literal property value) and is not
// a method of a proven object: the callee may register middleware on it.
func (x *expressAuth) addEscapes(read map[inlineKey]bool) {
	for _, file := range facts.Paths(x.r.files) {
		data := x.r.files[file].Routes
		if data == nil {
			continue
		}
		for _, cc := range data.Clients {
			e := cc.Expression
			if e.Kind != "call_expression" || e.Object == nil {
				continue
			}
			if e.Object.Kind == "member_expression" && e.Object.Object != nil {
				if v := x.r.resolve(file, cc.Scope, *e.Object.Object, 0, nil); v.kind == "app" || v.kind == "router" {
					continue
				}
			}
			if e.Object.Kind == "identifier" && e.Object.Name == "require" {
				continue
			}
			var candidates []facts.RouteExpr
			for _, a := range e.Args {
				candidates = append(candidates, a)
				if a.Kind == "object" {
					keys := make([]string, 0, len(a.Properties))
					for k := range a.Properties {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					for _, k := range keys {
						candidates = append(candidates, a.Properties[k])
					}
				}
			}
			seen := map[string]bool{}
			for _, a := range candidates {
				if a.Kind != "identifier" && a.Kind != "member_expression" {
					continue
				}
				v := x.r.resolve(file, cc.Scope, a, 0, nil)
				if (v.kind != "app" && v.kind != "router") || seen[v.key] || read[inlineKey{file, cc.Span, v.key}] {
					continue
				}
				seen[v.key] = true
				x.events[v.key] = append(x.events[v.key], &expressEvent{file: file, object: v.key, scope: cc.Scope, span: cc.Span, escape: whitespace(e.Object.Text), prefix: "/"})
			}
		}
	}
}

// anchor is the place the route enters an object's stack: its own
// registration call, or the `use` call that mounts the next router down.
type anchor struct {
	file  string
	scope int
	span  contract.Span
	event *expressEvent
	arg   int
}

func before(a, b contract.Span) bool {
	if a.StartLine != b.StartLine {
		return a.StartLine < b.StartLine
	}
	return a.StartColumn < b.StartColumn
}

// order says whether ev runs before the anchor on object: "before",
// "after" or "unknown". Source order holds within one function body of
// one file; across files only between two top-level statements when one
// is in the file that creates the object (it evaluates first).
func (x *expressAuth) order(ev *expressEvent, at anchor, objectFile string) string {
	if at.event != nil && ev == at.event {
		return "same"
	}
	file, scope, sp := ev.pos()
	return x.orderAt(file, scope, sp, at, objectFile)
}

// orderAt is order for a stack entry pushed by the statement at file,
// scope and sp.
func (x *expressAuth) orderAt(file string, scope int, sp contract.Span, at anchor, objectFile string) string {
	fe, fa := x.functionScope(file, scope), x.functionScope(at.file, at.scope)
	if file == at.file {
		if fe != fa {
			return "unknown"
		}
		if before(sp, at.span) {
			return "before"
		}
		return "after"
	}
	if fe == -1 && fa == -1 {
		if file == objectFile {
			return "before"
		}
		if at.file == objectFile {
			return "after"
		}
	}
	return "unknown"
}

// segments splits an Express path into its non-empty segments.
func segments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

// special reports Express path syntax other than a plain `:name`
// parameter (wildcards, optional and repeat modifiers, groups).
func special(s string) bool {
	return strings.ContainsAny(s, "*?+()[]{}")
}

func plainParameter(s string) bool {
	return strings.HasPrefix(s, ":") && !special(s)
}

// useMatch says whether a `use` path matches every request of a route
// path relative to the same router: "yes", "no" or a reason it may.
func useMatch(prefix, route string) string {
	p, s := segments(prefix), segments(route)
	for _, seg := range s {
		if special(seg) {
			return "route path " + route + " has wildcard or optional syntax"
		}
	}
	for i, seg := range p {
		if special(seg) {
			return "path " + prefix + " has wildcard or optional syntax"
		}
		if i >= len(s) {
			return "no"
		}
		switch {
		case plainParameter(seg):
		case plainParameter(s[i]):
			return "route parameter " + s[i] + " may match " + seg
		case seg == s[i]:
		case strings.EqualFold(seg, s[i]):
			return "path " + prefix + " matches only when routing is case-insensitive (the default)"
		default:
			return "no"
		}
	}
	return "yes"
}

// literalExcluded reports that a `use` path never matches a request of a
// route path: a literal segment of the prefix differs from the route's
// literal segment at the same place before any wildcard, optional or
// parameter segment of either (row RO-V2 reads it; useMatch, which DA-E1
// reads, answers a reason instead of "no" whenever the route has a
// wildcard anywhere).
func literalExcluded(prefix, route string) bool {
	p, s := segments(prefix), segments(route)
	for i, seg := range p {
		if i >= len(s) {
			return true
		}
		if special(seg) || special(s[i]) || strings.HasPrefix(seg, ":") || strings.HasPrefix(s[i], ":") {
			return false
		}
		if !strings.EqualFold(seg, s[i]) {
			return true
		}
	}
	return false
}

// level is one object of a mount chain, the anchor the route enters its
// stack at and the route's path relative to it.
type level struct {
	object string
	at     anchor
	path   string
}

// chains returns the mount chains of an object up to every proven app
// (each app is served on its own and may also be mounted), as levels from
// the object up; ok is false when there are more than the cap.
func (x *expressAuth) chains(object string, at anchor, path string) ([][]level, bool) {
	var out [][]level
	ok := true
	var walk func([]level, map[string]bool)
	walk = func(chain []level, seen map[string]bool) {
		if len(out) >= maxExpressChains {
			ok = false
			return
		}
		top := chain[len(chain)-1]
		if x.r.objects[top.object] == "app" {
			out = append(out, append([]level(nil), chain...))
		}
		for _, s := range x.sites {
			if s.to != top.object || seen[s.from] {
				continue
			}
			seen[s.from] = true
			next := level{object: s.from, at: anchor{file: s.event.file, scope: s.event.scope, span: s.event.span, event: s.event, arg: s.arg}, path: s.prefix + "/" + top.path}
			walk(append(chain, next), seen)
			delete(seen, s.from)
		}
	}
	walk([]level{{object: object, at: at, path: path}}, map[string]bool{object: true})
	return out, ok
}

// mwEntry is one mounted middleware or unknown entry with its key (file,
// span, argument) for comparing chains.
type mwEntry struct {
	key  string
	data map[string]any
}

// subtreeMiddleware reports whether object or a router mounted under it
// registers middleware or escapes (a sibling mounted earlier may run its
// middleware for this route's requests).
func (x *expressAuth) subtreeMiddleware(object string, seen map[string]bool) bool {
	if seen[object] {
		return false
	}
	seen[object] = true
	for _, ev := range x.events[object] {
		if ev.escape != "" {
			return true
		}
		for _, a := range ev.args {
			if a.mount == "" {
				return true
			}
		}
	}
	for _, s := range x.sites {
		if s.from == object && x.subtreeMiddleware(s.to, seen) {
			return true
		}
	}
	return false
}

// subtreeRoute returns the first route layer of object, mounted at
// prefix, or of a router mounted under it, whose method and path may
// match a request of a route of method at path (relative to the same
// parent router), as "<expression> (<METHOD> <path>)"; "" when none.
func (x *expressAuth) subtreeRoute(object, prefix, method, path string, seen map[string]bool) string {
	if seen[object] {
		return ""
	}
	seen[object] = true
	for _, rl := range x.routes[object] {
		if methodMayRun(rl.method, method) && (rl.dynamic || !routeDisjoint(prefix+"/"+rl.pattern, path)) {
			return rl.expression + " (" + rl.method + " " + rl.pattern + ")"
		}
	}
	for _, st := range x.sites {
		if st.from == object {
			if h := x.subtreeRoute(st.to, prefix+"/"+st.prefix, method, path, seen); h != "" {
				return h
			}
		}
	}
	return ""
}

// chainMiddleware composes one chain: root level first, each level's
// events in stack order.
//
// first names the apps and routers mounted earlier on the chain, below a
// prefix that may match the route's path, whose own stack holds
// middleware (row RO-V2): Express runs that middleware for every request
// that reaches the mount, so a catch-all among it (a 404 handler, an auth
// check that ends the response) may answer the route's requests first
// (Express 4.21.1 and 5.1.0).
func (x *expressAuth) chainMiddleware(chain []level, method, group string) (facts []mwEntry, unknown []mwEntry, errorHandlers []mwEntry, first []string) {
	addAt := func(reason, file string, span contract.Span, expression string) {
		tagAuth(reasonTag(reason))
		unknown = append(unknown, mwEntry{key: reason + "\x00" + file + fmt.Sprint(span) + expression, data: map[string]any{"reason": reason, "expression": expression, "file": file, "span": span}})
	}
	add := func(reason string, ev *expressEvent, expression string) {
		addAt(reason, ev.file, ev.span, expression)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		lv := chain[i]
		objectFile := x.r.objectFiles[lv.object]
		// DA-E1: an earlier route layer of this router whose method and
		// path may match runs its handlers first (a handler that calls
		// next() acts as middleware: `app.all('*', requireAuth)`).
		seenGroup := map[string]bool{}
		for _, rl := range x.routes[lv.object] {
			if i == 0 && rl.group == group || seenGroup[rl.group] || !methodMayRun(rl.method, method) {
				continue
			}
			if x.orderAt(rl.file, rl.scope, rl.span, lv.at, objectFile) == "after" {
				continue
			}
			if !rl.dynamic && routeDisjoint(rl.pattern, lv.path) {
				continue
			}
			seenGroup[rl.group] = true
			addAt("earlier route handler "+rl.expression+" ("+rl.method+" "+rl.pattern+") may run first", rl.file, rl.span, rl.expression)
		}
		for _, ev := range x.events[lv.object] {
			o := x.order(ev, lv.at, objectFile)
			if o == "after" {
				continue
			}
			if ev.escape != "" {
				add("passed to "+ev.escape+", which may register middleware", ev, ev.escape)
				continue
			}
			match := "yes"
			if ev.prefixState == "" {
				match = useMatch(ev.prefix, lv.path)
			}
			for _, a := range ev.args {
				if o == "same" && a.index >= lv.at.arg {
					break
				}
				name := whitespace(a.expr.Text)
				if a.mount != "" {
					// A router or app mounted earlier runs its own middleware
					// for every request that reaches it.
					if a.mount == chainObject(chain, i-1) || match == "no" {
						continue
					}
					if x.subtreeMiddleware(a.mount, map[string]bool{}) {
						add("router "+name+" mounted earlier may run its own middleware first", ev, name)
						if ev.prefixState != "" || !literalExcluded(ev.prefix, lv.path) {
							first = append(first, fmt.Sprintf("%s (%s:%d)", name, ev.file, ev.span.StartLine))
						}
						continue
					}
					if h := x.subtreeRoute(a.mount, ev.prefix, method, lv.path, map[string]bool{}); h != "" {
						add("router "+name+" mounted earlier has route handler "+h+" that may run first", ev, name)
					}
					continue
				}
				switch {
				case match == "no":
				case ev.prefixState != "":
					add(ev.prefixState, ev, name)
				case o == "unknown":
					add("order relative to the route not proven (another file or function)", ev, name)
				case ev.conditional && ev.exit:
					add("conditional registration: a return or throw before it in "+ev.via+" may skip it", ev, name)
				case ev.conditional:
					add("conditional registration", ev, name)
				case a.expr.Kind == "spread_element":
					add("spread argument", ev, name)
				case match != "yes":
					add(match, ev, name)
				case x.arity(ev.file, ev.scope, a.expr, 0) >= 4:
					// DA-E5: Express skips a layer whose function takes four
					// or more parameters unless an error is passed
					// (Layer.handle_request, fn.length > 3).
					tagAuth("DA-E5/error-handler")
					entry := map[string]any{"name": name, "prefix": ev.prefix, "file": ev.file, "span": ev.span}
					errorHandlers = append(errorHandlers, mwEntry{key: fmt.Sprintf("%s%v#%d", ev.file, ev.span, a.index), data: entry})
				default:
					tagAuth("DA-E1/use")
					if i > 0 {
						tagAuth("DA-E1/parent")
					}
					if ev.prefix != "/" {
						tagAuth("DA-E1/prefix")
					}
					entry := map[string]any{"name": name, "prefix": ev.prefix, "file": ev.file, "span": ev.span}
					if ev.place != nil {
						tagAuth("DA-E3/use")
						entry["via"] = map[string]any{"function": ev.via, "file": ev.place.file, "span": ev.place.span}
					}
					if d := x.r.declares(ev.file, ev.scope, a.expr); d != nil {
						entry["declares"] = d
					}
					facts = append(facts, mwEntry{key: fmt.Sprintf("%s%v#%d", ev.file, ev.span, a.index), data: entry})
				}
			}
		}
	}
	return facts, unknown, errorHandlers, first
}

// chainObject is the object at chain index i, or "" below the route's.
func chainObject(chain []level, i int) string {
	if i < 0 {
		return ""
	}
	return chain[i].object
}

// reasonTag maps an unknown reason to its contract tag.
func reasonTag(reason string) string {
	switch {
	case strings.HasPrefix(reason, "passed to"):
		return "DA-E1/escape"
	case strings.HasPrefix(reason, "router "):
		return "DA-E1/sibling"
	case strings.HasPrefix(reason, "dynamic path"):
		return "DA-E1/prefix-dynamic"
	case strings.HasPrefix(reason, "first argument"):
		return "DA-E1/first-argument"
	case strings.HasPrefix(reason, "order"):
		return "DA-E1/order"
	case strings.HasPrefix(reason, "conditional registration"):
		return "DA-E1/conditional"
	case reason == "spread argument":
		return "DA-E1/spread"
	case reason == "declared on some mount chains only":
		return "DA-E1/chains"
	case strings.HasPrefix(reason, "not mounted"):
		return "DA-E1/unmounted"
	case strings.HasPrefix(reason, "more than"):
		return "DA-E1/chain-cap"
	case strings.HasPrefix(reason, "earlier route handler"):
		return "DA-E1/earlier-route"
	}
	return "DA-E1/prefix-match"
}

// routeMiddleware composes the mounted middleware of a route registered
// by call c on object, as facts common to every chain and unknown
// entries; both nil when there is none. first names the mounted apps and
// routers whose middleware may answer the route's requests first on any
// chain (chainMiddleware, row RO-V2).
func (x *expressAuth) routeMiddleware(file string, c facts.RouteCall, object, method, pattern string, step chainStep) (mounted []map[string]any, unknownList []map[string]any, errorList []map[string]any, first []string) {
	group := step.group
	if group == "" {
		group = chainKey(file, c)
	}
	chains, ok := x.chains(object, anchor{file: file, scope: c.Scope, span: c.Span}, pattern)
	var extra []mwEntry
	if !ok {
		reason := fmt.Sprintf("more than %d mount chains", maxExpressChains)
		tagAuth("DA-E1/chain-cap")
		extra = append(extra, mwEntry{key: reason, data: map[string]any{"reason": reason}})
	}
	if len(chains) == 0 {
		chains = [][]level{{{object: object, at: anchor{file: file, scope: c.Scope, span: c.Span}, path: pattern}}}
		reason := "not mounted from a proven app: middleware of the routers above is not read"
		tagAuth("DA-E1/unmounted")
		extra = append(extra, mwEntry{key: reason, data: map[string]any{"reason": reason}})
	}
	var common []mwEntry
	count := map[string]int{}
	unknownSeen := map[string]bool{}
	var unknown []map[string]any
	for _, e := range extra {
		unknownSeen[e.key] = true
		unknown = append(unknown, e.data)
	}
	all := map[string]mwEntry{}
	var errorHandlers []map[string]any
	errorSeen := map[string]bool{}
	for i, chain := range chains {
		found, missing, errs, before := x.chainMiddleware(chain, method, group)
		first = goUnion(first, before)
		for _, e := range errs {
			if !errorSeen[e.key] {
				errorSeen[e.key] = true
				errorHandlers = append(errorHandlers, e.data)
			}
		}
		local := map[string]bool{}
		for _, f := range found {
			if local[f.key] {
				continue
			}
			local[f.key] = true
			count[f.key]++
			if _, ok := all[f.key]; !ok {
				all[f.key] = f
				if i == 0 {
					common = append(common, f)
				}
			}
		}
		for _, u := range missing {
			if !unknownSeen[u.key] {
				unknownSeen[u.key] = true
				unknown = append(unknown, u.data)
			}
		}
	}
	var out []map[string]any
	for _, f := range common {
		if count[f.key] == len(chains) {
			out = append(out, f.data)
		}
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if count[k] < len(chains) {
			tagAuth("DA-E1/chains")
			d := all[k].data
			unknown = append(unknown, map[string]any{"reason": "declared on some mount chains only", "expression": d["name"], "file": d["file"], "span": d["span"]})
		}
	}
	return out, unknown, errorHandlers, first
}

// deref follows e through bindings (local and imported) and
// object-literal properties to the expression it names, at most
// maxDerefDepth steps.
func (r *resolver) deref(file string, scope int, e facts.RouteExpr, depth int) (string, int, facts.RouteExpr) {
	if depth > maxDerefDepth {
		return file, scope, facts.RouteExpr{}
	}
	switch e.Kind {
	case "identifier":
		b, at, ok := r.binding(file, scope, e.Name)
		if !ok {
			return file, scope, e
		}
		if b.Module != "" {
			x := r.modules.ImportBinding(file, b.Module, b.Export, "")
			if x.External || x.Namespace || x.File == "" || x.Name == "" {
				return file, scope, e
			}
			return r.deref(x.File, 0, facts.RouteExpr{Kind: "identifier", Name: x.Name, Text: x.Name}, depth+1)
		}
		if b.Value.Kind == "" || b.Value.Kind == "function" || b.Value.Kind == "ambiguous" || b.Value.Kind == "parameter" {
			return file, scope, e
		}
		return r.deref(file, at, b.Value, depth+1)
	case "member_expression":
		if e.Object == nil {
			return file, scope, e
		}
		f, s, o := r.deref(file, scope, *e.Object, depth+1)
		if o.Kind == "object" && !o.Spread {
			if v, ok := o.Properties[e.Name]; ok {
				return r.deref(f, s, v, depth+1)
			}
		}
	}
	return file, scope, e
}

// declares is row DA-E2: the middleware expression is (or names, through
// bindings) a call to a factory of the closed list. It returns nil
// otherwise (DAD: the middleware is stated as written only).
func (r *resolver) declares(file string, scope int, e facts.RouteExpr) map[string]any {
	f, s, call := r.deref(file, scope, e, 0)
	if call.Kind != "call_expression" || call.Object == nil {
		tagAuth("DAD/not-a-factory")
		return nil
	}
	callee := r.resolve(f, s, *call.Object, 0, nil)
	factory, ok := expressAuthFactories[callee.file+"#"+callee.name]
	if callee.kind != "import" || !ok {
		tagAuth("DAD/not-a-factory")
		return nil
	}
	tagAuth("DA-E2/" + factory.library)
	out := map[string]any{"library": factory.library, "detail": factory.export}
	if factory.library == "passport" {
		out["detail"] = "strategy ?"
		if len(call.Args) > 0 {
			_, _, strategy := r.deref(f, s, call.Args[0], 0)
			if strategy.Kind == "literal" {
				out["detail"] = strategy.Name
			} else if call.Args[0].Kind == "array" {
				out["detail"] = whitespace(call.Args[0].Text)
			} else {
				tagAuth("DA-E2/strategy-unknown")
			}
		}
	}
	if factory.option != "" {
		out["option"] = factory.option
		out["optional"] = false
		if len(call.Args) > 0 {
			_, _, options := r.deref(f, s, call.Args[0], 0)
			value, present := options.Properties[factory.option]
			switch {
			case options.Kind != "object":
				out["optional"] = "unknown"
			case present && value.Kind == "false":
				out["optional"] = true
			case present && value.Kind == "true":
			case present || options.Spread:
				out["optional"] = "unknown"
			}
			if out["optional"] == "unknown" {
				tagAuth("DA-E2/option-unknown")
			}
		}
	}
	return out
}

// routeDeclares applies DA-E2 to a route's own middleware arguments: one
// entry per argument that is a factory call, with its index.
func (r *resolver) routeDeclares(file string, scope int, args []facts.RouteExpr) []map[string]any {
	var out []map[string]any
	for i, a := range args {
		if d := r.declares(file, scope, a); d != nil {
			d["index"] = i
			d["name"] = whitespace(a.Text)
			out = append(out, d)
		}
	}
	return out
}

// sortEvents puts each object's events in stack order: the object's own
// file first (it evaluates before any importer), then by file and span.
func (x *expressAuth) sortEvents() {
	for object, list := range x.events {
		home := x.r.objectFiles[object]
		sort.SliceStable(list, func(i, j int) bool {
			af, _, as := list[i].pos()
			bf, _, bs := list[j].pos()
			if (af == home) != (bf == home) {
				return af == home
			}
			if af != bf {
				return af < bf
			}
			if as != bs {
				return before(as, bs)
			}
			// Two uses of one DA-E3 call: body order.
			return before(list[i].span, list[j].span)
		})
	}
}

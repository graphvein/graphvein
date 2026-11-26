package routes

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Composition of Go routes (rows GR-C1-GR-C5, task T023): the
// composition graph of router objects, its roots, and the path entries
// each registration gets from every root that reaches its receiver.

// goComposition is one chi Mount (GR-C1) or net/http composition (GR-C3)
// read on proven parent objects.
type goComposition struct {
	// kind is mount (chi Mount), mux (net/http Handle/HandleFunc of a
	// ServeMux) or muxchi (a chi router at a ServeMux's "/", GR-C7).
	kind string
	// shadows are the paths a forwarding closure may answer itself
	// (GR-O8): a composed path that one matches is refused.
	shadows []string
	// methods are the request methods a wrapper or forwarding closure on
	// the way answers itself.
	methods           []string
	parents, children []string
	// unproven reports a mux handler that may also hold a value no row
	// proves (a binding of another value reaches it): no path is claimed
	// through the composition.
	unproven bool
	// alternative reports a handler that may instead hold a plain handler
	// (no router): the composition is conditional.
	alternative bool
	at          goAt
	call        *facts.GoRouteCall
	site        string
	// outer is the net/http registration pattern; prefix is the Mount
	// prefix or the StripPrefix prefix (nil without StripPrefix); value is
	// the router argument.
	outer, prefix, value *facts.GoExpr
}

// goEdge is one resolved step of the composition graph: the child object's
// paths are its parent's paths extended by the edge.
type goEdge struct {
	parent, child string
	// kind is same (chi Group and With, gin Use, a parameter binding),
	// chi (chi Route and Mount: TrimSuffix(prefix, "/")), gin (gin Group:
	// joinPaths) or mux (net/http: StripPrefix prefix and outer pattern).
	kind        string
	prefix      string
	outer       string
	resolution  string
	conditional bool
	file        string
	// blocked names why no path is claimed through the edge ("" for a
	// resolved edge): the child still is no root.
	blocked string
	// shadows are a muxchi edge's forwarding-closure shadows (GR-O8);
	// methods those its wrappers or closure answer; at is a
	// composition edge's call position.
	shadows []string
	methods []string
	at      goAt
	// hop is the unresolved mount a blocked edge stops at (T029 FU-2):
	// a prefix that is not a constant or a router argument no rule
	// proves; nil for other refusals. unknownPrefix marks a hop whose
	// prefix (chi Route/Mount, gin Group, StripPrefix) is not read,
	// unknownOuter a net/http hop whose outer pattern is not read.
	hop                         *mountHop
	unknownPrefix, unknownOuter bool
	// outerTail is the literal tail T of an unread outer pattern X + T
	// whose StripPrefix prefix is exactly X: the
	// router below the hop sees only paths starting with T. tailKnown
	// says it was read.
	outerTail string
	tailKnown bool
}

// sameReadOperand reports whether two expressions read one value at one
// call: the same identifier or selector chain of identifiers (no call,
// index or conversion, whose value may differ between two evaluations).
func sameReadOperand(a, b *facts.GoExpr) bool {
	if a == nil || b == nil || a.Kind != b.Kind || a.Name != b.Name {
		return false
	}
	switch a.Kind {
	case facts.GoIdentifier:
		return true
	case facts.GoSelector:
		return sameReadOperand(a.Object, b.Object)
	}
	return false
}

// outerTail reads the outer pattern of a net/http composition as X + T,
// T a constant, X spelled as the StripPrefix prefix (sameReadOperand):
// StripPrefix(X) leaves T + the rest, so GR-C3's tail check applies to T
// below the unread X. ok is false for any other form.
func (l *goLinker) outerTail(at goAt, outer, strip *facts.GoExpr) (tail string, ok bool) {
	if outer == nil || strip == nil || outer.Kind != facts.GoBinary || outer.Value != "+" {
		return "", false
	}
	if !sameReadOperand(outer.Left, strip) {
		return "", false
	}
	tail, _, ok = l.constant(at, outer.Right)
	if !ok || !strings.HasPrefix(tail, "/") || strings.ContainsAny(tail, "{} ") || tail == "/" {
		// A tail "/" passes every path on, as the unread outer pattern
		// already does (the hop's text and registration stay as they
		// were).
		return "", false
	}
	return tail, true
}

// goHop is one net/http composition a chain passes: the outer pattern on
// the parent mux and the StripPrefix prefix.
type goHop struct {
	outer, prefix string
	// gate is the sibling set of the hop's parent mux and at the hop's
	// composition.
	gate *goGate
	at   goAt
}

// goMountLevel is one chi Route or Mount a chain passed:
// the parent object and the chain's raw path at the parent's tree root.
type goMountLevel struct {
	tree string
	base string
}

// goChain is one way a root reaches an object: the raw path prefix in the
// framework's syntax (chi and gin) or the net/http hops, with the chain's
// conditional, resolution and scope.
type goChain struct {
	raw         string
	hops        []goHop
	conditional bool
	resolution  string
	scope       testscope.Scope
	// strip is the StripPrefix prefix of the root's serving position and
	// assume the wrappers it is served through (GR-C6, GR-V5).
	strip  string
	assume []string
	// root is the root the chain starts at (the oracle dispatches
	// through it).
	root string
	// gate is the ServeMux -> chi hop the chain passed (GR-C7), nil
	// otherwise.
	gate *goGateHop
	// methods are the request methods a wrapper or forwarding closure on
	// the chain answers itself.
	methods []string
	// owner is the net/http mux whose patterns the chain's next hop is
	// registered beside; mounts are the chi trees the
	// chain was mounted from.
	owner  string
	mounts []goMountLevel
	// bare reports that the chain's last chi Route or Mount prefix has no
	// trailing slash: chi's Mount then also registers the bare prefix, so
	// a "/" route of the mounted tree answers prefix as well as prefix/
	// (row GR-C1).
	bare bool
}

func (c goChain) key() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q|%v|%s|%s|%q|%q", c.raw, c.conditional, c.resolution, c.scope, c.strip, c.assume)
	for _, h := range c.hops {
		fmt.Fprintf(&b, "|%q %q", h.outer, h.prefix)
	}
	if c.gate != nil {
		fmt.Fprintf(&b, "|gate %q", c.gate.id)
	}
	fmt.Fprintf(&b, "|%q|%d|%v", c.methods, len(c.mounts), c.bare)
	for _, m := range c.mounts {
		fmt.Fprintf(&b, "|%s %q", m.tree, m.base)
	}
	return b.String()
}

// goPathEntry is a Go route's path entry: the shared entry with ADR 0019's
// trailing_slash flag, as FastAPI's; its assumption GR-V5 and the wrappers
// a root is served through (row GR-C6) are the shared entry's Assumptions
// and ServedThrough.
type goPathEntry struct {
	fastapiPathEntry
}

// goTestFail, when set by a test, is called at each isolated step of the
// Go route linker (a file's reading, a registration's paths) and may panic
// to check that a failure there never stops the other steps.
var goTestFail func(step, file string)

// reachCond reports that e carries object obj to its use only through
// bindings in conditional context (GR-C5: bindings inside if/else, cases,
// loops or closures): every reaching binding that holds obj is
// conditional, directly or through the bindings it reads.
func (l *goLinker) reachCond(at goAt, e *facts.GoExpr, obj string, seen map[string]bool) bool {
	if e == nil {
		return false
	}
	switch e.Kind {
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		if n.kind != "local" {
			return false
		}
		if c, ok := l.oapiReachCond(n, e.Name, obj); ok {
			return c
		}
		reach, _ := reaching(n.rec.fn, l.writes(n, e.Name), at)
		found, all := false, true
		for _, j := range reach {
			key := fmt.Sprintf("%s/b%d/%s", n.rec.key, j, e.Name)
			if seen[key] {
				continue
			}
			if !goHas(l.bindingValue(n.rec, j, e.Name).objects, obj) {
				continue
			}
			found = true
			bd := n.rec.fn.Bindings[j]
			bat := goAt{n.rec, bd.Block, goStart(bd.Span)}
			c := conditional(bat)
			if !c && len(bd.Values) == len(bd.Names) {
				seen[key] = true
				c = l.reachCond(bat, &bd.Values[hasName(bd.Names, e.Name)], obj, seen)
				delete(seen, key)
			}
			all = all && c
		}
		return found && all
	case facts.GoUnary:
		return l.reachCond(at, e.Object, obj, seen)
	case facts.GoSelector:
		if e.Name == "ServeHTTP" {
			return l.reachCond(at, e.Object, obj, seen)
		}
	case facts.GoCall:
		if e.Callee != nil && e.Callee.Kind == facts.GoSelector && e.Callee.Name == "Handler" && len(e.Args) == 1 {
			if imp, ok := l.importOf(at, e.Callee.Object); ok && imp.framework == goNetHTTP {
				return l.reachCond(at, &e.Args[0], obj, seen)
			}
		}
		if param, _, _, ok := l.wrapperCall(at, e); ok {
			return l.reachCond(at, &e.Args[param], obj, seen)
		}
	}
	return false
}

func goHas(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// wrappedObjects lists the router objects e holds only inside another call
// or a closure (GR-C4): the arguments of a call whose value is no proven
// router (the returns of a module function it calls included), the names
// a function literal uses, through the bindings that may reach a name.
// inside reports that e itself is already inside such a call; site names
// e's syntax position (object identities, as eval). The calls visited are
// marked read, so stripPrefixes does not read them again.
func (l *goLinker) wrappedObjects(at goAt, e *facts.GoExpr, site string, inside bool, seen map[string]bool, out map[string]bool) {
	if e == nil {
		return
	}
	add := func(v goValue) {
		if inside {
			for _, id := range v.objects {
				out[id] = true
			}
		}
	}
	switch e.Kind {
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		switch n.kind {
		case "local":
			reach, _ := reaching(n.rec.fn, l.writes(n, e.Name), at)
			for _, j := range reach {
				key := fmt.Sprintf("%s/b%d/%s/%v", n.rec.key, j, e.Name, inside)
				if seen[key] {
					continue
				}
				seen[key] = true
				bd := n.rec.fn.Bindings[j]
				add(l.bindingValue(n.rec, j, e.Name))
				if len(bd.Values) == len(bd.Names) {
					l.wrappedObjects(goAt{n.rec, bd.Block, goStart(bd.Span)}, &bd.Values[hasName(bd.Names, e.Name)], fmt.Sprintf("%s/b%d/%s", n.rec.key, j, e.Name), inside, seen, out)
				}
			}
		case "param":
			add(l.name(at, e.Name))
		}
	case facts.GoUnary:
		l.wrappedObjects(at, e.Object, site+"/&", inside, seen, out)
	case facts.GoSelector:
		if e.Name == "ServeHTTP" {
			l.wrappedObjects(at, e.Object, site+"/o", inside, seen, out)
		}
	case facts.GoCall:
		l.composed[e] = true
		if param, ok := l.passThrough(at, e, site); ok {
			// GR-O7: the wrapper holds its handler argument as
			// http.Handler(x) does; its other arguments are inside a call.
			for i := range e.Args {
				l.wrappedObjects(at, &e.Args[i], fmt.Sprintf("%s/a%d", site, i), inside || i != param, seen, out)
			}
			return
		}
		v := l.eval(at, e, site)
		if v.proven() {
			add(v)
			return
		}
		if e.Callee != nil && e.Callee.Kind == facts.GoSelector && e.Callee.Name == "Handler" && len(e.Args) == 1 {
			if imp, ok := l.importOf(at, e.Callee.Object); ok && imp.framework == goNetHTTP {
				l.wrappedObjects(at, &e.Args[0], site+"/a0", inside, seen, out)
				return
			}
		}
		for i := range e.Args {
			l.wrappedObjects(at, &e.Args[i], fmt.Sprintf("%s/a%d", site, i), true, seen, out)
		}
		rec := l.calledRecord(at, e)
		if rec == nil {
			// A method of a call result (GR-O5's factory_method_chain)
			// whose receiver type the call's declared result states:
			// the objects it returns reach this position unproven.
			rec = l.chainRecord(at, e)
		}
		if rec != nil {
			key := rec.key + "/returns"
			if !seen[key] {
				seen[key] = true
				for k, ret := range rec.fn.Returns {
					for i := range ret.Values {
						l.wrappedObjects(goAt{rec, ret.Block, goStart(ret.Span)}, &ret.Values[i], fmt.Sprintf("%s/r%d/%d", rec.key, k, i), true, seen, out)
					}
				}
			}
		}
	case facts.GoComposite:
		for i := range e.Elements {
			l.wrappedObjects(at, &e.Elements[i].Value, fmt.Sprintf("%s/e%d", site, i), true, seen, out)
		}
	case facts.GoFuncLit:
		for _, name := range l.literalNames(at, e) {
			id := facts.GoExpr{Kind: facts.GoIdentifier, Name: name}
			for _, o := range l.eval(at, &id, site+"/u"+name).objects {
				out[o] = true
			}
		}
	}
}

// literalNames are the names a function literal uses: its Uses, or for a
// captured literal the identifiers of its record's kept syntax.
func (l *goLinker) literalNames(at goAt, e *facts.GoExpr) []string {
	if e.Func == 0 || e.Func > len(l.records[at.rec.file]) {
		return e.Uses
	}
	rec := l.records[at.rec.file][e.Func-1]
	names := map[string]bool{}
	var walk func(x *facts.GoExpr)
	walk = func(x *facts.GoExpr) {
		if x == nil {
			return
		}
		if x.Kind == facts.GoIdentifier {
			names[x.Name] = true
		}
		if x.Kind == facts.GoFuncLit {
			for _, n := range l.literalNames(goAt{rec: rec}, x) {
				names[n] = true
			}
		}
		walk(x.Object)
		walk(x.Callee)
		walk(x.Left)
		walk(x.Right)
		for i := range x.Args {
			walk(&x.Args[i])
		}
		for i := range x.Elements {
			walk(&x.Elements[i].Value)
		}
	}
	for i := range rec.fn.Calls {
		walk(&rec.fn.Calls[i].Call)
	}
	for _, bd := range rec.fn.Bindings {
		for i := range bd.Values {
			walk(&bd.Values[i])
		}
	}
	for _, ret := range rec.fn.Returns {
		for i := range ret.Values {
			walk(&ret.Values[i])
		}
	}
	var out []string
	for n := range names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// calledRecord is the module function or method record a call resolves to
// (the factory rules of GR-O5), nil otherwise.
func (l *goLinker) calledRecord(at goAt, e *facts.GoExpr) *goRecord {
	c := e.Callee
	if c == nil {
		return nil
	}
	switch c.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, c.Name).kind == "func" {
			return l.funcRecord(l.ownFiles(at.rec.file), c.Name)
		}
	case facts.GoSelector:
		if c.Object == nil {
			return nil
		}
		if imp, ok := l.importOf(at, c.Object); ok {
			if imp.files != nil {
				return l.funcRecord(imp.files, c.Name)
			}
			return nil
		}
		if c.Object.Kind != facts.GoCall {
			return l.methodRecord(at, c.Object, c.Name)
		}
	}
	return nil
}

// chainRecord is the method record of a call x.f(...).M(...) whose inner
// call f or pkg.F declares one named result type T (GR-H2's typing of a
// call result): the method T.M. Its value stays refused
// (factory_method_chain); wrappedObjects reads its returns so that the
// routers it returns are no roots.
func (l *goLinker) chainRecord(at goAt, e *facts.GoExpr) *goRecord {
	c := e.Callee
	if c == nil || c.Kind != facts.GoSelector || c.Object == nil || c.Object.Kind != facts.GoCall || c.Object.Callee == nil {
		return nil
	}
	t, file := l.signature(at, c.Object.Callee)
	if t == nil {
		return nil
	}
	files, typ := l.typeFiles(file, t)
	if typ == "" {
		return nil
	}
	return l.funcRecord(files, typ+"."+c.Name)
}

// markWrapped records the objects h holds only inside another call or a
// closure at a composition position (GR-C4): none of them is a root. A
// first find at a site names it in route_mount_unresolved; the caller
// counts wrapped_router. It reports whether h holds any.
func (l *goLinker) markWrapped(at goAt, h *facts.GoExpr, file, site string, span contract.Span) bool {
	return l.markWrappedText(at, h, goArgText(*h), file, site, span)
}

// markWrappedText is markWrapped naming the expression text.
func (l *goLinker) markWrappedText(at goAt, h *facts.GoExpr, text, file, site string, span contract.Span) bool {
	out := map[string]bool{}
	l.wrappedObjects(at, h, site+"/h", false, map[string]bool{}, out)
	if len(out) == 0 {
		return false
	}
	for id := range out {
		if _, ok := l.wrappedAt[id]; !ok {
			l.wrappedAt[id] = site
		}
	}
	l.hopText(at, site, text, text+" (router behind another call or a closure)", span, upFormWrapped)
	if !l.counted[site+"\x00"+goWrappedRouter] {
		l.diagnostic(file, "route_mount_unresolved", text+" (router behind another call or a closure; no path is claimed through it)", span)
	}
	return true
}

// handlerRouters reads a route's handler argument: router objects it holds
// directly are used as a handler no composition reads (no root), and
// objects it holds only inside another call or a closure are wrapped
// (GR-C4, counted).
func (l *goLinker) handlerRouters(at goAt, h *facts.GoExpr, file, site string, span contract.Span) {
	if h.Kind != facts.GoFuncLit {
		for _, id := range l.eval(at, h, site+"/h").objects {
			l.handlers[id] = true
		}
	}
	if l.markWrapped(at, h, file, site, span) {
		l.count(file, site, goWrappedRouter)
	}
}

// stripPrefixes reads the http.StripPrefix calls of a record that no
// GR-C3 composition reads (a handler served by http.Server or
// ListenAndServe, a binding): a router they hold serves under a prefix no
// rule composes, so it is no root (wrapped_router, counted).
func (l *goLinker) stripPrefixes(rec *goRecord) {
	fn := rec.fn
	visit := func(at goAt, span contract.Span, e *facts.GoExpr, site string) {
		var walk func(e *facts.GoExpr, site string)
		walk = func(e *facts.GoExpr, site string) {
			if e == nil {
				return
			}
			if e.Kind == facts.GoCall && !l.composed[e] && e.Callee != nil && e.Callee.Kind == facts.GoSelector && e.Callee.Name == "StripPrefix" && len(e.Args) == 2 {
				if imp, ok := l.importOf(at, e.Callee.Object); ok && imp.framework == goNetHTTP && l.markWrappedText(at, e, "http.StripPrefix(...)", at.rec.file, site, span) {
					l.count(at.rec.file, site, goWrappedRouter)
				}
			}
			walk(e.Object, site+"/o")
			walk(e.Callee, site+"/f")
			for i := range e.Args {
				walk(&e.Args[i], fmt.Sprintf("%s/a%d", site, i))
			}
			for i := range e.Elements {
				walk(&e.Elements[i].Value, fmt.Sprintf("%s/e%d", site, i))
			}
		}
		walk(e, site)
	}
	for j, bd := range fn.Bindings {
		for i := range bd.Values {
			visit(goAt{rec, bd.Block, goStart(bd.Span)}, bd.Span, &bd.Values[i], fmt.Sprintf("%s/sb%d/%d", rec.key, j, i))
		}
	}
	for k, ret := range fn.Returns {
		for i := range ret.Values {
			visit(goAt{rec, ret.Block, goStart(ret.Span)}, ret.Span, &ret.Values[i], fmt.Sprintf("%s/sr%d/%d", rec.key, k, i))
		}
	}
	for k := range fn.Calls {
		c := &fn.Calls[k]
		visit(goAt{rec, c.Block, goStart(c.Span)}, c.Span, &c.Call, fmt.Sprintf("%s/c%d", rec.key, k))
	}
}

// graphEdges builds the composition graph's edges: the children of chi Route,
// Group and With and gin Group (GR-O1-GR-O3, GR-C1, GR-C2), the GR-O4
// parameter bindings and the chi Mount and net/http compositions (GR-C1,
// GR-C3). A prefix that is not constant blocks the edge
// (route_prefix_unresolved), as does a prefix chi rejects (invalid_pattern)
// or one with a regexp that can match '/' (regexp_param).
func (l *goLinker) graphEdges() []goEdge {
	var out []goEdge
	reported := map[string]bool{}
	prefixDiag := func(file, text string, span contract.Span, site string) {
		if !reported[site] {
			reported[site] = true
			l.diagnostic(file, "route_prefix_unresolved", text+" (prefix is not a constant; no path is claimed through it)", span)
		}
	}
	var ids []string
	for id := range l.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := l.objects[id]
		if o.parent == "" {
			continue
		}
		e := goEdge{parent: o.parent, child: id, kind: "same", resolution: "literal", conditional: o.conditional, file: o.file}
		if o.prefix != nil {
			site := id[:strings.Index(id, "<")]
			span := l.callSpan(o.at, o.expr)
			text, literal, ok := l.constant(o.at, o.prefix)
			switch {
			case !ok:
				e.blocked = "route_prefix_unresolved"
				prefixDiag(o.file, goArgText(*o.prefix), span, site)
				e.hop = l.hopAt(o.at, site, o.expr, span, "route_prefix_unresolved")
				e.hop.form = upFormRoute
				if o.framework == goGin {
					e.hop.form = upFormGroup
				}
				e.unknownPrefix = true
			case o.framework == goChi:
				e.kind, e.prefix = "chi", text
				e.blocked = l.chiPrefix(o.file, site, text, goArgText(*o.prefix), span)
			default:
				e.kind, e.prefix = "gin", text
			}
			if ok && !literal {
				e.resolution = "constant"
			}
		}
		out = append(out, e)
	}
	for _, b := range l.bindings {
		out = append(out, goEdge{parent: b.object, child: b.param, kind: "same", resolution: "literal", conditional: b.conditional, file: b.file})
	}
	for i, c := range l.compositions {
		file := c.at.rec.file
		cond := conditional(c.at)
		e := goEdge{kind: "chi", resolution: "literal", file: file, methods: c.methods, at: c.at}
		if l.refused[i] {
			e.blocked = goInvalidPattern
		}
		if c.kind == "mux" || c.kind == "muxchi" {
			e.kind = c.kind
			e.shadows = c.shadows
			outer, literal, ok := l.constant(c.at, c.outer)
			switch {
			case !ok:
				e.blocked = "route_prefix_unresolved"
				e.unknownOuter = true
				prefixDiag(file, goArgText(*c.outer), c.call.Span, c.site)
			case l.module(file).legacy:
				l.count(file, c.site, goLegacyMuxPatterns)
				e.blocked = goLegacyMuxPatterns
			default:
				if err := goMuxPattern(outer); err != nil {
					l.count(file, c.site, goInvalidPattern)
					l.diagnostic(file, "route_invalid_pattern", fmt.Sprintf("%q (%v)", outer, err), c.call.Span)
					e.blocked = goInvalidPattern
				}
				e.outer = outer
				if !literal {
					e.resolution = "constant"
				}
			}
		}
		syntax := false
		if c.prefix != nil {
			text, literal, ok := l.constant(c.at, c.prefix)
			switch {
			case !ok:
				if e.blocked == "" {
					e.blocked = "route_prefix_unresolved"
				}
				e.unknownPrefix = true
				prefixDiag(file, goArgText(*c.prefix), c.call.Span, c.site)
			case c.kind == "mount":
				if b := l.chiPrefix(file, c.site, text, goArgText(*c.prefix), c.call.Span); b != "" && e.blocked == "" {
					e.blocked = b
				}
			case strings.ContainsAny(text, "{}"):
				// StripPrefix strips the literal text; braces would read
				// as wildcards in the composed pattern.
				if e.blocked == "" {
					e.blocked = "route_prefix_unresolved"
				}
				e.unknownPrefix = true
				syntax = true
				prefixDiag(file, goArgText(*c.prefix), c.call.Span, c.site)
			}
			e.prefix = text
			if ok && !literal {
				e.resolution = "constant"
			}
		}
		if c.unproven && e.blocked == "" {
			e.blocked = "route_mount_unresolved"
			l.diagnostic(file, "route_mount_unresolved", goArgText(*c.value)+" (a binding of a value no rule proves a router reaches the composition; no path is claimed through it)", c.call.Span)
		}
		if e.unknownOuter && e.unknownPrefix && !syntax {
			e.outerTail, e.tailKnown = l.outerTail(c.at, c.outer, c.prefix)
		}
		if e.blocked == "route_prefix_unresolved" || e.blocked == "route_mount_unresolved" {
			e.hop = l.hopAt(c.at, c.site, &c.call.Call, c.call.Span, e.blocked)
			switch {
			case e.blocked == "route_mount_unresolved":
				e.hop.form = upFormBinding
			case c.kind == "mount":
				e.hop.form = upFormMount
			case e.unknownPrefix:
				e.hop.form = upFormStrip
			default:
				e.hop.form = upFormOuter
			}
			e.hop.syntax = e.hop.syntax || syntax
		}
		for _, p := range c.parents {
			for _, ch := range c.children {
				x := e
				x.parent, x.child = p, ch
				x.conditional = cond || c.alternative || l.reachCond(c.at, c.value, ch, map[string]bool{})
				out = append(out, x)
			}
		}
	}
	return out
}

// chiPrefix checks a chi Route or Mount prefix: chi panics on one that
// does not start with '/' (invalid_pattern), and a regexp parameter that
// can match '/' refuses the path (regexp_param). It returns the refusal.
func (l *goLinker) chiPrefix(file, site, prefix, text string, span contract.Span) string {
	if !strings.HasPrefix(prefix, "/") {
		l.count(file, site, goInvalidPattern)
		l.diagnostic(file, "route_invalid_pattern", text, span)
		return goInvalidPattern
	}
	if goChiRegexpSlash(prefix) {
		l.count(file, site, goRegexpParam)
		return goRegexpParam
	}
	return ""
}

// callSpan is the span of the listed call e read at at (a child's creation
// call: the nearest listed call at or after the reading position with e's
// tree, as canonical), or an empty span at the position.
func (l *goLinker) callSpan(at goAt, e *facts.GoExpr) contract.Span {
	if site := l.canonical(at, e, ""); site != "" {
		var k int
		if _, err := fmt.Sscanf(site[strings.LastIndex(site, "/c")+2:], "%d", &k); err == nil && k < len(at.rec.fn.Calls) {
			return at.rec.fn.Calls[k].Span
		}
	}
	return contract.Span{StartLine: at.pos.line, StartColumn: at.pos.col, EndLine: at.pos.line, EndColumn: at.pos.col}
}

// goGraph is the composition graph of one link (GR-C4): objects, resolved
// and blocked edges, roots and the chains reaching each object.
type goGraph struct {
	edges  []goEdge
	out    map[string][]goEdge
	in     map[string][]goEdge
	roots  []string
	chains map[string][]goChain
	// starts are each root's serving positions (GR-C6); none: the root
	// is served at its own paths.
	starts map[string][]goServe
}

// compose builds the composition graph and the chains from every root
// (GR-C4): a root is a constructor, zero-value or default mux object that
// no composition consumes, that is not wrapped or stored into a composite
// literal and not used directly as a handler. Each root's traversal is
// isolated: a failure becomes a warning and the other roots still compose.
func (l *goLinker) compose() *goGraph {
	g := &goGraph{out: map[string][]goEdge{}, in: map[string][]goEdge{}, chains: map[string][]goChain{}, starts: map[string][]goServe{}}
	g.edges = l.graphEdges()
	consumed := map[string]bool{}
	for _, e := range g.edges {
		g.out[e.parent] = append(g.out[e.parent], e)
		g.in[e.child] = append(g.in[e.child], e)
	}
	for _, c := range l.compositions {
		for _, ch := range c.children {
			consumed[ch] = true
		}
	}
	l.propagateWrapped()
	l.propagateEscapes()
	var ids []string
	for id := range l.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := l.objects[id]
		if o.kind != "ctor" && o.kind != "zero" && o.kind != "default" {
			continue
		}
		if consumed[id] || len(o.escapes) > 0 || l.handlers[id] || l.wrappedAt[id] != "" {
			continue
		}
		starts, blocked := l.serveStarts(id)
		if blocked {
			// Every serving position blocks the path (a StripPrefix
			// prefix no rule reads): no root.
			l.serveBlocked[id] = true
			continue
		}
		g.roots = append(g.roots, id)
		g.starts[id] = starts
	}
	for _, root := range g.roots {
		l.safeRoot(g, root)
	}
	return g
}

// propagateWrapped marks the objects bound to a wrapped parameter wrapped
// too: the parameter carries them into the wrapper.
func (l *goLinker) propagateWrapped() {
	for changed := true; changed; {
		changed = false
		for _, b := range l.bindings {
			if site := l.wrappedAt[b.param]; site != "" && l.wrappedAt[b.object] == "" {
				l.wrappedAt[b.object] = site
				changed = true
			}
		}
	}
}

// safeRoot composes the chains of one root, isolated.
func (l *goLinker) safeRoot(g *goGraph, root string) {
	o := l.objects[root]
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: o.file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go route composition from %s failed: %v", o.file, root, v)})
		}
	}()
	if goTestFail != nil {
		goTestFail("root", o.file)
	}
	start := goChain{resolution: "literal", scope: l.scope(o.file), root: root, owner: root}
	if o.framework == goGin {
		// gin's engine group has base path "/" (PS-GR-1).
		start.raw = "/"
	}
	l.tag("GR-C4")
	starts := []goChain{start}
	if served := g.starts[root]; len(served) > 0 {
		starts = nil
		for _, s := range served {
			c := start
			c.strip, c.assume, c.conditional, c.resolution = s.strip, s.wrappers, s.conditional, s.resolution
			c.methods = s.methods
			c.scope = chainScope(c.scope, l.scope(s.file))
			for _, f := range s.via {
				c.scope = chainScope(c.scope, l.scope(f))
			}
			if s.strip != "" {
				l.tag("GR-C6")
				l.tag("GR-C6/`http.StripPrefix(p, x)`")
			}
			if len(s.wrappers) > 0 {
				l.tag("GR-C6")
				l.tag("GR-C6/wrapper call")
			}
			starts = append(starts, c)
		}
	}
	l.walkChains(g, g.chains, root, starts)
}

// walkChains composes the chains from root through every resolved edge
// into chains (per-chain cycle guards): the real roots of safeRoot, and the
// hop objects of unresolved mounts (mountStops), whose chains are relative
// to the hop.
func (l *goLinker) walkChains(g *goGraph, chains map[string][]goChain, root string, starts []goChain) {
	visited := map[string]bool{}
	var visit func(id string, c goChain, onChain map[string]bool)
	visit = func(id string, c goChain, onChain map[string]bool) {
		key := id + "\x00" + c.key()
		if visited[key] {
			return
		}
		visited[key] = true
		chains[id] = append(chains[id], c)
		onChain[id] = true
		defer delete(onChain, id)
		for _, e := range g.out[id] {
			if e.blocked != "" || onChain[e.child] {
				continue
			}
			if next, ok := l.step(g, c, e); ok {
				visit(e.child, next, onChain)
			}
		}
	}
	for _, c := range starts {
		visit(root, c, map[string]bool{})
	}
}

// step composes chain c through the resolved edge e (GR-C1, GR-C2, GR-C3,
// GR-C7); false when the edge composes no path from c.
func (l *goLinker) step(g *goGraph, c goChain, e goEdge) (goChain, bool) {
	next := goChain{raw: c.raw, hops: c.hops, conditional: c.conditional || e.conditional, resolution: weaker(c.resolution, e.resolution), scope: chainScope(c.scope, l.scope(e.file)), strip: c.strip, assume: c.assume, root: c.root, gate: c.gate, methods: goUnion(c.methods, e.methods), owner: c.owner, mounts: c.mounts, bare: c.bare}
	switch e.kind {
	case "chi":
		next.raw = c.raw + strings.TrimSuffix(e.prefix, "/")
		next.mounts = append(append([]goMountLevel(nil), c.mounts...), goMountLevel{tree: e.parent, base: c.raw})
		next.bare = e.prefix != "" && !strings.HasSuffix(e.prefix, "/")
		l.tag("GR-C1")
	case "gin":
		next.raw = goJoinPaths(c.raw, e.prefix)
		next.bare = false
		l.tag("GR-C2")
	case "mux":
		next.hops = append(append([]goHop(nil), c.hops...), goHop{outer: e.outer, prefix: e.prefix, gate: l.gate(g, c.owner), at: e.at})
		next.owner = e.child
		next.bare = false
	case "muxchi":
		// GR-C7: only from a root ServeMux (no net/http hop
		// before), and only "/": the chi paths are unchanged.
		if len(c.hops) > 0 || c.gate != nil || e.outer != "/" {
			return goChain{}, false
		}
		next.gate = &goGateHop{id: e.parent + ">" + e.child, gate: l.gate(g, c.root), shadows: e.shadows}
	}
	return next, true
}

// scope is a file's test scope.
func (l *goLinker) scope(file string) testscope.Scope {
	if testscope.Classify(file, l.rules) == testscope.Test {
		return testscope.Test
	}
	return ""
}

// goJoinPaths is gin's joinPaths (utils.go): path.Join keeping a trailing
// slash of the relative path; an empty relative path is the base.
func goJoinPaths(base, rel string) string {
	if rel == "" {
		return base
	}
	final := path.Join(base, rel)
	if strings.HasSuffix(rel, "/") && !strings.HasSuffix(final, "/") {
		return final + "/"
	}
	return final
}

// goComposeMux composes a net/http registration pattern through the hops
// of a chain, innermost first (GR-C3): each hop's composed pattern is the
// StripPrefix prefix before the inner pattern's path, with the inner
// method and host, and is claimed only when the hop's outer pattern
// matches every request of it (method, host and path: equivalent or more
// general by net/http's pattern relations). It returns the composed path.
func goComposeMux(pattern string, hops []goHop) (string, bool) {
	cur, err := goParseMuxPattern(pattern)
	if err != nil {
		return "", false
	}
	_, _, curPath, _ := goSplitPattern(pattern)
	for i := len(hops) - 1; i >= 0; i-- {
		h := hops[i]
		outer, err := goParseMuxPattern(h.outer)
		if err != nil {
			return "", false
		}
		if h.prefix != "" && !strings.HasPrefix(h.prefix, "/") {
			// No request path reaches StripPrefix without its prefix.
			return "", false
		}
		curPath = h.prefix + curPath
		if strings.Contains(curPath, "//") {
			// The outer mux cleans such a path and redirects: the
			// composed path is never dispatched through this hop.
			return "", false
		}
		text := curPath
		if cur.host != "" {
			text = cur.host + text
		}
		if cur.method != "" {
			text = cur.method + " " + text
		}
		composed, err := goParseMuxPattern(text)
		if err != nil {
			return "", false
		}
		if outer.host != "" && outer.host != composed.host {
			return "", false
		}
		if rel := outer.comparePathsAndMethods(composed); rel != goEquivalent && rel != goMoreGeneral {
			return "", false
		}
		cur = composed
	}
	return curPath, true
}

// paths composes a registration's path entries from the chains reaching
// its receiver objects (GR-C1-GR-C5, GR-P2), deduplicated by template and
// trailing slash: unconditional when any chain is, the strongest
// resolution, scope test only when every chain is test-scope.
func (l *goLinker) paths(g *goGraph, reg goRegistration) []goPathEntry {
	if goTestFail != nil {
		goTestFail("paths", reg.file)
	}
	entries := []goPathEntry{}
	if reg.noPath != "" {
		return entries
	}
	at := goAt{reg.record, reg.call.Block, goStart(reg.call.Span)}
	unique := map[fastapiPathKey]int{}
	regScope := l.scope(reg.file)
	for _, id := range reg.objects {
		carried := false
		if recv := reg.call.Call.Callee; recv != nil && recv.Object != nil {
			carried = l.reachCond(at, recv.Object, id, map[string]bool{})
		}
		for _, c := range g.chains[id] {
			if len(c.methods) > 0 && (reg.method == "ANY" || goHas(c.methods, reg.method)) {
				// Step GR8 (B4): a wrapper or forwarding closure on the
				// chain answers this method itself.
				l.refusedWhy[reg.identity] = "a wrapper or forwarding closure on the way answers " + strings.Join(c.methods, ", ") + " itself"
				continue
			}
			for _, bare := range l.forms(reg, c) {
				raw, ok := l.rawPath(reg, c, bare)
				if !ok {
					continue
				}
				template, trailing, ok := l.template(reg.framework, raw)
				if !ok {
					continue
				}
				p := goPathEntry{fastapiPathEntry: fastapiPathEntry{pathEntry: pathEntry{Template: template, Conditional: c.conditional || reg.conditional || carried, Resolution: weaker(c.resolution, reg.resolution), Scope: chainScope(c.scope, regScope)}, TrailingSlash: trailing}}
				if len(c.assume) > 0 {
					p.Assumptions = []string{"GR-V5"}
					p.ServedThrough = append([]string(nil), c.assume...)
				}
				key := fastapiPathKey{p.Template, p.TrailingSlash}
				if i, found := unique[key]; found {
					old := &entries[i]
					old.Conditional = old.Conditional && p.Conditional
					old.Scope = mergedScope(old.Scope, p.Scope)
					if strength(p.Resolution) > strength(old.Resolution) {
						old.Resolution = p.Resolution
					}
					// GR-V5 holds only when every chain to the path crosses
					// a wrapper; a direct chain claims the path without it.
					mergeEntryAssumptions(&old.pathEntry, p.pathEntry)
					continue
				}
				unique[key] = len(entries)
				entries = append(entries, p)
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Template != entries[j].Template {
			return entries[i].Template < entries[j].Template
		}
		return !entries[i].TrailingSlash && entries[j].TrailingSlash
	})
	for _, p := range entries {
		if p.Conditional || p.Scope == testscope.Test {
			l.tag("GR-C5")
			break
		}
	}
	return entries
}

// goUnion is the sorted union of two name lists.
func goUnion(a, b []string) []string {
	set := map[string]bool{}
	for _, x := range append(append([]string(nil), a...), b...) {
		set[x] = true
	}
	var out []string
	for x := range set {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

// rawPath is a registration's composed raw path through one chain, in its
// framework's syntax, after the chain's served StripPrefix prefix (GR-C6:
// the request path is the prefix followed by the path the inner router
// sees).
func (l *goLinker) rawPath(reg goRegistration, c goChain, bare bool) (string, bool) {
	p, ok := l.innerPath(reg, c, bare)
	if !ok || c.strip == "" {
		return p, ok
	}
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	return c.strip + p, true
}

// forms lists the raw path forms a registration claims through one chain:
// false for the composed path; also true, the bare form, for a chi "/"
// route whose chain's last Route or Mount prefix has no trailing slash
// (chi's Mount registers the bare prefix too, so the route answers both
// prefix and prefix/; row GR-C1).
func (l *goLinker) forms(reg goRegistration, c goChain) []bool {
	if reg.framework == goChi && reg.name == "/" && c.bare {
		return []bool{false, true}
	}
	return []bool{false}
}

// goMountTaken is the refusal of a mounted chi route whose path a route
// of the router it is mounted on always takes (chi matches the parent's
// static route first); row SH-S24 reads it too.
const goMountTaken = "a route of the router it is mounted on always takes this path"

// innerPath is a registration's composed raw path through one chain as
// the root router sees it; bare asks for the bare form of forms.
func (l *goLinker) innerPath(reg goRegistration, c goChain, bare bool) (string, bool) {
	switch reg.framework {
	case goChi:
		p := c.raw + reg.name
		if bare {
			p = c.raw
		}
		if !l.mountsAllow(reg, c, p) {
			if !bare {
				l.refusedWhy[reg.identity] = goMountTaken
			}
			return "", false
		}
		if c.gate != nil && !c.gate.allows(reg.method, p) {
			l.gateRefused[reg.identity] = true
			return "", false
		}
		if c.gate != nil {
			l.tag("GR-C7")
			l.gatePassed[reg.identity] = true
		}
		return p, true
	case goGin:
		p := reg.pattern
		if reg.static == "Static" || reg.static == "StaticFS" {
			// gin's Static and StaticFS register p/*filepath (PS-GR-2).
			p = path.Join(p, "/*filepath")
		}
		return goJoinPaths(c.raw, p), true
	}
	if len(c.hops) == 0 {
		return reg.name[strings.IndexByte(reg.name, '/'):], true
	}
	p, ok := goComposeMux(reg.pattern, c.hops)
	if ok && !goHopsAllow(reg.pattern, c.hops) {
		l.refusedWhy[reg.identity] = "another pattern of the outer ServeMux or its redirect may answer this path"
		return "", false
	}
	if ok {
		l.tag("GR-C3")
	}
	return p, ok
}

// explained reports that a registration's objects reach no root because of
// a refusal already reported (a blocked edge, a wrapped or escaped
// object): route_unmounted is then not added.
func (l *goLinker) explained(g *goGraph, objects []string) bool {
	seen := map[string]bool{}
	var up func(id string) bool
	up = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		if o := l.objects[id]; o != nil && (len(o.escapes) > 0 || l.wrappedAt[id] != "" || l.mountRefused[id] || l.serveBlocked[id]) {
			return true
		}
		for _, e := range g.in[id] {
			if e.blocked != "" || up(e.parent) {
				return true
			}
		}
		return false
	}
	for _, id := range objects {
		if up(id) {
			return true
		}
	}
	return false
}

// goRouteGraph is the composition graph of one link in a plain form, for
// tests and the T023 oracle, which rebuilds the registration
// tree in the real routers from it, never from corpus code: the router
// objects, the edges (resolved and blocked) and the registrations.
type goRouteGraph struct {
	Objects       []goGraphObject       `json:"objects"`
	Edges         []goGraphEdge         `json:"edges"`
	Registrations []goGraphRegistration `json:"registrations"`
}

// goGraphObject is one router object.
type goGraphObject struct {
	ID        string `json:"id"`
	Framework string `json:"framework"`
	// Kind is ctor, zero, default, param, route, group, with or ginGroup.
	Kind string `json:"kind"`
	File string `json:"file"`
	Root bool   `json:"root"`
	// Served are a root's serving positions (GR-C6): the StripPrefix
	// prefix and the wrappers of each; none for a root served at its own
	// paths.
	Served []goGraphServe `json:"served,omitempty"`
}

// goGraphServe is one serving position of a root.
type goGraphServe struct {
	Strip    string   `json:"strip,omitempty"`
	Wrappers []string `json:"wrappers,omitempty"`
}

// goGraphChain is one way a root reaches a registration: the root, the
// served StripPrefix prefix, the raw path the root router sees (framework
// syntax: the oracle builds its sample request from it) and the path
// entry's template.
type goGraphChain struct {
	Root          string `json:"root"`
	Strip         string `json:"strip,omitempty"`
	Raw           string `json:"raw"`
	Template      string `json:"template"`
	TrailingSlash bool   `json:"trailing_slash,omitempty"`
	// Bare marks the bare prefix form of a chi "/" route (row GR-C1): chi
	// answers it through Mount's stub registration, which chi.Walk does
	// not list, so the oracle dispatches it but leaves it out of the
	// listing comparison.
	Bare bool `json:"bare,omitempty"`
}

// goGraphEdge is one composition step: Kind same, chi, gin or mux (see
// goEdge); Blocked names the refusal of an edge no path crosses.
type goGraphEdge struct {
	Parent      string `json:"parent"`
	Child       string `json:"child"`
	Kind        string `json:"kind"`
	Prefix      string `json:"prefix,omitempty"`
	Outer       string `json:"outer,omitempty"`
	Conditional bool   `json:"conditional,omitempty"`
	Blocked     string `json:"blocked,omitempty"`
	// Shadows are a muxchi edge's forwarding-closure shadows (GR-O8).
	Shadows []string `json:"shadows,omitempty"`
}

// goGraphRegistration is one route registration: its node identity, the
// objects it registers on, its method and pattern as written (with a
// net/http method and host), and the composed path entries.
type goGraphRegistration struct {
	Identity  string         `json:"identity"`
	File      string         `json:"file"`
	Line      int            `json:"line"`
	Framework string         `json:"framework"`
	Method    string         `json:"method"`
	Pattern   string         `json:"pattern"`
	Static    string         `json:"static,omitempty"`
	Objects   []string       `json:"objects"`
	Paths     []goPathEntry  `json:"paths"`
	Chains    []goGraphChain `json:"chains"`
}

// routeGraph is the composition graph of a finished link.
func (l *goLinker) routeGraph() goRouteGraph {
	out := goRouteGraph{Objects: []goGraphObject{}, Edges: []goGraphEdge{}, Registrations: []goGraphRegistration{}}
	g := l.graph
	if g == nil {
		return out
	}
	roots := map[string]bool{}
	for _, r := range g.roots {
		roots[r] = true
	}
	var ids []string
	for id := range l.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		o := l.objects[id]
		obj := goGraphObject{ID: id, Framework: o.framework, Kind: o.kind, File: o.file, Root: roots[id]}
		for _, s := range g.starts[id] {
			obj.Served = append(obj.Served, goGraphServe{Strip: s.strip, Wrappers: s.wrappers})
		}
		out.Objects = append(out.Objects, obj)
	}
	for _, e := range g.edges {
		out.Edges = append(out.Edges, goGraphEdge{Parent: e.parent, Child: e.child, Kind: e.kind, Prefix: e.prefix, Outer: e.outer, Conditional: e.conditional, Blocked: e.blocked, Shadows: e.shadows})
	}
	paths := map[string][]goPathEntry{}
	for _, n := range l.nodes {
		paths[n.Identity], _ = n.Attributes["paths"].([]goPathEntry)
	}
	for _, reg := range l.registrations {
		if reg.identity == "" {
			continue
		}
		gr := goGraphRegistration{Identity: reg.identity, File: reg.file, Line: reg.call.Span.StartLine, Framework: reg.framework, Method: reg.method, Pattern: reg.pattern, Static: reg.static, Objects: reg.objects, Paths: paths[reg.identity], Chains: []goGraphChain{}}
		if reg.noPath == "" {
			seen := map[goGraphChain]bool{}
			for _, id := range reg.objects {
				for _, c := range g.chains[id] {
					if len(c.methods) > 0 && (reg.method == "ANY" || goHas(c.methods, reg.method)) {
						// A method a wrapper or closure on the chain
						// answers itself claims no path.
						continue
					}
					for _, bare := range l.forms(reg, c) {
						inner, ok := l.innerPath(reg, c, bare)
						if !ok {
							continue
						}
						raw, ok := l.rawPath(reg, c, bare)
						if !ok {
							continue
						}
						template, trailing, ok := goTemplate(reg.framework, raw)
						if !ok {
							continue
						}
						ch := goGraphChain{Root: c.root, Strip: c.strip, Raw: inner, Template: template, TrailingSlash: trailing, Bare: bare}
						if !seen[ch] {
							seen[ch] = true
							gr.Chains = append(gr.Chains, ch)
						}
					}
				}
			}
			sort.Slice(gr.Chains, func(i, j int) bool {
				a, b := gr.Chains[i], gr.Chains[j]
				if a.Root != b.Root {
					return a.Root < b.Root
				}
				if a.Strip != b.Strip {
					return a.Strip < b.Strip
				}
				return a.Raw < b.Raw
			})
		}
		out.Registrations = append(out.Registrations, gr)
	}
	return out
}

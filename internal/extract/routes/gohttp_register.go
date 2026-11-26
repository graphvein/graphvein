package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Registrations (rows GR-R1-GR-R4), handlers and attributes (GR-H1-GR-H6)
// and the emitted route nodes.

// file reads one recognised file: the objects its bindings and returns
// make, the GR-O4 bindings of its plain calls, its registrations and the
// composite literals its router objects escape into.
func (l *goLinker) file(file string) {
	for _, rec := range l.records[file] {
		fn := rec.fn
		for j, bd := range fn.Bindings {
			if bd.Kind == facts.GoBindHandler {
				// x.Handler = h binds no name (a serving position, GR-C6).
				continue
			}
			for _, name := range bd.Names {
				if name != "" && name != "_" {
					l.bindingValue(rec, j, name)
				}
			}
		}
		for k, ret := range fn.Returns {
			for i := range ret.Values {
				l.eval(goAt{rec, ret.Block, goStart(ret.Span)}, &ret.Values[i], fmt.Sprintf("%s/r%d/%d", rec.key, k, i))
			}
		}
		for k := range fn.Calls {
			c := &fn.Calls[k]
			at := goAt{rec, c.Block, goStart(c.Span)}
			site := fmt.Sprintf("%s/c%d", rec.key, k)
			if c.Kind == facts.GoCallRouter {
				l.routerCall(at, c, site)
			} else {
				l.eval(at, &c.Call, site)
			}
		}
	}
}

// goRegistrationMethods are the method names that register routes, per
// framework (GR-R1-GR-R4).
var goRegistrationMethods = map[string]string{
	goNetHTTP: " Handle HandleFunc ",
	goChi:     " Get Post Put Patch Delete Head Options Connect Trace Handle HandleFunc Method MethodFunc ",
	goGin:     " GET POST PUT PATCH DELETE HEAD OPTIONS Any Handle Match Static StaticFS StaticFile StaticFileFS ",
}

func goRegisters(framework, name string) bool {
	return strings.Contains(goRegistrationMethods[framework], " "+name+" ")
}

// goRegistersAny reports a registration method of any framework (a
// refused receiver counts only for those).
func goRegistersAny(name string) bool {
	for fw := range goRegistrationMethods {
		if goRegisters(fw, name) {
			return true
		}
	}
	return false
}

// routerCall reads one router-named call: its receiver must hold proven
// router objects (GR-K2, GR-K3, GR-K4, GR-O*); a registration method on
// them registers routes, a Route, Group, With or Use call makes children.
func (l *goLinker) routerCall(at goAt, c *facts.GoRouteCall, site string) {
	e := &c.Call
	callee := e.Callee
	if callee == nil || callee.Kind != facts.GoSelector || callee.Object == nil {
		return
	}
	var recv goValue
	if imp, ok := l.importOf(at, callee.Object); ok {
		if imp.framework != goNetHTTP || callee.Name != "Handle" && callee.Name != "HandleFunc" {
			return
		}
		l.tag("GR-K2")
		l.tag("GR-K2/`http." + callee.Name + "`")
		recv = goObjects(l.defaultMux(at.rec.file))
	} else {
		recv = l.eval(at, callee.Object, site+"/o")
	}
	if len(recv.objects) > 0 && (!recv.proven() || l.framework(recv) == "") && (callee.Name == "Handle" || callee.Name == "HandleFunc") {
		// A Handle call on a receiver that may hold a ServeMux or
		// another value registers an unknown pattern on that mux
		// (GR-C7's siblings).
		for _, id := range recv.objects {
			if l.objects[id].framework == goNetHTTP {
				l.muxOpaque[id] = true
				l.muxOpaqueAt[id] = append(l.muxOpaqueAt[id], at)
			}
		}
	}
	if !recv.proven() || l.framework(recv) == "" {
		if recv.evidence && goRegistersAny(callee.Name) {
			form := recv.form
			if form == "" {
				form = goReceiverUnproven
			}
			l.count(at.rec.file, site, form)
		}
		return
	}
	if callee.Name == "Handle" || callee.Name == "HandleFunc" {
		// A ServeMux call that records no registration or composition
		// leaves its mux with an unknown pattern (GR-C7's siblings).
		before := len(l.registrations) + len(l.compositions)
		defer func() {
			if len(l.registrations)+len(l.compositions) == before {
				// A constant valid pattern (a wrapped or refused handler)
				// is still a known sibling; any other pattern
				// is unknown.
				p, _, known := "", false, false
				if len(c.Call.Args) == 2 && !l.module(at.rec.file).legacy {
					p, _, known = l.constant(at, &c.Call.Args[0])
					known = known && goMuxPattern(p) == nil
				}
				for _, id := range recv.objects {
					if l.objects[id].framework == goNetHTTP {
						if known {
							l.muxExtra[id] = append(l.muxExtra[id], goMuxExtra{p, at})
						} else {
							l.muxOpaque[id] = true
							l.muxOpaqueAt[id] = append(l.muxOpaqueAt[id], at)
						}
					}
				}
			}
		}()
	}
	fw := l.framework(recv)
	if fw == goChi && callee.Name == "Mount" {
		l.mount(at, c, site, recv)
		return
	}
	if !goRegisters(fw, callee.Name) {
		// Route, Group, With and Use make children; Mount and the rest
		// are composition or not modelled.
		l.call(at, e, site)
		return
	}
	l.register(at, c, site, fw, recv)
}

// register reads one registration call on proven objects of framework fw.
func (l *goLinker) register(at goAt, c *facts.GoRouteCall, site, fw string, recv goValue) {
	e := &c.Call
	name := e.Callee.Name
	file := at.rec.file
	args := e.Args
	reg := goRegistration{file: file, site: site, record: at.rec, call: c, framework: fw, objects: recv.objects, conditional: conditional(at)}
	for _, id := range recv.objects {
		if o := l.objects[id]; o.kind == "with" {
			reg.middleware = append([]string(nil), o.middleware...)
			break
		}
	}
	var methods []string
	patternArg, handlerArg := 0, 1
	switch fw {
	case goNetHTTP:
		if len(args) != 2 {
			return
		}
	case goChi:
		switch name {
		case "Handle", "HandleFunc":
		case "Method", "MethodFunc":
			if len(args) != 3 {
				return
			}
			m, ok := l.method(at, &args[0])
			if !ok {
				l.count(file, site, goDynamicMethod)
				l.diagnostic(file, "route_dynamic_methods", goArgText(args[0]), c.Span)
				return
			}
			m = strings.ToUpper(m)
			if !goChiMethods[m] {
				l.count(file, site, goInvalidPattern)
				l.diagnostic(file, "route_invalid_pattern", goArgText(args[0]), c.Span)
				return
			}
			methods = []string{m}
			patternArg, handlerArg = 1, 2
		default:
			methods = []string{strings.ToUpper(name)}
		}
		if len(args) != handlerArg+1 {
			return
		}
	case goGin:
		switch name {
		case "Handle":
			if len(args) < 2 {
				return
			}
			m, ok := l.method(at, &args[0])
			if !ok {
				l.count(file, site, goDynamicMethod)
				l.diagnostic(file, "route_dynamic_methods", goArgText(args[0]), c.Span)
				return
			}
			if !goGinMethod.MatchString(m) {
				l.count(file, site, goInvalidPattern)
				l.diagnostic(file, "route_invalid_pattern", goArgText(args[0]), c.Span)
				return
			}
			methods = []string{m}
			patternArg, handlerArg = 1, 2
		case "Match":
			if len(args) < 2 {
				return
			}
			list, ok := l.methodList(at, &args[0])
			if !ok {
				l.count(file, site, goDynamicMethod)
				l.diagnostic(file, "route_dynamic_methods", goArgText(args[0]), c.Span)
				return
			}
			for _, m := range list {
				if !goGinMethod.MatchString(m) {
					l.count(file, site, goInvalidPattern)
					l.diagnostic(file, "route_invalid_pattern", goArgText(args[0]), c.Span)
					return
				}
			}
			methods = list
			patternArg, handlerArg = 1, 2
		case "Any":
			methods = []string{"ANY"}
		case "Static", "StaticFS", "StaticFile", "StaticFileFS":
			methods = []string{"GET", "HEAD"}
			reg.static = name
			handlerArg = -1
		default:
			methods = []string{name}
		}
		if handlerArg >= 0 {
			if len(args) < handlerArg {
				return
			}
			// gin registers a route without handler arguments too: the
			// group's middleware chain serves it.
			handlerArg = len(args) - 1
			if handlerArg > patternArg {
				for _, a := range args[patternArg+1 : handlerArg] {
					reg.middleware = append(reg.middleware, goArgText(a))
				}
			}
		} else if len(args) < 2 {
			return
		}
	}
	if len(args) <= patternArg {
		return
	}
	if fw == goNetHTTP {
		// A handler that holds a ServeMux composes it (GR-C3);
		// one that holds a router only inside another call or a
		// closure is a wrapped router (GR-C4's refusal). Neither is a
		// route.
		if l.composes(at, c, site, recv) {
			return
		}
		if args[1].Kind != facts.GoFuncLit && l.wrapped(at, &args[1], site+"/h", map[string]bool{}) {
			l.markWrapped(at, &args[1], file, site, c.Span)
			l.handleHop(at, site, recv, &args[0], &args[1])
			l.count(file, site, goWrappedRouter)
			return
		}
	}
	// A generated server's registration reads its BaseURL from the
	// server's instances (GR-G1); every other pattern is GR-P1's.
	if !l.oapiRegistration(at, &reg, &args[patternArg]) {
		pattern, literal, ok := l.constant(at, &args[patternArg])
		if !ok {
			l.count(file, site, goDynamicPattern)
			l.diagnostic(file, "route_dynamic_pattern", goArgText(args[patternArg]), c.Span)
			return
		}
		l.tag("GR-P1")
		switch {
		case args[patternArg].Kind == facts.GoString:
			l.tag("GR-P1/string literal")
			reg.resolution = "literal"
		case args[patternArg].Kind == facts.GoBinary:
			l.tag("GR-P1/`+`")
			reg.resolution = "constant"
			if literal {
				reg.resolution = "literal"
			}
		default:
			l.tag("GR-P1/constant")
			reg.resolution = "constant"
		}
		reg.pattern, reg.name = pattern, pattern
	}
	pattern := reg.pattern
	if handlerArg > patternArg {
		reg.handler = &args[handlerArg]
	}
	switch fw {
	case goNetHTTP:
		if l.module(file).legacy {
			l.count(file, site, goLegacyMuxPatterns)
			return
		}
		if err := goMuxPattern(pattern); err != nil {
			l.count(file, site, goInvalidPattern)
			l.diagnostic(file, "route_invalid_pattern", fmt.Sprintf("%q (%v)", pattern, err), c.Span)
			return
		}
		method, host, _, rest := goSplitPattern(pattern)
		if method == "" {
			method = "ANY"
		}
		methods = []string{method}
		reg.host, reg.name = host, rest
		l.tag("GR-R1")
	case goChi:
		if (name == "Handle" || name == "HandleFunc") && strings.ContainsAny(pattern, " \t") {
			i := strings.IndexAny(pattern, " \t")
			m := strings.ToUpper(pattern[:i])
			reg.name = strings.TrimLeft(pattern[i+1:], " \t")
			if !goChiMethods[m] {
				l.count(file, site, goInvalidPattern)
				l.diagnostic(file, "route_invalid_pattern", goArgText(args[patternArg]), c.Span)
				return
			}
			methods = []string{m}
		} else if name == "Handle" || name == "HandleFunc" {
			methods = []string{"ANY"}
		}
		if !strings.HasPrefix(reg.name, "/") {
			l.count(file, site, goInvalidPattern)
			l.diagnostic(file, "route_invalid_pattern", goArgText(args[patternArg]), c.Span)
			return
		}
		if goChiRegexpSlash(reg.name) {
			l.count(file, site, goRegexpParam)
			reg.noPath = goRegexpParam
		}
		l.tag("GR-R2")
		switch name {
		case "Handle", "HandleFunc":
			l.tag("GR-R2/`Handle(p, h)`")
		case "Method", "MethodFunc":
			l.tag("GR-R2/`Method(m, p, h)`")
		default:
			l.tag("GR-R2/`Get/Post/Put/Patch/Delete/Head/Options/Connect/Trace(p, h)`")
		}
	case goGin:
		if reg.static != "" {
			if strings.ContainsAny(pattern, ":*") {
				l.count(file, site, goInvalidPattern)
				l.diagnostic(file, "route_invalid_pattern", goArgText(args[patternArg]), c.Span)
				return
			}
			l.tag("GR-R4")
			l.tag("GR-R4/`" + map[string]string{"Static": "Static(p, dir)", "StaticFS": "StaticFS(p, fs)", "StaticFile": "StaticFile(p, file)", "StaticFileFS": "StaticFileFS(p, file, fs)"}[name] + "`")
		} else {
			l.tag("GR-R3")
			switch name {
			case "Any":
				l.tag("GR-R3/`Any(p, h...)`")
			case "Handle":
				l.tag("GR-R3/`Handle(m, p, h...)`")
			case "Match":
				l.tag("GR-R3/`Match(ms, p, h...)`")
			default:
				l.tag("GR-R3/`GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS(p, h...)`")
			}
		}
	}
	if len(reg.middleware) > 0 && fw == goChi {
		l.tag("GR-O2")
	}
	if reg.handler != nil {
		l.handlerRouters(at, reg.handler, file, site, c.Span)
		if fw == goNetHTTP {
			l.handleHop(at, site, recv, &args[patternArg], reg.handler)
		}
	}
	for _, m := range methods {
		r := reg
		r.method = m
		r.order = len(l.registrations)
		l.registrations = append(l.registrations, r)
	}
}

// methodList reads gin Match's methods: a slice literal of string
// constants or http.Method* constants, distinct, in order.
func (l *goLinker) methodList(at goAt, e *facts.GoExpr) ([]string, bool) {
	if e.Kind != facts.GoComposite || e.Type == nil || e.Type.Kind != facts.GoSlice {
		return nil, false
	}
	var out []string
	seen := map[string]bool{}
	for _, el := range e.Elements {
		if el.Key != nil {
			return nil, false
		}
		m, ok := l.method(at, &el.Value)
		if !ok {
			return nil, false
		}
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out, len(out) > 0
}

// composes reads a net/http registration whose handler holds ServeMux
// objects (directly, as x.ServeHTTP, as http.Handler(x) or inside
// http.StripPrefix): a composition (GR-C3), not a route. It records the
// composition for step GR3's paths.
func (l *goLinker) composes(at goAt, c *facts.GoRouteCall, site string, recv goValue) bool {
	h := &c.Call.Args[1]
	inner := h
	var prefix *facts.GoExpr
	if h.Kind == facts.GoCall && h.Callee != nil && h.Callee.Kind == facts.GoSelector && len(h.Args) == 2 {
		if imp, ok := l.importOf(at, h.Callee.Object); ok && imp.framework == goNetHTTP && h.Callee.Name == "StripPrefix" {
			inner, prefix = &h.Args[1], &h.Args[0]
		}
	}
	var v goValue
	var fwd *facts.GoForward
	if inner.Kind == facts.GoFuncLit {
		if inner.Forward == nil || prefix != nil {
			return false
		}
		// GR-O8: a forwarding closure holds the router it serves.
		fv, ok := l.forwardValue(at, inner, site+"/h")
		if !ok {
			return false
		}
		v, fwd = fv, inner.Forward
		inner = &facts.GoExpr{Kind: facts.GoIdentifier, Name: fwd.Name}
	} else {
		innerSite := site + "/h"
		if prefix != nil {
			// The handler argument's own site is site/h (its
			// StripPrefix call); the router inside is its argument 1
			// (one site per syntax, or the memo answers for
			// the other expression).
			innerSite += "/a1"
		}
		v = copied(l.eval(at, inner, innerSite))
	}
	if len(v.objects) == 0 {
		return false
	}
	kind := "mux"
	switch l.framework(v) {
	case goNetHTTP:
		if fwd != nil {
			// A closure forwarding to a ServeMux is not modelled.
			return false
		}
	case goChi:
		// GR-C7: a chi router at the pattern "/" of a ServeMux.
		outer, _, ok := l.constant(at, &c.Call.Args[0])
		if !ok || outer != "/" || prefix != nil {
			return false
		}
		kind = "muxchi"
		if fwd != nil {
			l.tag("GR-C7/forwarding closure")
		} else {
			l.tag("GR-C7/`m.Handle(\"/\", x)`")
		}
	default:
		return false
	}
	if prefix != nil {
		l.composed[h] = true
	}
	comp := goComposition{kind: kind, parents: recv.objects, children: v.objects, unproven: v.unproven, alternative: v.alternative, at: at, call: c, site: site, outer: &c.Call.Args[0], prefix: prefix, value: inner}
	comp.methods = v.methods
	if fwd != nil {
		comp.shadows = fwd.Shadows
		comp.methods = goUnion(comp.methods, fwd.Methods)
	}
	l.compositions = append(l.compositions, comp)
	return true
}

// forwardValue is the value a forwarding closure serves (GR-O8): the
// router objects of its captured name, when that name is a local variable
// written exactly once (its declaration) or a parameter never assigned
// (the closure reads the variable when a request arrives, so a later write
// would change what it serves), every pass of the request keeps its path
// (GR-O7's request helpers), and the name holds proven chi router objects.
func (l *goLinker) forwardValue(at goAt, lit *facts.GoExpr, site string) (goValue, bool) {
	f := lit.Forward
	n := l.lookup(at, f.Name)
	switch n.kind {
	case "local":
		if w := l.writes(n, f.Name); len(w) != 1 || w[0] != n.decl {
			return goValue{}, false
		}
	case "param":
		if len(l.writes(n, f.Name)) != 0 {
			return goValue{}, false
		}
	default:
		return goValue{}, false
	}
	if !l.passesKeepPath(at.rec.file, "", f.Passes) || l.guardsDecide(at.rec.file, "", f.Guards) {
		return goValue{}, false
	}
	id := facts.GoExpr{Kind: facts.GoIdentifier, Name: f.Name}
	v := copied(l.eval(at, &id, site+"/f"))
	if !v.proven() || l.framework(v) != goChi {
		return goValue{}, false
	}
	l.tag("GR-O8")
	return v, true
}

// mount reads chi's r.Mount(p, x) on proven objects (GR-C1): x a proven
// chi router composes; a router only inside another call or a closure is
// wrapped_router (GR-C4); anything else, a router of another framework
// included, is mount_unproven_handler. Each refusal names the mount in
// route_mount_unresolved.
func (l *goLinker) mount(at goAt, c *facts.GoRouteCall, site string, recv goValue) {
	args := c.Call.Args
	if len(args) != 2 {
		return
	}
	file := at.rec.file
	v := copied(l.eval(at, &args[1], site+"/h"))
	if v.proven() && l.framework(v) == goChi {
		l.compositions = append(l.compositions, goComposition{kind: "mount", parents: recv.objects, children: v.objects, alternative: v.alternative, at: at, call: c, site: site, prefix: &args[0], value: &args[1], methods: v.methods})
		return
	}
	for _, id := range v.objects {
		l.handlers[id] = true
		l.mountRefused[id] = true
	}
	if len(v.objects) == 0 && l.markWrapped(at, &args[1], file, site, c.Span) {
		l.count(file, site, goWrappedRouter)
		return
	}
	l.count(file, site, goMountUnproven)
	l.diagnostic(file, "route_mount_unresolved", goArgText(args[1])+" (mount of a value no rule proves a chi router; no path is claimed through it)", c.Span)
}

// wrapped reports a value that holds a router object only inside another
// call (an argument, at any depth, through the bindings that may reach
// it) or a function literal (a name the literal uses): GR-C4's
// wrapped_router.
func (l *goLinker) wrapped(at goAt, e *facts.GoExpr, site string, seen map[string]bool) bool {
	holds := func(at goAt, e *facts.GoExpr, site string) bool {
		return len(l.eval(at, e, site).objects) > 0 || l.wrapped(at, e, site, seen)
	}
	switch e.Kind {
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		if n.kind != "local" {
			return false
		}
		writes := l.writes(n, e.Name)
		reach, _ := reaching(n.rec.fn, writes, at)
		for _, j := range reach {
			key := fmt.Sprintf("%s/b%d/%s", n.rec.key, j, e.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			bd := n.rec.fn.Bindings[j]
			i := hasName(bd.Names, e.Name)
			if len(bd.Values) != len(bd.Names) {
				continue
			}
			if l.wrapped(goAt{n.rec, bd.Block, goStart(bd.Span)}, &bd.Values[i], key, seen) {
				return true
			}
		}
	case facts.GoUnary:
		if e.Object != nil {
			return l.wrapped(at, e.Object, site+"/&", seen)
		}
	case facts.GoCall:
		for i := range e.Args {
			if holds(at, &e.Args[i], fmt.Sprintf("%s/a%d", site, i)) {
				return true
			}
		}
	case facts.GoComposite:
		for i := range e.Elements {
			if holds(at, &e.Elements[i].Value, fmt.Sprintf("%s/e%d", site, i)) {
				return true
			}
		}
	case facts.GoFuncLit:
		if e.Func != 0 {
			// A captured literal is a record of its own (its Uses are
			// read by the escape walk).
			break
		}
		for _, name := range e.Uses {
			id := facts.GoExpr{Kind: facts.GoIdentifier, Name: name}
			if len(l.eval(at, &id, site+"/u"+name).objects) > 0 {
				return true
			}
		}
	}
	return false
}

// handler resolves a registration's handler (GR-H1-GR-H5): the identity of
// a module function or of a method of a stated type, inline for a function
// literal, otherwise none.
func (l *goLinker) handler(at goAt, h *facts.GoExpr) (identity string, inline bool) {
	switch h.Kind {
	case facts.GoFuncLit:
		l.tag("GR-H4")
		return "", true
	case facts.GoCall:
		c := h.Callee
		if c != nil && c.Kind == facts.GoSelector && c.Name == "HandlerFunc" && len(h.Args) == 1 {
			if imp, ok := l.importOf(at, c.Object); ok && imp.framework == goNetHTTP {
				if id := l.handlerName(at, &h.Args[0]); id != "" {
					l.tag("GR-H3")
					return id, false
				}
			}
		}
		return "", false
	}
	return l.handlerName(at, h), false
}

// handlerName resolves f, pkg.F (GR-H1) or x.M with x's type stated by
// the syntax (GR-H2).
func (l *goLinker) handlerName(at goAt, h *facts.GoExpr) string {
	switch h.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, h.Name).kind != "func" {
			return ""
		}
		if id := l.callable(l.ownFiles(at.rec.file), h.Name, "function"); id != "" {
			l.tag("GR-H1")
			return id
		}
	case facts.GoSelector:
		if h.Object == nil {
			return ""
		}
		if imp, ok := l.importOf(at, h.Object); ok {
			if imp.files == nil {
				return ""
			}
			if id := l.callable(imp.files, h.Name, "function"); id != "" {
				l.tag("GR-H1")
				return id
			}
			return ""
		}
		t, file := l.typeOf(at, h.Object)
		if t == nil {
			return ""
		}
		files, typ := l.typeFiles(file, t)
		if typ == "" {
			return ""
		}
		if id := l.callable(files, typ+"."+h.Name, "method"); id != "" {
			l.tag("GR-H2")
			return id
		}
	}
	return ""
}

// owner is the node that contains a registration: the enclosing named
// function or method, else the file.
func (l *goLinker) owner(rec *goRecord) string {
	if rec.decl != nil {
		for _, n := range l.files[rec.file].Nodes {
			if (n.Kind == "function" || n.Kind == "method") && n.Span != nil && *n.Span == rec.decl.fn.Span {
				return n.Identity
			}
		}
	}
	return facts.ID("file", rec.file, rec.file)
}

// emit builds the route nodes, contains and exposes_route edges in
// registration order per file (GR-H6), with the paths composed from every
// root (GR-C1-GR-C5).
func (l *goLinker) emit() ([]contract.Node, []contract.Edge) {
	sort.SliceStable(l.registrations, func(i, j int) bool {
		a, b := l.registrations[i], l.registrations[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.call.Span.StartLine != b.call.Span.StartLine {
			return a.call.Span.StartLine < b.call.Span.StartLine
		}
		if a.call.Span.StartColumn != b.call.Span.StartColumn {
			return a.call.Span.StartColumn < b.call.Span.StartColumn
		}
		return a.order < b.order
	})
	l.conflicts()
	g := l.safeCompose()
	l.graph = g
	l.assignHopKeys()
	var nodes []contract.Node
	var edges []contract.Edge
	repeats := map[string]int{}
	// Every registration's identity and paths first: row SH-S24 compares
	// the paths of all chi routes before any shape is built.
	names := make([]string, len(l.registrations))
	allPaths := make([][]goPathEntry, len(l.registrations))
	for i := range l.registrations {
		reg := &l.registrations[i]
		if reg.noPath == goInvalidPattern {
			continue
		}
		q := reg.method + " " + reg.name
		key := reg.file + "#" + q
		repeats[key]++
		if repeats[key] > 1 {
			q += fmt.Sprintf("@%d", repeats[key])
		}
		names[i] = q
		reg.identity = facts.ID("route", reg.file, q)
		allPaths[i] = l.safePaths(g, *reg)
	}
	l.sharedChiPaths(g, allPaths)
	for i := range l.registrations {
		reg := &l.registrations[i]
		if reg.noPath == goInvalidPattern {
			continue
		}
		q := names[i]
		identity := reg.identity
		middleware := reg.middleware
		if middleware == nil {
			middleware = []string{}
		}
		paths := allPaths[i]
		attrs := map[string]any{"framework": reg.framework, "method": reg.method, "pattern": reg.pattern, "handler": nil, "handler_inline": false, "middleware": middleware, "paths": paths}
		unmounted := false
		if len(paths) == 0 && l.gateRefused[identity] {
			// GR-C7: the hop reaches the router, but another pattern
			// of the mux, a redirect or the closure may take the path.
			l.diagnostic(reg.file, "route_mount_unresolved", fmt.Sprintf("%s %q (another pattern of the ServeMux, its redirect or the forwarding closure may answer this path; no path is claimed through it)", reg.method, reg.pattern), reg.call.Span)
		} else if why := l.refusedWhy[identity]; len(paths) == 0 && why != "" {
			l.diagnostic(reg.file, "route_mount_unresolved", fmt.Sprintf("%s %q (%s; no path is claimed through it)", reg.method, reg.pattern, why), reg.call.Span)
		} else if len(paths) == 0 && reg.noPath == "" && !l.explained(g, reg.objects) {
			l.diagnostic(reg.file, "route_unmounted", fmt.Sprintf("%s %q (no resolved path from a root)", reg.method, reg.pattern), reg.call.Span)
			unmounted = true
		}
		var mounts []mountEntry
		scopes := make([]testscope.Scope, 0, len(paths))
		for _, p := range paths {
			scopes = append(scopes, p.Scope)
		}
		production, _ := scopedPaths(scopes)
		if !production && reg.noPath == "" && l.refusedWhy[identity] == "" && !l.gateRefused[identity] {
			// A route whose full paths are all test scope keeps the
			// unresolved mounts of its production chains for its entries
			// (UP-9 per scope); they are stored only on a pathless route.
			if mounts = l.mountEntries(g, *reg); len(mounts) > 0 && len(paths) == 0 {
				attrs[MountAttribute] = mounts
			}
		}
		if entries := l.safeUnknownPrefixPaths(*reg, paths, mounts, unmounted); len(entries) > 0 {
			attrs[UnknownPrefixAttribute] = entries
		}
		if testscope.Classify(reg.file, l.rules) == testscope.Test {
			attrs["scope"] = "test"
		}
		if reg.host != "" {
			attrs["host"] = reg.host
		}
		a := l.assumptions(reg.file, reg.framework)
		if l.gatePassed[identity] && len(paths) > 0 {
			// A path through a ServeMux -> chi hop (GR-C7) reads the
			// mux's Go 1.22 pattern semantics.
			a = append([]string{"GR-V2"}, a...)
			sort.Strings(a)
		}
		if len(a) > 0 {
			attrs["assumptions"] = a
		}
		at := goAt{reg.record, reg.call.Block, goStart(reg.call.Span)}
		switch {
		case reg.static != "":
			attrs["handler_expression"] = reg.static
			l.tag("GR-H5")
		case reg.handler == nil:
			l.tag("GR-H5")
		default:
			var id string
			var inline bool
			if reg.oapi != nil {
				// GR-G1: wrapper.N serves ServerInterface method N of
				// the instances' si.
				id = l.oapiHandler(reg.oapi, reg.handler)
				if id != "" {
					l.tag("GR-G1/implementing method")
				} else {
					attrs["handler_expression"] = "ServerInterface." + reg.handler.Name
				}
			} else {
				id, inline = l.handler(at, reg.handler)
			}
			switch {
			case reg.oapi != nil && id == "":
			case inline:
				attrs["handler_inline"] = true
			case id != "":
				attrs["handler"] = id
				edges = append(edges, facts.Edge("exposes_route", id, identity, reg.file, reg.call.Span))
			default:
				attrs["handler_expression"] = goArgText(*reg.handler)
				l.tag("GR-H5")
			}
		}
		l.tag("GR-H6")
		if group, unknown := l.safeRouteMiddleware(g, reg); len(group) > 0 || len(unknown) > 0 {
			if len(group) > 0 {
				attrs["group_middleware"] = group
			}
			if len(unknown) > 0 {
				attrs["group_middleware_unknown"] = unknown
			}
		}
		handlerID, _ := attrs["handler"].(string)
		if shape, built := l.safeShape(reg, handlerID); shape != nil {
			attrs[routeshape.AttributeKey] = shape
			// Model-type uses (row MT-6).
			routeshape.SetModelUses(attrs, built)
		}
		nodes = append(nodes, contract.Node{Type: "node", Identity: identity, Kind: "route", Level: 4, Name: q, QualifiedName: q, File: reg.file, Span: &reg.call.Span, ContentHash: reg.call.Hash, Attributes: attrs})
		edges = append(edges, facts.Edge("contains", l.owner(reg.record), identity, reg.file, reg.call.Span))
	}
	l.graph = g
	return nodes, edges
}

// safeCompose builds the composition graph; a failure becomes a warning
// and leaves every route without a path, never stopping the routes.
func (l *goLinker) safeCompose() (g *goGraph) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("Go route composition failed: %v", v)})
			g = &goGraph{out: map[string][]goEdge{}, in: map[string][]goEdge{}, chains: map[string][]goChain{}}
		}
	}()
	return l.compose()
}

// safePaths composes one registration's paths; a failure becomes a
// warning for its file and leaves that route without a path.
func (l *goLinker) safePaths(g *goGraph, reg goRegistration) (out []goPathEntry) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: reg.file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go route paths of %s %q failed: %v", reg.file, reg.method, reg.pattern, v)})
			out = []goPathEntry{}
		}
	}()
	return l.paths(g, reg)
}

// conflicts registers each net/http object's patterns in source order as
// its ServeMux would (GR-R1), compositions (GR-C3) included: a pattern
// that conflicts with an earlier one panics there, so the program never
// runs with both; the later one is refused (invalid_pattern). Only
// objects made in one place (a constructor, a zero value or the
// DefaultServeMux) are checked.
func (l *goLinker) conflicts() {
	type item struct {
		file    string
		span    contract.Span
		pattern string
		object  string
		reg     *goRegistration
		comp    int
	}
	var items []item
	single := func(objects []string) string {
		if len(objects) != 1 {
			return ""
		}
		o := l.objects[objects[0]]
		if o.kind != "ctor" && o.kind != "zero" && o.kind != "default" {
			return ""
		}
		return o.id
	}
	for i := range l.registrations {
		reg := &l.registrations[i]
		if reg.framework != goNetHTTP {
			continue
		}
		if id := single(reg.objects); id != "" {
			items = append(items, item{file: reg.file, span: reg.call.Span, pattern: reg.pattern, object: id, reg: reg})
		}
	}
	for i, c := range l.compositions {
		if c.kind != "mux" && c.kind != "muxchi" || l.module(c.at.rec.file).legacy {
			continue
		}
		id := single(c.parents)
		pattern, _, ok := l.constant(c.at, c.outer)
		if id == "" || !ok || goMuxPattern(pattern) != nil {
			continue
		}
		items = append(items, item{file: c.at.rec.file, span: c.call.Span, pattern: pattern, object: id, comp: i})
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.span.StartLine != b.span.StartLine {
			return a.span.StartLine < b.span.StartLine
		}
		return a.span.StartColumn < b.span.StartColumn
	})
	muxes := map[string]*goMux{}
	for _, it := range items {
		mux := muxes[it.object]
		if mux == nil {
			mux = &goMux{}
			muxes[it.object] = mux
		}
		earlier, err := mux.register(it.pattern)
		if err != nil || earlier == "" {
			continue
		}
		l.diagnostic(it.file, "route_invalid_pattern", fmt.Sprintf("%q (conflicts with %q on the same mux)", it.pattern, earlier), it.span)
		if it.reg != nil {
			l.count(it.reg.file, it.reg.site, goInvalidPattern)
			it.reg.noPath = goInvalidPattern
			continue
		}
		l.count(it.file, l.compositions[it.comp].site, goInvalidPattern)
		l.refused[it.comp] = true
	}
}

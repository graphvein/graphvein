package routes

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
)

// Escaped routers (contract row GR-C4, task T023): a constructor
// object is a root only when every use of every value that holds it is a
// use a contract row reads. The modelled uses are a registration's or a
// router child's receiver (GR-R*, GR-O1-GR-O3), a router's own methods
// (Use, NotFound, ServeHTTP, ...), the handler argument of a registration
// or composition (GR-R*, GR-C1, GR-C3, GR-C7, refused as wrapped_router
// where no row composes it), a serving position (GR-C6), a local binding
// (its own uses are read in turn), an argument bound to a router-typed
// parameter (GR-O4), the http.Handler argument of a wrapper (GR-O7) or of
// an http.Handler-returning call whose value is bound locally or served,
// net/http's StripPrefix, TimeoutHandler, MaxBytesHandler, Handler and
// HandlerFunc around such a use, a return GR-O5 or GR-O9 reads, an
// http.Server literal's Handler field and a generated server's entry call
// (GR-G1). Every other use makes the object escaped: it is no root, its
// routes get no path (route_unmounted is not added: the escape is
// reported as escaped_router with route_mount_unresolved naming it).
// Escape positions come from the kept syntax (bindings, returns, calls)
// and from the payload's escape records (row RU-escape), which cover the
// syntax the payload does not keep.

// Escape walk contexts.
const (
	goEscDiscard  = iota // the value is discarded
	goEscLocal           // the value is bound to a local of the record
	goEscModelled        // a contract row reads the value where it goes
	goEscAway            // the value escapes
)

// goEscCtx is where a walked value goes.
type goEscCtx struct {
	kind         int
	form, detail string
}

// goEscapeText is the diagnostic text of each escape form.
var goEscapeText = map[string]string{
	facts.GoEscapeStore:     "router stored in %s",
	facts.GoEscapeComposite: "router stored in a composite literal",
	facts.GoEscapeArgument:  "router passed to %s, which no rule reads it through",
	facts.GoEscapeSend:      "router sent on a channel",
	facts.GoEscapeMethod:    "router used through %s, which no rule reads",
	facts.GoEscapeReturn:    "router returned where no rule reads the return%s",
	goEscapeDiscard:         "router built by a call whose value is discarded",
}

// goEscapeDiscard is the link-side form of a factory call whose value is
// discarded.
const goEscapeDiscard = "discard"

// escapeWalk reads one record's escape positions.
func (l *goLinker) escapeWalk(rec *goRecord) {
	if l.oapiTemplate(rec) {
		// A generated server's own syntax passes the router on as row
		// GR-G1 reads it.
		return
	}
	fn := rec.fn
	for j, bd := range fn.Bindings {
		at := goAt{rec, bd.Block, goStart(bd.Span)}
		for i := range bd.Values {
			ctx := l.bindingCtx(at, bd, i)
			l.walkEsc(at, bd.Span, &bd.Values[i], ctx, fmt.Sprintf("%s/xb%d/%d", rec.key, j, i))
		}
	}
	for k, ret := range fn.Returns {
		at := goAt{rec, ret.Block, goStart(ret.Span)}
		for i := range ret.Values {
			ctx := goEscCtx{kind: goEscAway, form: facts.GoEscapeReturn}
			if rec.parent == nil && l.factory(at, &facts.GoExpr{Kind: facts.GoCall}, rec, i, len(ret.Values), fmt.Sprintf("%s/xr%d", rec.key, k)).proven() {
				// GR-O5 and GR-O9 read the return at each call, unless
				// the function may be called where no rule reads the
				// call.
				ctx = goEscCtx{kind: goEscModelled}
				if why := l.unreadCalls(rec, i); why != "" {
					ctx = goEscCtx{kind: goEscAway, form: facts.GoEscapeReturn, detail: why}
				}
			}
			l.walkEsc(at, ret.Span, &ret.Values[i], ctx, fmt.Sprintf("%s/xr%d/%d", rec.key, k, i))
		}
	}
	for k := range fn.Calls {
		c := &fn.Calls[k]
		var ctx goEscCtx
		switch c.Context {
		case facts.GoCallStatement:
			ctx = goEscCtx{kind: goEscDiscard}
		case facts.GoCallLocal:
			ctx = goEscCtx{kind: goEscLocal}
		default:
			continue
		}
		l.walkEsc(goAt{rec, c.Block, goStart(c.Span)}, c.Span, &c.Call, ctx, fmt.Sprintf("%s/xc%d", rec.key, k))
	}
	for k := range fn.Escapes {
		x := &fn.Escapes[k]
		at := goAt{rec, x.Block, goStart(x.Span)}
		if x.Form == facts.GoEscapeComposite && x.Detail == "Handler" && l.httpServerType(at, x.Type) {
			// An http.Server literal's Handler serves the router at its
			// own paths (GR-C6).
			continue
		}
		l.walkEsc(at, x.Span, &x.Value, goEscCtx{kind: goEscAway, form: x.Form, detail: x.Detail}, fmt.Sprintf("%s/xe%d", rec.key, k))
	}
}

// httpServerType reports net/http's Server type.
func (l *goLinker) httpServerType(at goAt, t *facts.GoExpr) bool {
	if t == nil || t.Kind != facts.GoSelector || t.Name != "Server" {
		return false
	}
	imp, ok := l.importOf(at, t.Object)
	return ok && imp.framework == goNetHTTP
}

// bindingCtx is where value i of a kept binding goes: a local the record
// declares, a discarded blank, a serving position (srv.Handler = h on an
// http.Server), or a store (a field or element target, a package-level or
// enclosing name, a type switch guard).
func (l *goLinker) bindingCtx(at goAt, bd facts.GoBinding, i int) goEscCtx {
	store := func(detail string) goEscCtx {
		return goEscCtx{kind: goEscAway, form: facts.GoEscapeStore, detail: detail}
	}
	target := func(name string) goEscCtx {
		switch {
		case name == "_":
			return goEscCtx{kind: goEscDiscard}
		case name == "":
			return store("a field or element")
		case bd.Kind == facts.GoBindDefine || bd.Kind == facts.GoBindVar || bd.Kind == facts.GoBindRange:
			return goEscCtx{kind: goEscLocal}
		}
		n := l.lookup(at, name)
		if (n.kind == "local" || n.kind == "param") && n.rec == at.rec {
			return goEscCtx{kind: goEscLocal}
		}
		return store(name)
	}
	switch bd.Kind {
	case facts.GoBindHandler:
		if l.serverVar(at, bd.Names[0]) {
			return goEscCtx{kind: goEscModelled}
		}
		return store(bd.Names[0] + ".Handler")
	case facts.GoBindConst, facts.GoBindRange, facts.GoBindRangeAssign:
		return goEscCtx{kind: goEscDiscard}
	case facts.GoBindTypeSwitch:
		return store("a type switch guard")
	}
	if len(bd.Values) == len(bd.Names) {
		return target(bd.Names[i])
	}
	ctx := goEscCtx{kind: goEscLocal}
	for _, name := range bd.Names {
		if c := target(name); c.kind == goEscAway {
			ctx = c
		}
	}
	return ctx
}

// walkEsc walks a value whose destination ctx says.
func (l *goLinker) walkEsc(at goAt, span contract.Span, e *facts.GoExpr, ctx goEscCtx, site string) {
	if e == nil {
		return
	}
	switch e.Kind {
	case facts.GoIdentifier:
		if ctx.kind == goEscAway {
			l.escapeValue(at, span, e, ctx, site)
		}
	case facts.GoUnary:
		if e.Value == "&" {
			l.walkEsc(at, span, e.Object, ctx, site+"/&")
		}
	case facts.GoSelector:
		if e.Object == nil {
			return
		}
		if _, ok := l.importOf(at, e.Object); ok {
			if ctx.kind == goEscAway {
				// http.DefaultServeMux as a value.
				l.escapeValue(at, span, e, ctx, site)
			}
			return
		}
		if e.Name == "ServeHTTP" {
			l.walkEsc(at, span, e.Object, ctx, site+"/o")
			return
		}
		// x.M as a value: a method value or a field read of a router.
		l.walkEsc(at, span, e.Object, goEscCtx{kind: goEscAway, form: facts.GoEscapeMethod, detail: e.Name}, site+"/o")
	case facts.GoCall:
		l.walkCallEsc(at, span, e, ctx, site)
	case facts.GoComposite:
		server := l.httpServerType(at, e.Type)
		for i := range e.Elements {
			el := &e.Elements[i]
			key := ""
			if el.Key != nil && el.Key.Kind == facts.GoIdentifier {
				key = el.Key.Name
			}
			if server && key == "Handler" {
				continue
			}
			l.walkEsc(at, span, &el.Value, goEscCtx{kind: goEscAway, form: facts.GoEscapeComposite, detail: key}, fmt.Sprintf("%s/e%d", site, i))
		}
	case facts.GoFuncLit:
		if ctx.kind == goEscAway && e.Request {
			// A handler literal that may serve a request through the
			// routers it uses goes where no row reads it.
			for _, name := range e.Uses {
				id := facts.GoExpr{Kind: facts.GoIdentifier, Name: name}
				l.escapeValue(at, span, &id, ctx, site+"/u"+name)
			}
		}
	}
}

// walkCallEsc walks a call: its callee decides where its arguments go.
func (l *goLinker) walkCallEsc(at goAt, span contract.Span, e *facts.GoExpr, ctx goEscCtx, site string) {
	c := e.Callee
	if c == nil || l.constructor(at, e) != "" {
		return
	}
	if s, _ := l.oapiEntry(at, e); s != nil {
		// A generated server's entry call (GR-G1) reads its router.
		return
	}
	text := goExprText(e.Callee)
	away := goEscCtx{kind: goEscAway, form: facts.GoEscapeArgument, detail: text}
	if ctx.kind == goEscAway || ctx.kind == goEscDiscard {
		// The call's own value (a factory's or a wrapper's router)
		// escapes with it, or is discarded: no row reads
		// it.
		if v := l.eval(at, e, site); len(v.objects) > 0 {
			c, ids := ctx, v.objects
			if ctx.kind == goEscDiscard {
				// Only a factory's roots matter: a router method's value
				// (a statement r.Route(...), gin's g.Use(mw)) is its
				// receiver's tree.
				c, ids = goEscCtx{kind: goEscAway, form: goEscapeDiscard}, nil
				if l.calledRecord(at, e) == nil && l.chainRecord(at, e) == nil {
					v.objects = nil
				}
				for _, id := range v.objects {
					if k := l.objects[id].kind; k == "ctor" || k == "zero" {
						ids = append(ids, id)
					}
				}
			}
			l.escapeObjects(at, span, ids, goExprText(e), c, site)
		}
	}
	switch c.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, c.Name).kind == "func" {
			l.moduleCallEsc(at, span, e, l.funcRecord(l.ownFiles(at.rec.file), c.Name), ctx, site)
			return
		}
		if goBuiltinStores[c.Name] && l.lookup(at, c.Name).kind == "" {
			// append and copy store their arguments in a
			// slice.
			l.argsEsc(at, span, e, away, site)
			return
		}
	case facts.GoSelector:
		if c.Object == nil {
			break
		}
		if imp, ok := l.importOf(at, c.Object); ok {
			switch {
			case imp.framework == goNetHTTP:
				l.httpCallEsc(at, span, e, ctx, site)
			case goTestServer(imp, c.Name):
				// net/http/httptest serves a handler named directly at its
				// own paths; anything else in that position escapes.
				for i := range e.Args {
					s := fmt.Sprintf("%s/a%d", site, i)
					if i == 0 && goDirect(&e.Args[0]) {
						// What the name holds only inside another call or a
						// closure is not served at its own paths.
						held, direct := map[string]bool{}, map[string]bool{}
						l.escapeHeld(at, &e.Args[0], s, map[string]bool{}, held)
						for _, id := range l.eval(at, &e.Args[0], s).objects {
							direct[id] = true
						}
						var wrapped []string
						for id := range held {
							if !direct[id] {
								wrapped = append(wrapped, id)
							}
						}
						sort.Strings(wrapped)
						l.escapeObjects(at, span, wrapped, goExprText(&e.Args[0]), away, s)
						continue
					}
					l.walkEsc(at, span, &e.Args[i], away, s)
				}
			case imp.files != nil:
				l.moduleCallEsc(at, span, e, l.funcRecord(imp.files, c.Name), ctx, site)
			default:
				l.argsEsc(at, span, e, l.unknownArgCtx(ctx, away), site)
			}
			return
		}
		recv := l.eval(at, c.Object, site+"/o")
		if len(recv.objects) > 0 {
			l.routerMethodEsc(at, span, e, recv, ctx, site)
			return
		}
		if c.Object.Kind == facts.GoCall {
			// f(...).M(): the inner call's value only selects M.
			l.walkEsc(at, span, c.Object, goEscCtx{kind: goEscLocal}, site+"/o")
			if rec := l.chainRecord(at, e); rec != nil {
				l.moduleCallEsc(at, span, e, rec, ctx, site)
				return
			}
		} else if rec := l.methodRecord(at, c.Object, c.Name); rec != nil {
			l.moduleCallEsc(at, span, e, rec, ctx, site)
			return
		}
		if goRegistersAny(c.Name) || c.Name == "Mount" {
			// A router-named call on a receiver no rule proves a router:
			// a router passed to it is registered where no row reads.
			l.argsEsc(at, span, e, away, site)
			return
		}
	case facts.GoFuncLit:
		// func(...){...}(args): the literal's body is its own record.
	}
	l.argsEsc(at, span, e, l.unknownArgCtx(ctx, away), site)
}

// unknownArgCtx is where the arguments of a call no rule resolves go: with
// the call's value when that is bound locally or read by a row (the call
// may wrap them: the uses of the value decide, as assumption GR-V5 or
// wrapped_router does), otherwise into the call.
func (l *goLinker) unknownArgCtx(ctx, away goEscCtx) goEscCtx {
	if ctx.kind == goEscLocal || ctx.kind == goEscModelled {
		return ctx
	}
	return away
}

// argsEsc walks every argument of a call in one context.
func (l *goLinker) argsEsc(at goAt, span contract.Span, e *facts.GoExpr, ctx goEscCtx, site string) {
	for i := range e.Args {
		a := &e.Args[i]
		if a.Kind == facts.GoSpread {
			a = a.Object
		}
		l.walkEsc(at, span, a, ctx, fmt.Sprintf("%s/a%d", site, i))
	}
}

// httpCallEsc walks a net/http package call: StripPrefix, TimeoutHandler,
// MaxBytesHandler and the Handler and HandlerFunc conversions carry their
// handler to the call's destination; the serving functions and the
// DefaultServeMux registrations read theirs (GR-C6, GR-R1, GR-C3, GR-C7).
func (l *goLinker) httpCallEsc(at goAt, span contract.Span, e *facts.GoExpr, ctx goEscCtx, site string) {
	name := e.Callee.Name
	carry := -1
	switch name {
	case "StripPrefix":
		carry = 1
	case "TimeoutHandler", "MaxBytesHandler", "Handler", "HandlerFunc":
		carry = 0
	case "Handle", "HandleFunc":
		return
	}
	if i, ok := goServeArg[name]; ok {
		for k := range e.Args {
			if k != i {
				l.walkEsc(at, span, &e.Args[k], goEscCtx{kind: goEscDiscard}, fmt.Sprintf("%s/a%d", site, k))
			}
		}
		return
	}
	away := goEscCtx{kind: goEscAway, form: facts.GoEscapeArgument, detail: goExprText(e.Callee)}
	for k := range e.Args {
		c := l.unknownArgCtx(ctx, away)
		if k == carry {
			c = ctx
		}
		l.walkEsc(at, span, &e.Args[k], c, fmt.Sprintf("%s/a%d", site, k))
	}
}

// moduleCallEsc walks a call of a module function or method: an argument
// bound to a router-typed parameter (GR-O4) or a wrapper's handler (GR-O7)
// is read; one passed to an http.Handler parameter of a function whose one
// result is an http.Handler goes with the call's value; any other escapes
// into the call (an http.Handler, interface or any parameter, a parameter
// that is reassigned).
func (l *goLinker) moduleCallEsc(at goAt, span contract.Span, e *facts.GoExpr, rec *goRecord, ctx goEscCtx, site string) {
	away := goEscCtx{kind: goEscAway, form: facts.GoEscapeArgument, detail: goExprText(e.Callee)}
	if rec == nil {
		l.argsEsc(at, span, e, l.unknownArgCtx(ctx, away), site)
		return
	}
	wrapParam, _, _, wrapper := l.wrapperCall(at, e)
	handlerResult := len(rec.fn.Results) == 1 && l.handlerType(rec.file, &rec.fn.Results[0].Type)
	for k := range e.Args {
		a := &e.Args[k]
		s := fmt.Sprintf("%s/a%d", site, k)
		if wrapper && k == wrapParam {
			l.walkEsc(at, span, a, ctx, s)
			continue
		}
		if k < len(rec.fn.Params) {
			p := rec.fn.Params[k]
			fw, value := l.routerType(rec.file, &p.Type)
			if fw != "" && !value && !p.Variadic && p.Name != "" && p.Name != "_" && len(l.writes(goName{kind: "param", rec: rec, decl: k}, p.Name)) == 0 {
				// GR-O4 binds the parameter; its uses are read in the
				// callee.
				continue
			}
			if handlerResult && !p.Variadic && l.handlerType(rec.file, &p.Type) && (ctx.kind == goEscLocal || ctx.kind == goEscModelled) {
				l.walkEsc(at, span, a, ctx, s)
				continue
			}
			if !p.Variadic && l.handlerType(rec.file, &p.Type) && l.directValue(at, a, s) && l.servesDirectly(rec, k) {
				// The callee only serves the parameter at its own paths
				// (an http.Server's Handler, a serving call's handler,
				// srv.Handler = h): a serving use.
				continue
			}
		}
		if a.Kind == facts.GoSpread {
			a = a.Object
		}
		l.walkEsc(at, span, a, away, s)
	}
}

// handlerType reports net/http's Handler type written in file.
func (l *goLinker) handlerType(file string, t *facts.GoExpr) bool {
	if t == nil || t.Kind != facts.GoSelector || t.Name != "Handler" || t.Object == nil || t.Object.Kind != facts.GoIdentifier {
		return false
	}
	imp, ok := l.imports[file][t.Object.Name]
	return ok && imp.framework == goNetHTTP
}

// routerMethodEsc walks a method call on router objects: registrations,
// compositions and children read their arguments (GR-R*, GR-C1, GR-C3,
// GR-C7, GR-O1-GR-O3); a router's own methods keep their receiver, their
// arguments escape into the router; any other method (gin's Handler())
// passes the router on where no row reads it.
func (l *goLinker) routerMethodEsc(at goAt, span contract.Span, e *facts.GoExpr, recv goValue, ctx goEscCtx, site string) {
	fw := l.framework(recv)
	name := e.Callee.Name
	switch {
	case goRegisters(fw, name), fw == goChi && name == "Mount",
		fw == goChi && (name == "Route" || name == "Group" || name == "With"),
		fw == goGin && (name == "Group" || name == "Use"):
		return
	case goRouterOwnMethods[name]:
		l.argsEsc(at, span, e, goEscCtx{kind: goEscAway, form: facts.GoEscapeArgument, detail: goExprText(e.Callee)}, site)
		return
	}
	if ctx.kind != goEscDiscard {
		l.escapeObjects(at, span, recv.objects, goExprText(e.Callee), goEscCtx{kind: goEscAway, form: facts.GoEscapeMethod, detail: name}, site)
	}
	l.argsEsc(at, span, e, goEscCtx{kind: goEscAway, form: facts.GoEscapeArgument, detail: goExprText(e.Callee)}, site)
}

// goRouterOwnMethods are the router methods whose call keeps the router
// where it is (the capture's routerOwnMethods).
var goRouterOwnMethods = map[string]bool{
	"Use": true, "NotFound": true, "MethodNotAllowed": true, "NoRoute": true, "NoMethod": true, "ServeHTTP": true,
	"Routes": true, "Middlewares": true, "Find": true, "NotFoundHandler": true, "MethodNotAllowedHandler": true,
	"Run": true, "RunTLS": true, "RunUnix": true, "RunFd": true, "RunListener": true, "LoadHTMLGlob": true,
	"LoadHTMLFiles": true, "LoadHTMLFS": true, "SetHTMLTemplate": true, "SetFuncMap": true, "Delims": true,
	"SetTrustedProxies": true, "HandleContext": true, "SecureJsonPrefix": true, "BasePath": true, "Match": true,
}

// escapeValue marks the objects an escaping value holds: directly, or
// inside the calls and closures its bindings hold.
func (l *goLinker) escapeValue(at goAt, span contract.Span, e *facts.GoExpr, ctx goEscCtx, site string) {
	set := map[string]bool{}
	l.escapeHeld(at, e, site, map[string]bool{}, set)
	var ids []string
	for id := range set {
		ids = append(ids, id)
	}
	text := goExprText(e)
	if text == "" {
		text = "router"
	}
	l.escapeObjects(at, span, ids, text, ctx, site)
}

// escapeObjects records that objects escape at site (escaped_router).
func (l *goLinker) escapeObjects(at goAt, span contract.Span, ids []string, text string, ctx goEscCtx, site string) {
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		o := l.objects[id]
		o.escapes = append(o.escapes, site)
	}
	why := goEscapeText[ctx.form]
	if strings.Contains(why, "%s") {
		why = fmt.Sprintf(why, ctx.detail)
	}
	if ctx.form == facts.GoEscapeComposite && ctx.detail != "" {
		text = ctx.detail + ": " + text
	}
	if !l.counted[site+"\x00"+goEscapedRouter] {
		l.diagnostic(at.rec.file, "route_mount_unresolved", text+" ("+why+"; no path is claimed through it)", span)
	}
	l.hopText(at, site, text, text+" ("+why+")", span, upFormEscaped)
	l.count(at.rec.file, site, goEscapedRouter)
}

// propagateEscapes marks the objects bound to an escaped parameter escaped
// too: the parameter holds them where they escape.
func (l *goLinker) propagateEscapes() {
	for changed := true; changed; {
		changed = false
		for _, b := range l.bindings {
			p, o := l.objects[b.param], l.objects[b.object]
			if p != nil && o != nil && len(p.escapes) > 0 && len(o.escapes) == 0 {
				o.escapes = append(o.escapes, p.escapes[0])
				changed = true
			}
		}
	}
}

// escapeHeld collects the router objects a value holds: its own (eval),
// those its local bindings hold, and those inside its calls' arguments,
// composite elements and handler closures (a value that wraps a router
// carries it). A router served by an http.Server literal's Handler or by a
// serving call is held by the server, not by the value (a server passed
// on serves its router at its own paths).
func (l *goLinker) escapeHeld(at goAt, e *facts.GoExpr, site string, seen, out map[string]bool) {
	if e == nil {
		return
	}
	for _, id := range l.eval(at, e, site).objects {
		out[id] = true
	}
	switch e.Kind {
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		if n.kind != "local" {
			return
		}
		reach, _ := reaching(n.rec.fn, l.writes(n, e.Name), at)
		for _, j := range reach {
			key := fmt.Sprintf("%s/b%d/%s", n.rec.key, j, e.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			bd := n.rec.fn.Bindings[j]
			if len(bd.Values) == len(bd.Names) {
				l.escapeHeld(goAt{n.rec, bd.Block, goStart(bd.Span)}, &bd.Values[hasName(bd.Names, e.Name)], key, seen, out)
			}
		}
	case facts.GoUnary:
		l.escapeHeld(at, e.Object, site+"/&", seen, out)
	case facts.GoSelector:
		if e.Name == "ServeHTTP" {
			l.escapeHeld(at, e.Object, site+"/o", seen, out)
		}
	case facts.GoCall:
		if c := e.Callee; c != nil && c.Kind == facts.GoSelector {
			if imp, ok := l.importOf(at, c.Object); ok {
				if _, serves := goServeArg[c.Name]; serves && imp.framework == goNetHTTP || goTestServer(imp, c.Name) {
					return
				}
			}
		}
		rec := l.calledRecord(at, e)
		for i := range e.Args {
			if rec != nil && i < len(rec.fn.Params) && !rec.fn.Params[i].Variadic && l.handlerType(rec.file, &rec.fn.Params[i].Type) && l.servesDirectly(rec, i) {
				// The callee serves the argument at its own paths (an
				// http.Server it builds holds it).
				continue
			}
			l.escapeHeld(at, &e.Args[i], fmt.Sprintf("%s/a%d", site, i), seen, out)
		}
	case facts.GoComposite:
		server := l.httpServerType(at, e.Type)
		for i := range e.Elements {
			el := &e.Elements[i]
			if server && el.Key != nil && el.Key.Kind == facts.GoIdentifier && el.Key.Name == "Handler" {
				continue
			}
			l.escapeHeld(at, &el.Value, fmt.Sprintf("%s/e%d", site, i), seen, out)
		}
	case facts.GoFuncLit:
		if e.Request {
			for _, name := range e.Uses {
				id := facts.GoExpr{Kind: facts.GoIdentifier, Name: name}
				for _, o := range l.eval(at, &id, site+"/u"+name).objects {
					out[o] = true
				}
			}
		}
	}
}

// goTestServer reports net/http/httptest's NewServer, NewTLSServer and
// NewUnstartedServer (Go 1.27.1: the server's Config.Handler is the
// handler, served at its own paths).
func goTestServer(imp goImport, name string) bool {
	return imp.path == "net/http/httptest" && (name == "NewServer" || name == "NewTLSServer" || name == "NewUnstartedServer")
}

// goDirect reports a value that is a router value itself: a name, & of
// one, or x.ServeHTTP of one.
func goDirect(e *facts.GoExpr) bool {
	switch e.Kind {
	case facts.GoIdentifier:
		return true
	case facts.GoUnary:
		return e.Value == "&" && e.Object != nil && goDirect(e.Object)
	case facts.GoSelector:
		return e.Name == "ServeHTTP" && e.Object != nil && goDirect(e.Object)
	}
	return false
}

// servesDirectly reports an http.Handler parameter k of rec whose every use
// in rec and its function literals serves it at its own paths: the Handler
// element of an http.Server literal, the handler argument of a net/http
// serving call, the value of srv.Handler = h with srv an http.Server, or
// the receiver of h.ServeHTTP(...); the parameter is never reassigned and
// no escape record names it (the capture keeps or records every
// use of an http.Handler parameter).
func (l *goLinker) servesDirectly(rec *goRecord, k int) bool {
	key := fmt.Sprintf("%s#%d", rec.key, k)
	if v, ok := l.direct[key]; ok {
		return v
	}
	l.direct[key] = false
	name := rec.fn.Params[k].Name
	if name == "" || name == "_" {
		l.direct[key] = true
		return true
	}
	if len(l.writes(goName{kind: "param", rec: rec, decl: k}, name)) > 0 {
		return false
	}
	ok := true
	var visit func(at goAt, e *facts.GoExpr, allowed bool)
	visit = func(at goAt, e *facts.GoExpr, allowed bool) {
		if e == nil || !ok {
			return
		}
		switch e.Kind {
		case facts.GoIdentifier:
			if e.Name == name && !allowed {
				ok = false
			}
			return
		case facts.GoCall:
			c := e.Callee
			if c != nil && c.Kind == facts.GoSelector && c.Name == "ServeHTTP" && c.Object != nil && c.Object.Kind == facts.GoIdentifier && c.Object.Name == name {
				for i := range e.Args {
					visit(at, &e.Args[i], false)
				}
				return
			}
			serve := -1
			if c != nil && c.Kind == facts.GoSelector {
				if imp, isImport := l.importOf(at, c.Object); isImport && imp.framework == goNetHTTP {
					if i, found := goServeArg[c.Name]; found {
						serve = i
					}
				}
			}
			visit(at, c, false)
			for i := range e.Args {
				visit(at, &e.Args[i], i == serve)
			}
			return
		case facts.GoComposite:
			server := l.httpServerType(at, e.Type)
			for i := range e.Elements {
				el := &e.Elements[i]
				handler := server && el.Key != nil && el.Key.Kind == facts.GoIdentifier && el.Key.Name == "Handler"
				visit(at, &el.Value, handler)
			}
			return
		case facts.GoFuncLit:
			for _, u := range e.Uses {
				if u == name {
					// A captured literal's uses are visited as its record;
					// an uncaptured one's are not seen.
					if e.Func == 0 {
						ok = false
					}
				}
			}
			return
		}
		visit(at, e.Object, false)
		visit(at, e.Callee, false)
		visit(at, e.Left, false)
		visit(at, e.Right, false)
		for i := range e.Args {
			visit(at, &e.Args[i], false)
		}
		for i := range e.Elements {
			visit(at, &e.Elements[i].Value, false)
		}
	}
	for _, r := range l.records[rec.file] {
		inside := false
		for x := r; x != nil; x = x.parent {
			inside = inside || x == rec
		}
		if !inside {
			continue
		}
		fn := r.fn
		for _, bd := range fn.Bindings {
			at := goAt{r, bd.Block, goStart(bd.Span)}
			for i := range bd.Values {
				direct := bd.Kind == facts.GoBindHandler && l.serverVar(at, bd.Names[0])
				visit(at, &bd.Values[i], direct)
			}
		}
		for _, ret := range fn.Returns {
			for i := range ret.Values {
				visit(goAt{r, ret.Block, goStart(ret.Span)}, &ret.Values[i], false)
			}
		}
		for i := range fn.Calls {
			c := &fn.Calls[i]
			visit(goAt{r, c.Block, goStart(c.Span)}, &c.Call, false)
		}
		for i := range fn.Escapes {
			x := &fn.Escapes[i]
			visit(goAt{r, x.Block, goStart(x.Span)}, &x.Value, false)
		}
	}
	l.direct[key] = ok
	return ok
}

// directValue reports a value whose router objects are all its own (eval):
// none is held only inside another call or a closure, so serving the value
// serves those routers at their own paths.
func (l *goLinker) directValue(at goAt, e *facts.GoExpr, site string) bool {
	held, direct := map[string]bool{}, map[string]bool{}
	l.escapeHeld(at, e, site, map[string]bool{}, held)
	for _, id := range l.eval(at, e, site).objects {
		direct[id] = true
	}
	for id := range held {
		if !direct[id] {
			return false
		}
	}
	return true
}

// goBuiltinStores are the predeclared functions whose arguments a
// result or slice keeps.
var goBuiltinStores = map[string]bool{"append": true, "copy": true}

// unreadCalls names why a factory's calls may be unread:
// a method whose name a module interface declares (a call
// through the interface is not resolved), or a function or method used as
// a value (called where no rule reads the call). "" otherwise.
func (l *goLinker) unreadCalls(rec *goRecord, result int) string {
	l.valueUses()
	name := rec.fn.Name
	if rec.fn.Receiver != nil {
		switch {
		case l.ifaceMethods[name]:
			return ": " + name + " may be called through a module interface"
		case l.methodValues[name]:
			return ": " + name + " is used as a method value"
		}
		return ""
	}
	if l.funcValues[path.Dir(rec.file)+"\x00"+name] {
		return ": " + name + " is used as a function value"
	}
	if why := l.unreadNameUses(rec); why != "" {
		return why
	}
	return l.unreadResults(rec, result, map[string]bool{})
}

// unreadNameUses applies the general rule of step GR10:
// a factory is read only when every use of its name in the module is a
// call the linker reads. The payload counts each file's calls and other
// uses of every name (facts.GoFacts.Names); any other use (a map, slice
// or field element, an argument, an assignment, a return, a comparison)
// makes its routers escape (calls themselves are read where the payload
// keeps them; see below).
// Names are matched by spelling: a same-named method of another type
// counts too (a refusal, never a claim).
func (l *goLinker) unreadNameUses(rec *goRecord) string {
	values := 0
	l.nameUsesOf(rec, func(_ string, u facts.GoNameUse) { values += u.Values })
	if values > 0 {
		return ": " + rec.fn.Name + " is used other than by a call"
	}
	// Calls outside the captured syntax are read through the payload's
	// result uses (unreadResults).
	return ""
}

// valueUses collects, once per link, the method names of the module's
// interface types and the functions and method names its router syntax
// uses as values (not called).
func (l *goLinker) valueUses() {
	if l.ifaceMethods != nil {
		return
	}
	l.ifaceMethods, l.funcValues, l.methodValues = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, file := range facts.Paths(l.files) {
		if g := l.files[file].Go; g != nil {
			for _, t := range g.Types {
				for _, m := range t.Methods {
					l.ifaceMethods[m] = true
				}
			}
		}
	}
	var visit func(at goAt, e *facts.GoExpr, callee bool)
	visit = func(at goAt, e *facts.GoExpr, callee bool) {
		if e == nil {
			return
		}
		switch e.Kind {
		case facts.GoIdentifier:
			if !callee && l.lookup(at, e.Name).kind == "func" {
				l.funcValues[path.Dir(at.rec.file)+"\x00"+e.Name] = true
			}
			return
		case facts.GoSelector:
			if !callee {
				if imp, ok := l.importOf(at, e.Object); ok {
					if imp.files != nil {
						l.funcValues[path.Dir(imp.files[0])+"\x00"+e.Name] = true
					}
				} else if e.Name != "ServeHTTP" {
					l.methodValues[e.Name] = true
				}
			}
			visit(at, e.Object, false)
			return
		case facts.GoCall:
			visit(at, e.Callee, true)
			for i := range e.Args {
				visit(at, &e.Args[i], false)
			}
			return
		}
		visit(at, e.Object, false)
		visit(at, e.Left, false)
		visit(at, e.Right, false)
		for i := range e.Args {
			visit(at, &e.Args[i], false)
		}
		for i := range e.Elements {
			visit(at, &e.Elements[i].Value, false)
		}
	}
	for _, file := range facts.Paths(l.files) {
		for _, rec := range l.records[file] {
			fn := rec.fn
			for _, bd := range fn.Bindings {
				for i := range bd.Values {
					visit(goAt{rec, bd.Block, goStart(bd.Span)}, &bd.Values[i], false)
				}
			}
			for _, ret := range fn.Returns {
				for i := range ret.Values {
					visit(goAt{rec, ret.Block, goStart(ret.Span)}, &ret.Values[i], false)
				}
			}
			for i := range fn.Calls {
				c := &fn.Calls[i]
				visit(goAt{rec, c.Block, goStart(c.Span)}, &c.Call, false)
			}
			for i := range fn.Escapes {
				x := &fn.Escapes[i]
				visit(goAt{rec, x.Block, goStart(x.Span)}, &x.Value, false)
			}
		}
	}
}

// nameKeys are the payload name keys (facts.GoFacts.Names) under which
// the files of the module write rec's calls, with the files each key is
// read in: F in the files of rec's directory, imp:<path>.F in any file
// whose import path resolves to that directory, .M (a method) anywhere.
func (l *goLinker) nameUsesOf(rec *goRecord, visit func(file string, u facts.GoNameUse)) {
	name := rec.fn.Name
	if rec.fn.Receiver != nil {
		for _, file := range facts.Paths(l.files) {
			if g := l.files[file].Go; g != nil {
				visit(file, g.Names["."+name])
			}
		}
		return
	}
	dir := path.Dir(rec.file)
	base, module := golang.ModuleAt(rec.file, l.modules)
	for _, file := range facts.Paths(l.files) {
		g := l.files[file].Go
		if g == nil {
			continue
		}
		if path.Dir(file) == dir {
			visit(file, g.Names[name])
		}
		for k, u := range g.Names {
			if !strings.HasPrefix(k, "imp:") || !strings.HasSuffix(k, "."+name) {
				continue
			}
			imp := strings.TrimSuffix(strings.TrimPrefix(k, "imp:"), "."+name)
			if d, ok := golang.ModuleImportDir(base, module, imp); ok && d == dir {
				visit(file, u)
			}
		}
	}
}

// unreadResults reads the result uses the payload records for the calls
// of rec outside the captured syntax (row RN-results): a use
// of result index result (or of the whole call) is read when it is an
// argument the callee serves at its own paths (an http.Handler parameter
// it only serves directly, a router-typed parameter that neither escapes
// nor is composed or served under a prefix or wrapper), or a return of a
// function whose own calls are read the same way. Any other use (a
// store, an element, another function's argument, a method other than
// the router's own) makes rec's routers escape.
func (l *goLinker) unreadResults(rec *goRecord, result int, seen map[string]bool) string {
	if seen[rec.key] {
		return ""
	}
	seen[rec.key] = true
	why := ""
	l.nameUsesOf(rec, func(file string, nu facts.GoNameUse) {
		for _, u := range nu.Results {
			if why != "" {
				return
			}
			if u.Result != -1 && u.Result != result {
				continue
			}
			switch u.Use {
			case facts.GoResultArgument:
				if !l.argumentRead(file, u.Callee, u.Arg) {
					why = fmt.Sprintf(": %s's result is passed to %s (%s:%d), which no rule reads it through", rec.fn.Name, u.Callee, file, u.Line)
				}
			case facts.GoResultReturn:
				r := l.resultRecord(file, u.Callee)
				if r == nil {
					why = fmt.Sprintf(": %s's result is returned by %s (%s:%d), whose calls no rule reads", rec.fn.Name, u.Callee, file, u.Line)
				} else if w := l.unreadResults(r, -1, seen); w != "" {
					why = w
				}
			default:
				why = fmt.Sprintf(": %s's result is used by %s (%s:%d), which no rule reads", rec.fn.Name, u.Callee, file, u.Line)
			}
		}
	})
	return why
}

// resultRecord is the captured function or method a name key of file
// names (F in its directory, imp:<path>.F through the module); nil for a
// method key (.M: its type is unknown) or anything unresolved.
func (l *goLinker) resultRecord(file, k string) *goRecord {
	switch {
	case strings.HasPrefix(k, "."):
		return nil
	case strings.HasPrefix(k, "imp:"):
		i := strings.LastIndex(k, ".")
		base, module := golang.ModuleAt(file, l.modules)
		dir, ok := golang.ModuleImportDir(base, module, k[len("imp:"):i])
		if !ok {
			return nil
		}
		var files []string
		for _, f := range facts.Paths(l.files) {
			if path.Dir(f) == dir && strings.HasSuffix(f, ".go") {
				files = append(files, f)
			}
		}
		return l.funcRecord(files, k[i+1:])
	}
	var files []string
	for _, f := range facts.Paths(l.files) {
		if path.Dir(f) == path.Dir(file) && strings.HasSuffix(f, ".go") {
			files = append(files, f)
		}
	}
	return l.funcRecord(files, k)
}

// argumentRead reports an argument that a module callee serves at its own
// paths: an http.Handler parameter it only serves directly, or a router
// parameter whose object neither escapes nor is composed, wrapped or
// served under a prefix or a wrapper.
func (l *goLinker) argumentRead(file, k string, arg int) bool {
	r := l.resultRecord(file, k)
	if r == nil || arg >= len(r.fn.Params) {
		return false
	}
	p := r.fn.Params[arg]
	if p.Variadic {
		return false
	}
	if l.handlerType(r.file, &p.Type) {
		return l.servesDirectly(r, arg)
	}
	fw, value := l.routerType(r.file, &p.Type)
	if fw == "" || value || p.Name == "" || p.Name == "_" || len(l.writes(goName{kind: "param", rec: r, decl: arg}, p.Name)) > 0 {
		return false
	}
	id := fmt.Sprintf("param:%s#%d", r.key, arg)
	o := l.objects[id]
	if o != nil && len(o.escapes) > 0 || l.wrappedAt[id] != "" {
		return false
	}
	for _, c := range l.compositions {
		if goHas(c.children, id) {
			return false
		}
	}
	for _, s := range l.served[id] {
		if s.strip != "" || len(s.wrappers) > 0 || s.blocked != "" {
			return false
		}
	}
	return true
}

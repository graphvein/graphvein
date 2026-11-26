package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Serving positions (contract row GR-C6, task T023): the handler
// a net/http server serves. A router object reaching one is served
// directly, under the http.StripPrefix prefixes on the way (Go 1.27.1
// net/http: StripPrefix removes the prefix from the request path before
// the inner handler sees it, and answers 404 to a path without it), or
// through wrapper calls, whose effect on the path is assumption GR-V5.

// goServeArg is the handler argument of each net/http serving function.
var goServeArg = map[string]int{"ListenAndServe": 1, "ListenAndServeTLS": 3, "Serve": 1, "ServeTLS": 1}

// goPathPreserving are the net/http functions whose handler argument
// (the first) receives the same request path (Go 1.27.1 net/http:
// TimeoutHandler serves r.WithContext(ctx), MaxBytesHandler a copy of r
// with another Body); Handler and HandlerFunc are conversions.
var goPathPreserving = map[string]bool{"Handler": true, "HandlerFunc": true, "TimeoutHandler": true, "MaxBytesHandler": true}

// goServe is one way a router object is served (GR-C6): the StripPrefix
// prefixes on the way, outermost first, and the wrappers it is served
// through. blocked names why no path is claimed through it.
type goServe struct {
	object      string
	strip       string
	resolution  string
	wrappers    []string
	conditional bool
	file        string
	blocked     string
	// via are the files of the parameter bindings that carry the object
	// to a helper's serving position (test scope).
	via []string
	// methods are the request methods a wrapper on the way answers
	// itself.
	methods []string
}

func (s goServe) key() string {
	return fmt.Sprintf("%s|%q|%s|%q|%v|%s|%s|%q|%q", s.object, s.strip, s.resolution, s.wrappers, s.conditional, s.file, s.blocked, s.via, s.methods)
}

// serving reads the serving positions of a record: the handler argument of
// a net/http serving call and the Handler field of an http.Server literal,
// in its bindings, returns and calls. Function literals are records of
// their own.
func (l *goLinker) serving(rec *goRecord) {
	fn := rec.fn
	visit := func(at goAt, span contract.Span, e *facts.GoExpr, site string) {
		var walk func(e *facts.GoExpr, site string)
		walk = func(e *facts.GoExpr, site string) {
			if e == nil || e.Kind == facts.GoFuncLit {
				return
			}
			switch e.Kind {
			case facts.GoCall:
				if c := e.Callee; c != nil && c.Kind == facts.GoSelector {
					if imp, ok := l.importOf(at, c.Object); ok && imp.framework == goNetHTTP {
						if i, ok := goServeArg[c.Name]; ok && i < len(e.Args) {
							l.serveAt(at, span, &e.Args[i], fmt.Sprintf("%s/a%d", site, i))
						}
					}
				}
			case facts.GoComposite:
				if t := e.Type; t != nil && t.Kind == facts.GoSelector && t.Name == "Server" {
					if imp, ok := l.importOf(at, t.Object); ok && imp.framework == goNetHTTP {
						for i := range e.Elements {
							el := &e.Elements[i]
							if el.Key != nil && el.Key.Kind == facts.GoIdentifier && el.Key.Name == "Handler" {
								l.serveAt(at, span, &el.Value, fmt.Sprintf("%s/e%d", site, i))
							}
						}
					}
				}
			}
			walk(e.Object, site+"/o")
			walk(e.Callee, site+"/f")
			walk(e.Left, site+"/l")
			walk(e.Right, site+"/r")
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
		at := goAt{rec, bd.Block, goStart(bd.Span)}
		if bd.Kind == facts.GoBindHandler && len(bd.Values) == 1 && l.serverVar(at, bd.Names[0]) {
			// srv.Handler = h with srv an http.Server.
			l.tag("GR-C6")
			l.tag("GR-C6/`srv.Handler = h`")
			l.serveAt(at, bd.Span, &bd.Values[0], fmt.Sprintf("%s/vh%d", rec.key, j))
		}
		for i := range bd.Values {
			visit(at, bd.Span, &bd.Values[i], fmt.Sprintf("%s/vb%d/%d", rec.key, j, i))
		}
	}
	for k, ret := range fn.Returns {
		for i := range ret.Values {
			visit(goAt{rec, ret.Block, goStart(ret.Span)}, ret.Span, &ret.Values[i], fmt.Sprintf("%s/vr%d/%d", rec.key, k, i))
		}
	}
	for k := range fn.Calls {
		c := &fn.Calls[k]
		visit(goAt{rec, c.Block, goStart(c.Span)}, c.Span, &c.Call, fmt.Sprintf("%s/vc%d", rec.key, k))
	}
}

// serverVar reports a name whose stated type (GR-H2's typing: a parameter
// or var declared, or a := of a composite literal) is net/http's Server or
// *Server.
func (l *goLinker) serverVar(at goAt, name string) bool {
	t, file := l.typeOf(at, &facts.GoExpr{Kind: facts.GoIdentifier, Name: name})
	if t == nil {
		return false
	}
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = t.Object
	}
	if t.Kind != facts.GoSelector || t.Name != "Server" || t.Object == nil || t.Object.Kind != facts.GoIdentifier {
		return false
	}
	imp, ok := l.imports[file][t.Object.Name]
	return ok && imp.framework == goNetHTTP
}

// goServeState is what a serving position has applied on the way to a
// value: StripPrefix prefixes, wrappers, conditional bindings.
type goServeState struct {
	strip       string
	resolution  string
	wrappers    []string
	conditional bool
	blocked     string
	// returns is set inside a module function's returns: a parameter's
	// object there depends on the call and is not recorded.
	returns bool
	// context names the path-preserving wrappers on the way that serve
	// r.WithContext (GR-O7): they are wrappers (GR-V5) for a chi root.
	context []string
	// methods are the request methods the wrappers on the way answer
	// themselves.
	methods []string
}

// serveAt records the router objects a serving position's handler h
// serves (GR-C6).
func (l *goLinker) serveAt(at goAt, span contract.Span, h *facts.GoExpr, site string) {
	l.serveValue(at, span, h, goServeState{resolution: "literal", conditional: conditional(at)}, site, map[string]bool{})
}

// serveValue follows a served value: through the bindings that may reach a
// name, & and ServeHTTP, conversions and the path-preserving net/http
// handlers, StripPrefix (its constant prefix is stripped first), wrapper
// calls (named, GR-V5) and the returns of a module function. A router used
// inside a function literal is wrapped (no path).
func (l *goLinker) serveValue(at goAt, span contract.Span, e *facts.GoExpr, st goServeState, site string, seen map[string]bool) {
	if e == nil {
		return
	}
	add := func(v goValue) {
		for _, id := range v.objects {
			if st.returns && strings.HasPrefix(id, "param:") {
				continue
			}
			wrappers := st.wrappers
			if len(st.context) > 0 && l.objects[id] != nil && l.objects[id].framework == goChi {
				wrappers = append(append([]string(nil), wrappers...), st.context...)
			}
			s := goServe{object: id, strip: st.strip, resolution: st.resolution, wrappers: wrappers, conditional: st.conditional, file: at.rec.file, blocked: st.blocked, methods: goUnion(st.methods, v.methods)}
			if !l.servedKeys[s.key()] {
				l.servedKeys[s.key()] = true
				l.served[id] = append(l.served[id], s)
			}
		}
	}
	switch e.Kind {
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		if n.kind != "local" {
			add(copied(l.eval(at, e, site)))
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
			bat := goAt{n.rec, bd.Block, goStart(bd.Span)}
			next := st
			next.conditional = st.conditional || conditional(bat)
			if len(bd.Values) != len(bd.Names) {
				add(copied(l.bindingValue(n.rec, j, e.Name)))
				continue
			}
			l.serveValue(bat, span, &bd.Values[hasName(bd.Names, e.Name)], next, key, seen)
			delete(seen, key)
		}
	case facts.GoUnary:
		if e.Value == "&" {
			if v := l.eval(at, e, site); v.proven() {
				add(v)
				return
			}
			l.serveValue(at, span, e.Object, st, site+"/&", seen)
		}
	case facts.GoSelector:
		if e.Name == "ServeHTTP" {
			l.serveValue(at, span, e.Object, st, site+"/o", seen)
			return
		}
		add(copied(l.eval(at, e, site)))
	case facts.GoCall:
		if v := copied(l.eval(at, e, site)); v.proven() {
			add(v)
			return
		}
		c := e.Callee
		if c != nil && c.Kind == facts.GoSelector {
			if imp, ok := l.importOf(at, c.Object); ok && imp.framework == goNetHTTP {
				switch {
				case c.Name == "StripPrefix" && len(e.Args) == 2:
					l.composed[e] = true
					l.serveValue(at, span, &e.Args[1], l.stripped(at, span, &e.Args[0], st, site), site+"/a1", seen)
					return
				case goPathPreserving[c.Name] && len(e.Args) >= 1:
					l.serveValue(at, span, &e.Args[0], st, site+"/a0", seen)
					return
				}
			}
		}
		if param, context, form, methods, ok := l.wrapperDecl(at, e); ok {
			// A path-preserving wrapper (GR-O7) serves its handler
			// argument's routers as they are (chi roots behind one that
			// serves r.WithContext excepted); its other arguments are
			// inside a wrapper call.
			l.tag("GR-O7")
			l.tag("GR-O7/" + form)
			inner := st
			inner.methods = goUnion(st.methods, methods)
			if context {
				inner.context = append(append([]string(nil), st.context...), goCalleeText(c))
			}
			other := st
			other.wrappers = append(append([]string(nil), st.wrappers...), goCalleeText(c))
			for i := range e.Args {
				next := other
				if i == param {
					next = inner
				}
				l.serveValue(at, span, &e.Args[i], next, fmt.Sprintf("%s/a%d", site, i), seen)
			}
			return
		}
		next := st
		next.wrappers = append(append([]string(nil), st.wrappers...), goCalleeText(c))
		for i := range e.Args {
			l.serveValue(at, span, &e.Args[i], next, fmt.Sprintf("%s/a%d", site, i), seen)
		}
		if rec := l.calledRecord(at, e); rec != nil {
			key := rec.key + "/serve-returns"
			if seen[key] {
				return
			}
			seen[key] = true
			defer delete(seen, key)
			ret := st
			ret.returns = true
			for k, r := range rec.fn.Returns {
				for i := range r.Values {
					l.serveValue(goAt{rec, r.Block, goStart(r.Span)}, span, &r.Values[i], ret, fmt.Sprintf("%s/r%d/%d", rec.key, k, i), seen)
				}
			}
		}
	case facts.GoFuncLit:
		// A closure at a serving position may route on the path itself:
		// no path is claimed through it (as at a composition position).
		if l.markWrappedText(at, e, "func(...){...}", at.rec.file, site, span) {
			l.count(at.rec.file, site, goWrappedRouter)
		}
	}
}

// stripped applies a StripPrefix prefix to a serving state: a constant
// prefix that starts with '/', does not end with '/' and holds no template
// syntax is appended; "" changes nothing (StripPrefix returns the handler
// itself); any other prefix blocks the path (route_prefix_unresolved).
func (l *goLinker) stripped(at goAt, span contract.Span, p *facts.GoExpr, st goServeState, site string) goServeState {
	if st.blocked != "" {
		return st
	}
	text, literal, ok := l.constant(at, p)
	reason := ""
	switch {
	case !ok:
		reason = "prefix is not a constant"
	case text == "":
		return st
	case !strings.HasPrefix(text, "/"):
		reason = "prefix does not start with '/', so no request path has it"
	case strings.HasSuffix(text, "/"):
		reason = "prefix ends with '/', so the inner handler sees a path without its leading '/'"
	case strings.ContainsAny(text, "{}*:"):
		reason = "prefix holds template syntax"
	}
	if reason != "" {
		st.blocked = "route_prefix_unresolved"
		key := at.rec.file + "\x00" + site + "\x00strip"
		if !l.counted[key] {
			l.counted[key] = true
			name := goArgText(*p)
			if name == "" {
				name = goExprText(p)
			}
			l.diagnostic(at.rec.file, "route_prefix_unresolved", "http.StripPrefix("+name+", ...) ("+reason+"; no path is claimed through it)", span)
		}
		return st
	}
	st.strip += text
	if !literal {
		st.resolution = weaker(st.resolution, "constant")
	}
	return st
}

// testServe reports a serving position in a test-scope file or reached
// through a binding in one.
func (l *goLinker) testServe(file string, via []string) bool {
	if l.scope(file) != "" {
		return true
	}
	for _, f := range via {
		if l.scope(f) != "" {
			return true
		}
	}
	return false
}

// goCalleeText names a wrapper call's callee (GR-V5's served_through).
func goCalleeText(c *facts.GoExpr) string {
	if c == nil {
		return "?"
	}
	return goExprText(c)
}

// goExprText renders a reduced expression's names: identifiers, selectors
// and calls (with their arguments elided).
func goExprText(e *facts.GoExpr) string {
	if e == nil {
		return "?"
	}
	if e.Text != "" {
		return e.Text
	}
	switch e.Kind {
	case facts.GoIdentifier:
		return e.Name
	case facts.GoSelector:
		return goExprText(e.Object) + "." + e.Name
	case facts.GoCall:
		return goExprText(e.Callee) + "(...)"
	case facts.GoString:
		return fmt.Sprintf("%q", e.Value)
	case facts.GoFuncLit:
		return "func(...){...}"
	}
	return "?"
}

// serveStarts are the chain starts of a root object from its serving
// positions (GR-C6): its own and those of the parameters bound to it
// (transitively), each a start with the stripped prefix and the wrappers.
// all reports that every serving position blocks the path. nil without a
// serving position: the root is served at its own paths (ADR 0016). A
// serving position in a test-scope file (or reached through a binding in
// one) does not serve a production root: a test serving a production
// router says nothing about where production serves it.
func (l *goLinker) serveStarts(id string) (starts []goServe, blocked bool) {
	production := l.scope(l.objects[id].file) == ""
	type item struct {
		id          string
		conditional bool
		via         []string
	}
	queue := []item{{id: id}}
	seen := map[string]bool{id: true}
	any := false
	blocked = true
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		for _, s := range l.served[it.id] {
			if production && l.testServe(s.file, it.via) {
				continue
			}
			any = true
			if s.blocked != "" {
				continue
			}
			blocked = false
			s.object = id
			s.conditional = s.conditional || it.conditional
			if s.strip == "" && len(s.wrappers) == 0 {
				// A direct serving position adds no fact to the root's
				// own paths (a root is one whether or not a visible
				// position serves it, ADR 0016): its context is not
				// carried.
				s.conditional = false
			}
			s.via = it.via
			starts = append(starts, s)
		}
		for _, b := range l.bindings {
			if b.object == it.id && !seen[b.param] {
				seen[b.param] = true
				queue = append(queue, item{id: b.param, conditional: it.conditional || b.conditional, via: append(append([]string(nil), it.via...), b.file)})
			}
		}
	}
	if !any {
		return nil, false
	}
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].key() < starts[j].key() })
	return starts, blocked
}

package routes

import (
	"sort"
	"strconv"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// This file is Express row DA-E3 (T026, docs/extract/auth.md):
// middleware registered inside a module function that receives the app or
// router as a parameter, read at the one place that function is called
// with the proven object. RealWorld's `configureMiddlewares({ app })` is
// the form: a plain call runs the function body to its end before the
// next statement, so the body's `use` calls push their layers onto the
// object's stack at the call's position, in body order. Every condition
// below must hold, else the call stays an escape ("passed to <callee>,
// which may register middleware", DAD) as before:
//
//   - the call is at the top level of its file and not guarded (branch,
//     loop, try, short-circuit);
//   - the callee resolves (locally or through the import graph) to a
//     function declaration or a const function, not async or a generator;
//   - the object is passed as a positional argument to an identifier
//     parameter, or as a property of an object literal argument to an
//     object pattern parameter that names that key;
//   - this is the only call of the function with that object anywhere;
//   - in the body, the parameter is not reassigned, not shadowed where it
//     is used, not bound to another name, not returned, not passed to
//     another call, used in no nested function, and every use of it is a
//     `p.use(...)` statement that mounts no router or app.
//
// A body `use` under a branch, loop or try, or after a return or throw
// in the body, is an unknown entry ("conditional registration").

// inlineKey names one call site's mapping of one object.
type inlineKey struct {
	file   string
	span   contract.Span
	object string
}

// inlineCandidate is one call that passes a proven object to a function
// parameter.
type inlineCandidate struct {
	file   string
	call   facts.ClientCall
	fn     value
	object string
	param  string
	callee string
}

// inlineCalls applies DA-E3 and records the calls it read, so addEscapes
// leaves them out.
func (x *expressAuth) inlineCalls() map[inlineKey]bool {
	var candidates []inlineCandidate
	count := map[string]int{}
	for _, file := range facts.Paths(x.r.files) {
		data := x.r.files[file].Routes
		if data == nil {
			continue
		}
		for _, cc := range data.Clients {
			e := cc.Expression
			if e.Kind != "call_expression" || e.Object == nil || e.Object.Kind != "identifier" {
				continue
			}
			fn := x.r.resolve(file, cc.Scope, *e.Object, 0, nil)
			if fn.kind != "function" {
				continue
			}
			fnScope := x.r.scopeOf(fn)
			for i, a := range e.Args {
				var params []facts.RouteParam
				if fnScope != nil {
					params = fnScope.Params
				}
				var param facts.RouteParam
				if i < len(params) {
					param = params[i]
				}
				add := func(object, name string) {
					count[fn.file+"#"+strconv.Itoa(fn.scope)+"#"+object]++
					candidates = append(candidates, inlineCandidate{file: file, call: cc, fn: fn, object: object, param: name, callee: whitespace(e.Object.Text)})
				}
				if v := x.r.resolve(file, cc.Scope, a, 0, nil); (a.Kind == "identifier" || a.Kind == "member_expression") && (v.kind == "app" || v.kind == "router") {
					add(v.key, param.Name)
					continue
				}
				if a.Kind != "object" {
					continue
				}
				keys := make([]string, 0, len(a.Properties))
				for k := range a.Properties {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					p := a.Properties[k]
					if p.Kind != "identifier" && p.Kind != "member_expression" {
						continue
					}
					v := x.r.resolve(file, cc.Scope, p, 0, nil)
					if v.kind != "app" && v.kind != "router" {
						continue
					}
					name := ""
					if !param.Open && !a.Spread {
						name = param.Props[k]
					}
					add(v.key, name)
				}
			}
		}
	}
	read := map[inlineKey]bool{}
	for _, c := range candidates {
		if count[c.fn.file+"#"+strconv.Itoa(c.fn.scope)+"#"+c.object] != 1 {
			continue
		}
		events, ok := x.inlineEvents(c)
		if !ok {
			continue
		}
		tagAuth("DA-E3/call")
		x.events[c.object] = append(x.events[c.object], events...)
		read[inlineKey{c.file, c.call.Span, c.object}] = true
	}
	return read
}

// scopeOf is the function scope of a function value, or nil.
func (r *resolver) scopeOf(fn value) *facts.RouteScope {
	data := r.files[fn.file].Routes
	if data == nil || fn.scope <= 0 || fn.scope >= len(data.Scopes) || !data.Scopes[fn.scope].Function {
		return nil
	}
	return &data.Scopes[fn.scope]
}

// inlineEvents checks DA-E3's conditions for one candidate and returns the
// body's use events placed at the call, in body order.
func (x *expressAuth) inlineEvents(c inlineCandidate) ([]*expressEvent, bool) {
	if c.param == "" || c.call.Guarded || x.functionScope(c.file, c.call.Scope) != -1 {
		return nil, false
	}
	fnScope := x.r.scopeOf(c.fn)
	if fnScope == nil || fnScope.Async {
		return nil, false
	}
	g, s := c.fn.file, c.fn.scope
	data := x.r.files[g].Routes
	if b, ok := fnScope.Bindings[c.param]; !ok || b.Value.Kind != "parameter" {
		// Reassigned (the extractor clears the binding) or not a parameter.
		return nil, false
	}
	for _, r := range c.fn.returns {
		if mentions(r, c.param) {
			return nil, false
		}
	}
	// No other binding in the body names the parameter (an alias).
	for i := range data.Scopes {
		if !x.within(g, i, s) {
			continue
		}
		for name, b := range data.Scopes[i].Bindings {
			if i == s && name == c.param {
				continue
			}
			if mentions(b.Value, c.param) && x.refersTo(g, i, c.param, s) {
				return nil, false
			}
		}
	}
	// The parameter is passed to no call in the body.
	for _, cc := range data.Clients {
		if !x.within(g, cc.Scope, s) {
			continue
		}
		e := cc.Expression
		if e.Kind != "call_expression" {
			continue
		}
		for _, a := range e.Args {
			if (mentions(a, c.param) || objectMentions(a, c.param)) && x.refersTo(g, cc.Scope, c.param, s) {
				return nil, false
			}
		}
	}
	place := &anchor{file: c.file, scope: c.call.Scope, span: c.call.Span}
	var out []*expressEvent
	for _, rc := range data.Calls {
		if !x.within(g, rc.Scope, s) || baseName(rc.Receiver) != c.param || !x.refersTo(g, rc.Scope, c.param, s) {
			continue
		}
		if rc.Method != "use" || rc.Receiver.Kind != "identifier" || x.functionScope(g, rc.Scope) != s {
			// A chained call, another method on it, or a use in a nested
			// function: not read.
			if rc.Method == "use" || rc.Receiver.Kind != "identifier" || x.functionScope(g, rc.Scope) != s {
				return nil, false
			}
			continue
		}
		ev := x.useEvent(g, rc, c.object)
		for _, a := range ev.args {
			if a.mount != "" {
				return nil, false
			}
		}
		ev.place = place
		ev.via = c.callee
		if !ev.conditional && (rc.Guarded || exitBefore(fnScope.Exits, rc.Span)) {
			ev.conditional = true
			ev.exit = exitBefore(fnScope.Exits, rc.Span)
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool { return before(out[i].span, out[j].span) })
	return out, true
}

// within reports whether scope is s or nested inside it in file.
func (x *expressAuth) within(file string, scope, s int) bool {
	data := x.r.files[file].Routes
	for scope >= 0 && data != nil && scope < len(data.Scopes) {
		if scope == s {
			return true
		}
		scope = data.Scopes[scope].Parent
	}
	return false
}

// refersTo reports whether name at scope binds to the parameter in s.
func (x *expressAuth) refersTo(file string, scope int, name string, s int) bool {
	_, at, ok := x.r.binding(file, scope, name)
	return ok && at == s
}

// exitBefore reports a return or throw of the body before span.
func exitBefore(exits []contract.Span, s contract.Span) bool {
	for _, e := range exits {
		if before(e, s) {
			return true
		}
	}
	return false
}

// baseName is the identifier a receiver chain starts from, or "".
func baseName(e facts.RouteExpr) string {
	for {
		switch e.Kind {
		case "identifier":
			return e.Name
		case "member_expression", "call_expression":
			if e.Object == nil {
				return ""
			}
			e = *e.Object
		default:
			return ""
		}
	}
}

// mentions reports whether e is name or a member or call chain on it.
func mentions(e facts.RouteExpr, name string) bool {
	return baseName(e) == name
}

// objectMentions reports whether an object or array literal holds name
// (at any depth).
func objectMentions(e facts.RouteExpr, name string) bool {
	for _, p := range e.Properties {
		if mentions(p, name) || objectMentions(p, name) {
			return true
		}
	}
	for _, p := range e.Parts {
		if mentions(p, name) || objectMentions(p, name) {
			return true
		}
	}
	for _, p := range e.Args {
		if mentions(p, name) || objectMentions(p, name) {
			return true
		}
	}
	return false
}

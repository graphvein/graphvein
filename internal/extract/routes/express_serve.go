package routes

import (
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// site is the entry site of an Express call: its function body (the
// nearest function scope) and its start.
func (r *resolver) site(file string, c facts.RouteCall) entrySite {
	at := c.Scope
	if data := r.files[file].Routes; data != nil {
		for at > 0 && at < len(data.Scopes) && !data.Scopes[at].Function {
			at = data.Scopes[at].Parent
		}
	}
	return entrySite{file: file, domain: at, line: c.Span.StartLine, column: c.Span.StartColumn}
}

// nodeHTTPModules are the Node modules whose createServer serves a
// request listener at its own root.
var nodeHTTPModules = map[string]bool{"http": true, "https": true, "node:http": true, "node:https": true}

// servedApps lists the apps a serving form proves are served at their own
// root, each with whether every such form is conditional (a
// call under a branch, loop, try or short-circuit, or inside a function
// body). The closed list of forms: `x.listen(...)`;
// `http.createServer(..., x, ...).listen(...)` and `s.listen(...)` with
// `s` a const binding of such a createServer call (Node's http and https, also as
// `node:` specifiers), x a proven app. A form in a test-scope file serves
// only a test-scope app.
func (r *resolver) servedApps(rules testscope.Rules) map[string]bool {
	served := map[string]bool{}
	before := map[string]bool{}
	for key := range r.objects {
		before[key] = true
	}
	serve := func(file string, cc facts.ClientCall, e facts.RouteExpr) {
		v := r.resolve(file, cc.Scope, e, 0, nil)
		if v.kind != "app" || !before[v.key] {
			return
		}
		if testscope.Classify(file, rules) == testscope.Test && testscope.Classify(r.objectFiles[v.key], rules) != testscope.Test {
			return
		}
		conditional := cc.Guarded || r.site(file, facts.RouteCall{Scope: cc.Scope}).domain != 0
		if old, seen := served[v.key]; seen {
			conditional = conditional && old
		}
		served[v.key] = conditional
	}
	createServer := func(file string, scope int, e facts.RouteExpr) []facts.RouteExpr {
		if e.Kind != "call_expression" || e.Object == nil {
			return nil
		}
		callee := r.resolve(file, scope, *e.Object, 0, nil)
		if callee.kind != "import" || !nodeHTTPModules[callee.file] || callee.name != "createServer" && callee.name != "default.createServer" {
			return nil
		}
		return e.Args
	}
	for _, file := range facts.Paths(r.files) {
		data := r.files[file].Routes
		if data == nil {
			continue
		}
		for _, cc := range data.Clients {
			e := cc.Expression
			if e.Kind != "call_expression" || e.Object == nil || e.Object.Kind != "member_expression" || e.Object.Name != "listen" || e.Object.Object == nil {
				continue
			}
			receiver := *e.Object.Object
			args := createServer(file, cc.Scope, receiver)
			if args == nil && receiver.Kind == "identifier" {
				if b, at, ok := r.binding(file, cc.Scope, receiver.Name); ok && !b.Mutable && b.Module == "" {
					args = createServer(file, at, b.Value)
				}
			}
			if args == nil {
				serve(file, cc, receiver)
				continue
			}
			for _, a := range args {
				serve(file, cc, a)
			}
		}
	}
	// Resolving may create objects (an inline express() call): they have
	// no binding a route could be registered on.
	for key := range r.objects {
		if !before[key] {
			delete(r.objects, key)
			delete(r.objectFiles, key)
		}
	}
	return served
}

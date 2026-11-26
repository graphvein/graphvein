package golang

import (
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the router syntax of task T023 (ADR 0076):
// in files that pass the route gate, the functions and function literals
// that mention router syntax, with their signatures, lexical blocks, the
// bindings of the names their calls use, the router-named calls and the
// calls that pass a value on, and their returns; package-level vars that
// mention router syntax or that those functions use; and, in every
// non-test file, the single named result type of each function. It records
// syntax only (rule 31): which object is a router, which call registers a
// route, which path it serves and which handler it names are decided at
// link time under the claim contract of docs/contract/claims/go-http-routers.md.
// The payload table in docs/lang/go-routes.md names every capture branch
// below by its row id, and TestGoRoutePayloadContract keeps code and table
// 1:1.

// goRouteTag is a test-only hook: every capture branch reports the payload
// row it serves and the record it produced. It is nil in production.
var goRouteTag func(row string, record any)

func tagRoute(row string, record any) {
	if goRouteTag != nil {
		goRouteTag(row, record)
	}
}

// routerMethods are the selector names of the registration, composition and
// object calls of net/http's ServeMux, chi v5 and gin v1 (contract rows
// GR-O1-GR-O3, GR-R1-GR-R4, GR-C1). A call with one of these selectors is a
// router call candidate whatever its receiver is: the receiver is proven
// at link time. Use, NotFound, MethodNotAllowed, NoRoute and NoMethod are
// not modelled and are not captured as calls of their own.
var routerMethods = map[string]bool{
	"Handle": true, "HandleFunc": true,
	"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true, "Head": true, "Options": true, "Connect": true, "Trace": true,
	"Method": true, "MethodFunc": true, "Route": true, "Group": true, "With": true, "Mount": true, "Use": true,
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true,
	"Any": true, "Match": true, "Static": true, "StaticFS": true, "StaticFile": true, "StaticFileFS": true,
}

// routerTypes are the router type names a parameter may declare (contract
// row GR-O4), qualified by a router import.
var routerTypes = map[string]bool{"ServeMux": true, "Router": true, "Mux": true, "Engine": true, "RouterGroup": true, "IRouter": true, "IRoutes": true}

// routerMembers are the router package members whose mention makes a
// function a router function: constructors and the DefaultServeMux API
// (contract row GR-K2).
var routerMembers = map[string]bool{"NewServeMux": true, "DefaultServeMux": true, "Handle": true, "HandleFunc": true, "NewRouter": true, "NewMux": true, "New": true, "Default": true}

// httpGateMembers are the net/http members whose mention gates a file that
// imports net/http (task T023, decision 10; the serving members since step
// GR4: a router served under http.StripPrefix or through a wrapper, rows
// GR-C3 and GR-C6).
var httpGateMembers = map[string]bool{"ServeMux": true, "NewServeMux": true, "DefaultServeMux": true, "Handle": true, "HandleFunc": true,
	"StripPrefix": true, "ListenAndServe": true, "ListenAndServeTLS": true, "Serve": true, "ServeTLS": true, "Server": true}

// serveMembers are the net/http functions that serve a handler (contract
// row GR-C6: the handler argument is a serving position).
var serveMembers = map[string]bool{"ListenAndServe": true, "ListenAndServeTLS": true, "Serve": true, "ServeTLS": true}

// goBuiltinFuncs are the predeclared functions; a call of one is never a
// module function call.
var goBuiltinFuncs = map[string]bool{"append": true, "cap": true, "clear": true, "close": true, "complex": true, "copy": true, "delete": true, "imag": true, "len": true, "make": true, "max": true, "min": true, "new": true, "panic": true, "print": true, "println": true, "real": true, "recover": true}

// goPredeclaredTypes are the predeclared type names; a result of one is no
// module named type.
var goPredeclaredTypes = map[string]bool{"any": true, "bool": true, "byte": true, "comparable": true, "complex64": true, "complex128": true, "error": true, "float32": true, "float64": true, "int": true, "int8": true, "int16": true, "int32": true, "int64": true, "rune": true, "string": true, "uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true}

// routerPackage returns the package name and gate row of a router import
// path: net/http, gin, chi v5 and earlier chi paths (GR-K1 refuses those at
// link time, counted).
func routerPackage(module string) (name, row string) {
	switch {
	case module == "net/http":
		return "http", "RG-http"
	case module == "github.com/gin-gonic/gin":
		return "gin", "RG-gin"
	case module == "github.com/go-chi/chi":
		return "chi", "RG-chi"
	case strings.HasPrefix(module, "github.com/go-chi/chi/v"):
		if _, err := strconv.Atoi(strings.TrimPrefix(module, "github.com/go-chi/chi/v")); err == nil {
			return "chi", "RG-chi"
		}
	}
	return "", ""
}

// routeCapture holds the per-file state of one router capture.
type routeCapture struct {
	source []byte
	// names are the file's local names of router imports, by package name
	// (http, chi, gin); dot and blank imports bind none.
	names map[string]string
	// imports are the local names of every import of the file, stdlib
	// those of standard-library imports.
	imports map[string]bool
	stdlib  map[string]bool
	out     *facts.GoRoutes
	// records maps a captured function literal to its record index.
	records map[*parse.Node]int
	// relevance memoizes relevant per function node.
	relevance map[*parse.Node]bool
	// uses are the identifiers each record's kept syntax uses, by record.
	uses map[int]map[string]bool
	// parent and block place a literal captured while reducing (one in a
	// package-level var's value: parent 0); maybe is the reduced record's
	// router filter (nil for a package-level var).
	parent, block int
	maybe         *maybeRouter
	// http is the file's local name of net/http ("" without one).
	http string
}

// captureRouteSyntax adds the router syntax of one file to its Go payload:
// Debug for every file, Signatures for every non-test file and Routes for a
// file that passes the route gate.
func captureRouteSyntax(g *facts.GoFacts, file string, source []byte, root *parse.Node, imports []facts.Import) {
	g.Debug = debugLines(source)
	if root == nil {
		return
	}
	test := strings.HasSuffix(file, "_test.go")
	if !test {
		g.Signatures = signatures(root)
	}
	g.Routes = captureRoutes(source, root, imports, test)
	if g.Routes != nil {
		g.Routes.Generated = generatedServer(source, imports)
	}
	g.Wrappers, g.Requests = captureRequestSyntax(root, imports)
	g.Names = nameUses(root, imports)
	resultUses(g, root, imports)
}

// debugLines returns the header's //go:debug lines that set httpmuxgo121
// (row RD-debug; assumption GR-V2). go:debug directives are read only
// before the package clause.
func debugLines(source []byte) []string {
	var out []string
	for _, l := range strings.Split(string(source), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "package ") || t == "package" {
			break
		}
		if strings.HasPrefix(t, "//go:debug") && strings.Contains(t, "httpmuxgo121") {
			tagRoute("RD-debug", struct{ Debug string }{t})
			out = append(out, t)
		}
	}
	return out
}

// signatures records the functions without receiver that declare exactly
// one result of a named type (row RS-signature).
func signatures(root *parse.Node) []facts.GoSignature {
	var out []facts.GoSignature
	for _, d := range root.Children {
		if d.Kind != "function_declaration" {
			continue
		}
		name := facts.Text(d.Fields["name"])
		t := singleResult(d.Fields["result"])
		if name == "" || t == nil || !namedResult(t) {
			tagRoute("RS-D", d.Kind)
			continue
		}
		s := facts.GoSignature{Name: name, Result: reduce(t, 0), Line: line(d)}
		tagRoute("RS-signature", s)
		out = append(out, s)
	}
	return out
}

// singleResult returns the type of a result list that declares exactly one
// result, or nil.
func singleResult(list *parse.Node) *parse.Node {
	if list == nil {
		return nil
	}
	if list.Kind != "parameter_list" {
		return list
	}
	params := kids(list)
	if len(params) != 1 || params[0].Kind != "parameter_declaration" || len(identifiers(params[0])) > 1 {
		return nil
	}
	return params[0].Fields["type"]
}

// namedResult reports a type T, *T, pkg.T or *pkg.T with T not predeclared.
func namedResult(t *parse.Node) bool {
	if t.Kind == "pointer_type" {
		t = firstChild(t)
	}
	if t == nil {
		return false
	}
	switch t.Kind {
	case "type_identifier":
		return !goPredeclaredTypes[t.Text]
	case "qualified_type":
		return true
	}
	return false
}

// captureRoutes applies the route gate and captures the file's router
// syntax; nil when the file does not pass the gate (row RG-D).
func captureRoutes(source []byte, root *parse.Node, imports []facts.Import, test bool) *facts.GoRoutes {
	c := &routeCapture{source: source, names: map[string]string{}, imports: map[string]bool{}, stdlib: map[string]bool{}, records: map[*parse.Node]int{}, relevance: map[*parse.Node]bool{}, uses: map[int]map[string]bool{}}
	gate := ""
	http := ""
	for _, imp := range imports {
		std := goStdlibRoots[strings.SplitN(imp.Module, "/", 2)[0]]
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias {
				// The package clause names an unaliased import; the last
				// path element is the usual name.
				alias = path.Base(imp.Module)
			}
			c.imports[alias] = true
			c.stdlib[alias] = c.stdlib[alias] || std
		}
		pkg, row := routerPackage(imp.Module)
		if pkg == "" {
			continue
		}
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias {
				alias = pkg
			}
			if alias != "_" && alias != "." {
				c.names[alias] = pkg
				if pkg == "http" {
					http = alias
				}
			}
		}
		if row == "RG-chi" || row == "RG-gin" && gate == "" {
			gate = row
		}
	}
	c.http = http
	if gate == "" && http != "" && mentions(root, http) {
		gate = "RG-http"
	}
	if gate == "" && c.callsEntry(root) {
		gate = "RG-generated"
	}
	if gate == "" {
		tagRoute("RG-D", struct{}{})
		return nil
	}
	c.out = &facts.GoRoutes{}
	tagRoute(gate, struct{ Routes bool }{true})
	for _, d := range root.Children {
		switch d.Kind {
		case "function_declaration", "method_declaration":
			if d.Fields["body"] != nil && c.relevant(d) {
				c.record(d, 0, 0)
			} else {
				tagRoute("RF-D", d.Kind)
			}
		case "const_declaration":
			if test {
				for _, spec := range specs(d, "const_spec") {
					c.constSpec(spec)
				}
			}
		}
	}
	used := map[string]bool{}
	for _, u := range c.uses {
		for name := range u {
			used[name] = true
		}
	}
	for _, d := range root.Children {
		if d.Kind != "var_declaration" {
			continue
		}
		for _, spec := range specs(d, "var_spec") {
			keep := c.trigger(spec)
			for _, name := range identifiers(spec) {
				keep = keep || used[name]
			}
			if !keep {
				tagRoute("RV-D", spec.Kind)
				continue
			}
			c.parent, c.block = 0, 0
			rd := reducer{route: c}
			v := facts.GoVar{Names: identifiers(spec), Line: line(spec)}
			if t := spec.Fields["type"]; t != nil {
				e := rd.reduce(t, 0)
				v.Type = &e
			}
			if list := spec.Fields["value"]; list != nil {
				for _, x := range kids(list) {
					v.Values = append(v.Values, rd.reduce(x, 0))
				}
			}
			tagRoute("RV-var", v)
			c.out.Vars = append(c.out.Vars, v)
		}
	}
	return c.out
}

// entryCall reports a call of a generated server's entry function with at
// least one argument (row RG-generated; contract row GR-G1): F(...) with F
// a name of OapiEntries that is no predeclared function, or pkg.F(...) with
// pkg the name of an import that is neither a standard-library nor a
// router package. The link pass decides whether F is one.
func (c *routeCapture) entryCall(n *parse.Node) bool {
	if n.Kind != "call_expression" {
		return false
	}
	args := n.Fields["arguments"]
	if args == nil || len(kids(args)) == 0 {
		return false
	}
	callee := n.Fields["function"]
	if callee == nil {
		return false
	}
	switch callee.Kind {
	case "identifier":
		return OapiEntries[callee.Text]
	case "selector_expression":
		op := callee.Fields["operand"]
		return op != nil && op.Kind == "identifier" && c.imports[op.Text] && !c.stdlib[op.Text] && c.names[op.Text] == "" && OapiEntries[facts.Text(callee.Fields["field"])]
	}
	return false
}

// callsEntry reports an entry call (entryCall) anywhere in a subtree.
func (c *routeCapture) callsEntry(n *parse.Node) bool {
	if c.entryCall(n) {
		return true
	}
	for _, ch := range n.Children {
		if c.callsEntry(ch) {
			return true
		}
	}
	return false
}

// mentions reports a selector or qualified type naming a net/http member of
// httpGateMembers through the file's net/http name (decision 10).
func mentions(n *parse.Node, http string) bool {
	switch n.Kind {
	case "selector_expression":
		if op := n.Fields["operand"]; op != nil && op.Kind == "identifier" && op.Text == http && httpGateMembers[facts.Text(n.Fields["field"])] {
			return true
		}
	case "qualified_type":
		if facts.Text(n.Fields["package"]) == http && httpGateMembers[facts.Text(n.Fields["name"])] {
			return true
		}
	}
	for _, ch := range n.Children {
		if mentions(ch, http) {
			return true
		}
	}
	return false
}

func (c *routeCapture) constSpec(spec *parse.Node) {
	names := identifiers(spec)
	vals := values(spec)
	if len(vals) != len(names) {
		tagRoute("RK-D", spec.Kind)
		return
	}
	for i, name := range names {
		if !stringShaped(vals[i]) {
			tagRoute("RK-D", spec.Kind)
			continue
		}
		k := facts.GoConst{Name: name, Type: typeOf(spec), Value: vals[i], Line: line(spec)}
		tagRoute("RK-const", k)
		c.out.Consts = append(c.out.Consts, k)
	}
}

// routerType reports a router type form: X.T or *X.T with X a router import
// name and T in routerTypes (contract row GR-O4).
func (c *routeCapture) routerType(t *parse.Node) bool {
	if t != nil && t.Kind == "pointer_type" {
		t = firstChild(t)
	}
	return t != nil && t.Kind == "qualified_type" && c.names[facts.Text(t.Fields["package"])] != "" && routerTypes[facts.Text(t.Fields["name"])]
}

// routerChildren are the router methods whose result is a router (chi
// Route, Group and With, gin Group and Use: contract rows GR-O1-GR-O3).
var routerChildren = map[string]bool{"Route": true, "Group": true, "With": true, "Use": true}

// serveMuxType reports X.ServeMux with X the file's net/http name, as a
// type or as a selector (the argument of new(http.ServeMux) may parse as
// either).
func (c *routeCapture) serveMuxType(t *parse.Node) bool {
	if t == nil {
		return false
	}
	var pkg, name string
	switch t.Kind {
	case "qualified_type":
		pkg, name = facts.Text(t.Fields["package"]), facts.Text(t.Fields["name"])
	case "selector_expression":
		if op := t.Fields["operand"]; op != nil && op.Kind == "identifier" {
			pkg, name = op.Text, facts.Text(t.Fields["field"])
		}
	}
	return pkg != "" && c.names[pkg] == "http" && name == "ServeMux"
}

// serverType reports X.Server with X the file's net/http name (a type or a
// selector): an http.Server composite literal's Handler field is a serving
// position (contract row GR-C6).
func (c *routeCapture) serverType(t *parse.Node) bool {
	if t == nil {
		return false
	}
	var pkg, name string
	switch t.Kind {
	case "qualified_type":
		pkg, name = facts.Text(t.Fields["package"]), facts.Text(t.Fields["name"])
	case "selector_expression":
		if op := t.Fields["operand"]; op != nil && op.Kind == "identifier" {
			pkg, name = op.Text, facts.Text(t.Fields["field"])
		}
	}
	return pkg != "" && c.names[pkg] == "http" && name == "Server"
}

// serveSyntax reports a selector naming net/http's StripPrefix or a serving
// function (serveMembers) through the file's net/http name.
func (c *routeCapture) serveSyntax(n *parse.Node) bool {
	if n == nil || n.Kind != "selector_expression" {
		return false
	}
	op := n.Fields["operand"]
	field := facts.Text(n.Fields["field"])
	return op != nil && op.Kind == "identifier" && c.names[op.Text] == "http" && (field == "StripPrefix" || serveMembers[field])
}

// serveCall reports a call of a standard-library function (x.F, x a
// standard-library import, net/http included) that is a net/http serving
// function or whose arguments hold a net/http StripPrefix or serving call
// outside function literals (row RC-serve): the serving positions and
// prefixes of contract rows GR-C3 and GR-C6. One in a binding's value or
// a return is part of that kept syntax instead.
func (c *routeCapture) serveCall(n *parse.Node) bool {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "selector_expression" {
		return false
	}
	op := callee.Fields["operand"]
	if op == nil || op.Kind != "identifier" || !c.stdlib[op.Text] {
		return false
	}
	if c.names[op.Text] == "http" && serveMembers[facts.Text(callee.Fields["field"])] {
		return true
	}
	return c.holdsServe(n.Fields["arguments"], false)
}

// holdsServe reports a net/http StripPrefix or serving call in a subtree
// outside function literals; with server set, also an http.Server
// composite literal (a binding kept for rows GR-C3 and GR-C6).
func (c *routeCapture) holdsServe(n *parse.Node, server bool) bool {
	if n == nil || n.Kind == "func_literal" {
		return false
	}
	switch n.Kind {
	case "call_expression":
		if callee := n.Fields["function"]; callee != nil && c.serveSyntax(callee) {
			return true
		}
	case "composite_literal":
		if server && c.serverType(n.Fields["type"]) {
			return true
		}
	}
	for _, ch := range n.Children {
		if c.holdsServe(ch, server) {
			return true
		}
	}
	return false
}

// handlerType reports the qualified type X.Handler with X the file's
// net/http name.
func (c *routeCapture) handlerType(t *parse.Node) bool {
	return t != nil && t.Kind == "qualified_type" && c.http != "" && facts.Text(t.Fields["package"]) == c.http && facts.Text(t.Fields["name"]) == "Handler"
}

// handlerResult reports a declared result X.Handler with X a router import
// name: a factory that may return a router (contract row GR-O5).
func (c *routeCapture) handlerResult(t *parse.Node) bool {
	return t != nil && t.Kind == "qualified_type" && c.names[facts.Text(t.Fields["package"])] != "" && facts.Text(t.Fields["name"]) == "Handler"
}

// trigger reports router syntax in a subtree outside function literals and
// router calls: a router type, or a router import member of routerMembers.
func (c *routeCapture) trigger(n *parse.Node) bool {
	switch n.Kind {
	case "func_literal":
		return c.relevant(n)
	case "qualified_type":
		if c.routerType(n) {
			return true
		}
	case "selector_expression":
		op := n.Fields["operand"]
		field := facts.Text(n.Fields["field"])
		if op != nil && op.Kind == "identifier" && c.names[op.Text] != "" && (routerMembers[field] || routerTypes[field]) {
			return true
		}
	case "call_expression":
		if callee := n.Fields["function"]; callee != nil && callee.Kind == "selector_expression" && routerMethods[facts.Text(callee.Fields["field"])] {
			return true
		}
	}
	for _, ch := range n.Children {
		if c.trigger(ch) {
			return true
		}
	}
	return false
}

// paramTypes maps the parameter and receiver names of a function node to
// their declared types.
func paramTypes(fn *parse.Node) map[string]*parse.Node {
	out := map[string]*parse.Node{}
	for _, field := range []string{"receiver", "parameters"} {
		list := fn.Fields[field]
		if list == nil {
			continue
		}
		for _, p := range kids(list) {
			for _, name := range identifiers(p) {
				out[name] = p.Fields["type"]
			}
		}
	}
	return out
}

// bound collects every name a function body declares or assigns, function
// literals included.
func bound(n *parse.Node, out map[string]bool) {
	switch n.Kind {
	case "short_var_declaration", "assignment_statement", "range_clause", "receive_statement":
		if left := n.Fields["left"]; left != nil {
			for _, x := range kids(left) {
				if x.Kind == "identifier" {
					out[x.Text] = true
				}
			}
		}
	case "var_spec", "const_spec":
		for _, name := range identifiers(n) {
			out[name] = true
		}
	case "type_switch_statement":
		if alias := n.Fields["alias"]; alias != nil {
			for _, name := range identifiers(alias) {
				out[name] = true
			}
		}
	}
	for _, ch := range n.Children {
		bound(ch, out)
	}
}

// excluded reports a router-named call whose receiver no rule can prove a
// router (rows GR-K3, GR-O4; row RC-excluded), so that it neither makes
// its function a router function nor is captured:
//   - the receiver is a package name of a non-router import (slog.Any: a
//     package function, never a method);
//   - the receiver chain starts at a parameter declared with a qualified
//     type that is no router type (*gin.Context, *http.Request,
//     *testing.T), never rebound in the body;
//   - the receiver is a parameter of another type (a module type, *Server),
//     or a call chain from it, never rebound: only a field of it may hold
//     a router (a struct field, receiver_unproven);
//   - the receiver is a local whose every binding holds a value that cannot
//     be a router (maybeRouter: a composite literal, a field, a call
//     rooted at such a parameter).
func (c *routeCapture) excluded(call *parse.Node, fr *frame) bool {
	callee := call.Fields["function"]
	if callee == nil || callee.Kind != "selector_expression" {
		return false
	}
	op := callee.Fields["operand"]
	root, viaField := op, false
	for root != nil {
		switch root.Kind {
		case "selector_expression":
			viaField = true
			root = root.Fields["operand"]
			continue
		case "index_expression":
			viaField = true
			root = root.Fields["operand"]
			continue
		case "call_expression":
			root = root.Fields["function"]
			if root != nil && root.Kind == "selector_expression" {
				// A method call: its receiver, not a field.
				root = root.Fields["operand"]
			}
			continue
		case "parenthesized_expression":
			root = firstChild(root)
			continue
		}
		break
	}
	if root == nil || root.Kind != "identifier" {
		return false
	}
	name := root.Text
	direct := op == root
	if t, ok := fr.params[name]; ok {
		if fr.rebound[name] || t == nil || c.routerType(t) {
			return false
		}
		if t.Kind == "pointer_type" {
			t = firstChild(t)
		}
		if t != nil && t.Kind == "qualified_type" {
			return true
		}
		return !viaField
	}
	if fr.m != nil && fr.m.bound[name] {
		return (direct || !viaField && !fr.m.typed[name]) && !fr.m.names[name] && fr.m.clear[name]
	}
	return direct && c.imports[name] && c.names[name] == ""
}

// routerCall reports a call whose callee is a selector named like a router
// method on a receiver no exclusion rules out.
func (c *routeCapture) routerCall(n *parse.Node, fr *frame) bool {
	return c.routerNamed(n) && !c.excluded(n, fr)
}

// routerNamed reports a call whose callee is a selector named like a router
// method.
func (c *routeCapture) routerNamed(n *parse.Node) bool {
	callee := n.Fields["function"]
	return callee != nil && callee.Kind == "selector_expression" && routerMethods[facts.Text(callee.Fields["field"])]
}

// scope collects a function's parameters, the names its body binds and the
// bindings of its body outside nested function literals, with the router
// filter over them.
func (c *routeCapture) scope(fn *parse.Node) *frame {
	fr := &frame{params: paramTypes(fn), rebound: map[string]bool{}}
	if body := fn.Fields["body"]; body != nil {
		bound(body, fr.rebound)
		var collect func(*parse.Node)
		collect = func(n *parse.Node) {
			if n.Kind == "func_literal" {
				return
			}
			fr.bindings = append(fr.bindings, bindingsOf(n, 0)...)
			for _, ch := range n.Children {
				collect(ch)
			}
		}
		collect(body)
	}
	fr.m = c.maybeRouter(fr)
	fr.bindings = nil
	return fr
}

// bindingsOf returns the bindings a node declares or assigns (rows RL-*),
// placed in block.
func bindingsOf(n *parse.Node, block int) []pendingBinding {
	switch n.Kind {
	case "range_clause":
		kind := facts.GoBindRangeAssign
		for _, tok := range n.Tokens {
			if tok == ":=" {
				kind = facts.GoBindRange
			}
		}
		if names := targetNames(n.Fields["left"]); names != nil {
			var vals []*parse.Node
			if r := n.Fields["right"]; r != nil {
				vals = []*parse.Node{r}
			}
			return []pendingBinding{{kind: kind, names: names, values: vals, block: block, span: n}}
		}
	case "short_var_declaration", "receive_statement":
		if left := n.Fields["left"]; left != nil {
			kind := facts.GoBindAssign
			for _, tok := range n.Tokens {
				if tok == ":=" {
					kind = facts.GoBindDefine
				}
			}
			if names := targetNames(left); names != nil {
				return []pendingBinding{{kind: kind, names: names, values: valueNodes(n.Fields["right"]), block: block, span: n}}
			}
		}
	case "assignment_statement":
		if isAssign(n) {
			// x.Handler = h: a server's handler set by an assignment
			// (row RL-handler; contract row GR-C6 decides whether x is
			// an http.Server).
			left, right := kids(n.Fields["left"]), valueNodes(n.Fields["right"])
			if len(left) == 1 && len(right) == 1 && left[0].Kind == "selector_expression" && facts.Text(left[0].Fields["field"]) == "Handler" {
				if op := left[0].Fields["operand"]; op != nil && op.Kind == "identifier" {
					return []pendingBinding{{kind: facts.GoBindHandler, names: []string{op.Text}, values: right, block: block, span: n}}
				}
			}
			if names := targetNames(n.Fields["left"]); names != nil {
				return []pendingBinding{{kind: facts.GoBindAssign, names: names, values: valueNodes(n.Fields["right"]), block: block, span: n}}
			}
		}
	case "var_spec", "const_spec":
		kind := facts.GoBindVar
		if n.Kind == "const_spec" {
			kind = facts.GoBindConst
		}
		return []pendingBinding{{kind: kind, names: identifiers(n), typ: n.Fields["type"], values: valueNodes(n.Fields["value"]), block: block, span: n}}
	case "type_switch_statement":
		if alias := n.Fields["alias"]; alias != nil {
			var vals []*parse.Node
			if v := n.Fields["value"]; v != nil {
				vals = []*parse.Node{v}
			}
			return []pendingBinding{{kind: facts.GoBindTypeSwitch, names: identifiers(alias), values: vals, block: block, span: alias}}
		}
	}
	return nil
}

// relevant reports a router function (rows RF-decl and RF-literal): a
// parameter or result of a router type, a result X.Handler, a router call
// (not excluded), a router import member of routerMembers, a router type
// anywhere in the body, or a function literal in the body that is
// relevant itself.
func (c *routeCapture) relevant(fn *parse.Node) bool {
	if r, ok := c.relevance[fn]; ok {
		return r
	}
	c.relevance[fn] = false
	result := false
	for _, t := range paramTypes(fn) {
		if t != nil && t.Kind == "pointer_type" {
			t = firstChild(t)
		}
		if c.routerType(t) || c.serverType(t) {
			result = true
		}
	}
	if list := fn.Fields["result"]; list != nil {
		if list.Kind != "parameter_list" {
			result = result || c.routerType(list) || c.handlerResult(list)
		} else {
			for _, p := range kids(list) {
				t := p.Fields["type"]
				result = result || c.routerType(t) || c.handlerResult(t)
			}
		}
	}
	body := fn.Fields["body"]
	if !result && body != nil {
		var fr *frame
		var scan func(*parse.Node) bool
		scan = func(n *parse.Node) bool {
			switch n.Kind {
			case "func_literal":
				return c.relevant(n)
			case "call_expression":
				if c.entryCall(n) {
					return true
				}
				if c.routerNamed(n) {
					if fr == nil {
						fr = c.scope(fn)
					}
					if !c.excluded(n, fr) {
						return true
					}
				}
			case "qualified_type":
				if c.routerType(n) || c.serverType(n) {
					return true
				}
			case "selector_expression":
				op := n.Fields["operand"]
				field := facts.Text(n.Fields["field"])
				if op != nil && op.Kind == "identifier" && c.names[op.Text] != "" && (routerMembers[field] || routerTypes[field]) {
					return true
				}
				if c.serveSyntax(n) {
					return true
				}
			}
			for _, ch := range n.Children {
				if scan(ch) {
					return true
				}
			}
			return false
		}
		result = scan(body)
	}
	c.relevance[fn] = result
	return result
}

// pending items of a record, reduced only when kept.
type pendingBinding struct {
	kind   string
	names  []string
	typ    *parse.Node
	values []*parse.Node
	block  int
	span   *parse.Node
}

type pendingCall struct {
	kind  string
	node  *parse.Node
	block int
	// serve marks a serving call (row RC-serve), kept whatever its
	// arguments are.
	serve bool
	// entry marks a generated server's entry call (row RC-generated),
	// kept whatever its arguments are.
	entry bool
	// context is where the call's value goes when no other kept syntax
	// holds the call (row RU-context).
	context string
}

type pendingReturn struct {
	node  *parse.Node
	block int
}

// frame is the state of one record while its body is walked.
type frame struct {
	index    int
	blocks   []facts.GoBlock
	params   map[string]*parse.Node
	rebound  map[string]bool
	bindings []pendingBinding
	calls    []pendingCall
	returns  []pendingReturn
	children []int
	results  []string
	// serving is set while the arguments of a captured serving call
	// (row RC-serve) are walked: a serving call inside is part of its tree.
	serving bool
	// m is the router filter over the body's bindings (scope).
	m *maybeRouter
	// body is the record's body; nodeBlock the block the walk placed each
	// visited node in (the escape scan, rows RU-*).
	body      *parse.Node
	nodeBlock map[*parse.Node]int
	// jumps is set by a goto or a labeled break or continue (row RJ-jump).
	jumps bool
}

func (fr *frame) block(kind string, parent int) int {
	fr.blocks = append(fr.blocks, facts.GoBlock{Parent: parent, Kind: kind})
	tagRoute("RB-"+kind, fr.blocks[len(fr.blocks)-1])
	return len(fr.blocks) - 1
}

// record captures one router function (a declaration, or a literal inside
// the record parent-1 at its block) and returns its index.
func (c *routeCapture) record(fn *parse.Node, parent, block int) int {
	index := len(c.out.Funcs)
	c.out.Funcs = append(c.out.Funcs, facts.GoRouteFunc{})
	f := facts.GoRouteFunc{Span: facts.Span(fn), Parent: parent, Block: block, Params: params(fn.Fields["parameters"]), Results: params(fn.Fields["result"])}
	row := "RF-literal"
	if fn.Kind == "func_literal" {
		c.records[fn] = index
	} else {
		row = "RF-decl"
		f.Name = facts.Text(fn.Fields["name"])
		f.Parent, f.Block = 0, 0
		if fn.Kind == "method_declaration" {
			_, _, f.Receiver = receiverType(fn)
		}
	}
	fr := c.scope(fn)
	fr.index = index
	if list := fn.Fields["result"]; list != nil && list.Kind == "parameter_list" {
		for _, p := range kids(list) {
			fr.results = append(fr.results, identifiers(p)...)
		}
	}
	body := fn.Fields["body"]
	fr.body = body
	fr.nodeBlock = map[*parse.Node]int{}
	fr.blocks = []facts.GoBlock{{Kind: facts.GoBlockBody}}
	tagRoute("RB-body", fr.blocks[0])
	c.walkChildren(fr, body, 0, false)
	c.finish(fr, &f)
	tagRoute(row, f)
	c.out.Funcs[index] = f
	return index
}

func (c *routeCapture) walkChildren(fr *frame, n *parse.Node, block int, kept bool) {
	for _, ch := range n.Children {
		c.walk(fr, ch, block, kept, false)
	}
}

// walk visits one node of a record's body: blocks, bindings, calls,
// returns and relevant function literals. kept is set inside the argument
// tree of a captured call, where only router calls are listed on their own;
// chain is set on the receiver chain of a router call, whose calls are part
// of its tree.
func (c *routeCapture) walk(fr *frame, n *parse.Node, block int, kept, chain bool) {
	if n == nil {
		return
	}
	if fr.nodeBlock != nil {
		if _, ok := fr.nodeBlock[n]; !ok {
			fr.nodeBlock[n] = block
		}
	}
	switch n.Kind {
	case "func_literal":
		if c.relevant(n) {
			if _, done := c.records[n]; !done {
				fr.children = append(fr.children, c.record(n, fr.index+1, block))
			}
		} else {
			tagRoute("RF-D", n.Kind)
		}
		return
	case "block":
		c.walkChildren(fr, n, fr.block(facts.GoBlockPlain, block), kept)
		return
	case "if_statement":
		b := fr.block(facts.GoBlockIf, block)
		for _, ch := range n.Children {
			switch ch {
			case n.Fields["consequence"]:
				c.walkChildren(fr, ch, fr.block(facts.GoBlockThen, b), kept)
			case n.Fields["alternative"]:
				e := fr.block(facts.GoBlockElse, b)
				if ch.Kind == "block" {
					c.walkChildren(fr, ch, e, kept)
				} else {
					c.walk(fr, ch, e, kept, false)
				}
			default:
				c.walk(fr, ch, b, kept, false)
			}
		}
		return
	case "for_statement":
		b := fr.block(facts.GoBlockFor, block)
		for _, ch := range n.Children {
			if ch == n.Fields["body"] {
				c.walkChildren(fr, ch, b, kept)
			} else {
				c.walk(fr, ch, b, kept, false)
			}
		}
		return
	case "expression_switch_statement", "type_switch_statement", "select_statement":
		b := block
		if n.Kind != "select_statement" {
			b = fr.block(facts.GoBlockSwitch, block)
		}
		fr.bindings = append(fr.bindings, bindingsOf(n, b)...)
		for _, ch := range n.Children {
			switch ch.Kind {
			case "expression_case", "default_case", "type_case", "communication_case":
				c.walkChildren(fr, ch, fr.block(facts.GoBlockCase, b), kept)
			default:
				if ch != n.Fields["alias"] {
					c.walk(fr, ch, b, kept, false)
				}
			}
		}
		return
	case "go_statement", "defer_statement":
		kind := facts.GoBlockGo
		if n.Kind == "defer_statement" {
			kind = facts.GoBlockDefer
		}
		c.walkChildren(fr, n, fr.block(kind, block), kept)
		return
	case "range_clause", "short_var_declaration", "receive_statement", "assignment_statement", "var_spec", "const_spec":
		list := bindingsOf(n, block)
		fr.bindings = append(fr.bindings, list...)
		// A serving call in a binding's value is part of the binding
		// (kept by holdsServe), not listed on its own.
		serving := fr.serving
		fr.serving = serving || len(list) > 0
		c.walkChildren(fr, n, block, kept)
		fr.serving = serving
		return
	case "goto_statement", "break_statement", "continue_statement":
		// A goto, or a break or continue naming a label, may skip any
		// statement of the body (row RJ-jump); a plain break or continue
		// only leaves the loop, switch or select it is in, whose body is
		// conditional context already.
		labeled := false
		for _, ch := range n.Children {
			labeled = labeled || ch.Kind == "label_name"
		}
		if n.Kind == "goto_statement" || labeled {
			fr.jumps = true
			tagRoute("RJ-jump", facts.GoRouteFunc{Jumps: true})
		}
		return
	case "return_statement":
		fr.returns = append(fr.returns, pendingReturn{node: n, block: block})
		// Returns are kept whole: a serving call in one is not listed.
		serving := fr.serving
		fr.serving = true
		c.walkChildren(fr, n, block, kept)
		fr.serving = serving
		return
	case "call_expression":
		kind := ""
		switch {
		case c.routerCall(n, fr):
			if !chain {
				kind = facts.GoCallRouter
			}
		case c.excludedCall(n, fr):
			tagRoute("RC-excluded", n.Kind)
		case !kept && !chain && !fr.serving && c.serveCall(n):
			// A serving call's arguments are walked as outside a captured
			// call: a plain or router call inside is still listed on its
			// own (row RC-serve).
			fr.calls = append(fr.calls, pendingCall{kind: facts.GoCallPlain, node: n, block: block, serve: true})
			fr.serving = true
			c.walkChildren(fr, n, block, kept)
			fr.serving = false
			return
		case !kept && !chain && !fr.serving && c.entryCall(n):
			fr.calls = append(fr.calls, pendingCall{kind: facts.GoCallPlain, node: n, block: block, entry: true})
			c.walkCall(fr, n, block, kept, chain, facts.GoCallPlain)
			return
		case !kept && !chain && c.passesValue(n):
			kind = facts.GoCallPlain
		default:
			tagRoute("RC-D", n.Kind)
		}
		if kind != "" {
			fr.calls = append(fr.calls, pendingCall{kind: kind, node: n, block: block})
		}
		c.walkCall(fr, n, block, kept, chain, kind)
		return
	}
	c.walkChildren(fr, n, block, kept)
}

// walkCall walks a call's children: the receiver chain of a router call is
// part of its tree; the arguments of a listed call are kept.
func (c *routeCapture) walkCall(fr *frame, n *parse.Node, block int, kept, chain bool, kind string) {
	router := kind == facts.GoCallRouter || chain
	inner := kept || kind != ""
	for _, ch := range n.Children {
		if ch == n.Fields["function"] && router && ch.Kind == "selector_expression" {
			// The receiver chain of a router call: a call that is the
			// selector's operand belongs to this call's tree.
			for _, part := range ch.Children {
				c.walk(fr, part, block, inner, part == ch.Fields["operand"] && part.Kind == "call_expression")
			}
			continue
		}
		c.walk(fr, ch, block, inner, false)
	}
}

// excludedCall reports a router-named call that excluded rules out.
func (c *routeCapture) excludedCall(n *parse.Node, fr *frame) bool {
	return c.routerNamed(n) && c.excluded(n, fr)
}

// passesValue reports a call of a module function or method candidate
// (moduleCallee) with an argument that may hold a router: a name, a
// selector, a call or & of one (row RC-call: a router passed to a module
// function, GR-O4).
func (c *routeCapture) passesValue(n *parse.Node) bool {
	if !c.moduleCallee(n.Fields["function"]) {
		return false
	}
	args := n.Fields["arguments"]
	if args == nil {
		return false
	}
	for _, a := range kids(args) {
		if valueShaped(a) {
			return true
		}
	}
	return false
}

// moduleCallee reports a callee the Go linker's call rules can resolve to a
// module function or method: a name other than a predeclared function, or
// x.F with x a name that is no standard-library or router import (an
// import of the standard library never names a module package).
func (c *routeCapture) moduleCallee(callee *parse.Node) bool {
	if callee == nil {
		return false
	}
	switch callee.Kind {
	case "identifier":
		return !goBuiltinFuncs[callee.Text]
	case "selector_expression":
		op := callee.Fields["operand"]
		return op != nil && op.Kind == "identifier" && !c.stdlib[op.Text] && c.names[op.Text] == ""
	}
	return false
}

// goStdlibRoots are the first path elements of the Go 1.27 standard
// library's packages.
var goStdlibRoots = map[string]bool{"archive": true, "bufio": true, "builtin": true, "bytes": true, "cmp": true, "compress": true, "container": true, "context": true, "crypto": true, "database": true, "debug": true, "embed": true, "encoding": true, "errors": true, "expvar": true, "flag": true, "fmt": true, "go": true, "hash": true, "html": true, "image": true, "index": true, "io": true, "iter": true, "log": true, "maps": true, "math": true, "mime": true, "net": true, "os": true, "path": true, "plugin": true, "reflect": true, "regexp": true, "runtime": true, "slices": true, "sort": true, "strconv": true, "strings": true, "structs": true, "sync": true, "syscall": true, "testing": true, "text": true, "time": true, "unicode": true, "unique": true, "unsafe": true, "weak": true}

func valueShaped(a *parse.Node) bool {
	switch a.Kind {
	case "identifier", "selector_expression", "call_expression", "type_conversion_expression":
		return true
	case "parenthesized_expression", "variadic_argument":
		if x := firstChild(a); x != nil {
			return valueShaped(x)
		}
	case "unary_expression":
		if operator(a) == "&" {
			if x := a.Fields["operand"]; x != nil {
				return valueShaped(x)
			}
		}
	}
	return false
}

// targetNames lists assignment or declaration targets by position: an
// identifier's name, "" for any other target; nil when no target is an
// identifier.
func targetNames(left *parse.Node) []string {
	if left == nil {
		return nil
	}
	var names []string
	any := false
	for _, x := range kids(left) {
		if x.Kind == "identifier" {
			names = append(names, x.Text)
			any = true
		} else {
			names = append(names, "")
		}
	}
	if !any {
		return nil
	}
	return names
}

func valueNodes(list *parse.Node) []*parse.Node {
	if list == nil {
		return nil
	}
	if list.Kind != "expression_list" {
		return []*parse.Node{list}
	}
	return kids(list)
}

// maybeRouter decides, per record, which names and values may hold a
// router object: the filter of rows RC-call and RE-closure. A value may
// hold one when it is a name that may, a call (other than a predeclared
// function or a method of a parameter no rule proves a router), a
// conversion or & of such a value, or a router package member
// (http.DefaultServeMux). A name may when it is a parameter of a router
// type (GR-O4), a name bound by a value that may, or a name the record
// neither declares nor binds (a package-level or enclosing name). Every
// other value (a literal, composite literal, field, index or slice
// expression, type assertion, channel receive, function literal, a
// parameter of another qualified type) is no proven router object (rows
// GR-K3, GR-O4), so no claim reads it as one.
type maybeRouter struct {
	c     *routeCapture
	fr    *frame
	names map[string]bool
	bound map[string]bool
	// typed are the locals declared `var x T`: the Go linker resolves
	// their method calls.
	typed map[string]bool
	// clear are the locals every binding of which holds a value that is
	// no router and no form GR-K3 refuses as receiver_unproven (clearValue).
	clear map[string]bool
}

func (c *routeCapture) maybeRouter(fr *frame) *maybeRouter {
	m := &maybeRouter{c: c, fr: fr, names: map[string]bool{}, bound: map[string]bool{}, typed: map[string]bool{}, clear: map[string]bool{}}
	for name, t := range fr.params {
		m.bound[name] = true
		if c.routerType(t) || c.handlerType(t) {
			// An http.Handler parameter may hold a router too (step GR8:
			// its uses are kept or recorded as escapes).
			m.names[name] = true
		}
	}
	for _, b := range fr.bindings {
		for _, name := range b.names {
			m.bound[name] = true
			if b.kind == facts.GoBindVar && b.typ != nil {
				m.typed[name] = true
				if c.routerType(b.typ) {
					m.names[name] = true
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, b := range fr.bindings {
			for i, name := range b.names {
				if name == "" || m.names[name] {
					continue
				}
				var v *parse.Node
				switch {
				case len(b.values) == len(b.names):
					v = b.values[i]
				case len(b.values) == 1:
					v = b.values[0]
				}
				if b.kind != facts.GoBindRange && b.kind != facts.GoBindRangeAssign && b.kind != facts.GoBindTypeSwitch && v != nil && m.value(v) {
					m.names[name], changed = true, true
				}
			}
		}
	}
	unclear := map[string]bool{}
	for _, b := range fr.bindings {
		for i, name := range b.names {
			var v *parse.Node
			switch {
			case len(b.values) == len(b.names):
				v = b.values[i]
			case len(b.values) == 1:
				v = b.values[0]
			}
			ok := false
			switch {
			case b.kind == facts.GoBindRange || b.kind == facts.GoBindRangeAssign || b.kind == facts.GoBindTypeSwitch:
			case v == nil:
				ok = b.kind == facts.GoBindVar && b.typ != nil && !c.routerType(b.typ)
			default:
				ok = m.clearValue(v)
			}
			if !ok {
				unclear[name] = true
			}
		}
	}
	for name := range m.bound {
		if fr.params[name] == nil && !unclear[name] {
			m.clear[name] = true
		}
	}
	return m
}

// clearValue reports a value that is no router object and no form GR-K3
// refuses as receiver_unproven (a field, an element, a type assertion, a
// receive): a literal, a function literal, an operator expression other
// than &, a composite literal of a type other than a router type, or a
// call the router filter rules out.
func (m *maybeRouter) clearValue(n *parse.Node) bool {
	switch n.Kind {
	case "interpreted_string_literal", "raw_string_literal", "int_literal", "float_literal", "imaginary_literal", "rune_literal", "true", "false", "nil", "func_literal", "binary_expression":
		return true
	case "parenthesized_expression":
		if x := firstChild(n); x != nil {
			return m.clearValue(x)
		}
	case "unary_expression":
		x := n.Fields["operand"]
		if operator(n) == "&" {
			return x != nil && x.Kind == "composite_literal" && m.clearValue(x)
		}
		return operator(n) != "<-"
	case "composite_literal":
		return !m.c.routerType(n.Fields["type"])
	case "call_expression":
		return !m.value(n)
	}
	return false
}

// name reports a name that may hold a router.
func (m *maybeRouter) name(name string) bool {
	return m.names[name] || !m.bound[name]
}

// value reports a value that may hold a router.
func (m *maybeRouter) value(n *parse.Node) bool {
	switch n.Kind {
	case "identifier":
		return m.name(n.Text)
	case "parenthesized_expression":
		if x := firstChild(n); x != nil {
			return m.value(x)
		}
	case "unary_expression":
		if x := n.Fields["operand"]; x != nil && operator(n) == "&" {
			return m.value(x)
		}
	case "type_conversion_expression":
		if x := n.Fields["operand"]; x != nil {
			return m.value(x)
		}
	case "selector_expression":
		op := n.Fields["operand"]
		return op != nil && op.Kind == "identifier" && m.c.names[op.Text] != "" && routerMembers[facts.Text(n.Fields["field"])]
	case "call_expression":
		callee := n.Fields["function"]
		if callee == nil {
			return false
		}
		if callee.Kind == "identifier" && callee.Text == "new" && !m.bound["new"] {
			// new(http.ServeMux) is a zero-value mux (GR-K4).
			args := kids(n.Fields["arguments"])
			return len(args) == 1 && m.c.serveMuxType(args[0])
		}
		if callee.Kind == "selector_expression" {
			op := callee.Fields["operand"]
			if op != nil && op.Kind == "call_expression" && routerChildren[facts.Text(callee.Fields["field"])] {
				// A router child of a call result (r.With(a).With(b),
				// g.Group("/a").Use(m)): a router when the inner call
				// may hold one (GR-O1-GR-O3).
				return m.value(op)
			}
			if op != nil && op.Kind == "identifier" {
				x := op.Text
				switch {
				case m.c.names[x] != "" && !m.bound[x]:
					// A router package function: a constructor.
					return routerMembers[facts.Text(callee.Fields["field"])]
				case m.bound[x] && m.fr.params[x] == nil:
					// A method of a local: the Go linker resolves it only
					// on a typed local; a router's own methods return
					// routers (Group, Route, With).
					return m.typed[x] || m.names[x]
				}
			}
		}
		return m.c.moduleCallee(callee) && !m.c.excludedRoot(callee, m.fr)
	}
	return false
}

// excludedRoot reports a callee whose receiver chain starts at a parameter
// declared with a qualified type that is no router type, never rebound (a
// method of a *gin.Context or a *testing.T): its call returns no router.
func (c *routeCapture) excludedRoot(callee *parse.Node, fr *frame) bool {
	if callee == nil || callee.Kind != "selector_expression" {
		return false
	}
	root := callee.Fields["operand"]
	for root != nil && root.Kind != "identifier" {
		switch root.Kind {
		case "selector_expression", "index_expression":
			root = root.Fields["operand"]
		case "call_expression":
			root = root.Fields["function"]
		case "parenthesized_expression":
			root = firstChild(root)
		default:
			return false
		}
	}
	if root == nil || fr.rebound[root.Text] {
		return false
	}
	t, ok := fr.params[root.Text]
	if !ok || t == nil || c.routerType(t) {
		return false
	}
	if t.Kind == "pointer_type" {
		t = firstChild(t)
	}
	return t != nil && t.Kind == "qualified_type"
}

// usesOf adds the identifiers of a subtree to used. A function literal adds
// its record's uses when it was captured, otherwise the names closureUses
// keeps.
func (c *routeCapture) usesOf(n *parse.Node, used map[string]bool, m *maybeRouter) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "identifier":
		if n.Text != "_" {
			used[n.Text] = true
		}
	case "func_literal":
		if index, ok := c.records[n]; ok {
			for name := range c.uses[index] {
				used[name] = true
			}
		} else {
			for _, name := range c.closureUses(n, m) {
				used[name] = true
			}
		}
		return
	}
	for _, ch := range n.Children {
		c.usesOf(ch, used, m)
	}
}

// closureUses lists the identifiers a function literal's body uses, without
// its own parameters and without names the enclosing record proves hold no
// router (row RE-closure): the link pass reads them to find a router used
// inside a closure (contract row GR-C4).
func (c *routeCapture) closureUses(n *parse.Node, m *maybeRouter) []string {
	used := map[string]bool{}
	var walk func(*parse.Node)
	walk = func(x *parse.Node) {
		if x.Kind == "identifier" && x.Text != "_" {
			used[x.Text] = true
		}
		for _, ch := range x.Children {
			walk(ch)
		}
	}
	if body := n.Fields["body"]; body != nil {
		walk(body)
	}
	for name := range paramTypes(n) {
		delete(used, name)
	}
	var out []string
	for name := range used {
		if m == nil || m.name(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// finish keeps the record's router calls, the plain calls that pass a value
// that may hold a router (row RC-call; the rest are row RC-D), the bindings
// of the names those calls, the returns and nested records use (directly or
// through the values of other kept bindings; row RL-D for the rest) and
// reduces the kept syntax.
func (c *routeCapture) finish(fr *frame, f *facts.GoRouteFunc) {
	f.Jumps = fr.jumps
	m := c.maybeRouter(fr)
	calls := fr.calls[:0]
	for _, call := range fr.calls {
		keep := call.kind == facts.GoCallRouter || call.serve || call.entry
		if !keep && !c.excludedRoot(call.node.Fields["function"], fr) {
			for _, a := range kids(call.node.Fields["arguments"]) {
				if a.Kind == "variadic_argument" {
					a = firstChild(a)
				}
				if a != nil && m.value(a) {
					keep = true
				}
			}
		}
		if keep {
			calls = append(calls, call)
		} else {
			tagRoute("RC-D", call.node.Kind)
		}
	}
	fr.calls = calls
	used := map[string]bool{}
	for _, call := range fr.calls {
		c.usesOf(call.node, used, m)
	}
	for _, r := range fr.returns {
		c.usesOf(r.node, used, m)
		if len(kids(r.node)) == 0 {
			for _, name := range fr.results {
				used[name] = true
			}
		}
	}
	for _, child := range fr.children {
		for name := range c.uses[child] {
			used[name] = true
		}
	}
	kept := make([]bool, len(fr.bindings))
	for i, b := range fr.bindings {
		// x.Handler = h is kept with the names it uses: a serving
		// position may read it (row GR-C6).
		if b.kind == facts.GoBindHandler {
			kept[i] = true
			used[b.names[0]] = true
			for _, v := range b.values {
				c.usesOf(v, used, m)
			}
			continue
		}
		// A binding whose value holds a net/http StripPrefix call or an
		// http.Server literal is kept: a serving position may read it
		// (rows GR-C3, GR-C6). So is one whose value calls a generated
		// server's entry function (row RC-generated: an instance).
		for _, v := range b.values {
			if !kept[i] && (c.holdsServe(v, true) || c.callsEntry(v)) {
				kept[i] = true
				c.usesOf(b.typ, used, m)
				for _, v := range b.values {
					c.usesOf(v, used, m)
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i, b := range fr.bindings {
			if kept[i] {
				continue
			}
			for _, name := range b.names {
				if used[name] {
					kept[i], changed = true, true
				}
			}
			if kept[i] {
				c.usesOf(b.typ, used, m)
				for _, v := range b.values {
					c.usesOf(v, used, m)
				}
			}
		}
	}
	c.uses[fr.index] = used
	escapes := c.escapes(fr, m, kept)
	f.Blocks = fr.blocks
	rd := reducer{route: c}
	c.maybe = m
	defer func() { c.maybe = nil }()
	for i, b := range fr.bindings {
		if !kept[i] {
			tagRoute("RL-D", b.kind)
			continue
		}
		c.parent, c.block = fr.index+1, b.block
		out := facts.GoBinding{Kind: b.kind, Names: b.names, Block: b.block, Span: facts.Span(b.span)}
		if b.typ != nil {
			t := rd.reduce(b.typ, 0)
			out.Type = &t
		}
		for _, v := range b.values {
			out.Values = append(out.Values, rd.reduce(v, 0))
		}
		tagRoute("RL-"+b.kind, out)
		f.Bindings = append(f.Bindings, out)
	}
	for _, call := range fr.calls {
		c.parent, c.block = fr.index+1, call.block
		out := facts.GoRouteCall{Kind: call.kind, Call: rd.reduce(call.node, 0), Block: call.block, Span: facts.Span(call.node), Context: call.context}
		if call.kind == facts.GoCallRouter {
			out.Hash = contract.ContentHash(c.source[call.node.Start:call.node.End])
		}
		row := "RC-" + call.kind
		if call.serve {
			row = "RC-serve"
		}
		if call.entry {
			row = "RC-generated"
		}
		tagRoute(row, out)
		f.Calls = append(f.Calls, out)
	}
	for _, r := range fr.returns {
		c.parent, c.block = fr.index+1, r.block
		out := facts.GoRouteReturn{Block: r.block, Span: facts.Span(r.node)}
		if list := kids(r.node); len(list) == 1 {
			for _, v := range valueNodes(list[0]) {
				out.Values = append(out.Values, rd.reduce(v, 0))
			}
		}
		tagRoute("RR-return", out)
		f.Returns = append(f.Returns, out)
	}
	for _, x := range escapes {
		c.parent, c.block = fr.index+1, x.block
		out := facts.GoEscape{Form: x.form, Value: rd.reduce(x.value, 0), Detail: x.detail, Block: x.block, Span: facts.Span(x.span)}
		if x.typ != nil {
			t := rd.reduce(x.typ, 0)
			out.Type = &t
		}
		tagRoute("RU-escape", out)
		f.Escapes = append(f.Escapes, out)
	}
	for _, call := range f.Calls {
		if call.Context != "" {
			tagRoute("RU-context", struct{ Context string }{call.Context})
		}
	}
}

// funcLiteral reduces a function literal inside router syntax: a captured
// record (row RE-funclit) or the identifiers its body uses (row
// RE-closure), with its forwarding proof when it passes one (rows
// RE-forward and RE-forward-D).
func (c *routeCapture) funcLiteral(n *parse.Node) facts.GoExpr {
	var e facts.GoExpr
	index, ok := c.records[n]
	if !ok && c.relevant(n) {
		index, ok = c.record(n, c.parent, c.block), true
	}
	if ok {
		e = facts.GoExpr{Kind: facts.GoFuncLit, Func: index + 1, Uses: c.closureUses(n, c.maybe)}
		tagRoute("RE-funclit", e)
	} else {
		e = facts.GoExpr{Kind: facts.GoFuncLit, Uses: c.closureUses(n, c.maybe)}
		tagRoute("RE-closure", e)
	}
	e.Request = c.requestLiteral(n)
	if e.Request {
		tagRoute("RE-request", struct{ Request bool }{true})
	}
	rc := &requestCapture{http: c.http, imports: c.imports}
	if f, proven := rc.forward(n); proven {
		e.Forward = f
		tagRoute("RE-forward", struct{ Forward *facts.GoForward }{f})
	} else {
		tagRoute("RE-forward-D", n.Kind)
	}
	return e
}

// argTexts sets the whitespace-free source text of the arguments of a call
// named like a router method (row RE-text): handler_expression and
// middleware read it. A function literal argument has none.
func (c *routeCapture) argTexts(n *parse.Node, e *facts.GoExpr) {
	callee := n.Fields["function"]
	args := n.Fields["arguments"]
	if callee == nil || callee.Kind != "selector_expression" || !routerMethods[facts.Text(callee.Fields["field"])] || args == nil {
		return
	}
	for i, a := range kids(args) {
		if i >= len(e.Args) || a.Kind == "func_literal" {
			continue
		}
		e.Args[i].Text = strings.Join(strings.Fields(string(c.source[a.Start:a.End])), "")
		tagRoute("RE-text", struct{ Text string }{e.Args[i].Text})
	}
}

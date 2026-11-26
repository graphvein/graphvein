package golang

import (
	"path"
	"sort"
	"strconv"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the request-path syntax of task T023 (ADR
// 0076, contract row GR-O7): in a file that imports net/http, the
// declarations whose syntax keeps a served request's path. A wrapper
// (row RW-wrapper) takes an http.Handler and returns it, or returns an
// http.HandlerFunc literal that serves the request it receives through the
// handler, unchanged or through r.WithContext, after only reading it; a
// request helper (row RQ-request) only reads its *http.Request parameter
// or passes it on. The calls that receive the request are recorded
// (Passes); whether they keep the path, and whether a call of a wrapper
// passes a router through, are decided at link time. The rules are Go
// 1.27.1 semantics: net/http's ServeMux dispatches on the request's
// RequestURI, Proto, Method, Host and URL (server.go, ServeMux.ServeHTTP
// and findHandler), and r.WithContext returns a shallow copy that shares
// the URL (request.go).

// requestMethods are the *http.Request methods a proven body may call:
// Go 1.27.1 request.go, none of them writes the request's Method, Host,
// RequestURI, Proto or URL (ParseForm and ParseMultipartForm write only
// Form, PostForm and MultipartForm).
var requestMethods = map[string]bool{
	"Context": true, "Cookie": true, "Cookies": true, "CookiesNamed": true, "BasicAuth": true, "UserAgent": true, "Referer": true,
	"FormValue": true, "PostFormValue": true, "FormFile": true, "ParseForm": true, "ParseMultipartForm": true, "MultipartReader": true,
	"PathValue": true, "ProtoAtLeast": true,
}

// requestFields are the exported fields of http.Request other than URL; a
// read of one copies a value or a reference that does not reach the URL.
var requestFields = map[string]bool{
	"Method": true, "Proto": true, "ProtoMajor": true, "ProtoMinor": true, "Header": true, "Body": true, "GetBody": true,
	"ContentLength": true, "TransferEncoding": true, "Close": true, "Host": true, "Form": true, "PostForm": true,
	"MultipartForm": true, "Trailer": true, "RemoteAddr": true, "RequestURI": true, "TLS": true, "Cancel": true,
	"Response": true, "Pattern": true,
}

// urlMembers are the url.URL fields and the url.URL methods that do not
// change the URL (Go 1.27.1 net/url): r.URL.Path and r.URL.Query() are
// reads.
var urlMembers = map[string]bool{
	"Scheme": true, "Opaque": true, "User": true, "Host": true, "Path": true, "RawPath": true, "OmitHost": true,
	"ForceQuery": true, "RawQuery": true, "Fragment": true, "RawFragment": true,
	"Query": true, "String": true, "EscapedPath": true, "EscapedFragment": true, "Hostname": true, "Port": true,
	"IsAbs": true, "Redacted": true, "RequestURI": true,
}

// requestCapture holds one file's state: the local name of net/http and
// the names its imports bind.
type requestCapture struct {
	http    string
	imports map[string]bool
}

// captureRequestSyntax returns the wrappers and request helpers of a file
// that imports net/http (rows RW-* and RQ-*); nothing for any other file.
func captureRequestSyntax(root *parse.Node, imports []facts.Import) ([]facts.GoWrapper, []facts.GoRequestFunc) {
	c := &requestCapture{imports: map[string]bool{}}
	for _, imp := range imports {
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias {
				alias = path.Base(imp.Module)
			}
			if alias == "_" || alias == "." {
				continue
			}
			c.imports[alias] = true
			if imp.Module == "net/http" {
				c.http = alias
			}
		}
	}
	if c.http == "" || root == nil {
		return nil, nil
	}
	var wrappers []facts.GoWrapper
	var requests []facts.GoRequestFunc
	for _, d := range root.Children {
		if d.Kind != "function_declaration" && d.Kind != "method_declaration" || d.Fields["body"] == nil {
			continue
		}
		if handler, ok := c.wrapperSignature(d); ok {
			if w, ok := c.wrapper(d, handler); ok {
				tagRoute("RW-wrapper", w)
				wrappers = append(wrappers, w)
			} else {
				tagRoute("RW-D", d.Kind)
			}
			continue
		}
		if r, ok := c.request(d); ok {
			tagRoute("RQ-request", r)
			requests = append(requests, r)
			continue
		}
		tagRoute("RQ-D", d.Kind)
	}
	return wrappers, requests
}

// httpType reports a qualified type http.<name> through the file's net/http
// name.
func (c *requestCapture) httpType(t *parse.Node, name string) bool {
	return t != nil && t.Kind == "qualified_type" && facts.Text(t.Fields["package"]) == c.http && facts.Text(t.Fields["name"]) == name
}

// requestType reports *http.Request.
func (c *requestCapture) requestType(t *parse.Node) bool {
	return t != nil && t.Kind == "pointer_type" && c.httpType(firstChild(t), "Request")
}

// paramNames lists a parameter list's names with their declarations, in
// order; an unnamed parameter is "".
func paramNames(list *parse.Node) (names []string, decls []*parse.Node) {
	for _, p := range kids(list) {
		if p.Kind != "parameter_declaration" && p.Kind != "variadic_parameter_declaration" {
			continue
		}
		ids := identifiers(p)
		if len(ids) == 0 {
			ids = []string{""}
		}
		for _, id := range ids {
			names = append(names, id)
			decls = append(decls, p)
		}
	}
	return names, decls
}

// wrapperSignature reports a declaration with exactly one http.Handler
// parameter and exactly one result of type http.Handler; handler is the
// parameter's index.
func (c *requestCapture) wrapperSignature(d *parse.Node) (handler int, ok bool) {
	if t := singleResult(d.Fields["result"]); !c.httpType(t, "Handler") {
		return 0, false
	}
	names, decls := paramNames(d.Fields["parameters"])
	handler = -1
	for i, p := range decls {
		if p.Kind == "parameter_declaration" && c.httpType(p.Fields["type"], "Handler") {
			if handler >= 0 {
				return 0, false
			}
			handler = i
		}
	}
	if handler < 0 || names[handler] == "" || names[handler] == "_" {
		return 0, false
	}
	return handler, true
}

// request reads a function or method with exactly one named *http.Request
// parameter whose every use is a read or a pass (row RQ-request).
func (c *requestCapture) request(d *parse.Node) (facts.GoRequestFunc, bool) {
	receiver := ""
	if d.Kind == "method_declaration" {
		if receiver = methodReceiver(d); receiver == "" {
			return facts.GoRequestFunc{}, false
		}
	}
	names, decls := paramNames(d.Fields["parameters"])
	param := -1
	for i, p := range decls {
		if c.requestType(p.Fields["type"]) {
			if param >= 0 {
				return facts.GoRequestFunc{}, false
			}
			param = i
		}
	}
	if param < 0 || names[param] == "" || names[param] == "_" {
		return facts.GoRequestFunc{}, false
	}
	b := c.body(d, names[param], nil)
	if !b.check([]*parse.Node{d.Fields["body"]}, false) {
		return facts.GoRequestFunc{}, false
	}
	return facts.GoRequestFunc{Name: facts.Text(d.Fields["name"]), Receiver: receiver, Param: param, Passes: b.passes, URL: b.readURL, Method: b.readMethod, Line: line(d)}, true
}

// requestBody checks the uses of a request name in a declaration.
type requestBody struct {
	c *requestCapture
	// request is the request parameter's name.
	request string
	parent  map[*parse.Node]*parse.Node
	// serves are the handler's serving calls (h.ServeHTTP(w, R) statements
	// directly in a returned literal), by call node.
	serves map[*parse.Node]bool
	// uses counts each name's identifier nodes outside call-function
	// position (a local binding of a callee name shows up there).
	uses map[string]int
	// receiver is the method's receiver name when it is only ever the
	// operand of a selector (a call recv.M(r) is a pass to a method of the
	// receiver's type); "" otherwise.
	receiver string
	// context reports a request read through r.WithContext (a serving
	// call's request or r = r.WithContext(x)).
	context bool
	passes  []facts.GoRequestPass
	seen    map[string]bool
	// readURL and readMethod report a read of r.URL or r.RequestURI and of
	// r.Method (a request helper that reads them may decide by
	// the request's path or method).
	readURL, readMethod bool
}

// body indexes a declaration's syntax: parents and identifier uses.
func (c *requestCapture) body(d *parse.Node, request string, serves map[*parse.Node]bool) *requestBody {
	b := &requestBody{c: c, request: request, parent: map[*parse.Node]*parse.Node{}, serves: serves, uses: map[string]int{}, seen: map[string]bool{}}
	var walk func(n *parse.Node)
	walk = func(n *parse.Node) {
		for _, ch := range n.Children {
			b.parent[ch] = n
			walk(ch)
		}
		if n.Kind == "identifier" {
			if p := b.parent[n]; p == nil || p.Kind != "call_expression" || p.Fields["function"] != n {
				b.uses[n.Text]++
			}
		}
	}
	walk(d)
	if d.Kind == "method_declaration" {
		if list := d.Fields["receiver"]; list != nil && len(kids(list)) == 1 {
			if ids := identifiers(kids(list)[0]); len(ids) == 1 && ids[0] != "_" && !c.imports[ids[0]] {
				b.receiver = ids[0]
				for n, p := range b.parent {
					if n.Kind == "identifier" && n.Text == ids[0] && p.Kind != "selector_expression" && p != kids(list)[0] {
						b.receiver = ""
					}
				}
			}
		}
	}
	return b
}

// check reports that every use of the request name in the subtrees is a
// read or a pass (and, in a wrapper, a serving call's request), recording
// the passes. With deferred set (a wrapper literal's region), the body of
// a function literal the literal itself defers runs after the serving call
// and is skipped; one deferred inside a nested literal runs when that
// literal returns and is read.
func (b *requestBody) check(trees []*parse.Node, deferred bool) bool {
	ok := true
	var walk func(n *parse.Node, nested bool)
	walk = func(n *parse.Node, nested bool) {
		if !ok || n == nil {
			return
		}
		if n.Kind == "func_literal" {
			nested = true
		}
		if n.Kind == "defer_statement" && deferred && !nested {
			if call := firstChild(n); call != nil && call.Kind == "call_expression" && call.Fields["function"] != nil && call.Fields["function"].Kind == "func_literal" {
				walk(call.Fields["arguments"], nested)
				return
			}
		}
		if n.Kind == "identifier" && n.Text == b.request && !b.use(n) {
			ok = false
			return
		}
		for _, ch := range n.Children {
			walk(ch, nested)
		}
	}
	for _, t := range trees {
		walk(t, false)
	}
	return ok
}

// rebinding reports an assignment r = r.WithContext(x): the request name
// then holds a shallow copy that shares the URL.
func (b *requestBody) rebinding(a *parse.Node) bool {
	if a == nil || a.Kind != "assignment_statement" || operator(a) != "=" {
		return false
	}
	left, right := kids(a.Fields["left"]), kids(a.Fields["right"])
	if len(left) != 1 || len(right) != 1 || left[0].Kind != "identifier" || left[0].Text != b.request {
		return false
	}
	call := right[0]
	f := call.Fields["function"]
	return call.Kind == "call_expression" && f != nil && f.Kind == "selector_expression" && facts.Text(f.Fields["operand"]) == b.request && f.Fields["operand"].Kind == "identifier" && facts.Text(f.Fields["field"]) == "WithContext" && len(kids(call.Fields["arguments"])) == 1
}

// use classifies one identifier node naming the request: a field read
// r.F (not URL), a read of r.URL.F, an allowed method call, a comparison,
// an argument of a call of a name or pkg.F (a pass), or the request of a
// serving call. Anything else (a binding, an assignment target, &r, a
// method that copies or changes the request) is no proof.
func (b *requestBody) use(n *parse.Node) bool {
	p := b.parent[n]
	if p == nil {
		return false
	}
	switch p.Kind {
	case "selector_expression":
		if p.Fields["operand"] != n {
			return false
		}
		field := facts.Text(p.Fields["field"])
		gp := b.parent[p]
		called := gp != nil && gp.Kind == "call_expression" && gp.Fields["function"] == p
		switch {
		case field == "WithContext" && called:
			if list := b.parent[gp]; list != nil && list.Kind == "expression_list" && b.rebinding(b.parent[list]) && b.parent[list].Fields["right"] == list {
				b.context = true
				return true
			}
			if b.servedRequest(gp) {
				b.context = true
				return true
			}
			return false
		case field == "URL":
			if gp == nil || gp.Kind != "selector_expression" || gp.Fields["operand"] != p || !urlMembers[facts.Text(gp.Fields["field"])] {
				return false
			}
			b.readURL = true
			return !b.written(gp)
		case called:
			return requestMethods[field]
		case requestFields[field] || requestMethods[field]:
			switch field {
			case "RequestURI", "Host", "Proto", "ProtoMajor", "ProtoMinor":
				// The fields ServeMux dispatches on besides the
				// method.
				b.readURL = true
			case "Method":
				b.readMethod = true
			}
			return !b.written(p)
		}
		return false
	case "binary_expression":
		op := operator(p)
		return op == "==" || op == "!="
	case "expression_list":
		a := b.parent[p]
		return b.rebinding(a) && a.Fields["left"] == p
	case "argument_list":
		call := b.parent[p]
		if call == nil || call.Kind != "call_expression" {
			return false
		}
		if b.servedRequest(n) {
			return true
		}
		arg := 0
		for _, a := range kids(p) {
			if a == n {
				break
			}
			arg++
		}
		return b.pass(call.Fields["function"], arg)
	}
	return false
}

// servedRequest reports that x is the request argument of one of the
// handler's serving calls.
func (b *requestBody) servedRequest(x *parse.Node) bool {
	list := b.parent[x]
	if list == nil || list.Kind != "argument_list" {
		return false
	}
	args := kids(list)
	return len(args) == 2 && args[1] == x && b.serves[b.parent[list]]
}

// written reports a selector chain rooted at the request that is an
// assignment target, an increment or decrement, or the operand of &.
func (b *requestBody) written(x *parse.Node) bool {
	for {
		p := b.parent[x]
		if p == nil {
			return false
		}
		switch p.Kind {
		case "selector_expression", "index_expression", "slice_expression", "parenthesized_expression":
			if p.Kind == "index_expression" && p.Fields["operand"] != x && p.Fields["index"] == x {
				return false
			}
			x = p
			continue
		case "expression_list":
			a := b.parent[p]
			return a != nil && (a.Kind == "assignment_statement" || a.Kind == "short_var_declaration" || a.Kind == "receive_statement" || a.Kind == "range_clause") && a.Fields["left"] == p
		case "inc_statement", "dec_statement":
			return true
		case "unary_expression":
			return operator(p) == "&"
		}
		return false
	}
}

// pass records a call that receives the request as argument arg: the
// callee must be a name or pkg.F (pkg an import of the file), neither ever
// bound or used as a value in the declaration, nor a predeclared function;
// or recv.M with recv the method's receiver name (Method: a method of the
// receiver's type).
func (b *requestBody) pass(f *parse.Node, arg int) bool {
	if f == nil {
		return false
	}
	var callee facts.GoExpr
	method := false
	switch f.Kind {
	case "identifier":
		if goBuiltinFuncs[f.Text] || b.uses[f.Text] > 0 {
			return false
		}
		callee = facts.GoExpr{Kind: facts.GoIdentifier, Name: f.Text}
	case "selector_expression":
		op := f.Fields["operand"]
		if op == nil || op.Kind != "identifier" {
			return false
		}
		switch {
		case op.Text == b.receiver:
			method = true
		case !b.c.imports[op.Text] || !b.onlySelected(op.Text):
			return false
		}
		callee = facts.GoExpr{Kind: facts.GoSelector, Name: facts.Text(f.Fields["field"]), Object: &facts.GoExpr{Kind: facts.GoIdentifier, Name: op.Text}}
	default:
		return false
	}
	key := callee.Name + "\x00" + strconv.Itoa(arg)
	if callee.Object != nil {
		key = callee.Object.Name + "." + key
	}
	if !b.seen[key] {
		b.seen[key] = true
		b.passes = append(b.passes, facts.GoRequestPass{Callee: callee, Arg: arg, Method: method})
	}
	return true
}

// onlySelected reports that every identifier node named name in the
// declaration is the operand of a selector (an import name, never bound
// locally).
func (b *requestBody) onlySelected(name string) bool {
	for n, p := range b.parent {
		if n.Kind == "identifier" && n.Text == name && (p.Kind != "selector_expression" || p.Fields["operand"] != n) {
			return false
		}
	}
	return true
}

// wrapper reads a declaration with the wrapper signature (row RW-wrapper):
// every return returns the handler parameter h or http.HandlerFunc(L) of a
// literal L(w, r *http.Request); h is used only there and as h.ServeHTTP(w,
// R) statements directly in such a literal, R being r or r.WithContext(x);
// every literal serves at least once, holds no goto or label, and every use
// of r that may run before or during one of its serving calls is a read or
// a pass (the request's path is the one the handler receives).
func (c *requestCapture) wrapper(d *parse.Node, handler int) (facts.GoWrapper, bool) {
	names, _ := paramNames(d.Fields["parameters"])
	h := names[handler]
	w := facts.GoWrapper{Name: facts.Text(d.Fields["name"]), Param: handler, Line: line(d)}
	if d.Kind == "method_declaration" {
		if w.Receiver = methodReceiver(d); w.Receiver == "" {
			return w, false
		}
	}
	idx := c.body(d, "", nil)
	if !idx.onlySelected(c.http) {
		return w, false
	}
	// Returns outside nested literals, and the literals they return.
	var literals []*parse.Node
	tops := map[*parse.Node]bool{}
	returns, ok := 0, true
	var findReturns func(n *parse.Node)
	findReturns = func(n *parse.Node) {
		if !ok || n.Kind == "func_literal" {
			return
		}
		if n.Kind == "return_statement" {
			returns++
			tops[n] = true
			vals := kids(firstChild(n))
			if firstChild(n) == nil || firstChild(n).Kind != "expression_list" || len(vals) != 1 {
				ok = false
				return
			}
			v := vals[0]
			switch {
			case v.Kind == "identifier" && v.Text == h:
			case v.Kind == "call_expression":
				f := v.Fields["function"]
				args := kids(v.Fields["arguments"])
				if f == nil || f.Kind != "selector_expression" || facts.Text(f.Fields["operand"]) != c.http || facts.Text(f.Fields["field"]) != "HandlerFunc" || len(args) != 1 || args[0].Kind != "func_literal" {
					ok = false
					return
				}
				literals = append(literals, args[0])
			default:
				ok = false
			}
			return
		}
		for _, ch := range n.Children {
			findReturns(ch)
		}
	}
	findReturns(d.Fields["body"])
	if !ok || returns == 0 {
		return w, false
	}
	// The serving calls: h.ServeHTTP(w, R) expression statements directly
	// in a returned literal (no literal, go or defer between).
	serves := map[*parse.Node]bool{}
	statements := map[*parse.Node]*parse.Node{}
	owner := map[*parse.Node]*parse.Node{}
	for _, lit := range literals {
		var find func(n *parse.Node)
		find = func(n *parse.Node) {
			if n.Kind == "func_literal" && n != lit {
				return
			}
			if n.Kind == "expression_statement" {
				if call := firstChild(n); call != nil && call.Kind == "call_expression" {
					f := call.Fields["function"]
					if f != nil && f.Kind == "selector_expression" && facts.Text(f.Fields["operand"]) == h && f.Fields["operand"].Kind == "identifier" && facts.Text(f.Fields["field"]) == "ServeHTTP" && len(kids(call.Fields["arguments"])) == 2 {
						serves[call] = true
						statements[call] = n
						owner[call] = lit
					}
				}
			}
			for _, ch := range n.Children {
				find(ch)
			}
		}
		find(lit)
	}
	// Every use of h is a return value or a serving call's receiver.
	for n, p := range idx.parent {
		if n.Kind != "identifier" || n.Text != h || n == idx.paramName(d, handler) {
			continue
		}
		if p.Kind == "expression_list" && tops[idx.parent[p]] {
			continue
		}
		if p.Kind == "selector_expression" && serves[idx.parent[p]] {
			continue
		}
		return w, false
	}
	for _, lit := range literals {
		lnames, ldecls := paramNames(lit.Fields["parameters"])
		if len(lnames) != 2 || !c.requestType(ldecls[1].Fields["type"]) || lnames[1] == "" || lnames[1] == "_" {
			return w, false
		}
		r := lnames[1]
		b := c.body(d, r, serves)
		calls := 0
		for call := range serves {
			if owner[call] != lit {
				continue
			}
			calls++
			args := kids(call.Fields["arguments"])
			req := args[1]
			switch {
			case req.Kind == "identifier" && req.Text == r:
			case req.Kind == "call_expression" && req.Fields["function"] != nil && req.Fields["function"].Kind == "selector_expression" && facts.Text(req.Fields["function"].Fields["operand"]) == r && facts.Text(req.Fields["function"].Fields["field"]) == "WithContext" && len(kids(req.Fields["arguments"])) == 1:
			default:
				return w, false
			}
			pre, ok := b.before(statements[call], lit.Fields["body"])
			if !ok || !b.check(append(pre, args...), true) {
				return w, false
			}
		}
		if calls == 0 || labels(lit) {
			return w, false
		}
		// Step GR8: a wrapper that answers some paths itself (a branch
		// chosen by r.URL that ends or serves) is no path-preserving
		// wrapper; the methods it answers itself are recorded.
		dec, ok := b.decisions(lit, serves)
		if !ok || dec.url || dec.dispatch {
			return w, false
		}
		w.Methods = mergeStrings(w.Methods, dec.methods)
		for _, g := range dec.guards {
			if !hasPass(w.Guards, g) {
				w.Guards = append(w.Guards, g)
			}
		}
		w.Context = w.Context || b.context
		for _, p := range b.passes {
			if !hasPass(w.Passes, p) {
				w.Passes = append(w.Passes, p)
			}
		}
	}
	return w, true
}

// paramName is the identifier node declaring parameter i of d.
func (b *requestBody) paramName(d *parse.Node, i int) *parse.Node {
	k := 0
	for _, p := range kids(d.Fields["parameters"]) {
		if p.Kind != "parameter_declaration" && p.Kind != "variadic_parameter_declaration" {
			continue
		}
		for _, ch := range p.Children {
			if ch.Kind == "identifier" {
				if k == i {
					return ch
				}
				k++
			}
		}
		if len(identifiers(p)) == 0 {
			k++
		}
	}
	return nil
}

func hasPass(list []facts.GoRequestPass, p facts.GoRequestPass) bool {
	for _, x := range list {
		if x.Arg == p.Arg && x.Callee.Name == p.Callee.Name && (x.Callee.Object == nil) == (p.Callee.Object == nil) && (x.Callee.Object == nil || x.Callee.Object.Name == p.Callee.Object.Name) {
			return true
		}
	}
	return false
}

// labels reports a goto or labeled statement in a literal (control flow
// the region rules below do not follow).
func labels(n *parse.Node) bool {
	if n.Kind == "goto_statement" || n.Kind == "labeled_statement" {
		return true
	}
	for _, ch := range n.Children {
		if labels(ch) {
			return true
		}
	}
	return false
}

// before lists the syntax that may run before or during the statement s in
// one run of the literal body: the statements before it in every enclosing
// list, the headers of the enclosing if, switch and select statements and
// case clauses (a fallthrough chain included), and every enclosing for
// statement whole. An if branch or case body that ends in a terminating
// statement (and holds no break or continue) never reaches s and is left
// out, at any depth outside function literals. ok is false when s sits
// somewhere these rules do not follow.
func (b *requestBody) before(s, body *parse.Node) (out []*parse.Node, ok bool) {
	for cur := s; cur != body; {
		p := b.parent[cur]
		if p == nil {
			return nil, false
		}
		switch p.Kind {
		case "statement_list":
			for _, sib := range kids(p) {
				if sib == cur {
					break
				}
				out = append(out, b.statement(sib)...)
			}
		case "block":
		case "if_statement":
			out = append(out, p.Fields["initializer"], p.Fields["condition"])
		case "for_statement":
			for _, ch := range kids(p) {
				if ch == cur {
					continue
				}
				out = append(out, ch)
			}
			out = append(out, b.statement(cur)...)
		case "expression_case", "default_case", "type_case", "communication_case":
			out = append(out, b.header(p)...)
			// A clause reached by fallthrough runs the bodies before it.
			clauses := kids(b.parent[p])
			for i := range clauses {
				if clauses[i] != p {
					continue
				}
				for j := i - 1; j >= 0 && fallsThrough(clauses[j]); j-- {
					out = append(out, clauses[j])
				}
			}
		case "expression_switch_statement", "type_switch_statement", "select_statement":
			for _, ch := range kids(p) {
				if ch != cur {
					out = append(out, b.header(ch)...)
				}
			}
		default:
			return nil, false
		}
		cur = p
	}
	var clean []*parse.Node
	for _, n := range out {
		if n != nil {
			clean = append(clean, n)
		}
	}
	return clean, true
}

// statement is the part of a statement that may run and then reach a later
// statement: blocks, if statements, loops, switches and selects without
// their terminating branches (recursively); anything else whole (a
// function literal is read whole: its return ends only the literal).
func (b *requestBody) statement(n *parse.Node) []*parse.Node {
	switch n.Kind {
	case "block", "statement_list":
		var out []*parse.Node
		for _, ch := range kids(n) {
			out = append(out, b.statement(ch)...)
		}
		return out
	case "if_statement":
		out := []*parse.Node{n.Fields["initializer"], n.Fields["condition"]}
		for _, branch := range []*parse.Node{n.Fields["consequence"], n.Fields["alternative"]} {
			if branch != nil && !b.terminating(branch) {
				out = append(out, b.statement(branch)...)
			}
		}
		return out
	case "for_statement":
		var out []*parse.Node
		for _, ch := range kids(n) {
			if ch == n.Fields["body"] {
				out = append(out, b.statement(ch)...)
			} else {
				out = append(out, ch)
			}
		}
		return out
	case "expression_switch_statement", "type_switch_statement", "select_statement":
		var out []*parse.Node
		for _, ch := range kids(n) {
			out = append(out, b.header(ch)...)
			if list := clauseBody(ch); list != nil && !b.terminating(list) {
				out = append(out, b.statement(list)...)
			}
		}
		return out
	}
	return []*parse.Node{n}
}

// header is a switch or select child without its clause body (a clause's
// case list or communication), or the child itself when it is no clause.
func (b *requestBody) header(ch *parse.Node) []*parse.Node {
	switch ch.Kind {
	case "expression_case", "default_case", "type_case", "communication_case":
		var out []*parse.Node
		for _, x := range kids(ch) {
			if x.Kind != "statement_list" {
				out = append(out, x)
			}
		}
		return out
	}
	return []*parse.Node{ch}
}

func clauseBody(ch *parse.Node) *parse.Node {
	switch ch.Kind {
	case "expression_case", "default_case", "type_case", "communication_case":
		for _, x := range kids(ch) {
			if x.Kind == "statement_list" {
				return x
			}
		}
	}
	return nil
}

// jumps reports a break or continue statement outside function literals:
// it may leave a branch that ends in a terminating statement.
func jumps(n *parse.Node) bool {
	if n.Kind == "break_statement" || n.Kind == "continue_statement" {
		return true
	}
	for _, ch := range n.Children {
		if ch.Kind != "func_literal" && jumps(ch) {
			return true
		}
	}
	return false
}

// fallsThrough reports a case clause whose last statement is fallthrough.
func fallsThrough(clause *parse.Node) bool {
	list := clauseBody(clause)
	stmts := kids(list)
	return len(stmts) > 0 && stmts[len(stmts)-1].Kind == "fallthrough_statement"
}

// terminating reports a block or statement list whose last statement is a
// terminating statement of the Go specification's subset read here: a
// return, a call of the predeclared panic, a terminating block, or an if
// with an else whose branches both terminate. Other terminating forms
// (for without condition, terminating switch and select) read as not
// terminating, which only keeps more syntax in the region.
func (b *requestBody) terminating(n *parse.Node) bool {
	if n == nil || jumps(n) {
		return false
	}
	switch n.Kind {
	case "block":
		for _, ch := range kids(n) {
			if ch.Kind == "statement_list" {
				return b.terminating(ch)
			}
		}
		return false
	case "statement_list":
		stmts := kids(n)
		if len(stmts) == 0 {
			return false
		}
		return b.terminating(stmts[len(stmts)-1])
	case "return_statement":
		return true
	case "expression_statement":
		call := firstChild(n)
		if call == nil || call.Kind != "call_expression" {
			return false
		}
		f := call.Fields["function"]
		return f != nil && f.Kind == "identifier" && f.Text == "panic" && b.uses["panic"] == 0
	case "if_statement":
		return n.Fields["alternative"] != nil && b.terminating(n.Fields["consequence"]) && b.terminating(n.Fields["alternative"])
	}
	return false
}

// forward reads a function literal func(w, r *http.Request) {...} for the
// forwarding proof (row RE-forward, T023): it serves r through
// X.ServeHTTP(w, r) expression statements directly in its body (no nested
// literal, go or defer between), X one name that every use in the literal
// so serves (the literal neither declares nor assigns it) and that is
// neither a parameter nor an import; every use of r that may run before or
// during a serving call is a read or a pass (the wrapper proof's region
// rules), r is never rebound; and every use of r.URL, anywhere in the
// literal, is r.URL.Path as the tag of an expression switch whose case
// values are all string literals, or as an operand of == whose other
// operand is a string literal. Those literals are the shadows: request
// paths the literal may answer without serving X. r.RequestURI is not read.
func (c *requestCapture) forward(lit *parse.Node) (*facts.GoForward, bool) {
	if c.http == "" || lit == nil || lit.Kind != "func_literal" || lit.Fields["body"] == nil {
		return nil, false
	}
	names, decls := paramNames(lit.Fields["parameters"])
	if len(names) != 2 || !c.requestType(decls[1].Fields["type"]) || names[1] == "" || names[1] == "_" {
		return nil, false
	}
	r := names[1]
	idx := c.body(lit, "", nil)
	if !idx.onlySelected(c.http) || labels(lit) {
		return nil, false
	}
	// The serving calls directly in the literal's body.
	serves := map[*parse.Node]bool{}
	statements := map[*parse.Node]*parse.Node{}
	target := ""
	ok := true
	var find func(n *parse.Node)
	find = func(n *parse.Node) {
		if !ok || n.Kind == "func_literal" && n != lit {
			return
		}
		if n.Kind == "expression_statement" {
			if call := firstChild(n); call != nil && call.Kind == "call_expression" {
				f := call.Fields["function"]
				if f != nil && f.Kind == "selector_expression" && f.Fields["operand"] != nil && f.Fields["operand"].Kind == "identifier" && facts.Text(f.Fields["field"]) == "ServeHTTP" {
					name := facts.Text(f.Fields["operand"])
					args := kids(call.Fields["arguments"])
					if target != "" && name != target || len(args) != 2 || args[1].Kind != "identifier" || args[1].Text != r {
						ok = false
						return
					}
					target = name
					serves[call] = true
					statements[call] = n
				}
			}
		}
		for _, ch := range n.Children {
			find(ch)
		}
	}
	find(lit.Fields["body"])
	if !ok || target == "" || target == r || target == names[0] || c.imports[target] {
		return nil, false
	}
	// Every use of the target is a serving call's receiver.
	for n, p := range idx.parent {
		if n.Kind != "identifier" || n.Text != target {
			continue
		}
		if p.Kind != "selector_expression" || p.Fields["operand"] != n || !serves[idx.parent[p]] {
			return nil, false
		}
	}
	b := c.body(lit, r, serves)
	for call := range serves {
		if b.pathGuarded(call, lit) {
			return nil, false
		}
		pre, ok := b.before(statements[call], lit.Fields["body"])
		if !ok || !b.check(append(pre, kids(call.Fields["arguments"])...), true) {
			return nil, false
		}
	}
	if b.context {
		return nil, false
	}
	shadows, ok := b.urlShadows(lit)
	if !ok {
		return nil, false
	}
	dec, ok := b.decisions(lit, serves)
	if !ok || dec.dispatch {
		// A closure that answers by host or protocol answers every path
		// for those requests.
		return nil, false
	}
	return &facts.GoForward{Name: target, Passes: b.passes, Shadows: shadows, Methods: dec.methods, Guards: dec.guards}, true
}

// requestDecisions are the branch decisions of a wrapper or forwarding
// literal: url reports a branch that ends or serves chosen by a
// condition reading r.URL or r.RequestURI; methods are the request methods
// a terminating branch chosen by r.Method == M answers; guards the passes
// in the conditions of such branches.
type requestDecisions struct {
	url      bool
	dispatch bool
	methods  []string
	guards   []facts.GoRequestPass
}

// httpMethods maps net/http's Method constants to their values (Go 1.27.1
// net/http/method.go).
var httpMethods = map[string]string{
	"MethodGet": "GET", "MethodHead": "HEAD", "MethodPost": "POST", "MethodPut": "PUT", "MethodPatch": "PATCH",
	"MethodDelete": "DELETE", "MethodConnect": "CONNECT", "MethodOptions": "OPTIONS", "MethodTrace": "TRACE",
}

// decisions reads every if and expression switch statement of a literal
// (outside nested literals) with a branch that ends in a terminating
// statement or holds a serving call: a deciding statement. Its conditions
// (initializer, condition, tag, case values) are read for r.URL and
// r.RequestURI (url), for request passes (guards) and for r.Method, which
// is allowed only as `if r.Method == M { ...; return }` (no else, no
// serving call inside) or a switch on r.Method whose clauses either end
// without serving or neither end nor serve (no default that ends): M
// (a string literal or http.MethodX) is then answered by the literal.
// Any other deciding read of r.Method is no proof (ok false).
func (b *requestBody) decisions(lit *parse.Node, serves map[*parse.Node]bool) (requestDecisions, bool) {
	var dec requestDecisions
	ok := true
	serving := func(n *parse.Node) bool {
		found := false
		var walk func(x *parse.Node)
		walk = func(x *parse.Node) {
			if found || x == nil {
				return
			}
			if serves[x] {
				found = true
				return
			}
			for _, ch := range x.Children {
				walk(ch)
			}
		}
		walk(n)
		return found
	}
	decides := func(branch *parse.Node) bool {
		return branch != nil && (b.terminating(branch) || serving(branch))
	}
	readsField := func(n *parse.Node, field string) bool {
		found := false
		var walk func(x *parse.Node)
		walk = func(x *parse.Node) {
			if found || x == nil || x.Kind == "func_literal" {
				return
			}
			if x.Kind == "selector_expression" && facts.Text(x.Fields["field"]) == field {
				if op := x.Fields["operand"]; op != nil && op.Kind == "identifier" && op.Text == b.request {
					found = true
					return
				}
			}
			for _, ch := range x.Children {
				walk(ch)
			}
		}
		walk(n)
		return found
	}
	method := func(x *parse.Node) (string, bool) {
		if x == nil {
			return "", false
		}
		switch x.Kind {
		case "interpreted_string_literal", "raw_string_literal":
			v, err := strconv.Unquote(facts.Text(x))
			return v, err == nil && v != ""
		case "selector_expression":
			if op := x.Fields["operand"]; op != nil && op.Kind == "identifier" && op.Text == b.c.http {
				v, found := httpMethods[facts.Text(x.Fields["field"])]
				return v, found
			}
		}
		return "", false
	}
	// dispatchRead reports a read of a dispatch field other than the path
	// and the method: r.Host, r.URL.Host, r.Proto.
	dispatchRead := func(n *parse.Node) bool {
		if readsField(n, "Host") || readsField(n, "Proto") || readsField(n, "ProtoMajor") || readsField(n, "ProtoMinor") {
			return true
		}
		found := false
		var walk func(x *parse.Node)
		walk = func(x *parse.Node) {
			if found || x == nil || x.Kind == "func_literal" {
				return
			}
			if x.Kind == "selector_expression" && facts.Text(x.Fields["field"]) == "Host" {
				if u := x.Fields["operand"]; u != nil && u.Kind == "selector_expression" && facts.Text(u.Fields["field"]) == "URL" {
					if op := u.Fields["operand"]; op != nil && op.Kind == "identifier" && op.Text == b.request {
						found = true
						return
					}
				}
			}
			for _, ch := range x.Children {
				walk(ch)
			}
		}
		walk(n)
		return found
	}
	isMethod := func(x *parse.Node) bool {
		return x != nil && x.Kind == "selector_expression" && facts.Text(x.Fields["field"]) == "Method" && x.Fields["operand"] != nil && x.Fields["operand"].Kind == "identifier" && x.Fields["operand"].Text == b.request
	}
	guards := func(conds []*parse.Node) {
		saved, seen := b.passes, b.seen
		b.passes, b.seen = nil, map[string]bool{}
		var walk func(x *parse.Node)
		walk = func(x *parse.Node) {
			if x == nil || x.Kind == "func_literal" {
				return
			}
			if x.Kind == "call_expression" {
				for i, a := range kids(x.Fields["arguments"]) {
					if a.Kind == "identifier" && a.Text == b.request {
						b.pass(x.Fields["function"], i)
					}
				}
			}
			for _, ch := range x.Children {
				walk(ch)
			}
		}
		for _, c := range conds {
			walk(c)
		}
		for _, p := range b.passes {
			if !hasPass(dec.guards, p) {
				dec.guards = append(dec.guards, p)
			}
		}
		b.passes, b.seen = saved, seen
	}
	var visit func(n *parse.Node)
	visit = func(n *parse.Node) {
		if !ok || n == nil || n.Kind == "func_literal" && n != lit {
			return
		}
		switch n.Kind {
		case "if_statement":
			cons, alt := n.Fields["consequence"], n.Fields["alternative"]
			conds := []*parse.Node{n.Fields["initializer"], n.Fields["condition"]}
			if decides(cons) || decides(alt) {
				guards(conds)
				for _, c := range conds {
					if readsField(c, "URL") || readsField(c, "RequestURI") {
						dec.url = true
					}
					if dispatchRead(c) {
						dec.dispatch = true
					}
				}
				if readsField(conds[0], "Method") || readsField(conds[1], "Method") {
					cond := conds[1]
					var m string
					found := false
					if conds[0] == nil && alt == nil && cond != nil && cond.Kind == "binary_expression" && operator(cond) == "==" && b.terminating(cons) && !serving(cons) {
						switch {
						case isMethod(cond.Fields["left"]):
							m, found = method(cond.Fields["right"])
						case isMethod(cond.Fields["right"]):
							m, found = method(cond.Fields["left"])
						}
					}
					if !found {
						ok = false
						return
					}
					dec.methods = mergeStrings(dec.methods, []string{m})
				}
			}
		case "expression_switch_statement":
			var clauses []*parse.Node
			deciding := false
			conds := []*parse.Node{n.Fields["initializer"], n.Fields["value"]}
			for _, ch := range kids(n) {
				if ch.Kind == "expression_case" || ch.Kind == "default_case" {
					clauses = append(clauses, ch)
					if ch.Kind == "expression_case" {
						conds = append(conds, ch.Fields["value"])
					}
					if list := clauseBody(ch); decides(list) {
						deciding = true
					}
				}
			}
			if deciding {
				guards(conds)
				for _, c := range conds {
					if readsField(c, "URL") || readsField(c, "RequestURI") {
						dec.url = true
					}
					if dispatchRead(c) {
						dec.dispatch = true
					}
				}
				methodRead := false
				for _, c := range conds {
					methodRead = methodRead || readsField(c, "Method")
				}
				if methodRead {
					if n.Fields["initializer"] != nil || !isMethod(n.Fields["value"]) {
						ok = false
						return
					}
					for _, cl := range clauses {
						list := clauseBody(cl)
						if serving(list) || cl.Kind == "default_case" && list != nil && b.terminating(list) {
							ok = false
							return
						}
						if list == nil || !b.terminating(list) {
							continue
						}
						for _, v := range kids(cl.Fields["value"]) {
							m, found := method(v)
							if !found {
								ok = false
								return
							}
							dec.methods = mergeStrings(dec.methods, []string{m})
						}
					}
				}
			}
		}
		for _, ch := range n.Children {
			visit(ch)
		}
	}
	visit(lit.Fields["body"])
	return dec, ok
}

// mergeStrings is the sorted union of two string lists.
func mergeStrings(a, b []string) []string {
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

// pathGuarded reports a serving call inside a branch chosen by the request
// path: a case clause of a switch on r.URL.Path, or a branch of an if
// statement whose condition reads r.URL. Such a literal forwards only some
// paths.
func (b *requestBody) pathGuarded(call, lit *parse.Node) bool {
	for cur := call; cur != lit; {
		p := b.parent[cur]
		if p == nil {
			return true
		}
		switch {
		case p.Kind == "expression_case":
			if sw := b.parent[p]; sw != nil && b.readsURL(sw.Fields["value"]) {
				return true
			}
		case p.Kind == "if_statement" && cur != p.Fields["condition"] && cur != p.Fields["initializer"]:
			if b.readsURL(p.Fields["condition"]) || b.readsURL(p.Fields["initializer"]) {
				return true
			}
		}
		cur = p
	}
	return false
}

// readsURL reports a selector r.URL in n.
func (b *requestBody) readsURL(n *parse.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == "selector_expression" && facts.Text(n.Fields["field"]) == "URL" {
		if op := n.Fields["operand"]; op != nil && op.Kind == "identifier" && op.Text == b.request {
			return true
		}
	}
	for _, ch := range n.Children {
		if b.readsURL(ch) {
			return true
		}
	}
	return false
}

// urlShadows checks the forwarding proof's rule for r.URL and r.RequestURI
// in the whole literal and returns the string literals r.URL.Path is
// compared with (sorted, distinct).
func (b *requestBody) urlShadows(lit *parse.Node) ([]string, bool) {
	set := map[string]bool{}
	literal := func(n *parse.Node) bool {
		if n == nil || n.Kind != "interpreted_string_literal" && n.Kind != "raw_string_literal" {
			return false
		}
		v, err := strconv.Unquote(facts.Text(n))
		if err != nil {
			return false
		}
		set[v] = true
		return true
	}
	ok := true
	for n, p := range b.parent {
		if !ok {
			break
		}
		if n.Kind != "identifier" || n.Text != b.request || p.Kind != "selector_expression" || p.Fields["operand"] != n {
			continue
		}
		switch facts.Text(p.Fields["field"]) {
		case "RequestURI":
			ok = false
			continue
		case "URL":
		default:
			continue
		}
		path := b.parent[p]
		if path == nil || path.Kind != "selector_expression" || path.Fields["operand"] != p || facts.Text(path.Fields["field"]) != "Path" {
			ok = false
			continue
		}
		use := b.parent[path]
		switch {
		case use != nil && use.Kind == "expression_switch_statement" && use.Fields["value"] == path && use.Fields["initializer"] == nil:
			for _, clause := range kids(use) {
				if clause.Kind != "expression_case" {
					continue
				}
				for _, v := range kids(clause.Fields["value"]) {
					if !literal(v) {
						ok = false
					}
				}
			}
		case use != nil && use.Kind == "binary_expression" && operator(use) == "==" && b.parent[use] != nil && b.parent[use].Kind == "if_statement" && b.parent[use].Fields["condition"] == use:
			other := use.Fields["right"]
			if other == path {
				other = use.Fields["left"]
			}
			if !literal(other) {
				ok = false
			}
		default:
			ok = false
		}
	}
	if !ok {
		return nil, false
	}
	var out []string
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out, true
}

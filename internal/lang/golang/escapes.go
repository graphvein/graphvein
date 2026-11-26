package golang

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the escape syntax of router values (task T023,
// ADR 0076, row RU-escape of docs/lang/go-routes.md): the positions
// outside a record's kept syntax where a value that may hold a router goes
// somewhere no contract row reads (a field, element or package-level
// store, a composite literal element, an argument of a call the payload
// does not keep, a channel send, a method of the router other than its
// own, the return of a function literal), and the context of every kept
// call that lies in syntax the payload does not keep (a statement, whose
// value is discarded, or the value of a local binding). The link pass
// evaluates each recorded value: a router object it holds escapes, and an
// escaped object is no root (contract row GR-C4). Together with the kept
// bindings, calls and returns, every use of a router value in a captured
// record is then visible to the link pass.

// Escape scan contexts: where the value of the expression being scanned
// goes.
const (
	escDiscard = iota // the value is discarded (a statement, a condition operand)
	escLocal          // the value is bound to a local name the record declares
	escAway           // the value escapes (form and detail say where)
)

// escCtx is a scan context.
type escCtx struct {
	kind   int
	form   string
	detail string
	typ    *parse.Node
}

// routerOwnMethods are the methods of net/http's ServeMux, chi's Mux and
// gin's Engine and RouterGroup whose call on a router neither stores nor
// returns the router itself (beyond the router children the contract
// rows read): a call of one is no escape of its receiver. gin's Handler()
// returns the engine as an http.Handler and is not listed.
var routerOwnMethods = map[string]bool{
	"Use": true, "NotFound": true, "MethodNotAllowed": true, "NoRoute": true, "NoMethod": true, "ServeHTTP": true,
	"Routes": true, "Middlewares": true, "Find": true, "NotFoundHandler": true, "MethodNotAllowedHandler": true,
	"Run": true, "RunTLS": true, "RunUnix": true, "RunFd": true, "RunListener": true, "LoadHTMLGlob": true,
	"LoadHTMLFiles": true, "LoadHTMLFS": true, "SetHTMLTemplate": true, "SetFuncMap": true, "Delims": true,
	"SetTrustedProxies": true, "HandleContext": true, "SecureJsonPrefix": true, "BasePath": true,
}

// escapeScan holds one record's scan state.
type escapeScan struct {
	c  *routeCapture
	fr *frame
	m  *maybeRouter
	// declared are the names the record declares (parameters, results and
	// := / var / const / range / type switch bindings outside literals).
	declared map[string]bool
	// keptCalls maps a kept call node to its index in fr.calls; keptValues
	// are the value nodes of kept bindings.
	keptCalls  map[*parse.Node]int
	keptValues map[*parse.Node]bool
	out        []pendingEscape
}

// pendingEscape is one escape position, reduced only at the end.
type pendingEscape struct {
	form, detail string
	value, typ   *parse.Node
	block        int
	span         *parse.Node
}

// escapes scans a record's body (rows RU-escape and RU-context): it sets
// the context of the kept calls it meets and returns the escape positions.
func (c *routeCapture) escapes(fr *frame, m *maybeRouter, kept []bool) []pendingEscape {
	if fr.body == nil {
		return nil
	}
	s := &escapeScan{c: c, fr: fr, m: m, declared: map[string]bool{}, keptCalls: map[*parse.Node]int{}, keptValues: map[*parse.Node]bool{}}
	for name := range fr.params {
		s.declared[name] = true
	}
	for _, name := range fr.results {
		s.declared[name] = true
	}
	for i, b := range fr.bindings {
		if b.kind != facts.GoBindAssign && b.kind != facts.GoBindRangeAssign && b.kind != facts.GoBindHandler {
			for _, name := range b.names {
				s.declared[name] = true
			}
		}
		if kept[i] {
			for _, v := range b.values {
				s.keptValues[v] = true
			}
		}
	}
	for i, call := range fr.calls {
		s.keptCalls[call.node] = i
	}
	s.statements(fr.body, 0, nil)
	return s.out
}

// block is the record block of a node (the walk's placement), or the
// enclosing one.
func (s *escapeScan) block(n *parse.Node, inherited int) int {
	if b, ok := s.fr.nodeBlock[n]; ok {
		return b
	}
	return inherited
}

// statements scans statement syntax. hidden are the names a function
// literal the payload does not capture declares (nil in the record's own
// body): they hold nothing the link pass can evaluate.
func (s *escapeScan) statements(n *parse.Node, block int, hidden map[string]bool) {
	if n == nil {
		return
	}
	block = s.block(n, block)
	switch n.Kind {
	case "func_literal":
		s.literal(n, block, hidden)
		return
	case "return_statement":
		if hidden == nil {
			// The record's own returns are kept (row RR-return).
			return
		}
		for _, v := range valueNodes(firstChild(n)) {
			s.value(v, block, hidden, escCtx{kind: escAway, form: facts.GoEscapeReturn})
		}
		return
	case "short_var_declaration", "assignment_statement", "var_spec":
		s.assignment(n, block, hidden)
		return
	case "send_statement":
		s.value(n.Fields["channel"], block, hidden, escCtx{kind: escDiscard})
		s.value(n.Fields["value"], block, hidden, escCtx{kind: escAway, form: facts.GoEscapeSend})
		return
	case "expression_statement", "go_statement", "defer_statement":
		for _, ch := range kids(n) {
			s.value(ch, block, hidden, escCtx{kind: escDiscard})
		}
		return
	case "if_statement", "for_statement", "expression_switch_statement", "type_switch_statement", "select_statement",
		"block", "statement_list", "expression_case", "default_case", "type_case", "communication_case", "labeled_statement",
		"range_clause", "for_clause", "receive_statement", "var_declaration", "const_declaration", "const_spec", "source_file":
	default:
		if strings.HasSuffix(n.Kind, "_statement") || strings.HasSuffix(n.Kind, "_declaration") {
			break
		}
		// An expression in a statement's header (a condition, a range
		// value, a case value): its value is read, not stored.
		s.value(n, block, hidden, escCtx{kind: escDiscard})
		return
	}
	for _, ch := range kids(n) {
		s.statements(ch, block, hidden)
	}
}

// literal scans a function literal: a captured one is its own record; the
// body of any other is scanned here with its own names hidden.
func (s *escapeScan) literal(n *parse.Node, block int, hidden map[string]bool) {
	if _, ok := s.c.records[n]; ok {
		return
	}
	inner := map[string]bool{}
	for k := range hidden {
		inner[k] = true
	}
	for name := range paramTypes(n) {
		inner[name] = true
	}
	if list := n.Fields["result"]; list != nil && list.Kind == "parameter_list" {
		for _, p := range kids(list) {
			for _, name := range identifiers(p) {
				inner[name] = true
			}
		}
	}
	if body := n.Fields["body"]; body != nil {
		declaredIn(body, inner)
		s.statements(body, block, inner)
	}
}

// declaredIn adds the names a body declares (:=, var, const, range :=, type
// switch), function literals included.
func declaredIn(n *parse.Node, out map[string]bool) {
	switch n.Kind {
	case "short_var_declaration", "range_clause", "receive_statement":
		define := false
		for _, tok := range n.Tokens {
			if tok == ":=" {
				define = true
			}
		}
		if left := n.Fields["left"]; left != nil && define {
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
		declaredIn(ch, out)
	}
}

// assignment scans a binding statement: a value bound to a name the scope
// declares is local; any other target (a field, an element, a
// dereference, a package-level or enclosing name) is a store.
func (s *escapeScan) assignment(n *parse.Node, block int, hidden map[string]bool) {
	var left, right []*parse.Node
	if n.Kind == "var_spec" {
		right = valueNodes(n.Fields["value"])
		for _, name := range identifiers(n) {
			left = append(left, &parse.Node{Kind: "identifier", Text: name})
		}
	} else {
		left, right = kids(n.Fields["left"]), valueNodes(n.Fields["right"])
		if n.Kind == "assignment_statement" && !isAssign(n) {
			// A compound assignment (+=) computes a value of no router.
			for _, v := range right {
				s.value(v, block, hidden, escCtx{kind: escDiscard})
			}
			return
		}
	}
	define := n.Kind == "var_spec"
	for _, tok := range n.Tokens {
		if tok == ":=" {
			define = true
		}
	}
	local := func(t *parse.Node) bool {
		if t.Kind != "identifier" {
			return false
		}
		if t.Text == "_" {
			return true
		}
		if hidden != nil {
			return define || hidden[t.Text]
		}
		return define || s.declared[t.Text]
	}
	ctxOf := func(t *parse.Node) escCtx {
		if t != nil && local(t) {
			return escCtx{kind: escLocal}
		}
		detail := ""
		if t != nil {
			detail = strings.Join(strings.Fields(string(s.c.source[t.Start:t.End])), "")
			if t.Kind == "identifier" && t.Start == 0 && t.End == 0 {
				detail = t.Text
			}
		}
		return escCtx{kind: escAway, form: facts.GoEscapeStore, detail: detail}
	}
	for _, t := range left {
		if t.Kind != "identifier" {
			// A target expression is read (x.f, m[k]): its operands are no
			// stored value.
			s.value(t, block, hidden, escCtx{kind: escDiscard})
		}
	}
	switch {
	case len(left) == len(right):
		for i, v := range right {
			if s.keptValues[v] {
				continue
			}
			s.value(v, block, hidden, ctxOf(left[i]))
		}
	case len(right) == 1:
		if s.keptValues[right[0]] {
			return
		}
		ctx := escCtx{kind: escLocal}
		for _, t := range left {
			if c := ctxOf(t); c.kind == escAway {
				ctx = c
			}
		}
		s.value(right[0], block, hidden, ctx)
	default:
		for _, v := range right {
			s.value(v, block, hidden, escCtx{kind: escDiscard})
		}
	}
}

// may reports a value that may hold a router (or a function literal that
// may serve one): the escape positions worth recording.
func (s *escapeScan) may(n *parse.Node, hidden map[string]bool) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case "identifier":
		if n.Text == "_" || hidden[n.Text] || s.c.imports[n.Text] && !s.m.bound[n.Text] {
			return false
		}
		return s.m.name(n.Text)
	case "parenthesized_expression", "type_assertion_expression":
		return s.may(firstChild(n), hidden)
	case "unary_expression":
		return operator(n) == "&" && s.may(n.Fields["operand"], hidden)
	case "selector_expression":
		op := n.Fields["operand"]
		field := facts.Text(n.Fields["field"])
		if op != nil && op.Kind == "identifier" && s.c.names[op.Text] != "" && !s.m.bound[op.Text] {
			return routerMembers[field]
		}
		return (field == "ServeHTTP" || routerMethods[field] || routerOwnMethods[field] || field == "Handler") && s.may(op, hidden)
	case "type_conversion_expression":
		return s.may(n.Fields["operand"], hidden)
	case "call_expression":
		if hidden == nil && s.m.value(n) {
			return true
		}
		if callee := n.Fields["function"]; callee != nil && callee.Kind == "selector_expression" && s.may(callee.Fields["operand"], hidden) {
			return true
		}
		for _, a := range kids(n.Fields["arguments"]) {
			if a.Kind == "variadic_argument" {
				a = firstChild(a)
			}
			if s.may(a, hidden) {
				return true
			}
		}
		return hidden != nil && s.c.moduleCallee(n.Fields["function"])
	case "composite_literal":
		return s.mayElements(n.Fields["body"], hidden)
	case "literal_value":
		return s.mayElements(n, hidden)
	case "func_literal":
		return s.c.requestLiteral(n)
	}
	return false
}

func (s *escapeScan) mayElements(body *parse.Node, hidden map[string]bool) bool {
	for _, el := range kids(body) {
		if el.Kind == "keyed_element" {
			el = el.Fields["value"]
		}
		if el != nil && el.Kind == "literal_element" {
			el = firstChild(el)
		}
		if s.may(el, hidden) {
			return true
		}
	}
	return false
}

// record adds one escape position.
func (s *escapeScan) record(n *parse.Node, block int, ctx escCtx) {
	s.out = append(s.out, pendingEscape{form: ctx.form, detail: ctx.detail, value: n, typ: ctx.typ, block: block, span: n})
}

// value scans an expression whose value goes where ctx says.
func (s *escapeScan) value(n *parse.Node, block int, hidden map[string]bool, ctx escCtx) {
	if n == nil {
		return
	}
	block = s.block(n, block)
	if k, ok := s.keptCalls[n]; ok {
		// A kept call: the link pass reads its tree; its value's
		// destination is recorded here.
		switch ctx.kind {
		case escDiscard:
			s.fr.calls[k].context = facts.GoCallStatement
		case escLocal:
			s.fr.calls[k].context = facts.GoCallLocal
		default:
			s.record(n, block, ctx)
		}
		return
	}
	switch n.Kind {
	case "func_literal":
		if ctx.kind == escAway && s.c.requestLiteral(n) {
			s.record(n, block, ctx)
		}
		s.literal(n, block, hidden)
		return
	case "identifier":
		if ctx.kind == escAway && s.may(n, hidden) {
			s.record(n, block, ctx)
		}
		return
	case "parenthesized_expression", "type_assertion_expression", "type_conversion_expression":
		if n.Kind == "type_conversion_expression" {
			s.value(n.Fields["operand"], block, hidden, ctx)
			return
		}
		s.value(firstChild(n), block, hidden, ctx)
		return
	case "unary_expression":
		if operator(n) == "&" {
			s.value(n.Fields["operand"], block, hidden, ctx)
			return
		}
	case "selector_expression":
		op := n.Fields["operand"]
		field := facts.Text(n.Fields["field"])
		if field == "ServeHTTP" || op != nil && op.Kind == "identifier" && s.c.names[op.Text] != "" && !s.m.bound[op.Text] {
			// x.ServeHTTP as a value is x; http.DefaultServeMux is a
			// router member.
			if ctx.kind == escAway && s.may(n, hidden) {
				s.record(n, block, ctx)
			}
			if field == "ServeHTTP" {
				return
			}
		} else if (routerMethods[field] || routerOwnMethods[field] || field == "Handler") && s.may(op, hidden) {
			// A method value of a router (hf := m.HandleFunc): the
			// router escapes into it.
			s.record(n, block, escCtx{kind: escAway, form: facts.GoEscapeMethod, detail: field})
			return
		}
		s.value(op, block, hidden, escCtx{kind: escDiscard})
		return
	case "composite_literal", "literal_value":
		body := n
		var typ *parse.Node
		if n.Kind == "composite_literal" {
			body, typ = n.Fields["body"], n.Fields["type"]
		}
		for _, el := range kids(body) {
			key := ""
			v := el
			if el.Kind == "keyed_element" {
				key = facts.Text(el.Fields["key"])
				v = el.Fields["value"]
			}
			if v != nil && v.Kind == "literal_element" {
				v = firstChild(v)
			}
			s.value(v, block, hidden, escCtx{kind: escAway, form: facts.GoEscapeComposite, detail: key, typ: typ})
		}
		return
	case "call_expression":
		s.call(n, block, hidden, ctx)
		return
	}
	for _, ch := range kids(n) {
		s.value(ch, block, hidden, escCtx{kind: escDiscard})
	}
}

// call scans a call the payload does not keep: its callee and arguments.
// A method of a router other than its own methods (routerOwnMethods and
// the registration and child methods) receives the router; an argument
// escapes into the call unless the call's own value is bound locally (the
// value then carries what it wraps, read where the local is used).
func (s *escapeScan) call(n *parse.Node, block int, hidden map[string]bool, ctx escCtx) {
	callee := n.Fields["function"]
	text := ""
	if callee != nil {
		text = strings.Join(strings.Fields(string(s.c.source[callee.Start:callee.End])), "")
	}
	argCtx := escCtx{kind: escAway, form: facts.GoEscapeArgument, detail: text}
	builtinStore := callee != nil && callee.Kind == "identifier" && (callee.Text == "append" || callee.Text == "copy") && !s.m.bound[callee.Text]
	if ctx.kind == escLocal && !builtinStore {
		// The call's value may wrap its arguments; append and copy store
		// them in a slice.
		argCtx = ctx
	}
	switch {
	case callee == nil:
	case callee.Kind == "func_literal":
		s.literal(callee, block, hidden)
	case callee.Kind == "selector_expression":
		op := callee.Fields["operand"]
		field := facts.Text(callee.Fields["field"])
		own := routerMethods[field] || routerOwnMethods[field]
		if !own && ctx.kind != escDiscard && op != nil && s.may(op, hidden) && !(op.Kind == "identifier" && s.c.imports[op.Text] && !s.m.bound[op.Text]) {
			s.record(op, block, escCtx{kind: escAway, form: facts.GoEscapeMethod, detail: field})
		} else if !own && op != nil && op.Kind != "identifier" {
			s.value(op, block, hidden, escCtx{kind: escDiscard})
		}
		if routerMethods[field] {
			// A router-named call the payload does not keep (row
			// RC-excluded): its receiver is no proven router, so any
			// router passed to it goes where no row reads.
			argCtx = escCtx{kind: escAway, form: facts.GoEscapeArgument, detail: text}
		}
	case callee.Kind == "call_expression" || callee.Kind == "parenthesized_expression":
		s.value(callee, block, hidden, escCtx{kind: escDiscard})
	}
	for _, a := range kids(n.Fields["arguments"]) {
		if a.Kind == "variadic_argument" {
			a = firstChild(a)
		}
		if callee != nil && callee.Kind == "identifier" && (callee.Text == "len" || callee.Text == "cap" || callee.Text == "new" || callee.Text == "make" || callee.Text == "panic" || callee.Text == "print" || callee.Text == "println") && !s.m.bound[callee.Text] {
			s.value(a, block, hidden, escCtx{kind: escDiscard})
			continue
		}
		s.value(a, block, hidden, argCtx)
	}
}

// requestLiteral reports a function literal with a *http.Request parameter
// (a handler that may serve a request through the routers it uses).
func (c *routeCapture) requestLiteral(n *parse.Node) bool {
	if c.http == "" {
		return false
	}
	for _, t := range paramTypes(n) {
		if t != nil && t.Kind == "pointer_type" {
			if x := firstChild(t); x != nil && x.Kind == "qualified_type" && facts.Text(x.Fields["package"]) == c.http && facts.Text(x.Fields["name"]) == "Request" {
				return true
			}
		}
	}
	return false
}

// nameUses counts, for every name a call may target, its calls and its
// other uses in a file (row RN-names, T023 GR10): an identifier F, a
// selector .M of a value (a method or method value) and imp:<path>.F of
// an import. Declaration names, assignment and definition targets, the
// keys of keyed composite elements, labels and type names are not uses.
// The router linker reads a factory only when the module's calls of its
// name are all calls it reads and nothing else uses the name (a value in
// a map, a field, an argument, a return, ...: called where no rule reads
// the call).
func nameUses(root *parse.Node, imports []facts.Import) map[string]facts.GoNameUse {
	paths := map[string]string{}
	for _, imp := range imports {
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias {
				alias = path.Base(imp.Module)
			}
			if alias != "_" && alias != "." {
				paths[alias] = imp.Module
			}
		}
	}
	out := map[string]facts.GoNameUse{}
	key := func(n *parse.Node) string {
		switch n.Kind {
		case "identifier":
			return n.Text
		case "selector_expression":
			op := n.Fields["operand"]
			field := facts.Text(n.Fields["field"])
			if op != nil && op.Kind == "identifier" {
				if p, ok := paths[op.Text]; ok {
					return "imp:" + p + "." + field
				}
			}
			return "." + field
		}
		return ""
	}
	skip := map[*parse.Node]bool{}
	var walk func(n *parse.Node)
	walk = func(n *parse.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "function_declaration", "method_declaration":
			skip[n.Fields["name"]] = true
		case "parameter_declaration", "variadic_parameter_declaration", "var_spec", "const_spec", "type_spec", "labeled_statement":
			for _, ch := range n.Children {
				if ch.Kind == "identifier" {
					skip[ch] = true
				}
			}
		case "short_var_declaration", "assignment_statement", "range_clause", "receive_statement":
			if left := n.Fields["left"]; left != nil {
				for _, x := range kids(left) {
					if x.Kind == "identifier" {
						skip[x] = true
					}
				}
			}
		case "keyed_element":
			if k := firstChild(n); k != nil {
				skip[k] = true
				if x := firstChild(k); x != nil {
					skip[x] = true
				}
			}
		case "call_expression":
			if f := n.Fields["function"]; f != nil {
				if k := key(f); k != "" {
					u := out[k]
					u.Calls++
					out[k] = u
					skip[f] = true
				}
			}
		case "selector_expression", "identifier":
			if !skip[n] {
				if k := key(n); k != "" && k != "_" {
					u := out[k]
					u.Values++
					out[k] = u
				}
			}
			if n.Kind == "selector_expression" {
				// The operand is a use of its own.
				walk(n.Fields["operand"])
				return
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
	tagRoute("RN-names", struct{ Names bool }{len(out) > 0})
	if len(out) == 0 {
		return nil
	}
	return out
}

// netServe are the net/http functions serving a handler, by argument
// index (contract row GR-C6), and the httptest constructors.
var netServe = map[string]int{"ListenAndServe": 1, "ListenAndServeTLS": 3, "Serve": 1, "ServeTLS": 1}
var testServe = map[string]bool{"NewServer": true, "NewTLSServer": true, "NewUnstartedServer": true}

// resultReadMethods are the methods whose call on a router keeps it where
// it is (serving it at its own paths, or registering on it).
func resultReadMethod(name string) bool {
	return routerMethods[name] || routerOwnMethods[name]
}

// resultUses records, for every call of a name outside the captured router
// syntax, the uses of its result that no rule reads by construction (row
// RN-results, T023 GR11): an argument of a module call, a return, or
// anything else. The router linker reads a factory's calls through these:
// a call in a file the route gate does not pass (gotify's main serving
// router.Create's engine through runner.Run) is read when each recorded
// use is one a row reads.
func resultUses(g *facts.GoFacts, root *parse.Node, imports []facts.Import) {
	if root == nil || g.Names == nil {
		return
	}
	paths := map[string]string{}
	stdlib := map[string]bool{}
	httpName, testName := "", ""
	for _, imp := range imports {
		for alias := range imp.Bindings {
			if !imp.ExplicitAlias {
				alias = path.Base(imp.Module)
			}
			if alias == "_" || alias == "." {
				continue
			}
			paths[alias] = imp.Module
			stdlib[alias] = goStdlibRoots[strings.SplitN(imp.Module, "/", 2)[0]]
			switch imp.Module {
			case "net/http":
				httpName = alias
			case "net/http/httptest":
				testName = alias
			}
		}
	}
	var captured []facts.GoRouteFunc
	if g.Routes != nil {
		captured = g.Routes.Funcs
	}
	inCaptured := func(n *parse.Node) bool {
		row, col := int(n.Row)+1, int(n.Column)
		for _, f := range captured {
			s := f.Span
			if (row > s.StartLine || row == s.StartLine && col >= s.StartColumn) && (row < s.EndLine || row == s.EndLine && col <= s.EndColumn) {
				return true
			}
		}
		return false
	}
	key := func(f *parse.Node) (string, bool) {
		switch f.Kind {
		case "identifier":
			return f.Text, !goBuiltinFuncs[f.Text]
		case "selector_expression":
			op := f.Fields["operand"]
			field := facts.Text(f.Fields["field"])
			if op != nil && op.Kind == "identifier" {
				if p, ok := paths[op.Text]; ok {
					return "imp:" + p + "." + field, !stdlib[op.Text]
				}
			}
			return "." + field, true
		}
		return "", false
	}
	parent := map[*parse.Node]*parse.Node{}
	var index func(n *parse.Node)
	index = func(n *parse.Node) {
		for _, ch := range n.Children {
			parent[ch] = n
			index(ch)
		}
	}
	index(root)
	enclosing := func(n *parse.Node) *parse.Node {
		for p := parent[n]; p != nil; p = parent[p] {
			switch p.Kind {
			case "function_declaration", "method_declaration", "func_literal":
				return p
			}
		}
		return nil
	}
	funcKey := func(fn *parse.Node) string {
		switch {
		case fn == nil || fn.Kind == "func_literal":
			return ""
		case fn.Kind == "method_declaration":
			return "." + facts.Text(fn.Fields["name"])
		}
		return facts.Text(fn.Fields["name"])
	}
	// use classifies one value position x (a call or a name holding its
	// result): ok false when a row reads it by construction.
	use := func(x *parse.Node, result int) (facts.GoResultUse, bool) {
		u := facts.GoResultUse{Result: result, Line: int(x.Row) + 1}
		p := parent[x]
		for p != nil && p.Kind == "parenthesized_expression" {
			x, p = p, parent[p]
		}
		switch {
		case p == nil:
		case p.Kind == "binary_expression":
			return u, false
		case p.Kind == "selector_expression" && p.Fields["operand"] == x:
			gp := parent[p]
			field := facts.Text(p.Fields["field"])
			if gp != nil && gp.Kind == "call_expression" && gp.Fields["function"] == p && resultReadMethod(field) {
				return u, false
			}
			u.Use, u.Callee = facts.GoResultOther, "."+field
			return u, true
		case p.Kind == "argument_list":
			call := parent[p]
			arg := 0
			for _, a := range kids(p) {
				if a == x {
					break
				}
				arg++
			}
			f := call.Fields["function"]
			if f != nil && f.Kind == "selector_expression" {
				op := f.Fields["operand"]
				field := facts.Text(f.Fields["field"])
				if op != nil && op.Kind == "identifier" && (op.Text == httpName && httpName != "" && netServe[field] == arg && field != "" && hasServe(field) || op.Text == testName && testName != "" && testServe[field] && arg == 0) {
					return u, false
				}
			}
			k, module := key(f)
			if k == "" || !module {
				u.Use, u.Callee = facts.GoResultOther, k
				return u, true
			}
			u.Use, u.Callee, u.Arg = facts.GoResultArgument, k, arg
			return u, true
		case p.Kind == "expression_list" && parent[p] != nil && parent[p].Kind == "return_statement":
			u.Use, u.Callee = facts.GoResultReturn, funcKey(enclosing(x))
			if u.Callee == "" {
				u.Use = facts.GoResultOther
			}
			return u, true
		case p.Kind == "literal_element" || p.Kind == "keyed_element":
			el := p
			if p.Kind == "literal_element" {
				el = parent[p]
			}
			if el != nil && el.Kind == "keyed_element" && kids(el)[len(kids(el))-1] == p {
				k := firstChild(el)
				lit := parent[parent[el]]
				if k != nil && facts.Text(k) == "Handler" && lit != nil && lit.Kind == "composite_literal" {
					if t := lit.Fields["type"]; t != nil && t.Kind == "qualified_type" && facts.Text(t.Fields["package"]) == httpName && facts.Text(t.Fields["name"]) == "Server" {
						return u, false
					}
				}
			}
		}
		u.Use = facts.GoResultOther
		return u, true
	}
	record := func(k string, u facts.GoResultUse) {
		nu := g.Names[k]
		nu.Results = append(nu.Results, u)
		g.Names[k] = nu
	}
	var walk func(n *parse.Node)
	walk = func(n *parse.Node) {
		if n.Kind == "call_expression" && !inCaptured(n) {
			if f := n.Fields["function"]; f != nil {
				if k, module := key(f); k != "" && module {
					p := parent[n]
					switch {
					case p == nil || p.Kind == "expression_statement" || p.Kind == "go_statement" || p.Kind == "defer_statement":
						// Discarded: no use (a router built and dropped
						// is dead code, ADR 0016).
					case p.Kind == "expression_list" && parent[p] != nil && (parent[p].Kind == "short_var_declaration" || parent[p].Kind == "assignment_statement" || parent[p].Kind == "var_spec") && len(kids(p)) == 1 && parent[p].Fields["left"] != p:
						bindResults(parent[p], n, k, enclosing(n), use, record)
					case p.Kind == "var_spec":
						bindResults(p, n, k, enclosing(n), use, record)
					default:
						if u, ok := use(n, -1); ok {
							record(k, u)
						}
					}
				}
			}
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(root)
	for k, nu := range g.Names {
		if len(nu.Results) > 0 {
			tagRoute("RN-results", struct{ Results bool }{true})
			g.Names[k] = nu
		}
	}
}

// hasServe reports a net/http serving function.
func hasServe(name string) bool { _, ok := netServe[name]; return ok }

// bindResults follows the names a binding statement gives a call's results
// through the enclosing body: each use of a name is classified as a value
// position (use); a target that is no identifier is another use.
func bindResults(stmt, call *parse.Node, k string, fn *parse.Node, use func(*parse.Node, int) (facts.GoResultUse, bool), record func(string, facts.GoResultUse)) {
	var targets []*parse.Node
	if stmt.Kind == "var_spec" {
		for _, ch := range stmt.Children {
			if ch.Kind == "identifier" {
				targets = append(targets, ch)
			}
		}
	} else if left := stmt.Fields["left"]; left != nil {
		targets = kids(left)
	}
	for i, t := range targets {
		if t.Kind != "identifier" {
			record(k, facts.GoResultUse{Use: facts.GoResultOther, Result: i, Callee: "store", Line: int(call.Row) + 1})
			continue
		}
		if t.Text == "_" || fn == nil {
			continue
		}
		var visit func(n *parse.Node)
		visit = func(n *parse.Node) {
			if n.Kind == "identifier" && n.Text == t.Text && n != t {
				if u, ok := use(n, i); ok {
					record(k, u)
				}
			}
			for _, ch := range n.Children {
				visit(ch)
			}
		}
		if body := fn.Fields["body"]; body != nil {
			visit(body)
		}
	}
}

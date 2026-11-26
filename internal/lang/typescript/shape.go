package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Route shape syntax (T029 SH-E, docs/contract/claims/route-shape-express.md).
// extractShapes records, per function, how it uses its own parameters
// (facts.ShapeFunction) and the file's top-level interfaces and type
// aliases (facts.ShapeType). Nothing is resolved here: the shape linker
// resolves names and follows calls at link time.

// shapeFunctionKinds are the function nodes whose parameter uses are read.
var shapeFunctionKinds = map[string]bool{
	"function_declaration": true, "generator_function_declaration": true,
	"function_expression": true, "generator_function": true,
	"arrow_function": true, "method_definition": true,
}

// shapeOrigin is what a local name stands for: a parameter (path empty)
// or a const alias of a property path of one.
type shapeOrigin struct {
	param int
	path  []string
}

func extractShapes(root *parse.Node, out *Facts) {
	if root == nil {
		return
	}
	r := &facts.ShapeSyntax{Types: shapeTypes(root)}
	useServer := hasDirective(prologue(root), "use server")
	var walk func(n, parent *parse.Node)
	walk = func(n, parent *parse.Node) {
		if n == nil {
			return
		}
		if shapeFunctionKinds[n.Kind] {
			if f, ok := shapeFunction(n, parent, useServer); ok {
				r.Functions = append(r.Functions, f)
			}
		}
		for _, c := range n.Children {
			walk(c, n)
		}
	}
	walk(root, nil)
	if len(r.Types) == 0 {
		r.Types = nil
	}
	if r.Types != nil || r.Functions != nil {
		out.Shape = r
	}
}

// shapeTypes reads the top-level (or exported) non-generic interfaces and
// type aliases. A name declared twice is recorded as "other".
func shapeTypes(root *parse.Node) map[string]facts.ShapeType {
	out := map[string]facts.ShapeType{}
	seen := map[string]int{}
	add := func(n *parse.Node) {
		name := text(n.Fields["name"])
		if name == "" {
			return
		}
		seen[name]++
		if n.Fields["type_parameters"] != nil {
			out[name] = facts.ShapeType{Kind: "other", Name: "generic " + name}
			return
		}
		switch n.Kind {
		case "interface_declaration":
			t := shapeObject(n.Fields["body"])
			for _, c := range n.Children {
				if c.Kind == "extends_type_clause" || c.Kind == "extends_clause" {
					t.Open = true
				}
			}
			out[name] = t
		case "type_alias_declaration":
			out[name] = routeShapeType(n.Fields["value"])
		}
	}
	for _, c := range root.Children {
		decl := c
		if c.Kind == "export_statement" && c.Fields["declaration"] != nil {
			decl = c.Fields["declaration"]
		}
		if decl.Kind == "interface_declaration" || decl.Kind == "type_alias_declaration" {
			add(decl)
		}
	}
	for name, n := range seen {
		if n > 1 {
			out[name] = facts.ShapeType{Kind: "other", Name: "declared twice"}
		}
	}
	return out
}

// shapeType reduces a type node to the forms the shape rows read.
func routeShapeType(n *parse.Node) facts.ShapeType {
	if n == nil {
		return facts.ShapeType{Kind: "other"}
	}
	switch n.Kind {
	case "type_annotation", "parenthesized_type":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				return routeShapeType(c)
			}
		}
	case "predefined_type":
		switch n.Text {
		case "string", "number", "boolean", "any", "unknown", "never", "undefined", "null", "object":
			return facts.ShapeType{Kind: n.Text}
		}
	case "literal_type":
		for _, c := range n.Children {
			switch c.Kind {
			case "null":
				return facts.ShapeType{Kind: "null"}
			case "undefined":
				return facts.ShapeType{Kind: "undefined"}
			case "string", "number", "true", "false":
				return facts.ShapeType{Kind: "literal", Name: c.Text}
			}
		}
	case "type_identifier", "nested_type_identifier":
		return facts.ShapeType{Kind: "name", Name: n.Text}
	case "generic_type":
		t := facts.ShapeType{Kind: "name", Name: text(n.Fields["name"])}
		if args := n.Fields["type_arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					t.Args = append(t.Args, routeShapeType(a))
				}
			}
		}
		return t
	case "array_type":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				return facts.ShapeType{Kind: "array", Args: []facts.ShapeType{routeShapeType(c)}}
			}
		}
	case "object_type":
		return shapeObject(n)
	case "union_type":
		t := facts.ShapeType{Kind: "union"}
		for _, c := range n.Children {
			if c.Kind == "comment" {
				continue
			}
			member := routeShapeType(c)
			if member.Kind == "union" {
				t.Args = append(t.Args, member.Args...)
			} else {
				t.Args = append(t.Args, member)
			}
		}
		return t
	}
	return facts.ShapeType{Kind: "other"}
}

// shapeObject reads an object type or interface body: property
// signatures; any other member makes it open.
func shapeObject(body *parse.Node) facts.ShapeType {
	t := facts.ShapeType{Kind: "object"}
	if body == nil {
		t.Open = true
		return t
	}
	for _, c := range body.Children {
		switch c.Kind {
		case "property_signature":
			name := c.Fields["name"]
			if name == nil || (name.Kind != "property_identifier" && name.Kind != "string") {
				t.Open = true
				continue
			}
			key := strings.Trim(name.Text, "\"'")
			p := facts.ShapeProp{Name: key, Optional: hasToken(c, "?"), Type: facts.ShapeType{Kind: "other"}}
			if ty := c.Fields["type"]; ty != nil {
				p.Type = routeShapeType(ty)
			}
			t.Props = append(t.Props, p)
		case "comment":
		default:
			t.Open = true
		}
	}
	return t
}

// shapeCollector gathers one function's events.
type shapeCollector struct {
	f       *facts.ShapeFunction
	origins map[string]shapeOrigin
	// consts are the unique const initializers of the body by name.
	consts map[string]*parse.Node
	// top and awaited mark the calls of the body's top-level statements
	// (facts.ShapeEvent Top, Awaited); tests maps such a call to the if
	// statement that tests its result (Test), and pendingIf maps that if
	// statement to the events whose TestAt it sets.
	top       map[*parse.Node]bool
	awaited   map[*parse.Node]bool
	tests     map[*parse.Node]shapeTest
	pendingIf map[*parse.Node][]int
	// stmts numbers the body's top-level statements (from 1) and arms
	// names the branches of its top-level if statements; stmt, arm and
	// armExits are those around the walk position (facts.ShapeEvent Stmt,
	// Arm, ArmExits).
	stmts    map[*parse.Node]int
	arms     map[*parse.Node]shapeArm
	stmt     int
	arm      int
	armExits bool
	// shadow names the parameters of a nested function being walked that
	// reuse an outer parameter name.
	shadow map[string]int
	// count is how often each name is declared in the body.
	count map[string]int
	// try is the number of try blocks around the walk position.
	try int
	// nested is the number of nested functions around the walk position:
	// their return statements are not this function's (T029 SH-N).
	nested int
	// returnAll keeps every return statement of the function (a route
	// handler, a server action); otherwise only response-forming returns
	// are kept unless the function records other events.
	returnAll bool
}

// shapeHTTPMethods are the export names of App Router route handlers.
var shapeHTTPMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true, "OPTIONS": true}

// shapeBodyReaders are the Fetch API request methods that read the body
// (T029 SH-N): a zero-argument call on a parameter path is the parsed
// value, recorded as the path segment "<name>()". clone() keeps the path.
var shapeBodyReaders = map[string]bool{"json": true, "formData": true, "text": true, "arrayBuffer": true, "blob": true}

// shapeFreeRoots are the next/headers functions whose zero-argument call
// reads the current request (parameter -2, path "<name>()"); the linker
// checks that the name is that import.
var shapeFreeRoots = map[string]bool{"headers": true, "cookies": true}

// shapeNavCalls are the next/navigation functions that end a route
// handler or a server action by throwing; their calls are recorded as
// parameter -2 call events with the function name as path.
var shapeNavCalls = map[string]bool{"notFound": true, "redirect": true, "permanentRedirect": true, "forbidden": true, "unauthorized": true}

// shapeSessionCalls are the next-auth functions that read the request's
// session cookie (T029 SH-S23); their calls are recorded as "session"
// events with the function name as path, and the linker checks the
// import.
var shapeSessionCalls = map[string]bool{"getServerSession": true}

func shapeFunction(n, parent *parse.Node, useServer bool) (facts.ShapeFunction, bool) {
	f := facts.ShapeFunction{Offset: int(n.Start), Line: int(n.Row) + 1, EndLine: int(n.EndRow) + 1, EndColumn: int(n.EndColumn)}
	c := &shapeCollector{returnAll: useServer || shapeHTTPMethods[shapeFunctionName(n, parent)], f: &f, origins: map[string]shapeOrigin{}, consts: map[string]*parse.Node{}, shadow: map[string]int{}, top: map[*parse.Node]bool{}, awaited: map[*parse.Node]bool{}, tests: map[*parse.Node]shapeTest{}, pendingIf: map[*parse.Node][]int{}, stmts: map[*parse.Node]int{}, arms: map[*parse.Node]shapeArm{}}
	f.Async = hasToken(n, "async")
	var items []*parse.Node
	if p := n.Fields["parameter"]; p != nil {
		items = []*parse.Node{p}
	} else if p := n.Fields["parameters"]; p != nil {
		for _, item := range p.Children {
			if item.Kind != "comment" {
				items = append(items, item)
			}
		}
	}
	var patterns []int
	for i, item := range items {
		p := shapeParameter(item)
		f.Params = append(f.Params, p)
		if p.Name != "" {
			c.origins[p.Name] = shapeOrigin{param: i}
		}
		if p.Keys != nil || p.Open {
			patterns = append(patterns, i)
		}
	}
	if rt := n.Fields["return_type"]; rt != nil {
		t := routeShapeType(rt)
		f.Returns = &t
	}
	if parent != nil && parent.Kind == "variable_declarator" && parent.Fields["value"] == n {
		if ty := parent.Fields["type"]; ty != nil {
			t := routeShapeType(ty)
			f.Decl = &t
		}
	}
	body := n.Fields["body"]
	if body != nil && body.Kind == "statement_block" && hasDirective(prologue(body), "use server") {
		c.returnAll = true
	}
	if body != nil {
		c.prepare(body)
		c.topLevel(body)
		if body.Kind != "statement_block" {
			// An arrow function's expression body is its return value.
			c.addReturn(body, int(body.Row)+1)
		}
		// Parameter patterns: their keys are read, and each local name
		// is an alias of the key.
		for _, i := range patterns {
			item := items[i]
			pattern := item
			if item.Kind == "required_parameter" || item.Kind == "optional_parameter" {
				pattern = item.Fields["pattern"]
			}
			if pattern != nil && pattern.Kind == "assignment_pattern" {
				pattern = pattern.Fields["left"]
			}
			if pattern != nil && pattern.Kind == "object_pattern" {
				c.destructure(pattern, shapeOrigin{param: i}, int(n.Row)+1)
			}
		}
		c.walk(body)
	}
	c.keepReturns()
	if len(f.Events) > 0 {
		locals := map[string]bool{}
		collectLocals(body, locals)
		for _, p := range f.Params {
			delete(locals, p.Name)
		}
		for name := range locals {
			f.Locals = append(f.Locals, name)
		}
		sort.Strings(f.Locals)
	}
	if body != nil {
		f.ReturnFn = returnedFunction(body, c)
	}
	if len(f.Events) == 0 && f.Returns == nil && f.Decl == nil && f.ReturnFn == 0 {
		// Async alone is not recorded: a function without events
		// passes nothing on.
		return f, false
	}
	return f, true
}

// shapeParameter reads one parameter as written.
func shapeParameter(item *parse.Node) facts.ShapeParam {
	var p facts.ShapeParam
	if item.Kind == "required_parameter" || item.Kind == "optional_parameter" {
		if ty := item.Fields["type"]; ty != nil {
			t := routeShapeType(ty)
			p.Type = &t
		}
		item = item.Fields["pattern"]
	}
	if item != nil && item.Kind == "assignment_pattern" {
		item = item.Fields["left"]
	}
	if item == nil {
		return p
	}
	switch item.Kind {
	case "identifier":
		p.Name = item.Text
	case "rest_pattern":
		p.Rest = true
	case "object_pattern":
		p.Keys = []string{}
		keys, open := patternKeys(item)
		p.Keys, p.Open = keys, open
	default:
		p.Open = true
	}
	return p
}

// patternKeys lists the literal keys of an object pattern; open is true
// when it has a rest element or a computed key.
func patternKeys(n *parse.Node) (keys []string, open bool) {
	keys = []string{}
	for _, c := range n.Children {
		switch c.Kind {
		case "shorthand_property_identifier_pattern":
			keys = append(keys, c.Text)
		case "object_assignment_pattern":
			if left := c.Fields["left"]; left != nil && left.Kind == "shorthand_property_identifier_pattern" {
				keys = append(keys, left.Text)
			} else {
				open = true
			}
		case "pair_pattern":
			key := c.Fields["key"]
			if key != nil && (key.Kind == "property_identifier" || key.Kind == "string") {
				keys = append(keys, strings.Trim(key.Text, "\"'"))
			} else {
				open = true
			}
		case "comment":
		default:
			open = true
		}
	}
	return keys, open
}

// collectLocals names every binding declared in n.
func collectLocals(n *parse.Node, out map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "variable_declarator":
		bindNames(n.Fields["name"], out)
	case "function_declaration", "generator_function_declaration", "class_declaration":
		if name := text(n.Fields["name"]); name != "" {
			out[name] = true
		}
	case "required_parameter", "optional_parameter":
		bindNames(n.Fields["pattern"], out)
	case "catch_clause":
		bindNames(n.Fields["parameter"], out)
	}
	if shapeFunctionKinds[n.Kind] {
		if p := n.Fields["parameter"]; p != nil {
			bindNames(p, out)
		}
	}
	for _, c := range n.Children {
		collectLocals(c, out)
	}
}

func bindNames(n *parse.Node, out map[string]bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "identifier", "shorthand_property_identifier_pattern":
		out[n.Text] = true
		return
	case "property_identifier":
		return
	case "pair_pattern":
		bindNames(n.Fields["value"], out)
		return
	}
	for _, c := range n.Children {
		bindNames(c, out)
	}
}

// prepare reads the const declarations of the body (aliases of parameter
// paths and initializers) and the identifier and member references the
// Checked flag reads.
func (c *shapeCollector) prepare(body *parse.Node) {
	count := map[string]int{}
	c.count = count
	var decls []*parse.Node
	var scan func(n *parse.Node, isConst bool)
	scan = func(n *parse.Node, isConst bool) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "lexical_declaration":
			isConst = hasToken(n, "const")
		case "variable_declaration":
			isConst = false
		case "variable_declarator":
			names := map[string]bool{}
			bindNames(n.Fields["name"], names)
			for name := range names {
				count[name]++
			}
			if isConst {
				decls = append(decls, n)
			}
		}
		if shapeFunctionKinds[n.Kind] && n.Fields["parameter"] != nil {
			count[n.Fields["parameter"].Text]++
		}
		if n.Kind == "required_parameter" || n.Kind == "optional_parameter" {
			names := map[string]bool{}
			bindNames(n.Fields["pattern"], names)
			for name := range names {
				count[name]++
			}
		}
		for _, ch := range n.Children {
			scan(ch, isConst)
		}
	}
	scan(body, false)
	for _, d := range decls {
		name := d.Fields["name"]
		if name != nil && name.Kind == "identifier" && count[name.Text] == 1 {
			if v := d.Fields["value"]; v != nil {
				c.consts[name.Text] = v
			}
		}
	}
	// Aliases in source order: `const b = req.body`, `const { body } = req`.
	for _, d := range decls {
		name, value := d.Fields["name"], d.Fields["value"]
		if name == nil || value == nil {
			continue
		}
		o, ok := c.root(value)
		if !ok {
			continue
		}
		if name.Kind == "identifier" && count[name.Text] == 1 {
			c.origins[name.Text] = o
		}
	}
	// Names declared more than once never alias.
	for name := range c.origins {
		if count[name] > 1 {
			if o := c.origins[name]; len(o.path) > 0 {
				delete(c.origins, name)
			}
		}
	}
}

// unwrap skips parentheses, await, non-null and type assertions.
func shapeUnwrap(n *parse.Node) *parse.Node {
	for n != nil {
		u := shapeUnwrap1(n)
		if u == n {
			return n
		}
		n = u
	}
	return n
}

// shapeUnwrap1 skips one wrapper shapeUnwrap skips (n itself when none).
func shapeUnwrap1(n *parse.Node) *parse.Node {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "await_expression", "non_null_expression", "as_expression", "satisfies_expression", "type_assertion":
			var inner *parse.Node
			for _, ch := range n.Children {
				if ch.Kind != "comment" && !strings.HasSuffix(ch.Kind, "type") && ch.Kind != "type_identifier" && ch.Kind != "type_arguments" {
					inner = ch
					break
				}
			}
			if n.Kind == "type_assertion" {
				inner = nil
				for i := len(n.Children) - 1; i >= 0; i-- {
					if n.Children[i].Kind != "comment" {
						inner = n.Children[i]
						break
					}
				}
			}
			if inner == nil {
				return n
			}
			return inner
		default:
			return n
		}
	}
	return n
}

// hasAwait reports an await among the wrappers shapeUnwrap skips.
func hasAwait(n *parse.Node) bool {
	for n != nil {
		if n.Kind == "await_expression" {
			return true
		}
		u := shapeUnwrap1(n)
		if u == n {
			return false
		}
		n = u
	}
	return false
}

// root returns the parameter path n stands for: a parameter, an alias,
// or a literal member chain of one. A computed key ends the path (whole).
func (c *shapeCollector) root(n *parse.Node) (shapeOrigin, bool) {
	o, whole, ok := c.rootWhole(n)
	return o, ok && !whole
}

func (c *shapeCollector) rootWhole(n *parse.Node) (shapeOrigin, bool, bool) {
	awaited := hasAwait(n)
	n = shapeUnwrap(n)
	if n == nil {
		return shapeOrigin{}, false, false
	}
	switch n.Kind {
	case "identifier":
		if c.shadow[n.Text] > 0 {
			return shapeOrigin{}, false, false
		}
		o, ok := c.origins[n.Text]
		return o, false, ok
	case "member_expression":
		o, whole, ok := c.rootWhole(n.Fields["object"])
		if !ok || whole {
			return o, whole, ok
		}
		prop := n.Fields["property"]
		if prop == nil || prop.Kind != "property_identifier" {
			return o, true, true
		}
		return shapeOrigin{param: o.param, path: appendPath(o.path, prop.Text)}, false, true
	case "subscript_expression":
		o, whole, ok := c.rootWhole(n.Fields["object"])
		if !ok || whole {
			return o, whole, ok
		}
		if idx := n.Fields["index"]; idx != nil && idx.Kind == "string" && len(idx.Text) >= 2 {
			return shapeOrigin{param: o.param, path: appendPath(o.path, idx.Text[1:len(idx.Text)-1])}, false, true
		}
		return o, true, true
	case "call_expression":
		// Fetch API body readers (`await request.json()`) and the
		// next/headers functions (`await headers()`) are values the shape
		// rows read (T029 SH-N); any other call is not a parameter path.
		if argCount(n.Fields["arguments"]) != 0 {
			return shapeOrigin{}, false, false
		}
		fn := shapeUnwrap(n.Fields["function"])
		if fn == nil {
			return shapeOrigin{}, false, false
		}
		if fn.Kind == "identifier" && shapeFreeRoots[fn.Text] && c.shadow[fn.Text] == 0 {
			// An un-awaited call is recorded apart ("headers()sync"): Next.js
			// 16 removed synchronous access, so the linker claims no name
			// read through it.
			root := fn.Text + "()"
			if !awaited {
				root += "sync"
			}
			return shapeOrigin{param: -2, path: []string{root}}, false, true
		}
		if fn.Kind != "member_expression" {
			return shapeOrigin{}, false, false
		}
		prop := text(fn.Fields["property"])
		if prop != "clone" && !shapeBodyReaders[prop] {
			return shapeOrigin{}, false, false
		}
		o, whole, ok := c.rootWhole(fn.Fields["object"])
		if !ok || whole {
			return o, whole, ok
		}
		if prop == "clone" {
			return o, false, true
		}
		return shapeOrigin{param: o.param, path: appendPath(o.path, prop+"()")}, false, true
	case "new_expression":
		// `new URL(request.url)`: the URL whose searchParams are the query.
		if ctor := n.Fields["constructor"]; ctor == nil || ctor.Kind != "identifier" || ctor.Text != "URL" || c.shadow["URL"] > 0 {
			return shapeOrigin{}, false, false
		}
		args := n.Fields["arguments"]
		if args == nil || argCount(args) == 0 {
			return shapeOrigin{}, false, false
		}
		o, whole, ok := c.rootWhole(firstChild(args))
		if !ok || whole {
			return shapeOrigin{}, false, false
		}
		return shapeOrigin{param: o.param, path: appendPath(o.path, "URL()")}, false, true
	}
	return shapeOrigin{}, false, false
}

// argCount is the number of arguments of an arguments node.
func argCount(args *parse.Node) int {
	if args == nil {
		return 0
	}
	n := 0
	for _, a := range args.Children {
		if a.Kind != "comment" {
			n++
		}
	}
	return n
}

// shapeFunctionName is the name a function is declared or bound with
// (`function GET`, `const GET = async () => ...`), "" when none.
func shapeFunctionName(n, parent *parse.Node) string {
	if name := text(n.Fields["name"]); name != "" {
		return name
	}
	if parent != nil && parent.Kind == "variable_declarator" && parent.Fields["value"] == n {
		if name := parent.Fields["name"]; name != nil && name.Kind == "identifier" {
			return name.Text
		}
	}
	return ""
}

// addReturn records a return statement (or an arrow function's expression
// body) of the function itself as a "return" event (parameter -1): the
// returned expression as written, the initializer of a returned const,
// and the arguments of the returned call or construction with the
// initializers of the consts among them (T029 SH-N).
func (c *shapeCollector) addReturn(value *parse.Node, line int) {
	m := facts.ShapeMethod{Name: "return"}
	var call *parse.Node
	if value != nil {
		m.Args = []facts.ShapeExpr{shapeExpr(value, 0)}
		u := shapeUnwrap(value)
		call = u
		if u != nil && u.Kind == "identifier" && c.shadow[u.Text] == 0 {
			if v := c.consts[u.Text]; v != nil {
				e := shapeExpr(shapeUnwrap(v), 0)
				m.Inits = []*facts.ShapeExpr{&e}
				call = shapeUnwrap(v)
			}
		}
	}
	methods := []facts.ShapeMethod{m}
	if call != nil && (call.Kind == "call_expression" || call.Kind == "new_expression") {
		a := facts.ShapeMethod{Name: "args"}
		if args := call.Fields["arguments"]; args != nil {
			for _, arg := range args.Children {
				if arg.Kind == "comment" {
					continue
				}
				a.Args = append(a.Args, shapeExpr(arg, 0))
				var init *facts.ShapeExpr
				if u := shapeUnwrap(arg); u != nil && u.Kind == "identifier" && c.shadow[u.Text] == 0 {
					if v := c.consts[u.Text]; v != nil {
						e := shapeExpr(shapeUnwrap(v), 0)
						init = &e
					}
				}
				a.Inits = append(a.Inits, init)
			}
		}
		if !hasInit(a.Inits) {
			a.Inits = nil
		}
		methods = append(methods, a)
	}
	c.add(facts.ShapeEvent{Kind: "return", Param: -1, Methods: methods, InTry: c.try > 0, Line: line})
}

// responseForm reports a returned expression that builds a response with
// the Fetch or Next.js API (`Response.json(...)`, `NextResponse.redirect(
// ...)`, `new Response(...)`), directly or through a returned const.
func responseForm(e facts.ShapeEvent) bool {
	exprs := e.Methods[0].Args
	if len(e.Methods[0].Inits) > 0 && e.Methods[0].Inits[0] != nil {
		exprs = append(exprs, *e.Methods[0].Inits[0])
	}
	for _, x := range exprs {
		switch {
		case x.Kind == "new_expression" && x.Object != nil && x.Object.Kind == "identifier" && (x.Object.Name == "Response" || x.Object.Name == "NextResponse"):
			return true
		case x.Kind == "call_expression" && x.Object != nil && x.Object.Kind == "member_expression" && x.Object.Object != nil && x.Object.Object.Kind == "identifier" && (x.Object.Object.Name == "Response" || x.Object.Object.Name == "NextResponse"):
			return true
		}
	}
	return false
}

// keepReturns drops the return events of a function that is neither a
// route handler nor a server action, unless they build a response or the
// function records other events: a helper that returns no response is
// never read for one, and the cache stays small. Test indexes (TestAt)
// are renumbered.
func (c *shapeCollector) keepReturns() {
	if c.returnAll {
		return
	}
	other := false
	for _, e := range c.f.Events {
		if e.Kind != "return" && e.Kind != "throw" {
			other = true
			break
		}
	}
	if other {
		return
	}
	kept := c.f.Events[:0]
	var removed []int
	for i, e := range c.f.Events {
		if e.Kind == "return" && !responseForm(e) {
			removed = append(removed, i)
			continue
		}
		kept = append(kept, e)
	}
	if len(removed) == 0 {
		return
	}
	for i := range kept {
		if kept[i].TestAt == 0 {
			continue
		}
		before := 0
		for _, r := range removed {
			if r < kept[i].TestAt {
				before++
			}
		}
		kept[i].TestAt -= before
	}
	c.f.Events = kept
}

func appendPath(p []string, s string) []string {
	out := make([]string, 0, len(p)+1)
	return append(append(out, p...), s)
}

func (c *shapeCollector) add(e facts.ShapeEvent) {
	e.Stmt, e.Arm, e.ArmExits = c.stmt, c.arm, c.armExits
	c.f.Events = append(c.f.Events, e)
}

// destructure records the keys an object pattern reads at origin o and
// aliases its local names.
func (c *shapeCollector) destructure(pattern *parse.Node, o shapeOrigin, line int) {
	keys, open := patternKeys(pattern)
	c.add(facts.ShapeEvent{Kind: "keys", Param: o.param, Path: o.path, Keys: keys, Open: open, Line: line})
	for _, ch := range pattern.Children {
		switch ch.Kind {
		case "shorthand_property_identifier_pattern":
			if c.count[ch.Text] > 1 {
				continue
			}
			c.origins[ch.Text] = shapeOrigin{param: o.param, path: appendPath(o.path, ch.Text)}
		case "object_assignment_pattern":
			if left := ch.Fields["left"]; left != nil && left.Kind == "shorthand_property_identifier_pattern" && c.count[left.Text] <= 1 {
				c.origins[left.Text] = shapeOrigin{param: o.param, path: appendPath(o.path, left.Text)}
			}
		case "pair_pattern":
			key, value := ch.Fields["key"], ch.Fields["value"]
			if key == nil || (key.Kind != "property_identifier" && key.Kind != "string") || value == nil {
				continue
			}
			k := strings.Trim(key.Text, "\"'")
			if value.Kind == "assignment_pattern" {
				value = value.Fields["left"]
			}
			if value == nil {
				continue
			}
			switch value.Kind {
			case "identifier":
				if c.count[value.Text] > 1 {
					continue
				}
				c.origins[value.Text] = shapeOrigin{param: o.param, path: appendPath(o.path, k)}
			case "object_pattern":
				c.destructure(value, shapeOrigin{param: o.param, path: appendPath(o.path, k)}, line)
			}
		}
	}
}

// walk records the events of n.
func (c *shapeCollector) walk(n *parse.Node) {
	if n == nil {
		return
	}
	line := int(n.Row) + 1
	if i, ok := c.stmts[n]; ok {
		saved := [2]int{c.stmt, c.arm}
		c.stmt, c.arm, c.armExits = i, 0, false
		defer func() { c.stmt, c.arm, c.armExits = saved[0], saved[1], false }()
	}
	if a, ok := c.arms[n]; ok {
		c.arm, c.armExits = a.arm, a.exits
		defer func() { c.arm, c.armExits = 0, false }()
	}
	switch n.Kind {
	case "comment":
		return
	case "throw_statement":
		c.add(facts.ShapeEvent{Kind: "throw", Param: -1, Line: line})
	case "return_statement":
		if c.nested == 0 {
			// The returned expression's own uses come first: a value
			// computed for the return runs before the response is built.
			for _, ch := range n.Children {
				c.walk(ch)
			}
			c.addReturn(firstChild(n), line)
			return
		}
	case "if_statement":
		// The events before a tested if statement (TestAt).
		for _, i := range c.pendingIf[n] {
			c.f.Events[i].TestAt = len(c.f.Events)
		}
	case "try_statement":
		c.try++
		c.walk(n.Fields["body"])
		c.try--
		c.walk(n.Fields["handler"])
		c.walk(n.Fields["finalizer"])
		return
	case "variable_declarator":
		name, value := n.Fields["name"], n.Fields["value"]
		if value != nil {
			if o, ok := c.root(value); ok {
				if name != nil && name.Kind == "identifier" {
					if a, aliased := c.origins[name.Text]; !aliased || a.param != o.param || strings.Join(a.path, ".") != strings.Join(o.path, ".") {
						// The value escapes into a local the walk does not track.
						c.add(facts.ShapeEvent{Kind: "read", Param: o.param, Path: o.path, Whole: true, Line: line})
					} else {
						// The path is read into a const alias.
						c.add(facts.ShapeEvent{Kind: "read", Param: o.param, Path: o.path, Bound: true, Line: line})
					}
				}
				if ty := n.Fields["type"]; ty != nil {
					t := routeShapeType(ty)
					c.add(facts.ShapeEvent{Kind: "cast", Param: o.param, Path: o.path, Type: &t, Line: line})
				}
				c.cast(value)
				if name != nil && name.Kind == "object_pattern" {
					c.destructure(name, o, line)
				}
				return
			}
		}
		if value != nil && name != nil {
			if call := shapeUnwrap(value); call != nil && call.Kind == "call_expression" {
				c.call(call)
				return
			}
		}
		c.walk(value)
		return
	case "assignment_expression", "augmented_assignment_expression":
		if o, whole, ok := c.rootWhole(n.Fields["left"]); ok {
			c.add(facts.ShapeEvent{Kind: "assign", Param: o.param, Path: o.path, Whole: whole, Line: line})
			c.walk(n.Fields["right"])
			return
		}
	case "call_expression":
		c.call(n)
		return
	case "new_expression":
		callee := facts.ShapeExpr{Kind: "new"}
		c.args(n.Fields["arguments"], &callee, nil, line)
		return
	case "as_expression", "satisfies_expression", "type_assertion":
		if c.cast(n) {
			c.walk(shapeUnwrap(n))
			return
		}
	case "spread_element":
		if o, _, ok := c.rootWhole(firstChild(n)); ok {
			c.add(facts.ShapeEvent{Kind: "read", Param: o.param, Path: o.path, Whole: true, Line: line})
			return
		}
	case "identifier", "member_expression", "subscript_expression":
		if o, whole, ok := c.rootWhole(n); ok {
			c.add(facts.ShapeEvent{Kind: "read", Param: o.param, Path: o.path, Whole: whole, Line: line})
			if n.Kind == "subscript_expression" {
				c.walk(n.Fields["index"])
			}
			return
		}
	}
	if shapeFunctionKinds[n.Kind] && n != nil {
		// A nested function: its own parameters shadow outer names.
		names := map[string]bool{}
		if p := n.Fields["parameter"]; p != nil {
			bindNames(p, names)
		}
		if p := n.Fields["parameters"]; p != nil {
			bindNames(p, names)
		}
		for name := range names {
			c.shadow[name]++
		}
		c.nested++
		defer func() {
			c.nested--
			for name := range names {
				c.shadow[name]--
			}
		}()
	}
	for _, ch := range n.Children {
		c.walk(ch)
	}
}

func firstChild(n *parse.Node) *parse.Node {
	for _, ch := range n.Children {
		if ch.Kind != "comment" {
			return ch
		}
	}
	return nil
}

// cast records `x as T` (and `<T>x`) of a parameter path.
func (c *shapeCollector) cast(n *parse.Node) bool {
	if n == nil || (n.Kind != "as_expression" && n.Kind != "satisfies_expression" && n.Kind != "type_assertion") {
		return false
	}
	o, ok := c.root(n)
	if !ok {
		return false
	}
	var ty *parse.Node
	for _, ch := range n.Children {
		if ch.Kind != "comment" && (strings.HasSuffix(ch.Kind, "type") || ch.Kind == "type_identifier" || ch.Kind == "type_arguments") {
			ty = ch
		}
	}
	if ty == nil {
		return false
	}
	t := routeShapeType(ty)
	if ty.Kind == "type_arguments" {
		t = routeShapeType(firstChild(ty))
	}
	c.add(facts.ShapeEvent{Kind: "cast", Param: o.param, Path: o.path, Type: &t, Line: int(n.Row) + 1})
	return true
}

// call records a call: methods called on a parameter path (a "call"
// event), or parameter paths passed to another callee ("pass" events).
func (c *shapeCollector) call(n *parse.Node) {
	line := int(n.Row) + 1
	if fn := shapeUnwrap(n.Fields["function"]); fn != nil && fn.Kind == "identifier" && shapeNavCalls[fn.Text] && c.shadow[fn.Text] == 0 {
		// notFound(), redirect(url): the linker checks the import.
		m := facts.ShapeMethod{}
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					m.Args = append(m.Args, shapeExpr(a, 0))
				}
			}
		}
		c.add(facts.ShapeEvent{Kind: "call", Param: -2, Path: []string{fn.Text}, Methods: []facts.ShapeMethod{m}, InTry: c.try > 0, Nested: c.nested > 0, Line: line})
	}
	if fn := shapeUnwrap(n.Fields["function"]); fn != nil && fn.Kind == "identifier" && shapeSessionCalls[fn.Text] && c.shadow[fn.Text] == 0 {
		c.add(facts.ShapeEvent{Kind: "session", Param: -2, Path: []string{fn.Text}, Nested: c.nested > 0, Line: line})
	}
	// Flatten a method chain on a parameter path: res.status(201).json(v).
	var methods []facts.ShapeMethod
	var argNodes []*parse.Node
	cur := n
	for {
		callee := shapeUnwrap(cur.Fields["function"])
		if callee == nil {
			break
		}
		m := facts.ShapeMethod{}
		args := cur.Fields["arguments"]
		if args != nil {
			for _, a := range args.Children {
				if a.Kind == "comment" {
					continue
				}
				m.Args = append(m.Args, shapeExpr(a, 0))
				var init *facts.ShapeExpr
				if a.Kind == "identifier" && c.shadow[a.Text] == 0 {
					if v := c.consts[a.Text]; v != nil {
						e := shapeExpr(shapeUnwrap(v), 0)
						init = &e
					}
				}
				m.Inits = append(m.Inits, init)
			}
			argNodes = append(argNodes, args)
		}
		if !hasInit(m.Inits) {
			m.Inits = nil
		}
		if callee.Kind != "member_expression" {
			if o, ok := c.root(callee); ok && len(methods) == 0 {
				// A call of the parameter itself: next(err).
				c.add(facts.ShapeEvent{Kind: "call", Param: o.param, Path: o.path, Methods: []facts.ShapeMethod{m}, Line: line})
				for _, a := range argNodes {
					c.walk(a)
				}
				return
			}
		}
		if callee.Kind == "member_expression" {
			m.Name = text(callee.Fields["property"])
			methods = append([]facts.ShapeMethod{m}, methods...)
			object := shapeUnwrap(callee.Fields["object"])
			if o, ok := c.root(callee.Fields["object"]); ok {
				c.add(facts.ShapeEvent{Kind: "call", Param: o.param, Path: o.path, Methods: methods, Line: line})
				for _, a := range argNodes {
					c.walk(a)
				}
				return
			}
			if object != nil && object.Kind == "call_expression" {
				cur = object
				continue
			}
		}
		break
	}
	// Not a chain on a parameter: the outer call's arguments may pass
	// parameter paths; walk the callee and the arguments for other uses.
	calleeExpr := shapeExpr(shapeUnwrap(n.Fields["function"]), 0)
	c.args(n.Fields["arguments"], &calleeExpr, n, line)
	c.walk(n.Fields["function"])
}

func hasInit(inits []*facts.ShapeExpr) bool {
	for _, i := range inits {
		if i != nil {
			return true
		}
	}
	return false
}

// args records the parameter paths passed as arguments of a call to
// callee (call is the call node, nil for new), and walks every other
// argument.
func (c *shapeCollector) args(args *parse.Node, callee *facts.ShapeExpr, call *parse.Node, line int) {
	if args == nil {
		return
	}
	argc := 0
	for _, a := range args.Children {
		if a.Kind != "comment" {
			argc++
		}
	}
	i := 0
	for _, a := range args.Children {
		if a.Kind == "comment" {
			continue
		}
		if o, whole, ok := c.rootWhole(a); ok {
			c.cast(shapeUnwrap2(a))
			e := facts.ShapeEvent{Kind: "pass", Param: o.param, Path: o.path, Whole: whole, Callee: callee, Arg: i, Argc: argc, InTry: c.try > 0, Line: line}
			if call != nil && c.top[call] {
				e.Top, e.Awaited = true, c.awaited[call]
				if t, ok := c.tests[call]; ok {
					e.Test = t.kind
					c.pendingIf[t.at] = append(c.pendingIf[t.at], len(c.f.Events))
				}
			}
			c.add(e)
		} else if spreadOf(a, c) {
			c.walk(a)
			for _, o := range c.spreads(a) {
				c.add(facts.ShapeEvent{Kind: "pass", Param: o.param, Path: o.path, Callee: callee, Arg: i, Argc: argc, Transformed: true, InTry: c.try > 0, Line: line})
			}
		} else {
			c.walk(a)
		}
		i++
	}
}

// shapeUnwrap2 skips parentheses and await only, keeping a type assertion.
func shapeUnwrap2(n *parse.Node) *parse.Node {
	for n != nil && (n.Kind == "parenthesized_expression" || n.Kind == "await_expression") {
		n = firstChild(n)
	}
	return n
}

// spreadOf reports an object literal argument that spreads a parameter
// path ({...req.body, id}).
func spreadOf(a *parse.Node, c *shapeCollector) bool {
	return len(c.spreads(a)) > 0
}

func (c *shapeCollector) spreads(a *parse.Node) []shapeOrigin {
	a = shapeUnwrap(a)
	if a == nil || a.Kind != "object" {
		return nil
	}
	var out []shapeOrigin
	for _, ch := range a.Children {
		if ch.Kind == "spread_element" {
			if o, ok := c.root(firstChild(ch)); ok {
				out = append(out, o)
			}
		}
	}
	return out
}

// shapeExpr reduces an expression to the compact form the shape linker
// reads, at most 8 levels deep (deeper parts are "other").
func shapeExpr(n *parse.Node, depth int) facts.ShapeExpr {
	n = shapeUnwrap(n)
	if n == nil {
		return facts.ShapeExpr{Kind: "other"}
	}
	if depth > 8 {
		return facts.ShapeExpr{Kind: "other"}
	}
	e := facts.ShapeExpr{Kind: n.Kind}
	switch n.Kind {
	case "identifier", "property_identifier", "this":
		e.Name = n.Text
	case "member_expression":
		o := shapeExpr(n.Fields["object"], depth+1)
		e.Object = &o
		e.Name = text(n.Fields["property"])
	case "call_expression":
		o := shapeExpr(n.Fields["function"], depth+1)
		e.Object = &o
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					e.Args = append(e.Args, shapeExpr(a, depth+1))
				}
			}
		}
	case "new_expression":
		o := shapeExpr(n.Fields["constructor"], depth+1)
		e.Object = &o
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					e.Args = append(e.Args, shapeExpr(a, depth+1))
				}
			}
		}
	case "null", "undefined", "true", "false":
	case "number":
		e.Name = n.Text
	case "string", "template_string":
		literal := len(n.Text) >= 2
		for _, ch := range n.Children {
			if ch.Kind == "template_substitution" {
				literal = false
			}
		}
		if literal {
			e.Kind, e.Name = "literal", n.Text[1:len(n.Text)-1]
		}
	case "object":
		e.Props = map[string]facts.ShapeExpr{}
		for _, ch := range n.Children {
			switch ch.Kind {
			case "pair":
				key := ch.Fields["key"]
				if key == nil || (key.Kind != "property_identifier" && key.Kind != "string" && key.Kind != "number") {
					e.Open = true
					continue
				}
				e.Props[strings.Trim(key.Text, "\"'`")] = shapeExpr(ch.Fields["value"], depth+1)
			case "shorthand_property_identifier":
				e.Props[ch.Text] = facts.ShapeExpr{Kind: "identifier", Name: ch.Text}
			case "method_definition":
				e.Props[text(ch.Fields["name"])] = facts.ShapeExpr{Kind: "method_definition"}
			case "comment":
			default:
				e.Open = true
			}
		}
	case "array":
		for _, ch := range n.Children {
			if ch.Kind != "comment" {
				e.Args = append(e.Args, shapeExpr(ch, depth+1))
			}
		}
	}
	return e
}

// shapeArm is a branch of a top-level if statement: 1 the consequence, 2
// the alternative; exits when it always ends in return or throw.
type shapeArm struct {
	arm   int
	exits bool
}

// shapeTest is the if statement that tests a top-level call's result.
type shapeTest struct {
	kind string
	at   *parse.Node
}

// topLevel marks the calls of the body's top-level statements (Top,
// Awaited) and finds the if statements that test a const result of one
// (Test). An arrow function's expression body is its top level.
func (c *shapeCollector) topLevel(body *parse.Node) {
	if body.Kind != "statement_block" {
		c.markTop(body)
		return
	}
	type result struct {
		call *parse.Node
		name *parse.Node
	}
	var results []result
	for i, st := range body.Children {
		c.stmts[st] = i + 1
		if st.Kind == "if_statement" {
			if cons := st.Fields["consequence"]; cons != nil {
				c.arms[cons] = shapeArm{arm: 1, exits: alwaysExits(cons)}
			}
			if alt := st.Fields["alternative"]; alt != nil {
				c.arms[alt] = shapeArm{arm: 2, exits: alwaysExits(firstChild(alt))}
			}
		}
		switch st.Kind {
		case "expression_statement":
			c.markTop(firstChild(st))
		case "lexical_declaration", "variable_declaration":
			for _, d := range st.Children {
				if d.Kind != "variable_declarator" {
					continue
				}
				call := c.markTop(d.Fields["value"])
				if call != nil && st.Kind == "lexical_declaration" && hasToken(st, "const") && d.Fields["name"] != nil {
					results = append(results, result{call: call, name: d.Fields["name"]})
				}
			}
		case "if_statement":
			if !alwaysExits(st.Fields["consequence"]) {
				continue
			}
			for _, r := range results {
				if _, done := c.tests[r.call]; done {
					continue
				}
				if kind := c.failureTest(st.Fields["condition"], r.name); kind != "" {
					c.tests[r.call] = shapeTest{kind: kind, at: st}
				}
			}
		}
	}
}

// markTop marks the call an expression is (under parentheses, await,
// non-null and type assertions) as a top-level call and returns it.
func (c *shapeCollector) markTop(n *parse.Node) *parse.Node {
	awaited := false
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "non_null_expression":
			n = firstChild(n)
			continue
		case "await_expression":
			awaited = true
			n = firstChild(n)
			continue
		case "as_expression", "satisfies_expression", "type_assertion":
			n = shapeUnwrap(n)
			if n != nil && (n.Kind == "as_expression" || n.Kind == "satisfies_expression" || n.Kind == "type_assertion") {
				return nil
			}
			continue
		case "call_expression":
			c.top[n] = true
			c.awaited[n] = awaited
			return n
		}
		return nil
	}
	return nil
}

// failureTest reports the failure test a condition makes of a result
// bound to name: "error" for `r.error` or a destructured `error`,
// "success" for `!r.success` or a destructured `!success`, alone or as an
// operand of `||`. Any other condition (an `&&`, the success test without
// `!`) is no test. Bindings are matched by name only when the name is
// declared once in the function.
func (c *shapeCollector) failureTest(cond, name *parse.Node) string {
	cond = unparen(cond)
	if cond == nil {
		return ""
	}
	if cond.Kind == "binary_expression" && hasToken(cond, "||") {
		if k := c.failureTest(cond.Fields["left"], name); k != "" {
			return k
		}
		return c.failureTest(cond.Fields["right"], name)
	}
	negated := false
	if cond.Kind == "unary_expression" && hasToken(cond, "!") {
		negated = true
		cond = unparen(cond.Fields["argument"])
		if cond == nil {
			return ""
		}
	}
	switch name.Kind {
	case "identifier":
		if c.count[name.Text] != 1 || cond.Kind != "member_expression" {
			return ""
		}
		object := unparen(cond.Fields["object"])
		if object == nil || object.Kind != "identifier" || object.Text != name.Text {
			return ""
		}
		switch prop := text(cond.Fields["property"]); {
		case prop == "error" && !negated:
			return "error"
		case prop == "success" && negated:
			return "success"
		}
	case "object_pattern":
		if cond.Kind != "identifier" || c.count[cond.Text] != 1 {
			return ""
		}
		for _, ch := range name.Children {
			key, local := "", ""
			switch ch.Kind {
			case "shorthand_property_identifier_pattern":
				key, local = ch.Text, ch.Text
			case "pair_pattern":
				if v := ch.Fields["value"]; v != nil && v.Kind == "identifier" {
					key, local = strings.Trim(text(ch.Fields["key"]), "\"'"), v.Text
				}
			}
			if local != cond.Text {
				continue
			}
			switch {
			case key == "error" && !negated:
				return "error"
			case key == "success" && negated:
				return "success"
			}
		}
	}
	return ""
}

func unparen(n *parse.Node) *parse.Node {
	for n != nil && n.Kind == "parenthesized_expression" {
		n = firstChild(n)
	}
	return n
}

// alwaysExits reports a consequence that always ends the function: a
// return or throw statement, or a block whose last statement is one.
func alwaysExits(n *parse.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case "return_statement", "throw_statement":
		return true
	case "statement_block":
		for i := len(n.Children) - 1; i >= 0; i-- {
			ch := n.Children[i]
			if ch.Kind == "comment" {
				continue
			}
			return ch.Kind == "return_statement" || ch.Kind == "throw_statement"
		}
	}
	return false
}

// returnedFunction is the start offset of the one function a factory
// returns: an expression body or a body whose last statement is a return,
// every return statement of the body (outside nested functions) returning
// the same inline function or the same const of the body bound to one.
// It is 0 otherwise.
func returnedFunction(body *parse.Node, c *shapeCollector) int {
	fnOf := func(n *parse.Node) int {
		n = unparen(n)
		if n == nil {
			return 0
		}
		if n.Kind == "identifier" && c.count[n.Text] == 1 {
			n = unparen(c.consts[n.Text])
		}
		if n != nil && (n.Kind == "arrow_function" || n.Kind == "function_expression") {
			return int(n.Start)
		}
		return 0
	}
	if body.Kind != "statement_block" {
		return fnOf(body)
	}
	last := -1
	for i := len(body.Children) - 1; i >= 0; i-- {
		if body.Children[i].Kind != "comment" {
			last = i
			break
		}
	}
	if last < 0 || body.Children[last].Kind != "return_statement" {
		return 0
	}
	at := 0
	ok := true
	var walk func(n *parse.Node)
	walk = func(n *parse.Node) {
		if n == nil || !ok || shapeFunctionKinds[n.Kind] {
			return
		}
		if n.Kind == "return_statement" {
			got := fnOf(firstChild(n))
			if got == 0 || at != 0 && got != at {
				ok = false
				return
			}
			at = got
			return
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(body)
	if !ok {
		return 0
	}
	return at
}

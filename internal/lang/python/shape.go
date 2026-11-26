package python

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the reduced syntax the FastAPI route shape linker
// reads (T029, docs/contract/claims/route-shape-fastapi.md):
// function signatures, the body sites that can add to a route's request
// or response shape, and the members of classes that may be Pydantic
// models. It is syntax only; every claim is decided at link time.

// shapeDepth bounds reduced annotations, defaults and call arguments.
const shapeDepth = 8

// shapeModules are gate SG's top-level modules.
var shapeModules = map[string]bool{"fastapi": true, "starlette": true, "pydantic": true, "sqlmodel": true}

// shapeGate is gate SG: the file has an absolute import of fastapi,
// starlette, pydantic or sqlmodel (or a submodule).
func shapeGate(imports []facts.Import) bool {
	for _, imp := range imports {
		first := strings.SplitN(imp.Module, ".", 2)[0]
		if !strings.HasPrefix(imp.Module, ".") && shapeModules[first] {
			return true
		}
	}
	return false
}

// watchedTypes are the annotations whose parameters the capture watches:
// a request object or a header/cookie/query mapping, whose reads by name
// the linker lists. Unannotated parameters are watched too.
var watchedTypes = map[string]bool{
	"Request": true, "HTTPConnection": true, "WebSocket": true, "Headers": true, "QueryParams": true, "Response": true,
	"dict": true, "Dict": true, "Mapping": true, "MutableMapping": true,
}

// shapeBuiltins are names whose calls never reach repository code: a call
// site for them is not recorded.
var shapeBuiltins = setOf("len", "str", "int", "float", "bool", "print", "isinstance", "issubclass", "list", "dict", "set",
	"tuple", "range", "enumerate", "zip", "getattr", "setattr", "hasattr", "super", "type", "any", "all", "sorted", "min",
	"max", "sum", "repr", "iter", "next", "map", "filter", "callable", "id", "hash", "round", "abs", "format", "vars",
	"open", "frozenset", "bytes", "object", "reversed", "divmod", "ord", "chr")

// shapeExpr reduces an annotation, default or call to the forms the shape
// linker reads. It keeps the RouteExpr conventions of dependencyExpr (so
// the security resolver's helpers read it) and adds unions, numbers,
// dictionaries and literal strings.
func shapeExpr(n *parse.Node, depth int) facts.RouteExpr {
	for n != nil && (n.Kind == "type" || n.Kind == "parenthesized_expression" || n.Kind == "await") && len(namedChildren(n)) == 1 {
		n = namedChildren(n)[0]
	}
	if n == nil {
		return facts.RouteExpr{}
	}
	if depth > shapeDepth {
		return facts.RouteExpr{Kind: "other", Offset: int(n.Start)}
	}
	switch n.Kind {
	case "identifier":
		return facts.RouteExpr{Kind: "identifier", Name: identifier(n.Text), Text: n.Text, Offset: int(n.Start)}
	case "attribute":
		o := shapeExpr(n.Fields["object"], depth+1)
		return facts.RouteExpr{Kind: "attribute", Name: facts.Text(n.Fields["attribute"]), Object: &o, Text: compactText(n.Text), Offset: int(n.Start)}
	case "string", "concatenated_string":
		if s, ok := literal(n); ok && len(s) <= 512 {
			return facts.RouteExpr{Kind: "literal", Name: s, Offset: int(n.Start)}
		}
		return facts.RouteExpr{Kind: "string", Offset: int(n.Start)}
	case "integer", "float":
		return facts.RouteExpr{Kind: n.Kind, Name: n.Text, Text: n.Text, Offset: int(n.Start)}
	case "unary_operator":
		arg := n.Fields["argument"]
		if arg != nil && (arg.Kind == "integer" || arg.Kind == "float") && strings.HasPrefix(strings.TrimSpace(n.Text), "-") {
			return facts.RouteExpr{Kind: arg.Kind, Name: "-" + arg.Text, Text: "-" + arg.Text, Offset: int(n.Start)}
		}
	case "true", "false", "none", "ellipsis":
		return facts.RouteExpr{Kind: n.Kind, Name: n.Text, Text: n.Text, Offset: int(n.Start)}
	case "call":
		e := facts.RouteExpr{Kind: "call", Text: compactText(n.Text), Offset: int(n.Start)}
		o := shapeExpr(n.Fields["function"], depth+1)
		e.Object = &o
		span := facts.Span(n)
		e.Span = &span
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				switch a.Kind {
				case "comment":
				case "keyword_argument":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Name: facts.Text(a.Fields["name"]), Value: shapeExpr(a.Fields["value"], depth+1)})
				case "dictionary_splat":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Value: facts.RouteExpr{Kind: a.Kind, Text: compactText(a.Text)}})
				default:
					e.Args = append(e.Args, shapeExpr(a, depth+1))
				}
			}
		}
		return e
	case "subscript":
		e := facts.RouteExpr{Kind: "subscript", Text: compactText(n.Text), Offset: int(n.Start)}
		o := shapeExpr(n.Fields["value"], depth+1)
		e.Object = &o
		for _, c := range n.Children[1:] {
			if c.Kind != "comment" {
				e.Args = append(e.Args, shapeExpr(c, depth+1))
			}
		}
		return e
	case "generic_type":
		if len(n.Children) == 0 {
			break
		}
		e := facts.RouteExpr{Kind: "subscript", Text: compactText(n.Text), Offset: int(n.Start)}
		o := shapeExpr(n.Children[0], depth+1)
		e.Object = &o
		for _, c := range n.Children[1:] {
			if c.Kind != "type_parameter" {
				continue
			}
			for _, p := range c.Children {
				if p.Kind != "comment" {
					e.Args = append(e.Args, shapeExpr(p, depth+1))
				}
			}
		}
		return e
	case "binary_operator", "union_type":
		if n.Kind == "binary_operator" && !unionOperator(n) {
			break
		}
		e := facts.RouteExpr{Kind: "union", Text: compactText(n.Text), Offset: int(n.Start)}
		var flatten func(*parse.Node)
		flatten = func(c *parse.Node) {
			for c != nil && (c.Kind == "type" || c.Kind == "parenthesized_expression") && len(namedChildren(c)) == 1 {
				c = namedChildren(c)[0]
			}
			if c != nil && (c.Kind == "union_type" || c.Kind == "binary_operator" && unionOperator(c)) {
				for _, o := range c.Children {
					if o.Kind != "comment" {
						flatten(o)
					}
				}
				return
			}
			e.Args = append(e.Args, shapeExpr(c, depth+1))
		}
		flatten(n)
		return e
	case "list", "tuple", "expression_list", "set":
		e := facts.RouteExpr{Kind: n.Kind, Offset: int(n.Start)}
		if n.Kind == "expression_list" {
			e.Kind = "tuple"
		}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Args = append(e.Args, shapeExpr(c, depth+1))
			}
		}
		return e
	case "dictionary":
		e := facts.RouteExpr{Kind: "dict", Offset: int(n.Start)}
		for _, c := range n.Children {
			switch c.Kind {
			case "comment":
			case "pair":
				e.Parts = append(e.Parts, shapeExpr(c.Fields["key"], depth+1))
				e.Args = append(e.Args, shapeExpr(c.Fields["value"], depth+1))
			default:
				// A ** splat: the dictionary may hold more keys.
				e.Invalid = true
			}
		}
		return e
	}
	return facts.RouteExpr{Kind: "other", Text: compactText(n.Text), Offset: int(n.Start)}
}

func namedChildren(n *parse.Node) []*parse.Node {
	var out []*parse.Node
	for _, c := range n.Children {
		if c.Kind != "comment" {
			out = append(out, c)
		}
	}
	return out
}

// shapeCapture accumulates one file's shape payload during the walk.
// Functions and classes are captured only in gate SG files (gated);
// handler registrations in every file.
type shapeCapture struct {
	p     *facts.PythonFacts
	out   facts.PythonShape
	gated bool
}

func newShapeCapture(p *facts.PythonFacts, imports []facts.Import) *shapeCapture {
	return &shapeCapture{p: p, out: facts.PythonShape{Version: facts.PythonShapeVersion}, gated: shapeGate(imports)}
}

func (c *shapeCapture) finish() *facts.PythonShape {
	if len(c.out.Functions)+len(c.out.Classes)+len(c.out.Handlers) == 0 {
		return nil
	}
	return &c.out
}

// call records an exception handler registration (rows SH-F37):
// `x.exception_handler(K)`, `x.add_exception_handler(K, h)` and the keys
// of an `exception_handlers=` keyword on any call. A key that is not read
// is recorded without Key (it may be any class).
func (c *shapeCapture) call(n *parse.Node, scope int) {
	line := int(n.Row) + 1
	if fn := n.Fields["function"]; fn != nil && fn.Kind == "attribute" {
		switch facts.Text(fn.Fields["attribute"]) {
		case "exception_handler", "add_exception_handler":
			h := facts.PythonShapeHandler{Kind: "exception_handler", Line: line, Scope: scope}
			for i, a := range callArgs(n) {
				if a.Kind == "list_splat" || a.Kind == "dictionary_splat" {
					break
				}
				if a.Kind == "keyword_argument" {
					if facts.Text(a.Fields["name"]) == "exc_class_or_status_code" {
						e := shapeExpr(a.Fields["value"], 0)
						h.Key = &e
					}
					continue
				}
				if i == 0 {
					e := shapeExpr(a, 0)
					h.Key = &e
				}
			}
			c.out.Handlers = append(c.out.Handlers, h)
		}
	}
	for _, a := range callArgs(n) {
		if a.Kind != "keyword_argument" || facts.Text(a.Fields["name"]) != "exception_handlers" {
			continue
		}
		v := shapeExpr(a.Fields["value"], 0)
		if v.Kind != "dict" || v.Invalid || len(v.Parts) == 0 {
			if v.Kind != "dict" || v.Invalid {
				c.out.Handlers = append(c.out.Handlers, facts.PythonShapeHandler{Kind: "exception_handler", Line: line, Scope: scope})
			}
			continue
		}
		for i := range v.Parts {
			k := v.Parts[i]
			c.out.Handlers = append(c.out.Handlers, facts.PythonShapeHandler{Kind: "exception_handler", Line: line, Scope: scope, Key: &k})
		}
	}
}

// assign records an `x.route_class = ...` assignment (row SH-F38) or an
// `x.default_response_class = ...` assignment (row SH-F24): a router's
// route class or default response class set after construction.
func (c *shapeCapture) assign(n *parse.Node, scope int) {
	left := n.Fields["left"]
	if left == nil || left.Kind != "attribute" {
		return
	}
	switch name := facts.Text(left.Fields["attribute"]); name {
	case "route_class", "default_response_class":
		c.out.Handlers = append(c.out.Handlers, facts.PythonShapeHandler{Kind: name, Line: int(n.Row) + 1, Scope: scope})
	}
}

// class captures the members of a class with bases (a candidate model).
func (c *shapeCapture) class(n *parse.Node, scope int) {
	if !c.gated {
		return
	}
	supers := n.Fields["superclasses"]
	if supers == nil || len(namedChildren(supers)) == 0 {
		return
	}
	cls := facts.PythonShapeClass{Scope: scope, Line: int(n.Row) + 1}
	if body := n.Fields["body"]; body != nil {
		for _, s := range body.Children {
			if m, ok := shapeMember(s); ok {
				cls.Members = append(cls.Members, m)
			}
		}
	}
	c.out.Classes = append(c.out.Classes, cls)
}

// shapeMember reduces one class-body statement: annotated fields with
// their value (a Field call keeps every keyword), model_config, a nested
// Config class, methods by name; everything else is kind other.
func shapeMember(s *parse.Node) (facts.PythonModelMember, bool) {
	line := int(s.Row) + 1
	other := facts.PythonModelMember{Kind: "other", Line: line, Detail: s.Kind}
	switch s.Kind {
	case "comment", "pass_statement":
		return facts.PythonModelMember{}, false
	case "function_definition":
		return facts.PythonModelMember{Kind: "method", Name: facts.Text(s.Fields["name"]), Line: line}, true
	case "decorated_definition":
		def := s.Fields["definition"]
		if def != nil && def.Kind == "function_definition" {
			m := facts.PythonModelMember{Kind: "method", Name: facts.Text(def.Fields["name"]), Line: line}
			for _, d := range s.Children {
				if d.Kind == "decorator" {
					m.Decorators = append(m.Decorators, shapeDecorator(d))
				}
			}
			return m, true
		}
		return other, true
	case "class_definition":
		if facts.Text(s.Fields["name"]) == "Config" {
			return nestedClass(s), true
		}
		return facts.PythonModelMember{Kind: "class", Name: facts.Text(s.Fields["name"]), Line: line}, true
	case "expression_statement":
		parts := namedChildren(s)
		if len(parts) != 1 {
			return other, true
		}
		switch parts[0].Kind {
		case "string", "concatenated_string", "ellipsis":
			return facts.PythonModelMember{}, false
		case "assignment":
			a := parts[0]
			left, right, annotation := a.Fields["left"], a.Fields["right"], a.Fields["type"]
			if left == nil || left.Kind != "identifier" || right != nil && right.Kind == "assignment" {
				return other, true
			}
			m := facts.PythonModelMember{Kind: "assign", Name: identifier(left.Text), Line: line}
			if annotation != nil {
				m.Kind = "annotated"
				ann := reduceModel(annotation, 1, modelAnnotationDepth)
				ann.Text = facts.CompactPython(annotation.Text)
				m.Annotation = &ann
			}
			if right != nil && (annotation != nil || m.Name == "model_config") {
				v := shapeValue(right)
				m.Value = &v
			} else if right != nil {
				// A plain assignment keeps a literal value (an enum member).
				if v := reduceModel(right, 1, modelValueDepth); v.Kind == "literal" {
					m.Value = &v
				}
			}
			return m, true
		}
	}
	return other, true
}

// shapeDecorator reduces a method decorator: a call keeps its keywords
// (field_validator(..., mode="before"), validator(..., pre=True)), any
// other form its dotted name.
func shapeDecorator(d *parse.Node) facts.PythonModelExpr {
	for _, c := range d.Children {
		if c.Kind == "comment" {
			continue
		}
		if r := unwrapModel(c); r != nil && r.Kind == "call" {
			return shapeValue(r)
		}
		return nameOnly(c)
	}
	return facts.PythonModelExpr{Kind: "other"}
}

// shapeValue reduces a field default: a call keeps every keyword's value
// (Field(alias=...), Field(default=...)), anything else as reduceModel.
func shapeValue(n *parse.Node) facts.PythonModelExpr {
	r := unwrapModel(n)
	if r == nil || r.Kind != "call" {
		return reduceModel(n, 1, modelValueDepth)
	}
	e := facts.PythonModelExpr{Kind: "call", Name: dottedModel(r.Fields["function"])}
	for _, a := range callArgs(r) {
		switch a.Kind {
		case "keyword_argument":
			e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Name: facts.Text(a.Fields["name"]), Value: reduceModel(a.Fields["value"], 2, modelValueDepth)})
		case "dictionary_splat":
			e.Keywords = append(e.Keywords, facts.PythonModelKeyword{Value: facts.PythonModelExpr{Kind: "other"}})
		case "list_splat":
			e.Args = append(e.Args, facts.PythonModelExpr{Kind: "other"})
		default:
			e.Args = append(e.Args, reduceModel(a, 2, modelValueDepth))
		}
	}
	return e
}

// function captures one function's signature and shape sites. It runs
// after the body walk, so the scope's local bindings are known.
func (c *shapeCapture) function(n *parse.Node, scope int) {
	if !c.gated {
		return
	}
	fn := facts.PythonShapeFunction{Scope: scope, Line: int(n.Row) + 1}
	if ret := n.Fields["return_type"]; ret != nil {
		e := shapeExpr(ret, 0)
		fn.Returns = &e
	}
	params := map[string]bool{}
	watched := map[string]bool{}
	if ps := n.Fields["parameters"]; ps != nil {
		kind := ""
		for _, p := range ps.Children {
			text := strings.TrimSpace(p.Text)
			switch {
			case p.Kind == "comment":
				continue
			case text == "/":
				for i := range fn.Params {
					fn.Params[i].Kind = "positional"
				}
				continue
			case text == "*" || p.Kind == "keyword_separator":
				kind = "keyword"
				continue
			}
			param := facts.PythonShapeParam{Kind: kind, Line: int(p.Row) + 1}
			nameNode := p
			if p.Kind != "identifier" {
				nameNode = p.Fields["name"]
				if nameNode == nil && len(p.Children) > 0 {
					nameNode = p.Children[0]
				}
			}
			switch {
			case strings.HasPrefix(text, "**"):
				param.Kind = "kwargs"
				kind = "keyword"
			case strings.HasPrefix(text, "*"):
				param.Kind = "args"
				kind = "keyword"
			}
			for nameNode != nil && nameNode.Kind != "identifier" && len(nameNode.Children) > 0 {
				nameNode = nameNode.Children[0]
			}
			if nameNode == nil || nameNode.Kind != "identifier" {
				continue
			}
			param.Name = identifier(nameNode.Text)
			if typ := p.Fields["type"]; typ != nil {
				a := shapeExpr(typ, 0)
				param.Annotation = &a
			}
			if value := p.Fields["value"]; value != nil {
				param.HasDefault = true
				d := shapeExpr(value, 0)
				param.Default = &d
			}
			fn.Params = append(fn.Params, param)
			params[param.Name] = true
			if param.Kind != "args" && param.Kind != "kwargs" && (param.Annotation == nil || watchedTypes[lastName(*param.Annotation)]) {
				watched[param.Name] = true
			}
		}
	}
	b := &shapeBody{c: c, scope: scope, params: params, watched: watched, used: map[string]bool{}, consumed: map[uint32]bool{}}
	b.walk(n.Fields["body"], false)
	fn.Generator = b.yielded
	// Keep an assignment when its name is returned or when it passes a
	// watched parameter on (its reads must be followed).
	returned := map[string]bool{}
	for _, s := range b.sites {
		if s.Kind == "return" && s.Name != "" {
			returned[s.Name] = true
		}
	}
	for _, s := range b.sites {
		if s.Kind == "assign" && !returned[s.Name] {
			if len(s.Args) == 0 {
				continue
			}
			s.Kind = "call"
		}
		fn.Sites = append(fn.Sites, s)
	}
	c.out.Functions = append(c.out.Functions, fn)
}

// lastName is the last dotted segment of an annotation's base name
// (Request for starlette.requests.Request, dict for dict[str, str]).
func lastName(e facts.RouteExpr) string {
	switch e.Kind {
	case "identifier", "attribute":
		return e.Name
	case "subscript":
		if e.Object != nil {
			return lastName(*e.Object)
		}
	case "union":
		// Request | None watches the parameter; a union of other types
		// does not.
		for _, a := range e.Args {
			if n := lastName(a); watchedTypes[n] {
				return n
			}
		}
	}
	return ""
}

// shapeBody walks one function body (not nested definitions).
type shapeBody struct {
	c        *shapeCapture
	scope    int
	params   map[string]bool
	watched  map[string]bool
	used     map[string]bool
	consumed map[uint32]bool
	sites    []facts.PythonShapeSite
	// yielded is set once a yield has been walked (source order).
	yielded bool
}

func unwrapValue(n *parse.Node) *parse.Node {
	for n != nil && (n.Kind == "parenthesized_expression" || n.Kind == "await") && len(namedChildren(n)) == 1 {
		n = namedChildren(n)[0]
	}
	return n
}

// chain splits an attribute chain rooted at a watched parameter:
// request.headers.get -> ("request", [headers, get]).
func (b *shapeBody) chain(n *parse.Node) (string, []string, bool) {
	n = unwrapValue(n)
	switch {
	case n == nil:
		return "", nil, false
	case n.Kind == "identifier":
		name := identifier(n.Text)
		return name, nil, b.watched[name]
	case n.Kind == "attribute":
		root, attrs, ok := b.chain(n.Fields["object"])
		if !ok {
			return "", nil, false
		}
		return root, append(attrs, facts.Text(n.Fields["attribute"])), true
	}
	return "", nil, false
}

// candidate reports whether a call's callee may be a repository function:
// a dotted name whose root is not a local value or a builtin.
func (b *shapeBody) candidate(fn *parse.Node) (string, bool) {
	name := dottedModel(fn)
	if name == "" {
		return "", false
	}
	root := strings.SplitN(name, ".", 2)[0]
	if shapeBuiltins[root] || b.params[root] {
		return "", false
	}
	if local, ok := b.c.p.Scopes[b.scope].Bindings[root]; ok && local.Import == nil && local.Value.Kind != "function_definition" && local.Value.Kind != "class_definition" {
		return "", false
	}
	return name, true
}

func (b *shapeBody) use(name string, line int) {
	if b.used[name] {
		return
	}
	b.used[name] = true
	b.sites = append(b.sites, facts.PythonShapeSite{Kind: "use", Line: line, Name: name})
}

// keyOf reads a literal key argument.
func keyOf(n *parse.Node) (*string, bool) {
	if n == nil {
		return nil, true
	}
	if s, ok := literal(unwrapValue(n)); ok {
		return &s, false
	}
	return nil, true
}

// callSite builds a call site's callee expression and the arguments that
// pass watched parameters on; the arguments it consumes are marked.
func (b *shapeBody) callSite(n *parse.Node, kind string, try bool) facts.PythonShapeSite {
	callee := shapeExpr(n.Fields["function"], 0)
	expr := facts.RouteExpr{Kind: "call", Object: &callee, Offset: int(n.Start), Text: compactText(n.Text)}
	span := facts.Span(n)
	expr.Span = &span
	site := facts.PythonShapeSite{Kind: kind, Line: int(n.Row) + 1, Try: try, Expr: &expr}
	index := 0
	for _, a := range callArgs(n) {
		value, keyword, i := a, "", index
		switch a.Kind {
		case "keyword_argument":
			value, keyword, i = a.Fields["value"], facts.Text(a.Fields["name"]), -1
		case "list_splat", "dictionary_splat":
			// Arguments after a splat cannot be bound by position.
			expr.Invalid = true
			continue
		default:
			index++
		}
		root, attrs, ok := b.chain(value)
		if ok && len(attrs) <= 1 {
			arg := facts.PythonShapeArg{Index: i, Keyword: keyword, Param: root}
			if len(attrs) == 1 {
				arg.Attr = attrs[0]
			}
			site.Args = append(site.Args, arg)
			b.consumed[unwrapValue(value).Start] = true
		}
	}
	return site
}

func (b *shapeBody) walk(n *parse.Node, try bool) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "function_definition", "class_definition", "decorated_definition", "lambda":
		return
	case "try_statement":
		for _, c := range n.Children {
			b.walk(c, try || c == n.Fields["body"])
		}
		return
	case "yield":
		for _, c := range n.Children {
			b.walk(c, try)
		}
		b.yielded = true
		return
	case "raise_statement":
		if v := unwrapValue(firstNamed(n)); v != nil && v.Kind == "call" {
			site := facts.PythonShapeSite{Kind: "raise", Line: int(n.Row) + 1, Try: try, AfterYield: b.yielded}
			e := shapeExpr(v, 0)
			site.Expr = &e
			b.sites = append(b.sites, site)
		}
	case "return_statement":
		v := unwrapValue(firstNamed(n))
		site := facts.PythonShapeSite{Kind: "return", Line: int(n.Row) + 1, Try: try}
		_, candidate := "", false
		if v != nil && v.Kind == "call" {
			_, candidate = b.candidate(v.Fields["function"])
		}
		switch {
		case candidate:
			site = b.callSite(v, "return", try)
			b.consumed[v.Start] = true
		case v != nil && v.Kind == "identifier" && !b.watched[identifier(v.Text)]:
			site.Name = identifier(v.Text)
		case v == nil || b.plain(v):
			site.Plain = true
		}
		b.sites = append(b.sites, site)
	case "assignment", "augmented_assignment":
		left, right := n.Fields["left"], unwrapValue(n.Fields["right"])
		if n.Kind == "assignment" && left != nil && left.Kind == "identifier" && n.Fields["type"] == nil && right != nil && right.Kind == "call" {
			if _, ok := b.candidate(right.Fields["function"]); ok {
				site := b.callSite(right, "assign", try)
				site.Name = identifier(left.Text)
				b.sites = append(b.sites, site)
				b.consumed[right.Start] = true
			}
		} else if n.Kind == "assignment" && left != nil && left.Kind == "identifier" && right != nil && b.plain(right) {
			b.sites = append(b.sites, facts.PythonShapeSite{Kind: "assign", Line: int(n.Row) + 1, Try: try, Plain: true, Name: identifier(left.Text)})
		}
		if left != nil && left.Kind == "identifier" && b.watched[identifier(left.Text)] {
			// A watched parameter reassigned: the walk of the left side
			// records it as a use (an escape); later reads are of another
			// value and are not recorded (SH-F17).
			for _, c := range n.Children {
				b.walk(c, try)
			}
			delete(b.watched, identifier(left.Text))
			return
		}
	case "keyword_argument":
		b.walk(n.Fields["value"], try)
		return
	case "call":
		fn := n.Fields["function"]
		if root, attrs, ok := b.chain(fn); ok && len(attrs) > 0 && !b.consumed[n.Start] {
			args := callArgs(n)
			var first *parse.Node
			if len(args) > 0 && args[0].Kind != "keyword_argument" && args[0].Kind != "list_splat" && args[0].Kind != "dictionary_splat" {
				first = args[0]
			}
			site := facts.PythonShapeSite{Kind: "read", Line: int(n.Row) + 1, Name: root, Attrs: attrs[:len(attrs)-1], Method: attrs[len(attrs)-1]}
			if first != nil {
				site.Key, site.Dynamic = keyOf(first)
			}
			b.sites = append(b.sites, site)
			for _, a := range args {
				b.walk(a, try)
			}
			return
		}
		if _, ok := b.candidate(fn); ok && !b.consumed[n.Start] {
			site := b.callSite(n, "call", try)
			if len(site.Args) > 0 {
				b.sites = append(b.sites, site)
			}
		}
		for _, a := range callArgs(n) {
			b.walk(a, try)
		}
		if fn != nil && fn.Kind != "identifier" {
			b.walk(fn, try)
		}
		return
	case "subscript":
		if root, attrs, ok := b.chain(n.Fields["value"]); ok {
			site := facts.PythonShapeSite{Kind: "read", Line: int(n.Row) + 1, Name: root, Attrs: attrs}
			var index *parse.Node
			for _, c := range n.Children[1:] {
				if c.Kind != "comment" {
					index = c
					break
				}
			}
			site.Key, site.Dynamic = keyOf(index)
			b.sites = append(b.sites, site)
			for _, c := range n.Children[1:] {
				b.walk(c, try)
			}
			return
		}
	case "attribute":
		if root, attrs, ok := b.chain(n); ok {
			if !b.consumed[n.Start] && !harmlessAttribute[attrs[0]] {
				b.use(root, int(n.Row)+1)
			}
			return
		}
		b.walk(n.Fields["object"], try)
		return
	case "identifier":
		if name := identifier(n.Text); b.watched[name] && !b.consumed[n.Start] {
			b.use(name, int(n.Row)+1)
		}
		return
	}
	for _, c := range n.Children {
		b.walk(c, try)
	}
}

// harmlessAttribute lists request attributes that read no parameter, header,
// cookie or body (Starlette's Request): reading them is not a use. url
// (query string, path) and state (filled from the request by middleware
// and dependencies) are not harmless.
var harmlessAttribute = setOf("base_url", "client", "app", "method", "url_for")

// plainBuiltins are builtin conversions whose result is never a Response.
var plainBuiltins = setOf("dict", "list", "str", "int", "float", "bool", "len", "tuple", "set", "frozenset", "sorted", "repr",
	"sum", "abs", "round", "format", "bytes")

// plain reports a value that is never a Response object (SH-F24): a
// literal, a container or comprehension, a comparison, arithmetic, a
// builtin conversion, or a conditional of such values.
func (b *shapeBody) plain(n *parse.Node) bool {
	n = unwrapValue(n)
	if n == nil {
		return false
	}
	switch n.Kind {
	case "string", "concatenated_string", "integer", "float", "true", "false", "none", "dictionary", "list", "tuple", "set",
		"list_comprehension", "dictionary_comprehension", "set_comprehension", "comparison_operator", "not_operator",
		"unary_operator", "binary_operator":
		return true
	case "conditional_expression":
		var parts []*parse.Node
		for _, c := range n.Children {
			if c.Kind != "comment" && c.Kind != "if" && c.Kind != "else" && strings.TrimSpace(c.Text) != "if" && strings.TrimSpace(c.Text) != "else" {
				parts = append(parts, c)
			}
		}
		// value if condition else other: the condition need not be plain.
		return len(parts) == 3 && b.plain(parts[0]) && b.plain(parts[2])
	case "call":
		fn := n.Fields["function"]
		if fn == nil || fn.Kind != "identifier" {
			return false
		}
		name := identifier(fn.Text)
		if !plainBuiltins[name] || b.params[name] {
			return false
		}
		_, local := b.c.p.Scopes[b.scope].Bindings[name]
		_, global := b.c.p.Scopes[0].Bindings[name]
		return !local && !global
	}
	return false
}

// shapeKeywords are the route decorator keywords whose values the shape
// linker reads in full.
var shapeKeywords = setOf("response_model", "responses", "status_code", "response_class", "include_in_schema",
	"response_model_include", "response_model_exclude", "response_model_exclude_none", "response_model_by_alias",
	"response_model_exclude_unset", "response_model_exclude_defaults")

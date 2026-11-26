package python

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// literal decodes Python strings, including implicit concatenation and constant
// f-strings. Bytes and interpolation are deliberately not string literals.
func literal(n *parse.Node) (string, bool) {
	if n == nil {
		return "", false
	}
	if n.Kind == "concatenated_string" {
		var b strings.Builder
		for _, c := range n.Children {
			if c.Kind == "comment" {
				continue
			}
			s, ok := literal(c)
			if !ok {
				return "", false
			}
			b.WriteString(s)
		}
		return b.String(), true
	}
	if n.Kind != "string" {
		return "", false
	}
	for _, c := range n.Children {
		if c.Kind == "interpolation" {
			return "", false
		}
	}
	s := n.Text
	i := strings.IndexAny(s, "\"'")
	if i < 0 {
		return "", false
	}
	prefix := strings.ToLower(s[:i])
	if strings.Contains(prefix, "b") {
		return "", false
	}
	quote := 1
	if len(s) >= i+6 && s[i:i+3] == strings.Repeat(s[i:i+1], 3) {
		quote = 3
	}
	if len(s) < i+2*quote {
		return "", false
	}
	s = s[i+quote : len(s)-quote]
	if !strings.Contains(prefix, "r") {
		var b strings.Builder
		for j := 0; j < len(s); j++ {
			if s[j] != '\\' {
				b.WriteByte(s[j])
				continue
			}
			j++
			if j >= len(s) {
				return "", false
			}
			switch s[j] {
			case '\n':
			case '\r':
				if j+1 < len(s) && s[j+1] == '\n' {
					j++
				}
			case '\\', '\'', '"':
				b.WriteByte(s[j])
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'v':
				b.WriteByte('\v')
			case 'x', 'u', 'U':
				length := 2
				if s[j] == 'u' {
					length = 4
				}
				if s[j] == 'U' {
					length = 8
				}
				if j+length >= len(s) {
					return "", false
				}
				v, err := strconv.ParseUint(s[j+1:j+1+length], 16, 32)
				if err != nil || !utf8.ValidRune(rune(v)) {
					return "", false
				}
				b.WriteRune(rune(v))
				j += length
			case 'N':
				return "", false // Unicode name tables are not a dependency.
			default:
				if s[j] >= '0' && s[j] <= '7' {
					end := j + 1
					for end < len(s) && end < j+3 && s[end] >= '0' && s[end] <= '7' {
						end++
					}
					v, _ := strconv.ParseUint(s[j:end], 8, 32)
					b.WriteRune(rune(v))
					j = end - 1
				} else {
					b.WriteByte('\\')
					b.WriteByte(s[j])
				}
			}
		}
		s = b.String()
	}
	if strings.Contains(prefix, "f") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "{{", "{"), "}}", "}")
	}
	return s, true
}

func expression(n *parse.Node) facts.RouteExpr {
	if n == nil {
		return facts.RouteExpr{}
	}
	e := facts.RouteExpr{Kind: n.Kind, Text: n.Text, Offset: int(n.Start)}
	if s, ok := literal(n); ok {
		e.Kind = "literal"
		e.Name = s
		return e
	}
	switch n.Kind {
	case "identifier":
		e.Name = n.Text
	case "attribute":
		o := expression(n.Fields["object"])
		e.Object = &o
		e.Name = facts.Text(n.Fields["attribute"])
	case "call":
		o := expression(n.Fields["function"])
		e.Object = &o
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				switch a.Kind {
				case "comment":
				case "keyword_argument":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Name: facts.Text(a.Fields["name"]), Value: expression(a.Fields["value"])})
				case "dictionary_splat":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Value: expression(a)})
				default:
					e.Args = append(e.Args, expression(a))
				}
			}
		}
	case "list", "tuple", "set":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Args = append(e.Args, expression(c))
			}
		}
	case "parenthesized_expression":
		if len(n.Children) == 1 {
			return expression(n.Children[0])
		}
	}
	return e
}

// bindingExpression keeps the syntax used by resolution, without retaining
// arbitrary constructor payloads (for example model data or dependency lists).
// Settings needs positional presence and keyword names; routers also need prefix.
func bindingExpression(n *parse.Node) facts.RouteExpr {
	if n == nil {
		return facts.RouteExpr{}
	}
	if n.Kind == "parenthesized_expression" && len(n.Children) == 1 {
		return bindingExpression(n.Children[0])
	}
	if n.Kind != "call" {
		switch n.Kind {
		case "identifier", "attribute", "string", "concatenated_string":
			return expression(n)
		}
		return facts.RouteExpr{Kind: n.Kind, Offset: int(n.Start)}
	}
	e := facts.RouteExpr{Kind: "call", Offset: int(n.Start)}
	span := facts.Span(n)
	e.Span = &span
	o := bindingExpression(n.Fields["function"])
	e.Object = &o
	if args := n.Fields["arguments"]; args != nil {
		for _, a := range args.Children {
			switch a.Kind {
			case "comment":
			case "keyword_argument":
				name := facts.Text(a.Fields["name"])
				value := facts.RouteExpr{}
				switch name {
				case "prefix":
					value = expression(a.Fields["value"])
				case "dependencies", "auto_error", "scheme_name":
					// Router/app dependency lists and security-scheme
					// settings (T026 AG-B4a, docs/extract/auth.md).
					value = dependencyExpr(a.Fields["value"], 0)
				case "tokenUrl", "bearerFormat", "openIdConnectUrl", "authorizationUrl", "scopes", "default_response_class", "route_class":
					// Security scheme details, and the response and route
					// classes of FastAPI and APIRouter objects (T029 SH-F).
					value = shapeExpr(a.Fields["value"], 0)
				case "name":
					// An APIKeyHeader/Query/Cookie key name (T029 SH-F).
					if strings.HasPrefix(lastSegment(dottedModel(n.Fields["function"])), "APIKey") {
						value = shapeExpr(a.Fields["value"], 0)
					}
				}
				e.Keywords = append(e.Keywords, facts.RouteKeyword{Name: name, Value: value})
			case "dictionary_splat":
				e.Keywords = append(e.Keywords, facts.RouteKeyword{Value: facts.RouteExpr{Kind: a.Kind, Text: a.Text}})
			default:
				arg := facts.RouteExpr{Kind: a.Kind}
				if len(e.Args) == 0 && strings.HasPrefix(lastSegment(dottedModel(n.Fields["function"])), "OAuth2") {
					// OAuth2PasswordBearer's positional tokenUrl (T029 SH-F).
					arg = shapeExpr(a, 0)
				}
				e.Args = append(e.Args, arg)
			}
		}
	}
	return e
}

func extractSyntax(file string, source []byte, root *parse.Node, out *facts.Facts) {
	p := &facts.PythonFacts{Scopes: []facts.PythonScope{{Parent: -1, Bindings: map[string]facts.PythonBinding{}}}}
	// routesLists maps a Mount(...) call in a routes= list literal to the
	// call that list is passed to (markRoutesList).
	routesLists := map[uint32]uint32{}
	// Gate DG (docs/extract/auth.md): only files importing fastapi keep
	// dependency parameters and Annotated aliases.
	p.FastAPIDependencies = importsFastAPI(out.Imports)
	newScope := func(parent int, name string, class bool) int {
		p.Scopes = append(p.Scopes, facts.PythonScope{Parent: parent, Name: name, Class: class, Bindings: map[string]facts.PythonBinding{}})
		return len(p.Scopes) - 1
	}
	// tries holds the byte ranges of try bodies: a binding there is
	// guarded (an earlier failure in the body skips it).
	var tries [][2]uint32
	guardedAt := func(pos uint32) bool {
		for _, t := range tries {
			if t[0] <= pos && pos < t[1] {
				return true
			}
		}
		return false
	}
	bind := func(scope int, name string, b facts.PythonBinding) {
		old := p.Scopes[scope].Bindings[name]
		b.Count = old.Count + 1
		b.Guarded = b.Guarded || old.Guarded || !b.Stable
		b.Stable = b.Stable && b.Count == 1
		p.Scopes[scope].Bindings[name] = b
	}
	ownerFor := func(n *parse.Node) (string, string) {
		if n == nil {
			return "", ""
		}
		var closest *contract.Node
		endRow, endColumn, _ := declEnd(n, source)
		for i := range out.Nodes {
			symbol := &out.Nodes[i]
			matchingKind := n.Kind == "class_definition" && symbol.Kind == "class" || n.Kind == "function_definition" && (symbol.Kind == "function" || symbol.Kind == "method")
			if matchingKind && symbol.Name == facts.Text(n.Fields["name"]) && symbol.Span != nil && symbol.Span.EndLine == int(endRow)+1 && symbol.Span.EndColumn == int(endColumn) && symbol.Span.StartLine <= int(n.Row)+1 {
				if closest == nil || symbol.Span.StartLine > closest.Span.StartLine || symbol.Span.StartLine == closest.Span.StartLine && symbol.Span.StartColumn > closest.Span.StartColumn {
					closest = symbol
				}
			}
		}
		if closest != nil {
			return closest.Identity, closest.QualifiedName
		}
		return "", ""
	}
	invalid := map[string]bool{}
	globals := map[int]map[string]bool{}
	decorated := map[uint32]bool{}
	awaited := map[uint32]bool{}
	models := newModelCapture(file, out.Imports, &out.Diagnostics)
	// Gate SG: route shape syntax (T029 SH-F, docs/extract/route-shape.md);
	// handler registrations in every file.
	shape := newShapeCapture(p, out.Imports)
	queries := newQueryCapture(out.Imports)
	// constants maps a module-level assignment's span to the constant node
	// the extractor emitted for it: the owner of a query in its initialiser.
	constants := map[contract.Span]string{}
	for _, node := range out.Nodes {
		if node.Kind == "constant" && node.Span != nil {
			constants[*node.Span] = node.Identity
		}
	}
	classDecorators := map[uint32][]*parse.Node{}
	var walk func(*parse.Node, int, string, string, bool, modelCtx)
	walk = func(n *parse.Node, scope int, owner, ownerName string, conditional bool, mctx modelCtx) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "decorated_definition":
			def := n.Fields["definition"]
			if def != nil {
				decorated[def.Start] = true
			}
			id, _ := ownerFor(def)
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					if def != nil {
						classDecorators[def.Start] = append(classDecorators[def.Start], c)
					}
					for _, call := range c.Children {
						if call.Kind == "call" {
							recordRoute(p, file, source, call, scope, owner, ownerName, conditional, id, true)
							shape.call(call, scope)
							if models != nil {
								models.call(call, scope, mctx)
							}
						}
					}
				}
			}
			walk(def, scope, owner, ownerName, conditional, mctx)
			return
		case "function_definition", "class_definition":
			id, q := ownerFor(n)
			name := identifier(facts.Text(n.Fields["name"]))
			class := n.Kind == "class_definition"
			child := newScope(scope, q, class)
			if class && models != nil {
				models.class(n, classDecorators[n.Start], scope, id, mctx)
			}
			if class {
				shape.class(n, child)
			}
			b := facts.PythonBinding{Identity: id, Stable: !conditional, Guarded: guardedAt(n.Start), ClassScope: child, Value: facts.RouteExpr{Kind: n.Kind, Name: q}}
			if bases := n.Fields["superclasses"]; bases != nil {
				for _, c := range bases.Children {
					b.Bases = append(b.Bases, expression(c))
				}
			}
			bind(scope, name, b)
			if params := n.Fields["parameters"]; params != nil {
				for _, c := range params.Children {
					param := c
					if c.Kind != "identifier" {
						param = c.Fields["name"]
						if param == nil && len(c.Children) > 0 {
							param = c.Children[0]
						}
					}
					if param != nil && param.Kind == "identifier" {
						bind(child, identifier(param.Text), facts.PythonBinding{})
						if !class {
							p.Scopes[child].Parameters = append(p.Scopes[child].Parameters, identifier(param.Text))
							queries.parameter(child, c)
						}
					}
					if !class && p.FastAPIDependencies {
						if d, ok := dependencyParameter(c); ok {
							p.Scopes[child].Dependencies = append(p.Scopes[child].Dependencies, d)
						}
					}
				}
			}
			symbol := id
			if symbol == "" {
				symbol = mctx.owner
			}
			if class {
				walk(n.Fields["body"], child, owner, ownerName, false, modelCtx{cond: mctx.cond, owner: symbol})
			} else {
				walk(n.Fields["body"], child, id, q, false, modelCtx{cond: true, owner: symbol})
				shape.function(n, child)
				if scope == 0 && !decorated[n.Start] {
					p.Scopes[child].Helper = helperSignature(n, id)
					if h := p.Scopes[child].Helper; h != nil {
						for i := range h.Parameters {
							if p.Scopes[child].Bindings[h.Parameters[i].Name].Count > 1 {
								h.Parameters[i].Blocked = true
							}
						}
					}
				}
			}
			return
		case "try_statement":
			if body := n.Fields["body"]; body != nil {
				tries = append(tries, [2]uint32{body.Start, body.End})
			}
		case "import_statement", "import_from_statement":
			for _, imp := range out.Imports {
				if imp.Span.StartLine != int(n.Row)+1 || imp.Span.StartColumn != int(n.Column) {
					continue
				}
				for local, name := range imp.Bindings {
					copy := imp
					bind(scope, local, facts.PythonBinding{Import: &copy, Imported: name, Stable: !conditional, Guarded: guardedAt(n.Start)})
				}
			}
			return
		case "assignment":
			value := bindingExpression(n.Fields["right"])
			if p.FastAPIDependencies && annotatedAlias(n.Fields["right"]) {
				// A candidate `X = Annotated[T, Depends(f)]` alias.
				value = dependencyExpr(n.Fields["right"], 0)
			}
			for _, name := range bindingNames(n.Fields["left"]) {
				bind(scope, name, facts.PythonBinding{Value: value, Stable: !conditional, Guarded: guardedAt(n.Start)})
			}
			if models != nil {
				models.assign(n, scope)
			}
			shape.assign(n, scope)
		case "augmented_assignment", "named_expression", "for_statement", "for_in_clause":
			left := n.Fields["left"]
			if left == nil {
				left = n.Fields["name"]
			}
			for _, name := range bindingNames(left) {
				bind(scope, name, facts.PythonBinding{})
				invalid[name] = true
			}
		case "type_alias_statement":
			// PEP 695 `type X = ...` binds X in the enclosing scope (a
			// TypeAliasType value).
			// The name is the first identifier of the left `type` node
			// (`type X = ...`, generic `type X[T] = ...`).
			left := firstNamed(n)
			for left != nil && left.Kind != "identifier" {
				left = firstNamed(left)
			}
			if left != nil {
				bind(scope, identifier(left.Text), facts.PythonBinding{Value: facts.RouteExpr{Kind: n.Kind, Offset: int(n.Start)}, Stable: !conditional, Guarded: guardedAt(n.Start)})
			}
		case "as_pattern":
			// except E as X, with f() as X (and case P as X) bind X in the
			// enclosing scope: a later X there is that value, not the
			// class or import of the same name (R5-B2).
			for _, name := range helperTargetNames(n.Fields["alias"]) {
				bind(scope, name, facts.PythonBinding{})
			}
		case "global_statement":
			if globals[scope] == nil {
				globals[scope] = map[string]bool{}
			}
			for _, c := range n.Children {
				if c.Kind == "identifier" {
					globals[scope][identifier(c.Text)] = true
				}
			}
		case "call":
			before := len(p.Routes)
			recordRoute(p, file, source, n, scope, owner, ownerName, conditional, "", false)
			if len(p.Routes) > before && p.Routes[before].Call.Method == "Mount" {
				if at, ok := routesLists[n.Start]; ok {
					p.Routes[before].InRoutesList, p.Routes[before].RoutesCall = true, int(at)
				}
			}
			markRoutesList(n, routesLists)
			shape.call(n, scope)
			if models != nil {
				models.call(n, scope, mctx)
			}
			queries.call(n, scope, mctx.owner)
			callee := n.Fields["function"]
			if callee != nil && (callee.Kind == "identifier" || callee.Kind == "attribute") {
				e := helperCallExpression(n)
				if len(e.Args)+len(e.Keywords) > 0 {
					p.HelperCalls = append(p.HelperCalls, facts.PythonHelperCall{Expression: e, Scope: scope, Conditional: conditional, Awaited: awaited[n.Start]})
				}
			}
		case "await":
			for _, c := range n.Children {
				for c.Kind == "parenthesized_expression" && len(c.Children) == 1 {
					c = c.Children[0]
				}
				if c.Kind == "call" {
					awaited[c.Start] = true
				}
			}
		}
		for _, c := range n.Children {
			cond := conditional
			switch n.Kind {
			case "if_statement", "elif_clause":
				cond = conditional || c == n.Fields["consequence"] || c == n.Fields["alternative"] || c.Kind == "elif_clause" || c.Kind == "else_clause"
			case "for_statement", "while_statement":
				cond = conditional || c == n.Fields["body"] || c.Kind == "else_clause"
			case "else_clause", "case_clause", "except_clause", "except_group_clause", "conditional_expression":
				cond = true
			}
			child := mctx
			child.cond = child.cond || modelCompound[n.Kind]
			child.args = child.args || n.Kind == "call" && c == n.Fields["arguments"] || n.Kind == "assignment" && c == n.Fields["right"] && facts.Text(n.Fields["left"]) == "__table_args__"
			if n.Kind == "assignment" && scope == 0 && c == n.Fields["right"] && constants[facts.Span(n)] != "" {
				child.owner = constants[facts.Span(n)]
			}
			walk(c, scope, owner, ownerName, cond, child)
		}
	}
	walk(root, 0, facts.ID("file", file, file), "", false, modelCtx{owner: facts.ID("file", file, file)})
	for scope, names := range globals {
		for name := range names {
			if p.Scopes[scope].Bindings[name].Count > 0 {
				invalid[name] = true
			}
		}
	}
	for name := range invalid {
		b := p.Scopes[0].Bindings[name]
		b.Stable = false
		p.Scopes[0].Bindings[name] = b
	}
	if models != nil {
		p.Models = models.finish()
	}
	p.Shape = shape.finish()
	if found, annotations, ok := queries.finish(models != nil, p.Scopes); ok {
		if p.Models == nil {
			p.Models = &facts.PythonModels{Version: facts.PythonModelsVersion}
		}
		p.Models.Queries = found
		p.Models.Annotations = annotations
	}
	out.Python = p
}

// markRoutesList records, for every Mount(...) call written directly as
// an element of a list literal passed as routes= to the call n, the
// offset of n (T035). The walk visits n before its arguments, so the
// Mount calls find it when they are recorded.
func markRoutesList(n *parse.Node, lists map[uint32]uint32) {
	args := n.Fields["arguments"]
	if args == nil {
		return
	}
	for _, a := range args.Children {
		if a.Kind != "keyword_argument" || facts.Text(a.Fields["name"]) != "routes" {
			continue
		}
		list := a.Fields["value"]
		if list == nil || list.Kind != "list" {
			continue
		}
		for _, element := range list.Children {
			if element.Kind == "call" && element.Fields["function"] != nil && starletteMountCall(element.Fields["function"]) {
				lists[element.Start] = n.Start
			}
		}
	}
}

// starletteMountCall reports a call spelled Mount(...) or x.Mount(...):
// the syntax a Starlette routing.Mount may take. The routes linker proves
// the callee; the extractor only keeps the call.
func starletteMountCall(callee *parse.Node) bool {
	switch callee.Kind {
	case "identifier":
		return callee.Text == "Mount"
	case "attribute":
		return facts.Text(callee.Fields["attribute"]) == "Mount"
	}
	return false
}

func recordRoute(p *facts.PythonFacts, file string, source []byte, n *parse.Node, scope int, owner, ownerName string, conditional bool, handler string, decorator bool) {
	callee := n.Fields["function"]
	if callee == nil {
		return
	}
	if !decorator && starletteMountCall(callee) {
		// A Starlette Mount(...) call (T035): the routes linker reads its
		// app argument at link time. It has no receiver; the callee stays
		// in Expression.Object for the linker to resolve.
		e := expression(n)
		p.Routes = append(p.Routes, facts.PythonRoute{Call: facts.RouteCall{Method: "Mount", Args: e.Args, Scope: scope, Owner: owner, OwnerName: ownerName, Conditional: conditional, Span: facts.Span(n), Hash: contract.ContentHash(source[n.Start:n.End])}, Expression: e})
		return
	}
	if callee.Kind != "attribute" {
		return
	}
	method := facts.Text(callee.Fields["attribute"])
	if !strings.Contains(" get post put patch delete options head trace api_route add_api_route include_router mount websocket websocket_route ", " "+method+" ") {
		return
	}
	e := expression(n)
	if args := n.Fields["arguments"]; args != nil {
		i := 0
		for _, a := range args.Children {
			if a.Kind != "keyword_argument" {
				continue
			}
			// Keyword order equals e.Keywords' order (expression above).
			for i < len(e.Keywords) && e.Keywords[i].Name == "" {
				i++
			}
			if i < len(e.Keywords) && shapeKeywords[e.Keywords[i].Name] && facts.Text(a.Fields["name"]) == e.Keywords[i].Name {
				// Response shape keywords keep their reduced value (T029
				// SH-F): response_model=list[Item], responses={...}.
				e.Keywords[i].Value = shapeExpr(a.Fields["value"], 0)
			}
			if i < len(e.Keywords) && facts.Text(a.Fields["name"]) == "dependencies" && e.Keywords[i].Name == "dependencies" {
				// Route and include_router dependency lists keep their
				// Depends calls' spans (T026 AG-B4a).
				e.Keywords[i].Value = dependencyExpr(a.Fields["value"], 0)
			}
			i++
		}
	}
	p.Routes = append(p.Routes, facts.PythonRoute{Call: facts.RouteCall{Receiver: expression(callee.Fields["object"]), Method: method, Args: e.Args, Scope: scope, Owner: owner, OwnerName: ownerName, Conditional: conditional, Span: facts.Span(n), Hash: contract.ContentHash(source[n.Start:n.End])}, Handler: handler, Decorator: decorator})
	// Preserve keyword-only prefix, path, endpoint and methods in a detached call.
	p.Routes[len(p.Routes)-1].Expression = e
}

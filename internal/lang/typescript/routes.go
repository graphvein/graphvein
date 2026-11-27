package typescript

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// routeExpr is the route syntax of n, built without sharing (see exprMemo).
func routeExpr(n *parse.Node) facts.RouteExpr {
	return (*exprMemo)(nil).route(n)
}

func (m *exprMemo) buildRoute(n *parse.Node) facts.RouteExpr {
	if n == nil {
		return facts.RouteExpr{}
	}
	e := facts.RouteExpr{Kind: n.Kind, Text: n.Text, Offset: int(n.Start)}
	switch n.Kind {
	case "identifier", "property_identifier":
		e.Name = n.Text
	case "member_expression":
		o := m.route(n.Fields["object"])
		e.Object = &o
		e.Name = text(n.Fields["property"])
	case "call_expression":
		callee := n.Fields["function"]
		// The embedded TS grammar attaches await to a generic call's callee.
		// Keep this correction specific to client syntax below.
		o := m.route(callee)
		e.Object = &o
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					e.Args = append(e.Args, m.route(a))
				}
			}
		}
	case "new_expression":
		// The constructor and its arguments (T029 SH-S: a passport strategy
		// registered with passport.use(new Strategy({...}))).
		if c := n.Fields["constructor"]; c != nil {
			o := m.route(c)
			e.Object = &o
		}
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "comment" {
					e.Args = append(e.Args, m.route(a))
				}
			}
		}
	case "parenthesized_expression":
		if len(n.Children) == 1 {
			return m.route(n.Children[0])
		}
	case "await_expression":
		// The awaited expression, so a const bound to `await f()` keeps
		// the call (Supabase client wrappers, row SB-K1).
		for _, c := range n.Children {
			if c.Kind != "comment" {
				o := m.route(c)
				e.Object = &o
				break
			}
		}
	case "object":
		e.Properties = map[string]facts.RouteExpr{}
		for _, c := range n.Children {
			if c.Kind == "pair" {
				key := strings.Trim(text(c.Fields["key"]), "\"'")
				e.Properties[key] = m.route(c.Fields["value"])
			} else if c.Kind == "shorthand_property_identifier" {
				e.Properties[c.Text] = facts.RouteExpr{Kind: "identifier", Name: c.Text, Text: c.Text}
			} else if c.Kind == "spread_element" {
				e.Spread = true
			}
		}
	case "template_substitution":
		// A tagged template's arguments are its children, so substitutions
		// arrive here directly; keep the reduced inner expression.
		if v := substitutionValue(n); v != nil {
			o := m.route(v)
			e.Object = &o
		}
	case "binary_expression":
		e.Operator = text(n.Fields["operator"])
		if e.Operator == "" {
			for _, token := range n.Tokens {
				if token == "+" || token == "??" || token == "||" {
					e.Operator = token
				}
			}
		}
		e.Parts = []facts.RouteExpr{m.route(n.Fields["left"]), m.route(n.Fields["right"])}
	case "string", "template_string":
		literal := true
		for _, c := range n.Children {
			if c.Kind == "template_substitution" {
				literal = false
			}
		}
		if literal && len(n.Text) >= 2 {
			e.Kind = "literal"
			e.Name = n.Text[1 : len(n.Text)-1]
		} else if n.Kind == "template_string" {
			start := n.Start + 1
			for _, c := range n.Children {
				if c.Kind != "template_substitution" {
					continue
				}
				if c.Start > start {
					e.Parts = append(e.Parts, facts.RouteExpr{Kind: "literal", Name: n.Text[start-n.Start : c.Start-n.Start]})
				}
				for _, v := range c.Children {
					if v.Kind != "comment" {
						e.Parts = append(e.Parts, m.route(v))
						break
					}
				}
				start = c.End
			}
			if start < n.End-1 {
				e.Parts = append(e.Parts, facts.RouteExpr{Kind: "literal", Name: n.Text[start-n.Start : len(n.Text)-1]})
			}
		}
	}
	return e
}

// substitutionValue returns the expression inside a template substitution
// `${...}`, skipping comments, or nil when there is none.
func substitutionValue(n *parse.Node) *parse.Node {
	for _, c := range n.Children {
		if c.Kind != "comment" {
			return c
		}
	}
	return nil
}

func extractRoutes(file string, source []byte, root *parse.Node, out *Facts) {
	extractRoutesWith(memoFor(root), file, source, root, out)
}

// extractRoutesWith is extractRoutes with the file's expression memo (nil:
// no sharing; see exprMemo).
func extractRoutesWith(m *exprMemo, file string, source []byte, root *parse.Node, out *Facts) {
	r := &facts.RouteFacts{Scopes: []facts.RouteScope{{Parent: -1, Bindings: map[string]facts.RouteBinding{}}}, Exports: map[string]string{}, Parameters: map[string][]facts.ClientParameter{}, ParameterScopes: map[string]int{}}
	declarations := map[int]map[string]int{}
	ambient := map[uint32]bool{}
	var markAmbient func(*parse.Node, bool)
	markAmbient = func(n *parse.Node, declared bool) {
		if n == nil {
			return
		}
		declared = declared || n.Kind == "ambient_declaration"
		if declared {
			ambient[n.Start] = true
		}
		for _, c := range n.Children {
			markAmbient(c, declared)
		}
	}
	markAmbient(root, false)
	// Import syntax supplies local lexical bindings; the shared resolver traverses exports.
	for _, imp := range out.Imports {
		for local, name := range imp.Bindings {
			r.Scopes[0].Bindings[local] = facts.RouteBinding{Module: imp.Module, Export: name}
		}
	}
	newScope := func(parent int) int {
		r.Scopes = append(r.Scopes, facts.RouteScope{Parent: parent, Bindings: map[string]facts.RouteBinding{}})
		return len(r.Scopes) - 1
	}
	// Map syntax spans back to emitted owners, including declaration-order suffixes.
	ownerFor := func(n *parse.Node, fallback, fallbackName string) (string, string) {
		for _, symbol := range out.Nodes {
			if symbol.Kind != "function" && symbol.Kind != "method" {
				continue
			}
			s := symbol.Span
			if s.StartLine <= int(n.Row)+1 && s.EndLine == int(n.EndRow)+1 && s.EndColumn == int(n.EndColumn) {
				return symbol.Identity, symbol.QualifiedName
			}
		}
		return fallback, fallbackName
	}
	// Server action syntax (rows SA-I1, SA-C1 of
	// docs/extract/server-actions.md) needs the route scopes: inline
	// actions keep their own scope, attributes the scope they read in.
	actions := &facts.ActionFacts{}
	assigned := map[string]bool{}
	// elements maps a JSX attribute's start byte to the name of the
	// element it is written on (row SA-C1 reads form actions only on
	// intrinsic form elements and next/form's Form).
	elements := map[uint32]string{}
	constValues, boundValues := declaratorValues(root)
	closestOwner := func(location contract.Span, owner, ownerName string) (string, string) {
		var closest *contract.Span
		for _, symbol := range out.Nodes {
			s := symbol.Span
			starts := s.StartLine < location.StartLine || s.StartLine == location.StartLine && s.StartColumn <= location.StartColumn
			ends := s.EndLine > location.EndLine || s.EndLine == location.EndLine && s.EndColumn >= location.EndColumn
			if starts && ends && (closest == nil || s.StartLine > closest.StartLine || s.StartLine == closest.StartLine && s.StartColumn >= closest.StartColumn) {
				owner = symbol.Identity
				ownerName = symbol.QualifiedName
				closest = s
			}
		}
		return owner, ownerName
	}
	var walk func(*parse.Node, int, string, string, bool, bool)
	walk = func(n *parse.Node, scope int, owner, ownerName string, conditional, exported bool) {
		if n == nil {
			return
		}
		if n.Kind == "ternary_expression" {
			if leaves, complete := ternaryLeaves(n); len(leaves) > 0 {
				actions.Ternaries = append(actions.Ternaries, facts.ActionTernary{Offset: int(n.Start), Leaves: leaves, Complete: complete})
			}
		}
		if left := n.Fields["left"]; (n.Kind == "assignment_expression" || n.Kind == "augmented_assignment_expression") && left != nil && left.Kind == "identifier" {
			assigned[left.Text] = true
		}
		if arg := n.Fields["argument"]; n.Kind == "update_expression" && arg != nil && arg.Kind == "identifier" {
			assigned[arg.Text] = true
		}
		if n.Kind == "jsx_opening_element" || n.Kind == "jsx_self_closing_element" {
			for _, c := range n.Children {
				if c.Kind == "jsx_attribute" {
					elements[c.Start] = text(n.Fields["name"])
				}
			}
		}
		if n.Kind == "jsx_attribute" {
			if a, ok := actionAttribute(n); ok {
				a.Element = elements[n.Start]
				location := span(n)
				a.Scope = scope
				a.Owner, a.OwnerName = closestOwner(location, owner, ownerName)
				a.Span = location
				a.Hash = contract.ContentHash(source[n.Start:n.End])
				actions.Attributes = append(actions.Attributes, a)
			}
		}
		switch n.Kind {
		case "class_declaration", "abstract_class_declaration", "function_declaration", "generator_function_declaration", "enum_declaration", "internal_module", "module":
			if declarations[scope] == nil {
				declarations[scope] = map[string]int{}
			}
			declarations[scope][text(n.Fields["name"])]++
		}
		switch n.Kind {
		case "import_statement":
			// Retain type-only syntax for the additive static path without
			// changing the existing imported-binding consumers.
			var imports func(*parse.Node, bool)
			imports = func(c *parse.Node, typeOnly bool) {
				for _, token := range c.Tokens {
					typeOnly = typeOnly || token == "type"
				}
				if typeOnly && (c.Kind == "identifier" || c.Kind == "import_specifier") {
					name := c.Text
					if c.Kind == "import_specifier" {
						name = text(c.Fields["alias"])
						if name == "" {
							name = text(c.Fields["name"])
						}
					}
					b := r.Scopes[scope].Bindings[name]
					b.Value.Invalid = true
					r.Scopes[scope].Bindings[name] = b
					if c.Kind == "import_specifier" {
						return
					}
				}
				for _, child := range c.Children {
					imports(child, typeOnly)
				}
			}
			imports(n, false)
			return
		case "class_declaration", "abstract_class_declaration":
			name := text(n.Fields["name"])
			if name == "" && r.Exports["default"] == "@default" {
				name = "@default"
			}
			if name != "" {
				value := staticClass(n, out)
				value.Invalid = ambient[n.Start]
				r.Scopes[scope].Bindings[name] = facts.RouteBinding{Value: value}
			}
		case "enum_declaration", "function_signature":
			if name := text(n.Fields["name"]); name != "" {
				r.Scopes[scope].Bindings[name] = facts.RouteBinding{}
			}
		case "export_statement":
			decl := n.Fields["declaration"]
			if decl == nil {
				decl = n.Fields["value"]
			}
			if decl != nil {
				for _, token := range n.Tokens {
					if token == "default" {
						// The grammar calls an anonymous default class "class",
						// although this direct export is a class declaration.
						if decl.Kind == "class" {
							clone := *decl
							clone.Kind = "class_declaration"
							decl = &clone
						}
						name := text(decl.Fields["name"])
						if decl.Kind == "identifier" {
							name = decl.Text
						}
						if name == "" {
							name = "@default"
							value := m.route(decl)
							if strings.HasPrefix(path.Base(file), "openapi-ts.config.") || strings.HasPrefix(path.Base(file), "orval.config.") {
								value = m.url(decl)
							}
							r.Scopes[scope].Bindings[name] = facts.RouteBinding{Value: value}
							if decl.Kind == "arrow_function" || decl.Kind == "function_expression" {
								clone := *decl
								clone.Fields = map[string]*parse.Node{}
								for k, v := range decl.Fields {
									clone.Fields[k] = v
								}
								clone.Fields["name"] = &parse.Node{Kind: "identifier", Text: name}
								decl = &clone
							}
						}
						r.Exports["default"] = name
					}
				}
				isDefault := false
				for _, token := range n.Tokens {
					isDefault = isDefault || token == "default"
				}
				walk(decl, scope, owner, ownerName, conditional, !isDefault)
			} else if n.Fields["source"] == nil {
				for _, c := range n.Children {
					if c.Kind == "export_clause" {
						for _, s := range c.Children {
							local, alias := text(s.Fields["name"]), text(s.Fields["alias"])
							if alias == "" {
								alias = local
							}
							r.Exports[alias] = local
						}
					}
				}
			}
			return
		case "variable_declarator":
			name, value := n.Fields["name"], n.Fields["value"]
			if name != nil && name.Kind == "identifier" {
				b := facts.RouteBinding{Value: m.route(value)}
				// Only module-level const literal initializers resolve mount prefixes.
				r.Scopes[scope].Bindings[name.Text] = b
				if exported {
					r.Exports[name.Text] = name.Text
				}
			} else if name != nil && requireDestructure(name, value, func(local string, b facts.RouteBinding) { r.Scopes[scope].Bindings[local] = b }) {
				// T036 row CJ-U3: the names read members of the required module.
			} else if name != nil {
				var bind func(*parse.Node)
				bind = func(n *parse.Node) {
					if n.Kind == "identifier" || n.Kind == "shorthand_property_identifier_pattern" {
						r.Scopes[scope].Bindings[n.Text] = facts.RouteBinding{}
						return
					}
					if n.Kind != "property_identifier" {
						for _, c := range n.Children {
							bind(c)
						}
					}
				}
				bind(name)
			}
			if value != nil && name != nil && name.Kind == "identifier" && (value.Kind == "arrow_function" || value.Kind == "function_expression") {
				clone := *value
				clone.Fields = map[string]*parse.Node{}
				for k, v := range value.Fields {
					clone.Fields[k] = v
				}
				clone.Fields["name"] = name
				walk(&clone, scope, owner, ownerName, conditional, exported)
			} else {
				walk(value, scope, owner, ownerName, conditional, false)
			}
			return
		case "lexical_declaration", "variable_declaration":
			isConst := false
			for _, token := range n.Tokens {
				isConst = isConst || token == "const"
			}
			for _, c := range n.Children {
				walk(c, scope, owner, ownerName, conditional, exported)
				if c.Kind == "variable_declarator" && !isConst {
					if name := c.Fields["name"]; name != nil && name.Kind == "identifier" {
						if b, ok := r.Scopes[scope].Bindings[name.Text]; ok {
							b.Mutable = true
							r.Scopes[scope].Bindings[name.Text] = b
						}
					}
				}
				if n.Kind == "variable_declaration" && c.Kind == "variable_declarator" {
					// var shadows the enclosing function even outside this block.
					at := scope
					for at > 0 && !r.Scopes[at].Function {
						at = r.Scopes[at].Parent
					}
					if name := c.Fields["name"]; name != nil && name.Kind == "identifier" && at != scope {
						r.Scopes[at].Bindings[name.Text] = facts.RouteBinding{}
					}
				}
				if c.Kind == "variable_declarator" && scope == 0 && isConst {
					name := text(c.Fields["name"])
					b := r.Scopes[scope].Bindings[name]
					b.Constant = b.Value.Kind == "literal"
					b.ModuleConst = true
					r.Scopes[scope].Bindings[name] = b
				}
			}
			return
		case "function_declaration", "generator_function_declaration", "function_expression", "arrow_function", "method_definition":
			child := newScope(scope)
			r.Scopes[child].Function = true
			r.Scopes[child].Async = asyncFunction(n)
			r.Scopes[child].Params = routeParams(n)
			r.Scopes[child].Start = int(n.Start)
			name := text(n.Fields["name"])
			bind := name != "" && n.Kind != "method_definition"
			if bind {
				kind := "function"
				if previous := r.Scopes[scope].Bindings[name]; previous.Value.Kind == "function" || previous.Value.Kind == "ambiguous" {
					kind = "ambiguous"
				}
				r.Scopes[scope].Bindings[name] = facts.RouteBinding{Value: facts.RouteExpr{Kind: kind}, ReturnScope: child, Async: hasToken(n, "async")}
				if exported {
					r.Exports[name] = name
				}
			}
			params := func(p *parse.Node) {
				patternBindings(p, func(name string) {
					r.Scopes[child].Bindings[name] = facts.RouteBinding{Value: facts.RouteExpr{Kind: "parameter"}}
				})
			}
			params(n.Fields["parameters"])
			params(n.Fields["parameter"])
			enclosing, enclosingName := owner, ownerName
			owner, ownerName = ownerFor(n, owner, ownerName)
			functionOwner, _ := ownerFor(n, "", "")
			if body := n.Fields["body"]; body != nil && body.Kind == "statement_block" && hasDirective(prologue(body), "use server") {
				a := facts.InlineAction{Form: inlineForm(n, constValues, boundValues), Async: hasToken(n, "async"), Scope: child, Offset: int(n.Start), Identity: functionOwner, Owner: enclosing, OwnerName: enclosingName, Span: span(n), Hash: contract.ContentHash(source[n.Start:n.End])}
				if n.Kind == "function_declaration" || constValues[n.Start] || boundValues[n.Start] {
					a.Name = name
				}
				actions.Inline = append(actions.Inline, a)
			}
			if functionOwner != "" {
				p := n.Fields["parameters"]
				if p == nil {
					p = n.Fields["parameter"]
				}
				var parameters []facts.ClientParameter
				if p != nil {
					items := p.Children
					if p.Kind == "identifier" {
						items = []*parse.Node{p}
					}
					for _, item := range items {
						name := item.Fields["pattern"]
						if name == nil {
							name = item.Fields["name"]
						}
						if item.Kind == "identifier" {
							name = item
						}
						if name != nil && name.Kind == "identifier" {
							parameters = append(parameters, facts.ClientParameter{Name: name.Text, Default: m.url(item.Fields["value"])})
						} else if item.Kind != "comment" {
							parameters = append(parameters, facts.ClientParameter{})
						}
					}
				}
				r.Parameters[functionOwner] = parameters
				r.ParameterScopes[functionOwner] = child
			}
			body := n.Fields["body"]
			var returns []facts.RouteExpr
			var collect func(*parse.Node)
			collect = func(v *parse.Node) {
				if v == nil {
					return
				}
				if v != body && (strings.Contains(v.Kind, "function") || v.Kind == "method_definition") {
					return
				}
				if v.Kind == "return_statement" {
					for _, c := range v.Children {
						if c.Kind != "comment" {
							returns = append(returns, m.route(c))
							break
						}
					}
					return
				}
				for _, c := range v.Children {
					collect(c)
				}
			}
			if body != nil && body.Kind != "statement_block" {
				returns = append(returns, m.route(body))
			} else {
				collect(body)
				// Without an unconditional terminal return, a factory can also yield
				// undefined. Do not infer framework objects from partial returns.
				terminalReturn := false
				if body != nil {
					for i := len(body.Children) - 1; i >= 0; i-- {
						if body.Children[i].Kind != "comment" {
							terminalReturn = body.Children[i].Kind == "return_statement"
							break
						}
					}
				}
				if !terminalReturn {
					returns = nil
				}
			}
			if bind {
				b := r.Scopes[scope].Bindings[name]
				b.Returns = returns
				r.Scopes[scope].Bindings[name] = b
			}
			if body != nil && body.Kind == "statement_block" {
				for _, c := range body.Children {
					walk(c, child, owner, ownerName, conditional, false)
				}
			} else {
				walk(body, child, owner, ownerName, conditional, false)
			}
			return
		case "statement_block":
			scope = newScope(scope)
		case "catch_clause", "for_in_statement":
			scope = newScope(scope)
			parameter := n.Fields["parameter"]
			if n.Kind == "for_in_statement" {
				parameter = n.Fields["left"]
			}
			patternBindings(parameter, func(name string) {
				r.Scopes[scope].Bindings[name] = facts.RouteBinding{}
			})
		case "assignment_expression", "augmented_assignment_expression", "update_expression", "unary_expression":
			left := n.Fields["left"]
			if left == nil {
				left = n.Fields["argument"]
			}
			if left != nil && (n.Kind != "unary_expression" || strings.HasPrefix(n.Text, "delete")) && (left.Kind == "member_expression" || left.Kind == "subscript_expression") {
				r.Clients = append(r.Clients, facts.ClientCall{Expression: facts.RouteExpr{Kind: "config_mutation", Parts: []facts.RouteExpr{m.url(left)}}, Scope: scope, Owner: owner, Span: span(n)})
			}
			if scope == 0 && !conditional && n.Kind == "assignment_expression" {
				left := m.route(n.Fields["left"])
				if left.Kind == "member_expression" && left.Name == "baseURL" && left.Object != nil && left.Object.Kind == "member_expression" && left.Object.Name == "defaults" && left.Object.Object != nil {
					r.Defaults = append(r.Defaults, facts.ClientDefault{Receiver: *left.Object.Object, Value: m.route(n.Fields["right"]), Scope: scope})
				}
			}
			// Mutation removes proof for this binding throughout its scope. This is
			// deliberately conservative rather than attempting control-flow analysis.
			if left := n.Fields["left"]; left != nil && left.Kind == "identifier" {
				for at := scope; at >= 0; at = r.Scopes[at].Parent {
					if _, ok := r.Scopes[at].Bindings[left.Text]; ok {
						r.Scopes[at].Bindings[left.Text] = facts.RouteBinding{}
						break
					}
				}
			}
		case "if_statement", "switch_statement", "ternary_expression":
			conditional = true
		case "return_statement", "throw_statement":
			// DA-E3: an exit of the nearest function body.
			at := scope
			for at > 0 && !r.Scopes[at].Function {
				at = r.Scopes[at].Parent
			}
			if at > 0 {
				r.Scopes[at].Exits = append(r.Scopes[at].Exits, span(n))
			}
		case "call_expression":
			if callee := n.Fields["function"]; callee != nil && (callee.Kind == "import" || callee.Kind == "identifier" && callee.Text == "require") {
				// A dynamic import() or require() loads a module at run
				// time (row SA-C3's server graph, T024 review).
				if spec, ok := loadSpecifier(n); ok {
					actions.Loads = append(actions.Loads, spec)
				} else {
					actions.LoadsUnread = true
				}
			}
			e := m.route(n)
			clientExpression := e
			clientNode := n
			if callee := n.Fields["function"]; callee != nil && callee.Kind == "await_expression" && len(callee.Children) == 1 {
				v := m.route(callee.Children[0])
				clientExpression.Object = &v
				clone := *n
				clone.Start = callee.Children[0].Start
				clone.Row = callee.Children[0].Row
				clone.Column = callee.Children[0].Column
				clientNode = &clone
			}
			location := span(clientNode)
			clientOwner, clientName := closestOwner(location, owner, ownerName)
			r.Clients = append(r.Clients, facts.ClientCall{Expression: clientExpression, Scope: scope, Owner: clientOwner, OwnerName: clientName, Span: location, Hash: contract.ContentHash(source[clientNode.Start:n.End])})
			if e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil {
				c := facts.RouteCall{Receiver: *e.Object.Object, Method: e.Object.Name, Args: e.Args, Scope: scope, Owner: owner, OwnerName: ownerName, Conditional: conditional, Span: span(n), Hash: contract.ContentHash(source[n.Start:n.End])}
				// Peel chained verb calls until the .route(pattern) builder.
				base := c.Receiver
				for base.Kind == "call_expression" && base.Object != nil && base.Object.Kind == "member_expression" {
					if base.Object.Name == "route" && len(base.Args) > 0 {
						p := base.Args[0]
						c.Pattern = &p
						c.Receiver = *base.Object.Object
						break
					}
					if !routeVerb(base.Object.Name) || base.Object.Object == nil {
						break
					}
					base = *base.Object.Object
				}
				if c.Method == "setConfig" {
					c.Args = m.url(n).Args
				}
				if c.Method == "use" || c.Method == "setConfig" || routeVerb(c.Method) {
					r.Calls = append(r.Calls, c)
				}
			}
		}
		for _, c := range n.Children {
			walk(c, scope, owner, ownerName, conditional, exported)
		}
	}
	walk(root, 0, id("file", file, file), "", false, false)
	for scope, counts := range declarations {
		for name, count := range counts {
			b := r.Scopes[scope].Bindings[name]
			if b.Value.Kind == "static_class" && (count > 1 || b.Value.Invalid) {
				b.Value.Properties = nil
				r.Scopes[scope].Bindings[name] = b
			}
		}
	}
	markHeyConfigConditionals(root, r)
	markGuardedCalls(root, r)
	cacheURLSyntax(m, root, r, out)
	cacheAngular(m, root, r, out)
	cacheGlobalNames(file, root, r)
	cacheCommonJS(root, r)
	r.SetSource(root.Text, int(root.Start))
	if r.CommonJS != nil || len(r.Calls) > 0 || len(r.Clients) > 0 || len(r.Scopes[0].Bindings) > 0 || len(r.AmbientStrings) > 0 || len(r.GlobalDefinitions) > 0 || len(r.AutoImports) > 0 {
		out.Routes = r
	}
	for i := range actions.Inline {
		a := &actions.Inline[i]
		a.Reassigned = a.Form == "let_binding" && assigned[a.Name]
	}
	if len(actions.Inline) > 0 || len(actions.Attributes) > 0 || len(actions.Ternaries) > 0 || len(actions.Loads) > 0 || actions.LoadsUnread {
		out.Actions = actions
	}
}

// loadSpecifier reads the module specifier of import(...) or require(...)
// when its first argument is a string literal (a template literal
// without substitutions included).
func loadSpecifier(n *parse.Node) (string, bool) {
	args := n.Fields["arguments"]
	if args == nil {
		return "", false
	}
	for _, c := range args.Children {
		switch c.Kind {
		case "comment":
			continue
		case "string":
			return strings.Trim(c.Text, "\"'"), true
		case "template_string":
			if len(c.Children) == 0 || !hasChild(c, "template_substitution") {
				return strings.Trim(c.Text, "`"), true
			}
		}
		return "", false
	}
	return "", false
}

// declaratorValues lists the function values of variable declarators by
// start byte: constValues those of a `const` declaration, boundValues
// those of `let` and `var` (row SA-I1 reads only const bindings).
func declaratorValues(root *parse.Node) (constValues, boundValues map[uint32]bool) {
	constValues, boundValues = map[uint32]bool{}, map[uint32]bool{}
	var walk func(*parse.Node)
	walk = func(n *parse.Node) {
		if n.Kind == "lexical_declaration" || n.Kind == "variable_declaration" {
			constant := n.Kind == "lexical_declaration" && hasToken(n, "const")
			for _, d := range n.Children {
				name, value := d.Fields["name"], d.Fields["value"]
				if d.Kind != "variable_declarator" || name == nil || name.Kind != "identifier" || value == nil {
					continue
				}
				if value.Kind == "arrow_function" || value.Kind == "function_expression" {
					if constant {
						constValues[value.Start] = true
					} else {
						boundValues[value.Start] = true
					}
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return constValues, boundValues
}

// inlineForm names the form of a function whose body prologue holds 'use
// server' (row SA-I1): named_function, const_arrow, const_function and
// anonymous are actions when async; sync, let_binding and method are
// refused (D).
func inlineForm(n *parse.Node, constValues, boundValues map[uint32]bool) string {
	switch {
	case n.Kind == "method_definition":
		return "method"
	case !hasToken(n, "async"):
		return "sync"
	case n.Kind == "function_declaration":
		return "named_function"
	case constValues[n.Start] && n.Kind == "arrow_function":
		return "const_arrow"
	case constValues[n.Start]:
		return "const_function"
	case boundValues[n.Start]:
		return "let_binding"
	}
	return "anonymous"
}

// ternaryLeaves flattens a conditional expression's alternatives through
// parentheses and nested conditionals: the plain identifiers among them,
// and whether every alternative is one.
func ternaryLeaves(n *parse.Node) ([]string, bool) {
	var leaves []string
	complete := true
	var walk func(*parse.Node)
	walk = func(v *parse.Node) {
		for v != nil && v.Kind == "parenthesized_expression" && len(v.Children) == 1 {
			v = v.Children[0]
		}
		switch {
		case v == nil:
			complete = false
		case v.Kind == "ternary_expression":
			walk(v.Fields["consequence"])
			walk(v.Fields["alternative"])
		case v.Kind == "identifier":
			leaves = append(leaves, v.Text)
		default:
			complete = false
		}
	}
	walk(n.Fields["consequence"])
	walk(n.Fields["alternative"])
	return leaves, complete
}

// actionAttribute reads a JSX attribute `name={value}` whose name is
// action or formAction (row SA-C1), or whose value is a plain identifier
// (an action passed as a prop, row D). Other attributes are not recorded.
func actionAttribute(n *parse.Node) (facts.ActionAttribute, bool) {
	if len(n.Children) != 2 || n.Children[0].Kind != "property_identifier" || n.Children[1].Kind != "jsx_expression" {
		return facts.ActionAttribute{}, false
	}
	var value *parse.Node
	for _, c := range n.Children[1].Children {
		if c.Kind != "comment" {
			if value != nil {
				return facts.ActionAttribute{}, false
			}
			value = c
		}
	}
	name := n.Children[0].Text
	if value == nil || name != "action" && name != "formAction" && value.Kind != "identifier" {
		return facts.ActionAttribute{}, false
	}
	return facts.ActionAttribute{Name: name, Value: routeExpr(value)}, true
}

func routeVerb(s string) bool {
	return strings.Contains(" get post put patch delete options head all ", " "+s+" ")
}

// asyncFunction reports an async or generator function, whose body may
// run after its call returns.
func asyncFunction(n *parse.Node) bool {
	if strings.Contains(n.Kind, "generator") {
		return true
	}
	for _, token := range n.Tokens {
		if token == "async" || token == "*" {
			return true
		}
	}
	return false
}

// routeParams reads a function's parameters as written (DA-E3): an
// identifier, an object pattern of shorthand or key: identifier
// properties (defaults allowed), else a form not read.
func routeParams(n *parse.Node) []facts.RouteParam {
	if p := n.Fields["parameter"]; p != nil {
		return []facts.RouteParam{routeParam(p)}
	}
	p := n.Fields["parameters"]
	if p == nil {
		return nil
	}
	var out []facts.RouteParam
	for _, item := range p.Children {
		if item.Kind == "comment" {
			continue
		}
		out = append(out, routeParam(item))
	}
	return out
}

func routeParam(item *parse.Node) facts.RouteParam {
	switch item.Kind {
	case "required_parameter", "optional_parameter":
		if pattern := item.Fields["pattern"]; pattern != nil {
			p := routeParam(pattern)
			p.Default = p.Default || item.Fields["value"] != nil
			return p
		}
	case "assignment_pattern":
		if left := item.Fields["left"]; left != nil {
			p := routeParam(left)
			p.Default = true
			return p
		}
	case "rest_pattern":
		return facts.RouteParam{Rest: true}
	case "identifier":
		return facts.RouteParam{Name: item.Text}
	case "object_pattern":
		out := facts.RouteParam{Props: map[string]string{}}
		for _, c := range item.Children {
			switch c.Kind {
			case "shorthand_property_identifier_pattern":
				out.Props[c.Text] = c.Text
			case "object_assignment_pattern":
				if left := c.Fields["left"]; left != nil && left.Kind == "shorthand_property_identifier_pattern" {
					out.Props[left.Text] = left.Text
				} else {
					out.Open = true
				}
			case "pair_pattern":
				key, value := c.Fields["key"], c.Fields["value"]
				if key != nil && key.Kind == "property_identifier" && value != nil && value.Kind == "identifier" {
					out.Props[key.Text] = value.Text
				} else {
					out.Open = true
				}
			case "comment":
			default:
				out.Open = true
			}
		}
		return out
	}
	return facts.RouteParam{}
}

// markGuardedCalls sets Guarded on the calls under a branch, loop, try
// or short-circuit operator of their own function body (or of the top
// level): a function's body starts unguarded, since DA-E3 places it at
// its call.
func markGuardedCalls(root *parse.Node, r *facts.RouteFacts) {
	guarded := map[contract.Span]bool{}
	var walk func(*parse.Node, bool)
	walk = func(n *parse.Node, g bool) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "function_declaration", "generator_function_declaration", "function_expression", "arrow_function", "method_definition", "generator_function":
			g = false
		case "if_statement", "switch_statement", "ternary_expression", "for_statement", "for_in_statement", "while_statement", "do_statement", "try_statement", "catch_clause":
			g = true
		case "binary_expression":
			for _, token := range n.Tokens {
				if token == "&&" || token == "||" || token == "??" {
					g = true
				}
			}
		case "call_expression":
			if g {
				guarded[span(n)] = true
			}
		}
		for _, c := range n.Children {
			walk(c, g)
		}
	}
	walk(root, false)
	for i := range r.Calls {
		r.Calls[i].Guarded = guarded[r.Calls[i].Span]
	}
	for i := range r.Clients {
		r.Clients[i].Guarded = guarded[r.Clients[i].Span]
	}
}

// patternBindings calls bind for each name a parameter list or binding
// pattern binds, with the binding rule of boundNames: identifiers of
// patterns, the left side of a default, the value of a key: value pair and
// rest elements bind; a default value and a computed key are expressions
// read in the enclosing scope and bind nothing (T034 review).
func patternBindings(p *parse.Node, bind func(name string)) {
	if p == nil {
		return
	}
	switch p.Kind {
	case "identifier", "shorthand_property_identifier_pattern":
		bind(p.Text)
		return
	case "property_identifier", "type_annotation", "computed_property_name":
		return
	case "required_parameter", "optional_parameter":
		patternBindings(p.Fields["pattern"], bind)
		return
	case "assignment_pattern", "object_assignment_pattern":
		patternBindings(p.Fields["left"], bind)
		return
	case "pair_pattern":
		patternBindings(p.Fields["value"], bind)
		return
	}
	for _, c := range p.Children {
		patternBindings(c, bind)
	}
}

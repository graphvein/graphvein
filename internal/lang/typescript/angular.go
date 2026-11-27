package typescript

import (
	"maps"
	"slices"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// angularExpr is the Angular provider syntax of n, built without sharing.
func angularExpr(n *parse.Node) facts.RouteExpr {
	return (*exprMemo)(nil).angular(n)
}

func (m *exprMemo) buildAngular(n *parse.Node) facts.RouteExpr {
	e := m.url(n)
	if n == nil {
		return e
	}
	// e may share its Parts and Properties with other expressions (exprMemo):
	// extend copies of them, never the shared ones.
	if n.Kind == "array" {
		e.Parts = slices.Clip(e.Parts)
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Parts = append(e.Parts, m.angular(c))
			}
		}
	}
	// Provider arrays are syntax, outside the URL evaluator's value subset.
	if n.Kind == "object" {
		e.Properties = maps.Clone(e.Properties)
		for _, p := range n.Children {
			if p.Kind == "pair" {
				e.Properties[strings.Trim(text(p.Fields["key"]), "\"'")] = m.angular(p.Fields["value"])
			}
		}
	}
	if n.Kind == "call_expression" || n.Kind == "new_expression" {
		if n.Kind == "new_expression" {
			object := m.url(n.Fields["constructor"])
			e.Object = &object
		}
		e.Args = nil
		if args := n.Fields["arguments"]; args != nil {
			for _, c := range args.Children {
				if c.Kind != "comment" {
					e.Args = append(e.Args, m.angular(c))
				}
			}
		}
	}
	return e
}

func cacheAngular(m *exprMemo, root *parse.Node, r *facts.RouteFacts, out *Facts) {
	cacheLocalInjection(r)
	// Skip unrelated modules before walking syntax or retaining Angular metadata.
	angular := false
	for _, imp := range out.Imports {
		angular = angular || strings.HasPrefix(imp.Module, "@angular/")
	}
	if !angular && !strings.Contains(root.Text, "class ") && !strings.Contains(root.Text, ".clone(") && !strings.Contains(root.Text, "providers") {
		return
	}
	a := &facts.AngularFacts{}
	type field struct {
		injection  facts.AngularInjection
		start, end uint32
		name       string
		local      bool
		scope      int
	}
	var fields []field
	var reboundThis [][2]uint32
	var visit func(*parse.Node, []facts.RouteExpr, bool)
	visit = func(n *parse.Node, decorators []facts.RouteExpr, conditional bool) {
		if n == nil {
			return
		}
		if n.Kind == "if_statement" || n.Kind == "ternary_expression" || n.Kind == "switch_statement" {
			conditional = true
		}
		if n.Kind == "function_declaration" || n.Kind == "function_expression" {
			reboundThis = append(reboundThis, [2]uint32{n.Start, n.End})
		}
		for _, c := range n.Children {
			if c.Kind == "decorator" && len(c.Children) > 0 {
				decorators = append(decorators, m.angular(c.Children[0]))
			}
		}
		if n.Kind == "class_declaration" || n.Kind == "abstract_class_declaration" {
			for _, dec := range decorators {
				for _, arg := range dec.Args {
					if arg.Kind == "object" {
						a.Registrations = append(a.Registrations, facts.AngularRegistration{Expression: arg, Conditional: conditional})
					}
				}
			}
			name := text(n.Fields["name"])
			cl := facts.AngularClass{Name: name, Decorators: decorators, Methods: map[string]string{}}
			for _, v := range out.Nodes {
				if v.Kind == "method" && strings.HasPrefix(v.QualifiedName, name+".") {
					if _, repeated := cl.Methods[v.Name]; repeated {
						cl.Methods[v.Name] = ""
					} else {
						cl.Methods[v.Name] = v.Identity
					}
				}
			}
			a.Classes = append(a.Classes, cl)
			body := n.Fields["body"]
			if body != nil {
				for _, member := range body.Children {
					if member.Kind == "public_field_definition" {
						value := m.angular(member.Fields["value"])
						if value.Kind == "call_expression" && value.Object != nil && len(value.Args) == 1 {
							fields = append(fields, field{injection: facts.AngularInjection{Token: value.Args[0], Factory: *value.Object}, start: n.Start, end: n.End, name: text(member.Fields["name"])})
						}
					}
					if member.Kind == "method_definition" && text(member.Fields["name"]) == "constructor" {
						if ps := member.Fields["parameters"]; ps != nil {
							for _, p := range ps.Children {
								pn := p.Fields["pattern"]
								if pn == nil {
									pn = p.Fields["name"]
								}
								if pn == nil || pn.Kind != "identifier" {
									continue
								}
								property := false
								for _, token := range p.Tokens {
									property = property || token == "readonly"
								}
								for _, c := range p.Children {
									property = property || c.Kind == "accessibility_modifier"
								}
								var token, factory facts.RouteExpr
								if typ := p.Fields["type"]; typ != nil && len(typ.Children) == 1 {
									token = m.url(typ.Children[0])
									token.Kind = "identifier"
									token.Name = typ.Children[0].Text
									if parts := strings.Split(token.Name, "."); len(parts) == 2 {
										object := facts.RouteExpr{Kind: "identifier", Name: parts[0]}
										token = facts.RouteExpr{Kind: "member_expression", Name: parts[1], Object: &object}
									}
								}
								for _, c := range p.Children {
									if c.Kind == "decorator" && len(c.Children) > 0 {
										dec := m.angular(c.Children[0])
										if dec.Object != nil && len(dec.Args) == 1 {
											token, factory = dec.Args[0], *dec.Object
										}
									}
								}
								if property && token.Kind != "" {
									fields = append(fields, field{injection: facts.AngularInjection{Token: token, Factory: factory}, start: n.Start, end: n.End, name: pn.Text})
								}
								if factory.Kind != "" {
									fields = append(fields, field{injection: facts.AngularInjection{Token: token, Factory: factory}, start: member.Start, end: member.End, name: pn.Text, local: true, scope: r.ParameterScopes[cl.Methods["constructor"]]})
								}
							}
						}
					}
				}
			}
		}
		if n.Kind == "object" || n.Kind == "call_expression" {
			e := m.angular(n)
			scope := 0
			for _, c := range r.Clients {
				if c.Expression.Offset == int(n.Start) {
					scope = c.Scope
					break
				}
			}
			if n.Kind == "object" {
				if _, ok := e.Properties["providers"]; ok {
					a.Registrations = append(a.Registrations, facts.AngularRegistration{Expression: e, Scope: scope, Conditional: conditional})
				}
				if _, ok := e.Properties["provide"]; ok {
					a.Registrations = append(a.Registrations, facts.AngularRegistration{Expression: e, Scope: scope, Conditional: conditional})
				}
			}
			if n.Kind == "call_expression" && e.Object != nil {
				callee := e.Object
				if callee.Kind == "member_expression" {
					callee = callee.Object
				}
				if callee != nil && callee.Kind == "identifier" && r.Scopes[0].Bindings[callee.Name].Module != "" {
					a.Registrations = append(a.Registrations, facts.AngularRegistration{Expression: e, Scope: scope, Conditional: conditional})
				}
			}
		}
		if n.Kind == "arrow_function" || n.Kind == "function_declaration" || n.Kind == "function_expression" || n.Kind == "method_definition" {
			ps := n.Fields["parameters"]
			if ps != nil && len(ps.Children) >= 2 {
				param := func(p *parse.Node) string {
					v := p.Fields["pattern"]
					if v == nil {
						v = p.Fields["name"]
					}
					if p.Kind == "identifier" {
						v = p
					}
					return text(v)
				}
				name := text(n.Fields["name"])
				if name == "" {
					for _, v := range out.Nodes {
						if v.Span != nil && v.Span.EndLine == span(n).EndLine && v.Span.EndColumn == span(n).EndColumn && v.Kind == "function" {
							name = v.Name
							break
						}
					}
				}
				if name == "intercept" {
					for _, v := range a.Classes {
						if _, ok := v.Methods["intercept"]; ok {
							name = v.Name
						}
					}
				}
				body := n.Fields["body"]
				in := facts.AngularInterceptor{Name: name, Request: param(ps.Children[0]), Next: param(ps.Children[1]), Body: m.urlStatements(body)}
				if body != nil && body.Kind != "statement_block" {
					in.Body = []facts.URLStatement{{Kind: "return_statement", Expression: m.url(body)}}
				}
				var expressions func(*parse.Node)
				expressions = func(v *parse.Node) {
					if v == nil {
						return
					}
					if v.Kind == "call_expression" || v.Kind == "new_expression" {
						in.Expressions = append(in.Expressions, m.angular(v))
					}
					for _, c := range v.Children {
						expressions(c)
					}
				}
				expressions(body)
				for _, c := range r.Clients {
					if c.Expression.Offset >= int(n.Start) && c.Expression.Offset < int(n.End) {
						in.Scope, in.URLScope = c.Scope, c.URLScope
						break
					}
				}
				a.Interceptors = append(a.Interceptors, in)
			}
		}
		for _, c := range n.Children {
			if c.Kind != "decorator" {
				visit(c, decorators, conditional)
			}
		}
	}
	visit(root, nil, false)
	for i := range r.Clients {
		c := &r.Clients[i]
		if c.Expression.Object == nil || c.Expression.Object.Object == nil {
			continue
		}
		receiver := c.Expression.Object.Object
		if receiver.Kind == "member_expression" && receiver.Object != nil && receiver.Object.Kind == "this" {
			rebound := false
			for _, interval := range reboundThis {
				rebound = rebound || c.Expression.Offset >= int(interval[0]) && c.Expression.Offset < int(interval[1])
			}
			if rebound {
				continue
			}
			for _, f := range fields {
				if !f.local && c.Expression.Offset >= int(f.start) && c.Expression.Offset < int(f.end) && receiver.Name == f.name {
					v := f.injection
					v.Scope = 0
					c.Injection = &v
				}
			}
		} else if receiver.Kind == "identifier" {
			for _, f := range fields {
				if f.local && receiver.Name == f.name && c.Expression.Offset >= int(f.start) && c.Expression.Offset < int(f.end) {
					for at := c.Scope; at >= 0; at = r.Scopes[at].Parent {
						if _, ok := r.Scopes[at].Bindings[receiver.Name]; ok {
							if at == f.scope {
								v := f.injection
								c.Injection = &v
							}
							break
						}
					}
				}
			}
		}
	}
	if len(a.Classes)+len(a.Interceptors)+len(a.Registrations) > 0 {
		r.Angular = a
	}
}

func cacheLocalInjection(r *facts.RouteFacts) {
	for i := range r.Clients {
		c := &r.Clients[i]
		if c.Expression.Object == nil || c.Expression.Object.Object == nil {
			continue
		}
		receiver := c.Expression.Object.Object
		if receiver.Kind == "identifier" {
			for at := c.Scope; at >= 0; at = r.Scopes[at].Parent {
				if b, ok := r.Scopes[at].Bindings[receiver.Name]; ok {
					constant, functionLocal := false, false
					for at := c.URLScope; at >= 0; at = r.URLScopes[at].Parent {
						if binding, ok := r.URLScopes[at].Bindings[receiver.Name]; ok {
							constant = binding.Evaluation != nil && binding.Evaluation.Constant
							for owner := at; owner >= 0; owner = r.URLScopes[owner].Parent {
								functionLocal = functionLocal || r.URLScopes[owner].Function
							}
							break
						}
					}
					if constant && functionLocal && b.Value.Kind == "call_expression" && b.Value.Object != nil && len(b.Value.Args) == 1 {
						c.Injection = &facts.AngularInjection{Token: b.Value.Args[0], Factory: *b.Value.Object, Scope: at}
					}
					break
				}
			}
		}
	}
}

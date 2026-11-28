package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func cacheURLSyntax(m *exprMemo, root *parse.Node, r *facts.RouteFacts, out *Facts) {
	r.URLScopes = []facts.RouteScope{{Parent: -1, Bindings: map[string]facts.RouteBinding{}}}
	for name, b := range r.Scopes[0].Bindings {
		if b.Module != "" {
			r.URLScopes[0].Bindings[name] = b
		}
	}
	// Every call of a chain (db.select().from(t)) starts at the same byte,
	// so per-call maps are keyed by the call's whole span: start byte and
	// end position (T018h Revision 4).
	callScopes := map[callKey]int{}
	urlScopes := map[int]int{}
	hasURL := false
	type mutation struct {
		scope int
		name  string
	}
	var mutations []mutation
	newScope := func(parent int, function bool) int {
		r.URLScopes = append(r.URLScopes, facts.RouteScope{Parent: parent, Function: function, Bindings: map[string]facts.RouteBinding{}})
		return len(r.URLScopes) - 1
	}
	var rootName func(*parse.Node) string
	rootName = func(n *parse.Node) string {
		if n == nil {
			return ""
		}
		if n.Kind == "identifier" {
			return n.Text
		}
		if n.Kind == "parenthesized_expression" || n.Kind == "as_expression" || n.Kind == "satisfies_expression" || n.Kind == "non_null_expression" || n.Kind == "type_assertion" {
			for _, c := range n.Children {
				if !strings.Contains(c.Kind, "type") && c.Kind != "comment" {
					return rootName(c)
				}
			}
		}
		if n.Kind == "member_expression" || n.Kind == "subscript_expression" {
			return rootName(n.Fields["object"])
		}
		return ""
	}
	var walk func(*parse.Node, int, string, bool, bool)
	walk = func(n *parse.Node, scope int, boundName string, constant, single bool) {
		if n == nil {
			return
		}
		if n.Kind == "member_expression" && text(n.Fields["property"]) == "href" {
			urlScopes[int(n.Start)] = scope
		}
		if n.Kind == "new_expression" && text(n.Fields["constructor"]) == "URL" {
			hasURL = true
		}
		switch n.Kind {
		case "import_statement":
			return
		case "lexical_declaration", "variable_declaration":
			isConst := false
			count := 0
			for _, t := range n.Tokens {
				isConst = isConst || t == "const"
			}
			for _, c := range n.Children {
				if c.Kind == "variable_declarator" {
					count++
				}
			}
			for _, c := range n.Children {
				walk(c, scope, "", isConst, count == 1)
			}
			return
		case "variable_declarator":
			name := n.Fields["name"]
			v := n.Fields["value"]
			if name != nil && (name.Kind == "identifier" || name.Kind == "undefined") {
				r.URLScopes[scope].Bindings[name.Text] = facts.RouteBinding{Evaluation: &facts.URLBinding{Value: m.url(v), Constant: constant, Single: single, Offset: int(n.Start)}}
				if v != nil && (v.Kind == "arrow_function" || v.Kind == "function_expression") {
					walk(v, scope, name.Text, constant, single)
				} else {
					walk(v, scope, "", false, false)
				}
			} else {
				var shadow func(*parse.Node)
				shadow = func(v *parse.Node) {
					if v == nil {
						return
					}
					if v.Kind == "identifier" || v.Kind == "undefined" || v.Kind == "shorthand_property_identifier_pattern" {
						r.URLScopes[scope].Bindings[v.Text] = facts.RouteBinding{}
						return
					}
					if !strings.Contains(v.Kind, "type") && v.Kind != "property_identifier" {
						for _, c := range v.Children {
							shadow(c)
						}
					}
				}
				shadow(name)
				walk(v, scope, "", false, false)
			}
			return
		case "function_declaration", "generator_function_declaration", "function_expression", "arrow_function", "method_definition":
			child := newScope(scope, true)
			name := boundName
			if name == "" {
				name = text(n.Fields["name"])
			}
			identity := ""
			for _, v := range out.Nodes {
				if v.Span != nil && v.Span.StartLine <= span(n).StartLine && v.Span.EndLine == span(n).EndLine && v.Span.EndColumn == span(n).EndColumn && (v.Kind == "function" || v.Kind == "method") {
					identity = v.Identity
					break
				}
			}
			p := n.Fields["parameters"]
			if p == nil {
				p = n.Fields["parameter"]
			}
			var parameters []facts.ClientParameter
			eligible := identity != "" && n.Kind != "generator_function_declaration" && n.Kind != "method_definition"
			for _, t := range n.Tokens {
				if t == "async" || t == "*" {
					eligible = false
				}
			}
			if p != nil {
				items := p.Children
				if p.Kind == "identifier" {
					items = []*parse.Node{p}
				}
				for _, item := range items {
					if item.Kind == "comment" {
						continue
					}
					pn := item.Fields["pattern"]
					if pn == nil {
						pn = item.Fields["name"]
					}
					if item.Kind == "identifier" {
						pn = item
					}
					if pn == nil || pn.Kind != "identifier" && pn.Kind != "undefined" {
						eligible = false
						continue
					}
					parameters = append(parameters, facts.ClientParameter{Name: pn.Text, Default: m.url(item.Fields["value"])})
					r.URLScopes[child].Bindings[pn.Text] = facts.RouteBinding{Value: facts.RouteExpr{Kind: "parameter"}}
				}
			}
			body := n.Fields["body"]
			statements := m.urlStatements(body)
			if body != nil && body.Kind != "statement_block" {
				statements = []facts.URLStatement{{Kind: "return_statement", Expression: m.url(body), Span: span(body)}}
			}
			eligible = eligible && eligibleURLStatements(statements)
			if name != "" && n.Kind != "method_definition" {
				old := r.URLScopes[scope].Bindings[name]
				if old.Evaluation == nil {
					old.Evaluation = &facts.URLBinding{Constant: true, Single: true, Offset: int(n.Start)}
				}
				if old.Evaluation.Helper != nil {
					old.Evaluation.Mutated = true
				}
				old.Evaluation.Helper = &facts.URLHelper{Identity: identity, Scope: child, Eligible: eligible, BoundConst: boundName != "", Parameters: parameters, Body: statements}
				old.Value = orvalTransportSyntax(n, identity, parameters, r, out)
				old.Evaluation.Value = facts.RouteExpr{}
				if !eligible {
					old.Evaluation.Helper.Body = nil
				}
				if boundName != "" && !constant {
					old.Evaluation.Helper.Eligible = false
				}
				r.URLScopes[scope].Bindings[name] = old
			}
			walk(body, child, "", false, false)
			return
		case "statement_block":
			scope = newScope(scope, false)
		case "for_in_statement":
			scope = newScope(scope, false)
			if left := n.Fields["left"]; left != nil {
				if left.Kind == "identifier" {
					r.URLScopes[scope].Bindings[left.Text] = facts.RouteBinding{}
				}
			}
		case "for_statement", "switch_body":
			scope = newScope(scope, false)
		case "catch_clause":
			scope = newScope(scope, false)
			if p := n.Fields["parameter"]; p != nil && p.Kind == "identifier" {
				r.URLScopes[scope].Bindings[p.Text] = facts.RouteBinding{}
			}
		case "class_declaration", "abstract_class_declaration", "enum_declaration":
			if name := text(n.Fields["name"]); name != "" {
				r.URLScopes[scope].Bindings[name] = facts.RouteBinding{}
			}
		case "call_expression":
			callScopes[keyOfCall(n)] = scope
			callee := n.Fields["function"]
			if callee != nil {
				s := strings.ReplaceAll(callee.Text, " ", "")
				if s == "Object.assign" || s == "Object.defineProperty" || s == "Object.defineProperties" || s == "Object.setPrototypeOf" {
					if args := n.Fields["arguments"]; args != nil && len(args.Children) > 0 {
						mutations = append(mutations, mutation{scope, rootName(args.Children[0])})
					}
				}
			}
		case "assignment_expression", "augmented_assignment_expression", "update_expression":
			left := n.Fields["left"]
			if left == nil {
				left = n.Fields["argument"]
			}
			if left != nil && (left.Kind == "member_expression" || left.Kind == "subscript_expression") {
				mutations = append(mutations, mutation{scope, rootName(left)})
			}
		case "unary_expression":
			if strings.HasPrefix(n.Text, "delete") {
				mutations = append(mutations, mutation{scope, rootName(n.Fields["argument"])})
			}
		}
		for _, c := range n.Children {
			walk(c, scope, "", false, false)
		}
	}
	walk(root, 0, "", false, false)
	for _, m := range mutations {
		for at := m.scope; at >= 0; at = r.URLScopes[at].Parent {
			if b, ok := r.URLScopes[at].Bindings[m.name]; ok {
				if at == 0 && b.Evaluation != nil {
					b.Evaluation.Mutated = true
				}
				break
			}
		}
	}
	for i := range r.Clients {
		c := &r.Clients[i]
		c.URLScope = callScopes[callKey{c.Expression.Offset, c.Span.EndLine, c.Span.EndColumn}]
	}
	// Client-only expressions carry richer values; routes retain their old syntax.
	byOffset := map[callKey]*parse.Node{}
	var calls func(*parse.Node)
	calls = func(n *parse.Node) {
		if n == nil {
			return
		}
		if n.Kind == "call_expression" {
			byOffset[keyOfCall(n)] = n
		}
		for _, c := range n.Children {
			calls(c)
		}
	}
	calls(root)
	for i := range r.Clients {
		c := &r.Clients[i]
		if n := byOffset[callKey{c.Expression.Offset, c.Span.EndLine, c.Span.EndColumn}]; n != nil {
			e := m.url(n)
			e.Object = c.Expression.Object
			c.Expression = e
		}
	}
	if hasURL {
		// URL object uses are keyed by start byte: a chain's start keeps
		// its innermost call's scope, as before.
		innermost := map[int]callKey{}
		for k := range callScopes {
			if prev, ok := innermost[k.start]; !ok || k.endLine < prev.endLine || k.endLine == prev.endLine && k.endColumn < prev.endColumn {
				innermost[k.start] = k
			}
		}
		for start, k := range innermost {
			urlScopes[start] = callScopes[k]
		}
		cacheURLObjects(root, r, urlScopes)
	}
	compact := m.compactor()
	for _, scope := range r.URLScopes {
		for _, b := range scope.Bindings {
			if b.Evaluation != nil {
				b.Evaluation.Value = compact(b.Evaluation.Value)
				if h := b.Evaluation.Helper; h != nil {
					for i := range h.Parameters {
						h.Parameters[i].Default = compact(h.Parameters[i].Default)
					}
					compactURLStatements(h.Body, compact)
				}
			}
		}
	}
}

func compactURLCache(e *facts.RouteExpr) {
	if e.Kind != "meta_property" {
		e.Text = ""
	}
	if e.Object != nil {
		compactURLCache(e.Object)
	}
	for i := range e.Args {
		compactURLCache(&e.Args[i])
	}
	for i := range e.Parts {
		compactURLCache(&e.Parts[i])
	}
	for k, v := range e.Properties {
		compactURLCache(&v)
		e.Properties[k] = v
	}
}
func compactURLStatements(body []facts.URLStatement, compact func(facts.RouteExpr) facts.RouteExpr) {
	for i := range body {
		body[i].Expression = compact(body[i].Expression)
		compactURLStatements(body[i].Body, compact)
		compactURLStatements(body[i].Else, compact)
	}
}

// callKey identifies a call node by its start byte and end position (a
// cached ClientCall's Expression.Offset and Span end).
type callKey struct{ start, endLine, endColumn int }

func keyOfCall(n *parse.Node) callKey {
	return callKey{int(n.Start), int(n.EndRow) + 1, int(n.EndColumn)}
}

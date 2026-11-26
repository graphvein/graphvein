package typescript

import (
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func urlBinding(r *facts.RouteFacts, scope int, name string) (facts.RouteBinding, int, bool) {
	for scope >= 0 && scope < len(r.URLScopes) {
		if b, ok := r.URLScopes[scope].Bindings[name]; ok {
			return b, scope, true
		}
		scope = r.URLScopes[scope].Parent
	}
	return facts.RouteBinding{}, 0, false
}
func urlAuthorityProperty(name string) bool {
	return name == "protocol" || name == "host" || name == "hostname" || name == "port"
}

// cacheURLObjects records a per-use proof, so later property mutations cannot
// silently turn an earlier URL serialization into a different value on replay.
func cacheURLObjects(root *parse.Node, r *facts.RouteFacts, scopes map[int]int) {
	type declaration struct{ node, block *parse.Node }
	declarations := map[int]declaration{}
	var uses []*parse.Node
	var collect func(*parse.Node, *parse.Node)
	collect = func(n, block *parse.Node) {
		if n == nil {
			return
		}
		if n.Kind == "program" || n.Kind == "statement_block" {
			block = n
		}
		if n.Kind == "variable_declarator" {
			declarations[int(n.Start)] = declaration{n, block}
		}
		if n.Kind == "member_expression" && text(n.Fields["property"]) == "href" {
			uses = append(uses, n)
		}
		if n.Kind == "call_expression" {
			f := n.Fields["function"]
			if f != nil && f.Kind == "member_expression" && text(f.Fields["property"]) == "toString" {
				uses = append(uses, n)
			}
		}
		for _, child := range n.Children {
			collect(child, block)
		}
	}
	collect(root, nil)
	replacements := map[int]facts.RouteExpr{}
	for _, use := range uses {
		member := use
		if use.Kind == "call_expression" {
			member = use.Fields["function"]
			if args := use.Fields["arguments"]; args != nil && len(args.Children) != 0 {
				continue
			}
		}
		object := member.Fields["object"]
		if object == nil || object.Kind != "identifier" {
			continue
		}
		b, at, ok := urlBinding(r, scopes[int(use.Start)], object.Text)
		if !ok || b.Evaluation == nil {
			continue
		}
		v := b.Evaluation
		init := v.Value
		if init.Kind != "new_expression" || init.Object == nil || init.Object.Kind != "identifier" || init.Object.Name != "URL" {
			continue
		}
		proof := facts.RouteExpr{Kind: "url_object", Text: use.Text, Offset: int(use.Start), Args: init.Args, Operator: strconv.Itoa(at)}
		d := declarations[v.Offset]
		_, _, shadowed := urlBinding(r, at, "URL")
		proof.Invalid = !v.Constant || !v.Single || shadowed || d.node == nil || d.block == nil || d.node.End > use.Start
		assigned := map[string]bool{}
		if !proof.Invalid {
			var refs func(*parse.Node, *parse.Node, *parse.Node, *parse.Node)
			refs = func(n, parent, grand, statement *parse.Node) {
				if n == nil || n.End <= d.node.End || n.Start >= use.Start {
					return
				}
				if n == d.block {
					for _, s := range n.Children {
						refs(s, n, nil, s)
					}
					return
				}
				if n.Kind == "identifier" && n.Text == object.Text {
					allowed := parent != nil && parent.Kind == "member_expression" && parent.Fields["object"] == n && grand != nil && grand.Kind == "assignment_expression" && grand.Fields["left"] == parent && statement.Kind == "expression_statement" && len(statement.Children) == 1 && statement.Children[0] == grand && urlAuthorityProperty(text(parent.Fields["property"]))
					if allowed {
						for _, t := range grand.Tokens {
							if t != "=" {
								allowed = false
							}
						}
					}
					if !allowed {
						proof.Invalid = true
					} else {
						property := text(parent.Fields["property"])
						assigned[property] = true
						proof.Keywords = append(proof.Keywords, facts.RouteKeyword{Name: property, Value: authoritySource(d.block, grand.Fields["right"], property)})
					}
				}
				for _, child := range n.Children {
					refs(child, n, parent, statement)
				}
			}
			refs(d.block, nil, nil, nil)
		}
		for _, name := range []string{"protocol", "host", "hostname", "port"} {
			if assigned[name] {
				proof.Name += name + " "
			}
		}
		replacements[int(use.Start)] = proof
	}
	var rewrite func(*facts.RouteExpr)
	rewrite = func(e *facts.RouteExpr) {
		if replacement, ok := replacements[e.Offset]; ok && (e.Kind == "call_expression" || e.Kind == "member_expression") {
			*e = replacement
		}
		if e.Object != nil {
			rewrite(e.Object)
		}
		for i := range e.Args {
			rewrite(&e.Args[i])
		}
		for i := range e.Parts {
			rewrite(&e.Parts[i])
		}
		for key, v := range e.Properties {
			rewrite(&v)
			e.Properties[key] = v
		}
		for i := range e.Keywords {
			rewrite(&e.Keywords[i].Value)
		}
	}
	var statements func([]facts.URLStatement)
	statements = func(body []facts.URLStatement) {
		for i := range body {
			rewrite(&body[i].Expression)
			statements(body[i].Body)
			statements(body[i].Else)
		}
	}
	for i := range r.Clients {
		rewrite(&r.Clients[i].Expression)
	}
	for i := range r.URLScopes {
		for name, b := range r.URLScopes[i].Bindings {
			rewrite(&b.Value)
			if b.Evaluation != nil {
				rewrite(&b.Evaluation.Value)
				if h := b.Evaluation.Helper; h != nil {
					statements(h.Body)
				}
			}
			r.URLScopes[i].Bindings[name] = b
		}
	}
}

// orvalTransportSyntax retains only a candidate's small prefix to its fetch.
// It does not make an async function eligible for general helper evaluation.
func orvalTransportSyntax(n *parse.Node, identity string, parameters []facts.ClientParameter, routes *facts.RouteFacts, out *Facts) facts.RouteExpr {
	if identity == "" {
		return facts.RouteExpr{}
	}
	body := n.Fields["body"]
	if body == nil || !strings.Contains(body.Text, "fetch") {
		return facts.RouteExpr{}
	}
	var sends []*parse.Node
	var visit func(*parse.Node)
	visit = func(at *parse.Node) {
		if at == nil {
			return
		}
		if at.Kind == "call_expression" {
			if orvalGlobalFetch(at, routes, out) {
				sends = append(sends, at)
			}
		}
		for _, c := range at.Children {
			visit(c)
		}
	}
	visit(body)
	if len(sends) == 0 {
		return facts.RouteExpr{}
	}
	p := facts.RouteExpr{Kind: "orval_transport", Text: "transport prefix is not proven", Invalid: true}
	if len(sends) != 1 || len(parameters) < 2 || parameters[0].Name == "" || parameters[1].Name == "" {
		return p
	}
	formal := n.Fields["parameters"]
	if formal == nil {
		return p
	}
	plain := 0
	for _, item := range formal.Children {
		if item.Kind == "comment" {
			continue
		}
		name := item.Fields["pattern"]
		if name == nil {
			name = item.Fields["name"]
		}
		if item.Kind == "identifier" {
			name = item
		}
		if name == nil || name.Kind != "identifier" {
			return p
		}
		plain++
		if plain == 2 {
			break
		}
	}
	if plain != 2 {
		return p
	}
	send := sends[0]
	s := orvalSendSpan(send)
	p.Span = &s
	p.Name = parameters[0].Name
	p.Operator = parameters[1].Name
	if args := send.Fields["arguments"]; args != nil {
		for _, a := range args.Children {
			if a.Kind != "comment" {
				p.Args = append(p.Args, urlExpr(a))
			}
		}
	}
	valid := len(p.Args) >= 1
	urls := map[string]bool{}
	statements := body.Children
	if body.Kind != "statement_block" {
		at := body
		if at.Kind == "await_expression" && len(at.Children) == 1 {
			at = at.Children[0]
		}
		valid = valid && at == send
		statements = nil
	}
	for _, statement := range statements {
		if statement.Kind == "comment" {
			continue
		}
		if statement.Start <= send.Start && statement.End >= send.End {
			// The send itself must be a top-level return/expression or const
			// initializer, not a nested callback, conditional or argument.
			at := statement
			if at.Kind == "lexical_declaration" {
				constant := false
				for _, t := range at.Tokens {
					constant = constant || t == "const"
				}
				valid = valid && constant
				var value *parse.Node
				for _, c := range at.Children {
					if c.Kind == "variable_declarator" && c.Start <= send.Start && c.End >= send.End {
						value = c.Fields["value"]
					}
				}
				at = value
			} else if at.Kind == "return_statement" || at.Kind == "expression_statement" {
				if len(at.Children) == 1 {
					at = at.Children[0]
				} else {
					at = nil
				}
			} else {
				at = nil
			}
			if at != nil && at.Kind == "await_expression" && len(at.Children) == 1 {
				at = at.Children[0]
			}
			valid = valid && at == send
			break
		}
		if statement.End > send.Start {
			break
		}
		switch statement.Kind {
		case "lexical_declaration":
			constant := false
			for _, t := range statement.Tokens {
				constant = constant || t == "const"
			}
			valid = valid && constant
			for _, c := range statement.Children {
				if c.Kind == "variable_declarator" {
					v := c.Fields["value"]
					if v != nil && v.Kind == "new_expression" && text(v.Fields["constructor"]) == "URL" {
						urls[text(c.Fields["name"])] = true
					}
				}
			}
		case "expression_statement":
			if len(statement.Children) != 1 {
				valid = false
				continue
			}
			a := statement.Children[0]
			left := a.Fields["left"]
			if a.Kind != "assignment_expression" || left == nil || left.Kind != "member_expression" || !urls[text(left.Fields["object"])] || !urlAuthorityProperty(text(left.Fields["property"])) {
				valid = false
			}
		default:
			valid = false
		}
	}
	var writes func(*parse.Node, *parse.Node)
	writes = func(at, parent *parse.Node) {
		if at == nil {
			return
		}
		if at.Kind == "identifier" && at.Text == p.Operator && at.Start < send.Start && (parent == nil || parent.Kind != "member_expression" || parent.Fields["object"] != at) {
			valid = false
			p.Text = "options escapes before fetch"
		}
		if strings.Contains(at.Kind, "assignment_expression") || at.Kind == "update_expression" {
			left := at.Fields["left"]
			if left == nil {
				left = at.Fields["argument"]
			}
			for left != nil && (left.Kind == "member_expression" || left.Kind == "subscript_expression") {
				left = left.Fields["object"]
			}
			if left != nil && (left.Text == p.Name || left.Text == p.Operator) {
				valid = false
				p.Text = "mutator parameter is reassigned or mutated"
			}
		}
		for _, c := range at.Children {
			writes(c, at)
		}
	}
	writes(body, nil)
	p.Invalid = !valid
	if valid {
		p.Text = ""
	}
	return p
}

func orvalSendSpan(n *parse.Node) contract.Span {
	s := span(n)
	if f := n.Fields["function"]; f != nil && f.Kind == "await_expression" && len(f.Children) == 1 {
		s.StartLine = int(f.Children[0].Row) + 1
		s.StartColumn = int(f.Children[0].Column)
	}
	return s
}

func orvalGlobalFetch(n *parse.Node, r *facts.RouteFacts, out *Facts) bool {
	f := n.Fields["function"]
	if f != nil && f.Kind == "await_expression" && len(f.Children) == 1 {
		f = f.Children[0]
	}
	e := routeExpr(f)
	name := ""
	if e.Kind == "identifier" && e.Name == "fetch" {
		name = "fetch"
	}
	if e.Kind == "member_expression" && e.Name == "fetch" && e.Object != nil && e.Object.Kind == "identifier" {
		if e.Object.Name == "window" || e.Object.Name == "globalThis" || e.Object.Name == "self" {
			name = e.Object.Name
		}
	}
	if name == "" {
		return false
	}
	s := orvalSendSpan(n)
	for _, c := range r.Clients {
		if c.Span != s || c.Expression.Kind != "call_expression" {
			continue
		}
		for scope := c.Scope; scope >= 0 && scope < len(r.Scopes); scope = r.Scopes[scope].Parent {
			if _, bound := r.Scopes[scope].Bindings[name]; bound {
				return false
			}
		}
		if name == "fetch" {
			for _, node := range out.Nodes {
				if node.QualifiedName == "fetch" {
					return false
				}
			}
		}
		return true
	}
	return false
}

// authoritySource returns the argument E of `const A = new URL(E)` when
// right is `A.<property>` (the same authority property it is assigned to)
// and A is declared in block, before right, with exactly that one
// argument, and every reference to A in block is such a read on the right
// of an assignment. Anything else is unknown. The clients extractor uses
// E as the base expression of an assignment-derived origin
// (docs/extract/clients.md "Build-time constant bases").
func authoritySource(block, right *parse.Node, property string) facts.RouteExpr {
	unknown := facts.RouteExpr{Kind: "unknown"}
	if block == nil || right == nil || right.Kind != "member_expression" || text(right.Fields["property"]) != property || strings.Contains(right.Text, "?.") {
		return unknown
	}
	object := right.Fields["object"]
	if object == nil || object.Kind != "identifier" {
		return unknown
	}
	name := object.Text
	var declarator, argument *parse.Node
	for _, statement := range block.Children {
		if statement.Kind != "lexical_declaration" || !hasToken(statement, "const") {
			continue
		}
		var declarators []*parse.Node
		for _, c := range statement.Children {
			if c.Kind == "variable_declarator" {
				declarators = append(declarators, c)
			}
		}
		if len(declarators) != 1 || text(declarators[0].Fields["name"]) != name {
			continue
		}
		if declarator != nil || statement.End > right.Start {
			return unknown
		}
		declarator = declarators[0]
		value := declarator.Fields["value"]
		if value == nil || value.Kind != "new_expression" || text(value.Fields["constructor"]) != "URL" {
			return unknown
		}
		var args []*parse.Node
		if list := value.Fields["arguments"]; list != nil {
			for _, a := range list.Children {
				if a.Kind != "comment" {
					args = append(args, a)
				}
			}
		}
		if len(args) != 1 {
			return unknown
		}
		argument = args[0]
	}
	if declarator == nil {
		return unknown
	}
	valid := true
	var refs func(n, parent, grand *parse.Node)
	refs = func(n, parent, grand *parse.Node) {
		if !valid {
			return
		}
		if n.Kind == "shorthand_property_identifier" && n.Text == name {
			valid = false
			return
		}
		if n.Kind == "identifier" && n.Text == name && n != declarator.Fields["name"] {
			read := parent != nil && parent.Kind == "member_expression" && parent.Fields["object"] == n && urlAuthorityProperty(text(parent.Fields["property"])) && !strings.Contains(parent.Text, "?.")
			valid = read && grand != nil && grand.Kind == "assignment_expression" && grand.Fields["right"] == parent
			return
		}
		for _, c := range n.Children {
			refs(c, n, parent)
		}
	}
	refs(block, nil, nil)
	if !valid {
		return unknown
	}
	return urlExpr(argument)
}

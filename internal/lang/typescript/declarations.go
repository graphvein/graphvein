package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// DeclarationSyntax preserves Angular-specific syntax without widening URL facts.
type DeclarationSyntax struct {
	Version    int                    `json:"version"`
	Decorators []DeclarationDecorator `json:"decorators"`
	Constants  []DeclarationConstant  `json:"constants"`
	Mutations  []DeclarationMutation  `json:"mutations"`
}

// DeclarationDecorator keeps the original argument wrappers and lexical scopes.
type DeclarationDecorator struct {
	Offset, Scope, URLScope int
	Arguments               []DeclarationExpr
}

// DeclarationExpr retains ordered syntax and evidence for scope-array leaves.
type DeclarationExpr struct {
	Kind, Text, Name string
	Span             contract.Span
	Value            facts.RouteExpr
	Object           *DeclarationExpr `json:",omitempty"`
	Parts            []DeclarationExpr
	Args             []DeclarationExpr
	Entries          []DeclarationProperty
	Names            []string `json:",omitempty"`
}

// DeclarationProperty distinguishes own assignments from uncertain object syntax.
type DeclarationProperty struct {
	Kind, Key string
	Value     DeclarationExpr
}

// DeclarationConstant records only module-level single-declarator const arrays.
type DeclarationConstant struct {
	Name  string
	Value DeclarationExpr
}

// DeclarationMutation keeps receiver proof separate from existing mutation rules.
type DeclarationMutation struct {
	Scope int
	Value facts.RouteExpr
}

// Valid rejects old or truncated additive payloads on replay.
func (s *DeclarationSyntax) Valid() bool {
	if s == nil || s.Version != 1 || s.Decorators == nil || s.Constants == nil || s.Mutations == nil {
		return false
	}
	for _, d := range s.Decorators {
		if d.Arguments == nil || d.Scope < 0 || d.URLScope < 0 {
			return false
		}
		for _, e := range d.Arguments {
			if !e.valid() {
				return false
			}
		}
	}
	for _, c := range s.Constants {
		if c.Name == "" || !c.Value.valid() || c.Value.Kind != "array" {
			return false
		}
	}
	return true
}

func (e DeclarationExpr) valid() bool {
	if e.Kind == "" || e.Span.StartLine < 1 || e.Span.EndLine < e.Span.StartLine {
		return false
	}
	switch e.Kind {
	case "array", "spread_element", "parenthesized_expression", "as_expression", "satisfies_expression", "type_assertion", "non_null_expression":
		if e.Parts == nil {
			return false
		}
	case "call_expression":
		if e.Object == nil || e.Args == nil {
			return false
		}
	case "object":
		if e.Entries == nil {
			return false
		}
	case "arrow_function", "function_expression":
		// An unsupported body legitimately has no retained return expression.
	default:
		if e.Value.Kind == "" {
			return false
		}
	}
	if e.Object != nil && !e.Object.valid() {
		return false
	}
	for _, p := range e.Parts {
		if !p.valid() {
			return false
		}
	}
	for _, a := range e.Args {
		if !a.valid() {
			return false
		}
	}
	for _, p := range e.Entries {
		if (p.Kind == "pair" || p.Kind == "shorthand_property_identifier") && !p.Value.valid() {
			return false
		}
	}
	return true
}

// Matches rejects incomplete decorator lists even when the remaining entries are valid.
func (s *DeclarationSyntax) Matches(t *TemplateSyntax) bool {
	if !s.Valid() || t == nil || len(s.Decorators) != len(t.Decorators) {
		return false
	}
	byOffset := map[int]DeclarationDecorator{}
	for _, d := range s.Decorators {
		byOffset[d.Offset] = d
	}
	for _, d := range t.Decorators {
		raw, ok := byOffset[d.Callee.Offset]
		if !ok || len(raw.Arguments) != d.Arguments {
			return false
		}
	}
	return true
}

func declarationExpr(n *parse.Node) DeclarationExpr {
	if n == nil {
		return DeclarationExpr{}
	}
	e := DeclarationExpr{Kind: n.Kind, Span: span(n)}
	switch n.Kind {
	case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
		for _, c := range n.Children {
			if !strings.Contains(c.Kind, "type") && c.Kind != "comment" {
				e.Parts = []DeclarationExpr{declarationExpr(c)}
				break
			}
		}
	case "array", "spread_element":
		e.Parts = []DeclarationExpr{}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Parts = append(e.Parts, declarationExpr(c))
			}
		}
	case "object":
		e.Entries = []DeclarationProperty{}
		for _, p := range n.Children {
			if p.Kind == "comment" {
				continue
			}
			entry := DeclarationProperty{Kind: p.Kind}
			key, value := p.Fields["key"], p.Fields["value"]
			if p.Kind == "shorthand_property_identifier" {
				key = p
				clone := *p
				clone.Kind = "identifier"
				value = &clone
			}
			if key != nil {
				entry.Key = key.Text
				if key.Kind == "string" {
					entry.Key, _ = jsString(key.Text)
				} else if key.Kind != "identifier" && key.Kind != "property_identifier" && key.Kind != "shorthand_property_identifier" && key.Kind != "number" {
					entry.Kind = "computed"
				}
			}
			entry.Value = declarationExpr(value)
			e.Entries = append(e.Entries, entry)
		}
	case "call_expression":
		e.Value = urlExpr(n)
		e.Args = []DeclarationExpr{}
		v := declarationExpr(n.Fields["function"])
		e.Object = &v
		if a := n.Fields["arguments"]; a != nil {
			for _, c := range a.Children {
				if c.Kind != "comment" {
					e.Args = append(e.Args, declarationExpr(c))
				}
			}
		}
	case "arrow_function", "function_expression":
		// Retain runtime parameter/name shadows without assigning resolver proof.
		names := map[string]bool{}
		collectParameterNames(n.Fields["parameters"], names)
		collectParameterNames(n.Fields["parameter"], names)
		collectParameterNames(n.Fields["name"], names)
		for name := range names {
			e.Names = append(e.Names, name)
		}
		sort.Strings(e.Names)
		for _, token := range n.Tokens {
			if token == "async" || token == "*" {
				e.Name = "async_or_generator"
			}
		}
		body := n.Fields["body"]
		if body != nil && body.Kind == "statement_block" {
			var statements []*parse.Node
			for _, c := range body.Children {
				if c.Kind != "comment" {
					statements = append(statements, c)
				}
			}
			body = nil
			if len(statements) == 1 && statements[0].Kind == "return_statement" {
				for _, c := range statements[0].Children {
					if c.Kind != "comment" {
						body = c
						break
					}
				}
			}
		}
		if body != nil {
			e.Parts = []DeclarationExpr{declarationExpr(body)}
		}
	default:
		e.Value = urlExpr(n)
	}
	return e
}

func collectParameterNames(n *parse.Node, names map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind == "identifier" || n.Kind == "shorthand_property_identifier_pattern" {
		names[n.Text] = true
		return
	}
	if strings.Contains(n.Kind, "type") || n.Kind == "property_identifier" {
		return
	}
	for _, c := range n.Children {
		collectParameterNames(c, names)
	}
}

// CaptureDeclarations adds source evidence while preserving all existing facts.
func CaptureDeclarations(root *parse.Node, f Facts, templates TemplateSyntax) DeclarationSyntax {
	s := DeclarationSyntax{Version: 1, Decorators: []DeclarationDecorator{}, Constants: []DeclarationConstant{}, Mutations: []DeclarationMutation{}}
	if f.Routes == nil {
		return s
	}
	mutationRoots := map[string]bool{}
	for _, imp := range f.Imports {
		for name := range imp.Bindings {
			mutationRoots[name] = true
		}
	}
	// Collect before walking mutations so writes before a declaration are kept.
	var collectRoots func(*parse.Node)
	collectRoots = func(n *parse.Node) {
		if n == nil {
			return
		}
		if n.Kind == "program" || n.Kind == "export_statement" {
			for _, child := range n.Children {
				collectRoots(child)
			}
			return
		}
		if n.Kind != "lexical_declaration" {
			return
		}
		constant := false
		for _, token := range n.Tokens {
			constant = constant || token == "const"
		}
		var declarators []*parse.Node
		for _, child := range n.Children {
			if child.Kind == "variable_declarator" {
				declarators = append(declarators, child)
			}
		}
		if constant && len(declarators) == 1 {
			if name := declarators[0].Fields["name"]; name != nil && name.Kind == "identifier" {
				mutationRoots[name.Text] = true
			}
		}
	}
	collectRoots(root)
	canMutate := func(n *parse.Node, shadowed map[string]bool) bool {
		for n != nil && (n.Kind == "member_expression" || n.Kind == "subscript_expression") {
			n = n.Fields["object"]
		}
		return n != nil && n.Kind == "identifier" && mutationRoots[n.Text] && !shadowed[n.Text]
	}
	classCalls := map[int]bool{}
	for _, d := range templates.Decorators {
		classCalls[d.Callee.Offset] = true
	}
	calls := map[int]facts.ClientCall{}
	mutationScopes := map[contract.Span]int{}
	for _, c := range f.Routes.Clients {
		if c.Expression.Kind == "call_expression" {
			calls[c.Expression.Offset] = c
		}
		if c.Expression.Kind == "config_mutation" {
			mutationScopes[c.Span] = c.Scope
		}
	}
	// Existing config-mutation records retain exact lexical scopes. Direct
	// identifier writes use the independent shadow check below, since the old
	// extractor deliberately erases a reassigned binding's value proof.
	scopeAt := func(n *parse.Node) int {
		return mutationScopes[span(n)]
	}
	var walk func(*parse.Node, bool, map[string]bool)
	walk = func(n *parse.Node, module bool, shadowed map[string]bool) {
		if n == nil {
			return
		}
		if declarationScope(n.Kind) {
			copy := map[string]bool{}
			for name := range shadowed {
				copy[name] = true
			}
			collectDeclarationShadows(n, copy)
			shadowed = copy
		}
		if n.Kind == "decorator" && len(n.Children) > 0 {
			call := n.Children[0]
			if call.Kind == "call_expression" && classCalls[int(call.Start)] {
				c := calls[int(call.Start)]
				d := DeclarationDecorator{Offset: int(call.Start), Scope: c.Scope, URLScope: c.URLScope, Arguments: []DeclarationExpr{}}
				if a := call.Fields["arguments"]; a != nil {
					for _, v := range a.Children {
						if v.Kind != "comment" {
							d.Arguments = append(d.Arguments, declarationExpr(v))
						}
					}
				}
				s.Decorators = append(s.Decorators, d)
			}
		}
		if module && n.Kind == "lexical_declaration" {
			constant, count := false, 0
			for _, t := range n.Tokens {
				constant = constant || t == "const"
			}
			for _, c := range n.Children {
				if c.Kind == "variable_declarator" {
					count++
				}
			}
			if constant && count == 1 {
				for _, c := range n.Children {
					if c.Kind == "variable_declarator" && c.Fields["name"] != nil && c.Fields["name"].Kind == "identifier" {
						v := c.Fields["value"]
						for v != nil && (v.Kind == "parenthesized_expression" || v.Kind == "as_expression") && len(v.Children) > 0 {
							var next *parse.Node
							for _, child := range v.Children {
								if child.Kind != "comment" && !strings.Contains(child.Kind, "type") {
									next = child
									break
								}
							}
							v = next
						}
						if v != nil && v.Kind == "array" {
							s.Constants = append(s.Constants, DeclarationConstant{Name: c.Fields["name"].Text, Value: declarationExpr(v)})
						}
					}
				}
			}
		}
		switch n.Kind {
		case "assignment_expression", "augmented_assignment_expression", "update_expression", "unary_expression":
			v := n.Fields["left"]
			if v == nil {
				v = n.Fields["argument"]
			}
			if n.Kind != "unary_expression" || strings.HasPrefix(n.Text, "delete") {
				if canMutate(v, shadowed) {
					s.Mutations = append(s.Mutations, DeclarationMutation{Scope: scopeAt(n), Value: urlExpr(v)})
				}
			}
		case "call_expression":
			callee := n.Fields["function"]
			if callee != nil && (callee.Kind == "member_expression" || callee.Kind == "subscript_expression") && canMutate(callee.Fields["object"], shadowed) {
				c := calls[int(n.Start)]
				s.Mutations = append(s.Mutations, DeclarationMutation{Scope: c.Scope, Value: urlExpr(callee.Fields["object"])})
			}
		}
		childModule := module && (n.Kind == "program" || n.Kind == "export_statement")
		for _, c := range n.Children {
			walk(c, childModule, shadowed)
		}
	}
	walk(root, true, map[string]bool{})
	return s
}

func declarationScope(kind string) bool {
	switch kind {
	case "function_declaration", "function_expression", "arrow_function", "method_definition", "statement_block", "for_statement", "for_in_statement", "catch_clause":
		return true
	}
	return false
}

func collectDeclarationShadows(root *parse.Node, names map[string]bool) {
	var pattern func(*parse.Node)
	pattern = func(n *parse.Node) {
		if n == nil {
			return
		}
		if n.Kind == "identifier" || n.Kind == "shorthand_property_identifier_pattern" {
			names[n.Text] = true
			return
		}
		if n.Kind == "type_annotation" || n.Kind == "property_identifier" {
			return
		}
		for _, c := range n.Children {
			pattern(c)
		}
	}
	pattern(root.Fields["parameters"])
	pattern(root.Fields["parameter"])
	var visit func(*parse.Node, bool)
	visit = func(n *parse.Node, nested bool) {
		if n == nil {
			return
		}
		if n != root && (n.Kind == "class_declaration" || n.Kind == "abstract_class_declaration" || n.Kind == "class" || n.Kind == "function_declaration") {
			if !nested {
				pattern(n.Fields["name"])
			}
			return
		}
		if n != root && declarationScope(n.Kind) {
			// var is function-scoped, including declarations in nested blocks.
			if n.Kind != "statement_block" && n.Kind != "for_statement" && n.Kind != "for_in_statement" {
				return
			}
			nested = true
		}
		if n.Kind == "variable_declaration" || !nested && n.Kind == "lexical_declaration" {
			for _, c := range n.Children {
				if c.Kind == "variable_declarator" {
					pattern(c.Fields["name"])
				}
			}
		}
		if !nested && (n.Kind == "class_declaration" || n.Kind == "function_declaration") {
			pattern(n.Fields["name"])
		}
		for _, c := range n.Children {
			visit(c, nested)
		}
	}
	if body := root.Fields["body"]; body != nil {
		visit(body, false)
	} else {
		visit(root, false)
	}
}

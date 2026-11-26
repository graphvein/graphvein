package typescript

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// InjectionSyntax preserves DI syntax independently of receiver and URL proofs.
type InjectionSyntax struct {
	Version         int                       `json:"version"`
	Classes         []InjectionClass          `json:"classes"`
	Sites           []InjectionSite           `json:"sites"`
	Constants       []InjectionConstant       `json:"constants"`
	TypeImports     []string                  `json:"type_imports"`
	ParameterScopes []InjectionParameterScope `json:"parameter_scopes"`
}

// InjectionParameterScope preserves binding patterns without default expressions.
// Existing receiver scopes intentionally retain their historical interpretation.
type InjectionParameterScope struct {
	Scope int
	Names []string
}

// InjectionClass distinguishes inherited factories from empty implementations.
type InjectionClass struct {
	Identity             string
	Extends, Constructor bool
	Parameters           []InjectionParameter
	TypeParameters       []string
}

// InjectionParameter retains unavailable types rather than manufacturing values.
type InjectionParameter struct {
	Name       string
	Span       contract.Span
	Type       InjectionType
	Decorators []DeclarationExpr
}

// InjectionType retains the original type grammar, including nullable unions.
type InjectionType struct {
	Kind, Text string
	Children   []InjectionType
}

// InjectionSite records the own injection context and existing lexical scope.
type InjectionSite struct {
	Source, Context string
	Scope           int
	Call            DeclarationExpr
	Shadows         []string
}

// InjectionConstant retains only module-level single-declarator const syntax.
type InjectionConstant struct {
	Name  string
	Value DeclarationExpr
}

// Valid makes missing or truncated additive syntax a cache miss.
func (s *InjectionSyntax) Valid() bool {
	if s == nil || s.Version != 2 || s.Classes == nil || s.Sites == nil || s.Constants == nil || s.TypeImports == nil || s.ParameterScopes == nil {
		return false
	}
	for _, c := range s.Classes {
		if c.Identity == "" || c.Parameters == nil || c.TypeParameters == nil {
			return false
		}
		for _, p := range c.Parameters {
			if p.Name == "" || p.Span.StartLine < 1 || p.Decorators == nil || !p.Type.valid() {
				return false
			}
			for _, d := range p.Decorators {
				if !validInjectionExpr(d) {
					return false
				}
			}
		}
	}
	for _, c := range s.Constants {
		if c.Name == "" || !validInjectionExpr(c.Value) {
			return false
		}
	}
	for _, c := range s.Sites {
		if c.Scope < 0 || c.Source == "" || c.Shadows == nil || c.Call.Kind != "call_expression" || len(c.Call.Args) > 1 || (c.Context != "field" && c.Context != "constructor" && c.Context != "function") || !validInjectionExpr(c.Call) {
			return false
		}
	}
	for _, p := range s.ParameterScopes {
		if p.Scope < 0 || p.Names == nil {
			return false
		}
	}
	return true
}

func validInjectionExpr(e DeclarationExpr) bool {
	if !e.valid() {
		return false
	}
	if e.Kind == "new_expression" && (e.Object == nil || e.Args == nil) {
		return false
	}
	if e.Object != nil && !validInjectionExpr(*e.Object) {
		return false
	}
	for _, p := range e.Parts {
		if !validInjectionExpr(p) {
			return false
		}
	}
	for _, a := range e.Args {
		if !validInjectionExpr(a) {
			return false
		}
	}
	return true
}

func (t InjectionType) valid() bool {
	if t.Kind == "" || t.Children == nil || t.Kind != "missing" && t.Text == "" {
		return false
	}
	for _, c := range t.Children {
		if !c.valid() {
			return false
		}
	}
	return true
}

func injectionType(n *parse.Node) InjectionType {
	t := InjectionType{Kind: "missing", Children: []InjectionType{}}
	if n == nil {
		return t
	}
	t.Kind, t.Text = n.Kind, n.Text
	for _, c := range n.Children {
		if c.Kind != "comment" {
			t.Children = append(t.Children, injectionType(c))
		}
	}
	return t
}

func injectionExpr(n *parse.Node) DeclarationExpr {
	e := declarationExpr(n)
	if n == nil {
		return e
	}
	e.Text = n.Text
	if n.Kind == "new_expression" {
		v := injectionExpr(n.Fields["constructor"])
		e.Object = &v
		e.Args = []DeclarationExpr{}
		if a := n.Fields["arguments"]; a != nil {
			for _, c := range a.Children {
				if c.Kind != "comment" {
					e.Args = append(e.Args, injectionExpr(c))
				}
			}
		}
	}
	if n.Kind == "call_expression" {
		e.Args = []DeclarationExpr{}
		if a := n.Fields["arguments"]; a != nil {
			for _, c := range a.Children {
				if c.Kind != "comment" {
					e.Args = append(e.Args, injectionExpr(c))
				}
			}
		}
	}
	if (n.Kind == "arrow_function" || n.Kind == "function_expression") && len(e.Parts) == 1 {
		body := n.Fields["body"]
		if body != nil && body.Kind == "statement_block" {
			for _, statement := range body.Children {
				if statement.Kind == "return_statement" {
					for _, value := range statement.Children {
						if value.Kind != "comment" {
							body = value
							break
						}
					}
					break
				}
			}
		}
		e.Parts[0] = injectionExpr(body)
	}
	if n.Kind == "array" || n.Kind == "spread_element" {
		e.Parts = []DeclarationExpr{}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Parts = append(e.Parts, injectionExpr(c))
			}
		}
	}
	if len(e.Parts) == 1 && (n.Kind == "parenthesized_expression" || n.Kind == "as_expression" || n.Kind == "type_assertion" || n.Kind == "satisfies_expression" || n.Kind == "non_null_expression") {
		for _, c := range n.Children {
			if c.Kind != "comment" && !strings.Contains(c.Kind, "type") {
				e.Parts[0] = injectionExpr(c)
				break
			}
		}
	}
	return e
}

// CaptureInjections records source syntax while leaving existing cache fields intact.
func CaptureInjections(root *parse.Node, f Facts) InjectionSyntax {
	s := InjectionSyntax{Version: 2, Classes: []InjectionClass{}, Sites: []InjectionSite{}, Constants: []InjectionConstant{}, TypeImports: []string{}, ParameterScopes: []InjectionParameterScope{}}
	parents := map[*parse.Node]*parse.Node{}
	calls := map[int]facts.ClientCall{}
	if f.Routes != nil {
		for _, c := range f.Routes.Clients {
			calls[c.Expression.Offset] = c
		}
	}
	owner := func(n *parse.Node, kind string) string {
		for _, v := range f.Nodes {
			if v.Kind == kind && v.Span != nil && v.Span.EndLine == int(n.EndRow)+1 && v.Span.EndColumn == int(n.EndColumn) {
				return v.Identity
			}
		}
		return ""
	}
	type scopeRange struct {
		start, end uint32
		scope      int
	}
	ranges := []scopeRange{}
	type typeRange struct {
		start, end uint32
		names      []string
	}
	typeRanges := []typeRange{}
	usedScopes := map[int]bool{}
	useEnclosingScopes := func(n *parse.Node) {
		for _, r := range ranges {
			if r.start <= n.Start && r.end >= n.End {
				usedScopes[r.scope] = true
			}
		}
	}
	var parameterScopes func(*parse.Node)
	parameterScopes = func(n *parse.Node) {
		if n == nil {
			return
		}
		if ps := n.Fields["type_parameters"]; ps != nil {
			r := typeRange{start: n.Start, end: n.End}
			for _, p := range ps.Children {
				if name := p.Fields["name"]; name != nil {
					r.names = append(r.names, name.Text)
				}
			}
			typeRanges = append(typeRanges, r)
		}
		id := owner(n, "function")
		if n.Kind == "method_definition" {
			id = owner(n, "method")
		}
		if f.Routes != nil && (strings.Contains(n.Kind, "function") || n.Kind == "method_definition") {
			if scope, ok := f.Routes.ParameterScopes[id]; ok && id != "" {
				names := map[string]bool{}
				ps := n.Fields["parameters"]
				if ps == nil {
					ps = n.Fields["parameter"]
				}
				if ps != nil {
					items := ps.Children
					if ps.Kind == "identifier" {
						items = []*parse.Node{ps}
					}
					for _, p := range items {
						v := p.Fields["pattern"]
						if v == nil {
							v = p.Fields["name"]
						}
						if p.Kind == "identifier" {
							v = p
						}
						collectParameterNames(v, names)
					}
				}
				p := InjectionParameterScope{Scope: scope, Names: []string{}}
				for name := range names {
					p.Names = append(p.Names, name)
				}
				sort.Strings(p.Names)
				s.ParameterScopes = append(s.ParameterScopes, p)
				ranges = append(ranges, scopeRange{n.Start, n.End, scope})
			}
		}
		for _, c := range n.Children {
			parents[c] = n
			parameterScopes(c)
		}
	}
	parameterScopes(root)
	var imports func(*parse.Node, bool)
	imports = func(n *parse.Node, only bool) {
		for _, t := range n.Tokens {
			only = only || t == "type"
		}
		if only && n.Kind == "import_specifier" {
			name := text(n.Fields["alias"])
			if name == "" {
				name = text(n.Fields["name"])
			}
			s.TypeImports = append(s.TypeImports, name)
			return
		}
		if only && n.Kind == "identifier" {
			s.TypeImports = append(s.TypeImports, n.Text)
			return
		}
		for _, c := range n.Children {
			imports(c, only)
		}
	}
	var walk func(*parse.Node, string, string, bool, bool)
	walk = func(n *parse.Node, source, context string, inClass, module bool) {
		if n == nil {
			return
		}
		if n.Kind == "import_statement" {
			imports(n, false)
			return
		}
		if n.Kind == "class_declaration" || n.Kind == "abstract_class_declaration" || n.Kind == "class" {
			source = owner(n, "class")
			context = "class"
			inClass = true
			if source != "" && (n.Kind != "class" || module) {
				useEnclosingScopes(n)
				c := InjectionClass{Identity: source, Parameters: []InjectionParameter{}, TypeParameters: []string{}}
				for _, r := range typeRanges {
					if r.start < n.Start && r.end >= n.End {
						c.TypeParameters = append(c.TypeParameters, r.names...)
					}
				}
				for _, ch := range n.Children {
					if ch.Kind == "class_heritage" {
						for _, h := range ch.Children {
							c.Extends = c.Extends || h.Kind == "extends_clause"
						}
					}
					if ch.Kind == "type_parameters" {
						for _, p := range ch.Children {
							if name := p.Fields["name"]; name != nil {
								c.TypeParameters = append(c.TypeParameters, name.Text)
							}
						}
					}
				}
				if b := n.Fields["body"]; b != nil {
					for _, m := range b.Children {
						if text(m.Fields["name"]) != "constructor" || m.Fields["body"] == nil {
							continue
						}
						c.Constructor = true
						if ps := m.Fields["parameters"]; ps != nil {
							for _, p := range ps.Children {
								if p.Kind == "comment" {
									continue
								}
								name := text(p.Fields["pattern"])
								if name == "" {
									name = text(p.Fields["name"])
								}
								if name == "" {
									name = p.Text
								}
								v := InjectionParameter{Name: name, Span: span(p), Type: injectionType(p.Fields["type"]), Decorators: []DeclarationExpr{}}
								for _, d := range p.Children {
									if d.Kind == "decorator" && len(d.Children) > 0 {
										v.Decorators = append(v.Decorators, injectionExpr(d.Children[0]))
									}
								}
								c.Parameters = append(c.Parameters, v)
							}
						}
						break
					}
				}
				s.Classes = append(s.Classes, c)
			}
		}
		if n.Kind == "method_definition" || n.Kind == "method_signature" {
			if text(n.Fields["name"]) == "constructor" && n.Fields["body"] != nil {
				context = "constructor"
			} else {
				context = "method"
			}
		}
		if n.Kind == "public_field_definition" || n.Kind == "field_definition" {
			context = "field"
			for _, t := range n.Tokens {
				if t == "static" {
					context = "static_field"
				}
			}
			// Computed keys and decorators execute outside the instance initializer.
			walk(n.Fields["value"], source, context, inClass, false)
			return
		}
		if n.Kind == "function_declaration" || n.Kind == "arrow_function" || n.Kind == "function_expression" || n.Kind == "generator_function_declaration" || n.Kind == "generator_function" {
			nested := context == "function" || context == "nested"
			source = ""
			context = "nested"
			if !inClass && !nested {
				source = owner(n, "function")
				if source != "" {
					context = "function"
				}
			}
			// Only constructors admit parameter defaults as injection contexts.
			walk(n.Fields["body"], source, context, inClass, false)
			return
		}
		if n.Kind == "call_expression" && source != "" && (context == "field" || context == "constructor" || context == "function") {
			callee := n.Fields["function"]
			head := callee
			for head != nil && head.Kind == "member_expression" {
				head = head.Fields["object"]
			}
			// Keep only named inject imports and namespace.inject. External Angular
			// proof remains a link-time decision, including imports through barrels.
			candidate := false
			if head != nil && head.Kind == "identifier" {
				for _, imp := range f.Imports {
					name := imp.Bindings[head.Text]
					candidate = candidate || callee == head && name == "inject" || callee.Kind == "member_expression" && callee.Fields["object"] == head && text(callee.Fields["property"]) == "inject" && name == "*"
				}
			}
			if candidate {
				scope := calls[int(n.Start)].Scope
				if _, ok := calls[int(n.Start)]; !ok {
					for _, v := range ranges {
						if n.Start >= v.start && n.End <= v.end {
							scope = v.scope
						}
					}
				}
				call := DeclarationExpr{Kind: "call_expression", Span: span(n), Args: []DeclarationExpr{}}
				calleeExpr := injectionExpr(callee)
				call.Object = &calleeExpr
				if args := n.Fields["arguments"]; args != nil {
					for _, arg := range args.Children {
						if arg.Kind != "comment" {
							call.Args = append(call.Args, injectionExpr(arg))
							break
						}
					}
				}
				useEnclosingScopes(n)
				shadows := []string{}
				{
					names := map[string]bool{}
					for at := parents[n]; at != nil; at = parents[at] {
						if declarationScope(at.Kind) {
							// Body declarations include hoisted var, but parameter default
							// expressions must never be interpreted as binding patterns.
							if strings.Contains(at.Kind, "function") || at.Kind == "method_definition" {
								break
							}
							collectDeclarationShadows(at, names)
							// Class names use type_identifier in the TypeScript grammar;
							// the declaration mutation helper only collects value patterns.
							for _, child := range at.Children {
								if child.Kind == "class_declaration" || child.Kind == "abstract_class_declaration" {
									names[text(child.Fields["name"])] = true
								}
							}
							if at.Kind == "for_statement" || at.Kind == "for_in_statement" {
								// Include loop-header declarations as well as its body.
								copy := *at
								copy.Fields = nil
								collectDeclarationShadows(&copy, names)
							}
						}
					}
					// Keep only names that can affect this callee or token proof.
					var relevant func(DeclarationExpr)
					relevant = func(e DeclarationExpr) {
						if e.Kind == "member_expression" {
							value := e.Value
							for value.Object != nil {
								value = *value.Object
							}
							if names[value.Name] {
								shadows = append(shadows, value.Name)
								delete(names, value.Name)
							}
						}
						if e.Kind == "identifier" && names[e.Text] {
							shadows = append(shadows, e.Text)
							delete(names, e.Text)
						}
						if e.Object != nil {
							relevant(*e.Object)
						}
						for _, part := range e.Parts {
							relevant(part)
						}
						for _, arg := range e.Args {
							relevant(arg)
						}
					}
					relevant(call)
					sort.Strings(shadows)
				}
				s.Sites = append(s.Sites, InjectionSite{Source: source, Context: context, Scope: scope, Call: call, Shadows: shadows})
			}
		}
		if module && n.Kind == "lexical_declaration" {
			constant := false
			for _, t := range n.Tokens {
				constant = constant || t == "const"
			}
			ds := []*parse.Node{}
			for _, c := range n.Children {
				if c.Kind == "variable_declarator" {
					ds = append(ds, c)
				}
			}
			if constant && len(ds) == 1 && ds[0].Fields["value"] != nil && ds[0].Fields["name"] != nil && ds[0].Fields["name"].Kind == "identifier" {
				v := injectionExpr(ds[0].Fields["value"])
				bare := v
				for len(bare.Parts) == 1 && (bare.Kind == "parenthesized_expression" || bare.Kind == "as_expression" || bare.Kind == "type_assertion" || bare.Kind == "satisfies_expression" || bare.Kind == "non_null_expression") {
					bare = bare.Parts[0]
				}
				if bare.Kind == "new_expression" {
					s.Constants = append(s.Constants, InjectionConstant{Name: text(ds[0].Fields["name"]), Value: v})
				}
			}
		}
		nextModule := module && (n.Kind == "program" || n.Kind == "export_statement")
		for _, c := range n.Children {
			if c.Kind != "decorator" {
				walk(c, source, context, inClass, nextModule)
			}
		}
	}
	walk(root, "", "module", false, true)
	kept := []InjectionParameterScope{}
	for _, p := range s.ParameterScopes {
		if usedScopes[p.Scope] {
			kept = append(kept, p)
		}
	}
	s.ParameterScopes = kept
	return s
}

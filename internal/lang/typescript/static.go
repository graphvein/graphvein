package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Class declarations use the existing detached expression container: Name is
// the emitted identity and Properties contains only proven own static methods.
// This is cache syntax, not public node attributes or resolved cross-file data.
func staticClass(n *parse.Node, out *Facts) facts.RouteExpr {
	e := facts.RouteExpr{Kind: "static_class", Properties: map[string]facts.RouteExpr{}}
	if n.Kind != "class_declaration" && n.Kind != "abstract_class_declaration" {
		return e
	}
	for _, symbol := range out.Nodes {
		if symbol.Kind == "class" && symbol.Span != nil && symbol.Span.EndLine == span(n).EndLine && symbol.Span.EndColumn == span(n).EndColumn {
			e.Name = symbol.Identity
			break
		}
	}
	body := n.Fields["body"]
	if body == nil {
		return e
	}
	blocked := map[string]bool{}
	dynamicStatic := false
	var thisWrites func(*parse.Node)
	thisWrites = func(n *parse.Node) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "assignment_expression", "augmented_assignment_expression", "update_expression", "unary_expression":
			left := n.Fields["left"]
			if left == nil {
				left = n.Fields["argument"]
			}
			if n.Kind != "unary_expression" || strings.HasPrefix(n.Text, "delete") {
				if left != nil && left.Fields["object"] != nil && left.Fields["object"].Kind == "this" {
					if left.Kind == "member_expression" {
						blocked[text(left.Fields["property"])] = true
					} else if left.Kind == "subscript_expression" {
						dynamicStatic = true
					}
				}
			}
		case "call_expression":
			e := urlExpr(n)
			if e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil && e.Object.Object.Name == "Object" && len(e.Args) > 0 && e.Args[0].Kind == "this" {
				switch e.Object.Name {
				case "assign", "defineProperties":
					dynamicStatic = true
				case "defineProperty":
					if len(e.Args) > 1 && e.Args[1].Kind == "literal" {
						blocked[e.Args[1].Name] = true
					} else {
						dynamicStatic = true
					}
				}
			}
		}
		for _, c := range n.Children {
			thisWrites(c)
		}
	}
	for _, m := range body.Children {
		if m.Kind == "class_static_block" {
			thisWrites(m)
		}
		name := m.Fields["name"]
		if name == nil {
			continue
		}
		static, accessor := false, false
		for _, token := range m.Tokens {
			static = static || token == "static"
			accessor = accessor || token == "get" || token == "set"
		}
		if static {
			thisWrites(m)
		}
		member := name.Text
		if name.Kind == "string" {
			var ok bool
			member, ok = jsString(name.Text)
			if !ok {
				dynamicStatic = dynamicStatic || static
				continue
			}
		} else if name.Kind == "computed_property_name" {
			// An unknown constructor key could replace any otherwise eligible
			// method, even when it appears before that method in the body.
			dynamicStatic = dynamicStatic || static
			continue
		}
		// Signatures have separate overload identities; the body supplies the
		// ordinary target. Fields, accessors and instance implementations block it.
		if strings.Contains(m.Kind, "signature") {
			continue
		}
		if m.Kind != "method_definition" || !static || accessor || (name.Kind != "property_identifier" && name.Kind != "string") || m.Fields["body"] == nil {
			blocked[member] = true
			continue
		}
		for _, symbol := range out.Nodes {
			if symbol.Kind == "method" && symbol.Span != nil && symbol.Span.EndLine == span(m).EndLine && symbol.Span.EndColumn == span(m).EndColumn {
				if _, exists := e.Properties[member]; exists || len(out.Repeated[symbol.Identity]) > 1 {
					blocked[member] = true
				}
				e.Properties[member] = facts.RouteExpr{Kind: "static_method", Name: symbol.Identity}
				break
			}
		}
	}
	for name := range blocked {
		delete(e.Properties, name)
	}
	if dynamicStatic {
		e.Properties = nil
	}
	return e
}

// StaticCall supplies the additive call-expression path shared by ordinary calls
// and client wrappers. Value references keep the existing namespace-only rules.
func (r *ModuleResolver) StaticCall(file string, scope int, callee facts.RouteExpr) string {
	if callee.Kind != "member_expression" || callee.Object == nil || strings.HasPrefix(callee.Name, "#") {
		return ""
	}
	b := r.classBinding(file, scope, *callee.Object)
	if b.Identity == "" || b.Ambiguous {
		return ""
	}
	method := r.staticClasses[b.Identity].Properties[callee.Name].Name
	if method == "" || r.staticMutations[b.Identity+"\x00"+callee.Name] || r.staticMutations[b.Identity+"\x00*"] {
		return ""
	}
	return method
}

func (r *ModuleResolver) classBinding(file string, scope int, e facts.RouteExpr) ExportBinding {
	if e.Kind == "member_expression" && e.Object != nil && e.Object.Kind == "identifier" {
		b := r.classBinding(file, scope, *e.Object)
		if b.Namespace {
			return r.Member(b, e.Name)
		}
		return ExportBinding{}
	}
	if e.Kind != "identifier" {
		return ExportBinding{}
	}
	d := r.files[file].Routes
	if d == nil {
		return ExportBinding{}
	}
	for at := scope; at >= 0 && at < len(d.Scopes); at = d.Scopes[at].Parent {
		if b, exists := d.Scopes[at].Bindings[e.Name]; exists {
			if b.Value.Invalid {
				return ExportBinding{}
			}
			if b.Module != "" {
				return r.ImportBinding(file, b.Module, b.Export, "")
			}
			if b.Value.Kind == "static_class" {
				return ExportBinding{File: file, Identity: b.Value.Name}
			}
			return ExportBinding{}
		}
	}
	return ExportBinding{}
}

// Rebuild guards on every relink so mutations in another file also invalidate
// cached callers. Resolve receivers without consulting guards to avoid cycles.
func (r *ModuleResolver) prepareStaticCalls() {
	r.staticClasses = map[string]facts.RouteExpr{}
	r.staticMutations = map[string]bool{}
	for _, f := range r.files {
		if f.Routes == nil {
			continue
		}
		for _, scope := range f.Routes.Scopes {
			for _, b := range scope.Bindings {
				if b.Value.Kind == "static_class" && b.Value.Name != "" {
					r.staticClasses[b.Value.Name] = b.Value
				}
			}
		}
	}
	for file, f := range r.files {
		if f.Routes == nil {
			continue
		}
		for _, c := range f.Routes.Clients {
			e := c.Expression
			var receiver facts.RouteExpr
			name := "*"
			if e.Kind == "config_mutation" && len(e.Parts) == 1 {
				left := e.Parts[0]
				if left.Object == nil {
					continue
				}
				receiver = *left.Object
				if left.Kind == "member_expression" {
					name = left.Name
				}
			} else if e.Kind == "call_expression" && e.Object != nil && e.Object.Kind == "member_expression" && e.Object.Object != nil && e.Object.Object.Kind == "identifier" && e.Object.Object.Name == "Object" && len(e.Args) > 0 {
				switch e.Object.Name {
				case "assign", "defineProperty", "defineProperties":
					receiver = e.Args[0]
					if e.Object.Name == "defineProperty" && len(e.Args) > 1 && e.Args[1].Kind == "literal" {
						name = e.Args[1].Name
					}
				default:
					continue
				}
			} else {
				continue
			}
			b := r.classBinding(file, c.Scope, receiver)
			if b.Identity != "" {
				r.staticMutations[b.Identity+"\x00"+name] = true
			}
		}
	}
}

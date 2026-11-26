package python

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file captures the reduced syntax the FastAPI security-dependency
// linker reads (T026, docs/extract/auth.md): parameter
// annotations and defaults, module-level Annotated aliases and dependency
// lists. It is syntax only; whether a name is fastapi's Depends, typing's
// Annotated or a fastapi.security class is decided at link time through
// the module resolver.

// dependencyDepth bounds the reduced expressions (Annotated[...] inside a
// default, nested calls): deeper forms are kind "other".
const dependencyDepth = 6

// importsFastAPI is gate DG: the file imports fastapi or a submodule of it.
func importsFastAPI(imports []facts.Import) bool {
	for _, imp := range imports {
		if imp.Module == "fastapi" || strings.HasPrefix(imp.Module, "fastapi.") {
			return true
		}
	}
	return false
}

// dependencyExpr reduces an annotation, default or dependency list to the
// forms the linker reads: names, attributes, calls (with their span),
// subscripts (Annotated[...]), lists and tuples, string and boolean
// literals. Everything else keeps only its kind.
func dependencyExpr(n *parse.Node, depth int) facts.RouteExpr {
	for n != nil && (n.Kind == "type" || n.Kind == "parenthesized_expression") && len(n.Children) == 1 {
		n = n.Children[0]
	}
	if n == nil {
		return facts.RouteExpr{}
	}
	if depth > dependencyDepth {
		return facts.RouteExpr{Kind: "other", Offset: int(n.Start)}
	}
	switch n.Kind {
	case "identifier", "attribute", "string", "concatenated_string":
		return expression(n)
	case "true", "false", "none":
		return facts.RouteExpr{Kind: n.Kind, Name: n.Text, Text: n.Text, Offset: int(n.Start)}
	case "call":
		e := facts.RouteExpr{Kind: "call", Text: compactText(n.Text), Offset: int(n.Start)}
		o := dependencyExpr(n.Fields["function"], depth+1)
		e.Object = &o
		span := facts.Span(n)
		e.Span = &span
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				switch a.Kind {
				case "comment":
				case "keyword_argument":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Name: facts.Text(a.Fields["name"]), Value: dependencyExpr(a.Fields["value"], depth+1)})
				case "dictionary_splat":
					e.Keywords = append(e.Keywords, facts.RouteKeyword{Value: facts.RouteExpr{Kind: a.Kind, Text: compactText(a.Text)}})
				default:
					e.Args = append(e.Args, dependencyExpr(a, depth+1))
				}
			}
		}
		return e
	case "subscript":
		e := facts.RouteExpr{Kind: "subscript", Offset: int(n.Start)}
		o := dependencyExpr(n.Fields["value"], depth+1)
		e.Object = &o
		for _, c := range n.Children[1:] {
			if c.Kind != "comment" {
				e.Args = append(e.Args, dependencyExpr(c, depth+1))
			}
		}
		return e
	case "generic_type":
		// Annotated[...] in an annotation position: the name, then a
		// type_parameter holding the items.
		if len(n.Children) == 0 {
			break
		}
		e := facts.RouteExpr{Kind: "subscript", Offset: int(n.Start)}
		o := dependencyExpr(n.Children[0], depth+1)
		e.Object = &o
		for _, c := range n.Children[1:] {
			if c.Kind != "type_parameter" {
				continue
			}
			for _, p := range c.Children {
				if p.Kind != "comment" {
					e.Args = append(e.Args, dependencyExpr(p, depth+1))
				}
			}
		}
		return e
	case "list", "tuple", "expression_list":
		e := facts.RouteExpr{Kind: n.Kind, Offset: int(n.Start)}
		if n.Kind == "expression_list" {
			e.Kind = "tuple"
		}
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Args = append(e.Args, dependencyExpr(c, depth+1))
			}
		}
		return e
	}
	return facts.RouteExpr{Kind: n.Kind, Text: compactText(n.Text), Offset: int(n.Start)}
}

// compactText keeps an expression's text short and on one line for the
// unknown entries the linker reports.
func compactText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return s
}

// dependencyParameter reduces one parameter node of a function in a gate
// DG file; ok is false for a parameter that can declare nothing (no
// annotation, no call default, or a splat).
func dependencyParameter(c *parse.Node) (facts.PythonDependencyParameter, bool) {
	switch c.Kind {
	case "typed_parameter", "default_parameter", "typed_default_parameter":
	default:
		return facts.PythonDependencyParameter{}, false
	}
	name := c.Fields["name"]
	if name == nil {
		for _, v := range c.Children {
			if v.Kind == "identifier" {
				name = v
				break
			}
		}
	}
	if name == nil || name.Kind != "identifier" {
		// *args: T and **kwargs: T are not dependency parameters.
		return facts.PythonDependencyParameter{}, false
	}
	p := facts.PythonDependencyParameter{Name: identifier(name.Text), Span: facts.Span(c)}
	if typ := c.Fields["type"]; typ != nil {
		a := dependencyExpr(typ, 0)
		p.Annotation = &a
	}
	if value := c.Fields["value"]; value != nil {
		for value.Kind == "parenthesized_expression" && len(value.Children) == 1 {
			value = value.Children[0]
		}
		if value.Kind == "call" {
			d := dependencyExpr(value, 0)
			p.Default = &d
		}
	}
	if p.Annotation == nil && p.Default == nil {
		return facts.PythonDependencyParameter{}, false
	}
	return p, true
}

// annotatedAlias reports whether an assignment's right side is a subscript
// (a candidate `X = Annotated[T, Depends(f)]` alias).
func annotatedAlias(n *parse.Node) bool {
	for n != nil && n.Kind == "parenthesized_expression" && len(n.Children) == 1 {
		n = n.Children[0]
	}
	return n != nil && n.Kind == "subscript"
}

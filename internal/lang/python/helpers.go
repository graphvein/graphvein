package python

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

func helperCallExpression(n *parse.Node) facts.RouteExpr {
	e := facts.RouteExpr{Kind: "call", Offset: int(n.Start)}
	callee := expression(n.Fields["function"])
	e.Object = &callee
	if args := n.Fields["arguments"]; args != nil {
		for _, a := range args.Children {
			switch a.Kind {
			case "comment":
			case "keyword_argument":
				e.Keywords = append(e.Keywords, facts.RouteKeyword{Name: facts.Text(a.Fields["name"]), Value: bindingExpression(a.Fields["value"])})
			case "dictionary_splat":
				e.Keywords = append(e.Keywords, facts.RouteKeyword{Value: facts.RouteExpr{Kind: a.Kind}})
			default:
				e.Args = append(e.Args, bindingExpression(a))
			}
		}
	}
	return e
}

func helperSignature(n *parse.Node, identity string) *facts.PythonHelper {
	h := &facts.PythonHelper{Identity: identity, Async: strings.HasPrefix(strings.TrimSpace(n.Text), "async ")}
	params := n.Fields["parameters"]
	if params == nil {
		return nil
	}
	kind := "positional"
	for _, c := range params.Children {
		text := strings.TrimSpace(c.Text)
		if text == "/" {
			for i := range h.Parameters {
				h.Parameters[i].Kind = "posonly"
			}
			continue
		}
		if text == "*" {
			kind = "kwonly"
			continue
		}
		if c.Kind == "comment" {
			continue
		}
		name := c.Fields["name"]
		if c.Kind == "identifier" {
			name = c
		}
		if name == nil && len(c.Children) > 0 {
			name = c.Children[0]
		}
		paramKind := kind
		if strings.HasPrefix(text, "**") {
			paramKind = "kwargs"
			kind = "kwonly"
		} else if strings.HasPrefix(text, "*") {
			paramKind = "varargs"
			kind = "kwonly"
		}
		for name != nil && name.Kind != "identifier" && len(name.Children) > 0 {
			name = name.Children[0]
		}
		if name == nil || name.Kind != "identifier" {
			continue
		}
		h.Parameters = append(h.Parameters, facts.PythonParameter{Name: name.Text, Kind: paramKind, Default: c.Fields["value"] != nil})
	}
	parameter := map[string]int{}
	for i, p := range h.Parameters {
		parameter[p.Name] = i
	}
	relevant := false
	var walk func(*parse.Node)
	walk = func(node *parse.Node) {
		if node == nil {
			return
		}
		// A nested declaration binds its name here. Ordinary inner assignments
		// have their own locals, but a nonlocal declaration can rebind ours.
		if node.Kind == "function_definition" || node.Kind == "class_definition" || node.Kind == "lambda" {
			if i, ok := parameter[facts.Text(node.Fields["name"])]; ok {
				h.Parameters[i].Blocked = true
			}
			var nonlocal func(*parse.Node)
			nonlocal = func(inner *parse.Node) {
				if inner == nil {
					return
				}
				if inner.Kind == "nonlocal_statement" {
					for _, c := range inner.Children {
						if i, ok := parameter[c.Text]; ok {
							h.Parameters[i].Blocked = true
						}
					}
				}
				for _, c := range inner.Children {
					nonlocal(c)
				}
			}
			nonlocal(node.Fields["body"])
			return
		}
		if node.Kind == "call" {
			f := node.Fields["function"]
			if f != nil && f.Kind == "attribute" && (facts.Text(f.Fields["attribute"]) == "include_router" || facts.Text(f.Fields["attribute"]) == "mount") {
				e := expression(node)
				if o := f.Fields["object"]; o != nil && o.Kind == "identifier" {
					if _, ok := parameter[o.Text]; ok {
						relevant = true
					}
				}
				for _, a := range e.Args {
					if a.Kind == "identifier" {
						if _, ok := parameter[a.Name]; ok {
							relevant = true
						}
					}
				}
				for _, k := range e.Keywords {
					if k.Value.Kind == "identifier" {
						if _, ok := parameter[k.Value.Name]; ok {
							relevant = true
						}
					}
				}
			}
		}
		var targets []*parse.Node
		switch node.Kind {
		case "assignment", "augmented_assignment", "for_statement", "for_in_clause":
			targets = append(targets, node.Fields["left"])
		case "named_expression":
			targets = append(targets, node.Fields["name"])
		case "as_pattern":
			targets = append(targets, node.Fields["alias"])
		case "delete_statement", "global_statement", "nonlocal_statement":
			targets = append(targets, node.Children...)
		}
		for _, t := range targets {
			for _, name := range helperTargetNames(t) {
				if i, ok := parameter[name]; ok {
					h.Parameters[i].Blocked = true
				}
			}
		}
		for _, c := range node.Children {
			walk(c)
		}
	}
	walk(n.Fields["body"])
	if !relevant {
		return nil
	}
	return h
}

func helperTargetNames(n *parse.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == "as_pattern_target" && len(n.Children) == 1 {
		return helperTargetNames(n.Children[0])
	}
	return bindingNames(n)
}

// BindHelperArguments validates the complete Python call before exposing any
// supplied parameter. Defaults and rebound parameters never establish objects.
func BindHelperArguments(parameters []facts.PythonParameter, call facts.RouteExpr) (map[string]facts.RouteExpr, bool) {
	bound := map[string]facts.RouteExpr{}
	byName := map[string]facts.PythonParameter{}
	var positional []facts.PythonParameter
	var varargs, kwargs bool
	for _, p := range parameters {
		byName[p.Name] = p
		switch p.Kind {
		case "positional", "posonly":
			positional = append(positional, p)
		case "varargs":
			varargs = true
		case "kwargs":
			kwargs = true
		}
	}
	for i, a := range call.Args {
		if a.Kind == "list_splat" {
			return nil, false
		}
		if i >= len(positional) {
			if !varargs {
				return nil, false
			}
			continue
		}
		bound[positional[i].Name] = a
	}
	seenKeywords := map[string]bool{}
	for _, k := range call.Keywords {
		if k.Name == "" || seenKeywords[k.Name] {
			return nil, false
		}
		seenKeywords[k.Name] = true
		p, ok := byName[k.Name]
		if !ok || p.Kind == "posonly" || p.Kind == "varargs" || p.Kind == "kwargs" {
			if !kwargs {
				return nil, false
			}
			continue
		}
		if _, duplicate := bound[k.Name]; duplicate {
			return nil, false
		}
		bound[k.Name] = k.Value
	}
	for _, p := range parameters {
		if p.Kind == "varargs" || p.Kind == "kwargs" {
			continue
		}
		if _, present := bound[p.Name]; !present && !p.Default {
			return nil, false
		}
		if p.Blocked {
			delete(bound, p.Name)
		}
	}
	return bound, true
}

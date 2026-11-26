package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// cacheCommonJS records the CommonJS exports a file writes at its top level
// (T036, rows CJ-E1..CJ-E4 of docs/extract/routes.md): `module.exports = E`
// and `exports.n = E` / `module.exports.n = E` as statements of the program
// itself. Every other write of the exports object (inside a function or a
// branch, a second write of one name, `exports = E`, a computed member,
// `Object.assign(module.exports, ...)`, both a module value and members, or
// an ESM export in the same file) leaves the exports unread: the value a
// `require` returns then depends on what runs, so no row claims it.
func cacheCommonJS(root *parse.Node, r *facts.RouteFacts) {
	if root == nil {
		return
	}
	out := &facts.CommonJSExports{}
	written, esm := false, false
	recorded := map[uint32]bool{}
	for _, statement := range root.Children {
		if statement.Kind == "export_statement" {
			esm = true
			continue
		}
		if statement.Kind != "expression_statement" {
			continue
		}
		for _, a := range statement.Children {
			if a.Kind != "assignment_expression" {
				continue
			}
			target, member, ok := exportsTarget(a.Fields["left"])
			if !ok {
				continue
			}
			recorded[a.Start] = true
			written = true
			value := routeExpr(a.Fields["right"])
			switch {
			case target == "module" && member == "":
				if out.Module != nil {
					out.Unread = true
				}
				out.Module = &value
			case member != "":
				if out.Members == nil {
					out.Members = map[string]facts.RouteExpr{}
				}
				if _, twice := out.Members[member]; twice {
					out.Unread = true
				}
				out.Members[member] = value
			default:
				// `exports = E` rebinds the local name only.
				out.Unread = true
			}
		}
	}
	var walk func(*parse.Node)
	walk = func(n *parse.Node) {
		switch n.Kind {
		case "assignment_expression", "augmented_assignment_expression":
			if !recorded[n.Start] && writesExports(n.Fields["left"]) {
				out.Unread = true
				written = true
			}
		case "call_expression":
			// Object.assign(exports, ...), Object.defineProperty(module.exports, ...).
			if callee := n.Fields["function"]; callee != nil && strings.HasPrefix(compact(callee.Text), "Object.") {
				if args := n.Fields["arguments"]; args != nil {
					for _, c := range args.Children {
						if c.Kind == "comment" || c.Kind == "(" {
							continue
						}
						if t := compact(c.Text); t == "exports" || t == "module.exports" {
							out.Unread = true
							written = true
						}
						break
					}
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if esm || out.Module != nil && len(out.Members) > 0 {
		out.Unread = true
	}
	if written {
		r.CommonJS = out
	}
}

// exportsTarget reads the left side of a top-level assignment: `exports`
// or `module.exports` (member ""), or one named member of either. A
// computed member or a deeper chain is not a target.
func exportsTarget(left *parse.Node) (target, member string, ok bool) {
	if left == nil {
		return "", "", false
	}
	switch t := compact(left.Text); {
	case t == "exports":
		return "exports", "", true
	case t == "module.exports":
		return "module", "", true
	}
	if left.Kind != "member_expression" {
		return "", "", false
	}
	property := left.Fields["property"]
	if property == nil || property.Kind != "property_identifier" {
		return "", "", false
	}
	switch compact(text(left.Fields["object"])) {
	case "exports":
		return "exports", property.Text, true
	case "module.exports":
		return "module", property.Text, true
	}
	return "", "", false
}

// writesExports reports whether an assignment's left side writes the
// exports object or one of its members.
func writesExports(left *parse.Node) bool {
	if left == nil {
		return false
	}
	t := compact(left.Text)
	for _, base := range []string{"exports", "module.exports"} {
		if t == base || strings.HasPrefix(t, base+".") || strings.HasPrefix(t, base+"[") {
			return true
		}
	}
	return false
}

// requireDestructure binds the names of `const { a, b: c } = require("m")`
// to the members they read (T036, row CJ-U3), so a router a module exports
// as a member resolves as `require("m").a` does. Only shorthand and
// `key: identifier` properties are read; a default value, a nested
// pattern, a computed key or a rest element binds the name to nothing.
func requireDestructure(pattern, value *parse.Node, bind func(name string, b facts.RouteBinding)) bool {
	if pattern == nil || pattern.Kind != "object_pattern" || value == nil || value.Kind != "call_expression" {
		return false
	}
	callee := value.Fields["function"]
	if callee == nil || callee.Kind != "identifier" || callee.Text != "require" {
		return false
	}
	if _, ok := loadSpecifier(value); !ok {
		return false
	}
	call := routeExpr(value)
	for _, c := range pattern.Children {
		switch c.Kind {
		case "shorthand_property_identifier_pattern":
			object := call
			bind(c.Text, facts.RouteBinding{Value: facts.RouteExpr{Kind: "member_expression", Text: "require." + c.Text, Name: c.Text, Offset: int(c.Start), Object: &object}})
		case "pair_pattern":
			key, local := c.Fields["key"], c.Fields["value"]
			if key == nil || local == nil || local.Kind != "identifier" || key.Kind != "property_identifier" {
				patternBindings(c, func(name string) { bind(name, facts.RouteBinding{}) })
				continue
			}
			object := call
			bind(local.Text, facts.RouteBinding{Value: facts.RouteExpr{Kind: "member_expression", Text: "require." + key.Text, Name: key.Text, Offset: int(c.Start), Object: &object}})
		default:
			patternBindings(c, func(name string) { bind(name, facts.RouteBinding{}) })
		}
	}
	return true
}

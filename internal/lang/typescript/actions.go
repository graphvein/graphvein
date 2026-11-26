package typescript

import (
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Server action syntax (T024b, ADR 0077, rows SA of
// docs/extract/server-actions.md). The extractor records directive
// prologues, the exports of 'use server' modules, inline 'use server'
// functions and JSX expression attributes; it never decides what a use
// names. The route walker (routes.go) records the scope-bound parts.

// prologue returns the directive prologue of a program or statement
// block: the leading expression statements that are a single string
// literal, as the TypeScript compiler reads them (comments are skipped; a
// parenthesized string ends the prologue). A string with an escape
// sequence keeps its raw text, so it never equals a directive name.
func prologue(block *parse.Node) []string {
	if block == nil {
		return nil
	}
	var out []string
	for _, statement := range block.Children {
		if statement.Kind == "comment" || statement.Kind == "hash_bang_line" {
			continue
		}
		if statement.Kind != "expression_statement" {
			break
		}
		var literal *parse.Node
		for _, c := range statement.Children {
			if c.Kind != "comment" {
				if literal != nil {
					literal = nil
					break
				}
				literal = c
			}
		}
		if literal == nil || literal.Kind != "string" {
			break
		}
		value := ""
		switch {
		case len(literal.Children) == 1 && literal.Children[0].Kind == "string_fragment":
			value = literal.Children[0].Text
		case len(literal.Children) > 0:
			value = literal.Text
		}
		out = append(out, value)
	}
	return out
}

// hasDirective reports whether a prologue holds name.
func hasDirective(directives []string, name string) bool {
	for _, d := range directives {
		if d == name {
			return true
		}
	}
	return false
}

// hasToken reports whether a node's anonymous tokens include token.
func hasToken(n *parse.Node, token string) bool {
	for _, t := range n.Tokens {
		if t == token {
			return true
		}
	}
	return false
}

// extractActions records the directive prologue and, for a 'use server'
// module, the form of every top-level export (row SA-M1). It runs after
// extractRoutes, which may already have recorded inline actions and
// attributes in out.Actions.
func extractActions(source []byte, root *parse.Node, out *Facts) {
	actions := out.Actions
	if actions == nil {
		actions = &facts.ActionFacts{}
	}
	actions.Directives = prologue(root)
	if hasDirective(actions.Directives, "use server") {
		actions.Exports = moduleActionExports(source, root, out)
	}
	if len(actions.Directives) > 0 || len(actions.Exports) > 0 || len(actions.Inline) > 0 || len(actions.Attributes) > 0 || len(actions.Ternaries) > 0 || len(actions.Loads) > 0 || actions.LoadsUnread {
		out.Actions = actions
	} else {
		out.Actions = nil
	}
}

// actionFunctionForm classifies a top-level function or variable value for
// row SA-M1.
func actionFunctionForm(n *parse.Node) string {
	switch n.Kind {
	case "function_declaration", "function_expression", "function":
		if hasToken(n, "async") {
			return "async_function"
		}
		return "sync_function"
	case "generator_function_declaration", "generator_function":
		return "non_function"
	}
	return ""
}

func actionValueForm(v *parse.Node) string {
	if v == nil {
		return "non_function"
	}
	for v.Kind == "parenthesized_expression" && len(v.Children) == 1 {
		v = v.Children[0]
	}
	switch v.Kind {
	case "arrow_function":
		if hasToken(v, "async") {
			return "const_async_arrow"
		}
		return "sync_function"
	case "function_expression", "function":
		if hasToken(v, "async") {
			return "const_async_function"
		}
		return "sync_function"
	case "call_expression":
		return "const_call"
	}
	return "non_function"
}

// importedName finds the import binding of a local name: its module and
// the name exported there.
func importedName(out *Facts, local string) (string, string, bool) {
	for _, imp := range out.Imports {
		if name, ok := imp.Bindings[local]; ok && name != "*" {
			return imp.Module, name, true
		}
	}
	return "", "", false
}

// moduleActionExports lists the exports of a 'use server' module with
// their SA-M1 form. Type-only exports (types, interfaces, `export type`)
// are not values and are not listed.
func moduleActionExports(source []byte, root *parse.Node, out *Facts) []facts.ActionExport {
	// locals maps a top-level binding to its form, for `export { a }`.
	locals := map[string]string{}
	for _, statement := range root.Children {
		declaration := statement
		if statement.Kind == "export_statement" && statement.Fields["declaration"] != nil {
			declaration = statement.Fields["declaration"]
		}
		switch declaration.Kind {
		case "function_declaration", "generator_function_declaration":
			locals[text(declaration.Fields["name"])] = actionFunctionForm(declaration)
		case "lexical_declaration", "variable_declaration":
			constant := declaration.Kind == "lexical_declaration" && hasToken(declaration, "const")
			for _, d := range declaration.Children {
				if d.Kind != "variable_declarator" || d.Fields["name"] == nil || d.Fields["name"].Kind != "identifier" {
					continue
				}
				form := actionValueForm(d.Fields["value"])
				if !constant && form != "non_function" && form != "sync_function" {
					form = "let_binding"
				}
				locals[d.Fields["name"].Text] = form
			}
		case "class_declaration", "abstract_class_declaration", "enum_declaration":
			locals[text(declaration.Fields["name"])] = "non_function"
		}
	}
	node := func(name string) string {
		for _, n := range out.Nodes {
			if n.QualifiedName == name && (n.Kind == "function" || n.Kind == "variable") {
				return n.Identity
			}
		}
		return ""
	}
	exported := func(name string) string {
		if identity := out.Exports[name]; identity != "" {
			if kind, _, _, err := contract.ParseIdentity(identity); err == nil && (kind == "function" || kind == "variable") {
				return identity
			}
		}
		return ""
	}
	var exports []facts.ActionExport
	add := func(statement *parse.Node, name, local, form, identity string) {
		exports = append(exports, facts.ActionExport{Name: name, Local: local, Form: form, Identity: identity, Span: span(statement), Hash: contract.ContentHash(source[statement.Start:statement.End])})
	}
	for _, statement := range root.Children {
		if statement.Kind != "export_statement" || typeOnlyStatement(statement) {
			continue
		}
		isDefault := hasToken(statement, "default")
		declaration := statement.Fields["declaration"]
		from := statement.Fields["source"]
		switch {
		case isDefault:
			v := statement.Fields["value"]
			if v == nil {
				v = declaration
			}
			for v != nil && v.Kind == "parenthesized_expression" && len(v.Children) == 1 {
				v = v.Children[0]
			}
			form := "default_other"
			switch {
			case v == nil:
			case v.Kind == "function_expression" || v.Kind == "function_declaration" || v.Kind == "function":
				form = "sync_function"
				if hasToken(v, "async") {
					form = "default_async_function"
				}
			case v.Kind == "arrow_function":
				// `export default async () => {}`: registered as the
				// default action (server_actions.rs, ExportDefaultExpr
				// Expr::Arrow); a sync arrow is a build error.
				form = "sync_function"
				if hasToken(v, "async") {
					form = "default_async_arrow"
				}
			case v.Kind == "call_expression":
				// `export default wrap(...)`: registered as the default
				// action (ExportDefaultExpr Expr::Call), as const_call.
				form = "default_call"
			case v.Kind == "identifier":
				// `export default foo`: registered with foo's binding
				// (ExportDefaultExpr Expr::Ident).
				local := v.Text
				switch lf := locals[local]; lf {
				case "async_function", "const_async_arrow", "const_async_function", "const_call":
					add(statement, "default", local, "default_clause", node(local))
					continue
				case "":
					if module, name, ok := importedName(out, local); ok {
						exports = append(exports, facts.ActionExport{Name: "default", Local: local, Imported: name, Module: module, Form: "imported_clause", Span: span(statement), Hash: contract.ContentHash(source[statement.Start:statement.End])})
						continue
					}
				default:
					form = lf
				}
			}
			add(statement, "default", "", form, exported("default"))
		case declaration != nil:
			switch declaration.Kind {
			case "function_declaration", "generator_function_declaration":
				name := text(declaration.Fields["name"])
				form := actionFunctionForm(declaration)
				add(statement, name, "", form, exported(name))
			case "lexical_declaration", "variable_declaration":
				for _, d := range declaration.Children {
					if d.Kind != "variable_declarator" || d.Fields["name"] == nil {
						continue
					}
					if d.Fields["name"].Kind != "identifier" {
						for _, bound := range destructuredNames(d.Fields["name"]) {
							add(statement, bound, "", "non_function", "")
						}
						continue
					}
					name := d.Fields["name"].Text
					add(statement, name, "", locals[name], exported(name))
				}
			case "class_declaration", "abstract_class_declaration":
				add(statement, text(declaration.Fields["name"]), "", "class", "")
			case "enum_declaration":
				add(statement, text(declaration.Fields["name"]), "", "non_function", "")
			}
		default:
			star := from != nil && !hasChild(statement, "export_clause")
			if star {
				add(statement, "*", "", "star", "")
				continue
			}
			for _, c := range statement.Children {
				if c.Kind != "export_clause" {
					continue
				}
				for _, s := range c.Children {
					if s.Kind != "export_specifier" || hasToken(s, "type") {
						continue
					}
					local, alias := text(s.Fields["name"]), text(s.Fields["alias"])
					if alias == "" {
						alias = local
					}
					if from != nil {
						add(statement, alias, local, "reexport", "")
						continue
					}
					form := locals[local]
					switch form {
					case "async_function", "const_async_arrow", "const_async_function", "const_call":
						add(statement, alias, local, "clause", node(local))
					case "":
						// An imported binding exported again is a server
						// reference too (next-custom-transforms
						// server_actions.rs: `export { foo }`).
						if module, name, ok := importedName(out, local); ok {
							exports = append(exports, facts.ActionExport{Name: alias, Local: local, Imported: name, Module: module, Form: "imported_clause", Span: span(statement), Hash: contract.ContentHash(source[statement.Start:statement.End])})
						} else {
							add(statement, alias, local, "reexport", "")
						}
					default:
						add(statement, alias, local, form, "")
					}
				}
			}
		}
	}
	return exports
}

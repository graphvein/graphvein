package rust

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// StringLiteral decodes Rust strings without evaluating macros or expressions.
func StringLiteral(text string) (string, bool) {
	if strings.HasPrefix(text, "r") {
		i := strings.IndexByte(text, '"')
		if i < 1 || strings.Trim(text[1:i], "#") != "" {
			return "", false
		}
		end := "\"" + text[1:i]
		if !strings.HasSuffix(text, end) || len(text) < i+1+len(end) {
			return "", false
		}
		return text[i+1 : len(text)-len(end)], true
	}
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return "", false
	}
	s := text[1 : len(text)-1]
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", false
		}
		switch s[i] {
		case '\\', '"', '\'':
			b.WriteByte(s[i])
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '0':
			b.WriteByte(0)
		case '\n', '\r':
			for i+1 < len(s) && strings.ContainsRune(" \t\r\n", rune(s[i+1])) {
				i++
			}
		case 'x':
			if i+2 >= len(s) {
				return "", false
			}
			v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
			if err != nil || v > 127 {
				return "", false
			}
			b.WriteByte(byte(v))
			i += 2
		case 'u':
			if i+1 >= len(s) || s[i+1] != '{' {
				return "", false
			}
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				return "", false
			}
			end += i + 2
			v, err := strconv.ParseUint(strings.ReplaceAll(s[i+2:end], "_", ""), 16, 32)
			if err != nil || !utf8.ValidRune(rune(v)) {
				return "", false
			}
			b.WriteRune(rune(v))
			i = end
		default:
			return "", false
		}
	}
	return b.String(), true
}

func rustPath(n *parse.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == "generic_function" {
		return rustPath(n.Fields["function"])
	}
	if n.Kind == "generic_type" {
		return rustPath(n.Fields["type"])
	}
	if n.Kind == "scoped_identifier" || n.Kind == "scoped_type_identifier" {
		p := rustPath(n.Fields["path"])
		name := facts.Text(n.Fields["name"])
		if p == "" && strings.HasPrefix(n.Text, "::") {
			return "::" + name
		}
		if p != "" {
			return p + "::" + name
		}
	}
	return compact(n.Text)
}

func syntax(file string, source []byte, root *parse.Node, emitted facts.Facts) *facts.RustFacts {
	out := &facts.RustFacts{}
	newScope := func(parent int, module, function string, n *parse.Node) int {
		out.Scopes = append(out.Scopes, facts.RustScope{Parent: parent, Module: module, Function: function, Start: n.Start, End: n.End, Span: facts.Span(n), Items: map[string][]facts.RustItem{}})
		return len(out.Scopes) - 1
	}
	identity := func(n *parse.Node) string {
		span := facts.Span(n)
		for _, d := range emitted.Nodes {
			if d.Span != nil && *d.Span == span {
				return d.Identity
			}
		}
		return ""
	}
	var expr func(*parse.Node, int, bool) facts.RustExpr
	expr = func(n *parse.Node, scope int, conditional bool) facts.RustExpr {
		if n == nil {
			return facts.RustExpr{}
		}
		e := facts.RustExpr{Kind: n.Kind, Text: n.Text, Scope: scope, Offset: n.Start, Span: facts.Span(n), Conditional: conditional}
		if value, ok := StringLiteral(n.Text); ok && (n.Kind == "string_literal" || n.Kind == "raw_string_literal") {
			e.Kind = "literal"
			e.Name = value
			return e
		}
		switch n.Kind {
		case "identifier", "scoped_identifier", "scoped_type_identifier", "generic_function":
			e.Kind = "path"
			e.Name = rustPath(n)
		case "call_expression":
			e.Kind = "call"
			callee := n.Fields["function"]
			if callee != nil && callee.Kind == "generic_function" {
				callee = callee.Fields["function"]
			}
			if callee != nil && callee.Kind == "field_expression" {
				e.Kind = "method"
				e.Name = facts.Text(callee.Fields["field"])
				v := expr(callee.Fields["value"], scope, conditional)
				e.Object = &v
				if field := callee.Fields["field"]; field != nil {
					e.Span.StartLine = int(field.Row) + 1
					e.Span.StartColumn = int(field.Column)
					e.Hash = contract.ContentHash(source[field.Start:n.End])
				}
			} else {
				e.Name = rustPath(callee)
			}
			if args := n.Fields["arguments"]; args != nil {
				for _, a := range args.Children {
					if a.Kind != "line_comment" && a.Kind != "block_comment" {
						e.Args = append(e.Args, expr(a, scope, conditional))
					}
				}
			}
			if e.Hash == "" {
				e.Hash = contract.ContentHash(source[n.Start:n.End])
			}
		case "binary_expression":
			if op := facts.Text(n.Fields["operator"]); op == "|" || strings.Contains(n.Text, "|") {
				e.Kind = "or"
				e.Args = []facts.RustExpr{expr(n.Fields["left"], scope, conditional), expr(n.Fields["right"], scope, conditional)}
			}
		case "parenthesized_expression", "return_expression":
			if len(n.Children) > 0 {
				return expr(n.Children[0], scope, conditional)
			}
		}
		return e
	}
	var walk func(*parse.Node, int, bool) []facts.RustStatement
	walk = func(n *parse.Node, scope int, conditional bool) []facts.RustStatement {
		if n == nil {
			return nil
		}
		s := &out.Scopes[scope]
		module, function := s.Module, s.Function
		if n.Kind == "use_declaration" {
			imports := usePaths(n.Fields["argument"], "", function, facts.Span(n))
			for i := range imports {
				imports[i].ModuleScope = module
			}
			out.Scopes[scope].Imports = append(out.Scopes[scope].Imports, imports...)
			return nil
		}
		if n.Kind == "mod_item" {
			if body := n.Fields["body"]; body != nil {
				// A mod item in a function body is no module of the file
				// (row RS-M5): its scope keeps the function, so module
				// lookups never select it.
				child := newScope(scope, facts.Join(module, facts.Text(n.Fields["name"])), function, body)
				walk(body, child, conditional)
			}
			return nil
		}
		if n.Kind == "impl_item" {
			// An impl's associated consts and types are no module items
			// (row RS-D9); its functions keep their scopes.
			if body := n.Fields["body"]; body != nil {
				for _, c := range body.Children {
					if c.Kind != "const_item" && c.Kind != "type_item" {
						walk(c, scope, conditional)
					}
				}
			}
			return nil
		}
		if n.Fields["name"] != nil && strings.HasSuffix(n.Kind, "_item") && n.Kind != "impl_item" {
			name := facts.Text(n.Fields["name"])
			item := facts.RustItem{Identity: identity(n)}
			if n.Kind == "const_item" || n.Kind == "static_item" {
				item.Value = expr(n.Fields["value"], scope, conditional)
				typ := compact(facts.Text(n.Fields["type"]))
				item.Constant = (typ == "&str" || typ == "&'staticstr") && item.Value.Kind == "literal"
			}
			kind, _, _, _ := contract.ParseIdentity(item.Identity)
			// Associated methods are members of their type, not bare lexical
			// function bindings. Their existing identities still serve factories.
			if kind != "method" {
				out.Scopes[scope].Items[name] = append(out.Scopes[scope].Items[name], item)
			}
			if n.Kind == "trait_item" {
				walk(n.Fields["body"], scope, conditional)
			}
			if n.Kind == "function_item" {
				body := n.Fields["body"]
				if body == nil {
					return nil
				}
				child := newScope(scope, module, item.Identity, body)
				statements := walk(body, child, conditional)
				out.Functions = append(out.Functions, facts.RustFunction{Identity: item.Identity, Scope: child, Body: statements, Conditional: conditional})
				return nil
			}
			return nil
		}
		if n.Kind == "let_declaration" {
			return []facts.RustStatement{{Kind: "let", Name: facts.Text(n.Fields["pattern"]), Scope: scope, Expr: expr(n.Fields["value"], scope, conditional), Conditional: conditional}}
		}
		if n.Kind == "assignment_expression" {
			return []facts.RustStatement{{Kind: "assign", Name: facts.Text(n.Fields["left"]), Scope: scope, Expr: expr(n.Fields["right"], scope, conditional), Conditional: conditional}}
		}
		if n.Kind == "call_expression" || n.Kind == "return_expression" || n.Kind == "identifier" || n.Kind == "scoped_identifier" {
			kind := "expr"
			if n.Kind == "return_expression" {
				kind = "return"
			}
			return []facts.RustStatement{{Kind: kind, Scope: scope, Expr: expr(n, scope, conditional), Conditional: conditional}}
		}
		if n.Kind == "closure_expression" || n.Kind == "macro_invocation" {
			return nil
		}
		if n.Kind == "expression_statement" && len(n.Children) > 0 {
			rows := walk(n.Children[0], scope, conditional)
			for i := range rows {
				if rows[i].Kind == "expr" {
					rows[i].Kind = "statement"
				}
			}
			return rows
		}
		branch := facts.Builtin(n.Kind, "if_expression match_arm loop_expression while_expression for_expression")
		if n.Kind == "block" && n.Start != out.Scopes[scope].Start {
			child := newScope(scope, module, function, n)
			return []facts.RustStatement{{Kind: "block", Scope: child, Children: walk(n, child, conditional), Conditional: conditional}}
		}
		var rows []facts.RustStatement
		cfg := false
		for _, c := range n.Children {
			if c.Kind == "attribute_item" {
				cfg = cfg || strings.HasPrefix(compact(c.Text), "#[cfg(")
				continue
			}
			if c.Kind == "line_comment" || c.Kind == "block_comment" {
				continue
			}
			rows = append(rows, walk(c, scope, conditional || branch || cfg)...)
			cfg = false
		}
		return rows
	}
	first := newScope(-1, "", "", root)
	walk(root, first, false)
	return out
}

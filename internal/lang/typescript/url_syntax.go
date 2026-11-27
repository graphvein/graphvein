package typescript

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// URL syntax is separate from route syntax so broader client evaluation cannot
// accidentally expand the route-pattern or factory contract.
func urlExpr(n *parse.Node) facts.RouteExpr {
	return (*exprMemo)(nil).url(n)
}

func (m *exprMemo) buildURL(n *parse.Node) facts.RouteExpr {
	if n == nil {
		return facts.RouteExpr{}
	}
	e := facts.RouteExpr{Kind: n.Kind, Text: n.Text, Offset: int(n.Start)}
	switch n.Kind {
	case "spread_element":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Parts = append(e.Parts, m.url(c))
			}
		}
	case "array":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				e.Parts = append(e.Parts, m.url(c))
			}
		}
	case "identifier", "property_identifier":
		e.Name = n.Text
	case "parenthesized_expression", "as_expression", "satisfies_expression", "non_null_expression", "type_assertion":
		for _, c := range n.Children {
			if !strings.Contains(c.Kind, "type") && c.Kind != "comment" {
				return m.url(c)
			}
		}
	case "number", "true", "false", "null", "undefined":
		e.Name = n.Text
	case "unary_expression":
		if strings.HasPrefix(n.Text, "-") || strings.HasPrefix(n.Text, "+") {
			e.Operator = n.Text[:1]
			e.Parts = []facts.RouteExpr{m.url(n.Fields["argument"])}
		}
	case "string":
		e.Kind = "literal"
		if text, ok := jsString(n.Text); ok {
			e.Name = text
		} else {
			e.Kind = "unknown"
		}
	case "member_expression", "call_expression", "subscript_expression", "new_expression":
		if n.Kind == "member_expression" {
			e.Name = text(n.Fields["property"])
		}
		field := "object"
		if n.Kind == "call_expression" {
			field = "function"
		}
		if n.Kind == "new_expression" {
			field = "constructor"
		}
		v := m.url(n.Fields[field])
		e.Object = &v
		if n.Kind == "subscript_expression" {
			e.Parts = []facts.RouteExpr{m.url(n.Fields["index"])}
		}
		if args := n.Fields["arguments"]; args != nil {
			e.Args = nil
			for _, a := range args.Children {
				if a.Kind != "comment" {
					e.Args = append(e.Args, m.url(a))
				}
			}
		}
	case "binary_expression":
		e.Operator = text(n.Fields["operator"])
		if e.Operator == "" {
			for _, token := range n.Tokens {
				if token == "+" || token == "??" || token == "||" {
					e.Operator = token
				}
			}
		}
		e.Parts = []facts.RouteExpr{m.url(n.Fields["left"]), m.url(n.Fields["right"])}
		// Keep nested URL-piece spelling even when assertions are transparent.
		// The enclosing request expression (and therefore its identity) is unchanged.
		for i, field := range []string{"left", "right"} {
			if part := n.Fields[field]; part != nil {
				e.Parts[i].Text = part.Text
			}
		}
	case "ternary_expression":
		e.Parts = []facts.RouteExpr{m.url(n.Fields["condition"]), m.url(n.Fields["consequence"]), m.url(n.Fields["alternative"])}
	case "object":
		e.Properties = map[string]facts.RouteExpr{}
		hasSpread := false
		for _, property := range n.Children {
			hasSpread = hasSpread || property.Kind == "spread_element"
		}
		e.Spread = hasSpread
		for _, p := range n.Children {
			if p.Kind == "comment" {
				continue
			}
			// Preserve every own entry (including duplicates and spreads) for
			// generated request/config proof. The evaluator still rejects Invalid.
			entry := facts.RouteKeyword{}
			if p.Kind == "pair" && p.Fields["key"] != nil {
				if hasSpread {
					entry.Value = m.url(p.Fields["value"])
				}
				key := p.Fields["key"]
				if key.Kind == "property_identifier" || key.Kind == "identifier" {
					entry.Name = key.Text
				} else if key.Kind == "string" {
					entry.Name, _ = jsString(key.Text)
				}
			} else if p.Kind == "shorthand_property_identifier" {
				entry.Name = p.Text
				if hasSpread {
					entry.Value = facts.RouteExpr{Kind: "identifier", Name: p.Text, Text: p.Text, Offset: int(p.Start)}
				}
			} else {
				entry.Value = m.url(p)
			}
			e.Keywords = append(e.Keywords, entry)
			if p.Kind == "shorthand_property_identifier" {
				name := p.Text
				if _, exists := e.Properties[name]; !exists {
					e.Keys = append(e.Keys, name)
				}
				v := facts.RouteExpr{Kind: "identifier", Name: name, Text: name, Offset: int(p.Start)}
				s := span(p)
				v.Span = &s
				e.Properties[name] = v
				continue
			}
			key := p.Fields["key"]
			if p.Kind != "pair" || key == nil || (key.Kind != "property_identifier" && key.Kind != "identifier" && key.Kind != "string" && key.Kind != "number") {
				// Preserve own query/queryFn keys for Orval override inspection,
				// including literal computed keys and method/accessor declarations.
				// Invalid stays true: this does not broaden object evaluation.
				own := key
				if own == nil {
					own = p.Fields["name"]
				}
				if own != nil && own.Kind == "computed_property_name" && len(own.Children) == 1 {
					own = own.Children[0]
				}
				name := ""
				if own != nil {
					if own.Kind == "string" {
						name, _ = jsString(own.Text)
					} else if own.Kind == "property_identifier" || own.Kind == "identifier" {
						name = own.Text
					}
				}
				if name == "query" || name == "queryFn" {
					value := facts.RouteExpr{Kind: "unknown"}
					if p.Kind == "pair" {
						value = m.url(p.Fields["value"])
					}
					e.Properties[name] = value
				}
				e.Invalid = true
				continue
			}
			name := key.Text
			if key.Kind == "string" {
				var ok bool
				name, ok = jsString(name)
				if !ok {
					e.Invalid = true
				}
			}
			if key.Kind == "number" {
				var ok bool
				name, ok = numericPropertyKey(name)
				if !ok {
					e.Invalid = true
				}
			}
			// A colon-form __proto__ property is a prototype setter, not an own
			// data property. Such objects are outside the plain-object subset.
			if name == "__proto__" {
				e.Invalid = true
			}
			if _, exists := e.Properties[name]; !exists {
				e.Keys = append(e.Keys, name)
			}
			v := m.url(p.Fields["value"])
			s := span(p)
			v.Span = &s
			e.Properties[name] = v
		}
	case "template_substitution":
		// Reached through a tagged template's arguments (sql`...${t}...`).
		if v := substitutionValue(n); v != nil {
			o := m.url(v)
			e.Object = &o
		}
	case "template_string":
		literal := true
		for _, c := range n.Children {
			if c.Kind == "template_substitution" {
				literal = false
			}
		}
		if literal {
			e.Kind = "literal"
			if v, ok := jsString(n.Text); ok {
				e.Name = v
			} else {
				e.Kind = "unknown"
			}
			break
		}
		e.Parts = nil
		start := n.Start + 1
		for _, c := range n.Children {
			if c.Kind != "template_substitution" {
				continue
			}
			if c.Start > start {
				raw := n.Text[start-n.Start : c.Start-n.Start]
				v, ok := jsString("`" + raw + "`")
				if !ok {
					e.Invalid = true
				}
				e.Parts = append(e.Parts, facts.RouteExpr{Kind: "literal", Name: v})
			}
			for _, v := range c.Children {
				if v.Kind != "comment" {
					part := m.url(v)
					part.Text = v.Text
					e.Parts = append(e.Parts, part)
					break
				}
			}
			start = c.End
		}
		if start < n.End-1 {
			v, ok := jsString("`" + n.Text[start-n.Start:len(n.Text)-1] + "`")
			if !ok {
				e.Invalid = true
			}
			e.Parts = append(e.Parts, facts.RouteExpr{Kind: "literal", Name: v})
		}
	}
	return e
}

func numericPropertyKey(raw string) (string, bool) {
	raw = strings.ReplaceAll(raw, "_", "")
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		if i, e := strconv.ParseUint(raw, 0, 64); e == nil {
			v = float64(i)
			err = nil
		}
	}
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return "", false
	}
	if v == 0 {
		return "0", true
	}
	if math.Abs(v) >= 1e-6 && math.Abs(v) < 1e21 {
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	s := strconv.FormatFloat(v, 'e', -1, 64)
	p := strings.Split(s, "e")
	i, _ := strconv.Atoi(p[1])
	sign := ""
	if i >= 0 {
		sign = "+"
	}
	return p[0] + "e" + sign + strconv.Itoa(i), true
}

// Decode JavaScript escapes without executing code. Invalid strings stay unknown.
func jsString(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	raw := s[1 : len(s)-1]
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c != '\\' {
			out.WriteByte(c)
			continue
		}
		i++
		if i >= len(raw) {
			return "", false
		}
		c = raw[i]
		switch c {
		case '\n':
		case '\r':
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++
			}
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'v':
			out.WriteByte('\v')
		case '0':
			if i+1 < len(raw) && raw[i+1] >= '0' && raw[i+1] <= '9' {
				return "", false
			}
			out.WriteByte(0)
		case 'x', 'u':
			count := 2
			if c == 'u' {
				count = 4
			}
			begin := i + 1
			end := begin + count
			if c == 'u' && begin < len(raw) && raw[begin] == '{' {
				begin++
				at := strings.IndexByte(raw[begin:], '}')
				if at < 0 {
					return "", false
				}
				end = begin + at
				i = end
			} else {
				i = end - 1
			}
			if end > len(raw) {
				return "", false
			}
			v, err := strconv.ParseUint(raw[begin:end], 16, 32)
			if err != nil || v > 0x10ffff {
				return "", false
			}
			if v >= 0xd800 && v <= 0xdbff {
				if i+6 >= len(raw) || raw[i+1:i+3] != "\\u" {
					return "", false
				}
				low, err := strconv.ParseUint(raw[i+3:i+7], 16, 32)
				if err != nil || low < 0xdc00 || low > 0xdfff {
					return "", false
				}
				out.WriteRune(utf16.DecodeRune(rune(v), rune(low)))
				i += 6
				continue
			}
			if v >= 0xdc00 && v <= 0xdfff {
				return "", false
			}
			out.WriteRune(rune(v))
		default:
			if c >= '1' && c <= '9' {
				return "", false
			}
			out.WriteByte(c)
		}
	}
	return out.String(), true
}

// urlStatements is the URL statement syntax of n, built without sharing.
func urlStatements(n *parse.Node) []facts.URLStatement {
	return (*exprMemo)(nil).urlStatements(n)
}

func (m *exprMemo) urlStatements(n *parse.Node) []facts.URLStatement {
	if n == nil {
		return nil
	}
	if n.Kind == "statement_block" {
		var out []facts.URLStatement
		for _, c := range n.Children {
			if c.Kind != "comment" {
				out = append(out, m.urlStatements(c)...)
			}
		}
		return out
	}
	s := facts.URLStatement{Kind: n.Kind, Span: span(n)}
	switch n.Kind {
	case "lexical_declaration":
		constant := false
		for _, t := range n.Tokens {
			constant = constant || t == "const"
		}
		var out []facts.URLStatement
		for _, c := range n.Children {
			if c.Kind == "comment" {
				continue
			}
			if c.Kind != "variable_declarator" || c.Fields["name"] == nil || c.Fields["name"].Kind != "identifier" {
				return []facts.URLStatement{s}
			}
			out = append(out, facts.URLStatement{Kind: "declaration", Name: c.Fields["name"].Text, Constant: constant, Expression: m.url(c.Fields["value"]), Span: span(c)})
		}
		return out
	case "expression_statement":
		if len(n.Children) == 1 {
			v := n.Children[0]
			if v.Kind == "assignment_expression" || v.Kind == "augmented_assignment_expression" {
				left := v.Fields["left"]
				if left != nil && left.Kind == "identifier" {
					s.Kind = "assignment"
					s.Name = left.Text
					s.Expression = m.url(v.Fields["right"])
					s.Operator = text(v.Fields["operator"])
					if s.Operator == "" {
						for _, t := range v.Tokens {
							if t == "=" || t == "+=" {
								s.Operator = t
							}
						}
					}
				}
			}
		}
	case "return_statement":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				s.Expression = m.url(c)
				break
			}
		}
	case "if_statement":
		s.Expression = m.url(n.Fields["condition"])
		s.Body = m.urlStatements(n.Fields["consequence"])
		s.Else = m.urlStatements(n.Fields["alternative"])
	case "else_clause":
		for _, c := range n.Children {
			if c.Kind != "comment" {
				return m.urlStatements(c)
			}
		}
	case "for_in_statement":
		left := n.Fields["left"]
		if left != nil && left.Kind == "identifier" {
			s.Name = left.Text
		}
		s.Expression = m.url(n.Fields["right"])
		s.Body = m.urlStatements(n.Fields["body"])
		for _, t := range n.Tokens {
			if t == "of" || t == "in" {
				s.Operator = t
			}
		}
	default:
		if n.Kind != "statement_block" {
			s.Kind = "unsupported"
		}
	}
	return []facts.URLStatement{s}
}

func eligibleURLStatements(body []facts.URLStatement) bool {
	for _, s := range body {
		switch s.Kind {
		case "declaration", "return_statement":
		case "assignment":
			if s.Operator != "=" && s.Operator != "+=" {
				return false
			}
		case "if_statement", "for_in_statement":
			if s.Kind == "for_in_statement" && s.Name == "" {
				return false
			}
			if !eligibleURLStatements(s.Body) || !eligibleURLStatements(s.Else) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

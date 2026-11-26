package typescript

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// The next.config redirects, rewrites and headers rules the route shape
// rows read (T029 SH-N24, docs/contract/claims/route-shape-nextjs.md).
// This reading is wider than the literal rewrites of row NX-P3 (which the
// client matcher reads and which stay as they are): a rule list may be an
// array literal, a top-level const bound to one and grown only with
// push(), spreads of such lists and concat() of them; every element must
// be an object literal. A rule's source is kept when it is a string
// literal or a top-level const bound to one; has and missing make it
// conditional.

// ruleReader resolves rule lists in one next.config file.
type ruleReader struct {
	root   *parse.Node
	consts map[string]*parse.Node
}

// keyRules reads the value of a redirects, rewrites or headers key (a
// method's body, or a function or arrow function value).
func (r ruleReader) keyRules(key string, member, value *parse.Node) ([]facts.NextConfigRule, bool) {
	var ret *parse.Node
	switch {
	case member != nil && member.Kind == "method_definition":
		ret = singleReturn(member.Fields["body"])
	case value != nil:
		v := peelExpression(value)
		switch {
		case v == nil:
		case v.Kind == "arrow_function":
			body := v.Fields["body"]
			if body != nil && body.Kind == "statement_block" {
				ret = singleReturn(body)
			} else {
				ret = body
			}
		case v.Kind == "function_expression" || v.Kind == "function":
			ret = singleReturn(v.Fields["body"])
		}
	}
	if ret == nil {
		return nil, false
	}
	if key == "rewrites" {
		if v := peelExpression(ret); v != nil && v.Kind == "object" {
			var out []facts.NextConfigRule
			for _, m := range v.Children {
				if m.Kind == "comment" {
					continue
				}
				phase, list := memberKey(m)
				if m.Kind != "pair" || phase != "beforeFiles" && phase != "afterFiles" && phase != "fallback" {
					return nil, false
				}
				rules, ok := r.list(key, phase, list, 0)
				if !ok {
					return nil, false
				}
				out = append(out, rules...)
			}
			return out, true
		}
		return r.list(key, "afterFiles", ret, 0)
	}
	return r.list(key, "", ret, 0)
}

// singleReturn is the expression of a body that is one return statement.
func singleReturn(body *parse.Node) *parse.Node {
	if body == nil || body.Kind != "statement_block" {
		return nil
	}
	var statements []*parse.Node
	for _, c := range body.Children {
		if c.Kind != "comment" {
			statements = append(statements, c)
		}
	}
	if len(statements) != 1 || statements[0].Kind != "return_statement" || len(statements[0].Children) != 1 {
		return nil
	}
	return statements[0].Children[0]
}

// list resolves a rule list expression.
func (r ruleReader) list(key, phase string, n *parse.Node, depth int) ([]facts.NextConfigRule, bool) {
	var out []facts.NextConfigRule
	ok := r.walkList(n, depth, func(obj *parse.Node) bool {
		out = append(out, r.rule(key, phase, obj, depth))
		return true
	})
	if !ok {
		return nil, false
	}
	return out, true
}

// walkList calls add for every object literal of a list expression: an
// array literal (spread elements resolved as lists), a top-level const
// bound to a list and changed only with push(), and concat() of lists
// and objects. It is false when any part is another form.
func (r ruleReader) walkList(n *parse.Node, depth int, add func(obj *parse.Node) bool) bool {
	n = peelExpression(n)
	if n == nil || depth > 8 {
		return false
	}
	switch n.Kind {
	case "array":
		for _, item := range n.Children {
			if !r.walkElement(item, depth, add) {
				return false
			}
		}
		return true
	case "identifier":
		bound, ok := r.consts[n.Text]
		if !ok {
			return false
		}
		pushed, ok := r.pushes(n.Text)
		if !ok || !r.walkList(bound, depth+1, add) {
			return false
		}
		for _, arg := range pushed {
			if !r.walkElement(arg, depth, add) {
				return false
			}
		}
		return true
	case "call_expression":
		// a.concat(b, ...): a list and lists or objects.
		f := n.Fields["function"]
		if f == nil || f.Kind != "member_expression" || text(f.Fields["property"]) != "concat" {
			return false
		}
		if !r.walkList(f.Fields["object"], depth+1, add) {
			return false
		}
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind == "comment" {
					continue
				}
				if p := peelExpression(a); p != nil && p.Kind == "object" {
					if !add(p) {
						return false
					}
					continue
				}
				if !r.walkList(a, depth+1, add) {
					return false
				}
			}
		}
		return true
	}
	return false
}

// walkElement handles one array element or push() argument: an object
// literal, or a spread list.
func (r ruleReader) walkElement(item *parse.Node, depth int, add func(obj *parse.Node) bool) bool {
	if item.Kind == "comment" {
		return true
	}
	if item.Kind == "spread_element" {
		return len(item.Children) == 1 && r.walkList(item.Children[0], depth+1, add)
	}
	obj := peelExpression(item)
	if obj == nil || obj.Kind != "object" {
		return false
	}
	return add(obj)
}

// rule reads one rule object.
func (r ruleReader) rule(key, phase string, obj *parse.Node, depth int) facts.NextConfigRule {
	rule := facts.NextConfigRule{Key: key, Phase: phase}
	known := true
	keysKnown := false
	for _, m := range obj.Children {
		if m.Kind == "comment" {
			continue
		}
		k, v := memberKey(m)
		if m.Kind != "pair" {
			// A spread or a computed key may add has, change the source or
			// set other headers.
			known, rule.Conditional = false, true
			keysKnown = false
			rule.HeaderKeys = nil
			continue
		}
		switch k {
		case "":
			// A computed key may set the header list.
			keysKnown = false
			rule.HeaderKeys = nil
		case "source":
			if s, ok := r.stringValue(v); ok {
				rule.Source = s
			} else {
				known = false
			}
		case "has", "missing":
			rule.Conditional = true
		case "basePath", "locale":
			// The source is matched against another path.
			known = false
		case "headers":
			if key == "headers" {
				rule.HeaderKeys, keysKnown = r.headerKeys(v, depth)
			}
		}
	}
	if !known {
		rule.Source = ""
	}
	if key == "headers" && keysKnown {
		rule.HeaderKeysRead = true
	} else {
		rule.HeaderKeys = nil
	}
	return rule
}

// headerKeys reads the names a headers rule sets: every element of its
// list an object literal whose key is a string literal, or a top-level
// const bound to one, that is an HTTP token. A name with a ':' (a source
// parameter Next.js substitutes) is not a token and is not read.
func (r ruleReader) headerKeys(n *parse.Node, depth int) ([]string, bool) {
	keys := []string{}
	ok := r.walkList(n, depth+1, func(obj *parse.Node) bool {
		name, read := "", false
		for _, m := range obj.Children {
			if m.Kind == "comment" {
				continue
			}
			k, v := memberKey(m)
			if m.Kind != "pair" || k == "" {
				return false
			}
			if k == "key" {
				name, read = r.stringValue(v)
				if !read || !httpToken(name) {
					return false
				}
			}
		}
		if !read {
			return false
		}
		keys = append(keys, name)
		return true
	})
	return keys, ok
}

// httpToken reports a non-empty HTTP field name (RFC 9110 token).
func httpToken(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", c):
		default:
			return false
		}
	}
	return true
}

// stringValue is a string literal, or a top-level const bound to one.
func (r ruleReader) stringValue(n *parse.Node) (string, bool) {
	if s, ok := stringLiteral(n); ok {
		return s, true
	}
	if p := peelExpression(n); p != nil && p.Kind == "identifier" {
		if bound, ok := r.consts[p.Text]; ok && !mutated(r.root, p.Text) {
			return stringLiteral(bound)
		}
	}
	return "", false
}

// pushes returns the arguments of every name.push(...) in the file, and
// false when the name is used in any other way than push, concat, a
// spread or a return (it may be changed or passed on).
func (r ruleReader) pushes(name string) ([]*parse.Node, bool) {
	var args []*parse.Node
	ok := true
	var walk func(n, parent, grand *parse.Node)
	walk = func(n, parent, grand *parse.Node) {
		if !ok || n == nil {
			return
		}
		if n.Kind == "identifier" && n.Text == name && parent != nil {
			switch {
			case parent.Kind == "variable_declarator" && parent.Fields["name"] == n:
			case parent.Kind == "spread_element", parent.Kind == "return_statement":
			case parent.Kind == "arrow_function" && parent.Fields["body"] == n:
			case parent.Kind == "pair" && parent.Fields["value"] == n:
				// A phase list of a returned rewrites object.
			case parent.Kind == "arguments" && grand != nil && grand.Kind == "call_expression" && concatCall(grand):
			case parent.Kind == "member_expression" && parent.Fields["object"] == n:
				// Checked at the member expression below.
			default:
				ok = false
			}
		}
		if n.Kind == "call_expression" {
			if f := n.Fields["function"]; f != nil && f.Kind == "member_expression" {
				if o := f.Fields["object"]; o != nil && o.Kind == "identifier" && o.Text == name {
					switch text(f.Fields["property"]) {
					case "push":
						if a := n.Fields["arguments"]; a != nil {
							for _, c := range a.Children {
								if c.Kind != "comment" {
									args = append(args, c)
								}
							}
						}
					case "concat":
					default:
						ok = false
					}
				}
			}
		}
		if n.Kind == "member_expression" {
			if o := n.Fields["object"]; o != nil && o.Kind == "identifier" && o.Text == name {
				if parent == nil || parent.Kind != "call_expression" || parent.Fields["function"] != n {
					ok = false
				}
			}
		}
		for _, c := range n.Children {
			walk(c, n, parent)
		}
	}
	walk(r.root, nil, nil)
	return args, ok
}

// concatCall reports a call of a concat method.
func concatCall(call *parse.Node) bool {
	f := call.Fields["function"]
	return f != nil && f.Kind == "member_expression" && text(f.Fields["property"]) == "concat"
}

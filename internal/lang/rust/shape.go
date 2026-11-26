package rust

import (
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// shapeGate reports whether a file can hold anything the Axum route shape
// payload records (facts.RustShape): handlers and extractors name axum or
// http types, and serialized types derive serde.
func shapeGate(source []byte) bool {
	s := string(source)
	return strings.Contains(s, "axum") || strings.Contains(s, "serde") || strings.Contains(s, "http")
}

// safeCaptureShape is captureShape with a recover guard: a failure in the
// shape capture of one file leaves that file without a shape payload (its
// routes then read every section from the handler as not read) and never
// stops its other facts or other files.
func safeCaptureShape(source []byte, root *parse.Node, emitted facts.Facts, scopes []facts.RustScope) (out *facts.RustShape) {
	defer func() {
		if recover() != nil {
			out = nil
		}
	}()
	return captureShape(source, root, emitted, scopes)
}

// captureShape records the Axum route shape payload of a file (T029):
// type declarations with their serde attributes, and the
// signatures, result expressions and request value uses of functions. It
// runs after syntax, whose scopes it points into. It returns nil for files
// outside the gate or without anything to record.
func captureShape(source []byte, root *parse.Node, emitted facts.Facts, scopes []facts.RustScope) *facts.RustShape {
	if !shapeGate(source) {
		return nil
	}
	c := &shapeCapture{scopes: scopes}
	for _, n := range emitted.Nodes {
		if n.Span != nil {
			c.spans = append(c.spans, n)
		}
	}
	c.walk(root, nil, "", "")
	if len(c.out.Types)+len(c.out.Fns) == 0 {
		return nil
	}
	c.out.Version = facts.RustShapeVersion
	return &c.out
}

type shapeCapture struct {
	scopes []facts.RustScope
	spans  []contract.Node
	out    facts.RustShape
}

// identity finds the emitted node of an item by its span.
func (c *shapeCapture) identity(n *parse.Node) string {
	span := facts.Span(n)
	for _, d := range c.spans {
		if *d.Span == span {
			return d.Identity
		}
	}
	return ""
}

// scopeOf returns the smallest syntax scope holding [start, end), or the
// scope whose bounds are exactly a block's.
func (c *shapeCapture) scopeOf(start, end uint32) int {
	best := 0
	for i, s := range c.scopes {
		if s.Start <= start && end <= s.End && s.End-s.Start <= c.scopes[best].End-c.scopes[best].Start {
			best = i
		}
	}
	return best
}

func line(n *parse.Node) int { return int(n.Row) + 1 }

// walk visits module-level items (and impl bodies), never function bodies.
func (c *shapeCapture) walk(n *parse.Node, attrs []*parse.Node, selfType, trait string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "struct_item", "enum_item", "type_item":
		c.typeDecl(n, attrs)
		return
	case "function_item":
		c.function(n, selfType, trait)
		return
	case "impl_item":
		self := compactType(facts.Text(n.Fields["type"]))
		tr := compactType(facts.Text(n.Fields["trait"]))
		if body := n.Fields["body"]; body != nil {
			for _, ch := range body.Children {
				c.walk(ch, nil, self, tr)
			}
		}
		return
	case "mod_item":
		c.walk(n.Fields["body"], nil, "", "")
		return
	case "source_file", "declaration_list":
		var pending []*parse.Node
		for _, ch := range n.Children {
			if ch.Kind == "attribute_item" {
				pending = append(pending, ch)
				continue
			}
			if ch.Kind == "line_comment" || ch.Kind == "block_comment" {
				continue
			}
			c.walk(ch, pending, selfType, trait)
			pending = nil
		}
	}
}

// shapeType converts a type node.
func shapeType(n *parse.Node) facts.RustType {
	if n == nil {
		return facts.RustType{Kind: "other"}
	}
	t := facts.RustType{Kind: "other", Text: compactType(n.Text)}
	switch n.Kind {
	case "type_identifier", "primitive_type", "scoped_type_identifier":
		t.Kind, t.Name = "path", rustPath(n)
	case "generic_type":
		t.Kind, t.Name = "path", rustPath(n.Fields["type"])
		if args := n.Fields["type_arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind == "lifetime" || a.Kind == "line_comment" || a.Kind == "block_comment" {
					continue
				}
				if a.Kind == "type_binding" {
					return facts.RustType{Kind: "other", Text: t.Text}
				}
				t.Args = append(t.Args, shapeType(a))
			}
		}
	case "reference_type":
		t.Kind, t.Args = "ref", []facts.RustType{shapeType(n.Fields["type"])}
	case "tuple_type":
		t.Kind = "tuple"
		for _, a := range n.Children {
			t.Args = append(t.Args, shapeType(a))
		}
	case "array_type":
		t.Kind, t.Args = "array", []facts.RustType{shapeType(n.Fields["element"])}
	case "unit_type":
		t.Kind = "unit"
	case "abstract_type":
		t.Kind, t.Name = "impl", rustPath(n.Fields["trait"])
	}
	return t
}

// serdeArgs reads the arguments of #[serde(...)] attributes and the
// derive paths of #[derive(...)] attributes. others are the names of the
// remaining attributes (facts.RustTypeDecl.Attrs): a `cfg_attr` is
// written with the names of the attributes it applies, `cfg_attr(serde)`.
func serdeArgs(attrs []*parse.Node) (derives []string, args []facts.RustAttr, others []string) {
	for _, a := range attrs {
		if len(a.Children) == 0 || a.Children[0].Kind != "attribute" {
			continue
		}
		attr := a.Children[0]
		name := compact(facts.Text(attr.Children[0]))
		tree := attr.Fields["arguments"]
		switch {
		case name == "derive" && tree != nil:
			inner := strings.TrimSuffix(strings.TrimPrefix(compact(tree.Text), "("), ")")
			for _, d := range strings.Split(inner, ",") {
				if d != "" {
					derives = append(derives, d)
				}
			}
		case name == "serde" && tree != nil:
			args = append(args, serdeTokens(tree.Text)...)
		case name == "cfg_attr" && tree != nil:
			var applied []string
			for i, t := range serdeTokens(tree.Text) {
				if i > 0 {
					applied = append(applied, t.Name)
				}
			}
			others = append(others, "cfg_attr("+strings.Join(applied, ",")+")")
		default:
			others = append(others, name)
		}
	}
	return derives, args, others
}

// serdeTokens reads the arguments of a serde attribute's token tree from
// its text, `(a, b = "x", c(d = "y"))`: tree-sitter keeps keywords such as
// `default` and `crate` as anonymous tokens, so the named children miss
// them.
func serdeTokens(text string) []facts.RustAttr {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "(") || !strings.HasSuffix(text, ")") {
		return []facts.RustAttr{{Name: compact(text), Nested: true}}
	}
	text = text[1 : len(text)-1]
	var parts []string
	depth, start := 0, 0
	inString := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		switch {
		case inString:
			if ch == '\\' {
				i++
			} else if ch == '"' {
				inString = false
			}
		case ch == '"':
			inString = true
		case ch == '(' || ch == '[' || ch == '{':
			depth++
		case ch == ')' || ch == ']' || ch == '}':
			depth--
		case ch == ',' && depth == 0:
			parts = append(parts, text[start:i])
			start = i + 1
		}
	}
	parts = append(parts, text[start:])
	var out []facts.RustAttr
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		name := p
		rest := ""
		if i := strings.IndexAny(p, "=("); i >= 0 {
			name, rest = strings.TrimSpace(p[:i]), strings.TrimSpace(p[i:])
		}
		a := facts.RustAttr{Name: name}
		switch {
		case rest == "":
		case strings.HasPrefix(rest, "="):
			if v, ok := StringLiteral(strings.TrimSpace(rest[1:])); ok {
				a.Value = v
			} else {
				a.Nested = true
			}
		default:
			a.Nested = true
		}
		for _, r := range name {
			if !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				a.Nested = true
			}
		}
		out = append(out, a)
	}
	return out
}

// fieldAttrs splits a field list's children into fields with their
// preceding attributes.
func fieldList(list *parse.Node) []facts.RustField {
	var out []facts.RustField
	var pending []*parse.Node
	position := 0
	for _, ch := range list.Children {
		switch ch.Kind {
		case "attribute_item":
			pending = append(pending, ch)
		case "field_declaration":
			_, args, others := serdeArgs(pending)
			out = append(out, facts.RustField{Name: facts.Text(ch.Fields["name"]), Type: shapeType(ch.Fields["type"]), Serde: args, Attrs: others, Line: line(ch)})
			pending = nil
		case "line_comment", "block_comment", "visibility_modifier":
		default:
			if list.Kind == "ordered_field_declaration_list" {
				_, args, others := serdeArgs(pending)
				out = append(out, facts.RustField{Name: strconv.Itoa(position), Type: shapeType(ch), Serde: args, Attrs: others, Line: line(ch)})
				position++
				pending = nil
			}
		}
	}
	return out
}

func (c *shapeCapture) typeDecl(n *parse.Node, attrs []*parse.Node) {
	id := c.identity(n)
	name := facts.Text(n.Fields["name"])
	if id == "" || name == "" {
		return
	}
	d := facts.RustTypeDecl{Identity: id, Name: name, Scope: c.scopeOf(n.Start, n.End), Line: line(n)}
	d.Derives, d.Serde, d.Attrs = serdeArgs(attrs)
	if tp := n.Fields["type_parameters"]; tp != nil {
		for _, p := range tp.Children {
			switch p.Kind {
			case "type_parameter", "optional_type_parameter":
				d.Generics = append(d.Generics, facts.Text(p.Fields["name"]))
				def := facts.RustType{}
				if dt := p.Fields["default_type"]; dt != nil {
					def = shapeType(dt)
				}
				d.Defaults = append(d.Defaults, def)
			case "type_identifier":
				d.Generics = append(d.Generics, p.Text)
				d.Defaults = append(d.Defaults, facts.RustType{})
			case "constrained_type_parameter":
				d.Generics = append(d.Generics, facts.Text(p.Fields["left"]))
				d.Defaults = append(d.Defaults, facts.RustType{})
			}
		}
	}
	hasDefault := false
	for _, def := range d.Defaults {
		hasDefault = hasDefault || def.Kind != ""
	}
	if !hasDefault {
		d.Defaults = nil
	}
	body := n.Fields["body"]
	switch n.Kind {
	case "struct_item":
		switch {
		case body == nil:
			d.Kind = "unit_struct"
		case body.Kind == "ordered_field_declaration_list":
			d.Kind = "tuple_struct"
			d.Fields = fieldList(body)
		default:
			d.Kind = "struct"
			d.Fields = fieldList(body)
		}
	case "enum_item":
		d.Kind = "enum"
		if body != nil {
			var pending []*parse.Node
			for _, ch := range body.Children {
				if ch.Kind == "attribute_item" {
					pending = append(pending, ch)
					continue
				}
				if ch.Kind != "enum_variant" {
					continue
				}
				_, args, others := serdeArgs(pending)
				pending = nil
				v := facts.RustVariant{Name: facts.Text(ch.Fields["name"]), Kind: "unit", Serde: args, Attrs: others}
				if b := ch.Fields["body"]; b != nil {
					v.Kind = "struct"
					if b.Kind == "ordered_field_declaration_list" {
						v.Kind = "tuple"
					}
				}
				d.Variants = append(d.Variants, v)
			}
		}
	case "type_item":
		d.Kind = "alias"
		t := shapeType(n.Fields["type"])
		d.Alias = &t
	}
	c.out.Types = append(c.out.Types, d)
}

// bodyScope finds the syntax scope of a function body block.
func (c *shapeCapture) bodyScope(body *parse.Node) int {
	for i, s := range c.scopes {
		if s.Start == body.Start && s.End == body.End {
			return i
		}
	}
	return c.scopeOf(body.Start, body.End)
}

type shapeRoot struct{ value, via string }

type fnCapture struct {
	fn       *facts.RustFnShape
	env      map[string]shapeRoot
	matches  int
	bareSeen map[string]bool
}

func (c *shapeCapture) function(n *parse.Node, selfType, trait string) {
	id := c.identity(n)
	body := n.Fields["body"]
	if id == "" || body == nil {
		return
	}
	f := facts.RustFnShape{Identity: id, Line: line(n), Scope: c.bodyScope(body), SelfType: selfType, Trait: trait}
	if tp := n.Fields["type_parameters"]; tp != nil {
		for _, p := range tp.Children {
			switch p.Kind {
			case "type_parameter", "optional_type_parameter":
				f.Generics = append(f.Generics, facts.Text(p.Fields["name"]))
			case "type_identifier":
				f.Generics = append(f.Generics, p.Text)
			case "constrained_type_parameter":
				f.Generics = append(f.Generics, facts.Text(p.Fields["left"]))
			}
		}
	}
	fc := &fnCapture{fn: &f, env: map[string]shapeRoot{}}
	if params := n.Fields["parameters"]; params != nil {
		for _, p := range params.Children {
			switch p.Kind {
			case "self_parameter":
				f.Params = append(f.Params, facts.RustParam{Pattern: compact(p.Text), Self: true, Line: line(p), Type: facts.RustType{Kind: "other", Text: compact(p.Text)}})
			case "parameter":
				rp := facts.RustParam{Pattern: compact(facts.Text(p.Fields["pattern"])), Type: shapeType(p.Fields["type"]), Line: line(p)}
				pat := p.Fields["pattern"]
				if pat != nil && pat.Kind == "identifier" {
					rp.Name = pat.Text
				}
				if pat != nil && pat.Kind == "tuple_struct_pattern" {
					var inner []*parse.Node
					for _, ch := range pat.Children {
						if ch != pat.Fields["type"] {
							inner = append(inner, ch)
						}
					}
					if len(inner) == 1 && inner[0].Kind == "identifier" {
						rp.Inner = inner[0].Text
					}
				}
				for _, b := range []string{rp.Name, rp.Inner} {
					if b != "" && b != "_" {
						fc.env[b] = shapeRoot{value: b}
					}
				}
				f.Params = append(f.Params, rp)
			}
		}
	}
	if rt := n.Fields["return_type"]; rt != nil {
		t := shapeType(rt)
		f.Return = &t
	}
	f.Results = append(fc.results(body, nil), fc.returns(body)...)
	fc.bindings(body)
	fc.uses(body)
	c.out.Fns = append(c.out.Fns, f)
}

// value converts an expression to the payload vocabulary.
func (fc *fnCapture) value(n *parse.Node, depth int) facts.RustValue {
	if n == nil {
		return facts.RustValue{Kind: "other"}
	}
	if depth > 6 {
		return facts.RustValue{Kind: "other", Line: line(n), Text: clip(n.Text)}
	}
	switch n.Kind {
	case "parenthesized_expression", "reference_expression", "await_expression":
		if v := n.Fields["value"]; v != nil {
			return fc.value(v, depth)
		}
		if len(n.Children) > 0 {
			return fc.value(n.Children[len(n.Children)-1], depth)
		}
	case "try_expression":
		if len(n.Children) > 0 {
			v := fc.value(n.Children[0], depth)
			v.Try = true
			return v
		}
	case "identifier", "scoped_identifier", "self":
		return facts.RustValue{Kind: "path", Name: rustPath(n), Line: line(n)}
	case "string_literal", "raw_string_literal":
		if s, ok := StringLiteral(n.Text); ok {
			return facts.RustValue{Kind: "literal", Name: s, Line: line(n)}
		}
	case "struct_expression":
		return facts.RustValue{Kind: "struct", Name: rustPath(n.Fields["name"]), Line: line(n), Text: compact(facts.Text(n.Fields["name"]))}
	case "tuple_expression":
		v := facts.RustValue{Kind: "tuple", Line: line(n)}
		for _, ch := range n.Children {
			if ch.Kind != "line_comment" && ch.Kind != "block_comment" {
				v.Args = append(v.Args, fc.value(ch, depth+1))
			}
		}
		return v
	case "array_expression":
		v := facts.RustValue{Kind: "array", Line: line(n)}
		for _, ch := range n.Children {
			if ch.Kind != "line_comment" && ch.Kind != "block_comment" {
				v.Args = append(v.Args, fc.value(ch, depth+1))
			}
		}
		return v
	case "macro_invocation":
		return facts.RustValue{Kind: "macro", Name: compact(facts.Text(n.Fields["macro"])), Line: line(n)}
	case "call_expression":
		callee := n.Fields["function"]
		v := facts.RustValue{Kind: "call", Line: line(n)}
		if callee != nil && callee.Kind == "generic_function" {
			callee = callee.Fields["function"]
		}
		if callee != nil && callee.Kind == "field_expression" {
			v.Kind = "method"
			v.Name = facts.Text(callee.Fields["field"])
			r := fc.value(callee.Fields["value"], depth+1)
			v.Recv = &r
		} else {
			v.Name = rustPath(callee)
		}
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind != "line_comment" && a.Kind != "block_comment" {
					v.Args = append(v.Args, fc.value(a, depth+1))
				}
			}
		}
		return v
	}
	return facts.RustValue{Kind: "other", Line: line(n), Text: clip(n.Text)}
}

func clip(s string) string {
	s = compact(s)
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

var unitValue = facts.RustValue{Kind: "other", Text: "()"}

// results returns the expressions a block or expression evaluates to,
// through match arms, if branches and blocks, each with the match arms on
// its path. Return expressions are collected by returns.
func (fc *fnCapture) results(n *parse.Node, arms []facts.RustArm) []facts.RustValue {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "block":
		var last *parse.Node
		for _, ch := range n.Children {
			if ch.Kind != "line_comment" && ch.Kind != "block_comment" && ch.Kind != "attribute_item" {
				last = ch
			}
		}
		if last == nil {
			return []facts.RustValue{withArms(unitValue, arms, line(n))}
		}
		if last.Kind == "expression_statement" {
			for _, t := range last.Tokens {
				if t == ";" {
					if len(last.Children) > 0 && last.Children[0].Kind == "return_expression" {
						return nil
					}
					return []facts.RustValue{withArms(unitValue, arms, line(last))}
				}
			}
			if len(last.Children) > 0 {
				return fc.results(last.Children[0], arms)
			}
		}
		if last.Kind == "let_declaration" || strings.HasSuffix(last.Kind, "_item") {
			return []facts.RustValue{withArms(unitValue, arms, line(last))}
		}
		return fc.results(last, arms)
	case "if_expression":
		out := fc.results(n.Fields["consequence"], arms)
		alt := n.Fields["alternative"]
		if alt == nil {
			return append(out, withArms(unitValue, arms, line(n)))
		}
		for _, ch := range alt.Children {
			out = append(out, fc.results(ch, arms)...)
		}
		return out
	case "match_expression":
		var out []facts.RustValue
		fc.matches++
		id := fc.matches
		scrutinee := compact(facts.Text(n.Fields["value"]))
		if body := n.Fields["body"]; body != nil {
			index := 0
			for _, a := range body.Children {
				if a.Kind != "match_arm" {
					continue
				}
				arm := facts.RustArm{Match: id, Index: index, Scrutinee: scrutinee}
				index++
				if pat := a.Fields["pattern"]; pat != nil {
					cond := pat.Fields["condition"]
					arm.Guard = cond != nil
					arm.Pattern = compact(pat.Text)
					for _, ch := range pat.Children {
						if ch != cond && ch.Kind != "line_comment" && ch.Kind != "block_comment" {
							arm.Pattern = compact(ch.Text)
							break
						}
					}
				}
				path := append(append([]facts.RustArm(nil), arms...), arm)
				out = append(out, fc.results(a.Fields["value"], path)...)
			}
		}
		return out
	case "return_expression":
		return nil
	case "unsafe_block", "async_block":
		if len(n.Children) > 0 {
			return fc.results(n.Children[len(n.Children)-1], arms)
		}
	}
	return []facts.RustValue{withArms(fc.value(n, 0), arms, 0)}
}

func withArms(v facts.RustValue, arms []facts.RustArm, l int) facts.RustValue {
	if len(arms) > 0 {
		v.Arms = append([]facts.RustArm(nil), arms...)
	}
	if v.Line == 0 {
		v.Line = l
	}
	return v
}

// returns collects the `return` expressions of a body, outside closures
// and nested items. A return inside a match arm keeps no arm: it is read
// as reachable from every arm, which over-approximates and never narrows.
func (fc *fnCapture) returns(n *parse.Node) []facts.RustValue {
	var out []facts.RustValue
	var visit func(*parse.Node)
	visit = func(n *parse.Node) {
		switch {
		case n.Kind == "closure_expression" || n.Kind == "async_block" || strings.HasSuffix(n.Kind, "_item"):
			return
		case n.Kind == "try_expression":
			fc.fn.Try = true
		case n.Kind == "return_expression":
			if len(n.Children) == 0 {
				out = append(out, withArms(unitValue, nil, line(n)))
			} else {
				out = append(out, fc.results(n.Children[0], nil)...)
			}
		}
		for _, ch := range n.Children {
			visit(ch)
		}
	}
	visit(n)
	return out
}

// bindings records every name a pattern of the body binds (lets of any
// pattern, match arms, if let, while let, for, closure parameters) and
// the names of nested function items. Macro bodies are opaque.
func (fc *fnCapture) bindings(n *parse.Node) {
	if n == nil || n.Kind == "macro_invocation" {
		return
	}
	add := func(p *parse.Node) {
		patternNames(p, &fc.fn.Bindings)
	}
	switch {
	case strings.HasSuffix(n.Kind, "_item"):
		if name := n.Fields["name"]; name != nil && n.Kind == "function_item" {
			fc.fn.Bindings = append(fc.fn.Bindings, name.Text)
		}
		return
	case n.Kind == "closure_expression":
		add(n.Fields["parameters"])
	case n.Kind == "let_declaration", n.Kind == "let_condition", n.Kind == "for_expression":
		add(n.Fields["pattern"])
	case n.Kind == "match_arm":
		add(n.Fields["pattern"])
	}
	for _, ch := range n.Children {
		fc.bindings(ch)
	}
}

// unwrapMethods pass a value on unchanged as far as its reads go: the
// Option and Result plumbing around `req.headers()` (axum 0.3) and clones.
var unwrapMethods = map[string]bool{"unwrap": true, "expect": true, "ok_or": true, "ok_or_else": true, "as_ref": true, "as_mut": true, "clone": true, "borrow": true}

// closureMethods hand the (unwrapped) value to a closure argument.
var closureMethods = map[string]bool{"and_then": true, "map": true, "map_or": true, "map_or_else": true, "inspect": true, "filter": true}

// root traces an expression back to a parameter value: through
// references, `?`, `.await`, unwrapping methods, names bound from it, and
// the request's headers (`x.headers()`, `x.headers`).
func (fc *fnCapture) root(n *parse.Node) (shapeRoot, bool) {
	for n != nil {
		switch n.Kind {
		case "parenthesized_expression", "reference_expression", "await_expression", "try_expression":
			if v := n.Fields["value"]; v != nil {
				n = v
			} else if len(n.Children) > 0 {
				n = n.Children[len(n.Children)-1]
			} else {
				return shapeRoot{}, false
			}
		case "identifier", "self":
			r, ok := fc.env[n.Text]
			return r, ok
		case "field_expression":
			if facts.Text(n.Fields["field"]) != "headers" {
				return shapeRoot{}, false
			}
			r, ok := fc.root(n.Fields["value"])
			if !ok || r.via != "" {
				return shapeRoot{}, false
			}
			return shapeRoot{r.value, ".headers"}, true
		case "call_expression":
			callee := n.Fields["function"]
			if callee == nil || callee.Kind != "field_expression" {
				return shapeRoot{}, false
			}
			name := facts.Text(callee.Fields["field"])
			args := n.Fields["arguments"]
			if name == "headers" && (args == nil || len(args.Children) == 0) {
				r, ok := fc.root(callee.Fields["value"])
				if !ok || r.via != "" {
					return shapeRoot{}, false
				}
				return shapeRoot{r.value, "headers()"}, true
			}
			if !unwrapMethods[name] {
				return shapeRoot{}, false
			}
			n = callee.Fields["value"]
		default:
			return shapeRoot{}, false
		}
	}
	return shapeRoot{}, false
}

// formatMacros only format their arguments (std, log and tracing).
var formatMacros = map[string]bool{"format": true, "print": true, "println": true, "eprint": true, "eprintln": true, "write": true, "writeln": true,
	"trace": true, "debug": true, "info": true, "warn": true, "error": true}

// formattingOnly reports a macro named like a formatting macro whose arguments, outside
// string literals, hold no call, method, field or `?`: its values are
// only formatted.
func formattingOnly(n *parse.Node) bool {
	name := facts.Text(n.Fields["macro"])
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	if !formatMacros[name] {
		return false
	}
	text := ""
	for _, ch := range n.Children {
		if ch.Kind == "token_tree" {
			text = ch.Text
		}
	}
	var b strings.Builder
	inString := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case !inString:
			b.WriteByte(c)
		}
	}
	code := b.String()
	return strings.Count(code, "(") <= 1 && !strings.ContainsAny(code, "?.[{")
}

// bare records a use of a parameter value that is neither a method call,
// a field read nor a call argument (returned, put in a struct, used in a
// macro), once per value and path: the linker reads it as a use of
// anything.
func (fc *fnCapture) bare(r shapeRoot, n *parse.Node) {
	fc.bareAs(r, n, "*")
}

// bareAs is bare with the use's method: "*", or "macro:<path>" for a
// value passed to a formatting macro.
func (fc *fnCapture) bareAs(r shapeRoot, n *parse.Node, method string) {
	key := r.value + "\x00" + r.via + "\x00" + method
	if fc.bareSeen[key] {
		return
	}
	if fc.bareSeen == nil {
		fc.bareSeen = map[string]bool{}
	}
	fc.bareSeen[key] = true
	fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: r.value, Via: r.via, Method: method, Line: line(n), Scope: fc.fn.Scope})
}

// rebound records a pattern that binds the name of a traced value again
// (a match arm, `if let`, `for`, a closure parameter, a destructuring
// let): the name may then hold another value, so the linker reads it as
// a reassignment. The trace is kept, which over-reads and never misses a
// use.
func (fc *fnCapture) rebound(pattern *parse.Node, at *parse.Node) {
	var names []string
	patternNames(pattern, &names)
	for _, name := range names {
		if r, ok := fc.env[name]; ok {
			fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: r.value, Via: r.via, Method: "=", Line: line(at), Scope: fc.fn.Scope})
		}
	}
}

// uses records the reads and calls of parameter values in a body.
func (fc *fnCapture) uses(n *parse.Node) {
	if n == nil || strings.HasSuffix(n.Kind, "_item") {
		return
	}
	switch n.Kind {
	case "match_arm", "let_condition", "for_expression":
		fc.rebound(n.Fields["pattern"], n)
	case "closure_expression":
		fc.rebound(n.Fields["parameters"], n)
	case "let_declaration":
		pat := n.Fields["pattern"]
		value := n.Fields["value"]
		if pat != nil && pat.Kind != "identifier" {
			fc.rebound(pat, n)
		}
		if pat != nil && pat.Kind == "identifier" {
			if value != nil {
				l := facts.RustLet{Name: pat.Text, Value: fc.value(value, 0), Line: line(n)}
				if t := n.Fields["type"]; t != nil {
					st := shapeType(t)
					l.Type = &st
				}
				fc.fn.Lets = append(fc.fn.Lets, l)
			}
			r, ok := fc.root(value)
			if prev, traced := fc.env[pat.Text]; traced && (!ok || prev != r) {
				// A traced name bound again to another value: earlier
				// and later uses may be of either.
				fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: prev.value, Via: prev.via, Method: "=", Line: line(n), Scope: fc.fn.Scope})
			}
			if ok && value != nil {
				// An alias of a request value: its uses are the alias's.
				fc.env[pat.Text] = r
				return
			}
			fc.uses(value)
			delete(fc.env, pat.Text)
			return
		}
	case "assignment_expression", "compound_assignment_expr":
		// A request value reassigned no longer holds the request's parts.
		if left := n.Fields["left"]; left != nil && left.Kind == "identifier" {
			if r, ok := fc.env[left.Text]; ok {
				fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: r.value, Via: r.via, Method: "=", Line: line(n), Scope: fc.fn.Scope})
				fc.uses(n.Fields["right"])
				delete(fc.env, left.Text)
				return
			}
		}
	case "field_expression":
		// A field of a request value other than its headers (`parts.uri`)
		// is a read the linker cannot name.
		if field := facts.Text(n.Fields["field"]); field != "headers" {
			if r, ok := fc.root(n.Fields["value"]); ok {
				fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: r.value, Via: r.via, Method: "." + field, Line: line(n), Scope: fc.fn.Scope})
				return
			}
		} else if r, ok := fc.root(n); ok {
			// The headers used other than by a method call (iterated,
			// borrowed into a struct): a use the linker cannot name.
			fc.bare(r, n)
			return
		}
	case "identifier":
		if r, ok := fc.env[n.Text]; ok {
			fc.bare(r, n)
		}
		return
	case "macro_invocation":
		if formattingOnly(n) {
			// A formatting macro by name: its values are only formatted
			// if the linker proves the macro is std's, log's or
			// tracing's (row SH-X14); the use records the macro path.
			name := "macro:" + compact(facts.Text(n.Fields["macro"]))
			var visit func(*parse.Node)
			visit = func(m *parse.Node) {
				if m.Kind == "identifier" {
					if r, ok := fc.env[m.Text]; ok {
						fc.bareAs(r, m, name)
					}
				}
				for _, ch := range m.Children {
					visit(ch)
				}
			}
			for _, ch := range n.Children {
				if ch != n.Fields["macro"] {
					visit(ch)
				}
			}
			return
		}
	case "call_expression":
		callee := n.Fields["function"]
		if callee != nil && callee.Kind == "generic_function" {
			callee = callee.Fields["function"]
		}
		args := n.Fields["arguments"]
		var argNodes []*parse.Node
		if args != nil {
			for _, a := range args.Children {
				if a.Kind != "line_comment" && a.Kind != "block_comment" {
					argNodes = append(argNodes, a)
				}
			}
		}
		if callee != nil && callee.Kind == "field_expression" {
			name := facts.Text(callee.Fields["field"])
			recv := callee.Fields["value"]
			if r, ok := fc.root(recv); ok && name == "headers" && r.via == "" && len(argNodes) == 0 {
				fc.bare(shapeRoot{r.value, "headers()"}, n)
				return
			}
			if r, ok := fc.root(recv); ok && !unwrapMethods[name] {
				if closureMethods[name] && len(argNodes) > 0 && argNodes[0].Kind == "closure_expression" {
					if ps := argNodes[0].Fields["parameters"]; ps != nil && len(ps.Children) == 1 && ps.Children[0].Kind == "identifier" {
						fc.env[ps.Children[0].Text] = r
						fc.uses(argNodes[0].Fields["body"])
						return
					}
				}
				u := facts.RustUse{Value: r.value, Via: r.via, Method: name, Line: line(callee.Fields["field"]), Scope: fc.fn.Scope}
				for _, a := range argNodes {
					u.Args = append(u.Args, fc.value(a, 1))
				}
				fc.fn.Uses = append(fc.fn.Uses, u)
				for _, a := range argNodes {
					fc.uses(a)
				}
				return
			}
		}
		for i, a := range argNodes {
			if r, ok := fc.root(a); ok {
				callee := fc.value(n, 0)
				callee.Args = nil
				fc.fn.Uses = append(fc.fn.Uses, facts.RustUse{Value: r.value, Via: r.via, Callee: &callee, Arg: i, Line: line(n), Scope: fc.fn.Scope})
			}
		}
		// The callee of a method call is no field read: visit its
		// receiver. An argument traced to a request value is the call use
		// recorded above.
		if fn := n.Fields["function"]; fn != nil && fn.Kind == "field_expression" {
			fc.uses(fn.Fields["value"])
		} else {
			fc.uses(fn)
		}
		for _, a := range argNodes {
			if _, ok := fc.root(a); !ok {
				fc.uses(a)
			}
		}
		return
	}
	for _, ch := range n.Children {
		fc.uses(ch)
	}
}

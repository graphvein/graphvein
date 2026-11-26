package rust

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Version 3 of the Tauri payload adds the syntax the linker needs to decide
// app scope and test-only commands (T015c2 Revision 2): app sites that may
// belong to another app, the outer attributes of a handler's function, and
// cfg(test) regions. All of it is syntax; the resolver decides at link time.

// prepare records, in one walk, every function's outer attribute items and
// the byte ranges of items and statements whose cfg holds only in test
// builds (an outer `#[cfg(..)]` before them, or an inner `#![cfg(..)]` at the
// top of a file or module body).
func (c *tauriCapture) prepare(n *parse.Node) {
	if n == nil || n.Kind == "macro_invocation" || n.Kind == "macro_definition" {
		return
	}
	var attrs []*parse.Node
	for _, child := range n.Children {
		switch child.Kind {
		case "attribute_item":
			attrs = append(attrs, child)
			continue
		case "inner_attribute_item":
			if facts.CfgTestOnly(strings.Replace(compact(child.Text), "#![", "#[", 1)) {
				c.testRegions = append(c.testRegions, [2]uint32{n.Start, n.End})
			}
			continue
		case "line_comment", "block_comment":
			continue
		}
		for _, a := range attrs {
			if facts.CfgTestOnly(compact(a.Text)) {
				c.testRegions = append(c.testRegions, [2]uint32{a.Start, child.End})
			}
		}
		if child.Kind == "function_item" && len(attrs) > 0 {
			c.fnAttrs[child] = attrs
		}
		attrs = nil
		c.prepare(child)
	}
}

// inTest reports whether offset lies in a cfg(test)-only region.
func (c *tauriCapture) inTest(offset uint32) bool {
	for _, r := range c.testRegions {
		if r[0] <= offset && offset < r[1] {
			return true
		}
	}
	return false
}

// attributePaths lists a function's outer attributes as Kind "attribute"
// records: each item's path, followed by the attribute paths a cfg_attr
// would apply (nested cfg_attr included), so a consumer can check every
// attribute that may run on the function.
func (c *tauriCapture) attributePaths(fn *parse.Node) []facts.TauriMacro {
	var out []facts.TauriMacro
	for _, item := range c.fnAttrs[fn] {
		span := facts.Span(item)
		scope := c.scope(span)
		var meta *parse.Node
		for _, child := range item.Children {
			if child.Kind == "attribute" {
				meta = child
			}
		}
		if meta == nil || len(meta.Children) == 0 {
			// An attribute the parser cannot read is recorded so the
			// consumer treats it as unknown.
			out = append(out, facts.TauriMacro{Path: "?", Scope: scope, Span: span, Kind: "attribute"})
			continue
		}
		name := compact(meta.Children[0].Text)
		out = append(out, facts.TauriMacro{Path: name, Scope: scope, Span: span, Kind: "attribute"})
		if name == "cfg_attr" {
			c.cfgAttrPaths(meta.Fields["arguments"], scope, span, &out)
		}
	}
	return out
}

func (c *tauriCapture) cfgAttrPaths(tree *parse.Node, scope int, span contract.Span, out *[]facts.TauriMacro) {
	items := splitCommas(c.tokens(tree))
	if len(items) < 2 {
		*out = append(*out, facts.TauriMacro{Path: "?", Scope: scope, Span: span, Kind: "attribute"})
		return
	}
	for _, inner := range items[1:] {
		if len(inner) == 0 {
			continue
		}
		segments, rooted, n := pathTokens(inner)
		if segments == nil {
			*out = append(*out, facts.TauriMacro{Path: "?", Scope: scope, Span: span, Kind: "attribute"})
			continue
		}
		path := joinPath(segments, rooted)
		*out = append(*out, facts.TauriMacro{Path: path, Scope: scope, Span: c.span(inner[0].start, inner[len(inner)-1].end), Kind: "attribute"})
		if path == "cfg_attr" && n < len(inner) && isGroup(inner[n], '(') {
			c.cfgAttrPaths(inner[n].node, scope, span, out)
		}
	}
}

// appSites records the syntax that may belong to a Tauri app (Revision 3,
// MA2): every value-position path through a Builder-like name (`root`), a
// let, return or field whose type is a Builder-like path and whose value is
// a `default` / `new` call not already recorded (`typed`), a type alias of a
// Builder-like path (`alias`), a visible re-export of a Builder-like name or
// a glob (`reexport`), a macro_rules! body or macro invocation token tree
// that names a Builder-like name (`macro`), invoke_handler calls without a
// generate_handler! argument and generate_context! invocations (also inside
// token trees). Builder-like names are `Builder` and the file's use aliases
// of it; type positions in signatures are not recorded. parent is n's
// parent, so a callee path records the whole call's span (the span a proof
// root has).
func (c *tauriCapture) appSites(n, parent *parse.Node, builders map[string]bool) {
	if n == nil {
		return
	}
	add := func(kind, path string, at *parse.Node, span contract.Span) {
		for _, a := range c.out.Apps {
			if a.Kind == kind && a.Path == path && a.Span == span {
				return
			}
		}
		c.out.Apps = append(c.out.Apps, facts.TauriAppSite{Kind: kind, Path: path, Scope: c.scope(span), Span: span, Test: c.inTest(at.Start)})
	}
	switch n.Kind {
	case "attribute_item", "inner_attribute_item":
		return
	case "macro_definition":
		c.macroBuilders(n, builders, add)
		return
	case "use_declaration":
		visible := false
		for _, child := range n.Children {
			visible = visible || child.Kind == "visibility_modifier"
		}
		if visible {
			for _, r := range useReexports(n.Fields["argument"], "", builders) {
				add("reexport", r, n, facts.Span(n))
			}
		}
		return
	case "type_item":
		if p := builderType(n.Fields["type"], builders); p != "" {
			add("alias", p, n, facts.Span(n))
		}
		return
	case "field_declaration":
		if p := builderType(n.Fields["type"], builders); p != "" {
			add("typed", p, n, facts.Span(n))
		}
		return
	case "ordered_field_declaration_list":
		for _, child := range n.Children {
			if p := builderType(child, builders); p != "" {
				add("typed", p, child, facts.Span(child))
			}
		}
		return
	case "let_declaration":
		if p := builderType(n.Fields["type"], builders); p != "" {
			if v := n.Fields["value"]; constructorCall(v, builders) {
				add("typed", p, v, facts.Span(v))
			}
		}
	case "function_item":
		if p := builderType(n.Fields["return_type"], builders); p != "" {
			for _, v := range returnedValues(n.Fields["body"]) {
				if constructorCall(v, builders) {
					add("typed", p, v, facts.Span(v))
				}
			}
		}
	case "macro_invocation":
		path := compact(facts.Text(n.Fields["macro"]))
		if lastSegment(path) == "generate_context" {
			add("context", path, n, facts.Span(n))
		}
		for _, child := range n.Children {
			if child.Kind != "token_tree" {
				continue
			}
			var nested []facts.TauriMacro
			c.nestedMacros(child, 0, &nested)
			for _, m := range nested {
				if lastSegment(m.Path) == "generate_context" {
					add("context", m.Path, n, m.Span)
				}
			}
		}
		c.macroBuilders(n, builders, add)
		return
	case "call_expression":
		captured := false
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if _, ok := c.handlerOf[a]; ok {
					captured = true
				}
			}
		}
		if field := methodCall(n); field != nil {
			if facts.Text(field.Fields["field"]) == "invoke_handler" && !captured {
				add("invoke_handler", "invoke_handler", n, facts.Span(n))
			}
		} else if callee := n.Fields["function"]; callee != nil {
			path := rustPath(callee)
			segments := strings.Split(strings.TrimPrefix(path, "::"), "::")
			if segments[len(segments)-1] == "invoke_handler" && !captured {
				add("invoke_handler", path, n, facts.Span(n))
			}
		}
	case "identifier", "scoped_identifier", "generic_function":
		if p := builderPath(pathSegments(n), builders); p != "" {
			span := facts.Span(n)
			if parent != nil && parent.Kind == "call_expression" && parent.Fields["function"] == n {
				span = facts.Span(parent)
			}
			add("root", p, n, span)
		}
		if n.Kind == "generic_function" {
			// The function path is the value; the type arguments are types.
			if f := n.Fields["function"]; f != nil && f.Kind == "field_expression" {
				c.appSites(f, n, builders)
			}
		}
		return
	}
	for _, child := range n.Children {
		c.appSites(child, n, builders)
	}
}

// pathSegments returns the segments of a path in value or type position,
// generic arguments removed and qualified paths (`<T>::f`, `<T as Tr>::f`)
// read through their type; a leading "" marks a rooted `::path`. A form it
// does not read gives nil.
func pathSegments(n *parse.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "identifier", "type_identifier", "crate", "self", "super", "metavariable":
		return []string{n.Text}
	case "scoped_identifier", "scoped_type_identifier":
		name := facts.Text(n.Fields["name"])
		if p := n.Fields["path"]; p != nil {
			prefix := pathSegments(p)
			if prefix == nil {
				return nil
			}
			return append(prefix, name)
		}
		if strings.HasPrefix(n.Text, "::") {
			return []string{"", name}
		}
		return []string{name}
	case "generic_type":
		return pathSegments(n.Fields["type"])
	case "generic_function":
		return pathSegments(n.Fields["function"])
	case "qualified_type":
		return pathSegments(n.Fields["type"])
	case "bracketed_type":
		for _, child := range n.Children {
			if child.Kind != "<" && child.Kind != ">" {
				return pathSegments(child)
			}
		}
	}
	return nil
}

// builderPath returns the path through the first Builder-like segment, or "".
func builderPath(segments []string, builders map[string]bool) string {
	for i, s := range segments {
		if builders[s] {
			return strings.Join(segments[:i+1], "::")
		}
	}
	return ""
}

// builderType returns the Builder-like path of a type that is directly a
// path (`tauri::Builder<Wry>`, `App`), or "". Wrapped types (`Option<..>`,
// references) hold no builder value of their own.
func builderType(t *parse.Node, builders map[string]bool) string {
	if t == nil {
		return ""
	}
	switch t.Kind {
	case "type_identifier", "scoped_type_identifier", "generic_type", "scoped_identifier", "identifier":
		return builderPath(pathSegments(t), builders)
	}
	return ""
}

// constructorCall reports whether v is a call whose callee ends in
// `default` or `new` (`Default::default()`, `<_>::default()`, `T::new()`) and
// names no Builder-like segment itself (that call is a root site already).
func constructorCall(v *parse.Node, builders map[string]bool) bool {
	if v == nil || v.Kind != "call_expression" {
		return false
	}
	callee := v.Fields["function"]
	if callee != nil && callee.Kind == "generic_function" {
		callee = callee.Fields["function"]
	}
	if callee == nil || callee.Kind != "identifier" && callee.Kind != "scoped_identifier" {
		return false
	}
	name := callee.Text
	if callee.Kind == "scoped_identifier" {
		name = facts.Text(callee.Fields["name"])
	}
	return (name == "default" || name == "new") && builderPath(pathSegments(callee), builders) == ""
}

// returnedValues lists the expressions a function body may return: its tail
// expression (through blocks, if/else, match arms and parentheses) and the
// operand of every return outside nested closures and items.
func returnedValues(body *parse.Node) []*parse.Node {
	var out []*parse.Node
	var tail func(*parse.Node)
	tail = func(e *parse.Node) {
		if e == nil {
			return
		}
		switch e.Kind {
		case "block":
			var last *parse.Node
			for _, child := range e.Children {
				if child.Kind != "line_comment" && child.Kind != "block_comment" && child.Kind != "{" && child.Kind != "}" {
					last = child
				}
			}
			switch {
			case last == nil || strings.HasSuffix(last.Kind, "_declaration") || strings.HasSuffix(last.Kind, "_item"):
			case last.Kind == "expression_statement":
				// A block-like expression (if, match) at the end of a block
				// is an expression statement without a semicolon.
				if !strings.HasSuffix(strings.TrimSpace(last.Text), ";") {
					for _, child := range last.Children {
						tail(child)
					}
				}
			default:
				tail(last)
			}
		case "if_expression":
			tail(e.Fields["consequence"])
			tail(e.Fields["alternative"])
		case "else_clause", "parenthesized_expression", "unsafe_block":
			for _, child := range e.Children {
				tail(child)
			}
		case "match_expression":
			if b := e.Fields["body"]; b != nil {
				for _, arm := range b.Children {
					if arm.Kind == "match_arm" {
						tail(arm.Fields["value"])
					}
				}
			}
		case "return_expression":
			for _, child := range e.Children {
				tail(child)
			}
		default:
			out = append(out, e)
		}
	}
	tail(body)
	var returns func(*parse.Node)
	returns = func(n *parse.Node) {
		if n == nil || n.Kind == "closure_expression" || n.Kind == "function_item" || n.Kind == "macro_invocation" {
			return
		}
		if n.Kind == "return_expression" {
			tail(n)
		}
		for _, child := range n.Children {
			returns(child)
		}
	}
	if body != nil {
		for _, child := range body.Children {
			returns(child)
		}
	}
	return out
}

// useReexports lists, for a visible use tree, the paths that re-export a
// Builder-like name (full path) or a glob (`prefix::*`) whose prefix does
// not start with crate, self or super. Every such re-export is app
// evidence once it resolves to tauri (conservative, MA2).
func useReexports(n *parse.Node, prefix string, builders map[string]bool) []string {
	if n == nil {
		return nil
	}
	join := func(a, b string) string {
		if a == "" {
			return b
		}
		return a + "::" + b
	}
	switch n.Kind {
	case "identifier", "scoped_identifier":
		full := join(prefix, compact(n.Text))
		if builders[lastSegment(full)] {
			return []string{full}
		}
	case "use_as_clause":
		return useReexports(n.Fields["path"], prefix, builders)
	case "scoped_use_list":
		inner := join(prefix, compact(facts.Text(n.Fields["path"])))
		return useReexports(n.Fields["list"], inner, builders)
	case "use_list":
		var out []string
		for _, child := range n.Children {
			out = append(out, useReexports(child, prefix, builders)...)
		}
		return out
	case "use_wildcard":
		full := join(prefix, compact(n.Text))
		base := strings.TrimSuffix(strings.TrimSuffix(full, "*"), "::")
		first := strings.Split(strings.TrimPrefix(base, "::"), "::")[0]
		if base != "" && first != "crate" && first != "self" && first != "super" {
			return []string{base + "::*"}
		}
	}
	return nil
}

// macroBuilders records a macro_rules! body or macro invocation whose token
// trees name a Builder-like identifier: its adjacent path (`tauri::Builder`,
// `B`), and `tauri::Builder` when the trees also name `tauri`, so a builder
// written inside a macro is still app evidence. The macro's own span and
// scope are recorded; macro hygiene and expansion are not modelled.
func (c *tauriCapture) macroBuilders(n *parse.Node, builders map[string]bool, add func(kind, path string, at *parse.Node, span contract.Span)) {
	var paths []string
	mentionsTauri := false
	var scan func(tree *parse.Node)
	scan = func(tree *parse.Node) {
		tokens := c.tokens(tree)
		for i, t := range tokens {
			switch {
			case t.kind == "group":
				scan(t.node)
			case t.kind == "ident" && t.text == "tauri":
				mentionsTauri = true
			case t.kind == "ident" && builders[t.text]:
				segments := []string{t.text}
				for j := i - 1; j >= 1 && isPunct(tokens[j], "::") && tokens[j-1].kind == "ident"; j -= 2 {
					segments = append([]string{tokens[j-1].text}, segments...)
				}
				paths = append(paths, strings.Join(segments, "::"))
			}
		}
	}
	var trees func(*parse.Node)
	trees = func(x *parse.Node) {
		for _, child := range x.Children {
			if child.Kind == "token_tree" {
				scan(child)
			} else if child.Kind == "macro_rule" {
				trees(child)
			}
		}
	}
	trees(n)
	if len(paths) > 0 && mentionsTauri {
		paths = append(paths, "tauri::Builder")
	}
	for _, p := range paths {
		add("macro", p, n, facts.Span(n))
	}
}

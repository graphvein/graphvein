package rust

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Tauri capture keeps syntax only. Its filters are the cheapest syntactic tests
// that keep every real target (an attribute whose last path segment is
// `command`, a macro whose last segment is `generate_handler`); whether a
// record is really Tauri's is proved later by the Rust resolver.

// token is one token of a macro or attribute token tree. Named parser nodes
// (identifiers, literals, nested trees) keep their node; punctuation and
// keywords, which the parser leaves anonymous, are lexed from the source gaps.
type token struct {
	kind       string // ident, literal, group, punct, word, other
	text       string
	start, end uint32
	node       *parse.Node
}

// tauriCapture holds per-file state for one capture pass.
type tauriCapture struct {
	file      string
	source    []byte
	lines     []uint32
	scopes    []facts.RustScope
	nodes     map[contract.Span]string
	aliases   map[string]bool
	out       facts.TauriSyntax
	handlerOf map[*parse.Node]int
	// owners marks function nodes that contain a captured handler.
	owners map[*parse.Node]bool
	// calls records, per function node, the identifier-rooted chains that
	// call invoke_handler (captured or not), once per outermost call.
	calls     map[*parse.Node][]flowCall
	seen      map[*parse.Node]bool
	functions []*parse.Node
	// fnAttrs holds each function's outer attribute items and testRegions
	// the byte ranges whose cfg holds only in test builds (version 3).
	fnAttrs     map[*parse.Node][]*parse.Node
	testRegions [][2]uint32
}

// flowCall is one recorded chain with its outermost call node, for ordering.
type flowCall struct {
	top  *parse.Node
	call facts.TauriCall
}

func captureTauri(file string, source []byte, root *parse.Node, emitted facts.Facts) *facts.TauriSyntax {
	c := &tauriCapture{file: file, source: source, nodes: map[contract.Span]string{}, aliases: map[string]bool{}, handlerOf: map[*parse.Node]int{}, owners: map[*parse.Node]bool{}, calls: map[*parse.Node][]flowCall{}, seen: map[*parse.Node]bool{}, fnAttrs: map[*parse.Node][]*parse.Node{}}
	c.lines = []uint32{0}
	for i, b := range source {
		if b == '\n' {
			c.lines = append(c.lines, uint32(i+1))
		}
	}
	if emitted.Rust != nil {
		c.scopes = emitted.Rust.Scopes
	}
	for _, n := range emitted.Nodes {
		if n.Span != nil && (n.Kind == "function" || n.Kind == "method") {
			c.nodes[*n.Span] = n.Identity
		}
	}
	// builders holds the Builder-like names of the file: Builder itself and
	// every use alias of it, transitively (`use tauri::Builder as B; use B as
	// C;`). Only resolution at link time decides that one is Tauri's.
	builders := map[string]bool{"Builder": true}
	for _, imp := range emitted.Imports {
		for alias, path := range imp.Bindings {
			if lastSegment(path) == "command" {
				c.aliases[alias] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, imp := range emitted.Imports {
			for alias, path := range imp.Bindings {
				if alias != "*" && alias != "_" && !builders[alias] && builders[lastSegment(path)] {
					builders[alias] = true
					changed = true
				}
			}
		}
	}
	c.prepare(root)
	// Handlers first, so chains can reference handlers by index.
	c.collectHandlers(root, nil)
	c.walkCommands(root)
	c.contexts(root, nil)
	c.flows()
	c.appSites(root, nil, builders)
	if len(c.out.Commands)+len(c.out.Handlers)+len(c.out.Flows)+len(c.out.Apps) == 0 {
		return nil
	}
	c.out.Version = facts.TauriSyntaxVersion
	return &c.out
}

// compactCode removes whitespace outside string literals, so an entry's cfg
// keeps `"a b"` distinct from `"ab"` (complementary arm pairs compare
// predicates as text, T015c2 Revision 3).
func compactCode(s string) string {
	var b strings.Builder
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted && c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			i++
			c = s[i]
		case c == '"':
			quoted = !quoted
		case !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'):
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func lastSegment(path string) string {
	parts := strings.Split(path, "::")
	return parts[len(parts)-1]
}

func (c *tauriCapture) span(start, end uint32) contract.Span {
	point := func(offset uint32) (int, int) {
		line := sort.Search(len(c.lines), func(i int) bool { return c.lines[i] > offset }) - 1
		return line + 1, int(offset - c.lines[line])
	}
	sl, sc := point(start)
	el, ec := point(end)
	return contract.Span{StartLine: sl, StartColumn: sc, EndLine: el, EndColumn: ec}
}

// scope mirrors ModuleResolver.ScopeAt: the smallest lexical scope containing span.
func (c *tauriCapture) scope(span contract.Span) int {
	best := 0
	for i, s := range c.scopes {
		start := s.Span.StartLine < span.StartLine || s.Span.StartLine == span.StartLine && s.Span.StartColumn <= span.StartColumn
		end := s.Span.EndLine > span.EndLine || s.Span.EndLine == span.EndLine && s.Span.EndColumn >= span.EndColumn
		if start && end && s.End-s.Start <= c.scopes[best].End-c.scopes[best].Start {
			best = i
		}
	}
	return best
}

func (c *tauriCapture) identity(n *parse.Node) string {
	if n == nil {
		return ""
	}
	return c.nodes[facts.Span(n)]
}

// tokens flattens one token tree level, without its outer delimiters.
func (c *tauriCapture) tokens(tree *parse.Node) []token {
	var out []token
	if tree == nil || tree.End-tree.Start < 2 {
		return out
	}
	pos, end := tree.Start+1, tree.End-1
	for _, child := range tree.Children {
		out = append(out, c.lex(pos, child.Start)...)
		pos = child.End
		switch child.Kind {
		case "line_comment", "block_comment":
			continue
		case "identifier", "crate", "self", "super":
			out = append(out, token{kind: "ident", text: child.Text, start: child.Start, end: child.End, node: child})
		case "string_literal", "raw_string_literal", "char_literal", "integer_literal", "float_literal", "boolean_literal":
			out = append(out, token{kind: "literal", text: child.Text, start: child.Start, end: child.End, node: child})
		case "token_tree":
			out = append(out, token{kind: "group", text: child.Text, start: child.Start, end: child.End, node: child})
		default:
			out = append(out, token{kind: "other", text: child.Text, start: child.Start, end: child.End, node: child})
		}
	}
	return append(out, c.lex(pos, end)...)
}

// lex splits source gaps between named nodes into keywords and punctuation.
func (c *tauriCapture) lex(start, end uint32) []token {
	var out []token
	if end > uint32(len(c.source)) || start >= end {
		return out
	}
	for i := start; i < end; {
		b := c.source[i]
		switch {
		case b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f':
			i++
		case b == ':' && i+1 < end && c.source[i+1] == ':':
			out = append(out, token{kind: "punct", text: "::", start: i, end: i + 2})
			i += 2
		case b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z':
			j := i
			for j < end && (c.source[j] == '_' || c.source[j] >= 'a' && c.source[j] <= 'z' || c.source[j] >= 'A' && c.source[j] <= 'Z' || c.source[j] >= '0' && c.source[j] <= '9') {
				j++
			}
			out = append(out, token{kind: "word", text: string(c.source[i:j]), start: i, end: j})
			i = j
		default:
			_, size := utf8.DecodeRune(c.source[i:end])
			out = append(out, token{kind: "punct", text: string(c.source[i : i+uint32(size)]), start: i, end: i + uint32(size)})
			i += uint32(size)
		}
	}
	return out
}

// splitCommas splits tokens at top-level commas; nested trees are single tokens.
func splitCommas(tokens []token) [][]token {
	items := [][]token{nil}
	for _, t := range tokens {
		if t.kind == "punct" && t.text == "," {
			items = append(items, nil)
			continue
		}
		items[len(items)-1] = append(items[len(items)-1], t)
	}
	return items
}

func isPunct(t token, text string) bool { return t.kind == "punct" && t.text == text }

func isGroup(t token, open byte) bool {
	return t.kind == "group" && len(t.text) > 0 && t.text[0] == open
}

// pathTokens reads `::? ident (:: ident)*` from the start of tokens.
func pathTokens(tokens []token) (segments []string, rooted bool, n int) {
	if n < len(tokens) && isPunct(tokens[n], "::") {
		rooted = true
		n++
	}
	for n < len(tokens) && tokens[n].kind == "ident" {
		segments = append(segments, tokens[n].text)
		n++
		if n+1 < len(tokens) && isPunct(tokens[n], "::") && tokens[n+1].kind == "ident" {
			n++
			continue
		}
		break
	}
	if len(segments) == 0 {
		return nil, false, 0
	}
	return segments, rooted, n
}

func joinPath(segments []string, rooted bool) string {
	p := strings.Join(segments, "::")
	if rooted {
		return "::" + p
	}
	return p
}

func (c *tauriCapture) qualifies(p string) bool {
	return lastSegment(p) == "command" || !strings.Contains(p, "::") && c.aliases[p]
}

// walkCommands visits every item list, tracking each item's outer attributes.
func (c *tauriCapture) walkCommands(n *parse.Node) {
	if n == nil || n.Kind == "macro_invocation" || n.Kind == "macro_definition" {
		return
	}
	var attrs []*parse.Node
	for _, child := range n.Children {
		switch child.Kind {
		case "attribute_item":
			attrs = append(attrs, child)
			continue
		case "line_comment", "block_comment":
			continue
		case "function_item":
			c.command(child, attrs)
		}
		attrs = nil
		c.walkCommands(child)
	}
}

func (c *tauriCapture) command(fn *parse.Node, attrs []*parse.Node) {
	cfg := false
	var found []facts.TauriAttribute
	for _, a := range attrs {
		cfg = cfg || c.cfgGated(a)
		found = append(found, c.attribute(a)...)
	}
	if len(found) == 0 {
		return
	}
	record := facts.TauriCommand{Identity: c.identity(fn), Name: facts.Text(fn.Fields["name"]), Span: facts.Span(fn), Attributes: found, Cfg: cfg, Test: c.inTest(fn.Start)}
	for _, child := range fn.Children {
		switch child.Kind {
		case "visibility_modifier":
			record.Visibility = compact(child.Text)
		case "function_modifiers":
			for _, t := range child.Tokens {
				record.AsyncFn = record.AsyncFn || t == "async"
			}
		}
	}
	c.out.Commands = append(c.out.Commands, record)
}

// attribute returns a record for each command-like attribute in an attribute
// item: one for a plain attribute, any number for cfg_attr lists.
func (c *tauriCapture) attribute(item *parse.Node) []facts.TauriAttribute {
	var meta *parse.Node
	for _, child := range item.Children {
		if child.Kind == "attribute" {
			meta = child
		}
	}
	if meta == nil || len(meta.Children) == 0 {
		return nil
	}
	name := compact(meta.Children[0].Text)
	span := facts.Span(item)
	if name == "cfg_attr" {
		return c.cfgAttr(meta.Fields["arguments"], nil, span)
	}
	if !c.qualifies(name) {
		return nil
	}
	a := facts.TauriAttribute{Path: name, Span: span, Form: "attribute", Scope: c.scope(span)}
	if meta.Fields["value"] != nil {
		a.Invalid = "unsupported attribute form"
	} else if args := meta.Fields["arguments"]; args != nil {
		if args.Text[0] != '(' {
			a.Invalid = "unsupported attribute form"
		} else {
			c.arguments(&a, args)
		}
	}
	return []facts.TauriAttribute{a}
}

// cfgGated reports whether an attribute item applies a cfg: `#[cfg(..)]`, or a
// cfg_attr whose attribute list contains cfg(..) at any nesting depth.
func (c *tauriCapture) cfgGated(item *parse.Node) bool {
	if strings.HasPrefix(compact(item.Text), "#[cfg(") {
		return true
	}
	for _, child := range item.Children {
		if child.Kind == "attribute" && len(child.Children) > 0 && compact(child.Children[0].Text) == "cfg_attr" {
			return c.appliesCfg(child.Fields["arguments"])
		}
	}
	return false
}

// appliesCfg reports whether a cfg_attr argument tree lists cfg(..), directly
// or inside a nested cfg_attr.
func (c *tauriCapture) appliesCfg(tree *parse.Node) bool {
	items := splitCommas(c.tokens(tree))
	if len(items) < 2 {
		return false
	}
	for _, inner := range items[1:] {
		if len(inner) != 2 || inner[0].kind != "ident" || !isGroup(inner[1], '(') {
			continue
		}
		if inner[0].text == "cfg" || inner[0].text == "cfg_attr" && c.appliesCfg(inner[1].node) {
			return true
		}
	}
	return false
}

// cfgAttr reads `cfg_attr(<predicate>, <attr>, ...)`. A nested cfg_attr
// applies only when every enclosing predicate holds, so its records carry the
// predicates of every level, outermost first, joined in one `all(p1, p2, ..)`.
func (c *tauriCapture) cfgAttr(tree *parse.Node, outer []string, span contract.Span) []facts.TauriAttribute {
	items := splitCommas(c.tokens(tree))
	if len(items) < 2 || len(items[0]) == 0 {
		return nil
	}
	first, last := items[0][0].start, items[0][len(items[0])-1].end
	predicates := append(append([]string(nil), outer...), normalizeNewlines(strings.TrimSpace(string(c.source[first:last]))))
	predicate := predicates[0]
	if len(predicates) > 1 {
		predicate = "all(" + strings.Join(predicates, ", ") + ")"
	}
	var out []facts.TauriAttribute
	for _, inner := range items[1:] {
		if len(inner) == 2 && inner[0].kind == "ident" && inner[0].text == "cfg_attr" && isGroup(inner[1], '(') {
			out = append(out, c.cfgAttr(inner[1].node, predicates, span)...)
			continue
		}
		segments, rooted, n := pathTokens(inner)
		if segments == nil || !c.qualifies(joinPath(segments, rooted)) {
			continue
		}
		a := facts.TauriAttribute{Path: joinPath(segments, rooted), Span: span, Form: "cfg_attr", Predicate: predicate, Scope: c.scope(span)}
		rest := inner[n:]
		switch {
		case len(rest) == 0:
		case len(rest) == 1 && isGroup(rest[0], '('):
			c.arguments(&a, rest[0].node)
		default:
			a.Invalid = "unsupported attribute form"
		}
		out = append(out, a)
	}
	return out
}

// normalizeNewlines maps CRLF to LF, as rustc does when it loads a source
// file, so a CRLF checkout records the same facts as an LF one.
func normalizeNewlines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// stringValue decodes a string or raw string literal token after newline
// normalization, so multi-line literals decode as rustc sees them.
func stringValue(n *parse.Node) (string, bool) {
	if n == nil || n.Kind != "string_literal" && n.Kind != "raw_string_literal" {
		return "", false
	}
	return StringLiteral(normalizeNewlines(n.Text))
}

// arguments parses `key = literal` and bare `async` items in source order.
// Anything else records the first invalid reason; parsed items stay recorded.
func (c *tauriCapture) arguments(a *facts.TauriAttribute, tree *parse.Node) {
	invalid := func(reason string) {
		if a.Invalid == "" {
			a.Invalid = reason
		}
	}
	items := splitCommas(c.tokens(tree))
	for i, item := range items {
		if len(item) == 0 {
			if i != len(items)-1 {
				invalid("empty argument")
			}
			continue
		}
		if len(item) == 1 && item[0].kind == "word" && item[0].text == "async" {
			a.Async = true
			continue
		}
		segments, rooted, n := pathTokens(item)
		if segments == nil {
			invalid("unrecognized argument")
			continue
		}
		key := joinPath(segments, rooted)
		rest := item[n:]
		switch {
		case len(rest) == 0:
			a.Arguments = append(a.Arguments, facts.TauriArgument{Key: key, Kind: "path"})
			invalid("bare path argument " + key)
		case len(rest) == 1 && rest[0].kind == "group":
			a.Arguments = append(a.Arguments, facts.TauriArgument{Key: key, Kind: "list"})
			invalid("list argument " + key)
		case isPunct(rest[0], "=") && len(rest) == 2 && rest[1].kind == "literal":
			arg := facts.TauriArgument{Key: key, Kind: "literal"}
			if value, ok := stringValue(rest[1].node); ok {
				arg.Kind = "string"
				arg.Value = &value
			}
			a.Arguments = append(a.Arguments, arg)
		case isPunct(rest[0], "="):
			a.Arguments = append(a.Arguments, facts.TauriArgument{Key: key, Kind: "expression"})
			invalid("non-literal value for " + key)
		default:
			invalid("unrecognized argument")
		}
	}
}

// collectHandlers records generate_handler! invocations in source order.
// Macro bodies are opaque token trees, so handlers written inside another
// macro (including macro_rules! bodies) are not captured.
func (c *tauriCapture) collectHandlers(n *parse.Node, function *parse.Node) {
	if n == nil {
		return
	}
	if n.Kind == "function_item" {
		function = n
	}
	if n.Kind == "macro_invocation" {
		macro := compact(facts.Text(n.Fields["macro"]))
		if lastSegment(macro) == "generate_handler" {
			c.handler(n, macro, function)
			if function != nil {
				c.owners[function] = true
			}
		}
		return
	}
	for _, child := range n.Children {
		c.collectHandlers(child, function)
	}
}

func (c *tauriCapture) handler(n *parse.Node, macro string, function *parse.Node) {
	span := facts.Span(n)
	owner := c.identity(function)
	if owner == "" {
		owner = facts.ID("file", c.file, c.file)
	}
	h := facts.TauriHandler{Path: macro, Span: span, Scope: c.scope(span), Owner: owner, Context: facts.TauriContext{Kind: "other"}, Test: c.inTest(n.Start)}
	if function != nil {
		h.Attributes = c.attributePaths(function)
	}
	var tree *parse.Node
	for _, child := range n.Children {
		if child.Kind == "token_tree" {
			tree = child
		}
	}
	if tree == nil {
		h.Invalid = "missing token tree"
	} else {
		c.entries(&h, c.tokens(tree))
	}
	c.handlerOf[n] = len(c.out.Handlers)
	c.out.Handlers = append(c.out.Handlers, h)
}

// entries parses `#![..]* (#[..]* path),* ,?` as generate_handler! does.
func (c *tauriCapture) entries(h *facts.TauriHandler, tokens []token) {
	fail := func(reason string) {
		h.Invalid = reason
		h.Entries = nil
		h.Plugin = ""
	}
	i := 0
	for i+2 < len(tokens) && isPunct(tokens[i], "#") && isPunct(tokens[i+1], "!") && isGroup(tokens[i+2], '[') {
		inner := c.tokens(tokens[i+2].node)
		if len(inner) > 0 && inner[0].kind == "ident" && inner[0].text == "plugin" {
			args := []token{}
			if len(inner) == 2 && isGroup(inner[1], '(') {
				args = c.tokens(inner[1].node)
			}
			if len(inner) != 2 || len(args) != 1 || args[0].kind != "ident" {
				fail("plugin attribute without identifier")
				return
			}
			if h.Plugin == "" {
				h.Plugin = args[0].text
			}
		}
		i += 3
	}
	items := splitCommas(tokens[i:])
	for k, item := range items {
		if len(item) == 0 {
			if k == len(items)-1 {
				continue
			}
			fail("empty entry")
			return
		}
		var attrs []string
		j := 0
		for j+1 < len(item) && isPunct(item[j], "#") && isGroup(item[j+1], '[') {
			attrs = append(attrs, compactCode(string(c.source[item[j].start:item[j+1].end])))
			j += 2
		}
		segments, rooted, n := pathTokens(item[j:])
		if segments == nil || j+n != len(item) {
			fail("unsupported entry syntax")
			return
		}
		start, end := item[j].start, item[len(item)-1].end
		h.Entries = append(h.Entries, facts.TauriEntry{Path: joinPath(segments, rooted), Segments: segments, Span: c.span(start, end), Hash: contract.ContentHash(c.source[start:end]), Attributes: attrs})
	}
}

// methodCall returns the field expression of a method call, else nil.
func methodCall(n *parse.Node) *parse.Node {
	if n == nil || n.Kind != "call_expression" {
		return nil
	}
	callee := n.Fields["function"]
	if callee != nil && callee.Kind == "generic_function" {
		callee = callee.Fields["function"]
	}
	if callee != nil && callee.Kind == "field_expression" {
		return callee
	}
	return nil
}

// soleArgument returns a call's only argument, ignoring comments.
func soleArgument(call *parse.Node) *parse.Node {
	args := call.Fields["arguments"]
	if args == nil {
		return nil
	}
	var only *parse.Node
	for _, a := range args.Children {
		if a.Kind == "line_comment" || a.Kind == "block_comment" {
			continue
		}
		if only != nil {
			return nil
		}
		only = a
	}
	return only
}

// contexts fills each handler's context from its ancestors, and records the
// chains of invoke_handler calls whose argument is not a captured handler.
func (c *tauriCapture) contexts(n *parse.Node, stack []*parse.Node) {
	if n == nil {
		return
	}
	if index, ok := c.handlerOf[n]; ok {
		c.context(index, n, stack)
		return
	}
	stack = append(stack, n)
	if field := methodCall(n); field != nil && facts.Text(field.Fields["field"]) == "invoke_handler" {
		if _, captured := c.handlerOf[soleArgument(n)]; !captured {
			chain, use, top := c.climb(stack, len(stack)-1)
			c.record(stack, top, chain, use)
		}
	}
	for _, child := range n.Children {
		c.contexts(child, stack)
	}
}

func (c *tauriCapture) context(index int, macro *parse.Node, stack []*parse.Node) {
	h := &c.out.Handlers[index]
	k := len(stack) - 1
	if k < 0 {
		return
	}
	parent := stack[k]
	if parent.Kind == "let_declaration" && parent.Fields["value"] == macro {
		h.Context = facts.TauriContext{Kind: "let", Binding: compact(facts.Text(parent.Fields["pattern"]))}
		return
	}
	if parent.Kind != "arguments" || k < 1 {
		return
	}
	call := stack[k-1]
	field := methodCall(call)
	if field == nil || soleArgument(call) != macro {
		return
	}
	chain, use, top := c.climb(stack, k-1)
	h.Context = facts.TauriContext{Kind: "argument", Method: facts.Text(field.Fields["field"]), Chain: &chain, Use: use}
	c.record(stack, top, chain, use)
}

// record keeps an identifier-rooted invoke_handler chain for its function's
// flow, once per outermost call (two handlers on one chain share it).
func (c *tauriCapture) record(stack []*parse.Node, top *parse.Node, chain facts.TauriChain, use string) {
	if chain.Root.Kind != "identifier" || c.seen[top] {
		return
	}
	c.seen[top] = true
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].Kind == "function_item" {
			if _, ok := c.calls[stack[i]]; !ok {
				c.functions = append(c.functions, stack[i])
			}
			c.calls[stack[i]] = append(c.calls[stack[i]], flowCall{top: top, call: facts.TauriCall{Chain: chain, Use: use}})
			return
		}
	}
}

// climb walks from the method call at stack[at] to the outermost call of its
// chain and returns the chain, its consumer and the outermost call. Each step
// is the call's receiver position inside a field expression of a larger
// method call. Parentheses, `?` and `.await` between calls do not end the
// chain; the descent in chain marks `?` and `.await` incomplete.
func (c *tauriCapture) climb(stack []*parse.Node, at int) (facts.TauriChain, string, *parse.Node) {
	top := at
	var consumer, child *parse.Node
	for {
		j := top - 1
		for j >= 0 && wrapper(stack[j].Kind) {
			j--
		}
		if j >= 0 && stack[j].Kind == "field_expression" && stack[j].Fields["value"] == stack[j+1] {
			next := j - 1
			if next >= 0 && stack[next].Kind == "generic_function" && stack[next].Fields["function"] == stack[j] {
				next--
			}
			if next >= 0 && methodCall(stack[next]) == stack[j] {
				top = next
				continue
			}
		}
		if j >= 0 {
			consumer, child = stack[j], stack[j+1]
		}
		break
	}
	chain := c.chain(stack[top])
	use := chainUse(consumer, child)
	if use == "other" {
		chain.Incomplete = true
	}
	return chain, use, stack[top]
}

// wrapper reports expressions a chain passes through: parentheses (transparent)
// and `?` and `.await` (which make the chain incomplete).
func wrapper(kind string) bool {
	return kind == "parenthesized_expression" || kind == "try_expression" || kind == "await_expression"
}

// unwrap returns the expression inside a wrapper, ignoring comments.
func unwrap(n *parse.Node) *parse.Node {
	for _, child := range n.Children {
		if child.Kind != "line_comment" && child.Kind != "block_comment" {
			return child
		}
	}
	return nil
}

// chainUse classifies the node that consumes a chain's value; child is the
// consumer's child on the path to the chain (after any trailing wrappers).
func chainUse(consumer, child *parse.Node) string {
	if consumer == nil {
		return "other"
	}
	switch consumer.Kind {
	case "expression_statement":
		for _, t := range consumer.Tokens {
			if t == ";" {
				return "statement"
			}
		}
		return "tail"
	case "let_declaration":
		if consumer.Fields["value"] == child {
			return "let"
		}
	case "assignment_expression":
		// Only a plain local is a binding a flow can follow; a field, deref
		// or index target stores the builder somewhere unrecorded.
		if l := consumer.Fields["left"]; consumer.Fields["right"] == child && l != nil && l.Kind == "identifier" {
			return "assign"
		}
	case "return_expression":
		return "return"
	case "arguments":
		return "argument"
	case "block":
		return "tail"
	case "closure_expression":
		if consumer.Fields["body"] == child {
			return "tail"
		}
	}
	return "other"
}

// chain descends from the outermost method call to the root receiver.
// Parentheses are transparent; `?`, `.await` and a root that is neither a call
// nor an identifier make the chain incomplete.
func (c *tauriCapture) chain(n *parse.Node) facts.TauriChain {
	var methods []facts.TauriMethod
	incomplete := false
	for n != nil {
		if wrapper(n.Kind) {
			incomplete = incomplete || n.Kind != "parenthesized_expression"
			n = unwrap(n)
			continue
		}
		field := methodCall(n)
		if field == nil {
			break
		}
		m := facts.TauriMethod{Name: facts.Text(field.Fields["field"]), Span: facts.Span(n)}
		if f := field.Fields["field"]; f != nil {
			m.Span = c.span(f.Start, n.End)
		}
		if arg := soleArgument(n); arg != nil {
			if index, ok := c.handlerOf[arg]; ok {
				m.Handler = &index
			}
		}
		m.Uncaptured = m.Handler == nil && m.Name == "invoke_handler"
		methods = append(methods, m)
		n = field.Fields["value"]
	}
	for i, j := 0, len(methods)-1; i < j; i, j = i+1, j-1 {
		methods[i], methods[j] = methods[j], methods[i]
	}
	root := c.root(n)
	return facts.TauriChain{Root: root, Methods: methods, Incomplete: incomplete || root.Kind == "other"}
}

func (c *tauriCapture) root(n *parse.Node) facts.TauriRoot {
	if n == nil {
		return facts.TauriRoot{Kind: "other", Syntax: "none"}
	}
	switch n.Kind {
	case "identifier":
		return facts.TauriRoot{Kind: "identifier", Name: n.Text, Offset: n.Start}
	case "call_expression":
		callee := n.Fields["function"]
		span := facts.Span(n)
		r := facts.TauriRoot{Kind: "call", Callee: rustPath(callee), Generic: hasGeneric(callee), Span: &span, Offset: n.Start}
		if args := n.Fields["arguments"]; args != nil {
			for _, a := range args.Children {
				if a.Kind == "line_comment" || a.Kind == "block_comment" {
					continue
				}
				if value, ok := stringValue(a); ok {
					r.Argument = &value
				}
				break
			}
		}
		return r
	}
	return facts.TauriRoot{Kind: "other", Syntax: n.Kind, Offset: n.Start}
}

func hasGeneric(n *parse.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == "type_arguments" {
		return true
	}
	for _, child := range n.Children {
		if hasGeneric(child) {
			return true
		}
	}
	return false
}

// flows records the bindings reachable from each function's handler roots,
// and the flow names that another binder may shadow at a root site.
func (c *tauriCapture) flows() {
	for _, fn := range c.functions {
		if !c.owners[fn] {
			continue
		}
		calls := c.calls[fn]
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].top.Start < calls[j].top.Start })
		var all []facts.TauriBinding
		body := fn.Fields["body"]
		c.bindings(body, nil, body, false, &all)
		names := map[string]bool{}
		var sites []facts.TauriRoot
		for _, call := range calls {
			sites = append(sites, call.call.Chain.Root)
			names[call.call.Chain.Root.Name] = true
		}
		kept := map[int]bool{}
		for changed := true; changed; {
			changed = false
			for i, b := range all {
				if kept[i] || !names[b.Name] {
					continue
				}
				kept[i] = true
				changed = true
				for _, alt := range b.Alternatives {
					if alt.Root.Kind == "identifier" {
						names[alt.Root.Name] = true
						sites = append(sites, alt.Root)
					}
				}
			}
		}
		flow := facts.TauriFlow{Function: c.identity(fn), Shadows: []string{}}
		for i, b := range all {
			if kept[i] {
				flow.Bindings = append(flow.Bindings, b)
			}
		}
		for _, call := range calls {
			flow.Calls = append(flow.Calls, call.call)
		}
		if len(flow.Bindings) == 0 {
			continue
		}
		var binders []binder
		collectBinders(fn, fn, nil, nil, &binders)
		shadowed := map[string]bool{}
		for _, site := range sites {
			for _, b := range binders {
				if b.name == site.Name && b.start <= site.Offset && site.Offset < b.end {
					shadowed[b.name] = true
				}
			}
		}
		// A macro invocation may expand to a rebinding of any identifier in
		// its tokens, so such a flow name is unproven anywhere in the body.
		inMacros := map[string]bool{}
		macroIdents(body, fn, inMacros)
		for name := range names {
			if inMacros[name] {
				shadowed[name] = true
			}
		}
		for name := range shadowed {
			flow.Shadows = append(flow.Shadows, name)
		}
		sort.Strings(flow.Shadows)
		c.macros(body, fn, &flow.Macros)
		sort.SliceStable(flow.Bindings, func(i, j int) bool { return flow.Bindings[i].Offset < flow.Bindings[j].Offset })
		c.out.Flows = append(c.out.Flows, flow)
	}
}

// macros records every macro invocation and macro_rules! definition under n
// in source order, skipping nested function items (flows of their own) and
// descending into closures. Invocations written inside a token tree
// (`info!("{}", env!("X"))`) are found by their `path ! (..)` token shape and
// take the outer invocation's scope.
func (c *tauriCapture) macros(n, fn *parse.Node, out *[]facts.TauriMacro) {
	if n == nil || n.Kind == "function_item" && n != fn {
		return
	}
	switch n.Kind {
	case "macro_invocation":
		span := facts.Span(n)
		scope := c.scope(span)
		*out = append(*out, facts.TauriMacro{Path: compact(facts.Text(n.Fields["macro"])), Scope: scope, Span: span, Kind: "invocation"})
		for _, child := range n.Children {
			if child.Kind == "token_tree" {
				c.nestedMacros(child, scope, out)
			}
		}
		return
	case "macro_definition":
		*out = append(*out, facts.TauriMacro{Path: facts.Text(n.Fields["name"]), Scope: c.scope(facts.Span(n)), Span: facts.Span(n), Kind: "definition"})
		return
	}
	for _, child := range n.Children {
		c.macros(child, fn, out)
	}
}

// nestedMacros scans one token tree, and the trees nested in it, for
// `path ! group` invocations.
func (c *tauriCapture) nestedMacros(tree *parse.Node, scope int, out *[]facts.TauriMacro) {
	tokens := c.tokens(tree)
	for i := 0; i < len(tokens); {
		if segments, rooted, n := pathTokens(tokens[i:]); n > 0 {
			if i+n+1 < len(tokens) && isPunct(tokens[i+n], "!") && tokens[i+n+1].kind == "group" {
				group := tokens[i+n+1]
				*out = append(*out, facts.TauriMacro{Path: joinPath(segments, rooted), Scope: scope, Span: c.span(tokens[i].start, group.end), Kind: "invocation"})
			}
			i += n
			continue
		}
		if tokens[i].kind == "group" {
			c.nestedMacros(tokens[i].node, scope, out)
		}
		i++
	}
}

// macroIdents collects the identifiers in the token trees of every macro
// invocation under n, skipping nested function items.
func macroIdents(n, fn *parse.Node, out map[string]bool) {
	if n == nil || n.Kind == "function_item" && n != fn {
		return
	}
	if n.Kind == "macro_invocation" {
		for _, child := range n.Children {
			if child.Kind == "token_tree" {
				treeIdents(child, out)
			}
		}
		return
	}
	for _, child := range n.Children {
		macroIdents(child, fn, out)
	}
}

func treeIdents(n *parse.Node, out map[string]bool) {
	if n.Kind == "identifier" {
		out[n.Text] = true
	}
	for _, child := range n.Children {
		treeIdents(child, out)
	}
}

// binder is a name bound by a form that Flows do not record, with the byte
// range in which it may be visible. Ranges are deliberately wide (the whole
// if, match arm, closure, loop, block or function), so a consumer may
// over-report shadowing but never miss it.
type binder struct {
	name       string
	start, end uint32
}

// collectBinders walks a function body, skipping nested function items (flows
// of their own) and macro bodies (opaque, see macroIdents). condition is the
// nearest if or while expression or match arm, the scope of an if let, while
// let or match guard let pattern; block is the nearest block, the scope of a
// destructuring let.
func collectBinders(n, fn, condition, block *parse.Node, out *[]binder) {
	if n == nil || n.Kind == "macro_invocation" || n.Kind == "function_item" && n != fn {
		return
	}
	add := func(pattern, scope *parse.Node) {
		var names []string
		patternNames(pattern, &names)
		for _, name := range names {
			*out = append(*out, binder{name: name, start: scope.Start, end: scope.End})
		}
	}
	switch n.Kind {
	case "function_item":
		add(n.Fields["parameters"], n)
	case "closure_expression":
		add(n.Fields["parameters"], n)
	case "if_expression", "while_expression":
		condition = n
	case "block":
		block = n
	case "let_condition":
		scope := condition
		if scope == nil {
			scope = n
		}
		add(n.Fields["pattern"], scope)
	case "match_arm":
		add(n.Fields["pattern"], n)
		condition = n
	case "for_expression":
		add(n.Fields["pattern"], n)
	case "let_declaration":
		if p := n.Fields["pattern"]; p != nil && p.Kind != "identifier" && block != nil {
			add(p, block)
		}
	}
	for _, child := range n.Children {
		collectBinders(child, fn, condition, block, out)
	}
}

// patternNames lists the identifiers a pattern (or parameter list) may bind.
// It over-includes (constants and unit variants look like bindings) and skips
// types, struct and variant paths, and match guards.
func patternNames(n *parse.Node, out *[]string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case "identifier", "shorthand_field_identifier":
		*out = append(*out, n.Text)
		return
	case "parameter":
		patternNames(n.Fields["pattern"], out)
		return
	case "scoped_identifier", "self_parameter", "line_comment", "block_comment":
		return
	}
	for _, child := range n.Children {
		if child == n.Fields["type"] || n.Kind == "match_pattern" && child == n.Fields["condition"] {
			continue
		}
		patternNames(child, out)
	}
}

// bindings collects lets and assignments of plain identifiers, excluding
// nested function items, which are flows of their own. gated is set inside a
// statement that carries a cfg; body is the function body, whose direct
// statements are the only unconditional ones.
func (c *tauriCapture) bindings(n, block, body *parse.Node, gated bool, out *[]facts.TauriBinding) {
	if n == nil || n.Kind == "function_item" || n.Kind == "macro_invocation" {
		return
	}
	if n.Kind == "block" {
		block = n
	}
	cfg := false
	for _, child := range n.Children {
		if child.Kind == "attribute_item" {
			cfg = cfg || c.cfgGated(child)
			continue
		}
		if child.Kind == "line_comment" || child.Kind == "block_comment" {
			continue
		}
		statement := child
		if child.Kind == "expression_statement" && len(child.Children) > 0 {
			statement = child.Children[0]
		}
		var name string
		var value *parse.Node
		kind := ""
		switch statement.Kind {
		case "let_declaration":
			if p := statement.Fields["pattern"]; p != nil && p.Kind == "identifier" {
				kind, name, value = "let", p.Text, statement.Fields["value"]
			}
		case "assignment_expression":
			if l := statement.Fields["left"]; l != nil && l.Kind == "identifier" {
				kind, name, value = "assign", l.Text, statement.Fields["right"]
			}
		}
		if kind != "" && block != nil {
			b := facts.TauriBinding{Name: name, Kind: kind, Block: facts.Span(block), Offset: statement.Start, End: statement.End, Cfg: gated || cfg, Conditional: n != body}
			b.Value, b.Alternatives, b.Complete = c.value(value)
			*out = append(*out, b)
		}
		// statement is the expression inside a statement wrapper, so the
		// recursion starts there and never records the same binding twice.
		c.bindings(statement, block, body, gated || cfg, out)
		cfg = false
	}
}

// value classifies a binding's value and lists its alternatives.
func (c *tauriCapture) value(n *parse.Node) (string, []facts.TauriChain, bool) {
	if n == nil {
		return "none", nil, false
	}
	switch n.Kind {
	case "if_expression", "match_expression", "block":
		alts, complete := c.tails(n)
		return map[string]string{"if_expression": "if", "match_expression": "match", "block": "block"}[n.Kind], alts, complete
	case "loop_expression", "while_expression", "for_expression":
		return "unsupported", nil, false
	}
	return "plain", []facts.TauriChain{c.chain(n)}, true
}

// tails returns the value chain of every branch, flattening nested branches.
func (c *tauriCapture) tails(n *parse.Node) ([]facts.TauriChain, bool) {
	if n == nil {
		return nil, false
	}
	switch n.Kind {
	case "block":
		var last *parse.Node
		for _, child := range n.Children {
			if child.Kind != "line_comment" && child.Kind != "block_comment" {
				last = child
			}
		}
		if last == nil || last.Kind == "attribute_item" || last.Kind == "let_declaration" || strings.HasSuffix(last.Kind, "_item") || last.Kind == "empty_statement" {
			return nil, false
		}
		if last.Kind == "expression_statement" {
			for _, t := range last.Tokens {
				if t == ";" {
					return nil, false
				}
			}
			if len(last.Children) == 0 {
				return nil, false
			}
			last = last.Children[0]
		}
		return c.tails(last)
	case "if_expression":
		alts, complete := c.tails(n.Fields["consequence"])
		otherwise := n.Fields["alternative"]
		if otherwise == nil || len(otherwise.Children) == 0 {
			return alts, false
		}
		more, ok := c.tails(otherwise.Children[len(otherwise.Children)-1])
		return append(alts, more...), complete && ok
	case "match_expression":
		var alts []facts.TauriChain
		complete := true
		body := n.Fields["body"]
		if body == nil {
			return nil, false
		}
		for _, arm := range body.Children {
			if arm.Kind != "match_arm" {
				continue
			}
			more, ok := c.tails(arm.Fields["value"])
			alts = append(alts, more...)
			complete = complete && ok
		}
		return alts, complete
	case "loop_expression", "while_expression", "for_expression":
		return nil, false
	}
	return []facts.TauriChain{c.chain(n)}, true
}

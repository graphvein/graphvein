package dart

import (
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// typeKinds maps the type declaration nodes to their node kind and row.
var typeKinds = map[string][2]string{
	"class_definition":           {"class", "DA-T1"},
	"mixin_declaration":          {"mixin", "DA-T2"},
	"extension_declaration":      {"extension", "DA-T3"},
	"extension_type_declaration": {"extension_type", "DA-T4"},
	"enum_declaration":           {"enum", "DA-T5"},
}

// bodyKinds are the type bodies whose members DA-F2 to DA-F5 and DA-C1 to
// DA-C3 read.
var bodyKinds = map[string]bool{"class_body": true, "extension_body": true, "enum_body": true}

// memberSignatures are the signatures a `method_signature` or `declaration`
// in a type body wraps.
var memberSignatures = map[string]bool{
	"function_signature": true, "getter_signature": true, "setter_signature": true, "operator_signature": true,
	"constructor_signature": true, "constant_constructor_signature": true, "factory_constructor_signature": true,
	"redirecting_factory_constructor_signature": true, "primary_constructor_body_signature": true,
}

// level is where a node sits: directly in the program, directly in a type
// body, or anywhere else (callable bodies, initializers, expressions).
type level int

const (
	atProgram level = iota
	inTypeBody
	inCode
)

// scope is where a declaration found during the walk belongs.
type scope struct {
	// list receives declarations found here: the owner's Children, or the
	// file's roots.
	list  *[]*besteffort.Decl
	level level
	// typeName is the enclosing type's declared name, "" outside a type or
	// in an anonymous extension (DA-C1 to DA-C3 compare with it).
	typeName string
}

// code is the scope below s outside type bodies: same owner, same type.
func (s scope) code() scope {
	s.level = inCode
	return s
}

type extractor struct {
	source  []byte
	t       tracer
	payload *facts.BestEffortFacts
}

// Extract returns the file's Dart facts: one node per claimed declaration
// with its contains edge, and the best-effort payload (directives as
// written) that Link resolves. A tree with an ERROR or named MISSING node
// claims nothing (row BE-2); the indexer applies the same refusal through
// besteffort.Refuse when only the parser saw a missing token.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	return extract(file, source, root, nil)
}

func extract(file string, source []byte, root *parse.Node, t tracer) facts.Facts {
	out := facts.Facts{Exports: map[string]string{}}
	if besteffort.Recovered(root) {
		t.trace("BE-2")
		// Row BE-8 does not apply to Dart: no Dart file declares a name
		// that an import names (package names come from pubspec.yaml), so
		// a refused file needs no scope.
		return besteffort.Refuse(out, besteffort.Scope{})
	}
	x := &extractor{source: source, t: t, payload: &facts.BestEffortFacts{Version: facts.BestEffortVersion}}
	var roots []*besteffort.Decl
	if root != nil {
		x.sequence(root, scope{list: &roots, level: atProgram})
	}
	besteffort.Name(roots)
	besteffort.Emit(&out, file, source, roots)
	out.BestEffort = x.payload
	return out
}

// sequence walks the children of n. Dart puts a signature and its
// function_body side by side, so declarations are read from
// the child sequence rather than from single nodes.
func (x *extractor) sequence(n *parse.Node, s scope) {
	kids := n.Children
	for i := 0; i < len(kids); i++ {
		c := kids[i]
		if tk, ok := typeKinds[c.Kind]; ok {
			x.typeDecl(c, tk[0], tk[1], s)
			continue
		}
		switch {
		case s.level == atProgram && (c.Kind == "function_signature" || c.Kind == "getter_signature" || c.Kind == "setter_signature"):
			index := i
			var body *parse.Node
			if i+1 < len(kids) && kids[i+1].Kind == "function_body" {
				body = kids[i+1]
				i++
			}
			x.topLevel(kids, index, c, body, s)
		case s.level == inTypeBody && (c.Kind == "method_signature" || c.Kind == "declaration"):
			index := i
			var body *parse.Node
			if c.Kind == "method_signature" && i+1 < len(kids) && kids[i+1].Kind == "function_body" {
				body = kids[i+1]
				i++
			}
			x.member(kids, index, c, body, s)
		case c.Kind == "local_function_declaration":
			x.local(c, s)
		case c.Kind == "import_or_export" || c.Kind == "part_directive":
			x.directive(c)
		case c.Kind == "part_of_directive" || c.Kind == "library_name":
			x.t.trace("DA-I7")
		case c.Kind == "type_alias" || c.Kind == "enum_constant" || c.Kind == "function_expression" || c.Kind == "initialized_identifier_list" || c.Kind == "static_final_declaration_list":
			// Type aliases, enum values, variables and lambdas declare
			// nothing; a lambda adds no owner, so its local functions
			// belong to the nearest emitted owner.
			x.t.trace("DA-R1")
			x.sequence(c, s.code())
		default:
			x.t.trace("D")
			x.sequence(c, s.code())
		}
	}
}

// typeDecl claims a type (rows DA-T1 to DA-T5) and walks its body.
func (x *extractor) typeDecl(n *parse.Node, kind, row string, s scope) {
	// declared is the name constructors are compared with; an anonymous
	// extension has none.
	declared := typeName(n)
	name := declared
	if name == "" && n.Kind == "extension_declaration" {
		if on := x.onType(n); on != "" {
			name = "extension on " + on
		}
	}
	if name == "" {
		x.t.trace("D")
		x.sequence(n, s.code())
		return
	}
	x.t.trace(row)
	d := &besteffort.Decl{Row: row, Kind: kind, Name: name, Node: n}
	if n.Kind == "class_definition" {
		for _, c := range n.Children {
			if c.Kind == "mixin" {
				d.Attributes = map[string]any{"mixin": true}
			}
		}
	}
	*s.list = append(*s.list, d)
	for _, c := range n.Children {
		switch {
		case bodyKinds[c.Kind]:
			x.sequence(c, scope{list: &d.Children, level: inTypeBody, typeName: declared})
		case c.Kind == "primary_constructor":
			// A Dart 3.12/3.13 primary constructor is not emitted.
			x.t.trace("DA-R1")
		}
	}
}

// typeName is a type declaration's declared name: the `name` field, the
// identifier of a mixin application `class C = B with M;`, or a mixin's
// first identifier child.
func typeName(n *parse.Node) string {
	if name := facts.Text(n.Fields["name"]); name != "" {
		return name
	}
	for _, c := range n.Children {
		switch {
		case n.Kind == "class_definition" && c.Kind == "mixin_application_class":
			for _, id := range c.Children {
				if id.Kind == "identifier" {
					return id.Text
				}
			}
		case n.Kind == "mixin_declaration" && c.Kind == "identifier":
			return c.Text
		}
	}
	return ""
}

// onType is an anonymous extension's on-type as written: the text between
// the `on` keyword and the body, comments and annotations removed, and
// whitespace removed except one space between two word characters
// (`List<int>`, `Map<String,int>?`, `void Function()`).
func (x *extractor) onType(n *parse.Node) string {
	var body *parse.Node
	for _, c := range n.Children {
		if bodyKinds[c.Kind] {
			body = c
		}
	}
	if body == nil {
		return ""
	}
	start := n.Start
	for _, c := range n.Children {
		if c.Start >= body.Start {
			break
		}
		if c.Kind == "annotation" || c.Kind == "comment" || c.Kind == "documentation_comment" || c.Kind == "type_parameters" {
			start = c.End
		}
	}
	text := strings.Join(strings.Fields(stripComments(x.source, n, start, body.Start)), " ")
	text = strings.TrimPrefix(text, "extension ")
	if !strings.HasPrefix(text, "on ") {
		return ""
	}
	return squeeze(strings.TrimPrefix(text, "on "))
}

// stripComments returns the source from start to end without the comments
// and annotations below n.
func stripComments(source []byte, n *parse.Node, start, end uint32) string {
	type cut struct{ start, end uint32 }
	var cuts []cut
	var collect func(*parse.Node)
	collect = func(c *parse.Node) {
		if c.Kind == "comment" || c.Kind == "documentation_comment" || c.Kind == "annotation" {
			cuts = append(cuts, cut{c.Start, c.End})
			return
		}
		for _, child := range c.Children {
			collect(child)
		}
	}
	collect(n)
	var b strings.Builder
	for i := start; i < end && int(i) < len(source); i++ {
		skipped := false
		for _, c := range cuts {
			if i >= c.start && i < c.end {
				i = c.end - 1
				skipped = true
				break
			}
		}
		if skipped {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(source[i])
	}
	return b.String()
}

// squeeze removes the spaces of a whitespace-normalized text except those
// between two word characters.
func squeeze(text string) string {
	word := func(c byte) bool {
		return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
	}
	var b strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] == ' ' && !(i > 0 && i+1 < len(text) && word(text[i-1]) && word(text[i+1])) {
			continue
		}
		b.WriteByte(text[i])
	}
	return b.String()
}

// topLevel claims a function, getter or setter directly under the program
// (rows DA-F1, DA-F3, DA-F4).
func (x *extractor) topLevel(kids []*parse.Node, index int, sig, body *parse.Node, s scope) {
	name := facts.Text(sig.Fields["name"])
	if name == "" {
		x.t.trace("D")
		x.sequence(sig, s.code())
		if body != nil {
			x.sequence(body, s.code())
		}
		return
	}
	d := &besteffort.Decl{Kind: "function", Name: name}
	switch sig.Kind {
	case "function_signature":
		d.Row = "DA-F1"
	case "getter_signature":
		d.Row = "DA-F3"
		d.Attributes = map[string]any{"accessor": "get"}
	case "setter_signature":
		d.Row = "DA-F4"
		d.Name = name + "="
		d.Attributes = map[string]any{"accessor": "set"}
	}
	x.t.trace(d.Row)
	d.Node = x.span(kids, index, sig, body, true)
	*s.list = append(*s.list, d)
	if body != nil {
		x.sequence(body, scope{list: &d.Children, level: inCode, typeName: s.typeName})
	}
}

// member claims a member of a type body: the signature a method_signature or
// declaration wraps (rows DA-F2 to DA-F5, DA-C1 to DA-C3), or nothing for
// fields (DA-R1) and the guarded constructor shapes (DA-G1, DA-G2).
func (x *extractor) member(kids []*parse.Node, index int, wrapper, body *parse.Node, s scope) {
	var sig *parse.Node
	for _, c := range wrapper.Children {
		if memberSignatures[c.Kind] {
			sig = c
			break
		}
	}
	walkRest := func(owner scope) {
		x.sequence(wrapper, owner)
		if body != nil {
			x.sequence(body, owner)
		}
	}
	if sig == nil || sig.Kind == "primary_constructor_body_signature" {
		// Fields and a primary constructor's body declare nothing; a lambda
		// in an initializer belongs to the type.
		x.t.trace("DA-R1")
		walkRest(s.code())
		return
	}
	d := &besteffort.Decl{Kind: "method", Name: facts.Text(sig.Fields["name"])}
	switch sig.Kind {
	case "function_signature":
		d.Row = "DA-F2"
	case "getter_signature":
		d.Row = "DA-F3"
		d.Attributes = map[string]any{"accessor": "get"}
	case "setter_signature":
		d.Row = "DA-F4"
		d.Attributes = map[string]any{"accessor": "set"}
		if d.Name != "" {
			d.Name += "="
		}
	case "operator_signature":
		d.Row = "DA-F5"
		d.Name = x.operator(sig)
	default:
		d.Kind = "constructor"
		d.Row, d.Name = constructor(sig, s.typeName)
		if d.Row == "DA-G1" || d.Row == "DA-G2" {
			// The subtree is skipped: what looks like a constructor here
			// is not one this row can name (DA-G1: an untyped method).
			x.t.trace(d.Row)
			return
		}
	}
	if d.Name == "" {
		x.t.trace("D")
		walkRest(s.code())
		return
	}
	x.t.trace(d.Row)
	d.Node = x.span(kids, index, wrapper, body, false)
	*s.list = append(*s.list, d)
	walkRest(scope{list: &d.Children, level: inCode, typeName: s.typeName})
}

// operator names an operator_signature `operator <op>`: the text between
// the `operator` keyword and the parameter list, without whitespace and
// comments (`operator ==`, `operator []=`, `operator -`).
func (x *extractor) operator(sig *parse.Node) string {
	var params *parse.Node
	for _, c := range sig.Children {
		if c.Kind == "formal_parameter_list" {
			params = c
		}
	}
	if params == nil {
		return ""
	}
	head := string(x.source[sig.Start:params.Start])
	i := strings.LastIndex(head, "operator")
	if i < 0 {
		return ""
	}
	op := besteffort.TypeText(x.source, sig, sig.Start+uint32(i+len("operator")), params.Start)
	if op == "" {
		return ""
	}
	return "operator " + op
}

// constructor reads a constructor signature (rows DA-C1 to DA-C3) against
// the enclosing type's name and returns its row and declared name, or a
// guard row (DA-G1, DA-G2) with no name. Only identifiers before the
// parameter list count: a redirecting factory's target follows it.
func constructor(sig *parse.Node, typeName string) (row, name string) {
	var ids []string
	for _, c := range sig.Children {
		if c.Kind == "formal_parameter_list" {
			break
		}
		if c.Kind == "identifier" {
			ids = append(ids, c.Text)
		}
	}
	// A `new` token is the concise form (`new named()`) unless a `.`
	// precedes it: `C.new()` is the classic spelling of the unnamed
	// constructor (Dart 2.15 constructor tear-offs).
	concise, dotNew := false, false
	for i, token := range sig.Tokens {
		if token != "new" {
			continue
		}
		if i > 0 && sig.Tokens[i-1] == "." {
			dotNew = true
		} else {
			concise = true
		}
	}
	switch sig.Kind {
	case "constructor_signature", "constant_constructor_signature":
		switch {
		case dotNew && typeName != "" && len(ids) == 1 && ids[0] == typeName:
			return "DA-C1", "new"
		case dotNew:
			return "DA-G1", ""
		case concise && len(ids) == 0:
			return "DA-C2", "new"
		case concise && len(ids) == 1 && facts.Text(sig.Fields["name"]) == ids[0]:
			return "DA-C2", ids[0]
		case concise || typeName == "" || len(ids) == 0 || len(ids) > 2 || ids[0] != typeName:
			// 0038280 also reads an untyped method (`foo();`,
			// `foo() {}`) as a constructor signature.
			return "DA-G1", ""
		case len(ids) == 1:
			return "DA-C1", "new"
		default:
			return "DA-C1", ids[1]
		}
	default:
		switch {
		case concise || dotNew || typeName == "" || len(ids) > 2:
			return "DA-G2", ""
		case len(ids) == 0:
			return "DA-C3", "new"
		case len(ids) == 1 && ids[0] == typeName:
			return "DA-C1", "new"
		case len(ids) == 1:
			return "DA-C3", ids[0]
		case ids[0] == typeName:
			return "DA-C1", ids[1]
		default:
			return "DA-G2", ""
		}
	}
}

// local claims a local function (row DA-F6) owned by the nearest emitted
// owner and walks its body.
func (x *extractor) local(n *parse.Node, s scope) {
	var lambda, sig *parse.Node
	for _, c := range n.Children {
		if c.Kind == "lambda_expression" {
			lambda = c
			sig = c.Fields["parameters"]
		}
	}
	name := ""
	if sig != nil && sig.Kind == "function_signature" {
		name = facts.Text(sig.Fields["name"])
	}
	if name == "" {
		x.t.trace("D")
		x.sequence(n, s.code())
		return
	}
	x.t.trace("DA-F6")
	d := &besteffort.Decl{Row: "DA-F6", Kind: "function", Name: name, Node: n}
	*s.list = append(*s.list, d)
	if body := lambda.Fields["body"]; body != nil {
		x.sequence(body, scope{list: &d.Children, level: inCode, typeName: s.typeName})
	}
}

// span applies row DA-S to the declaration whose signature or wrapper is
// kids[index], with its function_body sibling if any.
func (x *extractor) span(kids []*parse.Node, index int, decl, body *parse.Node, top bool) *parse.Node {
	x.t.trace("DA-S")
	n := &parse.Node{Kind: decl.Kind, Start: decl.Start, Row: decl.Row, Column: decl.Column, Fields: map[string]*parse.Node{}}
	if top {
		// A top-level `external` modifier is a token of the program,
		// outside the signature node.
		p := int(decl.Start)
		for p > 0 && (x.source[p-1] == ' ' || x.source[p-1] == '\t' || x.source[p-1] == '\n' || x.source[p-1] == '\r') {
			p--
		}
		const keyword = "external"
		if p >= len(keyword) && string(x.source[p-len(keyword):p]) == keyword && (p == len(keyword) || !wordByte(x.source[p-len(keyword)-1])) {
			x.setStart(n, uint32(p-len(keyword)), decl)
		}
	}
	for k := index - 1; k >= 0; k-- {
		c := kids[k]
		if c.Kind == "annotation" {
			n.Start, n.Row, n.Column = c.Start, c.Row, c.Column
			continue
		}
		if c.Kind == "comment" || c.Kind == "documentation_comment" {
			continue
		}
		break
	}
	last := decl
	if body != nil {
		last = body
	}
	n.End, n.EndRow, n.EndColumn = last.End, last.EndRow, last.EndColumn
	if body == nil {
		p := int(last.End)
		for p < len(x.source) && (x.source[p] == ' ' || x.source[p] == '\t') {
			p++
		}
		if p < len(x.source) && x.source[p] == ';' {
			n.End = uint32(p + 1)
			n.EndColumn = last.EndColumn + (n.End - last.End)
		}
	}
	n.Text = string(x.source[n.Start:n.End])
	return n
}

// setStart moves n's start to offset, before ref's start, computing the row
// and byte column from the source.
func (x *extractor) setStart(n *parse.Node, offset uint32, ref *parse.Node) {
	n.Start = offset
	n.Row = ref.Row - uint32(strings.Count(string(x.source[offset:ref.Start]), "\n"))
	lineStart := strings.LastIndexByte(string(x.source[:offset]), '\n') + 1
	n.Column = offset - uint32(lineStart)
}

func wordByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// literalURI matches a single-line string literal without interpolation or
// escapes, raw or not; anything else is not a literal URI (row DA-I8).
var literalURI = regexp.MustCompile(`^(?:'([^'\\$\n]*)'|"([^"\\$\n]*)"|r'([^'\n]*)'|r"([^"\n]*)")$`)

// directive records the URIs of an import, export or part directive as
// written (rows DA-I1 to DA-I8 classify them in Link).
func (x *extractor) directive(n *parse.Node) {
	word := "part"
	var uris []*parse.Node
	conditional := false
	if n.Kind == "part_directive" {
		for _, c := range n.Children {
			if c.Kind == "uri" {
				uris = append(uris, c)
			}
		}
	} else {
		var walk func(*parse.Node)
		walk = func(c *parse.Node) {
			switch c.Kind {
			case "library_import":
				word = "import"
			case "library_export":
				word = "export"
			case "uri":
				uris = append(uris, c)
				return
			case "configuration_uri":
				conditional = true
			}
			for _, child := range c.Children {
				walk(child)
			}
		}
		walk(n)
	}
	if conditional {
		x.t.trace("DA-I6")
	}
	if len(uris) == 0 {
		x.t.trace("D")
		return
	}
	for _, u := range uris {
		text := ""
		if len(u.Children) == 1 && u.Children[0].Kind == "string_literal" {
			if m := literalURI.FindStringSubmatch(u.Children[0].Text); m != nil {
				text = m[1] + m[2] + m[3] + m[4]
			}
		}
		if text == "" {
			x.t.trace("DA-I8")
			continue
		}
		x.payload.Imports = append(x.payload.Imports, facts.BestEffortImport{Name: text, Directive: word, Span: facts.Span(n)})
	}
}

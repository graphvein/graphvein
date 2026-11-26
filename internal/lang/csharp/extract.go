package csharp

import (
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// typeKinds maps the type declaration nodes to their node kind and row.
var typeKinds = map[string][2]string{
	"class_declaration":     {"class", "CS-T1"},
	"struct_declaration":    {"struct", "CS-T2"},
	"interface_declaration": {"interface", "CS-T3"},
	"record_declaration":    {"record", "CS-T4"},
	"enum_declaration":      {"enum", "CS-T5"},
}

// nameKinds are the using targets that are names (CS-I1, CS-I2). Any other
// alias target (a tuple, predefined, array or pointer type; C# 12) is D.
var nameKinds = map[string]bool{"identifier": true, "qualified_name": true, "generic_name": true, "alias_qualified_name": true}

// Extract returns the file's C# facts: one node per claimed declaration with
// its contains edge, and the best-effort payload (declared namespaces,
// global-namespace type names, using directives) that Link resolves. A tree
// with an ERROR or named MISSING node claims nothing (row BE-2); the indexer
// applies the same refusal through besteffort.Refuse when only the parser
// saw a missing token.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	return extract(file, source, root, nil)
}

// scope is where a declaration found during the walk belongs.
type scope struct {
	// list receives declarations found here: the owner's Children, or the
	// file's roots.
	list *[]*besteffort.Decl
	// typeName is the nearest enclosing type's name, the constructor and
	// destructor name (CS-F2, CS-F3).
	typeName string
	// namespace is the enclosing namespace, nested names joined with `.`.
	namespace string
	// inType is set below a type declaration.
	inType bool
}

// extractor holds one file's walk state.
type extractor struct {
	source  []byte
	t       tracer
	payload *facts.BestEffortFacts
	// arity is each type declaration's generic arity, for the C# generic
	// arity naming rule.
	arity map[*besteffort.Decl]int
}

func extract(file string, source []byte, root *parse.Node, t tracer) facts.Facts {
	out := facts.Facts{Exports: map[string]string{}}
	if besteffort.Recovered(root) {
		t.trace("BE-2")
		return besteffort.Refuse(out, RefusedScope(source))
	}
	x := &extractor{source: source, t: t, payload: &facts.BestEffortFacts{Version: facts.BestEffortVersion}, arity: map[*besteffort.Decl]int{}}
	var roots []*besteffort.Decl
	if root != nil {
		x.walkList(root.Children, scope{list: &roots})
	}
	x.arityNames(roots)
	besteffort.Name(roots)
	besteffort.Emit(&out, file, source, roots)
	out.BestEffort = x.payload
	return out
}

// walkList walks sibling nodes in order. A file-scoped namespace declaration
// spans only its own line, so it sets the namespace of the siblings that
// follow it (row CS-T6).
func (x *extractor) walkList(children []*parse.Node, s scope) {
	for _, c := range children {
		if c.Kind == "file_scoped_namespace_declaration" {
			name := besteffort.TypeText(x.source, c.Fields["name"], startOf(c.Fields["name"]), endOf(c.Fields["name"]))
			if name == "" {
				x.t.trace("D")
				continue
			}
			x.t.trace("CS-T6")
			s.namespace = facts.Join(s.namespace, name)
			x.payload.Namespaces = append(x.payload.Namespaces, s.namespace)
			continue
		}
		x.walk(c, s)
	}
}

func (x *extractor) walk(n *parse.Node, s scope) {
	if n == nil {
		return
	}
	if tk, ok := typeKinds[n.Kind]; ok {
		x.typeDecl(n, s, tk[0], tk[1])
		return
	}
	switch n.Kind {
	case "namespace_declaration":
		name := besteffort.TypeText(x.source, n.Fields["name"], startOf(n.Fields["name"]), endOf(n.Fields["name"]))
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-T6")
		inner := s
		inner.namespace = facts.Join(s.namespace, name)
		x.payload.Namespaces = append(x.payload.Namespaces, inner.namespace)
		if body := n.Fields["body"]; body != nil {
			x.walkList(body.Children, inner)
		}
	case "preproc_if", "preproc_elif", "preproc_else":
		// Every branch is walked; conditions are not evaluated.
		x.t.trace("CS-T6")
		x.walkList(n.Children, s)
	case "using_directive":
		x.using(n, s)
	case "global_statement":
		// Top-level statements belong to the implicit Program class in the
		// global namespace; their local functions have no owner (CS-F4).
		x.global("Program")
		x.walkList(n.Children, s)
	case "method_declaration":
		name := facts.Text(n.Fields["name"])
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-F1")
		d := &besteffort.Decl{Row: "CS-F1", Kind: "method", Name: name, Suffix: x.explicit(n), Node: n, Overloadable: true, Overload: x.parameters(n.Fields["parameters"])}
		x.member(n, s, d)
	case "constructor_declaration":
		name := facts.Text(n.Fields["name"])
		if name != s.typeName {
			// The pinned grammar reads a C# 14 extension block
			// `extension(string s) { ... }` without an error as a
			// constructor named `extension` whose members are local
			// functions; nothing in it is claimed.
			x.t.trace("CS-G1")
			return
		}
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-F2")
		d := &besteffort.Decl{Row: "CS-F2", Kind: "constructor", Name: name, Node: n, Overloadable: true, Overload: x.parameters(n.Fields["parameters"])}
		if hasModifier(n, "static") {
			d.Suffix = "@static"
		}
		x.member(n, s, d)
	case "destructor_declaration":
		name := s.typeName
		if name == "" {
			name = facts.Text(n.Fields["name"])
		}
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-F3")
		x.member(n, s, &besteffort.Decl{Row: "CS-F3", Kind: "method", Name: "~" + name, Node: n})
	case "local_function_statement":
		name := facts.Text(n.Fields["name"])
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-F4")
		x.member(n, s, &besteffort.Decl{Row: "CS-F4", Kind: "function", Name: name, Node: n})
	case "operator_declaration", "conversion_operator_declaration":
		name := x.operatorName(n)
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-F5")
		x.member(n, s, &besteffort.Decl{Row: "CS-F5", Kind: "method", Name: name, Node: n, Overloadable: true, Overload: x.parameters(n.Fields["parameters"])})
	case "property_declaration":
		name := facts.Text(n.Fields["name"])
		if name == "" {
			x.t.trace("D")
			x.walkList(n.Children, s)
			return
		}
		x.t.trace("CS-P1")
		x.member(n, s, &besteffort.Decl{Row: "CS-P1", Kind: "property", Name: name, Suffix: x.explicit(n), Node: n})
	case "indexer_declaration":
		x.t.trace("CS-P2")
		x.member(n, s, &besteffort.Decl{Row: "CS-P2", Kind: "property", Name: "this", Suffix: x.explicit(n), Node: n, Overloadable: true, Overload: x.parameters(n.Fields["parameters"])})
	case "delegate_declaration":
		x.t.trace("CS-R1")
		if !s.inType && s.namespace == "" {
			x.global(facts.Text(n.Fields["name"]))
		}
	case "event_declaration", "event_field_declaration", "field_declaration", "enum_member_declaration", "accessor_declaration", "lambda_expression", "anonymous_method_expression":
		// Nothing is claimed and no owner is added; the walk descends, since
		// a lambda or an accessor may hold a local function (CS-F4).
		x.t.trace("CS-R1")
		x.walkList(n.Children, s)
	default:
		x.t.trace("D")
		x.walkList(n.Children, s)
	}
}

// typeDecl handles rows CS-T1 to CS-T5: the type with its partial (and
// record struct) attribute, then its body with the type as owner.
func (x *extractor) typeDecl(n *parse.Node, s scope, kind, row string) {
	name := facts.Text(n.Fields["name"])
	if name == "" {
		x.t.trace("D")
		x.walkList(n.Children, s)
		return
	}
	x.t.trace(row)
	var attrs map[string]any
	if kind != "enum" && hasModifier(n, "partial") {
		attrs = map[string]any{"partial": true}
	}
	if kind == "record" && hasToken(n, "struct") {
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs["struct"] = true
	}
	d := &besteffort.Decl{Row: row, Kind: kind, Name: name, Node: n, Attributes: attrs}
	*s.list = append(*s.list, d)
	x.arity[d] = 0
	for _, c := range n.Children {
		if c.Kind == "type_parameter_list" {
			for _, p := range c.Children {
				if p.Kind == "type_parameter" {
					x.arity[d]++
				}
			}
		}
	}
	if !s.inType && s.namespace == "" {
		x.global(name)
	}
	if body := n.Fields["body"]; body != nil {
		x.walkList(body.Children, scope{list: &d.Children, typeName: name, namespace: s.namespace, inType: true})
	}
}

// member appends a callable or property and walks its children with it as
// the owner (local functions, lambdas).
func (x *extractor) member(n *parse.Node, s scope, d *besteffort.Decl) {
	*s.list = append(*s.list, d)
	inner := s
	inner.list = &d.Children
	x.walkList(n.Children, inner)
}

// global records a type name declared in the global namespace.
func (x *extractor) global(name string) {
	if name == "" {
		return
	}
	for _, g := range x.payload.GlobalTypes {
		if g == name {
			return
		}
	}
	x.payload.GlobalTypes = append(x.payload.GlobalTypes, name)
}

// using records a using directive for Link (rows CS-I1, CS-I2). `extern
// alias` is a different node (D); a using whose target is not a name (an
// alias of a tuple, predefined, array or pointer type) is D.
func (x *extractor) using(n *parse.Node, s scope) {
	alias := n.Fields["name"]
	var target *parse.Node
	for _, c := range n.Children {
		if c != alias && c.Kind != "comment" {
			target = c
		}
	}
	if target == nil || !nameKinds[target.Kind] {
		x.t.trace("D")
		return
	}
	segments := x.segments(target)
	if len(segments) == 0 {
		x.t.trace("D")
		return
	}
	// A name written with `global::` is looked up from the global
	// namespace only, wherever the directive sits.
	scope := s.namespace
	if strings.HasPrefix(besteffort.TypeText(x.source, target, target.Start, target.End), "global::") {
		scope = ""
	}
	imp := facts.BestEffortImport{Name: strings.Join(segments, "."), Static: hasToken(n, "static"), Alias: alias != nil, Scope: scope, Span: facts.Span(n)}
	for i := 1; i < len(segments); i++ {
		imp.Prefixes = append(imp.Prefixes, strings.Join(segments[:i], "."))
	}
	x.payload.Imports = append(x.payload.Imports, imp)
}

// segments splits a name into its dotted segments from the syntax, each as
// written with whitespace and comments removed; `global::` is dropped and
// any other `alias::` stays on its segment.
func (x *extractor) segments(n *parse.Node) []string {
	switch n.Kind {
	case "qualified_name":
		q, name := n.Fields["qualifier"], n.Fields["name"]
		if q == nil || name == nil {
			return nil
		}
		head := x.segments(q)
		if head == nil {
			return nil
		}
		return append(head, besteffort.TypeText(x.source, name, name.Start, name.End))
	case "alias_qualified_name":
		alias, name := n.Fields["alias"], n.Fields["name"]
		if alias == nil || name == nil {
			return nil
		}
		text := besteffort.TypeText(x.source, name, name.Start, name.End)
		if alias.Text == "global" {
			return []string{text}
		}
		return []string{alias.Text + "::" + text}
	case "identifier", "generic_name":
		return []string{besteffort.TypeText(x.source, n, n.Start, n.End)}
	}
	return nil
}

// explicit is the explicit interface suffix `@IFoo` of a member (rows CS-F1,
// CS-P1, CS-P2): the interface's last name segment without generic
// arguments, or "".
func (x *extractor) explicit(n *parse.Node) string {
	for _, c := range n.Children {
		if c.Kind != "explicit_interface_specifier" {
			continue
		}
		for _, name := range c.Children {
			if last := lastName(name); last != "" {
				return "@" + last
			}
		}
	}
	return ""
}

// lastName is the last identifier of a type name, without generic
// arguments or an alias qualifier.
func lastName(n *parse.Node) string {
	switch n.Kind {
	case "identifier":
		return n.Text
	case "qualified_name", "alias_qualified_name":
		if name := n.Fields["name"]; name != nil {
			return lastName(name)
		}
	case "generic_name":
		for _, c := range n.Children {
			if c.Kind == "identifier" {
				return c.Text
			}
		}
	}
	return ""
}

// operatorName is the CS-F5 name: `operator <op>` for an operator (its
// tokens after `operator`, so `operator checked +` stays apart from
// `operator +`), `implicit operator <T>` / `explicit operator <T>` for a
// conversion, T as written without whitespace.
func (x *extractor) operatorName(n *parse.Node) string {
	var before, after []string
	seen := false
	for _, tok := range n.Tokens {
		switch {
		case tok == "operator":
			seen = true
		case tok == ";":
		case seen:
			after = append(after, tok)
		default:
			before = append(before, tok)
		}
	}
	if !seen {
		return ""
	}
	if n.Kind == "operator_declaration" {
		if len(after) == 0 {
			return ""
		}
		return "operator " + strings.Join(after, " ")
	}
	typ := n.Fields["type"]
	if typ == nil || len(before) != 1 || (before[0] != "implicit" && before[0] != "explicit") {
		return ""
	}
	parts := append([]string{before[0], "operator"}, after...)
	return strings.Join(append(parts, besteffort.TypeText(x.source, typ, typ.Start, typ.End)), " ")
}

// parameters is the BE-6 type list of a parameter_list or
// bracketed_parameter_list: each parameter's modifiers (`ref`, `out`, `in`,
// `params`, `this`, `scoped`) and type as written, the type with
// whitespace, comments and attributes removed, modifiers separated by a
// space, parameter names dropped.
func (x *extractor) parameters(list *parse.Node) string {
	var types, loose []string
	var looseNodes []*parse.Node
	if list != nil {
		for _, p := range list.Children {
			switch p.Kind {
			case "parameter":
				var parts []string
				for _, c := range p.Children {
					if c.Kind == "modifier" {
						parts = append(parts, strings.TrimSpace(c.Text))
					}
				}
				if typ := p.Fields["type"]; typ != nil {
					parts = append(parts, besteffort.TypeText(x.source, typ, typ.Start, typ.End))
				}
				types = append(types, strings.Join(parts, " "))
			case "comment", "attribute_list":
			default:
				// tree-sitter-c-sharp leaves a `params` parameter's type and
				// name directly in the list; it is always the last parameter.
				looseNodes = append(looseNodes, p)
			}
		}
	}
	if len(looseNodes) > 0 {
		typ := looseNodes[0]
		if len(looseNodes) > 1 {
			typ = looseNodes[len(looseNodes)-2]
		}
		loose = append(loose, "params "+besteffort.TypeText(x.source, typ, typ.Start, typ.End))
	}
	return "(" + strings.Join(append(types, loose...), ",") + ")"
}

// arityNames applies the C# generic arity rule: within one owner (or the
// file's roots), when type declarations of the same name differ in generic
// arity (`Result` and `Result<T>`), each generic one gets the CLR metadata
// suffix “ `N “. Same-arity repeats are left to BE-5.
func (x *extractor) arityNames(list []*besteffort.Decl) {
	arities := map[string]map[int]bool{}
	for _, d := range list {
		if a, ok := x.arity[d]; ok {
			if arities[d.Name] == nil {
				arities[d.Name] = map[int]bool{}
			}
			arities[d.Name][a] = true
		}
	}
	for _, d := range list {
		if a, ok := x.arity[d]; ok && a > 0 && len(arities[d.Name]) > 1 {
			d.Suffix = "`" + strconv.Itoa(a)
		}
		x.arityNames(d.Children)
	}
}

func hasModifier(n *parse.Node, modifier string) bool {
	for _, c := range n.Children {
		if c.Kind == "modifier" && strings.TrimSpace(c.Text) == modifier {
			return true
		}
	}
	return false
}

func hasToken(n *parse.Node, token string) bool {
	for _, tok := range n.Tokens {
		if tok == token {
			return true
		}
	}
	return false
}

func startOf(n *parse.Node) uint32 {
	if n == nil {
		return 0
	}
	return n.Start
}

func endOf(n *parse.Node) uint32 {
	if n == nil {
		return 0
	}
	return n.End
}

// RefusedScope reads, from the raw text of a refused (BE-2) file, the
// namespaces its declarations can belong to (row BE-8). The scan counts
// every whole-word `namespace`, in comments and strings too, so it may
// over-match but never misses a declaration. Three cases prove something:
//
//   - Every occurrence is followed by a qualified name and `;`: file-scoped
//     namespace declarations. C# allows at most one per file, before every
//     type declaration and never next to a block namespace (compiler errors
//     CS8954, CS8955, CS8956; Microsoft's C# reference, "namespace"
//     keyword), so every type of the file sits under one of the names.
//   - Any other occurrences (block namespaces, a mix, or the word in
//     prose): the file declares only namespaces named by an occurrence and
//     namespaces under them (a nested block `namespace A { namespace B {} }`
//     declares A.B, which is under A), but it may also declare types in the
//     global namespace (ECMA-334 §14.2: members outside every namespace
//     declaration belong to the global namespace). The scope is Global: its
//     names bound only namespaces.
//   - No occurrence: the file declares no namespace, only global-namespace
//     types (a namespace is declared only by a namespace declaration,
//     ECMA-334 §14.3); Global with no names.
//
// A plain using can name only a namespace (ECMA-334 §7.8.1: a namespace_name
// shall refer to a namespace, or a compile-time error occurs; compiler error
// CS0138), so a Global scope still bounds plain usings, while static and
// alias usings, which may name a global type, stay hidden. Nothing is proved
// when the text has a `\u` escape (allowed in names, which defeats a byte
// scan) or when `namespace` is followed by a verbatim identifier (`@name`),
// which the scan does not read. No compiler was run here.
func RefusedScope(source []byte) besteffort.Scope {
	text := string(source)
	if strings.Contains(text, "\\u") || strings.Contains(text, "\\U") {
		return besteffort.Scope{}
	}
	occurrences := besteffort.Occurrences(source, "namespace")
	fileScoped := len(occurrences) > 0
	seen := map[string]bool{}
	var names []string
	for _, o := range occurrences {
		if o.Name == "" && o.Next == '@' {
			return besteffort.Scope{}
		}
		if o.Name == "" || o.Next != ';' {
			fileScoped = false
		}
		if o.Name != "" && !seen[o.Name] {
			seen[o.Name] = true
			names = append(names, o.Name)
		}
	}
	return besteffort.Scope{Known: true, Names: names, Global: !fileScoped}
}

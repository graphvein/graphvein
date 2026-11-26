package java

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/besteffort"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// typeKinds maps the type declaration nodes to their node kind and row.
var typeKinds = map[string][2]string{
	"class_declaration":           {"class", "JV-T1"},
	"interface_declaration":       {"interface", "JV-T2"},
	"enum_declaration":            {"enum", "JV-T3"},
	"record_declaration":          {"record", "JV-T4"},
	"annotation_type_declaration": {"annotation", "JV-T5"},
}

// bodyKinds are the type bodies whose direct type members are importable
// member types (enum_body_declarations sits inside enum_body).
var bodyKinds = map[string]bool{"class_body": true, "interface_body": true, "enum_body": true, "enum_body_declarations": true, "annotation_type_body": true}

// Extract returns the file's Java facts: one node per claimed declaration
// with its contains edge, and the best-effort payload (package, importable
// type paths, import directives) that Link resolves. A tree with an ERROR
// or named MISSING node claims nothing (row BE-2); the indexer applies the
// same refusal through besteffort.Refuse when only the parser saw a missing
// token.
func Extract(file string, source []byte, root *parse.Node) facts.Facts {
	return extract(file, source, root, nil)
}

// scope is where a declaration found during the walk belongs.
type scope struct {
	// list receives declarations found here: the owner's Children, or the
	// file's roots.
	list *[]*besteffort.Decl
	// top is set at program level (JV-F4).
	top bool
	// owned is set below an emitted owner (JV-T6).
	owned bool
	// typeName is the nearest enclosing type's name, the constructor name
	// (JV-F2); record holds that type's record header parameters when it
	// is a record, the compact constructor's parameters.
	typeName string
	record   *parse.Node
	// importable is set where a type declared here is a member type
	// reachable through types only (or a top-level type); typePath is the
	// enclosing importable type path.
	importable bool
	typePath   string
}

func (s scope) inner() scope {
	s.importable = false
	s.top = false
	return s
}

func extract(file string, source []byte, root *parse.Node, t tracer) facts.Facts {
	out := facts.Facts{Exports: map[string]string{}}
	if besteffort.Recovered(root) {
		t.trace("BE-2")
		return besteffort.Refuse(out, RefusedScope(source))
	}
	payload := &facts.BestEffortFacts{Version: facts.BestEffortVersion}
	var roots []*besteffort.Decl
	var walk func(n *parse.Node, s scope)
	walkChildren := func(n *parse.Node, s scope, skip *parse.Node) {
		for _, c := range n.Children {
			if c != skip {
				walk(c, s)
			}
		}
	}
	walk = func(n *parse.Node, s scope) {
		if n == nil {
			return
		}
		if tk, ok := typeKinds[n.Kind]; ok {
			name := facts.Text(n.Fields["name"])
			if name == "" {
				t.trace("D")
				walkChildren(n, s.inner(), nil)
				return
			}
			t.trace(tk[1])
			if s.owned {
				t.trace("JV-T6")
			}
			d := &besteffort.Decl{Row: tk[1], Kind: tk[0], Name: name, Node: n}
			*s.list = append(*s.list, d)
			inner := scope{list: &d.Children, owned: true, typeName: name}
			if n.Kind == "record_declaration" {
				inner.record = n.Fields["parameters"]
			}
			if s.importable {
				path := facts.Join(s.typePath, name)
				payload.Types = append(payload.Types, path)
				inner.typePath = path
			}
			body := n.Fields["body"]
			for _, c := range n.Children {
				if c == body {
					walkBody(c, inner, s.importable, walk, t)
					continue
				}
				walk(c, inner.inner())
			}
			return
		}
		switch n.Kind {
		case "program":
			walkChildren(n, s, nil)
		case "package_declaration":
			t.trace("JV-I1")
			for _, c := range n.Children {
				if c.Kind == "scoped_identifier" || c.Kind == "identifier" {
					payload.Package = besteffort.TypeText(source, c, c.Start, c.End)
				}
			}
		case "import_declaration":
			imp := facts.BestEffortImport{Span: facts.Span(n)}
			for _, tok := range n.Tokens {
				if tok == "static" {
					imp.Static = true
				}
			}
			for _, c := range n.Children {
				switch c.Kind {
				case "scoped_identifier", "identifier":
					imp.Name = besteffort.TypeText(source, c, c.Start, c.End)
				case "asterisk":
					imp.Star = true
				}
			}
			if imp.Name == "" {
				t.trace("D")
				return
			}
			payload.Imports = append(payload.Imports, imp)
		case "method_declaration":
			name := facts.Text(n.Fields["name"])
			if name == "" {
				t.trace("D")
				walkChildren(n, s.inner(), nil)
				return
			}
			row := "JV-F1"
			if s.top {
				row = "JV-F4"
			}
			t.trace(row)
			d := &besteffort.Decl{Row: row, Kind: "method", Name: name, Node: n, Overloadable: true, Overload: parameters(source, n.Fields["parameters"])}
			*s.list = append(*s.list, d)
			walkChildren(n, scope{list: &d.Children, owned: true, typeName: s.typeName}, nil)
		case "constructor_declaration", "compact_constructor_declaration":
			name := s.typeName
			if name == "" {
				name = facts.Text(n.Fields["name"])
			}
			if name == "" {
				t.trace("D")
				walkChildren(n, s.inner(), nil)
				return
			}
			t.trace("JV-F2")
			params := n.Fields["parameters"]
			if n.Kind == "compact_constructor_declaration" {
				params = s.record
			}
			d := &besteffort.Decl{Row: "JV-F2", Kind: "constructor", Name: name, Node: n, Overloadable: true, Overload: parameters(source, params)}
			*s.list = append(*s.list, d)
			walkChildren(n, scope{list: &d.Children, owned: true, typeName: s.typeName}, nil)
		case "annotation_type_element_declaration":
			name := facts.Text(n.Fields["name"])
			if name == "" {
				t.trace("D")
				return
			}
			t.trace("JV-F3")
			d := &besteffort.Decl{Row: "JV-F3", Kind: "method", Name: name, Node: n, Overloadable: true, Overload: "()"}
			*s.list = append(*s.list, d)
		case "object_creation_expression":
			// An anonymous class body declares nothing and adds no owner;
			// its arguments are still walked (lambdas, other expressions).
			body := (*parse.Node)(nil)
			for _, c := range n.Children {
				if c.Kind == "class_body" {
					body = c
				}
			}
			if body != nil {
				t.trace("JV-R1")
			}
			walkChildren(n, s.inner(), body)
		case "enum_constant":
			t.trace("JV-R2")
			body := n.Fields["body"]
			if body != nil {
				t.trace("JV-R1")
			}
			walkChildren(n, s.inner(), body)
		case "field_declaration", "constant_declaration", "static_initializer", "module_declaration":
			t.trace("JV-R2")
			walkChildren(n, s.inner(), nil)
		default:
			t.trace("D")
			walkChildren(n, s.inner(), nil)
		}
	}
	walk(root, scope{list: &roots, top: true, importable: true})
	besteffort.Name(roots)
	besteffort.Emit(&out, file, source, roots)
	out.BestEffort = payload
	return out
}

// walkBody walks a type body. Type declarations directly in it are member
// types, importable when their owner is; everything else is not.
func walkBody(body *parse.Node, s scope, ownerImportable bool, walk func(*parse.Node, scope), t tracer) {
	for _, c := range body.Children {
		if bodyKinds[c.Kind] {
			walkBody(c, s, ownerImportable, walk, t)
			continue
		}
		if c.Kind == "block" {
			// An instance initializer adds no owner.
			t.trace("JV-R2")
			walk(c, s.inner())
			continue
		}
		if _, ok := typeKinds[c.Kind]; ok {
			member := s
			member.importable = ownerImportable
			walk(c, member)
			continue
		}
		walk(c, s.inner())
	}
}

// parameters is the BE-6 type list of a formal_parameters node: each
// parameter's type as written with whitespace, comments and annotations
// removed and C-style array dimensions appended, varargs `...` kept,
// parameter names dropped. A receiver parameter (`Foo this`) is not part of
// a Java signature and is left out.
func parameters(source []byte, list *parse.Node) string {
	var types []string
	if list != nil {
		for _, p := range list.Children {
			switch p.Kind {
			case "formal_parameter":
				if facts.Text(p.Fields["name"]) == "this" {
					continue
				}
				typ := p.Fields["type"]
				text := ""
				if typ != nil {
					text = besteffort.TypeText(source, typ, typ.Start, typ.End)
				}
				if dims := p.Fields["dimensions"]; dims != nil {
					text += besteffort.TypeText(source, dims, dims.Start, dims.End)
				}
				types = append(types, text)
			case "spread_parameter":
				start, end := p.Start, p.End
				for _, c := range p.Children {
					if c.Kind == "modifiers" && c.End > start {
						start = c.End
					}
					if c.Kind == "variable_declarator" {
						end = c.Start
					}
				}
				types = append(types, besteffort.TypeText(source, p, start, end))
			}
		}
	}
	return "(" + strings.Join(types, ",") + ")"
}

// RefusedScope reads, from the raw text of a refused (BE-2) file, the
// packages its types can belong to (row BE-8). Every whole-word `package`
// followed by a qualified name counts, in comments and strings too, so the
// file's real package declaration is always among them; a name cut short by
// odd spacing is a prefix of the real one and only hides more. A file with no
// such occurrence declares its types in the unnamed package, and those
// cannot be imported (JLS 7.5: a type in an unnamed package has no canonical
// name, so every import form that names one is a compile-time error;
// javac 21 rejects `import C;`, `import C.D;`, `import C.*;` and
// `import static C.m;` for a class C in the unnamed package), so it hides
// nothing. Java translates `\uXXXX` escapes before it reads keywords, which
// a byte scan cannot follow, so a text with `\u` proves nothing.
func RefusedScope(source []byte) besteffort.Scope {
	if strings.Contains(string(source), "\\u") {
		return besteffort.Scope{}
	}
	seen := map[string]bool{}
	scope := besteffort.Scope{Known: true}
	for _, o := range besteffort.Occurrences(source, "package") {
		if o.Name != "" && !seen[o.Name] {
			seen[o.Name] = true
			scope.Names = append(scope.Names, o.Name)
		}
	}
	return scope
}

// Package typescript extracts conservative structural facts without a type checker.
package typescript

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// Version invalidates cached extraction whenever naming or resolution rules change.
const Version = "typescript-2"

// Import retains bindings so unchanged files can be relinked after target edits.
type Import = facts.Import

// Call postpones cross-file resolution until the complete symbol inventory exists.
type Call = facts.Call

// Facts is an internal cache format, never an extension of the NDJSON contract.
type Facts = facts.Facts

func span(n *parse.Node) contract.Span {
	return contract.Span{StartLine: int(n.Row) + 1, StartColumn: int(n.Column), EndLine: int(n.EndRow) + 1, EndColumn: int(n.EndColumn)}
}
func text(n *parse.Node) string {
	if n == nil {
		return ""
	}
	return n.Text
}
func compact(s string) string {
	var b strings.Builder
	chars := []rune(s)
	var quote rune
	for i := 0; i < len(chars); i++ {
		c := chars[i]
		if quote != 0 {
			b.WriteRune(c)
			if c == '\\' && i+1 < len(chars) {
				i++
				b.WriteRune(chars[i])
			} else if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			b.WriteRune(c)
			continue
		}
		if c == '/' && i+1 < len(chars) && chars[i+1] == '*' {
			i += 2
			for i+1 < len(chars) && !(chars[i] == '*' && chars[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		if c == '/' && i+1 < len(chars) && chars[i+1] == '/' {
			for i < len(chars) && chars[i] != '\n' {
				i++
			}
			continue
		}
		if !unicode.IsSpace(c) {
			b.WriteRune(c)
		}
	}
	return b.String()
}
func prefix(n, start *parse.Node, source []byte) *parse.Node {
	if n.Start <= start.Start {
		return n
	}
	copy := *n
	copy.Start = start.Start
	copy.Row = start.Row
	copy.Column = start.Column
	copy.Text = string(source[copy.Start:copy.End])
	return &copy
}
func join(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}
func id(kind, file, q string) string { v, _ := contract.Identity(kind, file, q); return v }
func edge(kind, src, dst, file string, location contract.Span) contract.Edge {
	return contract.Edge{Type: "edge", Kind: kind, Source: src, Target: dst, Provenance: "fact", Confidence: 1, Evidence: []contract.Evidence{{File: file, Span: location}}}
}

// FileNode supplies a location even when parsing is unsupported or fails.
func FileNode(file contract.File, source []byte) contract.Node {
	if file.Language == "typescript" || file.Language == "tsx" || file.Language == "javascript" {
		structureTag("TS-F1")
	}
	lines := strings.Split(string(source), "\n")
	s := contract.Span{StartLine: 1, EndLine: len(lines), EndColumn: len(lines[len(lines)-1])}
	return contract.Node{Type: "node", Identity: id("file", file.Path, file.Path), Kind: "file", Level: 3, Name: path.Base(file.Path), QualifiedName: file.Path, File: file.Path, Span: &s, ContentHash: file.ContentHash, Attributes: map[string]any{"precision": file.Precision}}
}

// Extract records syntax-backed declarations and unresolved candidates for replay.
func Extract(file string, source []byte, root *parse.Node) Facts {
	out := Facts{Exports: map[string]string{}, Repeated: map[string][]string{}}
	fileID := id("file", file, file)
	// The call binding model (scopes.go, rows TSX-1 to TSX-25): the
	// module scope, the scope the visit is in, and the calls waiting for
	// the binding check once every binding of the file is known.
	moduleScope := newScope(nil, true)
	moduleScope.this = thisOther
	cur := moduleScope
	enter := func(s *lexScope) func() {
		outer := cur
		cur = s
		return func() { cur = outer }
	}
	var sites []callSite
	// bodies marks the function and static block bodies: each opens a
	// function scope (var declarations land there) instead of a block
	// scope; the value is what `this` is in it.
	bodies := map[*parse.Node]thisKind{}
	// staticBodies holds the class of every class static block body.
	staticBodies := map[*parse.Node]*parse.Node{}
	// declared holds the scope a variable declarator binds its name in,
	// for the function or object owner it names (TSX-8, TSX-9).
	declared := map[*parse.Node]*lexScope{}
	// The writes of names, the scopes that call eval directly and the
	// const declarators decide which bindings are written (TSX-19).
	var writes []writeSite
	var evals []*lexScope
	constDecls := map[*parse.Node]bool{}
	// The member writes of the file and what plain declarators hold
	// decide which own methods some code replaces (TSX-25).
	var propertyWrites []memberWrite
	aliases := map[*parse.Node]alias{}
	write := func(target *parse.Node) {
		propertyWrites = memberTargets(target, cur, propertyWrites)
		for target != nil && (target.Kind == "parenthesized_expression" || target.Kind == "non_null_expression") && len(target.Children) > 0 {
			target = target.Children[0]
		}
		if target == nil {
			return
		}
		if target.Kind == "identifier" {
			writes = append(writes, writeSite{name: target.Text, scope: cur})
			return
		}
		for _, name := range boundNames(target) {
			writes = append(writes, writeSite{name: name, scope: cur})
		}
	}
	// memberOf holds the class and object-owner methods that are own
	// properties of their owner, members the same by qualified name
	// (TSX-7, TSX-9, TSX-13, TSX-20, TSX-21).
	memberOf := map[*parse.Node]member{}
	// methodOf holds every class method's class and staticness: what
	// `this` is in it (TSX-13).
	methodOf := map[*parse.Node]member{}
	members := map[string]member{}
	declarations := map[string]int{}
	// The bridge rows' syntax (T026 AG-B5, bridges_extract.go).
	bc := newBridgeCollector(file, source)
	add := func(n *parse.Node, kind, name, q, ownerID string, exported, defaultExport bool, decorators []string) string {
		location := span(n)
		key := id(kind, file, q)
		declarations[key]++
		if declarations[key] > 1 {
			structureTag("TS-D12")
			q += fmt.Sprintf("@%d", declarations[key])
		}
		identity := id(kind, file, q)
		if kind == "function" || kind == "method" {
			out.Repeated[key] = append(out.Repeated[key], identity)
		}
		attrs := map[string]any{}
		if exported {
			attrs["exported"] = true
		}
		if defaultExport {
			attrs["default_export"] = true
		}
		if len(decorators) > 0 {
			attrs["decorators"] = decorators
		}
		out.Nodes = append(out.Nodes, contract.Node{Type: "node", Identity: identity, Kind: kind, Level: 4, Name: name, QualifiedName: q, File: file, Span: &location, ContentHash: contract.ContentHash(source[n.Start:n.End]), Attributes: attrs})
		if ownerID == "" {
			ownerID = fileID
		}
		out.Edges = append(out.Edges, edge("contains", ownerID, identity, file, location))
		if exported && !defaultExport {
			out.Exports[name] = identity
		}
		if defaultExport {
			out.Exports["default"] = identity
		}
		return identity
	}
	var visit func(*parse.Node, string, string, string, bool, bool, []string)
	objectMembers := map[*parse.Node]bool{}
	// Only a declared variable establishes an object owner. Unbound literals
	// retain the surrounding scope, including literals under computed keys.
	var object func(*parse.Node, string, string, string, *parse.Node)
	object = func(n *parse.Node, owner, ownerID, caller string, decl *parse.Node) {
		structureTag("TS-D9")
		for _, c := range n.Children {
			objectMembers[c] = true
		}
		if decl != nil {
			// A method is the object's own property unless another entry
			// of the same key or a later spread replaces it.
			keys := map[string]int{}
			spread := -1
			for i, c := range n.Children {
				switch c.Kind {
				case "spread_element":
					spread = i
				case "shorthand_property_identifier":
					keys[c.Text]++
				case "pair", "method_definition":
					keys[propertyName(c.Fields["key"])+propertyName(c.Fields["name"])]++
				}
			}
			for i, c := range n.Children {
				if c.Kind == "method_definition" && i > spread && !accessor(c) {
					if name := propertyName(c.Fields["name"]); name != "" && keys[name] == 1 {
						memberOf[c] = member{owner: decl}
					}
				}
			}
		}
		for _, c := range n.Children {
			if c.Kind == "pair" && c.Fields["value"] != nil && c.Fields["value"].Kind == "object" {
				key := c.Fields["key"]
				visit(key, owner, ownerID, caller, false, false, nil)
				nested := owner
				nestedDecl := (*parse.Node)(nil)
				if key != nil && (key.Kind == "property_identifier" || key.Kind == "identifier" || key.Kind == "string") {
					name := key.Text
					if key.Kind == "string" && len(name) >= 2 {
						name = name[1 : len(name)-1]
					}
					nested = join(owner, name)
					// A nested object's methods are properties of the
					// owner's binding along the key path; under a computed
					// key they are not reachable by name.
					nestedDecl = decl
				}
				object(c.Fields["value"], nested, ownerID, caller, nestedDecl)
			} else {
				visit(c, owner, ownerID, caller, false, false, nil)
			}
		}
	}
	visit = func(n *parse.Node, owner, ownerID, caller string, exported, defaultExport bool, decorators []string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case "statement_block":
			if this, body := bodies[n]; body {
				s := newScope(cur, true)
				s.this = this
				// A static block's `this` is its class (TSX-25).
				if class := staticBodies[n]; class != nil {
					s.thisClass, s.thisSide = class, sideClass
				}
				defer enter(s)()
			} else {
				defer enter(newScope(cur, false))()
			}
		case "for_statement", "switch_body":
			defer enter(newScope(cur, false))()
		case "for_in_statement":
			s := newScope(cur, false)
			defer enter(s)()
			declares := false
			for _, token := range n.Tokens {
				switch token {
				case "var":
					declares = true
					s.varScope().bindPattern(n.Fields["left"], n, "TSX-1")
				case "let", "const":
					declares = true
					s.bindPattern(n.Fields["left"], n, "TSX-2")
				}
			}
			if !declares {
				// `for (x of xs)` writes x (TSX-19).
				write(n.Fields["left"])
			}
		case "catch_clause":
			s := newScope(cur, false)
			defer enter(s)()
			s.bindPattern(n.Fields["parameter"], n, "TSX-6")
		case "with_statement":
			// The object's properties shadow names in the body (TSX-15).
			s := newScope(cur, false)
			s.uncertain = true
			defer enter(s)()
		case "assignment_expression", "augmented_assignment_expression":
			if selfBind(n) {
				// `this.m = this.m.bind(this)` keeps the method (TSX-25).
				break
			}
			write(n.Fields["left"])
		case "update_expression":
			write(n.Fields["argument"])
		case "unary_expression":
			if hasToken(n, "delete") {
				propertyWrites = memberTargets(n.Fields["argument"], cur, propertyWrites)
			}
		case "class_static_block":
			if body := n.Fields["body"]; body != nil {
				bodies[body] = thisOther
				staticBodies[body] = cur.thisClass
			}
		}
		if n.Kind == "object" {
			for _, c := range n.Children {
				objectMembers[c] = true
			}
		}
		switch n.Kind {
		case "jsx_element":
			if i := bc.routeElement(n, owner, caller); i >= 0 {
				// A <Route>'s nested <Route> elements are its children
				// (createRoutesFromChildren); its attributes are not.
				open := n.Fields["open_tag"]
				visit(open, owner, ownerID, caller, false, false, nil)
				bc.routes = append(bc.routes, i)
				for _, c := range n.Children {
					if c != open {
						visit(c, owner, ownerID, caller, false, false, nil)
					}
				}
				bc.routes = bc.routes[:len(bc.routes)-1]
				return
			}
		case "jsx_self_closing_element":
			bc.routeElement(n, owner, caller)
			bc.tag(n, owner, caller)
		case "jsx_opening_element":
			bc.tag(n, owner, caller)
		case "jsx_attribute":
			bc.attribute(n, owner, caller)
		case "export_statement":
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					decorators = append(decorators, c.Text)
				}
			}
			def := false
			for _, token := range n.Tokens {
				def = def || token == "default"
			}
			if declaration := n.Fields["declaration"]; declaration != nil {
				visit(prefix(declaration, n, source), owner, ownerID, caller, true, def, decorators)
				return
			}
			if value := n.Fields["value"]; value != nil {
				switch {
				case value.Kind == "identifier":
					out.ExportClauses = append(out.ExportClauses, facts.ExportClause{Name: value.Text, Alias: "default", Span: span(n)})
				case defaultExportedValue(value):
					// Row TS-D3: the anonymous function or class that is
					// the default-exported value.
					visit(prefix(value, n, source), owner, ownerID, caller, true, def, decorators)
					return
				}
				// Anything else default-exported (a call, an object, ...)
				// is an expression: nothing nested in it is exported
				// (PS-S3-1).
				visit(value, owner, ownerID, caller, false, false, nil)
				return
			}
			module := strings.Trim(text(n.Fields["source"]), "\"'")
			typeOnly := false
			for _, token := range n.Tokens {
				typeOnly = typeOnly || token == "type"
			}
			if module != "" && typeOnly && hasChild(n, "import_attribute") {
				unread(&out, unreadImportAttributes, 1)
				return
			}
			clause := false
			for _, c := range n.Children {
				if c.Kind == "namespace_export" {
					for _, name := range c.Children {
						if name.Kind == "identifier" || name.Kind == "string" {
							out.ExportClauses = append(out.ExportClauses, facts.ExportClause{Module: module, Alias: strings.Trim(name.Text, "\"'"), Namespace: true, TypeOnly: typeOnly, Span: span(n)})
						}
					}
					clause = true
				}
				if c.Kind == "export_clause" {
					clause = true
					for _, spec := range c.Children {
						if spec.Kind != "export_specifier" {
							continue
						}
						local := strings.Trim(text(spec.Fields["name"]), "\"'")
						alias := strings.Trim(text(spec.Fields["alias"]), "\"'")
						if alias == "" {
							alias = local
						}
						specType := typeOnly
						for _, token := range spec.Tokens {
							specType = specType || token == "type"
						}
						out.ExportClauses = append(out.ExportClauses, facts.ExportClause{Module: module, Name: local, Alias: alias, TypeOnly: specType, Span: span(n)})
						if module == "" {
							out.Exports[alias] = "local:" + local
						}
					}
				}
			}
			if module != "" && !clause {
				out.ExportClauses = append(out.ExportClauses, facts.ExportClause{Module: module, Star: true, TypeOnly: typeOnly, Span: span(n)})
			}
			return
		case "import_statement":
			if importEquals(n) {
				// O-S3-7: `import x = require("s")` is not a claim site.
				unread(&out, unreadImportEquals, 1)
				for _, c := range n.Children {
					if c.Kind == "import_require_clause" {
						for _, v := range c.Children {
							if v.Kind == "identifier" {
								cur.bind(v.Text, binding{kind: bindOpaque, decl: n, row: "TSX-12"})
								break
							}
						}
					}
				}
				return
			}
			if typeOnlyStatement(n) && hasChild(n, "import_attribute") {
				// A type-only import's resolution-mode attribute changes
				// its resolution mode; Graphvein does not model it.
				unread(&out, unreadImportAttributes, 1)
				return
			}
			module := strings.Trim(text(n.Fields["source"]), "\"'")
			imp := Import{Module: module, Bindings: map[string]string{}, Span: span(n)}
			var bindings func(*parse.Node)
			bindings = func(c *parse.Node) {
				switch c.Kind {
				case "import_clause":
					for _, v := range c.Children {
						if v.Kind == "identifier" {
							imp.Bindings[v.Text] = "default"
						} else {
							bindings(v)
						}
					}
				case "import_specifier":
					name := text(c.Fields["name"])
					alias := text(c.Fields["alias"])
					if alias == "" {
						alias = name
					}
					imp.Bindings[alias] = name
				case "namespace_import":
					for _, v := range c.Children {
						if v.Kind == "identifier" {
							imp.Bindings[v.Text] = "*"
						}
					}
				default:
					for _, v := range c.Children {
						bindings(v)
					}
				}
			}
			bindings(n)
			out.Imports = append(out.Imports, imp)
			for local := range imp.Bindings {
				moduleScope.bind(local, binding{kind: bindImport, decl: n, row: "TSX-11"})
			}
			return
		case "internal_module", "module":
			// O-S3-6: namespace and module blocks claim nothing; their
			// members go to D (one count per block).
			if name := n.Fields["name"]; name != nil && name.Kind != "string" {
				base, _, _ := strings.Cut(name.Text, ".")
				cur.bind(base, binding{kind: bindOpaque, decl: n, row: "TSX-12"})
			}
			structureTag("D")
			unread(&out, unreadNamespaceMember, 1)
			return
		case "ambient_declaration":
			for _, c := range n.Children {
				switch c.Kind {
				case "statement_block", "internal_module", "module":
					// `declare global { }`, `declare module "x" { }`,
					// `declare namespace N { }`.
					unread(&out, unreadNamespaceMember, 1)
				default:
					// `declare` is outside the declaration's span (O-S3-5).
					visit(c, owner, ownerID, caller, exported, false, nil)
				}
			}
			return
		case "type_annotation", "type_arguments", "type_parameters", "object_type", "opting_type_annotation", "omitting_type_annotation", "asserts_annotation", "type_predicate_annotation":
			// PS-S3-2: type positions are never read for declarations.
			return
		case "as_expression", "satisfies_expression":
			if len(n.Children) > 0 {
				visit(n.Children[0], owner, ownerID, caller, false, false, nil)
			}
			return
		case "type_assertion":
			for _, c := range n.Children {
				if c.Kind != "type_arguments" {
					visit(c, owner, ownerID, caller, false, false, nil)
				}
			}
			return
		case "class_declaration", "abstract_class_declaration", "class", "interface_declaration", "type_alias_declaration", "enum_declaration":
			name := text(n.Fields["name"])
			if name == "" {
				if !defaultExport {
					return
				}
				name = "default.class"
			}
			kind := "class"
			switch n.Kind {
			case "interface_declaration":
				kind = "interface"
				structureTag("TS-D10")
			case "type_alias_declaration":
				kind = "type_alias"
				structureTag("TS-D10")
			case "enum_declaration":
				kind = "enum"
				structureTag("TS-D10")
			default:
				if name == "default.class" {
					structureTag("TS-D3")
				} else {
					structureTag("TS-D6")
				}
			}
			for _, c := range n.Children {
				if c.Kind == "decorator" {
					decorators = append(decorators, c.Text)
				}
			}
			q := join(owner, name)
			identity := add(n, kind, name, q, ownerID, exported, defaultExport, decorators)
			q = out.Nodes[len(out.Nodes)-1].QualifiedName
			if kind == "enum" {
				cur.bind(name, binding{kind: bindOpaque, decl: n, row: "TSX-12"})
			}
			if kind == "class" {
				ownMethods(n.Fields["body"], n, memberOf, methodOf)
			}
			if kind == "class" {
				s := newScope(cur, false)
				s.this = thisOther
				// `this` in a field initializer is the instance (the class
				// for a static field): member writes there reach the class
				// on either side (TSX-25).
				s.thisClass, s.thisSide = n, sideBoth
				if name != "default.class" {
					own := binding{kind: bindNamed, q: q, decl: n, row: "TSX-7"}
					if n.Kind != "class" {
						// A class declaration binds in the enclosing scope;
						// a class expression's name only inside the class.
						cur.bind(name, own)
					}
					// Inside the class its own name is immutable.
					own.constant = true
					s.bind(name, own)
				}
				defer enter(s)()
			}
			if kind == "class" {
				for _, c := range n.Children {
					if c.Kind != "decorator" {
						visit(c, q, identity, caller, false, false, nil)
					}
				}
			}
			return
		case "function_declaration", "generator_function_declaration", "function_signature", "function_expression", "generator_function", "arrow_function", "method_definition", "method_signature", "abstract_method_signature":
			name := text(n.Fields["name"])
			bound := n.Fields[boundField] != nil
			inObject := objectMembers[n]
			if name == "" {
				if !defaultExport {
					bc.pushFrame(n)
					// An anonymous function has no identity of its own,
					// but its parameters and body are scopes (TSX-5).
					this := thisOther
					if n.Kind == "arrow_function" {
						// An arrow function reads the enclosing `this`.
						this = thisNone
					}
					s := functionScope(cur, n, this)
					if body := n.Fields["body"]; body != nil && body.Kind == "statement_block" {
						bodies[body] = thisNone
					}
					restore := enter(s)
					for _, c := range n.Children {
						visit(c, owner, ownerID, caller, false, false, nil)
					}
					restore()
					bc.popFrame()
					return
				}
				name = "default.function"
				if n.Kind == "arrow_function" {
					name = "default.arrow"
				}
			}
			kind := "function"
			if strings.Contains(n.Kind, "method") {
				kind = "method"
			}
			switch {
			case strings.HasPrefix(name, "default."):
				structureTag("TS-D3")
			case kind == "method" && inObject:
				structureTag("TS-D8")
			case kind == "method":
				structureTag("TS-D7")
			case strings.Contains(n.Kind, "signature"):
				structureTag("TS-D2")
			case bound:
				structureTag("TS-D4")
			case n.Kind == "function_expression" || n.Kind == "generator_function":
				structureTag("TS-D5")
			default:
				structureTag("TS-D1")
			}
			q := join(owner, name)
			if strings.Contains(n.Kind, "signature") {
				q += "@overload" + compact(text(n.Fields["parameters"])+text(n.Fields["return_type"]))
			}
			identity := add(n, kind, name, q, ownerID, exported, defaultExport, decorators)
			q = out.Nodes[len(out.Nodes)-1].QualifiedName
			this := thisOther
			switch {
			case n.Kind == "arrow_function":
				this = thisNone
			case kind == "method" && !inObject:
				this = thisMethod
			}
			s := functionScope(cur, n, this)
			info, isMember := memberOf[n]
			if isMember && kind == "method" {
				members[q] = info
			}
			if this == thisMethod {
				s.method = q
				s.class = methodOf[n].owner
				s.static = methodOf[n].static
			}
			own := binding{kind: bindNamed, q: q, decl: n}
			switch {
			case strings.Contains(n.Kind, "signature") || kind == "method" || strings.HasPrefix(name, "default."):
				// Signatures bind nothing their implementation does not;
				// methods are properties, not lexical bindings.
			case bound:
				// TSX-8: the declarator's binding names this function. A
				// named function expression's own name is visible inside
				// only, and link time does not know it (TSX-10).
				c := n.Fields[boundField]
				own.row = "TSX-8"
				declared[c].named(name, c, q, own.row)
				if v := c.Fields["value"]; v != nil && v.Fields["name"] != nil {
					s.bind(v.Fields["name"].Text, binding{kind: bindOpaque, decl: n, row: "TSX-10"})
				}
			case n.Kind == "function_declaration" || n.Kind == "generator_function_declaration":
				own.row = "TSX-3"
				cur.bind(name, own)
			default:
				// TSX-18: a named function expression binds its name in
				// its own scope; no write can change it.
				own.row = "TSX-18"
				own.constant = true
				s.bind(name, own)
			}
			bc.enterFunction(q, n)
			if body := n.Fields["body"]; body != nil && body.Kind == "statement_block" {
				bodies[body] = thisNone
			}
			restore := enter(s)
			visit(n.Fields["body"], q, identity, identity, false, false, nil)
			restore()
			return
		case "lexical_declaration", "variable_declaration":
			for _, c := range n.Children {
				if c.Kind != "variable_declarator" {
					continue
				}
				if exported {
					c = prefix(c, n, source)
				}
				name := text(c.Fields["name"])
				value := c.Fields["value"]
				target, row := cur, "TSX-2"
				if n.Kind == "variable_declaration" {
					target, row = cur.varScope(), "TSX-1"
				}
				target.bindPattern(c.Fields["name"], c, row)
				declared[c] = target
				if v := unwrapExpression(value); v != nil && c.Fields["name"] != nil && c.Fields["name"].Kind == "identifier" && aliasValue(v) {
					// A member write through the variable writes the
					// object it holds (TSX-25), or no owner when it holds
					// a fresh literal or function (TSX-26).
					aliases[c] = alias{value: v, scope: cur}
				}
				if hasToken(n, "const") {
					constDecls[c] = true
				}
				if owner == "" && caller == "" && len(bc.frames) == 0 {
					bc.moduleConst(c, n)
				}
				if misparsed(value) {
					// PS-S3-3: tree-sitter read `=> tag<T>`...`` as a call
					// of the arrow function; the declarator claims nothing.
					unread(&out, unreadMisparse, 1)
					continue
				}
				if value != nil && (value.Kind == "arrow_function" || value.Kind == "function_expression" || value.Kind == "generator_function") {
					// A const binding supplies a semantic name to an otherwise anonymous function.
					clone := *value
					clone.Start = c.Start
					clone.End = c.End
					clone.Row = c.Row
					clone.Column = c.Column
					clone.EndRow = c.EndRow
					clone.EndColumn = c.EndColumn
					clone.Fields = map[string]*parse.Node{}
					for k, v := range value.Fields {
						clone.Fields[k] = v
					}
					clone.Fields["name"] = c.Fields["name"]
					clone.Fields[boundField] = c
					visit(&clone, owner, ownerID, caller, exported, defaultExport, decorators)
				} else {
					variableID := ""
					if exported && c.Fields["name"] != nil && c.Fields["name"].Kind != "identifier" {
						// PS-S3-9: a binding pattern names no variable.
						unread(&out, unreadBindingPattern, 1)
					} else if exported {
						structureTag("TS-D11")
						variableID = add(c, "variable", name, join(owner, name), ownerID, true, false, decorators)
					}
					if value != nil && value.Kind == "object" && c.Fields["name"].Kind == "identifier" {
						q := join(owner, name)
						if variableID != "" {
							q = out.Nodes[len(out.Nodes)-1].QualifiedName
						} else {
							variableID = ownerID
						}
						// TSX-9: the binding names the object's owner.
						target.named(name, c, q, "TSX-9")
						object(value, q, variableID, caller, c)
					} else {
						visit(value, owner, ownerID, caller, false, false, nil)
					}
				}
			}
			return
		case "call_expression":
			// call is n, or the corrected call of `await f<T>(x)`; the
			// tree's children are still visited below as they are.
			call := awaitedGenericCall(n)
			if callee := call.Fields["function"]; callee != nil {
				switch {
				case callee.Kind == "arrow_function":
					unread(&out, unreadMisparse, 1)
					return
				case awaitMisparse(call):
					// tree-sitter-javascript reads `await (x)` as a call of a
					// function named await (T020-TSG): no call site below; the
					// operand is still read, its calls are real.
					unread(&out, unreadMisparse, 1)
				case callee.Kind == "import":
					unread(&out, unreadDynamicImport, 1)
				case callee.Kind == "identifier" && callee.Text == "require":
					unread(&out, unreadRequireCall, 1)
				case callee.Kind == "identifier" && callee.Text == "eval":
					// A direct eval may declare var bindings in its
					// function (TSX-15) and assign any binding it sees
					// (TSX-19).
					cur.varScope().uncertain = true
					evals = append(evals, cur)
				}
				// Object.defineProperty and the like write members
				// (TSX-25).
				propertyWrites = append(propertyWrites, objectWrites(call, cur)...)
			}
			if !awaitMisparse(call) {
				bc.call(n, owner, caller)
			}
			if caller != "" && !awaitMisparse(call) {
				callee := call.Fields["function"]
				name := text(callee)
				staticName := ""
				if callee != nil && callee.Kind == "member_expression" {
					staticName = text(callee.Fields["property"])
				}
				if callee != nil && (callee.Kind == "identifier" || callee.Kind == "member_expression") {
					name = strings.ReplaceAll(name, "?.", ".")
				} else {
					name = ""
				}
				out.Calls = append(out.Calls, Call{StaticName: staticName, Source: caller, Scope: owner, Name: name, Span: span(call)})
				sites = append(sites, callSite{index: len(out.Calls) - 1, scope: cur})
			}
		}
		var pendingDecorators []string
		var firstDecorator *parse.Node
		for _, c := range n.Children {
			if c.Kind == "decorator" {
				if firstDecorator == nil {
					firstDecorator = c
				}
				pendingDecorators = append(pendingDecorators, c.Text)
				continue
			}
			if c.Kind == "comment" && firstDecorator != nil {
				// PS-S3-10: decorators bind to the next member across
				// comments.
				continue
			}
			if firstDecorator != nil {
				member := c
				c = prefix(c, firstDecorator, source)
				if objectMembers[member] {
					objectMembers[c] = true
				}
				if info, ok := memberOf[member]; ok {
					memberOf[c] = info
				}
				if info, ok := methodOf[member]; ok {
					methodOf[c] = info
				}
			}
			// Nothing nested in an expression or block is exported
			// (PS-S3-1); flags reach only the declaration they name.
			visit(c, owner, ownerID, caller, false, false, pendingDecorators)
			pendingDecorators = nil
			firstDecorator = nil
		}
	}
	visit(root, "", "", "", false, false, nil)
	for key, identities := range out.Repeated {
		if len(identities) < 2 {
			delete(out.Repeated, key)
		}
	}
	// Bindings hold throughout their scope, before their declaration too:
	// the check runs once the whole file is visited.
	symbols := map[string]bool{}
	methods := map[string]bool{}
	repeated := map[string]bool{}
	repeatedIDs := map[string]bool{}
	for _, identities := range out.Repeated {
		for _, identity := range identities {
			repeatedIDs[identity] = true
		}
	}
	for _, node := range out.Nodes {
		if node.Kind == "function" || node.Kind == "method" {
			symbols[node.QualifiedName] = true
			methods[node.QualifiedName] = node.Kind == "method"
			repeated[node.QualifiedName] = repeatedIDs[node.Identity]
		}
	}
	markWrites(writes, evals, constDecls)
	written := resolveMemberWrites(propertyWrites, aliases)
	checkCalls(out.Calls, sites, fileSymbols{symbols: symbols, methods: methods, repeated: repeated}, members, written)
	out.MemberWrittenNames = written.names()
	for alias, identity := range out.Exports {
		if strings.HasPrefix(identity, "local:") {
			delete(out.Exports, alias)
			for _, n := range out.Nodes {
				if n.QualifiedName == strings.TrimPrefix(identity, "local:") {
					out.Exports[alias] = n.Identity
					break
				}
			}
		}
	}
	if syn := bc.finish(out.Nodes); syn != nil {
		if out.TypeScript == nil {
			out.TypeScript = &facts.TypeScriptFacts{}
		}
		out.TypeScript.Bridges = syn
	}
	extractRoutes(file, source, root, &out)
	extractShapes(root, &out)
	extractNext(file, root, &out)
	extractActions(source, root, &out)
	return out
}

// boundField marks a function node that a variable declarator names (row
// TS-D4); it is never a tree-sitter field name.
const boundField = "graphvein:bound"

// defaultExportedValue reports the forms of row TS-D3: an anonymous
// function, generator, arrow function or class that is itself the
// default-exported value.
func defaultExportedValue(v *parse.Node) bool {
	switch v.Kind {
	case "arrow_function":
		return true
	case "function_expression", "generator_function", "class":
		return v.Fields["name"] == nil
	}
	return false
}

// misparsed reports tree-sitter's reading of an arrow function whose body
// starts with a tagged template with type arguments as a call of the arrow
// function (PS-S3-3): valid TypeScript calls an arrow function only in
// parentheses.
// awaitMisparse reports a call whose callee is the bare identifier await.
// Inside an async function or a module that text is the await operator
// applied to a parenthesised operand, which tree-sitter-javascript 0.25.0
// reads as a call; in a sloppy script it could be a real call of a function
// named await. Either way the call claims nothing, so no call site (an
// unknown target at that span) is invented.
func awaitMisparse(n *parse.Node) bool {
	callee := n.Fields["function"]
	return callee != nil && callee.Kind == "identifier" && callee.Text == "await"
}

// awaitedGenericCall corrects tree-sitter-typescript's reading of
// `await f<T>(x)` as a call of `await f` with type arguments (the grammar
// correction under "Call binding model" in
// docs/lang/typescript-resolution.md). ECMAScript's await takes a unary
// expression, so the source always awaits the call `f<T>(x)`; the
// written form `(await f)<T>(x)` keeps its parentheses in the tree and is
// not changed. The returned call has the awaited operand as its callee and
// starts there; any other node is returned as it is.
func awaitedGenericCall(n *parse.Node) *parse.Node {
	callee := n.Fields["function"]
	if callee == nil || callee.Kind != "await_expression" || len(callee.Children) != 1 || n.Fields["type_arguments"] == nil {
		return n
	}
	operand := callee.Children[0]
	fixed := *n
	fixed.Fields = make(map[string]*parse.Node, len(n.Fields))
	for k, v := range n.Fields {
		fixed.Fields[k] = v
	}
	fixed.Fields["function"] = operand
	fixed.Start, fixed.Row, fixed.Column = operand.Start, operand.Row, operand.Column
	return &fixed
}

func misparsed(v *parse.Node) bool {
	return v != nil && v.Kind == "call_expression" && v.Fields["function"] != nil && v.Fields["function"].Kind == "arrow_function"
}

// importEquals reports `import x = require("s")`.
func importEquals(n *parse.Node) bool {
	return hasChild(n, "import_require_clause")
}

func hasChild(n *parse.Node, kind string) bool {
	for _, c := range n.Children {
		if c.Kind == kind {
			return true
		}
	}
	return false
}

// typeOnlyStatement reports `import type ...` or `export type ... from`.
func typeOnlyStatement(n *parse.Node) bool {
	for _, t := range n.Tokens {
		if t == "type" {
			return true
		}
	}
	return false
}

// Link rebuilds all cross-file edges, including candidates from unchanged cached files.
func Link(files map[string]Facts) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkWithResolver(files, NewModuleResolver(files, nil))
}

// LinkWithResolver lets every consumer share the same current metadata and exports.
func LinkWithResolver(files map[string]Facts, resolver *ModuleResolver) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	var nodes []contract.Node
	var edges []contract.Edge
	diagnostics := append([]contract.Diagnostic(nil), resolver.Diagnostics()...)
	externals := map[string]bool{}
	assetNodes := map[string]contract.Node{}
	ambiguous := map[string]bool{}
	for _, f := range files {
		for _, identities := range f.Repeated {
			for _, identity := range identities {
				ambiguous[identity] = true
			}
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	withheld := RejectedOwners(files)
	for _, file := range paths {
		f := files[file]
		f.UnknownSites = nil
		if Rejected(f) {
			// Row TS-F2: no contains, imports or calls edge; the count only.
			diagnostics = append(diagnostics, unreadDiagnostic(file, f.TypeScript.Unread)...)
			files[file] = f
			continue
		}
		edges = append(edges, f.Edges...)
		symbols := map[string]string{}
		for _, n := range f.Nodes {
			if n.Kind == "function" || n.Kind == "method" {
				symbols[n.QualifiedName] = n.Identity
			}
		}
		bindings := map[string]ExportBinding{}
		externalBindings := map[string]bool{}
		imports := append([]Import(nil), f.Imports...)
		seenExports := map[string]bool{}
		for _, c := range f.ExportClauses {
			key := fmt.Sprintf("%s:%v", c.Module, c.Span)
			if c.Module != "" && !seenExports[key] {
				imports = append(imports, Import{Module: c.Module, Span: c.Span})
				seenExports[key] = true
			}
		}
		counts := map[string]int{}
		if f.TypeScript != nil {
			for form, n := range f.TypeScript.Unread {
				counts[form] += n
			}
		}
		for _, imp := range imports {
			if imp.Module == "" {
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "empty import specifier"})
				continue
			}
			if strings.HasPrefix(imp.Module, "/") {
				// Row TS-I6: web-root assets need bundler configuration,
				// not npm resolution.
				structureTag("TS-I6")
				identity := facts.External(assetNodes, "web", imp.Module)
				n := assetNodes[identity]
				n.Attributes["unresolved"] = true
				n.ContentHash, _ = contract.SyntheticHash(n.Attributes)
				assetNodes[identity] = n
				edges = append(edges, edge("imports", id("file", file, file), identity, file, imp.Span))
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "web-root import " + imp.Module + " cannot be resolved without bundler configuration"})
				for local := range imp.Bindings {
					externalBindings[local] = true
				}
				continue
			}
			resolution := resolver.Resolve(file, imp.Module)
			switch {
			case resolution.Refusal != "":
				structureTag("D")
				counts[resolution.Refusal]++
			case resolution.File != "":
				structureTag(resolution.Row)
				target := resolution.File
				edges = append(edges, edge("imports", id("file", file, file), id("file", target, target), file, imp.Span))
				for local, name := range imp.Bindings {
					bindings[local] = resolver.ImportBinding(file, imp.Module, name, "")
				}
			case resolution.External:
				// Row TS-I5: a package of this name outside the repository.
				structureTag("TS-I5")
				packageName := packageName(imp.Module)
				identity := id("external", ".", "npm:"+packageName)
				if !externals[identity] {
					attrs := map[string]any{"ecosystem": "npm", "name": packageName}
					hash, _ := contract.SyntheticHash(attrs)
					nodes = append(nodes, contract.Node{Type: "node", Identity: identity, Kind: "external", Level: 1, Name: packageName, QualifiedName: "npm:" + packageName, File: ".", ContentHash: hash, Attributes: attrs})
					externals[identity] = true
				}
				edges = append(edges, edge("imports", id("file", file, file), identity, file, imp.Span))
				for local := range imp.Bindings {
					externalBindings[local] = true
				}
			default:
				// Row TS-I7.
				structureTag("TS-I7")
				diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "unresolved_import", Message: "relative import " + imp.Module + " is unavailable"})
			}
		}
		diagnostics = append(diagnostics, unreadDiagnostic(file, counts)...)
		external, unknown := 0, 0
		type callEnd struct{ line, column int }
		staticSites := map[callEnd]facts.ClientCall{}
		if f.Routes != nil {
			for _, c := range f.Routes.Clients {
				if c.Expression.Kind == "call_expression" && c.Expression.Object != nil {
					staticSites[callEnd{c.Span.EndLine, c.Span.EndColumn}] = c
				}
			}
		}
		// Match by the expression's end as well: the TS grammar sometimes
		// includes await in the ordinary call span but not the client span.
		usedSites := map[contract.Span]bool{}
		for _, call := range f.Calls {
			site, hasSite := staticSites[callEnd{call.Span.EndLine, call.Span.EndColumn}]
			if hasSite {
				usedSites[site.Span] = true
			}
			target := ""
			if !call.Shadowed && call.Lexical != "" {
				// The call's lexical binding is this declaration, although
				// the qualified-name lookup below would pick another one
				// (row TSX-22).
				target = symbols[call.Lexical]
			} else if !call.Shadowed && call.Name != "" {
				name := call.Name
				if strings.HasPrefix(name, "this.") {
					owner := call.Scope
					if i := strings.LastIndex(owner, "."); i >= 0 {
						owner = owner[:i]
						name = owner + strings.TrimPrefix(name, "this")
					}
					target = symbols[name]
				}
				scope := call.Scope
				// An import binding resolves through the module resolver
				// only (row TSX-23).
				for target == "" && !call.LexicalImport {
					target = symbols[join(scope, name)]
					if scope == "" {
						break
					}
					if i := strings.LastIndex(scope, "."); i >= 0 {
						scope = scope[:i]
					} else {
						scope = ""
					}
				}
				if target == "" {
					base, member, _ := strings.Cut(name, ".")
					binding := bindings[base]
					if member != "" {
						binding = resolver.Member(binding, member)
					}
					target = binding.Identity
					if binding.External {
						externalBindings[base] = true
					}
				}
			}
			// Imported values may be classes, variables or interfaces, not callable symbols.
			if ambiguous[target] && call.Lexical == "" {
				// A repeated declaration (ADR 0011): keep these calls unresolved
				// rather than retargeting the formerly colliding declaration.
				// A call that carries its lexical binding (TSX-22) names the
				// one declaration it evaluates, repeated qualified name or not.
				target = ""
			}
			if target != "" {
				kind, _, _, _ := contract.ParseIdentity(target)
				if kind != "function" && kind != "method" {
					target = ""
				}
			}
			if target == "" && site.Expression.Object != nil && !call.MemberWritten && !unknownWritten(f.MemberWrittenNames, *site.Expression.Object) {
				// A member the file writes stays unresolved (rows TSX-25,
				// TSX-26), also when the binding model left the call to
				// this resolver.
				target = resolver.StaticCall(file, site.Scope, *site.Expression.Object)
			}
			if _, ok := withheld[target]; ok {
				// A declaration of a file row TS-F2 refuses is not emitted.
				target = ""
			}
			if target == "" {
				base := strings.Split(call.Name, ".")[0]
				if !call.Shadowed && (externalBindings[base] || facts.Builtin(base, "console JSON Math Object Array Promise String Number Boolean Date RegExp Map Set WeakMap WeakSet Symbol BigInt Intl fetch parseInt parseFloat isNaN isFinite setTimeout setInterval clearTimeout clearInterval queueMicrotask")) {
					external++
				} else {
					unknown++
					f.UnknownSites = append(f.UnknownSites, facts.Unknown(call))
				}
				continue
			}
			edges = append(edges, edge("calls", call.Source, target, file, call.Span))
		}
		// Existing ordinary extraction omits module evaluation and anonymous
		// top-level callbacks. Add only proven static calls at those sites;
		// their other calls keep the previous diagnostics and resolution.
		if f.Routes != nil {
			for _, c := range f.Routes.Clients {
				if usedSites[c.Span] || c.Expression.Kind != "call_expression" || c.Expression.Object == nil {
					continue
				}
				if unknownWritten(f.MemberWrittenNames, *c.Expression.Object) {
					// A write through a receiver of unknown owner may
					// replace the member (row TSX-26).
					continue
				}
				if target := resolver.StaticCall(file, c.Scope, *c.Expression.Object); target != "" && withheld[target] == "" {
					edges = append(edges, edge("calls", c.Owner, target, file, c.Span))
				}
			}
		}
		files[file] = f
		diagnostics = append(diagnostics, facts.Unresolved(file, external, unknown)...)
	}
	for _, n := range assetNodes {
		nodes = append(nodes, n)
	}
	// The contract uses one edge per kind/endpoints; retain every evidence span.
	merged := map[string]contract.Edge{}
	for _, e := range edges {
		key := e.Kind + "\x00" + e.Source + "\x00" + e.Target
		if old, ok := merged[key]; ok {
			old.Evidence = append(old.Evidence, e.Evidence...)
			merged[key] = old
		} else {
			merged[key] = e
		}
	}
	edges = nil
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		edges = append(edges, merged[key])
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Identity < nodes[j].Identity })
	return nodes, edges, diagnostics
}

// boundNames returns the names a declarator's name binds: the identifier
// itself, or every identifier of a destructuring pattern (array and object
// patterns, shorthand properties, the value side of key: value pairs, the
// left side of defaults and rest elements). Default values and computed
// keys bind nothing.
func boundNames(n *parse.Node) []string {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case "identifier", "shorthand_property_identifier_pattern":
		return []string{n.Text}
	case "pair_pattern":
		return boundNames(n.Fields["value"])
	case "assignment_pattern", "object_assignment_pattern":
		return boundNames(n.Fields["left"])
	case "array_pattern", "object_pattern", "rest_pattern":
		var names []string
		for _, c := range n.Children {
			names = append(names, boundNames(c)...)
		}
		return names
	}
	return nil
}

package typescript

import (
	"path"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/parse"
)

// This file holds the version 5 parts of the model payload (ADR 0064,
// T018g6): the TypeORM gate rows G10-G12, class decorator lists, instance
// members with their declared types (rows M-* and T-*) and module-level enum
// declarations (row S8). Like the rest of the payload it is syntax only: which
// decorator is TypeORM's `Column`, which class is an entity and which enum a
// default names are decided at link time through the TypeScript resolver.

// Class member kinds (ModelClassMember.Kind).
const (
	MemberProperty = "property" // Name, the modifier flags, Type, Decorators
	MemberMethod   = "method"   // Name, Decorators (no body)
	MemberGet      = "get"      // Name, Decorators (no body)
	MemberSet      = "set"      // Name, Decorators (no body)
	MemberUnknown  = "unknown"  // Decorators for a computed or private name; nothing for a constructor or an index signature
)

// ModelClassMember is one instance member of a captured class, in source order
// (version 5). Static members stay in ModelStatement.Statics. Only the
// declaration is kept: a property's initializer and a method's body are not,
// because TypeORM reads a member's decorators and declared type, never its
// value.
type ModelClassMember struct {
	Kind string        `json:"kind"`
	Span contract.Span `json:"span"`
	// Name is a plain identifier, string or number key, decoded; it is ""
	// on an unknown member.
	Name     string `json:"name,omitempty"`
	Optional bool   `json:"optional,omitempty"` // `name?: T`
	Definite bool   `json:"definite,omitempty"` // `name!: T`
	Abstract bool   `json:"abstract,omitempty"`
	Declare  bool   `json:"declare,omitempty"`
	Readonly bool   `json:"readonly,omitempty"`
	// HasInitializer is true for `name = value`; the value is not kept.
	HasInitializer bool `json:"has_initializer,omitempty"`
	// Type is the declared type of a property, nil without an annotation.
	// TypeORM infers a column type from the emitted design type, which
	// depends on this annotation (T018g6).
	Type *ModelType `json:"type,omitempty"`
	// Decorators are the member's decorator expressions in source order.
	Decorators []ModelExpr `json:"decorators,omitempty"`
}

// Declared type kinds (ModelType.Kind).
const (
	TypeKeyword      = "keyword"      // Name: string, number, boolean, bigint, any, unknown, object, never, void, undefined, null, symbol
	TypeReference    = "reference"    // Name (dotted), Arguments (count), Types (only for Promise<T> and Array<T>)
	TypeUnion        = "union"        // Types, flattened in source order
	TypeIntersection = "intersection" // Types, flattened in source order
	TypeArray        = "array"        // Types: the element type
	TypeLiteral      = "literal"      // Literal (string, number, boolean), Value
	TypeTuple        = "tuple"        // Types
	TypeFunction     = "function"     // nothing
	TypeUnknown      = "unknown"      // nothing
)

// modelTypeMaxDepth bounds a declared type tree; deeper types are unknown.
// Column annotations are shallow (`string | null`, `Promise<User[]>`).
const modelTypeMaxDepth = 8

// ModelType is a reduced declared type (version 5). Parentheses are
// transparent; every form outside the kinds above is unknown.
type ModelType struct {
	Kind string `json:"kind"`
	// Name is a keyword or a reference's dotted name.
	Name string `json:"name,omitempty"`
	// Literal is the literal type's kind and Value its decoded string,
	// number source text (with a leading -) or true/false.
	Literal string `json:"literal,omitempty"`
	Value   string `json:"value,omitempty"`
	// Arguments is a reference's type-argument count.
	Arguments int `json:"arguments,omitempty"`
	// Types are the members of a union, intersection or tuple, an array's
	// element type, or the type arguments of Promise<T> and Array<T>.
	Types []ModelType `json:"types,omitempty"`
}

// ModelEnumMember is one member of an enum declaration (row S8). Value is
// nil when the member has no initializer (an auto-increment member); a
// string or number literal (a `-` number included) is kept, any other
// initializer is unknown.
type ModelEnumMember struct {
	Name  string        `json:"name"`
	Span  contract.Span `json:"span"`
	Value *ModelExpr    `json:"value,omitempty"`
}

var modelMemberKinds = map[string]bool{MemberProperty: true, MemberMethod: true, MemberGet: true, MemberSet: true, MemberUnknown: true}

var modelTypeKeywords = map[string]bool{
	"string": true, "number": true, "boolean": true, "bigint": true, "any": true, "unknown": true,
	"object": true, "never": true, "void": true, "undefined": true, "null": true, "symbol": true,
}

func (m *ModelClassMember) valid(depth int) bool {
	if !modelMemberKinds[m.Kind] || (m.Kind != MemberUnknown && m.Name == "") {
		return false
	}
	if m.Type != nil && (m.Kind != MemberProperty || !m.Type.valid(0)) {
		return false
	}
	for i := range m.Decorators {
		if !m.Decorators[i].valid(depth + 1) {
			return false
		}
	}
	return true
}

func (t *ModelType) valid(depth int) bool {
	if t == nil || depth > modelTypeMaxDepth+1 {
		return false
	}
	for i := range t.Types {
		if !t.Types[i].valid(depth + 1) {
			return false
		}
	}
	switch t.Kind {
	case TypeKeyword:
		return modelTypeKeywords[t.Name] && len(t.Types) == 0
	case TypeReference:
		kept := t.Name == "Promise" || t.Name == "Array"
		return t.Name != "" && t.Arguments >= 0 && (kept && len(t.Types) == t.Arguments || !kept && len(t.Types) == 0)
	case TypeUnion, TypeIntersection:
		return len(t.Types) >= 2
	case TypeArray:
		return len(t.Types) == 1
	case TypeLiteral:
		return (t.Literal == "string" || t.Literal == "number" && t.Value != "" || t.Literal == "boolean" && (t.Value == "true" || t.Value == "false")) && len(t.Types) == 0
	case TypeTuple:
		return true
	case TypeFunction, TypeUnknown:
		return len(t.Types) == 0
	}
	return false
}

func enumMembersValid(list []ModelEnumMember, depth int) bool {
	for i := range list {
		v := list[i].Value
		if v != nil && ((v.Kind != ModelString && v.Kind != ModelNumber && v.Kind != ModelUnknown) || !v.valid(depth+1)) {
			return false
		}
	}
	return true
}

// typeormModule matches `typeorm` and its subpaths, never packages that
// share the prefix (`typeorm-naming-strategies`).
func typeormModule(module string) bool {
	return module == "typeorm" || strings.HasPrefix(module, "typeorm/")
}

// nestTypeormModule matches `@nestjs/typeorm` and its subpaths (G10 from
// T018g6): NestJS root and feature modules and option consts
// typed only with TypeOrmModuleOptions import nothing from `typeorm`, and
// discovery reads their forRoot, forRootAsync and forFeature calls and
// option objects (rows TO-K3, TO-K4, TO-K7).
func nestTypeormModule(module string) bool {
	return module == "@nestjs/typeorm" || strings.HasPrefix(module, "@nestjs/typeorm/")
}

// typeormConfigNames are the ormconfig script names TypeORM 0.3 reads
// (connection/ConnectionOptionsReader.js:193 at 0.3.31; G10 from
// T018g6).
var typeormConfigNames = map[string]bool{"ormconfig.ts": true, "ormconfig.js": true}

// typeormEntityDecorators are the class decorator names of row G11.
var typeormEntityDecorators = map[string]bool{"Entity": true, "ViewEntity": true, "ChildEntity": true}

// typeormGate returns the first TypeORM gate row a file passes (version 5):
//
//   - G10 the file imports `typeorm`, `@nestjs/typeorm` or a subpath of
//     either (value or type import), or is named ormconfig.ts or
//     ormconfig.js (TypeORM 0.3's options file, which often imports
//     nothing);
//   - G11 it has a module-level class with a class decorator whose callee is
//     an identifier named Entity, ViewEntity or ChildEntity (any import
//     source: a barrel that re-exports TypeORM's decorators);
//   - G12 it declares a module-level `enum` or `const enum` (plain or
//     `export`): enum members are column defaults and enum column values,
//     and the enum modules import nothing from an ORM.
func typeormGate(file string, root *parse.Node, f Facts) string {
	for _, imp := range f.Imports {
		if typeormModule(imp.Module) || nestTypeormModule(imp.Module) {
			return "G10"
		}
	}
	if typeormConfigNames[path.Base(file)] {
		return "G10"
	}
	if root == nil {
		return ""
	}
	if strings.Contains(root.Text, "Entity") && entityClassModule(root) {
		return "G11"
	}
	if strings.Contains(root.Text, "enum") {
		for _, n := range root.Children {
			if moduleEnum(n) != nil {
				return "G12"
			}
		}
	}
	return ""
}

// moduleEnum returns the enum declaration of a module-level `enum`,
// `const enum` or `export` of one; `declare enum` is ambient and not kept.
func moduleEnum(n *parse.Node) *parse.Node {
	if n.Kind == "enum_declaration" {
		return n
	}
	if n.Kind == "export_statement" {
		if d := n.Fields["declaration"]; d != nil && d.Kind == "enum_declaration" {
			return d
		}
	}
	return nil
}

// entityClassModule reports row G11's module-level entity class.
func entityClassModule(root *parse.Node) bool {
	entity := func(holder *parse.Node) bool {
		for _, d := range holder.Children {
			if d.Kind != "decorator" {
				continue
			}
			for _, c := range modelChildren(d) {
				if c.Kind == "call_expression" {
					c = c.Fields["function"]
				}
				if c != nil && c.Kind == "identifier" && typeormEntityDecorators[c.Text] {
					return true
				}
			}
		}
		return false
	}
	for _, n := range root.Children {
		switch n.Kind {
		case "class_declaration", "abstract_class_declaration":
			if entity(n) {
				return true
			}
		case "export_statement":
			d := n.Fields["declaration"]
			if d == nil {
				d = n.Fields["value"]
			}
			if d != nil && (d.Kind == "class_declaration" || d.Kind == "abstract_class_declaration" || d.Kind == "class") && (entity(n) || entity(d)) {
				return true
			}
		}
	}
	return false
}

// enum records a module-level enum declaration (row S8, version 5). holder
// is the export statement when there is one.
func (s *ModelSyntax) enum(n, holder *parse.Node, exported bool) {
	st := ModelStatement{Kind: StatementEnum, Span: span(holder), Exported: exported, Const: modelHasToken(n, "const"), EnumMembers: []ModelEnumMember{}}
	if name := n.Fields["name"]; name != nil {
		st.Name = name.Text
	}
	for _, m := range modelChildren(n.Fields["body"]) {
		member := ModelEnumMember{Span: span(m)}
		key, value := m, (*parse.Node)(nil)
		if m.Kind == "enum_assignment" {
			key, value = m.Fields["name"], m.Fields["value"]
		}
		member.Name, _ = modelKey(key)
		if value != nil {
			var v ModelExpr
			if inner := modelUnwrap(value); inner != nil && (inner.Kind == "string" || inner.Kind == "number" || inner.Kind == "unary_expression" && modelSignedNumber(inner)) {
				v = reduceModel(value, 1)
			}
			if v.Kind != ModelString && v.Kind != ModelNumber {
				v = modelUnknown(value, nil)
			}
			member.Value = &v
		}
		st.EnumMembers = append(st.EnumMembers, member)
	}
	s.Statements = append(s.Statements, st)
	tagModel("S8", st)
}

// modelSignedNumber reports `-n` or `+n` with n a number literal.
func modelSignedNumber(n *parse.Node) bool {
	arg := modelUnwrap(n.Fields["argument"])
	return arg != nil && arg.Kind == "number" && (modelHasToken(n, "-") || modelHasToken(n, "+"))
}

// classDecorators reduces the decorators that are direct children of p (a
// class declaration, the export statement holding one, or a class member).
func classDecorators(p *parse.Node, depth int) []ModelExpr {
	var out []ModelExpr
	if p == nil {
		return out
	}
	for _, c := range p.Children {
		if c.Kind == "decorator" {
			out = append(out, decoratorExpr(c, depth))
		}
	}
	return out
}

// decoratorExpr reduces one decorator: `@f(args)` is a call, `@f` and
// `@a.b` an identifier or member.
func decoratorExpr(d *parse.Node, depth int) ModelExpr {
	inner := modelChildren(d)
	if len(inner) != 1 {
		return modelUnknown(d, nil)
	}
	return reduceModel(inner[0], depth+1)
}

// classMembers reads a class body's instance members in order (version 5).
// Decorators of methods are children of the class body that precede the
// method; decorators of fields are children of the field.
func classMembers(body *parse.Node, depth int) []ModelClassMember {
	var out []ModelClassMember
	var pending []ModelExpr
	for _, c := range body.Children {
		switch c.Kind {
		case "decorator":
			pending = append(pending, decoratorExpr(c, depth))
			continue
		case "comment":
			continue
		}
		decorators := append(pending, classDecorators(c, depth)...)
		pending = nil
		if m, ok := classMember(c, decorators, depth); ok {
			out = append(out, m)
		}
	}
	return out
}

// classMember reads one class body member; static members and static
// blocks are not instance members (they are ModelStatement.Statics).
func classMember(c *parse.Node, decorators []ModelExpr, depth int) (ModelClassMember, bool) {
	m := ModelClassMember{Span: span(c), Decorators: decorators}
	switch c.Kind {
	case "public_field_definition", "field_definition", "method_definition", "method_signature", "abstract_method_signature":
		if modelHasToken(c, "static") {
			return m, false
		}
	case "class_static_block":
		return m, false
	default:
		// Index signatures and anything else the grammar puts in a body.
		m.Kind, m.Decorators = MemberUnknown, nil
		tagModel("M-unknown", m)
		return m, true
	}
	key := c.Fields["name"]
	if key == nil {
		key = c.Fields["property"]
	}
	// A private name (#x) and a computed key are not plain keys.
	name, ok := modelKey(key)
	switch {
	case c.Kind == "method_definition" && name == "constructor":
		// Parameter properties declare instance properties the payload
		// does not read; the constructor's decorators are not kept.
		m.Kind, m.Decorators = MemberUnknown, nil
		tagModel("M-unknown", m)
		return m, true
	case !ok:
		m.Kind = MemberUnknown
		tagModel("M-unknown", m)
		return m, true
	}
	m.Name = name
	if c.Kind == "public_field_definition" || c.Kind == "field_definition" {
		m.Kind = MemberProperty
		m.Optional = modelHasToken(c, "?")
		m.Definite = modelHasToken(c, "!")
		m.Abstract = modelHasToken(c, "abstract")
		m.Declare = modelHasToken(c, "declare")
		m.Readonly = modelHasToken(c, "readonly")
		m.HasInitializer = c.Fields["value"] != nil
		if a := c.Fields["type"]; a != nil {
			if inner := modelChildren(a); len(inner) == 1 {
				t := reduceType(inner[0], 0)
				m.Type = &t
			}
		}
		tagModel("M-property", m)
		return m, true
	}
	m.Kind = MemberMethod
	if modelHasToken(c, "get") {
		m.Kind = MemberGet
	} else if modelHasToken(c, "set") {
		m.Kind = MemberSet
	}
	tagModel("M-method", m)
	return m, true
}

// reduceType reduces a declared type (rows T-*). Every list skips comments.
func reduceType(n *parse.Node, depth int) ModelType {
	if n != nil && n.Kind == "parenthesized_type" {
		if inner := modelChildren(n); len(inner) == 1 {
			t := reduceType(inner[0], depth)
			tagModel("T-wrap", t)
			return t
		}
	}
	unknown := func() ModelType {
		t := ModelType{Kind: TypeUnknown}
		tagModel("T-unknown", t)
		return t
	}
	if n == nil || depth > modelTypeMaxDepth {
		return unknown()
	}
	list := func(kind string, nodes []*parse.Node) ModelType {
		t := ModelType{Kind: kind, Types: []ModelType{}}
		for _, c := range nodes {
			t.Types = append(t.Types, reduceType(c, depth+1))
		}
		return t
	}
	switch n.Kind {
	case "predefined_type":
		if !modelTypeKeywords[n.Text] {
			return unknown()
		}
		t := ModelType{Kind: TypeKeyword, Name: n.Text}
		tagModel("T-keyword", t)
		return t
	case "literal_type":
		inner := modelChildren(n)
		if len(inner) != 1 {
			return unknown()
		}
		var t ModelType
		switch c := inner[0]; c.Kind {
		case "null", "undefined":
			t = ModelType{Kind: TypeKeyword, Name: c.Kind}
			tagModel("T-keyword", t)
			return t
		case "string":
			v, ok := jsString(c.Text)
			if !ok {
				return unknown()
			}
			t = ModelType{Kind: TypeLiteral, Literal: "string", Value: v}
		case "number":
			t = ModelType{Kind: TypeLiteral, Literal: "number", Value: c.Text}
		case "unary_expression":
			if !modelSignedNumber(c) || !modelHasToken(c, "-") {
				return unknown()
			}
			t = ModelType{Kind: TypeLiteral, Literal: "number", Value: "-" + modelUnwrap(c.Fields["argument"]).Text}
		case "true", "false":
			t = ModelType{Kind: TypeLiteral, Literal: "boolean", Value: c.Kind}
		default:
			return unknown()
		}
		tagModel("T-literal", t)
		return t
	case "type_identifier", "nested_type_identifier", "generic_type":
		name := n
		var args []*parse.Node
		if n.Kind == "generic_type" {
			name = n.Fields["name"]
			args = modelChildren(n.Fields["type_arguments"])
		}
		if name == nil || (name.Kind != "type_identifier" && name.Kind != "nested_type_identifier") {
			return unknown()
		}
		t := ModelType{Kind: TypeReference, Name: compact(name.Text), Arguments: len(args)}
		if t.Name == "Promise" || t.Name == "Array" {
			t.Types = list(TypeReference, args).Types
		}
		tagModel("T-reference", t)
		return t
	case "union_type", "intersection_type":
		var flat []*parse.Node
		var collect func(*parse.Node)
		collect = func(x *parse.Node) {
			for _, c := range modelChildren(x) {
				if c.Kind == n.Kind {
					collect(c)
				} else {
					flat = append(flat, c)
				}
			}
		}
		collect(n)
		if len(flat) == 1 {
			// A leading `|` or `&` with a single member is that member.
			return reduceType(flat[0], depth)
		}
		kind, row := TypeUnion, "T-union"
		if n.Kind == "intersection_type" {
			kind, row = TypeIntersection, "T-intersection"
		}
		t := list(kind, flat)
		tagModel(row, t)
		return t
	case "array_type":
		inner := modelChildren(n)
		if len(inner) != 1 {
			return unknown()
		}
		t := list(TypeArray, inner)
		tagModel("T-array", t)
		return t
	case "tuple_type":
		t := list(TypeTuple, modelChildren(n))
		tagModel("T-tuple", t)
		return t
	case "function_type":
		t := ModelType{Kind: TypeFunction}
		tagModel("T-function", t)
		return t
	}
	return unknown()
}

// withoutTypeORMForms turns the forms only the TypeORM rows keep (async
// arrows and functions, await expressions, try statements; T018g6
// Revision 3 addendum 2) back into unknown entries with the same span, so
// the payload of a Drizzle or Sequelize file is what it was before those
// rows existed and no other reader sees them.
func (s *ModelSyntax) withoutTypeORMForms() {
	for i := range s.Declarations {
		legacyExpr(&s.Declarations[i].Value)
	}
	if s.Default != nil {
		legacyExpr(&s.Default.Value)
	}
	legacyStatements(s.Statements)
}

func legacyExpr(e *ModelExpr) {
	if e == nil {
		return
	}
	if e.Async || (e.Kind == ModelUnary && e.Value == "await") {
		*e = ModelExpr{Kind: ModelUnknown, Span: e.Span}
		return
	}
	for _, p := range []*ModelExpr{e.Callee, e.Object, e.Tag, e.Body, e.Left, e.Right, e.Index, e.Test, e.Then, e.Else} {
		legacyExpr(p)
	}
	for _, list := range [][]ModelExpr{e.Args, e.Substitutions, e.Elements} {
		for i := range list {
			legacyExpr(&list[i])
		}
	}
	for i := range e.Entries {
		legacyExpr(&e.Entries[i].Value)
	}
	legacyStatements(e.Statements)
}

func legacyStatements(list []ModelStatement) {
	for i := range list {
		st := &list[i]
		if st.Kind == StatementTry {
			*st = ModelStatement{Kind: StatementUnknown, Span: st.Span}
			continue
		}
		legacyExpr(st.Extends)
		legacyExpr(st.Value)
		for j := range st.DecoratorList {
			legacyExpr(&st.DecoratorList[j])
		}
		for j := range st.Members {
			for k := range st.Members[j].Decorators {
				legacyExpr(&st.Members[j].Decorators[k])
			}
		}
		for j := range st.EnumMembers {
			legacyExpr(st.EnumMembers[j].Value)
		}
		legacyStatements(st.Body)
		legacyStatements(st.Else)
		legacyStatements(st.Statics)
	}
}

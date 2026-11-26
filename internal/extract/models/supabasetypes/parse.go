package supabasetypes

import (
	"fmt"
	"strconv"
	"strings"
)

// typesFile is a generated types file as the parser read it.
type typesFile struct {
	// header is "type" for `export type Database = {` and "interface" for
	// `export interface Database {` (postgres-meta up to 0.77).
	header string
	// internal is true when the Database type starts with the
	// __InternalSupabase member (postgres-meta from 0.91).
	internal bool
	schemas  []schemaDecl
	// helpers are the helper declarations after Database, by name.
	helpers []string
	// line and endLine span the Database declaration.
	line, endLine int
}

// schemaDecl is one schema of the Database type.
type schemaDecl struct {
	name                    string
	line                    int
	tables, views           []relation
	tablesNever, viewsNever bool
}

// relation is one entry of a schema's Tables or Views.
type relation struct {
	name          string
	line, endLine int
	row           []field
	insert        []field
	update        []field
	rels          []relationship
}

// field is one property of a Row, Insert or Update type.
type field struct {
	name     string
	optional bool
	// typ is the TypeScript type as written (whitespace and quotes
	// normalised: members joined by " | ", strings in double quotes).
	typ string
	// nullable is true when the type's top-level union has a null member,
	// false when it has none, nil for unknown and any (they include null,
	// and the generator writes no `| null` beside them from 0.88).
	nullable *bool
	line     int
}

// relationship is one entry of a Relationships tuple.
type relationship struct {
	name       string
	columns    []string
	oneToOne   *bool
	refRel     string
	refColumns []string
	line       int
}

// helperNames are the declarations the generator writes after Database
// (postgres-meta 0.70 to 0.97): the schema aliases and the helper types.
var helperNames = map[string]bool{
	"PublicSchema": true, "DefaultSchema": true, "DatabaseWithoutInternals": true,
	"Tables": true, "TablesInsert": true, "TablesUpdate": true, "Enums": true, "CompositeTypes": true,
	"Constants": true,
}

// jsonForms are the Json declarations the generator writes (0.70 on),
// rendered as the parser renders types.
var jsonForms = map[string]bool{
	"string | number | boolean | null | { [key: string]: Json | undefined } | Json[]": true,
}

// parser reads one token stream.
type parser struct {
	toks  []token
	pos   int
	forms map[string]bool
}

// shapeError is a deviation from the generated form.
type shapeError struct {
	line int
	msg  string
}

func (e *shapeError) Error() string { return fmt.Sprintf("line %d: %s", e.line, e.msg) }

func (p *parser) peek() token { return p.toks[p.pos] }

func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) fail(format string, args ...any) {
	panic(&shapeError{line: p.peek().line, msg: fmt.Sprintf(format, args...)})
}

func (p *parser) is(kind tokenKind, text string) bool {
	t := p.peek()
	return t.kind == kind && t.text == text
}

func (p *parser) punct(text string) bool { return p.is(tokPunct, text) }

func (p *parser) expect(kind tokenKind, text string) token {
	if !p.is(kind, text) {
		p.fail("expected %q, found %q", text, p.peek().text)
	}
	return p.next()
}

func (p *parser) expectPunct(text string) token { return p.expect(tokPunct, text) }

// sep skips one optional member separator (`;` or `,`).
func (p *parser) sep() {
	if p.punct(";") || p.punct(",") {
		p.next()
	}
}

// key reads a property key: an identifier or a string.
func (p *parser) key() token {
	t := p.peek()
	if t.kind != tokIdent && t.kind != tokString {
		p.fail("expected a property name, found %q", t.text)
	}
	return p.next()
}

// member reads `<name>:` for a fixed member name.
func (p *parser) member(name string) token {
	t := p.key()
	if t.text != name {
		panic(&shapeError{line: t.line, msg: fmt.Sprintf("expected member %s, found %q", name, t.text)})
	}
	p.expectPunct(":")
	return t
}

// parseFile reads a whole generated types file. A deviation from the
// generated form is returned as an error naming its line.
func parseFile(src []byte) (f *typesFile, forms map[string]bool, err error) {
	toks, err := lex(src)
	if err != nil {
		return nil, nil, err
	}
	p := &parser{toks: toks, forms: map[string]bool{}}
	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(*shapeError)
			if !ok {
				panic(r)
			}
			f, forms, err = nil, nil, se
		}
	}()
	f = p.file()
	return f, p.forms, nil
}

func (p *parser) file() *typesFile {
	f := &typesFile{}
	// export type Json = <the generator's union>
	p.expect(tokIdent, "export")
	p.expect(tokIdent, "type")
	p.expect(tokIdent, "Json")
	p.expectPunct("=")
	line := p.peek().line
	if j, _ := p.typ(); !jsonForms[j] {
		panic(&shapeError{line: line, msg: "Json is not the generator's union: " + j})
	}
	p.sep()
	// export type Database = { ... } or export interface Database { ... }
	f.line = p.expect(tokIdent, "export").line
	switch {
	case p.is(tokIdent, "type"):
		p.next()
		p.expect(tokIdent, "Database")
		p.expectPunct("=")
		f.header = "type"
	case p.is(tokIdent, "interface"):
		p.next()
		p.expect(tokIdent, "Database")
		f.header = "interface"
	default:
		p.fail("expected the Database declaration, found %q", p.peek().text)
	}
	p.forms["export "+f.header+" Database"] = true
	p.expectPunct("{")
	if p.is(tokIdent, "__InternalSupabase") {
		p.next()
		p.expectPunct(":")
		p.expectPunct("{")
		p.member("PostgrestVersion")
		if p.peek().kind != tokString {
			p.fail("PostgrestVersion is not a string")
		}
		p.next()
		p.sep()
		p.expectPunct("}")
		p.sep()
		f.internal = true
		p.forms["__InternalSupabase"] = true
	}
	seen := map[string]bool{}
	for !p.punct("}") {
		s := p.schema()
		if seen[s.name] {
			p.fail("schema %s twice", s.name)
		}
		seen[s.name] = true
		f.schemas = append(f.schemas, s)
	}
	f.endLine = p.expectPunct("}").line
	p.sep()
	for p.peek().kind != tokEOF {
		f.helpers = append(f.helpers, p.helper())
	}
	return f
}

// helper reads one helper declaration after Database and skips its body:
// the helpers state no fact, so only their names are checked.
func (p *parser) helper() string {
	if p.is(tokIdent, "export") {
		p.next()
	}
	var name string
	switch {
	case p.is(tokIdent, "type"):
		p.next()
		name = p.key().text
	case p.is(tokIdent, "const"):
		p.next()
		name = p.key().text
		if name != "Constants" {
			p.fail("unexpected constant %s", name)
		}
	default:
		p.fail("expected a helper declaration, found %q", p.peek().text)
	}
	if !helperNames[name] {
		p.fail("%s is not a helper the generator writes", name)
	}
	depth := 0
	for {
		t := p.peek()
		if t.kind == tokEOF {
			if depth != 0 {
				p.fail("unbalanced helper %s", name)
			}
			return name
		}
		if depth == 0 && t.kind == tokIdent && (t.text == "export" || t.text == "type") {
			return name
		}
		if t.kind == tokPunct {
			switch t.text {
			case "{", "[", "(", "<":
				depth++
			case "}", "]", ")", ">":
				depth--
				if depth < 0 {
					p.fail("unbalanced helper %s", name)
				}
			}
		}
		p.next()
	}
}

// schema reads `<schema>: { Tables Views Functions Enums CompositeTypes }`.
func (p *parser) schema() schemaDecl {
	k := p.key()
	s := schemaDecl{name: k.text, line: k.line}
	p.expectPunct(":")
	p.expectPunct("{")
	p.member("Tables")
	s.tables, s.tablesNever = p.relations(true)
	p.sep()
	p.member("Views")
	s.views, s.viewsNever = p.relations(false)
	p.sep()
	for _, name := range []string{"Functions", "Enums", "CompositeTypes"} {
		p.member(name)
		p.skipBlock()
		p.sep()
	}
	p.expectPunct("}")
	p.sep()
	return s
}

// skipBlock skips a balanced `{ ... }`: Functions, Enums and
// CompositeTypes state no table fact.
func (p *parser) skipBlock() {
	p.expectPunct("{")
	depth := 1
	for depth > 0 {
		t := p.next()
		switch {
		case t.kind == tokEOF:
			p.fail("unbalanced block")
		case t.kind == tokPunct && (t.text == "{" || t.text == "[" || t.text == "("):
			depth++
		case t.kind == tokPunct && (t.text == "}" || t.text == "]" || t.text == ")"):
			depth--
		}
	}
}

// never reads `[_ in never]: never`, the generator's empty map.
func (p *parser) never() bool {
	if !p.punct("[") || p.toks[p.pos+1].text != "_" {
		return false
	}
	p.expectPunct("[")
	p.expect(tokIdent, "_")
	p.expect(tokIdent, "in")
	p.expect(tokIdent, "never")
	p.expectPunct("]")
	p.expectPunct(":")
	p.expect(tokIdent, "never")
	p.sep()
	p.forms["[_ in never]: never"] = true
	return true
}

// relations reads the body of Tables (tables true) or Views.
func (p *parser) relations(tables bool) ([]relation, bool) {
	p.expectPunct("{")
	if p.never() {
		p.expectPunct("}")
		return nil, true
	}
	var out []relation
	seen := map[string]bool{}
	for !p.punct("}") {
		r := p.relation(tables)
		if seen[r.name] {
			p.fail("%s twice", r.name)
		}
		seen[r.name] = true
		out = append(out, r)
	}
	p.expectPunct("}")
	if len(out) == 0 {
		p.fail("an empty map the generator writes as [_ in never]: never")
	}
	return out, false
}

// relation reads one table or view entry.
func (p *parser) relation(table bool) relation {
	k := p.key()
	r := relation{name: k.text, line: k.line}
	p.expectPunct(":")
	p.expectPunct("{")
	p.member("Row")
	r.row = p.fields()
	p.sep()
	if table || p.is(tokIdent, "Insert") {
		p.member("Insert")
		r.insert = p.fields()
		p.sep()
		p.member("Update")
		r.update = p.fields()
		p.sep()
	}
	p.member("Relationships")
	r.rels = p.relationships()
	p.sep()
	r.endLine = p.expectPunct("}").line
	p.sep()
	p.checkRelation(&r, table)
	return r
}

// checkRelation checks the generator's invariants between Row, Insert and
// Update: Row properties are never optional; Insert and Update name the
// same columns in the same order and every Update property is optional;
// Row starts with the columns in that order and may end with computed
// fields (functions taking the row, postgres-meta's
// `fn.argument_types === table.name`).
func (p *parser) checkRelation(r *relation, table bool) {
	fail := func(msg string) { panic(&shapeError{line: r.line, msg: r.name + ": " + msg}) }
	for _, f := range r.row {
		if f.optional {
			fail("Row property " + f.name + " is optional")
		}
	}
	if !table && r.insert == nil {
		return
	}
	if len(r.insert) != len(r.update) {
		fail("Insert and Update differ")
	}
	for i := range r.insert {
		if r.insert[i].name != r.update[i].name || !r.update[i].optional {
			fail("Insert and Update differ at " + r.update[i].name)
		}
	}
	if !table {
		return
	}
	if len(r.row) < len(r.insert) {
		fail("Row lacks Insert columns")
	}
	for i := range r.insert {
		if r.row[i].name != r.insert[i].name {
			fail("Row and Insert differ at " + r.row[i].name)
		}
	}
	if len(r.row) > len(r.insert) {
		p.forms["computed field"] = true
	}
}

// fields reads `{ name[?]: Type ... }`; names are unique.
func (p *parser) fields() []field {
	p.expectPunct("{")
	var out []field
	seen := map[string]bool{}
	for !p.punct("}") {
		k := p.key()
		f := field{name: k.text, line: k.line}
		if p.punct("?") {
			p.next()
			f.optional = true
		}
		p.expectPunct(":")
		f.typ, f.nullable = p.typ()
		switch {
		case f.nullable == nil:
			p.forms["unknown"] = true
		case *f.nullable:
			p.forms["| null"] = true
		}
		p.sep()
		if seen[f.name] {
			panic(&shapeError{line: k.line, msg: "property " + f.name + " twice"})
		}
		seen[f.name] = true
		out = append(out, f)
	}
	p.expectPunct("}")
	return out
}

// relationships reads the Relationships tuple.
func (p *parser) relationships() []relationship {
	p.expectPunct("[")
	var out []relationship
	for !p.punct("]") {
		r := relationship{line: p.expectPunct("{").line}
		p.member("foreignKeyName")
		r.name = p.str()
		p.sep()
		p.member("columns")
		r.columns = p.strings()
		p.sep()
		if p.is(tokIdent, "isOneToOne") {
			p.next()
			p.expectPunct(":")
			v := p.next()
			if v.kind != tokIdent || v.text != "true" && v.text != "false" {
				panic(&shapeError{line: v.line, msg: "isOneToOne is not a boolean"})
			}
			b := v.text == "true"
			r.oneToOne = &b
			p.sep()
		}
		p.member("referencedRelation")
		r.refRel = p.str()
		p.sep()
		p.member("referencedColumns")
		r.refColumns = p.strings()
		p.sep()
		p.expectPunct("}")
		p.sep()
		out = append(out, r)
		p.forms["Relationships"] = true
	}
	p.expectPunct("]")
	return out
}

func (p *parser) str() string {
	t := p.peek()
	if t.kind != tokString {
		p.fail("expected a string, found %q", t.text)
	}
	return p.next().text
}

func (p *parser) strings() []string {
	p.expectPunct("[")
	out := []string{}
	for !p.punct("]") {
		out = append(out, p.str())
		p.sep()
	}
	p.expectPunct("]")
	return out
}

// typeIdents are the type names the generator writes in a Row, Insert or
// Update property (pgTypeToTsType and the Json alias).
var typeIdents = map[string]bool{
	"string": true, "number": true, "boolean": true, "null": true, "undefined": true,
	"unknown": true, "never": true, "any": true, "Json": true, "true": true, "false": true,
	"PropertyKey": true,
}

// typ reads a type: a union of primaries, an optional leading `|`.
func (p *parser) typ() (string, *bool) {
	if p.punct("|") {
		p.next()
	}
	var members []string
	hasNull := false
	for {
		m := p.primary()
		if m == "null" {
			hasNull = true
		}
		members = append(members, m)
		if !p.punct("|") {
			break
		}
		p.next()
	}
	text := strings.Join(members, " | ")
	var nullable *bool
	switch {
	case hasNull:
		t := true
		nullable = &t
	case len(members) == 1 && (members[0] == "unknown" || members[0] == "any"):
	default:
		f := false
		nullable = &f
	}
	return text, nullable
}

// primary reads one union member: an atom followed by `[]` suffixes.
func (p *parser) primary() string {
	s := p.atom()
	for p.punct("[") && p.toks[p.pos+1].text == "]" {
		p.next()
		p.next()
		s += "[]"
	}
	return s
}

func (p *parser) atom() string {
	t := p.peek()
	switch {
	case t.kind == tokString:
		p.next()
		return strconv.Quote(t.text)
	case t.kind == tokNumber:
		p.next()
		return t.text
	case t.kind == tokPunct && t.text == "-" && p.toks[p.pos+1].kind == tokNumber:
		p.next()
		return "-" + p.next().text
	case t.kind == tokPunct && t.text == "(":
		p.next()
		inner, _ := p.typ()
		p.expectPunct(")")
		return "(" + inner + ")"
	case t.kind == tokPunct && t.text == "{":
		return p.objectType()
	case t.kind == tokIdent && (t.text == "Database" || t.text == "DatabaseWithoutInternals"):
		p.next()
		s := t.text
		n := 0
		for p.punct("[") && p.toks[p.pos+1].kind == tokString {
			p.next()
			s += "[" + strconv.Quote(p.next().text) + "]"
			p.expectPunct("]")
			n++
		}
		if n == 0 {
			p.fail("%s without an indexed access", t.text)
		}
		return s
	case t.kind == tokIdent && t.text == "Record":
		p.next()
		p.expectPunct("<")
		a, _ := p.typ()
		p.expectPunct(",")
		b, _ := p.typ()
		p.expectPunct(">")
		return "Record<" + a + ", " + b + ">"
	case t.kind == tokIdent && typeIdents[t.text]:
		p.next()
		return t.text
	}
	p.fail("type %q is not a form the generator writes", t.text)
	return ""
}

// objectType reads an object type literal (a composite or a record return
// type), with the Json index signature `[key: string]: T`.
func (p *parser) objectType() string {
	p.expectPunct("{")
	var parts []string
	for !p.punct("}") {
		if p.punct("[") {
			p.next()
			k := p.key().text
			p.expectPunct(":")
			kt, _ := p.typ()
			p.expectPunct("]")
			p.expectPunct(":")
			v, _ := p.typ()
			parts = append(parts, "["+k+": "+kt+"]: "+v)
			p.sep()
			continue
		}
		k := p.key().text
		opt := ""
		if p.punct("?") {
			p.next()
			opt = "?"
		}
		p.expectPunct(":")
		v, _ := p.typ()
		parts = append(parts, k+opt+": "+v)
		p.sep()
	}
	p.expectPunct("}")
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, "; ") + " }"
}

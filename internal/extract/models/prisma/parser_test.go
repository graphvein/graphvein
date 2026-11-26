package prisma

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *File {
	t.Helper()
	f := Parse("schema.prisma", []byte(src))
	for _, e := range f.Errors {
		t.Errorf("unexpected parse error %d:%d: %s", e.Line, e.Col, e.Message)
	}
	return f
}

func block(t *testing.T, f *File, keyword, name string) *Block {
	t.Helper()
	for _, b := range f.Blocks {
		if b.Keyword == keyword && b.Name == name {
			return b
		}
	}
	t.Fatalf("no %s %s in %+v", keyword, name, f.Blocks)
	return nil
}

func attr(t *testing.T, attrs []*Attribute, name string) *Attribute {
	t.Helper()
	for _, a := range attrs {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("no attribute %s", name)
	return nil
}

// TestParseBlocks reads every block kind with comments, doc comments, a
// trailing comment, the text Prisma ignores after `{` when it is a
// comment, positions and the CRLF and CR newlines.
func TestParseBlocks(t *testing.T) {
	src := strings.Join([]string{
		"// leading comment",
		"generator client {",
		`  provider        = "prisma-client"`,
		`  previewFeatures = ["views"] // trailing`,
		"}",
		"",
		"datasource db { // comment after the brace",
		`  provider     = "postgresql"`,
		`  relationMode = "prisma"`,
		`  schemas      = ["public", "auth"]`,
		"  empty        =",
		"}",
		"",
		"/// A user.",
		"/* block",
		"   comment */",
		"model User {",
		"  /// The key.",
		`  id    Int     @id @default(autoincrement())`,
		`  email String? @unique(map: "u_email") // trailing`,
		"  tags  String[]",
		"  role  Role    @default(MEMBER)",
		"  posts Post[]",
		"",
		`  @@map("users")`,
		`  @@schema("auth") /* trailing block comment */`,
		"}",
		"",
		"enum Role {",
		`  MEMBER @map("member")`,
		"  ADMIN",
		`  @@map("role_kind")`,
		"}",
		"",
		"view V {",
		"  id Int",
		"}",
		"",
		"type Address {",
		"  street String",
		"}",
	}, "\r\n")
	src = strings.Replace(src, "type Address {\r\n", "type Address {\r", 1)
	f := mustParse(t, src+"\n")
	if len(f.Blocks) != 6 {
		t.Fatalf("blocks %+v", f.Blocks)
	}
	gen := block(t, f, KeywordGenerator, "client")
	if gen.StartLine != 2 || gen.EndLine != 5 || len(gen.Properties) != 2 {
		t.Fatalf("generator %+v", gen)
	}
	if pf := gen.Properties[1].Value; pf.Kind != ExprArray || len(pf.Items) != 1 || pf.Items[0].Value != "views" || pf.Raw != `["views"]` {
		t.Errorf("previewFeatures %+v", pf)
	}
	ds := block(t, f, KeywordDatasource, "db")
	if len(ds.Properties) != 4 || ds.Properties[0].Value.Value != "postgresql" || ds.Properties[3].Key != "empty" || ds.Properties[3].Value != nil {
		t.Errorf("datasource %+v", ds.Properties)
	}
	user := block(t, f, KeywordModel, "User")
	if user.StartLine != 17 || user.EndLine != 27 || len(user.Fields) != 5 || len(user.Attributes) != 2 {
		t.Fatalf("model %+v", user)
	}
	id := user.Fields[0]
	if id.Name != "id" || id.Line != 19 || id.Col != 3 || id.Type.Name != "Int" || id.Type.Optional || id.Type.List || len(id.Attributes) != 2 {
		t.Errorf("id %+v %+v", id, id.Type)
	}
	def := attr(t, id.Attributes, "default")
	if def.Args == nil || len(def.Args.Args) != 1 || def.Args.Args[0].Value.Kind != ExprCall || def.Args.Args[0].Value.Value != "autoincrement" || len(def.Args.Args[0].Value.Args.Args) != 0 || def.Raw != "@default(autoincrement())" {
		t.Errorf("default %+v", def)
	}
	if a := attr(t, id.Attributes, "id"); a.Args != nil || a.Block {
		t.Errorf("@id %+v", a)
	}
	email := user.Fields[1]
	if !email.Type.Optional || attr(t, email.Attributes, "unique").Args.Args[0].Name != "map" || attr(t, email.Attributes, "unique").Args.Args[0].Value.Value != "u_email" {
		t.Errorf("email %+v", email)
	}
	if tags := user.Fields[2]; !tags.Type.List || tags.Type.Optional {
		t.Errorf("tags %+v", tags.Type)
	}
	if v := attr(t, user.Fields[3].Attributes, "default").Args.Args[0].Value; v.Kind != ExprConstant || v.Value != "MEMBER" {
		t.Errorf("enum default %+v", v)
	}
	if m := attr(t, user.Attributes, "map"); !m.Block || m.Line != 25 || m.Args.Args[0].Value.Value != "users" {
		t.Errorf("@@map %+v", m)
	}
	role := block(t, f, KeywordEnum, "Role")
	if len(role.Values) != 2 || role.Values[0].Name != "MEMBER" || attr(t, role.Values[0].Attributes, "map").Args.Args[0].Value.Value != "member" || len(role.Attributes) != 1 {
		t.Errorf("enum %+v", role)
	}
	if v := block(t, f, KeywordView, "V"); len(v.Fields) != 1 {
		t.Errorf("view %+v", v)
	}
	if ty := block(t, f, KeywordType, "Address"); len(ty.Fields) != 1 || ty.Fields[0].Name != "street" {
		t.Errorf("type %+v", ty)
	}
}

// TestParseArguments reads nested arguments: named and positional
// arguments, arrays of calls with named arguments, objects, numbers,
// native types with arguments, `@x()` and a trailing comma.
func TestParseArguments(t *testing.T) {
	f := mustParse(t, `model M {
  a Decimal @db.Decimal(10, 2) @default(-1.5)
  b String  @id() @default(dbgenerated("gen_random_uuid()"))
  c Json    @default("{\"k\": [1]}")
  d Int[]   @default([1, 2, 3])
  e Unsupported("tsvector")?
  f Parent  @relation("named", fields: [a, b], references: [x, y], onDelete: Cascade, map: "m_fk",)
  g Int     @something(key: { nested: [true, false], n: 1 })

  @@index([a(sort: Desc), b(ops: raw("text_pattern_ops"))], type: Gin, map: "m_idx")
  @@id(fields: [a, b], name: "pk")
}
`)
	m := block(t, f, KeywordModel, "M")
	a := m.Fields[0]
	db := attr(t, a.Attributes, "db.Decimal")
	if len(db.Args.Args) != 2 || db.Args.Args[0].Value.Kind != ExprNumber || db.Args.Args[1].Value.Value != "2" {
		t.Errorf("@db.Decimal %+v", db.Args)
	}
	if v := attr(t, a.Attributes, "default").Args.Args[0].Value; v.Kind != ExprNumber || v.Value != "-1.5" {
		t.Errorf("negative number %+v", v)
	}
	b := m.Fields[1]
	if id := attr(t, b.Attributes, "id"); id.Args == nil || len(id.Args.Args) != 0 {
		t.Errorf("@id() %+v", id)
	}
	if v := attr(t, b.Attributes, "default").Args.Args[0].Value; v.Kind != ExprCall || v.Value != "dbgenerated" || v.Args.Args[0].Value.Value != "gen_random_uuid()" || v.Raw != `dbgenerated("gen_random_uuid()")` {
		t.Errorf("dbgenerated %+v", v)
	}
	if v := attr(t, m.Fields[2].Attributes, "default").Args.Args[0].Value; v.Value != `{"k": [1]}` || v.Raw != `"{\"k\": [1]}"` {
		t.Errorf("escaped string %q raw %q", v.Value, v.Raw)
	}
	if v := attr(t, m.Fields[3].Attributes, "default").Args.Args[0].Value; v.Kind != ExprArray || len(v.Items) != 3 {
		t.Errorf("list default %+v", v)
	}
	e := m.Fields[4].Type
	if e.Name != "Unsupported" || e.Unsupported.Value != "tsvector" || !e.Optional {
		t.Errorf("Unsupported %+v", e)
	}
	rel := attr(t, m.Fields[5].Attributes, "relation")
	if !rel.Args.TrailingComma || len(rel.Args.Args) != 5 || rel.Args.Args[0].Name != "" || rel.Args.Args[0].Value.Value != "named" ||
		rel.Args.Args[1].Name != "fields" || len(rel.Args.Args[1].Value.Items) != 2 || rel.Args.Args[3].Value.Value != "Cascade" || rel.Args.Args[4].Name != "map" {
		t.Errorf("@relation %+v", rel.Args)
	}
	obj := attr(t, m.Fields[6].Attributes, "something").Args.Args[0].Value
	if obj.Kind != ExprObject || len(obj.Members) != 2 || obj.Members[0].Name != "nested" || len(obj.Members[0].Value.Items) != 2 {
		t.Errorf("object %+v", obj)
	}
	idx := attr(t, m.Attributes, "index")
	cols := idx.Args.Args[0].Value
	if cols.Kind != ExprArray || cols.Items[0].Kind != ExprCall || cols.Items[0].Value != "a" || cols.Items[0].Args.Args[0].Name != "sort" ||
		cols.Items[1].Args.Args[0].Value.Kind != ExprCall || cols.Items[1].Args.Args[0].Value.Args.Args[0].Value.Value != "text_pattern_ops" {
		t.Errorf("@@index fields %+v", cols)
	}
	if idx.Args.Args[1].Name != "type" || idx.Args.Args[1].Value.Value != "Gin" {
		t.Errorf("@@index type %+v", idx.Args.Args[1])
	}
}

// TestParseStrings checks Prisma's JSON string escapes, including a
// surrogate pair, and the escapes Prisma rejects.
func TestParseStrings(t *testing.T) {
	for _, tc := range []struct {
		lit, want, bad string
	}{
		{`"plain"`, "plain", ""},
		{`"q\"b\\s\/n\nt\tr\rb\bf\f"`, "q\"b\\s/n\nt\tr\rb\bf\f", ""},
		{`"\u00e9\u20AC"`, "é€", ""},
		{`"\ud83d\ude00"`, "\U0001F600", ""},
		{`"üñí"`, "üñí", ""},
		{`"\x"`, "", "unknown escape"},
		{`"C:\path"`, "", "unknown escape"},
		{`"\u12"`, "", "invalid unicode"},
		{`"\ud83d"`, "", "invalid unicode"},
		{`"\ud83d\u0041"`, "", "invalid unicode"},
		{`"open`, "", "unterminated"},
		{"\"tab\there\"", "", "unterminated"},
	} {
		f := Parse("s.prisma", []byte("datasource db {\n  provider = "+tc.lit+"\n}\n"))
		if tc.bad != "" {
			if len(f.Errors) == 0 || !strings.Contains(f.Errors[0].Message, tc.bad) {
				t.Errorf("%s: errors %+v, want %q", tc.lit, f.Errors, tc.bad)
			}
			continue
		}
		if len(f.Errors) != 0 {
			t.Errorf("%s: errors %+v", tc.lit, f.Errors)
			continue
		}
		if got := f.Blocks[0].Properties[0].Value.Value; got != tc.want {
			t.Errorf("%s: %q, want %q", tc.lit, got, tc.want)
		}
	}
}

// TestParseLegacyAndEdgeForms keeps the forms Prisma's grammar accepts but
// its validation refuses, so the front end can refuse them by row: a
// legacy colon, `T!`, `[T]`, `T[]?`, a field without a type, an empty
// named argument; and `@x ()` with spaces, which Prisma accepts.
func TestParseLegacyAndEdgeForms(t *testing.T) {
	f := mustParse(t, `model M {
  a: Int
  b Int!
  c [Int]
  d Int[]?
  e
  f Int @default(value:)
  g Int @ map ("g") @db.VarChar (10)
  h-x Int
}
`)
	m := block(t, f, KeywordModel, "M")
	if len(m.Fields) != 8 {
		t.Fatalf("fields %+v", m.Fields)
	}
	if !m.Fields[0].LegacyColon || m.Fields[1].Type.Legacy != "!" || m.Fields[2].Type.Legacy != "[]" || !m.Fields[2].Type.List {
		t.Errorf("legacy forms %+v %+v %+v", m.Fields[0], m.Fields[1].Type, m.Fields[2].Type)
	}
	if d := m.Fields[3].Type; !d.List || !d.Optional {
		t.Errorf("T[]? %+v", d)
	}
	if m.Fields[4].Type != nil {
		t.Errorf("field without type %+v", m.Fields[4].Type)
	}
	if arg := m.Fields[5].Attributes[0].Args.Args[0]; arg.Name != "value" || arg.Value != nil {
		t.Errorf("empty argument %+v", arg)
	}
	if g := m.Fields[6]; attr(t, g.Attributes, "map").Args.Args[0].Value.Value != "g" || attr(t, g.Attributes, "db.VarChar").Args.Args[0].Value.Value != "10" {
		t.Errorf("spaced attributes %+v", g.Attributes)
	}
	if m.Fields[7].Name != "h-x" {
		t.Errorf("identifier with a dash %+v", m.Fields[7])
	}
}

// TestParseErrors checks malformed input: each error is reported with its
// position, the parser goes on at the next line, and the blocks around
// the bad line are still read.
func TestParseErrors(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		// line of the first error, and a fragment of its message
		line int
		msg  string
	}{
		{"bad top-level line", "model A {\n  id Int @id\n}\nfoo bar\nmodel B {\n  id Int @id\n}\n", 4, "does not start with a Prisma schema keyword"},
		{"arbitrary block", "foo {\n  id Int\n}\nmodel B {\n  id Int @id\n}\n", 1, "block foo"},
		{"type alias", "type Id = String @id\nmodel B {\n  id Int @id\n}\n", 1, "type alias"},
		{"bad field line", "model A {\n  id Int @id\n  ; bad\n}\nmodel B {\n  id Int @id\n}\n", 3, "not a valid field"},
		{"argument across lines", "model A {\n  id Int @default(\n  1)\n}\nmodel B {\n  id Int @id\n}\n", 2, "expected an expression"},
		{"unclosed argument list", "model A {\n  id Int @default(1\n}\nmodel B {\n  id Int @id\n}\n", 2, "expected `,` or `)`"},
		{"text after the brace", "model A { id Int @id\n}\nmodel B {\n  id Int @id\n}\n", 1, "text after `{`"},
		{"close brace on a field line", "model A {\n  id Int @id }\n}\nmodel B {\n  id Int @id\n}\n", 2, "unexpected"},
		{"unclosed block", "model B {\n  id Int @id\n}\nmodel A {\n  id Int @id\n", 6, "not closed"},
		{"block attribute on a field line", "model A {\n  id Int @@id\n}\nmodel B {\n  id Int @id\n}\n", 2, "unexpected"},
		{"bad number", "model A {\n  id Int @default(1abc)\n}\nmodel B {\n  id Int @id\n}\n", 2, "invalid number"},
		{"array trailing comma", "model A {\n  id Int @default([1,])\n}\nmodel B {\n  id Int @id\n}\n", 2, "expected an expression"},
		{"bad character", "model A {\n  id Int @id\n}\n#\nmodel B {\n  id Int @id\n}\n", 4, "does not start"},
		{"identifier with underscore start", "model A {\n  _id Int @id\n}\nmodel B {\n  id Int @id\n}\n", 2, "not a valid field"},
		{"unterminated block comment", "model B {\n  id Int @id\n}\n/* open\nmodel A {\n", 4, "does not start"},
		{"invalid utf-8", "model B {\n  id Int @id\n}\n\xff\n", 1, "not valid UTF-8"},
		{"field type path", "model A {\n  id db.Int\n}\nmodel B {\n  id Int @id\n}\n", 2, "invalid field type"},
		{"comment inside arguments", "model A {\n  id Int @default(/* c */ 1)\n}\nmodel B {\n  id Int @id\n}\n", 2, "expected an expression"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Parse("bad.prisma", []byte(tc.src))
			if len(f.Errors) == 0 {
				t.Fatalf("no error; blocks %+v", f.Blocks)
			}
			if e := f.Errors[0]; e.Line != tc.line || !strings.Contains(e.Message, tc.msg) {
				t.Errorf("first error %d:%d %q, want line %d %q", e.Line, e.Col, e.Message, tc.line, tc.msg)
			}
			if tc.name == "invalid utf-8" {
				return
			}
			// The parser goes on: model B is read with its field.
			for _, b := range f.Blocks {
				if b.Keyword == KeywordModel && b.Name == "B" {
					if len(b.Fields) != 1 || b.Fields[0].Name != "id" {
						t.Errorf("model B %+v", b.Fields)
					}
					return
				}
			}
			t.Errorf("model B was not read after the error: %+v", f.Blocks)
		})
	}
}

// TestParseFilesIndependent checks that a bad file does not affect another
// one: parsing is per file and keeps no state between files.
func TestParseFilesIndependent(t *testing.T) {
	bad := Parse("a.prisma", []byte("model A {\n  id Int @default(\n"))
	good := Parse("b.prisma", []byte("model B {\n  id Int @id\n}\n"))
	if len(bad.Errors) == 0 || len(good.Errors) != 0 || len(good.Blocks) != 1 || good.Path != "b.prisma" {
		t.Fatalf("bad %+v, good %+v", bad.Errors, good)
	}
}

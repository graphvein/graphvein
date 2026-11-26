package diesel

import (
	"reflect"
	"strings"
	"testing"
)

func readSrc(t *testing.T, src string) []Item {
	t.Helper()
	trees, err := LexTrees(src)
	if err != nil {
		t.Fatalf("LexTrees: %v", err)
	}
	return ReadItems(trees)
}

// oneTable reads a source holding one table item.
func oneTable(t *testing.T, src string) *Table {
	t.Helper()
	items := readSrc(t, src)
	if len(items) != 1 || items[0].Kind != ItemTable {
		t.Fatalf("items %+v, want one table", items)
	}
	return items[0].Table
}

// TestReadItemSpans checks the lines that decide provenance (Revision 2,
// A3): a table's and a column's first attribute line, and diesel_cli
// 2.4's rust_types enums as ItemEnum items.
func TestReadItemSpans(t *testing.T) {
	src := `diesel::table! {
    /// docs
    #[sql_name = "people"]
    persons (id) {
        id -> Int4,
        /// a doc line
        #[max_length = 10]
        name -> Varchar,
    }
}

pub mod rust_types {
    #[derive(Debug)]
    pub enum Mood {
        Sad,
    }
}
`
	items := readSrc(t, src)
	if len(items) != 2 || items[0].Kind != ItemTable || items[1].Kind != ItemModule {
		t.Fatalf("items %+v", items)
	}
	tb := items[0].Table
	if tb.FirstLine != 2 || tb.Line != 4 {
		t.Errorf("table lines %d..%d, want 2..4", tb.FirstLine, tb.Line)
	}
	if c := tb.Columns[0]; c.FirstLine != 5 || c.Line != 5 {
		t.Errorf("column id lines %d..%d, want 5..5", c.FirstLine, c.Line)
	}
	if c := tb.Columns[1]; c.FirstLine != 6 || c.Line != 8 {
		t.Errorf("column name lines %d..%d, want 6..8", c.FirstLine, c.Line)
	}
	e := items[1].Items
	if len(e) != 1 || e[0].Kind != ItemEnum || e[0].Name != "Mood" || e[0].Line != 13 || e[0].NameLine != 14 || e[0].Reason != "pub enum Mood" {
		t.Errorf("rust_types items %+v", e)
	}
}

func TestReadItemForms(t *testing.T) {
	src := `// @generated automatically by Diesel CLI.
#![allow(unused)]
//! crate docs

/// A module containing custom SQL type definitions
pub mod sql_types {
    #[derive(diesel::query_builder::QueryId, Clone, diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "ltree"))]
    pub struct Ltree;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(name = "mood", schema = "app"))]
    pub struct Mood;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(postgres_type(oid = 1, array_oid = 2))]
    pub struct ByOid;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(mysql_type(name = "Enum"))]
    pub struct UsersRoleEnum;

    #[derive(diesel::sql_types::SqlType)]
    #[diesel(sqlite_type(name = "Text"))]
    pub struct Custom;

    #[derive(diesel::sql_types::SqlType)]
    pub struct NoAttr;

    #[diesel(postgres_type(name = "x", flavour = "y"))]
    pub struct OddArg;

    #[diesel(postgres_type(name = "mood2"))]
    #[diesel(enum_type)]
    pub struct Mood2;
}

table! { users { id -> Int4, } }
diesel::table! { posts (id) { id -> Int4, } }
::diesel::table!( comments { id -> Int4 } );
diesel::joinable!(posts -> users (user_id));
joinable!(a::posts -> users (user_id));
diesel::allow_tables_to_appear_in_same_query!(users, posts,);
pub mod other_schema {
    diesel::table! { other_schema.things { id -> Int4 } }
}
use diesel::sql_types::*;
infer_schema!("dotenv:DATABASE_URL");
fn helper() {}
mod external;
`
	items := readSrc(t, src)
	var kinds []ItemKind
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	want := []ItemKind{ItemInner, ItemInner, ItemModule, ItemTable, ItemTable, ItemTable, ItemJoinable, ItemJoinable, ItemAllowTables, ItemModule, ItemUse, ItemOther, ItemOther, ItemOther}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	mod := items[2]
	if mod.Name != "sql_types" || len(mod.Items) != 8 || len(mod.Attrs) != 1 || !mod.Attrs[0].Doc {
		t.Fatalf("sql_types module %+v", mod)
	}
	structs := map[string]SQLType{}
	for _, it := range mod.Items {
		if it.Kind != ItemStruct || !reflect.DeepEqual(it.Module, []string{"sql_types"}) {
			t.Fatalf("module item %+v", it)
		}
		structs[it.Name] = *it.Struct
	}
	checks := map[string]SQLType{
		"Ltree":         {Backend: "postgres", Name: "ltree", Derives: []string{"diesel::query_builder::QueryId", "Clone", "diesel::sql_types::SqlType"}, Line: 7, NameLine: 9},
		"Mood":          {Backend: "postgres", Name: "mood", Schema: "app", Derives: []string{"diesel::sql_types::SqlType"}, Line: 11, NameLine: 13},
		"ByOid":         {Backend: "postgres", OID: true, Derives: []string{"diesel::sql_types::SqlType"}, Line: 15, NameLine: 17},
		"UsersRoleEnum": {Backend: "mysql", Name: "Enum", Derives: []string{"diesel::sql_types::SqlType"}, Line: 19, NameLine: 21},
		"Custom":        {Backend: "sqlite", Name: "Text", Derives: []string{"diesel::sql_types::SqlType"}, Line: 23, NameLine: 25},
		"NoAttr":        {Derives: []string{"diesel::sql_types::SqlType"}, Unread: "no #[diesel(<backend>_type(...))] attribute", Line: 27, NameLine: 28},
		"Mood2":         {Backend: "postgres", Name: "mood2", Enum: true, Line: 33, NameLine: 35},
	}
	for name, w := range checks {
		if !reflect.DeepEqual(structs[name], w) {
			t.Errorf("struct %s = %+v, want %+v", name, structs[name], w)
		}
	}
	if structs["OddArg"].Unread == "" {
		t.Errorf("OddArg: want unread for an unknown argument")
	}
	if items[3].Macro != "table" || items[4].Macro != "diesel::table" || items[5].Macro != "::diesel::table" {
		t.Errorf("macro paths %q %q %q", items[3].Macro, items[4].Macro, items[5].Macro)
	}
	if j := items[6].Joinable; j == nil || !reflect.DeepEqual(j.Child, []string{"posts"}) || !reflect.DeepEqual(j.Parent, []string{"users"}) || j.Column != "user_id" {
		t.Errorf("joinable %+v", items[6].Joinable)
	}
	if j := items[7].Joinable; j == nil || !reflect.DeepEqual(j.Child, []string{"a", "posts"}) {
		t.Errorf("path joinable %+v", items[7].Joinable)
	}
	if a := items[8].Allow; a == nil || !reflect.DeepEqual(a.Tables, []string{"users", "posts"}) {
		t.Errorf("allow %+v", items[8].Allow)
	}
	nested := items[9]
	if nested.Name != "other_schema" || len(nested.Items) != 1 || nested.Items[0].Table.Schema != "other_schema" || !reflect.DeepEqual(nested.Items[0].Module, []string{"other_schema"}) {
		t.Errorf("schema module %+v", nested)
	}
	if items[10].Use != "diesel::sql_types::*" {
		t.Errorf("use %q", items[10].Use)
	}
	if items[11].Reason != "macro infer_schema!" || items[12].Reason != "fn helper" || items[13].Reason != "mod external" {
		t.Errorf("other reasons %q %q %q", items[11].Reason, items[12].Reason, items[13].Reason)
	}
}

func TestReadTable(t *testing.T) {
	tb := oneTable(t, `diesel::table! {
    use diesel::sql_types::*;
    use super::sql_types::{Ltree, Mood as M};

    /// Representation of the table.
    #[sql_name = "r#Users"]
    app.users (id, org_id) {
        /// The id.
        id -> Int4,
        org_id -> Int8,
        #[sql_name = "type"]
        #[max_length = 0x20]
        type_ -> Nullable<Varchar>,
        r#match -> Array<Nullable<Text>>,
        path -> ::diesel::sql_types::Text,
        pair -> Pair<Int4, Text>,
        turbo -> Range::<Int4>,
    }
}`)
	if tb.Failure != nil || tb.Unread != "" {
		t.Fatalf("table failure %+v unread %q", tb.Failure, tb.Unread)
	}
	if !reflect.DeepEqual(tb.Uses, []string{"diesel::sql_types::*", "super::sql_types::{Ltree, Mood as M}"}) {
		t.Errorf("uses %q", tb.Uses)
	}
	if tb.RustName != "users" || tb.SQLName != "Users" || tb.Schema != "app" || !tb.HasKeys || !reflect.DeepEqual(tb.Keys, []string{"id", "org_id"}) || len(tb.OtherAttrs) != 0 {
		t.Errorf("table %+v", tb)
	}
	if tb.Line != 7 || tb.BodyLine != 7 || tb.EndLine != 18 {
		t.Errorf("lines %d %d %d", tb.Line, tb.BodyLine, tb.EndLine)
	}
	type col struct {
		rust, sql, typ string
		max            int
		raw            bool
	}
	want := []col{
		{"id", "id", "Int4", -1, false},
		{"org_id", "org_id", "Int8", -1, false},
		{"type_", "type", "Nullable<Varchar>", 32, false},
		{"match", "match", "Array<Nullable<Text>>", -1, true},
		{"path", "path", "::diesel::sql_types::Text", -1, false},
		{"pair", "pair", "Pair<Int4, Text>", -1, false},
		{"turbo", "turbo", "Range<Int4>", -1, false},
	}
	if len(tb.Columns) != len(want) {
		t.Fatalf("%d columns, want %d", len(tb.Columns), len(want))
	}
	for i, w := range want {
		c := tb.Columns[i]
		max := -1
		if c.MaxLength != nil {
			max = *c.MaxLength
		}
		if c.RustName != w.rust || c.SQLName != w.sql || c.Type == nil || c.Type.String() != w.typ || max != w.max || c.RustRaw != w.raw || c.Unread != "" {
			t.Errorf("column %d = %+v (type %v), want %+v", i, c, c.Type, w)
		}
	}
}

func TestReadTableDefaults(t *testing.T) {
	tb := oneTable(t, "table! { posts { id -> Integer, title -> Text } }")
	if tb.HasKeys || tb.Keys != nil || tb.SQLName != "posts" || tb.Schema != "" || tb.Failure != nil {
		t.Errorf("table %+v", tb)
	}
	empty := oneTable(t, "table! { nopk () { a -> Integer, } }")
	if !empty.HasKeys || len(empty.Keys) != 0 || empty.Failure != nil {
		t.Errorf("empty key list %+v", empty)
	}
	raw := oneTable(t, "table! { r#type (r#id) { r#id -> Integer } }")
	if raw.RustName != "type" || !raw.RustRaw || raw.SQLName != "type" || raw.Failure != nil {
		t.Errorf("raw table %+v failure %+v", raw, raw.Failure)
	}
}

func TestReadTableUnreadForms(t *testing.T) {
	cases := []struct {
		name, src string
		table     bool // the table is unread (else a column)
		other     []string
	}{
		{"other table attribute", "table! { #[cfg(feature = \"x\")] t (id) { id -> Int4 } }", false, []string{"cfg"}},
		{"second sql_name", "table! { #[sql_name = \"a\"] #[sql_name = \"b\"] t (id) { id -> Int4 } }", false, []string{"sql_name"}},
		{"attribute on a use", "table! { #[allow(unused)] use x::*; t (id) { id -> Int4 } }", true, nil},
		{"lifetime argument", "table! { t (id) { id -> Int4, x -> Foo<'a> } }", false, nil},
		{"qualified self type", "table! { t (id) { id -> Int4, x -> <A as B>::C } }", false, nil},
		{"associated binding", "table! { t (id) { id -> Int4, x -> Foo<Item = Bar> } }", false, nil},
		{"other column attribute", "table! { t (id) { id -> Int4, #[cfg(x)] x -> Text } }", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb := oneTable(t, tc.src)
			if tb.Failure != nil {
				t.Fatalf("failure %+v, want a readable table", tb.Failure)
			}
			if tc.table {
				if tb.Unread == "" {
					t.Errorf("want the table unread")
				}
				return
			}
			if tc.other != nil {
				if !reflect.DeepEqual(tb.OtherAttrs, tc.other) {
					t.Errorf("other attrs %q, want %q", tb.OtherAttrs, tc.other)
				}
				return
			}
			last := tb.Columns[len(tb.Columns)-1]
			if last.Unread == "" && len(last.OtherAttrs) == 0 {
				t.Errorf("column %+v: want unread or other attributes", last)
			}
		})
	}
}

func TestReadTableFailures(t *testing.T) {
	many := make([]string, 129)
	for i := range many {
		many[i] = "c" + strings.Repeat("x", i) + " -> Int4"
	}
	cases := []struct {
		name, src, form string
	}{
		{"no braces", "table! { t (id) }", FailSyntax},
		{"tokens after block", "table! { t (id) { id -> Int4 } extra }", FailSyntax},
		{"keyword table name", "table! { type (id) { id -> Int4 } }", FailSyntax},
		{"keyword column name", "table! { t (id) { id -> Int4, type -> Text } }", FailSyntax},
		{"missing arrow", "table! { t (id) { id Int4 } }", FailSyntax},
		{"reference type", "table! { t (id) { id -> &str } }", FailSyntax},
		{"empty type", "table! { t (id) { id -> } }", FailSyntax},
		{"double comma", "table! { t (id) { id -> Int4,, x -> Text } }", FailSyntax},
		{"bad key list", "table! { t (id, 1) { id -> Int4 } }", FailSyntax},
		{"sql_name not a string", "table! { #[sql_name = 1] t (id) { id -> Int4 } }", FailSyntax},
		{"sql_name path form", "table! { t (id) { #[sql_name] id -> Int4 } }", FailSyntax},
		{"max_length not an integer", "table! { t (id) { #[max_length = \"5\"] id -> Int4 } }", FailSyntax},
		{"use without semicolon", "table! { use x::* }", FailSyntax},
		{"inner attribute", "table! { #![x] t (id) { id -> Int4 } }", FailSyntax},
		{"too many columns", "table! { t (cx) { " + strings.Join(many, ", ") + " } }", FailColumns},
		{"no key and no id", "table! { t { a -> Int4 } }", FailNoKey},
		{"no columns and no key", "table! { t { } }", FailNoKey},
		{"raw id is not id", "table! { t { r#id -> Int4 } }", FailNoKey},
		{"column named like table", "table! { t (id) { id -> Int4, t -> Text } }", FailColumnLikeName},
		{"key names no column", "table! { t (id, nope) { id -> Int4 } }", FailKeyNotColumn},
		{"duplicate column", "table! { t (id) { id -> Int4, id -> Text } }", FailDuplicateCol},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tb := oneTable(t, tc.src)
			if tb.Failure == nil || tb.Failure.Form != tc.form {
				t.Errorf("failure %+v, want form %s", tb.Failure, tc.form)
			}
		})
	}
}

func TestReadJoinableAndAllowRefusals(t *testing.T) {
	items := readSrc(t, `joinable!(posts users (user_id));
joinable!(posts -> users (user_id, x));
joinable!(posts -> users user_id);
allow_tables_to_appear_in_same_query!(a, b::c);
allow_tables_to_appear_in_same_query!();`)
	for i := 0; i < 4; i++ {
		if items[i].Unread == "" {
			t.Errorf("item %d %+v: want unread", i, items[i])
		}
	}
	if items[4].Allow == nil || len(items[4].Allow.Tables) != 0 {
		t.Errorf("empty allow %+v", items[4])
	}
}

func TestReadFileFailuresAndMarkers(t *testing.T) {
	var tags []string
	in := Input{Tag: func(row string) { tags = append(tags, row) }}
	src := `table! { users (id) { id -> Int4 } }
table! { users (id) { id -> Int4 } }
mod a { table! { users (id) { id -> Int4 } } }
table! { t { a -> Int4 } }
`
	sf, diags := ReadFile(in, "src/schema.rs", []byte(src))
	if sf.LexError != nil || len(diags) != 3 {
		t.Fatalf("diags %+v", diags)
	}
	for _, d := range diags {
		if d.Code != codeImportFailure || d.Severity != "warning" || d.File != "src/schema.rs" {
			t.Errorf("diag %+v", d)
		}
	}
	if !strings.Contains(diags[0].Message, "src/schema.rs:1: table! users does not compile (table users is declared 2 times in one module)") {
		t.Errorf("message %q", diags[0].Message)
	}
	if !reflect.DeepEqual(tags, []string{"DI-P1/duplicate-table", "DI-P1/duplicate-table", "DI-P1/no-key"}) {
		t.Errorf("tags %q", tags)
	}
	if sf.Items[2].Items[0].Table.Failure != nil {
		t.Errorf("the table in module a is not a duplicate")
	}
	sf, diags = ReadFile(Input{}, "src/schema.rs", []byte("table! { t (id) { id -> Int4 }"))
	if sf.LexError == nil || len(diags) != 1 || diags[0].Code != codeUnread {
		t.Errorf("unbalanced file: %+v %+v", sf, diags)
	}
}

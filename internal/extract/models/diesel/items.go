package diesel

import (
	"fmt"
	"strconv"
	"strings"
)

// ItemKind is the kind of an item the reader recognises in a schema file.
type ItemKind int

// Item kinds. Every item outside the first five is ItemOther (row D).
const (
	// ItemTable is a `table!`, `diesel::table!` or `::diesel::table!`
	// invocation (row DI-T1).
	ItemTable ItemKind = iota
	// ItemJoinable is a `joinable!` or `diesel::joinable!` invocation
	// (row DI-J1).
	ItemJoinable
	// ItemAllowTables is an `allow_tables_to_appear_in_same_query!`
	// invocation, bare or `diesel::` qualified (row DI-J2).
	ItemAllowTables
	// ItemModule is an inline `mod name { ... }`; Items holds its items.
	ItemModule
	// ItemStruct is a unit or field struct, read for the sql_types module
	// (row DI-S1).
	ItemStruct
	// ItemUse is a `use` declaration.
	ItemUse
	// ItemInner is an inner attribute or inner doc comment (#![...],
	// //!): no definition.
	ItemInner
	// ItemEnum is an `enum Name { ... }` item: diesel_cli 2.4 writes one
	// per Postgres enum in its `pub mod rust_types` module (no definition
	// there, row DI-S1); anywhere else it is row D. Reason says what it is.
	ItemEnum
	// ItemOther is any other item: another macro, a function, an impl,
	// a `mod name;`, ... (row D). Reason says what it is.
	ItemOther
)

// Item is one item of a schema file or of an inline module.
type Item struct {
	Kind ItemKind
	// Line and EndLine span the item, attributes included.
	Line, EndLine int
	// NameLine is the line of a struct's or enum's name (its attribute
	// lines are Line to NameLine).
	NameLine int
	// Module is the path of inline modules around the item (empty at the
	// file level).
	Module []string
	// Attrs are the item's outer attributes and doc comments.
	Attrs []Attr
	// Macro is the invocation path as written ("diesel::table") for macro
	// items.
	Macro string
	// Name is a module's or struct's name.
	Name string
	// Table is set for ItemTable, Joinable for ItemJoinable and Allow for
	// ItemAllowTables; Struct for ItemStruct; Items for ItemModule; Use is
	// the use tree as written.
	Table    *Table
	Joinable *Joinable
	Allow    *AllowTables
	Struct   *SQLType
	Items    []Item
	Use      string
	// Unread explains a recognised macro whose input the reader cannot
	// read (row D for that item); Reason says what an ItemOther is.
	Unread string
	Reason string
}

// Attr is one outer attribute (`#[...]`) or doc comment.
type Attr struct {
	// Doc is set for a doc comment or #[doc = ...]; Name is the
	// attribute path's single identifier ("sql_name"), "" when the path
	// is not a single identifier.
	Doc  bool
	Name string
	// Value is the literal of a `name = literal` attribute; Args the
	// group of a `name(...)` attribute; Path is true for a bare `#[name]`.
	Value *Token
	Args  *Tree
	Path  bool
	Line  int
	// Text is a doc comment's text after its marker (`///`), "" for an
	// attribute: the docs that tell diesel_cli 2.0 output from later
	// output (Revision 2 addendum).
	Text string
}

// Table is a `table!` invocation as diesel_table_macro_syntax 0.3.0
// parses it (lib.rs:33-80).
type Table struct {
	// Uses are the leading `use` declarations, as written.
	Uses []string
	// Attrs are the table's outer attributes (docs, sql_name, others).
	Attrs []Attr
	// RustName is the table module's name (raw flag in RustRaw); SQLName
	// the database name: #[sql_name] (r# prefix stripped) or the
	// unraw'd ident.
	RustName string
	RustRaw  bool
	SQLName  string
	// Schema is the written `schema.` prefix, "" when none.
	Schema string
	// Keys is the written primary-key list; HasKeys is false when there
	// is none (Diesel then uses `id`).
	Keys    []string
	HasKeys bool
	Columns []Column
	// OtherAttrs names table attributes other than docs and the first
	// #[sql_name]: the table's facts other than its name are D.
	OtherAttrs []string
	// Line is the table name's line; BodyLine and EndLine the column
	// block's braces. FirstLine is the line of the table's first outer
	// attribute or doc comment, Line when it has none: the lines
	// FirstLine to Line decide its provenance (row DI-G1).
	Line, BodyLine, EndLine, FirstLine int
	// Failure is set when the Diesel macros reject the table at compile
	// time (row DI-P1): the form and the reason.
	Failure *Failure
	// Unread is set when the table uses a token form the reader does not
	// model although Diesel may accept it (row D for the table).
	Unread string
}

// Column is one column definition `[#[attr]]* name -> TypePath`.
type Column struct {
	RustName string
	RustRaw  bool
	SQLName  string
	// Type is the column's type path; nil when Unread is set.
	Type *Type
	// MaxLength is #[max_length = n], nil when absent.
	MaxLength *int
	// Docs are the column's doc comments and #[doc] attributes as
	// written.
	Docs []Attr
	// OtherAttrs names attributes other than docs, the first #[sql_name]
	// and the first #[max_length].
	OtherAttrs []string
	// Line is the column name's line; FirstLine the line of its first
	// attribute or doc comment, Line when it has none: the lines FirstLine
	// to Line decide its provenance (row DI-G1).
	Line, FirstLine int
	// Unread is set when the type uses a form the reader does not model
	// (the column's facts other than its name are D).
	Unread string
}

// Type is a type path `seg::seg<Arg, ...>`. Only type arguments are
// modelled; a lifetime, const or associated-type argument makes the
// column's type unread.
type Type struct {
	// Leading is true for a path written with a leading `::`.
	Leading  bool
	Segments []Segment
}

// Segment is one path segment with its generic type arguments.
type Segment struct {
	Name string
	Args []*Type
}

// String writes the type path back in Diesel's spelling
// ("Nullable<Array<Text>>").
func (t *Type) String() string {
	var b strings.Builder
	if t.Leading {
		b.WriteString("::")
	}
	for i, s := range t.Segments {
		if i > 0 {
			b.WriteString("::")
		}
		b.WriteString(s.Name)
		if len(s.Args) > 0 {
			b.WriteByte('<')
			for j, a := range s.Args {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString(a.String())
			}
			b.WriteByte('>')
		}
	}
	return b.String()
}

// Joinable is `joinable!(child -> parent (column))` (diesel 2.3.14
// macros/mod.rs:81-86). Child and Parent are the written paths; a path
// of more than one segment is not modelled by row DI-J1.
type Joinable struct {
	Child, Parent []string
	Column        string
	Line          int
}

// AllowTables is `allow_tables_to_appear_in_same_query!(a, b, ...)`
// (macros/mod.rs:210).
type AllowTables struct {
	Tables []string
}

// SQLType is a struct of a sql_types module read through its
// `#[diesel(<backend>_type(...))]` attribute (print_schema.rs:440-455).
type SQLType struct {
	// Backend is "postgres", "mysql" or "sqlite"; "" when the struct has
	// no readable diesel type attribute (Unread then says why).
	Backend string
	// Name and Schema are the literal name and schema arguments.
	Name, Schema string
	// OID is true for postgres_type(oid = ..., array_oid = ...): no name.
	OID bool
	// Enum is true for diesel_cli 2.4's #[diesel(enum_type)] marker: the
	// type is an enum (row DI-S1).
	Enum bool
	// Line and NameLine are the struct's first attribute line and its
	// name's line: they decide its provenance (row DI-G1).
	Line, NameLine int
	// Derives are the #[derive(...)] paths as written.
	Derives []string
	Unread  string
}

// Failure is a compile-time failure the Diesel macros prove (row DI-P1).
type Failure struct {
	// Form names the DI-P1 form ("syntax", "columns", "no-key",
	// "column-named-like-table", "key-not-a-column", "duplicate-column",
	// "duplicate-table").
	Form    string
	Message string
	Line    int
}

// Failure forms of row DI-P1.
const (
	FailSyntax         = "syntax"
	FailColumns        = "columns"
	FailNoKey          = "no-key"
	FailColumnLikeName = "column-named-like-table"
	FailKeyNotColumn   = "key-not-a-column"
	FailDuplicateCol   = "duplicate-column"
	FailDuplicateTable = "duplicate-table"
)

// maxColumns is the largest table Diesel supports with every column
// feature (diesel_derives 2.3.10 table.rs:9-29). The 16/32/64 limits
// depend on crate features and are not modelled.
const maxColumns = 128

// keywords are the identifiers syn's Ident parser refuses unless written
// raw (syn 2 ident.rs accept_as_ident): a table, column or key named so
// is invalid `table!` syntax.
var keywords = map[string]bool{
	"_": true, "abstract": true, "as": true, "async": true, "await": true, "become": true,
	"box": true, "break": true, "const": true, "continue": true, "crate": true, "do": true,
	"dyn": true, "else": true, "enum": true, "extern": true, "false": true, "final": true,
	"fn": true, "for": true, "if": true, "impl": true, "in": true, "let": true, "loop": true,
	"macro": true, "match": true, "mod": true, "move": true, "mut": true, "override": true,
	"priv": true, "pub": true, "ref": true, "return": true, "Self": true, "self": true,
	"static": true, "struct": true, "super": true, "trait": true, "true": true, "try": true,
	"type": true, "typeof": true, "unsafe": true, "unsized": true, "use": true,
	"virtual": true, "where": true, "while": true, "yield": true,
}

func isPunct(t Tree, p string) bool { return t.Kind == Punct && t.Text == p }
func isWord(t Tree, w string) bool  { return t.Kind == Ident && !t.Raw && t.Text == w }
func isGroup(t Tree, d byte) bool   { return t.Kind == Group && t.Delim == d }
func isName(t Tree) bool            { return t.Kind == Ident && (t.Raw || !keywords[t.Text]) }

// lastLine returns the last line a tree spans.
func lastLine(t Tree) int {
	if t.Kind == Group {
		return t.EndLine
	}
	return t.Line
}

// ReadItems reads the items of a file's token trees, descending into
// inline modules. It never fails: what it cannot read is an ItemOther or
// an item with Unread or a Failure set.
func ReadItems(trees []Tree) []Item {
	return readItems(trees, nil)
}

func readItems(trees []Tree, module []string) []Item {
	var out []Item
	i := 0
	for i < len(trees) {
		start := i
		attrs, next := readAttrs(trees, i)
		i = next
		if i >= len(trees) {
			if len(attrs) > 0 {
				out = append(out, Item{Kind: ItemOther, Line: trees[start].Line, EndLine: lastLine(trees[len(trees)-1]), Module: module, Attrs: attrs, Reason: "attributes without an item"})
			}
			break
		}
		// Inner attributes and inner doc comments.
		if trees[i].Kind == InnerDoc {
			out = append(out, Item{Kind: ItemInner, Line: trees[i].Line, EndLine: trees[i].Line, Module: module})
			i++
			continue
		}
		if isPunct(trees[i], "#") && i+2 < len(trees) && isPunct(trees[i+1], "!") && isGroup(trees[i+2], '[') {
			out = append(out, Item{Kind: ItemInner, Line: trees[i].Line, EndLine: trees[i+2].EndLine, Module: module})
			i += 3
			continue
		}
		item := Item{Line: trees[start].Line, Module: module, Attrs: attrs}
		// Visibility.
		j := i
		if isWord(trees[j], "pub") {
			j++
			if j < len(trees) && isGroup(trees[j], '(') {
				j++
			}
		}
		if path, k, ok := macroPath(trees, i); ok {
			item.Macro = path
			body := trees[k+1]
			end := k + 2
			if end < len(trees) && isPunct(trees[end], ";") {
				end++
			}
			item.EndLine = lastLine(trees[end-1])
			readMacro(&item, body)
			out = append(out, item)
			i = end
			continue
		}
		if j < len(trees) {
			switch {
			case isWord(trees[j], "mod") && j+2 < len(trees) && trees[j+1].Kind == Ident && isGroup(trees[j+2], '{'):
				item.Kind, item.Name = ItemModule, trees[j+1].Text
				item.Items = readItems(trees[j+2].Kids, append(append([]string(nil), module...), trees[j+1].Text))
				item.EndLine = trees[j+2].EndLine
				out = append(out, item)
				i = j + 3
				continue
			case isWord(trees[j], "use"):
				end := j + 1
				for end < len(trees) && !isPunct(trees[end], ";") {
					end++
				}
				item.Kind, item.Use = ItemUse, joinTokens(trees[j+1:end])
				if end < len(trees) {
					end++
				}
				item.EndLine = lastLine(trees[end-1])
				out = append(out, item)
				i = end
				continue
			case isWord(trees[j], "struct") && j+1 < len(trees) && trees[j+1].Kind == Ident:
				item.Kind, item.Name, item.NameLine = ItemStruct, trees[j+1].Text, trees[j+1].Line
				item.Struct = readSQLType(attrs)
				item.Struct.Line, item.Struct.NameLine = item.Line, item.NameLine
				end := skipItem(trees, j)
				item.EndLine = lastLine(trees[end-1])
				out = append(out, item)
				i = end
				continue
			case isWord(trees[j], "enum") && j+1 < len(trees) && trees[j+1].Kind == Ident:
				end := skipItem(trees, j)
				item.Kind, item.Name, item.NameLine = ItemEnum, trees[j+1].Text, trees[j+1].Line
				item.EndLine = lastLine(trees[end-1])
				item.Reason = describe(trees[i:end])
				out = append(out, item)
				i = end
				continue
			}
		}
		end := skipItem(trees, i)
		item.Kind, item.EndLine = ItemOther, lastLine(trees[end-1])
		item.Reason = describe(trees[i:end])
		out = append(out, item)
		i = end
	}
	return out
}

// readAttrs reads outer attributes and doc comments from position i.
func readAttrs(trees []Tree, i int) ([]Attr, int) {
	var attrs []Attr
	for i < len(trees) {
		switch {
		case trees[i].Kind == OuterDoc:
			attrs = append(attrs, Attr{Doc: true, Name: "doc", Line: trees[i].Line, Text: trees[i].Text})
			i++
		case isPunct(trees[i], "#") && i+1 < len(trees) && isGroup(trees[i+1], '['):
			attrs = append(attrs, readAttr(trees[i+1]))
			i += 2
		default:
			return attrs, i
		}
	}
	return attrs, i
}

// readAttr reads the inside of `#[...]`: `name`, `name = literal` or
// `name(...)`; a path of several segments or another shape leaves Name
// empty.
func readAttr(g Tree) Attr {
	a := Attr{Line: g.Line}
	k := g.Kids
	if len(k) == 0 || k[0].Kind != Ident {
		return a
	}
	if len(k) > 1 && isPunct(k[1], "::") {
		return a
	}
	name := k[0].Text
	switch {
	case len(k) == 1:
		a.Name, a.Path = name, true
	case len(k) == 3 && isPunct(k[1], "="):
		a.Name = name
		if k[2].Kind == String || k[2].Kind == Number || k[2].Kind == Char || isWord(k[2], "true") || isWord(k[2], "false") {
			v := k[2].Token
			a.Value = &v
		}
	case len(k) == 2 && k[1].Kind == Group:
		a.Name = name
		args := k[1]
		a.Args = &args
	default:
		a.Name = name
	}
	a.Doc = a.Name == "doc" && a.Value != nil && a.Value.Kind == String
	return a
}

// macroPath reads a macro invocation path at i (`a!`, `a::b!`, `::a::b!`)
// followed by a group; k is the index of the `!`.
func macroPath(trees []Tree, i int) (string, int, bool) {
	var b strings.Builder
	j := i
	if j < len(trees) && isPunct(trees[j], "::") {
		b.WriteString("::")
		j++
	}
	for j < len(trees) && trees[j].Kind == Ident {
		if trees[j].Raw {
			b.WriteString("r#")
		}
		b.WriteString(trees[j].Text)
		j++
		if j < len(trees) && isPunct(trees[j], "::") {
			b.WriteString("::")
			j++
			continue
		}
		if j+1 < len(trees) && isPunct(trees[j], "!") && trees[j+1].Kind == Group {
			return b.String(), j, true
		}
		return "", 0, false
	}
	return "", 0, false
}

// macroKinds maps the invocation paths the contract lists to their kind.
var macroKinds = map[string]ItemKind{
	"table": ItemTable, "diesel::table": ItemTable, "::diesel::table": ItemTable,
	"joinable": ItemJoinable, "diesel::joinable": ItemJoinable, "::diesel::joinable": ItemJoinable,
	"allow_tables_to_appear_in_same_query":           ItemAllowTables,
	"diesel::allow_tables_to_appear_in_same_query":   ItemAllowTables,
	"::diesel::allow_tables_to_appear_in_same_query": ItemAllowTables,
}

func readMacro(item *Item, body Tree) {
	kind, ok := macroKinds[item.Macro]
	if !ok {
		item.Kind, item.Reason = ItemOther, "macro "+item.Macro+"!"
		return
	}
	item.Kind = kind
	switch kind {
	case ItemTable:
		item.Table = readTable(body.Kids, body)
	case ItemJoinable:
		item.Joinable, item.Unread = readJoinable(body.Kids)
	case ItemAllowTables:
		item.Allow, item.Unread = readAllowTables(body.Kids)
	}
}

// skipItem returns the index after the item starting at i: through the
// first `;` or `{...}` group at this level.
func skipItem(trees []Tree, i int) int {
	for j := i; j < len(trees); j++ {
		if isPunct(trees[j], ";") || isGroup(trees[j], '{') {
			return j + 1
		}
	}
	return len(trees)
}

// describe names an unread item by its first words.
func describe(trees []Tree) string {
	var words []string
	for _, t := range trees {
		if t.Kind != Ident || len(words) == 3 {
			break
		}
		words = append(words, t.Text)
	}
	if len(words) == 0 {
		return "unrecognised tokens"
	}
	return strings.Join(words, " ")
}

// joinTokens writes trees back as compact text (paths, use trees).
func joinTokens(trees []Tree) string {
	var b strings.Builder
	for i, t := range trees {
		if t.Kind == Ident && i > 0 && trees[i-1].Kind == Ident {
			b.WriteByte(' ')
		}
		switch t.Kind {
		case Group:
			open, close := string(t.Delim), map[byte]string{'(': ")", '[': "]", '{': "}"}[t.Delim]
			b.WriteString(open + joinTokens(t.Kids) + close)
		case Ident:
			if t.Raw {
				b.WriteString("r#")
			}
			b.WriteString(t.Text)
		case String:
			b.WriteString(strconv.Quote(t.Text))
		default:
			b.WriteString(t.Text)
			if t.Text == "," {
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

// readTable reads a table! body after diesel_table_macro_syntax 0.3.0
// (lib.rs:33-80) and checks the compile-time failures of diesel_derives
// 2.3.10 (table.rs:9-140). A shape syn refuses is a FailSyntax failure
// (lib.rs:1450-1462 turns every parse error into "invalid `table!`
// syntax"); a shape syn may accept but the reader does not model is
// Unread.
func readTable(k []Tree, body Tree) *Table {
	t := &Table{Line: body.Line, EndLine: body.EndLine}
	fail := func(line int, format string, args ...any) *Table {
		t.Failure = &Failure{Form: FailSyntax, Message: fmt.Sprintf(format, args...), Line: line}
		return t
	}
	i := 0
	// Leading use declarations (syn::ItemUse: attributes, visibility,
	// `use`, tree, `;`).
	for {
		attrs, j := readAttrs(k, i)
		if j < len(k) && isWord(k[j], "pub") {
			j++
			if j < len(k) && isGroup(k[j], '(') {
				j++
			}
		}
		if j >= len(k) || !isWord(k[j], "use") {
			break
		}
		end := j + 1
		for end < len(k) && !isPunct(k[end], ";") {
			end++
		}
		if end >= len(k) {
			return fail(k[j].Line, "a use declaration without `;`")
		}
		if !useTree(k[j+1 : end]) {
			t.Unread = "a use declaration the reader does not model"
		}
		if len(attrs) > 0 {
			t.Unread = "an attribute on a use declaration"
		}
		t.Uses = append(t.Uses, joinTokens(k[j+1:end]))
		i = end + 1
	}
	attrs, i := readAttrs(k, i)
	t.Attrs = attrs
	if i >= len(k) || !isName(k[i]) {
		line := body.Line
		if i < len(k) {
			line = k[i].Line
		}
		return fail(line, "the table name is not an identifier")
	}
	t.RustName, t.RustRaw = k[i].Text, k[i].Raw
	t.Line, t.FirstLine = k[i].Line, k[i].Line
	if len(attrs) > 0 {
		t.FirstLine = attrs[0].Line
	}
	i++
	if i+1 < len(k) && isPunct(k[i], ".") && isName(k[i+1]) {
		t.Schema = t.RustName
		if t.RustRaw {
			t.Unread = "a raw schema identifier"
		}
		t.RustName, t.RustRaw = k[i+1].Text, k[i+1].Raw
		i += 2
	}
	if i < len(k) && isGroup(k[i], '(') {
		keys, ok := identList(k[i].Kids)
		if !ok {
			return fail(k[i].Line, "the primary key list is not a list of identifiers")
		}
		t.Keys, t.HasKeys = keys, true
		i++
	}
	if i >= len(k) || !isGroup(k[i], '{') {
		return fail(t.Line, "no `{ columns }` block after the table name")
	}
	if i+1 != len(k) {
		return fail(k[i+1].Line, "tokens after the column block")
	}
	block := k[i]
	t.BodyLine, t.EndLine = block.Line, block.EndLine
	sqlName, rest, err := sqlNameAttr(attrs)
	if err != "" {
		return fail(t.Line, "%s", err)
	}
	t.OtherAttrs = attrNames(rest)
	// get_sql_name (lib.rs:117-137): the attribute's value, else the
	// identifier without its r# prefix (stored without it already).
	t.SQLName = t.RustName
	if sqlName != nil {
		t.SQLName = *sqlName
	}
	for _, part := range splitColumns(block.Kids) {
		c, msg := readColumn(part)
		if msg != "" {
			line := block.Line
			if len(part) > 0 {
				line = part[0].Line
			}
			return fail(line, "%s", msg)
		}
		t.Columns = append(t.Columns, c)
	}
	checkTable(t)
	return t
}

// takeAttr removes the first attribute with the given name, as take_lit
// does (lib.rs:140-176), and returns it and the others.
func takeAttr(attrs []Attr, name string) (*Attr, []Attr) {
	for i, a := range attrs {
		if a.Name == name && !a.Doc {
			rest := append(append([]Attr(nil), attrs[:i]...), attrs[i+1:]...)
			return &a, rest
		}
	}
	return nil, attrs
}

// sqlNameAttr takes the first #[sql_name] attribute: it must be
// `sql_name = "string"` (an r# prefix in the value is stripped,
// lib.rs:117-137); the message is a syntax failure otherwise.
func sqlNameAttr(attrs []Attr) (*string, []Attr, string) {
	a, rest := takeAttr(attrs, "sql_name")
	if a == nil {
		return nil, rest, ""
	}
	if a.Value == nil || a.Value.Kind != String || a.Value.Prefix != "" && a.Value.Prefix != "r" {
		return nil, nil, "#[sql_name] is not `sql_name = \"<name>\"`"
	}
	v := strings.TrimPrefix(a.Value.Text, "r#")
	return &v, rest, ""
}

// attrNames names the attributes other than doc comments.
func attrNames(attrs []Attr) []string {
	var out []string
	for _, a := range attrs {
		switch {
		case a.Doc:
		case a.Name == "":
			out = append(out, "an attribute with a path")
		default:
			out = append(out, a.Name)
		}
	}
	return out
}

// identList reads `a, b, c` with an optional trailing comma.
func identList(k []Tree) ([]string, bool) {
	out := []string{}
	for i := 0; i < len(k); i++ {
		if !isName(k[i]) {
			return nil, false
		}
		out = append(out, k[i].Text)
		if i+1 < len(k) {
			if !isPunct(k[i+1], ",") {
				return nil, false
			}
			i++
		}
	}
	return out, true
}

// useTree reports a use tree of paths, `*`, `self`, `as` renames and
// braced lists.
func useTree(k []Tree) bool {
	if len(k) == 0 {
		return false
	}
	for _, t := range k {
		switch {
		case t.Kind == Ident:
		case isPunct(t, "::"), isPunct(t, "*"), isPunct(t, ","):
		case isGroup(t, '{'):
			if len(t.Kids) > 0 && !useTree(t.Kids) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// splitColumns splits a column block at commas outside generic angle
// brackets; an empty last part (a trailing comma) is dropped.
func splitColumns(k []Tree) [][]Tree {
	var out [][]Tree
	depth := 0
	start := 0
	for i, t := range k {
		switch {
		case isPunct(t, "<"):
			depth++
		case isPunct(t, ">") && depth > 0:
			depth--
		case isPunct(t, ",") && depth == 0:
			out = append(out, k[start:i])
			start = i + 1
		}
	}
	if start < len(k) {
		out = append(out, k[start:])
	}
	return out
}

// readColumn reads `[#[attr]]* name -> TypePath` (lib.rs:85-107). A
// message is a syntax failure.
func readColumn(part []Tree) (Column, string) {
	var c Column
	if len(part) == 0 {
		return c, "an empty column definition (two commas)"
	}
	attrs, i := readAttrs(part, 0)
	if i >= len(part) || !isName(part[i]) {
		return c, "a column name is not an identifier"
	}
	c.RustName, c.RustRaw = part[i].Text, part[i].Raw
	c.Line, c.FirstLine = part[i].Line, part[i].Line
	if len(attrs) > 0 {
		c.FirstLine = attrs[0].Line
	}
	for _, a := range attrs {
		if a.Doc {
			c.Docs = append(c.Docs, a)
		}
	}
	i++
	if i >= len(part) || !isPunct(part[i], "->") {
		return c, fmt.Sprintf("column %s has no `->`", c.RustName)
	}
	i++
	typ := part[i:]
	if len(typ) == 0 || !(typ[0].Kind == Ident || isPunct(typ[0], "::") || isPunct(typ[0], "<")) {
		return c, fmt.Sprintf("the type of column %s is not a type path", c.RustName)
	}
	if typ[0].Kind == Ident && !typ[0].Raw && keywords[typ[0].Text] && typ[0].Text != "self" && typ[0].Text != "Self" && typ[0].Text != "super" && typ[0].Text != "crate" {
		return c, fmt.Sprintf("the type of column %s is not a type path", c.RustName)
	}
	if t, ok := readType(typ); ok {
		c.Type = t
	} else {
		c.Unread = "the type " + joinTokens(typ) + " is not a plain type path"
	}
	c.SQLName = c.RustName
	sqlName, rest, err := sqlNameAttr(attrs)
	if err != "" {
		return c, err
	}
	if sqlName != nil {
		c.SQLName = *sqlName
	}
	a, rest := takeAttr(rest, "max_length")
	if a != nil {
		n, ok := 0, false
		if a.Value != nil && a.Value.Kind == Number {
			n, ok = intLiteral(a.Value.Text)
		}
		if !ok {
			return c, "#[max_length] is not `max_length = <integer>`"
		}
		c.MaxLength = &n
	}
	c.OtherAttrs = attrNames(rest)
	return c, ""
}

// intLiteral reads a Rust integer literal (decimal, 0x, 0o, 0b, with
// underscores and an integer suffix); a float is not an integer.
func intLiteral(text string) (int, bool) {
	s := strings.ReplaceAll(text, "_", "")
	for _, suffix := range []string{"usize", "isize", "u128", "i128", "u64", "i64", "u32", "i32", "u16", "i16", "u8", "i8"} {
		if strings.HasSuffix(s, suffix) {
			s = strings.TrimSuffix(s, suffix)
			break
		}
	}
	base := 10
	switch {
	case strings.HasPrefix(s, "0x"):
		base, s = 16, s[2:]
	case strings.HasPrefix(s, "0o"):
		base, s = 8, s[2:]
	case strings.HasPrefix(s, "0b"):
		base, s = 2, s[2:]
	}
	v, err := strconv.ParseInt(s, base, 64)
	if err != nil || v < 0 || v > 1<<31-1 {
		return 0, false
	}
	return int(v), true
}

// readType reads a whole type path; ok is false for any other shape
// (a qualified self type, a lifetime or const argument, an associated
// type binding, `dyn`, a function sugar ...).
func readType(k []Tree) (*Type, bool) {
	t, n, ok := readTypeAt(k, 0)
	if !ok || n != len(k) {
		return nil, false
	}
	return t, true
}

func readTypeAt(k []Tree, i int) (*Type, int, bool) {
	t := &Type{}
	if i < len(k) && isPunct(k[i], "::") {
		t.Leading = true
		i++
	}
	for {
		if i >= len(k) || k[i].Kind != Ident {
			return nil, i, false
		}
		seg := Segment{Name: k[i].Text}
		if k[i].Raw {
			seg.Name = "r#" + seg.Name
		}
		i++
		if i+1 < len(k) && isPunct(k[i], "::") && isPunct(k[i+1], "<") {
			i++ // turbofish
		}
		if i < len(k) && isPunct(k[i], "<") {
			i++
			for {
				if i < len(k) && isPunct(k[i], ">") {
					i++
					break
				}
				arg, next, ok := readTypeAt(k, i)
				if !ok {
					return nil, next, false
				}
				seg.Args = append(seg.Args, arg)
				i = next
				if i < len(k) && isPunct(k[i], ",") {
					i++
					continue
				}
				if i < len(k) && isPunct(k[i], ">") {
					i++
					break
				}
				return nil, i, false
			}
		}
		t.Segments = append(t.Segments, seg)
		if i < len(k) && isPunct(k[i], "::") {
			i++
			continue
		}
		return t, i, true
	}
}

// checkTable applies the compile-time checks of diesel_derives 2.3.10 to
// a table that parsed: more than 128 columns (table.rs:9-29), no key list
// and no `id` column (table.rs:46-87), a key naming no column, a column
// named like its table (table.rs:130-140), two columns with one Rust
// name.
func checkTable(t *Table) {
	set := func(form, msg string, line int) {
		if t.Failure == nil {
			t.Failure = &Failure{Form: form, Message: msg, Line: line}
		}
	}
	if len(t.Columns) > maxColumns {
		set(FailColumns, fmt.Sprintf("table %s has %d columns; Diesel supports at most %d", t.RustName, len(t.Columns), maxColumns), t.Line)
	}
	seen := map[string]int{}
	hasID := false
	for _, c := range t.Columns {
		key := identKey(c.RustName, c.RustRaw)
		if line, dup := seen[key]; dup {
			set(FailDuplicateCol, fmt.Sprintf("column %s of table %s is declared twice (line %d)", c.RustName, t.RustName, line), c.Line)
		}
		seen[key] = c.Line
		if key == "id" {
			hasID = true
		}
		if key == identKey(t.RustName, t.RustRaw) {
			set(FailColumnLikeName, fmt.Sprintf("column %s is named like its table", c.RustName), c.Line)
		}
	}
	if !t.HasKeys && !hasID {
		set(FailNoKey, fmt.Sprintf("table %s has no primary key list and no `id` column", t.RustName), t.Line)
	}
	for _, k := range t.Keys {
		if _, ok := seen[k]; !ok {
			if _, ok := seen["r#"+k]; !ok {
				set(FailKeyNotColumn, fmt.Sprintf("primary key %s of table %s names no column", k, t.RustName), t.Line)
			}
		}
	}
}

// identKey is an identifier as proc_macro2 compares it: a raw identifier
// differs from the plain one.
func identKey(name string, raw bool) string {
	if raw {
		return "r#" + name
	}
	return name
}

// readJoinable reads `child -> parent (column)` with `::`-separated
// paths on both sides, as the macro_rules pattern accepts.
func readJoinable(k []Tree) (*Joinable, string) {
	j := &Joinable{}
	path := func(i int) ([]string, int) {
		var segs []string
		for i < len(k) && k[i].Kind == Ident {
			segs = append(segs, k[i].Text)
			i++
			if i < len(k) && isPunct(k[i], "::") {
				i++
				continue
			}
			break
		}
		return segs, i
	}
	if len(k) > 0 {
		j.Line = k[0].Line
	}
	var i int
	j.Child, i = path(0)
	if len(j.Child) == 0 || i >= len(k) || !isPunct(k[i], "->") {
		return nil, "not `child -> parent (column)`"
	}
	j.Parent, i = path(i + 1)
	if len(j.Parent) == 0 || i+1 != len(k) || !isGroup(k[i], '(') || len(k[i].Kids) != 1 || k[i].Kids[0].Kind != Ident {
		return nil, "not `child -> parent (column)`"
	}
	j.Column = k[i].Kids[0].Text
	return j, ""
}

// readAllowTables reads `a, b, c` with optional trailing commas.
func readAllowTables(k []Tree) (*AllowTables, string) {
	a := &AllowTables{}
	expectName := true
	for _, t := range k {
		switch {
		case expectName && t.Kind == Ident:
			a.Tables = append(a.Tables, t.Text)
			expectName = false
		case isPunct(t, ","):
			expectName = true
		default:
			return nil, "not a list of table names"
		}
	}
	return a, ""
}

// readSQLType reads a struct's #[derive(...)] and
// #[diesel(<backend>_type(...))] attributes.
func readSQLType(attrs []Attr) *SQLType {
	s := &SQLType{}
	found := false
	for _, a := range attrs {
		switch {
		case a.Name == "derive" && a.Args != nil && a.Args.Delim == '(':
			for _, part := range splitColumns(a.Args.Kids) {
				s.Derives = append(s.Derives, joinTokens(part))
			}
		case a.Name == "diesel" && a.Args != nil && a.Args.Delim == '(' && len(a.Args.Kids) == 1 && isWord(a.Args.Kids[0], "enum_type") && !s.Enum:
			// diesel_cli 2.4 marks the struct of an enum type
			// (print_schema.rs:1314-1316).
			s.Enum = true
		case a.Name == "diesel" && a.Args != nil && a.Args.Delim == '(':
			k := a.Args.Kids
			if len(k) != 2 || k[0].Kind != Ident || !isGroup(k[1], '(') {
				s.Unread = "a #[diesel(...)] attribute the reader does not model"
				continue
			}
			backend := map[string]string{"postgres_type": "postgres", "mysql_type": "mysql", "sqlite_type": "sqlite"}[k[0].Text]
			if backend == "" || found {
				s.Unread = "a #[diesel(...)] attribute the reader does not model"
				continue
			}
			found = true
			s.Backend = backend
			args := map[string]Tree{}
			for _, part := range splitColumns(k[1].Kids) {
				if len(part) != 3 || part[0].Kind != Ident || !isPunct(part[1], "=") {
					s.Unread = "a " + k[0].Text + " argument the reader does not model"
					continue
				}
				args[part[0].Text] = part[2]
			}
			for key, v := range args {
				switch {
				case (key == "name" || key == "schema") && v.Kind == String && v.Prefix == "":
					if key == "name" {
						s.Name = v.Text
					} else {
						s.Schema = v.Text
					}
				case backend == "postgres" && (key == "oid" || key == "array_oid") && v.Kind == Number:
					s.OID = true
				default:
					s.Unread = "a " + k[0].Text + " argument the reader does not model"
				}
			}
			if s.Name == "" && !s.OID && s.Unread == "" {
				s.Unread = k[0].Text + " has no name"
			}
		}
	}
	if !found && s.Unread == "" {
		s.Unread = "no #[diesel(<backend>_type(...))] attribute"
	}
	return s
}

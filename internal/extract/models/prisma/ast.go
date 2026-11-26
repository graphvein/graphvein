// Package prisma is the Prisma front end of the model kit
// (internal/extract/models, ADR 0058; T018g1, ADR 0057). It finds the
// Prisma schema of each package that depends on Prisma as the pinned
// Prisma 7 CLI finds it (discover.go), reads every file of that schema with
// a hand-written lexer and parser for the Prisma schema language (lexer.go,
// parser.go) and, from step G1B2 on, claims tables and columns under the
// claim contract of docs/extract/schema-prisma.md (contract.go).
//
// The parser follows the grammar of the pinned Prisma schema engine
// (psl/schema-ast datamodel.pest at prisma-engines 0edf323efd1d, Prisma
// 7.10.0): the language is line-based (a field, an attribute with its
// arguments and a datasource key are each one line), whitespace is spaces
// and tabs only, and strings are JSON strings. Every form the grammar
// accepts is read into the syntax tree below; everything Prisma rejects at
// parse time is a parse error of the file, because Prisma then loads no
// schema at all and no fact of it can be claimed.
package prisma

// File is one parsed .prisma file.
type File struct {
	// Path is the repository-relative path.
	Path   string
	Blocks []*Block
	// Errors are the parse errors; Prisma refuses a schema with any of
	// them, so a file with errors yields no claim.
	Errors []ParseError
}

// ParseError is one parse error with its 1-based position.
type ParseError struct {
	Line, Col int
	Message   string
}

// Block keywords. A block without a known keyword (Prisma's
// "arbitrary block") has Keyword "" and a parse error; a type alias
// (`type A = B`) has KeywordTypeAlias and a parse error.
const (
	KeywordModel      = "model"
	KeywordView       = "view"
	KeywordType       = "type"
	KeywordEnum       = "enum"
	KeywordDatasource = "datasource"
	KeywordGenerator  = "generator"
	KeywordTypeAlias  = "type alias"
)

// Block is one top-level block. Which lists are filled depends on the
// keyword: Fields for model, view and type; Values for enum; Properties
// for datasource and generator; Attributes (@@ block attributes) for
// model, view, type and enum.
type Block struct {
	Keyword string
	Name    string
	// StartLine is the keyword's line, EndLine the closing brace's (the
	// last line read when the block is not closed).
	StartLine, EndLine int
	Fields             []*Field
	Values             []*EnumValue
	Properties         []*Property
	Attributes         []*Attribute
}

// Field is one field line of a model, view or composite type.
type Field struct {
	Name      string
	Line, Col int
	// Type is nil when the line names no type (a validation error in
	// Prisma, kept so the front end can refuse it).
	Type       *FieldType
	Attributes []*Attribute
	// LegacyColon is the obsolete `name: Type` form.
	LegacyColon bool
}

// FieldType is a field's type with its modifiers.
type FieldType struct {
	// Name is the type name; for Unsupported("...") it is "Unsupported" and
	// Unsupported holds the string argument.
	Name        string
	Unsupported *Expr
	// List is `T[]`, Optional `T?`; both together are `T[]?`, which Prisma
	// rejects in validation.
	List, Optional bool
	// Legacy is the obsolete form: "!" (`T!`) or "[]" (`[T]`); empty for
	// the current syntax.
	Legacy    string
	Line, Col int
}

// EnumValue is one value line of an enum.
type EnumValue struct {
	Name       string
	Line, Col  int
	Attributes []*Attribute
}

// Property is one `key = value` line of a datasource or generator; Value
// is nil when nothing follows the `=`.
type Property struct {
	Key       string
	Value     *Expr
	Line, Col int
}

// Attribute is a field attribute (`@name(...)`) or a block attribute
// (`@@name(...)`). Name is the path after the @ signs (`id`, `db.VarChar`).
type Attribute struct {
	Name      string
	Block     bool
	Line, Col int
	// Args is nil when the attribute has no parentheses; `@x()` has an
	// empty, non-nil list (Prisma treats both alike).
	Args *ArgList
	// Raw is the attribute's source text as written.
	Raw string
}

// ArgList is a parenthesised argument list.
type ArgList struct {
	Args          []*Arg
	TrailingComma bool
}

// Arg is one argument: Name is empty for a positional argument; Value is
// nil for an empty named argument (`name:`), which Prisma rejects in
// validation.
type Arg struct {
	Name  string
	Value *Expr
}

// Expression kinds.
const (
	ExprString = "string"
	ExprNumber = "number"
	// ExprConstant is a bare path: true, false, an enum value, Cascade,
	// db.VarChar ...
	ExprConstant = "constant"
	ExprCall     = "call"
	ExprArray    = "array"
	ExprObject   = "object"
)

// Expr is one expression.
type Expr struct {
	Kind string
	// Value is the decoded string for ExprString, the text for ExprNumber
	// and ExprConstant, and the function name for ExprCall.
	Value string
	// Args are a call's arguments.
	Args *ArgList
	// Items are an array's elements.
	Items []*Expr
	// Members are an object's members, in order.
	Members []*Arg
	// Raw is the expression's source text as written.
	Raw       string
	Line, Col int
}

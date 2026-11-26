// Package models is the tool-neutral core of the model-defined schema
// (T018g0, ADR 0058, amending the package layout of ADR 0054). An ORM front
// end (SQLModel in internal/extract/models/sqlmodel; Prisma, Drizzle, ...
// later) reads its own source forms under its own claim contract and fills
// the records defined here: tables, columns, foreign keys, reasons and
// per-fact claims. This package then does what every front end shares:
// foreign-key resolution within a group (row L4 for SQLModel), the
// comparison with migration sets (L5), step isolation and the emitted node
// and edge shapes (L3), so the Schema API (ADR 0055) reads every ORM's
// tables the same way.
//
// The package decides no fact. A record field the front end leaves nil is
// unknown, and a claim is emitted only for a fact that is known in the
// final record, so a fact held as null never carries a claim.
package models

import (
	"github.com/graphvein/graphvein/internal/contract"
)

// Verification values of a claim (developer decision 2026-11-09): oracle
// means the ORM's oracle checked this kind of fact for the row on the
// group's dialect (or, for an ORM whose facts are dialect-independent, at
// the ORM's own metadata level); mapped means the fact comes from the ORM's
// own per-dialect mapping source and was not checked against that
// database. The Schema view marks mapped facts "unverified".
const (
	VerificationOracle = "oracle"
	VerificationMapped = "mapped"
)

// Group is one model group: one ORM tool and one schema root (G3, ADR
// 0055). Its tables share the group key, pins, assumptions and dialect.
type Group struct {
	// Tool is the ORM ("sqlmodel", "prisma", ...).
	Tool string
	// Root is the repository-relative root the tool's rule defines ("." for
	// the repository root); it is part of the group key.
	Root string
	// Pins are the library versions the contract describes.
	Pins map[string]any
	// Assumptions are the assumption ids every table of the group states.
	Assumptions []string
	// Dialect is the database dialect the source states (Prisma's
	// provider); empty when the source states none, and then the records
	// carry no dialect attribute.
	Dialect string
	// Dialects are the dialects a group whose dialect is chosen at run
	// time may run on (GORM's gotify: mysql, postgres, sqlite; ADR 0065).
	// Its tables carry them as dialects, and its columns may state the
	// DialectFacts once per dialect (Column.ByDialect). Empty for a group
	// with one stated dialect or none; a group must not set both.
	Dialects []string
	// Verification is the verification of a claim that does not set its
	// own (VerificationOracle or VerificationMapped).
	Verification string
}

// SetKey is the group key stored as the tables' set attribute:
// "models/<tool>/<root>".
func (g Group) SetKey() string { return SetKey(g.Tool, g.Root) }

// SetKey builds a model group key from a tool and a root.
func SetKey(tool, root string) string { return "models/" + tool + "/" + root }

// Reason explains why a fact or a set is not known, by contract row. Fact
// names the attribute it makes unknown or withholds (column_set or
// constraint_set for a table reason, an attribute name for a column
// reason) and is nil when the reason concerns the whole column claim.
// File is the file of Line, which can differ from the table's file.
type Reason struct {
	Row    string  `json:"row"`
	Fact   *string `json:"fact"`
	File   string  `json:"file"`
	Line   int     `json:"line"`
	Reason string  `json:"reason"`
}

// Fact returns a reason's fact name as a pointer.
func Fact(name string) *string { return &name }

// Claim is the provenance of one known column fact: the contract row whose
// branch set it, where (file and span of the source that states it) and
// how it is verified. Claims add no fact; they say why a fact holds. A
// per-dialect fact (ADR 0065) has one claim per dialect, each naming its
// Dialect; a dialect-independent fact's claim has none (omitted).
type Claim struct {
	Fact         string        `json:"fact"`
	Dialect      string        `json:"dialect,omitempty"`
	Row          string        `json:"row"`
	File         string        `json:"file"`
	Span         contract.Span `json:"span"`
	Verification string        `json:"verification"`
}

// ClaimSource is what a front end records when a branch sets a fact: the
// row and, when it differs from the column's own, the file and line of the
// source and the verification.
type ClaimSource struct {
	Row string
	// File and Line default to the column's File and Line.
	File string
	Line int
	// Verification defaults to the group's.
	Verification string
}

// ClaimFacts are the column facts that carry claims, in the order the
// claims list is written. name is the column's name; foreign_keys is the
// list of foreign keys (known when the column's list is known). Text kept
// as written beside a fact (default_value, server_default_text, the
// annotation) is covered by that fact's claim. type_family (a type whose
// modifiers are unknown) and autoincrement (the database generates the
// value) are ADR 0065's; no front end wrote them before.
var ClaimFacts = []string{
	"name", "type", "type_family", "length", "timezone", "precision", "scale", "nullable",
	"primary_key", "autoincrement", "index", "unique", "default", "server_default", "foreign_keys",
}

// Table is one model-defined table as a front end built it.
type Table struct {
	// Identity is "table:<file>#<Model>".
	Identity, File, Name string
	// Model is the model's lexical name (a class, a Prisma model); ModelLine
	// is its line for messages.
	Model     string
	ModelLine int
	// ModelIdentity is the node that contains the table (a class node), or
	// empty when there is none.
	ModelIdentity string
	// Span is the model statement's span; its lines give the content hash.
	Span contract.Span
	// Schema is the schema the source states, nil for the default schema;
	// SchemaKnown says whether either is proven.
	Schema      *string
	SchemaKnown bool
	Test        bool
	// ColumnsComplete and ConstraintsComplete give column_set and
	// constraint_set; the table is complete when both are.
	ColumnsComplete, ConstraintsComplete bool
	// PrimaryKeyOpen means the constraint set may hold a primary key: the
	// table's primary_key is unknown.
	PrimaryKeyOpen bool
	// ElementsUnknown means the constraint lists read may be replaced: they
	// are written as unknown.
	ElementsUnknown bool
	Reasons         []Reason
	Uniques         []map[string]any
	Indexes         []map[string]any
	Checks          []map[string]any
	Columns         []*Column
	// ByName finds a column by name for foreign-key resolution; nil builds
	// it from Columns (the last column of a name wins).
	ByName map[string]*Column
	// PrimaryKey, when set, is the primary key's column order as the source
	// states it (Prisma's @@id([b, a]); ADR 0057). The core writes it only
	// when it names exactly the columns whose primary_key is true; nil keeps
	// the column order.
	PrimaryKey []string
	// Attributes are the tool's own table attributes (SQLModel's class and
	// ddl_hooks), written beside the shared ones.
	Attributes map[string]any
	// Verification of the table's own facts (name, schema, keys and
	// constraint lists); empty uses the group's. A front end whose table
	// facts mix both values states mapped.
	Verification string
}

// Column is one model column as a front end built it. Attributes holds the
// column facts by record name (nil: unknown); Claims holds, per fact, the
// row whose branch set it.
type Column struct {
	Name, Attribute, File string
	Line                  int
	// IdentityName, when set, replaces Name in the column's identity and
	// qualified name (ADR 0057: a Prisma column keeps its field name in
	// the identity while its name is the @map'd column name). Empty
	// (SQLModel) uses Name.
	IdentityName string
	// Unknown makes every fact unknown (state unknown, no claims).
	Unknown bool
	// ForeignKeysUnknown makes the foreign-key list unknown.
	ForeignKeysUnknown bool
	Reasons            []Reason
	Attributes         map[string]any
	ForeignKeys        []*ForeignKey
	Claims             map[string]ClaimSource
	// ByDialect holds the DialectFacts a run-time dialect choice makes
	// differ, per dialect of the group (ADR 0065), and DialectClaims the
	// claim of each, per dialect; the flat fact of the same name is then
	// written unknown. Nil for a column whose facts hold on every dialect.
	// SetDialectFact and SetDialectTypeFamily fill both.
	ByDialect     map[string]map[string]any
	DialectClaims map[string]map[string]ClaimSource
}

// ForeignKey is one foreign key of a column; the core resolves its status
// and target (L4).
type ForeignKey struct {
	// Schema is the target schema as written, nil when none is written.
	Schema *string
	Table  string
	// Column is the referenced column this column pairs with; the
	// foreign_key edge goes to it.
	Column   string
	OnDelete any
	// Columns, when set, states the whole constraint (ADR 0057, T018g0
	// review): the local columns in key order, with RefColumns the
	// referenced columns in the same order (a composite key has several;
	// each local column's record lists the constraint and pairs with the
	// referenced column at its position, given in Column). Such a key is
	// written with local_columns, columns, on_update and name, and resolves
	// only when every referenced column is a known column of the target. Nil
	// (SQLModel) writes the single-column shape without those keys.
	Columns    []string
	RefColumns []string
	// OnUpdate is the ON UPDATE action (nil: unknown); written only with
	// Columns.
	OnUpdate any
	// Name is the constraint name the source states (nil: not claimed, for
	// example a name the tool would generate); written only with Columns.
	Name *string
	// File and Line locate the source that states it.
	File string
	Line int
	// Status (resolved, ambiguous, unresolved) and Target (the target
	// column's identity) are set by the core.
	Status string
	Target string
}

// ColumnIdentity is a column node's identity:
// "column:<table file>#<Model>.<column>", with the column's IdentityName
// in place of its name when it has one.
func ColumnIdentity(t *Table, c *Column) string {
	return "column:" + t.File + "#" + t.Model + "." + identityName(c)
}

func identityName(c *Column) string {
	if c.IdentityName != "" {
		return c.IdentityName
	}
	return c.Name
}

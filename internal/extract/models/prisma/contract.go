package prisma

// Row is one row of the Prisma claim contract (docs/extract/schema-prisma.md
// "Claim contract", copied from docs/contract/claims/prisma-models.md). The
// docs hold each row's form, claim, refusal and assumptions; this table
// holds what the code needs and TestPrismaContract keeps the two 1:1: the
// same ids in the same order, the same assumptions, every listed form
// named in its row.
type Row struct {
	ID string
	// Claims is false for the rows without a claim side (they read, mark or
	// refuse only: PR-B2, PR-B4, PR-A4, PR-R2) and for D, the single
	// default branch every unlisted form goes to.
	Claims bool
	// Assumptions are the environment assumptions (PR-V1 ... PR-V6) of
	// the row's assumptions column; PR-V6 for every row with a mapped
	// fact.
	Assumptions []string
	// Forms lists the forms of a row that names several; each claim
	// branch tags "<row>/<form>" from step G1B2 on, and the fixture must
	// reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "PR-": B blocks, F field
// types and modifiers, A field attributes, AA block attributes, R
// relations, E enums, P provable validation failures; D for the default
// branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("PR-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 25 rows plus D.
var Contract = []Row{
	{ID: "PR-B1", Claims: true, Assumptions: []string{"PR-V3"}, Forms: []string{"postgresql", "mysql", "sqlite", "sqlserver", "cockroachdb"}},
	{ID: "PR-B2"},
	{ID: "PR-B3", Claims: true, Assumptions: []string{"PR-V2", "PR-V5"}},
	{ID: "PR-B4", Forms: []string{"view", "type"}},
	{ID: "PR-F1", Claims: true, Assumptions: []string{"PR-V1", "PR-V6"}, Forms: []string{"String", "Int", "BigInt", "Float", "Decimal", "DateTime", "Boolean", "Json", "Bytes"}},
	{ID: "PR-F2", Claims: true, Assumptions: []string{"PR-V1", "PR-V6"}, Forms: []string{"none", "?", "[]"}},
	{ID: "PR-F3", Claims: true},
	{ID: "PR-F4", Claims: true, Assumptions: []string{"PR-V6"}},
	{ID: "PR-A1", Claims: true},
	{ID: "PR-A2", Claims: true, Assumptions: []string{"PR-V6"}, Forms: UniqueKinds},
	{ID: "PR-A3", Claims: true, Assumptions: []string{"PR-V1", "PR-V6"}, Forms: []string{"(a)", "(b)", "(c)", "(d)", "(e)"}},
	{ID: "PR-A4"},
	{ID: "PR-A5", Claims: true},
	{ID: "PR-A6", Claims: true, Assumptions: []string{"PR-V1"}, Forms: NativeTypes},
	{ID: "PR-A7", Claims: true},
	{ID: "PR-AA1", Claims: true},
	{ID: "PR-AA2", Claims: true, Assumptions: []string{"PR-V6"}, Forms: UniqueKinds},
	{ID: "PR-AA3", Claims: true, Forms: IndexTypes},
	{ID: "PR-AA4", Claims: true},
	{ID: "PR-AA5", Claims: true},
	{ID: "PR-R1", Claims: true, Assumptions: []string{"PR-V2", "PR-V6"}},
	{ID: "PR-R2"},
	{ID: "PR-R3", Claims: true, Assumptions: []string{"PR-V2", "PR-V5", "PR-V6"}, Forms: []string{"mapped index", "constraint"}},
	{ID: "PR-E1", Claims: true},
	{ID: "PR-P1", Claims: true, Assumptions: []string{"PR-V2"}},
	{ID: "D"},
}

// NativeTypes are the postgresql native types of row PR-A6 (`@db.<Native>`).
var NativeTypes = []string{
	"VarChar", "Char", "Text", "Uuid", "SmallInt", "Integer", "BigInt", "Real", "DoublePrecision", "Decimal",
	"Timestamp", "Timestamptz", "Date", "Time", "Timetz", "Boolean", "Json", "JsonB", "ByteA", "Inet",
	"Xml", "Bit", "VarBit", "Oid", "Citext", "Money",
}

// UniqueKinds are the forms of rows PR-A2 and PR-AA2: where the database
// holds a unique index Prisma derives (uniqueKind): an index on
// postgresql and sqlite (oracle), an index on mysql and cockroachdb
// (mapped), a unique constraint on sqlserver (mapped).
var UniqueKinds = []string{"index", "mapped index", "constraint"}

// IndexTypes are the access methods of row PR-AA3 (`@@index(type: ...)`).
var IndexTypes = []string{"BTree", "Hash", "Gist", "Gin", "SpGist", "Brin"}

// Assumptions are the environment assumptions of the contract
// (docs/extract/schema-prisma.md "Assumptions"); PR-V6 is stated only by
// facts with verification "mapped".
var Assumptions = []string{"PR-V1", "PR-V2", "PR-V3", "PR-V4", "PR-V5", "PR-V6"}

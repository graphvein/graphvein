// Package drizzle is the Drizzle front end of the model kit (T018g2, ADR
// 0059; docs/extract/schema-drizzle.md). It reads Drizzle schema modules
// from the cached TypeScript model payload (typescript.ModelSyntax) and the
// retained package.json, lockfile and drizzle-kit config sources, never
// re-parsing a file at link time, and hands the tables it may claim to the
// tool-neutral kit in internal/extract/models.
//
// Discovery (Discover: packages, pins, configs, schema files and the
// modules their exports reach, markers) and the claim contract table
// below come from step G2B2; the claim pass (Link) for tables and columns
// from step G2B3, for keys, the extra config and relations from step
// G2B4 (keys.go, extra.go, relations.go). The catalogue (catalogue.go)
// registers the contract for the schema API.
package drizzle

// Tool is the model group's tool name: group keys are
// "models/drizzle/<root>".
const Tool = "drizzle"

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows have a claim branch (the ten claim-side rows).
	SideClaim = "claim"
	// SideRefusal rows only refuse: DR-P1 turns provable failures into a
	// diagnostic and withholds the tables involved.
	SideRefusal = "refusal"
	// SideNone rows have no branch: DR-P2 and DR-V exist so the inventory
	// can count scope forms and the contract test can name the assumption
	// row.
	SideNone = "none"
	// SideDefault is D, the single default branch every unlisted form goes
	// to.
	SideDefault = "default"
)

// Row is one row of the Drizzle claim contract (docs/extract/schema-drizzle.md
// "Claim contract", copied from docs/contract/claims/drizzle-models.md). The
// docs hold each row's form, claim, refusal and assumptions; this table
// holds what the code needs, and TestDrizzleContract keeps the two 1:1: the
// same ids in the same order, the same assumptions, every listed form named
// in its row.
type Row struct {
	ID string
	// Side is SideClaim, SideRefusal, SideNone or SideDefault.
	Side string
	// Assumptions are the environment assumptions (DR-V1 ... DR-V6) of the
	// row's assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "DR-": K scope and naming, T
// tables, C columns and modifiers, X table extra config, E enums, R
// relations, P provable failures, V the assumption row; D for the default
// branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("DR-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 13 rows plus D.
var Contract = []Row{
	{ID: "DR-K1", Side: SideClaim, Assumptions: []string{"DR-V1", "DR-V4", "DR-V5"}, Forms: ScopeForms},
	{ID: "DR-K2", Side: SideClaim, Assumptions: []string{"DR-V3"}},
	{ID: "DR-T1", Side: SideClaim, Assumptions: []string{"DR-V2"}, Forms: []string{"pgTable", "sqliteTable", "mysqlTable", "<s>.table"}},
	{ID: "DR-C1", Side: SideClaim, Assumptions: []string{"DR-V1"}},
	{ID: "DR-C2", Side: SideClaim, Assumptions: []string{"DR-V2"}},
	{ID: "DR-C3", Side: SideClaim},
	{ID: "DR-K3", Side: SideClaim, Assumptions: []string{"DR-V3"}},
	{ID: "DR-X1", Side: SideClaim, Assumptions: []string{"DR-V2"}, Forms: []string{"index", "uniqueIndex", "primaryKey", "unique", "foreignKey", "check"}},
	{ID: "DR-E1", Side: SideClaim},
	{ID: "DR-R1", Side: SideClaim, Forms: []string{"one", "many"}},
	{ID: "DR-P1", Side: SideRefusal, Assumptions: []string{"DR-V2"}},
	{ID: "DR-P2", Side: SideNone, Assumptions: []string{"DR-V5"}},
	{ID: "DR-V", Side: SideNone},
	{ID: "D", Side: SideDefault},
}

// ScopeForms are the forms of row DR-K1 that discovery tags: the package
// dependency, the two ways a config is named, the literal config keys, the
// three schema shapes and the three ways a schema module's exports reach
// another module.
var ScopeForms = []string{
	"package dependency", "default", "`--config`", "literal `dialect`",
	"path", "glob", "array", "`casing`", "`schemaFilter`",
	"`export *`", "`export { } from`", "tsconfig `paths`",
}

// Assumptions are the environment assumptions of the contract
// (docs/extract/schema-drizzle.md "Assumptions"); DR-V6 is stated only by
// facts with verification "mapped" (MySQL).
var Assumptions = []string{"DR-V1", "DR-V2", "DR-V3", "DR-V4", "DR-V5", "DR-V6"}

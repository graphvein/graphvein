// Package gorm is the GORM v2 front end of the model kit (T018g5, ADR 0063;
// docs/extract/schema-gorm.md). It reads Go modules that require
// gorm.io/gorm from the cached Go model payload (facts.GoFacts version 1,
// docs/lang/go-models.md) and the retained go.mod sources, never re-parsing
// a file at link time, and will hand the tables it may claim to the
// tool-neutral kit in internal/extract/models.
//
// Step GB2 provides discovery (Discover: modules and pins, the gorm.Open
// sites with their dialect and configuration, the migrate sites with the
// receiver proof and the order rule, the models the sites list, and the
// markers for what is found but not read) and the claim contract table
// below. Step GB3 provides the claim pass's schema port (Link: names,
// fields, types, column facts, indexes, per-dialect rendering); step GB4
// adds relations, the migration order, the call failures and the indexer
// call.
package gorm

// Tool is the model group's tool name: group keys are
// "models/gorm/<module dir>".
const Tool = "gorm"

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows have a claim branch (the seventeen claim-side rows).
	SideClaim = "claim"
	// SideRefusal rows only refuse: GO-P1 turns provable migrate failures
	// into a diagnostic and withholds the models involved.
	SideRefusal = "refusal"
	// SideMarker rows only mark: GO-M2 lists a model that no read migrate
	// call reaches, GO-U1 a GORM v1 import; neither claims anything.
	SideMarker = "marker"
	// SideNone rows have no branch: GO-V exists so the contract test can
	// name the assumption row.
	SideNone = "none"
	// SideDefault is D, the single default branch every unlisted form goes
	// to.
	SideDefault = "default"
)

// Row is one row of the GORM claim contract (docs/extract/schema-gorm.md
// "Claim contract", copied from docs/contract/claims/gorm-models.md). The docs
// hold each row's form, claim, refusal and assumptions; this table holds
// what the code needs, and TestGormContract keeps the two 1:1: the same ids
// in the same order, the same assumptions, every listed form named in its
// row.
type Row struct {
	ID string
	// Side is SideClaim, SideRefusal, SideMarker, SideNone or SideDefault.
	Side string
	// Assumptions are the environment assumptions (GO-V1 ... GO-V7) of the
	// row's assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "GO-": K scope and naming, M
// models, F fields, T types, C column facts, I indexes, R relations, P
// provable failures, U unread tool, V the assumption row; D for the default
// branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("GO-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 21 rows plus D.
var Contract = []Row{
	{ID: "GO-K1", Side: SideClaim, Assumptions: []string{"GO-V1", "GO-V7"}},
	{ID: "GO-K2", Side: SideClaim, Assumptions: []string{"GO-V3", "GO-V4"}, Forms: HandleForms},
	{ID: "GO-K3", Side: SideClaim, Assumptions: []string{"GO-V2", "GO-V6"}, Forms: MigrateForms},
	{ID: "GO-K4", Side: SideClaim, Assumptions: []string{"GO-V1", "GO-V4"}},
	{ID: "GO-M1", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-M2", Side: SideMarker},
	{ID: "GO-F1", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-F2", Side: SideClaim, Assumptions: []string{"GO-V1"}},
	{ID: "GO-T1", Side: SideClaim, Assumptions: []string{"GO-V1"}},
	{ID: "GO-T2", Side: SideClaim, Assumptions: []string{"GO-V1", "GO-V3", "GO-V5"}},
	{ID: "GO-C1", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-C2", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-C3", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-C4", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-I1", Side: SideClaim, Assumptions: []string{"GO-V2"}},
	{ID: "GO-R1", Side: SideClaim, Assumptions: []string{"GO-V6"}},
	{ID: "GO-R2", Side: SideClaim, Assumptions: []string{"GO-V2", "GO-V6"}},
	{ID: "GO-R3", Side: SideClaim, Assumptions: []string{"GO-V2", "GO-V6"}},
	{ID: "GO-P1", Side: SideRefusal, Assumptions: []string{"GO-V2"}},
	{ID: "GO-U1", Side: SideMarker},
	{ID: "GO-V", Side: SideNone},
	{ID: "D", Side: SideDefault},
}

// HandleForms are the forms of row GO-K2 that discovery tags: the four
// supported dialectors, a dialector local (T018g5 Revision 2), the three
// configuration forms and the read keys.
var HandleForms = []string{
	"postgres", "sqlite", "glebarez sqlite", "mysql",
	"a `gorm.Dialector` local assigned dialectors",
	"config literal", "once-defined local", "no configuration argument",
	"`NamingStrategy` (literal)", "`DisableForeignKeyConstraintWhenMigrating`",
	"`IgnoreRelationshipsWhenMigrating`", "`mysql.New(mysql.Config{...})`",
}

// MigrateForms are the forms of row GO-K3 that discovery tags: the three
// migrate calls and the three argument forms.
var MigrateForms = []string{
	"`AutoMigrate`", "`Migrator().AutoMigrate`", "`Migrator().CreateTable`",
	"`new(T)`", "`&T{...}`", "`T{...}`",
}

// Assumptions are the environment assumptions of the contract
// (docs/extract/schema-gorm.md "Assumptions"); GO-V5 is stated only by
// facts with verification "mapped" (MySQL).
var Assumptions = []string{"GO-V1", "GO-V2", "GO-V3", "GO-V4", "GO-V5", "GO-V6", "GO-V7"}

// Package sequelize is the Sequelize v6 front end of the model kit (T018g3,
// ADR 0061; docs/extract/schema-sequelize.md). It reads Sequelize model,
// instance and options modules from the cached TypeScript model payload
// (typescript.ModelSyntax version 4) and the retained package.json,
// lockfile and configuration sources, never re-parsing a file at link
// time, and hands the tables it may claim to the tool-neutral kit in
// internal/extract/models.
//
// Step SQB2 provides discovery (Discover: packages, pins, drivers, the
// module-level `new Sequelize(...)` instances and their options, the
// dialect rule, the CommonJS reach for gated files, the models and
// association calls each instance reaches, and the markers for what is
// found but not read) and the claim contract table below. The claim pass
// (Link: records) comes from steps SQB3 (models, attributes and options)
// and SQB4 (references, associations, their failures and the cyclic
// rule).
package sequelize

// Tool is the model group's tool name: group keys are
// "models/sequelize/<instance file>".
const Tool = "sequelize"

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows have a claim branch (the eleven claim-side rows).
	SideClaim = "claim"
	// SideRefusal rows only refuse: SQ-P1 turns provable sync failures
	// into a diagnostic and withholds the models involved.
	SideRefusal = "refusal"
	// SideMarker rows only mark: SQ-U1 reports a migrations folder as
	// found, not read yet, and claims nothing.
	SideMarker = "marker"
	// SideNone rows have no branch: SQ-V exists so the contract test can
	// name the assumption row.
	SideNone = "none"
	// SideDefault is D, the single default branch every unlisted form goes
	// to.
	SideDefault = "default"
)

// Row is one row of the Sequelize claim contract
// (docs/extract/schema-sequelize.md "Claim contract", copied from
// docs/contract/claims/sequelize-models.md). The docs hold each row's form,
// claim, refusal and assumptions; this table holds what the code needs,
// and TestSequelizeContract keeps the two 1:1: the same ids in the same
// order, the same assumptions, every listed form named in its row.
type Row struct {
	ID string
	// Side is SideClaim, SideRefusal, SideMarker, SideNone or SideDefault.
	Side string
	// Assumptions are the environment assumptions (SQ-V1 ... SQ-V6) of the
	// row's assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "SQ-": K scope and naming, M
// model forms, A attributes, O model options, R associations, P provable
// failures, U unread tool folders, V the assumption row; D for the default
// branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("SQ-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 14 rows plus D.
var Contract = []Row{
	{ID: "SQ-K1", Side: SideClaim, Assumptions: []string{"SQ-V1", "SQ-V4", "SQ-V5"}, Forms: ScopeForms},
	{ID: "SQ-K2", Side: SideClaim, Assumptions: []string{"SQ-V1"}},
	{ID: "SQ-M1", Side: SideClaim, Assumptions: []string{"SQ-V2", "SQ-V3"}},
	{ID: "SQ-M2", Side: SideClaim, Assumptions: []string{"SQ-V2", "SQ-V3"}},
	{ID: "SQ-A1", Side: SideClaim, Assumptions: []string{"SQ-V1", "SQ-V3"}, Forms: []string{"helper member"}},
	{ID: "SQ-A2", Side: SideClaim, Assumptions: []string{"SQ-V2"}},
	{ID: "SQ-A3", Side: SideClaim, Assumptions: []string{"SQ-V2"}},
	{ID: "SQ-O1", Side: SideClaim, Assumptions: []string{"SQ-V5"}},
	{ID: "SQ-O2", Side: SideClaim, Assumptions: []string{"SQ-V2"}},
	{ID: "SQ-R1", Side: SideClaim, Assumptions: []string{"SQ-V2"}, Forms: []string{"belongsTo", "hasOne", "hasMany"}},
	{ID: "SQ-R2", Side: SideClaim, Assumptions: []string{"SQ-V2"}},
	{ID: "SQ-P1", Side: SideRefusal, Assumptions: []string{"SQ-V3"}},
	{ID: "SQ-U1", Side: SideMarker},
	{ID: "SQ-V", Side: SideNone},
	{ID: "D", Side: SideDefault},
}

// ScopeForms are the forms of row SQ-K1 that discovery tags: the package
// dependency, the lockfile pins, the instance, the two dialect rules, the
// literal options, the constructor options spread read under SQ-V5 (open
// point O1), the two ways a model reaches the instance and the two ways
// an association call is reached, and the sequelize-cli forms (Revision 3:
// its loop, a class's static associate, a required factory call, models
// object assignments and the required environment options).
var ScopeForms = []string{
	"package dependency", "lockfile pins", "`new Sequelize(...)`",
	"literal `dialect`", "single-driver rule", "`define`", "`schema`",
	"a spread", "imported instance binding", "factory's first parameter",
	"module level", "`associate(models)` loop",
	// Revision 3: the sequelize-cli forms.
	"sequelize-cli loop", "`static associate(models)`", "`require(...)(...)` factory call",
	"models object assignments", "`require(config)[env]`",
}

// Assumptions are the environment assumptions of the contract
// (docs/extract/schema-sequelize.md "Assumptions"); SQ-V6 is stated only
// by facts with verification "mapped" (MySQL, MariaDB, MSSQL).
var Assumptions = []string{"SQ-V1", "SQ-V2", "SQ-V3", "SQ-V4", "SQ-V5", "SQ-V6"}

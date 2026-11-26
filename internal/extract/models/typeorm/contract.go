// Package typeorm is the TypeORM front end of the model kit (T018g6, ADR
// 0064; docs/extract/schema-typeorm.md). It reads TypeORM 0.3.x and 1.x
// data sources, NestJS TypeOrmModule options and the entity classes they
// reach from the cached TypeScript model payload (typescript.ModelSyntax
// version 5) and the retained package.json, lockfile and tsconfig
// sources, never re-parsing a file at link time, and will hand the tables
// it may claim to the tool-neutral kit in internal/extract/models.
//
// Step TOB2 provides discovery (Discover: packages, pins and major lines,
// the module-level `new DataSource(...)` data sources with their
// candidate options, ormconfig files, the NestJS forRoot, forRootAsync,
// forFeature and registerAs forms, the dialect set, the naming strategy,
// the entity classes each group reaches through class references, globs
// and forFeature, the compiler flags of their files, the migrations
// option, and the markers for what is found but not read) and the claim
// contract table below. The claim pass (records) comes from steps TOB3
// and TOB4.
package typeorm

// Tool is the model group's tool name: group keys are
// "models/typeorm/<data source file>".
const Tool = "typeorm"

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows have a claim branch (the twenty-three claim-side
	// rows; TO-K7 and TO-K8 widen which options TO-K1 reads).
	SideClaim = "claim"
	// SideNoFact rows read a form as having no table fact: TO-E3's
	// decorator packages and TO-R3's inverse relation sides (counted,
	// never claimed).
	SideNoFact = "no-fact"
	// SideRefusal rows only refuse: TO-P1 turns provable initialize or
	// synchronize failures into a diagnostic, TO-P2 refuses project
	// decorators and helpers.
	SideRefusal = "refusal"
	// SideMarker rows only mark: TO-U1 reports the migrations option as
	// found, not read yet.
	SideMarker = "marker"
	// SideNone rows have no branch: TO-V exists so the contract test can
	// name the assumption row.
	SideNone = "none"
	// SideDefault is D, the single default branch every unlisted form goes
	// to.
	SideDefault = "default"
)

// Row is one row of the TypeORM claim contract
// (docs/extract/schema-typeorm.md "Claim contract", copied from
// docs/contract/claims/typeorm-models.md). The docs hold each row's form,
// claim, refusal and assumptions; this table holds what the code needs,
// and TestTypeORMContract keeps the two 1:1: the same ids in the same
// order, the same assumptions, every listed form named in its row.
type Row struct {
	ID string
	// Side is one of the Side constants.
	Side string
	// Assumptions are the environment assumptions (TO-V1 ... TO-V10) of
	// the row's assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "TO-": K scope and naming, E
// entity forms, C columns, X indexes and constraints, R relations, P
// provable failures and refusals, U unread tool folders, V the assumption
// row; D for the default branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("TO-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 29 rows plus D.
var Contract = []Row{
	{ID: "TO-K1", Side: SideClaim, Assumptions: []string{"TO-V1", "TO-V3", "TO-V4", "TO-V5", "TO-V9"}, Forms: ScopeForms},
	{ID: "TO-K2", Side: SideClaim, Assumptions: []string{"TO-V1", "TO-V9"}},
	{ID: "TO-K3", Side: SideClaim, Assumptions: []string{"TO-V1", "TO-V3", "TO-V4", "TO-V5"}, Forms: NestRootForms},
	{ID: "TO-K4", Side: SideClaim, Assumptions: []string{"TO-V5"}, Forms: []string{"`TypeOrmModule.forFeature(", "`autoLoadEntities: true`"}},
	{ID: "TO-K5", Side: SideClaim, Assumptions: []string{"TO-V8"}},
	{ID: "TO-K6", Side: SideClaim, Assumptions: []string{"TO-V1", "TO-V9", "TO-V10"}},
	{ID: "TO-K7", Side: SideClaim, Assumptions: []string{"TO-V5"}, Forms: []string{"`new DataSource(X)`", "`TypeOrmModule.forRoot(X)`"}},
	{ID: "TO-K8", Side: SideClaim, Assumptions: []string{"TO-V5"}},
	{ID: "TO-E1", Side: SideClaim, Assumptions: []string{"TO-V2", "TO-V5"}},
	{ID: "TO-E2", Side: SideClaim, Assumptions: []string{"TO-V5"}},
	{ID: "TO-E3", Side: SideNoFact, Assumptions: []string{"TO-V5"}},
	{ID: "TO-C1", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-C2", Side: SideClaim, Assumptions: []string{"TO-V3", "TO-V6"}},
	{ID: "TO-C3", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-C4", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-C5", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-C6", Side: SideClaim, Assumptions: []string{"TO-V2", "TO-V5"}},
	{ID: "TO-X1", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-X2", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-X3", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-X4", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-R1", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-R2", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-R3", Side: SideNoFact},
	{ID: "TO-R4", Side: SideClaim, Assumptions: []string{"TO-V2"}},
	{ID: "TO-P1", Side: SideRefusal, Assumptions: []string{"TO-V2", "TO-V3"}, Forms: FailureForms},
	{ID: "TO-P2", Side: SideRefusal},
	{ID: "TO-U1", Side: SideMarker},
	{ID: "TO-V", Side: SideNone},
	{ID: "D", Side: SideDefault},
}

// ScopeForms are the forms of row TO-K1 that discovery tags: the package
// dependency and its lockfile pins, the data source and its candidate
// rule, ormconfig, the two kinds of `entities` element, the dialect set,
// the compiler flags, and the `path` helpers imported by name.
var ScopeForms = []string{
	"package dependency", "lockfile pins", "`new DataSource(<options>)`", "candidate rule",
	"ormconfig", "class references", "the glob subset", "the dialect set", "tsconfig decorator flags",
	"`join(...)`/`resolve(...)` imported by name",
}

// NestRootForms are the forms of row TO-K3: the two root calls, the
// default data source factory, the 12.x data source name argument and the
// try statements of a useFactory block (T018g6 Revision 3 addendum 2).
var NestRootForms = []string{
	"`TypeOrmModule.forRoot(<options>)`", "`TypeOrmModule.forRootAsync(", "`dataSourceFactory`", "`forRoot(options, name)`", "`try`",
}

// FailureForms are TO-P1's numbered failures; discovery reaches (7), the
// claim pass the others.
var FailureForms = []string{"(1)", "(2)", "(3)", "(4)", "(5)", "(6)", "(7)", "(8)", "(9)", "(10)", "(11)", "(12)", "(13)", "(14)"}

// Assumptions are the environment assumptions of the contract in the
// docs' order (docs/extract/schema-typeorm.md "Assumptions"); TO-V6 is
// stated only by facts with verification "mapped" (MySQL, MariaDB, MSSQL,
// the SQLite-family drivers other than sqlite and better-sqlite3), TO-V8
// only with TO-K5 and TO-V10 only with TO-K6.
var Assumptions = []string{"TO-V1", "TO-V2", "TO-V3", "TO-V4", "TO-V5", "TO-V6", "TO-V7", "TO-V9", "TO-V10", "TO-V8"}

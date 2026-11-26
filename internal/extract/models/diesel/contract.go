package diesel

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows have a claim branch: DI-K1, T1, C1, C2, S1, J1 and,
	// since Revision 1 O2, DI-U2 (names only).
	SideClaim = "claim"
	// SideSelect rows decide which other rows' claims apply and claim
	// nothing themselves: DI-G1, the provenance of each line.
	SideSelect = "select"
	// SideNoFact rows are read so the contract test reaches the form, and
	// never yield a fact: DI-J2.
	SideNoFact = "nofact"
	// SideRefusal rows only refuse: DI-P1 turns the failures the Diesel
	// macros prove into a diagnostic and withholds the tables involved.
	SideRefusal = "refusal"
	// SideMarker rows only mark: DI-U1 reports the migrations directory
	// as found, not read yet.
	SideMarker = "marker"
	// SideNone rows have no branch: DI-V exists so the contract test can
	// name the assumption row.
	SideNone = "none"
	// SideDefault is D, the single default branch every unlisted form goes
	// to.
	SideDefault = "default"
)

// Row is one row of the Diesel claim contract
// (docs/extract/schema-diesel.md "Claim contract", copied from
// docs/contract/claims/diesel-models.md). The docs hold each row's form,
// claim, refusal and assumptions; this table holds what the code needs,
// and TestDieselContract keeps the two 1:1: the same ids in the same
// order, the same assumptions, every listed form named in its row.
type Row struct {
	ID string
	// Side is one of the Side constants.
	Side string
	// Assumptions are the environment assumptions (DI-V1 ... DI-V9) of
	// the row's assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixture must reach every form.
	Forms []string
}

// Kind is the row's kind, the letters after "DI-": K scope, G provenance,
// T tables, C columns, S custom SQL types, J joins, P provable failures,
// U unread or unconfigured, V the assumption row; D for the default
// branch.
func (r Row) Kind() string {
	if r.ID == "D" {
		return "D"
	}
	k := r.ID[len("DI-"):]
	for i, c := range k {
		if c >= '0' && c <= '9' {
			return k[:i]
		}
	}
	return k
}

// Contract is the claim contract in the docs' order: 12 rows plus D.
var Contract = []Row{
	{ID: "DI-K1", Side: SideClaim, Assumptions: []string{AssumePins, AssumeDialect, AssumeSettings}},
	{ID: "DI-G1", Side: SideSelect, Assumptions: []string{AssumeGenerated, AssumeCLI, AssumeNewerPin, AssumeOlderPin}},
	{ID: "DI-T1", Side: SideClaim, Assumptions: []string{AssumeGenerated, AssumeSettings}, Forms: []string{"table!", "diesel::table!", "::diesel::table!", "pub mod <ident> { ... }", "use", `#[sql_name = "<s>"]`, "schema.name", "r#", "()"}},
	{ID: "DI-C1", Side: SideClaim, Assumptions: []string{AssumeGenerated}, Forms: []string{`#[sql_name = "<s>"]`, "#[max_length = <int>]", "Nullable<...>"}},
	{ID: "DI-C2", Side: SideClaim, Assumptions: []string{AssumeModifiers, AssumeDialect}, Forms: []string{"Array<T>", "#[max_length = n]", "Numeric", "Range<T>"}},
	{ID: "DI-S1", Side: SideClaim, Assumptions: []string{AssumeGenerated}, Forms: []string{"use super::sql_types::X", "use super::sql_types::*", "postgres_type(oid = ..., array_oid = ...)", "mysql_type", "sqlite_type", "#[diesel(enum_type)]", "pub mod rust_types { ... }"}},
	{ID: "DI-J1", Side: SideClaim, Assumptions: []string{AssumeGenerated}, Forms: []string{"joinable!(child -> parent (column));", "diesel::joinable!(...)", "a::b"}},
	{ID: "DI-J2", Side: SideNoFact},
	{ID: "DI-P1", Side: SideRefusal, Forms: FailureForms},
	{ID: "DI-U1", Side: SideMarker},
	{ID: "DI-U2", Side: SideClaim, Assumptions: []string{AssumePins, AssumeDialect, AssumeUnconfigured}},
	{ID: "DI-V", Side: SideNone},
	{ID: "D", Side: SideDefault},
}

// FailureForms are the DI-P1 forms ReadFile tags ("DI-P1/<form>"), one
// per compile-time failure the item reader proves.
var FailureForms = []string{
	FailSyntax, FailColumns, FailNoKey, FailColumnLikeName,
	FailKeyNotColumn, FailDuplicateCol, FailDuplicateTable,
}

// Assumptions are the environment assumptions of the contract
// (docs/extract/schema-diesel.md "Assumptions"), in order.
var Assumptions = []string{
	AssumePins, AssumeGenerated, AssumeModifiers, AssumeDialect,
	AssumeSettings, AssumeCLI, AssumeUnconfigured, AssumeNewerPin,
	AssumeOlderPin,
}

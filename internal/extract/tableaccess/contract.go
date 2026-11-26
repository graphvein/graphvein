// Package tableaccess links code to the tables it reads and writes (T018h,
// ADR 0060; docs/extract/table-access.md). It reads cached facts only (the
// Rust sqlx query payload, and from later steps the Python, Prisma and
// Drizzle payloads) and the schema records the migration and model front
// ends emitted, and it emits:
//
//   - reads_table / writes_table edges, one per (kind, owner, table), plain
//     contract v1 edges whose evidence lists every site (W2);
//   - on each owner node, the attribute table_access (one entry per edge
//     with tool, rows, operations, assumes, role and verification;
//     Revision 2) and table_access_unknown (one entry per site a row
//     recognises but cannot bind, W5).
//
// Every claim comes from exactly one row of the claim contract below; a
// form no row covers goes to the single default branch D. The edges are a
// lower bound: triggers, views, cascades and functions are never claimed.
package tableaccess

// Row sides: what a contract row's branch does.
const (
	// SideClaim rows claim edges from a carrier form (CT-P, CT-R, CT-Z,
	// CT-S).
	SideClaim = "claim"
	// SideReader rows have no branch of their own: the SQL subset reader
	// (CT-Q1) and its name resolution (CT-Q2) are reached through the SQL
	// carriers and tagged alongside them.
	SideReader = "reader"
	// SideDefault is D, the single default branch every unlisted form goes
	// to; it claims nothing.
	SideDefault = "default"
)

// Verification labels (Revision 1, O6): oracle only for forms an oracle
// compared, mapped for every other claimed form.
const (
	VerificationOracle = "oracle"
	VerificationMapped = "mapped"
)

// Row is one row of the claim contract (docs/extract/table-access.md
// "Claim contract", copied from docs/contract/claims/table-access.md).
// TestTableAccessContract keeps the three 1:1: the same ids in the same
// order, the same assumptions, every listed form named in its row.
type Row struct {
	ID string
	// Side is SideClaim, SideReader or SideDefault.
	Side string
	// Tool is the edge attribute tool a claim of the row carries ("" for the
	// reader rows and D).
	Tool string
	// Assumptions are the row's TA-V ids, from its assumptions column.
	Assumptions []string
	// Forms lists the forms of a row that names several; a branch tags
	// "<row>/<form>" and the fixtures must reach every form.
	Forms []string
	// Branch is the function holding the row's branch, "" while the step
	// that implements the row (Unit) has not landed.
	Branch string
	// Unit is the T018h implementation step that implements the row.
	Unit string
	// Verification is the label the row's claims carry: oracle only when an
	// oracle compares the row's forms (TestTableAccessClaims checks that the
	// oracle fixture reaches every form of such a row).
	Verification string
}

// Contract is the claim contract in the docs' order: 16 rows plus D.
var Contract = []Row{
	{ID: "CT-P1", Side: SideClaim, Tool: "sqlmodel", Assumptions: []string{"TA-V2"}, Forms: PythonSelectForms, Branch: "claimPythonSelect", Unit: "H-B4", Verification: VerificationOracle},
	{ID: "CT-P2", Side: SideClaim, Tool: "sqlmodel", Assumptions: []string{"TA-V2"}, Forms: PythonWriteForms, Branch: "claimPythonWrite", Unit: "H-B4", Verification: VerificationOracle},
	{ID: "CT-P3", Side: SideClaim, Tool: "sqlmodel", Assumptions: []string{"TA-V2", "TA-V3c"}, Forms: PythonGetForms, Branch: "claimPythonGet", Unit: "H-B4", Verification: VerificationMapped},
	{ID: "CT-P4", Side: SideClaim, Tool: "sqlmodel", Assumptions: []string{"TA-V2", "TA-V3c"}, Forms: PythonObjectForms, Branch: "claimPythonObject", Unit: "H-B4", Verification: VerificationMapped},
	{ID: "CT-P5", Side: SideClaim, Tool: "sql", Assumptions: []string{"TA-V1", "TA-V4"}, Branch: "claimPythonText", Unit: "H-B4", Verification: VerificationOracle},
	{ID: "CT-R1", Side: SideClaim, Tool: "prisma", Assumptions: []string{"TA-V2", "TA-V3a"}, Forms: PrismaDelegateForms, Branch: "claimPrismaDelegate", Unit: "H-B5", Verification: VerificationMapped},
	{ID: "CT-R2", Side: SideClaim, Tool: "prisma", Assumptions: []string{"TA-V2", "TA-V3a"}, Forms: PrismaRelationForms, Branch: "claimPrismaRelations", Unit: "H-B5", Verification: VerificationMapped},
	{ID: "CT-R3", Side: SideClaim, Tool: "sql", Assumptions: []string{"TA-V1", "TA-V3a", "TA-V4"}, Forms: PrismaRawForms, Branch: "claimPrismaRaw", Unit: "H-B5", Verification: VerificationOracle},
	{ID: "CT-Z1", Side: SideClaim, Tool: "drizzle", Assumptions: []string{"TA-V2", "TA-V3b"}, Forms: DrizzleSelectForms, Branch: "claimDrizzleSelect", Unit: "H-B6", Verification: VerificationOracle},
	{ID: "CT-Z2", Side: SideClaim, Tool: "drizzle", Assumptions: []string{"TA-V2", "TA-V3b"}, Forms: DrizzleWriteForms, Branch: "claimDrizzleWrite", Unit: "H-B6", Verification: VerificationOracle},
	{ID: "CT-Z3", Side: SideClaim, Tool: "drizzle", Assumptions: []string{"TA-V2", "TA-V3b"}, Forms: DrizzleQueryForms, Branch: "claimDrizzleQuery", Unit: "H-B6", Verification: VerificationMapped},
	{ID: "CT-Z4", Side: SideClaim, Tool: "sql", Assumptions: []string{"TA-V1", "TA-V3b", "TA-V4"}, Forms: DrizzleSQLForms, Branch: "claimDrizzleSQL", Unit: "H-B6", Verification: VerificationOracle},
	{ID: "CT-S1", Side: SideClaim, Tool: "sqlx", Assumptions: []string{"TA-V1", "TA-V4"}, Forms: SqlxMacroForms, Branch: "claimSqlxMacro", Unit: "H-B3", Verification: VerificationOracle},
	{ID: "CT-S2", Side: SideClaim, Tool: "sqlx", Assumptions: []string{"TA-V1", "TA-V4"}, Forms: SqlxFunctionForms, Branch: "claimSqlxFunction", Unit: "H-B3", Verification: VerificationOracle},
	{ID: "CT-Q1", Side: SideReader, Assumptions: []string{"TA-V4"}, Forms: ReaderForms, Branch: "readSQL", Unit: "H-B3", Verification: VerificationOracle},
	{ID: "CT-Q2", Side: SideReader, Assumptions: []string{"TA-V4"}, Forms: ResolveForms, Branch: "resolveNames", Unit: "H-B3", Verification: VerificationOracle},
	{ID: "D", Side: SideDefault, Branch: "unknownExecution", Unit: "H-B3"},
}

// Assumptions are the environment assumptions in the docs' order.
var Assumptions = []string{"TA-V1", "TA-V2", "TA-V3a", "TA-V3b", "TA-V3c", "TA-V4"}

// PythonSelectForms are the CT-P1 entity and chain forms.
var PythonSelectForms = []string{"model class", "M.c", "col(M.c)", "aliased(M)", ".select_from", ".join", ".outerjoin", ".join_from"}

// PythonWriteForms are the CT-P2 statements and the upsert chain link.
var PythonWriteForms = []string{"insert", "update", "delete", "on_conflict_do_update"}

// PythonGetForms are the CT-P3 methods.
var PythonGetForms = []string{"get", "get_one"}

// PythonObjectForms are the CT-P4 methods and object proofs.
var PythonObjectForms = []string{"add", "delete", "merge", "refresh", "add_all", "M(...)", "M.model_validate", "M.from_orm"}

// PrismaDelegateForms are the CT-R1 operations and the dynamic delegate
// refusal.
var PrismaDelegateForms = []string{
	"findUnique", "findUniqueOrThrow", "findFirst", "findFirstOrThrow", "findMany", "count", "aggregate", "groupBy",
	"create", "createMany", "createManyAndReturn", "update", "updateMany", "updateManyAndReturn", "upsert", "delete", "deleteMany",
	"<r>[expr]",
}

// PrismaRelationForms are the CT-R2 positions, relation filters, nested
// writes, the write-side refusals and the spread refusal.
var PrismaRelationForms = []string{
	"include", "select", "some", "every", "none", "is", "isNot",
	"create", "createMany", "connectOrCreate", "upsert", "update", "updateMany", "delete", "deleteMany",
	"connect", "disconnect", "set", "Spread",
}

// PrismaRawForms are the CT-R3 methods and the fragment refusal.
var PrismaRawForms = []string{"$queryRaw", "$executeRaw", "$queryRawUnsafe", "$executeRawUnsafe", "Prisma.sql"}

// DrizzleSelectForms are the CT-Z1 builders, joins and table-position
// refusals. CT-Z1 and CT-Z2 claims are oracle only for postgresql groups
// (Revision 4): the toSQL oracle compares pg-core builders; a claim on a
// table of another dialect's group is mapped.
var DrizzleSelectForms = []string{
	"select", "selectDistinct", "selectDistinctOn", ".from(t)", "leftJoin", "innerJoin", "rightJoin", "fullJoin", "crossJoin", "Lateral", "$count",
	`.as("x")`, "$with", "a view", "a parameter",
}

// DrizzleWriteForms are the CT-Z2 builders and chain links.
var DrizzleWriteForms = []string{"insert", "update", "delete", ".from(u)", ".values", ".set", ".where", ".onConflictDoUpdate", ".onConflictDoNothing", ".returning", ".select(qb)"}

// DrizzleQueryForms are the CT-Z3 methods, with, and the partial forms.
var DrizzleQueryForms = []string{"findMany", "findFirst", "with", "orm_relations", "spread"}

// DrizzleSQLForms are the CT-Z4 carrier, the substitution positions and
// the sql_dynamic forms.
var DrizzleSQLForms = []string{".execute(", "value position", "table position", "sql.raw", "sql.identifier", "structural substitution"}

// SqlxMacroForms are the CT-S1 forms: the three macros, their _unchecked
// variants, and the two spellings (a sqlx:: path or a name a use sqlx::
// declaration imports).
var SqlxMacroForms = []string{"query!", "query_as!", "query_scalar!", "_unchecked", "sqlx::<m>!", "use sqlx::"}

// SqlxFunctionForms are the CT-S2 functions.
var SqlxFunctionForms = []string{"sqlx::query(", "query_as::<_, T>(", "query_scalar(", "query_with", "query_as_with"}

// ReaderForms are the CT-Q1 forms the reader tags: statement kinds,
// reference positions, and the clauses with a direction rule.
var ReaderForms = []string{
	"SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "writable CTEs",
	"FROM", "JOIN", "comma joins", "INSERT INTO t [(cols)]", "UPDATE t ... [FROM ...]", "DELETE FROM t [USING ...]",
	"subqueries", "ONLY", "alias", "ON CONFLICT ... DO NOTHING / DO UPDATE", "EXCLUDED", "RETURNING",
	"FOR UPDATE", "NO KEY UPDATE", "SHARE", "KEY SHARE", "unnest(...)",
}

// ResolveForms are the CT-Q2 forms: CTE shadowing, folding, quoting and
// qualification.
var ResolveForms = []string{"shadows", "folded to lower case", "unless quoted", "a qualified name"}

// Reason codes of table_access_unknown entries: a closed vocabulary
// (docs/extract/table-access.md "Unknown records"). An entry's reason is a
// code, optionally followed by ": " and a detail (the unresolved name, the
// form the reader refused).
const (
	ReasonNotLiteral         = "sql_not_literal"
	ReasonOutsideSubset      = "sql_outside_subset"
	ReasonNameUnresolved     = "sql_name_unresolved"
	ReasonAmbiguousGroup     = "ambiguous_group"
	ReasonNoSchemaSource     = "no_schema_source"
	ReasonStatementNotTraced = "statement_not_traced"
	ReasonObjectClass        = "object_class_unproven"
	ReasonRelationWriteSide  = "relation_write_side"
	ReasonSQLDynamic         = "sql_dynamic"
	ReasonCarrierUnmodelled  = "sql_carrier_unmodelled"
	ReasonEntityUnresolved   = "entity_unresolved"
	ReasonSitePartial        = "site_partial"
	// The Supabase rows' codes (T024c2).
	ReasonTableDynamic     = "table_dynamic"
	ReasonClientUnproven   = "client_unproven"
	ReasonEmbeddedResource = "embedded_resource"
	// T024 review: a Supabase query builder without a verb.
	ReasonBuilderUnread = "builder_unread"
)

// Reasons lists the vocabulary in the docs' order.
var Reasons = []string{
	ReasonNotLiteral, ReasonOutsideSubset, ReasonNameUnresolved, ReasonAmbiguousGroup,
	ReasonNoSchemaSource, ReasonStatementNotTraced, ReasonObjectClass, ReasonRelationWriteSide,
	ReasonSQLDynamic, ReasonCarrierUnmodelled, ReasonEntityUnresolved, ReasonSitePartial,
	ReasonTableDynamic, ReasonClientUnproven, ReasonEmbeddedResource, ReasonBuilderUnread,
}

// rowByID returns the contract row with the id; the ids are constants of
// this package, so a miss is a programming error caught by the tests.
func rowByID(id string) Row {
	for _, r := range Contract {
		if r.ID == id {
			return r
		}
	}
	return Row{ID: id}
}

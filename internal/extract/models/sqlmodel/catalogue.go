package sqlmodel

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the model contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A): the environment
// assumptions V1-V7 of docs/lang/python-models.md (V3 folded into V7, V5
// after v1 and not listed) and the rows of docs/extract/schema-models.md
// "Claim contract" with their claim branch and summary as written there
// (TestCatalogue checks both against the documents and modelClaimRows).

var environmentAssumptions = []models.Assumption{
	{ID: "V1", Title: "Library versions", Text: "SQLModel 0.0.39, SQLAlchemy 2.0.51, Pydantic 2.13.4 (pydantic-core 2.46.4).", FoldedInto: ""},
	{ID: "V2", Title: "Python", Text: "3.14 (fastapi requires >=3.14; langflow allows 3.10-3.14).", FoldedInto: ""},
	{ID: "V3", Title: "Import succeeds", Text: "A table claim means: importing the module defines this table in SQLModel.metadata with these facts.", FoldedInto: "V7"},
	{ID: "V4", Title: "Same metadata", Text: "Every SQLModel table class registers in the one default registry (sqlmodel/main.py 799-804) unless a registry= class keyword says otherwise (row C4, D).", FoldedInto: ""},
	{ID: "V6", Title: "Import-free mutations outside G1", Text: "Mutations of a model table that need no import (X1's set adds and attribute assignments) in a file that does not pass G1 are not captured; the table is assumed unmodified by them.", FoldedInto: ""},
	{ID: "V7", Title: "Import succeeds (assumption)", Text: "The model modules import successfully, as they do when the application runs (Revision 2, with rule P1 for failures the source proves).", FoldedInto: ""},
}

// Assumptions returns the model contract's environment assumptions in id
// order.
func Assumptions() []models.Assumption { return slices.Clone(environmentAssumptions) }

// TableAssumptions are the assumptions every model table states (Revision
// 3.3, O1) and every row's claims rest on.
func TableAssumptions() []string { return []string{"V1", "V4", "V6", "V7"} }

// contractRow is one row of the docs table: Branch is the function that
// holds its claim branch ("none" for a row without one), Summary its claim
// as the docs table summarises it.
type contractRow struct {
	ID, Branch, Summary string
}

var contractEntries = []contractRow{
	{"C1", "class", "a table for a `table=True` class whose direct bases are proven non-table"},
	{"C1b", "class", "no table"},
	{"C1c", "class", "none; `schema_model_flag_unknown`"},
	{"C2", "buildTable", "chain members contribute columns, nearest definition wins"},
	{"C3", "class", "none; `schema_model_not_claimed`"},
	{"C4", "class", "none (D)"},
	{"C5", "buildTable", "nothing; outside K-cfg the class is D"},
	{"C6", "buildTable", "columns and constraints partial; name only from the class's own `__tablename__`"},
	{"N1", "buildTable", "table name as written"},
	{"N2", "buildTable", "`Class.lower()` when absence is proven"},
	{"N3", "buildTable", "no table node; `schema_model_name_unknown`"},
	{"M0", "none", "nothing (not captured)"},
	{"M1", "buildTable", "nothing; outside K-dec the class is D"},
	{"M2", "fieldColumn", "a column from `Field(...)`"},
	{"M3", "fieldColumn", "a required column without default"},
	{"M4", "fieldColumn", "a column with a literal default"},
	{"M5", "fieldColumn", "no column (removes an inherited field)"},
	{"M6", "fieldColumn", "nothing"},
	{"M7", "buildTable", "nothing"},
	{"M8", "buildTable", "nothing (C5)"},
	{"M9", "buildTable", "nothing (C5)"},
	{"M10", "buildTable", "nothing; other keys make the class D"},
	{"M11", "buildTable", "table constraints and schema"},
	{"M12", "buildTable", "class D, the bound column unknown; a literal value is P1 (L7)"},
	{"A1", "annotation", "nullable"},
	{"A2", "annotation", "not nullable"},
	{"A3", "annotation", "nullability and type unknown"},
	{"A5", "annotation", "the alias read in its module"},
	{"A1m", "annotation", "non-plain metadata: column D"},
	{"F1", "fieldCall", "flags; absent: false"},
	{"F2", "fieldCall", "nullability"},
	{"F3", "fieldCall", "default absent or scalar"},
	{"F3b", "fieldCall", "default callable"},
	{"F4", "fieldCall", "foreign key (last-dot split)"},
	{"F5", "setType", "type from the annotation"},
	{"F6", "fieldCall", "type from `sa_type`"},
	{"F7", "fieldCall", "the column is the `Column(...)`"},
	{"F7r", "fieldCall", "P1 failure"},
	{"F8", "fieldCall", "column D"},
	{"F9", "fieldCall", "no fact; outside the signature P1"},
	{"K1", "columnCall", "column name and type"},
	{"K1b", "typeExpr", "type unknown; **(Rev 3.3)** form `alias` (a module-level name bound once to a T1 call followed only by `.with_variant(...)` calls): the column read, its type unknown with reason"},
	{"K2", "columnCall", "flags, nullable = not primary_key"},
	{"K3", "columnCall", "foreign key"},
	{"K4", "columnCall", "default, server default"},
	{"K5", "columnCall", "nothing"},
	{"K6", "columnCall", "column D"},
	{"T1", "typeExpr", "type class with length/timezone"},
	{"TA1", "tableArgs", "unique constraint"},
	{"TA2", "tableArgs", "index"},
	{"TA3", "tableArgs", "check constraint"},
	{"TA-D", "tableArgs", "constraint set, schema and primary key unknown"},
	{"U1", "class", "none; `schema_model_not_claimed` (also for a subclass of a declarative class)"},
	{"U2", "buildTable", "class or column D"},
	{"U3", "Link", "count in `schema_model_core_tables`"},
	{"U4", "Link", "count in `schema_model_core_tables`"},
	{"X1", "site", "set or facts not complete; **(Rev 3.1)** forms `PK` (primary key open), `FK` (foreign keys unknown), `replace` (columns unknown), `table_attr` (schema and listed elements unknown); **(Rev 3.2)** forms `table_identity` (an attribute assignment that may rename the table: `schema_model_not_claimed`, no table) and `column_identity` (one that may rename a column: no column node)"},
	{"U5", "site", "`ddl_hooks: true`"},
	{"I1", "none", "after v1 (V7, P1)"},
	{"I2", "none", "after v1"},
	{"D", "class", "none; partial with reason D"},
	{"L1", "resolve", "resolution through the Python resolver; **(Rev 3.3)** the K-dec builtins `classmethod`, `staticmethod`, `property` only when unbound"},
	{"L2", "class", "cross-file chain and base proof"},
	{"L3", "emit", "table and column nodes, contains edges"},
	{"L4", "resolveForeignKeys", "foreign_key edges"},
	{"L5", "compareMigrations", "`schema_model_migration_overlap`"},
	{"L6", "Link", "`schema_model_unread`, `schema_model_core_tables`"},
	{"L7", "buildTable", "`schema_model_import_failure`, no table (forms F7r, F4, F9, F5, primary key, M12)"},
	{"L8", "site", "site receivers, test scope"},
}

// Contract returns the model contract's rows in the docs table's order;
// every row's claims rest on TableAssumptions.
func Contract() []models.ContractEntry {
	out := make([]models.ContractEntry, 0, len(contractEntries))
	for _, r := range contractEntries {
		out = append(out, models.ContractEntry{ID: r.ID, Branch: r.Branch, Summary: r.Summary, Assumptions: TableAssumptions()})
	}
	return out
}

// Tool is the group tool of SQLModel tables.
const Tool = "sqlmodel"

func init() {
	models.Register(models.Catalogue{Tool: Tool, Assumptions: Assumptions(), Rows: Contract()})
}

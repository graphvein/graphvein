package gorm

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the GORM claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as the
// other model front ends do): the environment assumptions GO-V1 to GO-V7
// and the rows of docs/extract/schema-gorm.md "Claim contract" with their
// claim branch and claim cell as written there (TestGormCatalogue checks
// both against the document).

var environmentAssumptions = []models.Assumption{
	{ID: "GO-V1", Title: "Pins", Text: "The `gorm.io/gorm`, dialector driver and `github.com/jinzhu/inflection` versions in the module's `go.mod` are the ones that built the binary that migrated the database (`pins: {\"gorm.io/gorm\": \"<v>\", \"gorm.io/driver/<d>\": \"<v>\", ...}`), and gorm's DDL code is the same as at the verified pins for every version in the accepted range (Revision 1, O5: verified at 1.25.12 and 1.31.2; a newer v1 version is read under this assumption, shown as \"rules verified at gorm 1.25.12-1.31.2\"); a dialector driver pin outside the verified driver versions (postgres v1.5.11 and v1.6.3, sqlite v1.5.7 and v1.6.0, glebarez sqlite v1.11.0, mysql v1.6.0) is read under it too and shown with the verified versions."},
	{ID: "GO-V2", Title: "Schema deployed", Text: "The database was created by the migrate calls read, on an empty database, and every call succeeded (the application started), and no other code changed the schema: a read `Migrator()` DDL call or `Exec`/`Raw` with DDL text is marked (the tables it may change keep partial sets, the table list may be incomplete), while SQL the application builds at run time or a separate migration tool runs is outside the records. `AutoMigrate` on existing tables only adds columns, indexes and constraints; a database created by older versions of the application (gotify has kept one since 2018) can hold more."},
	{ID: "GO-V3", Title: "Dialect", Text: "The database is of the group's dialect: the single dialector every read `gorm.Open` site uses, or one of several chosen at runtime (then the dialect-dependent facts are claimed per dialect) (Revision 1 O1)."},
	{ID: "GO-V4", Title: "No runtime naming changes", Text: "No code changes the naming strategy, `inflection`'s rules or the configuration after `gorm.Open` beyond what is read (a read mutation makes the dependent facts unknown)."},
	{ID: "GO-V5", Title: "Mapped dialect", Text: "MySQL facts follow gorm.io/driver/mysql as its source states at the pinned version, for a MySQL server 5.6 or later (`SELECT VERSION()` switches datetime precision off for 5.0-5.5 and changes options for MariaDB, `mysql.go:128-161`); never `oracle`."},
	{ID: "GO-V6", Title: "Schema parse order", Text: "No code parses a model's schema (a query, `db.Model(...)`, another goroutine) before the migrate calls that the order rule reads, so GORM's schema cache holds what the migrate calls put there."},
	{ID: "GO-V7", Title: "Build", Text: "The packages compile as read: no build constraint, generated file or `vendor` copy changes the struct set or the migrate calls."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a branch) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"GO-K1": {"module", "the group (`models/gorm/<module dir>`), `pins`"},
	"GO-K2": {"openSite", "the group's `dialect` (single dialector) or runtime; the naming strategy; foreign keys on/off; join tables and auto-added targets on/off"},
	"GO-K3": {"reorder", "the model set (tables to claim), the migrate order, `table_list` complete only when every read site is fully read"},
	"GO-K4": {"tableName", "table `name`, column names, join-table, index and constraint names (`Person` -> `people`, `Human` -> `humen`, `Leaf` -> `leafs`, `HTTPLog` -> `http_logs`, `OIDCID` -> `o_id_c_id`)"},
	"GO-M1": {"table", "a table (identity `table:<file>#<Struct>`) with its column set"},
	"GO-M2": {"notProven", "none: listed as \"model, not proven migrated\" (marker diagnostic naming the struct)"},
	"GO-F1": {"fill", "the column set and order"},
	"GO-F2": {"parseField", "column `name`"},
	"GO-T1": {"column", "column `length`, `precision`, `scale`: the modifiers of the SQL type GO-T2 claims (per dialect in a runtime group); GORM's data type with its size, precision and scale is the unclaimed `tool_type`"},
	"GO-T2": {"render", "column `type`: the SQL type (per dialect in a runtime group)"},
	"GO-C1": {"table", "`primary_key` columns (in field order), autoincrement"},
	"GO-C2": {"column", "`nullable`"},
	"GO-C3": {"defaults", "default present or absent; literal text: postgres `7`, `1.5`, `true`, `'x'::text`; sqlite `7`, `\"x\"`, `true`, `null`; numeric literal text in every dialect; raw text: presence only"},
	"GO-C4": {"uniqueName", "a unique constraint (name on postgres and mysql; on sqlite the column set only) and `unique: true`"},
	"GO-I1": {"indexes", "index name, columns in order, unique, partial, descending flags; created after the table on postgres (`IF NOT EXISTS`) and sqlite, inline on mysql"},
	"GO-R1": {"guess", "the foreign-key column pairing and its type copy (GO-T1); has-relations register on the child (`relationship.go:101-106`) only when the parent's schema is parsed before the child's table is created: same `AutoMigrate` call (all values are parsed before any table is created), an earlier ordered call, or the parse closure of either (every relation field's struct is parsed)"},
	"GO-R2": {"many2many", "the join table (identity `table:<file>#<Struct>.<Field>`), its columns, types, foreign keys"},
	"GO-R3": {"foreignKeys", "`foreign_keys` with name, columns, target, referenced columns, actions; `foreign_keys: []` proven when the config disables them (`DisableForeignKeyConstraintWhenMigrating`, gotify) or every relation is read"},
	"GO-P1": {"run", "none for the failing model and every later model of the same call on that dialect (the call returns at the first error, `migrator.go:122-206`)"},
	"GO-U1": {"unreadV1", "marker (`unread_tools`: \"found, not read\")"},
	"GO-V":  {"none", "stated on the records"},
	"D":     {"notClaimed", "nothing; marker diagnostic `schema_model_unread` with `tool: \"gorm\"`"},
}

// Catalogue returns the GORM contract's catalogue: rows in the docs
// table's order, each with the assumptions of its row (Contract).
func Catalogue() models.Catalogue {
	c := models.Catalogue{Tool: Tool, Assumptions: slices.Clone(environmentAssumptions)}
	for _, r := range Contract {
		e := catalogueRows[r.ID]
		// A row without assumptions lists none ([]), never null.
		c.Rows = append(c.Rows, models.ContractEntry{ID: r.ID, Branch: e.Branch, Summary: e.Summary, Assumptions: append([]string{}, r.Assumptions...)})
	}
	return c
}

func init() {
	models.Register(Catalogue())
}

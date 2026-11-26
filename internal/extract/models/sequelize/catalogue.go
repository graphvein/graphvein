package sequelize

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the Sequelize claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as the
// Prisma and Drizzle front ends do): the environment assumptions SQ-V1 to
// SQ-V6 and the rows of docs/extract/schema-sequelize.md "Claim contract"
// with their claim branch and claim cell as written there
// (TestSequelizeCatalogue checks both against the document).

var environmentAssumptions = []models.Assumption{
	{ID: "SQ-V1", Title: "Pins", Text: "The `sequelize` and `inflection` versions in the package's lockfile are the ones that created the database (`pins: {\"sequelize\": \"<v>\", \"inflection\": \"<v>\"}`)."},
	{ID: "SQ-V2", Title: "Schema deployed", Text: "The database was created by the models' `sync` or by migrations that match the models (sequelize-cli or umzug migrations are not read). Gladys never calls `sync` (umzug migrations only); opencollective-api calls it in a dev setup script only."},
	{ID: "SQ-V3", Title: "Models load", Text: "Module side effects do not change the models beyond what is read, and values the rules do not read (ENUM label lists from imported constants) are valid on the dialect."},
	{ID: "SQ-V4", Title: "Dialect", Text: "The database is of the instance's literal `dialect`, or of the dialect the package's single installed Sequelize driver implies (open point O3)."},
	{ID: "SQ-V5", Title: "Global define", Text: "The instance has no `define` options beyond the ones read (open point O1)."},
	{ID: "SQ-V6", Title: "Mapped dialects", Text: "MySQL, MariaDB and MSSQL facts follow Sequelize's query generator as its source states at the pinned version (never `oracle`)."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a branch) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"SQ-K1": {"Discover", "the group (`models/sequelize/<instance file>`), its model set, `dialect`, `pins`, the global `define` options"},
	"SQ-K2": {"tableName", "table `name` and `schema`, column names (`Person` -> `People`, `Mouse` -> `Mice`, `Archived` -> `Archiveds`, `BlogPost` underscored -> `blog_posts`; freeze ignores `underscored`)"},
	"SQ-M1": {"readModel", "a model (table)"},
	"SQ-M2": {"initForm", "a model named by `modelName` or the class name"},
	"SQ-A1": {"readType", "column type as the catalog shows it: postgres `character varying(255)`, `timestamp with time zone`, `double precision` (FLOAT and DOUBLE), `numeric(p,s)`, `bytea`, `enum_<table>_<column>` (quoted when not lower case) and `[]` for arrays, `tstzrange`; sqlite `VARCHAR(255)`, `DATETIME`, `TINYINT(1)`, `DOUBLE PRECISION`, `TEXT` for ENUM, `UUID`, `JSON`; enum labels in order when every value is a string literal; `VIRTUAL`: no column; a helper member (`CustomDataTypes(DataTypes).currency`): the facts of the substituted value, with its verification"},
	"SQ-A2": {"columnFacts", "nullability: postgres not null when `allowNull: false` or a primary key; sqlite (`dialects/sqlite/query-generator.js:36-75`) a single primary key whose type contains `INT` becomes `INTEGER PRIMARY KEY` (nullable in the catalog, type `INTEGER`), composite primary-key members are not null, any other column only with `allowNull: false`; primary key columns; `autoIncrement` on postgres: `SERIAL`/`BIGSERIAL` (integer, `nextval` default); unique: single-column unique, or composite unique over the columns sharing a group name (the group name is not the constraint name); default: string, number, boolean literal (sqlite booleans `1`/`0`), `null` as no default, any literal on JSON/JSONB as `JSON.stringify` text, `sequelize.literal(\"...\")` / `fn(...)`: a database default is present (its text is not claimed: Postgres re-renders it, and the oracle compared presence only); `NOW`, `UUIDV4`, `UUIDV1`: no database default; `index: true` on JSONB: a `gin` index named `underscore(\"<table>_<column>\")` (`lib/model.js:883-889`)"},
	"SQ-A3": {"columnForeignKeys", "foreign key to that table and column (`key` absent: `id`), actions as written upper-cased, else `NO ACTION` (the database default; Sequelize writes no clause); on mssql (Revision 3) the actions its attributesToSQL keeps: the first references to a table loses `onUpdate`, a later one to the same table and a self-reference lose both (`NO ACTION`; `dialects/mssql/query-generator.js:488-494`, `:542-561`)"},
	"SQ-O1": {"defaultAttributes", "the `id`, `createdAt`, `updatedAt` (`DATE`, not null), `deletedAt` (`DATE`, nullable, with `paranoid`), `version` (`INTEGER` not null default `0`) columns, names through SQ-K2; a user attribute of the same name wins entirely"},
	"SQ-O2": {"indexes", "index name (omitted: `inflection.underscore(\"<table>_<fields>\")`, `lib/utils.js:413-421`), columns, unique, method (`using`), partial (`where` present)"},
	"SQ-R1": {"foreignKeySpec", "the foreign-key attribute on the holder (belongsTo: source; hasOne/hasMany: target): name `foreignKey`, else `camelize(as or B's singular + \"_\" + B's key)` (belongsTo), `camelize(A's singular + \"_\" + A's key)` (hasMany), `camelize(singularize(as or A's name) + \"_\" + A's key)` (hasOne uses `as`); a new attribute has the key's type and is nullable; merged into an existing attribute without overriding any value it already has, first association first; unless `constraints: false`, a foreign key with `onDelete` as written, else belongsTo `SET NULL` only when the attribute's `allowNull` is truthy and `NO ACTION` otherwise (an omitted `allowNull` gives `NO ACTION` on a nullable column), hasOne/hasMany `SET NULL` or `CASCADE` likewise; `onUpdate` as written, else `CASCADE` (`associations/belongs-to.js:77-80`, `has-many.js:103-106`, `has-one.js:78-81`); on mssql the actions as SQ-A3 states"},
	"SQ-R2": {"joinTable", "a join table named `through`: `createdAt`/`updatedAt` first (from the source's timestamps), then the two keys (`camelize(singular + \"_\" + key)` unless given), both not null, together the primary key, each a foreign key `CASCADE`/`CASCADE` to its model (`associations/belongs-to-many.js:186-247`)"},
	"SQ-P1": {"failure", "diagnostic `schema_model_import_failure` (row named); no claim for the models involved"},
	"SQ-U1": {"migrations", "`unread_tools` entry (found, not read yet)"},
	"SQ-V":  {"none", "stated, never claimed as facts"},
	"D":     {"notClaimed", "nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `sequelize`) for whole definitions"},
}

// Catalogue returns the Sequelize contract's catalogue: rows in the docs
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

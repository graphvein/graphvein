package drizzle

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the Drizzle claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as the
// Prisma front end does): the environment assumptions DR-V1 to DR-V6 and
// the rows of docs/extract/schema-drizzle.md "Claim contract" with their
// claim branch and claim cell as written there (TestDrizzleCatalogue
// checks both against the document).

var environmentAssumptions = []models.Assumption{
	{ID: "DR-V1", Title: "Pins", Text: "The drizzle-orm and drizzle-kit versions in the package's lockfile are the ones that created the database."},
	{ID: "DR-V2", Title: "Schema deployed", Text: "The database was created from this schema by `drizzle-kit push`, or by `generate` + `migrate` of migrations generated from it (the config's `out` folder is not read), so schema and database agree."},
	{ID: "DR-V3", Title: "Runtime casing", Text: "When a column name is derived from the config's `casing`, the application's `drizzle({ casing })` uses the same value."},
	{ID: "DR-V4", Title: "Dialect", Text: "The database is of the config's literal `dialect`, and the table builders come from that dialect's module."},
	{ID: "DR-V5", Title: "Working directory", Text: "drizzle-kit runs in the package directory (as the package's own scripts run it), so the config's `schema` paths and the default config file name resolve against that directory."},
	{ID: "DR-V6", Title: "Mapped MySQL", Text: "drizzle-kit's MySQL serializer behaves as its source states at the pinned version, and the server is MySQL 8.0.19 or later, whose `information_schema.COLUMNS.COLUMN_TYPE` reports the forms \"MySQL mapping\" lists (facts with `verification: \"mapped\"`: mysql)."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a branch) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"DR-K1": {"Discover", "the group (`models/drizzle/<config path>`), its file set, `dialect`, `pins`; marker diagnostics for files outside the schema that call a table builder"},
	"DR-K2": {"readColumn", "the property key; with a literal config `casing`, the key converted by the pinned rule (`drizzle-orm/casing.js:3-13` `toSnakeCase`/`toCamelCase`, applied only to omitted names, `keyAsName`, `column-builder.js:8`)"},
	"DR-T1": {"readTable", "a table: name (literal first argument), schema (`pgSchema` literal; else `public` on postgresql, none on sqlite/mysql), dialect from the builder module; on postgresql only a table (or enum) whose schema is in the `push` schema filter: the config's literal `schemaFilter`, else `[\"public\"]` (developer decision 2026-11-10); a table two configs read: claimed once, in the group of the first config by root, when they agree on `dialect`, `casing` and `schemaFilter` (identities are per file)"},
	"DR-C1": {"builtinType", "column name (literal first argument, else DR-K2); SQL type as the oracle catalog shows it (`varchar(20)` -> `character varying(20)`, `timestamp` -> `timestamp without time zone`, `.array()` -> `<type>[]`); `serial`/`smallserial`/`bigserial`: `integer`/`smallint`/`bigint`, not null, sequence default; on mysql (mapped) the `COLUMN_TYPE` MySQL 8.0.19+ reports: `serial` -> `bigint unsigned`, `boolean` -> `tinyint(1)`, `char` -> `char(1)`, the rest as drizzle-kit writes them; `mode`, sqlite `text({enum})` and `bigint({mode})` are TypeScript-only (no fact; no CHECK, oracle)"},
	"DR-C2": {"modifiers", "nullability; single-column primary key (name not claimed: inline, the database names it); unique: postgresql a UNIQUE constraint `<table>_<column>_unique` (pre-casing column name) or the literal name, `nulls: \"not distinct\"` as `nulls_not_distinct: true`; sqlite a unique index of that name; default: string `'x'` (quotes doubled), number and boolean as written, json/jsonb object or array `'<JSON.stringify>'::json(b)`, array column `'{\"a\",\"b\"}'`, `sql` text by the rendering rule, `defaultNow()` `now()`, `defaultRandom()` `gen_random_uuid()` (`bin.cjs:17021-17045`); MySQL `defaultNow()` a default (presence, `date.common.js:6-8`; MySQL default texts are presence only); foreign key to the referenced table and column, name `<table>_<col>_<reftable>_<refcol>_fk` (`pg-core/foreign-keys.js:55`) truncated to 63 bytes on postgresql, actions as written, else `no action` for both (oracle); generated: `generated: \"stored\"` with expression text by the rendering rule; identity: `identity: \"always\"`/`\"by default\"` and not null; the `$`-prefixed client modifiers and `$type`: no fact, and no database default is proven by them"},
	"DR-C3": {"customTypeSQL", "column type `t` as written (kaneo `bytea`, dokploy `text`; oracle)"},
	"DR-K3": {"casedConstraintName", "explicit (literal) column names are never converted (oracle: `explicitName` kept); index and primary/foreign key generated names use the converted names, unique generated names the pre-casing names"},
	"DR-X1": {"extraConfig", "indexes with name (omitted: `<table>_<cols>_index`, `bin.cjs:16919`, cased names), columns, uniqueness, method (`btree` when absent), per column `desc` and `nulls` (default `last` also for `desc()`, `pg-core/columns/common.js:74-77`: `.desc()` renders `DESC NULLS LAST`), opclass, partial predicate text by the rendering rule; composite primary key name (omitted: `<table>_<cols>_pk`, `pg-core/primary-keys.js:34`; postgresql: its columns not null; sqlite: unnamed, nullability unchanged, A10); unique constraint (postgresql; sqlite unique index) name (omitted: `<table>_<cols>_unique`); composite foreign keys with actions and name (omitted: `<table>_<cols>_<reftable>_<refcols>_fk`, truncated to 63 bytes); check name and expression text"},
	"DR-E1": {"columnFunction", "Postgres enum type (schema-qualified) with its values in order; the column's type is the enum (array with `.array()`); MySQL `enum('a','b')` column type (mapped); sqlite `text({enum})`: `text`, no fact for the values"},
	"DR-R1": {"relations", "ORM-level relations only (developer decision 2026-11-09): per entry, kind (`one`/`many`), the source and target table identities and, for `one` with literal `fields`/`references`, the column lists, in table attribute `orm_relations`; never a `foreign_key` edge, never a database fact (`verification` not set; the view labels them ORM relations)"},
	"DR-P1": {"failure", "diagnostic `schema_model_import_failure` (row named); no claim for the tables involved"},
	"DR-P2": {"none", "none (they are not in the group); covered by DR-K1's marker"},
	"DR-V":  {"none", "stated, never claimed as facts"},
	"D":     {"unread", "nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `drizzle`) for whole declarations (one per module without a payload, naming its exported values)"},
}

// Catalogue returns the Drizzle contract's catalogue: rows in the docs
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

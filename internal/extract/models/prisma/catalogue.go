package prisma

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the Prisma claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as in
// T018g0): the environment assumptions PR-V1 to PR-V6 and the rows of
// docs/extract/schema-prisma.md "Claim contract" with their claim branch
// and claim cell as written there (TestPrismaCatalogue checks both
// against the document).

var environmentAssumptions = []models.Assumption{
	{ID: "PR-V1", Title: "Pins", Text: "The Prisma version in the package's lockfile is the one that creates the database."},
	{ID: "PR-V2", Title: "Schema deployed", Text: "The database was created from this schema by Prisma Migrate or `db push`; hand-edited `migration.sql` is outside it."},
	{ID: "PR-V3", Title: "Provider", Text: "The datasource URL points to a database of the declared provider."},
	{ID: "PR-V4", Title: "Schema location", Text: "The schema found is the one the CLI uses (no `--schema` flag; the CLI runs in the package directory)."},
	{ID: "PR-V5", Title: "Default schema", Text: "The datasource URL has no `schema` parameter (or names `public`)."},
	{ID: "PR-V6", Title: "Mapped provider", Text: "Prisma's `<provider>` connector behaves as its source states at the locked version (facts with `verification: \"mapped\"`: mysql, sqlserver, cockroachdb)."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a claim side) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"PR-B1":  {"readDatasource", "group `dialect` (`postgresql`, `mysql`, `sqlite`, `sqlserver`, `cockroachdb`); `relation_mode` (`foreignKeys` when absent, else as written); `schemas` list read (gates PR-AA5)"},
	"PR-B2":  {"none", "`previewFeatures` read when a literal list (gates `views`, and `multiSchema` before 6.13.0); no claim"},
	"PR-B3":  {"buildTable", "one table, name `M` or PR-AA4; columns from its scalar and enum fields; schema from PR-AA5, else `public`; `@@ignore` tables claimed like any other (Migrate creates them; attribute `ignored: true`); `primary_key: null` proven when no `@id`/`@@id` (hoppscotch 2 tables)"},
	"PR-B4":  {"none", "none; marker diagnostic `schema_model_unread` (Migrate creates nothing for a view)"},
	"PR-F1":  {"columnType", "`type` as written; Postgres type `text`, `integer`, `bigint`, `double precision`, `numeric(65,30)`, `timestamp(3) without time zone`, `boolean`, `jsonb`, `bytea`; SQLite declared type `TEXT`, `INTEGER`, `BIGINT`, `REAL`, `DECIMAL`, `DATETIME`, `BOOLEAN`, `JSONB`, `BLOB` (oracle)"},
	"PR-F2":  {"nullable", "none: `nullable: false`; `?`: `nullable: true`; `[]`: array of the element type (`text[]`, `\"E\"[]`), **`nullable: true`**"},
	"PR-F3":  {"columnType", "column exists, type the literal text as written, nullability by PR-F2"},
	"PR-F4":  {"enumType", "column type the enum's database name (PR-E1); on sqlite `TEXT` (oracle); on mysql the inline `ENUM(...)` and on cockroachdb the enum's name from the connector source (`mapped`)"},
	"PR-A1":  {"fieldID", "single-column primary key; constraint name only from literal `map:` (generated `<table>_pkey` never claimed); with `@unique` on the same field, also PR-A2"},
	"PR-A2":  {"columnUnique", "`unique: true` on the column; one single-column unique entry whose kind is provider-dependent: a **unique index** on postgresql and sqlite (oracle: `index`), on mysql and cockroachdb from the connector source (`mapped index`), a UNIQUE **constraint** on sqlserver (`unique_constraints`, mapped: `flavour/mssql/renderer.rs:154-166, :228-242`); name only from literal `map:`"},
	"PR-A3":  {"columnDefault", "(a) server default; its text (`server_default_text`, the database's text as Prisma renders it) on postgresql and sqlite for a string (`'...'`, `'` doubled), integer (postgresql: 0 to 2147483647), boolean or enum value (its mapped name, quoted), else presence only; (b) server default `CURRENT_TIMESTAMP` (also on `@db.Date` and together with `@updatedAt`); (c) `serial`/`bigserial`/`smallserial`: sequence default, presence only (the sequence name is not claimed), type kept; (d) server default, text as written (sqlite: presence only); (e) no server default, `default: \"client\"`"},
	"PR-A4":  {"none", "client-side; no server default unless PR-A3 also present (then PR-A3)"},
	"PR-A5":  {"field", "column name `x` (identity keeps the field name)"},
	"PR-A6":  {"nativeType", "native type as written (D6) with the Postgres type it renders (`VARCHAR(n)` -> `character varying(n)` ...); only the corpus forms and the fixture's are oracle-checked, the build's fixture covers every listed form"},
	"PR-A7":  {"field", "column claimed like any other (Migrate creates it); attribute `ignored: true`"},
	"PR-AA1": {"blockID", "composite primary key over the mapped column names; constraint name only from literal `map:`; `name:` is client-only (no fact)"},
	"PR-AA2": {"blockIndex", "a unique entry over the mapped columns, its kind as in PR-A2 (`index`, `mapped index`, `constraint`); name only from literal `map:`; `name:` client-only"},
	"PR-AA3": {"blockIndex", "index with the mapped columns; name only from literal `map:`; `type:` (`BTree`, `Hash`, `Gist`, `Gin`, `SpGist`, `Brin`) as the access method, `BTree` when absent"},
	"PR-AA4": {"buildTable", "table name `t` (identity keeps the model name)"},
	"PR-AA5": {"readSchemaAttr", "table (enum) schema `s` (no preview feature from 6.13.0; before it the `multiSchema` preview feature)"},
	"PR-R1":  {"owningRelation", "under `foreignKeys`: one `foreign_key` (composite when several fields) from the mapped columns to the referenced model's mapped table and columns; `on_delete` as written, else `Restrict` (`NoAction` on sqlserver) when any `fields:` scalar is required and `SetNull` otherwise; `on_update` as written, else `Cascade`; name only from literal `map:`. Under `relationMode = \"prisma\"`: no foreign key, the table's `foreign_keys: []` proven, the relation listed in attribute `emulated_relations` (not an edge)"},
	"PR-R2":  {"none", "no column"},
	"PR-R3":  {"joinTable", "join table `_<name>` with `<name>` the explicit relation name or `<A>To<B>` (model names, not `@@map`, byte order); columns `A` and `B` (`nullable: false`, typed as the id of the first and second model); index on `B`; key `(\"A\",\"B\")`: on postgresql from Prisma 6.0.0 a primary key, on postgresql with Prisma 5 and on sqlite a unique index (no primary key); with Prisma 7 on mysql and cockroachdb a unique index (`mapped index`) and on sqlserver a UNIQUE constraint (`constraint`), no primary key, from the connector source (`sql_schema_calculator.rs:349-354`); where the key is claimed on postgresql or sqlite, under `foreignKeys` two foreign keys `ON DELETE CASCADE ON UPDATE CASCADE`; schema of model A's table (`public` without PR-AA5). Self-relations included (both columns reference the same table)"},
	"PR-E1":  {"enumType", "enum values (mapped) and type name (mapped), attribute of the columns that use it (no node; T018b1 has no enum kind)"},
	"PR-P1":  {"buildTable", "diagnostic `schema_model_import_failure` (row named), no claim for the model (or, for whole-schema failures, the schema)"},
	"D":      {"none", "nothing; the table or column is partial with `D` as reason, or a marker diagnostic for whole blocks"},
}

// Catalogue returns the Prisma contract's catalogue: rows in the docs
// table's order, each with the assumptions of its row (Contract).
func Catalogue() models.Catalogue {
	c := models.Catalogue{Tool: Tool, Assumptions: slices.Clone(environmentAssumptions)}
	for _, r := range Contract {
		e := catalogueRows[r.ID]
		// A row without assumptions lists none ([]), never null: the API
		// serves the list as it is.
		c.Rows = append(c.Rows, models.ContractEntry{ID: r.ID, Branch: e.Branch, Summary: e.Summary, Assumptions: append([]string{}, r.Assumptions...)})
	}
	return c
}

func init() {
	models.Register(Catalogue())
}

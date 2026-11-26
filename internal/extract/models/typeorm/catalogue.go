package typeorm

import (
	"slices"

	"github.com/graphvein/graphvein/internal/extract/models"
)

// The catalogue of the TypeORM claim contract for the schema API's
// /api/v0/schema/assumptions (T018f, ADR 0055 option 6A, registered as the
// other model front ends do): the environment assumptions
// TO-V1 to TO-V10 and the rows of docs/extract/schema-typeorm.md "Claim
// contract" with their claim branch and claim cell as written there
// (TestTypeORMCatalogue checks both against the document); D's entry is
// kept for that check but not served (Catalogue).

var environmentAssumptions = []models.Assumption{
	{ID: "TO-V1", Title: "Pins", Text: "The `typeorm` version in the package's lockfile (`pins: {\"typeorm\": \"<v>\", \"typescript\": \"<v>\"}`) created the database and the `typescript` version compiled the entities."},
	{ID: "TO-V2", Title: "Schema deployed", Text: "The database was created by `synchronize` for the reached entities or by migrations that match them (TypeORM migration classes are not read, TO-U1). seerr: production runs its migrations (`synchronize: false`, `migrationsRun` in dev Postgres); the 55 SQLite and 20 Postgres migrations give the synchronize catalog except generated constraint names."},
	{ID: "TO-V3", Title: "Decorator metadata", Text: "The entities are compiled by `tsc` (the pinned `typescript`) with `experimentalDecorators` and `emitDecoratorMetadata` as the nearest tsconfig chain states them, and `strictNullChecks` as that chain states it (`strict` implies it). Without both decorator flags proven `true`, every design-type inferred type is unknown; with `experimentalDecorators` not `true`, the group is D (TypeORM 0.3 needs legacy decorators)."},
	{ID: "TO-V4", Title: "Dialect", Text: "The database is of the DataSource's literal `type`; when the options are selected at run time among several literal option objects (seerr: five objects, `sqlite` and `postgres`), the dialect is one of that set (open point O1)."},
	{ID: "TO-V5", Title: "Modules load", Text: "Entity module side effects do not change the metadata beyond what is read (no `getMetadataArgsStorage()` mutation, no decorator applied conditionally or in a loop), and when an `entities` glob is read, the process runs with the package directory as working directory (cwd-relative globs) and a compiled output mirrors the source tree (`__dirname` globs). NestJS (TO-K4): an import declaration loads its module (TypeScript elides an import used only as a type; such a module is still counted), and a module loaded only by a dynamic `import()` is not seen (it leaves a list incomplete only when no root reaches the `forFeature` file)."},
	{ID: "TO-V6", Title: "Mapped dialects", Text: "MySQL, MariaDB and MSSQL facts follow TypeORM 0.3.31's driver source (`driver/mysql/MysqlDriver.js`, `driver/sqlserver/SqlServerDriver.js`) as cited (never `oracle`)."},
	{ID: "TO-V7", Title: "Schemas exist", Text: "An entity `schema` on Postgres names a schema that exists: `synchronize` does not create it."},
	{ID: "TO-V9", Title: "Verified versions (TA3)", Text: "The rules were verified at TypeORM 0.3.22, 0.3.31, 1.1.2 and 1.1.3 (oracle) and TypeScript 5.4.5, 5.8.3, 5.9.3 and 6.0.3; a pin of another 0.3.x or 1.x version is read with the per-major rules and this assumption shown (open point O14)."},
	{ID: "TO-V10", Title: "Naming-strategy package (TA3, only with TO-K6)", Text: "The `typeorm-naming-strategies` version in the lockfile is 4.x (verified at 4.1.0, the latest; its peer range names TypeORM 0.2/0.3, and under 1.x it is installed past that range and behaves the same)."},
	{ID: "TO-V8", Title: "Config namespaces load (TA2, only with TO-K5)", Text: "A `registerAs` namespace read through `configService.get('<token>')` is in the `ConfigModule` load list at run time (nest-admin: `load: [...Object.values(config)]`, `src/app.module.ts:39`)."},
}

// catalogueRow is a row's claim branch (the function that holds it,
// "none" for a row without a branch) and its claim cell.
type catalogueRow struct {
	Branch, Summary string
}

var catalogueRows = map[string]catalogueRow{
	"TO-K1": {"Discover", "the group (`set: models/typeorm/<file>`), `pins`, `dialect` (one type) or the dialect set, the reached entity classes, `table_list` complete when every `entities` element is read"},
	"TO-K2": {"entity", "the names"},
	"TO-K3": {"forRootAsync", "the group, `pins`, `dialect` from the literal `type`, the scope options as TO-K1"},
	"TO-K4": {"features", "reached entities: the options' `entities` (TO-K1 element rules) when it is an array, plus every `forFeature` entity registered for that data source name in a file the root's application loads through static imports (from the root module file and from every entry file of the package, a non-test file no other package file imports, whose imports reach the root file: import declarations and `export ... from` clauses, resolved by the module resolver, recursively; package imports are not followed) (`typeorm.module.js:23-25`, `entities-metadata.storage.js:5-28`, `typeorm-core.module.js:166-183`); a non-array `entities` option is replaced by the `forFeature` list (`:176-179`; in 12.x an object-map `entities` keeps its values and the `forFeature` list is appended, `typeorm-core.module.js:182-186` at 12.0.3; a string `entities` with `autoLoadEntities`: `may_be_incomplete` on every version); without `autoLoadEntities`, `forFeature` reaches no table (repositories only)"},
	"TO-K5": {"configGet", "the registered options, read as if written in place"},
	"TO-K6": {"naming", "every TO-K2 name through the strategy (`snake-naming.strategy.js` at 4.1.0): table the literal name, else `snakeCase(className)` (`:7-9`); column the literal `name` verbatim, else `snakeCase(property)` (`:10-13`); join column the `@JoinColumn` `name`, else `snakeCase(relationProperty + \"_\" + referencedColumnProperty)` (`:17-19`); junction table `snakeCase(ownerTable + \"_\" + property + \"_\" + targetTable)` of the unprefixed names (`:20-26`); junction columns `snakeCase(table + \"_\" + referencedColumnDatabaseName)` (`:27-29`), `_1`/`_2` and `entityPrefix` inherited; Postgres enum types from the snake column name; generated names as TO-X4 over the snake names; `snakeCase` is TypeORM's own (`util/StringUtils.js:29-36` / `:32-39`)"},
	"TO-K7": {"candidates", "the options, read as if written in place (Maintainerr `src/app/config/typeOrmConfig.ts:4`, used at `src/app/app.module.ts:49`)"},
	"TO-K8": {"neutralSpread", "the spread adds no scope option; the other keys read as TO-K1 (aqualink `ormconfig.ts:15-34`: `{url}` or `{host, port, database, username, password}`)"},
	"TO-E1": {"entity", "a model (table); `synchronize: false`: the table and its columns claimed with `verification: \"mapped\"` under the shown assumption \"TypeORM does not create this table; this is the shape the code expects\" (Revision 1 O4)"},
	"TO-E2": {"members", "inherited columns, relations, indexes and uniques, base-class members first"},
	"TO-E3": {"decorator", "no table fact: the member is read as if the decorator were absent"},
	"TO-C1": {"newColumn", "the column; primary-key columns in member order (composite allowed); generation: postgres `increment` a `nextval` sequence default (serial), `identity` `GENERATED BY DEFAULT`, `uuid` default `uuid_generate_v4()`; sqlite `increment` on the single integer primary key `INTEGER PRIMARY KEY AUTOINCREMENT`, `uuid` no default"},
	"TO-C2": {"typeOn", "type per dialect"},
	"TO-C3": {"defaultOn", "type without an explicit one: postgres `timestamp without time zone` (`PostgresDriver.js:170-176`), sqlite `datetime` (`AbstractSqliteDriver.js:125-131`), version `integer`; create/update default `now()` (postgres) / `datetime('now')` (sqlite) unless the column states a `default`; delete-date columns nullable (`metadata/ColumnMetadata.js:269`); create/update/version not null"},
	"TO-C4": {"option", "nullability (primary keys not null); default per dialect and type family: postgres `normalizeDefault` (`PostgresDriver.js:793-830`), sqlite (`AbstractSqliteDriver.js:465-483`), mysql (`MysqlDriver.js:642-670`); a string `'<value>'` on a character, text (not MySQL's TEXT) or enum column only; on a boolean column a boolean string (true/false, t/f, yes/no, y/n, on/off, 1/0, any case) and a number 1/0 as postgres stores them (`true`/`false`) and on sqlite as written; a number as written on postgres integer, numeric and character columns (quoted on enum columns) and on sqlite, quoted (`toFixed`) on mysql number, boolean, character and enum columns; a boolean `true`/`false` on a postgres boolean column, `1`/`0` on sqlite and on mysql boolean and number columns; a function default as its text, except on postgres a text containing `CURRENT_TIMESTAMP`, `CURRENT_DATE`, `CURRENT_TIME`, `LOCALTIMESTAMP` or `LOCALTIME`, where exactly `CURRENT_TIMESTAMP` is `now()` and any other such text is unknown (`PostgresDriver.js:1270-1304`; **1.x** rewrites only a whole keyword with an optional precision, `:1455-1486` at 1.1.3, so the same claim holds and other texts stay unknown); `unique: true` a single-column unique constraint (`Column.js:54-58`); a default's `verification` is its dialect's only for a form the oracle fixture compares on that dialect (`oracleDefaultForms`), else `mapped`"},
	"TO-C5": {"enumType", "postgres: an enum type (TO-K2 name) with the labels in order (numbers as strings); sqlite: `varchar` with a `CHECK (col IN (...))` (labels claimed, constraint unnamed); mysql/mariadb `ENUM(...)` (mapped)"},
	"TO-C6": {"wrapped", "the column as the inner decorator with the call's literal options: name, nullability, unique and primary claimed; type and default unknown when the wrapper assigns them"},
	"TO-X1": {"indexDecorator", "an index: columns in order, `unique`, partial (`where` present), explicit name; junction tables: one index per side"},
	"TO-X2": {"constraints", "a unique constraint: columns, explicit name"},
	"TO-X3": {"classConstraint", "a check constraint; explicit name claimed; the expression text as written (not compared: the database rewrites it)"},
	"TO-X4": {"constraintName", "the name, under the shown assumption \"schema made by synchronize\""},
	"TO-R1": {"relation", "join columns (TO-K2 names) referencing the target's primary columns or the named ones (`RelationJoinColumnBuilder.js:103-118`), each typed as the referenced column (integer for an `increment` key, `uuid`/`varchar` for a `uuid` key, length kept); nullable unless `nullable: false` or primary (`metadata/RelationMetadata.js:173-174`); a foreign key unless `createForeignKeyConstraints: false` (`:178-179`, `RelationJoinColumnBuilder.js:62-67`) with `onDelete`/`onUpdate` as written (upper case), else `NO ACTION` (`metadata/ForeignKeyMetadata.js:32-33`); explicit `foreignKeyConstraintName` claimed; a join column whose name equals the database name of an explicit column of the same entity reuses that column: its own nullability, default and uniqueness stay, its type becomes the referenced column's (`RelationJoinColumnBuilder.js:141`, `:186-190` / `:163`, `:205-209`; Maintainerr 9, aqualink 12 sites); explicit composite `@JoinColumn([...])` columns in array order (0.3.x) or **1.x** in the target's primary-key order (`:129-137` at 1.1.3); on a MySQL-only group, an index on the foreign key's columns named as the foreign key (`mapped`) when no declared index, unique constraint or the primary key leads with them (InnoDB creates it: MySQL reference manual, \"FOREIGN KEY Constraints\"; TypeORM names every constraint and reads an index named like a referential constraint as the foreign key's, `driver/mysql/MysqlQueryRunner.js:1507-1520`, `:1881`)"},
	"TO-R2": {"relation", "as TO-R1, plus a unique constraint on the join columns unless they are all primary or no foreign key is created (`RelationJoinColumnBuilder.js:81-95`)"},
	"TO-R3": {"relation", "no table fact (counted; the owner side's row claims)"},
	"TO-R4": {"junction", "a junction table (TO-K2 names): one column per primary (or named referenced) column of each side, typed as referenced, not null, together the primary key; two foreign keys: the owner side's with the relation's `onDelete`/`onUpdate`, else `CASCADE`; the inverse side's with the inverse relation's `onDelete`/`onUpdate` when the owner names an inverse property (`NO ACTION` when that relation states none), else `CASCADE` (`JunctionEntityMetadataBuilder.js:162-198`); **1.x:** the inverse relation's `onDelete`/`onUpdate` each, `CASCADE` when it states none (`?? \"CASCADE\"`, `:177-190` at 1.1.3), composite sides in primary-key order (`:241`, `:273`); one index per side"},
	"TO-P1": {"fail", "diagnostic `schema_model_import_failure` (row named); no claim for any table of the group on that dialect: validation runs for every entity before any DDL, and the schema build runs in one transaction that is rolled back (`schema-builder/RdbmsSchemaBuilder.js:52-78`)"},
	"TO-P2": {"member", "refused: the member's column (name, type and every fact) unknown and the table's column set `partial`, reason naming the wrapper; a helper value: that fact unknown"},
	"TO-U1": {"migrations", "`unread_tools` entry (found, not read yet)"},
	"TO-V":  {"none", "stated, never claimed as facts"},
	"D":     {"unread", "no claim; marker diagnostic `schema_model_unread` with `tool: \"typeorm\"` and the form"},
}

// Catalogue returns the TypeORM contract's catalogue: assumptions in the
// docs' order, rows in the docs table's order, each with the assumptions
// of its row (Contract). The schema API keys model rows per tool, so the
// default row D is served beside the other tools' D rows (Revision 3
// addendum 3, OP2).
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

# Schema from Drizzle models

ADR 0059 (T018g2, `docs/contract/claims/drizzle-models.md`; inventory and
oracle baseline in the T018g2 task record (internal)). The Drizzle front
end `internal/extract/models/drizzle` reads the drizzle-kit configuration
of each package that depends on Drizzle and the schema modules it names,
from the cached TypeScript model payload (`docs/lang/typescript-models.md`)
and the retained `package.json`, lockfile and JSON config sources, and
hands its tables to the tool-neutral model kit `internal/extract/models`
(ADR 0058, `docs/extract/schema-models.md`), which emits the records every
ORM front end shares (`source: "model"`, `tool: "drizzle"`, group
`models/drizzle/<config path>`).

**Status (2026-11-09):** discovery (`discover.go`,
`scripts.go`, `config.go`, `glob.go`, `reach.go`; pins from the shared
lockfile reader `internal/extract/models/lockfile`), the contract table
(`contract.go`), the claim pass for every claim row (`link.go`,
`tables.go`, `columns.go`, `modifiers.go`, `render.go`, `lookup.go`; `keys.go`, `extra.go`, `relations.go`), the
catalogue for `/api/v0/schema/assumptions` (`catalogue.go`) and the
indexer call (`internal/index/index.go`, after the Prisma front end) are in
place, and the committed oracle harness with `TestDrizzleOracle` ("Oracle" below) found 0 disagreements on the fixture, kaneo and
dokploy.

## Discovery

As the pinned drizzle-kit finds its configuration and schema (verified in
the drizzle-kit 0.31.11 and 0.31.9 bundles, `node_modules/drizzle-kit/bin.cjs`,
line numbers from 0.31.11). `Discover` returns the packages, their pins
and configurations; a proven configuration is one model group.

- **Packages.** A directory with a `package.json` (not under
  `node_modules`) whose `dependencies` or `devDependencies` name
  `drizzle-orm` or `drizzle-kit`.
- **Pins (DR-V1).** The locked versions of `drizzle-orm` and
  `drizzle-kit`, read for that package from the lockfile beside it, else
  from the nearest lockfile above it (`pnpm-lock.yaml` importer,
  `package-lock.json` `packages`, `yarn.lock`; every YAML document of a
  pnpm lockfile; the reader `internal/extract/models/lockfile` shared with
  the Prisma front end, `docs/extract/schema-prisma.md` "Discovery").
  `pins` is
  `{"drizzle-orm": v, "drizzle-kit": v}` only when both are proven, else
  `null` (DR-V1 still listed). A package that depends only on one of the
  two has `pins: null`.
- **Versions.** drizzle-orm 0.x is read (developer decision 2026-11-09).
  A locked drizzle-orm version, or without one the `package.json` range,
  whose major is 1 or more (`1.0.0-rc.*` included): the package's configs
  are not read, one `schema_model_not_proven` marker.
- **Configs.** drizzle-kit uses a config file when no option but
  `--config` is given, the options otherwise ("cli" mode), and exits when
  both are given (`assertCollisions`, `bin.cjs:10716-10731`). A config file
  is `--config <path>` or the first of `drizzle.config.ts`,
  `drizzle.config.js`, `drizzle.config.json` in the working directory
  (`drizzleConfigFromFile`, `bin.cjs:15838-15870`). DR-V5: drizzle-kit
  runs in the package directory. A package's configurations are therefore:
  - the first default config name present in the package directory (named
    `default`; group root its path, kaneo `apps/api/drizzle.config.ts`);
  - every config named by a literal `--config <path>` or
    `--config=<path>` of a `drizzle-kit` command in the package's
    `package.json` scripts, resolved against the package directory (named
    `--config`; dokploy `apps/dokploy/server/db/drizzle.config.ts`, six
    scripts, one group);
  - a `drizzle-kit` command with literal `--schema` and `--dialect` (and
    optionally `--casing`) and no `--config` (named `flags`; group root
    `<package.json>#<first script>`, one group per distinct flag set;
    fixture only).

  The scripts reader splits a script at `&&`, `||`, `;`, `|`, `&` and
  newlines outside quotes. A command runs drizzle-kit when a word is
  `drizzle-kit`, ends in `/drizzle-kit` or starts with `drizzle-kit@`.
  `schema_model_not_proven` markers (no configuration read from that
  command): a `cd`/`pushd` earlier in the script or a runner option that
  changes the directory (`-C`, `--dir`, `--prefix`, `--cwd`, `--filter`,
  `-F`, `--workspace`, `-w`) (DR-V5 not proven); a non-literal option
  value (`$`, a backquote, a backslash); `--config` together with another
  cli option (`--schema`, `--dialect`, `--casing`, `--out`, `--driver`,
  `--url`, `--breakpoints`; drizzle-kit exits); a `--config` that does not
  exist or leaves the repository; cli options without both a literal
  `--schema` and `--dialect`. Commands of other tools (dokploy
  `packages/server`'s `@better-auth/cli generate --config ...`) are not
  drizzle-kit commands.
- **Other config files.** A `drizzle.config.*` file that is neither a
  package's default config nor named by a literal `--config`:
  `schema_model_unread` marker, not read.
- **Config object.** Read, never run: the payload's `export default`
  expression, `defineConfig({...})` (with `defineConfig` imported from
  `drizzle-kit`) or an object literal, `satisfies`/`as` transparent; or a
  `drizzle.config.json` object. Keys are read in order (a later key
  wins). A spread or computed key, any other default export (an
  identifier, a CommonJS `module.exports`, a config file without a
  payload) makes the config unproven. `dialect` must be a string literal:
  `postgresql`, `sqlite`, `turso` (SQLite rules), `mysql`; `singlestore`
  and `gel` go to D (`schema_model_unread`); anything else, a missing or
  non-literal dialect: unproven. `schema` must be a string literal (a
  template without substitutions counts) or an array of them; missing or
  non-literal: unproven. `casing` absent: none; `snake_case` or
  `camelCase`: read; anything else: `CasingKnown` false (DR-K2: derived
  names unknown). `schemaFilter` absent: nil; literal string or array:
  read; else `SchemaFilterKnown` false. `out`, `dbCredentials`,
  `migrations`, `strict`, `verbose` are not facts.
- **Schema paths** (`prepareFilenames`, `bin.cjs:19713-19745`).
  Each pattern is a glob matched against the package directory, not
  the config's directory, with glob 8's `dot: false`: a literal path, `*`
  and `?` within one segment, `**` over zero or more segments; a wildcard
  never matches a name that starts with a dot. A matched file is read; a
  matched directory contributes the files directly in it. Braces, classes,
  negation, extglobs, a backslash, an absolute path, a `..` after a
  wildcard or a path outside the repository: unproven. Matched files
  with an extension other than `.ts .js .mjs .cjs .mts .cts`: one
  `schema_model_unread` marker, not read. No matched file with one of
  those extensions: unproven (drizzle-kit exits). Only the repository's
  scanned files are matched (a file ignored by `.gitignore` or
  `.graphveinignore` is not seen).
- **Modules reached** (`prepareFromExports`, `bin.cjs:16727-16770`).
  drizzle-kit reads `Object.values(require(file))` of each schema file, so
  the modules its re-exports reach contribute values: `export * from`
  (every name visible in the re-exporting module) and `export { x } from`
  or `export { x }` of an imported binding (only `x`), followed through
  the shared TypeScript resolver (relative paths, tsconfig `paths`,
  workspace packages; dokploy `@dokploy/server/*`). A namespace re-export
  (`export * as ns`) is one object, never a table, and type-only
  re-exports are erased: neither reaches a module. External packages are
  skipped; a re-export that resolves to no repository file is one
  `schema_model_unread` marker and the rest is kept. `Config.Modules`
  lists each module with the names visible from it.
- **No config.** A Drizzle package without any configuration found: no
  claims, one `schema_model_not_proven` marker naming the package's files
  whose module-level declarations call a table builder, with counts; none
  when no file does (dokploy `apps/schedules`).
- **Files outside the schema (DR-P2).** Per proven config, the files whose
  module-level declarations call a table builder, in the config's package
  or in a package that owns a reached module (nearest `package.json`),
  and that no module of the config is: one `schema_model_unread` marker
  (kaneo `apps/api/auth-schema.ts`, dokploy
  `packages/server/auth-schema2.ts`). A table builder call is a call
  chain whose first call is `pgTable`, `sqliteTable`, `mysqlTable`,
  `singlestoreTable`, `gelTable` or a `*TableCreator` imported from a
  `drizzle-orm/<dialect>-core` module (also through a namespace import),
  or `<s>.table(...)` on a `pgSchema`/`mysqlSchema` const of the file or
  on an imported binding. Commented-out code is not in the payload.

## Claim pass

`Link` runs discovery and, per proven config, reads the exported values
of the config's modules (drizzle-kit's `Object.values(exports)`,
deduplicated by declaration) and hands the tables to the kit. Identifiers
resolve to captured declarations through imports, `export { }` clauses
and `export *` barrels (`lookup.go`), so a spread object, `customType`,
enum or `sql` const may sit outside the schema. Verified in drizzle-orm
0.45.4 and drizzle-kit 0.31.11; every text below was compared with
`drizzle-kit export` of the fixture's postgresql and sqlite schemas, the postgresql types and
defaults with the catalog of a scratch Postgres 17.11 and the sqlite ones with a scratch SQLite 3.53.1 file (the full oracle
is step G2B5's).

- **Tables (DR-T1).** `export const x = pgTable("n", {...}, extra?)`
  (also `sqliteTable`, `mysqlTable`, a namespace import, or an
  `export { x as y }` clause) with a literal name and an object literal of
  columns; `<s>.table(...)` on an exported `pgSchema("s")` const (schema
  `s`), else schema `public` on postgresql and none on sqlite/mysql.
  Not claimed: a non-exported table (A1, no record); `mysqlSchema(...)
  .table` (A9, `schema_model_unread`); on postgresql a table whose schema
  is outside the effective push filter, the config's literal
  `schemaFilter` or `["public"]` when it is absent (`schema_model_not_claimed`:
  push keeps only those schemas, `bin.cjs:10770`, `:16944`, generate keeps
  every schema, `bin.cjs:19850`, so the table's existence depends on the
  command; a non-literal filter claims no table). Row D
  (`schema_model_unread`): a non-literal name, a non-object columns
  argument (the `(t) => ({...})` form), table creators, `singlestoreTable`,
  `gelTable`, a call chain such as `.enableRLS()`, a `let`/`var` table, a
  default-exported table, a builder of another dialect than the config's
  (drizzle-kit collects only that dialect's tables, DR-V4), `pgSchema`
  with a non-literal name or `"public"` (drizzle-orm throws).
- **Columns object (DR-C1).** Properties in order, a later key replacing
  an earlier value in its first position (JavaScript object semantics);
  a spread of a module-level object literal (same file or imported)
  contributes its properties. Any other spread: `column_set` partial
  (DR-C1); a shorthand, method or computed entry: D.
- **Column builders (DR-C1).** From the table's dialect module (another
  dialect's builder: column unknown, D), name and config by drizzle-orm's
  `getColumnNameAndConfig` (`utils.js:117-122`: a non-empty string first
  argument is the name, an object first argument the config). Types
  (`getSQLType`, catalog form):

  | dialect | builder | type |
  |---|---|---|
  | postgresql | `text`, `integer`, `smallint`, `boolean`, `real`, `date`, `uuid`, `inet`, `json`, `jsonb` | the builder name |
  | postgresql | `doublePrecision` | `double precision` |
  | postgresql | `serial`, `smallserial`, `bigserial` | `integer`, `smallint`, `bigint`; not null and a sequence default (`serial.js:7-8`) |
  | postgresql | `bigint({mode})`, `bigserial({mode})` | `bigint` (a config object is required: `bigint.js:50-55` reads `config.mode`) |
  | postgresql | `varchar({length})`, `char({length})` | `character varying(n)`, `character(n)`; without length `character varying`, `character(1)` |
  | postgresql | `numeric({precision, scale})` | `numeric(p,s)`, `numeric(p,0)`, `numeric` |
  | postgresql | `timestamp({precision, withTimezone})`, `time(...)` | `timestamp(p) with time zone`, `timestamp without time zone`, ... |
  | postgresql | `interval()` | `interval` (fields or precision: type unknown) |
  | sqlite | `integer`, `real`, `blob`, `numeric` | the builder name (any `mode`) |
  | sqlite | `text({length, mode, enum})` | `text(n)` with a length, `text` without one or with `mode: "json"` |
  | mysql | `int`, `smallint`, `tinyint`, `mediumint`, `bigint({unsigned})` | the name, `... unsigned` (mapped) |
  | mysql | `boolean`, `text`, `tinytext`, `mediumtext`, `longtext`, `json`, `date`, `year`, `serial` | the builder name (mapped) |
  | mysql | `varchar({length})`, `char({length})`, `datetime({fsp})`, `timestamp({fsp})`, `time({fsp})` | `varchar(n)`, `datetime(fsp)`, ... (mapped) |

  `.array()` (postgresql, no argument, first in the chain): `<type>[]`;
  modifiers written before it are not on the array column (a new builder,
  `pg-core/columns/common.js:147-153`): they are dropped and the column's
  other facts unknown (D). `mode`, `enum`, `$type` are TypeScript-only.
  Any other builder of the dialect module: name read, type unknown.
  A config value the type depends on that is not literal: type unknown.
- **Names (DR-C1, DR-K2, DR-K3).** A literal name is the column name
  (DR-C1; DR-K3 when the config has a literal `casing`: never converted).
  An omitted name is the property key, converted by `casing`
  (`casing.js:3-13`; the lookahead of `[A-Z]+(?![a-z])` is scanned by
  hand, `render.go`): DR-K2, assumption DR-V3 on the table. Omitted under
  a non-literal `casing`: the column is unknown.
- **Modifiers (DR-C2).** Applied in order as the builder's config is set:
  `.notNull()`; `.primaryKey()` (also `primaryKey({autoIncrement})` on
  sqlite) sets not null; `.unique(name?, {nulls})`; `.default(x)`;
  `.defaultNow()` (postgresql timestamp, time, date: `now()`);
  `.defaultRandom()` (postgresql uuid: `gen_random_uuid()`);
  `.generatedAlwaysAs(sql | () => sql)` (postgresql: `generated:
  "stored"`, `generated_expression` by the rendering rule, an own-table
  substitution only in the arrow form); `.generatedAlwaysAsIdentity()`,
  `.generatedByDefaultAsIdentity()` (postgresql integer, smallint, bigint:
  `identity` `always`/`by default`, not null, `int.common.js:5-38`);
  `$defaultFn`, `$default`, `$onUpdate`, `$onUpdateFn` (client only: no
  database default, A3), `$type`. `.references()`: below.
  Any other modifier: D, and every fact it could change (nullability,
  keys, uniqueness, index, foreign keys, default) unknown.
- **Absences.** `nullable: true`, `primary_key`/`unique`/`index: false`
  and `foreign_keys: []` need every modifier read and every element of
  the table's extra config read (below); otherwise those facts
  are unknown (sqlite keeps nullability, a composite key does not change
  it there, A10). Several
  `.primaryKey()` columns: primary key unknown (D). MySQL `serial`:
  nullability, uniqueness and default unknown (the type implies NOT NULL,
  UNIQUE, AUTO_INCREMENT).
- **Unique constraints (DR-C2).** Name: the literal, else
  `<table>_<builder name>_unique` (the builder name is the literal name or
  the pre-casing key, A12), truncated to 63 bytes on postgresql (A13).
  postgresql: `unique_constraints` `{name, columns, nulls_not_distinct}`;
  sqlite: `indexes` `{name, columns, unique: true}` (A10); mysql:
  `unique_constraints` `{name, columns}` (mapped). A builder in a spread
  object is shared by every table spreading it and keeps the first
  table's generated name: name unknown, constraint set partial.
- **Defaults (DR-C2; DR-C1 for serials).** `default` `server`,
  `server_default: true` and `server_default_text` as drizzle-kit renders
  it (postgresql `bin.cjs:17021-17045`, sqlite `:18611-18617`): an `sql`
  template without substitutions verbatim (also through a module-level
  const, same file or imported), a string `'...'` with `'` doubled, a
  number as JavaScript prints it (plain decimal literals only, else the
  text is unknown), a boolean, a json/jsonb value `'<JSON.stringify>'::json(b)`
  (postgresql) or `'<JSON.stringify>'` (sqlite), an array on an array
  column `'{...}'` (`buildArrayString`, `bin.cjs:16849-16872`; strings
  with a quote, double quote or backslash: text unknown). A serial: a
  sequence default, text not claimed. `.default(null)` on postgresql:
  `default: "absent"`, `server_default: false` (Postgres stores no
  default for `DEFAULT null`). No default modifier: `absent`/`false`;
  only client modifiers: `default` unknown, `server_default: false`.
  Unknown: a non-literal argument, a call, `new Date()`, an `sql` default
  with a substitution (drizzle-kit throws, `bin.cjs:15930-15942`), `null`
  on sqlite and mysql, an object default on a column that is neither
  json nor an array, identity, generated and sqlite integer primary key
  (rowid) columns. MySQL: presence only (mapped).
- **Custom types (DR-C3).** A module-level `customType({...})` const of
  the table's dialect module whose `dataType()` returns one string
  literal (the payload keeps a method's single returned literal; it
  cannot depend on parameters); `toDriver`/`fromDriver` have no SQL
  effect; any other entry: type unknown.
- **Enums (DR-E1).** A column of an exported `pgEnum("n", [...])` or
  `<s>.enum(...)`: `type` the enum name (`<s>.<n>` in another schema,
  `[]` with `.array()`) and `enum` `{name, schema, values}` (values null
  when not literal). MySQL `mysqlEnum("c", [...])`: `enum('a','b')`
  (mapped).
- **Failures (DR-P1, `schema_model_import_failure`).** A table using a
  non-exported enum (A2) or `pgSchema`, or an enum whose schema is outside
  the push filter; two exported tables with one schema-qualified name;
  two columns with one name; two `.unique()` constraints with one name in
  a table (drizzle-kit exits, `bin.cjs:16997`); on sqlite one unique
  index name in two tables (index names are per database). The tables
  involved are not claimed.
- **Groups.** `verification` `oracle` on postgresql and sqlite, `mapped`
  on mysql; table `assumptions` DR-V1, V2, V4, V5, plus V3 (a name from
  `casing`) and V6 (mysql). A declaration two configs reach: see
  "Revision after the independent review" (B2).

## Keys, extra config and relations

Verified in drizzle-orm 0.45.4 and drizzle-kit 0.31.11; the fixture's
postgresql forms were compared with `drizzle-kit export` applied to a
scratch Postgres 17.11 (catalog names, index definitions, NOT NULL), the
casing forms with `drizzle-kit generate`, the sqlite forms with `export`
(the full oracle is step G2B5's).

- **Foreign keys (DR-C2, DR-X1).** `.references(() => T.col, {onDelete,
  onUpdate})` and the extra config's `foreignKey({columns, foreignColumns,
  name?})` with `.onDelete(a)`/`.onUpdate(a)`, resolved once every table
  of the group is read (`keys.go`): `T` is a table const of the group
  (same file, imported, or a module-level alias `const user = userTable`,
  read from the TypeScript facts' module bindings), the keys its
  properties. Each local column lists the whole constraint
  (`local_columns`, `columns`, `on_update`, `name`; the kit's composite
  shape). Actions: a literal of `cascade`, `restrict`, `no action`, `set
  null`, `set default`, else unknown; absent: `no action` for both.
  Name: the literal, else `<table>_<cols>_<reftable>_<refcols>_fk` from
  the builder names (`pg-core/foreign-keys.js:44-55`), then drizzle-kit's
  casing pass (each original column name replaced by its cased name, the
  first occurrence, `bin.cjs:17105-17115`), truncated to 63 bytes on
  postgresql (A13); sqlite: no name (inline, unnamed, A10); mysql: a name
  over 64 characters is unknown. A target that is not a claimed table of
  the group (not exported, refused, D) or a column that is not known: the
  column's `foreign_keys` unknown, the constraint set partial. A key that
  is not a property of a complete target table: DR-P1.
- **Extra config (DR-X1, `extra.go`).** The third argument, an arrow with
  one named parameter whose body is an array (flattened one level, as
  `getTableConfig` does) or an object (its property values, the
  deprecated form). Elements from the table's dialect module:
  - `index(name?)` / `uniqueIndex(name?)` with `.on(cols)` or (postgresql)
    `.using(method, cols)`, then `.where(sql)`, `.concurrently()` (no
    fact) and `.with({...})` (`storage_parameters: null`, unknown). Entry
    `{name, columns, unique, method, orders, nulls, opclasses, partial,
    where, storage_parameters}` on postgresql (`method` `btree` when
    absent; per column `asc`/`desc`, `nulls` `last` unless `.nullsFirst()`,
    also for `.desc()`, A15; `.op("cls")`); `{name, columns, unique,
    partial, where}` on sqlite; `{name, columns, unique}` on mysql. An
    `sql` column is an expression: `columns[i]` null, `expressions[i]` its
    text (`"<column>"` substitutions). Omitted name (postgresql only):
    `<table>_<cased columns>_index` (`bin.cjs:16919`); unnamed with an
    expression, or unnamed on sqlite/mysql: D. A column listed twice: D
    (`.on()` resets the column's index config after its first use).
  - `primaryKey({columns, name?})`: the key in its order
    (`primary_key`), `primary_key_name` the literal or
    `<table>_<builder names>_pk` after the casing pass, truncated
    (postgresql); null on sqlite (unnamed) and mysql (always `PRIMARY`).
    Its columns are not null on postgresql and mysql (`bin.cjs:16137`),
    unchanged on sqlite (A10); a one-column `primaryKey()` on sqlite is
    the column's own key. With a column `.primaryKey()` as well: primary
    key unknown (D).
  - `unique(name?).on(cols)` with `.nullsNotDistinct()` (postgresql): a
    unique constraint (postgresql, mysql) or unique index (sqlite), name
    the literal or `<table>_<builder names>_unique` (pre-casing, A12).
  - `foreignKey(...)` as above; `check(name, sql)`: `{name, expression}`.
  - Texts by the rendering rule: `${t.col}` or `${<table const>.col}`
    renders `"<table>"."<column>"` in `.where` and `check`, `"<column>"` in
    an index expression; any other substitution, a table outside schema
    `public`, or mysql with a substitution: the text is null (unknown).
  - Anything else (another element, a column argument that is not
    `t.col`, a block body, a destructured parameter): D for that element,
    the constraint set partial, the primary key open, and the column
    absences the element could change unknown (as before G2B4).
- **Column facts from the extra config.** `index: true` when a
  non-unique index has exactly that column (partial or not), else
  `false` (row DR-X1 when the table has an extra config, DR-T1 without);
  `unique: true` also from a one-column `unique()` or non-partial
  `uniqueIndex`; a column whose only one-column unique index is partial:
  `unique` unknown; `nullable: false` from a postgresql or mysql
  composite key (row DR-X1).
- **Relations (DR-R1, `relations.go`).** `relations(T, ({ one, many }) =>
  ({...}))` (or `(h) => ({ k: h.one(...) })`) among the exported values
  of the group's modules, `T` a claimed table (aliases followed): each
  entry `k: one(U, { fields: [T.a], references: [U.b], relationName? })`,
  `one(U)` or `many(U, { relationName? })` is listed in `T`'s attribute
  `orm_relations` as `{name, kind, source, target, table, columns,
  references}` (`target`/`table` null when `U` is not a claimed table;
  `columns`/`references` only for `one` with literal lists). Never a
  `foreign_key` edge, never a claim. Another entry form: D, a reason on
  the table (fact `orm_relations`). `() => ({})` declares none. A
  relations() of a table the group does not claim: `schema_model_unread`.
  Module-level `relations(...)` calls of the package outside the group's
  modules (kaneo's `relations.ts`, 31): one `schema_model_unread` marker
  on the config naming the files and counts.
- **Failures (DR-P1), added.** An extra-config column or a `.references()`
  / `foreignColumns` key that is not a property of a complete table
  (drizzle-orm reads a property of undefined), a unique name used by a
  column and a `unique()` element; on sqlite, extra-config index and
  unique names count in the database-wide name check. The tables
  involved are not claimed; a table that references a refused table gets
  an unknown foreign key, not a failure.
- **Catalogue.** `catalogue.go` registers DR-V1..DR-V6 and every row with
  its claim cell and branch function (`models.Register`), so
  `/api/v0/schema/assumptions` lists the Drizzle contract
  (`TestDrizzleCatalogue` checks it against this document).

## Migrations folder and overlap

- **Migrations folder.** A config's drizzle-kit `out` folder (the literal
  `out` key, else drizzle-kit's default `drizzle`, `bin.cjs:15299`,
  `:15325`) is resolved against the package directory, where drizzle-kit
  runs (A5), and reported for every config whose keys are read, proven or
  not: `schema_model_migrations` "`<config>: drizzle-kit migrations folder
  (not read): <folder>`" (the code Prisma's front end uses). A non-literal
  `out`, a config that cannot be read whole (spread, no default export) or
  a folder outside the repository names none. No pass reads the folder;
  the server lists it in `unread_tools` as tool `drizzle-kit` when it
  holds `meta/_journal.json` or a `.sql` file directly in it (marker the
  journal, files the `.sql` count; `internal/server/schemamodels.go`
  `drizzleMigrations`), and the Schema view says "Drizzle migrations
  found, not read yet". kaneo: `apps/api/drizzle` (journal, 74 `.sql`
  files); dokploy: `apps/dokploy/drizzle` (journal, 200).
- **Overlap.** The migration pass's records reach `drizzle.Input`
  (`Migrations`, `Edges`): a Drizzle table whose name a migration set
  also defines gets the kit's `schema_model_migration_overlap` diagnostic
  (with the column difference when both are complete); the sources are
  never merged. No Drizzle row tags it (as for Prisma).

## Oracle

`scripts/oracle/drizzle_catalog.sh` with its Node helper
`scripts/oracle/drizzle_catalog.mjs` (own code, never run by CI; Node
22.13 or later, npm, psql, go). Per run it installs the pinned
drizzle-kit and drizzle-orm (and, for the corpora, the packages the
schema modules import at load, at the corpus lockfile's versions) into
scratch with `--ignore-scripts`, copies the schema modules and every
module their runtime imports reach (relative imports and the package's
tsconfig `paths`; type-only imports skipped) into a tree under that
install, writes a scratch config with the run's literal dialect, casing
and absolute schema paths (the project's own config is never loaded), and
runs `drizzle-kit export`, or `generate` into a scratch folder when the
run has a casing (export ignores casing). A non-zero exit,
anything on stderr or no `CREATE TABLE` is a harness failure (export exits
0 with nothing on stdout when a module fails to load). postgresql SQL is
applied to a fresh database on the scratch Postgres 17 cluster
(`scripts/cloud-postgres.sh`), sqlite SQL to a scratch SQLite file
(`node:sqlite`); mysql SQL is kept as the mapped reference. The catalog
(columns with `format_type`, defaults, generated expressions and identity;
constraints with `pg_get_constraintdef`; indexes with columns, per-column
order and nulls, operator classes, method, predicate, key expressions and
storage parameters; enums; SQLite pragmas and `sqlite_master`) goes into
a JSON dump. `TestDrizzleOracle` then lists every claimed expression text
(defaults, generated expressions, checks, index predicates and
expressions), the harness puts each through the run's database the way
drizzle-kit's DDL does (`SET DEFAULT`, `ADD COLUMN ... GENERATED`, `ADD
CONSTRAINT ... CHECK`, `CREATE INDEX ... WHERE` or on the expression,
inside a rolled-back transaction) and stores Postgres's rendering, so both
sides compare in the same catalog form.

Safety: the oracle executes schema modules. Every module of a run is read
before it runs; the helper refuses a copied module with a top-level call
statement or `await` and one with a dynamic `import()` or `require`.
dokploy's three imports called only inside functions
(`@dokploy/server/constants` `paths`, `lib/encryption`, `templates`
`generatePassword`) are stubs that throw when called; the validation,
hostname and branch modules are copied for real; `db/schema/dbml.ts`
(`pgGenerate` at load) is not reached. Package scripts never run, the
corpus checkouts are only read, and the network is the npm registry.

Drops: forms that stop drizzle-kit loading the schema or Postgres applying
it are removed from the copy, none of them claimed (the test fails if a
dropped table is claimed): the fixture's DR-P1 tables (non-exported enum
and `pgSchema`, duplicate table, column, unique and sqlite index names, a
missing extra-config or referenced column), the string spread of the D
table `partial`, the D columns `unknowns.odd` (`.using()` is not a
function) and `unknowns.late` (a substitution in a default: drizzle-kit
throws), and `authors.rlsId`, whose foreign key (unknown: `rls` is not
claimed) names a column Postgres rejects as a target; from the review
revision the DR-P1 table `review/refs.ts` `byEmail` (a foreign key to a
column that is neither the primary key nor unique) and the column
`byCode.code` (unknown foreign key to a column that is unique only through
a unique index, which drizzle-kit creates after the foreign keys; both
rejected by Postgres when kept, checked once). The corpora drop nothing.

`TestDrizzleOracle` (`oracle_test.go`, skipped unless
`GRAPHVEIN_DRIZZLE_ORACLE` names the dump and
`GRAPHVEIN_DRIZZLE_ORACLE_ROOT` the directory its runs are relative to)
compares per claimed table: the table, a complete column set, the primary
key and its name, unique constraints (name, columns,
`nulls_not_distinct`), indexes (name, columns, uniqueness, method, order,
nulls, operator classes, partial and predicate, expressions, storage
parameters), checks; per claimed column fact: type (`format_type`; the
SQLite declared type without case), nullability, primary key, unique,
index, default and server default (SQLite: one pair of outer parentheses
stripped on both sides, G2B3), the default text, identity, generated
expression, enum values and foreign keys (columns, target, actions, name
on postgresql; SQLite keeps no name). MySQL claims are checked against the
printed SQL (table, column line, type, `NOT NULL`, `DEFAULT`). The pins
and dialect of every table must be the run's. A fact is `oracle` only for
a form the oracle compared (T018g0 rule): `TestDrizzleOracleForms` (not
gated) keys every oracle-labelled fact of the fixture by dialect, fact,
row and value class (the type without arguments, an enum, the shape of a
default text, each referential action, each index option) and requires
exactly the committed `oracleForms`; `TestDrizzleOracle` over the
fixture's dump requires each of them compared.

Results (Postgres 17.11, SQLite through Node 24.22.2's `node:sqlite`; the fixture rows after the review revision):

| run | command | tables | columns | facts compared | disagreements | forms dropped |
|---|---|---|---|---|---|---|
| fixture `pg` (kit 0.31.11, orm 0.45.4) | export | 14 | 89 | 991 | 0 | 15 |
| fixture `pgschema` | export | 1 | 1 | 16 | 0 | 0 |
| fixture `cased` (`snake_case`) | generate | 2 | 7 | 92 | 0 | 0 |
| fixture `camel` (`camelCase`) | generate | 1 | 3 | 34 | 0 | 0 |
| fixture `sqlite` | export | 3 | 14 | 164 | 0 | 2 |
| fixture `mysql` (mapped reference) | export | 3 | 15 | 62 | 0 | 0 |
| kaneo (kit 0.31.11, orm 0.45.4) | export | 51 | 442 | 5,349 | 0 | 0 |
| dokploy (kit 0.31.9, orm 0.45.2) | export | 68 | 960 | 9,499 | 0 | 0 |

Findings fixed by class rules (fixture cases in `pg/db/keys.ts`
`audits`, `pg/db/enums/mood.ts`, `pg/lib/shared.ts`):

- **O1 Enum type names are quoted as format_type quotes them.** dokploy's
  41 enum columns of mixed-case enums (`"applicationStatus"`) disagreed:
  the claimed type was the bare name. The enum type is now rendered with
  Postgres's identifier quoting (lower-case letters, digits and
  underscores and not a non-unreserved keyword stay bare; the keyword list
  is `pg_get_keywords()` of Postgres 17.11), schema included.
- **O2 customType literals.** Postgres spells a `dataType()` literal its
  own way (`varchar(255)` is `character varying(255)`), so the claim is
  limited to the literals the oracle compared (`bytea`, `text`); any
  other postgresql literal is type unknown (DR-C3 refusal).
- **Coverage.** Forms the corpora used that the fixture did not (the
  `restrict` and `set null` delete actions, `cascade` updates, a partial
  non-unique btree index, a plain unique index, a `text` customType) are
  fixture cases now, so `oracleForms` covers both corpora.

To run it (scratch only):

    GV_PG_DIR=<scratch>/pg bash scripts/cloud-postgres.sh start
    bash scripts/oracle/drizzle_catalog.sh --scratch <scratch>/oracle \
      --pg "$(GV_PG_DIR=<scratch>/pg bash scripts/cloud-postgres.sh url)" \
      --out <scratch>/fixture.json --root testdata/fixtures/drizzle-models --fixture
    GRAPHVEIN_DRIZZLE_ORACLE=<scratch>/fixture.json \
      GRAPHVEIN_DRIZZLE_ORACLE_ROOT=$PWD/testdata/fixtures/drizzle-models \
      go test ./internal/extract/models/drizzle -run '^TestDrizzleOracle$' -v

(`--corpus` with `--root` the directory holding the `kaneo` and `dokploy`
checkouts runs the corpora.)

## Revision after the independent review (2026-11-09)

Class rules from the review of T018g2 (task file, same heading), each with
a fixture case or test:

- **B1 Modules without a payload are never silent.** A schema module the
  config matches or reaches that has no model payload (gate G1 needs a
  direct `drizzle-orm/<dialect>-core` import; builders imported through a
  local re-export, fixture `pg/db/review/reexported.ts` through
  `pg/lib/core.ts`) and whose visible exports include a value declaration
  gets one `schema_model_unread` marker on that file: `<file>: <n>
  exported value(s) not read (<names>): the module has no Drizzle model
  payload ...`. Functions and classes are not counted (never tables), and
  re-exports are followed by discovery. The capture is not widened: that
  needs an aggregate extractor version bump and a gate redesign (ADR 0059,
  C4); the marker is the v1 rule.
- **Group list marker.** Every claim-pass diagnostic about a declaration
  that may be a table of the group but is not claimed there (B1, D tables,
  table creators, DR-P1 failures, the push-filter not-claimed, B2) ends
  with `; the table list of model group <set> may be incomplete`. The
  server (`internal/server`, `modelListMarker`) counts such a diagnostic
  in that group's `table_list` whatever file it is on, and from this
  revision reads the `schema_` diagnostics of TypeScript (and `.prisma`)
  files at all: before, only `.py` diagnostics reached `table_list`, so a
  Drizzle group was `complete` with unread tables. A per-declaration
  message (`<file>:<line>: ...`) counts one in the file's `unread`.
  Markers of declarations that create no table of the config (another
  dialect's builder, `mysqlSchema(...).table`) carry no list marker but,
  on a file with a claimed table, still make the list `may_be_incomplete`
  (conservative).
- **B2 Two configs, one module.** Identities are per file, so a table is
  one node in one group. Configs that agree on `dialect`, `casing` and
  `schemaFilter` produce the same facts: the table is claimed in the group
  of the first config by root, and each other group gets
  `schema_model_not_claimed` naming the claiming group (with its list
  marker). Configs that differ: claimed in neither, the message names the
  key. One config file run by two packages (a default config another
  package names with `--config`) is one group when both working
  directories resolve its schema to the same files, and the message never
  names it twice; when they resolve it differently, no table is claimed
  (`schema_model_not_proven`, DR-V5).
- **B3 MySQL catalog forms and modifiers (mapped).** Types are the
  `COLUMN_TYPE` MySQL 8.0.19+ reports (DR-V6 states the server version):
  `serial` -> `bigint unsigned` (SERIAL is BIGINT UNSIGNED NOT NULL
  AUTO_INCREMENT UNIQUE; drizzle-kit's own introspection reads that back
  as serial, `bin.cjs:16431-16437`), `boolean` -> `tinyint(1)` (BOOLEAN is
  TINYINT(1), the one integer type 8.0.19+ shows with a width), `char` ->
  `char(1)`; `varchar` without a length (MySQL rejects it) is type unknown;
  a customType literal on mysql is type unknown. `.autoincrement()`
  (`mysql-core/columns/common.js:61-65`; AUTO_INCREMENT,
  `bin.cjs:16027`, `:23159`): the default is unknown (not a default), the
  other facts are read. `timestamp().defaultNow()`
  (`date.common.js:6-8`, ``default(sql`(now())`)``): a default
  (presence; MySQL default texts stay presence only).
  `timestamp().onUpdateNow()` (`date.common.js:10-14`, ON UPDATE
  CURRENT_TIMESTAMP, `bin.cjs:16028`, `:23158`): without an explicit
  default the default is unknown (drizzle-orm marks a default; MySQL's
  depends on `explicit_defaults_for_timestamp`), the other facts are read.
  The oracle's MySQL text check reads the SQL's type through the same
  three reported forms (`mysqlReported`). Fixture `mysql/schema.ts`
  `counters`.
- **DR-P1 addition: foreign keys to non-keys (postgresql).** A claimed
  foreign key whose referenced columns are neither the primary key nor a
  unique constraint nor a non-partial unique index without expressions of
  a table whose keys are all read makes the referencing table a failure
  (Postgres: "there is no unique constraint matching given keys for
  referenced table"; oracle run with `byEmail` kept). Found by the oracle
  while adding the case: drizzle-kit writes the foreign keys of created
  tables before their indexes (`applyPgSnapshotsDiff`, `bin.cjs:28011`,
  `:29041-29042`), so a key to columns that are unique only through a
  unique index is rejected from one generate or push too; it is unknown
  (it exists only when the index came from an earlier migration).
  Fixture `pg/db/review/refs.ts`.
- **C1/C2 harness.** Claimed expression texts are put through the run's
  database with the table's schema on the search path but read back under
  the default one, as the catalog dump reads them (an enum default in a
  non-public schema reads `'member'::app.app_role` on both sides; fixture
  `appUsers.role`). `prepare` expands a matched directory into the files
  directly in it with a loadable extension, as discovery does (fixture
  `pg` schema `./db/review`).
- **C3 wording.** `$defaultFn`/`$onUpdate`: "value set by Drizzle at
  runtime; database default unknown". A table creator's marker names the
  tables its calls build (`it builds the table(s) "created"`), also when
  the creator is not exported (fixture `pg/db/review/creator.ts`).
- **UI (A1).** A Drizzle table's `relations()` section states nothing
  about database foreign keys: "Used by Drizzle's relational queries only.
  Database foreign keys, if any, are listed under Foreign keys." Prisma's
  emulated-relation text, true there, stays (`relationTexts` per tool).

## Assumptions

Ids namespaced per the plan's shared piece 3 (task file "Environment";
DR-V6 from the dialect rule):

- **DR-V1 Pins.** The drizzle-orm and drizzle-kit versions in the
  package's lockfile are the ones that created the database.
- **DR-V2 Schema deployed.** The database was created from this schema by
  `drizzle-kit push`, or by `generate` + `migrate` of migrations generated
  from it (the config's `out` folder is not read), so schema and database
  agree.
- **DR-V3 Runtime casing.** When a column name is derived from the
  config's `casing`, the application's `drizzle({ casing })` uses the same
  value.
- **DR-V4 Dialect.** The database is of the config's literal `dialect`,
  and the table builders come from that dialect's module.
- **DR-V5 Working directory.** drizzle-kit runs in the package directory
  (as the package's own scripts run it), so the config's `schema` paths and
  the default config file name resolve against that directory.
- **DR-V6 Mapped MySQL.** drizzle-kit's MySQL serializer behaves as its
  source states at the pinned version, and the server is MySQL 8.0.19 or
  later, whose `information_schema.COLUMNS.COLUMN_TYPE` reports the forms
  "MySQL mapping" lists (facts with `verification: "mapped"`: mysql).

## Claim contract

Copied from `docs/contract/claims/drizzle-models.md` (ids and text
identical); `contract.go` holds the same rows, and `TestDrizzleContract`
checks the three against each other.


Finalised from the inventory and the oracle (the investigation). Row kinds: **K**
scope and naming; **T** tables; **C** columns and modifiers; **X** table
extra config; **E** enums; **R** relations; **P** provable failures; **D**
the single default branch. Every form the corpora use has exactly one row
(counts per row: the T018g2 task record (internal)); forms only the fixture
uses come from the same oracle runs. The contract test keeps
`docs/extract/schema-drizzle.md`, the Go table and the code 1:1 (plan,
shared piece 4). **14 rows: 13 rows plus D**; DR-P1 only refuses, DR-P2 and
DR-V have no branch.

Dialect rule (developer decision 2026-11-09):
- **postgresql:** facts verified by the oracle (`verification:
  "oracle"`): `drizzle-kit export` applied to a scratch Postgres 17.
- **sqlite / turso:** the same oracle on a scratch SQLite file
  (`sqlite_master`, `pragma table_xinfo`, `index_list`, `index_info`,
  `foreign_key_list`); facts that agree are `verification: "oracle"`.
- **mysql:** facts taken from drizzle-kit's MySQL source at the pinned
  version (`generateMySqlSnapshot`, `bin.cjs:15990-16295`, column SQL types
  from `drizzle-orm/mysql-core/columns/*.js` `getSQLType`), with file:line
  in `docs/extract/schema-drizzle.md`, and `drizzle-kit export --dialect
  mysql` output (no database) as the rendered reference: `verification:
  "mapped"`, assumption DR-V6 "drizzle-kit's MySQL serializer behaves as
  its source states at the pinned version". Never `oracle`.
- **Unknown in every dialect:** the database's own choices (collations,
  engine, charset, Postgres-generated constraint names such as
  `<table>_pkey` for an inline primary key).

Provider-independent facts (table and column names, column set,
nullability, key and index column lists) are claimed for every supported
dialect with the dialect's verification.

Rendering rule for `sql` templates (pinned by the oracle):
a template without substitutions is its text verbatim. A substitution is
rendered only when it is a column of the same table (`${table.col}`,
`${t.col}`): as `"<table>"."<column>"` in `.where(...)`, `check(...)` and
`generatedAlwaysAs(...)`, and as `"<column>"` inside an index `.on(sql`...`)`
expression; any other substitution makes the text unknown. In `.default`,
drizzle-kit refuses substitutions (`sqlToStr`, `bin.cjs:15930-15942`
throws): default unknown.

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| DR-K1 | scope (Discovery above): package dependency, default or `--config`-named config, literal `dialect`, `schema` (path, glob, array), `casing`, `schemaFilter`; schema files and the modules their exports reach (`export *`, `export { } from`, tsconfig `paths`) | the group (`models/drizzle/<config path>`), its file set, `dialect`, `pins`; marker diagnostics for files outside the schema that call a table builder | non-literal `dialect`/`schema`, unsupported glob syntax, a config not named by the default or a literal `--config`, no config: no claims (marker); drizzle-orm 1.x: marker, no claims; one config file run from two package directories that resolve its schema to different files: no claims (`schema_model_not_proven`, DR-V5), the same files: one group | DR-V1, DR-V4, DR-V5 |
| DR-K2 | column name omitted (`text()`, `integer({ mode })`) | the property key; with a literal config `casing`, the key converted by the pinned rule (`drizzle-orm/casing.js:3-13` `toSnakeCase`/`toCamelCase`, applied only to omitted names, `keyAsName`, `column-builder.js:8`) | non-literal `casing`: name unknown, column claim refused | DR-V3 |
| DR-T1 | exported module-level `const` (or a name in an `export { }` clause) holding `pgTable("n", cols, extra?)`, `sqliteTable`, `mysqlTable`, or `<s>.table("n", ...)` where `<s>` is an exported `pgSchema("s")` const | a table: name (literal first argument), schema (`pgSchema` literal; else `public` on postgresql, none on sqlite/mysql), dialect from the builder module; on postgresql only a table (or enum) whose schema is in the `push` schema filter: the config's literal `schemaFilter`, else `["public"]` (developer decision 2026-11-10); a table two configs read: claimed once, in the group of the first config by root, when they agree on `dialect`, `casing` and `schemaFilter` (identities are per file) | a postgresql table or enum whose schema is outside that filter, or any under a non-literal `schemaFilter`: not claimed (`schema_model_not_claimed`; `push` keeps only the filter's schemas, `bin.cjs:16944`, `generate` every schema, `bin.cjs:19850`); non-exported table: no table (oracle); `mysqlSchema(...).table`: no table (drizzle-kit skips schema tables on MySQL, `bin.cjs:16271`); a table two configs read that differ in one of those keys: not claimed in either (`schema_model_not_claimed`), the other configs of an agreeing set: `schema_model_not_claimed` naming the group that claims it; non-literal name, `pgTableCreator`/`sqliteTableCreator`/`mysqlTableCreator` (the marker names the tables its calls build), builders in functions or loops, columns not an object literal: D (table not claimed, marker); every such marker ends with the group's list marker, so the group's `table_list` is `may_be_incomplete` | DR-V2 |
| DR-C1 | column builder call from the table's dialect module, from a closed per-dialect list the oracle has run (the investigation: `text`, `varchar({length})`, `char({length})`, `integer`, `smallint`, `bigint({mode})`, `serial`, `smallserial`, `bigserial`, `boolean`, `real`, `doublePrecision`, `numeric({precision, scale})`, `timestamp({precision, withTimezone, mode})`, `date`, `time`, `interval`, `uuid`, `inet`, `json`, `jsonb` on postgresql; `text({length, mode, enum})`, `integer({mode})`, `real`, `blob({mode})`, `numeric` on sqlite; the build's fixture adds the rest of the 0.45 list before they are claimed); `.array()`; spread of a module-level object literal of columns (same file or imported) | column name (literal first argument, else DR-K2); SQL type as the oracle catalog shows it (`varchar(20)` -> `character varying(20)`, `timestamp` -> `timestamp without time zone`, `.array()` -> `<type>[]`); `serial`/`smallserial`/`bigserial`: `integer`/`smallint`/`bigint`, not null, sequence default; on mysql (mapped) the `COLUMN_TYPE` MySQL 8.0.19+ reports: `serial` -> `bigint unsigned`, `boolean` -> `tinyint(1)`, `char` -> `char(1)`, the rest as drizzle-kit writes them; `mode`, sqlite `text({enum})` and `bigint({mode})` are TypeScript-only (no fact; no CHECK, oracle) | builder outside the run list: type unknown; a non-literal config argument: the facts it carries unknown; spread of anything else: column set not complete; MySQL types from the mapped source only; mysql `varchar` without a length (MySQL rejects it): type unknown | DR-V1 |
| DR-C2 | modifiers `.notNull()`; `.primaryKey()` (implies not null on every dialect); `.unique(name?, {nulls})`; `.default(x)` with x a string, number, boolean, null, array or object literal, an `sql` template, or a module-level `const` holding one (same file or imported); `.defaultNow()`, uuid `.defaultRandom()`; `.references(() => t.col, {onDelete, onUpdate})` with `t` a table const of the group; `.generatedAlwaysAs(sql or () => sql)`, `.generatedAlwaysAsIdentity(opts?)`, `.generatedByDefaultAsIdentity(opts?)`; client-side `$defaultFn`, `$default`, `$onUpdate`, `$onUpdateFn`, `$type<T>()`; MySQL `.autoincrement()` (integer builders, `serial`), `timestamp().defaultNow()` and `timestamp().onUpdateNow()` | nullability; single-column primary key (name not claimed: inline, the database names it); unique: postgresql a UNIQUE constraint `<table>_<column>_unique` (pre-casing column name) or the literal name, `nulls: "not distinct"` as `nulls_not_distinct: true`; sqlite a unique index of that name; default: string `'x'` (quotes doubled), number and boolean as written, json/jsonb object or array `'<JSON.stringify>'::json(b)`, array column `'{"a","b"}'`, `sql` text by the rendering rule, `defaultNow()` `now()`, `defaultRandom()` `gen_random_uuid()` (`bin.cjs:17021-17045`); MySQL `defaultNow()` a default (presence, `date.common.js:6-8`; MySQL default texts are presence only); foreign key to the referenced table and column, name `<table>_<col>_<reftable>_<refcol>_fk` (`pg-core/foreign-keys.js:55`) truncated to 63 bytes on postgresql, actions as written, else `no action` for both (oracle); generated: `generated: "stored"` with expression text by the rendering rule; identity: `identity: "always"`/`"by default"` and not null; the `$`-prefixed client modifiers and `$type`: no fact, and no database default is proven by them | a non-literal default or a `new Date(...)`/call default: default unknown; `sql` default with a substitution: default unknown; a `references` target that does not resolve to a table column of the group: foreign key unknown (table partial); identity sequence options other than literals: those facts unknown; `.unique()` on sqlite under a name used twice: DR-P1; MySQL `.autoincrement()`: default unknown (AUTO_INCREMENT is not a default), the other facts read; `.onUpdateNow()` without a default: default unknown (drizzle-orm marks a default, `date.common.js:10-14`; MySQL's depends on the server), the other facts read | DR-V2 |
| DR-C3 | `customType<...>({ dataType() { return "t"; } })` as a module-level const (same file or imported), used as a column builder | column type `t` as written (kaneo `bytea`, dokploy `text`; oracle) | `dataType` with parameters, a non-literal return, or anything else in the object that changes SQL: type unknown; on postgresql a literal other than `bytea` and `text`, the ones the oracle compared (Postgres spells others its own way, `varchar(255)` as `character varying(255)`): type unknown; on mysql any literal (no literal is mapped): type unknown | - |
| DR-K3 | literal `casing` in the config together with explicit names | explicit (literal) column names are never converted (oracle: `explicitName` kept); index and primary/foreign key generated names use the converted names, unique generated names the pre-casing names | - | DR-V3 |
| DR-X1 | third argument `(t) => [ ... ]` or the deprecated `(t) => ({ ... })` of a table builder, elements: `index(name?)` / `uniqueIndex(name?)` with `.on(...)` or `.using(method, ...)`, column arguments with `.asc()`, `.desc()`, `.nullsFirst()`, `.nullsLast()`, `.op("cls")`, `.where(sql)`, `.concurrently()`; `primaryKey({columns, name?})`; `unique(name?).on(...)` with `.nullsNotDistinct()`; `foreignKey({columns, foreignColumns, name?})` with `.onDelete()`/`.onUpdate()`; `check(name, sql)` | indexes with name (omitted: `<table>_<cols>_index`, `bin.cjs:16919`, cased names), columns, uniqueness, method (`btree` when absent), per column `desc` and `nulls` (default `last` also for `desc()`, `pg-core/columns/common.js:74-77`: `.desc()` renders `DESC NULLS LAST`), opclass, partial predicate text by the rendering rule; composite primary key name (omitted: `<table>_<cols>_pk`, `pg-core/primary-keys.js:34`; postgresql: its columns not null; sqlite: unnamed, nullability unchanged, A10); unique constraint (postgresql; sqlite unique index) name (omitted: `<table>_<cols>_unique`); composite foreign keys with actions and name (omitted: `<table>_<cols>_<reftable>_<refcols>_fk`, truncated to 63 bytes); check name and expression text | index `.on(sql`...`)`: expression index, columns unknown, expression text by the rendering rule; `.with({...})` storage parameters: unknown; any other element, a non-array/object body or a block body: D for that element (table partial) | DR-V2 |
| DR-E1 | exported `pgEnum("n", ["a", ...])` or `<s>.enum("n", [...])` (exported `pgSchema`), used as a column builder (same file or imported); MySQL `mysqlEnum("col", [...])` and sqlite `text({enum})` column forms | Postgres enum type (schema-qualified) with its values in order; the column's type is the enum (array with `.array()`); MySQL `enum('a','b')` column type (mapped); sqlite `text({enum})`: `text`, no fact for the values | non-literal values: values unknown; a non-exported enum used by a column: DR-P1 (oracle: no `CREATE TYPE`, the table fails) | - |
| DR-R1 | `relations(table, ({ one, many }) => ({ k: one(t, { fields, references }), k2: many(t) }))` (0.45) in the group's schema modules | ORM-level relations only (developer decision 2026-11-09): per entry, kind (`one`/`many`), the source and target table identities and, for `one` with literal `fields`/`references`, the column lists, in table attribute `orm_relations`; never a `foreign_key` edge, never a database fact (`verification` not set; the view labels them ORM relations) | target not a table const of the group: target unknown; `relations()` outside the schema modules (kaneo `relations.ts`, 31): counted in a marker, not read; `defineRelations` (1.0): D | - |
| DR-P1 | failures provable from the source: a non-exported enum or `pgSchema` used by an exported table; two exported tables with one schema-qualified name in a group; a column name used twice in one table; two unique constraints with one name in a table (`bin.cjs:16997`, drizzle-kit exits); a `references` or extra-config column that is not a column of the named table; on postgresql a foreign key whose referenced columns are neither the primary key nor a unique constraint nor a unique index of a table whose keys are all read (Postgres rejects it; review addition, oracle) | diagnostic `schema_model_import_failure` (row named); no claim for the tables involved | a foreign key whose referenced columns are unique only through a unique index (drizzle-kit adds the foreign keys of created tables before their indexes, `bin.cjs:29041-29042`, so Postgres rejects it from one generate or push): foreign key unknown; other failures: assumed absent | DR-V2 |
| DR-P2 | `drizzle-kit` reads only the configured schema: tables defined only in files the scope does not reach | none (they are not in the group); covered by DR-K1's marker | - | DR-V5 |
| DR-V | assumptions DR-V1..DR-V6 attached to every record of the group (`assumptions`) | stated, never claimed as facts | - | - |
| D | anything else: `pgView`, `pgMaterializedView`, `pgSequence`, `pgPolicy`/RLS (`.enableRLS()`), `pgRole`, table creators, tables in functions, unknown builders or modifiers, `singlestore`/`gel` dialects, 1.0-only forms (`defineRelations`, `snakeCase.table`, `casing` on builders), a schema module without a model payload (builders imported through a local re-export) | nothing; the table or column is partial with `D` as reason, or a marker diagnostic `schema_model_unread` (tool `drizzle`) for whole declarations (one per module without a payload, naming its exported values) | always | - |

Rows DR-P2 and DR-V exist so the inventory can count every scope form and
the contract test can name the assumption row; neither has a claim branch.
The claim side rows are DR-K1, K2, K3, T1, C1, C2, C3, X1, E1, R1 (ten).

## MySQL mapping (DR-V6)

MySQL facts are `verification: "mapped"`: taken from drizzle-kit's MySQL
serializer and drizzle-orm's MySQL column classes, never from a database.
Sources at drizzle-orm 0.45.4 (the `mysql-core/columns/*.js` files of
0.45.2 are identical) and drizzle-kit 0.31.11:

- Snapshot: `generateMySqlSnapshot`, `drizzle-kit/bin.cjs:15990-16295`;
  a table is added only when it has no schema (`bin.cjs:16271`:
  `mysqlSchema(...).table` creates nothing).
- Column SQL types, `getSQLType()` in `drizzle-orm/mysql-core/columns/`:

| builder | SQL type | source |
|---|---|---|
| `bigint({mode, unsigned})` | `bigint`, `bigint unsigned` | `bigint.js:20`, `:46` |
| `binary({length})` | `binary`, `binary(n)` | `binary.js:27` |
| `boolean` | `boolean` | `boolean.js:18` |
| `char({length})` | `char`, `char(n)` | `char.js:23` |
| `customType` | the `dataType()` text | `custom.js:30` |
| `date({mode})` | `date` | `date.js:19`, `:44` |
| `datetime({mode, fsp})` | `datetime`, `datetime(fsp)` | `datetime.js:25`, `:57` |
| `decimal({precision, scale, unsigned})` | `decimal`, `decimal(p)`, `decimal(p,s)`, `... unsigned` | `decimal.js:29`, `:68`, `:104` |
| `double({precision, scale, unsigned})` | `double`, `double(p)`, `double(p,s)`, `... unsigned` | `double.js:22` |
| `mysqlEnum("c", [...])` | `enum('a','b')` | `enum.js:20`, `:41` |
| `float({precision, scale, unsigned})` | `float`, `float(p)`, `float(p,s)`, `... unsigned` | `float.js:22` |
| `int({unsigned})` | `int`, `int unsigned` | `int.js:17` |
| `json` | `json` | `json.js:15` |
| `mediumint({unsigned})` | `mediumint`, `mediumint unsigned` | `mediumint.js:20` |
| `real({precision, scale})` | `real`, `real(p, s)` | `real.js:20` |
| `serial` | `serial` | `serial.js:17` |
| `smallint({unsigned})` | `smallint`, `smallint unsigned` | `smallint.js:20` |
| `text`, `tinytext`, `mediumtext`, `longtext` | the builder name | `text.js:20` (`textType`, `:24-39`) |
| `time({fsp})` | `time`, `time(fsp)` | `time.js:18` |
| `timestamp({mode, fsp})` | `timestamp`, `timestamp(fsp)` | `timestamp.js:21`, `:49` |
| `tinyint({unsigned})` | `tinyint`, `tinyint unsigned` | `tinyint.js:20` |
| `varbinary({length})` | `varbinary`, `varbinary(n)` | `varbinary.js:31` |
| `varchar({length})` | `varchar`, `varchar(n)` | `varchar.js:24` |
| `year` | `year` | `year.js:15` |

The claimed `type` is the form MySQL reports for these (`serial` as
`bigint unsigned`, `boolean` as `tinyint(1)`, `char` as `char(1)`; review
B3, "Revision after the independent review").

G2B3 claims the builders listed under "Claim pass", nullability from
`notNull` (`bin.cjs:16015`), `.unique()` as a unique constraint
(`bin.cjs:16040-16062`) and a default's presence (`bin.cjs:16064`);
`drizzle-kit export --dialect mysql` output is the rendered reference
(G2B5).

## Tests

- `TestDrizzleContract` (`contract_test.go`): this table equals the task
  file's row for row; `contract.go` lists the same rows in the same order
  with the same assumptions and sides (ten claim rows, DR-P1 refusal only,
  DR-P2 and DR-V without a branch, D last); every listed form is named in
  its row; the assumptions section lists DR-V1 to DR-V6.
- `discover_test.go`: kaneo's shape (default config, stale file marker),
  dokploy's shape (`--config` in scripts, working-directory schema path,
  `export *` through tsconfig `paths`, a package without a config,
  commented-out schema), namespace, renamed and type-only re-exports,
  schema paths and globs, every config key and unproven form, the D
  dialects, the scripts reader, versions (1.x refused, pins null), an
  unresolved re-export, and `TestDiscoverScopeForms` (every DR-K1 form
  tagged, every tag a row or listed form).
- `fixture_test.go` (`testdata/fixtures/drizzle-models/`, golden
  `link.golden.json`): `TestDrizzleFixture`, the tag half of the contract
  test (golden, `modeltest.CheckClaims`, every row but DR-V reached, every
  form a row lists reached, every tag and claim a row),
  `TestDrizzleFixtureFacts` (the facts each row decides),
  `TestDrizzleFixtureKeys` (foreign keys, extra config, casing, sqlite,
  mysql, relations, the G2B4 failures). `TestDrizzleIndexedFixture`
  (`indexed_test.go`): the fixture indexed through the indexer, golden
  `testdata/fixtures/drizzle-models.golden.json`, and a warm replay that
  parses nothing and emits the same stream.
- `TestDrizzleCatalogue` (`contract_test.go`): the registered catalogue's
  claim cells, assumptions and branch functions against this document.
  `render_test.go`: casing against drizzle-orm's own output, number,
  JSON and array texts, truncation, catalog types. `link_test.go`: step
  isolation, a schema two configs share (agreeing and differing), one config run by two packages, a module without a payload (B1), a
  table creator's marker (C3), a non-literal casing. Server
  `TestSchemaDrizzleTableList` (both backends), `TestModelTableList` and
  `TestModelDiagnosticCount`: a Drizzle group's `table_list` from the
  linker's markers.
- `TestDrizzleCorpusLink` (`corpus_test.go`, skipped unless
  `GRAPHVEIN_DRIZZLE_CORPUS` is set): the claim pass on kaneo and dokploy
  against the investigation's baseline (tables, columns, NOT NULL, defaults, no
  nullability unknown; primary keys, unique constraints, foreign keys,
  indexes, unique and partial indexes; every table complete; the
  relations() calls read with their targets).
- `TestDrizzleOracle` and `TestDrizzleOracleForms` (`oracle_test.go`):
  see "Oracle". `TestDiscoverOut` (`discover_test.go`): the migrations
  folder marker; `TestLinkMigrationOverlap` (`link_test.go`): the overlap
  diagnostic.
- `TestDrizzleCorpusDiscover` (`corpus_test.go`, skipped unless
  `GRAPHVEIN_DRIZZLE_CORPUS` names the `.corpus` directory): discovery on
  kaneo and dokploy against the investigation's discovery rows.

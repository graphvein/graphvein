# Schema from Prisma models

ADR 0057 (T018g1, `docs/contract/claims/prisma-models.md`; inventory and
oracle baseline in the T018g1 task record (internal)). The Prisma front end
`internal/extract/models/prisma` reads the Prisma schema of each package
that depends on Prisma and hands its tables to the tool-neutral model kit
`internal/extract/models` (ADR 0058, `docs/extract/schema-models.md`),
which emits the records every ORM front end shares (`source: "model"`,
`tool: "prisma"`, group `models/prisma/<schema root>`).

**Status (revised after the independent review, 2026-11-09):** discovery (Prisma 5, 6 and 7), the lexer and
parser, the contract table, the kit extension, the table and column claims
(`link.go`, `schema.go`, `table.go`, `types.go`), relations and join tables
(`relations.go`), validation failures and the catalogue (`catalogue.go`)
are in place, and the indexer calls `prisma.Link` after the SQLModel
linker. The oracle (`scripts/oracle/prisma_catalog.sh`,
`TestPrismaOracle`) checks postgresql and sqlite facts against the catalog
Prisma's own SQL creates ("Oracle" below).

## Discovery

As the pinned Prisma 7 CLI finds the schema (verified in the 7.9.1 and
7.10.0 CLI sources; `discover.go`); Prisma 5 and 6 follow the rule after
this list (`discover_legacy.go`):

- **Packages.** A directory with a `package.json` (not under
  `node_modules`) whose `dependencies` or `devDependencies` name `prisma`
  or `@prisma/client`. A name only in `pnpm.onlyBuiltDependencies` or
  similar is not a dependency.
- **Pins (PR-V1).** The locked version of the `prisma` package from the
  lockfile beside the package, else from the nearest lockfile above it (a
  workspace root): `pnpm-lock.yaml` (the package's importer,
  `.` for the lockfile's own directory; lockfile 5, 6 and 9; every YAML
  document is read, since recent pnpm writes an environment document
  first, and documents that lock different versions give none; the
  shared reader `internal/extract/models/lockfile`),
  `package-lock.json` (lockfile 2 and 3: `node_modules/prisma` as Node
  resolves it from the package directory upwards; lockfile 1: the root
  project's dependency) and `yarn.lock` (classic and Berry, by the
  package.json range). Several lockfiles in that directory must agree. A
  missing, inexact or ambiguous version gives `pins: null` (the assumption
  is still listed); a package that depends only on `@prisma/client` has no
  CLI pin.
- **Version gate.** The major is the locked `prisma` version's, else the
  locked `@prisma/client` version's, else a package.json range's with a
  fixed major (`^7.1.0`, `7.x`). Majors 5, 6 and 7 are read; another
  major, or none proven, gives no schema and `schema_model_not_proven`.
- **Config file**, in the package directory only, in the CLI's order:
  `prisma7.config.*` and `.config/prisma7.*` (7.10.0 and later only; when
  the version is not proven and such a file exists, the schema is not
  proven), then `prisma.config.*`, `.config/prisma.*`,
  `.config/prisma.config.*` and their `index.*` variants (extensions `.js
  .ts .mjs .cjs .mts .cts` in that order). The config is read, never run:
  the object literal of `export default defineConfig({...})` or `export
  default {...}`. A `schema` member that is one string literal (quotes, or
  a template without substitutions) names a file or a directory, resolved
  against the config file's directory; a directory's `.prisma` files,
  recursively, form one multi-file schema. A schema member that is not a
  literal (or a later spread, a computed key), a config the reader cannot
  read, or a named path that does not exist: no schema,
  `schema_model_not_proven`.
- **Default lookup.** Without a config `schema`: `./schema.prisma`, then
  `./prisma/schema.prisma`, relative to the config file's directory (the
  package directory without a config). `package.json`
  `"prisma": {"schema": ...}` is not read by Prisma 7: a
  `schema_model_unread` marker.
- **Markers.** `.prisma` files outside every discovered schema are counted
  in one `schema_model_unread` diagnostic.
- **Migrations directory (Prisma 7).** A loaded Prisma 7 config
  the reader can read whose `migrations` member is an object literal with
  a `path` that is one string literal names the Prisma Migrate directory,
  resolved against the config file's directory (independent of whether
  its `schema` is proven). It gives one info diagnostic
  `schema_model_migrations` on the config file, message `<config>: Prisma
  Migrate migrations directory (not read): <dir>`, which the server's
  `unread_tools` rule reads. A path that is not literal (a call, a
  variable, a spread or computed key in either object), absolute, or
  outside the repository gives no marker: the default rule (a
  `migrations/` directory beside the schema) applies. Prisma 5 and 6
  configs are not read for it.
- **One schema, several packages.** Packages whose schemas
  share a file form one cluster. When they all name the same schema and
  their Prisma versions give the same facts (the same major and the same
  side of every claim gate: 6.0, 6.2, 6.13 and whether the version is
  proven), the schema is claimed once (the first package by manifest
  path; `pins` only when every package locks the same version, else
  null); otherwise nothing of it is claimed, with a
  `schema_model_not_claimed` warning naming the packages. Identities are
  per file, so a schema is never claimed twice.

**Prisma 5 and 6** (developer decision 2026-11-09; verified in the
prisma/prisma sources at 5.22.0 and 6.19.3 and the 6.x history, ADR 0057
"Prisma 5 and 6"): config files are read only from 6.13.0 (the names
above without `prisma7.*`; a config `schema` comes first); 6.4.0 to 6.12.x
read `prisma.config.ts` with rules this reader does not model, so such a
file leaves the schema unproven, and so does any config file when the 6.x
minor is not proven. Then `package.json` `"prisma": {"schema": ...}`
(resolved against the package directory; a folder only from 6.7.0, when
prismaSchemaFolder is GA), then `./schema.prisma` and
`./prisma/schema.prisma` relative to the package directory; before 6.6.0
a `prisma/schema` folder beside `prisma/schema.prisma`, or alone, leaves
the schema unproven (it is a schema only with a preview feature).

## Lexer and parser

Hand-written (`lexer.go`, `parser.go`), following the grammar of the pinned
schema engine (`psl/schema-ast/src/parser/datamodel.pest` at prisma-engines
`0edf323efd1d`, Prisma 7.10.0):

- The language is line-based: a field, an enum value, a block attribute
  and a `key = value` line each end at a newline (`\n`, `\r\n`, `\r`);
  attribute arguments never span lines. Whitespace is spaces (Unicode
  space separators) and tabs.
- Blocks `model`, `view`, `type` (composite), `enum`, `datasource`,
  `generator`; Prisma ignores the rest of the line after a block's `{`
  (reported as an error when it is not a comment, since the fields on it
  are lost). A type alias, a block without a keyword and any other line
  are errors, as in Prisma.
- Fields: name, the obsolete `name: Type` colon, an optional type
  (`T`, `T?`, `T[]`, `T[]?`, the obsolete `T!` and `[T]`,
  `Unsupported("...")`), field attributes. Attributes `@path(args)` and
  `@@path(args)` with positional, named and empty named arguments and a
  trailing comma; expressions: function calls, arrays, objects, numbers,
  strings, constant paths, nested to any depth. `@x()` and `@x` are both
  kept (`Args` empty or nil), and Prisma treats them alike.
- Comments: `//`, `///` (doc comments, never a fact) and `/* */` as whole
  lines or trailing a line.
- Strings are JSON strings, as Prisma's: `\" \\ \/ \b \f \n \r \t` and
  `\uXXXX` with UTF-16 surrogate pairs; any other escape, a bad `\u`, a
  raw control character or a missing closing quote is an error.
- Errors carry line and column. Prisma loads no schema with a parse error,
  so a file with errors yields no claim; the parser goes on at the next
  line so every error and every other block is still reported, and one
  file's errors never affect another file.

Both corpora parse with no error and the inventory's counts (umami 26
models, 289 fields, 601 attributes; hoppscotch 23 models, 4 enums, 226
fields and values, 160 attributes; `TestPrismaCorpusParse`).

## Kit extension (ADR 0057, T018g0 review)

- `models.ForeignKey` states a whole constraint when `Columns` is set:
  the local columns in key order, `RefColumns` the referenced columns,
  `OnUpdate` and `Name` (the constraint name the source states, null for a
  name Prisma would generate). Each local column's record lists the
  constraint and pairs with the referenced column at its position
  (`Column`); the record entry gains `local_columns`, `columns`,
  `on_update` and `name`, and the key resolves only when every referenced
  column is known. Without `Columns` (SQLModel) the entry keeps its
  single-column shape, byte for byte.
- `models.Table.PrimaryKey` states the primary key's column order
  (`@@id([b, a])`); it is written only when it names exactly the columns
  whose `primary_key` is true, else the column order stays with a
  `schema_model_internal_error` diagnostic.
- `models.Column.IdentityName` replaces the column name in
  the identity and qualified name only: a Prisma column's identity keeps
  its field name (PR-A5) while `name`, the primary key and the index lists
  use the column name.

## Assumptions

- **PR-V1 Pins.** The Prisma version in the package's lockfile is the one
  that creates the database.
- **PR-V2 Schema deployed.** The database was created from this schema by
  Prisma Migrate or `db push`; hand-edited `migration.sql` is outside it.
- **PR-V3 Provider.** The datasource URL points to a database of the
  declared provider.
- **PR-V4 Schema location.** The schema found is the one the CLI uses (no
  `--schema` flag; the CLI runs in the package directory).
- **PR-V5 Default schema.** The datasource URL has no `schema` parameter
  (or names `public`).
- **PR-V6 Mapped provider.** Prisma's `<provider>` connector behaves as
  its source states at the locked version (facts with `verification:
  "mapped"`: mysql, sqlserver, cockroachdb).

## Claim contract

Copied from `docs/contract/claims/prisma-models.md` (ids and text identical);
`contract.go` holds the same rows, and `TestPrismaContract` checks the
three against each other.

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| PR-B1 | `datasource` with literal `provider` and optional literal `relationMode` (or its deprecated alias `referentialIntegrity`), `schemas` | group `dialect` (`postgresql`, `mysql`, `sqlite`, `sqlserver`, `cockroachdb`); `relation_mode` (`foreignKeys` when absent, else as written); `schemas` list read (gates PR-AA5) | non-literal `provider` or `relationMode`: every provider-dependent fact and every foreign-key fact unknown; `mongodb`: no tables (marker; collections out of scope); more than one datasource, or `relationMode` with `referentialIntegrity`: PR-P1; `url`, `directUrl`, `shadowDatabaseUrl` with Prisma 7: PR-P1 (Prisma 7 rejects them, P1012); with Prisma 5 and 6 and other keys: not read; Prisma 5 and 6: provider-dependent facts of mysql, sqlserver and cockroachdb unknown (their mapping is cited for 7) | PR-V3 |
| PR-B2 | `generator` block | `previewFeatures` read when a literal list (gates `views`, and `multiSchema` before 6.13.0); no claim | non-literal list: treated as no preview features | - |
| PR-B3 | `model M { ... }`, with or without `@@ignore` | one table, name `M` or PR-AA4; columns from its scalar and enum fields; schema from PR-AA5, else `public`; `@@ignore` tables claimed like any other (Migrate creates them; attribute `ignored: true`); `primary_key: null` proven when no `@id`/`@@id` (hoppscotch 2 tables) | name bound twice across the schema files: PR-P1 | PR-V2, PR-V5 (no `@@schema`) |
| PR-B4 | `view` (needs preview `views`), `type` (composite, MongoDB), unknown block keywords | none; marker diagnostic `schema_model_unread` (Migrate creates nothing for a view) | always | - |
| PR-F1 | scalar type `String Int BigInt Float Decimal DateTime Boolean Json Bytes` without PR-A6 | `type` as written; Postgres type `text`, `integer`, `bigint`, `double precision`, `numeric(65,30)`, `timestamp(3) without time zone`, `boolean`, `jsonb`, `bytea`; SQLite declared type `TEXT`, `INTEGER`, `BIGINT`, `REAL`, `DECIMAL`, `DATETIME`, `BOOLEAN`, `JSONB`, `BLOB` (oracle) | sqlite `Json` before Prisma 6.2.0 (or an unproven version): type unknown; mysql/sqlserver/cockroachdb: type from the connector source, `verification: "mapped"`, PR-V6 | PR-V1, PR-V6 |
| PR-F2 | modifiers: none, `?`, `[]` on a scalar or enum type | none: `nullable: false`; `?`: `nullable: true`; `[]`: array of the element type (`text[]`, `"E"[]`), **`nullable: true`** | scalar list on a provider without scalar lists (mysql, sqlite, sqlserver): PR-P1; `[]` on a model type: PR-R2/PR-R3; cockroachdb's `<type>[]` from the connector source (`mapped`) | PR-V1, PR-V6 |
| PR-F3 | `Unsupported("...")` with a literal | column exists, type the literal text as written, nullability by PR-F2 | anything beyond one literal: type unknown | - |
| PR-F4 | enum-typed field (enum of this schema) | column type the enum's database name (PR-E1); on sqlite `TEXT` (oracle); on mysql the inline `ENUM(...)` and on cockroachdb the enum's name from the connector source (`mapped`) | enum not found: D; sqlite before Prisma 6.2.0 (or an unproven version): type unknown; sqlserver: type unknown | PR-V6 |
| PR-A1 | `@id` / `@id()`, optional literal `map:` | single-column primary key; constraint name only from literal `map:` (generated `<table>_pkey` never claimed); with `@unique` on the same field, also PR-A2 | combined with `@@id` in the model, or two `@id`: PR-P1; other arguments (`length:`, `sort:`, `clustered:`): PK kept, those facts unknown | - |
| PR-A2 | `@unique` / `@unique()`, optional literal `map:` | `unique: true` on the column; one single-column unique entry whose kind is provider-dependent: a **unique index** on postgresql and sqlite (oracle: `index`), on mysql and cockroachdb from the connector source (`mapped index`), a UNIQUE **constraint** on sqlserver (`unique_constraints`, mapped: `flavour/mssql/renderer.rs:154-166, :228-242`); name only from literal `map:` | non-literal arguments: unique unknown; `where:` (partial unique index): D for the index (no `unique` claim, no unconditional index, table partial); other arguments: index kept, those facts unknown; provider-dependent facts unknown: the entry's kind unknown (table partial) | PR-V6 |
| PR-A3 | `@default(...)`: (a) boolean, number, string, enum value or list literal; (b) `now()`; (c) `autoincrement()` on `Int`, `BigInt` or `@db.SmallInt`/`@db.Integer`/`@db.BigInt`; (d) `dbgenerated("...")`; (e) `uuid()`, `uuid(4)`, `uuid(7)`, `cuid()`, `cuid(2)`, `nanoid()`, `nanoid(n)`, `ulid()` | (a) server default; its text (`server_default_text`, the database's text as Prisma renders it) on postgresql and sqlite for a string (`'...'`, `'` doubled), integer (postgresql: 0 to 2147483647), boolean or enum value (its mapped name, quoted), else presence only; (b) server default `CURRENT_TIMESTAMP` (also on `@db.Date` and together with `@updatedAt`); (c) `serial`/`bigserial`/`smallserial`: sequence default, presence only (the sequence name is not claimed), type kept; (d) server default, text as written (sqlite: presence only); (e) no server default, `default: "client"` | every other function (`sequence()`, `auto()`, `dbgenerated()` without argument) or a non-literal argument: default unknown; `cuid(2)`, `uuid(n)` before Prisma 6.0.0 and `ulid()` before 6.2.0 (or an unproven version): default unknown; (c) on mysql, sqlserver and sqlite (AUTO_INCREMENT, IDENTITY, AUTOINCREMENT: column properties, not a DEFAULT) and an `Int` `@id` on sqlite (always INTEGER PRIMARY KEY AUTOINCREMENT): `default` and `server_default` unknown; a `Json` literal on sqlite (Prisma's SQL is invalid there): default unknown; sqlite oracle-verified; mysql/sqlserver/cockroachdb from the connector source with `verification: "mapped"` (PR-V6), literal text presence only | PR-V1, PR-V6 |
| PR-A4 | `@updatedAt` | client-side; no server default unless PR-A3 also present (then PR-A3) | - | - |
| PR-A5 | `@map("x")` | column name `x` (identity keeps the field name) | non-literal: column name unknown, column claim refused | - |
| PR-A6 | `@db.<Native>(args)` on postgresql: `VarChar(n)`, `Char(n)`, `Text`, `Uuid`, `SmallInt`, `Integer`, `BigInt`, `Real`, `DoublePrecision`, `Decimal(p,s)`, `Timestamp(n)`, `Timestamptz(n)`, `Date`, `Time(n)`, `Timetz(n)`, `Boolean`, `Json`, `JsonB`, `ByteA`, `Inet`, `Xml`, `Bit(n)`, `VarBit(n)`, `Oid`, `Citext`, `Money` | native type as written (D6) with the Postgres type it renders (`VARCHAR(n)` -> `character varying(n)` ...); only the corpus forms and the fixture's are oracle-checked, the build's fixture covers every listed form | an unlisted native type (Prisma 6.19.3 and 7.10.0 may read it as an extension type) or argument forms other than integer literals: type unknown | PR-V1 |
| PR-A7 | `@ignore` | column claimed like any other (Migrate creates it); attribute `ignored: true` | - | - |
| PR-AA1 | `@@id([a, b], name:, map:)` with literal field names of the model | composite primary key over the mapped column names; constraint name only from literal `map:`; `name:` is client-only (no fact) | field list not literal names of this model's scalar fields: D (PK unknown) | - |
| PR-AA2 | `@@unique([..], name:, map:)` | a unique entry over the mapped columns, its kind as in PR-A2 (`index`, `mapped index`, `constraint`); name only from literal `map:`; `name:` client-only | as PR-AA1; `where:` (partial unique index): D for the index; the kind as in PR-A2 | PR-V6 |
| PR-AA3 | `@@index([..], map:, type:)` with field arguments `sort:`, `ops:` | index with the mapped columns; name only from literal `map:`; `type:` (`BTree`, `Hash`, `Gist`, `Gin`, `SpGist`, `Brin`) as the access method, `BTree` when absent | `sort:`/`ops:`/`length:`: index and columns kept, those facts unknown; `where:` (partial index): D for the index | - |
| PR-AA4 | `@@map("t")` | table name `t` (identity keeps the model name) | non-literal: table name unknown, table claim refused | - |
| PR-AA5 | `@@schema("s")` on a model or enum, `s` listed in the datasource `schemas` | table (enum) schema `s` (no preview feature from 6.13.0; before it the `multiSchema` preview feature) | `schemas` absent or `s` not listed, or a model or enum without `@@schema` while `schemas` is listed: PR-P1; before 6.13.0 without the preview feature: `schemas` PR-P1, `@@schema` schema unknown (also for a Prisma 6 version not proven) | - |
| PR-R1 | owning relation field `@relation(fields: [..], references: [..], onDelete:, onUpdate:, map:)` (optional positional or `name:` relation name), literal lists of equal length | under `foreignKeys`: one `foreign_key` (composite when several fields) from the mapped columns to the referenced model's mapped table and columns; `on_delete` as written, else `Restrict` (`NoAction` on sqlserver) when any `fields:` scalar is required and `SetNull` otherwise; `on_update` as written, else `Cascade`; name only from literal `map:`. Under `relationMode = "prisma"`: no foreign key, the table's `foreign_keys: []` proven, the relation listed in attribute `emulated_relations` (not an edge) | referenced model unknown, lists not literal or of different lengths: D for that relation (table partial); mysql and cockroachdb: the foreign-key columns' `index` unknown (the database may add one); default actions where provider-dependent facts are unknown: unknown; mysql, sqlserver and cockroachdb: foreign keys and default actions from the connector source (`mapped`) | PR-V2, PR-V6 |
| PR-R2 | back-relation fields (`T[]`, `T?` without `fields:`) | no column | - | - |
| PR-R3 | implicit many-to-many (list fields on both sides, no `fields:`), both models with a single-field `@id` | join table `_<name>` with `<name>` the explicit relation name or `<A>To<B>` (model names, not `@@map`, byte order); columns `A` and `B` (`nullable: false`, typed as the id of the first and second model); index on `B`; key `("A","B")`: on postgresql from Prisma 6.0.0 a primary key, on postgresql with Prisma 5 and on sqlite a unique index (no primary key); with Prisma 7 on mysql and cockroachdb a unique index (`mapped index`) and on sqlserver a UNIQUE constraint (`constraint`), no primary key, from the connector source (`sql_schema_calculator.rs:349-354`); where the key is claimed on postgresql or sqlite, under `foreignKeys` two foreign keys `ON DELETE CASCADE ON UPDATE CASCADE`; schema of model A's table (`public` without PR-AA5). Self-relations included (both columns reference the same table) | an id that is composite or missing: PR-P1 (Prisma rejects it); an id not known: no join table; join tables are not claimed when either side is a view; mysql, sqlserver and cockroachdb: foreign keys unknown, and the key too before Prisma 7 | PR-V2, PR-V5, PR-V6 |
| PR-E1 | `enum E { V @map("v") ... @@map("e") @@schema("s") }` | enum values (mapped) and type name (mapped), attribute of the columns that use it (no node; T018b1 has no enum kind) | non-literal map: value or name unknown | - |
| PR-P1 | Prisma validation failures provable from the files: duplicate block names, `@id` with `@@id` or two `@id`, a scalar list on a provider without lists, `@@schema` rules (PR-AA5), more than one datasource, `relationMode` with `referentialIntegrity`, an implicit many-to-many side without a single `@id`, `view` without the `views` preview feature, a datasource `url`, `directUrl` or `shadowDatabaseUrl` with Prisma 7 ("schema rejected by the pinned Prisma version") | diagnostic `schema_model_import_failure` (row named), no claim for the model (or, for whole-schema failures, the schema) | other failures: assumed absent | PR-V2 |
| D | any other block, field form, attribute or argument | nothing; the table or column is partial with `D` as reason, or a marker diagnostic for whole blocks | always | - |

## Records

One model group per discovered schema: `set: "models/prisma/<schema
path>"` (the schema file, or the directory of a multi-file schema),
`dialect` the literal provider, `pins` `{"prisma": "<locked version>"}` or
null. Identities `table:<file>#<Model>` and `column:<file>#<Model>.<field>`
(the field name, PR-A5; the record's `name` is the column name).

- **Table.** `name` (PR-AA4 or the model name), `schema` (`public` on
  postgresql and cockroachdb without `@@schema`, PR-V5; null, the default
  schema, on mysql and sqlserver; `@@schema`'s name under PR-AA5;
  `schema_known: false` where PR-AA5 refuses), `primary_key` (column
  names in key order; `[]` proven when the model has no `@id`/`@@id`),
  `primary_key_name` (literal `map:` only), `indexes` (every `@unique`,
  `@@unique` and `@@index`: `{name, columns, unique, method}`, `name` only
  from a literal `map:`, `method` the `@@index` `type:` or `BTree` on
  postgresql, null otherwise and for unique indexes), `unique_constraints`
  (`[]` on every provider but sqlserver; on sqlserver every
  `@unique` and `@@unique` without `where:` as `{name, columns}`), `model`, `relation_mode`, `ignored` (`@@ignore`),
  `assumptions` per table (PR-V1 to PR-V4, PR-V5 for a table in the
  default schema of postgresql or cockroachdb, PR-V6 where the table holds
  a mapped fact: a mapped provider with Prisma 7), `verification`
  (`oracle` on postgresql and sqlite, else `mapped`).
- **Column.** `type` (PR-F1/PR-A6: the scalar type as written; PR-F3 the
  Unsupported literal; PR-F4 the enum's database name, on MySQL the inline
  `ENUM(...)`), `sql_type` (postgresql: the catalog's `format_type`
  spelling, `[]` for a list; mapped providers: the DDL text Prisma
  renders), `native_type` (`@db.` attribute as written, without `@db.`),
  `length`, `precision`, `scale`, `timezone` (postgresql only, from the
  catalog type), `nullable` (PR-F2), `primary_key`, `unique`, `index`
  (true for the only column of an `@@index`), `default` (`absent`,
  `server` for forms (a)-(d), `client` for (e)), `default_value` (the
  `@default` argument as written), `server_default`,
  `server_default_text` (the database's text, never the Prisma literal:
  for (a) on postgresql the text Prisma renders for a string `'...'` with
  `'` doubled, an integer, a boolean or an enum value (its mapped name,
  quoted), else null with a reason; (b) `CURRENT_TIMESTAMP` per provider;
  (c) only CockroachDB's `unique_rowid()`: a serial column's sequence
  default, MySQL's AUTO_INCREMENT and SQL Server's IDENTITY claim
  presence only; (d) the `dbgenerated` text as written), `enum` (`{name,
  database_name, values, schema}`), `ignored` (`@ignore`),
  `foreign_keys` (`[]` proven, or the constraints of row PR-R1:
  `{table, schema, column, local_columns, columns, on_delete, on_update,
  name, status}`, a composite key listed on each of its columns).
- **Relations.** Foreign keys are resolved after every model's
  table is built (PR-R1). Under `relationMode = "prisma"` the table's
  `emulated_relations` lists `{field, model, table, columns,
  references}` and no column has a foreign key. On mysql and cockroachdb a
  foreign key's columns have `index` unknown (the database may add one)
  and the table's constraint set is partial. A relation the rows cannot
  resolve (an unknown field, a refused referenced model) is D: its columns'
  foreign keys unknown.
- **Join tables (PR-R3).** `table:<file of model A>#_<relation name>`,
  `model: null`, `join_table: {relation, models, fields}`, columns `A`
  and `B` typed as the ids; an index on `B`, the key (a primary key
  `(A, B)` on postgresql from Prisma 6.0.0, a unique index on `(A, B)` on
  postgresql with Prisma 5 and on sqlite) and the two cascading foreign
  keys; on mysql, sqlserver and cockroachdb the foreign keys are unknown
  with a reason, and with Prisma 7 the key is mapped (no primary key; a
  unique index on mysql and cockroachdb, a UNIQUE constraint in
  `unique_constraints` on sqlserver), unknown before.
- **Unique kind.** Whether a `@unique` or `@@unique` (and a
  mapped join table's key) is a unique index or a unique constraint is
  provider-dependent: an index on postgresql and sqlite (oracle), on mysql
  (`UNIQUE INDEX`, `flavour/mysql/renderer.rs:225-253`) and cockroachdb
  (`CREATE UNIQUE INDEX`, the postgres renderer `:361-391`), mapped; a
  constraint on sqlserver (`ADD CONSTRAINT ... UNIQUE` or a table
  `CONSTRAINT ... UNIQUE`, `flavour/mssql/renderer.rs:154-166, :211-242`),
  mapped. Where provider-dependent facts are unknown (a non-literal
  provider, a mapped provider before Prisma 7) the entry is in neither
  list and the constraint set is partial; the column's `unique: true` is
  provider-independent and stays. A partial unique index (`where:`) is D
  like a partial `@@index`.
- **Validation failures (PR-P1).** A model-level failure withdraws that
  model's table, a schema-level one the schema, with
  `schema_model_import_failure` naming the row.
- **Verification.** Provider-independent facts (name, nullability, keys,
  unique, index, foreign-key absence) are `oracle` on every SQL provider;
  provider-dependent facts (type and its sizes, default, server default)
  are `oracle` on postgresql and sqlite and `mapped` on mysql, sqlserver
  and cockroachdb (PR-V6); with a non-literal or unlisted provider they
  are unknown and so is every foreign key (PR-B1).
- **Absence only where proven.** An unread field attribute (D) withholds
  that column's absences (not unique, no default, no key, no foreign
  key); an unread block attribute withholds every column's; an unread
  `@@unique` or `@@index` (a `where:` partial index, an unlisted `type:`)
  withholds only "not unique" or "not indexed".
- **Unknown defaults.** `sequence()`, `auto()`, `dbgenerated()` without
  an argument, any other function, `map:` outside sqlserver, `@updatedAt`
  without `@default` (the client sets it: `server_default: false`,
  `default` unknown), on MySQL any default of a Json or Bytes column (not
  rendered), and a client function the locked version does not have
  (`cuid(2)`, `uuid(n)` before 6.0.0, `ulid()` before 6.2.0).
- **No DEFAULT, no default claim.** `autoincrement()` on mysql
  (AUTO_INCREMENT), sqlserver (IDENTITY) and sqlite (AUTOINCREMENT on an
  `Int` id; nothing at all on a `BigInt` id), and every `Int` `@id` on
  sqlite (always `INTEGER PRIMARY KEY AUTOINCREMENT`, which the database
  fills): `default` and `server_default` unknown with a reason, never
  `true` (decision 2026-11-09; the record has no
  autoincrement attribute). A `Json` literal default on sqlite (Prisma
  writes `DEFAULT {}`, which SQLite rejects) is unknown too.
- **Default text.** Postgres keeps a negative or non-int4 integer
  constant as a cast string (`'-1'::integer`), so those claim presence
  only; SQLite keeps a `dbgenerated` expression without its outer
  parentheses, so its text is presence only there.
- **Prisma 5 and 6.** Rows apply as for 7 where Prisma's source is the
  same (postgresql types, defaults, keys, indexes and foreign keys at
  5.22.0, 6.19.3 and 7.10.0); the version gates are in ADR 0057 "Prisma 5
  and 6". On mysql, sqlserver and cockroachdb the provider-dependent facts
  are unknown (their mapping is cited for 7); sqlite's are oracle-checked
  at 5.22.0, 6.19.3 and 7.10.0, with `Json` fields and enums read from
  6.2.0 (5.22.0 and 6.1.0 reject them).
- **Prisma 7 datasource URLs.** `url`, `directUrl` and
  `shadowDatabaseUrl` in the datasource are rejected by Prisma 7 (P1012,
  psl `validate/datasource_loader.rs:132-142`, checked with the 7.10.0
  CLI): PR-P1 for the whole schema, "schema rejected by the pinned Prisma
  version".
- **Prisma Migrate.** `migrations/` beside a schema is not
  read (the SQL pass reads sqlx sets); the server lists it as the unread
  tool `prisma-migrate` (`docs/cli/serve.md`, `unread_tools`).

## Provider mapping (PR-V6)

Facts with `verification: "mapped"` come from Prisma's connector source at
the locked version's engines (prisma-engines `0edf323efd1d`, Prisma
7.10.0; paths under `psl/psl-core/src/builtin_connectors/` and
`schema-engine/connectors/sql-schema-connector/src/`), never from the docs
page:

| fact | mysql | sqlserver | cockroachdb |
|---|---|---|---|
| scalar types (PR-F1) | `mysql_datamodel_connector.rs:75-86`, rendered by `flavour/mysql/renderer.rs:426-459` (`Boolean` -> `BOOLEAN`, `:429`): `VARCHAR(191)`, `INTEGER`, `BIGINT`, `DOUBLE`, `DECIMAL(65, 30)`, `DATETIME(3)`, `BOOLEAN`, `JSON`, `LONGBLOB` | `mssql_datamodel_connector.rs:64-80`, `flavour/mssql/renderer.rs:525-555`: `NVARCHAR(1000)`, `INT`, `BIGINT`, `FLOAT(53)`, `DECIMAL(32,16)`, `DATETIME2`, `BIT`, `NVARCHAR(1000)` (Json), `VARBINARY(max)` | `cockroach_datamodel_connector.rs:71-83`, `flavour/postgres/renderer.rs:620-659`: `STRING`, `INT4`, `INT8`, `FLOAT8`, `DECIMAL(65,30)`, `TIMESTAMP(3)`, `BOOL`, `JSONB`, `BYTES` |
| enum column (PR-F4) | inline `ENUM('v', ...)` (`flavour/mysql/renderer.rs:398-401`) | none (enums unsupported: type unknown) | the enum's name (`flavour/postgres/renderer.rs:549-554`) |
| scalar list (PR-F2) | unsupported: type unknown (PR-P1 in G1B3) | unsupported: type unknown | `<type>[]` (`renderer.rs:653-657`) |
| `now()` (PR-A3 b) | `CURRENT_TIMESTAMP(3)` (`flavour/mysql/renderer.rs:514-518`) | `CURRENT_TIMESTAMP` (`flavour/mssql/renderer.rs:574`) | `CURRENT_TIMESTAMP` (`flavour/postgres/renderer.rs:885`) |
| `autoincrement()` (PR-A3 c) | `AUTO_INCREMENT` (`flavour/mysql/renderer.rs:41`, `:389-390`): a column property, default not claimed | `IDENTITY(1,1)` (`flavour/mssql/renderer.rs:49-50`): a column property, default not claimed | `unique_rowid()` (`flavour/postgres/schema_calculator.rs:82-84`, `renderer.rs:887`) |
| other defaults (PR-A3 a, d, e) | literal and `dbgenerated` rendered (`renderer.rs:508-523`), except Json and Bytes columns (`:22-34`: default unknown); client functions are Prisma-level (`sql_schema_calculator.rs:595-630`) | rendered (`flavour/mssql/renderer.rs:562-578`); `map:` names the default constraint | rendered (`flavour/postgres/renderer.rs:836-888`) |
| native types (`@db.*`) | not read (type unknown) | not read | not read |
| unique index kind (PR-A2, PR-AA2, PR-R3 key) | `UNIQUE INDEX` (`flavour/mysql/renderer.rs:225-253`, sql-ddl `mysql.rs:189-197`, `:333-340`): an index | UNIQUE constraint, a filtered one stays an index (`flavour/mssql/renderer.rs:154-166`, `:211-242`): `unique_constraints` | `CREATE UNIQUE INDEX` (`flavour/postgres/renderer.rs:361-391`, sql-ddl `postgres.rs:365-368`): an index |
| join table key (PR-R3) | unique index on (A, B), no primary key (`sql_schema_calculator.rs:349-354`) | as mysql, as a UNIQUE constraint | as mysql (`flavour/postgres/schema_calculator.rs:216-222`) |

## Oracle

`scripts/oracle/prisma_catalog.sh` (with `prisma_catalog.py`; never run
by CI) installs each Prisma CLI from npm into scratch, prints the SQL
Migrate would run (`migrate diff --from-empty --to-schema ... --script`
on 7, with a scratch `prisma.config.ts` holding a literal URL;
`--to-schema-datamodel` and a `url = env("DATABASE_URL")` added to the
datasource on 5 and 6), applies it to a fresh database (postgresql: the
scratch Postgres 17 of `scripts/cloud-postgres.sh`, with the `citext`,
`pg_trgm` and `btree_gist` extensions the fixture's types and indexes
need; sqlite: a scratch SQLite file) and dumps the catalog (pg_catalog;
SQLite `sqlite_master` and `pragma table_xinfo`, `index_list`,
`index_xinfo`, `foreign_key_list`). `TestPrismaOracle` (skipped unless
`GRAPHVEIN_PRISMA_ORACLE` names the dump and `GRAPHVEIN_PRISMA_ORACLE_ROOT`
the directory its runs are relative to) links each run's directory, with
the lockfile's `prisma` pin rewritten to the run's version, and compares
every claimed table, column set, type (and its sizes, enum name, schema
and values), nullability, primary key and its name, unique and index
facts and lists, default, server default and its text, and foreign key
(columns, target, actions, name) with the catalog; a claimed fact the
catalog lacks or contradicts is a disagreement (precision stop S1). A
SQLite column declared `AUTOINCREMENT` with any default claim is one too.
A claimed default text agrees with the catalog's when they are equal or
the catalog adds a cast: a type name, or the column's own `format_type`
with or without its modifier (`'it''s'::character varying` on a
`character varying(20)` column; `TestDefaultTextAgrees`).

Runs: `--fixture` (the `pg` and `sqlite` packages with 7.10.0, 6.19.3 and
5.22.0; `multi` and `emulated` with 7.10.0; `v5` with 5.22.0; `v6` with
6.19.3) and `--corpus` (umami with 7.10.0, hoppscotch with 7.9.1). Prisma
validates a schema as a whole, so the forms Prisma or Postgres rejects are
removed before a run and listed in the dump (`dropped`): the PR-P1 models,
the D forms (an unknown attribute, refused native types and defaults, a
relation naming an unknown field, `@@fulltext`, a partial index, a
composite type, the partial unique indexes), on 5.22.0 `cuid(2)` and `ulid()`, on sqlite the Json
literal default and, on 5.22.0, the Json and enum fields. None of them is
claimed.

## Revision after the independent review (2026-11-09)

Step G1F, class rules (never case patches), each with a fixture case; the
contract rows changed 1:1 in the task file, `docs/extract/schema-prisma.md`
and `contract.go` (PR-B1, PR-F1, PR-F2, PR-F4, PR-A2, PR-A3, PR-AA2,
PR-R1, PR-R3, PR-P1).

- **A1 Unique kind per provider.** On sqlserver a `@unique`/`@@unique`
  without a predicate, and the implicit many-to-many key, is a UNIQUE
  constraint (prisma-engines `0edf323efd1d`
  `flavour/mssql/renderer.rs:154-166, :211-242`), not a unique index. The
  kind is claimed per provider: `unique_constraints` on sqlserver
  (`mapped`), `indexes` on postgresql and sqlite (`oracle`), on mysql
  (`UNIQUE INDEX`) and cockroachdb (`CREATE UNIQUE INDEX`) `indexes`
  (`mapped`); where provider-dependent facts are unknown the entry is in
  neither list and the constraint set is partial. PR-A2 and PR-AA2 gain
  the forms `index`, `mapped index`, `constraint`; PR-R3 claims the
  mapped providers' key with Prisma 7 (no primary key; foreign keys stay
  unknown). Fixture: `sqlserver`, `mysql`, `cockroach`, `dynamic`.
- **B1 Oracle cast rule.** The comparator accepts a default cast equal to
  the column's `format_type`, with or without its modifier
  (`'it''s'::character varying`). Fixture: `Defaults.quoted`
  (`@db.VarChar(20)`) and `Defaults.fixed` (`@db.Char(2)`).
- **B2 Partial unique indexes.** `@unique(where:)` and `@@unique(...,
  where:)` (the partialIndexes preview, Prisma 7) are D like
  `@@index(where:)`: no `unique` claim, no unconditional index, the table
  partial with a reason. Fixture: `Partial.slug`, `Partial` `@@unique`.
- **B3 One schema, two packages.** Packages whose schemas share a file
  claim the schema once when they name the same schema and their versions
  agree on every claim gate (`claimGates`; pins null when the exact
  versions differ), else nothing, with a `schema_model_not_claimed`
  warning naming the packages. No duplicate identities. Fixture: `twin`
  (7.10.0 and 7.9.1: claimed once), `split` (7.10.0 and 6.19.3: not
  claimed).
- **C1 Prisma 7 datasource URLs.** `url`, `directUrl` or
  `shadowDatabaseUrl` with a Prisma 7 pin: PR-P1 for the schema ("schema
  rejected by the pinned Prisma version"; P1012 from the 7.10.0 CLI, psl
  `validate/datasource_loader.rs:132-142`). Fixture: `urlv7`.
- **C2 Emulated relations in the Schema view.** The table detail lists
  `emulated_relations` under "Relations enforced by Prisma, not the
  database", as text, without foreign-key chips (`EmulatedRelations`,
  vitest `prisma.test.tsx`).
- **C3 Dialect of Prisma groups.** `/schema/sets` serves a model group's
  common stored `dialect` (the datasource provider; null for SQLModel).
- **C4 PR-V6 in the assumptions.** Every row whose mapped facts rest on
  PR-V6 lists it (PR-F1, PR-F2, PR-F4, PR-A2, PR-A3, PR-AA2, PR-R1,
  PR-R3); a table and its group list PR-V6 where they hold a mapped fact
  (a mapped provider with Prisma 7), not merely on a mapped provider.
- **C5 Prisma migrations (rule).** `prisma/migrations` is
  not read by the SQL pass (sqlx only). The server's `unread_tools` adds
  Prisma Migrate: a `migrations/` directory beside a stored `.prisma` file,
  or the directory a `schema_model_migrations` marker names (a Prisma 7
  config's literal `migrations.path`), with
  `migration_lock.toml` or `*/migration.sql` is
  `{"tool": "prisma-migrate", "path", "marker", "files"}`, shown as
  "Prisma migrations found, not read yet"; a model table's history no
  longer says that no migration touches it, only that none Graphvein
  reads does.

Oracle (0 disagreements): fixture 10 runs (pg 7.10.0/6.19.3: 1168 facts
each, pg 5.22.0: 1149, sqlite 7.10.0/6.19.3: 406 each, sqlite 5.22.0:
378, multi 63, emulated 80, v5 68, v6 66); corpus umami 7.10.0 2513 facts,
hoppscotch 7.9.1 1814 facts.

## Tests

`internal/extract/models/prisma`: `TestPrismaContract` (the docs table
equals the task file's, `contract.go` has the same rows in the same order
with the same assumptions, and every listed form is named in its row),
parser tests over small inline schemas (`TestParseBlocks`,
`TestParseArguments`, `TestParseStrings`, `TestParseLegacyAndEdgeForms`,
`TestParseErrors` with malformed input, `TestParseFilesIndependent`),
discovery tests (`TestDiscoverCorpusShapes`, `TestDiscoverRules`,
`TestDiscoverVersions`, `TestDiscoverMarkers`, `TestReadConfigSchema`), and
the env-gated `TestPrismaCorpusParse` and `TestPrismaCorpusDiscover`
(skipped unless `GRAPHVEIN_PRISMA_CORPUS` names a directory holding the
pinned `umami` and `hoppscotch` checkouts). The kit's extension is tested
in `internal/extract/models` (`TestForeignKeyConstraint`,
`TestPrimaryKeyOrder`, `TestColumnIdentityName`). `TestPrismaOracle`
(env-gated, above) compares the claims with the oracle's catalog.

Claims: `TestPrismaFixture` runs `prisma.Link` over
`testdata/fixtures/prisma-models/` (golden
`testdata/fixtures/prisma-models.golden.json`), runs the shared claim
checks (`modeltest.CheckClaims`; every claim and reason names a contract
row) and checks that every listed form of a multi-form row and every row
this unit implements is reached; `TestPrismaFixtureFacts` spells out the
decided facts. The env-gated `TestPrismaCorpusLink` checks the corpora's
table, column and claim counts and the investigation oracle baseline counts.

Step G1B3: `TestPrismaIndexedFixture` indexes the fixture through the
indexer and owns the golden `testdata/fixtures/prisma-models.golden.json`
(a store snapshot, then a warm replay that parses nothing);
`TestPrismaFixture` is the full contract test (every row's branch reached,
every listed form, every tag a row); `TestPrismaCatalogue` checks the
registered catalogue against this document; `TestDiscoverLegacy` covers
the Prisma 5 and 6 discovery rule. The fixture's packages are listed in
its README.

Review fixes (2026-11-09): `TestPrismaFixtureFacts` checks the unique
kinds per provider, the partial unique forms, the shared schema rule
(`twin`, `split`) and the Prisma 7 URL rejection (`urlv7`);
`TestClaimGates` checks that every `since` gate is in `claimGates`;
`TestDefaultTextAgrees` the oracle's cast rule.

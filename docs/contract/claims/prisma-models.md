# Claim contract: Prisma models

The claim contract of the Prisma model extractor (ADR 0057) as recorded when it was
designed (task T018g1, internal record). `docs/extract/schema-prisma.md` and the code
table in `internal/extract/models/prisma` must equal these rows (`TestPrismaContract`).

## Claim contract

Finalised from the inventory and the oracle (the investigation). Row kinds: **B**
blocks; **F** field types and modifiers; **A** field attributes; **AA**
block attributes; **R** relations; **E** enums; **P** provable validation
failures; **D** the single default branch. Every form the corpora use has
exactly one row (counts per row: the T018g1 inventory (internal record)); the
forms only the fixture uses come from the same oracle runs. The contract
test keeps `docs/extract/schema-prisma.md`, the Go table and the code 1:1
(plan, shared piece 4). 26 rows: 25 rows plus D; PR-B2, PR-B4, PR-A4 and PR-R2 have no claim side (they read, mark or refuse only).

Provider rule (developer decision 2026-11-09, replaces the G5 default):
- **postgresql:** facts verified by the oracle (`verification: "oracle"`).
- **sqlite:** the oracle also runs on SQLite (cheap, no server: Prisma's
  generated SQL applied to a scratch SQLite file, catalog read through
  `sqlite_master` / `pragma table_info`, `index_list`, `foreign_key_list`);
  facts that agree are `verification: "oracle"`. The implementation added this oracle
 and the SQLite mapping in rows PR-F1, PR-F4, PR-A3 and
  PR-R3.
- **mysql, sqlserver, cockroachdb:** provider-dependent facts (SQL type,
  default, foreign keys and their actions, index method, scalar-list
  columns) are claimed from Prisma's own connector source at the locked
  Prisma version (the per-provider type and default mapping in the schema
  engine's connectors, cited by file and line in the contract, never from
  the docs page) with `verification: "mapped"` and the assumption PR-V6:
  "Prisma's <provider> connector behaves as its source states at the
  locked version". The UI marks them "mapped, not verified" (popover:
  "From Prisma's <provider> mapping; not checked against a <provider>
  database"; an "unverified" chip on the table). If a scratch MySQL or SQL
  Server becomes cheap, the same facts move to `oracle` with no UI change.
- **Unknown in every provider:** what Prisma itself leaves to the database
  or the environment: the default schema without `?schema=` (PR-V5
  claims `public` under the stated assumption, developer decision), and
  collations, engine and charset options.
- **mongodb:** no tables (marker; out of scope).
Provider-independent facts (names, column set, nullability of non-list
fields, key and index column lists) are claimed for every SQL provider.
`verification` is the per-fact field T018g0 adds to the shared model
record (decision 2026-11-09).

Developer decisions on the investigation (2026-11-09): PR-V5 claims `public` under the
stated assumption; `@unique`/`@@unique` are recorded as indexes with
`unique: true`; implicit many-to-many join tables (including
self-relations) are claimed; providers as above.

Lexing: `//` comments and `///` doc comments are skipped; a doc comment
is never a fact. Strings are Prisma strings (double-quoted, backslash
escapes). `@x()` equals `@x` (oracle: umami `@id()`, `@unique()`).

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

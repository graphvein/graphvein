# ADR 0057 - Prisma models

**Status:** Accepted (delegated review, 2026-11-09)

Prisma model schemas (`docs/contract/claims/prisma-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"): file type and discovery, the hand-written lexer and parser,
node shapes and the unique-index decision, row and assumption
ids, the oracle, and the model kit's extension for Prisma's keys (T018g0 review). Written with step G1B1; step G1B0, which ran first, recorded the
store read that keeps `/api/v0/schema/sets` fast for the larger model
schemas Prisma brings (last section; developer decision 2026-11-09: the
note goes into this ADR because ADR 0058 was accepted before G1B0
finished).

## Context

Milestone M2 of the project plan asks for ORM models in each first-tier language, Prisma first
for TypeScript, each with a pinned corpus and its own claim contract.
Prisma states a whole database schema in `.prisma` files: the tables
Prisma Migrate creates follow from the file, the pinned Prisma version and
the datasource's provider. Two corpora are pinned (umami 7.10.0,
hoppscotch 7.9.1; plan decision G4). The investigation inventoried every
form they use, checked the semantics in the pinned Prisma sources and ran
an oracle (Prisma's own SQL applied to a scratch Postgres 17, then the
catalog); the claim contract (26 rows) is finalised from it. The shared
model kit (ADR 0058) emits the records; its review (C4) left composite
foreign keys, `on_update`, constraint names and a stated primary-key order
to Prisma.

## Options considered

1. **Reading the schema.** (a) A tree-sitter grammar for the Prisma schema
   language (a community grammar exists): a new grammar in the parser
   pool, a cached payload, and a grammar that is not Prisma's own, so its
   accepted language differs from Prisma's in places nobody checked.
   (b) Prisma's own parser (the Rust `psl` crate or its WebAssembly build
   from `@prisma/prisma-schema-wasm`): exact, but a new runtime dependency
   (a Wasm module of several MB per version, or cgo). (c) A hand-written
   lexer and parser in Go that follows Prisma's grammar file
   (`datamodel.pest`) rule by rule, at link time over retained sources, as
   T018a reads SQL.
2. **Finding the schema.** (a) Every `.prisma` file is a schema. (b) The
   CLI's rule: the package that depends on Prisma, its config file's
   literal `schema`, else `./schema.prisma` then `./prisma/schema.prisma`.
3. **Unique attributes.** (a) `@unique`/`@@unique` as unique constraints
   (as an ORM's docs describe them). (b) As unique indexes, as Prisma
   Migrate creates them (`CREATE UNIQUE INDEX`; no `u` constraint in any
   oracle run).
4. **Kit extension for Prisma's keys.** (a) A Prisma-only foreign-key
   record beside the kit's. (b) Optional fields on the kit's `ForeignKey`
   and `Table` that only Prisma sets, written only when set, so SQLModel's
   records stay byte-identical.

## Decision

- **Parser: option 1(c).** `internal/extract/models/prisma` (`lexer.go`,
  `parser.go`) follows `psl/schema-ast/src/parser/datamodel.pest` and
  `parse_expression.rs` at prisma-engines `0edf323efd1d` (the 7.10.0
  engines): line-based fields and attributes, spaces and tabs as
  whitespace, JSON strings with Prisma's escapes, the rest of a block's
  first line ignored after `{`, every block keyword (model, view, type,
  enum, datasource, generator) and the obsolete forms its grammar still
  accepts kept in the tree so the claim rows can refuse them. Whatever
  Prisma's parser rejects (a type alias, a block without a keyword, an
  invalid line, a bad escape) is a parse error of the file; Prisma then
  loads no schema, so a file with an error gives no claim. The parser
  never fails and goes on at the next line, so a bad file reports every
  error and never affects another file. No grammar, no cached payload, no
  new dependency (pnpm lockfiles are read with the YAML package already in
  `go.mod`).
- **Discovery: option 2(b)**, as verified in the investigation (the
  default lookup tries `./schema.prisma` first; `package.json` `prisma`
  is not read by Prisma 7; the lockfile may be at the
  workspace root). Config files are read, never run: only a literal
  `schema` in `export default defineConfig({...})` or `export default
  {...}`; anything else gives no schema and `schema_model_not_proven`. A
  directory named by the config is a multi-file schema of its `.prisma`
  files, recursively (Prisma's schema-files-loader). The rule is applied
  for Prisma 7 (the major from the lockfile, else a package.json range
  with a fixed major); `prisma7.config.*` counts from 7.10.0 on. Prisma 5
  and 6 have their own rule (section "Prisma 5 and 6").
- **Unique attributes: option 3(b)**. Records list them under
  `indexes` with `unique: true` and set the column's `unique: true`; never
  under `unique_constraints`.
- **Node shapes.** The kit's (ADR 0058): `table` and `column` nodes,
  `source: "model"`, `tool: "prisma"`, group `models/prisma/<schema root>`
  (the schema file, or the directory of a multi-file schema), `dialect`
  the datasource's provider, `pins` `{"prisma": "<locked version>"}` or
  null, per-fact `verification` (`oracle` for postgresql and, after G1B4,
  sqlite; `mapped` with PR-V6 for mysql, sqlserver and cockroachdb).
  Identities `table:<schema file>#<Model>` and
  `column:<schema file>#<Model>.<column>`; an implicit many-to-many join
  table is `table:<file of model A>#_<relation name>` with columns
  `.A` and `.B`, its `model` attribute null and a
  `join_table` attribute (`relation`, `models`, `fields`).
- **Row and assumption ids** carry the `PR-` prefix (PR-B1 ... PR-P1, D;
  PR-V1 ... PR-V6), so they never collide with SQLModel's within the
  `models` source. `contract.go` holds the rows; `TestPrismaContract` keeps
  it, `docs/extract/schema-prisma.md` and the task file's table 1:1.
- **Kit extension: option 4(b)** (T018g0 review). `models.ForeignKey`
  gains `Columns` and `RefColumns` (a composite key: each local column's
  record lists the constraint and pairs, through `Column`, with the
  referenced column at its position), `OnUpdate` and `Name` (only a name
  the source states; a generated name is never claimed). They are written
  (`local_columns`, `columns`, `on_update`, `name`) only when `Columns` is
  set, and such a key resolves only when every referenced column is a
  known column of the target. `models.Table.PrimaryKey` states the key's
  order (`@@id([b, a])`); the core writes it only when it names exactly the
  key columns, else the column order with `schema_model_internal_error`.
  `models.Column.IdentityName` replaces the column name in the
  column's identity and qualified name only, so a Prisma column keeps its
  field name in the identity (`column:<file>#<Model>.<field>`) while its
  name, the primary key and the index lists use the `@map`'d column name.
  SQLModel sets none of them: its goldens are unchanged.
- **Oracle**: `prisma migrate diff --from-empty --to-schema
  <schema> --script` with the corpus pin installed in scratch, the SQL
  applied to a scratch Postgres 17 (`scripts/cloud-postgres.sh`), the
  catalog dumped as JSON with T018a2's queries; `TestPrismaOracle`
  (env-gated) compares every claimed fact. Rejected: feeding Prisma's SQL
  through Graphvein's own migration pass, which would test that pass's
  coverage of Prisma's DDL rather than the Prisma claims.

## Consequences

- Prisma's language is read exactly as far as its grammar file goes; a
  later Prisma release that changes the grammar needs the parser checked
  against the new `datamodel.pest` (the version gate refuses majors other
  than 5, 6 and 7, not new minors).
- Both corpora parse with no error and the inventory's counts; discovery
  finds their schemas at the paths the investigation recorded.
- The kit's records gain optional foreign-key keys and a stated primary
  key order; SQLModel's output is byte-identical (goldens unchanged).

## Known gaps

- Config files are not run: a schema path computed in code, a config that
  re-exports another module, or a `module.exports` config gives no schema
  (a diagnostic, never a guess).
- `--schema` flags in scripts are not read (assumption PR-V4).
- `bun.lock` and `npm-shrinkwrap.json` are not read for pins (`pins:
  null`).
- A multi-file schema found only through a config's directory is read with
  every `.prisma` file the scanner retained; files the repository's ignore
  rules hide from the scanner are missing from it.

## Relations, failures, versions and wiring

### Decisions

- **Foreign keys (row PR-R1)** are resolved once every model's table is
  built: one constraint per owning relation through the kit's
  `ForeignKey.Columns` (composite keys on each local column), default
  `on_delete` as Prisma's `default_on_delete_action` and
  `referential_arity` compute it (Restrict, NoAction on SQL Server, else
  SetNull), `on_update` Cascade. Under `relationMode = "prisma"` (and its
  deprecated alias `referentialIntegrity`, which Prisma 5 to 7 still
  accept) there is no constraint: the table's `emulated_relations` lists
  `{field, model, table, columns, references}` and the columns'
  `foreign_keys: []` are proven. On MySQL and CockroachDB, which may add
  an index for a foreign key on their own, the columns' `index` is unknown
  and the table's constraint set partial.
- **Implicit many-to-many (PR-R3)**: relation fields pair by model pair
  and relation name, as `ingest_relation` does; two list fields without
  `fields:` make the join table. Its key is claimed only where it is a
  primary key: postgresql from Prisma 6.0.0 (`JoinTableUniquenessConstraint`).
  Other providers and Prisma 5 use a unique index on (A, B), which the row
  does not list: the key is unknown there, and so are the join table's
  foreign keys (their actions are per flavour,
  `m2m_foreign_key_action`).
- **Validation failures (PR-P1)**: model-level failures (two `@id`, `@id`
  with `@@id`, a scalar list on mysql, sqlite or sqlserver, the `@@schema`
  rules, an implicit many-to-many side without a single-field id) withdraw
  that model's table; schema-level ones (a duplicate block name, more than
  one datasource, `relationMode` with `referentialIntegrity`, a view
  without the `views` preview feature, an enum breaking the `@@schema`
  rules, `schemas` without `multiSchema` before 6.13.0) withdraw the
  schema. An unlisted postgresql native type is no longer a PR-P1 form:
  Prisma 6.19.3 and 7.10.0 may read it as an extension type, so the type is
  unknown instead.
- **`server_default_text` is the database's text** (developer decision
  2026-11-09), never the Prisma literal: on postgresql the text Prisma's
  renderer writes for a string, integer, boolean or enum value (an enum
  value as its mapped name), `CURRENT_TIMESTAMP` for `now()`, and a
  `dbgenerated` text as written; a serial column's sequence default,
  MySQL's AUTO_INCREMENT and SQL Server's IDENTITY claim presence only.
  Other literals and the mapped providers' literals claim presence only,
  with a reason on `server_default_text`.
- **Catalogue**: `prisma.Catalogue()` registers PR-V1 to PR-V6 and the 26
  rows (claim cell as written in the docs, the row's assumptions) with
  `models.Register`, so `/api/v0/schema/assumptions` lists them under
  `models`.
- **Wiring**: one `prisma.Link` call in `internal/index/index.go` after the
  SQLModel linker. No extractor version bump: model records are
  link-time and uncached (G1B0's rule), and a warm index relinks.
- **Schema view**: a `default: "client"` column has the `default_state`
  `client` and reads "set by Prisma (client)" with the function, not
  unknown and not a server default (server `modelDefaultState`, web
  `defaultCell`).

### Prisma 5 and 6 (developer decision 2026-11-09)

Discovery accepts proven majors 5, 6 and 7. Each gate below is taken from
Prisma's own source at the version where the behaviour changes (prisma
5.22.0 and 6.19.3, their history between 6.0.0 and 6.19.3, and the
prisma-engines commits `605197351a3c` (5.22.0) and `c2990dca591c`
(6.19.3)):

| behaviour | gate |
|---|---|
| config file | none before 6.4.0; `prisma.config.ts` 6.4.0 to 6.12.x (early-access rules not modelled: not proven); from 6.13.0 the c12 names of 7.9.1, literal `schema` first |
| `package.json` `"prisma": {"schema"}` | read by 5 and 6 (after a config `schema`); a folder needs `prismaSchemaFolder` before 6.7.0 (not proven) |
| default lookup | `schema.prisma`, `prisma/schema.prisma`; before 6.6.0 a `prisma/schema` folder may be the schema or conflict (not proven) |
| `@@schema` | `multiSchema` preview feature before 6.13.0 (GA in 6.13.0); without it `schemas` is PR-P1 and `@@schema` unknown |
| join table key | primary key on postgresql from 6.0.0; Prisma 5 a unique index (unknown) |
| client defaults | `cuid(2)`, `uuid(n)` from 6.0.0, `ulid()` from 6.2.0; earlier: default unknown |
| mysql, sqlserver, cockroachdb mapping | cited for 7 only; provider-dependent facts unknown on 5 and 6 |

Without an exact locked version, a Prisma 6 minor is not proven: a gate
that depends on it takes the outcome every 6.x release agrees on, or
leaves the fact (or the schema) unknown. Postgres types, defaults, keys,
indexes and foreign keys render the same at 5.22.0, 6.19.3 and 7.10.0
(renderer and `sql_schema_calculator.rs` compared), so they are claimed
with `verification: "oracle"`; step G1B4 runs the oracle with the latest
5.x and 6.x CLIs on the fixture.

### Known gaps

- Minor versions of 5.x and 6.x other than the gates above are assumed
  to behave as 5.22.0 and 6.19.3 do; only those two are compared with 7.
- `@updatedAt` without `@default` keeps `default` unknown (row PR-A4 has
  no claim side), so the Schema view shows it as unknown rather than as a
  client-set value.
- A relation shape Prisma would reject as ambiguous (three fields with
  one relation name) is not detected; its join table is simply not
  claimed.

## Oracle, SQLite and Prisma 5 and 6 facts

### Context

The contract's facts were checked against Prisma's SQL by hand in the investigation;
step G1B4 commits the oracle, adds SQLite (developer decision 2026-11-09:
sqlite facts become `oracle` where a SQLite oracle agrees), runs Prisma
5.22.0 and 6.19.3 too, and settles G1B3's question on MySQL's
AUTO_INCREMENT and SQL Server's IDENTITY (decision
2026-11-09: they are not DEFAULTs).

### Options considered

1. Comparing the claims with Prisma's SQL parsed by Graphvein's migration
   pass: rejected in the investigation (it would test that pass's coverage).
2. The real catalog of a database built from Prisma's SQL (chosen):
   postgresql on the scratch Postgres 17 cluster, sqlite on a scratch
   SQLite file read through `sqlite_master` and the pragmas (Python's
   `sqlite3`; no SQLite server or CLI is needed).
3. For runs at another version than a package's pin: a fixture package
   per version (more golden churn) or the same schema linked with the
   lockfile's pin rewritten to the run's version (chosen: the test links
   what Graphvein would claim for a project locked at that version).

### Decision

- **Harness**: `scripts/oracle/prisma_catalog.sh` and
  `prisma_catalog.py` (never run by CI). Prisma validates a schema as a
  whole, so forms Prisma or Postgres rejects (all PR-P1 or D, none
  claimed) are removed per run and listed in the dump's `dropped`.
  Extensions are the environment's: the harness creates `citext`,
  `pg_trgm` and `btree_gist` before applying the SQL.
- **`TestPrismaOracle`** compares every claimed table, column set, type
  (sizes, enum name, schema and values), nullability, key and key name,
  unique and index facts and lists, default, server default and its text,
  and foreign key (columns, target, actions, name).
- **SQLite mapping** (rows PR-F1, PR-F4, PR-A3, PR-R3; the same SQL at
  5.22.0, 6.19.3 and 7.10.0): declared types `TEXT INTEGER BIGINT REAL
  DECIMAL DATETIME BOOLEAN JSONB BLOB`; an enum column is `TEXT`; `Json`
  and enums from 6.2.0 (6.1.0 and 5.22.0 reject both, 6.2.0 prints the
  same SQL as 7.10.0; type unknown before); the default text as for
  postgresql (any canonical integer); default actions as on postgresql;
  the join table's key a unique index on `(A, B)`.
- **No DEFAULT, no default claim** (class rule): `autoincrement()` on
  mysql, sqlserver and sqlite, and every single-field `Int` `@id` on
  sqlite (Prisma always renders `INTEGER PRIMARY KEY AUTOINCREMENT`, with
  or without `@default(autoincrement())`; a `BigInt` id with
  `autoincrement()` renders `BIGINT PRIMARY KEY` and gets no generated
  value at all): `default` and `server_default` unknown with a reason.
  The record has no autoincrement or identity attribute, so nothing else
  is claimed instead.
- **Precision findings fixed by class rule**: Postgres keeps a negative or
  non-int4 integer default as a cast string (`'-1'::integer`,
  `'3000000000'::bigint`), so integer text is claimed only from 0 to
  2147483647; SQLite keeps a parenthesised `dbgenerated` expression
  without its outer parentheses, so that text is presence only on sqlite;
  a `Json` literal default on sqlite renders `DEFAULT {}`, which SQLite
  rejects, so it is unknown.
- **Prisma 5 and 6** (developer decision): the `pg` fixture runs with
  5.22.0 and 6.19.3 as well as 7.10.0, the `sqlite` fixture likewise,
  `v5` with 5.22.0 and `v6` with 6.19.3. Turned on where the oracle
  agrees: the Prisma 5 postgresql join table (a unique index on `(A, B)`,
  an index on `B`, the cascading foreign keys; G1B3 left the key and the
  foreign keys unknown) and every sqlite fact at 5, 6 and 7. Kept: the
  mysql, sqlserver and cockroachdb mapping on 5 and 6 (no oracle);
  `uuid(n)` before 6.0.0 (5.22.0 accepts `uuid(7)`, but earlier 5.x are
  not checked); `cuid(2)` and `ulid()` gates (5.22.0 rejects both, as the
  gates say).

### Consequences

- The fixture, umami and hoppscotch agree with the catalog on every
  claimed fact (the T018g1 task record (internal) "G1B4" has the numbers).
- The fixture's `Composite` indexes were made valid for Postgres (a Hash
  index on one column, Brin without `sort:`, the SP-GiST index on a text
  column, `map:` names for the second index of a column), so each listed
  access method is oracle-checked; Gist on text uses `btree_gist`.

### Known gaps

- Forms Prisma or the database rejects but the claim pass reads (a
  primary-key `map:` on sqlite or mysql, a multi-column Hash index, an
  access method the column type has no operator class for, two indexes
  with the same generated name) are assumed absent under PR-V2; they are
  not PR-P1 rows.
- The harness runs 5.22.0, 6.19.3, 7.9.1 and 7.10.0; 6.1.0, 6.2.0 and
  7.0.0 were run by hand on a scratch sqlite schema (the 6.2.0 gate).
  Other minor versions are not run.

## Store-side claims projection for /schema/sets

### Context

T018g0 (ADR 0058) stores a `claims` list on every model column. `/sets`
does not serve them (Revision 1) but read every column with `Store.Nodes`,
which decodes each stored payload whole: on langflow (412 model columns,
1.1 MB of column payloads, 0.38 MB without claims) that decode took
langflow's `/sets` from about 50 ms on main (118dd29) to about 68 ms
(+30 %), over the 20 % latency gate. `/sets` needs the claims only to count
them by verification (`verification_counts`).

### Options considered

1. Keep decoding (accepted for T018g0 by option (a); over the gate).
2. A store read without model claims, and the counts computed in SQL in the
   same query (SQLite `json_each`, Postgres `json_array_elements`).
   Measured on langflow: the SQLite JSON functions of the pure-Go driver
   cost as much as the Go decode they replace (`json_remove` about 4 ms,
   the per-claim count about 10-13 ms more); `/sets` was 70 ms, no gain.
3. A store read without model claims, and the counts stored on the table
   record when it is written (chosen).
4. Store claims in their own column or table (schema migration on both
   backends; more than this needs).

### Decision

- **Store interface**: `Store.NodesWithoutModelClaims(ctx, repository,
  Query) ([]contract.Node, error)` reads the nodes a Query selects in
  `Nodes` order, a model record (attribute `source: "model"`) without its
  `claims` attribute, removed before Go decodes the payload. SQLite:
  `json_remove(payload, '$.attributes.claims')` when
  `json_extract(payload, '$.attributes.source') = 'model'`. Postgres: the
  payload rebuilt with the `json` type's `json_each` and `json_object_agg`
  without the `claims` key, which keeps every other value's text as
  stored (`jsonb`'s `attributes - 'claims'` would rewrite number text such
  as `1e+21` or `-0`). Postgres's JSON functions refuse a document with a
  `\u0000` escape, so a payload holding one is returned whole and the
  claims are removed in Go (same result, rare path). Tested on both
  backends in the shared store tests (`TestNodesWithoutModelClaims`):
  equal to `Nodes` minus the claims for awkward number text, NUL,
  non-ASCII and escaped strings, non-list claims, filters and order.
- **Record** (link-time, no version bump): the model core writes
  `verification_counts` on each model table, the claims of the columns it
  contains counted by verification (`{"oracle": n}`; `{}` without
  claims). `modeltest.CheckClaims`, which every front end's tests run,
  checks that the stored counts equal the columns' claims.
- **Version rule applied**: the aggregate extractor version is bumped when
  a cached per-file payload (fact cache) changes. Model records are built
  at link time from cached Python facts and are not cached (ADR 0054), so
  every `graphvein index` run, warm or fresh, rewrites them: no bump
  (extractors-31 stays). A store last written before this change has model
  tables without `verification_counts`; `/sets` serves null counts for
  them (never a guessed count) until the next index run writes them.
- **API**: `/sets` reads columns with the new method and serves the
  table's stored counts as `verification_counts`; `/sets` and `/table`
  leave `verification_counts` out of the served table node (`/table`'s
  neighbours too), so both answer byte for byte as before. Generic node
  views (`/source`) show the record verbatim, the new attribute included.

### Consequences

- langflow `/sets` (fresh SQLite index, median of 20 interleaved, three
  runs): main 48.2 / 51.5 / 56.3 ms, T018g0 head 66.5 / 69.4 / 69.2 ms,
  this change 57.9 / 55.6 / 59.6 ms (+20 % / +8 % / +6 % on main). `/table`
  and `/summary` unchanged within noise. Responses equal to T018g0's byte
  for byte (apart from the index's `finished_at`).
- Golden indexer outputs gain `verification_counts` on model tables; the
  server goldens do not change.
- Front ends written later (Prisma first) get the counts from the core
  without code of their own.

### Known gaps

- The remaining `/sets` overhead over main is the SQL `json_remove` (about
  4-6 ms on langflow with the pure-Go SQLite driver). Removing it needs
  claims stored apart from the node payload (option 4).
- A table and its counts can disagree only if a column were written to a
  table by something other than the model core; none does today, and
  `CheckClaims` would fail a front end that did.

## Revision after the independent review (2026-11-09)

### Context

The independent review of T018g1 found a provider-dependent fact labelled
as if it were provider-independent (A1: SQL Server creates UNIQUE
constraints, not unique indexes), a comparator gap in the oracle (B1),
partial unique indexes claimed as unconditional (B2), duplicate
identities when two packages discover one schema (B3), Prisma 7 schemas
that the CLI rejects claimed anyway (C1), and gaps in the API, the view
and the assumptions (C2 to C5). Details per finding:
the T018g1 task record (internal), "Revision after the independent
review (2026-11-09)".

### Options considered

- A1: keep "unique index" everywhere and mark sqlserver's unknown; or
  claim the kind per provider from the connector source. The second keeps
  the fact and labels it `mapped`, as every other mapped fact.
- B3: pick one package (the nearest, the first); claim the schema under
  each package with distinct identities; or claim it once only when the
  packages' versions give the same facts. Picking one could state facts
  for a version the other package's CLI does not run; distinct
  identities would duplicate every table in the view.
- C5: read Prisma's `migration.sql` with the SQL pass; or list the
  directory as an unread tool. The SQL pass is sqlx's (rule:
  unread tool).

### Decision

- The unique kind is provider-dependent (`schema.uniqueKind`): index on
  postgresql and sqlite (oracle), mysql and cockroachdb (mapped),
  constraint on sqlserver (mapped), unknown where provider-dependent facts
  are unknown (neither list, constraint set partial). The mapped
  providers' join table key is claimed with Prisma 7 the same way.
- `where:` on `@unique`/`@@unique` is D.
- A schema shared by several packages is claimed once when the schema
  and the claim gates (`claimGates`, checked by `TestClaimGates`) agree,
  with pins only when the exact versions agree; else not claimed, with a
  warning.
- Prisma 7: datasource `url`, `directUrl`, `shadowDatabaseUrl` are PR-P1.
- The oracle accepts a cast to the column's own type.
- PR-V6 is in the assumptions of every row with a mapped fact, and in a
  table's when it holds one.
- Server: model groups serve `dialect`; `unread_tools` lists
  `prisma-migrate`. Web: emulated relations listed as text; Prisma
  migrations named as found, not read.

### Consequences

The contract rows PR-B1, PR-F1, PR-F2, PR-F4, PR-A2, PR-A3, PR-AA2,
PR-R1, PR-R3 and PR-P1 changed (docs and code 1:1, the catalogue golden
too). The fixture golden changed (new cases, line shifts). A Prisma 7
project still carrying `url` in its schema gets no tables, which is what
its CLI does.

### Known gaps

- The SQL Server, MySQL and CockroachDB kinds are mapped, not
  oracle-checked (no cheap database).
- The URL rule is verified with the 7.10.0 CLI; earlier 7.x releases are
  assumed to reject the same properties (the removal is a 7.0 change).
  If one did not, the schema is refused rather than claimed wrongly.
- The prisma-migrate rule is path-based (a `.prisma` file beside
  `migrations/`), not tied to discovery, plus the directory a Prisma 7
  config names in a literal `migrations.path` (step G1G: the linker's
  `schema_model_migrations` marker, which the server reads back from the
  message). A non-literal path, and a Prisma 5 or 6 config's, is not
  followed (the default rule applies).

## Review notes (delegated review, 2026-11-09)

- **Results:** oracle (`scripts/oracle/prisma_catalog.sh`, `TestPrismaOracle`) 0 disagreements on every run: fixture postgresql 7.10.0 / 6.19.3 (1,168 facts each) and 5.22.0 (1,149), sqlite 7.10.0 / 6.19.3 (406) and 5.22.0 (378), multi-file, emulated, v5 and v6 packages; corpora umami 7.10.0 (2,513 facts: 26/26 tables complete, 229 columns, 0 database foreign keys under `relationMode = "prisma"`, 30 emulated relations) and hoppscotch 7.9.1 (1,814 facts: 23/23 tables, 171 columns, 22 foreign keys). Gates: 10 other corpora unchanged apart from `verification_counts` on SQLModel tables (G1B0); self-index +667 fixture records; Langflow store +4 KiB, timing fresh -0.3 % / warm -0.9 %; langflow `/sets` 58-59 ms vs main 67 ms (G1B0 projection); umami `/sets` 21 ms, hoppscotch 19 ms. CI run 42719492523 (Windows and postgres green, 0 FAIL, only expected and env-gated skips).
- **Steps:** G1B0 store-side projection (`NodesWithoutModelClaims`, stored `verification_counts`); G1B1 lexer/parser (Prisma's `datamodel.pest`), discovery and the kit extension (composite foreign keys, `on_update`, names, ordered primary key); G1B2 table and column claims; G1B3 relations, implicit many-to-many, failures, Prisma 5/6 version gates, wiring; G1B4 oracle (Postgres and SQLite, Prisma 5/6/7) with four precision fixes; G1F review fixes; G1G wording and `migrations.path`.
- **Independent review:** one class A (sqlserver `@unique` is a UNIQUE constraint; was claimed as an `oracle` unique index) and B1-B3, C1-C5, all fixed as class rules (unique kind per provider, `format_type` casts in the comparator, partial unique -> D, a schema shared by two packages claimed once or not at all, Prisma 7 datasource URLs -> PR-P1, emulated relations in the view, group `dialect`, PR-V6 in row assumptions, `prisma-migrate` unread tool).
- **Developer decisions (2026-11-09):** PR-V5 `public` under a stated assumption; unique indexes recorded as indexes with `unique: true` (sqlserver: constraints, per the review); implicit many-to-many join tables claimed; postgresql and sqlite `oracle`, mysql/sqlserver/cockroachdb `mapped` from the connector source (PR-V6, "unverified" in the view); Prisma 5 and 6 read with version gates; `/sets` latency accepted for T018g0 with this task's projection as the fix.
- **Known gaps / follow-ups (labelled, no confident wrong fact):** forms Prisma rejects at deploy time but parses are claimed under PR-V2 (schema deployed); Prisma tables show the SQLAlchemy line "DDL hooks: unknown"; a Prisma 6 config's `migrations.path` is not followed; `uuid(n)` on 5.x gated conservatively; mapped join tables leave column `index` facts and foreign keys unclaimed.

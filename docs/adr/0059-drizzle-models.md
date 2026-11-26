# ADR 0059 - Drizzle models and the TypeScript model payload

**Status:** Accepted (delegated review, 2026-11-10)

Drizzle model schemas (`docs/contract/claims/drizzle-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"). Written with the change that decides the cached TypeScript
model payload (gate, shape, version, validity). Discovery, node shapes, the
per-dialect unique form, row and assumption ids and the oracle
are fixed by the task file's claim contract and the investigation results; units
G2B2-G2B5 implement them and add their implementation notes to this ADR.

## Context

Milestone M2 of the project plan asks for ORM models in each first-tier language. Drizzle states
tables in TypeScript: `pgTable("t", { ... }, (t) => [...])` calls in schema
modules that a drizzle-kit config names. The investigation pinned kaneo
(drizzle-orm 0.45.4, drizzle-kit 0.31.11) and dokploy (0.45.2, 0.31.9),
inventoried every form, checked the semantics in the pinned packages and ran
an oracle (drizzle-kit's own SQL applied to a scratch Postgres 17); the
claim contract is finalised from it.

Unlike Prisma (ADR 0057), Drizzle cannot be read at link time from retained
sources: the schema language is TypeScript, which needs the tree-sitter
parse that runs only in the bounded parse workers (ADR 0002, 0006, 0012),
and on a cache hit the indexer never parses a file
(`internal/index/index.go`). The existing TypeScript caches do not hold the
syntax either: module bindings are `RouteExpr` values that keep object
properties in a map (column order lost), drop array elements and spreads,
keep arrow functions as text and read a tagged template's argument as
children; `urlExpr` keeps order and arrays but runs only at URL and config
sites.

## Options considered

1. **Widen `RouteExpr` or `urlExpr`.** Every TypeScript file's cache and
   every route and client consumer would change; rejected for the same
   reason as ADR 0053 option 1.
2. **Re-parse schema files at link time.** A second parse path outside the
   bounded pipeline, and a parse of every schema file on every warm run
   (ADR 0030 records a forced reparse on every run as a defect).
3. **A gated, cached, tool-neutral payload (chosen).** A new additive
   field captured during the existing parse only in files that can define
   Drizzle tables or configure drizzle-kit, holding only module-level
   declarations and the default export as an ordered reduced tree.

## Decision

- `typescript.CachedFacts.Models` (JSON `models`, omitted when the gate
  does not hold), type `typescript.ModelSyntax`, version 1
  (`typescript.ModelSyntaxVersion`). Existing TypeScript facts and
  payloads are unchanged; imports and `ExportClauses` stay where they are
  and resolution stays at link time (rule 31).
- Gate (`typescript.ModelGate`), all syntactic, from the extracted import
  list (comments never count): G1 an import of `drizzle-orm/<dialect>-core`
  or a subpath (the table, enum, column and `customType` builders); G2 an
  import of `drizzle-kit` (configs); G3 drizzle-kit's default config name
  `drizzle.config.ts`/`.js` (a config need not import `drizzle-kit`). The
  root `drizzle-orm` module is not a gate: kaneo has 508 and dokploy 163
  query files importing it.
- Captured: every module-level `const`/`let`/`var` declarator with an
  identifier name whose initializer, after TypeScript wrappers, is a call,
  a tagged template or an object literal (name, keyword, `exported` for the
  `export const` form, span, value), and the `export default` expression.
  This is the cheapest syntactic test that keeps every table, enum,
  `customType`, `relations()`, `sql` const and config value of both
  corpora.
- Reduced expression tree, order kept everywhere: call (callee, arguments),
  member (object, identifier property), identifier, string, number (with a
  leading sign), boolean, null, template and tagged template (cooked
  strings, substitutions), object (ordered entries: property, shorthand,
  spread, method with its single `return <literal>`, unknown), array
  (ordered elements, spreads), arrow function (parameter names, expression
  body or a block with one `return`), and `unknown` with its span for
  everything else (optional chains, operators, `new`, functions, async
  arrows, computed keys, depth over 48). `as`, `satisfies`, `!`, `<T>x` and
  parentheses are transparent; type arguments and comments are never kept.
- Payload table: `docs/lang/typescript-models.md` ("Payload table"); every
  capture branch reports its row id and `TestModelPayloadContract` keeps
  code and table 1:1 (snippet per row, fixture `drizzle-syntax` reaches
  every row). `TestDrizzleCensus` checks the capture against the investigation's
  inventory when `GRAPHVEIN_DRIZZLE_CENSUS_CORPUS` names the corpus
  directory.
- Validity: `ModelSyntax.Valid()` (version, declaration list, names,
  keywords, every expression's kind and required fields) is checked with
  the other TypeScript payloads on every cache read; anything else is a
  cache miss. A capture failure (a recovered panic) is diagnostic
  `model_syntax_capture_failed`; the file keeps its other facts, is not
  cached, and no other file is affected.
- Versioning: the aggregate extractor version moves to `extractors-32`
  (living documents only).

## Consequences

- Every TypeScript file is re-extracted once (aggregate version bump).
  Files outside the gate cache the same facts as before; snapshots change
  only in their header.
- The census finds G1 in kaneo 8 and dokploy 54 files and G2 in one config
  each (the investigation's dokploy 55 was a text count that included a fully
  commented-out file).
- The payload adds no graph record and no contract change. Later units
  decide every claim at link time through the TypeScript resolver.
- T018g3 (Sequelize) extends the gate instead of adding a second payload.

## Known gaps

- CommonJS `require("drizzle-orm/pg-core")` and `module.exports` configs
  are not captured; linking reports such files as not read.
- A config named by `--config` with another name and no `drizzle-kit`
  import has no payload (the config is unproven, never guessed).
- Values outside the captured declarations (an `sql` const in a file
  outside the gate, `const n = 5`) are not in the payload; a claim that
  needs them is unknown.
- The oracle: see the task file until step G2B5 records its notes here.
- (G2B3, kept by the developer's decision of 2026-11-10, DR-T1 reworded)
  Under push, drizzle-kit keeps only the tables of the config's
  `schemaFilter` (default `["public"]`), under generate every table; a
  postgresql table or enum in a schema outside that filter is not
  claimed, and a non-literal filter claims no table. A project that only
  runs `generate` + `migrate` loses those tables (no wrong fact). A builder object
  shared through a spread keeps the first table's generated unique name,
  so that name is unknown. Builders outside the investigation run list, `.array(n)`,
  `interval` with fields, and defaults whose rendered text JavaScript
  could print differently (numbers with exponents, JSON with keys
  JavaScript reorders) are unknown or presence-only.
- Discovery matches only the repository's scanned files: a schema file
  ignored by `.gitignore` or `.graphveinignore` is not seen, although
  drizzle-kit's glob would load it.
- A config that `export default`s an identifier, or is reached only by a
  `cd` or a workspace runner option in a script, is unproven, never
  guessed.

## Implementation notes

### Discovery and contract table

- Package `internal/extract/models/drizzle`, `Discover(Input)`: the input
  is the TypeScript facts of every file, the cached model payloads, the
  shared `typescript.ModuleResolver` and the retained sources; nothing is
  parsed at link time. Rules and markers:
  `docs/extract/schema-drizzle.md` "Discovery".
- Group roots: a config file's repository path
  (`models/drizzle/apps/api/drizzle.config.ts`); a script's literal
  `--schema`/`--dialect` flags without a config (fixture only) use
  `<package.json>#<first script>`, one group per distinct flag set.
- Configs: the package directory's first default name (drizzle-kit's
  order `.ts`, `.js`, `.json`) and every literal `--config` of a
  `drizzle-kit` command in the package's scripts; commands that change the
  working directory, pass non-literal values or combine `--config` with
  other options (drizzle-kit exits, `bin.cjs:10729`) are markers. The
  config object is read from the payload's default export
  (`defineConfig` imported from `drizzle-kit`, or an object literal) or as
  JSON; a spread or computed key makes it unproven.
- Schema paths: glob 8's `dot: false` subset (`*`, `?`, `**`, literal
  paths) against the package directory; matched directories contribute
  their direct files.
- Module reach: `export *` carries the names visible in the re-exporting
  module, `export { x } from` and `export { x }` of an imported binding
  carry `x`; namespace and type-only re-exports reach nothing
  (`prepareFromExports` reads `Object.values(exports)`, which never looks
  inside a namespace object).
- Pins: the Prisma front end's lockfile reader, copied, with one change:
  every YAML document of a `pnpm-lock.yaml` is read, because recent pnpm
  writes an environment document first (kaneo); documents that lock
  different versions give none. `pins` needs both drizzle-orm and
  drizzle-kit; drizzle-orm 1.x (locked, or the range without a lock) is
  refused.
- Diagnostics: `schema_model_not_proven` for a package without a config
  that has table builder files, a refused package, an unproven config or
  script command; `schema_model_unread` for unused `drizzle.config.*`
  files, the D dialects, matched files with other extensions, unresolved
  re-exports and, per proven config, the table builder files of its
  packages it does not reach (DR-P2).
- `contract.go` holds the 14 rows with their side (ten claim rows, DR-P1
  refusal, DR-P2 and DR-V none, D default), assumptions and the DR-K1
  forms; `TestDrizzleContract` keeps it 1:1 with the docs and the task
  file. The catalogue registration for `/api/v0/schema/assumptions` waits
  for the claim branches it names.

### Tables and columns

- `Link` (`link.go`) runs discovery, then one claim pass per proven
  config (`tables.go`, `columns.go`, `modifiers.go`, `render.go`,
  `lookup.go`) and hands the tables to the kit (`models.Link`; rows
  `Emit: DR-T1`, `ForeignKeys: DR-C2`). It is not called by the indexer
  yet.
- Identities: `table:<file>#<const>`, `column:<file>#<const>.<key>`; the
  column's `name` is the database name (literal, or the key converted by
  the config's `casing`), its identity name the property key (ADR 0057).
- Group: `dialect` as the config writes it (`turso` read by the SQLite
  rules), `verification` `oracle` on postgresql and sqlite, `mapped` on
  mysql; table `assumptions` DR-V1, V2, V4, V5, plus V3 when a column name
  comes from `casing` and V6 on mysql.
- Visibility: the tables, enums and `pgSchema` values among the exported
  values of the group's modules (`Config.Modules`), deduplicated by
  declaration. Identifiers resolve through imports, `export { }` clauses
  and `export *` barrels to captured declarations (`lookup.go`), so a
  spread object, a `customType`, an enum or an `sql` const may sit in a
  file outside the schema.
- Column records: `type` as the catalog shows it (postgresql
  `format_type`, checked on a scratch Postgres 17.11 against drizzle-kit
  0.31.11 `export` of the fixture), `nullable`, `primary_key`, `unique`,
  `index` (false only without extra config), `default`
  (`absent`/`server`/null), `server_default`, `server_default_text`
  (drizzle-kit's rendered text), `foreign_keys` (`[]` only without
  `.references()` and extra config), `enum` (`{name, schema, values}`),
  `identity`, `generated` and `generated_expression`. Unique constraints
  of `.unique()` go to `unique_constraints` (postgresql, with
  `nulls_not_distinct`; mysql) or to `indexes` as unique indexes (sqlite,
  A10).
- A table with a third argument keeps it for step G2B4: its constraint
  set is partial, its primary key open, and the column absences an extra
  config could change (nullability outside sqlite, keys, uniqueness,
  index, foreign keys) are unknown with a DR-X1 reason.
- DR-P1 here: a non-exported enum or `pgSchema` used by a table, two
  tables with one schema-qualified name, two columns with one name, two
  `.unique()` constraints with one name in a table, and on sqlite one
  unique index name in two tables. The rest of DR-P1 comes with G2B4.
- Two configs reaching the same declaration: it is claimed in neither
  (`schema_model_not_claimed`), as identities are per file.
- Lockfile reader: one shared package `internal/extract/models/lockfile`
  replaces the Prisma and Drizzle copies; it reads every YAML document of
  a `pnpm-lock.yaml` (Prisma's output on its fixture is unchanged; a
  kftray/kaneo-style lockfile with an environment document first now
  pins).

### Keys, extra config, relations and wiring

- `keys.go`: `.references()` (DR-C2) and `foreignKey()` (DR-X1) are
  collected per table and resolved by the group once every table is read
  (the referenced table may be in another file); each local column lists
  the whole constraint in the kit's composite shape (`local_columns`,
  `columns`, `on_update`, `name`). Generated names follow drizzle-orm's
  `getName()` with drizzle-kit's casing pass (a first-occurrence string
  replacement of each original column name, applied also to literal
  names) and Postgres's 63-byte truncation; sqlite foreign keys have no
  name, mysql names over 64 characters are unknown.
- `extra.go`: the extra config's elements (DR-X1) become the table's
  `indexes`, `unique_constraints`, `check_constraints`, `primary_key`
  order and `primary_key_name`; an element no form covers is D for that
  element (constraint set partial, primary key open, the column absences
  unknown). Index entries gain per-dialect keys (`method`, `orders`,
  `nulls`, `opclasses`, `expressions`, `partial`, `where`,
  `storage_parameters`); the kit writes them as given.
- `relations.go`: DR-R1 entries are the table attribute `orm_relations`
  (developer decision 2026-11-09: ORM-level, never an edge, no claim);
  the Schema view lists them like Prisma's emulated relations
  (`OrmRelations`, sharing the list component), and names a Drizzle
  table's declaration "the table definition".
- Identifier resolution follows module-level `const a = b` aliases
  through the TypeScript facts' module bindings (the payload does not
  capture an identifier initializer); kaneo's relations name its tables
  through such aliases.
- `catalogue.go` registers the Drizzle catalogue (`models.Register`) for
  `/api/v0/schema/assumptions`; `internal/index/index.go` calls `Link`
  once after the Prisma front end, with the cached payloads and the
  module resolver (no parse at link time; a warm replay emits the same
  stream, `TestDrizzleIndexedFixture`).
- Known gaps added: index column modifiers (`.desc()` ...) written on a
  column outside an index element would carry into the next index in
  drizzle-orm and are not modelled (no corpus form); duplicate index or
  check names other than the listed DR-P1 cases, a foreign key to columns
  without a unique constraint and a column `.primaryKey()` together with a
  `primaryKey()` element are assumed absent or left unknown, never
  claimed; the migration-set overlap comparison is not passed migration
  records for Drizzle groups (no overlap diagnostic yet).
  (Closed by G2B5: the overlap comparison now receives them.)

### Oracle, migrations folder and overlap

- The oracle is committed as `scripts/oracle/drizzle_catalog.sh` with a
  Node helper (`drizzle_catalog.mjs`), never run by CI, and
  `TestDrizzleOracle` (env-gated): drizzle-kit's own SQL (`export`, or
  `generate` under a casing, A4) applied to a scratch Postgres 17 or a
  scratch SQLite file, the catalog dumped, and every claimed expression
  text put through the same database so both sides compare in Postgres's
  rendering. The harness writes its own config (the project's never runs),
  copies only the modules the schema reaches, stubs imports called only
  inside functions and refuses a module with a top-level call. Forms that
  stop drizzle-kit or Postgres for the whole schema are dropped from the
  copy; none is claimed. 0 disagreements on the fixture (pg, pgschema,
  both casings, sqlite; mysql against the printed SQL) and on kaneo (5,349
  facts) and dokploy (9,499 facts); `docs/extract/schema-drizzle.md`
  "Oracle" has the numbers.
- Oracle rule (T018g0): `TestDrizzleOracleForms` keys every
  oracle-labelled fixture fact by dialect, fact, row and value class and
  requires exactly the committed list the oracle compared; the corpora
  use no form outside it.
- Class rules from the oracle: enum column types are quoted as
  `format_type` quotes identifiers (dokploy's mixed-case enums; the
  keyword list is Postgres 17.11's `pg_get_keywords()`), and a postgresql
  `customType` literal is claimed only when the oracle compared it
  (`bytea`, `text`; DR-C3's refusal cell says so).
- A config's drizzle-kit `out` folder (literal, default `drizzle`, against
  the package directory) is a `schema_model_migrations` marker, the code
  Prisma uses, with its own message part; the server lists the folder as
  the unread tool `drizzle-kit` when it holds `meta/_journal.json` or a
  `.sql` file (decision; server and web change outside the
  task file's allowed list, by that decision). The migration pass's
  records reach `drizzle.Input`, so a Drizzle table named like a
  migration-set table gets `schema_model_migration_overlap`.
- Known gaps added: a foreign key whose target column has no unique
  constraint (the fixture's `authors.rlsId`, unknown because its target is
  not claimed) makes Postgres reject the migration; a claimed table
  referencing a claimed column that is neither unique nor a key is not
  refused (no corpus form). A drizzle-orm 1.x package (refused) gets no
  migrations-folder marker.

### Revision after the independent review (2026-11-09)

Class rules, each with a fixture case or test
(`docs/extract/schema-drizzle.md` "Revision after the independent
review" has the detail; the contract rows DR-K1, DR-T1, DR-C1, DR-C2,
DR-C3, DR-P1 and D are reworded in both tables and the catalogue):

- **The payload gate stays as decided; a module outside it is never
  silent (B1).** A schema module the config matches or reaches without a
  model payload (builders imported through a local re-export: gate G1
  sees no `drizzle-orm/<dialect>-core` import) whose visible exports
  include a value declaration gets one `schema_model_unread` marker
  naming the file and its exported values, and the group's `table_list`
  is `may_be_incomplete`. Following a local re-export at parse time was
  considered and not done: see the next point.
- **Widening the payload gate needs an aggregate extractor version bump
  (C4).** A file's cached TypeScript facts are valid with `Models == nil`
  (the field is omitted outside the gate, and a cache read accepts its
  absence), so a gate that captures more files does not invalidate the
  caches of files it newly covers: their payload would stay missing until
  they change. Any change to `ModelGate` (or to what a captured file
  holds) must therefore move the aggregate extractor version
  (`extractors-<n>`), as G2B1 did, so every TypeScript file is
  re-extracted once; `ModelSyntaxVersion` alone is not enough because a
  file without a payload carries no version.
- **Group list marker (server).** Claim-pass diagnostics about a
  declaration that may be a table of a group but is not claimed there
  end with `; the table list of model group <set> may be incomplete`;
  the server counts such a diagnostic in that group's `table_list`
  wherever it is filed, and now reads the `schema_` diagnostics of
  non-Python files (it read only `.py` diagnostics, so a Drizzle group
  was `complete` with unread tables). Message-based, like the migrations
  marker: diagnostics have no group field, and adding one would change
  the indexer contract.
- **Two configs, one module (B2, option b).** Claimed once,
  in the first config's group by root, when the configs agree on
  dialect, casing and schemaFilter (the facts are the same); the other
  groups get a not-claimed diagnostic naming that group. "Claimed in each
  group" is not representable while identities are per file (one node,
  one `set`); recorded as a decision for the developer. Configs that
  differ: claimed in neither. One config file run by two packages is one
  group when both directories resolve the same files, else unclaimed
  (DR-V5).
- **MySQL (B3, mapped).** Types in the form MySQL 8.0.19+ reports
  (`serial` -> `bigint unsigned`, `boolean` -> `tinyint(1)`, `char` ->
  `char(1)`), DR-V6 now names the server version; `.autoincrement()`,
  `timestamp().defaultNow()` and `.onUpdateNow()` read, each leaving only
  the facts it could change unknown.
- **DR-P1 addition (postgresql).** A foreign key whose referenced columns
  are neither key nor unique on a table whose keys are all read fails
  the referencing table. The oracle showed that a key to columns unique
  only through a unique index is rejected too from one generate or push
  (drizzle-kit adds foreign keys before indexes, `bin.cjs:29041-29042`);
  it is unknown.
- Harness: enum defaults in non-public schemas normalised under the
  catalog's search path; directory `schema` patterns expanded. Wording of
  the `$defaultFn`/`$onUpdate` reason; table-creator markers name their
  tables. UI: the Drizzle relations text states nothing about foreign
  keys.
- Known gaps: a marker of a declaration that creates no table of the
  config (another dialect's builder, `mysqlSchema(...).table`) still makes
  a list `may_be_incomplete` when it is on a file with a claimed table;
  MySQL types assume 8.0.19 or later (older servers report `int(11)`);
  `dev.mysql.com` was not reachable from the session, so the MySQL forms
  are cited from drizzle-kit's source and MySQL's documented aliases, not
  re-read.

## Review notes (delegated review, 2026-11-10)

- **Results:** oracle (`scripts/oracle/drizzle_catalog.sh`, `TestDrizzleOracle`, `TestDrizzleOracleForms` pinning 125 oracle-labelled forms) 0 disagreements: fixture pg 991 facts, pgschema 16, cased 92, camel 34 (`generate`), sqlite 164, mysql 62 (mapped, against the printed SQL); kaneo 5,349 facts (51 tables, 442 columns, 66 foreign keys, 83 indexes), dokploy 9,499 facts (68 tables, 960 columns, 135 foreign keys, 28 enums). Gates: 12 other corpora identical records (header extractors-31 -> 32); kaneo +1,007 and dokploy +2,127 Drizzle records; self-index +582 fixture records; Langflow store equal, timing fresh -0.3 % / warm -0.7 %; langflow `/sets` and `/summary` at main; kaneo `/sets` 46-48 ms, dokploy 63-67 ms (1.06 MB). CI run 42757700138 (Windows and postgres green, 0 FAIL); earlier runs 42753543070 and 42756226828 failed on Windows only (parse deadline started before grammar setup, fixed in `internal/parse` with `TestSetupOutsideFileTimeout`; `TestTimeoutRetiresWorker` made deterministic with a parse hook).
- **Steps:** G2A the investigation (16 outline corrections A1-A16); G2B1 TypeScript model payload (`ModelSyntax` v1, extractors-32); G2B2 discovery and contract; G2B3 tables/columns and the shared lockfile reader (also fixes Prisma pins on multi-document pnpm lockfiles); G2B4 keys, extra config, relations, wiring; G2B5 oracle, migration overlap, `drizzle-kit` unread tool; G2F review fixes (incl. the index-lock test start-up race); G2G gates; G2W/G2W2 Windows timing fixes.
- **Independent review:** no wrong stored fact on probes (Auth.js/T3, casing, globs, pgSchema, SQLite); class A in UI prose (ORM-relation text claimed no database foreign keys) fixed with per-tool text; B1 payload-less schema modules now marked and the group `may_be_incomplete` (the server reads `schema_` diagnostics of every file); B2 configs agreeing on dialect/casing/filter claim a shared table once; B3 MySQL types in catalog form and narrower unknowns; FK to a non-unique target is DR-P1 on postgresql.
- **Developer decisions (2026-11-09):** drizzle-orm 0.x only (1.0-only forms D); postgresql and sqlite `oracle`, mysql `mapped` from drizzle-kit's source; only the config's schema is read; `relations()` ORM-level only; dokploy as second corpus. Decisions under the session delegation: conservative `push` schemaFilter rule (DR-T1); customType literals on postgresql claimed only for oracle-compared literals (DR-C3); shared-table rule (B2, option a).
- **Known gaps / follow-ups (labelled, no confident wrong fact):** builders imported only through a local re-export are not captured (marked `schema_model_unread`, list `may_be_incomplete`); widening the payload gate needs an aggregate version bump; MySQL defaults presence only; CommonJS configs not captured; dev.mysql.com was blocked (MySQL forms cited from drizzle-kit source); the Drizzle fixture packages reuse prisma-models package names (`duplicate_workspace_package` on self-index); dokploy `/sets` about 1 MB (paging later if it grows).

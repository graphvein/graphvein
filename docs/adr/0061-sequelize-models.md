# ADR 0061 - Sequelize models and model payload version 2

**Status:** Accepted (delegated review, 2026-11-14)

Sequelize v6 models (`docs/contract/claims/sequelize-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"). Written with the change that widens the cached TypeScript model
payload of ADR 0059 to version 2 (gate, statement and expression kinds,
validity). Discovery, node shapes, row and assumption ids and the oracle
are fixed by the task file's claim contract, the investigation results and Revision 1;
steps SQB2-SQB5 implement them and add their implementation notes here.

## Context

Sequelize states tables in TypeScript or JavaScript, ESM or CommonJS:
`class X extends Model` with one module-level `X.init({attributes},
{sequelize, ...options})` (opencollective-api, 52 models), or
`sequelize.define("name", {attributes}, {options})` inside a CommonJS
factory `module.exports = (sequelize, DataTypes) => {...}` that an index
module `require`s and calls with the instance (Gladys, 32 models).
Associations are module-level calls (`A.belongsTo(B, {...})`, 197 in
opencollective-api's `models/index.ts`) or calls inside
`x.associate = (models) => {...}` run by a loop in the index module (53 in
Gladys). Gladys's instance options come from
`require("../utils/getConfig")()`, which returns
`require("../config/config")[NODE_ENV]` (open point O2, accepted in
Revision 1): the global `define` (`underscored`, `freezeTableName`, the
timestamp names) decides every table and column name.

As for Drizzle (ADR 0059), the link pass has no tree on a cache hit
(`internal/index/index.go`), and a second parse path outside the bounded
workers is ruled out. The version 1 model payload keeps only module-level
`const/let/var` declarations whose initializer is a call, tagged template
or object literal, and the default export, and opens only for Drizzle
files (G1-G3). Sequelize needs, beyond that (task file, "Payload decision",
cache table): module-level expression statements (`X.init(...)`,
`A.belongsTo(...)`, `module.exports = e`, `x.associate = e`), class
headers (`extends`), `new Sequelize(...)`, destructured requires
(`const { DataTypes } = require("sequelize")`), function bodies as
statement lists (factories, `associate`), and files that import nothing
from Sequelize (factories; Gladys's options and environment modules).

## Options considered

1. **A separate Sequelize payload.** A second gated cache field with its
   own tree types. Rejected: the reduced tree, its validity rules and its
   wrappers would be duplicated; Drizzle and Sequelize read the same
   JavaScript syntax.
2. **Widen the model payload, tool-neutral, additive (chosen).** Version 2
   adds gate rows, a module-level statement list and new expression kinds;
   version 1 fields keep their meaning, so the Drizzle linker reads the same
   values (its kind switches send the new kinds to the branches that
   version 1's `unknown` reached).
3. **Capture every JavaScript file's CommonJS exports.** Would cover any
   options module reached by `require`, but Gladys alone has hundreds of
   CommonJS modules whose exports are large function bodies. Rejected for
   the two narrow syntactic rows G7 and G8 below.
4. **Gate by the files a gated file requires.** A file's payload would
   depend on other files, which breaks caching by content hash (AGENTS.md:
   caches attach to content). Rejected.

## Decision

`typescript.CachedFacts.Models` stays the field; `ModelSyntaxVersion` is 2
and `Valid()` rejects every other version, so version 1 entries miss once.
The aggregate extractor version moves to the next free `extractors-N`
(`extractors-35` on this branch: main is at `extractors-32` and the
unmerged T018h stack is at `extractors-34`, so this branch takes the number
after it for now and the next free number at its own merge time; only the
living documents name it, never other ADRs or task records). The payload table in `docs/lang/typescript-models.md` names every
capture branch; `TestModelPayloadContract` keeps code and table 1:1 over
the `drizzle-syntax` and `sequelize-syntax` fixtures.

Gate (`ModelGate(file, root, facts)`, first matching row; G1-G3
unchanged):

- **G4** the file imports `sequelize` or a subpath (value or type import;
  `sequelize-typescript` is not `sequelize`).
- **G5** the file calls `require("sequelize")` (or a subpath) anywhere.
- **G6** the file has a module-level statement `<e>.init(<object>,
  <object>)` or a call `<e>.define(<string>, <object>, ...)` anywhere.
- **G7** the file has an object property `dialect: "<d>"`, `<d>` one of
  Sequelize 6's dialects: the options module of open point O2
  (`config/config.js`). Added in SQB1; the task's gate listed G4-G6 only.
- **G8** the file's module-level `module.exports = F` or `export default F`
  is a non-async arrow or function expression returning `<id>[<expr>]`,
  `<id>` bound by a relative `require` or import: the environment selector
  of O2 (`utils/getConfig.js`). Added in SQB1.

G5-G8 walk the tree only when a substring precheck on the file text could
succeed (`require` and `sequelize`; `define`; `dialect`; `init`; `[`).

Statements (gated files only): module-level expression statements whose
expression is a call or an `=` assignment (S3), class declarations with
name, `exported`, `default`, `abstract`, the `extends` expression and a
count of class and member decorators, never their arguments (S4),
object-pattern declarators with their bindings and initializer (S5), and
S1 also keeps `new` initializers. Function bodies of arrows (block bodies)
and function expressions are ordered statement lists: declarations,
destructuring declarations, expression statements, class declarations,
returns, and an `unknown` entry for every other statement, so a consumer
sees a conditional or loop between two statements (rows B-*). Module-level
statements other than these stay uncaptured (SD), as in version 1.

Expressions: `new` (callee, arguments), `function` (name, parameters,
statements; async and generator functions stay `unknown`, as async arrows
do), `assign` (`=` only), `index` (`o[e]`), `binary` (operator and both
operands) and `unary` (`typeof`, `!`, ...). `this`, regular expressions,
conditionals, class expressions and compound assignments stay `unknown`.
The task file listed `new`, `function`, `regex` and `this`; `index` is
needed by O2's `config[env]`, and `binary`/`unary` by the recognised
`associate` loop's filter (`typeof m.associate === "function"`), which the
linker must read before it trusts the loop.

CommonJS reach reads `require("<relative>")` initializers and
`module.exports = e` / `exports.x = e` statements from the payload of gated
files only; it is not a general CommonJS resolver (ADR 0014's deviation
stays for every other consumer).

## Consequences

- Gated files on the corpora: opencollective-api 281 of its TypeScript and
  JavaScript files (all G4; 224 outside `migrations/`), Gladys 86 (G5 52,
  48 outside `migrations/`; G6 32; G7 1; G8 1). No file of the other
  corpora passes G4-G8 unless it uses Sequelize. Payload bytes (JSON,
  uncompressed): opencollective-api about 3.1 MB, Gladys about 0.9 MB.
- Timing and store size: see the SQB1 section of
  the T018g3 task record (internal).
- The Drizzle fixtures change only where version 2 now captures a form
  version 1 left `unknown` (a `new Date()` default's reason names the kind
  `new`; the drizzle-syntax payload gains statements and the new kinds).
  No Drizzle claim changes.
- The census test `TestSequelizeCensus` checks the capture against the investigation's inventory on both corpora.

## Known gaps

- Association calls in a file that neither imports nor requires
  `sequelize` and defines no model are not in any payload. The linker sees
  their call sites in `Facts.Calls` and must treat the target tables' column
  sets as not complete (SQB2/SQB4). Both corpora hold their associations in
  gated files.
- Statements nested in module-level blocks (`if (x) { X.init(...) }`) are
  not captured; `Facts.Calls` lists the call sites, which the linker uses
  to count `init` sites (SQ-M2's "exactly one unconditional statement").
- Class members are not captured, so the sequelize-cli `static
  associate(models)` form is D; decorator arguments wait for T018g6.
- An options module whose `dialect` is not a string literal, and an
  environment selector of another shape (an inline
  `require("./config.json")[env]` in the index module is captured through
  the index module's own gate), have no payload; their facts are unknown.
- JSON configuration (`config/*.json`, node-config) is not TypeScript and
  is read by the linker from retained sources (O1), outside this payload.

## Implementation notes

### Payload version 2

- `internal/lang/typescript/models.go` (version, gate dispatch, new
  expression kinds) and `models_statements.go` (statements, G4-G8).
- Tests: `models_test.go` (gate rows, shapes, Sequelize forms, validity,
  the payload contract), `models_fixture_test.go` (both fixtures, replay),
  `models_sequelize_census_test.go` (set
  `GRAPHVEIN_SEQUELIZE_CENSUS_CORPUS`).

### Discovery and contract table

- `internal/extract/models/sequelize`: `contract.go` (the 15 rows, sides
  claim, refusal, marker for SQ-U1, none for SQ-V, default),
  `discover.go` (packages, pins, drivers, migrations folders, instances),
  `options.go` (the options argument by constructor arity, literal
  `dialect`/`define`/`schema`, open points O1 and O2, the dialect rule),
  `cjs.go` (the CommonJS reach of this ADR plus ESM exports),
  `reach.go` (models, markers) and `associations.go` (module-level calls,
  the recognised loop). Docs: `docs/extract/schema-sequelize.md`.
- The ungated call-site cache (`Facts.Routes.Clients`, every call with
  its callee chain, T018h) is the source of the "found elsewhere"
  markers: define calls in functions, project wrapper calls (O8) and
  association calls outside the reach rule. `Facts.Calls` lists only calls
  inside named functions, so it is not used.
- `inflection` is pinned only from a `package-lock.json`, resolved from
  the installed sequelize directory as Node does; pnpm and yarn lockfiles
  leave `pins` null (SQ-V1 still listed).
- A module-level arrow declaration is not in the payload (S1 keeps calls,
  tagged templates, objects and `new`), so a project wrapper such as
  `Temporal` is matched by the import that binds its module's export.
- Not yet read by the server: the `schema_model_migrations` marker with
  the Sequelize message is emitted by discovery; the unread-tool rule of
  the schema endpoints (T018f) does not list it yet.

### Models, attributes and options

- `internal/extract/models/sequelize`: `link.go` (`Link`, the kit group per
  instance: `oracle` on postgres and sqlite, `mapped` plus SQ-V6 on mysql,
  mariadb and mssql, dialect-independent facts only and `mapped` when the
  dialect is unknown), `tables.go` (model forms, naming, default
  attributes, mutations after init), `options_model.go` (the
  `Utils.merge` of the instance's `define` and the model's options),
  `attrs.go`, `types.go` and `ddl.go` (attributes, types per dialect,
  column facts as the dialects' `attributeToSQL` and `createTableQuery`
  write them, uniques and indexes), `inflection.go` (inflection 1.13.4's
  pluralize, singularize and underscore, Sequelize's camelize; ASCII only)
  and `render.go` (JavaScript literal helpers copied from the Drizzle
  front end).
- Table attributes beyond the kit's: `model_name`, `model_form`; column
  attribute `enum` (`{name, schema, values}`) on postgres, as Drizzle's.
- The SQ-P1 failures that the models, attributes and options rows read
  (Sequelize throws while defining the model, or the database rejects the
  table: no type, an ENUM without values or with a non-string label on
  postgres, a default outside the labels, `id` without `primaryKey`, bad
  timestamp options, duplicate column names, autoIncrement misuse, `fn`
  defaults on sqlite, index fields that are no column, duplicate table or
  index names in a group) are refused here; the association failures stay
  with step SQB4. Without them this step would emit tables that `sync`
  never creates.
- Until step SQB4 every foreign-key list is unknown, and the column set of
  every table a reached association may add attributes to (belongsTo's
  source, hasOne's and hasMany's target; every table when a holder does not
  resolve, a belongsToMany is present or discovery reports an association
  marker) is partial.
- Column facts are derived from the same decisions Sequelize's generators
  make, including their string tests on the column definition; a user
  text that would change those tests (finding-level guards) makes the
  column unknown. `TestDDLRendering` compares the pass with the generators'
  own output on all five dialects in a scratch Node process.

### References, associations, failures and wiring

- The claim pass runs in Sequelize's order: every model as `init` /
  `define` leaves it, then the reached association calls in execution
  order (they add or merge one attribute each with `Utils.mergeDefaults`,
  or define a join table), then each table's columns as `sync` writes
  them, the group's failures and the foreign-key lists. A column without
  `references` has the proven empty list.
- The cyclic rule follows `getModelsTopoSortedByForeignKey`; a graph that
  may be cyclic (something not read could close a cycle) is treated as
  cyclic. On postgres, the per-model `alter` pass makes foreign keys on
  primary-key columns unknown, and every foreign key of a model with a
  column whose changeColumn may fail (ARRAY of ENUM, a DEFAULT before
  UNIQUE or REFERENCES, a SERIAL that is not the primary key, a column
  that is not read). Probed in scratch on Postgres 17: a plain default
  with `unique: true`, a default with `references` and a non-key SERIAL
  each stop the pass, as the investigation's literal default did. Mapped dialects in
  such a group: every foreign key unknown.
- An unread attribute of a model can make its primary key composite, and
  Sequelize gives no references to a member of a composite key: an
  association whose key is the primary key of such a model leaves only
  its attribute's foreign-key list unknown.
- `belongsToMany` with a string `through` builds the join table from the
  source's options (Sequelize defines it with them) and the instance's
  global `timestamps`; a through model gets its key attributes and its
  implicit id unknown.
- Discovery's `schema_model_unread` definition markers end with the
  group's table-list suffix (one diagnostic per instance), as the claim
  pass's own diagnostics do.
- `internal/index/index.go` calls `Link` once after the Drizzle front end;
  the aggregate version stays `extractors-35` (this task's bump). The
  catalogue is registered for `/api/v0/schema/assumptions`, and the
  server's unread-tool rule reads the Sequelize migrations marker (tool
  `sequelize-migrations`, a minimal `internal/server` change allowed by
  a decision).

### Attribute helpers (task Revision 2)

- Row SQ-A1 gains the form "helper member" (open point O4, a contract
  change by reviewer decision): `H(args).member` or `H(args)`, `H` a
  module-level function whose body is one `return` of an object literal,
  read as the member with the parameters replaced by the call's own
  arguments (DataTypes, used only as `<p>.X`, or literals); names of the
  helper's module are unknown in the substituted value. Any other helper
  stays unknown.
- Model payload version 3: gate row G9 admits a file with such a helper
  function (opencollective-api's `server/models/DataTypes.ts` imports
  nothing from Sequelize), and row S6 captures module-level function
  declarations (name, export flags, the function as an E-function). The
  aggregate extractor version stays `extractors-35`, unmerged; a branch
  cache from before version 3 leaves a helper module without a payload,
  so its columns stay unknown (never a wrong fact) until the file
  changes.

### Oracle

- `scripts/oracle/sequelize_catalog.sh` and `sequelize_catalog.cjs`
  install sequelize 6.37.8, pg 8.23.1 and sqlite3 5.1.7 (its prebuilt
  binding through its own prebuild-install step) and typescript 5.9.3 in
  scratch. Node 24's type stripping is not enough: the models are ESM
  TypeScript with extensionless relative imports and named imports from
  modules that must be stubs, which needs a CommonJS module transform;
  typescript transpiles only (no type check).
- Each run loads its modules in one Node process: modules outside the
  run's allow list and every package but Sequelize's own are permissive
  stubs, the Sequelize constructor keeps the corpus's options except the
  connection (scratch database; a missing dialect is the run's, SQ-V4,
  recorded), opencollective-api's instance module is replaced by an
  instance of its literal dialect. `sync({force: true})` runs; when it
  fails, Sequelize's own steps are replayed per model with deferral and
  every failure recorded. Schemas a model names are created first.
- `TestSequelizeOracle` compares every claimed fact with the catalog;
  uniqueness is compared per column set and per column (the cyclic
  path's `_key1` duplicates count once), jsonb default texts as JSON
  values. Over the fixture it also checks that every oracle form
  `TestSequelizeClaims` allows was compared.

### Review findings (task Revision 3)

- Payload version 4 (`docs/lang/typescript-models.md`): module-level
  `for...of` loops (row S7), `if` statements without `else` in captured
  bodies (row B-if), a class's static members (plain static methods as
  function statements, every other static member unknown with its name),
  and declarations initialised by `<call>[<expr>]` (row S1). No gate row
  changes, so the aggregate extractor version stays `extractors-35`
  (unmerged); version 3 payloads miss once.
- mssql's foreign-key actions follow its generator
  (`dialects/mssql/query-generator.js:488-494`, `:542-561`),
  compared by `TestDDLRendering` with Sequelize's own rendering
  (no driver is installed, so this stays `mapped`).
- Instance hooks (`addHook`, hook-type methods on the instance or the
  class, a `hooks` constructor option) leave the group unclaimed (row D;
  R3-B1). A foreign key needs a primary-key or unique target column: a
  provably non-unique one is an SQ-P1 failure, an unproven one leaves the
  list unknown, and a table referencing a table sync does not create is
  refused in turn (R3-B2). SQ-P1's refusal column and `failures.go` list
  the same failures (R3-B3); a failure that throws while the models load
  stops the definitions and association calls after it, and those whose
  order to it is not proven (R3-C1).
- The foreign-key step is one guarded, all-or-nothing step per group
  (R3-C2); a partial table always has a table-level reason, and a model
  of another package on an instance marks the instance's group (R3-C3).
- The sequelize-cli forms are read (R3-C4, decision): the
  `Object.keys`/`Object.values` loops (`forEach` or `for...of`, `&&` or
  `if` guard), `static associate(models)`, `M.X = require(...)(...)`
  entries and inline `require(...)(...)` factory calls, and
  `require(config)[env]` options under open point O2's agreement rule;
  the template's `fs.readdirSync` loader and class + init inside a factory
  stay unread (markers).

## Review notes (delegated review, 2026-11-14)

Accepted after the green `[ci windows]` run 43605833309 on f921361
(Windows and postgres green; postgres job 0 FAIL, only env-gated skips).

- Results: Sequelize v6 model extraction (`Model.init`, `define`,
  sequelize-cli template forms, helper members, associations,
  references), TypeScript model payload v4, extractor version
  extractors-35, 15-row claim contract 1:1 with code and docs.
- Oracle (real `sync()` on scratch Postgres 17 and SQLite, corpus modules
  read first and side effects stubbed): fixture 10 runs 2,314 facts,
  opencollective-api 6,952, Gladys 3,077, 0 disagreements; mysql, mariadb
  and mssql facts `mapped` from Sequelize 6.37.8's query generator, with
  claimed DDL compared against that generator (`TestDDLRendering`).
- Revisions: 1 (open points, decorator capture moved to T018g6, oracle
  tooling), 2 (helper members, SQB4's extra refusals), 3 (independent
  review: mssql FK actions, instance hooks, non-unique target keys,
  failure list 1:1, load-time throws, all-or-nothing FK step, the
  sequelize-cli template forms).
- Coverage: opencollective-api 52/52 tables, 747/750 columns, FKs
  131/147 (cyclic alter-pass refusals), uniques equal to the catalog
  after `_key1` duplicates; Gladys all targets.
- Gates: fresh/warm index within 10 % of main on opencollective-api,
  Gladys and langflow; store +1.94 % / +0.75 % / +49 KB; other corpora's
  output identical.
- Assumptions shown: SQ-V1..V6 (sync creates the schema, module imports
  succeed, config and environment selection, mapped dialects).
- Known gaps (labelled, no wrong fact): `fs.readdirSync` model loaders,
  class + init inside a factory, `this.belongsTo` in static associate,
  associations modules outside gate G4, unread belongsToMany options and
  the `name` option, migrations (found, not read).
- After v1: sequelize-typescript decorators (with T018g6), migration
  reading, narrowing gate G9.
- Also carried on this branch: `duplicate_crate_name` diagnostics on
  valid paths and the rule that an invalid diagnostic never aborts a run
  (bug on main since T019a's fixture).

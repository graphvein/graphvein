# ADR 0063 - GORM models and the Go model payload

**Status:** Accepted (delegated review, 2026-11-16)

GORM v2 models (`docs/contract/claims/gorm-models.md`, plan
the T018 task record (internal) "ORMs in the other first-tier
languages"). Written with the change that adds the cached Go model payload
(version 1) and its cache wiring. Discovery, node shapes, row and
assumption ids and the oracle are fixed by the task file's claim contract,
The investigation results and Revision 1 (2026-11-15); steps GB2-GB5 implement them
and add their implementation notes here.

## Context

GORM states tables in Go structs: exported fields with `gorm:"..."` tags,
embedded structs (`gorm.Model`), named field types, `TableName()` methods
and the configuration passed to `gorm.Open`. The tables exist only after
`db.AutoMigrate(models...)` or `db.Migrator().CreateTable(...)` runs on a
`*gorm.DB`, and the DDL depends on the dialector, the naming strategy, the
migrate order (has-relation foreign keys register on the child only when
the parent's schema was parsed first) and GORM's own reordering, which
adds belongs-to targets and join tables nobody listed (task file).
gotify keeps its five models in files that import only `time`;
realworld migrates in seven calls across two packages, one through a
package function (`users.AutoMigrate()`).

On a cache hit the indexer replays stored facts and never parses the file,
so the link pass has no tree (`internal/index/index.go`); a second parse
path outside the bounded workers is ruled out, as for Drizzle (ADR 0059)
and Sequelize (ADR 0061). The Go facts cached before this ADR keep one
node per declaration, the imports and the calls with their callee name:
no struct fields, tags, embedded fields, underlying types, method bodies,
call arguments, receiver chains or statement nesting.

## Options considered

1. **Read retained sources at link time.** Rejected: no tree on a cache
   hit, and a second parse path outside the bounded workers (ADR 0012).
2. **Gate the whole payload on GORM imports.** Smallest cache, but misses
   real models: gotify's five model files import only `time`, and an
   untagged model in an import-free file is a real form. Rejected for the
   type shapes; kept for the GORM sites (gate G1).
3. **Type shapes in every non-test Go file, GORM sites in gated files
   (chosen, task open point O3).** Reach is cross-file: a migrate call in
   one package names structs, embedded structs, field types and constants
   in others. The self-index (322 non-test Go files, 829 structs, 4,108
   fields) bounds the cost; the timing gate decides (fallback below).
4. **Fallback (O3): type shapes only in files passing G1 or a `gorm:`
   tag (G2).** Smaller, but a model, embedded struct or named type in
   another file becomes unknown. Applied only if the timing gate fails.
5. **Capture all methods, locals and calls of every file.** Rejected: GORM
   reads five method names, and discovery reads a closed set of call
   selectors; the rest is cache weight without a rule that reads it.

## Decision

### Payload (version 1)

`facts.Facts.Go` (type `facts.GoFacts`, `facts.GoModelsVersion` 1),
captured by `internal/lang/golang` in the bounded extraction workers;
syntax only (rule 31). The payload table in `docs/lang/go-models.md`
(rows G1, GT, GD, T-*, V1, F1, F-param, L-*, X-*, N-*, FD, E-*) is the
capture contract; `TestGoPayloadContract` keeps it, the Go row table and
the capture branches 1:1.

- **Every non-test Go file:** package-level struct types (fields with
  names, embedded and pointer flags, type trees, decoded tags), named
  non-struct types and aliases (underlying type trees), the methods GORM's
  schema parse calls (`TableName`, `Value`, `Scan`, `GormDataType`,
  `GormDBDataType`: receiver base type, pointer flag, parameter and result
  counts; a `TableName` body that is one `return e` keeps `e`), and
  package-level string-shaped constants (string literals, names,
  selectors and `+`), which Revision 1's O7 resolves `TableName` bodies
  through.
- **Gate G1** (an import of `gorm.io/gorm` or a package under it, a
  `gorm.io/driver/...` package, `github.com/glebarez/sqlite`,
  `github.com/jinzhu/inflection`, or `github.com/jinzhu/gorm` or a package
  under it): package-level vars with type and value trees, and per
  function or method with a body its receiver, parameters and results,
  the calls GORM discovery reads (selectors `AutoMigrate`, `CreateTable`,
  `Open`, `New`, `Migrator`, `Set`, `Session`, `Debug`, `WithContext`, and
  calls on the inflection import) with their receiver chains and
  arguments, the locals those calls and the configuration mutations use
  (transitively), assignments to configuration selectors, the top-level
  call statements that the order rule expands, and a nesting mark per
  local, site and mutation (`unconditional`, `if-header`, `conditional`,
  `closure`) with the index of its top-level statement.
- **`_test.go` files** carry only the version: GORM discovery never reads
  them (row GO-K1, open point O10).
- **Expression trees:** identifier, selector, call, composite literal with
  keyed and positional elements, unary, binary (Revision 1, O7: `+` of
  constants), string, number, boolean, nil, spread, index and generic
  instantiation, and the type kinds pointer, slice, array, map, chan,
  func, interface and struct; parentheses transparent; everything else
  `unknown`; depth bounded at 48 as the TypeScript model payload.
- **Validity:** `GoFacts.ModelsValid()` (version, gated-only fields,
  required fields, kinds, nesting marks); the indexer's cache check also
  misses on a Go file without a payload, so entries of another version,
  damaged entries and entries written before the bump are re-extracted. A
  capture failure (recovered panic) leaves the payload absent with a
  `model_syntax_capture_failed` warning; the file's other facts and every
  other file are kept, and the next run tries again.
- **Aggregate extractor version** extractors-36 (placeholder; renumbered
  at merge if another stack merges first).

### Linking

As the task file fixes them: module and pins from `go.mod` (GO-K1);
module-own import resolution factored out of `golang.Link` (`moduleAt`,
`packageFiles`); `gorm.Open` sites, dialect and configuration (GO-K2);
migrate sites, receiver proof, order and `ReorderModels` auto-add
(GO-K3); the naming port with inflection v1.0.0 ported under a table
test against the real library (GO-K4, O4); models, fields, types, column
facts, indexes and relations (GO-M1-GO-R3); provable failures (GO-P1);
the v1 marker (GO-U1); assumptions GO-V1-GO-V7 on the records; the
default branch D. Records go through the model kit (ADR 0058) in group
`models/gorm/<module dir>`; runtime-dialect groups take per-dialect
claims from the kit (T018g0b, ADR 0065) once merged, unknown with a
reason until then. The oracle lets GORM migrate into a scratch
Postgres 17 and a scratch SQLite file through `github.com/glebarez/sqlite`
and compares GORM's parsed schema and the catalogs with every claimed
fact.

## Consequences

- Go fact cache entries grow by the type shapes of every non-test file
  (measured in GB1, below) and, for gated files, by the GORM sites; every
  Go file misses once (the aggregate bump and the payload check).
- The payload is syntax: reach, receiver proof, dialect, configuration
  and naming are decided at link time over the current inventory, so
  editing one file never leaves another file's cached claim stale.
- Goldens change only through the version string.

## Known gaps

- A migrate call in a file that passes no gate (a receiver whose type is
  inferred from another package's function result, with no GORM import
  in the file) has no site in the payload; its callee name is still in
  `Facts.Calls` (`StaticName` `AutoMigrate` or `CreateTable`), and
  discovery marks the group's table list as possibly incomplete (GB2).
- A receiver declared as a function literal's parameter (gormigrate's
  `func(tx *gorm.DB) error`) is not in the payload, so such a site is D.
- Struct types declared inside functions are not captured; a migrate
  argument naming one is D under GO-K3 (Revision 2 captures their names,
  `LocalTypes`, so the argument is not mistaken for the package's type).
- The handle flows (Revision 2, A4) see the gated files only: a struct
  literal or field assignment in a file outside gate G1, a function used
  as a value (its calls are not resolved), and an assignment to a
  package-level `*gorm.DB` variable are not followed.
- A `Migrator()` DDL call or DDL text (Revision 2, A3) is read where it
  is captured and literal: SQL built at run time, a migration tool and
  DDL in a file outside gate G1 under another name are outside the
  records (GO-V2). A dropped or renamed column stays listed (as an
  unknown column) and a dropped or renamed table stays listed with
  partial sets. DDL text is read statement by statement with a small
  tokenizer (quotes, comments, `;`), not a SQL parser: only `ALTER
  TABLE`, `DROP TABLE`, `RENAME TABLE` and `CREATE INDEX ... ON` name a
  target; any other ALTER, DROP or RENAME is taken to change every table
  (Revision 2 addendum). A `CREATE TABLE IF NOT EXISTS` naming a claimed
  table before `AutoMigrate` would leave its columns' types to the text;
  it keeps the marker-only rule.
- Methods with other names, other calls and unused locals are not
  captured; no contract row reads them.
- Relation fields of embedded structs (`guessEmbeddedBelongs`,
  `guessEmbeddedHas`) and polymorphic relations are not ported (row D:
  the foreign keys of both sides and the types the relation may copy are
  unknown) (GB4).
- GORM iterates its relation maps, so the order among a value's
  dependencies, and the side that builds a join table declared twice, is
  not fixed: the simulation leaves what depends on it unknown (a failure
  inside such a segment withholds the whole segment; open point O8)
  (GB4).
- A migrate site that is not read makes every claimed table's foreign
  keys unknown (it may parse a model whose has-relation registers on the
  table); a finer rule would scan the module's structs for has-shaped
  fields (GB4).
- `MigrateColumn` on an existing table is not ported: a type copy whose
  owner is parsed between the table's creation and a later migration
  leaves the type unknown (GB4).

## Implementation notes

### Go model payload version 1

Capture in `internal/lang/golang/models.go` (called from `Extract` through
a recover guard), types and validity in `internal/lang/facts/golang.go`,
the cache check in `internal/index/index.go`. Tests: payload forms over
inline sources (`models_test.go`: type shapes, sites, receivers, chains,
configuration literals, mutations, nesting, inflection, comments, depth,
damaged entries, capture failure), `TestGoPayloadContract`,
`TestGormCensus` over gotify and realworld against
`docs/contract/claims/data/gorm-models-inventory.jsonl` (`scripts/cloud-corpus.sh gotify
realworld-gin`), and `TestGoModelPayloadCache` in `internal/index`.
Timing (interleaved with main, fresh and warm, heavy lock, medians of 4
rounds on a quiet machine): self-index fresh +6.5 %, warm -3.2 %; gotify
fresh -3.7 %, warm +0.2 %; store +0.35 % (self-index) and +0.59 %
(gotify); the capture's in-process cost over the self-index is below the
noise and its cached Go facts grow by 2.6 %. The gate holds, so the O3
fallback is not applied (details and the loaded-machine sets in
the T018g5 task record (internal), GB1).

### Discovery and contract table

`internal/extract/models/gorm`: `Discover` (`discover.go`: modules,
pins, version status, files read, GO-M2 and GO-U1 markers; `handle.go`:
`gorm.Open` sites, dialectors, configuration and its merge, mutations;
`sites.go`: migrate sites, chain, receiver proof, arguments, the
ungated-file marker; `order.go`: the order rule and its expansions) and
`contract.go` (22 entries: 21 rows plus D). `go.mod` is read by
`lockfile.ParseGoMod` (`internal/extract/models/lockfile/gomod.go`);
`golang.ModuleAt`, `golang.ModuleImportDir` and `golang.PackageFiles`
(`internal/lang/golang/modules.go`) are the module-own resolution
factored out of `golang.Link`, which now calls them. Version range per
Revision 1 (O5): v1.25.12-v1.31.2 verified, later v1 read under GO-V1
with "rules verified at gorm 1.25.12-1.31.2", older refused. Tests:
`TestGormContract`, `TestGormDiscoveryTags`, the inline discovery tests
and `TestGormDiscoveryCorpus` (every GO-K1, GO-K2, GO-K3 and GO-M1
discovery and read row of the inventory, both ways: gotify 21, realworld
30; no marker on either corpus). Docs: `docs/extract/schema-gorm.md`.

### Schema port

`internal/extract/models/gorm`: `Link` (`tables.go`: one kit group per
read module, the not-claimed markers, the tables, columns, indexes and
unique constraints), `schema.go` (the schema parse over the payload:
type classification, `ParseField`, embedding, the primary-key and
autoincrement rules, the `TableName` method with promotion and Go
constant folding), `render.go` (per dialect: postgres catalog types,
sqlite declared types, mysql mapped types, autoincrement, not-null and
defaults), `naming.go` (the default `NamingStrategy` and `formatName`
with the dialect limits), `inflection.go` (jinzhu/inflection v1.0.0),
`tags.go` (`ParseTagSetting`, `CheckTruth`) and `fkcopy.go` (types a
relation may copy onto a foreign key, withheld until GB4 reads
relations; removed in GB4). A run-time group states every dialect-dependent fact per
dialect through the ADR 0065 kit (`oracle` for postgres and sqlite,
`mapped` for mysql); the column `type` is the dialect's SQL type (GO-T2)
and GORM's own data type is the unclaimed `tool_type` annotation. The
indexer does not call `Link` yet (GB4). Tests: `TestInflection` and
`TestNaming` against the real libraries' outputs
(`testdata/port-1.31.2.json`), `TestGormFixture` (golden
`testdata/link.golden.json` over `testdata/fixtures/gorm-models/`, part
1), `TestGormClaimTags`, `TestGormStepIsolation` and the inline rule
tests (`tables_test.go`). A scratch comparison of the fixture, gotify
and realworld records with the investigation catalogs found no disagreement
(GB5 commits the oracle).

### Relations, migrate order, failures and wiring

`internal/extract/models/gorm`: `relations.go` (the relation parse:
`parseRelation`, `guessRelation` with its guess levels and positional
pairing, the type copy recorded with the schema whose parse makes it,
the has-relation registration on the child, `buildMany2ManyRelation`
with the tag rewriting of `utils.go:54-67`, `ParseConstraint` with the
reverse-relation yield; polymorphic and embedded relations not read),
`migrate.go` (the call simulation: the call list from the proven
sequence or every proven site, each call's parse closure, the
`ReorderModels` port with deferred join tables and the ineffective
many2many dependency, segments, provable failures and the narrowing to
the failing table and the later ones, index-name clashes in creation
order, the tri-state parse-order answers for an order that is not
proven) and `foreignkeys.go` (constraints per table, names per dialect,
actions, the type copy decision, join-table agreement); `catalogue.go`
registers the `/api/v0/schema/assumptions` catalogue. `fkcopy.go` is
removed. A module under `testdata`, `vendor` or a `.`/`_` directory is
refused (GO-V7). The indexer calls `gorm.Link` after the Sequelize front
end (`internal/index/index.go`). The server lists GORM v1 imports in
`unread_tools` (`gorm-v1`, one entry per package directory; Revision 1
addendum 2, P2). Tests: the full contract test (`TestGormClaimTags`:
every row with a branch and every listed form reached by the fixture),
`TestGormCatalogue`, `TestGormRelationOrder`, `TestGormIndexedFixture`
(golden `testdata/fixtures/gorm-models.golden.json`), the fixture's
relation modules and `TestGormCorpusLink` at the investigation coverage targets
(gotify 5 tables, 42 columns, 10 indexes, foreign keys `[]`; realworld 8
tables, 46 columns, 9 indexes, the 10 foreign keys of the investigation sqlite
catalog).

### Oracle

`scripts/oracle/gorm_catalog.sh` with `scripts/oracle/gorm_oracle/`
(files under `//go:build ignore`, so neither `go vet ./...` nor `go mod
tidy` of the graphvein module reads them and no dependency is added):
scratch modules at gorm 1.31.2 and 1.25.12 with the postgres driver and
glebarez, the model packages copied by `copytypes.go` (types, constants,
literal variables, GORM's schema methods), one run per module in source
order, every call run even after a failure, the catalog dumped; GORM's
parsed schema is dumped for reading a disagreement, and the comparison
(`TestGormOracle`) reads the catalog only, since every claimed fact is a
database fact (the GORM data type is the unclaimed `tool_type`). The
fixture's `pg/` run creates the domain `blob` its custom type needs
(GO-V2, O11). Results: the T018g5 task record (internal).

### Revision 2 (independent review)

The decisions on the independent review (task file,
"Revision 2"). The payload changes inside extractors-36 (the branch is
not merged, so no released cache holds the earlier shape): sites gain
`SetupJoinTable`, `Exec`, `Raw` and the Migrator DDL methods
(`facts.GoMigratorDDL`); gated functions gain `LocalTypes`, `Returns`
(of a function whose first result is `*x.DB`), `Calls`, `Stores` and
`Literals` (where a value of handle shape is passed on). The receiver
proof follows a handle through returns, parameters, fields and
assignments and refuses any path through another chain method. A raw
type text with more than one type name, `SetupJoinTable`, Migrator DDL
calls and DDL text leave the facts they may change unknown or partial;
the postgres index class and the sqlite bare function default are
provable failures (confirmed by the oracle). Driver pins outside the
verified versions are shown with GO-V1, and a postgres driver before
v1.5.6 leaves names over 63 bytes unknown (its 64-byte hash, which
postgres truncates, confirmed by the oracle at v1.5.4). A dialector local
assigned several dialectors gives a runtime group. Discovery guards each
module and the indexer recovers a failure of the whole pass. The server's
unread-tools rule reads the GO-U1 markers through a new store read,
`DiagnosticsWithMessage` (code and message text, checked again in Go),
tested on SQLite and Postgres.

### Revision 2 addendum

An `Exec`/`Raw` DDL literal is split into statements (`;` outside quotes
and comments, `internal/extract/models/gorm/rawddl.go`). A statement
`ALTER TABLE <name>` (with `IF EXISTS`, `ONLY`, a `RENAME TO <new>` naming
the new table too), `DROP TABLE <name>, ...` or mysql's `RENAME TABLE a TO
b, ...` names its tables (plain, `"quoted"` or `` `quoted` `` identifiers,
the last part of a qualified name, compared without case so a spelling
never hides a change); that table keeps only its table and column names:
column and constraint sets partial, schema, primary key, index and
constraint lists and every column unknown, with a GO-K3 reason naming the
statement (as Diesel's non-benign patch removal, ADR 0062). Any other
ALTER, DROP or RENAME (a type, an index, a sequence, which `CASCADE` can
spread to any table) is read as naming no table: every table of the group
keeps only its names. `CREATE [UNIQUE] INDEX ... ON <table>` leaves that
table's index and constraint lists unknown (every table's when no `ON`
table is read); another CREATE keeps the Revision 2 rule (marker, list
marker, foreign keys unknown). The marker now looks at every statement,
not only the text's first word. No oracle run carries such text, so the
oracle comparison is unchanged.

Part 2: a `CREATE [modifiers] TABLE [IF NOT
EXISTS] <name>` statement names its table like an ALTER (that table keeps
only its names, whatever the statement's order beside the migrate calls,
which is not proven), and one whose target is not read leaves every table
of the group only its names; other CREATE statements keep the marker
rule.

## Review notes (delegated review, 2026-11-16)

- **Steps:** the investigation inventory and oracle baseline, GB1 Go model payload
  v1, GB2 discovery and contract table, GB3 schema port, GB4 relations,
  migrate-call simulation and failures, GB5 oracle, GF review fixes
  (Revision 2), GG raw-DDL rule and gate, GH raw CREATE TABLE rule.
- **Independent review (GR):** class A findings all fixed: raw `type:`
  texts (A1), SetupJoinTable (A2), schema changes outside the migrate
  calls (A3), the receiver proof (A4), new GO-P1 forms confirmed on the
  oracle (A5), local types (A6), driver pins outside the verified range
  (A7). Follow-ups: an `Exec`/`Raw` ALTER, DROP, RENAME or
  CREATE TABLE naming a claimed table leaves it its names only (an
  unreadable target: every table of the group), CREATE INDEX makes the
  table's index lists unknown, whatever the statement order.
- **Results:** oracle 0 disagreements on claimed facts (fixture 3,737
  facts; gotify 888 on postgres and sqlite; realworld-gin 505 plus 312
  cross-dialect on postgres); MySQL facts are `mapped`. Corpus snapshot:
  16 of 18 corpora identical to main, gotify gains 5 complete tables,
  realworld-gin 8. Timing within 10 % (self-index +1.8 % fresh / +1.3 %
  warm, gotify +1.3 % / +1.5 %, langflow +1.7 % / +2.6 %); store gotify
  +3.0 %, self-index +0.5 %. CI run 43774517569 (`ci/s30-t018g5`):
  Windows and postgres green, postgres raw log 0 FAIL, 161 `/postgres`
  subtests, only env-gated skips.
- **Assumptions shown:** GO-V1..GO-V7.
- **Known gaps / after v1:** MySQL not compared on a server; an
  application database created by older versions; build constraints and
  generated files outside the oracle; GF's handle-flow follow-ups.

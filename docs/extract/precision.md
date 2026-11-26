# Precision labels: the rule table

Graphvein labels each scope `precise` or `best_effort` (ADR 0066, task
T020). A scope is `precise` only when a row of the table below names it,
every piece of the row's evidence exists, and the facts were produced by
Graphvein's own indexer at the binary's extractor version. Everything
else is `best_effort`: the single default branch, row D.

`precise` never means complete. It means Graphvein states no fact on the
forms the scope's claim contract does not cover, and shows what it could
not read as unknown. A scope label never replaces a fact-level label: a
`mapped` fact in a precise scope still shows "unverified", an unknown stays
unknown, and an inferred edge keeps its confidence.

The code is `internal/precision` (`rules.go`). `TestPrecisionRules` keeps
the Go table, this table and the table in
`docs/contract/claims/precision-labels.md` 1:1 (row IDs, scopes, evidence
names) and checks every row's evidence. The task file is a record: a file
row added after it was written must be one of its planned rows with the
same scope.

## Two kinds of label

- **File label** (contract v1 file `precision`, copied to the file node's
  `attributes.precision`): the language's structural scope only (files,
  declarations, imports). The scanner asks `precision.FileLabel` for every
  file. A precise fact family read from a file never raises its file label,
  and a best-effort one never lowers it. Go has row PL-L1 (T020),
  Python row PL-L2, TypeScript, TSX and JavaScript row PL-L3
 and Rust row PL-L4: all four planned languages have
  their rows, each with its own claim contract, oracle and independent
  review; every other language is `best_effort`. The file label of a
  precise language is an upper bound: the indexer stores `best_effort` for
  a file whose structure it refused (for Go: a file go/parser rejects, a
  file with a lone CR; for Python: a file the reference Python rejects; for
  TypeScript, TSX and JavaScript: a file tree-sitter rejects, TS-F2; for
  Rust: a file in which tree-sitter reports a syntax error, a file with a
  lone CR; for all of them: a file it could not parse or extract, or one
  dropped by validation), and the server serves it with reason
  `structure_refused`. A Rust file no crate root of a read `Cargo.toml`
  reaches is stored `best_effort` too, with an info diagnostic
  `structure_unrooted`, and served with reason `structure_unrooted`: its
  declarations are claimed, but its imports are not resolved as a crate
  member and no oracle configuration compiles it.
- **Scope label** for a fact family: computed by the server with
  `precision.ScopeLabel` from a marker it reads from stored data (the
  family, the group's `tool`, the migration set's `dialect`). Not stored
  and not part of the contract.

## Definition of `precise`

A row earns `precise` when all of these hold; `TestPrecisionRules` checks
each from the row's evidence fields.

| # | condition | how the test checks it |
|---|---|---|
| P1 | Claim contract with a 1:1 test and a single default branch | the named test functions exist in the named package's test files (`go/parser`) |
| P2 | Golden fixtures cover the scope | `testdata/fixtures/<name>.golden.json` or `testdata/repos/<name>.golden.json` exists |
| P3 | Corpus checks recorded | the cited quote is in the named ADR's "Review notes" section |
| P4 | One independent review, no open class A finding | the cited quote is in the named ADR's "Review notes" section |
| P5 | Oracle where cheap | the named oracle test functions exist, or the row states `oracle: none` with a reason |
| P6 | The rule belongs to the running extractor | the table's digest is pinned next to `index.Version`; changing a row without a version bump fails |

Quotes are compared after collapsing whitespace and removing Markdown bold
markers. The test checks that the evidence exists and is cited, not that
the review was good; T020's independent review (R1) reads every row by
hand.

## Refusals (the label falls to row D)

Before any row is consulted, `ScopeLabel` refuses:

| reason | when |
|---|---|
| `no_run` | no run is stored for the repository: nothing to label, and no producer to name (T020 review) |
| `external_indexer` | the run was produced by an external indexer (ADR 0010) |
| `extractor_mismatch` | the run's extractor version is empty or differs from the binary's `index.Version`: the table that earned the label may not be the one that produced the facts |
| `default_branch` | no row matches the marker (including a marker with no family, and a row's own refusal below) |
| `marker_conflict` | no row matches and the scope's records name more than one tool or dialect (or some name none), so no single row can be named |

A run whose header names no indexer cannot occur:
`docs/contract/v1/header.schema.json` requires `indexer.name` with
`minLength: 1`, and the store's `BeginRun` validates every header against
it, so no reason exists for it.

## Rule table

| row | scope | marker (what is labelled) | P1 contract test | P2 goldens | P3 corpus | P4 independent review | P5 oracle | refusal (falls to D) |
|---|---|---|---|---|---|---|---|---|
| PL-F1 | SQL migration schema, sqlx sets, postgres dialect | `migration`, `table`, `column`, `db_index` nodes of a migration set with tool `sqlx` and dialect `postgres`; their `contains`, `alters`, `foreign_key` edges | `internal/extract/schema`: `TestContractRows`, `TestContractDocument` | `sql-migrations` | ADR 0051 review notes | ADR 0047 review notes, ADR 0051 review notes | `TestOracleFixtures`, `TestOracleRealHistories` | a set with another or no dialect (`schema_unsupported_dialect`), or another tool |
| PL-F2 | SQLModel / SQLAlchemy models | model groups with `tool` `sqlmodel` | `internal/extract/models/sqlmodel`: `TestModelClaimContract` | `sqlmodel-models`, `sqlmodel-link` | ADR 0054 review notes | ADR 0054 review notes | `TestModelOracle` | none beyond the global ones |
| PL-F3 | Prisma models | model groups with `tool` `prisma` | `internal/extract/models/prisma`: `TestPrismaContract` | `prisma-models` | ADR 0057 review notes | ADR 0057 review notes | `TestPrismaOracle` | none beyond the global ones |
| PL-F4 | Drizzle models | model groups with `tool` `drizzle` | `internal/extract/models/drizzle`: `TestDrizzleContract` | `drizzle-models` | ADR 0059 review notes | ADR 0059 review notes | `TestDrizzleOracle`, `TestDrizzleOracleForms` | none beyond the global ones |
| PL-F5 | Sequelize models | model groups with `tool` `sequelize` | `internal/extract/models/sequelize`: `TestSequelizeContract` | `sequelize-models` | ADR 0061 review notes | ADR 0061 review notes | `TestSequelizeOracle` | none beyond the global ones |
| PL-F6 | Diesel models | model groups with `tool` `diesel` | `internal/extract/models/diesel`: `TestDieselContract` | `diesel-models` | ADR 0062 review notes | ADR 0062 review notes | `TestDieselOracle` | none beyond the global ones |
| PL-F7 | GORM models | model groups with `tool` `gorm` | `internal/extract/models/gorm`: `TestGormContract` | `gorm-models` | ADR 0063 review notes | ADR 0063 review notes | `TestGormOracle` | none beyond the global ones |
| PL-F8 | TypeORM models | model groups with `tool` `typeorm` | `internal/extract/models/typeorm`: `TestTypeORMContract` | `typeorm-models` | ADR 0064 review notes | ADR 0064 review notes | `TestTypeORMOracle` | none beyond the global ones |
| PL-F9 | Code-to-table links | `reads_table` / `writes_table` edges and the owner's `table_access` rows | `internal/extract/tableaccess`: `TestTableAccessContract` | `table-access-sqlmodel`, `table-access-prisma`, `table-access-drizzle`, `table-access-sqlx` | ADR 0060 review notes | ADR 0060 review notes | `TestSQLTablesOracle`, `TestSQLAlchemyOracle` | owner sites in `table_access_unknown` stay unknown (not labelled) |
| PL-F10 | Go HTTP routes | `route` nodes with `framework` `net/http`, `chi` or `gin`: route existence, `method`, `pattern`, `host` and the path entries' `template`, `trailing_slash` and `resolution`, with the GR-V assumptions shown, and the `go_routes_unread` counts (`docs/extract/routes.md`, Go section); not `handler`, `middleware`, the `exposes_route` edges, `conditional` or `scope` (no oracle compares them: served `unverified`, edges `not_compared`), nor the `calls_route` links of client calls (`client_calls`, row D) | `internal/extract/routes`: `TestGoRoutesContract`, `TestGoRoutesContractTags`; `internal/lang/golang`: `TestGoRoutePayloadContract` | `go-routes` | ADR 0076 review notes | ADR 0076 review notes (GR-R and its confirmation pass) | `TestGoRoutesMuxOracle`, `TestGoMuxPatternOracle` (in the suite; the chi/gin graph oracle and the source oracle of `scripts/oracle/go_routes` opt-in) | routes of another framework (Express, FastAPI, Axum) stay best effort; a transport whose routes name several frameworks is `marker_conflict` |
| PL-L1 | Go structure | files with language `go` (test files included): file records and nodes, top-level declarations and their `contains` edges, `imports` edges (`docs/lang/go-structure.md`); not `calls` edges (`code_calls`, row D) | `internal/lang/golang`: `TestGoStructureContract` | `go-structure`, `go` | ADR 0068 review notes | ADR 0068 review notes (S1R) | `TestGoStructureOracle`, `TestGoStructureGoList` | a Go file whose structure claims nothing (go/parser rejects it, GS-F2; a lone CR, GS-F3; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused` |
| PL-L2 | Python structure | files with language `python` (test files included): file records and nodes, top-level declarations and their `contains` edges, `imports` edges to repository files and to `python-stdlib` / `pypi` external nodes (`docs/lang/python-structure.md`); not `calls` edges (`code_calls`, row D) | `internal/lang/python`: `TestPythonStructureContract` | `python-structure`, `python-implicit-roots`, `python` | ADR 0069 review notes | ADR 0069 review notes (S2R) | `TestPythonStructureOracle` (CPython 3.14.10; runs in CI's postgres and main jobs, which set up CPython 3.14 with setup-python) | a Python file whose structure claims nothing (the reference Python rejects it, PS-F2; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused` |
| PL-L3 | TypeScript, TSX, JavaScript structure | files with language `typescript`, `tsx` or `javascript` (declaration and test files included): file records and nodes, declarations and their `contains` edges, `imports` edges to repository files and to `npm` external nodes (row TS-I5, `docs/lang/typescript-structure.md`); not `calls` edges (`code_calls`, row D) | `internal/lang/typescript`: `TestTypeScriptStructureContract` | `typescript-structure`, `module-resolution`, `typescript` | ADR 0070 review notes | ADR 0070 review notes (S3R) | `TestTypeScriptStructureOracle` (TypeScript 6.0.3 compiler API; opt-in: skips without `GRAPHVEIN_TS_ORACLE_DIR`, so it does not run in CI) | a TypeScript, TSX or JavaScript file whose structure claims nothing (tree-sitter rejects it, TS-F2; not parsed or not extracted, TS-F3; or dropped by validation) is stored `best_effort`, served with reason `structure_refused` |
| PL-L4 | Rust structure | files with language `rust` (tests, examples, benches and build scripts included): file records and nodes, module-level declarations, impl associated `const` / `type` items (RS-D9) and items in the bodies of claimed functions (RS-D10, `local: true`) with their `contains` edges, `mod` and `use` `imports` edges to repository files and to `cargo` external nodes for declared dependencies and sysroot crates (`docs/lang/rust-structure.md`); not `calls` edges (`code_calls`, row D) | `internal/lang/rust`: `TestRustStructureContract` | `rust-structure`, `rust` | ADR 0071 review notes (S4F, 12 oracle configurations; 48 entries no configuration reaches are a known gap) | ADR 0071 review notes (S4R) | `TestRustStructureOracle` (syn 2, `cargo metadata`, rustc 1.97.0 `-Zunpretty=hir-tree`; opt-in, never in CI) | a Rust file whose structure claims nothing (tree-sitter reports a syntax error, RS-F2; a lone CR, RS-F3; not parsed, not extracted or dropped by validation) is stored `best_effort`, served with reason `structure_refused`; a Rust file reached from no crate root of a read `Cargo.toml` is stored `best_effort`, served with reason `structure_unrooted` |
| D | everything else | every file label of a language no PL-L row names (all languages but go, python, typescript, tsx, javascript and rust, including java, c_sharp, dart, html, sql, unknown); routes other than Go's `net/http`, chi and gin routes (PL-F10), client calls, code calls, cross-boundary links, cross-repository links (never precise), Angular, Tauri, OpenAPI, test scope, any external-indexer run | - | - | - | - | - | - (this is the default) |

The verbatim quotes each row cites are in `internal/precision/rules.go`.

PL-F9's scope is the links into tables of a group whose tool is `sqlx`,
`sqlmodel`, `prisma` or `drizzle` (`Match.Tools`; the marker of a link is
its target table's group tool). Links into Supabase tables (T024c2, rows
SB of `docs/extract/table-access.md`) fall to row D until the SB rows
carry their own evidence; `/api/v0/precision` counts them as the row D
family `table_access_other`, and a change item of such a link (or an
owner's `table_access` field holding one) is `best_effort`. A link whose
target table the server cannot read on the head side is `best_effort`
with `scope_unknown`.

Mapped dialects inside PL-F3..PL-F8 (mysql, mariadb, mssql, sqlserver,
cockroachdb where the contract lists them) are covered by the row: the
per-fact `verification: mapped` already says "unverified" (T020 open
point O13). On the Schema section's source card, a Precise model group
with a table or column fact that is not oracle-verified carries a note
beside its chip, from `/schema/sets` fields alone (T020 review):
"dialect unknown" when the group states no dialect (a Prisma provider
that is not a literal), else "mapped: unverified". A group whose facts
are all oracle-verified (SQLModel, which states no dialect) gets none.

## Row D families

`/api/v0/precision` lists each fact family it labels through row D with
the facts this index holds in it (`default.families`, in this order), so
a family that no row earns is still counted, never left out:

| family | facts counted | unit |
|---|---|---|
| `schema_other` | schema tables of groups no row names (a sqlite set, the `set: null` group, a conflicting tool) | tables |
| `table_access_other` | `reads_table` / `writes_table` edges into tables of a group whose tool row PL-F9 does not list (Supabase tables, T024c2) | edges |
| `routes` | `route` nodes | nodes |
| `client_calls` | `client_call` nodes | nodes |
| `ipc_commands` | `ipc_command` nodes | nodes |
| `ipc_calls` | `ipc_call` nodes | nodes |
| `server_actions` | `server_action` nodes (T024b) | nodes |
| `action_calls` | `action_call` nodes (T024b) | nodes |
| `code_calls` | every code `calls` edge (ADR 0073) | edges |
| `cross_repository_links` | under `serve --project`, the stored project links with an end in this member, by that end's file language; 0 outside project mode | links |

`TestPrecisionDefaultFamilies` keeps this table 1:1 with the server's
list.

## Cross-repository links

A link between two repositories of a project (ADR 0075, T022: kinds
`calls_route` and `documents_route` written by the project link pass) is
never `precise`, whatever the file labels of its two ends. Each rests on
an assumption its evidence does not prove: that the call reaches this
provider ("matched across project repositories; base URL not proven"),
or the `consumes` declaration in `graphvein-project.yaml` it used, plus,
for a link to another member's Next.js route, the row assumptions it
lists in `assumes` (NX-V1, NX-V3; TOP-1 of 2026-11-18). Its label is
therefore:

- **Scope:** `best_effort`, row D, reason `default_branch`, family
  `cross_repository_links`, on every run (an eligible built-in run
  included). `precision.ScopeLabel` answers this before any row is
  consulted, and `TestPrecisionRules` refuses a row that names the
  family: making one precise is a change of this rule and of ADR 0075,
  not a new row.
- **Fact:** `provenance: inferred` with the confidence of the path match
  tier (0.7 for an exact template match, as inside one repository), and
  the link's `assumption`, which every view shows next to the link
  (Callers in other repositories, a call's project links, the project
  impact path labels, `graphvein callers|impact --project`).
- **Where it is served:** `links_precision` on the `project` part of
  `/api/v0/api/endpoint`, `/api/v0/api/flow` and each call of
  `/api/v0/api/calls`, and the family's count in `/api/v0/precision`.
  A member's own file labels and family labels are unchanged by project
  membership.

`TestCrossRepositoryLinksNeverPrecise` holds the scope label on every
kind of run.

## Not in the table

Java, C# and Dart (the T021 tier is best-effort by decision; the test
rejects any row that names them), HTML, SQL outside sqlx sets, and every
language
without a structural extractor. Routes of Express, FastAPI and Axum, client
calls, code calls (the `calls` edges between functions and methods,
family `code_calls`), Angular, Tauri and OpenAPI have no claim contract
and stay `best_effort`
until one earns a row through its own task. A file label never covers the
calls in the file: every view that shows code `calls` edges next to a file
label (the Flow tab, a code peek of a function or method, a change item of
a `calls` edge) carries the `code_calls` label from the server (T020 O-S1-5
(a)), and `/api/v0/precision` counts them as the row D family
`code_calls` (ADR 0073).

Go HTTP routes (PL-F10, task T023, developer decision GO4) are labelled
by the route's `framework`: the server's endpoint label names the
endpoints' common framework (a transport whose routes name several, such
as Express and Go routes in one repository, is `marker_conflict`), a
change item of a route or of its `exposes_route` edge names the route's
framework, and `/api/v0/precision` counts Go routes under PL-F10. The
Precise scope is what the oracles compare: route existence, `method`,
`pattern`, `host`, `template`, `trailing_slash`, `resolution` and the
GR-V assumptions (review of GR9). The facts no oracle compares
are outside it: a PL-F10 label carries `unverified: ["handler",
"middleware", "conditional", "scope"]`, which the views show as an
`unverified` tag beside the chip and beside each of those facts; in a
comparison, those fields of a route item and every `exposes_route` item
are Best effort with reason `not_compared`. The `calls_route` links of
client calls stay best effort.

## Changing the table

A row is added only when every piece of its evidence exists. Any change to
the table changes its digest; `TestPrecisionRules` then requires a new
`index.Version` and a new pin, so a label flip shows in change impact with
cause `extractor_or_relink`. The file label is computed by the scanner on
every run and never cached, and scope labels are not stored, so a flip
invalidates no cache and expires no disposition.

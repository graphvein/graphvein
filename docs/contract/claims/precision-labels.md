# Claim contract: precision labels

The label rule table of the precision labels (ADR 0066, `docs/extract/precision.md`) as
recorded when the feature was designed (task T020, internal record), and the table of
planned rows. `internal/precision` keeps its rule table 1:1 with these rows
(`TestPrecisionRules`).

## Label rule table

One row per scope that earns `precise` today, from inventory §2. Rows are
**family scopes**: no language's structural scope earns `precise` today
(no claim contract, inventory §2 F1-F8), so **every file label stays
`best_effort` at the end of implementation steps B1-B3**, and no golden changes in
those units. Rows PL-L1..PL-L4 below are added by the structural units
S1-S4 when, and only when, their evidence exists.

Marker: how the server recognises a fact of the scope from stored data.

| row | scope | marker (what is labelled) | P1 contract test | P2 goldens | P3 corpus | P4 independent review | P5 oracle | refusal (falls to D) |
|---|---|---|---|---|---|---|---|---|
| PL-F1 | SQL migration schema, sqlx sets, postgres dialect | `migration`, `table`, `column`, `db_index` nodes with `source` migration and set dialect `postgres`; their `contains`, `alters`, `foreign_key` edges | `internal/extract/schema`: `TestContractRows`, `TestContractDocument` | `sql-migrations` | ADR 0051 review notes (ten corpora; axum, axum-ddd) | ADR 0047 review notes (nine fresh reviews), ADR 0051 review notes | `TestOracleFixtures`, `TestOracleRealHistories` (Postgres 17) | set with another or no dialect (`schema_unsupported_dialect`) |
| PL-F2 | SQLModel / SQLAlchemy models | model `table` / `column` nodes, `tool` `sqlmodel` | `internal/extract/models/sqlmodel`: `TestModelClaimContract` | `sqlmodel-models`, `sqlmodel-link` | ADR 0054 review notes (fastapi, langflow) | ADR 0054 review notes | `TestModelOracle` | none beyond the global ones |
| PL-F3 | Prisma models | `tool` `prisma` | `prisma`: `TestPrismaContract` | `prisma-models` | ADR 0057 review notes | ADR 0057 review notes ("Independent review") | `TestPrismaOracle` | as PL-F2 |
| PL-F4 | Drizzle models | `tool` `drizzle` | `drizzle`: `TestDrizzleContract` | `drizzle-models` | ADR 0059 review notes | ADR 0059 review notes | `TestDrizzleOracle`, `TestDrizzleOracleForms` | as PL-F2 |
| PL-F5 | Sequelize models | `tool` `sequelize` | `sequelize`: `TestSequelizeContract` | `sequelize-models` | ADR 0061 review notes (opencollective-api, Gladys) | ADR 0061 review notes (Revision 3) | `TestSequelizeOracle` | as PL-F2 |
| PL-F6 | Diesel models | `tool` `diesel` | `diesel`: `TestDieselContract` | `diesel-models` | ADR 0062 review notes | ADR 0062 review notes (DR) | `TestDieselOracle` | as PL-F2 |
| PL-F7 | GORM models | `tool` `gorm` | `gorm`: `TestGormContract` | `gorm-models` | ADR 0063 review notes (gotify, realworld-gin) | ADR 0063 review notes (GR) | `TestGormOracle` | as PL-F2 |
| PL-F8 | TypeORM models | `tool` `typeorm` | `typeorm`: `TestTypeORMContract` | `typeorm-models` | ADR 0064 review notes (seerr, nest-admin) | ADR 0064 review notes (TR) | `TestTypeORMOracle` | as PL-F2 |
| PL-F9 | Code-to-table links | `reads_table` / `writes_table` edges and the owner's `table_access` rows | `internal/extract/tableaccess`: `TestTableAccessContract` | `table-access-sqlmodel`, `-prisma`, `-drizzle`, `-sqlx` | ADR 0060 review notes (axum, axum-ddd, langflow, kaneo, umami) | ADR 0060 review notes (Revision 5) | `TestSQLTablesOracle`, `TestSQLAlchemyOracle` | owner sites in `table_access_unknown` stay unknown (not labelled) |
| PL-L1 | Go structure | Go files (test files included): file records and nodes, top-level declarations, `contains` and `imports` edges (`docs/lang/go-structure.md`); calls excluded (`code_calls` stays D) | `internal/lang/golang`: `TestGoStructureContract` | `go-structure`, `go` | ADR 0068 review notes (fixture, gotify, realworld-gin, self-index) | ADR 0068 review notes (S1R verdict and the S1F fixes) | `TestGoStructureOracle` (go/build `ImportDir`), `TestGoStructureGoList` (opt-in `go list`) | a Go file whose structure claims nothing (GS-F2, GS-F3, not parsed or extracted, dropped by validation): stored `best_effort`, reason `structure_refused` |
| D | everything else | every file label of a language no PL-L row names (all languages but go, including java, c_sharp, dart, html, sql, unknown); routes, client calls, code calls, cross-boundary links, Angular, Tauri, OpenAPI, test scope, any external-indexer run | - | - | - | - | - | - (this is the default) |

Mapped dialects inside PL-F3..PL-F8 (mysql, mariadb, mssql) are covered
by the row: their contracts list them and the per-fact `verification:
mapped` already says "unverified" (open point O13).

Planned rows (not in the table until the structure contracts add them with their
evidence; each is a file-label row):

| row | scope | earns through | oracle |
|---|---|---|---|
| PL-L1 | Go structure | the Go structure contract, ADR 0068 (added, in the table above) | go/parser, go/ast and go/build `ImportDir` from the standard library, plus an opt-in `go list` differential, in test files only |
| PL-L2 | Python structure | the Python structure contract, ADR 0069 | Python `ast` / `symtable` run with `python3 -I` in `<scratch>` (cheap) |
| PL-L3 | TypeScript, TSX, JavaScript structure | the TypeScript structure contract, ADR 0070 | the TypeScript compiler API from a pinned `typescript` package in `<scratch>` (ADR 0014 used it once) |
| PL-L4 | Rust structure | the Rust structure contract, ADR 0071 | none cheap (rust-analyzer is not); P5 states the reason |

Not planned for v1: java, c_sharp, dart (T021 is best-effort by
decision), html (Angular templates are a family, F16), sql outside sqlx
sets, and every language without a structural extractor.

A test (`TestPrecisionRules`) keeps the Go table, this table and
`docs/extract/precision.md` 1:1 (same row IDs, scopes and evidence
names), checks P1-P6 for every row, and checks that no row names java,
c_sharp or dart (T021 guard).

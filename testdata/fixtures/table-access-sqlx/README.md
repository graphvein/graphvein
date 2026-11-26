# table-access-sqlx fixture (T018h, ADR 0060)

A Cargo workspace of sqlx packages for code-to-table linking
(`docs/extract/table-access.md`). Golden:
`testdata/fixtures/table-access-sqlx.golden.json` (the whole indexed
graph). The Rust code is never compiled; the migrations apply to Postgres
17 as written, and `scripts/oracle/sql_tables.sh` compares every claimed
SQL site with the Postgres analyzer.

No live ignore files and no ignored directory names are used, so there is
no inert-name mapping.

| package | cases |
|---|---|
| `app` | `src/main.rs`: CT-S1 macros on `sqlx::` paths (`query!`, `query_as!`, `query_scalar!`, the three `_unchecked` forms), every CT-Q1 form (FROM, JOIN, comma joins, aliases, INSERT columns, ON CONFLICT DO UPDATE with EXCLUDED and DO NOTHING, INSERT ... SELECT, UPDATE ... FROM, UPDATE ... FROM itself, DELETE FROM ONLY ... USING, RETURNING, the four lock clauses, a CTE shadowing a name, a writable CTE, subqueries in FROM and expressions, `unnest(...)` in FROM), CT-Q2 forms (folded, quoted, qualified `audit.events`, shadowed), refusals (a view, a renamed table's old name, a view next to a table: partial, `truncate`), `select now()` (nothing), a closure owner. `src/repo.rs`: macros imported by `use sqlx::{query, query_as}`, the five CT-S2 functions (inferred), `sql_not_literal`, several statements, role `builds` (a statement returned, a statement bound to a local), `statement_not_traced` executions (a statement from a call, a QueryBuilder, the local), a nested module that does not see the `use` (no site). `local_fn` (T020): a site inside a local function is owned by the enclosing function (the local function has no node, row RS-D10 of `docs/lang/rust-structure.md`); an execution of the enclosing function around the local function's item does not make it `executes` (role `builds`), and the statement a local function returns stays `statement_not_traced`. `keyword_user` (R5-B1): `FROM user` is Postgres's `current_user` function (no claim, no record) next to `FROM "user"`, the table `1_init.sql` creates. `tests/it.rs`: a test-scope owner. |
| `nomig` | a sqlx package without a migration set: `no_schema_source` |
| `lite` | a SQLite set (only migration records): `no_schema_source` |
| `pathpkg` | CREATE SCHEMA and SET search_path: unqualified tables carrying a search_path reason are not resolved; a table the path moved to `app` is claimed only as `app.moved` |

# sql-migrations fixture (T018a, ADR 0047, Revisions 1 and 2)

A Cargo workspace of sqlx migration sets. Golden:
`testdata/fixtures/sql-migrations.golden.json` (schema records only).

No live ignore files and no ignored directory names are used, so there is
no inert-name mapping. Two inputs cannot be committed portably and are
created by the tests in their temporary copy:

- `pg-unreadable/migrations/2_unreadable.sql`, a file that is not valid
  UTF-8 (golden run, index and store tests, and `internal/extract/schema`);
- `pg-all-unreadable/migrations/1_only.sql`, the same content, so that
  every member of that set is unreadable (golden run, index, store and
  server tests, and `internal/extract/schema`, which also excludes it with
  a `.graphveinignore` it writes; no ignore file is committed);
- `pg-unreadable/migrations/4_linked.sql`, a symlink, only in
  `internal/extract/schema` and only where the platform allows symlinks.

`TestSchemaCRLF` also converts `pg-app/migrations/3_alter.sql` to CRLF in a
copy and expects identical records.

| package | cases |
|---|---|
| `pg-app` | every modeled statement and column constraint, lowercase and uppercase keywords, quoted mixed-case and 70-byte unquoted names, nested comments, `E''`, a dollar-quoted body with `;`, unnamed and duplicate unnamed indexes, composite keys, `serial`, REFERENCES without columns, ADD/DROP/RENAME COLUMN, ALTER TYPE, SET/DROP NOT NULL and DEFAULT, RENAME TO, DROP TABLE and recreate, DROP INDEX, an unnamed index with a generated name (5), opaque SELECT and DO, unsupported CREATE TYPE and VIEW, an unparseable ALTER and a `public.`-qualified name (8), an unterminated dollar quote (9), an `.up.sql`/`.down.sql` pair, a README.md and a subdirectory (ignored) |
| `pg-workspace-dep` | `sqlx = { workspace = true }` with the multi-line inline table in the root `Cargo.toml` |
| `pg-serial` | R1 serial aliases, ADD COLUMN serial, no-effect DML |
| `pg-search-path` | R2 search_path changes |
| `pg-opaque-exists` | R3 IF NOT EXISTS after an opaque statement |
| `pg-set-opacity` | R4 extension, DML in a set with functions, set function call |
| `pg-unreadable` | R5 (members added by the tests, above) |
| `pg-all-unreadable` | T018b1: every member unreadable (added by the tests), so the set has no migration node, only `schema_member_unreadable` on its manifest; `migrations/README.md` keeps the directory in Git |
| `bad-manifest` | T018b1: a `Cargo.toml` with a missing value gives `schema_manifest_unreadable`; its `migrations/1_init.sql` gets no record |
| `pg-select-into` | T018b1: SELECT INTO inside parentheses, before UNION and after WITH (created by Postgres: unsupported, a relation, CREATE TABLE IF NOT EXISTS skipped); INSERT INTO and MERGE INTO in a CTE, `AS into`, `y.into`, a string, a quoted identifier and a comment (no INTO clause); INTO on a later SELECT and in a subquery (refused: migration 2 may fail, 2 and 3 `applied: null`). 1 applies to Postgres 17 as written, 2 is refused |
| `sqlite-app` | migration records only, unsupported-dialect diagnostic |
| `bad-version`, `dup-version` | set-level order diagnostics, no schema objects |
| `no-sqlx` | `migrations/` without a sqlx dependency: no set |
| `with-config` | `sqlx.toml`: diagnostic, tables partial |
| `pg-rev2-columns` | Revision 2 WF1 (a column named `exclude` in CREATE TABLE and ADD, an EXCLUDE USING constraint), G3 (`unique` from primary key, UNIQUE, unique index; not from partial, expression or multi-column indexes), WF7 (DROP COLUMN CASCADE drops a generated column, its index and a CHECK), G5 (ADD COLUMN DEFAULT of a set function counts as opaque), WF5 (`DROP INDEX IF EXISTS public.x` of an unqualified table's index, with IF EXISTS since Revision 8 so that the migration stays applied; `ALTER INDEX <table> RENAME`; `ALTER INDEX <index> RENAME`) |
| `pg-rev2-relations` | WF3 (CREATE TABLE IF NOT EXISTS over a view, sequence, index, composite type, materialized view, SELECT INTO target and CREATE SCHEMA element: no table; over an enum: refused), WF8 for a refused statement (partial tables, `applied: null` for 2 and 3, a dropped table kept) |
| `pg-rev2-shadow` | WF9 (`pg_temp.` table: no node), WF4 (DROP TABLE under a same-name TEMP table; DROP TABLE, DROP COLUMN and RENAME after a search_path change) |
| `pg-rev2-schema` | WF6 (ALTER SCHEMA RENAME) |
| `pg-rev2-types` | G1 (DROP TYPE CASCADE), WF7 without CASCADE (refused, the migration may fail) |
| `pg-rev2-roles` | G2 (ALTER ROLE SET search_path), WF2 (DROP OWNED) |
| `pg-drops` | T018a Revision 7: DROP CONSTRAINT IF EXISTS (2, applied; every key of `notes` unknown), DROP CONSTRAINT of a written name on a table a foreign key references (3; T018a2 C3, row M5g-1: the name is exact on the complete `accounts`, so only its UNIQUE key on `email` goes, the primary key and the reference from `projects` stay and the migration is applied) and of a generated name (4, may fail; Revision 7). Kept out of `pg-app` so that `pg-app`'s later migrations keep their facts and its Postgres catalog check still compares keys |

In `pg-app`, `8_unreadable_alter.sql` (a statement that cannot be read) and
`9_unterminated.sql` (a lexing failure) make the migration one that may
fail (Revision 2 WF8): `tail` and `reversible` are partial and migrations
8, 9 and 10 have `applied: null`. The pg-rev2 and pg-drops packages are not applied to
Postgres by the CI check; some of them hold statements Postgres refuses on
purpose.

`TestSchemaPostgresSemantics` (CI postgres job only) applies `pg-app` in
sqlx order except `8_unreadable_alter.sql`, `9_unterminated.sql` and the
down migration, and compares the catalog with the extracted records.

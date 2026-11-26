# prisma-models fixture (T018g1)

A workspace of small packages that exercise the Prisma claim contract
(`docs/extract/schema-prisma.md`). The root `package-lock.json` pins
`prisma` 7.10.0 for every package (PR-V1, a workspace-root lockfile)
except `v5` (5.22.0), `v6` and `split/legacy` (6.19.3) and `twin/app`
(7.9.1), pinned by nested `<package>/node_modules/prisma` entries. `TestPrismaIndexedFixture`
indexes this directory and checks the golden
`testdata/fixtures/prisma-models.golden.json`; `TestPrismaFixture` runs
the claim pass over it for the contract test and `TestPrismaFixtureFacts`
spells out the decided facts.

| package | what it covers |
|---|---|
| `pg` | postgresql (oracle; also run with 5.22.0 and 6.19.3): every scalar type (PR-F1), modifiers (PR-F2), Unsupported (PR-F3), enums with `@map`/`@@map` (PR-F4, PR-E1), every listed native type and refused ones (PR-A6), every default form and the unknown ones (a string default on `@db.VarChar(n)` and `@db.Char(n)`), negative and non-int4 integers (presence only) (PR-A3, PR-A4), `@id`/`@unique` with `map:` (PR-A1, PR-A2), `@map`/`@@map` (PR-A5, PR-AA4, a non-literal of each), `@ignore`/`@@ignore` (PR-A7, PR-B3), composite keys and every index type (PR-AA1-AA3), unread field and block attributes, a partial index and partial unique indexes (D), relations with explicit and default actions, `map:`, a composite key and a one-to-one (PR-R1), back-relations (PR-R2), a relation naming an unknown field (D), implicit many-to-many with a generated name, an explicit name with an `@@map`'d model and a self-relation (PR-R3), models Prisma rejects (two `@id`, `@id` with `@@id`, a many-to-many side without a single id: PR-P1), the database's default text (PR-A3), a view and a composite type (PR-B4) |
| `multi` | a config naming a directory: a multi-file schema; a literal `migrations.path` (the `schema_model_migrations` marker); `schemas` with `@@schema` (PR-AA5), an unlisted schema and a model without `@@schema` (PR-P1) |
| `emulated` | `relationMode = "prisma"`: `emulated_relations`, no foreign keys, a join table without foreign keys |
| `dupe`, `noview` | schema-level PR-P1: a block name defined twice, a view without the `views` preview feature |
| `v5` | Prisma 5.22.0: `package.json` `prisma.schema`, `multiSchema` as a preview feature, a join table whose key is unknown, `ulid()` before 6.2.0 |
| `v6` | Prisma 6.19.3: a config naming a folder, `multiSchema` GA, `ulid()` as a client default, a join table with its primary key |
| `mysql`, `sqlserver`, `cockroach` | mapped providers (PR-V6): `@unique`/`@@unique` as unique indexes (mysql, cockroachdb) or UNIQUE constraints (sqlserver) and the join table key the same way, connector types and defaults (autoincrement() on MySQL and SQL Server: unknown), MySQL's inline enum, a scalar list on MySQL (PR-P1), native types refused, foreign keys with the connector's default action (SQL Server NoAction) and MySQL's possible index |
| `sqlite` | sqlite (oracle): every scalar type, an enum and Unsupported, every default form with the database's text, the Int id without a default claim (INTEGER PRIMARY KEY AUTOINCREMENT), autoincrement() and a Json literal default unknown, keys, unique and other indexes, foreign keys with default and explicit actions, a join table with its unique index |
| `twin`, `twin/app` | one schema discovered by two packages whose versions (7.10.0, 7.9.1) agree on every claim gate: claimed once, pins null |
| `split`, `split/legacy` | one schema discovered by Prisma 7.10.0 and 6.19.3 packages: nothing claimed, a warning naming both |
| `urlv7` | a datasource `url` with Prisma 7: the schema is rejected (PR-P1) |
| `mongo` | provider mongodb: no tables, a marker |
| `dynamic` | a non-literal provider and relation mode; a `@unique` whose kind is then unknown |
| `nodep` | a `.prisma` file in a package without the dependency: an unread marker |
| `nonliteral` | a config whose `schema` is computed: not proven |

No file here is an ignore file or a path the repository ignores, so no
name mapping is needed.

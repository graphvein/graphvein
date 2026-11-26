# drizzle-models fixture

Own code (no third-party text) for T018g2. Part 1 covers the
tables and columns rows of the claim contract
(`docs/extract/schema-drizzle.md`); part 2 adds foreign keys,
the extra config, relations and the remaining DR-P1 failures.
`link.golden.json` is the claim pass's output (records and diagnostics,
`TestDrizzleFixture`); the test skips it and this README when reading the
sources. `../drizzle-models.golden.json` is the fixture indexed through
the indexer (`TestDrizzleIndexedFixture`). No file needs an inert name.
The oracle runs on it (`scripts/oracle/drizzle_catalog.sh
--fixture`, `TestDrizzleOracle`); the forms the harness drops from its
copy are listed in that script and in `docs/extract/schema-drizzle.md`
"Oracle".

| path | what it covers |
|---|---|
| `pnpm-lock.yaml` | workspace lockfile with pnpm's environment document first (shared reader); pins 0.45.4 / 0.31.11, `v1` 1.0.0-rc.1 |
| `pg/` | default config name, array `schema` with a glob, literal `schemaFilter`; `db/schema.ts`: every postgresql builder of the run list, `.array()`, serial kinds, identity, generated columns, each default kind (literals, json, arrays, `sql`, an `sql` const, `defaultNow`, `defaultRandom`, `null`), client-side modifiers, `$type`, enums (same file, imported from `db/enums/mood.ts`, `pgSchema` enum), `customType` and a spread object from `lib/shared.ts` (outside the schema), a builder outside the list (`cidr`), unknown defaults and modifiers, modifiers before `.array()`, `.references()`, a table with extra config, a `pgSchema` table, a second export name, a non-exported table, DR-P1 failures (non-exported enum and `pgSchema`, duplicate table, column and unique names), D forms (non-literal name, columns callback, `.enableRLS()`, `let`, another dialect's table, a partial columns object); `old-schema.ts`: a stale file outside the schema |
| `pg/db/keys.ts` | (part 2) inline `.references()` (another file, actions, self, an unclaimed target, a name truncated to 63 bytes); extra config as an array and as the deprecated object: composite `primaryKey`, `unique().on()` with `.nullsNotDistinct()`, `foreignKey` (one and two columns, named), `index`/`uniqueIndex` with `.on`, `.using`, `.desc()`, `.nullsFirst()`, `.op()`, `.where(sql)`, `.concurrently()`, `.with()`, an expression column, a check; D elements (positional `primaryKey`, a block body); DR-P1 (a missing extra-config column, a missing referenced column, a unique name used twice); `relations()` through a module-level alias, `one`/`many`, `h.one`, `() => ({})`, an unclaimed target, an unread entry |
| `pg/db/keys.ts` `audits`, `pg/db/enums/mood.ts` `logLevel`, `pg/lib/shared.ts` `plainText`/`shortText` | (G2B5) forms the corpora use: `restrict` and `set null` delete actions, a `cascade` update, a partial non-unique index, a plain unique index; a mixed-case enum (quoted type); a `text` customType (claimed) and a `varchar(255)` one (type unknown) |
| `pg/db/review/` (a directory schema pattern), `pg/lib/core.ts` | (later revision) `reexported.ts`: builders through a local re-export, no model payload (B1 marker); `creator.ts`: a non-exported `pgTableCreator` (marker names its tables); `refs.ts`: a foreign key to a column that is neither key nor unique (DR-P1) and one to a column unique only through a unique index (unknown); `pg/db/schema.ts` `appUsers.role`: an enum default in a non-public schema (harness C1) |
| `pg/db/relations.ts` | (part 2) `relations()` outside the configured schema (counted in a marker) |
| `pgschema/` | a `pgSchema` table without `schemaFilter` (not claimed: push keeps only `public`) |
| `cased/`, `camel/` | `casing` `snake_case` and `camelCase`: omitted and literal names, a generated unique name (A12); (part 2) generated primary key, foreign key and index names under casing |
| `sqlite/` | config named by `--config` in the scripts, path against the package directory; SQLite types, unique index (A10), defaults, integer primary key; a unique index name used by two tables (DR-P1); (part 2) extra config: unnamed composite key, partial index, unique index, check, unnamed foreign key, one-column `primaryKey()` |
| `mysql/` | mapped MySQL types, enum, serial, default presence; a `mysqlSchema` table (A9); (part 2) composite key, index, unique, foreign key; (later revision) `counters`: `.autoincrement()`, `timestamp().defaultNow()`, `.onUpdateNow()`, `char` without a length; types in MySQL's reported form |
| `noconfig/` | a Drizzle package without a config (marker) |
| `v1/` | a drizzle-orm 1.0 release candidate (refused) |

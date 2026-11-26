# table-access-drizzle

Fixture for T018h rows CT-Z1..CT-Z4 (Drizzle select, write and relational
query builders, and `sql` templates run by `execute`) and the Drizzle
execution records of row D. The root package pins `drizzle-orm` 0.45.4 and
`drizzle-kit` 0.31.11 (`package-lock.json`); `drizzle.config.ts` is its
group (schema `src/db/schema.ts`, `schemaFilter` public and audit).

- `src/db/schema.ts`: `users`, `posts`, `comments`, `tags`, `post_tags`,
  the `pgSchema` table `audit.log`, a view, `relations()` with a target
  outside the schema (`src/db/extra.ts`). A schema module is not a site
  (W6), so the view's definition claims nothing.
- `src/db/tables.ts` (`export *` barrel), `src/db/aliases.ts` (a
  module-level alias const and a const object of tables),
  `src/db/client.ts` (the database).
- `src/select.ts`: CT-Z1, every builder and join, `$count`, each Targets
  form, a lateral join on a subquery (partial), a subquery in `inArray`
  (its own site, executed by the outer one), the refusals (`$with`, a
  view, a parameter), a module-level site, calls that are no site.
- `src/write.ts`: CT-Z2, every chain link, upsert, `update ... from`, a
  join on an update chain (`site_partial`), `insert ... select`, a
  transaction callback, a transaction holding no site (D), a parameter
  target, calls that are no site.
- `src/query.ts`: CT-Z3 with nested `with`, and its partial and unknown
  forms.
- `src/raw.ts`: CT-Z4 (table, column and value substitutions, raw names,
  a `pgSchema` table, a statement with no table), `sql_dynamic`, an `sql`
  value bound to a local (D), the reader's refusals.
- `src/__tests__/select.spec.ts`: a test-scope site.
- `other/`: a package without a Drizzle group: its table-const site is
  claimed (W7, Revision 4), its relational query is unknown.

`scripts/oracle/sql_tables.sh --root` on this directory compares every
claimed CT-Z1/CT-Z2 site's `toSQL()` SQL and every claimed CT-Z4 site's
SQL with the Postgres analyzer. No ignore files and no ignored paths:
nothing needs an inert name.

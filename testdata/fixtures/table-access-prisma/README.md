# table-access-prisma

Fixture for T018h rows CT-R1..CT-R3 (Prisma delegate calls, relation
fields and raw SQL) and the Prisma execution records of row D. The root
package pins `prisma` 7.10.0 (`package-lock.json`); `prisma/schema.prisma`
is its group (`User`, `Profile`, `Post`, `Comment` mapped to `comments`,
`Tag`, `Group` mapped to `user_groups`, the implicit many-to-many join
tables `_PostToTag` and `_GroupToUser`, and an `@@ignore`'d `Legacy`).

- `src/users.ts`: every CT-R1 operation through `this.prisma`, the
  imported client and a `tx` callback; a `$transaction` over queries built
  elsewhere (D); a dynamic delegate; calls that are no site; a
  module-level site.
- `src/relations.ts`: CT-R2 under `include`, `select`, every relation
  filter and every nested write; the refusals (`connect`, `disconnect`,
  `set`, a spread) and the partially read sites (`site_partial`: a join
  table, `orderBy` and `_count` over a relation, a to-one filter without
  `is`, an argument that is not an object literal); `fluent` (R5-C1): the
  fluent API (`findUnique(...).posts()`, a chained `.author().profile()`,
  an implicit many-to-many `.groups()` with its join table) is
  `site_partial`, `.then` is nothing.
- `src/raw.ts`: CT-R3 tagged and `Unsafe` forms, an escape sequence, a statement with no table, and the refusals
  (`Prisma.sql` fragments inline and through a local, non-literal
  arguments, a statement outside the subset, an unresolved name).
- `src/__tests__/users.spec.ts`: a test-scope site and a mock call.
- `other/`: a package without a Prisma group (W7): no site.

No ignore files and no ignored paths: nothing needs an inert name.

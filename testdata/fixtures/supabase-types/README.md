# supabase-types

Fixture for T024c3 rows SB-G1, SB-G2 and D (Supabase's generated
TypeScript types as a schema source labelled "generated types";
`docs/extract/schema-supabase-types.md`). Each directory is its own npm
package.

- `app/`: `lib/database.types.ts` in postgres-meta's current form
  (0.91 to 0.97): `export type Database = {` with `__InternalSupabase`,
  a computed field (`lists.todo_count`), an identity column (`id?: never`),
  an enum and a range column (`unknown`: nullability unknown), a view
  (`open_todos`, no record), the `DatabaseWithoutInternals` and
  `DefaultSchema` aliases, helpers and `Constants`. SB-G1: two tables.
- `legacy/`: `src/types.ts` in the 0.70 to 0.77 form
  (`export interface Database {`, no `isOneToOne`), reformatted with
  semicolons and single quotes as projects do. SB-G1: one table.
- `migrated/`: a Supabase migration set (`supabase/config.toml`,
  `supabase/migrations`) beside `lib/types.ts`. SB-G2: the set is the
  schema source; the types disagree with it on purpose (a table and a
  column only in the types, a table and a column only in the set, a
  nullability, a relationship to `auth.users`); the `storage` schema is a
  platform schema and is not compared.
- `handwritten/`: `database.ts` starts like generated types but has an
  optional `Row` property: `supabase_types_unread`.
- `kysely/`: `db.ts` declares a `Database` interface without the
  generator's header: D, nothing.

The fixture holds no ignore files and no ignored paths, so it needs no
inert names.

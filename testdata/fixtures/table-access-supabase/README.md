# table-access-supabase

Fixture for T024c2 rows SB-K1 and SB-T1..SB-T5 (Supabase JS clients and
their PostgREST chains) and the Supabase D records
(`docs/extract/table-access.md` "Supabase rows"). The root package declares
`@supabase/supabase-js`, `@supabase/ssr` and `@supabase/auth-helpers-nextjs`;
`supabase/config.toml` and `supabase/migrations` are its Supabase set
(T024c1): `profiles`, `products`, `prices`, `subscriptions`, `tags`,
`product_tags` (a junction), `messages` (two keys to `profiles`), the
`billing.invoices` table and a view `active_products` (its own migration,
so the tables stay complete).

- `src/lib/client.ts`: the clients: module consts bound to `createClient`
  (one with a literal `db.schema`, one with a non-literal one),
  `createBrowserClient`, an async wrapper of `createServerClient`, an
  arrow wrapper of the auth-helpers `createRouteHandlerClient`, a wrapper
  of a wrapper, and a bounded const table name.
- `src/db/reads.ts`: SB-T1 reads through every proof (imported consts, a
  path-alias import, a direct factory call, a function-scope const, an
  awaited wrapper, the auth-helpers wrapper), `db.schema`, SB-T2 with a
  literal and a non-literal schema, a view and a missing table
  (`sql_name_unresolved`).
- `src/db/embeds.ts`: SB-T3: one key, nested levels, an alias, a hint, two
  keys, a junction table, a spread, a view, aggregates (no embed), and a
  select argument that is not a literal (T024c3: partial, embedded tables
  not read); (T024) final `!inner` / `!left` join modifiers
  (resolved as without them) and a hint before `!inner` (partial).
- `src/db/builders.ts` (T024): a const-bound builder used with
  a verb in its function (one chain), a returned builder and `let`
  builders (`builder_unread` unknowns).
- `src/db/writes.ts`: insert, `insert(...).select()`, update, upsert,
  delete, and an embed after a write.
- `src/db/other.ts`: D (`rpc`, storage), a returned builder
  (`builder_unread`, T024), SB-T4,
  and SB-T5 receivers (a parameter, `let`, destructuring, a member, a
  client with a non-literal `db.schema`).
- `other/`: a package that declares supabase-js without a Supabase set
  (`no_schema_source`).
- `typed/`: a package without a Supabase set whose
  `src/database.types.ts` is Supabase's generated types (T024c3, SB-G1):
  reads and writes resolve against its tables (`assumes: SB-V6`), an
  embed stays partial (relationships not resolved), a missing name is
  `sql_name_unresolved`.
- `plain/`: a package that declares no Supabase package: a knex
  `from(...).select()` is invisible.

The fixture holds no ignore files and no ignored paths, so it needs no
inert names.

# supabase-migrations (T024c1, T024c1b, T024c1c, NX-F2)

Supabase migration sets for rows SB-M1 to SB-M10 of
`docs/contract/claims/nextjs-supabase.md` (ADR 0077, `docs/extract/schema.md`).
No inert names: the fixture holds no ignore file and no ignored path.

- `app/supabase/config.toml` makes `app/supabase/migrations` a set (SB-M1).
  `20200101000000_init.sql` is the first entry and a legacy init file the
  Supabase CLI skips; `seed_data.sql` does not match the CLI's name pattern
  and is ignored. The three other files are applied in lexical order.
- References to `auth.users`, `storage.buckets` and `storage.objects`
  resolve to platform objects (SB-M2, assumption SB-V2).
- `ENABLE`, `DISABLE`, `FORCE` and `NO FORCE ROW LEVEL SECURITY`, `CREATE
  POLICY` (with and without `FOR`, `AS`, `TO`), `ALTER POLICY ... RENAME
  TO`, another `ALTER POLICY`, `DROP POLICY [IF EXISTS]`, a policy on
  `storage.objects` (a table the set does not hold) and one on
  `public.audit_log` beside the unqualified `audit_log` (its policies are
  unknown) cover SB-M3.
- `app/supabase/migrations/20240104000000_functions.sql` (T024c1b):
  PL/pgSQL `SELECT ... INTO` parameters and declared variables, a function
  call in `FROM`, a variable named `content`, a routine's `SET search_path`
  and a trigger naming `public.touch_profile` for the set's unqualified
  function; every statement applies (oracle-checked). Its tail (R1) inserts
  into `storage.buckets` (SB-M7, assumption SB-V5): no trigger of the set
  fires on it, so the `CREATE TABLE IF NOT EXISTS bucket_notes` after it,
  with a `char_length` CHECK and an index named like the generated shape,
  is complete.
- `app/supabase/migrations/20240106000000_sql_functions.sql` (T024c1c):
  three LANGUAGE sql functions whose bodies PostgreSQL parse-analyses at
  creation, proven by R1b.P5's analysed form: chatbot-ui's EXISTS over a
  table with a parameter, `(storage.foldername(p_name))[2]` and
  `storage.filename(p_name)` (SB-M8, assumption SB-V2), an alias-qualified
  column, and a scalar column of a table; the migration applies and
  `attachments` is complete (oracle-checked).
- `no-config/supabase/migrations` has no `config.toml`, and every entry
  follows the CLI's name pattern: a Supabase set by SB-M5 (assumption
  SB-V3, package the directory). Its second file creates `vector` and
  `http`, the platform extensions of SB-M4 (assumption SB-V4), into the
  `extensions` schema, whose known effect (SB-M6) leaves the `CREATE TABLE
  IF NOT EXISTS` after them complete.
  `20240102500000_vector_columns.sql` (NX-F2) uses their types as column
  types: `vector(3)` unqualified through the platform's search_path,
  `extensions.halfvec(16000)` and `http_header[]` (SB-M10, assumptions
  SB-V2 and SB-V4; checked live with pgvector 0.8.7 and pgsql-http 1.7 by
  `TestSupabaseNXF2CasesPostgres`).
- `rls-data/supabase` (NX-F2) is a project of its own: its set defines a
  trigger function and a trigger, so the `UPDATE` at its end is opaque
  (O1), but every routine of the set is read and none runs a statement,
  `EXECUTE` or setting, so it cannot change row level security: both
  tables' `rls` flags stay known (SB-M9). A separate project keeps the
  opaque statement from making `app`'s tables incomplete for the oracle.
- `rls-catalog/supabase` (T024) is a project of its own: after
  a trigger, a superuser's `UPDATE pg_catalog.pg_class` and `DELETE FROM
  pg_catalog.pg_policy` change row level security with no statement the
  pass reads, so `notes`' `rls` flags and `policies` are unknown (SB-M9
  applies only to data statements on tables the set holds). It is not a
  `TestSupabaseOracle` root (that replay runs without superuser rights,
  as the platform, and Postgres refuses the migration with 42501, which
  the pass marks may-fail); `TestSupabaseB5FixturePostgres` replays it
  both ways.
- `rls-rule/supabase` (stacked gate NX-S) is a project of its own: a rule
  on `events` whose actions write `pg_catalog.pg_class` and
  `pg_catalog.pg_policy` for `notes`, which the set creates after the
  rule; the `INSERT` into `events` at the end fires it. SB-M9 does not
  read rule actions, so `notes`' `rls` flags and `policies` are unknown.
  Not a `TestSupabaseOracle` root (a role without superuser rights is
  refused with 42501, as in `rls-catalog`); `TestSupabaseRuleFixturePostgres`
  replays it both ways.
- `rls-dynamic/supabase` (2026-11-18) is a project of its own: a
  `DO` block's dynamic `EXECUTE` creates the `rls-rule` rule, which the
  pass cannot read, and the set defines no routine. Code the pass cannot
  read ends SB-M9 for the set, so `notes`' `rls` flags and `policies` are
  unknown. Not a `TestSupabaseOracle` root (refused with 42501 without
  superuser rights); `TestSupabaseDynamicFixturePostgres` replays it both
  ways.
- `not-cli/supabase/migrations` has no `config.toml` and holds
  `notes.txt`, which the CLI does not apply: no set (SB-M5 refusal).

`TestSupabaseOracle` replays `app` and `rls-data` on PostgreSQL 17 with the Supabase
prelude and finds no disagreement (tables, columns, keys, the `rls` flags,
the policies and the platform foreign keys). `no-config` is not replayed:
a plain PostgreSQL lets no non-superuser create its two extensions, which
assumption SB-V4 covers.

# Next.js and Supabase census and oracles (the T024 investigation)

Scratch-only tools behind the T024 task record (internal). Nothing here runs
in the default tests, and none of it builds, installs or runs corpus code.

- `census.cjs`: reads one corpus checkout with the TypeScript 6.0.3
  compiler API (install it as in `scripts/oracle/ts_structure/README.md`
  and point `TS_DIR` at that directory) and the `next` package's pure path
  helpers (`dist/shared/lib`, extracted from the npm tarball of the
  corpus's locked `next` version). Writes one JSON line per route handler,
  Pages Router API file, server action, action use, client call (from a
  `graphvein index --emit` file) and Supabase call site.

  ```bash
  node census.cjs <name> <checkout> <next>/package/dist <name>.ndjson <tables.json|-> > <name>.jsonl
  ```

  `next@15` and later also need `dist/lib/constants.js`,
  `dist/server/lib/interception-routes.js` and
  `dist/shared/lib/invariant-error.js` from the same tarball.
- `replay-supabase.sh` with `supabase-prelude.sql`: replays a checkout's
  `supabase/migrations` into the throwaway PostgreSQL 17 cluster of
  `scripts/cloud-postgres.sh` (cloud sessions only) over a stub of the
  objects the Supabase platform provides, and prints the tables, views and
  functions as JSON (the `<tables.json>` argument of `census.cjs`).
  `supabase-prelude.sql` is also the prelude of `TestSupabaseOracle`
  (`internal/extract/schema/oracle_supabase_test.go`, T024c1), which runs
  with `GRAPHVEIN_TEST_POSTGRES_URL` on the fixture and on the checkouts in
  `GRAPHVEIN_SUPABASE_CORPORA` (a path list):

  ```bash
  C=spike/parsing/.corpus
  GRAPHVEIN_TEST_POSTGRES_URL=$(bash scripts/cloud-postgres.sh url) \
    GRAPHVEIN_SUPABASE_CORPORA=$C/chatbot-ui:$C/supabase-stripe \
    go test ./internal/extract/schema -run TestSupabaseOracle -v
  ```
- `next_paths.cjs`: the route template oracle of `TestNextRoutesOracle`
  (`internal/extract/routes/nextjs_oracle_test.go`, opt-in). Extract the
  `next` package (MIT) from the npm registry into scratch, read it, and
  point the test at its `dist`:

  ```bash
  mkdir -p "$SCRATCH/next" && cd "$SCRATCH/next" && npm pack next@16.2.10 && tar xzf next-16.2.10.tgz
  GRAPHVEIN_NEXT_ORACLE_DIST="$SCRATCH/next/package/dist" \
  GRAPHVEIN_NEXT_ORACLE_CORPORA="<checkout>:<checkout>" \
    go test ./internal/extract/routes -run TestNextRoutesOracle -v
  ```
- `next_rewrites.cjs`: the rewrite oracle of `TestNextRewritesOracle`
  (same file, same opt-in, T024a2). It runs the package's own
  `getPathMatch` and `prepareDestination` (`dist/shared/lib/router/utils`;
  `next/dist/...` requires resolve to the extracted package) on the
  test's literal source, destination and path cases:

  ```bash
  GRAPHVEIN_NEXT_ORACLE_DIST="$SCRATCH/next/package/dist" \
    go test ./internal/extract/routes -run TestNextRewritesOracle -v
  ```

  Checked with `next` 14.1.0 and 16.2.10: 29 cases agree, 7 unsure or
  unmapped in Graphvein (not compared), 0 disagreements.
- `server_actions.cjs`: the server action oracle of
  `TestServerActionsOracle` (`internal/index/nextjs_actions_oracle_test.go`,
  T024b, opt-in). It reads a checkout with the TypeScript 6.0.3 compiler
  (install as in `scripts/oracle/ts_structure/README.md`): directive
  prologues, the export names of `'use server'` modules, inline
  `'use server'` functions, and every identifier the checker resolves to
  one of them, with its form.

  ```bash
  GRAPHVEIN_TS_ORACLE_DIR="$SCRATCH/oracle" \
  GRAPHVEIN_ACTIONS_ORACLE_CORPORA="<checkout>:<checkout>" \
    go test ./internal/index -run TestServerActionsOracle -v
  ```

  Checked on the fixture and the six T024 corpora: 25/25 actions and 19/19
  linked uses agree, 0 disagreements.

T024c1b R1: with `GV_REAL_EXTENSIONS=1`, `replay-supabase.sh` also writes
`<out>.members.json`, the objects `vector` and `http` own (`pg_depend`
deptype `e`), which are compared by hand with `supabaseExtensionObjects`
in `internal/extract/schema/supabase.go` (types with their array types,
domains, functions, aggregates and access methods by name; no relation).

T024c2: `postgrest_chains.cjs` is the chain oracle of
`TestSupabaseChainsOracle` (`internal/extract/tableaccess/supabase_test.go`,
opt-in). It evaluates every claimed fixture chain with the query builder of
`@supabase/postgrest-js` (MIT; 1.18.1, supabase-stripe's pin) and prints
method, path, schema and `Prefer`; nothing connects (`@supabase/node-fetch`
is replaced by a stub that throws, and no builder is awaited).

```bash
mkdir -p "$SCRATCH/postgrest" && cd "$SCRATCH/postgrest" && npm pack @supabase/postgrest-js@1.18.1 && tar xzf supabase-postgrest-js-1.18.1.tgz
GRAPHVEIN_POSTGREST_ORACLE_DIR="$SCRATCH/postgrest/package" \
  go test ./internal/extract/tableaccess -run TestSupabaseChainsOracle -v
```

Checked on the fixture: 23/23 chains agree (operation, table, schema).

T024 review: `GRAPHVEIN_POSTGREST_ORACLE_CORPORA` (an OS path list of
checkouts) runs the same comparison on every claimed SB-T1 and SB-T2
chain of each checkout, so the corpus edges labelled `oracle` are
compared chains; a free name in a chain's arguments (a filter value such
as `session.user.id`) reads as a placeholder whose members and calls are
the placeholder again. Version 1.9 packages ship `dist/main` instead of
`dist/cjs`; the script takes either. Checked with 1.18.1 and 1.9.2
(chatbot-ui's lock): fixture 29/29, chatbot-ui 130/130, supabase-stripe
12/12 chains agree.

T024 review: a chain that starts with a const builder binding (row
SB-T1's const builder: `const q = supabase.from('t');
q.select()`) carries the binding (`bound`: name and value text), and the
script evaluates it from the binding's value, so these chains are compared
too and keep the label `oracle`. Checked with 1.18.1 and 1.9.2: fixture
30/30 (with `src/db/builders.ts:7`), chatbot-ui 130/130, supabase-stripe
12/12 chains agree.

```bash
C=spike/parsing/.corpus
GRAPHVEIN_POSTGREST_ORACLE_DIR="$SCRATCH/postgrest/package" \
  GRAPHVEIN_POSTGREST_ORACLE_CORPORA=$C/chatbot-ui:$C/supabase-stripe \
  go test ./internal/extract/tableaccess -run TestSupabaseChainsOracle -v
```

T024 review, C4: `server_actions.cjs` also computes row SA-C3's module
graphs with the compiler's own module resolution (`ts.resolveModuleName`)
and import elision (a binding is kept when its symbol is a value): the
client graph over kept imports and re-exports, the server graph over
every import and re-export that is not type-only plus `import()` and
`require()` of a string literal (an unresolved one leaves it open). Each
direct call gets the boundary those graphs prove; `TestServerActionsOracle`
fails when Graphvein labels a call `client` that the compiler graphs do
not prove (`unknown` claims nothing). It also reads row SA-M1's default
export forms (`export default` of an async arrow, a call or an identifier,
from `server_actions.rs` at `next` 14.1.0 to 16.4.2) and row SA-C1's
element rule (`action` on an intrinsic `form` or next/form's `Form`,
`formAction` on an intrinsic `button` or `input`, resolved by the
checker).

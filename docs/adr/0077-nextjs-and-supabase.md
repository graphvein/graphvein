# ADR 0077 - Next.js App Router routes, server actions and Supabase table access

**Status:** Accepted (delegated review, 2026-11-19)

## Context

Milestone M5 (item 2) of the project plan adds Next.js App Router route handlers and server
actions and the Supabase JS client. The T024 investigation (2026-11-18;
the T024 task records (internal)) pinned four
corpora (chatbot-ui, ai-chatbot, saas-starter, supabase-stripe) and
re-read umami and dokploy with main (`extractors-45`):

- 188 route handler files with 246 verb exports produce no route node;
  19 fetch sites to those handlers are `unmatched`.
- 25 server actions and their 27 uses are ordinary functions; only direct
  calls have `calls` edges.
- 162 Supabase client sites produce nothing, and `supabase/migrations`
  is not read (migration discovery is sqlx-only, ADR 0047), so there is no
  table to link to. The existing PostgreSQL 17 statement reader reads those
  files: in a scratch sqlx wrapper it yields exactly the tables PostgreSQL
  yields (26 and 5), but row level security statements and references to
  the Supabase platform's `auth` and `storage` objects make tables partial.

The task draft (internal; its tables are in `docs/contract/claims/nextjs-supabase.md`) holds the claim
contracts (rows NX, SA, SB, each with one default branch D). The developer
decided open points O1-O7 on 2026-11-18 (recorded in the task file under
"Decisions (developer, 2026-11-18)"); this ADR reflects those decisions.

## Options considered and decisions

### 1. Next.js route paths

- **Transcribe the file-system rule in Go and check it against Next.js's
  own helpers (chosen).** Groups and slots drop out, `[x]`, `[...x]`,
  `[[...x]]` become `{x}`, `{*x}`, `{*x}` optional; private folders and
  intercepting segments are refused. The oracle runs `normalizeAppPath`,
  `getRouteRegex` and `getSortedRoutes` from the `next` package at each
  corpus's locked version on file paths only: 188/188 agree.
- Run `next build` or the route manifest: executes corpus code. Rejected.

Routes are ordinary HTTP route nodes (ADR 0013) with `framework: nextjs`;
the client matcher (ADR 0018) gains them as candidates. Next.js routes are
same-origin, so a call with a literal origin never links to one (a
precision rule found on dokploy, whose `api.porkbun.com` calls would
otherwise link to `/api/[...trpc]`). `basePath` composes as a prefix;
an env-dependent `basePath` leaves the base unresolved. Rewrites and
middleware are not applied and the edge says so (assumption NX-V1).

Decided (O2 (a)): Pages Router `pages/api/**` files are routes too, one
`ANY` route per file (`framework: nextjs_pages`), only together with the
same-origin rule. Decided (O7, revised): linking follows file-system
routing with NX-V1 shown on every linked call; `next.config` literal
`rewrites` (and `basePath`) are read in the route unit: a call whose path
matches a literal rewrite `source` gets no Next.js candidate of that
project (the request may be rewritten elsewhere), and non-literal
configuration stays unknown (`nextjs_rewrites_unread`, base `unresolved`).

### 2. Server actions

- **A transport of its own (chosen, O1 (a)).** A
  `server_action` node per action (module `'use server'` exports and inline
  `'use server'` functions), `exposes_action` from the function, and
  `calls_action` from `<form action>`, `formAction`, `useActionState` /
  `useFormState` and direct calls. Shown in the API section next to HTTP
  and Tauri IPC (ADR 0042), as Tauri commands are (ADR 0038).
  Decided (O3, revised): every direct call to an action is a
  `calls_action`; `boundary: client` only where proven (the call's file
  has `'use client'` or is reached from such a file through resolved
  static imports), otherwise `boundary: unknown`; never a server-side call
  labelled as a client call.
- As HTTP routes `POST <page>`: the request goes to whichever page renders
  the form, with an action ID header; a route fact would be wrong.
- Plain `calls` edges only: correct but invisible in the API view.

### 3. Supabase

- **Supabase migration sets read by the existing reader, client calls as
  code-to-table rows (chosen).** Discovery follows the Supabase CLI
  (`supabase/config.toml` and `supabase/migrations`, name
  `^([0-9]+)_(.*)\.sql$`, lexical order), `tool: supabase`. The platform's
  objects (`auth`, `storage`, `extensions`, roles) resolve as external
  (assumption SB-V2). Client receivers are proven only through the
  `@supabase/supabase-js`, `@supabase/ssr` and auth-helpers factories
  (direct, const, imported const, one wrapper level); receiver names never
  prove a client. `.from('t')` plus a PostgREST verb gives
  `reads_table` / `writes_table`, `inferred` (assumption SB-V1, as TA-V1).
- Decided (O4, extended): row level security statements are
  schema-neutral; each table keeps an oracle-checked `rls` flag
  (`pg_class`) and its policies' name, command and table as written
  (`pg_policies`), with no claim about what a policy permits.
- Decided (O5, revised): an embedded resource in a `select` string
  resolves to a table only where exactly one foreign key relationship
  proves it (`mapped`); otherwise the base table plus a `partial` unknown.
- Decided (O6, revised): the Supabase CLI's generated `database.types.ts`
  (exact header and shape gated) is a schema source labelled "generated
  types" (`tool: supabase_types`) where a package has no
  migration set; without either, unknown records only
  (`no_schema_source`).
- Name-based receivers (`supabase.from`): rejected; unproven receivers
  with the exact chain shape give an unknown record instead.

### 4. Oracles

TypeScript 6.0.3 for export names, directives and use resolution; the
`next` path helpers for templates; Supabase migrations replayed into the
throwaway PostgreSQL 17 cluster over a platform prelude
(`scripts/oracle/nextjs_supabase/`) for tables; `@supabase/postgrest-js`'s
query builder, which builds method and URL without a request, for chain
operations. Not cheap, so mapped: `basePath` evaluation, rewrites, the
client module graph, PostgREST embedded-resource resolution.

## Implementation notes (T024a, extractors-47)

- Cached syntax: the TypeScript extractor adds a small `facts.NextFacts`
  payload (destructured export names, the default-export statement span,
  and the literal reading of `next.config.*`: `basePath`,
  `trailingSlash`, `pageExtensions`, `rewrites`). `routes.LinkNext` builds
  the route nodes at link time from it and the shared export facts; no
  file is re-parsed and nothing is executed.
- Route nodes carry `project`, `base_path` and, when the config has the
  key, `rewrites` (`literal` rules or `unread`); the client matcher reads
  them for the same-origin rule, rewrite withholding and unresolved bases.
- A route path with an env-dependent `basePath` (`resolution:
  unresolved`) is a candidate only for calls whose base is unresolved too:
  a call without a base reaches it only if the deployment's `basePath` is
  empty, which nothing proves.
- Linked calls carry `assumes: ["NX-V1"]` on the path entry and the
  `client_call` node (edges have no attributes).
- Oracle: `TestNextRoutesOracle` (opt-in, `GRAPHVEIN_NEXT_ORACLE_DIST`)
  runs Next.js's own `normalizeAppPath` / `getRouteRegex` from an extracted
  `next` package (MIT) on route file paths.

## Implementation notes (T024a2, extractors-48)

- SWR keys (row NX-L2): a key the bounded URL evaluator (ADR 0020) reads
  as strings is read; a `null` alternative is SWR's "do not fetch" key and
  drops out. An inline arrow fetcher counts when its text proves one plain
  parameter and an expression body that starts with the recorded fetch or
  axios GET of that parameter without options, the only request in the
  arrow. The request keeps its transport (fetch, or axios with its
  instance's base) for URL rules; the node reports `library: swr`.
- Rewrites (row NX-L3) follow Next.js's order as `next` 14.1.0 and
  16.2.10 implement it (`resolve-routes.js`, `filesystem.js`,
  `load-custom-routes.js`): beforeFiles rules, an exact static file check,
  afterFiles rules (each followed by a check of every route), a route
  check, fallback rules; sources match case-insensitively and strictly;
  `basePath` prefixes sources and same-origin destinations. Only a sure
  match of the first rule that may match re-routes, and only when no
  later rule may match the destination before a route surely serves it;
  everything else withholds as in T024a. Pages and `public/` files are
  not modelled, so afterFiles and fallback re-routes show assumption
  NX-V3. `TestNextRewritesOracle` checks the source and destination
  reading against `next`'s own `getPathMatch` and `prepareDestination`.

### T024b implementation notes (2026-11-18, extractors-49)

- Package `internal/extract/actions`, reading the new cached
  `facts.ActionFacts` (directive prologues, `'use server'` module exports,
  inline `'use server'` functions with their route scope, JSX expression
  attributes) and the route scopes and module resolver at link time.
- A linked use is an `action_call` level-4 node in its owner with the
  `calls_action` edge, as Tauri's `ipc_call` (edges carry no attributes;
  the API section counts calls as nodes). Only proven uses are stored, so
  the transport's one status is `linked`; refused forms are counted
  (`server_action_unread`, `server_action_use_unread`).
- The client module graph of row SA-C3 follows only imports that load a
  module at runtime (side effects, or a non-type binding naming a value
  node or a namespace) and never enters a `'use server'` module.
- Actions are declared only under a `package.json` that declares `next`.
- API section: transport `server_action` (endpoint kind `server_action`,
  call kind `action_call`), families `server_actions` and `action_calls`
  (row D, best_effort), a teal transport colour.
- Revision R1: a server module graph (from `app/`, `pages/`, middleware,
  proxy and instrumentation, stopping at `'use client'`) makes a module in
  both bundles `boundary: unknown`; conditional values and inline actions in
  form attributes link; unread uses set `unread_uses` (never "not
  called"); `imported_clause` and `let` / `var` inline actions follow
  `next-custom-transforms` `server_actions.rs`; Changes row C18 (C17 until the stacked gate, where T026 kept C17).

### T024c1 implementation notes (2026-11-18, extractors-51)

- Supabase sets join the schema pass beside sqlx sets
  (`internal/extract/schema/supabase.go`): discovery and lexical order per
  the CLI's `ListLocalMigrations` (SB-M1), the same PostgreSQL 17 reader,
  rows and default branch; each file runs in one implicit transaction
  block (the CLI's pgx batch with its bookkeeping `INSERT`); a repeated
  version or an unreadable member makes it and later members `applied:
  null`. Mapped, not oracle-checked: the block model and the bookkeeping
  table check.
- SB-M2: one-column foreign keys to the platform keys `auth.users(id)`,
  `storage.objects(id)`, `storage.buckets(id)` of the same type are
  `status: external` references assuming SB-V2 (no node, no edge); the
  migration records list the SB-V2 objects (`platform`), shown in the set
  strip; the API adds `external_references` (table detail, sets) and
  `platform` (sets), omitted when empty.
- SB-M3: `rls {enabled, forced}` and `policies [{name, command, table}]`
  on Supabase tables only; RLS words and policy statements are
  schema-neutral there; sqlx sets are unchanged.
- Oracle: `TestSupabaseOracle` extends the T018a2 harness with a catalog
  hook (`relrowsecurity`, `relforcerowsecurity`, `pg_policy`); 0
  disagreements on the fixture, chatbot-ui and supabase-stripe.
- Known gaps: a policy whose `USING` or `WITH CHECK` PostgreSQL would
  refuse is read as succeeding (decision O4: schema-neutral; the oracle
  replays the corpora); `[db.migrations] enabled = false` and
  `schema_paths` in `config.toml` are not read; a project without
  `config.toml` (supabase-stripe) has no set; `CREATE EXTENSION` of the
  extensions Supabase lets the `postgres` role create (`http`, `vector`)
  still may fail under E6, which leaves most chatbot-ui tables partial.

### T024c1b implementation notes (2026-11-18, extractors-52)

- SB-M4 (assumption SB-V4, `mapped`): in a Supabase set, `CREATE
  EXTENSION` of `http` or `vector` is not superuser-only (Y51's O2.P4
  may-fail does not apply); it stays opaque (O2). Supabase's allow-list
  (supautils' privileged extensions in the supabase/postgres image) is not
  on a reachable host: the Supabase CLI source (Go module proxy, v1.226.5
  and v2.121.0, MIT) carries no machine-readable list, only a link to the
  supabase/postgres repository, and the npm registry has none. The list is
  the two extensions the corpora create, each checked by replaying
  chatbot-ui unshimmed on PostgreSQL 17.12 with the PGDG builds of
  pgvector 0.8.7 (PostgreSQL licence) and pgsql-http 1.7 (MIT), as a
  superuser standing in for supautils: 25/25 migrations, 26 tables.
- SB-M5 (assumption SB-V3): a `supabase/migrations` directory without
  `config.toml` whose entries all follow the CLI's name pattern (and that
  is no sqlx set) is a Supabase set with the directory as its package.
- The `platform` attribute adds `assumptions: [{id, text}]` (SB-V3, one
  SB-V4 per extension the set creates), which the set strip lists.
- Reader rows the corpora reach, each a precise Postgres rule with live
  cases (`TestSupabaseC1bCasesPostgres`) and the fixture oracle: R6.P14's
  PL/pgSQL subset reads `SELECT ... [INTO [STRICT] targets]`
  (make_execsql_stmt, read_into_target) with a function call as a FROM
  item, and declared names that are unreserved SQL keywords (decl_varname
  is a T_WORD); N2.P5 proves a routine's `SET search_path` (check_search_path
  checks list syntax only); Y09 takes `public.f` for the set's unqualified
  `f` created under E4's search_path.
- Known gaps: `CREATE EXTENSION` stays opaque, so a later `CREATE TABLE IF
  NOT EXISTS` stays partial and a later CHECK's functions are not proven
  (chatbot-ui's 26 tables stay partial); DML on platform tables
  (`INSERT INTO storage.buckets`) is not modelled, so chatbot-ui's second
  migration may fail; the extension list is not Supabase's own.

### T024c1b R1 notes (2026-11-18, extractors-53)

- SB-M6: a platform extension created into a named schema (not public)
  is a known effect (`supabaseExtensionObjects`, from the pgvector 0.8.7
  and pgsql-http 1.7 install scripts: no relation, no implicit cast from a
  pg_catalog type, no polymorphic argument), guarded by the set creating
  none of those names and no operator-like object. The real-extension
  replay lists each extension's `pg_depend` members: they equal the lists
  (no relation).
- SB-M7 (assumption SB-V5, shown with the statement): `INSERT INTO
  storage.buckets` skips N3.P1 and, when nothing in the set can attach a
  trigger or rule to `storage.buckets` (no other use of the name besides
  `REFERENCES`, no dynamic `EXECUTE`, no body in another language), is no
  opaque data change.
- `char_length(col) <op> n` on `text` columns and `<table>_<col>_idx`
  index names were already proven where the table's facts are known; they
  were partial only after the opaque statements. `char_length` over
  `varchar` stays unproven (overload resolution through the preferred
  type is not modelled).
- Known gaps: chatbot-ui's fifth migration (`add_files.sql`) may fail on
  a `LANGUAGE sql` function body whose query Postgres analyses at creation
  (R1b.P5); every later migration stays `applied: null`.

### T024c1c notes (2026-11-18, extractors-55)

- A LANGUAGE sql AS body is parse-analysed at creation when
  `check_function_bodies` is on (fmgr_sql_validator, REL_17_6; checked in
  the source and live): `R1b.P5` now proves one closed form of it
  (`internal/extract/schema/sqlbody.go`; the expression reader gains
  parameters, EXISTS, one array subscript and qualified platform calls
  only while proving such a body). Not proven, so may fail as before:
  several statements, joins, other clauses, polymorphic or array
  parameters, a SET clause, BEGIN ATOMIC bodies, E1 or set functions in
  the body, and any body after an opaque statement, an operator-like
  object or a statement naming `row_security`.
- SB-M8 (assumption SB-V2): `storage.foldername(text)` returns `text[]`,
  `storage.filename(text)` returns `text`, the only signatures the
  Supabase storage API's migrations define (verification mapped).
- chatbot-ui: `add_files.sql` applies; the next blocker is
  `add_file_items.sql`'s `vector(384)` column (M2.P1: the type is created
  by the platform extension in `extensions`, which the pass does not hold
  as a type).

### T024c2 implementation notes (2026-11-18, extractors-54)

- `internal/extract/tableaccess/supabase.go`: rows SB-K1, SB-T1..SB-T5 and
  the table's D, copied into `docs/extract/table-access.md` "Supabase
  rows" (`TestSupabaseContract`, `TestSupabaseTags` over fixture
  `table-access-supabase`). The pass reads the cached call chains
  (`facts.ClientCall`) and route scopes; the TypeScript cache adds
  `Mutable` (let / var) and `Async` to bindings and the awaited expression
  to `await_expression`, so `let` stays unproven and an async wrapper
  counts only when awaited.
- Claims resolve like a CT-Q2 reference against the Supabase sets of the
  chain's npm package (exact names, `schema.name`); the base claim is
  row SB-T1 (`oracle`: `TestSupabaseChainsOracle` runs every claimed
  fixture chain through `@supabase/postgrest-js` 1.18.1, MIT, offline:
  method, path and schema agree on 23/23), embeds are a second site of
  row SB-T3 (`mapped`). An embed claims only in a set whose tables are all
  complete and which has no opaque statement; a junction is any third
  table with keys to both; any `!` refuses.
- `.rpc(fn)` on a proven client is a D `statement_not_traced` record
  naming the function. SB-T5 records only in packages that declare a
  Supabase JS package.
- Precision: row PL-F9's `Match.Tools` lists the table tools it covers
  (sqlx, sqlmodel, prisma, drizzle); Supabase links are `best_effort` and
  `/api/v0/precision` counts them as the row D family
  `table_access_other`. The schema catalogue serves SB-V1.

### T024c3 implementation notes (2026-11-18, extractors-55)

- `internal/extract/models/supabasetypes`: rows SB-G1, SB-G2 and D
  (`docs/extract/schema-supabase-types.md`, copied from the task;
  `TestSupabaseTypesContract`, `TestSupabaseTypesTags` over fixture
  `supabase-types`, golden `TestSupabaseTypesIndexedFixture`). The accepted
  form is postgres-meta's `typescript` template (npm
  `@supabase/postgres-meta`, MIT; read at 0.60.0 to 0.97.0, the template
  left the published package at 0.99.0): a token-level parser that ignores
  whitespace, comments, separators and quote style (prettier output and
  projects' reformatting) and refuses every other form
  (`supabase_types_unread`). Tables go through the model kit as group
  `models/supabase_types/<file>`, every fact `mapped`; columns are the
  `Insert` properties (`Row` may end with computed fields, which are no
  columns); nullability is the written `| null` (a bare `unknown` or
  `any` is unknown: from 0.88 the generator writes no `| null` beside
  them). Relationships stay a table attribute, not foreign keys: the
  generator writes no referenced schema (chatbot-ui's and supabase-stripe's
  keys to `auth.users` read `referencedRelation: "users"`, and stripe has
  a `public.users` too), and 0.97 leaves out keys to other schemas.
- SB-G2: in a package with a Supabase migration set no types table is
  emitted; the types are compared with the set (tables; columns,
  nullability and relationships on complete migration tables) and every
  difference is an info diagnostic, never resolved.
- Table access: a package without a Supabase set resolves SB-T1/SB-T2
  against its generated types (`assumes: SB-V6`); SB-T3 never resolves an
  embed through them. Accepted contract change: a `.select(<non-literal>)`
  keeps the base claim and records a partial `embedded_resource` unknown
  ("embedded tables not read") with the package's other tables as
  candidates. `internal/changes` counts `supabase_types` tables as
  covered by a code-to-table row.
- Oracle: `scripts/oracle/nextjs_supabase/supabase_types.cjs`
  (TypeScript 6.0.3, `TestSupabaseTypesOracle`, opt-in): tables, columns,
  `Row` properties, written nullability and relationships agree on the
  fixture and both corpus files (41 tables, 322 `Row` properties, 76
  relationships, 0 disagreements).
- Precision: tool `supabase_types` is in no PL row: its tables and the SB
  edges into them are `best_effort`.

### T024 review fixes (2026-11-18, extractors-56)

Fixes for the T024 task record (internal) (A1, B1-B4, C1-C7), each as a class
rule with its contract row, docs and fixture kept 1:1.

- A1 (row SA-C3): `valueImports` follows re-export clauses with a module
  (`export { a } from`, `export *`, `export * as ns`), which load that
  module whenever the barrel loads. The two graphs now differ in
  direction: the client graph follows proven loads only (a binding that
  names a value declaration), the server graph every import or re-export
  that is not type-only (the compiler may or may not elide it), and
  `import()` / `require()` of a string literal (recorded per file as
  `ActionFacts.Loads`). One the resolver cannot follow in a server-graph
  file leaves the server graph open: no module is then proven
  client-only (`server_graph_incomplete`). Considered: following only the resolved
  binding's file (`Modules.Export(...).File`) - rejected, a barrel loads
  every module it re-exports.
- B1 (row NX-L3): before any outcome of an afterFiles or fallback rule
  (an external, unmapped or applied destination), a route of the project
  that surely serves the call path wins (Next.js checks the file system
  before afterFiles rules and every route before fallback rules,
  `resolve-routes.js`); one that only may serve it withholds.
- B2 (row NX-L1): a same-origin call in a Next.js application matches
  only that application's routes; a call outside every application in a
  repository with several is `ambiguous` (`nextjs_origin: unknown`), never
  linked. Considered: linking a shared package's call when exactly one
  application has the route - rejected, the module may run in another
  application where the path is a 404. The applications come from
  `routes.NextProjectDirs` (row NX-P1's layout), passed to the client
  matcher as origin entries.
- B3 (row SB-T3): a final `!inner` / `!left` is PostgREST's join type and
  leaves the embedded resource as without it; any other name after `!`
  is a hint (partial).
- B4 and C1 (row SA-M1): `export default` of an async arrow
  (`default_async_arrow`), a call (`default_call`) and an identifier of a
  local action binding (`default_clause`) or an import (`imported_clause`)
  are actions, read from `server_actions.rs` at every corpus version
  (`next` 14.1.0, 15.1.4, 15.6.0-canary.59, 16.2.10, 16.4.1, 16.4.2). Sync
  functions: a build error from 15.1.4 (exported functions inherit the
  file directive); at 14.1.0 a sync arrow and a sync function exported by
  name are registered (the runtime check tests `typeof` only). D for both
  is a missed action at 14.1.0, never a wrong one.
- C2 (row NX-R3): `_`-prefixed Pages Router files and folders are routes
  (no private-folder filter in `collectPagesFiles` / `recursiveReadDir`
  at those versions).
- C3: the committed vitest cache under `web/src/node_modules/` is removed
  from the tree and `/web/**/node_modules/` is ignored; the files stay in
  the history of `dea9bc3e` (history is not rewritten).
- C4 (verification labels): the postgrest-js oracle runs on the corpus
  chains too (`GRAPHVEIN_POSTGREST_ORACLE_CORPORA`), so SB-T1 / SB-T2
  edges keep `oracle`; the route template oracle's scope is stated
  (App Router template strings only); a boundary oracle in
  `server_actions.cjs` computes the SA-C3 graphs with the compiler's
  module resolution and import elision, and every `client` label must be
  one those graphs prove.
- C5 (row SB-T1): a const-bound builder used with a verb in its function
  is read as one chain ("const builder"); any other chain without a verb
  on a proven client is an unknown `builder_unread` (no longer D).
- C6 (row SB-M3): an RLS flag is unknown in both directions when a
  statement the pass did not read may have changed it: an opaque
  statement since the flag was set, a statement on the table that could
  not be read, code that alters it, or an unreadable member. The former
  "false is unknown on a partial table" rule is gone (it left a `true`
  that an opaque `DISABLE` could have cleared); read statements change
  the flags only through SB-M3's four words. A policy's `table` is the
  table holding it under its current name (pg_policies follows renames);
  `TestSupabaseOracle` compares it.
- C7 (row SA-C1): `form_action` only for `action` on an intrinsic `form`
  or next/form's `Form` and `formAction` on an intrinsic `button` or
  `input`; the same attribute on a component is a prop (D, counted).

### NX-F2 notes (2026-11-18, extractors-56)

Coverage recovery after the T024 review fixes, rows SB-M9 and SB-M10
(task table, `docs/extract/schema.md` "Supabase sets"; code in
`internal/extract/schema/supabase.go`, tests `supabase_nxf2_test.go`).
T024c1c (LANGUAGE sql bodies, SB-M8) was merged first; its fixture
migration became `20240106000000_sql_functions.sql`, because the review's
`20240105000000_archive.sql` already held its version (the Supabase CLI
keys migrations by version).

- SB-M9: C6 made every RLS flag unknown after any opaque statement, so
  chatbot-ui's `UPDATE`s in `20240125192042_upgrade_openai_models.sql`
  (opaque only because the set defines triggers, O1) left 24 of 26
  `enabled` flags unknown. A data statement runs only code the set can
  name (PostgreSQL 17: the target's and its foreign key actions' triggers
  and rules, the written rows' defaults, constraints, index and policy
  expressions; event triggers fire on DDL only). The decision reuses the
  P12 inventory, which reads a superset of that code (every trigger
  function and every routine named outside its definition, closed over
  calls and data changes) instead of resolving triggers per table and
  per foreign key cascade: a bounded inventory with no statement in it
  proves the flags unchanged. Options considered: per-table trigger and
  cascade resolution (narrower code to read, but a new trigger-to-table
  model to keep right); a set-wide "every routine is inert" rule (rejects
  chatbot-ui, whose `delete_storage_object` calls the http extension).
  The rule holds only while every routine of the set was read: no earlier
  opaque statement other than such a data statement, no unread statement.
  The statement stays opaque for every other fact (columns, keys,
  completeness).
- SB-M10: SB-M6's extension types are column types in the extension's
  schema. An unqualified name resolves by the session's search_path; the
  platform's default `"$user", public, extensions` is stated in SB-V2.
  Modifiers follow each type's `typmod_in`, checked live on PostgreSQL
  17.12 with the PGDG builds postgresql-17-pgvector 0.8.7 and
  postgresql-17-http 1.7.2 (extension version 1.7), installed into a
  relocated copy of the server in scratch, not into the system.
  `TestSupabaseNXF2CasesPostgres` skips the SB-M10 cases where the server
  lacks the two extensions (CI's postgres job).
- Known gaps: chatbot-ui's `add_file_items.sql` still may fail on its
  `hnsw` index (M4.P4: the access method's capabilities and the
  `vector_cosine_ops` operator class are not modelled); its
  `match_file_items_*` functions take `vector` parameters, which SB-M10
  does not cover (only column types). R1b.P5's analysed SQL bodies still
  require no opaque statement before them, so SB-M9's data statements
  stop later LANGUAGE sql bodies from being proven.

### NX-G notes (2026-11-18, gate preparation)

Follow-ups B5 and C8 of the T024 review's confirmation pass.

- B5 (SB-M9): a superuser's `UPDATE pg_catalog.pg_class SET
  relrowsecurity = false` or `DELETE FROM pg_catalog.pg_policy` changes
  row level security without a statement SB-M3 reads, so SB-M9 now
  applies only when the data statement's target and every data target of
  its inventory (`DELETE` targets added to the P12 scan) is a table the
  set holds: unqualified and resolving to one held table (never a `pg_`
  name, which pg_catalog shadows), or qualified by its schema, or by
  `public` for a table created unqualified, with no other relation of the
  name recorded (a view may carry rules). Any other data statement, at
  top level (also in a set without code) or in the bounded inventory of
  an O1 statement, and an O1 statement whose inventory is unbounded, make
  the `rls` flags and the `policies` of every held table unknown and end
  SB-M9 for the set (a catalog write may also change code). SB-M7's
  `storage.buckets` insert stays excepted under SB-V5. The unbounded case
  also closes a gap the cases exposed: `DO $$ BEGIN EXECUTE 'DROP POLICY
  p ON ' || 't'; END $$` kept `policies: [p]`, PostgreSQL drops it.
  Options considered: a list of catalog relations (the catalog grows and
  views and platform tables carry rules and triggers too, so a positive
  rule on held tables is narrower to keep right); treating only
  `pg_catalog` targets (misses views, `information_schema` and platform
  tables). The first version required `writeTarget`'s exact
  qualification and made chatbot-ui's flags unknown again (its trigger
  functions insert into `public.profiles`, created as `profiles`); the
  `public` rule restores them, checked by the oracle. Fixture
  `rls-catalog` (its own project: TestSupabaseOracle's replay runs
  without superuser rights, as the platform, and is refused with 42501,
  which the pass marks may-fail).
- C8: the postgrest-js harness evaluates a chain that starts with a const
  builder binding (C5) from the binding's value (`bound`: name and value
  text), so every chain labelled `oracle` is compared; no label changed.

## NX-S notes (stacked gate on T023, T022 and T026; extractors-49)

- Version: T024 is extractors-49 on the stacked branch (T023 46, T022 47,
  T026 48 keep theirs). Changes row C17 stays T026's ("React UI"); the
  server action row is C18.
- SB-M9 and rules: SB-M9 did not read rule actions, and PostgreSQL runs
  a rule's actions with every `INSERT`, `UPDATE` or `DELETE` on its table
  (`rewriteHandler.c`); an action may write `pg_catalog.pg_class` or
  `pg_policy` (checked live: row level security off, policy dropped). A
  top-level `CREATE [OR REPLACE] RULE` in a Supabase set now makes the
  `rls` flags and `policies` of every held table unknown and ends SB-M9
  for the set; every later data statement does the same (a table created
  later may be what an action writes); a rule created by code of the set
  applies from the set's start. Options considered: reading rule actions
  into the P12 inventory (precise, but a new parser surface for a form no
  corpus uses); refusing only data statements on a rule's own table
  (needs the rule's target resolved, and `DO INSTEAD` rewrites). Fixture
  `rls-rule`; no launch corpus has a rule, so no count changed.
- SB-M9 and code the pass cannot read (TOP-1 of 2026-11-18): a rule, trigger
  or event trigger created through a dynamic `EXECUTE` is invisible, and
  an event trigger (also one created at top level) runs with every later
  DDL statement. A statement that runs code the pass cannot read (an
  unbounded P12 inventory), a `CREATE EVENT TRIGGER` (top level, or in
  the set's code from the set's start) and a file whose rest cannot be
  lexed end SB-M9 for the set: every table's `rls` flags and `policies`
  are unknown to its end, tables created later included. Fixture
  `rls-dynamic`; dynamic rule, trigger and event trigger cases checked
  live as a superuser (the event trigger cases were wrong before: `rls`
  enabled `true`, Postgres `false`).
- Projects (T022): the stored route matcher of the project link pass
  leaves Next.js routes out. A call in another repository is not proven
  to share the application's origin (NX-L1), and a project link could not
  carry NX-V1/NX-V3; such a call stays unlinked. Superseded by TOP-1 of 2026-11-18: another member's Next.js routes are project link targets under
  the project rules (the cross-repository assumption; a literal origin
  only through a declared `origins` entry), the link stores `assumes`
  (NX-V1, NX-V3) beside its assumption text (additive field of
  `store.ProjectLink`), the same-origin rule stays the repository's own,
  and an application divided by link scopes is no target
  (`docs/extract/project-links.md`, "Next.js route targets").

## Consequences

- New extensible kinds (contract v1 unchanged): nodes `server_action` and
  `action_call` (T024b), edges `exposes_action`, `calls_action`; route `framework` values
  `nextjs` and `nextjs_pages`; migration set `tool: supabase`; schema
  source `tool: supabase_types`; table attributes `rls` and `policies`;
  `table_access` `tool: supabase`. `docs/contract/v1.md` lists them.
- Each implementation step bumps the extractor version.
- Precision labels stay `best_effort` for these families until rows earn
  them (a Supabase migration-set row beside PL-F1; the SB rows under PL-F9
  only with their own evidence).

## Known gaps

Interception routes, middleware and proxy files, non-literal rewrites,
redirects and headers, rewrite chains and rewrites a call may match only
unprovably (withheld, not re-routed), pages and `public/` files at a
rewritten call's path (assumption NX-V3), Pages Router pages, i18n
prefixes, app wrappers returned from hooks (umami `useApi`), Playwright
`request` calls, actions passed through props, `.rpc`, storage, auth and
realtime calls, embedded resources without a single foreign key proof,
Supabase access with neither a migration set nor generated types in the
package, generated-types relationships as foreign keys and their TypeScript
types as Postgres types (T024c3), the boundary of direct action calls outside the client module
graph, server imports the module resolver cannot resolve (a shared module
could then read `client`), one server graph for the whole repository (a
dynamic import in one application makes every application's client labels
unknown), and escaped Supabase builders whose verb use is elsewhere
(`builder_unread`), a Next.js application divided by a member's link scopes as a project
link target (TOP-1 of 2026-11-18) and server actions on the architecture map (T026 draws
neither `exposes_action` nor `calls_action`).

## Open points

None open: O1-O7 were decided by the developer on 2026-11-18 (task file,
"Decisions (developer, 2026-11-18)"). Steps: T024a routes, `pages/api`,
rewrites and `basePath`, client links; T024b server actions; T024c1
Supabase migration sets with RLS; T024c2 Supabase table access with
embeds; T024c3 generated types.

## Acceptance notes (2026-11-19)

- Results: combined gate CI run 44533180619 on 0ef4343 (`[ci windows]`; T023 + T022 + T026 + T024 stacked): postgres raw log 0 FAIL, 226 `/postgres` PASS (skips only environment-gated live cases: SB-M10 extension cases need pgvector/pgsql-http, B5 public-qualified cases need a superuser; both were checked live locally); Windows 0 FAIL. Full check on the stacked head: 0 FAIL, 225 `/postgres` PASS.
- Revisions: T024a route handlers, pages/api, next.config basePath/rewrites, client links; T024a2 SWR keys and rewrite re-routing; T024b server actions (+R1 boundary precision, conditional and unread uses); T024c1/c1b/c1c Supabase migration sets, RLS flags and policies, platform assumptions, extension effects, SQL function bodies (developer decision for one more chatbot-ui unit); T024c2 Supabase table access; T024c3 generated types; NX-F/NX-F2/NX-G independent-review fixes (barrel re-exports, served-first rewrites, per-app same-origin, join modifiers, symmetric and recovered RLS flags, oracle labels) and its confirmation pass; stack follow-ups (C18 renumbering, `CREATE RULE` and unreadable code end SB-M9, project links to Next.js routes with NX assumptions).
- Coverage: 246 App Router + 14 Pages Router routes on the six corpora; 33 own-route client links (ai-chatbot 15 incl. SWR); 25/25 server actions, 20/20 uses, 4 boundary `client` labels proven; chatbot-ui 130 and supabase-stripe 12 Supabase access sites linked; supabase-stripe 5/5 tables complete; chatbot-ui 3/26 tables complete, RLS enabled known on 26/26 and forced on 24/26.
- Verification labels: oracle-compared: next 14.1.0/16.2.10 route templates and rewrites; TypeScript 6.0.3 actions, uses and boundaries; postgrest-js 1.18.1/1.9.2 for every chain labelled `oracle` (fixture 30, chatbot-ui 130, supabase-stripe 12); PostgreSQL 17 replay with the Supabase prelude (tables, RLS flags, policies); TypeScript generated types (41 tables). Mapped: client module graph beyond the compiler-proven boundary, embedded selects via unique foreign keys, generated-types relationships (listed, not resolved).
- Assumptions shown: NX-V1 (middleware/rewrites not applied), NX-V3 (afterFiles/fallback re-routes), SB-V1..SB-V6 (platform objects, search_path, extensions, CLI discovery without config.toml, platform-table inserts, generated types).
- Known gaps: chatbot-ui's 23 partial tables, blocked first by `add_file_items.sql:29` (`USING hnsw (... vector_cosine_ops)`), then `vector` function parameters; rule actions and dynamic SQL end SB-M9 conservatively; builders escaping after a verb use are partly unread; a Next.js app divided by a member's link scopes is not a project-link target.
- After v1: pgvector index access methods and operator classes; `vector` function parameters; generated-types relationships as foreign keys; TanStack/other routers.

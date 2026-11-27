# Known gaps

Graphvein states a fact only where it can read the code that proves it.
Everything else is meant to show up as a gap, not as a guess. This page
lists the main things Graphvein does not see or does not model today, and
how each one shows up: as **unknown**, as **unresolved** (a call or import
with no target), as a **label** (Best effort, inferred, partial, may fail,
not read), or as **nothing** (no node, no edge and no warning). Items that
show up as nothing are the ones to keep in mind when you read an answer as
complete.

Each item links to the decision record (ADR) that describes it in full.
"Not modelled yet" means Graphvein does not read that form today.

## Languages and frameworks

- **Calls are Best effort in every language.** There is no type checker:
  dynamic dispatch, inherited members, values passed around in variables
  and most assignments are not followed. Such calls stay unresolved, and
  every view that shows call edges carries the Best effort label
  ([ADR 0066](adr/0066-precision-labels.md),
  [ADR 0068](adr/0068-go-structure-contract.md)).
- **Precise structure covers four languages.** Go, Python, Rust and
  TypeScript/JavaScript files, declarations and imports can be labelled
  Precise. A file the parser or the reference compiler rejects is labelled
  Best effort and claims nothing beyond the file itself
  ([ADR 0066](adr/0066-precision-labels.md),
  [ADR 0070](adr/0070-typescript-structure-contract.md)).
- **Java, C# and Dart are Best effort only.** Files, types, callables and
  some imports are read; calls, build files and frameworks are not
  ([ADR 0067](adr/0067-java-csharp-dart-best-effort.md)).
- **Other languages are listed as files only**, with language "unknown":
  no declarations, imports or calls.
- **Ignored and vendored folders are invisible.** Files in `vendor`,
  `node_modules`, `dist`, `build`, folders matched by `.gitignore`, and
  symlinked folders are not read, so imports into them stay unresolved
  ([ADR 0070](adr/0070-typescript-structure-contract.md),
  [ADR 0071](adr/0071-rust-structure-contract.md)).
- **Code made at build time is not read.** Rust macro output, generated
  Java classes and Go `replace` or `go.work` modules have no nodes; imports
  of them are unresolved or external
  ([ADR 0071](adr/0071-rust-structure-contract.md),
  [ADR 0068](adr/0068-go-structure-contract.md)).
- **Python import roots are an assumption.** Graphvein assumes the program
  starts from the parent of its top-level package. A project that starts
  from another directory may import different files; ambiguous cases are
  refused, not guessed ([ADR 0069](adr/0069-python-structure-contract.md)).
- **Deployment files are partly read.** For the architecture map, Docker
  Compose override merging, `extends` and `include`, nginx, CDK, Kubernetes
  and Helm are not read. Guard-style auth (Starlette, Nest) and Tauri
  capabilities are not modelled. Graphvein never states that a route has
  no middleware ([ADR 0079](adr/0079-architecture-graph.md)).

## Routes and calls

- **Only the listed frameworks give routes.** Express, FastAPI, Axum,
  Go `net/http`, chi and gin, Next.js, Angular and Tauri are read. For
  Next.js, App Router route handlers (`route.ts`, one route per exported
  HTTP method) and Pages Router API routes (a default export in a file
  under `pages/api/` or `src/pages/api/`) are read; a Pages Router API
  route is one route for every method (`ANY`), with one shape that merges
  the reads of all its `req.method` branches. Pages Router pages, Next.js
  middleware files and interception routes are not modelled, nor are
  gorilla/mux, echo, fiber and httprouter; they show nothing
  ([ADR 0076](adr/0076-go-http-routers.md),
  [ADR 0077](adr/0077-nextjs-and-supabase.md)).
- **Some route paths are not known.** A router mounted through a form
  Graphvein cannot follow (a variable prefix, a wrapper function, an app
  started from the command line or a Dockerfile) keeps its route without a
  full path. The route is listed with "path not known" (or "path withheld"
  when, in Go, another pattern or route could answer the same path) or
  under unknown-prefix paths ([ADR 0084](adr/0084-unknown-prefix-paths.md),
  [ADR 0089](adr/0089-search-and-couldnt-read-endpoints.md)). For a router
  that nothing mounts, nothing shows whether it is served at all.
- **Route order is not modelled.** When two routes can answer the same
  request, both are shown as candidates; which one the framework picks
  first is not decided ([ADR 0081](adr/0081-openapi-explorer-try-it.md),
  [ADR 0076](adr/0076-go-http-routers.md)).
- **Client URLs are evaluated only in simple forms.** URL builders,
  interceptors, `Request` and `URL` objects and most values that flow
  through local variables are not evaluated. Such calls are marked dynamic and do
  not link ([ADR 0018](adr/0018-client-calls-and-cross-boundary-linking.md),
  [ADR 0020](adr/0020-bounded-client-url-evaluation.md)).
- **Path matching is case-sensitive**, even for Express routes that the
  framework matches without regard to case
  ([ADR 0018](adr/0018-client-calls-and-cross-boundary-linking.md)).
- **Generated clients**: hey-api and Orval clients are read; other
  generators and plugins are not, and their calls stay unresolved or
  dynamic ([ADR 0025](adr/0025-hey-api-generated-clients.md),
  [ADR 0027](adr/0027-orval-generated-clients.md)).
- **Supabase**: `.rpc`, storage, auth and realtime calls are not modelled
  ([ADR 0077](adr/0077-nextjs-and-supabase.md)).
- **The generated OpenAPI document leaves things out.** Routes behind
  unproven mounts are listed under `x-graphvein.unresolved`, not in
  `paths`. A missing `security` entry means "not read", but generic tools
  read it as "no requirement". Request and response shapes are often
  partial ([ADR 0081](adr/0081-openapi-explorer-try-it.md)).

## TypeScript and JavaScript call binding

- **Calls Graphvein cannot bind stay unresolved.** Calls through
  parameters, destructured names, `this` outside a class method, names
  that are written after their declaration, or code under `with` or
  `eval` have no edge: a missing edge, not a wrong one
  ([ADR 0011](adr/0011-typescript-javascript-object-owners-and-repeated-declarations.md)).
- **Dispatch is static.** `this.m()` resolves to the class's own method;
  an override in a subclass gets no edge.
- **Writes Graphvein does not follow are not seen.** A method replaced
  through a mixin that receives the object as an argument, through
  `Object.setPrototypeOf`, or from another file still shows the original
  method as the target. A plain call through a named import
  (`import { m } from "./api"; m()`) still resolves to the export after
  the same file writes `api.m = f` through the namespace.
- **Writing members through an unknown receiver costs edges.** A file that
  writes `target[key] = value` (or `api.m = f` on an imported module)
  through a receiver whose owner is not known leaves that file's member
  calls with the written name unresolved (all of them for a computed
  key): missing edges, never wrong ones.
- **Repeated names.** When a file declares several functions with the same
  qualified name (common in minified bundles), calls to that name stay
  unresolved.

## Schema

- **SQL migrations are judged under Postgres rules.** Graphvein predicts
  which migrations may fail, but the prediction is not exhaustive: a
  migration that Postgres would refuse can still show as applied
  ([ADR 0051](adr/0051-schema-claim-contract-and-postgres-oracle.md)).
- **Existing data is not evaluated.** Adding NOT NULL, a key or a CHECK
  to a table that holds rows is shown as "may fail", even when the rows
  would pass ([ADR 0051](adr/0051-schema-claim-contract-and-postgres-oracle.md)).
- **Extensions, functions and triggers are opaque.** What `CREATE
  EXTENSION`, function bodies and triggers create or change is not known;
  later tables are marked partial or opaque
  ([ADR 0047](adr/0047-sql-migration-schema.md)).
- **Statements Graphvein cannot read** make the rest of the migration set
  partial. Tables made with `LIKE`, `INHERITS`, `PARTITION OF` or `OF`
  have no columns ([ADR 0047](adr/0047-sql-migration-schema.md)).
- **Some tools are found, not read.** Alembic and Diesel migrations, and
  SQLAlchemy declarative, `Mapped` and Core tables, are listed as "not
  read" ([ADR 0053](adr/0053-python-model-syntax-payload.md),
  [ADR 0055](adr/0055-schema-assumptions-and-model-tables-in-the-api.md),
  [ADR 0062](adr/0062-diesel-models.md)).
- **ORM versions are assumed.** Model facts follow the pinned versions of
  each tool; the assumption is shown on every table. Column order and
  generated constraint names are never claimed
  ([ADR 0054](adr/0054-model-defined-schema.md),
  [ADR 0058](adr/0058-model-record-kit.md)).
- **MySQL, SQL Server and CockroachDB are mapped, not checked** against a
  real database; their facts carry the "unverified" chip
  ([ADR 0057](adr/0057-prisma-models.md),
  [ADR 0066](adr/0066-precision-labels.md)).
- **A column's source is its definition.** A later ALTER changes the
  column's facts but not the content hash of its definition, so the view
  can call the definition unchanged while the column's meaning changed
  ([ADR 0048](adr/0048-schema-read-api.md)).

## Project mode

- **Links without declarations rest on path matching.** A frontend that
  also calls a third-party API with paths like a member's routes can link
  wrongly. Such links are labelled inferred and show the assumption
  ([ADR 0075](adr/0075-multi-repository-projects.md)).
- **Base URL suggestions** read env files, Compose files, dev-server proxy
  configs and literal build-time defines only; Kubernetes and reverse-proxy
  configs are not read.
- **A rename on disk is not recognised.** The old member's index stays
  until `graphvein project forget` removes it.
- **Members indexed by an older version** may show ambiguous or unproven
  calls until they are re-indexed.

## Change impact

- **Impact is a lower bound.** Unresolved calls are counted as possible
  missing callers; they do not prove the impact boundary is complete
  ([ADR 0009](adr/0009-graph-read-queries-and-cli.md)).
- **Deep walks stop at a cap.** A walk that reaches the node cap says so;
  it never reports "nothing beyond"
  ([ADR 0056](adr/0056-change-journal-and-impact.md)).
- **Extractor upgrades and relinking** between two runs cannot be told
  apart; they are shown together. A run made from a working tree with
  uncommitted changes has no commit to re-index
  ([ADR 0056](adr/0056-change-journal-and-impact.md)).
- **Pins carry no author** and the change journal is not branch-aware; in
  team mode a teammate's run on another branch appears like any run.
- **React components wrapped in `memo` or `forwardRef`** are not followed
  by impact.

## Workspace and settings

- **Index freshness ignores settings.** When settings have changed since
  the last run, freshness can still read `fresh`; the settings state is
  reported separately ([ADR 0088](adr/0088-settings-editing.md)).
- **Runs from an external indexer** record no settings (shown as unknown)
  and no list of files without facts
  ([ADR 0088](adr/0088-settings-editing.md),
  [ADR 0089](adr/0089-search-and-couldnt-read-endpoints.md)).
- **Only the root `.graphveinignore`** is shown in settings; nested
  `.gitignore` files are not.
- **Several folders sharing one Postgres database** are separated by name
  only: the database cannot tell which folder a set of facts came from
  ([ADR 0087](adr/0087-multi-index-server.md)).
- **Folder names are matched by letter case only**: on macOS, the same
  folder typed in another Unicode form can become a second entry.
- **Search matches exact text**: "get user" finds only that text, not
  both words. Nested repositories are not listed
  ([ADR 0089](adr/0089-search-and-couldnt-read-endpoints.md)).
- **No authentication against other local processes.** The server binds
  to loopback and uses a session token, which keeps other web pages and
  remote machines out, not programs running as you
  ([ADR 0041](adr/0041-local-server-and-embedded-web-ui.md)).

## The MCP server

- **The test-code filter applies to results, not to the walk.** Caps,
  blind spots and near misses still count test code; a production node
  reached only through test code is marked `via_test_code`
  ([ADR 0082](adr/0082-mcp-server.md)).
- **A freshness check is reused for two seconds.** A tool whose rows carry
  no file state can report `fresh` right after an edit; `checked_at` and
  `reused` show the check's age.
- **Table use is over-reported where code is unbound.** Table access sites
  with no known package appear as "may also be used by" for every table
  their tool can reach: over-reported, never missed.
- **Near-miss lists stop at ten** candidates; the count is then a lower
  bound.
- **Endpoint rows** give the route declaration's file and line, not the
  handler's; the `endpoint` tool gives the handler.
- **In project mode**, `changes` reads one member's change set and does
  not cross links; `link_changes` names a changed link but not what
  changed in it; `find` needs the member's checkout.

# ADR 0079 - Architecture graph: components, links, annotations and chains

**Status:** Accepted (delegated review, 2026-11-19)

## Context

Launch 1 (item 3; `ROADMAP.md`) and milestone M2 of the project plan ask for the 2D architecture graph "with
protocol/auth annotations where the code proves them", from components (L1)
down to the line. The design rounds (the T026 task record (internal))
ended with developer decisions AG1-AG18 (2026-11-18): combination X+ (quiet
map, fixed three-cell glyph protocol | auth | TLS, inspector with sources,
Links tab with a Not proven list, chain drawn in place), React Flow + ELK
(ADR 0046), components from confirmed manifests with kinds only from stored
facts, deploy markers that never create links, libraries folded, tests and
docs hidden, SDK imports as dotted stubs, auth worded "declares" / "as
written" / "documented", TLS and database engine unknown unless a
deploy-config file proves them, and a chain from any node in both
directions with all branches.

The investigation inventory (the T026 task record (internal)) measured what main
can compute: no L1 or L2 node is stored; manifests are scanned files whose
content no extractor states as a package (the Python resolver reads
pyproject and uv workspaces for import roots only, ADR 0072; `cargotoml`
reads Cargo files for sqlx and Diesel); protocol facts for HTTP, Tauri IPC
and SQL exist; auth is stored only as Express route middleware names and
OpenAPI `security`; no deploy configuration is read. Langflow route chains
outgrow any map (210 of 296 still grow at depth 6).

The claim contracts are in `docs/contract/claims/architecture-graph.md` (rows
CM, CK, CV, CD, CE, AP, AA, AT, AE, DC, DA, CH).

## Options considered

### 1. Where components come from

- **A. The indexer states manifests as `package` nodes; the server derives
  components (chosen).** One level-2 `package` node per confirmed manifest
  (identity `package:<manifest path>#<name or directory>`, attributes
  ecosystem, name, workspace members as written, binary markers); unread
  manifests are diagnostics. The server groups packages by directory into
  L1 components, assigns files by nearest enclosing directory and computes
  kinds (CK) and visibility (CV) from stored nodes. The application keeps
  consuming only contract output (AGENTS.md boundary), and kinds change
  with the facts without a re-index.
- B. The server parses manifests itself when a view opens: reads source
  files outside the contract; breaks the external-indexer path.
- C. The indexer emits finished L1 `component` nodes with kinds: kinds
  depend on facts of other extractors and on test-scope configuration, so
  every configuration change would need a re-index, and the indexer would
  make an architecture judgement the server can make from facts.
- D. `contains` edges from each component to every file: 10,233 extra
  edges on langflow for a path rule the server computes in microseconds.

### 2. Deploy configuration

- **A. A deploy-config extractor emitting `deploy_service` (compose) and
  `dockerfile` nodes with parsed attributes, per file, no merging (chosen).**
  Environment values are reduced to URL scheme and host (secrets never
  enter the store). Markers and engine/TLS annotations are server
  computations over these nodes; no edge kind is added, so a marker can
  never become a link (AG3).
- B. Edges from services to components (`builds`): a mapping heuristic
  stored as a fact; build context `.` with `dockerfile: backend/Dockerfile`
  (fastapi) shows how such an edge would point at the wrong component.
- C. No deploy config (AG8 B, AG9 B): rejected by the developer.

### 3. Auth facts

- **A. Attributes on existing nodes (chosen):** FastAPI
  `security_dependencies` and Express `mounted_middleware`, Axum `layers` on
  route nodes; hey-api `security` on client-call nodes. The wording is fixed
  by the server per row (AG7).
- B. New `auth_scheme` nodes and `requires_auth` edges: suggests an
  enforcement graph the code does not prove (langflow declares every scheme
  with `auto_error=False`).

### 4. Chain query

- **A. One traversal shared with change impact (chosen).** ADR 0056's
  breadth-first traversal (`internal/changes` impact) follows incoming
  edges by a relation table with depth, node and fan-out caps. The chain
  needs both directions and its own relation table (CH rows), so the
  traversal moves into a small shared package parameterised by a relation
  table and direction; impact keeps its table and output (its goldens stay
  unchanged), the chain gets CH1-CH12 with the same caps (depth 6, 2,000
  members, fan-out 200) reported when they bind.
- B. Extend the Flow endpoint (ADR 0045): it is route-centred with two
  fixed hops and roles; a table or file start and all-branches traversal
  do not fit its response.
- C. A third traversal in the server: a second copy of cap and tier logic.

### 5. Layout

- **React Flow + ELK in the existing worker (AG2, ADR 0046), no new
  dependency (chosen).** Compound nodes with `INCLUDE_CHILDREN` for
  expansion in place; component positions pinned during a chain by
  re-running layout inside the crossed components only. Measured on
  langflow (Node main thread): overview 117 ms, largest 3-step chain
  115 ms; large expansions use the list (AG14) instead of one node per row.

## Decision

Options 1A, 2A, 3A, 4A and 5 as above, with the claim contracts of
`docs/contract/claims/architecture-graph.md`:

- New node kinds `package` (level 2), `deploy_service` (level 2) and
  `dockerfile` (level 3); new attributes on `route` and `client_call`
  nodes. Contract v1 kinds and attributes are extensible, so
  `contract_version` stays 1; `docs/contract/v1.md` and
  `docs/extract/*.md` list them; each extractor unit bumps the extractor
  version.
- Components, kinds, visibility, links and annotation cells are computed by
  the server from stored facts on each request (like ADR 0066's scope
  labels), never stored.
- HTTP API (additive, `api_version` 0, freshness in every response, 409 on
  an index change between reads as ADR 0042): `GET /api/v0/architecture`
  (components, links with cells and sources, database cards, stubs, Hidden
  counts, Not proven list), `GET /api/v0/architecture/component?id=`
  (expanded content: files, routes, calls, tables, unlinked calls),
  `GET /api/v0/architecture/chain?id=&direction=` (steps, members, edges,
  tiers, evidence, folds, caps, not-modelled lines, hidden test counts).
  `/api/v0/api/flow` gains table roles (AG18).
- Wording: annotation strings never say protected, secure, authenticated,
  encrypted or enforces; a test checks every emitted string.
- Map budget (UI): 3 steps each way, 5 members per step, 40 members; the
  rest folds with "Open in Flow tab".

## Consequences

- No stored data changes meaning; the new kinds and attributes appear after
  a re-index with the new extractor version. Old indexes show components as
  `unknown` with the reason "re-index to read manifests".
- The architecture view's numbers equal the API section's and Flow tab's
  (same edges, same test scope).
- Every annotation family the developer listed has a row; families without
  a fact today render as "?" with a reason until their unit lands, so units
  can merge in any order after AG-B1.
- The shared traversal touches change impact's code path; its goldens and
  `TestChangeClaimContract` must stay unchanged.

## Implementation notes (the implementation)

### AG-B1 component model and API (2026-11-18)

- `internal/extract/packages` states rows CM-M1..CM-M5, CM-W1 and CM-U1
  (`docs/extract/packages.md`); it reads Cargo and pyproject TOML with the
  shared `cargotoml` reader and finds `setup.py` calls in the cached Python
  facts (no execution). Package nodes join the node list after every other
  linker, so no linker reads them. Extractor version bumped.
- `internal/server/architecture.go` and `architecture_links.go` serve
  `GET /api/v0/architecture` and `/api/v0/architecture/component`
  (`docs/cli/serve.md`). Code tables `componentRows`, `linkRows` and
  `annotationRows` list the task file's rows in order; rows whose facts a
  later unit adds (AA3-AA6, AT2, AE2) are marked pending and stay on their
  default rows.
- O1 (a): CK6's closed generator list plus `architecture.hide` in
  `graphvein.yaml` (new row CV4, `docs/config.md`).
- CV1 reads the component's files other than its manifests (a manifest is
  never test scope, so "every file" would never hold).
- An index with no package node and no `component_manifest_unread`
  diagnostic (an older index, an external indexer) shows every manifest
  directory as unread rather than guessing.
- Shape the deploy-config unit (AG-B3) must emit for CD1 and CD3, read
  today only from crafted test snapshots: a `dockerfile` node whose `file`
  is the Dockerfile (CD1 compares its directory with the component's), and
  `deploy_service` nodes whose attributes `build_dockerfile` and
  `build_context` hold the resolved repository-relative path, absent when
  not given or interpolated. AG-B3 may rename them; then CD3's reader in
  `serviceComponent` changes with it.
- Pending gaps: CED counts only the edge kinds the view reads; the "may
  also use" count reads every function, method and file node per request.

### AG-B2 chain query and API (2026-11-18)

- `internal/traverse` holds the breadth-first walk, the relation-table type
  (`traverse.Rule`, matched by `traverse.Match`) and the caps (depth 6,
  2,000 reached nodes, fan-out 200). Change impact (`internal/changes`)
  runs on it with its own I-row table (its goldens and
  `TestChangeClaimContract` unchanged); the chain runs on it with the CH
  tables `chainDown` and `chainUp` (`internal/server/architecture_chain.go`,
  `TestChainContract`). The walk reports every binding cap per step with
  the number of nodes it kept out.
- `GET /api/v0/architecture/chain` (`docs/cli/serve.md`): steps with every
  member within the caps, every followed edge between members (all
  branches, one per stored edge kind), CH11 end lines, CH10 hidden counts,
  CHD counts, and the map budget as a per-step folding hint (members whose
  parent is drawn first); the lists are never cut by the budget.
- A file start walks from all its nodes at once (step 0); a file reached as
  a step (a module-level table accessor, an Angular template) is followed
  only through the rows that apply to file nodes (CH6, CH8), not expanded
  to its symbols, which no edge reaches. A component start walks the
  `/architecture` links (CE1-CE4 and SQL links to database cards).
- Edges the other direction's table follows (the edge back towards the
  start) are not counted under CHD.
- CH11's named dependency-injection steps wait for AG-B4a's
  `security_dependencies`; the generic end line applies meanwhile.
  AG-B5's `passes_function` is followed by CH8 since that unit (see
  below); stored `renders` is followed by CH8 too.
- AG-B1's pending "may also use" read: `store.Query.Attribute` (both
  backends: a `LIKE` prefilter on the payload text, then the exact key test
  in Go; `TestNodesAttribute`) reads only the owners holding
  `table_access_unknown`. Langflow overview 2.1-2.7 s -> 0.80-0.98 s.

### AG-B4a FastAPI and hey-api auth (2026-11-18)

- `internal/extract/routes/fastapi_security.go` states DA-F1..DA-F3 and
  DAD as the route attributes `security_dependencies` and
  `security_dependencies_unknown` (`docs/extract/auth.md`, code table
  `routes.FastAPIAuthContract`, `TestAuthFastAPIContract`, fixture
  `auth-fastapi`); `internal/extract/clients` states DA-H1 as the
  client-call attribute `security` (`clients.HeyAPIAuthContract`,
  `TestAuthHeyAPIContract`, fixture `auth-heyapi`). Order and choice follow
  FastAPI 0.161.1's `get_dependant`/`analyze_param`; chains come from the
  existing mount graph (a Starlette mount starts a new chain; dependencies
  on some mount chains only are unknown).
- New cached Python syntax (gate DG: files importing fastapi keep
  dependency parameters and `Annotated` aliases; constructor keywords
  `dependencies`, `auto_error`, `scheme_name` keep their values; route and
  include lists keep call spans): extractors-50 kept (one number per
  stack, this stack not merged). hey-api needs no cache change.
- Contract rows changed: DA-F3 adds `OAuth2` (the base the documented
  OAuth2 subclasses extend) and refuses positional, `**kwargs` and
  repository-`__init__` settings as `unknown`; AA4's value is "generated
  client documents <scheme>", not "client sends": hey-api adds credentials
  only when the application configures `auth`, which is not read.
- Server: AA3 and AA4 are no longer pending (`declared`; "documented by
  the generated client, as written in <file>"; state `as_written` /
  `documented`, never `proven`). CH11 names the injection from the stored
  chains: upstream "FastAPI dependency injection into n route(s) as
  declared in security_dependencies"; downstream a route's "dependencies
  declared, not followed as calls". No new edge kind: the dependency steps
  stay attributes, so change impact and the chain walk are unchanged.
- Oracles (opt-in): FastAPI's own `app.openapi()` on the fixture apps
  (`scripts/oracle/fastapi_security.sh`, 24 operations agree, 0 disagree)
  and `@hey-api/openapi-ts` 0.99.0 regenerating the fixture client
  (`scripts/oracle/heyapi_security.sh`, byte-equal output, 5 calls agree,
  0 disagree).

### AG-B4b Express and Axum auth (2026-11-18)

- `internal/extract/routes/express_auth.go` states DA-E1, DA-E2 and DAD as
  the Express route attributes `mounted_middleware`,
  `mounted_middleware_unknown` and `middleware_declares`;
  `internal/extract/routes/axum_auth.go` states DA-X1, DA-X2 and DAD as the
  Axum route attributes `layers` and `layers_unknown`
  (`docs/extract/auth.md`, code tables `routes.ExpressAuthContract` and
  `routes.AxumAuthContract`, `TestAuthExpressContract`,
  `TestAuthAxumContract`, fixtures `auth-express` and `auth-axum`).
  Composition runs at link time on the existing mount graphs; no cached
  syntax changes (Express escapes read the call syntax every TypeScript
  file already caches), so extractors-50 is kept.
- Semantics read in Express 4.21.1 / router 2.2.0 (Express 5.1.0): stack
  order, `end: false` path matching at a segment boundary, case-insensitive
  by default, mounted routers and apps inherit earlier matching layers; and
  in axum 0.7.9 / 0.8.8 `path_router.rs`: `layer` and `route_layer` map
  over the routes held at the call, `nest`/`merge` move layered endpoints.
- Contract rows changed: DA-E1 and DA-X1 are refined with their refusal
  lists; DA-E2 (passport, express-jwt, express-oauth2-jwt-bearer,
  express-openid-connect) and DA-X2 (tower-http
  `ValidateRequestHeaderLayer::bearer/basic`, `axum::middleware::from_fn`,
  axum-login `login_required!`) are new rows for the closed,
  source-verified factory lists.
- Server: AA5 and AA6 are no longer pending (state `as_written`): AA5
  "middleware <name>, mounted at <prefix>", AA6 "<method> <expression>"
  (expression capped at 60 characters in the cell), each with "declares
  <library> (<detail>)" or "middleware function <f>" from the closed lists
  and "... ? (n unread forms)" for unknown entries; AA1 adds the route
  arguments' declares.
- Oracle (opt-in): the fixture apps served by Express 4.21.1 and 5.1.0
  (`scripts/oracle/express_middleware.sh`): 16 requests per version, 7
  agree exactly, 9 within unknown entries, 0 disagree. Axum: mapped.

### AG-B3 deploy-config extractor (2026-11-18)

- `internal/extract/deploy` states rows DC-S1..DC-F1 (`docs/extract/deploy.md`)
  at link time from the retained sources, per file, with the YAML reader
  already in `go.mod` (`go.yaml.in/yaml/v3`) and a small Dockerfile reader
  (continuations, comments, `# escape=`, heredocs); no new dependency, no
  cache payload, no own version bump (extractors-50 is the stack's number).
  Deploy nodes join the node list with the package nodes, after every
  linker. The shape AG-B1 recorded is kept: `build_context` and
  `build_dockerfile` are resolved repository-relative paths, absent when
  not given (the Dockerfile) or not resolvable; a `dockerfile` node's
  `file` is the Dockerfile.
- Server (`internal/server/architecture_deploy.go`): AT2 attaches each
  `tls=true` router of a CD3-mapped service to that component's `ingress`
  list ("ingress TLS at router <r> as configured in <file> (service <s>)");
  an HTTP link into the component keeps "TLS ?" and gains a pointer in its
  source (O3 (a)); a TLS router on an unmapped service is a Not proven
  entry. AE2 reads, per accessor component, the database image services of
  the same compose file that its mapped service names by `depends_on` or a
  URL host: one value without stored dialects is the cell (state
  `as_configured`); with stored dialects that prove nothing, or with AE1
  agreeing, the facts are evidence lines; disagreement (between facts or
  with AE1) is AED with every fact listed. Database services nobody ties
  are evidence lines on every card ("not tied to this card"), or Not proven
  entries when there is no card.
- Oracle: compose-go v2.16.2 and BuildKit's parser v0.34.2 in a scratch
  module (`scripts/oracle/deploy_config.sh`, `TestDeployOracle`).
- Revision R1 (decisions AG-B3-1 (b), AG-B3-2 (b)): new row
  CD4 maps a service with neither `build` nor `image` by name to the one
  component its same-name services in the other compose files of its
  directory map to by CD3, with the basis shown on the marker and the
  ingress: "merged ... by Compose's default file discovery" for the
  directory's default file and override (compose-go's discovery lists,
  `deploy.DefaultPair`), else "assumption: <file> is combined with
  <files>". Facts stay per file in the store; the combination is a server
  reading with its basis visible. AE2 reads CD3 mappings only. CD3 no
  longer falls back to the context when a written Dockerfile lies in no
  component directory; unmapped services carry their reason.

### AG-C1 UI map (2026-11-18)

- `web/src/sections/architecture/`: `model.ts` (cards and lines from the
  answer and the Show toggles; links bundled per unordered card pair, a
  differing cell merged to "mixed" with counts from the links' own cells;
  markers, ghosts and stubs never become link lines), `layout.ts` (ELK
  layered DOWN in the Flow tab's worker; chips are ELK centre labels moved
  onto their line unless that covers a card or chip), `MapView.tsx` (the
  lazy chunk), `Inspector.tsx`, `LinksTab.tsx`, `rows.ts` (expanded
  content, list above 12 rows, AG14), `params.ts` (hash state; `chain=` is
  reserved for AG-C2).
- AG-C2 hooks: the Inspector's `chain` slot, MapView's chain comment and
  the reserved hash parameter. AG-C3 is the API section's Flow tab.
- Label table rows 31-41 (22-32 before the T022 stack) (`web/src/lib/labels.ts`); the plain style shows
  the server's words, the technical style adds kinds, families and
  contract rows raw.
- Evidence IDE links join link members with `/architecture/component`
  answers of both ends (members carry no location). That endpoint returns
  no IDE status object, so the inspector's IDE buttons are built without
  one (the editor caveat reads as for an unknown source).
- ELK timing on langflow in headless Chromium (worker, 1440 px): overview
  with every toggle on 293-382 ms (median 305 ms over 5 loads, worker start
  included), default overview 290-403 ms; gate 500 ms.

### AG-C2 UI chain, inspector, Links tab (2026-11-18)

- `chain.ts` (pure): numbers the answer's steps top to bottom over both
  directions (furthest upstream step 1, the start, then the down steps);
  `buildChainMap` opens each card a drawn member crosses (a `container`
  node) to its members only, puts a table in its database card (the chain
  member's new `database` field), folds every member the server's budget
  hint leaves out into "+ n more at step k" per card (members of a card the
  Show toggles hide fold at the root, counted as hidden), drops the
  overview lines touching an opened card and draws the chain's own lines:
  one per member pair inside a card, one bundle per card pair between
  cards (AG15). A component start (CH9) numbers the cards and focuses the
  lines of its CE links instead.
- Layout: ELK (layered DOWN, the page worker) places the top-level cards
  with each opened card at its inner size and one stand-in edge per pair of
  opened cards joined by a chain line; inside a card the members sit in
  one column per step, left to right (X+-1); chain lines are routed
  orthogonally afterwards (`layout.ts` `innerLayouts`, `chainRoute`). A
  single ELK hierarchy run (INCLUDE_CHILDREN) was tried first: it lays every
  card out in the root's direction, one long column per chain, unreadable
  at 1440 px. The other cards are laid out again rather than pinned
  (follow-up).
- `ChainPanel.tsx` is the inspector's top block (the C1 `chain` slot):
  per step the stored edges into it (wording and tier, counted), members
  with source, CH11 end lines, CH12 cap notes, CH10 hidden counts and map
  folds; 25 members per step before "Show all" (not virtualised; the caps
  bound a direction to 2,000). One selection (hash `at`) for map and list,
  with a code peek; arrow keys walk the chain, Esc clears it. "Open in Flow
  tab" opens the start route or command, else the selected or nearest one;
  a chain with none (a component chain) has the button disabled. "Show in
  Links tab" filters the ledger to the links the chain crosses (member
  pairs for HTTP/IPC, card pairs for SQL and in-process); the Links tab
  also downloads its rows as CSV.
- Selecting a component card or an expanded row draws its chain; a
  database, libraries or stub card has none (the chain endpoint takes
  node identities and component directories only).
- Not proven fixes: compose services and unmapped ingress grouped in one
  collapsed item; a service whose database image AE2 ties to a card (new
  `not_proven[].database`), and same-name services of its directory whose
  reason says they write no image or build of their own, are noted, not
  listed; "may also use" names the component and the card (the set added
  when two cards share a name); an entry of a component hidden by default
  says so. Cards show the AT2 ingress ("TLS as configured"), the inspector
  each marker's `basis` and each ingress's router settings as written.
- Map budget revision (check): a fold never hides a single
  member. A step with one member left over draws it (within the per-step
  and total budget it may exceed them by that one), and a one-member step
  past the third is drawn when its parent is; fastapi `GET /api/v1/items/`
  now draws all 7 steps (ItemsTableContent was folded as up step 4);
  langflow `PATCH /api/v1/flows/{flow_id}` draws 15 (was 12), 119 fold.
  Goldens `chain-route`, `chain-route-all`, `chain-table`, `chain-inner`
  change accordingly; `TestMapBudgetSingleMember`.
- Server additions (additive API fields, goldens regenerated): chain
  members `database` (tables) and `full_path` (routes, served path);
  `not_proven[]` `service`, `file`, `reason`, `database`.
- Label table rows 42-53 (33-44 before the T022 stack), both styles.
- ELK timing on langflow in headless Chromium (worker, 1440 px, last
  layout of the page, 5 loads): `PATCH /api/v1/flows/{flow_id}` chain
  66-419 ms (median 196 ms), table `Flow` chain (478 members, 40 drawn)
  111-319 ms (median 231 ms); gate 500 ms.

### AG-C3 Flow tab Tables column (2026-11-18)

- Server: ADR 0045 revision R1 (`internal/server/flow_tables.go`): roles
  `nested` (CH7 downstream at the container's hop) and `table` (CH6, hop 1
  and 2), table edges with the chain's CH6 `wording`, `may_also_use`
  (stored `table_access_unknown` sites of returned handler-side
  functions) and `tables_beyond` (hop-3 counts, never an absence). Every
  existing flow golden gains only `may_also_use: []` and `tables_beyond`;
  the crafted `flowtables` snapshot has its own golden (SQLite and
  Postgres, equal bodies).
- UI: a Tables column (cards with access and tier, "R fact · W 0.9", the
  schema source as sub line), a "Reads / writes" line tier with the
  server's wording, nested functions badged in their container's column,
  two state boxes no line touches ("may also use", "+ n at hop 3" or "No
  other table at hop 3" with "deeper calls not read"), and with 1 hop the
  hidden hop-2 tables counted in the summary. ELK places each table level
  with its accessors (langflow: six tables over about 1,000 px), so
  `packTables` stacks the column after layout and reroutes each table line
  from where ELK's route enters the last gap.
- "Open in Flow tab" adds `back=` (the Architecture hash); the Flow tab
  then starts on the widest column range ending at Tables that fits, level
  with the Tables column, reveals the selected member (a member folded
  into a card selects that card) and links back.
- Corpus (extractors-50 on this branch, scratch SQLite): fastapi
  `GET /api/v1/items/`: `read_items` builds a query on `Item` (reads,
  fact), 4 unbound sites (`statement_not_traced`), hop 3 no other table.
  Langflow `PATCH /api/v1/flows/{flow_id}`: nested `update_flow.operation`
  and `update_flow.update_attempt` at hop 0; six tables (`Flow` read and
  written, fact; `MCPServer` read fact and may write 0.9; `Folder`,
  `Deployment`, `FlowVersion`, `FlowVersionDeploymentAttachment` read),
  3 unbound sites, hop 3 no other table and 4 unbound sites; 68 nodes, 87
  edges, no truncation; response 34-45 ms.

### AG-B5 TypeScript bridges (2026-11-18)

- Decision O4 (b) as a claim contract of its own,
  `docs/lang/typescript-bridges.md` (rows JR1-JR3, PF1-PF3, RR1-RR5, one
  default branch D; `TestTypeScriptBridgesContract`, fixture `ts-bridges`
  with golden). Syntax is collected in the structural walk and cached in
  `TypeScriptFacts.Bridges`; `typescript.LinkBridges` resolves it on every
  link (calls' scope chain, then the Precise import graph), after the
  Angular linker, so no HTTP consumer reads the new nodes. Stack version
  stays `extractors-50`.
- Edge kinds: `renders` (reused; React's element owner, not mounting),
  `passes_function` (new: a function passed as a value, call not
  modelled); react-router routes reuse `ui_route` (`framework:
  "react-router"`, Angular naming) with `routes_to` (Component, element
  tags, lazy Component) and `passes_function` (loader, action). No edge
  attributes: the route node's `component`/`element`/`loader`/`action`
  attributes name the role.
- react-router semantics read from react-router 7.13.0 and 7.18.2
  (`loadLazyRoute`, `mapRouteProperties`, `createRoutesFromChildren`):
  static keys win over lazy ones; a Component (static or possibly lazy)
  replaces `element`, so those element tags are D.
- Shadowing is stricter than the calls scope (destructured parameters,
  anonymous callback parameters, body declarations before use), so a
  bridge is never drawn through a local binding.
- CH8 follows `passes_function` ("passed as callback, call not modelled" /
  "passed as callback by, call not modelled"); `renders` and `routes_to`
  were already followed. The CH11 end line text is unchanged.
- Oracle `scripts/oracle/ts_bridges/oracle.cjs` (typescript@6.0.3):
  fixture 49/49, react 98/98, fastapi frontend 469/469, langflow frontend
  4,526/4,526 claimed evidence sites agree (44 in files outside the
  compiled programs not compared); under-claims: JSX tags the compiler
  resolves to a repository function or class and Graphvein does not claim
  0/58, 0/428, 8/3,509.
- Not done (follow-ups): change impact has no relation row for
  `passes_function` or for React `renders` into functions; change
  classification puts `passes_function` in D (all three done in AG-F
  below); react-router 7.5 object-form
  `lazy`, `ErrorBoundary`/`HydrateFallback`, TanStack Router route trees
  are not read as routes (TanStack `component: X` is PF2 only).

### AG-F T026 fix round (2026-11-18)

- Change impact (ADR 0056, T019 Revision 7, `ContractVersion` `T019-r7`):
  row C17 "React UI" (react-router `ui_route` nodes, React `renders`,
  `routes_to` and `passes_function` from a react-router route; a React
  route was listed under "Angular UI"), other `passes_function` edges C16
  (were D); I14 follows `renders` and `routes_to` into a function
  component, I15 follows `passes_function` ("passes this as callback,
  call not modelled", always inferred). Fixture `change-impact` gains a
  small React app (`react/`).
- Flow tab: the table chip ("Reads · may write" / technical "R fact · W
  0.9") and table line texts read through the label table (rows 54, 55);
  table-line labels on one row are separated (one label per text, other
  texts stacked above).
- Express DA-E3: `use` calls in a module function that receives the app
  or router as a parameter, read at its only top-level unguarded call with
  that object (`configureMiddlewares({ app })`); conditions in
  `docs/extract/auth.md`. Body uses under a branch or after a return or
  throw are unknown entries. Oracle: 22 requests per Express version, 8
  exact, 14 within unknown entries, 0 disagree.
- Axum DA-X3 (`get(h).layer(e)`: wraps the methods chained before it,
  axum 0.7.9 and 0.8.8 `method_routing.rs`) and DA-X4 (a tower
  `ServiceBuilder::new()` stack's inner layers in written order, outermost
  first; tower 0.4.11, 0.5.2, 0.5.3 and tower-http 0.6.8 builder methods,
  any other method leaves the stack unnamed). AA6 renders both.

### Review fix round (2026-11-18)

Fixes of the independent review (the T026 task record (internal)), each as a
class rule with its contract row, docs and test:

- A1 Express earlier route handlers: a route registration is a layer of
  its router's stack. DA-E1 gains the escape "earlier route handler
  <expr> (<METHOD> <path>) may run first" for an earlier `all`,
  same-method (GET for HEAD, any for `all`) or `route(p)` layer whose
  path is not proven disjoint (a literal segment differing
  case-insensitively before any wildcard, optional or group syntax, or
  plain paths of different segment counts), and "router <r> mounted
  earlier has route handler ..." for an earlier sibling router. No
  order-and-path proof states such a handler as a fact. DA-E4:
  `route(p).all(f...)` chained before a method leads that method's route
  middleware and is no `ANY` route. AA5 reads "earlier route handlers ?
  (n route layers may run first)". The Express oracle plan gains `more`
  modules derived from the Express documentation's `app.all` and `Route`
  examples (Express 4 `*`, Express 5 `*splat` in their own modules): 32
  requests per version, 13 exact, 19 within unknown entries, 0 disagree.
- B4 / DA-E5: a `use` function of `Function.length` 4 or more (cached
  parameter default/rest flags and function scope offsets, extractor
  version `extractors-51` on the branch, renumbered to `extractors-46` at
  the gate merge and to `extractors-48` when stacked on T023 and T022) is listed in `mounted_error_handlers`, never as mounted
  middleware.
- B1: DC-T1 reads `traefik.enable`, DC-T2 `--providers.docker.exposedbydefault`;
  AT2 states no TLS for a service Traefik's Docker provider ignores and
  lists its routers as Not proven (`ingress_ignored`). Traefik static
  configuration files stay unread.
- B2: DC-S1 `dotenv_compose_file` (only that a `COMPOSE_FILE` line exists
  in the compose directory's `.env`; no value stored); CD4 then uses the
  assumption wording. Mapped from compose-go's `WithConfigFileEnv` after
  `WithDotEnv`; the deploy oracle's discovery does not load dotenv files.
- B3: HTTP link members carry `full_path` (the chain's served path); the
  Links tab, inspector and CSV show it, the pattern as written secondary.
- C1: CH11's text says what is followed ("no stored caller and no
  resolved bridge; ..."). C7: downstream, a leaf function in a file with an
  `unresolved_calls` diagnostic ends with "? not modelled: no stored
  callee; <file> has n calls the indexer could not resolve, ...".
- C2: database accessors count `builds` ("builds a query on", CH6's
  wording) apart from reads and writes; the Flow tab's table chip and the
  inspector read it through the label table (row 56 `dbAccess`).
- C3: Axum layer expressions drop Rust comments. C4: change-impact paths
  inferred only by I15 or a gap have `confidence: null` (T019 Revision 7).
  C5: the card's TLS chip keeps the CD4 basis ("assumed" / "merged").
  C6: `wrapper` calls (HTTP and IPC) are neither in the ghost nor in the
  call counts (`wrapper_calls`). C8: the tied note separates same-name
  services without an image. C9: AE1 names its source ("migration
  dialect", "ORM model dialect (<tool>)"). C10: CK7, the unassigned root's
  kind `unassigned_files`, never folded.

## Known gaps

- Go and Next.js corpora are not measured (extractors not on main).
- Compose merging (`compose.yml` + `compose.override.yml`), `extends` and
  `include`, nginx, CDK, Kubernetes and Helm are not read (DCD).
- Docs components depend on open point O1; SDK stub granularity on O2; TLS
  placement on O3. JSX tags and callbacks the code does not resolve stay
  unmodelled (AG-B5 rows D).
- Test scope does not cover e2e or fuzz crates (kftray-e2e, fuzz stay
  visible as binaries).
- The map budget and the list threshold are not user-tested; ELK timing was
  measured in Node, not in the browser worker.
- Tauri capabilities (IPC access control) and Starlette/Nest/guard-style
  auth are not modelled (DAD).
- FastAPI: class dependencies, callable instances, inline scheme instances,
  PEP 695 `type` aliases and dependency functions in files that do not
  import fastapi are unknown entries (AG-B4a); an unread form can hide a
  declared scheme (`auth ?`) but never add one.
- Express: middleware registered by a function that receives the app or
  router, inside other function bodies, or behind a dynamic path is an
  unknown entry; across files, order is read only between top-level
  statements (a circular import could reorder them). Axum: layers outside
  the router (a tower `ServiceBuilder` or a function wrapping the served
  router) and `Handler::layer` are not read; a `ServiceBuilder` stack with
  a method no list names is stated as written only (AG-F reads stacks and
  method-router layers); neither framework ever states "no middleware"
  (AG-B4b).

## TOP-2 of 2026-11-18: server actions, project members, deterministic Not proven

A contract change the developer asked for (rows added to
`docs/contract/claims/architecture-graph.md`; code and tables stay 1:1):

- **CE7 / AP5 (server actions).** `calls_action` from an action call in one
  component to a Next.js server action in another is a `server_action`
  link (members named by the action); inside one component it is counted
  under "not drawn". Next.js sends an action as a POST to the page that
  renders it, so the protocol is "server action" (proven by
  `calls_action`), auth stays `auth ?` (checks inside the action are not
  read) and TLS `TLS ?` (ATD), never "n/a": a direct call from server code
  may run in process, which the protocol source says, but nothing proves
  the same process. Corpus: chatbot-ui's 4 `calls_action` edges are inside
  its one app (not drawn); fastapi, langflow and owncast unchanged.
- **CE8 / CH13 (project members).** Under `serve --project` a member's
  calls the stored link pass links into another member are no longer in
  the "not linked" ghost: they are a CE8 HTTP link to a muted card
  "<repo> (other repository)", with the stored assumption texts, counted
  per component as linked across repositories beside the repository's own
  count. Incoming links draw the other member's card as the source. A
  chain crosses into that card as far as the stored link and ends with
  "continues in <repo>"; the other repository is never walked. Auth reads
  the route's stored facts in whichever member holds it (documented
  security only for this repository's routes). react + express: react
  shows 27 calls linked across repositories to express (repository alone:
  28 not linked, the 28th is dynamic and stays in the ghost); express shows
  27 calls of react into @conduit/article (18) and @conduit/user (9).
  Single-repository answers are unchanged (no `project` field).
- **Not proven order.** The "not linked" entries follow the sorted ghosts
  (component, family), so two requests return one body (owncast's two
  entries were in map order).
- **R1 (operations).** A stored `documents_route` project link resolves
  CE5's "documented, no route" the same way: the operation is a
  "documents" row with the other member's card (row CE8) and counts as
  linked across repositories beside the repository's own count; react's
  19 operations now document express routes and leave Not proven. The
  links line and the project heading go through the label table (rows
  57-58). The pass stores no other cross-repository kind, so no other
  per-repository unknown is resolved by it.
## Acceptance notes (2026-11-19)

- Results: combined gate CI run 44533180619 on 0ef4343 (`[ci windows]`): postgres raw log 0 FAIL, 226 `/postgres` PASS; Windows 0 FAIL. Full check on the stacked head: 0 FAIL, 225 `/postgres` PASS; check-web ok; UI 3.18 MiB (size limit raised to 3.5 MiB in AG-C1 with the reason recorded).
- Revisions: three design/mockup rounds with the developer (X+ chosen; chains from any node, both directions, all branches); AG-B1 components and API; AG-B2 chain query on a traversal shared with change impact (langflow overview 2.5 s -> 0.85 s); AG-B3 deploy config (+R1 CD4 overlays, no context fallback); AG-B4a FastAPI/hey-api auth; AG-B4b Express/Axum auth; AG-B5 TypeScript bridges (`renders`, `passes_function`, react-router routes); AG-C1 map; AG-C2 chain view and inspector; AG-C3 Flow Tables column (ADR 0045 R1); AG-I/AG-F integration and fix round (T019 Revision 7 rows C16/C17, I14/I15 after the stack renumbering); independent-review fixes (Express earlier route layers, Traefik enable, COMPOSE_FILE, full paths, labels), confirmed by the reviewer; stack follow-ups CE7 server actions, CE8/CH13 project mode.
- Coverage: fastapi 4 components, HTTP 23 (17 declare OAuth2PasswordBearer), SQL 21, postgres 18 as configured, backend TLS "as configured, assumed"; kftray 17 components, IPC 98; langflow 31 components, HTTP 162, SQL 564 (118/118 operations agree with its committed spec); composite 12 components, HTTP 27; express 19/19 operations agree with its swagger file; axum/axum-ddd layers named; owncast and chatbot-ui on the map after stacking.
- Verification labels: oracle-compared: compose-go/BuildKit (92 corpus files), FastAPI `app.openapi()` (24 ops), hey-api regeneration, Express 4/5 middleware traces, TypeScript bridges (react 98, fastapi 469, langflow 4,526). Mapped only (source reading): CD3/CD4 component mapping, Traefik labels and static flags (AT2), Axum layers (AA6), AE2 database engine, the COMPOSE_FILE dotenv rule. Auth wording never stronger than declares / as written / documented; TLS only "as configured" per hop.
- Assumptions shown in the UI: CD4 "combined with" basis; TLS chip "as configured, assumed"; "earlier route handlers ?" as a conservative over-report.
- Known gaps: Compose `extends`/`include`; nginx/CDK/Kubernetes/Helm; Traefik static files and swarm `deploy.labels`; Tauri capabilities; Starlette/Nest guards; FastAPI class/callable dependencies; Axum `Handler::layer`; the express card in project mode counts incoming calls but not operations.
- After v1: react-router object-form `lazy` and ErrorBoundary routes; TanStack route trees; map budget user testing; in-browser ELK timing.

## Revision (T042): chain depth up to 10, default 6

Status of this revision: Accepted (delegated review, 2026-11-28; the
decisions above keep their status).

- Context: the chain shares the change impact walk and its caps
  (`internal/traverse`); its default depth was the cap itself, 6. A
  route-to-frontend chain through three helper functions needs 7 steps.
- Decision: the shared depth cap is 10 (ADR 0056, revision T042), and the
  chain default stops following it: `traverse.ChainDefaultDepth = 6` is
  the default of `GET /api/v0/architecture/chain` and the MCP `chain`
  tool. CH12 reads "the requested depth (default 6, at most 10)"; the
  answer's `caps.depth` reports the largest depth a request may name (10),
  its `depth` the walked depth. In MCP a chain depth bound's `limit` is
  the walked depth (it showed `caps.depth`, which was wrong whenever the
  request named a smaller depth).
- The evidence chain panel gets a depth switch (1 to 10) next to the
  direction switch, in the same style as the Impact tab's; the hash
  carries `depth`, and the starting depth follows ADR 0078's precedence
  (browser, `ui.chain_depth`, 6).
- Consequences: default chains are unchanged apart from the reported
  `caps.depth`; deeper chains are opt-in. The map budget (3 steps, 5 per
  step, 40 in total) is unchanged, so a deeper chain folds more members on
  the map while the inspector still lists every member within the caps.
- Known gaps: as in ADR 0056's T042 revision; the map draws at most the
  budget whatever the depth.

## Revision (T067): the libraries band, SDK chips on cards

Status of this revision: Accepted (delegated review, 2026-11-28; the
decisions above keep their status). Design record and mockups: task T062
(variant A with every recommended default, developer decision
2026-11-27).

- Context: AG4 folded every CV3 library (kind `package` only) into one
  "Workspace libraries (n)" card, hidden by default. On pdfcraft that
  left 0 of 127 links on the default map (82 in-process calls and imports
  from 8 cards into 13 libraries, 109 links between libraries); with
  Libraries on, the folded card took the map to 23 % zoom (names at
  3.4 px) and still said only "they connect to libraries", not to which.
  AG5's dotted stub cards added one card per component to every map.
- Decision AG4 (revised): Libraries is on by default. The map has two
  lanes: the cards (ELK, as before) on top, and the libraries band, a
  fixed grid below them, holding each CV3 library as a chip: its name
  without the prefix every library shares (at least three libraries, the
  footer names it), the number of components a stored link reaches it
  from, the directory in the tooltip or on a second line (names repeat,
  or at most 12 chips); a root package reads "<name> (root ./)". Band
  rows: *Used by a card* (ordered under the cards using each library),
  *Used by libraries, level n* (one level below the deepest library using
  it; a cycle stops at its first repeat), *No stored user* (linked, but no
  stored link ends at it), *No stored link* (never worded "unused": P3).
  A row shows at most 24 chips (four lines of six); the rest fold into one
  "+ n more libraries" chip that opens the band. Every card-library pair
  is a line (AG15 bundling unchanged), routed down the column gutters, and
  the card carries one line chip summing them; library-library pairs are
  counted on the band and the inspector and drawn while one of their ends
  is selected. Selecting a library lights its users and what it uses and
  lists both with counts; it starts no chain (a card still does, CH9). The
  Libraries toggle off returns to hiding them, counted in the Hidden note.
  Above six cards the map uses 180 px cards with the kind on the second
  line.
- Decision AG5 (drawing only): the SDK stub is a dashed "SDK n" chip on
  its card, the stub's text and protocol cell in the tooltip and the
  inspector; the fact, its text and "never a protocol" are unchanged. The
  SDK imports toggle hides the chips, counted as before.
- Consequences: no server, API, contract or golden change; the API's
  `visibility: folded` and `libraries` block are read as before. The claim
  contract's CV3, CE4 and CE6 claim cells name the new drawing. The hash
  writes `show=nolibraries` for the toggle off; an old link's
  `show=libraries` still reads as on. The minimap counts cards, not band
  chips. A chain drawn in place opens a library's chip as a card in the
  card lane like any other card.
- Known gaps: the band cap is per row, so a band with many rows can still
  be tall (artcraft not measured, P4); library-library lines use simple
  curves inside the band and can cross chips; the band is not part of the
  ELK layout, so card-card lines never route around it (they never reach
  it: the band sits below every card). Classification questions P1-P4 of
  the design record are unchanged.

## Revision (T072): band lines on a bus, selection marks on the chips

Status of this revision: Accepted (delegated review, 2026-11-28; the
decisions above keep their status). Design review: task T071 (declutter
review, recommendations V1 and V3 with their defaults, developer decision
2026-11-27).

- Context: the T067 band drew 18 card-library lines on pdfcraft with 21
  crossing points and 15 passes of a line under a chip it does not end
  at (11 lines in one 40 px cell); a selected library drew its library
  links as curves across the band (44 lines, the lit ones under 45
  chips), and dimming could not help because the popular libraries touch
  most of the band.
- Decision AG4 (drawing of the band's lines, revised): every card-library
  pair is still a line, now routed on a bus: one trunk per card from its
  line chip (along a gutter no card stands in when a card stands below)
  to the card's own track between the card lane and the band, right
  angles throughout, one port per card on each chip. Tracks are ordered
  to cross least; tracks whose spans are apart share a level. When the
  card row wraps, its top chip line holds the libraries of the cards with
  the fewest libraries (a straight drop under their card), the rest fill
  the lines below and are fed by an inner bus between the first and
  second chip line; deeper chips (and the "+ n more libraries" chip when
  a card's line ends there) are entered from the side along a channel in
  a column gap, one slot per card. Chip columns are 20 px apart where the
  same number of chips per line still fits. Resting lines are a lighter
  solid grey; the lines of the card or chip under the pointer or holding
  keyboard focus light. A band no card line reaches is placed as before.
- Decision (selection, revises "drawn while one of their ends is
  selected"): library-library links are marked on the chips, not drawn.
  While a card or library is selected, each chip a stored link joins to
  it shows the stored count of those links with the direction (↑ it uses
  the selection, ↓ the selection uses it) instead of the number of
  components using it, and the band footer names it ("selected: ↑ n use
  it · ↓ it uses m · stored counts"). A library-library line selected on
  its own (inspector, Links tab) is still drawn. Unrelated chips dim to
  25 % (35 %), unrelated band lines to 10 % (18 %); cards keep 35 %.
- Chain drawn in place: chain line chips are painted above their lines;
  the inspector says an end note once per step when every member of the
  step carries the identical note ("Both: ...", "All n: ...").
- Consequences: no server, API, contract or golden change. pdfcraft at
  1440x900: 9 crossing points (all right angles between trunks), 0
  passes under a chip, fit zoom 0.941 (0.959), card names 13.6 px
  (13.9 px), 10.9 px at 900x800 (11.1 px); with `engine` selected 2 lit
  lines and 26 marked chips instead of 28 lit curves. The step badges on
  band chips are unchanged.
- Known gaps: the trunks of different cards still cross at right angles
  (9 points on pdfcraft); a gutter search falls back to the gutter right
  of the card when no clear one exists (not seen on a packed lane); the
  quiet variant (lines only under the pointer) of the review was not
  built.

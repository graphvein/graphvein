# Claim contract: architecture graph

The claim contract tables of the architecture graph (ADR 0079): components, component
links, annotations, deploy config, auth extractors and the chain. They were fixed when
the feature was designed (task T026, internal record) and are copied here verbatim.
The code tables in `internal/server`, `internal/extract/packages`, `internal/extract/deploy`,
`internal/extract/clients` and `internal/extract/routes` are kept 1:1 with these rows by
their contract tests.

## Claim contract: components (rows CM, CK, CV, CD)

The indexer states manifest facts (one `package` node per confirmed
manifest, ADR 0079); the server derives components, kinds and visibility
from stored facts only.

| id | form | claim | refusal / unknown |
|---|---|---|---|
| CM-M1 | scanned `package.json`, a JSON object with a nonempty string `name` | package (ecosystem npm) at its directory; `workspaces`, `bin` recorded | no `name`: not a package (a `workspaces` root without `name` is CM-W1); invalid JSON: CM-U1 |
| CM-M2 | scanned `Cargo.toml` with `[package]` and a string `name`, read with `internal/extract/cargotoml` | package (cargo); `[[bin]]`, `[lib]` recorded | `[workspace]` only: CM-W1; reader refusal: CM-U1 |
| CM-M3 | scanned `pyproject.toml` with a string `[project].name` or `[tool.poetry].name` | package (python); `[project.scripts]` names recorded | tool configuration only (ruff, pytest, ...): not a package; `[tool.uv.workspace]` without a name: CM-W1; reader refusal: CM-U1 |
| CM-M4 | scanned `go.mod` with a `module` directive | package (go module) | no directive: CM-U1 |
| CM-M5 | scanned `setup.py` with a module-level `setup(...)` call imported from `setuptools` or `distutils` (Python `ast`, no execution), or `setup.cfg` with `[metadata] name`, in a directory no CM-M1..M4 package encloses (same directory or an ancestor) | package (python), name only when a string literal | enclosed, or no such call (langflow `initial_setup/setup.py`, `logging/setup.py`): not a package, no diagnostic |
| CM-W1 | workspace root without a package of its own (Cargo virtual manifest, `package.json` with `workspaces` and no `name`, uv workspace root without `[project]`) | not a component; its directory's files go to the next enclosing component or the root (CM-R1) | - |
| CM-U1 | a manifest of CM-M1..M5's file names the reader refuses (invalid JSON, TOML the reader does not accept) | component in state `unread` at that directory, kind unknown; its files stay there (never fall to an ancestor) | diagnostic `component_manifest_unread` (info) |
| CM-A1 | every scanned file | belongs to the nearest enclosing directory with a CM-M1..M5 or CM-U1 component; several manifests in one directory make one component | - |
| CM-R1 | files under no component | the root component "unassigned (n files)"; when the repository root has its own package, that package is the root component | - |
| CK1 | a component holding at least one `route` node without `scope: test` | kind `server`, frameworks listed | - |
| CK2 | ... at least one non-test `ipc_command` | kind `tauri_core` | - |
| CK3 | ... at least one non-test `client_call` or `ipc_call` and no CK1 route | kind `client` | - |
| CK4 | ... at least one `table` node (any schema source) | kind `schema_source` | - |
| CK5 | a CM-M2 package with `src/main.rs`, `[[bin]]` or `src/bin/*.rs`; a CM-M3 package with `[project.scripts]`; a CM-M1 package with `bin`; a CM-M4 module with a stored `package main` file declaring `func main` | kind `binary` | - |
| CK6 | (O1 (a)) a CM-M1 package declaring `@docusaurus/core`, `vitepress`, `@astrojs/starlight` or `nextra` | kind `docs_site` | - |
| CK7 | Review fix: the CM-R1 unassigned root with no CK1..CK6 kind | kind `unassigned_files` (files under no manifest; no role claimed, never folded by CV3) | - |
| CKD | everything else | kind `package` (no role claimed); several kinds may hold at once (langflow base: server, schema_source, binary) | always |
| CV1 | every file of the component other than its manifests is test scope (`internal/testscope`, configuration applied; a manifest is never test scope) | hidden by default, counted in the Hidden note ("tests: n components") | - |
| CV2 | kind `docs_site` | hidden by default, counted ("docs") | - |
| CV3 | kind `package` only (CKD) | visibility `folded`; the map draws it as one chip in the libraries band below the cards, shown by default (the Libraries toggle hides the band, counted in the Hidden note) (AG4, ADR 0079 revision T067; until T067 one folded "Workspace libraries (n)" card, off by default) | - |
| CV4 | (O1 (a) override) the component directory matches an `architecture.hide` pattern of `graphvein.yaml` (tests pattern syntax) | hidden by default, counted ("configured") | - |
| CVD | everything else | shown | always |
| CD1 | a Dockerfile (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`) in the component directory that the deploy-config extractor read (DC-F1) | marker "Dockerfile <path>" | unread Dockerfile: no marker, the gap is listed |
| CD2 | `tauri.conf.json` in the component directory or its `src-tauri` child | marker "Tauri app" | - |
| CD3 | a compose service (DC-S1) with a `build`: when a `dockerfile` is written, its resolved directory is exactly a component directory; else its resolved `build.context` is exactly a component directory | marker "compose service <s> (<file>)" | interpolated context or Dockerfile, image-only service, a written Dockerfile in no component directory (AG-B3-2 (b): no context fallback): no marker, listed as "? not mapped: <reason>" in the Not proven list |
| CD4 | (AG-B3-1 (b)) a compose service with neither `build` nor `image`, whose same-name services in the other compose files of its directory that CD3 maps all map to one component | marker on that component with its basis: "merged with service <s> of <file> by Compose's default file discovery" when the two files are the directory's default file and override file (compose-go `DefaultFileNames` / `DefaultOverrideFileNames`), else "assumption: <file> is combined with <files>"; review fix: the default-discovery wording only when the directory's stored `.env` has no `COMPOSE_FILE` line (DC-S1 `dotenv_compose_file`), else the assumption wording naming that `.env` | no such service, or several components: no marker, "? not mapped: <reason>" |
| CDD | everything else | no marker. A marker never creates a link | always |

## Claim contract: component links (rows CE)

Links bundle stored edges per (source component, target component,
family). Default scope excludes edges whose source is test scope (counted
in the Hidden note; the tests toggle includes them).

| id | form | claim | unknown |
|---|---|---|---|
| CE1 | `calls_route` from a client call in A to a route in B (A ≠ B) | HTTP link A → B: count, tier histogram (the chip shows the weakest), methods | client calls of A with status `dynamic`, `unmatched`, `ambiguous`, `test_only`: a dashed "not linked (n)" ghost per component with counts by status, never a link; review fix: status `wrapper` (a recognized wrapper's forwarding call, whose requests are its call sites) is in neither the ghost nor the call count, counted apart as `wrapper_calls` |
| CE2 | `invokes_command` from an IPC call in A to a command in B | Tauri IPC link A → B | IPC calls without an edge: as CE1's ghost (status `wrapper` excluded as in CE1) |
| CE3 | `reads_table` / `writes_table` from a symbol in A to a table of schema set S | SQL link A → database card S (one card per schema set, tool shown), R and W counts; review fix: an edge whose owner's `table_access` entry has role `builds` counts as B, "builds a query on" (CH6's wording, also on the Flow tab's table chip) | `table_access_unknown` entries of A's symbols: "may also use" count on the card, never a link |
| CE4 | `calls` and `imports` between symbols or files of different components (no `external` target) | in-process link, both directions bundled ("⇄ n · m back"); with a CV3 library (T067): one line per card-library pair into its band chip, the card's line chip summing them ("In-process n · m libraries"); between two CV3 libraries: counted on the band, drawn for the selected library, listed in the Links tab | - |
| CE5 | `documents_route` from an operation in A to a route in B; `calls_operation` from a client call to an operation | no own map link: the documented values feed AA2 on the CE1 link the operation documents; the Links tab lists "documents" rows | operation with no route: "documented, no route" in the Not proven list |
| CE6 | `imports` of an `external` node from a non-test file of A (stdlib excluded) | a dashed "SDK n" chip on the component's card (T067; until T067 a muted dotted stub card per O2's choice), wording "imports <package>" in its tooltip and the inspector; never a line or a protocol | - |
| CE7 | (TOP-2 of 2026-11-18) `calls_action` from an action call in A to a Next.js server action in B (A ≠ B; `docs/extract/server-actions.md`) | server action link A → B: count, tier histogram, members named by the action | an action call is stored only when linked, so no ghost; inside one component: CED |
| CE8 | (TOP-2 of 2026-11-18, `serve --project` only) a stored cross-repository `calls_route` project link from a client call of this member to a route of another member, or from another member's call to a route of this member (ADR 0075) | HTTP link between the component and a muted card "<repo> (other repository)", with the links' assumption texts (cross-repository assumption, row assumptions), tier and stale reasons as stored; the calls count as "linked across repositories" per component (beside the repository's own count without a link), never in CE1's ghost; (R1) a stored `documents_route` project link from an operation of this member (or into its route) is a "documents" row with that card, and the operation counts as linked across repositories, never in CE5's "documented, no route" (beside the repository's own count); (2026-11-24 T044) the card counts both kinds each way: calls to it and from it, operations documenting its routes and its operations documenting this member's routes (each operation once); calls are counted per link and filtered by the test scope (scope `production` leaves out links whose call is test code), operations are counted per distinct operation and not filtered by the test scope | ambiguous and other outcomes of the pass: CE1's ghost as in the repository; a single-repository server: no CE8 |
| CED | every other stored edge kind (`renders`, `injects`, `routes_to`, `uses_config`, `foreign_key`, `alters`, ...) and edges inside one component | not a component link; the Links tab counts them under "not drawn" by kind | always |

## Claim contract: annotations (rows AP, AA, AT, AE)

Cells per link; a bundle shows "mixed" (half cell) when its members
differ, with counts in the inspector (AG15). Sources in brackets are the
label the cell shows.

| id | cell | form | value [source] |
|---|---|---|---|
| AP1 | protocol | CE1 | "HTTP" + method and path per member [proven: route and client-call facts]; HTTP version, WebSocket: not claimed |
| AP2 | protocol | CE2 | "Tauri IPC" [proven: `invokes_command`] |
| AP3 | protocol | CE3 | "SQL via <tool>" [proven: schema set `tool`]; engine from AE rows |
| AP4 | protocol | CE4 | "in-process" [proven: `calls`/`imports`] |
| AP5 | protocol | CE7 | "server action" [proven: `calls_action`; Next.js sends an action as a POST to the page that renders it, so auth stays AAD ("checks inside the action are not read") and TLS ATD; a direct call not proven to cross the client boundary may run in process, which the source says] |
| APD | protocol | anything else (stdio, sockets, queues, gRPC, a stub) | "protocol ?" [unknown: not modelled] |
| AA1 | auth, server side | Express route with stored `middleware` names | "middleware <name>" [as written]; empty list: "no route middleware" [as written] |
| AA2 | auth, documented | the route's `documents_route` operation(s) with `security` | "documented: <scheme names>"; `security: []`: "documented: none" [documented]; operations disagreeing: mixed |
| AA3 | auth, server side | FastAPI route whose parameters (`Depends`, `Security`, `Annotated[..., Depends(...)]`) or route, router or app `dependencies=` reach, through resolved dependency functions, an instance of a `fastapi.security` class or a subclass (DA-F rows) | "declares <class> (<scheme name>)"; `auto_error=False`: "declares <class> (<scheme name>), optional (auto_error=False)"; unknown `auto_error`: ", auto_error ?" [declared] |
| AA4 | auth, client side | hey-api generated call whose options carry `security: [...]` literal (DA-H rows) | "generated client documents <scheme>" [documented by the generated client, as written in <file>] (AG-B4a: not "sends": the client adds credentials only when the application configures `auth`, which is not read) |
| AA5 | auth, server side | Express `app.use` / `router.use` mounted before the route on its app or router chain (DA-E rows) | "middleware <name>, mounted at <prefix>" [as written] |
| AA6 | auth, server side | Axum `Router::layer` / `route_layer` on the router that registers the route (DA-X rows) | "layer <expression>" [as written]; AG-F: a DA-X4 stack reads "layer ServiceBuilder [<inner> · ...]" in written order, a DA-X3 layer "method router layer <expression>" |
| AAD | auth | Tauri IPC, in-process, SQL, every route or call no row covers | "auth ?" [unknown: <reason>] (Tauri: "capabilities not read") |
| AT1 | TLS | CE2 Tauri IPC, CE4 in-process | "n/a, in-app" |
| AT2 | TLS | (per O3) a Traefik router with `tls=true` on a compose service CD3 maps to the target component (DC-T rows); review fix: not when Traefik's Docker provider ignores the service (`traefik.enable` false, or a same-directory Traefik service with `exposedbydefault=false` and no `traefik.enable=true`), listed as Not proven | "TLS at ingress <router> as configured in <file>" on that hop only; the card chip keeps a CD4 basis ("assumed" / "merged") |
| AT3 | TLS | absolute `https://` origins in client calls or spec `servers` | shown in the inspector as "documented origin", never as this link's TLS |
| ATD | TLS | everything else | "TLS ?" [unknown: transport security comes from deployment; no deploy config proves this hop] |
| AE1 | engine | stored table dialect `postgres` on every table of the set: a migration set (sqlx postgres) or ORM model tables (drizzle `pgTable`, ...) | "PostgreSQL" [proven: migration dialect; review fix: "proven: ORM model dialect (<tool>)" for model tables, "proven: stored table dialect (<sources>)" when a set mixes them] |
| AE2 | engine | a compose service with a database image (`postgres`, `mysql`, `mariadb`, `mongo`) named by `depends_on` or a URL host in the environment of the CD3 service of a component that accesses the schema set | "<engine> <tag> as configured in <file> (service <s>)"; image-only services elsewhere: listed on the card as "deploy config names <image> (<file>), not tied" |
| AE3 | engine evidence | a DB driver in the accessing component's manifest dependencies | "driver <name> declared in <manifest>" (evidence line, never the engine value) |
| AED | engine | everything else, and conflicting AE2 facts | "engine ?" [unknown]; conflicts list every fact |

## Claim contract: deploy config (rows DC)

| id | form | fact | refusal |
|---|---|---|---|
| DC-S1 | `compose.y(a)ml`, `docker-compose*.y(a)ml`, `compose.*.y(a)ml` with a `services` mapping | one `deploy_service` node per service: image as written plus parsed name and tag, `build.context` and `build.dockerfile` as written and resolved, `depends_on` names, ports, an `interpolated` flag per value; review fix: `dotenv_compose_file`, the directory's `.env` when it has a `COMPOSE_FILE` line (no value stored) | YAML the reader refuses, anchors it cannot resolve: diagnostic, no nodes for that file |
| DC-S2 | `environment` entries whose value parses as a URL | scheme and host only (never user info, password, path or query) | other values: not stored |
| DC-T1 | service `labels` `traefik.http.routers.<r>.{rule,entrypoints,tls,tls.certresolver}`; review fix: `traefik.enable` | router facts on the service (rule as written); `traefik_enable` as written and parsed | interpolated router name: kept with `interpolated: true` |
| DC-T2 | Traefik command-line `--entrypoints.<e>.address`, `...http.redirections.entrypoint.to/scheme` on a `traefik` image service; review fix: `--providers.docker.exposedbydefault` | entrypoint facts on that service; `traefik_exposed_by_default` | - |
| DC-F1 | Dockerfile (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`; not `*.dockerignore`) parsed by the stage reader | `FROM` images per stage, `EXPOSE` ports | parse failure: diagnostic, no node |
| DCD | everything else (nginx, CDK, Kubernetes, Helm, `.env` files, compose `extends`/`include`, merges across files) | nothing; facts are per file, never merged | always |

## Claim contract: auth extractors (rows DA)

| id | form | fact | refusal |
|---|---|---|---|
| DA-F1 | a FastAPI route handler parameter `x = Depends(f)` / `Security(f)` / `x: Annotated[T, Depends(f)]` (also through a module-level `Annotated` alias such as `CurrentUser`), and `dependencies=[Depends(f)]` on the route, `APIRouter`, `include_router` or `FastAPI` | a dependency step to `f` resolved by the Python structure resolver | unresolved `f`: dependency step `unresolved` (AAD with reason) |
| DA-F2 | a dependency function's own parameters, recursively (cycle-safe, depth ≤ 8) | further steps | depth cap: `truncated` |
| DA-F3 | a step to a module-level instance of `OAuth2`, `OAuth2PasswordBearer`, `OAuth2AuthorizationCodeBearer`, `HTTPBearer`, `HTTPBasic`, `HTTPDigest`, `APIKeyHeader`, `APIKeyQuery`, `APIKeyCookie`, `OpenIdConnect` or a class whose resolved bases reach one of them | route attribute `security_dependencies`: class, scheme name, `auto_error` literal | non-literal (or positional, `**kwargs`, a repository `__init__` on the way) `auto_error` / `scheme_name`: `unknown` |
| DA-H1 | a hey-api generated call (`library: hey-api`) whose options object literal has `security: [ {scheme, type, in, name}... ]` | client-call attribute `security` as written | non-literal entries: `security: unknown` |
| DA-E1 | `app.use(...)` / `router.use(...)` on an app or router object the existing Express extractor proves, whose layer Express pushes before the route's (same function body of one file in source order; across files only between top-level statements, the object's own file first), on the route's router or on each router of its mount chain before the `use` that mounts the next one down (middleware arguments before a router in the same call included), with a literal or absent path that matches the route path at a segment boundary (AG-B4b: Express 4.21.1 and 5.1.0 router sources) | route attribute `mounted_middleware`: per middleware argument the name as written, the `use` path (`/` when absent), file and span, root router first; only middleware every mount chain of the route states | `mounted_middleware_unknown` entry with a reason: the object passed to another call (it may register middleware), a router mounted earlier at a matching path that has its own middleware, a dynamic path or a first argument that may be a path, order not proven (another function or file), conditional registration, a spread argument, a path that matches only case-insensitively or through a route parameter or wildcard syntax, middleware on some mount chains only, a route not mounted from a proven app, more than 64 chains; review fix: an earlier route layer (`app.all`, `router.all`, a registration of the same method, GET for a HEAD route, any method for an `all` route, or a `route(p)` chain holding one) on the route's router or a router of its mount chain, pushed before the anchor or in an order not proven, whose path is not proven disjoint from the route's path at that level (two literal segments at the same position that differ case-insensitively before the first segment of either path with other syntax, or two plain paths with a different segment count: Express route layers match the whole path), "earlier route handler <expr> (<METHOD> <path>) may run first"; a router mounted earlier whose own route layer may match the same way, "router <r> mounted earlier has route handler <expr> (<METHOD> <path>) that may run first" |
| DA-E2 | a middleware (route argument or DA-E1 entry) that is, or names through at most 4 bindings and object-literal properties, a call to a closed source-verified list: `passport` `authenticate` (0.7.0), `express-jwt` `expressjwt` (8.5.1) or default export (6.1.2), `express-oauth2-jwt-bearer` `auth` (1.6.1), `express-openid-connect` `requiresAuth` (2.19.2) | `declares: {library, detail}` (passport: the strategy literal; others: the export name); express-jwt `credentialsRequired` and express-oauth2-jwt-bearer `authRequired` literal `false`: `optional: true`; route arguments in `middleware_declares` with their index | non-literal strategy: `strategy ?`; non-literal or spread options: `optional: "unknown"` |
| DA-E3 | AG-F: `use` calls in the body of a module function (a function declaration or const function, not async or a generator) whose parameter receives a proven app or router, at the function's only call with that object anywhere, a top-level unguarded call (positional argument to an identifier parameter, or an object-literal property to an object-pattern parameter naming that key); the parameter not reassigned, aliased, returned, passed to another call or used in a nested function, every use on it a plain `p.use(...)` that mounts no router or app (RealWorld `configureMiddlewares({ app })`) | DA-E1 entries placed at the call, in body order, with `via: {function, file, span}` of the call | any condition not met: the DA-E1 escape entry as before; a body `use` that is guarded or after a return or throw of the body: `mounted_middleware_unknown` "conditional registration" |
| DA-E4 | Review fix: `router.route(p).all(f...)` chained before a method call of the same `route(p)` builder (`route(p).all(requireAuth).get(h)`; Express 4.21.1 `Route.all` / `Route.dispatch` and router 2.2.0 push one route stack entry per call and run them in order, `all` entries for every method) | the `all` handlers lead the later method's route `middleware` (AA1), in chain order; the `all` step is no route of its own | an `all` step chained after the last method: an `ANY` route as before; a same-method step chained earlier: no claim (DAD) |
| DA-E5 | Review fix: a DA-E1 middleware argument that is a function of length 4 or more (JavaScript `Function.length`: parameters before the first default or rest parameter), as an inline function or a name bound to a function declaration or a module-level const function, locally or through one import (Express 4.21.1 `Layer.handle_request` and router 2.2.0: `fn.length > 3` is skipped for a request without an error) | route attribute `mounted_error_handlers`: name, `use` path, file and span; never in `mounted_middleware` (it does not run for a normal request) | length not proven (a call, an unresolved name): DA-E1 as before |
| DA-X1 | `.layer(e)` / `.route_layer(e)` on a proven `axum::Router` (ADR 0016 recognition) after the route's registration on that router, or after the `nest` / `merge` / factory return that moved the route's router into it (axum 0.7.9 and 0.8.8 `path_router.rs`: both map over the routes held at the call) | route attribute `layers`: expression as written, method, file, span, innermost first; only layers every mount chain applies | conditional layer, layers on some mount chains only: `layers_unknown`; receiver not a proven Router (kftray's tower client layer): nothing |
| DA-X2 | a layer argument that is a call to `tower_http::validate_request::ValidateRequestHeaderLayer::bearer` / `::basic` (tower-http 0.6.8), `axum::middleware::from_fn(f)` / `from_fn_with_state(s, f)`, or the `axum_login::login_required!` macro (0.18.0), resolved by the shared Rust resolver | `declares: {library, detail}` (tower-http, axum-login with the backend type as written) or `middleware_function: f` as written (from_fn) | every other layer (`ServiceBuilder` stacks included): the expression as written only |
| DA-X3 | AG-F: `.layer(e)` / `.route_layer(e)` on the method router of a route (`get(h).layer(e)`): axum 0.7.9 and 0.8.8 `method_routing.rs` map it over the endpoints the method router holds at the call, so it wraps the methods chained before it, never later ones | `layers` entries with `on: "method_router"`, innermost (before every Router layer); DA-X2 and DA-X4 apply to `e` | conditional: `layers_unknown` |
| DA-X4 | AG-F: a layer argument that is a method chain on tower's `ServiceBuilder::new()`: every method is tower's layer-adding builder method (tower 0.4.11, 0.5.2, 0.5.3) or tower-http's `ServiceBuilderExt` method (0.6.8), or an identity (`into_inner`, `check_*`) | `stack` on the layer entry: each inner layer as written in written order (tower applies the first written outermost), `.layer(e)` / `.option_layer(e)` with DA-X2's names | another method in the chain (`service`, a user trait's method): no `stack`, the layer as written only (DAD) |
| DAD | everything else (custom auth inside handler bodies, decorators, `Request` inspection, Starlette middleware, guards in Nest, Tauri capabilities) | nothing | always |

## Claim contract: chain (rows CH)

Start: any node at any level (component, file, route, handler, function or
method, client call, IPC call or command, table). Direction up, down or
both; all branches.

| id | form | step [wording] |
|---|---|---|
| CH1 | `calls` between functions/methods | down: callee [calls]; up: caller [called by] |
| CH2 | `contains` function/method → client call or IPC call | down: the call [makes request]; up: its owner [made in] |
| CH3 | `calls_route` | down: route [calls route, tier as stored]; up: client call |
| CH4 | `exposes_route` | down from a route: its handler [handled by]; up from a handler: its route |
| CH5 | `invokes_command`, `exposes_command` | as CH3/CH4 for Tauri IPC |
| CH6 | `reads_table`, `writes_table` | down: table [reads / writes, "may read / may write" when inferred, ADR 0056 I8 wording]; up from a table: its accessors; each edge carries `access`, the owner's `table_access` entry as change impact's I8 step reads it (tool, role, operations, rows, verification `oracle`/`mapped`, assumes; T030 D7) |
| CH7 | `contains` function/method → function/method (nested) | down from any function: nested functions, dashed [contains, not a call]; up from an inner function to its enclosing one only when the inner one has no stored caller (ADR 0045 `enclosing` rule) |
| CH8 | Angular `renders`, `injects`, `routes_to`, `loads_children` (stored) | followed, dashed, with ADR 0056 I9 wording |
| CH9 | a start or step that is a component or a file | component: its CE1-CE4 links, transitively over components; file: the union of the chains of its symbols, routes, calls and tables |
| CH10 | a member whose node is test scope | not followed; counted per step ("n hidden by test scope") |
| CH11 | a step ending at a function with no stored caller (up), or an AA3 dependency step | an end line "? not modelled": "no stored caller and no resolved bridge; JSX renders and passed callbacks are followed only where the tag or value resolves to this function, other dependency injection and dynamic calls are not modelled" (O4 (a); review fix: worded after AG-B5, whose stored `renders` / `passes_function` facts are followed as dashed bridges before this end line applies), DI from DA-F steps named; review fix: downstream, a function with nothing to follow whose file stores an `unresolved_calls` diagnostic ends with "? not modelled: no stored callee; <file> has n calls the indexer could not resolve, not followed (this function's calls may be among them)" |
| CH12 | caps | the requested depth (default 6, at most 10 since T042), 2,000 members, fan-out 200 per node (ADR 0056 defaults); every binding cap reported per step ("+ n beyond step 6") |
| CH13 | (TOP-2 of 2026-11-18, `serve --project` only) a client call member with a stored cross-repository project link (down), a route member another member's calls link to (up), or a CE8 link of a component chain | one step into the other member's muted card: its route (down) or call (up) [calls route in <repo> / called by client call in <repo> (project link), the link's tier], and an end line "continues in <repo>" with the link's assumption: the other repository is not walked |
| CHD | every other edge kind | not followed; counted as "not followed: <kind> ×n" |

Path tier: the weakest edge on it; the inspector says where ("inferred 0.9
at step 3").

**Map budget** (decided default, measured in the inventory §4): on the map
at most 3 steps each way, 5 members per step and 40 members in total; the
rest folds into per-step counts with "Open in Flow tab". The inspector lists
every member within the caps grouped per step (virtualised). Fits on
fastapi: route chains 23 of 23 both ways; langflow: 124 of 296 downstream.

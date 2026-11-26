# ADR 0013 — HTTP route nodes and path composition

**Status:** Accepted (developer review, 2026-10-09)
**Date:** 2026-10-08

## Context

T011a needs source-backed HTTP routes and repeatable path composition without
changing contract v1, persistence, dependencies or symbol identities. This
decision covers Express, FastAPI and Axum; only Express is implemented here.
Revision 2 of T011a accepts express-promise-router and makes every Express app
a composition root. The investigation remains recorded in the T011a task record (internal).

## Options considered

- Receiver-name inference: rejected; clients and unrelated routers share names.
- Cache composed paths: rejected; editing a mounting file would leave cached
  routes stale.
- Put mount prefixes in identity: rejected; moving a router would detach its
  content-bound annotations.
- Resolve allowlisted factory imports, cache syntax, compose on every run:
  chosen; gives evidence-backed recognition and current paths.

## Decision

Recognition comes from a resolved import of an allowlisted package, never from
receiver names. Adding a factory requires a task and fixture. Express factories
are the table in docs/extract/routes.md: express default creates an app;
express default.Router or named Router creates a router; express-promise-router
default creates a router. Local/relative-imported object bindings and one level
of same/relative-imported factory return are supported. FastAPI recognizes
FastAPI/APIRouter from fastapi; Axum recognizes axum::Router::new with method
routers from axum::routing. These latter implementations are deferred.

A route is a level-4 source node, kind route. Its span and source hash cover
the registration call expression (the verb call in a route chain). The nearest
emitted lexical owner contains it: an enclosing named function, otherwise the
file. Decoded qualified name is METHOD plus one space plus the literal pattern
without mounting prefixes; method is uppercase, all/any becomes ANY. Identity
uses contract escaping: route:<registration file>#<METHOD> <pattern>. Repeated
identities in one file receive @2, @3 in declaration order under ADR 0011.
Body/line movement and prefix edits do not change identity.

Patterns require string literals or substitution-free template literals;
dynamic patterns emit no route and a route_dynamic_pattern warning identifying
the call span. Cache per-file candidates, objects, bindings and mounts in the
existing fact cache. Compose against the complete current inventory every run.
Express use(prefix, router) and use(router), FastAPI APIRouter(prefix=) and
include_router(router, prefix=), Axum nest(prefix, router) and merge(router)
compose paths. Prefixes resolve only from literals or module-level const
literal bindings, including resolved relative imports. The FastAPI
settings-instance/class-default case remains pending T011b.

Every express() app is a root, including a sub-app mounted elsewhere. Traverse
resolved mounts from each root, guarding cycles per chain. Deduplicate repeated
path evidence across roots. An unresolved router argument emits
route_mount_unresolved naming the expression; an unresolved prefix emits
route_prefix_unresolved. Neither invents a path. Routers unreachable from any
root emit route_unmounted. Workspace packages, path aliases and re-export
traversal remain out of scope.

Attributes and exact path normalization are specified in docs/extract/routes.md.
The last handler argument is the handler; preceding arguments are middleware
text without whitespace, without edges. A resolved callable gets a fact,
confidence-1 exposes_route edge **from handler to route**, with registration
span evidence. Existing callable/ambiguity rules apply; inline/unresolved
handlers have no edge. An unresolved non-inline handler retains whitespace-free
handler_expression and a null handler, including handlers behind export-star
barrels.

Aggregate extractor version becomes extractors-7. Per-file syntax lives in
internal/lang/typescript; composition in internal/extract/routes. Contract v1,
store interface, schema and dependencies remain unchanged.

### Downstream T012 (not implemented here)

Client extractors for fetch, axios, Angular HttpClient, Tauri invoke and Dart
http/Dio emit candidates with method and normalized URL template. T012 uses
the same normalization specified in docs/extract/routes.md, matches method
and each paths.template with parameter names as wildcards, and emits
calls_route from calling symbol to route, provenance inferred with confidence.
Unmatched calls are reported. Impact on a handler traverses
handler -exposes_route-> route <-calls_route- caller, labelling inferred
results with confidence. The exposes_route direction is fixed by this ADR.

## Consequences

Every framework shares a route model without extending the transport. Path
changes can update route attributes without reparsing the registration file.
The extractor version invalidates existing fact caches. Source hash covers
registration syntax, while composed attributes depend on the current inventory.

## Known gaps

Resolution is syntactic and conservative: no arbitrary expression evaluation,
workspace/path-alias resolution, export-star traversal or recursive factory
analysis. Reordering repeated registrations can reassign suffixes; identity plus
content hash still prevents silent reuse of annotations for a different body.
FastAPI and Axum extraction and T012 client linking are later tasks. Postgres
and cross-platform execution require CI; no storage changes are part of T011a.

## Review notes (developer, 2026-10-09)

- Accepted. Express corpus: 19/19 RealWorld operations. Zero routes on the
  React, Angular, FastAPI, Axum and Langflow corpora; Langflow's
  axios-heavy frontend confirms that client calls never become routes.
- All 24 corpus handlers sit behind export * barrels, so the corpus has 0
  exposes_route edges. Route-to-handler tracing (M2/M3) depends on
  re-export traversal; it is the next task, together with workspace-package
  and tsconfig path-alias resolution (3 route_mount_unresolved in the
  corpus come from @conduit/* workspace imports). T012 needs the same
  module resolution for frontend code.
- In a .route(p).get(h).post(h2) chain, each verb call's span contains the
  earlier chain links, so editing one verb's handler changes its siblings'
  hashes and would mark their dispositions for re-review. Acceptable for
  now; narrow the span to the verb member call and its arguments if route
  dispositions become noisy.
- Route diagnostics embed byte positions in their messages. Diagnostics are
  republished every run, so this causes no graph-row churn.

## Review notes (developer, 2026-10-09, after T011b)

- Handler and mount resolution resolved by ADR 0014: the Express corpus
  now has 24/24 handler edges and 0 unresolved mounts.

## Revision 1 (T035: mounted Express apps)

**Status:** Accepted (delegated review, 2026-11-24)

### Context

The Decision's "every express() app is a root, including a sub-app mounted
elsewhere" stored, for a mounted app, full paths the served app answers
404 for: with `app.use("/sub", sub)`, `sub`'s `sub.get("/ping")` was stored
as `/ping` beside `/sub/ping`. Express 4.21.1 and 5.1.0 (both MIT) serve a
mounted app only below its mount. FastAPI had the same rule (ADR 0015
Revision 1). The T035 task record (internal) has the oracle.

### Options considered

- Keep the rule: rejected; a confident wrong path per mounted route.
- An app a `use` consumes is no root (chosen), as for FastAPI.
- Also end DA-E1 mount chains only at unconsumed apps: rejected; an app
  both mounted and served on its own (RealWorld's `apps/user`, mounted in
  `apps/local` and served with `listen`) would state the outer app's
  middleware for a deployment that does not run it. Path and middleware
  claims are kept on the safe side each: no own-root path, and the own
  chain still counted.

### Decision

Every Express app is a root unless a `use` consumes it. `x.use(p, app)`
and `x.use(app)` on a proven app or router `x` consume `app`: a constant
or absent prefix composes the paths through the mount, any other prefix is
a hop (`unresolved_mounts`, now also for an app, not only a router). A
`use` on an object no row reads consumes every proven app among its
arguments, named in `route_mount_unresolved`; its routes keep no path. A
`use` in a test-scope file consumes only a test-scope app. DA-E1 mount
chains are unchanged (every app is a chain end). A link-time change: cached
syntax is unchanged.

Step EM2 adds two parts. A consumed app stays a root when a serving form
from a closed list proves it is also served at its own root: `x.listen(...)`,
`http.createServer(..., x, ...).listen(...)` (Node's `http` and `https`,
also as `node:` specifiers), and `s.listen(...)` with `s` a const binding of
such a `createServer` call; the own-root paths are conditional when every
such form is under a branch, loop, try or short-circuit or inside a
function. A form in a test-scope file serves only a test-scope app. Lambda
and serverless wrappers are not in the list: whether the wrapper's value
is exported (and so deployed) is not in the cached syntax, and a gateway's
base-path mapping can rewrite the path. Route order: Express passes a
request a mounted app or router does not answer on to the parent's next
entry (4.21.1 and 5.1.0), so a mount does not shadow a later parent route
by its prefix; a route of the mounted app that matches first is the
earlier route handler DA-E1 already records in
`mounted_middleware_unknown`, and the later route keeps its path, as for
every earlier route handler (it is reached when the earlier handler calls
`next()`). A mounted app or router whose own stack holds middleware runs
it for every request reaching the mount, and a catch-all among it answers
a later parent route below the prefix first (Express 4.21.1 and 5.1.0:
404). Such a route keeps its paths, conditional, with the shown
assumption RO-V2 ("a mounted app's catch-all may answer first") naming
the mount; never dropped, since middleware that calls
`next()` lets the route answer.

T036 (CommonJS modules) reads the mounts above through CommonJS too: a
relative `require("./m")` in a JavaScript file resolves as Node's CommonJS
loader does (the path, `.js`/`.json`/`.node` appended, a directory's
`index`; compiler options do not apply, since `require` is resolved at run
time) to the module's CommonJS exports, cached syntax (extractors-59) recording
only top-level `module.exports = E`, `exports.n = E` and
`module.exports.n = E` writes, and `const { n } = require("./m")` binds `n`
to that member. The mounted value then composes as through an ESM import,
with the same refusals: a specifier that is not a literal or a conditional
choice of requires gives no value (`route_mount_unresolved`), a prefix that
is not a constant a hop, a mount under a branch a conditional path. Any
other write of the exports object (in a function or branch, twice,
`exports = E`, `Object.assign`, an ESM `export` beside it) leaves the
module's exports unread, and a `.ts` module required from JavaScript, an
ES module, a directory with its own package.json and a `require` in a
TypeScript file give no value. The
rows are CJ-U1..CJ-U4 and CJ-E1..CJ-E4 of `docs/extract/routes.md`;
Express 4.21.1 and 5.1.0 serve every path the fixture stores with its own
handler.

### Consequences

A mounted app's routes have only the paths the served app answers. Routes
of apps mounted through an unread object or an unread prefix lose their
own-root path (precision-safe recall loss). Middleware of the app above a
mount stays "declared on some mount chains only" on a mounted app's routes.

### Known gaps

An app both mounted and served on its own through a form outside the
closed list (a Lambda or serverless wrapper, `new http.Server(sub)`, a
server bound with `let`) loses its own-root paths when its mount has a
prefix. An earlier route handler that ends every response still leaves
the later route's path stored (DA-E1 marks it; whether a handler calls
`next()` is not read). Object identity is
syntactic: a factory returning one `express()` both served as the main app
and mounted elsewhere is consumed everywhere. An app passed to anything
other than `use` (an `http.createServer` argument, a wrapper) is not read
as a mount. CommonJS exports written conditionally or through
`Object.assign` keep no path though Express may serve them (recall only),
and a `require` in a TypeScript file or of a directory's package.json
`main` is not read.

Revision 1 review notes (2026-11-24): confirmed pre-existing wrong facts (mounted Express and FastAPI sub-apps stored at their own root; the real frameworks answer 404 there) fixed with oracles on Express 4.21.1/5.1.0 and FastAPI 0.121.1/0.136.3/0.137.0/0.165.0; one independent review and a confirmation pass (no open finding); gate run 45311511149 green on Linux, Postgres and Windows; no corpus route changed except conditional marks with shown assumptions (RealWorld 21, seerr 1, gladys 1). Known gaps: an app served through forms outside the closed list (Lambda wrappers, a string passed to `uvicorn.run`, a Dockerfile command) keeps only its mounted paths.

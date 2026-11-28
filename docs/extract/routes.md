# HTTP route extraction

ADRs 0013–0016 define the shared model. Express, FastAPI and Axum use cached syntax
candidates and composition on every run (extractors-64, contract v1). Go routers
(net/http, chi, gin; ADR 0076) have their own section below. Next.js
route handlers and Pages Router API files (ADR 0077) are at the end.

## Factory allowlist

| Resolved package | Export/callee | Object |
|---|---|---|
| express | default factory (`express()`, including require) | app/root |
| express | default factory's Router member or named Router | router |
| express-promise-router | default factory | router |

Names of local receivers never establish recognition. Relative imported object
bindings and one level of local/imported factory returns resolve. New allowlist
entries require a task and fixture. Re-exports, workspace packages and path aliases use the shared
TypeScript/JavaScript resolver (ADR 0014). CommonJS require of an allowlisted factory is supported,
and a relative `require` reads the module's CommonJS exports (T036, see
"CommonJS modules" below).

## Registration and attributes

Direct get/post/put/patch/delete/options/head/all calls and route(pattern) verb
chains each produce a route per verb on a proven object. all uses ANY. Patterns
are literal contents as written, excluding surrounding quotes/backticks;
substitution-free templates are literals. Variables, concatenations, arrays
and regex patterns produce route_dynamic_pattern warnings and no route.

Route nodes are source nodes at level 4, with registration-call spans and hashes.
Qualified names are METHOD plus a space plus unprefixed literal pattern;
duplicates in source declaration order get @2, @3. Contract identity escaping
applies. Contains edges come from the nearest emitted named function or file.

Every later registration of the same method/raw pattern on one proven Express
app/router in one file emits route_duplicate_registration. The warning names
both spans and reports whether resolved handlers and middleware are identical.
Differing registrations are warned too. Equivalent tied routes link to the first
registration only after existing method, specificity and scope filters, when
their non-null non-inline handler, middleware and matching path template/scope
agree. Other frameworks and different routing objects never use this rule.
Every source registration keeps its separate identity.

The registration groups this proof establishes are stored (extractors-47,
ADR 0075): each Express route of a group of two or more registrations
sharing one proof (one proven app/router in one file, method, raw pattern,
resolved non-inline handler, middleware) carries `equivalent_registrations`,
the group's identities in source order. The routing object key stays
internal; matching path template/scope is still checked when a tie is
resolved. A route outside such a group has no attribute. The project link
pass reads only this attribute and so collapses exactly the ties the
in-repository linker collapses (`TestStoredMatcherReplaysClientLinks`).

| Attribute | Meaning |
|---|---|
| framework | express, fastapi or axum (Go: see the Go section) |
| equivalent_registrations | Express only, when proven: identities of the equivalent registrations (ADR 0022) in source order, the route itself included |
| method | uppercase HTTP verb, or ANY |
| pattern | literal contents at registration, without mounts |
| handler | resolved callable identity or null |
| handler_inline | true for anonymous arrow/function handler arguments |
| handler_expression | whitespace-free non-inline handler text when unresolved; otherwise absent |
| middleware | list of whitespace-free preceding handler arguments; no middleware edges |
| mounted_middleware, mounted_middleware_unknown, middleware_declares | Express `use` middleware composed onto the route and auth-library factories ([auth extraction](auth.md), T026 AG-B4b) |
| layers, layers_unknown | Axum `layer` / `route_layer` composed onto the route ([auth extraction](auth.md), T026 AG-B4b) |
| group_middleware, group_middleware_unknown | gin and chi router-level middleware (`Use`, `Group` arguments, a `With` above the route, `gin.Default`) composed onto the route ([auth extraction](auth.md), T029 SH-S) |
| paths | sorted list of composed path entries; empty when no chain resolves |
| unresolved_mounts | only on a route with no path entry whose composition stopped at a hop no row reads (Express: a `use` prefix that is not a constant; FastAPI: an `include_router`/`mount` prefix (a mounted app's routes included) or an included router's own `prefix=` that is not a constant; Axum: a `nest` prefix that is not a constant; Go: see the Go section): per hop, its key, hash, reason, text, file and line, and the route's paths below the hop. See "Unresolved mounts" below |
| unresolved_prefix_paths | only on a route with no path entry whose prefix the code does not fix (Go, Express, FastAPI, Axum: see "Unknown-prefix paths" below; ADR 0084): the paths proven below the unknown part, each with `prefix: "unresolved"`; never a full path. `paths` keeps its meaning |
| scope | test when the registration file is test-scope; omitted otherwise |

Each path entry has template, conditional (boolean), resolution (literal,
constant or settings_default), and optional: true only when an Express optional parameter occurs.
Any constant prefix makes the chain's resolution constant. Any mount inside
an if/switch/conditional expression makes the chain conditional. Multiple
roots reaching the same template deduplicate. If any chain is unconditional,
conditional is false; if any chain is literal, resolution is literal. Optional
is true if any chain contains an optional parameter. Sort by template.

Path entries carry scope: test only when every reaching chain is test-scope;
any production chain omits it. Root definitions, composition sites, contributing
FastAPI helper bodies/invocations and registrations contribute scope; prefix
values do not. Merge independently within the template/slash pair. See
[test scope](test-scope.md). This is link-time data under extractors-49;
unchanged cached syntax, identities, spans and hashes are preserved.

## Exact template normalization

Join root, mount prefixes and registration pattern with `/`. Collapse repeated
slashes, ensure a leading `/`, and remove trailing `/` except for root.
Preserve case, punctuation, percent escapes and dot segments; do not URL-decode.

Express reads each path piece (a registration pattern, a `use` prefix)
under the Express major that parses it (T028, claim contract
[express5-paths](../contract/claims/express5-paths.md), rows
EX5-W1..EX5-W14): the nearest `package.json` declaring `express` with a
range of one major, 4 or 5, decides; otherwise the major is unknown and a
piece is claimed only where Express 4 and 5 read it the same. Express 4
(path-to-regexp 0.1): `:name` filling a segment becomes `{name}`, `:name?`
`{name}` plus optional: true on the entry, a `*` ending a registration
pattern `{*}`. Express 5 (path-to-regexp 8): `:name` becomes `{name}`,
`*name` ending a registration pattern `{*name}` (so `/r` + `/*splat` is
`/r/{*splat}`), and `{...}` groups in a registration pattern expand into
one path entry per combination unless one keeps an empty segment or a
trailing slash; patterns Express 5 refuses get no path (warning
`route_pattern_invalid`). Every other form (a piece not starting with `/`,
a literal byte no request path carries, an empty segment, parameters
inside a segment, escapes, wildcards elsewhere, Express 4
regular-expression syntax, a group in a prefix, readings that differ under
an unknown major) gets no path through that piece (info
`route_pattern_unmodelled`; a route mounted only below such a prefix is
named in one instead of `route_unmounted`), never a guessed literal. Only
a final `:name?` of a registration pattern sets optional. A manifest that
does not parse makes the major unknown; a pnpm `catalog:` range reads
`pnpm-workspace.yaml`. This is a link-time change (extractors-50): cached syntax,
identities, spans and hashes are unchanged. FastAPI preserves brace parameters and strips Starlette convertor
suffixes (`{id:int}` becomes `{id}`). Starlette's `path` convertor matches
the rest of the path, slashes included: when `{name:path}` is the whole
final segment of the composed path it is stored as the final catch-all
`{*name}` (`/api/{_path:path}` becomes `/api/{*_path}`), so the client
matcher's catch-all rules apply to it as to every framework; elsewhere
(`/files/{p:path}/meta`, `/x{p:path}`) it keeps the one-segment form
`{p}`, as before (a multi-segment value there is not modelled). The
generated OpenAPI document writes the catch-all back as `{name}` with its
`matches` note (OG-18).

Every framework stores its final rest-of-path wildcard in the one form
`{*name}`, or `{*}` where the wildcard has no name: Express 4 `*` (`{*}`),
Express 5 `*name`, Next.js `[...name]` and `[[...name]]` (optional), Axum
`*name` and `{*name}`, chi `*` (`{*}`), gin `*name`, net/http `{name...}`
and a subtree pattern `/a/` (`/a/{*}`), and FastAPI's final
`{name:path}` (extractors-58). Colons and asterisks outside braces remain literal for FastAPI. T012 uses the same slash normalization for client URL path templates, without
Express colon conversion, and compares parameter names as wildcards.

## Composition and diagnostics

Every Express app is a root unless a `use` consumes it (T035): an
app passed to `use` on another app or router answers only below that mount
(Express 4.21.1 and 5.1.0 answer 404 at the sub-app's own paths on the app
that mounts it), so its routes get only the paths composed through the
mount. A consumed app whose `use` prefix is not a constant keeps no path
and stores the hop in `unresolved_mounts`; an app passed to `use` on an
object no row reads (`holder.server.use("/raw", raw)`, a parameter) is
consumed too, named in `route_mount_unresolved`, and keeps no path. A `use`
in a test-scope file consumes only a test-scope app. The mount chains of
DA-E1 ([auth extraction](auth.md)) still end at every app, consumed or not:
an app both mounted and served on its own (the RealWorld corpus mounts
`apps/user`'s app in `apps/local` and also serves it with `listen` and a
Lambda handler) would otherwise state middleware of the outer app its own
deployment does not run.

A consumed app stays a root when a serving form proves it is also served at
its own root, from a closed list: `x.listen(...)`,
`http.createServer(..., x, ...).listen(...)` (Node's `http` and `https`,
also as `node:http`/`node:https`), and `s.listen(...)` with `s` a const
binding of such a `createServer` call, `x` a proven app. Its own-root paths
are conditional when every serving form is under a branch, loop, try or
short-circuit or inside a function body. A serving form in a test-scope
file serves only a test-scope app. Lambda and serverless wrappers and every
other form give no own-root path. Route order: Express passes a request a
mounted app or router does not answer on to the next entry of the app that
mounts it (Express 4.21.1 and 5.1.0), so a mount does not take a later
parent route by its prefix; a route of the mounted app that matches the
same request first is an earlier route handler (DA-E1), recorded in
`mounted_middleware_unknown`, and the later route keeps its path. A mounted
app or router whose own stack (or a router below it) holds middleware runs
it for every request that reaches the mount, so a catch-all among it (a
404 handler, a check that ends the response) answers a later parent route
below the mount's prefix first (Express 4.21.1 and 5.1.0: `/z/b` 404 after
`sub.use(notFound)` and `app.use("/z", sub)`). Such a route
keeps its paths, conditional, with assumption RO-V2 ("a mounted app's
catch-all may answer first") and `may_answer_first` naming the mount
(`catchSub (web/app.js:109)`); the path is never dropped, since
middleware that calls `next()` lets the route answer. The mark is per
route: when any mount chain of the route crosses such a mount, every path
of the route carries it.
use(prefix, router) appends the prefix;
use(router) preserves the current path. Prefixes require literals or
module-level const literal bindings, including imported ones. Traverse each
root with cycle detection per chain, retaining all distinct reachable paths.
No path is guessed through an unresolved mount or prefix.

Diagnostics name expressions and include registration/mount call byte spans in
their messages (contract v1 diagnostics have no structured span field):

- route_dynamic_pattern: warning, dynamic registration pattern.
- route_mount_unresolved: info, router argument cannot resolve.
- route_prefix_unresolved: info, prefix cannot resolve.
- route_unmounted: info, registration receiver has no resolved path from a root.
- route_trailing_slash_unproven: info, Axum: a path of the route depends on a trailing slash the crate's axum version does not prove (T029 FU-3b).
- route_pattern_invalid: warning, an Express 5 pattern path-to-regexp 8 refuses (EX5-W10).
- route_pattern_unmodelled: info, an Express path piece no row reads (EX5-W14).
- route_shadowed: info, FastAPI: a composed path of the route that a Starlette mount with a read prefix added before it takes; the path is not stored, and the route attribute `shadowed_paths` lists it with the mount. The message names the path and the mount.

`route_unmounted` names only a route below no hop: a route whose router is
reached through a stored hop (`unresolved_mounts`) is mounted, below a
prefix the code does not fix, and does not report it.

### CommonJS modules (T036)

A router, app or router factory that a module exports with CommonJS and
another module loads with `require` composes exactly as through an ESM
import: `app.use("/p", require("./routes"))` with `module.exports = router`
in `routes.js` gives the router's routes the paths below `/p`, and every
rule above (constant prefixes, conditional mounts, `unresolved_mounts` for
a prefix that is not a constant, consumed apps, route order) applies
unchanged. The exports are cached syntax (extractors-59); the rows below
are the only forms read, each to one row, and everything else is the
default D: no value, so a mount of it reports `route_mount_unresolved` and
its routes keep no path. Express 4.21.1 and 5.1.0 serve every stored path
of the fixture `testdata/fixtures/express-commonjs` with its own handler
(the T036 task record (internal) has the oracle). The fixture
`testdata/fixtures/express-commonjs-unscanned` holds the unscanned
candidates of row CJ-U1: Node 24.22.2 with Express 4.21.1 and 5.1.0 serves
the unscanned module's routes there, never those of the scanned later
candidate.

| id | form | value | refused |
|---|---|---|---|
| CJ-U1 | `require("spec")` with a string-literal relative specifier (`./`, `../`) in a `.js` or `.cjs` file, `require` not shadowed, resolved as Node's CommonJS loader resolves it over the scanned files: the path, then with `.js`, `.json`, `.node` appended, then the directory's `index` with the same extensions; the first that exists, when it is a `.js` or `.cjs` file | the module's CommonJS value (CJ-E1, or the exports object of CJ-E2 and CJ-E3) | a specifier that is not a literal, a conditional expression of requires, a specifier no candidate matches (a TypeScript file required from JavaScript: Node loads none), a JSON or native module, a directory with its own package.json, a TypeScript or ES module requirer, a module without CommonJS exports (an ES module: `require` returns its namespace object): no value; a candidate before the first scanned one that exists on disk but was not scanned (an entry the scan skipped: ignored by `.gitignore`, `.graphveinignore` or a built-in ignored name, unreadable, binary or a symlink, or dropped after reading; a skipped directory stands for everything below it; read from the scanner's own list, never by following a link): Node loads code Graphvein never read, so no value, and the mount's `route_mount_unresolved` names the entry (`(loads web/routes.js, not scanned)`) |
| CJ-U2 | `require("spec").name` with CJ-U1's specifier | member `name` of the exports object (CJ-E2, CJ-E3) | a member the module does not write, a member of a CJ-E1 value that is not an object literal: no value |
| CJ-U3 | `const { name, key: local } = require("spec")` (any declaration keyword) | `name` and `local` are `require("spec").name` and `require("spec").key` (CJ-U2, or the package's member for a package specifier: `const { Router } = require("express")` is express's Router) | a property with a default value, a nested pattern, a computed or string key, a rest element: the name binds nothing |
| CJ-U4 | `require("pkg")` with a package specifier | the package's default export, read by the factory allowlist (unchanged) | - |
| CJ-E1 | `module.exports = E` as a statement of the module's top level, written once | `E` read in the module's top scope: a router, an app, a function whose call returns one (the existing factory rule), another `require`; an object literal is the exports object of CJ-E2 | see CJ-E4 |
| CJ-E2 | `module.exports = { name, key: E }` (CJ-E1) without a spread element | member `name` is the shorthand binding, member `key` is `E` | an object literal with a spread element: no member |
| CJ-E3 | `exports.name = E` or `module.exports.name = E` as top-level statements, each name written once, no CJ-E1 write | member `name` is `E` | see CJ-E4 |
| CJ-E4 | any other write of the exports object: inside a function, branch or loop, a second write of one name or of `module.exports`, `exports = E`, a computed or deeper member, `Object.assign` or `Object.defineProperty` on `exports` or `module.exports`, CJ-E1 beside CJ-E3 writes, an ESM `export` in the same file | none: the module's exports are unread, no value for any `require` of it | - |
| D | every other form | no value | - |

The loader's resolution ignores compiler options (`jsconfig.json`,
`tsconfig.json`): `require` is resolved at run time, never as an import.
Recall left: exports written conditionally or through `Object.assign`
(Express serves them when the branch runs), `require` in TypeScript files
(their compiled layout is not read), a directory's package.json `main`,
and package specifiers other than the factory allowlist. A module Node
would load that is not in the scanned files (an ignored generated file)
is not seen: the next candidate may be read in its place.

Resolved non-inline handlers get handler -> route exposes_route edges,
provenance fact, confidence 1, with registration evidence. Existing callable
resolution and repeated-symbol ambiguity apply. Barrel handlers use shared export resolution; inline and unresolved handlers
emit no handler edge.

## FastAPI (ADR 0015)

Recognize external imports only: FastAPI/APIRouter from fastapi, FastAPI from
fastapi.applications, and APIRouter from fastapi.routing. Aliases and namespace
imports work; scanned modules shadow installed packages. Use the shared
[Python resolver](../lang/python-resolution.md) for bindings, handlers and
prefixes. Local apps/routers count within their binding function's body.

HTTP decorators get/post/put/patch/delete/options/head/trace produce one route.
api_route and add_api_route expand distinct uppercase literal methods from a
list, tuple or set. Missing/None methods default to GET; empty literal methods
collections produce one ANY route, matching Starlette's unrestricted method set.
Other methods expressions produce route_dynamic_methods warnings. Unknown
keyword unpacks cannot prove omitted prefixes or methods, so defaults are not
guessed through them. Registration
paths require strings or replacement-free f-strings; implicit concatenation is
supported. Websocket/websocket_route produce no HTTP nodes. Stacked decorators
share one handler. add_api_route resolves endpoint arguments; lambdas are inline
and unavailable endpoints retain handler_expression. Middleware is empty:
FastAPI dependency injection is not modeled as middleware; the security
dependencies a route declares are the attributes `security_dependencies` and
`security_dependencies_unknown` ([auth extraction](auth.md), T026 AG-B4a).

Decorator spans/hashes cover only the call expression, excluding @ and the
decorated body. The enclosing named function or file contains the route; the
decorated function gets the exposes_route edge. Route identity always uses the
unprefixed registration pattern, including any convertor spelling.

Compose APIRouter's keyword-only prefix, include_router's keyword-only prefix,
and Starlette mount paths by concatenation before normalization. Mount only
proven FastAPI apps; other ASGI apps are ignored. Every FastAPI app is a root
unless a Starlette mount consumes it (T035): a mounted app answers only below
its mount (FastAPI 0.121.1/Starlette 0.49.3 and FastAPI 0.165.0/Starlette
1.10.0 answer 404 at the sub-app's own paths), so its routes get only the
paths composed through the mount. An `x.mount(p, app)` consumes `app` whether
or not `p` is read (an unread `p` is a hop: the routes keep no path and store
it in `unresolved_mounts`) and whether or not `x` is read (an unread `x`:
`route_mount_unresolved` naming the call, no path). `x.router.mount(p,
app)` with `x` a proven FastAPI app is `x.mount(p, app)` (FastAPI's `mount`
calls `self.router.mount`). A Starlette `Mount(p, app)` call
(`starlette.routing.Mount`, also through `fastapi.routing`) consumes `app`.
Written directly as an element of the list literal passed as `routes=` to a
`FastAPI(...)` constructor that is a proven app outside a helper context, it
composes as `x.mount(p, app)` on that app (step EM: FastAPI passes `routes=`
to its router; a constant `p` gives the paths through the mount, any other
`p` is a hop, as for `mount`). Any other position (an `APIRouter(routes=...)`
list, whose `Mount` an `include_router` does not copy in FastAPI 0.121.1; a
list bound to a name or built elsewhere; a constructor inside a helper) is
not composed: the call is named in `route_mount_unresolved` and the app's
routes keep no path. A mount in a test-scope file consumes only a
test-scope app; a production app only tests mount keeps its own root. A
project class named `Mount` consumes nothing.

`router.mount(p, app)` on an `APIRouter` composes only where
the nearest pyproject.toml requiring fastapi proves a lowest version of
0.137.0 or later (as the route shape reads it for version-dependent forms):
FastAPI 0.137.0 introduced lazy included routers and serves the mount
below the include's prefix, without the mounting router's own prefix
(`APIRouter(prefix="/p")` mounting at `/rm`, included with prefix `/i`:
`/i/rm/...`); 0.136.3 and earlier drop it at `include_router`. Without that
proof the mounted app is consumed and the call is named in
`route_mount_unresolved` (no path); with it, a prefix that is not a
constant gives `route_prefix_unresolved` and no path.

Route order: Starlette dispatches an app's routes, mounts and
included routers in the order they were added, and a mount answers every
request below its prefix, 404 included. A composed path of a route reached
through an entry added after a mount on the same app or router (or in an
order no rule proves: another file, another function body or helper
context) is not stored when the mount's prefix takes it (info
`route_shadowed`): a path strictly below the prefix, a trailing-slash
path equal to it, any path for a mount at `""`; a prefix parameter takes
any segment. The prefix itself is not taken, and a template parameter
below a literal prefix segment is taken only for some values (the path is
kept). Every `mount` call on a read app or router counts, whatever it
mounts (`StaticFiles` included); a `Mount` in a constructor's `routes=`
list literal comes before every call on the object. A mount whose prefix
is not read may take a later path or not (a static or media mount at a
configured URL before the routes is a common layout): the path is kept,
conditional, with assumption RO-V1 ("a mount at an unknown prefix may
answer first") and `may_answer_first` naming the mount; only a
read prefix that takes the path withholds it. A route whose every path is
withheld stores them in the route attribute `shadowed_paths` (`path`,
`by`: the mount), so it is shown as shadowed, never as unmounted. Route
order between two routes is not modelled.

Route-order assumptions (shown with the path wherever path assumptions
are shown, and on every client or project link to the path: the link
carries the ids in its `assumes`, see "Path assumptions on links" in
[client calls](clients.md)):

- **RO-V1** A FastAPI mount added before the route whose prefix the code
  does not read does not take this path (FastAPI 0.165.0 / Starlette
  1.10.0 serve it when the prefix is elsewhere, answer 404 when the prefix
  covers it).
- **RO-V2** An Express app or router mounted earlier below a prefix that
  may match passes this path's requests on (its middleware calls `next()`
  for them; Express 4.21.1 and 5.1.0 answer with the mounted stack's
  catch-all otherwise).

A consumed app stays a root when served at its own root by a form from a
closed list: `uvicorn.run(x, ...)` or `uvicorn.run(app=x)` with
`x` a proven app (conditional under a branch or loop, the
`if __name__ == "__main__":` guard included, or inside a function), and
the `[tool.fastapi] entrypoint = "pkg.mod:name"` of a committed
pyproject.toml naming a module-level binding of a proven app in
`<manifest dir>/pkg/mod.py` or `<manifest dir>/pkg/mod/__init__.py`
(fastapi-cli 0.0.34's `fastapi run` imports it from the manifest's
directory; unconditional). A serving form in a test-scope file serves only
a test-scope app; any other form (a command line, a Dockerfile, a string
passed to `uvicorn.run`) gives no own-root path.
Includes are order-insensitive live compositions;
current FastAPI (>=0.137) uses lazy included routers, while older versions copy
registrations at inclusion time. General runtime include-order analysis is not modeled.

Prefix resolution accepts literals, stable module-level literal assignments,
and settings_default class literals. Settings defaults require an unconditional
single module instance of a scanned direct BaseSettings subclass, with no
positional args or override of the requested own-class field. Arbitrary keyword
unpacking stays unresolved. Inheritance, Field defaults, validators and
environment values are not evaluated. This resolution is explicitly
environment-overridable. A chain takes its weakest resolution
(settings_default < constant < literal); duplicate templates retain the
strongest resolution and become unconditional if any chain is unconditional.

Includes/mounts in if/elif/else, match cases, loop bodies/else, except/try-else,
or conditional expressions set conditional=true. Try bodies, finally, with and
the object's binding function body are unconditional. Cycles terminate per
chain. Unavailable prefixes/mounts and unreachable routers reuse the shared
route_prefix_unresolved, route_mount_unresolved and route_unmounted diagnostics.

### Helper parameters and trailing slashes (ADR 0019)

At explicit uniquely resolved calls, bind an undecorated module-level helper's
parameters using Python positional-only, positional/keyword and keyword-only
rules. Validate the whole call first; defaults establish no object. Variadic
parameters accept extra explicit arguments according to Python, but variadic
containers and calls using argument unpacking establish no binding. Rebound
parameters are excluded, including assignment, annotation, augmented assignment,
loop targets, with-as, walrus, deletion and global/nonlocal declarations.
Methods and nested/decorated functions are excluded. Async helpers require an
explicit await; creating a coroutine alone does not execute the body.

Resolve supplied arguments at the call site using the shared Python resolver.
Proven apps/routers can replace include_router/mount receivers and included
router/app arguments; annotations do not prove objects. No parameter prefixes,
default expressions or enclosing-helper parameters are followed. Each call gets
its own local object context. Function-local imports already resolve under the
shared rules. Uncalled helpers add no parameter-derived chain. Registrations
on parameter receivers are outside this task. Body and call-site conditions
combine with OR using the existing conditional-context list.

For FastAPI only, concatenate the full raw root/prefix/mount/registration path
before normalization. Emit trailing_slash: true only if it ends with slash and
the normalized template is not root; otherwise omit the key. Entries with the
same template and different slash flags are distinct. Merge condition and proof
strength only within the same template/flag pair; sort template then false before
true. Express and Axum never receive this flag. Client matching remains unchanged.

## Axum (ADR 0016)

Use the shared [Rust resolver](../lang/rust-resolution.md). Recognition requires
external axum::Router::new/default (including generic Router::<S>) and routing
constructors. Scanned libraries named axum shadow the external crate. Aliases,
groups, globs and qualified paths work; receiver/type names cannot prove a router.

Inline get/post/put/patch/delete/head/options/trace/connect produce their explicit
verb; any produces ANY. on accepts literal MethodFilter verb constants combined
with bitwise |. Service variants preserve handler_expression and a null handler.
Chained verbs add registrations; method-router layer and other methods are
passthroughs. Variable method routers and nonliteral filters emit
route_dynamic_methods with no route. route_service produces one ANY registration.
Axum's implicit HEAD behavior is not a separate source registration.

Normal/raw string literals and const/static &str literal initializers, including
imports, prove patterns and prefixes. Other expressions emit route_dynamic_pattern
or route_prefix_unresolved. Const patterns also give constant path resolution.
Identity uses the original unprefixed pattern spelling. Constructor verb spans
cover the whole call; chained verb spans begin at the method name and end at the
closing parenthesis, keeping sibling hashes independent. Closures are
handler_inline; resolved callable paths receive exposes_route edges; unresolved
paths retain handler_expression. middleware is empty; router layers are the attributes `layers` and `layers_unknown` ([auth extraction](auth.md), T026 AG-B4b).
The nearest named function, otherwise file, contains the route.

Follow ordered let bindings, let shadowing and mut assignment within a function
and lexical blocks. Named factory tail/return expressions reuse their returned
object at resolved calls. Router methods preserve that object. nest prepends
its prefix; merge preserves the path. nest_service with a resolved router
argument composes as nest (T029 FU-3d), except that the router's `/` route
is answered at the prefix and at the prefix with a trailing slash (both
claimed only for a crate whose axum version is proven within 0.6.20 to
0.8.x; axum 0.6.20, 0.7.9 and 0.8.8 checked, 404 at the root); a
nest_service prefix ending with `/` is not claimed. A nest_service whose
argument is no resolved router (a router wrapped in another service)
emits route_mount_unresolved, and the routers its argument holds are
consumed: their routes get no path instead of a root path. Concatenate, convert
leading :name/*name segments to {name}/{*name}, preserve braces, then collapse
slashes/remove trailing slashes except root. A nested / becomes the prefix.
Unavailable routers/prefixes reuse route_mount_unresolved, route_prefix_unresolved
and route_unmounted; no path is guessed through them.

Trailing slashes (T029 FU-3b): axum serves a pattern as written. Checked
on 127.0.0.1 against axum 0.8.8, 0.7.9, 0.6.20 and 0.3.4 (the T029 FU-3
task record, internal): 0.6.20 to 0.8.8 answer `route("/t/")` only at
`/t/`, `nest("/n", r)` with r's `/` only at `/n`, r's `/t/` at `/n/t/`,
and `nest("/p/", r)` with r's `/x` at `/p/x` and r's `/` at `/p/`; 0.3.4
answers the nest at `/p/` with `/p//x` and redirects the other slash
variant with 301. A path whose composition holds a pattern (other than
`/`) or a nest prefix ending with `/` is composed exactly for a crate whose axum version is proven within 0.6.20 to 0.8.x (rows SH-X11, SH-X25; 0.6.20, 0.7.9 and 0.8.8 were checked):
each nest prefix's trailing `/` is dropped before the next part, the
root `/` below a prefix is the prefix as written, and the entry keeps
`trailing_slash: true` when the path ends with `/`. For any other crate
such a path is not claimed (`route_trailing_slash_unproven` naming the
registration; when it was the route's only path, the route has no path).
A doubled slash and `nest("/")` are not claimed either. Paths without
such a slash are composed as before.

Every chain not consumed by a resolved nest/merge is a root, including dead code.
Merges/nests are intentionally order-insensitive. Traverse with per-chain cycle
guards. Registration/composition/rebinding in if/else (including if let), match
arms, loop bodies or cfg-bearing syntax marks paths conditional. The owning
function body is unconditional. Duplicate templates keep strongest resolution
and become unconditional if any chain is unconditional.

## Go: net/http ServeMux, chi v5, gin v1 (ADR 0076)

Task T023. The Go route linker (`internal/extract/routes/gohttp*.go`) reads
the cached Go router payload of gated files ([go-routes](../lang/go-routes.md))
and the retained `go.mod` sources at link time; it never re-parses a file.
Step GR2 (recognition, objects, registrations, patterns and handlers)
emits route nodes, `exposes_route` edges and diagnostics; step GR3
composes their paths (rows GR-C1-GR-C5, "Composition" below)
adds serving positions (GR-C6) and step GR5 path-preserving wrappers
(GR-O7).

Recognition comes from resolved imports and proven objects, never from
names: `r.Get(...)` on something no row proves a router (an HTTP client's
`c.request.Get(ctx, "/v1/me")`) is no route and is not counted. The import
names are the package clauses (`http`, `chi`, `gin`) unless aliased; a
scanned module whose path is the import path (or a prefix of it) shadows
the external package. A file outside a scanned module is not read.

Objects: constructors (GR-K2), zero-value `ServeMux` values (GR-K4), the
DefaultServeMux (one per module), router-typed parameters (GR-O4), chi
`Route`/`Group` callback parameters and children (GR-O1), `With` children
(GR-O2), gin groups (GR-O3), factory returns (GR-O5) and `ServeHTTP`
method values and `http.Handler(x)` conversions (GR-O6), and calls of
path-preserving wrappers (GR-O7). A name's object
is read from the bindings that may reach the use (GR-K3): the last one
that runs on every path before the use, the later ones in other blocks,
and every one in an enclosing `for` block. A value one of whose reaching
bindings is not proven holds no object. Objects are identified by their
syntax site, so a factory's object exists once however often it is
called; a parameter holds, per resolved call, the caller's objects (the
bindings step GR3 composes through). A factory whose return is one of its
own parameters is not read (its value depends on the call).

Registrations on proven objects (GR-R1-GR-R4) are route nodes in the
shared shape (identity `route:<file>#<METHOD> <pattern>`, `@2`, `@3` for
repeats in a file, level 4, call span and hash, contained by the nearest
named function or the file). A net/http registration whose handler holds
a ServeMux (directly, as `x.ServeHTTP`, `http.Handler(x)` or inside
`http.StripPrefix`) is a composition (GR-C3), not a route; one whose
handler holds a router only inside another call or a closure is a wrapped
router (`wrapped_router`), not a route either. A net/http pattern is
checked with Go 1.27.1's `ServeMux` pattern parser, and each constructor,
zero-value or default mux's patterns are registered in source order with
its conflict check: a pattern that conflicts with an earlier one is
refused (`invalid_pattern`, warning `route_invalid_pattern`). Both are
transcribed from `net/http` (`gohttp_mux.go`), so the indexer imports no
network package; `TestGoMuxPatternOracle` checks the transcription
against the real `ServeMux`.

| Attribute (Go) | Meaning |
|---|---|
| framework | `net/http`, `chi` or `gin` |
| method | the pattern's method (net/http), the verb, the constant of `Method`/`Handle`/`Match`; `ANY` for a method-less net/http or chi registration and gin `Any`; GET and HEAD for the gin static forms |
| pattern | the constant pattern as written (net/http: with its method and host) |
| handler, handler_inline, handler_expression | GR-H1-GR-H5; a gin static route's expression is the method name; a gin registration without handler arguments has neither |
| middleware | gin arguments before the handler; chi `With` arguments of the receiver (accumulated over chained `With`); a function literal is `func(...){...}` |
| host | the host of a net/http host pattern |
| assumptions | `GR-V2` on net/http routes; `GR-V1` on chi and gin routes of a module whose `go.mod` requires another version than the verified ones (or none) |
| paths | the entries composed from every root (GR-C1-GR-C6); `trailing_slash` as ADR 0019; `assumptions` (`["GR-V5"]`) and `served_through` on an entry reached only through a wrapper at a serving position that GR-O7 does not prove path-preserving |
| scope | test for a test-scope file |

### Composition

The linker builds a composition graph over the router objects: the
children of chi `Route`, `Group` and `With` and gin `Group` (and `Use`,
the same object), the GR-O4 parameter bindings (each resolved call binds
the caller's objects), chi `Mount(p, x)` with `x` a proven chi router
(GR-C1) and net/http `Handle`/`HandleFunc` compositions of a ServeMux
(GR-C3). Roots (GR-C4) are the constructor, zero-value and default mux
objects no composition consumes, that are not stored into a composite
literal (`escaped_router`), not reached only through another call or a
closure at a composition position (`wrapped_router`: the handler of a
mux registration or a chi `Mount`, including a module function's returns
reached from there and the names a function literal uses; a router a
wrapped parameter carries is wrapped too), not served by an
`http.StripPrefix` that no GR-C3 composition or GR-C6 serving position
reads (`wrapped_router`), and not used directly as a registration's
handler. Dead code and test files keep their roots.

Serving positions (GR-C6) decide where a root's paths start:
the handler argument of `net/http`'s `ListenAndServe`,
`ListenAndServeTLS`, `Serve` and `ServeTLS` and the `Handler` field of an
`http.Server` literal, read in every captured record's bindings, returns
and calls (the payload keeps serving calls and the bindings holding
`StripPrefix`, a serving call or an `http.Server` literal). The
handler is followed through the bindings that may reach a name, `&`,
`x.ServeHTTP`, the `http.Handler` and `http.HandlerFunc` conversions,
`http.TimeoutHandler` and `http.MaxBytesHandler` (their Go 1.27.1 source
serves the same request path), the returns of a module function (a
parameter's object there is not recorded: it depends on the call),
`http.StripPrefix(p, x)` (Go 1.27.1: the prefix is removed before the
inner handler sees the path, a path without it is answered 404, `""`
returns the handler itself) and any other call, a wrapper whose
arguments are followed with the wrapper named. A serving position in a
helper whose router-typed parameter is bound at resolved calls serves the
bound objects; one in a test-scope file (or reached through a binding in
one) does not serve a production root. A root with serving positions is
served only through them: each is a chain start with the prefixes (in order) before the
root's own paths, and with assumption GR-V5 when it crosses a wrapper
(a start with a prefix or a wrapper is conditional when its serving
position or a binding on the way is; a direct one adds nothing to the
root's own chain, so its context is not carried);
without serving positions (dead code, a router served by another
framework's `Run`, a serving position the payload does not see) the root
is served at its own paths (ADR 0016). A prefix that is not constant,
does not start with `/` (no request path has it), ends with `/` (the
inner router would see a path without its leading `/`) or holds `{`,
`}`, `*` or `:` blocks that start (`route_prefix_unresolved`); a root all
of whose starts are blocked is no root. A router used inside a function
literal at a serving position is wrapped (`wrapped_router`), as at a
composition position. The `Handler` of an `http.Server` set by an
assignment `srv.Handler = h` is a serving position too when `srv`'s
stated type (a parameter or `var` declared, or a `:=` of a composite
literal, as GR-H2 types a receiver) is `http.Server` or `*http.Server`
.

Path-preserving wrappers (row GR-O7): a call of a module
function or method whose syntax proves that it serves the request it
receives with the same path (`docs/lang/go-routes.md`, "Wrappers and
request helpers": it returns its `http.Handler` parameter or an
`http.HandlerFunc` literal that calls `h.ServeHTTP(w, r)` or
`h.ServeHTTP(w, r.WithContext(c))`, and every use of the request that may
run before that call reads it or passes it to a module function or method
that only reads it, recursively) holds its handler argument's objects, as
`http.Handler(x)` does. Compositions (`root.Handle("/api/",
withAuth(api))`, `r.Mount("/x", cors(sub))`) and serving positions
(`http.ListenAndServe(addr, cors(mux))`) read through it, with no
assumption. A wrapper serving `r.WithContext(c)` passes no chi router:
chi keeps its routing state in the request's context. Every other
wrapper is GR-C4's `wrapped_router` at a composition position and
assumption GR-V5 at a serving position.

Forwarding closures, methods of a call result and the ServeMux -> chi
hop (rows GR-O8, GR-O9 and GR-C7). A chi router registered at
the pattern `/` of a root ServeMux (`m.Handle("/", r)`,
`m.HandleFunc("/", r.ServeHTTP)`), or reached there through a function
literal that only forwards the request to it (`m.HandleFunc("/",
func(w, req) { ...; root.ServeHTTP(w, req) })`, the forwarding proof of
`docs/lang/go-routes.md`, with `root` written once), keeps its paths: Go
1.27.1's `ServeMux.ServeHTTP` hands the request on unchanged apart from
`r.Pattern` and the wildcard matches, and chi routes on the URL path.
The `/` pattern is the mux's least specific, so a path is claimed only
when no other pattern of that mux (or of a parameter bound to it), no
`/tree` -> `/tree/` redirect of one and no string literal the closure
switches on or compares `r.URL.Path` with (its shadows: it answers those
itself) may match any request of the path; a mux with a `Handle` call of
unknown pattern claims none. Such a route states assumption GR-V2; a
refused path names the route in `route_mount_unresolved`. A method called
on a call result, `handlers.New(h).Handler()` with `New` declaring one
result of a module type `T`, is read as `T.Handler`'s returns (GR-O5), so
a router built in that method, or a generated server's router (GR-G1), is
mounted where the call appears.

Each root is traversed with a per-chain cycle guard; every distinct chain
state is kept. A chain's path text is framework syntax: chi appends
`TrimSuffix(prefix, "/")` (a prefix must start with `/`, else
`invalid_pattern`; a regexp that can match `/` is `regexp_param`), gin
applies `joinPaths` from the engine base `/`, and net/http records each
composition hop (outer pattern, `StripPrefix` prefix). A net/http
registration's pattern is composed through its hops innermost first: the
prefix goes before the inner path, keeping the inner method and host, and
the hop holds only when its outer pattern is equivalent or more general
than the composed pattern by net/http's own pattern relations and its host
is empty or the same (PS-GR-3). Compositions take part in the mux's
conflict check like registrations (a later conflicting one is
`invalid_pattern`). A prefix or outer pattern that is not constant
(GR-P1) blocks the edge with `route_prefix_unresolved`; a StripPrefix
prefix with `{` or `}` is refused the same way (the composed pattern would
read it as a wildcard). A hop whose StripPrefix prefix does not start
with `/`, or whose composed path holds `//` (the outer mux cleans the
path and redirects, so the hop never dispatches it), claims no path
through that chain (found while building the oracle). A mux
handler that may also hold an unproven value blocks the composition
(`route_mount_unresolved`).

Path entries (GR-P2 templates) carry `conditional: true` when the
registration, a composition, a child call, a parameter binding's call or
every binding that carries the object to its use is in conditional context
(GR-C5), `resolution` `constant` when any prefix or the pattern is a
constant, and `scope: test` only when every reaching chain is test-scope
(root, composition and binding files and the registration). Entries merge
by template and trailing slash (unconditional and production when any
chain is, the strongest resolution). A path entry reached only through
chains that cross a wrapper at a serving position carries
`assumptions: ["GR-V5"]` and `served_through` (the wrappers' names, for
example `["cors"]`); an entry that another chain reaches without a
wrapper has neither. A client or project link to the entry carries GR-V5
in its `assumes` ([client calls](clients.md), "Path
assumptions on links"). A route whose objects no root
reaches gets `route_unmounted` unless a refusal already named the cause (a
blocked edge, a wrapped or escaped object, a chi mount of another
framework's router).

Each step is isolated: a failure while recognising or reading a file,
composing from one root or composing one registration's paths becomes a
`go_routes_failed` warning; the other files, roots and routes are kept.
The composition graph (objects with each root's serving positions, edges
with their refusals, registrations with their paths and, per chain, the
root, the served prefix and the raw path the root router sees) is
exported to tests (`goRouteGraph`).

Oracle: `TestGoRoutesMuxOracle` (in the normal suite)
registers the fixture's net/http registrations and compositions in real
Go 1.27.1 `ServeMux`es and dispatches a sample request for every claimed
chain through its root and served prefix (also HEAD for a GET pattern);
with `GRAPHVEIN_GO_ROUTES_CENSUS_CORPUS` set it does the same on the five
corpora. `scripts/oracle/go_routes/run.sh` (opt-in, its own Go module)
rebuilds the chi and gin trees at chi v5.2.4 and v5.5.0, gin v1.10.0 and
v1.12.0, and compares `chi.Walk` and `Engine.Routes()` listings and
dispatch; since step GR7 both oracles also rebuild a root ServeMux with a
chi router at `/` (its sibling registrations and compositions, and the
closure's shadow cases answering themselves) and dispatch the chi chains
claimed from that root through it. Both rebuild the trees from
Graphvein's own composition graph, so they check composition and
dispatch given Graphvein's recognition. Since step GR8 the
same script also runs the **source oracle** (`TestGoRoutesSourceOracle`):
it compiles the fixture's module `testdata/fixtures/go-routes/source`
(Graphvein's own test code) with the real `net/http`, chi and gin at both
version pairs, builds every case's handler by calling the case function,
and dispatches every path Graphvein claims (the request must reach the
route's own tagged handler) and every refused route's own pattern
(reported, never a disagreement). It checks roots, sibling sets, wrapper
and forwarding proofs and handlers, not only composition. Facts the oracle compares carry no label of their own; the
task summary (the T023 task record (internal)) lists which are
oracle-checked and which stay mapped.

A Go path entry of an `ANY` route may be partial by method: when a
sibling pattern or outer pattern takes only some methods of the composed
path (an outer `GET /api/users` beside a mounted `ANY /users`), the
route keeps the entry, since it still answers the other methods (GR-C3
refuses only a sibling that takes every request of the path; step GR8,
review confirmation pass). The client matcher's method check then may
link a call of a method the sibling answers.

### Go claim contract

Copied from `docs/contract/claims/go-http-routers.md`; `TestGoRoutesContract`
keeps this table, the task's table and the code table
(`gohttp_contract.go`) 1:1.

| id | modelled form | facts claimed | refusal (to D, counted form) / diagnostic |
|---|---|---|---|
| GR-K1 | a Go file of a scanned module (GS-M1) importing `net/http`, `github.com/go-chi/chi/v5` or `github.com/gin-gonic/gin`, by plain or aliased import (resolved as GS-I4 external imports; a scanned module with one of those paths shadows it) | recognition: the file's router calls can be read | dot import of a router package (`dot_import`); `github.com/go-chi/chi` before v5 (`unsupported_module`); for `net/http` registrations, GR-V2 not met (`legacy_mux_patterns`) |
| GR-K2 | constructors `http.NewServeMux()`, `chi.NewRouter()`, `chi.NewMux()`, `gin.New()`, `gin.Default()`; the DefaultServeMux through package-level `http.Handle`, `http.HandleFunc` and `http.DefaultServeMux` | a router object (framework, creation site) | - |
| GR-K3 | ordered local bindings in one function body: `:=`, `=`, `var x = e`, `var x T = e` (an interface type such as `http.Handler` included), shadowing in nested blocks, as Axum's ordered let bindings | the object a name holds at each use | a binding that holds an unproven value on some path reaching the use (`if r == nil { r = chi.NewRouter() }` after `r := options.BaseRouter`), a struct field, package-level variable, map or slice element, type assertion or channel receive as receiver (`receiver_unproven`) |
| GR-K4 | zero-value `ServeMux` objects: `var m http.ServeMux`, `m := http.ServeMux{}` (or `var m = http.ServeMux{}`), `&http.ServeMux{}`, `new(http.ServeMux)` (Go 1.27.1 `net/http/server.go`: `NewServeMux` returns `&ServeMux{}` and `DefaultServeMux` is a zero `ServeMux`, so the zero value is a ready mux) | a router object (net/http, creation site); a value variable is read as a receiver, through `&` and as `m.ServeHTTP` | a value variable read as a plain value (a copy of the mux) holds no object (`receiver_unproven` when it is a receiver) |
| GR-O1 | chi `r.Route(p, f)` and `r.Group(f)`, `f` a function literal or a module function (identifier or `pkg.F`) whose first parameter is `chi.Router` | a child object: `Route` with prefix `p`, `Group` with the same path; the callback body unconditional relative to the call (GR-V3); `Route` and `Group` also return that child | any other callback (`callback_unread`) |
| GR-O2 | chi `r.With(mw...)` | an inline child with the same path; `mw` are the middleware of registrations made directly on it | - |
| GR-O3 | gin `g.Group(p, mw...)`; `g.Use(mw...)` used as a value | `Group`: a child with prefix `p` (GR-C2); `Use`: the same object | - |
| GR-O4 | a router object passed as an argument at an explicit call of a module function or method resolved by the Go linker's rules, to a parameter declared `*http.ServeMux`, `chi.Router`, `*chi.Mux`, `*gin.Engine`, `*gin.RouterGroup`, `gin.IRouter` or `gin.IRoutes` (ADR 0019's helper rules: each call its own context, call-site conditions OR body conditions) | the parameter holds that object in that call's context | a parameter that is reassigned (`param_rebound`); a parameter no resolved call binds keeps its registrations without a path (`route_unmounted`, as gotify's plugin `RegisterWebhook`) |
| GR-O5 | a call of a module function or method whose every `return` returns a proven object of one framework (recursively, cycle-guarded) | the call's value is that object (each object once, not copied) | a method called on a call result other than GR-O9's (`factory_method_chain`); a function with another return |
| GR-O6 | `x.ServeHTTP` (method value) of a proven object; a conversion `http.Handler(x)` | the object itself | - |
| GR-O7 | path-preserving wrappers: a call `f(x)`, `pkg.F(x)` or `v.M(x)` (`v` typed as GR-H2 types a receiver) of a module function or method with one `http.Handler` parameter and one `http.Handler` result whose syntax passes the wrapper proof (`docs/lang/go-routes.md`, "Wrappers and request helpers": every return is the parameter or `http.HandlerFunc(func(w, r) {...})`; the parameter is used only in `h.ServeHTTP(w, r)` or `h.ServeHTTP(w, r.WithContext(c))` statements of such a literal; every use of `r` that may run before or during such a call reads the request, rebinds `r = r.WithContext(c)` or passes `r` to a module function or method that only reads it, recursively), `x` its `http.Handler` argument | the call holds `x`'s objects, as `http.Handler(x)` does (GR-O6): compositions (GR-C1, GR-C3) and serving positions (GR-C6) read through it, without assumption GR-V5 (Go 1.27.1: `ServeMux` dispatches on the request's `RequestURI`, `Proto`, `Method`, `Host` and `URL`, which the proof leaves unchanged; `r.WithContext` shares the `URL`) | a wrapper that serves `r.WithContext(c)` around a chi router (chi keeps its routing state in the context); any other wrapper: at a composition position GR-C4's `wrapped_router`, at a serving position GR-C6's assumption GR-V5; a wrapper with a branch that ends or serves chosen by a condition reading `r.URL` or `r.RequestURI` (it answers some paths itself) or calling a request helper that reads the URL or the method, directly or through its passes, or that no rule resolves: not path-preserving; a wrapper that answers a method itself in `if r.Method == M { ...; return }` (M a string literal or `http.MethodX`, no else) or a switch on `r.Method` with ending clauses: routes of M and ANY routes get no path through it, any other deciding read of `r.Method` refuses the proof |
| GR-O8 | forwarding closures: a function literal `func(w, r *http.Request)` that is the handler argument of a GR-C7 registration and passes the forwarding proof (`docs/lang/go-routes.md`, "Forwarding closures": its only serving calls are `X.ServeHTTP(w, r)` statements with the request unchanged and outside every branch chosen by `r.URL`; every use of `r` before or during them reads it or passes it to a request helper; `r.URL` is read only as `r.URL.Path` switched on with string-literal cases or compared `==` with a string literal in an `if` condition), `X` a local variable written exactly once (its declaration) or a parameter never assigned, whose value holds proven chi router objects | the literal holds `X`'s objects, as `http.Handler(X)` does (GR-O6); the string literals `r.URL.Path` is switched on or compared with are shadows: a composed path one of them may match is refused (GR-C7) | any other literal, a closure forwarding to a ServeMux, `X` written more than once, a pass GR-O7's helpers do not prove: GR-C4's `wrapped_router` as before; a deciding condition that calls a request helper reading the URL or the method, or one no rule resolves: no proof; a method answered in `if r.Method == M { ...; return }`: routes of M and ANY routes get no path through the closure |
| GR-O9 | a method called on a call result: `f(args).M()` or `pkg.F(args).M()` with `f` a module function that declares exactly one result of a module type `T` or `*T` (GR-H2's typing of a call result) and `M` a captured method of `T` (`handlers.New(h).Handler()`) | the call's value is what GR-O5 reads from `M`'s returns (objects built inside `M`, or a generated server's router, GR-G1) | `M` returning anything else (a field of the receiver, a parameter, a value no row proves) or not resolved (`factory_method_chain`) |
| GR-R1 | net/http `m.Handle(p, h)` / `m.HandleFunc(p, f)` on a ServeMux object (GR-K2), `p` constant (GR-P1) and accepted by Go 1.27.1's pattern parser, `h` not a router object (GR-C3 reads those) | one route: method from the pattern (`GET /x` -> GET, which also answers HEAD; no method -> ANY); `host` attribute for a host pattern; `pattern` the literal as written | a pattern the parser rejects or that conflicts with an earlier pattern on the same mux (`invalid_pattern`, warning `route_invalid_pattern`) |
| GR-R2 | chi `Get/Post/Put/Patch/Delete/Head/Options/Connect/Trace(p, h)`; `Handle(p, h)`, `HandleFunc(p, f)` (ANY, or the method of a `"METHOD /p"` pattern, PS-GR-4); `Method(m, p, h)`, `MethodFunc(m, p, f)` with `m` a string constant or `http.Method*` | one route per registration with that method | a non-constant method (`dynamic_method`, warning `route_dynamic_methods`); a method chi rejects or a pattern not starting with `/` (`invalid_pattern`) |
| GR-R3 | gin `GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS(p, h...)`; `Any(p, h...)` (ANY); `Handle(m, p, h...)` with constant `m`; `Match(ms, p, h...)` with `ms` a slice literal of string constants or `http.Method*` | one route per distinct method (`Any`: one ANY route) | a non-constant method or methods list (`dynamic_method`) |
| GR-R4 | gin `Static(p, dir)`, `StaticFS(p, fs)`, `StaticFile(p, file)`, `StaticFileFS(p, file, fs)` | GET and HEAD routes; path `p/{*filepath}` for `Static`/`StaticFS`, `p` for the file forms (PS-GR-2); handler null, `handler_expression` the call's method name | - |
| GR-P1 | a pattern or prefix that is a string literal, a constant (local `const`, a package-level `const` of the module's packages, imported), or a `+` of those (Go constant evaluation) | the pattern text; path `resolution`: `literal` for literals, `constant` otherwise | any other expression (`dynamic_pattern`: warning `route_dynamic_pattern`, no route; for a prefix `route_prefix_unresolved`, no path) |
| GR-P2 | template normalization of a composed path (shared slash rules of routes.md) | gin `:x` -> `{x}`, `*x` -> `{*x}`; chi `{x}` kept, `{x:re}` -> `{x}` (as FastAPI's convertors), `*` -> `{*}`; net/http `{x}` kept, `{x...}` -> `{*x}`, a trailing `{$}` -> exact path with `trailing_slash: true`, a subtree pattern `/a/` -> `/a/{*}`; a trailing `/` elsewhere -> `trailing_slash: true` (ADR 0019's flag, now for all three frameworks) | a chi regexp that can match `/` (`regexp_param`: the route keeps its node, no path) |
| GR-C1 | chi `r.Mount(p, x)` with `x` a proven chi router object (GR-K2, GR-O5); `Route` (GR-O1) composes the same way | the child's paths are `TrimSuffix(prefix, "/") + child path` (`Route("/nested/")` + `/` = `/api/nested/`, oracle); a child's `/` route gets the prefix's template with `trailing_slash` (it answers `prefix/`; `Mount("/api", x)` also answers the bare `/api`, so the route also gets the bare prefix's entry without `trailing_slash` (Mount's stub registration, TOP-1 of 2026-11-18; checked against the parent tree as any composed path), while a prefix written with a trailing slash, `Mount("/api/", x)` or `Route("/c/", ...)`, does not: chi v5 `Mount`) | `x` not a proven chi router, a router of another framework included (`mount_unproven_handler`, `route_mount_unresolved`); a prefix chi rejects (not starting with `/`: `invalid_pattern`) or with a regexp that can match `/` (`regexp_param`): no path through it; a registration of the same method (or ANY) in a parent tree (the router mounted on, its Group and With children and bound parameters) that matches every request of the composed path relative to that tree, segment by segment (a static segment equal, a parameter, a regexp parameter matching a static segment; step GR8): no path through the mount for that route (chi tries static and parameter nodes before the mount's catch-all) |
| GR-C2 | gin group prefixes | `joinPaths` (gin `utils.go`): `path.Join(base, p)`, keeping a trailing slash of `p`; the engine's base is `/` (PS-GR-1); an empty pattern is the group's path | - |
| GR-C3 | net/http `m.Handle(outer, http.StripPrefix(prefix, x))` and `m.Handle(outer, x)` / `m.HandleFunc(outer, x.ServeHTTP)` with `x` a proven ServeMux (a chi router at `/`: GR-C7) | the child's paths are `prefix + child path` (`""` without `StripPrefix`), claimed through this chain only when `outer` (method, host and path) matches every request of the child route (PS-GR-3) | a prefix or outer pattern that is not constant, or a prefix holding `{` or `}` (`route_prefix_unresolved`); an outer pattern that conflicts with an earlier pattern on its mux (`invalid_pattern`); a handler that may also hold an unproven value (`route_mount_unresolved`; a binding of `http.HandlerFunc(func(...){...})` whose literal uses no router object is no unproven value: the chain holds through the router's bindings, conditional per GR-C5): no path through it; a sibling pattern on the hop's parent mux more specific than `outer` that matches every request of the composed pattern (the path is never dispatched to the route; a partial overlap stays the matcher's precedence), the bare path of a sibling subtree pattern that does when `outer` ends in a wildcard (a redirect), or a parent mux with a Handle call of unknown pattern or that escapes, a sibling in the other branch of an `if` than the composition excepted: no path through the hop for that route |
| GR-C4 | roots | every proven constructor object no resolved composition consumes, and every use of which a row reads (step GR8: a registration's or child's receiver, a router's own methods, a registration's or composition's handler, a serving position, a local binding, a GR-O4 argument, a GR-O7 wrapper's handler, net/http's `StripPrefix`, `TimeoutHandler`, `MaxBytesHandler`, `Handler` and `HandlerFunc` around such a use, the `http.Handler` argument of an `http.Handler`-returning call bound locally or served, a router passed to an `http.Handler` parameter its module callee only serves at its own paths (an `http.Server`'s `Handler`, a serving call, `srv.Handler = h`), the handler of `httptest.NewServer`, `NewTLSServer` and `NewUnstartedServer`, a return GR-O5 or GR-O9 reads, an `http.Server` literal's `Handler`, a GR-G1 entry call), is a root (ADR 0016's rule), including dead code and test files; paths are composed from every root through resolved compositions, per-chain cycle guards, all distinct reachable paths kept | an object some use of which no row reads (passed to a parameter GR-O4 does not bind, an `http.Handler`, interface, `any` or reassigned parameter included, or to a call no rule resolves whose value is not bound locally; stored in a field, element, dereference, package-level or enclosing variable; sent on a channel; returned where GR-O5 and GR-O9 do not read the return, a multi-result constructor included; used through a method value or a method other than the router's own, gin's `Handler()` included; a handler-shaped closure using it in such a position): `escaped_router`, `route_mount_unresolved` naming the use, no root, and a parameter's escape is its bound objects' (the payload's escape records, row RU-escape, cover the syntax it does not keep); an object that reaches a composition position (the handler argument of `Handle`, `HandleFunc`, `Mount`, `StripPrefix`, directly or through bindings and returns) only inside another call or a closure other than a GR-O7 wrapper or GR-O8 forwarding closure (`wrapped_router`, `route_mount_unresolved` naming the call; open point O1); an object stored into a composite literal (a struct, slice or map element; the `Handler` field of an `http.Server` literal excepted, which serves it as a root) (`escaped_router`, `route_mount_unresolved` naming the element); a router served by an `http.StripPrefix` no GR-C3 composition or GR-C6 serving position reads (`wrapped_router`); a router returned by a method called on a call result whose receiver type the call's declared result states, where GR-O9 does not prove the value (step GR6: the composition stays refused, the router is `wrapped_router` and no root); a router used directly as a registration's handler is no root either (its routes: `route_unmounted`) |
| GR-C5 | conditional and scope | `conditional: true` for registrations, compositions or bindings inside `if`/`else`, `switch`/`select` cases, `for`/`range` bodies, closures other than GR-O1 callbacks, or helper bodies whose call is conditional (ADR 0019 OR rule), and for a composition whose handler may instead hold such a plain `http.HandlerFunc` literal; path-entry `scope: test` only when every reaching chain is test-scope (routes.md) | - |
| GR-C6 | serving positions: the handler argument of `net/http`'s `ListenAndServe`, `ListenAndServeTLS`, `Serve` and `ServeTLS`, the `Handler` field of an `http.Server` literal and the value of a `srv.Handler = h` assignment with `srv` stated `http.Server` or `*http.Server` as GR-H2 types a receiver, followed through bindings, `&`, `x.ServeHTTP`, the `http.Handler` and `http.HandlerFunc` conversions, `http.TimeoutHandler` and `http.MaxBytesHandler` (path-preserving in Go 1.27.1's source), the returns of a module function, `http.StripPrefix(p, x)`, path-preserving wrappers (GR-O7) and any other wrapper call `w(..., x, ...)` | a root served through `http.StripPrefix(p, x)` has the paths `p` + its path (Go 1.27.1 `StripPrefix` removes `p` before the inner router sees the path; nested prefixes in order); a root served through a wrapper call keeps its paths with assumption GR-V5 on every path entry (`assumptions: ["GR-V5"]`, `served_through` naming the wrappers), dropped when another chain reaches the same path without a wrapper; a root with serving positions is served only through them (a serving position in a test-scope file, or reached through a binding in one, does not serve a production root) | a prefix that is not constant, does not start with `/`, ends with `/` or holds `{`, `}`, `*` or `:` (`route_prefix_unresolved`: no path through it, and a root served only that way is no root); a router used inside a function literal at a serving position (`wrapped_router`, `route_mount_unresolved`) |
| GR-C7 | a ServeMux -> chi hop: net/http `m.Handle("/", x)` or `m.HandleFunc("/", x.ServeHTTP)` with `x` a proven chi router, or `m.HandleFunc("/", f)` with `f` a forwarding closure of one (GR-O8); the outer pattern exactly `/` (no method, no host), no `StripPrefix`, `m` a root ServeMux (no net/http hop before it) | the chi router's paths are unchanged (Go 1.27.1 `ServeMux.ServeHTTP` passes the request on, setting only `r.Pattern` and the wildcard matches; chi routes on `r.URL.RawPath`/`r.URL.Path`), claimed only when no other pattern registered on `m` (or on a parameter bound to it), no `/tree` -> `/tree/` redirect of a sibling subtree pattern and no shadow of the closure may match any request of the path (the chi path read as a ServeMux pattern: parameters as wildcards, a final `*` as `{x...}`, a final `/` as `{$}`) and the path is clean; the route states assumption GR-V2 (the mux's Go 1.22 patterns) | an outer pattern other than `/`, a router of another framework, a `StripPrefix` (not composed: the router stays a handler, `route_unmounted`); a mux with a Handle call of unknown pattern, a path a sibling, redirect or shadow may take, a parameter inside a segment, a path ServeMux cleans: no path through the hop (`route_mount_unresolved` naming the route); a mux that escapes (GR-C4: a sibling may be registered where no rule sees it) or the DefaultServeMux (packages such as `net/http/pprof` and `expvar` register on it in `init`; step GR8): the gate is opaque, no path through the hop |
| GR-H1 | the handler argument (net/http and chi: the handler position; gin: the last argument) is a module function: identifier or `pkg.F` | `handler` = the function's identity; `exposes_route` edge handler -> route (fact, confidence 1, registration evidence) | - |
| GR-H2 | a method value `x.M` whose receiver type is stated by the syntax: a receiver or parameter declared `T`/`*T`, a `var` with a type, a composite literal `T{...}`/`&T{...}`, or a call of a module function with one declared result type, `T` a module named type with method `M` (GS-D4) | as GR-H1, to the method `T.M` | - |
| GR-H3 | `http.HandlerFunc(f)` with `f` a GR-H1 or GR-H2 form | as `f` | - |
| GR-H4 | a function literal | `handler_inline: true`, no edge | - |
| GR-H5 | any other handler expression (a call result such as `mw.RequireAdminAuth(h.Index)`, an interface method value, a local value, a field) | `handler: null`, `handler_expression` (whitespace-free), no edge | - |
| GR-H6 | registration attributes | `framework` (`net/http`, `chi`, `gin`); `method`; `pattern` (literal as written, method and host included for net/http); qualified name `METHOD <pattern without its method prefix>`, `@2`, `@3` for repeats in a file; `middleware`: gin arguments before the handler, chi `With(...)` arguments of the receiver chain (registration-site middleware only: `Use` and group middleware are not listed); `host` (net/http); `assumptions` (GR-V1, GR-V2 when they apply) | - |
| GR-G1 | oapi-codegen generated servers (developer decision GO2): a file whose payload carries the generated-server record (`docs/lang/go-routes.md`, row RO-server: the header `// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version vX.Y.Z DO NOT EDIT.` of v2.8.0, v2.8.1 or v2.8.2 and the server declarations of the chi server, gin server or net/http server output equal to those versions' templates token for token, every registered `wrapper.N` calling `siw.Handler.N` once); its instances are the resolved calls of its entry functions outside the template's own functions: `Handler(si)`, `HandlerFromMux(si, r)` (and `HandlerFromMuxWithBaseURL(si, r, u)`), `HandlerWithOptions(si, o)`, gin `RegisterHandlers(router, si)` and `RegisterHandlersWithOptions(router, si, o)` | the router local of `HandlerWithOptions` holds every instance's router: a new router (the template's constructor) for `Handler` and for an options literal without `BaseRouter`, else the proven routers of the `BaseRouter` name; the entry call's value is its instance's router; gin's `router` parameter binds as GR-O4; each registration's pattern is the server's BaseURL (`""` for `Handler`, `HandlerFromMux` and `RegisterHandlers`, else the constant `u` or the options literal's `BaseURL` element, GR-P1, `""` when absent) followed by the template's path (net/http: after the method), when every instance gives the same BaseURL; the handler `wrapper.N` is method `N` of the module struct type every instance states for `si` (typed as GR-H2 types a receiver, or a composite literal or call result; implementing method), with the `exposes_route` edge | an instance whose BaseURL is not constant or differs from another instance's (`route_prefix_unresolved` at the instance: every route of the server keeps its node, no path); a `BaseRouter` that is not a name holding proven routers of the server's framework, or options that are no composite literal (`receiver_unproven`); no visible instance: no path (`route_unmounted`); a `si` type that is not one module struct type: `handler: null`, `handler_expression` `ServerInterface.N`; a file failing the header or shape check is read by the other rows (its registrations are `receiver_unproven`) |
| GR-V | assumptions GR-V1-GR-V5 (Environment) | stated on the routes | - |
| D | anything else | nothing; counted per file in one info diagnostic `go_routes_unread` with `(form=n, ...)`, plus the shared route diagnostic where one applies | - |

D forms (counted in one info diagnostic `go_routes_unread` per file,
`(form=n, ...)`): `dot_import`, `unsupported_module`,
`legacy_mux_patterns`, `receiver_unproven`, `callback_unread`,
`param_rebound`, `factory_method_chain`, `invalid_pattern`,
`dynamic_method`, `dynamic_pattern`, `regexp_param`,
`mount_unproven_handler`, `wrapped_router`, `escaped_router`. A refused
receiver counts only with evidence that it is a router (a router-typed
declaration or field, a reaching proven object, or for
`factory_method_chain` a module method of that name declaring a router
result); other router-named calls on unproven receivers (HTTP clients,
`Header().Get`) are not counted. `regexp_param` keeps the route node
(without a path). Diagnostics: `route_dynamic_pattern`,
`route_dynamic_methods` and `route_invalid_pattern` (warnings),
`route_mount_unresolved` (info) naming a wrapped or escaped router.
The same counts are stored on the file's `file` node as the attribute
`go_route_forms_outside_contract` (`{form: n}`, only on files with the
diagnostic), from the linker's result, so a consumer such as the
couldn't-read page (row CR-38) never parses the message.

Known gaps: assignments in function literals to an outer router
variable are not read; a router stored into a struct field, map element
or channel by an assignment or send statement is not visible in the
payload (only composite literals are: `escaped_router`); chi `QUERY`
(v5.5.0 only) is refused as version-dependent; promoted methods of
embedded types do not resolve as handlers. Composition (GR3-GR5): a
wrapper GR-O7 does not prove (a request passed to a method of another
type, a standard-library function or a closure; a wrapper factory
`func(...) func(http.Handler) http.Handler`) is assumed path-preserving
at a serving position (GR-V5, shown) and blocks the path at a
composition position; a serving call on another library's server, or an
`http.Server` handler assigned through a field or element (`s.srv.Handler
= h`), is not a serving position, so such a root is served at its own
paths; a router
served from a module function's return where the function is reached
from no serving position is served at its own paths; a factory's
conditional returns are not marked conditional; a router of another
framework under a net/http registration or a chi `Handle` gets no path
(no row composes it).

Generated servers (row GR-G1): an oapi-codegen v2.8.0-v2.8.2
chi, gin or net/http server whose file passes the capture's token-for-token
template check (`docs/lang/go-routes.md`, "Generated servers") registers
its routes on the routers of its instances, the resolved calls of its
entry functions: a new router for `Handler(si)` and an options literal
without `BaseRouter` (the entry call's value; a root when nothing
consumes it), else the router named by `BaseRouter`; gin's `router`
parameter binds as GR-O4. The `if r == nil` of the template is decided
per instance: a path entry is conditional only when every instance
carrying its router is (GR-C5). Every instance must give the same
constant BaseURL; otherwise every route of the server keeps its node
without a path and each instance is named in `route_prefix_unresolved`.
The handler is the ServerInterface method `N` of the module struct type
every instance states for `si`; a server that is instantiated with an
interface value or with several types keeps `handler: null` and
`handler_expression: ServerInterface.N`. Not read: options passed as a
variable, a `BaseRouter` that is not a name (both `receiver_unproven`),
the strict-server wrappers (`NewStrictHandler`: `si` is then an
interface value), echo, fiber, gorilla and iris servers, and generator
versions whose templates differ.
## Next.js App Router and Pages Router API (ADR 0077)

Task T024a (`docs/contract/claims/nextjs-supabase.md`), extractors-49, contract v1
unchanged (new attribute values only). `routes.LinkNext` runs at link time
over cached TypeScript facts: the shared export facts of each route file,
plus a small `Next` payload the TypeScript extractor keeps (destructured
export names, the default-export statement, and the literal reading of
`next.config.*`). No file is parsed again at link time and nothing is
executed.

### Claim contract (Next.js)

Assumptions:

- **NX-V1** Routing is the file-system routing of the app and pages
  directories plus the literal `rewrites` read by NX-P3: `middleware.ts` /
  `proxy.ts`, non-literal `rewrites`, `redirects` and `headers` are not
  applied. Shown on every link to a Next.js route (`assumes: ["NX-V1"]` on
  the linked `client_call`, since edges carry no attributes).
- **NX-V2** A `next.config` wrapped in plugin calls (`withNextIntl(config)`,
  `withA(withB(config))`) keeps the `basePath`, `trailingSlash` and `pageExtensions` of the object
  it wraps.
- **NX-V3** (T024a2) An afterFiles or fallback rewrite applies only when
  no page and no `public/` file serves the call's original path: Graphvein
  checks the project's route handlers and Pages Router API files, not its
  pages or public files. Shown on calls such a rewrite re-routes
  (`assumes: ["NX-V1", "NX-V3"]`).

| id | form | claim | refusal / unknown | assumptions |
|---|---|---|---|---|
| NX-P1 | a scanned `package.json` declaring `next` in `dependencies` or `devDependencies`, whose directory holds `app/` (else `src/app/`) and/or `pages/` (else `src/pages/`) | a Next.js project rooted there; its app directory and pages directory (Next.js `findPagesDir`) | `app` and `pages` under different parents (Next.js refuses): project unread, diagnostic `nextjs_project_unread` | - |
| NX-P2 | `next.config.{js,mjs,cjs,ts,mts}` in the project directory whose default export / `module.exports` is an object literal, a const bound to one, or plugin calls (nested or not) each wrapping that one argument; a spread of object literals (or of a conditional between them) makes only the keys those literals name unread, any other spread every key the literal does not name | `basePath` (string literal), `trailingSlash` (boolean literal), `pageExtensions` (array of string literals); absent keys take Next.js defaults (`""`, `false`, `tsx ts jsx js`) | any other value of the three keys, a function config, an unreadable file: `basePath` unresolved (`base: unresolved` on every path entry), `trailingSlash` unknown (flag omitted), `pageExtensions` non-literal: the project's routes go to D (`nextjs_config_unread`) | NX-V2 |
| NX-P3 | the `rewrites` key of an NX-P2 config object: a function (method, arrow or `async`) whose body returns, or an arrow whose body is, an array literal, or an object literal with `beforeFiles` / `afterFiles` / `fallback` array literals; every element an object literal with only `source` and `destination` string literals, `source` a path of literal segments and `:name`, `:name*`, `:name+`, `:name?` parameters | the project's literal rewrite rules (`phase`, `source`, `destination`), recorded on the project | anything else (a variable, a call, `push`, `has` / `missing` / `basePath` / `locale` keys, regex groups): rewrites `unread` (diagnostic `nextjs_rewrites_unread`, info); linking continues under NX-V1, which says so | NX-V2 |
| NX-R1 | a file `route.<ext>` (ext from `pageExtensions`) under the app directory, no directory part starting with `_` | the route template: each directory segment in order; `(name)` group and `@slot` dropped; `[n]` becomes `{n}`; `[...n]` becomes `{*n}`; `[[...n]]` becomes `{*n}` with `optional: true`; other segments literal; `/` when nothing remains | a part starting with `_` (private folder): not a route, no record; an intercepting segment `(.)x`, `(..)x`, `(...)x`, `(..)(..)x`; a `%`-escaped segment; a segment with brackets of another shape; a `page.<ext>` in the same directory (Next.js build error): D, `nextjs_route_unread` info | - |
| NX-R2 | in an NX-R1 file, an export named `GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD` or `OPTIONS`: `export [async] function`, `export const X = <expr>`, `export { a as X }`, `export { X } from "m"`, destructuring `export const { X } = ...` | one level-4 route node per verb (`framework: nextjs`, `method`, `pattern` = template, `paths` per NX-R4); `handler` the function the binding resolves to through the shared TypeScript export resolver, else null with `handler_expression`; resolved handlers get `exposes_route` (fact) | `export *` that the shared resolver cannot expand: D (`nextjs_route_exports_unknown`); lowercase `get` etc. and other names (`runtime`, `dynamic`, helpers): not routes | - |
| NX-R3 | (O2 (a)) a file `<pagesDir>/api/**.<ext>` (ext from `pageExtensions`) with a default export | one route node `ANY` (`framework: nextjs_pages`), template from the path with `index` stripped and NX-R1's bracket rules | no default export, a `.d.ts` file: D; a template NX-R1 refuses: D (`nextjs_route_unread`). An `_`-prefixed file or folder under `api/` is a route (T024 review: `next` 14.1.0 to 16.4.2 collect pages without the App Router's private-folder filter; only `_app`, `_document` and `_error` at the pages root are reserved) | - |
| NX-R4 | path composition for NX-R2/NX-R3 routes | one path entry: `basePath` + template, slash-normalized as ADR 0013; `resolution: literal` (no or literal `basePath`) or base `unresolved` (NX-P2 refusal); `trailing_slash: true` when `trailingSlash` is literally true; `optional` from NX-R1 | - | - |
| NX-L1 | an existing `client_call` alternative (fetch, axios, hey-api, Orval) | Next.js routes join the matcher's candidates under the existing rules (ADR 0018, 0020); client `HEAD` matches a route `GET` (Next.js auto-implements HEAD); an auto `OPTIONS` is not a route; a call linked to a Next.js route carries `assumes: ["NX-V1"]` | same-origin rule: an alternative with a literal origin never matches a Next.js route (it stays as today); (T024 review) an alternative in a file of a Next.js application (the deepest NX-P1 directory holding it) has only that application's routes as Next.js candidates, and one in a file outside every application, in a repository with several, never links to a Next.js route (`ambiguous`, `nextjs_origin: unknown`, the matching routes as candidates: the file may run in any of them); a route path with base `unresolved` matches only an alternative whose base is unresolved too, and such a link is aligned with the unknown-prefix tier (decision U6 of T033, row UP-19 of the unknown-prefix claim contract): `match: suffix`, `route_prefix: unresolved`, confidence 0.6, `assumes` NX-V1, UP-V1, UP-V2; an alternative matching both a Next.js and another framework's route: ambiguous as today; a call from another repository of a project is matched by the project link pass under its own rules (`docs/extract/project-links.md`, "Next.js route targets"; TOP-1 of 2026-11-18) | NX-V1 |
| NX-L2 | `useSWR(k, f)` / `useSWRImmutable(k, f)` from `swr` (shared resolver); `k` a literal or bounded const string, or (T024a2) a key the bounded URL evaluator (ADR 0020) reads as strings only, where a `null` alternative (SWR's "do not fetch" key) drops out; `f` a resolved function forwarding its first parameter unchanged as the URL of a recognised fetch (existing wrapper rules), or (T024a2) an inline arrow function with one plain parameter whose expression body starts with a fetch or axios GET of that parameter without options, the only request inside the arrow | a `client_call` node `library: swr`, method `GET`, paths from `k` (an environment base unresolved as for other clients), linked as NX-L1; an inline fetcher's own request node gets `status: wrapper` | an array, function or null-only key, a key with any other value (a parameter, an object, a call), an unresolved or global fetcher (`SWRConfig`), an inline fetcher with a block body, options or another request: D (counted) | NX-V1 |
| NX-L3 | an alternative whose path may match the `source` of an NX-P3 rule of the project whose routes are candidates (NX-L1) | (T024a2) Next.js order (`next` 14.1.0 and 16.2.10 `resolve-routes.js`: beforeFiles, exact static check, afterFiles, route check, fallback); the first rule that may match decides. (1) Served first (T024 review), with a literal call base and project `basePath`: for an afterFiles rule, a static route of the project that surely serves the call path (equal literal segments), for a fallback rule any route that surely serves it, is matched as without the rule, whatever the rule's destination. (2) Re-routed: with a literal call base and project `basePath`, a source that surely matches (equal literal segments, same case, no trailing slash), a same-origin destination of literal segments and source parameters, and no later rule that may match the rewritten path before a route surely serves it: the project's routes are matched at the rewritten path; the path entry records `nextjs_rewrite` (source), `nextjs_rewrite_destination`, `nextjs_rewrite_phase`; an afterFiles or fallback link also assumes NX-V3 | when (1) does not apply: a route that only may serve the path (a call parameter against a literal, a case difference), or any other may-match (an external, protocol-relative or unmapped destination, an unresolved base, a call parameter or a case difference against a literal source segment, a trailing slash, a chain): no Next.js route of that project is a candidate; the alternative records `nextjs_rewrite: <source>` | NX-V1, NX-V3 |
| D | everything else: middleware / proxy, non-literal rewrites, redirects, headers, `generateStaticParams`, Pages Router pages, i18n locale prefixes, `next/navigation` and `<Link>` (navigation, not requests), Playwright `request` calls, app wrappers returned from hooks | nothing; NX-R1 and NX-R2 refusals are counted (`nextjs_route_unread`) | always | - |

The code table is `routes.NextContract` (`internal/extract/routes/nextjs_contract.go`);
every branch reports its row, and `TestNextRoutesContract` /
`TestNextRoutesFixture` (`internal/index/nextjs_routes_test.go`) keep the
code, this table and the task's table 1:1 and check that the fixture
`testdata/fixtures/nextjs-routes` reaches every row and D.

### Route nodes

- Identity `route:<file>#<METHOD> <template>`; `framework` `nextjs` (App
  Router) or `nextjs_pages` (Pages Router API, method `ANY`); `pattern` the
  template without `basePath`; `project` the project directory; `handler`
  the resolved function (with an `exposes_route` fact edge) or null with
  `handler_expression` (a destructured export, a wrapper call result, an
  export that resolves to no function); `middleware` is always empty.
- `paths` holds one entry: `basePath` + template (`resolution: literal`),
  or the template alone with `resolution: unresolved` when `basePath` is
  not a literal; `optional: true` for an optional catch-all;
  `trailing_slash: true` when `trailingSlash` is literally `true`.
- `base_path`: the literal value (`""` when absent) or null when unread.
- `rewrites`: `{status: "literal", rules: [{phase, source, destination}]}`
  or `{status: "unread"}`; omitted when the config has no `rewrites` key.
- Content hash: the method, template and the text of the export statement.

### Matching (rows NX-L1..NX-L3)

- Same-origin rule: an alternative with a literal origin never matches a
  Next.js route.
- Own origin (T024 review): a relative URL resolves against the origin
  of the application that served the page. An alternative in a file of a
  Next.js application (the deepest directory `routes.NextProjectDirs`
  lists: a `package.json` declaring `next` with an app or pages
  directory) has only that application's Next.js routes as candidates.
  An alternative in a file outside every application (a shared package)
  in a repository with several may run in any of them: when its best
  candidates include a Next.js route it is `ambiguous` with
  `nextjs_origin: "unknown"` (the candidates listed), never linked.
- A client `HEAD` matches a route `GET` (Next.js implements HEAD from GET);
  a Pages Router route (`ANY`) accepts every method.
- A path entry with `resolution: unresolved` is a candidate only for an
  alternative whose own base is unresolved: a call without a base reaches
  that route only when the deployment's `basePath` is empty, which nothing
  proves.
- An alternative whose path may match a literal rewrite `source` of the
  route's project (a call parameter matches any segment, literals compare
  without case as Next.js does; with an unknown base, any suffix of the
  call is tried) gets no candidate from that project and records
  `nextjs_rewrite: <source>` on its path entry, unless a route of the
  project surely serves the path before the rule applies (T024 review: a static route before an afterFiles rule, any route before a
  fallback rule, whatever the destination; the call is then matched as
  without the rule), or row NX-L3 proves the rewrite (T024a2): then the project's routes are matched at the
  destination and the path entry also records `nextjs_rewrite_destination`
  and `nextjs_rewrite_phase`. Sources and same-origin destinations take
  the literal `basePath` as Next.js's `processRoutes` adds it; a
  destination's `:name`, `:name?`, `:name*`, `:name+` take the segments
  the source's parameter of that name matched (`routes.NextRewrite`,
  checked against `next`'s own `getPathMatch` and `prepareDestination` by
  `TestNextRewritesOracle`). The checks against the project's routes
  ignore the method: Next.js resolves the file by path.
- A linked alternative to a Next.js route carries `assumes: ["NX-V1"]`
  (also on the client call node).
- `useSWR(k, f)` / `useSWRImmutable(k, f)` (row NX-L2) become `client_call`
  nodes with `library: swr`, method `GET`, `wrapper` the fetcher (null for
  an inline fetcher, whose own request node becomes `status: wrapper`);
  unread forms give the info diagnostic `swr_call_unread`. A key is
  evaluated like any URL (template literals, environment bases, consts,
  conditionals); the URL and base rules are the fetcher's transport's
  (fetch, or axios with its instance's `baseURL`).

### Diagnostics (info)

`nextjs_project_unread` (app and pages directories under different
folders), `nextjs_config_unread` (keys that are not literals),
`nextjs_rewrites_unread`, `nextjs_route_unread` (message: the refused form:
`intercepting_segment`, `percent_segment`, `bracket_unrecognised`,
`catch_all_not_last`, `page_and_route`), `nextjs_route_exports_unknown`
(an `export *` of a module Graphvein cannot list), `swr_call_unread`.

### Decisions

- Project directory: the directory of a scanned `package.json` declaring
  `next`; a file belongs to the deepest such project. An `app` / `pages`
  directory exists when a scanned file lies under it.
- Config file: the first of `next.config.js`, `.mjs`, `.cjs`, `.ts`, `.mts`
  present in the project directory. A spread in the config object makes
  every key it does not name unread; an assignment to or
  `Object.assign` of the config const makes the whole config unread.
- `pageExtensions` unread (a function config included): no route of the
  project is read (D), since which files are routes is then unknown.
- Pages Router: files under `<pagesDir>/api/` named `*.d.<ext>` and
  files without a default export are not routes. A name or folder
  starting with `_` is a route (T024 review): `next` 14.1.0, 15.1.4,
  15.6.0-canary.59, 16.2.10 and 16.4.2 collect pages with
  `recursiveReadDir(pagesDir, { pathnameFilter: isPageFile })` and no
  `ignorePartFilter` (the App Router's private-folder rule); only
  `/_app`, `/_document` and `/_error` at the pages root are reserved.
- Oracle scope (T024 review): `TestNextRoutesOracle` compares the App
  Router template strings only (`NextAppTemplate` against `next`'s
  `normalizeAppPath` / `getRouteRegex` on route file paths, the private
  folder flag included). It does not compare Pages Router templates,
  custom `pageExtensions`, the verb export forms, the page-and-route
  refusal or the client links; those are `mapped`. `TestNextRewritesOracle`
  compares `routes.NextRewrite` with `getPathMatch` / `prepareDestination`
  on literal cases; the served-first order of row NX-L3 is read from
  `resolve-routes.js` and not compared.

## Unresolved mounts (T029 FU-2)

A route that gets no path because composition stopped at a hop the rows
cannot read stores the hop instead of a guess, in the route attribute
`unresolved_mounts` (rows OG-94..OG-96 of
[OpenAPI generation](../contract/claims/openapi-generation.md)). Each
entry has:

- `key`: `mount:<file>#<function> <hop>`, where `<hop>` is the hop call
  with its first argument (`r.Route(base)`, `app.use(base)`,
  `app.include_router(prefix=PREFIX)`, `APIRouter(prefix=os.environ["OWN"])`,
  `app.nest(base)` or `.nest(base)` on an Axum call chain)
  or, for a wrapped or escaped Go router, the expression named in the
  diagnostic; `@N` marks the Nth hop of the same text in one function. The
  key never holds a line number, so it survives edits elsewhere.
- `registration`: a content hash of the file, the function and the hop's
  whole text with function bodies elided and, when the routers above the
  hop are proven, the composition above it (the known prefixes and hops
  from the root). It changes when the hop or anything composed above it
  changes, not when routes below it are added or removed.
- `reason` (`route_prefix_unresolved` or `route_mount_unresolved`), `hop`
  (the whole text), `file` and `line`.
- `value_covers`: what a value for the hop states. `hop`: every router
  above the hop is proven; the value is only the part the hop adds, and
  each path carries the known prefix above the hop in `above` (absent at
  the root). `nothing`: the hop adds no part Graphvein could not read (a
  net/http `Handle` without `StripPrefix` whose outer pattern or router is
  not proven, a wrapped router passed to `Handle`): the paths are the
  served paths and the only value is `/`. `root`: a router above the hop
  is not proven either (an escaped router, a hop below another unknown
  hop): the value is the whole prefix from the root.
- `paths`: the route's paths below the hop, through the same rows and in
  the same entry form as `paths`: composed through the proven routers
  above the hop with the unread part left out (`hop`), as served
  (`nothing`), or from the hop's router as if it were served at `/`
  (`root`). The path `""` is the hop's prefix itself (gin `g.GET("", h)`
  below a group), distinct from `/`, the prefix with a trailing slash.
- `shadowing: not_checked` (Go and Axum): the Go checks that look at the
  routers above a hop (GR-C1's parent tree, GR-C3's sibling patterns,
  GR-C7's gate) cannot run without the hop's prefix; for Axum the routes
  and nests beside the hop on the parent router are not compared with the
  placed paths.

The hop is the first one composition stops at walking up from the
registration; a route below several hops has one entry per hop. The index
never takes a value for a hop: `explorer.path_values` in `graphvein.yaml`
supplies one ([configuration](../config.md#explorer)) and only the
generated OpenAPI document, the explorer and try-it use it.

Axum (T029 FU-3): a `nest` whose prefix is not a constant and whose
router is resolved is a hop. Its `value_covers` is `hop` when the router
the nest is called on has a path, is below no unknown hop and no nest
prefix above holds a trailing or doubled slash: each path then carries
that router's paths in `above` and they are hashed into the
registration; otherwise it is `root`. An Axum nest always adds a part, so
it is never `nothing`. The paths below it are the nested router's
paths composed as from a root (nests and merges below it, the Axum
normalisation). The nested router's `/` path is stored as `""` (the hop's prefix itself,
without a trailing slash: axum 0.6.20, 0.7.9 and 0.8.8 answer `/api` and
404 on `/api/` for `nest("/api", Router::new().route("/", h))`); it, and
the trailing-slash paths below the hop (composed as above), are stored
only for a crate whose axum version is proven within 0.6.20 to 0.8.x (rows SH-X11, SH-X25; 0.6.20, 0.7.9 and 0.8.8 were checked), else left out. The hash elides closure bodies and inline router
chains among the nest's arguments. A `nest_service` hop is the same
(key text `.nest_service(<prefix>)`); its nested `/` is stored as `""`
and `/` (the value with and without a trailing slash). Not hops: a
`nest`, `nest_service` or `merge` whose router argument no rule resolves
(no route is linked below it). Recall gap: a router nested both under a
constant prefix and under a hop keeps only its constant path.

## Unknown-prefix paths (T033, ADR 0084)

A route whose prefix the code does not fix keeps `paths: []` and stores,
in `unresolved_prefix_paths`, the paths proven below the unknown part
(Draft claim contract
[unknown-prefix-paths](../contract/claims/unknown-prefix-paths.md)).
The entries are a projection of the route's `unresolved_mounts`: one per
path and hop, `template` and `above` as stored below the hop,
`prefix: "unresolved"`, `prefix_reason` the hop's reason and `hop` its
key, with the path entry's `trailing_slash`, `conditional`, `resolution`,
`optional`, `scope` and (Go) `assumptions` and `served_through`. Only the
client matcher's fallback tier (rows UP-13..UP-20) is meant to read them;
OpenAPI generation, try-it and every surface reading `paths` are
unchanged.

Go: a hop's row follows from where composition stopped. A
chi `Route` or `Mount` or a gin `Group` prefix that is not a constant is
UP-1; a net/http `Handle` composition whose `StripPrefix` prefix or outer
pattern is not a constant is UP-2; a wrapped or escaped router, or a
composition whose handler binding may hold a value no rule proves, is
UP-4 (`prefix_reason: route_mount_unresolved`; with `value_covers:
nothing` the template is the served path below the unproven hop, with
`root` the path from the hop's router). A generated server whose
instances give no one constant BaseURL (GR-G1) has no hop: each
registration gets one entry, the template's path with the BaseURL left
out, `hop: null` (UP-3). Entries from several hops are merged by
(`above`, `template`, `trailing_slash`); the first hop in key order is
kept (UP-8). An outer pattern `X + T` whose `StripPrefix` prefix is `X`
(the same identifier or selector) leaves the router below only the paths
starting with `T`: a path below the hop that does not start with `T` gets
no entry (GR-C3's tail check below the unread `X`; Go 1.27.1 answers 404
there). No entry: a route with a production path (UP-9; a
route whose paths are all test scope keeps its production entries), a
route with `route_unmounted` (UP-10), a `StripPrefix` constant holding
template syntax (the hop is stored for path values, UP-2's refusal) and
every pathless route with no hop (a prefix chi rejects, a refused check,
a `StripPrefix` at a serving position (GR-C6), which stores no hop: UP-2's
refusal, row D). The D forms tagged by the code are `template_syntax` and
`no_hop`. Go entries keep the path entry's `assumptions` and
`served_through` (GR-V5), so the caveat the path below the hop carries is
not lost.

Express, FastAPI and Axum: every hop of these frameworks is
a prefix that is not a constant (`prefix_reason:
route_prefix_unresolved`): an Express `use(p, router)` (UP-5), a FastAPI
`include_router(r, prefix=p)`, a `mount(p, app)` (or a Starlette
`Mount(p, ...)` in a `routes=` list) or an included router's own
`prefix=` (UP-6), an Axum `nest(p, r)` or `nest_service(p, s)` with the nested
router resolved (UP-7). The entries are the paths stored below the hop,
in the framework's path form: Express parameters as `{name}`; FastAPI
templates normalized as its `paths` are (a final `{name:path}` stored as
`{*name}`, a trailing slash as `trailing_slash`); Axum's nested `/` as
`""` (UP-10: stored, never matched) and its trailing-slash paths only on
a crate whose axum version is proven (SH-X11, SH-X25). Merged and sorted
as for Go (UP-8). A FastAPI `mount(p, app)` with `p` not a constant
consumes the mounted app (no root of its own), so its routes'
paths are stored below the hop and become entries; a route whose every
path an earlier mount with a read prefix takes (`route_shadowed`) stores
no hop and gets no entry (D `not_stored` when it is below a hop). A route
whose full paths are all test scope (a test harness mounts the router at
a known prefix) keeps the entries of its production hops (UP-9 per
scope). No entry (row D, forms tagged by the code):
an Express route whose own pattern, or a mount prefix on a chain to it,
no row reads (`unread_pattern`, UP-5's refusal: the route reports
`route_pattern_unmodelled` or another refusal of the Express path rows),
a route below a hop whose path below it is not stored (`not_stored`: a
FastAPI pattern `""` right at the hop; an Axum nested `/` or
trailing-slash path on a crate whose axum version is not proven), and an
Axum route whose every path was left out for an unproven trailing slash
(`slash_unproven`). A router nothing mounts, includes or nests reports
`route_unmounted` and gets no entry (UP-10). A failure while projecting
one route is the warning `route_unknown_prefix_failed`; the route keeps
no entries. Fixture: `testdata/fixtures/unknown-prefix-mounts`.

### Unknown-prefix paths claim contract (route side)

Copied from the public claim contract's route side and its default row;
`TestUnknownPrefixContract` keeps this table, the public one and the
code table (`unknown_prefix_contract.go`) 1:1.

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-1 | Go: a chi `Route(p, f)` or `Mount(p, x)`, or a gin `Group(p)`, whose prefix `p` is not a constant (GR-P1 refusal on a prefix, hop reason `route_prefix_unresolved`), with the routers below it proven | one entry per path stored below the hop (`unresolved_mounts[].paths`), `above` when `value_covers` is `hop` | a prefix chi rejects or a regexp prefix (`invalid_pattern`, `regexp_param`): no hop, no entry |
| UP-2 | Go: a net/http composition (GR-C3) whose `StripPrefix` prefix or outer pattern is not a constant | as UP-1; for an outer pattern `X + T` (`T` a constant other than `/`) whose `StripPrefix` prefix is `X` spelled the same (an identifier or a selector of identifiers), only the paths below the hop starting with `T` (GR-C3's tail check below the unread `X`) | a prefix holding `{`, `}`, `*` or `:`: no entry; a path below the hop that does not start with `T`: no entry; a `StripPrefix` at a serving position (GR-C6: no hop is stored there): no entry |
| UP-3 | Go: an oapi-codegen generated server (GR-G1) whose instances give no one constant BaseURL | one entry per registration: the template's path, `hop: null`, `prefix_reason: route_prefix_unresolved` | no visible instance (`route_unmounted`): no entry |
| UP-4 | Go: a router reached only through a hop no row proves (a router argument, a wrapped or escaped router; hop reason `route_mount_unresolved`, `value_covers` `nothing` or `root`) | as UP-1, `prefix_reason: route_mount_unresolved` (decision U2; precision-safe default: no entry) | - |
| UP-5 | Express: `use(p, router)` with `p` not a constant | as UP-1 | a prefix piece no row reads (`route_pattern_unmodelled`): no entry |
| UP-6 | FastAPI: `include_router(r, prefix=p)` or `mount(p, ...)` (the `mount` method, `x.router.mount` on a proven app, or a Starlette `Mount(p, ...)` in the `routes=` list literal of a proven `FastAPI(...)` constructor) with `p` not a constant, or an included router's own `prefix=` that is not a constant | as UP-1 | a router nothing includes, or reached only through a mount no row composes (an `APIRouter.mount` whose FastAPI version is not proven >= 0.137.0, a `Mount` in any other routes list; `route_unmounted`): no entry; an `APIRouter.mount` with `p` not a constant under a proven >= 0.137.0 (`route_prefix_unresolved`, no hop is stored): no entry; a pattern `""` right at the hop (not stored below it): no entry |
| UP-7 | Axum: `nest(p, r)` or `nest_service(p, s)` with `p` not a constant and the router resolved | as UP-1 (the nested `/` as `""` and the trailing-slash paths only for a crate whose axum version is proven, rows SH-X11, SH-X25) | a nest, nest_service or merge whose router no rule resolves: no entry; the nested `/` or a trailing-slash path on a crate whose axum version is not proven (not stored below the hop): no entry; a route whose every path is left out for an unproven trailing slash (`route_trailing_slash_unproven`): no entry |
| UP-8 | the entry list | entries deduplicated by (`above`, `template`, `trailing_slash`) across hops (at most 16 hops, as stored), merged as `paths` entries are (conditional, scope, resolution), sorted by `above` then `template` | - |
| UP-9 | a route with at least one usable `paths` entry, applied per scope | a route with a production-scope `paths` entry: no `unresolved_prefix_paths` (a known path is never weakened by an unknown one); a route whose `paths` are all test scope: its production entries only (all test scope: a test harness mount never removes a production entry) | - |
| UP-10 | refused route forms | none: a route below no hop (`route_unmounted`), a Next.js `basePath` read as not literal (row NX-R4 keeps its `resolution: unresolved` entries in `paths`; shown and linked as UP-29 and UP-19 state, decision U6), a dynamic registration pattern (no route node), the path `""` below a hop (stored, never matched: it has no literal segment) | - |
| D | anything else | nothing: no entry, no candidate, no call-side reading; the route keeps its existing state (empty `paths`, the existing diagnostics and `unresolved_mounts`), and a call of a helper of any other shape keeps the ordinary evaluation |

## Cached expression trees (T069)

TypeScript/JavaScript route, client, URL binding and Angular syntax stores
each call, binding and registration with its detached expression tree
(`facts.RouteExpr`). The extractor builds each node's expression once per
file and shares it: a call nested in another call's arguments or callee
chain is one value in the enclosing call's tree and in its own client
record, so the facts grow with the file, not with the file times its call
nesting. A file that constructs a URL (`new URL(...)`) is extracted without
sharing, because the URL object proofs rewrite its client and URL binding
syntax in place.

The fact cache stores a file's `RouteFacts` as one table of distinct
expressions, children before parents, with every expression of the facts a
reference into it; a Text that is the source at the expression's offset is
stored as its length, with the source text covering them stored once
(`internal/lang/facts/exprtable.go`). A warm run decodes each entry once
and shares it. The decoded facts equal those of the plain JSON layout
(extractors-63 and earlier), so no fact changes (extractors-64).

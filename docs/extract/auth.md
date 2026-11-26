# Auth extraction: FastAPI, hey-api, Express middleware, Axum layers and gin/chi router middleware

T026 (FastAPI, hey-api) and AG-B4b (Express, Axum), ADR 0079
option 3A; extractors-48 (one number per stack),
contract v1 (new attributes only). The claim contracts are rows DA-F1..DA-F3,
DA-H1 and DAD of `docs/contract/claims/architecture-graph.md` ("Claim contract:
auth extractors"); the code tables `routes.FastAPIAuthContract` and
`clients.HeyAPIAuthContract` list them in order, and
`TestAuthFastAPIContract` / `TestAuthHeyAPIContract` keep code and table 1:1
and check that the fixtures `auth-fastapi` and `auth-heyapi` reach every row
and the unknown branch.

The facts say what the code **declares**, never that a route is protected
(AG7): a FastAPI scheme with `auto_error=False` lets requests without
credentials through (every scheme langflow declares), and a hey-api client
adds credentials only when the application configures `auth`.

## FastAPI (rows DA-F1..DA-F3)

### Gate DG and cached syntax

A Python file that imports `fastapi` or a submodule of it (gate DG) keeps,
in its cached syntax:

- each function scope's parameters that have an annotation or a call
  default (`PythonScope.Dependencies`: name, reduced annotation, reduced
  default, parameter span);
- module-level assignments whose value is a subscript (candidate
  `X = Annotated[T, Depends(f)]` aliases) with their arguments.

Every Python file keeps the values of the keywords `dependencies`,
`auto_error` and `scheme_name` in constructor calls (`APIRouter(...)`,
`FastAPI(...)`, scheme instances), and route and `include_router` calls keep
their `dependencies=` lists with each call's span. Reduced expressions keep
names, attributes, calls, subscripts, lists, tuples, string and boolean
literals to depth 6; other forms keep their kind only. Nothing is evaluated.

### Rows

| row | form | fact |
|---|---|---|
| DA-F1 | a handler parameter `x = Depends(f)` / `Security(f)`, `x: Annotated[T, Depends(f)]`, a module-level alias of it (also nested aliases, flattened as Python flattens `Annotated`), and `dependencies=[Depends(f)]` on the route decorator or `add_api_route`, the `APIRouter`, `include_router` or `FastAPI` | a step to `f` resolved by the shared Python resolver |
| DA-F2 | a dependency function's own parameters, recursively | further steps; cycle-safe; depth above 8 is `truncated` |
| DA-F3 | a step to a stable module-level instance of `OAuth2`, `OAuth2PasswordBearer`, `OAuth2AuthorizationCodeBearer`, `HTTPBearer`, `HTTPBasic`, `HTTPDigest`, `APIKeyHeader`, `APIKeyQuery`, `APIKeyCookie`, `OpenIdConnect` (from `fastapi.security` or its defining submodule) or of a repository class whose resolved bases reach one | an entry of `security_dependencies` |
| DAD | everything else | an entry of `security_dependencies_unknown`, or nothing for a form that declares no dependency |

Order and choice follow FastAPI 0.161.1 (`fastapi/dependencies/utils.py`):
app, then each `include_router` and router of the mount chain from the
root, then the route's own list, then the handler's parameters in
declaration order; in `Annotated`, the last FastAPI-specific item (`Depends`,
`Security`, `Query`, `Path`, `Body`, `Header`, `Cookie`, `Form`, `File`,
`Param`) wins, then a `Depends` default. A Starlette `mount` starts a new
app: the parent's dependencies do not apply. `Depends()` without a callable
falls back to the annotation type.

Scheme settings (DA-F3): `scheme_name` is the literal as written, else the
instance class's name (FastAPI: `scheme_name or self.__class__.__name__`);
`auto_error` is the literal as written, else the default `true`. Either is
`"unknown"` when not a literal, given positionally where the class accepts
it (`OAuth2PasswordBearer` positions 1 and 4,
`OAuth2AuthorizationCodeBearer` 3 and 6), when the call has `**kwargs`, or
when a repository class on the way defines `__init__`.

Unknown entries (DAD and the refusals of DA-F1/DA-F2), each with a reason:
unresolved dependency (DA-F1/DA-F2 refusal), `truncated: dependency depth
above 8` (DA-F2 refusal), dependency cycle, class dependency (its `__init__`
is not read), external callable (for example `OAuth2PasswordRequestForm`),
inline value (`Depends(HTTPBearer())`: DA-F3 reads module-level instances),
lambda or other expression, instance of a class that reaches no scheme (its
`__call__` is not read), unstable instance binding, parameters of a function
in a file outside gate DG, a later or unresolved `Annotated` item,
`dependencies` that is not a literal list, keyword unpacking that may supply
`dependencies`, a list item that is not a `Depends`/`Security` call, and
dependencies declared on some mount chains of a route only (the ones every
chain declares are kept). Every form outside the table (decorators, custom
auth in handler bodies, `Request` inspection, Starlette middleware,
`app.dependency_overrides`, PEP 695 `type` aliases) is DAD with no entry.

Precision: an unread form can only hide a scheme (the route then shows
`auth ?`), never add one.

### Route attributes

```json
"security_dependencies": [
  {"class": "OAuth2PasswordBearer", "subclass": "CookieBearer",
   "scheme_name": "CookieBearer", "auto_error": false,
   "instance": "app/security.py#cookie_bearer",
   "chain": [
     {"via": "parameter user", "file": "app/routers/admin.py", "span": {...}, "target": "function:app/deps.py#cookie_user"},
     {"via": "parameter token", "file": "app/deps.py", "span": {...}, "target": "app/security.py#cookie_bearer"}]}],
"security_dependencies_unknown": [
  {"reason": "unresolved dependency", "expression": "helper.check", "chain": []}]
```

One entry per scheme instance (the first chain in FastAPI's order). `via`
is `parameter <name>`, `route dependencies`, `router dependencies`,
`include_router dependencies` or `app dependencies`; `span` is the
parameter or the `Depends` call. Both attributes are absent when empty.

## hey-api (row DA-H1)

A proven hey-api call (`library: hey-api`, docs/extract/clients.md) whose
options object literal has `security: [...]` gets the client-call attribute
`security` as written: a list of entries with the string keys `in`, `key`,
`name`, `scheme`, `type` (hey-api's `Auth`). An options object with spreads
(the generated `...options`) still counts; a non-literal array, an entry
with a spread, a computed key, another key or a non-literal value makes
`security: "unknown"`. No `security` property: no attribute (DAD). No cache
change: the request's options object was already cached.

## Server (rows AA3, AA4, CH11)

`GET /api/v0/architecture` fills the HTTP auth cell from these attributes
(state `as_written`, never `proven`): AA3 "declares <class> (<scheme
name>)", with ", optional (auto_error=False)" or ", auto_error ?", source
`declared`; AA4 "generated client documents <entry>" (entries joined by
"or", each as written: type, scheme, name, "in <in>"), source "documented by
the generated client, as written in <file>". The chain (CH11) names the
injection: an upstream function with no stored caller that a route's chain
names ends with "FastAPI dependency injection into n route(s) as declared in
security_dependencies (...)"; a route reached downstream gets "dependencies
declared, not followed as calls: <steps> (<class>)".

## Oracle

Opt-in, never in CI:

- `scripts/oracle/fastapi_security.sh --scratch DIR`: a scratch venv with
  FastAPI 0.161.1 (MIT) and its pinned dependencies (listed in the script
  with licences, all published at least 14 days before the unit) imports
  the fixture apps `app.main:app`, `app.partner:app`, `app.partner:sub` of a
  scratch copy (never corpus code; `app/unread.py` holds the forms FastAPI
  refuses at startup and is not imported) and writes `app.openapi()`
  security per operation; `TestFastAPISecurityOracle` compares. Precision:
  every stated scheme name is in the operation's security; recall: every
  operation scheme is stated unless the route has an unknown entry or an
  unknown scheme name. 2026-11-18: 24 operations agree, 0 disagree.
- `scripts/oracle/heyapi_security.sh --scratch DIR`: `@hey-api/openapi-ts`
  0.99.0 (MIT) with `typescript` 6.0.3 (Apache-2.0) regenerates the fixture
  client from `openapi.json`, checks that the committed generated files are
  byte-equal, reads the generated `security` arrays with the TypeScript
  compiler and `TestHeyAPISecurityOracle` compares them with the stored
  attributes. 2026-11-18: 5 calls agree, 0 disagree.

Corpora are checked against committed specs where they exist (langflow
`docs/openapi/openapi.json`), otherwise labelled mapped.

## Express (rows DA-E1..DA-E5)

Code tables `routes.ExpressAuthContract`, `TestAuthExpressContract`,
fixture `auth-express`. Composition runs at link time on the existing
Express mount graph; no cache change (the call syntax it reads for escapes
is cached for every TypeScript/JavaScript file already).

Model (Express 4.21.1 `lib/router/index.js` and `lib/application.js`;
Express 5.1.0 with router 2.2.0 agree): `use` pushes one layer per
middleware argument onto its router's stack in call order; the first
argument is the path unless it is a function (or array starting with
one); a request walks the stack in order, a `use` layer matching at a
segment boundary (`end: false`, case-insensitive by default, trailing
slash ignored). So a middleware runs before a route's handler when its
layer is pushed before the route's on the route's router, or before the
`use` that mounts the route's router (or app) on each router up the mount
chain, middleware arguments before the router in the same call included.

| row | form | fact |
|---|---|---|
| DA-E1 | a `use` call on a proven app or router, before the route on its router or mount chain, with a literal (or const) or absent path matching the route path | `mounted_middleware`: per middleware argument `name` (as written, whitespace removed), `prefix` (`/` when absent), `file`, `span`; root router first; only entries every chain from the route up to an app states (each app is a chain end: it can be served alone and mounted) |
| DA-E2 | a middleware that is, or names through at most 4 local or imported bindings and object-literal properties, a call to passport `authenticate`, express-jwt `expressjwt` (8.x) or its default export (6.x), express-oauth2-jwt-bearer `auth`, express-openid-connect `requiresAuth` | `declares: {library, detail}` on the entry (passport: the strategy literal, else `strategy ?`; others: the export name); `option` and `optional` (`true` for a literal `credentialsRequired: false` / `authRequired: false`, `"unknown"` when not literal); route arguments: `middleware_declares` entries with `index` and `name` |
| DA-E3 | AG-F: `use` calls in the body of a module function (a function declaration or const function, not async or a generator) whose parameter receives a proven app or router, at the function's only call with that object anywhere, which is a top-level call not under a branch, loop, try or short-circuit (positional argument to an identifier parameter, or object-literal property to an object-pattern parameter naming that key); the parameter is not reassigned, bound to another name, returned, passed to another call or used in a nested function, and every use of it is a plain `p.use(...)` mounting no router or app | DA-E1 entries placed at the call in body order, each with `via: {function, file, span}` (the call); `file` and `span` stay the `use` call's |
| DA-E4 | Review fix: `router.route(p).all(f...)` chained before a method of the same builder (`route(p).all(requireAuth).get(h)`; Express `Route` runs its stack in order, `all` entries for every method) | the `all` handlers lead the later method's `middleware` (AA1) in chain order; the `all` step is no route of its own (a trailing `all` stays an `ANY` route) |
| DA-E5 | Review fix: a DA-E1 middleware argument whose `Function.length` is 4 or more (parameters before the first default or rest parameter): an inline function, or a name bound to a function declaration or module-level const function, locally or through one import | `mounted_error_handlers` (`name`, `prefix`, `file`, `span`), never `mounted_middleware`: Express skips such a layer for a request without an error (`Layer.handle_request`: `fn.length > 3`) |
| DAD | everything else | an entry of `mounted_middleware_unknown`, or nothing |

**Earlier route layers (review fix.** A route registration is a
layer of its router's stack too: `app.all('/api/*', requireAuth)` (the
example of Express's `app.all` documentation), `router.all('*', mw)`, a
same-method route (`get('/:id')` before `get('/feed')`), GET for a HEAD
route, any method for an `all` route, or a `route(p)` chain holding one.
When such a layer on the route's router, or on a router of its mount
chain, is pushed before the anchor (or in an order not proven) and its
path is not proven disjoint from the route's path at that level, its
handlers may run first (they act as middleware when they call `next()`):
`mounted_middleware_unknown` "earlier route handler <expr> (<METHOD>
<path>) may run first". Disjoint is proven by two literal segments at the
same position that differ case-insensitively, before the first segment of
either path with syntax other than literals and `:name` parameters
(segments before it are anchored), or for two such plain paths by a
different segment count (route layers match the whole path, `end: true`). An earlier sibling router mounted at a matching
path whose own route layer may match the same way: "router <r> mounted
earlier has route handler <expr> (<METHOD> <path>) that may run first".
The AA5 cell then reads "earlier route handlers ? (n route layers may run
first)". No order-and-path proof states such a handler as a fact.

DA-E3 reads a plain call: Express's `use` is synchronous and a plain call
runs the body to its end before the next statement, so the body's layers
enter the stack at the call. A body `use` under a branch, loop or try, or
after a `return` or `throw` of the body, is an unknown entry
("conditional registration: a return or throw before it in <function>
may skip it" for the latter). A call that fails any condition stays the
escape entry ("passed to <callee>, which may register middleware").

Order: within one function body of one file, source order; across files,
only between two top-level statements, the file that creates the object
first (it evaluates before its importers). Unknown entries (`reason`,
`expression`, `file`, `span`): the object passed to another call ("passed
to <callee>, which may register middleware"), a router mounted earlier at
a matching path that has middleware of its own (Express runs it for every
request that reaches it), a dynamic path or a first argument that may be a
path, order not proven, conditional registration, a spread argument, a
path that matches only case-insensitively or through a route parameter or
wildcard syntax, middleware on some mount chains only, a route not mounted
from a proven app, more than 64 chains. A `use` after the route is not
stated (it runs only when the handler calls `next`). Nothing ever says a
route has no middleware.

## Axum (rows DA-X1, DA-X2; AG-B4b)

Code tables `routes.AxumAuthContract`, `TestAuthAxumContract`, fixture
`auth-axum`. Read in axum 0.7.9 and 0.8.8 (`routing/mod.rs`,
`routing/path_router.rs`): `Router::layer` and `Router::route_layer` both
map over the routes the router holds at the call (`route_layer` leaves the
fallback alone), so a layer wraps the routes added before it; `nest` and
`merge` move the other router's layered endpoints in, so a route keeps its
own router's layers and gains the parent's later ones.

| row | form | fact |
|---|---|---|
| DA-X1 | `.layer(e)` / `.route_layer(e)` on a proven `axum::Router` after the route's registration, or after the `nest` / `merge` / factory return that moved its router in | `layers`: `expression` (as written, whitespace collapsed), `method`, `file`, `span`, innermost first; only layers every chain applies |
| DA-X2 | a layer argument that is a call to `tower_http::validate_request::ValidateRequestHeaderLayer::bearer` / `::basic` (tower-http 0.6.8, feature `auth`), `axum::middleware::from_fn(f)` / `from_fn_with_state(s, f)`, or `axum_login::login_required!` (0.18.0) | `declares: {library, detail}` (tower-http; axum-login with the backend type as written) or `middleware_function: f` |
| DA-X3 | AG-F: `.layer(e)` / `.route_layer(e)` on the method router of a route (`get(h).layer(e)`); `MethodRouter::layer` maps over the endpoints the method router holds at the call (axum 0.7.9 and 0.8.8 `method_routing.rs`), so it wraps the methods chained before it only | `layers` entries with `on: "method_router"`, before every Router layer (innermost first) |
| DA-X4 | AG-F: a layer argument that is a method chain on tower's `ServiceBuilder::new()` whose methods are all tower's layer-adding builder methods (tower 0.4.11, 0.5.2, 0.5.3 `builder/mod.rs`), tower-http `ServiceBuilderExt` methods (0.6.8 `builder.rs`) or identities (`into_inner`, `check_clone`, `check_service`, `check_service_clone`) | `stack` on the layer entry: `expression` (`.layer(e)` / `.option_layer(e)`: `e` as written, with DA-X2's names; other methods: the call as written), `method`, `span`, in written order, which tower applies outermost first (`Stack::new(layer, self.layer)`) |
| DAD | everything else | an entry of `layers_unknown` (conditional layer; layer on some chains only; more than 64 chains), or nothing |

A receiver that is not a proven Router (a tower `ServiceBuilder` client
stack, kftray) gets nothing. Not read: layers outside the router (a
`ServiceBuilder` or function wrapping the served router), a
`ServiceBuilder` chain with a method no list names (the layer is stated as
written, without `stack`), `Handler::layer` (`get(h.layer(...))`),
`from_extractor`.

## Server (rows AA5, AA6)

AA5 "middleware <name>, mounted at <prefix>" and AA6 "<method>
<expression>" (the cell caps the expression at 60 characters), both state
`as_written`, with ", declares <library> (<detail>)" (", optional
(<option>: false)" or ", <option> ?" where DA-E2 reads one) or ",
middleware function <f>", then "mounted middleware ? (n unread forms)" /
"layers ? (n unread forms)" for unknown entries ("earlier route handlers ?
(n route layers may run first)" for DA-E1's earlier route layers). AA1
appends the route arguments' declares. `mounted_error_handlers` is not in
the cell (DA-E5). AA6 expressions are the layer's code with Rust line and
block comments removed (review fix.

## Express oracle

Opt-in, never in CI: `scripts/oracle/express_middleware.sh --scratch DIR`
installs Express 4.21.1 and 5.1.0 (MIT, `--ignore-scripts`), serves the
fixture apps (`app.js`: `app`, `sub`, `other`; `libs.js` and `cjs.cjs`
import the auth libraries and are not loaded) on 127.0.0.1 and sends the
requests of `oracle.json`; each fixture middleware records its name as
written. `TestExpressMiddlewareOracle` compares: with no unknown entry the
trace equals `mounted_middleware` then `middleware`; with unknown entries
that list is a subsequence of the trace. 2026-11-18: 16 requests per
version, 7 exact, 9 within unknown entries, 0 disagree. Review fix: `oracle.json`
gains `more` modules with their own requests, derived from the Express
documentation's `app.all` and `Route` examples, not from the extractor:
`earlier.js` (both versions: `route(p).all(...).get(...)`, an `all` and a
`:id` route before others, a disjoint `all`, an earlier sibling router,
a named and an inline error handler), `earlier4.js` (Express 4 only:
`app.all('/api/*')`, `router.all('*')`) and `express5/earlier5.js` (Express 5
only: the same with `*splat`, since Express 5 refuses an unnamed `*`).
32 requests per version, 13 exact, 19 within unknown entries, 0
disagree. AG-F (DA-E3,
`configured.js`, six more requests): 22 requests per version, 8 exact, 14
within unknown entries, 0 disagree. Review fix: `oracle.json`
gains `more` modules with their own requests, derived from the Express
documentation's `app.all` and `Route` examples, not from the extractor:
`earlier.js` (both versions: `route(p).all(...).get(...)`, an `all` and a
`:id` route before others, a disjoint `all`, an earlier sibling router,
a named and an inline error handler), `earlier4.js` (Express 4 only:
`app.all('/api/*')`, `router.all('*')`) and `express5/earlier5.js` (Express 5
only: the same with `*splat`, since Express 5 refuses an unnamed `*`).
32 requests per version, 13 exact, 19 within unknown entries, 0
disagree.

## gin and chi router middleware (row SH-S18; T029 SH-S)

Rows of the [route security claim
contract](../contract/claims/route-shape-security.md); extractor version 57
caches gin and chi `Use` calls as router calls (their arguments as written).
A gin or chi `route` node may carry:

- `group_middleware`: the router-level middleware proven to run before the
  route's own middleware and handler on every chain that serves it,
  outermost first: `name` (as written), `file` and `span` (the call that
  registers it), `added_by` (`gin.Default` for its Logger and Recovery).
  A gin `Use`, a chi `Use`, a gin `Group(p, mw...)` argument and a chi
  `With` above the route's own With chain count when they end before the
  registration, or before the creation, binding or mount call leading to
  it, in the same function body and outside a branch, loop or function
  literal; a function called with the router before that point
  contributes the unconditional `Use` calls it makes on its parameter.
  gin v1.12.0 copies a group's handlers when a route or child group is
  made; chi v5.5.0 panics on a `Use` after a route and copies inline
  middleware at `With` and `Group`: so a later `Use` never applies.
- `group_middleware_unknown`: `reason`, `expression`, `file`, `span` for
  middleware that may run but is not proven: order not proven, conditional
  registration (also a return before it in a called function, and any
  `Use` or such call in a function body holding a `goto` or a labeled
  `break` or `continue`, which may skip it), applied on
  some chains only, a receiver that may hold another router, a router
  served or mounted through a wrapper, more than 64 chains.

Both are absent when empty; the attribute `middleware` (the route's own
arguments and its With chain) is unchanged; net/http routes get neither.
The security shape reads them (rows SH-S19..SH-S22). The oracle
`scripts/oracle/route_security` compares the handler count gin itself
prints for each fixture route with the stored middleware (equal without
unknown entries, at least as many with them).

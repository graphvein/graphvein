# Claim contract: route security beyond FastAPI

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-S1`..`SH-S24` and
the default row `D`. They say which auth forms of Express, Axum, gin, chi
and Next.js fill the `security` section and `security_schemes` of a route
shape (the shared model of the [route shape claim
contract](route-shape.md), row SM-19), and which gin and chi router
middleware is stored on routes (row SH-S18). The Go table
`securityShapeRows` (`internal/extract/routes/shape_security_contract.go`)
lists the same rows in the same order with the same forms;
`TestSecurityShapeContractTable` keeps the two 1:1 and
`TestSecurityShapeFixtureRows` checks that the fixture
`testdata/fixtures/route-shape-security` reaches every form.

The facts say what the code **declares** and what the library does with
it, never that a route is safe: authorization (roles, ownership) is not a
scheme, and a handler may still refuse a request. A form no row lists goes
to `D`: it states no scheme, and the route's middleware attributes keep it
as written (custom middleware, gin-jwt, sessions, other passport
strategies, axum `from_fn`).

## Rules for every row

- A section with no source (no row below matched) stays `not_read`: auth
  Graphvein does not read is no evidence of none. `provably_none` is never
  stated.
- One source is one middleware, layer or extractor that runs for the
  route; its alternatives are the credential forms it accepts. Every
  source must pass, so the route's requirements are the combinations of
  one alternative per source (SH-S1).
- A requirement is `fact` when every source refuses a request without its
  credential before the handler runs; `intended` when one may let it
  through (an optional flag, a custom callback, a Verifier without an
  Authenticator, an `Option` extractor).
- A detail not read is null (SM-19): a scheme whose credential form is not
  read has type `unknown`; an API key whose name is not read has
  `param_name` null.
- The generated document writes `security` only for a `complete` section
  of `fact` requirements whose schemes are complete
  ([OG-87](openapi-generation.md)); everything else stays in
  `x-graphvein.shape`.

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SH-S1 | requirements and section state | `and`, `alternatives`, `intended`, `cap`, `partial`, `complete` | One requirement per combination of the sources' alternatives (`and`: several sources apply together; `alternatives`: more than one requirement), kind `intended` when any source is (`intended`); more than 16 combinations: no requirement and the reason says so (`cap`). The section is `partial` with the reasons of every source and every middleware not read (`partial`), `complete` only when the framework's complete rule (SH-S12, SH-S22) holds, every requirement is `fact` and every scheme has its details (`complete`). |
| SH-S2 | scheme names | `http`, `apiKey`, `unknown` | `bearerAuth` (`http`, bearer) and `basicAuth` (`http`, basic); `apiKey_<in>_<name>` (`apiKey`; `apiKey_<in>` when the name is not read); a library name for type `unknown`. One name is one definition, so a document never sees two definitions under one name from these rows. |
| SH-S3 | express-jwt | `default`, `getToken`, `optional` | `expressjwt(options)` (8.x) or the 6.x default export, as DA-E2 resolves it: express-jwt reads `Authorization: Bearer <token>` (`/^Bearer$/i`) and answers 401 without it (`default`, scheme `bearerAuth`). A `getToken` option, or options that are not a literal object without spread, make the scheme `unknown` (`getToken`). `credentialsRequired: false` (or a value that is not a boolean literal) makes the requirement `intended` (`optional`). |
| SH-S4 | express-oauth2-jwt-bearer | `auth`, `optional` | `auth(options?)`: the token is read from `Authorization: Bearer`, the `access_token` query parameter or an `access_token` field of a urlencoded body (exactly one), 401 otherwise (`auth`): alternatives `bearerAuth` and `apiKey_query_access_token`; the body form keeps the section partial. `authRequired: false` or a non-literal value: `intended` (`optional`). |
| SH-S5 | express-basic-auth | `default` | The default export called (`basicAuth({...})`): HTTP Basic, 401 without valid credentials: `basicAuth`. |
| SH-S6 | passport.authenticate | `strategy`, `unregistered`, `session`, `callback` | `passport.authenticate(name, options?)` on passport's default instance: the strategy registered under `name` by the only `passport.use(...)` that can define it (a registration whose name is not read makes every lookup fail) is read by SH-S7..SH-S9 (`strategy`). No such registration, a strategy class no row reads, or a name that is not a literal (a list of strategies, which passes when any one does, is one: its elements are not read): scheme `unknown` named `passport-<name>` (`passport` when the name is not read), requirement `intended`, since a strategy whose behaviour is not read may let every request through (passport-anonymous, a custom strategy) (`unregistered`). `session` with no registration that names it or whose name is not read is passport's built-in SessionStrategy, which restores a login session when there is one and otherwise passes the request on: no requirement, and the middleware counts as read for SH-S12 (`session`). A callback argument (anything but an options object) decides what a failure does: `intended` (`callback`). |
| SH-S7 | passport-jwt | `fromAuthHeaderAsBearerToken`, `fromHeader`, `fromUrlQueryParameter`, `other` | `new Strategy({jwtFromRequest: ExtractJwt.x(...)})` with literal options: `fromAuthHeaderAsBearerToken()` -> `bearerAuth`; `fromHeader('name')` -> `apiKey` header (only a lower-case literal: Node lower-cases header names and passport-jwt looks the name up as given); `fromUrlQueryParameter('name')` -> `apiKey` query. Any other extractor (`fromBodyField`, `fromExtractors`, `fromAuthHeaderWithScheme`, a function) or options not read: scheme `unknown` (`other`). Default strategy name `jwt`. |
| SH-S8 | passport-http-bearer | `Strategy` | `new Strategy(verify)` (default name `bearer`): `Authorization: Bearer`, the `access_token` query parameter or body field: alternatives `bearerAuth` and `apiKey_query_access_token`, the body form keeps the section partial. |
| SH-S9 | passport-http Basic | `BasicStrategy` | `new BasicStrategy(verify)` (default name `basic`): `basicAuth`. |
| SH-S10 | express-openid-connect | `requiresAuth`, `requiresLoginCheck` | `requiresAuth()` without an argument: a session cookie whose name the app's `auth()` configuration sets: scheme `unknown`, requirement `fact` (unauthenticated requests are redirected or refused) (`requiresAuth`). With an argument (the `requiresLoginCheck` function, which decides whether a login is required): `intended` (`requiresLoginCheck`). |
| SH-S11 | Express middleware read | `mounted`, `route`, `unknown-entry` | The route's `mounted_middleware` entries (DA-E1, root router first) and then its own middleware arguments are read in that order (`mounted`, `route`); every `mounted_middleware_unknown` entry is a reason and blocks `complete` (`unknown-entry`). Middleware chained with `route(p).all` is not read. |
| SH-S12 | Express complete | `known`, `complete` | A middleware of the closed non-auth list (morgan, compression, cors, helmet, cookie-parser, the express and body-parser parsers, celebrate, express-validator, passport's `initialize()` and `session()`) runs without refusing for credentials (`known`). The section is `complete` when every middleware read is an SH-S3..SH-S10 source or on that list and no unknown entry exists (`complete`). express-openapi-validator is not on the list (it may check the spec's security). |
| SH-S13 | Axum typed Bearer header | `TypedHeader-Bearer`, `Option` | A handler extractor `TypedHeader<Authorization<Bearer>>` (axum 0.6 `TypedHeader` or axum-extra's, the headers crate's types): a missing or undecodable header is rejected (400): `bearerAuth`, `fact` (`TypedHeader-Bearer`); inside `Option<..>`: `intended` (`Option`). |
| SH-S14 | Axum typed Basic header | `TypedHeader-Basic` | `TypedHeader<Authorization<Basic>>`: `basicAuth`, as SH-S13. |
| SH-S15 | tower-http header layers | `bearer`, `basic` | A route layer `ValidateRequestHeaderLayer::bearer(token)` or `::basic(user, password)` (DA-X2 `declares`): 401 unless the Authorization header equals the configured value: `bearerAuth` / `basicAuth`, `fact`. |
| SH-S16 | axum-login | `login_required` | `login_required!(...)` (DA-X2): a session cookie whose name the session layer sets: scheme `unknown`, `fact`. |
| SH-S17 | Axum section | `partial` | The Axum section is never `complete`: layers outside the router (a service wrapping the served router) are not read; `layers_unknown` entries, other layers (`from_fn`, `ServiceBuilder` stacks) and repository or unread extractors are reasons. |
| SH-S18 | gin and chi router middleware | `use`, `call`, `order`, `conditional`, `jump`, `chains`, `gin.Default`, `served-wrapper` | Stored as route attributes `group_middleware` (`name` as written, `file`, `span` of the call that registers it, `added_by` for gin.Default's) and `group_middleware_unknown` (`reason`, `expression`, `file`, `span`). A gin `Use`, a chi `Use`, a gin `Group(p, mw...)` argument or a chi `With` above the route's own With chain applies when it ends before the registration, or before the creation, binding or mount call that leads to the route, in the same function body, outside a branch, loop or function literal (`use`); a function called with the router before that point applies every unconditional `Use` it makes on its parameter, unless a return may end its body first (`call`). gin v1.12.0 copies a group's handlers at each registration and child creation; chi v5.5.0 panics on a `Use` after a route and copies inline middleware at `With` and `Group`, so later calls never apply. Unknown entries: order not proven (another function, overlapping syntax) (`order`), conditional registration (`conditional`), any Use, or call that makes one, in a function body holding a `goto` or a labeled `break` or `continue`, which may skip it (`jump`), middleware on some chains only, kept only when every chain from a root states it (`chains`), a receiver that may hold another router, more than 64 chains. `gin.Default()` adds `gin.Logger()` and `gin.Recovery()` (`gin.Default`). A router served or mounted through a wrapper call is an unknown entry (`served-wrapper`). The route attribute `middleware` (route arguments and the With chain) is unchanged. |
| SH-S19 | gin BasicAuth | `BasicAuth`, `BasicAuthForRealm` | `gin.BasicAuth(accounts)` and `gin.BasicAuthForRealm(accounts, realm)`: 401 without valid credentials: `basicAuth`. (`BasicAuthForProxy` reads Proxy-Authorization: D.) |
| SH-S20 | chi BasicAuth | `BasicAuth` | chi's `middleware.BasicAuth(realm, creds)`: `basicAuth`. |
| SH-S21 | jwtauth | `Verifier`, `Verify`, `Authenticator`, `no-Authenticator` | `jwtauth.Verifier(ja)` reads `Authorization: BEARER <t>` (case-insensitive) or the `jwt` cookie: alternatives `bearerAuth` and `apiKey_cookie_jwt` (`Verifier`); `jwtauth.Verify(ja, finders...)` with jwtauth's `TokenFromHeader`, `TokenFromCookie`, `TokenFromQuery` (`jwt` query parameter) gives one alternative per finder, any other finder a scheme `unknown` (`Verify`). The requirement is `fact` only when `jwtauth.Authenticator` runs later in the same chain (it answers 401 without a valid token) (`Authenticator`); without one it is `intended` (`no-Authenticator`). |
| SH-S22 | Go complete | `known`, `complete` | `gin.Logger()`, `gin.Recovery()` and chi's `middleware.Logger`, `Recoverer`, `RequestID`, `RealIP` never refuse for credentials (`known`). A gin or chi section is `complete` when every route and router middleware is an SH-S19..SH-S21 source or on that list and no unknown entry exists (`complete`). net/http routes get no section from these rows. |
| SH-S23 | next-auth session | `getServerSession` | `getServerSession(...)` imported from `next-auth` or `next-auth/next`, called in an App Router handler or a function whose result it returns: next-auth reads a session cookie whose name depends on its configuration and returns null without one: scheme `apiKey_cookie` (name null), requirement `intended` (the handler decides). Middleware and proxy files are not read for auth. next-auth 5's `auth()` exported by destructuring `NextAuth(...)` is not resolved: D. |
| SH-S24 | chi path served by another registration | `shared-path` | A chi route whose method (a route for every method meets any) and full path (parameter names ignored, trailing slash kept) equal another chi route's served from the same root, or whose path through a mount a route of the router it is mounted on always takes (the route linker then claims no path through that mount): chi v5.5.0 keeps the last registration of a pattern and method and matches a parent router's static route before a mounted router's routes, so this registration may not serve the path. Its section is never `complete` and its requirements are `intended`, with the reason. |
| D | everything else | | No scheme. |

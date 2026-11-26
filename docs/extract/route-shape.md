# Route request and response shape

Graphvein describes what a route accepts and returns: path, query, header
and cookie parameters, the request body, responses and security. Every part
says how Graphvein knows it, and a part Graphvein did not read is shown as
"not read", never as "none". The model is shared by every framework; the
rules are rows SM-1..SM-29 of the
[route shape claim contract](../contract/claims/route-shape.md), and the
decision is recorded in [ADR 0081](../adr/0081-openapi-explorer-try-it.md)
("Shape model").

## What is stored

A `route` node may carry the attribute `shape` (model version 1). A route
without it has every section not read. The attribute holds:

- seven sections, `path`, `query`, `header`, `cookie`, `body`, `responses`
  and `security`, each with a state:
  - `not_read`: Graphvein did not read this part;
  - `provably_none`: the code proves there is nothing (with the evidence);
  - `partial`: some items were read and more may exist (with a reason);
  - `complete`: every item is known, from an enforced declaration or a
    committed spec;
- items, each with a provenance: its kind, the extractor rule, the source
  file and line, the chain of calls followed to reach it, and a confidence:
  - `fact`: a typed declaration the framework enforces at runtime;
  - `read_in_handler`: a name read in the handler or a function it calls,
    with no type and never required;
  - `intended`: a static type nothing checks at runtime (a TypeScript
    generic, `as T`);
  - `returned`: the declared type of a returned value, not validated;
  - `mapped`: from a committed spec or a typed frontend client linked to
    the route;
- `security_schemes` and `models` (named schemas referred to by `ref`);
- `follow`: the depth limit the run used and every followed call chain,
  with its depth and why it stopped.

Schemas use a small JSON-Schema-like language: object (fields with a
presence each: required, optional or unknown), array, string, number,
integer, boolean, null, enum of literals, a reference to a model, and
`unknown` with a reason.

The server serves the checked shape on the endpoint detail (`shape`) and a
compact summary on the endpoint list; see
[serve](../cli/serve.md#get-apiv0apiendpointidscoperoot).

## Model types

When a section's item was read from a named model (a Pydantic class and
its repository bases, a Go struct and the structs it embeds, a Rust serde
type, a TypeScript interface or type alias, a module-level zod or Joi
schema), the route also lists the model's declaration in the attribute
`model_uses` and has a `uses_model` edge to the declaration node, so
change impact reaches the route when the model changes ("through request
body (fact) model UserIn"). Only declarations the extractor itself
resolved, and whose node is emitted, are recorded; each use keeps the
item's section and provenance kind. The rules are rows MT-1..MT-19 of the
[route model types claim contract](../contract/claims/route-model-types.md).
An extractor marks a schema node it read from a declaration with the
declaration's identity (`Schema.Decls`, never stored in the shape) and
calls `routeshape.SetModelUses` with the shape it stored.

## Following calls

Parameters and bodies are often read in functions the handler calls. The
shape extractors follow the handler's values only through proven call
targets (one stored definition), up to a depth limit set by
`index.shape_depth` in [`graphvein.yaml`](../config.md#index) or
`graphvein index --shape-depth` (default 6, from 1 to 10). A chain stops
for one of these reasons, which is stored with it:

| Stop | Meaning |
|---|---|
| `ended` | the value is not passed on |
| `depth_cap` | the next call is proven but deeper than the limit: "depth cap reached (N)", raise the setting to follow it |
| `cycle` | the call returns to a function already on the chain |
| `joined` | the call reaches a function already read through another chain |
| `budget` | 256 functions were already read for this route |
| `resolver_error` | reading a function failed (recorded; other chains continue) |
| `unresolved`, `ambiguous`, `external` | the target is not proven: no definition, several, or a library |
| `transformed`, `opaque_type` | the value is changed before it is passed on, or its type is `any`, `unknown` or generic |

## Adding a framework

A framework's shape extractor (for example FastAPI, Express, gin, Axum,
Next.js) uses the shared package `internal/extract/routeshape` and adds its
own claim table. It never changes the model; if a form needs a claim the
model cannot express, report it as a contract change.

1. **Claim table.** Write the framework's `SH-<family><n>` table first
   (public, under `docs/extract/`): one row per code form read, each naming
   the section, the provenance kind and the model rows it relies on, plus a
   default row for every form not read. Every hazard of the framework
   (sequence parameters that are bodies, dependencies reading headers,
   generics that are not checked, ...) has a row or a refusal. Keep a Go
   table 1:1 with it and test that.
2. **Build the shape.** Start from `routeshape.New(depthLimit)` (every
   section `not_read`), set only the sections the extractor reads, and give
   every item a `routeshape.Provenance` whose `Rule` is the framework's row.
   Mark a section `partial` with a reason whenever a chain stopped before
   the end; use `provably_none` and `complete` only with evidence of kind
   `fact` or `mapped`. A name read in code is `Access: ReadByHandler` (or
   `ReadByDependency` with the chain), presence `unknown`, no schema.
3. **Follow calls.** Implement `routeshape.Resolver` for the language:
   `Visit(ctx, frame)` reads `frame.Function`, records the reads of
   `frame.Value` (with `frame.Chain` as their provenance chain), and
   returns the calls through which the value leaves the function. Return a
   call with an empty `Stop` only for a proven target; otherwise return it
   with `StopUnresolved`, `StopAmbiguous`, `StopExternal`,
   `StopTransformed` or `StopOpaqueType`. Call `routeshape.Follow(ctx,
   start, depthLimit, resolver)` and store `routeshape.Records(section,
   result)` in `shape.Follow.Chains`. Do the following at link time over
   cached syntax, so changing the depth needs no re-parse; if parse-time
   data is needed, cache enough for depth 10 (`routeshape.MaxDepthLimit`).
4. **Store.** `routeshape.ToAttribute(shape)` checks every model rule and
   returns the value for `attrs[routeshape.AttributeKey]`. If it refuses,
   store no shape and record a diagnostic; never store a shape that breaks
   a rule.
5. **Test.** A fixture with a golden output for every row, including the
   default and every hazard; the framework's oracle where one exists (the
   framework's own OpenAPI output, a compiler, a schema library), with any
   disagreement treated as a precision stop. Run the extractor over the
   corpora and report `routeshape.Stats` (`Table()`): how deep chains go and
   how many hit `depth_cap`, so the default depth is set from data.
6. **Version.** Shape extraction changes link output: bump the extractor
   version as for any extraction change.

## Express and Next.js Pages Router API

Rows SH-E1..SH-E36 of the [Express shape claim
contract](../contract/claims/route-shape-express.md). The extractor runs
after the Express and Next.js linkers (`routes.LinkShapes`) and stores a
shape on every Express route and every Pages Router API route.

**Cached syntax.** The TypeScript extractor records, per file, the
parameter-use syntax of its functions and its top-level interfaces and
type aliases (`facts.ShapeSyntax`, extractor version 54): for each
function, its parameters (names, destructured keys, type annotations),
the type annotation of the const that binds it, its declared return type
and the uses of its parameters: property paths read (directly, through
const aliases or by destructuring), method chains called on them
(`res.status(201).json({...})`), calls they are passed to, type
assertions, assignments and `throw` statements. Nothing in it is
resolved; names are resolved and calls followed at link time, so a new
`index.shape_depth` needs no re-parse.

**What is read.**

- Path: the parameters of the composed path templates (Express) or of
  the file name (Pages), typed string, or an array of strings for an
  Express 5 or Pages catch-all; complete when every path has the same
  names, `provably_none` when the template has none.
- Query, headers, cookies, body: names read in the handler and in the
  functions it passes the request (or `req.body`, `req.query`, ...) to,
  never typed or required. Cookies count on Express only when
  `cookie-parser` is proven mounted before the route.
- Validation that runs: zod `parse` (and `safeParse` whose result is
  tested), Joi `validate` with a tested error and `validateAsync`,
  `celebrate` on the route: kind `fact`, with types and presence from the
  schema. A throwing validation counts only as a top-level statement of
  its function, awaited when it returns a promise, before anything may
  send, and reached through calls that are the same; a tested result
  counts only in the handler, tested by a top-level `if` that always
  returns or throws. Anything else is `intended`. Joi types that Joi
  converts by default (`number`, `boolean`) are typed only with
  `strict()`; a Joi body schema not `required()` at its root claims no
  presence unless an Express 4 JSON parser runs first. express-validator
  chains on the route only name fields.
- TypeScript types (`Request<P, ResBody, ReqBody, Query>`,
  `RequestHandler<...>`, `as T`, typed callee parameters): kind
  `intended`, as models built from repository interfaces and type
  aliases.
- Responses: `res.status(n)` chains, `sendStatus`, `redirect`, and a
  send without a status as 200 only when nothing read, and no middleware
  that is not read, may set another status; `res.json` claims
  `application/json` except for 204 and 304, and on Express only when the
  code read sets no Content-Type. Middleware is read too: a known library
  or a function that uses the response only for sends that end the
  request (an auth check answering 401 or 403) changes nothing; any other
  middleware may change the status, the Content-Type or the body, so none
  of them is claimed; inline object literals give the
  field names; a const holding a proven callee's result gives its
  declared return type (kind `returned`). Thrown errors and `next(err)`
  keep the section partial.

**Hazards.** A mounted `express-openapi-validator` and the body parsers
are noted in the sections' reasons (the committed spec, shown as
`mapped` by its own rows, decides the shape there). A Pages Router API
route is one route for every method, so the reads of all `req.method`
branches are merged. Path parameters a Pages handler reads through
`req.query` stay path parameters.

## Next.js App Router route handlers and server actions

Rows SH-N1..SH-N29 of the [Next.js route shape claim
contract](../contract/claims/route-shape-nextjs.md). The same linker as the
Express rows (`routes.LinkShapes`) stores a shape on every App Router route
(`framework: nextjs`) and every Next.js `server_action` node; the Pages
Router API keeps the Express rows.

**Cached syntax.** The TypeScript shape payload (above) also records, from
extractor version 57: the Fetch API body readers as parameter paths
(`await request.json()` is the path `json()`, `formData()`, `text()`,
`arrayBuffer()`, `blob()`; `request.clone()` is the request), `new
URL(request.url)` as the path `url.URL()`, zero-argument calls of
`headers()` and `cookies()` as values of their own, calls of `notFound`,
`redirect`, `permanentRedirect`, `forbidden` and `unauthorized`, and the
return statements of a function (the returned expression, the
initializer of a returned `const` and the arguments of the returned call
or construction). Returns are kept for functions named like an HTTP
method, functions of a `'use server'` file or with the directive, and
functions with other recorded uses; in any other function only returns
that build a response (`Response.json`, `new Response`, `NextResponse.*`)
are kept. Names are resolved at link time (`headers` must be the import of
`next/headers`, `Response` the global, `NextResponse` the export of
`next/server`).

**What is read.**

- Path: the parameters of the route template, strings, an array of
  strings for a catch-all; an optional catch-all is partial. The `params`
  annotation is never read (hazard H15).
- Query, headers, cookies: names read with `get`, `getAll` or `has` on
  `request.nextUrl.searchParams`, `new URL(request.url).searchParams`,
  `request.headers`, `request.cookies`, and on `await headers()` and
  `await cookies()` of `next/headers` (an un-awaited call claims no name
  and no response: Next.js 16 throws on it), in the handler and the functions it passes the
  request (or one of these values) to; never typed or required. A value
  iterated, passed to an unread function or written leaves the section
  partial; a written one keeps no name.
- Body: fields read on the parsed JSON body (`const b = await
  request.json()`, destructuring) and on `formData()`; content type null
  (`json()` parses whatever the Content-Type says). zod `parse` and a
  tested `safeParse` of the JSON body follow the Express enforcement rules
  (a top-level statement, awaited, before any return that may have
  answered, outside `try`; a tested result in the handler): kind `fact`;
  a failed parse makes Next.js answer 500. Anything else is `intended`, as
  are `as T` casts and typed callee parameters.
- Responses: the handler's returns. `Response.json` and
  `NextResponse.json` give their status (200 by default) and
  `application/json` unless `init.headers` is not a literal object or sets
  a Content-Type; `new Response` and `new NextResponse` give their status,
  an empty body for `null`, `text/plain;charset=UTF-8` for a string literal
  and a literal Content-Type header; `NextResponse.redirect` gives 307 and
  `Response.redirect` 302 by default, only for a URL proven absolute (a
  relative one makes both throw); `notFound()`, `redirect()` and
  `permanentRedirect()` of `next/navigation` give 404, 307 and 308 with an
  empty body, only in a function's own body (not in a nested function or
  callback) and when no call site on the way from the handler is inside
  `try` or a promise chain, which may catch them. A returned call of a
  proven function is followed and its own returns read. Statuses a form
  refuses (a body with 204, a redirect with 200, `Response.json(undefined)`)
  are not claimed. A middleware or proxy file whose matcher may cover the
  route (only a literal string matcher can exclude it), or next.config
  `redirects`, `rewrites` or `headers` whose literal source may match it
  (or that are not read), may answer or add headers: no status the code
  does not write is claimed, and no content type or body. A config
  redirect or applicable rewrite without `has` or `missing` whose source
  covers the whole route answers it before the handler: no response is
  claimed. A `headers` rule whose header names are all read literals,
  none of them Content-Type, Content-Length, Content-Encoding or
  Transfer-Encoding in any case, only adds response headers and changes
  no claim. Rule and header lists may be grown with `push()`,
  spread or `concat()`. A file that
  exports `dynamic = 'force-static'` (or a `dynamic` that is not a string
  literal) hands its handlers an empty query, headers and cookies and may
  answer from a build-time render (`dynamic = 'error'` makes Next.js 14
  throw on any read of the request, the body included): no name read there is claimed, and no
  response either when the handler uses any of those values.
- Server actions: path and query are not read (an action is a POST to the
  page that renders it); a `FormData` (or untyped) parameter's fields read
  form the body; one typed parameter gives an `intended` body with its
  type, and a zod parse of it a `fact`: the arguments travel as a list, and
  the schema describes argument 1 only (further arguments are not
  checked); results (returned object literals, the declared return type)
  are responses with status `default` and no content type: the result is
  not an HTTP response of its own.

**Security.** Next.js route handlers: see "Security beyond FastAPI"
below.

**Not read.** Joi and other validators, streamed
responses, `NextResponse.next()` and `rewrite()`, other route segment
config (`revalidate`, `runtime`, `dynamic` `'auto'` or `'force-dynamic'`;
the handler sees the request as sent), redirects and
`notFound()` in server actions, several typed action arguments.

The oracle `scripts/oracle/nextjs_shape` builds the fixture
`testdata/fixtures/nextjs-shape` with Next.js 14.1.0, 15.5.4 and 16.2.10,
serves it on 127.0.0.1 and drives every claimed status, content type and
empty body, the requests a `fact` body must refuse and accept, the server
actions through their action ids, and compares the zod schemas
(`toJSONSchema`) and the TypeScript checker's view of every name read and
every model.

## Axum

The Axum shape extractor reads Axum routes (axum 0.3 to 0.8). Its rows
are `SH-X1`..`SH-X29` of the
[Axum route shape claim contract](../contract/claims/route-shape-axum.md).

- **What is read.** The handler is the function or method the route
  names; inline closures and services are not read. Every Rust file whose
  text mentions `axum`, `serde` or `http` caches its type declarations
  (serde derives and attributes, fields, generics, aliases) and its
  functions' signatures, returned expressions and request value uses
  (Rust payload `RustShape` version 2), so the shape is built at link time
  and a new `--shape-depth` needs no re-parse.
- **Parameters.** The route's template gives the path parameters
  (required facts); `Path<T>` types them by position (scalar, tuple) or by
  serde field name (struct). `Query<T>` and `Form<T>` on `GET` (axum's
  form extractor reads the query on `GET` only, also for `HEAD` routes)
  give declared query parameters from the
  struct's fields: required unless `Option` or `default`, typed only when
  serde_urlencoded reads the type from one value (scalars, unit enums and
  newtypes of them). `Form<T>` on a route answering several methods
  (`any`) may read the query or the body: both stay partial. `TypedHeader<T>` of a known headers type gives a
  required header (optional inside `Option`). `HeaderMap` reads
  (`get`, `get_all`, `contains_key` with a literal, an `http::header`
  constant or a string constant) give names read in code, never typed or
  required, also when read by a repository `FromRequestParts` or
  `FromRequest` extractor (then "read by a dependency"). Any other use of
  a request or parts value (its URI, `parts.extract::<..>()`, a value
  stored or rebound), and any followed chain of one that is cut (depth
  cap, a callee not read), leaves the query, cookies and body partial.
- **Bodies.** `Json<T>` (`application/json`) and `Form<T>`
  (`application/x-www-form-urlencoded`) give the serde schema of `T` as a
  fact; `String` and `Bytes` a body of any content type. Only one
  extractor reads the body (hazard H16).
- **Types.** serde's derive is read: `rename`, `rename_all` (serde's own
  per-character rules), `alias`,
  `default`, `skip*`, `skip_serializing_if`, `deny_unknown_fields`,
  `Option`, unit enums, generic structs instantiated at the use and type
  aliases expanded. A type without the derive (a custom implementation),
  `with`/`serialize_with`/`deserialize_with`, `flatten`, tagged or
  untagged enums, enums with data and library types other than `Uuid` and
  chrono dates are `unknown` with the reason (hazard H17; `validator`
  constraints are not read). So is a type carrying an attribute that may
  rewrite serde's view and is not read (`serde_as`, a `cfg_attr` applying
  `serde` or a derive, any attribute macro outside a short inert list).
  Serialized floats are nullable (serde_json writes NaN as `null`).
- **Responses.** A declared `Json<T>`, `String`, `Html<T>`, `StatusCode`
  or `(StatusCode, T)`, and for `impl IntoResponse` the returned
  expressions (`Json(value)`, `(StatusCode::X, ..)`, `StatusCode::X`,
  `Redirect::temporary`/`permanent`, string literals, calls followed into
  the callee). `Result<T, E>`: the `Ok` values as `T`; `Err(E::V)` takes
  the status of variant `V` when every result of `E`'s `IntoResponse` impl
  that can answer for `V` (through `match self` arms, also in a status
  method) is the same `StatusCode` constant and no such arm has a guard.
  A `Result` whose values are all `Err` claims no `Ok` response. Values
  with the same status and content type but different schemas give an
  `unknown` schema. A tuple's header parts may set the content type: it
  is claimed only from a literal `content-type` part or when no part sets
  it. Text content types depend on the axum version (`text/plain` in 0.3,
  `; charset=utf-8` since 0.4) and are claimed only when the version is
  proven from `Cargo.lock` or `Cargo.toml`. Errors returned with `?`,
  panics and extractor rejections are not read: the responses section is
  always partial. `Redirect::to` changed status between axum versions and
  is not claimed.
- **Calls.** A request value (`HeaderMap`, the request, an extractor's
  parts) or a returned call is followed when the call's target resolves
  to one repository function or method (`helpers::f`, `Self::f`,
  `self.f()`); methods of other values are `unresolved`, library
  functions `external`, and a name the function binds itself (a local
  closure, a parameter) never resolves to an item of that name.
- **Security.** See "Security beyond FastAPI" below.
- **Not read.** Cookies, middleware (layers) other than for security,
  extractor rejection statuses, `Option`/`Result`-wrapped extractors other
  than `Option<TypedHeader<T>>`, and library extractors such as
  `Multipart` or `CookieJar`.

The oracle `scripts/oracle/axum_shape` builds the fixtures
`testdata/fixtures/route-shape-axum` (axum 0.8.8) and
`testdata/fixtures/route-shape-axum03` (axum 0.3.4) with the real crates
(the fixture `testdata/fixtures/route-shape-axum-versions` covers the
version reading and is golden-only),
serves them on 127.0.0.1 and compares every claimed status, content type,
required and optional field and parameter, type, enum and closed object
with real requests and serde_json output.

## FastAPI

The FastAPI and Starlette shape extractor reads rows SH-F1..SH-F39 of the
[FastAPI route shape claim contract](../contract/claims/route-shape-fastapi.md).
It follows FastAPI 0.161.1's own rules, checked against FastAPI's
`app.openapi()` and a TestClient run on the fixtures
`testdata/fixtures/route-shape-fastapi`,
`testdata/fixtures/route-shape-fastapi-handlers`,
`testdata/fixtures/route-shape-fastapi-unpack` and
`testdata/fixtures/route-shape-fastapi-assign`
(`scripts/oracle/fastapi_shape.sh`). Rules that depend on the FastAPI
version (return annotations validated from 0.89.0, form models from
0.113.0) apply only when the nearest `pyproject.toml` requiring `fastapi`
proves the version with a lower bound.

- **Cached syntax.** Files that import `fastapi`, `starlette`, `pydantic`
  or `sqlmodel` (gate SG) cache their function signatures, the body sites
  that can add to a shape (raises, returned calls, calls that pass the
  request on, names read on the request) and the members of classes with
  bases. A call into any other file is a proven target whose body is not
  read: its chain stops with `resolver_error` and the sections stay
  partial. Exception handler registrations and `route_class` and
  `default_response_class` assignments
  are cached in every Python file.
- **Parameters.** The handler and its dependency tree (`Depends()` and
  `Security()` on functions and classes with `__init__`, `Annotated`
  aliases, and the `dependencies=` lists of the route, routers,
  `include_router` and the app) are classified as FastAPI does: a name in
  the path template is a path parameter, a scalar a query parameter, a
  sequence or model a body field (a `list[UUID]` without `Query()` is
  body, not query), markers decide the rest. Framework values (`Request`,
  `Response`, `BackgroundTasks`, ...) are classified by annotation, never
  by name. `UploadFile` and sequences of it are file fields; a `Form()`
  parameter typed with a model is that model's fields. `Annotated`
  metadata other than constraints, `AfterValidator` and documentation
  (a `BeforeValidator`, ...) makes the type unknown, as do Pydantic
  before/wrap/plain field validators, `populate_by_name` with an aliased
  field and an enum's `_missing_`. When the whole tree is read, an empty
  section is `provably_none`. Reading `request.url` or `request.state`
  leaves the sections partial.
- **Names read on the request.** `request.headers["X"]`,
  `.get("X")`, cookies, query and path parameters read in the handler or a
  dependency (or a function the request is passed to) are listed as
  "read by the handler" or "read by a dependency": never typed, never
  required, never OpenAPI parameters. Security scheme credentials (the
  `Authorization` header of an OAuth2 scheme) are in the security section,
  not the header section.
- **Responses.** The success status and model come from `status_code=`
  (else the response class's default: 307 for `RedirectResponse`),
  `response_model=` (which overrides the return annotation) or the return
  annotation (`fact` when the manifest proves FastAPI 0.89.0, else
  `returned`). The success item is `fact` only when every return of the
  handler is proven not to be a `Response` object (FastAPI sends one as
  is); otherwise it is `intended`. `response_model_include`, `_exclude`,
  `_exclude_none` and `by_alias=False` make the success schema unknown;
  generator handlers (streamed) have an `intended` success item without a
  content type. `responses=` entries are documentation (kind
  `intended`); `raise HTTPException(status_code=n)` in the handler, a
  dependency or a followed function adds status `n` unless it is inside
  `try` or after a `yield`; the 422 validation error is stated for routes
  that declare a parameter or body, `fact` only when some declared item
  can fail validation. Project exception handlers for `HTTPException` or
  `RequestValidationError` (and any `FastAPI(**options)`, which may pass
  them) lower those statuses to `intended`; a custom `route_class` on the
  route's router lowers every fact of the route. A `default_response_class`
  (or keyword unpacking, which may pass one) applies to the routes the
  object serves, as FastAPI resolves it: the routes registered on it and
  on every router included into it, directly or through other routers,
  and the routes of the router an `include_router(...)` call with one
  includes. Those routes get no success content type, and no default
  success status unless the class is FastAPI's or Starlette's with
  status 200; other routes keep theirs. Any `x.default_response_class =
  ...` assignment makes every route's default success status and content
  type not read (object and order not modelled). The section is always partial:
  middleware is not read.
- **Security.** Schemes come from the route's security dependency facts
  (`docs/extract/auth.md`) and are stated only with every field their
  type needs; FastAPI's own `OAuth2PasswordBearer` with a computed
  `tokenUrl` is stated with `token_url` null. A repository subclass of a
  scheme class is not stated: its own `__call__` is followed as a
  dependency. The route's requirement holds all its schemes, `intended`
  when a scheme has `auto_error=False`.
- **Chains.** The dependency tree is recorded under section `query`, a
  request value under the section it reads (`header` for the request
  object) and returned values under `responses`.

## gin, chi and net/http

The Go shape extractor reads gin v1, chi v5 and net/http routes. Its rows
are `SH-G1`..`SH-G38` of the
[Go route shape claim contract](../contract/claims/route-shape-go.md).

- **What is read.** The handler is the route's function, method or
  function literal. Every function that declares a parameter of type
  `*gin.Context`, `*http.Request` or `http.ResponseWriter` has its calls
  cached (Go payload version 10), so the shape is built at link time and a
  new `--shape-depth` needs no re-parse.
- **Parameters.** `c.Param`, `chi.URLParam`, `r.PathValue`, `c.Query` and
  its variants, `r.URL.Query().Get`, `r.FormValue`, `c.GetHeader`,
  `r.Header.Get`, `c.Cookie` and `r.Cookie` give names read in code: never
  typed, never required. A `strconv` parse of the value is noted by its
  row (`SH-G4`), not as a type. Names may come from a literal, a package
  string constant or a helper's caller (`withID(c, "id", ...)`).
- **Bodies.** `c.ShouldBindJSON(&v)` and `c.BindJSON(&v)` give the type of
  `v` as a `fact` when gin enforces it (`BindJSON`, or an error checked
  with a `return`), with `binding:"required"` fields required; an
  unchecked bind is `intended`. `json.NewDecoder(r.Body).Decode(&v)` is
  read the same way, without required fields. gin's `Bind` and
  `ShouldBind` choose the binder from the Content-Type, so their content
  type is not read (hazard H18); on a `GET` route they bind the query.
  `ShouldBindQuery` and `ShouldBindUri` give declared query and path
  parameters from the `form` and `uri` tags.
- **Responses.** `c.JSON`, `c.String`, `c.Status`, `c.AbortWith...`,
  `w.WriteHeader`, `http.Error`, `http.NotFound`, `http.Redirect` and
  `json.NewEncoder(w).Encode(v)` give statuses (literals, `net/http`
  constants or a caller's argument) and bodies of kind `returned`. An
  `Encode` without an earlier `WriteHeader` is `200` only in the handler
  itself and only when nothing else wrote to the writer first.
- **Types.** Struct fields follow encoding/json (json tag names, `-`,
  `omitempty`, the `string` option, embedded structs with Go's dominance
  rules). A type with a marshaling method, a generic, an interface, a type
  outside the repository or a recursive reference is `unknown` with the
  reason.
- **Calls.** A call that passes a request value is followed when its
  target is proven: a module function, a method of the function's own
  receiver type or of a variable's declared concrete type. A method of an
  interface is `ambiguous`; library functions are `external`.
- **Security.** From the route's middleware and its router-level
  middleware (attribute `group_middleware`); see "Security beyond FastAPI"
  below.
- **Not read.** Middleware other than for security, `c.XML` and the other
  gin renderers, `w.Write`, and the
  routes of oapi-codegen generated servers, whose sections say
  "generated from spec" (the committed spec is their shape source).

The oracle `scripts/oracle/go_shape` compiles the fixture
`testdata/fixtures/go-shape` with the real encoding/json, gin and chi and
compares the claimed field sets, types, presences, statuses and binds with
real marshaling and real responses on 127.0.0.1.

## Security beyond FastAPI

Rows SH-S1..SH-S24 of the [route security claim
contract](../contract/claims/route-shape-security.md) fill the `security`
section of Express, Axum, gin, chi and Next.js App Router routes from a
closed list of auth libraries whose credential form is fixed or read from
literal options. Custom auth (a repository middleware, an axum `from_fn`,
gin-jwt, a session the library names from its configuration) states no
scheme: it stays as written in the route's middleware attributes, and a
section with no source stays `not_read`.

- **Express.** The route's `mounted_middleware` (every chain, root first)
  and its own middleware arguments, in order: express-jwt (Bearer;
  `getToken` makes the scheme unknown, `credentialsRequired: false` the
  requirement intended), express-oauth2-jwt-bearer (Bearer or the
  `access_token` query parameter; the body form keeps the section
  partial), express-basic-auth (Basic), `passport.authenticate` with the
  strategy its `passport.use` registers (passport-jwt's
  `fromAuthHeaderAsBearerToken`, `fromHeader` with a lower-case name and
  `fromUrlQueryParameter`; passport-http-bearer; passport-http Basic; a
  strategy whose registration or class is not read, or a strategy list,
  is an intended requirement, and the built-in `session` strategy states
  none) and express-openid-connect's `requiresAuth()` (a session cookie:
  scheme unknown; with a `requiresLoginCheck` argument the requirement is
  intended). The section is
  complete when every other middleware is on a closed list of libraries
  that never refuse for credentials (cors, helmet, morgan, compression, the
  parsers, celebrate, express-validator) and no mounted middleware is
  unknown.
- **Axum.** `TypedHeader<Authorization<Bearer>>` and `<Basic>` extractors
  (`Option` makes the requirement intended), tower-http
  `ValidateRequestHeaderLayer::bearer` and `::basic` layers, axum-login's
  `login_required!` (scheme unknown). Always partial: layers outside the
  router are not read.
- **gin and chi.** The Go route linker stores router-level middleware on
  routes: `group_middleware` (a gin `Use` or `Group` argument, a chi `Use`
  or a `With` above the route, `gin.Default`'s Logger and Recovery, applied
  when it is registered before the route or the child router that leads to
  it, in the same function, unconditionally, or in a function called with
  the router before that point) and `group_middleware_unknown` (order not
  proven, conditional, in a function holding a `goto` or labeled `break`
  or `continue`, on some chains only, a router served through a wrapper).
  A chi route whose method and full path another route of the same served
  router has may not serve it (chi keeps the last registration and prefers
  a parent's static route to a mounted router's): its section is never
  complete and its requirements are intended. gin's `BasicAuth` and `BasicAuthForRealm`, chi's
  `middleware.BasicAuth` and jwtauth's `Verifier` / `Verify` with a later
  `Authenticator` (Bearer, or the `jwt` cookie or query parameter) are
  read; complete when every other middleware is gin's Logger or Recovery
  or chi's Logger, Recoverer, RequestID or RealIP.
- **Next.js.** `getServerSession` of next-auth 4 in a route handler: a
  session cookie whose name depends on the configuration (key name not
  read), requirement intended. next-auth 5's `auth()` exported by
  destructuring is not resolved.
- **Generated document.** A complete section of fact requirements with
  complete schemes becomes the operation's `security` and
  `components.securitySchemes` (OG-87); everything else stays in
  `x-graphvein.shape`.

The oracle `scripts/oracle/route_security` serves the fixture
`testdata/fixtures/route-shape-security` with the real libraries (Express
4.21.1 and the npm versions its package.json pins, gin v1.12.0, chi
v5.5.0, jwtauth v5.4.0, axum 0.8.8, axum-extra 0.10.3, tower-http 0.6.8) on
127.0.0.1 and checks that a request with each claimed requirement's
credentials reaches the handler and that one without them (or missing one
scheme of a requirement) does not.

## Committed specs

A route documented by a committed OpenAPI 3.0, 3.1 or 3.2 or Swagger 2.0
operation (one inferred `documents_route` edge of the
[OpenAPI import](openapi.md), exact matches only) also shows what that
operation says: rows SH-O1..SH-O19 of the
[committed spec claim contract](../contract/claims/route-shape-spec.md).
The items are added at link time, after every framework stored its own
shape, and no syntax is cached.

- **Kind.** Every item, scheme and model from a spec is `mapped`, with
  the link's confidence (0.9, or 0.7 when the spec's server names an
  origin) and the spec file and line it was read from; schema models are
  named by their file and JSON pointer. Nothing from a spec is a `fact`.
- **Side by side.** The code's items, states and models are never
  changed. A section the code did not read and the spec fills becomes
  `partial` with the reason "not read in the code: every item is mapped
  from the committed spec"; a section the code read keeps its state and
  takes the spec's items beside its own; a `provably_none` section takes
  none. A route documented by several operations takes none.
- **Spec differs.** Where a code fact and the spec contradict each other
  (a parameter's presence or type, parameter names of a proven section, a
  body where the code proves none, a success status the spec does not
  document, authentication), the section's reason says "spec differs:"
  and what differs. Graphvein never decides which side is right. Media
  types compare by type and subtype without parameters, and a media range
  such as `*/*` covers what it names.
- **What is read.** Parameters (merged from the path item, with
  OpenAPI's ignored headers left out), request bodies per media type and
  whether the spec requires one (in the reason: the model has no body
  presence), responses per status and media type (a response without
  content is the empty body), security requirements and schemes. Schemas:
  types, `nullable` (3.0), type lists and a `null` member of `anyOf` or
  `oneOf` (3.1; a `oneOf` only when its other member rejects null), enums
  and `const` (a number float64 would change makes the enum `unknown`),
  object fields with `required`, `additionalProperties`, arrays,
  references within the document (cycles end at the model; a cycle of
  references alone is `unknown`, "reference cycle") and a single `allOf`
  member. A field marked `readOnly` or `writeOnly` anywhere its type is
  read through has presence `unknown`, and the section's reason says so:
  the shape keeps one view of a schema for requests and responses.
  Other compositions, discriminators, conditionals, references to other
  documents and anything deeper than 32 levels are `unknown` with the
  reason. Swagger 2.0 response keys such as `2XX` are not stored (ranges
  are OpenAPI 3).
- **Generated document.** The [generated OpenAPI
  document](../contract/claims/openapi-generation.md) still describes the
  code only: mapped items stay in `x-graphvein.shape.not_standard`, and a
  section filled only by the spec reads as not read there. The Explorer
  shows mapped items with their chip and the notes under each section.

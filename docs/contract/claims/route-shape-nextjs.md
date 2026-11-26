# Claim contract: Next.js App Router route handler and server action shape

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-N1`..`SH-N29` and
the default row `D` say which code forms the Next.js App Router route
handler and server action shape extractor reads and what it may claim from
each. Every claim is stored with the model of the [route shape claim
contract](route-shape.md) (rows SM-1..SM-29), which it never changes. The
Go table `routes.NextShapeRows`
(`internal/extract/routes/shape_next_contract.go`) lists the same rows in
the same order with the same titles, and `TestNextShapeContract` keeps the
two equal and checks that the fixture `testdata/fixtures/nextjs-shape`
reaches every row. How the extractor works is described in
[route shape](../../extract/route-shape.md#nextjs-app-router-route-handlers-and-server-actions).
The Pages Router API is read by the [Express rows](route-shape-express.md).

## Vocabulary

- **Handler**: the function a route node's `handler` names (row NX-R2 of
  the [Next.js route contract](nextjs-supabase.md): an exported `GET`,
  `POST`, ... resolved to one function). **Action**: the function a
  `server_action` node's `handler` names.
- **Request**: the handler's first parameter. Its parts are read by path:
  `request.nextUrl.searchParams` and `new URL(request.url).searchParams`
  (the query), `request.headers`, `request.cookies`, `await
  request.json()` (the JSON body), `await request.formData()` (the form
  body), through const aliases and destructuring as the Express rows read
  them. `request.clone()` is the request.
- **next/headers values**: `await headers()` and `await cookies()` whose
  name is the import of that export of `next/headers` where it is called
  (not shadowed by a local or a parameter). A call without `await` is
  **un-awaited**: Next.js 16 removed synchronous access (the call returns
  a promise, and a member read on it throws), while 14 and 15 allow it.
- **Map read**: `get('x')`, `getAll('x')` or `has('x')` with a string
  literal on a query, headers, cookies or form data value.
- **Returned response**: the expression of a `return` statement of the
  handler (or an arrow function's expression body; never a return of a
  nested function), or the initializer of a `const` it returns.
- **Own body**: a function's statements outside the nested functions,
  arrow functions and callbacks it defines.
- **Caught chain**: a followed function (SH-N25) reached through a call
  site, in the handler or in any function between, that is inside a `try`
  block or is the receiver of `.then`, `.catch` or `.finally`.
- **Proven absolute URL**: a string literal starting with `http://` or
  `https://`; `new URL(x, base)` of the global `URL` whose `base` is such a
  literal or the handler's `request.url` or `request.nextUrl`; `new
  URL(request.url)`; `request.nextUrl.clone()`; directly or through the
  `const` the argument is bound to. The request is the route handler's
  first parameter, read in the handler itself (not in a followed
  function).
  `Response` must be the global (no binding of the name in scope) and
  `NextResponse` the export of `next/server`.
- **Proven call target**, **top-level call**, **tested result** and
  **enforced**: as the [Express rows](route-shape-express.md#vocabulary)
  define them, except that **may have sent** is a return before the call
  (other than a returned response with a literal 4xx or 5xx status in a
  branch of an `if` statement directly in the body that always returns or
  throws) or a `redirect()`/`permanentRedirect()` of `next/navigation`.
- **Middleware**: a file `middleware.<ext>` or `proxy.<ext>` (ts, js, mjs,
  mts, tsx, jsx, cjs, cts) in the route's project directory or its `src`
  directory. It **may run** before the route unless its `export const
  config` is an object literal whose `matcher` is a string literal of
  literal segments and `:name`, `:name*`, `:name+`, `:name?` parameters
  that cannot match the route template (an array or any other matcher
  form may match).
- **Config rules**: the `redirects`, `rewrites` and `headers` of the
  project's `next.config.*` (the file NX-P2 reads). A key is read when its
  function returns a rule list: an array literal, a top-level `const`
  bound to one and changed only with `push()`, spreads of such lists, and
  `concat()` of them, every element an object literal (rewrites also as
  an object of `beforeFiles`, `afterFiles` and `fallback` lists). A rule's
  source is read when it is a string literal or a top-level `const` bound
  to one (not with `basePath` or `locale` keys). Rules **may apply** to the
  route when the config is not read as an object, when one of those keys
  is not read (or a spread may set it), or when a rule whose source is not
  read, or whose source may match the route template as a middleware
  matcher does, is a redirect, a headers rule that may set a content
  header (SH-N30), a `beforeFiles` rewrite or
  (for a template with a parameter) an `afterFiles` rewrite; Next.js
  serves a route before its `fallback` rewrites, and a static route before
  `afterFiles` rewrites. Such a redirect or rewrite **answers** the route
  when it has no `has` or `missing` condition, the project keeps the
  default `trailingSlash`, and its source covers the whole template (equal
  literal segments, `:name` for one segment, a final `:name*`, or a final
  `:name+` for one or more segments that are not a catch-all).
- **Static route config**: `export const dynamic` of the route's file
  whose value is `'force-static'` or not a string literal. Next.js then
  hands every handler of the file a request whose query, headers and
  cookies (also those of `next/headers`) are empty, and may answer `GET`
  from a build-time render; the body is passed unchanged (Next.js 14.1.0,
  15.5.4 and 16.2.10). `export const dynamic = 'error'` is a static route
  config too, which also covers the body: Next.js 14.1.0 throws (500) on
  any read of the request's URL, query, headers, cookies or body, while
  15.5.4 and 16.2.10 pass the request as sent.

## Claim contract

| Row | Title | Forms read | Claim | Not claimed |
|---|---|---|---|---|
| SH-N1 | App Router path parameters from the route template | every `{name}` and `{*name}` of the route's paths (NX-R1: `[name]`, `[...name]`) | `path` item: declared, kind `fact`, required, type string; a catch-all an array of strings; section `complete` with the template as evidence | an optional catch-all `[[...name]]`: `partial`; the `params` type annotation (`Promise<{ id: string }>`) is never read (H15) and reads of `params` add nothing |
| SH-N2 | a route template without parameters | a template without `{` | `path` `provably_none`, evidence the template (kind `fact`) | |
| SH-N3 | query names read | map reads on `request.nextUrl.searchParams` or `new URL(request.url).searchParams` | `query` item `read_by_handler`, kind `read_in_handler` | a name not a literal: SH-N4 |
| SH-N4 | a request value read as a whole or written | the request, its URL (`request.url`, `request.nextUrl`, a `URL` of it, `search`, `href`) or a map passed to an unread function, iterated (`forEach`, `entries`, `Object.fromEntries`), spread or read with a computed key; a map written (`set`, `append`, `delete`) or the request written | a note in the section's reason; a written section keeps no name read | no item |
| SH-N5 | header names read on the request | map reads on `request.headers` | `header` item, kind `read_in_handler`; names compare case-insensitively (the Fetch `Headers` object ignores case) | |
| SH-N6 | header and cookie names read through next/headers | map reads on `await headers()` and `await cookies()` of `next/headers`, in the handler, the action or a function the request is passed to | `header` or `cookie` item, kind `read_in_handler` | a `headers` or `cookies` that is not that import: nothing; an un-awaited call used other than bound to a `const`: no name read through it and a note; in a route handler or a function its responses are followed into, no response is claimed (it throws on Next.js 16) |
| SH-N7 | cookie names read on the request | map reads on `request.cookies` | `cookie` item, kind `read_in_handler` | |
| SH-N8 | JSON body fields read | `const b = await request.json(); b.x`, destructuring, `(await request.json()).x` | one body variant of kind `read_in_handler`, content type null (`json()` parses whatever the Content-Type), an object whose fields have presence and type `unknown` | |
| SH-N9 | form data fields read | map reads on `await request.formData()`, and on a server action's `FormData` (or untyped) parameter | the field names in the `read_in_handler` body variant, content type null (multipart or urlencoded) | |
| SH-N10 | a body read as text, bytes or a stream | `request.text()`, `arrayBuffer()`, `blob()`, `request.body` | a note: the body's fields are not read | |
| SH-N11 | a body passed on changed | the JSON body spread into a value passed to a function, a member of it passed to a proven function | chain stop `transformed` with the site; the section is `partial` | the callee's reads |
| SH-N12 | zod parse of the JSON body | `S.parse(body)`, `S.parseAsync(body)` (awaited) with `body` the parsed JSON body (or a server action's one argument) and `S` a zod schema resolved in the repository (the Express rule SH-E20 forms), enforced | a body variant of kind `fact` with the schema; a failed parse throws, so Next.js answers 500. A server action: the schema of argument 1 only (its arguments are sent as a list; further arguments are not checked), said in a note | not enforced: SH-N14 |
| SH-N13 | zod safeParse with the result tested | `S.safeParse(body)` (`safeParseAsync` awaited) whose result is tested in the handler before anything may have sent | as SH-N12 | as SH-N12 |
| SH-N14 | a validation that is not tested or not enforced | SH-N12/SH-N13 forms inside a `try` block, after a return that may have sent, not top-level, reached through such a call, or a `safeParse` not tested | untested: nothing; not enforced: the schema as kind `intended` (required presence becomes `unknown`) with a note | |
| SH-N15 | zod schema forms | the zod factories and modifiers of the Express row SH-E20 | the schema with presence as SH-E20 states it | every other form: type `unknown` with a reason |
| SH-N16 | TypeScript types of the body | `(await request.json()) as T`, `const b: T = await request.json()`, the JSON body passed to a proven function whose parameter is typed `T` | a body variant of kind `intended` (types as models, the Express row SH-E22) | `any`, `unknown`, `{}`, generics |
| SH-N17 | Response.json and NextResponse.json | a returned `Response.json(v, init)` or `NextResponse.json(v, init)`; `init` absent, `undefined` or an object literal without a spread whose `status` is absent or a number literal | a response at that status (200 without one), kind `read_in_handler`; `application/json` when `init` has no `headers` or a literal object without a Content-Type key; body: an object literal's fields (presence and type `unknown`), a literal with `toJSON` unknown | a status outside 200..599 or one without a body (101, 103, 204, 205, 304: the method throws), a value argument absent, `undefined` or a function literal (the method throws, Next.js answers 500): SH-N22; a returned const: no content type (its headers may be changed) |
| SH-N18 | new Response and new NextResponse | a returned `new Response(body, init)` or `new NextResponse(body, init)` with `init` as SH-N17 | a response at that status (200 without one); no body (`null`, `undefined`, absent): schema `null`, no content type; a string or template literal: schema string, `text/plain;charset=UTF-8`; a literal Content-Type header in `init.headers`: that content type | a body with a status that has none (the constructor throws), headers not a literal object: SH-N22; other bodies: schema not read |
| SH-N19 | redirect responses | a returned `NextResponse.redirect(url, init)` (`init` a number literal or an object literal with a literal `status`) or `Response.redirect(url, status)` with `url` a proven absolute URL | `307` (`NextResponse`) or `302` (`Response`) without a status, else the literal; one of 301, 302, 303, 307, 308; schema `null`, no content type | a `url` not proven absolute (a relative URL makes both methods throw: Next.js answers 500), another status (the method throws), a status not read: SH-N22 |
| SH-N20 | notFound, redirect and permanentRedirect from next/navigation | a call of the `next/navigation` import `notFound()`, `redirect(url)`, `permanentRedirect(url)` in the own body of a route handler or of a function its responses are followed into, not inside a `try` block and not in a caught chain | `404`, `307` or `308`, schema `null`, no content type | a second (type) argument, inside a `try` block or in a caught chain: SH-N22; outside the own body (a nested function or callback): SH-N23; in a server action: SH-N28 |
| SH-N21 | the declared return type of a proven callee in a response body | `Response.json(v)` with `v` a const bound to (or `v` itself) a call of a proven function with a declared return type (`Promise<T>` read as `T`) | a second response item at the same status of kind `returned` | `any`, `unknown`, an unresolved type |
| SH-N22 | a status or content type not read | a non-literal status (`{ status }`, a variable), a status a form refuses, headers not read, a Content-Type value not a literal, a next/navigation call in a `try` block; a `HEAD` handler in a file that also exports `GET` (Next.js 14.1.0 answers `HEAD` from the prerendered `GET` output without running it) | no item (or no content type; for that `HEAD` handler no response at all); a note in the reason | |
| SH-N23 | thrown errors and returns not read | `throw`, a return without a value, a returned value that is no SH-N17..SH-N19 form and no call, a function with no cached syntax, a next/navigation call outside the own body | a note; the section stays `partial` or `not_read` | |
| SH-N24 | middleware, config rules and static route config before the handler | a middleware or proxy file that may run, config rules that may apply, a static route config (vocabulary) | middleware and config rules: no response without an explicit status is claimed and no response claims a content type or a body; notes on the query, header and cookie sections (they may answer, rewrite the request or change its headers). A config rule that answers the route: no response at all, a note naming the rule. Static route config: the query, header and cookie sections (with `'error'` also the body section) claim nothing (`not_read` with the reason), and when the handler (or a function the request flows to) uses any of them, no response is claimed (its statuses may depend on the emptied values) | |
| SH-N25 | call following through proven targets | the request, one of its parts (query, headers, cookies, JSON body, form data) passed as an argument; a returned call of a function | the callee is read with the same rows (`routeshape.Follow`, depth limit `index.shape_depth`); a returned call's own returns are read for the responses, and whether the chain is caught is carried down it | unproven targets stop with `unresolved`, `ambiguous` or `external` |
| SH-N26 | a handler that is not a proven function | an export bound to a wrapper call (`export const GET = withAuth(...)`), a handler without syntax | the path section only; every other section `not_read` with the reason | |
| SH-N27 | server action arguments | a `FormData` (or untyped) parameter: as SH-N9; exactly one parameter with another type: a body variant of kind `intended` with the type of argument 1 (the arguments are sent as a list; further arguments are not checked), said in a note; a zod parse of that parameter: as SH-N12 | path and query `not_read` (an action has none of its own) | several typed parameters: a note; rest and destructured parameters |
| SH-N28 | server action results | a returned object literal; a returned call of a proven function with a declared return type; the action's declared return type (`Promise<T>` read as `T`) | a response with status `default` and no content type (the result is not an HTTP response of its own): kind `read_in_handler` with the literal's fields, or `returned` | other returned values, `redirect()` and `notFound()` in an action: a note |
| SH-N29 | a server action that is not a proven function | an action whose `handler` is not a function with read syntax (a `const` bound to a call) | every section `not_read` with the reason | |
| SH-N30 | a next.config headers rule that sets no content header | a `headers` rule whose `headers` list (an array literal, a top-level `const` grown only with `push()`, spreads and `concat()` of them) has only object literals whose `key` is a string literal, or a top-level `const` bound to one, that is an HTTP token, none equal ignoring case to a content header: `Content-Type` (Next.js 15.5.4 and 16.2.10 send the rule's value instead of the handler's, 14.1.0 both), `Content-Length` (the body is cut to that length), `Content-Encoding` or `Transfer-Encoding` (they change how the body is read) | nothing: the rule is not one that may apply (SH-N24); it only adds response headers, so the route's statuses, content types and bodies and the request the handler reads stay as the other rows claim them | a list or a `key` not read, a key with a `:` parameter (Next.js substitutes source parameters into keys), a member that may replace the list after it (a spread or computed key), a key equal to a content header in any case: the rule may apply (SH-N24) |
| D | anything no row covers | other `NextResponse` methods (`next`, `rewrite`), `Response.error()`, Joi and other validators, `forbidden()` and `unauthorized()` (experimental), route segment config other than a static route config (`dynamic` `'auto'` or `'force-dynamic'`, `revalidate`, `runtime`, `dynamicParams`: the handler sees the request as sent), streamed responses | not stored: the section stays `not_read` or `partial` with a reason | |

## Hazards

- H12 (a field read in one branch is not required): names read are never
  required (SH-N3, SH-N5..SH-N9).
- H13 (the whole body merged into state): SH-N4, SH-N11.
- H14 (a project helper validates the query or the body depending on
  the method): a schema passed as a parameter is not resolved, so such a
  helper claims nothing; the request passed to it is followed for names
  read (SH-N25).
- H15 (`params: Promise<{ id: string }>` names, not types): SH-N1 never
  reads the annotation; path parameters are strings from the template.
- Thrown validation errors answer 500, not 400: a `fact` schema says the
  handler refuses what the schema refuses, not which status it uses.
- A `try` block around `redirect()` or `notFound()` may swallow the error
  Next.js uses to answer: SH-N22.
- Middleware may answer, rewrite, redirect or add response headers for
  any route its matcher may match: SH-N24. So may next.config
  `redirects`, `rewrites` and `headers` rules (SH-N24); a headers rule
  that provably sets no content header (Content-Type, Content-Length,
  Content-Encoding, Transfer-Encoding) does not (SH-N30).
- `export const dynamic = 'force-static'` empties the request every
  handler of the file sees: SH-N24.
- A relative redirect URL throws (500), `Response.json(undefined)` throws
  (500), and `notFound()` below a `try` anywhere on the call chain may be
  caught: SH-N19, SH-N17, SH-N20.

## Oracle

`scripts/oracle/nextjs_shape` (opt-in, never run by CI; its README says
how) builds the fixture's `web` application with pinned Next.js releases
(`next build`, then `next start` on 127.0.0.1) and drives every claimed
status and content type, the requests a `fact` body schema must refuse
or accept, and the compiler and zod checks of the claimed types. The
results are in the T029 SH-N task record (internal).

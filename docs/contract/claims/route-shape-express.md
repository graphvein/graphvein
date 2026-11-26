# Claim contract: Express and Next.js Pages Router API route shape

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-E1`..`SH-E36` and
the default row `D` say which code forms the Express and Next.js Pages
Router API shape extractor reads and what it may claim from each. Every
claim is stored with the model of the [route shape claim
contract](route-shape.md) (rows SM-1..SM-29), which it never changes. The
Go table `routes.ExpressShapeRows`
(`internal/extract/routes/shape_contract.go`) lists the same rows in the
same order with the same titles, and `TestExpressShapeContract` keeps the
two equal and checks that the fixture `testdata/fixtures/express-shape`
reaches every row. How the extractor works is described in
[route shape](../../extract/route-shape.md#express-and-nextjs-pages-router-api).

## Vocabulary

- **Handler**: the route's last registration argument (Express) or the
  file's default export (Pages Router API), when it is a proven function:
  an inline function, or the function node the route links to with
  `exposes_route`.
- **Request object** (`req`, the handler's first parameter), **response
  object** (`res`, its second) and **next** (an Express handler's third).
- **Read in code**: a literal name read as `req.query.x`, `req.query['x']`,
  through a const alias (`const q = req.query`) or by destructuring
  (`const { x } = req.query`, a destructured parameter), in the handler or
  a function it passes the object to (rows SM-12, SM-13: kind
  `read_in_handler`, presence `unknown`, no type).
- **Proven call target** (row SH-E35): a function declared once in the
  file, a const arrow function or function expression, an imported
  function resolved by the module resolver, or a method of a local object
  literal const. Never a method of an instance, `this`, a parameter, a
  local binding, a library export or a name declared twice.
- **Top-level call**: a call that is the whole expression of an
  expression statement, or the whole initializer of a declarator, of a
  statement directly in the function body (an arrow function's expression
  body counts), under parentheses and `await` only. A call inside an
  `if`, a loop, a `switch`, a `try` block, a conditional or logical
  expression or a callback, or with `.then`/`.catch` on it, is not one.
- **Tested result**: a `const` declared by a top-level statement whose
  initializer is a top-level call, followed by an `if` statement directly
  in the same body whose condition is the failure test (`r.error`, a
  destructured `error`, `!r.success`, a destructured `!success`), alone or
  as an operand of `||` (an `&&` with another test, or the success test
  without `!`, is no test), and whose consequence always ends in `return`
  or `throw` (a `return`/`throw` statement, or a block whose last statement
  is one). Bindings are matched by name only when the name is declared
  once in the function. Joi results are tested by `error` only.
- **May have sent**: a method call on, a pass of, or an assignment to a
  parameter other than the one the request value is followed through
  (the response, `next`, ...), except inside a branch of an `if`
  statement directly in the body that always ends in `return` or `throw`
  and whose every use of those parameters is a send of a literal 4xx or
  5xx status or `next(err)` (code after that `if` runs only when the
  branch did not, and the branch lets no request succeed; a branch that
  may answer 1xx..3xx, or with a status not read, counts).
- **Enforced**: a validation failure stops the request before anything is
  sent. A throwing form (`parse`, `parseAsync`, `validateAsync`) is
  enforced when it is a top-level call, awaited when it returns a promise
  (`parseAsync`, `validateAsync`), with nothing that may have sent before
  it in its function, and every call of the chain that reached it is the
  same: a top-level call, awaited when the callee is `async` or declares a
  `Promise` return type, with nothing that may have sent before it. A
  tested result (`safeParse`, `safeParseAsync` awaited, `validate`) is
  enforced only in the handler itself, with nothing that may have sent
  before the call or between the call and its test (a helper's tested
  result says nothing about whether its caller stops).
- **Harmless middleware**: a known library (a body parser, cookie-parser,
  the OpenAPI validator, celebrate, an express-validator export, morgan,
  compression, cors, helmet), or a proven function (inline, resolved, or
  the one function a proven factory returns, `isAuthenticated()`) whose
  response parameter is not used, or used only by `res.locals` and by
  sends after which no `next` call can follow on the same path (the `if`
  branch of the send always exits, or no later statement or event of the
  same branch calls `next`).
- **Unread middleware**: a mounted middleware or route argument before the
  handler that is not harmless; a mounted middleware that is not proven
  (`mounted_middleware_unknown`), except one only conditionally
  registered, declared on some mount chains or of unproven order that is
  harmless (it changes nothing whether or not it runs); or route
  middleware chained with `route(p).all`. It may set a status or the
  Content-Type, or replace `res.json` (SH-E25).

## Claim contract

| Row | Title | Forms read | Claim | Not claimed |
|---|---|---|---|---|
| SH-E1 | Express path parameters from the route template | every `{name}` and `{*name}` of the route's composed paths (`paths` attribute, T028) | `path` item: declared, kind `fact`, required, type string; `{*name}` an array of strings under Express 5; section `complete` with the template as evidence when every path has the same names and no optional segment | a name in some paths only, an optional segment, an unnamed wildcard (`{*}`): `partial`; a `{*name}` under Express 4 or an unknown major has no type |
| SH-E2 | a template without parameters | composed paths without `{` | `path` `provably_none`, evidence the template (kind `fact`) | no composed path: `not_read` |
| SH-E3 | Pages Router API path parameters from the file name | `[name]` and `[...name]` segments (route template `{name}`, `{*name}`) | as SH-E1; a catch-all is an array of strings | an optional catch-all `[[...name]]`: `partial`, no item |
| SH-E4 | query names read | `req.query.x`, `req.query['x']`, aliases, destructuring, `req.query.x.f()` | `query` item `read_by_handler`, kind `read_in_handler` | a computed key (SH-E6) |
| SH-E5 | Pages: a query name that is a dynamic segment | `req.query.id` where `id` is a path parameter of the Pages route | nothing in `query` (Next.js puts path parameters in `req.query`; the path item stands) | |
| SH-E6 | a request object read as a whole | `req.query[k]`, `{...req.body}`, `Object.keys(req.query)`, `req.query` passed to an unread function, a destructuring rest element; `req.body = ...` (the request object or a parameter holding it reassigned), `req.body.x = ...` | a note in the section's reason (`partial` or `not_read`); a section whose object is reassigned keeps no name read, a field written keeps no read of that field | no item |
| SH-E7 | header names read | `req.get('X')`, `req.header('X')` (Express), `req.headers['x']`, `req.headers.x`, `req.headers.get('x')` | `header` item, kind `read_in_handler`; names compare case-insensitively | a name not a string literal: reason only; a `req.headers` key with an upper-case letter: a note (Node lowercases the keys of `req.headers`, so the read never matches), no item |
| SH-E8 | cookie names read | `req.cookies.x`, `req.signedCookies.x` when `cookie-parser` is mounted before the route (proven `mounted_middleware` or a route argument); Pages Router API always (Next.js parses cookies) | `cookie` item, kind `read_in_handler` | |
| SH-E9 | cookies without a proven cookie parser | cookie reads on an Express route without SH-E8's parser | nothing; `cookie` stays `not_read` with the reason | |
| SH-E10 | body fields read | `req.body.x`, aliases, destructuring | one body variant of kind `read_in_handler`, content type null, an object whose fields have presence `unknown` and type `unknown` | |
| SH-E11 | a body passed on changed | `f({...req.body, x})`, `f(req.body.article)` to a proven function | chain stop `transformed` with the site; the section is `partial` | the callee's reads |
| SH-E12 | zod parse | `S.parse(req.body)`, `S.parseAsync(req.query)` with `S` a zod schema resolved in the repository (SH-E20), enforced | body: a variant of kind `fact` with the schema; query: one declared `fact` item per field with its presence and type | `S` not resolved: an ordinary call (SH-E35); not enforced (not a top-level call, `parseAsync` not awaited, after a call that may have sent, reached through such a call or an `async` callee not awaited): SH-E19 |
| SH-E13 | zod safeParse with the result tested | `S.safeParse(req.body)`, `S.safeParseAsync(...)` (awaited) whose result is tested in the handler, nothing that may have sent before the test | as SH-E12 | result not tested, tested in a called function, or a call that may have sent before the test: SH-E19 |
| SH-E14 | Joi validate with the error tested, validateAsync | `S.validate(req.body)` with a tested result, `S.validateAsync(req.query)` enforced as SH-E12 says, `S` a Joi schema (SH-E21) | as SH-E12; with a second (options) argument every presence and `additional` become `unknown`; a body schema whose root is not `required()` gives its top-level fields presence `unknown` with a note (Joi accepts `undefined`, which `req.body` is without a body), except on an Express 4 route with a proven `express.json`/`bodyParser.json` before it (it sets `req.body` to `{}`); on an Express route with no proven body parser such a schema is not enforced (SH-E19) | `Joi.assert`, `Joi.attempt`: not read; the options are not read, so convert is never proven off by them |
| SH-E15 | celebrate on the route | `celebrate({ body: S, query: S })` (also `[Segments.BODY]`, `[Segments.QUERY]`) as a route argument before the handler | as SH-E12 with `additional` unknown (celebrate's options are not read); with a second (Joi options) argument every presence becomes `unknown` too; the body root rule of SH-E14 applies | `params`, `headers`, `cookies` segments |
| SH-E16 | express-validator chains on the route | `body('x')...`, `query('x')...`, `header('x')...` as route arguments | the top-level name read (kind `read_in_handler`), never typed or required (a chain enforces nothing without `validationResult`) | `check()`, `param()`, `cookie()`, wildcards |
| SH-E17 | Request and RequestHandler type arguments | `req: Request<P, ResBody, ReqBody, Query>` on the handler, `const h: RequestHandler<P, ResBody, ReqBody, Query>`, both imported from `express` | `ReqBody`: a body variant of kind `intended`; `Query`: query items of kind `intended` (presence `optional` for `?`, else `unknown`) | `P` (path parameters are strings), `ResBody`; `any`, `unknown`, `{}`, generics |
| SH-E18 | type assertions and typed callee parameters | `req.body as T`, `const b: T = req.body`, `f(req.body)` with `f(input: T)` proven | a body variant (or query items) of kind `intended` | as SH-E17 |
| SH-E19 | a validation that is not tested or not enforced | SH-E13/SH-E14 forms whose result is not a tested result; SH-E12..SH-E15 forms that are not enforced | untested: nothing; not enforced: the schema as an item of kind `intended` (required presence becomes `unknown`); a note in the reason | |
| SH-E20 | zod schema forms | `z.string()`, `z.number()`, `z.int()`, `z.boolean()`, `z.null()`, `z.literal(v)`, `z.array(S)`, `S.array()`, `z.object({...})`, `z.strictObject`, `z.looseObject`; modifiers `optional`, `default`, `nullable`, `nullish`, `int`, `strict`, `passthrough`, `strip`, `loose`, `extend({...})`, `partial()` and the checks that keep the type (`min`, `max`, `email`, `uuid`, `regex`, `refine`, ...) | the schema; a field without `optional`/`default`/`nullish` is required; `z.object` allows other keys (zod strips them) | every other factory or modifier (`enum`, `union`, `transform`, `coerce`, `pick`, ...): type `unknown` with a reason, presence `unknown` |
| SH-E21 | Joi schema forms | `Joi.string()`, `number()`, `boolean()`, `array().items(S)`, `object({...})`, `object().keys({...})`; modifiers `required`, `exist`, `optional`, `forbidden`, `strip`, `default`, `strict`, `integer`, `valid(...)`, `insensitive`, `allow(null)`, `empty(null)`, `allow('')`, `empty('')`, `unknown()` and the checks that keep the type (`min`, `max`, `email`, `description`, ...) | the schema; presence: the last of `required`/`exist` (required) and `optional` in the chain, else optional (`default` keeps it, `forbidden`/`strip` make it unknown); an object forbids other keys unless `unknown()`; `number()` and `boolean()` are typed only with convert off, by `strict()` on the schema or an ancestor (Joi converts `"5"` and `"true"` by default), else type `unknown` with the reason; `empty(null)`: not nullable on a required key, nullable on an optional one, type `unknown` when presence is not known (options passed and no explicit presence) | as SH-E20; `valid(...)` with `insensitive()`: type `unknown`; `empty(...)` of another value: type `unknown` |
| SH-E22 | TypeScript types as models | interfaces and type aliases declared at the top level of a repository file (followed through imports): properties of type string, number, boolean, null, string/number/boolean literals, arrays, object literals, unions of a type with null/undefined or of literals, other such types | a model of the item's kind (`intended` or `returned`) referred to by `ref`; a `?` property, or one whose type is a union with `undefined`, is optional, others `unknown`; only `null` makes a type nullable | generic, extended or open types, `Date`, other unions: type `unknown` with a reason |
| SH-E23 | a send with a literal status | `res.status(n).json(v)`, `.send(v)`, `.end()`, `.jsonp(v)`, `.render(...)`, with chainable `set`, `header`, `type`, `cookie`, ... between; `n` from 200 to 599 | a response `n` of kind `read_in_handler`; `json` claims `application/json` except for 204 and 304, and on Express only when no `type`, `contentType` or `set`/`header`/`setHeader`/`append` of Content-Type (or of a name not read) occurs in the chains read and no unread middleware runs before the handler (Express's `json` keeps a Content-Type already set; Next.js's sets its own); calls after the sending method are ignored, as Express ignores them | 1xx: SH-E31; otherwise the content type is null with a note |
| SH-E24 | sendStatus | `res.sendStatus(n)` | a response `n`, body not read (empty for 204 and 304) | |
| SH-E25 | a send without a status | `res.json(v)`, `res.send(v)`, `res.end()` | a response `200` only when no `res.status(n)` alone (before or after the send), no `res.statusCode =` and no unproven call of the response exists in the chains read, and no unread middleware runs before the handler | otherwise a note in the reason; with unread middleware no response of the route claims a body or a content type either |
| SH-E26 | redirect | `res.redirect(url)`, `res.redirect(n, url)` | `302` (Express) or `307` (Pages Router API), or `n` | |
| SH-E27 | response bodies written in the call | `json({...})`, `send({...})`: the literal's keys; `send()`, `end()` without an argument, status 204 or 304: an empty body | an object whose fields have presence and type `unknown` (nested literals read the same way); empty: schema `null` | a literal that defines `toJSON`: type `unknown`; other values: schema null (not read) |
| SH-E28 | the declared return type of a proven callee | `const v = await f(...); res.json(v)` with `f` a proven function with a declared return type (`Promise<T>` read as `T`) | a second response item at the same status of kind `returned` with the type's schema (SH-E22) | `any`, `unknown`, an unresolved type |
| SH-E29 | thrown errors and next | `throw`, `next(err)`, `next()` in a function read for the response | a note: the status comes from error handling (not read) | |
| SH-E30 | the response passed to a proven function | `helper(res)` | followed (SH-E35); its sends count as the route's | |
| SH-E31 | a status not read | `res.status(code)` with a non-literal, a 1xx or out-of-range code | no item; a note in the reason | |
| SH-E32 | a mounted OpenAPI validator | `express-openapi-validator`'s middleware mounted before the route (H9) | a note on the request sections that the committed spec decides the request shape; nothing is taken from it here (SH-O) | |
| SH-E33 | body parsers | `express.json()`, `express.urlencoded()`, `bodyParser.*` mounted before the route or as a route argument | a note on `body`; the content type is never claimed (null) | |
| SH-E34 | Pages Router API routes serve every method | `req.method` branches of a Pages Router API handler | one shape for the route (method `ANY`): the reads of every branch together, with a note; Next.js parses the body unless the file's `config.api.bodyParser` says otherwise (noted) | per-method shapes |
| SH-E35 | call following through proven targets | the request, one of its objects (`req.body`, `req.query`, ...), the response or `next` passed as an argument | the callee is read with the same rows (`routeshape.Follow`, depth limit `index.shape_depth`); the chains are stored | unproven targets stop with `unresolved`, `ambiguous` or `external` |
| SH-E36 | a handler that is not a proven function | a wrapper call (`wrap(h)`), an unresolved name | the path section only; every other section `not_read` with the reason | |
| D | anything no row covers | other response methods (`download`, `sendFile`, `format`, ...), `req.param()`, registration type arguments | not stored: the section stays `not_read` or `partial` with a reason | |

## Hazards

- H9 (a global spec validator and body parser decide the request shape):
  SH-E32 and SH-E33 note them; nothing is claimed from the spec here.
- H10 (TypeScript generics are not runtime checks; Joi and zod are): the
  two evidence levels are separate items, `intended` (SH-E17, SH-E18) and
  `fact` (SH-E12..SH-E15), never merged (SM-15).
- H11 (statuses from thrown errors and error middleware): SH-E29 keeps the
  responses section `partial` with the reason.
- H12 (a field read in one branch is not required): names read are never
  required (SH-E4, SH-E10).
- H13 (the whole body merged into other state): SH-E6, SH-E11.
- Express 5 makes `req.query` a getter (assignments to it do not change
  it) and parses query strings with the simple parser (Express 4: `qs`,
  nested keys): names read are top-level keys in both, and no type is
  claimed for them.
- H15 (`{id: string}` type arguments are names, not types): SH-E17 does
  not read `P`; path parameters are strings from the template.

## Oracles

`scripts/oracle/express_shape` (opt-in, never run by CI; its README says
how) runs on the fixture only, with pinned packages: zod 4
`z.toJSONSchema(S, { io: "input" })` and Joi `describe()` for the fixture
schemas against the stored fields, types and presence; the TypeScript
compiler for every type the store labels `intended` or `returned`; and
the fixture served on 127.0.0.1 by Express 4.21.2, Express 5.1.0 and
Next.js 15.5.4's `apiResolver`. The served apps answer a known-valid
request per route and, for every `fact` schema, requests that must be
refused (each required field missing, each typed field of the wrong kind
or a value Joi or zod would convert, `null` for a field that is not
nullable, an extra key where extra keys are forbidden, no body where a
top-level field is required) and requests that must be accepted (an
optional field missing, `null` for a nullable field); every claimed
status must be observed and every claimed content type must match. The
results are in the T029 SH-E task record (internal).

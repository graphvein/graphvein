# Claim contract: shape differences of a link

ADR 0081 (**Accepted**), section "Shape differences on links". Rows
`SD-1`..`SD-12` and the default row `D`. The Go table `shapediff.Rows`
(`internal/shapediff/contract.go`) lists the same rows in the same order
with the same forms, and `TestContractTable` keeps the two 1:1: every form
the code reads or serves is the form of exactly one row, and every row's
forms are written in the table below. `TestShapeDifferences`
(`internal/server`) indexes the fixtures `testdata/fixtures/shape-differences`
and `testdata/fixtures/shape-differences-project` and checks that every
row is reached.

A link's details (the API section's Flow tab line detail, and the
Explorer operation's links area) say how what a linked frontend call
sends differs from what the route reads, only where both sides are fully
read. Anywhere else they say nothing: never "no differences", never a
guess.

## Vocabulary

- **Sends**: the client_call node attribute `sends` (model version 1):
  for the body and the query string, the keys a call provably sends, how
  they are known (`literal` or `typed`), where (file and line), and
  whether every key is known (`complete`).
- **Fully read** (route side): the route's section (rows SM-2..SM-5 of
  the [route shape claims](route-shape.md)) is `complete`, carries no
  reason (no unread middleware or validator caveat), no chain followed
  for it stopped at the depth limit, and every item compared is a `fact`
  (an enforced declaration; a `mapped` item describes a spec, not the
  handler).
- **Difference**: a key one side has and the other does not, with both
  sides' provenance.

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SD-1 | attribute and version | `attribute:sends`, `version:1` | The attribute is stored only on a client_call whose `wrapper` is null and whose status is not `wrapper`, for a part a row SD-2..SD-5 proves. A reader refuses another version, an unknown field, a repeated key, a part without provenance, or a typed part marked complete, and then compares nothing. |
| SD-2 | fetch with a literal JSON body | `call:fetch_json_body` | `fetch(url, init)` (library `fetch`, method neither GET nor HEAD) whose `init` is an object literal without a spread, a computed key or a method, with `headers` an object literal of the same kind holding exactly one `Content-Type` key (any case) whose literal media type is `application/json`, and `body: JSON.stringify(<object literal>)` with the global `JSON` and one argument. The body's own keys are sent, except a key whose value is written `undefined` (JSON leaves it out); an object literal under a key is read the same way one level down. A spread, a computed key, a method or an accessor in a body object makes that object not complete (its literal keys are still sent). Without the header (or with `headers` not written in the init object) fetch sends `text/plain`: D. |
| SD-3 | fetch with literal URLSearchParams | `call:fetch_search_params` | A fetch whose URL is a template ending in `new URLSearchParams(<object literal>)` (or its `.toString()`), with the global `URLSearchParams`, right after the template's only `?` (and no `#`). Every own key is sent as a query parameter (an `undefined` value is sent as the string "undefined"). Completeness as in SD-2. |
| SD-4 | hey-api body type | `call:heyapi_body` | A proven hey-api request (library `hey-api`) inside a generated operation whose request object has only own keys and spreads of one parameter `options` of type `Options<TData, ...>`, where `Options` is the generator's own generic type and `TData` a repository object type with a required `body` property whose type is a repository object type (named, or written inline). The body is JSON (axios's default for an object). The required properties of the body type are sent, with the object types under them; a typed part is never complete (a value of a TypeScript type may carry more keys), so it never gives SD-9. Own `headers` keep the body JSON only in the generator's own form for a JSON operation: an object literal with exactly one `Content-Type` key whose literal media type is `application/json`, and spreads of `options.headers`. An own `body`, other `headers` or a `bodySerializer`, or another spread (a form serializer), is D for the body. |
| SD-5 | hey-api query type | `call:heyapi_query` | As SD-4 for a required `query` property of `TData` (an own `query` or `querySerializer` is D for the query). |
| SD-6 | route body fully read | `route:body` | The route's body section is fully read and has exactly one item of the call's media type (`application/json`, parameters ignored), of kind `fact`, whose schema is an object, or a reference to a `fact` model that is one, whose `additional` is not `allowed` (an object that keeps unlisted keys may hand them to the handler). Nested objects are compared under the same conditions. |
| SD-7 | route query fully read | `route:query` | The route's query section is fully read with every item a declared `fact`, or `provably_none` with its evidence. |
| SD-8 | sent by the frontend, not read by the handler | `difference:sent_not_read` | A key the call sends (SD-2..SD-5) that the route's compared object (SD-6) or query parameters (SD-7) do not list. Not reported for an object (or query section) with a field or parameter of presence `unknown`: its accepted names are not exactly the listed ones (a serde `alias` is accepted, also with `deny_unknown_fields`). Call side: the key's file and line; route side: the object's (or the query section's) provenance. |
| SD-9 | read by the handler, not sent by this call | `difference:read_not_sent` | A key the route declares `required` as a `fact` that a complete literal part (SD-2, SD-3) does not send. A key whose schema accepts null counts only under a rule that proves the requirement for it (SH-F22, which claims it only when Pydantic 2 is proven). Call side: the object's file and line; route side: the object's (or the parameter's) provenance. |
| SD-10 | compared, no difference | `result:no_difference` | Both sides fully read and agreeing: the comparison is served with an empty `differences` list, and the link details show nothing special. |
| SD-11 | cross-repository links | `link:cross_repository` | In project mode, a calls_route link from another member is compared by the same rows; its comparison carries `cross_repository: true` and the link details label it as a cross-repository link. |
| SD-12 | where differences are shown | `field:shape_differences` | `shape_differences` (additive) on each HTTP caller of `GET /api/v0/api/endpoint`, on each project caller of its `project` part, and on each calls_route edge of `GET /api/v0/api/flow`, with `call_links` and `route_links` (the editor link of each position, built as `shape_links` are). Absent when nothing is compared. The Web UI shows only a comparison with differences, in the words of SD-8 and SD-9, each side with its provenance chip; never "unused", "broken" or "error". |
| D | anything no row covers | | Nothing is recorded or compared, and the link details show nothing: a dynamic or spread body, headers not written at the call, a wrapper, axios, Angular, SWR and Orval calls, a typed client whose type is not read, a route section that is partial or not read, a depth cap, a mapped or intended item, a form body. |

## Known gaps

- A literal key whose value is `undefined` only at run time is still
  counted as sent (JSON.stringify leaves it out).
- hey-api: a caller may pass its own `headers` through `options` and so
  change the body's media type; the generated default (JSON) is assumed.
- zod `z.object` strips unknown keys but is stored as `additional:
  allowed` (it accepts them), so such routes are not compared.
- Express and Next.js bodies are never `complete` (the content type is
  not read), so their links show nothing.

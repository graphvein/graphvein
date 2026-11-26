# Claim contract: route shape from a committed spec

ADR 0081 (**Accepted**), section "Shape model". Rows `SH-O1`..`SH-O19` and
the default row `D`. They say what the committed OpenAPI or Swagger
operation linked to a route adds to the route's shape, in the shared
model of the [route shape claim contract](route-shape.md) (rows `SM-*`).
The Go table `specShapeRows` (`internal/extract/openapi/shape_contract.go`)
lists the same rows in the same order with the same forms, and
`TestSpecShapeContractTable` keeps the two 1:1; `TestSpecShapeGolden`
checks that the fixture `testdata/fixtures/route-shape-spec` (a FastAPI
app documented by an OpenAPI 3.0, an OpenAPI 3.1 and a Swagger 2.0 spec)
reaches every form.

A spec form no row lists goes to `D`: it is not stored.

## How the shape is read

The [OpenAPI import](../../extract/openapi.md) links each committed
operation to the routes it documents with an inferred `documents_route`
edge (exact matches only). After every framework has stored its own
shape, each route linked by exactly one such edge gets the operation's
parameters, request body, responses and security as items of kind
`mapped` (SM-10), with the edge's confidence (0.9, or 0.7 when the spec's
server names an origin) and the spec file and line as their source. The
operation's references to parameters, request bodies and responses are
resolved as the import resolves them; schema references are resolved
within the spec document only.

Precision rules that hold for every row:

- Nothing from a spec is a `fact`: a committed spec says what its author
  documented, not what the code enforces. Mapped items are written beside
  the code's items, never instead of them, and never change a state the
  code proved.
- A contradiction between a code fact and the spec is written as a
  "spec differs" note in the section's reason (SH-O18), never resolved
  silently in either direction.
- Every schema keyword the shape subset cannot express makes that schema
  `unknown` with the reason; a keyword that only narrows a value
  (`format`, `minimum`, `pattern`, ...) is not stored (`D`).
- The JSON pointer of a schema is kept in its model's name
  (`<file>#<pointer>`); the model provenance is mapped with the same
  confidence.

## Known gaps

- One view of a schema for requests and responses. A model is shared by
  the request body and the responses that reference it, so a field the
  spec marks `readOnly` or `writeOnly` gets presence `unknown` in both
  directions, also where the spec's `required` or optional would hold
  (SH-O12). A request and a response view of models would be a change of
  the shared shape model.
- Numbers are stored as float64 by the shared model; an enum literal that
  would change is refused rather than stored exactly (SH-O11).

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SH-O1 | linked route | `link:documents_route` | A route that is the target of exactly one `documents_route` edge takes the mapped items of that edge's operation, each with provenance kind `mapped`, rule the row of its form, the file and line of its declaration in the spec (after resolving a referenced parameter, body or response), an empty chain and the edge's confidence (SM-11). A route without a stored shape starts from every section `not_read` with the run's depth limit. |
| SH-O2 | several documenting operations | `link:several` | A route documented by more than one operation (two specs, or two operations of one spec) takes none of them: which one describes it is not proven. The `documents_route` edges still name them all. |
| SH-O3 | section states | `state:spec_only`, `state:code_kept`, `state:provably_none_kept` | A section the code did not read (`not_read`) that receives mapped items becomes `partial` with the reason "not read in the code: every item is mapped from the committed spec" (the spec says what it documents; the code may read more). A section the code read (`partial` or `complete`) keeps its state and reason and takes the mapped items beside its own. A `provably_none` section keeps its state and takes no items (SM-3); a spec that declares some is a "spec differs" note (SH-O18). A spec that declares nothing for a section adds nothing: a spec's silence is never `provably_none`. |
| SH-O4 | parameters | `param:path`, `param:query`, `param:header`, `param:cookie`, `param:override`, `param:content` | The path item's and the operation's parameters merged (an operation parameter replaces the path item's of the same name and location; header names compare case-insensitively): `declared`, presence `required` when the spec says `required: true` and for every path parameter (OpenAPI requires them, SM-16), otherwise `optional`; the schema of `schema` (SH-O9..SH-O15). A parameter serialized by `content` has an `unknown` schema. |
| SH-O5 | ignored header parameters | `param:reserved-header` | OpenAPI 3 ignores a header parameter named `Accept`, `Content-Type` or `Authorization`: not stored. |
| SH-O6 | Swagger 2.0 parameters | `swagger:parameter`, `swagger:body`, `swagger:formData`, `swagger:file` | A path, query or header parameter takes its schema from its own `type`, `items` and `enum`. The `body` parameter is the request body: one item per effective `consumes` content type (null when none), its required noted as in SH-O7. The `formData` parameters are one body object of their fields (presence from `required`, additional `unknown`) for each effective `multipart/form-data` or `application/x-www-form-urlencoded` content type (null when none). `type: file` is `unknown`. |
| SH-O7 | request body | `body:content`, `body:required`, `body:no-schema` | One body item per media type of `requestBody.content`, with its schema; a media type without a schema has an `unknown` schema. The model has no body presence: the section's reason says "the committed spec requires a request body" or "marks the request body optional". |
| SH-O8 | responses | `response:status`, `response:range`, `response:default`, `response:content`, `response:no-content`, `response:swagger`, `response:swagger-range` | One item per documented status (a code `100`..`599`, a range written in upper case (OpenAPI 3 only), or `default`; other keys are not stored) and media type, with its schema. A response without content (3.x) or without a schema (Swagger) has no content type and the empty body (schema `null`), as the spec says. A Swagger schema is stored once per effective `produces` content type (none: content type null). A Swagger 2.0 key such as `2XX` is not a 2.0 status (ranges are OpenAPI 3): it is not stored, and the section's reason names it. OpenAPI does not require a spec to list every status: the section is never `complete` from a spec alone. |
| SH-O9 | schema types | `schema:string`, `schema:number`, `schema:integer`, `schema:boolean`, `schema:null`, `schema:object`, `schema:array`, `schema:no-type` | `type` gives the schema type (`null` only from OpenAPI 3.1). A schema without `type`, `enum` or `const`, or with an unknown type name, is `unknown`. An array without `items` has `unknown` items. |
| SH-O10 | null | `schema:nullable-3.0`, `schema:type-list-3.1`, `schema:null-member` | OpenAPI 3.0 `nullable: true` beside a `type` adds null; OpenAPI 3.1 ignores `nullable`. A 3.1 type list of one type and `null` is that type, nullable; of several types `unknown`; a type list before 3.1 is `unknown`. A 3.1 `anyOf` of exactly one schema and `{type: null}` is that schema, nullable; so is a `oneOf` of them when the other schema does not accept null itself (a `oneOf` rejects a value both members accept); when it does, or when that cannot be told from the spec, the `oneOf` is `unknown` (SH-O15). |
| SH-O11 | enum and const | `schema:enum`, `schema:const`, `schema:inexact-number` | `enum` is an enum of its literals (strings, numbers, booleans, null) as written, whatever `type` or `nullable` say; 3.1 `const` is an enum of one. A number the shape cannot store as the same value (the model stores JSON numbers as float64: an integer beyond 2^53, a decimal with more digits than float64 holds) makes the enum `unknown`: a rounded literal would be another value. |
| SH-O12 | object fields | `schema:required`, `schema:additionalProperties`, `schema:readOnly-writeOnly` | `properties` are the fields; a field named in `required` is `required`, others `optional`; a required name without a property is a required field of `unknown` schema. A field marked `readOnly` or `writeOnly`, required or not, on the property or on any schema its type is read through (a local `$ref` chain, a single `allOf` member, a 3.1 `oneOf`/`anyOf` with `null`), has presence `unknown`, and the reason of each section that reaches it says so: such a field is sent in responses only (readOnly) or requests only (writeOnly), and the shape has one view of a schema for both directions (see Known gaps). `additionalProperties: false` is additional `forbidden`; absent, `true` or a schema is `allowed`; `patternProperties`, `unevaluatedProperties` or `propertyNames` make it `unknown`. |
| SH-O13 | references | `ref:local`, `ref:external`, `ref:missing`, `ref:siblings`, `ref:cycle` | A `$ref` within the same document is a reference to a model named `<file>#<pointer>`, built once (so a cycle through an object or array ends at the model). A cycle with no schema in between (`$ref` chains, single `allOf` members, 3.1 null members returning to their start) names no value: `unknown` ("reference cycle") with an `openapi_ref_unresolved` diagnostic. A reference to another document or URI is `unknown` (not resolved); one that names nothing is `unknown`. In OpenAPI 3.1, a `$ref` beside keywords other than annotations (description, title, examples, deprecated, readOnly, writeOnly, default) is `unknown`; before 3.1 those keywords are ignored, as the spec says. |
| SH-O14 | single allOf member | `schema:allOf-one` | An `allOf` of exactly one schema, beside annotations only, is that schema. |
| SH-O15 | schemas not modelled | `schema:composition`, `schema:conditional`, `schema:prefixItems`, `schema:boolean-schema`, `schema:depth`, `schema:not-literal` | `allOf`, `oneOf`, `anyOf` (other than SH-O10 and SH-O14), `not`, `discriminator`, `if`/`then`/`else`, `dependentSchemas`, `prefixItems`, 3.1 boolean schemas, an enum value that is not a literal, and nesting deeper than 32 levels (SM-20) are `unknown` with the reason. |
| SH-O16 | security requirements | `security:operation`, `security:root`, `security:anonymous` | The operation's `security`, otherwise the root's: one requirement per entry, its schemes with their scopes. An empty `security: []` or an empty entry `{}` is the anonymous alternative (kind `mapped`, SM-19). No `security` at either level adds nothing. |
| SH-O17 | security schemes | `scheme:http`, `scheme:apiKey`, `scheme:oauth2`, `scheme:openIdConnect`, `scheme:basic-2.0`, `scheme:other`, `scheme:undefined`, `scheme:renamed` | Each named scheme from `components.securitySchemes` (3.x, references resolved) or `securityDefinitions` (Swagger), with the fields of its type (SM-19): `scheme` and `bearerFormat`; `in` and `name`; the token URL of the password or client-credentials flow when those name one URL (Swagger: `password` or `application` flow); `openIdConnectUrl`. Swagger `basic` is HTTP basic. `mutualTLS` and other types, and a name the spec does not define, are schemes of type `unknown`. A mapped scheme whose name the route's code already uses is stored as `<name> (spec)`. |
| SH-O18 | spec differs | `differs:presence`, `differs:type`, `differs:names`, `differs:body`, `differs:status`, `differs:security` | Notes added to the reason of a section the code read, starting "spec differs:": a declared `fact` parameter whose presence (required or optional) or top-level type differs from the spec's parameter of the same name (integer and number, and enums, do not differ; a code `string` accepts any text, so it differs from no type); in a `complete` or `provably_none` query, header or cookie section proven by a `fact`, names the spec declares and the code does not, and the reverse (path parameters are matched by position by the route link, so their names are not compared); a body the spec declares where the code proves none, and in a `complete` body content types on one side only (media types compare by type and subtype, case-insensitively, without parameters; a media range such as `*/*` or `application/*` covers the types it names and is never a difference itself); a top-level body type that differs for one content type; a `fact` 2xx status the spec documents neither as a code, `2XX` nor `default`; a `fact` requirement that all alternatives need authentication where the spec requires none, and (complete sections) an anonymous code alternative where the spec requires authentication. |
| SH-O19 | generated document | | The generated document describes the code ([openapi-generation](openapi-generation.md), OG-60, OG-77): mapped items are kept in `x-graphvein.shape.not_standard` with their kind, a section whose items all come from the spec has state `not_read` there (the stored state is the code's state for every rule of the generator: OG-41's `not_read` list and the security fallback of OG-30/OG-31), and a mapped security alternative is never part of the written requirement. |
| D | anything no row covers | | Not stored: response headers, links, callbacks, examples, `format` and every constraint keyword, `x-` extensions, Swagger `x-nullable`, parameter `style`/`explode`, external documentation. |

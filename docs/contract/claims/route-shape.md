# Claim contract: route request and response shape

ADR 0081 (**Accepted**), section "Shape model". Rows `SM-1`..`SM-29` and the
default row `D`. The Go table `routeshape.Rows`
(`internal/extract/routeshape/contract.go`) lists the same rows in the same
order with the same forms, and `TestContractTable` keeps the two 1:1: every
constant of the model is the form of exactly one row, and every row's forms
are written in the table below. `TestViolations` breaks each checked
invariant once and expects its row.

These rows are the model every framework's shape extractor writes. They say
what a stored shape may claim and what it must leave unclaimed; which code
forms a framework reads is in that framework's own `SH-*` table. A form no
row covers goes to `D`: it is not stored.

## Vocabulary

- **Section**: one of `path`, `query`, `header`, `cookie` (parameters),
  `body` (request body), `responses`, `security`.
- **Item**: one parameter, body variant, response or security requirement.
  Every item carries one **provenance**: kind, rule, source file and line,
  the call chain followed to reach the site, and a confidence.
- **Schema**: a JSON-Schema-like subset (row SM-20).
- **Chain**: the calls followed from the handler to a site (rows
  SM-21..SM-29).

## Claim contract

| Row | Title | Forms | Rule |
|---|---|---|---|
| SM-1 | attribute and model version | `attribute:shape`, `version:1` | A route's shape is the route node attribute `shape`, model version 1 (contract v1 open attributes: no contract version change). A route without the attribute has every section `not_read`. A reader refuses another version, an unknown field or a broken invariant and shows the route as not read, never a partial guess. An unknown section state is refused here. |
| SM-2 | not_read | `state:not_read` | Graphvein did not read this part. No items, no evidence. Absence of items never means "none". |
| SM-3 | provably_none | `state:provably_none` | The code proves there is nothing here. No items; `evidence` of kind `fact` or `mapped` names the declaration that proves it. Absence of reads is never evidence (a route with no auth read is not public). |
| SM-4 | partial | `state:partial` | Some items were read and more may exist. `reason` says what may be missing (an unresolved call, a dynamic key, a depth cap); no evidence. |
| SM-5 | complete | `state:complete` | Every item is known. At least one item, every item of kind `fact` or `mapped`, and `evidence` of kind `fact` or `mapped`. |
| SM-6 | kind fact | `kind:fact` | A typed declaration the framework enforces at runtime (FastAPI annotation, a validator that runs, Axum `Json<T>`). |
| SM-7 | kind read_in_handler | `kind:read_in_handler` | A name read in the handler or a function it calls; type and presence not read (SM-12, SM-13). |
| SM-8 | kind intended | `kind:intended` | A static type nothing checks at runtime: a TypeScript generic or `as T`, a callee's parameter type. |
| SM-9 | kind returned | `kind:returned` | The declared type of a returned value, not validated by the framework. |
| SM-10 | kind mapped | `kind:mapped` | From a committed spec or a typed frontend client linked to the route; its confidence is the inferred link's. |
| SM-11 | provenance fields and confidence | | Every item, model, scheme and evidence has a known kind, a rule (the extractor's own row, e.g. `SH-F4`), a file and a line of at least 1, and chain steps with file, line and function. Confidence is 1 for every kind read from code (ARCHITECTURE: facts are 1); a `mapped` item takes the inferred link's confidence in (0, 1]. |
| SM-12 | names read in code are never required or typed | `access:read_by_handler` | A parameter read by name in the handler (or its callees) has kind `read_in_handler`, presence `unknown` and no schema. A body built from names read is an object whose fields all have presence `unknown` and type `unknown`. Never a declared parameter. |
| SM-13 | names read by a dependency are not declared parameters | `access:read_by_dependency` | A name read by a dependency or middleware (a FastAPI dependency reading `request.headers`) has access `read_by_dependency`, kind `read_in_handler`, presence `unknown`, no schema, and a non-empty chain to the dependency. The generator never writes it as an OpenAPI parameter. |
| SM-14 | required only from fact or mapped | `presence:required`, `presence:optional`, `presence:unknown` | Presence `required` appears only in items, fields and models of kind `fact` or `mapped`. `optional` means proven optional; `unknown` means not proven either way. |
| SM-15 | intended and returned types never become a proven schema | | A schema inside a `fact` or `mapped` item or model never refers to a model of kind `intended`, `returned` or `read_in_handler`. An intended type is its own item of kind `intended` beside the fact item, never merged into it. |
| SM-16 | parameters | `access:declared`, `section:path`, `section:query`, `section:header`, `section:cookie` | A parameter has a name, an access and a presence; a schema only when its type was read. Within a section a (name, kind) pair appears once; header names compare case-insensitively. A declared path parameter of kind `fact` or `mapped` is required. A name read in code is never `declared`. |
| SM-17 | request body | `section:body` | One item per (content type, kind). `content_type` null means not read; an empty string is refused. `schema` null means not read. |
| SM-18 | responses | `section:responses` | One item per (status, content type, kind). Status is a code `100`..`599`, a range `1XX`..`5XX` or `default`; a status that was not read is not an item (the section is `partial`). A proven empty body is the schema of type `null`; `schema` null means not read. |
| SM-19 | security requirements and schemes | `section:security`, `scheme:http`, `scheme:apiKey`, `scheme:oauth2`, `scheme:openIdConnect`, `scheme:unknown` | Items are alternatives; the schemes of one item apply together. Every scheme named exists in `security_schemes` (unique names). A scheme holds only the fields of its type (`scheme`, `bearer_format` for http; `in` header/query/cookie and `param_name` for apiKey; `token_url` for oauth2; `openid_connect_url` for openIdConnect); a detail not read is null, never empty. The anonymous alternative (no schemes) needs kind `fact` or `mapped`. |
| SM-20 | schema language | `type:object`, `type:array`, `type:string`, `type:number`, `type:integer`, `type:boolean`, `type:null`, `type:enum`, `type:ref`, `type:unknown`, `additional:allowed`, `additional:forbidden`, `additional:unknown` | A schema node has one type and only that type's fields: object (fields with unique names and a presence each, and `additional` allowed, forbidden or unknown), array (`items` required), enum (one or more literals: string, number, boolean, null), ref (names a model in `models`; model names unique), unknown (a non-empty `reason`; never omitted). `nullable` adds null. Nesting is bounded by 32 levels; deeper is cut to unknown by the extractor. |
| SM-21 | call following: proven targets only, configurable depth limit | `config:index.shape_depth`, `flag:--shape-depth` | Extractors follow the handler's values only through proven call targets (a unique stored definition, never a name guess), up to the run's depth limit: `index.shape_depth` in `graphvein.yaml` or `graphvein index --shape-depth`, default 6, allowed 1 to 10. Following runs at link time over cached syntax, so changing the limit needs a re-index, never a re-parse; an extractor that needs parse-time data caches enough for depth 10. The limit is stored in `follow.depth_limit`; an item's chain and a chain record's depth never exceed it, and a record's steps equal its depth. |
| SM-22 | stop ended | `stop:ended` | The value is not passed on from the last function read. No stop site. |
| SM-23 | stop depth_cap | `stop:depth_cap` | The next call is proven but would pass the depth limit: recorded at depth = limit with the call site, shown as "depth cap reached (N)", never as unknown or none. SH units report how many chains hit it per corpus. |
| SM-24 | stop cycle | `stop:cycle` | The next call returns to a function already on the chain with the same value. |
| SM-25 | stop joined | `stop:joined` | The call reaches a function and value already read through another chain; its reads are already recorded once. |
| SM-26 | stop budget and resolver_error | `stop:budget`, `stop:resolver_error` | `budget`: 256 functions were already read for this start (a call site is recorded). `resolver_error`: reading a function failed or the run was cancelled; recorded with the message, never fatal for other chains or routes. |
| SM-27 | stop at an unproven target | `stop:unresolved`, `stop:ambiguous`, `stop:external` | The call target is not proven: no unique definition, several candidates, or outside the repository. Recorded with the call site; the section is `partial`. |
| SM-28 | stop at a value that cannot be read further | `stop:transformed`, `stop:opaque_type` | The value is transformed before it is passed on (spread, merge, re-assignment), or its type is `any`, `unknown` or an uninstantiated generic. Recorded with the site; the section is `partial`. |
| SM-29 | chain records and depth statistics | | Every followed chain is stored in `follow.chains` with its section, start, steps, depth, stop, site and detail. `routeshape.Stats` builds the histogram of depths and stop reasons per corpus, so the default limit is set from measured data. |
| D | anything no row covers | | Not stored: the section stays `not_read`, or `partial` with a reason. |

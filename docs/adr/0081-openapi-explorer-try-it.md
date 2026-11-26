# ADR 0081 - OpenAPI generation, API explorer and "try it" proxy

**Status:** Accepted (delegated review, 2026-11-23)

Pending developer decisions are marked **pending** and listed in the
T029 design record (internal; G1-G10, a-g). This ADR records the
recommended decision; the design round may change it before
implementation.

## Context

Milestone M2 asks for OpenAPI generation, a Swagger-like interface and a
guarded "try it" proxy, accepted when "extracted OpenAPI for
full-stack-fastapi-template matches FastAPI's own spec on paths and
methods" and "a mutating try it call cannot be sent without confirmation".
VISION.md: "absence only where the API proves it".

The investigation inventory (the T029 task record, internal) measured what main
stores per route: method and composed path templates (with router
prefixes, trailing-slash flags, conditional and settings-default markers,
assumptions) for Express, FastAPI, Axum, net/http, chi, gin and Next.js;
path parameter names from the templates; FastAPI security dependencies,
Express middleware names and Axum layers as written. Nothing is stored
about query, header or cookie parameters, request bodies, responses,
parameter types, tags, operation ids or the server a route runs on.
Imported OpenAPI specs are stored as `api_spec` / `api_operation` nodes
with inferred `documents_route` edges (ADRs 0023, 0024).

Against committed references, extracted paths and methods agree with no
contradiction: full-stack-fastapi-template 23 of 23 operations of its
committed generated client (generated from FastAPI's own spec), the
realworld spec for Express, Axum and gin, gotify's committed Swagger 2
spec, seerr's and owncast's committed specs, and langflow's committed
FastAPI docs spec (117 operations agree; its one operation not extracted
with a path is registered under a factory parameter prefix and stored with
no path). Some corpora store routes without a path (dozzle: every route
behind an unproven mount; Next.js apps with an environment-dependent
`basePath`). One repository can hold several servers (langflow: two apps
both registering `GET /health`).

The egress boundary (ADR 0040) planned a try-it purpose; the local server
(ADRs 0041, 0052, 0075) has the request checks a state-changing endpoint
needs.

## Options considered

Generation:

- **Swagger-UI or Scalar rendering a full document, filling unknown parts
  with defaults** (`responses: {200: ...}`, `schema: {type: string}`,
  operation ids from handler names). Looks complete; states facts
  Graphvein never read. Rejected.
- **A document from stored facts only, with unknowns named in an
  extension** (`x-graphvein`), OpenAPI 3.1 so `responses` may be absent.
  Selected.
- **Merging imported specs into the generated document.** Hides which
  facts came from code and which from a hand-written file. Rejected:
  imported specs stay separate, linked per operation.

Explorer:

- **Embed Swagger UI or Scalar** (ARCHITECTURE.md's original table names
  them). Both render absent fields as "no parameters" or "no body" and
  cannot show "not read"; their "try it" sends from the browser to the
  document's `servers` (a request interceptor could redirect it to the
  proxy, but the guardrails would then depend on client code); both are
  large new UI dependencies that would need review against the strict
  CSP (ADR 0041). Rejected.
- **The API section's own React view over the generated document.**
  Selected: shows unknowns, markers and auth as written, and calls only
  the server's proxy.

Try-it:

- **Browser sends the request directly (CORS).** Bypasses egress and
  guardrails. Rejected (already decided).
- **Server proxy with a browser-supplied URL.** Lets any page of the
  server reach any host the policy allows. Rejected.
- **Server proxy that builds the URL from a configured environment and an
  operation of the generated document.** Selected.

## Decision

1. `internal/openapi` (new) builds an OpenAPI 3.1.1 document from stored route nodes through the `store`
   interface, under the claim contract
   `docs/contract/claims/openapi-generation.md` (rows OG-1..OG-99, default
   OG-D: anything not covered is not written). Paths and methods come from
   stored path entries; path parameters are named, required and untyped;
   query, header, cookie parameters, bodies, responses, operation ids,
   summaries and tags are never written and are listed per operation in
   `x-graphvein.not_read`. `security` is written only for complete FastAPI
   HTTP schemes with every dependency known and `auto_error` true;
   everything else is in `x-graphvein.auth`, and `security: []` is never
   written. Routes without a usable path, forms OpenAPI cannot express
   (multi-segment wildcards, optional parameters, host patterns, methods
   outside the eight, ANY) and conflicts (equivalent templates, duplicate
   operations) are listed in the root `x-graphvein`, never guessed into
   `paths`. Conditional and settings-default paths are included and marked.
   One document per repository, per member in project mode; `servers`
   only from configured try-it environments. Output is deterministic
   JSON or YAML. See "Generation" below.
2. The document is offered by a CLI command `graphvein openapi`, a server
   endpoint `GET /api/v0/openapi` and a UI download, all from one function.
3. The Explorer is a sidebar section of its own (`#/explorer`; developer
   decision after the design round, variant B with variant C's provenance
   chips, "Explorer UI" below), consuming the generated document, under
   rows OX-1..OX-49 of `docs/contract/claims/try-it.md`, and linking back
   to the API section for callers and impact. Imported specs appear as
   their own list and a compare view, never merged.
4. Try-it (developer decisions 2026-11-20; rows TI-1..TI-49 of
   `docs/contract/claims/try-it.md`, final for the backend). Requests go
   to `POST` endpoints of the local server that apply ADR 0052's request
   checks plus the session token and loopback peer of ADR 0075. The
   browser names an environment, a stored route and one of its path keys,
   and values; the server substitutes the path values into the key, or,
   for a free-form path the user confirms, appends the user's path to the
   environment's base URL. `internal/egress` gains the purpose `try-it`
   (configured infrastructure: allowed in every mode); each send uses a
   policy holding only the selected environment's origin (and its proxy
   when the environment opts in), never follows a redirect automatically
   and always verifies TLS.
   - **User-confirmed options, not hard refusals** (developer decision
     2026-11-20: maximum coverage without confident wrong facts;
     Graphvein does not gatekeep what the user has access to). Where a
     request cannot be proven safe but the user may have a reason to send
     it, the server offers an option that needs an explicit user action:
     an edit of the environment's file through the guarded edit (diff
     preview, hash precondition, token, loopback peer), or a one-time
     confirmation bound to the exact request. Nothing is a silent
     default; the server stays the enforcement point.
     - Untrusted TLS: the refusal carries the leaf certificate (subject,
       issuer, names, validity, SHA-256 of its DER); the user may pin it
       (`tls.pinned_sha256`), SSH-style: accepted for that environment's
       origin whatever its name, issuer or dates; pins add trust and
       never reject a certificate that verifies; a certificate that
       neither verifies nor is pinned is "changed", refused with old and
       new fingerprints. `tls.ca_file` adds a PEM bundle (mkcert's root)
       to the system roots for the environment, read at send time. The
       DER fingerprint was chosen over the SPKI one because it is what
       browsers and openssl show for comparison; renewing a pinned
       certificate needs a new pin (shown as "changed"). A pin is bound
       to one origin (project environments name it per pin). The CA
       file's content hash is part of the environment's identity.
     - A missing auth variable: `prepare` names it; the request is sent
       without auth only with `send_without_auth`, bound into the
       confirmation, GET included.
     - Free-form paths: allowed with a one-time confirmation that a
       "don't ask again" grant never covers; any of the eight methods;
       the path stays relative to the base URL with the dot-segment and
       encoding rules of operation paths.
     - A method override, `_method` or a body on GET/HEAD: treated as
       mutating (the normal confirmation).
     - `multipart/form-data` bodies built by the server from fields and
       base64 files, with a boundary derived from the content so the
       confirmation binds the bytes. WebSocket stays unsupported.
     - Redirects: never followed automatically; the answer says how to
       follow once: through an environment that holds the target, or
       after adding the target's origin as an environment (guarded
       edit), as a confirmed free-form request.
     - `use_system_proxy` per environment (default off): the egress
       client honours `HTTPS_PROXY`/`HTTP_PROXY`/`NO_PROXY` for that
       environment only; the proxy joins the send's policy and the
       client refuses any destination but the environment's origin. An
       https proxy's TLS is made apart from the origin's (system roots,
       the proxy's name; never the environment's pins or CA file).
       NO_PROXY follows Go's documented rules, implemented in
       `internal/egress` (no new dependency).
     - `limits` per environment (`request_bytes`, `response_bytes`,
       `timeout_seconds`; ceilings 64 MiB, 256 MiB, 600 s) set through
       the guarded edit; a cut or refused request names the limit.
     Still refused: hosts never configured (they become configured only
     through the guarded edit), CRLF, `Host` and framing headers, h2c,
     WebSocket, cross-site requests to Graphvein's own endpoints.
   - **Confirmations, no gatekeeping.** No method is blocked. GET and HEAD
     are sent directly outside production; every other method needs a
     server-issued, one-time confirmation id bound to the exact request
     (environment, method, URL, typed headers, body hash; 120 s), or a
     per-environment "don't ask again until I switch" grant that a
     confirmed send can create. A production-labelled environment
     (`production`, `prod`) needs one acknowledgement per selection,
     covering every method including GET; no typed name. Selecting
     another environment revokes acknowledgements, grants and pending
     confirmations. Grants, acknowledgements and typed values are per
     member and bound to the environment's origin, base path, label,
     auth configuration, certificate pins, CA file and proxy use; a file
     edit changing any of them drops them, as a switch does. A GET or
     HEAD with a body or a method override needs the confirmation of a
     mutating method. A request without a valid confirmation or grant is
     refused by the server whatever the client does (the M2
     acceptance).
   - **Auth.** A header value comes from a named environment variable, or
     from a value the user types for the session: kept in server memory
     only, never written, logged or returned after entry, cleared on
     restart, and preferred over the variable.
   - **Limits and logs.** Body sent 1 MiB, response read 4 MiB
     (truncated after), 30 s per exchange unless the environment raises
     them within their ceilings, 4 sends at once. Logs carry
     environment, label, method, path template, status, sizes and
     duration only; response headers that carry credentials or have
     secret-like names are redacted; no request history on disk.
5. Environments (name, user-chosen label, `base_url`, optional auth
   naming a header and a variable) live in the repository's
   `graphvein.yaml` `tryit` section; in project mode the manifest's
   `tryit` section adds environments spanning members (a base URL per
   member), and each member's own environments apply too. A URL typed in
   the UI is always saved: the server writes it as an environment to
   `graphvein.yaml` or the manifest through the guarded edit of ADR 0075
   (diff preview, hash precondition, token and loopback peer, atomic
   write). Only environments in a file are dialable. Base URL suggestions
   (stored Compose published ports and Traefik routers, committed spec
   `servers`, Dockerfile `EXPOSE`, the addresses other project members
   call the member at, port literals in code and config: `listen(3000)`,
   `uvicorn.run(port=)`, Go listen addresses, package.json scripts,
   Procfile; framework defaults labelled as such) each carry their source
   file and line and a reason, and are never used until the user picks
   one, which goes through the edit.

## Generation

Implemented by the generation core (T029) after the developer
decisions of 2026-11-20; rows OG-1..OG-93 of
`docs/contract/claims/openapi-generation.md`.

### Decisions

- **One document per repository** (design G1 option A), per member in
  project mode; every operation names its route, file, line, framework
  and extraction rule in `x-graphvein`, so the explorer can filter by
  component. Which app object serves a route is not stored, so one
  document can mix servers of a monorepo.
- **One function, three outlets** (G3): `openapi.Generate` and
  `openapi.Encode` feed `graphvein openapi [--format json|yaml] [--out
  file]`, `GET /api/v0/openapi?format=` and the UI download. YAML is the
  same document re-encoded through the existing `yaml.v3` dependency
  (key order kept), so it costs no new dependency and no second golden.
- **OpenAPI 3.1.1** (G4).
- **Conditional and settings-default paths are included** (G2), marked
  `x-graphvein.conditional` and `x-graphvein.resolution:
  settings_default`.
- **Duplicates are never picked** (G1, OG-20): two routes with one method
  and path key from several registrations or apps are listed in
  `x-graphvein.conflicts` with no operation, except an Express
  `equivalent_registrations` group, which the matcher already treats as
  one route.
- **`servers` only from configured try-it environments** (G5):
  `openapi.Servers` lists the project environments that give the member a
  base URL, then the repository's `tryit.environments`, with name, label,
  file and line in `x-graphvein`; never an auth setting. A base URL
  inferred from code is never a server.
- **No `operationId`** (G6), no summary, tags or description; listed in
  `x-graphvein.not_read`.
- **Security** (G8): standard `security` only from complete facts: the
  route's shape security section when it is read (complete, every
  alternative and scheme a fact with its OpenAPI fields), else FastAPI's
  stored HTTP scheme classes with `auto_error` true and every dependency
  known. `security: []` is never written, even for a section proven
  empty: a proxy or middleware outside the code may still require auth.
- **Test-only routes are left out and counted** (G9).
- **Committed specs are never merged** (G10): listed in
  `x-graphvein.imported_specs`, and `x-graphvein.documented_by` names the
  committed operation of a `documents_route` edge.
- **Routes behind an unknown path part** (G11 option A) are listed in
  `x-graphvein.unresolved` with the unknown part named (a Next.js
  `basePath` read as not literal; a mount or prefix not read, with the
  stored diagnostic naming the registration when there is one) and the
  keys that would place them.
- **User-supplied path values.** `graphvein.yaml` gains an `explorer`
  section: `explorer.path_values: [{key, value}]`. Keys must survive
  re-indexing, so they name sources, not runs: a route identity
  (`route:<file>#<METHOD> <pattern>`) for the whole path of one route, or
  `nextjs_base_path:<project directory>` for a project's `basePath`. A
  value is used only for a route without a usable path read from code;
  the operation then carries `resolution: set_by_you` and an assumption
  with the key and value. Values change only the explorer, try-it and the
  generated document. The UI edits them through `POST
  /api/v0/explorer/path-values`, which mirrors the manifest edit (ADR
  0075): typed operations, a diff preview, the file's hash as a
  precondition, a session token, a loopback peer, the server's Host and
  Origin checks, a strict re-validation of the result as the CLI loads it,
  and an atomic write that keeps comments.
- **Shape emission** (OG-70..OG-89): only items of kind `fact` become
  `parameters`, `requestBody`, `responses` and `security`; a name read in
  code, an intended or returned type and a mapped item stay in
  `x-graphvein.shape.not_standard` with the row that keeps them out. No
  row writes a non-fact item into a standard field with a label: a
  generic OpenAPI tool reads standard fields as guarantees and ignores
  extensions. A fact parameter whose presence was not read, a header
  OpenAPI ignores, a body or response without a content type and a
  partial security section are refused for the same reason. Fact models
  become `components.schemas` (shared when the whole definition is equal,
  numbered otherwise). Section states, depth caps and stop counts are in
  `x-graphvein.shape`.

### Options considered

- **Per-component documents** (G1 option B): wrong for a server split
  over several packages (realworld Express). Rejected for now; a stored
  root per route (G1 option C) is the exact follow-up.
- **A prefix-level key for an unresolved mount** (one value for every
  route behind it): the composition chain through an unresolved mount is
  not stored, so the generator cannot know which routes sit behind it or
  where the prefix goes. Rejected; route keys and the Next.js basePath key
  (whose position is known) are the stable keys available.
- **Standard fields with an `x-graphvein` label for intended, returned
  or mapped items.** Rejected: see "Shape emission" above.

### Explorer UI

Added after the mockup round (three end-to-end variants: A, tabs in the
API section; B, a Swagger-like section; C, a spec-first view with a
drawer). The developer chose B with C's provenance chip on every field:
the Explorer is primarily API documentation for unfamiliar or generated
repositories, which people read as Swagger presents it.

### Options considered

- **A: Operation and Try it tabs in the API section.** No new navigation,
  but documentation is reached through the route list one route at a
  time. Rejected.
- **B: a sidebar section "Explorer".** Operations grouped and collapsible
  (router file, or the documenting spec's tag), method-coloured rows,
  documentation and Try it out inline in the expanded operation, the
  document in a Spec tab. Repeats the route list in another shape.
  Selected.
- **C: an "OpenAPI document" view in the API section.** Shows the whole
  document structure, but a paths tree is a worse reading order for an
  unfamiliar API than grouped operations. Its provenance chip per field is
  kept.

### Decision

- Route `#/explorer?view=&group=&op=&q=&route=`; a lazy chunk, so the entry
  chunk does not grow for people who never open it.
- One source: `GET /api/v0/openapi` (plus `GET /api/v0/source` for the
  tags of the committed spec operations that document routes, only while
  grouping by tag, and the path-values endpoints). Every section shows the
  state the document proves: "Not read by Graphvein" for a field in
  `x-graphvein.not_read` or a stored `not_read` state, "None, proven" only
  for `provably_none` with its evidence, "Partial: more may exist" with
  the reason. Items the document keeps in `x-graphvein` are shown with
  their claim row, never as standard fields.
- Labels of kinds, states, resolutions, accesses and values set by you
  are rows 59-64 of the label table (ADR 0078).
- Unknown path parts are edited through the guarded path-values edit
  (preview with diff, then apply); the operation then shows "path set by
  you".
- Project mode: a member select with each member's operation count from
  its own document; every count on the page is the selected member's.
- A header, query parameter or cookie an applying security scheme reads
  is shown in its section ("read by the security scheme (see Security)"),
  and a proven-empty section where a scheme reads, or may read, says "None
  besides the security scheme" (OX-35): the credential is never hidden
  behind "None, proven".
- The API section's route detail shows one route's stored shape in its
  Overview tab ("Request and response", OX-40..OX-43) with the same
  section pieces and rules, read from the endpoint's `shape` rather than
  the document (the detail already serves it, with its own error), and
  links to the Explorer with `route=<route identity>`: the Explorer maps
  the identity to the operations the document writes for it, so the API
  section never derives an OpenAPI path key itself.

### Editor links on shape chips

Context: the developer asked that a chip's file:line open in the
configured editor, without clutter. Editor links are built only by the
server, per position (a node's `ide`, the schema reasons' links), and the
Explorer reads only the document, which must stay free of editor links
(they depend on the browser's editor and the checkout's path).

Options considered: building the URL in the browser from the editor's
template (rejected: the UI never builds or rewrites a link, and the
server's caveats, such as IntelliJ IDEA's tab size or a changed file,
would be lost); a new link endpoint taking positions (rejected: a new
interface for what one existing response can carry); links inside the
document's `x-graphvein` (rejected: the document is portable).

Decision: `GET /api/v0/api/endpoint` gains an additive `shape_links` map
("file:line" to an IDE link, built with the same function as the schema
reasons' links, under the response's `ide` status and `editor=`) for the
positions the route's shape chips show: its registration, the first chain
step of each security dependency and every provenance of its shape. The
route detail reads it from the response it already has; the Explorer
requests the endpoint detail of an operation's route when the operation
is opened (once per route and editor). Each chip with a link ends in an
icon-only button with the editor's mark (the IDE buttons' `IdeMark`,
named by `presentIde`), muted until hover or focus; a chip without a
link, or with editor links off, shows none.

Known gaps: the Explorer shows the buttons only after the route's detail
arrives, and only for routes in the index it reads (a document from a
committed spec has none); a chip's chain steps have no button.

### Routes that use a database model

Context: the developer asked that the Schema view's table
detail list the routes that accept or return the table's model, and that
the route detail say when one of its models is also a table. A FastAPI
route often takes or returns a SQLModel `table=True` class; just as often
it uses a Pydantic schema that copies the table's fields, which is a
different class with different validation.

Options considered: matching by name or by fields (rejected: a copy of a
table's fields, or a class with the table's name in another file, is not
the table's model, and the result could not be proven); a new edge kind
written at index time (rejected for this unit: an indexer contract and
extractor version change, while the stored shapes and model tables
already hold everything needed, and the model-type edges of step W2 are
designed in parallel); computing the link in the server from the stored
shape and the stored table (chosen).

Decision: a shape model and a model table are linked only when they are
the same declaration node (row MT-20 of
`docs/contract/claims/route-model-types.md`): the model's provenance is
the start line of a stored class node with the model's name, and the
table's `class` attribute is that class node's identity. Today only
SQLModel tables store their class node, so only FastAPI routes using
SQLModel table classes are linked. `GET /api/v0/schema/table` gains an
additive `model_routes` for model tables (the routes with each use's
direction, section, status and provenance, editor links for the chips,
and a state); `GET /api/v0/api/endpoint` gains an additive
`model_tables` for HTTP routes with a shape. "No route uses this model"
is said only when every stored route has a shape whose request and
response sections are complete or provably none and whose schemas are
read to the end (MT-26); otherwise the UI says how many routes were not
fully read, or that shapes were not read at all.

Known gaps: Go and Rust shapes hold inline schemas, not named models, so
a GORM struct bound with `ShouldBindJSON` or a Diesel struct is never
linked; Express shapes name TypeScript interfaces and type aliases, never
a TypeORM entity class; model tools other than SQLModel do not store
their class node (`no_declaration`). With FastAPI the response sections
are usually partial (statuses from exception handlers), so a proven
"none" is rare. The Explorer operation does not show the note (it reads
the generated document).

### Shape differences on links

Context: with route shapes stored for every framework, the developer asked
that the existing link details (the Flow tab's line detail and the
Explorer operation's links area, no new section) show how what a linked
frontend call sends differs from what the route reads, only when both
sides are fully read, and nothing otherwise (never "no differences").

Options considered: comparing at link time and storing the result on the
calls_route edge (rejected: edges carry no attributes in the contract,
and a project link joins nodes of two stores); reading the call's
arguments in the server (rejected: the server reads stored nodes only,
never cached syntax); a new comparison endpoint (rejected: a new interface
for what the link responses can carry).

Decision: the extractor records what a call provably sends as the
client_call attribute `sends` (contract v1 open attribute, link time,
no new cached syntax): the keys of a literal JSON fetch body or literal
`URLSearchParams`, and the required properties of a proven hey-api
operation's body and query types. The server compares it with the
route's stored `shape` when it presents a link, only where the route's
section is `complete` with no caveat and no depth cap and every compared
item is a `fact`, and serves the result as the additive
`shape_differences` on the endpoint detail's callers (and project
callers, marked `cross_repository`) and on the Flow endpoint's
calls_route edges, with each side's editor links. The rows are
SD-1..SD-12 of the
[shape differences claims](../contract/claims/shape-differences.md).
The UI shows only comparisons with a difference, in the words "sent by
the frontend, not read by the handler" and "read by the handler, not sent
by this call" (the latter only for a key the handler requires as a fact
and a complete literal), each side with its provenance chip.

Known gaps: Express and Next.js bodies are never `complete`, and zod
objects are stored as accepting unlisted keys, so their links show
nothing; Orval, axios, Angular and SWR calls record no `sends`; a hey-api
caller may change the media type through `options.headers`; a literal
key whose value is `undefined` only at run time counts as sent.

### Try it out (T029 part B)

Developer decisions 2026-11-22 on the mockups' variant B:

- **One environment for the page**: a bar above the operations shows the
  selected environment (label chip, name, base URL, where its auth comes
  from); while one is selected its label is on every operation row.
  Environments are added by typing a URL or picking a suggestion (source
  kind and file:line, used only when picked), always through the guarded
  edit with a diff preview and one explicit confirmation.
- **Production**: one warning dialog when switching to a production
  environment, then a persistent marker; no typed name, nothing blocked.
  In project mode the one dialog lists every member the project
  environment covers. The server keeps one selection per process (TI-8)
  and records acknowledgements per member, so the UI remembers the
  project environment the user acknowledged and, when the member changes,
  selects it for the new member with the acknowledgement, without a
  second dialog, only when the dialog listed that member with the base
  URL it has now and the manifest's hash is unchanged; a member added or
  a base URL changed since, or any manifest edit, shows the dialog again
  with the reason. A full page reload forgets it (a new dialog), which
  errs on the side of asking.
- **Confirmations in place of Send** (variant B), with the reasons the
  server gives and "don't ask again for this environment until I switch";
  GET and HEAD outside production are sent directly. The UI always calls
  `prepare` first and follows its answer; `send` is the server's check.
- **Refusals the user can act on** open a dialog: missing auth (send
  without auth, or a value for the session), an untrusted or changed
  certificate (trust it: the settings edit with the pin), a limit (raise
  it), a redirect to another origin (add it as an environment and follow
  once as a free-form request).
- **Builder**: path values (required, type or "type not read"), query and
  header rows pre-filled from the operation's items with their
  provenance, a JSON template from a fact or intended schema marked as a
  template, multipart fields and files, a free-form path; "Copy as curl"
  with the auth as `$NAME` or a placeholder.

Rows OX-20..OX-34; labels rows 65-68.

### Known gaps

- Grouping by tag reads each documenting `api_operation` node once
  (bounded to four requests at a time); the document carries no tags
  (OG-24). A field in `documented_by` with the operation's tags would
  replace those reads.
- OX-8 (precision label), OX-9 (imported operations list) and OX-10
  (compare view) are not built yet.
- Try it out keeps no history: a response is shown until the panel
  closes (TI-21 keeps nothing on the server either).
- A security scheme kept in `x-graphvein` whose type the document does
  not give (an OAuth2 scheme without flows, OG-87) has no known location:
  no name is shown, only "None besides the security scheme".

## Consequences

- The document is valid OpenAPI 3.1 and honest, and stays thin until
  shape extractors populate the route `shape` attribute.
- `graphvein.yaml` has a new optional section; the guarded edit writes the
  repository's `graphvein.yaml` (creating it with `version: 1` when
  absent) and nothing else.

### Known gaps

- `requestBody.required` is never written (the shape model has no body
  presence); OpenAPI's default reads as optional, and
  `x-graphvein.required: "not_read"` says otherwise.
- A response's `description` is "Not read by Graphvein" (OpenAPI
  requires the field).
- First-match precedence (Express, Starlette) is shown as candidates
  (`x-graphvein.may_be_shadowed`, order not checked): registration order
  across routers is not stored.
- A route behind an unresolved mount needs one value per route.
- A route identity changes when the registration's file or pattern
  changes; its value is then reported `unused` and must be set again. A route
  key records the route's content hash with its value, so a moved
  identity is reported (`registration changed`) instead of placing
  another route; identical twin registrations cannot hold a value.

## Shape model

Added after the design round (developer decisions 2026-11-20): the
Explorer is primarily API documentation for unfamiliar repositories, and
request/response shape is extracted for every supported framework by
separate units that run in parallel. They need one shared model.

### Options considered

- **Per-framework ad hoc attributes** (each extractor stores its own
  `query_params`, `body_type`, ... in its own form). Fastest for the first
  framework; the generator, the Explorer and the honesty rules would then
  be written once per framework, and two units could encode "not read" and
  "none" differently. Rejected.
- **A shared model in one package, one route attribute, one claim table**
  (`internal/extract/routeshape`, attribute `shape`, rows SM-1..SM-29 of
  `docs/contract/claims/route-shape.md`), each framework adding only its
  own read rules (`SH-*` tables). Selected.
- **A new node and edge kind per parameter or schema** (`route_param`,
  `schema` nodes with `has_param` edges). Queryable per field, but every
  item would need a stable identity and the contract would grow new kinds
  for data that is only ever read per route. Rejected for now; the
  attribute can be projected into nodes later without changing its
  meaning.

### Decision

1. Package `internal/extract/routeshape` holds the Go model (`Shape`, its
   sections, `Param`, `Content`, `Response`, `Requirement`, `Scheme`,
   `Model`, `Schema`), `Validate` (every invariant, by row), `ToAttribute`
   / `FromAttribute`, the call-following helper (`Follow`, `Resolver`,
   `Frame`, `Call`, `Records`) and the depth statistics (`Stats`).
2. Each section has a state: `not_read` (nothing claimed), `provably_none`
   (with evidence of kind fact or mapped), `partial` (with a reason) or
   `complete`. Each item has a provenance: kind (`fact`,
   `read_in_handler`, `intended`, `returned`, `mapped`), the extractor's
   rule, file and line, the call chain, confidence (1 for code, the link's
   for mapped). The invariants of the shape inventory are model rules
   checked on write and on read: a name read in code is never required and
   never typed; intended and returned types never become the schema of a
   fact; headers read by dependencies are `read_by_dependency`, never
   declared parameters.
3. Storage: the route node attribute `shape` (model version 1). Contract
   v1 attributes are an open map, so this is an optional attribute, not a
   contract version change; `docs/contract/v1.md` names it. The server
   decodes and validates it and serves it on `GET /api/v0/api/endpoint`
   (`shape`, or `shape_error` for an invalid one) and a compact summary on
   the endpoint list rows; both fields are absent when no shape is stored,
   so existing goldens do not change.
4. Call following: through proven call targets only (a unique stored
   definition), at link time over cached syntax. The depth limit is a
   setting like the other index settings: `index.shape_depth` in
   `graphvein.yaml` or `graphvein index --shape-depth`, default 6 (the
   architecture chain default), allowed 1 to 10. Every chain is stored
   with its depth and stop reason (`ended`, `depth_cap`, `cycle`, `joined`,
   `budget`, `resolver_error`, `unresolved`, `ambiguous`, `external`,
   `transformed`, `opaque_type`); `depth_cap` is its own reason, shown as
   "depth cap reached (N)" with a hint to raise the setting, never as
   unknown. The limit used is stored on every shape
   (`follow.depth_limit`). Each SH unit reports, per corpus, the histogram
   of chain depths and stop reasons (`routeshape.Stats`), so the default
   is set from data; it is lowered only if timing on the largest corpora
   requires it.

### Consequences

- Shape extractors run in parallel without touching each other's code:
  each writes a `Shape` through `ToAttribute`, which refuses a shape that
  breaks a row, and adds its own `SH-*` table.
- Changing `index.shape_depth` needs a re-index (a re-link over the fact
  cache), never a re-parse; an extractor that needs parse-time data caches
  enough for depth 10.
- Shapes are stored per route: a model shared by many routes is repeated
  in each route's attribute.

### Known gaps

- The depth limit is recorded on each stored shape, not in the run header:
  a run that stores no shape does not record which limit it would have
  used. Recording it in the header is a contract change left for the first
  SH unit that needs it.
- No extractor populates the attribute yet; until one does, every route's
  shape is not read.
- The schema subset has no formats, numeric bounds, string patterns or
  composition (`oneOf`, `allOf`); a form that needs one is `unknown` with a
  reason.

## Consequences

- The generated document is honest and valid but thin: for every corpus
  it has paths, methods, path parameter names and some auth, and nothing
  about payloads. Schema extraction per framework (FastAPI first) is a
  later task with its own claim table and oracle.
- Tools that need operation ids or responses (client generators) get
  little from the document until then; the document is for reading,
  comparing and trying, not for client generation.
- New non-GET endpoints on the local server; the egress boundary tests
  extend to the `try-it` purpose; the `serve` runtime test still expects
  no egress attempt without a send.
- New configuration section `tryit` in `graphvein.yaml` and in the
  project manifest (strict decoding like every section).
- The error envelope gains an optional `details` member, set only by the
  try-it answers that document it (certificate, missing auth, limits).
- A pinned certificate is trusted for its origin even when expired or
  issued for another name: the user's explicit choice, shown with its
  validity and names when it is made; pins never widen trust for any
  other origin.
- With `use_system_proxy` the proxy sees every plain-http request of that
  environment in full, and for https the destination host and port (the
  request stays inside TLS); it is the user's own proxy, chosen per
  environment.
- Raised limits let one send hold up to 64 MiB of body and 256 MiB of
  response in server memory.
- A typed auth value lives in the server's memory for the process
  lifetime: any local process that can talk to the server and read its
  session token can use it (ADR 0041 "No authentication"); it never
  reaches disk or logs.


## Prefix-level path values (T029 FU-2)

**Status:** Accepted (delegated review, 2026-11-23)

### Context

A route behind a hop the linkers cannot read (a prefix from
configuration, a router argument or wrapper no rule proves) had no path,
and the only user value was a route key: one value per route. dozzle
(about 85 routes under `r.Route(base, ...)`, `base` a flag) and miniflux
(about 100 user-interface routes behind a middleware chain) needed a value
for every route.

### Options considered

1. Keep route keys only. Correct, but unusable at that scale.
2. A key per unknown hop whose value is relative to the hop's parent
   router (the design sketch: the stored chain above the hop composes the
   rest, two unknown hops need two values, inner applied first). In both
   corpora the parent itself has no proven path (dozzle's router escapes
   into a return no rule reads; miniflux's mux reaches its server through
   calls no rule reads), so no value would place a route.
3. A key per unknown hop whose value is the whole prefix where the routes
   below the hop are served ("the hop passes requests on with prefix P").
   One value places every route below the hop whatever is above it.

### Decision

Option 3. The linkers store, on each route without a path, per hop where
composition stopped walking up from the registration, the route attribute
`unresolved_mounts`: the hop's key (`mount:<file>#<function> <hop call and
first argument>`, `@N` for repeats in one function; no line), a hash of
the hop (file, function, whole call with function bodies elided), the stop
reason, and the route's paths below the hop composed through the same
rows from the hop's router. `explorer.path_values` takes the key; the
generator places each route at `value + path below the hop` (rows
OG-94..OG-96), marked `set_by_you`, checked against the stored hash
(`registration changed`), refusing twins, never displacing a code-read
operation (OG-92), with a route key taking precedence. Express and FastAPI
operations go through OG-19's first-match check like any other; Go
operations are marked `shadowing: not_checked` because the GR-C1, GR-C3
and GR-C7 checks need the parent router above the hop. Axum `nest` hops
(T029 FU-3) follow the same form; their operations are also marked
`shadowing: not_checked` (the routes beside the hop on the parent router
are not compared), and the nested `/` path lands at the value without a
trailing slash, stored only for a crate whose axum version is proven
within 0.6.20 to 0.8.x. A hop below
another unknown hop takes the whole prefix in its own value; the routes
directly below the outer hop need the outer hop's value.

Revised after the independent review (T029 FU-2 fixes): what the value
covers depends on what Graphvein proved above the hop, and each stored hop
says so in `value_covers`:

- `hop`: every router above the hop is proven. The known prefix above the
  hop is composed into each stored path (`above`), shown fixed in the
  editor, and the value is only the part the hop adds (chi
  `r.Route("/outer", ...)` then `r.Route(base, ...)`: value `/b` places
  `/outer/b/...`). The composition above the hop (the known prefixes and
  hops from the root) is hashed into the registration, so an edit above
  the hop marks the value "registration changed".
- `nothing`: the hop adds no part Graphvein could not read (a net/http
  `Handle` without `StripPrefix` whose outer pattern or router is not
  proven, a wrapped router passed to `Handle`). The stored paths are the
  served paths; the only accepted value is `/`, stating that the hop
  passes the requests on unchanged.
- `root`: a router above the hop is not proven either (dozzle's escaped
  router, miniflux's middleware chain). The value is the whole prefix from
  the root, and the editor says so ("the routers above this hop are not
  proven either").

An empty path below a hop (gin `g.GET("", h)`) is stored as `""` and
placed at the prefix alone, distinct from `/` (the prefix with a trailing
slash). An Express or FastAPI candidate refused against a code-read
operation (OG-92) is still listed, with its `set_by_you` key, in that
operation's `may_be_shadowed` (OG-19), since with the stated value the
route registered earlier answers the requests.

Options considered for the revision: keep the whole-prefix value and only
name the read outer prefix in the editor (simpler, but the value then
repeats a prefix Graphvein read, and an edit above the hop is not
detected); compose the known prefix and value only the hop's part
(chosen: the value states only what Graphvein did not read).

The attribute is an optional key in the open `attributes` map of route
nodes: contract v1 already accepts it, so `contract_version` stays 1
(docs/contract/v1.md, "Versioning and extensions"). The stored output
changes, so the extractor version is extractors-56.

### Consequences

- dozzle: one value for `r.Route(base)` places its 85 routes; miniflux:
  one value for either of the two hops above the user-interface mux places
  its 100 routes (see the T029 FU-2 task record (internal) for the counts).
- The explorer's "Not in the document" fold lists the hops ("Missing
  prefixes") with the number of routes below each and a preview of where
  they land; route keys keep working.

### Known gaps

- Go serving positions whose `StripPrefix` prefix is not a constant are
  not stored as hops; neither is the nested `/` path of an Axum hop in a
  crate whose axum version is not proven within 0.6.20 to 0.8.x. An Axum
  router nested both under a constant prefix and under a hop keeps only
  its constant path.
- A FastAPI hop inside a helper function is one hop for every call of the
  helper; a value applies to the pathless routes of every instance.
- A refused prefix (a pattern no row reads, an invalid chi prefix) is not
  a hop: a value cannot make a refused form read.
- The Go checks above a hop are not re-run with the user's prefix; the
  operations say so instead.
- A wrapper or forwarding closure passed to a net/http `Handle` covers
  nothing: a wrapper that rewrites the path cannot be described by a hop
  value (route keys still can). A Go hop whose composition through the
  placeholder for the unread part fails (an outer pattern that does not
  match it) stores no entry for that route.
- With `hop`, a parameter or wildcard in the value is not checked against
  the framework's composition rules beyond OG-91's value syntax.


## Security schemes beyond FastAPI and group middleware (T029 SH-S)

**Status:** Accepted (delegated review, 2026-11-23)

The shape model's `security` section (row SM-19) was filled only for
FastAPI. T029 SH-S fills it for Express (express-jwt,
express-oauth2-jwt-bearer, express-basic-auth, passport strategies,
express-openid-connect), Axum (typed Bearer/Basic headers, tower-http
header layers, axum-login), gin and chi (BasicAuth, jwtauth) and Next.js
(next-auth `getServerSession`), by rows SH-S1..SH-S24 of the [route
security claim contract](../contract/claims/route-shape-security.md); a
form no row lists states no scheme and the section stays `not_read`.
Generation is unchanged: `security` is still written only under OG-87.

gin and chi auth usually sits on the router, not the route, so the Go
linker stores two new route attributes (row SH-S18,
[docs/extract/auth.md](../extract/auth.md)): `group_middleware`, the
router-level middleware proven to run before the route on every chain
that serves it, and `group_middleware_unknown`, middleware that may run
but is not proven (order, conditional registration, some chains only,
wrapped routers). They are additive, optional keys in the open
`attributes` map of route nodes, listed in
[docs/contract/v1.md](../contract/v1.md): contract v1 already accepts
them, so `contract_version` stays 1. The stored output and the cached
syntax change (gin/chi `Use` calls, next-auth calls), so the extractor
version is extractors-57.

Known gaps: authorization (roles, ownership) is never a scheme; custom
middleware, sessions and strategies no row lists keep only their
middleware attributes; the Axum section is never `complete` (layers
outside the served router are not read).

## Model-type edges and change impact through models (T029 W2)

### Context

Route shapes name the models their items use (a Pydantic class, a Go
struct, a Rust serde type, a TypeScript interface or zod schema), and the
shape extractors resolve each to one declaration. Change impact (ADR 0056)
followed no relation from a changed model to the routes using it, so a
changed request or response model reached nothing.

### Options considered

1. **Derive at query time from the stored shapes.** The stored shape
   keeps model names and the declaration's file and line for FastAPI and
   TypeScript models, but Go and Axum shapes inline struct schemas without
   any declaration; matching names or lines to nodes after the fact would
   be a guess, and every traversal step would decode every route's shape.
2. **Store the declaration on the shape model** (a new field of `models`).
   The shape reader refuses unknown fields (row SM-1), so this changes the
   shape model version; and Go and Axum would still need models.
3. **Record the resolution at link time as an edge plus a route
   attribute** (chosen). The extractor marks each schema node with the
   declarations it was read from (in memory only, never stored in the
   shape); the indexer keeps the uses whose declaration node is emitted,
   writes them on the route (`model_uses`) and adds one `uses_model` edge
   per route and declaration.

### Decision

Option 3, rows MT-1..MT-19 of
[route-model-types.md](../contract/claims/route-model-types.md). The edge
kind and the attribute are additive within contract v1 (extensible edge
kinds, open attributes): no `contract_version` change. Edges carry no
attributes in v1, so the role (section, status) and the using item's
provenance kind live on the route's `model_uses` entries; the edge is a
fact when some use is read from code and inferred only when every use is
mapped. Change impact adds relation I16 (incoming `uses_model` from a
declaration to a route), whose steps carry the route's uses and whose
wording names role and kinds; a gained or lost edge is reported on its
route's C3 item (`models_changed`). The change classification version
becomes `T019-r8`.

### Consequences

- A changed model (or a field inside it, which changes its content hash)
  reaches the routes using it, and from them their callers, within the
  existing caps.
- Intended and returned uses make a fact edge: the code names the type.
  The path says "depends on" and the step's chips say "Intended" or
  "Returned"; the dependency is on the route's declared interface, not on
  a runtime check.
- Extraction output changes (new edges and attributes): the extractor
  version is bumped with the T029 stack.

### Known gaps

- Spec-mapped models name no code declaration, so no mapped use is stored
  today (row MT-10 is in place for one that does).
- Type aliases expanded by the Rust or TypeScript readers record only the
  final declaration, and a Go type alias (`type A = pkg.T`) records only
  the alias, never its target (a changed target does not reach the
  route); server action arguments are not routes and have no
  edges; a FastAPI `Query()`, `Header()` or `Cookie()` parameter typed with a model is not read by the shape and records nothing.
- A removed model reached through the change set reads the route as the
  head graph holds it; when the head route no longer lists the use, the
  step keeps only the relation wording and the name.

## Known gaps

- Graphvein does not store which app object serves a route; one document
  per repository can mix servers in a monorepo.
- Absence of `security` in an operation reads, in OpenAPI, as "no
  requirement"; the description and `x-graphvein.not_read` say otherwise,
  but a generic tool will not read them.
- Routes behind unproven mounts or a non-literal Next.js `basePath` are
  not in `paths` (listed in `x-graphvein.unresolved`); for some corpora
  (dozzle, umami, ai-chatbot) that is every route.
- Express optional parameters and multi-segment wildcards are not
  represented in `paths`.
- Worst-case memory of try-it: 4 sends in flight, each up to a 96 MiB
  posted JSON, a 64 MiB body and a 256 MiB response (plus the JSON answer
  of that response), so about 1.7 GiB or more when every environment
  raises its limits to the ceilings. The in-flight count is not lowered
  for large limits; a user who raises them accepts that cost.
- With `use_system_proxy` and an `http://` environment, the proxy reads
  every header, the auth value included, in clear; `prepare` says so.
- A pin is per certificate, not per key: a renewed certificate is
  "changed" and needs a new pin.
- On Windows and macOS, a `ca_file` is added to a system pool that the
  platform verifier also consults; the exact combination follows Go's
  `x509.SystemCertPool` rules.
- The multipart summary trusts the part metadata the UI sends; file
  contents arrive base64 in JSON (about 4/3 of their size).
- The confirmation proves a deliberate act in the UI of this server; any
  local process that can read the session token can also confirm (ADR
  0041 "No authentication").
- Session state (selection, acknowledgement, grants) is per server
  process, not per browser tab: two tabs share it.
- Try-it uses the stored route's own path entries; routes the generated
  document lists as conflicts (duplicate operations across apps) are still
  callable by route identity; Next.js routes behind an unresolved
  `basePath` (and other unavailable operations) can be tried only as a
  confirmed free-form path.
- Port literals are read line by line in their literal shape; a port in
  a variable, a multi-line call or Kubernetes Service ports give no
  suggestion.

## Review notes (2026-11-23)

Accepted after the final gate (CI green on Linux, Postgres and Windows). Counts and oracle runs are those recorded in
the T029 task record (internal); every unit ended with the full suite
(SQLite and Postgres) passing. Review classes: A = confident wrong fact
in a common form, B = wrong fact in a narrower form or a guard gap,
C = minor or documentation.

**Try it backend and options (TI, TI2).** Results: one test per TI row
group on SQLite and Postgres, the row table kept 1:1 with the code;
certificates, CA files, pins, system proxy (http and https), multipart,
free-form paths and redirects tested against local test servers.
Revisions: TI 1 A (grants and typed auth followed an environment to a new
origin), 3 B (auth not in the confirmation hash, method override and
GET bodies, multipart/WebSocket refusals), 4 C; TI2 1 B (an https proxy
verified with the origin's trust), 6 C (per-origin pins, encoded `/` in
free-form paths, CA file checks, cleartext proxy warning, memory gap).
All fixed.

**Shape model (SM).** Rows SM-1..SM-29; 53 violation cases; depth
default 6, configurable 1..10; no corpus chain reached the cap at 6 or 10.

**Generation core and path values (GC, FU-2, FU-3).** Results: the
OpenAPI 3.1 validator (plus each schema as JSON Schema 2020-12) on every
golden and 44 corpus documents: 0 invalid. Paths and methods against the
specs committed in 10 corpora: 0 contradictions (fastapi 23/23 exact;
langflow's 6 reference-only operations all accounted for). Prefix-level
values: one value places dozzle's 84 and miniflux's 100 pathless routes;
axum `nest` hops confirmed on axum 0.7.9 and 0.8.8. Revisions: GC 5 B
(subclassed schemes, user values removing code operations, unstable
route keys, Starlette convertor wording, first-match shadowing), FU-2
1 A + 3 B + 1 C (gin `GET("")` slash, what a hop value covers,
non-stripping net/http hops), FU-3 `nest_service`. All fixed.

**Shape units.** Every unit has a claim table kept 1:1 with code and an
oracle run against the real framework; all final runs 0 disagreements.
- Go (gin, chi, net/http): real servers; 35 routes requested, 36 request
  fields decoded and refused by kind. Review 2 A, 7 B, 4 C fixed
  (validator `dive` and struct-kind `required`, late Content-Type,
  embedded types, never-sent statuses).
- FastAPI: `app.openapi()` (52 operations) and TestClient (72 fact
  statuses reached) on the fixture; the template's committed client
  (23 operations) and langflow's committed spec (117 operations, 4
  differences, all spec drift). Review findings 1-16 (8 of class A) fixed as class
  rules; keyword unpacking now scoped (FU-1, oracle 0 disagreements).
- Axum: axum 0.8.8 228 checks and 0.3.4 65 checks; one precision stop
  (Form on HEAD) fixed as a class rule. Review 9 A, 4 B fixed (serde
  attributes and renames, error-variant arms, version-dependent text).
- Express and Pages Router: Express 4 and 5, zod, Joi, celebrate,
  TypeScript; 278 requests, 0 disagreements (the reviewed build had 125).
  Review 2 A, 9 B, 2 C fixed.
- Next.js App Router: 14.1.0, 15.5.4, 16.2.10, identical counts. Review
  6 A, 3 B fixed; next.config rule lists and header-only rules read.
- Committed specs (mapped items): swagger-parser oracle over owncast,
  gotify, express, seerr, langflow (625 routes, every item): 0
  disagreements. Review 6 B/C fixed.
- Security schemes: 94 checks agree (accepted with credentials, refused
  without, open paths open). Review 1 A, 3 B, 1 C fixed.

**Explorer UI, route detail, editor icons (UI-A, UI-B, W1, W1b).**
Results: web suite and committed assets match a clean build; screens
checked at narrow widths. Revisions: 2 A (re-serialised response bodies,
project-mode production acknowledgement), 3 B, 5 C; all fixed.

**Model-type edges (W2), schema model routes (W3), shape differences
(W4).** Results: a changed model reaches its routes and their callers on
the fastapi template (4 routes) and gotify (1 route, a lower bound);
W3's absence is strict (`none` only when every route is fully read);
W4 compared 10 fastapi template links, 0 differences, all read correct
against the code. Revisions: W2 1 A (function-local Go types), 2 B;
W3 1 B, 2 C; W4 2 A (Pydantic 1 projects, serde `alias`); all fixed.

**Coverage per corpus (short).** FastAPI: template and langflow fully
placed; most fields fact. Go: gin/chi/net/http shapes partial (handlers
read, middleware not); dozzle and miniflux need one prefix value each.
Axum: four corpora, no hop needed. Express: request facts, few responses
(seerr's middleware replaces `res.json`). Next.js: umami's dynamic routes
still blocked by environment-built rewrites; ai-chatbot and umami
unplaced without a value. Committed specs: owncast, gotify, express,
seerr, langflow mapped.

**Assumptions shown in the view.** A value set by you (key, value and,
for a hop, the routes it covers); settings defaults and conditional
registrations; `shadowing: not_checked` and `may_be_shadowed`; sections
not read with their reason; mapped items labelled as from the spec;
"no differences" never shown, only differences between fully read sides.

**Known gaps** are those listed in the sections above.

**After v1.** Precision label, imported-operations list and compare view
(OX-8..OX-10); tags in `documented_by`; a stored root app per route for
per-server documents; body presence and JSON pointers in the shape
model; schema formats and composition; per-operation try-it history;
OAuth2 schemes with an unread token URL; Go/Rust/TypeORM models linked to
database tables.

# ADR 0024 — OpenAPI operation route links

**Status:** Accepted (developer review, 2026-10-18)
**Date:** 2026-10-18

## Context

Imported OpenAPI operations describe a declared API surface. They need source
route correspondence while retaining the distinction between a declaration
and an inferred implementation link. Client matching already defines method,
specificity, FastAPI slash, test scope and equivalent Express registration rules.

## Options considered

- Duplicate matching inside OpenAPI: selection behavior could diverge from clients.
- Turn operations into client_call nodes: changes identities and misrepresents declarations.
- Expose an exact-only adapter over the existing matcher: chosen.

## Decision

`internal/extract/clients.RouteMatcher` retains the existing matcher and Express
equivalence proofs. Its typed `ExactRequest`/`ExactResult` adapter supplies a
resolved base, so operations cannot take the client's unresolved-base suffix
fallback. The existing client matching implementation and call path are unchanged.
The adapter preserves additionalOperations method spelling, implicit HEAD,
specificity, candidate caps, scope filtering and duplicate selection.

Revision 1 filters Express and FastAPI candidates in this adapter whenever the
method contains an ASCII lowercase letter, before specificity or scope selection.
Node's [llhttp method parser](https://github.com/nodejs/llhttp/blob/main/src/llhttp/http.ts)
selects case-sensitive uppercase method tokens and rejects other spellings before
Express dispatch. Uvicorn's
[httptools protocol](https://github.com/Kludex/uvicorn/blob/main/uvicorn/protocols/http/httptools_impl.py)
uses HttpRequestParser, whose
[implementation](https://github.com/MagicStack/httptools/blob/master/httptools/parser/parser.pyx)
wraps llhttp, so the same restriction applies to FastAPI under that server.
ANY registrations cannot bypass parser rejection. Axum's hyper/http stack accepts
case-preserving extension methods ([Method implementation](https://github.com/hyperium/http/blob/master/src/method.rs));
its ANY routes remain eligible. Remaining candidates retain the existing
ambiguity, specificity and test-scope rules. Uppercase methods and all client
results are unchanged.

The index passes the current matcher and configuration to OpenAPI's link-time
pass after route composition. Each path entry gains status, nullable match,
capped sorted candidates and full tie count. Operation status is linked when
all emitted entries link; otherwise unresolved > unmatched > test_only > ambiguous.
Unresolved server templates are never matched. Emit one inferred documents_route
edge per distinct linked target, with the operation span as evidence. Confidence
is 0.9 without origins, 0.7 with origins; alternatives use the strongest confidence.

`openapi.base_paths` is a strict mapping of spec files or directories, including
`.`. The longest containing key replaces all server alternatives with one joined
configured path and no origins. Strict decoding reuses clients' base-map rules;
include/exclude retain their existing independent discovery behavior. Config
changes relink without parsing unchanged language files. No per-file OpenAPI
cache is introduced; aggregate extractor version remains extractors-14.

Emit one info openapi_coverage diagnostic for each emitted api_spec, including
empty specs, with operation counts by status. Existing unsupported versions
that emit no spec keep their unsupported-version diagnostic. Apply the YAML
column-zero prefilter only to YAML; JSON uses its existing byte-string test.

Contract v1 already permits the added attributes and edge kind. Node identities,
source hashes/spans, route/client attributes, storage schema/interface and
dependencies remain unchanged. Generation, export and query traversal through
documents_route are deferred.

## Consequences

API declarations expose coverage without claiming that inferred links are facts.
Existing precision limits remain visible as unmatched or ambiguous results.
The only existing golden change is the OpenAPI projection's added matching
attributes and coverage diagnostics; a separate namespaced fixture covers links.

## Known gaps

Uppercase methods outside a server's supported method list (for example FOO on
Node) are not filtered. The FastAPI rule models uvicorn's httptools parser;
alternative server/parser configurations are not inferred.

Repository-wide matching does not identify the application/deployment that owns
a spec: Langflow's two production /health routes remain ambiguous. Factory-returned
FastAPI router includes remain unresolved (ADR 0020 review notes). Existing client
matcher limits, including framework registration order and Express case sensitivity,
remain unchanged. Matching uses the existing emitted path entries (at most ten),
while path_count continues reporting the full declaration count. The source-hash
limitations for fragment and anchor edits remain those of ADR 0023. Impact/callers
traversal through documents_route is deferred to M5.

## Review notes (developer, 2026-10-18)

- Accepted. Express spec 19/19 exact at 0.7 (favorite/unfavorite to the first
  registrations); React + Express 38/38; React + Axum and React + Axum DDD
  19/19 at 0.7; React and RealWorld alone 19 unmatched each. Langflow 118 of
  121 operations link to source-verified targets: POST /api/v2/workflows is
  unmatched in both specs (factory-returned include, ADR 0020 note) and
  GET /health is ambiguous between two production applications. Zero wrong
  targets. Client results byte-identical on all 12 indexes.
- Langflow's FastAPI-generated spec doubles as an audit of route extraction:
  98% of its operations resolve to extracted routes.
- Revision 1: the task's rule let lowercase additionalOperations methods match
  Express ANY routes, which Node's llhttp parser rejects before dispatch.
  Lowercase methods now never match Express or FastAPI (uvicorn httptools)
  routes; Axum ANY still matches.
- The JSON prefilter fix cut the Langflow OpenAPI pass from about 591 ms to
  50 ms. Timing against main: fresh -1.56%, warm -0.77%.
- Gaps: uppercase methods outside a server's method list (FOO on Node) are not
  filtered; the FastAPI rule models uvicorn httptools only; which application
  owns a spec is not modeled (Langflow /health); impact/callers traversal
  through documents_route is deferred to M5.

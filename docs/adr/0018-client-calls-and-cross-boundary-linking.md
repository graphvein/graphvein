# ADR 0018 - Client calls and cross-boundary linking

**Status:** Accepted (developer review, 2026-10-11)
**Date:** 2026-10-11

## Context

T012a needs source-backed fetch/axios calls linked to current route paths, with
configuration-only relinking and conservative treatment of opaque URL builders.
Revision 2 rejects the false knowledge-base link documented in A9.

## Options considered

- Infer libraries from receiver names: rejected; scanned packages and lexical
  bindings can shadow transports.
- Discard every dynamic prefix and suffix-match: rejected by the deployment
  witness; helper results can contain resource segments.
- Evaluate arbitrary URL helpers: deferred to T012d.
- Cache detached syntax and recognize/link against the shared current resolver:
  chosen, with R9's bounded environment-base proof.

## Decision

Add level-4 source client_call nodes. Their span/hash is the call expression,
with nearest emitted named owner containment. Identity uses owner, uppercase
method (DYNAMIC when unknown), and compact URL source; repeated identities get
declaration-order @2/@3. Templates, bases and configuration do not affect identity.
Open contract-v1 attributes carry library, method, expression, origin, template,
base, status, match, wrapper and ambiguity data. Candidate lists are sorted and
capped at ten; candidate_count retains the full tie size. No transport version,
schema, store interface or dependency change is required.

Capture syntax in TypeScript route scopes, retaining client call arguments,
wrapper parameters and module defaults. Fetch requires a proven unbound global
or global-object member; axios requires the resolved external default export.
Follow bindings and one factory-return level. Wrapper calls reuse existing
callable edges, with three levels and repeated/cycle guards.

Apply URL-piece and base precedence rules in docs/extract/clients.md. Only
R9's environment members, their literal fallbacks, and one-level module const
initializers may supply an unresolved leading base. Other leading dynamic
expressions produce a null template. Parameter-only suffixes never match.
Strict clients.base_paths maps directory prefixes to explicit path bases,
longest prefix first, applying only to unresolved bases at relink time.

Compose routes first, then match by method, parameter segments and specificity.
Emit calls_route from client_call to route, provenance inferred, confidence
0.9 exact without literal origin, 0.7 exact with origin, 0.6 suffix. This amends
ADR 0013's downstream plan, whose source was the calling symbol. Uncalled
routes produce no diagnostics. Aggregate extraction version is extractors-11.

Impact follows outgoing exposes_route, incoming calls_route, incoming contains
from client_call to owner, then existing incoming calls/imports/references.
Route callers include calls and owners. Inferred results expose the minimum
confidence along their deterministic shortest path; fact-only results retain
their existing output.

## Consequences

Configuration changes relink without reparsing. Opaque helper-heavy Langflow
calls remain dynamic even when their runtime targets are known from source.
Source identity remains independent of deployment configuration. Cross-boundary
results retain the supporting edge evidence and inferred labels.

## Known gaps

No URL-builder evaluation, interceptor evaluation, arbitrary local dataflow,
Request/URL object evaluation, class-instance type inference or generated-client
resolution. Fetch method normalization uppercases custom verbs too; actual
fetch preserves nonstandard spellings such as lowercase patch. Slash-bounded
parameters may contain runtime slashes. Matching is case-sensitive even for
Express's default case-insensitive routes. Axios defaults ignore creation order.
Postgres and other operating systems require CI evidence; storage is unchanged.

## Review notes (developer, 2026-10-11)

- Accepted. Langflow: 5 calls link, all source-verified against backend
  registrations; zero false links. The A9 deployment-run witness is
  dynamic. Production link share is 1/179 by design until bounded
  URL-helper evaluation (T012d). React+Express and React+Axum composites
  are equivalent to separate indexes (F2). Multiset comparison shows no
  regression on any corpus; existing goldens change only the extractor
  version. Postgres CI shows only the four known helper skips.
- Suffix matching (0.6 tier) is now restricted to environment-shaped
  bases and has fixture coverage only; no corpus exercises it. Add a
  corpus with an import.meta.env or process.env base when one is found.
- A dynamic call (null path_template) can show base: configured, which
  will mislead in the UI. T012d should keep base unresolved when the
  template is null.
- Wrappers with several sends are excluded. Revisit with an explicit
  representation if a corpus shows them.
- Timing passed (+5.68% median) with very wide spread (branch samples
  138.7 s and 228.9 s). Protocol from now on: when sample spread exceeds
  the gate width, run three interleaved pairs instead of two.

## Amendment (T017a)

**Status:** Accepted (delegated review, 2026-11-03)

"Parameter-only suffix" means a suffix match in which no literal call segment
is matched by a literal route segment. On the call side, a call with no
literal segment never tries suffix matching. On the route side, a route
remainder (`route[at:]`, `at >= 1`) matches only when at least one of its
literal segments equals the literal call segment at the same position. A
remainder of parameters or a final catch-all alone never matches, because the
unknown base would be the only evidence. A literal call segment taken by a route
parameter or swallowed by a catch-all does not count. Exact matching, method
rules, specificity, ties, scopes and confidences are unchanged.

Review notes (T017a, delegated review 2026-11-03): the investigation found 0 changed
records on all ten public corpus checkouts, the self-index and all fixtures
(the private corpus not run); 18 existing suffix links all have a literal score of at
least 1 and stay. An independent review found no blocker or major issue; the
tests fail on the old condition. Known gaps: (1) an unresolved-base call whose
only exact candidate has no literal segment (route `/{id}` or `ANY /{*}`) still
links as exact at 0.9, and removing a parameter-only suffix candidate can turn a
former tie into such a link (0 occurrences today; follow-up T017b); (2) a
remainder that starts with a parameter can still suffix-match on one shared
literal (`/users/{id}/edit` from `${API}/profile/edit`), the same evidence level
as `/items` from `/api/items`.

## Amendment (T017b)

**Status:** Accepted (delegated review, 2026-11-04)

This extends the T017a principle to the exact tier and to the suffix
remainder's anchor. For a call whose base is `unresolved`, every accepted
candidate matches at least one literal call segment with a literal route
segment:

- Exact (Rule E): the match needs a score of at least 1, so a literal-free
  route (`/{id}`, `/{*}`, `/`, an optional final parameter omitted to `/`) is
  never a candidate. This applies whatever the origin, including
  `origin_resolution: unresolved` with an unresolved base (reviewer Revision
  0). Resolved bases (`none`, `literal`, `constant`, `configured`) keep such
  exact matches, because their path is known.
- Suffix (Rule P): besides a score of at least 1 (T017a), the remainder's
  first segment must be a literal, or a parameter matched by a call
  parameter. A remainder that starts with a route parameter or a non-final
  `{*}` while the call's first segment is a literal is rejected.
- Exact anchor (reviewer Revision 1): with an unresolved base, whatever the
  origin, an exact match needs the same anchor. A route whose first segment
  is a parameter is a candidate only when the call's first segment is a
  parameter, so `/{org}/settings` no longer links `${API}/account/settings`
  and `/{a}/{b}/edit` no longer links `${API}/profile/x/edit` at exact 0.9.
  The rejected variant may still match as an anchored suffix. Resolved bases
  are unchanged.

Reasoning. With an unknown base, a literal-free route has only the segment
count as evidence, plus the assumption that the base adds no segment; an
`ANY /{*}` fallback matches every path. A parameter-first remainder lets the
matcher place the base boundary so that the call's own leading literal
becomes a parameter value (`/users/{id}/edit` from `${API}/profile/edit`),
leaving one shared literal as evidence. Both now give `unmatched`, which has a
remedy (`clients.base_paths` makes the base `configured`). Rule E removes only
score-0 candidates, which can win only when no candidate scores 1 or more, so
it never creates or moves an edge; Rule P can let a lower-scoring
literal-first candidate win or turn a former tie into a link.

The investigation totals (T017b inventory): 0 changed records on the ten public corpus
checkouts and all fixtures (the private corpus not run). The self-index has 1 changed
record: a page-relative test call with template `/` whose three root-route
candidates from unrelated fixtures were `ambiguous` and are now `unmatched`
(Rule E, no edge). Rule P changed nothing (all 22 existing suffix links and 36
suffix candidates start with a literal). No edge is removed or added. The
contract, schema, extractor version and confidences are unchanged.

Revision 1 the investigation (T017b inventory): the exact anchor changes 0 records on the
ten corpus checkouts, both self-indexes and all fixtures; no unresolved-base
exact link or candidate there starts with a route parameter.

Known gaps:

1. A dynamic first call segment is a call parameter, so it still anchors a
   parameter-first remainder: `${API}/${x}/edit` links `/users/{id}/edit` as
   suffix 0.6, although `${x}` may be a prefix segment rather than the id.
2. Recall loss: a versioned route `/api/{version}/users` no longer matches
   `${API}/v1/users` (Rule P rejects the remainder `{version}/users`); the
   call is `unmatched`. The remedy is `clients.base_paths`: mapping the
   calling directory to the path base `/api` makes the base `configured`, and
   the full path `/api/v1/users` matches exactly.
3. `base` is merged per template across a call's URL alternatives
   (`internal/extract/clients/paths.go`, the `ev.bases` merge, where an
   unresolved base wins). An entry with an assignment-derived origin
   (`origin_resolution: unresolved`, base `none` on its own) can therefore be
   matched with base `unresolved` when another alternative has the same
   template and an unresolved base. That is how Revision 0's subset (base
   `unresolved` with `origin_resolution: unresolved`) occurs; the investigation found it
   on no input. Rule E and the exact anchor apply to it; the suffix tier does
   not (it needs an origin that is not `unresolved`).
4. Express 5 wildcards are not modelled: the normalizer turns `/*splat` into
   `/{*}splat`, which acts as a catch-all as a final segment (prefix `{*`)
   and as a literal that never matches elsewhere; `{...}` optional groups are
   not modelled. No input registers either form (the T017b investigation).

Review notes (T017b, delegated review 2026-11-04): the investigation found 1 changed record
(self-index `url-evaluation/frontend.ts:10`, page-relative `/`, ambiguous to
unmatched, no edge) and 0 on all ten public corpus checkouts and every fixture
(the private corpus not run). The independent review accepted Rules E and P and found one
wrong-link class (G1: a parameter-first exact match with an unresolved base
still linked at 0.9); Revision 1 extended the literal anchor to the exact tier
(0 changed records everywhere). A fresh re-check of Revision 1 with 14 extra
cases found no wrong link. Gates: corpus snapshots identical on all ten,
self-index only the listed record, cross-binary warm replay equal to cold,
Langflow interleaved timing fresh -0.5% and warm +0.3% (within noise), store
size unchanged (935,981,056 bytes). Known gaps as listed above.

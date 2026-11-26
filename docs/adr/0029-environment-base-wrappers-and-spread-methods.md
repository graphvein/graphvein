# ADR 0029 - Environment-base wrappers and spread methods

**Status:** Accepted (developer review, 2026-10-24)
**Date:** 2026-10-24

## Context

ADR 0018 permits a narrow environment-shaped leading URL base in direct calls,
but its wrapper recognizer requires every prefix piece to be literal. Fetch's
old method reader also ignores object spreads, incorrectly treating an unknown
spread without an own method as GET. Admitting these wrappers requires proving
the method separately at every invocation.

## Options considered

- Admit the URL shape while retaining GET for unknown spreads: risks false links.
- Reject every spread: loses methods from literal invocation options and proven
  const objects, including defaults.
- Retain ordered init syntax and compose methods at link time: chosen.

## Decision

Amend ADR 0018's fetch wrapper prefix rule to permit one leading environment base
under its existing direct-call proof, followed by literal/const pieces and the
URL parameter last. Transparent assertions do not affect proof. Freeze that base
as link-pass syntax before crossing files; preserve its request-site source
spelling. Existing URL normalization, slash boundary, configuration, matching
tiers and confidences remain unchanged. For new environment wrapper chains,
follow whole URL parameters between wrappers. A resource helper interpolating
an ID into its argument stays an ordinary request intent rather than hiding its
edge behind another wrapper. Existing literal-prefix wrapper behavior is kept.

Evaluate fetch object entries in source order. A later own method or proven
spread method replaces the previous value. Proven absence, null and undefined
spreads preserve it. Unknown spreads make it unknown until a later own method
overrides it. An absent or undefined method means GET. Preserve the existing
dynamic-method attributes and omit route edges when the method is unknown.
Axios configuration handling is unchanged.

An options-parameter spread binds a literal invocation object recursively.
Missing or unshadowed undefined arguments bind undefined when there is no
declared default. A default proven to be an object, null or undefined binds that
value; an unproven declared default stays unknown and cannot imply GET. Other
supplied options stay unknown, including a const identifier
at the invocation boundary. Preserve this distinction while forwarding through
the existing three-wrapper limit and repeated-group/cycle guards. Existing
options.method forwarding reads spread-aware own properties too. Constants in
the wrapper body are frozen in their defining lexical context. Defaults are
evaluated in the scope enclosing the function, before body or inner-block
declarations exist, with parameter references unknown (including earlier
parameters). Existing cached URL scopes express this distinction; no new cache
field is needed and extractors-19 is retained. Invocation object values are
evaluated in the caller's context.

Ordered cached entries in spread-containing objects retain each property's value,
including duplicate keys. Objects without spreads reuse their existing map of
last own properties rather than duplicating values in the cache. Wrapper
parameter defaults use the richer client syntax, and
parameter shadowing of undefined is retained. Invalidate aggregate caches with
extractors-19. Contract v1, source identity rules, dependencies, storage schema
and store interfaces are unchanged. A corrected dynamic method changes the
method component of that client's identity under the existing identity rule.

The empty environment base_expression came from evaluating an identifier through
its compacted initializer, which intentionally lacked source text. Keep the
identifier expression at the request site instead. Direct environment members
and fallbacks retain their own spelling; wrappers retain the spelling at their
fetch site. All-null paths continue omitting base_expression, and configured
bases continue clearing it: neither has an unresolved, usable leading base to
describe.
Whole-URL const aliases retain an empty base_expression when no separate base
expression is written at the request site. Their compacted initializers still
provide path evaluation; the complete URL alias is not mislabeled as a base.

## Consequences

Unknown options cannot create speculative GET links. Source-ordered duplicates
and spreads survive cache replay. Imported defaults relink unchanged callers.
The synthetic fixture and the public census record the behavior and existing
golden changes; private per-site evidence remains outside the repository.

## Known gaps

Direct nonliteral fetch init objects retain the existing dynamic boundary.
The existing bounded object/helper evaluator, module table mutation rules and
HTTP method normalization limits remain. General URL helpers, string member
calls, resource attributes/CSS and axios spread semantics are outside this change.

## Review notes (developer, 2026-10-24)

- Accepted. Private corpus gate: 44 request intents, 43 linked exact at 0.9,
  1 dynamic (URL helper), 0 wrong links; wrapper invocation methods
  GET/POST/PATCH/DELETE 15/15/4/7; 44 distinct routes linked. The PLAN
  criterion holds for fetch request intents; routes reached only through
  element attributes (img/video src, poster) and the dynamic URL helper
  remain backlog items.
- No existing calls_route edge changed in any corpus or the self-index; all
  41 fixed Langflow sample outcomes are unchanged. Precision corrections
  from GET to unknown: one React inner fetch, nine Orval fixture inner
  fetches and one private inner fetch; none had edges. Their identities
  change in the method component under the existing identity rule.
- Revision 1: spread-referenced option defaults that were not proven were
  bound as empty and produced a speculative GET link; they are now unknown.
  Defaults are evaluated in the scope enclosing the function, so body
  declarations never bind them (the baseline produced a linked GET for a
  body-shadowed default). Primitive defaults are conservatively unknown.
- Gaps: a const options identifier at an invocation stays dynamic while the
  same const spread into a literal resolves; new environment wrapper chains
  keep URL-composing resource helpers as request intents while literal-prefix
  chains still treat them as wrappers; 373 Langflow whole-URL alias sites
  keep an empty base_expression (omission would be cleaner); identity
  compaction drops whitespace between identifier tokens
  (`(process.env.APIasstring)`), inherited from ADR 0018 and affecting
  identities, to fix before M4 persists dispositions.
- Timing against main, three interleaved pairs (medians): fresh index -0.65%,
  warm index -3.07%, ingest within +0.12%. Revision 1 is link-time only and
  changed no cached syntax.

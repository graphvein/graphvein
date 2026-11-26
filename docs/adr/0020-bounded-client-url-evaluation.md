# ADR 0020 - Bounded client URL evaluation

**Status:** Accepted (developer review, 2026-10-12)
**Date:** 2026-10-12

## Context

T012d requires source-backed URL builders, tables and constants without executing
repository code. Opaque prefixes must not lose resource segments. FastAPI slash
twins already retain their source path flags.

## Options considered

- Execute helpers: violates deterministic offline source inspection.
- Drop unknown prefixes: can link the wrong resource.
- Cache resolved URLs: becomes stale when imported tables change.
- Cache bounded syntax and evaluate against current declarations: chosen.

## Decision

Keep client evaluation syntax separate from route factory syntax. Cache lexical
bindings, object property order/spans and eligible emitted helper bodies. Use the
shared TypeScript resolver at link time. Follow module constants and single local
const declarators under lexical/source-order rules; reject mutated module tables.

Evaluate ECMAScript values, coercion, defaults, short circuiting and conditional
alternatives within T012d B1-B6. Helpers admit three levels and 2,000 steps per
site; recursion and unsupported statements remain dynamic. Memoize only within
a link pass; keys include depth, values, property order and literal-object origin.

Normalize alternatives under existing piece/base rules and deduplicate on
template plus slash flag, with at most four entries. Nodes expose paths and
capped sorted evaluated-helper identities. Node status takes the worst entry,
except existing wrapper sites. Null templates retain unresolved bases despite
configuration. Emit one inferred calls_route edge per linked target at the
strongest existing tier, with call, property and helper-return evidence.

Slash filtering applies only to equal-template FastAPI ties containing both
matching and differing flags. Preserve existing method/specificity rules and
Express ambiguity. Source identities, contract v1, store interfaces/schema and
dependencies are unchanged. Invalidate syntax caches with extractors-13.

## Consequences

Editing a table relinks unchanged callers without reparsing them. Runtime
conditions remain explicit alternatives; unsupported code remains visible as a
gap. Derived attributes do not change source identities or hashes.

## Known gaps

URL/Request objects, call-site let/var dataflow, nonliteral object arguments in
loops, async helpers and general execution are excluded. Existing framework
route-order/convertor, Express case, axios creation-order/interceptor and
multi-segment dynamic-piece limits remain outside the stated rules.

## Review notes (developer, 2026-10-12)

- Accepted. Langflow production links rise from 1/179 to 156/179 (87.2%);
  full scan 161/594. Production: 4 ambiguous, 6 unmatched, 13 dynamic,
  16 conditional calls. Fixed sample: zero false linked entries; every
  in-scope known target links, including the A9 deployment-run witness
  (now an exact link to its real deployments route); both conditional
  cancel alternatives link to source-proven targets. The six earlier
  Langflow links keep their targets. Other corpora identical apart from
  the header; React keeps its two dynamic calls.
- All four production ambiguous calls tie a production route with a
  route from a test FastAPI app
  (src/backend/tests/unit/services/tracing/test_otel_fastapi_patch.py:
  GET /flows/{flow_id}, GET /version). Test-app roots (ADR 0019 note) now
  cost real links; a test-scope task is scheduled next (T011g).
- POST /api/v2/workflows (run-flow-bridge.ts:695) is unmatched because
  its router comes from create_workflow_router(...) included as a call
  result in api/router.py. FastAPI includes of factory-returned routers
  are a backlog item.
- The investigation's full-census forecast missed one interpolated-authority case
  (mcp-server.spec.ts:144, null template); disclosed, and the production
  forecast was exact.
- The investigation predictions printed internal piece markers (U+0001/U+0002) in
  origin values for interpolated hosts. Fixture and repo goldens contain
  none; the next client task asserts no control characters in any client
  attribute across the corpus.
- Timing against post-T011f main: fresh +0.92%, warm +4.35%.
- Gaps: URL/Request objects, call-site let/var flow, multi-segment
  dynamic pieces, nonliteral loop objects, async helpers.

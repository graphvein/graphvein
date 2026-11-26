# ADR 0021 - Test scope at composition and matching time

**Status:** Accepted (developer review, 2026-10-12)
**Date:** 2026-10-12

## Context

Test FastAPI apps tie production Langflow registrations, blocking four real
client links. Helpers also contribute paths production never serves. The
original the T011g investigation found that preferring production for test calls creates
33 wrong self-index fixture links; Revision 1 limits scope filtering to
production calls and accepts the parsing sample's source-consistent link.

## Options considered

- Remove test files during scanning: loses graph facts and test callers.
- Cache scope in per-file syntax: requires cache invalidation on config edits.
- Prefer production for every call: rejected by the investigation precision stop.
- Classify paths and compose scope on every link pass: chosen.

## Decision

Add internal/testscope with a pure Classify(path, Rules) function. Rules are
validated once by strict tests.include/exclude decoding; the zero value uses
only built-ins. Implement the specified case-sensitive gitignore subset using
standard-library segment matching and whole-segment ** traversal. The scanner
and dependencies stay unchanged. Exclude wins over includes and built-ins.

Pass current rules to route and client linkers through optional rule arguments,
preserving callers that use built-ins. Each composition chain combines test
scope from root definitions, composition sites, contributing FastAPI helper
bodies and invocation sites, and registrations. Merged paths remain production
when any chain is production; slash flags remain distinct. Prefix values do not
carry scope. Route registrations and client invocation sites receive scope=test
independently of composed paths and wrapper implementations.

Apply existing method, specificity and slash-twin selection first. Production
calls drop test-only entries in a mixed winning tier; entirely test-only tiers
produce test_only with no edge and no lower-tier fallback. Test calls ignore
scope entirely. Node status order is dynamic, unmatched, test_only, ambiguous,
then linked; inner wrapper sites retain wrapper status. Candidates remain capped
at ten with their full ambiguous count. Queries continue traversing test calls.

Scope is derived graph data in extensible contract-v1 attributes. No cached
per-file syntax, identity, span, hash, transport version, store interface/schema
or dependency changes. Aggregate extractor version remains extractors-13.

## Consequences

Configuration-only edits relink cached syntax without parsing unchanged files.
Test routes/calls remain available to impact and callers. Production scope
means path classification, not proof of deployment: documentation and parsing
samples deliberately remain production-scope unless configured otherwise.

## Known gaps

Rust cfg(test) module bodies, Cargo examples/benches and documentation examples
are outside built-in classification. Gitignore negation and backslash escapes
are rejected. Scope cannot distinguish unrelated fixture repositories sharing
URLs; preserving baseline matching for test calls prevents scope from creating
wrong confident links between them. Other route and client precision limits
remain those of ADRs 0013, 0016, 0018, 0019 and 0020.

## Review notes (developer, 2026-10-12)

- Accepted. Langflow production links rise from 156/179 to 160/179
  (89.4%); full scan 165 linked, 0 ambiguous, 6 unmatched, 423 dynamic.
  The four former ambiguous calls link exact 0.9 to flows.py
  GET /{flow_id} (three calls) and endpoints.py GET /version. The six
  /model-provider-policy entries are test-scope; the /api/v1 entries are
  unchanged. Zero production test_only calls on any corpus; zero false
  links in the fixed sample; the six other external corpora are
  identical apart from attributes.
- The investigation stopped on a task error: production-first tie-breaking for
  test calls would have linked 33 fixture calls to the unrelated Rust
  parsing sample. Revision 1 limited scope to production calls.
- Self-index: spike/parsing/samples/items.tsx:5 now links to the
  items.rs sample route. Samples are production-scope by classification;
  tests.include can reclassify them.
- Fully interpolated authorities (http://${host}:${port}/path) keep a
  partial origin/path and report unmatched; a coincidental template
  match would be a confident wrong link. T012b adds a fixture and makes
  such calls dynamic with a null template. The dynamic-port case shows
  origin "http://localhost:"; revisit when the UI displays origins.
- Timing against main: fresh -1.84%, warm -1.25%.
- Gaps: Rust cfg(test) bodies, Cargo examples/benches and documentation
  examples are unclassified; unrelated fixture repositories sharing URLs
  stay ambiguous in the self-index (multi-repository identity, M4).
- Review fixed the status vocabulary in docs/extract/clients.md.

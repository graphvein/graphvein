# ADR 0033 — Angular compilation scopes and template matching

**Status:** Accepted (developer review, 2026-10-27)
**Date:** 2026-10-26

## Context

T014c3 consumes ADR 0030 syntax and ADR 0031 declaration leaves to prove
host-template references without changing cached syntax or historical edges.
Revision 1 replaces the original unknown-block exclusion, which the cache
could not implement, with conservative recovery and orphan-block rules.

## Options considered

- Add block syntax or reparse templates: prohibited by this task and unnecessary
  for the revised conservative rule.
- Treat every recovered element as matchable: can emit edges for children that
  Angular drops under an unknown block.
- Rebuild scopes and match the verified selector subset at link time, with
  explicit uncertainty and conservative recovery suppression: chosen.

## Decision

Reuse post-duplicate declaration edges and partitioned leaf counts. Recursive
NgModule compilation/export scopes propagate open, unknown and invalid status;
cycles poison scopes. Standalone scopes include self and imports; declared
components use their single declaring module. Missing standalone/declaration
proof gives none. deferredImports gives unknown.

Parse only T014c3 S1 compounds and comma lists. Unsupported selectors are
diagnosed once per class and make nonmatches unknown. Match static raw names,
then input and output names in Angular's overwrite order. Structural wrappers
use only the first structural key; secondary microsyntax stays unknown.
Validate each satisfying source span against recovery. Multiple matching
components suppress their component edges while preserving directive edges.

Emit fact renders and uses_pipe edges at confidence 1 with template evidence;
inline evidence uses mapped host coordinates. Count each matching usage/class
pair before edge merging. Add partitioned element, structural and pipe counters,
scope status and recovery count under angular.template_matching. Pipe matches
in open scopes are shadowable and emit no edge. No external targets are added.

R1a classifies recovery wholly contained in a captured non-dropped attribute
value or block parameter as expression recovery. Other recovery suppresses
every usage starting at or after the earliest structural recovery. R1b uses
cached block ancestry and source gaps containing only whitespace and complete
HTML comments to identify orphan connected blocks; their contents are unknown.
case/default require a switch parent. R1c diagnoses the earliest structural
recovery or orphan block once per host, naming its line and cause.

C2 rejects dot-prefixed resource URLs other than ./ and ../ as unresolved
instead of joining them as relative paths. Existing rooted, outside-repository
and backslash guards retain precedence. Link's public signature is unchanged.
No contract, store, schema, dependency, grammar or cache-version change occurs.

## Consequences

Scopes and references relink on unchanged-source replay after imports,
configuration or template changes. Historical declaration/injection facts stay
intact. Shared backend tests register the matching fixture for SQLite locally
and Postgres in CI. Corpus and timing evidence is in the T014c3 task record (internal).

## Known gaps

- Structural recovery, including text-interpolation errors, suppresses later
  siblings as well as malformed content. This loses coverage conservatively.
- Repeated else and wrongly ordered connected blocks are not diagnosed.
- Secondary microsyntax keys, class/:not selectors, binding subtypes and pipe
  provider precedence remain outside the verified subset.
- This rule relies on unknown-block headers producing structural recovery
  covering @; the required production-grammar probes are recorded in the task
  summary. No new block parser is introduced.

## Review notes (developer, 2026-10-27)

- Accepted. Corpus results (all match the T014c3 expectations):
  - angular: 3 complete and 15 open scopes; custom elements 25 (23 linked,
    2 external_scope); structural 5 (4 linked, 1 unknown: home *rxLet,
    expression recovery); pipes 13 (6 shadowable, 7 external_scope);
    27 match pairs; 20 renders edges; 0 uses_pipe.
  - angular-ngmodule: 18 open scopes; custom elements 22 (20 linked,
    2 external_scope); structural 18 (5 linked, 13 external_scope);
    pipes 5 (1 shadowable, 4 external_scope); 25 match pairs; 20 renders
    edges; 0 uses_pipe.
  - All 40 renders edges checked against selectors, imports and module
    exports; no wrong target. 0 template diagnostics in both corpora.
  - The other eight corpora are byte-identical apart from the header; the
    Angular + Express composite keeps 24 clients (21 linked, 3 wrappers).
- Revision 1: the first run stopped at P4 because the template cache keeps no
  unknown-named blocks. R1a/R1b derive the same exclusion at link time from
  recovery spans and source gaps. All four corpus recovery spans are
  expression recovery; no orphan blocks.
- Timing (three interleaved Langflow pairs, medians): fresh index -0.55%,
  fresh ingest +2.66%, warm index -0.36%, warm ingest +2.36%. The Langflow
  store size is identical.
- Postgres CI: TestAngularDeclarationsRoundTrip/angular-template-matching
  passes; only the four known helper skips.
- Conservative deviations (coverage only, no wrong facts):
  - Dotted, colon or @ attribute names make every non-match on that element
    unknown; N3 required this only when a candidate could be satisfied.
  - An export of a class missing from a module's compilation scope is invalid
    even when that scope is unknown (an unresolved import could supply it);
    the label should be unknown.
  - The element that carries ngNonBindable uses raw static names like its
    descendants, because the cache flags both.
  - A class declared by a production module and a test module has scope none.
  - Pipe matches in open scopes are shadowable until pipe-name precedence is
    verified from annotations/component/src/handler.ts (2026-10-27).
  - Dropped stylesheet links are never matched; Angular keeps non-resolvable
    stylesheet links as elements.
- Gaps:
  - T014b capture: a valueless attribute directly before "/>" (<app-x dir/>)
    caches the raw name "dir/", losing its matches. Fix with the next
    template-cache change.
  - Errors in text interpolations count as structural recovery and make the
    rest of the template unknown.
  - Repeated @else and connected-branch ordering are not diagnosed.
  - Template scope and structure diagnostics on external templates are filed
    against the HTML file; the message names the host.

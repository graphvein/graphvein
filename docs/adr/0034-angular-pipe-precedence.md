# ADR 0034 - Angular pipe-name precedence in open scopes

**Status:** Accepted (developer review, 2026-10-27)
**Date:** 2026-10-27

## Context

ADR 0033 suppresses all local pipe edges in open scopes. T014c3b verifies
Angular's dependency order to identify the subset that external contributors
cannot shadow. This changes only pipe counters, matching usage counts and
uses_pipe edges. Contract v1 and extractors-22 remain unchanged.

## Sources and verified order

Read the supplied offline bundles only; no sources were fetched. References
below are one-based bundle lines, including section headers:

- `angular-t014c3b-scope-sources.txt` (a local offline bundle): standalone.ts,
  `StandaloneComponentScopeReader.getScopeForComponent`, v21.2.12 lines
  30-101 and 15.2.3 lines 989-1053. O-S: self first, imports in flattened
  source order, NgModule exports expanded at their import position. The seen
  declaration set retains the first occurrence, including expanded exports.
- The same bundle: local.ts,
  `LocalModuleScopeRegistry.getScopeOfModuleReference`, v21 lines 399-675,
  v15 lines 1293-1530. O-M: imported exported scopes and standalone pipes
  first, declarations last. Maps keyed by declaration preserve first insertion
  position even when their value is replaced (v21 435, 487-491, 524, 573;
  v15 1319, 1363-1367, 1394, 1441).
- V3: that function processes exports in source order, expanding a re-exported
  module at its export position and retaining first class insertion
  (v21 598-653; v15 1463-1512). Directives and pipes are concatenated separately
  in both output scopes; only relative pipe order matters here.
- `angular-t014d-sources.txt` (a local offline bundle): handler.ts,
  `ComponentDecoratorHandler.resolveComponentDependencies`, v21 lines
  1675-1734; v15 `resolve` reconstructed from removed diff lines 7297-7369.
  H1: the last dependency with a pipe name wins via `pipes.set`. V21 skips
  null names. H2: `createMatcherFromScope`, lines 2507-2535, adds every
  non-null directive selector; v15 does this inside `resolve`, diff lines
  7357-7369. Directive behavior is unchanged.
- H3 correction: v21 lines 1773-1784 construct
  `[...explicitlyDeferredDependencies, ...dependencies]` and iterate it into
  the already eager-populated pipe map. Eager dependencies therefore run
  after deferred ones in this pass, contrary to the task's H3 wording.
  deferredImports remains unknown in Graphvein, so this has no effect on P.
- H4: selectorless scopes use local names, v21 lines 1718-1725. They remain
  outside the modeled subset.

## Options considered

- Keep every open-scope pipe shadowable: safe but loses proven coverage.
- Model external declarations or change cached syntax: unnecessary and outside
  this task's scope.
- Retain ordered local/external units during existing link-time flattening and
  prove precedence conservatively: chosen.

## Decision

Replace only ADR 0033's blanket open-scope pipe suppression with rule P.
Capture successful import leaves during the ADR 0031 array traversal, before
edge merging. Local repeated leaves keep their first unit; external leaves
are never deduplicated, even when they share a package identity. A standalone
host uses its ordered imports. A declared host uses its declaring module's
imports followed by one declaration unit. These units are transient, never
serialized or cached.

An external unit is open. A local NgModule unit uses the status and local
classes of its exported scope. Other local leaves are closed singleton units.
Unknown/invalid/none scopes retain their previous suppression.

In an open scope, a usage links only with exactly one matching local pipe,
no local pipe with missing/unknown name, a closed first contributing unit,
and every open unit strictly before that unit. The first contribution can
come from an NgModule export; a later direct import cannot move it. The
existing complete-scope rule, invalid contexts, flagged pipes and recovery
handling retain their precedence in classification.

## Consequences

The five standalone corpus defaultImage usages and the declared corpus
markdown usage link with fact/confidence 1 and pipe-name evidence. Standalone
article markdown stays shadowable. Scope status, selector matching, declaration
facts, diagnostics, identities, cache payloads and versions are unchanged.
Fixture tests cover source-order expansion, shared external package targets,
duplicate contributions, both Angular defaults and cached replay/import edits.

## Known gaps

Open local NgModule units are deliberately opaque even when an internal export
order could prove their local pipe wins. Multiple local pipes of the same name
keep the prior classification; no winner is chosen between them. Missing names
retain uncertainty. Deferred and selectorless scopes remain unmodeled. External
leaf kinds are not resolved, so even an external directive is conservatively
treated as an open unit.

## Review notes (developer, 2026-10-27)

- Accepted. Results:
  - angular: pipes 13 = 5 linked (defaultImage in header, article-comment,
    article-meta, article and profile), 1 shadowable (article markdown, followed
    by FormsModule and ReactiveFormsModule), 7 external_scope; matches 27 -> 32;
    5 uses_pipe edges; renders unchanged (20).
  - angular-ngmodule: pipes 5 = 1 linked (markdown, declared after the article
    module's imports), 4 external_scope; matches 25 -> 26; 1 uses_pipe edge.
  - The other eight corpora are byte-identical including the header. The
    self-index changes only in the new fixture and the existing Open host
    (CommonModule precedes LocalPipe).
- Order models O-S (standalone.ts) and O-M (local.ts) verified from the
  v21.2.12 and 15.2.3 sources; handler.ts last-wins verified in both versions.
- The task's H3 wording was reversed: v21's second pass iterates deferred then
  eager dependencies into the eager-populated map, so eager pipes are set last.
  No output effect while deferredImports scopes stay unknown.
- Timing (two interleaved pairs per Angular corpus): every phase within noise
  (largest +3% on sub-second medians).
- Gaps: open local NgModule units are opaque even when their internal export
  order would prove the local pipe; duplicate local names choose no winner;
  selectorless and deferred scopes are unmodelled; external leaves of any kind
  are open units.

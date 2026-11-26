# ADR 0030 — Angular template syntax cache

**Status:** Accepted (developer review, 2026-10-25)
**Date:** 2026-10-24

## Context

T014b needs source syntax for later Angular declaration, template and router
linking. ADR 0028 currently caches only HTML recovery locations. ADR 0006
requires content/version keyed cache replay; ADR 0012 bounds detached trees.
Syntax capture must not change existing graph records or diagnostic behavior.

## Options considered

- Parse inline bodies inside each host worker: requires a new worker API and
  careful accounting for simultaneous host/guest detached roots.
- Parse inline bodies recursively in the host consumer: adds a detached guest
  while the original pipeline can already be at its tree limit.
- Capture host metadata/ranges, finish the host pipeline, then run a second
  bounded Angular pass (chosen): retains ordinary syntax and sources, not host
  trees; reuses the existing registered grammar and shared compilation engine.

## Decision

HTML cache entries contain error spans, elements, attributes, blocks and pipes.
Every array is required; empty arrays are valid and absent/null arrays miss.
TypeScript/TSX/JavaScript cache entries embed their unchanged shared facts and
add a `template_syntax` payload containing class decorators and inline results.
This avoids extending the shared facts package or changing linkers. Preserve
malformed graph-record rejection even when additive syntax is incomplete.

Capture every identifier/member-chain class decorator call without proving an
Angular import. Retain class identity, callee syntax/location, argument count,
explicit object-literal metadata proof (so `{}` differs from a non-object),
ordered own metadata entries and raw value syntax using the existing client
URL expression form. Spreads/computed keys remain explicit uncertainty.
Do not evaluate selectors, exportAs, standalone, jit or router metadata here.

After host `Consume` returns, every host worker has joined and every detached
host tree has been released. A second `Consume` parses only admissible literal
body slices through the registered Angular grammar. Both passes independently
retain ADR 0012's `2 * workers + 1` maximum; they never overlap. There is no
additional detached guest tree per host worker. Source slices and cached facts
still scale with repository size, as before. No parser API change is needed.

Each guest job uses the existing independent deadline, panic recovery, worker
retirement and cleanup behavior. Capture panics are recovered per template.
Guest failure reasons are persisted with the host facts but make the syntax
payload ineligible for reuse, so a later run retries it without losing
its graph facts or failing another template/file. Inadmissible literals cache
only their reason and host location. No new diagnostics are emitted.

Use LF-normalized UTF-8 source and B2's constant byte offset mapping. Guest line
one adds the content-start column; later lines keep byte columns. Every span,
including error/name/value/parameter spans, moves by the same rule.

Attribute classification follows A1 rather than grammar type names, including
data- prefixes and interpolated static values. Grammar-delimited source ranges
retain exact spelling. Quote-aware lexical scanning within expression-bearing
ranges locates pipes, excluding `||` and quoted strings, including recovered
expressions. All intersecting captured items are flagged. The full error-span
array remains authoritative uncertainty: absence of a pipe inside recovery is
never evidence that the expression uses no pipe. Blocks retain source order,
raw header spelling, raw parameter segments and ancestry; connection/grouping
and semantic evaluation belong to T014c/T014d. Non-bindable descendants remain
static and dropped script/style/stylesheet elements are retained with flags.

Bump aggregate cache semantics to `extractors-20`. Keep contract v1, store
interface/schema, dependencies, grammar bytes, runtime pin, compilation-cache
namespace and all existing graph identities unchanged.

## Consequences

Inline guest modules are created in the second pass rather than borrowing a
host worker's mutable runtime. Compilation is shared, but instantiation incurs
additional cold work. A no-op replay runs neither pass. The new payloads consume
additional store space; measurements are in the T014b task record (internal).

## Known gaps

- Angular grammar recoveries remain, including microsyntax, postfix `!` and
  `@default never`. Consumers must use recovery spans conservatively.
- Syntax is not Angular import proof, directive matching, grouping, ownership,
  expression evaluation or a new source of graph facts.
- Substitution/escape/nonliteral/spread/computed forms remain inadmissible.
- Local execution is Windows-only; backend/cross-platform limitations and
  performance evidence are reported in the task summary.

## Review notes (developer, 2026-10-25)

- Accepted. Snapshot bodies of all nine corpora and the self-index are
  byte-identical to main apart from the header; warm replay parses zero files.
  Angular corpus: 11 HTML files, 8 admissible inline bodies, 266 elements,
  387 attributes, 13 pipes, 4 recovery spans; class decorator callees
  Component 18, Directive 1, Pipe 2, Injectable 6. Inline profile error spans
  map to host 12:[61,62) and 12:[62,63).
- The task's S4 reference (T014a census) had no inline rows; ADR 0028's
  the investigation diagnosis was used instead (task error, not a data gap).
- Two sequential Consume passes keep ADR 0012's bound; guest failures are
  retried on the next run. Postgres CI: TestAngularTemplateSyntaxCache passes.
- Timing, two interleaved pairs on Langflow (medians): fresh index -1.44%,
  fresh ingest +2.85%, warm index +2.43%, warm ingest +0.51%; store +151,552
  bytes (+0.017%).
- For T014c: Angular keeps raw names on static attributes (only bindings are
  data- normalized), so matching uses raw_name for static attributes;
  structural microsyntax keys (ngForOf etc.) are not derived yet; pipes are
  captured in event bindings although Angular rejects them; a panic in host
  TypeScript decorator capture is silent and forces a reparse on every run
  (add a diagnostic); `@default never` recovers as an error span.

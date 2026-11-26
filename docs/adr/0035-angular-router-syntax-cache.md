# ADR 0035 — Angular router syntax cache

**Status:** Accepted (delegated review, 2026-11-01)

## Context

T014d1's census found all 28 corpus route objects in existing declaration
constants. Existing client calls retain provideRouter's scope but omit the
seven top-level decorator registrations. Bootstrap chains share start offsets.
T014d2 needs registration evidence, static loader targets and missing array
forms without changing historical payloads or graph records.

## Options considered

- Extend historical declaration/client fields: risks changing existing consumers.
- Resolve routing while parsing: caches decisions that become stale after
  import/configuration edits and exceeds this task's cache-only scope.
- Add separate, versioned source-only router syntax: selected.

## Decision

Add router beside template_syntax, declarations and injections in the TypeScript
cache envelope. Version 1 requires calls, loaders and arrays, including empty
arrays. Incomplete, missing or obsolete payloads miss. Invalidate aggregate
caches with extractors-23; contract v1 is unchanged.

Capture only the filters and forms in [Angular router syntax](../extract/angular-router.md).
Compact callee references omit duplicated text and arguments. First arguments
use DeclarationExpr; bootstrap receivers retain syntax and their called callee.
Retain default-export literal arrays and module const arrays wrapped in
satisfies only when absent from DeclarationSyntax.Constants.

Reuse an existing scope after matching full call span and callee and checking
relevant source binding agreement. Otherwise keep module scope plus sorted
source-only shadow names. For calls, relevant names include the callee root and
every identifier and shorthand property identifier in the first argument subtree,
excluding property keys, member property names and type nodes. Both shadow
intersection and cached-scope reuse use this set; conservative over-inclusion
keeps resolution unresolved. Sync loaders retain root-only shadows. Binding patterns
exclude parameter default expressions; blocks, loop headers and catch clauses
are included. Sync loaders include callback parameters. Earlier scopes and
payloads are never modified.

Relevant local shadows always use the fallback: older scopes have no declaration
spans to prove the nearest binding among multiple same-name locals. Scope roots
follow called receivers and subscripts as well as plain member chains. Type
annotations on an identifier then-callback parameter do not change its binding.

## Consequences

The new fixture has a router-only golden, independent assertions and shared
backend round-trip coverage. Postgres runs only in CI. No dependencies, schema,
store interface, historical payload, node, edge, diagnostic or identity changes
are introduced. Framework proof and routing graph output remain T014d2 work.

## Known gaps

Async/generator functions, dynamic/escaped specifiers, unsupported then callbacks
and multi-statement bodies remain explicit syntax gaps. Capture is not Angular
proof; consumers must reject unrelated APIs and local shadows. provideRoutes,
raw ROUTES providers and resetConfig remain outside capture and are counted by
grep in the census.

## Review notes (delegated review, 2026-11-01)

- Accepted after Revision 1. Results:
  - Router payloads: angular 1 provideRouter call and 11 import_default
    loaders; angular-ngmodule 8 calls (1 forRoot, 6 forChild,
    1 bootstrapModule) and 4 import_member loaders; no other corpus has router
    calls, loaders or extra arrays. The self-index's three extra arrays are the
    router fixture's default and checked arrays and the angular-declarations
    fixture's `SAT` satisfies const.
  - The investigation census (the T014d1 task record, internal): all 28 corpus route objects live in
    existing declaration constants. Every path evaluates exactly. Every
    component, guard and resolver target resolves; the local requireAuth
    const arrow resolves through a file+name node lookup. Every loader
    resolves; the profile.routes default export resolves through its routes
    const.
  - The seven NgModule registrations already produce scope_imports edges to
    the @angular/router package node; router facts will be additive.
  - Snapshot bodies of all ten checkouts and the self-index are identical
    apart from the version header; historical payloads are unchanged.
  - Langflow (two interleaved pairs, medians): fresh index +0.71%, fresh
    ingest -2.50%, warm index -2.63%, warm ingest -3.46%; fresh store
    +0.0193%.
  - Postgres CI: TestAngularRouterSyntaxRoundTrip/angular-router-syntax/postgres
    passes on the original commit and on Revision 1; only the four helper
    skips.
- Revision 1: call shadows cover every identifier and shorthand property in
  the first argument. A local declaration between an inline route array and
  module scope leaves the identifier unresolved instead of resolving the
  module binding.
- Verified for T014d2 (router.ts, v21.2.12 and 15.2.3): the Router reads
  `inject(ROUTES, {optional: true})` flattened, then resetConfig runs
  validateConfig (dev mode) and standardizeConfig. Every ROUTES multi provider
  of the root environment injector contributes; order only affects
  first-match precedence.
- Gaps:
  - Method-form loaders (`loadComponent() { ... }`) produce no loader record;
    consumers see a method entry and must treat the object as partial.
  - Array capture is syntactic: any default-export literal array or
    satisfies-wrapped module const array is cached, routes or not.
  - Compact callee references keep names only; consumers prove them with
    target(), never by text.
  - provideRoutes, raw ROUTES providers, resetConfig and importProvidersFrom
    are not captured.

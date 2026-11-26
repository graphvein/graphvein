# ADR 0032 — Angular requested-token injection facts

**Status:** Accepted (developer review, 2026-10-26)
**Date:** 2026-10-25

## Context

T014c2 requires requested-token facts independently of the HttpClient receiver
proof in ADR 0022. Constructor dependencies include plain parameters; types
without runtime values must never become edges. Provider changes must relink
unchanged sources. The investigation census and results are in the T014c2 task record (internal).

## Options considered

- Expand the receiver cache: changes historical field meanings and risks client
  and wrapper regressions.
- Resolve injections while parsing: caches package/provider decisions and makes
  configuration-only edits stale.
- Separate versioned syntax and link-time DI proof: chosen.

## Decision

Add a versioned `injections` payload beside `declarations`, invalidate aggregate
cache entries with extractors-22, and require complete payload arrays on replay.
Keep constructor implementation presence, extends, original parameter type
grammar, direct decorator syntax, type-only bindings, single module const new
initializers and own injection contexts. Preserve binding-pattern parameter
names separately: the historical receiver scopes also traverse default
expressions, which can introduce false parameter shadows. A private proof view
removes those false shadows only for the new consumer. Existing facts are never
mutated, and the original client resolver keeps its prior behavior.

At link time, eligible Angular classes receive partitioned leaf counts. Emit
fact injects edges at confidence 1 to scanned class declarations or existing
package external nodes, merging evidence without merging leaf counts. Function
owners receive edges only for their own non-nested calls outside class bodies.
Ignore static fields, methods, anonymous callbacks and nested functions.

Parameter decorators follow direct @angular/core specifier proof. Runtime inject
and forwardRef follow the unchanged shared resolver, including barrels. Capture
runtime sites only for named imports whose imported name is inject (including
aliases), or namespace.inject, from any specifier. Retain only the callee and
first argument, without duplicate whole-call syntax or options. Retain parameter
scope corrections only for ancestors of captured sites or recorded classes.
Type-only
imports, unavailable type syntax and non-class targets remain unresolved.
InjectionToken constants and Attribute/HostAttributeToken requests have counts
without speculative targets.

Expose ADR 0022's existing production override lookup with provider evidence;
supplement eligible Injectable own useClass/useFactory/useValue/useExisting keys.
Keep requested-token edges even when overridden. Diagnose each source/token
once, while counting each overridden leaf. Reuse ADR 0021 test classification.

C1 requires a single Injectable argument to be an object literal itself. Wrapped
or referenced objects make metadata invalid, including combined decorators.

## Consequences

Contract v1, identities, dependencies, storage interfaces/schema, source node
hashes and all existing client/wrapper behavior remain unchanged. Configuration,
package and provider edits relink on cached replay. Cache size increases for the
additional syntax; timings and store sizes are recorded in the task summary.
SQLite and Postgres round-trip tests include the new fixture.

Revision 1 uses injection payload version 2, keeping aggregate extractors-22.
The Angular Link API receives the injection syntax map as an ordinary parameter.
Checkpoint and truncated payloads miss. Field initializers resolve in the class
decorator's enclosing lexical scope. Source-only shadow names cover declarations
between sites and their resolution scope, including blocks, loop headers and
catch clauses. Retain only names used by the callee or token. A shadowed callee
creates no leaf; a shadowed token is unresolved with reason locally shadowed.
The field-initializer case with an enclosing local class is conservative and
unresolved as required by Revision 1, rather than linking a nested class.
Nested arrows never acquire their enclosing emitted owner's injection edge.

## Known gaps

No provider selection, injector hierarchy execution or inherited dependency
expansion. Edges describe requested tokens rather than the eventual provider.
External declarations have no scanned class inventory. Unsupported token
expressions remain explicit unresolved diagnostics. Existing ADR 0022's indirect
Inject behavior is intentionally unchanged pending a separate correction.

Renaming inject through a scanned barrel (export {inject as acquire}, then
importing acquire) is not captured. Importing an export named inject under a
local alias still qualifies. Type names resolve through value bindings, so a
local interface shadowing an imported class name is not detected; Angular rejects
such source. Historical ADR 0022 receiver scopes still treat identifiers in
parameter default expressions as parameter bindings; only the injection proof
uses the separate corrected binding patterns.

## Review notes (developer, 2026-10-26)

- Accepted.
  - angular: 27 eligible classes; 17 constructors, 42 parameters;
    16 field-initializer sites in 15 classes; 58 class leaves (22 linked,
    36 external); 51 class edges (22 local; external: @angular/router 11,
    @angular/common 5, @angular/core 12, @angular/platform-browser 1); 4 function
    edges (tokenInterceptor, errorInterceptor, requireAuth x2).
  - angular-ngmodule: 50 eligible classes; 28 constructors, 54 parameters
    (including `constructor (` spellings); 31 linked, 23 external leaves;
    49 edges.
  - Both corpora have 0 unresolved, invalid, overridden or inherited leaves
    and 0 Angular warnings.
  - The Angular + Express composite and the other eight corpora are
    unchanged.
- Revision 1:
  - Capture is limited to `inject`-named imports and `ns.inject`, storing the
    callee and first argument only. The Langflow fresh store went from +1.63%
    (checkpoint) to +0.07% over main.
  - Field initializers resolve in the class's enclosing scope.
  - Block, loop and catch shadows between a site and its resolution scope
    make the token unresolved ("locally shadowed"), or remove the leaf when
    they shadow the callee.
  - Three edges were dropped by the rename-through-barrel gap (fixture
    `acquire`, angular-httpclient `use` x2).
- Timing (Revision 1 vs main, two Langflow pairs): fresh index -0.14%,
  fresh ingest -3.17%, warm index +0.48%, warm ingest +2.49%.
- Postgres CI: TestAngularDeclarationsRoundTrip/angular-injects passes. The
  postgres job now pulls mirror.gcr.io/library/postgres:17; public.ecr.aws
  anonymous pulls hit "Data limit exceeded" on shared runner IPs.
- Gaps:
  - DI flags (Optional/Self/SkipSelf/Host) are not recorded.
  - InjectionToken consts are counted, not linked to their variable nodes.
  - Inherited constructor dependencies are not expanded.
  - Owners of function sites are matched by node end position.
  - A field initializer whose token names an enclosing local class is
    unresolved rather than linked.
  - The self-index Sites override count is 0 because testdata providers are
    test scope.

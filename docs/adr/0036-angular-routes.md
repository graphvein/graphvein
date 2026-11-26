# ADR 0036 — Angular routes

**Status:** Accepted (delegated review, 2026-11-01)

## Context

ADR 0035 caches Angular router syntax (registration calls, bootstrapModule
calls, loader forms and previously uncached arrays) without proof. T014d2 turns
that payload, the declaration constants, the captured class decorators and the
ADR 0031 module scopes into routing facts. The Router reads every ROUTES multi
provider of the root environment injector, flattened (router.ts, v21.2.12 and
15.2.3): provideRouter in application providers and forRoot/forChild providers
of every NgModule in the bootstrapped module's transitive import closure. A lazy
NgModule's own injector (self: true) holds the ROUTES of that module and the
modules it imports transitively. validateNode (dev mode) rejects malformed
routes with truthiness tests. Both Angular corpora's 28 route objects are
covered by existing caches (the T014d2 investigation, B0a).

## Options considered

- Hash route objects from a new router payload field (B0b): needs a payload
  version and an extractor bump. Rejected for hashes: the normalized source
  bytes are in the link-time sources map for cached and fresh files alike (C1).
  (Revision 1 bumps the payload for other syntax; hashes stay B0a.)
- Decide root provideRouter calls by location (outside class decorators and
  reached route providers): the original decision. Superseded by Revision 1:
  it claimed production paths for never-invoked factories, unbootstrapped
  consts, lazy NgModule provider consts and route-level provider consts, and
  flipped with reachability elsewhere (review 1 F1).
- Prove roots from application bootstraps without new syntax: impossible, the
  version-1 payload kept neither bootstrapApplication calls nor const config
  objects.
- Resolve routes at parse time: caches decisions that go stale after
  edits to imported files, modules or package inputs. Rejected.
- Model registrations by receiver or callee names: produces confident wrong
  links for unrelated APIs and local factories. Rejected.
- Compose routes at link time from proven registrations only, emitting
  unproven context as unresolved: selected.

## Decision

`internal/extract/angular` emits, on every relink:

- level-4 `ui_route` source nodes, one per route object reached from a proven
  registration or loader. Span is the object literal; the content hash is
  computed at link time from the normalized source over that span (B0a);
- `routes_to` (route to linked component class), `loads_children` (route to a
  lazy NgModule class, or to a loaded routes array's variable node, else its
  file node) and `contains` (owner or file to top-level route, parent route to
  child), all facts with confidence 1 and source evidence;
- flat attributes and `angular_route_*` diagnostics as specified in
  [Angular routes](../extract/angular-routes.md).

Proof: provideRouter and RouterModule must resolve through the ADR 0014
resolver to the external `@angular/router` exports, not ambiguous, not
type-only and not locally shadowed. A bootstrap needs a proven
platformBrowserDynamic()/platformBrowser() receiver and a first argument that
resolves to an active local ng_module. forRoot/forChild calls are registrations
of the NgModules whose imports flatten to that exact call (transient
module/file/span tuples captured during ADR 0031 flattening, never cached).
provideRouter inside a captured decorator of an active NgModule registers for
that module; in any other class decorator it is unproven. Otherwise
(Revision 1) a production provideRouter is a root only when proven inside the
`providers` of the options of a proven `bootstrapApplication` /
`createApplication` call (`@angular/platform-browser`): an object literal, or
an identifier or member chain (for example `ns.cfg`) resolving to an unmutated
module const object, whose own
providers value is read through array literals, nested arrays and proven
unmutated const arrays; call sites match by file and full span. Every other
production provideRouter is unproven. Test files keep the location rule (A1).
Roots and bootstrap closures compose from "", lazy NgModules from the loading
route's paths; closures follow local NgModule import and export leaves. Every other proven registration emits its nodes with
empty paths and one info diagnostic. Paths of chains that touch a test file
carry scope test (ADR 0021). Qualified names are `ROUTE <raw path literal>`
segments under the nearest emitted lexical owner, nested under literal parents,
with @N repeats; they never depend on evaluation.

Reviewer decisions recorded with this ADR (the investigation design notes):

1. R6 follows the verified truthiness semantics of validateNode exactly
   (`path && matcher`, `redirectTo && children`, ...; `=== void 0` only where
   Angular uses it). `path: ''` with a matcher and `redirectTo: ''` with
   children are therefore valid.
2. `outlet: ''` and `outlet: 'primary'` are both the primary outlet.
3. requireStandaloneComponents checks only `component` (recursing into literal
   children of a lazily loaded array); loadComponent targets always require a
   standalone component.
4. `component`: linked when the resolved class has role component (standalone
   checked only under requireStandaloneComponents); invalid with a warning and
   no edge for a class with another proven role or no decorator at all (ADR
   0031 leaf precedent); unresolved otherwise, including decorated classes whose
   role is unproven and conflict classes.
5. A proven forRoot/forChild that no NgModule imports is not a registration;
   its routes are not emitted and no diagnostic is raised (no listed code fits:
   angular_route_root_unproven describes registrations that emit nodes).
6. Guard, resolver and title targets link only an exact identity from target()
   (class or other kind), or the unique function/variable/constant/class node
   of a local File+Name; otherwise they are unresolved.
7. Root provideRouter was decided by location only; replaced by Revision 1
   (bootstrap-proven roots) for production files.

## Revisions

Revision 1 (review 1). The reviewer's decisions on review 1 amend the T014d2
task (the committed task record is not edited):

- F1 (rule 14): roots as above. Router payload version 2 retains
  bootstrap_application / create_application calls with their options argument
  (`Options`, same DeclarationExpr and shadow rules as first arguments) and
  module const objects with an own `providers` key (`configs`). Version-1
  payloads miss; the aggregate version is extractors-24 (B0b mechanics).
- F2: closure(X) follows ordered local NgModule import and export leaves; an
  NgModule injector imports its exports (JIT core/src/render3/jit/module.ts
  `imports: [imports, exports]`; AOT ngtsc ng_module handler extends
  injectorImports with exported NgModules, v21.2.12). The original "exports
  never carry providers" was wrong.
- F3: mutations of an anonymous `export default [...]` through a default import
  (recorded under the module's default binding) make the array unresolved.
- F4 (A3): payload version 2 also records `methods`, the key names of method
  and accessor entries of objects that own a path or matcher key. A named
  method makes only its key present-but-unevaluated (`lazy: true` for a
  loadComponent method); a spread, computed key or unnamed method makes absent
  keys unknown (null attributes, unresolved component/children_load), never
  false or absent.
- F5: routes with children_with_load_children, redirect_with_load_children or
  redirect_with_children are not traversed (no child paths, no loads_children).
- F6: requireStandaloneComponents applies per reach; an eager reach keeps the
  routes_to edge, the lazy reach still warns.
- F7: @N suffixes number every object literal array element in the file's
  cached trees, reached or not.
- F9: loadChildren to an inactive NgModule is unresolved. F10: parentheses,
  `as` and `satisfies` around path literals are transparent in names. F12:
  messages carry line:column.

## Consequences

Contract v1 is unchanged: ui_route, routes_to and loads_children are new
extensible kinds. Revision 1 changes the router payload to version 2 and the
aggregate version to extractors-24 (every fact cache misses once);
dependencies, schema and store interface are unchanged; existing nodes, edges,
attributes and diagnostics are untouched. Graph records of files that import
or load a changed routes file are relinked without reparsing them. The new
fixture `angular-routes` has a routing-projection golden, independent
assertions and shared backend round-trip coverage (Postgres runs only in CI).
In the self-index the new records are those of the new
`testdata/fixtures/angular-routes` fixture (test scope there, so the location
rule applies) plus the C7 records of
`testdata/fixtures/angular-router-syntax/calls.ts` (Revision 1 changes only
their message columns); counts are in the T014d2 summary.

## Known gaps

- Bootstrap-proven roots under-claim: provideRouter returned by local helper
  functions, `mergeApplicationConfig(...)`, configs built by spreads, configs
  exported as `export default {...}`, and provideRouter in a lazy NgModule's
  `providers` (which Angular reads with self: true, under the loading route's
  prefix) are unproven: nodes with empty paths and an info diagnostic.
- Test files still classify provideRouter by location (A1): one inside a const
  later used as Component or route providers in a test file is a test-scope
  root, and the route-providers demotion depends on which route objects are
  reached.
- RouterModule.forRoot/forChild that no NgModule imports (for example inside
  `importProvidersFrom(...)` in bootstrapApplication providers) emits nothing
  and no diagnostic (review 1 F11).
- Mutations through calls on other receivers (`Object.assign(routes[0], ...)`,
  `Object.defineProperty`) are not detected (review 1 F8; the ADR 0031
  mutated-set gap): such routes may link the original component. The same
  holds for route arrays, for a bootstrapped config object
  (`Object.assign(cfg, {providers: []})`) and for a providers const array, so
  such a call can still yield a wrong root (review 2 R2-2).
- Writes through wrapped receivers are not seen as mutations: the mutation
  check (`canMutate` in `internal/lang/typescript/declarations.go`) follows
  only bare identifier, member and subscript receivers, not parentheses,
  `as`, `satisfies` or `!` (`(ROUTES as any[]).length = 0`,
  `(R2 as any[]).splice(0)`, `delete (cfg as any).providers`). Such arrays and
  configs can still yield paths and roots. Not fixed here because the same
  check drives the ADR 0031 declaration mutation sets (review 2 R2-1).
- The location of a bootstrapApplication/createApplication call is not
  checked: a call inside a never-invoked exported function still proves a
  root, matching the bootstrapModule rule (review 2 R2-4).
- A bootstrap_application call with more than one argument, or a
  create_application call with arguments but no captured Options, proves no
  root; this only under-claims (review 2 R2-7).
- ModuleWithProviders in NgModule `exports` registers nothing (only imports
  record registration tuples); AOT rejects that form.
- provideRoutes, raw ROUTES providers, resetConfig, importProvidersFrom,
  route-level providers, routerLink and guard/resolver edges are not modeled.
- Provider order (first-match precedence) is not modeled; duplicate full paths
  are all listed.
- Named-outlet routes and their descendants compose no path.
- forRoot reached through a lazy closure contributes no path from that chain.
  Angular's FOR_ROOT_CALLED_TWICE guard exists only in dev mode, so this is
  conservative rather than exact.
- The redirectTo plus canMatch check (absent in 15.2.3) is applied only where
  the project's Angular major is proven to be at least 21.
- Open partial objects (spread, computed key or unnamed method) never compose a
  path, because any missing key, including outlet and matcher, is unknown;
  spreads of local const objects are not evaluated.
- Path, outlet, title, redirectTo and pathMatch values that name identifiers
  are evaluated only at module scope and when no name is locally shadowed.
- Routes of an invalid configuration still link their component targets; the
  object carries the invalid codes.
- Async/Observable loaders, `.then` destructuring and string loadChildren stay
  unresolved with the cached reason.

## Review notes (delegated review, 2026-11-01)

- Delegated review in place of developer review.
- Results:
  - The investigation: P1-P3 pass (T014d2 inventory and census).
  - Review 1: 1 blocker, 3 major, 4 minor, 4 nit; resolved by Revision 1.
  - Review 2: 0 blocker, 0 major, 3 minor, 4 nit; resolved as known gaps and
    documentation fixes (R2-1..R2-7 in the T014d2 summary).
  - Gates at 37ff48a (extractors-24) pass: all 28 corpus route objects match
    the hand truth; existing records unchanged on every checkout, the
    composite and the self-index. Langflow timing passed on the permitted
    repeat series after a warm-ingest outlier (both series reported); fresh
    store +0.011%. CI green, including the postgres job.
- Revisions: Revision 1 (review 1) made production roots bootstrap-proven
  (bootstrapApplication / createApplication options), introduced router
  payload version 2 (`Options`, `configs`, `methods`) and extractors-24,
  followed NgModule exports in closures, made partial-object keys unknown
  instead of absent, stopped traversal of rejected children/loadChildren
  configurations, applied requireStandaloneComponents per reach, numbered @N
  over all cached array elements, and covered anonymous default-export
  mutations, inactive lazy NgModules, transparent label wrappers and message
  columns (see Revisions).
- Gaps: see Known gaps, including the review 2 additions (wrapped mutation
  receivers, Object.assign on configs and providers arrays, unchecked bootstrap
  call location, unrecognised bootstrap argument shapes).

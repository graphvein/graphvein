# Angular routes

ADR 0036 (with Revision 1, review 1); aggregate extractors-49 (router payload
version 2). Routing facts are linked on every run in `internal/extract/angular`
from the router payload ([router syntax](angular-router.md), ADR 0035),
declaration constants and mutations, captured class decorators,
`InjectionSyntax.TypeImports` and the ADR 0031 roles, standalone values and
ordered NgModule imports and exports. Contract v1 is unchanged; existing
records are untouched.

## Proof

- provide_router: the callee resolves to the external `@angular/router` export
  `provideRouter`; for_root/for_child: the callee object resolves to the external
  `RouterModule`. The binding is not ambiguous, the root name is not type-only
  and not in the call's Shadows.
- bootstrap_module: the receiver is a call to the external
  `platformBrowserDynamic` (`@angular/platform-browser-dynamic`) or
  `platformBrowser` (`@angular/platform-browser`); the first argument is an
  identifier resolving to an active local ng_module; no root is shadowed.
- bootstrap_application / create_application: the callee resolves to the
  external `@angular/platform-browser` export `bootstrapApplication` /
  `createApplication` under the same binding rules. Both read
  `options.providers` into the application environment injector
  (browser.ts createProvidersConfig, v21.2.12 and 15.2.3).
- Every identifier is resolved with the record's Scope; a name in Shadows is
  unresolved ("locally shadowed").

## Registrations and roots

- forRoot/forChild calls register for every NgModule whose imports flatten to
  that call (file plus full span; transient tuples from ADR 0031 flattening).
  A proven call that no NgModule imports registers nothing and is not reported.
- provideRouter inside a captured decorator of an active ng_module X registers
  for X; inside any other class decorator it is unproven.
- Revision 1: any other production provideRouter is a root only when it is
  proven to reach the application environment injector: its call (file plus
  full span) is an element of the `providers` of a proven application call's
  options. The options are an object literal, or an identifier or member chain
  resolving to an unmutated module const object (router `configs`); the own
  `providers` key must not be overwritable by a later spread, computed key or
  method. Providers are read from array literals, nested arrays and spreads or
  identifiers of proven unmutated const arrays (cycle guarded); a spread call
  element is not a site. Every other production provideRouter (route-object
  `providers` whether reached or not, class bodies, functions, unbootstrapped
  consts, unknown option forms such as `mergeApplicationConfig(...)`, lazy
  NgModule `providers` consts) is unproven: its nodes have empty paths and
  angular_route_root_unproven. A production provideRouter reached only from
  test-file application calls composes test-scope paths.
- Test files keep the location rule (A1): a provideRouter outside any class
  decorator and outside the `providers` value of a reached route object is a
  root; its paths are test-scope.
- Roots (prefix ""): root provideRouter calls, and the registrations of every
  module in closure(M) for each bootstrap of M. closure(X) is X plus,
  transitively, the local ng_module targets of its ordered import and export
  leaves: an NgModule injector imports its exports too (core
  render3/jit/module.ts; compiler-cli ng_module handler injectorImports).
- loadChildren resolving to an active local ng_module L contributes the
  registrations of closure(L), prefixed with the loading route's composed path.
  An inactive NgModule target (partial, invalid or conflicting metadata) is
  unresolved.
  A forRoot reached this way composes no path and raises
  angular_route_for_root_lazy. Lazy chains carry a per-chain cycle guard
  (angular_route_cycle).
- Every other proven registration still emits its nodes, with empty paths and
  one info angular_route_root_unproven when it reaches at least one route
  object or unresolved entry.

## Route lists

Array literals are read in order: object elements are routes; spreads of an
array literal or of a proven unmutated module const array are flattened (cycle
guarded); other elements raise info angular_route_entry_unresolved. An
identifier or member chain resolves to a `DeclarationSyntax.Constants` entry or
a router `Arrays` entry of the target file (an `export default [...]` array is
`default`). Parentheses, `as` and `satisfies` are transparent. Objects inside a
call's first argument use that call's Scope and Shadows; all others use module
scope with no shadows. `children` nests a list.

An object with a spread, computed key, method or accessor is `partial` and R6
is skipped. A method or accessor whose name the router payload records
(`methods`) defines only that key: present but never evaluated. A spread,
computed key or unnamed method makes the object open: keys before it are
unknown, and absent keys are unknown, not absent (A3). An open object composes
no path; a partial object with only named methods composes when its path is a
known own key and outlet and matcher are absent or known. Duplicate keys: the
last wins.

Routes whose R6 codes include children_with_load_children,
redirect_with_load_children or redirect_with_children are never traversed:
Angular rejects them, so neither children nor loadChildren contribute nodes,
paths or loads_children edges through them (their own node and warning stay).

## Loaders and targets

A loadComponent/loadChildren pair matches the router loader of the same file
whose ValueSpan equals the pair value span. import_default resolves the
module's `default` export, import_member its `Member`, sync the cached value
with the loader's Scope and Shadows; inadmissible loaders are unresolved with
the cached reason.

- loadChildren: a class with role ng_module is a lazy module (linked); a routes
  array is linked to its variable node when emitted, else its file node, and is
  read with requireStandaloneComponents; another proven role is invalid;
  anything else unresolved. A const array is mutated when any binding to it
  is (an anonymous `export default [...]` is matched through the module's
  default binding as well). For a rejected configuration (above) the attribute
  is `{status: "invalid", reason: "loadChildren is never used: ..."}` with no
  edge and no angular_route_target_invalid.
- loadComponent: role component with standalone not false is linked; an
  NgModule, a non-standalone component or another role is invalid.
- component: role component is linked. requireStandaloneComponents applies
  per reach: standalone false is invalid (warning, no edge) only when every
  reach of the object is inside a lazily loaded routes array; when an eager
  reach also exists the edge is kept (status linked) and the lazy reach still
  raises angular_route_target_invalid ending "(lazy reach only)". Another role, or a class with no
  decorator, is invalid; a decorated class whose role is unproven, a conflict
  class or a non-class value is unresolved. When both component and
  loadComponent are present, component is used (Angular reads
  `route.component ?? route._loadedComponent`).
- Target objects (guards, resolve, title, redirect functions): inline arrow or
  function is `{status: "inline"}`; an exact identity from target() is linked;
  a local File+Name links to the unique function, variable, constant or class
  node of that file with that qualified name; an external binding is
  `{status: "external", target: <package node>}`; anything else unresolved.

## Nodes

Kind `ui_route`, level 4, file and span of the object literal, content hash of
the normalized source over that span (computed at link time). Names come from
source literals only; see [identities](../lang/identity.md#angular-ui-routes).

Attributes (flat): `framework: "angular"`; `path` (evaluated string or null;
omitted when absent, null on open objects without one); `matcher: true`;
`paths` (sorted `{path, scope?}`); `lazy` (true when loadComponent is an own
known key or a named method, false when absent, null when it may be
overwritten or supplied by a spread);
`component` and `children_load` (`{status, target?, reason?}`, status linked,
external, unresolved or invalid); `redirect_to`, `path_match`, `outlet`
(string or null; a v21 redirect function is a target object); `title` (string
or target object); `guards` (present keys of can_activate, can_match,
can_activate_child, can_deactivate, can_load, each a source-ordered list of
target objects, or null when the value is not an array literal); `resolve`
(key to target object, or null); `providers: true`; `partial: true`; `invalid`
(sorted R6 codes); `scope: "test"` for test-file objects. On open objects every
absent key that a spread could supply is unknown: `matcher`, `redirect_to`,
`path_match`, `outlet`, `title`, `resolve` and `providers` are null, each absent
guard key is null, and `component` / `children_load` are `{status:
"unresolved", reason: "may be supplied by a spread or computed key"}`. Named
methods give `component` / `children_load` status unresolved ("value is a
method or accessor; not evaluated") and null for other keys. Attribute strings
never contain U+0000-U+001F (such values are unknown).

## Paths

Start from the chain prefix, append each path ('' adds nothing), join with "/",
ensure a leading slash, collapse repeated slashes and drop a trailing slash
except for the root; case, `:params` and `**` are kept. Composition stops for a
route and its descendants when the path is unknown, a matcher is present, the
path starts with "/", the outlet is not primary ('' and 'primary' are primary)
or unknown, or an ancestor stopped. A chain is test-scope if the root call or
bootstrap, a registration, a loading route or the object itself is in a test
file; entries merge by path and a production chain omits scope.

## Edges

All facts with confidence 1. `routes_to`: ui_route to linked component class,
evidence the component or loader value span. `loads_children`: ui_route to the
lazy NgModule class or the loaded array's variable/file node, evidence the
loader value span. `contains`: owner (or file) to each top-level route and
parent route to each child, evidence the contained route's file and span (the
HTTP route convention).

## Validation (R6)

validateNode is evaluated with Angular truthiness, only for non-partial objects
and only when a violation is certain. Codes: `named_outlet_without_target`,
`redirect_with_children`, `redirect_with_load_children`,
`children_with_load_children`, `component_with_load_component`,
`redirect_with_component`, `redirect_with_guard` (canActivate; canMatch only
for proven Angular majors of 21 or later), `path_with_matcher`,
`missing_target`, `missing_path`, `leading_slash`,
`empty_path_redirect_without_path_match`. Each raises a warning
angular_route_invalid.

## Diagnostics

| Code | Severity | When |
|---|---|---|
| angular_route_invalid | warning | an R6 violation |
| angular_route_target_invalid | warning | a proven component or loader target of the wrong kind |
| angular_route_for_root_lazy | warning | forRoot reached through a lazy closure |
| angular_route_root_unproven | info | a registration not reached from a proven root |
| angular_route_entry_unresolved | info | an unresolved route list or entry |
| angular_route_cycle | info | a lazy chain revisits a module or array |

Messages name the file, line and 1-based column of the object or call
(`route at <file>:<line>:<column>: <reason>`) and the reason.

Not modeled: provideRoutes, raw ROUTES providers, resetConfig,
importProvidersFrom (including RouterModule.forRoot inside it, which emits
nothing), route-level providers, routerLink, guard and resolver edges, provider
order, mutations through calls such as `Object.assign(routes[0], ...)` or
`Object.assign(cfg, {providers: []})` (ADR 0031 gap), and writes through
wrapped receivers such as `(ROUTES as any[]).length = 0` (ADR 0036 known
gaps). The fixture is `testdata/fixtures/angular-routes`.

# Angular declarations

Requested-token DI facts: [Angular injection](angular-injection.md).

ADR 0031 adds link-time Angular class metadata under contract v1. Aggregate
cache version: extractors-49. No new node kinds or identity rules are introduced.

Only direct non-type-only imports from exactly `@angular/core`, proven external
by ADR 0014, assign roles: component, directive, pipe, ng_module or injectable.
Aliases and namespaces qualify. Indirect decorators, scanned shadows and absent
class identities produce diagnostics. Conflicts produce only
`angular: {role: null, conflict: true}`.

The class `angular` object records metadata completeness and applicable
selector/export_as, standalone/source, pipe name/pure, injectable, jit, template
and scope counts. Parentheses and `as` may wrap metadata; other assertions are
invalid. Spreads/computed keys/methods/accessors make metadata partial and
unevaluated. Last own key wins. An own jit key suppresses component/directive/
NgModule analysis, including jit:false; pipes ignore jit.

Scalars reuse ADR 0020's evaluator. Present values that cannot be evaluated or
have the wrong type stay unknown and never take a default. The optional sorted
`unknown_keys` array lists these keys using their metadata spelling: selector,
exportAs, standalone, name, pure and templateUrl. Existing
angular_value_unevaluated and angular_value_wrong_type diagnostics are unchanged.

Explicit booleans determine standalone. An explicit unknown gives null and
standalone_source unknown, with no version lookup or angular_version_unknown
diagnostic. Only an absent standalone key uses a version default:
core/compiler-cli need matching majors, using nearest package-lock
v1/v2/v3 or bun JSONC entries before package ranges. Single-major exact/caret/
tilde/x ranges qualify. Angular 19 and later default true; earlier versions
false. Missing, mismatched or multi-major versions give null and one diagnostic
per project.

`uses_template` targets scanned file nodes. templateUrl wins over inline
template, is case-exact/repository-contained, and records linked, unparsed,
missing or unresolved status. Inline templates reuse ADR 0030 results and retain
the unparsed reason.

Dot-prefixed templateUrl values other than ./ and ../ (for example
.templates/x.html) are unresolved with reason "resource path is not relative"
(C2). Rooted/outside-repository and backslash guards retain their existing
behavior. Compilation scopes and template references are described in
[Angular template matching](angular-matching.md).

`declares`, `scope_imports` and `scope_exports` reuse class and package external
nodes. Arrays flatten literals, spreads of literals/proven module const arrays,
forwardRef and import/export ngModule objects. External ModuleWithProviders
calls qualify only in NgModules. Mutations, cycles, local factories and
unsupported expressions remain unresolved. Evidence includes entry and
expansion locations. Repeated entries merge edges; counts still describe every
leaf, partitioned into linked (local), external, unresolved and invalid.
Mutation capture retains only unshadowed import bindings (named, default or
namespace) and module-level single-declarator const roots in the same file,
following receiver member/subscript chains. Other mutations cannot reach an
eligible array or namespace and add no declaration payload.

Declarations require non-standalone components/directives/pipes and a directive
selector. Imports require NgModules or standalone declarations. Exports retain
local checks; transitive validation is deferred. Distinct declaring modules in
one production/test scope suppress all their declarations of a duplicate class.
A repeated entry in one module and a separate test declaration do not.
An unknown selector is unresolved, rather than selectorless; only an absent
key or empty string proves a missing selector. Invalid entries require a proven
class declaration rejected by these rules, including classes without Angular
roles. Conflicted classes remain unresolved. Variables, functions, aliases,
class expressions and const arrays that cannot be proven give info
angular_scope_entry_unresolved with reason "target is not a class declaration"
when resolved. No speculative edge follows an alias initializer.

Warnings describe invalid Angular source; info describes unavailable proof.
Host capture panics emit angular_syntax_capture_failed and retry on subsequent
runs without dropping extracted graph facts. Configuration/resource changes
relink every run. Source caches contain no Angular proof or derived attributes.

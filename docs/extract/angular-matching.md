# Angular template matching

ADRs 0033 and 0034; extractors-49 and contract v1 unchanged.

Matching runs at link time after [declarations](angular-declarations.md) and
[injection](angular-injection.md). Active component classes with linked external
or parsed inline templates receive angular.template_matching. Templates may
have multiple owners, each using its own scope.

Standalone scopes include self, local standalone imports and NgModule exports.
Declared hosts use their single declaring module's compilation scope, including
transitive module exports. External leaves give open; unresolved leaves and
deferredImports give unknown; invalid leaves, illegal exports and cycles give
invalid. Missing standalone or declaring-module proof gives none. Invalid and
none scopes emit no template edges.

The verified selector subset is lowercase element names and attribute presence
or value compounds, including comma alternatives. Attribute values compare
case-insensitively. Unsupported selectors are diagnosed and keep nonmatches
unknown. Static attributes retain raw names; bindings clear static values and
two-way bindings contribute both input and Change output names. i18n,
references and variables do not contribute matching names. Dotted, namespaced,
animation-like names and encoded/non-ASCII values retain uncertainty.

Structural wrappers match ng-template using their primary key only. Secondary
microsyntax keys remain underived; multiple structural attributes are invalid.
Real ng-template elements use ordinary attributes. Non-bindable descendants use
raw static attributes. Dropped elements and ng-content are not matched.

renders edges connect hosts to local matched components/directives. Multiple
matching components are diagnosed and suppressed while directive matches remain.
uses_pipe edges require a unique local name with no unknown pipe names. Complete
scopes link as before. In open scopes, rule P also links when the pipe's first
contributing import unit is closed and follows every open unit. Standalone
units follow flattened imports; declared hosts use module imports followed by
one declarations unit. Local leaves retain their first position; external
leaves remain distinct even when their package targets coincide. Local NgModule
units contribute their exported local classes and are open when their exported
scope is open. Other open-scope local matches remain shadowable. Ordered leaves
are captured at link time before edge merging, without changing cached syntax.
Action/two-way and track
pipes are invalid. All edges are fact/confidence 1 and merge source evidence.
Inline evidence uses TypeScript host coordinates; external evidence uses HTML.

Counters partition custom elements (excluding Angular container/template/content
elements), structural wrappers and captured pipes by status. Attribute-only
directive matches on ordinary elements still produce edges and match pairs.
The overlay records scope, matches, elements, structural, pipes and recovery_spans
(the number of cached error spans). Unmatched closed-scope usages are info
diagnostics. No external template-reference nodes or edges are synthesized.

Expression recovery lies wholly inside an attribute value or block parameter.
Every other error span is structural: all usages starting at or after the first
such span become unknown. Contents of orphan connected blocks are also unknown;
adjacency permits only whitespace and complete HTML comments, with matching
block/element ancestry and allowed predecessor names. case/default require a
switch parent. One angular_template_structure_unknown info diagnostic per host
names the first affected line and cause. Errors in text interpolations therefore
lose later coverage. Repeated else and branch ordering remain undiagnosed.

Other diagnostic codes are angular_template_scope_unknown,
angular_selector_unmodeled, angular_template_multiple_components,
angular_pipe_invalid_context and angular_template_unmatched. Each host usage
is assessed from current scopes and cached source spans on every replay.

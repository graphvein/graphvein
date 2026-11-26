# Angular template and decorator syntax

Class roles, template resources and scope edges: [Angular declarations](angular-declarations.md).

ADR 0030, extractors-49, contract v1 unchanged.

`internal/lang/template` captures every retained HTML/HTM file using ADR 0028's
Angular grammar. Cache fields are `error_spans`, `elements`, `attributes`,
`blocks` and `pipes`; all must be arrays, including clean empty results.
These are internal cache facts, never NDJSON nodes, edges or diagnostics.

Elements retain raw names, parent indices, dropped/non-bindable flags and
ng-content select values. Attributes retain raw/data-normalized/binding names,
A1 kind, raw value, source/name/value spans, i18n exclusion markers, extra
structural-attribute flags and off-ng-template variable flags. Bindings in
non-bindable descendants become static; their blocks and expressions are text.
Script/style/stylesheet links remain visible as dropped source elements.

Blocks retain canonical and raw names, parent block/element indices, full spans
and ordered raw parameter slices with spans. Connection/grouping is deferred;
comments and intervening whitespace do not create an implicit association.
Pipes retain the source name/span, error intersection flag and a track-context
flag (pipes are not valid in track expressions). Quote-aware scanning is bounded
to binding values, interpolations and block/let header values; it excludes `||`
and quoted pipe characters. Static values do not create pipe uses.

Recovery spans include ERROR and unnamed MISSING locations. A captured item
intersecting recovery is flagged; an item covering a zero-width MISSING point
is also flagged. Missing expression references or pipes inside these spans are
**unknown**, never absent. Later linking must report that uncertainty rather
than silently omitting a uses_pipe edge.

`typescript.CachedFacts` embeds existing language facts and adds
`template_syntax` with required `decorators`/`inline` arrays. Decorators preserve
callee aliases/member chains, class identity, argument count and ordered raw
metadata entries. Value syntax reuses the client URL expression form. No
Angular import/name filtering or selector/standalone/jit/exportAs evaluation
occurs. Spreads/computed keys remain explicit entries and inadmissible reasons.

Only own `template` properties in first-argument object literals are considered.
Metadata parentheses and assertions are transparent. String/no-substitution
template literals without a backslash are parsed as raw body bytes. Other
forms retain a reason. All host roots are released before a second bounded
Angular `Consume` pass. Each guest has its own deadline and failure isolation;
transient guest failure reasons persist but make that payload a cache miss on
the next run, preserving host graph facts and retrying the guest.

Inline byte mapping adds content-start offset. Line one adds content-start
byte column; subsequent lines retain columns and add content-start line minus
one. This applies to every nested span, including grammar recovery locations.
CRLF sources normalize before parsing, hashing and mapping. Replay through the
store interface avoids all host/guest parsing on unchanged valid payloads.

Template ownership and declarations are described in
[Angular declarations](angular-declarations.md); directive/pipe links and
recovery handling in [Angular template matching](angular-matching.md).
Router composition remains future T014d work.

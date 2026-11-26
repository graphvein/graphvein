# Angular injection facts

ADR 0032 adds requested-token `injects` edges under contract v1, using
extractors-49 syntax caches. Every edge is a fact at confidence 1, with sorted,
deduplicated evidence for all contributing leaves.

Sources are eligible Angular-role classes and emitted functions outside class
bodies. Conflict, jit and invalid-metadata classes gain nothing; partial metadata
does not prevent constructor analysis. Constructor overload signatures are
ignored. A class without an implementation constructor and with extends records
inherited:true and creates no constructor leaves.

Constructor parameter properties and ordinary parameters count equally. Direct
non-type-only named/namespace imports from exactly @angular/core establish
parameter decorators after external-package proof. Inject and Attribute require
one argument; source order decides their token. Optional/Self/SkipSelf/Host are
flags only. Other Angular decorators are invalid. Local/barrel decorators are
ignored and the type applies. This intentionally preserves ADR 0022's older
receiver behavior separately.

Types remove only null members of unions, then require one reference entity.
Type arguments are ignored. Type-only imports, namespace-alone references,
interfaces, aliases, parameters, enums, missing annotations and unsupported type
forms remain unresolved. Resolve runtime entities through ADR 0014.

Runtime inject capture accepts named imports whose imported name is inject,
including aliases and barrels, and namespace.inject from any specifier. The
unchanged resolver must prove the external @angular/core export inject at link
time. Each site retains only its callee and first argument; options and duplicate
whole-call syntax are omitted. Instance
initializers, constructor bodies/defaults and the own body of a non-nested
emitted function qualify. Static fields, methods/accessors, nested functions,
anonymous callbacks, module-level calls and ineligible classes create no leaves.

Fields resolve in their class decorator's enclosing lexical scope. Local
declarations between a site and its resolution scope, including blocks, loop
headers and catch bindings, suppress a shadowed callee entirely and make shadowed
tokens unresolved with reason locally shadowed. Enclosing local classes in field
initializers are conservatively unresolved. Sibling blocks do not shadow a site.

Parentheses and TypeScript assertions are transparent for runtime tokens.
External forwardRef with one single-return arrow/function resolves its returned
expression. Scanned class declarations link directly. External exports link to
the existing package node, so @angular/common/http targets @angular/common.
Single-declarator module const new InjectionToken initializers, including imported
or barrel references, count as injection_token without an edge. Attribute and
inject(new HostAttributeToken(...)) count as attribute without an edge.

Each eligible class's angular.injects records entries, linked, external,
unresolved, invalid, injection_token and attribute; these partition all leaves.
Overridden is a subset of linked plus external. Repeated leaves still count
separately after edges merge. Functions have no injects attribute.

Production provider overrides reuse ADR 0022 unchanged. Eligible Injectable own
provider keys also override, ignoring spreads, computed keys, methods and
accessors. Edges remain attached to the requested token. Info
angular_injection_overridden identifies the source/token and provider locations
once per pair; test providers are ignored. Invalid source produces warning
angular_injection_invalid; unavailable proof produces info
angular_injection_unresolved with a reason and source/site/token text.

The additive injections cache requires all top-level and parameter arrays and
version 2. Checkpoint, missing or truncated syntax forces a host reparse. Parameter binding
patterns are stored independently of default expressions to keep the historical
receiver cache unchanged. Link-time package/provider/configuration edits never
require reparsing unchanged TypeScript.

Parameter scope corrections are retained only for scopes enclosing a captured
site or used by a recorded class. Invalid parameter decorators name the specific
DECORATOR_ARITY_WRONG or DECORATOR_UNEXPECTED error. Diagnostic token spelling
omits the type-annotation colon.

Known gaps: inject renamed through a scanned barrel to a different imported name
is not captured. Type names use value bindings, so a local interface shadowing an
imported class name is not detected (Angular rejects that source). The historical
ADR 0022 receiver scopes still count identifiers in parameter default expressions
as bindings; the new injection proof has its own corrected parameter patterns.

C1 additionally makes a single Injectable argument invalid unless it is directly
an object literal, without parentheses or assertions.

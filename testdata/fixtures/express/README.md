# Express route fixture

Hand-written TypeScript and CommonJS syntax; no external corpus source.
Imports are syntax only, so no packages need to be installed. No inert-name
mapping is needed. The golden captures route nodes, their containment and
handler edges, and route diagnostics. Other language facts retain their own
goldens. The fixture covers factory import provenance, chains, mounts, duplicate
registrations, barrel handlers, unresolved workspace imports and client/lookalike negatives.
The binding cases also exercise default factory returns, locally exported factory
aliases, deduplication across roots, local factories, mutated receivers, dynamic
regex/array/concatenation/templates, and ambiguous handlers. No package code is
executed during indexing.

T011b resolves hidden through barrel.ts and recognizes the imported Router relay
at bindings.ts:29. The latter intentionally keeps its historical pattern name
/not-resolved-through-reexport; it is now a route with a handler but no mount,
so it has empty paths and a route_unmounted diagnostic.

# Angular injection syntax and link-time proof

This synthetic repository covers constructor tokens, direct parameter decorators,
runtime inject contexts, unavailable types, inherited factories, merged evidence,
provider overrides and strict Injectable metadata. No live ignore files or inert
path mappings are needed. `scope.test.ts` supplies test-only providers.

`lexical.ts` covers enclosing class scope, constructor blocks, callee shadows,
loop and catch bindings, member token heads, nested arrows sharing an end span,
and an unshadowed sibling site. Local declarations conservatively prevent token
edges; a declaration in a sibling block does not affect an outer site.

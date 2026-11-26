# Environment-base wrappers and fetch init spreads

Synthetic Express routes and TypeScript requests cover source-ordered fetch
methods, environment bases through assertions, wrapper defaults, three wrapper
levels, repeated sends, opaque prefixes, unknown options and lexical shadowing.
`frontend.ts` imports its wrappers from another file to exercise freezing and
cache replay. No fixture names need restoring and there are no live ignore files.
The unique final route segment avoids changing other fixtures' exact or suffix
candidate sets when Graphvein indexes its own repository.

Revision 1 appends missing-default, body-shadowed-default and opaque-const-default
wrappers without moving existing request lines. Missing options contribute no
properties; the body declaration cannot replace the module POST initializer;
unproven defaults (including the existing call default on frontend.ts:29) keep
the method dynamic and emit no route edge. Independent assertions and the golden
cover these cases alongside cache replay and imported-default relinking.

# FastAPI fixture

Hand-authored source and golden expectations cover project roots, package
submodule imports and initializer re-exports, direct and stacked registrations,
methods defaults/list/tuple, inline and unresolved endpoints, nested router
prefixes, literal/imported-constant/settings-default includes, conditional
merging, mounted sub-app roots, function-local apps, dynamic diagnostics,
websocket exclusion, local lookalike classes and repeated identities.
Unknown keyword unpacks cannot prove omitted constructor/include prefixes or
api_route methods; these produce diagnostics and never guessed default paths.

No inert-name restoration is needed: there are no live ignore files or ignored
paths. The compact golden lists every route's identity, handler, owner,
registration line and composed paths, plus route diagnostics and handler edges.
Tests independently verify the exact source span/hash and cached replay.
Expected paths were written from the fixture source, never indexer output.

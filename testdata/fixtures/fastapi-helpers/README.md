# FastAPI helper mounts

This fixture proves receiver and included-object parameters, imported helper
aliases, function-local imports, conditional bodies/call sites, keyword calls,
two-router calls, mount paths and slash twins. Negative cases cover uncalled,
nested and decorated helpers, enclosing-helper parameters, unpacked calls,
defaults, rebinding and parameter prefixes. Async helpers require await.

No inert-name restoration is needed: no live ignore files or ignored paths are
present. The test checks hand-written expected paths before comparing the full
route snapshot with the golden output. Cached replay and target-only edits must
equal a fresh index.

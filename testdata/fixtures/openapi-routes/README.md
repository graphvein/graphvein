# OpenAPI route links

All route URLs use `/t013b` so self-indexing cannot link unrelated fixtures.
Tests copy this directory into a temporary repository, renaming
`_graphvein.yaml` to `graphvein.yaml`. There are no ignore-file mappings.

The 3.2 spec covers relative and absolute servers, method spelling, implicit
HEAD, parameters and literals, specificity, duplicate and ambiguous Express
registrations, FastAPI slash twins, multiple servers, unresolved servers,
wildcards and optional parameters. `tests/` supplies a test-scope spec and
routes. The production spec's `/t013b/only-test` operation is test_only.

`configured-openapi.json` has a relative server and an operation-level absolute
server whose path is wrong. Configuration replaces both with `/t013b`.
In the repository self-index this configuration is inert; those two operations
remain unresolved and unmatched, respectively. The inherited `testdata/` scope
also allows the only-test operation to link within this same fixture.

# Test scope fixture

URLs use the unique /t011g prefix so this fixture adds no competitors to
pre-existing self-index calls. No inert names need restoring: every input here
can be tracked under its real name, and there are no live ignore files or
ignored paths. Tests copy this fixture to a temporary directory before indexing.

t011g_backend.py has production registrations, two equally specific production candidates,
an ANY fallback, and a helper. tests/test_apps.py adds test roots, a registration
on the imported production app, a test include and helper invocations. The helper
path also has a production chain and therefore remains production. __tests__/app.ts
and tests/router.rs cover Express and Cargo integration-test roots. check.spec.ts
ignores scope when matching: mixed targets remain ambiguous and a test-only
target still links. checks/config.ts is included by configuration; kept.test.ts
overrides the built-in rule through exclude. frontend.ts's dynamic authority
checks that internal URL pieces do not escape into attributes.

The golden projection keeps complete route/client nodes and calls_route edges.
Assertions independently check the expected scopes, statuses, targets and
confidence before comparing the golden. Config-only replay must parse zero files.

Python modules have unique t011g names to avoid resolving another fixture's
similarly named module when Graphvein indexes all fixtures together. Test helper
bodies and wrapper invocations have separate cases. Cross-file Express and Axum
compositions keep source registrations independent of their test chain scope.

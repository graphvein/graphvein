# Project link fixture (T022 B3, B5)

Four hand-written member repositories for the project link pass
(`internal/projectlink`, docs/extract/project-links.md). No external corpus
source. Imports are syntax only; nothing needs installing.

- `frontend/`: fetch calls with an `import.meta.env.VITE_API` base (users,
  a user by id, orders, a duplicated route, a missing route), a call with
  a literal origin (`https://partner.example.com`), a call with two
  conditional alternatives, a call to the equivalent registrations, a call
  whose two alternatives have different bases (`VITE_API`,
  `VITE_PARTNER`; matched per alternative from the stored base), a dynamic
  call, and an OpenAPI spec whose server is `https://partner.example.com`.
- `backend/`: Express routes for users, a user by id, orders, two
  registrations of `/t022/duplicate` with different handlers, and two
  equivalent registrations of `/t022/same` (ADR 0022; stored as
  `equivalent_registrations`, so the project pass links the first).
- `backend2/`: Express routes for users and a user by id.
- `monorepo/` (link scopes): two Express servers in one
  repository, `apps/users` (users, a user by id, health) and `apps/orders`
  (users again, orders, health), so `GET /t022/users` and
  `GET /t022/mono/health` are ambiguous inside the repository. Calls in
  `web/` (a base per server, an undeclared base, a wrapper, and a call
  linked inside the repository to the users server although its base names
  the orders server: the "conflicts with declaration" case of T022 B9), `admin/` (a call and an OpenAPI spec), `ops/` and `tools/`
  (the default scope).
- `scenarios/*.yaml`: project manifests (`graphvein-project.yaml` format)
  read by `TestFixtureScenarios`: `two` (frontend + backend), `three` (two
  backends, ambiguous), `consumes` (frontend consumes backend), `selector`
  (a `bases` selector for backend, an `origins` selector for backend2),
  `unindexed` (a member without a stored index), `scopes` (a consumer
  declaring a link scope that holds none of its calls: the same result as
  `two`), `monorepo` (the monorepo's scopes `users`, `orders`, `web`,
  `admin` and `ops` with their own `consumes`, resolving in-repository
  ambiguity as a project overlay; frontend consumes `monorepo/users`) and
  `monorepo-default` (the same servers as scopes, no declarations) and
  `monorepo-default-scope` (only `apps/users` and `ops` are
  declared scopes, so the orders server is in the default scope; frontend
  and the `ops` scope consume `monorepo` with `default_scope: true`).

Goldens: `../projectlink.<scenario>.golden.json` (links with run IDs
replaced by `<run of member>`, and every source's outcome).

Every URL uses `/t022` so the repository's own self-index links no other
fixture.

Inert name mapping: `frontend/openapi.yaml.fixture` and
`monorepo/admin/openapi.yaml.fixture` are restored to `openapi.yaml` in
the temporary copy, so the repository's own
self-index finds no OpenAPI spec outside the OpenAPI fixtures (the
self-index check in `internal/index/openapi_corpus_test.go`).

# Project impact fixture (T022 B7)

Two hand-written member repositories for project-scoped callers and
impact (`graphvein callers|impact --project`, `graphvein changes impact
--project`; ADR 0075 "Query, impact and change journal"). No external
corpus source; imports are syntax only, nothing needs installing.

- `frontend/`: `src/api.ts` makes two fetch calls with an
  `import.meta.env.VITE_API` base (`GET` and `POST /t022b7/users`);
  `src/page.ts` holds the UI functions calling them; an OpenAPI spec
  documents `GET /t022b7/users` without a server.
- `backend/`: an Express app (`src/server.ts`) registering both routes
  with handlers that query the Drizzle table `users` (`src/db/schema.ts`,
  group `drizzle.config.ts`, versions pinned in `package-lock.json` as in
  `table-access-drizzle`).

The project link pass links both calls (`calls_route`) and the operation
(`documents_route`) to the backend's routes with the default assumption,
so a change of the table reaches `UsersPage` through table -> handler ->
route -> project link -> client call -> api function -> UI function.

Tests: `TestProjectQueryGoldens` (cmd/graphvein, golden
`../projectimpact.cli-query.golden.json`) and `TestProjectImpactGolden`
(internal/changes, golden `../change-impact.project.golden.json`); the MCP
project session (`TestSessionGoldens`, internal/mcp, golden
`internal/mcp/testdata/session-project.golden.json`) reads it too; its
stale part indexes the frontend again and the backend with `listUsers`'
body changed (one statement split in two), so change impact crosses the
stale link. The
latter edits its copy of the backend for a second revision (a column
added to `users`, the list query changed, the POST route removed); the
edits are spelled out in the test.

Every URL uses `/t022b7` so the repository's own self-index links no other
fixture.

Inert name mapping: `frontend/openapi.yaml.fixture` is restored to
`openapi.yaml` in the temporary copy, so the repository's own self-index
finds no OpenAPI spec outside the OpenAPI fixtures.

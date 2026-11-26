# shape-differences-project fixture

Row SD-11 of `docs/contract/claims/shape-differences.md` (T029 W4): a
cross-repository link compared in project mode. Two hand-written member
repositories; nothing needs installing.

- `api/`: a FastAPI app whose `POST /w4p/users` declares the body
  `UserCreate` (`email`, `name`, both required).
- `web/`: one fetch call with an `import.meta.env.VITE_API` base and a
  literal JSON body (`email`, `role`).

`TestShapeDifferences` (`internal/server`) writes the project manifest
(members `web` and `api`), indexes both, runs the project link pass and
checks the endpoint detail's project caller against
`../shape-differences-project.golden.json`: `role` is sent and not read,
`name` is read and not sent, labelled `cross_repository`.

No ignore files or renamed ignored paths are needed.

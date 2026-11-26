# tryit fixture

Repository for the "try it" server tests (T029, rows TI-* of
`docs/contract/claims/try-it.md`). The server tests index it and read:

- route nodes for the operation match (Express `server.ts`, FastAPI
  `api/main.py`): one-segment parameters, a POST and a DELETE, `all`
  (any method), a wildcard, an optional parameter and a trailing slash;
- base URL suggestions: Compose published ports and a Traefik router
  (`docker-compose.yml`; the `db` service is an image, not read; the
  interpolated port is not read), `EXPOSE` (`Dockerfile`), spec servers
  (`openapi.yaml`; the templated server is not read), `app.listen(3000)`,
  `uvicorn.run(port=8001)`, `ListenAndServe(":8080")` (`main.go`), a
  package.json script `--port 4000`, a Procfile `--bind`, and framework
  defaults. `tests/server.test.ts` is test code: its `listen(9999)` is not
  suggested.

The golden is `internal/server/testdata/tryit-suggestions.golden.json`.
No file here is an ignore file or an ignored path.

# shape-differences fixture

Rows SD-1..SD-12 of `docs/contract/claims/shape-differences.md` (T029 W4):
what a linked frontend call sends compared with what the route reads.
Hand-written; nothing needs installing.

- `backend/app/main.py`: a FastAPI app with Pydantic bodies (`ItemCreate`,
  `ProfileUpdate` with a nested `Address`), a route with declared query
  parameters, and a route that reads the raw request body (partial).
- `server/app.ts`: an Express app with a Joi-validated body and a route
  reading `req.body.text`. Express bodies are never complete (the content
  type is not read), so their links show nothing.
- `frontend/`: a hey-api client (`openapi-ts.config.ts` with
  `@hey-api/client-axios`). `src/client/sdk.gen.ts`, `client.gen.ts` and
  `types.gen.ts` are hand-written in the form @hey-api/openapi-ts 0.99.0
  writes (as in the `auth-heyapi` fixture); `src/client/client/index.ts`
  is a stand-in for the generator's bundled client. The types differ from
  the backend on purpose (`ProfileUpdate.nickname`, the `search` query
  parameter), as a client generated from an older spec would.
- `backend-v1/` (`pydantic==1.10.13`) and `backend-v2/` (`pydantic>=2.7`):
  the same `Person` model (`fullName` alias, `nick: Optional[str]` without
  a default). Only the Pydantic 2 project gives SD-9 for `nick`; under
  Pydantic 1 (and without a proven version) its presence is unknown.
- `rust/`: an axum crate. `NewUser` has a serde `alias` with
  `deny_unknown_fields`, so sending the alias gives no SD-8; `NewTeam`
  has none and gives SD-8 for an extra key.
- `web/api.ts`: fetch calls with literal JSON bodies and literal
  `URLSearchParams`, each commented with the row it covers, and the forms
  that give nothing (a dynamic body, shared headers, no Content-Type, a
  partial route, an Express route).

Every URL uses `/w4` so the repository's own self-index links no other
fixture. Golden: `../shape-differences.golden.json` (`TestShapeDifferences`
in `internal/server`: the stored `sends` attributes and the comparison
each route's endpoint detail serves per caller; the Flow endpoint must
serve the same on each calls_route edge).

No ignore files or renamed ignored paths are needed.

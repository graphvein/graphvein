# change-impact-models fixture

Own code (no third-party text) for T029 W2 (rows MT-1..MT-19 of
`docs/contract/claims/route-model-types.md`): two revisions of one small
repository with a FastAPI app (`py/`), a gin app (`go/`) and an Express app
with a fetch client (`web/`). `internal/changes/modeluse_golden_test.go`
indexes rev1 then rev2 on SQLite and Postgres and checks the comparison and
the impact of every changed model against
`../change-impact.models.golden.json`. No file needs an inert name: there is
no ignore file and no ignored directory.

| change (rev1 to rev2) | expected |
|---|---|
| `py/app/models.py`: `Address` gains `zip` (nested in the `UserIn` body) | C2 `body_changed`; I16 to `POST /users`, "through request body (fact) model Address" |
| `py/app/models.py`: `UserBase` gains `email` (repository base of `UserIn` and `UserOut`) | I16 to `POST /users` (body and response) and `GET /users/{user_id}` (response) |
| `go/types.go`: `Money` gains `Cents` (nested in `Order`) | I16 to `POST /orders` and `GET /orders/:id`, returned responses |
| `go/types.go` and `go/main.go`: new `Status`, returned by `GET /health` | C2 added; C3 `models_changed` on `GET /health` (field `model_edges`); I16 from `Status` |
| `web/src/types.ts`: `Pet` gains `tag` (the declared return type of `loadPet`, SH-E28) | I16 to `GET /pets/:id` (returned), then I3 to the client call in `web/src/client.ts` and I2 to `showPet` |
| `web/src/schemas.ts`: zod `NewPet` gains `tag` (parsed in `POST /pets/checked`, SH-E12) | I16 from the `variable` node, "through request body (fact) model NewPet" |

`PetInput` (an intended body, SH-E17), `NewOrder` (a gin-bound fact body)
and `Unused` do not change; they show the uses kept on unchanged routes.

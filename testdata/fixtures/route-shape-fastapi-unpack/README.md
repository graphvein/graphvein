# route-shape-fastapi-unpack fixture

Rows SH-F24, SH-F37 and SH-F38 of `docs/contract/claims/route-shape-fastapi.md`
for keyword unpacking and `default_response_class`: the response defaults
apply to the routes the object serves, not to every route of the project.
A separate project because a `FastAPI(**options)` may pass exception
handlers, which lower the raised statuses and the 422 of every route.
Hand-authored, importable by FastAPI 0.161.1:

- `app/responses.py`: `CreatedJSON` (a `JSONResponse` sent with 201 when
  FastAPI passes no status), an `HTTPException` handler answering 418, and
  the option dictionaries the sub-app and a router unpack.
- `app/main.py`: the plain app; its own route `/root` and the plain router
  keep FastAPI's defaults (success 200 `application/json`); `plain.styled`
  is included with `default_response_class=CreatedJSON`, the unpacked
  router under `/unpacked`, and the sub-app is mounted at `/sub` (a mount
  passes no default on).
- `app/routers/plain.py`: `router` (included only into the plain app: not
  affected), `shared` (included into the plain app and into the unpacked
  sub-app: affected) and `styled`.
- `app/routers/unpacked.py`: `APIRouter(**ROUTER_OPTIONS)` (its own route
  also loses every fact to row SH-F38: unpacking may set `route_class`)
  and `nested`, included into it (inherits the default, keeps its facts).
  Its prefix is not read (unpacking may set one), so both routes are
  stored without a path.
- `app/sub.py`: `FastAPI(**APP_OPTIONS)`, mounted on the plain app.
- `app/unread.py` (not imported by the oracle): an `include_router` call
  with keyword unpacking, and an include on an object not read (it passes
  on every default response class of the project).

The golden is `route-shape-fastapi-unpack.golden.json`.
`scripts/oracle/fastapi_shape.sh` runs FastAPI's spec and TestClient on it
too: the unaffected routes' success statuses are reached, the withheld
ones are shown changed where the request passes through an object that
sets the default. No ignore files or renamed ignored paths are needed.

# route-shape-fastapi-handlers fixture

Row SH-F37 of `docs/contract/claims/route-shape-fastapi.md` (project
exception handlers) and the unproven-version branch of row SH-F26. A
separate project because the handlers apply to every route of the index:

- `app/main.py`: a handler for Starlette's `HTTPException` (decorator), one
  for `RequestValidationError` and one for status 418
  (`add_exception_handler`), one for `Exception` (does not catch
  `HTTPException`); routes raising 404 and 418 and declaring a failing
  query parameter (their raised statuses and 422 are `intended`), and a
  return annotation without a FastAPI version in `pyproject.toml`
  (`returned`). `POST /people` takes `Person`, the unproven-Pydantic
  branch of row SH-F22: the manifest names no Pydantic version, so the
  nullable fields without a default (`nick`, `tag` aliased `t`) have
  presence `unknown` (Pydantic 1 gives them a None default); `count`
  (`= ...`) and `name` are required under both majors.

The golden is `route-shape-fastapi-handlers.golden.json`.
`scripts/oracle/fastapi_shape.sh` runs FastAPI's spec and TestClient on it
too: the lowered statuses are never sent. No ignore files or renamed
ignored paths are needed.

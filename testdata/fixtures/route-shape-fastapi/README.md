# route-shape-fastapi fixture

Rows SH-F1..SH-F39 and D (SH-F37 in the separate
`route-shape-fastapi-handlers` fixture) of `docs/contract/claims/route-shape-fastapi.md`
(the FastAPI route shape). Hand-authored, importable by FastAPI 0.161.1:

- `app/models.py`: Pydantic models with defaults, `T | None` without a
  default, `Field(alias=)`, `default_factory`, `extra="forbid"`, a base
  model, a forward reference (unknown), a computed field (unknown), an
  `Enum` and a `Literal`.
- `app/security.py`: one scheme of each stated class, `auto_error=False`,
  a module-constant `tokenUrl` and an f-string one (refused).
- `app/deps.py`: dependency functions (query, header, a path parameter
  declared by a dependency), `Annotated` aliases, a dependency reading the
  request (header and cookie, H3), a class with `__init__`, `Security`
  with scopes, a chain four functions deep and a lambda dependency.
- `app/helpers.py`: functions the request is passed to (one, two and three
  calls deep), a cycle, `**kwargs`, a builtin use, a function in a file
  outside gate SG (`app/plain.py`), and a function defined twice.
- `app/crud.py`: returned values with and without annotations, a model
  constructor, an `HTTPException` with a status constant and one inside
  `try`.
- `app/routers/items.py`, `app/routers/admin.py`, `app/main.py`: the
  routes, router and `include_router` dependencies, a router prefix with a
  path parameter, convertors, `include_in_schema=False`, a `JSONResponse`
  returned directly and a `Response` parameter whose status is set, models
  with a serializer, a `model_validator` and an `alias_generator`, and a
  raise in a helper whose call is inside `try` (not stated).
- `app/routers/review.py`: the classes of the SH-F review: `request.url`
  and `request.state` reads, a Response returned through an unannotated
  helper, `response_model_*` options, a form model, `populate_by_name`,
  `RedirectResponse` without a status, generator handlers, before/after
  validators and `_missing_`, a route whose only parameter cannot fail,
  a raise after `yield`, a repository OAuth2 subclass with `__call__`,
  `list[UploadFile]`, a reassigned request, `set_cookie` on a Response
  parameter and a router with a custom `route_class`.
- `pyproject.toml` requires `fastapi>=0.115.0`: the version-dependent rows
  (return annotations, form models) apply.
- `app/unread.py`: a SQLModel table class used as a body; not imported by
  the oracle (it needs no sqlmodel).

The golden `route-shape-fastapi.golden.json` is the default depth (6);
`route-shape-fastapi.depth2.golden.json` the chains at depth 2 (depth cap
reached). `scripts/oracle/fastapi_shape.sh` compares the app with
FastAPI's own `app.openapi()` and drives it with FastAPI's TestClient
(`fastapi_shape_runtime.py`). No ignore files or renamed ignored paths are
needed.

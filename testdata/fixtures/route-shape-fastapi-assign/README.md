# route-shape-fastapi-assign fixture

Row SH-F24 of `docs/contract/claims/route-shape-fastapi.md`: a
`default_response_class` set by attribute assignment. A separate project
because the assignment applies project-wide (neither the object nor the
order is modelled):

- `app/main.py`: `app.router.default_response_class = RedirectResponse`
  and `router.default_response_class = RedirectResponse` before the
  router is included. `/go` and `/hop` send 307 (no success item is
  stated); `/target` declares `response_class=JSONResponse` and keeps its
  200 `application/json` item.

The golden is `route-shape-fastapi-assign.golden.json`.
`scripts/oracle/fastapi_shape.sh` runs FastAPI's spec and TestClient on it
too. No ignore files or renamed ignored paths are needed.

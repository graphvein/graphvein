# Unresolved mount hops fixture (T029 FU-2)

Hand-written Go (chi, gin, net/http), Express, FastAPI and Axum syntax; no
external corpus source, nothing is installed or executed. No inert-name
mapping is needed.

Each framework has routes below a hop composition cannot read, so the
routes get no path and store the hop in the route attribute
`unresolved_mounts` (rows OG-94..OG-96 of
`docs/contract/claims/openapi-generation.md`):

- `goapp/chi.go`: `r.Route(base, ...)` with `base` a parameter (dozzle's
  form), a nested `Route` below it, two hops with the same text in
  `Twin` (ambiguous: no value can be tied to either), and in `Nested` a
  hop below the read prefix `/outer` (the value is only the hop's part).
- `goapp/gin.go`: `e.Group(prefix)` with `GET("/")` and `GET("")` (the
  prefix with and without a trailing slash).
- `goapp/mux.go`: a ServeMux behind a middleware that answers one path
  itself (miniflux's form: the router escapes into the wrapper, a hop
  from the root, and the call result is passed to the outer `Handle("/")`,
  a hop that adds nothing), `http.StripPrefix(prefix, api)` below
  `root.Handle(prefix+"/", ...)`, and `root.Handle(prefix+"/", api)`
  without StripPrefix (adds nothing: only `/` is accepted).
- `web/app.js`: `app.use(base, api)` with `base` from the environment,
  a code-read route equal to one of the placed paths up to parameter
  names (the refused placement is listed as may shadow it), a second hop
  whose only route has its own route value, and a hop below the read
  prefix `/x`.
- `api/app/main.py`: `include_router(items, prefix=PREFIX)` with `PREFIX`
  from the environment, `APIRouter(prefix=os.environ["OWN"])`, and a
  router with an unread own prefix included at the read prefix `/x`.
- `rustapp/src/lib.rs` (axum 0.8 by its `Cargo.toml` requirement, T029
  FU-3): `.nest(base, api())` with `base` a parameter; below it `/` routes
  (one through a `merge`), a constant inner `nest(V2, ...)`, a
  parameter route and `/slash/` (stored below the hop with its trailing
  slash); `twin` holds two `r.nest(&a, Router::new()...)` hops
  with the same text whose inline routers are elided in the hash
  (ambiguous); `outer` nests at a parameter below the read prefix
  `/outer` (`value_covers: hop`) and `stacked` nests at a parameter below
  another parameter nest (`value_covers: root`); `service_hop` is a
  `nest_service` hop whose nested `/` is placed with and without a
  trailing slash.
- `rustold/src/lib.rs` (axum 0.3): `.nest(&prefix, inner())`; the nested
  `/` route is not stored below the hop (axum 0.3 is outside the versions
  taken as checked).

Every route below a hop also stores the paths proven below it in
`unresolved_prefix_paths` (T033, ADR 0084; Go first, then Express,
FastAPI and Axum).

`mount-hops.golden.json` holds the route nodes and route diagnostics
(`internal/index`, `TestMountHopsGolden`);
`mount-hops.openapi.golden.json` the generated document with one value
per hop (`internal/openapi`, `TestFixtureDocuments`, configuration
`mountHopsConfig`).

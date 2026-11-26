# Unknown-prefix paths fixture, Express, FastAPI and Axum (T033)

Hand-written Express 4, FastAPI and Axum syntax; no external corpus
source, nothing is installed or executed. No inert-name mapping is needed.

Each case is a route whose prefix the code does not fix, or a negative
case next to one, for the route side of the unknown-prefix path contract
(rows UP-5..UP-10 and D of
`docs/contract/claims/unknown-prefix-paths.md`). Routes below a hop store
the hop in `unresolved_mounts` (T029 FU-2) and the paths proven below it in
`unresolved_prefix_paths`, with `prefix: "unresolved"`.

- `web/app.js` (`express` 4.21.1 by `web/package.json`): `app.use(config.base,
  api)` (UP-5) with parameter routes and a read prefix `/v1` below the hop;
  a hop below the read prefix `/x` (`above: "/x"`); a `use` inside an `if`
  (conditional); one router below two configured prefixes (one entry,
  the first hop's key, UP-8); one router at `/known` and below a configured
  prefix (it has a path: no entry, UP-9); a pattern no Express 4 row reads
  (`/files/:file.:ext`, no entry, D `unread_pattern`) beside a read one
  below the same hop; a router mounted only below the unread prefix
  `/old*` (D `unread_pattern`); a router nothing mounts (`route_unmounted`,
  UP-10).
- `web/app.test.js`: a hop in a test file (the entry has `scope: "test"`).
- `api/app/main.py` (FastAPI by `api/pyproject.toml`):
  `include_router(items, prefix=settings.api_prefix)` (UP-6) with a
  `{rest:path}` route (stored as `{*rest}`) and a `""` route right at the
  hop (not stored below the hop: D `not_stored`); `APIRouter(prefix=
  os.environ["OWN"])` included at the read prefix `/x` (UP-6, own prefix,
  `above: "/x"`); `app.mount(os.environ["SUB"], sub)` of a FastAPI
  sub-application (consumed, no root of its own: its route's path is an
  entry below the hop, UP-6 `mount(p, ...)`); an include inside an `if`
  (conditional); one router included at `/known` and at a configured
  prefix, and the `/health` route, both registered after that mount,
  whose prefix is not read: it may take a later path or not, so their
  paths are kept, conditional, with assumption RO-V1 naming the mount
  (`/known/shared/item` is a full path, so that route gets no
  entry, UP-9); a router with an unread prefix that nothing includes
  (`route_unmounted`, UP-10); `site.mount(os.environ["STATIC"],
  StaticFiles(...))` before `site.include_router(site_api, prefix="/api")`
  and `@site.get("/health/live")` (RO-V1 on `/api/items/list` and
  `/health/live`; FastAPI 0.165.0 serves both with `STATIC=/static` and
  answers 404 where the prefix covers them), and the router `scoped`
  included below `settings.api_prefix` here and at `/t` by
  `api/tests/test_scoped.py` (UP-9 per scope: the test-scope path
  `/t/scoped/item` and the production entry `…/scoped/item`).
- `web-esm/` (ESM): `src/app.js` mounts the router of
  `src/routes.js` with `app.use(process.env.BASE, router)` and
  `test/routes.test.js` mounts it at `/t`: the route keeps the test-scope
  path `/t/items/list` and the production entry `…/items/list` (UP-9 per
  scope).
- `rustapp/src/lib.rs` (axum 0.8 by its `Cargo.toml`): `.nest(&cfg.base,
  api())` (UP-7, nest) with `/` (stored as `""`, UP-10), a parameter, a
  `{*rest}` catch-all, `/slash/` (`trailing_slash`), a chained `get(h).post(h)`
  and a read inner `nest("/v1", ...)`; `.nest_service(base, served())`
  (UP-7, nest_service: the nested `/` as `""` and `/`); a hop below the
  read prefix `/x`; a router nested at `/known` and at a parameter (UP-9).
- `rustold/src/lib.rs` (axum 0.3, outside the checked versions): below
  `.nest(&prefix, inner())` the nested `/` is not stored (D `not_stored`)
  and `/items/:id` is; a root route `/page/` whose trailing slash is not
  proven (no path, no entry, D `slash_unproven`).

`unknown-prefix-mounts.golden.json` holds the route nodes and route
diagnostics (`internal/index`, `TestUnknownPrefixMountsGolden`);
`internal/extract/routes`' `TestUnknownPrefixWebContractTags` checks that
the fixture reaches every row and form of the unit.

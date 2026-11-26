# FastAPI mounts fixture (T035)

Hand-written FastAPI syntax; no external source, nothing is installed or
executed by the tests. No inert-name mapping is needed.

A FastAPI app a Starlette mount consumes answers only below the mount
(checked against FastAPI 0.121.1 with Starlette 0.49.3 and FastAPI 0.165.0
with Starlette 1.10.0: 404 at the sub-app's own paths), so it is no root of
its own (`docs/extract/routes.md`, FastAPI section):

- M1 `app.mount("/sub", sub)`: `/sub/ping` is stored as `/sub/sub/ping` only.
- M2 `env_host.mount(os.environ["SUB"], env)`: no path; the hop is stored
  in `unresolved_mounts` (on an app of its own, since such a mount may take
  any later route of its app, M19).
- M3 `include_router` inside the mounted sub-app: `/m/inc/r/x` only.
- M4 nested mounts: `/a/b/leaf` only.
- M5 a mount inside an `if`: the composed path is conditional, and there is
  no root path.
- M6, M7 Starlette `Mount(...)` in the routes list of a `FastAPI(...)`
  (imported from `starlette.routing`, and through `fastapi.routing`):
  composed as `app.mount`: `/lst/listed`, `/lst2/listed2`.
- M8 `app.router.mount(...)` is `app.mount(...)`: `/raw/raw`.
- M9 `Mount` of a `StaticFiles` app, and a project class named `Mount`
  (`app/local.py`): neither consumes an app; `kept` keeps `/kept`.
- M10 `tests/test_mounts.py` mounts the production app `standalone`: a
  test-scope mount does not consume it (`/alone`, plus the test-scope
  `/t/alone`).
- M11 a routes-list `Mount` whose prefix is not a constant: no path; the
  hop is stored in `unresolved_mounts`.
- M12 `Mount` in an `APIRouter(routes=...)` list, the router included in an
  app: not composed (FastAPI 0.121.1's `include_router` copies no `Mount`),
  no path (`route_mount_unresolved`).
- M13 a routes list bound to a name first: not read, no path.
- M14 `x.router.mount` with a prefix that is not a constant: the hop.
- M15 a mount on an `APIRouter` the app includes: FastAPI serves
  it only from 0.137.0, and `api/pyproject.toml` proves no version: no
  path (`route_mount_unresolved`).
- M16, M17 a route added after `app.mount("/sh", ...)` below the
  prefix (`/sh/a`, `/sh/`, and `/sh/inc-after` of a router included after
  the mount) is never reached: no path (`route_shadowed`); `/sh/early`, the
  router included before the mount (`/sh/inc-before`), `/sh` itself and
  `/{v}/c` keep their paths.
- M18 a mount of a `StaticFiles` app takes `/files/list` all the same.
- M19 a mount whose prefix is not read may take any later route:
  `/dyn/late` has no path, `/dyn/early` keeps it.
- M20 an app mounted and also served by `uvicorn.run` under the main
  guard keeps its own root, conditional (`/own`), beside `/srv/own`.
- M21 an app a test serves with `uvicorn.run`: no own root (`/sbt/by-test`
  only).
- `api-new/` requires `fastapi>=0.137.0`: N1 a mount on an
  included `APIRouter(prefix="/p")` composes below the include prefix
  without the router's own prefix (`/i/rm/s`); N2 the same at a prefix
  that is not a constant: no path; N3 `[tool.fastapi] entrypoint` names
  `newapp.main:app`, which the gateway also mounts: `/hello` and
  `/gw/hello`.

Unknown-prefix paths (row UP-6, `mount(p, ...)`): the routes below a
mount hop whose prefix is not a constant also store their path below the
hop in `unresolved_prefix_paths` (M2 `/env/ping`, M11 `/listed-env`, M14
`/raw-env`). M19's `/dyn/late` is shadowed (`route_shadowed`), stores no
hop and gets no entry.

The fixture oracle ran it with `SUB=/e ADMIN=1 LST=/le
RAW=/re DYN=/dy RM=/rme` and a `static/` directory created in a scratch
copy.

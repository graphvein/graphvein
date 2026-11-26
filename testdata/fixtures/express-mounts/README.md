# Express mounts fixture (T035)

Hand-written Express code; no external source, nothing is installed or
executed by the tests. No inert-name mapping is needed. The apps are
exported so an oracle can serve them.

An Express app another app or router mounts with `use` answers only below
the mount (checked against Express 4.21.1 and 5.1.0: 404 at the sub-app's
own paths on the app that mounts it), so it is no root of its own
(`docs/extract/routes.md`, Express "Composition and diagnostics"):

- X1 `app.use("/sub", sub)`: `/sub/ping` is stored as `/sub/sub/ping` only.
- X2 `app.use(process.env.SUB, env)`: no path; the hop is stored in
  `unresolved_mounts`.
- X3 a router used inside the mounted sub-app: `/m/inc/x` only.
- X4 nested sub-apps: `/a/b/leaf` only.
- X5 a mount inside an `if`: the composed path is conditional, and there is
  no root path.
- X6 a sub-app mounted through a router mounted on the app: `/out/in/vr`
  only.
- X7 `app.use(flat)` without a prefix: `/flat`, the same path through the
  mount.
- X8 an array of prefixes (not read): no path; the hop is stored.
- X9 `holder.server.use("/raw", raw)`, a receiver the linker does not read:
  no path (`route_mount_unresolved`).
- X10 `tests/app.test.js` mounts the production app `standalone`: a
  test-scope mount does not consume it (`/alone`, plus the test-scope
  `/t/alone`).
- X11 a route added after `app.use("/sh", shadowSub)`: Express
  passes a request the mounted app does not answer on, so `/sh/b` is the
  parent's; at `/sh/a` the sub-app's route comes first, recorded as an
  earlier route handler in `mounted_middleware_unknown` (DA-E1), the path
  kept as for every earlier route handler.
- X12-X15 apps mounted in `app.js` and also served at their own
  root in `serve.js`: `served.listen(...)`, `http.createServer(created)
  .listen(...)`, a const `https.createServer({}, bound)` then
  `server.listen(...)`, and `later.listen(...)` inside a function
  (conditional): each keeps its own-root path beside the mounted one.
- X16 an app only `tests/app.test.js` serves with `listen`: no own root.
- X17, X18 a sub-app or router whose own stack holds a
  pathless middleware (a 404 catch-all) mounted before a parent route below
  its prefix (`/z/b`, `/api/status/now`): Express runs the mounted stack
  first, and the catch-all answers 404 (Express 4.21.1 and 5.1.0). The
  paths are kept, conditional, with assumption RO-V2 and
  `may_answer_first` naming the mount; never dropped (middleware that calls
  `next()` lets the route answer).
- X19 parent routes outside the mount's prefix (`/zz/c`, and `/zz/*`, whose
  wildcard does not hide the literal mismatch with `/z`): unconditional.

Unknown-prefix paths (row UP-5): the routes below a `use` hop whose prefix
is not a constant also store their path below the hop in
`unresolved_prefix_paths` (X2 `/env/ping`, X8 `/multi`).

The oracle serves the exported apps itself and does not
import `serve.js`.

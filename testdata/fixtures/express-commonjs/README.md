# Express CommonJS fixture (T036)

Hand-written Express code in CommonJS; no external source, nothing is
installed or executed by the tests. No inert-name mapping is needed.
`web/app.js` exports the app so an oracle can serve it.

A router a module exports with CommonJS and another module mounts with
`require` composes as through an ESM import (`docs/extract/routes.md`,
Express "CommonJS modules", rows CJ-U1..CJ-U4 and CJ-E1..CJ-E4):

- C1 `app.use("/a", require("./routes/a"))`, `module.exports = router`:
  `/a/x`, `/a/items/{id}`, `/a/list` (the named handler gets its
  `exposes_route` edge).
- C2 the require bound to a const first: `/b/y` (the router comes from a
  destructured `const { Router } = require("express")`).
- C3 `module.exports = { router }` read as `require("./routes/c").router`:
  `/c/z`.
- C4 `exports.router = router`, `const { router: dRouter } = require(...)`:
  `/d/w`.
- C5 `module.exports.items = items`, `const { items } = require(...)`:
  `/e/v`.
- C6 a directory required through its `index.js`: `/f/u`.
- C7 a factory function exported with `module.exports = makeRouter`, called
  at the mount: `/g/t`.
- C8 a mount inside an `if`: `/h/s`, conditional.
- C9 `module.exports = require("./i-inner")`: `/i/r`.
- C10 a prefix read from the environment: no path; the hop is stored in
  `unresolved_mounts` and `/q` below it in `unresolved_prefix_paths`.
- C11 a router without a prefix: `/k/p`.
- C12 a factory exported in an object literal (`module.exports = {
  makeItems }`), destructured and called with an argument: `/l/o`.

`web/jsconfig.json` sets `"moduleResolution": "NodeNext"` without
`"module"`, under which the compiler would refuse the extensionless
specifiers: `require` is resolved as Node's loader resolves it, so every
case above composes regardless.

`web/refused/app.js` mounts modules no row reads; each router keeps no
path (`route_mount_unresolved` at the mount, `route_unmounted` at the
route). The oracle does not serve this app (Express throws at some of the
mounts):

- R1 `require(process.env.ROUTES_MODULE)`: the specifier is no literal.
- R2 `flag ? require("./r2a") : require("./r2b")`.
- R3 `module.exports` written inside an `if`.
- R4 `module.exports` written twice.
- R5 an ES module (`export default router`): `require` returns its
  namespace object.
- R6 `exports = router` rebinds the local name only.
- R7 the exports object `{ router }` itself mounted.
- R8 a module that does not exist.
- R9 a `.ts` module required from JavaScript.
- R10 exports written through `Object.assign(module.exports, ...)`.
- R11 a destructured name with a default value.

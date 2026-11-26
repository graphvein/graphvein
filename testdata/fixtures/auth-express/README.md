# auth-express fixture (T026 AG-B4b)

Hand-written JavaScript (ES modules, plus one CommonJS file); no corpus
source, no inert-name mapping needed (no ignore files, no `node_modules`).
`TestAuthExpressContract` indexes it (rows DA-E1, DA-E2, DAD of
docs/contract/claims/architecture-graph.md; golden `auth-express.golden.json`).

- `app.js`, `api.js`, `admin.js`, `mw.js`, `setup.js`: `use` ordering,
  paths (literal, parameter, case, dynamic, array, computed), mounts, a
  sub-app, an earlier sibling router, an escape (`configure(app)`), a
  conditional `use`, a spread and a `use` inside a function (app `other`).
  Runnable: the oracle (`scripts/oracle/express_middleware.sh`) serves
  `app`, `sub` and `other` with Express 4.21.1 and 5.1.0 and sends the
  requests of `oracle.json`; each middleware records its name as written.
- `setup.js`, `configured.js` (AG-F, row DA-E3): module functions that
  receive the app as a parameter: read at the call (`configure`,
  `configureAll` with an object pattern, a guarded use and a use after an
  early return) or left as escapes (called twice, async, aliased, passed
  on). `app.js` re-exports the `configured.js` apps for the oracle.
- `libs.js`, `options.js`, `cjs.cjs`: the DA-E2 auth library factories
  (syntax only, never loaded; the packages are not installed).
- `earlier.js`, `earlier4.js`, `express5/earlier5.js` (AG-RF, rows DA-E1,
  DA-E4, DA-E5): earlier route handlers; `earlier4.js` is Express 4 syntax,
  `express5/earlier5.js` Express 5 syntax under its own `package.json`
  declaring `express` ^5.1.0, so its paths are read as Express 5 reads them
  (T028, `docs/contract/claims/express5-paths.md` row EX5-W12).

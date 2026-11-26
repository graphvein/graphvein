# express5-paths fixture (T028)

Hand-written JavaScript (ES modules); no corpus source, no inert-name
mapping needed (no ignore files, no `node_modules`). `TestExpressPathContract`
indexes it and checks `express5-paths.golden.json` (routes, their path
entries and the route diagnostics) and that every row of
[the Express path claim contract](../../../docs/contract/claims/express5-paths.md)
is reached.

- `v4/`: `package.json` declares `express` 4.21.1 (row EX5-W12): literal,
  parameter, optional parameter and `*` forms (EX5-W1..EX5-W4), a parameter
  mount prefix, and Express 4 forms no row reads (EX5-W14: `/*splat`,
  `ab?cd`, `:file.:ext`, a `*` mount prefix).
- `v5/`: `package.json` declares `express` ^5.1.0 (EX5-W12): literal,
  parameter, named wildcard (the T028 case `/r5` + `/*splat`), whole-pattern
  wildcard and group forms (EX5-W5..EX5-W9), and Express 5 forms no row
  reads (EX5-W14). `invalid.js` holds patterns Express 5 refuses (EX5-W10);
  it never loads under Express 5.
- `unknown/`: `package.json` declares `^4.21.1 || ^5.1.0` (EX5-W13): only
  the forms both majors read the same are claimed (EX5-W11).
- Later cases (T028): `v4/` and `v5/` also hold a non-final
  optional parameter in a pattern and in a prefix (B2), pieces without a
  leading slash (B4), an empty segment, a space, `^` and non-ASCII text
  (C1), and groups whose alternatives keep an empty segment or a trailing
  slash (B1). Major detection: `bom/` (a manifest with a UTF-8 byte order
  mark, Express 5), `nested/sub/` (a nested `{"type": "module"}` manifest
  without express under an Express 5 one), `nested/broken/` (a manifest
  that does not parse: unknown, never loaded by the oracle), `ws/` (the
  workspace root declares Express 5), `peerdev/` (peer `^4.21.1 || ^5.1.0`
  with development `^5.1.0`: unknown), `pnpm/` (`catalog:` to Express 5,
  `catalog:legacy` to Express 4 through `pnpm-workspace.yaml`).
- `oracle.json`: the requests `scripts/oracle/express_paths.sh` sends to
  each app (each also with a trailing slash) under Express 4.21.1 and 5.1.0,
  the documented recall gaps (EX5-W3, EX5-W4, EX5-W8) and the patterns
  Express 5 must refuse. Every
  route sits under its own first segment so no two routes of one app match
  the same request.

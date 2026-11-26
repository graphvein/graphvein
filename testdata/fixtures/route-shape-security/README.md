# route-shape-security

Fixture of the route security claim contract
(`docs/contract/claims/route-shape-security.md`, rows SH-S1..SH-S23): one
small app per framework.

- `express/`: express-jwt, express-oauth2-jwt-bearer, express-basic-auth,
  passport with passport-jwt, passport-http-bearer and passport-http,
  express-openid-connect, a repository middleware (`custom.js`), a mounted
  router and a conditional mount; the built-in session strategy, a
  strategy list, passport-anonymous and `requiresAuth` with a
  `requiresLoginCheck` argument. `server.js` serves the app for the
  oracle.
- `goapp/`: gin (`ginapp`: Group arguments, Use before and after routes,
  Use in a called function and in a function literal, a conditional Use,
  routes reached through two groups, a `goto` skipping a Use in
  `jump.go`) and chi (`chiapp`: jwtauth Verifier,
  Verify and Authenticator, BasicAuth with With, a router mounted through a
  wrapper in `served.go`; in `review.go` a `goto` skipping a Use, a route
  replaced by a later registration and a parent's static route matched
  before a mounted router's).
- `axumapp/`: typed Authorization extractors and tower-http
  ValidateRequestHeaderLayer in `src/lib.rs`; `src/extra.rs` is not in the
  crate's module tree (the oracle does not compile it) and holds an
  axum-login layer, a from_fn layer and a repository extractor.
- `nextapp/`: an App Router handler reading the next-auth session.

The golden `route-shape-security.golden.json` (TestRouteSecurityGolden)
lists every route's security section and the gin and chi router middleware
attributes; `route-shape-security.openapi.golden.json` is the generated
document. `scripts/oracle/route_security/run.sh` serves the Express, gin,
chi and Axum apps with the real libraries and checks every claimed
credential form. No file here is an ignore file or an ignored path.

# Client exact literal precision fixture

T017b. The calls in `main` have an environment-shaped base
(`process.env.API`), so the base is `unresolved`. Every accepted candidate
must match at least one literal call segment with a literal route segment:
literal-free routes are never candidates (Rule E), and a suffix remainder
must start with a literal or with a parameter matched by a call parameter
(Rule P). An exact match needs the same anchor: a route that starts with a
parameter is a candidate only when the call starts with a parameter
(Revision 1). The calls in `resolved` have a known base (`none`) and keep
their literal-free and parameter-first exact links.

| Call | Base | Expected |
|---|---|---|
| `GET /health` | unresolved | unmatched (E; was exact to `GET /:id`) |
| `POST /anything` | unresolved | unmatched (E; was exact to `ANY *`) |
| `GET /profile/t017b-edit` | unresolved | unmatched (P on `/t017b-users/:id/t017b-edit`, then E on `ANY *`) |
| `GET /${tenant}/t017b-reports` | unresolved | suffix 0.6 to `/t017b-api/:tenant/t017b-reports` |
| `GET /t017b-items` | unresolved | suffix 0.6 to `/t017b-api/t017b-items` |
| `GET /t017b-users/7/t017b-edit` | unresolved | exact 0.9 to `/t017b-users/:id/t017b-edit` |
| `GET /account/t017b-settings` | unresolved | unmatched (Revision 1 on `/:org/t017b-settings`, then E on `ANY *`) |
| `GET '/health'` | none | exact 0.9 to `GET /:id` |
| `POST '/anything'` | none | exact 0.9 to `ANY *` |
| `GET '/account/t017b-settings'` | none | exact 0.9 to `/:org/t017b-settings` |

Inert name: the literal-free routes (`GET /:id`, `ANY *`) are stored as
`fallback.ts.inert`, and the test restores it to `fallback.ts`. As a live
file in the repository they would become exact candidates of unmatched
resolved-base calls and OpenAPI operations of every other fixture in
Graphvein's self-index (T017b investigation). No verb-specific catch-all avoids
that, because every Express verb has such a call there. No ignore-file or
directory mapping is needed. The parameter-first route
`/:org/t017b-settings` stays live in `backend.ts`: its unique literal gives it
no candidates outside this fixture in the self-index (T017b Revision 1).

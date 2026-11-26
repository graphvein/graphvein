# Unknown-prefix tier fixture (T033)

Hand-written Go (chi v5, net/http) and TypeScript syntax; no external
corpus source, nothing is installed or executed. No inert-name mapping is
needed.

The routes in `server/` are served below prefixes read from configuration
(`r.Route(base, ...)`, `http.StripPrefix(prefix, ...)`), so they have no
`paths` entry, only `unresolved_prefix_paths` entries (route side, rows
UP-1 and UP-2). The calls in `web/calls.ts` exercise the client matcher's
unknown-prefix tier, rows UP-13..UP-20 of
`docs/contract/claims/unknown-prefix-paths.md` (copied in
`docs/extract/clients.md`, "Unknown-prefix tier"). Every path segment
starts with `upb` so that no other fixture's call matches these routes in
an index of the whole repository.

| Line | Call | Row | Expected |
|---|---|---|---|
| 2 | `GET /v1/upb/orders/${id}` | UP-14 | linked 0.6 to `GET /upb/orders/{id}` (V = `/v1`), `assumes: ["UP-V1"]` |
| 3 | `POST /upb/orders` | UP-14 | linked 0.6 to `POST /upb/orders` (empty V) |
| 4 | `GET /v1/upbping` | UP-16 | unmatched: one literal segment |
| 5 | `GET /v1/upb/shared/list` | UP-17 | ambiguous: the same tail below two bases (2 candidates) |
| 6 | `GET /v1/upb/stats/daily` | UP-17 | ambiguous: `/daily` matches with one literal segment; no ranking (2 candidates) |
| 7 | `GET /v1/acme/upb/settings` | UP-16 | unmatched: `acme` would fill the route's leading `{org}` (not anchored) |
| 8 | ``GET /v1/${org}/upb/settings`` | UP-14 | linked 0.6 to `GET /{org}/upb/settings` (a call parameter takes `{org}`) |
| 9 | ``GET /${tenant}/upb/orders/${id}`` | D | unmatched: a call parameter inside the unknown run |
| 10 | `GET /v1/upb/files` | UP-18 | unmatched: chi serves `/upb/files/` only in its own slash form |
| 11 | `GET /v1/upb/files/` | UP-14 | linked 0.6 to `GET /upb/files/` |
| 12 | `GET /k/upb/known/items` | UP-13 | linked 0.9 (exact) to the route with a full path; the unknown-prefix route with the same tail is not considered |
| 13 | `GET /z/upb/known/items` | UP-14 | linked 0.6 to the unknown-prefix route |
| 14 | ``HEAD /v1/upb/reports/${id}`` | UP-18 | linked 0.6 to the net/http `GET /upb/reports/{id}` (net/http GET answers HEAD) |
| 15 | ``HEAD /v1/upb/orders/${id}`` | UP-18 | unmatched: chi's GET does not answer HEAD |
| 16 | `GET /v1/upb/testonly/items` | UP-18 | test_only: only a test-file route matches |
| 17 | ``GET /outer/v2/upb/inner/${id}`` | UP-14 | linked 0.6 to `GET /upb/inner/{name}` (`above: "/outer"`, V = `/v2`) |
| 18 | ``DELETE /v1/upb/orders/${id}`` | UP-18 | unmatched: no route of the method |
| 21 | ``${import.meta.env.VITE_API}/upb/orders/${id}`` | UP-15 | linked 0.6 to `GET /upb/orders/{id}`, `assumes: ["UP-V1", "UP-V2"]` |
| 22 | ``${import.meta.env.VITE_API}/v1/upb/orders/${id}`` | UP-15 | unmatched: the template is not the entry's template |
| 23 | ``${import.meta.env.VITE_API}/outer/upb/inner/${id}`` | UP-15 | unmatched: the entry has `above`, which the call's base cannot stand for |
| 24 | ``https://api.example.com/v1/upb/orders/${id}`` | UP-20 | unmatched: a literal origin |
| 27 | `GET /k/app/upb/widgets/list` | UP-22 | ambiguous: the existing tiers find only the known final catch-all `GET /app/*` (served at `/k/app/{*}`), and `GET /upb/widgets/list` below the unknown prefix matches it: both are candidates, neither is linked |
| 28 | `GET /k/app/other/page` | UP-13 | linked 0.9 to the catch-all: no unknown-prefix route matches |
| 29 | `GET /k/app/daily` | UP-22 | ambiguous: `GET /daily` matches with one literal segment (a competitor as UP-17 counts them; UP-16's minimum is not needed for an ambiguity) |

`unknown-prefix.golden.json` holds the routes' `paths` and entries, the
`calls_route` edges and the client calls (`internal/index`,
`TestUnknownPrefixLinks`, which also checks each call above independently
of the golden and that the fixture reaches every matcher row).
`TestStoredMatcherReplaysClientLinks` replays every alternative with the
stored matcher. Express routes below a configured base are not in this
fixture: their entries come with the Express route side (row UP-5).

The surfaces (T033) are checked on this fixture by
`internal/server` (`TestUnknownPrefixGoldens`: goldens
`internal/server/testdata/unknown-prefix-{endpoints,endpoint,endpoint-above,flow,chain,architecture}.golden.json`;
`TestUnknownPrefixRows`) and `internal/mcp` (`TestUnknownPrefixSessions`,
golden `internal/mcp/testdata/session-unknown-prefix.golden.json`, part
`up`).

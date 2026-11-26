# Axum trailing slash fixture (T029 FU-3b)

Hand-written Axum syntax; nothing is installed or executed. No inert-name
mapping is needed.

Four crates hold the same router, each with another axum requirement:
`v08` (`0.8`), `v0620` (`=0.6.20`), `v034` (`0.3.4`) and `unproven`
(`>=0.6`, not read as one version). The router has a pattern with a
trailing slash (`/t/`), one without (`/u`), a nest at `/n` of a router
with `/`, `/t/` and `/u`, and a nest at `/p/` of a router with `/x` and
`/`, and (except `v034`: axum 0.3 has no `nest_service`) a
`nest_service("/svc", ...)` of a router with `/`, `/a` and `/t/`, and a
`nest_service("/wrap", ServiceBuilder::new().service(wrapped()))` whose
router is wrapped (its route `/w` gets no path).

Checked against axum 0.8.8, 0.7.9, 0.6.20 and 0.3.4 on 127.0.0.1 (the
T029 FU-3 task record, internal): 0.6.20 to 0.8.8 answer `/t/`, `/u`,
`/n`, `/n/t/`, `/n/u`, `/p/x` and `/p/` (and 404 the other slash
variants); 0.3.4 answers `/p//x` for the nest at `/p/` and redirects
`/t` and `/u/`. So `v08` and `v0620` keep the slashes; `v034` and
`unproven` claim only the paths without a trailing slash (`/u`, `/n`,
`/n/u`, `/svc/a`) and report `route_trailing_slash_unproven` for the
others. For `nest_service`, 0.6.20 to 0.8.8 answer the router's `/` at
`/svc` and `/svc/` (404 at `/`), `/a` at `/svc/a` and `/t/` at `/svc/t/`.

`axum-trailing-slash.golden.json` holds the route nodes and diagnostics
(`internal/index`, `TestAxumTrailingSlashGolden`). The four crates share
one router, so a generated document would hold only conflicts; the
placement of a trailing-slash path in a document is covered by the
mount-hops fixture (`/slash/` below an Axum hop).

# Link path assumptions fixture

Hand-written Express, FastAPI and Go routes with a TypeScript client; no
external source, nothing is installed or executed by the tests. No
inert-name mapping is needed.

A client link through a route path entry that rests on a path assumption
of the route side carries the assumption ids in its `assumes`
(`docs/extract/clients.md`, "Path assumptions on links"); the confidence
is unchanged. `client/calls.ts`, one call per line:

- A1 `/z/b`: an Express router whose stack ends with a 404 middleware is
  mounted at `/z` before the parent route: RO-V2.
- A2 `/y/c` and `/w/c`: one router mounted below `/y` after a catch-all
  router and below `/w`; the route side marks RO-V2 per route (every
  entry), so both links carry it.
- A3 `/plain/one`: a plain route, no assumption.
- F1 `/health/live`: a FastAPI mount whose prefix is read from the
  environment is added before the route: RO-V1.
- G1 `/gw/items/all`: a net/http mux served through a wrapper that
  changes the path: GR-V5.
- G2 `/v1/gp/orders/list`: a mux below a prefix the caller passes
  (`unresolved_prefix_paths`) served through the same wrapper: linked by
  the unknown-prefix tier at 0.6 with UP-V1, then GR-V5.
- G3 `/gb/items/all` and `/direct/gb/items/all`: one mux served through
  the wrapper (GR-V5) and, below `/direct`, without it: each link carries
  what its own entry rests on (GR-V5, none).

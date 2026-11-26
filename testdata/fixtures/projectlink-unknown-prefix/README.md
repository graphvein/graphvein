# Project link fixture: unknown-prefix routes (T033 UP-C)

Three hand-written member repositories for the project link pass
(`internal/projectlink`, docs/extract/project-links.md, rows PL-R13 and
PL-L7; rows UP-21, UP-25 and UP-26 of
docs/contract/claims/unknown-prefix-paths.md). No external corpus source;
nothing is installed or executed. No inert-name mapping is needed.

- `web/`: a frontend whose `withBase` helper prepends a base read from the
  page configuration (row UP-39), with calls to `/upd/orders/list` (through
  `withBase`), `/v1/upd/order/{id}` (a full path, V = `/v1`), the same
  list path with the literal origin `https://api.example.com`,
  `/upd/shared/items` and `/upd/tie/items` (through `withBase`),
  `/updping` (one literal segment) and `/upd/order/{id}` below
  `import.meta.env.VITE_API`.
- `api/`: chi routes below configured bases (`r.Route(base, ...)`,
  `r.Route(other, ...)`): no route has a path, only
  `unresolved_prefix_paths` entries; `/upd/tie/items` is served below both
  bases (a tie inside the provider).
- `api2/`: a chi route `/upd/shared/items` below a configured base.
- `scenarios/*.yaml`: project manifests read by
  `TestUnknownPrefixProjectScenarios`:

| Scenario | Declaration | Expected |
|---|---|---|
| `undeclared` | none | every call reaching only unknown-prefix routes is `unproven` (PL-R13); the literal-origin call PL-R5; `/updping` PL-R8 (UP-16) |
| `declared` | `web` consumes `api` | the list, order and shared calls link to `api` at 0.6 (PL-L7, `assumes` UP-V1, UP-V3 and UP-V2 for an unresolved base); `/upd/tie/items` ambiguous (PL-R10, UP-17); the literal origin not named (PL-R5) |
| `declared-origin` | `web` consumes `api` with `origins: [https://api.example.com]` | only the literal-origin call links (PL-L7 through row UP-21); the other calls are not covered (PL-R6) |
| `declared-two` | `web` consumes `api` and `api2` | `/upd/shared/items` ambiguous across the two providers (PL-R11); the others as in `declared` |

Goldens: `projectlink-unknown-prefix.<scenario>.golden.json` (links with
runs replaced by the member name, and every outcome).

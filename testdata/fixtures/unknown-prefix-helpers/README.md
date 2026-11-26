# Unknown-prefix call side fixture (T033 UP-C)

Hand-written Go (chi v5) and TypeScript syntax; no external corpus source,
nothing is installed or executed. No inert-name mapping is needed.

`server/api.go` serves its routes below a base read from configuration
(`r.Route(base, ...)`, as dozzle's `createRouter` does), so they have no
`paths` entry, only `unresolved_prefix_paths` entries (row UP-1); one
route (`/k/upc/known/items`) has a full path. `web/config.ts` builds the
base from the parsed page configuration (`{ maxLogs: 400, ...pageConfig }`,
as dozzle's `assets/stores/config.ts`) and declares URL helpers of the
shapes rows UP-39 and UP-40 of `docs/contract/claims/unknown-prefix-paths.md`
(copied in `docs/extract/clients.md`, "Unknown-prefix call side") name;
`web/calls.ts` calls them; `web/auto.ts` calls two of them without an
import, as dozzle's modules do, through the generated
`web/auto-imports.d.ts` (row UP-41). Every path segment starts with `upc` so that no
other fixture's call matches these routes in an index of the whole
repository.

Lines are in `web/calls.ts` unless a file is named.

| Line | Call | Row | Expected |
|---|---|---|---|
| 4 | `withBase("/upc/api/version")` (template helper) | UP-39 | base `unresolved` (`config.base`), linked 0.6 by UP-15 to `GET /upc/api/version`, `assumes: ["UP-V1", "UP-V2"]` |
| 5 | `withBase("/upc/api/profile")`, PATCH | UP-39 | linked 0.6 to `PATCH /upc/api/profile` |
| 6 | ``plusBase(`/upc/hosts/${id}/logs`)`` (`X + p` helper) | UP-39 | linked 0.6 to `GET /upc/hosts/{host}/logs` |
| 7 | `fnBase(...)` (function declaration) | UP-39 | linked 0.6 to `GET /upc/api/version` |
| 8 | `rootBase(...)` (`X` a module const read from the page configuration) | UP-39 | base expression `BASE`, linked 0.6 |
| 9 | `withBase("/upchealth")` | UP-39 | unmatched: one literal segment (UP-16, dozzle's `/healthcheck`) |
| 10 | `withBase("/upc/shared/list")` | UP-39 | ambiguous: the same template below two bases (UP-17) |
| 11 | `withBase("/upc/inner/x")` | UP-39 | unmatched: the only entry has `above` (UP-15) |
| 12 | `withBase("/upc/known/items")` | UP-39 | linked 0.6 by the existing suffix tier to the route with a full path (UP-13: the unknown-prefix tier is not reached) |
| 13 | `withBase(source)` | UP-40 | dynamic: the argument is a variable |
| 14 | `withBase("upc/api/version")` | UP-40 | dynamic: no leading `/` |
| 15 | ``withBase(`${source}?window=1`)`` | UP-40 | dynamic: the argument starts with a substitution |
| 16 | `letBase(...)` | UP-40 | dynamic: the base is a `let` |
| 17 | `callBase(...)` | UP-40 | dynamic: the base is a call |
| 18 | `writtenBase(...)` | UP-40 | dynamic: the base object is written to (`written.base = "/z"`) |
| 19 | `knownBase("/upc/known/items")` | UP-40 | the ordinary evaluation reads the base `/k`: linked 0.9 (exact) to `/k/upc/known/items` |
| 20 | `missingBase(...)` | UP-40 | dynamic: a known object without the property (`undefined` at run time) |
| 21 | `twoParams(...)` | D | dynamic: two parameters |
| 22 | `suffixed(...)` | D | dynamic: a piece after the parameter |
| 23 | `prefixed(...)` | D | the ordinary evaluation (`/{base}/upc/api/version`, unmatched) |
| 25 | `local(...)` (a helper inside the function) | UP-40 | dynamic: not bound at the module's top level |
| `auto.ts` 2 | `withBase("/upc/api/version")` without an import | UP-41 | bound by `web/auto-imports.d.ts` (generated header, `const withBase: typeof import('./config').withBase`), then UP-39: linked 0.6 |
| `auto.ts` 3 | ``withBase(`/upc/hosts/${id}/logs`)`` without an import | UP-41 | linked 0.6 to `GET /upc/hosts/{host}/logs` |
| `auto.ts` 4 | `letBase(...)` without an import | UP-41 | bound, then refused by UP-40: dynamic |
| `plain/calls.ts` 2 | `plusBase(...)` without an import | D | dynamic: `web/plain/auto-imports.d.ts` has no generated header, so it binds nothing |

`unknown-prefix-helpers.golden.json` holds the routes' `paths` and
entries, the `calls_route` edges and the client calls (`internal/index`,
`TestUnknownPrefixHelpers`, which also checks each call above
independently of the golden and that the fixture reaches every call-side
row). `TestStoredMatcherReplaysClientLinks` replays every alternative with
the stored matcher. A refused call keeps exactly the ordinary evaluation's
result: the rows evaluate the base and the argument first and restore the
evaluation's state before falling back.

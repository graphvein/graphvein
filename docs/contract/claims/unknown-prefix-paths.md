# Claim contract: unknown-prefix paths

**Status:** in force (ADR 0084, accepted 2026-11-24). Rows `UP-1`..`UP-49` are reserved
for this contract; the numbers missing below are reserved for rows a later
unit adds as a reported contract change. Recorded in the T033 task record
(internal).

Implementation status: the route
side (rows UP-1..UP-10 and D) is implemented for Go routers, Express,
FastAPI and Axum; the code table is `unknownPrefixContract` in
`internal/extract/routes`, copied into
[docs/extract/routes.md](../../extract/routes.md) ("Unknown-prefix
paths"), and `TestUnknownPrefixContract` keeps the three 1:1. Decided
while implementing and now stated in the rows: UP-2's
serving-position form (GR-C6) is a refusal (no hop is stored at a serving
position, so such a route gets no entry) and an outer pattern `X + T` with
`StripPrefix(X)` keeps only the paths starting with `T`; a FastAPI
`mount(p, ...)` hop yields entries since the mounted application is
consumed (no root of its own, after the FastAPI mount fix), and UP-6 names
the mount forms that compose and those that do not; entries keep a path
entry's `assumptions`, `served_through` and `may_answer_first`; UP-9
applies per scope. Rows
UP-5..UP-7 (Express, FastAPI, Axum) are implemented. The
client matcher rows (UP-13..UP-20) and the project link row UP-25 are
implemented: the code table is `UnknownPrefixMatcherRows` in
`internal/extract/clients`, copied into
[docs/extract/clients.md](../../extract/clients.md) ("Unknown-prefix
tier"), kept 1:1 by `TestUnknownPrefixMatcherContract`; the fixture
`testdata/fixtures/unknown-prefix` reaches every row
(`TestUnknownPrefixLinks`). UP-25 is the project link row PL-R13 of
[project-links.md](../../extract/project-links.md). Of the surface rows,
only UP-30's assumption text is shown so far (the local server's caller
reason `prefix_assumed`). The call-side rows UP-39 to UP-41 and the
declaration rows UP-21 and UP-26 are implemented (developer
decisions of 2026-11-23 on U3, U5 and U7): the call-side code table is
`UnknownPrefixCallRows` in `internal/extract/clients`, copied into
[docs/extract/clients.md](../../extract/clients.md) ("Unknown-prefix call
side"), kept 1:1 by `TestUnknownPrefixCallContract`; the fixture
`testdata/fixtures/unknown-prefix-helpers` reaches every call-side row
(`TestUnknownPrefixHelpers`); UP-21 is a matcher row (as above); UP-26 is
the project link row PL-L7 of the project link contract, fixture
`testdata/fixtures/projectlink-unknown-prefix`
(`TestUnknownPrefixProjectScenarios`). The surface rows UP-29 to UP-33
and UP-36 are implemented, UP-37: the stored attributes are read for
display by `routes.PrefixUnknownPaths` and the link assumptions by
`clients.LinkAssumptions`; the server views are described in
[serve.md](../../cli/serve.md) and the MCP answers in
[mcp-server.md](mcp-server.md); the server goldens
`internal/server/testdata/unknown-prefix-*.golden.json`
(`TestUnknownPrefixGoldens`, `TestUnknownPrefixRows`), the MCP golden
`session-unknown-prefix.golden.json` (`TestUnknownPrefixSessions`) and
`TestUnknownPrefixPathsField` (change journal) cover them. Decision U6 of
2026-11-23: a Next.js route whose `basePath` is not read (row NX-R4 of
[nextjs-supabase.md](nextjs-supabase.md)) keeps its `resolution:
unresolved` entries in `paths`, but every surface shows them as these rows
show an entry (UP-29, reason `nextjs_base_path_unresolved`), and a link to
such a route is aligned with UP-19 (row NX-L1: `match: suffix`,
`route_prefix: unresolved`, confidence 0.6, `assumes` NX-V1, UP-V1,
UP-V2); which route a call links to does not change.

A route whose prefix the code does not fix (a configured base such as
dozzle's `r.Route(base, ...)`, a prefix that is not a constant, a router
reached through a hop no row proves) has no path entry today: its `paths`
attribute is empty, and the hop where composition stopped is stored in
`unresolved_mounts` (rows OG-94..OG-96 of
[openapi-generation.md](openapi-generation.md)). This contract adds, for
such a route, the part of its path that *is* proven, with the prefix
explicitly unknown, and lets the client matcher link a call to it only in
a refused-by-default fallback tier at the suffix confidence (0.6).

Row kinds: **R** route side (which routes get entries, the entry form);
**L** client matcher; **P** project link pass; **S** surfaces; **C** call
side; **V** assumptions (no
branch); **D** the single default branch. Every claim the code makes is
named by exactly one row; every other form falls to D.

## Route side

The stored attribute is `unresolved_prefix_paths` (a new optional route
attribute; `paths` keeps its meaning: full composed paths only). Each
entry: `template` (the proven path below the unknown part, normalized by
the framework's rows), `prefix: "unresolved"`, `prefix_reason`
(`route_prefix_unresolved` or `route_mount_unresolved`), `hop` (the
`unresolved_mounts` key the entry comes from, or null for UP-3), `above`
(the known prefix above the hop, only when the hop's `value_covers` is
`hop`), and, as in `paths`, `trailing_slash`, `conditional`, `resolution`,
`optional`, `scope`, `assumptions`, `served_through` (Go: GR-V5) and
`may_answer_first` (an earlier mount or router named by assumption RO-V1
or RO-V2 of the route extraction).

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-1 | Go: a chi `Route(p, f)` or `Mount(p, x)`, or a gin `Group(p)`, whose prefix `p` is not a constant (GR-P1 refusal on a prefix, hop reason `route_prefix_unresolved`), with the routers below it proven | one entry per path stored below the hop (`unresolved_mounts[].paths`), `above` when `value_covers` is `hop` | a prefix chi rejects or a regexp prefix (`invalid_pattern`, `regexp_param`): no hop, no entry |
| UP-2 | Go: a net/http composition (GR-C3) whose `StripPrefix` prefix or outer pattern is not a constant | as UP-1; for an outer pattern `X + T` (`T` a constant other than `/`) whose `StripPrefix` prefix is `X` spelled the same (an identifier or a selector of identifiers), only the paths below the hop starting with `T` (GR-C3's tail check below the unread `X`) | a prefix holding `{`, `}`, `*` or `:`: no entry; a path below the hop that does not start with `T`: no entry; a `StripPrefix` at a serving position (GR-C6: no hop is stored there): no entry |
| UP-3 | Go: an oapi-codegen generated server (GR-G1) whose instances give no one constant BaseURL | one entry per registration: the template's path, `hop: null`, `prefix_reason: route_prefix_unresolved` | no visible instance (`route_unmounted`): no entry |
| UP-4 | Go: a router reached only through a hop no row proves (a router argument, a wrapped or escaped router; hop reason `route_mount_unresolved`, `value_covers` `nothing` or `root`) | as UP-1, `prefix_reason: route_mount_unresolved` (decision U2; precision-safe default: no entry) | - |
| UP-5 | Express: `use(p, router)` with `p` not a constant | as UP-1 | a prefix piece no row reads (`route_pattern_unmodelled`): no entry |
| UP-6 | FastAPI: `include_router(r, prefix=p)` or `mount(p, ...)` (the `mount` method, `x.router.mount` on a proven app, or a Starlette `Mount(p, ...)` in the `routes=` list literal of a proven `FastAPI(...)` constructor) with `p` not a constant, or an included router's own `prefix=` that is not a constant | as UP-1 | a router nothing includes, or reached only through a mount no row composes (an `APIRouter.mount` whose FastAPI version is not proven >= 0.137.0, a `Mount` in any other routes list; `route_unmounted`): no entry; an `APIRouter.mount` with `p` not a constant under a proven >= 0.137.0 (`route_prefix_unresolved`, no hop is stored): no entry; a pattern `""` right at the hop (not stored below it): no entry |
| UP-7 | Axum: `nest(p, r)` or `nest_service(p, s)` with `p` not a constant and the router resolved | as UP-1 (the nested `/` as `""` and the trailing-slash paths only for a crate whose axum version is proven, rows SH-X11, SH-X25) | a nest, nest_service or merge whose router no rule resolves: no entry; the nested `/` or a trailing-slash path on a crate whose axum version is not proven (not stored below the hop): no entry; a route whose every path is left out for an unproven trailing slash (`route_trailing_slash_unproven`): no entry |
| UP-8 | the entry list | entries deduplicated by (`above`, `template`, `trailing_slash`) across hops (at most 16 hops, as stored), merged as `paths` entries are (conditional, scope, resolution), sorted by `above` then `template` | - |
| UP-9 | a route with at least one usable `paths` entry, applied per scope | a route with a production-scope `paths` entry: no `unresolved_prefix_paths` (a known path is never weakened by an unknown one); a route whose `paths` are all test scope: its production entries only (all test scope: a test harness mount never removes a production entry) | - |
| UP-10 | refused route forms | none: a route below no hop (`route_unmounted`), a Next.js `basePath` read as not literal (row NX-R4 keeps its `resolution: unresolved` entries in `paths`; shown and linked as UP-29 and UP-19 state, decision U6), a dynamic registration pattern (no route node), the path `""` below a hop (stored, never matched: it has no literal segment) | - |

## Client matcher

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-13 | a call alternative the existing tiers (exact, origin, suffix; ADR 0018) leave without a candidate | the `unresolved_prefix_paths` entries become its candidates; an alternative with any existing candidate (linked, ambiguous or test-only) is never matched against them, so no existing edge changes, except as row UP-22 states | an alternative a Next.js literal rewrite withholds (row NX-L3): no candidate |
| UP-14 | an alternative whose path is known from its start: base `none`, `literal`, `constant` or `configured`, no literal origin | candidate when the call's segments are `above` + V + `template`: the call starts with `above` (route parameters take any call segment), V is a run of zero or more literal call segments (the unknown prefix), and the rest matches `template` by the existing segment rules (route parameters, final catch-all, optional final parameter) | a call parameter inside V |
| UP-15 | an alternative with base `unresolved` and an origin that is not `unresolved` | candidate when the entry has no `above` and the call's template matches `template` entirely (aligned: the call's unknown base stands for the route's unknown prefix, assumption UP-V2) | an entry with `above` (its unknown part does not start the path, so the call's base cannot stand for it); any other overlap of two unknown parts |
| UP-16 | anchoring and minimum evidence | a candidate matches at least two literal route segments (of `above` and `template`) with literal call segments, and `template`'s first segment is a literal or a parameter matched by a call parameter | fewer than two: no candidate (the alternative stays `unmatched`) |
| UP-17 | uniqueness | the alternative links only when exactly one route has an entry that matches it with at least one literal segment, before UP-16's minimum and whatever prefix V each match implies; routes are never ranked against each other by literal count in this tier | two or more such routes: `ambiguous`, candidates and `candidate_count` as today |
| UP-18 | method, host, slash form and scope | the existing rules: equal method, ANY, client HEAD to a GET route of Express, Axum, net/http; a net/http host route only for its host; a strict-slash router's entry only for a call of its own slash form (TOP-2 of 2026-11-18); production calls prefer production entries, test-only outcomes as today | - |
| UP-19 | the link | one `calls_route` edge, provenance `inferred`, confidence 0.6; the path entry records `match: "suffix"` and `route_prefix: "unresolved"`; the call carries `assumes` `UP-V1` (and `UP-V2` for UP-15) | - |
| UP-20 | refused alternatives | none: a literal origin (the route's origin is not known), unless row UP-21 admits it in the project link pass; `origin_resolution: unresolved` (rows BC-2, BC-3: exact only; never proven, a declaration included), a Next.js route (never a candidate here) | - |
| UP-21 | project link pass: an alternative with a literal origin matched against the routes of another member, where a `consumes` entry of the call's consumer that covers it names that origin in `origins` (rows PL-R5, PL-L4 of the project link contract) and so names the provider | candidate as UP-14 reads a call known from its start (the declared origin stands for the provider's server; the prefix stays unknown); UP-16, UP-17 and UP-18 apply; a link is row UP-26 (PL-L7) | an origin no covering entry names (UP-20, PL-R5); the routes of the call's own member (as LF-1: no waiver) |
| UP-22 | an alternative whose every candidate in the existing tiers is tied only through paths ending in a final catch-all (an SPA or root fallback: `r.Get("/*", spa)`, `mux.Handle("/", ...)`, `app.get("*", ...)`), and an `unresolved_prefix_paths` entry matches it as rows UP-14, UP-15 and UP-18 read a call, with at least one literal segment (UP-17's competitor; UP-16's minimum is not needed for an ambiguity), on a production route (for a test call, any route) | `ambiguous`, as row LF-2 makes a catch-all ambiguous beside a more specific route: the candidates are the catch-alls and those routes, `candidate_count` as today; never linked to either (a router tries the route below the unknown prefix before the fallback) | only test-scope entries match a production call, or no entry matches with a literal segment: the catch-all link stands |

## Project link pass

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-25 | a cross-repository source (ADR 0075) whose only candidates in its candidate providers are `unresolved_prefix_paths` entries, and that no `consumes` entry naming the provider covers (a member without `consumes`, or an entry naming the source's own member) | none (decision U5): outcome `unproven` (PL-R13), the reason naming the unknown prefix and the missing declaration | - |
| UP-26 | a cross-repository call alternative covered by a `consumes` entry of its consumer that names the provider member (for a literal origin, also the origin: UP-21), whose result in that provider rests on `unresolved_prefix_paths` entries | the result counts as any result of a declared provider: one route in one possible provider links (`calls_route`, row PL-L7, inferred, 0.6, `match: "suffix"`, link attribute `route_prefix: "unresolved"`), with `assumes` UP-V1 (and UP-V2 for an unresolved base) and UP-V3, the assumption naming the entry and ending "route prefix not read from code: assumes ..."; a tie inside the provider is `ambiguous` (UP-17, PL-R10); a second possible provider makes it `ambiguous` (PL-R11) | the routes of the source's own member (the scope overlay PL-S1..PL-S3, an entry naming the own repository): PL-R13; an OpenAPI operation path (exact tier only) |

## Surfaces

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-29 | Routes list and route detail | a route with entries shows each entry as `above` + `…` + `template` (dozzle: `…/api/hosts/{host}/logs/stream`) with the label "prefix unknown" and its reason and the hop; it is never shown as a full path, never sorted as one (after every full path, by its known part) and never counted as one (`path_count` counts full paths); the flag `unmounted` is replaced by `prefix_unresolved`; a Next.js route whose `basePath` is not read (NX-R4, decision U6) is shown the same way, reason `nextjs_base_path_unresolved` | - |
| UP-30 | Callers and the endpoint detail | an UP link is listed with tier `suffix`, confidence 0.6 and the UP-V1 (UP-V2) assumption text | - |
| UP-31 | change journal (C3, C5; ADR 0056) | `unresolved_prefix_paths` is a C3 field of a route that stores it on either side (null to a value `known_now`, a value to null `unknown_now`, otherwise `changed`); identity and content hash are unchanged by entries (identity is the registration pattern); C5 treats an UP link as any link | - |
| UP-32 | change impact | the impact traversal follows UP links as other inferred `calls_route` edges; paths are shown in the UP-29 form (`prefix_unknown_paths`, never in `route_paths`); a `calls_route` step carries its link's assumptions (`link_assumptions`: UP-V1, UP-V2 and an unproven origin) | - |
| UP-33 | architecture graph | CE1 counts UP links in the tier histogram (`suffix`); a route's full path stays null, the UP-29 form shown beside it (`prefix_path` of a link member or chain member); a link member and a CH3 chain edge carry their link's assumptions | - |
| UP-34 | OpenAPI generation, `explorer.path_values` | unchanged: an entry is never a path key (OG-10 reads `paths` only), the route stays in `x-graphvein.unresolved` with its keys (OG-45), a value set by you places it (OG-92, OG-95) and never feeds the matcher or the index | - |
| UP-35 | try-it | unchanged: a route needs a full path (a value set by you) | - |
| UP-36 | MCP answers | a route's `path` stays null; the answer adds the entries (`prefix_unknown` on an endpoints row, `prefix_unknown_paths` on the endpoint, a change impact path and the chain's `prefix_path`) and the links with their assumptions (`prefix_assumptions` ids on a caller, a callers or impact step and a chain edge, the texts in the envelope's assumptions) | - |
| UP-37 | precision labels | entries and UP links are best effort (row D of the precision rules; outside PL-F10): a Precise route label over routes that show entries (or NX-R4 entries) names `prefix-unknown paths` among its unverified facts, marked beside the `…` paths | - |

## Call side

| id | modelled form | facts claimed | refusal (to D) / diagnostic |
|---|---|---|---|
| UP-39 | a call `H(a)` of a URL helper `H` bound at its module's top level (a `const` arrow or function expression, or a function declaration) with exactly one plain parameter `p` without a default, whose body is exactly `return X + p` or ``return `${X}${p}` `` (an arrow's expression body included): `p` used once, as the last piece, nothing before `X` or after `p`; `X` a member read `O.k` of a `const`, or a `const` itself, declared once at the top level of the helper's own module (not imported), never written to, whose value the bounded evaluator does not read (dozzle's `withBase`, `X` = `config.base`, `config` an object spreading the parsed page configuration); `a` evaluating to strings that each start with a literal `/` | the call's alternatives as for `a`, after a base `unresolved` whose `base_expression` is the spelling of `X` (resolution `evaluated`, `H` in `helpers`); the matcher reads them as any unresolved-base call: the existing tiers, then UP-15 | - |
| UP-40 | refused calls of a helper of UP-39's body shape | none (the ordinary evaluation stands; `dynamic` when it cannot read `X`): a helper not bound at its module's top level; `X` a parameter, a `let` or `var`, a call, an imported value, a value written elsewhere, or a `const` whose value the evaluator reads (a known object without the property is `undefined`, not a configured base); no argument, more than one, or a spread; an argument that is not a string starting with a literal `/` (a variable, an absolute URL, a relative path, a template starting with a substitution) | - |
| UP-41 | the helper of a UP-39 call reached through a declaration file generated by unplugin-auto-import (its leading comments hold `// Generated by unplugin-auto-import`): the callee is a name no binding of the calling file declares, and exactly one such file whose directory holds the calling file declares it inside `declare global { }` as `const NAME: typeof import('./module').NAME` (a relative module, the export of the same name; dozzle's `assets/auto-imports.d.ts`), the module resolving to a scanned file | the callee is that module's export, read by UP-39 and UP-40 only | any other reading of the helper (the call keeps the ordinary evaluation of an unbound name: `dynamic`); a declaration file without the header, another declaration form (`['NAME']`, a package module, another export name), a name two such files declare, a calling file outside the declaration file's directory |

## Assumptions

| id | text |
|---|---|
| UP-V1 | The route is served below a prefix the code does not fix (configuration or an unproven hop); the call is taken to reach it because its path ends with the route's proven path below that prefix. |
| UP-V2 | The call's unknown base and the route's unknown prefix are the same value (for example both read from the same configuration). |
| UP-V3 | A consumes entry of the project manifest names the provider (and, for a literal origin, that origin): the call is taken to reach that provider's server, below a prefix its code does not fix. |

## Default

| id | modelled form | facts claimed |
|---|---|---|
| D | anything else | nothing: no entry, no candidate, no call-side reading; the route keeps its existing state (empty `paths`, the existing diagnostics and `unresolved_mounts`), and a call of a helper of any other shape keeps the ordinary evaluation |

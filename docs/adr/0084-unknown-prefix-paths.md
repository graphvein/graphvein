# ADR 0084 - Unknown-prefix paths

**Status:** Accepted (delegated review, 2026-11-24)

## Context

A route whose prefix the code does not fix gets no path today. The route
extractors compose a route's paths from its registration up to a root;
where a prefix is not a constant (dozzle's `r.Route(base, ...)` with
`base` from configuration, an Express `use(process.env.BASE, router)`, a
FastAPI `include_router(r, prefix=settings.PREFIX)`, an Axum
`nest(&base, api())`) or a router is reached through a hop no row proves
(miniflux's UI mux behind a middleware chain and an unread `StripPrefix`),
composition stops, the route keeps its node with an empty `paths`, and the
shared diagnostic `route_prefix_unresolved` or `route_mount_unresolved`
names the hop (ADR 0013, ADR 0076 decision 9). Since T029 FU-2 (ADR 0081)
the route also stores the hop in `unresolved_mounts`, with the paths
proven below it, so that a value set by you in `explorer.path_values` can
place it in the generated OpenAPI document; the index itself never takes
that value.

The consequence is that such a route has no callers: the client matcher
(ADR 0018) only reads `paths`. On the pinned corpora, dozzle has 0 of 85
production routes with a path, miniflux 74 of 174 (its 100 UI routes sit
behind the unproven hop), while the Express, FastAPI and Axum corpora have
none of this form. The developer decided (GO3, option (b) of the Go
routers task's open point O3) that such routes keep a path entry with an
explicitly unknown prefix, matched only by the client matcher's suffix
tier, as a change shared by every framework.

Three facts from the T033 investigation analysis (task record, internal) shape
the decision:

- Every consumer of `paths` reads an entry's `template` as the full path:
  the Routes list and its sort order, the architecture graph's full path,
  the schema view's routes, try-it, OpenAPI's path keys (OG-10), the
  change journal's `unknown_now` rule for C3 (an empty `paths` is
  unknown), the `unmounted` flag. An entry whose template is not the full
  path changes the meaning of every one of them.
- The paths below a hop are already composed and stored, per hop, in
  `unresolved_mounts[].paths`, with the known part above the hop in
  `above` when the routers above are proven (`value_covers: hop`).
- Ranking candidates of this tier by literal segment count, as the
  existing tiers do, links wrong routes. When the first two segments of
  every known route path on the corpora are treated as unknown and the
  corpora's own exactly linked call alternatives are matched again,
  ranking links 8 of 405 to a wrong owncast route (6 with a minimum of
  two literal segments) (a call to
  `/api/moderation/chat/user/{id}` wins
  `…/moderation/chat/user/{id}` of another router over its own
  `…/chat/user/{id}`); matching other corpora's calls against dozzle's and
  miniflux's routes links 12 calls on one literal segment (`/login`,
  `/categories`, `/keys`), all wrong.

## Options considered

1. **Entries in `paths` with `"prefix": "unresolved"`, contract version
   bump.** The developer's example shape, in the existing attribute.
   Every `paths` consumer changes in the same change, and contract v1's
   rule for a change of an existing record's meaning applies: bump
   `contract_version` to 2, update the schemas, docs and every golden.
   Rejected: the largest change for no capability the other options lack,
   and an external indexer written for v1 is refused.
2. **Entries in `paths` without a bump**, as Next.js already does for a
   `basePath` read as not literal (row NX-R4, `resolution: unresolved`).
   Rejected: the Next.js precedent shows the cost (its entries are shown
   as full paths by the Routes list today), and it silently changes what
   older readers of the stored data see.
3. **A new optional route attribute `unresolved_prefix_paths`** holding
   entries of the developer's shape (`template`, `prefix: "unresolved"`)
   plus where the unknown part is (`hop`, `above`, `prefix_reason`);
   `paths` keeps its meaning. Additive: contract v1 allows optional
   attributes in the open `attributes` map without a version change, as
   for `unresolved_mounts`, `shape` and `model_uses`.
4. **No new attribute; the matcher reads `unresolved_mounts[].paths`.**
   The smallest change (no extractor or route golden change). Rejected
   as the stored form: the attribute exists for explorer path values, its
   entries also exist for hops this design may refuse, the generated
   server case (GR-G1) has no hop, and every surface would re-derive the
   same projection. It remains the source the extractor projects from.
5. **Resolve configuration defaults** (go-arg or flag struct tags, the
   option (c) of O3). A new library surface per configuration package,
   and a default is not the deployed value. Not this task.

For the matcher: (a) the existing suffix tier extended to these entries
with its literal-count ranking; (b) a separate fallback tier, considered
only for an alternative the existing tiers leave without a candidate,
with a minimum of two matched literal segments and no ranking between its
candidates (any second route that matches makes the alternative
ambiguous); (c) no matching, entries for display only.

## Decision

Option 3 for storage, matcher option (b), as the Draft claim contract
[unknown-prefix-paths](../contract/claims/unknown-prefix-paths.md) (rows
UP-1..UP-49) states:

- The link pass projects each route's `unresolved_mounts[].paths` (and,
  for a generated server whose BaseURL is not one constant, the
  template's paths) into `unresolved_prefix_paths`, only for a route with
  no usable `paths` entry.
- The client matcher considers these entries only for an alternative the
  existing tiers leave without a candidate. A call whose path is known
  from its start matches when it is `above` + an unknown run of literal
  segments + the entry's template; a call with an unresolved base matches
  only when its template equals `above` + template (both unknown parts
  taken to be the same value). Literal origins are refused, except in the
  project link pass through a `consumes` entry that names the origin and
  the provider (row UP-21, below). At least two
  literal segments must match; any second route with a match makes the
  call ambiguous. A link is `calls_route`, inferred, confidence 0.6,
  `match: suffix`, with `route_prefix: "unresolved"` on the path entry and
  the assumption UP-V1 (and UP-V2) on the call.
- No `contract_version` change. The aggregate extractor version is bumped
  once (the route nodes and client links change).
- OpenAPI generation, `explorer.path_values` and try-it are unchanged:
  an entry is never a path key; a value set by you still places the
  route and never feeds the matcher.
- Project links use the entries only through an explicit declaration
  (developer decision of 2026-11-23 on U5 and U7, rows UP-21, UP-25,
  UP-26): a cross-repository call whose only candidates are these entries
  is `unproven` unless a `consumes` entry of its consumer that covers it
  names the provider member (and, for a literal origin, names the origin
  in `origins`); then the provider's result counts as any declared
  provider's, under UP-16 and UP-17 (a tie inside the provider or a second
  declared provider stays ambiguous), at 0.6 with the declaration shown as
  assumption UP-V3. The routes of the call's own member never link this
  way.
- The call side reads a URL helper that prepends one base read from
  runtime configuration (developer decision of 2026-11-23 on U3, rows
  UP-39 and UP-40): a closed list of helper shapes (one plain parameter,
  body exactly `return X + p` or `` `${X}${p}` ``, `X` a module-level
  `const` of the helper's own module, or a member read of one, never
  written to and not read by the evaluator) makes the call an
  unresolved-base call with `base_expression` the spelling of `X`; every
  other helper keeps the ordinary evaluation. The helper's cached body is
  enough for the shape. dozzle's modules call `withBase` without an
  import (unplugin-auto-import inserts it at build time), so row UP-41
  binds such an unbound name through the committed declaration file the
  plugin generates (`declare global { const withBase: typeof
  import('./stores/config').withBase }`, with the plugin's header), for
  UP-39 only; that declaration form is a new cached syntax fact of
  declaration files (`RouteFacts.AutoImports`), a parse-time cache change
  inside the branch-internal aggregate version extractors-59.

## Consequences

- dozzle and miniflux routes keep their empty `paths` and gain entries
  (85 and 100 routes); the Routes list shows them as
  `…/api/hosts/{host}/logs/stream` with "prefix not read from code"
  instead of the misleading `unmounted` flag.
- Existing `calls_route` edges are unchanged: the tier only fills calls
  that are `unmatched` today.
- Before the call-side rows no corpus call linked in this tier: dozzle's
  frontend builds every URL with `withBase(path)`, whose base is runtime
  configuration, so its calls were `dynamic`. With rows UP-39 and UP-40
  the investigation analysis expected 18 links, all correct by the source, and
  one refusal (`/healthcheck`, one literal segment); the corpus result is
  recorded in the T033 task record (internal).
- The change journal reports the new attribute as a C3 field of a route
  that stores it on either side (`known_now` on the first comparison
  after the upgrade, cause `extractor_or_relink`); a route without it on
  either side keeps its item as before. Route identities and content
  hashes do not change (a route's identity is its registration pattern).
- Every surface (routes list, endpoint detail, flow, architecture graph
  and chain, change impact, MCP) shows an entry as the known part with
  `…` for the prefix and its reason, never as a full path, and shows the
  assumptions of every link resting on one (the views'
  fields are described in docs/cli/serve.md and the MCP claim contract).
- The goldens of fixtures with unresolved hops (`go-routes`,
  `mount-hops`, `fastapi-helpers`) change by the new attribute; every
  golden that records the extractor version changes by the bump.
- Review fixes: a precise route label over routes that show
  entries names "prefix-unknown paths" among its unverified facts (UP-37:
  best effort); a route reached through a stored hop no longer reports
  `route_unmounted`; UP-9 applies per scope (a test harness mount at a
  known prefix never removes a production entry); a net/http hop whose
  outer pattern is `X + T` with `StripPrefix(X)` keeps only the paths
  starting with `T` (Go 1.27.1); an alternative whose only existing
  candidates are final catch-alls is `ambiguous` when an entry qualifies
  under UP-16 (row UP-22, a reported contract change), so a known SPA or
  root fallback no longer takes every API call at 0.9 beside an API below
  a configured base.

## Known gaps

- A route with a known production path that is also reached through an
  unknown hop gets no entry (UP-9): the unknown chain is not stored for
  it. A route whose known paths are all test scope keeps its production
  entries, but its `unresolved_mounts` are not stored (the attribute stays
  on pathless routes only), so an entry's `hop` key may name a hop the
  route itself does not list.
- Row UP-22 adds the unknown-prefix routes as candidates only when every
  existing candidate is a final catch-all; a catch-all that ties with a
  more specific known route is decided by the existing rules.
- Routes below no hop (`route_unmounted`: an unbound router parameter
  such as gotify's plugin routes, a FastAPI router nothing includes) get
  no entry: nothing shows they are served at all.
- Next.js routes with a `basePath` read as not literal keep row NX-R4:
  their entries stay in `paths` (a stored entry whose template is not the
  full path, which an older reader of the store still reads as one). By
  decision U6 of 2026-11-23 every surface shows them as unknown-prefix
  paths (row UP-29, reason `nextjs_base_path_unresolved`), and a link to
  one (an unresolved-base call, the only kind NX-R4 admits) is aligned
  with row UP-19: `match: suffix`, `route_prefix: unresolved`, 0.6,
  `assumes` NX-V1, UP-V1, UP-V2. No link is added or removed (15 links on
  ai-chatbot, 1 on umami, at 0.9 before); moving the entries into
  `unresolved_prefix_paths` would have sent them through UP-16 and UP-20
  and removed links, so it was not done.
- An unresolved-base call to another service whose path happens to equal
  a route's known part (UP-15) can link wrongly; the minimum, the
  uniqueness rule and the 0.6 confidence with UP-V2 shown are the only
  guards. No corpus case exists either way.
- No Express, FastAPI or Axum corpus has a route of this form: those
  rows rest on fixtures only.
- Row UP-39 cannot tell a base that names the application's own server
  from one that names another service (a CDN, a separate API host): the
  helper's shape is the only evidence, and UP-V2 is shown on every link
  it gives. A helper imported from another module as its base, or a base
  built inside the helper, is not read (UP-40). UP-41 trusts the generated
  declaration file to match the plugin's configuration (`dirs`, the files
  it transforms); a stale committed file can name a helper the build no
  longer injects.
- A `consumes` declaration (UP-26) proves which repository a call reaches,
  not the prefix: a project whose provider serves the same template below
  two prefixes of one router is ambiguous (UP-17) even when the
  declaration names that provider alone.

## Review notes (2026-11-24)

- Review: the investigation with a precision stop (cross-corpus control and strip simulation showed wrong links for the naive rule; rules UP-13, UP-16, UP-17, UP-20, UP-25 refuse them), one independent review of T033 together with the FastAPI/Express mount fixes (F1-F10: 3 A, 4 B, 3 C), a fix round, and a confirmation pass (F1-F10 confirmed; F11 links inherit path assumptions, F12 unscanned `require` candidate refused; both fixed).
- Revisions: UP-15 only for entries without `above`; UP-13 refuses rewrite-withheld alternatives; UP-21/UP-26 allow literal-origin and cross-repository links only through a `consumes` declaration (developer decision 2026-11-23); UP-39..41 call-side helpers including the auto-import declaration file; UP-22 catch-all-only ambiguity; route-order assumptions RO-V1/RO-V2 keep paths conditional instead of dropping them; Next.js links on an unread `basePath` aligned to 0.6 with UP-V1/UP-V2 (U6).
- Results: gate run 45311511149 (Linux, Postgres and Windows; 0 FAIL, 253 `/postgres` PASS; skips: the opt-in oracles, censuses and the two new strip-oracle tests); local full check 0 FAIL, 252 `/postgres` PASS.
- Coverage per corpus: dozzle 85 routes with unknown-prefix entries and 24 new links (all adjudicated correct), miniflux 100 entries (no client calls reach them), umami 1 and ai-chatbot 15 Next.js links at 0.6; no other corpus changes. Strip oracle on 34 corpora: K=1 362 and K=2 247 correct of 453, 0 wrong; partial strip 0 wrong; cross-corpus control 932 alternatives, 0 would-link. Framework probes: chi/gin/net/http, Express 4.21.1/5.1.0, FastAPI 0.165.0, Axum 0.8.4.
- Assumptions shown: UP-V1 (served below a prefix the code does not fix), UP-V2 (the call's unknown base equals that prefix), UP-V3 (a `consumes` declaration).
- Known gaps: as listed above, plus: RO-V2 is marked per route, not per mount chain; Express `require` in TypeScript files and conditional exports are not read.
- After v1: gap filling of unknown prefixes (launch 2), version-specific Axum checks.

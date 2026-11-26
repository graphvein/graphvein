# nextjs-routes fixture (T024a)

Next.js route handlers, Pages Router API files, next.config reading and
client links, one project per directory. Every row of the Next.js claim
contract (docs/extract/routes.md) and its default branch D is reached;
TestNextRoutesContract checks that. No file needs an inert name.

- `web/`: App Router and Pages Router side by side, literal `rewrites`,
  every verb export form, slots, groups, catch-alls, private folders and
  the refused forms; client calls in `web/components/client.tsx`.
- `docs/`: env-dependent `basePath` (base unresolved) and non-literal
  `rewrites`.
- `blog/`: literal `basePath`, `trailingSlash` and `pageExtensions`,
  `src/app`.
- `broken/`: `app` and `src/pages` under different folders (refused).
- `odd/`: a function config (every key unread: routes not read).
- `web/components/swr.tsx` (T024a2): SWR template and conditional keys,
  inline fetch and axios fetchers, and the forms row NX-L2 leaves to D.
- `proxy/` (T024a2): literal `basePath` `/proxy` and literal rewrites in
  all three phases; `proxy/lib/calls.ts` notes per call whether row NX-L3
  re-routes it, matches it as usual or withholds it.
- `legacy/` (T024): an afterFiles rule with an external
  destination on a static route's path and a fallback to an external site
  for every path; routes that surely serve a call win (linked), the rest
  is withheld (`legacy/components/calls.tsx`).
- `mono/` (T024): two applications (`apps/web`, `apps/admin`)
  and a shared package; a relative call in `apps/web` does not reach the
  admin application's route (unmatched), the same call in the shared
  package is ambiguous (`nextjs_origin: unknown`).
- `web/pages/api/_helper.ts` (T024): a Pages Router route,
  `ANY /api/_helper`.

The `docs/` application's routes (a `basePath` not read, row NX-R4) are
shown as unknown-prefix paths and their link is aligned with the
unknown-prefix tier (decision U6 of T033: suffix, 0.6, UP-V1 and UP-V2;
`TestNextRoutesFixture` checks the confidence). The surfaces are checked
on this fixture by `internal/server` (`TestUnknownPrefixGoldens`, goldens
`internal/server/testdata/unknown-prefix-next-{endpoints,endpoint}.golden.json`)
and `internal/mcp` (`TestUnknownPrefixSessions`, part `next`).

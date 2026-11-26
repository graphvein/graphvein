# nextjs-shape fixture

Next.js App Router route handlers and server actions for the route shape
rows SH-N1..SH-N29 (`docs/contract/claims/route-shape-nextjs.md`).

- `web/`: a Next.js 15 application with route handlers under `app/api`,
  an `/admin` route behind a middleware with a literal string matcher,
  helpers under `lib/` (response helpers, a request chain eight calls
  deep, a response chain three calls deep) and server actions in
  `app/actions.ts`. The oracle `scripts/oracle/nextjs_shape` builds and
  serves this application.
  `app/api/review/` and `lib/nav.ts` hold the forms of the SH-N review
  (relative redirect URLs, `force-static`, `notFound()` below a `try` on
  the call chain or in a nested function, `Response.json(undefined)`,
  un-awaited `headers()`), and `next.config.js` a conditional redirect and
  headers rules for some of those routes; the oracle keeps that config.
  The headers rules of `cfgh` and `cfgx` (a list grown with `push()`)
  set no content header (SH-N30); those of `cfct` (Content-Type),
  `cfcl` (Content-Length) and `cfk` (a name not read) may change the
  responses (SH-N24).
- `cfg/`: a Next.js 15 application whose `next.config.mjs` is a function,
  so its redirects may apply to every route (golden only).
- `edge/`: a Next.js 16 application whose `proxy.ts` has an array matcher,
  which may run before every route (golden only).

No file uses an inert name: nothing here matches the repository's ignore
rules.

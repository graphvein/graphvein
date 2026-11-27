# Next.js App Router and server action shape oracle (T029 SH-N)

Checks the shapes Graphvein stores for the fixture
`testdata/fixtures/nextjs-shape` (rows SH-N1..SH-N29 of
[the Next.js route shape claim contract](../../../docs/contract/claims/route-shape-nextjs.md))
against real Next.js releases. Opt-in, never run by CI.

```bash
bash scripts/oracle/nextjs_shape/nextjs_shape.sh --scratch "$SCRATCH"
# one release, or against a snapshot of the fixture:
bash scripts/oracle/nextjs_shape/nextjs_shape.sh --scratch "$SCRATCH" --sets next15
graphvein index <copy of the fixture> --emit snap.ndjson
bash scripts/oracle/nextjs_shape/nextjs_shape.sh --scratch "$SCRATCH" --snapshot snap.ndjson
```

Under a shared lock, run it as `flock -o LOCK bash ...` so that no
server it starts inherits the lock. It writes only under `--scratch` (per release `result.json`: counts,
disagreements, notes; the build and server logs) and exits 1 when anything
disagrees. It needs `go`, `node`, `npm` and `curl` on `PATH` and network
access to the npm registry for the first install of each release. Without
`--snapshot` it indexes a copy of the fixture with `go run ./cmd/graphvein`.

## What it compares

For each release, the fixture's `web` application is copied into the
scratch directory with a `next.config.js` that loads the fixture's own
(its `redirects` and `headers` rules) and skips type errors, plus one
oracle-only page that renders every server action as a form action (so its
action id appears in the HTML); it is built with `next build` and served
by `next start` on 127.0.0.1. `oracle.mjs`, with the requests listed in
`cases.json`, then checks:

1. **Route handlers.** Every request of a route is sent. Every claimed
   status (kind `read_in_handler` or `returned`) must be observed on some
   request of its route; every observed response at a claimed status must
   have the claimed content type (exact header value), an empty body where
   the schema is `null`, a non-empty body where it is a string, the fields
   of a claimed literal body, and a JSON value that fits a `returned`
   schema. A request with `expect` must answer that status (per release
   when it names one per set) and one with `expect_header` that header
   (per release when it names one per set):
   they show the framework behaviour a refusal rests on (a relative
   redirect URL answers 500, `force-static` empties the request, a caught
   `notFound()`, a next.config redirect). For every `fact` body schema, the request marked `valid` must
   be accepted, and derived requests must be refused (status 400 or more):
   each required field missing, each typed field of another kind (`"5"`
   for a number, `123` for a string, ...), `null` for a field that is not
   nullable, `1.5` for an integer, no body where a field is required; an
   optional field missing and an extra key where extra keys are allowed
   must be accepted.
2. **Server actions.** Each action is called through its action id
   (`Next-Action` header, arguments as a JSON list). A claimed literal
   result's fields must appear in a returned value of the flight response;
   for a `fact` argument schema (argument 1's), the valid arguments must
   be accepted, the valid arguments plus an extra one too (further
   arguments are not checked), and no argument, each required field
   missing or of the wrong kind must make the action fail.
3. **zod.** `z.toJSONSchema(S, { io: "input" })` of each fixture schema
   listed in `cases.json` against the stored fields, required set, types
   and extra keys.
4. **TypeScript.** The checker (`strict`) over the application: every
   query, header, cookie and form field name the store lists as read must
   be the literal argument of `get`, `getAll` or `has` at the stored file
   and line on a receiver of the matching type (`URLSearchParams`,
   `Headers`, `RequestCookies`, `FormData`, or the read-only variants of
   `next/headers`); every model (kind `intended` or `returned`) must have
   the declared type's properties, optionality, nullability and types.

## Pinned packages and licences

`next14/`, `next15/` and `next16/` hold a `package.json` with exact
versions and its `package-lock.json`; the script installs each with
`npm ci --ignore-scripts` into the scratch directory (optional packages
included: Next.js needs its SWC compiler binary to build). Nothing is
installed into the repository.

| Set | Packages (direct) | Licences of the installed tree |
|---|---|---|
| `next14` | next 14.1.0, react and react-dom 18.2.0, zod 4.6.7, joi 17.13.9, typescript 5.6.3, @types/react 18.2.79, @types/node 20.17.6 | MIT, ISC, Apache-2.0, BSD-3-Clause (joi and its @hapi/@sideway dependencies), 0BSD, BlueOak-1.0.0 (graceful-fs); caniuse-lite (data) CC-BY-4.0 |
| `next15` | next 15.5.29, react and react-dom 19.1.1, zod 4.6.7, joi 17.13.9, typescript 5.6.3, @types/react 19.1.13, @types/node 20.17.6 | as `next14`; sharp and its libvips binaries (Apache-2.0, LGPL-3.0-or-later parts) are installed as optional packages but not used (no image is optimized) |
| `next16` | next 16.2.10, react and react-dom 19.2.0, zod 4.6.7, joi 17.13.9, typescript 5.6.3, @types/react 19.2.2, @types/node 20.17.6 | as `next15` |

`next` 14.1.0 and 16.2.10 are the versions locked by two of the Next.js
test corpora (chatbot-ui and ai-chatbot), so they stay exact even though
npm audit reports advisories against them: no release of the 14.1 or 16.2
line fixes all of them, and the oracle only builds and serves the
fixture on 127.0.0.1, never user input. `next` 15.5.x has no corpus pin
and follows the newest patch of its line. The `postcss` 8.4.31 that every
`next` release here pins exactly is reported for the same reason.

# ADR 0041 - Local server and embedded web UI

**Status:** Accepted (delegated review, 2026-11-03)

## Context

ARCHITECTURE.md plans a React UI embedded in the single binary and an HTTP
API that binds to 127.0.0.1 by default. VISION requires that stale data is
never presented as current, so index freshness must be visible on every
screen. ADR 0040 makes `internal/egress` the only package that may open a
socket and allows listeners only on loopback IP literals through
`egress.ListenLoopback`. The UI decisions in the T016 task record (internal)
chose layout A (U1) and shadcn/ui on Radix (U2), with the M5 mockup's tokens.

Two constraints shape the build: `go build` and `go test` must keep working
on Windows, macOS and Linux without Node, and `go:embed` needs the files at
compile time (a pattern that matches nothing is a compile error; a pattern
without `all:` silently drops names starting with `.` or `_`, which bundler
hashes can produce).

T016 delivers this in two runs: T016c1 (server, API v0, security, a
hand-written placeholder page) and T016c2 (the `web/` project, built assets,
drift check). This ADR records the decisions for both; T016c2 amends it with
measurements.

## Options considered and decisions

### 1. Built assets

- **Committed in `internal/server/ui/static` (chosen).** `go build` and
  `go test` work without Node on every OS and in every CI job; releases need
  no JavaScript toolchain; the binary a developer builds is the released
  one. Cost: diff noise (mitigated by `linguist-generated`) and possible
  drift between sources and committed output (decision 2).
- Built only in CI or release jobs. Needs a placeholder or a build tag for
  local builds, so the binary a developer runs differs from the released
  one. Rejected.
- `go generate` running Vite. Still needs Node on every machine that
  builds. Rejected.

`internal/server/ui` embeds `all:static` and exposes it as `ui.Assets`
(`fs.Sub` rooted at `static/`).

### 2. Drift detection

`scripts/check-web.sh` rebuilds from the lockfile with the pinned Node and
npm and fails when `internal/server/ui/static` differs from a clean build. It
runs in CI on Linux when web inputs change; the Linux build is canonical
(a different Windows build is not a failure). `scripts/test.sh` does not run
it, so Go tests pass without Node.

### 3. Versions

Node 24.22.2 LTS with npm 11.19.1, pinned through `.nvmrc`, `packageManager`
and `engine-strict`; exact dependency versions, lockfile v3, `npm ci` only.
TypeScript 6.0.3, because typescript-eslint does not yet support 7.

### 4. Dependencies and licences

Runtime: react, react-dom, radix-ui, class-variance-authority, clsx,
tailwind-merge, lucide-react. Development only: vite, @vitejs/plugin-react,
typescript, tailwindcss, @tailwindcss/vite, tw-animate-css, vitest, jsdom,
@testing-library/react, @testing-library/dom, eslint, @eslint/js,
typescript-eslint, eslint-plugin-react-hooks, @types/react, @types/react-dom.
No router, data-fetching, state, font or icon-font library; adding a
dependency later updates this ADR.

A lockfile-based check allows MIT, ISC, BSD-2-Clause, BSD-3-Clause,
Apache-2.0 and 0BSD for runtime packages, plus MPL-2.0, CC-BY-4.0, CC0-1.0,
MIT-0 and BlueOak-1.0.0 for development packages; missing licences and
OR/AND expressions fail. Minification drops licence comments, so the build
writes `third-party-licenses.txt`, which the binary serves.

Some build tools copy their own code into the output, so the bundle is not
only runtime packages: Tailwind CSS (preflight, theme variables,
utilities), tw-animate-css (keyframes and animation utilities) and
rolldown (the CommonJS interop helpers, derived from esbuild; its
`THIRD-PARTY-LICENSE` is included). `web/scripts/bundled-tools.mjs` lists
them as `BUNDLED_TOOLS`; the licence check holds them to the runtime
allow-list, and the notice file carries their notices. Each known tool has
markers (Tailwind's banner comment, tw-animate-css's `--tw-enter-*`
variables, rolldown's helper prologue, Vite's modulepreload polyfill); the
notice step fails when a marker of an unlisted tool is in the build or a
listed tool left none. Vite's modulepreload polyfill is turned off
(`build.modulePreload.polyfill: false`): the page is one entry chunk with no
preload links, so it only added Vite's code. Tools that transform code
without adding their own (TypeScript, @vitejs/plugin-react in production,
the CSS minifier lightningcss, MPL-2.0) are not listed. `npm run build`
writes the notice file itself, so a plain build output is complete.
`.npmrc` sets `ignore-scripts=true`; no package needs an install script
(fsevents ships a prebuilt binary).

### 5. Fencing

`web/go.mod` makes `web/` its own module, so `go list ./...`, `go vet`,
`go test` and the network boundary walk never enter `web/node_modules`
(which contains Go files). A root `.graphveinignore` keeps the minified
bundle out of Graphvein's self-index.

### 6. Serving and security

- Listener: only `egress.ListenLoopback`; `--addr` must be a loopback IP
  literal (default `127.0.0.1:7420`; port 0 picks a free port). There is no
  opt-in for other interfaces; ADR 0040's "serving beyond loopback" stays
  deferred. 7420/tcp is listed as `ipluminary` (multichannel real-time
  lighting control, not a developer tool) in nmap's service list, which
  follows the IANA registry (iana.org itself was not reachable to confirm);
  it is not a common development port.
- `http.Server` with read-header, read, write and idle timeouts and a 32 KiB
  header limit; cleartext HTTP/1.1 only.
- Host allow-list against DNS rebinding: `127.0.0.1:P`, `localhost:P`,
  `[::1]:P` and the bound literal, ASCII case-insensitive; everything else
  403.
- GET and HEAD only (405 otherwise); no CORS headers. Future mutating
  endpoints must add `http.CrossOriginProtection` before they ship.
  See ADR 0052: `/api/v0/index/run` also takes POST, behind
  `http.CrossOriginProtection`.
- `Sec-Fetch-Site: cross-site|same-site` on `/api/` is rejected, so other
  sites cannot trigger freshness walks with no-cors requests. For browsers
  without Fetch Metadata, a present `Origin` that is not one of the
  server's own origins (`http://` plus an allowed Host) is rejected too; an
  absent `Origin` (curl, scripts, same-origin GET) is allowed.
- A strict CSP (`'self'` only, no inline code), `nosniff`, `no-referrer`,
  `DENY` framing and same-origin COOP/CORP on every response.
- Content types come from an explicit table, not `mime.TypeByExtension`,
  whose Windows registry lookup can turn `.js` into `text/plain` (fatal for
  module scripts under `nosniff`).
- `graphvein serve` opens the store read-only, like the query commands.

Alternatives rejected: binding names such as `localhost` (may resolve to
non-loopback addresses); a token in the URL for authentication (adds
friction while any local process can read the store file anyway);
`http.FileServer` (directory listings, sniffed content types).

### 7. Routing

- **Hash routing (chosen).** The API owns `/api/`; the UI's API section
  (`#/api`) and its deep links never collide with it. Unknown server paths
  stay honest 404s instead of an `index.html` fallback that answers wrong
  asset paths with HTML, and nothing depends on a base path. Cost: in-page
  anchors cannot use `href="#id"`; jump links use `scrollIntoView`.
- History routing with an `index.html` fallback. Rejected for the reasons
  above.

### 8. HTTP API v0

Path prefix `/api/v0/`. One error envelope
`{"error": {"code", "message"}}` with codes `not_found`,
`method_not_allowed`, `host_not_allowed`, `cross_site_request`,
`store_error`, `unavailable` (503, the server is stopping) and `internal`.
`GET /api/v0/status` reports the extractor
version, the latest run (by parsed finish time, because run IDs are random
and RFC3339Nano text does not sort) and freshness computed per request with
the same calculation as `graphvein status`; no caching, calculations
serialized by a one-slot semaphore, changed paths capped at 1000 with an
exact count. `checked_at` is the instant the check starts, before the
scan, so "fresh as of `checked_at`" never names a time after the scan
began. When Ctrl+C ends `serve`, request contexts are cancelled at once
(`http.Server.BaseContext`), so running walks stop, and `Serve` returns
only after the last handler finished, before the store is closed. A failed
freshness check is `unknown`, never `fresh`; clients
treat unknown future states as `unknown`. The schema and state table are in
[docs/cli/serve.md](../cli/serve.md).

### 9. Styling

Tailwind CSS v4 with shadcn/ui components copied into `web/` (button, badge,
tooltip, separator, skeleton) on the M5 tokens, following the OS colour
scheme; system font stacks only, no web fonts.

## Measurements (T016c2)

- Bundle (Vite 8.3.5, Node 24.22.2, linux/amd64 container, after the
  T016c2 review fixes): JS 330,540 bytes (103,690 gzip -9), CSS 28,120
  bytes (6,376 gzip), `index.html` 513 bytes, `favicon.svg` 484 bytes,
  `third-party-licenses.txt` 114,209 bytes (81 runtime packages, 3 bundled
  build tools and the shadcn/ui notice). Embedded total 473,866 bytes,
  below the 2 MiB test bound.
- Binary (`CGO_ENABLED=0 go build ./cmd/graphvein`): 38,238,335 bytes with
  the T016c1 placeholder, 38,707,248 bytes with the built UI (+468,913
  bytes, +1.2 %).
- Reproducibility: two builds from different checkout paths were byte
  identical; the output contains no absolute paths. Tailwind scans only
  `web/src` (`source(".")`) and skips Markdown there
  (`@source not "./**/*.md"`), so files outside the sources and the
  component README cannot change the CSS: Markdown-only changes skip CI, so
  a class name in a README would otherwise change the bundle unchecked
  (verified: class names appended to `src/components/ui/README.md` leave
  the hashes unchanged; without the exclusion the CSS changed). Tailwind
  still scans comments in source files, so a utility name in a comment
  adds CSS (the drift check catches it).
- Licences in the lockfile: runtime MIT 81, Apache-2.0 1, ISC 1, 0BSD 1;
  bundled build tools MIT 3; dev MIT 201, Apache-2.0 18, ISC 12, MPL-2.0 12, BSD-2-Clause 8,
  BSD-3-Clause 3, BlueOak-1.0.0 4, MIT-0 2, CC-BY-4.0 1, CC0-1.0 1, 0BSD 1.
  `npm audit`: 0 runtime advisories; 11 moderate in the ESLint toolchain
  (minimatch ReDoS, GHSA-g3hv-75wg-m249), development only, with no fix in
  the pinned major versions.
- CSP and Radix: the shell uses Slot, Tooltip (Popper, Portal, Presence)
  and Separator. They set positions through React `style` props, which go
  through the CSSOM and are not inline styles under `style-src 'self'`; the
  bundle contains no code that creates `<style>` elements or calls
  `insertRule` (react-remove-scroll is tree-shaken out). In Chromium
  (Playwright) the shell, the open tooltips and the failure banner raised
  no `securitypolicyviolation` event and no console error.

## Consequences

- `go build` produces the complete binary with the UI; the T016c1 binary
  grew by about 1.0 MB (net/http server code and a 364-byte placeholder).
- Every later view (API section, architecture, schema, review) adds
  endpoints under `/api/v0/` and sections under hash routes.
- `serve` has its own runtime offline test (`TestServeCommand`, with the
  egress observer and the default resolver and HTTP transport tripwires of
  the offline run test): its only network operation is one allowed loopback
  listen.

## Known gaps

- Committed minified assets cannot be reviewed by reading; trust rests on
  the drift check, and only the Linux build is checked.
- Lockfile licence fields are self-declared by package authors.
- The bundled-tool markers are heuristics: a tool whose injected code has
  no known marker (a new helper, a new plugin) is not detected. Adding a
  build plugin means checking its output and extending
  `bundled-tools.mjs`. (Review 1 of T016c2 found the tool notices missing;
  fixed.)
- No authentication against other local processes.
- SIGTERM is not graceful (handling it needs `syscall`, which the boundary
  reserves); the store is read-only, so this is safe.
- One SQLite read connection serializes requests; each status request costs
  a full freshness walk (0.86 s on Langflow's 10,233 files).
- Radix Dialog/Sheet (needed by T016d's gap sheet) inject `<style>` elements
  through react-remove-scroll, which `style-src 'self'` blocks. T016d must
  check this and, if needed, add a per-response nonce rather than
  `'unsafe-inline'`. The T016c2 shell components do not inject styles
  (measured above). Resolved in ADR 0042 (T016d2): the gap sheet is a native
  `<dialog>` with `showModal()`, the CSP is unchanged and no nonce is used;
  Radix Dialog's violation was confirmed in Playwright under the real CSP.
- An extractor-version mismatch does not change the freshness state; the UI
  shows it separately.
- The `internal` error code is reserved; v0 has no path that emits it.
- Replacing or deleting the store while `serve` runs is not detected: for
  SQLite on Linux and macOS the open connection keeps reading a deleted or
  replaced file, so `serve` can disagree with `graphvein status` (seen:
  `fresh` against "Not indexed"); a dropped and re-created Postgres database
  or schema is not handled either. Restart `serve` after re-creating the
  store. Re-indexing in place with `graphvein index` is supported; a status
  request that overlaps the commit may answer `store_error` or `unknown`
  (SQLite busy, the read connection has no busy timeout), or `stale` beside
  the new run, because freshness and runs are separate reads; never a wrong
  `fresh`.

## Review notes (delegated review, 2026-11-03)

- Delegated review in place of developer review. Covers the T016c1 part
  (server, status endpoint, security, embedded placeholder); the T016c2 web
  toolchain decisions in this ADR are reviewed with T016c2.
  - The investigation: no stop (port 7420 checked against nmap-services; iana.org is
    blocked by the proxy).
  - Review 1 (independent): no blocker or major; 4 minor and 3 small
    findings fixed in c3733ef: `checked_at` taken before the scan; request
    contexts cancelled on shutdown with 503 `unavailable` and handlers
    awaited before the store closes; R3 tripwires in the serve test; a
    present non-loopback Origin is rejected; smoke test on
    testdata/fixtures/env-wrappers; read-only store path covered on both
    backends; single listen error message. Replacing or deleting the store
    while serving is a documented known gap.
  - Re-check (fresh reviewer): no counterexample to "fresh only from a
    completed, successful zero-change check". Two nits kept as known gaps:
    after the 5 s grace a request read just before `Close` may outlive
    `Serve` (it answers 503 or store_error, never fresh); `fresh` can
    describe a slightly later state when a file is edited and reverted
    mid-scan (serve.md wording corrected).
  - `sqlite.OpenRead` has no busy timeout, so status requests during an
    index commit can return 500 `store_error` or `unknown` (backlog).
  - Ctrl+C under Git Bash/mintty on Windows is not verified (developer
    check).
  - CI green on Linux and Windows incl. the serve smoke step; postgres job
    only the four helper skips, `TestStatusStates/postgres` and
    `TestStatusRunsFailure/postgres` pass. Binary +1.02 MB.

## Review notes, web toolchain part (delegated review, 2026-11-03)

- T016c2 (web/ project, committed assets, drift check, shell UI):
  - Review 1 (independent): no blocker; 2 major (licence notice omitted
    build tools whose code is bundled; Tailwind scanned Markdown under
    web/src so Markdown-only commits could drift the bundle unseen) and 5
    minor findings, fixed in 1b6ff20 (bundled-tools list checked against
    the built assets, `@source not "./**/*.md"`, extractor tooltip without
    a run, web CI trigger on server goldens, stale answer withdrawn on tab
    return, complete plain build). Pre-existing flaky
    `TestIgnoreMatcherMatchesReference` (map mutated while ranging) fixed.
  - Re-check (fresh reviewer): 3 minor UI precision gaps fixed in 3339b8c:
    answers to requests started before a withdrawal are ignored; `fresh`
    that contradicts any invariant (indexed, last_run, zero changes, no
    truncation, no error) is shown as unknown; no extractor claim without
    an index run.
  - CI green: check-web (Linux only, Decisions Q2), serve smoke test on
    Linux and Windows, postgres job. Embedded UI about 474 KB; binary
    +1.2%. Tailwind also scans comments in sources (a class-like word in a
    comment can add CSS; the drift check catches it).

## Revision 1 (T038: typed folder paths behind the guards)

**Status:** Accepted (delegated review, 2026-11-25)

### Context

Section 6 kept every request read-only on one store, and the server read
only the folder it was started on. The T038 workspace
([ADR 0087](0087-multi-index-server.md)) lets the UI add a folder to the
workspace by typing its path, so the server must read a folder it was not
started on and start an index run there. This revision records the rules
for that; design question D5 of the T038 task record (internal), whose
recommended default the developer accepted on 2026-11-24.

### Options considered

- **Two guarded POST endpoints with a confirm value (chosen).**
  `POST /api/v0/workspace/check` validates a typed path and reads nothing
  but file metadata, `graphvein.yaml`, `graphvein-project.yaml` and the
  folder's store (read-only); `POST /api/v0/workspace/add` registers the
  folder and starts its run, and needs the one-time `confirm` value that a
  `ready` check returned for exactly that resolved folder (10 minutes,
  used up by any add). The UI's confirmation dialog ("Index <path>?
  Graphvein will read every file in it") shows that answer; a script
  cannot skip the step that names the path.
- A folder picker (native dialog). Not available to a page under this
  CSP and loopback model. Rejected.
- Only a "copy the command" helper. Kept as well (`command` in the check
  answer), but alone it does not meet the requirement. Rejected as the
  only way.

### Decision

1. Both endpoints, and `POST /api/v0/workspace/remove`, pass every check
   of a state-changing POST (Host allow-list, `Sec-Fetch-Site`, `Origin`,
   `http.CrossOriginProtection`) plus a loopback peer (403
   `not_loopback`), the per-process session token in `X-Graphvein-Token`
   (403 `token_required`; served by `GET /api/v0/workspace` behind the GET
   guard), `Content-Type: application/json` and one strict JSON object of
   at most 4 KiB, as the manifest edit (ADR 0075) does.
2. Only absolute paths are accepted (`~` and variables are not expanded);
   symbolic links are resolved and the resolved folder is what is shown,
   registered and indexed.
3. Refused: a missing path, a file, the filesystem root, the home folder
   itself, the user configuration folder itself and the registry's folder
   (and anything inside it), any path inside a `.graphvein` or `.git`
   folder, a folder inside an already registered folder (the answer names
   that entry), a manifest or `graphvein.yaml` that does not load, and any
   other error reading the path. A folder that is already registered is
   answered as such, without a confirm value.
4. The index of an added folder is written where `graphvein index` writes
   it (`<folder>/.graphvein/index.db`, or the configured store). No cancel
   (ADR 0052); only existing project manifests are added.

The rules are claim rows WS-21..WS-38 of
[the workspace contract](../contract/claims/workspace.md).

### Consequences

- Section 6's "GET and HEAD only" gains three POST-only paths. The server
  can now read and index any folder a local user types, behind the same
  bar as the manifest edit.
- The "No authentication against other local processes" known gap
  stands: any local process can still run `graphvein index` itself; the
  loopback peer, the token and the confirm value keep other web pages and
  remote peers out, not local processes.

## Review notes (delegated review, 2026-11-25, T038)

Revision 1 (typed folder paths behind the existing guards plus a confirmation) accepted with ADR 0087 after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS)). The guards held in the independent review (Host/Origin/Fetch Metadata, loopback, session token, single-use confirmation bound to its folder, symlink and traversal refusals).

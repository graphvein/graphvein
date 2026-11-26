# Graphvein web UI

The local web UI that `graphvein serve` embeds: React 19, TypeScript (strict),
Vite, Tailwind CSS v4 and shadcn/ui components on Radix (ADR 0041). The
build output is committed in `internal/server/ui/static`, so `go build` and
`go test` never need Node. You need Node only to change the UI.

## Versions

- Node **24.22.2** with npm **11.19.1** (`.nvmrc`, `packageManager`,
  `engines`, `engine-strict=true` in `.npmrc`). Other versions are refused,
  because a different toolchain can produce a different bundle.
- Every dependency is pinned to an exact version (`save-exact=true`) and
  locked in `package-lock.json` (lockfile v3). Install with `npm ci`, never
  `npm install`, unless you are deliberately changing a dependency (which
  also updates ADR 0041).

## Workflow

In Git Bash (Windows), or any POSIX shell:

```
cd web
npm ci
npm run dev          # http://127.0.0.1:5173/, proxies /api to 127.0.0.1:7420
```

Run `graphvein serve <repo>` (default address `127.0.0.1:7420`) in another
terminal so the dev server has an API to talk to.

Scripts (they work under cmd.exe and sh):

| script | does |
|---|---|
| `npm run typecheck` | `tsc --noEmit` |
| `npm run lint` | ESLint (typescript-eslint strict type-checked, react-hooks) |
| `npm test` | Vitest with jsdom |
| `npm run build` | Vite build into `../internal/server/ui/static`, then `third-party-licenses.txt` (`scripts/third-party-licenses.mjs`) |
| `npm run licences` | licence allow-list check over `package-lock.json` |

## Rebuilding the committed assets

After any change under `web/`, from the repository root:

```
bash scripts/check-web.sh
git add internal/server/ui/static
```

`check-web.sh` checks the Node and npm versions, runs `npm ci`, the licence
check, typecheck, lint, tests and the build, and fails when
`internal/server/ui/static` differs from what is committed (including a
generated file that an ignore rule would hide). CI runs it on Linux when
web inputs change (`web/`, the committed build, the server goldens in
`internal/server/testdata/`, the workflow or the script); the Linux build is
canonical.

`npm run build` writes the complete output: Vite empties the directory and
builds the bundle, then `scripts/third-party-licenses.mjs` writes
`third-party-licenses.txt`. That script also checks the bundle against
`scripts/bundled-tools.mjs`, the build tools whose own code the build copies
into the output (Tailwind CSS, tw-animate-css, the bundler's CommonJS
helpers, Vite's preload helper for the lazy Flow chunk); their licences are held to the runtime allow-list and listed in
the notice file. It fails when the bundle contains a known marker of a tool
missing from that list, or when a listed tool left no marker.

The licence check accepts one exception to the permissive runtime list,
recorded in ADR 0046: `elkjs` at exactly 0.12.0 under
`EPL-2.0 OR GPL-3.0-or-later`, taken as EPL-2.0. Any other version fails,
so an upgrade needs a new review (and a new source statement in
`scripts/third-party-licenses.mjs`). New dependencies are resolved with
`npm install --before=<two weeks ago>` so nothing younger than 14 days
enters the lockfile.

`.npmrc` sets `ignore-scripts=true`: no package needs an install script.
The only package with one, `fsevents` (an optional macOS file watcher for
the dev server), ships a prebuilt `fsevents.node`, so skipping its
`node-gyp rebuild` changes nothing; Tailwind's native and WASM engines are
plain optional packages without scripts. Note
that npm then also skips `pre`/`post` scripts, which is why the build runs
the licence step inside the `build` script itself.

## Layout

- `src/api/` status types, the hand-written `parseStatus` guard and the
  fetch client; `endpoints.ts` (types, guards and requests of the API
  section endpoints, `index_changed` retried once) and `categories.ts` (raw
  status to category; only `linked` is linked). Tests parse the Go server's
  goldens from `internal/server/testdata/` (`src/test/fixtures.ts`,
  `src/test/apiFixtures.ts`) so the two sides cannot drift.
- `src/hooks/useStatus.ts` polling (30 s while visible, on focus and on
  "Check"; never two requests at once).
- `src/router.ts` hash routing (`#/api`); the server owns `/api/`.
- `src/shell/` sidebar, top bar, freshness and extractor badges, banner.
  The top bar lays out by its own width (a container query named `top`,
  `TopBar.tsx`): one row with every label from 1300 px, one row with the
  badge's time and Check's label in their tooltips from 980 px, two rows
  of fixed height below.
- `src/hooks/useEndpoints.ts`, `useEndpointDetail.ts`, `useCalls.ts` one
  request per parameter set (`useApiRequest.ts`); a new key drops the old
  answer at once.
- `src/sections/api/` the API section (ADR 0042): toolbar, coverage cards
  and compact strip with the per-root breakdown (folded to one line when it
  would wrap: `RootsFold.tsx`, pure `foldRoots` in `coverage.ts`), endpoint
  list, detail,
  callers table, never-called state, gap sheet (native `<dialog>`), split
  pane, deep-link parameters (`params.ts`, `#/api?...`), the detail tabs.
- `src/sections/api/flow/` the Flow tab (T016f2, ADR 0045/0046): pure
  `tiers.ts` (edge tiers), `graph.ts` (response to cards and lines, hops,
  near misses, folding, filter, peek), `measure.ts` (card sizes with the
  360 px cap and the middle ellipsis) and `layout.ts` (ELK input and the
  adapter for its answer), `elk.ts` (the worker client);
  `FlowTab.tsx` is the lazy chunk, the only module that imports
  `@xyflow/react`; `FlowControls.tsx` stays in the entry chunk.
  `src/api/flow.ts` and `src/hooks/useFlow.ts` load the data.
- `src/sections/schema/` the Schema section (T018b2, ADR 0048): pure
  `state.ts` (page states, visible groups, common state), `entry.ts` (the
  sidebar tag), `cells.ts` (every cell and value rule, 1:1 with its test),
  `map.ts` (compact map and diagram edges, the 1-hop neighbourhood above
  25 tables) and `params.ts` (`#/schema?...`); `MiniMap.tsx` and
  `OverviewTab.tsx` are lazy chunks using the Flow tab's ELK worker
  client; only `OverviewTab.tsx` imports `@xyflow/react`. `src/api/schema.ts`
  and `src/hooks/useSchema*.ts` load the data (`/sets` once per index run);
  the shell owns the summary request. Tests use the server's schema
  goldens plus crafted variants (`src/test/schemaFixtures.ts`).
- `src/sections/changes/` the Changes section (T019b, ADR 0056, V3
  "Global compare, three panes"): pure `words.ts` (every wording of a
  change value, 1:1 with `words.test.ts`), `refusal.ts` (refusal and
  empty pages by error code), `impact.ts` (path groups, fan-out folding),
  `params.ts` (`#/changes?...`); `usePaged.ts` appends cursor pages.
  `src/api/changes.ts` holds the guards and client of
  `/api/v0/changes/*`; `src/shell/compare.tsx` the shell's compare state
  (base per browser, the one summary request, the branch job, the
  resolved comparison every request key includes, and `useCheckedAnswer`,
  which withholds an answer from another index run than the summary) and
  `src/shell/CompareChip.tsx` the top bar chip and base menu. The
  "changed since" markers of the API, Flow and Schema views (T019b part
  2): pure `markers.ts` (which groups each view reads, every marker word,
  the identity lookup; 1:1 with `markers.test.ts`), `src/shell/markers.tsx`
  (one load per group and resolved comparison, shared by the views; off
  with the chip, dropped together when the base is refused, off when the
  comparison's head is not the current index) and
  `src/shell/ChangeMarker.tsx` (the chip-like marker, a link or, inside a
  listbox option, a decoration). `assumptions.tsx` shows the `assumes`
  ids of I8 paths with their catalogue texts and the Schema view's
  popover. Tests wrap
  the T019a goldens `testdata/fixtures/change-impact.*.golden.json` in the
  endpoints' envelopes (`src/test/changesFixtures.ts`).
- `src/components/VirtualList.tsx` the hand-written fixed-height windowed
  listbox (no virtualization dependency).
- `src/components/CodePeek.tsx` the reusable code peek panel (T016e2, ADR
  0044): give it a node identity (and optionally the expected index run,
  a title, a step and a close handler); it requests `/api/v0/source`
  itself (`src/api/source.ts`, `src/hooks/useSource.ts`) and draws the
  highlight only from the server's `highlight`. No API-section imports, so
  graph views can reuse it. `src/components/VerticalSplit.tsx` is its
  top/bottom resize split.
- `src/lib/ide.ts` editor button labels (`ide.label`, then the scheme).
- `src/sections/NotFound.tsx` the page for unknown hashes.
- `src/components/ui/` copied shadcn/ui components; see its README.
- `src/styles/tokens.css` the M5 design tokens (plus `--code-bg`,
  `--code-hl`, `--code-sp` for the code peek and `--edge`, `--canvas`,
  `--dotgrid` for graph views); `src/index.css` maps them to
  Tailwind colours.

`go.mod` in this directory only fences `web/` off from the Graphvein Go
module: `node_modules` contains Go files that `go test ./...` would
otherwise build.

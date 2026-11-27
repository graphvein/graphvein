# ADR 0046 - Flow tab graph libraries and the elkjs licence exception

**Status:** Accepted (delegated review, 2026-11-04)

## Context

The Flow tab (T016f2, the T016f task record (internal)) draws the stored
neighbourhood of an endpoint from `GET /api/v0/api/flow` (ADR 0045). The
developer decided the graph stack in the T016 task record (internal)
("Graph views"): React Flow (`@xyflow/react`) for the canvas and ELK
(`elkjs`) for the layout, ELK loaded lazily in a web worker only when a
graph opens.

ADR 0041 (decision 4) allows only permissive licences for runtime packages
(`MIT`, `ISC`, `BSD-2-Clause`, `BSD-3-Clause`, `Apache-2.0`, `0BSD`) and
fails any SPDX expression, so a copyleft or dual-licensed package needs a
recorded decision. elkjs is Eclipse Public License 2.0 (weak, file-level
copyleft); from 0.12.0 on it is published as `EPL-2.0 OR GPL-3.0-or-later`.
The developer answered UQ2 on 2026-11-03: **B**, elkjs pinned at exactly
0.12.0, taken under EPL-2.0, with the safeguards below. ADR 0041's CSP
(`script-src 'self'`, no `worker-src`) blocks `blob:` workers and allows a
classic same-origin worker (T016f F6). This ADR records the licence policy
change and the dependency, chunk and size decisions of T016f2 that ADR 0041
asks to be recorded (ADR 0045's section 8 points here; ADR 0045 is
accepted and not edited).

## Options considered

### Layout library and licence (UQ2)

- A: elkjs 0.11.1 (`EPL-2.0` only) with a name+version exception.
- **B: elkjs 0.12.0 (`EPL-2.0 OR GPL-3.0-or-later`), taking EPL-2.0
  (chosen by the developer).** Same obligations as A; the check accepts
  that one expression for that one package and version.
- C: a permissive layout (`@dagrejs/dagre`): no orthogonal routes per edge,
  which is why ELK was chosen; reopens the library decision.

### How ELK's code reaches the page

- **The worker file only, unmodified, plus Graphvein's own client of the
  worker's message protocol (chosen).** `elkjs/lib/elk-worker.min.js` is
  imported with Vite's `?url`, so the build copies it byte for byte to
  `/assets/elk-worker.min-<hash>.js` (checked: identical to the package
  file and to the file in the elkjs `0.12.0` tag). `web/src/sections/api/
  flow/elk.ts` posts `{id, cmd: "register" | "layout", ...}` and reads
  `{id, data}` / `{id, error}`, the protocol of elkjs 0.12.0's
  `lib/elk-api.js`; no elkjs file is bundled into Graphvein's chunks, so
  the EPL-2.0 code stays in its own file. A test runs the client against
  elkjs's real worker code (its in-process build).
- `elkjs/lib/elk-api.js` bundled into the Flow chunk: 9.8 KB of EPL-2.0
  code minified together with Graphvein code in one file; rejected to keep
  the file boundary clean.
- `elk.bundled.js` in the page or a `blob:` worker: the first runs layout
  on the main thread, the second is blocked by the CSP (F6).

### Chunking

- **A `React.lazy` Flow chunk (chosen)** holding `@xyflow/react`, its CSS
  and the tab's components; the worker starts on the first layout. The
  entry chunk contains neither React Flow nor ELK (checked in the build and
  in the browser: no Flow chunk or worker request before the tab opens).
- Vite 8 wraps every dynamic import in its preload helper (it also loads
  the lazy chunk's CSS with a `<link>`, allowed by `style-src 'self'`);
  the helper's code is in the entry chunk even with `build.modulePreload:
  false`. It is listed as a bundled tool (`web/scripts/bundled-tools.mjs`,
  marker: its `meta[property=csp-nonce]` lookup), so its licence notice is
  served; the modulepreload polyfill stays out.

## Decision

1. Runtime dependencies, exact pins: `@xyflow/react` **12.12.0**
   (2026-09-24, MIT) and `elkjs` **0.12.0** (2026-07-17). The lockfile was
   resolved with `npm install --before=2026-10-20`; every new entry was
   published at least 14 days before 2026-11-03 (latest: `@types/d3-zoom`
   3.0.9, 2026-09-29). New entries: `@xyflow/system` 0.0.83, `zustand`
   4.5.7, `use-sync-external-store` 1.7.0, `classcat` 5.0.5 (MIT);
   `d3-color` 3.1.0, `d3-dispatch` 3.0.1, `d3-drag` 3.0.0, `d3-interpolate`
   3.0.1, `d3-selection` 3.0.0, `d3-timer` 3.0.1, `d3-transition` 3.0.1,
   `d3-zoom` 3.0.0 (ISC), `d3-ease` 3.0.1 (BSD-3-Clause); `@types/d3-color`
   3.1.3, `@types/d3-drag` 3.0.7, `@types/d3-interpolate` 3.0.4,
   `@types/d3-selection` 3.0.12, `@types/d3-transition` 3.0.9,
   `@types/d3-zoom` 3.0.9 (MIT; runtime dependencies of `@xyflow/system`).
   None of the d3 patch releases of 2026-11-03 is in the lockfile.
2. Licence check (`web/scripts/check-licences.mjs`): an `EXCEPTIONS` list
   with exactly one entry, `{name: "elkjs", version: "0.12.0", licence:
   "EPL-2.0 OR GPL-3.0-or-later"}`, recorded as taken under EPL-2.0. Any
   other version or licence string of elkjs fails ("review the new
   version"), and so does the exception without the package. Every other
   runtime package stays on ADR 0041's permissive allow-list; SPDX
   expressions still fail for every other package.
3. Graphvein takes elkjs under **EPL-2.0** and distributes it unmodified in
   object code form, as the separate worker file above.
4. The served licence page (`/third-party-licenses.txt`, generated by
   `web/scripts/third-party-licenses.mjs`) carries elkjs's `LICENSE.md`
   (the EPL-2.0 text) and, before it, a statement that Graphvein takes
   elkjs under EPL-2.0 and ships it unmodified as a separate file, with the
   source: `https://github.com/kieler/elkjs/tree/0.12.0`, the npm tarball
   `https://registry.npmjs.org/elkjs/-/elkjs-0.12.0.tgz`, and the ELK
   release it is built from, `https://github.com/eclipse-elk/elk/tree/v0.12.0`.
   The statement is keyed by name and version and fails the build when the
   package is gone.
5. CSP unchanged: the worker is a classic same-origin script under
   `/assets/` (no inline, `blob:` or `data:` worker); one worker per page,
   created on the first layout, reused; a layout over 10 s terminates it and
   shows an error with Retry.
6. Embedded size bound (`internal/server/static_test.go`) raised from
   2 MiB to **3 MiB**, as far as needed: the tree is 2,611,567 bytes (was
   587,035). Measured (raw / gzip -9): ELK worker 1,595,334 / 461,891; Flow
   chunk 212,332 / 67,792 plus its CSS 16,737 / 2,852; entry chunk 476,575 /
   142,604 (was 449,835 / 134,752: the tab strip, the flow guard and graph
   building, the list and sidebar rails and Vite's preload helper; no React
   Flow or ELK code); entry CSS 59,812 / 10,979; licence page 249,780 (was
   114,209; most of the growth is vite's own `LICENSE.md`, now listed as a
   bundled tool). The binary grows by about the tree's growth (2.0 MB).

## Consequences

- A version change of elkjs fails the licence check until this ADR (or a
  successor) reviews it; so does the source statement, which names the
  version.
- The worker protocol client must be re-checked against the new
  `lib/elk-api.js` on any elkjs upgrade (the worker test helps).
- React Flow's attribution link stays visible (bottom left of the canvas);
  it is a plain link and opens nothing by itself.
- The entry CSS is unchanged by React Flow: its CSS is in the Flow chunk's
  stylesheet, loaded with the chunk.

## Known gaps

- The project has no LICENSE file of its own (T016f F5); whether EPL-2.0
  components fit the project's own licence is the developer's call before
  shipping.
- Which ELK commit elkjs 0.12.0 was compiled from is not recorded in the
  package: elkjs's CI builds against ELK `master`, its README release
  procedure against the matching ELK release tag. The source statement
  names the tag `v0.12.0` and the elkjs tag, whose committed
  `lib/elk-worker.min.js` is identical to the shipped file.
- Vite's licence notice is large (98 KB) because vite's `LICENSE.md` lists
  the dependencies of vite itself, not only the helper's code.
- Layout caching is per page and in memory (24 layouts).

## Review notes (delegated review, 2026-11-04)

- Developer decision (2026-11-03): elkjs 0.12.0 under its EPL-2.0 option; @xyflow/react 12.12.0; MSAGL 1.1.24 (MIT) rejected after a comparison (layer constraints unimplemented, 1 release in 24 months), dagre rejected (routes through nodes on dense graphs).
- Independent review of T016f2: no blocker; licence exception is exactly elkjs 0.12.0 with exactly "EPL-2.0 OR GPL-3.0-or-later", runtime only; shipped worker byte-identical to the npm tarball's `lib/elk-worker.min.js`; all 21 new lockfile entries at least 14 days old; no eval, `new Function` or `blob:`; CSP unchanged; precision checks pass (only stored edges or labelled candidates drawn; "defined in" edges untiered; hop-2 callers through enclosing functions labelled as such).
- Revisions after review: back edges (handler -> route) laid out forward with the arrowhead kept at the stored target; Flow-tab peek height, sticky controls and pan-to-selection; focus restore after re-layout; chip units; minimap never covers a card; first view fitted to call sites, endpoint and handler with pan pills (recorded decision "G1 starts fitted to callers-route-handler").
- Gates (2026-11-03): Langflow (5 routes) and kftray (2 commands): on-screen numbers equal /flow and /endpoint, drawn lines equal stored edges, 0 CSP violations or console errors, text >= 12 px, nothing under overlays; acceptance route GET /api/v1/flows/{flow_id} shows its 3 callers, read_flow and the hop-2 callers; largest flow laid out in 0.93 s.
- Known gaps: on the largest Langflow flow the handler starts off-screen (call cards over 600 px wide; reachable by the pill); wide call cards clip the column caption; changing hops re-lays out the whole graph; React Flow attribution kept visible.

## Revision (T073): the legend folds into a strip with a Legend button

Status of this revision: Proposed (pending the developer's review; the
decisions above keep their status).

### Context

T016f D6 (UQ4 A) placed the whole legend in an always visible strip
below the canvas. The T071 design review measured it on owncast (`GET
/api/config`, 2 callers): 117 px in three rows under a 442 px graph at
1440x900, 195 px under a 200 px graph at 900x800, and 5 of its 7 line
kinds had no line on screen. The developer chose T071 question 8 with its
default on 2026-11-27.

### Options considered

- Keep the always visible legend (D6): every kind is always explained,
  at the cost of up to half the canvas on a short pane.
- A strip of the drawn kinds plus a Legend button opening the full legend
  (T071's recommendation, chosen).
- Hide the legend entirely behind a button: the filter chips of the drawn
  kinds would be one click away too; rejected.

### Decision

- The strip shows the kinds with a line on this screen as filter chips
  (unchanged: sample, label, count, toggle), "+ n kinds with no line
  here" (label table row 73; n counts the listed tiers at 0 plus Dynamic
  and Unmatched), the calls' precision labels while the strip is at least
  1000 px wide (the route's own label is in the route header) and a
  Legend button, the same disclosure control as the Architecture map's.
- The button opens the full legend over the bottom of the canvas (no
  re-layout): every tier the filter can dim with its count, Dynamic and
  Unmatched disabled at 0 with their gap sheet links, "defined in", the
  other-target box, the wrapper pill, the changed dot, the notes and
  every precision label. Esc or the button closes it. The choice is not
  remembered: it is closed on every visit.

### Consequences

- At 0 a kind is counted, never shown as absent; its explanation and its
  gap sheet are one click away instead of always on screen.
- The graph gains the legend's extra rows (measured in the T073 record).
- `Legend.test.tsx` and the legend cases of `FlowTab.test.tsx` and
  `places.test.tsx` test the strip and the opened legend.

### Known gaps

- The 1000 px threshold for the precision labels is a container query on
  the strip, not measured from the chips.

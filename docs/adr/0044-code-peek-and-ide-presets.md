# ADR 0044 - Code peek source endpoint and IDE presets

**Status:** Accepted (delegated review, 2026-11-03)

## Context

The developer asked (2026-11-03, task T016e) to show a node's real source
inside the app: the lines around its span, the exact span highlighted and
an "Open in editor" button, honest about freshness. The endpoint must
follow ADR 0041's loopback, Host, `Sec-Fetch-Site`/`Origin` and CSP rules,
be confined to the indexed repository (no traversal, no symlink escape, no
file outside the indexed files) and have size and line caps. The editor
should be a one-word setting with verified URL schemes, keeping
`ide.link_template` for anything else, and Windows paths must work. The
panel will be reused by the Flow tab, trace lanes and the overview
(T016-ui-decisions, "Graph views").

VISION's precision principle governs the highlight: never mark lines as the
node's span unless that is proven. What the store holds decides what can be
proven (T016e F1): the whole-file content hash (`files.content_hash`,
read with `Store.FileHashes`), the node span (1-based lines, 0-based UTF-8
byte columns on LF-normalised source) and the node content hash, which for
source nodes covers exactly the span bytes (contract v1 "Hashes", ADR
0005). No indexed source text is stored anywhere, so a changed file can be
compared with its indexed version only through the span hash.

The scanner (`internal/scan`) never follows a symlink, reads only regular
files, and does not index files with a NUL byte or invalid UTF-8 (T016e
F4). Before this ADR the default IDE template was
`vscode://file/{abs_path}:{line}:{column}`, which yields
`vscode://file//home/u/a.ts` on Linux and macOS: VS Code turns that leading
`//` into a UNC authority, a different resource from the workspace's file
(T016e F5). Golden tests replaced the root with `{ROOT}`, which hid the
double slash.

Editor URL schemes were read from primary sources fetched from
raw.githubusercontent.com on 2026-11-03 (T016e F6, re-checked in T016e1
A1); cursor.com, docs.cursor.com, zed.dev and sublimetext.com are blocked
by the environment's proxy and were not read.

## Options considered and decisions

### 1. Source by node identity

- **Chosen:** `GET /api/v0/source?id=&context=`, where `id` is a stored
  node's identity. The only file opened is that node's stored `file`, and
  only when it is a valid contract path and a key of `FileHashes`.
- A `file`+`line` endpoint: the request path becomes the traversal
  surface, and every check must then hold against hostile input. Evidence
  spans can later be peeked by edge identity instead.
- Serving raw files under `/files/`: the same surface plus a content type
  and caching question, and no freshness statement.

The endpoint is generic (`/api/v0/source`, not under `/api/v0/api/`)
because graph views reuse it. It uses ADR 0042's runs-before/after
consistency check (one retry, then 503 `index_changed`) and `store_error`.

### 2. Highlight rule

- **Chosen:** `exact` when the current file hash equals the stored hash and
  the span is in bounds on UTF-8 boundaries; `text_match` when the hash
  differs but the bytes at the stored span are non-empty, hash to the
  node's content hash and occur exactly once in the current file
  (overlapping occurrences counted); otherwise no highlight (`changed` with
  a reason, `span_invalid` for an unchanged file whose span does not fit).
  Graphvein highlights exactly the bytes whose hash it checked, so a
  producer whose node hash covers another range gets no highlight, never a
  wrong one. The uniqueness test removes the trap of identical short text
  (`fetch(url)` in two functions) landing on the old position after an
  edit; empty spans hash the same everywhere and never match.
- Highlight only in unchanged files: safe, but loses every highlight after
  an unrelated edit in the same file.
- Best-effort relocation of a moved span: a guess; the span length of a
  multi-line span is not stored, so a hash alone cannot locate it.
- Storing indexed text: a store and size change, out of scope.

The response states the result explicitly (`exact`, `text_match`,
`changed`, `span_invalid`, `missing`, `not_regular`, `too_large`, `binary`,
`unreadable`, `no_location`, `not_indexed`; docs/cli/serve.md). The
presented node in the same response is built from the same bytes
(`query.NewPresenterWithReader`), so `node.ide.exact`, `node.file_state`
and `source.state` always agree.

### 3. Confinement

- **Chosen:** open the root with `os.Root` per request, `Lstat` every path
  component and the file through it (any symlink, or a component that is
  neither a directory nor, for the last one, a regular file, is
  `not_regular`; a component that is a regular file is `missing`, which is
  what opening through it reports), open the file, require the opened file
  to be the regular file just checked (`os.SameFile`), and read at most
  4 MiB + 1 bytes. No other path API is used. The root itself may be a
  symlink; the user chose it.
- `filepath.EvalSymlinks` prefix checks: racy between check and open.
- `os.Root` alone: it follows symlinks that stay inside the root, which
  the scanner never does, so the endpoint could show a file that was not
  the indexed one.

### 4. Caps

4 MiB per file (checked by `Stat` and by a limited read), 400 lines per
window, 4,096 bytes per line (cut at a rune boundary and listed in
`cut_lines`), `context` 0 to 50 with default 8. A3 of T016e1: no tracked
text file of the ten corpus checkouts exceeds 4 MiB (the six files above
it are images, audio or NUL-containing data the scanner does not index).

### 5. UTF-16 highlight columns

- **Chosen:** highlight columns in UTF-16 code units within the returned
  line text, which is how a browser slices strings, so the client computes
  nothing.
- Byte columns: every client would convert, and a client bug would draw a
  wrong range.

### 6. IDE presets

- **Chosen:** a data table in `internal/config/ide.go`: `vscode` (default),
  `vscode-insiders`, `cursor`, `vscodium`, `jetbrains`, `zed`. `ide` is an
  editor name (`ide: cursor`) or a mapping with `editor`, `label`,
  `link_template`, `column_unit`, `column_base`; each explicit key
  overrides the preset, the preset supplies the rest. An explicit
  `link_template` drops the preset's label (the template may open another
  editor), even when it equals the preset's template. Strict decoding as
  before; an unknown editor names the valid ones. The server reports the
  label as `ide.label` in every `ide` status object (additive).
- Presets as template aliases only: no label, no column settings.
- A free `label` without presets: every user would copy schemes by hand.

Verified formats (sources in `internal/config/ide.go` and
docs/config.md):

| editor | template | column | status |
|---|---|---|---|
| vscode, vscode-insiders | `<scheme>://file{abs_url_path}:{line}:{column}` | UTF-16, 1-based | verified (docs and handler) |
| vscodium | `vscodium://file{abs_url_path}:...` | as VS Code | verified (build script; VS Code's handler) |
| cursor | `cursor://file{abs_url_path}:...` | as VS Code, assumed | unverified (docs blocked, source not public) |
| jetbrains | `idea://open?file={abs_path}&line={line}&column={column}` | code points (logical column), 0-based | verified from source; macOS and IntelliJ IDEA only |
| zed | `zed://file{abs_path}:{line}:{column}` | code points, 1-based | verified from source |

A1 corrected T016e F6 on one point: Zed's column is not a byte offset.
`navigate_to_positions` (crates/recent_projects/src/remote_connections.rs)
passes `column - 1` to `point_from_external_input`
(crates/text/src/text.rs), which counts characters (`chars()`), so the zed
preset uses `codepoint`.

Review correction (T016e1 review): the jetbrains preset first used
`utf16`. IntelliJ's `LogicalPosition` javadoc
(JetBrains/intellij-community
platform/editor-ui-api/src/com/intellij/openapi/editor/LogicalPosition.java)
says the column counts characters with "surrogate pairs of characters
... counted as one column" and a tab occupying "up to the tab size", so
the preset uses `codepoint` (column base 0 unchanged).

### 7. JetBrains

- **Chosen:** `idea://open?file=...` (registered by IntelliJ IDEA's macOS
  bundle).
- JetBrains Toolbox `jetbrains://<tool>/navigate/reference?project=...`:
  needs the IDE project name or a git remote, which Graphvein would have
  to guess.
- No JetBrains preset: loses the macOS IDEA users for no gain.

### 8. No Sublime Text preset

Sublime Text registers no URL scheme of its own; `subl://` needs a
third-party handler with differing formats, and its documentation could not
be fetched. A preset that silently does nothing without extra software
would mislead. docs/config.md shows a `link_template` and `label` for such
a handler.

### 9. `{abs_url_path}` and the default

`{abs_url_path}` is `{abs_path}` with exactly one leading slash
(`/home/u/a.ts`, `/C:/src/a.ts`; a UNC path keeps its two). The default is
the `vscode` preset, so the default link on Linux and macOS loses the
double slash; on Windows it is byte-identical. A template written with
`{abs_path}` keeps its exact output. The golden normalisers
(`golden.ReplaceRoot`) now treat `{ROOT}` as the root without its leading
slash, always written after a slash, so committed links stay
`vscode://file/{ROOT}/...` on every OS while an accidental double slash on
POSIX stays visible as `vscode://file//{ROOT}/...`.

## Consequences

- One new read-only endpoint; no store, contract, schema, extractor or
  dependency change. The handler opens no socket (ADR 0040 boundary).
- `graphvein query`, `callers` and `impact` links change on Linux and macOS
  only by the removed double slash; Windows output is unchanged.
- The ADR 0042 responses gain `ide.label`; their goldens gain exactly that
  field.
- Clients must treat an unknown `state` as "no highlight, show the state".

## Known gaps

- Cursor's scheme is unverified until a developer clicks a link (T016e1
  gate G7); Zed on Windows and every preset other than the developer's
  installed editors are not run.
- JetBrains: macOS and IntelliJ IDEA only; columns differ where tabs are
  expanded (a logical column counts a tab up to the tab size, the preset
  counts it as one code point).
- `text_match` residual case: the node's own text was deleted and a single
  other copy moved exactly to the old line and column.
- No relocation of moved spans; no peek of edge evidence spans.
- `FileHashes` is read on every request (all stored files), and one SQLite
  read connection serialises requests; latency on Langflow is measured in
  the T016e1 gates (G4).
- A swap of an in-root directory for a symlink between the `Lstat` walk and
  the open can make the open resolve through the symlink (inside the root,
  `os.Root` keeps it there), but the final `SameFile` check compares the
  opened file with the `Lstat` of the final component, so only the same
  inode that the walk checked is read; any other file is `not_regular`.
- A regular file swapped for a FIFO between the `Lstat` walk and the open
  can block the request goroutine in the open (a local denial of service by
  someone with write access to the repository; nothing outside the root is
  read). A non-blocking open needs `syscall`, which is outside
  `internal/server`'s allow-list.
- An IDE link's column on line 1 of a file with a byte order mark counts
  the mark (unchanged behaviour of the presenter); the peek's highlight
  drops it.

## Review notes (delegated review, 2026-11-03)

- Independent review of T016e1: no blocker or major findings. Confinement (node id only, FileHashes key, ValidPath, os.Root, Lstat walk, SameFile) and highlight precision (exact only on equal file hash; text_match only for a non-empty span whose bytes hash to the node and occur exactly once, overlaps counted) verified, with mutation tests.
- Revision after review: the JetBrains preset counts code points (IntelliJ LogicalPosition counts a surrogate pair as one column), not UTF-16 units; overlap test added; Known gaps corrected (directory-to-symlink swap) and extended (FIFO swap can block one request).
- Gates (2026-11-03): span-hash audit 78,645/78,645 nodes on all ten public corpus checkouts; largest indexed file 3.1 MB (Langflow uv.lock) under the 4 MiB cap; Langflow source latency medians 25-32 ms, 96-98 ms for the 3.1 MB file (gate 100 ms; a file nearer the cap may exceed it). CI green incl. postgres.
- Open: the Cursor scheme is unverified (cursor.com was not reachable from the test environment) and Windows editor links need a developer click check (G7).

## Amendment (T016e2): code peek UI

**Status:** Accepted (delegated review, 2026-11-03)

This amendment records the panel contract of the web UI that consumes
`GET /api/v0/source` (T016e2). It changes nothing above; the header
status applies to the endpoint and presets only.

- **Component.** `web/src/components/CodePeek.tsx` takes a node identity
  plus an optional source client and fetches the source itself, so any
  view (the API detail pane now, the Flow tab and trace lanes later) can
  show a node without knowing the response shape. The API section places
  it in a bottom split of the detail pane (`VerticalSplit`, resizable,
  height kept in localStorage).
- **States shown.** Loading, failure (with Retry) and every no-source
  state keep the panel's size. `exact` shows the lines with the highlight;
  `text_match` the highlight plus an amber banner; `changed` and
  `span_invalid` the current lines with no highlight and a high-contrast
  "lines may have moved" banner; `missing`, `not_regular`, `too_large`,
  `binary`, `unreadable`, `no_location` and `not_indexed` state their
  reason with no code. An unknown state shows its lines without a
  highlight and names the raw state. An answer of another index run than
  the surrounding view is not shown (notice with Retry). Banners sit
  outside the scrolling code area; cut lines and a truncated window are
  noted.
- **Highlight only from the server.** The panel bands and marks exactly
  the server's `highlight` range (UTF-16 columns), and only for `exact`
  and `text_match`; the client guard drops a highlight sent with any other
  state. The panel never computes, guesses or moves a range; a changed
  file scrolls to the stored start line without marking it.
- **No-source behaviour.** The IDE button is disabled with the reason as
  its name whenever no lines were read (and on a run mismatch); it is
  enabled whenever lines were read, labelled from `ide.label`, else from
  the link's scheme, else "Open in editor".
- **Context.** 8 lines each side; "Show more" above or below adds 8, up to
  50. The previous answer of the same node stays visible while more lines
  load; an answer for another node, context or reload is never shown under
  the current header.
- **Keyboard and mouse.** A caller or near-miss row click opens its peek
  (clicks on links, buttons and text selections inside the row do not);
  each row has a `</>` toggle button (pressed while open, named after the
  caller and its location) and the handler has one. Rows are one roving
  tab stop; while focus is on a row or a control inside it, Up/Down move
  between rows (the open peek follows), Esc closes; Enter on the row
  toggles, Enter and Space on a control are the control's own. Esc inside
  the panel or its close button closes it, and focus returns to the
  element that opened it. The open row has `aria-expanded` and
  `aria-controls` naming the panel. While the peek is open the coverage
  cards collapse to the summary strip without its per-root rows (the
  toolbar's root filter remains).
- **Tokens.** Colours come only from Graphvein tokens: `--code-bg`,
  `--code-hl` (line band) and `--code-sp` (character mark), defined for
  light and dark in the indigo family; no syntax colouring, no new
  dependency.

Review notes (T016e2, delegated review 2026-11-03): independent review found no blocker, major or wrong-highlight issue (highlight only from the server's `exact`/`text_match`; stale responses never shown under another row's header). Revision after review: focus returns to the opener, row keys work from the row's controls, distinct accessible names with `aria-expanded`/`aria-controls`, race tests, and the compact strip drops its Roots rows while the peek is open. Langflow browser gate: 10/10 highlights equal the API span, 0 CSP violations, light and dark. Known gaps: the open peek is not in the URL hash; the shell footer reads "unknown" until the first status answer arrives on large repositories (wording predates T016e2).

## Amendment (2026-11-04, T016k): JetBrains column unit and byte order mark

**Status:** Accepted (delegated review, 2026-11-04)

Review correction (2026-11-04, T016k), recorded by ADR 0050 "Review fixes
R2"; the text above is kept as it was decided.

- The jetbrains preset's column unit is `utf16`, not `codepoint`.
  IntelliJ navigates `idea://open` by the offset of
  `DocumentUtil.calculateOffset` (UTF-16 units via `charAt`, a tab adds the
  tab size), reached from `OpenFileDescriptor` through
  `LazyRangeMarkerFactoryImpl`; the `LogicalPosition` javadoc cited in the
  T016e1 review correction is not the path the link takes.
- Known gap closed: "An IDE link's column on line 1 of a file with a byte
  order mark counts the mark". The presenter now drops the mark before
  converting, as the peek's highlight does.
- Known gap replaced (ADR 0050): "columns differ where tabs are expanded";
  a jetbrains link after a tab names the line only.

Review notes (2026-11-04, T016k, delegated review 2026-11-04): the
correction was verified from intellij-community and zed master
(2026-11-04) and re-checked end to end through `/api/v0/source?editor=`
for all six presets on the stacked head of 2026-11-04: "😀" before the node
counts two UTF-16 units for jetbrains, a line-1 byte order mark is not
counted by any preset, a jetbrains link after a tab or on a changed file
names the line only. Golden change limited to the cursor and jetbrains
notes in `ide-builtin.golden.json`. Full suite, corpora, timing and store
size as recorded in ADR 0050 "Review notes (2026-11-04)"; CI: [ci windows]
batch run on dd42753 green. Known gaps: see ADR 0050 (tab line-only,
Cursor unit assumed).

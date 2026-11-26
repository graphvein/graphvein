# ADR 0050 - Per-browser editor choice for IDE links

**Status:** Accepted (delegated review, 2026-11-04)

Numbering: the task (T016k) asked for the next free number; 0048 (T018b1)
and 0049 (T016h) are taken on other branches, so this is 0050.

## Context

`graphvein.yaml`'s `ide` setting (ADR 0044) chooses the editor of every IDE
link, for the CLI and for `graphvein serve`. The editor is a personal
choice, so a repository setting should only be the team default. T016k adds
a per-browser editor picker to the web UI (developer decisions of
2026-11-04: sidebar placement, picker A, K3 A, K4 A, K5 A, logos for three
editors).

A client-only picker cannot build correct links. The column depends on the
preset's unit and base (UTF-16 and 1-based for VS Code, code points and
1-based for Zed, code points and 0-based for IntelliJ IDEA); converting
needs the current line text and the file hash check, which only the server
has (caller rows, gap sheets and Flow cards carry no line text). Rebuilding
templates in TypeScript would duplicate `encodePath`, `absURLPath` and the
placeholder filling, and a drift would produce a confident wrong link.
Rewriting a link's scheme is wrong because the path form, the query syntax
and the column differ.

The server also could not tell Graphvein's built-in default from a team
choice: `config.Load` set `IDE.Editor` to `vscode` either way.

## Options considered

1. **A request parameter `editor=<preset>` on the node-presenting
   endpoints (chosen).** Links stay server-built from one code path; the
   client only names a preset.
2. Links for every preset in every response: six times the link data, and
   every client must select the right one.
3. Columns in every unit plus the absolute path, with templates filled in
   the client: duplicated encoding, the drift risk above.
4. A server-side per-user setting (cookie or user config file): state on the
   server, the CLI question the developer left for later, and not per
   browser.
5. Rewriting the scheme in the client: wrong links.

## Decision

- `/api/v0/api/endpoint`, `/api/v0/api/calls`, `/api/v0/api/flow`,
  `/api/v0/source`, `/api/v0/schema/table` and `/api/v0/schema/migration`
  (the schema endpoints that present nodes; `/schema/summary` and
  `/schema/sets` carry no links) take an optional `editor`. With it, links
  and the label come from that preset alone (`config.PresetIDE`); the
  repository's `ide` mapping, including `link_template`, `label`, `column_unit` and
  `column_base`, does not apply. An unknown or empty value is 400
  `bad_request` and never falls back (rule 14); a repeated one is 400. A
  requested editor builds links even when `graphvein.yaml` fails to load
  (K3 A): `ide.available` true, `ide.error` keeps the message.
- The `ide` status object gains `editor` (the preset whose format built the
  links; null for a custom repository template or when no links could be
  built) and `source` (`request`, `repository` or `builtin`). Without
  `editor`, a load error gives `available: false`, `editor: null` and
  `source: repository` (the file exists and its setting is what failed).
- `config.Config` records `IDEGiven` (a nonempty `ide` setting; `ide: {}`
  counts as not given) and `IDECustom` (an explicit `link_template`). They
  are fields of `Config`, not of `config.IDE`, so `IDE` values and their
  comparisons (CLI, query presenter, existing tests) are unchanged.
- `config.Preset` gains `Status` (`verified`, `partly_verified`, `limited`)
  and `Note`, next to the sources; docs/config.md shows the same words.
- New `GET /api/v0/ide`: `presets` (`editor`, `label`, `status`, `note`, in
  table order) and `repository` (`available`, `error`, `editor`, `label`,
  `custom`, `source`). It reads `graphvein.yaml` per request and never the
  index, and goes through the same security handler.
- The web UI stores the choice in `localStorage` key `graphvein.editor.v1`
  (a preset name or `off`; absent = repository default), checks it against
  `/api/v0/ide` before any data request, and sends it as `editor=`. Labels
  and source words come from each response's `ide` object, never from the
  stored choice. "No editor links" (`off`) is client-only: no parameter is
  sent and every button is disabled with the reason.
- Editor marks: official logos for VS Code (blue icon from Microsoft's
  branding zip, unaltered, with at least 25% clear space), IntelliJ IDEA
  (official product icon, unaltered, only at 24 px and above; smaller marks
  use the monogram) and Zed (logomark in full black, full white on the dark
  theme, scaled proportionally); monograms for VS Code Insiders (prohibited
  by Microsoft), VSCodium and Cursor (no stated permission). Sources, terms
  and trademark lines are in `web/scripts/third-party-licenses.mjs` and on
  the third-party licences page only; the build fails when a logo file is
  not listed there or its hash changed. The source marker and caveat badge
  never touch a logo or its clear space (VS Code 25% of the icon;
  IntelliJ IDEA half the black square's width around the square, 1/8 of
  the icon width past its edge, so its tile has no border; Zed states
  none): beside a logo they sit in a column next to the tile; monograms
  keep them on the corners (review fix A1).
- JetBrains column after a tab (review fix, rule 14; replaces the ADR 0044
  known gap "columns differ where tabs are expanded"): IntelliJ's
  column expands a tab to the IDE's tab size, which the server does not
  know. `config.Preset` gains `LineTemplate` (jetbrains:
  `idea://open?file={abs_path}&line={line}`) and `config.LineTemplate`
  finds it by the link template. When the current line has a tab before
  the node's start column, the presenter fills the line-only template:
  `exact: false`, new `line_only: true` (omitted when false, so every
  other link and golden is unchanged), `column` the configured base.
  Verified from JetBrains/intellij-community: `CommandLineProcessor.kt`
  `processInternalProtocol` reads a missing `column` as -1 and
  `PlatformProjectOpenProcessor.kt` `navigateToFileFromCommandLine` opens
  `OpenFileDescriptor(project, file, line - 1, column.coerceAtLeast(0))`,
  the line's start. Options considered: line-only (chosen); keep the
  code-point column with `exact: false` and a caveat (a column the server
  knows is wrong whenever the tab size is not 1); assume a tab size of 4
  (a guess). The CLI query output follows, as it shares the presenter.

## Consequences

- Six endpoints gain a parameter and two `ide` fields; one endpoint is
  new. The existing goldens change only by `ide.editor` and `ide.source`.
- No store, contract, schema, extractor, CLI or npm dependency change. The
  CLI keeps using the repository's `ide` setting.
- The choice is per browser profile and per server origin (localStorage),
  so two ports keep separate choices and one port serving different
  repositories shares it.
- Adding a preset is one row in `internal/config/ide.go` (status and note
  included); the picker lists it without a UI change (it shows a monogram
  until a logo is reviewed).

## Known gaps

- Bundling the logos in a distributed product is not confirmed: Microsoft's
  brand page does not address bundling (contact vscode@microsoft.com), and
  JetBrains asks commercial products for explicit permission. Confirm both
  before any public distribution.
- The IntelliJ IDEA icon was taken from JetBrains' own repository
  (github.com/JetBrains/logos, `web/intellij-idea/intellij-idea.svg`)
  because the brand page download host (resources.jetbrains.com) was
  blocked by the environment's proxy; byte identity with the brand page file
  is not verified.
- The Zed logomark files are the brand page's PNG downloads scaled from
  1012 to 128 px; the licence of the download files is not stated.
- Cursor logo (T016k2, developer decision 2026-11-16): Cursor now shows
  the official 2D cube from the cursor.com/brand asset package, unaltered
  (`CUBE_2D_LIGHT.svg` on the light theme, `CUBE_2D_DARK.svg` on the dark
  theme), in the picker and the "Open in" buttons, replacing the monogram
  named in the Decision above. The brand page and the package grant no
  explicit permission and state no prohibition, clear space or minimum
  size; the page says only "Resources to represent Cursor consistently and
  accurately." Bundling is no more confirmed than for the other logos.
  Record: the T016k2 task record (internal).
- When `/api/v0/ide` fails, the UI sends no editor and cannot show preset
  caveats on the buttons (the repository default's label is still the
  server's); a stored `off` still holds. With the list failed the picker
  offers no options, so leaving `off` needs Retry.
- A jetbrains link after a tab, or on a file that changed since indexing,
  opens the line, not the node's column (Review fixes R2). Only the start
  column is checked (the preset's template has no end column).
- Cursor links are never `exact` until a Cursor build is clicked or its
  handler is documented (Review fixes R2).
- `column_unit` or `column_base` overrides on a preset template other than
  jetbrains' and cursor's are trusted as the user's statement about their
  editor; the server does not check them against the editor.
- A user-level CLI editor setting remains a later option.

## Review fixes R2

Review correction (2026-11-04, T016k), second review.

Principle: a link claims `exact: true` only when every step of the column
conversion is proven for that editor; otherwise it is line-only (when the
preset has a line template) or `exact: false` with a caveat.

- **JetBrains column unit (A1, wrong link).** The preset counted code
  points, after IntelliJ's `LogicalPosition` javadoc (ADR 0044 review
  correction). IntelliJ navigates by offset: `OpenFileDescriptor` builds
  its range marker through `LazyRangeMarkerFactoryImpl.createRangeMarker`,
  which calls `DocumentUtil.calculateOffset(document, line, column,
  tabSize)` (platform/core-impl; walks the line with `charAt`, one UTF-16
  unit per column, a tab adds `tabSize`), and `navigateInEditor` moves the
  caret to that offset whenever it differs from the logical position's
  (JetBrains/intellij-community master, fetched 2026-11-04). The unit is
  now `utf16`; tabs stay line-only. "😀 = foo" with the node at `foo`
  (byte 7) is `column=5`, not 4.
- **Byte order mark (A2, wrong link, pre-existing).** Contract columns
  count the mark's three bytes on line 1 (docs/contract/v1.md); VS Code,
  IntelliJ IDEA and Zed drop it from the document text. `convert` drops it
  first on line 1, as the source endpoint's highlight does; a column inside
  the mark is not exact. This closes the ADR 0044 known gap (see its
  amendment).
- **Changed file, JetBrains (C1).** An inexact jetbrains link named the
  stored byte column, which is not IntelliJ's unit. Whenever a link is not
  exact and the preset has a line template, the link names the line only.
- **Cursor (C2).** Cursor's column unit is only assumed from VS Code, and
  no Cursor documentation or source proves it (cursor.com documents only
  `cursor://anysphere.cursor-deeplink/...`; its source is not public).
  `config.Preset` gains `ColumnCaveat` and `config.ColumnCaveat(template)`
  (keyed on the template, like `LineTemplate`); a cursor link converted
  from unchanged source is `exact: false` with the UTF-16 column VS Code
  would use.
- **Link field `caveat`.** `query.IDELink` gains `caveat` (omitted when
  empty): `tab_size_unknown` (line-only after a tab) or
  `column_unit_assumed` (Cursor). An inexact link without a caveat comes
  from a changed file or an unverifiable position, as before. The web UI
  names the reason: "opens the line only: a tab precedes the node ...",
  "column unit assumed from VS Code", "opens the line only: the file
  changed since indexing; the line may have moved", and "the column may be
  off" for a caveat it does not know. Options considered for the UI
  reason: derive it in the client from the preset (cannot tell a tab from
  a changed file) or a server reason (chosen).
- Goldens: `ide-builtin.golden.json` changes only the cursor and jetbrains
  `note` strings. No other golden has a jetbrains, cursor or BOM link.

## Review notes (2026-11-04)

Delegated review, 2026-11-04. Records: the T016k task records (internal).

- **Results** (gates on the stacked head of 2026-11-04, the T016k task record (internal)
  "Gates (2026-11-04, stacked head)"): full suite with a scratch Postgres
  17.11 exit 0, 1,288 PASS lines, 98 `/postgres` PASS incl.
  `TestSchemaEditorParameter/postgres`, only the four helper skips plus
  `TestSourceStates/{sqlite,postgres}/unreadable`; web checks ok (650
  tests, no static drift); `smoke-serve.sh` with the branch binary ok
  (`/api/v0/ide` and an `editor=zed` link). Ten corpora body
  byte-identical; the private corpus not run. Langflow timing fresh +0.6%, warm
  +0.7%; store size identical. Playwright: the picker opens from the rail
  (8 rows) and the sidebar, light and dark, 0 console errors, 0 CSP
  violations. R2 column re-check through `/api/v0/source?editor=` for all
  six presets and `/schema/table` on axum: emoji, BOM, tab, CRLF and
  changed-file cases as decided (verdict correct). CI: [ci windows] batch
  run on dd42753 green.
- **Revisions.** R1: brand terms (an official logo keeps its clear space:
  marker and badge in a column beside the tile; IntelliJ IDEA's tile has
  no visible border; 704 marks measured, 0 overlaps), licence notes
  (JetBrains logos LICENSE.txt, Zed trademark wording), keyboard-reachable
  status icons, a stored `off` holding while `/api/v0/ide` loads or
  fails, and JetBrains links after a tab naming the line only
  (`LineTemplate`, verified from intellij-community). R2 (principle: a
  link is `exact` only when every conversion step is proven for that
  editor): the JetBrains column unit is UTF-16 (`DocumentUtil.
  calculateOffset`), any inexact JetBrains link is line-only, the byte
  order mark is dropped on line 1 before converting (closes the ADR 0044
  known gap, see its amendment), Cursor links are `exact: false` with
  `caveat: column_unit_assumed`, and links gain the `caveat` field. The
  schema endpoints that present nodes (`/schema/table`,
  `/schema/migration`) take `editor=` like the other node endpoints.
- **Known gaps** (above, plus the summary's follow-ups): before any public
  distribution, confirm logo bundling with Microsoft and obtain JetBrains'
  explicit permission for a commercial product (pre-release item), and
  re-fetch the IntelliJ icon from the brand page to compare; Zed download
  licence unstated; with `/api/v0/ide` failed no preset caveats and no
  picker options (Retry first); Cursor stays inexact until a Cursor click
  or documentation proves its unit; other presets' `column_unit` and
  `column_base` overrides are trusted; Windows Zed and Cursor clicks to
  update statuses; `vscodium-insiders` preset; a user-level CLI editor
  setting. Developer decision K6 decided as option a: IDE buttons keep
  their text labels with the mark, as the task body says.

## Revision 1: first-click chooser, short notes and per-link status (2026-11-24)

**Status:** Accepted (delegated review, 2026-11-25)

**Context.** A first-time user's editor links opened VS Code (the built-in default) with no hint where to change it; the editor picker had moved from the sidebar into Settings; presets with caveats showed long notes inline; project-caller links carried a fixed editor status, so a browser choice could read as ignored.

**Decision.**
- While neither this browser nor the repository (`ide.editor` or `ide.link_template`) has chosen an editor, the first activation of any editor link, Jump-to included, opens a chooser beside it (presets, "Remember for this browser", "More in Settings") instead of navigating; picking opens that link in the chosen editor and, if remembered, stores the browser choice under the existing key. Once a choice exists, links open directly. One shared component renders every editor link; a test enforces it.
- Every editor link's tooltip says which editor opens it and that it can be changed in Settings. Settings shows the editor in its own first section, "Editor", with stable anchors.
- `GET /api/v0/ide` gains `short_note` per preset (one short line, from the Go preset table, the single source of preset wording); the full `note` opens from a status icon (amber partly verified, grey limited) that never selects the preset.
- Project-caller links carry `ide_status` per link (the member repository's own configuration and the request's `editor=`), replacing a fixed status.

**Consequences.** Contract-visible additions only (`short_note`, `ide_status`; goldens ide-builtin and project-users, MCP `omitted` entries); no link is built in the browser (the server still builds every link; after a pick the view asks again with `editor=`). Without "Remember", the choice lasts until the page reloads.

**Known gaps.** The Changes head link never shows the chooser (the change endpoints take no `editor=`); at narrow widths the note popover can cover the row's name while open.

## Review notes (delegated review, 2026-11-25)

Revision 1 accepted after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS)): first-click chooser through one shared EditorLink (enforcing test, Jump-to included), short_note and status icons, ide_status per project link. Developer-requested; verified by tests and screenshots.

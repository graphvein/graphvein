# ADR 0088 - Settings editing: one guarded edit of graphvein.yaml with value origins

**Status:** Accepted (delegated review, 2026-11-25)

## Context

The web UI gets a Settings page (T039): the commonly changed
`graphvein.yaml` keys as a form, the whole file one click away, every value
with its origin, and a re-index prompt for keys that only an index run
reads. `graphvein.yaml` stays the source of truth: the page never keeps a
second copy of a setting.

Three guarded edits of configuration files already exist, each with the
same pattern (ADR 0075's manifest edit, ADR 0081's explorer path values
and try-it environments): typed operations, a unified-diff preview, the
hash of the file the edit was based on, the hash of the previewed result,
a per-process session token, a loopback peer, `http.CrossOriginProtection`
and an atomic write. They re-encode the file through yaml.v3, which keeps
comments attached to nodes but re-indents the whole file with two spaces
and drops blank lines, so a one-value edit of a hand-formatted file shows a
diff of every line.

Constraints from the developer's decisions (2026-11-19/20, 2026-11-24):
comments and keys the form does not show are preserved; an invalid file is
never written; each value says where it comes from (built-in default,
repository file, this browser; the browser level stays in the UI); keys
that need a re-index say so; in project mode each member's own file plus
the manifest's try-it environments; ignore patterns are shown read-only
from `.graphveinignore` (no new key); `ui.impact_depth` and
`ui.chain_depth` appear only once T042 adds them.

## Options considered

1. **A new `GET`/`POST /api/v0/config` with typed operations on a fixed key
   table, plus a whole-text form** (design D1 (a), D2 (a)). One endpoint, one
   key table, one claim contract; the existing endpoints keep their own
   semantics.
2. **Extend each existing endpoint per section.** No single place says
   which keys the form edits; origins and re-index needs would be spread
   over several answers.
3. **Let the UI parse YAML** for origins and edits. Two parsers that can
   disagree with the strict decoder; rejected (design D4).

For the write itself:

- (a) re-encode the yaml.v3 node tree (what the other edits do);
- (b) splice text only, without a reference;
- (c) make the edit on the node tree as the reference, splice the text
  line-wise, and keep the splice only when it decodes to exactly the
  reference tree; otherwise fall back to (a).

For "needs a re-index":

- a fixed table per key (design D5 (a));
- also a config hash stored with every index run, so "written, not in the
  index yet" is known after a restart (D5's addition; a store schema
  change).

For what that stored hash covers:

- (i) the bytes of `graphvein.yaml` the run read. Simple, but a comment,
  a blank line, a re-indent or an at-once key (`ui.labels`) would report
  the index as built with other settings, and no changed key could be
  named;
- (ii) a canonical form of the effective values of the keys the run reads
  (the index inputs), recorded with the values so the changed keys can be
  named.

For where it is stored:

- in the run's JSON payload (no migration; an older binary would read a
  newer row without the field silently);
- in a column of its own on `index_runs` (migration 6, both backends; a
  run without it is NULL, which reads as unknown).

## Decision

Option 1 with write strategy (c), the fixed per-key table, and a record
of the index inputs (ii) in a column of its own on every index run.

- **Key table.** `settingKeys` in `internal/config/settings.go` lists every
  key the form edits with its kind (enum, integer, boolean, list, map),
  choices or range, built-in default, when a change applies (`at_once` or
  `next_index_run`), whether the section may be a scalar shorthand
  (`ide: cursor`) and a key that locks it (`ide.link_template` locks
  `ide.editor`). Thirteen keys today (rows ST-1..ST-11, ST-31 and ST-32
  of [the settings claim contract](../contract/claims/settings.md)):
  `ui.labels`, `ide.editor`, `index.shape_depth`, `clients.base_paths`,
  `clients.link_generated_output`, `tests.include`, `tests.exclude`,
  `openapi.include`, `openapi.exclude`, `openapi.base_paths`,
  `architecture.hide`, `ui.impact_depth`, `ui.chain_depth` (T042: each
  was one table row and one claim row). The table decides only which keys have a control and their
  JSON type; which values are valid is decided by the strict decoder
  (`config.Parse`) alone.
- **GET** answers per repository (`repository=` in project mode, through
  the existing member dispatch): each key with value, default, origin
  (`file` with line, `builtin`, `not_set`; `unknown` when the file does not
  load or the member has no checkout) and `applies`; the raw text, whether
  the file exists and its hash; `other_keys` (keys outside the form, with
  line, and for `explorer.path_values` and `tryit.environments` the guarded
  edit that edits them); the root `.graphveinignore` read-only; `reindex`
  (the existing `POST /api/v0/index/run`, scope repository or project);
  in project mode the manifest's try-it environments read-only, edited
  through `POST /api/v0/tryit/environments` target `project`.
- **POST** takes `operations` (`set` with a JSON value of the key's kind,
  `remove`) or `text` (the whole file), previews, and applies the preview's
  `result_hash` atomically. Guards are the path value edit's; the edit
  shares its lock, and the try-it environment edit of a repository's
  `graphvein.yaml` now takes that lock too, so no two edits of one
  `graphvein.yaml` interleave (before, a path value edit and a try-it edit
  could: each checked the base hash under its own lock). An invalid result is previewed with the decoder's message
  and never written. Typed edits of a file that does not load are refused;
  the whole-text edit can fix it. The answer names the form keys whose
  effective value changes (with `applies`), whether anything outside the
  form changes, and `reindex_needed`.
- **Minimal edit (c).** Only the edited entry's lines change: a value on
  its key's line is replaced in place with its line comment; a new key
  goes after its section's last entry, a new section after the last root
  entry (with a blank line when the file separates sections with one); new
  lines take the file's line ending and indentation step; a removed entry
  takes its comment lines and the comment lines directly above it (as the
  node tree drops its head comment), and an emptied section goes too. A
  block list or mapping value is edited entry by entry (revision after
  the Settings UI review): entries are matched (a mapping by key, which
  must keep its order; a list by the longest common subsequence of its
  items), a kept entry keeps all its lines, a mapping entry whose value
  alone changes is rewritten in place with its line comment, an added
  entry takes its neighbours' indentation, and a removed entry takes its
  own lines and the comment lines directly above it. The splice is kept
  only when it decodes to the reference tree (same kinds, resolved tags,
  values and key order); a reordered mapping, a changed entry whose lines
  hold a comment, or a flow-style section falls back to re-encoding, which
  the answer reports as `reformatted: true`; the reference tree reuses the
  nodes of kept entries, so their comments survive the re-encode.
- **Lost comments, never "kept".** Every preview (typed or whole text)
  lists in `lost_comments` the comments of the file on disk that the
  result no longer has, from comparing the comments of both texts (the
  comments yaml.v3's parser attaches to the node trees, so a quote inside
  a plain scalar, a byte order mark and a # in a block scalar are read as
  YAML reads them; a line scanner only for a text that does not parse;
  each comment text of the result accounts for one of the file). Revision
  after the T039 review: the earlier line scanner opened a quoted scalar
  at an apostrophe inside a plain scalar and missed a comment after a
  byte order mark, so it hid lost comments. The UI states only that list,
  never that comments are kept: the earlier sentence was wrong for an
  edited map entry whose inline comment the re-encode dropped.
- **`serve` with a file that does not load** (revision after the Settings
  UI review). No earlier decision covered it: `serve` called the strict
  `ResolveRepository` and refused to start, so the Settings page that can
  fix the file was unreachable. `serve <path>` now starts, prints the
  decoder's message, and the UI shows every view but Settings and the
  project overview as "the configuration does not load" with the file and
  the message. The identity is the override, else the file's own
  `repository` key when it can be read (the YAML parses, or exactly one
  root line sets it and parses alone), else the directory name when the
  file sets no key; a key that is there but cannot be read is refused
  with "give it with --repository" (no guessed identity). Index runs keep
  the strict refusal.
- The existing path value and try-it environment edits are unchanged:
  their operations carry semantics the generic table does not (route
  registration hashes, certificate pins per origin, project base URLs).
- `.graphveinignore` is not written (design D3 (c)).
- **Settings an index was built with** (rows ST-41..ST-46). The index
  inputs are exactly the key table's `next_index_run` rows
  (`config.IndexInputKeys`): `index.shape_depth`, `clients.base_paths`,
  `clients.link_generated_output`, `tests.include`, `tests.exclude`,
  `openapi.include`, `openapi.exclude`, `openapi.base_paths`. Each run of
  the built-in indexer reads them from the same bytes it decodes its
  configuration from, with the built-in default for an absent key and the
  `--shape-depth` flag's value when given (every value decoded with
  yaml.v3 as the strict decoder decodes it, never converted again from
  the raw text: before the T039 review `shape_depth: 0x7` read as 0 on
  the current side, so freshness said "settings changed" after every
  re-index), and stores
  `{"format": 1, "hash": "sha256:…", "values": {…}}` in
  `index_runs.settings` (migration 6, SQLite and Postgres). Values are
  canonical: a pattern list is a sorted set (the index applies "any
  pattern matches"), a base-path map a keyed object (the index reads a Go
  map), an absent list or map is empty. Option (ii) is chosen because (i)
  flags comment-only and layout edits and at-once keys as stale and cannot
  name a key; the column because a NULL is an unambiguous "not recorded"
  and the schema version makes an older binary refuse the store instead
  of misreading it. `freshness.settings` of `GET /api/v0/status` (and of
  each project member), `index_settings` of `GET /api/v0/config` and a
  line of `graphvein status` compare the last run's record with the
  current file: `unchanged`, `changed` with the keys in table order,
  `unknown` with a reason (no record, another format, the file does not
  load, no checkout, a key the record lacks), or `not_indexed`. The file
  freshness (`freshness.state`) is not changed by it. `graphvein status
  --project` adds each member's comparison to its row, and the MCP
  `status` tool reports it with `changed` and `unknown` as unknowns
  (ST-47, MC-33). The Settings page and the UI's freshness badge and
  member dots show `changed` with the keys; `unknown` is never shown as
  unchanged (ST-48).

## Consequences

- One place lists the editable keys; a test keeps the key table, the row
  list and the claim table 1:1, and checks that every row names a test.
- A form edit of a hand-formatted file shows a one- or two-line diff; when
  that cannot be proven, the user sees the whole-file re-indent before
  confirming.
- The UI shows "applies at the next index run" from the table and offers
  the existing re-index; after a restart, freshness says whether the index
  was built with the current settings and which keys a re-index would
  apply.
- The store schema goes to version 6: a store indexed by an older version
  is migrated by the next `graphvein index` (or server re-index), and the
  query commands and `serve` refuse it until then, as for earlier
  migrations. Its old runs have no record and report `unknown`.
- `docs/config.md` lists `tryit` among the root keys (it was missing).

## Known gaps

- The workspace server (T038) resolves `serve <path>` with
  `ResolveServeRepository`, and opens a registered entry whose file does
  not load the same way (`ResolveRegisteredRepository`: the file's
  repository key when it can be read, else the registered name), so its
  views are gated as well. Its check of a new folder still refuses a file
  that does not load (WS-33): nothing is registered under a guessed name.
- A comment's line is found in the text after the parser has read it; two
  comments with the same text may swap lines in `lost_comments`, never
  hide a loss. A text that does not parse is compared with a line
  scanner (so a broken result's comments are approximate; it cannot be
  written anyway).
- While the file does not load the views are gated in the UI only; the
  API endpoints still answer from the stored index (built with an earlier
  file) or refuse where they read the file.

- Runs from an external indexer or `graphvein ingest` record no settings
  (they are not Graphvein's index inputs): their settings are `unknown`.
- `changes.*` (journal limits) is not an index input: it bounds the
  change journal, not the facts, so changing it never reports the index
  as stale.
- A key added to the index inputs later is `unknown` for runs recorded
  before it (`unknown_keys`), until the next run.
- `freshness.state` stays the files' state: a client that reads only it
  sees `fresh` while `settings.state` is `changed`. Folding the settings
  into the state would change every consumer (MCP, queries, the change
  endpoints) and is left to a decision.
- `other_keys_changed` is computed only when both texts load; for a fixed
  broken file every form key is listed as changed (built-in defaults
  included), and the change list says "the file did not load before;
  every key is treated as changed".
- `ide.editor` cannot be edited while `ide.link_template` is set; the YAML
  view can.
- Only the root `.graphveinignore` is shown (the only one the scanner
  reads); nested `.gitignore` files are not.

## Review notes (delegated review, 2026-11-25, T039)

Accepted after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS)). One independent review (R-1..R-8: decoder-faithful values, comments from the parser, the link template row, editor info refresh) fixed with failing-first tests; integration added the depth keys ST-31/32 and the Editor section. graphvein.yaml stays the source of truth; every write goes through the previewed guarded edit. Known gaps as recorded above (keys outside the form compared by raw text, on the safe side).

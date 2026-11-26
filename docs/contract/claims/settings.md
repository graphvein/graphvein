# Claim contract: Settings (graphvein.yaml keys, origins, guarded edit)

**Status:** in force with ADR 0088 (accepted, delegated review, 2026-11-25). Rows `ST-1`..`ST-49` are
reserved for this contract; `ST-1`..`ST-30` are the server's (task T039,
recorded in the T039 task record (internal)). `ST-31` and `ST-32` are the
keys `ui.impact_depth` and `ui.chain_depth` (T042); `ST-33`..`ST-40` stay
free for the Settings UI. `ST-41`..`ST-46` are the settings an index run was built
with (T039); `ST-47`..`ST-49` are the project status
and the Settings UI (T039).

The code tables are `settingKeys` in `internal/config/settings.go` (one
entry per key row, in this order) and `settingsRows` in
`internal/server/settings_rows.go` (every row, naming its test), and
`IndexInputKeys` in `internal/config/index_inputs.go` (the index inputs:
exactly the `next_index_run` key rows);
`TestSettingsContractTable` keeps the three 1:1. The endpoint is
documented in [serve](../../cli/serve.md#get-and-post-apiv0config), the
keys in [configuration](../../config.md).

## Keys the form edits

Every key of `graphvein.yaml` the Settings form shows with a typed
control has exactly one row. A key not listed here is never edited by a
typed operation: the request is refused with 400 `unknown_key` (the
unknown default branch, `D`), and the file keeps it as it is. When a
change applies: `at_once` keys are read from the file on every request
of the endpoints that use them; `next_index_run` keys are read by the
index run, which re-links over the fact cache without re-parsing
unchanged files.

| Row | Key | Kind (JSON value of a `set`) | Built-in default | Applies |
|---|---|---|---|---|
| ST-1 | `ui.labels` | enum `plain`, `technical` (string) | `plain` | at_once (`GET /api/v0/ui`) |
| ST-2 | `ide.editor` | enum of the editor presets (string); `ide: <name>` is the same key and is edited in place; not editable while `ide.link_template` is set (the template builds the links) | `vscode` | at_once (`GET /api/v0/ide`, IDE links) |
| ST-3 | `index.shape_depth` | integer 1 to 10 (number without fraction) | `6` | next_index_run |
| ST-4 | `clients.base_paths` | map: ordered list of `{key, value}` strings | not set | next_index_run |
| ST-5 | `clients.link_generated_output` | boolean | `false` | next_index_run |
| ST-6 | `tests.include` | list of strings | not set | next_index_run |
| ST-7 | `tests.exclude` | list of strings | not set | next_index_run |
| ST-8 | `openapi.include` | list of strings | not set | next_index_run |
| ST-9 | `openapi.exclude` | list of strings | not set | next_index_run |
| ST-10 | `openapi.base_paths` | map: ordered list of `{key, value}` strings | not set | next_index_run |
| ST-11 | `architecture.hide` | list of strings | not set | at_once (architecture endpoints) |
| ST-31 | `ui.impact_depth` | integer 1 to 10 (number without fraction); the depth the Impact tab's change impact starts with; this browser's choice (Settings or the tab's depth switch) wins in the UI | `3` | at_once (`GET /api/v0/ui`) |
| ST-32 | `ui.chain_depth` | integer 1 to 10 (number without fraction); the depth the architecture evidence chain starts with; this browser's choice (Settings or the chain's depth switch) wins in the UI | `6` | at_once (`GET /api/v0/ui`) |

Which values a key accepts beyond its JSON type (a choice, a range, a
pattern) is decided only by the strict decoder of `graphvein.yaml`
(`config.Parse`), never by a second rule set.

## What GET /api/v0/config states

| Row | Claim | Rule | Refusal or absence |
|---|---|---|---|
| ST-12 | Origin from the file | a key the file sets has `origin: file`, the line of its key (the `ide` line for the shorthand) and the value as the strict decoder reads it (`shape_depth: 0x7` is 7, `010` is 8, `1_0` is 10); `ide.editor` has `locked_by` (`ide.link_template` with its line) while the template is set | - |
| ST-13 | Origin built-in | a key the file does not set that has a built-in default has `origin: builtin` and that default as its value | - |
| ST-14 | Origin not set | a key the file does not set and that has no default has `origin: not_set` and `value: null`: nothing is applied | - |
| ST-15 | File that does not load | `config_error` is the strict decoder's message (file and line, the line named once); `graphvein serve` starts on such a file (an index run refuses it) and the web UI shows every view but Settings and the project overview as "the configuration does not load" with the file and that message; every key has `origin: unknown`, `value: null`, `editable: false`; `other_keys` is empty; the raw text and hash are still served | typed edits: 409 `invalid_config`; a whole-text edit (ST-28) is accepted |
| ST-16 | Keys outside the form | `other_keys` lists every scalar root key and every key inside a mapping section that no key row covers, in file order with its line; `edited_by` names the guarded edit of `explorer.path_values` (`/api/v0/explorer/path-values`) and `tryit.environments` (`/api/v0/tryit/environments`), else null. Typed edits never change them | - |
| ST-17 | At once | for an `at_once` key the next request of the endpoint that uses it reads the written file; no re-index is offered | - |
| ST-18 | Next index run | for a `next_index_run` key the edit's answer has `reindex_needed: true`; `reindex` names `POST /api/v0/index/run` with scope `repository`, or `project` under `serve --project` (every member, then the link pass) | - |
| ST-19 | Ignore file | the root `.graphveinignore` (the only one the scanner reads) is served as text with `exists`, `editable: false`; Settings never writes it | - |
| ST-20 | Project environments | in project mode `project` lists the manifest's `tryit.environments` (name, label, line, every member's base URL, `for_member`) read fresh, or the manifest's load `error`; `edited_by` is `/api/v0/tryit/environments` (target `project`) | - |
| ST-21 | Member without a checkout | `editable: false` with the reason; `exists`, `hash`, `text` and the ignore file are null; every key has `origin: unknown`, `value: null` | POST: 409 `no_checkout` |

## What POST /api/v0/config does

| Row | Claim | Rule | Refusal |
|---|---|---|---|
| ST-22 | Guards | the checks of every state-changing POST (Host allow-list, `Sec-Fetch-Site`, `Origin`, `http.CrossOriginProtection`) plus a loopback peer, this handler's session token in `X-Graphvein-Token` (from GET), `Content-Type: application/json`, one JSON object with only `base_hash`, `operations` or `text`, `apply`, `result_hash` and no duplicate member, at most 1 MiB, `text` at most 256 KiB | 403 `host_not_allowed`, `cross_site_request`, `not_loopback`, `token_required`; 415 `unsupported_media_type`; 400 `bad_request`; 413 `too_large`; another method: 405 with `Allow: GET, HEAD, POST` |
| ST-23 | Concurrent edit | `base_hash` must be the hash of the file on disk when the edit is made (`absent` for no file) | 409 `config_changed`, nothing written |
| ST-24 | Preview | without `apply` the answer has `diff` (unified), `changes` in words (a scalar with the value it replaces; a list or mapping item by item, `adds`/`changes`/`removes` with `key: value` items, never JSON), `lost_comments`, `valid` and the decoder's `problems`, `result_hash`, `changed_keys` (form keys whose effective value changes, with `applies`, each value as the strict decoder reads it), `other_keys_changed`, `reindex_needed`, `reformatted`; nothing is written | - |
| ST-25 | Apply | `apply: true` with the preview's `result_hash` writes exactly that text atomically (temporary file and rename) | 409 `preview_changed`; 422 `invalid_config` (does not load); 400 `no_change` |
| ST-26 | Typed operation | `{op: set, key, value}` or `{op: remove, key}`; `key` must have a key row and be editable; `value` must have the JSON type of the key's kind | 400 `unknown_key`, `invalid_value`, `bad_request`; 400 `edit_refused` (a locked key, removing a key the file does not set) |
| ST-27 | Minimal edit | a typed edit changes only the lines of the edited entry: a value on its key's line is replaced in place with its line comment; a block list or mapping is edited entry by entry (a kept entry keeps all its lines and comments, a mapping entry whose value alone changes keeps its line comment, a removed entry takes its own lines and the comment lines directly above it); new lines take the file's line ending and indentation step; the result is kept only when it decodes to exactly the node-tree edit. Otherwise (a reordered mapping, a comment inside the lines a changed entry would replace, a flow section) the node tree is encoded with two-space indentation, reusing the nodes of kept entries, `reformatted: true` and a change line saying so, plus one for a byte order mark it drops, CRLF line endings it turns into LF and each anchor it drops. In both cases `lost_comments` lists every comment of the file on disk the result no longer has (the comments yaml.v3's parser attaches to both texts' node trees compared; a line scanner's for a text that does not parse), and the UI never says comments are kept: with no lost comment it says nothing about comments | - |
| ST-28 | Whole text | `text` replaces the file; it is validated, previewed and applied like a typed edit; for a file that did not load every form key is in `changed_keys` and `changes` says "the file did not load before; every key is treated as changed" | invalid YAML or an invalid key: `valid: false` with the decoder's message, apply 422 `invalid_config` |
| ST-29 | Remove | `remove` deletes the key's lines (with its comments and the comment lines directly above it); a section it empties goes too; the built-in default (or nothing) applies again | - |
| ST-30 | Serialised | an edit's read, base check and write run under the lock of the explorer's path value edit, which the try-it environment edit of a repository file also takes, so two edits of one `graphvein.yaml` never interleave | the later one: 409 `config_changed` |

## Settings an index was built with

An index run of the built-in indexer records the effective values of the
keys it reads (the index inputs), so freshness can say whether
`graphvein.yaml` changed since that index in a way a re-index would apply.

| Row | Claim | Rule | Refusal or absence |
|---|---|---|---|
| ST-41 | Recorded with the run | every run of the built-in indexer (`graphvein index`, the server's re-index, a project member's index, a ref snapshot) stores, with its `index_runs` row (column `settings`, schema 6, SQLite and Postgres), the index inputs it read: every `next_index_run` key row (ST-3..ST-10) with its effective value (the built-in default when the file does not set it; the `--shape-depth` flag's value when given), the format and the SHA-256 hash of the canonical values; read from the same bytes the run decoded | a run from an external indexer or `graphvein ingest` records nothing: its settings are unknown (ST-44) |
| ST-42 | Canonical values | lists are compared as sorted sets without duplicates and maps as keyed objects (the index reads patterns as "any match" and base paths as a map); comments, blank lines, quoting, indentation, flow or block style, entry order and writing a default explicitly change nothing, so none of them makes an index stale | - |
| ST-43 | Changed | when a recorded key's value differs from the current file's, `settings.state` is `changed` and `changed_keys` names every such key in key-table order, with `index_hash` and `current_hash`; a re-index applies them | - |
| ST-44 | Unknown, never unchanged | `settings.state` is `unknown` with a `reason` when the last run recorded nothing (an index from before schema 6, an external indexer), the record has another format or does not decode, the current file does not load (the decoder's message, file name only) or the member has no checkout; a current key the record lacks is listed in `unknown_keys` (state `unknown` unless another key changed) | - |
| ST-45 | Not index inputs | the `at_once` keys (`ui.labels`, `ide.editor`, `architecture.hide`) and every key outside the key table (`ide.*`, `changes.*`, `explorer.*`, `tryit.*`, `egress.*`, `repository`, `version`) are not recorded and never make an index stale: no index fact depends on them (`changes.*` bounds the change journal, not the facts) | - |
| ST-46 | Where it is reported | `freshness.settings` of `GET /api/v0/status` and of each member of `GET /api/v0/project`, `index_settings` of `GET /api/v0/config`, and one line of `graphvein status`; the file freshness (`freshness.state`, `changed_files`) is not changed by it. `not_indexed` without runs | - |

## Project status and the Settings UI

| Row | Claim | Rule | Refusal or absence |
|---|---|---|---|
| ST-47 | Project status | `graphvein status --project` adds to each indexed member's row with a checkout here `settings unchanged since its last index`, `settings changed since its last index: KEYS (re-index to apply)` or `settings since its last index unknown: REASON` (the comparison of ST-43/ST-44 with the member's own `graphvein.yaml`); the MCP `status` tool reports the same object (its row MC-33) | an indexed member without a checkout: `unknown: no checkout here`; a member not indexed: nothing |
| ST-48 | UI freshness | when `freshness.settings.state` is `changed`, the top-bar badge says so (a fresh index: "Fresh · settings changed" in the warning colour; a stale one adds "settings changed" to its time) and its tooltip names the changed keys and "Re-index to apply"; a project member's dot turns to the warning colour with the same words in its tooltip. `unchanged` is said only in the tooltip | `unknown`, a state the UI does not know or an absent object is never shown as unchanged: the tooltip says the comparison is not known, with the reason |
| ST-49 | Value ladder | the Settings page shows for each key the rows it has: This browser (`ui.labels`, `ide.editor`: the stored choice), Repository (the `GET /api/v0/config` value with its `graphvein.yaml:line`, or not set) and Built-in (the default, or none); the value in use is the first set row in that order and is bold, the others are muted; the This browser row says "not set" when the browser chose nothing; while `locked_by` is set (ide.link_template) the Repository row is that key at its line and neither the editor value nor the built-in default is in use; an edit whose answer changes an `ide.*` key or `other_keys_changed` reads the editor defaults again; a repository value is edited only through the previewed edit (ST-24, ST-25), whose dialog states only what the answer proves (the lost comments, keys outside the form unchanged when `other_keys_changed` is false); `explorer.path_values` and `tryit.environments` link to the Explorer's editors (`#/explorer?edit=path-values`, `#/explorer?edit=environments`) | `origin: unknown` (the file does not load, no checkout) shows no repository value and no value in use beyond the browser's own choice |


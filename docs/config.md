# Project configuration

Graphvein reads `graphvein.yaml` from the root passed to `index`, `status`,
`query`, `callers` or `impact` (default root `.`). It does not search parent
directories or use the current directory when another root was supplied.
A missing file uses defaults.

```yaml
version: 1
repository: graphvein
ide: vscode
```

`version` is required when the file exists and must be integer 1. `repository`
is optional; if supplied it must be a nonempty string without NUL and without
the substring `@ref:`, which is reserved for ref snapshots (see
[Changes](#changes)). These are
supported root keys alongside optional `ide`, `ui`, `clients`, `tests`, `openapi`, `changes`, `architecture`, `index`, `explorer`, `tryit` and `egress`. Repository resolution is
`--repository`, then the
configured repository, then the absolute root's directory basename. Teammates
can share an ID even when their checkout directories have different names.
An explicitly empty or NUL-containing flag, or one containing `@ref:`, is a
usage error, and so is a directory basename containing `@ref:` when it would
become the identifier.

Unknown or duplicate keys, wrong types, malformed YAML, empty files and multiple
YAML documents are errors. Errors name the file and the line when available,
and commands exit 2 before opening storage. A repository flag does not bypass
config validation. `ingest` uses the stream header's repository rather than
resolving a project ID.

Several repositories that work together (a frontend and a backend in
separate repositories) are grouped by a separate project manifest,
`graphvein-project.yaml`; see [graphvein project](cli/project.md). It never
changes a repository's own `graphvein.yaml` or its identities.

The web UI's Settings page (`graphvein serve`) shows the commonly changed
keys with their effective value and origin (this file with its line, the
built-in default, or not set) and edits them, or the whole file, through
a guarded edit with a diff preview that is written only when the result
loads ([serve](cli/serve.md#get-and-post-apiv0config),
[ADR 0088](adr/0088-settings-editing.md)). A form edit changes only the
lines of the edited key, keeping comments, blank lines, indentation and
every other key; when it cannot, the preview says the file will be
re-indented with two spaces. Keys read by the index run
(`index.shape_depth`, `clients`, `tests`, `openapi`) apply at the next
index run; `ui`, `ide` and `architecture` apply at once. Each index run
records the values of the keys it read, so `graphvein status`, the
status API and the Settings page say when they changed since the index
("settings changed since last index", naming the keys) or that it is not
known (an index from an earlier version or an external indexer). Comments
and layout are never a change.

Secrets stay in environment variables. Storage configuration remains
`GRAPHVEIN_SQLITE_PATH` or `GRAPHVEIN_POSTGRES_URL`; it is not supported in YAML.

External indexers receive no variables whose names start with `GRAPHVEIN_`.
See [external invocation](contract/external-indexer.md).

## SQLite store files

The SQLite store is in WAL journal mode
([ADR 0049](adr/0049-sqlite-wal-journal-mode.md)), so `serve`, `status` and
the query commands keep reading the last committed run while
`graphvein index` writes. Next to `index.db` (by default in
the folder's own store folder in the per-user stores folder, or
`<repo>/.graphvein/` for a store kept inside the repository, which the scanner
ignores but Git lists unless an ignore file covers it; ADR 0093) SQLite keeps
two sidecar files:

- While any Graphvein process has the store open: `index.db-wal` and
  `index.db-shm`. During a fresh index of a large repository the `-wal` file
  can grow to about the size of the store.
- After `graphvein index` exits with nothing else open: only `index.db`.
- After `graphvein index` exits while `serve` runs: `index.db-wal` at 0 bytes
  and `index.db-shm`.
- After `serve`, `status` or a query command exits last: `index.db-wal` and
  `index.db-shm` may remain. This is harmless; the next `graphvein index`
  with no other Graphvein process open removes them.
- After a crash: `index.db-wal` may hold the last committed run; the next
  open recovers it.

When `graphvein index` exits while `serve` is inside a read, it waits up to
1 s for the read to end so it can empty `index.db-wal`, logging an Info line
first; if the read is still running, it logs a warning and exits, leaving
the committed run in `index.db-wal` for the next checkpoint.

A third file, `index.db-lock`, is the index lock
([ADR 0052](adr/0052-reindex-endpoint-and-index-lock.md)): an empty SQLite
file that `graphvein index` and a re-index started from `serve` hold an OS
lock on for the whole run, so only one index run uses the store at a time.
It stays after the run, is never written, and its lock goes away when the
holding process exits. Never delete it while any Graphvein process runs:
on Linux and macOS a second run could then lock a new file next to the
running one (the run checks the file before it commits and fails if it was
replaced). A `-journal` file next to it after a crash is removed by the
next run.

### Temporary snapshot

Every index run (`graphvein index`, `index --project`, a re-index or a ref
snapshot started from `serve`, and `changes` indexing a missing ref) first
writes the run's NDJSON snapshot to a temporary file,
`snapshot-<tag>-*.ndjson` (`<tag>` is a short hash of the store's file
name; `snapshot-*.ndjson` with Postgres), and ingests it from there. The
file goes next to the store: the directory of its `index.db` (the
folder's store folder in the per-user stores folder by default,
`<repo>/.graphvein/` for a store inside the repository, which the
scanner ignores, or the directory of `GRAPHVEIN_SQLITE_PATH`), so a run
with the store outside the
repository writes nothing into the indexed tree, not even an empty
`.graphvein/`. With Postgres, or a SQLite path inside the repository but
outside its `.graphvein/`, the file goes to the operating system's
temporary directory instead, so the scan never reads it. The run removes
the file when it ends, successfully or not, and removes a directory it
created for it when that directory is empty. A run that is killed (for
example by the operating system's out-of-memory killer) cannot clean up
and leaves the file behind in that same place (never in the repository
unless the store is in its `.graphvein/`). The next run of the same SQLite
store, which holds the store's index lock, removes it; the snapshots of
other stores in the same directory are left alone. A leftover in the
operating system's temporary directory (Postgres) is never removed by a
run; it is safe to delete once no index run is active.

Never delete `index.db-wal` or `index.db-shm` on their own, and never delete
or replace `index.db` while any Graphvein process has it open. To reset a
store, stop `serve` and delete `index.db`, `index.db-wal` and `index.db-shm`
together. Copying only `index.db` while an `index.db-wal` exists copies an
older snapshot.

Network filesystems (SMB or NFS shares, mapped network drives) are not
supported for the SQLite store: WAL needs every process to share memory on
one host. Use a local disk or Postgres. On a local filesystem that cannot
give SQLite shared memory, `graphvein index` logs a warning and the store
stays in rollback mode, where readers wait up to 3 s for a committing index
([ADR 0043](adr/0043-sqlite-read-busy-timeout.md)).

A process that can read the store but cannot write its directory (a
read-only mount, or a directory owned by another user) can only read a WAL
store while `index.db-shm` already exists, because the reader must create
that file. Whether it exists depends on which Graphvein process closed the
store last: such a reader fails with an error naming the directory as not
writable after `graphvein index` exited alone (no `-shm` left), and works
after `serve` or a query command exited last, as long as it can read the
`-wal` and `-shm` files they left. Make the directory writable, or copy `index.db`
and its `-wal`/`-shm` files to a writable directory.

## Workspace registry

`graphvein serve` lists every index it knows in the web UI's Workspace
sidebar and serves them from one server
([ADR 0087](adr/0087-multi-index-server.md),
[serve](cli/serve.md#workspace-multi-index-server)). The list is a
registry file outside every repository:

- Location: `GRAPHVEIN_WORKSPACE_FILE` when set; otherwise `workspace.yaml`
  in a `graphvein` folder of the user configuration directory:
  `%AppData%\graphvein\workspace.yaml` on Windows,
  `~/Library/Application Support/graphvein/workspace.yaml` on macOS,
  `$XDG_CONFIG_HOME/graphvein/workspace.yaml` (default
  `~/.config/graphvein/workspace.yaml`) elsewhere. The folder is created
  with owner-only permissions on first write.
- Written by `graphvein serve <path>` and `serve --project` (after the
  store opened), and by "Add a folder" and "Remove from the list" in the
  UI. `graphvein index` does not register: indexing a temporary copy or
  a corpus never adds it to the sidebar.
- Contents: `version: 1` and `entries`, each with `kind` (`repository` or
  `project`), `name` (the repository ID or project name), the absolute
  folder `path` with symbolic links resolved, and `added_at`. The store
  location is not stored there: each index is opened where the store
  resolver finds it for its folder, as the CLI does
  ([ADR 0093](adr/0093-per-user-store-location.md)):
  `GRAPHVEIN_POSTGRES_URL` of the `serve` process, else the folder's own
  `.graphvein/index.db` when one exists (an index made inside the folder),
  else a `stores.yaml` entry, else `<stores>/<name>-<8 hex>/index.db` in
  the per-user stores folder. `GRAPHVEIN_SQLITE_PATH` names one file, so it
  applies only to the index named on the `serve` command line; every other
  entry opens from its own store. Under `GRAPHVEIN_POSTGRES_URL` every
  entry shares one database, which keeps facts by repository ID and
  project name: two entries in different folders with one name are not
  served until one is renamed (`repository:` in `graphvein.yaml`) or
  removed. Never a URL or secret.
- Every write replaces the file atomically, under an exclusive lock on
  `workspace.yaml.lock` beside it, so several `serve` processes can add
  and remove entries at once. The lock goes away when its process exits;
  the empty lock file stays and is harmless. A file that does not parse is
  reported (the UI shows the error) and never rewritten; fix or delete it.
- Paths are stored with the file system's own letter case: on Windows and
  a case-insensitive macOS volume, one folder typed in two cases is one
  entry. One folder is one entry: serving it again after its repository or
  project was renamed keeps the stored entry and its id (the new name is
  shown once its index is open).
- An entry whose folder is gone stays listed (as "folder not found") until
  it is removed; removing an entry never deletes the folder or its index.

Scripts and tests that run `serve` should point `GRAPHVEIN_WORKSPACE_FILE`
at a temporary file and `GRAPHVEIN_STORE_DIR` at a temporary folder, so
they leave the user's registry and stores alone.

`stores.yaml`, beside `workspace.yaml`, holds the exceptions to the
default: `default_dir` (the folder new indexes go to, set with Change… on
"New indexes are kept in" in the web UI's Settings, Storage) and
`entries` (a folder whose index is kept elsewhere: chosen with Change… in
the Add a folder confirmation, or moved with Move… in Settings or
`graphvein store move`). Settings' Storage section opens without an index
and shows where each index is, its place and size; Move and Change… are
checked by the server before anything is written
([serve](cli/serve.md#storage-get-apiv0store-post-apiv0storecheck-move-default),
[ADR 0088](adr/0088-settings-editing.md) revision 1). Indexes that exist
never move on their own.

## IDE

IDE links open a node's file at its line and column in an editor. The
repository's `ide` setting is the team default: the CLI always uses it, and
in `graphvein serve` each browser can override it with its own editor choice
(the "Editor for links" control on the Settings page, [serve](cli/serve.md#editor-for-links),
ADR 0050). Pick the editor by name:

```yaml
version: 1
ide: cursor
```

`ide: <editor>` is shorthand for `ide: {editor: <editor>}`. The mapping form
can override any field of the preset:

```yaml
version: 1
ide:
  editor: jetbrains
  label: "Open in IDEA"
  column_base: 0
```

| editor | link_template | column_unit | column_base | label | status |
|---|---|---|---|---|---|
| `vscode` (default) | `vscode://file{abs_url_path}:{line}:{column}` | `utf16` | 1 | Open in VS Code | `verified`: Verified from VS Code's documentation and its URL handler. |
| `vscode-insiders` | `vscode-insiders://file{abs_url_path}:{line}:{column}` | `utf16` | 1 | Open in VS Code Insiders | `verified`: Verified from VS Code's handler, and a link was clicked on Windows. |
| `cursor` | `cursor://file{abs_url_path}:{line}:{column}` | `utf16` | 1 | Open in Cursor | `partly_verified`: cursor://file is confirmed; line, column and the Windows path form are assumed from VS Code, so the column is marked as not exact. |
| `vscodium` | `vscodium://file{abs_url_path}:{line}:{column}` | `utf16` | 1 | Open in VSCodium | `verified`: Verified from VSCodium's build script, which keeps VS Code's handler; not documented or clicked. |
| `jetbrains` | `idea://open?file={abs_path}&line={line}&column={column}` | `utf16` | 0 | Open in IntelliJ IDEA | `limited`: macOS only, IntelliJ IDEA only; where a tab precedes the node on its line or the file changed since indexing, the link opens the line only (the IDE's tab size is not known). |
| `zed` | `zed://file{abs_path}:{line}:{column}` | `codepoint` | 1 | Open in Zed | `verified`: Verified from Zed's source, including Windows; not yet clicked on Windows. |

The status and its note are the `status` and `note` of `GET /api/v0/ide`
(`internal/config/ide.go`); the web UI shows them next to the editor and on
every IDE button of a preset that is not `verified`. Editor lists show only
a short form under the name (`short_note`: "column assumed" for `cursor`,
"macOS only" for `jetbrains`) and the full note from a status icon.

Sources (fetched 2026-11-03, ADR 0044): VS Code's URL documentation
(microsoft/vscode-docs `docs/configure/command-line.md`, "Opening VS Code
with URLs", which also gives the `vscode-insiders://` prefix) and its
protocol handler (microsoft/vscode `src/vs/code/electron-main/app.ts`,
`src/vs/base/common/uri.ts`); VSCodium's build script (`prepare_vscode.sh`,
`urlProtocol` `vscodium`); IntelliJ IDEA's sources
(JetBrains/intellij-community: `MacDistributionCustomizer.kt`,
`IdeaCommunityProperties.kt`, `CommandLineProcessor.kt`,
`PlatformProjectOpenProcessor.kt`, `OpenFileDescriptor.java`,
`LazyRangeMarkerFactoryImpl.java`, `DocumentUtil.java`); Zed's sources (zed-industries/zed:
`crates/zed/src/zed/open_listener.rs`,
`crates/recent_projects/src/remote_connections.rs`, `crates/text/src/text.rs`).

Documentation check (fetched 2026-11-04): VS Code's page
(<https://code.visualstudio.com/docs/configure/command-line>, "Opening VS
Code with URLs") gives `vscode://file/{full path to file}:line:column` with
the example `vscode://file/c:/myProject/package.json:5:10`, and "If you are
using VS Code Insiders builds, the URL prefix is `vscode-insiders://`". Zed's
CLI reference (<https://zed.dev/docs/reference/cli>) documents
`zed myfile.txt:42:10 # Open at line 42, column 10` but not `zed://file`
links; Zed's Windows installer (`crates/zed/resources/windows/zed.iss`)
registers the `zed` URL protocol. IntelliJ IDEA's help
(<https://www.jetbrains.com/help/idea/opening-files-from-command-line.html>)
documents only `idea [--line <number>] [--column <number>] <path>`, not
`idea://` links. Sublime Text's command line page
(<https://www.sublimetext.com/docs/command_line.html>) says "Filenames may
be given a :line or :line:column suffix" and documents no URL scheme.
VSCodium documents no URL format; its build script is the source.

Known limits:

- `cursor` is only partly verified. Cursor's documentation
  (<https://cursor.com/docs/reference/deeplinks>) describes only
  `cursor://anysphere.cursor-deeplink/...` prompt, command and rule links.
  A Cursor staff answer on the forum
  (<https://forum.cursor.com/t/remote-uri-opens-a-new-window-every-time/166093>,
  2026-07-19) confirms the file form: "Local cursor://file/... links aren't
  affected". The `:{line}:{column}` suffix, the `/D:/...` form on Windows and
  the UTF-16, 1-based column are not documented; the preset assumes VS
  Code's handler. Cursor's source is not public. Because the column unit
  is only assumed, a Cursor link is never `exact`: a link converted from
  unchanged source has `exact: false` and `caveat: column_unit_assumed`
  (the web UI says "column unit assumed from VS Code"), with the UTF-16
  column VS Code would use.
- `jetbrains` works only on macOS (IntelliJ registers `idea://` only in its
  macOS bundle) and only for IntelliJ IDEA (PyCharm, WebStorm and the other
  products register their own schemes). Its column is 0-based and counts
  UTF-16 units: `OpenFileDescriptor` takes its offset from
  `DocumentUtil.calculateOffset` (through `LazyRangeMarkerFactoryImpl`),
  which walks the line one `char` (UTF-16 unit) per column, and
  `navigateInEditor` moves the caret to that offset, so an emoji counts
  two. A tab adds the IDE's tab size, which the server does not know, so
  on a line with a tab before the node the link names the line only
  (`idea://open?file=...&line=N`, no `column`): `exact: false`,
  `line_only: true`, `caveat: tab_size_unknown`, `column` the configured
  base. A file that changed since indexing also gets the line-only link
  (no caveat), because its stored byte column is not IntelliJ's column.
  IntelliJ reads a missing column as -1 and opens the line's start
  (`CommandLineProcessor.kt` `processInternalProtocol`,
  `PlatformProjectOpenProcessor.kt` `navigateToFileFromCommandLine`:
  `column.coerceAtLeast(0)`). This follows the template: a `link_template`
  equal to the preset's behaves the same, and `column_unit` or
  `column_base` overrides do not turn it off.
  JetBrains Toolbox links (`jetbrains://`) need the IDE project name, which
  Graphvein does not know, so there is no preset for them.
- `zed` counts columns in characters (code points); Zed's documentation
  does not name the unit. On Windows the link reads `zed://fileC:/src/a.ts:...`:
  Zed's handler strips `zed://file` and URL-decodes the rest, so this yields
  `C:/src/a.ts`, while a `zed://file/D:/...` form would yield `/D:/...`,
  which is not a Windows path. The link has not been tried on a Windows
  machine.
- Sublime Text registers no URL scheme of its own; `subl://` links need a
  third-party handler, whose format differs between handlers, so there is no
  preset. With such a handler installed, set its template and a label:

```yaml
version: 1
ide:
  link_template: "subl://open?url=file://{abs_path}&line={line}&column={column}"
  label: "Open in Sublime Text"
```

Keys of the mapping: `editor` (a preset name), `label`, `link_template`,
`column_unit` and `column_base`. Each explicit key overrides the preset's
value, and the preset (default `vscode`) supplies the rest. An explicit
`link_template` replaces the preset's template and also drops the preset's
label, because the template may open something else; only an explicit
`label` is then used. `label` must be a nonempty string of at most 60
characters without control characters. An unknown editor is an error such
as `unknown ide editor "sublime" (want vscode, vscode-insiders, cursor,
vscodium, jetbrains or zed)`. There is no `ide: none`: `none` is an unknown
editor like any other, so the whole file fails to load. To hide IDE links
for yourself, choose "No editor links" in the web UI's editor picker. Unknown or duplicate keys, aliases, wrong
types and empty strings are errors naming the file and line, as for the
root mapping.

Contract spans remain 0-based UTF-8 byte columns on LF-normalized source;
links convert them. `column_unit` accepts `utf16`, `codepoint` or `byte`;
`column_base` accepts integer 0 or 1. Lines stay 1-based for every preset.

Templates accept `{abs_path}`, `{abs_url_path}`, `{rel_path}`, `{line}`,
`{column}`, `{end_line}` and `{end_column}` only, and require one of the path
placeholders. Paths use forward slashes; the absolute path joins the supplied
repository root with the contract relative path. Paths are percent-encoded as
UTF-8 (space `%20`, `#` `%23`, `%` `%25`, also `&`, `=`, `+` and `?`, so the
value is safe in a query string), preserving slashes and the Windows drive
colon. `{abs_path}` is the plain absolute path: `/home/u/a.ts` or
`C:/src/my%20project/a.ts`. `{abs_url_path}` is the same path with exactly
one leading slash, the URL path form VS Code's handler expects:
`/home/u/a.ts` or `/C:/src/my%20project/a.ts` (a Windows UNC path keeps its
two leading slashes). The default link is therefore
`vscode://file/home/u/a.ts:3:5` on Linux and macOS and
`vscode://file/C:/src/a.ts:3:5` on Windows. A template written with
`vscode://file/{abs_path}` keeps its earlier output, which has a double slash
on Linux and macOS. Relative paths use the same encoding. The configured base
applies to both columns.

The web UI (`graphvein serve`) builds its IDE links from the same `ide`
settings as the CLI, loading `graphvein.yaml` on every request, and reports
the label as `ide.label` (null for a template without a preset label), unless
the browser chose an editor: then the server builds the links of that preset
alone (`editor=` parameter), and no repository field (`link_template`,
`label`, `column_unit`, `column_base`) applies.

Commands read each source file at most once. A link claims `exact: true`
only when every step of the column conversion is proven for that editor;
otherwise it names the line only (when the editor has a line-only form,
jetbrains) or is `exact: false` with a caveat. Hash mismatch, unreadable source,
missing lines, offsets beyond line ends or inside UTF-8 sequences produce
`exact: false` with both original byte columns plus the configured base
(jetbrains: the line-only link).
A link that names the line only adds `line_only: true`; a link converted
from unchanged source that is still not exact adds `caveat`
(`tab_size_unknown` or `column_unit_assumed`); both are omitted otherwise.
On line 1 of a file with a byte order mark the conversion first drops the
mark's three bytes, which contract columns count but VS Code, IntelliJ IDEA
and Zed drop from the document (as the code peek's highlight does), so
`\uFEFFfunc x` at byte 7 is `:1:5` in VS Code; a column inside the mark is
not exact.

Review correction (2026-11-04, T016k): the jetbrains column unit was
`codepoint` (from IntelliJ's `LogicalPosition`); IntelliJ navigates by the
UTF-16 offset of `DocumentUtil.calculateOffset`, so it is `utf16`. Line 1
of a file with a byte order mark counted the mark; it no longer does.
A changed-file jetbrains link named the stored byte column; it is now
line-only. Cursor links were `exact: true`; they are now `exact: false`
with `caveat: column_unit_assumed`.
Synthetic nodes receive no link. Only test comparisons replace absolute roots
with `{ROOT}` (the root without its leading slash, always written after a
slash, so `vscode://file/{ROOT}/a.ts` on every OS); real output always
includes the actual checkout root.

## Clients

Optional clients.base_paths maps repository-relative directory paths (or '.')
to base paths beginning with '/', or an empty string for no prefix. Unknown
keys, duplicate directories, aliases, invalid paths and non-string values are
errors under the same strict decoding rules. Example:

```yaml
version: 1
clients:
  base_paths:
    frontend: /api
    frontend/admin: /admin/api
```

The longest directory prefix containing a call supplies its unresolved base.
Resolved source bases take precedence. Opaque dynamic URL-builder results have
null templates, keep base unresolved and cannot be repaired by configuration. Configuration edits
relink current client calls without reparsing unchanged source files.

Optional `clients.link_generated_output` (a boolean, default `false`)
links the client calls of committed bundler output too. By default a file
whose path matches a bundler output layout (Next.js `_next/static/`, Vite
`assets/<name>-<hash>.js`, webpack `static/js/<name>.<hash>.chunk.js`) and
that is minified keeps its `client_call` nodes with status
`generated`, links none of them and gets one `client_calls_generated_output`
diagnostic ([ADR 0085](adr/0085-generated-bundle-output.md),
[client extraction](extract/clients.md)). Only `true` or `false` is
accepted (not `yes` or a string). Example:

```yaml
version: 1
clients:
  link_generated_output: true
```

## Tests

Optional tests.include and tests.exclude are lists of nonempty string patterns,
without NUL, leading ! or backslash escapes. Unknown/duplicate keys, aliases,
wrong types and malformed character classes are errors naming the config file
and line, with command exit 2. Exclude wins over includes and built-in test rules.

```yaml
version: 1
tests:
  include: ['checks/**', '**/*.scenario.ts']
  exclude: ['tests/production-adapter.ts']
```

Patterns use a case-sensitive gitignore subset relative to the repository root.
A slash other than a trailing slash anchors the pattern at the root; otherwise
it matches at any depth. A trailing slash matches ancestor directories only.
Other patterns match a file or any ancestor directory. * matches within a
segment, ? matches one non-slash character, [...] is a character class with !
or ^ negation, and a whole ** segment matches zero or more segments. The scanner
is unchanged. Configuration edits relink without reparsing unchanged files.
See [test scope](extract/test-scope.md) for built-ins and matching behavior.

## Architecture

Optional `architecture.hide` is a list of patterns in the `tests` pattern
syntax, matched against each component's directory (the directory of its
package manifest): a matching component is hidden by default in the
architecture view (row CV4 of `docs/contract/claims/architecture-graph.md`),
counted in its Hidden note as "configured", like test-only components and
docs sites (a package declaring `@docusaurus/core`, `vitepress`,
`@astrojs/starlight` or `nextra`). It changes visibility only, never a
component, kind or link, and applies without a re-index. Unknown keys,
non-list values and invalid patterns are errors naming the line.

```yaml
version: 1
architecture:
  hide: ['examples/', 'website/']
```

## OpenAPI

Optional `openapi.include` and `openapi.exclude` use exactly the `tests` pattern
syntax and strict list/key/type validation described above; invalid configuration
exits 2. These patterns affect discovery only, independently of test scope.
Exclude wins over include, basename and content discovery.

```yaml
version: 1
openapi:
  include: ['contracts/**']
  exclude: ['examples/old-api.yaml']
  base_paths:
    contracts: /api
    contracts/admin.yaml: /admin/api
```

Only scanner-visible JSON/YAML files can be included. A confirmed root version
is still required. Configuration edits relink retained sources without reparsing
unchanged language files. See [OpenAPI import](extract/openapi.md).

Optional `openapi.base_paths` maps repository-relative spec files or directories
(or `.`) to bases beginning with `/`, or `""` for none. The longest containing
key applies. Validation uses the same strict map/path/string rules as
`clients.base_paths`, including rejection of duplicate keys and aliases.
Unlike client bases, this override replaces all server-derived alternatives,
including already resolved servers, with one configured path and no origins.
It affects matching at link time, so edits require no language reparse.

## Index

```yaml
version: 1
index:
  shape_depth: 6
```

Optional `index.shape_depth` (integer 1 to 10, default 6) bounds how many
proven calls the route shape extractors follow from a handler to find
parameters, bodies and responses ([route shape](extract/route-shape.md)).
A chain that would go deeper ends with the stop reason `depth_cap`, shown
as "depth cap reached (N)" on the affected items; raise the value to follow
further. `graphvein index --shape-depth N` overrides it for one run (the
server's re-index uses the file's value). Following runs at link time over
cached syntax, so a change takes effect on the next index run without
re-parsing; the depth used is stored on every route shape. Unknown keys,
duplicate keys, non-integers and values out of range are errors naming the
line.

## Explorer

```yaml
version: 1
explorer:
  path_values:
    - key: "route:src/workflows.py#POST%20%2Fworkflows"
      value: /api/v2/workflows/{workflow_id}
    - key: nextjs_base_path:apps/web
      value: /dashboard
    - key: "mount:internal/web/routes.go#createRouter%20r.Route%28base%29"
      value: /
      registration: "sha256:d7fab18bd33c75f2d3b5f461bda203366a795aabf4df4e1d8ab1643305d8d800"
```

Optional `explorer.path_values` states path parts Graphvein could not read
from the code, so the API explorer, try-it and the generated OpenAPI
document ([graphvein openapi](cli/openapi.md)) can place a route that has
no usable path (rows OG-90..OG-96 of the
[claim contract](contract/claims/openapi-generation.md)). The values never
change the index, links or any other view, and wherever they are used they
are marked "set by you", never shown as read from code.

Each entry has `key` and `value`, both strings, and optionally `registration`:

- `key: route:<file>#<METHOD> <pattern>` (the route's identity, as the
  explorer and `x-graphvein.unresolved[].keys` show it, percent-escaped):
  `value` is the whole path template of that route, starting with `/`;
  parameters are `{name}` (a name without `/`, `*` or `:`). The key stays
  the same across re-indexing while the file and the registration stay,
  but moves to another registration when one with the same method and
  pattern is added or removed above it in the file (the `@2` ordinals
  shift). `registration` (`sha256:` and 64 hex digits, the route's
  stored content hash; the explorer's edit writes it) ties the value to
  the registration: when the hash no longer matches, the value is not
  used (`unused`, "registration changed"); a registration whose text has
  an identical twin in the file cannot be tied and gets no value; without
  `registration` the value is used with a warning.
- `key: nextjs_base_path:<project directory>` (`.` for a project at the
  repository root): `value` is the Next.js `basePath` of that project,
  used before every route template of it when the configured `basePath`
  is not a literal (read from the environment or a condition). It starts
  with `/`, does not end with `/` and holds no braces, or is `""` (no
  base path).
- `key: mount:<file>#<function> <hop>` (an unresolved mount hop, as the
  explorer's "Missing prefixes" list and `x-graphvein.unresolved[].keys`
  show it; [route extraction](extract/routes.md#unresolved-mounts-t029-fu-2)):
  `value` is the part of the path Graphvein could not read at the hop.
  One value covers every route below the hop (dozzle's
  `r.Route(base, ...)`: about 85 routes). What it covers depends on what
  Graphvein proved above the hop (the explorer's editor says which):
  - every router above the hop is proven: the value is only the part the
    hop adds. Graphvein composes the prefix it read above the hop, so
    `r.Route("/outer", ...)` then `r.Route(base, ...)` with value `/b`
    places the routes at `/outer/b/...`.
  - the hop adds nothing Graphvein could not read (a net/http `Handle`
    without `StripPrefix` whose pattern or router is not proven, a router
    behind a wrapper passed to `Handle`): the routes are placed at the
    paths Graphvein read, and the only accepted value is `/`, which states
    that the hop passes the requests on unchanged.
  - a router above the hop is not proven either (dozzle's escaped router,
    miniflux's middleware chain, a hop below another unknown hop): the
    value is the whole prefix from the root, including everything above
    the hop.

  `/` means the hop adds no prefix. For a wrapper Graphvein could not
  prove passes paths through unchanged, the value states that it passes
  the request through with this prefix. The value starts with `/`, does
  not end with `/` (unless it is `/`), has no empty, `.` or `..` segment
  and may hold `{name}` parameters. `registration` (written by the
  explorer's edit) is the hop's hash, which also covers the prefixes and
  hops Graphvein read above it: when the hop's text or the composition
  above it changes, the value is not used ("registration changed"); two
  hops with the same text in one function cannot be told apart and get
  no value. Operations
  placed below a Go or Axum hop are marked `shadowing: not_checked`: Graphvein did
  not check whether a pattern beside the hop answers some of their paths
  first.

A value holds no spaces, control characters, `?` or `#`. A route key wins
over its project's base path key and over its hops' keys. A value for a route that has a path read
from code is never used, and a value whose path is already a code-read
operation (same path and method, or the same path up to parameter names)
is refused on its own; the document reports both `unused` with the
reason, and the operations read from code stay. Keys are
unique; unknown keys, a missing `key` or `value`, other types and values
that break these rules are errors naming the line. The explorer edits this
list with a guarded edit (diff preview, the file's hash, a session token,
loopback only, atomic write; [serve](cli/serve.md#get-and-post-apiv0explorerpath-values)),
which keeps the file's comments and re-indents it with two spaces.

## Changes

```yaml
version: 1
changes:
  journal_runs: 30
  journal_budget: 1.0
  max_pins: 5
  max_ref_snapshots: 4
```

Optional `changes` bounds what the store keeps for change comparisons
([ADR 0056](adr/0056-change-journal-and-impact.md)). Every key is optional and
takes the default shown above.

- `journal_runs` (integer 1 to 1000): how many index runs the change journal
  keeps. Every run of `graphvein index` and of the server's re-index writes one
  journal run: the payloads that run changed or removed, its extractor, the
  HEAD commit and whether the indexed files were exactly that commit (dirty
  flag), and the files with error diagnostics (knowledge gaps). An unchanged
  re-index writes one small row and no entries.
- `journal_budget` (number above 0, at most 100): the journal's entry bytes
  may be at most this factor times the current graph's payload bytes. The
  oldest runs are dropped first when either limit is exceeded; the newest run
  is always kept, so "since the previous run" stays comparable. A comparison
  whose base run is no longer covered is refused, never answered partially.
- `max_pins` (integer 1 to 100): how many pinned baselines a repository may
  hold. Pins never expire; each keeps the payloads its records had at the pin.
- `max_ref_snapshots` (integer 2 to 100: a ref pair needs both snapshots): how many indexed git commits (ref
  snapshots, for branch-versus-base comparisons) are kept; the least recently
  used is dropped first, with everything stored under it.

A ref snapshot is stored in the same store under the repository identifier
`<repository>@ref:<commit>`. Its commit's tree is read with local git
commands only (no fetch: a partial clone whose objects are missing fails
instead), written to a temporary directory and indexed with the fact cache
of the working tree's index, so only files that differ are parsed. The
commit's own `graphvein.yaml` configures extraction; when the commit has
none, the working tree's file is used. The `changes` limits always come from
the working tree's `graphvein.yaml`. Unknown or duplicate keys, wrong types
and values out of range are errors naming the file and line.

## UI

```yaml
version: 1
ui:
  labels: technical
  impact_depth: 4
  chain_depth: 8
```

Optional `ui` holds web UI defaults for `graphvein serve` (T025b, ADR
0078; depths T042). Each key applies when a browser has made no choice of
its own; a browser's own choice always wins.

| key | values | built-in default | what it sets |
| --- | --- | --- | --- |
| `labels` | `plain` or `technical` | `plain` | how the UI words ids, kinds and confidence numbers ([serve: Labels](cli/serve.md#labels)) |
| `impact_depth` | integer 1 to 10 | 3 | the depth the Impact tab of the Changes section starts with ([serve: Depth defaults](cli/serve.md#depth-defaults)) |
| `chain_depth` | integer 1 to 10 | 6 | the depth the architecture chain starts with |

`labels` absent means plain, with a one-time banner asking each browser;
set, it applies without the banner. The depth keys only choose where the
depth switches start; any depth 1 to 10 can still be chosen in the UI,
and a link that names a depth shows that depth. Unknown or duplicate
keys, a non-mapping section and any other value (a depth outside 1 to 10,
a quoted or decimal number) are errors naming the file and line, like
every other section.

## Try it

```yaml
version: 1
tryit:
  environments:
    - name: local
      label: local
      base_url: http://127.0.0.1:8000
    - name: production
      label: production
      base_url: https://api.example.com/v1
      auth:
        header: Authorization
        env: API_TOKEN
        prefix: "Bearer "
    - name: dev
      label: local
      base_url: https://127.0.0.1:8443
      tls:
        pinned_sha256:
          - 9595c84a6a0056655a84aeab1ea353000b20d094d4fdc0247c76540ae5a151df
        ca_file: certs/rootCA.pem
      use_system_proxy: false
      limits:
        response_bytes: 16777216
        timeout_seconds: 120
```

Optional `tryit` lists the environments the web UI's "try it" may send
requests to (T029, ADR 0081; rows `TI-*` of
[the try-it claim contract](contract/claims/try-it.md)). An environment
is the only thing that makes a host reachable for a try-it request:
`graphvein serve` dials exactly the origin (scheme, host and port) of the
environment the user selected, through `internal/egress` with the purpose
`try-it`, which is allowed in every egress mode. Nothing is inferred from
file names or code; the UI's base URL suggestions are only written here
when the user picks one and confirms the previewed edit (see
[serve: Try it endpoints](cli/serve.md#try-it-endpoints-t029)).

- `environments` is a list. Each entry has `name` (1 to 64 letters,
  digits, `-`, `_`, `.`; unique), `label` (the user's word, 1 to 32
  characters: `local`, `staging`, `production`, ...) and `base_url`
  (`http://` or `https://`, a host name or IP literal, optional port and
  base path; no user info, query, fragment or `.`/`..` segments; scheme
  and host are lower-cased and a trailing `/` is dropped).
- A label of `production` or `prod` (any case) makes the environment a
  production one: the UI must acknowledge it once each time it is
  selected before any request is sent, GET included.
- Optional `auth` names one request header and the environment variable
  that holds its value: `header` (an HTTP token; not `Host`,
  `Connection`, `Content-Length`, `Transfer-Encoding`, `TE`, `Trailer`,
  `Upgrade`, `Keep-Alive`, `Expect` or `Proxy-*`), `env` (a variable
  name, never a value) and optional `prefix` (put before the value, for
  example `"Bearer "`). The server reads the variable when it sends; when
  it is unset, the request is refused unless a value was typed in the UI
  for the session (kept in server memory only).
- Optional `tls` adds trust for this environment's origin only; it never
  turns verification off (row TI-17):
  - `pinned_sha256` lists SHA-256 fingerprints of leaf certificates (of
    their DER encoding: what browsers and `openssl x509 -fingerprint
    -sha256` show), at most 16, as 64 hex digits (a `sha256:` prefix and
    colons are accepted). A pinned certificate is accepted for this
    origin whatever its names, issuer or validity dates, SSH-style: for a
    self-signed development server, or one whose certificate names
    another host. Pins add trust; a certificate that verifies normally is
    still accepted. When the server presents a certificate that neither
    verifies nor is pinned, the send is refused as "certificate changed"
    with the old and new fingerprints (row TI-29); nothing is accepted
    silently. The UI offers the fingerprint of a refused certificate and
    writes the pin through the guarded edit. A pin applies to one origin:
    a plain fingerprint to the environment's `base_url` origin; the form
    `{origin: https://host:port, sha256: ...}` names it, and is required
    in the manifest's project environments (a base URL per member), where
    the origin must be one of the environment's base URLs.
  - `ca_file` is a PEM bundle (for example mkcert's root CA) trusted in
    addition to the system roots for this environment; a relative path is
    read from the repository root (from the project directory for the
    manifest's environments). It is read for every request (a regular
    file of at most 4 MiB, within the environment's time limit); a
    missing, unreadable or empty file refuses the send with the reason
    (row TI-42). Its content is part of the environment's identity: when
    the bundle changes, the environment is selected again.
- Optional `use_system_proxy: true` (default `false`) sends this
  environment's requests through the proxy the server's environment
  names: `HTTPS_PROXY` (or `https_proxy`) for https, `HTTP_PROXY` (or
  `http_proxy`) for http, unless `NO_PROXY` (or `no_proxy`) matches the
  host (`*`, an IP, a CIDR range, `example.com` for it and its
  subdomains, `.example.com` for subdomains only, optionally with
  `:port`). `localhost` and loopback addresses are never proxied. The
  variables are read at each send; `http`, `https`, `socks5` and
  `socks5h` proxies are supported, and user info in the proxy URL is sent
  to the proxy, never shown. An `https` proxy is verified against the
  system roots for its own name; the environment's `tls` never applies
  to it. With an `http://` environment the proxy sees the whole request,
  auth header included, in clear. The final destination is still only
  the environment's origin (row TI-43). Without this option every proxy
  variable is ignored.
- Optional `limits` raises (or lowers) this environment's limits:
  `request_bytes` (default 1 MiB, at most 64 MiB), `response_bytes`
  (default 4 MiB, at most 256 MiB) and `timeout_seconds` (default 30, at
  most 600), each a whole number of at least 1 (row TI-44). A value above
  its ceiling is an error, never clamped. A cut response or a refused body
  names the limit and how to raise it.
- `tls`, `use_system_proxy` and `limits` can be edited from the UI
  through the same guarded edit that adds environments (row TI-48).
  Changing `tls` or `use_system_proxy` makes the environment a different
  destination for the session: it must be selected again.
- Unknown or duplicate keys, aliases, a missing `name`, `label` or
  `base_url`, a duplicate name and any invalid value are errors naming the
  file and line, like every other section.

In project mode the manifest's own `tryit` section
([project: The manifest](cli/project.md#the-manifest)) lists project-wide
environments with `base_urls` (member: URL) instead of `base_url`, and
the same `name`, `label`, `auth`, `tls`, `use_system_proxy` and
`limits` (applying to every member's base URL); each member's
environments are those with a base URL for it plus its own
`graphvein.yaml` ones.

Limits unless an environment sets `limits`: a request body sent of at
most 1 MiB, a response read up to 4 MiB (the rest is dropped and the
answer marked truncated), 30 seconds per exchange. Fixed: 10 seconds per
connection attempt, 4 requests at once. Redirects are never followed
automatically (the UI can add the target's origin as an environment and
follow once), and TLS certificates are always verified against the
system roots, the environment's `ca_file` and its pins.

## Egress

```yaml
version: 1
egress:
  mode: offline
```

Optional `egress` is a mapping with the single key `mode`, whose value is the
string `offline` or `llm`. Absent means `offline`. Unknown or duplicate keys,
aliases, a non-mapping section, and non-string or other values are errors
naming the file and line; commands exit 2 before opening storage. There is no
environment override.

`offline` allows only explicitly configured infrastructure: the Postgres host
from `GRAPHVEIN_POSTGRES_URL` is always allowed, in every mode, and so is
the origin of the "try it" environment selected in the UI
([Try it](#try-it)), for that one request, and, when that environment
sets `use_system_proxy`, the proxy named by the server's proxy
variables. `llm` will
additionally allow configured LLM providers; none exist yet, so today it
permits exactly what `offline` permits. See ADR 0040.

# Claim contract: workspace (multi-index server)

Task T038 ([ADR 0087](../../adr/0087-multi-index-server.md), and the ADR 0041
revision for typed folder paths). Rows `WS-1`..`WS-40` and the default
row `WS-D` are what `graphvein serve` states about the workspace: the
registry, the list of indexes, dispatch by entry, warm indexes and their
answer cache, the background freshness checks, and the typed-path flow
that adds a folder. They are implemented by `internal/workspace` and
`internal/server/workspace*.go`; `TestWorkspaceContractTable` keeps this
table 1:1 with the code's row list (`workspaceRows`), checks that every row
names an existing test, and that every row a refusal carries is in the
table. Rows `WS-41`..`WS-69` are reserved for the web UI's claims; the
The T038 review (ADR 0087 revision 2) took `WS-53` and `WS-54` from that
range for two server rows of the typed-path flow and `WS-55` and `WS-56`
for two web UI rows; revision 3 took `WS-57` (server) and `WS-58` (web UI)
for names shared in one store.

Endpoints and payloads are documented in
[docs/cli/serve.md](../../cli/serve.md) ("Workspace"); the registry file in
[docs/config.md](../../config.md) ("Workspace registry").

## Registry

| Row | Topic | Rule | Refusal or unknown |
|---|---|---|---|
| WS-1 | registry file | `GRAPHVEIN_WORKSPACE_FILE`, else `workspace.yaml` in the `graphvein` folder of the user configuration directory (`%AppData%` on Windows, `~/Library/Application Support` on macOS, `$XDG_CONFIG_HOME` or `~/.config` elsewhere); a missing file is an empty registry | no configuration directory: the registry is unavailable, `serve <path>` still serves its index and the list reports `registry.error` |
| WS-2 | registry format | YAML `version: 1` and `entries`, each with `kind` (`repository` or `project`), `name`, an absolute `path` and `added_at`; unknown fields refused; the store location is never stored (it is resolved from the folder as the CLI resolves it) | a file that does not parse, another version or an invalid entry is an error on every read and write, and is never rewritten or shown as empty |
| WS-3 | atomic writes | every write reads, changes and replaces the file through a temporary file in the same folder under an exclusive operating-system lock on `workspace.yaml.lock` (`flock` on Linux, macOS and the BSDs, `LockFileEx` on Windows), so writers in two processes never lose an entry; the kernel releases the lock when its holder exits, so the lock file left behind never blocks; reads take no lock | the lock held for more than 10 s: the write fails with "held by another process" and the file is unchanged |
| WS-4 | entry identity | `id` is `<name>~<first 8 hex of SHA-256(kind, NUL, path, NUL, name)>` at registration: the same on every start and in every link (`ws=`); the same name in another folder gets another id; one folder is one entry: adding a folder already registered (also under a new name or kind, as `serve <path>` after a rename does) returns the stored entry and its id; paths are compared as equal strings, or as spellings differing only in letter case that the file system confirms are one folder (`os.SameFile`), so adding one folder spelled two ways on a case-insensitive file system gives one entry | a folder that is gone is compared by its text only |
| WS-5 | folders that are gone | an entry is never dropped by the server or the registry; only remove drops it | - |
| WS-6 | who registers | `serve <path>` and `serve --project` (after their store opened), and add from the UI; `graphvein index` does not | a registry that cannot be written is logged; serving goes on |

## The list: GET /api/v0/workspace

| Row | Topic | Rule | Refusal or unknown |
|---|---|---|---|
| WS-7 | the list | every entry, projects first, then repositories, each by name, then by path; with the session token for the workspace POSTs and the open limit (8) | - |
| WS-8 | entry state | `folder_gone` when the folder is proven missing (any other stat error is not called gone), also while its index is still open; else `open` (its index is open in this server) or `not_open`; the list stats each folder and reads each manifest (and, under a shared store, each `graphvein.yaml`, WS-57), and never opens a store or probes a lock, so it writes nothing into an entry's folder; `name` is the registered one, or while open the one the index serves: the repository `graphvein.yaml` gave as it opened, or the name of the manifest the open project serves (as opened, or as a manifest edit or any request of that project last read it from disk) | - |
| WS-9 | entry freshness | `null` until a background check of the entry or a `GET /api/v0/status` for it (or for a project member) finished, whose answer is recorded at once; only a later check replaces a recorded one; then the states of `GET /api/v0/status` (`fresh` only from a successful zero-change check of a store with runs), with `checked_at` taken before the walk; a project's freshness is folded at each list over the members its manifest lists then, checked at the oldest member's time: `null` while any listed member has no recorded check (a member added since, or one whose listed path is not the path its recorded check was for: an open project checks the paths it opened with until any request of that project reads the edited manifest), otherwise `fresh` only when every member is, `stale` when any member is, `not_indexed` when every member is, else `unknown`; a member without a checkout is `unknown`; a folder that is gone has `null` | never `fresh` from the registry alone, nor over a member nobody checked |
| WS-10 | project members | read from `graphvein-project.yaml` on disk at each list: repository, path, whether the checkout exists, its freshness | a manifest that does not load: `members` null and `members_error` |
| WS-11 | entry job | the entry's latest re-index job of this server process (the `job` of `GET /api/v0/index/run?ws=`) while its index is open, read from memory (no lock probe) | not open: `null` |
| WS-12 | current entry and suggestion | `current` is the entry served without `ws` (the command-line index, or without a path the registered entry whose folder is the working directory); `suggestion` is an unregistered working directory holding `graphvein-project.yaml` or a `.graphvein` folder | none: `null` |

## Dispatch and warm indexes

| Row | Topic | Rule | Refusal or unknown |
|---|---|---|---|
| WS-13 | per-index dispatch | every endpoint of a single-index server answers per entry through `ws=<id>`; `ws` is removed before the index's handlers see the request; without `ws` the current entry answers, so single-index URLs keep working | no current entry: 400 `ws_required` |
| WS-14 | ws errors | - | an id the workspace does not have: 404 `unknown_entry` (never another index in its place); `ws` empty or given twice: 400 `bad_request`; a registry that cannot be read: 500 `registry_error` |
| WS-15 | open errors | an entry's index opens read-only on first use, as `serve <path>` or `serve --project` opens it; `POST /api/v0/index/run?ws=` first creates an empty store where `graphvein index` would when there is none; `GRAPHVEIN_SQLITE_PATH` applies only to the index named on the command line | no store at the folder: 409 `no_index`, naming the run request and the `graphvein index` command; the folder gone: 409 `folder_gone` (nothing is created), also for an index already open when the request may write (any method but `GET` and `HEAD`); `GRAPHVEIN_SQLITE_PATH` set and the entry is not the command-line index: 409 `single_store`; a name shared with another folder in one store: 409 `shared_store_conflict` (WS-57); any other open failure: 500 `store_error` |
| WS-16 | open indexes | at most 8 indexes open; beyond that the least recently used index without a request in flight, a running re-index or ref job, and not the command-line index, is closed (its jobs stopped, cache dropped, store closed) | all busy: the limit is exceeded until one is idle |
| WS-17 | answer cache | only `GET` of `/api/v0/api/endpoints`, `/api/v0/precision`, `/api/v0/schema/summary`, `/sets`, `/assumptions`, `/api/v0/architecture` and `/architecture/chain` (views that read only the store and `graphvein.yaml`), keyed by path, query (without `ws`) and the index fingerprint; 200 answers only; `X-Graphvein-Cache: hit`, `miss` or `bypass` | status, freshness, source, file states, editor links and every other path are never cached |
| WS-18 | cache invalidation | the fingerprint is the latest run (count, id, finish time) of the repository or of every member, the content hash of each `graphvein.yaml` and, for a project, the manifest files and the last link pass; a commit by another process, a configuration or manifest edit changes it; the end of this server's run of the index drops the cache, and an answer computed before that is not stored | the fingerprint cannot be read: `bypass` (answered uncached) |
| WS-19 | cache bounds | at most 64 MiB per index and 256 MiB per server, least recently used answers evicted first; an answer over 4 MiB is not cached | - |
| WS-20 | background checks | one check at a time, in its own goroutine: every entry once at start, then every 30 s the open entries and each entry that is not open whose check is missing or older than 2 minutes; an add asks for a pass at once; a folder that is gone, or an entry in a shared-store conflict (WS-57), is not checked and its recorded check is dropped; a check cut short by shutdown records nothing | - |

## Typed folder paths: POST /api/v0/workspace/check, /add, /remove

| Row | Topic | Rule | Refusal or unknown |
|---|---|---|---|
| WS-21 | POST guards | the guard of every state-changing POST (Host allow-list, `Sec-Fetch-Site`, `Origin`, `http.CrossOriginProtection`) plus a loopback peer, the session token in `X-Graphvein-Token` (from `GET /api/v0/workspace`), `Content-Type: application/json`, at most 4 KiB, one JSON object with only the documented fields and no duplicate keys | 403 `host_not_allowed`, `cross_site_request`, `not_loopback`, `token_required`; 415 `unsupported_media_type`; 413 `too_large`; 400 `bad_request`; another method: 405 |
| WS-22 | absolute paths only | the path is used as typed; `~` and variables are not expanded | relative: refused `path_not_absolute` |
| WS-23 | symbolic links | resolved; `resolved` (what the confirmation names and add registers) is the absolute folder without links, each component spelled as the file system lists it (on Windows with an upper-case drive letter), so one folder typed in two cases resolves to one path | - |
| WS-24 | no such folder | - | refused `path_not_found` |
| WS-25 | not a folder | - | a file: refused `not_a_directory` |
| WS-26 | filesystem root | - | refused `filesystem_root` |
| WS-27 | home folder | - | the home folder itself: refused `home_directory` |
| WS-28 | configuration folders | - | the user configuration folder itself, or the registry's folder or a folder inside it: refused `config_directory` |
| WS-29 | index and git folders | - | a path with a `.graphvein` or `.git` component: refused `inside_index_store` |
| WS-30 | inside a registered folder | - | refused `inside_registered`, naming the entry |
| WS-53 | system folders | - | the operating system's own folders, as typed or resolved, compared as strings and as one folder on disk (`os.SameFile`): `/tmp`, `/etc`, `/dev`, `/proc`, `/sys`, `/run`, `/var`, `/var/lib`, `/usr`, `/bin`, `/sbin`, `/lib`, `/lib64`, `/boot` themselves (on macOS also `/private/tmp`, `/private/etc`, `/private/var`) and any folder inside `/dev`, `/proc` or `/sys`; on Windows the system drive's (`%SystemDrive%`, else `C:`) `Windows`, `Program Files`, `Program Files (x86)` and `ProgramData` and the folders `%WINDIR%`, `%SystemRoot%`, `%ProgramFiles%`, `%ProgramFiles(x86)%` and `%ProgramData%` name, compared after removing a `\\?\`, `\\.\` (also `\\?\UNC\`) or `\\localhost\X$\` prefix, applying `.` and `..` and dropping each component's trailing dots and spaces, ignoring letter case and separators (an 8.3 short name is matched on disk): refused `system_directory` |
| WS-54 | folders holding registered entries | a folder that contains registered entries (a monorepo above its parts) is not refused; a `ready` check lists in `contains` those whose folder exists (id, kind, name, path; empty when none; a registered folder that is gone is left out), and its index reads their files again into a store of its own | - |
| WS-31 | already registered | outcome `registered` with the entry id and the index command; no confirm value | - |
| WS-32 | project folders | a folder holding `graphvein-project.yaml` is a project named by the manifest, with its members (repository, path, checkout) | a manifest that does not load: refused `invalid_manifest` |
| WS-33 | repository folders | any other folder is a repository named by its `graphvein.yaml` `repository`, else the folder name | a `graphvein.yaml` that does not load: refused `invalid_config` |
| WS-57 | names shared in one store | only when every entry's index lives in one store (`GRAPHVEIN_POSTGRES_URL` set), where facts are kept by repository ID and links by project name, not by folder: an entry's names are its repository (from `graphvein.yaml`, else the registered name; an open index's names as it opened, `--repository` included) or, for a project, its name and each member: a member with a path (whether or not its checkout exists) from that path, a member without a path from no folder; two registered entries whose folders differ (a member's path counts as its folder, so a project and its member's own folder do not conflict) and that use one name are both in conflict, and so are an entry that uses a name from a folder and a project whose member without a path uses it (that member reads whatever the store holds under the name, which would be the other folder's facts; two members without a path share it by design and do not conflict): the list carries `conflict` (code, message, the name and the other entry's id, name and path) and no freshness, every per-index request is refused, the background check records nothing, a typed path that would add another user of a name is refused before its store is read, and `serve <path>` or `serve --project` exits 1 before it registers or opens anything; renaming one (`repository:` in `graphvein.yaml`, `name:` in `graphvein-project.yaml`) or removing one entry ends it, and the message asks for both folders to be indexed again; without `GRAPHVEIN_POSTGRES_URL` each folder has its own store (`GRAPHVEIN_SQLITE_PATH` serves only the command-line index, WS-15), so nothing is shared | per-index requests: 409 `shared_store_conflict`; a typed path: refused `shared_store_conflict` naming the entry; the message names the other entry, both folders and how to rename one |
| WS-34 | index presence | the folder's store opened read-only: `present` with the last run's finish time when runs are stored; `absent` when no store exists or it holds no run; the store location (`sqlite` path, or `postgres` and the variable name, never the URL) | any other open or read failure: `unknown` with the error |
| WS-35 | confirm value | a `ready` check returns a random one-time value bound to the resolved folder, kind and name, valid 10 minutes; at most 32 are pending (the oldest dropped) | - |
| WS-36 | add | needs `path` (the resolved folder) and its `confirm` value, which any add uses up; re-runs every rule; registers the entry; then (`index` absent or true) opens the folder's index, creating an empty store where `graphvein index` would, and starts its run as `POST /api/v0/index/run?ws=` does: 202 with the entry and the job, whose progress is counted by the run | no, used, expired or another folder's value: 409 `confirm_required`; the folder changed since the check: 409 `path_changed` (neither registers); the index not opened or the run refused after the entry was registered: 200 with the entry, `job` null and `run_error` (the code and message `POST /api/v0/index/run` would answer, or `store_error`) |
| WS-37 | add without a run | `"index": false` only registers: 200, `job` null, no store created; `POST /api/v0/index/run?ws=` starts its run later (WS-15) | - |
| WS-38 | add a project | the run of a project is the project re-index (each member checkout, then the link pass) with the project job document | - |
| WS-39 | remove | drops the entry from the registry only: the folder and its index stay; an open index is closed (when its last request ends) | the command-line index: 409 `entry_in_use`; a running re-index or ref job: 409 `index_running`; no such entry: 404 `unknown_entry` |
| WS-40 | index run shape under --project | `GET /api/v0/index/run` always carries `repository` (the empty string in project mode, beside `project`) | - |
| WS-D | any other path error | - | an error reading the path that no row covers (for example an invalid name): refused `path_unreadable` with the system's message; a registry that cannot be read during a check: refused `registry_error` |

## Web UI (WS-41..WS-69)

What the web UI states about the workspace beyond the rows above. The UI
reads only `GET /api/v0/workspace`, the typed-path POSTs and the per-index
endpoints with `ws=`. `web/src/shell/workspace/rows.ts` is kept 1:1 with
this section by `rows.test.ts`, which also checks that every row names an
existing web test. Rows `WS-53`, `WS-54` and `WS-57` are server rows
(above); `WS-59`..`WS-68` stay free.

| Row | Topic | Rule | Refusal or unknown |
|---|---|---|---|
| WS-41 | freshness dot | an entry's or member's dot comes from its `freshness` only: green for `fresh`, amber for `stale` only; hollow (neutral) for `not_indexed` and `unknown`, and hollow and worded "not checked yet" while `freshness` is null; never green from the registry, an open state or a finished run | a state the UI does not know: hollow, with the server's word |
| WS-42 | folder gone | an entry in state `folder_gone` is struck through with a dashed dot and "folder not found at <path>"; it is never opened by a click and stays listed | - |
| WS-43 | link status | the link status line of a project is shown only for the project being viewed, from its `GET /api/v0/project` link pass (as before T038); other projects show their member count and no link state | - |
| WS-44 | row progress | an entry's running job shows its phase and, only when the run counted both numbers, `done/total` with a bar; no time or fraction is estimated; a committed job offers "Open"; a failed job shows its error code | an unknown phase is shown as the server's word, without a bar |
| WS-45 | add confirmation | the dialog names the resolved folder, the kind and name the check reports (a project with every member path and whether its checkout exists), where the index is written (`store`: the SQLite path, or Postgres and the variable name), what the folder's index holds (`present` with the last run's time, `absent`) and that files are counted by the scan when it starts, with no estimate before; nothing is read until "Index" is pressed | `index.state` `unknown`: "could not be read" with the error |
| WS-46 | add refusals | every check refusal shows the server's message under a plain heading per code (no folder, not a folder, root, home, configuration folder, inside an index or git folder, inside a registered folder with that entry, invalid manifest or configuration); a POST refused by the guard (403) or a `confirm_required`/`path_changed` answer says nothing was read or indexed | an unknown code: the server's code and message |
| WS-47 | links to another index | a URL whose `ws` names no entry of this server says so, naming the id, and shows the current entry (or the first-run state), never another entry in its place silently | - |
| WS-48 | opening an index | after a switch, until the selected index's first status answer, the content shows skeletons and "Opening the index of <name>"; nothing of the previous index is shown meanwhile | an open refusal (`no_index`, `store_error`, `unknown_entry`) is shown as the status failure of that entry |
| WS-49 | view memory | the last view of each entry (section and its URL query, the selected project member, scroll positions) is kept per browser by entry id and restored on switching back; a remembered item the index no longer has is reported by the section's own notice | storage blocked: nothing remembered, nothing claimed |
| WS-50 | copy link | the link holds the entry id, the project member and the section's full URL query, so it opens the same view; the toast names the entry, member and section | clipboard unavailable: the toast says so and shows the link to copy by hand |
| WS-51 | first run | with no entry and no current index, no section shows a fact: the sections are greyed and the page offers "Add a folder" and the `graphvein index` command | - |
| WS-52 | one re-index state | the top bar's Re-index control shows the viewed index's latest job from `GET /api/v0/index/run?ws=`, whoever started it (top bar, add flow, another tab) | - |
| WS-55 | index an entry without a store | an entry whose freshness is `not_indexed` says "not indexed yet · Index it" on one line under its row that never wraps at sidebar width (the row keeps the name, truncated when long, and a hollow dot); "Index it" starts its run (`POST /api/v0/index/run?ws=`) and shows the run's progress on the row; the add dialog's "Already in the workspace" offers the same for a registered folder | the run refused: the refusal's code and message on the row |
| WS-56 | added but not indexed | an add answered with `run_error` says the folder was added to the workspace and indexing did not start, with the reason and a retry; never that the folder was not added | - |
| WS-58 | names shared in one store | an entry with a `conflict` (WS-57) has a hollow dot and, on its row, "name shared with <other entry>" with the server's message (which says how to rename one in `graphvein.yaml`) as its tooltip and screen-reader text; no freshness and no "Index it" are shown for it | - |
| WS-69 | any other workspace state | - | an entry `state`, `kind` or job state the UI does not know is shown with the server's word and never mapped onto a known one |

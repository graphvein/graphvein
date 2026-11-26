# graphvein serve

```
graphvein
graphvein serve [--addr 127.0.0.1:7420]
graphvein serve [path] [--repository id] [--addr 127.0.0.1:7420]
graphvein serve --project <manifest or its directory> [--addr 127.0.0.1:7420]
```

Serves the local web UI and HTTP API v0. Design and reasons:
[ADR 0041](../adr/0041-local-server-and-embedded-web-ui.md); one server
for every index of the workspace:
[ADR 0087](../adr/0087-multi-index-server.md).

- `graphvein` with no arguments serves the workspace too and opens the
  browser at it ([below](#running-graphvein-with-no-arguments)).
- `graphvein serve` with neither a path nor `--repository` serves the
  workspace registry ([below](#workspace-multi-index-server)): every
  registered repository and project, none opened until used. With no
  entry the UI shows its first-run screen ("Add a folder").
- `graphvein serve <path>` and `serve --project` serve that index exactly
  as before (the store is opened at start, with the same errors and exit
  codes), register it in the workspace registry, and serve the other
  registered indexes beside it. Requests without `ws=` answer for it.

- `path` and `--repository` work as for `status` and `query`: path-first or
  flag-first; repository precedence is flag, `graphvein.yaml`, directory
  basename ([configuration](../config.md)).
- A `graphvein.yaml` that does not load does not stop `serve` (unlike
  `index`): it prints the decoder's message and "Serving anyway", and the
  web UI shows the error until the file is fixed (Settings, below). The
  repository is then the file's own `repository` key when it can be read
  (the YAML parses, or exactly one root line sets it), the directory name
  when the file sets none, or `--repository`; a key that is there but
  cannot be read is a usage error (exit 2) asking for `--repository`.
  A registered folder opened from the workspace list is served the same
  way, its repository being the file's key when it can be read, else the
  name it was registered under; adding a new folder whose file does not
  load is still refused (`invalid_config`).
- Storage is chosen as for `query`: `GRAPHVEIN_POSTGRES_URL`, or
  `GRAPHVEIN_SQLITE_PATH` (default `<path>/.graphvein/index.db`). Requests
  read the store through a read-only connection: `serve` never creates or
  migrates it at start. A missing SQLite file or an old schema fails like
  `query`. Run `graphvein index` first. The only write is a re-index
  started with `POST /api/v0/index/run`
  ([ADR 0052](../adr/0052-reindex-endpoint-and-index-lock.md)): it opens a
  separate writer store for that run only, exactly as `graphvein index`
  with default flags does, under the index lock below.
- **Index lock.** One index run at a time per store: `graphvein index`
  (built-in and `--indexer`), `graphvein ingest` and a re-index started
  from `serve` take the same lock before scanning (ingest: before writing)
  and release it after the writer store closed.
  SQLite: an OS lock on the empty file `<store>-lock` (default
  `.graphvein/index.db-lock`), covering the whole store file. Postgres: a
  session-level advisory lock `(714839211, k)`, where `k` hashes the
  current schema and the repository. The lock is released when the holder
  exits, however it exits; there is no stale-lock cleanup to do. A
  `graphvein index` that finds the lock held waits 2 s, then prints
  `another index run holds the lock for this store (<lock file or "Postgres advisory lock">); try again when it has finished`
  and exits 1 before scanning; `graphvein ingest` does the same before
  writing (on Postgres it reads the stream header first, because the lock
  is per repository).
- Re-indexing while `serve` runs is supported: run `graphvein index` with
  the same storage settings and the next status request reports the new
  run. The SQLite store is in WAL mode
  ([ADR 0049](../adr/0049-sqlite-wal-journal-mode.md)), so a running or
  committing `graphvein index`, however large, does not block `serve`: it
  answers from the last committed run until the new run commits. A few
  short locks remain (recovery after a crash, the last connection's
  close-time cleanup, and stores on a filesystem without WAL support, which
  stay in rollback mode and log a warning when indexed); a request that
  meets one waits up to 3 s per read for it (a status request reads twice,
  so about 6 s at most; [ADR 0043](../adr/0043-sqlite-read-busy-timeout.md)),
  and a lock held longer still answers 500 `store_error` or `unknown`
  (SQLite reports the database as locked). A request that overlaps the
  commit may also answer `stale` beside the new run; the next request is
  correct.
- Replacing or deleting the store while `serve` runs is not detected:
  deleting the SQLite file, indexing into a new file at the same path, or
  dropping and re-creating the Postgres database or schema. `serve` may keep
  answering from the old data (for SQLite on Linux and macOS it keeps
  reading the deleted file), so its answers can disagree with
  `graphvein status`. Restart `serve` after re-creating the store. Never
  delete or replace `index.db`, or delete its `index.db-wal` or
  `index.db-shm` file, while any Graphvein process has the store open: stop
  `serve` first, then delete the three files together
  ([SQLite store files](../config.md#sqlite-store-files)).
- `--addr` (default `127.0.0.1:7420`) must be a loopback IP literal, such as
  `127.0.0.1:7420`, `127.0.0.2:7420` or `[::1]:7420`. Names, including
  `localhost`, and other addresses such as `0.0.0.0` are rejected. Port 0
  picks a free port. There is no option to serve beyond this machine.

On start, `serve` prints one line to stdout:

```
Serving <repository> at http://127.0.0.1:7420/
Serving <repository> (+N in the workspace) at http://127.0.0.1:7420/
Serving the workspace (N entries) at http://127.0.0.1:7420/
```

The second form appears when the workspace has N other entries, the
third for `serve` without a path.

The URL holds the actual port (also when port 0 was given), IPv6 hosts in
brackets. Logs go to stderr (slog text): `server started`, `server stopped`,
a warning for each rejected request (reason, Host, path; never the query
string) and an error for each failed request. Ctrl+C stops the server: it
cancels running requests at once (a status request in progress answers 503
`unavailable`), waits up to 5 seconds for connections to finish and then
closes the store. A running re-index is cancelled first and rolls back;
`serve` waits for it to close its writer store and release the index lock
before it exits. SIGTERM is not handled and ends the process at once; a
re-index in progress is then lost without a trace (its transaction never
committed) and its lock is released by the operating system or the
database.

Exit codes: 2 for usage errors (unknown flag, extra argument, an `--addr`
that is not a loopback IP literal); 1 when the store cannot be opened, the
address cannot be bound (for example the port is in use; try
`--addr 127.0.0.1:0`) or serving fails; 0 after Ctrl+C. In Git Bash under
mintty the shutdown is graceful but bash reports exit status 130 for a
Ctrl+C; run through `winpty` to see the program's own status (verified on
Windows, 2026-11-04).

## Running graphvein with no arguments

`graphvein` with no arguments, on every platform and however it was
started (a terminal, a double-click on `graphvein.exe` in Windows
Explorer, a desktop launcher), serves the workspace exactly as
`graphvein serve` with no path does, and opens the default browser at its
URL. `graphvein help` (also `-h`, `--help`) prints the commands, and every
command with arguments behaves as before.

- **Address.** `127.0.0.1:7420`, the `serve` default. When it cannot be
  bound, graphvein first asks it `GET /api/v0/workspace` (loopback only,
  2 s, through the egress policy). If a Graphvein server answers, nothing
  new is started: graphvein prints
  `Graphvein is already running at <url>; opened it.`, opens the browser
  there and exits 0. Otherwise it tries `127.0.0.1:7421` to
  `127.0.0.1:7430` in order and serves on the first free one; the banner
  and the browser use the address actually bound. `graphvein serve` keeps
  failing on a busy `--addr`, as before.
- **Banner.** After the usual `Serving the workspace ...` line, stdout
  shows the URL (`Graphvein web UI: <url>`), "Add a repository in the
  browser. Run `graphvein help` for the commands." and "Press Ctrl+C or
  close this window to stop."
- **Browser.** Opened with the system URL handler (Windows
  `rundll32 url.dll,FileProtocolHandler <url>`, macOS `open <url>`, other
  systems `xdg-open <url>`), started as a process with the URL as one
  argument, never through a shell. It is opened only when a desktop is
  there: never in an SSH session (`SSH_CONNECTION` or `SSH_TTY` set), and
  on Linux and other Unix systems only when `DISPLAY` or `WAYLAND_DISPLAY`
  is set. Without one, or when the handler cannot be started (a warning is
  logged), graphvein prints `Open <url> in your browser.` and keeps
  serving; a browser problem never stops it.
- **Every port taken.** When none of `127.0.0.1:7420` to `127.0.0.1:7430`
  can be bound (and the first is not a Graphvein server), graphvein says
  so on stderr, suggests `graphvein serve --addr 127.0.0.1:PORT` (or
  `--addr 127.0.0.1:0` for a port the system picks) and exits 1. When
  stdin is a terminal it first waits for Enter, so a double-clicked
  `graphvein.exe` window stays open until the message has been read.
- Stop it with Ctrl+C or by closing the window; the shutdown is that of
  `graphvein serve`.

## Project mode (`--project`)

`graphvein serve --project` serves one project
([ADR 0075](../adr/0075-multi-repository-projects.md) "Server and UI",
[project manifests](project.md)): one project per server, read from the
project store that `graphvein index --project` filled (SQLite
`<manifest dir>/.graphvein/index.db`, or `GRAPHVEIN_POSTGRES_URL` /
`GRAPHVEIN_SQLITE_PATH` as for `index --project`). `serve` on a repository
is unchanged. A path or `--repository` with `--project` is a usage error
(exit 2), as is a manifest that does not load. It prints

```
Serving project <name> (<n> members) at http://127.0.0.1:7420/
```

(with ` (+N in the workspace)` before ` at` when the workspace has N other
entries).

- **Every endpoint per member.** Each endpoint of HTTP API v0 below answers
  for one member, exactly as a single-repository server for that member
  would, and takes an optional `repository` parameter naming it. Without
  it, a project with one member answers for that member; with several
  members the request is ambiguous and answers 400 `repository_required`
  ("this server serves project P with members a, b; name one with
  repository="). A name that is not a member, or `repository` given twice,
  answers 400 `bad_request`. The schema endpoints keep their `editor`
  parameter (`/api/v0/schema/table?repository=api&id=...&editor=zed`).
- **Checkouts.** Source, IDE links and freshness resolve through the
  member's checkout path. A member without a checkout here (no path, or
  the folder is missing) answers every per-member endpoint from the
  stored index (T022 B9): nodes carry no file state (`file_state` null,
  never "missing") and no editor link (the `ide` status is unavailable
  with the error "member M has no checkout here; editor links need its
  files"), freshness is `unknown` with the error "no checkout here", and
  `graphvein.yaml` reads as absent (defaults; the server's own directory
  is never read in its place). Only code peek (`/api/v0/source`), a
  `ref:` base and a ref job (`POST /api/v0/changes/refs`) need its files
  and answer 409 `no_checkout`.
- **Project endpoints** ([below](#project-endpoints-serve---project)):
  `/api/v0/project`, `/api/v0/project/callers`, `/api/v0/project/impact`,
  `/api/v0/project/changes/impact`, `/api/v0/project/links/changes` and
  `/api/v0/project/suggestions` (GET and HEAD only), and
  `/api/v0/project/manifest` (GET, and POST for a manifest edit).
  `/api/v0/api/endpoint` of an HTTP route adds the cross-repository links
  into it (`project`), and `/api/v0/api/endpoints` counts them per route.
- **Re-index.** `POST /api/v0/index/run` re-indexes the whole project as
  `graphvein index --project` does: each member checkout under its own
  index lock, then the link pass under the project lock, never two locks
  at once. A `repository` parameter is refused (400). Members without a
  checkout are linked from the store. Body `{"link_pass_only": true}`
  runs only the link pass over the stored indexes (offered after a
  manifest edit that changed no member).
- **Manifest.** The server reads the manifest at start. A manifest edit
  from the UI (T022 B10, [below](#project-manifest-endpoints-serve---project))
  writes the file and reloads it: the server serves the edited
  declarations (and a renamed member) at once, without a restart, and the
  links are stale until the link pass runs. A rename also removes the old
  name's rows from the project store in the same request (below).
  Declarations edited on disk by other means (an editor, `git checkout`,
  `graphvein project add`) are served from the next request to a
  `/api/v0/project` path or to `/api/v0/status` (whose freshness walks the
  member's path, so a path changed on disk is checked at the new folder):
  the server compares the content hash of `graphvein-project.yaml` and
  `graphvein-project.local.yaml` with those it serves (two small reads per
  such request) and reloads on a change.
  A changed declaration makes every link stale (the stale-link notice);
  a comment or path change does not. A file that does not validate is
  logged and ignored, and the server keeps serving the manifest it had,
  until the file validates again.
- **Security** is that of a single-repository server: the same loopback
  bind, Host allow-list, Fetch Metadata and Origin checks, and the same
  method rule (POST only on the re-index, the writing change endpoints,
  per member, and the manifest edit). A manifest edit also needs the
  server's session token and a loopback peer (ADR 0075 "Manifest editing
  from the UI").

## Workspace (multi-index server)

One `serve` process serves every index of the workspace registry
([ADR 0087](../adr/0087-multi-index-server.md); the registry file:
[configuration](../config.md#workspace-registry); claim rows:
[workspace](../contract/claims/workspace.md)).

- **Entries.** An entry is a repository folder or a project folder
  (holding `graphvein-project.yaml`), with a stable `id`
  (`<name>~<8 hex digits>`, WS-4). One folder is one entry: serving or
  adding a folder already registered (after a rename too) uses the stored
  entry and its id; the name shown is re-read from `graphvein.yaml` (or
  the manifest) when the index opens. Each index is opened from its
  folder as the CLI opens it (default `<folder>/.graphvein/index.db`;
  `GRAPHVEIN_POSTGRES_URL` of the `serve` process applies to every entry).
  `GRAPHVEIN_SQLITE_PATH` names one file, so it applies only to the index
  named on the command line: while it is set, every other entry answers
  409 `single_store`.
- **One database for every entry.** With `GRAPHVEIN_POSTGRES_URL` every
  entry's index lives in one database, which tells repositories apart by
  repository ID and projects by name, not by folder. Two registered
  entries in different folders that use one name (a repository's, a
  project's, or a project member's with a path, whether or not its
  checkout is there) are both refused, since either could be answered
  with the other's facts. A project member without a path reads whatever
  the database holds under its name, so it conflicts in the same way with
  any entry that uses the name from a folder (two members without a path
  share it): the list carries
  `conflict` for each and no freshness, every per-index request answers
  409 `shared_store_conflict`, the background check skips them, a typed
  path that would add another user of the name is refused, and
  `serve <path>` or `serve --project` exits 1 before it serves. Give one
  folder another `repository:` in its `graphvein.yaml` (or the project
  another `name:`) and index it again, or remove one entry (WS-57).
- **Dispatch.** Every endpoint below takes `ws=<entry id>` and then
  answers exactly as a single-index server for that entry would (a
  project member still takes `repository=`). Without `ws` the request
  goes to the current entry: the index named on the command line, or,
  for `serve` without a path, the registered entry whose folder is the
  working directory. Without one, a per-index request answers 400
  `ws_required`. An unknown id answers 404 `unknown_entry`, never another
  index; an entry without a stored index 409 `no_index` (start its run
  with `POST /api/v0/index/run?ws=<id>`, which creates the store, or run
  `graphvein index`); an entry whose folder is gone 409 `folder_gone`
  (a request that may write, any method but `GET` and `HEAD`, also when
  the entry's index is already open: nothing is created at the folder).
- **Warm indexes.** An index opens on its first request and stays open,
  at most 8 at a time; beyond that the least recently used one without a
  request in flight or a running job is closed (the command-line index
  never is). A few GET views (`/api/v0/api/endpoints`, `/api/v0/precision`,
  `/api/v0/schema/summary`, `/sets`, `/assumptions`,
  `/api/v0/architecture`, `/architecture/chain`) are answered from a
  per-index cache while the index's latest runs, `graphvein.yaml` (and for
  a project the manifest files and the last link pass) are unchanged;
  the end of a re-index started by this server drops it. These answers
  carry `X-Graphvein-Cache: hit`, `miss` or `bypass`. Status, freshness,
  source, file states and editor links are never cached. Bounds: 64 MiB
  per index, 256 MiB in all, no answer over 4 MiB.
- **Background freshness.** After start, one entry at a time is checked
  as `GET /api/v0/status` checks it; afterwards, every 30 s, the open
  entries, and the entries that are not open whose check is missing (an
  entry added since) or older than 2 minutes. An add asks for a pass at
  once. A `GET /api/v0/status` for an entry (or a project member) records
  its answer as that entry's freshness at once. An entry shows no
  freshness until a check finished; a check that finds the folder gone
  drops what was recorded.

### GET /api/v0/workspace

```json
{
  "api_version": 0,
  "registry": {"path": "/home/dev/.config/graphvein/workspace.yaml", "error": null},
  "token": "Q7K2...",
  "current": "api~1f0c2b9a",
  "suggestion": null,
  "open_limit": 8,
  "entries": [
    {"id": "shop~7d41e0c2", "kind": "project", "name": "shop", "path": "/home/dev/src/shop",
     "registered": true, "current": false, "state": "not_open",
     "freshness": {"state": "stale", "checked_at": "2026-11-24T10:00:00Z", "changed_count": null},
     "members": [{"repository": "api", "path": "/home/dev/src/api", "checkout": true,
                  "freshness": {"state": "fresh", "checked_at": "2026-11-24T10:00:00Z", "changed_count": 0}}],
     "members_error": null, "job": null, "conflict": null},
    {"id": "api~1f0c2b9a", "kind": "repository", "name": "api", "path": "/home/dev/src/api",
     "registered": true, "current": true, "state": "open", "freshness": null,
     "members": null, "members_error": null, "job": null, "conflict": null}
  ]
}
```

- Order: projects first, then repositories, each by name, then path.
- `state`: `open`, `not_open` or `folder_gone` (the folder is missing,
  also while its index is still open; the entry stays until removed).
- `freshness`: `null` until checked, and for a folder that is gone; then
  the state of `GET /api/v0/status` with `checked_at` (when the check
  started) and `changed_count` (and `error` for `unknown`). A project's
  freshness is folded at each list over the members its manifest lists
  then: `null` while any of them has no recorded check (a member added
  since, or whose path in the manifest is no longer the one that was
  checked: an open project checks the paths it opened with until any
  request of that project reads the edited manifest), else `fresh`
  only when every member is, `stale` when any is.
- `members`: from the manifest on disk; `null` with `members_error` when it
  does not load.
- `job`: the entry's latest job of this server (as
  `GET /api/v0/index/run?ws=` reports it) while the index is open, read
  from the server's memory. The list stats each entry's folder and reads
  each project manifest; it never opens a store or probes a lock, so it
  never writes into an entry's folder.
- `name`: the registered name, or, while the index is open, the name it
  serves: the repository its `graphvein.yaml` gave when the index opened,
  or the name of the manifest the open project serves (the one it opened
  with, or the one a manifest edit or any request of that project last
  read from disk; every request of an open project reads the manifest's
  files first and serves an edited manifest from then on).
- `conflict`: `null`, or under `GRAPHVEIN_POSTGRES_URL` the name this
  entry shares with an entry in another folder: `{"code":
  "shared_store_conflict", "message", "kind"` (`repository` or
  `project`), `"name"`, `"entry"`, `"entry_name"`, `"entry_path"}` (the
  other entry); `freshness` and members' freshness are then `null`.
- `registered`: false only for the command-line index when writing the
  registry failed (`registry.error` says why).
- `suggestion`: the working directory, when it is not registered and holds
  a project manifest or a `.graphvein` folder (`{"path", "kind"}`).
- `token`: the session token of the POSTs below.

### POST /api/v0/workspace/check, /add, /remove

Typed folder paths (ADR 0041 revision). Each POST passes the security
checks of every state-changing POST and also needs a loopback peer (403
`not_loopback`), the session token in `X-Graphvein-Token` (403
`token_required`), `Content-Type: application/json` and one JSON object
of at most 4 KiB with only the fields below. GET and other methods get 405.

**check** `{"path": "/home/dev/src/miniflux"}` reads nothing but file
metadata, `graphvein.yaml`, `graphvein-project.yaml` and the folder's store
(opened read-only), and answers 200:

```json
{
  "api_version": 0,
  "path": "/home/dev/src/miniflux",
  "resolved": "/home/dev/src/miniflux",
  "outcome": "ready",
  "refusal": null,
  "kind": "repository",
  "name": "miniflux",
  "members": null,
  "index": {"state": "absent", "last_run": null, "error": null},
  "store": {"kind": "sqlite", "path": "/home/dev/src/miniflux/.graphvein/index.db"},
  "entry": null,
  "confirm": "M4X...",
  "confirm_expires_at": "2026-11-24T10:10:00Z",
  "command": "graphvein index '/home/dev/src/miniflux'",
  "contains": []
}
```

- `outcome`: `ready` (with a one-time `confirm` value, valid 10 minutes),
  `registered` (the folder is already an entry: `entry`, no confirm) or
  `refused` with `refusal: {"row", "code", "message", "entry"}`. Codes:
  `path_not_absolute` (`~` and variables are not expanded),
  `path_not_found`, `not_a_directory`, `filesystem_root`,
  `system_directory` (the operating system's own folders: `/tmp`, `/etc`,
  `/dev`, `/proc`, `/sys`, `/run`, `/var`, `/var/lib`, `/usr`, `/bin`,
  `/sbin`, `/lib`, `/lib64`, `/boot` themselves and anything inside
  `/dev`, `/proc` and `/sys`; on Windows the system drive's `Windows`,
  `Program Files`, `Program Files (x86)` and `ProgramData`, also spelled
  with a `\\?\`, `\\.\` or `\\localhost\C$\` prefix, trailing dots or
  spaces, or as an 8.3 short name), `home_directory`, `config_directory`,
  `inside_index_store` (a `.graphvein` or `.git` folder),
  `inside_registered` (with `entry`),
  `invalid_manifest`, `invalid_config`, `shared_store_conflict` (under
  `GRAPHVEIN_POSTGRES_URL`, a repository or project name an entry in
  another folder uses, with `entry`), `path_unreadable`, `registry_error`.
- `resolved`: the absolute folder with symbolic links resolved; the
  confirmation names it and add registers it.
- `kind`/`name`/`members`: a folder holding `graphvein-project.yaml` is a
  project (members with their paths and whether each checkout exists);
  any other folder a repository named by `graphvein.yaml` or the folder.
- `index.state`: `present` (runs stored; `last_run`), `absent` (no store,
  or no run in it) or `unknown` (`error`).
- `store`: where `graphvein index` writes the index (`postgres` names the
  variable `GRAPHVEIN_POSTGRES_URL`, never the URL).
- `command`: the terminal command that indexes the folder (single quotes,
  for bash and Git Bash).
- `contains` (with `ready`): the registered entries inside the folder whose folder exists (a moved one is left out),
  `[{"id", "kind", "name", "path"}]`, empty when none. A folder above
  registered ones (a monorepo) may be added; its index reads their files
  again into a store of its own, and the confirmation names them.

**add** `{"path": "<resolved>", "confirm": "<value>", "index": true}`
uses the confirm value up (whatever the outcome), checks the folder again
and registers it. With `index` true (the default) it opens the folder's
index, creating an empty store where `graphvein index` would, and starts
its run as `POST /api/v0/index/run?ws=` does (a project: the project
re-index), answering 202 `{"api_version": 0, "entry": {...},
"added": true, "job": {...}}`; poll `GET /api/v0/index/run?ws=<id>` for
progress. With `index: false` it only registers (200, `job` null); the
entry is checked at once and its run can be started later with
`POST /api/v0/index/run?ws=<id>`. Once registered the entry stays: when
its index cannot be opened or the run is refused (`index_locked`,
`reindex_unsupported`, `index_lock_unavailable`, `single_store`,
`store_error`), the add answers 200 with the entry, `job` null and
`"run_error": {"code", "message"}` (null otherwise). 409
`confirm_required` (no, used, expired or another folder's value) and 409
`path_changed` (a rule now refuses it, or its kind or name changed)
register nothing. There is no cancel; stopping `serve` cancels a running
job.

**remove** `{"id": "<entry id>"}` drops the entry from the registry and
closes its index; the folder and its index are never deleted. 409
`entry_in_use` (the index named on the command line), 409 `index_running`,
404 `unknown_entry`.

## Security behaviour

- **Loopback only.** The listener is opened by `egress.ListenLoopback`
  (ADR 0040). Cleartext HTTP/1.1; no TLS, no HTTP/2.
- **Host check (DNS rebinding).** With the server bound to `H:P`, only the
  Host values `127.0.0.1:P`, `localhost:P`, `[::1]:P` and the bound `H:P`
  are accepted, compared ASCII case-insensitively. Everything else, such as
  a Host without port, another port, `localhost.:P`, any other name or an
  empty Host, gets 403 `host_not_allowed`.
- **Methods per path.** Every path takes only GET and HEAD, except
  `/api/v0/index/run`, which also takes POST (start a re-index), the
  writing change endpoints, the project manifest edit and
  `/api/v0/explorer/path-values` and `/api/v0/config` (POST: guarded
  edits of `graphvein.yaml`), and `/api/v0/workspace/check`, `/add` and
  `/remove` (POST only; [workspace](#post-apiv0workspacecheck-add-remove)). Other
  methods, including OPTIONS, get 405 `method_not_allowed` with
  `Allow: GET, HEAD` (or `Allow: GET, HEAD, POST` on the re-index path,
  `/api/v0/tryit/environments`, `/api/v0/explorer/path-values` and
  `/api/v0/config`; `POST` on the change POST paths and
  the try-it POST paths) and a message naming the allowed methods. No
  CORS headers are ever sent, so a cross-origin request that needs a
  preflight never runs.
- **State-changing POST.** After the Host, method, `Sec-Fetch-Site` and
  `Origin` checks below, a POST also passes Go's
  `http.CrossOriginProtection` (403 `cross_site_request`): it rejects
  `Sec-Fetch-Site` other than `same-origin` or `none`, and, without that
  header, an `Origin` whose host is not the request's Host (an old browser
  on `http://localhost:P` posting to `127.0.0.1:P`). A request with neither
  header (curl, scripts) passes. Then `Content-Type` must be
  `application/json` (415 `unsupported_media_type`), so an HTML form or a
  `text/plain` "simple" request can never start a run, and the body must be
  exactly `{}` (400 `bad_request`).
- **Cross-site API calls.** Requests to `/api/` with `Sec-Fetch-Site:
  cross-site` or `same-site` get 403 `cross_site_request`, so other web
  pages cannot trigger freshness checks. `same-origin`, `none` and a missing
  header (curl, scripts) are allowed. For browsers without Fetch Metadata,
  a present `Origin` header must also be one of the server's own origins
  (`http://` followed by an allowed Host value, compared ASCII
  case-insensitively); any other value, including `null` or an empty value,
  gets 403 `cross_site_request`. A missing `Origin` is allowed.
- **Headers on every response**, errors included:

  ```
  Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'
  X-Content-Type-Options: nosniff
  Referrer-Policy: no-referrer
  X-Frame-Options: DENY
  Cross-Origin-Opener-Policy: same-origin
  Cross-Origin-Resource-Policy: same-origin
  ```

- **No authentication.** Any process on this machine can read the API, as
  it can read the store file.

## Web UI

`GET /` and `GET /index.html` serve the UI page (`Cache-Control: no-cache`).
Files under `/assets/` have content-hashed names and are served with
`Cache-Control: public, max-age=31536000, immutable`; other files at the
root get `no-cache`. Any other path is a plain-text 404; there are no
directory listings and no fallback to `index.html`. The UI uses hash routing
(`/#/api`), so its own paths never reach the server.

The UI is a shell for the views to come: a sidebar with the sections (API,
Explorer, Architecture, Schema and Changes; Review shown as "Soon"), and a top bar
with the repository, the compare chip, the freshness badge, a "Check"
button and the extractor version. The top bar follows its own width: one
row with every label from 1300 px; one row from 980 px, where the badge's
time and Check's label move into their tooltips and accessible names; two
rows of fixed height below (breadcrumb and status controls, then the chip
and the extractor badge). Every part reserves its widest state, so no state
moves the bar; a long base name is cut with an ellipsis and the chip's
tooltip names it in full. It requests the status on load, every 30 s while the tab is
visible, when the tab becomes visible or gets focus, and on "Check" ("Check"
re-requests the status; it does not re-index). Only a successful answer
whose state is `fresh` shows "Fresh"; `stale`, `not_indexed`, `unknown`, any
state the UI does not know and a failed request ("Status unavailable") use
the high-contrast warning style, and a failed request replaces the previous
result. An extractor mismatch (`extractor.matches` false) is shown on the
extractor badge as "re-index recommended"; it does not change the freshness
state. `/third-party-licenses.txt` lists the licences of the software in the
UI bundle. The sidebar footer shows the extractor and server versions of
the last successful status answer only: skeletons until the first answer
(and while a withdrawn answer is re-checked), "unavailable" after a failed
request; no value is kept from an earlier answer. Sources and the rebuild
workflow: `web/README.md`.

The Explorer section (`#/explorer`, T029, ADR 0081 "Explorer UI") is
API documentation built from `GET /api/v0/openapi` only: operations grouped
by router file or by the tag of the committed spec operation that
documents them, collapsible, with method-coloured rows; an expanded
operation shows the handler, path markers (settings default, set by you,
conditional, may be shadowed, equivalent registrations, depth cap reached),
parameters, request body, responses, security and auth as written, each
item with a provenance chip (Fact, Read in handler, Intended, Returned,
Mapped, with file:line) and each section with the state the document
proves ("Not read by Graphvein", "None, proven" with its evidence,
"Partial: more may exist" with the reason). A chip whose file:line has a
link in the route's `shape_links` (the Explorer requests `GET
/api/v0/api/endpoint` for the route of an operation when it is opened,
once per route and editor; the document itself carries no editor link)
ends in a small icon-only button with the editor's mark, "Open
<file>:<line> in <editor>". From the same response, an opened operation
lists under "Linked calls" each linked call (in project mode also each
cross-repository caller, labelled "cross-repository link") whose request
differs from what the route reads, with the same "Request differences"
as the Flow tab's line detail; it lists nothing for calls that were not
compared or agree. Routes with an unknown path
part are listed below with the part marked and "Enter the missing part",
which edits `explorer.path_values` through `POST
/api/v0/explorer/path-values` (preview with diff, then apply). The Spec tab
shows the document as YAML or JSON and downloads it. A header, query
parameter or cookie a security scheme of the operation reads is shown in
its section as "read by the security scheme (see Security)", and such a
section never reads "None, proven" ("None besides the security scheme").

The API section's route detail shows the same sections for one route in
its Overview tab, under "Request and response": the endpoint's `shape`
(`GET /api/v0/api/endpoint`) with the Explorer's provenance chips and
section states (each chip with its editor button from `shape_links`), the
sections not read named in one "Not read by Graphvein" line, and "Open in Explorer" (`#/explorer?route=<route identity>`), which
opens the operation the document writes for that route or says why there
is none. In project mode both are the selected member's. When a model of
the shape is also a database table (`model_tables`), the block notes "The
model X (file) is also the database table Y", naming the class and its
file, with a link to the table in the Schema view.

"Try it out" sends requests through the try-it endpoints below. An
environment bar above the operations shows the selected environment (its
label, also on every operation while selected, its base URL and where its
auth comes from), lists the environments and the base URL suggestions
(source and file:line, used only when picked) and adds one from a typed
URL through the guarded edit (preview with diff, then "Allow and save").
Switching to a production-labelled environment shows one warning, then a
production marker. In an expanded operation the builder takes path
values, query and header rows pre-filled from the operation's items, a
body (raw, a JSON template marked as a template, or multipart fields and
files) or a free-form path; GET and HEAD are sent directly outside
production, every other request shows the exact request and the reasons
in place of Send ("Don't ask again for this environment until I switch").
Missing auth, an untrusted or changed certificate, a limit and a redirect
to another origin each open a dialog with the user's options; the
response shows status, time, sizes, redacted headers and the body (pretty
JSON). "Copy as curl" writes the auth as `$NAME`, never a value. The rows
are OX-1..OX-49 of the [try-it claim contract](../contract/claims/try-it.md).

The Schema section (T018f R2) shows one card per source group in its set
strip (migration sets and model groups), a dashed card for a source the
index proves was read and that holds nothing, and one per `unread_tools`
entry ("Alembic migrations found, not read yet"); a "Rests on" line on the
group (`rests_on`, a union: "Facts in <group> rest on") and on the table
(an intersection), row chips where a record's claims rest on more than its
table, and per-fact "why" popovers (contract row, assumption texts, the
source line as an IDE link under the editor choice) that open on click or
Enter, close on Escape and are placed with `position: fixed`. A group
card shows the group's precision chip; beside a Precise model group's chip
a "dialect unknown" or "mapped: unverified" note says that some of its
facts are not checked against a database (`docs/extract/precision.md`). A
model table's detail has "Routes that accept or return this model"
(`model_routes`): each route with its method, path, whether it accepts or
returns the model (and where: request body, a parameter, a response
status, inside another model), the use's provenance chip and links to the
route in the API section and in the Explorer. "No route of this repository uses this model" (in project mode, naming
the member) appears only when the server proved it; otherwise the section says how
many routes were not fully read, or that route shapes were not read. The
Assumptions tab (`#/schema?tab=assumptions&a=E2`) lists
`/api/v0/schema/assumptions`. Below 600 px the sidebar is its icon rail
and popovers are bottom sheets.

The Architecture section (`#/architecture`, T026, combination
X+) draws `GET /api/v0/architecture` as a map (React Flow on the ELK
worker, ADR 0046; the sidebar is its icon rail): one card per shown
component (kind, manifests, counts, deploy markers as text, calls with no
link as a dashed chip), one card per schema set, a folded "Workspace
libraries" card and the tests, docs and configured components behind the
Show toggles (off by default; Tests also requests `scope=all`), one muted
dotted SDK stub per component (on by default), and a Hidden note with what
the toggles leave out. Links between one pair of cards are bundled into one
line whose chip shows the families, the count and the fixed three-cell
glyph protocol | auth | TLS (proven filled, "as written" / documented
olive, mixed half, n/a grey, unknown hatched). Selecting a card or chip
dims the rest and fills the inspector (full strip, each cell's value and
source, tiers, evidence members with IDE links from
`/architecture/component`); "Expand in place" lists a component's routes,
calls, commands and tables by file (a filterable, scrolling list above 12
rows, one bundled line into its header) and a row opens the code peek. The
breadcrumb shows level tags L1-L4; a minimap shows above 12 cards; the
Links tab lists every link with per-cell sources, the SDK stubs, the
documents counts and the Not proven list (compose services mapped to no
component in one collapsed "Deploy services not tied to a component (n)"
item with each reason inside; a service whose database image AE2 ties to a
database card, and same-name services of its directory that write no
image or build of their own, are noted there, not listed; an entry of a
component hidden by default says "in hidden <docs|test|configured>
component <name>"; "may also use" names the database card), with a CSV of
the rows shown. A card's deploy line shows its AT2 ingress ("TLS as
configured", never the link's TLS) and its markers; the inspector shows
each marker's `basis` and each ingress with its router settings as
written. In project mode (TOP-2 of 2026-11-18) another member reached by the
stored project links is a muted dashed card "<repo> · other repository";
a component's calls the pass links there read "n linked across
repositories" with "repository alone: m not linked" as secondary text
(never the "not linked" chip), the line's inspector and Links tab row show
the stored assumption, and a chain crosses into that card as far as the
stored link ("continues in <repo>").

Selecting a component card, an expanded row or "Chain from" on a chain
member draws its chain in place (T026, from
`/architecture/chain`): the crossed cards open to the chain's members only
(one column per step, numbered top to bottom over both directions), lines
inside a card join two members, lines between cards are bundled per card
pair with their wording and tier, and the rest of the map is dimmed. The
members the server's budget hint leaves out fold into "+ n more at step k"
per card (members of a hidden card fold at the root); "Open in Flow tab"
opens the Flow tab for the start route or command (else the selected or
nearest one). A component start numbers the cards and lines of its CE
links instead. The inspector's top block is the evidence chain: each step
with the stored edges into it (wording and tier), its members with their
source, CH11 end lines under the member, CH12 cap notes, CH10 hidden
counts and the step's map folds, every member within the caps (25 per step
before "Show all"); a member selected there or on the map is one
selection with its code peek, arrow keys walk the chain and Esc clears it.
Beside the direction switch, a depth switch (1 to 10, T042) sets how many
steps the chain follows each way; see [Depth defaults](#depth-defaults).
"Show in Links tab" filters the ledger to the links the chain crosses.
Wording goes through the label table (rows 31-53, both styles). State
lives in the hash (`tab`, `show`, `exp`, `node`, `edge`, `item`, `chain`,
`dir`, `depth`, `at`).

**Re-index** (T016j, variant D) sits right of "Check" in a fixed-width slot
(the badge reserves its widest label too, so nothing in the top bar moves
between states). Idle it starts a run (`POST /api/v0/index/run`; filled
while the badge is Stale or Not indexed). While the index lock is held the
same button shows the run and opens its details (click, Enter or Space;
Escape or Close closes them): "Indexing 212 / 457" with a bar only when the
server sends `progress`, otherwise "Indexing" with an indeterminate bar;
"Indexing (CLI)" when another process holds the lock; "Re-index failed ·
details" (code, message, the index still shown, Dismiss, Retry) until
dismissed or the next run; a disabled-looking "Re-index" with the reason
when the lock state is unknown. The UI polls `GET /api/v0/index/run` with
the status every 30 s and every second while the lock is held. The badge
keeps describing the shown index during a run and never says "Fresh" unless
`lock.state` is `free` (a fresh index shows "Indexed T" meanwhile); after a
commit it says "Indexed T', checking freshness..." until a status answer
names the new run, then the views reload and a dismissible note says "Now
showing the index of T' (was T)". There is no cancel control: stopping
`serve` cancels the run.

The API section (`#/api`, ADR 0042) reads only the endpoints below and the
polled status. It shows the coverage per transport (cards, or a compact
strip below 1400 x 820 px) with every source root of the calling sites, the
endpoint list (HTTP and Tauri IPC groups, windowed) and the selected
endpoint's handler, OpenAPI operations and callers. Default scope is
production; "Include tests" switches to `all`; the source-root filter sets
`root` on every request. Only the exact status `linked` is shown as linked;
statuses the UI does not know are shown raw under "Other". Callers are only
the stored edges; near misses are listed apart; dynamic calls appear only
in the gap sheets, which list the calls behind each non-linked count (and
the endpoints without a linked caller) with search, grouping by reason, file or root,
and "Copy list". When the polled status names another last run than the
loaded data, the view requests everything again; a stale status adds a
banner and every changed file is marked; `index_changed` is retried once.
A list of source roots whose chips would not fit on one line (cards and
strip, measured from the rendered chips) folds to one line: the largest
roots by call count that fit (ties in the list's order), the active root's
chip when a root filter is set, and a "+N more roots" chip that unfolds
the full list in its usual order; "Show fewer" folds it again. The choice
is one per browser for every roots list (`localStorage`
`graphvein.api.rootsExpanded`, exactly `1` = unfolded); a list that fits is
shown unchanged. Root chips filter like the Source root select.
The view state is in the hash:
`#/api?transport=&tests=1&root=&sel=<id>&gap=<transport>:<category>&peek=<id>`. IDE
buttons open only links the server built (`ide.link` of each presented
node); the UI never builds or rewrites a link. Each button's label is the
response's `ide.label`, else the link's scheme (`vscode`, `vscode-insiders`,
`cursor`, `vscodium`, `vscodium-insiders`, `idea`, `zed`), else "Open in
editor"; it starts with the editor's mark, and its title and accessible
name add the source in words from `ide.source` ("Zed, your choice in this
browser", "VS Code, the repository default from graphvein.yaml", "VS Code,
Graphvein's default (graphvein.yaml sets no editor)", "custom link from
graphvein.yaml, not verified by Graphvein"). A preset that is not
`verified` carries a visible warning (partly verified) or info (limited)
icon on the button and its note in the title and name ("Open in Cursor,
your choice in this browser (partly verified: ...)"). A response whose
`ide.editor` is not the requested editor (an older server ignores
`editor=`) is named as what it is, with "This server ignored your editor
choice". A link the server did not vouch for adds an info icon (unless a
warning already shows) and a note to its title and name instead of the
changed-file note: `ide.caveat` `tab_size_unknown` (IntelliJ IDEA after a
tab, line only) "opens the line only: a tab precedes the node and the
editor's tab size is not known"; `column_unit_assumed` (Cursor) "column
unit assumed from VS Code"; `ide.line_only` without a caveat (IntelliJ
IDEA on a changed file) "opens the line only: the file changed since
indexing; the line may have moved"; any other caveat "the column may be
off".

The compare chip (T019b, ADR 0056) sets the base for the Changes section
and for every view's "changed since" markers: the previous run (default),
a journal run, a pin or a branch (`ref:<base>[..<head>]`). The choice and
an on/off state are kept in this browser (`localStorage`
`graphvein.compare.v1`); one click on its "×" turns comparing off. The chip
shows the change count of `/api/v0/changes/summary` for that base, or the
refusal (`base_expired`, `base_unknown`, `reindex_needed`,
`git_unavailable`, ...) when the server refuses it. Its menu pins the
latest run (`POST pins`), unpins, and starts a branch comparison
(`POST refs`, then `GET refs` once a second until the job ends; the base
switches when it has committed). The Changes section (`#/changes?group=
&change=&q=&tests=1&sel=&tab=change|impact|raw&depth=&pane=`) shows the
summary of both sides with its banners, the groups in the sidebar under
"Changes" (`not_extracted` groups read "not extracted", never 0), one
group's items and one item with its Change, Impact and Raw tabs. Item
links to the editor are the head side's `after.ide` only; the change
endpoints take no `editor=` parameter, so a browser editor choice is not
applied there (the link says so). Below 600 px a group select replaces the
sidebar list and one pane shows at a time, with a back step.

"Changed since" markers (T019b part 2) follow the chip's base: on the API
section's endpoint rows (`routes` and `ipc` items whose identity is the
row; endpoints the base had and the current index lacks are counted in the
list footer), on Flow cards (the items of every identity a card stands for,
and the `calls_route` or `invokes_command` items that leave it), on Schema
tables, columns and migration rows. They read `/api/v0/changes/items`
(every page of each group the view needs, `tests=true`), once per resolved
comparison: the summary's index run, both sides (run, pin, commit) and the
base's entry in `/api/v0/changes/bases` (a pin's id and run, the ref
snapshots), which reloads with every status answer, so a re-run branch
comparison or a pin deleted and pinned again from the CLI reloads the
markers, the list and the impact panel. An items or impact answer read
from another index run than the summary is not shown: the page checks the
status, reloads the summary and shows loading meanwhile. Markers show only
when the comparison's head is the current index (the views draw the current
index); for a branch comparison whose head is another commit the chip says
"markers off: this comparison's head is not the current index". A marker
names the change in the Changes section's words ("paths changed, handler
changed": every label and changed field, the full text in its tooltip;
"body changed", "added", "edited (history rewritten)");
`unknown now` and `known now` keep their own dashed style and are never
worded as changed, added or removed. A marker opens its item in Changes
(`#/changes?group=&sel=&pane=item`); inside a list option, which may not
hold a control, it is a decoration that opens the item on click, and the
detail header repeats it as a link. On an endpoint row it follows the path
and becomes a dot in a narrow list, so it never hides the path; the list
footer's count of endpoints no longer in the index honours "Include tests".
With comparing off, or a base the server refuses, no view shows a marker.
Branch comparisons read "main..feature", or "main..current index" without a
head ref, in every state; the menu and the summary name the merge base
commit. The impact panel shows each
`assumes` id of an I8 step with its catalogue title and text; the title
chip opens the Schema view's popover.

Routes in the Changes section (T025) are named by their full composed
path with router prefixes, as the API section shows them: a route item
from its side's stored `paths`, an impact path from `route_paths`; a route
mounted at several paths shows the first production path and "1 of N
paths". Every full path (here, and in the API section's list, header,
composed paths and Flow cards) carries the served trailing `/` of an entry
with `trailing_slash: true`; entries differing only in it are shown as
distinct paths. A near miss names its route the same way when the seed item
or a loaded impact path gives its paths, else marks it "path as written".
A route without a full path but with a path whose prefix the code does not
fix (T033 row UP-29) shows that path with `…` for the prefix followed by
"prefix unknown", the reason in its tooltip (from the item side's
`unresolved_prefix_paths`, or an impact path's `prefix_unknown_paths`);
never as a full path. An impact path through a `calls_route` link with
assumptions lists them ("assumes UP-V1 · UP-V2", each text in its
tooltip). The API section's routes list, endpoint header, Flow cards and
the Architecture chain, links and inspector show such a path the same way;
the endpoint header lists every unknown-prefix path with its reason and
hop, and Flow edges, chain steps and link members list their link
assumptions.
A route without a composed path shows its path as written in its
file followed by the API section's words: "shadowed" (`shadowed_paths`
or `route_shadowed`), "path withheld" (the side's `path_withheld` or the
impact path's `route_withheld`: a registration whose path the Go linker
withholds, couldn't-read row CR-44), else "path not known"; never "not
mounted", which no stored fact proves (ADR 0089 Revision 5). One whose
response carries no composed paths shows it followed by "path as
written". The path as written is always in the tooltip. Internal ids and
raw kinds are not the default text (T025): contract and impact row ids
(C3, I4) and stored edge kinds are in tooltips, edges read "Fact · 1.0",
"Exact path match · 0.9" or "Path suffix match · 0.6" (another edge form
keeps its stored fields), provenance reads "Fact · 1.0" or
"Inferred · 0.9" (just "Fact" or "Inferred" where the answer has no
confidence), and an assumption chip reads its catalogue title with the id
in its tooltip. A confidence is a fixed rank per match rule, not a
probability, and is printed as stored (1 as 1.0, 0.85 as 0.85). This
is the plain label style, the default; the technical style shows the ids,
tiers with confidence numbers and raw kinds instead ([Labels](#labels)).

### Project mode (serve --project)

Under `serve --project` (T022 B9, ADR 0075 "UI view") the page first
asks `GET /api/v0/project`; a single-repository server answers 404 and the
UI is exactly as described above. In project mode:

- **The project lives in the sidebar** (T022 B9, reshaped in unit
  MR-UI by the developer's decision of 2026-11-19): there is no second
  bar, and the top bar is the single-repository one. The sidebar's
  "Repository" section is the project's **member list**: the project's
  name at the top (a link to the Project view), one row per member with
  its freshness dot (green fresh, amber stale or not indexed, hollow for
  a member without a local checkout; the tooltip says which), the
  selected row highlighted, and the link status line ("links fresh ·
  pass 2 · 27 links"; stale: "links stale · pass 3: re-index the
  project", warning style, its reasons in the tooltip; "no link pass
  yet"), which links to the Project view. Clicking a row switches the
  repository: every section shows one member, and every request names
  it with `repository=` (not the project document, nor the project
  re-index). Without a choice the first member is shown and the note
  "shown by default: choose a repository" sits under its highlighted
  row; a choice is kept per project in browser storage. Switching
  reloads every section. "Project" is the first entry under Sections. In
  the collapsed sidebar (icon rail: graph views and windows under 600
  px) a repository icon (a dot when the selection is the default or the
  links are stale) opens a popover with the same member list and link
  status. Wording through the label table (rows 28 to 30: link status,
  member freshness, the default note; both styles).
- **Project view** (`#/project`, also in the sidebar): members (ID
  source, checkout and remote, freshness, index time, links and stale
  links, index errors), the links summary with the pass's outcome counts,
  the declarations or the "no declarations" explanation, the ambiguous
  sources grouped by candidate providers with an explanation per claim
  row (never a link), the stale-link notice with its reasons, and the
  stored links flagged "conflicts with declaration". Below them,
  **Edit declarations** (T022 B10): the consumes entries of every member
  and link scope with their selectors (change the selector, default scope
  only, remove), a form to add an entry (consumer, provider or its scope or
  default scope, selector lists), each member with its link scopes
  (remove) and Rename (only for a member whose name the manifest sets; a
  name from the member's own `graphvein.yaml` wins and is shown as such),
  a form to declare a link scope (member, name, folder inside the member),
  and Suggestions read on demand (proposals with their evidence, file:line
  and row; Accept). Every edit first shows a preview of
  `graphvein-project.yaml`: what each operation does, the unified diff,
  validation problems (an invalid result cannot be written) and what makes
  the links follow; only "Write graphvein-project.yaml" writes it. After a
  write the view offers "Run the link pass" (no member changed) or
  "Re-index the project" (a rename). Adding a member by folder stays in
  `graphvein project add`; no form takes a member path.
- **API section**: the Callers tab lists "Callers in other repositories"
  (repository badge, the assumption, "Inferred across repositories" or
  `inferred 0.7`, stale marking with reasons) and the calls of other
  repositories left ambiguous with the route; a route without callers in
  its repository but with project callers is not shown as "no linked caller";
  the endpoint list shows "+N" callers from other repositories; the Flow
  tab names project callers above the canvas without drawing them; the
  gap sheet says when a call is linked or ambiguous across repositories.
- **Client-call counts are the project's** (T022 MR-UI): on the API
  coverage cards, the summary strip and the per-root rows the linked
  percentage reads "linked in the project" and counts calls linked across
  repositories (their own cell, "Linked across repositories", in the
  brand colour, after "Linked"); "Unmatched in the project" excludes
  them, and "Ambiguous across repositories" counts calls several other
  members could serve. The repository's own figures are the secondary
  line ("Inside react alone: 0% linked · 27 unmatched · ..."; the strip's
  percentage tooltip). Each cell's gap sheet lists the project-scope
  calls. The endpoint detail's dynamic and wrapper notes read the same
  counts.
- **Changes**: the Impact tab uses `/api/v0/project/changes/impact` (a
  `ref:` base keeps the member endpoint) and labels a path that crosses
  repositories ("crosses from express into react") with the link's
  assumption and the reached node's repository badge.
- A member **without a checkout** shows "no local checkout here" above
  the section; its sections answer from the stored index without code
  peek, editor links or freshness.

### Editor for links

The editor of IDE links is a per-browser choice (T016k, ADR 0050). The
control is the "This browser" row of "Editor for links" in the first
section of the Settings page, "Editor" (`#/settings?at=editor`; Settings is
linked at the bottom of the sidebar and of the icon rail; T039 moved it
there from the sidebar footer, with the same storage key): the editor's
mark, a source marker and the editor's name. When `graphvein.yaml` sets
`ide.link_template`, the same section shows it read-only with its line
(edited in the YAML view). Every Settings section and key row has a stable
anchor, `#/settings?at=<section>` (`editor`, `labels`, `indexing`, ...) or
`#/settings?at=<key>` (`ide.editor`).
The marker is a filled dot for your choice in this browser, a ring for the
repository default from `graphvein.yaml`, and absent for Graphvein's
built-in default (`vscode` when `graphvein.yaml` sets no `ide`); a badge
marks a caveat (amber: partly verified, not saved, saved choice ignored,
custom template; red: config error; grey: limited). On a monogram the
marker and badge sit on the tile's corners; next to an official logo (VS
Code, IntelliJ IDEA, Zed) they sit in a column beside the tile, outside the
logo and its clear space, as the brands' terms require. The control's
accessible name and its focus or hover tooltip say the same in words, for
example "Editor for links: Cursor, your choice in this browser. Partly
verified: ...".

Activating the control (click, Enter or Space) opens a popover with one
radio row per option: "Use repository default <editor>" with its source
line (shown when checked) or the load error (always), the six presets in
table order (the name, at most one short muted line, the preset's
`short_note` such as "macOS only", and beside the name, for a preset that
is not verified, a status icon: amber warning = partly verified, grey info
= limited; none when verified. The icon is a tab stop named "About
<editor> support"; its full note opens in a small popover on hover,
keyboard focus, click or tap, never selects the row, and Esc closes the
note before the picker; no long note is shown inline), and "No editor
links" (every IDE button stays, disabled, with the reason; its line shown
when checked). Notices above
the list report a config error, a saved choice this server does not offer,
blocked storage and a failed preset list. A footer legend explains the
icons and markers. Selecting applies at once and the popover stays open.
Keyboard: Enter or Space on the control opens it with focus on the checked
row; Up, Down, Home and End move; Space or Enter selects; Esc closes and
returns focus to the control. There are no global shortcuts.

First-click chooser: while this browser applies no choice and
`graphvein.yaml` loads and sets no `ide` (no editor, no `link_template`;
`GET /api/v0/ide` reports `repository.source` `builtin`), the first
activation of any editor link (click, Enter, the `o` shortcut) does not
navigate. A popover anchored to that link offers "Open with" and the six
presets (the picker's marks; under a preset that is not verified one short
muted line, its `short_note` such as IntelliJ IDEA's "macOS only", and
beside its name a status icon, amber for partly verified and grey for
limited, whose full note opens on hover, keyboard focus, click or tap and
never selects the editor; Esc closes that note before the chooser), a checked "Remember for this browser" and "More in
Settings" (`#/settings?at=editor`). Picking an editor stores it as the
picker does (same key; with the box unchecked, for this page only, and the
next page load asks again) and opens that link in the picked editor: the
view asks the server again with `editor=` and the rebuilt link opens once
it arrives (at most 8 seconds later; nothing opens after that). Up, Down,
Home and End move in the list; Enter or Space picks; Esc or a click
outside closes it without opening anything and returns focus to the link.
Once any choice exists (this browser's or the repository's) links open
directly. Jump-to asks the same way: opening a file or symbol in the
editor (Enter, Ctrl+Enter, a click) shows the chooser anchored to its row
while the palette stays open; the pick asks `/api/v0/search` again with
`editor=` for that result and opens its link (at most 8 seconds; otherwise
the palette's status line says so), and Esc closes the chooser first,
then the palette. A project caller's link follows the same choice. The
Changes section's head link, which the browser choice does not build,
never asks. Every editor link's tooltip starts with "Open in
<editor> · change in Settings", or "Choose an editor" while the chooser
applies.

Precedence for every link and label: (1) the browser's choice, a stored
preset that `GET /api/v0/ide` lists, sent as `editor=` and winning over the
repository's `ide` including a custom `link_template`; (2) the repository
default (`graphvein.yaml`'s `ide`, or the built-in `vscode`, shown as
"default"); (3) no link: when `graphvein.yaml` fails to load and there is no
browser choice, or when "No editor links" is chosen. A browser choice still
builds links when `graphvein.yaml` fails to load; the error stays visible
in the picker and on the control.

Storage: `localStorage` key `graphvein.editor.v1` holds a preset name or
`off`; no key means "Use repository default" (choosing it removes the key).
A stored value the server does not list (or longer than 64 characters) is
ignored and kept, the control shows "saved choice ignored", and choosing any
option overwrites it. With storage blocked the choice applies to the page
only ("not saved"). Other tabs follow a change. Data requests wait until
`GET /api/v0/ide` has answered; if it fails, no `editor` is sent and the
repository default applies, except that a stored `off` still holds (no
links; the control says "No editor links" and the failure). localStorage belongs to the origin, so the
choice is per browser profile and per server address: `127.0.0.1:7420` and
`127.0.0.1:7421` keep separate choices, and one address serving different
repositories shares it, as intended for a personal setting. Other browsers,
other addresses and the CLI keep using the repository default.

Editor marks: the official logos of VS Code, IntelliJ IDEA (24 px and
larger only; buttons show its monogram), Zed (full black, full white on
the dark theme) and Cursor (the 2D cube, its light-theme or dark-theme
file), under their brand conditions; neutral monograms for VS Code
Insiders, VSCodium and custom templates. The attribution is on
`/third-party-licenses.txt` only.

### Labels

How the UI words ids, kinds and confidence numbers is a per-browser choice
(T025b, ADR 0078), stored like the editor. It is the "This browser" row of
"Label style" on the Settings page (moved from the sidebar footer by T039,
same storage key `graphvein.labels.v1`): Not set / Plain / Technical. "Not
set" removes the stored key, so the repository's `ui.labels` (or plain)
applies again.

- **Plain** (default): T025's words with the confidence beside the rule
  name (ADR 0078 Revision 1). "Exact path match · 0.9", "Fact · 1.0" or
  "Inferred · 0.9", "client call", an assumption's catalogue title; rule
  and row ids (I4, C3), stored edge fields and raw kinds are in the
  tooltips. Every plain chip or label that names a provenance or rule
  with a stored confidence reads "rule · number" in one number format
  (1.0, 0.9, 0.875): endpoint header badges, Explorer and schema
  provenance chips ("Read in handler · 1.0"), architecture tiers
  ("Inferred · 0.9"), Flow table chips and lines ("Reads · 1.0, may
  write · 0.9"); where the API sends no number (a move hint, a path
  inferred only by its rule) the word stands alone.
- **Technical**: what T025 moved into tooltips is the primary text again:
  `inferred 0.9 exact`, `fact 1.0`, Flow edges and legend `exact 0.9`
  and `exact 0.9 / 0.7`, the impact rule and contract row tags (I4, C3,
  C4 C5), `exposes_route → calls_route`, `client_call`, assumption ids (V1,
  TA-V3c) and the ids a column row rests on (E2). The plain words are in
  the tooltips.

Both styles read one table, `web/src/lib/labels.ts`, keyed by the rows of
the T025 table (the T025 task record (internal)), four project mode
rows (T022 B9, 22-25: a cross-repository link "Inferred across
repositories" or `inferred 0.7`; a link pass outcome "Several possible
routes" or `ambiguous`; its claim row, hidden in plain; an impact path
"crosses from express into react" or `express → react`), two manifest
editing rows (T022 B10, 26-27: an operation "Accept" or
`accept_suggestion`; a selector field "path prefixes" or `path_prefixes`),
the architecture section's rows 31-41 (T026 AG-C1: kinds, families, cell
sources and states, tiers, level tags, the Hidden note, ghosts, Not proven
entries, markers and stubs; the technical style adds the contract rows),
rows 42-53 (T026 AG-C2: the chain summary, a chain step's edge wording and
tier, CH11 end lines, CH12 cap notes, folds, CH10 hidden counts, CHD
counts, the Not proven deploy group and its database-service note, AT2
ingress, "may also use" with the database card's name, and where a hidden
component's entry comes from) and two T025b rows,
the impact panel's Unknowns line (untraversed edge kinds: "exposes route"
or `exposes_route`; a relation not extracted: "Architecture components:
..." or "Architecture components (I11): ...", the rule id in the tooltip);
a test checks that every row has both variants. A route's full path (row
9) and an edge form without a checked word (row 4) read the same in both.
Strings T025 left raw (reason and move-hint row tags, precision chips) are
unchanged in both.

Precedence: (1) the browser's choice; (2) the repository default,
`graphvein.yaml`'s `ui.labels` ([configuration](../config.md#ui)), read
from `GET /api/v0/ui`; (3) plain. The setting's tooltip names the source.

First launch: while nothing is chosen in this browser and the repository
sets no default, a one-time banner sits across the top of the content
(not a dialog; the page stays usable): "How should Graphvein label facts?"
with two equal buttons, "Plain" (e.g. Exact path match · 0.9) and "Technical"
(e.g. inferred 0.9 exact, I4), and a close button (×) that keeps plain.
Choosing or closing hides it for good in this browser; it waits for
`GET /api/v0/ui` so it never flashes when the repository sets a default.

Storage: `localStorage` key `graphvein.labels.v1` holds `plain` or
`technical` (another value is ignored); `graphvein.labels-banner.v1` holds
`dismissed` once the banner was closed. With storage blocked, labels are
plain, the banner shows on each load, and a choice applies to the page
only ("Not saved" under the setting). Other tabs follow a change.

### Depth defaults

The Impact tab of the Changes section and the architecture chain each
have a depth switch, 1 to 10 (T042). The depth they start with follows
the precedence of the label style: (1) the last depth chosen in this
browser; (2) the repository default, `graphvein.yaml`'s `ui.impact_depth`
or `ui.chain_depth` ([configuration](../config.md#ui)), read from
`GET /api/v0/ui`; (3) the built-in 3 for change impact and 6 for the
chain. A `depth` in the hash (a shared link) applies to that page and is
not stored. The switch's "Depth" label names in its tooltip where the
applied depth comes from. While `GET /api/v0/ui` has not answered and
nothing else decides the depth, the walk waits, so it is requested once.
Settings shows both keys under "Labels and views" with the same three rungs (this
browser, the repository, built-in): its "This browser" select writes the
same stored key as the switches ("Not set" removes it), and a repository
value written there applies at once.

Storage: `localStorage` keys `graphvein.impact-depth.v1` and
`graphvein.chain-depth.v1` hold an integer 1 to 10 (any other value is
ignored). Choosing a depth stores it and writes it to the hash. With
storage blocked, a choice applies to this page only.

The Schema section (`#/schema`, T018b2) reads only the four schema
endpoints below and the polled status. The shell requests
`/api/v0/schema/summary` at load and again only when the status names
another last run; the Schema entry carries a tag from it: "none found"
(only for `found: none`), "re-index" (`version_ambiguous`,
`predates_schema`), "unknown" (every other unknown reason or an unknown
`found`), "N unreadable" (failed sets, sets without records and unreadable
manifests; warning tone; never scan gaps, and a missing count, scan gaps
included, reads "unknown") or "tests only" (every set test-only), in that
order of precedence; no tag while the summary loads or failed, or when
not indexed. `found: unknown` opens "Schema not known for this index"
(never "No migrations found"); the shell requests `/sets` once per index
run, from the first visit to the section (leaving it and coming back reads
the loaded answer), and for `scan_incomplete` and `manifest_unreadable` too, to list the
paths. The home shows the selected set's strip (counts from the lists,
the common state once, other states with full badges, the table-list
line with every reason), under it one strip per failed set, set without
records, unreadable manifest and one for the scan gaps, a compact foreign-key map
(one hop around the selected table above 25 tables) and the table list;
the table detail shows every column fact by the reading rules below,
with "none ◑" only in `complete_in_source` tables and "unknown" for every
null or missing value. An absence (no foreign key, no referencing table,
no column, no ALTER, no table created) is stated only when the API proves
it: plainly for a `complete` table, with ◑ for `complete_in_source`, as
"none known (last known)" otherwise, and as the unresolved table texts
when the table has unresolved references; a set or migration whose
statements were not analysed shows "not extracted", never 0. Tabs: Tables, Migrations (statement counts, null
is "not analysed"; stored diagnostics verbatim) and Overview (the ER
diagram, React Flow and the layout worker loaded on first use, with the
full-fact panel). "Include tests" shows test-only sets. Keyboard hints,
each with a visible control: `/` filter, `o` Open in IDE, `+` `-` `0`
zoom on the Overview, Esc closes its panel. The view state is in the
hash: `#/schema?tab=migrations|overview&set=&sel=&mig=&tests=1`.

Code peek (T016e2, ADR 0044): clicking a caller or near-miss row, or its
`</>` button ("Peek source (Enter)"; pressed while open, pressing again
closes), opens the node's source from `GET /api/v0/source` in a resizable
split below the detail pane; the handler has the same button. While it is
open the coverage cards collapse to the compact strip, without its per-root
rows (the Source root select still filters). Keyboard, with focus on a row
or a control in it: Up/Down move between rows (the open peek follows),
Enter on the row toggles, Esc closes; closing returns focus to whatever
opened the peek. The header shows the kind, name, state, path with
`line:column` and the editor button (disabled when no source was read). The
highlight is drawn only from `source.highlight` (`exact`, `text_match`): a
line band plus a mark on the exact characters; `text_match` adds an amber
"file changed" banner; `changed` and `span_invalid` show the current lines
without any highlight under a high-contrast banner; states that read nothing
keep the panel's size and state the reason; an unknown state is shown raw
without highlight. 8 lines of context, "Show more" adds 8 up to 50; no
syntax colouring. An answer read from another index run than the loaded
view is not shown. The peek height is kept per browser. The open peek is
in the hash (`peek=<node identity>`), so a reload or a shared link opens
it again once the endpoint has loaded (an identity that is not one of the
endpoint's callers, near misses or handler is ignored); selecting another
endpoint removes it, and Overview <-> Callers keeps it.

Detail tabs (T016f2): the selected endpoint's header stays above three tabs,
**Overview** (handler, OpenAPI), **Callers N** (the callers table, near
misses, the "no linked caller" state; the default) and **Flow**; the tab is in the
hash (`tab=overview|flow`; Callers is the default and left out).

Flow tab (T016f2, ADRs 0045 and 0046): who calls the endpoint and what its
handler runs, one or two hops each way, from `GET /api/v0/api/flow`. Only
the response's stored edges are drawn, each in its tier from its stored
fields: fact 1.0 thick (labelled "Fact · 1.0"), exact 0.9/0.7 solid
(labelled "Exact path match · 0.9" or "· 0.7"; the legend chip shows
0.9, its tooltip the 0.7 rank), suffix 0.6 thin and muted
(labelled "Path suffix match · 0.6"; the legend says once that the number
is a fixed rank per match rule, not a probability),
in-code calls plain, a conditional call's targets labelled
"1 of K" with the other target as a muted dashed box; the stored `contains`
step from an owner to its enclosing function is a thin grey "defined in"
line without a confidence label, and callers reached through it say "calls
<fn> (contains this call)" and are never counted as callers. Near misses
are the only dashed lines ("ambiguous · 1 of N", a recorded candidate, not
a link). An owner with one call is folded into the call card; a wrapper is
a "via fn()" pill in the call card. The summary line comes from `counts`
(equal to the Callers tab); `truncated` shows a cap banner and `omitted`
"+N more" chips (not expandable). Controls: `1 hop` / `2 hops` and "Near
misses" in the tab row (default 2 hops, near misses on; they filter the
loaded answer and never re-request), zoom out, zoom in and Fit on the
canvas (Fit returns to zoom 1, where every text is at least 12 px; the
wheel and the buttons may zoom out to 0.5), a minimap, and the always
visible legend whose chips (Fact, Exact, Suffix, Call, Near miss) dim a
tier without re-layout; Dynamic and Unmatched are disabled at 0 ("never
attached to an endpoint") with a link to their gap sheets. A wide graph
starts at the callers with a "Handler + downstream" pill. Clicking a card
(a button) selects it and opens its code peek; its toolbar has Peek source
(Enter), the IDE button (o) and the incoming and outgoing cards, each with
a button that selects that line. Clicking a line or its label selects it
and opens the peek of its stored source with one line of stored facts
(kind, provenance, confidence, match, evidence file:line). Under it, a
line whose call and route were compared with a difference (the edge's
`shape_differences`) lists "Request differences": each key with "sent by
the frontend, not read by the handler" or "read by the handler, not sent
by this call", and a Call and a Handler chip with file:line and its
editor button; a line that was not compared, or whose sides agree, shows
nothing more. Keyboard hints:
arrows move between connected cards, `+` `-` `0` zoom, `1` `2` hops, Esc
closes the peek, then clears the selection. The hash keeps `hops=1`,
`nm=0`, `tiers=`, `node=` and `edge=`; a selection whose peek was closed
adds `peek=0`, so a reload keeps it closed. Switching to or from the Flow
tab clears the selection and the peek. The 2-hop graph is laid out once
and `1 hop` draws its 1-hop cards at the same places, so switching hops
keeps every card, the view and the focus where they were (the near-miss
toggle still lays out again). Cards are at most 240-360 px wide at zoom
1, narrower on a narrower canvas so the call sites, the endpoint and the
handler fit in the first view (a canvas width change lays out again):
longer titles, paths and notes are cut in the middle with "…" (a path
keeps its last `/` segment), the full texts are in the card's tooltip
(hover or focus) and accessible name, and the peek header shows the full
path. Column captions sit in a row pinned below the canvas's toolbar, at
the canvas's left edge while their column is partly in view. While the tab is open the sidebar is
an icon rail and the endpoint list a vertical rail whose button shows the
list over the pane. React Flow and the layout worker load only when the tab
opens: the Flow chunk, then elkjs's unmodified worker file from `/assets/`
(a same-origin worker under the unchanged CSP); a layout taking over 10 s
shows an error with Retry and a link to the Callers tab.

Tables column (T026 AG-C3, AG18): right of "Callees · hop 2", the tables
the handler, its nested functions and its callees read or write (stored
`reads_table` / `writes_table` edges only), each card with its access and
tier ("R fact · W 0.9") and its schema source as the sub line; lines are
the stored edges in the "Reads / writes" tier, labelled with the server's
wording ("reads", "may write", "builds a query on"). Nested functions sit
in the handler or callee column of their container's hop, badged
"nested", joined by a "nested, not a call" line (the stored `contains`).
Two boxes in the column are no node and no line touches them: "may also
use" lists the unbound sites (`may_also_use`) with their function, line,
hop, candidates or "any <tool> table" and reason; the fold box says "+ n
at hop 3" from `tables_beyond` (or that hop 3 has no other table), with
the callees the fan-out cap cut and "deeper calls not read". The summary
counts drawn and written tables as "a lower bound"; with 1 hop the hop-2
tables are hidden and counted ("+n at hop 2"). "Open in Flow tab" in the
Architecture section adds `node=` (the chain's selected member; a member
folded into a card selects that card) and `back=` (the Architecture hash,
accepted only when it starts with `#/architecture`): the first view then
shows the widest range of columns ending at Tables that fits, the
selected card is revealed and peeked, and "← Architecture chain" links
back; changing the endpoint or the tab drops `back`.


### Settings

The Settings page (`#/settings`, T039, [ADR 0088](../adr/0088-settings-editing.md))
is linked at the bottom of the sidebar and of the icon rail. It is one page
with sections (Display; Indexing and scan; Matching; Explorer path values;
Try it environments; Architecture; Other keys) and a page index, which folds
away below about 1100 px. Every key shows its rows: This browser (only
`ui.labels` and `ide.editor`, the controls above), Repository (the value of
[`GET /api/v0/config`](#get-and-post-apiv0config) with its
`graphvein.yaml:line` chip, or not set) and Built-in (the default, or
none). The value in use is the first set row in that order, in bold (row
ST-49 of the [settings claim contract](../contract/claims/settings.md)); a
file that does not load or a member without a checkout shows "unknown" and
no repository value. The This browser row says "Not set" when the browser
chose nothing. While `ide.link_template` is set, the editor's Repository
row is "the link template (ide.link_template)" with that key's line, and
neither an editor value nor the built-in default is in use. A filter shows All, the keys changed from their
default, or the per-browser keys.

Repository values are edited in place; the save bar at the bottom (one
height in every state) counts the unsaved changes and says when they
apply. "Review and save…" shows the server's preview (the changes in
words, when each applies, the unified diff, valid or the decoder's
problems) and "Write graphvein.yaml" applies exactly the previewed result,
only while the file still has the hash the page read; otherwise nothing is
written and the page reads the file again. When the index's settings
differ from the file afterwards (`index_settings.state` `changed`), a
notice names the values not in the index yet and offers "Re-index now",
the same run as the top bar's Re-index control, with its progress. "Edit
as YAML" edits the whole file (line numbers and the form field each line
feeds) through the same preview. `.graphveinignore` is shown read-only;
`explorer.path_values` and `tryit.environments` link to the Explorer's
editors, which edit them (`#/explorer?edit=path-values` scrolls to the
path value editors or says the document has none to enter;
`#/explorer?edit=environments` opens the try it environment picker).
The preview says what the server proved about comments: the comments the
result loses are listed with the changes; it never says comments are
kept, and says nothing about comments when none is lost. An edit that
changes an `ide.*` key (in the YAML view: any key outside the form) reads
the editor defaults again. In project mode the scope select is the member select,
and the manifest's try-it environments are listed read-only.

When `graphvein.yaml` does not load, `graphvein serve` starts anyway
(an index run still refuses it): every view but Settings and the project
overview says the configuration does not load, naming the file and the
decoder's message, with a link to Settings, whose YAML view fixes it.
The views read the file again on every section change and new run.

When `freshness.settings.state` is `changed`, the top-bar badge says
"Fresh · settings changed" (amber), or adds "settings changed" to a stale
badge's time, and a project member's dot turns amber; their tooltips name
the changed keys and "Re-index to apply" (row ST-48). An unknown or absent
comparison is never shown as unchanged.

## HTTP API v0

All API responses are `application/json; charset=utf-8` with
`Cache-Control: no-store`. Unknown paths under `/api/` give 404 `not_found`.

### Errors

```json
{"error": {"code": "store_error", "message": "reading runs: ..."}}
```

| code | status | when |
|---|---|---|
| `bad_request` | 400 | a missing, repeated or invalid query parameter (API section, schema and source endpoints) |
| `not_found` | 404 | unknown API path, or no stored endpoint or node with the requested `id` |
| `method_not_allowed` | 405 | a method other than GET or HEAD (or POST on `/api/v0/index/run` and the writing change endpoints) |
| `unsupported_media_type` | 415 | POST without `Content-Type: application/json` |
| `index_locked` | 409 | POST `/api/v0/index/run` while the index lock is held, by this server's job or another process |
| `reindex_unsupported` | 409 | POST `/api/v0/index/run` when the latest run came from an external indexer |
| `index_lock_unavailable` | 503 | POST `/api/v0/index/run` when the index lock cannot be opened or probed |
| `host_not_allowed` | 403 | Host header not in the allow-list |
| `cross_site_request` | 403 | `Sec-Fetch-Site` is `cross-site` or `same-site`, or `Origin` is present and not this server's |
| `store_error` | 500 | runs (status) or any store read (API section, schema, source) failed |
| `index_changed` | 503 | the index changed during the read and again during its one retry; retry |
| `unavailable` | 503 | the server is stopping and cancelled the request (or refused a new re-index) |
| `internal` | 500 | reserved for unexpected failures; also a re-index job's error code after a recovered panic |
| `repository_required` | 400 | project mode: a per-member endpoint without `repository` when the project has several members |
| `no_checkout` | 409 | project mode: code peek, a `ref:` base or a ref job for a member without a checkout here |
| `ws_required` | 400 | workspace: a per-index request without `ws` on a server without a current index |
| `unknown_entry` | 404 | workspace: `ws` (or a remove) names no entry of this server's workspace |
| `no_index` | 409 | workspace: the entry's folder has no stored index |
| `registry_error` | 500 | workspace: the registry file cannot be read or written |
| `not_loopback`, `token_required` | 403 | a guarded edit (workspace POSTs, manifest edit, path values, try-it) from another machine or without the session token |
| `confirm_required`, `path_changed` | 409 | workspace add without a valid confirm value, or a folder that changed since its check |
| `entry_in_use`, `index_running` | 409 | workspace remove of the command-line index, or of an entry whose run is running |

Messages never contain the Postgres URL. The change endpoints add their
own codes ([below](#change-endpoints-adr-0056)).

### Re-index: POST and GET /api/v0/index/run

Design: [ADR 0052](../adr/0052-reindex-endpoint-and-index-lock.md). The run
executes inside `serve`, with the built-in indexer and the default flags of
`graphvein index` (`--workers`, `--timeout`, `--max-file-size` and
`--shape-depth` are not repeated from an earlier run; `index.shape_depth` of
`graphvein.yaml` applies). Requests keep answering from the last
committed run while it runs: SQLite WAL readers and Postgres READ COMMITTED
readers never see the run before it commits.

**POST** with `Content-Type: application/json` and body `{}` (at most
1 KiB; any field is 400: v1 takes no options) takes the index lock (waiting
up to 2 s), starts a job and answers 202 with the GET document below and
`Location: /api/v0/index/run`. Other answers: 409 `index_locked` (the
message says whether this server's own job or another process holds the
lock), 409 `reindex_unsupported`, 400, 415, 403, 503
`index_lock_unavailable`, 503 `unavailable` (stopping). There is no cancel
endpoint; stopping `serve` cancels a running job.

**GET** (and HEAD) is cheap: it reads the lock state and this server's
latest job, never walking the repository.

```json
{
  "api_version": 0,
  "repository": "kftray",
  "lock": { "state": "held_here", "error": null },
  "job": {
    "id": "Q7K2...",
    "state": "running",
    "started_at": "2026-11-07T07:21:03.112Z",
    "ended_at": null,
    "phase": "parsing",
    "progress": { "done": 212, "total": 457, "unit": "files" },
    "replaces": { "finished_at": "2026-11-07T07:06:05.414Z" },
    "run": null,
    "error": null
  }
}
```

- `lock.state`: `free`, `held_here` (this server's job holds it),
  `held_elsewhere` (another process or another server), `unknown` with
  `lock.error` (the lock could not be probed, or re-index is not configured).
  Treat any other value as `unknown`, never as `free`. On SQLite the probe
  creates the empty `index.db-lock` file if it is missing and holds the lock
  for about a millisecond; an acquirer's 2 s wait rides it out.
- `job`: `null` until this server process starts a job; only the latest job
  is kept, in memory (a restarted server has none, and never reports a CLI
  run's outcome, only its lock).
- `job.state`: `running`; `committed` (the run transaction committed;
  `run` is `{ "id", "finished_at" }` of the committed run, `finished_at` in
  the same form as status `index.last_run.finished_at`); `failed` (`error`
  is `{ "code", "message" }`: `index_failed` with the indexing or ingest
  error verbatim, `index_lock_lost` when the lock could not be proven held
  right before the commit, `store_error` when the writer store could not be
  opened, `internal` for a recovered panic); `cancelled` (server shutdown;
  rolled back). A failed or cancelled run leaves the store unchanged.
- `job.phase`, non-null until the job ends (`ended_at` set; the job
  records its outcome, then releases the lock, so a document may show an
  ended job with `held_here` for a moment, never a free lock next to a job
  that has not ended): `starting` (opening the writer store), `scanning`, `cache`
  (fact cache lookups), `parsing`, `linking` (also saving the fact cache and
  writing the snapshot file), `writing` (ingest into the run transaction),
  `committing`, `finishing` (closing the writer store, after a commit or a
  failure). A `committed` job reports `finishing` while it still holds the
  lock. Treat an unknown phase as a word to show, with no fraction.
- `job.progress`: non-null only in `cache` and `parsing` (`unit: "files"`)
  and `writing` (`unit: "records"`, lines of the snapshot), with `done` and
  `total` both counted by the run; `null` in every other phase. There is no
  estimate.
- `job.replaces`: the latest committed run when the job started (`null` if
  none): the run requests keep answering from until the switch.

`GET /api/v0/status` is unchanged and knows nothing of the lock: while a
run is in flight it reports the previous run as `last_run` and that run's
freshness, which can be `fresh`. A client that must not show "fresh" during
a run combines it with `lock.state`; a client that switches to a new run
waits for `job.state: "committed"` and then for a status answer whose
`last_run.finished_at` is the job's `run.finished_at` or later.

### GET /api/v0/status

```json
{
  "api_version": 0,
  "repository": "graphvein",
  "server_version": "dev",
  "extractor": {"current": "extractors-28", "indexed": "extractors-28", "matches": true},
  "index": {
    "indexed": true,
    "last_run": {
      "finished_at": "2026-11-03T14:32:05.123Z",
      "indexer": {"name": "graphvein", "version": "extractors-28"},
      "contract_version": 1,
      "commit": "abc123"
    }
  },
  "freshness": {
    "state": "stale",
    "checked_at": "2026-11-03T14:40:00Z",
    "changed_count": 2,
    "changed_files": ["cmd/graphvein/main.go", "internal/server/server.go"],
    "changed_files_truncated": false,
    "settings": {
      "state": "changed",
      "changed_keys": ["index.shape_depth"],
      "unknown_keys": [],
      "index_hash": "sha256:…",
      "current_hash": "sha256:…",
      "reason": null
    }
  }
}
```

Every request runs its own freshness calculation, the same one as
`graphvein status` (same scan and ignore rules); nothing is cached.
Concurrent requests wait for each other rather than scanning in parallel.

| condition | `freshness.state` | other fields |
|---|---|---|
| runs cannot be read | (no body) | 500 `store_error` |
| freshness check fails (incomplete scan: an unreadable file, directory or ignore file, or a path the contract cannot carry; store read). A symlink, binary, non-UTF-8 or non-regular file the scan skips is not a failure | `unknown` | `changed_count` null, `changed_files` [], `error` holds the message |
| no runs | `not_indexed` | every scanned file listed, as `status` lists them |
| runs, files changed | `stale` | count and sorted paths |
| runs, nothing changed | `fresh` | count 0, [] |

- `changed_files` are sorted repository-relative paths (added, modified and
  deleted files), at most 1000; `changed_count` is always exact and
  `changed_files_truncated` is true when the list was cut. `error` appears
  only for `unknown`.
- `index.indexed` is true when at least one run exists. `last_run` is the
  run with the latest finish time (null when there is none); `commit` is
  omitted when the producer recorded none.
- `extractor.current` is this binary's extractor version. `indexed` is the
  last run's version when Graphvein produced it, else null (external
  indexers have their own versions); `matches` compares the two and is null
  when `indexed` is. A mismatch does not change the freshness state, which
  depends on file contents only; re-indexing is recommended.
- Times are UTC RFC 3339. `checked_at` is the instant the freshness check
  started, before the scan. No change that existed at `checked_at` and was
  still present when its file was read is missed; a file edited and reverted
  during the scan, or an index commit landing mid-check, can make `fresh`
  describe a slightly later state.
- Clients must treat any `state` other than the four above as `unknown`,
  never as fresh.
- `settings` compares the `graphvein.yaml` settings the last run read
  with the file as it is now ([ADR 0088](../adr/0088-settings-editing.md),
  rows ST-41..ST-46 of the [settings claim contract](../contract/claims/settings.md)).
  Each run of the built-in indexer records its index inputs: the keys
  that apply at the next index run (`index.shape_depth`,
  `clients.base_paths`, `clients.link_generated_output`, `tests.include`,
  `tests.exclude`, `openapi.include`, `openapi.exclude`,
  `openapi.base_paths`) with their effective values (built-in defaults
  included, the `--shape-depth` flag's value when given). Values are
  compared, not bytes: comments, layout, quoting, the order of a pattern
  list or base-path map and writing a default explicitly are not
  changes; `ui`, `ide`, `architecture` (they apply at once) and every key
  outside the Settings form never make an index stale. `state` is
  `unchanged`; `changed` (`changed_keys` in key-table order; a re-index
  applies them); `unknown` with a `reason` (the run recorded nothing, as
  an index from before schema 6 or an external indexer's; the file does
  not load; a member without a checkout; `unknown_keys` lists current
  keys the record lacks); or `not_indexed`. `index_hash` and
  `current_hash` are the SHA-256 of the canonical values, null when not
  known. `settings` never changes `freshness.state`, which describes the
  files; clients treat any other `settings.state` as `unknown`.

### API section endpoints

Four read-only endpoints serve the API section of the UI (ADR 0042; the flow endpoint ADR 0045): every
HTTP route and Tauri IPC command, who calls each one, how sure each link is,
and where the graph is incomplete. They read the store after the request
arrives; there is no cache and no freshness walk (combine them with
`/api/v0/status`). Each reads the runs first and last; when the latest run
or the run count differs, it repeats the whole read once and then answers
503 `index_changed`. Unknown query parameters are ignored; a parameter given
twice is 400 `bad_request`.

The endpoints that present nodes (`/api/v0/api/endpoint`,
`/api/v0/api/calls`, `/api/v0/api/flow`, `/api/v0/source`,
`/api/v0/schema/table` and `/api/v0/schema/migration`) take an optional
`editor` (ADR 0050): a preset name (`vscode`, `vscode-insiders`,
`cursor`, `vscodium`, `jetbrains`, `zed`). With it, links and `ide.label`
are built from that preset alone; the repository's `ide` setting (including
`link_template`, `label`, `column_unit` and `column_base`) does not apply,
and links are built even when `graphvein.yaml` fails to load (`ide.available`
true, `ide.error` still holding the message). An unknown or empty value is
400 `bad_request` (`editor must be one of vscode, vscode-insiders, cursor,
vscodium, jetbrains, zed; not "sublime"`) and never falls back to another
editor; a repeated `editor` is 400 as for every parameter.

Common parameters:

- `scope` = `production` (default) | `test` | `all`. Production is a node
  without a `scope` attribute, test is `scope: "test"`, all is every node. A
  node with any other `scope` value is only in `all` and is counted in
  `unknown_scope`.
- `root` (optional): an exact source root (below; `.` allowed). Calls of
  other roots are left out of every count, caller and list. An unknown root
  gives empty results, not an error.

Transports are fixed by node kind (the `transport` attribute is not read):

| transport | endpoint kind | call kind | call edge | handler edge |
|---|---|---|---|---|
| `http` | `route` | `client_call` | `calls_route` | `exposes_route` |
| `tauri_ipc` | `ipc_command` | `ipc_call` | `invokes_command` | `exposes_command` |
| `server_action` | `server_action` | `action_call` | `calls_action` | `exposes_action` |

Server action calls (T024b, ADR 0077, [server actions](../extract/server-actions.md))
are stored only when proven, so their one documented status is `linked`.

**Source root** of a call: the deepest directory, from the call file's own
directory up to the repository root, that holds a stored file (an indexed,
not ignored file) named `Cargo.toml`, `deno.json`, `deno.jsonc`, `go.mod`,
`package.json`, `pyproject.toml`, `setup.cfg` or `setup.py`; else the file's
first path segment; else `.` (a file at the root, or a synthetic node).
`manifest` is that file's path (the first name in byte order when one
directory holds several), or null for the fallbacks. Roots are a property of
calling sites only. Paths are split on `/` only, as the contract requires
(docs/contract): a path with `\` from another indexer is not converted, so
its directories are not seen and it falls back to its first `/` segment or
`.`.

**Counts**, per transport, with E = endpoints in scope, C = calls in scope
(and in `root`), X = the stored call edges into all endpoints:

- `endpoint_count` = |E|; `calls.total` = |C|.
- `calls.by_status[s]` = calls whose `attributes.status` is exactly `s`; a
  missing or non-string status counts under `""`. The values sum to `total`.
- `caller_count` of an endpoint = distinct calls in C with an edge in X to
  it, whatever their status; `never_called` = endpoints in E with
  `caller_count` 0, except a server action with a stored `unread_uses`
  count (an unread use may reach it; flag `unread_use`, T024b R1). The UI
  shows them as "no linked caller" ("no linked caller in production" in
  production scope, "in the project" under `--project`), never as "never
  called": unmatched, dynamic and wrapper calls may still reach them. The
  key keeps its name.
- `linked_without_edge` = calls in C with status `linked` and no edge in X
  (an index inconsistency; expected 0).
- `edges_from_unlinked` = calls in C whose status is not `linked` but that
  have an edge in X (for example a dynamic call one of whose alternatives
  links).
- `unknown_scope` = calls in C with a scope value other than `test`.
- `roots` = every source root of the calls in scope, ignoring `root`, with
  `manifest`, `total` and `by_status`, `.` first, then byte order. The totals
  sum to the scope's total.

Raw statuses are passed through unchanged. Clients map them to categories
and must treat any status they do not know as "other", never as linked.

#### GET /api/v0/api/endpoints?scope=&transport=&root=

`transport` = `all` (default) | `http` | `tauri_ipc` | `server_action`;
with `all` every transport is present, also when empty.

```json
{
  "api_version": 0, "repository": "langflow",
  "index": {"finished_at": "...", "indexer": {"name": "graphvein", "version": "extractors-28"}, "contract_version": 1},
  "scope": "production", "root": null,
  "transports": [
    {"transport": "http", "frameworks": ["fastapi"], "endpoint_count": 296, "never_called": 149,
     "calls": {"total": 588, "by_status": {"dynamic": 422, "linked": 160, "unmatched": 6},
               "linked_without_edge": 0, "edges_from_unlinked": 0, "unknown_scope": 0},
     "roots": [{"root": "docs", "manifest": "docs/package.json", "total": 409, "by_status": {"dynamic": 409}},
               {"root": "src/frontend", "manifest": "src/frontend/package.json", "total": 179,
                "by_status": {"dynamic": 13, "linked": 160, "unmatched": 6}}],
     "openapi": {"specs": 2, "documented_endpoints": 116},
     "endpoints": [{"id": "route:...", "name": "GET /{flow_id}", "method": "GET", "path": "/api/v1/flows/{flow_id}",
                    "path_count": 1, "pattern": "/{flow_id}", "framework": "fastapi", "file": "src/...", "line": 505,
                    "scope": null, "caller_count": 3, "documented": true, "flags": []}],
     "precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
     "callers_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"}},
    {"transport": "tauri_ipc", "frameworks": [], "endpoint_count": 0, "never_called": 0,
     "calls": {"total": 0, "by_status": {}, "linked_without_edge": 0, "edges_from_unlinked": 0, "unknown_scope": 0},
     "roots": [], "openapi": null, "endpoints": [],
     "precision": {...}, "callers_precision": {...}}
  ]
}
```

- `index` is the last run as in `/api/v0/status` (`last_run`), null when not
  indexed.
- `precision` and `callers_precision` (T020, ADR 0066) are the transport's
  scope labels for its endpoints (routes or IPC commands) and its calls
  (client calls or IPC calls), the same values `/endpoint` serves for each
  of its endpoints; see [GET /api/v0/precision](#get-apiv0precision).
- HTTP rows: `method`, `pattern`, `framework` are the stored attributes (null
  when absent or not strings); `path` is the template of the first composed
  path whose entry is in scope (production and all: without `scope: test`;
  test: with it), else of the first path, null when there is none, with
  a trailing `/` when the entry has `trailing_slash: true` and is not the
  root (the path FastAPI serves, ADR 0019; T025);
  `path_count` = number of composed paths (full paths only); `scope` is the raw attribute;
  a path whose prefix the code does not fix is never a full path (T033,
  ADR 0084, rows UP-29 and NX-R4 of the
  [unknown-prefix claims](../contract/claims/unknown-prefix-paths.md)):
  a route whose only paths are `unresolved_prefix_paths` entries, or Next.js
  entries with `resolution: unresolved` (a `basePath` not read), has `path`
  null, `path_count` 0 and `prefix_unknown` (absent otherwise):
  `{path, reason, reason_text, hop, count}`, where `path` is the known part
  with `…` for the unknown prefix (`…/api/hosts/{host}/logs/stream`,
  `/outer/…/upb/inner/{name}` when a known prefix sits above the unknown
  one), `reason` the stored reason (`route_prefix_unresolved`,
  `route_mount_unresolved`, `nextjs_base_path_unresolved`), `reason_text`
  "prefix unknown: ..." in words, `hop` the unresolved mount key (null for
  a generated server or a Next.js `basePath`) and `count` the route's
  unknown-prefix paths (the first in scope is shown, as for `path`);
  `documented` = at least one stored `documents_route` edge. `openapi.specs`
  = number of `api_spec` nodes (0 means no spec indexed);
  `documented_endpoints` = rows with `documented`. `shape` (absent when the
  route has no stored request/response shape, which means every section is
  not read) is the compact form: `{sections: {<section>: {state, items}},
  depth_cap, depth_limit}` for the sections `path`, `query`, `header`,
  `cookie`, `body`, `responses`, `security`
  ([route shape claims](../contract/claims/route-shape.md)).
- Tauri rows carry `command`, `registry`, `cfg_gated`, `test_only`,
  `unreachable` (absent = false), `plugin` and `handler` instead of method,
  path, pattern and documented; `openapi` is null.
- Server action rows (T024b) carry `action` (the export or bound name,
  null for an anonymous inline action), `action_scope` (`module` or
  `inline`), `form` and `handler`; `openapi` is null.
- `flags` (sorted): http `unmounted` (no composed path, no
  unknown-prefix path and no withheld path: the path is not known, shown
  as "path not known", never as proof that the route is unreachable),
  `prefix_unresolved` (no full
  path, but a path whose prefix the code does not fix: `prefix_unknown`),
  `shadowed` (no full path: an earlier mount whose prefix is read takes
  every composed path, the route attribute `shadowed_paths`; the route is
  mounted), `path_withheld` (no full path: a registration whose path the
  Go linker withholds, from the stored `route_mount_unresolved`
  diagnostic at the route's span, couldn't-read row CR-44; replaces
  `unmounted`; ADR 0089 Revision 5), `path_assumed` (a full path rests on an assumption stored on
  its entry: GR-V5, or RO-V1 "a mount at an unknown prefix may answer
  first", RO-V2 "a mounted app's catch-all may answer first"; the
  endpoint view names it beside the path), `conditional`,
  `test_paths`, `handler_inline`, `handler_unresolved` (no handler and not
  inline); tauri `cfg_gated`, `test_only`, `unreachable`, `unproven_registry`,
  `plugin_registry`, `unresolved_entry` (command null); server_action
  `anonymous` (action null), `handler_unresolved` (handler null), `inline`,
  `unread_use` (an unread use may reach it: shown as "may be called", never
  as "no linked caller").
- Order: http by `path` (null last; those with `prefix_unknown` first, by
  its path without the leading `…`, then by `pattern`), then method (GET,
  HEAD, POST, PUT, PATCH, DELETE, OPTIONS, ANY, others in byte order), then
  `id`; tauri by `command` (null last), then `id`; server actions by file,
  line, then `id`.

#### GET /api/v0/api/endpoint?id=&scope=&root=

`id` is a canonical node identity of kind `route`, `ipc_command` or `server_action`,
URL-encoded as a query value (identities contain `#`, `%` and `/`): 400 when
it is missing, not canonical or of another kind; 404 `not_found` ("no API
endpoint with identity ...") when it is not stored, for example after a
re-index removed it.

```json
{
  "api_version": 0, "repository": "...", "index": {...}, "scope": "production", "root": null,
  "ide": {"available": true, "error": null, "label": "Open in VS Code", "editor": "vscode", "source": "builtin"},
  "endpoint": <presented node>, "transport": "http",
  "handler": {"state": "resolved", "expression": null, "node": <presented node>,
              "edge": {"kind": "exposes_route", "provenance": "fact", "confidence": 1}},
  "openapi": {"specs": 2, "operations": [{"node": <presented api_operation>,
              "edge": {"kind": "documents_route", "provenance": "inferred", "confidence": 0.9}}]},
  "callers": [{"node": <presented call node>, "owner": {"id": "function:...", "kind": "function", "qualified_name": "getFlowFn"},
               "edges": [{"kind": "calls_route", "provenance": "inferred", "confidence": 0.9, "match": "exact"}],
               "reasons": []}],
  "near_misses": [{"node": <presented call node>, "owner": {...}, "relation": "ambiguous_candidate",
                   "candidate_count": 3, "candidate_count_exact": true, "reasons": [...]}],
  "near_misses_incomplete": 0,
  "precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "callers_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"}
}
```

- `precision` is the scope label of the endpoint's family (routes, or IPC
  commands) and `callers_precision` that of its calls (client calls, or
  IPC calls), each `{label, row, reason}` as in
  [GET /api/v0/precision](#get-apiv0precision). No rule row names these
  families, so both are `best_effort` from row D today. `/flow` carries the
  same two fields; they label the route or command and call facts only, a
  structural node keeps its file's label (`/source`).
- A presented node is the stored node (attributes verbatim) plus `ide`
  ({`link`, `line`, `column`, `exact`, optional `line_only` and `caveat`} as in the query commands, or null for
  a node without span, a synthetic node or when the configuration cannot be
  loaded) and `file_state`: `unchanged`, `changed` or `missing` (the file
  cannot be read now), comparing the file as it is with its stored hash; null
  for synthetic nodes. Presented call nodes add `source_root`.
- `ide.available` is false and `ide.error` holds the message when
  `graphvein.yaml` cannot be loaded; the data is still returned. `ide.label`
  (ADR 0044) is the button text of the configured editor: the explicit
  `label`, else the preset's label while the preset's template is in use,
  else null (a user template); null when the configuration cannot be
  loaded. Clients without a label recognise the link's scheme.
- `ide.editor` (ADR 0050) is the preset whose format built the links, or
  null for a custom repository template (and when no links could be built).
  `ide.source` says which rule chose it: `request` (the `editor` parameter),
  `repository` (`graphvein.yaml` has a nonempty `ide` setting; also when
  `graphvein.yaml` fails to load without `editor`) or `builtin` (no
  `graphvein.yaml`, no `ide` key or `ide: {}`: Graphvein's default).
- `handler.state`: `resolved` (the `handler` attribute names a stored node),
  `missing` (it names a node that is not stored; `node` null), `inline`
  (http `handler_inline`), `unresolved` (no handler; http `expression` =
  `handler_expression`). `edge` is the stored exposes edge from that handler,
  or null.
- `openapi` (http only): the operations with a stored `documents_route`
  edge to the route, by identity; null for tauri.
- `callers`: every call in scope (and root) with a stored call edge to the
  endpoint, by file, span start and identity, whatever its status; `edges`
  are those stored edges with their stored provenance and confidence. `match`
  (http only) is the `match` of the call's `linked` path entry whose
  candidates are exactly this endpoint (exact before suffix), else null.
  Callers never come from `status`, `candidates` or `alternatives` alone.
- `near_misses`: calls in scope (and root) without an edge to the endpoint
  that list it in recorded candidates of an `ambiguous` or `test_only`
  choice (http: a path entry with that status, whatever the call's own
  status, so a `dynamic` call with an ambiguous entry counts; tauri: the
  node's `candidates`, recorded only when the node status is `ambiguous` or
  `test_only`). `relation` = `ambiguous_candidate` | `test_only_candidate`;
  `candidate_count` = the tie size (http: the entry's stored
  `candidate_count`, at least the number listed; tauri: the number listed).
  `candidate_count_exact` is false when that size is only a lower bound: no
  count is stored and the list holds the cap of ten candidates (always so
  for a tauri list of ten); a UI then says "at least". Dynamic and unmatched
  choices record no candidates and are never near misses.
- `near_misses_incomplete` = calls with such a choice that do not list the
  endpoint but whose candidate list may have been cut (http stored
  `candidate_count` larger than the list, or no count and ten listed; tauri
  ten candidates), so the endpoint may be an unlisted candidate.
- `shape` (HTTP routes; absent when none is stored: every section not read)
  is the route's request/response shape as stored in its `shape` attribute,
  decoded and checked against rows SM-1..SM-29 of the
  [route shape claims](../contract/claims/route-shape.md): seven sections
  with a state (`not_read`, `provably_none`, `partial`, `complete`) and
  items with provenance, `security_schemes`, `models`, and `follow`
  (`depth_limit`, the call-following limit of the run, and the followed
  `chains` with their stop reasons, `depth_cap` among them). A stored shape
  that is invalid is not served: `shape_error` says why and `shape` is
  absent. The raw attribute stays in `endpoint.attributes`. The route
  detail's "Request and response" block in the Web UI shows it.
- `shape_links` (HTTP routes, additive; T029) maps each `"file:line"` a
  shape chip shows to its editor link, an object of the same form as a
  presented node's `ide` (`link`, `line`, `column`, `exact`, `line_only`,
  `caveat`), built for that line exactly as a node's link under the same
  `ide` status and `editor=`. The positions are the route's registration
  (its file and start line), the first chain step of each stored
  `security_dependencies` entry, and every provenance of the stored
  `shape` (items, section evidence, `security_schemes`, `models`); chain
  steps are not chips and have none. The field is absent when there is no
  link, such as when editor links are unavailable. The Web UI's chips open
  only these links and show no editor button for a position without one.
- `model_tables` (HTTP routes with a valid stored shape, additive; T029)
  lists the shape's models that are also database tables: `{model, class,
  table, name, set, tool, row}`, where `model` is the shape's model name,
  `class` the class node both are declared by, `table` the table's
  identity and `name`, `set` and `tool` its attributes (null when not
  strings). A model is a table only when it is the same declaration node
  (row MT-20 of the [route model type claims](../contract/claims/route-model-types.md)):
  its provenance is the start line of a stored class node with its name,
  and the table's `class` attribute names that class node (SQLModel tables
  today). A class with the same fields or name is never matched. Sorted by
  model name, then table identity; `[]` when no model is a table; absent
  without a shape. The route detail's "Request and response" block notes
  "also the database table X" with a link to the Schema view.
- `path_withheld` (HTTP routes, additive; ADR 0089 Revision 5): true when
  the route has no full path and is a registration whose path the Go
  linker withholds (the list's flag `path_withheld`, couldn't-read row
  CR-44); absent otherwise. The header then says "path withheld", else
  "path not known" for a route with no path of any kind; never "not
  mounted".
- `owner` is the source of the call's stored `contains` edge (null when
  none).
- `shape_differences` (HTTP callers, additive; T029) compares what the
  call sends (its stored `sends` attribute) with what the route reads (its
  stored `shape`), rows SD-1..SD-12 of the
  [shape differences claims](../contract/claims/shape-differences.md). It
  is absent when nothing was compared: the call's keys are not proven, or
  the route's section is not fully read (`complete` with no reason, no
  depth cap, every compared item a `fact`). Present, it holds `compared`
  (the sections compared: `body`, `query`), `differences` (each with
  `row`, `section`, `field`, `direction` `sent_not_read` or
  `read_not_sent`, `text`, and `call` and `route` sides with `file`,
  `line`, `rule` and `kind`; empty when the sections agree), and
  `call_links` and `route_links` (the editor link of each side's
  `"file:line"`, of the same form as `shape_links`; absent without
  links). In project mode the `project` part's callers carry it too, with
  `cross_repository: true`.

#### GET /api/v0/api/calls?transport=&status=&scope=&root=

The calls of one transport with the given raw statuses, for gap lists.
`transport` = `http` | `tauri_ipc` | `server_action` (required); `status` (required) =
comma-separated raw statuses, where an empty item selects calls whose status
is missing or not a string (`status=` alone, or `status=,weird`).

```json
{
  "api_version": 0, "repository": "...", "index": {...}, "scope": "production",
  "transport": "http", "statuses": ["dynamic"], "root": null, "ide": {...},
  "total": 422,
  "calls": [{"node": <presented call node>, "owner": {...}, "targets": [],
             "reasons": [{"code": "method_unknown", "text": "Method is not known statically", "detail": "options.method"}]}]
}
```

`statuses` are the requested values, distinct and sorted. `calls` are
sorted by file, span start and identity, without paging or server-side
search; `targets` are the stored call-edge targets of each call (non-empty
only for a call that links through one alternative). Endpoints without a
linked caller are not listed here; they are the rows with `caller_count` 0.

#### GET /api/v0/api/flow?id=&scope=&root=

The stored neighbourhood of one endpoint for the Flow tab (ADR 0045), one
or two hops each way, always both hops (clients filter hops and near
misses). Parameters, errors (400, 404, 500, 503 `index_changed`), headers, `index`, `ide` and the
consistency check are those of `/api/v0/api/endpoint`. Callers and near
misses are computed by the same code as there, so `counts` equal its
`callers`, `near_misses` and `near_misses_incomplete` for the same request.

```json
{
  "api_version": 0, "repository": "...", "index": {...}, "scope": "production", "root": null,
  "ide": {...}, "transport": "http",
  "endpoint": <presented node>, "handler": <handler as in /endpoint>,
  "counts": {"callers": 3, "near_misses": 0, "near_misses_incomplete": 0},
  "nodes": [{"node": <presented node>, "roles": ["call"], "hop": 1, "omitted": {"callers": 0, "callees": 0}}],
  "edges": [{"kind": "calls_route", "source": "...", "target": "...", "provenance": "inferred",
             "confidence": 0.9, "match": "exact", "evidence": [{"file": "...", "span": {...}}]}],
  "candidates": [{"call": "client_call:...", "endpoint": "route:...", "relation": "ambiguous_candidate",
                  "candidate_count": 2, "candidate_count_exact": true}],
  "skipped_edges": 0,
  "truncated": null,
  "precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "callers_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "code_calls_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "may_also_use": [{"accessor": "function:...", "hop": 1, "row": "CT-P5", "tool": "sql", "reason": "sql_not_literal",
                    "partial": true, "line": 14, "candidates": null}],
  "tables_beyond": {"hop": 3, "tables": 2, "functions": 2, "may_also_use": 1}
}
```

`precision` and `callers_precision` are `/endpoint`'s scope labels for the
same endpoint (the route or command family and its calls' family); they do
not cover structural nodes, which keep their file's label.
`code_calls_precision` is the label of the code `calls` edges of the
handler's call chain (family `code_calls`, T020 O-S1-5 (a)): no file label
covers a file's calls, so a structural node's Precise file label never
vouches for them.

Node roles (a node appears once with all its roles, in this order; `hop`
is its smallest):

| role | hop | rule |
|---|---|---|
| `endpoint` | 0 | the requested node |
| `handler` | 0 | the stored node named by `handler` (`handler.state` `resolved`) |
| `call` | 1 | the `callers` of `/endpoint` |
| `owner` | 1 | source of each call's stored `contains` edge (as `owner` in `/endpoint`) |
| `enclosing` | 1 | source of a caller owner's stored `contains` edge when it is a `function` or `method`; one step only |
| `other_target` | 1 | other targets of a call's stored call edges (a conditional call), with its raw `scope` |
| `near_miss` | 1 | the `near_misses` of `/endpoint` (owner only, no further expansion) |
| `near_miss_owner` | 1 | source of each near miss's stored `contains` edge; a node owning a call too also has `owner` |
| `callee` | 1, 2 | targets of the handler's stored outgoing `calls` edges, then of those callees' |
| `caller` | 2 | sources of stored incoming `calls` edges of each caller owner and enclosing function |
| `nested` | 0, 1, 2 | (T026 AG-C3, chain row CH7) functions and methods the handler, a nested function or a callee contains by a stored `contains` edge, transitively up to 8 levels, at the container's hop; their callees are callees of the next hop (hop 1 from the handler and its nested functions, hop 2 from hop-1 callees and their nested functions) |
| `table` | 1, 2 | (T026 AG-C3, chain row CH6) targets (kind `table`) of stored `reads_table` / `writes_table` edges of the handler, nested functions and callees, at the accessor's hop and at least 1 |

- The enclosing step is the stored `contains` edge, not a call: callers
  reached through it call the enclosing function (their `calls` edge
  targets it) and are never route callers; no count includes them.
- `scope` and `root` filter call and near-miss nodes only; hop-2 functions
  store no scope, so none is applied to them.
- `edges`: every stored edge used to reach a node plus every stored `calls`
  edge between two returned nodes (recursion is a self-loop), verbatim, by
  kind, source, target. HTTP call edges add `match` (as in `/endpoint`,
  null when no linked path entry says so) and, additive (T029),
  `shape_differences` when the call and its target route were compared
  (the same value `/endpoint` serves on that caller), and `assumptions`
  when the link rests on any (T033; absent otherwise): each `{id, text}`,
  `origin` (an origin nothing proves, LF-1) or `UP-V1`, `UP-V2` (the
  unknown-prefix tier, or a Next.js route whose `basePath` is not read),
  read from the call. An edge to a node that is not
  stored is left out and counted in `skipped_edges`.
- `candidates`: one entry per near miss, a recorded candidate and not an
  edge; never mixed into `edges`.
- Tables (T026 AG-C3, ADR 0045 revision): `reads_table` and
  `writes_table` edges add `wording`, chain row CH6's words ("reads",
  "writes", "may read" / "may write" when inferred, "builds a query on"
  for a query builder's entry). Upstream functions (owners, enclosing
  functions, callers) never contribute tables. `may_also_use` lists every
  `table_access_unknown` site of a returned handler-side function
  (`accessor`, its `hop`, the stored `row`, `tool`, `reason`, `partial`,
  `line`, and `candidates` as `{id, name}` with `name` null when the table
  is not stored, or null for any table of the tool), ordered by hop,
  accessor and line: a site the indexer recognised but could not bind,
  never an edge. `tables_beyond` counts what lies one hop further: the
  stored callees of the hop-2 handler-side functions (no fan-out cap) and
  their nested functions, the tables they read or write that the response
  does not return (`tables`), those functions (`functions`) and their
  unknown sites (`may_also_use`); deeper hops are not read, so a zero is
  never an absence beyond hop 3. It is null when no handler node is stored
  or hop 2 was dropped. Nested functions and tables count towards the node
  and edge totals; dropping hop 2 drops hop-2 nested functions and tables.
- Caps, ordered by file, span start, identity: at most 25 `calls`
  neighbours per node and direction; `omitted` counts the cut neighbours
  that no returned edge reaches (a cut neighbour returned another way, with
  its edge induced, is not counted); at most 200 calls and 200 near misses
  (`counts` stay uncut); when hop 2 would make more than 300 nodes or 600
  edges, every hop-2 node, its edges and the roles and cuts only hop 2 gave
  are dropped and hop 1 stays complete. Hop 1 is never cut; when it alone
  exceeds 300 nodes or 600 edges the reason `hop1_over_totals` is given,
  adding nothing to the omitted counts. `truncated` is null or
  {`nodes_omitted`, `edges_omitted`, `reasons`}, reasons sorted from
  `calls_cap`, `hop1_over_totals`, `hop2_dropped`, `near_misses_cap`. Under
  `calls_cap` and `near_misses_cap`, `nodes_omitted` counts only the cut
  call and near-miss nodes (not owners, enclosing functions, other targets
  or callers they would have added) and `edges_omitted` only the cut calls'
  edges to the endpoint; under `hop2_dropped` they count the removed hop-2
  nodes and the edges touching them.
- Nodes are ordered by hop, first role, file, span start, identity. Call
  and near-miss nodes are presented with `source_root`.

#### Reasons

`reasons` explain a call that is not linked, derived from its stored
attributes only (the index stores no reason text); the first is the primary
reason. `detail` is the stored spelling the reason refers to, or null. A
call whose status is exactly `linked` has none, except `origin_assumed`
(below), one per stored origin assumption, `prefix_assumed` (below),
one per assumption of an unknown-prefix link, and `path_assumed` (below),
one per path assumption of the matched route path entry. A status this version does
not document for the transport (including `""` and case variants) gets the
single reason `status_unknown`: "Status "<raw>" is not known to this
Graphvein version".

client_call, in this order:

| code | condition | text | detail |
|---|---|---|---|
| `origin_assumed` | status `linked` with `origin_assumptions` (a 0.7 link whose origin nothing proves, row LF-1 of docs/extract/clients.md) | Assumption: `<stored text>` ("origin https://api.example.com assumed to be this repository's server") | null |
| `prefix_assumed` | status `linked`, or a stored link on a call of another status (after `alternative_linked`), with `UP-V1` or `UP-V2` in the call's `assumes` (a link of the unknown-prefix tier, rows UP-19, UP-V1, UP-V2 of docs/extract/clients.md) | Assumption: `<the assumption's text>` ("The route is served below a prefix the code does not fix ...") | the assumption id |
| `path_assumed` | as `prefix_assumed`, with `GR-V5`, `RO-V1` or `RO-V2` in the call's `assumes` (the route path entry the link matched rests on that path assumption, docs/extract/routes.md), after the `prefix_assumed` reasons | Assumption: `<the assumption's text>` ("a mounted app's catch-all may answer first: ...") | the assumption id |
| `wrapper_inner` | status `wrapper` | Inner request of a wrapper; the wrapper's call sites are listed as calls | null |
| `generated_output` | status `generated` (committed bundler output, ADR 0085) | Committed bundler output; its calls are not linked (clients.link_generated_output in graphvein.yaml links them) | `generated_output` (the marker row, `GB-1` ...) |
| `no_matching_route` | a path entry is `unmatched` | No route matches this method and path | `<method> <template>` |
| `ambiguous` | a path entry is `ambiguous` | `<n>` routes match equally (`At least <n> ...` when no count is stored and ten are listed) | template |
| `test_only_target` | a path entry is `test_only` | Only test-scope routes match | template |
| `url_unevaluated` | every template is null | URL is not evaluated statically | `url_expression` |
| `some_alternatives_unevaluated` | some, not all, templates null | Some URL alternatives are not evaluated | `url_expression` |
| `method_unknown` | `method` null | Method is not known statically | `method_expression` |
| `base_unresolved` | `base` `unresolved` with a non-empty `base_expression` | Base URL is not resolved | `base_expression` |
| `generated_unproven` | `library` `orval`, a `generated` entry with null template | Generated client; its transport is not proven | `operation` |
| `alternative_linked` | the call has stored call edges | A stored link exists although the status is "<raw>" | null |
| `status` | none of the above | Status is "<raw>" | raw status |

ipc_call, in this order:

| code | condition | text | detail |
|---|---|---|---|
| `wrapper_inner` | status `wrapper` | Inner invoke of a wrapper; the wrapper's call sites are listed as calls | null |
| `command_unevaluated` | `command` null and status `dynamic`, or an alternative without command | Command name is not evaluated statically | node name without `INVOKE ` |
| `global_form` | `form` `global` | Called through a window.__TAURI__ global, not an imported invoke | null |
| `registry_unproven` | status | A command registry in this repository is not proven; it could be the one that receives this call | null |
| `acl_possible` | status | The app may restrict commands with permissions; whether this call is allowed is not proven | null |
| `ambiguous` | status | More than one Tauri app could receive this call | null |
| `shadow_possible` | status | An unresolved registry entry before the match could take this name | null |
| `unknown_command` | status | No resolved command has this name, but an unresolved registry entry might | null |
| `test_only` | status | Only test-scope or test-only commands have this name | null |
| `unmatched` | status | No registered command has this name | `command`, else the unmatched alternative's command |
| `plugin` | status | Plugin command; plugin commands are not linked | `plugin\|plugin_command` |
| `global_unlinked` | status | Calls through the window.__TAURI__ globals are not linked | null |
| `alternative_linked` | the call has stored call edges | A stored link exists although the status is "<raw>" | null |
| `status` | none of the above | Status is "<raw>" | raw status |

### Schema endpoints

Five read-only endpoints serve the database schema that the index holds
from sqlx SQL migrations (ADR 0048; records: ADR 0047,
[docs/extract/schema.md](../extract/schema.md)) and from model classes
(`source: "model"`, ADR 0054,
[docs/extract/schema-models.md](../extract/schema-models.md)), served as
model groups since T018f (ADR 0055; "T018f additions" below). They follow the API section
conventions above: the store is read after the request arrives, there is no
cache and no freshness walk, the runs are read first and last (one retry,
then 503 `index_changed`), unknown parameters are ignored and a parameter
given twice is 400 `bad_request`. Every response starts with `api_version`
(0), `repository` and `index` (the last run, as `last_run` in
`/api/v0/status`; null when not indexed). There is no scope filter: every
set and table is returned, with the raw `scope` attribute and the set's
derived `test_only`; hiding test-only sets is the client's.
`/api/v0/schema/table` and `/api/v0/schema/migration` take the optional
`editor` of the node-presenting endpoints above (ADR 0050), with the same
400s and precedence and the same `ide.editor` and `ide.source`;
`/api/v0/schema/summary`, `/api/v0/schema/sets` and
`/api/v0/schema/assumptions` present no nodes and carry no links or `ide`
(`editor` there is an unknown parameter, ignored).

**Raw values on the wire.** Stored attributes are returned verbatim inside
each node's `attributes`; the API never renames, defaults or normalises
them. Values the API derives are separate top-level fields with the rules
below, and none turns an unknown into a known value: a missing attribute or
one of an unexpected JSON type derives `unknown` (or null), never `false`,
`0` or a default. **Edges are the only links**: a table belongs to a
migration, a column or index to a table, and a column to another column
only through stored `contains`, `alters` and `foreign_key` edges;
`references.table` is returned as written and never resolved by name.

#### GET /api/v0/schema/summary

The cheap count for the shell's Schema entry, read at load and again when
`/api/v0/status` reports another `last_run.finished_at`. It reads the runs,
the `migration` nodes, the stored file paths, the diagnostics of the
stored `Cargo.toml` files and the diagnostics whose file is not stored
(where the scanner reports what it did not read); never tables, columns or
edges.

```json
{"api_version": 0, "repository": "env-wrappers", "index": {...},
 "found": "none", "unknown_reason": null, "sets": 0, "test_only_sets": 0, "failed_sets": 0,
 "sets_without_records": 0, "unreadable_manifests": 0, "scan_gaps": 0}
```

- `sets`: distinct string `set` values of the stored migrations;
  `test_only_sets`: those whose `test_only` is true; `failed_sets`: the
  number of `failed_sets` entries of `/sets`; `sets_without_records`,
  `unreadable_manifests` and `scan_gaps`: the number of entries of the
  `/sets` lists of the same names. All null when not indexed.
- `found`: `sets` when `sets`, `failed_sets` or `sets_without_records` is
  above 0; otherwise `none` when the last run's indexer is exactly
  `graphvein` with version `extractors-N` (N decimal without leading
  zeros) and N at least 28, the first aggregate version that proves the
  schema pass ran, and `scan_gaps` and `unreadable_manifests` are 0
  (none must be proven: the index shows that no manifest could have been
  missed); otherwise
  `unknown` with `unknown_reason`: `not_indexed` (no run),
  `external_indexer` (another producer: absence says nothing),
  `predates_schema` (`extractors-26` or lower), `version_ambiguous`
  (`extractors-27`: used by builds before and after the schema pass; re-index)
  `version_unreadable` (other text), `scan_incomplete` (a version that
  proves the pass ran, but the scanner did not read a path that may hide a
  manifest: an entry of `scan_gaps`; checked before the next reason) or
  `manifest_unreadable` (a version that proves the pass ran, but a
  `Cargo.toml` no pass could read may declare a set). `unknown_reason` is
  null unless `found` is `unknown`. `none` means "no sqlx migration set
  among the indexed manifests", never "no database". A `Cargo.toml` that
  the repository's ignore rules (`.gitignore`, `.graphveinignore`) or
  Graphvein's default exclusions (`target`, `vendor`, `node_modules`, a
  nested Git repository, ...) exclude is outside the index, so `none` says
  nothing about it.

#### GET /api/v0/schema/sets

Everything a list, grid or diagram needs, in one response: every group of
tables with its migrations, tables, columns, indexes and foreign-key pairs.
No parameters, no paging.

```json
{
  "api_version": 0, "repository": "axum", "index": {...},
  "sets": [{
    "set": "migrations", "source": "migrations", "tool": "sqlx", "package": "Cargo.toml",
    "dialect": "postgres", "conflicts": [],
    "objects": "modeled", "not_modeled_reasons": [],
    "table_list": "may_be_incomplete", "table_list_reasons": ["opaque_statements"],
    "opaque_total": 6, "opaque_by_kind": {"extension": 1, "function_call": 5},
    "test_only": false, "diagnostic_counts": {"schema_opaque_statement": 6},
    "migrations": [{"id": "migration:migrations/2_user.sql#migration", "file": "migrations/2_user.sql",
                    "attributes": {...}, "creates": ["table:migrations/2_user.sql#user"], "alters": []}],
    "tables": [{"node": {...stored table node...}, "completeness": "complete_in_source",
                "created_by": "migration:migrations/2_user.sql#migration", "altered_by": [],
                "columns": [{"node": {...stored column node...}, "default_state": "written"}],
                "indexes": [{...stored db_index node...}],
                "foreign_keys_out": 0, "foreign_keys_in": 5, "unresolved_references": 0}],
    "foreign_keys": [{"source_table": "table:...#follow", "source_column": "column:...#follow.followed_user_id",
                      "target_table": "table:...#user", "target_column": "column:...#user.user_id",
                      "edge": {"kind": "foreign_key", "provenance": "fact", "confidence": 1}}],
    "unattached": {"columns": 0, "indexes": 0},
    "linked_without_edge": 0
  }],
  "failed_sets": [],
  "sets_without_records": [],
  "unreadable_manifests": [],
  "scan_gaps": []
}
```

Groups and order:

- A group is keyed by the stored `set` attribute (exact string, the set
  directory) of its `migration` and `table` nodes; records without a string
  `set` form one group with `set: null`, ordered last; the others in byte
  order. In the `set: null` group every set-level field is null or empty.
  `source` is `migrations` for a group with a migration node, else null.
- `tool`, `package`, `dialect`: the common value of the group's migrations;
  null with the name in `conflicts` when they differ or some lack it.
- `migrations`: those with an integer `order` by `order` (whatever their
  `applied`), then the others by file and identity; `attributes` verbatim,
  `creates` (outgoing `contains` to tables) and `alters` (outgoing
  `alters`) by identity.
- `tables` by qualified name, then identity. `created_by`: the first
  migration by identity with a `contains` edge to the table, or null;
  `altered_by`: sources of `alters` edges, in the group's migration order.
  `columns` are the table's `contains` targets of kind `column` by integer
  `position`, then identity (no integer position last); `indexes` its
  `db_index` targets by qualified name.
- `foreign_keys`: every stored `foreign_key` edge from a column of the
  group's tables, with both ends' tables by `contains` (`target_table`
  null when the target column has none), by source table, source column,
  target column. One entry per column pair: the API does not group pairs
  into constraints. `foreign_keys_out`/`_in` count the stored pairs per
  table over the repository (a self-reference counts in both).
- `external_references` (T024c1, omitted when 0): the table's migration
  reference entries with `status` `external`, a platform table outside the
  set (Supabase's `auth.users`, `storage.objects`, `storage.buckets`,
  assumption SB-V2); they are neither linked nor unresolved. A set group's
  `platform` (omitted when absent) is the migration records' `platform`
  object: `{name, assumption, objects}`.
- `unresolved_references`: the table's reference entries whose target is
  not linked: for a migration column its `references` object plus each
  element of `additional_references` with a `status` other than exactly
  `linked` or `external`; for a model column (T018f Revision 2) each element of
  `foreign_keys` with a `status` other than exactly `resolved` (an
  `unresolved` or `ambiguous` model foreign key). A value of another JSON
  type counts one entry.
- `unattached` (only in the `set: null` group, which then exists even with
  no tables): column and `db_index` nodes without a `contains` edge from a
  table. Expected 0.
- `linked_without_edge`: columns with a `linked` reference entry but no
  stored `foreign_key` edge (an index inconsistency, expected 0).
- `test_only`: true when the group has migrations and every one has `scope`
  exactly `test`; false when it has migrations otherwise; null without
  migrations.
- `failed_sets`: one `{package, message}` per stored `schema_internal_error`
  diagnostic of a `Cargo.toml`, by file then message: a set the schema pass
  failed on, which has no group.
- `sets_without_records`: one `{package, diagnostic_counts}` per stored
  `Cargo.toml`, by file, that carries a `schema_*` diagnostic other than
  `schema_internal_error` and `schema_manifest_unreadable`, has no
  `schema_internal_error` and is the `package` of no stored migration: a
  set the pass found but emitted no record for, for example one whose
  every member is unreadable (not UTF-8 or ignored). `diagnostic_counts`
  counts the stored diagnostics of the manifest and its `sqlx.toml` by
  code. Whether such a set is test-only is unknown.
- `unreadable_manifests`: one `{file, code, message}` per stored
  `schema_manifest_unreadable` diagnostic of a `Cargo.toml` (the schema
  pass could not read it) and per stored scanner `symlink`, `file_read`,
  `file_binary`, `file_not_utf8` or `file_not_regular` diagnostic of a
  path named `Cargo.toml` (the scanner did not read it), by
  file then message; `code` names the diagnostic the entry comes from.
  Whether such a manifest declares a set is unknown.
- `scan_gaps`: one `{file, code, message}` per stored scanner diagnostic,
  in stored order, for a path the scanner did not read that may hide a
  manifest: `scan_read` (a directory could not be listed), `symlink` (not
  followed; may be a directory or a `Cargo.toml`), `invalid_path` (an
  entry with an unsupported path was skipped; `file` is empty),
  `ignore_read` (an ignore file could not be read, so which rules apply is
  unknown) and `file_read`, `file_binary`, `file_not_utf8` or
  `file_not_regular` (a file not read: unreadable, holding a NUL byte, not
  UTF-8, or not a regular file) of a path named `Cargo.toml`. These are
  every code the scanner writes. With an entry, `summary` never says `none`.

Set-level fields. The group's *files* are its migration files, each package
manifest its migrations name, and `sqlx.toml` in each package directory.
The *set record* is the `set_opaque_statements` attribute of its
migrations; it is known when every migration carries an object with an
integer `total` and an object `by_kind` and all are equal.

- `objects`: `modeled` when `dialect` is exactly `postgres` (the dialect
  T018a models), the set record is known and no `schema_migration_order`
  diagnostic is stored for the group's files; otherwise `not_modeled` with
  `not_modeled_reasons` (sorted): `dialect_unsupported`, `order_failed`,
  `not_analysed` (Postgres, no order diagnostic, set record unknown).
- `table_list`: `not_modeled` when `objects` is; else `may_be_incomplete`
  with `table_list_reasons` (sorted) `opaque_statements` (set record
  `total` above 0), `member_unreadable` (a `schema_member_unreadable`
  diagnostic), `unsupported_config` (a `schema_unsupported_config`
  diagnostic), `unsupported_statements` (a migration with an integer
  `unsupported` above 0, or a `schema_unsupported_statement` diagnostic.
  T018a counts several table-creating forms as unsupported, not opaque:
  `CREATE TABLE ... OF type`, `PARTITION OF`, `LIKE` and `INHERITS` emit a
  partial table node (name, no columns); `SELECT ... INTO t` (any INTO of
  a select-like statement), `CREATE TABLE t AS` and `CREATE SCHEMA s
  CREATE TABLE ...` emit no table node at all), `statement_counts_unknown` (a migration whose `applied` is not
  exactly `false` without an integer `unsupported` of 0 or more); else
  `complete`. Per-migration `opaque` counts are not summed: they leave out
  opaque statements the set total includes.
- `opaque_total`, `opaque_by_kind`: the set record's values verbatim; null
  when `objects` is `not_modeled`.
- `diagnostic_counts`: stored diagnostics of the group's files by code.
- `precision` (T020, ADR 0066): the group's scope label `{label, row,
  reason}` as in [GET /api/v0/precision](#get-apiv0precision). A migration
  set is labelled by its migrations' common `tool` and `dialect` (PL-F1:
  `sqlx`, `postgres`), a model group by its tables' common `tool`
  (PL-F2..PL-F8); a conflicting value, any other tool or dialect and the
  `set: null` group fall to row D (`default_branch`). The label is a
  property of the scope, never of one fact: an unknown stays unknown, and
  a `mapped` fact in a precise group keeps its own `verification`.

#### GET /api/v0/schema/table?id=

`id`: a canonical identity of kind `table` (400 when missing, not
canonical or of another kind; 404 "no table with identity ..." when not
stored). Optional `editor` as above. One table in detail. Nodes are
presented as in `/api/v0/api/endpoint` (stored node plus `ide` and
`file_state`).

```json
{
  "api_version": 0, "repository": "axum", "index": {...}, "ide": {...},
  "set": "migrations", "dialect": "postgres",
  "table": {...presented table...}, "completeness": "complete_in_source",
  "columns": [{"node": {...presented column...}, "default_state": "none",
               "foreign_keys": [{"target_column": "column:...#article.article_id",
                                 "target_table": "table:...#article",
                                 "edge": {"kind": "foreign_key", "provenance": "fact", "confidence": 1, "evidence": [...]}}]}],
  "indexes": [{"node": {...presented db_index...}}],
  "history": [{"migration": {"id": "...", "file": "...", "version": "4", "order": 4, "description": "article"},
               "event": "created", "evidence": [{"file": "...", "span": {...}}]}],
  "reasons": [],
  "neighbours": {"outgoing": [{"table": {...stored table node...}, "completeness": "complete_in_source",
                               "pairs": [{"source_column": "...", "target_column": "..."}]}],
                 "incoming": []},
  "unresolved_references": [],
  "truncated": null
}
```

- `set`, `dialect`: the table's attributes when strings, else null.
- `completeness` (the same rule in every response): `complete` when `state`
  is exactly `complete` and `opaque_after` is the integer 0;
  `complete_in_source` when `state` is `complete` and `opaque_after` an
  integer above 0; `partial` when `state` is `partial`; `unknown`
  otherwise.
- `default_state` per column: `written` when `default` is a string;
  `implied` when `default` is null and `default_implied` a string;
  `none` when `default` is null, `default_implied` absent and the table
  `complete` or `complete_in_source`; `unknown` otherwise (partial or
  unknown table, missing `default`, unexpected types).
- `foreign_keys` per column: its stored outgoing `foreign_key` edges by
  target identity, with the target's table by `contains` (or null) and the
  edge's evidence verbatim.
- `history`: a `created` event for the first creating migration (evidence:
  the table's span), then an `altered` event per incoming `alters` edge
  (evidence: the edge's spans), by the migration's integer `order`, then
  identity. Dropped tables and intermediate states are not available;
  renames show in `previous_names` and the `alters` evidence.
- `reasons`: the stored `reasons` entries verbatim, each object with
  `migration_ref` (`{id, file, version, order}` of the named migration, or
  null when it is not stored or the reason is set-level with `migration:
  null`).
- `neighbours`: one hop through stored `foreign_key` edges: `outgoing` the
  tables holding the target columns, `incoming` the tables holding the
  source columns of edges into this table; a self-reference is in both.
  Each neighbour is the stored table node (no columns), its `completeness`
  and its column `pairs`. At most 200 tables per direction by identity;
  `truncated` is null or `{tables_omitted, pairs_omitted, reasons:
  ["neighbours_cap"]}`. `/sets` counts are uncut.
- `precision` is the scope label of the table's `/schema/sets` group
  (same rule, same value).
- `unresolved_references`: `{column, reference}` for each reference entry
  that is not linked (the `/sets` rule above, by the record's source), by
  column position then stored order. `external_references` (T024c1,
  omitted when empty) lists the `external` entries the same way. A Supabase
  table's `rls` and `policies` attributes (SB-M3) are in `table.attributes`. Unresolved references from other tables into this one have no edge
  and are not listed here (they are counted on the referencing table).
- `model_routes` (model tables only, additive; T029, rows MT-20..MT-29 of
  the [route model type claims](../contract/claims/route-model-types.md))
  lists the routes whose stored request/response shape uses the table's
  model, the same declaration node (MT-20); absent for a migration table.

  ```json
  "model_routes": {
    "state": "listed", "row": "MT-25", "class": "class:app/models.py#Hero",
    "routes": [{"route": "route:app/main.py#POST%20%2Fheroes", "method": "POST", "path": "/heroes",
                "pattern": "/heroes", "framework": "fastapi", "scope": null, "model": "Hero",
                "uses": [{"direction": "request", "section": "body", "name": null, "status": null,
                          "content_type": "application/json", "via": [], "nested": false,
                          "provenance": {...shape provenance...}, "row": "MT-21"}]}],
    "routes_total": 7, "routes_with_shape": 7, "routes_not_fully_read": 7,
    "links": {"app/main.py:10": {...IDE link...}}
  }
  ```

  - `state`: `listed` (at least one route); `none` (no route uses it and
    every stored route has a shape whose parameter, body and response
    sections are complete or provably none and whose schemas are read to
    the end, with no object accepting fields it does not list: a proven
    absence among the repository's own routes); `none_found` (no route whose shape was
    read uses it, but `routes_not_fully_read` routes may: a shape not
    stored, a section not read or partial, an unknown schema);
    `not_read` (no route stores a shape, or there is no route: nothing is
    known); `no_declaration` (the table's `class` attribute names no stored
    class node, so no model can be proven to be it; every model tool but
    SQLModel today). `row` is the claim row of the state.
  - `routes`: one entry per route and shape model, by served path (the
    production path, else the pattern), method and identity. `path` is
    chosen as in the endpoint list. Each use is a parameter (`direction`
    `request`, `section` `path`, `query`, `header` or `cookie`, `name`),
    the body (`request`, `body`) or a response (`response`, `responses`,
    `status`), with the shape item's `content_type` and `provenance`;
    `nested` is true when the reference is inside an object's fields, and
    `via` lists the models reached through, outermost first.
  - `links` maps each use's provenance `"file:line"` to its editor link
    (as `shape_links` on the endpoint detail); absent when there is none.

#### GET /api/v0/schema/migration?id=

`id`: a canonical identity of kind `migration` (400/404 as above);
optional `editor` as above. One migration: the presented node, `set`, `previous` and `next` (the migrations
of the same set right before and after it by integer `order`, whatever
their `applied`; null at the ends or without an integer `order`),
`creates` (`{id, name, completeness}` of the tables it holds by `contains`),
`alters` (the same plus the `alters` edge's `evidence`), and `diagnostics`
(the stored diagnostics of its file in stored order as `{severity, code,
message}`, at most 500) with `diagnostics_total` uncut. Messages name the
statement's line and byte span as text; the API does not parse them. A down
migration (`direction: "down"`, `applied: false`) has empty `creates` and
`alters`.

#### T018f additions (ADR 0055)

Additive fields (`api_version` stays 0). An index built before
extractors-30 has no provenance attributes: `claims`, `opaque_sites` and
`rests_on` are then null, never `[]`.

- `/summary`: `model_groups` (distinct stored `set` values of model
  tables; null when not indexed), `sources_read` (`["migrations"]` for
  `extractors-28`, `["migrations", "models"]` from `extractors-29`, `[]`
  for an older graphvein version, null when not indexed or another
  indexer), `unread_tools` (below; null when not indexed). `found` is
  `sets` also when `model_groups` is above 0.
- `/sets`: top-level `unread_tools` and `model_list_reasons` (the
  reasons that hold for the whole repository, whatever its model groups:
  `python_unread`, `python_scan_gap`, as in a model group's
  `table_list_reasons`; `[]` when none; null when `sources_read` lacks
  `models`; the strip's "No model table claimed" card names them). Every group: `tables_by_scope`
  (`{"production": n, "test": m}` by the tables' stored `scope`, so a
  client that hides test tables can say how many), `rests_on` (the union
  of the assumptions behind every claim, reason and opaque site of the
  group, or of a model group's stored `assumptions`; sorted). Every table:
  `rests_on` (the intersection over the rows of every claim of its columns
  and indexes, or a model table's stored `assumptions`: every fact rests
  on these; null when a record has no claims or the table states no
  fact), `overlaps` (`[{table, set, source, columns_only_here,
  columns_only_there}]`: the tables of the other source with the same
  name, L5's rule, on both tables; the column lists only when the
  migration table's `state` and the model table's `column_set` are
  `complete`, else null; `[]` when there is none; on a model table null
  while `unread_tools` is not empty, because a migration tool the index did
  not read may create a table of the same name; on a migration table null
  unless every model table is listed: `sources_read` holds `models`,
  `model_list_reasons` is `[]` and every model group's `table_list` is
  `complete`), `opaque_sites`
  (migration tables: the opaque statements of the set's applied
  migrations after the table's CREATE, `[{migration, span, row, kind}]`;
  null when a migration has no list or the length is not the table's
  `opaque_after`; model tables null).
- Model groups follow the migration groups, one per stored `set` of the
  model tables (G3: `models/<tool>/<root>`; SQLModel's is
  `models/sqlmodel/.` from extractors-31, `models` before), with `source: "models"`, `tool` (common stored tool, else null
  and `conflicts`), `dialect` (the tables' common stored dialect: a
  Prisma group's datasource provider; null for SQLModel, or with
  `dialect` in `conflicts` when they differ), `package` null, `objects: "modeled"`,
  `opaque_total`/`opaque_by_kind` null, `migrations: []`, and `pins`
  (common stored pins, else null with `pins_conflict: true`),
  `assumptions` (union of the tables' lists; null when one lacks it),
  `files` (`[{file, tables, unread, core_tables}]`: files with a model
  table or a `schema_model_unread`/`schema_model_core_tables` count; the
  counts are 0 without a diagnostic on an index that proves the linker
  ran, null when a message's count is unreadable), `diagnostic_counts`
  (the `schema_model_*` diagnostics of the repository's Python files for
  SQLModel's group; for every group the `schema_*` diagnostics of the
  group's files and, **(T018g2 review)** for a non-SQLModel group, those
  whose message ends with `; the table list of model group <set> may be
  incomplete`, wherever they are filed: the Drizzle linker's markers for a
  module without a model payload, an unread table form, a DR-P1 failure
  or a table not claimed; a `<file>:<line>: ...` marker counts one in the
  file's `unread`). `table_list`:
  `may_be_incomplete` with reasons `model_unread`, `model_core_tables`,
  `model_counts_unknown`, `model_not_claimed` (an import-failure or
  not-claimed diagnostic: such a class may still be a table),
  `python_unread` (a `.py` file with `file_too_large`,
  `parse_failed`, `extract_failed`, `file_read`, `file_binary`,
  `file_not_utf8` or `file_not_regular`: its classes were not read),
  `python_scan_gap` (a scanner diagnostic that may hide a `.py` file:
  `scan_read`, `symlink`, `invalid_path`, `ignore_read`, or `file_read`,
  `file_not_utf8` or `file_not_regular` on a path without an extension);
  `complete` only without a reason and with `models` in `sources_read`;
  else null. Model tables: `completeness` `complete`/`partial` by
  `state`, else `unknown`; `created_by` null, `altered_by` and `indexes`
  `[]`; columns' `default_state` `server` (`server_default` true),
  `python` (`default` `scalar`/`callable` and `server_default` false: a
  Python-side default the database does not have), `client` (**(T018g1)**
  `default` `client` and `server_default` false: a value the Prisma
  client generates on insert, such as `uuid()`; the database has none),
  `none` (`default`
  `absent`, `server_default` false, table complete), else `unknown`.
  **(T018g0)** From extractors-31 model columns carry `claims` (one per
  known fact: `{fact, row, file, span, verification}`, with `file` instead
  of a migration; `docs/extract/schema-models.md`) and model tables a
  `verification` (`oracle` or `mapped`); `/table` serves them like
  migration claims, with `migration_ref: null` and `link` to the claim's
  file and line. On an index built before extractors-31 a model column has
  no `claims` attribute and `/table` serves `claims: null` ("not
  recorded"), as for a migration column of an index built before
  extractors-30. The view shows an "unverified" chip on a model table and
  column with a `mapped` fact; SQLModel's facts are all `oracle`.
  **(T018g0 Revision 1)** `/sets` serves a model column's stored node
  without its `claims` (only `/table` serves them, with links) and each
  model table a `verification_counts` object: its columns' claims counted
  by `verification` (`{"oracle": n, "mapped": m}`, a claim without a
  string verification counted as `unknown`; `{}` for a table without
  claims), null on a migration table and when a model column has no
  `claims` list (an index built before extractors-31). The list row of a
  model table with a `mapped` count or table `verification: "mapped"`
  shows an "unverified" mark; the table header shows the chip from the
  counts until `/table` answers, then from the claims.
  **(T018g0 Revision 2)** `verification_counts` is also null for a model
  table whose record has no `verification` (an index built before
  extractors-31, even without columns). The view marks anything that is
  not `oracle` as unverified: a model claim or table verification that is
  `mapped`, missing or any other value, and every count other than
  `oracle`; for a value other than `mapped` the popover says the
  verification is not recorded. The extractor never emits a model claim
  without a known verification (the core refuses it with a
  `schema_model_internal_error` diagnostic).
  **(ADR 0057)** The counts are the table record's stored
  `verification_counts` (written by the linker), served beside the node
  and left out of the node's attributes in `/sets` and `/table` (generic
  node views such as `/source` show the record verbatim); `/sets` reads
  model columns without their `claims` (the store removes them in SQL).
  The counts are null for a model table record without them (an index
  last written before ADR 0057; the next `graphvein index` writes them).
  **(ADR 0065)** A model group whose dialect is chosen at run time adds
  `dialects` (the sorted list every table stores; tables that differ or
  lack it add `dialects` to `conflicts`), omitted for every other group;
  its columns' nodes carry `by_dialect` (per-dialect facts, the flat facts
  null), and any model column may carry `type_family` (`{name, unknown}`:
  a type whose listed modifiers are unknown, `type` null) and `tool_type`
  (the ORM's own type, not a database fact, no claim). `/table` adds
  `dialects` beside `dialect` (omitted otherwise) and serves a
  per-dialect claim with its `dialect`, one per dialect, with `link` as
  any model claim.
- `/table?id=` answers model tables too (`history: []`, `indexes: []`,
  model attributes verbatim). Each column and index carries `claims` (the
  stored list, each with `migration_ref` and `link`, the IDE link to the
  statement's start under the request's editor, null without links; null
  when the record has none). `reasons` entries keep their stored `row`
  (and `fact` for model reasons) and gain `link` (the statement span, or
  a model reason's `file` and `line`). `rests_on`, `opaque_sites` (each
  with `migration_ref` and `link`) and `overlaps` (each with the other
  table's `completeness` and `name`) as in `/sets`.
- `/migration?id=`: `opaque_sites` (the stored list, each with `link`;
  null when the record has none).
- `GET /api/v0/schema/assumptions`: `extractor` (this build's aggregate
  version; the catalogue is the build's, not the index's), `assumptions`
  (`[{id, source, title, text, folded_into}]`: E1-E6 and E8, E7 folded
  into E8, V1, V2, V4, V6, V7, V3 folded into V7; titles and first
  sentences of `docs/extract/schema.md` and `docs/lang/python-models.md`;
  the registered ORM tools' ids; and, with source `table_access`, the
  code-to-table assumptions TA-V1, TA-V2, TA-V3a to TA-V3c and TA-V4 that
  `reads_table`/`writes_table` entries name in `assumes`, with the texts of
  `docs/extract/table-access.md` and short titles; no rows for that source)
  and `rows`, keyed by row source and then id because the contracts reuse
  ids (`{"migrations": {"M4": {source, tool, branch, summary, assumptions}},
  "models/sqlmodel": {"K1b": {...}}, "models/typeorm": {"D": {...}}}`):
  model rows are keyed per tool (`models/<tool>`, `source: "models"`,
  `tool` the tool; `tool` is null on migration rows), so each tool's
  default row D keeps its own text (T018g6 Revision 3 addendum 3); every
  migration row and precondition with
  `contractAssumptions` (`[]` rests on none), every model row with the
  assumptions model tables state. Built once per process. Every served
  string is plain user text (T018f Revision 2): the documents' Markdown
  marks, revision and review references are removed (the documents and the
  Go tables keep them, 1:1), and E7's and V7's texts are written for the
  reader. A migration row's `branch` is its form; a model row's `branch`
  is null (the model contract names the Go function that holds the
  branch, which is no form a reader can use).
- `unread_tools` (Alembic marker rule, from the stored file paths): a
  directory `D` with `D/env.py` and `D/script.py.mako` or a
  `D/versions/*.py` file gives `{"tool": "alembic", "path": D, "marker":
  <nearest alembic.ini in D or an ancestor, or null>, "files": <D/versions/*.py
  without __init__.py>}`; an `alembic.ini` that marks no directory gives
  `path` and `files` null. Prisma Migrate rule (T018g1 review): a
  directory `D/migrations` beside a Prisma schema (`D` holds a `.prisma`
  file) with `D/migrations/migration_lock.toml` or a
  `D/migrations/<name>/migration.sql` gives `{"tool": "prisma-migrate",
  "path": "D/migrations", "marker": <its migration_lock.toml, or null>,
  "files": <migration.sql files>}`; the SQL pass reads sqlx sets only.
  A directory a stored Prisma 7 config names in a literal
  `migrations.path` (the linker's `schema_model_migrations` marker on the
  config; `docs/extract/schema-prisma.md`) is listed the same way, with
  the same evidence, wherever it is; the directory beside the schema is
  still listed when one exists.
  Ordered by path, then marker. An empty list never means "no
  migrations".

#### Reading the schema states

Clients (the schema view) must follow these rules:

- Only `completeness: "complete"` may be shown without qualification.
  `complete_in_source` is "complete as far as the migrations show" with the
  opaque count; `partial` lists `reasons`; `unknown` and any value a client
  does not know are shown as unknown, never as complete.
- Every column and index fact of a table that is not `complete` or
  `complete_in_source` is "last known from source": more columns may exist
  and these may have changed. A partial table may have no columns (LIKE,
  OF, PARTITION OF, INHERITS, an unread body): that is not "no columns". A
  `db_index` node's `unique: true` on a partial table is last known too.
- Null is unknown, never absent: table `primary_key` is the key's columns,
  `[]` for "no primary key" and `null` for "not known"; `unique_keys` is a
  list or `null` (not known); column `nullable`, `unique` and `primary_key`
  are `true`, `false` or `null`, and `null` must never be drawn as
  `false`, "nullable" or "no key". A `false` or `[]` on a
  `complete_in_source` table holds as the modeled statements leave it.
- Migration `applied` is `true`, `false` or `null` ("unknown whether it
  applies", never "not applied"); statement counts and `errors` are null
  when the migration was not analysed, never 0.
- `default_state` `none` means "no default written or implied"; `unknown`
  must not be drawn as "no default".
- `type` is the written text; `type_resolved` exists only for resolved
  serial aliases. Clients do not normalise types.
- `schema: null` means unqualified (resolved through search_path when the
  migrations run), not "no schema" and not `public`.
- `table_list` other than `complete` means the set may hold tables the
  migrations do not show; `objects: not_modeled` means the set's tables were
  not extracted (zero tables is not an empty schema). A non-empty
  `failed_sets` or `sets_without_records` means a set exists that the
  index holds nothing for; a non-empty `unreadable_manifests` means one
  may.
  Summary `found: unknown` is not "none found".
- A foreign key is drawn only from `foreign_keys` (stored edges); a
  reference with another status is an unresolved reference, never a link.

### Change endpoints (ADR 0056)

Design: [ADR 0056](../adr/0056-change-journal-and-impact.md); the claim
contract (rows G, C, D and relations I) is in
the T019 task record (internal). The endpoints answer
"what did a change do to the stored facts, and what depends on them"
through `internal/changes`, the same code `graphvein changes` and the M3
MCP tools call. Every GET response starts with `api_version`,
`repository`, `index` (as in `/api/v0/status`) and `freshness` (the
status calculation; the change endpoints reuse one check for up to 2 s
within the same index state, and `checked_at` is always the instant of the
check reported, so a reused check never claims a later instant).

`base` is `previous` (default: the run before the latest), `run:<id>` (a
journal run still covered), `pin:<name>` or `ref:<base>[..<head>]`. A ref
base compares the merge base of `<base>` and `<head>` (or `HEAD` when no
head is given) with `<head>`'s snapshot, or with the current index when no
head is given; both snapshots must be indexed first (`POST refs`), else 404
`ref_unknown`. Ref names are resolved locally with `git rev-parse`; the
server never accepts a path. Without a head ref the merge base is taken with
the commit the current index recorded, not the live `HEAD`; when `HEAD` has
moved since, `summary.head_commit_differs` is true, and an index that
recorded no commit (made outside git) answers 409 `reindex_needed`. `ref:a...b`
is refused (400). A stored snapshot of another extractor version counts as
not indexed: `POST refs` indexes it again (G4).

A comparison is bound to its head run (G6). The server keeps the last 4
computed comparisons in memory, keyed by the resolved base (a pin's id and
run, a ref pair's commits and snapshot runs and versions) and the head run,
so paging does not recompute and a deleted or re-pointed pin never answers
from a stale comparison (a deleted pin is 404 `base_unknown`). Reads run between two reads of the runs; a run committed in
between repeats the read once, then answers **409** `index_changed` (not
503 as the other endpoints), and so does a cursor from another head run or
filter; a cursor that does not decode is 400 `bad_request`.

| code | status | when |
|---|---|---|
| `bad_request` | 400 | a bad parameter, a cursor that does not decode, `ref:a...b`, a POST body with an unknown or duplicate member |
| `not_found` | 404 | `impact` of an `id` that names no item of the comparison |
| `base_unknown` | 404 | no run, pin or journal of that name (also a deleted pin); the first run (no previous run) |
| `base_expired` | 410 | the journal or pin overlay no longer covers the run (G5) |
| `reindex_needed` | 409 | a ref comparison whose head is the current index made by another extractor version (G4), or a ref base without head ref on an index that recorded no commit |
| `index_changed` | 409 | a run committed during the request, or a cursor of another comparison |
| `ref_unknown` | 404 | a ref name that names no commit, or a ref snapshot not indexed yet |
| `ref_objects_missing` | 422 | a ref job: the commit's objects are not all local (partial clone, also when git's lazy fetch is refused mid-read); nothing is fetched |
| `git_unavailable` | 503 | git is not installed or the root is not a git repository |
| `pin_limit`, `pin_exists` | 409 | `POST pins` beyond `changes.max_pins`, or a name in use |
| `pin_unknown` | 404 | `POST pins/delete` of an unknown name |
| `index_locked` | 409 | a POST while the index lock is held |
| `index_lock_unavailable` | 503 | a POST on a server without a lock or writer store |

The POST paths follow ADR 0052's rules: `http.CrossOriginProtection`,
`Content-Type: application/json` (else 415), a body of at most 4 KiB with
exactly the listed members (else 400), 409 while the index lock is held.

#### GET /api/v0/changes/bases

The journal runs still covered (newest first), the pins, the ref
snapshots and the retention settings (`changes.*` in
[configuration](../config.md)).

```json
{
  "api_version": 0,
  "repository": "app",
  "index": {"finished_at": "2026-11-13T23:30:38.567819705Z", "indexer": {"name": "graphvein", "version": "extractors-32"}, "contract_version": 1},
  "freshness": {"state": "fresh", "checked_at": "2026-11-13T23:30:40.589952288Z", "changed_count": 0, "changed_files": [], "changed_files_truncated": false},
  "runs": [
    {"seq": 2, "run_id": "6Z2HP2FAVEAC76ZPCR7XTN7AYK", "previous_run": "T4HWU6N5ITYXHNPMUA4VJDI6J7",
     "indexer": {"name": "graphvein", "version": "extractors-32"}, "finished_at": "2026-11-13T23:30:38.567819705Z",
     "counts": {"edge": {"added": 6, "changed": 13, "removed": 12}, "file": {"added": 2, "changed": 8, "removed": 1}, "node": {"added": 7, "changed": 25, "removed": 7}},
     "entry_bytes": 37782, "gaps": ["web/broken.ts"], "gaps_truncated": false, "gaps_fileless": false},
    {"seq": 1, "run_id": "T4HWU6N5ITYXHNPMUA4VJDI6J7", "initial": true,
     "indexer": {"name": "graphvein", "version": "extractors-32"}, "finished_at": "2026-11-13T23:30:38.374091665Z",
     "counts": {"edge": {"added": 85, "changed": 0, "removed": 0}, "file": {"added": 21, "changed": 0, "removed": 0}, "node": {"added": 76, "changed": 0, "removed": 0}},
     "entry_bytes": 0, "gaps": [], "gaps_truncated": false, "gaps_fileless": false}
  ],
  "pins": [
    {"id": "GIBREPBE532B7UMSWFRGTIATUZ", "name": "release-1", "seq": 1, "created_at": "2026-11-13T23:30:38.401551915Z",
     "run": {"seq": 1, "run_id": "T4HWU6N5ITYXHNPMUA4VJDI6J7", "initial": true, "indexer": {"name": "graphvein", "version": "extractors-32"},
             "finished_at": "2026-11-13T23:30:38.374091665Z", "counts": {}, "entry_bytes": 0, "gaps": [], "gaps_truncated": false, "gaps_fileless": false}}
  ],
  "refs": [],
  "retention": {"journal_runs": 30, "journal_budget": 1, "max_pins": 5, "max_ref_snapshots": 4}
}
```

A ref snapshot entry is `{"namespace", "commit", "extractor", "created_at",
"last_used_at", "config_source", "run"}` (`config_source`: `ref`,
`working_tree` or `none`).

#### GET /api/v0/changes/summary?base=

Both sides, `extractor_changed` (indexer versions differ: unchanged files
may change as `extractor_or_relink`), `partial` (a side has knowledge gaps
or is wholly partial: `gaps_truncated`, `gaps_fileless`, `gaps_unknown`),
counts per group (items per change kind, cause and kind; `moves` are
records equal once spans are dropped; `other_changes` differ outside the
row's compared fields; `context` are changed records that are no item of
their own: file nodes, the cause context of the items in their file, and
the `exposes_route`, `alters`, `reads_table` or `writes_table` edges of a
route, migration or code symbol that is itself added or removed; `hidden_tests` are test-scope items), `locations`
(L5 location nodes, counted only), `file_metadata_only` (file records whose
size or git blob id differ while the LF-normalised content hash is equal:
line-ending conversion, never a change), `not_extracted` groups (never zero)
and `extractor_recheck` (open point O7: the ref comparison that separates an
extractor upgrade from relinking, offered when both runs' commits are known
and clean). Groups are always listed, in the order `routes`,
`client_calls`, `api_operations`, `ipc`, `server_actions` (T024b R1), `migration_tables`,
`model_tables`, `migrations`, `code`, `angular`, `react`, `architecture`, `files`,
`gaps`, `other`, `unclassified` (two shown):

```json
{
  "api_version": 0,
  "repository": "app",
  "index": {"finished_at": "2026-11-13T23:30:38.567819705Z", "indexer": {"name": "graphvein", "version": "extractors-32"}, "contract_version": 1},
  "freshness": {"state": "fresh", "checked_at": "2026-11-13T23:30:40.624064408Z", "changed_count": 0, "changed_files": [], "changed_files_truncated": false},
  "base": "pin:release-1",
  "summary": {
    "contract_version": "T019-r8",
    "base": {"kind": "pin", "repository": "app", "pin": "release-1", "run": "T4HWU6N5ITYXHNPMUA4VJDI6J7", "finished_at": "2026-11-13T23:30:38.374091665Z",
             "dirty": null, "extractor": {"name": "graphvein", "version": "extractors-32"}, "partial": false, "partial_reasons": [], "gaps": [], "gaps_truncated": false},
    "head": {"kind": "current", "repository": "app", "run": "6Z2HP2FAVEAC76ZPCR7XTN7AYK", "finished_at": "2026-11-13T23:30:38.567819705Z",
             "dirty": null, "extractor": {"name": "graphvein", "version": "extractors-32"}, "partial": true, "partial_reasons": [], "gaps": ["web/broken.ts"], "gaps_truncated": false},
    "extractor_changed": false,
    "head_commit_differs": false,
    "partial": true,
    "groups": [
      {"group": "routes", "items": 2, "changes": {"changed": 1, "removed": 1}, "causes": {"code": 1, "relink": 1},
       "moves": 4, "other_changes": 0, "context": 1, "hidden_tests": 0, "kinds": {"route": 2}},
      {"group": "client_calls", "items": 7, "changes": {"added": 1, "changed": 2, "link_lost": 3, "removed": 1}, "causes": {"code": 7},
       "moves": 0, "other_changes": 0, "context": 0, "hidden_tests": 0, "kinds": {"calls_route": 3, "client_call": 4}}
    ],
    "locations": 0,
    "file_metadata_only": 0,
    "not_extracted": ["architecture"],
    "extractor_recheck": null,
    "total": 52
  }
}
```

#### GET /api/v0/changes/items?base=&group=&change=&cause=&kind=&q=&tests=&raw=&cursor=&limit=

Items in a stable order (group, change, identity); `limit` 1 to 500
(default 100); `cursor` is the previous page's opaque `next_cursor`.
`tests=true` includes test-scope items (G7; `hidden_tests` counts the left
out ones); `raw=true` adds both stored payloads (`raw`). `q` matches a
substring of the identity or name. Each item has `id` (the address for
impact: a node identity, or `file:<path>`, `edge:<key>`, `gap:<path>`),
`row` (the contract row that produced it), `change` (`added`, `removed`,
`changed`, `body_changed`, `unknown_now`, `known_now`, `link_gained`,
`link_lost`, `link_unknown_now`, `became_partial`, `recovered`,
`unclassified`), `before` and `after` (file, span and the stored fields the
row compares; `null` = absent on that side), `fields` (changed fields, each
`changed`, `unknown_now` or `known_now`), `labels` (`edited`,
`verdict_changed`, `alters_changed` for migrations; `handler_changed` for
routes; `alters_changed` also on a listed table the changed `alters` edge
connects; `table_access_changed` for a code symbol whose `reads_table` or
`writes_table` edges changed, with the tables gained and lost in a field
named after the edge kind), `reason` (why a link was lost: `target_removed`,
`target_changed`, `call_changed`, `call_absent` (the call was removed); or
why a claim was
downgraded: `gap`, `table_not_complete`, `table_list_not_complete`,
`base_table_list_unknown`, ...; `excluded` for a removed file the head tree
still has: ignore rules, the size limit or its language keep it out of the
index), `wording` of a removal (`no_longer_in_index`; `deleted` only for a
file the head commit's tree, or the working tree for the current index,
lacks; `call_removed` for a link lost with its call), `cause` (`code`,
`relink`, `extractor_or_relink`), `provenance` and `confidence` (edges),
`hints` (inferred move hints, C14), `raw` with `related` (the changed
`exposes_route` or `alters` edges reported with a route or migration) and
`impact_count` (null until the impact
of the item was computed). `after.ide` is the editor link, only when the
head is the current index and the file is unchanged since it was indexed.

```json
{
  "api_version": 0,
  "repository": "app",
  "index": {"finished_at": "2026-11-13T23:30:38.567819705Z", "indexer": {"name": "graphvein", "version": "extractors-32"}, "contract_version": 1},
  "freshness": {"state": "fresh", "checked_at": "2026-11-13T23:30:40.633478753Z", "changed_count": 0, "changed_files": [], "changed_files_truncated": false},
  "base": "pin:release-1",
  "items": [
    {
      "id": "edge:[\"calls_route\",\"client_call:web/api.ts#removeLegacy.DELETE%20%27%2Fapi%2Flegacy%27\",\"route:server/app.ts#DELETE%20%2Fapi%2Flegacy\"]",
      "group": "client_calls", "change": "link_lost", "row": "C5", "record": "edge",
      "identity": "[\"calls_route\",\"client_call:web/api.ts#removeLegacy.DELETE%20%27%2Fapi%2Flegacy%27\",\"route:server/app.ts#DELETE%20%2Fapi%2Flegacy\"]",
      "kind": "calls_route",
      "name": "client_call:web/api.ts#removeLegacy.DELETE%20%27%2Fapi%2Flegacy%27 -> route:server/app.ts#DELETE%20%2Fapi%2Flegacy",
      "before": {"file": "web/api.ts", "span": {"start_line": 10, "start_column": 9, "end_line": 10, "end_column": 51}, "fields": {"confidence": 0.9, "provenance": "inferred"}},
      "after": null,
      "fields": [], "labels": [],
      "reason": "target_removed", "wording": null, "cause": "code",
      "provenance": "inferred", "confidence": 0.9, "test": false, "hints": [], "impact_count": null
    }
  ],
  "total": 7,
  "hidden_tests": 0,
  "next_cursor": "eyJjIjoiODE5ZjMyOWUzZTQzOGFlYzE5YTliZTQwIiwiZiI6IjFhMmIzYzRkNWU2ZjcwODEiLCJvIjoxfQ"
}
```

An edge's id is `edge:` plus its key, the JSON array `[kind, source,
target]`; percent-encode it in a query string.

A route item's side has `path_withheld: true` (additive, absent
otherwise; ADR 0089 Revision 5) when that side has no full path and is a
registration whose path the Go linker withholds: its
`route_mount_unresolved` diagnostic names the route's span (couldn't-read
row CR-44). It is read from the diagnostics of the side's graph: the head
side always, the base side only for a ref base (a journal run keeps no
diagnostics, so its side is never marked).

Each item also carries `precision` (T020, ADR 0066), computed by the
server so a client never joins labels itself:

```json
"precision": {
  "file": {"language": "sql", "precision": "best_effort", "precision_reason": "default_branch", "precision_row": "D", "reported_precision": null},
  "scope": {"label": "precise", "row": "PL-F1", "reason": null},
  "fields": {"columns": {"label": "precise", "row": "PL-F1", "reason": null}},
  "scope_side": "head"
}
```

- `file` is the file label of the item's file (head side, else base side)
  as in `/source`, read from the head side's stored file record; all null
  for a run or pin head (no graph of its own) or a file without a record.
- `scope` is the label of the item's record family: a schema record's
  `/schema/sets` group (a column or index by its containing table), a
  route, client call, IPC command or IPC call and their edges
  (`exposes_route`, `calls_route`, `exposes_command`, `invokes_command`),
  `reads_table` and `writes_table` edges (PL-F9), code `calls` edges
  (family `code_calls`, never covered by the file label); null for a
  structural record (file, function, `contains`, ...), whose file label
  applies. A
  schema record whose group the head side cannot show (a removed record, a
  head without a graph) is `best_effort` with reason `scope_unknown`.
- `fields` maps each changed field to its family's label: the item's
  `scope`, except a field named `table_access` (PL-F9); null where the file
  label applies.
- Labels use the head run; when the item has a base side and the base run
  is external or of another extractor version, that refusal applies.
  `scope_side` names the side whose run decided `scope` and `fields`:
  `head`, or `base` in that case (reindexing relabels the head, never a
  stored base).

#### GET /api/v0/changes/impact?base=&id=&group=&depth=&cursor=&limit=

The traversal from one item (`id`) or every seed item of a group
(`group`; exactly one of them). Seeds are the node items of rows C2 to C13,
C16, C17 and C18; other items answer 400. Breadth-first over the relation rows I1
to I10 and I14 to I16 in their direction (I8, T019 Revision 4: from a table the incoming
`reads_table` / `writes_table` edges to the code using it; from a column
its table's edges, the step's `access.column_use` `"not_extracted"`), `depth` 1 to 10 (default 3; at most 6 before T042), at most 2,000
reached nodes and 200 dependents per node; each binding cap is listed in
`caps`. A removed seed takes its first hop through the removed edges of the
change set (`removed: true`) and continues in the graph that still holds its
dependents. Each path is the shortest one to a dependent, with its steps
(edge kind, relation, provenance, confidence, evidence), `provenance`
(`fact` only when every step is and none is I15, whose passed callback
proves no call), `confidence` (the weakest step) and
`claim` ("depends on" for a fact path, "may depend on" otherwise; never
"breaks"); a path through a node in a gap file is inferred too. A path
that reaches a route also has `route_paths` (T025, additive): the route
node's composed paths as stored (ADR 0013), each `{"template", "scope",
"trailing_slash"}` (`scope` `"test"` or null; `trailing_slash` true when
the served path ends in a `/` the normalized template drops, FastAPI only,
ADR 0019; entries differing only in it are distinct paths), so a client can
show the full path with router prefixes instead of the path as written in
the route's file; an empty list means no full path is known: when
`route_shadowed` is true an earlier mount takes every composed path of
the route first (route attribute `shadowed_paths`), so it is mounted and
its paths are withheld; when `route_withheld` is true the route is a
registration whose path the Go linker withholds (its
`route_mount_unresolved` diagnostic names the route's span, couldn't-read
row CR-44; ADR 0089 Revision 5); both absent otherwise. Other paths have no `route_paths` member,
and neither does a route the graph it was read from no longer holds.
`route_paths` holds full paths only: a reached route's paths whose prefix
the code does not fix (T033 rows UP-29, UP-32; a Next.js `basePath` not
read, NX-R4) are in `prefix_unknown_paths` (absent when there is none),
each `{path, above, template, trailing_slash, scope, reason, hop}` with
`path` the `…` form of the routes list. A `calls_route` step carries
`link_assumptions` (absent when there is none): the assumptions its link
rests on, read from the client call, each `{id, text}`: `origin` (an origin
nothing proves to be the server, LF-1) or `UP-V1`, `UP-V2` (the
unknown-prefix tier, or a Next.js route whose `basePath` is not read).
`wording`
is the last edge's I-table wording ("calls this route", "documents this
route", "references this column", "alters this table", "renders this", ...),
else the relation's; an I8 edge reads "reads this table" / "writes this
table" when a fact, "may read / may write this table" when inferred, and
"builds a query on this table" for a statement the owner builds but does
not run. An I8 step also has `access`, the owner's `table_access` entry
for the edge: `tool`, `role`, `operations`, `rows`, `verification`
(`oracle` or `mapped`) with `verification_wording` ("verified by an
oracle", "mapped, not verified"), `assumes`, `column_use` (other steps
have no `access` member). An I16 step (T029 W2: a changed model
declaration to a route whose shape uses it, incoming `uses_model`; rows
MT-13..MT-19 of the [route model types
contract](../contract/claims/route-model-types.md)) has `model`: the
declaration's `name` and the route's `uses` of it, each with `section`,
`status` (a response's, else null), `kind` (`fact`, `intended`,
`returned` or `mapped`), `rule`, `file`, `line` and `confidence`; its path
reads "through request body (fact) model UserIn" or "through response
(returned) model Pet" (other steps have no `model` member). A route whose
`uses_model` edges changed is a C3 item labelled `models_changed`, with
the declarations gained and lost in the field `model_edges`. `table_uses` has one entry per table whose
users the traversal read: `found` (the owners with an edge to it,
`wording` "uses found": a lower bound, never "no users") and
`may_also_be_used_by` (inferred: owners whose `table_access_unknown`
entries name the table as a candidate, with `row`, `line`, `tool`,
`reason`, `partial`, and the owners of unbound entries, whose candidates
are null, that may reach it: `unbound: true`; an unbound SQLModel, Prisma
or Drizzle entry reaches only that tool's model tables, an entry of any
other tool every table; owners already found are left out). `unknowns` lists what is not impact: blind spots (unresolved call sites in any file
whose callee name matches a seed or reached function, ADR 0009), near
misses of a seed or reached route (ADR 0042), nodes in gap files,
`untraversed` edge kinds with counts (row ID: incoming edges no relation
follows, and unfollowed outgoing `exposes_*` edges) and `not_extracted`
relations (I8 for tables and columns of a source no code-to-table row
covers: a SQLite or MySQL migration set, a model tool without rows; I11:
architecture components). `paths` is paged like items (`limit`
default 100, `total`, `next_cursor`); the traversal is cached with the
comparison, and the item's `impact_count` is set from then on.

```json
{
  "api_version": 0,
  "repository": "app",
  "index": {"finished_at": "2026-11-13T23:30:38.567819705Z", "indexer": {"name": "graphvein", "version": "extractors-32"}, "contract_version": 1},
  "freshness": {"state": "fresh", "checked_at": "2026-11-13T23:30:40.644077548Z", "changed_count": 0, "changed_files": [], "changed_files_truncated": false},
  "base": "pin:release-1",
  "seeds": ["function:server/handlers.ts#listItems"],
  "depth": 3,
  "paths": [
    {
      "node": "route:server/app.ts#GET%20%2Fapi%2Fitems", "kind": "route", "seed": "function:server/handlers.ts#listItems",
      "distance": 1, "relation": "I4", "claim": "depends on", "wording": "handled by the changed code",
      "provenance": "fact", "confidence": 1,
      "route_paths": [{"template": "/api/items", "scope": null, "trailing_slash": false}],
      "steps": [
        {"kind": "exposes_route", "source": "function:server/handlers.ts#listItems", "target": "route:server/app.ts#GET%20%2Fapi%2Fitems",
         "relation": "I4", "provenance": "fact", "confidence": 1,
         "evidence": [{"file": "server/app.ts", "span": {"start_line": 7, "start_column": 0, "end_line": 7, "end_column": 32}}],
         "via": null, "removed": false}
      ]
    }
  ],
  "unknowns": {
    "blind_spots": [],
    "near_misses": [],
    "gap_nodes": [],
    "untraversed": {"contains": 2, "exposes_route": 1},
    "not_extracted": [{"relation": "I11", "nodes": []}]
  },
  "caps": [],
  "table_uses": [],
  "total": 3,
  "next_cursor": "ODE5ZjMyOWUzZTQzOGFlYzE5YTliZTQwAGZ1bmN0aW9uOnNlcnZlci9oYW5kbGVycy50cyNsaXN0SXRlbXMAADMAMQ"
}
```

#### POST /api/v0/changes/pins and POST /api/v0/changes/pins/delete

`POST pins` with `{"name": "...", "run": "<run id>"}` (`run` optional: the
newest journal run; `name` 1 to 200 bytes) pins a journal run under the
index lock and answers 201:

```json
{"api_version": 0, "repository": "app",
 "pin": {"id": "A2ZG6QIGGISEXWEKMR5RQVUOGA", "name": "before-refactor", "seq": 2, "created_at": "2026-11-13T23:30:40.653248854Z",
         "run": {"seq": 2, "run_id": "6Z2HP2FAVEAC76ZPCR7XTN7AYK", "previous_run": "T4HWU6N5ITYXHNPMUA4VJDI6J7",
                 "indexer": {"name": "graphvein", "version": "extractors-32"}, "finished_at": "2026-11-13T23:30:38.567819705Z",
                 "counts": {"edge": {"added": 6, "changed": 13, "removed": 12}, "file": {"added": 2, "changed": 8, "removed": 1}, "node": {"added": 7, "changed": 25, "removed": 7}},
                 "entry_bytes": 37782, "gaps": ["web/broken.ts"], "gaps_truncated": false, "gaps_fileless": false}}}
```

`POST pins/delete` with `{"name": "..."}` answers
`{"api_version": 0, "deleted": "before-refactor", "repository": "app"}`.

#### POST and GET /api/v0/changes/refs

`POST refs` with `{"base": "<ref>", "head": "<ref>"}` (`head` optional)
resolves both names (404 `ref_unknown`, 503 `git_unavailable`), takes the
index lock and starts a job that indexes the merge base and the head commit
into their ref namespaces (only those without a snapshot), answering 202
with the GET document and `Location: /api/v0/changes/refs`. `GET refs`
reports the latest job; `state` is `running`, `committed`, `failed` (with
`error.code`, for example `ref_objects_missing`) or `cancelled`:

```json
{"api_version": 0, "repository": "app",
 "job": {"id": "Q3P7ZQ6M2V5D4WJ3T4H4J6B4XA", "state": "committed", "started_at": "2026-11-13T23:31:02.1Z", "ended_at": "2026-11-13T23:31:03.4Z",
         "base": "ref:main..feature", "commits": ["0d8c4f7e5b1a2c3d4e5f60718293a4b5c6d7e8f9", "9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b"],
         "indexed": ["0d8c4f7e5b1a2c3d4e5f60718293a4b5c6d7e8f9"], "error": null}}
```

With no job yet, `job` is `null`.

### Architecture endpoints (T026, ADR 0079)

Three read-only endpoints serve the component model of the architecture
graph and its chains. Components, kinds, visibility, deploy markers, links and annotation
cells are computed from stored facts on every request and never stored, by
the claim contracts of `docs/contract/claims/architecture-graph.md` (rows CM,
CK, CV, CD, CE, AP, AA, AT, AE; every value names its row). They follow the
API section conventions above: no cache, runs read first and last (one
retry, then 503 `index_changed`), unknown parameters ignored, a parameter
given twice is 400 `bad_request`, and every response starts with
`api_version` (0), `repository` and `index`. `graphvein.yaml` is read on
every request (test scope and `architecture.hide`); a configuration error is
400 `bad_request`.

#### GET /api/v0/architecture?scope=

`scope` is `production` (default) or `all`; production leaves out every
edge whose source is test scope (a node with `scope: "test"` or a file the
test-scope rules classify) and counts them in `hidden.test_edges` by
family. The response:

- `manifests`: `read`, or `not_read` with `notice` when the index states no
  `package` node and no `component_manifest_unread` diagnostic (an older
  index or another indexer): every manifest directory is then an unread
  component (CM-U1).
- `components[]`: `id` and `directory` (the manifest directory, `.` for the
  root), `name` (the package names, else the directory), `state` (`read`;
  `unread` for a CM-U1 directory, kind `unknown`; `unassigned` for the root
  that holds the files under no manifest, CM-R1), `manifests[]` (`file`,
  `package` identity, `ecosystem`, `name`, or `unread` with the reason),
  `kinds[]` (`server`, `tauri_core`, `client`, `schema_source`, `binary`,
  `docs_site`, `package`, `unassigned_files` for the unassigned root (CK7,
  never folded), `unknown`), `frameworks[]` (of its routes),
  `visibility` (`shown`, `folded` into the libraries card, `hidden`) with
  `hidden_by` (`tests`, `docs`, `configured`), `rows[]` (the CK and CV rows
  that applied), `markers[]` (`kind` `dockerfile`, `tauri_app` or
  `compose_service`, `label`, `file`, `row`, and for CD4 `basis`: Compose's
  default merge or the assumption that two compose files are combined,
  also appended to `label`; a marker never makes a link),
  `ingress[]` when a compose service CD3 maps here has a Traefik router
  with `tls=true` (AT2, O3 (a): `router`, `service`, `file`, `as_written`
  `rule`/`entrypoints`/`cert_resolver`, `row`, `text` "ingress TLS at
  router <r> as configured in <file> (service <s>)", plus `basis` when the
  service maps by CD4; omitted when empty)
  and `counts` (files, test files, manifests, and its non-test routes,
  client calls, IPC commands, IPC calls and tables; `wrapper_calls`: client
  and IPC calls with status `wrapper`, a wrapper's forwarding call, which
  are neither in the call counts nor in the ghost, CE1/CE2). An ingress is
  omitted, and listed in `not_proven` as `ingress_ignored`, when Traefik's
  Docker provider ignores the service (`traefik.enable=false`, or
  `exposedbydefault=false` without `traefik.enable=true`).
- `links[]`: one per (source component, target component, family):
  `family` `http` (CE1), `tauri_ipc` (CE2), `sql` (CE3, target
  `database:<set>`), `in_process` (CE4, both directions bundled: `count`
  from `source`, `back_count` back) or `server_action` (CE7: `calls_action`
  into a Next.js server action of another component, members named by the
  action; protocol `server action` (AP5), `auth ?`, `TLS ?`; TOP-2 of 2026-11-18), `by_kind` edge counts, `tiers`
  (`fact` or `inferred <confidence>`, the confidence in the UI's one
  number format: a whole number with one decimal, `inferred 1.0`, every
  other value in its shortest form, `inferred 0.875`) and `weakest`, `methods`, `members[]`
  for HTTP and IPC (each stored edge with its route method, path (the
  pattern as written) and `full_path` (the route's path as served, as the
  chain's), or command, tier and the member's auth text; an HTTP member
  whose route has no full path has `prefix_path`, the `…` form of the
  routes list (T033 row UP-33), and an HTTP member whose link rests on
  assumptions has `assumptions`, each `{id, text}` as on a change impact
  step's `link_assumptions`), `origins[]` (AT3: https
  origins of client calls and spec servers, labelled documented, never the
  link's TLS) and `cells` (`protocol`, `auth`, `tls`). A cell has `value`,
  `state` (`proven`, `as_written`, `documented`, `as_configured`,
  `not_applicable`, `unknown`, `mixed`), `source` (the label the UI shows: `proven: ...`,
  `as written`, `documented`, `unknown: <reason>`), `rows[]` and, when
  members differ, `counts` per value. `state` is `proven` exactly when the
  source is proven. Auth today: Express route middleware as written (AA1),
  OpenAPI `security` as documented (AA2), FastAPI security dependencies
  "declares <class> (<scheme name>)" with ", optional (auto_error=False)"
  when written so (AA3, source `declared`) and hey-api generated `security`
  "generated client documents <entries>" (AA4, source "documented by the
  generated client, as written in <file>"), Express `use` middleware
  "middleware <name>, mounted at <prefix>" (AA5) and Axum layers
  "<layer|route_layer> <expression>" (AA6), both as written, with
  "declares <library> (<detail>)" for the closed auth-library lists and
  "... ? (n unread forms)" for unknown entries (docs/extract/auth.md); every
  other link is `auth ?` with its reason. TLS is `TLS ?` unless in-app (`n/a, in-app`); an HTTP
  link into a component with `ingress[]` keeps `TLS ?` and its source
  points to that ingress (rows `ATD`, `AT2`).
- In project mode (`serve --project`, TOP-2 of 2026-11-18, rows CE8 and
  CH13) a member's answer adds `project`: `project`, `link_pass` (`state`
  `none`, `fresh` or `stale`, `pass`, `stale_reasons`), `links_precision`
  (never precise), `members[]` (the muted card of each other member a link
  reaches: `id` `repository:<name>`, `repository`, `name` "<name> (other
  repository)", `row` CE8, `calls_out` and `calls_in`, and
  `operations_out` and `operations_in`: the distinct OpenAPI operations a
  stored `documents_route` project link connects, this repository's to the
  member's routes and the member's to this repository's routes; a member
  only such links reach gets a card too), `across[]` (per
  component: `linked_across`, its calls without a repository link that a
  stored project link connects to another member, `repository_not_linked`,
  the repository's own count of calls without a link, `operations_across`
  and `repository_no_route` for OpenAPI operations a stored `documents_route`
  project link connects to another member's route (they leave the "documented,
  no route" entry and are a `documents[]` row, `row` CE8, with that member's
  card), and `repositories`)
  and `assumptions[]`. Such calls are no longer in the component's ghost or
  its Not proven entry. Each stored cross-repository `calls_route` link is a
  member of a CE8 link (`family` `http`, `row` CE8) between the component
  and `repository:<name>`, outgoing or incoming, with `repository`,
  `assumptions[]` (the stored assumption texts), `assumes[]` and
  `stale_reasons[]` when set, and `source_repository` / `target_repository`
  on the member whose call or route is the other member's node. A chain
  (`/architecture/chain?repository=`) crosses into that card as far as the
  stored link: a call's linked route (down) or a route's linked calls (up)
  as one step with `repository` set and id `repository:<name>/<identity>`,
  and an end line (`row` CH13) "continues in <name>: ..." with the link's
  assumption; a component chain stops at the card with the same end line.
  A single-repository server never sends `project`.
- `databases[]`: one card per schema set (`set`, `tool`, `tables`, `engine`
  cell: `PostgreSQL` only from a stored migration dialect (AE1); else, with
  no stored dialect, `<image> <tag>` in state `as_configured` when the
  compose services of the accessing components name exactly one database
  image of the same file (AE2, "as configured in <file> (service <s>)");
  else `engine ?`, with every deploy fact listed when they conflict;
  `evidence[]`: driver dependencies of the accessing components'
  manifests (AE3), deploy facts that do not set the cell, and database
  images "not tied to this card", never the engine), `accessors[]` per component (`reads`,
  `writes`, `builds`: edges of a query builder's entry, `table_access` role
  `builds`, worded "builds a query on" as on the chain and not counted in
  `reads`/`writes`; `may_also_use`: table-access sites bound to no table).
  The engine cell's AE1 source names where the dialect is stored ("proven:
  migration dialect", "proven: ORM model dialect (<tool>)").
- `stubs[]`: one muted stub per component that imports external packages
  (standard libraries and unresolved names excluded): `packages`, `text`,
  and a `protocol` cell that is always `protocol ?` (CE6, APD).
- `ghosts[]`: per component and family, calls with no stored link, by
  status (CE1: `dynamic`, `unmatched`, `ambiguous`, `test_only`; CE2: any
  IPC call that is not `linked`).
- `documents[]`: `documents_route` and `calls_operation` counts per
  component pair (CE5; no map link).
- `hidden`: components hidden by tests, docs and configuration, and the
  test-scope edges left out. `libraries`: the folded components and the
  in-process counts into and out of the card.
- `not_drawn`: edges counted but never drawn (CED): edges of the families'
  kinds inside one component, and every `renders`, `injects`, `routes_to`,
  `loads_children`, `uses_config`, `foreign_key` and `alters` edge.
- `not_proven[]`: `kind` (`component_unread`, `calls_not_linked`,
  `documented_no_route`, `may_also_use`, `service_not_mapped`,
  `ingress_not_mapped`, `database_not_tied` (no card exists)), `row`,
  (`service_not_mapped` texts end with the reason: image only, Dockerfile
  in no component directory, interpolated path, no same-name service maps),
  `component`, `text`, `count`; a `service_not_mapped` entry also has
  `service`, `file` and `reason`, and `database` when AE2 ties the
  service's database image to that card (the service still maps to no
  component); a `may_also_use` entry has `database`, its card.

No string the endpoints derive says protected, secure, authenticated,
encrypted or enforces.

#### GET /api/v0/architecture/component?id=&editor=

The expanded content of one component (`id` is its directory; unknown: 404
`not_found`; missing: 400): `component` as above, `files[]` (`path`,
`test`), `routes`, `calls` (client and IPC calls), `commands`, `tables`
(presented nodes with IDE links, as the API section), `unlinked` (non-test
calls without a link, by status) and `external_packages[]` (identity,
ecosystem, name and import count from non-test files: the list behind the
stub, O2 (a)).

#### GET /api/v0/architecture/chain?id=&direction=&depth=&scope=

The chain of one start (T026, rows CH1-CH12 and CHD), in both
directions with all branches, on the breadth-first walk change impact uses
(`internal/traverse`, ADR 0056's caps). `id` (required, else 400) is a node
identity (any kind: a function or method, route, client or IPC call, IPC
command, table, file, ...) or a component directory as `/architecture`
lists it; neither: 404 `not_found`. `direction` is `up`, `down` or `both`
(default); `depth` is 1 to 10 (default 6; T042 raised the maximum from
6, the default stays); `scope` is `production`
(default) or `all` (test scope followed too).

- Down follows `calls` to callees (CH1), a function's client and IPC calls
  (CH2), `calls_route` to routes (CH3), a route's handler (CH4), IPC
  `invokes_command` and the command's handler (CH5), `reads_table` and
  `writes_table` to tables (CH6), functions nested in a function (CH7,
  dashed, "contains, not a call") and Angular `renders`, `injects`,
  `routes_to`, `loads_children`, React `renders`, react-router `routes_to`
  and `passes_function` (CH8, dashed; `passes_function` reads "passed as
  callback, call not modelled", T026). Up follows the same edges
  backwards; an inner function reaches its enclosing function (CH7) only
  when it has no stored caller.
- A file start (CH9) walks from every node of the file at once (step 0
  holds them); a component start (CH9) walks the component links of
  `/architecture` (CE1-CE4 and the SQL links to `database:<set>` cards)
  transitively, members of kind `component` or `database`.
- Test-scope members (node `scope: "test"` or a test-scope file) are not
  followed in the default scope and are counted per step (CH10).
- An upstream function with no stored caller and nothing else to follow
  ends with an `ends[]` line (CH11): "? not modelled: no stored caller and no resolved bridge; JSX renders and passed callbacks are followed only where the tag or value resolves to this function, other dependency injection and dynamic calls are not modelled" (review fix: `renders` and `passes_function` bridges are
  followed before it applies). When FastAPI routes' `security_dependencies` chains name the
  function, the line names the injection instead ("FastAPI dependency
  injection into n route(s) as declared in security_dependencies (...)");
  downstream, a route with declared chains gets "dependencies declared, not
  followed as calls: <steps> (<class>)".
- Caps (CH12): the requested depth (default 6, at most 10), 2,000 members
  per direction, fan-out 200 per member per step; each binding cap is
  listed on its step (`bounds[]`: `cap`, `step`, `beyond`, `text` such as
  "+ 21 beyond step 6"), and in the side's `caps`. The answer's `depth` is
  the walked depth; `caps.depth` is the largest depth a request may name
  (10).
- Every other edge of the walk's own sense is counted in `not_followed` by
  kind (CHD).

```json
{
  "api_version": 0, "repository": "app", "index": {...},
  "start": {"id": "route:api/items.py#GET%20%2Fitems", "kind": "route", "level": "node"},
  "direction": "both", "scope": "production", "depth": 6,
  "caps": {"depth": 10, "members": 2000, "fan_out": 200},
  "up": {
    "steps": [{"step": 1, "hidden_test": 0, "bounds": [],
               "map": {"shown": ["client_call:..."], "folded": 0},
               "members": [{"id": "client_call:...", "kind": "client_call", "name": "...", "qualified_name": "...",
                            "file": "ui/src/app.ts", "line": 7, "component": "ui", "method": "GET", "status": "linked",
                            "row": "CH3", "tier": "inferred 0.9", "tier_step": 1, "parents": ["route:..."]}]}],
    "edges": [{"from": "route:...", "to": "client_call:...", "kind": "calls_route", "row": "CH3",
               "wording": "called by client call", "dashed": false, "tier": "inferred 0.9", "count": 1, "evidence": [...]}],
    "ends": [{"member": "function:...", "row": "CH11", "text": "? not modelled: ..."}],
    "not_followed": {"contains": 2}, "caps": [], "members": 4, "hidden_test": 0
  },
  "down": {...},
  "map": {"steps": 3, "per_step": 5, "total": 40, "shown": 6, "folded": 1, "open_in_flow": true}
}
```

A table member has `database`, its card (`database:<set>`), when the table
stores a set; a route member has `full_path`, its served path (first
composed path, trailing slash restored), when it has one, else
`prefix_path`, its path whose prefix the code does not fix with `…` for
the prefix (T033 row UP-33), when it has one; `path` stays the pattern as
written. A CH3 (`calls_route`) edge has `assumptions` when its link rests
on any (absent otherwise), each `{id, text}`: `origin` (LF-1) or `UP-V1`,
`UP-V2`, read from the client call.

`steps[0]` holds the start. Each member names the row of the edge that
first reached it, its path `tier` (the weakest edge on that first path,
`tier_step` where it is; `fact` with step 0 when every edge is a fact) and
its `parents` on the previous step (every branch). `edges[]` lists every
followed edge between members in chain order (`from` nearer the start),
one per stored edge kind, with the wording of its row and direction (CH6
as ADR 0056 I8: `reads`, `writes`, `may read`, `may write`, `builds a
query on`; upstream `read by`, `written by`, ...). The lists are never cut
by the map: `map` per step is the map budget's folding hint (AG16: at most
3 steps each way, 5 members per step, 40 in total; members whose parent is
drawn first), the rest folds into the step's `folded` count (never a
single member: a step with one member left over draws it, past the third
step when its parent is drawn), and
`open_in_flow` says the map does not show every member.

### GET /api/v0/precision

The precision rule table as this binary holds it, whether the latest run
may use it, and what the store holds (T020, ADR 0066,
[docs/extract/precision.md](../extract/precision.md)). No parameters;
read-only, with the same runs-before/after consistency check, errors and
headers as the API section endpoints.

```json
{
  "api_version": 0, "repository": "app", "index": {...},
  "binary_extractor": "extractors-49",
  "run": {"built_in": true, "extractor_matches": true, "refusal": null},
  "rules": [{
    "id": "PL-F2", "kind": "family", "scope": "SQLModel / SQLAlchemy models",
    "label": "precise", "reason": null,
    "languages": ["python"],
    "facts": {"unit": "tables", "total": 19, "by_language": {"python": 19}},
    "marker": "model `table` / `column` nodes and their edges in groups with `tool` `sqlmodel`",
    "match": {"family": "schema_models", "tools": ["sqlmodel"], "dialects": []},
    "evidence": {"contract": [{"package": "internal/extract/models/sqlmodel", "name": "TestModelClaimContract"}],
                 "goldens": ["sqlmodel-models", "sqlmodel-link"],
                 "corpus": [{"adr": "0054", "quote": "..."}], "review": [{"adr": "0054", "quote": "..."}],
                 "oracle": [{"package": "internal/extract/models/sqlmodel", "name": "TestModelOracle"}], "oracle_none": null},
    "refusal": "none beyond the global ones"
  }],
  "default": {"row": "D", "label": "best_effort", "scope": "everything else", "covers": "every file label (...); routes, ...",
              "families": [{"family": "routes", "label": "best_effort", "languages": ["typescript"],
                            "facts": {"unit": "nodes", "total": 10, "by_language": {"typescript": 10}}}]},
  "files": {"reported_by": null, "total": 22,
            "languages": [{"language": "python", "total": 19, "by_label": {"best_effort": 19}, "reported_by_label": null}]}
}
```

- `run` is null when nothing is indexed. `built_in` is true for
  Graphvein's own indexer; `extractor_matches` compares the run header's
  extractor version with `binary_extractor` (null for an external run,
  whose version is the producer's own); `refusal` is `external_indexer`,
  `extractor_mismatch` or null. With nothing indexed (no run stored),
  `run` is null, so there is no `refusal` either: `no_run` appears only as
  the `reason` of every rule row here and of each transport's `precision`
  and `callers_precision` on `/api/v0/api/endpoints` (never
  `external_indexer`); the UI shows no chip then. With a refusal no label anywhere is
  `precise`.
- `rules` are the rows of the rule table in order. `label` is what the row
  yields for this index: `precise`, or `best_effort` with the run's
  refusal as `reason`. The evidence names are what `TestPrecisionRules`
  checks (contract and oracle test functions, golden fixture names, quotes
  from ADR review notes); a row with no cheap oracle has `oracle_none`.
- `facts` (null when nothing is indexed) counts the facts this index holds
  in the row's scope by the language of the file each is in (`none` when
  that file has no stored record): `tables` for schema rows (the tables of
  the groups whose marker the row names), `edges` for PL-F9 (the
  `reads_table` and `writes_table` edges into stored tables). A count of 0
  says this index has no fact of the scope: a precise row then guarantees
  nothing here. For a family row `languages` are the keys of
  `facts.by_language` (the rule table states no fixed list of languages a
  family reads); for a file row (none yet) the languages it labels.
- `default` is row D with its wording from `docs/extract/precision.md`, and
  `families` the families the API labels that fall to it, with their
  facts: `schema_other` (tables of groups no row names, such as a sqlite
  set or the `set: null` group), `routes`, `client_calls`, `ipc_commands`,
  `ipc_calls`, `server_actions` and `action_calls` (`nodes`, T024b), and
  `code_calls` (`edges`: every code `calls`
  edge, by the language of its source's file; ADR 0073), and
  `cross_repository_links` (`links`: under `serve --project`, the stored
  project links with an end in this member, by the language of that
  end's file; 0 for a single repository; T022 B11). Each family's
  `label` is decided like the views' scope labels; `code_calls` is
  `best_effort` on every run, whatever the file labels of its files (the
  same label as `code_calls_precision`), and `cross_repository_links`
  likewise (the same label as `links_precision`).
- `files` (null when nothing is indexed) counts the stored file records by
  language. `by_label` counts them by Graphvein's file label, decided as
  `/source` decides it (run refusals first, then the stored value checked
  against the rule table), never by the stored value alone. These are
  counts of what the store holds, not a share of "precise code". For an
  external run `reported_by` names the producer and `reported_by_label`
  counts the labels it stored, not checked by Graphvein and never
  Graphvein's; for a built-in run `reported_by_label` is null.

Every `reason` (and `precision_reason`) a label carries is one of
`default_branch` (no row names the scope), `marker_conflict` (the scope's
records name more than one tool or dialect, so no single row can be
named), `no_run` (no run is stored), `external_indexer`,
`extractor_mismatch`, `stored_label_invalid` and `structure_refused` and
`structure_unrooted` (file labels only) and `scope_unknown` (a change item
only). The list is
`labelReasons` in `internal/server/precision.go`;
`internal/server/testdata/precision-reasons.golden.json` holds it for the
web tests, which check that every reason has its own words.

Labels elsewhere: `/source` serves the file label, `/schema/sets` and
`/schema/table` each group's scope label, `/api/endpoints` (per transport), `/api/endpoint` and `/api/flow`
the endpoint and call families' labels, `/changes/items` each item's file
and scope labels; every one is `{label, row, reason}` (file labels:
`precision`, `precision_reason` and `precision_row`) decided from the
same table and the same run refusals.

### GET /api/v0/ide

The editor presets and the repository's default editor, for the editor
picker (ADR 0050), so the preset table lives only in Go
(`internal/config/ide.go`). It reads `graphvein.yaml` on every request and
never the index, so it has no consistency check; it has the same security
headers and Host and origin checks as every endpoint, and ignores query
parameters.

```json
{
  "api_version": 0,
  "presets": [
    {"editor": "vscode", "label": "Open in VS Code", "status": "verified",
     "note": "Verified from VS Code's documentation and its URL handler.", "short_note": ""},
    {"editor": "cursor", "label": "Open in Cursor", "status": "partly_verified",
     "note": "cursor://file is confirmed; line, column and the Windows path form are assumed from VS Code, so the column is marked as not exact.", "short_note": "column assumed"},
    {"editor": "jetbrains", "label": "Open in IntelliJ IDEA", "status": "limited",
     "note": "macOS only, IntelliJ IDEA only; ...", "short_note": "macOS only"},
    ...
  ],
  "repository": {"available": true, "error": null, "editor": "vscode",
                 "label": "Open in VS Code", "custom": false, "source": "builtin"}
}
```

- `presets` lists all six presets in table order; `status` is `verified`,
  `partly_verified` or `limited`, `note` one sentence (the table in
  [configuration](../config.md#ide)), `short_note` the note's caveat in a
  few words (`column assumed` for `cursor`, `macOS only` for `jetbrains`;
  empty for a verified preset). The editor rows of Settings and of the
  first-click chooser show `short_note` under the name and the full `note`
  from a status icon beside it.
- `repository` is the team default exactly as `graphvein.yaml` gives it:
  `editor`, `label` and `source` as `ide.editor`, `ide.label` and
  `ide.source` of a request without `editor`; `custom` is true for a custom
  `link_template`. When `graphvein.yaml` fails to load, `available` is false,
  `error` holds the message, `editor` and `label` are null and `source` is
  `repository`.

### GET /api/v0/ui

The web UI defaults the repository sets in `graphvein.yaml` (T025b, ADR
0078; depths T042), so the UI can apply `ui.labels`, `ui.impact_depth`
and `ui.chain_depth` when the browser has no choice of its own. It
reads `graphvein.yaml` on every request and never the index, so it has no
consistency check; it has the same security headers and Host and origin
checks as every endpoint, and ignores query parameters.

```json
{"api_version": 0, "labels": "technical", "impact_depth": 4, "chain_depth": null, "error": null, "report_url": null}
```

- `labels` is `ui.labels` (`plain` or `technical`), or null when
  `graphvein.yaml` sets none or fails to load.
- `impact_depth` and `chain_depth` are `ui.impact_depth` and
  `ui.chain_depth` (1 to 10), or null when `graphvein.yaml` sets none or
  fails to load ([Depth defaults](#depth-defaults)).
- `error` is the load error of `graphvein.yaml`, else null.
- `report_url` is the public wrong-fact issue form the binary was built
  with (`REPORT_URL=https://... scripts/build.sh`, the public-repository
  switch of ADR 0089), or null. While it is null the UI renders no
  "Report a wrong fact" flag; no repository setting can set or change it.
  Only an absolute https URL without user information, query or fragment
  is served; any other build value is logged at startup and served as
  null.

### GET /api/v0/search?q=&kinds=&limit=

The jump-to palette's search (T040, ADR 0089, claim contract
[jump-search](../contract/claims/jump-search.md), rows JS-1..JS-20). It
matches stored names and paths only, never a file's text, and never
states that something does not exist.

- `q` (required): one string, trimmed, 1..200 bytes, matched as a
  substring with ASCII case folding (`A`-`Z` only).
- `kinds`: a comma list of `route`, `operation`, `table`, `symbol`,
  `file`; all five when absent.
- `limit`: items per kind, 1..50, default 10.
- `editor`: the editor preset for the links, as on the other node
  endpoints.

What each kind matches: routes by every full path, the pattern, the
unknown-prefix path the item is labelled with when it has no full path
(without the leading `…`, so any part of the shown label finds it) and the
name ("GET /x"); operations (the generated OpenAPI document, as
`GET /api/v0/openapi` builds it) by path key; tables (migration and model)
and symbols (`class`, `function`, `method`, `struct`, `interface`, `type`,
`type_alias`, `enum`, `trait`, `union`, `record`, `variable`, `constant`,
`constructor`, `macro`, `mixin`, `extension`, `extension_type`,
`annotation`) by name and qualified name; files by base name and path.
Calls, columns, diagnostics and the files Graphvein could not read are not
searched.

Ranking: an item's tier is the best of its keys: `prefix` (the key starts
with the query), `word` (the query starts at a word start: after a
character that is not an ASCII letter or digit, at a lower-to-upper case
change or between letters and digits) or `substring`; then the shorter
key, the key's bytes and the identity. Kind groups are ordered by their
best tier, then route, operation, table, symbol, file.

```json
{"api_version": 0, "repository": "app", "query": "items",
 "kinds": ["route", "operation", "table", "symbol", "file"], "limit": 10,
 "ide": {...}, "index": {...},
 "searched": {"indexed": true, "files": 26, "files_not_read": 0, "scanner_gaps": 0},
 "absence": "not_proven",
 "totals": {"route": 3, "operation": 3, "table": 0, "symbol": 2, "file": 0},
 "groups": [
  {"kind": "route", "available": true, "unavailable": [], "total": 3,
   "best_tier": "word", "truncated": false,
   "items": [{"kind": "route", "repository": "app",
              "id": "route:server/app.ts#GET%20%2Fitems", "label": "/items",
              "tier": "word", "match_key": "path", "file": "server/app.ts",
              "line": 13, "ide": {...}, "method": "GET", "path": "/items",
              "test": false}]}]}
```

- `searched` names what was searched: whether the repository is indexed,
  its stored files, and the files not read and scanner gaps of the
  couldn't-read page, which are not searched. `absence` is always
  `not_proven`: no result is never proof that something does not exist.
- A group with `available: false` could not be searched in the
  repositories `unavailable` names (operations when `graphvein.yaml` does
  not load); the other kinds are answered.
- Items: `label` is the text to show (a route's shown path, or its
  unknown-prefix path, pattern or name; an operation's path key; a name;
  a file path), `id` the node identity (the route for an operation, the
  `file` node for a file, null without one), `ide` the server-built
  editor link (null for an operation and without a checkout). Kind fields:
  routes `method`, `path`, `prefix_unknown`, `test`; operations `method`,
  `path`; tables `qualified_name`, `source`; symbols `node_kind`,
  `qualified_name`; files `language`.
- Under `serve --project`, `repository=` answers one member as above.
  Without it every member is searched: `first=<member>` puts that member
  first (then manifest order), every item names its `repository`, a kind
  group lists its items by tier first and, within a tier, the first
  member's items before the others' (a better match of another member
  comes before a weaker one of the first member), and the body
  is `{"project", "query", "kinds", "limit", "absence", "members":
  [{"repository", "checkout", "ide", "index", "searched", "totals"}],
  "totals", "groups"}` with project totals that are the members' sums.
  `first` with `repository`, or outside project mode, is 400.

### GET /api/v0/unread?scope=&limit=

The "What Graphvein couldn't read" page (T040, ADR 0089, claim contract
[couldnt-read](../contract/claims/couldnt-read.md), rows CR-1..CR-43).
Every part comes from the stored index, computed by the code of the
endpoint that already serves it where one does; what Graphvein does not
model at all, for any repository, is listed in
[known gaps](../known-gaps.md). `scope` is `production`
(default), `test` or `all`; `limit` bounds every list (1..2000, default
200; counts are never capped); `editor` as above.

```json
{"api_version": 0, "repository": "app", "scope": "production", "limit": 200,
 "ide": {...}, "index": {...}, "builtin_run": true,
 "config": "repository", "config_error": null,
 "counts": {"unplaced_routes": 1, "mounts": 0, "dynamic_patterns": 0,
            "router_form_files": 0, "router_forms": 0,
            "router_form_files_not_counted": 0, "dynamic_calls": 3, "wrapper_calls": 1,
            "routes_without_shape": 0, "routes_shape_invalid": 0,
            "routes_with_sections_not_read": 10, "routes_depth_cap": 0,
            "files_not_read": 0, "files_syntax_errors": 0, "scanner_gaps": 0,
            "files_without_facts": 3, "unresolved_calls_external": 30,
            "unresolved_calls_unknown": 8, "files_counted": 17,
            "files_not_counted": 0, "route_failures": 0, "mounts_unresolved": 0,
            "routes_withheld": 0, "files_records_dropped": 0, "files_structure_outside_contract": 0},
 "route_paths": {"document": {"available": true, "reason": null},
                 "unplaced": {"total": 1, "truncated": false, "items": [...]},
                 "mounts": {...}, "dynamic": {...}, "router_forms": {...},
                 "failed": {...}, "mounts_unresolved": {...}, "routes_withheld": {...},
                 "forms": 0, "form_files_not_counted": 0},
 "client_calls": {"dynamic": {...}, "wrapper": {...}},
 "shapes": {"routes": 10, "without_shape": {...}, "shape_invalid": {...},
            "sections": {"path": 1, "query": 10, ...}, "sections_not_read": {...}},
 "depth_caps": {"routes": {...}},
 "files": {"scope_applied": true, "not_read": {...}, "syntax_errors": {...},
           "scanner_gaps": {...}, "records_dropped": {...},
           "structure_outside_contract": {...},
           "without_facts": {"available": true, "reason": null,
                             "by_extension": [{"extension": ".vue", "count": 236}],
                             "by_category": [{"category": "code_with_reader", "count": 0},
                                             {"category": "code_no_reader", "count": 236}, ...],
                             "files": {...}}},
 "code_calls": {"available": true, "reason": null,
                "languages": ["go", "javascript", "python", "rust", "tsx", "typescript"],
                "external": 30, "unknown": 8, "files_counted": 17,
                "files_not_counted": 0, "files": {...}}}
```

- `counts` names every count of the page, null where its part is not
  available (not indexed, an external indexer's run, `graphvein.yaml` not
  loading). No count adds different units; a UI total of the page is not
  provided.
- `route_paths`: the explorer's lists from the generated document
  (production routes, whatever the scope): `unplaced` routes without a
  usable path (with the reason and the path value keys that would place
  them, and `prefix_unknown`, the route's unknown-prefix path as the
  endpoint list writes it, `"…/api/x"`, or null when it has none) and
  `mounts` (unresolved mount hops with the routes below); both
  are `document.available: false` with the load error when
  `graphvein.yaml` does not load. `dynamic`: stored
  `route_dynamic_pattern` and `route_dynamic_methods` diagnostics with the
  expression and line read from their documented message form
  (`<expression> at L:C-L:C`; another form keeps the message and has
  `line: null`). `router_forms`: one `go_routes_unread` diagnostic per
  file, message verbatim, with `forms` and `by_form` from the file node's
  structured attribute `go_route_forms_outside_contract` (null when the
  node holds none, for example an index written before it existed);
  `route_paths.forms` sums the items with counts and
  `form_files_not_counted` counts the others. The message is never
  parsed. `failed`: stored `go_routes_failed` diagnostics, message
  verbatim, `line: null` (`file` empty for a run-wide failure): Go route
  recognition or linking failed there, so those routes are not known.
  `mounts_unresolved`: stored `route_mount_unresolved` diagnostics, a
  mount whose router is not resolved, through which no path is claimed,
  with the expression and line from the same message form as `dynamic`.
  `routes_withheld`: the stored `route_mount_unresolved` diagnostics of a
  Go file whose expression is a route registration (`METHOD "pattern"
  (reason)`, the form the Go linker writes): the route is found, but its
  path is withheld because another pattern of the ServeMux, its redirect,
  a forwarding closure or a route of the router it is mounted on may
  answer it. They are not in `mounts_unresolved`, and the route is
  labelled by its pattern with "(path withheld)" (ADR 0089 Revision 5).
  A route with no path of any kind is otherwise labelled "(shadowed)"
  when its `shadowed_paths` are stored, else "(path not known)", in
  these lists and in search results.
- `client_calls`: HTTP client calls in scope whose stored status is
  `dynamic` or `wrapper` (the `/api/v0/api/calls` rule), with their owner.
- `shapes`: HTTP routes in scope with no stored shape (every section not
  read), with a stored shape that does not decode (`shape_invalid`), and
  per section the routes whose shape has it `not_read`; `depth_caps`:
  routes whose shape stopped a chain at the depth cap, with `depth_cap`
  and `depth_limit`.
- `files`: `not_read` (`file_too_large`, `parse_failed`, `extract_failed`
  on stored files; `file_read` on a path the scanner could not read,
  `file_binary` on a file holding a NUL byte, `file_not_utf8` on a file
  that is not UTF-8 and `file_not_regular` on a FIFO, socket or device,
  all `stored: false`), `syntax_errors` (`syntax_error`: the readers claim
  nothing beyond the file there), `scanner_gaps` (`scan_read`, `symlink`,
  `invalid_path`, `ignore_read`: paths not indexed, never counted as
  files), `records_dropped` (`extract_invalid_record`, message verbatim:
  a record extracted from the file did not validate and was dropped;
  when it was a fact record, no fact from that file is stored; `file`
  empty for a run-level record), `structure_outside_contract`
  (`ts_structure_unread`, one per file, message verbatim: TypeScript or
  JavaScript declarations or imports outside the structural claim
  contract, which claim nothing) and `without_facts`: in a built-in run, files whose language has
  no built-in reader and from which no fact is stored (no node other than
  the file node, no edge evidence), grouped by extension. They are "no
  fact stored from this file", never "unread": Dockerfiles, `go.mod` and
  compose files hold facts and are not listed. Paths left out by rule
  (`.gitignore`, `.graphveinignore`, the scanner's default names, nested
  repositories) have no record and are never listed. Items of `not_read`
  and `without_facts` have `category` (`code_with_reader`,
  `code_no_reader`, `not_code`, `other`, by extension from the claim
  file's table) and both lists carry `by_category`, every category
  counted. Every file item has `test`
  from the `tests` rules; with `graphvein.yaml` not loading the rules are
  unknown, `test` is null, `scope_applied` is false and every file is
  kept.
- `code_calls`: in a built-in run, the sums of the stored `file` node
  attributes `unresolved_calls_external` and `unresolved_calls_unknown`
  over the files counted: Go, Python, TypeScript, TSX, JavaScript and
  Rust files without a not-read or syntax-error diagnostic whose file node
  holds both counts as integers. Every other file of those languages is
  in `files_not_counted`. The diagnostic message text is never read.
- Under `serve --project`, `repository=` answers one member's page.
  Without it the body is `{"project", "scope", "limit", "totals",
  "not_available", "members": [...]}`: every member's page and, per
  count, the members' sum; a count not available for a member is null at
  project scope and `not_available` names the members. A member without
  a checkout is answered from the store, with no editor links and the
  built-in test rules (`config: "defaults"`).

### GET /api/v0/openapi

The OpenAPI 3.1 document of the repository's stored routes (T029, ADR
0081): the same bytes as [`graphvein openapi`](openapi.md), built under
the [claim contract](../contract/claims/openapi-generation.md) from the
stored routes, their auth and shape attributes, the imported specs and
`explorer.path_values` in `graphvein.yaml`; `servers` are the configured
try-it environments (project ones for the member first). The Explorer section
(`#/explorer`) reads it and its download buttons save it.

- `format=json` (default) answers `Content-Type: application/json;
  charset=utf-8`; `format=yaml` answers `application/yaml;
  charset=utf-8`; anything else is 400 `bad_request`.
- The read is repeated once when an index run commits in between, then
  503 `index_changed`, as for every API read. A repository that is not
  indexed gets a document with no paths and `x-graphvein.index: null`.
- `graphvein.yaml` that does not load answers 409 `invalid_config`: the
  document would otherwise silently drop the values set by you.
- Project mode: `repository=` names the member (its routes and its own
  `graphvein.yaml`; one document per member). Without it, a project with
  one member answers that member's document, and a project with several
  lists them: `{"api_version": 0, "project": "users", "members":
  [{"repository": "backend", "document": "/api/v0/openapi?repository=backend",
  "checkout": true}]}`.

### GET and POST /api/v0/explorer/path-values

The explorer's user-supplied path parts (`explorer.path_values` in
[`graphvein.yaml`](../config.md#explorer); rows OG-90..OG-96): a value for
a route Graphvein stored without a usable path, for a Next.js
`basePath` read from the environment, or for an unresolved mount hop (one
prefix for every route below it). A value changes only the explorer,
try-it and the generated document, and is always shown as set by you.
In project mode `repository=` names the member, whose own
`graphvein.yaml` is edited.

GET:

```json
{"api_version": 0, "file": "graphvein.yaml", "exists": true, "hash": "sha256:…",
 "editable": true, "reason": null, "token": "<session token>",
 "values": [{"key": "route:src/app.js#POST%20%2Forphan", "value": "/api/orphans/{id}",
             "status": "used", "operations": 1}],
 "unresolved": [{"route": "route:web/app/api/x/route.ts#GET%20%2Fapi%2Fx", "method": "GET",
                 "pattern": "/api/x", "file": "web/app/api/x/route.ts", "line": 1,
                 "reason": "unresolved_base", "templates_after_base": ["/api/x"],
                 "unknown": {"part": "base_path", "project": "web", "text": "…"},
                 "keys": ["route:web/app/api/x/route.ts#GET%20%2Fapi%2Fx", "nextjs_base_path:web"]}],
 "mounts": [{"key": "mount:internal/web/routes.go#createRouter%20r.Route%28base%29",
             "hop": "r.Route(base,func(...){...})", "file": "internal/web/routes.go", "line": 284,
             "reason": "route_prefix_unresolved", "registration": "sha256:…",
             "shadowing_not_checked": true, "routes": 85,
             "examples": [{"method": "GET", "above": "", "path": "/"},
                          {"method": "GET", "above": "", "path": "/api/cloud/config"}],
             "ambiguous": false, "value_covers": "root", "above": [], "framework": "chi"}]}
```

`hash` names the file on disk now (the hash of empty text when there is
no file: an edit then creates it with `version: 1` and the values);
`values` are the effective values with their use (`used` or `unused` with
`why`, such as a conflict with a code-read operation or "registration
changed", "ambiguous registration"), the recorded `registration` and a
`warning` for a route key without one (the UI shows it), `unresolved` the routes still without a path, each with the keys
that would place it (most specific first). A project member without a
checkout here is `editable: false` with the reason. `mounts` are the
unresolved mount hops of the production routes (rows OG-94, OG-95): the
hop's key and hash, its text, file and line, the number of routes below
it without a path read from code, up to three of them (method, the
known prefix `above` the hop and the path below it), whether the Go
checks above the hop were not run (`shadowing_not_checked`, OG-96),
whether a twin hop with the same text makes it `ambiguous`, what a value
states (`value_covers`: `hop`, only the part the hop adds after the
known prefixes in `above`; `nothing`, only `/` is accepted; `root`, the
whole prefix from the root) and the routes' `framework`. A `set` of a
mount key records the hop's registration and refuses a value other than
`/` for a `nothing` hop. The token is random
per server process, as the manifest edit's.

POST, with `Content-Type: application/json` and `X-Graphvein-Token:
<token>`, body at most 64 KiB:

```json
{"base_hash": "sha256:…",
 "operations": [{"op": "set", "key": "route:src/app.js#POST%20%2Forphan", "value": "/api/orphans/{id}"},
                {"op": "remove", "key": "nextjs_base_path:web"}]}
```

1 to 64 operations, each `{op: set, key, value}` or `{op: remove, key}`.
A `set` of a route key also writes the route's stored content hash as
`registration` (row OG-90); a route key no stored route has, or whose
registration has an identical twin in its file, is 400 `invalid_value`.
A `set` of a mount key writes the hop's hash as `registration`; a key no
stored route has below it, or an `ambiguous` hop, is 400 `invalid_value`.
Without `apply` the answer is a preview and nothing is written:

```json
{"api_version": 0, "applied": false, "base_hash": "sha256:…", "result_hash": "sha256:…",
 "diff": "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n@@ …", "valid": true, "problems": [],
 "changes": ["sets the path value of route:src/app.js#POST%20%2Forphan to \"/api/orphans/{id}\" (shown as set by you)"]}
```

Adding `"apply": true, "result_hash": "<the preview's result_hash>"`
writes the result atomically (temporary file and rename) through the
same writer the configuration tests use (comments kept, two-space
indentation); the answer has `applied: true`. Nothing is re-indexed: the
next document read uses the new values.

Answers: 200; 400 `bad_request` (body, unknown or duplicate member,
operation shape), 400 `invalid_value` (a key or value breaks row OG-91),
400 `edit_refused` (removing a value that is not set), 400 `no_change`;
403 `token_required`, `not_loopback`, `cross_site_request` or
`host_not_allowed`; 409 `config_changed` (the file's hash is not
`base_hash`), 409 `preview_changed`, 409 `no_checkout` (project member
without a checkout); 413; 415; 422 `invalid_config` (apply of a result
that does not load). A refused edit leaves the file untouched; edits are
serialised.

### GET and POST /api/v0/config

The Settings page's keys and its guarded edit of `graphvein.yaml` (T039,
[ADR 0088](../adr/0088-settings-editing.md), rows ST-1..ST-30 of the
[settings claim contract](../contract/claims/settings.md)). In project
mode `repository=` names the member, whose own `graphvein.yaml` is read
and edited.

GET:

```json
{"api_version": 0, "repository": "shop", "file": "graphvein.yaml",
 "exists": true, "hash": "sha256:…", "text": "version: 1\nui:\n  labels: technical\n",
 "editable": true, "reason": null, "token": "<session token>", "config_error": null,
 "keys": [{"key": "ui.labels", "kind": "enum", "choices": ["plain", "technical"],
           "value": "technical", "default": "plain", "origin": "file", "line": 3,
           "applies": "at_once", "editable": true, "reason": null},
          {"key": "index.shape_depth", "kind": "integer", "min": 1, "max": 10,
           "value": 6, "default": 6, "origin": "builtin", "line": null,
           "applies": "next_index_run", "editable": true, "reason": null},
          {"key": "tests.include", "kind": "list", "value": null, "default": null,
           "origin": "not_set", "line": null, "applies": "next_index_run",
           "editable": true, "reason": null}],
 "other_keys": [{"key": "version", "line": 1, "edited_by": null},
                {"key": "explorer.path_values", "line": 5, "edited_by": "/api/v0/explorer/path-values"}],
 "reindex": {"path": "/api/v0/index/run", "scope": "repository"},
 "index_settings": {"state": "unchanged", "changed_keys": [], "unknown_keys": [],
                    "index_hash": "sha256:…", "current_hash": "sha256:…", "reason": null},
 "ignore": {"file": ".graphveinignore", "exists": false, "text": "", "editable": false},
 "project": null}
```

`keys` lists every key the form edits, in form order: `ui.labels`,
`ide.editor`, `index.shape_depth`, `clients.base_paths`,
`clients.link_generated_output`, `tests.include`, `tests.exclude`,
`openapi.include`, `openapi.exclude`, `openapi.base_paths`,
`architecture.hide` ([configuration](../config.md)). `kind` is `enum`
(`choices`), `integer` (`min`, `max`), `boolean`, `list` (strings) or
`map` (an ordered list of `{"key", "value"}` strings). `origin` is `file`
(the file sets it; `line` is its key's line), `builtin` (the built-in
`default` applies), `not_set` (nothing applies, `value` null) or `unknown`
(the file does not load, or the member has no checkout: `value` null).
`applies` is `at_once` (read from the file on every request of the
endpoints that use it) or `next_index_run` (read by the index run, which
re-links without re-parsing; `reindex` names the run to start:
`POST /api/v0/index/run`, scope `repository`, or `project` under
`--project`, which re-indexes every member and then links). `value` is
the value as the strict decoder reads it (`shape_depth: 0x7` is 7, `010`
is 8). `ide.editor` is `editable: false` with a `reason` while
`ide.link_template` is set, and `locked_by` names that key with its line
(`{"key": "ide.link_template", "line": 4}`; null for every other key):
the template builds the links, not the editor value or its default.
`index_settings` is `freshness.settings` of `/api/v0/status` for the
text served here: whether the last index run was built with these
settings (`changed_keys` names the keys a re-index would apply; `unknown`
when the file does not load or the run recorded nothing).

`other_keys` are the keys the file sets outside the form (every scalar
root key and every key inside a section), in file order; a typed edit
never changes them. `edited_by` names the guarded edit of
`explorer.path_values` and `tryit.environments`; the others are edited in
the YAML view (the `text` form below). `hash` is the base for an edit
(`absent` when there is no file; the first edit creates it with
`version: 1`). When the file does not load, `config_error` is the strict
decoder's message with the file and line, every key is `origin: unknown`
and not editable, and `other_keys` is empty; `text` and `hash` are still
served so the YAML view can fix the file. `ignore` is the root
`.graphveinignore`, the only one the scanner reads, shown read-only;
Settings never writes it. In project mode `project` is the manifest's
part, read fresh on every request:

```json
"project": {"file": "graphvein-project.yaml", "error": null,
            "environments": [{"name": "shared", "label": "staging", "line": 10, "for_member": true,
                              "base_urls": [{"repository": "backend", "base_url": "http://127.0.0.1:8000"}]}],
            "edited_by": "/api/v0/tryit/environments"}
```

edited through the try-it environment edit with target `project`. A
member without a checkout here is `editable: false` with the reason;
`exists`, `hash`, `text` and `ignore.exists`/`ignore.text` are null and
every key is `origin: unknown`.

POST, with `Content-Type: application/json` and `X-Graphvein-Token:
<token>`, body at most 1 MiB, either typed operations:

```json
{"base_hash": "sha256:…",
 "operations": [{"op": "set", "key": "ui.labels", "value": "plain"},
                {"op": "set", "key": "clients.base_paths", "value": [{"key": "web", "value": "/api"}]},
                {"op": "remove", "key": "index.shape_depth"}]}
```

or the whole file (at most 256 KiB):

```json
{"base_hash": "sha256:…", "text": "version: 1\nui:\n  labels: plain\n"}
```

1 to 64 operations, each `{op: set, key, value}` with a value of the key's
JSON type, or `{op: remove, key}`. Which values are valid (a choice, a
range, a pattern) is decided by the strict decoder of `graphvein.yaml`
alone. A typed edit changes only the lines of the edited entry: a value on
its key's line is replaced in place and keeps its line comment, a new key
goes after the last entry of its section (a new section at the end), new
lines take the file's line ending and indentation step, a removed key
takes its lines and the comment lines directly above it, and a section it
empties goes too. A block list or mapping is edited entry by entry: every
line of an entry the edit keeps stays (its comments too), a mapping entry
whose value alone changes is rewritten in place with its line comment, an
added entry is written next to its neighbours, and a removed entry takes
its lines and the comment lines directly above it. The text edit is kept
only when it decodes to exactly the same tree as the edit made on the
parsed file; otherwise (a reordered mapping, comments inside the lines a
changed entry would replace, a flow-style section) the parsed file is
encoded again with two-space indentation and without blank lines, reusing
the nodes of the entries it keeps: `reformatted: true` and a line in
`changes` say so. A re-encode also says in `changes` when it drops a
byte order mark, turns CRLF line endings into LF or drops an anchor.
`lost_comments` lists every comment of the file on disk
that the result no longer has, with its line there (the comments yaml.v3's
parser reads in both texts compared, or a line scanner's when a text does
not parse; a removed entry's own comments, and any a re-encode drops),
each also named in `changes`. Without `apply` the answer is a
preview and nothing is written:

```json
{"api_version": 0, "applied": false, "base_hash": "sha256:…", "result_hash": "sha256:…",
 "diff": "--- a/graphvein.yaml\n+++ b/graphvein.yaml\n@@ …", "valid": true, "problems": [],
 "changes": ["sets ui.labels to `plain` (was `technical`)"], "reformatted": false, "lost_comments": [],
 "changed_keys": [{"key": "ui.labels", "applies": "at_once"}],
 "other_keys_changed": false, "reindex_needed": false}
```

`changes` word each operation: a scalar with the value it replaces, a list
or mapping item by item as `key: value` (`sets openapi.base_paths: adds
`v2: /v2`; changes `contracts: /api/v1` (was `/api`); removes `legacy: /old``),
never as JSON. `changed_keys` are the form keys whose effective value changes;
`other_keys_changed` is true when anything outside the form changes (a
text edit). A whole-text edit of a file that did not load lists every form
key in `changed_keys` and says in `changes` "the file did not load
before; every key is treated as changed"; `reindex_needed` is true when a changed key applies at the
next index run. An invalid result is `valid: false` with the decoder's
message in `problems`. Adding `"apply": true, "result_hash": "<the
preview's result_hash>"` writes exactly the previewed text atomically
(temporary file and rename); the answer has `applied: true`. Nothing is
re-indexed.

Answers: 200; 400 `bad_request` (body, unknown or duplicate member, both
or neither of `operations` and `text`, operation shape), 400 `unknown_key`
(a key the form does not edit), 400 `invalid_value` (a value of the wrong
JSON type), 400 `edit_refused` (removing a key the file does not set,
`ide.editor` under a `link_template`), 400 `no_change`; 403
`token_required`, `not_loopback`, `cross_site_request` or
`host_not_allowed`; 409 `config_changed` (the file's hash is not
`base_hash`), 409 `preview_changed`, 409 `invalid_config` (a typed edit of
a file that does not load), 409 `no_checkout`; 413 `too_large`; 415; 422
`invalid_config` (apply of a result that does not load). A refused edit
leaves the file untouched. Every edit of a `graphvein.yaml` (this one, the
path values and the try-it environments of a repository) runs under one
lock, so two never interleave.

### GET /api/v0/source?id=&context=

The current lines around one stored node's span, for the code peek panel
(ADR 0044). The span is highlighted only when Graphvein can show that these
lines are the indexed ones; otherwise the lines are shown without a
highlight, or not at all, with an explicit state. It uses the same
runs-before/after consistency check, errors and headers as the API section
endpoints.

- `id` (required): the canonical identity of any stored node, URL-encoded:
  400 when missing, given twice or not canonical; 404 `not_found` ("no node
  with identity ...") when not stored.
- `context` (optional): lines before and after the span, decimal digits
  only, 0 to 50, default 8; anything else is 400.

The request never carries a path. The only file read is the node's stored
`file`, and only when that is a valid contract path and a stored file.

For `id=route:backend.ts#GET%20%2Ft015b-requests` and `context=1` on the
`testdata/fixtures/env-wrappers` fixture:

```json
{
  "api_version": 0, "repository": "...", "index": {...},
  "ide": {"available": true, "error": null, "label": "Open in VS Code", "editor": "vscode", "source": "builtin"},
  "node": <presented node>,
  "source": {
    "state": "exact", "reason": null,
    "file": {"path": "backend.ts", "size": 288, "line_count": 8,
             "language": "typescript", "precision": "best_effort",
             "precision_reason": "default_branch", "precision_row": "D",
             "reported_precision": null},
    "first_line": 3,
    "lines": ["function handle(req, res) { res.json({}); }", "app.get('/t015b-requests', handle);", "app.post('/t015b-requests', handle);"],
    "cut_lines": [], "truncated": false,
    "highlight": {"start_line": 4, "start_column": 0, "end_line": 4, "end_column": 34}
  },
  "code_calls_precision": null
}
```

- `code_calls_precision` is, for a `function` or `method` node, the label
  of code `calls` edges (family `code_calls`; T020 O-S1-5 (a)): the views
  show a function's calls next to this file label, which never covers
  them. It is null for every other node.

- `node` is a presented node as above, built from the same read as
  `source`: `exact` comes with `file_state` `unchanged` and `ide.exact`
  true, or an `ide.caveat` (the column cannot be vouched for that editor:
  a line-only link after a tab, a column unit only assumed); `span_invalid` with `unchanged`; `text_match`, `changed` and
  `binary` with `changed`; every state that read nothing with `missing`.
- `source.lines` are the lines of the LF-normalised current file from
  `first_line`, without line terminators. Line 1 drops a leading byte order
  mark. A line longer than 4,096 bytes is cut at a character boundary and
  its number is listed in `cut_lines`.
- The window runs from `start_line - context` to `end_line + context`,
  clamped to the file, at most 400 lines: a longer span shows its first
  lines with `truncated: true`. Without a trusted span (`changed`,
  `span_invalid`) it is centred on the stored start line, clamped to the
  file.
- `highlight` is null unless the state is `exact` or `text_match`. Lines are
  1-based; columns are 0-based UTF-16 code units within the returned line
  texts (what a browser slices), end exclusive, clipped to a cut line. An
  end after the returned lines (a truncated window, or a span that ends
  after the file's final line feed) moves to the end of the last returned
  line.
- `file.size` is the current raw size in bytes (null when not measured);
  `file.line_count` the current line count (null when not read).
- `file.language` and `file.precision` are the file label (T020, ADR 0066;
  see [GET /api/v0/precision](#get-apiv0precision)): the stored file
  record's language, and Graphvein's label for it. `precision` is the
  stored value only when the latest run is Graphvein's own at this
  binary's extractor version and the value is the one the rule table gives
  that language; otherwise it is `best_effort` and `precision_reason` says
  why: `default_branch` (no row names the language), `external_indexer`,
  `extractor_mismatch`, `structure_refused` (the language has a file row,
  PL-L1 for Go, PL-L2 for Python, PL-L3 for TypeScript, TSX and
  JavaScript, PL-L4 for Rust, but the indexer stored `best_effort` for this
  file because its structure claims nothing: for Go a file go/parser
  rejects or a file with a lone CR, ADR 0068; for Python a file CPython
  3.14 rejects, ADR 0069; for TypeScript, TSX and JavaScript a file
  tree-sitter rejects, ADR 0070; for Rust a file in which tree-sitter
  reports a syntax error or a file with a lone CR, ADR 0071; or one it
  could not parse or extract), `structure_unrooted` (a Rust file no crate
  root of a read `Cargo.toml` reaches: its declarations are claimed, but
  its imports are not resolved as a crate member and no oracle
  configuration compiles it; the indexer stores `best_effort` and an info
  diagnostic `structure_unrooted`, which the server reads for this reason;
  ADR 0071), or `stored_label_invalid` (the store holds a value this
  binary's table does not give). `precision_row` is the rule row behind
  the label on an eligible run: the language's file row (`PL-L1`, `PL-L2`,
  `PL-L3`, `PL-L4`) when the file is precise
  or its structure was refused, `D` for the default branch, null
  otherwise. For an external run the producer's own value is
  `reported_precision` ("reported by" the run's indexer, never Graphvein's
  label); it is null otherwise. All five are null when the file has no
  stored record (`no_location`).

| state | when | lines | highlight |
|---|---|---|---|
| `exact` | the file's current hash equals its stored hash and the span is in bounds on character boundaries | yes | yes |
| `text_match` | the hash differs; the span is in bounds and non-empty, its bytes hash to the node's `content_hash`, and that text occurs exactly once in the file (overlapping occurrences counted) | yes | yes; the file changed since the index |
| `changed` | the hash differs and `text_match` does not hold; `reason` is `span_out_of_range`, `empty_span`, `span_text_differs` or `span_not_unique` | yes, around the stored line | no |
| `span_invalid` | the hash is equal but the span is out of bounds or splits a UTF-8 sequence (an indexer error; logged once as a warning) | yes, around the stored line | no |
| `missing` | the file or a directory on its path does not exist, or a path component is a file | no | no |
| `not_regular` | a path component under the root is a symlink (even one that stays inside the root), a component is not a directory, the file is not a regular file, or the path would leave the root | no | no |
| `too_large` | the file is larger than 4 MiB (checked before and while reading) | no | no |
| `binary` | the file contains a NUL byte or is not valid UTF-8 (the scanner would not index it) | no | no |
| `unreadable` | any other open, stat or read error; `reason` holds the message | no | no |
| `no_location` | a synthetic node (`file` `.`) or a node without span | no | no |
| `not_indexed` | the node's file is not a stored file or not a valid contract path (a store inconsistency; nothing is read) | no | no |

Checks run in that order of precedence: `no_location`, `not_indexed`, the
confined read (`missing`, `not_regular`, `unreadable`, `too_large`),
`binary`, then the span. `reason` is null except for `changed` and
`unreadable`. Clients treat a state they do not know as "no highlight, show
the state".

`text_match` claims only that the highlighted bytes are exactly the indexed
span text and occur once in the file now; the node may still mean something
else (a prefix edited in another file), so clients show that the file
changed. A moved span is not relocated.

Confinement: the repository root is opened with `os.Root`, every path
component of the stored file is checked with `Lstat` (no symlink is
followed, as the scanner follows none; the root itself may be a symlink),
the opened file must be the checked regular file, and at most 4 MiB + 1
bytes are read. The server reads the store's file hashes on every request.


### Project endpoints (serve --project)

Only under `serve --project` (T022 B8, ADR 0075 "Server and UI"); a
single-repository server answers 404 `not_found` on these paths. Every
node is named by the pair (`repository`, `identity`), never one
concatenated string. Links and their freshness come from the last stored
link pass through `projectlink.View`, shared with `graphvein status
--project` and the project-scoped CLI reads, so "stale" means the same
everywhere: a link is stale when the manifest's declarations changed, or
when a member at either end failed to index before the pass or was
indexed again since. Stale links are shown and followed, never dropped.

#### GET /api/v0/project

```json
{
  "api_version": 0, "project": "users", "server_version": "...",
  "members": [{
    "repository": "frontend", "repository_source": "manifest",
    "path": "frontend", "path_source": "manifest", "checkout": true,
    "remote": {"url": "https://github.com/o/frontend.git", "repository_root": true},
    "extractor": {...}, "index": {...}, "freshness": {...},
    "links": {"in_pass": true, "pass_run": "...", "index_error": null,
              "links": 3, "stale": 0, "stale_reasons": []}
  }],
  "link_pass": {"state": "fresh", "pass": 1, "links": 3, "stale_links": 0,
                "outcomes": {"linked": 3}, "stale_reasons": []},
  "checks": []
}
```

- `repository_source` is where the ID was confirmed: `manifest`, or
  `member graphvein.yaml` (ADR 0075 identity rule 4). `path` is relative
  to the manifest's directory (slash separated), null without one;
  `path_source` is `manifest`, `local file` or null.
- `remote` is the checkout's git `origin` URL, read with local git
  commands only (`git rev-parse --show-toplevel`, `git config --get
  remote.origin.url`; nothing is fetched). A user name and password in a
  URL form are removed (`https://user:token@host/...` is shown as
  `https://host/...`). `url` is null without an origin remote;
  `repository_root` is false when the checkout is a folder inside a git
  repository (a monorepo member, whose remote is the whole repository's).
  `remote` is null without a checkout or outside git. It is metadata
  only, never the ID.
- `extractor`, `index` and `freshness` are those of `/api/v0/status` for
  the member (freshness walks its checkout; a member without one is
  `unknown` with the error "no checkout here", and its `settings` are
  `unknown`).
- `links`: the member in the last pass (`in_pass`, the run it linked,
  the index error the pass recorded), the links with this member at
  either end, the stale ones and their distinct reasons.
- `link_pass.state` is `none` (no pass stored), `fresh` or `stale`;
  `stale_reasons` say why the pass would link differently if run now
  (declarations changed, a member joined, left, failed to index or was
  indexed again); `outcomes` counts the pass's sources per claim status
  ([project links](../extract/project-links.md)).
- `checks` lists findings that are not errors. `same_remote`: two or
  more members are separate repository roots with the same origin URL
  (compared without scheme, credentials, host case, a trailing slash or
  `.git`), so one repository may be indexed under several IDs and its
  routes would be ambiguous between them. Folders of one repository
  sharing its remote are not reported.
- `declarations` (T022 B9): the manifest's `consumes` entries of members
  and of their link scopes (`consumer`, `provider`, `scope`,
  `default_scope`, and `text` worded as a declared link's assumption
  words it) and the declared link `scopes` (`repository`, `name`,
  `path`). Both empty means the default rules: every member's routes are
  candidates for every member's calls.
- `ambiguous` (T022 B9): the pass's `ambiguous` sources, `total` and
  `groups` by claim row and candidate providers (the repositories of the
  candidate routes), largest first; each group lists at most 50
  `sources` (the source, its template, the full candidate count and the
  listed candidates), `count` has them all. Nodes are named by
  `repository` and `identity` with `kind`, `name`, `file` and `line`
  (empty when the member no longer stores the node). Never a link.
- `conflicts` (T022 B9): stored in-repository links that a link scope
  declaration contradicts ("conflicts with declaration"): a call with one
  unconditional alternative that its own index linked (`linked`) to a
  route of one scope, while the `consumes` entries of the call's consumer
  that cover it and name its own repository name other scopes. Each has
  the `call`, the `consumer`, the `declared` entries, the `target` route
  and its `target_scope` (empty for the default scope). Computed from the
  current manifest and indexes (only when a member declares scopes);
  scopes never override an in-repository link, so the link stays.

#### GET /api/v0/project/callers?term=&repository=&depth=  and  /api/v0/project/impact?term=&repository=&depth=

`graphvein callers|impact --project --json`
(`query.ExecuteProject`, [queries](queries.md#project-scope---project)) with
`api_version` added: `term` (required) is an identity or a name, looked up
in the member `repository` names (optional; an unknown member is 400) or
in every member. `depth` is 1 to 10 (default 1 for callers, 3 for impact).
Inside a member the traversal follows exactly the single-repository
edges; from a route it follows the stored `calls_route` links back into
other members. Link steps carry the assumption, provenance `inferred`, the
link's confidence and, when stale, `stale_reasons`. A term found in
several members answers 200 with `"error": "ambiguous target"` and the
`candidates` (repository and identity), as the CLI's JSON does. The walk
has the shared reached-node and fan-out caps; `caps`, `bounds` and `ends`
report the caps that bound and the "? not modelled" ends, as in the CLI's
JSON.

#### GET /api/v0/project/changes/impact?repository=&base=&id=&group=&depth=&cursor=&limit=

`/api/v0/changes/impact` of the member `repository` (required with
several members), continued through the project's stored links into the
other members as `graphvein changes impact --project` does (relations I12
`calls_route` and I13 `documents_route`; ADR 0075 "Query and impact").
Same parameters, paging and errors as the member endpoint; a `ref:` base is
refused with 400, because links name the members' current indexes, not ref
snapshots. The response adds `project` and `members` (each other member's
unknowns, table uses and link sources it no longer stores). Cursors of the
two endpoints are not interchangeable (409 `index_changed`). The member
endpoint `/api/v0/changes/impact` stays single-repository in project mode.

#### GET /api/v0/project/links/changes?base=

The link journal diff of `graphvein changes --project`: links added,
removed or changed between the newest kept pass and `base` (`previous`,
the default, or `pass:N`; anything else is 400, an unknown pass 404
`base_unknown`). `state` says what was compared: `compared` (`links`
holds the diff), `same_pass` (the base is the newest kept pass: nothing
compared, which is not proof that no link changed; `links` is null and
`links_note` says so) or `no_comparison` (fewer than two passes are kept;
`links` is null with `links_note`). A member's own changes are
`/api/v0/changes/summary?repository=`.

#### Cross-repository links in the API section

`/api/v0/api/endpoint?repository=&id=` of an HTTP route adds `project`:

```json
"project": {
  "project": "users",
  "link_pass": {"state": "fresh", "pass": 1, "stale_reasons": []},
  "links_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "callers": [{"repository": "frontend", "identity": "client_call:...",
               "node": <presented call node>, "owner": {...},
               "kind": "calls_route", "provenance": "inferred", "confidence": 0.9,
               "evidence": [...], "assumption": "matched across project repositories; base URL not proven",
               "stale": false, "stale_reasons": [],
               "ide_status": {"available": true, "error": null, "label": "Open in VS Code",
                              "editor": "vscode", "source": "builtin"}}],
  "operations": [<the same for documents_route links from OpenAPI operations>],
  "possible_callers": [{"repository": "frontend", "identity": "client_call:...", "kind": "calls_route",
                        "row": "PL-R11", "status": "ambiguous", "wording": "ambiguous across repositories",
                        "reason": "", "candidate_count": 2, "candidate_repositories": ["axum", "express"]}],
  "declaration_conflicts": [{"call": "client_call:...", "consumer": "monorepo/web",
                             "declared": ["`monorepo/web` consumes `monorepo/orders` (bases: VITE_ORDERS)"],
                             "target": "route:...", "target_scope": "users"}]
}
```

`links_precision` is the scope label of every link listed (family
`cross_repository_links`, decided from the member's run): never
`precise`, since each link rests on its `assumption`
([precision](../extract/precision.md#cross-repository-links); T022 B11).
`callers` are the calls of other members the stored pass linked to the
route; the node is presented through its own member's checkout (IDE link,
file state, source root), and is null when that member no longer stores it
(a stale link). The node's IDE link follows the same precedence as every
other link: the request's `editor=` first, then the member's own
`graphvein.yaml` `ide`, then the built-in default; `ide_status` (additive,
the shape of the endpoint's `ide`) says which applied, and is absent when
the node is not presented through a member's checkout. The UI names such a
link and asks with the first-click chooser like any other. `possible_callers` (T022 review, replacing
`ambiguous_calls`) are sources of other members the pass did not link but
that may reach this route: an `ambiguous` outcome with the route among its
candidates (a list cut at ten counts a route sorting after its last listed
candidate), or an `incomplete` one (PL-R7, a candidate provider without a
stored index) with the route among the indexed providers' candidates
(`reason` names the provider). They are "may be called by", never a link
and never a caller. `declaration_conflicts` (T022 review) are
in-repository callers whose link a scope declaration contradicts; the UI
marks them "linked inside the repository; the declaration names another
scope", as `/api/v0/api/calls` marks the call (`project.declaration_conflict`,
null otherwise). The in-repository `callers` are unchanged. The `scope` and `root` parameters filter the
in-repository calls only. `/api/v0/api/endpoints?repository=` adds
`project_caller_count` and `project_possible_caller_count` (the route's
`possible_callers` that are calls) to each route row, and to the HTTP
transport `never_called_in_project` (routes with neither an in-repository
caller, nor a project link, nor a possible caller) and
`possibly_called_in_project` (routes with only possible callers: "possibly
called (ambiguous across repositories)", T022 review); `never_called`
keeps its single-repository meaning.
The HTTP transport also adds `project_linked_calls` (T022 B9): the calls
of the scope and root that a stored project link connects to a route of
another member; `calls.by_status` keeps the repository's own statuses.
`calls.project_by_status` and each root's `project_by_status` (T022
MR-UI) count the same calls by their project-scope status: a call linked
inside the repository stays `linked`; otherwise a stored project link
makes it `linked_across`, an `ambiguous` link pass outcome (several
routes of other members, no link) `ambiguous_across`, and every other
call keeps its repository status. In project mode the API section's
headline percentage, bar, cells and per-root rows use these counts, with
the repository's own as secondary text.

`/api/v0/api/calls` takes `status_scope=project` (serve --project, HTTP
only; 400 otherwise, and for a value other than `repository` or
`project`): `status` then matches each call's project-scope status, and
the answer carries `"status_scope": "project"`. The gap sheets request
it in project mode, so "Unmatched in the project" lists only the calls
the project did not link.

`/api/v0/api/flow?repository=&id=` of an HTTP route adds the same
`project` (T022 B9). Project links are not stored edges of the
repository, so they are not among `nodes` and `edges`.

Each call of `/api/v0/api/calls?repository=&transport=http` adds
`project` (T022 B9):

```json
"project": {
  "link_pass": {"state": "fresh", "pass": 1, "stale_reasons": []},
  "links_precision": {"label": "best_effort", "row": "D", "reason": "default_branch"},
  "links": [{"target": {"repository": "backend", "identity": "route:...", "kind": "route",
                        "name": "GET /users", "file": "src/server.ts", "line": 13},
             "kind": "calls_route", "provenance": "inferred", "confidence": 0.9,
             "evidence": [...], "assumption": "matched across project repositories; base URL not proven",
             "stale": false, "stale_reasons": []}],
  "outcomes": [{"template": "", "row": "PL-L1", "status": "linked", "reason": "",
                "candidate_count": 0, "candidates": [...]}]
}
```

`links` are the call's stored project links, `outcomes` the pass's
results for it (one per alternative of a call with several): why it has
a link or none. Both are empty when the pass has nothing on the call;
`targets` and `reasons` keep their single-repository meaning.

#### Re-index in project mode

`GET /api/v0/index/run` answers `"project": "<name>"` beside
`"repository": ""` (always present, so clients that read `repository` as a
string keep working; T038). While the server's own job runs the lock state is
`held_here`; otherwise the project lock is probed (SQLite: the store
lock). The job adds:

```json
"member": "frontend",
"project": {
  "members": [{"repository": "frontend", "state": "committed", "run": {"id": "...", "finished_at": "..."}, "error": null},
              {"repository": "ghost", "state": "no_checkout", "run": null, "error": null}],
  "link_pass": {"pass": 2, "links": 3, "added": 0, "removed": 0, "changed": 0}
}
```

`member` names the member being indexed (absent during the link pass,
whose phase is `linking`, and after the job). Member states are
`pending`, `running`, `committed`, `failed`, `no_checkout` and, in a job
started with `{"link_pass_only": true}`, `linked_from_store` (not indexed;
its stored index is linked). The job is
`committed` only when every member checkout committed and the pass was
stored; a member that failed (its lock held by another run, a scan or
store error) makes the job `failed` with code `index_failed`, the pass
still running with its previous index and marking its links stale, as
`graphvein index --project` does; a failed pass is `link_pass_failed`.
A POST while another process holds the project lock answers 409
`index_locked`; a member whose latest run came from an external indexer
409 `reindex_unsupported`.

### Project manifest endpoints (serve --project)

Manifest editing from the UI (T022 B10, ADR 0075 "Manifest editing from
the UI"). `graphvein-project.yaml` stays the source of truth: these
endpoints edit its declarations through the same writer as the CLI
(comments kept) and write nothing until a preview is confirmed. No request
names a file: the server writes only the manifest it was started with,
never `graphvein-project.local.yaml`, and adding a member by folder stays
in `graphvein project add`. A single-repository server answers 404.

#### GET /api/v0/project/manifest

```json
{"api_version": 0, "project": "users", "file": "graphvein-project.yaml",
 "hash": "sha256:…", "content": "<the file>", "token": "<session token>",
 "members": [{"repository": "frontend", "repository_source": "manifest", "checkout": true,
              "consumes": [{"repository": "backend", "scope": "", "default_scope": false,
                            "select": {"bases": ["VITE_API"], "origins": [], "path_prefixes": []}}],
              "scopes": [{"name": "admin", "path": "admin", "consumes": []}]}]}
```

The file as it is on disk now (with the local file, validated as the CLI
does; one that does not validate answers 409 `invalid_manifest`), its hash
and the server's session token: random per server process, readable only
by a page of this server (the Host, `Sec-Fetch-Site` and `Origin` checks
of every API request apply). A manifest over 256 KiB answers 413.

#### GET /api/v0/project/suggestions

`graphvein project suggest --json`'s listing (`proposals` with evidence,
`unmatched`, `unread`) for the manifest on disk (`hash`) and the stored
calls. Nothing is applied; accepting proposal N is the edit operation
`accept_suggestion`.

#### POST /api/v0/project/manifest

Headers `Content-Type: application/json` and `X-Graphvein-Token: <token>`;
body at most 16 KiB:

```json
{"base_hash": "sha256:…", "operations": [{"op": "accept_suggestion", "number": 1}]}
```

Without `apply` the answer is a preview; nothing is written:

```json
{"api_version": 0, "applied": false, "base_hash": "sha256:…", "result_hash": "sha256:…",
 "diff": "--- a/graphvein-project.yaml\n+++ b/graphvein-project.yaml\n@@ …",
 "valid": true, "problems": [], "changes": ["accepts suggestion 1: react consumes express (bases: __API_URL__)"],
 "reindex": {"kind": "link_pass", "reason": "only declarations change: the link pass re-links the stored indexes"}}
```

`diff` is the unified diff (3 lines of context) of the file; `problems`
are the validation errors of the result (rule table in
[project.md](project.md)); `reindex.kind` is `link_pass` when only
declarations change and `project` when a member is renamed (the new
name has no stored index). Adding `"apply": true, "result_hash":
"<the preview's result_hash>"` writes the result atomically (temporary
file and rename in the manifest's folder) and reloads the manifest; the
answer is the same document with `applied: true`. The links are then
stale until `POST /api/v0/index/run` (`{"link_pass_only": true}` for
`link_pass`, `{}` for `project`).

An applied rename also retires the old name (T022 B11): under the
project lock, taken before the file is written, the server removes from
the project store every link and outcome naming the old name, its member
record and its whole stored index (runs, files, nodes, edges,
diagnostics, fact cache, change journal, pins, ref snapshots) in one
transaction, so nothing stays under a name no manifest uses. The answer
then carries `retired`, one entry per old name:
`{"repository": "backend", "removed": true, "error": null}`; `removed`
is false with the reason in `error` when the server cannot write the
store, or when another stored project still names that repository (its
rows are kept). The link journal keeps its past passes. A re-index job
running refuses the rename with 409 `index_running`, and a project lock
held by another process with 409 `index_locked`; nothing is written
then. A rename made on disk is not retired: its old name's rows stay
until `graphvein project forget OLD-NAME` removes them
([project](project.md#commands)).

The preview of a rename lists, in `retiring`, each old name with a stored
index (T022 review): `{"repository": "backend", "stored": true,
"nodes": 412, "projects": ["users"], "standalone": false, "confirm":
"delete stored index of backend (412 nodes)"}`. `projects` are the
projects whose stored link pass lists it; `standalone` is true when none
does (the index was made on its own, so this may be the store's only
copy). The apply must list every such name in `"confirm_delete":
["backend"]`, else it is refused with 409 `delete_confirmation_required`
and nothing is written; the UI shows the entry and asks for that second
confirm (a checkbox labelled with `confirm`) before it enables Write.

Operations (1 to 32; each takes exactly the fields listed, any other
field, such as a file or member path, is 400):

| `op` | Fields (optional in brackets) | Writes |
|---|---|---|
| `add_consume` | `member`, [`scope`], `consume` {`repository`, [`scope`], [`default_scope`], [`select` {`bases`, `origins`, `path_prefixes`}]} | a consumes entry of the member (or of its link scope `scope`) |
| `remove_consume` | `member`, [`scope`], `target` {`repository`, [`scope`], [`default_scope`]} | removes that entry (an emptied list is removed) |
| `set_select` | `member`, [`scope`], `target`, `select` | replaces the entry's selector in place; `{}` clears it |
| `set_default_scope` | `member`, [`scope`], `repository`, `value` | `true`: the entry for the whole repository names only its default scope; `false`: the reverse |
| `add_scope` | `member`, `name`, `path` | a link scope; `path` must be relative, slash-separated, without `.` or `..`, and an existing folder inside the member's checkout after symbolic links, with every component in exactly the case on disk (the link pass compares scope folders by exact case; T022 review) (else 400 `path_refused`) |
| `remove_scope` | `member`, `name` | removes the scope with its consumes list |
| `rename_repository` | `member`, `to` | the member's name and every consumes entry naming it; a name the member's own `graphvein.yaml` sets wins, so the result is invalid |
| `accept_suggestion` | `number` | proposal N of the suggestions for the manifest on disk (a declared one is refused) |

Answers: 200; 400 `bad_request` (body, unknown or missing field,
operation count), 400 `edit_refused` (an operation names an entry, member
or scope that is not there), 400 `path_refused`, 400 `no_change` (apply of
an edit that changes nothing); 403 `token_required`, `not_loopback`,
`cross_site_request` or `host_not_allowed`; 409 `manifest_changed` (the
file's hash is not `base_hash`), 409 `preview_changed` (the edit no longer
gives `result_hash`, for example a suggestion's evidence moved), 409
`invalid_manifest` (the file on disk does not validate), 409
`index_running` or `index_locked` (a rename while the project is being
indexed); 413; 415; 422
`invalid_manifest` (apply of an invalid result). A refused edit leaves the
file untouched; edits are serialised.

### Try it endpoints (T029)

The "try it" proxy sends a request to a running instance of the API on
the user's behalf, through `internal/egress` (ADR 0040, ADR 0081). Claim
contract: [`docs/contract/claims/try-it.md`](../contract/claims/try-it.md)
rows `TI-*`; environments are configured in `graphvein.yaml` or the
project manifest ([configuration: Try it](../config.md#try-it)). The
browser never sends a URL: it names an environment, a stored route and one
of its path keys, and values; the server builds the URL, applies the
guardrails and answers with the upstream response as data.

Every POST below passes the checks of every state-changing POST (Host,
`Sec-Fetch-Site`, `Origin`, `http.CrossOriginProtection`), then needs a
loopback peer (403 `not_loopback`), this server process's session token in
`X-Graphvein-Token` (403 `token_required`; the token is in the
environments listing), `Content-Type: application/json` (415) and one JSON
object of the documented fields (400 `bad_request` for an unknown field,
a duplicate key; a body over 2 MiB, 96 MiB for `prepare` and `send`:
413 `too_large`). In project mode
every request names the member in `repository` (query parameter for GET,
body field for POST; optional with one member, else 400
`repository_required`). Session state (selection, acknowledgements,
grants, typed auth values, confirmations) is held in server memory only,
per member, and bound to the environment as its files describe it now
(origin, base path, label, auth): when a file edit changes any of these,
the environment is unselected and everything held for it is dropped.
Where a request cannot be proven safe but the user may have a reason to
send it (an untrusted certificate, a path that is not the operation's, a
missing auth variable, a redirect to another origin), the answer offers a
user-confirmed option: an edit of the environment through the guarded
edit below, or a one-time confirmation bound to the exact request. Some
error answers carry `error.details` (documented with the code).

**`GET /api/v0/tryit/environments[?repository=]`** lists the
environments of the repository (project mode: the manifest's environments
with a base URL for the member, then the member's own):

```json
{"api_version": 0, "repository": "api", "token": "…",
 "selected": "repository:local",
 "environments": [{"id": "repository:local", "name": "local", "label": "local",
   "production": false, "source": "repository", "file": "graphvein.yaml", "line": 4,
   "base_url": "http://127.0.0.1:8000/api", "origin": "http://127.0.0.1:8000",
   "auth": {"header": "Authorization", "env": "API_TOKEN", "env_set": true,
            "prefix": "Bearer ", "session_value": false},
   "selected": true, "acknowledged": false, "remembered": false,
   "options": {"tls": {"pins": [{"origin": null, "sha256": "9595c84a…"}], "ca_file": null},
               "use_system_proxy": false,
               "limits": {"request_bytes": null, "response_bytes": 16777216, "timeout_seconds": null}},
   "effective_limits": {"request_bytes": 1048576, "response_bytes": 16777216, "timeout_seconds": 30}}],
 "files": [{"target": "repository", "file": "graphvein.yaml", "exists": true,
            "hash": "sha256:…", "writable": true}],
 "config_error": null,
 "limits": {"request_body_bytes": 1048576, "response_body_bytes": 4194304,
            "timeout_seconds": 30, "confirmation_ttl_seconds": 120},
 "limit_ceilings": {"request_bytes": 67108864, "response_bytes": 268435456, "timeout_seconds": 600}}
```

`auth` fields are null without auth; no value is ever listed. `options`
are the environment's options as its file states them (`tls` null
without pins or CA file; a `limits` entry null where the default
applies; `limits` null when none is set); `effective_limits` are the
limits a send uses; `limits` at the top are the defaults and
`limit_ceilings` the highest values an environment may set. `files`
gives the `base_hash` of an edit (`"absent"` for a file that does not
exist); project mode adds `{"target": "project", "file":
"graphvein-project.yaml", …}`; `writable` is false for a member without a
checkout. `config_error` (project mode) says why a member's own
`graphvein.yaml` was not read. An invalid file answers 409
`invalid_config` / `invalid_manifest`.

**`POST /api/v0/tryit/environments`** adds an environment typed in the UI
(row TI-30), as the manifest edit does: preview, then apply.

```json
{"repository": "api", "target": "repository", "base_hash": "sha256:… | absent",
 "environment": {"name": "staging", "label": "staging", "base_url": "https://staging.example.com",
                 "auth": {"header": "Authorization", "env": "STAGING_TOKEN", "prefix": "Bearer "}},
 "apply": false, "result_hash": ""}
```

`target` is `repository` (the repository's `graphvein.yaml`, created with
`version: 1` when absent) or `project` (the manifest: a new project
environment with this member's base URL, or this member's base URL in an
existing one, whose label and auth are kept: send neither). `auth` is
optional and names a variable, never a value. The answer:

```json
{"api_version": 0, "applied": false, "target": "repository", "file": "graphvein.yaml",
 "base_hash": "…", "result_hash": "sha256:…", "diff": "--- a/graphvein.yaml\n+++ …",
 "valid": true, "problems": [], "change": "adds environment staging (…) to graphvein.yaml of api",
 "environment": "repository:staging"}
```

With `apply: true` and the preview's `result_hash` the file is replaced
atomically. Refusals: 409 `file_changed` (the file is not the one of
`base_hash`), 409 `preview_changed`, 422 `invalid_config` (nothing
written), 409 `environment_exists`, 409 `no_checkout`, 400 for an invalid
name, label or URL. The file is re-read on every request, so the new
environment is listed and selectable at once; nothing typed is dialable
before it is written.

The same endpoint changes the options of an existing environment (row
TI-48) when the request carries `options` instead of `environment`:

```json
{"repository": "api", "target": "repository", "base_hash": "sha256:…",
 "options": {"name": "dev",
             "tls": {"pins": [{"origin": null, "sha256": "9595c84a…"}], "ca_file": "certs/rootCA.pem"},
             "use_system_proxy": false,
             "limits": {"request_bytes": null, "response_bytes": 16777216, "timeout_seconds": 120}},
 "apply": false, "result_hash": ""}
```

`tls`, `use_system_proxy` and `limits` replace the environment's keys as
a whole: `tls: null` (or no pins and no `ca_file`), `false` and a null
limit remove them. A pin's `origin` is optional in a repository
environment (it has one origin) and required in a project environment
(one of its base URLs' origins): a pin is accepted only for its origin. Fingerprints are accepted with a `sha256:` prefix,
colons and in upper case, and written as 64 lower-case hex digits;
limits must be 1 to their ceiling. The answer, preview and apply are
those of adding an environment (`change` reads "sets the try-it options
of environment dev in graphvein.yaml"); 400 `edit_refused` when the file
has no such environment. With `target: "project"` the manifest's project
environment is changed, for every member. A change of `tls` or
`use_system_proxy` (or of the CA file's content) changes the
environment's identity: it must be
selected (and acknowledged, for production) again.

**`GET /api/v0/tryit/suggestions[?repository=]`** lists base URL
candidates (rows TI-31..TI-41), never used until the user picks one and
the edit above writes it:

```json
{"api_version": 0, "repository": "api",
 "notice": "suggested, not used until you pick one; …",
 "suggestions": [{"url": "http://127.0.0.1:8002", "source_kind": "compose_published_port",
   "row": "TI-31", "file": "docker-compose.yml", "line": 2,
   "reason": "Compose service api publishes container port 8000 on port 8002 of this machine"}]}
```

`source_kind` is one of `compose_published_port`, `traefik_router`,
`openapi_server`, `frontend_env` (project mode), `listen_call`,
`uvicorn_run`, `go_listen`, `package_json_script`, `procfile`,
`dockerfile_expose`, `framework_default` (file and line null), in that
order.

**`POST /api/v0/tryit/select`** records the UI's environment switch:
`{"repository", "environment": "<id>", "acknowledge_production": false}`
(an empty `environment` selects none).
Selecting another environment (or `""`, none) revokes every production
acknowledgement, every "don't ask again" grant and every pending
confirmation. A production environment (label `production` or `prod`)
needs `acknowledge_production: true` once per selection before any send
(the UI's dialog). Answer: `{"api_version", "selected", "production",
"acknowledged", "acknowledgement_required", "remembered"}`.

**`POST /api/v0/tryit/auth`** holds an auth value typed for this server
session: `{"repository", "environment", "header": "", "value": "…"}`
(header defaults to the environment's `auth.header`, else
`Authorization`), or `{"repository", "environment", "clear": true}`. The
value is kept in memory only, wins over the environment variable, is
never written, logged or returned: the answer is `{"api_version",
"environment", "header", "session_value"}`.

**`POST /api/v0/tryit/prepare`** and **`POST /api/v0/tryit/send`** take
the same request:

```json
{"repository": "api", "environment": "repository:local",
 "route": "route:app/main.py#POST%20%2Fitems%2F", "path": "/items/", "method": "POST",
 "path_values": {"id": "42"}, "free_form": false, "query": [{"name": "q", "value": "x"}],
 "headers": [{"name": "X-Trace", "value": "1"}],
 "body": "{\"name\": \"a\"}", "content_type": "application/json", "multipart": null,
 "send_without_auth": false, "confirmation": "", "remember": false}
```

`route` is the stored route identity and `path` one of its path keys
(the template, with the served trailing slash), exactly as the API
section lists them; `method` must equal the route's (400
`path_mismatch`, `method_mismatch` otherwise).

- **Free-form path** (row TI-26): with `free_form: true`, `path` is a
  literal path appended to the environment's base URL (for example
  `/admin/users/7`), `method` any of the eight OpenAPI methods, no
  `path_values`; `route` must still name a stored route (the operation
  the user started from). The path starts with `/`, has no `?` or `#`
  (query pairs go in `query`); characters a path segment cannot hold are
  percent-encoded, a valid `%XX` is kept; a `.` or `..` segment, plain or
  encoded, is refused (400 `invalid_path_value`). It always needs a
  one-time confirmation; "don't ask again" never covers it.
- **Multipart** (row TI-45): `multipart` replaces `body` and
  `content_type` with parts the server builds into a
  `multipart/form-data` body: `[{"name": "title", "value": "hello"},
  {"name": "avatar", "filename": "a.png", "content_type": "image/png",
  "data_base64": "iVBORw0…"}]`. Each part has `name` and exactly one of
  `value` (text) and `data_base64` (standard base64); a part with
  `filename` is a file (type `application/octet-stream` unless given).
  At most 128 parts; the built body counts against the environment's
  `request_bytes`. The boundary is derived from the content, so
  `prepare` and `send` build the same bytes.
- **Sending without auth** (row TI-47): when the environment's `auth.env`
  variable is not set and no value was typed, `prepare` answers
  `auth_missing` and no confirmation; `send` refuses with 400
  `auth_missing` (`details: {"header", "env"}`) unless the request sets
  `send_without_auth: true` (GET and HEAD included). The choice is part
  of the confirmation; it is refused (400) while auth is available.

`prepare` sends nothing and answers the exact request:

```json
{"api_version": 0, "environment": "repository:local", "label": "local", "production": false,
 "method": "POST", "url": "http://127.0.0.1:8000/items/", "headers": ["X-Trace", "Content-Type"],
 "free_form": false, "auth_header": "Authorization", "auth_source": "env",
 "auth_missing": null, "send_without_auth": false, "body_bytes": 13, "body_sha256": "…",
 "multipart": null,
 "limits": {"request_bytes": 1048576, "response_bytes": 4194304, "timeout_seconds": 30},
 "proxy": null, "proxy_cleartext": false,
 "confirmation_required": true, "confirmation_reasons": ["method"],
 "confirmation": "…", "expires_at": "2026-11-20T10:02:00Z",
 "acknowledgement_required": false, "selected": true}
```

`auth_missing` is `{"header": "X-Api-Key", "env": "API_KEY"}` when the
variable is not set; `multipart` lists `{"name", "filename",
"content_type", "bytes"}` per part (never content); `proxy` is
`"http://proxy.example:3128"` (scheme, host and port, never user info)
when the environment uses the system proxy, and `proxy_cleartext` is
true when the environment is `http://`: the proxy then sees the whole
request, headers and auth value included, in clear. `confirmation_reasons` are
`method` (not GET or HEAD), `method_override` (a GET or HEAD with
`X-HTTP-Method-Override`, `X-HTTP-Method`, `X-Method-Override` or a
`_method` query parameter), `body_on_get_or_head` (a GET or HEAD with a
body or content type) and `free_form_path`. `confirmation` is null when
none is needed, or while `auth_missing` is set without
`send_without_auth`.

`send` sends it, only to the selected environment (409
`environment_not_selected`), after the production acknowledgement (409
`production_acknowledgement_required`), and, when `prepare` says a
confirmation is required, with `confirmation` set to prepare's id for
exactly this request (one use, 120 s) or, except for a free-form path,
after a grant (409 `confirmation_required`). With a valid confirmation,
`remember: true` grants the environment until the UI selects another
one. The answer:

```json
{"api_version": 0, "environment": "repository:local", "method": "POST",
 "url": "http://127.0.0.1:8000/items/", "status": 201, "status_text": "Created",
 "headers": [{"name": "Set-Cookie", "value": "[redacted]"}],
 "body": "{…}", "body_encoding": "text", "body_bytes": 42, "truncated": false, "truncated_reason": null,
 "truncated_limit": null, "redirect": null, "redirect_follow": null, "duration_ms": 12, "remembered": false}
```

A non-text body is the hex of its first 4 KiB (`body_encoding: "hex"`).
`truncated_reason` is `"size"` (the response limit), `"timeout"` (the
time limit cut the body after the status arrived; the status is the one
received) or `"read_error"`; `truncated_limit` then names the limit:
`{"limit": "response_bytes", "value": 4194304, "ceiling": 268435456,
"hint": "raise limits.response_bytes of the environment (at most
268435456) through the environment edit"}`.

Redirects are never followed automatically: `redirect` holds the
`Location`, and for 301, 302, 303, 307 and 308 with an http(s) target
`redirect_follow` says how to follow it once (row TI-46):

```json
"redirect_follow": {"url": "https://login.example.com/start?next=a", "origin": "https://login.example.com",
  "same_origin": false, "method": "GET", "keeps_body": false,
  "environment": null, "path": "/start", "query": [{"name": "next", "value": "a"}],
  "suggested_environment": {"name": "login.example.com-443", "base_url": "https://login.example.com"}}
```

`method` is what a browser would use (303, and POST on 301 and 302,
become GET; 307 and 308 keep the method and, `keeps_body`, the body).
`environment` is the environment of this repository whose base URL holds
the target, with `path` relative to its base URL; when none does,
`suggested_environment` proposes adding the target's origin through the
guarded edit above (`path` is then relative to the origin). The user
selects that environment and sends the target as a free-form request
(`free_form: true`, `path`, `query`), with its own confirmation and that
environment's production rules.

`Sec-WebSocket-*` headers: 400 `not_supported` ("WebSocket endpoints are
not supported in try-it yet"); `HTTP2-Settings`: 400 `not_supported`.

Upstream failures: 502 `destination_blocked`, 502 `upstream_error`, 502
`upstream_tls_error` (a TLS failure other than the certificate), 504
`upstream_timeout` (`details`: the `timeout_seconds` limit); 429
`too_many_requests` beyond 4 sends at once; 413 `body_too_large`
(`details`: the `request_bytes` limit); 409 `ca_file_error` (the
environment's `tls.ca_file` is missing, not a regular file, larger than
4 MiB, unreadable within the environment's time limit or has no PEM
certificate); 409 `invalid_proxy` (the environment uses the system
proxy and its variable is not a usable proxy URL); 502
`proxy_tls_error` (TLS with an `https` proxy failed: its certificate is
checked against the system roots for the proxy's own name, never with
the environment's pins or CA file).

A certificate that is neither trusted nor pinned answers 502
`upstream_certificate_untrusted`, or `upstream_certificate_changed` when
the environment pins other certificates (rows TI-29, TI-17), with:

```json
{"error": {"code": "upstream_certificate_untrusted", "message": "…",
  "details": {"environment": "repository:dev", "origin": "https://127.0.0.1:8443",
    "certificate": {"subject": "CN=dev box", "issuer": "CN=dev box", "sans": ["localhost", "127.0.0.1"],
                    "not_before": "2026-11-01T00:00:00Z", "not_after": "2027-11-01T00:00:00Z",
                    "sha256": "9595c84a…", "self_signed": true},
    "reason": "x509: certificate signed by unknown authority", "pinned_sha256": []}}}
```

`sha256` is the SHA-256 of the leaf certificate's DER, lower-case hex
(what browsers and `openssl x509 -fingerprint -sha256` show, without
colons). To trust it the UI writes it into the environment's
`tls` pins with the options edit (`pinned_sha256` lists the pins of
this origin); a pinned certificate is
accepted for that environment's origin whatever its names, issuer or
validity. A changed certificate is never accepted silently.

Limits: body sent 1 MiB, response read 4 MiB, 30 s per exchange unless
the environment raises them (ceilings 64 MiB, 256 MiB, 600 s), 10 s per
dial. The log records environment, label, method, path template
(`(free-form)` for a free-form path), status,
sizes and duration, never values or bodies; nothing is stored.

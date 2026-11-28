# ADR 0087 - Multi-index server and the workspace registry

**Status:** Accepted (delegated review, 2026-11-25)

## Context

Until now one `graphvein serve` process served one index: a repository
(`serve <path>`) or a project (`serve --project`). Switching to another
repository meant stopping the server and starting another, and the UI
could not list what else was indexed on the machine. The T038 workspace
(the T038 task record (internal); developer decisions of 2026-11-19/20 and
the mockup round of 2026-11-24, every recommended default accepted)
asks for a sidebar listing every indexed repository and project with its
freshness, switching without a restart, keeping recently used indexes
warm, adding a folder from the UI, and links that name the index.

Constraints: ADR 0041 (loopback only, Host allow-list, Fetch Metadata and
Origin checks, read-only store for requests), ADR 0052 (the index lock;
a re-index runs inside `serve` with its own writer store), ADR 0075
(projects: one store per project, members keyed by repository), the
boundary rules (only `internal/egress` opens sockets, all persistence
through the `store` interface) and VISION's precision rule: nothing may be
shown as fresh or present that the store did not prove.

## Options considered

### Where the list of indexes lives (D1)

- **(a) A registry file in the user configuration directory (chosen).**
  `os.UserConfigDir()/graphvein/workspace.yaml` (Windows `%AppData%`,
  macOS `~/Library/Application Support`, Linux `$XDG_CONFIG_HOME` or
  `~/.config`), overridable with `GRAPHVEIN_WORKSPACE_FILE`. An entry is a
  pointer only: kind, name, resolved folder, time added.
- (b) Discovering `.graphvein/index.db` files under configured roots.
  Walks the disk, finds stale copies, cannot see Postgres stores.
  Rejected.
- (c) Flags only (`serve A B --project C`). Loses the list on every start.
  Rejected.

### How serve starts (D2)

- **Keep both existing forms and add the registry (chosen).** `serve`
  with no path and no `--repository` serves the registry; `serve <path>`
  and `serve --project` behave exactly as before (store opened at start,
  same exit codes) and register their index. Their printed line is
  unchanged while the workspace has no other entry and gains
  `(+N in the workspace)` otherwise.
- New flags (`--workspace`). More surface for the same result. Rejected.

Whether the command-line forms register was left open by the design
round; they do: serving an index is the interactive act that puts it in
the sidebar. `graphvein index` does not register (D1 listed it): index
runs in scripts, CI, temporary copies and corpus checks would fill the
registry with folders that vanish. This is an open point for the
developer.

### One process, several indexes

- **(a) Dispatch by entry inside one server (chosen).** The single-index
  routes are unchanged and built per open index (`indexHandler`); a
  workspace dispatcher reads `ws=<entry id>`, removes it from the query
  and hands the request to that index's routes. Without `ws` the current
  entry answers, so every existing URL, the embedded UI before its
  workspace update, the smoke test and scripts keep working.
- (b) Path prefixes (`/w/<id>/api/v0/...`). Every client URL changes, and
  the UI's hash links and the CSP's `connect-src 'self'` gain nothing.
  Rejected.
- (c) One server process per index behind a proxy. More processes,
  ports and lifecycle code; the egress boundary would need a dialer.
  Rejected.

### Warm indexes (D3)

- An opened index keeps its read store, lockers, runner, change
  endpoints and try-it state. At most 8 are open; beyond that the least
  recently used index without a request in flight, a running re-index or
  ref job, and not the command-line index, is closed. The limit can be
  exceeded while all are busy (closing one would cancel a user's run).
- Answer cache: only GET views that read nothing but the store and
  `graphvein.yaml` (`/api/v0/api/endpoints`, `/precision`,
  `/schema/summary`, `/schema/sets`, `/schema/assumptions`,
  `/architecture`, `/architecture/chain`). Views that present file states,
  editor links or source lines depend on the files on disk, which change
  without a new run, so caching them could show a stale "unchanged":
  they are never cached, nor are status and freshness (ADR 0041).
  An answer is keyed by path and query and served only while the index's
  fingerprint is unchanged: the latest run (count, id, finish time) of
  the repository or of every member, the content hash of each
  `graphvein.yaml`, and for a project the manifest files and the last
  link pass. A commit by another process therefore misses at once; the
  end of this server's own run additionally drops the cache and starts a
  new generation, so an answer computed during the run is never stored.
  Bounds: 64 MiB per index, 256 MiB per server, answers over 4 MiB not
  cached (the design's numbers; not yet measured on the largest corpora,
  see Known gaps). Every cached path answers `X-Graphvein-Cache: hit`,
  `miss` or `bypass` (fingerprint unreadable: computed, not cached).

### Freshness of entries that are not current (D4)

- **Background checks, one at a time (chosen):** every entry once after
  start, then the open entries every 30 s. An entry shows no freshness
  until its check finished (the UI's hollow dot); never `fresh` from the
  registry. A project folds its members: `fresh` only when every member
  is, `stale` when any member is (proven), `not_indexed` when every member
  is, otherwise `unknown`. A folder that is gone is not checked.
- Checking only when opened. The sidebar would show no freshness for most
  entries. Rejected.

### Typed folder paths (D5)

Recorded as the revision of ADR 0041 (typed paths behind the guards):
`POST /api/v0/workspace/check` validates a path and returns a one-time
confirm value; `POST /api/v0/workspace/add` needs it. Rules and refusals
are the claim rows WS-21..WS-38.

### Removed or moved folders (D6), cancel (D7), projects (D8)

- An entry whose folder is gone stays listed (`folder_gone`) until the
  user removes it; nothing drops an entry by itself.
- No cancel endpoint; stopping `serve` cancels a running job (ADR 0052).
- Only an existing project manifest can be added; projects are created
  with `graphvein project init` and edited with the manifest editor.

### Identity in links and view memory (D9)

`id = <name>~<first 8 hex of SHA-256(kind, NUL, path, NUL, name)>`: the
name keeps a link readable, the hash separates two entries of the same
name and is the same on every start. A link carries `ws=<id>` (and
`repository=` for a project member); an id the server does not have is
404 `unknown_entry`, never another index in its place.

## Decision

1. `internal/workspace` owns the registry file: strict YAML (`version: 1`,
   unknown fields refused), atomic replace through a temporary file in the
   same folder, a damaged file reported and never rewritten, entries never
   dropped except by remove. The store location is not stored: each entry
   is opened from its folder as the CLI opens it, under the `serve`
   process's `GRAPHVEIN_SQLITE_PATH` / `GRAPHVEIN_POSTGRES_URL`, so no URL
   or secret is ever written. (D1 suggested recording the store location
   and a Postgres variable name per entry; the process environment
   already decides it for the CLI, so storing it would only let the two
   disagree.)
2. `server.Options.Workspace` makes a server a multi-index server: the
   command layer passes the registry, the command-line index (opened by
   the caller, never closed by the server) and an `Open` function that
   opens an entry exactly as `serve <path>` or `serve --project` would.
   Every `serve` form now builds a workspace server; `server.Options`
   without `Workspace` keeps the single-index server unchanged (tests,
   `server.Reader` for MCP).
3. Endpoints: `GET /api/v0/workspace`, `POST /api/v0/workspace/check`,
   `/add`, `/remove` (docs/cli/serve.md "Workspace"); `ws=` on every
   per-index endpoint. Claim contract:
   [workspace](../contract/claims/workspace.md) (WS-1..WS-40, WS-D).
4. `GET /api/v0/index/run` always carries `repository` (empty in project
   mode, beside `project`), so the UI's parser accepts project mode.

## Consequences

- One server, one port, one session token per process for all indexes;
  every guard of ADR 0041/0052 applies unchanged to every index.
- A serve process can hold up to 8 open stores (SQLite read connections
  or Postgres pools) and their caches; memory grows with use up to the
  bounds above plus each index's own state.
- The first file Graphvein writes outside the indexed folders (besides
  stores configured there): the registry. Tests and the serve smoke test
  point `GRAPHVEIN_WORKSPACE_FILE` at a temporary file.
- `serve` with no argument no longer means `serve .`: it serves the
  registry, with the working directory's registered entry as current.
  Scripts that relied on the old meaning must pass `.`.
- The runtime egress test of `serve` still sees exactly one loopback
  listen; the background checks and the registry are file operations.

## Revision 1: registry lock, folder casing, status freshness

Recorded after the T038 server follow-ups; accepted with the ADR on 2026-11-25.

### Registry writes

- **An operating-system lock on a lock file (chosen).** Every write (add,
  remove) takes an exclusive lock on `workspace.yaml.lock` next to the
  registry, then reads the file, changes it and replaces it atomically,
  so two `serve` processes never lose each other's entries. The lock is
  `flock(2)` on Linux, macOS and the BSDs (the standard `syscall`
  package) and `LockFileEx` on Windows (`golang.org/x/sys/windows`,
  already in the module graph through the SQLite driver, now a direct
  requirement); no cgo. The lock belongs to the open file, so two
  registries in one process exclude each other as two processes do.
  A writer waits up to 10 s, then fails with "held by another process"
  and leaves the file unchanged; serve logs it and goes on (WS-6). Reads
  take no lock: the atomic replace already gives them a whole file.
- **Stale locks:** the kernel releases the lock when its holder exits or
  crashes, so the lock file left behind is an empty file that blocks
  nobody. It is never deleted: deleting it while another process waits
  on it would let two processes lock two different files.
- A lock file created with `O_EXCL` and removed after the write.
  Portable, but a crash leaves it behind, and telling a stale lock from a
  slow writer needs a timeout and a race-free takeover. Rejected.
- Platforms other than Linux, macOS, the BSDs and Windows (none is a
  release target) keep the in-process serialization only.

### Folder casing

- **Resolve to the file system's own spelling (chosen).**
  `workspace.Resolve` (used for every typed path, the command-line folder
  and the working directory) returns each component as its parent folder
  lists it. On Windows `filepath.EvalSymlinks` already does this and
  upper-cases the drive letter; elsewhere a component missing from its
  parent's listing is replaced by the one listed name that differs only
  in case and that `os.SameFile` confirms is the same folder. On a
  case-sensitive file system nothing changes.
- **Compare with the file system's answer:** `workspace.SamePath` and
  `workspace.Inside` treat two spellings that differ only in case as one
  folder when both exist and `os.SameFile` says so, never from the text
  alone (a case-sensitive APFS volume or Linux holds both). They are used
  for registered-folder, inside-registered, home, configuration-folder and
  working-directory matches, and an add matches an existing entry of the
  same kind and name this way (an entry written before this rule or by
  hand).
- A case-folded comparison on Windows and macOS only. Wrong on
  case-sensitive volumes of either. Rejected.

### Status freshness

- A `GET /api/v0/status` answered for an entry (or a project member)
  records its freshness as that entry's (or member's) at once; a project
  is folded again over its manifest's members, a member never checked
  counting as unknown, with the oldest member's `checked_at`. A recorded
  check is replaced only by a later one, so a background check that
  started before a status request never overwrites its answer.

## Revision 2: review (list precision, typed-path rules, one folder one entry)

**Status:** Accepted (delegated review, 2026-11-25)

An independent review of the first build found the list claiming more
than it knew, a read path writing into user folders, and gaps in the
typed-path rules. The decisions below are proposed for review;
claim rows changed: WS-4, WS-8, WS-9, WS-11, WS-15, WS-20, WS-36, WS-37;
new rows WS-53 and WS-54 (server) and WS-55 and WS-56 (web UI), taken
from the reserved range WS-41..WS-69.

### The list never writes and never claims more than a check found

- **Folder gone first (R4).** `folder_gone` takes precedence over `open`:
  an open index whose folder was moved is listed gone (its store stays
  open until idle), its recorded freshness is not shown, and a check that
  finds the folder gone drops the record.
- **No lock probe on the list (R6).** The list reports each open entry's
  job from the runner's memory and touches an entry's folder only with a
  stat (and reads project manifests). The SQLite lock probe answers
  "free" when the lock file's folder is missing instead of creating it
  (a holder keeps its file open, so nobody can hold a lock there). A
  per-index request for a gone folder is refused (`folder_gone`), so a
  run cannot recreate it either. Considered: keeping the probe and only
  skipping gone folders (rejected: a race between the stat and the probe
  still recreates the folder).
- **Projects fold over the members listed now (R3).** A project's
  freshness is computed at each list over the manifest's current members;
  a member without a recorded check makes the project `null` (not
  checked), never fresh. Considered: comparing the member sets of the
  last check and the manifest (rejected: equivalent but adds state).
- **Entries that are not open age (R5).** A pass re-checks an entry that
  is not open once its check is older than 2 minutes; the bound keeps a
  dot at most about two and a half minutes old while the walks stay one
  at a time (the largest walk measured, langflow's, took 0.86 s, so an
  entry nobody looks at costs about one such walk every two minutes). The tooltip states the check's age.
  Considered: a hollow dot for old checks in the UI only (rejected: the
  state then never becomes true again without opening the entry).
- **No client time on server states (R2).** The UI shows the server's
  `checked_at` of each member and never stamps its own clock.

### Adding and indexing

- **Starting a run creates the store (R7).** `POST /api/v0/index/run?ws=`
  opens the entry with create, as add does, so an entry added without a
  run (or whose `.graphvein` was deleted) can be indexed from its row
  ("Index it"). Every other request still opens the store as it is. An
  add asks the check loop for a pass at once, so the new entry reads
  "not indexed" rather than "not checked yet".
- **An add that registered says so (R9).** When the index cannot be
  opened or the run is refused after the entry was registered, the add
  answers 200 with the entry, `job` null and `run_error`; the dialog says
  the folder was added and indexing did not start, with a retry.
  Considered: unregistering the entry on a run failure (rejected: a
  second process may already list it, and the user's intent to add it
  stands).

### Typed-path rules (R8)

- **System folders (WS-53, `system_directory`).** `/tmp`, `/etc`, `/dev`,
  `/proc`, `/sys`, `/var`, `/usr`, `/bin`, `/sbin`, `/boot` themselves
  (macOS also their `/private` targets) and anything inside the virtual
  `/dev`, `/proc` and `/sys`; on Windows the system drive's `Windows`,
  `Program Files`, `Program Files (x86)` and `ProgramData`, and the
  folders their environment variables name. Folders inside the others
  (`/tmp/app`, `/usr/src/app`) stay allowed: they hold real projects.
- **Parents of registered entries (WS-54).** Allowed (a monorepo above
  its parts is legitimate); the check lists the registered entries it
  contains and the confirmation names them, since its index reads their
  files again into a store of its own. Considered: refusing them as the
  review suggested (rejected: indexing a monorepo above its parts is a
  legitimate use).

### One folder, one entry; one SQLite file, one index

- **Identity by folder.** The registry treats a folder already registered
  (`SamePath`) as the same entry whatever name or kind it is added
  under, so `serve <path>` after a rename of `graphvein.yaml`'s
  repository does not add a second entry; the entry keeps its id (links
  stay valid) and, while its index is open, shows the name
  `graphvein.yaml` (or the manifest) gives now. A server opening an entry
  reads the repository from `graphvein.yaml` when it names one, else
  keeps the registered name (which may come from `--repository`).
- **`GRAPHVEIN_SQLITE_PATH` applies only to the index named on the
  command line.** It names one file; opening every entry on it answered
  every entry from one store (an entry indexed elsewhere answered "no
  index"). While it is set, the server opens no other entry: they answer
  409 `single_store` with the reason. Considered: ignoring the variable
  for other entries (rejected: project runs and the CLI's own helpers
  read it, so a run started from the UI would write somewhere else than
  the server reads).

## Revision 3: names shared in one store, neutral not-indexed dot

**Status:** Accepted (delegated review, 2026-11-25)

The review left one wrong-fact risk open: under
`GRAPHVEIN_POSTGRES_URL` every entry's index lives in one database, which
keeps a repository's facts by its repository ID and a project's links by
its name, not by folder. Two registered folders with one repository name
(two copies of a checkout, or two unrelated folders that happen to share
a directory name) read whichever folder was indexed last, and a freshness
check of one compares its files with the other's facts (two identical
copies would even read "fresh"). New claim rows WS-57 (server) and WS-58
(web UI); rows WS-8, WS-15, WS-20, WS-41, WS-54 and WS-55 changed.

- **Both entries are refused (WS-57).** An entry's names are its
  repository (`graphvein.yaml`, else the registered name; an open index's
  names as it opened, `--repository` included) or, for a project, its
  name and each member with a checkout. Two entries whose folders differ
  and that share a name are both in conflict: every per-index request
  answers 409 `shared_store_conflict` (also for an index already open,
  since the other entry may have been registered since), the background
  check records nothing, the list carries `conflict` (the name and the
  other entry) and no freshness, a typed path that would add another user
  of a name is refused before its store is read, and `serve <path>` or
  `serve --project` exits 1 before registering. A member whose checkout is
  the other entry's own folder (a project and its member registered on
  its own) reads that folder's facts and is no conflict; a member without
  a checkout reads the shared store by design (ADR 0075) and claims
  nothing.
  Considered: refusing only the entry registered second (rejected: the
  store holds whichever folder was indexed last, so the first entry's
  answers may already be the second folder's); keying the shared store by
  folder (rejected here: a storage schema and contract change, and it
  would break the intended sharing of a member without a checkout).
- **After a rename, both folders are indexed again.** The facts stored
  under the shared name are whichever folder was indexed last, so the
  message asks for both folders to be indexed again. Until then the
  folder that kept the name is answered from what is stored; its
  freshness check compares its files with those facts, so differing
  content reads `stale` (live check: two identical copies read `fresh`,
  which is true of the content).
- **The registered name stands in when the folder cannot say.** A folder
  that is gone, or whose `graphvein.yaml` or manifest does not load,
  claims its registered name, since facts indexed under it may still be
  stored. So moving a folder and serving it from its new place conflicts
  with the old entry until that entry is removed: a refusal, never a
  wrong answer.
- **`GRAPHVEIN_SQLITE_PATH`** shares nothing between entries since
  revision 2 (only the command-line index opens on it), so no conflict
  rule applies there.
- **Neutral dot for "not indexed" and "unknown" (WS-41, WS-55).** Amber is
  kept for `stale`, a proven change; `not_indexed`, `unknown` and words the
  UI does not know draw the hollow dot of "not checked yet", which claims
  nothing. "not indexed yet · Index it" is one line under the entry's row
  that never wraps; the row keeps the name, truncated when long (on the
  row itself, at the sidebar's 187 px, the line would leave the name no
  width).
- **`contains` lists only folders that exist (WS-54).** A registered
  folder that is gone is no part of what the index would read.

## Revision 4: re-check of the fix rounds

**Status:** Accepted (delegated review, 2026-11-25)

A focused re-check of revisions 2 and 3 found four places where the
server still said more than it knew. Claim rows changed: WS-8, WS-9,
WS-15, WS-53, WS-57.

- **A member's check is for a path (WS-9).** Each member's recorded check
  keeps the path the manifest gave when it was checked; the list shows it
  only while the manifest on disk still gives that path, so a member
  re-pointed while its project is open is `null` (and the project with
  it) instead of the old folder's state. A member's `GET /api/v0/status`
  now reads an edited manifest first, as project requests do (T022 B11),
  so it checks the new folder. Considered: closing an open project when
  its manifest changes on disk (rejected: it drops warm state and jobs
  for an edit the project requests already pick up).
- **Writes test the folder first (WS-15).** A request that may write
  (any method but `GET` and `HEAD`) tests whether the entry's folder is
  gone before it uses the index, also when the index is already open; a
  run of an open index whose folder was moved recreated the folder and
  committed an empty index. Reads of an open index still answer (R4).
- **More system folders, and Windows spellings (WS-53).** `/run`
  (virtual, like `/dev`), `/lib`, `/lib64` and `/var/lib` are refused
  themselves; folders inside `/run` stay allowed, since removable drives
  mount under `/run/media`. On Windows a path is compared after removing
  `\\?\`, `\\.\` and `\\localhost\X$\` prefixes, applying `.` and `..`
  and dropping trailing dots and spaces (which Windows ignores), and
  every candidate is also compared on disk with `os.SameFile`, which
  catches 8.3 short names.
- **A member without a path claims its name (WS-57).** Under a shared
  store such a member reads whatever is stored under its repository ID,
  which revision 3 let it do whatever wrote it. It now claims the name
  without a folder: an entry that claims the name from a folder is in
  conflict with its project (the member would serve that folder's facts
  as its own); two members without a path share the name, as ADR 0075
  intends. Considered: refusing only that member's requests (rejected:
  the project's link pass reads the member too, and one conflict model
  per entry keeps the list, the typed-path check and `serve` agreeing).
- **Names as served (WS-8).** The row now says what the code does: an
  open entry's name is the one its index serves (as opened, or the
  manifest the open project last read), not what the files say now.

## Revision 5: one store resolver for every entry (T068 part 2)

**Status:** Accepted (delegated review, 2026-11-28)

[ADR 0093](0093-per-user-store-location.md) puts new indexes in a
per-user stores folder and makes one function, `storeloc.Resolve`, decide
every folder's store. This revision makes the server use it. Claim rows
WS-70..WS-81 (new) and WS-12, WS-15, WS-21, WS-37 and WS-57 (changed) of
[the workspace contract](../contract/claims/workspace.md).

### `single_store` is removed

- **Each entry resolves its own store (chosen).** The entry named on the
  command line resolves with `Scope{CommandLine: true}`, so
  `GRAPHVEIN_SQLITE_PATH` names its store; every other entry resolves with
  `CommandLine: false` and opens from its own store. Revision 2 rejected
  ignoring the variable for other entries because project runs and the
  CLI's helpers read it themselves, so a run started from the UI could
  write where the server does not read. That reason is gone: an entry is
  resolved once when it opens, and its read store, index and project
  locks, writer, snapshot folder and project runs (member runs and the
  link pass) all use that location (`storeAt` in `cmd/graphvein`), never
  the environment. A test proves another entry never opens the
  variable's file (WS-70).
- Keeping 409 `single_store`. It made every other entry unusable while
  the variable was set, the audit's finding. Rejected.

### A store that moved is never recreated

- **The place is checked before and after the index lock (chosen).** A
  move holds the store's index lock while it copies, and deletes the old
  files afterwards; a re-index that waited for that lock would then
  create an empty store at the old place, which the two-index refusal
  would name at once. Every lock of an entry (re-index, project, member)
  checks before it creates the store folder and again once held that the
  folder still resolves where the entry opened; otherwise the run stops
  with `storeloc.ErrMoved`, answered 409 `store_moved` (WS-81). The
  writer checks too. `graphvein index` and `index --project` hold their
  announced place the same way.
- Checking only in the writer. The lock folder (and `folder.txt`) would
  already have been recreated at the old place. Rejected.

### What else the server reads through the resolver

- The working directory is suggested when the resolver finds an index of
  it, wherever it is kept, instead of when it holds a `.graphvein` folder
  (WS-72).
- A folder with two indexes (or whose store folder belongs to another
  folder) answers 409 `two_indexes` (`foreign_store`) on every
  per-index request and is refused as a typed path (WS-71), instead of a
  500 `store_error`.
- Moving an entry's store from the UI (ADR 0041 revision 2) stops serving
  that entry while it moves: its requests answer 409 `store_moving`, the
  background check skips it, requests in flight finish (at most 30 s),
  its index is closed (the command-line index through
  `WorkspaceIndex.Close`, which `serve` now supplies and calls once), and
  the next request opens it at the new place. A running re-index or ref
  job of this server refuses the move (`index_running`).

### Consequences

- With `GRAPHVEIN_SQLITE_PATH` set, every entry but the command-line one
  opens normally; the startup warning about other entries is gone.
- Project runs and lockers take the store location as a parameter
  (`runProjectIndex`, `linkProject`, `indexProjectMember`), so the CLI and
  the server pass the place they announced.

## Known gaps

- A `serve` process that did not make the move (another `serve`, or the
  CLI's `graphvein store move`) keeps reading the store it opened until
  the entry closes or the server restarts (on Linux and macOS the open
  file is the deleted one); its re-index is refused with `store_moved`
  rather than writing at the old place.
- Unicode normalization is not folded: a folder name typed in another
  normalization form than the one the volume stores (macOS) keeps the
  typed form, so it can still become a second entry; only letter case is
  matched. A folder that is gone is compared by its text only.
- The cache bounds are the design's constants, not yet measured on
  langflow and opencollective; the per-server bound is enforced by
  evicting from the index that adds an answer only.
- The background check opens a closed index's store for each check (no
  LRU effect) and walks every registered folder once at start (0.86 s for
  langflow's 10,233 files, serialized).
- Several indexes sharing `GRAPHVEIN_POSTGRES_URL` share one database,
  separated by repository ID and project name only. Revision 3 refuses
  two registered folders with one name, but the database cannot say which
  folder its facts came from: a name indexed into the database by a folder
  that is not registered (a `graphvein index` of a copy) is not detected.
  Recording the source folder with each run would close this; it is a
  storage change.
- A not-open entry's name in the list is the registered one until its
  index opens (revision 2 re-reads it only on open).
- The system-folder rule lists fixed folders; other operating-system
  folders (for example `/opt` or `/Library`) are not refused. The
  `os.SameFile` comparison of Windows spellings runs only where the
  system folder exists, and is tested on Windows only through CI.
- Closed at integration (2026-11-24): every request of an open project
  (code peek, change views, Settings, not only project and status
  requests) reads the manifest's files first (two small reads and hashes)
  and serves an edited manifest from then on.
- ADR 0041's "no authentication against other local processes" stands:
  the typed-path endpoints rely on the loopback peer, the session token
  and the confirm value, not on a new authentication scheme.

## Review notes (delegated review, 2026-11-25, T038)

Accepted with Revisions 1-4 after the final CI gate of 2026-11-25 (CI runs 45627357238, 45632313364 and 45638139948 (the last green on Linux, Postgres and Windows; postgres log 0 FAIL, 291 /postgres PASS): Linux, Postgres and Windows). One independent review (R1-R9) and a focused re-check (W1-W4), each fixed with tests from the review's inputs; the rounds converged. Measured: eight open indexes (langflow, opencollective-api and others) about 107 MB, the largest cached answer about 0.8 MB; no stale answer after a CLI re-index in another process, a graphvein.yaml edit or a project link pass. Reads never write to disk; a moved folder is folder_gone; system folders are refused (WS-53, with Windows prefix forms and os.SameFile); a shared Postgres store refuses entries that share a name (WS-57). Known gaps as recorded above (a shared store cannot tell which folder its facts came from; the Windows SameFile path is covered by Windows CI only).

# ADR 0093: Per-user store location

**Status:** Accepted (delegated review, 2026-11-28)

## Context

Until now `graphvein index` wrote `<folder>/.graphvein/index.db`, printed no
path, and `git status` then listed `?? .graphvein/`. `docs/config.md` said Git ignores `.graphvein/`; only the
scanner does. The folder `.graphvein/` is also the place ARCHITECTURE keeps
for committed rationale (`rationale.source: files`), so hiding it with an
ignore file would hide that too. A store holds run history, change
baselines and pins that a re-index cannot rebuild, so it belongs in a data
folder, not a cache folder.

A design round compared three variants; the developer chose variant A
with every recommended default (2026-11-27). This ADR records the store
location, the lookup order, the new file and variables and the CLI
surface. The server endpoints, the removal of `single_store` (ADR 0087)
and the UI (ADRs 0041, 0088) follow as revisions of those ADRs.

## Options considered

- **(a) Per-user data folder, one store per indexed folder** (chosen).
- (b) Inside the repository as before, plus a `.graphvein/.gitignore`
  written by Graphvein: smallest change, but hides the folder reserved for
  committed rationale and still writes into the repository.
- (c) Ask once per machine: asks a newcomer a question before they have
  seen the tool and puts a prompt into a CLI that scripts run.

Store key: the resolved folder path (chosen) rather than the repository
name or remote URL, with which two clones would share one store.

## Decision

### Where a new index lives

`<stores>/<name>-<h8>/index.db`, where `<name>` is the folder's base name
(letters, digits, `.`, `-`, `_`; anything else becomes `-`; at most 40
bytes; `folder` when nothing is left) and `<h8>` the first eight hex digits
of the SHA-256 of the resolved folder path (`workspace.Resolve`: absolute,
symbolic links resolved, the file system's own letter case). A project
store is keyed by its manifest folder the same way.

`<stores>` is, in order: `GRAPHVEIN_STORE_DIR`; `default_dir` of
`stores.yaml`; the platform's per-user data folder plus
`graphvein/stores`:

| Platform | Data folder |
|---|---|
| Windows | `%LocalAppData%` (not the roaming `%AppData%`: it can be redirected to a network share, and SQLite's WAL needs a local disk) |
| macOS | `~/Library/Application Support` |
| Linux and others | `$XDG_DATA_HOME` when absolute, else `~/.local/share` |

Graphvein creates the stores folder and each store folder owner-only
(0700) when a run is about to create the store, never on a read, and
writes `folder.txt` (the resolved folder path) into the store folder. A
store folder whose `folder.txt` names another folder is never opened for
this one (a key collision or a hand copy).

### Lookup order (`internal/storeloc.Resolve`)

For folder F, through one function used by every CLI command (index,
status, the queries, changes, openapi, mcp, serve, ingest of the current
folder, index --project and its locks):

1. `GRAPHVEIN_POSTGRES_URL` set: Postgres (team mode, unchanged).
2. `GRAPHVEIN_SQLITE_PATH` set and F is the folder named on this command
   line (`Scope.CommandLine`): that file.
3. `F/.graphvein/index.db` exists: that file (every v0.1.1 store keeps
   working where it is).
4. `stores.yaml` maps F to a store folder: `index.db` there.
5. Otherwise `<stores>/<name>-<h8>/index.db`.

When more than one of 3-5 holds an `index.db`, every command refuses:
`two indexes for F: A and B. Graphvein uses neither until one is removed
(see graphvein store list); nothing was read or written`. A downgrade to
v0.1.1, which re-creates `.graphvein/`, is the realistic way to get there.
An in-folder store is never moved implicitly.

`GRAPHVEIN_SQLITE_PATH` is kept and not deprecated; it applies to the
command-line folder only. The server resolves every other workspace entry
with `Scope{CommandLine: false}` ([ADR 0087](0087-multi-index-server.md)
revision 5), so `single_store` is gone.

### `stores.yaml`

A new file beside `workspace.yaml` (so `GRAPHVEIN_WORKSPACE_FILE` moves
both): `version: 1`, optional `default_dir`, `entries: [{path, store}]`
where `path` is the resolved indexed folder and `store` the folder holding
its `index.db`. It is read strictly (unknown keys, another version and
relative paths are errors and the file is never rewritten), and written
atomically under its own operating-system lock (`stores.yaml.lock`) like
the registry. It is a separate file because the registry's decoder is
strict: a key added to `workspace.yaml` would make a v0.1.1 binary refuse
the registry. `workspace.yaml` is unchanged. Entries list exceptions only:
recording the default place removes the entry. Changing `default_dir`
never moves a store; stores in the old default folder get an entry.

### CLI

- `graphvein index` prints, first on stdout, `Index: <path>` with a
  parenthesis saying why there: `new`, then `outside the repository` (new
  stores only), `inside the repository`, `from GRAPHVEIN_SQLITE_PATH, for
  this folder only`, `folder from GRAPHVEIN_STORE_DIR` (or `folder from
  stores.yaml`), `kept where stores.yaml says`; in team mode `Index:
  Postgres database from GRAPHVEIN_POSTGRES_URL (repository <name>)`, never
  the URL. `index --project` prints the same line for the project store
  (`project <name>`). Today's lines follow unchanged. A newly created store
  in the stores folder or a `stores.yaml` place ends with `Browse it:` and
  `Keep it elsewhere:` hints; an in-folder store ends with a note naming
  `graphvein store move`. Paths with spaces are double-quoted in the hints.
- `graphvein store where [path]`: the store path, where it is kept and why,
  its size and the last run.
- `graphvein store move <path> [<folder>] [--yes]`: moves the store to
  `<folder>/<name>-<h8>/index.db`, or without `<folder>` to the folder's
  default place; asks `Move it? [y/N]` unless `--yes`. Under the store's
  index lock (waiting `indexrun.LockWait` for a running run) it copies with
  SQLite `VACUUM INTO` (which folds in `-wal`) to `index.db.moving`, checks
  the copy (the same run ids and the same row count in every table),
  renames it into place, records the place in `stores.yaml`, and only then
  deletes the old `index.db`, `-wal`, `-shm` and `-lock` files and an
  emptied store or `.graphvein` folder. A failure before the record keeps
  the original and removes the partial copy. Refused: Postgres, a set
  `GRAPHVEIN_SQLITE_PATH`, a folder without an index, and destinations
  that are a network path on Windows, a file system root, the home folder
  itself, inside a `.git` folder, the indexed folder or inside it, a
  registered workspace folder or inside it, a file, not writable, or
  already holding an index or another folder's store folder.
- `graphvein store list`: every store Graphvein knows (the stores folders,
  `stores.yaml` entries, in-folder stores of registered folders) with
  folder, path and size; a folder that is gone is shown `(not found)`. In
  team mode a first line says the local stores are not used.
- `graphvein index` re-resolves after taking the index lock and stops if a
  move finished meanwhile, so it never creates a store at the old place.

### API for the server

`storeloc.Default()` builds the process resolver; `Resolver.Resolve(folder,
Scope)` returns a `Location` (`Folder`, `Origin`, `Path`, `Exists`,
`StoresDir`, `DirSource`, `StoresFile`, `Inside()`, `Dir()`, `Prepare()`);
`PlanMove(folder, destDir, registered)` returns a `MovePlan` (`From`, `To`,
`ToDefault`) whose two paths a confirm value can bind; `Move(ctx, plan,
MoveOptions)` refuses a plan whose folder no longer resolves to `From`;
`CheckDestination` is the refusal list; `Assign(folder, storeDir)` records
a chosen place (Add with "Change…"); `SetDefaultDir` changes the default
folder; `List(registered...)` lists stores.

## Consequences

- Nothing is written into the repository by default; `git status` stays
  clean, and the stores are owner-only on a shared machine.
- Moving or renaming a folder leaves its index behind under the old key: a
  re-index of the new place starts a fresh store, and `store list` shows
  the old one as `(not found)`.
- Scripts that read the first line of `graphvein index` output see the new
  `Index:` line (public behaviour change).
- `graphvein ingest` uses the store of the current folder through the same
  order (before: `./.graphvein/index.db`).
- Deleting a folder no longer deletes its index; `store list` finds it.

## Known gaps

- Adopting the store of a moved folder (`store move --from-id`) and
  deleting a store whose folder is gone (`store remove`) are follow-ups.
- A run that waited on the old lock during a move now stops
  (`storeloc.ErrMoved`; ADR 0087 revision 5): `graphvein index`
  resolves again once it holds the lock, and `index --project` and every
  run of `serve` check the place before and after taking it. A running `serve` that did not make the move
  keeps reading the store it opened until the entry closes or the server
  restarts; a move made from the UI closes and reopens it.
- On Windows the old files cannot be deleted while another process has
  them open; the move then reports them, and both stores are refused
  until they are deleted.
- An in-folder store of a folder never registered in the workspace is not
  in `store list`.
- Network drives mapped to a letter are detected on Windows through
  `GetDriveType` (`DRIVE_REMOTE`); elsewhere a network file system is a
  mounted folder and is not detected.
- `graphvein index` in a subfolder still makes a second, separate store
  (not decided here); the `Index:` line makes it visible.

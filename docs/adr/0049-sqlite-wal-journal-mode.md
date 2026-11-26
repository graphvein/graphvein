# ADR 0049 - SQLite WAL journal mode

**Status:** Accepted (delegated review, 2026-11-04)

## Context

ADR 0043 gave SQLite read connections a 3 s busy timeout and recorded a
known gap: the store uses rollback journaling, so a commit (or an earlier
page-cache spill) of `graphvein index` holds an EXCLUSIVE lock that blocks
every reader. A full rewrite of this repository held it for about 5 s at
commit (T016g fact 3), longer than the bound, so `graphvein serve` answers
500 `store_error` or freshness `unknown` then. ADR 0043 also recorded two
writer defects: `sqlite.Open` set `foreign_keys` and `busy_timeout` with
`ExecContext` on one pooled connection only, and passed the raw path to the
driver, which cuts it at the first `?` on macOS and Linux.

Task T016h verified the semantics. The driver is `modernc.org/sqlite
v1.60.1`, SQLite 3.53.4 transpiled to Go (not cgo, not WASM; ADR 0002 does
not apply). Sources: the SQLite documentation (www.sqlite.org: `wal.html`,
`pragma.html`, `uri.html`, `lockingv3.html`, `c3ref/wal_checkpoint_v2.html`,
`c3ref/open.html`, `c3ref/wal_autocheckpoint.html`, `releaselog/3_53_4.html`),
the SQLite 3.53.4 source and the driver source, and experiments against the
driver in scratch (the T016h task record (internal), the investigation).

- D1-D2: the driver uses SQLite's own `unix` and `win32` VFSes, without
  `SQLITE_OMIT_WAL` or `SQLITE_OMIT_SHARED_MEM`; shared memory and locks
  are real OS calls on Linux, macOS and Windows.
- D3-D4: on macOS some filesystems (AFP, SMB, NFS, dot-file locking) get
  locking without shared memory. Converting to WAL there leaves the mode
  unchanged and returns the old mode, not an error ("If the conversion to
  WAL could not be completed ... the journaling mode will be unchanged and
  the string returned from the primitive will be the prior journaling
  mode", wal.html section 3). Opening a database that is already in WAL
  mode on such a filesystem fails (wal.html section 8).
- D5: DSN parameters are applied to every new connection; the shorthand
  keys are validated before any pragma runs; a `mode=ro` connection whose
  DSN asks for WAL fails, so the reader DSN carries no journal mode.
- D6-D7: a DSN that does not start with `file:` is cut at the first `?`.
  With a `file:` URI SQLite percent-decodes the path; `file:///C:/...` is a
  Windows drive path (uri.html section 3.1).
- W1: WAL readers read a snapshot (their "end mark") and are not blocked by
  the writer, including while it spills and commits (investigation step A3: 50 MB
  uncommitted under `BEGIN EXCLUSIVE` and `BEGIN IMMEDIATE`, every read
  under 1 ms with the committed rows only).
- W2: WAL is persistent in the file (header bytes 18/19 = 2; investigation step A1).
- W3: remaining reader waits: WAL recovery after a crash, the last
  connection's close-time cleanup, and a connection in exclusive locking
  mode (wal.html section 9). Investigation step A6 found that a connection can only take
  exclusive locking mode on a WAL store while no other connection has it
  open, so Graphvein's own processes never create that case.
- W4-W5: the last connection to close checkpoints and deletes `-wal` and
  `-shm`. A `mode=ro` reader creates them when missing and, closing last,
  leaves them (investigation step A4, A5 on Linux).
- W6: "WAL does not work over a network filesystem" (wal.html section 1).
- W7: automatic PASSIVE checkpoints at 1000 pages; TRUNCATE waits through
  the busy handler for readers to leave the WAL and truncates it to zero
  bytes. wal.html section 6 says readers "might block" during a RESTART or
  TRUNCATE checkpoint, while `c3ref/wal_checkpoint_v2.html` says these
  modes do "not impede readers"; investigation step A5 measured a reader polling during a
  TRUNCATE checkpoint of a 200 MB WAL: no failure, at most 1.5 ms.
- W8: `synchronous=FULL` is the default; in WAL it syncs the WAL at each
  commit ("FULL is atomic, consistent, isolated, and durable (ACID) in WAL
  mode", pragma.html).
- W9: committed frames in a surviving `-wal` are recovered (investigation step A5:
  SIGKILL after commit, the run is read back by a read-only and a writer
  open).
- W10: one index run is one transaction, so a fresh run grows the WAL to
  about the size of the data written, and the checkpoint copies it into the
  database file.
- SQLite 3.53.4 includes the fix for the WAL-reset bug (fixed in 3.51.3,
  wal.html section 11).

## Options considered

- **(a) WAL journal mode for the writer (chosen).**
- **(b) Shorter commits with rollback journaling.** Committing in chunks
  breaks the atomic publish of a complete run (ADR 0006); a larger page
  cache only moves spills to the commit. Rejected.
- **(c) A read bound longer than the commit.** Commit time grows with the
  store and the wait cannot be cancelled (ADR 0043 fact 5). Rejected.
- **(d) Write a new file and swap it in.** Renaming over an open file fails
  on Windows; readers on Linux and macOS keep the old inode; warm runs
  would copy the whole store. Rejected.
- **(e) A retryable error during long locks.** Same missing answer. Stays a
  follow-up of ADR 0043.

## Decision

- **R1 Writer DSN.** `sqlite.Open` opens
  `file:<escaped absolute path>?_busy_timeout=10000&_foreign_keys=1&_journal_mode=WAL&_pragma=journal_size_limit(67108864)&_synchronous=FULL`,
  so every pooled connection gets every setting. The `ExecContext` pragmas
  are gone. `:memory:` opens `file::memory:?_busy_timeout=10000&_foreign_keys=1`.
  The reader DSN is unchanged.
- **R2 Paths.** One helper builds both DSNs: absolute path, forward
  slashes, a leading `/` before a drive letter, `url.URL` escaping. The
  directory is created from the same absolute path.
- **R3 Read-back.** `Open` reads `PRAGMA journal_mode`; anything other than
  `wal` logs a warning and the store continues in rollback mode (developer
  decision D-3).
- **R4 Checkpoint.** Only the writer checkpoints. SQLite's automatic
  PASSIVE checkpoint runs after the commit; the writer store's `Close` then
  runs `PRAGMA wal_checkpoint(TRUNCATE)` on a dedicated connection that
  is discarded afterwards: first without waiting, then, only if a reader is
  inside a read transaction, after an Info log line, waiting up to 1 s
  (T016h review fix R3; the 10 s writer busy timeout no longer applies to
  the checkpoint), and closes the pool. A busy or failed checkpoint is
  logged, not returned: the run is already committed. The checkpoint is
  skipped (logged) when a transaction still holds the pool's connection.
  `journal_size_limit` (64 MiB) bounds a `-wal` left behind.
- **R5 Files.** Documented in `docs/config.md` ("SQLite store files") and
  `docs/cli/serve.md`: the sidecar files, never deleting `-wal`/`-shm`
  alone or replacing `index.db` while a Graphvein process has it open, and
  network filesystems unsupported (D-4: no opt-out setting).

`synchronous` stays FULL (D-2). The read bound of ADR 0043 (3 s) is
unchanged and now covers only the W3 waits and rollback-mode fallbacks.

This closes ADR 0043's large-rewrite known gap and its two writer review
notes (per-connection pragmas, `?` in paths).

## Consequences

- `serve`, `status` and the query commands answer from the last committed
  run during any index run, whatever its size (unit-scale proof:
  `TestStatusDuringWriteLockWAL`, a 4 s write lock with spilled pages).
- `index.db-wal` and `index.db-shm` appear next to the store while it is
  open; a reader closing last leaves them (harmless, removed by the next
  writer that closes alone).
- The journal mode is a persistent change to every store a new binary
  opens for writing. Older Graphvein binaries (same SQLite major version)
  open WAL stores normally; nothing converts a store back.
- Network filesystems are unsupported for the SQLite store.
- A fresh run writes its pages twice (WAL, then checkpoint) and peaks at
  about database plus WAL on disk. The Langflow cost (T016h Part C) is
  measured separately from this change.

## Known gaps

- A local filesystem without shared memory keeps rollback mode with only a
  warning; a WAL store moved onto such a filesystem fails to open
  (wal.html section 8).
- The W3 waits remain, bounded by the 3 s read timeout.
- Windows and macOS sidecar lifecycle details come from CI logs
  (`TestWALFilesAfterClose`), not from runs on those hosts.
- `\\?\` long-path prefixes and UNC paths are not supported.
- The writer does not use `_txlock=immediate`: a second concurrent `index`
  fails on a read-to-write upgrade (`SQLITE_BUSY_SNAPSHOT`) instead of
  waiting at `BEGIN`.
- A reader that cannot write the store's directory cannot open a WAL
  store without an existing `-shm` (wal.html section 5); rollback mode read
  fine there. Measured as uid 65534 on a mode 555 directory (T016h review
  fix R1): after a writer closed alone (only `index.db`) the open fails
  with `SQLITE_READONLY_DIRECTORY` (1544); with a `-wal` but no `-shm` it
  fails with `SQLITE_CANTOPEN` (14); with both left by a reader that closed
  last it reads normally. So whether it works depends on which process
  closed last. `OpenRead` maps both failures to an error naming the
  directory and the remedy; the WAL mode stays persistent (switching back
  on close would lose concurrent reads during `index`) and `immutable=1`
  is not used (it would hide a concurrent writer's commits).

## Review notes (2026-11-04)

Delegated review, 2026-11-04. Records: the T016h task records (internal).

- **Results** (gates on the stacked head of 2026-11-04, the T016h task record (internal)
  "Gates (2026-11-04, stacked head)"): full suite with a scratch Postgres
  17.11 exit 0, 1,288 PASS lines, 98 `/postgres` PASS, only the four helper
  skips plus `TestSourceStates/{sqlite,postgres}/unreadable`. Ten corpora
  body byte-identical; the private corpus not run. Langflow timing fresh main 205.5 s
  vs branch 206.8 s (+0.6%), warm no-op 75.5 vs 76.0 s (+0.7%, parses 0);
  ingest 44-47 s vs 41-42 s; axum-ddd within +1 s. Store size after a
  clean close identical (Langflow 935,981,056 B, axum-ddd 3,317,760 B);
  branch stores are WAL (header bytes 18/19 = 2/2), no `-wal`/`-shm` left
  after `index` closes, while `serve` holds a store `-wal` 0 B and `-shm`
  32,768 B. Schema endpoint latency on WAL stores all under target. CI:
  [ci windows] batch run on dd42753 green.
- **Revisions** (T016h review fixes): R1 a reader on a store whose
  directory it cannot write was measured as uid 65534 (1544
  `SQLITE_READONLY_DIRECTORY`; 14 `SQLITE_CANTOPEN` with `-wal` and no
  `-shm`) and `OpenRead` maps both to an error naming the directory and
  the remedy; WAL stays persistent and `immutable=1` is not used
  (decision). R2 `TestWALFilesAfterClose` fails on any log
  line from `Close` and asserts a 0-byte `-wal` (mutation-checked). R3 the
  close checkpoint first tries without waiting, logs Info only when it may
  block and waits at most 1 s (`checkpointBusyTimeout`) on a discarded
  connection. R4 `docs/config.md` section order. R5 the untested fallback
  path recorded as a follow-up.
- **Known gaps** (above, plus the summary's follow-ups): rollback mode
  with only a warning on a filesystem without shared memory, and that
  fallback path is untested; W3 waits bounded by the 3 s read timeout;
  Windows and macOS sidecar details from CI logs only; no `\\?\` or UNC
  paths; no `_txlock=immediate` (D8; a second concurrent `index` fails
  on `SQLITE_BUSY_SNAPSHOT`); the read-only-directory case depends on
  which process closed last; no distinct retryable error for a locked
  store (ADR 0043); no opt-out for network filesystems.

# ADR 0052 - Re-index endpoint and the shared index lock

**Status:** Accepted (delegated review, 2026-11-07)

## Context

UI decision U1 (the T016 task record (internal)) puts a Re-index button
in the top bar; only "Check" ships today. Re-index is the first
state-changing HTTP endpoint. ADR 0041 made `graphvein serve` read-only
(GET/HEAD only, the store opened read-only) and required
`http.CrossOriginProtection` before any mutating endpoint ships. ADR 0049
(WAL) lets SQLite readers answer from the last committed run while a
writer works; Postgres readers always do (one run is one transaction).

Nothing serializes index runs today. Two concurrent `graphvein index`
runs both scan and parse; on SQLite the second then fails on a
read-to-write upgrade (`SQLITE_BUSY_SNAPSHOT`, ADR 0049 known gap), on
Postgres it waits and then replaces the first. Fact cache rows are written
outside the run transaction. A server that can start runs makes concurrent
runs likely (a click while a terminal runs `graphvein index`), so the lock
must be shared by the CLI and the server, on both stores, across
processes, and must not outlive a crashed holder.

Constraints: no cgo outside ADR 0002; only `internal/egress` opens network
connections; all persistence behind the `store` package; Windows, macOS
and Linux; the boundary test rejects `syscall` and `golang.org/x/sys`
imports outside a reviewed per-file list.

The design and its verified facts (F1-F7) are in
the T016j task record (internal).

## Options considered

### 1. Where the run executes

- **(a) In the server process, with a writer store opened per run
  (chosen).** The server keeps its read-only store for requests; a run
  opens `sqlite.Open`/`postgres.Open` with the CLI's locator, runs the
  code shared with the CLI (`internal/indexrun`) and closes the writer.
  Progress comes from callbacks. Cost: parse workers and their memory
  live in the serve process during a run.
- (b) A `graphvein index` subprocess. Needs `os/exec` in the server
  (subprocess allow-list), progress by parsing stdout, and the child's
  environment and binary path. Rejected: fragile progress, a new
  subprocess boundary.
- (c) Serve with a writer store at all times. Runs migrations at serve
  start and gives every request handler a store that can write. Rejected:
  widens ADR 0041's read-only guarantee for every request, not just the
  run.

### 2. Lock primitive

SQLite store:

- **(a) A separate SQLite lock file held with `BEGIN IMMEDIATE` (chosen).**
  `<store path>-lock`, never written. SQLite's `unix` and `win32` VFSes
  take real OS advisory locks (ADR 0049 D1-D2), the OS drops them when the
  process exits, and no new import is needed. Measured (F2): cross-process
  and in-process refusal, release on `kill -9`.
- (b) OS file locks (`flock`, `LockFileEx`) through `syscall` and
  `golang.org/x/sys/windows`. Same semantics, but per-OS code, new
  boundary allow-list entries and a direct `x/sys` dependency. Rejected as
  more surface for the same result.
- (c) A lock row or PID file with liveness checks. Stale-lock detection
  depends on PID reuse and clocks, and a row in the store needs a schema
  change. Rejected.
- (d) The store's own write lock. Covers only the ingest transaction, not
  the minutes of scan and parse before it, and fails late. Rejected.

Postgres store:

- **(a) A session-level advisory lock on a dedicated connection
  (chosen).** `pg_try_advisory_lock(714839211, k)`,
  `k = int32(FNV-1a-32(current_schema() || 0x00 || repository))`.
  Released when the session ends; observable through `pg_locks` without
  taking it; works in read-only sessions (F3). No schema change.
- (b) A transaction-level advisory lock. It would need a transaction open
  through scan and parse. Rejected.
- (c) A lock table with a heartbeat. Schema change and timing rules for
  staleness. Rejected.

### 3. Progress reporting

- **(a) Polling a cheap `GET /api/v0/index/run` (chosen):** 1 s while a
  lock is held, otherwise with the 30 s status poll. Fits the existing
  request model, timeouts and shutdown accounting.
- (b) Server-sent events. A long-lived response conflicts with the 2 min
  `WriteTimeout` and with `Serve` waiting for every handler at shutdown.
  Rejected for v1.

### 4. Cross-site protection

- **(a) `http.CrossOriginProtection` plus the existing Host,
  `Sec-Fetch-Site` and `Origin` checks, plus a required
  `Content-Type: application/json` (chosen).** CrossOriginProtection
  rejects same-site and cross-site fetch metadata and a mismatched
  `Origin` (F1); the content type makes any cross-origin request a
  preflighted one, which the server never grants.
- (b) A CSRF token. Adds state and a bootstrap request; the Fetch Metadata
  checks already cover current browsers. Rejected.

## Decision

1. `graphvein serve` gains `POST /api/v0/index/run` (start; 202, 409
   `index_locked`, 409 `reindex_unsupported`, 400, 415, 403, 503
   `index_lock_unavailable`) and `GET /api/v0/index/run` (lock state and
   the latest job of this server process: state, phase, progress, the run
   it replaces, the committed run or the error). The method rule becomes
   per path; only this path accepts POST. Schemas and the state table are
   in the task file and, when implemented, `docs/cli/serve.md`.
2. The run executes in the server process with a writer store opened only
   for the run (option 1a), through `internal/indexrun`, which
   `graphvein index` uses too. Server runs use the CLI defaults and the
   built-in indexer only.
3. `internal/store` gains `IndexLocker`/`IndexLease`/`LockState`/
   `ErrIndexLocked`, implemented by `internal/store/sqlite` (option 2a,
   SQLite) and `internal/store/postgres` (option 2a, Postgres).
   `Transaction` gains `Run()` (the committed run's metadata). The lock is
   held from before the scan until after the writer store closes.
   `graphvein index` (built-in and `--indexer`) and the server acquire it
   with a 2 s wait; the CLI exits 1 when it is held. Before `Commit` the
   lease is checked; a lost lease fails and rolls back the run.
4. Progress by polling (option 3a). A fraction is reported only where both
   numbers come from the run (cache lookups and parsing in files, ingest in
   records); every other phase has `progress: null`.
5. Server shutdown cancels a running job; `Serve` returns only after the
   job rolled back, closed the writer and released the lock.
6. The UI shows "indexing" only while the server reports a held lock and
   "fresh" for a new run only after the job reported it committed and a
   status answer names it (task file, "Governing UI principle").

## Consequences

- The first mutating endpoint; ADR 0041's "GET and HEAD only" becomes
  "GET and HEAD only, except `/api/v0/index/run`".
- Concurrent runs are refused up front, before any scan, on both stores;
  ADR 0049's `SQLITE_BUSY_SNAPSHOT` gap no longer happens between two
  current binaries. Fact cache writes are serialized too.
- A new sidecar file `index.db-lock` next to the SQLite store
  (`docs/config.md` must list it; never delete it while Graphvein runs).
- A serve process's memory and CPU rise to `graphvein index` levels during
  a run (measured in T016j gate G6).
- The CLI and the server share one run implementation, so a server run
  writes exactly what a CLI run with default flags writes.
- The `internal` error code gets its first use (a recovered panic in the
  job goroutine).

## Known gaps

- Older Graphvein binaries do not take the lock and can still run
  concurrently with a new one.
- Postgres: a holder whose host disappears without closing its socket
  keeps the lock until TCP keepalive ends the session (F3: server
  `tcp_keepalives_idle` 7200 s here). A key collision between two
  repositories in one schema refuses a run that could have run, never the
  reverse.
- Postgres: the lease is checked before `Commit`; a session lost after the
  check and before the commit finishes is not detected.
- SQLite: deleting `index.db-lock` while it is held lets a second holder
  create a new file and lock it (Linux and macOS; Windows refuses to delete
  an open file). The holder's pre-commit check notices a replaced file, so
  only a deletion after that check goes unnoticed.
- Windows releases a dead process's file locks "depending upon available
  system resources" (Microsoft `LockFileEx` documentation); covered by the
  CI kill test, not measured on a Windows host.
- Lock state probes on SQLite take the lock for about a millisecond;
  acquirers wait 2 s, so they are not refused, but a third-party tool that
  takes the lock without waiting could be.
- Jobs live in memory: a restarted server forgets its last job (the UI
  reports the outcome as unknown) and the server never sees a CLI run's
  failure, only its lock.
- The server re-indexes with default flags; a repository indexed with
  other `--max-file-size`, `--timeout` or `--workers` values changes when
  re-indexed from the UI (the run does not record its options).
- No holder details (process, host, start time) for a lock held
  elsewhere.
- No authentication: any local process can start a run, as it could run
  `graphvein index` (ADR 0041).

## Implementation notes (server, CLI and storage part)

Recorded by the implementation of the decision-independent part (the UI,
D1-D7, is not implemented yet). Where it differs from the text above, this
section is what the code does.

- **Phase `starting`.** A job reports `starting` while its writer store
  opens (and migrates), before `scanning`. Without it the POST answer would
  have claimed `scanning` before the scan began.
- **SQLite `State` creates the lock file** when it is missing (the same
  empty file `Acquire` creates), so a directory where no lock can be taken
  reports an error (`unknown`) instead of `free`.
- **SQLite `Check`** compares the lock file with the one the lease locked
  (`os.SameFile`): a lock file deleted or replaced while held fails the
  run before commit with `index_lock_lost`. The known gap below therefore
  only remains for a deletion after the check.
- **`postgres.LockKey`** is exported so tests and operators can find the
  lock in `pg_locks` (`classid` 714839211, `objid` = the key as unsigned).
  `current_schema()` NULL (a `search_path` naming no existing schema) is an
  error, never an empty schema.
- **`indexrun.WithStore`** opens the writer store with the lease check and
  closes it under the lock; `graphvein index --indexer` uses it, so the
  server never imports the external-indexer path into its run.
- **Status during a run.** `GET /api/v0/status` stays unchanged: during a
  run it reports the previous run and that run's freshness, which may be
  `fresh`; it never names the run in flight (verified on both backends with
  the run held inside its write transaction, and after failed runs). The UI
  rule "never Fresh while the lock is held" must combine status with
  `GET /api/v0/index/run`.
- **Unknown lock test.** The "unwritable lock directory" case is tested
  with a lock path whose parent is a regular file, which fails for root and
  on every system, instead of a chmod test that would be skipped as root.

## Review fixes (2026-11-07)

- **`graphvein ingest` takes the index lock** like `graphvein index`
  (same 2 s wait, same message, exit 1 when held), and its writer store
  checks the lease before the commit (`indexrun.WithStore`). On Postgres
  it reads the stream header first: the lock key needs the repository.
  Before this, an ingest could write while another run held the lock.
- **Outcome before release.** A server job records its terminal state
  (`state`, `error`, `ended_at`) before it releases the lease, and
  `GET /api/v0/index/run` reads the job and probes the lock under the same
  mutex, so the document never pairs `lock.state: free` with a job that
  has not ended. A document may show an ended job with `held_here` for the
  moment of the release. The UI also treats "free lock, job not ended" as
  "outcome still being read" (fast polling, never idle, never Fresh), for
  any server that answers so.
- **`State` during this locker's own `Acquire`** (both backends) waits for
  that `Acquire` to end, then answers (`held_here` when it succeeded):
  between the lock being taken and the lease being recorded, a probe used
  to report the process's own lock as `held_elsewhere`. The wait is at most
  the acquirer's wait (2 s).


## Review notes (2026-11-07, delegated review)

- Results: CI batch run 42373834320 on `wt/s26-ui` 70a45e3 green (Windows and the postgres job: 0 FAIL, 121 distinct `/postgres` PASS, only the four helper skips); local full check with Postgres 17 (120 `/postgres` PASS); ten corpora and the self-index byte-identical to main; Langflow fresh -1.8%, warm -2.2% (three interleaved pairs, warm parses 0), store size identical; Playwright on Langflow and a kftray copy (never "Fresh" before the new run, fixed-width slot, switch notice clear of the Flow caption row), 0 console errors, 0 CSP violations.
- UI: developer decisions D1-D8 (variant D: the Re-index button is the run's control in a fixed-width slot; the badge never changes because a run starts), after round-2 mockups without layout shift.
- Revisions from review: the job records its outcome before releasing the lease and the run document reads job and lock under one mutex (a failure could look idle for up to 30 s); `graphvein ingest` takes the index lock and reads its stream once for the lock key (no seek; the locked repository equals the written one); a held lock outranks an old POST refusal; a lock-state query waits for the locker's own acquire.
- Known gaps (accepted): a Postgres holder whose host vanishes keeps the lock up to the TCP keepalive (about 2 h); server re-index uses default flags (runs do not record options); the CLI writes its temp snapshot under `<root>/.graphvein` even when the store is elsewhere; the `pg_locks` probe runs under the run mutex; a POST acquire can delay GETs up to 2 s; a CLI-started run appears in the UI at the next 30 s poll; `graphvein status` does not show a held lock.

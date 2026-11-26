# ADR 0043 - SQLite read busy timeout

**Status:** Accepted (delegated review, 2026-11-03)

## Context

`graphvein serve` reads the index through `sqlite.OpenRead`, which set no
busy timeout. A status or API section request that met a write lock held by
a concurrent `graphvein index` failed at once with SQLITE_BUSY: 500
`store_error`, or freshness `unknown`. ADR 0041 records this as a known gap
of re-indexing while `serve` runs. `graphvein status`, `query`, `callers`
and `impact` share `OpenRead` and failed the same way.

Verified semantics (2026-11-03, task T016g). The driver is
`modernc.org/sqlite v1.60.1` (pure Go, SQLite 3.53.4), not the ADR 0002 cgo
package. www.sqlite.org could not be fetched (proxy 403); the facts come from
the driver's source and from experiments against it in scratch:

1. The store uses rollback journaling (`journal_mode` is `delete`); nothing
   sets WAL. Readers block on writers.
2. An open write transaction holds RESERVED, which readers pass, until it
   modifies the database file; from then until it ends it holds EXCLUSIVE,
   which blocks every reader. The file is modified at commit, or earlier
   when the page cache spills. `ingest` keeps one transaction from
   `BeginRun` to `Commit`.
3. Self-index of this repository on Linux: an unchanged re-index showed no
   BUSY; a full rewrite (about 45 MB of new rows) showed short BUSY bursts
   (10-345 ms) on cache spills and one 5.0 s block at commit.
4. `database/sql` may close and reopen pooled connections. A
   `PRAGMA busy_timeout` sent with `ExecContext` configures only the
   connection that ran it (after a recycle it read 0). The driver applies
   DSN parameters (`_pragma=busy_timeout(N)`, `_busy_timeout=N`) to every new
   connection; with a `file:` URI SQLite keeps `mode=ro`.
5. The busy wait ignores the request context: the driver's cancellation
   calls `sqlite3_interrupt`, which does not stop the busy handler's sleep,
   and the driver exposes no busy-handler hook.
6. Postgres readers do not block on an uncommitted run (MVCC).

## Options considered

- **(a) Busy timeout on read connections, set in the DSN (chosen).**
  Short lock windows are waited out; a longer lock still fails as before.
  Setting it per connection with `ExecContext` was rejected (fact 4).
- **(b) WAL journal mode.** Removes reader blocking on commit, so it would
  also cover the large-rewrite commit of fact 3. Rejected here: it is a
  persistent change to the database file, adds `-wal` and `-shm` sidecar
  files, has read-only and network-filesystem constraints, and needs its own
  ADR and corpus measurements. It is the follow-up for large rewrites.
- **(c) One read transaction per request.** Every read of a request would
  see one snapshot, but the long SHARED lock would make the writer's commit
  wait, and while the writer holds PENDING it blocks every other reader.
  ADR 0042 rejected new store methods for this.
- **(d) A retry loop in the server.** Same outcome as a timeout with more
  code, and it would only cover the server, not the CLI read commands.

## Decision

`sqlite.OpenRead` opens `file:<abs path>?mode=ro&_pragma=busy_timeout(3000)`,
so every pooled read connection waits up to **3 s** for a lock
(`readBusyTimeout`; `openRead` and `readDSN` take the bound so tests can use
a short one).

Why 3 s: it covers incremental re-index commit windows (fact 3); it stays
below serve's 5 s shutdown grace, because the wait cannot be cancelled
(fact 5); it is far below the web client's 15 s request timeout and the
server's 2 min `WriteTimeout`.

When the bound is exceeded nothing new is reported: status answers 500
`store_error` (runs read) or freshness `unknown`, an API section 500
`store_error`, exactly as before. Never `fresh`, never an empty or partial
answer, no new error code. There is no single snapshot per request: a read
that waited returns the newly committed state, and the API section's
runs-before/after check sees the new run, repeats once, then answers 503
`index_changed` (ADR 0042 section 6).

`graphvein status`, `query`, `callers` and `impact` share `OpenRead`, so
they also wait up to 3 s instead of failing at once. This is intended.

This narrows the re-index known gap of ADR 0041: a request overlapping a
commit shorter than 3 s now waits and answers correctly.

## Consequences

- Re-indexing next to `serve` no longer produces `store_error` or
  `unknown` for commits shorter than 3 s.
- A request blocked by a lock delays serve's shutdown: each store read
  waits up to 3 s on its own, so one blocked read fits the 5 s shutdown
  grace, but a status request whose freshness and runs reads are both
  blocked can outlast it.
- A status request under a lock that never clears now takes up to 3 s per
  read before answering 500 (freshness read and runs read: about 6 s).
- Postgres is unchanged.

## Known gaps

- A commit that holds EXCLUSIVE longer than 3 s (large rewrites, fact 3)
  still yields `store_error` or `unknown`. WAL (option b) is the follow-up.
- The wait cannot be cancelled by the request context or by Ctrl+C on
  `serve`; it is only bounded.
- The writer connection (`sqlite.Open`) sets `busy_timeout` and
  `foreign_keys` with `ExecContext`, per connection (fact 4); a recycled
  writer connection loses them. Not changed here.
- A locked store is still reported as 500 `store_error`, not as a distinct
  retryable error.

## Review notes (delegated review, 2026-11-03)

- Independent review: no blocker or major findings; no path reports `fresh` because of the wait (multi-query reads run in one deferred read transaction; status reads freshness before runs, so a waited read can only yield `stale` beside a newer run). Driver semantics confirmed in modernc.org/sqlite v1.60.1 (`applyQueryParams` on every new connection; `file:` URI with `SQLITE_OPEN_URI`, so `mode=ro` holds; `url.URL` escapes `?`, `#`, `%` and spaces). Windows paths checked by construction only (`file:///D:/...`), not on a Windows host beyond CI.
- Revision after review: the timeout comment and docs/cli/serve.md say "per read"; a status request makes two reads and can take about 6 s.
- Known gaps: WAL journal mode (large rewrites still block readers for longer than the bound); `sqlite.Open` sets its pragmas per connection, so a recycled writer connection would lose them, and it passes a raw path that the driver truncates at `?` on macOS/Linux; a distinct retryable error for a locked store; the pre-existing freshness/runs read pair can see different runs (edit-and-revert window, slightly wider with the wait).

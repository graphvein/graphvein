package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/store"
)

// indexLockClass is the first key of the two-key advisory lock that
// serializes index runs. The migration lock uses the one-key space
// (714839210); the two key spaces never overlap (ADR 0052, F3).
const indexLockClass = 714839211

// projectLockClass is the first key of the project lock (ADR 0075): the
// same two-key scheme as the index lock, in its own class, so a project
// and a repository with the same name never block each other.
const projectLockClass = 714839212

// acquireRetry is how often Acquire retries pg_try_advisory_lock while it
// waits for the current holder.
const acquireRetry = 100 * time.Millisecond

// IndexLocker returns the index lock for repository in the Postgres
// database at url. A lease is a session-level advisory lock
// pg_try_advisory_lock(714839211, k) on a dedicated connection that stays
// idle for the lease, where k hashes current_schema() and the repository:
// advisory locks are per database, not per schema, so two schemas with the
// same repository name must not block each other. The lock is released
// when the session ends, so a holder that exits, however it exits, leaves
// no stale lock. Connections go through egress.OpenPostgres like the
// store's own.
func IndexLocker(url, repository string) store.IndexLocker {
	return &indexLocker{url: url, repository: repository, class: indexLockClass, what: "index lock"}
}

// ProjectLocker returns the project lock for project in the Postgres
// database at url: the project link pass holds it while it reads every
// member and replaces the project's links (ADR 0075). It works like
// IndexLocker, with class 714839212 and k = LockKey(schema, project), so
// two schemas, or a project and a repository of the same name, never
// block each other. Member index runs keep their own index locks.
func ProjectLocker(url, project string) store.IndexLocker {
	return &indexLocker{url: url, repository: project, class: projectLockClass, what: "project lock"}
}

type indexLocker struct {
	url string
	// repository is the lock's subject: a repository for the index lock,
	// a project for the project lock.
	repository string
	// class is the advisory lock's first key and what names the lock in
	// errors.
	class int32
	what  string
	mu    sync.Mutex
	// acquiring is held for the whole of an Acquire, and State waits for
	// it: between the moment the lock is granted and the lease being
	// recorded, pg_locks would show this process's own lock as another
	// holder's (T016j review C).
	acquiring sync.Mutex
	lease     *indexLease
	// probe is a one-connection pool for State, opened on first use.
	probe *sql.DB
}

type indexLease struct {
	locker *indexLocker
	db     *sql.DB
	conn   *sql.Conn
	key    int32
	once   sync.Once
	err    error
}

// LockKey is the second advisory lock key: FNV-1a-32 of the schema, a NUL
// byte and the repository, as a signed 32-bit integer. A collision between
// two repositories refuses a run that could have run, never the reverse.
// It is exported so tests and operators can find a lock in pg_locks
// (classid 714839211, objid = the key's 32 bits as unsigned).
func LockKey(schema, repository string) int32 {
	h := fnv.New32a()
	h.Write([]byte(schema))
	h.Write([]byte{0})
	h.Write([]byte(repository))
	return int32(h.Sum32())
}

// sessionKey computes the lock key on a connection, from the schema that
// connection's store reads and writes.
func (l *indexLocker) sessionKey(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (int32, error) {
	var schema sql.NullString
	if err := q.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		return 0, fmt.Errorf("reading the Postgres schema for the %s: %w", l.what, err)
	}
	if !schema.Valid {
		return 0, fmt.Errorf("reading the Postgres schema for the %s: the search_path names no existing schema", l.what)
	}
	return LockKey(schema.String, l.repository), nil
}

// heldQuery finds a granted index lock in this database through pg_locks,
// which every role may read; it never takes the lock. objid is an oid, the
// key's 32 bits read as unsigned, so the key is passed as that number.
const heldQuery = `SELECT EXISTS (SELECT 1 FROM pg_locks
	WHERE locktype = 'advisory' AND classid = $1::bigint::oid AND objid = $2::bigint::oid
	AND objsubid = 2 AND granted
	AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
	AND ($3::boolean = false OR pid = pg_backend_pid()))`

// Acquire takes the lock on a new dedicated connection, retrying every
// 100 ms until wait has passed.
func (l *indexLocker) Acquire(ctx context.Context, wait time.Duration) (store.IndexLease, error) {
	l.acquiring.Lock()
	defer l.acquiring.Unlock()
	l.mu.Lock()
	held := l.lease != nil
	l.mu.Unlock()
	if held {
		return nil, fmt.Errorf("%w (Postgres advisory lock, by this process)", store.ErrIndexLocked)
	}
	db, err := egress.OpenPostgres(l.url, nil)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("connecting for the %s: %w", l.what, err)
	}
	fail := func(err error) (store.IndexLease, error) {
		conn.Close()
		db.Close()
		return nil, err
	}
	key, err := l.sessionKey(ctx, conn)
	if err != nil {
		return fail(err)
	}
	deadline := time.Now().Add(wait)
	for {
		var got bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1::int4, $2::int4)", l.class, key).Scan(&got); err != nil {
			return fail(fmt.Errorf("taking the %s: %w", l.what, err))
		}
		if got {
			break
		}
		if !time.Now().Before(deadline) {
			return fail(fmt.Errorf("%w (Postgres advisory lock)", store.ErrIndexLocked))
		}
		timer := time.NewTimer(min(acquireRetry, time.Until(deadline)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail(ctx.Err())
		case <-timer.C:
		}
	}
	lease := &indexLease{locker: l, db: db, conn: conn, key: key}
	l.mu.Lock()
	l.lease = lease
	l.mu.Unlock()
	return lease, nil
}

// State reports LockHeldHere while this locker holds a lease, otherwise
// reads pg_locks on the probe connection.
// waitForAcquire returns once no Acquire of this locker is in progress.
func (l *indexLocker) waitForAcquire() {
	l.acquiring.Lock()
	defer l.acquiring.Unlock()
}

func (l *indexLocker) State(ctx context.Context) (store.LockState, error) {
	// An Acquire of this locker in progress ends first (it waits at most
	// its wait), so the answer is held_here when it succeeded.
	l.waitForAcquire()
	l.mu.Lock()
	if l.lease != nil {
		l.mu.Unlock()
		return store.LockHeldHere, nil
	}
	if l.probe == nil {
		db, err := egress.OpenPostgres(l.url, nil)
		if err != nil {
			l.mu.Unlock()
			return "", err
		}
		db.SetMaxOpenConns(1)
		l.probe = db
	}
	probe := l.probe
	l.mu.Unlock()
	key, err := l.sessionKey(ctx, probe)
	if err != nil {
		return "", err
	}
	var held bool
	if err := probe.QueryRowContext(ctx, heldQuery, int64(l.class), int64(uint32(key)), false).Scan(&held); err != nil {
		return "", fmt.Errorf("reading the %s state: %w", l.what, err)
	}
	if held {
		return store.LockHeldElsewhere, nil
	}
	return store.LockFree, nil
}

// Close releases a lease the locker still holds and closes the probe pool.
func (l *indexLocker) Close() error {
	l.mu.Lock()
	lease, probe := l.lease, l.probe
	l.probe = nil
	l.mu.Unlock()
	var err error
	if lease != nil {
		err = lease.Release()
	}
	if probe != nil {
		err = errors.Join(err, probe.Close())
	}
	return err
}

// Check pings the lease connection and confirms in pg_locks that its own
// backend still holds the lock. A terminated session fails the ping.
func (l *indexLease) Check(ctx context.Context) error {
	if err := l.conn.PingContext(ctx); err != nil {
		return fmt.Errorf("%s session lost: %w", l.locker.what, err)
	}
	var held bool
	if err := l.conn.QueryRowContext(ctx, heldQuery, int64(l.locker.class), int64(uint32(l.key)), true).Scan(&held); err != nil {
		return fmt.Errorf("%s session lost: %w", l.locker.what, err)
	}
	if !held {
		return fmt.Errorf("%s no longer held by this session", l.locker.what)
	}
	return nil
}

// Release unlocks and closes the session. Closing the pool ends the
// session, which releases the lock even when the unlock failed (for
// example because the session was already gone), so only close errors are
// reported.
func (l *indexLease) Release() error {
	l.once.Do(func() {
		_, _ = l.conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1::int4, $2::int4)", l.locker.class, l.key)
		connErr := l.conn.Close()
		dbErr := l.db.Close()
		l.locker.mu.Lock()
		if l.locker.lease == l {
			l.locker.lease = nil
		}
		l.locker.mu.Unlock()
		if err := errors.Join(connErr, dbErr); err != nil {
			l.err = fmt.Errorf("releasing the %s: %w", l.locker.what, err)
		}
	})
	return l.err
}

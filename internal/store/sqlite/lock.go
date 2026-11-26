package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/store"
)

// LockSuffix names the index lock file next to a store: index.db-lock for
// the default store. The file is an empty SQLite database that is never
// written; its only use is the lock SQLite's VFS takes on it (ADR 0052).
const LockSuffix = "-lock"

// LockPath returns the index lock file for the store at storePath.
func LockPath(storePath string) string {
	if storePath == "" {
		storePath = DefaultPath
	}
	return storePath + LockSuffix
}

// IndexLocker returns the index lock of the SQLite store at storePath. The
// lock covers the whole store file, every repository in it, because one
// SQLite file has one writer anyway.
//
// A lease is a connection to <store>-lock that has run BEGIN IMMEDIATE and
// keeps that transaction open: SQLite's unix and win32 VFSes hold a real OS
// lock (RESERVED) for it, which a second connection, in this process or
// another, cannot take, and which the operating system drops when the
// holding process exits, however it exits. Nothing is ever written, so the
// file stays empty; a -journal left by a crash is removed by the next
// opener.
func IndexLocker(storePath string) store.IndexLocker {
	return &indexLocker{path: LockPath(storePath)}
}

// ProjectLocker returns the project lock of the SQLite store at storePath
// (ADR 0075). One SQLite file has one writer, so the project lock is the
// store's index lock: the project link pass and every member's index run
// exclude each other. project names the lock's subject only; it does not
// change which lock is taken.
func ProjectLocker(storePath, project string) store.IndexLocker {
	_ = project
	return IndexLocker(storePath)
}

type indexLocker struct {
	path string
	mu   sync.Mutex
	// acquiring is held for the whole of an Acquire, and State waits for
	// it: between the moment the lock is taken and the lease being
	// recorded, a probe would see this process's own lock as another
	// holder's (T016j review C).
	acquiring sync.Mutex
	// lease is the lease this locker holds, nil when it holds none.
	lease *indexLease
}

type indexLease struct {
	locker *indexLocker
	db     *sql.DB
	conn   *sql.Conn
	// file identifies the lock file the lease locked, so Check notices a
	// file that was deleted or replaced while held.
	file os.FileInfo
	once sync.Once
	err  error
}

// lockDSN opens the lock file read-write, creating it if needed, with the
// given busy timeout. The file stays in rollback journal mode: WAL would
// move the write lock into shared memory.
func lockDSN(p string, busy time.Duration) (string, error) {
	return fileURI(p, fmt.Sprintf("_pragma=busy_timeout(%d)", busy.Milliseconds()))
}

// begin opens a dedicated connection to the lock file and runs BEGIN
// IMMEDIATE on it, waiting at most busy for another holder. It returns
// store.ErrIndexLocked (wrapped) when SQLite reports SQLITE_BUSY.
func (l *indexLocker) begin(ctx context.Context, busy time.Duration) (*sql.DB, *sql.Conn, error) {
	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return nil, nil, fmt.Errorf("creating index lock directory: %w", err)
	}
	dsn, err := lockDSN(l.path, busy)
	if err != nil {
		return nil, nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("opening index lock %s: %w", l.path, err)
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("opening index lock %s: %w", l.path, err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		conn.Close()
		db.Close()
		var coded sqliteCoder
		if errors.As(err, &coded) && coded.Code()&0xff == sqliteBusy {
			return nil, nil, fmt.Errorf("%w (%s)", store.ErrIndexLocked, l.path)
		}
		return nil, nil, fmt.Errorf("taking index lock %s: %w", l.path, err)
	}
	return db, conn, nil
}

// Acquire takes the lock, letting SQLite's busy handler wait up to wait
// for the current holder. The driver cannot interrupt that wait with ctx,
// so callers keep wait short (ADR 0052: 2 s).
func (l *indexLocker) Acquire(ctx context.Context, wait time.Duration) (store.IndexLease, error) {
	l.acquiring.Lock()
	defer l.acquiring.Unlock()
	l.mu.Lock()
	held := l.lease != nil
	l.mu.Unlock()
	if held {
		return nil, fmt.Errorf("%w (%s, by this process)", store.ErrIndexLocked, l.path)
	}
	db, conn, err := l.begin(ctx, wait)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(l.path)
	if err != nil {
		conn.ExecContext(context.Background(), "ROLLBACK")
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("reading index lock %s: %w", l.path, err)
	}
	lease := &indexLease{locker: l, db: db, conn: conn, file: info}
	l.mu.Lock()
	l.lease = lease
	l.mu.Unlock()
	return lease, nil
}

// State reports LockHeldHere while this locker holds a lease. Otherwise it
// probes: BEGIN IMMEDIATE without waiting, then ROLLBACK. SQLITE_BUSY means
// another holder has it; any other failure leaves the state unknown. The
// probe holds the lock for about a millisecond, which an acquirer's wait
// rides out. When the lock file's folder exists, it creates the empty
// lock file if it is missing, so a directory where no lock can be taken
// reports an error instead of "free". When the folder itself is missing
// no process can hold the lock (a holder keeps its file open), so the
// answer is "free" and nothing is created: a read never recreates a
// folder that was moved or deleted (T038 review).
func (l *indexLocker) State(ctx context.Context) (store.LockState, error) {
	// An Acquire of this locker in progress ends first (it waits at most
	// its wait), so the answer is held_here when it succeeded.
	l.waitForAcquire()

	l.mu.Lock()
	held := l.lease != nil
	l.mu.Unlock()
	if held {
		return store.LockHeldHere, nil
	}
	if _, err := os.Stat(filepath.Dir(l.path)); errors.Is(err, fs.ErrNotExist) {
		return store.LockFree, nil
	}
	db, conn, err := l.begin(ctx, 0)
	if errors.Is(err, store.ErrIndexLocked) {
		return store.LockHeldElsewhere, nil
	}
	if err != nil {
		return "", err
	}
	defer db.Close()
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		return "", fmt.Errorf("releasing index lock probe %s: %w", l.path, err)
	}
	return store.LockFree, nil
}

// waitForAcquire returns once no Acquire of this locker is in progress.
func (l *indexLocker) waitForAcquire() {
	l.acquiring.Lock()
	defer l.acquiring.Unlock()
}

// Close releases a lease the locker still holds; probes keep nothing open.
func (l *indexLocker) Close() error {
	l.mu.Lock()
	lease := l.lease
	l.mu.Unlock()
	if lease != nil {
		return lease.Release()
	}
	return nil
}

// Check fails when the lock file the lease locked is no longer the file at
// the lock path: someone deleted or replaced it, so a second holder could
// lock a new file (ADR 0052 known gap). An open lease cannot otherwise be
// lost while this process runs.
func (l *indexLease) Check(context.Context) error {
	info, err := os.Stat(l.locker.path)
	if err != nil {
		return fmt.Errorf("index lock %s is gone: %w", l.locker.path, err)
	}
	if !os.SameFile(info, l.file) {
		return fmt.Errorf("index lock %s was replaced while held", l.locker.path)
	}
	return nil
}

// Release ends the lock transaction and closes its connection.
func (l *indexLease) Release() error {
	l.once.Do(func() {
		_, rollbackErr := l.conn.ExecContext(context.Background(), "ROLLBACK")
		connErr := l.conn.Close()
		dbErr := l.db.Close()
		l.locker.mu.Lock()
		if l.locker.lease == l {
			l.locker.lease = nil
		}
		l.locker.mu.Unlock()
		if err := errors.Join(rollbackErr, connErr, dbErr); err != nil {
			l.err = fmt.Errorf("releasing index lock %s: %w", l.locker.path, err)
		}
	})
	return l.err
}

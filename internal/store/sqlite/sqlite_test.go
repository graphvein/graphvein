package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
)

// storeWithRun creates a migrated store at a temporary path with one
// committed run for repository "fixture" and returns the path. The store is
// in WAL mode, as Open leaves it.
func storeWithRun(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	storeWithRunAt(t, path)
	return path
}

// rollbackStoreWithRun is storeWithRun switched back to rollback
// journaling through a raw handle: the journal mode of a store on a
// filesystem without shared memory (ADR 0049). In WAL mode no lock a test
// handle can take blocks readers while another connection has the file
// open, so the busy-timeout tests use this store to hold a reader-blocking
// lock with holdLock.
func rollbackStoreWithRun(t *testing.T) string {
	t.Helper()
	path := storeWithRun(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "delete" {
		t.Fatalf("journal_mode = %q, want delete", mode)
	}
	return path
}

// storeWithRunAt is storeWithRun at a chosen path.
func storeWithRunAt(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h := contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-11-03T00:00:00Z"}
	tx, err := s.BeginRun(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// holdLock takes an EXCLUSIVE lock on the database at path through a
// separate handle, the lock a committing graphvein index holds on a
// rollback-journal store (rollbackStoreWithRun; on a WAL store it does not
// block readers). The
// returned function releases it; it may be called once, from any
// goroutine.
func holdLock(t *testing.T, path string) (release func()) {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		conn.Close()
		db.Close()
		t.Fatal(err)
	}
	return func() {
		if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
			t.Error(err)
		}
		conn.Close()
		db.Close()
	}
}

// releaseAfter releases the lock after d on a timer and returns a channel
// that is closed once it has been released.
func releaseAfter(d time.Duration, release func()) <-chan struct{} {
	done := make(chan struct{})
	time.AfterFunc(d, func() {
		release()
		close(done)
	})
	return done
}

// TestReadBusyTimeoutEveryConnection proves the busy timeout comes from the
// DSN: a connection opened after the pool recycled the first one reports
// the same bound, which a PRAGMA sent once with ExecContext would not.
func TestReadBusyTimeoutEveryConnection(t *testing.T) {
	ctx := context.Background()
	path := storeWithRun(t)
	const bound = 1234 * time.Millisecond
	s, err := openRead(ctx, path, bound)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	dsn, err := readDSN(path, bound)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "mode=ro") || !strings.Contains(dsn, "_pragma=busy_timeout(1234)") {
		t.Fatalf("DSN %q", dsn)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Millisecond)
	check := func() {
		t.Helper()
		var ms int
		if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&ms); err != nil {
			t.Fatal(err)
		}
		if ms != int(bound.Milliseconds()) {
			t.Fatalf("busy_timeout = %d, want %d", ms, bound.Milliseconds())
		}
	}
	check()
	// Wait until the pool has closed the first connection for its age, then
	// read again on the connection that replaces it.
	for i := 0; db.Stats().MaxLifetimeClosed == 0; i++ {
		if i == 500 {
			t.Fatal("the pool never recycled the connection")
		}
		time.Sleep(2 * time.Millisecond)
		check()
	}
	check()
	if _, err := db.ExecContext(ctx, "CREATE TABLE refused(x)"); err == nil {
		t.Fatal("read connection accepted a write")
	}
}

// TestReadWaitsForWriteLock proves a read that meets a writer's lock waits
// for it instead of failing at once, and then sees the committed data.
func TestReadWaitsForWriteLock(t *testing.T) {
	ctx := context.Background()
	path := rollbackStoreWithRun(t)
	s, err := openRead(ctx, path, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	release := holdLock(t, path)
	start := time.Now()
	done := releaseAfter(300*time.Millisecond, release)
	defer func() { <-done }()
	runs, err := s.Runs(ctx, "fixture")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("read failed after %v: %v", elapsed, err)
	}
	if len(runs) != 1 || runs[0].Header.Indexer.Name != "test" {
		t.Fatalf("runs = %+v", runs)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("read returned after %v, before the lock was released", elapsed)
	}
}

// TestReadBusyTimeoutExceeded proves the wait is bounded: a lock held past
// the bound fails the read with SQLITE_BUSY, and OpenRead's schema check
// waits like any other read.
func TestReadBusyTimeoutExceeded(t *testing.T) {
	ctx := context.Background()
	path := rollbackStoreWithRun(t)
	s, err := openRead(ctx, path, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	release := holdLock(t, path)
	start := time.Now()
	runs, err := s.Runs(ctx, "fixture")
	elapsed := time.Since(start)
	release()
	if err == nil || !strings.Contains(err.Error(), "SQLITE_BUSY") {
		t.Fatalf("read under a held lock: runs %+v, err %v", runs, err)
	}
	if elapsed < 150*time.Millisecond || elapsed >= 2*time.Second {
		t.Fatalf("read failed after %v, want about 200ms", elapsed)
	}

	release = holdLock(t, path)
	done := releaseAfter(300*time.Millisecond, release)
	defer func() { <-done }()
	ro, err := OpenRead(ctx, path)
	if err != nil {
		t.Fatalf("OpenRead under a briefly held lock: %v", err)
	}
	ro.Close()
}

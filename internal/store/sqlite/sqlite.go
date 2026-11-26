// Package sqlite supplies the default, pure-Go local store.
package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
	_ "modernc.org/sqlite"
)

// DefaultPath keeps the index alongside the project without requiring configuration.
const DefaultPath = ".graphvein/index.db"

// readBusyTimeout is how long a read connection waits for a lock before it
// fails with SQLITE_BUSY (ADR 0043). The writer puts the store in WAL
// journal mode (ADR 0049), so a running graphvein index, even while it
// commits, no longer blocks readers: they read the last committed snapshot.
// Readers can still meet a lock in a few cases: WAL recovery after a crash,
// the short exclusive lock of the last connection's close-time cleanup, a
// connection in exclusive locking mode, and stores on a filesystem without
// shared memory, which stay in rollback journaling where a commit (or an
// earlier page-cache spill) holds an EXCLUSIVE lock that blocks every
// reader. Three seconds covers those waits. The driver cannot cancel the
// wait with the request context, so the bound for each read also stays
// below serve's 5 s shutdown grace (a status request makes two reads, so it
// can take about 6 s; ADR 0043 records this). A longer lock still ends as
// an error, never as a stale answer reported as current.
const readBusyTimeout = 3 * time.Second

// writeBusyTimeout is how long a write connection waits for another
// connection's lock.
const writeBusyTimeout = 10 * time.Second

// checkpointBusyTimeout bounds how long the writer store's Close waits for
// readers to leave the WAL. It is shorter than writeBusyTimeout because the
// run is already committed when Close runs: a checkpoint that cannot finish
// only leaves a larger -wal, so graphvein index should not hold its exit
// for long on it.
const checkpointBusyTimeout = time.Second

// writeJournalSizeLimit caps the size of the -wal file SQLite leaves on
// disk after a commit or a WAL reset. One index run is one transaction, so
// a fresh run grows the WAL to about the size of the store; without a limit
// the file would keep that size until the next TRUNCATE checkpoint.
const writeJournalSizeLimit = 64 << 20

// OpenRead refuses to create or migrate a database for graph query commands.
// Every read connection waits up to readBusyTimeout for a lock, so serve,
// status and the query commands ride out the remaining short locks instead
// of failing at once.
func OpenRead(ctx context.Context, p string) (store.Store, error) {
	return openRead(ctx, p, readBusyTimeout)
}

// openRead is OpenRead with a chosen busy timeout, so tests can use a short
// bound.
func openRead(ctx context.Context, p string, busy time.Duration) (store.Store, error) {
	dsn, err := readDSN(p, busy)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite read: %w", err)
	}
	db.SetMaxOpenConns(1)
	s, err := sqlstore.OpenRead(ctx, db, "sqlite")
	if err != nil {
		return nil, readOpenError(p, err)
	}
	return s, nil
}

// sqliteCoder is implemented by the driver's error type; Code returns
// SQLite's extended result code. Matching on this interface rather than on
// the driver's concrete type lets tests build the error.
type sqliteCoder interface {
	Code() int
}

// SQLite result codes that readOpenError recognises
// (https://sqlite.org/rescode.html).
const (
	sqliteBusy              = 5    // SQLITE_BUSY, primary code
	sqliteCantOpen          = 14   // SQLITE_CANTOPEN, primary code
	sqliteReadonlyDirectory = 1544 // SQLITE_READONLY_DIRECTORY
)

// readOpenError turns the errors a read-only open of a WAL store gives in a
// directory the process cannot write into an actionable message. A reader
// must create index.db-shm when it is missing (wal.html section 5), so a
// store whose last process closed cleanly fails there with
// SQLITE_READONLY_DIRECTORY, and one that has a -wal but no -shm fails with
// SQLITE_CANTOPEN. A rollback-mode store opened fine in such a directory,
// so without this the failure looks like a damaged store. CANTOPEN has
// other causes, so it is only explained when index.db itself opens, a -wal
// exists and the -shm does not, and then only as the likely cause. Every
// other error is returned unchanged.
func readOpenError(p string, err error) error {
	var coded sqliteCoder
	if !errors.As(err, &coded) {
		return err
	}
	if p == "" {
		p = DefaultPath
	}
	dir := filepath.Dir(p)
	switch {
	case coded.Code() == sqliteReadonlyDirectory:
		return fmt.Errorf("the store directory %s is not writable: a WAL store needs to create index.db-shm next to index.db (ADR 0049); make the directory writable or copy index.db and its -wal/-shm files to a writable directory: %w", dir, err)
	case coded.Code()&0xff == sqliteCantOpen && fileReadable(p) && fileExists(p+"-wal") && !fileExists(p+"-shm"):
		return fmt.Errorf("cannot open the WAL store %s: index.db-shm is missing and SQLite could not create it, most likely because the directory %s is not writable (ADR 0049); make the directory writable or copy index.db and its -wal/-shm files to a writable directory: %w", p, dir, err)
	}
	return err
}

// fileReadable reports whether p opens for reading.
func fileReadable(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// fileExists reports whether p exists; any other stat error counts as
// unknown and returns false, so readOpenError keeps the driver's error.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// fileURI turns a file path into the file: URI the driver hands to SQLite
// unchanged, followed by query. A plain path is not safe: the driver cuts a
// name that does not start with "file:" at its first "?" and reads the
// rest as DSN parameters, so a directory such as "a?b" would open the
// wrong file. url.URL percent-escapes "?", "#", "%", spaces and non-ASCII
// bytes, and SQLite decodes them again. A Windows path such as C:\x gets a
// leading slash, giving file:///C:/x, which SQLite reads as a drive letter.
func fileURI(p, query string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolving SQLite path: %w", err)
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	u.RawQuery = query
	return u.String(), nil
}

// readDSN builds the read-only file: URI for p. The busy timeout is a DSN
// parameter rather than a PRAGMA sent with ExecContext because database/sql
// may close and reopen pooled connections; the driver applies DSN
// parameters to every new connection, while an Exec configures only the
// connection that ran it. SQLite itself also reads the query string and
// keeps mode=ro. The DSN carries no journal mode: the driver refuses to
// open a mode=ro connection that asks to change it, and a reader follows
// whatever mode the writer left in the file.
func readDSN(p string, busy time.Duration) (string, error) {
	if p == "" {
		p = DefaultPath
	}
	return fileURI(p, fmt.Sprintf("mode=ro&_pragma=busy_timeout(%d)", busy.Milliseconds()))
}

// writeDSN builds the writer's DSN. Every setting is a DSN parameter for the
// same reason as in readDSN: a pooled connection that database/sql reopens
// gets them again, and the driver validates the shorthand keys before any
// statement runs, so a typo fails the open. WAL lets readers keep reading
// the last committed run while graphvein index writes and commits (ADR
// 0049); synchronous FULL is SQLite's default, stated so the durability of
// a committed run does not depend on it. ":memory:" gets no journal
// settings: an in-memory database cannot use WAL.
func writeDSN(p string) (string, error) {
	base := fmt.Sprintf("_busy_timeout=%d&_foreign_keys=1", writeBusyTimeout.Milliseconds())
	if p == ":memory:" {
		return "file::memory:?" + base, nil
	}
	if p == "" {
		p = DefaultPath
	}
	return fileURI(p, base+fmt.Sprintf("&_journal_mode=WAL&_pragma=journal_size_limit(%d)&_synchronous=FULL", writeJournalSizeLimit))
}

// Open creates a local index and applies its versioned migrations
// automatically. It puts the store in WAL journal mode. A filesystem that
// gives SQLite no shared memory (for example some macOS network and AFP
// volumes) keeps rollback journaling: SQLite reports the old mode instead
// of an error, so Open reads the mode back and only logs a warning, because
// the store still works there, with readers waiting for a committing index
// as before. Network filesystems are not supported for the store at all:
// WAL needs every process to share memory on one host.
func Open(ctx context.Context, path string) (store.Store, error) {
	if path == "" {
		path = DefaultPath
	}
	memory := path == ":memory:"
	if !memory {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolving SQLite path: %w", err)
		}
		path = abs
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, fmt.Errorf("creating SQLite directory: %w", err)
		}
	}
	dsn, err := writeDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	wal := false
	if !memory {
		var mode string
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
			db.Close()
			return nil, fmt.Errorf("configuring SQLite %s: %w", path, err)
		}
		wal = strings.EqualFold(mode, "wal")
		if !wal {
			slog.Warn("SQLite store is not in WAL mode; readers wait for a committing index as before (ADR 0043)", "path", path, "journal_mode", mode)
		}
	}
	s, err := sqlstore.Open(ctx, db, "sqlite")
	if err != nil {
		return nil, err
	}
	if !wal {
		return s, nil
	}
	return &walStore{Store: s, db: db, path: path}, nil
}

// walStore is a writer store in WAL mode. Its Close checkpoints the WAL
// into the database file and truncates it before closing the pool, so the
// -wal file is empty after graphvein index exits even while serve keeps the
// store open (with nothing else open, SQLite's own close-time checkpoint
// then deletes -wal and -shm). Only the writer checkpoints: readers open
// the file read-only.
type walStore struct {
	store.Store
	db   *sql.DB
	path string
}

// Close runs the TRUNCATE checkpoint and closes the store. The run is
// already committed and durable in the WAL, so a busy or failed checkpoint
// is only logged; Close returns the pool's close error. It is skipped while
// a transaction still holds the pool's only connection, because waiting
// for that connection would block Close.
//
// The checkpoint first runs without waiting. Only if a reader is still
// inside a read transaction does it log at Info that index is waiting and
// retry, waiting up to checkpointBusyTimeout. Both attempts run on a
// dedicated connection with its own busy_timeout, and that connection is
// discarded afterwards, so no pooled connection keeps the changed timeout.
func (s *walStore) Close() error {
	if s.db.Stats().InUse > 0 {
		slog.Warn("skipping SQLite WAL checkpoint: a transaction is still open", "path", s.path)
		return s.Store.Close()
	}
	s.checkpoint(context.Background())
	return s.Store.Close()
}

// checkpoint runs the close-time TRUNCATE checkpoint and logs its outcome.
func (s *walStore) checkpoint(ctx context.Context) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		slog.Warn("SQLite WAL checkpoint failed", "path", s.path, "error", err)
		return
	}
	defer conn.Close()
	// Returning ErrBadConn from Raw makes database/sql discard the
	// connection instead of returning it to the pool.
	defer conn.Raw(func(any) error { return driver.ErrBadConn })

	busy, logFrames, checkpointed, err := truncateCheckpoint(ctx, conn, 0)
	if err == nil && busy != 0 {
		slog.Info("waiting for readers to leave the SQLite WAL before closing", "path", s.path, "timeout", checkpointBusyTimeout)
		busy, logFrames, checkpointed, err = truncateCheckpoint(ctx, conn, checkpointBusyTimeout)
	}
	switch {
	case err != nil:
		slog.Warn("SQLite WAL checkpoint failed", "path", s.path, "error", err)
	case busy != 0:
		slog.Warn("SQLite WAL checkpoint did not complete; readers still use the WAL", "path", s.path, "busy", busy, "log_frames", logFrames, "checkpointed_frames", checkpointed)
	}
}

// truncateCheckpoint sets conn's busy timeout and runs one TRUNCATE
// checkpoint on it. A SQLITE_BUSY error is reported as busy, like the
// checkpoint's own busy column, so the caller can retry.
func truncateCheckpoint(ctx context.Context, conn *sql.Conn, wait time.Duration) (busy, logFrames, checkpointed int, err error) {
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", wait.Milliseconds())); err != nil {
		return 0, 0, 0, fmt.Errorf("setting checkpoint busy timeout: %w", err)
	}
	err = conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logFrames, &checkpointed)
	var coded sqliteCoder
	if errors.As(err, &coded) && coded.Code()&0xff == sqliteBusy {
		return 1, 0, 0, nil
	}
	return busy, logFrames, checkpointed, err
}

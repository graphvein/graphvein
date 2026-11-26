package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestWriterPragmasEveryConnection proves the writer's settings come from
// the DSN: a connection opened after the pool recycled the first one
// reports WAL and the same pragmas, which settings sent once with
// ExecContext would not. It asserts WAL on every OS, so CI shows that the
// driver supports WAL on Windows and macOS.
func TestWriterPragmasEveryConnection(t *testing.T) {
	ctx := context.Background()
	dsn, err := writeDSN(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Millisecond)
	want := map[string]string{
		"journal_mode":       "wal",
		"busy_timeout":       "10000",
		"foreign_keys":       "1",
		"journal_size_limit": "67108864",
		"synchronous":        "2",
	}
	check := func() {
		t.Helper()
		for name, value := range want {
			var got string
			if err := db.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != value {
				t.Fatalf("%s = %q, want %q", name, got, value)
			}
		}
	}
	check()
	for i := 0; db.Stats().MaxLifetimeClosed == 0; i++ {
		if i == 500 {
			t.Fatal("the pool never recycled the connection")
		}
		time.Sleep(2 * time.Millisecond)
		check()
	}
	check()
}

// journalMode reads the journal mode stored in the file at path through a
// raw handle.
func journalMode(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	return mode
}

// TestOpenConvertsRollbackStore proves an existing rollback-journal store
// (every store written before WAL) is converted by Open and keeps its data.
func TestOpenConvertsRollbackStore(t *testing.T) {
	ctx := context.Background()
	path := rollbackStoreWithRun(t)
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Header.Indexer.Name != "test" {
		t.Fatalf("runs after conversion = %+v", runs)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if mode := journalMode(t, path); mode != "wal" {
		t.Fatalf("journal_mode after Open = %q, want wal", mode)
	}
}

// TestOpenPathWithQuestionMark proves Open and OpenRead use the whole path,
// including characters that are special in a URI. A raw path would be cut
// at "?" by the driver; Windows file names cannot contain "?".
func TestOpenPathWithQuestionMark(t *testing.T) {
	names := []string{"a#b", "a%b", "a%20b", "a b", "ä"}
	if runtime.GOOS != "windows" {
		names = append(names, "a?b")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			tmp := t.TempDir()
			path := filepath.Join(tmp, name, "index.db")
			storeWithRunAt(t, path)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("store not at its path: %v", err)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != name {
				var got []string
				for _, e := range entries {
					got = append(got, e.Name())
				}
				t.Fatalf("entries in %s = %q, want only %q", tmp, got, name)
			}
			ro, err := OpenRead(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer ro.Close()
			runs, err := ro.Runs(ctx, "fixture")
			if err != nil || len(runs) != 1 {
				t.Fatalf("runs %+v, err %v", runs, err)
			}
		})
	}
}

// fillWAL writes about 50 MB in conn's open transaction, far beyond
// SQLite's default 2 MB page cache, so the uncommitted pages spill. With
// rollback journaling a spill takes the EXCLUSIVE lock that blocks readers.
func fillWAL(t *testing.T, conn *sql.Conn) {
	t.Helper()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "CREATE TABLE spill(b BLOB)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := conn.ExecContext(ctx, "INSERT INTO spill VALUES(randomblob(1048576))"); err != nil {
			t.Fatal(err)
		}
	}
}

// TestReadDuringWriteWAL proves a reader is not blocked by a writer that
// holds the write lock and has spilled pages: it answers at once with the
// committed run only, and sees the new run after the writer commits.
func TestReadDuringWriteWAL(t *testing.T) {
	ctx := context.Background()
	path := storeWithRun(t)
	dsn, err := writeDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO index_runs(id, repository, payload) SELECT id || '-next', repository, payload FROM index_runs"); err != nil {
		t.Fatal(err)
	}
	fillWAL(t, conn)

	ro, err := openRead(ctx, path, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for i := 0; i < 5; i++ {
		start := time.Now()
		runs, err := ro.Runs(ctx, "fixture")
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("read during a write failed after %v: %v", elapsed, err)
		}
		if len(runs) != 1 {
			t.Fatalf("read during a write saw %d runs, want the committed one", len(runs))
		}
		if elapsed >= 100*time.Millisecond {
			t.Fatalf("read during a write took %v", elapsed)
		}
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	runs, err := ro.Runs(ctx, "fixture")
	if err != nil || len(runs) != 2 {
		t.Fatalf("read after commit: %d runs, err %v", len(runs), err)
	}
}

// sidecars lists which of the -wal and -shm files exist next to path, with
// the -wal size.
func sidecars(t *testing.T, path string) (walSize int64, wal, shm bool) {
	t.Helper()
	fi, err := os.Stat(path + "-wal")
	switch {
	case err == nil:
		walSize, wal = fi.Size(), true
	case !errors.Is(err, fs.ErrNotExist):
		t.Fatal(err)
	}
	if _, err := os.Stat(path + "-shm"); err == nil {
		shm = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return walSize, wal, shm
}

// TestWALFilesAfterClose proves the sidecar lifecycle the docs describe: a
// writer closing alone leaves no -wal; a writer closing while a reader has
// the store open leaves an empty -wal (the TRUNCATE checkpoint). Which
// files remain after the reader closes last is logged per OS for the
// record. t.TempDir's cleanup also proves every handle was released, which
// on Windows includes the -shm mapping.
func TestWALFilesAfterClose(t *testing.T) {
	ctx := context.Background()
	path := storeWithRun(t)
	if size, wal, shm := sidecars(t, path); wal {
		t.Fatalf("after the writer closed alone: -wal present (%d bytes), -shm %v", size, shm)
	}

	ro, err := OpenRead(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ro.Runs(ctx, "fixture"); err != nil {
		t.Fatal(err)
	}
	w, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SaveFactCache(ctx, "fixture", "a.go", "hash", "1", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	// The reader is not inside a read transaction (Runs has returned), so
	// the checkpoint must complete: any checkpoint log line is a failure.
	logged := captureLog(t, func() {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if logged != "" {
		t.Fatalf("Close logged with no reader in a read transaction: %s", logged)
	}
	size, wal, shm := sidecars(t, path)
	t.Logf("%s: after the writer closed with a reader open: -wal %v (%d bytes), -shm %v", runtime.GOOS, wal, size, shm)
	if size != 0 {
		t.Fatalf("after the writer closed with a reader open: -wal is %d bytes, want 0", size)
	}
	runs, err := ro.Runs(ctx, "fixture")
	if err != nil || len(runs) != 1 {
		t.Fatalf("reader after the writer closed: %d runs, err %v", len(runs), err)
	}
	if err := ro.Close(); err != nil {
		t.Fatal(err)
	}
	size, wal, shm = sidecars(t, path)
	t.Logf("%s: after the reader closed last: -wal %v (%d bytes), -shm %v", runtime.GOOS, wal, size, shm)

	w, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if size, wal, shm := sidecars(t, path); wal {
		t.Fatalf("after a writer reopened and closed alone: -wal present (%d bytes), -shm %v", size, shm)
	}
}

// captureLog runs f with the default slog logger writing to a buffer at
// Debug level and returns what was logged.
func captureLog(t *testing.T, f func()) string {
	t.Helper()
	var logged bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)
	f()
	return logged.String()
}

// TestCloseCheckpointWaitBounded proves the close-time checkpoint waits at
// most checkpointBusyTimeout for a reader inside a read transaction, logs
// at Info before it waits and warns when it gives up, and leaves no pooled
// connection with the changed busy timeout.
func TestCloseCheckpointWaitBounded(t *testing.T) {
	ctx := context.Background()
	path := storeWithRun(t)
	dsn, err := readDSN(path, readBusyTimeout)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	w, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	ws, ok := w.(*walStore)
	if !ok {
		t.Fatalf("Open returned %T, want *walStore", w)
	}
	tx, err := reader.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM index_runs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	// A commit after the reader's snapshot leaves WAL frames the reader may
	// still need, so a TRUNCATE checkpoint cannot finish.
	if err := w.SaveFactCache(ctx, "fixture", "a.go", "hash", "1", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	logged := captureLog(t, func() { ws.checkpoint(ctx) })
	elapsed := time.Since(start)
	if elapsed < checkpointBusyTimeout/2 || elapsed > checkpointBusyTimeout+time.Second {
		t.Fatalf("checkpoint with a reader in a read transaction took %v, want about %v", elapsed, checkpointBusyTimeout)
	}
	if !strings.Contains(logged, "level=INFO msg=\"waiting for readers") {
		t.Fatalf("no Info line before waiting: %s", logged)
	}
	if !strings.Contains(logged, "level=WARN msg=\"SQLite WAL checkpoint did not complete") {
		t.Fatalf("no warning after the bounded wait: %s", logged)
	}
	var timeout int
	if err := ws.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout != int(writeBusyTimeout.Milliseconds()) {
		t.Fatalf("pooled connection busy_timeout after the checkpoint = %d, want %d", timeout, writeBusyTimeout.Milliseconds())
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	logged = captureLog(t, func() {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if logged != "" {
		t.Fatalf("Close after the reader left logged: %s", logged)
	}
	if size, wal, _ := sidecars(t, path); wal && size != 0 {
		t.Fatalf("-wal is %d bytes after the reader left and the writer closed", size)
	}
}

// codedError stands in for the driver's error type, which tests cannot
// build, so readOpenError's mapping is tested on every OS and as any user.
type codedError int

func (e codedError) Error() string { return fmt.Sprintf("sqlite error (%d)", int(e)) }
func (e codedError) Code() int     { return int(e) }

// TestReadOpenError proves which open errors get the read-only directory
// explanation. A real SQLITE_READONLY_DIRECTORY needs a process that cannot
// write the directory, which root (the cloud container) and Windows cannot
// set up; T016h's review fixes record the end-to-end measurement.
func TestReadOpenError(t *testing.T) {
	write := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name    string
		err     error
		files   []string // created next to index.db, which always exists
		explain string   // substring of the explanation; "" means unchanged
	}{
		{"readonly directory", codedError(sqliteReadonlyDirectory), nil, "is not writable: a WAL store needs to create index.db-shm"},
		{"readonly directory with sidecars", codedError(sqliteReadonlyDirectory), []string{"-wal", "-shm"}, "is not writable"},
		{"cantopen, -wal without -shm", codedError(sqliteCantOpen), []string{"-wal"}, "index.db-shm is missing and SQLite could not create it"},
		{"extended cantopen, -wal without -shm", codedError(sqliteCantOpen | 1<<8), []string{"-wal"}, "index.db-shm is missing"},
		{"cantopen, no -wal", codedError(sqliteCantOpen), nil, ""},
		{"cantopen, -shm present", codedError(sqliteCantOpen), []string{"-wal", "-shm"}, ""},
		{"plain readonly", codedError(8), nil, ""},
		{"busy", codedError(sqliteBusy), nil, ""},
		{"not a driver error", errors.New("boom"), nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "index.db")
			write(t, path)
			for _, suffix := range tt.files {
				write(t, path+suffix)
			}
			wrapped := fmt.Errorf("opening existing sqlite store: %w", tt.err)
			got := readOpenError(path, wrapped)
			if tt.explain == "" {
				if got != wrapped {
					t.Fatalf("readOpenError changed %v into %v", wrapped, got)
				}
				return
			}
			if !strings.Contains(got.Error(), tt.explain) || !strings.Contains(got.Error(), dir) || !strings.Contains(got.Error(), "ADR 0049") {
				t.Fatalf("readOpenError = %q, want %q, the directory and ADR 0049", got, tt.explain)
			}
			if !errors.Is(got, tt.err) {
				t.Fatalf("readOpenError lost the driver error: %v", got)
			}
		})
	}
	t.Run("cantopen, index.db missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.db")
		write(t, path+"-wal")
		err := codedError(sqliteCantOpen)
		if got := readOpenError(path, err); got != error(err) {
			t.Fatalf("readOpenError changed %v into %v", err, got)
		}
	})
}

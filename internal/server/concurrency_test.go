package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// lastRunOf is the last run as the status endpoint reports it, read
// through the write store so the expectation does not depend on the
// handler under test.
func lastRunOf(t *testing.T, s store.Store) *lastRun {
	t.Helper()
	runs, err := s.Runs(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	last := latestRun(runs)
	if last == nil {
		t.Fatal("no run stored")
	}
	return last
}

// TestStatusDuringOpenRun proves an index run that has started but not
// committed does not disturb readers: status still reports the previous
// run and stale files, and an API section answers. On SQLite the open
// transaction holds only RESERVED, which readers pass; on Postgres the
// reads see the committed snapshot (MVCC).
func TestStatusDuringOpenRun(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		ctx := context.Background()
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		first := lastRunOf(t, w)
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		h := contract.Header{Type: "header", ContractVersion: contract.Version, Indexer: contract.Indexer{Name: "graphvein", Version: "test"}, Repository: "fixture", GeneratedAt: "2026-11-03T00:00:00Z"}
		tx, err := w.BeginRun(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil {
				t.Error(err)
			}
		}()
		if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: "main.go", Language: "go", Precision: "best_effort", ContentHash: contract.ContentHash([]byte("package main\n\nfunc main() {}\n"))}); err != nil {
			t.Fatal(err)
		}

		opt := testOptions(root, s)
		got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if got.Index.LastRun == nil || *got.Index.LastRun != *first {
			t.Fatalf("last run during an open run = %+v, want %+v", got.Index.LastRun, first)
		}
		if got.Freshness.State != stateStale || len(got.Freshness.ChangedFiles) != 1 || got.Freshness.ChangedFiles[0] != "main.go" {
			t.Fatalf("freshness during an open run: %+v", got.Freshness)
		}
		rec := get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("API section during an open run: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// polled is one status answer collected by the poller goroutine.
type polled struct {
	code int
	body []byte
}

// TestConcurrentIndexAndServe re-indexes while status is polled, the way a
// graphvein index next to a running serve does. Every answer must succeed,
// and fresh may only be reported beside the run that indexed the edit. The
// fixture's commit takes milliseconds, far inside the read busy timeout, so
// the result does not depend on timing.
func TestConcurrentIndexAndServe(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		prev := lastRunOf(t, w)
		for round := 1; round <= 5; round++ {
			src := fmt.Sprintf("package main\n\n// round %d\nfunc main() {}\n", round)
			if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			stop := make(chan struct{})
			done := make(chan []polled)
			go func() {
				var out []polled
				for {
					select {
					case <-stop:
						done <- out
						return
					default:
					}
					rec := get(t, opt, http.MethodGet, "/api/v0/status", nil)
					out = append(out, polled{code: rec.Code, body: rec.Body.Bytes()})
				}
			}()
			indexRoot(t, root, w)
			close(stop)
			answers := <-done
			next := lastRunOf(t, w)
			if next.FinishedAt == prev.FinishedAt {
				t.Fatalf("round %d: the new run has the previous run's finish time %s", round, next.FinishedAt)
			}
			for i, a := range answers {
				if a.code != http.StatusOK {
					t.Fatalf("round %d answer %d: status %d: %s", round, i, a.code, a.body)
				}
				var got statusResponse
				if err := json.Unmarshal(a.body, &got); err != nil {
					t.Fatal(err)
				}
				switch {
				case got.Freshness.State == stateFresh:
					if got.Index.LastRun == nil || *got.Index.LastRun != *next {
						t.Fatalf("round %d answer %d: fresh beside run %+v, want the new run %+v", round, i, got.Index.LastRun, next)
					}
				case got.Freshness.State != stateStale:
					t.Fatalf("round %d answer %d: state %q (%s)", round, i, got.Freshness.State, got.Freshness.Error)
				}
			}
			got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
			if got.Freshness.State != stateFresh || got.Index.LastRun == nil || *got.Index.LastRun != *next {
				t.Fatalf("round %d after index: %+v beside %+v", round, got.Freshness, got.Index.LastRun)
			}
			t.Logf("round %d: %d answers while indexing", round, len(answers))
			prev = next
		}
	})
}

// holdSQLiteLock takes an EXCLUSIVE lock on the SQLite file at path
// through its own handle, the lock graphvein index holds while it commits
// to a rollback-journal store. On a WAL store this lock does not block
// readers, so callers first switch the store with toRollback. The returned
// function releases it.
func holdSQLiteLock(t *testing.T, path string) (release func()) {
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

// toRollback switches the closed SQLite store at path back to rollback
// journaling, the mode a store keeps on a filesystem without shared memory
// (ADR 0049), where a committing writer still blocks readers.
func toRollback(t *testing.T, path string) {
	t.Helper()
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
}

// TestStatusSQLiteLocked runs status against a rollback-journal SQLite
// store whose lock is held by another connection. A short lock is waited
// out and the answer is correct; a lock held past the real read bound
// fails as store_error, never as a 200. The second case waits for the full
// bound on each read (the freshness read, then the runs read).
func TestStatusSQLiteLocked(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	w, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	root := fixtureRoot(t)
	indexRoot(t, root, w)
	want := lastRunOf(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	toRollback(t, path)
	ro, err := sqlite.OpenRead(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	opt := testOptions(root, ro)

	release := holdSQLiteLock(t, path)
	start := time.Now()
	released := make(chan struct{})
	time.AfterFunc(500*time.Millisecond, func() {
		release()
		close(released)
	})
	rec := get(t, opt, http.MethodGet, "/api/v0/status", nil)
	elapsed := time.Since(start)
	<-released
	got := decodeStatus(t, rec)
	if got.Freshness.State != stateFresh || got.Index.LastRun == nil || *got.Index.LastRun != *want {
		t.Fatalf("after a short lock: %+v beside %+v", got.Freshness, got.Index.LastRun)
	}
	if elapsed < 450*time.Millisecond {
		t.Fatalf("status answered after %v, before the lock was released", elapsed)
	}

	release = holdSQLiteLock(t, path)
	rec = get(t, opt, http.MethodGet, "/api/v0/status", nil)
	release()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("under a held lock: status %d: %s", rec.Code, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Code != "store_error" || !strings.HasPrefix(env.Error.Message, "reading runs:") || !strings.Contains(env.Error.Message, "SQLITE_BUSY") {
		t.Fatalf("envelope %+v", env)
	}
}

// endpointCount sums the endpoint counts of an API section answer.
func endpointCount(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("API section: status %d: %s", rec.Code, rec.Body.String())
	}
	var list endpointsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tr := range list.Transports {
		n += tr.EndpointCount
	}
	return n
}

// TestStatusDuringWriteLockWAL is the unit-scale form of a large rewrite
// next to a running serve: another connection holds the write lock on the
// WAL store for longer than the read bound and writes far beyond the page
// cache, so pages spill. Status still answers at once with the previous
// run, and the API section with every endpoint, because WAL readers read
// the last committed snapshot.
func TestStatusDuringWriteLockWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	w, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := apiFixtureRoot(t)
	indexRoot(t, root, w)
	want := lastRunOf(t, w)
	ro, err := sqlite.OpenRead(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	opt := testOptions(root, ro)
	endpoints := endpointCount(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil))
	if endpoints == 0 {
		t.Fatal("the API fixture has no endpoints")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	locked := time.Now()
	for _, q := range []string{"CREATE TABLE spill(b BLOB)", "DELETE FROM index_runs"} {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 50; i++ {
		if _, err := conn.ExecContext(ctx, "INSERT INTO spill VALUES(randomblob(1048576))"); err != nil {
			t.Fatal(err)
		}
	}
	answers := 0
	for time.Since(locked) < 4*time.Second {
		start := time.Now()
		got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("status under the write lock took %v", elapsed)
		}
		if got.Index.LastRun == nil || *got.Index.LastRun != *want {
			t.Fatalf("status under the write lock: last run %+v, want %+v", got.Index.LastRun, want)
		}
		if got.Freshness.State != stateFresh {
			t.Fatalf("status under the write lock: freshness %+v", got.Freshness)
		}
		if n := endpointCount(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil)); n != endpoints {
			t.Fatalf("API section under the write lock: %d endpoints, want %d", n, endpoints)
		}
		answers++
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d answers under a write lock held for %v", answers, time.Since(locked).Round(time.Millisecond))
}

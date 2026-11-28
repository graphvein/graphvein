package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// TestIndexRefusedWhileLocked holds the index lock with another locker and
// runs graphvein index, built-in and --indexer: both exit 1 with the lock
// message before scanning, and the store does not change. After the
// release the same command indexes.
func TestIndexRefusedWhileLocked(t *testing.T) {
	lockBackends(t, func(t *testing.T, locker store.IndexLocker, lockName string, runs func() int) {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		if code := run([]string{"index", root, "--repository", "fixture"}, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		before := runs()
		lease, err := locker.Acquire(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		want := "another index run holds the lock for this store (" + lockName + "); try again when it has finished\n"
		for _, args := range [][]string{
			{"index", root, "--repository", "fixture"},
			{"index", root, "--repository", "fixture", "--indexer", os.Args[0]},
		} {
			out.Reset()
			errOut.Reset()
			started := time.Now()
			// Only the Index line (ADR 0093), printed before the lock is
			// taken; nothing about a run.
			if code := run(args, &out, &errOut); code != 1 || errOut.String() != want || !strings.HasPrefix(out.String(), "Index: ") || strings.Count(out.String(), "\n") != 1 {
				t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errOut.String())
			}
			if time.Since(started) < time.Second {
				t.Fatalf("%v: refused without waiting for the holder", args)
			}
			if after := runs(); after != before {
				t.Fatalf("%v: store changed while refused: %d runs, had %d", args, after, before)
			}
			left, _ := filepath.Glob(filepath.Join(root, ".graphvein", "snapshot-*"))
			if len(left) != 0 {
				t.Fatalf("%v: refused run wrote a snapshot: %v", args, left)
			}
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		if code := run([]string{"index", root, "--repository", "fixture"}, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		if after := runs(); after != before+1 {
			t.Fatalf("%d runs after the release, want %d", after, before+1)
		}
	})
}

// TestIngestRefusedWhileLocked: graphvein ingest takes the same lock as
// graphvein index, so it exits 1 with the same message while another
// holder has it, leaves the store unchanged, and ingests after the release.
func TestIngestRefusedWhileLocked(t *testing.T) {
	lockBackends(t, func(t *testing.T, locker store.IndexLocker, lockName string, runs func() int) {
		const snapshot = "../../testdata/contract/valid.ndjson"
		var out, errOut bytes.Buffer
		if code := run([]string{"ingest", snapshot}, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		before := runs()
		lease, err := locker.Acquire(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		out.Reset()
		errOut.Reset()
		started := time.Now()
		want := "another index run holds the lock for this store (" + lockName + "); try again when it has finished\n"
		if code := run([]string{"ingest", snapshot}, &out, &errOut); code != 1 || errOut.String() != want || out.Len() != 0 {
			t.Fatalf("ingest while locked: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
		}
		if time.Since(started) < time.Second {
			t.Fatal("ingest refused without waiting for the holder")
		}
		if after := runs(); after != before {
			t.Fatalf("store changed while refused: %d runs, had %d", after, before)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		if code := run([]string{"ingest", snapshot}, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
		if after := runs(); after != before+1 {
			t.Fatalf("%d runs after the release, want %d", after, before+1)
		}
	})
}

// lockBackends runs check on SQLite and on Postgres (in a schema of its
// own, skipped without GRAPHVEIN_TEST_POSTGRES_URL) with the environment
// pointing graphvein at that store, a second locker on it, its lock name
// and a function counting the runs of repository "fixture".
func lockBackends(t *testing.T, check func(t *testing.T, locker store.IndexLocker, lockName string, runs func() int)) {
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.db")
		t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
		t.Setenv("GRAPHVEIN_SQLITE_PATH", path)
		locker := sqlite.IndexLocker(path)
		defer locker.Close()
		check(t, locker, sqlite.LockPath(path), func() int {
			s, err := sqlite.OpenRead(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			runs, err := s.Runs(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			return len(runs)
		})
	})
	t.Run("postgres", func(t *testing.T) {
		base := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
		unavailable := func() {
			if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
				t.Fatal("GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
			}
			t.Skip("Postgres unavailable: GRAPHVEIN_TEST_POSTGRES_URL unset or unreachable")
		}
		if base == "" {
			unavailable()
		}
		admin, err := egress.OpenPostgres(base, nil)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = admin.PingContext(ctx)
		cancel()
		if err != nil {
			unavailable()
		}
		schema := "graphvein_cli_lock_" + strings.ToLower(rand.Text())
		if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}()
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		withSchema := u.String()
		t.Setenv("GRAPHVEIN_POSTGRES_URL", withSchema)
		t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(t.TempDir(), "unused.db"))
		locker := postgres.IndexLocker(withSchema, "fixture")
		defer locker.Close()
		check(t, locker, "Postgres advisory lock", func() int {
			s, err := postgres.OpenRead(context.Background(), withSchema)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			runs, err := s.Runs(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			return len(runs)
		})
	})
}

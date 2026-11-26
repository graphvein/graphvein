package store_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// holderEnv makes the test binary hold an index lock instead of running
// tests: "sqlite <store path>" or "postgres <url>". TestMain takes the
// lock, prints "held" and waits until it is killed, so a test can prove
// that the lock of a dead process is released.
const holderEnv = "GRAPHVEIN_TEST_LOCK_HOLDER"

func TestMain(m *testing.M) {
	if spec := os.Getenv(holderEnv); spec != "" {
		os.Exit(holdLock(spec))
	}
	os.Exit(m.Run())
}

// holdLock is the subprocess side of holderEnv.
func holdLock(spec string) int {
	backend, location, _ := strings.Cut(spec, " ")
	var locker store.IndexLocker
	switch backend {
	case "sqlite":
		locker = sqlite.IndexLocker(location)
	case "postgres":
		locker = postgres.IndexLocker(location, "fixture")
	default:
		fmt.Fprintln(os.Stderr, "unknown backend", backend)
		return 2
	}
	if _, err := locker.Acquire(context.Background(), 5*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("held")
	time.Sleep(time.Hour)
	return 0
}

// lockStore is one store's index lock under test.
type lockStore struct {
	// locker returns a new locker on the store, for repository "fixture".
	locker func() store.IndexLocker
	// other returns a locker on a different store (another file, another
	// schema) for the same repository name.
	other func() store.IndexLocker
	// holder is the holderEnv value that locks this store.
	holder string
	// lose makes a held lease unprovable: SQLite removes the lock file,
	// Postgres terminates the lease's session. It reports false where the
	// platform refuses (Windows keeps an open file).
	lose func(t *testing.T) bool
	// granted counts granted index locks (Postgres only; -1 for SQLite).
	granted func(t *testing.T) int
	// project returns a new project locker on the store (ADR 0075).
	project func(name string) store.IndexLocker
	// projectGranted counts granted project locks of name (Postgres only;
	// -1 for SQLite, whose project lock is the store lock).
	projectGranted func(t *testing.T, name string) int
}

// lockBackends runs check against a SQLite store and, like backends, a
// fresh Postgres schema when GRAPHVEIN_TEST_POSTGRES_URL is set.
func lockBackends(t *testing.T, check func(t *testing.T, ls lockStore)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "index.db")
		otherPath := filepath.Join(dir, "other", "index.db")
		check(t, lockStore{
			locker: func() store.IndexLocker { return sqlite.IndexLocker(path) },
			other:  func() store.IndexLocker { return sqlite.IndexLocker(otherPath) },
			holder: "sqlite " + path,
			lose: func(t *testing.T) bool {
				err := os.Remove(sqlite.LockPath(path))
				if err != nil && runtime.GOOS == "windows" {
					return false
				}
				if err != nil {
					t.Fatal(err)
				}
				return true
			},
			granted:        func(*testing.T) int { return -1 },
			project:        func(name string) store.IndexLocker { return sqlite.ProjectLocker(path, name) },
			projectGranted: func(*testing.T, string) int { return -1 },
		})
	})
	t.Run("postgres", func(t *testing.T) {
		ctx := context.Background()
		base := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
		if base == "" {
			postgresUnavailable(t, base, false)
		}
		admin, err := egress.OpenPostgres(base, nil)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		t.Cleanup(func() { admin.Close() })
		pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		pingErr := admin.PingContext(pingCtx)
		cancel()
		if pingErr != nil {
			postgresUnavailable(t, base, false)
		}
		schemaURL := func() string {
			schema := "graphvein_lock_" + strings.ToLower(rand.Text())
			if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
					t.Error(err)
				}
			})
			return withSearchPath(t, base, schema)
		}
		mine, other := schemaURL(), schemaURL()
		granted := func(t *testing.T) int {
			t.Helper()
			var n int
			// Advisory locks are per database and other packages' tests take
			// index locks in the same database concurrently, so count only
			// this schema's key.
			if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=714839211 AND objsubid=2 AND granted AND objid=$1::bigint::oid AND database=(SELECT oid FROM pg_database WHERE datname=current_database())", int64(uint32(postgresKey(t, mine)))).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		check(t, lockStore{
			locker: func() store.IndexLocker { return postgres.IndexLocker(mine, "fixture") },
			other:  func() store.IndexLocker { return postgres.IndexLocker(other, "fixture") },
			holder: "postgres " + mine,
			lose: func(t *testing.T) bool {
				var n int
				// Advisory locks are per database, so other tests' locks
				// may be granted too; only this schema's key is ours.
				if err := admin.QueryRowContext(ctx, `SELECT count(pg_terminate_backend(pid)) FROM pg_locks WHERE locktype='advisory' AND classid=714839211 AND objsubid=2 AND granted AND objid=$1::bigint::oid`, int64(uint32(postgresKey(t, mine)))).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 1 {
					t.Fatalf("terminated %d sessions, want 1", n)
				}
				return true
			},
			granted: granted,
			project: func(name string) store.IndexLocker { return postgres.ProjectLocker(mine, name) },
			projectGranted: func(t *testing.T, name string) int {
				t.Helper()
				var n int
				if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=714839212 AND objsubid=2 AND granted AND objid=$1::bigint::oid AND database=(SELECT oid FROM pg_database WHERE datname=current_database())", int64(uint32(postgresSchemaKey(t, mine, name)))).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			},
		})
	})
}

// withSearchPath adds a search_path runtime parameter to a Postgres URL,
// the way a user points Graphvein at a schema.
func withSearchPath(t *testing.T, base, schema string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		// Keyword/value connection strings take the parameter as a pair.
		return base + " search_path=" + schema
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// postgresKey reads the second lock key the locker derives for the
// schema of u and repository "fixture", computed by Postgres itself from
// a held lock, so the test does not repeat the hash.
func postgresKey(t *testing.T, u string) int32 {
	t.Helper()
	return postgresSchemaKey(t, u, "fixture")
}

// postgresSchemaKey is postgres.LockKey for the schema of u and name.
func postgresSchemaKey(t *testing.T, u, name string) int32 {
	t.Helper()
	db, err := egress.OpenPostgres(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var schema string
	if err := db.QueryRow("SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	return postgres.LockKey(schema, name)
}

func TestIndexLockAcquireAndState(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx := context.Background()
		a, b := ls.locker(), ls.locker()
		defer a.Close()
		defer b.Close()
		wantState := func(l store.IndexLocker, want store.LockState) {
			t.Helper()
			got, err := l.State(ctx)
			if err != nil || got != want {
				t.Fatalf("state %q, %v; want %q", got, err, want)
			}
		}
		wantState(a, store.LockFree)
		if n := ls.granted(t); n > 0 {
			t.Fatalf("State took the lock: %d granted", n)
		}
		lease, err := a.Acquire(ctx, 2*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		wantState(a, store.LockHeldHere)
		wantState(b, store.LockHeldElsewhere)
		if n := ls.granted(t); n != -1 && n != 1 {
			t.Fatalf("%d granted index locks, want 1", n)
		}
		started := time.Now()
		_, err = b.Acquire(ctx, 300*time.Millisecond)
		if !errors.Is(err, store.ErrIndexLocked) {
			t.Fatalf("second acquire: %v, want ErrIndexLocked", err)
		}
		if elapsed := time.Since(started); elapsed < 250*time.Millisecond {
			t.Fatalf("second acquire gave up after %v, before its wait", elapsed)
		}
		if _, err := a.Acquire(ctx, 0); !errors.Is(err, store.ErrIndexLocked) {
			t.Fatalf("acquire by the holder: %v, want ErrIndexLocked", err)
		}
		if err := lease.Check(ctx); err != nil {
			t.Fatal(err)
		}
		// Another store with the same repository name is independent.
		other := ls.other()
		defer other.Close()
		wantState(other, store.LockFree)
		otherLease, err := other.Acquire(ctx, 0)
		if err != nil {
			t.Fatalf("other store blocked: %v", err)
		}
		if err := otherLease.Release(); err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
		if err := lease.Release(); err != nil {
			t.Fatalf("second release: %v", err)
		}
		wantState(a, store.LockFree)
		wantState(b, store.LockFree)
		lease, err = b.Acquire(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		wantState(b, store.LockHeldHere)
		wantState(a, store.LockHeldElsewhere)
		if err := b.Close(); err != nil {
			t.Fatal(err)
		}
		wantState(a, store.LockFree)
	})
}

// TestIndexLockStateDuringOwnAcquire asks a locker for its state while
// its own Acquire waits for another holder (T016j review C): State answers
// after that Acquire ended, held_here once it succeeded, never the
// process's own lock reported as another holder's.
func TestIndexLockStateDuringOwnAcquire(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx := context.Background()
		holder, mine := ls.locker(), ls.locker()
		defer holder.Close()
		defer mine.Close()
		held, err := holder.Acquire(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		acquired := make(chan error, 1)
		go func() {
			_, err := mine.Acquire(ctx, 3*time.Second)
			acquired <- err
		}()
		time.Sleep(200 * time.Millisecond)
		go func() {
			time.Sleep(300 * time.Millisecond)
			held.Release()
		}()
		state, err := mine.State(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := <-acquired; err != nil {
			t.Fatalf("acquire: %v", err)
		}
		if state != store.LockHeldHere {
			t.Fatalf("state during own acquire: %q, want %q", state, store.LockHeldHere)
		}
	})
}

// TestIndexLockStateNeverBlocksAcquire probes the state in a loop while
// another locker acquires and releases with the CLI's 2 s wait: a probe
// may hold the SQLite lock for a moment, but an acquirer always rides it
// out, and a Postgres probe never takes the lock at all.
//
// Each acquire starts only once a new probe has finished since the last
// one, so the probes really run between and during the acquires: on a
// loaded machine the 20 acquires used to finish before the probe
// goroutine had run once, and the test failed with "no probe ran".
func TestIndexLockStateNeverBlocksAcquire(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx, cancel := context.WithCancel(context.Background())
		prober, acquirer := ls.locker(), ls.locker()
		defer prober.Close()
		defer acquirer.Close()
		var wg sync.WaitGroup
		var probes atomic.Int64
		var probeErr error
		probing := make(chan struct{})
		wg.Go(func() {
			defer close(probing)
			for ctx.Err() == nil {
				if _, err := prober.State(ctx); err != nil && ctx.Err() == nil {
					probeErr = err
					return
				}
				probes.Add(1)
			}
		})
		stop := func() {
			cancel()
			wg.Wait()
		}
		seen := int64(0)
		for i := range 20 {
			if err := waitForProbe(probing, &probes, seen); err != nil {
				stop()
				t.Fatalf("before acquire %d: %v (probe error: %v)", i, err, probeErr)
			}
			seen = probes.Load()
			start := time.Now()
			lease, err := acquirer.Acquire(ctx, 2*time.Second)
			if err != nil {
				stop()
				t.Fatalf("acquire %d during probes, after %v and %d probes: %v",
					i, time.Since(start), probes.Load(), err)
			}
			if err := lease.Release(); err != nil {
				stop()
				t.Fatal(err)
			}
		}
		stop()
		if probeErr != nil {
			t.Fatal(probeErr)
		}
	})
}

// waitForProbe returns once more than seen probes have finished, or an
// error when the prober stopped or none finished within 10 s.
func waitForProbe(probing <-chan struct{}, probes *atomic.Int64, seen int64) error {
	deadline := time.After(10 * time.Second)
	for probes.Load() <= seen {
		select {
		case <-probing:
			return errors.New("the prober stopped")
		case <-deadline:
			return errors.New("no probe finished within 10 s")
		case <-time.After(time.Millisecond):
		}
	}
	return nil
}

// TestIndexLockReleasedWhenHolderDies kills a holder process and expects
// the lock to be free again within 5 s: the operating system (SQLite) or
// the database (Postgres) releases it, with no stale-lock logic.
func TestIndexLockReleasedWhenHolderDies(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx := context.Background()
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), holderEnv+"="+ls.holder)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer cmd.Wait()
		defer cmd.Process.Kill()
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil || line != "held\n" {
			t.Fatalf("holder said %q: %v", line, err)
		}
		locker := ls.locker()
		defer locker.Close()
		if state, err := locker.State(ctx); err != nil || state != store.LockHeldElsewhere {
			t.Fatalf("state %q, %v; want held_elsewhere", state, err)
		}
		if _, err := locker.Acquire(ctx, 0); !errors.Is(err, store.ErrIndexLocked) {
			t.Fatalf("acquire while held: %v", err)
		}
		if err := cmd.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		lease, err := locker.Acquire(ctx, 5*time.Second)
		if err != nil {
			t.Fatalf("lock not released after the holder died: %v", err)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	})
}

// TestIndexLeaseCheckDetectsLoss makes a held lease unprovable and expects
// Check to fail, so a run refuses to commit.
func TestIndexLeaseCheckDetectsLoss(t *testing.T) {
	lockBackends(t, func(t *testing.T, ls lockStore) {
		ctx := context.Background()
		locker := ls.locker()
		defer locker.Close()
		lease, err := locker.Acquire(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer lease.Release()
		if err := lease.Check(ctx); err != nil {
			t.Fatal(err)
		}
		if !ls.lose(t) {
			// Windows refuses to delete the open lock file, which is the
			// protection the check stands in for elsewhere.
			return
		}
		if err := lease.Check(ctx); err == nil {
			t.Fatal("Check passed after the lock was lost")
		}
	})
}

// TestIndexLockUnavailable reports an error, never "free", when the lock
// cannot be opened: here the store directory is a regular file, which
// fails the same way for root and on every system.
func TestIndexLockUnavailable(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	locker := sqlite.IndexLocker(filepath.Join(blocker, "index.db"))
	defer locker.Close()
	if state, err := locker.State(context.Background()); err == nil {
		t.Fatalf("state %q without error", state)
	}
	if _, err := locker.Acquire(context.Background(), 0); err == nil || errors.Is(err, store.ErrIndexLocked) {
		t.Fatalf("acquire: %v, want a non-lock error", err)
	}
}

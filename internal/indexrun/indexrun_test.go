package indexrun

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// runStore is one backend under test: a writer opener and the index lock
// of the same store.
type runStore struct {
	open   func(context.Context) (store.Store, error)
	locker func() store.IndexLocker
	// dir is the store's directory (Options.SnapshotDir): "" for Postgres.
	dir string
}

// backends runs check against a SQLite store and, when
// GRAPHVEIN_TEST_POSTGRES_URL is set, a fresh Postgres schema (CI's
// postgres job requires the second, like the other shared backend tests).
func backends(t *testing.T, check func(t *testing.T, rs runStore)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "index.db")
		check(t, runStore{
			open:   func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) },
			locker: func() store.IndexLocker { return sqlite.IndexLocker(path) },
			dir:    filepath.Dir(path),
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
		schema := "graphvein_indexrun_" + strings.ToLower(rand.Text())
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
		check(t, runStore{
			open:   func(ctx context.Context) (store.Store, error) { return postgres.Open(ctx, withSchema) },
			locker: func() store.IndexLocker { return postgres.IndexLocker(withSchema, "fixture") },
		})
	})
}

// fixture copies the small Go fixture repository.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	return root
}

// harness holds the lock and runs Run with recorded progress.
type harness struct {
	t      *testing.T
	rs     runStore
	root   string
	lease  store.IndexLease
	locker store.IndexLocker
	events []Progress
}

func newHarness(t *testing.T, rs runStore) *harness {
	t.Helper()
	locker := rs.locker()
	t.Cleanup(func() { locker.Close() })
	lease, err := locker.Acquire(context.Background(), LockWait)
	if err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, rs: rs, root: fixture(t), lease: lease, locker: locker}
}

func (h *harness) options() Options {
	h.events = nil
	return Options{
		Root:        h.root,
		Repository:  "fixture",
		Lease:       h.lease,
		OpenStore:   h.rs.open,
		SnapshotDir: h.rs.dir,
		Parser:      parse.Options{Workers: 1, CacheDir: filepath.Join(h.t.TempDir(), "cache")},
		Progress:    func(p Progress) { h.events = append(h.events, p) },
	}
}

// runs reads the committed runs through a fresh store.
func (h *harness) runs() []store.Run {
	h.t.Helper()
	s, err := h.rs.open(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	defer s.Close()
	runs, err := s.Runs(context.Background(), "fixture")
	if err != nil {
		h.t.Fatal(err)
	}
	return runs
}

// noSnapshotLeft checks that the temporary snapshot was removed from the
// store's directory and that the run, whose store is outside the root,
// created nothing under the root.
func (h *harness) noSnapshotLeft() {
	h.t.Helper()
	if _, err := os.Lstat(filepath.Join(h.root, ".graphvein")); !errors.Is(err, os.ErrNotExist) {
		h.t.Fatalf("the run created .graphvein in the indexed root: %v", err)
	}
	if h.rs.dir == "" {
		return
	}
	left, _ := filepath.Glob(filepath.Join(h.rs.dir, "snapshot-*"))
	if len(left) != 0 {
		h.t.Fatalf("temporary snapshot left: %v", left)
	}
}

var phaseOrder = []string{PhaseStarting, index.PhaseScanning, index.PhaseCache, index.PhaseParsing, index.PhaseLinking, PhaseWriting, PhaseCommitting, PhaseFinishing}

// checkProgress verifies phase order and that counts appear only in the
// countable phases, with done <= total and the right unit.
func checkProgress(t *testing.T, events []Progress) {
	t.Helper()
	last := -1
	for _, p := range events {
		at := slices.Index(phaseOrder, p.Phase)
		if at < 0 || at < last {
			t.Fatalf("phase %q out of order in %+v", p.Phase, events)
		}
		last = at
		switch {
		case !p.Counted:
			if p.Done != 0 || p.Total != 0 || p.Unit != "" {
				t.Fatalf("uncounted report carries numbers: %+v", p)
			}
		case p.Phase == index.PhaseCache || p.Phase == index.PhaseParsing:
			if p.Unit != UnitFiles || p.Done > p.Total || p.Done < 0 {
				t.Fatalf("bad file count %+v", p)
			}
		case p.Phase == PhaseWriting:
			if p.Unit != UnitRecords || p.Done > p.Total || p.Done < 0 {
				t.Fatalf("bad record count %+v", p)
			}
		default:
			t.Fatalf("counted report in phase %q", p.Phase)
		}
	}
}

func TestRunCommits(t *testing.T) {
	backends(t, func(t *testing.T, rs runStore) {
		h := newHarness(t, rs)
		opt := h.options()
		var committed store.Run
		var finishingBeforeCommit bool
		opt.OnCommit = func(run store.Run) {
			committed = run
			finishingBeforeCommit = slices.ContainsFunc(h.events, func(p Progress) bool { return p.Phase == PhaseFinishing })
		}
		result, err := Run(context.Background(), opt)
		if err != nil {
			t.Fatal(err)
		}
		checkProgress(t, h.events)
		for _, phase := range phaseOrder {
			if !slices.ContainsFunc(h.events, func(p Progress) bool { return p.Phase == phase }) {
				t.Fatalf("phase %q never reported: %+v", phase, h.events)
			}
		}
		if finishingBeforeCommit {
			t.Fatal("finishing reported before the commit")
		}
		var lastWriting Progress
		for _, p := range h.events {
			if p.Phase == PhaseWriting {
				lastWriting = p
			}
		}
		if !lastWriting.Counted || lastWriting.Done != lastWriting.Total || lastWriting.Total == 0 {
			t.Fatalf("writing did not end at its total: %+v", lastWriting)
		}
		if result.Run.ID == "" || result.Run != committed {
			t.Fatalf("result run %+v, committed %+v", result.Run, committed)
		}
		runs := h.runs()
		if len(runs) != 1 || runs[0].ID != result.Run.ID || runs[0].FinishedAt != result.Run.FinishedAt {
			t.Fatalf("stored runs %+v, want %+v", runs, result.Run)
		}
		h.noSnapshotLeft()
		// Run never releases the lease: the caller does, after the close.
		if state, err := h.locker.State(context.Background()); err != nil || state != store.LockHeldHere {
			t.Fatalf("lock after Run: %q, %v", state, err)
		}
	})
}

// failingLease is a lease whose Check fails, as after a lost session.
type failingLease struct{ store.IndexLease }

func (failingLease) Check(context.Context) error { return errors.New("session gone") }

func TestRunFailuresKeepThePreviousRun(t *testing.T) {
	backends(t, func(t *testing.T, rs runStore) {
		h := newHarness(t, rs)
		if _, err := Run(context.Background(), h.options()); err != nil {
			t.Fatal(err)
		}
		before := h.runs()
		emitErr := errors.New("emit failed")
		cases := []struct {
			name  string
			setup func(*Options) context.Context
			check func(error) bool
		}{
			{"emit", func(o *Options) context.Context {
				o.Emit = func(context.Context, string, store.Store, io.Writer, index.Options) (index.Summary, error) {
					return index.Summary{}, emitErr
				}
				return context.Background()
			}, func(err error) bool { return errors.Is(err, emitErr) }},
			{"ingest", func(o *Options) context.Context {
				o.Emit = func(_ context.Context, _ string, _ store.Store, w io.Writer, _ index.Options) (index.Summary, error) {
					_, err := io.WriteString(w, "{\"type\":\"not-a-header\"}\n")
					return index.Summary{}, err
				}
				return context.Background()
			}, func(err error) bool { return err != nil }},
			{"lost lease", func(o *Options) context.Context {
				o.Lease = failingLease{o.Lease}
				return context.Background()
			}, func(err error) bool { return errors.Is(err, ErrLockLost) }},
			{"cancelled", func(o *Options) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				progress := o.Progress
				o.Progress = func(p Progress) {
					progress(p)
					if p.Phase == index.PhaseParsing {
						cancel()
					}
				}
				// Change a file so the run has something to parse.
				if err := os.WriteFile(filepath.Join(h.root, "extra.go"), []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return ctx
			}, func(err error) bool { return errors.Is(err, context.Canceled) }},
			{"store", func(o *Options) context.Context {
				o.OpenStore = func(context.Context) (store.Store, error) { return nil, errors.New("no store") }
				return context.Background()
			}, func(err error) bool {
				var se *StoreError
				return errors.As(err, &se)
			}},
		}
		for _, tc := range cases {
			opt := h.options()
			ctx := tc.setup(&opt)
			_, err := Run(ctx, opt)
			if !tc.check(err) {
				t.Fatalf("%s: error %v", tc.name, err)
			}
			if after := h.runs(); !slices.Equal(after, before) {
				t.Fatalf("%s: runs changed: %+v", tc.name, after)
			}
			h.noSnapshotLeft()
			if tc.name != "store" && !slices.ContainsFunc(h.events, func(p Progress) bool { return p.Phase == PhaseFinishing }) {
				t.Fatalf("%s: store not closed under the lock: %+v", tc.name, h.events)
			}
			checkProgress(t, h.events)
		}
		// The lock is still this caller's, and released only by it.
		if err := h.lease.Release(); err != nil {
			t.Fatal(err)
		}
		other := rs.locker()
		defer other.Close()
		lease, err := other.Acquire(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		lease.Release()
	})
}

func TestWithStoreNeedsLeaseAndOpener(t *testing.T) {
	err := WithStore(context.Background(), nil, nil, Hooks{}, func(context.Context, store.Store) error {
		t.Fatal("ran without a lease")
		return nil
	})
	if err == nil {
		t.Fatal("no error without a lease")
	}
}

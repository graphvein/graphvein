package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/postgres"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

// holderEnv makes the test binary hold an index lock, as a separate
// graphvein index process would, instead of running tests: "sqlite <store
// path>" or "postgres <url>". It prints "held" and waits to be killed.
const holderEnv = "GRAPHVEIN_TEST_LOCK_HOLDER"

func TestMain(m *testing.M) {
	if spec := os.Getenv(holderEnv); spec != "" {
		backend, location, _ := strings.Cut(spec, " ")
		locker := sqlite.IndexLocker(location)
		if backend == "postgres" {
			locker = postgres.IndexLocker(location, "fixture")
		}
		if _, err := locker.Acquire(context.Background(), 5*time.Second); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("held")
		time.Sleep(time.Hour)
	}
	os.Exit(m.Run())
}

// runBackend is one store a re-index test runs against: the read-only
// store serve answers from, a writer opener and the index lock, all on the
// same store.
type runBackend struct {
	read   store.Store
	open   func(context.Context) (store.Store, error)
	locker func() store.IndexLocker
	holder string
	// dir is the store's directory (Options.StoreDir): "" for Postgres.
	dir string
}

// runBackends runs check on SQLite and, when GRAPHVEIN_TEST_POSTGRES_URL
// is set, on a fresh Postgres schema (the postgres CI job requires it),
// after indexing root once with the writer.
func runBackends(t *testing.T, root string, check func(t *testing.T, rb runBackend)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "index.db")
		open := func(ctx context.Context) (store.Store, error) { return sqlite.Open(ctx, path) }
		w, err := open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		indexRoot(t, root, w)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		ro, err := sqlite.OpenRead(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		check(t, runBackend{read: ro, open: open, locker: func() store.IndexLocker { return sqlite.IndexLocker(path) }, holder: "sqlite " + path, dir: filepath.Dir(path)})
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
		schema := "graphvein_reindex_" + strings.ToLower(rand.Text())
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
		open := func(ctx context.Context) (store.Store, error) { return postgres.Open(ctx, withSchema) }
		w, err := open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		indexRoot(t, root, w)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		ro, err := postgres.OpenRead(context.Background(), withSchema)
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		check(t, runBackend{read: ro, open: open, locker: func() store.IndexLocker { return postgres.IndexLocker(withSchema, "fixture") }, holder: "postgres " + withSchema})
	})
}

// runHarness is a handler with re-index enabled, kept for the whole test
// so its runner (and job) persist between requests.
type runHarness struct {
	t       *testing.T
	handler http.Handler
	runs    *runner
	opt     Options
}

func newRunHarness(t *testing.T, opt Options) *runHarness {
	t.Helper()
	h, runs := newHandler(opt, testHosts)
	t.Cleanup(runs.stop)
	return &runHarness{t: t, handler: h, runs: runs, opt: opt}
}

// do sends one request; body nil means no body.
func (h *runHarness) do(method, path string, body string, header map[string]string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = testHost
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

var jsonHeader = map[string]string{"Content-Type": "application/json"}

// post starts a run the way the UI does.
func (h *runHarness) post() *httptest.ResponseRecorder {
	h.t.Helper()
	return h.do(http.MethodPost, indexRunPath, "{}", jsonHeader)
}

// document decodes a run document strictly.
func decodeRun(t *testing.T, rec *httptest.ResponseRecorder, status int) indexRunResponse {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var out indexRunResponse
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

func (h *runHarness) get() indexRunResponse {
	h.t.Helper()
	return decodeRun(h.t, h.do(http.MethodGet, indexRunPath, "", nil), http.StatusOK)
}

// errorCode returns the v0 error code of rec after checking its status.
func runErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return env.Error.Code
}

// wait polls GET until the job has ended and released the lock, checking on every answer that
// the document never claims more than the run proved: a fraction only in
// a counted phase, never done above total, a committed run only with its
// ID and time, and a held lock while the job has not ended.
func (h *runHarness) wait() indexRunResponse {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		doc := h.get()
		job := doc.Job
		if job == nil {
			h.t.Fatal("job vanished")
		}
		checkJobInvariants(h.t, doc)
		// The outcome is recorded just before the lease is
		// released, so an ended job may still hold the lock briefly.
		if job.EndedAt != nil && doc.Lock.State != lockHeldHere {
			return doc
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("job did not end")
	return indexRunResponse{}
}

func checkJobInvariants(t *testing.T, doc indexRunResponse) {
	t.Helper()
	job := doc.Job
	if job.Progress != nil {
		if job.Phase == nil || !slices.Contains([]string{"cache", "parsing", "writing"}, *job.Phase) {
			t.Fatalf("progress outside a counted phase: %+v", job)
		}
		if job.Progress.Done < 0 || job.Progress.Done > job.Progress.Total {
			t.Fatalf("progress %+v", job.Progress)
		}
		if want := map[string]string{"cache": "files", "parsing": "files", "writing": "records"}[*job.Phase]; job.Progress.Unit != want {
			t.Fatalf("unit %q in phase %q", job.Progress.Unit, *job.Phase)
		}
	}
	if job.EndedAt == nil && doc.Lock.State != lockHeldHere {
		t.Fatalf("running job without a held lock: %+v", doc)
	}
	if job.EndedAt != nil && (job.Phase != nil || job.Progress != nil) {
		t.Fatalf("ended job with a phase: %+v", job)
	}
	if (job.State == jobCommitted) != (job.Run != nil) {
		t.Fatalf("run %+v in state %q", job.Run, job.State)
	}
	if (job.State == jobFailed) != (job.Error != nil) {
		t.Fatalf("error %+v in state %q", job.Error, job.State)
	}
}

func runOptions(root string, rb runBackend) Options {
	opt := testOptions(root, rb.read)
	opt.Locker = rb.locker()
	opt.OpenWriter = rb.open
	opt.StoreDir = rb.dir
	return opt
}

// fakeLocker answers with fixed results, for the request pipeline tests.
type fakeLocker struct {
	acquireErr error
	state      store.LockState
	stateErr   error
}

func (f fakeLocker) Acquire(context.Context, time.Duration) (store.IndexLease, error) {
	return nil, f.acquireErr
}
func (f fakeLocker) State(context.Context) (store.LockState, error) { return f.state, f.stateErr }
func (f fakeLocker) Close() error                                   { return nil }

func TestIndexRunRequestPipeline(t *testing.T) {
	root := fixtureRoot(t)
	opt := testOptions(root, nil)
	opt.Locker = fakeLocker{acquireErr: fmt.Errorf("%w (test)", store.ErrIndexLocked), state: store.LockHeldElsewhere}
	opt.OpenWriter = func(context.Context) (store.Store, error) { return nil, errors.New("unused") }
	// A store whose latest run is the built-in indexer's, without I/O.
	opt.Store = runsStore{runs: []store.Run{{ID: "r", FinishedAt: "2026-11-07T07:00:00Z", Header: contract.Header{Indexer: contract.Indexer{Name: "graphvein"}}}}}
	h := newRunHarness(t, opt)
	// passes is the answer of a request that cleared every check: the
	// fake lock is held, so 409 index_locked.
	const passes = "index_locked"
	for _, tc := range []struct {
		name, method, path, body string
		header                   map[string]string
		status                   int
		code, allow              string
	}{
		{"post", http.MethodPost, indexRunPath, "{}", jsonHeader, 409, passes, ""},
		{"charset", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json; charset=utf-8"}, 409, passes, ""},
		{"trailing space", http.MethodPost, indexRunPath, " {}\n", jsonHeader, 409, passes, ""},
		{"options", http.MethodOptions, indexRunPath, "", nil, 405, "method_not_allowed", "GET, HEAD, POST"},
		{"put", http.MethodPut, indexRunPath, "{}", jsonHeader, 405, "method_not_allowed", "GET, HEAD, POST"},
		{"delete", http.MethodDelete, indexRunPath, "", nil, 405, "method_not_allowed", "GET, HEAD, POST"},
		{"post status", http.MethodPost, "/api/v0/status", "{}", jsonHeader, 405, "method_not_allowed", "GET, HEAD"},
		{"post endpoints", http.MethodPost, "/api/v0/api/endpoints", "{}", jsonHeader, 405, "method_not_allowed", "GET, HEAD"},
		{"post page", http.MethodPost, "/", "{}", jsonHeader, 405, "method_not_allowed", "GET, HEAD"},
		{"post run subpath", http.MethodPost, indexRunPath + "/cancel", "{}", jsonHeader, 405, "method_not_allowed", "GET, HEAD"},
		{"host", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Host": "evil.example:7420"}, 403, "host_not_allowed", ""},
		{"same-site", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-site"}, 403, "cross_site_request", ""},
		{"cross-site", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "cross-site"}, 403, "cross_site_request", ""},
		{"fetch none", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "none"}, 409, passes, ""},
		{"same-origin", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:7420"}, 409, passes, ""},
		{"foreign origin", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Origin": "http://evil.example"}, 403, "cross_site_request", ""},
		// An old browser without Fetch Metadata: the Origin is one of this
		// server's names but not the request's Host, which only
		// CrossOriginProtection rejects.
		{"origin host mismatch", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Origin": "http://localhost:7420"}, 403, "cross_site_request", ""},
		{"origin matches host", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Origin": "http://127.0.0.1:7420"}, 409, passes, ""},
		{"no content type", http.MethodPost, indexRunPath, "{}", nil, 415, "unsupported_media_type", ""},
		{"text/plain", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "text/plain"}, 415, "unsupported_media_type", ""},
		{"form", http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 415, "unsupported_media_type", ""},
		{"empty body", http.MethodPost, indexRunPath, "", jsonHeader, 400, "bad_request", ""},
		{"null", http.MethodPost, indexRunPath, "null", jsonHeader, 400, "bad_request", ""},
		{"array", http.MethodPost, indexRunPath, "[]", jsonHeader, 400, "bad_request", ""},
		{"field", http.MethodPost, indexRunPath, `{"workers":2}`, jsonHeader, 400, "bad_request", ""},
		{"two objects", http.MethodPost, indexRunPath, "{} {}", jsonHeader, 400, "bad_request", ""},
		{"too large", http.MethodPost, indexRunPath, "{" + strings.Repeat(" ", 2048) + "}", jsonHeader, 400, "bad_request", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(tc.method, tc.path, tc.body, tc.header)
			if code := runErrorCode(t, rec, tc.status); code != tc.code {
				t.Fatalf("code %q, want %q", code, tc.code)
			}
			if got := rec.Header().Get("Allow"); got != tc.allow {
				t.Fatalf("Allow %q, want %q", got, tc.allow)
			}
			if rec.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("security headers missing")
			}
		})
	}
	// GET and HEAD answer with the document; the fake lock is held
	// elsewhere and this server has started no job.
	doc := h.get()
	if doc.Lock.State != lockHeldElsewhere || doc.Lock.Error != nil || doc.Job != nil || doc.Repository != "fixture" {
		t.Fatalf("document %+v", doc)
	}
	if rec := h.do(http.MethodHead, indexRunPath, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("HEAD %d", rec.Code)
	}
}

// runsStore answers Runs only.
type runsStore struct {
	store.Store
	runs []store.Run
}

func (r runsStore) Runs(context.Context, string) ([]store.Run, error) { return r.runs, nil }

// TestIndexRunLockUnavailable: without a locker, or when the lock cannot
// be probed or opened, GET reports the lock as unknown with the reason and
// POST answers 503; nothing is ever reported free on a guess.
func TestIndexRunLockUnavailable(t *testing.T) {
	root := fixtureRoot(t)
	base := testOptions(root, runsStore{})
	writer := func(context.Context) (store.Store, error) { return nil, errors.New("unused") }
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		locker store.IndexLocker
		writer func(context.Context) (store.Store, error)
	}{
		{"no locker", nil, writer},
		{"no writer", fakeLocker{state: store.LockFree}, nil},
		{"probe fails", fakeLocker{stateErr: errors.New("probe failed"), acquireErr: errors.New("open failed")}, writer},
		// A real SQLite lock whose directory is a regular file: fails for
		// root too, unlike a read-only directory.
		{"unopenable lock", sqlite.IndexLocker(filepath.Join(blocker, "index.db")), writer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opt := base
			opt.Locker, opt.OpenWriter = tc.locker, tc.writer
			h := newRunHarness(t, opt)
			// Deferred as well as registered for cleanup: a failed check must
			// stop a gated job before the backend's deferred teardown runs.
			defer h.runs.stop()
			doc := h.get()
			if doc.Lock.State != lockUnknown || doc.Lock.Error == nil || *doc.Lock.Error == "" {
				t.Fatalf("lock %+v", doc.Lock)
			}
			if code := runErrorCode(t, h.post(), http.StatusServiceUnavailable); code != "index_lock_unavailable" {
				t.Fatalf("code %q", code)
			}
			if h.get().Job != nil {
				t.Fatal("a refused POST recorded a job")
			}
		})
	}
}

// TestIndexRunCommits starts a run, polls it to the end and checks that the
// committed run is the one status then names, with the same finish time.
func TestIndexRunCommits(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		h := newRunHarness(t, opt)
		// Deferred as well as registered for cleanup: a failed check must
		// stop a gated job before the backend's deferred teardown runs.
		defer h.runs.stop()
		before := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if doc := h.get(); doc.Lock.State != lockFree || doc.Job != nil {
			t.Fatalf("before: %+v", doc)
		}
		rec := h.post()
		started := decodeRun(t, rec, http.StatusAccepted)
		if rec.Header().Get("Location") != indexRunPath {
			t.Fatalf("Location %q", rec.Header().Get("Location"))
		}
		job := started.Job
		if job == nil || job.State != jobRunning || job.ID == "" || job.StartedAt == "" || job.Run != nil {
			t.Fatalf("started job %+v", job)
		}
		if job.Replaces == nil || job.Replaces.FinishedAt != before.Index.LastRun.FinishedAt {
			t.Fatalf("replaces %+v, status last run %+v", job.Replaces, before.Index.LastRun)
		}
		done := h.wait()
		if done.Job.State != jobCommitted || done.Job.ID != job.ID || done.Lock.State != lockFree {
			t.Fatalf("finished: %+v %+v", done.Lock, done.Job)
		}
		after := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if after.Index.LastRun == nil || after.Index.LastRun.FinishedAt != done.Job.Run.FinishedAt {
			t.Fatalf("status last run %+v, job run %+v", after.Index.LastRun, done.Job.Run)
		}
		if after.Index.LastRun.FinishedAt == before.Index.LastRun.FinishedAt {
			t.Fatal("status still names the previous run")
		}
		if after.Freshness.State != stateFresh {
			t.Fatalf("freshness after the run: %+v", after.Freshness)
		}
		runs, err := rb.read.Runs(context.Background(), "fixture")
		if err != nil || !slices.ContainsFunc(runs, func(r store.Run) bool { return r.ID == done.Job.Run.ID }) {
			t.Fatalf("committed run %q not stored: %v", done.Job.Run.ID, err)
		}
		// The store is outside root: the run writes nothing there.
		if _, err := os.Lstat(filepath.Join(root, ".graphvein")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the re-index created .graphvein in the root: %v", err)
		}
		if rb.dir != "" {
			left, _ := filepath.Glob(filepath.Join(rb.dir, "snapshot-*"))
			if len(left) != 0 {
				t.Fatalf("temporary snapshot left: %v", left)
			}
		}
		// The lock is free again: graphvein index could run now.
		other := rb.locker()
		defer other.Close()
		lease, err := other.Acquire(context.Background(), 0)
		if err != nil {
			t.Fatal(err)
		}
		lease.Release()
	})
}

// gate blocks a writer store at a chosen point until released, so a test
// can look at the server while the run is in flight.
type gate struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func newGate() *gate { return &gate{reached: make(chan struct{}), release: make(chan struct{})} }

// pass signals the first arrival and blocks until released or ctx ends.
func (g *gate) pass(ctx context.Context) error {
	g.once.Do(func() { close(g.reached) })
	select {
	case <-g.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// gatedStore stops the run at its first fact cache lookup (cache phase)
// or its first file write (inside the run transaction).
type gatedStore struct {
	store.Store
	cache, write *gate
	fail         error
	panicking    bool
}

func (g *gatedStore) FactCache(ctx context.Context, repo, path, hash, version string) ([]byte, error) {
	if g.panicking {
		panic("injected panic")
	}
	if g.fail != nil {
		return nil, g.fail
	}
	if g.cache != nil {
		if err := g.cache.pass(ctx); err != nil {
			return nil, err
		}
	}
	return g.Store.FactCache(ctx, repo, path, hash, version)
}

func (g *gatedStore) BeginRun(ctx context.Context, h contract.Header, options ...store.RunOptions) (store.Transaction, error) {
	tx, err := g.Store.BeginRun(ctx, h, options...)
	if err != nil || g.write == nil {
		return tx, err
	}
	return &gatedTransaction{Transaction: tx, gate: g.write}, nil
}

type gatedTransaction struct {
	store.Transaction
	gate *gate
}

func (g *gatedTransaction) UpsertFile(ctx context.Context, f contract.File) error {
	if err := g.gate.pass(ctx); err != nil {
		return err
	}
	return g.Transaction.UpsertFile(ctx, f)
}

func gatedOpener(open func(context.Context) (store.Store, error), wrap func(store.Store) store.Store) func(context.Context) (store.Store, error) {
	return func(ctx context.Context) (store.Store, error) {
		s, err := open(ctx)
		if err != nil {
			return nil, err
		}
		return wrap(s), nil
	}
}

// TestIndexRunConflicts: a lock held by another locker in this process, by
// a separate process, or by this server's own running job answers 409
// index_locked, and GET says who holds it.
func TestIndexRunConflicts(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		ctx := context.Background()
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		g := newGate()
		opt.OpenWriter = gatedOpener(rb.open, func(s store.Store) store.Store { return &gatedStore{Store: s, cache: g} })
		h := newRunHarness(t, opt)
		// Deferred as well as registered for cleanup: a failed check must
		// stop a gated job before the backend's deferred teardown runs.
		defer h.runs.stop()

		// Another locker in this process (as a second serve would be).
		other := rb.locker()
		defer other.Close()
		lease, err := other.Acquire(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		if code := runErrorCode(t, h.post(), http.StatusConflict); code != "index_locked" {
			t.Fatalf("code %q", code)
		}
		if doc := h.get(); doc.Lock.State != lockHeldElsewhere || doc.Job != nil {
			t.Fatalf("held elsewhere: %+v", doc)
		}
		lease.Release()

		// A separate process, like graphvein index.
		cmd := exec.Command(os.Args[0])
		cmd.Env = append(os.Environ(), holderEnv+"="+rb.holder)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil || line != "held\n" {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatalf("holder said %q: %v", line, err)
		}
		rec := h.post()
		if code := runErrorCode(t, rec, http.StatusConflict); code != "index_locked" || !strings.Contains(rec.Body.String(), "another process") {
			t.Fatalf("code %q: %s", code, rec.Body.String())
		}
		if doc := h.get(); doc.Lock.State != lockHeldElsewhere || doc.Job != nil {
			t.Fatalf("held by a process: %+v", doc)
		}
		cmd.Process.Kill()
		cmd.Wait()
		// Postgres ends the holder's session when it notices the closed
		// socket, a moment after the process died (F3: within 0.3 s).
		for deadline := time.Now().Add(5 * time.Second); ; {
			doc := h.get()
			if doc.Lock.State == lockFree {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("after the holder died: %+v", doc)
			}
			time.Sleep(20 * time.Millisecond)
		}

		// This server's own job, stopped in the cache phase.
		decodeRun(t, h.post(), http.StatusAccepted)
		<-g.reached
		rec = h.post()
		if code := runErrorCode(t, rec, http.StatusConflict); code != "index_locked" || !strings.Contains(rec.Body.String(), "this server") {
			t.Fatalf("code %q: %s", code, rec.Body.String())
		}
		doc := h.get()
		checkJobInvariants(t, doc)
		if doc.Lock.State != lockHeldHere || doc.Job.State != jobRunning || doc.Job.Phase == nil || *doc.Job.Phase != "cache" {
			t.Fatalf("own job: %+v %+v", doc.Lock, doc.Job)
		}
		if doc.Job.Progress == nil || doc.Job.Progress.Unit != "files" {
			t.Fatalf("cache phase without its file count: %+v", doc.Job)
		}
		// A graphvein index started now is refused too.
		if _, err := other.Acquire(ctx, 0); !errors.Is(err, store.ErrIndexLocked) {
			t.Fatalf("CLI acquire during a server run: %v", err)
		}
		close(g.release)
		if done := h.wait(); done.Job.State != jobCommitted {
			t.Fatalf("job %+v", done.Job)
		}
	})
}

// TestIndexRunReadsDuringRun holds the run inside its write transaction:
// status and the API section keep answering from the previous run, which
// status names, and never from the run in flight. A failed run leaves
// status on the previous run as well.
func TestIndexRunReadsDuringRun(t *testing.T) {
	root := apiFixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		g := newGate()
		opt.OpenWriter = gatedOpener(rb.open, func(s store.Store) store.Store { return &gatedStore{Store: s, write: g} })
		h := newRunHarness(t, opt)
		// Deferred as well as registered for cleanup: a failed check must
		// stop a gated job before the backend's deferred teardown runs.
		defer h.runs.stop()
		before := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		endpoints := endpointCount(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil))
		// A changed file makes the previous run stale, so a status during
		// the run could not pass for the new run's.
		// The name is new in each backend's subtest, which share root.
		added := "added_" + strings.ToLower(rand.Text()) + ".ts"
		if err := os.WriteFile(filepath.Join(root, added), []byte("export const added = 1;\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		decodeRun(t, h.post(), http.StatusAccepted)
		<-g.reached
		doc := h.get()
		checkJobInvariants(t, doc)
		if doc.Job.Phase == nil || *doc.Job.Phase != "writing" || doc.Job.Progress == nil || doc.Job.Progress.Unit != "records" {
			t.Fatalf("job in the write transaction: %+v", doc.Job)
		}
		for range 3 {
			during := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
			if *during.Index.LastRun != *before.Index.LastRun {
				t.Fatalf("status during the run names %+v, want the previous run %+v", during.Index.LastRun, before.Index.LastRun)
			}
			if during.Freshness.State != stateStale {
				t.Fatalf("freshness during the run: %+v", during.Freshness)
			}
			if n := endpointCount(t, get(t, opt, http.MethodGet, "/api/v0/api/endpoints?scope=all", nil)); n != endpoints {
				t.Fatalf("API section during the run: %d endpoints, want %d", n, endpoints)
			}
		}
		close(g.release)
		done := h.wait()
		if done.Job.State != jobCommitted {
			t.Fatalf("job %+v", done.Job)
		}
		after := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if after.Index.LastRun.FinishedAt != done.Job.Run.FinishedAt || after.Freshness.State != stateFresh {
			t.Fatalf("after: %+v %+v", after.Index.LastRun, after.Freshness)
		}
	})
}

// lostLocker hands out leases whose Check fails, as after a terminated
// Postgres session.
type lostLocker struct{ store.IndexLocker }

func (l lostLocker) Acquire(ctx context.Context, wait time.Duration) (store.IndexLease, error) {
	lease, err := l.IndexLocker.Acquire(ctx, wait)
	if err != nil {
		return nil, err
	}
	return lostLease{lease}, nil
}

type lostLease struct{ store.IndexLease }

func (lostLease) Check(context.Context) error { return errors.New("session gone") }

// TestIndexRunFailures: each failure ends the job as failed with its code,
// keeps the previous run (status still names it) and frees the lock.
func TestIndexRunFailures(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		before := decodeStatus(t, get(t, testOptions(root, rb.read), http.MethodGet, "/api/v0/status", nil))
		for _, tc := range []struct {
			name string
			code string
			edit func(*Options)
		}{
			{"store", "store_error", func(o *Options) {
				o.OpenWriter = func(context.Context) (store.Store, error) { return nil, errors.New("cannot open") }
			}},
			{"index", "index_failed", func(o *Options) {
				o.OpenWriter = gatedOpener(rb.open, func(s store.Store) store.Store { return &gatedStore{Store: s, fail: errors.New("cache broke")} })
			}},
			{"lock lost", "index_lock_lost", func(o *Options) { o.Locker = lostLocker{o.Locker} }},
			{"panic", "internal", func(o *Options) {
				o.OpenWriter = gatedOpener(rb.open, func(s store.Store) store.Store { return &gatedStore{Store: s, panicking: true} })
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				opt := runOptions(root, rb)
				defer opt.Locker.Close()
				tc.edit(&opt)
				h := newRunHarness(t, opt)
				// Deferred as well as registered for cleanup: a failed check must
				// stop a gated job before the backend's deferred teardown runs.
				defer h.runs.stop()
				decodeRun(t, h.post(), http.StatusAccepted)
				done := h.wait()
				if done.Job.State != jobFailed || done.Job.Error.Code != tc.code || done.Job.Error.Message == "" {
					t.Fatalf("job %+v error %+v", done.Job, done.Job.Error)
				}
				if done.Lock.State != lockFree {
					t.Fatalf("lock after failure: %+v", done.Lock)
				}
				after := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
				if *after.Index.LastRun != *before.Index.LastRun {
					t.Fatalf("status after a failed run names %+v, want %+v", after.Index.LastRun, before.Index.LastRun)
				}
			})
		}
	})
}

// TestIndexRunOutcomeBeforeRelease pauses a failing job between recording
// its outcome and releasing the lease (T016j review): a document in that
// window shows the failure with the lock still held, never a free lock
// next to a job that has not ended; after the release, free and failed.
func TestIndexRunOutcomeBeforeRelease(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		opt.OpenWriter = func(context.Context) (store.Store, error) { return nil, errors.New("cannot open") }
		h := newRunHarness(t, opt)
		defer h.runs.stop()
		reached, release := make(chan struct{}), make(chan struct{})
		h.runs.beforeRelease = func() {
			close(reached)
			<-release
		}
		decodeRun(t, h.post(), http.StatusAccepted)
		<-reached
		doc := h.get()
		checkJobInvariants(t, doc)
		if doc.Lock.State != lockHeldHere || doc.Job.State != jobFailed || doc.Job.EndedAt == nil {
			t.Fatalf("in the release window: lock %+v, job %+v", doc.Lock, doc.Job)
		}
		close(release)
		deadline := time.Now().Add(10 * time.Second)
		for {
			doc = h.get()
			checkJobInvariants(t, doc)
			if doc.Lock.State == lockFree {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("lock not released: %+v", doc.Lock)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if doc.Job.State != jobFailed || doc.Job.Error.Code != "store_error" {
			t.Fatalf("after the release: %+v", doc.Job)
		}
	})
}

// TestIndexRunUnsupportedAfterExternalSnapshot refuses to re-index a
// repository whose latest run came from an external indexer.
func TestIndexRunUnsupportedAfterExternalSnapshot(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		data, err := os.ReadFile("../../testdata/fixtures/external-indexer/snapshot.golden.ndjson")
		if err != nil {
			t.Fatal(err)
		}
		w, err := rb.open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := ingest.Stream(context.Background(), bytes.NewReader(data), w); err != nil {
			t.Fatal(err)
		}
		w.Close()
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		h := newRunHarness(t, opt)
		// Deferred as well as registered for cleanup: a failed check must
		// stop a gated job before the backend's deferred teardown runs.
		defer h.runs.stop()
		rec := h.post()
		if code := runErrorCode(t, rec, http.StatusConflict); code != "reindex_unsupported" || !strings.Contains(rec.Body.String(), "--indexer") {
			t.Fatalf("code %q: %s", code, rec.Body.String())
		}
		if doc := h.get(); doc.Job != nil || doc.Lock.State != lockFree {
			t.Fatalf("refused run left %+v", doc)
		}
	})
}

// TestIndexRunShutdown stops a real server while its job runs: the job is
// cancelled and rolled back, the store keeps the previous run, the lock is
// free, and Serve returns only after the job ended.
func TestIndexRunShutdown(t *testing.T) {
	root := fixtureRoot(t)
	runBackends(t, root, func(t *testing.T, rb runBackend) {
		before, err := rb.read.Runs(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		opt := runOptions(root, rb)
		defer opt.Locker.Close()
		g := newGate()
		opt.OpenWriter = gatedOpener(rb.open, func(s store.Store) store.Store { return &gatedStore{Store: s, cache: g} })
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		srv, err := Listen(ctx, "127.0.0.1:0", opt)
		if err != nil {
			t.Fatal(err)
		}
		served := make(chan error, 1)
		go func() { served <- srv.Serve(ctx) }()
		client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
		req, err := http.NewRequest(http.MethodPost, srv.URL()+"api/v0/index/run", strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("POST %d", resp.StatusCode)
		}
		<-g.reached
		cancel()
		select {
		case err := <-served:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("Serve did not return")
		}
		// Serve has returned, so the job must have ended already.
		doc := srv.runs.document(context.Background())
		if doc.Job == nil || doc.Job.State != jobCancelled || doc.Job.EndedAt == nil || doc.Job.Error != nil {
			t.Fatalf("job after shutdown: %+v", doc.Job)
		}
		if doc.Lock.State != lockFree {
			t.Fatalf("lock after shutdown: %+v", doc.Lock)
		}
		after, err := rb.read.Runs(context.Background(), "fixture")
		if err != nil || !slices.Equal(after, before) {
			t.Fatalf("runs changed by a cancelled job: %+v, %v", after, err)
		}
		// A stopped runner refuses new jobs.
		u, err := url.Parse(srv.URL())
		if err != nil {
			t.Fatal(err)
		}
		stopped := (&runHarness{t: t, handler: srv.http.Handler}).do(http.MethodPost, indexRunPath, "{}", map[string]string{"Content-Type": "application/json", "Host": u.Host})
		if code := runErrorCode(t, stopped, http.StatusServiceUnavailable); code != "unavailable" {
			t.Fatalf("code %q", code)
		}
	})
}

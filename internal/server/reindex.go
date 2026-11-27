package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/storeloc"
)

// indexRunPath is the only path that accepts POST (ADR 0052).
const indexRunPath = "/api/v0/index/run"

// maxRunBody bounds the POST body; v1 takes no options, only {}.
const maxRunBody = 1 << 10

// Lock states of GET /api/v0/index/run. Clients treat any other value as
// unknown, never as free.
const (
	lockFree          = "free"
	lockHeldHere      = "held_here"
	lockHeldElsewhere = "held_elsewhere"
	lockUnknown       = "unknown"
)

// Job states.
const (
	jobRunning   = "running"
	jobCommitted = "committed"
	jobFailed    = "failed"
	jobCancelled = "cancelled"
)

// indexRunResponse is the body of GET /api/v0/index/run and of a 202 POST
// answer, in the documented field order (docs/cli/serve.md).
type indexRunResponse struct {
	APIVersion int `json:"api_version"`
	// Repository is empty in project mode, where Project names the
	// project the job re-indexes. It is always present: the web UI reads
	// it as a string (T038).
	Repository string  `json:"repository"`
	Project    string  `json:"project,omitempty"`
	Lock       lockDoc `json:"lock"`
	Job        *jobDoc `json:"job"`
}

type lockDoc struct {
	State string  `json:"state"`
	Error *string `json:"error"`
}

// jobDoc is the server's latest job. Pointer fields are replaced, never
// mutated, so a shallow copy taken under the runner's lock is a snapshot.
type jobDoc struct {
	ID        string       `json:"id"`
	State     string       `json:"state"`
	StartedAt string       `json:"started_at"`
	EndedAt   *string      `json:"ended_at"`
	Phase     *string      `json:"phase"`
	Progress  *progressDoc `json:"progress"`
	Replaces  *runRef      `json:"replaces"`
	Run       *runRef      `json:"run"`
	Error     *errorBody   `json:"error"`
	// Member (project mode) is the member being indexed while the job
	// runs; absent during the link pass and after the job ended.
	Member *string `json:"member,omitempty"`
	// Project (project mode) is each member's outcome and the link pass.
	Project *projectJobDoc `json:"project,omitempty"`
}

// projectJobDoc is a project re-index: members in manifest order, then the
// link pass. Like jobDoc it is replaced, never mutated.
type projectJobDoc struct {
	Members  []memberJobDoc `json:"members"`
	LinkPass *passJobDoc    `json:"link_pass"`
}

// Member states of a project re-index.
const (
	memberPending    = "pending"
	memberRunning    = "running"
	memberCommitted  = "committed"
	memberFailed     = "failed"
	memberNoCheckout = "no_checkout"
	// memberLinkedOnly: a link-pass-only job (after a manifest edit that
	// changed no member) links the member's stored index without indexing.
	memberLinkedOnly = "linked_from_store"
)

type memberJobDoc struct {
	Repository string  `json:"repository"`
	State      string  `json:"state"`
	Run        *runRef `json:"run"`
	Error      *string `json:"error"`
}

// passJobDoc is the stored link pass of a project re-index.
type passJobDoc struct {
	Pass    int64 `json:"pass"`
	Links   int   `json:"links"`
	Added   int   `json:"added"`
	Removed int   `json:"removed"`
	Changed int   `json:"changed"`
}

// progressDoc is set only when both numbers come from the run itself.
type progressDoc struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Unit  string `json:"unit"`
}

type runRef struct {
	ID         string `json:"id,omitempty"`
	FinishedAt string `json:"finished_at"`
}

// runner owns the server's index runs: at most one job at a time, started
// by POST and executed in a goroutine whose context derives from the
// runner, not from the request, so it outlives the POST and ends only with
// the server.
type runner struct {
	opt Options
	ctx context.Context
	// cancel stops a running job; stop calls it at shutdown.
	cancel context.CancelFunc
	// start serializes POSTs, so two clicks in this process cannot race
	// between "no job runs" and the lock.
	start sync.Mutex
	// mu guards job and stopped.
	mu      sync.Mutex
	job     *jobDoc
	stopped bool
	// jobs counts running job goroutines; stop waits for them.
	jobs sync.WaitGroup
	// beforeRelease, when set (tests only), runs after the job's outcome
	// is recorded and before its lease is released.
	beforeRelease func()
	// onEnd, when set (a workspace server's answer cache), runs once a
	// job's outcome is recorded: a run may have committed.
	onEnd func()
}

// running reports whether a job runs now.
func (rn *runner) running() bool {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	return rn.job != nil && rn.job.EndedAt == nil
}

func newRunner(opt Options) *runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &runner{opt: opt, ctx: ctx, cancel: cancel}
}

// whileIdle runs fn while no re-index job runs and none can start (a POST
// waits for fn), or refuses with 409 index_running. A manifest rename uses
// it so a job reading the old manifest cannot index the old name while its
// rows are removed. A nil runner has no jobs.
func (rn *runner) whileIdle(fn func() error) error {
	if rn == nil {
		return fn()
	}
	rn.start.Lock()
	defer rn.start.Unlock()
	rn.mu.Lock()
	running := rn.job != nil && rn.job.EndedAt == nil
	rn.mu.Unlock()
	if running {
		return &apiError{http.StatusConflict, "index_running", "a re-index is running; rename the member when it has finished"}
	}
	return fn()
}

// ServeHTTP answers GET and HEAD with the run document and POST with a new
// run; NewHandler admits no other method here.
func (rn *runner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		rn.post(w, r)
		return
	}
	writeJSON(w, http.StatusOK, rn.document(r.Context()))
}

// document reads the lock state and the latest job. It never walks the
// repository, so it is cheap enough to poll every second.
//
// Both are read under mu: a job records its outcome under mu before it
// releases the lease, so a document can never pair a free lock with a job
// that has not ended (which a client would read as "nothing runs" and hide
// the outcome until its next slow poll).
func (rn *runner) document(ctx context.Context) indexRunResponse {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	out := indexRunResponse{APIVersion: 0, Repository: rn.opt.Repository, Lock: rn.lockState(ctx)}
	if rn.opt.Project != nil {
		out.Repository, out.Project = "", rn.opt.Project.current().Name
		// The job takes each member's lock and then the project lock
		// through lockers of its own, which a probe of Options.Locker
		// would see as another holder's.
		if rn.job != nil && rn.job.EndedAt == nil {
			out.Lock = lockDoc{State: lockHeldHere}
		}
	}
	if rn.job != nil {
		job := *rn.job
		out.Job = &job
	}
	return out
}

// latestJob is a copy of the latest job from memory, nil when none ran.
// Unlike document it never probes the lock, so the workspace list can call
// it for every open index on every poll without touching the disk (T038
// review: a probe of a moved folder's lock recreated the folder).
func (rn *runner) latestJob() *jobDoc {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if rn.job == nil {
		return nil
	}
	job := *rn.job
	return &job
}

// lockState asks the locker. Without a locker, or when the probe fails,
// the state is unknown with the reason: never assumed free.
func (rn *runner) lockState(ctx context.Context) lockDoc {
	unknown := func(message string) lockDoc { return lockDoc{State: lockUnknown, Error: &message} }
	if !rn.available() {
		return unknown("re-index is not available: this server has no index lock or writer store")
	}
	state, err := rn.opt.Locker.State(ctx)
	if err != nil {
		return unknown(err.Error())
	}
	switch state {
	case store.LockFree:
		return lockDoc{State: lockFree}
	case store.LockHeldHere:
		return lockDoc{State: lockHeldHere}
	case store.LockHeldElsewhere:
		return lockDoc{State: lockHeldElsewhere}
	}
	return unknown(fmt.Sprintf("unrecognised lock state %q", state))
}

// available reports whether this server can re-index: a lock and a writer
// store, or in project mode the project lock and the project re-index.
func (rn *runner) available() bool {
	if rn.opt.Project != nil {
		return rn.opt.Locker != nil && rn.opt.Project.Reindex != nil
	}
	return rn.opt.Locker != nil && rn.opt.OpenWriter != nil
}

// post starts a run. The security pipeline (Host, method, Sec-Fetch-Site,
// Origin, CrossOriginProtection) already ran in NewHandler.
func (rn *runner) post(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "POST /api/v0/index/run needs Content-Type: application/json")
		return
	}
	linkPassOnly := false
	if rn.opt.Project != nil {
		var ok bool
		if linkPassOnly, ok = projectRunBody(http.MaxBytesReader(w, r.Body, maxRunBody)); !ok {
			writeError(w, http.StatusBadRequest, "bad_request", `the body must be {} or, under --project, {"link_pass_only": true}`)
			return
		}
	} else if !emptyObject(http.MaxBytesReader(w, r.Body, maxRunBody)) {
		writeError(w, http.StatusBadRequest, "bad_request", "the body must be exactly {}: this endpoint takes no options")
		return
	}
	if rn.opt.Project != nil {
		if _, present := r.URL.Query()["repository"]; present {
			writeError(w, http.StatusBadRequest, "bad_request", "a re-index under --project re-indexes the whole project (each member, then the link pass); leave out repository")
			return
		}
	}
	ctx := r.Context()
	if err := rn.begin(ctx, linkPassOnly); err != nil {
		var ae *apiError
		if errors.As(err, &ae) {
			writeError(w, ae.status, ae.code, ae.message)
			return
		}
		logFailure(rn.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.Header().Set("Location", indexRunPath)
	writeJSON(w, http.StatusAccepted, rn.document(ctx))
}

// begin starts a job (POST /api/v0/index/run, or adding a folder to the
// workspace) and returns once it runs, or the refusal: an *apiError, or
// a store read error.
func (rn *runner) begin(ctx context.Context, linkPassOnly bool) error {
	if !rn.available() {
		return &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", "re-index is not available: this server has no index lock or writer store"}
	}
	rn.start.Lock()
	defer rn.start.Unlock()
	rn.mu.Lock()
	stopped, running := rn.stopped, rn.job != nil && rn.job.EndedAt == nil
	rn.mu.Unlock()
	if stopped || errors.Is(context.Cause(ctx), errStopping) {
		return &apiError{http.StatusServiceUnavailable, "unavailable", "the server is stopping"}
	}
	if running {
		return &apiError{http.StatusConflict, "index_locked", "this server's re-index is still running"}
	}
	if rn.opt.Project != nil {
		return rn.beginProject(ctx, linkPassOnly)
	}
	runs, err := rn.opt.Store.Runs(ctx, rn.opt.Repository)
	if err != nil {
		return fmt.Errorf("reading runs: %w", err)
	}
	last := latestStoreRun(runs)
	if last != nil && last.Header.Indexer.Name != "graphvein" {
		return &apiError{http.StatusConflict, "reindex_unsupported", fmt.Sprintf("the latest run was produced by the external indexer %q; re-index it with graphvein index --indexer", last.Header.Indexer.Name)}
	}
	lease, err := rn.opt.Locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return &apiError{http.StatusConflict, "index_locked", "another process holds the index lock for this store (graphvein index or another graphvein serve); try again when it has finished"}
	}
	if errors.Is(err, storeloc.ErrMoved) {
		// The store moved while this server had it open (WS-81): the run
		// stops instead of creating an index at the old place.
		return &apiError{http.StatusConflict, "store_moved", err.Error()}
	}
	if err != nil {
		return &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", err.Error()}
	}
	job := &jobDoc{ID: rand.Text(), State: jobRunning, StartedAt: rn.timestamp(), Phase: newPtr(indexrun.PhaseStarting)}
	if last != nil {
		job.Replaces = &runRef{FinishedAt: canonicalTime(last.FinishedAt)}
	}
	rn.mu.Lock()
	rn.job = job
	rn.mu.Unlock()
	rn.jobs.Add(1)
	go rn.execute(job, lease)
	return nil
}

// emptyObject reports whether r holds exactly one JSON object with no
// members, and nothing after it but white space.
func emptyObject(r io.Reader) bool {
	dec := json.NewDecoder(r)
	var body map[string]json.RawMessage
	if err := dec.Decode(&body); err != nil || body == nil || len(body) != 0 {
		return false
	}
	return dec.Decode(&struct{}{}) == io.EOF
}

// projectRunBody reads a project re-index body: {} re-indexes every member
// checkout and links; {"link_pass_only": true} only links the stored
// indexes (offered after a manifest edit that changed no member, T022
// B10). Anything else is refused.
func projectRunBody(r io.Reader) (linkPassOnly bool, ok bool) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var body struct {
		LinkPassOnly *bool `json:"link_pass_only"`
	}
	if err := dec.Decode(&body); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return false, false
	}
	return body.LinkPassOnly != nil && *body.LinkPassOnly, true
}

// execute runs the job and records its outcome. It records the outcome
// (state, error, ended_at) before it releases the lease, which is released
// only after indexrun.Run closed the writer store, and recovers a panic as
// the internal error, so a bug fails the job instead of the server.
func (rn *runner) execute(job *jobDoc, lease store.IndexLease) {
	defer rn.jobs.Done()
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = &panicError{value: v}
			}
		}()
		_, err = indexrun.Run(rn.ctx, indexrun.Options{
			Root:        rn.opt.Root,
			Repository:  rn.opt.Repository,
			Lease:       lease,
			OpenStore:   rn.opt.OpenWriter,
			Parser:      parse.Options{Timeout: indexrun.DefaultTimeout},
			MaxFileSize: indexrun.DefaultMaxFileSize,
			Progress:    func(p indexrun.Progress) { rn.progress(job, p) },
			OnCommit:    func(run store.Run) { rn.committed(job, run) },
			SnapshotDir: rn.opt.StoreDir,
			StoreName:   rn.opt.StoreName,
		})
	}()
	rn.finish(job, err)
	if rn.onEnd != nil {
		rn.onEnd()
	}
	if rn.beforeRelease != nil {
		rn.beforeRelease()
	}
	if releaseErr := lease.Release(); releaseErr != nil {
		rn.opt.logger().Warn("releasing the index lock failed", "error", releaseErr)
	}
}

// panicError is a panic recovered in the job goroutine.
type panicError struct{ value any }

func (p *panicError) Error() string { return fmt.Sprintf("internal error: %v", p.value) }

// progress records a phase report. A fraction is kept only when the run
// counted both numbers; any other report clears it.
func (rn *runner) progress(job *jobDoc, p indexrun.Progress) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if job.EndedAt != nil {
		return
	}
	job.Phase = newPtr(p.Phase)
	job.Progress = nil
	if p.Counted && job.State == jobRunning {
		job.Progress = &progressDoc{Done: p.Done, Total: p.Total, Unit: p.Unit}
	}
}

// committed records the committed run as soon as the transaction
// committed; the job keeps the lock (phase finishing) while the writer
// store closes.
func (rn *runner) committed(job *jobDoc, run store.Run) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	job.State = jobCommitted
	job.Progress = nil
	job.Run = &runRef{ID: run.ID, FinishedAt: canonicalTime(run.FinishedAt)}
}

// finish records the outcome; execute releases the lock right after.
func (rn *runner) finish(job *jobDoc, err error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	job.EndedAt = newPtr(rn.timestamp())
	job.Phase = nil
	job.Progress = nil
	if job.State == jobCommitted {
		if err != nil {
			rn.opt.logger().Warn("re-index committed but reported an error", "error", err)
		}
		rn.opt.logger().Info("re-index committed", "run", job.Run.ID)
		return
	}
	var panicked *panicError
	var storeErr *indexrun.StoreError
	switch {
	case err == nil:
		// Run returned without committing and without an error; it cannot,
		// but the state must not claim success.
		job.State, job.Error = jobFailed, &errorBody{Code: "internal", Message: "the run ended without a commit"}
	case errors.As(err, &panicked):
		job.State, job.Error = jobFailed, &errorBody{Code: "internal", Message: err.Error()}
	case rn.ctx.Err() != nil:
		job.State = jobCancelled
	case errors.Is(err, indexrun.ErrLockLost):
		job.State, job.Error = jobFailed, &errorBody{Code: "index_lock_lost", Message: err.Error()}
	case errors.As(err, &storeErr):
		job.State, job.Error = jobFailed, &errorBody{Code: "store_error", Message: err.Error()}
	default:
		job.State, job.Error = jobFailed, &errorBody{Code: "index_failed", Message: err.Error()}
	}
	rn.opt.logger().Warn("re-index did not commit", "state", job.State, "error", err)
}

// stop cancels a running job and waits until it rolled back, closed the
// writer store and released the lock. Serve calls it after the last
// request handler returned, so no POST can start another job.
func (rn *runner) stop() {
	rn.mu.Lock()
	rn.stopped = true
	running := rn.job != nil && rn.job.EndedAt == nil
	rn.mu.Unlock()
	if running {
		rn.opt.logger().Info("stopping the running re-index")
	}
	rn.cancel()
	rn.jobs.Wait()
}

func (rn *runner) timestamp() string {
	return rn.opt.now().UTC().Format(time.RFC3339Nano)
}

// canonicalTime formats a stored finish time the way GET /api/v0/status
// reports last_run.finished_at, so clients can compare the two.
func canonicalTime(s string) string {
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func newPtr[T any](v T) *T { return &v }

// beginProject starts a project re-index (serve --project). Members whose
// latest run came from an external indexer refuse it, as for one
// repository. The project lock is probed, not taken: the job takes each
// member's lock and then the project lock in turn, as graphvein index
// --project does, and records a member it could not lock as failed.
func (rn *runner) beginProject(ctx context.Context, linkPassOnly bool) error {
	project := rn.opt.Project.current()
	for _, m := range project.Members {
		if !m.Checkout || linkPassOnly {
			continue
		}
		runs, err := rn.opt.Store.Runs(ctx, m.Repository)
		if err != nil {
			return fmt.Errorf("reading runs of %s: %w", m.Repository, err)
		}
		if last := latestStoreRun(runs); last != nil && last.Header.Indexer.Name != "graphvein" {
			return &apiError{http.StatusConflict, "reindex_unsupported", fmt.Sprintf("the latest run of member %s was produced by the external indexer %q; re-index it with graphvein index --indexer", m.Repository, last.Header.Indexer.Name)}
		}
	}
	state, err := rn.opt.Locker.State(ctx)
	if errors.Is(err, storeloc.ErrMoved) {
		// The store moved while this server had it open (WS-81): the run
		// stops instead of creating an index at the old place.
		return &apiError{http.StatusConflict, "store_moved", err.Error()}
	}
	if err != nil {
		return &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", err.Error()}
	}
	if state == store.LockHeldElsewhere {
		return &apiError{http.StatusConflict, "index_locked", "another process holds the project lock (graphvein index or another graphvein serve); try again when it has finished"}
	}
	doc := &projectJobDoc{Members: []memberJobDoc{}}
	for _, m := range project.Members {
		st := memberPending
		switch {
		case !m.Checkout:
			st = memberNoCheckout
		case linkPassOnly:
			st = memberLinkedOnly
		}
		doc.Members = append(doc.Members, memberJobDoc{Repository: m.Repository, State: st})
	}
	job := &jobDoc{ID: rand.Text(), State: jobRunning, StartedAt: rn.timestamp(), Phase: newPtr(indexrun.PhaseStarting), Project: doc}
	rn.mu.Lock()
	rn.job = job
	rn.mu.Unlock()
	rn.jobs.Add(1)
	go rn.executeProject(job, project, linkPassOnly)
	return nil
}

// executeProject runs a project re-index and records its outcome. The
// locks are taken and released inside Reindex, so nothing is held once it
// returns.
func (rn *runner) executeProject(job *jobDoc, project config.Project, linkPassOnly bool) {
	defer rn.jobs.Done()
	var result ProjectRun
	var panicked error
	func() {
		defer func() {
			if v := recover(); v != nil {
				panicked = &panicError{value: v}
			}
		}()
		result = rn.opt.Project.Reindex(rn.ctx, project, linkPassOnly, func(repository string, p indexrun.Progress) { rn.projectProgress(job, repository, p) })
	}()
	rn.finishProject(job, result, panicked)
	if rn.onEnd != nil {
		rn.onEnd()
	}
	if rn.beforeRelease != nil {
		rn.beforeRelease()
	}
}

// projectProgress records which member is being indexed (empty: the link
// pass) and its phase.
func (rn *runner) projectProgress(job *jobDoc, repository string, p indexrun.Progress) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	if job.EndedAt != nil {
		return
	}
	job.Phase, job.Progress, job.Member = newPtr(p.Phase), nil, nil
	if repository == "" {
		job.Phase = newPtr(phaseLinking)
	} else {
		job.Member = newPtr(repository)
	}
	if p.Counted {
		job.Progress = &progressDoc{Done: p.Done, Total: p.Total, Unit: p.Unit}
	}
	doc := &projectJobDoc{Members: append([]memberJobDoc{}, job.Project.Members...), LinkPass: job.Project.LinkPass}
	for i := range doc.Members {
		if doc.Members[i].Repository == repository && doc.Members[i].State == memberPending {
			doc.Members[i].State = memberRunning
		}
	}
	job.Project = doc
}

// finishProject records a project re-index's outcome: committed only when
// every member checkout committed and the link pass was stored.
func (rn *runner) finishProject(job *jobDoc, result ProjectRun, panicked error) {
	rn.mu.Lock()
	defer rn.mu.Unlock()
	job.EndedAt = newPtr(rn.timestamp())
	job.Phase, job.Progress, job.Member = nil, nil, nil
	doc := &projectJobDoc{Members: append([]memberJobDoc{}, job.Project.Members...)}
	byRepo := map[string]ProjectMemberRun{}
	for _, m := range result.Members {
		byRepo[m.Repository] = m
	}
	var failed []string
	for i := range doc.Members {
		m, ok := byRepo[doc.Members[i].Repository]
		switch {
		case !ok || !m.Indexed:
			if st := doc.Members[i].State; st != memberNoCheckout && st != memberLinkedOnly {
				doc.Members[i].State = memberFailed
			}
		case m.Error != nil:
			doc.Members[i].State, doc.Members[i].Error = memberFailed, newPtr(m.Error.Error())
			failed = append(failed, m.Repository+": "+m.Error.Error())
		case m.Run != nil:
			doc.Members[i].State = memberCommitted
			doc.Members[i].Run = &runRef{ID: m.Run.ID, FinishedAt: canonicalTime(m.Run.FinishedAt)}
		default:
			doc.Members[i].State = memberFailed
		}
	}
	if result.Pass != nil {
		p := result.Pass
		doc.LinkPass = &passJobDoc{Pass: p.Seq, Links: len(p.Links), Added: p.Added, Removed: p.Removed, Changed: p.Changed}
	}
	job.Project = doc
	switch {
	case panicked != nil:
		job.State, job.Error = jobFailed, &errorBody{Code: "internal", Message: panicked.Error()}
	case rn.ctx.Err() != nil:
		job.State = jobCancelled
	case result.PassError != nil:
		job.State, job.Error = jobFailed, &errorBody{Code: "link_pass_failed", Message: result.PassError.Error()}
	case len(failed) > 0:
		job.State, job.Error = jobFailed, &errorBody{Code: "index_failed", Message: "some members failed to index (" + strings.Join(failed, "; ") + "); the link pass linked their previous index and marks their links stale"}
	default:
		job.State = jobCommitted
		rn.opt.logger().Info("project re-index committed", "pass", doc.LinkPass.Pass)
		return
	}
	rn.opt.logger().Warn("project re-index did not commit", "state", job.State)
}

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the change endpoints of ADR 0056 under /api/v0/changes/:
// GET bases, summary, items, impact and refs; POST pins, pins/delete and
// refs. Every read goes through internal/changes; nothing here interprets a
// change set.

const (
	changesBasesPath      = "/api/v0/changes/bases"
	changesSummaryPath    = "/api/v0/changes/summary"
	changesItemsPath      = "/api/v0/changes/items"
	changesImpactPath     = "/api/v0/changes/impact"
	changesPinsPath       = "/api/v0/changes/pins"
	changesPinsDeletePath = "/api/v0/changes/pins/delete"
	changesRefsPath       = "/api/v0/changes/refs"
)

// postPaths are the paths that accept POST (ADR 0052's rules apply to
// each). /api/v0/changes/refs also answers GET.
var postPaths = map[string]bool{
	indexRunPath: true, changesPinsPath: true, changesPinsDeletePath: true, changesRefsPath: true,
	projectManifestPath:   true,
	tryItEnvironmentsPath: true, tryItSelectPath: true, tryItAuthPath: true, tryItPreparePath: true, tryItSendPath: true,
	explorerValuesPath: true, configPath: true,
	workspaceCheckPath: true, workspaceAddPath: true, workspaceRemovePath: true,
	storeCheckPath: true, storeMovePath: true, storeDefaultPath: true,
}

// getAlso are POST paths that answer GET and HEAD too.
var getAlso = map[string]bool{indexRunPath: true, changesRefsPath: true, projectManifestPath: true, tryItEnvironmentsPath: true, explorerValuesPath: true, configPath: true}

// comparisonCache is how many computed comparisons the server keeps, keyed
// by base and head, so paging does not recompute (ADR 0056).
const comparisonCache = 4

// maxChangesBody bounds a POST body of the change endpoints.
const maxChangesBody = 4 << 10

// changesAPI serves the change endpoints. It keeps the last computed
// comparisons and the ref indexing job.
type changesAPI struct {
	opt Options
	mu  sync.Mutex
	// cache holds the most recent comparisons, newest first.
	cache []cachedComparison
	refs  *refRunner
	// state caches the stored file hashes and the freshness check per
	// index state (changes_fresh.go).
	state changesState
	// background is the parent of every comparison's preparation
	// (changes.Comparison.Prepare); shutdown cancels it and waits on
	// preparing, so nothing reads the store once Serve returns. stopped
	// (under mu) keeps a late request from starting another one.
	background     context.Context
	stopBackground context.CancelFunc
	preparing      sync.WaitGroup
	stopped        bool
}

type cachedComparison struct {
	key string
	// run names the latest run the comparison was computed at.
	run string
	cmp *changes.Comparison
	// prepareCtx is the context of the comparison's background
	// preparation until startPreparations starts it; cancel stops it.
	prepareCtx context.Context
	cancel     context.CancelFunc
}

func newChangesAPI(opt Options) *changesAPI {
	ctx, cancel := context.WithCancel(context.Background())
	return &changesAPI{opt: opt, refs: newRefRunner(opt), background: ctx, stopBackground: cancel}
}

// changesSet is the change endpoints of every repository a server serves
// (one, or each member with a checkout in project mode).
type changesSet []*changesAPI

// shutdowner stops what a route set runs in the background.
type shutdowner interface{ shutdown() }

// busy reports whether any member's ref indexing job runs.
func (cs changesSet) busy() bool {
	for _, a := range cs {
		if a.refs.running() {
			return true
		}
	}
	return false
}

// shutdown shuts every member's change endpoints down.
func (cs changesSet) shutdown() {
	for _, a := range cs {
		a.shutdown()
	}
}

// shutdown stops the comparisons' background preparation and a running ref
// job, and waits for both.
func (a *changesAPI) shutdown() {
	a.mu.Lock()
	a.stopped = true
	a.stopBackground()
	a.mu.Unlock()
	a.preparing.Wait()
	a.refs.shutdown()
}

// startPreparations builds the per-graph indexes of the comparisons
// computed since the last call (blind spots, and I8's candidates when an
// item is a table or column), in the background, so the first impact
// request on one does not pay for reading every file and symbol node. serveRead calls
// it after writing its response: started earlier, the preparation competed
// with the rest of the summary request (the freshness walk) and slowed it.
// A traversal that starts meanwhile waits for the preparation.
func (a *changesAPI) startPreparations() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	for i := range a.cache {
		c := &a.cache[i]
		if c.prepareCtx == nil {
			continue
		}
		ctx, cmp := c.prepareCtx, c.cmp
		c.prepareCtx = nil
		a.preparing.Add(1)
		go func() {
			defer a.preparing.Done()
			if err := cmp.Prepare(ctx, a.opt.Store); err != nil && ctx.Err() == nil {
				// A traversal builds the index itself and reports the error.
				a.opt.logger().Warn("preparing a comparison failed", "error", err)
			}
		}()
	}
}

// routes registers the change endpoints.
func (a *changesAPI) routes(routes map[string]http.Handler) {
	routes[changesBasesPath] = http.HandlerFunc(a.bases)
	routes[changesSummaryPath] = http.HandlerFunc(a.summary)
	routes[changesItemsPath] = http.HandlerFunc(a.items)
	routes[changesImpactPath] = http.HandlerFunc(a.impact)
	routes[changesPinsPath] = http.HandlerFunc(a.pin)
	routes[changesPinsDeletePath] = http.HandlerFunc(a.unpin)
	routes[changesRefsPath] = a.refs
}

// changesHeader opens every change response: the repository, the index
// as /api/v0/status reports it and freshness (ADR 0056).
type changesHeader struct {
	APIVersion int             `json:"api_version"`
	Repository string          `json:"repository"`
	Index      *lastRun        `json:"index"`
	Freshness  freshnessStatus `json:"freshness"`
}

func (a *changesAPI) header(ctx context.Context, runs []store.Run) changesHeader {
	return changesHeader{APIVersion: 0, Repository: a.opt.Repository, Index: latestRun(runs), Freshness: a.state.freshness(ctx, a.opt, runs)}
}

// changesError maps the errors of internal/changes, the store and gitref to
// the API's codes.
func changesError(err error) (int, string) {
	var notIndexed *changes.RefNotIndexedError
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		return ae.status, ae.code
	case errors.Is(err, errIndexChanged), errors.Is(err, changes.ErrCursorMismatch):
		return http.StatusConflict, "index_changed"
	case errors.Is(err, store.ErrBaseExpired):
		return http.StatusGone, "base_expired"
	case errors.Is(err, store.ErrBaseUnknown), errors.Is(err, store.ErrNoJournal), errors.Is(err, changes.ErrNoPrevious):
		return http.StatusNotFound, "base_unknown"
	case errors.Is(err, changes.ErrReindexNeeded):
		return http.StatusConflict, "reindex_needed"
	case errors.As(err, &notIndexed), errors.Is(err, store.ErrRefUnknown), errors.Is(err, gitref.ErrRefUnknown):
		return http.StatusNotFound, "ref_unknown"
	case errors.Is(err, gitref.ErrObjectsMissing):
		return http.StatusUnprocessableEntity, "ref_objects_missing"
	case errors.Is(err, gitref.ErrGitUnavailable):
		return http.StatusServiceUnavailable, "git_unavailable"
	case errors.Is(err, store.ErrPinLimit):
		return http.StatusConflict, "pin_limit"
	case errors.Is(err, store.ErrPinExists):
		return http.StatusConflict, "pin_exists"
	case errors.Is(err, store.ErrPinUnknown):
		return http.StatusNotFound, "pin_unknown"
	case errors.Is(err, store.ErrIndexLocked):
		return http.StatusConflict, "index_locked"
	case errors.Is(err, changes.ErrUnknownItem):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, changes.ErrNotSeed), errors.Is(err, changes.ErrCursorInvalid):
		return http.StatusBadRequest, "bad_request"
	}
	return http.StatusInternalServerError, "store_error"
}

func (a *changesAPI) fail(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		cancelled(w, r.Context())
		return
	}
	status, code := changesError(err)
	if status == http.StatusInternalServerError {
		logFailure(a.opt.logger(), r, err)
	}
	writeError(w, status, code, err.Error())
}

// consistent runs read between two reads of the runs (ADR 0042); a run
// committed in between repeats the read once, then the request fails with
// 409 index_changed (G6).
func (a *changesAPI) consistent(ctx context.Context, read func(runs []store.Run) (any, error)) (any, error) {
	for attempt := 0; attempt < 2; attempt++ {
		before, err := a.opt.Store.Runs(ctx, a.opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading runs: %w", err)
		}
		body, err := read(before)
		if err != nil {
			return nil, err
		}
		after, err := a.opt.Store.Runs(ctx, a.opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading runs: %w", err)
		}
		if sameRuns(before, after) {
			return body, nil
		}
	}
	return nil, errIndexChanged
}

func (a *changesAPI) serveRead(w http.ResponseWriter, r *http.Request, read func(ctx context.Context, q url.Values, runs []store.Run) (any, error)) {
	q := r.URL.Query()
	defer a.startPreparations()
	body, err := a.consistent(r.Context(), func(runs []store.Run) (any, error) { return read(r.Context(), q, runs) })
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
	// Send the response before startPreparations runs.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// comparison returns the cached comparison for the request's base and the
// current head, computing it when absent. The key holds the latest run and,
// for ref bases, the resolved commits and their snapshots' runs, so a new
// run or a re-indexed ref never reuses a stale comparison.
func (a *changesAPI) comparison(ctx context.Context, q url.Values, runs []store.Run) (*changes.Comparison, error) {
	raw, _, err := param(q, "base")
	if err != nil {
		return nil, err
	}
	base, err := changes.ParseBase(raw)
	if err != nil {
		return nil, badRequest("%v", err)
	}
	run := ""
	if last := latestStoreRun(runs); last != nil {
		run = last.ID + "\x00" + last.FinishedAt
	}
	key := base.String() + "\x00" + run
	if base.Kind == changes.BasePin {
		// The key names the pin's identity and run, so a pin deleted or
		// re-pointed under the same name never answers from a stale
		// comparison (Revision 3, R3-1).
		pins, err := a.opt.Store.Pins(ctx, a.opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading pins: %w", err)
		}
		found := false
		for _, p := range pins {
			if p.Name == base.Value {
				key += "\x00" + p.ID + "\x00" + strconv.FormatInt(p.Seq, 10)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("pin %s: %w", base.Value, store.ErrBaseUnknown)
		}
	}
	if base.Kind == changes.BaseRef {
		if a.opt.noCheckout {
			return nil, a.opt.noCheckoutError("a ref base")
		}
		indexed, err := changes.IndexedCommit(ctx, a.opt.Store, a.opt.Repository)
		if err != nil {
			return nil, err
		}
		refs, err := changes.ResolveRefs(ctx, a.opt.Root, base, indexed)
		if err != nil {
			return nil, err
		}
		key += "\x00" + refs.Base + "\x00" + refs.Head + "\x00" + strconv.FormatBool(refs.HeadCommitDiffers)
		snapshots, err := a.opt.Store.RefSnapshots(ctx, a.opt.Repository)
		if err != nil {
			return nil, fmt.Errorf("reading ref snapshots: %w", err)
		}
		for _, s := range snapshots {
			key += "\x00" + s.Namespace + "=" + s.Run.RunID + "@" + s.Extractor
		}
	}
	a.mu.Lock()
	for i, c := range a.cache {
		if c.key == key {
			a.cache = append(append([]cachedComparison{c}, a.cache[:i]...), a.cache[i+1:]...)
			a.mu.Unlock()
			return c.cmp, nil
		}
	}
	a.mu.Unlock()
	cmp, err := changes.Compare(ctx, a.opt.Store, a.opt.Repository, base, changes.Options{Root: a.opt.Root, ExtractorVersion: index.Version, Schema: ChangeSchema{}})
	if err != nil {
		return nil, err
	}
	prepareCtx, cancel := context.WithCancel(a.background)
	a.mu.Lock()
	// A comparison computed at an earlier run is never asked for again
	// (its key names the run): it leaves the cache with the oldest beyond
	// comparisonCache, and its preparation stops. A request still holding
	// it builds what it needs with its own context.
	kept := []cachedComparison{{key: key, run: run, cmp: cmp, prepareCtx: prepareCtx, cancel: cancel}}
	for _, c := range a.cache {
		if c.run != run || len(kept) == comparisonCache {
			c.cancel()
			continue
		}
		kept = append(kept, c)
	}
	a.cache = kept
	a.mu.Unlock()
	return cmp, nil
}

// GET /api/v0/changes/bases

type basesResponse struct {
	changesHeader
	changes.Bases
	Retention retentionDoc `json:"retention"`
}

// retentionDoc reports the changes.* settings of graphvein.yaml.
type retentionDoc struct {
	JournalRuns     int     `json:"journal_runs"`
	JournalBudget   float64 `json:"journal_budget"`
	MaxPins         int     `json:"max_pins"`
	MaxRefSnapshots int     `json:"max_ref_snapshots"`
}

func (a *changesAPI) bases(w http.ResponseWriter, r *http.Request) {
	a.serveRead(w, r, func(ctx context.Context, _ url.Values, runs []store.Run) (any, error) {
		list, err := changes.ListBases(ctx, a.opt.Store, a.opt.Repository)
		if err != nil {
			return nil, err
		}
		cfg, err := a.opt.loadConfig()
		if err != nil {
			return nil, badRequest("reading graphvein.yaml: %v", err)
		}
		c := cfg.Changes
		return basesResponse{changesHeader: a.header(ctx, runs), Bases: list, Retention: retentionDoc{c.JournalRuns, c.JournalBudget, c.MaxPins, c.MaxRefSnapshots}}, nil
	})
}

// GET /api/v0/changes/summary

type summaryResponse struct {
	changesHeader
	Base    string          `json:"base"`
	Summary changes.Summary `json:"summary"`
}

func (a *changesAPI) summary(w http.ResponseWriter, r *http.Request) {
	a.serveRead(w, r, func(ctx context.Context, q url.Values, runs []store.Run) (any, error) {
		cmp, err := a.comparison(ctx, q, runs)
		if err != nil {
			return nil, err
		}
		return summaryResponse{changesHeader: a.header(ctx, runs), Base: cmp.Base.String(), Summary: cmp.Summary}, nil
	})
}

// GET /api/v0/changes/items

type itemsResponse struct {
	changesHeader
	Base string `json:"base"`
	changes.ItemPage
	// Items shadows ItemPage.Items with each item's precision labels
	// (T020, precision_changes.go).
	Items []itemWithPrecision `json:"items"`
}

func boolParam(q url.Values, name string) (bool, error) {
	v, present, err := param(q, name)
	if err != nil || !present {
		return false, err
	}
	switch v {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	}
	return false, badRequest("%s must be true or false, not %q", name, v)
}

func intParam(q url.Values, name string, min, max int) (int, error) {
	v, present, err := param(q, name)
	if err != nil || !present {
		return 0, err
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, badRequest("%s must be an integer from %d to %d, not %q", name, min, max, v)
	}
	return n, nil
}

func (a *changesAPI) items(w http.ResponseWriter, r *http.Request) {
	a.serveRead(w, r, func(ctx context.Context, q url.Values, runs []store.Run) (any, error) {
		var f changes.Filter
		var err error
		for name, dst := range map[string]*string{"group": &f.Group, "change": &f.Change, "cause": &f.Cause, "kind": &f.Kind, "q": &f.Q} {
			if *dst, _, err = param(q, name); err != nil {
				return nil, err
			}
		}
		if f.IncludeTests, err = boolParam(q, "tests"); err != nil {
			return nil, err
		}
		if f.Raw, err = boolParam(q, "raw"); err != nil {
			return nil, err
		}
		cursor, _, err := param(q, "cursor")
		if err != nil {
			return nil, err
		}
		limit, err := intParam(q, "limit", 1, changes.MaxLimit)
		if err != nil {
			return nil, err
		}
		cmp, err := a.comparison(ctx, q, runs)
		if err != nil {
			return nil, err
		}
		page, err := cmp.Items(f, changes.Page{Cursor: cursor, Limit: limit})
		if err != nil {
			return nil, err
		}
		if err := a.link(ctx, cmp, runs, page.Items); err != nil {
			return nil, err
		}
		labelled, err := itemsPrecision(ctx, a.opt.Store, cmp.ChangeSet(), page.Items)
		if err != nil {
			return nil, err
		}
		return itemsResponse{changesHeader: a.header(ctx, runs), Base: cmp.Base.String(), ItemPage: page, Items: labelled}, nil
	})
}

// link adds the head side's IDE link to items when the head is the current
// index and the item's file still has the stored content hash (ADR 0050).
// The states are copied: the cached comparison is shared.
func (a *changesAPI) link(ctx context.Context, cmp *changes.Comparison, runs []store.Run, items []changes.Item) error {
	if cmp.ChangeSet().Head.Kind != store.SideCurrent || len(items) == 0 {
		return nil
	}
	hashes, err := a.state.fileHashes(ctx, a.opt, runs)
	if err != nil {
		return err
	}
	h := &apiHandler{opt: a.opt}
	p := h.newPresenter(hashes, nil)
	for i := range items {
		after := items[i].After
		if after == nil || after.Span == nil || after.File == "" || !p.ide.Available {
			continue
		}
		if p.p.FileState(after.File, hashes[after.File]) != query.FileUnchanged {
			continue
		}
		presented := p.p.Node(contract.Node{File: after.File, Span: after.Span}, hashes[after.File])
		st := *after
		st.IDE = presented.IDE
		items[i].After = &st
	}
	return nil
}

// GET /api/v0/changes/impact

type impactResponse struct {
	changesHeader
	Base string `json:"base"`
	*changes.ImpactResult
	// Total counts the paths; Paths holds one page of them.
	Total int     `json:"total"`
	Next  *string `json:"next_cursor"`
}

func (a *changesAPI) impact(w http.ResponseWriter, r *http.Request) {
	a.impactAt(w, r, false)
}

// projectImpact is GET /api/v0/project/changes/impact: the member's
// impact continued through the project's stored links into the other
// members (relations I12 and I13; ADR 0075, T022 B7). Ref bases are
// refused, as by graphvein changes impact --project.
func (a *changesAPI) projectImpact(w http.ResponseWriter, r *http.Request) {
	a.impactAt(w, r, true)
}

// impactAt serves an impact request, at project scope when project is
// true.
func (a *changesAPI) impactAt(w http.ResponseWriter, r *http.Request, project bool) {
	a.serveRead(w, r, func(ctx context.Context, q url.Values, runs []store.Run) (any, error) {
		id, hasID, err := param(q, "id")
		if err != nil {
			return nil, err
		}
		group, hasGroup, err := param(q, "group")
		if err != nil {
			return nil, err
		}
		if hasID == hasGroup {
			return nil, badRequest("give exactly one of id and group")
		}
		depth, err := intParam(q, "depth", 1, changes.MaxDepth)
		if err != nil {
			return nil, err
		}
		limit, err := intParam(q, "limit", 1, changes.MaxLimit)
		if err != nil {
			return nil, err
		}
		if limit == 0 {
			limit = changes.DefaultLimit
		}
		cursor, _, err := param(q, "cursor")
		if err != nil {
			return nil, err
		}
		var scope *changes.ProjectScope
		if project {
			raw, _, err := param(q, "base")
			if err != nil {
				return nil, err
			}
			if base, err := changes.ParseBase(raw); err == nil && base.Kind == changes.BaseRef {
				return nil, badRequest("%v", changes.ErrProjectRef)
			}
			if scope, err = a.opt.project.projectScope(ctx); err != nil {
				return nil, err
			}
		}
		cmp, err := a.comparison(ctx, q, runs)
		if err != nil {
			return nil, err
		}
		result, err := changes.Impact(ctx, a.opt.Store, cmp, changes.ImpactRequest{ID: id, Group: group, Depth: depth, Project: scope})
		if err != nil {
			return nil, err
		}
		// The cursor is opaque: the comparison, the request and an offset.
		offset := 0
		bound := cmp.ID() + "\x00" + id + "\x00" + group + "\x00" + strconv.Itoa(result.Depth) + "\x00"
		if project {
			// A project cursor never pages a single-repository answer.
			bound = "project\x00" + bound
		}
		if cursor != "" {
			data, err := base64.RawURLEncoding.DecodeString(cursor)
			c, ok := strings.CutPrefix(string(data), bound)
			n, convErr := strconv.Atoi(c)
			if err != nil || !ok || convErr != nil || n < 0 || n > len(result.Paths) {
				return nil, changes.ErrCursorMismatch
			}
			offset = n
		}
		page := *result
		end := min(offset+limit, len(page.Paths))
		page.Paths = page.Paths[offset:end]
		out := impactResponse{changesHeader: a.header(ctx, runs), Base: cmp.Base.String(), ImpactResult: &page, Total: len(result.Paths)}
		if end < len(result.Paths) {
			next := base64.RawURLEncoding.EncodeToString([]byte(bound + strconv.Itoa(end)))
			out.Next = &next
		}
		return out, nil
	})
}

// POST rules shared by the change endpoints: JSON content type, a bounded
// body with exactly the allowed members.
func decodePost(w http.ResponseWriter, r *http.Request, v any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxChangesBody))
	if err != nil {
		return badRequest("invalid body: %v", err)
	}
	if err := duplicateKeys(data); err != nil {
		return badRequest("invalid body: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return badRequest("invalid body: %v", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object expected")
	}
	return nil
}

// duplicateKeys refuses a JSON text with a repeated member name in any
// object: encoding/json keeps the last one silently, so {"name":"a",
// "name":"b"} would pin b while the client may mean a (R3-10).
func duplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	type frame struct {
		object bool
		keys   map[string]bool
		key    bool // the next string token is a member name
	}
	var stack []*frame
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				if top != nil && top.object {
					top.key = true
				}
				stack = append(stack, &frame{object: t == '{', keys: map[string]bool{}, key: t == '{'})
			case '}', ']':
				stack = stack[:len(stack)-1]
			}
			continue
		case string:
			if top != nil && top.object && top.key {
				if top.keys[t] {
					return fmt.Errorf("duplicate member %q", t)
				}
				top.keys[t] = true
				top.key = false
				continue
			}
		}
		if top != nil && top.object {
			top.key = true
		}
	}
}

// locked runs fn with a writer store under the index lock, taken briefly
// (ADR 0056: pinning and unpinning take the index lock).
func (a *changesAPI) locked(ctx context.Context, fn func(s store.Store) error) error {
	if a.opt.Locker == nil || a.opt.OpenWriter == nil {
		return &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", "this server has no index lock or writer store"}
	}
	lease, err := a.opt.Locker.Acquire(ctx, indexrun.LockWait)
	if err != nil {
		if errors.Is(err, store.ErrIndexLocked) {
			return err
		}
		return &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", err.Error()}
	}
	defer lease.Release()
	s, err := a.opt.OpenWriter(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := lease.Check(ctx); err != nil {
		return err
	}
	return fn(s)
}

type pinRequest struct {
	Run  string `json:"run"`
	Name string `json:"name"`
}

type pinResponse struct {
	APIVersion int       `json:"api_version"`
	Repository string    `json:"repository"`
	Pin        store.Pin `json:"pin"`
}

// POST /api/v0/changes/pins
func (a *changesAPI) pin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, "POST"))
		return
	}
	var req pinRequest
	if err := decodePost(w, r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	if req.Name == "" || len(req.Name) > 200 || strings.ContainsRune(req.Name, 0) {
		a.fail(w, r, badRequest("name must be 1 to 200 bytes without NUL"))
		return
	}
	cfg, err := a.opt.loadConfig()
	if err != nil {
		a.fail(w, r, badRequest("reading graphvein.yaml: %v", err))
		return
	}
	var pin store.Pin
	err = a.locked(r.Context(), func(s store.Store) error {
		var err error
		pin, err = changes.PinRun(r.Context(), s, a.opt.Repository, req.Run, req.Name, cfg.Changes.MaxPins)
		return err
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, pinResponse{APIVersion: 0, Repository: a.opt.Repository, Pin: pin})
}

// POST /api/v0/changes/pins/delete
func (a *changesAPI) unpin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", methodText(r, "POST"))
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodePost(w, r, &req); err != nil {
		a.fail(w, r, err)
		return
	}
	err := a.locked(r.Context(), func(s store.Store) error {
		return changes.UnpinName(r.Context(), s, a.opt.Repository, req.Name)
	})
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"api_version": 0, "repository": a.opt.Repository, "deleted": req.Name})
}

// refRunner owns the server's ref indexing job (POST refs, GET refs): at
// most one at a time, run in a goroutine under the index lock.
type refRunner struct {
	opt  Options
	ctx  context.Context
	stop context.CancelFunc
	mu   sync.Mutex
	job  *refJob
	jobs sync.WaitGroup
}

type refJob struct {
	ID        string     `json:"id"`
	State     string     `json:"state"`
	StartedAt string     `json:"started_at"`
	EndedAt   *string    `json:"ended_at"`
	Base      string     `json:"base"`
	Commits   []string   `json:"commits"`
	Indexed   []string   `json:"indexed"`
	Error     *errorBody `json:"error"`
}

type refsResponse struct {
	APIVersion int     `json:"api_version"`
	Repository string  `json:"repository"`
	Job        *refJob `json:"job"`
}

func newRefRunner(opt Options) *refRunner {
	ctx, cancel := context.WithCancel(context.Background())
	return &refRunner{opt: opt, ctx: ctx, stop: cancel}
}

// running reports whether a ref indexing job runs now.
func (rr *refRunner) running() bool {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.job != nil && rr.job.EndedAt == nil
}

func (rr *refRunner) document() refsResponse {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	out := refsResponse{APIVersion: 0, Repository: rr.opt.Repository}
	if rr.job != nil {
		j := *rr.job
		j.Commits = append([]string{}, j.Commits...)
		j.Indexed = append([]string{}, j.Indexed...)
		out.Job = &j
	}
	return out
}

// ServeHTTP answers GET and HEAD with the job document and POST with a new
// job.
func (rr *refRunner) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusOK, rr.document())
		return
	}
	var req struct {
		Base string `json:"base"`
		Head string `json:"head"`
	}
	fail := func(err error) {
		status, code := changesError(err)
		writeError(w, status, code, err.Error())
	}
	if err := decodePost(w, r, &req); err != nil {
		fail(err)
		return
	}
	if req.Base == "" {
		fail(badRequest("base is required: a ref name"))
		return
	}
	base := changes.Base{Kind: changes.BaseRef, Ref: req.Base, Head: req.Head}
	if rr.opt.noCheckout {
		fail(rr.opt.noCheckoutError("a ref job"))
		return
	}
	indexed, err := changes.IndexedCommit(r.Context(), rr.opt.Store, rr.opt.Repository)
	if err != nil {
		fail(err)
		return
	}
	refs, err := changes.ResolveRefs(r.Context(), rr.opt.Root, base, indexed)
	if err != nil {
		fail(err)
		return
	}
	if rr.opt.Locker == nil || rr.opt.OpenWriter == nil {
		writeError(w, http.StatusServiceUnavailable, "index_lock_unavailable", "this server has no index lock or writer store")
		return
	}
	rr.mu.Lock()
	if rr.job != nil && rr.job.EndedAt == nil {
		rr.mu.Unlock()
		writeError(w, http.StatusConflict, "index_locked", "a ref indexing job is still running")
		return
	}
	rr.mu.Unlock()
	lease, err := rr.opt.Locker.Acquire(r.Context(), indexrun.LockWait)
	if err != nil {
		if errors.Is(err, store.ErrIndexLocked) {
			fail(err)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "index_lock_unavailable", err.Error())
		return
	}
	commits := []string{refs.Base}
	if refs.Head != "" && refs.Head != refs.Base {
		commits = append(commits, refs.Head)
	}
	job := &refJob{ID: rand.Text(), State: jobRunning, StartedAt: rr.opt.now().UTC().Format(time.RFC3339Nano),
		Base: base.String(), Commits: commits, Indexed: []string{}}
	rr.mu.Lock()
	rr.job = job
	rr.mu.Unlock()
	rr.jobs.Add(1)
	go rr.execute(job, lease)
	w.Header().Set("Location", changesRefsPath)
	writeJSON(w, http.StatusAccepted, rr.document())
}

// execute indexes each commit that has no snapshot yet, then records the
// outcome before releasing the lock.
func (rr *refRunner) execute(job *refJob, lease store.IndexLease) {
	defer rr.jobs.Done()
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = &panicError{value: v}
			}
		}()
		var snapshots []store.RefSnapshot
		snapshots, err = rr.opt.Store.RefSnapshots(rr.ctx, rr.opt.Repository)
		if err != nil {
			return
		}
		have := map[string]bool{}
		for _, s := range snapshots {
			if changes.SnapshotUsable(s, index.Version) {
				have[s.Commit] = true
			}
		}
		for _, commit := range job.Commits {
			if have[commit] {
				continue
			}
			_, err = indexrun.RunRef(rr.ctx, indexrun.RefOptions{Commit: commit, Options: indexrun.Options{
				Root: rr.opt.Root, Repository: rr.opt.Repository, Lease: lease, OpenStore: rr.opt.OpenWriter, SnapshotDir: rr.opt.StoreDir, StoreName: rr.opt.StoreName,
				Parser: parse.Options{Timeout: indexrun.DefaultTimeout}, MaxFileSize: indexrun.DefaultMaxFileSize,
			}})
			if err != nil {
				return
			}
			rr.mu.Lock()
			job.Indexed = append(job.Indexed, commit)
			rr.mu.Unlock()
		}
	}()
	rr.mu.Lock()
	ended := rr.opt.now().UTC().Format(time.RFC3339Nano)
	job.EndedAt = &ended
	switch {
	case err == nil:
		job.State = jobCommitted
	case rr.ctx.Err() != nil:
		job.State = jobCancelled
	default:
		_, code := changesError(err)
		if code == "store_error" {
			code = "index_failed"
		}
		job.State, job.Error = jobFailed, &errorBody{Code: code, Message: err.Error()}
		rr.opt.logger().Warn("ref indexing did not commit", "error", err)
	}
	rr.mu.Unlock()
	if releaseErr := lease.Release(); releaseErr != nil {
		rr.opt.logger().Warn("releasing the index lock failed", "error", releaseErr)
	}
}

// shutdown cancels a running job and waits for it.
func (rr *refRunner) shutdown() {
	rr.stop()
	rr.jobs.Wait()
}

package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/storeloc"
	"github.com/graphvein/graphvein/internal/workspace"
)

// This file holds the multi-index server (ADR 0087, T038): one server
// answers for every index of the workspace registry. Each per-index
// endpoint takes ws=<entry id>; without it the request goes to the
// current entry, so the single-index URLs keep working. Indexes are opened
// on first use and kept open (warm) up to maxOpenIndexes.

// Paths of the workspace endpoints. GET /api/v0/workspace lists the
// entries; the three POST paths are the typed-path flow (ADR 0041
// revision) and remove.
const (
	workspacePath       = "/api/v0/workspace"
	workspaceCheckPath  = "/api/v0/workspace/check"
	workspaceAddPath    = "/api/v0/workspace/add"
	workspaceRemovePath = "/api/v0/workspace/remove"
	// wsParam names the registry entry a per-index request is for.
	wsParam = "ws"
)

// maxOpenIndexes bounds the indexes kept open (D3). Beyond it the least
// recently used idle index is closed.
const maxOpenIndexes = 8

// defaultCheckInterval is how often the background check revisits the
// open indexes after the first pass over every entry (D4).
const defaultCheckInterval = 30 * time.Second

// notOpenRecheck is how old a recorded check of an entry that is not open
// may get before a pass checks it again (WS-20; T038 review). Two
// minutes keeps a dot that is not open at most about two and a half
// minutes old (this bound plus one interval) while a pass still walks one
// entry at a time: with the 8-index limit and a few dozen entries the
// walks stay a small share of each interval, and an entry nobody looks at
// costs one walk every two minutes, not one every 30 s.
const notOpenRecheck = 2 * time.Minute

// Entry states of GET /api/v0/workspace.
const (
	entryOpen       = "open"
	entryNotOpen    = "not_open"
	entryFolderGone = "folder_gone"
)

// ErrNoIndex is returned by WorkspaceOptions.Open when the entry's store
// does not exist yet (no index run ever wrote it). Any other open error is
// reported as it is, never as "not indexed".
var ErrNoIndex = errors.New("no index is stored for this folder")

// WorkspaceIndex is one opened registry entry: the options a
// single-index server for it would have, and how to release them.
type WorkspaceIndex struct {
	Entry workspace.Entry
	// Options carries Root, Repository, Store, Locker, OpenWriter,
	// StoreDir, StoreName and, for a project, Project. Version, Assets, Logger,
	// Now and ReportURL come from the server's own options.
	Options Options
	// Close releases the store and the lockers. For the index named on
	// the command line (Current) the server calls it only to move that
	// index's store (POST /api/v0/store/move); nil there means its store
	// cannot be moved from the UI.
	Close func()
}

// WorkspaceOptions makes a server serve the workspace registry (ADR 0087).
type WorkspaceOptions struct {
	// Registry is the registry file; an unavailable registry still serves
	// Current.
	Registry *workspace.Registry
	// Current is the index named on the command line (serve <path> or
	// serve --project), opened by the caller and never closed by the
	// server; nil for serve without a path.
	Current *WorkspaceIndex
	// Open opens an entry's index read-only, with the lockers and writer
	// store a re-index needs. With create it first creates an empty store
	// when none exists (adding a folder); without it a missing store is
	// ErrNoIndex.
	Open func(ctx context.Context, e workspace.Entry, create bool) (*WorkspaceIndex, error)
	// Stores resolves where each folder's index is stored (ADR 0093), the
	// one resolver the CLI uses: entries other than Current resolve
	// without GRAPHVEIN_SQLITE_PATH. nil turns the store fields and the
	// store endpoints off (tests of other parts).
	Stores *storeloc.Resolver
	// CreateStore creates an empty store at loc: adding a folder whose
	// index the user put inside the folder (an in-folder store is found by
	// its file). nil refuses that place.
	CreateStore func(ctx context.Context, loc storeloc.Location) error
	// WorkDir is the server's working directory: without Current, the
	// registered entry for it is current, and an unregistered folder with
	// an index or manifest is suggested.
	WorkDir string
	// Home and ConfigDirs are refused as typed paths (D5); empty values
	// are looked up from the operating system.
	Home       string
	ConfigDirs []string
	// SharedStore says every entry's index lives in one store (the
	// database GRAPHVEIN_POSTGRES_URL names), where entries are told apart
	// by repository ID and project name only: two entries in different
	// folders using one name are refused (WS-57).
	SharedStore bool
	// MaxOpen overrides maxOpenIndexes (tests).
	MaxOpen int
	// CheckInterval overrides defaultCheckInterval; negative turns the
	// background checks off (tests).
	CheckInterval time.Duration
}

// openIndex is an open entry with its routes.
type openIndex struct {
	entry   workspace.Entry
	handler *indexHandler
	close   func()
	pinned  bool
	// refs counts requests (and background checks) using the index; an
	// index is closed only at zero. used orders eviction.
	refs int
	used uint64
	// detached marks an index removed from the workspace while in use;
	// the last release closes it.
	detached bool
}

// workspaceServer is a server's workspace state.
type workspaceServer struct {
	opt     Options
	ws      *WorkspaceOptions
	handler http.Handler
	// token guards the typed-path POSTs, like the manifest edit's.
	token  string
	budget *cacheBudget

	// openMu serializes opening indexes, so one entry is never opened
	// twice at once.
	openMu sync.Mutex
	// mu guards everything below.
	mu       sync.Mutex
	open     map[string]*openIndex
	tick     uint64
	stopped  bool
	checks   map[string]entryCheck
	confirms map[string]confirmRecord
	// currentID is the entry served without ws ("" when none).
	currentID string
	// moving marks entries whose store is being moved: requests for them
	// are refused (409 store_moving) and nothing opens their store.
	moving map[string]bool
	// direct counts opens of an entry's store outside the open indexes
	// (background checks) in progress; a move waits for them.
	direct map[string]int

	bgCtx    context.Context
	bgCancel context.CancelFunc
	bg       sync.WaitGroup
	// wake asks the check loop for a pass now (an entry was added), so a
	// new entry is checked without waiting for the interval.
	wake chan struct{}
}

func newWorkspaceServer(opt Options, allowedHosts []string) *workspaceServer {
	ctx, cancel := context.WithCancel(context.Background())
	s := &workspaceServer{
		opt: opt, ws: opt.Workspace, token: rand.Text(), budget: &cacheBudget{limit: cacheTotalLimit},
		open: map[string]*openIndex{}, checks: map[string]entryCheck{}, confirms: map[string]confirmRecord{},
		moving: map[string]bool{}, direct: map[string]int{},
		bgCtx: ctx, bgCancel: cancel, wake: make(chan struct{}, 1),
	}
	if s.ws.Registry == nil {
		s.ws.Registry = workspace.Unavailable(errors.New("no workspace registry is configured"))
	}
	if c := s.ws.Current; c != nil {
		s.currentID = c.Entry.ID
		s.open[c.Entry.ID] = &openIndex{entry: c.Entry, handler: s.newHandler(c), pinned: true}
	} else if s.ws.WorkDir != "" {
		if dir, err := workspace.Resolve(s.ws.WorkDir); err == nil {
			if entries, err := s.ws.Registry.Entries(); err == nil {
				workspace.Sort(entries)
				for _, e := range entries {
					if workspace.SamePath(e.Path, dir) {
						s.currentID = e.ID
						break
					}
				}
			}
		}
	}
	s.handler = guard(opt, allowedHosts, s.serveAPI)
	return s
}

// newHandler builds the routes of an opened entry with the server's own
// version, assets, logger and clock, and an answer cache.
func (s *workspaceServer) newHandler(wi *WorkspaceIndex) *indexHandler {
	o := wi.Options
	o.Version, o.Assets, o.Logger, o.Now = s.opt.Version, s.opt.Assets, s.opt.Logger, s.opt.Now
	o.ReportURL = s.opt.ReportURL
	o.Workspace = nil
	id, project := wi.Entry.ID, wi.Options.Project
	o.onStatus = func(repository, path string, f freshnessStatus) {
		s.recordStatus(id, project, repository, path, f)
	}
	ih := newIndexHandler(o)
	ih.cache = newAnswerCache(ih, s.budget)
	ih.runs.onEnd = ih.cache.invalidate
	return ih
}

func (s *workspaceServer) maxOpen() int {
	if s.ws.MaxOpen > 0 {
		return s.ws.MaxOpen
	}
	return maxOpenIndexes
}

// serveAPI dispatches an API request that passed the guard: the
// workspace endpoints, or a per-index endpoint of the entry ws names.
func (s *workspaceServer) serveAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case workspacePath:
		s.list(w, r)
		return
	case workspaceCheckPath, workspaceAddPath, workspaceRemovePath, storeCheckPath, storeMovePath, storeDefaultPath:
		s.post(w, r)
		return
	case storePath:
		s.storeSettings(w, r)
		return
	}
	id, r, err := s.entryOf(r)
	if err == nil {
		// Starting a run creates the store when the folder has none yet
		// (an entry added without a run, or whose .graphvein was deleted),
		// as add does (T038 review); every other request opens the
		// store as it is.
		create := r.Method == http.MethodPost && r.URL.Path == indexRunPath
		// Any request that may write (every method but GET and HEAD)
		// first tests the folder, also for an index already open: a run
		// of an open index whose folder was moved would recreate it
		// (T038 re-check W2).
		write := r.Method != http.MethodGet && r.Method != http.MethodHead
		var oi *openIndex
		if oi, err = s.acquire(r.Context(), id, create, write); err == nil {
			defer s.release(oi)
			oi.handler.serve(w, r)
			return
		}
	}
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	logFailure(s.opt.logger(), r, err)
	writeError(w, http.StatusInternalServerError, "store_error", err.Error())
}

// entryOf reads the ws parameter and returns the request without it, so
// the index's handlers and the answer cache never see it. Without ws the
// request is for the current entry.
func (s *workspaceServer) entryOf(r *http.Request) (string, *http.Request, error) {
	q := r.URL.Query()
	values, present := q[wsParam]
	if !present {
		current := s.current()
		if current == "" {
			return "", r, &apiError{http.StatusBadRequest, "ws_required", "this server has no current index; name a workspace entry with ws= (GET " + workspacePath + " lists them)"}
		}
		return current, r, nil
	}
	if len(values) != 1 || values[0] == "" {
		return "", r, badRequest("ws must be given once and name a workspace entry")
	}
	q.Del(wsParam)
	u := *r.URL
	u.RawQuery = q.Encode()
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return values[0], r2, nil
}

// entries lists the registry with the command-line entry first if the
// registry lacks it (its write failed), and the registry's error.
func (s *workspaceServer) entries() ([]workspace.Entry, error) {
	entries, err := s.ws.Registry.Entries()
	if c := s.ws.Current; c != nil {
		found := false
		for _, e := range entries {
			found = found || e.ID == c.Entry.ID
		}
		if !found {
			entries = append(entries, c.Entry)
		}
	}
	workspace.Sort(entries)
	return entries, err
}

// entry finds id among the entries.
func (s *workspaceServer) entry(id string) (workspace.Entry, error) {
	entries, err := s.entries()
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	if err != nil {
		return workspace.Entry{}, &apiError{http.StatusInternalServerError, "registry_error", err.Error()}
	}
	return workspace.Entry{}, &apiError{http.StatusNotFound, "unknown_entry", fmt.Sprintf("this server's workspace has no entry %q; GET %s lists them", id, workspacePath)}
}

// acquire returns the open index of entry id, opening it when needed
// (with create, an empty store is created first, for a folder being
// added). With write (a request that may write) a folder that is gone is
// refused even when its index is open. The caller releases it.
func (s *workspaceServer) acquire(ctx context.Context, id string, create, write bool) (*openIndex, error) {
	// A name shared with another folder in a shared store is refused
	// before anything is opened or answered, also for an open index (the
	// other entry may have been registered since it opened).
	if err := s.sharedConflict(id); err != nil {
		return nil, err
	}
	if err := s.notMoving(id); err != nil {
		return nil, err
	}
	if write || create {
		e, err := s.entry(id)
		if err != nil {
			return nil, err
		}
		if folderGone(e.Path) {
			return nil, folderGoneError(e)
		}
	}
	if oi, err := s.hold(id, true); oi != nil || err != nil {
		return oi, err
	}
	e, err := s.entry(id)
	if err != nil {
		return nil, err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	if oi, err := s.hold(id, true); oi != nil || err != nil {
		return oi, err
	}
	// A folder that is gone is never opened: with create, opening would
	// write a new store and so recreate the folder.
	if folderGone(e.Path) {
		return nil, folderGoneError(e)
	}
	// A move that started meanwhile waits for this open (openMu) and
	// closes what it opened; one marked before is refused here.
	if err := s.notMoving(id); err != nil {
		return nil, err
	}
	wi, err := s.ws.Open(ctx, e, create)
	if errors.Is(err, ErrNoIndex) {
		return nil, &apiError{http.StatusConflict, "no_index", fmt.Sprintf("no index is stored for %s (%s) yet; start its run (\"Index it\" in the Workspace list, or POST /api/v0/index/run?ws=%s) or run %s", e.Name, e.Path, e.ID, indexCommandFor(e.Kind, e.Path))}
	}
	if ae := storeOpenError(err); ae != nil {
		return nil, ae
	}
	if err != nil {
		return nil, fmt.Errorf("opening the index of %s: %w", e.Name, err)
	}
	wi.Entry = e
	oi := &openIndex{entry: e, handler: s.newHandler(wi), close: wi.Close, refs: 1}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		oi.shutdown()
		return nil, &apiError{http.StatusServiceUnavailable, "unavailable", "the server is stopping"}
	}
	s.tick++
	oi.used = s.tick
	s.open[id] = oi
	victims := s.evictLocked()
	s.mu.Unlock()
	for _, v := range victims {
		s.opt.logger().Info("closing an index beyond the open limit", "entry", v.entry.ID)
		v.shutdown()
	}
	return oi, nil
}

// folderGoneError refuses a request for entry e, whose folder is gone.
func folderGoneError(e workspace.Entry) error {
	return &apiError{http.StatusConflict, "folder_gone", fmt.Sprintf("the folder of %s is not at %s any more; the entry stays listed until it is removed", e.Name, e.Path)}
}

// hold takes a reference on id when it is open; use marks it as used.
func (s *workspaceServer) hold(id string, use bool) (*openIndex, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, &apiError{http.StatusServiceUnavailable, "unavailable", "the server is stopping"}
	}
	oi := s.open[id]
	if oi == nil {
		return nil, nil
	}
	oi.refs++
	if use {
		s.tick++
		oi.used = s.tick
	}
	return oi, nil
}

// current is the entry served without ws, "" when none.
func (s *workspaceServer) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentID
}

func (s *workspaceServer) release(oi *openIndex) {
	s.mu.Lock()
	oi.refs--
	victims := s.evictLocked()
	if oi.detached && oi.refs == 0 {
		oi.detached = false
		victims = append(victims, oi)
	}
	s.mu.Unlock()
	for _, v := range victims {
		v.shutdown()
	}
}

// evictLocked removes the least recently used idle indexes beyond the
// limit and returns them for closing outside mu. An index with a request
// in flight, a running job or the command-line pin is never closed, so
// the limit can be exceeded while all are busy.
func (s *workspaceServer) evictLocked() []*openIndex {
	var out []*openIndex
	for len(s.open) > s.maxOpen() {
		var victim *openIndex
		for _, oi := range s.open {
			if oi.pinned || oi.refs > 0 || oi.handler.busy() {
				continue
			}
			if victim == nil || oi.used < victim.used {
				victim = oi
			}
		}
		if victim == nil {
			break
		}
		delete(s.open, victim.entry.ID)
		out = append(out, victim)
	}
	return out
}

// name is the entry's name as its index resolved it when it opened (the
// repository of graphvein.yaml, or the project manifest's name now), so a
// rename shows without a new entry; the registry's name and id stay.
func (oi *openIndex) name() string {
	if p := oi.handler.opt.Project; p != nil {
		if n := p.current().Name; n != "" {
			return n
		}
	}
	if r := oi.handler.opt.Repository; r != "" {
		return r
	}
	return oi.entry.Name
}

// shutdown stops the index's background work, drops its cache and
// releases its store.
func (oi *openIndex) shutdown() {
	oi.handler.shutdown()
	oi.handler.cache.invalidate()
	if oi.close != nil {
		oi.close()
	}
}

// start begins the background freshness checks.
func (s *workspaceServer) start() {
	if s.ws.CheckInterval < 0 {
		return
	}
	s.bg.Add(1)
	go s.checkLoop()
}

// shutdown stops the background checks and every open index; the
// command-line index's store stays for its owner to close.
func (s *workspaceServer) shutdown() {
	s.bgCancel()
	s.bg.Wait()
	s.mu.Lock()
	s.stopped = true
	open := s.open
	s.open = map[string]*openIndex{}
	s.mu.Unlock()
	for _, oi := range open {
		oi.shutdown()
	}
}

// openIDs lists the IDs of the open indexes.
func (s *workspaceServer) openIDs() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for id := range s.open {
		out[id] = true
	}
	return out
}

// GET /api/v0/workspace

type workspaceResponse struct {
	APIVersion int         `json:"api_version"`
	Registry   registryDoc `json:"registry"`
	// Token is the session token of the workspace POSTs.
	Token      string               `json:"token"`
	Current    *string              `json:"current"`
	Suggestion *workspaceSuggestion `json:"suggestion"`
	OpenLimit  int                  `json:"open_limit"`
	Entries    []workspaceItem      `json:"entries"`
}

type registryDoc struct {
	Path  string  `json:"path"`
	Error *string `json:"error"`
}

// workspaceSuggestion is the working directory when it holds an index or a
// project manifest and is not registered (the first-run screen offers it).
type workspaceSuggestion struct {
	Path string         `json:"path"`
	Kind workspace.Kind `json:"kind"`
}

type workspaceItem struct {
	ID   string         `json:"id"`
	Kind workspace.Kind `json:"kind"`
	Name string         `json:"name"`
	Path string         `json:"path"`
	// Registered is false only for the command-line index when writing
	// it to the registry failed.
	Registered bool   `json:"registered"`
	Current    bool   `json:"current"`
	State      string `json:"state"`
	// Freshness is null until a background check of this entry finished.
	Freshness    *checkDoc         `json:"freshness"`
	Members      []workspaceMember `json:"members"`
	MembersError *string           `json:"members_error"`
	// Job is the entry's latest re-index job of this server process
	// (GET /api/v0/index/run's job), null when the index is not open.
	Job *jobDoc `json:"job"`
	// Conflict is set when the entry uses a repository or project name
	// that another entry in another folder uses in the shared store
	// (WS-57); its index is then not answered. Null otherwise.
	Conflict *conflictDoc `json:"conflict"`
}

type workspaceMember struct {
	Repository string    `json:"repository"`
	Path       *string   `json:"path"`
	Checkout   bool      `json:"checkout"`
	Freshness  *checkDoc `json:"freshness"`
}

// checkDoc is one background freshness check, in the states of GET
// /api/v0/status.
type checkDoc struct {
	State        string `json:"state"`
	CheckedAt    string `json:"checked_at"`
	ChangedCount *int   `json:"changed_count"`
	Error        string `json:"error,omitempty"`
}

func (s *workspaceServer) list(w http.ResponseWriter, r *http.Request) {
	entries, regErr := s.entries()
	registered := map[string]bool{}
	if regEntries, err := s.ws.Registry.Entries(); err == nil {
		for _, e := range regEntries {
			registered[e.ID] = true
		}
	}
	out := workspaceResponse{APIVersion: 0, Registry: registryDoc{Path: s.ws.Registry.Path()}, Token: s.token, OpenLimit: s.maxOpen(), Entries: []workspaceItem{}}
	if regErr != nil {
		msg := regErr.Error()
		out.Registry.Error = &msg
	}
	current := s.current()
	if current != "" {
		out.Current = newPtr(current)
	}
	out.Suggestion = s.suggestion(entries)
	open := s.openIDs()
	conflicts := s.conflicts(entries)
	s.mu.Lock()
	checks := make(map[string]entryCheck, len(s.checks))
	for id, c := range s.checks {
		checks[id] = c
	}
	s.mu.Unlock()
	// The list reads the registry, the open indexes' memory and each
	// project's manifest; of an entry's folder it only stats the path. It
	// never probes a lock or opens a store, so a poll cannot recreate a
	// folder that was moved (T038 review).
	for _, e := range entries {
		item := workspaceItem{ID: e.ID, Kind: e.Kind, Name: e.Name, Path: e.Path, Registered: registered[e.ID], Current: e.ID == current, State: entryNotOpen}
		// A folder proven gone is listed gone even while its index is
		// still open (it closes when idle): R4.
		gone := folderGone(e.Path)
		if open[e.ID] {
			if !gone {
				item.State = entryOpen
			}
			if oi, _ := s.hold(e.ID, false); oi != nil {
				item.Job = oi.handler.runs.latestJob()
				item.Name = oi.name()
				s.release(oi)
			}
		}
		if gone {
			item.State = entryFolderGone
		}
		c, checked := checks[e.ID]
		if sc := conflicts[e.ID]; sc != nil {
			// Facts under a name another folder uses say nothing true of
			// this folder (WS-57): no freshness is shown.
			item.Conflict = sc.doc(item.Name)
			checked = false
		}
		if checked && !gone && e.Kind != workspace.KindProject {
			doc := c.doc
			item.Freshness = &doc
		}
		if e.Kind == workspace.KindProject {
			item.Members = []workspaceMember{}
			p, err := config.LoadProject(e.Path)
			if err != nil {
				msg := err.Error()
				item.MembersError = &msg
				item.Members = nil
			}
			for _, m := range p.Members {
				wm := workspaceMember{Repository: m.Repository, Checkout: m.Checkout}
				if m.Path != "" {
					wm.Path = newPtr(m.Path)
				}
				// A member's check is shown only while the manifest
				// still gives the path that was checked: an open project
				// serves the manifest it opened with until a project or
				// status request reads the edited one (T038 re-check W1).
				if md, ok := c.members[m.Repository]; checked && ok && !gone && c.paths[m.Repository] == m.Path {
					wm.Freshness = &md
				}
				item.Members = append(item.Members, wm)
			}
			if checked && !gone && err == nil {
				item.Freshness = foldListed(item.Members, c.doc.CheckedAt)
			}
		}
		out.Entries = append(out.Entries, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// foldListed is a project's freshness folded over the members the
// manifest lists now (WS-9; T038 review): null while any listed member
// has no recorded check (a member added since the last check, never
// checked, or whose path changed since its check), so a project is never fresh over a member nobody checked;
// otherwise the fold of their states, checked at the oldest member's
// time. A project without members is unknown as of its last check.
func foldListed(members []workspaceMember, checkedAt string) *checkDoc {
	var states []string
	oldest := ""
	for _, m := range members {
		if m.Freshness == nil {
			return nil
		}
		states = append(states, m.Freshness.State)
		if oldest == "" || laterCheck(oldest, m.Freshness.CheckedAt) {
			oldest = m.Freshness.CheckedAt
		}
	}
	if len(members) == 0 {
		return &checkDoc{State: stateUnknown, CheckedAt: checkedAt}
	}
	return &checkDoc{State: foldMemberStates(states), CheckedAt: oldest}
}

// folderGone reports a folder that is proven missing; any other stat
// error is not called gone.
func folderGone(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, fs.ErrNotExist)
}

func (s *workspaceServer) suggestion(entries []workspace.Entry) *workspaceSuggestion {
	if s.ws.WorkDir == "" {
		return nil
	}
	dir, err := workspace.Resolve(s.ws.WorkDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if workspace.SamePath(e.Path, dir) {
			return nil
		}
	}
	if _, err := os.Stat(filepath.Join(dir, config.ProjectFile)); err == nil {
		return &workspaceSuggestion{Path: dir, Kind: workspace.KindProject}
	}
	// An index of the working directory, wherever the store resolver
	// finds it (WS-72): its own .graphvein, a stores.yaml entry or the
	// stores folder. Nothing is opened or created.
	if s.ws.Stores != nil {
		if loc, err := s.ws.Stores.Resolve(dir, storeloc.Scope{}); err == nil && loc.Exists {
			return &workspaceSuggestion{Path: dir, Kind: workspace.KindRepository}
		}
	}
	return nil
}

// Background freshness checks (D4).

// entryCheck is the last finished check of an entry; for a project, its
// members' checks too, each with the member path the manifest gave when
// it was checked ("" for none).
type entryCheck struct {
	doc     checkDoc
	members map[string]checkDoc
	paths   map[string]string
}

func (s *workspaceServer) checkLoop() {
	defer s.bg.Done()
	ctx := s.bgCtx
	interval := s.ws.CheckInterval
	if interval == 0 {
		interval = defaultCheckInterval
	}
	entries, _ := s.entries()
	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		s.checkEntry(ctx, e)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		entries, _ := s.entries()
		open := s.openIDs()
		for _, e := range entries {
			if ctx.Err() != nil {
				return
			}
			if open[e.ID] || s.checkDue(e.ID) {
				s.checkEntry(ctx, e)
			}
		}
	}
}

// checkDue reports an entry that is not open whose check is missing (an
// entry added since the first pass) or older than notOpenRecheck (R5).
func (s *workspaceServer) checkDue(id string) bool {
	s.mu.Lock()
	c, ok := s.checks[id]
	s.mu.Unlock()
	if !ok {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, c.doc.CheckedAt)
	return err != nil || s.opt.now().Sub(at) >= notOpenRecheck
}

// poke asks the check loop for a pass now; a pass already asked for is
// enough.
func (s *workspaceServer) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// checkEntry computes the freshness of one entry as GET /api/v0/status
// does and records it. An entry whose folder is gone is not checked; a
// check cut short by shutdown records nothing.
func (s *workspaceServer) checkEntry(ctx context.Context, e workspace.Entry) {
	if folderGone(e.Path) {
		// What was checked before the folder went says nothing now (R4).
		s.mu.Lock()
		delete(s.checks, e.ID)
		s.mu.Unlock()
		return
	}
	if s.sharedConflict(e.ID) != nil {
		// Its store answers for another folder too (WS-57): no check.
		s.mu.Lock()
		delete(s.checks, e.ID)
		s.mu.Unlock()
		return
	}
	now := func() string { return s.opt.now().UTC().Format(time.RFC3339Nano) }
	checkedAt := now()
	var c entryCheck
	if oi, _ := s.hold(e.ID, false); oi != nil {
		c = checkOptions(ctx, oi.handler.opt, checkedAt)
		s.release(oi)
	} else {
		// A store being moved is not opened; a move waits for an open
		// that started before it.
		if !s.beginDirect(e.ID) {
			return
		}
		defer s.endDirect(e.ID)
		wi, err := s.ws.Open(ctx, e, false)
		switch {
		case errors.Is(err, ErrNoIndex):
			c.doc = checkDoc{State: stateNotIndexed, CheckedAt: checkedAt}
		case err != nil:
			c.doc = checkDoc{State: stateUnknown, CheckedAt: checkedAt, Error: err.Error()}
		default:
			c = checkOptions(ctx, wi.Options, checkedAt)
			if wi.Close != nil {
				wi.Close()
			}
		}
	}
	if ctx.Err() != nil {
		return
	}
	s.mu.Lock()
	// A status request answered while this check walked is newer: keep
	// it.
	if old, ok := s.checks[e.ID]; !ok || !laterCheck(old.doc.CheckedAt, c.doc.CheckedAt) {
		s.checks[e.ID] = c
	}
	s.mu.Unlock()
}

// recordStatus records the freshness a GET /api/v0/status of entry id
// computed (WS-9), unless a newer check is already recorded. For a
// project, the member's state replaces its earlier one and the project is
// folded again over the manifest's members; a member never checked counts
// as unknown, and the project's checked_at is its oldest member's.
func (s *workspaceServer) recordStatus(id string, project *ProjectOptions, repository, path string, f freshnessStatus) {
	doc := checkDoc{State: f.State, CheckedAt: f.CheckedAt, ChangedCount: f.ChangedCount, Error: f.Error}
	var members []string
	if project != nil {
		for _, m := range project.current().Members {
			members = append(members, m.Repository)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.checks[id]
	if project == nil {
		if !ok || !laterCheck(old.doc.CheckedAt, doc.CheckedAt) {
			s.checks[id] = entryCheck{doc: doc}
		}
		return
	}
	if prev, ok := old.members[repository]; ok && laterCheck(prev.CheckedAt, doc.CheckedAt) {
		return
	}
	c := entryCheck{members: map[string]checkDoc{}, paths: map[string]string{}}
	for name, md := range old.members {
		c.members[name] = md
		c.paths[name] = old.paths[name]
	}
	c.members[repository] = doc
	c.paths[repository] = path
	var states []string
	oldest := ""
	for _, name := range members {
		md, ok := c.members[name]
		if !ok {
			states = append(states, stateUnknown)
			continue
		}
		states = append(states, md.State)
		if oldest == "" || laterCheck(oldest, md.CheckedAt) {
			oldest = md.CheckedAt
		}
	}
	if oldest == "" {
		oldest = doc.CheckedAt
	}
	c.doc = checkDoc{State: foldMemberStates(states), CheckedAt: oldest}
	s.checks[id] = c
}

// laterCheck reports whether checked_at a is strictly later than b. A
// value that does not parse is never later.
func laterCheck(a, b string) bool {
	ta, err := time.Parse(time.RFC3339Nano, a)
	if err != nil {
		return false
	}
	tb, err := time.Parse(time.RFC3339Nano, b)
	if err != nil {
		return true
	}
	return ta.After(tb)
}

// checkOptions checks one repository, or every member of a project.
func checkOptions(ctx context.Context, opt Options, checkedAt string) entryCheck {
	if opt.Project == nil {
		changed, err := opt.freshness(ctx)
		return entryCheck{doc: checkFor(ctx, opt, opt.Repository, changed, err, checkedAt)}
	}
	p := opt.Project.current()
	c := entryCheck{members: map[string]checkDoc{}, paths: map[string]string{}}
	var states []string
	for _, m := range p.Members {
		var doc checkDoc
		if !m.Checkout {
			doc = checkDoc{State: stateUnknown, CheckedAt: checkedAt, Error: errNoCheckout.Error()}
		} else {
			changed, err := index.Freshness(ctx, m.Path, m.Repository, opt.Store)
			doc = checkFor(ctx, opt, m.Repository, changed, err, checkedAt)
		}
		c.members[m.Repository] = doc
		c.paths[m.Repository] = m.Path
		states = append(states, doc.State)
	}
	c.doc = checkDoc{State: foldMemberStates(states), CheckedAt: checkedAt}
	return c
}

func checkFor(ctx context.Context, opt Options, repository string, changed []string, err error, checkedAt string) checkDoc {
	runs, rerr := opt.Store.Runs(ctx, repository)
	if rerr != nil {
		return checkDoc{State: stateUnknown, CheckedAt: checkedAt, Error: rerr.Error()}
	}
	f := freshnessFor(changed, err, len(runs) > 0)
	return checkDoc{State: f.State, CheckedAt: checkedAt, ChangedCount: f.ChangedCount, Error: f.Error}
}

// foldMemberStates folds member states: fresh only when every member is
// fresh; stale when any member is (that is proven); otherwise unknown,
// or not indexed when every other member is not indexed. A project
// without members is unknown.
func foldMemberStates(states []string) string {
	if len(states) == 0 {
		return stateUnknown
	}
	count := map[string]int{}
	for _, st := range states {
		count[st]++
	}
	switch {
	case count[stateFresh] == len(states):
		return stateFresh
	case count[stateStale] > 0:
		return stateStale
	case count[stateNotIndexed] == len(states):
		return stateNotIndexed
	}
	return stateUnknown
}

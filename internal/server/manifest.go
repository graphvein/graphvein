package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/projectsuggest"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds manifest editing from the UI (ADR 0075 "Manifest editing
// from the UI", T022 B10). The YAML stays the source of truth: the UI sends
// typed operations on declarations (consumes entries and their selectors,
// default_scope, link scopes, repository names, accepting a suggestion),
// the server answers with the unified diff of the manifest and the
// validation result, and writes only when the request applies exactly that
// previewed result to exactly the file it was based on. No request names a
// file: the server writes only the manifest it was started with, never the
// local override file, and the only path an edit carries is a link scope
// folder, checked to be a folder inside the member's checkout.

// Paths of the manifest endpoints.
const (
	projectManifestPath    = "/api/v0/project/manifest"
	projectSuggestionsPath = "/api/v0/project/suggestions"
)

// Limits of a manifest edit.
const (
	// maxManifestBody bounds a POST body.
	maxManifestBody = 16 << 10
	// maxManifestOps bounds the operations of one edit.
	maxManifestOps = 32
	// maxManifestSize bounds the manifest the server reads to edit.
	maxManifestSize = 256 << 10
)

// tokenHeader carries the session token of a manifest edit. The token is
// random per server process and readable only by a page of this server
// (GET /api/v0/project/manifest, behind the Host, Sec-Fetch-Site and
// Origin checks); a custom header also makes every cross-origin request a
// preflighted one, which the server never grants.
const tokenHeader = "X-Graphvein-Token"

// projectRouter holds a project server's routes and rebuilds them when a
// manifest edit is written, so the server serves the edited declarations
// (and a renamed member) without a restart.
type projectRouter struct {
	opt   Options
	runs  *runner
	token string
	// editMu serialises manifest edits (read, check the base, write,
	// reload) and reloads of a manifest edited on disk.
	editMu sync.Mutex
	// diskHash is filesHash of the manifest and local file the server
	// serves now (or last refused as invalid); editMu guards it.
	diskHash string

	mu     sync.RWMutex
	routes map[string]http.Handler
	// apis are the change endpoints of every route set built, so shutdown
	// stops a ref job started before a reload too.
	apis    changesSet
	stopped bool
}

func newProjectRouter(opt Options, runs *runner) *projectRouter {
	pr := &projectRouter{opt: opt, runs: runs, token: rand.Text()}
	// The files as they are now stand for the manifest the server was
	// started with; an edit made between loading and here is picked up
	// by the first project request.
	pr.diskHash, _ = pr.filesHash()
	pr.build()
	return pr
}

// filesHash names the manifest and local file on disk now. Both count:
// the local file can change a member's path (T022 B11).
func (pr *projectRouter) filesHash() (string, error) {
	dir := pr.opt.Project.current().Dir
	data, err := readBounded(filepath.Join(dir, config.ProjectFile))
	if err != nil {
		return "", err
	}
	local, err := readBounded(filepath.Join(dir, config.ProjectLocalFile))
	if errors.Is(err, fs.ErrNotExist) {
		return manifestHash(data) + " no-local", nil
	}
	if err != nil {
		return "", err
	}
	return manifestHash(data) + " " + manifestHash(local), nil
}

// refreshFromDisk reloads the manifest when its files' content changed on
// disk since the server loaded or wrote it (an editor, git checkout,
// graphvein project add; T022 B11). It runs on every request of the
// project (each member endpoint reads the member's folder, T038 re-check
// W1 and its follow-up), and costs two small reads and hashes when
// nothing changed. A changed
// manifest is served at once and its links are stale until the next link
// pass, exactly as after a UI edit; an invalid one is logged and the
// server keeps serving the manifest it has, so a half-saved file never
// takes the project down.
func (pr *projectRouter) refreshFromDisk() {
	pr.editMu.Lock()
	defer pr.editMu.Unlock()
	hash, err := pr.filesHash()
	if err != nil || hash == pr.diskHash {
		return
	}
	pr.diskHash = hash
	_, _, p, err := pr.readManifest()
	if err != nil {
		pr.opt.logger().Warn("graphvein-project.yaml changed on disk but does not validate; serving the manifest loaded before", "error", err)
		return
	}
	pr.reload(p)
	pr.opt.logger().Info("graphvein-project.yaml changed on disk; serving it, links stale until the next link pass", "project", p.Name)
}

// build makes the route set of the current manifest. The caller holds mu
// (or is constructing pr).
func (pr *projectRouter) build() {
	routes, apis := newProjectRoutes(pr.opt, pr.runs)
	routes[projectManifestPath] = manifestHandler{pr: pr}
	routes[projectSuggestionsPath] = suggestionsHandler{pr: pr}
	pr.routes = routes
	pr.apis = append(pr.apis, apis...)
}

func (pr *projectRouter) lookup(path string) (http.Handler, bool) {
	// Every request reads the manifest on disk first (two small reads and
	// hashes when nothing changed): a member's status walks the member's
	// path (T038 re-check W1), and every other member endpoint (code
	// peek, change views, Settings) reads the member's folder too, so a
	// path changed on disk would otherwise be read at the old folder.
	pr.refreshFromDisk()
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	h, ok := pr.routes[path]
	return h, ok
}

// reload serves p from now on.
func (pr *projectRouter) reload(p config.Project) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.stopped {
		return
	}
	pr.opt.Project.setCurrent(p)
	pr.build()
}

// busy reports whether a ref indexing job of any route set runs.
func (pr *projectRouter) busy() bool {
	pr.mu.RLock()
	apis := pr.apis
	pr.mu.RUnlock()
	return apis.busy()
}

func (pr *projectRouter) shutdown() {
	pr.mu.Lock()
	pr.stopped = true
	apis := pr.apis
	pr.mu.Unlock()
	apis.shutdown()
	pr.opt.Project.closeOpened()
}

// manifestFile is the one file an edit may write.
func (pr *projectRouter) manifestFile() string {
	return filepath.Join(pr.opt.Project.current().Dir, config.ProjectFile)
}

// readManifest reads the manifest as it is on disk now, with the local
// file, and validates both as the CLI would.
func (pr *projectRouter) readManifest() (data, local []byte, p config.Project, err error) {
	dir := pr.opt.Project.current().Dir
	if data, err = readBounded(filepath.Join(dir, config.ProjectFile)); err != nil {
		return nil, nil, config.Project{}, err
	}
	local, err = readBounded(filepath.Join(dir, config.ProjectLocalFile))
	if errors.Is(err, fs.ErrNotExist) {
		local, err = nil, nil
	}
	if err != nil {
		return nil, nil, config.Project{}, err
	}
	p, err = config.ParseProject(dir, data, local)
	if err != nil {
		return data, local, p, &apiError{http.StatusConflict, "invalid_manifest", "the manifest on disk does not validate, so it cannot be edited from the UI: " + err.Error()}
	}
	return data, local, p, nil
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(data) > maxManifestSize {
		return nil, &apiError{http.StatusRequestEntityTooLarge, "manifest_too_large", fmt.Sprintf("%s is larger than %d KiB; edit it in a text editor", filepath.Base(path), maxManifestSize>>10)}
	}
	return data, nil
}

// manifestHash names one manifest text; an edit is based on it.
func manifestHash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// GET /api/v0/project/manifest

type manifestResponse struct {
	APIVersion int    `json:"api_version"`
	Project    string `json:"project"`
	// File is the manifest's name; never a path to choose.
	File    string `json:"file"`
	Hash    string `json:"hash"`
	Content string `json:"content"`
	// Token goes in the X-Graphvein-Token header of every edit.
	Token   string              `json:"token"`
	Members []manifestMemberDoc `json:"members"`
}

type manifestMemberDoc struct {
	Repository string `json:"repository"`
	// RepositorySource is "member graphvein.yaml" when the member's own
	// config names it: a rename is then refused (its own name wins).
	RepositorySource string               `json:"repository_source"`
	Checkout         bool                 `json:"checkout"`
	Consumes         []manifestConsumeDoc `json:"consumes"`
	Scopes           []manifestScopeDoc   `json:"scopes"`
}

type manifestScopeDoc struct {
	Name     string               `json:"name"`
	Path     string               `json:"path"`
	Consumes []manifestConsumeDoc `json:"consumes"`
}

type manifestConsumeDoc struct {
	Repository   string      `json:"repository"`
	Scope        string      `json:"scope"`
	DefaultScope bool        `json:"default_scope"`
	Select       selectorDoc `json:"select"`
}

type selectorDoc struct {
	Bases        []string `json:"bases"`
	Origins      []string `json:"origins"`
	PathPrefixes []string `json:"path_prefixes"`
}

func (s selectorDoc) selector() config.Selector {
	return config.Selector{Bases: s.Bases, Origins: s.Origins, PathPrefixes: s.PathPrefixes}
}

func selectorOf(s config.Selector) selectorDoc {
	nonNil := func(v []string) []string { return append([]string{}, v...) }
	return selectorDoc{Bases: nonNil(s.Bases), Origins: nonNil(s.Origins), PathPrefixes: nonNil(s.PathPrefixes)}
}

func consumesOf(list []config.Consume) []manifestConsumeDoc {
	out := []manifestConsumeDoc{}
	for _, c := range list {
		out = append(out, manifestConsumeDoc{Repository: c.Repository, Scope: c.Scope, DefaultScope: c.DefaultScope, Select: selectorOf(c.Select)})
	}
	return out
}

type manifestHandler struct{ pr *projectRouter }

func (h manifestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var err error
	if r.Method == http.MethodPost {
		err = h.post(w, r)
	} else {
		err = h.get(w)
	}
	var ae *apiError
	switch {
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	case err != nil:
		logFailure(h.pr.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "manifest_error", err.Error())
	}
}

func (h manifestHandler) get(w http.ResponseWriter) error {
	data, _, p, err := h.pr.readManifest()
	if err != nil {
		return err
	}
	out := manifestResponse{Project: p.Name, File: config.ProjectFile, Hash: manifestHash(data), Content: string(data), Token: h.pr.token, Members: []manifestMemberDoc{}}
	for _, m := range p.Members {
		doc := manifestMemberDoc{Repository: m.Repository, RepositorySource: m.RepositorySource, Checkout: m.Checkout, Consumes: consumesOf(m.Consumes), Scopes: []manifestScopeDoc{}}
		for _, s := range m.Scopes {
			doc.Scopes = append(doc.Scopes, manifestScopeDoc{Name: s.Name, Path: s.Path, Consumes: consumesOf(s.Consumes)})
		}
		out.Members = append(out.Members, doc)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// POST /api/v0/project/manifest

type manifestEditRequest struct {
	// BaseHash is the hash of the manifest the edit was made on; a
	// different file on disk refuses the edit (409 manifest_changed).
	BaseHash   string            `json:"base_hash"`
	Operations []json.RawMessage `json:"operations"`
	// Apply writes the result; without it the answer is a preview.
	Apply bool `json:"apply"`
	// ResultHash is the preview's result_hash, required with apply: the
	// write happens only when the edit still gives exactly the text the
	// user confirmed (a suggestion list read from files may have moved).
	ResultHash string `json:"result_hash"`
	// ConfirmDelete names the old repository names whose stored index the
	// user confirmed deleting (T022 review): the preview's retiring
	// entries with a stored index must all be listed, else apply is
	// refused (409 delete_confirmation_required) and nothing is written.
	ConfirmDelete []string `json:"confirm_delete,omitempty"`
}

// retiringDoc is one old name a rename would retire, as the preview shows
// it: "delete stored index of NAME (N nodes)" from the store shared by all
// projects. Standalone is true when no stored link pass lists the
// repository: the index was made on its own (not by a project pass), and
// may be the store's only copy of that repository.
type retiringDoc struct {
	Repository string   `json:"repository"`
	Stored     bool     `json:"stored"`
	Nodes      int      `json:"nodes"`
	Projects   []string `json:"projects"`
	Standalone bool     `json:"standalone"`
	// Confirm is the text the UI shows on the second confirm.
	Confirm string `json:"confirm"`
}

// manifestOp is one typed operation. Which fields an operation takes is
// listed in opFields; any other field, a path field included, is refused.
type manifestOp struct {
	Op     string `json:"op"`
	Member string `json:"member"`
	// Scope names the member's link scope whose consumes list the
	// operation edits; empty for the member's own list.
	Scope   string       `json:"scope"`
	Consume *consumeEdit `json:"consume"`
	Target  *consumeEdit `json:"target"`
	Select  *selectorDoc `json:"select"`
	Repo    string       `json:"repository"`
	Value   *bool        `json:"value"`
	Name    string       `json:"name"`
	Path    string       `json:"path"`
	To      string       `json:"to"`
	Number  int          `json:"number"`
}

type consumeEdit struct {
	Repository   string       `json:"repository"`
	Scope        string       `json:"scope"`
	DefaultScope bool         `json:"default_scope"`
	Select       *selectorDoc `json:"select"`
}

func (c consumeEdit) target() config.ConsumeTarget {
	return config.ConsumeTarget{Repository: c.Repository, Scope: c.Scope, DefaultScope: c.DefaultScope}
}

// opFields lists, per operation, the fields it takes and (first in each
// list after op) the ones it requires.
var opFields = map[string]struct{ required, optional []string }{
	"add_consume":       {[]string{"member", "consume"}, []string{"scope"}},
	"remove_consume":    {[]string{"member", "target"}, []string{"scope"}},
	"set_select":        {[]string{"member", "target", "select"}, []string{"scope"}},
	"set_default_scope": {[]string{"member", "repository", "value"}, []string{"scope"}},
	"add_scope":         {[]string{"member", "name", "path"}, nil},
	"remove_scope":      {[]string{"member", "name"}, nil},
	"rename_repository": {[]string{"member", "to"}, nil},
	"accept_suggestion": {[]string{"number"}, nil},
}

type manifestEditResponse struct {
	APIVersion int  `json:"api_version"`
	Applied    bool `json:"applied"`
	// BaseHash is the manifest the edit was applied to; ResultHash the
	// text it gives (the manifest's hash after an apply).
	BaseHash   string `json:"base_hash"`
	ResultHash string `json:"result_hash"`
	// Diff is the unified diff of graphvein-project.yaml; empty when the
	// edit changes nothing.
	Diff string `json:"diff"`
	// Valid reports whether the result validates as graphvein project
	// would; Problems says why not. An invalid result is never written.
	Valid    bool     `json:"valid"`
	Problems []string `json:"problems"`
	// Changes says in one sentence per operation what it does.
	Changes []string     `json:"changes"`
	Reindex reindexOffer `json:"reindex"`
	// Retired is present only on an applied edit that renamed a member:
	// for each old name, whether its rows (project links, outcomes, member
	// record and stored index) were removed from the project store with
	// the write (T022 B11), so no rows stay under a name no manifest uses.
	Retired []retiredDoc `json:"retired,omitempty"`
	// Retiring is present on an edit that renames a member: what the
	// write would delete per old name. An apply must list every entry
	// with a stored index in confirm_delete.
	Retiring []retiringDoc `json:"retiring,omitempty"`
}

// retiredDoc is one member name a rename retired.
type retiredDoc struct {
	Repository string `json:"repository"`
	Removed    bool   `json:"removed"`
	// Error says why the old name's rows are still stored; null when
	// they were removed.
	Error *string `json:"error"`
}

// reindexOffer is what makes the links follow the edit: the link pass
// alone when no member changed (declarations only), otherwise a project
// re-index (a renamed member has no stored index under its new name).
type reindexOffer struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

const (
	reindexLinkPass = "link_pass"
	reindexProject  = "project"
)

func (h manifestHandler) post(w http.ResponseWriter, r *http.Request) error {
	if !loopbackPeer(r) {
		return &apiError{http.StatusForbidden, "not_loopback", "manifest edits are accepted only from this machine"}
	}
	got := r.Header.Values(tokenHeader)
	if len(got) != 1 || subtle.ConstantTimeCompare([]byte(got[0]), []byte(h.pr.token)) != 1 {
		return &apiError{http.StatusForbidden, "token_required", "a manifest edit needs this server's session token in " + tokenHeader + " (from GET " + projectManifestPath + ")"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "POST " + r.URL.Path + " needs Content-Type: application/json"}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxManifestBody))
	if err != nil {
		return &apiError{http.StatusRequestEntityTooLarge, "too_large", fmt.Sprintf("a manifest edit is at most %d KiB", maxManifestBody>>10)}
	}
	if err := duplicateKeys(body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	var req manifestEditRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("invalid body: one JSON object with base_hash, operations, apply and result_hash expected")
	}
	if len(req.Operations) == 0 || len(req.Operations) > maxManifestOps {
		return badRequest("an edit has 1 to %d operations", maxManifestOps)
	}
	if req.Apply != (req.ResultHash != "") {
		return badRequest("apply needs result_hash (the result_hash of the preview being confirmed), and only apply takes it")
	}
	ops := make([]manifestOp, 0, len(req.Operations))
	for i, raw := range req.Operations {
		op, err := decodeOp(raw)
		if err != nil {
			return badRequest("operation %d: %v", i+1, err)
		}
		ops = append(ops, op)
	}

	pr := h.pr
	pr.editMu.Lock()
	defer pr.editMu.Unlock()
	data, local, p, err := pr.readManifest()
	if err != nil {
		return err
	}
	base := manifestHash(data)
	if req.BaseHash != base {
		return &apiError{http.StatusConflict, "manifest_changed", "graphvein-project.yaml changed on disk since this edit was prepared; reload it and make the edit again"}
	}
	out := manifestEditResponse{BaseHash: base, Problems: []string{}, Changes: []string{}, Reindex: reindexOffer{Kind: reindexLinkPass,
		Reason: "only declarations change: the link pass re-links the stored indexes"}}
	next := data
	var suggestions *projectsuggest.Result
	for i, op := range ops {
		var change string
		next, change, err = pr.applyOp(r.Context(), next, p, op, &suggestions)
		if err != nil {
			var ae *apiError
			if errors.As(err, &ae) {
				ae.message = fmt.Sprintf("operation %d (%s): %s", i+1, op.Op, ae.message)
				return ae
			}
			return &apiError{http.StatusBadRequest, "edit_refused", fmt.Sprintf("operation %d (%s): %v", i+1, op.Op, err)}
		}
		out.Changes = append(out.Changes, change)
		if op.Op == "rename_repository" {
			out.Reindex = reindexOffer{Kind: reindexProject, Reason: "a member is renamed: the write removes its stored index and links under the old name, so the project is indexed again"}
		}
	}
	out.ResultHash, out.Diff = manifestHash(next), unifiedDiff(config.ProjectFile, string(data), string(next))
	result, verr := config.ParseProject(p.Dir, next, local)
	out.Valid = verr == nil
	if verr != nil {
		// Messages name files relative to the manifest's folder.
		out.Problems = append(out.Problems, strings.ReplaceAll(verr.Error(), p.Dir+string(filepath.Separator), ""))
	}
	gone := renamedAway(p, result)
	if verr == nil && len(gone) > 0 {
		if out.Retiring, err = pr.retiring(r.Context(), gone); err != nil {
			return err
		}
	}
	if !req.Apply {
		writeJSON(w, http.StatusOK, out)
		return nil
	}
	switch {
	case req.ResultHash != out.ResultHash:
		return &apiError{http.StatusConflict, "preview_changed", "the edit no longer gives the previewed manifest (a suggestion's evidence may have changed); preview it again"}
	case !out.Valid:
		return &apiError{http.StatusUnprocessableEntity, "invalid_manifest", "the edited manifest does not validate, so nothing was written: " + out.Problems[0]}
	case out.Diff == "":
		return &apiError{http.StatusBadRequest, "no_change", "the edit changes nothing"}
	}
	confirmed := map[string]bool{}
	for _, name := range req.ConfirmDelete {
		confirmed[name] = true
	}
	for _, rd := range out.Retiring {
		if rd.Stored && !confirmed[rd.Repository] {
			why := ""
			if rd.Standalone {
				why = " It was indexed on its own (no stored link pass lists it), so this may be the store's only copy."
			}
			return &apiError{http.StatusConflict, "delete_confirmation_required", fmt.Sprintf("the rename deletes the stored index of %s (%d nodes) from the store shared by all projects; confirm \"%s\" to write it.%s", rd.Repository, rd.Nodes, rd.Confirm, why)}
		}
	}
	write := func() error {
		if err := config.WriteFileAtomic(pr.manifestFile(), next); err != nil {
			return err
		}
		out.Applied = true
		pr.reload(result)
		if hash, err := pr.filesHash(); err == nil {
			pr.diskHash = hash
		}
		return nil
	}
	if len(gone) > 0 {
		// A rename retires the old name in the same request: the project
		// lock is taken before the write (a held lock refuses the edit
		// with nothing written) and no re-index may start until the old
		// name's rows are gone, so a job reading the old manifest cannot
		// index the old name again.
		err = pr.runs.whileIdle(func() error {
			var rerr error
			out.Retired, rerr = pr.retire(r.Context(), p.Name, gone, write)
			return rerr
		})
	} else {
		err = write()
	}
	if err != nil {
		return err
	}
	pr.opt.logger().Info("project manifest edited from the UI", "project", result.Name, "operations", len(ops), "reindex", out.Reindex.Kind)
	writeJSON(w, http.StatusOK, out)
	return nil
}

// renamedAway lists the members of before that after no longer names, in
// manifest order. The UI never adds or removes a member, so these are the
// old names of renamed members.
func renamedAway(before, after config.Project) []string {
	kept := map[string]bool{}
	for _, m := range after.Members {
		kept[m.Repository] = true
	}
	var out []string
	for _, m := range before.Members {
		if !kept[m.Repository] {
			out = append(out, m.Repository)
		}
	}
	return out
}

// retiring reads, per old name, what retiring it would delete.
func (pr *projectRouter) retiring(ctx context.Context, gone []string) ([]retiringDoc, error) {
	out := []retiringDoc{}
	for _, name := range gone {
		f, err := pr.opt.Store.RepositoryFootprint(ctx, name)
		if err != nil {
			return nil, err
		}
		if !f.Stored {
			continue
		}
		out = append(out, retiringDoc{Repository: name, Stored: f.Stored, Nodes: f.Nodes, Projects: f.Projects, Standalone: f.Stored && len(f.Projects) == 0,
			Confirm: fmt.Sprintf("delete stored index of %s (%d nodes)", name, f.Nodes)})
	}
	return out, nil
}

// retire writes the manifest with write and then removes every row of
// the old names gone from the project store (store.RetireProjectMember),
// all under the project lock. A server that cannot write the store still
// writes the manifest and says why the rows stay; a lock held by another
// process refuses the edit before anything is written.
func (pr *projectRouter) retire(ctx context.Context, project string, gone []string, write func() error) ([]retiredDoc, error) {
	notRemoved := func(why string) []retiredDoc {
		out := []retiredDoc{}
		for _, name := range gone {
			out = append(out, retiredDoc{Repository: name, Error: newPtr(why)})
		}
		return out
	}
	if pr.opt.Locker == nil || pr.opt.OpenWriter == nil {
		if err := write(); err != nil {
			return nil, err
		}
		return notRemoved("this server cannot write the project store, so the old name's stored index and links stay until graphvein index --project runs a pass (links) or are removed by hand (index)"), nil
	}
	lease, err := pr.opt.Locker.Acquire(ctx, indexrun.LockWait)
	if errors.Is(err, store.ErrIndexLocked) {
		return nil, &apiError{http.StatusConflict, "index_locked", "another process holds the project lock (graphvein index or another graphvein serve), so the rename was not written; try again when it has finished"}
	}
	if err != nil {
		return nil, &apiError{http.StatusServiceUnavailable, "index_lock_unavailable", "taking the project lock for the rename: " + err.Error()}
	}
	defer lease.Release()
	if err := write(); err != nil {
		return nil, err
	}
	w, err := pr.opt.OpenWriter(ctx)
	if err != nil {
		return notRemoved("opening the project store: " + err.Error()), nil
	}
	defer w.Close()
	out := []retiredDoc{}
	for _, name := range gone {
		doc := retiredDoc{Repository: name, Removed: true}
		if err := lease.Check(ctx); err != nil {
			doc.Removed, doc.Error = false, newPtr("project lock lost: "+err.Error())
		} else if err := w.RetireProjectMember(ctx, project, name); err != nil {
			doc.Removed, doc.Error = false, newPtr(err.Error())
		}
		if doc.Removed {
			pr.opt.logger().Info("renamed member retired from the project store", "project", project, "repository", name)
		} else {
			pr.opt.logger().Warn("renamed member's rows stay in the project store", "project", project, "repository", name, "error", *doc.Error)
		}
		out = append(out, doc)
	}
	return out, nil
}

// loopbackPeer reports whether the request came over a loopback
// connection. Listen binds only loopback addresses; this holds the edit
// endpoint to that even if a handler is mounted elsewhere.
func loopbackPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// decodeOp decodes one operation, refusing a field the operation does not
// take, a missing required field, or an unknown operation.
func decodeOp(raw json.RawMessage) (manifestOp, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return manifestOp{}, errors.New("an operation is a JSON object")
	}
	var name string
	if err := json.Unmarshal(fields["op"], &name); err != nil {
		return manifestOp{}, errors.New("op must name the operation")
	}
	spec, ok := opFields[name]
	if !ok {
		return manifestOp{}, fmt.Errorf("unknown op %q", name)
	}
	allowed := map[string]bool{"op": true}
	for _, f := range append(append([]string{}, spec.required...), spec.optional...) {
		allowed[f] = true
	}
	for f := range fields {
		if !allowed[f] {
			return manifestOp{}, fmt.Errorf("%s takes no field %q (an edit names no file or member folder)", name, f)
		}
	}
	for _, f := range spec.required {
		if _, ok := fields[f]; !ok {
			return manifestOp{}, fmt.Errorf("%s needs %q", name, f)
		}
	}
	var op manifestOp
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&op); err != nil {
		return manifestOp{}, fmt.Errorf("invalid %s: %v", name, err)
	}
	if op.Target != nil && op.Target.Select != nil {
		return manifestOp{}, errors.New("target names an entry (repository, scope, default_scope); its select goes in select")
	}
	return op, nil
}

// applyOp applies one operation to data; p is the manifest as read (the
// edit's base), for the checks that need resolved members.
func (pr *projectRouter) applyOp(ctx context.Context, data []byte, p config.Project, op manifestOp, suggestions **projectsuggest.Result) ([]byte, string, error) {
	owner := op.Member
	if op.Scope != "" {
		owner += "/" + op.Scope
	}
	switch op.Op {
	case "add_consume":
		c := config.Consume{Repository: op.Consume.Repository, Scope: op.Consume.Scope, DefaultScope: op.Consume.DefaultScope}
		if op.Consume.Select != nil {
			c.Select = op.Consume.Select.selector()
		}
		next, err := config.AddProjectConsume(data, op.Member, op.Scope, c)
		return next, "adds: " + strings.ReplaceAll(projectlink.DeclarationText(owner, c), "`", ""), err
	case "remove_consume":
		next, err := config.RemoveProjectConsume(data, op.Member, op.Scope, op.Target.target())
		return next, fmt.Sprintf("removes %s's consumes entry for %s", owner, op.Target.target()), err
	case "set_select":
		sel := op.Select.selector()
		next, err := config.SetProjectConsumeSelect(data, op.Member, op.Scope, op.Target.target(), sel)
		what := "clears the selector of"
		if !sel.Empty() {
			what = "sets the selector (" + selectorText(sel) + ") of"
		}
		return next, fmt.Sprintf("%s %s's entry for %s", what, owner, op.Target.target()), err
	case "set_default_scope":
		next, err := config.SetProjectConsumeDefaultScope(data, op.Member, op.Scope, op.Repo, *op.Value)
		if *op.Value {
			return next, fmt.Sprintf("narrows %s's entry for %s to its default scope", owner, op.Repo), err
		}
		return next, fmt.Sprintf("widens %s's entry for %s from its default scope to the whole repository", owner, op.Repo), err
	case "add_scope":
		if err := scopeFolderProblem(p, op.Member, op.Path); err != nil {
			return nil, "", err
		}
		next, err := config.AddProjectScope(data, op.Member, config.Scope{Name: op.Name, Path: op.Path})
		return next, fmt.Sprintf("declares link scope %s/%s (folder %s)", op.Member, op.Name, op.Path), err
	case "remove_scope":
		next, err := config.RemoveProjectScope(data, op.Member, op.Name)
		return next, fmt.Sprintf("removes link scope %s/%s", op.Member, op.Name), err
	case "rename_repository":
		next, err := config.RenameProjectRepository(data, op.Member, op.To)
		return next, fmt.Sprintf("renames repository %s to %s, in every consumes entry too", op.Member, op.To), err
	case "accept_suggestion":
		if *suggestions == nil {
			calls, err := projectsuggest.ReadCalls(ctx, pr.opt.Store, p)
			if err != nil {
				return nil, "", fmt.Errorf("reading stored calls: %w", err)
			}
			res, err := projectsuggest.Suggest(ctx, p, calls)
			if err != nil {
				return nil, "", fmt.Errorf("reading suggestions: %w", err)
			}
			*suggestions = &res
		}
		list := (*suggestions).Proposals
		if op.Number < 1 || op.Number > len(list) {
			return nil, "", fmt.Errorf("no proposal %d (there are %d)", op.Number, len(list))
		}
		s := list[op.Number-1]
		if s.Declared {
			return nil, "", fmt.Errorf("proposal %d: %s already has a consumes entry for %s; edit that entry instead (one entry per provider)", op.Number, joinScope(s.Member, s.Scope), joinScope(s.Provider, s.ProviderScope))
		}
		c := s.Consume()
		next, err := config.AddProjectConsume(data, s.Member, s.Scope, c)
		return next, fmt.Sprintf("accepts suggestion %d: %s", op.Number, strings.ReplaceAll(projectlink.DeclarationText(joinScope(s.Member, s.Scope), c), "`", "")), err
	}
	return nil, "", fmt.Errorf("unknown op %q", op.Op)
}

func joinScope(repository, scope string) string {
	if scope == "" {
		return repository
	}
	return repository + "/" + scope
}

func selectorText(s config.Selector) string {
	var parts []string
	for _, l := range []struct {
		name   string
		values []string
	}{{"bases", s.Bases}, {"origins", s.Origins}, {"path_prefixes", s.PathPrefixes}} {
		if len(l.values) > 0 {
			parts = append(parts, l.name+" "+strings.Join(l.values, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

// scopeFolderProblem checks a link scope folder from an edit: a clean,
// relative, slash-separated path (no "..", no drive or backslash) that is
// an existing folder inside the member's checkout once symbolic links are
// resolved. It is the only path an edit carries.
func scopeFolderProblem(p config.Project, member, path string) error {
	refuse := func(format string, args ...any) error {
		return &apiError{http.StatusBadRequest, "path_refused", fmt.Sprintf(format, args...)}
	}
	if !config.ScopePathValid(path) {
		return refuse("scope folder %q must be relative to the member, with forward slashes and no '.' or '..' elements", path)
	}
	var m *config.Member
	for i := range p.Members {
		if p.Members[i].Repository == member {
			m = &p.Members[i]
		}
	}
	if m == nil {
		return refuse("the manifest has no member %q", member)
	}
	if !m.Checkout {
		return refuse("member %s has no checkout here, so a scope folder cannot be checked; declare it with the checkout present", member)
	}
	root, err := filepath.EvalSymlinks(m.Path)
	if err != nil {
		return refuse("member %s: %v", member, err)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(m.Path, filepath.FromSlash(path)))
	if err != nil {
		return refuse("member %s has no folder %s", member, path)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return refuse("folder %s leaves member %s (through a symbolic link)", path, member)
	}
	if info, err := os.Stat(real); err != nil || !info.IsDir() {
		return refuse("%s in member %s is not a folder", path, member)
	}
	// T022 review: the link pass compares scope folders by exact case,
	// and a case-insensitive file system (macOS, Windows) resolves a
	// wrong case. Each declared component must be an entry of its parent
	// with exactly that name, or the scope would be silently empty.
	if onDisk, ok := caseExact(m.Path, path); !ok {
		return refuse("member %s has no folder %s with exactly that case (on disk: %s); scope folders are compared by exact case", member, path, onDisk)
	}
	return nil
}

// caseExact reports whether every component of the slash-separated path
// below root is a directory entry with exactly that name, following
// symbolic links component by component. When one is not, it returns the
// path as far as it matched, with the entry's case on disk when only the
// case differs.
func caseExact(root, path string) (string, bool) {
	dir := root
	var matched []string
	for _, part := range strings.Split(path, "/") {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return strings.Join(matched, "/"), false
		}
		exact, folded := false, ""
		for _, e := range entries {
			switch {
			case e.Name() == part:
				exact = true
			case strings.EqualFold(e.Name(), part):
				folded = e.Name()
			}
		}
		if !exact {
			if folded != "" {
				matched = append(matched, folded)
			}
			return strings.Join(matched, "/"), false
		}
		matched = append(matched, part)
		next, err := filepath.EvalSymlinks(filepath.Join(dir, part))
		if err != nil {
			return strings.Join(matched, "/"), false
		}
		dir = next
	}
	return path, true
}

// GET /api/v0/project/suggestions: graphvein project suggest's listing
// (proposals with evidence, unmatched and unread files), read from the
// manifest on disk and the stored calls. Nothing is applied: accepting
// one is a manifest edit (accept_suggestion).

type suggestionsResponse struct {
	APIVersion int `json:"api_version"`
	// Hash is the manifest the listing was read for.
	Hash string `json:"hash"`
	projectsuggest.Result
}

type suggestionsHandler struct{ pr *projectRouter }

func (h suggestionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data, _, p, err := h.pr.readManifest()
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	if err == nil {
		var calls projectsuggest.Calls
		if calls, err = projectsuggest.ReadCalls(ctx, h.pr.opt.Store, p); err == nil {
			var res projectsuggest.Result
			if res, err = projectsuggest.Suggest(ctx, p, calls); err == nil {
				writeJSON(w, http.StatusOK, suggestionsResponse{Hash: manifestHash(data), Result: res})
				return
			}
		}
	}
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	logFailure(h.pr.opt.logger(), r, err)
	writeError(w, http.StatusInternalServerError, "suggest_error", err.Error())
}

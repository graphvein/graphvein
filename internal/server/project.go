package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/gitref"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/indexrun"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/projectlink"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds project mode (graphvein serve --project; ADR 0075
// "Server and UI", T022 B8): one project per server (P8). Every endpoint
// of a single-repository server answers per member and takes a
// repository parameter; the project endpoints below read the members
// together through the stored link pass (projectlink.View).

// Paths of the project endpoints. All are GET and HEAD only except the
// manifest edit (manifest.go, T022 B10).
const (
	projectPath               = "/api/v0/project"
	projectCallersPath        = "/api/v0/project/callers"
	projectImpactPath         = "/api/v0/project/impact"
	projectChangesImpactPath  = "/api/v0/project/changes/impact"
	projectLinkChangesPath    = "/api/v0/project/links/changes"
	projectMaxDepth           = query.ProjectMaxDepth
	projectDefaultCallerDepth = 1
	projectDefaultImpactDepth = 3
)

// ProjectOptions makes a server serve one project. The project store is
// Options.Store; Options.Locker is the project lock (ADR 0075 "Locks") and
// Options.OpenWriter opens the project store for writing.
type ProjectOptions struct {
	// Project is the manifest the server was started with. A manifest edit
	// from the UI (T022 B10) reloads it: the server then serves the edited
	// declarations, and the links are stale until the next link pass.
	// Declarations edited on disk by other means are picked up by the next
	// project request (T022 B11: the files' content hash is compared on
	// every /api/v0/project request).
	Project config.Project
	// MemberLockers holds each member's index lock in the project store,
	// which the change endpoints' pins and ref jobs take as they do for
	// one repository. A member without one answers those 503, unless
	// OpenMemberLocker opens one (a member renamed from the UI).
	MemberLockers map[string]store.IndexLocker
	// OpenMemberLocker, when set, opens the index lock of a member that
	// MemberLockers lacks. The server closes what it opened at shutdown.
	OpenMemberLocker func(repository string) store.IndexLocker
	// Reindex re-indexes project as graphvein index --project does: each
	// member checkout under its own index lock, then the link pass under
	// the project lock, never two locks at once; with linkPassOnly it runs
	// only the link pass over the members' stored indexes (after a
	// manifest edit that changed no member). It reports the member being
	// indexed and its phases through progress (repository empty for the
	// link pass). Nil disables re-index from the UI.
	Reindex func(ctx context.Context, project config.Project, linkPassOnly bool, progress func(repository string, p indexrun.Progress)) ProjectRun

	// mu guards reloaded and opened.
	mu       sync.Mutex
	reloaded *config.Project
	opened   map[string]store.IndexLocker
}

// current is the manifest the server serves now: the one it was started
// with, or the last one a UI edit wrote.
func (po *ProjectOptions) current() config.Project {
	po.mu.Lock()
	defer po.mu.Unlock()
	if po.reloaded != nil {
		return *po.reloaded
	}
	return po.Project
}

// setCurrent records the manifest a UI edit wrote.
func (po *ProjectOptions) setCurrent(p config.Project) {
	po.mu.Lock()
	defer po.mu.Unlock()
	po.reloaded = &p
}

// memberLocker returns member's index lock: the one the caller gave, or
// one opened (once) through OpenMemberLocker.
func (po *ProjectOptions) memberLocker(repository string) store.IndexLocker {
	if l := po.MemberLockers[repository]; l != nil {
		return l
	}
	po.mu.Lock()
	defer po.mu.Unlock()
	if l := po.opened[repository]; l != nil || po.OpenMemberLocker == nil {
		return l
	}
	l := po.OpenMemberLocker(repository)
	if po.opened == nil {
		po.opened = map[string]store.IndexLocker{}
	}
	po.opened[repository] = l
	return l
}

// closeOpened closes the lockers memberLocker opened.
func (po *ProjectOptions) closeOpened() {
	po.mu.Lock()
	defer po.mu.Unlock()
	for _, l := range po.opened {
		if l != nil {
			l.Close()
		}
	}
	po.opened = nil
}

// ProjectRun is what one project re-index did.
type ProjectRun struct {
	// Members are in manifest order.
	Members []ProjectMemberRun
	// Pass is the stored link pass; nil when the pass failed.
	Pass      *store.ProjectLinkRun
	PassError error
}

// ProjectMemberRun is one member's part of a project re-index.
type ProjectMemberRun struct {
	Repository string
	// Indexed reports that the member's checkout was indexed (or tried);
	// false for a member without a checkout, which is linked from the store.
	Indexed bool
	// Run is the committed run, nil when the member failed or was not
	// indexed.
	Run   *store.Run
	Error error
}

// phaseLinking is the re-index phase of the link pass in project mode.
const phaseLinking = "linking"

// projectState is what every handler of a project server shares.
type projectState struct {
	opt     Options
	project config.Project
	// members holds each member's options, as a single-repository server
	// for it would have them.
	members map[string]Options
	names   []string
}

func newProjectState(opt Options) *projectState {
	ps := &projectState{opt: opt, project: opt.Project.current(), members: map[string]Options{}}
	for _, m := range ps.project.Members {
		mo := opt
		mo.Project, mo.project = nil, ps
		mo.Repository, mo.Root = m.Repository, ""
		if m.Checkout {
			mo.Root = m.Path
		}
		mo.noCheckout = !m.Checkout
		mo.Locker = opt.Project.memberLocker(m.Repository)
		ps.members[m.Repository] = mo
		ps.names = append(ps.names, m.Repository)
	}
	return ps
}

// member resolves the repository parameter: the named member, or the only
// member when the project has one. Several members and no parameter is
// ambiguous: 400, never a guess.
func (ps *projectState) member(q url.Values) (config.Member, error) {
	name, present, err := param(q, "repository")
	if err != nil {
		return config.Member{}, err
	}
	if !present {
		if len(ps.project.Members) == 1 {
			return ps.project.Members[0], nil
		}
		return config.Member{}, &apiError{http.StatusBadRequest, "repository_required",
			fmt.Sprintf("this server serves project %s with members %s; name one with repository=", ps.project.Name, strings.Join(ps.names, ", "))}
	}
	for _, m := range ps.project.Members {
		if m.Repository == name {
			return m, nil
		}
	}
	return config.Member{}, badRequest("repository %q is not a member of project %s (members: %s)", name, ps.project.Name, strings.Join(ps.names, ", "))
}

// view reads the last link pass with its freshness.
func (ps *projectState) view(ctx context.Context) (projectlink.View, error) {
	return projectlink.ReadView(ctx, ps.opt.Store, ps.project)
}

// memberRoutePaths are the endpoints a single-repository server serves
// for its one repository; project mode serves each per member.
func memberRoutePaths() []string {
	paths := []string{"/api/v0/status", changesBasesPath, changesSummaryPath, changesItemsPath, changesImpactPath,
		changesPinsPath, changesPinsDeletePath, changesRefsPath, projectChangesImpactPath}
	for p := range newAPIRoutes(Options{}) {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// newProjectRoutes builds the routes of a project server: every
// single-repository endpoint dispatched by the repository parameter, the
// project re-index runner and the project endpoints.
func newProjectRoutes(opt Options, runs *runner) (map[string]http.Handler, changesSet) {
	ps := newProjectState(opt)
	byPath := map[string]map[string]http.Handler{}
	for _, p := range memberRoutePaths() {
		byPath[p] = map[string]http.Handler{}
	}
	var apis changesSet
	for _, m := range ps.project.Members {
		mo := ps.members[m.Repository]
		routes := newAPIRoutes(mo)
		routes["/api/v0/status"] = newStatusHandler(mo)
		api := newChangesAPI(mo)
		api.routes(routes)
		routes[projectChangesImpactPath] = http.HandlerFunc(api.projectImpact)
		if mo.noCheckout {
			// Code peek reads the member's files: the one endpoint that
			// cannot answer from the store (ref bases and ref jobs refuse
			// inside the change handlers).
			noCheckout := mo.noCheckoutError("code peek (/api/v0/source)")
			routes["/api/v0/source"] = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeError(w, noCheckout.status, noCheckout.code, noCheckout.message)
			})
		}
		apis = append(apis, api)
		for p, h := range routes {
			byPath[p][m.Repository] = h
		}
	}
	out := map[string]http.Handler{}
	for p, handlers := range byPath {
		out[p] = memberDispatch{project: ps, handlers: handlers}
	}
	out[openapiPath] = openapiMembersHandler{ps: ps, dispatch: out[openapiPath]}
	out[searchPath] = searchProjectHandler{ps: ps, dispatch: out[searchPath]}
	out[unreadPath] = unreadProjectHandler{ps: ps, dispatch: out[unreadPath]}
	out[indexRunPath] = runs
	out[projectPath] = projectHandler{ps: ps}
	out[projectCallersPath] = projectQueryHandler{ps: ps, command: "callers"}
	out[projectImpactPath] = projectQueryHandler{ps: ps, command: "impact"}
	out[projectLinkChangesPath] = projectLinkChangesHandler{ps: ps}
	return out, apis
}

// memberDispatch sends a request to the handler of the member its
// repository parameter names.
type memberDispatch struct {
	project  *projectState
	handlers map[string]http.Handler
}

func (d memberDispatch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m, err := d.project.member(r.URL.Query())
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.code, ae.message)
		return
	}
	d.handlers[m.Repository].ServeHTTP(w, r)
}

// GET /api/v0/project

type projectResponse struct {
	APIVersion    int                `json:"api_version"`
	Project       string             `json:"project"`
	ServerVersion string             `json:"server_version"`
	Members       []projectMemberDoc `json:"members"`
	LinkPass      projectPassDoc     `json:"link_pass"`
	Checks        []projectCheck     `json:"checks"`
	// Declarations, Ambiguous and Conflicts explain the links for the web
	// UI's Project view (T022 B9; projectview.go).
	Declarations projectDeclarationsDoc `json:"declarations"`
	Ambiguous    projectAmbiguousDoc    `json:"ambiguous"`
	Conflicts    []projectConflictDoc   `json:"conflicts"`
}

type projectMemberDoc struct {
	Repository string `json:"repository"`
	// RepositorySource says where the ID was confirmed (ID source): the
	// manifest, or the member's own graphvein.yaml.
	RepositorySource string `json:"repository_source"`
	// Path is the checkout relative to the manifest's directory, slash
	// separated; null when no path is given.
	Path       *string         `json:"path"`
	PathSource *string         `json:"path_source"`
	Checkout   bool            `json:"checkout"`
	Remote     *remoteDoc      `json:"remote"`
	Extractor  extractorStatus `json:"extractor"`
	Index      *lastRun        `json:"index"`
	Freshness  freshnessStatus `json:"freshness"`
	Links      memberLinksDoc  `json:"links"`
}

// remoteDoc is the checkout's git remote: metadata only, never the ID.
type remoteDoc struct {
	// URL is the origin remote without credentials; null without one.
	URL *string `json:"url"`
	// RepositoryRoot is false when the checkout is a folder inside a git
	// repository (a monorepo member), whose remote is the whole
	// repository's.
	RepositoryRoot bool `json:"repository_root"`
}

// memberLinksDoc is a member's state against the last link pass.
type memberLinksDoc struct {
	InPass bool `json:"in_pass"`
	// PassRun is the member's run the pass linked; null when the pass
	// found no stored index of it.
	PassRun    *string `json:"pass_run"`
	IndexError *string `json:"index_error"`
	// Links counts the links with this member at either end, Stale those
	// of them that are stale, with the distinct reasons.
	Links        int      `json:"links"`
	Stale        int      `json:"stale"`
	StaleReasons []string `json:"stale_reasons"`
}

type projectPassDoc struct {
	// State is none (no pass stored), fresh or stale.
	State      string         `json:"state"`
	Pass       int64          `json:"pass"`
	Links      int            `json:"links"`
	StaleLinks int            `json:"stale_links"`
	Outcomes   map[string]int `json:"outcomes"`
	// StaleReasons say why the pass would link differently if run now.
	StaleReasons []string `json:"stale_reasons"`
}

// projectCheck is a finding about the project's members that is not an
// error but deserves a look.
type projectCheck struct {
	Check   string   `json:"check"`
	Members []string `json:"members"`
	Message string   `json:"message"`
}

// checkSameRemote: two members are separate repository roots with the same
// origin remote URL.
const checkSameRemote = "same_remote"

type projectHandler struct{ ps *projectState }

func (h projectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := h.compute(ctx)
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	if err != nil {
		logFailure(h.ps.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h projectHandler) compute(ctx context.Context) (projectResponse, error) {
	ps := h.ps
	out := projectResponse{Project: ps.project.Name, ServerVersion: ps.opt.Version, Members: []projectMemberDoc{}, Checks: []projectCheck{}}
	view, err := ps.view(ctx)
	if err != nil {
		return out, err
	}
	out.LinkPass = projectPassDoc{State: "none", Outcomes: map[string]int{}, StaleReasons: []string{}}
	type memberCount struct {
		links, stale int
		reasons      []string
	}
	counts := map[string]*memberCount{}
	for _, name := range ps.names {
		counts[name] = &memberCount{reasons: []string{}}
	}
	if view.Stored {
		out.LinkPass.State, out.LinkPass.Pass, out.LinkPass.Links = stateFresh, view.Set.Pass, len(view.Set.Links)
		out.LinkPass.StaleReasons = append(out.LinkPass.StaleReasons, view.PassReasons()...)
		if len(out.LinkPass.StaleReasons) > 0 {
			out.LinkPass.State = stateStale
		}
		for _, o := range view.Set.Outcomes {
			out.LinkPass.Outcomes[o.Status]++
		}
		for _, l := range view.Set.Links {
			reasons := view.StaleReasons(l)
			if len(reasons) > 0 {
				out.LinkPass.StaleLinks++
			}
			ends := []string{l.Source.Repository}
			if l.Target.Repository != l.Source.Repository {
				ends = append(ends, l.Target.Repository)
			}
			for _, repo := range ends {
				c := counts[repo]
				if c == nil {
					continue
				}
				c.links++
				if len(reasons) > 0 {
					c.stale++
					for _, why := range reasons {
						if !contains(c.reasons, why) {
							c.reasons = append(c.reasons, why)
						}
					}
				}
			}
		}
	}
	inPass := map[string]store.ProjectMember{}
	for _, m := range view.Set.Members {
		inPass[m.Repository] = m
	}
	remotes := map[string]gitref.Remote{}
	for _, m := range ps.project.Members {
		doc, remote, err := h.member(ctx, m)
		if err != nil {
			return out, err
		}
		if remote != nil {
			remotes[m.Repository] = *remote
		}
		c := counts[m.Repository]
		doc.Links = memberLinksDoc{Links: c.links, Stale: c.stale, StaleReasons: c.reasons}
		if pm, ok := inPass[m.Repository]; ok {
			doc.Links.InPass = true
			if pm.Run != "" {
				doc.Links.PassRun = newPtr(pm.Run)
			}
			if pm.IndexError != "" {
				doc.Links.IndexError = newPtr(pm.IndexError)
			}
		}
		out.Members = append(out.Members, doc)
	}
	out.Checks = append(out.Checks, sameRemoteChecks(ps.names, remotes)...)
	if err := ps.projectExplained(ctx, view, &out); err != nil {
		return out, err
	}
	return out, nil
}

// member describes one member: identity source, checkout, remote, index
// and freshness of its checkout.
func (h projectHandler) member(ctx context.Context, m config.Member) (projectMemberDoc, *gitref.Remote, error) {
	ps := h.ps
	doc := projectMemberDoc{Repository: m.Repository, RepositorySource: m.RepositorySource, Checkout: m.Checkout}
	if m.Path != "" {
		p := m.Path
		if rel, err := filepath.Rel(ps.project.Dir, m.Path); err == nil {
			p = rel
		}
		doc.Path = newPtr(filepath.ToSlash(p))
	}
	if m.PathSource != "" {
		doc.PathSource = newPtr(m.PathSource)
	}
	checkedAt := ps.opt.now()
	var changed []string
	freshErr := errors.New("no checkout here: freshness needs the member's files")
	var remote *gitref.Remote
	if m.Checkout {
		changed, freshErr = index.Freshness(ctx, m.Path, m.Repository, ps.opt.Store)
		if r, err := gitref.RemoteOf(ctx, m.Path); err == nil {
			remote = &r
			doc.Remote = &remoteDoc{RepositoryRoot: samePath(r.TopLevel, m.Path)}
			if r.URL != "" {
				doc.Remote.URL = newPtr(r.URL)
			}
		}
	}
	runs, err := ps.opt.Store.Runs(ctx, m.Repository)
	if err != nil {
		return doc, nil, fmt.Errorf("reading runs of %s: %w", m.Repository, err)
	}
	doc.Index = latestRun(runs)
	doc.Extractor = extractorFor(index.Version, doc.Index)
	doc.Freshness = freshnessFor(changed, freshErr, len(runs) > 0)
	doc.Freshness.CheckedAt = checkedAt.UTC().Format(time.RFC3339Nano)
	if m.Checkout {
		doc.Freshness.Settings = settingsStatusFor(runs, settingsAt(m.Path))
	} else {
		doc.Freshness.Settings = settingsStatusFor(runs, noCheckoutSettings)
	}
	return doc, remote, nil
}

// samePath compares two directories after resolving symbolic links, so a
// checkout under a linked folder still counts as its repository's root.
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// sameRemoteChecks reports members that are separate repository roots
// with the same origin URL (compared without scheme, credentials, case of
// the host, a trailing slash or .git): the same repository checked out
// twice under two IDs, whose routes would all be ambiguous between the
// two. Folders of one repository (a monorepo) share a remote legitimately
// and are not reported.
func sameRemoteChecks(names []string, remotes map[string]gitref.Remote) []projectCheck {
	byURL := map[string][]string{}
	var order []string
	for _, name := range names {
		r, ok := remotes[name]
		if !ok || r.URL == "" {
			continue
		}
		key := normalRemote(r.URL)
		if len(byURL[key]) == 0 {
			order = append(order, key)
		}
		byURL[key] = append(byURL[key], name)
	}
	var out []projectCheck
	for _, key := range order {
		members := byURL[key]
		var roots []string
		tops := map[string]bool{}
		for _, name := range members {
			r := remotes[name]
			if !tops[r.TopLevel] {
				tops[r.TopLevel] = true
				roots = append(roots, name)
			}
		}
		if len(roots) < 2 {
			continue
		}
		out = append(out, projectCheck{Check: checkSameRemote, Members: roots,
			Message: fmt.Sprintf("members %s are separate checkouts with the same remote URL; if they are one repository, its facts are indexed under several IDs and its routes are ambiguous between them", strings.Join(roots, ", "))})
	}
	return out
}

// normalRemote reduces a remote URL to host and path for comparison:
// https://Host/o/r.git, ssh://git@host/o/r and git@host:o/r compare equal.
func normalRemote(u string) string {
	u = gitref.StripCredentials(u)
	if _, rest, ok := strings.Cut(u, "://"); ok {
		u = rest
	} else if at := strings.Index(u, "@"); at >= 0 && strings.Contains(u[at:], ":") {
		u = strings.Replace(u[at+1:], ":", "/", 1)
	}
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	host, path, _ := strings.Cut(u, "/")
	if h, _, ok := strings.Cut(host, ":"); ok {
		host = h // a port in an ssh URL
	}
	return strings.ToLower(host) + "/" + path
}

// GET /api/v0/project/callers and /api/v0/project/impact

// projectQueryHandler runs graphvein callers or impact --project
// (query.ExecuteProject): term (an identity or a name) is looked up in the
// member repository names, or in every member; an ambiguous term answers
// 200 with error and candidates, as the CLI's JSON does.
type projectQueryHandler struct {
	ps      *projectState
	command string
}

type projectQueryResponse struct {
	APIVersion int `json:"api_version"`
	query.ProjectResponse
}

func (h projectQueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	body, err := func() (any, error) {
		term, present, err := param(q, "term")
		if err != nil {
			return nil, err
		}
		if !present || term == "" {
			return nil, badRequest("parameter term is required: an identity or a name")
		}
		depth := projectDefaultCallerDepth
		if h.command == "impact" {
			depth = projectDefaultImpactDepth
		}
		if d, err := intParam(q, "depth", 1, projectMaxDepth); err != nil {
			return nil, err
		} else if d != 0 {
			depth = d
		}
		repository := ""
		if _, present := q["repository"]; present {
			m, err := h.ps.member(q)
			if err != nil {
				return nil, err
			}
			repository = m.Repository
		}
		res, err := query.ExecuteProject(ctx, h.ps.opt.Store, query.ProjectOptions{Command: h.command, Term: term, Repository: repository, Depth: depth, Project: h.ps.project})
		if err != nil {
			return nil, err
		}
		return projectQueryResponse{ProjectResponse: res}, nil
	}()
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	var ae *apiError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, body)
	case errors.As(err, &ae):
		writeError(w, ae.status, ae.code, ae.message)
	default:
		logFailure(h.ps.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}

// GET /api/v0/project/links/changes

// projectLinkChangesResponse is the link journal diff of graphvein changes
// --project: links added, removed or changed between two kept passes.
// Each member's own changes are /api/v0/changes/summary?repository=.
type projectLinkChangesResponse struct {
	APIVersion int    `json:"api_version"`
	Project    string `json:"project"`
	Base       string `json:"base"`
	// State is "compared" when Links holds a diff; "same_pass" when the
	// base is the newest kept pass, so nothing was compared (never "no
	// change": the MCP link_changes tool reports the same state); and
	// "no_comparison" when fewer than two passes are kept.
	State string                   `json:"state"`
	Links *changes.ProjectLinkDiff `json:"links"`
	// LinksNote says why Links is null (state same_pass or no_comparison).
	LinksNote *string `json:"links_note"`
}

// Link change states of projectLinkChangesResponse.State.
const (
	linkChangesCompared     = "compared"
	linkChangesSamePass     = "same_pass"
	linkChangesNoComparison = "no_comparison"
)

type projectLinkChangesHandler struct{ ps *projectState }

func (h projectLinkChangesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body, err := h.compute(ctx, r.URL.Query())
	if ctx.Err() != nil {
		cancelled(w, ctx)
		return
	}
	if err != nil {
		status, code := changesError(err)
		if errors.Is(err, changes.ErrPassUnknown) {
			status, code = http.StatusNotFound, "base_unknown"
		}
		if status == http.StatusInternalServerError {
			logFailure(h.ps.opt.logger(), r, err)
		}
		writeError(w, status, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (h projectLinkChangesHandler) compute(ctx context.Context, q url.Values) (projectLinkChangesResponse, error) {
	out := projectLinkChangesResponse{Project: h.ps.project.Name, Base: changes.BasePrevious, State: linkChangesNoComparison}
	raw, _, err := param(q, "base")
	if err != nil {
		return out, err
	}
	runs, err := h.ps.opt.Store.ProjectLinkRuns(ctx, h.ps.project.Name)
	if err != nil {
		return out, fmt.Errorf("reading the link journal: %w", err)
	}
	var from int64
	switch {
	case raw == "" || raw == changes.BasePrevious:
		if len(runs) >= 2 {
			from = runs[len(runs)-2].Seq
		}
	case strings.HasPrefix(raw, "pass:"):
		n, err := strconv.ParseInt(strings.TrimPrefix(raw, "pass:"), 10, 64)
		if err != nil || n <= 0 {
			return out, badRequest("base %q: want previous or pass:N", raw)
		}
		out.Base, from = raw, n
	default:
		return out, badRequest("base %q: want previous or pass:N", raw)
	}
	switch {
	case len(runs) == 0:
		out.LinksNote = newPtr("no link pass yet; run graphvein index --project")
	case from == 0:
		out.LinksNote = newPtr("only one link pass is kept; nothing to compare it with")
	case from == runs[len(runs)-1].Seq:
		// Comparing the newest pass with itself would list no change,
		// which is not proof that no link changed.
		out.State = linkChangesSamePass
		out.LinksNote = newPtr(fmt.Sprintf("the base is pass %d, the newest kept link pass: nothing compared, not proof that no link changed; use base previous or an older pass", from))
	default:
		diff, err := changes.DiffProjectLinks(h.ps.project.Name, runs, from, runs[len(runs)-1].Seq)
		if err != nil {
			return out, err
		}
		out.State, out.Links = linkChangesCompared, &diff
	}
	return out, nil
}

// Cross-repository callers of an API endpoint (GET /api/v0/api/endpoint in
// project mode).

// projectEndpointDoc lists what the project's stored links say about one
// route of a member: client calls of other members linked to it
// (calls_route), OpenAPI operations documenting it (documents_route), and
// calls the pass left ambiguous with this route among the candidates.
type projectEndpointDoc struct {
	Project  string           `json:"project"`
	LinkPass projectPassBrief `json:"link_pass"`
	// LinksPrecision is the scope label of every link listed here: never
	// precise (docs/extract/precision.md "Cross-repository links").
	LinksPrecision scopeLabel       `json:"links_precision"`
	Callers        []projectLinkDoc `json:"callers"`
	Operations     []projectLinkDoc `json:"operations"`
	// PossibleCallers are the sources of other members the pass left
	// ambiguous with this route among the candidates, or incomplete with
	// this repository among the candidate providers (T022 review):
	// "may be called by", never callers.
	PossibleCallers []possibleSource `json:"possible_callers"`
	// DeclarationConflicts are in-repository callers whose link a scope
	// declaration contradicts (T022 review).
	DeclarationConflicts []callConflictDoc `json:"declaration_conflicts"`
}

type projectPassBrief struct {
	State        string   `json:"state"`
	Pass         int64    `json:"pass"`
	StaleReasons []string `json:"stale_reasons"`
}

// projectLinkDoc is one cross-repository link into the endpoint. Its
// source node is presented through its member's checkout (IDE link, file
// state); null when that member no longer stores it (a stale link).
type projectLinkDoc struct {
	Repository   string              `json:"repository"`
	Identity     string              `json:"identity"`
	Node         *presentedNode      `json:"node"`
	Owner        *ownerRef           `json:"owner"`
	Kind         string              `json:"kind"`
	Provenance   string              `json:"provenance"`
	Confidence   float64             `json:"confidence"`
	Evidence     []contract.Evidence `json:"evidence"`
	Assumption   string              `json:"assumption"`
	Assumes      []string            `json:"assumes,omitempty"`
	Stale        bool                `json:"stale"`
	StaleReasons []string            `json:"stale_reasons"`
	// IDEStatus (additive) says how the node's editor link was built: the
	// member's own editor precedence (the browser's editor= first, then
	// the member's graphvein.yaml, then the built-in default), so the view
	// names and treats the link like every other editor link instead of
	// assuming a status. Absent when the node is not presented through a
	// member's checkout (no node, or a member without options here).
	IDEStatus *ideStatus `json:"ide_status,omitempty"`
	// ShapeDifferences (calls_route links, additive; T029 W4) compares
	// what the call sends with what the route reads, labelled as a
	// cross-repository link; absent when nothing is compared.
	ShapeDifferences *shapeDifferencesDoc `json:"shape_differences,omitempty"`
}

// possibleSource is a source of another member that may reach the route
// without a link (projectlink.PossibleCaller).
type possibleSource struct {
	Repository            string   `json:"repository"`
	Identity              string   `json:"identity"`
	Kind                  string   `json:"kind"`
	Row                   string   `json:"row"`
	Status                string   `json:"status"`
	Wording               string   `json:"wording"`
	Reason                string   `json:"reason"`
	CandidateCount        int      `json:"candidate_count"`
	CandidateRepositories []string `json:"candidate_repositories"`
}

// callConflictDoc marks a call whose in-repository link a link scope
// declaration contradicts: scopes never override such a link, so views
// show it as "linked inside the repository; the declaration names
// another scope" (projectlink.Conflict).
type callConflictDoc struct {
	Call        string   `json:"call"`
	Consumer    string   `json:"consumer"`
	Declared    []string `json:"declared"`
	Target      string   `json:"target"`
	TargetScope string   `json:"target_scope"`
}

// projectEndpoint reads the links into the route (repository, id).
func (ps *projectState) projectEndpoint(ctx context.Context, repository string, route contract.Node, routePresenter *presenter, editor *editorChoice) (*projectEndpointDoc, error) {
	id := route.Identity
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	label, err := ps.linksLabel(ctx, repository)
	if err != nil {
		return nil, err
	}
	out := &projectEndpointDoc{Project: ps.project.Name, Callers: []projectLinkDoc{}, Operations: []projectLinkDoc{}, PossibleCallers: []possibleSource{}, DeclarationConflicts: []callConflictDoc{},
		LinkPass: projectPassBrief{State: "none", StaleReasons: []string{}}, LinksPrecision: label}
	conflicts, err := ps.conflictsOf(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range conflicts {
		if c.Target.Repository == repository && c.Target.Identity == id && c.Call.Repository == repository {
			out.DeclarationConflicts = append(out.DeclarationConflicts, callConflictDoc{Call: c.Call.Identity, Consumer: c.Consumer, Declared: c.Declared, Target: c.Target.Identity, TargetScope: c.TargetScope})
		}
	}
	if !view.Stored {
		return out, nil
	}
	out.LinkPass.State, out.LinkPass.Pass = stateFresh, view.Set.Pass
	out.LinkPass.StaleReasons = append(out.LinkPass.StaleReasons, view.PassReasons()...)
	if len(out.LinkPass.StaleReasons) > 0 {
		out.LinkPass.State = stateStale
	}
	links := view.LinksTo(repository, id)
	bySource := map[string][]string{}
	for _, l := range links {
		bySource[l.Source.Repository] = append(bySource[l.Source.Repository], l.Source.Identity)
	}
	type presented struct {
		nodes  map[string]contract.Node
		owners map[string]*ownerRef
		p      *presenter
	}
	sources := map[string]presented{}
	for repo, ids := range bySource {
		rd := &reader{ctx: ctx, s: ps.opt.Store, repo: repo}
		nodes, err := rd.byIdentity(ids)
		if err != nil {
			return nil, err
		}
		owners, err := rd.owners(ids)
		if err != nil {
			return nil, err
		}
		var p *presenter
		if mo, ok := ps.members[repo]; ok && mo.Root != "" {
			hashes, err := rd.fileHashes()
			if err != nil {
				return nil, err
			}
			p = (&apiHandler{opt: mo}).newPresenter(hashes, editor)
		}
		sources[repo] = presented{nodes: nodes, owners: owners, p: p}
	}
	for _, l := range links {
		src := sources[l.Source.Repository]
		doc := projectLinkDoc{Repository: l.Source.Repository, Identity: l.Source.Identity, Owner: src.owners[l.Source.Identity], Kind: l.Kind,
			Provenance: l.Provenance, Confidence: l.Confidence, Evidence: l.Evidence, Assumption: l.Assumption, Assumes: l.Assumes, StaleReasons: []string{}}
		if doc.Evidence == nil {
			doc.Evidence = []contract.Evidence{}
		}
		if n, ok := src.nodes[l.Source.Identity]; ok {
			var pn presentedNode
			if src.p != nil {
				pn = src.p.call(n)
				status := src.p.ide
				doc.IDEStatus = &status
			} else {
				pn = presentedNode{Node: query.Node{Node: n}}
			}
			doc.Node = &pn
			if l.Kind == "calls_route" {
				doc.ShapeDifferences = shapeDifferences(route, n, presenterLinker(routePresenter), presenterLinker(src.p), true)
			}
		}
		doc.StaleReasons = append(doc.StaleReasons, view.StaleReasons(l)...)
		doc.Stale = len(doc.StaleReasons) > 0
		switch l.Kind {
		case "calls_route":
			out.Callers = append(out.Callers, doc)
		case "documents_route":
			out.Operations = append(out.Operations, doc)
		}
	}
	for _, pc := range view.PossibleCallers(repository, id) {
		if _, ok := ps.members[pc.Source.Repository]; !ok {
			continue
		}
		out.PossibleCallers = append(out.PossibleCallers, possibleSource{Repository: pc.Source.Repository, Identity: pc.Source.Identity, Kind: pc.Kind, Row: pc.Row,
			Status: pc.Status, Wording: pc.Wording, Reason: pc.Reason, CandidateCount: pc.CandidateCount, CandidateRepositories: pc.CandidateRepositories})
	}
	return out, nil
}

// linksLabel is the scope label of the cross-repository links shown with
// repository's facts, decided like every family label from the member's
// latest run (precision.FamilyCrossRepositoryLinks never earns a row).
func (ps *projectState) linksLabel(ctx context.Context, repository string) (scopeLabel, error) {
	runs, err := ps.opt.Store.Runs(ctx, repository)
	if err != nil {
		return scopeLabel{}, fmt.Errorf("reading runs of %s: %w", repository, err)
	}
	run, _ := precisionRun(runs)
	return labelOf(run, precision.Marker{Family: precision.FamilyCrossRepositoryLinks}), nil
}

// projectCallerCounts counts the calls_route links into each route of
// repository.
func (ps *projectState) projectCallerCounts(ctx context.Context, repository string) (map[string]int, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	seen := map[store.ProjectNodeRef]map[string]bool{}
	for _, l := range view.Set.Links {
		if l.Kind != "calls_route" || l.Target.Repository != repository {
			continue
		}
		t := store.ProjectNodeRef{Repository: l.Target.Repository, Identity: l.Target.Identity}
		if seen[t] == nil {
			seen[t] = map[string]bool{}
		}
		key := l.Source.Repository + "\x00" + l.Source.Identity
		if !seen[t][key] {
			seen[t][key] = true
			out[l.Target.Identity]++
		}
	}
	return out, nil
}

// projectPossibleCounts counts, per route of ids in repository, the
// calls of other members that may call it without a link (ambiguous or
// incomplete outcomes, projectlink.View.PossibleCallers; T022 review).
func (ps *projectState) projectPossibleCounts(ctx context.Context, repository string, ids []string) (map[string]int, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, id := range ids {
		for _, pc := range view.PossibleCallers(repository, id) {
			if _, ok := ps.members[pc.Source.Repository]; ok && pc.Kind == projectlink.KindCallsRoute {
				out[id]++
			}
		}
	}
	return out, nil
}

// projectLinkedCalls returns the calls of repository with a calls_route
// project link into another member.
func (ps *projectState) projectLinkedCalls(ctx context.Context, repository string) (map[string]bool, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, l := range view.Set.Links {
		if l.Kind == projectlink.KindCallsRoute && l.Source.Repository == repository && l.Target.Repository != repository {
			out[l.Source.Identity] = true
		}
	}
	return out, nil
}

// Project-scope call statuses (T022 MR-UI): in project mode the API
// section's headline counts read the project's view of a call, not only
// its repository's.
const (
	// statusLinkedAcross is the project status of a call the repository
	// does not link but a stored project link connects to a route of
	// another member.
	statusLinkedAcross = "linked_across"
	// statusAmbiguousAcross is the project status of a call the
	// repository does not link and the link pass found several possible
	// routes for in other members (outcome ambiguous), never a link.
	statusAmbiguousAcross = "ambiguous_across"
)

// projectCallStatus returns a call's status in the project scope: a call
// linked inside its repository stays "linked" (a fact there); otherwise
// a project link makes it linked_across, an ambiguous outcome without a
// link ambiguous_across, and anything else keeps the repository's
// status (unmatched in the repository is unmatched in the project).
func projectCallStatus(c contract.Node, linked, ambiguous map[string]bool) string {
	s := statusOf(c)
	switch {
	case s == "linked":
		return s
	case linked[c.Identity]:
		return statusLinkedAcross
	case ambiguous[c.Identity]:
		return statusAmbiguousAcross
	}
	return s
}

// projectAmbiguousCalls returns the calls of repository whose link pass
// outcome is ambiguous (several possible routes in other members).
func (ps *projectState) projectAmbiguousCalls(ctx context.Context, repository string) (map[string]bool, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, o := range view.Set.Outcomes {
		if o.Kind == projectlink.KindCallsRoute && o.Source.Repository == repository && o.Status == projectlink.StatusAmbiguous {
			out[o.Source.Identity] = true
		}
	}
	return out, nil
}

// projectStatusFunc returns projectCallStatus bound to repository's
// stored links and outcomes.
func (ps *projectState) projectStatusFunc(ctx context.Context, repository string) (func(contract.Node) string, error) {
	linked, err := ps.projectLinkedCalls(ctx, repository)
	if err != nil {
		return nil, err
	}
	ambiguous, err := ps.projectAmbiguousCalls(ctx, repository)
	if err != nil {
		return nil, err
	}
	return func(c contract.Node) string { return projectCallStatus(c, linked, ambiguous) }, nil
}

// projectScope is the project scope of a member's change impact.
func (ps *projectState) projectScope(ctx context.Context) (*changes.ProjectScope, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	return &changes.ProjectScope{Project: ps.project.Name, Members: ps.names, Links: view}, nil
}

// Project links of client calls (GET /api/v0/api/calls in project mode).

// callProjectDoc is what the stored link pass says about one call.
type callProjectDoc struct {
	// LinkPass is the pass the links come from (state none without one).
	LinkPass projectPassBrief `json:"link_pass"`
	// LinksPrecision is the scope label of the call's links: never
	// precise (docs/extract/precision.md "Cross-repository links").
	LinksPrecision scopeLabel `json:"links_precision"`
	// Links are the call's project links (calls_route into another
	// member's route, or a scope overlay into its own), in key order.
	Links []callLinkDoc `json:"links"`
	// Outcomes are the pass's results for the call, one per alternative
	// for a call with several: why it has a link or none.
	Outcomes []callOutcomeDoc `json:"outcomes"`
	// DeclarationConflict is set when the call's in-repository link
	// contradicts a scope declaration (T022 review): "linked inside the
	// repository; the declaration names another scope". Null otherwise.
	DeclarationConflict *callConflictDoc `json:"declaration_conflict"`
}

// callLinkDoc is one project link from a call; the target route is named
// by (repository, identity) with its name and place.
type callLinkDoc struct {
	Target       projectNodeBrief    `json:"target"`
	Kind         string              `json:"kind"`
	Provenance   string              `json:"provenance"`
	Confidence   float64             `json:"confidence"`
	Evidence     []contract.Evidence `json:"evidence"`
	Assumption   string              `json:"assumption"`
	Assumes      []string            `json:"assumes,omitempty"`
	Stale        bool                `json:"stale"`
	StaleReasons []string            `json:"stale_reasons"`
}

type callOutcomeDoc struct {
	Template       string                 `json:"template"`
	Row            string                 `json:"row"`
	Status         string                 `json:"status"`
	Reason         string                 `json:"reason"`
	CandidateCount int                    `json:"candidate_count"`
	Candidates     []store.ProjectNodeRef `json:"candidates"`
}

// callLinks reads the project links and outcomes of repository's calls ids.
// Every call gets a document, with empty lists when the pass has nothing
// on it, so a view can tell "no project link" from "not asked".
func (ps *projectState) callLinks(ctx context.Context, repository string, ids []string) (map[string]*callProjectDoc, error) {
	view, err := ps.view(ctx)
	if err != nil {
		return nil, err
	}
	brief := projectPassBrief{State: "none", StaleReasons: []string{}}
	if view.Stored {
		brief.State, brief.Pass = stateFresh, view.Set.Pass
		brief.StaleReasons = append(brief.StaleReasons, view.PassReasons()...)
		if len(brief.StaleReasons) > 0 {
			brief.State = stateStale
		}
	}
	label, err := ps.linksLabel(ctx, repository)
	if err != nil {
		return nil, err
	}
	out := map[string]*callProjectDoc{}
	for _, id := range ids {
		out[id] = &callProjectDoc{LinkPass: brief, LinksPrecision: label, Links: []callLinkDoc{}, Outcomes: []callOutcomeDoc{}}
	}
	conflicts, err := ps.conflictsOf(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range conflicts {
		if doc, ok := out[c.Call.Identity]; ok && c.Call.Repository == repository && doc.DeclarationConflict == nil {
			doc.DeclarationConflict = &callConflictDoc{Call: c.Call.Identity, Consumer: c.Consumer, Declared: c.Declared, Target: c.Target.Identity, TargetScope: c.TargetScope}
		}
	}
	if !view.Stored {
		return out, nil
	}
	b := newBriefs(ctx, ps.opt.Store)
	for _, id := range ids {
		for _, l := range view.LinksFrom(repository, id) {
			if l.Kind != projectlink.KindCallsRoute {
				continue
			}
			target := store.ProjectNodeRef{Repository: l.Target.Repository, Identity: l.Target.Identity}
			b.want(target)
			doc := callLinkDoc{Target: projectNodeBrief{Repository: target.Repository, Identity: target.Identity}, Kind: l.Kind, Provenance: l.Provenance,
				Confidence: l.Confidence, Evidence: l.Evidence, Assumption: l.Assumption, Assumes: l.Assumes, StaleReasons: []string{}}
			if doc.Evidence == nil {
				doc.Evidence = []contract.Evidence{}
			}
			doc.StaleReasons = append(doc.StaleReasons, view.StaleReasons(l)...)
			doc.Stale = len(doc.StaleReasons) > 0
			out[id].Links = append(out[id].Links, doc)
		}
	}
	for _, o := range view.Set.Outcomes {
		doc, ok := out[o.Source.Identity]
		if !ok || o.Source.Repository != repository || o.Kind != projectlink.KindCallsRoute {
			continue
		}
		count := o.CandidateCount
		if count < len(o.Candidates) {
			count = len(o.Candidates)
		}
		candidates := o.Candidates
		if candidates == nil {
			candidates = []store.ProjectNodeRef{}
		}
		doc.Outcomes = append(doc.Outcomes, callOutcomeDoc{Template: o.Template, Row: o.Row, Status: o.Status, Reason: o.Reason, CandidateCount: count, Candidates: candidates})
	}
	if err := b.read(); err != nil {
		return nil, err
	}
	for _, doc := range out {
		for i := range doc.Links {
			t := doc.Links[i].Target
			doc.Links[i].Target = b.of(store.ProjectNodeRef{Repository: t.Repository, Identity: t.Identity})
		}
	}
	return out, nil
}

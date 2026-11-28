package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	routepaths "github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/store"
)

// This file holds the API section endpoints (ADR 0042, docs/cli/serve.md):
// GET /api/v0/api/endpoints, /api/v0/api/endpoint and /api/v0/api/calls;
// flow.go adds /api/v0/api/flow (ADR 0045).
// They are read-only and uncached: every request reads the store after it
// arrives.

// errIndexChanged reports that the runs changed during both read attempts.
var errIndexChanged = errors.New("the index changed while reading; retry")

// apiError is a client error answered with the error envelope.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.message }

func badRequest(format string, args ...any) error {
	return &apiError{http.StatusBadRequest, "bad_request", fmt.Sprintf(format, args...)}
}

// apiHandler serves one API section endpoint. compute parses the request
// (failing with an *apiError before any read) and returns a read function;
// the handler runs that function inside the consistency check.
type apiHandler struct {
	opt     Options
	compute func(h *apiHandler, q url.Values) (readFunc, error)
}

// readFunc reads one response body. runs are the runs read before it.
type readFunc func(ctx context.Context, rd *reader, runs []store.Run) (any, error)

func (h *apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	read, err := h.compute(h, r.URL.Query())
	var body any
	if err == nil {
		body, err = h.consistent(ctx, read)
	}
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
	case errors.Is(err, errIndexChanged):
		writeError(w, http.StatusServiceUnavailable, "index_changed", err.Error())
	default:
		logFailure(h.opt.logger(), r, err)
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}

// consistent runs read between two reads of the runs. Store reads are
// separate transactions, so an ingest may commit between them; when the
// latest run or the run count differs afterwards, the whole read is
// repeated once, then the request fails with errIndexChanged.
func (h *apiHandler) consistent(ctx context.Context, read readFunc) (any, error) {
	rd := &reader{ctx: ctx, s: h.opt.Store, repo: h.opt.Repository}
	for attempt := 0; attempt < 2; attempt++ {
		before, err := rd.runs()
		if err != nil {
			return nil, err
		}
		body, err := read(ctx, rd, before)
		if err != nil {
			return nil, err
		}
		after, err := rd.runs()
		if err != nil {
			return nil, err
		}
		if sameRuns(before, after) {
			return body, nil
		}
	}
	return nil, errIndexChanged
}

// sameRuns compares the run count and the latest run's ID and finish time.
func sameRuns(a, b []store.Run) bool {
	if len(a) != len(b) {
		return false
	}
	la, lb := latestStoreRun(a), latestStoreRun(b)
	if la == nil || lb == nil {
		return la == nil && lb == nil
	}
	return la.ID == lb.ID && la.FinishedAt == lb.FinishedAt
}

// reader wraps the store reads of one request with error context.
type reader struct {
	ctx  context.Context
	s    store.Store
	repo string
}

func (rd *reader) runs() ([]store.Run, error) {
	runs, err := rd.s.Runs(rd.ctx, rd.repo)
	if err != nil {
		return nil, fmt.Errorf("reading runs: %w", err)
	}
	return runs, nil
}

// withheldSites reads the sites of the route registrations whose path the
// Go linker withholds (couldn't-read row CR-44), so a view says "path
// withheld" for them rather than "path not known".
func (rd *reader) withheldSites() (map[routepaths.RouteSite]bool, error) {
	diagnostics, err := rd.s.DiagnosticsWithCodes(rd.ctx, rd.repo, []string{routepaths.WithheldCode}, nil)
	if err != nil {
		return nil, fmt.Errorf("reading withheld route registrations: %w", err)
	}
	return routepaths.WithheldSites(diagnostics), nil
}

func (rd *reader) nodes(kind string) ([]contract.Node, error) {
	nodes, err := rd.s.Nodes(rd.ctx, rd.repo, store.Query{Kind: kind})
	if err != nil {
		return nil, fmt.Errorf("reading %s nodes: %w", kind, err)
	}
	return nodes, nil
}

// columnsWithoutModelClaims reads every column node, a model column
// without its claims: /schema/sets never shows them, and decoding every
// model claim made langflow's /schema/sets about 30 % slower (ADR 0057).
func (rd *reader) columnsWithoutModelClaims() ([]contract.Node, error) {
	nodes, err := rd.s.NodesWithoutModelClaims(rd.ctx, rd.repo, store.Query{Kind: "column"})
	if err != nil {
		return nil, fmt.Errorf("reading column nodes: %w", err)
	}
	return nodes, nil
}

func (rd *reader) byIdentity(ids []string) (map[string]contract.Node, error) {
	out := map[string]contract.Node{}
	if len(ids) == 0 {
		return out, nil
	}
	nodes, err := rd.s.NodesByIdentity(rd.ctx, rd.repo, distinct(ids))
	if err != nil {
		return nil, fmt.Errorf("reading nodes by identity: %w", err)
	}
	for _, n := range nodes {
		out[n.Identity] = n
	}
	return out, nil
}

func (rd *reader) edges(ids []string, kind string, dir store.Direction) ([]contract.Edge, error) {
	if len(ids) == 0 {
		return []contract.Edge{}, nil
	}
	edges, err := rd.s.Edges(rd.ctx, rd.repo, ids, []string{kind}, dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s %s edges: %w", dir, kind, err)
	}
	return edges, nil
}

func (rd *reader) fileHashes() (map[string]string, error) {
	hashes, err := rd.s.FileHashes(rd.ctx, rd.repo)
	if err != nil {
		return nil, fmt.Errorf("reading file hashes: %w", err)
	}
	return hashes, nil
}

// containers maps each identity in ids to its incoming stored contains
// edge (the first by source identity when there are several). The owner of
// a call (/endpoint) and the enclosing function of an owner (/flow, ADR
// 0045) both use this rule.
func (rd *reader) containers(ids []string) (map[string]contract.Edge, error) {
	edges, err := rd.edges(ids, "contains", store.Incoming)
	if err != nil {
		return nil, err
	}
	first := map[string]contract.Edge{}
	for _, e := range edges {
		if cur, ok := first[e.Target]; !ok || e.Source < cur.Source {
			first[e.Target] = e
		}
	}
	return first, nil
}

// owners maps each call identity to the source of its incoming contains
// edge (the first by identity when there are several).
func (rd *reader) owners(callIDs []string) (map[string]*ownerRef, error) {
	first, err := rd.containers(callIDs)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(first))
	for _, e := range first {
		ids = append(ids, e.Source)
	}
	nodes, err := rd.byIdentity(ids)
	if err != nil {
		return nil, err
	}
	out := map[string]*ownerRef{}
	for call, e := range first {
		if n, ok := nodes[e.Source]; ok {
			out[call] = &ownerRef{ID: n.Identity, Kind: n.Kind, QualifiedName: n.QualifiedName}
		}
	}
	return out, nil
}

func distinct(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func identities(nodes []contract.Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Identity
	}
	return out
}

// Shared response parts.

// ownerRef names the function, method or file that contains a call.
type ownerRef struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	QualifiedName string `json:"qualified_name"`
}

// edgeRef copies an edge's precision as stored.
type edgeRef struct {
	Kind       string  `json:"kind"`
	Provenance string  `json:"provenance"`
	Confidence float64 `json:"confidence"`
}

func refOf(e contract.Edge) edgeRef {
	return edgeRef{Kind: e.Kind, Provenance: e.Provenance, Confidence: e.Confidence}
}

// routeEdgeRef is an HTTP call edge with the match tier of the path entry
// that links to the endpoint.
type routeEdgeRef struct {
	edgeRef
	Match *string `json:"match"`
}

// ideStatus says whether IDE links could be built for this response, and
// the button label of the editor that built them (ADR 0044): the explicit or
// preset label, or null when the template is the user's own, so clients
// fall back to recognising the link's scheme. Editor and Source say which
// rule of the precedence in ADR 0050 chose that editor.
type ideStatus struct {
	Available bool    `json:"available"`
	Error     *string `json:"error"`
	Label     *string `json:"label"`
	// Editor is the preset whose format built the links; null for a
	// custom repository template or when no links could be built.
	Editor *string `json:"editor"`
	// Source is ideSourceRequest, ideSourceRepository or ideSourceBuiltin.
	Source string `json:"source"`
}

// presentedNode is a stored node, attributes verbatim, plus presentation:
// an IDE link, the current state of its file and, for call nodes, the
// source root of the calling site.
type presentedNode struct {
	query.Node
	FileState  *query.FileState `json:"file_state"`
	SourceRoot *string          `json:"source_root,omitempty"`
}

// presenter presents nodes for one request. A configuration error without
// an editor parameter drops every IDE link but keeps the data and the file
// states.
type presenter struct {
	p      *query.Presenter
	ide    ideStatus
	hashes map[string]string
	roots  *rootFinder
	// noCheckout presents a project member without a checkout here: no
	// file state (null, never "missing": the file is not known to be
	// gone) and no editor link.
	noCheckout bool
}

func (h *apiHandler) newPresenter(hashes map[string]string, editor *editorChoice) *presenter {
	return h.newPresenterReading(hashes, editor, nil)
}

// newPresenterReading is newPresenter with file reads done by read (nil:
// plain reads below the root). editor is the preset the request named
// (nil: none); it replaces the repository's ide setting (ADR 0050).
func (h *apiHandler) newPresenterReading(hashes map[string]string, editor *editorChoice, read func(string) ([]byte, error)) *presenter {
	ide, ideConfig := resolveIDE(h.opt, editor)
	if h.opt.noCheckout {
		// Nothing is read from disk: the member's files are elsewhere.
		read = func(string) ([]byte, error) { return nil, errNoCheckout }
	}
	qp := query.NewPresenter(h.opt.Root, ideConfig)
	if read != nil {
		qp = query.NewPresenterWithReader(h.opt.Root, ideConfig, read)
	}
	return &presenter{p: qp, ide: ide, hashes: hashes, roots: newRootFinder(hashes), noCheckout: h.opt.noCheckout}
}

func (p *presenter) node(n contract.Node) presentedNode {
	out := presentedNode{Node: p.p.Node(n, p.hashes[n.File])}
	if !p.ide.Available {
		out.IDE = nil
	}
	if n.File != "." && !p.noCheckout {
		state := p.p.FileState(n.File, p.hashes[n.File])
		out.FileState = &state
	}
	return out
}

func (p *presenter) call(n contract.Node) presentedNode {
	out := p.node(n)
	root := p.roots.find(n.File).root
	out.SourceRoot = &root
	return out
}

// Parameters.

// param returns a query parameter that may appear at most once.
func param(q url.Values, name string) (value string, present bool, err error) {
	values, present := q[name]
	if len(values) > 1 {
		return "", true, badRequest("parameter %s is given more than once", name)
	}
	if !present {
		return "", false, nil
	}
	return values[0], true, nil
}

func scopeParam(q url.Values) (string, error) {
	v, present, err := param(q, "scope")
	if err != nil || !present {
		return scopeProduction, err
	}
	switch v {
	case scopeProduction, scopeTest, scopeAll:
		return v, nil
	}
	return "", badRequest("scope must be production, test or all, not %q", v)
}

func rootParam(q url.Values) (*string, error) {
	v, present, err := param(q, "root")
	if err != nil || !present {
		return nil, err
	}
	return &v, nil
}

func transportParam(q url.Values, allowAll bool) ([]transport, string, error) {
	v, present, err := param(q, "transport")
	if err != nil {
		return nil, "", err
	}
	if !present && allowAll {
		v = "all"
	}
	if v == "all" && allowAll {
		return transports, v, nil
	}
	for _, t := range transports {
		if t.Name == v {
			return []transport{t}, v, nil
		}
	}
	if !present {
		return nil, "", badRequest("parameter transport is required: http, tauri_ipc or server_action")
	}
	if allowAll {
		return nil, "", badRequest("transport must be all, http, tauri_ipc or server_action, not %q", v)
	}
	return nil, "", badRequest("transport must be http, tauri_ipc or server_action, not %q", v)
}

// indexOf is the last run as /api/v0/status reports it; nil when not
// indexed.
func indexOf(runs []store.Run) *lastRun { return latestRun(runs) }

// GET /api/v0/api/endpoints

type endpointsResponse struct {
	APIVersion int                `json:"api_version"`
	Repository string             `json:"repository"`
	Index      *lastRun           `json:"index"`
	Scope      string             `json:"scope"`
	Root       *string            `json:"root"`
	Transports []transportSummary `json:"transports"`
}

type transportSummary struct {
	Transport     string          `json:"transport"`
	Frameworks    []string        `json:"frameworks"`
	EndpointCount int             `json:"endpoint_count"`
	NeverCalled   int             `json:"never_called"`
	Calls         callCounts      `json:"calls"`
	Roots         []rootCounts    `json:"roots"`
	OpenAPI       *openAPISummary `json:"openapi"`
	Endpoints     any             `json:"endpoints"`
	// Precision and CallersPrecision are the transport's scope labels, the
	// same as /endpoint's for each of its endpoints (T020): the summary
	// strip shows them without a client-side join.
	Precision        scopeLabel `json:"precision"`
	CallersPrecision scopeLabel `json:"callers_precision"`
	// NeverCalledInProject (serve --project, HTTP only) counts the routes
	// with neither a caller in the repository nor a project link:
	// never_called alone counts routes only other members call.
	NeverCalledInProject *int `json:"never_called_in_project,omitempty"`
	// PossiblyCalledInProject (serve --project, HTTP only) counts the
	// routes with neither a caller nor a project link that calls of other
	// members may still reach: candidates of an ambiguous outcome, or of
	// an incomplete one (T022 review). They are "possibly called
	// (ambiguous across repositories)", not never called.
	PossiblyCalledInProject *int `json:"possibly_called_in_project,omitempty"`
	// ProjectLinkedCalls (serve --project, HTTP only) counts the calls of
	// the scope and root that a stored project link connects to a route of
	// another member (T022 B9); calls.by_status keeps the repository's own
	// statuses.
	ProjectLinkedCalls *int `json:"project_linked_calls,omitempty"`
}

type openAPISummary struct {
	Specs               int `json:"specs"`
	DocumentedEndpoints int `json:"documented_endpoints"`
}

type routeRow struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Method    *string `json:"method"`
	Path      *string `json:"path"`
	PathCount int     `json:"path_count"`
	// PrefixUnknown (T033 row UP-29) is set when Path is null and the
	// route has a path whose prefix the code does not fix: the known part
	// with the prefix marked, never a full path. PathCount counts full
	// paths only.
	PrefixUnknown *routePrefixView `json:"prefix_unknown,omitempty"`
	Pattern       *string          `json:"pattern"`
	Framework     *string          `json:"framework"`
	File          string           `json:"file"`
	Line          *int             `json:"line"`
	Scope         any              `json:"scope"`
	CallerCount   int              `json:"caller_count"`
	Documented    bool             `json:"documented"`
	Flags         []string         `json:"flags"`
	// ProjectCallerCount (serve --project only) counts the calls of other
	// members linked to the route by the stored link pass.
	ProjectCallerCount *int `json:"project_caller_count,omitempty"`
	// ProjectPossibleCallerCount (serve --project only) counts the calls of
	// other members that may call the route without a link (ambiguous or
	// incomplete outcomes).
	ProjectPossibleCallerCount *int `json:"project_possible_caller_count,omitempty"`
	// Shape is the compact request/response shape (section states and
	// item counts, routeshape.Summary); absent when the route has no
	// stored shape, which means every section is not read.
	Shape *routeshape.Summary `json:"shape,omitempty"`
}

// routePrefixView is the unknown-prefix path a route row shows (T033 rows
// UP-29, NX-R4 under decision U6): Path is its display form ("…" for the
// prefix the code does not fix, "…/api/hosts/{host}/logs/stream"), Reason
// the stored reason code with its wording, Hop the unresolved mount the
// entry comes from (null for a generated server or a Next.js basePath)
// and Count the route's unknown-prefix paths.
type routePrefixView struct {
	Path       string  `json:"path"`
	Reason     string  `json:"reason"`
	ReasonText string  `json:"reason_text"`
	Hop        *string `json:"hop"`
	Count      int     `json:"count"`
}

// routePrefix is a route row's unknown-prefix view in scope, or nil when
// the row shows a full path or the route has no unknown-prefix path.
func routePrefix(n contract.Node, scope string) *routePrefixView {
	if routePath(n, scope) != nil {
		return nil
	}
	list := routepaths.PrefixUnknownPaths(n.Attributes)
	shown := routepaths.ShownPrefixPath(list, scope)
	if shown == nil {
		return nil
	}
	return &routePrefixView{Path: shown.Path, Reason: shown.Reason, ReasonText: prefixReasonText(shown.Reason), Hop: shown.Hop, Count: len(list)}
}

// prefixReasonText words an unknown-prefix reason, "prefix unknown: ...".
func prefixReasonText(code string) string {
	if text, ok := routepaths.PrefixReasonText[code]; ok {
		return "prefix unknown: " + text
	}
	return "prefix unknown"
}

// actionRow is one server action of the endpoint list (T024b).
type actionRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Action      *string  `json:"action"`
	ActionScope *string  `json:"action_scope"`
	Form        *string  `json:"form"`
	Handler     *string  `json:"handler"`
	Framework   *string  `json:"framework"`
	File        string   `json:"file"`
	Line        *int     `json:"line"`
	Scope       any      `json:"scope"`
	CallerCount int      `json:"caller_count"`
	Flags       []string `json:"flags"`
}

type commandRow struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Command     *string  `json:"command"`
	Registry    *string  `json:"registry"`
	CfgGated    bool     `json:"cfg_gated"`
	TestOnly    bool     `json:"test_only"`
	Unreachable bool     `json:"unreachable"`
	Plugin      *string  `json:"plugin"`
	Handler     *string  `json:"handler"`
	Framework   *string  `json:"framework"`
	File        string   `json:"file"`
	Line        *int     `json:"line"`
	Scope       any      `json:"scope"`
	CallerCount int      `json:"caller_count"`
	Flags       []string `json:"flags"`
}

func lineOf(n contract.Node) *int {
	if n.Span == nil {
		return nil
	}
	line := n.Span.StartLine
	return &line
}

func computeEndpoints(h *apiHandler, q url.Values) (readFunc, error) {
	scope, err := scopeParam(q)
	if err != nil {
		return nil, err
	}
	selected, _, err := transportParam(q, true)
	if err != nil {
		return nil, err
	}
	root, err := rootParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		roots := newRootFinder(hashes)
		out := endpointsResponse{Repository: h.opt.Repository, Index: indexOf(runs), Scope: scope, Root: root, Transports: []transportSummary{}}
		for _, t := range selected {
			summary, err := summarize(rd, t, scope, root, roots)
			if err != nil {
				return nil, err
			}
			endpoints, err := rd.nodes(t.EndpointKind)
			if err != nil {
				return nil, err
			}
			summary.Precision, summary.CallersPrecision = endpointLabels(runs, t, endpoints)
			if rows, ok := summary.Endpoints.([]routeRow); ok && h.opt.project != nil {
				counts, err := h.opt.project.projectCallerCounts(ctx, h.opt.Repository)
				if err != nil {
					return nil, err
				}
				ids := make([]string, 0, len(rows))
				for _, r := range rows {
					ids = append(ids, r.ID)
				}
				possible, err := h.opt.project.projectPossibleCounts(ctx, h.opt.Repository, ids)
				if err != nil {
					return nil, err
				}
				never, maybe := 0, 0
				for i := range rows {
					n, m := counts[rows[i].ID], possible[rows[i].ID]
					rows[i].ProjectCallerCount = &n
					rows[i].ProjectPossibleCallerCount = &m
					switch {
					case n > 0 || rows[i].CallerCount > 0:
					case m > 0:
						// T022 review: a route only ambiguous or incomplete
						// calls may reach is not "never called".
						maybe++
					default:
						never++
					}
				}
				summary.NeverCalledInProject = &never
				summary.PossiblyCalledInProject = &maybe
			}
			if h.opt.project != nil && t.Name == httpTransport.Name {
				// T022 B9: the calls of this scope and root that the project
				// links into another member's route, so a view does not
				// read the repository's "unmatched" as the project's.
				linked, err := h.opt.project.projectLinkedCalls(ctx, h.opt.Repository)
				if err != nil {
					return nil, err
				}
				calls, err := rd.nodes(t.CallKind)
				if err != nil {
					return nil, err
				}
				n := 0
				for _, c := range calls {
					if linked[c.Identity] && inScope(c, scope) && (root == nil || roots.find(c.File).root == *root) {
						n++
					}
				}
				summary.ProjectLinkedCalls = &n
				status, err := h.opt.project.projectStatusFunc(ctx, h.opt.Repository)
				if err != nil {
					return nil, err
				}
				addProjectCounts(&summary, calls, scope, root, roots, status)
			}
			out.Transports = append(out.Transports, summary)
		}
		return out, nil
	}, nil
}

// summarize counts one transport and lists its endpoints.
func summarize(rd *reader, t transport, scope string, root *string, roots *rootFinder) (transportSummary, error) {
	endpoints, err := rd.nodes(t.EndpointKind)
	if err != nil {
		return transportSummary{}, err
	}
	calls, err := rd.nodes(t.CallKind)
	if err != nil {
		return transportSummary{}, err
	}
	edges, err := rd.edges(identities(endpoints), t.CallEdge, store.Incoming)
	if err != nil {
		return transportSummary{}, err
	}
	cov := countCoverage(endpoints, calls, edges, scope, root, roots)
	out := transportSummary{
		Transport:     t.Name,
		Frameworks:    frameworksOf(cov.Endpoints),
		EndpointCount: len(cov.Endpoints),
		NeverCalled:   cov.NeverCalled,
		Calls:         cov.Calls,
		Roots:         cov.Roots,
	}
	if t.Name == actionTransport.Name {
		rows := make([]actionRow, 0, len(cov.Endpoints))
		for _, n := range cov.Endpoints {
			if cov.Callers[n.Identity] == 0 && unreadUses(n) {
				// May be called through an unread use: not never called.
				out.NeverCalled--
			}
			rows = append(rows, actionRow{
				ID: n.Identity, Name: n.Name, Action: stringAttr(n, "action"), ActionScope: stringAttr(n, "action_scope"), Form: stringAttr(n, "form"),
				Handler: stringAttr(n, "handler"), Framework: stringAttr(n, "framework"), File: n.File, Line: lineOf(n), Scope: n.Attributes["scope"],
				CallerCount: cov.Callers[n.Identity], Flags: actionFlags(n),
			})
		}
		sortActions(rows)
		out.Endpoints = rows
		return out, nil
	}
	if t.Name != httpTransport.Name {
		rows := make([]commandRow, 0, len(cov.Endpoints))
		for _, n := range cov.Endpoints {
			rows = append(rows, commandRow{
				ID: n.Identity, Name: n.Name, Command: stringAttr(n, "command"), Registry: stringAttr(n, "registry"),
				CfgGated: boolAttr(n.Attributes, "cfg_gated"), TestOnly: boolAttr(n.Attributes, "test_only"),
				Unreachable: boolAttr(n.Attributes, "unreachable"), Plugin: stringAttr(n, "plugin"), Handler: stringAttr(n, "handler"),
				Framework: stringAttr(n, "framework"), File: n.File, Line: lineOf(n), Scope: n.Attributes["scope"],
				CallerCount: cov.Callers[n.Identity], Flags: commandFlags(n),
			})
		}
		sortCommands(rows)
		out.Endpoints = rows
		return out, nil
	}
	specs, err := rd.nodes("api_spec")
	if err != nil {
		return transportSummary{}, err
	}
	docs, err := rd.edges(identities(cov.Endpoints), "documents_route", store.Incoming)
	if err != nil {
		return transportSummary{}, err
	}
	documented := map[string]bool{}
	for _, e := range docs {
		documented[e.Target] = true
	}
	out.OpenAPI = &openAPISummary{Specs: len(specs)}
	withheld, err := rd.withheldSites()
	if err != nil {
		return transportSummary{}, err
	}
	rows := make([]routeRow, 0, len(cov.Endpoints))
	for _, n := range cov.Endpoints {
		paths := fullPaths(objects(n.Attributes["paths"]))
		if documented[n.Identity] {
			out.OpenAPI.DocumentedEndpoints++
		}
		rows = append(rows, routeRow{
			ID: n.Identity, Name: n.Name, Method: stringAttr(n, "method"), Path: routePath(n, scope), PathCount: len(paths), PrefixUnknown: routePrefix(n, scope),
			Pattern: stringAttr(n, "pattern"), Framework: stringAttr(n, "framework"), File: n.File, Line: lineOf(n),
			Scope: n.Attributes["scope"], CallerCount: cov.Callers[n.Identity], Documented: documented[n.Identity], Flags: routeFlags(n, withheld),
			Shape: routeShapeSummary(n),
		})
	}
	sortRoutes(rows)
	out.Endpoints = rows
	return out, nil
}

// sortRoutes orders routes by display path (null last, then pattern),
// method order, then identity.
func sortRoutes(rows []routeRow) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if less, equal := lessNullLast(a.Path, b.Path); !equal {
			return less
		}
		if a.Path == nil {
			// A path whose prefix is unknown is never sorted as a full
			// path (UP-29): those rows follow every full path, by their
			// known part, before the rows with no path at all.
			if less, equal := lessNullLast(prefixOf(a), prefixOf(b)); !equal {
				return less
			}
			if less, equal := lessNullLast(a.Pattern, b.Pattern); !equal {
				return less
			}
		}
		if ra, rb := methodRank(a.Method), methodRank(b.Method); ra != rb {
			return ra < rb
		}
		if less, equal := lessNullLast(a.Method, b.Method); !equal {
			return less
		}
		return a.ID < b.ID
	})
}

// prefixOf is a row's unknown-prefix path without its leading "…", or nil.
func prefixOf(r routeRow) *string {
	if r.PrefixUnknown == nil {
		return nil
	}
	s := strings.TrimPrefix(r.PrefixUnknown.Path, "…")
	return &s
}

// sortActions orders server actions by file, then line, then identity.
func sortActions(rows []actionRow) {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.File != b.File {
			return a.File < b.File
		}
		al, bl := -1, -1
		if a.Line != nil {
			al = *a.Line
		}
		if b.Line != nil {
			bl = *b.Line
		}
		if al != bl {
			return al < bl
		}
		return a.ID < b.ID
	})
}

// sortCommands orders commands by name (null last), then identity.
func sortCommands(rows []commandRow) {
	sort.Slice(rows, func(i, j int) bool {
		if less, equal := lessNullLast(rows[i].Command, rows[j].Command); !equal {
			return less
		}
		return rows[i].ID < rows[j].ID
	})
}

// GET /api/v0/api/endpoint

type endpointResponse struct {
	APIVersion           int             `json:"api_version"`
	Repository           string          `json:"repository"`
	Index                *lastRun        `json:"index"`
	Scope                string          `json:"scope"`
	Root                 *string         `json:"root"`
	IDE                  ideStatus       `json:"ide"`
	Endpoint             presentedNode   `json:"endpoint"`
	Transport            string          `json:"transport"`
	Handler              handlerInfo     `json:"handler"`
	OpenAPI              *openAPIDetail  `json:"openapi"`
	Callers              []callerEntry   `json:"callers"`
	NearMisses           []nearMissEntry `json:"near_misses"`
	NearMissesIncomplete int             `json:"near_misses_incomplete"`
	// Precision is the scope label of the endpoint's family (routes or IPC
	// commands) and CallersPrecision that of its calls (client calls or
	// IPC calls); both fall to the default branch today (T020).
	Precision        scopeLabel `json:"precision"`
	CallersPrecision scopeLabel `json:"callers_precision"`
	// Project (serve --project only) lists the cross-repository links
	// into the endpoint: callers in other members, each with its
	// assumption, inferred provenance and freshness.
	Project *projectEndpointDoc `json:"project,omitempty"`
	// Shape (HTTP routes) is the route's validated request/response shape
	// (docs/contract/claims/route-shape.md); absent when none is stored,
	// which means every section is not read. ShapeError is set instead
	// when a stored shape is invalid.
	Shape      *routeshape.Shape `json:"shape,omitempty"`
	ShapeError *string           `json:"shape_error,omitempty"`
	// ShapeLinks (HTTP routes) maps each "file:line" a shape chip shows
	// (the route's registration, its security dependencies' declarations
	// and every provenance of the shape) to its editor link, built like a
	// node's; absent when there is none, such as when editor links are
	// unavailable (shapelinks.go).
	ShapeLinks map[string]*query.IDELink `json:"shape_links,omitempty"`
	// ModelTables (HTTP routes with a valid stored shape; T029 row MT-28)
	// lists the shape's models that are also database tables, the same
	// declaration node; empty when none is, absent without a shape.
	ModelTables *[]routeModelTable `json:"model_tables,omitempty"`
	// PathWithheld (HTTP routes) marks a route registration whose path the
	// Go linker withholds (couldn't-read row CR-44; the endpoint list's
	// flag path_withheld): the header says "path withheld" rather than
	// "path not known". Absent otherwise.
	PathWithheld bool `json:"path_withheld,omitempty"`
}

type handlerInfo struct {
	State      string         `json:"state"`
	Expression *string        `json:"expression"`
	Node       *presentedNode `json:"node"`
	Edge       *edgeRef       `json:"edge"`
}

type openAPIDetail struct {
	Specs      int             `json:"specs"`
	Operations []operationLink `json:"operations"`
}

type operationLink struct {
	Node presentedNode `json:"node"`
	Edge edgeRef       `json:"edge"`
}

type callerEntry struct {
	Node    presentedNode `json:"node"`
	Owner   *ownerRef     `json:"owner"`
	Edges   []any         `json:"edges"`
	Reasons []reason      `json:"reasons"`
	// ShapeDifferences (HTTP, additive; T029 W4) compares what the call
	// sends with what the route reads; absent when nothing is compared.
	ShapeDifferences *shapeDifferencesDoc `json:"shape_differences,omitempty"`
}

type nearMissEntry struct {
	Node           presentedNode `json:"node"`
	Owner          *ownerRef     `json:"owner"`
	Relation       string        `json:"relation"`
	CandidateCount int           `json:"candidate_count"`
	// CandidateCountExact is false when candidate_count is only a lower
	// bound: the candidate list was cut at its cap and no full count is
	// stored, so a UI says "at least".
	CandidateCountExact bool     `json:"candidate_count_exact"`
	Reasons             []reason `json:"reasons"`
}

// Handler states.
const (
	handlerResolved   = "resolved"
	handlerMissing    = "missing"
	handlerInline     = "inline"
	handlerUnresolved = "unresolved"
)

// endpointParams parses the parameters /endpoint and /flow share: id (a
// route, ipc_command or server_action identity), scope and root.
func endpointParams(q url.Values) (id string, t transport, scope string, root *string, err error) {
	id, present, err := param(q, "id")
	if err != nil {
		return "", t, "", nil, err
	}
	if !present || id == "" {
		return "", t, "", nil, badRequest("parameter id is required")
	}
	kind, _, _, err := contract.ParseIdentity(id)
	if err != nil {
		return "", t, "", nil, badRequest("id %q is not a canonical node identity", id)
	}
	t, ok := transportFor(kind)
	if !ok {
		return "", t, "", nil, badRequest("id %q is a %s, not a route, ipc_command or server_action", id, kind)
	}
	if scope, err = scopeParam(q); err != nil {
		return "", t, "", nil, err
	}
	if root, err = rootParam(q); err != nil {
		return "", t, "", nil, err
	}
	return id, t, scope, root, nil
}

func computeEndpoint(h *apiHandler, q url.Values) (readFunc, error) {
	id, t, scope, root, err := endpointParams(q)
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		found, err := rd.byIdentity([]string{id})
		if err != nil {
			return nil, err
		}
		endpoint, ok := found[id]
		if !ok {
			return nil, &apiError{http.StatusNotFound, "not_found", "no API endpoint with identity " + id}
		}
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		p := h.newPresenter(hashes, editor)
		out := endpointResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), Scope: scope, Root: root, IDE: p.ide,
			Endpoint: p.node(endpoint), Transport: t.Name, Callers: []callerEntry{}, NearMisses: []nearMissEntry{},
		}
		out.Precision, out.CallersPrecision = endpointLabels(runs, t, []contract.Node{endpoint})
		if out.Handler, err = handlerOf(rd, p, t, endpoint); err != nil {
			return nil, err
		}
		if t.Name == httpTransport.Name {
			if out.OpenAPI, err = operationsOf(rd, p, id); err != nil {
				return nil, err
			}
			out.Shape, out.ShapeError = routeShape(endpoint)
			out.ShapeLinks = shapeLinks(p, endpoint, out.Shape)
			if out.ModelTables, err = routeModelTables(rd, out.Shape); err != nil {
				return nil, err
			}
			if routepaths.MayBeWithheld(endpoint) {
				withheld, err := rd.withheldSites()
				if err != nil {
					return nil, err
				}
				out.PathWithheld = routepaths.PathWithheld(endpoint, withheld)
			}
		}
		sel, err := selectCalls(rd, p.roots, t, id, scope, root)
		if err != nil {
			return nil, err
		}
		callers, misses, missOf, edgesFrom := sel.callers, sel.misses, sel.missOf, sel.edgesFrom
		out.NearMissesIncomplete = sel.incomplete
		owners, err := rd.owners(append(identities(callers), identities(misses)...))
		if err != nil {
			return nil, err
		}
		for _, c := range callers {
			entry := callerEntry{Node: p.call(c), Owner: owners[c.Identity], Edges: []any{}, Reasons: callReasons(t, c, true)}
			if t.Name == httpTransport.Name {
				entry.ShapeDifferences = shapeDifferences(endpoint, c, presenterLinker(p), presenterLinker(p), false)
			}
			sorted := edgesFrom[c.Identity]
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Kind < sorted[j].Kind })
			for _, e := range sorted {
				if t.Name == httpTransport.Name {
					entry.Edges = append(entry.Edges, routeEdgeRef{edgeRef: refOf(e), Match: linkedMatch(c, id)})
				} else {
					entry.Edges = append(entry.Edges, refOf(e))
				}
			}
			out.Callers = append(out.Callers, entry)
		}
		// A near miss may still link elsewhere through another alternative;
		// its reasons say so only when such an edge is stored.
		missEdges, err := rd.edges(identities(misses), t.CallEdge, store.Outgoing)
		if err != nil {
			return nil, err
		}
		linksElsewhere := map[string]bool{}
		for _, e := range missEdges {
			linksElsewhere[e.Source] = true
		}
		for _, c := range misses {
			out.NearMisses = append(out.NearMisses, nearMissEntry{
				Node: p.call(c), Owner: owners[c.Identity], Relation: missOf[c.Identity].relation,
				CandidateCount: missOf[c.Identity].count, CandidateCountExact: missOf[c.Identity].exact,
				Reasons: callReasons(t, c, linksElsewhere[c.Identity]),
			})
		}
		if h.opt.project != nil && t.Name == httpTransport.Name {
			if out.Project, err = h.opt.project.projectEndpoint(ctx, h.opt.Repository, endpoint, p, editor); err != nil {
				return nil, err
			}
		}
		return out, nil
	}, nil
}

// endpointCalls is the caller and near-miss selection of one endpoint. It
// is shared by /api/v0/api/endpoint and /api/v0/api/flow (ADR 0045), so the
// Callers table and the Flow tab count the same calls by construction.
type endpointCalls struct {
	callers []contract.Node // calls with a stored call edge to the endpoint, by site
	misses  []contract.Node // near misses, by site
	missOf  map[string]nearMissResult
	// incomplete counts the calls whose cut candidate list may hide the
	// endpoint (near_misses_incomplete).
	incomplete int
	// edgesFrom holds the stored call edges to the endpoint by caller.
	edgesFrom map[string][]contract.Edge
}

// selectCalls reads the calls of t and selects, in scope and root, the
// callers and near misses of endpoint id.
func selectCalls(rd *reader, roots *rootFinder, t transport, id, scope string, root *string) (endpointCalls, error) {
	out := endpointCalls{missOf: map[string]nearMissResult{}, edgesFrom: map[string][]contract.Edge{}}
	calls, err := rd.nodes(t.CallKind)
	if err != nil {
		return out, err
	}
	edges, err := rd.edges([]string{id}, t.CallEdge, store.Incoming)
	if err != nil {
		return out, err
	}
	for _, e := range edges {
		if e.Target == id {
			out.edgesFrom[e.Source] = append(out.edgesFrom[e.Source], e)
		}
	}
	for _, c := range calls {
		if !inScope(c, scope) || root != nil && roots.find(c.File).root != *root {
			continue
		}
		if len(out.edgesFrom[c.Identity]) > 0 {
			out.callers = append(out.callers, c)
			continue
		}
		miss := nearMiss(t, c, id)
		if miss.listed {
			out.misses = append(out.misses, c)
			out.missOf[c.Identity] = miss
		} else if miss.capped {
			out.incomplete++
		}
	}
	sort.Slice(out.callers, func(i, j int) bool { return lessBySite(out.callers[i], out.callers[j]) })
	sort.Slice(out.misses, func(i, j int) bool { return lessBySite(out.misses[i], out.misses[j]) })
	return out, nil
}

// nearMissResult is how one call relates to an endpoint it has no edge to.
type nearMissResult struct {
	relation string
	count    int
	exact    bool // count is the tie size, not a lower bound
	listed   bool // the call lists the endpoint as a candidate
	capped   bool // unlisted, but a cut candidate list may hide it
}

// nearMiss decides whether call c records endpoint id as a candidate of an
// ambiguous or test-only choice. Only those statuses record candidates;
// dynamic and unmatched choices never do, so they are never near misses.
// For http the status is the path entry's: a call whose node status is,
// for example, dynamic can still hold an ambiguous entry. A tauri call
// records candidates on the node only, so its node status decides.
func nearMiss(t transport, c contract.Node, id string) nearMissResult {
	var r nearMissResult
	if t.Name == httpTransport.Name {
		capped := false
		for _, p := range objects(c.Attributes["paths"]) {
			entryStatus, _ := p["status"].(string)
			if entryStatus != "ambiguous" && entryStatus != "test_only" {
				continue
			}
			candidates := stringList(p["candidates"])
			count, exact := candidateTie(p)
			if count > len(candidates) || !exact {
				capped = true
			}
			if !r.listed && contains(candidates, id) {
				r = nearMissResult{relation: entryStatus + "_candidate", count: count, exact: exact, listed: true}
			}
		}
		r.capped = capped && !r.listed
		return r
	}
	status := statusOf(c)
	if status != "ambiguous" && status != "test_only" {
		return r
	}
	candidates := stringList(c.Attributes["candidates"])
	exact := len(candidates) < maxCandidates
	if contains(candidates, id) {
		return nearMissResult{relation: status + "_candidate", count: len(candidates), exact: exact, listed: true}
	}
	r.capped = !exact
	return r
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// linkedMatch is the match tier of the call's linked path entry whose
// candidates are exactly [id]; exact wins over suffix. Nil when no entry
// says so.
func linkedMatch(c contract.Node, id string) *string {
	var best *string
	for _, p := range objects(c.Attributes["paths"]) {
		candidates := stringList(p["candidates"])
		if p["status"] != "linked" || len(candidates) != 1 || candidates[0] != id {
			continue
		}
		m, ok := p["match"].(string)
		if !ok {
			continue
		}
		if best == nil || *best != "exact" && m == "exact" {
			best = &m
		}
	}
	return best
}

// handlerOf reports how the endpoint's handler is known, from its stored
// attributes and the exposes edge from that handler.
func handlerOf(rd *reader, p *presenter, t transport, endpoint contract.Node) (handlerInfo, error) {
	info, _, _, err := handlerWithEdge(rd, p, t, endpoint)
	return info, err
}

// handlerWithEdge is handlerOf that also returns the stored handler node
// and the full exposes edge (nil when absent), which /flow draws.
func handlerWithEdge(rd *reader, p *presenter, t transport, endpoint contract.Node) (handlerInfo, *contract.Node, *contract.Edge, error) {
	handler := stringAttr(endpoint, "handler")
	if handler == nil {
		if t.Name == httpTransport.Name && boolAttr(endpoint.Attributes, "handler_inline") {
			return handlerInfo{State: handlerInline}, nil, nil, nil
		}
		info := handlerInfo{State: handlerUnresolved}
		if t.Name == httpTransport.Name {
			info.Expression = stringAttr(endpoint, "handler_expression")
		}
		return info, nil, nil, nil
	}
	found, err := rd.byIdentity([]string{*handler})
	if err != nil {
		return handlerInfo{}, nil, nil, err
	}
	info := handlerInfo{State: handlerMissing}
	var node *contract.Node
	if n, ok := found[*handler]; ok {
		presented := p.node(n)
		info.State, info.Node, node = handlerResolved, &presented, &n
	}
	edges, err := rd.edges([]string{endpoint.Identity}, t.ExposeEdge, store.Incoming)
	if err != nil {
		return handlerInfo{}, nil, nil, err
	}
	var edge *contract.Edge
	for _, e := range edges {
		if e.Source == *handler && e.Target == endpoint.Identity {
			ref := refOf(e)
			info.Edge, edge = &ref, &e
			break
		}
	}
	return info, node, edge, nil
}

// operationsOf lists the OpenAPI operations documenting a route.
func operationsOf(rd *reader, p *presenter, id string) (*openAPIDetail, error) {
	specs, err := rd.nodes("api_spec")
	if err != nil {
		return nil, err
	}
	edges, err := rd.edges([]string{id}, "documents_route", store.Incoming)
	if err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(edges))
	for _, e := range edges {
		sources = append(sources, e.Source)
	}
	nodes, err := rd.byIdentity(sources)
	if err != nil {
		return nil, err
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].Source < edges[j].Source })
	out := &openAPIDetail{Specs: len(specs), Operations: []operationLink{}}
	for _, e := range edges {
		if n, ok := nodes[e.Source]; ok && e.Target == id {
			out.Operations = append(out.Operations, operationLink{Node: p.node(n), Edge: refOf(e)})
		}
	}
	return out, nil
}

// GET /api/v0/api/calls

type callsResponse struct {
	APIVersion int      `json:"api_version"`
	Repository string   `json:"repository"`
	Index      *lastRun `json:"index"`
	Scope      string   `json:"scope"`
	Transport  string   `json:"transport"`
	Statuses   []string `json:"statuses"`
	// StatusScope is "project" when the statuses matched the calls'
	// project-scope status (status_scope=project), absent otherwise.
	StatusScope string      `json:"status_scope,omitempty"`
	Root        *string     `json:"root"`
	IDE         ideStatus   `json:"ide"`
	Total       int         `json:"total"`
	Calls       []callEntry `json:"calls"`
}

type callEntry struct {
	Node    presentedNode `json:"node"`
	Owner   *ownerRef     `json:"owner"`
	Targets []string      `json:"targets"`
	Reasons []reason      `json:"reasons"`
	// Project (serve --project only, HTTP calls) is what the stored link
	// pass says about the call: its links into other members' routes and
	// its outcomes (T022 B9). Targets and Reasons keep their
	// single-repository meaning.
	Project *callProjectDoc `json:"project,omitempty"`
}

func computeCalls(h *apiHandler, q url.Values) (readFunc, error) {
	selected, _, err := transportParam(q, false)
	if err != nil {
		return nil, err
	}
	t := selected[0]
	raw, present, err := param(q, "status")
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, badRequest("parameter status is required: comma-separated raw statuses")
	}
	statuses := distinct(strings.Split(raw, ","))
	wanted := setOf(statuses...)
	scope, err := scopeParam(q)
	if err != nil {
		return nil, err
	}
	root, err := rootParam(q)
	if err != nil {
		return nil, err
	}
	editor, err := editorParam(q)
	if err != nil {
		return nil, err
	}
	// status_scope=project (serve --project, HTTP; T022 MR-UI) matches the
	// statuses against each call's project-scope status, the one the
	// coverage counts use there.
	statusScope, _, err := param(q, "status_scope")
	if err != nil {
		return nil, err
	}
	switch {
	case statusScope == "" || statusScope == "repository":
		statusScope = "repository"
	case statusScope != "project":
		return nil, badRequest("status_scope must be repository or project, not %q", statusScope)
	case h.opt.project == nil:
		return nil, badRequest("status_scope=project needs graphvein serve --project")
	case t.Name != httpTransport.Name:
		return nil, badRequest("status_scope=project is for transport http")
	}
	return func(ctx context.Context, rd *reader, runs []store.Run) (any, error) {
		hashes, err := rd.fileHashes()
		if err != nil {
			return nil, err
		}
		p := h.newPresenter(hashes, editor)
		all, err := rd.nodes(t.CallKind)
		if err != nil {
			return nil, err
		}
		status := statusOf
		if statusScope == "project" {
			if status, err = h.opt.project.projectStatusFunc(ctx, h.opt.Repository); err != nil {
				return nil, err
			}
		}
		var calls []contract.Node
		for _, c := range all {
			if inScope(c, scope) && wanted[status(c)] && (root == nil || p.roots.find(c.File).root == *root) {
				calls = append(calls, c)
			}
		}
		sort.Slice(calls, func(i, j int) bool { return lessBySite(calls[i], calls[j]) })
		ids := identities(calls)
		edges, err := rd.edges(ids, t.CallEdge, store.Outgoing)
		if err != nil {
			return nil, err
		}
		targets := map[string][]string{}
		for _, e := range edges {
			targets[e.Source] = append(targets[e.Source], e.Target)
		}
		owners, err := rd.owners(ids)
		if err != nil {
			return nil, err
		}
		out := callsResponse{
			Repository: h.opt.Repository, Index: indexOf(runs), Scope: scope, Transport: t.Name, Statuses: statuses,
			Root: root, IDE: p.ide, Total: len(calls), Calls: make([]callEntry, 0, len(calls)),
		}
		if statusScope == "project" {
			out.StatusScope = statusScope
		}
		var project map[string]*callProjectDoc
		if h.opt.project != nil && t.Name == httpTransport.Name {
			if project, err = h.opt.project.callLinks(ctx, h.opt.Repository, ids); err != nil {
				return nil, err
			}
		}
		for _, c := range calls {
			ts := distinct(targets[c.Identity])
			entry := callEntry{Node: p.call(c), Owner: owners[c.Identity], Targets: ts, Reasons: callReasons(t, c, len(ts) > 0)}
			if project != nil {
				entry.Project = project[c.Identity]
			}
			out.Calls = append(out.Calls, entry)
		}
		return out, nil
	}, nil
}

// newAPIRoutes returns the API section handlers, the source endpoint
// (ADR 0044), the editor preset list (ADR 0050), the UI defaults (T025b),
// the precision rule table (ADR 0066) and the schema endpoints (ADR 0048)
// by path.
func newAPIRoutes(opt Options) map[string]http.Handler {
	return map[string]http.Handler{
		"/api/v0/api/endpoints": &apiHandler{opt: opt, compute: computeEndpoints},
		"/api/v0/api/endpoint":  &apiHandler{opt: opt, compute: computeEndpoint},
		"/api/v0/api/calls":     &apiHandler{opt: opt, compute: computeCalls},
		"/api/v0/api/flow":      &apiHandler{opt: opt, compute: computeFlow},
		"/api/v0/source":        newSourceHandler(opt),
		"/api/v0/ide":           ideHandler{opt: opt},
		"/api/v0/ui":            uiHandler{opt: opt},
		"/api/v0/precision":     &apiHandler{opt: opt, compute: computePrecision},
		// Schema endpoints (ADR 0048, schema.go).
		"/api/v0/schema/summary":     &apiHandler{opt: opt, compute: computeSchemaSummary},
		"/api/v0/schema/sets":        &apiHandler{opt: opt, compute: computeSchemaSets},
		"/api/v0/schema/table":       &apiHandler{opt: opt, compute: computeSchemaTable},
		"/api/v0/schema/migration":   &apiHandler{opt: opt, compute: computeSchemaMigration},
		"/api/v0/schema/assumptions": &apiHandler{opt: opt, compute: computeSchemaAssumptions},
		// Architecture endpoints (T026, ADR 0079, architecture.go).
		"/api/v0/architecture":           &apiHandler{opt: opt, compute: computeArchitecture},
		"/api/v0/architecture/component": &apiHandler{opt: opt, compute: computeArchitectureComponent},
		"/api/v0/architecture/chain":     &apiHandler{opt: opt, compute: computeArchitectureChain},
		// The generated OpenAPI document and the explorer's path values
		// (T029, openapi.go).
		openapiPath:        openapiHandler{opt: opt},
		explorerValuesPath: newExplorerValuesHandler(opt),
		// The Settings page's keys and guarded edit (T039, settings.go).
		configPath: newConfigHandler(opt),
		// Jump-to search and the couldn't-read page (T040, ADR 0089).
		searchPath: searchHandler{opt: opt},
		unreadPath: unreadHandler{opt: opt},
	}
}

// latestStoreRun is the run with the greatest parsed finish time, as
// latestRun picks it; nil when no finish time parses.
func latestStoreRun(runs []store.Run) *store.Run {
	var best *store.Run
	var bestAt time.Time
	for i := range runs {
		at, err := time.Parse(time.RFC3339Nano, runs[i].FinishedAt)
		if err != nil {
			continue
		}
		if best == nil || at.After(bestAt) {
			best, bestAt = &runs[i], at
		}
	}
	return best
}

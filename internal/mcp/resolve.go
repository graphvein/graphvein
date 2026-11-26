package mcp

import (
	"context"
	"fmt"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"net/url"
	"strings"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/server"
)

// targetLookup is what one tool's target may name (row MC-7). Every tool
// resolves through Server.resolve with the rule of query.Resolve (an
// identity, a name or a qualified-name suffix at a name boundary resolves;
// any other suffix match is a candidate only), so the same target names
// the same node in every tool, and several matches are always refused as
// ambiguous with every candidate (T030 review).
type targetLookup struct {
	// what names the lookup in a refusal ("stored node", "stored table").
	what string
	// kind, when set, is the only node kind that resolves (table: a
	// table); a node of another kind with the name is a candidate only.
	kind string
	// routes: a "METHOD /path" target names the HTTP routes served at that
	// path (every composed path, server.ServedPaths) before any node
	// lookup, and only a route resolves it: a node of another kind with
	// that name (an OpenAPI operation, a client call) is a candidate only.
	routes bool
	// components: a component directory the architecture view lists
	// resolves too (chain); "component:DIR" names it explicitly.
	components bool
	// endpoints: only endpoint rows resolve (the endpoint tool), of the
	// HTTP transport for a "METHOD /path" target, else of every transport.
	endpoints bool
	// scope is the endpoints view scope the route rows are read in.
	scope string
}

// resolved is a target resolved to one node or component.
type resolved struct {
	id   string
	repo string
	// match is the tier that resolved it: identity, name,
	// qualified_suffix (query.Match*), served_path or component.
	match string
}

// candidate is one match that did not resolve on its own.
type candidate struct {
	repo, id string
}

const (
	matchServedPath = "served_path"
	matchComponent  = "component"
	componentPrefix = "component:"
)

// resolve resolves target for a tool (MC-7). repo is the member to look
// in; in project mode an empty repo looks in every indexed member.
func (s *Server) resolve(ctx context.Context, e *Envelope, repo, target string, lk targetLookup) (resolved, error) {
	tagRow("MC-7")
	if strings.TrimSpace(target) == "" {
		return resolved{}, badRequest("target is required")
	}
	r, err := s.resolveOnce(ctx, e, repo, target, lk)
	if err == nil {
		e.Request["resolved"] = r.id
		e.Request["match"] = r.match
	}
	return r, err
}

func (s *Server) resolveOnce(ctx context.Context, e *Envelope, repo, target string, lk targetLookup) (resolved, error) {
	scope := orDefault(lk.scope, "all")
	if lk.components && strings.HasPrefix(target, componentPrefix) {
		dir := strings.TrimPrefix(target, componentPrefix)
		if s.isComponent(ctx, repo, dir) {
			return resolved{id: dir, repo: repo, match: matchComponent}, nil
		}
		return resolved{}, &toolError{code: "not_found", message: fmt.Sprintf("the architecture view lists no component %q", dir)}
	}
	if isIdentity(target) {
		if kind, _, _, _ := contract.ParseIdentity(target); lk.kind != "" && kind != lk.kind {
			return resolved{}, &toolError{code: "not_found", message: fmt.Sprintf("%s is a %s identity: the %s lookup takes %s nodes only", target, kind, e.Tool, lk.kind)}
		}
		return resolved{id: target, repo: repo, match: query.MatchIdentity}, nil
	}
	repos := s.lookupRepos(e, repo)
	route := lk.routes && routeForm(target)
	// The endpoint tool resolves endpoint rows only: their identities.
	var only map[string]bool
	if lk.endpoints {
		only = map[string]bool{}
		transports := []string{"http", "tauri_ipc", "server_action"}
		if route {
			transports = transports[:1]
		}
		for _, r := range repos {
			for _, t := range transports {
				rows, err := s.endpointRows(ctx, r, t, scope)
				if err != nil {
					return resolved{}, err
				}
				for _, row := range rows {
					only[r+"\x00"+str(row["id"])] = true
				}
			}
		}
	}
	if route {
		var hits []candidate
		for _, r := range repos {
			ids, err := s.routesServedAt(ctx, r, target, scope)
			if err != nil {
				return resolved{}, err
			}
			for _, id := range ids {
				hits = append(hits, candidate{r, id})
			}
		}
		switch {
		case len(hits) == 1:
			return resolved{id: hits[0].id, repo: hits[0].repo, match: matchServedPath}, nil
		case len(hits) > 1:
			return resolved{}, s.ambiguous(fmt.Sprintf("%d HTTP routes are served at %q", len(hits), target), hits)
		}
	}
	component := lk.components && s.isComponent(ctx, repo, target)
	var found, others []candidate
	var tiers []string
	for _, r := range repos {
		nodes, err := s.opt.Store.Nodes(ctx, r, query.TargetFilter(target))
		if err != nil {
			return resolved{}, fmt.Errorf("looking up %q: %w", target, err)
		}
		resolving, partial := query.Resolve(target, nodes)
		for _, n := range resolving {
			switch {
			case only != nil && !only[r+"\x00"+n.Identity], lk.kind != "" && n.Kind != lk.kind, route && n.Kind != "route":
				others = append(others, candidate{r, n.Identity})
			default:
				found = append(found, candidate{r, n.Identity})
				tiers = append(tiers, query.MatchOf(n, target))
			}
		}
		for _, n := range partial {
			others = append(others, candidate{r, n.Identity})
		}
	}
	switch {
	case component && len(found) > 0:
		return resolved{}, s.ambiguous(fmt.Sprintf("the component %q and %d stored nodes match %q (pass %s%s for the component)", target, len(found), target, componentPrefix, target), append([]candidate{{repo, componentPrefix + target}}, found...))
	case component:
		return resolved{id: target, repo: repo, match: matchComponent}, nil
	case len(found) == 1:
		return resolved{id: found[0].id, repo: found[0].repo, match: tiers[0]}, nil
	case len(found) > 1:
		return resolved{}, s.ambiguous(fmt.Sprintf("%d %ss match %q", len(found), lk.what, target), found)
	}
	forms := "an identity, a name or a qualified-name suffix at a name boundary"
	if lk.routes {
		forms += fmt.Sprintf(", or 'METHOD /path' as served (any served path, scope %s)", scope)
	}
	if lk.components {
		forms += ", or a component directory"
	}
	if len(others) > 0 {
		return resolved{}, &toolError{code: "not_found", candidates: s.candidateList(others),
			message: fmt.Sprintf("no %s is named %q (%s); the candidates end with it inside a name or are not a %s the tool reads: call again with one identity", lk.what, target, forms, lk.what)}
	}
	return resolved{}, &toolError{code: "not_found", message: fmt.Sprintf("no %s matches %q (%s)", lk.what, target, forms)}
}

// lookupRepos is the member list a lookup reads: the given repository, or
// in project mode without one every member with a stored run.
func (s *Server) lookupRepos(e *Envelope, repo string) []string {
	if s.opt.Project == nil || repo != "" {
		return []string{repo}
	}
	var out []string
	for _, m := range e.Freshness.Members {
		if m.Index != nil {
			out = append(out, m.Repository)
		}
	}
	return out
}

func (s *Server) ambiguous(what string, list []candidate) error {
	return &toolError{code: "ambiguous", candidates: s.candidateList(list), message: what + "; call again with one identity"}
}

// candidateList is the refusal's candidates: identities, or in project
// mode {repository, identity} as the project query views list them.
func (s *Server) candidateList(list []candidate) []any {
	out := []any{}
	seen := map[candidate]bool{}
	for _, c := range list {
		if seen[c] {
			continue
		}
		seen[c] = true
		if s.opt.Project != nil {
			out = append(out, map[string]any{"repository": c.repo, "identity": c.id})
		} else {
			out = append(out, c.id)
		}
	}
	return out
}

// endpointRows reads the endpoints view's rows of one transport.
func (s *Server) endpointRows(ctx context.Context, repo, transport, scope string) ([]map[string]any, error) {
	q := url.Values{"transport": {transport}, "scope": {scope}}
	if s.opt.Project != nil {
		q.Set("repository", repo)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/api/endpoints", q)
	if err != nil {
		return nil, err
	}
	v, err := decode(raw)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	for _, t := range listOf(v["transports"]) {
		tm, _ := t.(map[string]any)
		for _, r := range listOf(tm["endpoints"]) {
			if row, ok := r.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

// routePaths are the paths of one route: those it is served at
// (server.ServedPaths) and those whose prefix the code does not fix
// (routes.PrefixUnknownPaths, T033 row UP-36).
type routePaths struct {
	served   []server.ServedPath
	prefixed []routes.PrefixUnknownPath
}

// servedPaths reads the paths of the routes of rows from the store, by
// route identity.
func (s *Server) servedPaths(ctx context.Context, repo string, rows []map[string]any) (map[string]routePaths, error) {
	out := map[string]routePaths{}
	var ids []string
	for _, row := range rows {
		ids = append(ids, str(row["id"]))
	}
	if len(ids) == 0 {
		return out, nil
	}
	nodes, err := s.opt.Store.NodesByIdentity(ctx, repo, ids)
	if err != nil {
		return nil, fmt.Errorf("reading the served paths of %d routes: %w", len(ids), err)
	}
	for _, n := range nodes {
		out[n.Identity] = routePaths{served: server.ServedPaths(n), prefixed: routes.PrefixUnknownPaths(n.Attributes)}
	}
	return out, nil
}

// inScope reports whether a served path belongs to a view scope: all
// takes every path, test the test-scope ones, production the others (N2:
// a test-only mount is never a production path).
func inScope(p server.ServedPath, scope string) bool {
	switch scope {
	case "all":
		return true
	case "test":
		return p.Scope == "test"
	}
	return p.Scope != "test"
}

// routesServedAt is the HTTP routes of repo served at target ("METHOD
// /path"): every row whose method matches and one of whose served paths
// in scope is the path.
func (s *Server) routesServedAt(ctx context.Context, repo, target, scope string) ([]string, error) {
	method, path, _ := strings.Cut(target, " ")
	rows, err := s.endpointRows(ctx, repo, "http", scope)
	if err != nil {
		return nil, err
	}
	var kept []map[string]any
	for _, row := range rows {
		if str(row["method"]) == method {
			kept = append(kept, row)
		}
	}
	paths, err := s.servedPaths(ctx, repo, kept)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, row := range kept {
		for _, p := range paths[str(row["id"])].served {
			if p.Path == path && inScope(p, scope) {
				out = append(out, str(row["id"]))
				break
			}
		}
	}
	return out, nil
}

// fileStater adds file_state to the nodes the query package serves (find,
// callers and impact), which carry none of their own (R2): the same
// query.Presenter.FileState the views use, against the stored hash, one
// read per file and repository.
type fileStater struct {
	s          *Server
	ctx        context.Context
	presenters map[string]*query.Presenter
	hashes     map[string]map[string]string
}

func (s *Server) fileStater(ctx context.Context) *fileStater {
	return &fileStater{s: s, ctx: ctx, presenters: map[string]*query.Presenter{}, hashes: map[string]map[string]string{}}
}

// state is the file state of file in repo; "" when it cannot be told
// (a synthetic node, a member without a checkout, a failed read of the
// stored hashes), which adds no file_state.
func (f *fileStater) state(repo, file string) string {
	if file == "" || file == "." {
		return ""
	}
	p, ok := f.presenters[repo]
	if !ok {
		root := f.s.opt.Root
		if f.s.opt.Project != nil {
			root = ""
			for _, m := range f.s.opt.Project.Members {
				if m.Repository == repo && m.Checkout {
					root = m.Path
				}
			}
		}
		if root != "" {
			hashes, err := f.s.opt.Store.FileHashes(f.ctx, repo)
			if err == nil {
				p = query.NewPresenter(root, config.IDE{})
				f.hashes[repo] = hashes
			}
		}
		f.presenters[repo] = p
	}
	if p == nil {
		return ""
	}
	return string(p.FileState(file, f.hashes[repo][file]))
}

// add sets node's file_state (a compacted node with file and, in project
// mode, repository) and records it on the envelope.
func (f *fileStater) add(e *Envelope, node map[string]any, repo string) {
	if _, ok := node["file_state"]; ok {
		return
	}
	if r := str(node["repository"]); r != "" {
		repo = r
	}
	if st := f.state(repo, str(node["file"])); st != "" {
		node["file_state"] = st
		e.fileState(str(node["file"]), st)
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/extract/routes"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/server"
	"github.com/graphvein/graphvein/internal/traverse"
)

// tools are the registered tools in tools/list order, each with its own
// toolFor function; the envelope, freshness, labels, paging and refusals
// are shared.
var tools = []tool{
	{name: "status", title: "Index status", project: true, register: toolFor(statusTool),
		description: "Is the index current, and what can be trusted? Freshness (changed files since the last index, paged), the last run, extractor match and precision refusal; in project mode every member and the link pass."},
	{name: "find", title: "Find stored nodes", project: true, register: toolFor(findTool),
		description: "Where is X defined? Stored nodes whose identity, exact name or qualified-name suffix matches term, with file and line. No match means no stored node matches, never that it does not exist."},
	{name: "endpoints", title: "List endpoints", project: true, register: toolFor(endpointsTool),
		description: "Which routes (or IPC commands, server actions) exist? Rows of the API view with caller counts and flags, filtered by method and path prefix, paged; caller_count 0 is 'no stored caller', never 'dead'."},
	{name: "endpoint", title: "Endpoint detail", project: true, register: toolFor(endpointTool),
		description: "What does this route do? Handler, request and response shape by section state, callers with their stored edges and match, and near misses (candidates, never callers). target: identity, 'METHOD /path' as served, or a name."},
	{name: "source", title: "Source lines", project: true, register: toolFor(sourceTool),
		description: "Show the current lines of a node, highlighted only when they are proven to be the indexed ones; any other state is shown with its reason."},
	{name: "callers", title: "Callers", project: true, register: toolFor(callersTool),
		description: "Who calls this function or route? Stored call paths up to depth (default 1, a route 2; at most 10), with provenance and confidence = weakest edge, the caps that bound, '? not modelled' ends and, for a route, near misses. An empty result with a bound cap means unknown beyond the cap, not none."},
	{name: "impact", title: "Impact", project: true, register: toolFor(impactTool),
		description: "What depends on this code? Stored dependents up to depth (default 3; at most 10): 'depends on' for a fact path, 'may depend on' for an inferred one, never 'breaks'; blind spots are possibly missing callers, never results."},
	{name: "chain", title: "Architecture chain", project: true, register: toolFor(chainTool),
		description: "The full path from UI code and client calls through routes and handlers to tables, up (towards callers), down (towards callees and tables) or both, from a node or a component: members per step with the edge wording, row and tier (fact or inferred with the weakest confidence), table edges with their access verification. Depth default 6, at most 10; bound caps, '? not modelled' ends and edges not followed are reported. An end is never 'unused'."},
	{name: "table", title: "Table schema and users", project: true, register: toolFor(tableTool),
		description: "A table's schema (columns with their stored facts, claims by verification oracle or mapped, indexes, foreign keys, history, completeness, assumptions) and the code that reads, writes or builds a query on it with each use's verification and assumptions. Users are a lower bound (20 per page; include schema or users reads one half): sites that may also use the table are listed as unknowns, and a table whose access is not extracted says so; never 'no users'."},
	{name: "changes", title: "Changes since a base", project: true, register: toolFor(changesTool),
		description: "What changed in the facts between a stored base (previous run by default, run:ID, pin:NAME, a stored ref: snapshot) and the current index: the summary by group and the items with row, change, cause and labels, paged. A base that is not stored is refused; the server never indexes."},
	{name: "change_impact", title: "Impact of a change", project: true, register: toolFor(changeImpactTool),
		description: "What a changed item (or group) reaches: paths 'depends on' (fact) or 'may depend on' (inferred, confidence = weakest step), never 'breaks', depth default 3, at most 10; blind spots, near misses, gaps, table uses (a lower bound) and caps; in project mode across repositories through stored links, each with its assumption."},
	{name: "unresolved", title: "Unresolved calls and uncalled endpoints", project: true, register: toolFor(unresolvedTool),
		description: "What Graphvein could not link: the calls of a transport by status (dynamic, unmatched, ambiguous, ...) with their reasons, never links, and the endpoints with no stored caller in this scope, worded only so ('no stored caller', never 'dead')."},
	{name: "overview", title: "Repository overview", project: true, register: toolFor(overviewTool),
		description: "What the repository is made of: components (paged) with kinds, frameworks and counts, links by family with their weakest tier and cell states, databases, the schema summary and the precision summary (rules, labels, file counts). Unread components and unknown cells are unknowns."},
	{name: "link_changes", title: "Project link changes", project: true, register: toolFor(linkChangesTool),
		description: "Project mode only: which cross-repository links changed between two kept link passes (the pass before the newest by default, or pass:N): links added, removed or changed with their kind and ends, paged. It reads the stored link journal only and never links or indexes; a single-repository server refuses it."},
}

// Arguments of each tool. Zero values are the defaults; the request echo
// shows the value used.

type statusArgs struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"changed files per page (default 50, at most 500)"`
	Cursor string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

type findArgs struct {
	Term       string `json:"term" jsonschema:"identity, exact name or qualified-name suffix (case-sensitive)"`
	Kind       string `json:"kind,omitempty" jsonschema:"node kind, for example function, method, route, table"`
	File       string `json:"file,omitempty" jsonschema:"exact repository-relative file"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member to search (required there)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"results per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

type endpointsArgs struct {
	Transport  string `json:"transport,omitempty" jsonschema:"http (default), tauri_ipc or server_action"`
	Method     string `json:"method,omitempty" jsonschema:"HTTP method filter, for example GET"`
	PathPrefix string `json:"path_prefix,omitempty" jsonschema:"keep rows whose path (or pattern) starts with this"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default), test or all"`
	Detail     string `json:"detail,omitempty" jsonschema:"compact (default: no per-route shape summary) or full"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"rows per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

type endpointArgs struct {
	Target     string `json:"target" jsonschema:"endpoint identity, 'METHOD /path' as the endpoints tool lists it, or a stored name"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default), test or all"`
	Detail     string `json:"detail,omitempty" jsonschema:"compact (default: shape states and field names and types) or full (every shape item with its provenance, OpenAPI operation attributes)"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"callers per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

type sourceArgs struct {
	Target     string `json:"target" jsonschema:"node identity, 'METHOD /path' as served, a name or a qualified-name suffix at a name boundary"`
	Context    *int   `json:"context,omitempty" jsonschema:"lines around the span (default 8, at most 50)"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
}

type walkArgs struct {
	Target     string `json:"target" jsonschema:"node identity, 'METHOD /path' as served (the route), a name or a qualified-name suffix at a name boundary"`
	Depth      int    `json:"depth,omitempty" jsonschema:"steps to walk (callers default 1, a route 2; impact default 3; at most 10)"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default: results in test code left out and counted) or all"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member whose nodes the target is looked up in"`
	Limit      int    `json:"limit,omitempty" jsonschema:"results per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
	unknownsArgs
}

// requireIndexed refuses a tool other than status on a repository with no
// stored run (MC-8): there is no fact to answer from.
func requireIndexed(s *Server, e *Envelope) error {
	if s.opt.Project == nil && e.Freshness.State == "not_indexed" {
		return &toolError{code: "not_indexed", message: "the repository is not indexed: run graphvein index first"}
	}
	return nil
}

// memberQuery adds the repository argument in project mode.
func memberQuery(s *Server, q url.Values, repository string) {
	if s.opt.Project != nil && repository != "" {
		q.Set("repository", repository)
	}
}

func statusTool(ctx context.Context, s *Server, in statusArgs, e *Envelope) error {
	tagRow("MC-9")
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	e.Request["limit"], e.Request["cursor"] = n, in.Cursor
	tagRow("MC-2")
	if s.opt.Project != nil {
		var changed []any
		for _, m := range e.Freshness.Members {
			r := map[string]any{"kind": "member", "repository": m.Repository, "checkout": m.Checkout, "state": m.State, "changed_count": m.ChangedCount, "index": m.Index}
			if m.Error != "" {
				r["error"] = m.Error
			}
			if m.Settings != nil {
				r["settings"] = settingsResult(e, m.Settings, "member "+m.Repository+": ")
			}
			changed = append(changed, r)
			if m.Index != nil && m.Index.ExtractorMatches != nil && !*m.Index.ExtractorMatches {
				e.unknown("MC-9", "extractor_mismatch", "member "+m.Repository+" was indexed by another extractor version: no label is precise until it is re-indexed", nil)
			}
		}
		e.Results = page(e, changed, offset, n)
		if lp := e.Freshness.LinkPass; lp != nil && len(lp.StaleReasons) > 0 {
			e.unknown("MC-18", "link_pass_stale", "the link pass is stale: project links may differ if linked now", lp.StaleReasons)
		}
		return nil
	}
	raw, err := s.reader.Read(ctx, "/api/v0/precision", nil)
	if err != nil {
		return err
	}
	pv, err := decode(raw)
	if err != nil {
		return err
	}
	var refusal any
	if run, ok := pv["run"].(map[string]any); ok {
		refusal = run["refusal"]
	}
	// The whole changed-file list (the view lists up to 1,000 with the
	// exact count) is paged here; the envelope shows the first 50. The
	// check is this call's own (status never reuses one, R1).
	if e.status == nil {
		return fmt.Errorf("status: no freshness check read")
	}
	v := *e.status
	idx := map[string]any{"kind": "index", "indexed": v.Index.Indexed, "index": e.Freshness.Index, "precision_refusal": refusal}
	if v.Freshness.Settings != nil {
		idx["settings"] = settingsResult(e, v.Freshness.Settings, "")
	}
	files := []any{idx}
	for _, f := range v.Freshness.ChangedFiles {
		files = append(files, map[string]any{"kind": "changed_file", "file": f})
	}
	e.Results = page(e, files, offset, n)
	if v.Freshness.ChangedFilesTruncated && v.Freshness.ChangedCount != nil {
		tagRow("MC-3")
		e.label("bound", "changed_files")
		e.Bounds = append(e.Bounds, Bound{Cap: "changed_files", Limit: len(v.Freshness.ChangedFiles), Beyond: intp(*v.Freshness.ChangedCount - len(v.Freshness.ChangedFiles)), Row: "MC-3",
			Text: fmt.Sprintf("changed-file list cap reached (%d): %d more changed files not listed; the count is exact", len(v.Freshness.ChangedFiles), *v.Freshness.ChangedCount-len(v.Freshness.ChangedFiles))})
	}
	if e.Freshness.Index != nil && e.Freshness.Index.Commit == nil {
		e.unknown("MC-9", "commit_not_recorded", "the run recorded no commit", nil)
	}
	if e.Freshness.Index != nil && e.Freshness.Index.ExtractorMatches != nil && !*e.Freshness.Index.ExtractorMatches {
		e.unknown("MC-9", "extractor_mismatch", "the index was built by another extractor version: no label is precise until re-indexed", nil)
	}
	if refusal != nil {
		e.unknown("MC-9", "precision_refusal", "the precision view refuses precise labels for this run", refusal)
	}
	return nil
}

// settingsResult maps a settings comparison into a status result (row
// MC-33): the state, the keys and the reason as the view sent them. A
// change is an unknown (the index was built with other settings: its
// facts may differ after a re-index); so is an unknown comparison, which
// is never restated as unchanged. who prefixes the texts in project mode.
func settingsResult(e *Envelope, v *settingsView, who string) map[string]any {
	tagRow("MC-33")
	out := map[string]any{"state": v.State, "changed_keys": nonNil(v.ChangedKeys), "unknown_keys": nonNil(v.UnknownKeys)}
	if v.Reason != nil {
		out["reason"] = *v.Reason
	}
	switch v.State {
	case "unchanged", "not_indexed":
	case "changed":
		e.unknown("MC-33", "settings_changed", who+"graphvein.yaml changed an index setting since the last index run ("+strings.Join(v.ChangedKeys, ", ")+"): facts may differ after a re-index", v.ChangedKeys)
	default:
		reason := "no reason given"
		if v.Reason != nil {
			reason = *v.Reason
		}
		e.unknown("MC-33", "settings_unknown", who+"whether the index was built with the current graphvein.yaml settings is not known: "+reason, nonNil(v.UnknownKeys))
	}
	return out
}

// nonNil keeps an absent list an empty list in the answer.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func findTool(ctx context.Context, s *Server, in findArgs, e *Envelope) error {
	tagRow("MC-10")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Term) == "" {
		return badRequest("term is required")
	}
	root, repo, err := s.member(in.Repository, true)
	if err != nil {
		return err
	}
	tagRow("MC-2")
	e.Request["term"], e.Request["kind"], e.Request["file"], e.Request["limit"], e.Request["cursor"] = in.Term, in.Kind, in.File, n, in.Cursor
	if s.opt.Project != nil {
		e.Request["repository"] = repo
	}
	out, err := query.Execute(ctx, s.opt.Store, query.Options{Command: "query", Term: in.Term, Root: root, Repository: repo, Kind: in.Kind, File: in.File, Fresh: s.queryFreshFor(e)})
	if err != nil {
		return err
	}
	var nodes []any
	states := s.fileStater(ctx)
	for _, node := range out.Nodes {
		m, err := toMap(node)
		if err != nil {
			return err
		}
		r := compactNode(e, m)
		if s.opt.Project != nil {
			r["repository"] = repo
		}
		states.add(e, r, repo)
		nodes = append(nodes, r)
	}
	e.Results = page(e, nodes, offset, n)
	if len(nodes) == 0 {
		e.unknown("MC-10", "no_match", "no stored node matches; this is not proof it does not exist (ignored files, files with gaps and languages not read have no nodes)", nil)
	}
	return nil
}

// member resolves the repository argument to a checkout root and ID. In
// single-repository mode it must be empty or the repository itself.
func (s *Server) member(repository string, needCheckout bool) (root, repo string, err error) {
	if s.opt.Project == nil {
		if repository != "" && repository != s.opt.Repository {
			return "", "", badRequest("this server serves repository %s only", s.opt.Repository)
		}
		return s.opt.Root, s.opt.Repository, nil
	}
	if repository == "" {
		if len(s.opt.Project.Members) == 1 {
			repository = s.opt.Project.Members[0].Repository
		} else {
			var names []string
			for _, m := range s.opt.Project.Members {
				names = append(names, m.Repository)
			}
			return "", "", &toolError{code: "repository_required", message: "project " + s.opt.Project.Name + " has members " + strings.Join(names, ", ") + "; name one with repository"}
		}
	}
	for _, m := range s.opt.Project.Members {
		if m.Repository == repository {
			if needCheckout && !m.Checkout {
				return "", "", &toolError{code: "no_checkout", message: "member " + repository + " has no checkout here"}
			}
			return m.Path, m.Repository, nil
		}
	}
	return "", "", badRequest("repository %q is not a member of project %s", repository, s.opt.Project.Name)
}

func endpointsTool(ctx context.Context, s *Server, in endpointsArgs, e *Envelope) error {
	tagRow("MC-11")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	transport, scope := orDefault(in.Transport, "http"), orDefault(in.Scope, "production")
	detail, err := detailParam(in.Detail)
	if err != nil {
		return err
	}
	tagRow("MC-2")
	e.Request["transport"], e.Request["method"], e.Request["path_prefix"], e.Request["scope"], e.Request["detail"], e.Request["limit"], e.Request["cursor"] = transport, in.Method, in.PathPrefix, scope, detail, n, in.Cursor
	q := url.Values{"transport": {transport}, "scope": {scope}}
	if s.opt.Project != nil {
		_, repo, err := s.member(in.Repository, false)
		if err != nil {
			return err
		}
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/api/endpoints", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	ts, _ := v["transports"].([]any)
	if len(ts) != 1 {
		return fmt.Errorf("endpoints view answered %d transports for %s", len(ts), transport)
	}
	t, _ := ts[0].(map[string]any)
	rows, _ := t["endpoints"].([]any)
	summary := map[string]any{}
	for _, k := range sortedKeys(t) {
		val := t[k]
		switch k {
		case "endpoints":
		case "transport", "frameworks", "endpoint_count", "never_called", "calls", "roots", "openapi", "never_called_in_project", "possibly_called_in_project", "project_linked_calls":
			summary[k] = val
		case "precision", "callers_precision":
			summary[k] = val
			labelPrecision(e, val)
		default:
			e.notModelled("endpoints."+k, val)
		}
	}
	e.Summary = summary
	var byMethod []map[string]any
	for _, r := range rows {
		row, _ := r.(map[string]any)
		if in.Method != "" && !strings.EqualFold(str(row["method"]), in.Method) {
			continue
		}
		byMethod = append(byMethod, row)
	}
	// Every path a route is served at counts (R8): a router mounted at
	// two prefixes is listed under either; the row shows one path and
	// served_paths lists them all.
	served, err := s.servedPaths(ctx, repoOf(s, q), byMethod)
	if err != nil {
		return err
	}
	var kept []any
	for _, row := range byMethod {
		rp := served[str(row["id"])]
		paths := rp.served
		if len(paths) > 1 {
			// Every path with its scope; the filter reads the ones in
			// the request's scope only (N2).
			row["served_paths"] = paths
		}
		if in.PathPrefix != "" && !anyPrefix(paths, rp.prefixed, scope, str(row["pattern"]), in.PathPrefix) {
			continue
		}
		for _, f := range listOf(row["flags"]) {
			e.label("flag", str(f))
		}
		if row["prefix_unknown"] != nil {
			// T033 row UP-36: path stays null; prefix_unknown is the
			// known part with "…" for the prefix, never a full path.
			e.label("path", "prefix_unknown")
		}
		if cc, ok := row["caller_count"].(json.Number); ok && cc.String() == "0" {
			e.label("claim", "no stored caller")
		}
		if detail == detailCompact {
			row = compactRow(e, row)
		}
		kept = append(kept, row)
	}
	e.Results = page(e, kept, offset, n)
	if len(kept) == 0 && (in.Method != "" || in.PathPrefix != "") {
		e.unknown("MC-11", "no_row_matches_filter", "no endpoint row of this transport and scope matches the filter (every served path compared): not proof that no such endpoint exists", nil)
	}
	return nil
}

// repoOf is the repository a view query names (project mode), else the
// one repository served.
func repoOf(s *Server, q url.Values) string {
	if r := q.Get("repository"); r != "" {
		return r
	}
	return s.opt.Repository
}

// anyPrefix reports whether one of a route's served paths in scope
// starts with prefix. A route without a served path matches by its known
// part: a path whose prefix the code does not fix
// (T033 UP-36) when its known part above the unknown prefix, or else the
// proven path below it, starts with prefix; a route with neither by its
// pattern. Such a row keeps prefix_unknown and is never a served path.
func anyPrefix(paths []server.ServedPath, prefixed []routes.PrefixUnknownPath, scope, pattern, prefix string) bool {
	if len(paths) == 0 {
		for _, p := range prefixed {
			known := p.Template
			if p.Above != "" {
				known = p.Above
			}
			if strings.HasPrefix(known, prefix) {
				return true
			}
		}
		return pattern != "" && strings.HasPrefix(pattern, prefix)
	}
	for _, p := range paths {
		if inScope(p, scope) && strings.HasPrefix(p.Path, prefix) {
			return true
		}
	}
	return false
}

func endpointTool(ctx context.Context, s *Server, in endpointArgs, e *Envelope) error {
	tagRow("MC-12")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	scope := orDefault(in.Scope, "production")
	detail, err := detailParam(in.Detail)
	if err != nil {
		return err
	}
	tagRow("MC-2")
	e.Request["target"], e.Request["scope"], e.Request["detail"], e.Request["limit"], e.Request["cursor"] = in.Target, scope, detail, n, in.Cursor
	q := url.Values{"scope": {scope}}
	_, repo, err := s.member(in.Repository, false)
	if err != nil {
		return err
	}
	if s.opt.Project != nil {
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	r, err := s.resolve(ctx, e, repo, in.Target, targetLookup{what: "endpoint", routes: true, endpoints: true, scope: scope})
	if err != nil {
		return err
	}
	id := r.id
	q.Set("id", id)
	raw, err := s.reader.Read(ctx, "/api/v0/api/endpoint", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	out := map[string]any{}
	var callers, nearMisses []any
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch k {
		case "api_version", "repository", "index", "scope", "root":
			// Echoed by the envelope (request, freshness).
		case "ide", "shape_links":
			e.omit(k)
		case "endpoint":
			m, _ := val.(map[string]any)
			for ck, cv := range compactNode(e, m) {
				out[ck] = cv
			}
			if attrs, ok := m["attributes"].(map[string]any); ok {
				for _, a := range []string{"method", "pattern", "framework", "command", "action"} {
					if av, ok := attrs[a]; ok {
						out[a] = av
					}
				}
				if _, ok := attrs["paths"]; ok {
					// Every path the route is served at (R8).
					out["served_paths"] = server.ServedPaths(contract.Node{Attributes: attrs})
					if list := routes.PathAssumptions(attrs); len(list) > 0 {
						// Step UP-F (MC-12): a served path that rests on an
						// assumption (GR-V5, RO-V1, RO-V2) names it, with its
						// text in the envelope's assumptions.
						out["path_assumptions"] = list
						for _, p := range list {
							for _, id := range p.Assumptions {
								e.addAssumption(Assumption{ID: id, Text: routes.PathAssumptionText[id], Row: "MC-12"})
							}
						}
					}
				}
				if list := routes.PrefixUnknownPaths(attrs); len(list) > 0 {
					// T033 row UP-36: the paths whose prefix the code does
					// not fix, each with its reason; never served paths.
					out["prefix_unknown_paths"] = list
					e.label("path", "prefix_unknown")
				}
			}
		case "project":
			out[k] = projectEndpoint(e, val)
		case "openapi":
			out[k] = val
			if detail == detailCompact {
				out[k] = compactOpenAPI(e, val)
			}
		case "transport", "model_tables", "near_misses_incomplete":
			out[k] = val
		case "precision", "callers_precision":
			out[k] = val
			labelPrecision(e, val)
		case "handler":
			h := handlerOf(e, val)
			out[k] = h
		case "shape":
			out[k] = val
			labelShape(e, val)
			if detail == detailCompact {
				out[k] = compactShape(e, val)
			}
		case "callers":
			for _, c := range listOf(val) {
				callers = append(callers, callEntry(e, c, "calls this route", id))
			}
		case "near_misses":
			for _, c := range listOf(val) {
				nearMisses = append(nearMisses, callEntry(e, c, "", ""))
			}
		default:
			e.notModelled("endpoint."+k, val)
		}
	}
	if callers == nil {
		callers = []any{}
	}
	if nearMisses == nil {
		nearMisses = []any{}
	}
	// The callers list is the paged list; near misses are capped upstream
	// (candidate_count_exact says when a count is a lower bound).
	out["callers"] = page(e, callers, offset, n)
	out["near_misses"] = nearMisses
	for _, nm := range nearMisses {
		m, _ := nm.(map[string]any)
		e.label("near_miss", str(m["relation"]))
		if exact, ok := m["candidate_count_exact"].(bool); ok && !exact {
			e.unknown("MC-12", "candidate_count_at_least", fmt.Sprintf("near miss %s: candidate_count %v is a lower bound", str(m["id"]), m["candidate_count"]), nil)
		}
	}
	if nmi, ok := v["near_misses_incomplete"].(json.Number); ok && nmi.String() != "0" {
		e.unknown("MC-12", "near_misses_incomplete", nmi.String()+" calls may also be near misses but their candidate lists were cut upstream", nil)
	}
	projectCallers := 0
	if pm, ok := out["project"].(map[string]any); ok {
		projectCallers = len(listOf(pm["callers"]))
	}
	if len(callers) == 0 && projectCallers == 0 {
		e.unknown("MC-12", "no_stored_caller", "no stored caller in this scope: not proof that nothing calls it (dynamic, unmatched and ambiguous calls are listed by the near misses or not linked)", nil)
	}
	e.Results = []any{out}
	return nil
}

// projectEndpoint maps the endpoint view's project part (serve --project):
// callers and operations of other members through stored project links,
// each a "may" with its assumption and stale reasons (MC-18); the rest is
// passed on as served.
func projectEndpoint(e *Envelope, val any) map[string]any {
	tagRow("MC-18")
	pm, _ := val.(map[string]any)
	out := map[string]any{}
	for _, k := range sortedKeys(pm) {
		switch k {
		case "callers", "operations":
			var list []any
			for _, item := range listOf(pm[k]) {
				m, _ := item.(map[string]any)
				entry := map[string]any{}
				for _, ik := range sortedKeys(m) {
					switch ik {
					case "node":
						n, _ := m[ik].(map[string]any)
						entry[ik] = compactNode(e, n)
					case "evidence":
						e.omit("project.evidence")
					case "ide_status":
						// How the UI's editor link was built; the node's
						// editor link is left out (node.ide) too.
						e.omit("project.ide_status")
					default:
						entry[ik] = m[ik]
					}
				}
				e.label("provenance", str(m["provenance"]))
				if a := str(m["assumption"]); a != "" {
					e.Assumptions = appendAssumption(e.Assumptions, a)
				}
				if st, _ := m["stale"].(bool); st {
					e.unknown("MC-18", "stale_link", "a project link of this endpoint is stale", m["stale_reasons"])
				}
				list = append(list, entry)
			}
			if list == nil {
				list = []any{}
			}
			out[k] = list
		case "links_precision":
			out[k] = pm[k]
			labelPrecision(e, pm[k])
		default:
			out[k] = pm[k]
		}
	}
	return out
}

func sourceTool(ctx context.Context, s *Server, in sourceArgs, e *Envelope) error {
	tagRow("MC-13")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	q := url.Values{}
	tagRow("MC-2")
	e.Request["target"] = in.Target
	if in.Context != nil {
		q.Set("context", strconv.Itoa(*in.Context))
		e.Request["context"] = *in.Context
	} else {
		e.Request["context"] = server.DefaultSourceContext
	}
	_, repo, err := s.member(in.Repository, false)
	if err != nil {
		return err
	}
	if s.opt.Project != nil {
		e.Request["repository"] = repo
		q.Set("repository", repo)
	}
	r, err := s.resolve(ctx, e, repo, in.Target, targetLookup{what: "stored node", routes: true})
	if err != nil {
		return err
	}
	q.Set("id", r.id)
	raw, err := s.reader.Read(ctx, "/api/v0/source", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	out := map[string]any{}
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch k {
		case "api_version", "repository", "index":
		case "ide":
			e.omit(k)
		case "node":
			m, _ := val.(map[string]any)
			out["node"] = compactNode(e, m)
		case "source":
			src, _ := val.(map[string]any)
			state := str(src["state"])
			e.label("source", state)
			if state != "exact" && state != "text_match" {
				e.unknown("MC-13", "source_"+state, "no highlight: "+orDefault(str(src["reason"]), state), nil)
			}
			if f, ok := src["file"].(map[string]any); ok {
				if p, ok := f["precision"].(string); ok {
					e.label("precision", p)
				}
			}
			if cut := listOf(src["cut_lines"]); len(cut) > 0 {
				tagRow("MC-3")
				e.label("bound", "cut_lines")
				e.Bounds = append(e.Bounds, Bound{Cap: "cut_lines", Limit: server.MaxLineBytes, Beyond: intp(len(cut)), Row: "MC-3", Text: fmt.Sprintf("%d lines cut at %d bytes", len(cut), server.MaxLineBytes)})
			}
			if t, _ := src["truncated"].(bool); t {
				tagRow("MC-3")
				e.label("bound", "lines")
				e.Bounds = append(e.Bounds, Bound{Cap: "lines", Limit: server.MaxSourceLines, Row: "MC-3", Text: fmt.Sprintf("line window cut at %d lines", server.MaxSourceLines)})
			}
			out["source"] = src
		case "code_calls_precision":
			out[k] = val
			labelPrecision(e, val)
		default:
			e.notModelled("source."+k, val)
		}
	}
	e.Results = []any{out}
	return nil
}

func callersTool(ctx context.Context, s *Server, in walkArgs, e *Envelope) error {
	tagRow("MC-14")
	return walkTool(ctx, s, "callers", in, e)
}

func impactTool(ctx context.Context, s *Server, in walkArgs, e *Envelope) error {
	tagRow("MC-16")
	return walkTool(ctx, s, "impact", in, e)
}

// walkTool answers callers and impact from query.Execute (one repository)
// or the project query view (project mode), whose depth maxima and
// defaults they keep (D2): one repository at most traverse.MaxDepth, a
// project at most query.ProjectMaxDepth.
func walkTool(ctx context.Context, s *Server, command string, in walkArgs, e *Envelope) error {
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Target) == "" {
		return badRequest("target is required")
	}
	tagRow("MC-7")
	tagRow("MC-2")
	e.Request["target"], e.Request["depth"] = in.Target, in.Depth
	max := query.MaxDepth
	if s.opt.Project != nil {
		max = query.ProjectMaxDepth
	}
	depth := in.Depth
	if depth == 0 {
		depth = 1
		if command == "impact" {
			depth = 3
		}
	}
	if depth < 0 || depth > max {
		return badRequest("depth must be 1 to %d, not %d", max, in.Depth)
	}
	tagRow("MC-2")
	e.Request["target"], e.Request["depth"], e.Request["limit"], e.Request["cursor"] = in.Target, depth, n, in.Cursor
	if err := e.unknownsPaging(in.unknownsArgs); err != nil {
		return err
	}
	// Production scope leaves out results in test code (MC-31).
	scope, err := walkScopeParam(in.Scope)
	if err != nil {
		return err
	}
	e.Request["scope"] = scope
	if s.opt.Project == nil && in.Repository != "" && in.Repository != s.opt.Repository {
		return badRequest("this server serves repository %s only", s.opt.Repository)
	}
	repo := in.Repository
	if s.opt.Project == nil {
		repo = s.opt.Repository
	}
	// The target is resolved here, by the one rule of every tool (MC-7):
	// a "METHOD /path" target names the route served there first, and a
	// partial name is a candidate only. The walk then gets the identity.
	r, err := s.resolve(ctx, e, repo, in.Target, targetLookup{what: "stored node", routes: true})
	if err != nil {
		return err
	}
	var v map[string]any
	if s.opt.Project != nil {
		q := url.Values{"term": {r.id}, "depth": {strconv.Itoa(depth)}}
		if r.repo != "" {
			q.Set("repository", r.repo)
			e.Request["repository"] = r.repo
		}
		raw, err := s.reader.Read(ctx, "/api/v0/project/"+command, q)
		if err != nil {
			return err
		}
		if v, err = decode(raw); err != nil {
			return err
		}
	} else {
		out, err := query.Execute(ctx, s.opt.Store, query.Options{Command: command, Term: r.id, Root: s.opt.Root, Repository: s.opt.Repository, Depth: depth, Fresh: s.queryFreshFor(e)})
		if err != nil {
			return err
		}
		if v, err = toMap(out); err != nil {
			return err
		}
	}
	switch str(v["error"]) {
	case "":
	case "ambiguous target":
		return &toolError{code: "ambiguous", message: fmt.Sprintf("several stored nodes match %q; call again with one identity", in.Target), candidates: listOf(v["candidates"])}
	case "no matching target":
		return &toolError{code: "not_found", message: fmt.Sprintf("no stored node matches %q (identity, exact name or qualified-name suffix)", in.Target)}
	default:
		return &toolError{code: "store_error", message: str(v["error"])}
	}
	target, _ := v["target"].(map[string]any)
	targetNode := compactNode(e, target)
	states := s.fileStater(ctx)
	states.add(e, targetNode, r.repo)
	e.Request["resolved"] = targetNode["id"]
	specOperationNote(e, str(targetNode["kind"]))
	if command == "callers" && str(targetNode["kind"]) == "route" && depth < 2 {
		// A route's callers are the client calls and their owners.
		e.Request["depth"] = 2
		e.Request["depth_note"] = "a route's callers are its client calls (step 1) and the code that makes them (step 2): depth raised to 2"
	}
	if s.opt.Project != nil {
		e.Request["repository"] = str(target["repository"])
	}
	effective := depth
	if command == "callers" && str(targetNode["kind"]) == "route" && depth < 2 {
		effective = 2
	}
	// The stored attributes of the target and every reached node, for the
	// origin assumptions of the calls_route steps (LF-1) a path takes.
	nodeAttrs := map[string]map[string]any{}
	for _, nv := range append([]any{target}, listOf(v["results"])...) {
		nm, _ := nv.(map[string]any)
		if rn, ok := nm["node"].(map[string]any); ok {
			nm = rn
		}
		if attrs, ok := nm["attributes"].(map[string]any); ok {
			nodeAttrs[str(nm["identity"])] = attrs
		}
	}
	ws := newWalkScope(s, scope)
	if err := ws.classify(listOf(v["results"])); err != nil {
		return err
	}
	targetKey := nodeKey(str(target["repository"]), str(target["identity"]))
	hiddenResults, hiddenEnds := 0, 0
	var ends []any
	known := map[string]bool{"api_version": true, "project": true, "command": true, "members": true, "link_pass": true, "freshness": true, "target": true, "nodes": true, "error": true, "candidates": true, "total": true}
	var results []any
	// The files of the shown results, by repository, for MC-16's count of
	// unresolved call sites in them when production scope hid results.
	shownFiles := map[string]map[string]bool{}
	for _, k := range sortedKeys(v) {
		val := v[k]
		switch {
		case known[k]:
		case k == "results":
			for _, res := range listOf(val) {
				rm, _ := res.(map[string]any)
				node, _ := rm["node"].(map[string]any)
				if ws.hidden(str(node["repository"]), str(node["identity"])) {
					hiddenResults++
					continue
				}
				wr := walkResult(e, command, res, nodeAttrs)
				markTest(wr, ws.isTest(str(node["repository"]), str(node["identity"])))
				if ws.viaTest(rm, targetKey, nodeKey(str(node["repository"]), str(node["identity"]))) {
					wr["via_test_code"] = true
				}
				if file := str(node["file"]); file != "" && file != "." {
					if shownFiles[str(node["repository"])] == nil {
						shownFiles[str(node["repository"])] = map[string]bool{}
					}
					shownFiles[str(node["repository"])][file] = true
				}
				states.add(e, wr, r.repo)
				results = append(results, wr)
			}
		case k == "bounds":
			for _, b := range listOf(val) {
				e.Bounds = append(e.Bounds, walkBound(e, b, effective, max))
			}
		case k == "caps":
		case k == "ends":
			for _, end := range listOf(val) {
				em, _ := end.(map[string]any)
				if nodeKey(str(em["repository"]), str(em["node"])) != targetKey && ws.hidden(str(em["repository"]), str(em["node"])) {
					hiddenEnds++
					continue
				}
				ends = append(ends, end)
			}
		case k == "possibly_missing_callers", k == "unknown_calls_in_result_files", k == "possibly_missing_call_sites", k == "blind_spot_data":
		case k == "possible_callers":
			for _, pc := range listOf(val) {
				if m, ok := pc.(map[string]any); ok {
					if err := ws.markPossibleCaller(ctx, m); err != nil {
						return err
					}
					labelTest(e, []any{m})
				}
				e.unknown("MC-18", "possible_caller", "a call of another member may call this route without a project link (ambiguous or incomplete link outcome): may be called by, never a caller", pc)
			}
		default:
			e.notModelled(command+"."+k, val)
		}
	}
	if command == "impact" {
		if hiddenResults > 0 {
			count, err := ws.resultFileUnknownCalls(ctx, shownFiles)
			if err != nil {
				return err
			}
			v["unknown_calls_in_result_files"] = json.Number(strconv.Itoa(count))
		}
		if err := ws.markSites(ctx, listOf(v["possibly_missing_call_sites"])); err != nil {
			return err
		}
		blindSpots(e, v)
	}
	if results == nil {
		results = []any{}
	}
	sortResults(results)
	e.Results = page(e, results, offset, n)
	labelTest(e, e.Results)
	walkEnds(e, ends, str(targetNode["id"]), e.Results)
	ws.report(e, hiddenResults, hiddenEnds)
	if len(results) == 0 {
		text := "no stored caller within the depth: not proof that nothing calls it (see bounds, ends and near misses)"
		if command == "impact" {
			text = "no stored dependent within the depth: not proof that nothing depends on it (see bounds, ends and blind spots)"
		}
		if hiddenResults > 0 {
			text = "outside test code, " + text + "; test-code results are counted in test_scope_hidden"
		}
		e.unknown(map[string]string{"callers": "MC-14", "impact": "MC-16"}[command], "no_result", text, nil)
	}
	if command == "callers" && str(targetNode["kind"]) == "route" {
		if err := s.routeNearMisses(ctx, e, ws, str(targetNode["id"]), str(target["repository"])); err != nil {
			return err
		}
	}
	return nil
}

// routeNearMisses adds the endpoint view's near misses of a route to its
// callers (G3, MC-15): calls that list the route among candidates without
// a stored edge are unknowns, never callers.
func (s *Server) routeNearMisses(ctx context.Context, e *Envelope, ws *walkScope, id, repository string) error {
	tagRow("MC-15")
	q := url.Values{"id": {id}, "scope": {"all"}}
	if s.opt.Project != nil {
		q.Set("repository", repository)
	}
	raw, err := s.reader.Read(ctx, "/api/v0/api/endpoint", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	for _, nm := range listOf(v["near_misses"]) {
		entry := callEntry(e, nm, "", "")
		nmm, _ := nm.(map[string]any)
		node, _ := nmm["node"].(map[string]any)
		if err := ws.markNodeTest(entry, repository, node); err != nil {
			return err
		}
		labelTest(e, []any{entry})
		e.label("near_miss", str(entry["relation"]))
		e.unknown("MC-15", "near_miss", "may call this route: "+str(entry["relation"])+" (not linked, never a caller)", entry)
	}
	if nmi, ok := v["near_misses_incomplete"].(json.Number); ok && nmi.String() != "0" {
		e.unknown("MC-15", "near_misses_incomplete", nmi.String()+" calls may also be near misses but their candidate lists were cut upstream", nil)
	}
	return nil
}

// blindSpots maps impact's blind spots (MC-16): possibly missing callers
// are unknowns, never results; unavailable data is said as such.
func blindSpots(e *Envelope, v map[string]any) {
	if b, _ := v["blind_spot_data"].(bool); !b {
		e.unknown("MC-16", "blind_spot_data_unavailable", "blind-spot data unavailable: possibly missing callers are unknown, not zero", nil)
	} else {
		count := v["possibly_missing_callers"]
		text := fmt.Sprintf("%v unresolved call sites name a reached function: possibly missing callers, not results", count)
		if str(count) == "0" {
			text = "no unresolved call site names a reached function (name-matched blind spots; calls the indexer did not record are not covered)"
		}
		sites := e.listPage(listOf(v["possibly_missing_call_sites"]))
		labelTest(e, listOf(sites["items"]))
		e.unknown("MC-16", "possibly_missing_callers", text, map[string]any{"count": count, "call_sites": sites})
	}
	if c, ok := v["unknown_calls_in_result_files"].(json.Number); ok && c.String() != "0" {
		e.unknown("MC-16", "unknown_calls_in_result_files", c.String()+" unresolved call sites in the files of these results; none names a reached function", nil)
	}
}

// walkEnds maps the walk's "? not modelled" ends (MC-17) as the chain
// does (R14): one unknown per distinct end line, listing the ends among
// the target and this page's results, with the exact total of ends of
// that line; the other results' ends are on their pages.
func walkEnds(e *Envelope, ends []any, target string, shown []any) {
	onPage := map[string]bool{target: true}
	for _, r := range shown {
		if m, ok := r.(map[string]any); ok {
			onPage[str(m["id"])] = true
		}
	}
	var texts []string
	total := map[string]int{}
	nodes := map[string][]any{}
	for _, raw := range ends {
		m, _ := raw.(map[string]any)
		text := str(m["text"])
		if _, ok := total[text]; !ok {
			texts = append(texts, text)
			nodes[text] = []any{}
		}
		total[text]++
		id := str(m["node"])
		if !onPage[id] {
			continue
		}
		if r := str(m["repository"]); r != "" {
			nodes[text] = append(nodes[text], map[string]any{"repository": r, "node": id})
		} else {
			nodes[text] = append(nodes[text], id)
		}
	}
	for _, text := range texts {
		data := map[string]any{"nodes": nodes[text], "total": total[text]}
		if more := total[text] - len(nodes[text]); more > 0 {
			data["other_pages"] = more
		}
		e.unknown("MC-17", "not_modelled_end", text, data)
	}
}

// walkResult maps one callers or impact result.
func walkResult(e *Envelope, command string, r any, nodeAttrs map[string]map[string]any) map[string]any {
	m, _ := r.(map[string]any)
	node, _ := m["node"].(map[string]any)
	out := compactNode(e, node)
	out["distance"] = m["distance"]
	provenance := orDefault(str(m["provenance"]), "fact")
	out["provenance"] = provenance
	e.label("provenance", provenance)
	switch {
	case command == "callers":
		out["claim"] = "called by"
	case provenance == "fact":
		out["claim"] = "depends on"
	default:
		out["claim"] = "may depend on"
	}
	e.label("claim", str(out["claim"]))
	if provenance == "inferred" {
		out["confidence"] = m["confidence"]
		out["confidence_rule"] = "weakest_edge"
		e.label("confidence", "weakest_edge")
	}
	var via, assumed []any
	for _, st := range listOf(m["path"]) {
		step, _ := st.(map[string]any)
		vs := map[string]any{"edge": step["kind"], "source": step["source"], "target": step["target"], "provenance": step["provenance"], "confidence": step["confidence"]}
		if str(step["kind"]) == "calls_route" {
			// A link whose origin nothing proves to be the server (LF-1)
			// is shown with its assumption, never as a plain link.
			if texts := originAssumptions(nodeAttrs[str(step["source"])], str(step["target"])); len(texts) > 0 {
				vs["origin_assumptions"] = texts
				assumed = append(assumed, texts...)
				e.originAssumed(texts, map[string]string{"callers": "MC-14", "impact": "MC-16"}[command])
			}
			// A link of the unknown-prefix tier (or a Next.js route whose
			// basePath is not read) with its UP assumptions (UP-36).
			// The matched route path entry's assumptions (GR-V5, RO-V1,
			// RO-V2) as route_path_assumptions.
			for k, ids := range e.prefixAssumed(nodeAttrs[str(step["source"])], str(step["target"]), map[string]string{"callers": "MC-14", "impact": "MC-16"}[command]) {
				vs[k] = ids
			}
		}
		if ev := listOf(step["evidence"]); len(ev) > 0 {
			first, _ := ev[0].(map[string]any)
			vs["file"] = first["file"]
			if sp, ok := first["span"].(map[string]any); ok {
				vs["line"] = sp["start_line"]
			}
			e.omit("via.evidence")
		}
		for _, k := range sortedKeys(step) {
			switch k {
			case "kind", "source", "target", "provenance", "confidence", "evidence":
			case "type":
				e.omitHow("via.type", "the record type (always edge)")
			case "source_repository", "target_repository", "project_link", "assumption", "stale", "stale_reasons":
				vs[k] = step[k]
			default:
				e.notModelled(command+".results.path."+k, step[k])
			}
		}
		if pl, _ := step["project_link"].(bool); pl {
			tagRow("MC-18")
			e.Assumptions = appendAssumption(e.Assumptions, str(step["assumption"]))
			if st, _ := step["stale"].(bool); st {
				e.unknown("MC-18", "stale_link", "a project link on this path is stale", step["stale_reasons"])
			}
		}
		via = append(via, vs)
	}
	if via == nil {
		via = []any{}
	}
	out["via"] = via
	if assumed != nil {
		out["origin_assumptions"] = distinctAny(assumed)
	}
	for _, k := range []string{"assumptions", "stale"} {
		if val, ok := m[k]; ok {
			out[k] = val
		}
	}
	for _, k := range sortedKeys(m) {
		switch k {
		case "node", "distance", "provenance", "confidence", "path", "assumptions", "stale":
		default:
			e.notModelled(command+".results."+k, m[k])
		}
	}
	return out
}

// originAssumptions returns the origin assumptions (LF-1, T034) a call's
// stored attributes carry for its link to route: the stored
// origin_assumption of each path entry that names route among its
// candidates. When no entry names it (or route is empty), it is every
// assumption the call stores, as the server's caller reasons show them:
// an assumption is never dropped because it cannot be placed.
func originAssumptions(attrs map[string]any, route string) []any {
	var out []any
	if route != "" {
		for _, p := range listOf(attrs["paths"]) {
			pm, _ := p.(map[string]any)
			text := str(pm["origin_assumption"])
			if text == "" {
				continue
			}
			for _, c := range listOf(pm["candidates"]) {
				if str(c) == route {
					out = append(out, text)
					break
				}
			}
		}
	}
	if len(out) == 0 {
		for _, a := range listOf(attrs["origin_assumptions"]) {
			if text := str(a); text != "" {
				out = append(out, text)
			}
		}
	}
	return distinctAny(out)
}

// originAssumed adds each origin assumption to the envelope's assumptions
// (id origin_assumed), once per text, under row.
func (e *Envelope) originAssumed(texts []any, row string) {
	tagRow(row)
	for _, t := range texts {
		text := str(t)
		found := false
		for _, a := range e.Assumptions {
			found = found || (a.ID == "origin_assumed" && a.Text == text)
		}
		if !found {
			e.Assumptions = append(e.Assumptions, Assumption{ID: "origin_assumed", Text: text, Row: row})
		}
	}
}

// prefixAssumed returns the assumption ids a call's link to route rests
// on, as clients.LinkAssumptions reads them, by the key a result shows
// them under: the unknown-prefix ones (UP-V1, UP-V2; T033 row UP-36) as
// prefix_assumptions, the matched route path entry's (GR-V5, RO-V1,
// RO-V2) as route_path_assumptions. It adds each to the
// envelope's assumptions with its contract text under row; keys without
// an id are absent.
func (e *Envelope) prefixAssumed(attrs map[string]any, route, row string) map[string]any {
	out := map[string]any{}
	for _, a := range clients.LinkAssumptions(attrs, route) {
		if a.ID == clients.LinkAssumptionOrigin {
			continue
		}
		key := assumptionKey(a.ID)
		list, _ := out[key].([]any)
		out[key] = append(list, a.ID)
		e.addAssumption(Assumption{ID: a.ID, Text: a.Text, Row: row})
	}
	return out
}

// assumptionKey is the result key a link assumption id is listed under:
// route_path_assumptions for a path assumption of the matched route path
// entry, prefix_assumptions otherwise.
func assumptionKey(id string) string {
	if clients.PathAssumption(id) {
		return "route_path_assumptions"
	}
	return "prefix_assumptions"
}

// linkAssumed maps a view's stored link assumptions ({id, text}: an
// unproven origin, UP-V1, UP-V2, or a path assumption of the matched
// route path entry) into the keys a result shows them under,
// origin_assumptions (texts), prefix_assumptions and
// route_path_assumptions (ids), and adds each to the envelope's
// assumptions under row.
func (e *Envelope) linkAssumed(val any, row string) map[string]any {
	out := map[string]any{}
	var origins []any
	for _, raw := range listOf(val) {
		a, _ := raw.(map[string]any)
		id, text := str(a["id"]), str(a["text"])
		if id == clients.LinkAssumptionOrigin {
			origins = append(origins, text)
			continue
		}
		key := assumptionKey(id)
		list, _ := out[key].([]any)
		out[key] = append(list, id)
		e.addAssumption(Assumption{ID: id, Text: text, Row: row})
	}
	if len(origins) > 0 {
		out["origin_assumptions"] = origins
		e.originAssumed(origins, row)
	}
	return out
}

// addAssumption adds a to the envelope's assumptions once (by id and
// text).
func (e *Envelope) addAssumption(a Assumption) {
	tagRow(a.Row)
	for _, b := range e.Assumptions {
		if b.ID == a.ID && b.Text == a.Text {
			return
		}
	}
	e.Assumptions = append(e.Assumptions, a)
}

func appendAssumption(list []Assumption, text string) []Assumption {
	for _, a := range list {
		if a.Text == text {
			return list
		}
	}
	return append(list, Assumption{ID: "project_link", Text: text, Row: "MC-18"})
}

// walkBound maps one traverse bound with its limit and sentence (MC-3).
func walkBound(e *Envelope, b any, depth, maxDepth int) Bound {
	tagRow("MC-3")
	m, _ := b.(map[string]any)
	step, _ := strconv.Atoi(str(m["step"]))
	beyond, _ := strconv.Atoi(str(m["beyond"]))
	tb := traverse.Bound{Cap: str(m["cap"]), Step: step, Beyond: beyond}
	limit := map[string]int{traverse.CapDepth: depth, traverse.CapReached: traverse.MaxReached, traverse.CapFanOut: traverse.MaxFanOut}[tb.Cap]
	e.label("bound", tb.Cap)
	text := traverse.BoundText(tb)
	if tb.Cap == traverse.CapDepth {
		if depth < maxDepth {
			text += fmt.Sprintf("; call again with a larger depth (at most %d) to see them", maxDepth)
		} else {
			text += "; call again from a node at the last step to see them"
		}
	}
	return Bound{Cap: tb.Cap, Limit: limit, Step: &step, Beyond: &beyond, Text: text, Row: "MC-3"}
}

// sortResults keeps the reused order (distance, then repository and
// identity as the query sorted them); maps from JSON keep it already, so
// this only makes the order independent of map iteration.
func sortResults(results []any) {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i].(map[string]any), results[j].(map[string]any)
		da, _ := strconv.Atoi(str(a["distance"]))
		db, _ := strconv.Atoi(str(b["distance"]))
		return da < db
	})
}

// handlerOf maps the endpoint view's handler.
func handlerOf(e *Envelope, val any) map[string]any {
	h, _ := val.(map[string]any)
	out := map[string]any{"state": h["state"], "expression": h["expression"]}
	e.label("handler", str(h["state"]))
	if n, ok := h["node"].(map[string]any); ok {
		out["node"] = compactNode(e, n)
	} else {
		out["node"] = nil
	}
	if ed, ok := h["edge"].(map[string]any); ok {
		out["edge"] = ed
		e.label("provenance", str(ed["provenance"]))
	} else {
		out["edge"] = nil
	}
	for _, k := range sortedKeys(h) {
		switch k {
		case "state", "expression", "node", "edge":
		default:
			e.notModelled("handler."+k, h[k])
		}
	}
	return out
}

// callEntry maps a caller or near miss of the endpoint view: the call
// node compacted, its owner, stored edges (callers) or relation and
// candidate counts (near misses), and reasons, verbatim.
func callEntry(e *Envelope, c any, claim, route string) map[string]any {
	m, _ := c.(map[string]any)
	n, _ := m["node"].(map[string]any)
	out := compactNode(e, n)
	if claim != "" {
		out["claim"] = claim
		e.label("claim", claim)
		// A caller linked under an origin assumption (LF-1) carries it
		// here and in the envelope, never as a plain link.
		attrs, _ := n["attributes"].(map[string]any)
		if texts := originAssumptions(attrs, route); len(texts) > 0 {
			out["origin_assumptions"] = texts
			e.originAssumed(texts, "MC-12")
		}
		for k, ids := range e.prefixAssumed(attrs, route, "MC-12") {
			out[k] = ids
		}
	}
	for _, k := range sortedKeys(m) {
		val := m[k]
		switch k {
		case "node":
		case "owner", "reasons", "relation", "candidate_count", "candidate_count_exact", "project":
			out[k] = val
		case "edges":
			out[k] = val
			for _, ed := range listOf(val) {
				em, _ := ed.(map[string]any)
				e.label("provenance", str(em["provenance"]))
				if mt := str(em["match"]); mt != "" {
					e.label("match", mt)
				}
			}
		default:
			e.notModelled("call."+k, val)
		}
	}
	return out
}

// nodeKeys are the keys of a stored node as views and query serve it:
// kept (id, kind, names, file, line, file state, source root, repository)
// or dropped as presentation (MC-6). Any other key is MC-D.
var nodeKeys = map[string]bool{"type": false, "identity": true, "kind": true, "level": false, "name": true, "qualified_name": true,
	"file": true, "span": true, "content_hash": false, "attributes": false, "ide": false, "file_state": true, "source_root": true, "repository": true}

// compactNode keeps a node's facts and location and drops presentation:
// the editor link, the raw stored attributes (the view's own fields carry
// what it claims from them), the content hash, the record type and level.
func compactNode(e *Envelope, m map[string]any) map[string]any {
	out := map[string]any{}
	if m == nil {
		return out
	}
	for _, k := range sortedKeys(m) {
		val := m[k]
		keep, known := nodeKeys[k]
		switch {
		case !known:
			e.notModelled("node."+k, val)
		case !keep:
			e.omit("node." + k)
		case k == "identity":
			out["id"] = val
		case k == "span":
			if sp, ok := val.(map[string]any); ok {
				out["line"] = sp["start_line"]
				out["end_line"] = sp["end_line"]
			}
		case k == "file_state":
			out[k] = val
			e.fileState(str(m["file"]), str(val))
		default:
			out[k] = val
		}
	}
	return out
}

// labelPrecision records a {label,row,reason} precision label.
func labelPrecision(e *Envelope, val any) {
	if m, ok := val.(map[string]any); ok {
		e.label("precision", str(m["label"]))
	}
}

// labelShape records the state of every shape section.
func labelShape(e *Envelope, val any) {
	m, _ := val.(map[string]any)
	keys := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if sec, ok := m[k].(map[string]any); ok {
			if st, ok := sec["state"].(string); ok {
				e.label("shape", st)
				if st == "partial" {
					e.unknown("MC-12", "shape_"+st, k+": "+orDefault(str(sec["reason"]), legend["shape"][st]), nil)
				}
			}
		}
	}
}

func isIdentity(s string) bool { return query.IsIdentity(s) }

func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding query answer: %w", err)
	}
	return decode(raw)
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	}
	return fmt.Sprint(v)
}

func listOf(v any) []any {
	l, _ := v.([]any)
	return l
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func distinctAny(in []any) []any {
	seen := map[string]bool{}
	var out []any
	for _, v := range in {
		if !seen[str(v)] {
			seen[str(v)] = true
			out = append(out, v)
		}
	}
	return out
}

// sortedKeys orders a view object's keys, so unknowns and labels are
// added in the same order on every call (map order is random).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func intp(n int) *int { return &n }

// queryFreshFor is queryFresh in one-repository mode; a project's find
// reads a member, whose check the project view made without the file
// list, so the query scans that member itself.
func (s *Server) queryFreshFor(e *Envelope) *query.Freshness {
	if s.opt.Project != nil {
		return nil
	}
	return queryFresh(e)
}

// routeForm reports whether target is spelled "METHOD /path": an upper-case
// word, one space and a path.
func routeForm(target string) bool {
	method, path, ok := strings.Cut(target, " ")
	if !ok || method == "" || !strings.HasPrefix(path, "/") {
		return false
	}
	return strings.ToUpper(method) == method && strings.IndexFunc(method, func(r rune) bool { return r < 'A' || r > 'Z' }) < 0
}

// specOperationNote is R13's note (MC-7) for callers, impact and chain:
// a spec operation is the spec's description of a route, not the route,
// so a walk from it answers for the operation node.
func specOperationNote(e *Envelope, kind string) {
	if kind == "api_operation" {
		e.unknown("MC-7", "target_is_spec_operation", "the target is an OpenAPI operation (the spec's description of a route), not the route: the answer is for the operation node; the endpoint tool lists the route it documents", nil)
	}
}

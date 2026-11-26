package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// unresolvedArgs are the unresolved tool's arguments.
type unresolvedArgs struct {
	Transport  string `json:"transport,omitempty" jsonschema:"http (default), tauri_ipc or server_action"`
	Status     string `json:"status,omitempty" jsonschema:"comma-separated call statuses (default: every status but linked that the transport's summary counts)"`
	Scope      string `json:"scope,omitempty" jsonschema:"production (default), test or all"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"entries per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
}

// unresolvedTool answers what Graphvein could not link (rows MC-26 and
// MC-27): the calls of a transport whose status is not linked, each with
// its reasons (/api/v0/api/calls), then the endpoints the API view counts
// with caller_count 0, each worded "no stored caller" and only that. The
// two lists are one paged list; the summary has the view's counts.
func unresolvedTool(ctx context.Context, s *Server, in unresolvedArgs, e *Envelope) error {
	tagRow("MC-26")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	transport, scope := orDefault(in.Transport, "http"), orDefault(in.Scope, "production")
	tagRow("MC-2")
	e.Request["transport"], e.Request["scope"], e.Request["limit"], e.Request["cursor"] = transport, scope, n, in.Cursor
	q := url.Values{"transport": {transport}, "scope": {scope}}
	if err := s.memberParam(e, q, in.Repository); err != nil {
		return err
	}
	raw, err := s.reader.Read(ctx, "/api/v0/api/endpoints", q)
	if err != nil {
		return err
	}
	v, err := decode(raw)
	if err != nil {
		return err
	}
	ts := listOf(v["transports"])
	if len(ts) != 1 {
		return fmt.Errorf("endpoints view answered %d transports for %s", len(ts), transport)
	}
	t, _ := ts[0].(map[string]any)
	calls, _ := t["calls"].(map[string]any)
	byStatus, _ := calls["by_status"].(map[string]any)
	statuses := splitList(in.Status)
	if len(statuses) == 0 {
		for _, st := range sortedKeys(byStatus) {
			if st != "linked" {
				statuses = append(statuses, st)
			}
		}
	}
	e.Request["status"] = strings.Join(statuses, ",")
	summary := map[string]any{"transport": transport, "calls": calls}
	for _, k := range []string{"endpoint_count", "never_called", "never_called_in_project", "possibly_called_in_project", "precision", "callers_precision"} {
		if val, ok := t[k]; ok {
			summary[k] = val
		}
	}
	labelPrecision(e, t["precision"])
	labelPrecision(e, t["callers_precision"])
	e.Summary = summary
	var entries []any
	if len(statuses) > 0 {
		cq := url.Values{"status": {strings.Join(statuses, ",")}}
		for k, val := range q {
			cq[k] = val
		}
		raw, err := s.reader.Read(ctx, "/api/v0/api/calls", cq)
		if err != nil {
			return err
		}
		cv, err := decode(raw)
		if err != nil {
			return err
		}
		for _, c := range listOf(cv["calls"]) {
			entries = append(entries, unlinkedCall(e, c))
		}
		for _, k := range sortedKeys(cv) {
			switch k {
			case "api_version", "repository", "index", "scope", "transport", "statuses", "root", "total", "calls", "status_scope":
			case "ide":
				e.omit(k)
			default:
				e.notModelled("calls."+k, cv[k])
			}
		}
	}
	tagRow("MC-27")
	for _, r := range listOf(t["endpoints"]) {
		row, _ := r.(map[string]any)
		if entry := noCallerEndpoint(e, row); entry != nil {
			entries = append(entries, entry)
		}
	}
	if entries == nil {
		entries = []any{}
	}
	e.Results = page(e, entries, offset, n)
	e.unknown("MC-26", "recognised_calls_only", "only calls the extractors recognise are listed or counted: a request made another way (an unknown client library, a computed method) is in no list", nil)
	e.unknown("MC-27", "no_stored_caller_not_proof", "an endpoint with no stored caller may still be called: by unlinked calls above, from outside the repository, or in a way no extractor reads", nil)
	return nil
}

// unlinkedCall maps one call of the calls view: location and owner, its
// status, method and URL expression as stored, its paths' candidates and
// the view's reasons. It is never a link (MC-26).
func unlinkedCall(e *Envelope, raw any) map[string]any {
	m, _ := raw.(map[string]any)
	node, _ := m["node"].(map[string]any)
	out := compactNode(e, node)
	out["entry"] = "unlinked_call"
	attrs, _ := node["attributes"].(map[string]any)
	status := str(attrs["status"])
	out["status"] = status
	e.label("call_status", status)
	// The call's stored attributes are its facts (R7): every known key
	// is kept (a null value says nothing and is left out), any other is
	// MC-D.
	for k, val := range e.mapKeys("calls.attributes", attrs, callAttributeKeys, callAttributeOmitted) {
		if val != nil && k != "status" && k != "paths" {
			out[k] = val
		}
	}
	// An alternative linked under an origin assumption (LF-1) keeps it,
	// in the call's fields and in the envelope.
	if texts := originAssumptions(attrs, ""); len(texts) > 0 {
		e.originAssumed(texts, "MC-26")
	}
	var paths []any
	for _, p := range listOf(attrs["paths"]) {
		pm, _ := p.(map[string]any)
		paths = append(paths, e.mapKeys("calls.paths", pm, callPathKeys, nil))
	}
	if paths != nil {
		out["paths"] = paths
	}
	// The call's own name repeats in its qualified name and the owner's
	// identity says all its kind and name do.
	for _, k := range []string{"end_line", "source_root", "file_state"} {
		if _, ok := out[k]; ok && (k != "file_state" || out[k] == "unchanged") {
			delete(out, k)
			e.omitHow("calls end_line, source_root and an unchanged file_state", "the find tool with the call's identity")
		}
	}
	if str(out["qualified_name"]) != "" && strings.HasSuffix(str(out["qualified_name"]), str(out["name"])) {
		delete(out, "qualified_name")
		e.omitHow("calls.qualified_name (owner name and call name)", "it is the owner's name and the call's name")
	}
	for _, k := range sortedKeys(m) {
		switch k {
		case "node":
		case "owner":
			if om, ok := m[k].(map[string]any); ok {
				out[k] = om["id"]
				e.omitHow("calls.owner kind and qualified name", "both are in the owner's identity")
			} else {
				out[k] = m[k]
			}
		case "reasons", "targets", "project":
			out[k] = m[k]
		default:
			e.notModelled("call."+k, m[k])
		}
	}
	return out
}

// noCallerEndpoint is an endpoint row the API view counts with
// caller_count 0 (MC-27): "no stored caller" in this scope, never "dead"
// or "not called". A row with an unread use is "may be called"; in
// project mode a row with project callers is left out and one with
// possible project callers is "may be called from another member".
func noCallerEndpoint(e *Envelope, row map[string]any) map[string]any {
	cc, ok := row["caller_count"].(json.Number)
	if !ok || cc.String() != "0" {
		return nil
	}
	if pc, ok := row["project_caller_count"].(json.Number); ok && pc.String() != "0" {
		return nil
	}
	out := map[string]any{"entry": "endpoint_no_stored_caller"}
	for k, val := range e.mapKeys("endpoints_no_caller", row, noCallerKeys, noCallerOmitted) {
		if val != nil {
			out[k] = val
		}
	}
	claim := "no stored caller"
	if row["prefix_unknown"] != nil {
		e.label("path", "prefix_unknown")
	}
	for _, f := range listOf(row["flags"]) {
		e.label("flag", str(f))
		if str(f) == "unread_use" {
			claim = "may be called (an unread use may reach it)"
		}
	}
	if pp, ok := row["project_possible_caller_count"].(json.Number); ok && pp.String() != "0" {
		claim = "may be called from another member (ambiguous or incomplete project link outcome)"
		tagRow("MC-18")
	}
	out["claim"] = claim
	e.label("claim", "no stored caller")
	return out
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// The unresolved tool's known view keys (R7): kept, or left out with how;
// any other key is MC-D.
var (
	callAttributeKeys = keySet("status", "method", "url_expression", "library", "wrapper", "command", "action", "base", "base_expression",
		"method_expression", "conditional", "alternatives", "candidates", "form", "framework", "transport", "scope", "plugin", "plugin_command",
		"operation", "hook", "boundary", "paths", "helpers", "helper_count", "assumes", "origin", "origin_resolution", "path_template",
		"trailing_slash", "match", "candidate_count", "security", "project_link", "nextjs_origin", "nextjs_rewrite", "nextjs_rewrite_destination",
		"nextjs_rewrite_phase", "generated_output", "origin_assumptions")
	// callAttributeOmitted are the call facts left out for size, with
	// where they are served.
	callAttributeOmitted = map[string]string{
		"sends": "the keys and shape the call sends (T029, compared with a linked route's shape): GET /api/v0/api/calls (graphvein serve)",
	}
	callPathKeys = keySet("template", "resolution", "status", "candidates", "candidate_count", "conditional", "base", "match", "origin",
		"trailing_slash", "origin_resolution", "nextjs_origin", "nextjs_rewrite", "nextjs_rewrite_destination", "nextjs_rewrite_phase", "origin_assumption", "assumes", "route_prefix")
	noCallerKeys    = keySet("id", "name", "method", "path", "prefix_unknown", "served_paths", "command", "action", "file", "line", "flags", "project_possible_caller_count")
	noCallerOmitted = map[string]string{
		"caller_count":         "always 0 here (that is why the row is listed)",
		"project_caller_count": "always 0 here (that is why the row is listed)",
		"path_count":           "the endpoints tool lists the row with detail full",
		"pattern":              "the endpoints tool lists the row with detail full",
		"framework":            "the endpoints tool lists the row with detail full",
		"scope":                "the endpoints tool lists the row with detail full",
		"documented":           "the endpoints tool lists the row with detail full",
		"shape":                "the endpoint tool shows the route's shape",
	}
)

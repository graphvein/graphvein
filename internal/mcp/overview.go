package mcp

import (
	"context"
	"fmt"
	"net/url"
)

// overviewArgs are the overview tool's arguments.
type overviewArgs struct {
	Scope      string `json:"scope,omitempty" jsonschema:"production (default) or all: which components the architecture view counts"`
	Repository string `json:"repository,omitempty" jsonschema:"project mode: the member (required when the project has several)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"components per page (default 50, at most 500)"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of a previous page"`
	unknownsArgs
}

// overviewTool answers what the repository is made of (row MC-28): the
// architecture view's components (paged) and links summarized by family
// and weakest tier with their cell states, the databases, the schema
// summary, and the precision view's rules and file labels as counts.
// Unread components, unknown cells and everything the views say is not
// proven are unknowns.
func overviewTool(ctx context.Context, s *Server, in overviewArgs, e *Envelope) error {
	tagRow("MC-28")
	if err := requireIndexed(s, e); err != nil {
		return err
	}
	offset, n, err := pageParams(in.Limit, in.Cursor)
	if err != nil {
		return err
	}
	scope := orDefault(in.Scope, "production")
	tagRow("MC-2")
	e.Request["scope"], e.Request["limit"], e.Request["cursor"] = scope, n, in.Cursor
	if err := e.unknownsPaging(in.unknownsArgs); err != nil {
		return err
	}
	q := url.Values{}
	if err := s.memberParam(e, q, in.Repository); err != nil {
		return err
	}
	aq := url.Values{"scope": {scope}}
	for k, v := range q {
		aq[k] = v
	}
	read := func(path string, q url.Values) (map[string]any, error) {
		raw, err := s.reader.Read(ctx, path, q)
		if err != nil {
			return nil, err
		}
		return decode(raw)
	}
	arch, err := read("/api/v0/architecture", aq)
	if err != nil {
		return err
	}
	schema, err := read("/api/v0/schema/summary", q)
	if err != nil {
		return err
	}
	prec, err := read("/api/v0/precision", q)
	if err != nil {
		return err
	}
	summary := map[string]any{}
	var components []any
	for _, k := range sortedKeys(arch) {
		val := arch[k]
		switch k {
		case "api_version", "repository", "index", "scope":
		case "manifests", "notice", "hidden", "not_drawn", "libraries", "documents":
			summary[k] = val
		case "project":
			// serve --project: the other members as cards, with the link
			// pass and the assumptions their links rest on (MC-18).
			tagRow("MC-18")
			summary[k] = val
			pm, _ := val.(map[string]any)
			for _, a := range listOf(pm["assumptions"]) {
				e.Assumptions = appendAssumption(e.Assumptions, str(a))
			}
			if lp, ok := pm["link_pass"].(map[string]any); ok {
				e.label("link_pass", str(lp["state"]))
				if len(listOf(lp["stale_reasons"])) > 0 {
					e.unknown("MC-18", "link_pass_stale", "the link pass is stale: project links may differ if linked now", lp["stale_reasons"])
				}
			}
		case "components":
			for _, c := range listOf(val) {
				components = append(components, componentOf(e, c))
			}
		case "links":
			summary[k] = linksSummary(e, val)
		case "databases":
			var dbs []any
			for _, d := range listOf(val) {
				m, _ := d.(map[string]any)
				db := e.mapKeys("databases", m, databaseKeys, map[string]string{"evidence": "the facts behind each engine: GET /api/v0/architecture (graphvein serve)"})
				cellUnknown(e, str(m["id"])+" engine", m["engine"])
				dbs = append(dbs, db)
			}
			if dbs == nil {
				dbs = []any{}
			}
			summary[k] = dbs
		case "stubs":
			// One unknown for all stubs: each names a component and the
			// external packages it imports, whose protocol is not modelled.
			if stubs := listOf(val); len(stubs) > 0 {
				e.unknown("MC-28", "external_stubs", fmt.Sprintf("%d components import external packages whose protocol is not modelled (listed in data)", len(stubs)), e.listPage(stubs))
			}
		case "ghosts":
			for _, g := range listOf(val) {
				m, _ := g.(map[string]any)
				e.unknown("MC-28", "unlinked_calls", fmt.Sprintf("%s: %v %s calls not linked (by status; the unresolved tool lists them)", str(m["component"]), m["total"], str(m["family"])), m["by_status"])
			}
		case "not_proven":
			// One unknown per kind, each entry with its own text.
			var kinds []string
			byKind := map[string][]any{}
			for _, np := range listOf(val) {
				m, _ := np.(map[string]any)
				k := str(m["kind"])
				if byKind[k] == nil {
					kinds = append(kinds, k)
				}
				entry := map[string]any{}
				for ek, ev := range m {
					if ek != "kind" {
						entry[ek] = ev
					}
				}
				byKind[k] = append(byKind[k], entry)
			}
			for _, k := range kinds {
				list := byKind[k]
				text := str(list[0].(map[string]any)["text"])
				if len(list) > 1 {
					text = fmt.Sprintf("%d not proven (%s), each with its text in data", len(list), k)
				}
				e.unknown("MC-28", "not_proven_"+k, text, e.listPage(list))
			}
		default:
			e.notModelled("architecture."+k, val)
		}
	}
	schemaOut := map[string]any{}
	for _, k := range sortedKeys(schema) {
		switch k {
		case "api_version", "repository", "index":
		default:
			schemaOut[k] = schema[k]
		}
	}
	if str(schema["found"]) != "sets" {
		e.unknown("MC-28", "schema_"+str(schema["found"]), "schema: "+str(schema["found"])+" ("+orDefault(str(schema["unknown_reason"]), "no reason given")+")", nil)
	}
	for _, k := range []string{"failed_sets", "unreadable_manifests", "scan_gaps"} {
		if c, _ := asInt(schema[k]); c > 0 {
			e.unknown("MC-28", "schema_"+k, fmt.Sprintf("schema: %d %s: tables there may be missing", c, k), nil)
		}
	}
	if len(listOf(schema["unread_tools"])) > 0 {
		e.unknown("MC-28", "schema_unread_tools", "schema: these tools' sources are not read", schema["unread_tools"])
	}
	// T070: model sources a linker found and refused as a whole.
	if len(listOf(schema["refused_sources"])) > 0 {
		e.unknown("MC-28", "schema_refused_sources", "schema: these model sources were found and not read (the message gives the reason)", schema["refused_sources"])
	}
	summary["schema"] = schemaOut
	summary["precision"] = precisionSummary(e, prec)
	e.Summary = summary
	if components == nil {
		components = []any{}
	}
	e.Results = page(e, components, offset, n)
	return nil
}

// componentOf is one component: identity, state and reason, kinds,
// frameworks, counts and manifests by file. An unread or unassigned
// component is an unknown.
func componentOf(e *Envelope, c any) map[string]any {
	m, _ := c.(map[string]any)
	out := e.mapKeys("components", m, componentKeys, nil)
	var manifests []any
	for _, mf := range listOf(m["manifests"]) {
		mm, _ := mf.(map[string]any)
		manifests = append(manifests, e.mapKeys("components.manifests", mm, manifestKeys, nil))
	}
	if manifests == nil {
		manifests = []any{}
	}
	out["manifests"] = manifests
	e.label("component", str(m["state"]))
	if st := str(m["state"]); st != "read" {
		e.unknown("MC-28", "component_"+st, "component "+str(m["id"])+" is "+st+": "+str(m["reason"]), nil)
	}
	return out
}

// linksSummary counts the architecture links by family and weakest tier
// and lists each link with its count, weakest tier and cell states (no
// member lists); unknown cells are unknowns.
func linksSummary(e *Envelope, val any) map[string]any {
	byFamily := map[string]int{}
	var links []any
	for _, l := range listOf(val) {
		m, _ := l.(map[string]any)
		byFamily[str(m["family"])]++
		cells := map[string]any{}
		if cm, ok := m["cells"].(map[string]any); ok {
			for _, ck := range sortedKeys(cm) {
				cell, _ := cm[ck].(map[string]any)
				c := e.mapKeys("links.cells", cell, cellKeys, cellOmitted)
				cells[ck] = c
				e.label("cell", str(cell["state"]))
				if str(cell["state"]) == "unknown" {
					tagRow("MC-28")
				}
			}
		}
		e.label("tier", tierFamily(str(m["weakest"])))
		link := e.mapKeys("links", m, linkKeys, linkOmitted)
		link["cells"] = cells
		links = append(links, link)
	}
	if links == nil {
		links = []any{}
	}
	return map[string]any{"count": len(links), "by_family": byFamily, "links": links,
		"cells_note": "a cell state unknown means the property is not proven either way"}
}

// cellUnknown adds an unknown for a cell whose state is unknown.
func cellUnknown(e *Envelope, what string, val any) {
	cell, _ := val.(map[string]any)
	if str(cell["state"]) == "" {
		return
	}
	e.label("cell", str(cell["state"]))
	if str(cell["state"]) == "unknown" {
		e.unknown("MC-28", "unknown_cell", what+": "+str(cell["source"]), nil)
	}
}

// precisionSummary is the precision view as counts: the run (extractor
// match, refusal), each rule's id, label, scope and fact total, the
// default row's families, and file labels by language.
func precisionSummary(e *Envelope, p map[string]any) map[string]any {
	out := map[string]any{}
	run, _ := p["run"].(map[string]any)
	out["run"] = run
	if run != nil && run["refusal"] != nil {
		e.unknown("MC-28", "precision_refusal", "the precision view refuses precise labels for this run", run["refusal"])
	}
	var rules []any
	for _, r := range listOf(p["rules"]) {
		m, _ := r.(map[string]any)
		rule := e.mapKeys("precision.rules", m, ruleKeys, ruleOmitted)
		rule["facts"] = factCounts(e, "precision.rules.facts", m["facts"])
		rules = append(rules, rule)
		e.label("precision", str(m["label"]))
	}
	if rules == nil {
		rules = []any{}
	}
	out["rules"] = rules
	if d, ok := p["default"].(map[string]any); ok {
		var fams []any
		for _, f := range listOf(d["families"]) {
			m, _ := f.(map[string]any)
			fam := e.mapKeys("precision.default.families", m, keySet("family", "label", "languages"), map[string]string{"facts": ""})
			fam["facts"] = factCounts(e, "precision.default.families.facts", m["facts"])
			fams = append(fams, fam)
		}
		if fams == nil {
			fams = []any{}
		}
		def := e.mapKeys("precision.default", d, keySet("row", "label", "scope"), map[string]string{"covers": precisionHow, "families": ""})
		def["families"] = fams
		out["default"] = def
		e.label("precision", str(d["label"]))
	}
	for _, k := range sortedKeys(p) {
		switch k {
		case "api_version", "repository", "index", "run", "rules", "default":
		case "binary_extractor":
			e.omitHow("precision.binary_extractor", "this binary's extractor version: freshness.index.extractor_current")
		case "files":
			out[k] = p[k]
		default:
			e.notModelled("precision."+k, p[k])
		}
	}
	return out
}

// precisionHow is where the precision view's left-out texts are.
const precisionHow = "GET /api/v0/precision (graphvein serve) or docs/extract/precision.md"

// factCounts is a precision fact count (total and unit); the counts by
// language are left out (MC-6).
func factCounts(e *Envelope, what string, v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	return e.mapKeys(what, m, keySet("total", "unit"), map[string]string{"by_language": "fact counts by language: " + precisionHow})
}

// The overview's known view keys (R7): kept, or left out with how; any
// other key is MC-D.
var (
	componentKeys = keySet("id", "directory", "name", "state", "reason", "kinds", "frameworks", "visibility", "hidden_by", "rows", "markers", "ingress", "counts", "manifests")
	manifestKeys  = keySet("file", "package", "ecosystem", "name", "unread")
	databaseKeys  = keySet("id", "set", "tool", "tables", "engine", "accessors")
	linkKeys      = keySet("id", "family", "row", "source", "target", "count", "back_count", "by_kind", "tiers", "weakest", "cells", "repository", "assumptions", "assumes", "stale_reasons")
	linkOmitted   = map[string]string{
		"members": "each link's members and evidence: call chain with a component, or GET /api/v0/architecture (graphvein serve)",
		"methods": "each link's HTTP methods: call chain with a component, or GET /api/v0/architecture (graphvein serve)",
		"origins": "each link's origins: call chain with a component, or GET /api/v0/architecture (graphvein serve)",
	}
	cellKeys    = keySet("value", "state", "counts")
	cellOmitted = map[string]string{
		"source": "the fact behind each cell state: GET /api/v0/architecture (graphvein serve)",
		"rows":   "the rows behind each cell state: GET /api/v0/architecture (graphvein serve)",
	}
	ruleKeys    = keySet("id", "kind", "scope", "label", "reason", "languages")
	ruleOmitted = map[string]string{"marker": precisionHow, "match": precisionHow, "refusal": precisionHow, "evidence": precisionHow, "facts": ""}
)

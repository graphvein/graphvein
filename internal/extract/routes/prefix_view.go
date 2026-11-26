package routes

import (
	"encoding/json"
	"strings"
)

// Reading the stored unknown-prefix paths for display (T033, rows
// UP-29..UP-33 and UP-36 of docs/contract/claims/unknown-prefix-paths.md).
// Every view that shows a route's path reads it through these functions, so
// a path whose prefix the code does not fix is never shown, sorted or
// counted as a full path.

// NextBasePathUnresolved is the prefix reason of a Next.js route whose
// project's basePath is not read as a literal (row NX-R4: its paths entry
// has resolution "unresolved" and holds the template without the base).
const NextBasePathUnresolved = "nextjs_base_path_unresolved"

// PrefixReasonText words each prefix reason for a reader, by code.
var PrefixReasonText = map[string]string{
	"route_prefix_unresolved": "a prefix on the way to the route is not a constant (for example read from configuration)",
	"route_mount_unresolved":  "the router is reached through a mount Graphvein does not prove",
	NextBasePathUnresolved:    "the Next.js basePath is not read as a literal",
}

// PrefixUnknownPath is one path of a route whose prefix the code does not
// fix, as the views show it: Path is the display form, the known part
// above the unknown prefix, "…" for the unknown part and the proven
// template below it (dozzle: "…/api/hosts/{host}/logs/stream"). It is
// never a full path: no view may sort, filter, place or try it as one.
type PrefixUnknownPath struct {
	Path          string  `json:"path"`
	Above         string  `json:"above,omitempty"`
	Template      string  `json:"template"`
	TrailingSlash bool    `json:"trailing_slash,omitempty"`
	Scope         string  `json:"scope"`
	Reason        string  `json:"reason"`
	Hop           *string `json:"hop"`
}

// FullPathEntry reports whether a stored "paths" entry is a full composed
// path. A Next.js entry whose basePath is unresolved (row NX-R4) holds the
// template without the deployment's base, so it is not.
func FullPathEntry(entry map[string]any) bool {
	return entry["resolution"] != "unresolved"
}

// PrefixDisplay is the display form of an unknown-prefix path: above, then
// "…" for the unknown part, then the template, with the served trailing
// slash restored as servedPath does for full paths.
func PrefixDisplay(above, template string, trailingSlash bool) string {
	if trailingSlash && template != "/" {
		template += "/"
	}
	if above == "" {
		return "…" + template
	}
	return strings.TrimSuffix(above, "/") + "/…" + template
}

// PrefixUnknownPaths lists a route's paths whose prefix is unknown, from
// its stored attributes, in stored order and without repeats: the entries
// of unresolved_prefix_paths (rows UP-1..UP-8), then the "paths" entries a
// Next.js unresolved basePath leaves without their base (row NX-R4). Empty
// when the route has none.
func PrefixUnknownPaths(attrs map[string]any) []PrefixUnknownPath {
	out := []PrefixUnknownPath{}
	seen := map[PrefixUnknownPath]bool{}
	add := func(p PrefixUnknownPath) {
		key := p
		key.Hop = nil
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	for _, e := range storedEntries(attrs[UnknownPrefixAttribute]) {
		template, ok := e["template"].(string)
		if !ok {
			continue
		}
		above, _ := e["above"].(string)
		slash := e["trailing_slash"] == true
		p := PrefixUnknownPath{Path: PrefixDisplay(above, template, slash), Above: above, Template: template, TrailingSlash: slash, Scope: entryScope(e)}
		p.Reason, _ = e["prefix_reason"].(string)
		if hop, ok := e["hop"].(string); ok {
			p.Hop = &hop
		}
		add(p)
	}
	for _, e := range storedEntries(attrs["paths"]) {
		template, ok := e["template"].(string)
		if !ok || FullPathEntry(e) {
			continue
		}
		slash := e["trailing_slash"] == true
		add(PrefixUnknownPath{Path: PrefixDisplay("", template, slash), Template: template, TrailingSlash: slash, Scope: entryScope(e), Reason: NextBasePathUnresolved})
	}
	return out
}

// ShownPrefixPath picks the unknown-prefix path a view shows for a route
// in a scope, as the views pick a full path: the first entry in scope
// ("test" or not), else the first; nil when there is none.
func ShownPrefixPath(list []PrefixUnknownPath, scope string) *PrefixUnknownPath {
	if len(list) == 0 {
		return nil
	}
	for i := range list {
		if (scope == "test") == (list[i].Scope == "test") {
			return &list[i]
		}
	}
	return &list[0]
}

// entryScope is a stored entry's scope: "test" when test code mounts it,
// else "production".
func entryScope(e map[string]any) string {
	if e["scope"] == "test" {
		return "test"
	}
	return "production"
}

// storedEntries reads a stored list of objects, typed (as an extractor
// builds it) or decoded from JSON (as a store returns it).
func storedEntries(v any) []map[string]any {
	if v == nil {
		return nil
	}
	if list, ok := v.([]any); ok {
		out := make([]map[string]any, 0, len(list))
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	if list, ok := v.([]map[string]any); ok {
		return list
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

// PathAssumptionText words each path assumption a full path entry can
// rest on, by id, for the views and the MCP answers: GR-V5
// (Go, docs/extract/routes.md "Go claim contract"), RO-V1 and RO-V2
// (route order, docs/extract/routes.md "FastAPI" and "Composition and
// diagnostics").
var PathAssumptionText = map[string]string{
	"GR-V5": "the path is served through a wrapper not proven to keep it: assumes the wrapper passes the request on unchanged",
	"RO-V1": "a mount at an unknown prefix may answer first: assumes the mount added before the route, whose prefix the code does not read, does not take this path",
	"RO-V2": "a mounted app's catch-all may answer first: assumes the app or router mounted earlier below this path's prefix passes the request on",
}

// PathAssumption is one full path of a route that rests on assumptions,
// as the views and the MCP answers show it: the served path, its scope,
// the assumption ids and what they name (the wrappers of GR-V5, the
// earlier mounts of RO-V1 and RO-V2).
type PathAssumption struct {
	Path           string   `json:"path"`
	Scope          string   `json:"scope"`
	Assumptions    []string `json:"assumptions"`
	ServedThrough  []string `json:"served_through,omitempty"`
	MayAnswerFirst []string `json:"may_answer_first,omitempty"`
}

// PathAssumptions lists a route's full paths that rest on an assumption,
// in stored order; empty when none does.
func PathAssumptions(attrs map[string]any) []PathAssumption {
	out := []PathAssumption{}
	for _, e := range storedEntries(attrs["paths"]) {
		template, ok := e["template"].(string)
		if !ok || !FullPathEntry(e) {
			continue
		}
		ids := storedStrings(e["assumptions"])
		if len(ids) == 0 {
			continue
		}
		path := template
		if e["trailing_slash"] == true && path != "/" {
			path += "/"
		}
		out = append(out, PathAssumption{Path: path, Scope: entryScope(e), Assumptions: ids, ServedThrough: storedStrings(e["served_through"]), MayAnswerFirst: storedStrings(e["may_answer_first"])})
	}
	return out
}

// storedStrings reads a stored list of strings, typed or decoded.
func storedStrings(v any) []string {
	switch list := v.(type) {
	case []string:
		return append([]string(nil), list...)
	case []any:
		var out []string
		for _, x := range list {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

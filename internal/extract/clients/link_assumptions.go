package clients

import (
	"encoding/json"

	"github.com/graphvein/graphvein/internal/extract/routes"
)

// LinkAssumption is one assumption a stored calls_route link rests on, as
// the views show it beside the link (T033): an origin nothing
// proves to be the target's server (row LF-1, ID "origin"), a row
// assumption of the unknown-prefix tier (UP-V1, UP-V2; ID the row id), or
// a path assumption of the route path entry the link matched (GR-V5,
// RO-V1, RO-V2).
type LinkAssumption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// LinkAssumptionOrigin is the ID of an origin assumption (row LF-1).
const LinkAssumptionOrigin = "origin"

// LinkAssumptions reads, from a client call's stored attributes, the
// assumptions its link to route rests on: those of each path entry that
// links route (its origin_assumption and the UP-V and path assumption ids
// among its assumes), origin assumptions first, each once. When no entry
// names route (or route is empty), it is every assumption the call stores
// (origin_assumptions, then the UP-V and path assumption ids of assumes),
// as the caller reasons show them: an assumption is never dropped because
// it cannot be placed. Other ids (NX-V1, NX-V3) are not link assumptions
// here.
func LinkAssumptions(attrs map[string]any, route string) []LinkAssumption {
	var origins, prefixes []LinkAssumption
	seen := map[LinkAssumption]bool{}
	add := func(list *[]LinkAssumption, a LinkAssumption) {
		if a.Text != "" && !seen[a] {
			seen[a] = true
			*list = append(*list, a)
		}
	}
	if route != "" {
		for _, p := range objectList(attrs["paths"]) {
			if p["status"] != "linked" || !containsString(stringValues(p["candidates"]), route) {
				continue
			}
			if text, ok := p["origin_assumption"].(string); ok {
				add(&origins, LinkAssumption{LinkAssumptionOrigin, text})
			}
			for _, id := range stringValues(p["assumes"]) {
				add(&prefixes, LinkAssumption{id, AssumptionText(id)})
			}
		}
	}
	if len(origins) == 0 && len(prefixes) == 0 {
		for _, text := range stringValues(attrs["origin_assumptions"]) {
			add(&origins, LinkAssumption{LinkAssumptionOrigin, text})
		}
		for _, id := range stringValues(attrs["assumes"]) {
			add(&prefixes, LinkAssumption{id, AssumptionText(id)})
		}
	}
	return append(origins, prefixes...)
}

// AssumptionText is the shown text of a link assumption id: an
// unknown-prefix one (UnknownPrefixAssumptions) or a path assumption of
// the matched route path entry (routes.PathAssumptionText); "" for any
// other id.
func AssumptionText(id string) string {
	if text, ok := UnknownPrefixAssumptions[id]; ok {
		return text
	}
	return routes.PathAssumptionText[id]
}

// PathAssumption reports a path assumption id of a route path entry
// (GR-V5, RO-V1, RO-V2) that a link carries in its assumes.
func PathAssumption(id string) bool {
	_, ok := routes.PathAssumptionText[id]
	return ok
}

// objectList reads a stored list of objects, typed or decoded from JSON.
func objectList(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		return list
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, x := range list {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case nil:
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out []map[string]any
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

// stringValues reads a stored list of strings, typed or decoded from JSON.
func stringValues(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, x := range list {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

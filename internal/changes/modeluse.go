package changes

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
)

// Model-type impact (relation I16; rows MT-13..MT-19 of
// docs/contract/claims/route-model-types.md). The indexer stores a
// route's model uses on the route node (attribute model_uses,
// routeshape.ModelUsesKey) and one uses_model edge per route and
// declaration (routeshape.UsesModelEdge); an I16 step carries the route's
// uses of the changed declaration so a reader can tell a request body
// from a response and a fact from an intended, returned or mapped type.

// modelUsesKey is routeshape.ModelUsesKey (TestModelUseNames keeps them
// equal; this package reads stored facts and does not import extractors).
const modelUsesKey = "model_uses"

// ModelStep is how the route of an I16 step uses the model declaration.
type ModelStep struct {
	// Name is the declaration's name.
	Name string `json:"name"`
	// Uses are the route's uses of the declaration, one per (section,
	// status, kind). Empty when the route node stores none (a removed
	// route read from the change set without the attribute): the step
	// then claims only the edge.
	Uses []ModelUseLabel `json:"uses"`
}

// ModelUseLabel is one use: the shape section of the using item, its
// response status, and its provenance kind (fact, intended, returned or
// mapped; row MT-9) and rule, file and line.
type ModelUseLabel struct {
	Section string  `json:"section"`
	Status  *string `json:"status"`
	Kind    string  `json:"kind"`
	Rule    string  `json:"rule"`
	File    string  `json:"file"`
	Line    int     `json:"line"`
	// Confidence is 1 for a use read from code and the link's for a
	// mapped use (row MT-10).
	Confidence float64 `json:"confidence"`
}

// modelRouteNodes reads the route nodes of the current candidates' I16
// edges, for their model_uses attribute.
func (t *traversal) modelRouteNodes(cands []candidate, graph string) (map[string]*contract.Node, error) {
	seen := map[string]bool{}
	var ids []string
	for _, cd := range cands {
		if cd.f.ID == "I16" && !seen[cd.edge.Source] && t.visited[cd.edge.Source] == nil {
			seen[cd.edge.Source] = true
			ids = append(ids, cd.edge.Source)
		}
	}
	return t.nodesOf(graph, ids)
}

// modelStep builds the Model of an I16 step from the route's stored uses
// of the declaration model.
func modelStep(route *contract.Node, model string) *ModelStep {
	out := &ModelStep{Uses: []ModelUseLabel{}}
	if _, _, q, err := contract.ParseIdentity(model); err == nil {
		out.Name = q
	}
	if route == nil {
		return out
	}
	list, _ := route.Attributes[modelUsesKey].([]any)
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		if m["model"] != model {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out.Name = name
		}
		u := ModelUseLabel{Confidence: 1}
		if c, ok := number(m["confidence"]); ok {
			u.Confidence = c
		}
		u.Section, _ = m["section"].(string)
		u.Kind, _ = m["kind"].(string)
		u.Rule, _ = m["rule"].(string)
		u.File, _ = m["file"].(string)
		if s, ok := m["status"].(string); ok && s != "" {
			u.Status = &s
		}
		if l, ok := number(m["line"]); ok {
			u.Line = int(l)
		}
		out.Uses = append(out.Uses, u)
	}
	return out
}

// number reads a stored attribute number, whichever way the store
// decoded it.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// modelRoles are the wording of a use's section (row MT-15).
func modelRole(section string) string {
	switch section {
	case "body":
		return "request body"
	case "responses":
		return "response"
	case "":
		return ""
	}
	return section + " parameter"
}

// modelWording is the wording of a path ending in an I16 step: "through
// request body model UserIn (fact)", "through response model User
// (returned)", one part per role with its provenance kinds; the relation's
// wording plus the name when the route stores no uses (row MT-15).
func modelWording(s Step) string {
	if s.Model == nil {
		rel, _ := relationOf("I16")
		return rel.Wording
	}
	kinds := map[string][]string{}
	var roles []string
	for _, u := range s.Model.Uses {
		role := modelRole(u.Section)
		if role == "" {
			continue
		}
		if _, ok := kinds[role]; !ok {
			roles = append(roles, role)
		}
		if !inList(kinds[role], u.Kind) {
			kinds[role] = append(kinds[role], u.Kind)
		}
	}
	if len(roles) == 0 {
		return "through request/response model " + s.Model.Name
	}
	order := map[string]int{"request body": 0, "response": 2}
	rank := func(r string) int {
		if v, ok := order[r]; ok {
			return v
		}
		return 1
	}
	sort.SliceStable(roles, func(i, j int) bool { return rank(roles[i]) < rank(roles[j]) })
	parts := make([]string, 0, len(roles))
	for _, r := range roles {
		k := append([]string{}, kinds[r]...)
		sort.Strings(k)
		parts = append(parts, r+" ("+strings.Join(k, ", ")+")")
	}
	return "through " + strings.Join(parts, " and ") + " model " + s.Model.Name
}

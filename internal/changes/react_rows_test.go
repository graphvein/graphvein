package changes

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
)

// TestReactRows holds T019 Revision 7's class rules: react-router routes
// and React bridge edges are C17 ("React UI"), Angular structure edges stay
// C12, and other passed functions are C16.
func TestReactRows(t *testing.T) {
	node := func(id string, attrs map[string]any) *contract.Node {
		return &contract.Node{Identity: id, Kind: identityKind(id), Attributes: attrs}
	}
	c := &classifier{graphNodes: map[string]*contract.Node{}}
	for _, n := range []*contract.Node{
		node("ui_route:a.tsx#ROUTE%20x", map[string]any{"framework": "react-router"}),
		node("ui_route:a.ts#ROUTE%20y", map[string]any{"framework": "angular"}),
		node("class:p.ts#ParentComponent", map[string]any{"angular": map[string]any{"role": "component"}}),
		node("class:c.ts#ChildComponent", map[string]any{"angular": map[string]any{"role": "component"}}),
		node("class:r.tsx#Board", map[string]any{}),
		node("class:r.tsx#Cell", map[string]any{}),
	} {
		c.graphNodes[n.Identity] = n
	}
	cases := []struct {
		name, kind, source, target, want string
	}{
		{"function renders function", "renders", "function:a.tsx#Page", "function:b.tsx#Card", "C17"},
		{"module code renders a class", "renders", "file:a.tsx#a.tsx", "class:r.tsx#Board", "C17"},
		{"class component renders a class component", "renders", "class:r.tsx#Board", "class:r.tsx#Cell", "C17"},
		{"Angular component renders a component", "renders", "class:p.ts#ParentComponent", "class:c.ts#ChildComponent", "C12"},
		{"renders between unknown classes", "renders", "class:x.ts#X", "class:y.ts#Y", "C12"},
		{"react-router route to its Component", "routes_to", "ui_route:a.tsx#ROUTE%20x", "function:b.tsx#Card", "C17"},
		{"react-router loader", "passes_function", "ui_route:a.tsx#ROUTE%20x", "function:l.ts#loader", "C17"},
		{"Angular route", "routes_to", "ui_route:a.ts#ROUTE%20y", "class:c.ts#ChildComponent", "C12"},
		{"callback passed by a function", "passes_function", "function:a.ts#run", "function:a.ts#tick", "C16"},
	}
	for _, tc := range cases {
		got := c.edgeRow(&contract.Edge{Kind: tc.kind, Source: tc.source, Target: tc.target})
		if got != tc.want {
			t.Errorf("%s: row %s, want %s", tc.name, got, tc.want)
		}
	}
	if got := nodeRow(c.graphNodes["ui_route:a.tsx#ROUTE%20x"]); got != "C17" {
		t.Errorf("react-router route: row %s, want C17", got)
	}
	if got := nodeRow(c.graphNodes["ui_route:a.ts#ROUTE%20y"]); got != "C12" {
		t.Errorf("Angular route: row %s, want C12", got)
	}
}

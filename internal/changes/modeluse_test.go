package changes

import (
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// TestModelUseNames keeps the names this package reads equal to the ones
// the indexer writes (rows MT-1, MT-2, MT-3, MT-14).
func TestModelUseNames(t *testing.T) {
	if modelUsesKey != routeshape.ModelUsesKey {
		t.Errorf("attribute %q, the indexer writes %q", modelUsesKey, routeshape.ModelUsesKey)
	}
	f, err := followOf("I16", routeshape.UsesModelEdge)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range f.From {
		if !routeshape.DeclarationKind(k) {
			t.Errorf("I16 follows from %s, which is no declaration kind", k)
		}
	}
	for _, k := range []string{"class", "struct", "type", "interface", "type_alias", "enum", "union", "variable", "constant"} {
		if !inList(f.From, k) || !routeshape.DeclarationKind(k) {
			t.Errorf("declaration kind %s: I16 %t, indexer %t", k, inList(f.From, k), routeshape.DeclarationKind(k))
		}
	}
	if e := (&classifier{}).edgeRow(&contract.Edge{Kind: routeshape.UsesModelEdge}); e != "C3" {
		t.Errorf("uses_model edges are row %s, want C3 (MT-13)", e)
	}
}

// TestModelWording holds row MT-15: the role and kinds of every use, the
// relation's wording when the route stores no uses.
func TestModelWording(t *testing.T) {
	route := &contract.Node{Attributes: map[string]any{modelUsesKey: []any{
		map[string]any{"model": "class:m.py#User", "name": "User", "section": "body", "kind": "fact", "rule": "SH-F18", "file": "a.py", "line": float64(3)},
		map[string]any{"model": "class:m.py#User", "name": "User", "section": "responses", "status": "200", "kind": "returned", "rule": "SH-F26", "file": "a.py", "line": float64(4)},
		map[string]any{"model": "class:m.py#User", "name": "User", "section": "responses", "status": "201", "kind": "mapped", "rule": "SH-O4", "file": "spec.yaml", "line": float64(9), "confidence": 0.7},
		map[string]any{"model": "class:m.py#User", "name": "User", "section": "query", "kind": "fact", "rule": "SH-G30", "file": "a.py", "line": float64(5)},
		map[string]any{"model": "class:m.py#Other", "name": "Other", "section": "body", "kind": "fact", "rule": "SH-F18", "file": "a.py", "line": float64(6)},
	}}}
	m := modelStep(route, "class:m.py#User")
	if len(m.Uses) != 4 || m.Name != "User" || m.Uses[2].Confidence != 0.7 || m.Uses[1].Status == nil || *m.Uses[1].Status != "200" {
		t.Fatalf("model step = %+v", m)
	}
	got := modelWording(Step{Model: m})
	want := "through request body (fact) and query parameter (fact) and response (mapped, returned) model User"
	if got != want {
		t.Errorf("wording %q, want %q", got, want)
	}
	if got := modelWording(Step{Model: modelStep(nil, "class:m.py#Gone")}); got != "through request/response model Gone" {
		t.Errorf("wording without uses %q", got)
	}
	if got := modelWording(Step{}); got != "through request/response model" {
		t.Errorf("wording without a model %q", got)
	}
}

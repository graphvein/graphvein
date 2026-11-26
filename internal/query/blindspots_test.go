package query

import (
	"context"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
	"testing"
)

type edgeStore struct {
	store.Store
	edges []contract.Edge
}

func (s edgeStore) Edges(context.Context, string, []string, []string, store.Direction) ([]contract.Edge, error) {
	return s.edges, nil
}

func TestBlindSpotNamesAndUnavailableData(t *testing.T) {
	name, caller := "save", "function:elsewhere.py#blind"
	span := contract.Span{StartLine: 1, EndLine: 1, EndColumn: 6}
	target := Node{Node: contract.Node{Identity: "method:a.py#A.save", QualifiedName: "A.save"}}
	reached := Node{Node: contract.Node{Identity: "method:b.py#B.save%402", QualifiedName: "B.save@2"}}
	newResponse := func() Response {
		return Response{Target: &target, Results: []Result{{Node: reached}}, PossiblyMissingCallSites: []MissingSite{}}
	}
	out := newResponse()
	if err := blindSpots(context.Background(), edgeStore{}, "r", []contract.Node{{File: "elsewhere.py", Attributes: map[string]any{}}}, &out); err != nil || out.BlindSpotData || out.PossiblyMissingCallers != 0 {
		t.Fatal(out, err)
	}
	files := []contract.Node{{File: "elsewhere.py", Attributes: map[string]any{"unresolved_unknown_call_sites": []MissingSite{{Name: &name, Caller: &caller, Span: span}, {Name: nil, Span: span}}}}}
	out = newResponse()
	if err := blindSpots(context.Background(), edgeStore{}, "r", files, &out); err != nil || !out.BlindSpotData || out.PossiblyMissingCallers != 1 || len(out.PossiblyMissingCallSites[0].Matches) != 2 {
		t.Fatal(out, err)
	}
	out = newResponse()
	resolved := edgeStore{edges: []contract.Edge{{Kind: "calls", Source: caller, Evidence: []contract.Evidence{{File: "elsewhere.py", Span: span}}}}}
	if err := blindSpots(context.Background(), resolved, "r", files, &out); err == nil {
		t.Fatal("resolved call also reported as unknown")
	}
}

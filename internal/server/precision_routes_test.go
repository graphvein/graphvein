package server

import (
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/precision"
	"github.com/graphvein/graphvein/internal/store"
)

// TestRouteLabels checks the Go routes row (PL-F10, T023 GO4): endpoint
// labels name the endpoints' common framework, so Go routes are Precise,
// Express routes best effort and a transport of both a marker conflict;
// change items of a route or its exposes_route edge are labelled by the
// route's framework.
func TestRouteLabels(t *testing.T) {
	run := runOfIndexer("graphvein", index.Version)
	route := func(fw string) contract.Node {
		return contract.Node{Kind: "route", Attributes: map[string]any{"framework": fw}}
	}
	for _, c := range []struct {
		name   string
		nodes  []contract.Node
		label  precision.Label
		row    string
		reason string
	}{
		{"chi", []contract.Node{route("chi"), route("chi")}, precision.Precise, "PL-F10", ""},
		{"net/http", []contract.Node{route("net/http")}, precision.Precise, "PL-F10", ""},
		{"express", []contract.Node{route("express")}, precision.BestEffort, precision.DefaultRow, precision.ReasonDefault},
		{"mixed", []contract.Node{route("gin"), route("express")}, precision.BestEffort, precision.DefaultRow, precision.ReasonMarkerConflict},
		{"none", nil, precision.BestEffort, precision.DefaultRow, precision.ReasonDefault},
	} {
		got := labelOf(run, routeMarker(familyRoutes, c.nodes))
		reason := ""
		if got.Reason != nil {
			reason = *got.Reason
		}
		if got.Label != c.label || got.Row != c.row || reason != c.reason {
			t.Errorf("%s: %+v (reason %q)", c.name, got, reason)
		}
	}
	node := changes.Item{Record: store.RecordNode, Kind: "route", After: &changes.State{Fields: map[string]any{"framework": "gin"}}}
	if l := itemScope(node, run, nil, nil, nil, nil); l == nil || l.Row != "PL-F10" {
		t.Errorf("route item: %+v", l)
	}
	edge := changes.Item{Record: store.RecordEdge, Kind: "exposes_route", Name: "function:a.go#h -> route:a.go#GET /x"}
	// Handler identity is outside PL-F10's scope (T023 GR10).
	if l := itemScope(edge, run, nil, nil, nil, map[string]string{"route:a.go#GET /x": "chi"}); l == nil || l.Row != precision.DefaultRow || l.Reason == nil || *l.Reason != reasonNotCompared {
		t.Errorf("exposes_route item: %+v", l)
	}
	if l := labelOf(run, routeMarker(familyRoutes, []contract.Node{route("chi")})); len(l.Unverified) != 4 || l.Unverified[0] != "handler" {
		t.Errorf("chi route label: unverified %v", l.Unverified)
	}
	if l := itemScope(edge, run, nil, nil, nil, nil); l == nil || l.Row != precision.DefaultRow {
		t.Errorf("exposes_route item without its route: %+v", l)
	}
}

package server

import (
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/query"
	"github.com/graphvein/graphvein/internal/shapediff"
)

// This file holds the shape_differences of a link (T029 W4, rows SD-6 to
// SD-12 of docs/contract/claims/shape-differences.md): what a linked
// client call sends compared with what the route reads, served on the
// callers of GET /api/v0/api/endpoint (also the project's
// cross-repository callers) and on the calls_route edges of GET
// /api/v0/api/flow. It is computed when the link is presented, from the
// two stored nodes only, so it is never out of step with them.

// shapeDifferencesDoc is the comparison of one link. Absent (nil) means
// nothing was compared, which the UI shows as nothing at all; present
// with no differences means the compared sections agree.
type shapeDifferencesDoc struct {
	// CrossRepository marks a cross-repository link (project mode, row
	// SD-11): the call and the route are in different members.
	CrossRepository bool                   `json:"cross_repository,omitempty"`
	Compared        []string               `json:"compared"`
	Differences     []shapediff.Difference `json:"differences"`
	// CallLinks and RouteLinks map each "file:line" a difference names on
	// that side to its editor link, as shape_links does for the route's
	// chips (one map per side: in project mode the two sides are in
	// different checkouts); absent without links.
	CallLinks  map[string]*query.IDELink `json:"call_links,omitempty"`
	RouteLinks map[string]*query.IDELink `json:"route_links,omitempty"`
}

// linker builds the editor link of one line, or nil.
type linker func(file string, line int) *query.IDELink

// presenterLinker is p's line link, or none when p is nil (a member
// without a checkout).
func presenterLinker(p *presenter) linker {
	return func(file string, line int) *query.IDELink {
		if p == nil {
			return nil
		}
		return p.link(file, contract.Span{StartLine: line, EndLine: line})
	}
}

// shapeDifferences compares the call's stored "sends" with the route's
// stored shape. Both must decode and pass their checks; otherwise the
// link has no comparison.
func shapeDifferences(route, call contract.Node, routeLink, callLink linker, crossRepository bool) *shapeDifferencesDoc {
	if route.Kind != "route" || call.Kind != "client_call" {
		return nil
	}
	value, ok := call.Attributes[shapediff.AttributeKey]
	if !ok {
		return nil
	}
	sends, err := shapediff.FromAttribute(value)
	if err != nil {
		return nil
	}
	shape, _ := routeShape(route)
	result := shapediff.Compare(shape, sends)
	if result == nil {
		return nil
	}
	out := &shapeDifferencesDoc{CrossRepository: crossRepository, Compared: result.Compared, Differences: result.Differences}
	add := func(m *map[string]*query.IDELink, l linker, side shapediff.Side) {
		link := l(side.File, side.Line)
		if link == nil {
			return
		}
		if *m == nil {
			*m = map[string]*query.IDELink{}
		}
		(*m)[shapeLinkKey(side.File, side.Line)] = link
	}
	for _, d := range result.Differences {
		add(&out.CallLinks, callLink, d.Call)
		add(&out.RouteLinks, routeLink, d.Route)
	}
	return out
}

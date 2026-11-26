package server

import (
	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
)

// routeShape reads a route's stored shape attribute (routeshape rows SM-1
// and D). A route without one returns (nil, nil): every section is not
// read, and the endpoint responses leave the shape fields out. A stored
// shape that does not decode or breaks a model invariant is never served
// as knowledge: shape is nil and the error says why.
func routeShape(n contract.Node) (*routeshape.Shape, *string) {
	value, ok := n.Attributes[routeshape.AttributeKey]
	if !ok {
		return nil, nil
	}
	s, err := routeshape.FromAttribute(value)
	if err != nil {
		msg := err.Error()
		return nil, &msg
	}
	return s, nil
}

// routeShapeSummary is the compact shape of a route list row: nil when
// the route has no valid shape.
func routeShapeSummary(n contract.Node) *routeshape.Summary {
	s, _ := routeShape(n)
	if s == nil {
		return nil
	}
	sum := routeshape.Summarize(s)
	return &sum
}

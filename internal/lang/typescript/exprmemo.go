package typescript

import (
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/parse"
)

// exprMemo builds each node's detached expression once per file, so that a
// call nested in another call's arguments or callee chain shares one value
// (and its Args, Parts, Properties and Object) between the enclosing call's
// tree and its own client record (T069). Without it the facts of an
// expression grew with its size times its call nesting.
//
// A nil *exprMemo builds without sharing, as before T069. Sharing is safe
// only while nothing writes into a built expression's slices, maps or Object
// in place: copy them first (angularExpr's extension, compactedURL). The
// file's URL object rewrite (cacheURLObjects) does write in place, so a file
// that constructs a URL is extracted without a memo (hasURLConstructor).
type exprMemo struct {
	routes, urls, angulars map[*parse.Node]facts.RouteExpr
}

func newExprMemo() *exprMemo {
	return &exprMemo{routes: map[*parse.Node]facts.RouteExpr{}, urls: map[*parse.Node]facts.RouteExpr{}, angulars: map[*parse.Node]facts.RouteExpr{}}
}

// memoFor returns a memo for the file of root, or nil when the file
// constructs a URL (see exprMemo).
func memoFor(root *parse.Node) *exprMemo {
	if hasURLConstructor(root) {
		return nil
	}
	return newExprMemo()
}

// hasURLConstructor reports whether the tree has a `new URL(...)`, the
// condition under which cacheURLSyntax rewrites client and URL binding
// syntax in place (cacheURLObjects). It checks every node, a superset of
// the nodes cacheURLSyntax visits.
func hasURLConstructor(n *parse.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == "new_expression" && text(n.Fields["constructor"]) == "URL" {
		return true
	}
	for _, c := range n.Children {
		if hasURLConstructor(c) {
			return true
		}
	}
	return false
}

func (m *exprMemo) route(n *parse.Node) facts.RouteExpr {
	if m == nil {
		return m.buildRoute(n)
	}
	return memo(m.routes, n, m.buildRoute)
}

func (m *exprMemo) url(n *parse.Node) facts.RouteExpr {
	if m == nil {
		return m.buildURL(n)
	}
	return memo(m.urls, n, m.buildURL)
}

func (m *exprMemo) angular(n *parse.Node) facts.RouteExpr {
	if m == nil {
		return m.buildAngular(n)
	}
	return memo(m.angulars, n, m.buildAngular)
}

// memo returns build's expression of n, building it once. The builders are
// pure functions of the node.
func memo(seen map[*parse.Node]facts.RouteExpr, n *parse.Node, build func(*parse.Node) facts.RouteExpr) facts.RouteExpr {
	if n == nil {
		return build(n)
	}
	if e, ok := seen[n]; ok {
		return e
	}
	e := build(n)
	seen[n] = e
	return e
}

// compactor returns how the file's URL binding syntax is compacted for the
// cache: compactedURL when expressions are shared, else in place as before
// T069. In place matters there: cacheURLObjects gives a URL object proof
// the Args of its binding's value, so compacting the value in place also
// compacts those Args in every client and binding the proof replaced.
func (m *exprMemo) compactor() func(facts.RouteExpr) facts.RouteExpr {
	if m != nil {
		return compactedURL
	}
	return func(e facts.RouteExpr) facts.RouteExpr {
		compactURLCache(&e)
		return e
	}
}

// compactedURL is compactURLCache without writing into e: a copy whose
// Text is cleared on e and on every expression reachable through Object,
// Args, Parts and Properties (except meta_property), sharing everything
// else with e.
func compactedURL(e facts.RouteExpr) facts.RouteExpr {
	if e.Kind != "meta_property" {
		e.Text = ""
	}
	if e.Object != nil {
		o := compactedURL(*e.Object)
		e.Object = &o
	}
	e.Args = compactedURLs(e.Args)
	e.Parts = compactedURLs(e.Parts)
	if e.Properties != nil {
		properties := make(map[string]facts.RouteExpr, len(e.Properties))
		for k, v := range e.Properties {
			properties[k] = compactedURL(v)
		}
		e.Properties = properties
	}
	return e
}

func compactedURLs(list []facts.RouteExpr) []facts.RouteExpr {
	if list == nil {
		return nil
	}
	out := make([]facts.RouteExpr, len(list))
	for i, v := range list {
		out[i] = compactedURL(v)
	}
	return out
}

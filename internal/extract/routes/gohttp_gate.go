package routes

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The ServeMux -> chi hop (contract row GR-C7, task T023): a chi
// router registered at the pattern "/" of a ServeMux, directly or through a
// forwarding closure (GR-O8), keeps its paths. Go 1.27.1's
// ServeMux.ServeHTTP (net/http/server.go) passes the request it received to
// the matched handler, setting only r.Pattern and the matched wildcards;
// chi routes on r.URL.RawPath or r.URL.Path and its own context, which the
// ServeMux does not set. The "/" pattern is the least specific of a mux, so
// a request reaches the chi router only when no other pattern of that mux
// matches it, when the mux does not redirect it (a path that is not clean,
// or /tree for a registered /tree/), and when a forwarding closure does
// not answer it itself. A composed path any of these may take is refused:
// no claim, never a guess.

// goGate is the set of patterns that may take a request from a root
// ServeMux's "/" pattern: its other registrations and compositions. opaque
// reports a Handle or HandleFunc call on the mux whose pattern is unknown.
type goGate struct {
	patterns []*goMuxPat
	opaque   bool
	// entries are the same siblings with where they are registered (an
	// unknown pattern has no pat; a mux-wide reason has no record): GR-C3
	// hops skip those in the other branch of an if.
	entries []goGateEntry
}

// goGateEntry is one sibling of a gate.
type goGateEntry struct {
	pat *goMuxPat
	at  goAt
}

// goMuxExtra is a known sibling pattern recorded without a registration.
type goMuxExtra struct {
	pattern string
	at      goAt
}

// goExclusive reports two positions in different branches of one if
// statement of the same record (then and else): they never both run.
func goExclusive(a, b goAt) bool {
	if a.rec == nil || a.rec != b.rec || a.rec.fn == nil {
		return false
	}
	blocks := a.rec.fn.Blocks
	branch := map[int]int{}
	for x := a.block; x > 0 && x < len(blocks); x = blocks[x].Parent {
		if k := blocks[x].Kind; k == "then" || k == "else" {
			branch[blocks[x].Parent] = x
		}
	}
	for y := b.block; y > 0 && y < len(blocks); y = blocks[y].Parent {
		if k := blocks[y].Kind; k == "then" || k == "else" {
			if x, ok := branch[blocks[y].Parent]; ok && x != y {
				return true
			}
		}
	}
	return false
}

// goGateHop is one ServeMux -> chi hop a chain passed: the gate of its
// root mux and the shadows of the forwarding closure (none for a router
// registered directly).
type goGateHop struct {
	id      string
	gate    *goGate
	shadows []string
}

// gate collects the sibling patterns of root, a net/http root mux: the
// patterns of every registration and composition on the root or on an
// object a "same" edge carries it to (a parameter it binds), other than
// the "/" chi hops themselves.
func (l *goLinker) gate(g *goGraph, root string) *goGate {
	if gt, ok := l.gates[root]; ok {
		return gt
	}
	gt := &goGate{}
	l.gates[root] = gt
	group := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range g.out[id] {
			if e.kind == "same" && !group[e.child] && l.objects[e.child].framework == goNetHTTP {
				group[e.child] = true
				queue = append(queue, e.child)
			}
		}
	}
	add := func(pattern string, at goAt) {
		p, err := goParseMuxPattern(pattern)
		if err != nil {
			gt.opaque = true
			gt.entries = append(gt.entries, goGateEntry{at: at})
			return
		}
		gt.patterns = append(gt.patterns, p)
		gt.entries = append(gt.entries, goGateEntry{pat: p, at: at})
	}
	var ids []string
	for id := range group {
		ids = append(ids, id)
		for _, p := range l.muxExtra[id] {
			add(p.pattern, p.at)
		}
		for _, at := range l.muxOpaqueAt[id] {
			gt.entries = append(gt.entries, goGateEntry{at: at})
		}
		if l.muxOpaque[id] {
			gt.opaque = true
		}
		if o := l.objects[id]; o != nil && (len(o.escapes) > 0 || o.kind == "default") {
			gt.entries = append(gt.entries, goGateEntry{})
			// Step GR8: a mux that escapes may get registrations no rule
			// sees (A2); the DefaultServeMux gets the init-time
			// registrations of imported packages (net/http/pprof,
			// expvar: B2).
			gt.opaque = true
		}
	}
	sort.Strings(ids)
	for _, reg := range l.registrations {
		if reg.framework == goNetHTTP && goAny(reg.objects, group) {
			add(reg.pattern, goAt{reg.record, reg.call.Block, goStart(reg.call.Span)})
		}
	}
	for _, c := range l.compositions {
		if c.kind != "mux" && c.kind != "muxchi" || !goAny(c.parents, group) {
			continue
		}
		outer, _, ok := l.constant(c.at, c.outer)
		switch {
		case !ok || l.module(c.at.rec.file).legacy:
			gt.opaque = true
			gt.entries = append(gt.entries, goGateEntry{at: c.at})
		case c.kind == "muxchi":
			// The hop itself ("/"); a second one conflicts and is
			// refused as invalid_pattern.
		default:
			add(outer, c.at)
		}
	}
	return gt
}

// goAny reports that one of ids is in set.
func goAny(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}

// allows reports that a chi registration with method (ANY for every
// method) and path p (chi syntax, as the root mux sees it) is reached
// through the hop: p is clean, has a ServeMux equivalent, and no sibling
// pattern, no /tree -> /tree/ redirect of a sibling and no shadow of the
// forwarding closure may match any request of it.
func (h *goGateHop) allows(method, p string) bool {
	if h.gate.opaque {
		return false
	}
	q, ok := goChiAsMux(method, p)
	if !ok {
		return false
	}
	for _, s := range h.gate.patterns {
		if s.comparePathsAndMethods(q) != goDisjoint {
			return false
		}
		// ServeMux redirects /tree to /tree/ when /tree/ is a subtree
		// pattern and /tree matches no pattern exactly.
		if text := s.str; strings.HasSuffix(text, "/") && s.last().multi && len(s.segments) > 1 {
			bare, err := goParseMuxPattern(strings.TrimSuffix(text, "/"))
			if err != nil || bare.comparePathsAndMethods(q) != goDisjoint {
				return false
			}
		}
	}
	for _, sh := range h.shadows {
		if !strings.HasPrefix(sh, "/") || strings.ContainsAny(sh, "{}") {
			return false
		}
		text := sh
		if strings.HasSuffix(text, "/") {
			text += "{$}"
		}
		s, err := goParseMuxPattern(text)
		if err != nil || s.comparePathsAndMethods(q) != goDisjoint {
			return false
		}
	}
	return true
}

// goChiAsMux is a ServeMux pattern matching every request a chi route with
// method and path p matches (a superset: a chi regexp parameter reads as a
// wildcard segment, a GET route also as HEAD): {name} and {name:re}
// segments become wildcards, a final * a multi-segment wildcard, a final
// '/' the exact {$}. A path ServeMux would clean and redirect ("//", "."
// or ".." segments), or a parameter inside a segment, has none.
func goChiAsMux(method, p string) (*goMuxPat, bool) {
	if !strings.HasPrefix(p, "/") || strings.Contains(p, "//") {
		return nil, false
	}
	segs := strings.Split(p[1:], "/")
	for i, seg := range segs {
		switch {
		case seg == "." || seg == "..":
			return nil, false
		case seg == "*" && i == len(segs)-1:
			segs[i] = fmt.Sprintf("{gv%d...}", i)
		case strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") && strings.Count(seg, "{") == 1 && strings.Count(seg, "}") == 1:
			segs[i] = fmt.Sprintf("{gv%d}", i)
		case seg == "" && i == len(segs)-1:
			segs[i] = "{$}"
		case strings.ContainsAny(seg, "{}*"):
			return nil, false
		}
	}
	text := "/" + strings.Join(segs, "/")
	if method != "" && method != "ANY" {
		text = method + " " + text
	}
	q, err := goParseMuxPattern(text)
	if err != nil {
		return nil, false
	}
	return q, true
}

// goHopsAllow checks the siblings of every net/http hop of a chain
// (Go 1.27.1 ServeMux precedence: the most specific matching
// pattern wins, and /tree redirects to /tree/ when only the subtree
// pattern matches it exactly): a hop's composed pattern is refused when a
// sibling pattern on the hop's parent mux that is more specific than the
// hop's outer pattern matches every request of it (the composed path is
// then never dispatched to the route; a partial overlap is the matcher's
// precedence, not a claim), when a sibling subtree's bare path does (a
// redirect, the outer pattern ending in a wildcard), or when the parent
// mux has a registration of unknown pattern or escapes. A sibling in the
// other branch of an if than the hop's composition is skipped.
func goHopsAllow(pattern string, hops []goHop) bool {
	_, _, curPath, _ := goSplitPattern(pattern)
	method, host, _, _ := goSplitPattern(pattern)
	for i := len(hops) - 1; i >= 0; i-- {
		h := hops[i]
		curPath = h.prefix + curPath
		if h.gate == nil {
			continue
		}
		outer, err := goParseMuxPattern(h.outer)
		if err != nil {
			return false
		}
		text := host + curPath
		if method != "" {
			text = method + " " + text
		}
		q, err := goParseMuxPattern(text)
		if err != nil {
			return false
		}
		for _, en := range h.gate.entries {
			if goExclusive(en.at, h.at) {
				continue
			}
			s := en.pat
			if s == nil {
				// A sibling of unknown pattern, or a mux that escapes.
				return false
			}
			if s.comparePathsAndMethods(outer) == goMoreSpecific && goCoversAll(s, q) {
				return false
			}
			if t := s.str; strings.HasSuffix(t, "/") && s.last().multi && len(s.segments) > 1 && outer.last().multi {
				bare, err := goParseMuxPattern(strings.TrimSuffix(t, "/"))
				if err != nil || goCoversAll(bare, q) {
					return false
				}
			}
		}
	}
	return true
}

// mountsAllow checks the chi trees a chain was mounted from (step GR8,
// B5; chi v5 tree.go: a static or parameter node is tried before the
// mount's catch-all, so a route of the parent tree that matches every
// request of the composed path takes them all): a chi route is refused
// through the chain when a registration of the same method (or ANY) in a
// parent tree matches every request of the path relative to that tree.
func (l *goLinker) mountsAllow(reg goRegistration, c goChain, p string) bool {
	for _, m := range c.mounts {
		if !strings.HasPrefix(p, m.base) {
			continue
		}
		rel := p[len(m.base):]
		tree := l.chiTree(m.tree)
		for _, s := range l.registrations {
			if s.framework != goChi || s.file == reg.file && s.site == reg.site || !goAny(s.objects, tree) || s.method != reg.method && s.method != "ANY" {
				continue
			}
			if goChiCovers(s.name, rel) {
				return false
			}
		}
	}
	return true
}

// chiTree is the set of chi objects that share id's routing tree: those
// connected to it by "same" edges (Group and With children, parameter
// bindings), memoized.
func (l *goLinker) chiTree(id string) map[string]bool {
	if t, ok := l.trees[id]; ok {
		return t
	}
	adj := map[string][]string{}
	for _, e := range l.graph.edges {
		if e.kind == "same" {
			adj[e.parent] = append(adj[e.parent], e.child)
			adj[e.child] = append(adj[e.child], e.parent)
		}
	}
	tree := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		for _, y := range adj[x] {
			if !tree[y] {
				tree[y] = true
				queue = append(queue, y)
			}
		}
	}
	l.trees[id] = tree
	return tree
}

// goChiCovers reports that chi pattern a matches every request path of
// chi pattern b: segment by segment, a static segment equals b's, and a
// parameter (a regexp one only when b's segment is static and the regexp
// matches it whole) matches b's segment. Catch-alls cover nothing here:
// the mount's own catch-all has the longer prefix.
func goChiCovers(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		x, y := as[i], bs[i]
		switch {
		case x == y && !strings.ContainsAny(x, "{}*"):
		case strings.HasPrefix(x, "{") && strings.HasSuffix(x, "}") && strings.Count(x, "{") == 1:
			name := x[1 : len(x)-1]
			if k := strings.IndexByte(name, ':'); k >= 0 {
				if y == "" || strings.ContainsAny(y, "{}*") {
					return false
				}
				re, err := regexp.Compile("^(?:" + name[k+1:] + ")$")
				if err != nil || !re.MatchString(y) {
					return false
				}
				continue
			}
			if y == "" || strings.Contains(y, "*") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// goCoversAll reports that pattern s matches every request of pattern q
// (equivalent or more general, net/http pattern.go).
func goCoversAll(s, q *goMuxPat) bool {
	rel := s.comparePathsAndMethods(q)
	return rel == goEquivalent || rel == goMoreGeneral
}

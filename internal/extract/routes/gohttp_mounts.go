package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unresolved mounts of Go routes (T029 FU-2, mounts.go): the hops where
// composition stops are the blocked edges of a prefix that is not a
// constant or of a router argument no rule proves (route_prefix_unresolved,
// route_mount_unresolved), and the wrapped and escaped routers (GR-C4).
// The paths below a hop are composed from the hop's object as from a root,
// through the same rows; the checks above the hop (GR-C1's parent tree,
// GR-C3's sibling patterns, GR-C7's gate) cannot run without the prefix,
// so the stored entry says shadowing was not checked.

// goFunctionName names the declaration enclosing rec in a mount key.
func goFunctionName(rec *goRecord) string {
	if rec == nil || rec.decl == nil || rec.decl.fn == nil || rec.decl.fn.Name == "" {
		return "(package)"
	}
	fn := rec.decl.fn
	if fn.Receiver != nil {
		if t := goReceiverName(&fn.Receiver.Type); t != "" {
			return t + "." + fn.Name
		}
	}
	return fn.Name
}

// goReceiverName is the named type of a receiver type (T, *T, T[P]).
func goReceiverName(e *facts.GoExpr) string {
	for depth := 0; e != nil && depth < 8; depth++ {
		switch e.Kind {
		case facts.GoIdentifier, facts.GoSelector:
			return e.Name
		}
		if e.Object != nil {
			e = e.Object
		} else {
			e = e.Type
		}
	}
	return ""
}

// goHopText is a hop call's text: the callee and its first argument
// (all false), or every argument with function literal bodies elided.
func goHopText(e *facts.GoExpr, all bool) string {
	if e == nil {
		return "?"
	}
	if e.Kind != facts.GoCall || e.Callee == nil {
		return goExprText(e)
	}
	args := e.Args
	if !all && len(args) > 1 {
		args = args[:1]
	}
	parts := make([]string, 0, len(args))
	for i := range args {
		a := args[i]
		switch t := goArgText(a); {
		case a.Kind == facts.GoFuncLit:
			parts = append(parts, "func(...){...}")
		case t != "":
			parts = append(parts, t)
		default:
			parts = append(parts, goExprText(&a))
		}
	}
	return goExprText(e.Callee) + "(" + strings.Join(parts, ",") + ")"
}

// hopAt records the hop of a blocked edge at site (once per site).
func (l *goLinker) hopAt(at goAt, site string, call *facts.GoExpr, span contract.Span, reason string) *mountHop {
	if h := l.hops[site]; h != nil {
		return h
	}
	h := &mountHop{file: at.rec.file, function: goFunctionName(at.rec), text: goHopText(call, false), full: goHopText(call, true), span: span, reason: reason}
	l.hops[site] = h
	return h
}

// hopText records the hop of a wrapped or escaped router at site; form
// is upFormWrapped or upFormEscaped (row UP-4).
func (l *goLinker) hopText(at goAt, site, short, full string, span contract.Span, form string) {
	if l.hops[site] != nil || at.rec == nil {
		return
	}
	l.hops[site] = &mountHop{file: at.rec.file, function: goFunctionName(at.rec), text: short, full: full, span: span, reason: "route_mount_unresolved", form: form}
}

// assignHopKeys gives every recorded hop its key.
func (l *goLinker) assignHopKeys() {
	hops := make([]*mountHop, 0, len(l.hops))
	for _, h := range l.hops {
		hops = append(hops, h)
	}
	assignMountKeys(hops)
}

// goStop is one object where a pathless registration's composition stops,
// with the hop that stops it.
type goStop struct {
	object string
	hop    *mountHop
}

// mountStops walks up from a registration's objects through resolved edges
// to the first hops that stop composition: an escaped or wrapped object
// (not passed), a blocked edge whose hop is recorded.
func (l *goLinker) mountStops(g *goGraph, objects []string) []goStop {
	seen := map[string]bool{}
	var out []goStop
	var up func(id string)
	up = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		stopped := false
		if o := l.objects[id]; o != nil {
			for _, site := range o.escapes {
				if h := l.hops[site]; h != nil {
					out = append(out, goStop{id, h})
					stopped = true
				}
			}
		}
		if site := l.wrappedAt[id]; site != "" {
			if h := l.hops[site]; h != nil {
				out = append(out, goStop{id, h})
				stopped = true
			}
		}
		for _, e := range g.in[id] {
			if e.hop != nil {
				out = append(out, goStop{id, e.hop})
			}
		}
		if stopped {
			return
		}
		for _, e := range g.in[id] {
			if e.blocked == "" {
				up(e.parent)
			}
		}
	}
	for _, id := range objects {
		up(id)
	}
	return out
}

// goMountMark stands for the unread part of a hop while the paths below
// it are composed (a literal segment no route has): it is cut out of each
// composed template, which leaves the known prefix above the hop and the
// path below it.
const goMountMark = "/0graphveinmount0"

// goHandleHop is the net/http Handle a wrapped router is passed to: the
// muxes it is registered on, its pattern and the prefix of a StripPrefix
// around the wrapper. Handle adds no other prefix, so the hop covers
// nothing (or only an unread StripPrefix prefix): the value "/" states
// that the wrapper passes the requests on unchanged (a wrapper that
// changes the path needs route values).
type goHandleHop struct {
	parents []string
	// pattern is Handle's pattern, strip the prefix of an
	// http.StripPrefix(p, x) handler argument (nil without one).
	pattern, strip *facts.GoExpr
	at             goAt
	file           string
}

// handleHop records the Handle call of a wrapped router hop recorded at
// site, when the muxes it is registered on are proven.
func (l *goLinker) handleHop(at goAt, site string, recv goValue, pattern, handler *facts.GoExpr) {
	h := l.hops[site]
	if h == nil || recv.unproven || len(recv.objects) == 0 {
		return
	}
	hh := goHandleHop{parents: recv.objects, pattern: pattern, at: at, file: at.rec.file}
	if handler.Kind == facts.GoCall && handler.Callee != nil && handler.Callee.Kind == facts.GoSelector && handler.Callee.Name == "StripPrefix" && len(handler.Args) == 2 {
		if imp, ok := l.importOf(at, handler.Callee.Object); ok && imp.framework == goNetHTTP {
			hh.strip = &handler.Args[0]
		}
	}
	l.handleHops[h] = hh
}

// hopEdges are the edges a hop stops (a blocked edge, or the Handle of a
// wrapped router), and whether the value covers the hop's own part
// (MountCoversHop), nothing (MountCoversNothing) or the whole prefix from
// the root (MountCoversRoot: a parent router is not proven, or the hop is
// an escape). The hop's covers and chain are set on first use.
func (l *goLinker) hopEdges(g *goGraph, h *mountHop) []goEdge {
	if l.hopStarts == nil {
		l.hopStarts = map[*mountHop][]goChain{}
	}
	var edges []goEdge
	for _, e := range g.edges {
		if e.hop == h {
			edges = append(edges, e)
		}
	}
	if hh, ok := l.handleHops[h]; ok && len(edges) == 0 {
		outer, _, known := l.constant(hh.at, hh.pattern)
		strip, stripKnown := "", true
		if hh.strip != nil {
			var ok bool
			strip, _, ok = l.constant(hh.at, hh.strip)
			stripKnown = ok && !strings.ContainsAny(strip, "{}")
		}
		tail, tailKnown := "", false
		if !known && !stripKnown {
			tail, tailKnown = l.outerTail(hh.at, hh.pattern, hh.strip)
		}
		for _, p := range hh.parents {
			edges = append(edges, goEdge{parent: p, kind: "mux", outer: outer, prefix: strip, unknownOuter: !known, unknownPrefix: !stripKnown, resolution: "literal", conditional: conditional(hh.at), file: hh.file, at: hh.at, hop: h, outerTail: tail, tailKnown: tailKnown})
		}
	}
	if _, done := l.hopStarts[h]; done {
		return edges
	}
	h.covers = MountCoversRoot
	var texts []string
	var starts []goChain
	for _, e := range edges {
		if len(g.chains[e.parent]) == 0 {
			l.hopStarts[h] = nil
			return nil
		}
		for _, c := range g.chains[e.parent] {
			if next, ok := l.hopStep(g, c, e, ""); ok {
				starts = append(starts, next)
				texts = append(texts, goChainText(next))
			}
		}
	}
	l.hopStarts[h] = starts
	if len(starts) == 0 {
		return nil
	}
	h.covers = MountCoversNothing
	for _, e := range edges {
		if e.unknownPrefix {
			h.covers = MountCoversHop
		}
	}
	h.chain = aboveChain(texts)
	return edges
}

// hopStep composes chain c through a hop's edge e into child as if the
// edge were read: the unread prefix is goMountMark, an unread outer
// pattern is "/" (the value states that the hop passes the requests on).
// chi's bare prefix form is not claimed below the mark, and the sibling
// patterns of a net/http hop's mux are not checked (shadowing not_checked).
func (l *goLinker) hopStep(g *goGraph, c goChain, e goEdge, child string) (goChain, bool) {
	x := e
	x.blocked, x.child = "", child
	if x.kind == "same" && e.unknownPrefix {
		x.kind = "chi"
		if o := l.objects[e.child]; o != nil && o.framework == goGin {
			x.kind = "gin"
		}
	}
	if e.unknownPrefix {
		x.prefix = goMountMark
	}
	if e.unknownOuter {
		x.outer = "/"
		if e.tailKnown && e.unknownPrefix {
			// GR-C3's tail check below the unread part:
			// StripPrefix(X) of an outer pattern X + T leaves
			// T + the rest, so only the paths starting with T are served
			// through the hop (Go 1.27.1 net/http).
			x.outer = goMountMark + e.outerTail
		}
	}
	next, ok := l.step(g, c, x)
	if ok && e.unknownPrefix && x.kind == "chi" {
		next.bare = false
	}
	if ok && x.kind == "mux" && len(next.hops) > 0 {
		// The sibling patterns beside the hop are not checked (OG-96):
		// its own unread pattern is one of them.
		hops := append([]goHop(nil), next.hops...)
		hops[len(hops)-1].gate = nil
		next.hops = hops
	}
	return next, ok
}

// goChainText is a chain's composition text for a hop's registration:
// its raw prefix, StripPrefix and net/http hops.
func goChainText(c goChain) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q %q", c.raw, c.strip)
	for _, h := range c.hops {
		fmt.Fprintf(&b, " [%q %q]", h.outer, h.prefix)
	}
	return b.String()
}

// mountEntries are a pathless registration's unresolved mounts: per hop,
// the paths composed through it (the known prefix above it in "above")
// when the routers above it are proven, else the paths composed from the
// hop's object as from a root. A registration whose objects have a chain
// (its paths were refused by a check, not stopped) gets none.
func (l *goLinker) mountEntries(g *goGraph, reg goRegistration) (out []mountEntry) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: reg.file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go unresolved mounts of %s %q failed: %v", reg.file, reg.method, reg.pattern, v)})
			out = nil
		}
	}()
	for _, id := range reg.objects {
		for _, c := range g.chains[id] {
			// A production chain reaches the registration: its path was
			// refused by a check, not stopped at a hop. A test-scope
			// chain says nothing about production (UP-9 per scope).
			if c.scope != testscope.Test {
				return nil
			}
		}
	}
	byKey := map[string]int{}
	for _, s := range l.mountStops(g, reg.objects) {
		edges := l.hopEdges(g, s.hop)
		chains := map[string][]goChain{}
		if s.hop.covers == MountCoversRoot {
			chains = l.relativeChains(g, s.object)
		} else {
			var starts []goChain
			for _, e := range edges {
				if e.child != "" && e.child != s.object {
					continue
				}
				for _, c := range g.chains[e.parent] {
					if next, ok := l.hopStep(g, c, e, s.object); ok {
						starts = append(starts, next)
					}
				}
			}
			l.walkChains(g, chains, s.object, starts)
		}
		rel := *g
		rel.chains = chains
		// The relative pass must not leave its refusals on the route.
		why, refused, passed := l.refusedWhy, l.gateRefused, l.gatePassed
		l.refusedWhy, l.gateRefused, l.gatePassed = map[string]string{}, map[string]bool{}, map[string]bool{}
		entries := goSplitMark(l.paths(&rel, reg), s.hop.covers)
		l.refusedWhy, l.gateRefused, l.gatePassed = why, refused, passed
		if len(entries) == 0 {
			continue
		}
		if i, ok := byKey[s.hop.key]; ok {
			// Two objects under one hop: one entry, every path.
			out[i].Paths = goSortEntries(goDistinctEntries(append(out[i].Paths.([]goPathEntry), entries...)))
			continue
		}
		byKey[s.hop.key] = len(out)
		out = append(out, newMountEntry(s.hop, goSortEntries(entries), "not_checked"))
	}
	return sortMountEntries(out)
}

// goSplitMark cuts goMountMark out of composed entries: the template
// before it is the known prefix above the hop (Above), the rest the path
// below the hop ("" for the prefix itself, "/" for the prefix with a
// trailing slash). With MountCoversHop an entry without the mark is
// dropped; with MountCoversNothing entries are the served paths.
func goSplitMark(entries []goPathEntry, covers string) []goPathEntry {
	var out []goPathEntry
	for _, e := range entries {
		i := strings.Index(e.Template, goMountMark)
		switch {
		case covers == MountCoversNothing:
			if i < 0 {
				out = append(out, e)
			}
			continue
		case i < 0 && covers == MountCoversHop:
			continue
		case i < 0:
			out = append(out, e)
			continue
		}
		above, below := e.Template[:i], e.Template[i+len(goMountMark):]
		if strings.Contains(below, goMountMark) || (covers == MountCoversRoot && above != "") {
			continue
		}
		if below == "" && e.TrailingSlash {
			below, e.TrailingSlash = "/", false
		}
		e.Template, e.Above = below, above
		out = append(out, e)
	}
	return goDistinctEntries(out)
}

// goSortEntries orders entries by known prefix, template and trailing
// slash.
func goSortEntries(list []goPathEntry) []goPathEntry {
	sort.Slice(list, func(a, b int) bool {
		if list[a].Above != list[b].Above {
			return list[a].Above < list[b].Above
		}
		if list[a].Template != list[b].Template {
			return list[a].Template < list[b].Template
		}
		return !list[a].TrailingSlash && list[b].TrailingSlash
	})
	return list
}

// goDistinctEntries drops repeated (prefix above, template, trailing
// slash) entries, keeping the first.
func goDistinctEntries(list []goPathEntry) []goPathEntry {
	seen := map[fastapiPathKey]bool{}
	var out []goPathEntry
	for _, e := range list {
		k := fastapiPathKey{e.Above + "\x00" + e.Template, e.TrailingSlash}
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

// relativeChains composes the chains from a hop's object as from a root
// (memoised per object), starting at goMountMark so that a path that is
// the prefix itself stays distinct from the prefix with a trailing slash.
func (l *goLinker) relativeChains(g *goGraph, object string) map[string][]goChain {
	if l.relChains == nil {
		l.relChains = map[string]map[string][]goChain{}
	}
	if c, ok := l.relChains[object]; ok {
		return c
	}
	chains := map[string][]goChain{}
	l.relChains[object] = chains
	o := l.objects[object]
	if o == nil {
		return chains
	}
	start := goChain{resolution: "literal", scope: l.scope(o.file), root: object, owner: object}
	if o.framework == goGin || o.framework == goChi {
		start.raw = goMountMark
	}
	l.walkChains(g, chains, object, []goChain{start})
	return chains
}

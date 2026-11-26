package routes

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Router-level middleware of gin and chi routes (task T029, row
// SH-S18 of docs/contract/claims/route-shape-security.md). A route's own
// middleware arguments and a chi With chain are the route attribute
// middleware already; this file adds the middleware its routers apply:
// gin Use and Group arguments, chi Use, a With or Group higher up, and
// gin.Default's Logger and Recovery. They are stored as the route
// attributes group_middleware (proven to run before the handler on every
// chain that serves the route) and group_middleware_unknown (anything
// whose order, condition or chain is not proven), so the security shape
// reads router-level auth like the Express mounted_middleware.
//
// Semantics, read in gin v1.12.0 (routergroup.go) and chi v5.5.0
// (mux.go): gin's Use appends to the group's handlers and a route or a
// child group copies the handlers at its registration or creation, so a
// Use applies to what is registered after it on that group. chi's Use on
// a router panics once a route, Mount, With or Group was added to it, and
// a With or Group copies the middleware its inline parent holds at
// creation, so there too exactly the Use calls before a registration
// apply. One rule serves both: a Use applies when it ends before the
// registration (or the creation of the child the route is on) in the same
// function body, unconditionally; a Use whose order is not proven is an
// unknown entry, never a fact.

// goMW is one middleware argument read on a router call.
type goMW struct {
	expr *facts.GoExpr
	text string
	file string
	span contract.Span
	at   goAt
	// implicit names the constructor that adds it (gin.Default).
	implicit string
}

// goUse is one Use call on proven router objects.
type goUse struct {
	objects []string
	mw      []goMW
	at      goAt
	span    contract.Span
}

// goCtor is the constructor call of a ctor object.
type goCtor struct {
	framework, name string
	at              goAt
	span            contract.Span
}

// goMWUnknown is one middleware that may run before a route but is not
// proven to.
type goMWUnknown struct {
	reason, expression, file string
	span                     contract.Span
}

// goRouteMW is what one emitted route's security shape reads: the route's
// own middleware arguments, its router-level middleware and the unknown
// entries.
type goRouteMW struct {
	route   []goMW
	group   []goMW
	unknown []goMWUnknown
}

// goGroupMW holds the linker's middleware records.
type goGroupMW struct {
	uses    []goUse
	seen    map[string]bool
	args    map[string][]goMW
	ctors   map[string]goCtor
	byRoute map[string]goRouteMW
}

// maxGoMWChains bounds the chains read per route; beyond it the route
// gets an unknown entry.
const maxGoMWChains = 64

func (l *goLinker) recordUse(at goAt, e *facts.GoExpr, recv goValue) {
	g := &l.groupMW
	span := l.callSpan(at, e)
	key := fmt.Sprintf("%s:%d:%d:%d:%d", at.rec.file, span.StartLine, span.StartColumn, span.EndLine, span.EndColumn)
	if g.seen == nil {
		g.seen = map[string]bool{}
	}
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	u := goUse{objects: append([]string(nil), recv.objects...), at: at, span: span}
	for i := range e.Args {
		u.mw = append(u.mw, goMW{expr: &e.Args[i], text: goArgText(e.Args[i]), file: at.rec.file, span: span, at: at})
	}
	g.uses = append(g.uses, u)
}

// recordGroupArgs keeps the middleware arguments of a gin Group (after
// its prefix) and of a chi With, applied when the child is created.
func (l *goLinker) recordGroupArgs(id, kind string, at goAt, e *facts.GoExpr) {
	var args []facts.GoExpr
	switch kind {
	case "ginGroup":
		args = e.Args[1:]
	case "with":
		args = e.Args
	default:
		return
	}
	g := &l.groupMW
	if g.args == nil {
		g.args = map[string][]goMW{}
	}
	if _, done := g.args[id]; done {
		return
	}
	span := l.callSpan(at, e)
	list := []goMW{}
	for i := range args {
		list = append(list, goMW{expr: &args[i], text: goArgText(args[i]), file: at.rec.file, span: span, at: at})
	}
	g.args[id] = list
}

func (l *goLinker) recordCtor(site, fw string, at goAt, e *facts.GoExpr) {
	g := &l.groupMW
	if g.ctors == nil {
		g.ctors = map[string]goCtor{}
	}
	if _, ok := g.ctors[site]; !ok && e.Callee != nil {
		g.ctors[site] = goCtor{framework: fw, name: e.Callee.Name, at: at, span: l.callSpan(at, e)}
	}
}

// goPoint is where something attaches to a router: a registration, the
// creation of a child, a binding call or a composition, in one function.
type goPoint struct {
	rec  *goRecord
	span contract.Span
}

// placeBefore says how a Use at span in rec stands to p: "before" (it
// ends before p starts in the same function body), "after", or
// "unknown" (another function, or overlapping syntax such as a chained
// call).
func placeBefore(rec *goRecord, span contract.Span, p goPoint) string {
	if rec != p.rec {
		return "unknown"
	}
	start := goStart(p.span)
	if start.after(span) {
		return "before"
	}
	if span.StartLine > p.span.StartLine || span.StartLine == p.span.StartLine && span.StartColumn > p.span.StartColumn {
		return "after"
	}
	return "unknown"
}

// goMWPath is the middleware one chain applies, outermost first.
type goMWPath struct {
	entries []goMW
	unknown []goMWUnknown
}

// routeMiddleware computes a registration's router-level middleware: the
// route attributes group_middleware and group_middleware_unknown, and the
// record the security shape reads.
func (l *goLinker) routeMiddleware(g *goGraph, reg *goRegistration) ([]map[string]any, []map[string]any) {
	if g == nil || reg.framework == goNetHTTP {
		return nil, nil
	}
	rm := goRouteMW{route: l.routeArgs(reg)}
	pt := goPoint{rec: reg.record, span: reg.call.Span}
	var paths []goMWPath
	budget := maxGoMWChains
	over := false
	for _, obj := range reg.objects {
		ps, ok := l.mwPaths(g, obj, "", pt, true, map[string]bool{}, &budget)
		if !ok {
			over = true
		}
		paths = append(paths, ps...)
	}
	if len(reg.objects) > 1 {
		rm.unknown = append(rm.unknown, goMWUnknown{reason: "the receiver may hold another router", expression: goArgText(*reg.call.Call.Callee.Object), file: reg.file, span: reg.call.Span})
	}
	if over {
		rm.unknown = append(rm.unknown, goMWUnknown{reason: fmt.Sprintf("more than %d chains", maxGoMWChains), file: reg.file, span: reg.call.Span})
	}
	if len(paths) == 0 {
		if over || len(reg.objects) > 1 {
			l.groupMWRoute(reg, rm)
			return nil, unknownAttrs(rm.unknown)
		}
		return nil, nil
	}
	// Entries every chain states are facts, in the first chain's order;
	// the others are unknown (some chains only).
	key := func(m goMW) string {
		return fmt.Sprintf("%s|%s|%d:%d|%s", m.text, m.file, m.span.StartLine, m.span.StartColumn, m.implicit)
	}
	count := map[string]int{}
	for _, p := range paths {
		seen := map[string]bool{}
		for _, m := range p.entries {
			if k := key(m); !seen[k] {
				seen[k] = true
				count[k]++
			}
		}
	}
	added := map[string]bool{}
	for _, p := range paths {
		for _, m := range p.entries {
			k := key(m)
			if added[k] {
				continue
			}
			added[k] = true
			if count[k] == len(paths) {
				rm.group = append(rm.group, m)
			} else {
				tagShape("SH-S18/chains")
				rm.unknown = append(rm.unknown, goMWUnknown{reason: "applied on some chains only", expression: m.text, file: m.file, span: m.span})
			}
		}
		rm.unknown = append(rm.unknown, p.unknown...)
	}
	rm.unknown = dedupeUnknown(rm.unknown)
	l.groupMWRoute(reg, rm)
	var facts []map[string]any
	for _, m := range rm.group {
		e := map[string]any{"name": m.text, "file": m.file, "span": m.span}
		if m.implicit != "" {
			e["added_by"] = m.implicit
		}
		facts = append(facts, e)
	}
	return facts, unknownAttrs(rm.unknown)
}

// safeRouteMiddleware is routeMiddleware isolated: a failure records a
// warning and stores no middleware attribute, never stopping the route.
func (l *goLinker) safeRouteMiddleware(g *goGraph, reg *goRegistration) (group, unknown []map[string]any) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostic(reg.file, "go_routes_failed", fmt.Sprintf("%s %q (router middleware: %v)", reg.method, reg.pattern, v), reg.call.Span)
			group, unknown = nil, nil
		}
	}()
	return l.routeMiddleware(g, reg)
}

func (l *goLinker) groupMWRoute(reg *goRegistration, rm goRouteMW) {
	if l.groupMW.byRoute == nil {
		l.groupMW.byRoute = map[string]goRouteMW{}
	}
	l.groupMW.byRoute[reg.identity] = rm
}

func unknownAttrs(list []goMWUnknown) []map[string]any {
	var out []map[string]any
	for _, u := range list {
		out = append(out, map[string]any{"reason": u.reason, "expression": u.expression, "file": u.file, "span": u.span})
	}
	return out
}

func dedupeUnknown(list []goMWUnknown) []goMWUnknown {
	seen := map[string]bool{}
	var out []goMWUnknown
	for _, u := range list {
		k := fmt.Sprintf("%s|%s|%s|%v", u.reason, u.expression, u.file, u.span)
		if !seen[k] {
			seen[k] = true
			out = append(out, u)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return before(out[i].span, out[j].span)
	})
	return out
}

// routeArgs are a registration's own middleware arguments: gin's
// handlers before the last, and a chi With chain (the route attribute
// middleware), as syntax for the security shape.
func (l *goLinker) routeArgs(reg *goRegistration) []goMW {
	var out []goMW
	switch reg.framework {
	case goGin:
		args := reg.call.Call.Args
		if reg.static != "" || len(args) < 3 {
			return nil
		}
		patternArg := 0
		if reg.call.Call.Callee.Name == "Handle" || reg.call.Call.Callee.Name == "Match" {
			patternArg = 1
		}
		for i := patternArg + 1; i < len(args)-1; i++ {
			out = append(out, goMW{expr: &args[i], text: goArgText(args[i]), file: reg.file, span: reg.call.Span, at: goAt{reg.record, reg.call.Block, goStart(reg.call.Span)}})
		}
	case goChi:
		if len(reg.objects) == 1 {
			out = l.withChain(reg.objects[0])
		}
	}
	return out
}

// withChain is the middleware of a chi With object and the With objects
// it was made from, outermost first.
func (l *goLinker) withChain(id string) []goMW {
	o := l.objects[id]
	if o == nil || o.kind != "with" {
		return nil
	}
	return append(l.withChain(o.parent), l.groupMW.args[id]...)
}

// mwPaths walks from obj, at p, up to the roots: each chain's middleware,
// outermost first. lead reports that obj is the registration's object or a
// With above it whose middleware the route attribute already lists. ok is
// false when the chain budget ran out.
func (l *goLinker) mwPaths(g *goGraph, obj, from string, p goPoint, lead bool, onPath map[string]bool, budget *int) ([]goMWPath, bool) {
	if onPath[obj] {
		return nil, true
	}
	onPath[obj] = true
	defer delete(onPath, obj)
	o := l.objects[obj]
	if o == nil {
		return nil, true
	}
	here := l.usesBefore(g, obj, from, p, false, map[string]bool{})
	// The object's own creation arguments (gin Group, chi With).
	var own []goMW
	if !(lead && o.kind == "with") {
		own = l.groupMW.args[obj]
	}
	var out []goMWPath
	ok := true
	isRoot := false
	for _, r := range g.roots {
		if r == obj {
			isRoot = true
		}
	}
	if isRoot {
		var base []goMW
		if c, found := l.groupMW.ctors[obj]; found && c.framework == goGin && c.name == "Default" {
			tagShape("SH-S18/gin.Default")
			for _, name := range []string{"Logger", "Recovery"} {
				expr := &facts.GoExpr{Kind: facts.GoCall, Callee: &facts.GoExpr{Kind: facts.GoSelector, Name: name, Object: &facts.GoExpr{Kind: facts.GoIdentifier, Name: "gin"}}}
				base = append(base, goMW{expr: expr, text: "gin." + name + "()", file: c.at.rec.file, span: c.span, at: c.at, implicit: "gin.Default"})
			}
		}
		starts := g.starts[obj]
		if len(starts) == 0 {
			out = append(out, goMWPath{entries: concatMW(base, own, here.entries), unknown: here.unknown})
		}
		for _, s := range starts {
			var unk []goMWUnknown
			for _, w := range s.wrappers {
				tagShape("SH-S18/served-wrapper")
				span := o.span
				if c, found := l.groupMW.ctors[obj]; found {
					span = c.span
				}
				unk = append(unk, goMWUnknown{reason: "the router is served through a wrapper that may run first", expression: w, file: s.file, span: span})
			}
			out = append(out, goMWPath{entries: concatMW(base, own, here.entries), unknown: append(unk, here.unknown...)})
		}
		*budget -= len(out)
		return out, *budget >= 0
	}
	for _, e := range g.in[obj] {
		if e.blocked != "" {
			continue
		}
		for _, up := range l.edgePoints(e, obj) {
			parentLead := lead && o.kind == "with" && e.kind == "same" && l.objects[e.parent] != nil && l.objects[e.parent].kind == "with" && o.parent == e.parent
			ps, fine := l.mwPaths(g, e.parent, obj, up.point, parentLead, onPath, budget)
			if !fine {
				ok = false
			}
			for _, pp := range ps {
				unk := append(append([]goMWUnknown(nil), pp.unknown...), up.unknown...)
				out = append(out, goMWPath{entries: concatMW(pp.entries, own, here.entries), unknown: append(unk, here.unknown...)})
			}
			if *budget < 0 {
				return out, false
			}
		}
	}
	return out, ok
}

func concatMW(lists ...[]goMW) []goMW {
	var out []goMW
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// goEdgePoint is where an object attaches to its parent through one edge,
// with any unknown entry the edge itself adds.
type goEdgePoint struct {
	point   goPoint
	unknown []goMWUnknown
}

// edgePoints are the attachment points of child to e.parent: its
// creation (a child object), each binding call (a parameter) or each
// composition call (chi Mount, net/http).
func (l *goLinker) edgePoints(e goEdge, child string) []goEdgePoint {
	o := l.objects[child]
	var out []goEdgePoint
	if o != nil && o.parent == e.parent && o.at.rec != nil {
		out = append(out, goEdgePoint{point: goPoint{rec: o.at.rec, span: l.callSpan(o.at, o.expr)}})
		return out
	}
	if e.kind == "same" {
		for _, b := range l.bindings {
			if b.param == child && b.object == e.parent && b.at.rec != nil {
				out = append(out, goEdgePoint{point: goPoint{rec: b.at.rec, span: b.span}})
			}
		}
		return out
	}
	for _, c := range l.compositions {
		if !goHas(c.parents, e.parent) || !goHas(c.children, child) || c.at.rec == nil {
			continue
		}
		ep := goEdgePoint{point: goPoint{rec: c.at.rec, span: c.call.Span}}
		if (c.kind == "mux" || c.kind == "muxchi") && c.value != nil && c.value.Kind == facts.GoCall {
			if _, _, _, wrapper := l.wrapperCall(c.at, c.value); wrapper {
				tagShape("SH-S18/served-wrapper")
				ep.unknown = append(ep.unknown, goMWUnknown{reason: "the router is mounted through a wrapper that may run first", expression: goArgText(*c.value), file: c.at.rec.file, span: c.call.Span})
			}
		}
		out = append(out, ep)
	}
	return out
}

// usesBefore are the Use calls that apply to obj at p: those on obj
// before p in p's function, and every Use a function called with obj
// before p makes on its parameter (the call runs to its end first). With
// whole, obj is such a parameter and p is unused: every unconditional Use
// in the parameter's own function body applies, unless a return may end
// the body before it.
func (l *goLinker) usesBefore(g *goGraph, obj, skip string, p goPoint, whole bool, seen map[string]bool) goMWPath {
	var out goMWPath
	if seen[obj] {
		return out
	}
	seen[obj] = true
	o := l.objects[obj]
	place := func(rec *goRecord, span contract.Span) string {
		if !whole {
			return placeBefore(rec, span, p)
		}
		if o == nil || o.kind != "param" || rec == nil || rec.file != o.file || rec.fn == nil || rec.fn.Span != o.span {
			return "unknown"
		}
		for _, r := range rec.fn.Returns {
			if goStart(span).after(r.Span) {
				return "return"
			}
		}
		if rec.fn.Jumps {
			return "jump"
		}
		return "before"
	}
	for _, u := range l.groupMW.uses {
		if !goHas(u.objects, obj) {
			continue
		}
		at := place(u.at.rec, u.span)
		switch {
		case at == "after":
			continue
		case len(u.objects) > 1:
			tagShape("SH-S18/order")
			out.unknown = append(out.unknown, useUnknown(u, "Use on a receiver that may hold another router")...)
		case at == "return":
			tagShape("SH-S18/conditional")
			out.unknown = append(out.unknown, useUnknown(u, "conditional registration: a return before it may skip it")...)
		case at == "unknown":
			tagShape("SH-S18/order")
			out.unknown = append(out.unknown, useUnknown(u, "order relative to the route not proven")...)
		case at == "jump" || jumps(u.at.rec):
			tagShape("SH-S18/jump")
			out.unknown = append(out.unknown, useUnknown(u, jumpReason)...)
		case conditional(u.at):
			tagShape("SH-S18/conditional")
			out.unknown = append(out.unknown, useUnknown(u, "conditional registration")...)
		default:
			tagShape("SH-S18/use")
			out.entries = append(out.entries, u.mw...)
		}
	}
	// A parameter bound to obj by a call before p: its Use calls ran.
	for _, b := range l.bindings {
		if b.object != obj || b.param == skip || b.at.rec == nil {
			continue
		}
		at := place(b.at.rec, b.span)
		if at == "after" {
			continue
		}
		callee := l.usesBefore(g, b.param, "", goPoint{}, true, seen)
		out.unknown = append(out.unknown, callee.unknown...)
		if len(callee.entries) == 0 {
			continue
		}
		if at == "before" && !b.conditional && !jumps(b.at.rec) {
			tagShape("SH-S18/call")
			out.entries = append(out.entries, callee.entries...)
			continue
		}
		reason := "order relative to the route not proven"
		switch {
		case b.conditional || at == "return":
			reason = "conditional registration"
		case jumps(b.at.rec):
			tagShape("SH-S18/jump")
			reason = jumpReason
		}
		for _, m := range callee.entries {
			out.unknown = append(out.unknown, goMWUnknown{reason: reason, expression: m.text, file: m.file, span: m.span})
		}
	}
	return out
}

// jumpReason is the unknown entry's reason for a Use (or a call that
// makes one) in a function body holding a goto or a labeled break or
// continue: the jump may skip it (row SH-S18, form jump).
const jumpReason = "conditional registration: a goto or labeled break or continue in the function may skip it"

// jumps reports a record whose body holds a goto or a labeled break or
// continue (payload row RJ-jump).
func jumps(rec *goRecord) bool {
	return rec != nil && rec.fn != nil && rec.fn.Jumps
}

// useUnknown turns a Use call's arguments into unknown entries.
func useUnknown(u goUse, reason string) []goMWUnknown {
	var out []goMWUnknown
	for _, m := range u.mw {
		out = append(out, goMWUnknown{reason: reason, expression: m.text, file: m.file, span: m.span})
	}
	if len(u.mw) == 0 {
		out = append(out, goMWUnknown{reason: reason, file: u.at.rec.file, span: u.span})
	}
	return out
}

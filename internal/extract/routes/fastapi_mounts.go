package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unresolved mounts of FastAPI routes (T029 FU-2, mounts.go): an
// include_router or Starlette mount whose prefix is not a constant, and a
// router whose own prefix= is not one (route_prefix_unresolved), are hops;
// the paths below each are composed from its router as from an app.

// fastapiPrefix is a router's own prefix as read.
type fastapiPrefix struct {
	text, resolution string
	valid            bool
}

// fastapiUnknownMount is one object below a hop. own marks a router whose
// own prefix is the hop: the paths below start without it.
type fastapiUnknownMount struct {
	hop *mountHop
	// from is the app or router whose include_router or mount is the
	// hop ("" for a router's own prefix).
	from        string
	to          string
	own         bool
	conditional bool
	scope       testscope.Scope
}

// pythonScopeName is the name of the function or module scope of a hop.
func pythonScopeName(p *facts.PythonFacts, scope int) string {
	if p == nil || scope <= 0 || scope >= len(p.Scopes) || p.Scopes[scope].Name == "" {
		return "(module)"
	}
	return p.Scopes[scope].Name
}

// fastapiCallHop is the hop of an include_router or mount call (one per
// call site, shared by helper contexts).
func fastapiCallHop(sites map[string]*mountHop, file string, p *facts.PythonFacts, c facts.RouteCall, e facts.RouteExpr) *mountHop {
	site := fmt.Sprintf("%s:%d:%d", file, c.Span.StartLine, c.Span.StartColumn)
	if h := sites[site]; h != nil {
		return h
	}
	prefix, ok := keyword(e, "prefix")
	if c.Method == "mount" || c.Method == "Mount" {
		prefix, ok = argument(e, "path", 0)
	}
	short := facts.CompactPython(c.Receiver.Text) + "." + c.Method + "(...)"
	if ok {
		short = facts.CompactPython(c.Receiver.Text) + "." + c.Method + "(prefix=" + facts.CompactPython(prefix.Text) + ")"
		if c.Method == "mount" {
			short = facts.CompactPython(c.Receiver.Text) + ".mount(" + facts.CompactPython(prefix.Text) + ")"
		}
		if c.Method == "Mount" && e.Object != nil {
			// A Starlette Mount(...) in a routes= list has no receiver.
			short = facts.CompactPython(e.Object.Text) + "(" + facts.CompactPython(prefix.Text) + ")"
		}
	}
	// A Starlette mount hop (the `mount` method or a `Mount(...)` in a
	// routes= list) is row UP-6's `mount(p, ...)` form: the mounted app is
	// consumed, so its routes are below the hop.
	form := upFormInclude
	if c.Method == "mount" || c.Method == "Mount" {
		form = upFormStarlette
	}
	h := &mountHop{file: file, function: pythonScopeName(p, c.Scope), text: short, full: facts.CompactPython(e.Text), span: c.Span, reason: "route_prefix_unresolved", form: form}
	sites[site] = h
	return h
}

// fastapiCtorHop is the hop of a router constructor whose own prefix is
// not a constant.
func fastapiCtorHop(sites map[string]*mountHop, p *facts.PythonFacts, o pythonObject) *mountHop {
	site := fmt.Sprintf("%s:ctor:%d", o.file, o.ctor.Offset)
	if h := sites[site]; h != nil {
		return h
	}
	text := "APIRouter(prefix=" + facts.CompactPython(o.prefix.Text) + ")"
	full := pythonCallText(o.ctor)
	if full == "" {
		full = text
	}
	h := &mountHop{file: o.file, function: pythonScopeName(p, o.scope), text: text, full: full, reason: "route_prefix_unresolved", form: upFormOwnPrefix}
	if o.ctor.Span != nil {
		h.span = *o.ctor.Span
	} else if o.prefix.Span != nil {
		h.span = *o.prefix.Span
	}
	sites[site] = h
	return h
}

// pythonCallText is a call's text rebuilt from its parts (the Python
// facts keep no text for a constructor call): the callee, then every
// argument and keyword, compacted. "" when e is not a call.
func pythonCallText(e facts.RouteExpr) string {
	if e.Text != "" {
		return facts.CompactPython(e.Text)
	}
	if e.Kind != "call" || e.Object == nil || e.Object.Text == "" {
		return ""
	}
	var parts []string
	for _, a := range e.Args {
		parts = append(parts, facts.CompactPython(a.Text))
	}
	for _, k := range e.Keywords {
		if k.Name == "" {
			parts = append(parts, "**"+facts.CompactPython(k.Value.Text))
			continue
		}
		parts = append(parts, k.Name+"="+facts.CompactPython(k.Value.Text))
	}
	return facts.CompactPython(e.Object.Text) + "(" + strings.Join(parts, ",") + ")"
}

// fastapiRelative assigns the hops' keys and composes, per unknown mount,
// the prefixes below it to every object it reaches.
func fastapiRelative(unknown []fastapiUnknownMount, sites map[string]*mountHop, prefixes map[string]fastapiPrefix, adjacent map[string][]mount) []map[string][]pathEntry {
	hops := make([]*mountHop, 0, len(sites))
	for _, h := range sites {
		hops = append(hops, h)
	}
	assignMountKeys(hops)
	out := make([]map[string][]pathEntry, len(unknown))
	for i, u := range unknown {
		into := map[string][]pathEntry{}
		var walk func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool, first bool)
		walk = func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool, first bool) {
			if seen[key] {
				return
			}
			seen[key] = true
			defer delete(seen, key)
			base := prefix
			if !(first && u.own) {
				p := prefixes[key]
				if !p.valid {
					return
				}
				prefix += p.text
				resolution = weaker(resolution, p.resolution)
			}
			into[key] = append(into[key], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Scope: scope})
			for _, m := range adjacent[key] {
				start := prefix
				if m.fromBase {
					start = base
				}
				walk(m.to, start+m.prefix, conditional || m.conditional, weaker(resolution, m.resolution), chainScope(scope, m.scope), seen, false)
			}
		}
		walk(u.to, "", u.conditional, "literal", u.scope, map[string]bool{}, true)
		out[i] = into
	}
	return out
}

// fastapiHopsAbove sets each hop's known prefixes above it, when every
// chain to the hop is proven (MountCoversHop): for an include_router or
// mount, the paths of the object it is called on; for a router's own
// prefix, the paths of each includer followed by the include prefix. A
// hop on an object below another unknown hop, or a router also included
// at an unknown prefix, covers the whole prefix from the root.
func fastapiHopsAbove(unknown []fastapiUnknownMount, relative []map[string][]pathEntry, paths map[string][]pathEntry, mounts []mount) map[*mountHop][]pathEntry {
	byHop := map[*mountHop][]int{}
	var order []*mountHop
	for i, u := range unknown {
		if byHop[u.hop] == nil {
			order = append(order, u.hop)
		}
		byHop[u.hop] = append(byHop[u.hop], i)
	}
	belowOther := func(object string, own int) bool {
		for j, rel := range relative {
			if j == own {
				continue
			}
			if _, ok := rel[object]; ok {
				return true
			}
		}
		for _, u := range unknown {
			if u.from != "" && u.to == object {
				return true
			}
		}
		return false
	}
	above := map[*mountHop][]pathEntry{}
	for _, h := range order {
		var list []pathEntry
		proven := true
		for _, i := range byHop[h] {
			u := unknown[i]
			if !u.own {
				if belowOther(u.from, -1) || len(paths[u.from]) == 0 {
					proven = false
					break
				}
				list = append(list, paths[u.from]...)
				continue
			}
			if belowOther(u.to, i) {
				proven = false
				break
			}
			for _, m := range mounts {
				if m.to != u.to {
					continue
				}
				if belowOther(m.from, -1) || len(paths[m.from]) == 0 {
					proven = false
					break
				}
				for _, p := range paths[m.from] {
					p.Template += m.prefix
					p.Conditional = p.Conditional || m.conditional
					p.Resolution = weaker(p.Resolution, m.resolution)
					p.Scope = chainScope(p.Scope, m.scope)
					list = append(list, p)
				}
			}
		}
		if !proven || len(list) == 0 {
			h.covers = MountCoversRoot
			continue
		}
		var texts []string
		for _, p := range list {
			texts = append(texts, p.Template)
		}
		h.covers, h.chain = MountCoversHop, aboveChain(texts)
		above[h] = list
	}
	return above
}

// fastapiMountEntries are a pathless registration's unresolved mounts.
// A route whose raw path below the hop is empty (pattern "" right at the
// hop) is not stored: its path is the prefix alone, without the trailing
// slash rule a template carries.
func fastapiMountEntries(unknown []fastapiUnknownMount, relative []map[string][]pathEntry, above map[*mountHop][]pathEntry, objects []string, pattern string, registrationScope testscope.Scope) []mountEntry {
	byKey := map[string]int{}
	var out []mountEntry
	for i, u := range unknown {
		var bases []pathEntry
		for _, o := range objects {
			bases = append(bases, relative[i][o]...)
		}
		if len(bases) == 0 {
			continue
		}
		unique := map[fastapiPathKey]fastapiPathEntry{}
		if at, ok := byKey[u.hop.key]; ok {
			for _, e := range out[at].Paths.([]fastapiPathEntry) {
				unique[fastapiPathKey{e.Above + "\x00" + e.Template, e.TrailingSlash}] = e
			}
		}
		tops := above[u.hop]
		if u.hop.covers != MountCoversHop {
			tops = []pathEntry{{Resolution: "literal"}}
		}
		for _, top := range tops {
			for _, base := range bases {
				raw := base.Template + pattern
				if raw == "" || !strings.HasPrefix(raw, "/") {
					continue
				}
				base.Scope = chainScope(chainScope(base.Scope, top.Scope), registrationScope)
				base.Conditional = base.Conditional || top.Conditional
				base.Resolution = weaker(base.Resolution, top.Resolution)
				base.Template = pythonNormalize(raw)
				base.Above = top.Template
				p := fastapiPathEntry{pathEntry: base, TrailingSlash: strings.HasSuffix(raw, "/") && base.Template != "/"}
				key := fastapiPathKey{p.Above + "\x00" + p.Template, p.TrailingSlash}
				if old, found := unique[key]; found {
					p.Conditional = p.Conditional && old.Conditional
					p.Scope = mergedScope(p.Scope, old.Scope)
					if strength(old.Resolution) > strength(p.Resolution) {
						p.Resolution = old.Resolution
					}
				}
				unique[key] = p
			}
		}
		if len(unique) == 0 {
			continue
		}
		entries := make([]fastapiPathEntry, 0, len(unique))
		for _, p := range unique {
			entries = append(entries, p)
		}
		sort.Slice(entries, func(a, b int) bool {
			if entries[a].Above != entries[b].Above {
				return entries[a].Above < entries[b].Above
			}
			if entries[a].Template != entries[b].Template {
				return entries[a].Template < entries[b].Template
			}
			return !entries[a].TrailingSlash && entries[b].TrailingSlash
		})
		if at, ok := byKey[u.hop.key]; ok {
			out[at].Paths = entries
			continue
		}
		byKey[u.hop.key] = len(out)
		out = append(out, newMountEntry(u.hop, entries, ""))
	}
	return sortMountEntries(out)
}

// starletteMountNames are the callees a Mount(...) call must resolve to:
// Starlette's routing.Mount, which fastapi.routing re-exports.
var starletteMountNames = map[string]bool{
	"starlette.routing.Mount": true,
	"fastapi.routing.Mount":   true,
}

// starletteMount reads a Mount(...) call (T035). A proven FastAPI app it
// mounts is consumed: it answers only below the mount. Written directly
// in the routes= list literal of a FastAPI(...) constructor bound outside
// a helper context, the mount composes: ok is true and the
// caller mounts target on owner at prefix, as app.mount would (FastAPI
// passes routes= to its APIRouter, whose routes Starlette serves first).
// Any other position (an APIRouter's routes, a list built elsewhere) is
// not composed, so the app's routes keep no path and the call is named
// in route_mount_unresolved.
func (r *fastapiResolver) starletteMount(file string, syntax facts.PythonRoute, instance helperInstance, consume func(pythonObject, testscope.Scope), diagnostic func(file, code, severity, expression string, s contract.Span)) (owner, target pythonObject, prefix facts.RouteExpr, ok bool) {
	c, e := syntax.Call, syntax.Expression
	if e.Object == nil {
		return owner, target, prefix, false
	}
	b := r.modules.Expression(file, c.Scope, *e.Object)
	if !b.External || !starletteMountNames[fullName(b)] {
		return owner, target, prefix, false
	}
	targetExpr, found := argument(e, "app", 1)
	if !found {
		return owner, target, prefix, false
	}
	target = r.object(file, c.Scope, targetExpr, map[string]bool{})
	if target.kind != "app" {
		return owner, target, prefix, false
	}
	consume(target, chainScope(testscope.Classify(file, r.rules), instance.scopeLabel))
	if syntax.InRoutesList && r.context == "" {
		owner = r.objects[fmt.Sprintf("%s:%d", file, syntax.RoutesCall)]
		prefix, found = argument(e, "path", 0)
		if owner.kind == "app" && found {
			return owner, target, prefix, true
		}
	}
	diagnostic(file, "route_mount_unresolved", "info", facts.CompactPython(e.Text), c.Span)
	return owner, target, prefix, false
}

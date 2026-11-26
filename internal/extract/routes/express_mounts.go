package routes

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unresolved mounts of Express routes (T029 FU-2, mounts.go): a `use`
// whose prefix is not a constant (route_prefix_unresolved) is a hop; the
// routers it mounts compose the paths below it as roots would.

// expressUnknownMount is one router mounted at a hop.
type expressUnknownMount struct {
	hop *mountHop
	// from is the app or router the `use` is called on.
	from        string
	to          string
	conditional bool
	scope       testscope.Scope
}

// ownerName is the enclosing declaration's name of a call, "(module)" at
// module level.
func ownerName(c facts.RouteCall) string {
	if c.OwnerName == "" {
		return "(module)"
	}
	return c.OwnerName
}

// useText is a `use` call without whitespace, function arguments
// abbreviated, as hops and diagnostics name it.
func useText(c facts.RouteCall) string {
	parts := []string{}
	for _, a := range c.Args {
		if a.Kind == "arrow_function" || a.Kind == "function_expression" {
			parts = append(parts, "function(...){...}")
		} else {
			parts = append(parts, whitespace(a.Text))
		}
	}
	return whitespace(c.Receiver.Text) + ".use(" + strings.Join(parts, ",") + ")"
}

// unknownMount records the routers and apps a `use` with an unread prefix
// mounts; each app is consumed (it answers only below the hop). An app
// first created by resolving an argument is dropped, as before: it has
// no binding a route could be registered on.
func (r *resolver) unknownMount(file string, c facts.RouteCall, from string, rules testscope.Rules, consume func(key, file string)) []expressUnknownMount {
	before := map[string]bool{}
	for key := range r.objects {
		before[key] = true
	}
	receiver := whitespace(c.Receiver.Text)
	hop := &mountHop{file: file, function: ownerName(c), text: receiver + ".use(" + whitespace(c.Args[0].Text) + ")", full: useText(c), span: c.Span, reason: "route_prefix_unresolved"}
	var out []expressUnknownMount
	for _, argument := range c.Args[1:] {
		target := r.resolve(file, c.Scope, argument, 0, nil)
		if target.kind == "app" && !before[target.key] {
			delete(r.objects, target.key)
			delete(r.objectFiles, target.key)
			continue
		}
		if target.kind == "app" {
			consume(target.key, file)
		}
		if target.kind != "router" && target.kind != "app" {
			continue
		}
		out = append(out, expressUnknownMount{hop: hop, from: from, to: target.key, conditional: c.Conditional, scope: testscope.Classify(file, rules)})
	}
	return out
}

// consumeUnread consumes every app a `use` on an object no row reads
// mounts (`holder.server.use("/raw", raw)`, a parameter's `use`): the
// app answers only below that mount, whose path is unknown. It reports
// whether the call mounts an app. An app first created by resolving an
// argument is dropped, as in unknownMount.
func (r *resolver) consumeUnread(file string, c facts.RouteCall, consume func(key, file string)) bool {
	before := map[string]bool{}
	for key := range r.objects {
		before[key] = true
	}
	found := false
	for _, argument := range c.Args {
		target := r.resolve(file, c.Scope, argument, 0, nil)
		if target.kind != "app" {
			continue
		}
		if !before[target.key] {
			delete(r.objects, target.key)
			delete(r.objectFiles, target.key)
			continue
		}
		consume(target.key, file)
		found = true
	}
	return found
}

// expressHopsAbove sets each hop's known prefixes above it: the paths of
// the app or router its `use` is called on, when every chain to it is
// proven (none passes a prefix no row reads or another unknown hop); the
// value then covers only the hop's own part (MountCoversHop).
func expressHopsAbove(unknown []expressUnknownMount, relative []map[string][]pathEntry, paths map[string][]pathEntry, blocked map[string][]string) map[*mountHop][]pathEntry {
	above := map[*mountHop][]pathEntry{}
	below := map[string]bool{}
	for _, rel := range relative {
		for object := range rel {
			below[object] = true
		}
	}
	done := map[*mountHop]bool{}
	for _, u := range unknown {
		if done[u.hop] {
			continue
		}
		done[u.hop] = true
		list := paths[u.from]
		if len(list) == 0 || below[u.from] || len(blocked[u.from]) > 0 {
			u.hop.covers = MountCoversRoot
			continue
		}
		var texts []string
		var entries []pathEntry
		for _, p := range list {
			p.Template = strings.TrimSuffix(slashes(p.Template), "/")
			entries = append(entries, p)
			texts = append(texts, p.Template)
		}
		u.hop.covers = MountCoversHop
		above[u.hop] = entries
		u.hop.chain = aboveChain(texts)
	}
	return above
}

// expressMountEntries are a pathless registration's unresolved mounts:
// the registration's pattern after every path below each hop that reaches
// its router, with the known prefix above the hop (above) when it has
// one.
func expressMountEntries(unknown []expressUnknownMount, relative []map[string][]pathEntry, above map[*mountHop][]pathEntry, object string, read pathReading, registrationScope testscope.Scope) []mountEntry {
	byKey := map[string]int{}
	var out []mountEntry
	for i, u := range unknown {
		bases := relative[i][object]
		if len(bases) == 0 {
			continue
		}
		var entries []pathEntry
		if at, ok := byKey[u.hop.key]; ok {
			entries = out[at].Paths.([]pathEntry)
		}
		unique := map[string]int{}
		for j, e := range entries {
			unique[e.Above+"\x00"+e.Template] = j
		}
		tops := above[u.hop]
		if u.hop.covers != MountCoversHop {
			tops = []pathEntry{{Resolution: "literal"}}
		}
		for _, top := range tops {
			for _, p := range bases {
				for _, alternative := range read.alternatives {
					entry := pathEntry{Template: slashes(p.Template + "/" + alternative), Conditional: p.Conditional || top.Conditional, Resolution: weaker(p.Resolution, top.Resolution), Optional: p.Optional || top.Optional || read.optional, Scope: chainScope(chainScope(p.Scope, top.Scope), registrationScope), Above: top.Template}
					k := entry.Above + "\x00" + entry.Template
					if at, found := unique[k]; found {
						entries[at].Conditional = entries[at].Conditional && entry.Conditional
						entries[at].Scope = mergedScope(entries[at].Scope, entry.Scope)
						if strength(entry.Resolution) > strength(entries[at].Resolution) {
							entries[at].Resolution = entry.Resolution
						}
						entries[at].Optional = entries[at].Optional || entry.Optional
						continue
					}
					unique[k] = len(entries)
					entries = append(entries, entry)
				}
			}
		}
		sort.Slice(entries, func(a, b int) bool {
			if entries[a].Above != entries[b].Above {
				return entries[a].Above < entries[b].Above
			}
			return entries[a].Template < entries[b].Template
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

// belowAnyHop reports whether any of the objects is reached through a
// hop (relative holds, per hop, the prefixes below it of every object it
// reaches; Express, FastAPI and Axum).
func belowAnyHop(relative []map[string][]pathEntry, objects ...string) bool {
	for _, rel := range relative {
		for _, o := range objects {
			if len(rel[o]) > 0 {
				return true
			}
		}
	}
	return false
}

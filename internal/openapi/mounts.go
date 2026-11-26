package openapi

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routes"
)

// Unresolved mounts (rows OG-94..OG-96): a route stored without a path
// carries, per hop where composition stopped, the hop's key and the paths
// below it (route attribute unresolved_mounts). A value set by you for the
// hop's key completes the path: with value_covers "hop" it is the part
// the hop adds, placed between the known prefix above the hop and the path
// below it; with "nothing" the paths are the served paths and only "/" is
// accepted; with "root" it is the whole prefix from the root.

// What a mount value states (Mount.Covers, row OG-95), as the linkers
// store it.
const (
	MountCoversHop     = routes.MountCoversHop
	MountCoversNothing = routes.MountCoversNothing
	MountCoversRoot    = routes.MountCoversRoot
)

// mountInfo is one stored unresolved mount of a route.
type mountInfo struct {
	key, registration, reason, hop, file, shadowing, covers string
	line                                                    int
	paths                                                   []pathEntry
}

// readMounts reads the route attribute unresolved_mounts.
func readMounts(v any) []mountInfo {
	var out []mountInfo
	for _, m := range objects(v) {
		mi := mountInfo{paths: readEntries(m["paths"])}
		mi.key, _ = m["key"].(string)
		mi.registration, _ = m["registration"].(string)
		mi.reason, _ = m["reason"].(string)
		mi.hop, _ = m["hop"].(string)
		mi.file, _ = m["file"].(string)
		mi.shadowing, _ = m["shadowing"].(string)
		mi.covers, _ = m["value_covers"].(string)
		if mi.covers != routes.MountCoversHop && mi.covers != routes.MountCoversNothing {
			mi.covers = routes.MountCoversRoot
		}
		mi.line = intValue(m["line"])
		if mi.key != "" {
			out = append(out, mi)
		}
	}
	return out
}

// joinMount places a path below a hop: the known prefix above the hop,
// then the value ("/" adds nothing), then the path below it. The path ""
// below a hop is the prefix itself; the path "/" is the prefix with a
// trailing slash, except for Express (which ignores it).
func joinMount(above, value, template string, slash bool, framework string) (string, bool) {
	prefix := strings.TrimSuffix(above, "/")
	if value != "/" {
		prefix += value
	}
	switch template {
	case "":
		if prefix == "" {
			return "/", false
		}
		return prefix, false
	case "/":
		if prefix == "" {
			return "/", false
		}
		return prefix, framework != "express"
	}
	return prefix + template, slash
}

// collectMounts lists the hops of the production routes (doc.Mounts) and
// counts the keys per hop hash (two keys with one hash: ambiguous).
func (g *generator) collectMounts(list []*routeInfo) {
	keysByHash := map[string]map[string]bool{}
	byKey := map[string]*Mount{}
	examples := map[string]map[MountExample]bool{}
	above := map[string]map[string]bool{}
	for _, r := range list {
		for _, m := range r.mounts {
			if keysByHash[m.registration] == nil {
				keysByHash[m.registration] = map[string]bool{}
			}
			keysByHash[m.registration][m.key] = true
			mt := byKey[m.key]
			if mt == nil {
				mt = &Mount{Key: m.key, Hop: m.hop, File: m.file, Line: m.line, Reason: m.reason, Registration: m.registration, ShadowingNotChecked: m.shadowing == "not_checked", Examples: []MountExample{}, Covers: m.covers, Above: []string{}, Framework: r.framework}
				byKey[m.key] = mt
				examples[m.key] = map[MountExample]bool{}
				above[m.key] = map[string]bool{}
			}
			mt.Routes++
			for _, e := range m.paths {
				if !e.test && e.template != nil {
					examples[m.key][MountExample{Method: r.method, Above: strings.TrimSuffix(e.above, "/"), Path: withSlash(*e.template, e.trailingSlash)}] = true
					if m.covers == routes.MountCoversHop {
						above[m.key][strings.TrimSuffix(e.above, "/")] = true
					}
				}
			}
		}
	}
	g.hopKeys = map[string]int{}
	for hash, keys := range keysByHash {
		g.hopKeys[hash] = len(keys)
	}
	for key, mt := range byKey {
		mt.Ambiguous = g.hopKeys[mt.Registration] > 1
		list := make([]MountExample, 0, len(examples[key]))
		for e := range examples[key] {
			list = append(list, e)
		}
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if a.Above != b.Above {
				return a.Above < b.Above
			}
			if a.Path != b.Path {
				return a.Path < b.Path
			}
			return a.Method < b.Method
		})
		if len(list) > 3 {
			list = list[:3]
		}
		mt.Examples = list
		for a := range above[key] {
			mt.Above = append(mt.Above, a)
		}
		sort.Strings(mt.Above)
		g.doc.Mounts = append(g.doc.Mounts, *mt)
	}
	sort.Slice(g.doc.Mounts, func(i, j int) bool { return g.doc.Mounts[i].Key < g.doc.Mounts[j].Key })
}

// mountCandidates are a pathless route's candidates from the values set
// for its hops (OG-95): each used hop's paths below it after the value.
func (g *generator) mountCandidates(r *routeInfo) []candidate {
	var out []candidate
	seen := map[string]bool{}
	for _, m := range r.mounts {
		v := g.values[m.key]
		if v == nil {
			continue
		}
		switch {
		case g.hopKeys[m.registration] > 1:
			v.Why = WhyMountTwin
			continue
		case v.Registration != "" && v.Registration != m.registration:
			v.Why = WhyMountChanged
			continue
		}
		if m.covers == routes.MountCoversNothing && v.Value != "/" {
			v.Why = WhyMountNothing
			continue
		}
		v.Status, v.Why = UserValueUsed, ""
		for _, e := range m.paths {
			if e.test || e.template == nil {
				continue
			}
			template, slash := joinMount(e.above, v.Value, *e.template, e.trailingSlash, r.framework)
			key := withSlash(template, slash)
			if seen[key] {
				continue
			}
			seen[key] = true
			as := append(r.routeAssumptions(), Assumption{ID: SetByYou, From: "graphvein.yaml explorer.path_values", Key: m.key, Value: v.Value, Text: mountAssumption(m, e.above)})
			for _, id := range e.assumptions {
				as = append(as, Assumption{ID: id, From: "path_entry"})
			}
			out = append(out, candidate{route: r, key: key, template: template, resolution: SetByYou, conditional: e.conditional, assumptions: as,
				userKey: m.key, optional: e.optional, mount: true, shadowing: m.shadowing})
		}
	}
	return out
}

// mountAssumption is the text of a mount value's assumption (OG-95).
func mountAssumption(m mountInfo, above string) string {
	hop := m.hop + " (" + m.file + ")"
	switch m.covers {
	case routes.MountCoversHop:
		at := "at the root"
		if a := strings.TrimSuffix(above, "/"); a != "" {
			at = "after the prefix " + a + " read above it"
		}
		return "the part " + hop + " adds to the path is set by you: you state that it passes requests on with this prefix, " + at + "; Graphvein did not read this part"
	case routes.MountCoversNothing:
		return "you state that " + hop + " passes requests on unchanged: Graphvein read that it adds no prefix, but did not prove that it passes them on"
	}
	return "the whole prefix of the routes below " + hop + " is set by you, from the root: the routers above this hop are not proven either; Graphvein did not read it"
}

// mountAttribute is the route attribute the generator reads (OG-94).
const mountAttribute = routes.MountAttribute

// intValue reads a stored number: the store decodes attributes with
// json.Number, a plain decode gives float64.
func intValue(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

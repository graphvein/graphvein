package routes

import (
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unresolved mounts of Axum routes (T029 FU-3, mounts.go): a `nest` whose
// prefix is not a constant (route_prefix_unresolved) is a hop; the router
// it nests composes the paths below it through the same nest and merge
// rows as a root would.
//
// The routes below an Axum hop are marked shadowing "not_checked", as Go's
// are: the routes beside the hop on the parent router are not compared
// with the placed paths.
//
// The root path of the nested router (a `/` route, or a `/` route of a
// router merged into it) is the hop's prefix itself without a trailing
// slash (axum 0.6.20, 0.7.9 and 0.8.8 answer `/api`, not `/api/`, for
// `nest("/api", Router::new().route("/", h))`; the scratch oracle of the
// T029 FU-3 task record (internal)). That path, and paths with a trailing
// slash (axum_slash.go), are stored only when the crates' axum version is
// proven within the checked versions (rows SH-X11, SH-X25); otherwise they
// are left out (unknown).

// axumUnknownMount is the router nested at one hop.
type axumUnknownMount struct {
	hop *mountHop
	// from is the router the nest is called on.
	from string
	// service marks a nest_service hop: the nested router's `/` is
	// answered at the prefix and at the prefix with a trailing slash.
	service     bool
	to          string
	conditional bool
	scope       testscope.Scope
}

// axumHop builds the hop of a nest call e in function owner.
func axumHop(file, owner string, e facts.RustExpr) *mountHop {
	function := "(file)"
	if _, _, q, err := contract.ParseIdentity(owner); err == nil && q != "" {
		function = q
	}
	receiver := ""
	if e.Object != nil && e.Object.Kind == "path" {
		receiver = whitespace(e.Object.Text)
	}
	first := ""
	parts := make([]string, 0, len(e.Args))
	for i, a := range e.Args {
		if i == 0 {
			first = whitespace(a.Text)
		}
		parts = append(parts, axumHopArgument(a))
	}
	form := upFormNest
	if e.Name == "nest_service" {
		form = upFormNestService
	}
	return &mountHop{file: file, function: function, text: receiver + "." + e.Name + "(" + first + ")", full: receiver + "." + e.Name + "(" + strings.Join(parts, ",") + ")", span: e.Span, reason: "route_prefix_unresolved", form: form}
}

// axumHopArgument is an argument's text in the hop's hash: a closure's
// body and an inline router chain are elided, so a route added inside
// them does not change the hop's registration.
func axumHopArgument(a facts.RustExpr) string {
	switch a.Kind {
	case "closure_expression":
		return "|..|{...}"
	case "method":
		root := a
		for root.Kind == "method" && root.Object != nil {
			root = *root.Object
		}
		return whitespace(root.Text) + "…"
	}
	return whitespace(a.Text)
}

// unknownNest records a nest whose prefix is not a constant.
func (r *axumLinker) unknownNest(file, owner string, e facts.RustExpr, from, target string, conditional bool) {
	r.unknown = append(r.unknown, axumUnknownMount{hop: axumHop(file, owner, e), from: from, service: e.Name == "nest_service", to: target, conditional: conditional, scope: testscope.Classify(file, r.rules)})
}

// axumRelative assigns the hops' keys and composes, per hop, the prefixes
// below it to every router it reaches (as visit does from a root).
func axumRelative(unknown []axumUnknownMount, adjacent map[string][]mount) []map[string][]pathEntry {
	hops := make([]*mountHop, 0, len(unknown))
	for _, u := range unknown {
		hops = append(hops, u.hop)
	}
	assignMountKeys(hops)
	out := make([]map[string][]pathEntry, len(unknown))
	for i, u := range unknown {
		into := map[string][]pathEntry{}
		var walk func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool)
		walk = func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool) {
			if seen[key] {
				return
			}
			seen[key] = true
			defer delete(seen, key)
			into[key] = append(into[key], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Scope: scope})
			for _, m := range adjacent[key] {
				walk(m.to, axumJoinPrefix(prefix, m.prefix), conditional || m.conditional, weaker(resolution, m.resolution), chainScope(scope, m.scope), seen)
			}
		}
		walk(u.to, "", u.conditional, "literal", u.scope, map[string]bool{})
		out[i] = into
	}
	return out
}

// axumHopsAbove sets each hop's known prefixes above it: the paths of the
// router its nest is called on, when that router has a path and is not
// itself below an unknown hop, and no prefix above holds a trailing or
// doubled slash; the value then covers only the hop's own part
// (MountCoversHop). Otherwise the value is the whole prefix from the root
// (MountCoversRoot). An Axum nest always adds a part, so never
// MountCoversNothing.
func axumHopsAbove(unknown []axumUnknownMount, relative []map[string][]pathEntry, paths map[string][]pathEntry) map[*mountHop][]pathEntry {
	above := map[*mountHop][]pathEntry{}
	below := map[string]bool{}
	for _, rel := range relative {
		for object := range rel {
			below[object] = true
		}
	}
	for _, u := range unknown {
		u.hop.covers = MountCoversRoot
		list := paths[u.from]
		if len(list) == 0 || below[u.from] {
			continue
		}
		var texts []string
		var entries []pathEntry
		proven := true
		for _, p := range list {
			prefixes := axumPrefixes(p.Template)
			if axumSlashForm(prefixes, "") {
				proven = false
				break
			}
			p.Template = strings.TrimSuffix(axumNormalize(strings.Join(prefixes, "")), "/")
			entries = append(entries, p)
			texts = append(texts, p.Template)
		}
		if !proven {
			continue
		}
		u.hop.covers = MountCoversHop
		above[u.hop] = entries
		u.hop.chain = aboveChain(texts)
	}
	return above
}

// axumMountEntries are a pathless registration's unresolved mounts: its
// pattern after every prefix below each hop that reaches its router,
// normalised as the route's own paths are, with the known prefix above
// the hop (above) when it has one. The nested router's `/` is stored as
// "" (the prefix itself, without a trailing slash).
func (r *axumLinker) axumMountEntries(relative []map[string][]pathEntry, above map[*mountHop][]pathEntry, reg axumRegistration, pattern, patternResolution string, registrationScope testscope.Scope, versions *axumVersions) []mountEntry {
	byKey := map[string]int{}
	var out []mountEntry
	for i, u := range r.unknown {
		bases := relative[i][reg.object.key]
		if len(bases) == 0 {
			continue
		}
		proven := axumSlashProven(versions.of(u.hop.file)) && axumSlashProven(versions.of(reg.file))
		unique := map[axumMountKey]fastapiPathEntry{}
		if at, ok := byKey[u.hop.key]; ok {
			for _, e := range out[at].Paths.([]fastapiPathEntry) {
				unique[axumMountKey{e.Above, e.Template, e.TrailingSlash}] = e
			}
		}
		tops := above[u.hop]
		if u.hop.covers != MountCoversHop {
			tops = []pathEntry{{Resolution: "literal"}}
		}
		for _, top := range tops {
			for _, base := range bases {
				prefixes := axumPrefixes(base.Template)
				if len(prefixes) == 0 && pattern == "/" && !proven {
					// The nested router's root is the hop's prefix without
					// a slash only on the checked versions.
					continue
				}
				formed, ok := axumPaths(base.Template, pattern, proven)
				if !ok {
					continue
				}
				if len(prefixes) == 0 && pattern == "/" {
					// The prefix itself; a nest_service hop also answers
					// the prefix with a trailing slash ("/").
					formed = []axumFormed{{"", false}}
					if u.service {
						formed = append(formed, axumFormed{"/", false})
					}
				}
				for _, f := range formed {
					p := fastapiPathEntry{pathEntry: base, TrailingSlash: f.slash}
					p.Scope = chainScope(chainScope(p.Scope, top.Scope), registrationScope)
					p.Conditional = p.Conditional || top.Conditional
					p.Resolution = weaker(p.Resolution, top.Resolution)
					p.Above = top.Template
					p.Template = f.template
					p.Resolution = weaker(p.Resolution, patternResolution)
					p.Conditional = p.Conditional || reg.object.conditional
					key := axumMountKey{p.Above, p.Template, p.TrailingSlash}
					if old, ok := unique[key]; ok {
						p.Conditional = p.Conditional && old.Conditional
						p.Scope = mergedScope(p.Scope, old.Scope)
						if strength(old.Resolution) > strength(p.Resolution) {
							p.Resolution = old.Resolution
						}
					}
					unique[key] = p
				}
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
		out = append(out, newMountEntry(u.hop, entries, "not_checked"))
	}
	return sortMountEntries(out)
}

// axumMountKey tells a stored path below a hop apart: the prefix above
// the hop, the template and its trailing slash.
type axumMountKey struct {
	above, template string
	slash           bool
}

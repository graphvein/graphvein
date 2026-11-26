package routes

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unknown-prefix paths (task T033, ADR 0084, claim contract
// docs/contract/claims/unknown-prefix-paths.md): a route whose prefix the
// code does not fix keeps an empty "paths" (full composed paths only) and
// gains, in the route attribute unresolved_prefix_paths, the part of its
// path that is proven, with the prefix stated as unknown. The entries are
// projected from the route's unresolved mounts (mounts.go), so they never
// claim more than the hops already store; only the client matcher's
// fallback tier reads them.

// UnknownPrefixAttribute is the route attribute holding the unknown-prefix
// path entries.
const UnknownPrefixAttribute = "unresolved_prefix_paths"

// unknownPrefixValue is the stored "prefix" of every entry: the part of
// the path before "above" + template is not read from code.
const unknownPrefixValue = "unresolved"

// unknownPrefixEntry is one stored unknown-prefix path of a route. The
// fields after Hop are those of the framework's path entry, so a reader
// treats them as it treats "paths" entries.
type unknownPrefixEntry struct {
	Template string `json:"template"`
	Prefix   string `json:"prefix"`
	// PrefixReason is the reason of the hop the entry comes from
	// (route_prefix_unresolved or route_mount_unresolved).
	PrefixReason string `json:"prefix_reason"`
	// Hop is the unresolved_mounts key of that hop; nil when no hop is
	// stored (a generated server whose BaseURL is not one constant).
	Hop *string `json:"hop"`
	// Above is the known prefix above the hop (value_covers "hop").
	Above         string          `json:"above,omitempty"`
	TrailingSlash bool            `json:"trailing_slash,omitempty"`
	Conditional   bool            `json:"conditional"`
	Resolution    string          `json:"resolution"`
	Optional      bool            `json:"optional,omitempty"`
	Scope         testscope.Scope `json:"scope,omitempty"`
	Assumptions   []string        `json:"assumptions,omitempty"`
	ServedThrough []string        `json:"served_through,omitempty"`
	// MayAnswerFirst names the earlier entries of RO-V1 and RO-V2.
	MayAnswerFirst []string `json:"may_answer_first,omitempty"`
}

// mergeUnknownPrefixEntries is row UP-8: entries deduplicated by (above,
// template, trailing slash) across hops, keeping the first entry's hop and
// reason, merged as path entries are (conditional only when every source
// is, test scope only when every source is, the strongest resolution; an
// assumption only when every source states it), sorted by above, then
// template, the form without a trailing slash first.
func mergeUnknownPrefixEntries(list []unknownPrefixEntry) []unknownPrefixEntry {
	type key struct {
		above, template string
		slash           bool
	}
	at := map[key]int{}
	var out []unknownPrefixEntry
	for _, e := range list {
		k := key{e.Above, e.Template, e.TrailingSlash}
		i, found := at[k]
		if !found {
			at[k] = len(out)
			out = append(out, e)
			continue
		}
		old := &out[i]
		old.Conditional = old.Conditional && e.Conditional
		old.Scope = mergedScope(old.Scope, e.Scope)
		if strength(e.Resolution) > strength(old.Resolution) {
			old.Resolution = e.Resolution
		}
		old.Optional = old.Optional && e.Optional
		if len(old.Assumptions) == 0 || len(e.Assumptions) == 0 {
			old.Assumptions, old.ServedThrough, old.MayAnswerFirst = nil, nil, nil
		} else {
			old.Assumptions = goUnion(old.Assumptions, e.Assumptions)
			old.ServedThrough = goUnion(old.ServedThrough, e.ServedThrough)
			old.MayAnswerFirst = goUnion(old.MayAnswerFirst, e.MayAnswerFirst)
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].Above != out[b].Above {
			return out[a].Above < out[b].Above
		}
		if out[a].Template != out[b].Template {
			return out[a].Template < out[b].Template
		}
		return !out[a].TrailingSlash && out[b].TrailingSlash
	})
	return out
}

// unknownPrefixTag is a test-only hook: every branch of the Express,
// FastAPI and Axum projection reports its row ("<row>" or
// "<row>/<form>", rows of unknownPrefixContract). The Go linker reports
// through its own tag function (goLinker.upTag).
var unknownPrefixTag func(row string)

func tagUnknownPrefix(row string) {
	if unknownPrefixTag != nil {
		unknownPrefixTag(row)
	}
}

// Hop forms of the FastAPI and Axum hops (mountHop.form), named in rows
// UP-6 and UP-7. An Express `use` hop has no form (UP-5 names one). A
// FastAPI `mount(p, ...)` hop (the `mount` method or a Starlette
// `Mount(p, ...)` in a `routes=` list) reaches entries since the mounted
// app is consumed (no root of its own, T035).
const (
	upFormInclude     = "`include_router(r, prefix=p)`"
	upFormStarlette   = "`mount(p, ...)`"
	upFormOwnPrefix   = "own `prefix=`"
	upFormNest        = "`nest(p, r)`"
	upFormNestService = "`nest_service(p, s)`"
)

// Forms of D the Express, FastAPI and Axum projection tags.
const (
	// upDefaultUnread: a pathless Express route whose own pattern, or a
	// mount prefix on a chain to it, no row reads (route_pattern_unmodelled
	// and the other refusals of the Express path rows): UP-5's refusal.
	upDefaultUnread = "unread_pattern"
	// upDefaultNotStored: a route below a hop whose path below it is not
	// stored (a FastAPI pattern "" right at the hop; an Axum nested `/`
	// or trailing-slash path on a crate whose axum version is not proven).
	upDefaultNotStored = "not_stored"
	// upDefaultSlash: an Axum route whose every path was left out because
	// its trailing slash is not proven (route_trailing_slash_unproven).
	upDefaultSlash = "slash_unproven"
)

// upWebDefaultForms are the D forms of step UP-A2.
var upWebDefaultForms = []string{upDefaultUnread, upDefaultNotStored, upDefaultSlash}

// upRouteState is what a framework linker knows about a route when it
// decides its unknown-prefix entries.
type upRouteState struct {
	// file, method and pattern name the route in a failure diagnostic.
	file, method, pattern string
	// hasPaths: the route has at least one usable production paths entry
	// (UP-9); testPaths: it has usable paths entries, all test scope (UP-9
	// applies per scope: a test harness mount never removes a production
	// entry).
	hasPaths, testPaths bool
	// refused names the D form when the route's own state refuses every
	// entry (upDefaultUnread, upDefaultSlash); "" otherwise.
	refused string
	// belowHop: the route's router is reached through at least one hop
	// (whether or not a path below it is stored).
	belowHop bool
	// mounts are the route's stored unresolved mounts.
	mounts []mountEntry
	// hops finds a hop by its key, for its form.
	hops map[string]*mountHop
}

// safeWebUnknownPrefix isolates one route's projection: a failure is a
// diagnostic and the route keeps no entries.
func safeWebUnknownPrefix(row string, s upRouteState) (out []unknownPrefixEntry, problem *contract.Diagnostic) {
	defer func() {
		if v := recover(); v != nil {
			out = nil
			problem = &contract.Diagnostic{Type: "diagnostic", File: s.file, Severity: "warning", Code: "route_unknown_prefix_failed", Message: fmt.Sprintf("%s: unknown-prefix paths of %s %q failed: %v", s.file, s.method, s.pattern, v)}
		}
	}()
	return webUnknownPrefix(row, s), nil
}

// webUnknownPrefix decides an Express (row UP-5), FastAPI (UP-6) or Axum
// (UP-7) route's unresolved_prefix_paths. Every hop of these frameworks
// is a prefix that is not a constant (route_prefix_unresolved); the
// entries are the paths stored below each hop, one per path and hop,
// merged by UP-8. Every other pathless route gets none.
func webUnknownPrefix(row string, s upRouteState) []unknownPrefixEntry {
	switch {
	case s.hasPaths:
		// UP-9: a known path is never weakened by an unknown one.
		tagUnknownPrefix("UP-9")
		return nil
	case s.refused != "":
		tagUnknownPrefix("D/" + s.refused)
		return nil
	case !s.belowHop && s.testPaths:
		// UP-9: only test paths, and no hop to a production entry.
		tagUnknownPrefix("UP-9")
		return nil
	case !s.belowHop:
		// UP-10: a route below no hop is reported route_unmounted.
		tagUnknownPrefix("UP-10")
		tagUnknownPrefix("UP-10/`route_unmounted`")
		return nil
	}
	var list []unknownPrefixEntry
	for _, m := range s.mounts {
		before := len(list)
		for _, p := range mountPaths(m) {
			if p.Template == "" {
				// UP-10: the prefix itself below the hop (an Axum nested
				// `/`) is stored; it has no literal segment, so the
				// matcher never takes it.
				tagUnknownPrefix("UP-10/the path `\"\"` below a hop")
			}
			key := m.Key
			p.Hop = &key
			p.PrefixReason = m.Reason
			list = append(list, p)
		}
		if len(list) == before {
			continue
		}
		tagUnknownPrefix(row)
		if h := s.hops[m.Key]; h != nil && h.form != "" {
			tagUnknownPrefix(row + "/" + h.form)
		}
	}
	if len(list) == 0 {
		tagUnknownPrefix("D/" + upDefaultNotStored)
		return nil
	}
	if s.testPaths {
		list = productionEntries(list, tagUnknownPrefix)
		if len(list) == 0 {
			return nil
		}
	}
	tagUnknownPrefix("UP-8")
	return mergeUnknownPrefixEntries(list)
}

// upFormTestPaths is UP-9's per-scope form: a route whose full paths are
// all test scope keeps its production entries only.
const upFormTestPaths = "all test scope"

// productionEntries applies UP-9 per scope to a route whose full paths
// are all test scope: its test-scope entries go (the test path is known),
// its production entries stay (a test harness says nothing about where
// production serves the route). tag reports the row.
func productionEntries(list []unknownPrefixEntry, tag func(string)) []unknownPrefixEntry {
	tag("UP-9")
	tag("UP-9/" + upFormTestPaths)
	var out []unknownPrefixEntry
	for _, e := range list {
		if e.Scope != testscope.Test {
			out = append(out, e)
		}
	}
	return out
}

// scopedPaths reports, for a route's full path entries, whether one is
// production scope and whether they are all test scope (UP-9 per scope).
func scopedPaths(scopes []testscope.Scope) (production, testOnly bool) {
	for _, s := range scopes {
		if s != testscope.Test {
			return true, false
		}
	}
	return false, len(scopes) > 0
}

// mountPaths are a stored mount's paths below its hop as unknown-prefix
// entries (without hop and reason): Express stores pathEntry, FastAPI and
// Axum fastapiPathEntry (with the trailing slash).
func mountPaths(m mountEntry) []unknownPrefixEntry {
	var out []unknownPrefixEntry
	add := func(p pathEntry, slash bool) {
		out = append(out, unknownPrefixEntry{Template: p.Template, Prefix: unknownPrefixValue, Above: p.Above, TrailingSlash: slash, Conditional: p.Conditional, Resolution: p.Resolution, Optional: p.Optional, Scope: p.Scope, Assumptions: p.Assumptions, ServedThrough: p.ServedThrough, MayAnswerFirst: p.MayAnswerFirst})
	}
	switch paths := m.Paths.(type) {
	case []pathEntry:
		for _, p := range paths {
			add(p, false)
		}
	case []fastapiPathEntry:
		for _, p := range paths {
			add(p.pathEntry, p.TrailingSlash)
		}
	}
	return out
}

// hopIndex maps hop keys to hops.
func hopIndex(hops []*mountHop) map[string]*mountHop {
	out := make(map[string]*mountHop, len(hops))
	for _, h := range hops {
		out[h.key] = h
	}
	return out
}

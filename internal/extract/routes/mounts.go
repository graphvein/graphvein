package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Unresolved mounts (T029 FU-2, rows OG-94..OG-96 of
// docs/contract/claims/openapi-generation.md): a route the linker gives no
// path because composition stopped at a hop it cannot read (a prefix that
// is not a constant, a router argument or wrapper no rule proves, an
// escaped router) stores, per such hop, the hop's stable identity and the
// paths proven below it. A user value for the hop's key in
// explorer.path_values ("the routes under this hop are served with prefix
// P") then places every route under it in the generated document; the
// index itself never takes the value.

// MountAttribute is the route attribute holding the unresolved mounts.
const MountAttribute = "unresolved_mounts"

// maxMountsPerRoute bounds the hops stored on one route.
const maxMountsPerRoute = 16

// mountHop is one hop where composition stopped: where it is, what it
// reads, and why no path is claimed through it.
type mountHop struct {
	file string
	// function is the enclosing declaration's name ("(package)" at
	// package level, "(module)" for module-level Python or JavaScript).
	function string
	// text is the short form named in the key (the call and its first
	// argument); full is the hop's whole text with function bodies elided,
	// hashed into the registration.
	text, full string
	span       contract.Span
	reason     string
	// key is assigned by assignMountKeys.
	key string
	// covers says what a value for the hop states (MountCovers*); ""
	// is MountCoversRoot.
	covers string
	// chain is the text of the composition above the hop (the known
	// prefixes and hops from the root to it), hashed into the
	// registration: an edit above the hop changes it. Empty when the
	// value covers the path from the root.
	chain string
	// form is the hop form an unknown-prefix entry is claimed by: a Go
	// form (gohttp_unknown_prefix.go, rows UP-1, UP-2 and UP-4), a
	// FastAPI or Axum form named in row UP-6 or UP-7
	// (unknown_prefix.go); "" for an Express `use` (UP-5 names one
	// form). syntax marks a net/http StripPrefix prefix that is a
	// constant holding template syntax: its hop is stored, but no
	// unknown-prefix entry comes from it (row UP-2's refusal).
	form   string
	syntax bool
}

// What a value for a hop states (stored as value_covers, row OG-95).
const (
	// MountCoversHop: the routers above the hop are proven and their
	// prefix is composed into each path's "above"; the value is only the
	// part the hop adds.
	MountCoversHop = "hop"
	// MountCoversNothing: the hop adds no part Graphvein could not read
	// (a net/http Handle without StripPrefix whose outer pattern or
	// router is not proven); the paths are the served paths and the only
	// value is "/", stating that the hop passes them on.
	MountCoversNothing = "nothing"
	// MountCoversRoot: the routers above the hop are not proven either;
	// the value is the whole prefix from the root.
	MountCoversRoot = "root"
)

// registration is the hop's content hash: it changes when the hop's text
// changes, or the composition above it, even while its key stays the same.
func (h *mountHop) registration() string {
	text := h.file + "\x00" + h.function + "\x00" + h.full
	if h.chain != "" {
		text += "\x00above\x00" + h.chain
	}
	return contract.ContentHash([]byte(text))
}

// assignMountKeys gives every hop its explorer.path_values key:
// mount:<file>#<function> <text>, with @N for the Nth hop of the same
// text in the same function (in source order).
func assignMountKeys(hops []*mountHop) {
	sorted := append([]*mountHop(nil), hops...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.span.StartLine != b.span.StartLine {
			return a.span.StartLine < b.span.StartLine
		}
		if a.span.StartColumn != b.span.StartColumn {
			return a.span.StartColumn < b.span.StartColumn
		}
		if a.text != b.text {
			return a.text < b.text
		}
		if a.full != b.full {
			return a.full < b.full
		}
		return a.reason < b.reason
	})
	seen := map[string]int{}
	done := map[*mountHop]bool{}
	for _, h := range sorted {
		if done[h] {
			continue
		}
		done[h] = true
		q := h.function + " " + mountKeyText(h.text)
		base := h.file + "\x00" + q
		seen[base]++
		if seen[base] > 1 {
			q += fmt.Sprintf("@%d", seen[base])
		}
		h.key = facts.ID("mount", h.file, q)
	}
}

// mountKeyText keeps a key's text on one line without spaces.
func mountKeyText(s string) string {
	s = strings.Join(strings.Fields(s), "")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return "?"
	}
	return s
}

// mountEntry is one stored unresolved mount of a route.
type mountEntry struct {
	Key          string `json:"key"`
	Registration string `json:"registration"`
	Reason       string `json:"reason"`
	Hop          string `json:"hop"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	// Shadowing is "not_checked" when the linker's sibling and shadow
	// checks above the hop cannot run without the prefix (Go routers):
	// another pattern beside the hop may answer some of the paths.
	Shadowing string `json:"shadowing,omitempty"`
	// Covers says what a value for the hop states (MountCovers*).
	Covers string `json:"value_covers"`
	// Paths are the route's paths below the hop, in the framework's
	// stored path entry form, relative to the hop's prefix; with
	// MountCoversHop each carries the known prefix above the hop in
	// "above" ("" or absent: the hop is at the root), with
	// MountCoversNothing they are the served paths.
	Paths any `json:"paths"`
}

// newMountEntry builds a route's stored entry for hop h with the paths
// below it.
func newMountEntry(h *mountHop, paths any, shadowing string) mountEntry {
	covers := h.covers
	if covers == "" {
		covers = MountCoversRoot
	}
	return mountEntry{Key: h.key, Registration: h.registration(), Reason: h.reason, Hop: h.full, File: h.file, Line: h.span.StartLine, Shadowing: shadowing, Covers: covers, Paths: paths}
}

// aboveChain is a hop's chain text from the known prefixes above it:
// sorted, distinct, one per line.
func aboveChain(prefixes []string) string {
	set := map[string]bool{}
	for _, p := range prefixes {
		set[p] = true
	}
	list := make([]string, 0, len(set))
	for p := range set {
		list = append(list, p)
	}
	sort.Strings(list)
	return strings.Join(list, "\n")
}

// sortMountEntries orders a route's mounts by key and bounds them.
func sortMountEntries(list []mountEntry) []mountEntry {
	sort.Slice(list, func(i, j int) bool { return list[i].Key < list[j].Key })
	if len(list) > maxMountsPerRoute {
		list = list[:maxMountsPerRoute]
	}
	return list
}

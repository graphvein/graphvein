package routes

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Unknown-prefix paths of Go routes (task T033, ADR 0084; rows
// UP-1..UP-4 and UP-8..UP-10 of docs/contract/claims/unknown-prefix-paths.md,
// code table unknownPrefixContract). A pathless Go route's entries are its
// unresolved mounts' paths (gohttp_mounts.go), one entry per path and hop,
// with the hop's key and reason; a generated server whose BaseURL is not
// one constant (GR-G1) has no hop, and its registrations' entries are the
// template's paths. Every other pathless route gets none (row D).

// Go hop forms (mountHop.form): which row an entry from the hop is claimed
// by.
const (
	// upFormRoute, upFormMount, upFormGroup: chi Route, chi Mount, gin
	// Group with a prefix that is not a constant (UP-1).
	upFormRoute = "`Route(p, f)`"
	upFormMount = "`Mount(p, x)`"
	upFormGroup = "`Group(p)`"
	// upFormStrip, upFormOuter: a net/http composition (GR-C3) whose
	// StripPrefix prefix, or else whose outer pattern, is not a constant
	// (UP-2).
	upFormStrip = "`StripPrefix` prefix"
	upFormOuter = "outer pattern"
	// upFormWrapped, upFormEscaped, upFormBinding: a router reached through
	// a hop no row proves: behind a wrapper call or closure, escaped, or a
	// composition whose handler binding may hold a value no rule proves
	// (UP-4).
	upFormWrapped = "wrapped"
	upFormEscaped = "escaped"
	upFormBinding = "binding"
)

// upFormRows maps a hop form to its row.
var upFormRows = map[string]string{
	upFormRoute: "UP-1", upFormMount: "UP-1", upFormGroup: "UP-1",
	upFormStrip: "UP-2", upFormOuter: "UP-2",
	upFormWrapped: "UP-4", upFormEscaped: "UP-4", upFormBinding: "UP-4",
}

// upTag tags a branch of the unknown-prefix contract (TestUnknownPrefixContract).
func (l *goLinker) upTag(row string) {
	l.tag(upTagPrefix + row)
}

// upTagPrefix separates the unknown-prefix contract's tags from the Go
// route contract's.
const upTagPrefix = "unknown-prefix:"

// safeUnknownPrefixPaths isolates one route's projection: a failure is a
// diagnostic and the route keeps no entries.
func (l *goLinker) safeUnknownPrefixPaths(reg goRegistration, paths []goPathEntry, mounts []mountEntry, unmounted bool) (out []unknownPrefixEntry) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: reg.file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go unknown-prefix paths of %s %q failed: %v", reg.file, reg.method, reg.pattern, v)})
			out = nil
		}
	}()
	return l.unknownPrefixPaths(reg, paths, mounts, unmounted)
}

// unknownPrefixPaths decides a Go route's unresolved_prefix_paths from its
// paths, its stored unresolved mounts and whether route_unmounted was
// reported for it (row UP-10).
func (l *goLinker) unknownPrefixPaths(reg goRegistration, paths []goPathEntry, mounts []mountEntry, unmounted bool) []unknownPrefixEntry {
	scopes := make([]testscope.Scope, 0, len(paths))
	for _, p := range paths {
		scopes = append(scopes, p.Scope)
	}
	production, testOnly := scopedPaths(scopes)
	switch {
	case production:
		// UP-9: a known path is never weakened by an unknown one.
		l.upTag("UP-9")
		return nil
	case testOnly && len(mounts) == 0:
		// UP-9: only test paths, and no hop to a production entry.
		l.upTag("UP-9")
		return nil
	case reg.noPath == goOapiUnresolved:
		return l.oapiUnknownPrefix(reg)
	case unmounted:
		l.upTag("UP-10")
		l.upTag("UP-10/`route_unmounted`")
		return nil
	}
	var list []unknownPrefixEntry
	for _, m := range mounts {
		h := l.hopByKey(m.Key)
		if h == nil {
			continue
		}
		row := upFormRows[h.form]
		switch {
		case row == "":
			// Defensive: every Go hop is recorded with its form.
			l.upTag("D/no_hop")
			continue
		case h.syntax:
			// UP-2's refusal: the prefix is read, but holds template
			// syntax StripPrefix strips literally.
			l.upTag("D/template_syntax")
			continue
		}
		l.upTag(row)
		if h.form != upFormBinding {
			// The binding form is UP-4's without a name of its own in the
			// row.
			l.upTag(row + "/" + h.form)
		}
		entries, _ := m.Paths.([]goPathEntry)
		for _, p := range entries {
			if p.Template == "" {
				// UP-10: the prefix itself below the hop is stored; it has
				// no literal segment, so the matcher never takes it.
				l.upTag("UP-10/the path `\"\"` below a hop")
			}
			key := m.Key
			list = append(list, unknownPrefixEntry{Template: p.Template, Prefix: unknownPrefixValue, PrefixReason: m.Reason, Hop: &key, Above: p.Above, TrailingSlash: p.TrailingSlash, Conditional: p.Conditional, Resolution: p.Resolution, Optional: p.Optional, Scope: p.Scope, Assumptions: p.Assumptions, ServedThrough: p.ServedThrough, MayAnswerFirst: p.MayAnswerFirst})
		}
	}
	if len(list) == 0 {
		// D: no hop stored (a check refused the path, a prefix chi
		// rejects, a StripPrefix at a serving position), or no path below
		// a hop.
		l.upTag("D/no_hop")
		return nil
	}
	if testOnly {
		list = productionEntries(list, l.upTag)
		if len(list) == 0 {
			return nil
		}
	}
	l.upTag("UP-8")
	return mergeUnknownPrefixEntries(list)
}

// hopByKey finds a recorded hop by its key.
func (l *goLinker) hopByKey(key string) *mountHop {
	for _, h := range l.hops {
		if h.key == key {
			return h
		}
	}
	return nil
}

// oapiUnknownPrefix is row UP-3: a registration of a generated server whose
// instances give no one constant BaseURL has the template's path (the
// registration's pattern with the BaseURL left out, normalized as GR-P2)
// as its one entry, with no hop. A server with no visible instance never
// gets here (its BaseURL is "", its routes route_unmounted).
func (l *goLinker) oapiUnknownPrefix(reg goRegistration) []unknownPrefixEntry {
	if reg.oapi == nil || len(reg.oapi.instances) == 0 {
		l.upTag("D/no_hop")
		return nil
	}
	raw := reg.name
	switch reg.framework {
	case goGin:
		raw = reg.pattern
	case goNetHTTP:
		i := strings.IndexByte(raw, '/')
		if i < 0 {
			l.upTag("D/no_hop")
			return nil
		}
		raw = raw[i:]
	}
	template, trailing, ok := goTemplate(reg.framework, raw)
	if !ok {
		l.upTag("D/no_hop")
		return nil
	}
	l.upTag("UP-3")
	entry := unknownPrefixEntry{Template: template, Prefix: unknownPrefixValue, PrefixReason: "route_prefix_unresolved", TrailingSlash: trailing, Conditional: reg.conditional, Resolution: reg.resolution, Scope: l.scope(reg.file)}
	l.upTag("UP-8")
	return mergeUnknownPrefixEntries([]unknownPrefixEntry{entry})
}

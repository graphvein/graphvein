package routes

import (
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Trailing slashes in Axum paths (T029 FU-3b). Axum serves a route's
// pattern as written: `route("/t/", h)` answers `/t/` and 404s on `/t`
// (axum 0.6.20, 0.7.9 and 0.8.8; 0.3.4 answers `/t/` and redirects `/t`
// with 301). A nest prefix's trailing slash also counts: under 0.6.20,
// 0.7.9 and 0.8.8 `nest("/p/", r)` answers r's `/x` at `/p/x` and r's `/`
// at `/p/`, while `nest("/n", r)` answers r's `/` at `/n` only; 0.3.4
// answers r's `/x` at `/p//x` instead. These are the results of the
// scratch oracle of the T029 FU-3 task record (internal).
//
// A path whose composition involves such a slash is therefore composed
// exactly only for a crate whose axum version is proven within the
// checked behaviour (axumSlashProven); otherwise it is not claimed
// (route_trailing_slash_unproven). Paths without such a slash keep the
// earlier composition unchanged.

// prefixSeparator separates the nest prefixes of a chain in the composed
// prefix the Axum linker carries before a path is formed.
const prefixSeparator = "\x00"

var (
	// axumSlashLow and axumSlashHigh bound the versions whose trailing
	// slash behaviour is taken as checked: 0.6.20 (the last 0.6 release),
	// 0.7.x and 0.8.x (0.7.9 and 0.8.8 checked).
	axumSlashLow  = semver{0, 6, 20}
	axumSlashHigh = semver{0, 9, 0}
)

// axumSlashProven reports whether every version in v answers trailing
// slashes as checked.
func axumSlashProven(v versionRange) bool {
	return v.atLeast(axumSlashLow) && v.below(axumSlashHigh)
}

// serviceMark starts a prefix that a nest_service added (T029 FU-3d).
const serviceMark = "\x01"

// axumPrefixes splits a composed prefix into its nest prefixes, without
// the nest_service marks.
func axumPrefixes(composed string) []string {
	var out []string
	for _, p := range strings.Split(composed, prefixSeparator) {
		if p = strings.TrimPrefix(p, serviceMark); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// axumServiceLast reports whether the last nest prefix of a composed
// prefix was added by nest_service: a `/` route there is the service's
// root.
func axumServiceLast(composed string) bool {
	parts := strings.Split(composed, prefixSeparator)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return strings.HasPrefix(parts[i], serviceMark)
		}
	}
	return false
}

// axumFormed is one path formed by axumPaths.
type axumFormed struct {
	template string
	slash    bool
}

// axumPaths forms the paths of pattern below a composed prefix. A router
// nested with nest_service answers its `/` route at the prefix and at the
// prefix with a trailing slash (axum 0.6.20, 0.7.9 and 0.8.8 checked; the
// T029 FU-3 task record, internal), so that root gives two paths, claimed
// only for a proven version; a nest_service prefix ending with `/` is not
// checked. Every other path is axumPath's. ok is false when no path is
// claimed.
func axumPaths(composed, pattern string, proven bool) ([]axumFormed, bool) {
	prefixes := axumPrefixes(composed)
	for _, p := range strings.Split(composed, prefixSeparator) {
		if strings.HasPrefix(p, serviceMark) && strings.HasSuffix(p, "/") {
			return nil, false
		}
	}
	if pattern == "/" && axumServiceLast(composed) {
		if !proven {
			return nil, false
		}
		template, _, ok := axumPath(prefixes, pattern, proven)
		if !ok {
			return nil, false
		}
		return []axumFormed{{template, false}, {template, true}}, true
	}
	template, slash, ok := axumPath(prefixes, pattern, proven)
	if !ok {
		return nil, false
	}
	return []axumFormed{{template, slash}}, true
}

// axumSlashForm reports whether a path's composition involves a trailing
// slash (a pattern other than `/` ending with `/`, a nest prefix ending
// with `/`) or a doubled slash, whose answer depends on the axum version.
func axumSlashForm(prefixes []string, pattern string) bool {
	if pattern != "/" && strings.HasSuffix(pattern, "/") || strings.Contains(pattern, "//") {
		return true
	}
	for _, p := range prefixes {
		if strings.HasSuffix(p, "/") || strings.Contains(p, "//") {
			return true
		}
	}
	return false
}

// axumPath forms the path of pattern below prefixes: its normalised
// template and whether it ends with a slash. ok is false when the path
// depends on a trailing slash the version does not prove.
func axumPath(prefixes []string, pattern string, proven bool) (template string, slash, ok bool) {
	if !axumSlashForm(prefixes, pattern) {
		return axumNormalize(strings.Join(prefixes, "") + pattern), false, true
	}
	if !proven || strings.Contains(pattern, "//") {
		return "", false, false
	}
	acc := ""
	for _, p := range prefixes {
		if p == "/" || strings.Contains(p, "//") {
			// nest("/") is refused by axum 0.7+; a doubled slash is not
			// checked.
			return "", false, false
		}
		acc = strings.TrimSuffix(acc, "/") + p
	}
	path := strings.TrimSuffix(acc, "/") + pattern
	if pattern == "/" && acc != "" {
		path = acc
	}
	return axumNormalize(path), len(path) > 1 && strings.HasSuffix(path, "/"), true
}

// axumJoinPrefix appends a nest prefix to a composed prefix, keeping the
// prefixes apart (merges and factory returns add none).
func axumJoinPrefix(composed, prefix string) string {
	if prefix == "" {
		return composed
	}
	return composed + prefixSeparator + prefix
}

// consumeWithin marks the routers an unresolved nest_service argument
// holds (a router wrapped in another service) as consumed: they are
// served below the prefix through a service no row reads, so their
// routes get no path instead of a root path (T029 FU-3d).
func (r *axumLinker) consumeWithin(file, owner string, e facts.RustExpr, env []map[string]axumObject) {
	if e.Kind == "path" || e.Kind == "call" || e.Kind == "method" {
		if o := r.expression(file, owner, e, env); o.key != "" {
			r.consumed[o.key] = true
			return
		}
	}
	if e.Object != nil {
		r.consumeWithin(file, owner, *e.Object, env)
	}
	for _, a := range e.Args {
		r.consumeWithin(file, owner, a, env)
	}
}

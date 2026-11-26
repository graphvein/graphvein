package routes

import (
	"strings"
)

// Entry order and shadowing (T035). Starlette dispatches a
// request through an app's entries (routes, mounts, included routers) in
// the order they were added, and the first entry that matches answers
// it. A Mount matches every request below its prefix and answers it from
// the mounted app, 404 included (FastAPI 0.121.1/Starlette 0.49.3 and
// FastAPI 0.165.0/Starlette 1.10.0): a later route below the prefix is
// never reached. A path a composed chain stores for such a route is not
// stored; it is named in a route_shadowed diagnostic. (An Express mount
// passes every request none of its routes answers on to the next entry,
// Express 4.21.1 and 5.1.0; an earlier route that matches is DA-E1's
// earlier route handler, docs/extract/auth.md.)

// entrySite is where an entry is added to its routing object. Two sites
// have a known order only in one function body of one file (domain is the
// function scope, context a FastAPI helper context): there source order
// is execution order. A FastAPI routes= list entry (first) is added
// before every call on the object.
type entrySite struct {
	file         string
	domain       int
	context      string
	line, column int
	first        bool
}

// entryOrder reports whether a is added before b, and whether their
// order is known at all. A site is not before itself.
func entryOrder(a, b entrySite) (before, known bool) {
	if a == b {
		return false, true
	}
	if a.first != b.first {
		return a.first, true
	}
	if a.file != b.file || a.domain != b.domain || a.context != b.context {
		return false, false
	}
	if a.line != b.line {
		return a.line < b.line, true
	}
	return a.column < b.column, true
}

// viaStep is one routing object on a composed chain: base is the path
// up to the object (before a router's own prefix), site the entry on the
// object the chain continues through (nil on the chain's last object,
// whose entry is the registration itself).
type viaStep struct {
	object string
	base   string
	site   *entrySite
}

// extendVia returns via with one more step, never sharing its backing
// array with a sibling chain.
func extendVia(via []viaStep, step viaStep) []viaStep {
	out := make([]viaStep, len(via), len(via)+1)
	copy(out, via)
	return append(out, step)
}

// parameterSegment reports a segment that holds a parameter (Starlette
// `{x}`, `{x:conv}`, stored `{x}` and `{*x}`), which matches any value.
func parameterSegment(s string) bool {
	return strings.Contains(s, "{")
}

// starletteMountTakes reports whether a Starlette Mount at the raw prefix
// takes every request of the raw template: Starlette compiles the mount
// to `^<prefix>/(?P<path>.*)$` (the prefix without its trailing slash),
// so a template strictly below the prefix is taken, the prefix itself is
// not. A parameter of the prefix takes any segment; a template parameter
// below a literal prefix segment is taken only for some values: not
// taken (the template is still served for the others).
func starletteMountTakes(prefix, template string) bool {
	ps, ts := segments(prefix), segments(template)
	if len(ps) == 0 {
		// Mount("") takes every path, "/" included.
		return true
	}
	if len(ts) < len(ps) || len(ts) == len(ps) && !strings.HasSuffix(template, "/") {
		return false
	}
	for i, p := range ps {
		if parameterSegment(p) {
			continue
		}
		if parameterSegment(ts[i]) || p != ts[i] {
			return false
		}
	}
	return true
}

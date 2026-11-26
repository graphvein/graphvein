package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/testscope"
)

// routeShadowed names a path a composed chain reaches but an earlier
// Starlette mount takes (mount_order.go): the path is not
// stored.
const routeShadowed = "route_shadowed"

// ShadowedAttribute is the route attribute naming the composed paths an
// earlier Starlette mount takes (route_shadowed): each a shadowedPath. A
// route whose every path is withheld this way is mounted, not unmounted.
const ShadowedAttribute = "shadowed_paths"

// shadowedPath is one withheld path and the mount that takes it.
type shadowedPath struct {
	Path string `json:"path"`
	By   string `json:"by"`
}

// sortedStringKeys returns a map's keys in order.
func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// fastapiShownTemplate is a raw template normalized as stored, with its
// trailing slash written out (a path entry's trailing_slash).
func fastapiShownTemplate(raw string) string {
	t := pythonNormalize(raw)
	if strings.HasSuffix(raw, "/") && t != "/" {
		t += "/"
	}
	return t
}

// routerMountVersion is the FastAPI release whose include_router serves
// a Starlette mount added to the included APIRouter (0.137.0: lazy
// included routers, `_IncludedRouter`). 0.136.3 and earlier copy only
// the router's API routes and drop its mounts (checked with
// 0.121.1, 0.136.3, 0.137.0 and 0.165.0).
var routerMountVersion = python.Version{0, 137, 0}

// fastapiShadow is one Starlette mount on an object: it takes every
// request below prefix (valid: the prefix is read) from the entries added
// after it.
type fastapiShadow struct {
	site   entrySite
	prefix string
	valid  bool
	text   string
}

// listShadow records a Mount(...) written in the routes= list literal of
// a proven FastAPI or APIRouter constructor: the list's routes come
// before every call on the object.
func (r *fastapiResolver) listShadow(file string, syntax facts.PythonRoute, shadows map[string][]fastapiShadow) {
	if !syntax.InRoutesList || r.context != "" {
		return
	}
	owner := r.objects[fmt.Sprintf("%s:%d", file, syntax.RoutesCall)]
	if owner.kind == "" {
		return
	}
	e := syntax.Expression
	if e.Object == nil {
		return
	}
	if b := r.modules.Expression(file, syntax.Call.Scope, *e.Object); !b.External || !starletteMountNames[fullName(b)] {
		return
	}
	prefix, found := argument(e, "path", 0)
	value, valid := "", false
	if found {
		value, _, valid = r.modules.Prefix(file, syntax.Call.Scope, prefix)
	}
	c := syntax.Call
	shadows[owner.key] = append(shadows[owner.key], fastapiShadow{site: entrySite{file: file, first: true, line: c.Span.StartLine, column: c.Span.StartColumn}, prefix: value, valid: valid, text: facts.CompactPython(e.Text)})
}

// fastapiTakenBy names the mount that takes every request of raw (a
// registration's raw composed template, its chain via, the registration
// at reg) before the route: a mount on an object of the chain added
// before the chain's entry on that object (or in an order no rule
// proves) whose read prefix takes the path. "" when none does. may lists
// the earlier mounts whose prefix is not read:
// such a mount may take the path or not, so the path is kept,
// conditional, with assumption RO-V1 naming them (a mount at a
// configured URL before the routes is a common layout; dropping every
// later path for it would lose the whole app).
func fastapiTakenBy(shadows map[string][]fastapiShadow, via []viaStep, reg entrySite, raw string) (taken string, may []string) {
	for _, step := range via {
		entry := reg
		if step.site != nil {
			entry = *step.site
		}
		for _, sh := range shadows[step.object] {
			before, known := entryOrder(sh.site, entry)
			if sh.site == entry || known && !before {
				continue
			}
			text := fmt.Sprintf("%s (%s:%d)", sh.text, sh.site.file, sh.site.line)
			switch {
			case !sh.valid:
				may = append(may, text)
			case starletteMountTakes(step.base+sh.prefix, raw):
				return text, nil
			}
		}
	}
	return "", may
}

// mountAssumption is the assumption a path below an earlier mount whose
// prefix is not read rests on (row RO-V1): that mount does not take it.
const mountAssumption = "RO-V1"

// catchAllAssumption is the assumption a later Express path below an
// earlier mounted app or router with middleware rests on (row RO-V2): its
// middleware passes the request on.
const catchAllAssumption = "RO-V2"

// fastapiUvicornRun are the callees that serve their app argument at its
// own root.
var fastapiUvicornRun = map[string]bool{"uvicorn.run": true, "uvicorn.main.run": true}

// servedApps lists the FastAPI apps a serving form proves are served at
// their own root, each with whether every such form is
// conditional. The closed list: `uvicorn.run(x, ...)` (or `app=x`) with x
// a proven app (conditional when the call is under a branch or loop, the
// `if __name__ == "__main__":` guard included, or inside a function), and
// the `[tool.fastapi] entrypoint` of a committed pyproject.toml naming a
// module-level binding of a proven app (fastapi-cli's `fastapi run`).
// A form in a test-scope file serves only a test-scope app.
func (r *fastapiResolver) servedApps(files map[string]facts.Facts, rules testscope.Rules) map[string]bool {
	served := map[string]bool{}
	serve := func(file string, o pythonObject, conditional bool) {
		if o.kind != "app" {
			return
		}
		if testscope.Classify(file, rules) == testscope.Test && o.scopeLabel != testscope.Test {
			return
		}
		if old, seen := served[o.key]; seen {
			conditional = conditional && old
		}
		served[o.key] = conditional
	}
	r.peek = true
	defer func() { r.peek = false }()
	for _, file := range facts.Paths(files) {
		p := files[file].Python
		if p == nil {
			continue
		}
		for _, call := range p.HelperCalls {
			e := call.Expression
			if e.Object == nil {
				continue
			}
			if b := r.modules.Expression(file, call.Scope, *e.Object); !b.External || !fastapiUvicornRun[fullName(b)] {
				continue
			}
			arg, ok := argument(e, "app", 0)
			if !ok {
				continue
			}
			serve(file, r.object(file, call.Scope, arg, map[string]bool{}), call.Conditional || call.Scope != 0)
		}
	}
	for _, ep := range r.modules.FastAPIEntrypoints() {
		serve(ep.File, r.object(ep.File, 0, facts.RouteExpr{Kind: "identifier", Name: ep.Name, Text: ep.Name}, map[string]bool{}), false)
	}
	return served
}

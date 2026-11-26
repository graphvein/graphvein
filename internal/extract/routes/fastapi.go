package routes

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
	"github.com/graphvein/graphvein/internal/testscope"
)

var pythonFactories = map[string]map[string]string{
	"fastapi":              {"FastAPI": "app", "APIRouter": "router"},
	"fastapi.applications": {"FastAPI": "app"},
	"fastapi.routing":      {"APIRouter": "router"},
}

type pythonObject struct {
	key, kind, file string
	baseKey         string
	scope           int
	prefix          facts.RouteExpr
	hasPrefix       bool
	scopeLabel      testscope.Scope
	// deps is the constructor's dependencies= list (T026 AG-B4a).
	deps dependencyList
	// ctor is a router's constructor call (an unresolved own prefix is
	// a hop, T029 FU-2).
	ctor facts.RouteExpr
	// routeClass says why the routes registered on this object may use a
	// custom route class (route shape row SH-F38); "" for APIRoute.
	routeClass string
	// responseClass says why the success content type of the routes this
	// object serves is not read (a default_response_class, or keyword
	// unpacking that may pass one); responseStatus why their default
	// success status is not read (route shape row SH-F24). FastAPI
	// applies an object's default_response_class to the routes registered
	// on it and to the routes of every router included into it.
	responseClass, responseStatus string
}

// fastapiInclude is one include_router call as the response defaults see
// it (route shape row SH-F24): the routes of router to inherit the
// default_response_class of from and of the call itself. from is "" when
// the including object is not read.
type fastapiInclude struct {
	from, to                      string
	responseClass, responseStatus string
}

type fastapiResolver struct {
	modules     *python.ModuleResolver
	objects     map[string]pythonObject
	bindings    map[string]pythonObject
	helperFile  string
	helperScope int
	context     string
	rules       testscope.Rules
	helperTest  testscope.Scope
	// includes are the include_router calls whose router is read, also
	// when the including object or the prefix is not (row SH-F24).
	includes []fastapiInclude
	// unpackedApps says where a FastAPI constructor takes keyword
	// unpacking, which may pass exception_handlers (row SH-F37).
	unpackedApps []string
	// peek resolves an object without registering it.
	peek bool
}

// responseDefaults reads a default_response_class keyword, or keyword
// unpacking, of a FastAPI, APIRouter or include_router call (row SH-F24):
// why the success content type is not read, and why the default success
// status is not read (a class whose default is not 200, one not read, or
// unpacking).
func (r *fastapiResolver) responseDefaults(file string, scope int, e facts.RouteExpr, line int, what string) (contentType, status string) {
	for _, kw := range e.Keywords {
		switch kw.Name {
		case "default_response_class":
			tagShape("SH-F24/default_response_class")
			why := fmt.Sprintf("%s default_response_class %s (%s:%d)", what, describe(kw.Value), file, line)
			if contentType == "" {
				contentType = why
			}
			if st, ok := responseClassStatus(r.modules.Expression(file, scope, kw.Value)); !ok || st != "200" {
				status = why
			}
		case "":
			tagShape("SH-F24/**options")
			why := fmt.Sprintf("keyword unpacking on %s may set default_response_class (%s:%d)", what, file, line)
			return why, why
		}
	}
	return contentType, status
}

func (r *fastapiResolver) object(file string, scope int, e facts.RouteExpr, seen map[string]bool) pythonObject {
	if file == r.helperFile && scope == r.helperScope && e.Kind == "identifier" {
		if o, ok := r.bindings[e.Name]; ok {
			return o
		}
	}
	key := fmt.Sprintf("%s:%d:%d:%s:%s", file, scope, e.Offset, e.Kind, e.Text)
	if seen[key] {
		return pythonObject{}
	}
	seen[key] = true
	defer delete(seen, key)
	if e.Kind == "identifier" || e.Kind == "attribute" {
		b := r.modules.Expression(file, scope, e)
		if b.File != "" && !b.External && b.Module == nil {
			// A local object belongs to exactly the function that binds it.
			if b.Scope != 0 && (b.File != file || b.Scope != scope) {
				return pythonObject{}
			}
			return r.object(b.File, b.Scope, b.Syntax.Value, seen)
		}
	}
	if e.Kind == "call" && e.Object != nil {
		b := r.modules.Expression(file, scope, *e.Object)
		if b.External {
			module, name := b.File, b.Name
			if at := strings.LastIndex(name, "."); at >= 0 {
				module += "." + name[:at]
				name = name[at+1:]
			}
			kind := pythonFactories[module][name]
			if kind != "" {
				o := pythonObject{key: fmt.Sprintf("%s:%d", file, e.Offset), kind: kind, file: file, scope: scope}
				o.scopeLabel = testscope.Classify(file, r.rules)
				if r.context != "" && file == r.helperFile && scope == r.helperScope {
					o.baseKey = o.key
					o.key += ":" + r.context
					o.scopeLabel = chainScope(o.scopeLabel, r.helperTest)
				}
				if kind == "router" {
					o.prefix, o.hasPrefix = keyword(e, "prefix")
					o.ctor = e
				}
				o.deps = readDependencyList(file, scope, e, kind+" dependencies")
				constructor := "FastAPI"
				if kind == "router" {
					constructor = "APIRouter"
				}
				o.responseClass, o.responseStatus = r.responseDefaults(file, scope, e, spanLine(e), constructor)
				for _, kw := range e.Keywords {
					switch {
					case kw.Name == "route_class" && kind == "router":
						// FastAPI takes no route_class (row SH-F38).
						if b := r.modules.Expression(file, scope, kw.Value); !b.External || fullName(b) != "fastapi.routing.APIRoute" {
							o.routeClass = fmt.Sprintf("route_class %s (%s:%d)", describe(kw.Value), file, spanLine(e))
						}
					case kw.Name == "" && kind == "router":
						tagShape("SH-F38/**options")
						o.routeClass = fmt.Sprintf("keyword unpacking may set route_class (%s:%d)", file, spanLine(e))
					case kw.Name == "" && kind == "app":
						where := fmt.Sprintf("%s:%d", file, spanLine(e))
						if !slices.Contains(r.unpackedApps, where) {
							r.unpackedApps = append(r.unpackedApps, where)
						}
					}
				}
				if !r.peek {
					r.objects[o.key] = o
				}
				return o
			}
		}
	}
	return pythonObject{}
}

// spanLine is an expression's first line (0 when it has no span).
func spanLine(e facts.RouteExpr) int {
	if e.Span == nil {
		return 0
	}
	return e.Span.StartLine
}

func keyword(e facts.RouteExpr, name string) (facts.RouteExpr, bool) {
	var unpack *facts.RouteExpr
	for _, kw := range e.Keywords {
		if kw.Name == name {
			return kw.Value, true
		}
		if kw.Name == "" && kw.Value.Kind == "dictionary_splat" {
			value := kw.Value
			unpack = &value
		}
	}
	// An unpack can supply an omitted prefix/path/methods/endpoint. Its absence
	// cannot be proved, so do not silently substitute the library's default.
	if unpack != nil {
		return *unpack, true
	}
	return facts.RouteExpr{}, false
}

func argument(e facts.RouteExpr, name string, index int) (facts.RouteExpr, bool) {
	if len(e.Args) > index {
		return e.Args[index], true
	}
	return keyword(e, name)
}

func weaker(a, b string) string {
	if strength(a) < strength(b) {
		return a
	}
	return b
}

func strength(resolution string) int {
	switch resolution {
	case "literal":
		return 3
	case "constant":
		return 2
	case "settings_default":
		return 1
	}
	return 0
}

// parameter finds brace parameters for the Starlette convertor strip.
var parameter = regexp.MustCompile(`\{[^}]*\}|:([A-Za-z_][A-Za-z0-9_]*)(\?)?|\*`)

func pythonNormalize(s string) string {
	raw := strings.FieldsFunc(s, func(r rune) bool { return r == '/' })
	last := ""
	if len(raw) > 0 {
		last = raw[len(raw)-1]
	}
	// Strip Starlette convertors before shared slash normalization. Express
	// parameters remain unaffected by this Python-only preparation.
	s = parameter.ReplaceAllStringFunc(s, func(p string) string {
		if strings.HasPrefix(p, "{") {
			if i := strings.Index(p, ":"); i >= 0 {
				return p[:i] + "}"
			}
		}
		return p
	})
	// Colon and asterisk characters outside braces are literal in Starlette;
	// Express's parameter/wildcard substitutions must not reinterpret them.
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '/' })
	// Starlette's path convertor matches the rest of the path, slashes
	// included: as the whole final segment it is the final catch-all
	// {*name} every framework stores (docs/extract/routes.md). Elsewhere
	// it keeps the one-segment form above.
	if n := len(parts); n > 0 && n == len(raw) && finalPath.MatchString(last) {
		parts[n-1] = "{*" + strings.TrimPrefix(strings.SplitN(last, ":", 2)[0], "{") + "}"
	}
	return "/" + strings.Join(parts, "/")
}

// finalPath is a whole segment that is one parameter with Starlette's path
// convertor (Starlette's own parameter spelling, no spaces).
var finalPath = regexp.MustCompile(`^\{[A-Za-z_][A-Za-z0-9_]*:path\}$`)

// LinkFastAPI composes cached Python registrations on every current inventory.
// It preserves source identity independently of router/include/mount prefixes.
//
// shapeDepth is the call-following depth limit of the route shapes
// (routeshape row SM-21); 0 means routeshape.DefaultDepth.
func LinkFastAPI(files map[string]facts.Facts, modules *python.ModuleResolver, shapeDepth int, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	if shapeDepth == 0 {
		shapeDepth = routeshape.DefaultDepth
	}
	rules := selectedRules(scopeRules)
	r := fastapiResolver{modules: modules, objects: map[string]pythonObject{}, rules: rules}
	var nodes []contract.Node
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	diagnostic := func(file, code, severity, expression string, s contract.Span) {
		diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: fmt.Sprintf("%s at %d:%d-%d:%d", expression, s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)})
	}
	type registration struct {
		file   string
		syntax facts.PythonRoute
		object pythonObject
	}
	var registrations []registration
	var mounts []mount
	// unknownMounts are the hops of prefixes that are not constants (T029
	// FU-2), hopSites their hops by site.
	var unknownMounts []fastapiUnknownMount
	hopSites := map[string]*mountHop{}
	// mountDeps holds each mount's dependency side, index for index.
	var mountDeps []fastapiMountDeps
	// mounted are the FastAPI apps a Starlette mount consumes (T035): a
	// mounted app answers only below its mount, so it is no root of its
	// own. A mount read only in test scope does not consume an app
	// outside it.
	mounted := map[string]bool{}
	consume := func(target pythonObject, scope testscope.Scope) {
		if scope == testscope.Test && target.scopeLabel != testscope.Test {
			return
		}
		mounted[target.key] = true
	}
	// shadows are the Starlette mounts of each object, in entry order:
	// a mount takes every request below its prefix.
	shadows := map[string][]fastapiShadow{}
	instances := r.helperInstances(files)
	for _, file := range facts.Paths(files) {
		r.bindings, r.helperFile, r.context = nil, "", ""
		r.helperTest = ""
		p := files[file].Python
		if p == nil {
			continue
		}
		for scope, s := range p.Scopes {
			if s.Class {
				continue
			}
			var names []string
			for name := range s.Bindings {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				r.object(file, scope, facts.RouteExpr{Kind: "identifier", Name: name, Text: name}, map[string]bool{})
			}
		}
		for _, syntax := range p.Routes {
			contexts := []helperInstance{{}}
			if syntax.Call.Method == "include_router" || syntax.Call.Method == "mount" {
				if resolved := instances[helperKey(file, syntax.Call.Scope)]; len(resolved) > 0 {
					if helperParameterMount(p, syntax) {
						contexts = resolved
					} else {
						contexts = append(contexts, resolved...)
					}
				}
			}
			for _, instance := range contexts {
				r.bindings, r.helperFile, r.helperScope, r.context = instance.bindings, instance.file, instance.scope, instance.context
				r.helperTest = instance.scopeLabel
				c := syntax.Call
				c.Conditional = c.Conditional || instance.conditional
				if c.Method == "Mount" {
					r.listShadow(file, syntax, shadows)
					owner, target, prefix, ok := r.starletteMount(file, syntax, instance, consume, diagnostic)
					if !ok {
						continue
					}
					// A Mount in the routes= list of a FastAPI app
					// composes as app.mount would.
					scope := chainScope(testscope.Classify(file, rules), instance.scopeLabel)
					value, resolution, valid := modules.Prefix(file, c.Scope, prefix)
					if !valid {
						diagnostic(file, "route_prefix_unresolved", "info", prefix.Text, c.Span)
						unknownMounts = append(unknownMounts, fastapiUnknownMount{hop: fastapiCallHop(hopSites, file, p, c, syntax.Expression), from: owner.key, to: target.key, conditional: c.Conditional, scope: scope})
						continue
					}
					site := entrySite{file: file, first: true, line: c.Span.StartLine, column: c.Span.StartColumn}
					mounts = append(mounts, mount{from: owner.key, to: target.key, prefix: value, resolution: resolution, conditional: c.Conditional, scope: scope, site: &site})
					mountDeps = append(mountDeps, fastapiMountDeps{starlette: true})
					continue
				}
				o := r.object(file, c.Scope, c.Receiver, map[string]bool{})
				if o.kind == "" && c.Method == "mount" && c.Receiver.Kind == "attribute" && c.Receiver.Name == "router" && c.Receiver.Object != nil {
					// app.router.mount(...) is app.mount(...): FastAPI.mount
					// calls self.router.mount.
					if inner := r.object(file, c.Scope, *c.Receiver.Object, map[string]bool{}); inner.kind == "app" {
						o = inner
					}
				}
				if o.kind == "" {
					if c.Method == "mount" {
						// The mounting object is not read: an app mounted
						// on it still answers only below that mount.
						if targetExpr, ok := argument(syntax.Expression, "app", 1); ok {
							if target := r.object(file, c.Scope, targetExpr, map[string]bool{}); target.kind == "app" {
								consume(target, chainScope(testscope.Classify(file, rules), instance.scopeLabel))
								diagnostic(file, "route_mount_unresolved", "info", facts.CompactPython(syntax.Expression.Text), c.Span)
							}
						}
					}
					if c.Method == "include_router" {
						// The including object is not read: the router's
						// routes may inherit any default_response_class of
						// the project (row SH-F24). Peek: resolving the
						// router must not register objects the paths and
						// diagnostics would otherwise not see.
						if targetExpr, ok := argument(syntax.Expression, "router", 0); ok {
							r.peek = true
							target := r.object(file, c.Scope, targetExpr, map[string]bool{})
							r.peek = false
							if target.kind != "" {
								ct, st := r.responseDefaults(file, c.Scope, syntax.Expression, c.Span.StartLine, "include_router")
								r.includes = append(r.includes, fastapiInclude{to: target.key, responseClass: ct, responseStatus: st})
							}
						}
					}
					continue
				}
				if c.Method == "websocket" || c.Method == "websocket_route" {
					continue
				}
				if c.Method != "mount" && c.Method != "include_router" {
					if syntax.Decorator || c.Method == "add_api_route" {
						registrations = append(registrations, registration{file, syntax, o})
					}
					continue
				}
				e := syntax.Expression
				targetExpr, ok := argument(e, "router", 0)
				prefix := facts.RouteExpr{Kind: "literal"}
				site := entrySite{file: file, domain: c.Scope, context: instance.context, line: c.Span.StartLine, column: c.Span.StartColumn}
				if c.Method == "mount" {
					prefix, _ = argument(e, "path", 0)
					targetExpr, ok = argument(e, "app", 1)
					// Every mount on a read object takes the requests
					// below its prefix, whatever it mounts.
					value, _, valid := modules.Prefix(file, c.Scope, prefix)
					shadows[o.key] = append(shadows[o.key], fastapiShadow{site: site, prefix: value, valid: valid, text: facts.CompactPython(e.Text)})
				} else if v, present := keyword(e, "prefix"); present {
					prefix = v
				}
				if !ok {
					continue
				}
				target := r.object(file, c.Scope, targetExpr, map[string]bool{})
				if c.Method == "mount" && target.kind != "app" {
					continue
				}
				if c.Method == "mount" {
					consume(target, chainScope(testscope.Classify(file, rules), instance.scopeLabel))
				}
				if c.Method == "mount" && o.kind == "router" {
					// A mount on an APIRouter is served through
					// include_router only from FastAPI 0.137.0,
					// and then below the include's prefix without the
					// router's own prefix. Composed only where the
					// manifest proves that version, and only at a constant
					// prefix (a hop's known part above would carry the
					// router's own prefix).
					if v, _, proven := modules.FastAPIMinimum(file); !proven || !v.AtLeast(routerMountVersion) {
						diagnostic(file, "route_mount_unresolved", "info", facts.CompactPython(e.Text), c.Span)
						continue
					}
					if _, _, valid := modules.Prefix(file, c.Scope, prefix); !valid {
						diagnostic(file, "route_prefix_unresolved", "info", prefix.Text, c.Span)
						continue
					}
				}
				if target.kind == "" {
					diagnostic(file, "route_mount_unresolved", "info", targetExpr.Text, c.Span)
					continue
				}
				if c.Method == "include_router" {
					// Recorded before the prefix is read: an include whose
					// prefix is not read still passes its defaults on.
					ct, st := r.responseDefaults(file, c.Scope, e, c.Span.StartLine, "include_router")
					r.includes = append(r.includes, fastapiInclude{from: o.key, to: target.key, responseClass: ct, responseStatus: st})
				}
				value, resolution, valid := modules.Prefix(file, c.Scope, prefix)
				if !valid {
					diagnostic(file, "route_prefix_unresolved", "info", prefix.Text, c.Span)
					unknownMounts = append(unknownMounts, fastapiUnknownMount{hop: fastapiCallHop(hopSites, file, p, c, e), from: o.key, to: target.key, conditional: c.Conditional, scope: chainScope(testscope.Classify(file, rules), instance.scopeLabel)})
					continue
				}
				mounts = append(mounts, mount{from: o.key, to: target.key, prefix: value, resolution: resolution, conditional: c.Conditional, scope: chainScope(testscope.Classify(file, rules), instance.scopeLabel), site: &site, fromBase: c.Method == "mount" && o.kind == "router"})
				md := fastapiMountDeps{starlette: c.Method == "mount"}
				if !md.starlette {
					md.list = readDependencyList(file, c.Scope, e, "include_router dependencies")
				}
				mountDeps = append(mountDeps, md)
			}
		}
	}
	r.bindings, r.helperFile, r.context = nil, "", ""
	// Resolve each router's own prefix once. An unknown prefix blocks every
	// incoming chain; an app no mount consumes stays a root of its own.
	prefixes := map[string]fastapiPrefix{}
	// A router's own prefix is a hop only for a router something
	// includes: an unincluded router is unmounted, not behind a hop.
	included := map[string]bool{}
	for _, m := range mounts {
		included[m.to] = true
	}
	for _, u := range unknownMounts {
		included[u.to] = true
	}
	var keys []string
	for key := range r.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		o := r.objects[key]
		v := fastapiPrefix{resolution: "literal", valid: true}
		if o.hasPrefix {
			v.text, v.resolution, v.valid = modules.Prefix(o.file, o.scope, o.prefix)
		}
		prefixes[key] = v
		if !v.valid {
			// Constructor locations are kept in the expression for accurate evidence.
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: o.file, Severity: "info", Code: "route_prefix_unresolved", Message: o.prefix.Text + " on router " + key})
			if included[key] && o.kind == "router" {
				unknownMounts = append(unknownMounts, fastapiUnknownMount{hop: fastapiCtorHop(hopSites, files[o.file].Python, o), to: key, own: true, scope: o.scopeLabel})
			}
		}
	}
	paths := map[string][]pathEntry{}
	adjacent := map[string][]mount{}
	for _, m := range mounts {
		adjacent[m.from] = append(adjacent[m.from], m)
	}
	var visit func(string, string, bool, string, testscope.Scope, map[string]bool, []viaStep)
	visit = func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool, via []viaStep) {
		if seen[key] {
			return
		}
		seen[key] = true
		defer delete(seen, key)
		p := prefixes[key]
		if !p.valid {
			return
		}
		base := prefix
		prefix += p.text
		resolution = weaker(resolution, p.resolution)
		paths[key] = append(paths[key], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Scope: scope, via: extendVia(via, viaStep{object: key, base: base})})
		for _, m := range adjacent[key] {
			start := prefix
			if m.fromBase {
				start = base
			}
			visit(m.to, start+m.prefix, conditional || m.conditional, weaker(resolution, m.resolution), chainScope(scope, m.scope), seen, extendVia(via, viaStep{object: key, base: base, site: m.site}))
		}
	}
	// An app a mount consumes is still a root when a serving form proves
	// it is also served on its own; its own-root paths are
	// conditional when every such form is.
	served := r.servedApps(files, rules)
	for _, key := range keys {
		own, isServed := served[key]
		if r.objects[key].kind == "app" && (!mounted[key] || isServed) {
			visit(key, "", mounted[key] && own, "literal", r.objects[key].scopeLabel, map[string]bool{}, nil)
		}
	}
	relative := fastapiRelative(unknownMounts, hopSites, prefixes, adjacent)
	hopsByKey := map[string]*mountHop{}
	for _, h := range hopSites {
		hopsByKey[h.key] = h
	}
	mountsAbove := fastapiHopsAbove(unknownMounts, relative, paths, mounts)
	security := newSecurityLinker(files, modules)
	shapes := newFastAPIShapes(files, modules, security, shapeDepth)
	for _, where := range r.unpackedApps {
		shapes.unpackedApp(where)
	}
	defaults := r.inheritedDefaults(keys)
	dependencyChains := objectChains(r.objects, mounts, mountDeps)
	duplicates := map[string]int{}
	for _, reg := range registrations {
		file, syntax := reg.file, reg.syntax
		c := syntax.Call
		e := syntax.Expression
		// The object's chains from every root (helper contexts included),
		// or its own list when no chain reaches it.
		chains := append([][]dependencyList(nil), dependencyChains[reg.object.key]...)
		for _, key := range keys {
			if r.objects[key].baseKey == reg.object.key {
				chains = append(chains, dependencyChains[key]...)
			}
		}
		if len(chains) == 0 {
			chains = [][]dependencyList{{reg.object.deps}}
		}
		objectDeps := agreeChains(chains)
		ownDeps := readDependencyList(file, c.Scope, e, "route dependencies")
		var securityAttrs map[string]any
		pattern, ok := argument(e, "path", 0)
		if !ok {
			continue
		}
		if pattern.Kind != "literal" {
			diagnostic(file, "route_dynamic_pattern", "warning", pattern.Text, c.Span)
			continue
		}
		methods := []string{strings.ToUpper(c.Method)}
		if c.Method == "api_route" || c.Method == "add_api_route" {
			methods = []string{"GET"}
			if v, present := keyword(e, "methods"); present && !(v.Kind == "none") {
				valid := v.Kind == "list" || v.Kind == "tuple" || v.Kind == "set"
				methods = nil
				unique := map[string]bool{}
				for _, item := range v.Args {
					if item.Kind != "literal" {
						valid = false
						break
					}
					method := strings.ToUpper(item.Name)
					if !unique[method] {
						methods = append(methods, method)
						unique[method] = true
					}
				}
				if !valid {
					diagnostic(file, "route_dynamic_methods", "warning", v.Text, c.Span)
					continue
				}
				// Starlette restricts methods only for a nonempty set; an empty
				// literal collection therefore accepts every HTTP method.
				if len(methods) == 0 {
					methods = []string{"ANY"}
				}
			}
		}
		for _, method := range methods {
			q := method + " " + pattern.Name
			key := file + "#" + q
			duplicates[key]++
			if duplicates[key] > 1 {
				q += fmt.Sprintf("@%d", duplicates[key])
			}
			identity := facts.ID("route", file, q)
			attrs := map[string]any{"framework": "fastapi", "method": method, "pattern": pattern.Name, "handler": nil, "handler_inline": false, "middleware": []string{}}
			registrationScope := testscope.Classify(file, rules)
			if registrationScope == testscope.Test {
				attrs["scope"] = "test"
			}
			handler := syntax.Handler
			if !syntax.Decorator {
				h, found := argument(e, "endpoint", 1)
				if found {
					if h.Kind == "lambda" {
						attrs["handler_inline"] = true
					} else {
						b := modules.Expression(file, c.Scope, h)
						kind, _, _, _ := contract.ParseIdentity(b.Identity)
						if kind == "function" || kind == "method" {
							handler = b.Identity
						} else {
							attrs["handler_expression"] = facts.CompactPython(h.Text)
						}
					}
				}
			}
			if handler != "" {
				attrs["handler"] = handler
				edges = append(edges, facts.Edge("exposes_route", handler, identity, file, c.Span))
			}
			if securityAttrs == nil {
				securityAttrs = security.routeSecurity(objectDeps, ownDeps, handler)
			}
			for key, value := range securityAttrs {
				attrs[key] = value
			}
			var rawTemplates []string
			unique := map[fastapiPathKey]fastapiPathEntry{}
			registrationPaths := append([]pathEntry(nil), paths[reg.object.key]...)
			for _, key := range keys {
				if r.objects[key].baseKey == reg.object.key {
					registrationPaths = append(registrationPaths, paths[key]...)
				}
			}
			regSite := entrySite{file: file, domain: c.Scope, line: c.Span.StartLine, column: c.Span.StartColumn}
			shadowed := map[string]string{}
			for _, base := range registrationPaths {
				base.Scope = chainScope(base.Scope, registrationScope)
				raw := base.Template + pattern.Name
				by, may := fastapiTakenBy(shadows, base.via, regSite, raw)
				if by != "" {
					// A mount added before this route takes every request
					// of the path: not stored on this chain.
					if t := fastapiShownTemplate(raw); shadowed[t] == "" {
						shadowed[t] = by
					}
					continue
				}
				if len(may) > 0 {
					// RO-V1: an earlier mount whose prefix is not read may
					// take the path; it is kept, conditional, with the
					// assumption that it does not.
					base.Conditional = true
					base.Assumptions = []string{mountAssumption}
					base.MayAnswerFirst = goUnion(nil, may)
				}
				rawTemplates = append(rawTemplates, raw)
				base.Template = pythonNormalize(raw)
				p := fastapiPathEntry{pathEntry: base, TrailingSlash: strings.HasSuffix(raw, "/") && base.Template != "/"}
				key := fastapiPathKey{p.Template, p.TrailingSlash}
				if old, found := unique[key]; found {
					p.Conditional = p.Conditional && old.Conditional
					p.Scope = mergedScope(p.Scope, old.Scope)
					if strength(old.Resolution) > strength(p.Resolution) {
						p.Resolution = old.Resolution
					}
					mergeEntryAssumptions(&p.pathEntry, old.pathEntry)
				}
				unique[key] = p
			}
			entries := []fastapiPathEntry{}
			for _, p := range unique {
				entries = append(entries, p)
			}
			sort.Slice(entries, func(i, j int) bool {
				if entries[i].Template != entries[j].Template {
					return entries[i].Template < entries[j].Template
				}
				return !entries[i].TrailingSlash && entries[j].TrailingSlash
			})
			attrs["paths"] = entries
			var withheld []shadowedPath
			for _, t := range sortedStringKeys(shadowed) {
				kept := false
				for _, p := range entries {
					kept = kept || fastapiShownTemplate(p.Template) == t || p.TrailingSlash && p.Template+"/" == t
				}
				if !kept {
					diagnostic(file, routeShadowed, "info", fmt.Sprintf("%s %s taken first by %s", method, t, shadowed[t]), c.Span)
					withheld = append(withheld, shadowedPath{Path: t, By: shadowed[t]})
				}
			}
			if len(withheld) > 0 {
				// The route attribute names the withheld paths, so a view
				// tells a shadowed route from an unmounted
				// one.
				attrs[ShadowedAttribute] = withheld
			}
			if shape, built, problem := shapes.build(shapeInput{file: file, syntax: syntax, handler: handler, templates: rawTemplates, chains: objectDeps, own: ownDeps, security: securityAttrs, pattern: pattern.Name, routeClass: reg.object.routeClass,
				responseClass: defaults(reg.object.key, true), responseStatus: defaults(reg.object.key, false)}); problem != nil {
				diagnostics = append(diagnostics, *problem)
			} else {
				attrs[routeshape.AttributeKey] = shape
				// Model-type uses (row MT-4).
				routeshape.SetModelUses(attrs, built)
			}
			// Unknown-prefix paths (T033, row UP-6): the paths stored
			// below the route's hops.
			scopes := make([]testscope.Scope, 0, len(entries))
			for _, p := range entries {
				scopes = append(scopes, p.Scope)
			}
			production, testOnly := scopedPaths(scopes)
			upState := upRouteState{file: file, method: method, pattern: pattern.Name, hasPaths: production, testPaths: testOnly, hops: hopsByKey}
			if !production {
				var objects []string
				objects = append(objects, reg.object.key)
				for _, key := range keys {
					if r.objects[key].baseKey == reg.object.key {
						objects = append(objects, key)
					}
				}
				upState.belowHop = belowAnyHop(relative, objects...)
				// A route whose every path an earlier route takes first
				// (route_shadowed) is not unmounted and stores no
				// unresolved mount, so nothing below a hop is projected
				// for it (D, not_stored).
				if len(shadowed) == 0 {
					list := fastapiMountEntries(unknownMounts, relative, mountsAbove, objects, pattern.Name, registrationScope)
					switch {
					case len(list) > 0:
						upState.mounts = list
						if len(entries) == 0 {
							attrs[MountAttribute] = list
						}
					case len(entries) == 0:
						// A route reached through a stored hop is mounted,
						// below a prefix the code does not fix: it is not
						// reported unmounted.
						diagnostic(file, "route_unmounted", "info", c.Receiver.Text, c.Span)
					}
				}
			}
			if list, problem := safeWebUnknownPrefix("UP-6", upState); problem != nil {
				diagnostics = append(diagnostics, *problem)
			} else if len(list) > 0 {
				attrs[UnknownPrefixAttribute] = list
			}
			nodes = append(nodes, contract.Node{Type: "node", Identity: identity, Kind: "route", Level: 4, Name: q, QualifiedName: q, File: file, Span: &c.Span, ContentHash: c.Hash, Attributes: attrs})
			edges = append(edges, facts.Edge("contains", c.Owner, identity, file, c.Span))
		}
	}
	return nodes, edges, diagnostics
}

// inheritedDefaults returns, for a registration object's key, why the
// success content type (contentType true) or the default success status
// of its routes is not read (route shape row SH-F24). FastAPI resolves a
// route's response class from the route, the router it is registered on,
// and then each include_router call and including router up to the app
// (fastapi.routing._RouterIncludeContext in 0.161.1): every object that
// includes the route's router, directly or through other routers, can
// supply it. A Starlette mount does not pass it on (the mounted app keeps
// its own). The walk is a union over every include chain, so a nearer
// explicit default never hides a farther one (precision-safe). An include
// whose including object is not read contributes every default of the
// project.
func (r *fastapiResolver) inheritedDefaults(keys []string) func(key string, contentType bool) []string {
	parents := map[string][]fastapiInclude{}
	for _, in := range r.includes {
		parents[in.to] = append(parents[in.to], in)
	}
	pick := func(contentType bool, objectCT, objectStatus string) string {
		if contentType {
			return objectCT
		}
		return objectStatus
	}
	// project is every default of the project, for an unread includer.
	project := func(contentType bool) []string {
		var out []string
		for _, key := range keys {
			o := r.objects[key]
			if why := pick(contentType, o.responseClass, o.responseStatus); why != "" {
				out = append(out, why)
			}
		}
		for _, in := range r.includes {
			if why := pick(contentType, in.responseClass, in.responseStatus); why != "" {
				out = append(out, why)
			}
		}
		return out
	}
	return func(key string, contentType bool) []string {
		var out []string
		add := func(why string) {
			if why != "" && !slices.Contains(out, why) {
				out = append(out, why)
			}
		}
		seen := map[string]bool{}
		queue := []string{key}
		for len(queue) > 0 {
			k := queue[0]
			queue = queue[1:]
			if seen[k] {
				continue
			}
			seen[k] = true
			o := r.objects[k]
			add(pick(contentType, o.responseClass, o.responseStatus))
			// Copies of the object made per helper call (their includes
			// are recorded under the copy's key).
			for _, other := range keys {
				if r.objects[other].baseKey == k {
					queue = append(queue, other)
				}
			}
			for _, in := range parents[k] {
				add(pick(contentType, in.responseClass, in.responseStatus))
				if in.from == "" {
					tagShape("SH-F24/unread includer")
					for _, why := range project(contentType) {
						add(why)
					}
					continue
				}
				queue = append(queue, in.from)
			}
		}
		sort.Strings(out)
		return out
	}
}

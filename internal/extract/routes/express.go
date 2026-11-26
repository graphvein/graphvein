// Package routes composes source-derived route candidates against each current inventory.
package routes

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Factory recognition is keyed exclusively by resolved package and export.
// Extending this table requires a task and a fixture (ADR 0013).
var factories = map[string]map[string]string{
	"express":                {"default": "app", "default.Router": "router", "Router": "router"},
	"express-promise-router": {"default": "router"},
}

type value struct {
	kind, key, file, name string
	scope                 int
	constant              bool
	returns               []facts.RouteExpr
}

type resolver struct {
	files       map[string]facts.Facts
	objects     map[string]string
	modules     *typescript.ModuleResolver
	objectFiles map[string]string
	// majors caches the Express major of each directory (EX5-W12).
	majors map[string]int
	// unscanned is the last unread entry a relative require would load
	// before any scanned candidate (nodeRequire, row CJ-U1): the mount
	// being resolved names it in its route_mount_unresolved diagnostic.
	unscanned string
}

func (r *resolver) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
	data := r.files[file].Routes
	if data == nil {
		return facts.RouteBinding{}, 0, false
	}
	for scope >= 0 && scope < len(data.Scopes) {
		if b, ok := data.Scopes[scope].Bindings[name]; ok {
			return b, scope, true
		}
		scope = data.Scopes[scope].Parent
	}
	return facts.RouteBinding{}, 0, false
}

func (r *resolver) resolve(file string, scope int, e facts.RouteExpr, depth int, seen map[string]bool) value {
	if seen == nil {
		seen = map[string]bool{}
	}
	key := fmt.Sprintf("%s:%d:%d:%s:%s", file, scope, e.Offset, e.Kind, e.Name)
	if seen[key] {
		return value{}
	}
	seen[key] = true
	defer delete(seen, key)
	switch e.Kind {
	case "literal":
		return value{kind: "literal", name: e.Name}
	case "identifier":
		b, at, ok := r.binding(file, scope, e.Name)
		if !ok {
			return value{}
		}
		if b.Module != "" {
			return r.importValue(r.modules.ImportBinding(file, b.Module, b.Export, ""), depth, seen)
		}
		if b.Value.Kind == "function" {
			return value{kind: "function", file: file, scope: b.ReturnScope, returns: b.Returns}
		}
		v := r.resolve(file, at, b.Value, depth, seen)
		v.constant = b.Constant && v.kind == "literal"
		return v
	case "member_expression":
		if e.Object == nil {
			return value{}
		}
		v := r.resolve(file, scope, *e.Object, depth, seen)
		if v.kind == "import" {
			if v.name == "*" {
				v.name = e.Name
			} else {
				v.name += "." + e.Name
			}
			return v
		}
		if v.kind == "namespace" {
			return r.importValue(r.modules.Export(v.file, e.Name), depth, seen)
		}
		if v.kind == "commonjs" {
			return r.commonJSMember(v.file, e.Name, depth, seen)
		}
	case "call_expression":
		if e.Object == nil {
			return value{}
		}
		if e.Object.Kind == "identifier" && e.Object.Name == "require" && len(e.Args) == 1 && e.Args[0].Kind == "literal" {
			if _, _, shadowed := r.binding(file, scope, "require"); !shadowed {
				if target, local := r.requiredFile(file, e.Args[0].Name); local {
					return r.commonJSModule(target, depth, seen)
				}
				return value{kind: "import", file: e.Args[0].Name, name: "default"}
			}
		}
		v := r.resolve(file, scope, *e.Object, depth, seen)
		if v.kind == "import" {
			if kind := factories[v.file][v.name]; kind != "" {
				object := fmt.Sprintf("%s:%d", file, e.Offset)
				r.objects[object] = kind
				r.objectFiles[object] = file
				return value{kind: kind, key: object}
			}
		}
		if v.kind == "function" && depth == 0 && len(v.returns) > 0 {
			var result value
			for i, expression := range v.returns {
				next := r.resolve(v.file, v.scope, expression, depth+1, seen)
				if next.kind != "router" && next.kind != "app" {
					return value{}
				}
				if i > 0 && result.key != next.key {
					return value{}
				}
				result = next
			}
			return result
		}
	}
	return value{}
}

func (r *resolver) importValue(b typescript.ExportBinding, depth int, seen map[string]bool) value {
	if b.External {
		return value{kind: "import", file: b.File, name: b.Name}
	}
	if b.Namespace {
		return value{kind: "namespace", file: b.File}
	}
	if b.File != "" && b.Name != "" {
		return r.resolve(b.File, 0, facts.RouteExpr{Kind: "identifier", Name: b.Name}, depth, seen)
	}
	return value{}
}

func whitespace(s string) string {
	return strings.Map(func(c rune) rune {
		if unicode.IsSpace(c) {
			return -1
		}
		return c
	}, s)
}

// handler uses the existing callable naming, import exports and ambiguity policy.
func (r *resolver) handler(file string, c facts.RouteCall, e facts.RouteExpr) string {
	if e.Kind != "identifier" && e.Kind != "member_expression" {
		return ""
	}
	name := whitespace(e.Text)
	if name == "" {
		name = e.Name
	}
	base := strings.Split(name, ".")[0]
	b, _, exists := r.binding(file, c.Scope, base)
	if exists && b.Module == "" && b.Value.Kind != "function" {
		return ""
	}
	target := ""
	f := r.files[file]
	scope := c.OwnerName
	for {
		q := facts.Join(scope, name)
		for _, n := range f.Nodes {
			if (n.Kind == "function" || n.Kind == "method") && n.QualifiedName == q {
				target = n.Identity
				break
			}
		}
		if target != "" || scope == "" {
			break
		}
		if i := strings.LastIndex(scope, "."); i >= 0 {
			scope = scope[:i]
		} else {
			scope = ""
		}
	}
	if target == "" && exists && b.Module != "" {
		member := ""
		if strings.HasPrefix(name, base+".") {
			member = strings.TrimPrefix(name, base+".")
		}
		target = r.modules.ImportBinding(file, b.Module, b.Export, member).Identity
	}
	for _, f := range r.files {
		for _, group := range f.Repeated {
			for _, identity := range group {
				if identity == target {
					return ""
				}
			}
		}
	}
	kind, _, _, _ := contract.ParseIdentity(target)
	if kind != "function" && kind != "method" {
		return ""
	}
	return target
}

type mount struct {
	from, to, prefix, resolution string
	conditional                  bool
	scope                        testscope.Scope
	// site is the mount or include call on from (entry order);
	// fromBase composes the prefix from the path to from without
	// from's own prefix (a Starlette mount on an APIRouter, FastAPI
	// >= 0.137.0).
	site     *entrySite
	fromBase bool
}

// expressMount is an Express mount with its prefix read under the `use`
// file's Express major (T028); a prefix no row reads composes no path.
type expressMount struct {
	mount
	prefixPaths pathReading
}
type pathEntry struct {
	Template    string          `json:"template"`
	Conditional bool            `json:"conditional"`
	Resolution  string          `json:"resolution"`
	Optional    bool            `json:"optional,omitempty"`
	Scope       testscope.Scope `json:"scope,omitempty"`
	// Above is the known prefix above an unresolved mount hop, on the
	// paths stored below the hop (T029 FU-2, mounts.go).
	Above string `json:"above,omitempty"`
	// Assumptions are the assumption ids the entry rests on: GR-V5 (a Go
	// path served through a wrapper), RO-V1 (a FastAPI mount whose prefix
	// is not read was added before the path's chain) or RO-V2 (an Express
	// app or router mounted earlier runs its own middleware first). Kept
	// on a merged entry only when every chain to it states one.
	Assumptions []string `json:"assumptions,omitempty"`
	// ServedThrough names the wrappers a GR-V5 path is served through.
	ServedThrough []string `json:"served_through,omitempty"`
	// MayAnswerFirst names, for RO-V1 and RO-V2, the entries added
	// before the path that may answer its requests first.
	MayAnswerFirst []string `json:"may_answer_first,omitempty"`
	// via is the chain that composed the entry (entry order);
	// never stored.
	via []viaStep
}

// mergeEntryAssumptions merges the assumptions of two entries for the
// same path: an assumption holds only when every chain to the path
// states one (a chain without it claims the path without the caveat);
// the named wrappers and earlier entries are united.
func mergeEntryAssumptions(old *pathEntry, p pathEntry) {
	if len(old.Assumptions) == 0 || len(p.Assumptions) == 0 {
		old.Assumptions, old.ServedThrough, old.MayAnswerFirst = nil, nil, nil
		return
	}
	old.Assumptions = goUnion(old.Assumptions, p.Assumptions)
	old.ServedThrough = goUnion(old.ServedThrough, p.ServedThrough)
	old.MayAnswerFirst = goUnion(old.MayAnswerFirst, p.MayAnswerFirst)
}

// Link rebuilds route nodes, containment and handler edges from cached candidates.
func Link(files map[string]facts.Facts) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkWithResolver(files, typescript.NewModuleResolver(files, nil))
}

// LinkWithResolver keeps route bindings consistent with import and call edges.
func LinkWithResolver(files map[string]facts.Facts, modules *typescript.ModuleResolver, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	rules := selectedRules(scopeRules)
	r := resolver{files: files, objects: map[string]string{}, objectFiles: map[string]string{}, modules: modules}
	var nodes []contract.Node
	var edges []contract.Edge
	var diagnostics []contract.Diagnostic
	type registration struct {
		file   string
		call   facts.RouteCall
		object value
	}
	var registrations []registration
	var mounts []expressMount
	// unknownMounts are the routers mounted below a prefix that is not
	// a constant (T029 FU-2): their routes' paths below the hop are
	// stored for a user value.
	var unknownMounts []expressUnknownMount
	auth := newExpressAuth(&r)
	// mounted are the apps a `use` consumes (T035): Express
	// serves a mounted app only below its mount (Express 4.21.1 and 5.1.0
	// answer 404 at the sub-app's own paths on the app that mounts it),
	// so it is no root of its own. A mount in test scope does not consume
	// an app outside it.
	mounted := map[string]bool{}
	consume := func(key, file string) {
		if testscope.Classify(file, rules) == testscope.Test && testscope.Classify(r.objectFiles[key], rules) != testscope.Test {
			return
		}
		mounted[key] = true
	}
	diagnostic := func(file, code, severity, expression string, s contract.Span) {
		diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: severity, Code: code, Message: fmt.Sprintf("%s at %d:%d-%d:%d", expression, s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)})
	}
	for _, file := range facts.Paths(files) {
		data := files[file].Routes
		if data == nil {
			continue
		}
		// Discover all apps as roots, even apps without local registrations.
		for scope, s := range data.Scopes {
			names := []string{}
			for name := range s.Bindings {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				r.resolve(file, scope, facts.RouteExpr{Kind: "identifier", Name: name}, 0, nil)
			}
		}
		calls := append([]facts.RouteCall{}, data.Calls...)
		sort.SliceStable(calls, func(i, j int) bool {
			a, b := calls[i].Span, calls[j].Span
			if a.StartLine != b.StartLine {
				return a.StartLine < b.StartLine
			}
			if a.StartColumn != b.StartColumn {
				return a.StartColumn < b.StartColumn
			}
			if a.EndLine != b.EndLine {
				return a.EndLine < b.EndLine
			}
			return a.EndColumn < b.EndColumn
		})
		for _, c := range calls {
			object := r.resolve(file, c.Scope, c.Receiver, 0, nil)
			if object.kind != "app" && object.kind != "router" {
				if c.Method == "use" && r.consumeUnread(file, c, consume) {
					diagnostic(file, "route_mount_unresolved", "info", useText(c), c.Span)
				}
				continue
			}
			if c.Method != "use" {
				registrations = append(registrations, registration{file, c, object})
				continue
			}
			if len(c.Args) == 0 {
				continue
			}
			event := auth.addUse(file, c, object)
			prefix, resolution, start := "", "literal", 0
			if len(c.Args) > 1 {
				first := r.resolve(file, c.Scope, c.Args[0], 0, nil)
				if first.kind != "app" && first.kind != "router" {
					start = 1
					if first.kind != "literal" || (c.Args[0].Kind != "literal" && !first.constant) {
						diagnostic(file, "route_prefix_unresolved", "info", c.Args[0].Text, c.Span)
						unknownMounts = append(unknownMounts, r.unknownMount(file, c, object.key, rules, consume)...)
						continue
					}
					prefix = first.name
					if c.Args[0].Kind != "literal" {
						resolution = "constant"
					}
				}
			}
			prefixPaths := pathReading{alternatives: []string{""}}
			if start == 1 {
				prefixPaths = readExpressPath(prefix, r.expressMajor(file), false)
				if prefixPaths.refusal != "" {
					diagnostic(file, prefixPaths.refusal, refusalSeverity(prefixPaths.refusal), prefixPaths.reason, c.Span)
				}
			}
			for i, argument := range c.Args[start:] {
				r.unscanned = ""
				target := r.resolve(file, c.Scope, argument, 0, nil)
				if target.kind != "app" && target.kind != "router" {
					text := argument.Text
					if r.unscanned != "" {
						// Row CJ-U1: Node loads a module the
						// scan did not read.
						text += " (loads " + r.unscanned + ", not scanned)"
					}
					diagnostic(file, "route_mount_unresolved", "info", text, c.Span)
					continue
				}
				if target.kind == "app" {
					consume(target.key, file)
				}
				mounts = append(mounts, expressMount{mount{from: object.key, to: target.key, prefix: prefix, resolution: resolution, conditional: c.Conditional, scope: testscope.Classify(file, rules)}, prefixPaths})
				auth.addSite(object.key, target.key, event, start+i, prefix)
			}
		}
	}
	auth.addEscapes(auth.inlineCalls())
	auth.sortEvents()
	// Traverse each root independently. A branch-local visited set terminates cycles
	// without discarding another mount of the same router under a different prefix.
	paths := map[string][]pathEntry{}
	// blocked names, per object, the mount prefixes no row reads that a
	// chain reached it through (T028 review).
	blocked := map[string][]string{}
	var visit func(string, string, bool, bool, string, testscope.Scope, map[string]bool)
	visit = func(object, prefix string, optional, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool) {
		if seen[object] {
			return
		}
		seen[object] = true
		defer delete(seen, object)
		paths[object] = append(paths[object], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Optional: optional, Scope: scope})
		for _, m := range mounts {
			if m.from == object {
				res := resolution
				if m.resolution == "constant" {
					res = "constant"
				}
				// A prefix no row reads composes no path (EX5-W10, EX5-W14).
				if m.prefixPaths.refusal != "" {
					blocked[m.to] = append(blocked[m.to], m.prefix)
				}
				for _, alternative := range m.prefixPaths.alternatives {
					visit(m.to, prefix+"/"+alternative, optional || m.prefixPaths.optional, conditional || m.conditional, res, chainScope(scope, m.scope), seen)
				}
			}
		}
	}
	// The paths below each unknown mount, composed from its router as
	// from a root through the same mounts.
	var hops []*mountHop
	for _, u := range unknownMounts {
		hops = append(hops, u.hop)
	}
	assignMountKeys(hops)
	hopsByKey := hopIndex(hops)
	relative := make([]map[string][]pathEntry, len(unknownMounts))
	for i, u := range unknownMounts {
		into := map[string][]pathEntry{}
		var walk func(object, prefix string, optional, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool)
		walk = func(object, prefix string, optional, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool) {
			if seen[object] {
				return
			}
			seen[object] = true
			defer delete(seen, object)
			into[object] = append(into[object], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Optional: optional, Scope: scope})
			for _, m := range mounts {
				if m.from != object {
					continue
				}
				res := resolution
				if m.resolution == "constant" {
					res = "constant"
				}
				for _, alternative := range m.prefixPaths.alternatives {
					walk(m.to, prefix+"/"+alternative, optional || m.prefixPaths.optional, conditional || m.conditional, res, chainScope(scope, m.scope), seen)
				}
			}
		}
		walk(u.to, "", false, u.conditional, "literal", u.scope, map[string]bool{})
		relative[i] = into
	}
	// An app a `use` consumes is still a root when a serving form proves
	// it is also served on its own; its own-root paths are
	// conditional when every such form is.
	served := r.servedApps(rules)
	rootKeys := []string{}
	for key, kind := range r.objects {
		if _, own := served[key]; kind == "app" && (!mounted[key] || own) {
			rootKeys = append(rootKeys, key)
		}
	}
	sort.Strings(rootKeys)
	for _, root := range rootKeys {
		conditional := false
		if mounted[root] {
			conditional = served[root]
		}
		visit(root, "", false, conditional, "literal", testscope.Classify(r.objectFiles[root], rules), map[string]bool{})
	}
	mountsAbove := expressHopsAbove(unknownMounts, relative, paths, blocked)
	// Route layers: every registration is a layer of its router's stack
	// that may run before a later route (DA-E1 earlier route handlers);
	// steps of one `route(p)` chain share a layer (DA-E4).
	sites := make([]routeSite, 0, len(registrations))
	for _, reg := range registrations {
		sites = append(sites, routeSite{reg.file, reg.call})
	}
	chained := routeChains(sites)
	for _, reg := range registrations {
		auth.addRouteLayer(reg.file, reg.call, reg.object.key, chained[chainKey(reg.file, reg.call)])
	}
	duplicates := map[string]int{}
	firstRegistration := map[string]contract.Node{}
	for _, reg := range registrations {
		file, c := reg.file, reg.call
		args := c.Args
		pattern := c.Pattern
		step := chained[chainKey(file, c)]
		if step.group != "" && c.Method == "all" && step.later {
			// DA-E4: a `route(p).all(f...)` step chained before another
			// method is that method's route middleware, not a route.
			continue
		}
		if pattern == nil {
			if len(args) == 0 {
				continue
			}
			p := args[0]
			pattern = &p
			args = args[1:]
		}
		if pattern.Kind != "literal" {
			diagnostic(file, "route_dynamic_pattern", "warning", pattern.Text, c.Span)
			continue
		}
		method := strings.ToUpper(c.Method)
		if c.Method == "all" {
			method = "ANY"
		}
		q := method + " " + pattern.Name
		key := file + "#" + q
		duplicates[key]++
		if duplicates[key] > 1 {
			q += fmt.Sprintf("@%d", duplicates[key])
		}
		identity := facts.ID("route", file, q)
		attrs := map[string]any{"framework": "express", "method": method, "pattern": pattern.Name, "handler": nil, "handler_inline": false}
		middleware := []string{}
		// DA-E4: the handlers of `all` steps chained before this method on
		// the same `route(p)` run first, in chain order (Express Route
		// dispatch); they lead the route's own middleware.
		var chainedMiddleware []facts.RouteExpr
		for _, before := range step.before {
			if before.Method == "all" {
				tagAuth("DA-E4/all")
				chainedMiddleware = append(chainedMiddleware, before.Args...)
			}
		}
		own := chainedMiddleware
		if len(args) > 0 {
			h := args[len(args)-1]
			inline := h.Kind == "arrow_function" || h.Kind == "function_expression"
			attrs["handler_inline"] = inline
			if !inline {
				if handler := r.handler(file, c, h); handler != "" {
					attrs["handler"] = handler
					edges = append(edges, facts.Edge("exposes_route", handler, identity, file, c.Span))
				} else {
					attrs["handler_expression"] = whitespace(h.Text)
				}
			}
			own = append(append([]facts.RouteExpr(nil), chainedMiddleware...), args[:len(args)-1]...)
		}
		for _, m := range own {
			middleware = append(middleware, whitespace(m.Text))
		}
		if declared := r.routeDeclares(file, c.Scope, own); len(declared) > 0 {
			attrs["middleware_declares"] = declared
		}
		attrs["middleware"] = middleware
		mounted, unknown, errorHandlers, answerFirst := auth.routeMiddleware(file, c, reg.object.key, method, pattern.Name, step)
		if len(mounted) > 0 {
			attrs["mounted_middleware"] = mounted
		}
		if len(unknown) > 0 {
			attrs["mounted_middleware_unknown"] = unknown
		}
		if len(errorHandlers) > 0 {
			attrs["mounted_error_handlers"] = errorHandlers
		}
		duplicateKey := file + "\x00" + reg.object.key + "\x00" + method + "\x00" + pattern.Name
		if first, exists := firstRegistration[duplicateKey]; exists {
			identical := first.Attributes["handler"] != nil && attrs["handler"] == first.Attributes["handler"] && attrs["handler_inline"] == false && first.Attributes["handler_inline"] == false
			sameMiddleware := fmt.Sprint(first.Attributes["middleware"]) == fmt.Sprint(middleware)
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "route_duplicate_registration", Message: fmt.Sprintf("%s %s registrations at %d:%d-%d:%d and %d:%d-%d:%d; handler identical=%t, middleware identical=%t", method, pattern.Name, first.Span.StartLine, first.Span.StartColumn, first.Span.EndLine, first.Span.EndColumn, c.Span.StartLine, c.Span.StartColumn, c.Span.EndLine, c.Span.EndColumn, identical, sameMiddleware)})
		} else {
			firstRegistration[duplicateKey] = contract.Node{Span: &c.Span, Attributes: attrs}
		}
		registrationScope := testscope.Classify(file, rules)
		if registrationScope == testscope.Test {
			attrs["scope"] = "test"
		}
		entries := []pathEntry{}
		unique := map[string]int{}
		// The pattern is read under its file's Express major (T028); a
		// pattern no row reads gets no path.
		read := readExpressPath(pattern.Name, r.expressMajor(file), true)
		if read.refusal != "" {
			diagnostic(file, read.refusal, refusalSeverity(read.refusal), read.reason, c.Span)
		}
		var composed []pathEntry
		for _, p := range paths[reg.object.key] {
			for _, alternative := range read.alternatives {
				composed = append(composed, pathEntry{Template: slashes(p.Template + "/" + alternative), Conditional: p.Conditional, Resolution: p.Resolution, Optional: p.Optional || read.optional, Scope: chainScope(p.Scope, registrationScope)})
			}
		}
		for _, entry := range composed {
			full := entry.Template
			// An unconditional/literal chain proves the same template without the
			// qualifications needed by another root. Keep one entry per template.
			if at, found := unique[full]; found {
				entries[at].Conditional = entries[at].Conditional && entry.Conditional
				entries[at].Scope = mergedScope(entries[at].Scope, entry.Scope)
				if entry.Resolution == "literal" {
					entries[at].Resolution = "literal"
				}
				entries[at].Optional = entries[at].Optional || entry.Optional
			} else {
				unique[full] = len(entries)
				entries = append(entries, entry)
			}
		}
		if len(answerFirst) > 0 {
			// RO-V2: an app or router mounted
			// earlier below a prefix that may match runs its own
			// middleware first, and a catch-all among it answers the
			// request (Express 4.21.1 and 5.1.0: 404 at a later parent
			// route below the mount). The paths are kept, conditional,
			// with the assumption that it passes the request on; never
			// dropped (middleware that calls next() lets the route
			// answer). The mark is per route: a chain is not tied to a
			// path entry, so every path carries it.
			for i := range entries {
				entries[i].Conditional = true
				entries[i].Assumptions = []string{catchAllAssumption}
				entries[i].MayAnswerFirst = answerFirst
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			a, b := entries[i], entries[j]
			if a.Template != b.Template {
				return a.Template < b.Template
			}
			if a.Conditional != b.Conditional {
				return !a.Conditional
			}
			return a.Resolution < b.Resolution
		})
		attrs["paths"] = entries
		scopes := make([]testscope.Scope, 0, len(entries))
		for _, p := range entries {
			scopes = append(scopes, p.Scope)
		}
		production, testOnly := scopedPaths(scopes)
		var hopMounts []mountEntry
		if !production && read.refusal == "" {
			if list := expressMountEntries(unknownMounts, relative, mountsAbove, reg.object.key, read, registrationScope); len(list) > 0 {
				hopMounts = list
				if len(entries) == 0 {
					attrs[MountAttribute] = list
				}
			}
		}
		// Unknown-prefix paths (T033, row UP-5): the paths stored below
		// the route's hops, unless a pattern or prefix on its chain is
		// one no row reads.
		upState := upRouteState{file: file, method: method, pattern: pattern.Name, hasPaths: production, testPaths: testOnly, mounts: hopMounts, hops: hopsByKey, belowHop: belowAnyHop(relative, reg.object.key)}
		if read.refusal != "" || len(blocked[reg.object.key]) > 0 {
			upState.refused = upDefaultUnread
		}
		if list, problem := safeWebUnknownPrefix("UP-5", upState); problem != nil {
			diagnostics = append(diagnostics, *problem)
		} else if len(list) > 0 {
			attrs[UnknownPrefixAttribute] = list
		}
		if len(entries) == 0 && read.refusal == "" {
			if prefixes := blocked[reg.object.key]; len(prefixes) > 0 {
				// The router is mounted, but only below prefixes no row
				// reads: name them rather than calling it unmounted.
				diagnostic(file, routePatternUnmodelled, "info", fmt.Sprintf("%s mounted only below the unread mount prefix(es) %q", c.Receiver.Text, uniqueSorted(prefixes)), c.Span)
			} else if _, viaHop := attrs[MountAttribute]; !viaHop {
				// A route reached through a stored hop is mounted, below
				// a prefix the code does not fix: not
				// unmounted.
				diagnostic(file, "route_unmounted", "info", c.Receiver.Text, c.Span)
			}
		}
		nodes = append(nodes, contract.Node{Type: "node", Identity: identity, Kind: "route", Level: 4, Name: q, QualifiedName: q, File: file, Span: &c.Span, ContentHash: c.Hash, Attributes: attrs})
		edges = append(edges, facts.Edge("contains", c.Owner, identity, file, c.Span))
	}
	equivalentRegistrations(nodes, ExpressEquivalence(files, modules, nodes))
	return nodes, edges, diagnostics
}

// equivalentRegistrations stores ADR 0022's equivalence proof as the route
// attribute equivalent_registrations (ADR 0075 "Link pass"), so a matcher
// that reads stored nodes alone (the project link pass) can select the
// first registration exactly as the in-repository linker does. Every route
// of a group of two or more registrations with one proof gets the group's
// identities in source order; a route without such a group gets nothing.
// The derived routing object key itself stays internal.
func equivalentRegistrations(nodes []contract.Node, proof map[string]string) {
	groups := map[string][]int{}
	for i, n := range nodes {
		if key, ok := proof[n.Identity]; ok {
			groups[key] = append(groups[key], i)
		}
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(a, b int) bool { return spanBefore(*nodes[group[a]].Span, *nodes[group[b]].Span) })
		ids := make([]string, 0, len(group))
		for _, i := range group {
			ids = append(ids, nodes[i].Identity)
		}
		for _, i := range group {
			nodes[i].Attributes["equivalent_registrations"] = ids
		}
	}
}

// spanBefore orders spans by start, then end, as registrations are read.
func spanBefore(a, b contract.Span) bool {
	left := [4]int{a.StartLine, a.StartColumn, a.EndLine, a.EndColumn}
	right := [4]int{b.StartLine, b.StartColumn, b.EndLine, b.EndColumn}
	for i := range left {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

// ExpressEquivalence supplies router proof to the matcher. The derived
// routing object key stays internal; the groups it proves are stored as
// equivalent_registrations (see equivalentRegistrations).
func ExpressEquivalence(files map[string]facts.Facts, modules *typescript.ModuleResolver, nodes []contract.Node) map[string]string {
	r := resolver{files: files, objects: map[string]string{}, objectFiles: map[string]string{}, modules: modules}
	proof := map[string]string{}
	for _, n := range nodes {
		if n.Attributes["framework"] != "express" || n.Attributes["handler"] == nil || n.Attributes["handler_inline"] != false || n.Span == nil {
			continue
		}
		d := files[n.File].Routes
		if d == nil {
			continue
		}
		for _, c := range d.Calls {
			method := strings.ToUpper(c.Method)
			if c.Method == "all" {
				method = "ANY"
			}
			if c.Span != *n.Span || method != n.Attributes["method"] {
				continue
			}
			v := r.resolve(n.File, c.Scope, c.Receiver, 0, nil)
			if v.kind == "app" || v.kind == "router" {
				proof[n.Identity] = fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%q", n.File, v.key, n.Attributes["method"], n.Attributes["pattern"], n.Attributes["handler"], n.Attributes["middleware"])
			}
		}
	}
	return proof
}

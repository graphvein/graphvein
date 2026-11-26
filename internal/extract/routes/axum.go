package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/rust"
	"github.com/graphvein/graphvein/internal/testscope"
)

type axumObject struct {
	key         string
	conditional bool
}
type axumFunction struct {
	file   string
	syntax facts.RustFunction
}
type axumRegistration struct {
	file, owner string
	object      axumObject
	pattern     facts.RustExpr
	verb        facts.RustExpr
	method      string
	handler     facts.RustExpr
	service     bool
	seq         int
	// methodLayers are the MethodRouter `.layer` / `.route_layer` calls
	// that wrap this method's endpoint, innermost first (DA-X3).
	methodLayers []facts.RustExpr
}

type axumPendingMount struct {
	file, from, target string
	prefix             facts.RustExpr
	conditional        bool
	span               contract.Span
	expression         string
	seq                int
	via                string
	// owner and call are the nest's function and call, for its hop
	// when the prefix is not a constant (T029 FU-3).
	owner string
	call  facts.RustExpr
}
type axumLinker struct {
	modules       *rust.ModuleResolver
	functions     map[string]axumFunction
	completed     map[string]axumObject
	active        map[string]bool
	objects       map[string]bool
	objectFiles   map[string]string
	rules         testscope.Rules
	processed     map[string]bool
	registrations []axumRegistration
	mounts        []mount
	consumed      map[string]bool
	pending       []axumPendingMount
	diagnostics   []contract.Diagnostic
	// seq orders registrations, layers and mounts as the source applies
	// them (AG-B4b): a layer covers what its router holds at that point.
	seq    int
	layers []axumLayer
	edges  []axumEdge
	// unknown are the routers nested below a prefix that is not a
	// constant (T029 FU-3, axum_mounts.go).
	unknown []axumUnknownMount
}

func (r *axumLinker) diagnostic(file, code, expression string, span contract.Span) {
	severity := "info"
	if code == "route_dynamic_pattern" || code == "route_dynamic_methods" {
		severity = "warning"
	}
	r.diagnostics = append(r.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: code, Severity: severity, Message: fmt.Sprintf("%s at %d:%d-%d:%d", expression, span.StartLine, span.StartColumn, span.EndLine, span.EndColumn)})
}

func externalAxum(b rust.Binding, name string) bool {
	return b.External == "axum" && b.Name == name && !b.Ambiguous
}

func (r *axumLinker) filter(file string, e facts.RustExpr) ([]string, bool) {
	if e.Kind == "or" && len(e.Args) == 2 {
		a, ok := r.filter(file, e.Args[0])
		b, valid := r.filter(file, e.Args[1])
		return append(a, b...), ok && valid
	}
	if e.Kind != "path" {
		return nil, false
	}
	b := r.modules.Resolve(file, e.Scope, e.Name)
	const prefix = "routing::MethodFilter::"
	if b.External != "axum" || !strings.HasPrefix(b.Name, prefix) {
		return nil, false
	}
	verb := strings.TrimPrefix(b.Name, prefix)
	if !facts.Builtin(verb, "GET POST PUT PATCH DELETE HEAD OPTIONS TRACE CONNECT") {
		return nil, false
	}
	return []string{verb}, true
}

func (r *axumLinker) methods(file string, e facts.RustExpr) ([]axumRegistration, bool) {
	if e.Kind == "method" && e.Object != nil {
		verbs, ok := r.methods(file, *e.Object)
		if !ok {
			return nil, false
		}
		if facts.Builtin(e.Name, "get post put patch delete head options trace connect any on get_service post_service put_service patch_service delete_service head_service options_service trace_service connect_service any_service on_service") {
			more, valid := r.verb(file, e, e.Name)
			return append(verbs, more...), valid
		}
		if (e.Name == "layer" || e.Name == "route_layer") && len(e.Args) == 1 {
			// DA-X3: MethodRouter::layer and ::route_layer map over the
			// endpoints the method router holds at the call (axum 0.7.9
			// and 0.8.8 method_routing.rs), so they wrap the methods
			// chained before them, never later ones.
			for i := range verbs {
				verbs[i].methodLayers = append(verbs[i].methodLayers, e)
			}
		}
		return verbs, true
	}
	if e.Kind != "call" {
		return nil, false
	}
	b := r.modules.Resolve(file, e.Scope, e.Name)
	if b.External != "axum" || !strings.HasPrefix(b.Name, "routing::") {
		return nil, false
	}
	return r.verb(file, e, strings.TrimPrefix(b.Name, "routing::"))
}

func (r *axumLinker) verb(file string, e facts.RustExpr, name string) ([]axumRegistration, bool) {
	service := strings.HasSuffix(name, "_service")
	name = strings.TrimSuffix(name, "_service")
	if !facts.Builtin(name, "get post put patch delete head options trace connect any on") {
		return nil, false
	}
	methods := []string{strings.ToUpper(name)}
	index := 0
	if name == "on" {
		if len(e.Args) < 2 {
			return nil, false
		}
		var ok bool
		methods, ok = r.filter(file, e.Args[0])
		if !ok {
			return nil, false
		}
		index = 1
	}
	if len(e.Args) <= index {
		return nil, false
	}
	var result []axumRegistration
	unique := map[string]bool{}
	for _, method := range methods {
		if !unique[method] {
			unique[method] = true
			result = append(result, axumRegistration{verb: e, method: method, handler: e.Args[index], service: service})
		}
	}
	return result, true
}

func findObject(env []map[string]axumObject, name string) (axumObject, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		if o, ok := env[i][name]; ok {
			return o, true
		}
	}
	return axumObject{}, false
}

func (r *axumLinker) expression(file, owner string, e facts.RustExpr, env []map[string]axumObject) axumObject {
	if e.Kind == "path" {
		o, _ := findObject(env, e.Name)
		return o
	}
	if e.Kind == "call" {
		b := r.modules.Resolve(file, e.Scope, e.Name)
		if externalAxum(b, "Router::new") || externalAxum(b, "Router::default") {
			key := fmt.Sprintf("%s:%d", file, e.Offset)
			r.objects[key] = true
			r.objectFiles[key] = file
			return axumObject{key, e.Conditional}
		}
		if b.Identity != "" && !b.Ambiguous {
			return r.function(b.Identity)
		}
		return axumObject{}
	}
	if e.Kind != "method" || e.Object == nil {
		return axumObject{}
	}
	o := r.expression(file, owner, *e.Object, env)
	if o.key == "" {
		return o
	}
	key := fmt.Sprintf("%s:%d:%d:%s", file, e.Span.StartLine, e.Span.StartColumn, e.Name)
	if r.processed[key] {
		return o
	}
	r.processed[key] = true
	conditional := o.conditional || e.Conditional
	switch e.Name {
	case "route", "route_service":
		if len(e.Args) < 2 {
			return o
		}
		pattern := e.Args[0]
		if _, _, ok := r.modules.Prefix(file, pattern); !ok {
			r.diagnostic(file, "route_dynamic_pattern", pattern.Text, e.Span)
			return o
		}
		var verbs []axumRegistration
		if e.Name == "route_service" {
			verbs = []axumRegistration{{method: "ANY", verb: e, handler: e.Args[1], service: true}}
		} else {
			var ok bool
			verbs, ok = r.methods(file, e.Args[1])
			if !ok {
				r.diagnostic(file, "route_dynamic_methods", e.Args[1].Text, e.Span)
				return o
			}
		}
		r.seq++
		for _, v := range verbs {
			v.seq = r.seq
			v.file = file
			v.owner = owner
			v.object = o
			v.object.conditional = conditional
			v.pattern = pattern
			r.registrations = append(r.registrations, v)
		}
	case "merge", "nest", "nest_service":
		index := 0
		prefix := facts.RustExpr{Kind: "literal"}
		if e.Name != "merge" {
			index = 1
			if len(e.Args) > 0 {
				prefix = e.Args[0]
			}
		}
		if len(e.Args) <= index {
			return o
		}
		target := r.expression(file, owner, e.Args[index], env)
		if target.key == "" {
			arg := e.Args[index]
			if arg.Kind == "call" {
				b := r.modules.Resolve(file, arg.Scope, arg.Name)
				if _, known := r.functions[b.Identity]; known && r.active[b.Identity] {
					r.seq++
					r.pending = append(r.pending, axumPendingMount{file, o.key, b.Identity, prefix, conditional, e.Span, arg.Text, r.seq, e.Name, owner, e})
					return o
				}
			}
			r.diagnostic(file, "route_mount_unresolved", e.Args[index].Text, e.Span)
			if e.Name == "nest_service" {
				// The service may wrap routers: they are served below
				// the prefix, not at the root (T029 FU-3d).
				r.consumeWithin(file, owner, e.Args[index], env)
			}
			return o
		}
		r.consumed[target.key] = true
		value, resolution, ok := r.modules.Prefix(file, prefix)
		if !ok {
			r.diagnostic(file, "route_prefix_unresolved", prefix.Text, e.Span)
			r.unknownNest(file, owner, e, o.key, target.key, conditional || target.conditional)
			return o
		}
		if e.Name == "nest_service" {
			value = serviceMark + value
		}
		r.mounts = append(r.mounts, mount{from: o.key, to: target.key, prefix: value, resolution: resolution, conditional: conditional || target.conditional, scope: testscope.Classify(file, r.rules)})
		r.edge(o.key, target.key, e.Name)
	case "layer", "route_layer":
		r.layer(file, o.key, e)
	}
	return o
}

func (r *axumLinker) statements(file, owner string, statements []facts.RustStatement, env []map[string]axumObject) []axumObject {
	var returned []axumObject
	for _, s := range statements {
		if s.Kind == "block" {
			child := r.statements(file, owner, s.Children, append(env, map[string]axumObject{}))
			returned = append(returned, child...)
			continue
		}
		o := r.expression(file, owner, s.Expr, env)
		o.conditional = o.conditional || s.Conditional
		switch s.Kind {
		case "let":
			env[len(env)-1][strings.TrimPrefix(s.Name, "mut ")] = o
		case "assign":
			for i := len(env) - 1; i >= 0; i-- {
				if _, ok := env[i][s.Name]; ok {
					env[i][s.Name] = o
					break
				}
			}
		case "expr", "return":
			if o.key != "" {
				// Conditional registrations/rebindings already annotate their own
				// graph steps. Only a conditional return adds a factory-wide step.
				o.conditional = s.Conditional
				returned = append(returned, o)
			}
		}
	}
	return returned
}

func (r *axumLinker) function(identity string) axumObject {
	if o, ok := r.completed[identity]; ok {
		return o
	}
	if r.active[identity] {
		return axumObject{}
	}
	f, ok := r.functions[identity]
	if !ok {
		return axumObject{}
	}
	r.active[identity] = true
	returned := r.statements(f.file, identity, f.syntax.Body, []map[string]axumObject{{}})
	o := axumObject{}
	if len(returned) > 0 {
		// One logical object per factory also preserves distinct conditional
		// return branches. The virtual object never becomes a contract node.
		o.key = "factory:" + identity
		r.objects[o.key] = true
		r.objectFiles[o.key] = f.file
		for _, v := range returned {
			r.consumed[v.key] = true
			r.mounts = append(r.mounts, mount{from: o.key, to: v.key, resolution: "literal", conditional: v.conditional, scope: testscope.Classify(f.file, r.rules)})
			r.edge(o.key, v.key, "return")
		}
	}
	delete(r.active, identity)
	r.completed[identity] = o
	return o
}

func axumNormalize(text string) string {
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == '/' })
	for i, p := range parts {
		if strings.HasPrefix(p, ":") && len(p) > 1 {
			parts[i] = "{" + p[1:] + "}"
		}
		if strings.HasPrefix(p, "*") && len(p) > 1 {
			parts[i] = "{*" + p[1:] + "}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

// LinkAxum composes source-backed registrations using the shared Rust resolver.
func LinkAxum(files map[string]facts.Facts, modules *rust.ModuleResolver, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkAxumShapes(files, modules, nil, routeshape.DefaultDepth, scopeRules...)
}

// LinkAxumShapes is LinkAxum with the run's route shape depth limit
// (index.shape_depth, routeshape row SM-21) and the retained sources,
// whose Cargo.toml and Cargo.lock files give each crate's axum version
// (rows SH-X11, SH-X25): every route gets the shape attribute of task
// T029 (axumshape.go). Without sources no version is proven.
func LinkAxumShapes(files map[string]facts.Facts, modules *rust.ModuleResolver, sources map[string][]byte, shapeDepth int, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	shaper := newAxumShaper(files, modules, shapeDepth)
	shaper.versions = &axumVersions{sources: sources, cache: map[string]versionRange{}}
	r := axumLinker{modules: modules, functions: map[string]axumFunction{}, completed: map[string]axumObject{}, active: map[string]bool{}, objects: map[string]bool{}, processed: map[string]bool{}, consumed: map[string]bool{}}
	r.rules, r.objectFiles = selectedRules(scopeRules), map[string]string{}
	var ids []string
	for _, file := range facts.Paths(files) {
		if f := files[file].Rust; f != nil {
			for _, fn := range f.Functions {
				if fn.Identity != "" {
					r.functions[fn.Identity] = axumFunction{file, fn}
					ids = append(ids, fn.Identity)
				}
			}
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		r.function(id)
	}
	for _, p := range r.pending {
		target := r.completed[p.target]
		if target.key == "" {
			r.diagnostic(p.file, "route_mount_unresolved", p.expression, p.span)
			continue
		}
		r.consumed[target.key] = true
		value, resolution, ok := modules.Prefix(p.file, p.prefix)
		if !ok {
			r.diagnostic(p.file, "route_prefix_unresolved", p.prefix.Text, p.span)
			r.unknownNest(p.file, p.owner, p.call, p.from, target.key, p.conditional || target.conditional)
			continue
		}
		if p.via == "nest_service" {
			value = serviceMark + value
		}
		r.mounts = append(r.mounts, mount{from: p.from, to: target.key, prefix: value, resolution: resolution, conditional: p.conditional || target.conditional, scope: testscope.Classify(p.file, r.rules)})
		r.edges = append(r.edges, axumEdge{from: p.from, to: target.key, seq: p.seq, via: p.via})
	}
	adjacent := map[string][]mount{}
	for _, m := range r.mounts {
		adjacent[m.from] = append(adjacent[m.from], m)
	}
	relative := axumRelative(r.unknown, adjacent)
	paths := map[string][]pathEntry{}
	var visit func(string, string, bool, string, testscope.Scope, map[string]bool)
	visit = func(key, prefix string, conditional bool, resolution string, scope testscope.Scope, seen map[string]bool) {
		if seen[key] {
			return
		}
		seen[key] = true
		defer delete(seen, key)
		paths[key] = append(paths[key], pathEntry{Template: prefix, Conditional: conditional, Resolution: resolution, Scope: scope})
		for _, m := range adjacent[key] {
			visit(m.to, axumJoinPrefix(prefix, m.prefix), conditional || m.conditional, weaker(resolution, m.resolution), chainScope(scope, m.scope), seen)
		}
	}
	var roots []string
	for key := range r.objects {
		if !r.consumed[key] {
			roots = append(roots, key)
		}
	}
	sort.Strings(roots)
	for _, root := range roots {
		visit(root, "", false, "literal", testscope.Classify(r.objectFiles[root], r.rules), map[string]bool{})
	}
	mountsAbove := axumHopsAbove(r.unknown, relative, paths)
	hopsByKey := map[string]*mountHop{}
	for _, u := range r.unknown {
		hopsByKey[u.hop.key] = u.hop
	}
	// Declaration ordering is independent of factory traversal order.
	sort.SliceStable(r.registrations, func(i, j int) bool {
		a, b := r.registrations[i], r.registrations[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.verb.Span.StartLine != b.verb.Span.StartLine {
			return a.verb.Span.StartLine < b.verb.Span.StartLine
		}
		return a.verb.Span.StartColumn < b.verb.Span.StartColumn
	})
	var nodes []contract.Node
	var edges []contract.Edge
	duplicates := map[string]int{}
	for _, reg := range r.registrations {
		pattern, patternResolution, _ := modules.Prefix(reg.file, reg.pattern)
		q := reg.method + " " + pattern
		key := reg.file + "#" + q
		duplicates[key]++
		if duplicates[key] > 1 {
			q += fmt.Sprintf("@%d", duplicates[key])
		}
		identity := facts.ID("route", reg.file, q)
		attrs := map[string]any{"framework": "axum", "method": reg.method, "pattern": pattern, "handler": nil, "handler_inline": false, "middleware": []string{}}
		registrationScope := testscope.Classify(reg.file, r.rules)
		if registrationScope == testscope.Test {
			attrs["scope"] = "test"
		}
		if !reg.service && reg.handler.Kind == "closure_expression" {
			attrs["handler_inline"] = true
		} else {
			b := rust.Binding{}
			if !reg.service && reg.handler.Kind == "path" {
				b = modules.Resolve(reg.file, reg.handler.Scope, reg.handler.Name)
			}
			kind, _, _, _ := contract.ParseIdentity(b.Identity)
			if (kind == "function" || kind == "method") && !b.Ambiguous {
				attrs["handler"] = b.Identity
				edges = append(edges, facts.Edge("exposes_route", b.Identity, identity, reg.file, reg.verb.Span))
			} else {
				attrs["handler_expression"] = strings.Join(strings.Fields(reg.handler.Text), "")
			}
		}
		// Trailing slashes are kept as axum serves them for a proven
		// version (axum_slash.go); otherwise such a path is not claimed.
		proven := axumSlashProven(shaper.versions.of(reg.file))
		unique := map[fastapiPathKey]fastapiPathEntry{}
		dropped := 0
		for _, base := range paths[reg.object.key] {
			formed, ok := axumPaths(base.Template, pattern, proven)
			if !ok {
				dropped++
				continue
			}
			for _, f := range formed {
				p := fastapiPathEntry{pathEntry: base, TrailingSlash: f.slash}
				p.Scope = chainScope(p.Scope, registrationScope)
				p.Template = f.template
				p.Resolution = weaker(p.Resolution, patternResolution)
				p.Conditional = p.Conditional || reg.object.conditional
				key := fastapiPathKey{p.Template, p.TrailingSlash}
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
		slashed := []fastapiPathEntry{}
		for _, p := range unique {
			slashed = append(slashed, p)
		}
		sort.Slice(slashed, func(i, j int) bool {
			if slashed[i].Template != slashed[j].Template {
				return slashed[i].Template < slashed[j].Template
			}
			return !slashed[i].TrailingSlash && slashed[j].TrailingSlash
		})
		entries := make([]pathEntry, 0, len(slashed))
		for _, p := range slashed {
			entries = append(entries, p.pathEntry)
		}
		attrs["paths"] = slashed
		if dropped > 0 {
			r.diagnostic(reg.file, "route_trailing_slash_unproven", reg.pattern.Text, reg.verb.Span)
		}
		// Unknown-prefix paths (T033, row UP-7): the paths stored below
		// the route's hops.
		scopes := make([]testscope.Scope, 0, len(entries))
		for _, p := range entries {
			scopes = append(scopes, p.Scope)
		}
		production, testOnly := scopedPaths(scopes)
		upState := upRouteState{file: reg.file, method: reg.method, pattern: pattern, hasPaths: production, testPaths: testOnly, hops: hopsByKey}
		if len(entries) == 0 && dropped > 0 {
			upState.refused = upDefaultSlash
		}
		if !production && dropped == 0 {
			list := r.axumMountEntries(relative, mountsAbove, reg, pattern, patternResolution, registrationScope, shaper.versions)
			switch {
			case len(list) > 0:
				upState.mounts = list
				if len(entries) == 0 {
					attrs[MountAttribute] = list
				}
			case len(entries) == 0:
				// A route reached through a stored hop is mounted, below
				// a prefix the code does not fix: not
				// unmounted.
				r.diagnostic(reg.file, "route_unmounted", reg.pattern.Text, reg.verb.Span)
			}
			upState.belowHop = belowAnyHop(relative, reg.object.key)
		}
		if list, problem := safeWebUnknownPrefix("UP-7", upState); problem != nil {
			r.diagnostics = append(r.diagnostics, *problem)
		} else if len(list) > 0 {
			attrs[UnknownPrefixAttribute] = list
		}
		if layers, unknown := r.routeLayers(reg); len(layers) > 0 || len(unknown) > 0 {
			if len(layers) > 0 {
				attrs["layers"] = layers
			}
			if len(unknown) > 0 {
				attrs["layers_unknown"] = unknown
			}
		}
		handlerID, _ := attrs["handler"].(string)
		if shape, built, err := shaper.shapeOf(reg, handlerID, entries, attrs); err == nil {
			attrs[routeshape.AttributeKey] = shape
			// Model-type uses (row MT-7).
			routeshape.SetModelUses(attrs, built)
		} else {
			r.diagnostics = append(r.diagnostics, contract.Diagnostic{Type: "diagnostic", File: reg.file, Code: "route_shape_refused", Severity: "warning", Message: fmt.Sprintf("%s: %v", q, err)})
		}
		owner := reg.owner
		if owner == "" {
			owner = facts.ID("file", reg.file, reg.file)
		}
		nodes = append(nodes, contract.Node{Type: "node", Identity: identity, Kind: "route", Level: 4, Name: q, QualifiedName: q, File: reg.file, Span: &reg.verb.Span, ContentHash: reg.verb.Hash, Attributes: attrs})
		edges = append(edges, facts.Edge("contains", owner, identity, reg.file, reg.verb.Span))
	}
	return nodes, edges, r.diagnostics
}

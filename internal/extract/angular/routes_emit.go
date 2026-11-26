package angular

import (
	"fmt"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// evaluate reads a route value with the existing bounded evaluator. Values
// that name identifiers are evaluated only at module scope and only when none
// of their names is locally shadowed; otherwise they are unknown.
func (r *router) evaluate(ctx routeContext, e expression) (string, string, bool) {
	v := evaluationExpr(e)
	names := map[string]bool{}
	identifierNames(v, names)
	if len(names) > 0 && ctx.scope != 0 {
		return "", "", false
	}
	for _, name := range ctx.shadows {
		if names[name] {
			return "", "", false
		}
	}
	m := clients.EvaluateMetadata(r.l.files, r.l.modules, ctx.file, 0, v)
	if !m.Known || hasControl(m.Text) {
		return "", "", false
	}
	return m.Kind, m.Text, true
}

func identifierNames(e facts.RouteExpr, names map[string]bool) {
	if e.Kind == "identifier" {
		names[e.Name] = true
	}
	if e.Object != nil {
		identifierNames(*e.Object, names)
	}
	for _, p := range e.Parts {
		identifierNames(p, names)
	}
	for _, a := range e.Args {
		identifierNames(a, names)
	}
	for _, p := range e.Properties {
		identifierNames(p, names)
	}
	for _, k := range e.Keywords {
		identifierNames(k.Value, names)
	}
}

// loaderBinding resolves a loadComponent/loadChildren value through the
// cached loader record whose value span equals the entry value span.
func (r *router) loaderBinding(obj *routeObject, entry typescript.DeclarationProperty) (typescript.ExportBinding, string) {
	loader, ok := r.loaders[obj.file+"\x00"+spanKey(entry.Value.Span)]
	if !ok {
		return typescript.ExportBinding{}, "loader syntax is not captured"
	}
	member := ""
	switch loader.Form {
	case "import_default":
		member = "default"
	case "import_member":
		member = loader.Member
	case "sync":
		if loader.Value == nil {
			return typescript.ExportBinding{}, "loader value is not captured"
		}
		return r.resolve(routeContext{file: obj.file, scope: loader.Scope, shadows: loader.Shadows}, unwrapRoute(*loader.Value).Value)
	default:
		reason := loader.Reason
		if reason == "" {
			reason = "inadmissible loader"
		}
		return typescript.ExportBinding{}, "inadmissible loader: " + reason
	}
	b := r.l.modules.ImportBinding(obj.file, *loader.Specifier, member, "")
	if b == (typescript.ExportBinding{}) && member == "default" {
		// An `export default [...]` array has no declaration; the router
		// payload names it "default".
		if m := r.l.modules.Resolve(obj.file, *loader.Specifier); !m.External && m.File != "" {
			if _, ok := r.arrays[m.File+"\x00default"]; ok {
				b = typescript.ExportBinding{File: m.File, Name: "default"}
			}
		}
	}
	if b.Ambiguous {
		return b, "ambiguous export"
	}
	if b == (typescript.ExportBinding{}) {
		return b, "loader module or export is unresolved"
	}
	return b, ""
}

func (r *router) externalTarget(file string, b typescript.ExportBinding) *routeLoad {
	load := &routeLoad{status: "external"}
	load.target = r.l.external(file, b.File)
	return load
}

// componentLoad classifies a component target (component, else loadComponent).
func (r *router) componentLoad(obj *routeObject) *routeLoad {
	key := "component"
	k, ok := obj.keys[key]
	if !ok {
		key = "loadComponent"
		if k, ok = obj.keys[key]; !ok {
			if obj.open {
				return &routeLoad{status: "unresolved", reason: hiddenReason}
			}
			return nil
		}
	}
	load := &routeLoad{span: k.entry.Value.Span}
	if k.method {
		load.status, load.reason = "unresolved", methodReason
		return load
	}
	if !k.known {
		load.status, load.reason = "unresolved", "value may be overwritten by a later spread"
		return load
	}
	var b typescript.ExportBinding
	reason := ""
	if key == "component" {
		e := unwrapRoute(k.entry.Value)
		if e.Kind != "identifier" && e.Kind != "member_expression" {
			load.status, load.reason = "unresolved", "component is not an identifier or member chain"
			return load
		}
		b, reason = r.resolve(obj.ctx, e.Value)
	} else {
		b, reason = r.loaderBinding(obj, k.entry)
	}
	if reason != "" {
		load.status, load.reason = "unresolved", reason
		return load
	}
	if b.External {
		e := r.externalTarget(obj.file, b)
		e.span = load.span
		return e
	}
	if !strings.HasPrefix(b.Identity, "class:") {
		load.status, load.reason = "unresolved", "target is not a class declaration"
		return load
	}
	load.target = b.Identity
	d := r.l.classes[b.Identity]
	switch {
	case d != nil && d.role == "component":
		standalone := d.attrs["standalone"]
		switch {
		case standalone == false && key == "loadComponent":
			load.status, load.reason = "invalid", "loadComponent requires a standalone component"
		case standalone == false && obj.lazy && !obj.eager:
			load.status, load.reason = "invalid", lazyStandaloneReason
		default:
			// requireStandaloneComponents applies per reach: an eager reach
			// keeps the link, a lazy reach of the same object still warns.
			load.status = "linked"
			load.lazyOnly = standalone == false && obj.lazy
		}
	case d != nil && d.role != "":
		load.status, load.reason = "invalid", "target has Angular role "+d.role+", not component"
	case d != nil || r.decorated[b.Identity]:
		load.status, load.reason = "unresolved", "target's Angular role cannot be proven"
	default:
		load.status, load.reason = "invalid", "target class has no Angular role"
	}
	return load
}

const (
	hiddenReason         = "may be supplied by a spread or computed key"
	methodReason         = "value is a method or accessor; not evaluated"
	lazyStandaloneReason = "a lazily loaded routes array requires standalone components"
)

// childrenLoad classifies loadChildren once per object.
func (w *routeWalk) childrenLoad(obj *routeObject) *routeLoad {
	if obj.children != nil {
		return obj.children
	}
	r := w.r
	k := obj.keys["loadChildren"]
	load := &routeLoad{span: k.entry.Value.Span}
	obj.children = load
	b, reason := r.loaderBinding(obj, k.entry)
	if reason != "" {
		load.status, load.reason = "unresolved", reason
		return load
	}
	if b.External {
		e := r.externalTarget(obj.file, b)
		e.span = load.span
		obj.children = e
		return e
	}
	if strings.HasPrefix(b.Identity, "class:") {
		load.target = b.Identity
		d := r.l.classes[b.Identity]
		switch {
		case d != nil && d.role == "ng_module" && d.active:
			load.status, load.module = "linked", b.Identity
		case d != nil && d.role == "ng_module":
			load.status, load.reason = "unresolved", "NgModule metadata cannot be evaluated"
		case d != nil && d.role != "":
			load.status, load.reason = "invalid", "target has Angular role "+d.role+", not ng_module"
		default:
			load.status, load.reason = "unresolved", "target is not a proven NgModule or routes array"
		}
		return load
	}
	value, key, ok := r.constArray(b)
	if !ok {
		load.status, load.reason = "unresolved", "target is not a proven NgModule or routes array"
		return load
	}
	if r.mutated(key) {
		load.status, load.reason = "unresolved", "mutated const array"
		return load
	}
	load.status, load.array, load.arrayFile, load.arrayKey = "linked", &value, b.File, key
	load.target = facts.ID("file", b.File, b.File)
	for _, n := range r.l.files[b.File].Nodes {
		if n.Kind == "variable" && n.QualifiedName == b.Name {
			load.target = n.Identity
		}
	}
	return load
}

func (load *routeLoad) attribute() map[string]any {
	out := map[string]any{"status": load.status}
	if load.target != "" && load.status != "unresolved" {
		out["target"] = load.target
	}
	if load.reason != "" {
		out["reason"] = load.reason
	}
	return out
}

// targetObject describes a guard, resolver, title or redirect function.
func (r *router) targetObject(ctx routeContext, e expression) map[string]any {
	e = unwrapRoute(e)
	if e.Kind == "arrow_function" || e.Kind == "function_expression" {
		return map[string]any{"status": "inline"}
	}
	if e.Kind != "identifier" && e.Kind != "member_expression" {
		return map[string]any{"status": "unresolved", "reason": "unsupported expression: " + e.Kind}
	}
	b, reason := r.resolve(ctx, e.Value)
	if reason != "" {
		return map[string]any{"status": "unresolved", "reason": reason}
	}
	if b.External {
		out := map[string]any{"status": "external"}
		if t := r.l.external(ctx.file, b.File); t != "" {
			out["target"] = t
		}
		return out
	}
	if b.Identity != "" {
		return map[string]any{"status": "linked", "target": b.Identity}
	}
	if b.File != "" && b.Name != "" && !b.Namespace {
		var found []string
		for _, n := range r.l.files[b.File].Nodes {
			switch n.Kind {
			case "function", "variable", "constant", "class":
				if n.QualifiedName == b.Name {
					found = append(found, n.Identity)
				}
			}
		}
		if len(found) == 1 {
			return map[string]any{"status": "linked", "target": found[0]}
		}
		return map[string]any{"status": "unresolved", "reason": "no unique declaration node"}
	}
	return map[string]any{"status": "unresolved", "reason": "target is unresolved"}
}

// truth reports Angular truthiness of a key: 1 true, 0 false, -1 unknown.
func (r *router) truth(obj *routeObject, key string) int {
	k, ok := obj.keys[key]
	if !ok {
		return 0
	}
	e := unwrapRoute(k.entry.Value)
	switch e.Kind {
	case "object", "array", "arrow_function", "function_expression", "class":
		return 1
	}
	if kind, text, known := r.evaluate(obj.ctx, e); known {
		switch kind {
		case "string":
			return boolInt(text != "")
		case "boolean":
			return boolInt(text == "true")
		case "integer", "number":
			return boolInt(text != "0" && text != "NaN")
		case "null", "undefined":
			return 0
		case "object":
			return 1
		}
		return -1
	}
	if e.Kind == "identifier" || e.Kind == "member_expression" {
		if r.callable(obj.ctx, e) {
			return 1
		}
	}
	return -1
}

// void reports whether a key is `=== undefined`: 1 yes, 0 no, -1 unknown.
func (r *router) void(obj *routeObject, key string) int {
	k, ok := obj.keys[key]
	if !ok {
		return 1
	}
	e := unwrapRoute(k.entry.Value)
	switch e.Kind {
	case "object", "array", "arrow_function", "function_expression", "class", "string", "template_string":
		return 0
	}
	if kind, _, known := r.evaluate(obj.ctx, e); known {
		return boolInt(kind == "undefined")
	}
	if e.Kind == "identifier" || e.Kind == "member_expression" {
		if r.callable(obj.ctx, e) {
			return 0
		}
	}
	return -1
}

// callable reports a value proven to name a local class or function
// declaration, which is always truthy and never undefined.
func (r *router) callable(ctx routeContext, e expression) bool {
	t := r.targetObject(ctx, e)
	target, _ := t["target"].(string)
	return t["status"] == "linked" && (strings.HasPrefix(target, "class:") || strings.HasPrefix(target, "function:"))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (r *router) stringValue(obj *routeObject, key string) (string, bool) {
	k, ok := obj.keys[key]
	if !ok || !k.known {
		return "", false
	}
	kind, text, known := r.evaluate(obj.ctx, k.entry.Value)
	return text, known && kind == "string"
}

// invalid evaluates validateNode (both verified versions) with Angular's
// truthiness. Only definite violations are reported; partial objects are skipped.
func (r *router) invalid(obj *routeObject) []string {
	if obj.checked {
		return obj.codes
	}
	obj.checked = true
	if obj.partial {
		return nil
	}
	obj.codes = r.validate(obj)
	return obj.codes
}

func (r *router) validate(obj *routeObject) []string {
	t := func(key string) int { return r.truth(obj, key) }
	var codes []string
	add := func(code string, violated bool) {
		if violated {
			codes = append(codes, code)
		}
	}
	none := t("component") == 0 && t("loadComponent") == 0 && t("children") == 0 && t("loadChildren") == 0
	outlet, outletKnown := r.stringValue(obj, "outlet")
	add("named_outlet_without_target", none && t("redirectTo") == 0 && t("outlet") == 1 && outletKnown && outlet != "primary")
	add("redirect_with_children", t("redirectTo") == 1 && t("children") == 1)
	add("redirect_with_load_children", t("redirectTo") == 1 && t("loadChildren") == 1)
	add("children_with_load_children", t("children") == 1 && t("loadChildren") == 1)
	add("component_with_load_component", t("component") == 1 && t("loadComponent") == 1)
	add("redirect_with_component", t("redirectTo") == 1 && (t("component") == 1 || t("loadComponent") == 1))
	// canMatch joined the redirect guard check after 15.2.3; it is applied only
	// where the project's Angular major is proven to be at least the verified 21.
	canMatch := r.l.version(obj.file).major >= 21 && t("canMatch") == 1
	add("redirect_with_guard", t("redirectTo") == 1 && (t("canActivate") == 1 || canMatch))
	add("path_with_matcher", t("path") == 1 && t("matcher") == 1)
	add("missing_target", r.void(obj, "redirectTo") == 1 && none)
	add("missing_path", r.void(obj, "path") == 1 && r.void(obj, "matcher") == 1)
	path, pathKnown := r.stringValue(obj, "path")
	add("leading_slash", pathKnown && strings.HasPrefix(path, "/"))
	add("empty_path_redirect_without_path_match", pathKnown && path == "" && r.void(obj, "redirectTo") == 0 && r.void(obj, "pathMatch") == 1)
	sort.Strings(codes)
	return codes
}

// emit names reached route objects, then builds nodes, attributes and edges.
func (r *router) emit(w *routeWalk) []contract.Node {
	l := r.l
	objects := append([]*routeObject(nil), w.order...)
	sort.SliceStable(objects, func(i, j int) bool {
		a, b := objects[i], objects[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.expr.Span.StartLine != b.expr.Span.StartLine {
			return a.expr.Span.StartLine < b.expr.Span.StartLine
		}
		return a.expr.Span.StartColumn < b.expr.Span.StartColumn
	})
	var nodes []contract.Node
	for _, obj := range objects {
		obj.owner = facts.ID("file", obj.file, obj.file)
		l.routeBase(obj)
		if obj.parent != nil {
			obj.owner = obj.parent.identity
		} else if owner := l.routeOwner(obj); owner != nil {
			obj.owner = owner.Identity
		}
		qualified, name := obj.base, obj.segment
		if n := r.repeat(obj); n > 1 {
			qualified += fmt.Sprintf("@%d", n)
			name += fmt.Sprintf("@%d", n)
		}
		obj.identity = facts.ID("ui_route", obj.file, qualified)
		span := obj.expr.Span
		source := l.sources[obj.file]
		start, end := offsets(source, span)
		hash := contract.ContentHash(source[start:end])
		nodes = append(nodes, contract.Node{Type: "node", Identity: obj.identity, Kind: "ui_route", Level: 4, Name: name, QualifiedName: qualified, File: obj.file, Span: &span, ContentHash: hash, Attributes: r.attributes(w, obj)})
		l.edges = append(l.edges, facts.Edge("contains", obj.owner, obj.identity, obj.file, span))
	}
	return nodes
}

// routeBase sets the qualified name before @N suffixes: literal children nest
// under their parent's base, others under the nearest lexical owner.
func (l *linker) routeBase(obj *routeObject) {
	obj.base = obj.segment
	if obj.parent != nil {
		obj.base = obj.parent.base + "." + obj.segment
	} else if owner := l.routeOwner(obj); owner != nil {
		obj.base = owner.QualifiedName + "." + obj.segment
	}
}

// repeat returns the 1-based position of an object among the object literals
// of its file that share its base name, in source order. Candidates are every
// object literal element of an array literal in the file's cached trees
// (declaration constants, router arrays, router call arguments), reached or
// not, so a name depends only on the declaring file's content (F7).
func (r *router) repeat(obj *routeObject) int {
	if r.names == nil {
		r.names = map[string]map[string]int{}
	}
	names, ok := r.names[obj.file]
	if !ok {
		names = r.candidateNames(obj.file)
		r.names[obj.file] = names
	}
	if n, ok := names[spanKey(obj.expr.Span)]; ok {
		return n
	}
	return 1
}

func (r *router) candidateNames(file string) map[string]int {
	ctx := routeContext{file: file, shadows: []string{}}
	seen := map[string]bool{}
	var candidates []*routeObject
	var visit func(e expression, parent *routeObject)
	var elements func(e expression, parent *routeObject)
	elements = func(e expression, parent *routeObject) {
		for _, p := range e.Parts {
			p = unwrapRoute(p)
			switch {
			case p.Kind == "object":
				key := spanKey(p.Span)
				if seen[key] {
					continue
				}
				seen[key] = true
				obj := r.newRouteObject(ctx, p, parent)
				r.l.routeBase(obj)
				candidates = append(candidates, obj)
				for _, entry := range p.Entries {
					if k := obj.keys["children"]; entry.Key == "children" && k.known && k.entry.Value.Span == entry.Value.Span && unwrapRoute(entry.Value).Kind == "array" {
						elements(unwrapRoute(entry.Value), obj)
						continue
					}
					visit(entry.Value, nil)
				}
			case p.Kind == "spread_element" && len(p.Parts) == 1 && unwrapRoute(p.Parts[0]).Kind == "array":
				elements(unwrapRoute(p.Parts[0]), parent)
			default:
				visit(p, nil)
			}
		}
	}
	visit = func(e expression, parent *routeObject) {
		e = unwrapRoute(e)
		if e.Kind == "array" {
			elements(e, parent)
			return
		}
		if e.Object != nil {
			visit(*e.Object, nil)
		}
		for _, p := range e.Parts {
			visit(p, nil)
		}
		for _, a := range e.Args {
			visit(a, nil)
		}
		for _, entry := range e.Entries {
			visit(entry.Value, nil)
		}
	}
	for _, c := range r.l.syntax[file].Constants {
		visit(c.Value, nil)
	}
	for _, a := range r.routers[file].Arrays {
		visit(a.Value, nil)
	}
	for _, c := range r.routers[file].Calls {
		if c.First != nil {
			visit(*c.First, nil)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i].expr.Span, candidates[j].expr.Span
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartColumn < b.StartColumn
	})
	counts := map[string]int{}
	names := map[string]int{}
	for _, c := range candidates {
		counts[c.base]++
		names[spanKey(c.expr.Span)] = counts[c.base]
	}
	return names
}

// routeOwner is the nearest emitted lexical owner node containing the object.
func (l *linker) routeOwner(obj *routeObject) *contract.Node {
	var best *contract.Node
	nodes := l.files[obj.file].Nodes
	for i := range nodes {
		n := &nodes[i]
		if n.Kind == "file" || n.Span == nil || !spanContains(*n.Span, obj.expr.Span) {
			continue
		}
		if best == nil || spanContains(*best.Span, *n.Span) {
			best = n
		}
	}
	return best
}

func (r *router) attributes(w *routeWalk, obj *routeObject) map[string]any {
	l := r.l
	attrs := map[string]any{"framework": "angular"}
	has := func(key string) bool { _, ok := obj.keys[key]; return ok }
	// hidden reports an absent key that a spread or computed key may supply:
	// unknown, not absent (A3).
	hidden := func(key string) bool { return !has(key) && obj.open }
	value := func(key string) any {
		if text, ok := r.stringValue(obj, key); ok {
			return text
		}
		return nil
	}
	if has("path") || hidden("path") {
		attrs["path"] = value("path")
	}
	if has("matcher") {
		attrs["matcher"] = true
	} else if hidden("matcher") {
		attrs["matcher"] = nil
	}
	paths := []string{}
	for p := range obj.paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	entries := []map[string]any{}
	for _, p := range paths {
		entry := map[string]any{"path": p}
		if !obj.paths[p] {
			entry["scope"] = "test"
		}
		entries = append(entries, entry)
	}
	attrs["paths"] = entries
	switch k, ok := obj.keys["loadComponent"]; {
	case ok && (k.known || k.method):
		attrs["lazy"] = true
	case ok || obj.open:
		attrs["lazy"] = nil
	default:
		attrs["lazy"] = false
	}
	if c := r.componentLoad(obj); c != nil {
		attrs["component"] = c.attribute()
		switch c.status {
		case "linked":
			l.edges = append(l.edges, facts.Edge("routes_to", obj.identity, c.target, obj.file, c.span))
			if c.lazyOnly {
				w.diagnostic(obj.file, obj.expr.Span, "angular_route_target_invalid", "warning", "component target "+c.target+": "+lazyStandaloneReason+" (lazy reach only)")
			}
		case "invalid":
			w.diagnostic(obj.file, obj.expr.Span, "angular_route_target_invalid", "warning", "component target "+c.target+": "+c.reason)
		}
	}
	if k, ok := obj.keys["loadChildren"]; ok || obj.open {
		var load *routeLoad
		switch {
		case !ok:
			load = &routeLoad{status: "unresolved", reason: hiddenReason}
		case k.method:
			load = &routeLoad{status: "unresolved", reason: methodReason}
		case !k.known:
			load = &routeLoad{status: "unresolved", reason: "value may be overwritten by a later spread"}
		case r.unloaded(obj) != "":
			// Angular rejects the configuration; the loader is never used.
			classified := w.childrenLoad(obj)
			load = &routeLoad{status: "invalid", reason: "loadChildren is never used: invalid route configuration (" + r.unloaded(obj) + ")"}
			if classified.status == "linked" || classified.status == "external" {
				load.target = classified.target
			}
		default:
			load = w.childrenLoad(obj)
		}
		attrs["children_load"] = load.attribute()
		switch {
		case load.status == "linked":
			l.edges = append(l.edges, facts.Edge("loads_children", obj.identity, load.target, obj.file, load.span))
		case load.status == "invalid" && r.unloaded(obj) == "":
			w.diagnostic(obj.file, obj.expr.Span, "angular_route_target_invalid", "warning", "loadChildren target "+load.target+": "+load.reason)
		}
	}
	if k, ok := obj.keys["redirectTo"]; ok {
		attrs["redirect_to"] = value("redirectTo")
		e := unwrapRoute(k.entry.Value)
		if k.known && attrs["redirect_to"] == nil && (e.Kind == "arrow_function" || e.Kind == "function_expression") {
			attrs["redirect_to"] = map[string]any{"status": "inline"}
		} else if k.known && attrs["redirect_to"] == nil && (e.Kind == "identifier" || e.Kind == "member_expression") {
			if t := r.targetObject(obj.ctx, e); t["status"] == "linked" && strings.HasPrefix(t["target"].(string), "function:") {
				attrs["redirect_to"] = t
			}
		}
	} else if hidden("redirectTo") {
		attrs["redirect_to"] = nil
	}
	for _, key := range []string{"pathMatch", "outlet"} {
		if has(key) || hidden(key) {
			attrs[map[string]string{"pathMatch": "path_match", "outlet": "outlet"}[key]] = value(key)
		}
	}
	if k, ok := obj.keys["title"]; ok {
		if text, known := r.stringValue(obj, "title"); known {
			attrs["title"] = text
		} else if k.known {
			attrs["title"] = r.targetObject(obj.ctx, k.entry.Value)
		} else {
			attrs["title"] = nil
		}
	} else if hidden("title") {
		attrs["title"] = nil
	}
	guards := map[string]any{}
	for _, g := range guardKeys {
		k, ok := obj.keys[g.key]
		if !ok {
			if obj.open {
				guards[g.attr] = nil
			}
			continue
		}
		e := unwrapRoute(k.entry.Value)
		if !k.known || e.Kind != "array" {
			guards[g.attr] = nil
			continue
		}
		list := []any{}
		for _, p := range e.Parts {
			if p.Kind == "spread_element" {
				list = append(list, map[string]any{"status": "unresolved", "reason": "spread element"})
				continue
			}
			list = append(list, r.targetObject(obj.ctx, p))
		}
		guards[g.attr] = list
	}
	if len(guards) > 0 {
		attrs["guards"] = guards
	}
	if k, ok := obj.keys["resolve"]; ok {
		e := unwrapRoute(k.entry.Value)
		if !k.known || e.Kind != "object" {
			attrs["resolve"] = nil
		} else {
			resolve := map[string]any{}
			for _, entry := range e.Entries {
				if entry.Kind != "pair" && entry.Kind != "shorthand_property_identifier" {
					resolve = nil
					break
				}
				resolve[entry.Key] = r.targetObject(obj.ctx, entry.Value)
			}
			if resolve == nil {
				attrs["resolve"] = nil
			} else {
				attrs["resolve"] = resolve
			}
		}
	} else if hidden("resolve") {
		attrs["resolve"] = nil
	}
	if has("providers") {
		attrs["providers"] = true
	} else if hidden("providers") {
		attrs["providers"] = nil
	}
	if obj.partial {
		attrs["partial"] = true
	}
	if codes := r.invalid(obj); len(codes) > 0 {
		attrs["invalid"] = codes
		for _, code := range codes {
			w.diagnostic(obj.file, obj.expr.Span, "angular_route_invalid", "warning", "invalid route configuration: "+code)
		}
	}
	if r.test(obj.file) {
		attrs["scope"] = "test"
	}
	return attrs
}

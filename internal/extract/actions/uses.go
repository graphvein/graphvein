package actions

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// resolution is what an identifier names at a use site.
type resolution int

const (
	// unresolved: no binding, an ambiguous binding or an import the
	// resolver does not follow; it may be an action.
	unresolved resolution = iota
	// other: a binding that is proven not to be an action.
	other
	// isAction: the binding is a server action.
	isAction
)

// binding walks the route scopes of file from scope outwards.
func (l *linker) binding(file string, scope int, name string) (facts.RouteBinding, int, bool) {
	r := l.in.TypeScript[file].Routes
	if r == nil {
		return facts.RouteBinding{}, 0, false
	}
	for scope >= 0 && scope < len(r.Scopes) {
		if b, ok := r.Scopes[scope].Bindings[name]; ok {
			return b, scope, true
		}
		scope = r.Scopes[scope].Parent
	}
	return facts.RouteBinding{}, 0, false
}

// resolve decides what the identifier name names at file and scope: the
// shared resolver's lexical scopes and, for an import, the module
// resolver's export binding (rows SA-C1..SA-C3).
func (l *linker) resolve(file string, scope int, name string) (*action, resolution) {
	b, at, ok := l.binding(file, scope, name)
	if !ok {
		return nil, unresolved
	}
	if b.Module != "" {
		if b.Value.Invalid {
			// A type-only import names no value.
			return nil, other
		}
		if target := l.in.Modules.Resolve(file, b.Module); target.File != "" {
			if a := l.reexported[target.File][b.Export]; a != nil {
				return a, isAction
			}
		}
		eb := l.in.Modules.ImportBinding(file, b.Module, b.Export, "")
		switch {
		case eb.Ambiguous || eb.File == "" && !eb.External:
			return nil, unresolved
		case eb.External || eb.Namespace:
			return nil, other
		}
		if a := l.byIdentity[eb.Identity]; eb.Identity != "" && a != nil {
			return a, isAction
		}
		return l.local(eb.File, 0, eb.Name)
	}
	return l.local(file, at, name)
}

// local decides a binding found in scope at of file, without following
// imports again.
func (l *linker) local(file string, at int, name string) (*action, resolution) {
	b, at, ok := l.binding(file, at, name)
	if !ok {
		return nil, unresolved
	}
	if b.Module != "" {
		return nil, unresolved
	}
	switch b.Value.Kind {
	case "ambiguous":
		return nil, unresolved
	case "function":
		if a := l.byScope[file][b.ReturnScope]; a != nil {
			return a, isAction
		}
	}
	if at == 0 {
		if a := l.byLocal[file][name]; a != nil {
			return a, isAction
		}
	}
	return nil, other
}

// hook reports the React hook a callee names: useActionState from react
// or useFormState from react-dom, imported by name or read from a
// namespace or default import of the package (row SA-C2).
func (l *linker) hook(file string, scope int, callee facts.RouteExpr) string {
	known := func(module, name string) string {
		switch {
		case module == "react" && name == "useActionState":
			return "useActionState"
		case module == "react-dom" && name == "useFormState":
			return "useFormState"
		}
		return ""
	}
	imported := func(name string) (string, string, bool) {
		b, _, ok := l.binding(file, scope, name)
		if !ok || b.Module == "" || b.Value.Invalid {
			return "", "", false
		}
		if r := l.in.Modules.Resolve(file, b.Module); !r.Package() {
			return "", "", false
		}
		return b.Module, b.Export, true
	}
	switch callee.Kind {
	case "identifier":
		if module, export, ok := imported(callee.Name); ok {
			return known(module, export)
		}
	case "member_expression":
		if callee.Object == nil || callee.Object.Kind != "identifier" {
			return ""
		}
		if module, export, ok := imported(callee.Object.Name); ok && (export == "*" || export == "default") {
			return known(module, callee.Name)
		}
	}
	return ""
}

// use is one linked use of an action; a conditional use names every
// action among its alternatives.
type use struct {
	owner, ownerName, name string
	acts                   []*action
	span                   contract.Span
	hash                   string
	attrs                  map[string]any
}

// alternatives reads a value that may name actions: an identifier, a
// conditional whose alternatives are identifiers (R1), or an inline
// action written in place. It returns the actions named, whether the
// value is conditional, and whether some alternative is unresolved.
func (l *linker) alternatives(file string, scope int, v facts.RouteExpr) (acts []*action, conditional, unresolvedAlt, known bool) {
	switch v.Kind {
	case "identifier":
		act, r := l.resolve(file, scope, v.Name)
		if r == isAction {
			return []*action{act}, false, false, true
		}
		return nil, false, r == unresolved, true
	case "arrow_function", "function_expression", "function":
		if act := l.byOffset[file][v.Offset]; act != nil {
			return []*action{act}, false, false, true
		}
		return nil, false, false, true
	case "ternary_expression":
		a := l.in.TypeScript[file].Actions
		if a == nil {
			return nil, true, false, false
		}
		for _, t := range a.Ternaries {
			if t.Offset != v.Offset {
				continue
			}
			seen := map[*action]bool{}
			for _, leaf := range t.Leaves {
				act, r := l.resolve(file, scope, leaf)
				switch {
				case r == isAction && !seen[act]:
					seen[act] = true
					acts = append(acts, act)
				case r == unresolved:
					unresolvedAlt = true
				}
			}
			return acts, true, unresolvedAlt || !t.Complete, true
		}
		return nil, true, false, false
	}
	return nil, false, false, false
}

// formAttribute reports an attribute that submits a form to its value
// (row SA-C1): `action` on an intrinsic `<form>` or on next/form's
// `Form` (its default export, imported by name), `formAction` on an
// intrinsic `<button>` or `<input>` (React's form actions). The same
// attribute on any other element, a component above all, is a prop: the
// component decides what it does with the function (row D).
func (l *linker) formAttribute(file string, a facts.ActionAttribute) bool {
	switch {
	case a.Name == "action" && a.Element == "form":
		return true
	case a.Name == "formAction" && (a.Element == "button" || a.Element == "input"):
		return true
	case a.Name == "action" && a.Element != "" && !strings.Contains(a.Element, "."):
		b, _, ok := l.binding(file, a.Scope, a.Element)
		return ok && b.Module == "next/form" && b.Export == "default" && !b.Value.Invalid
	}
	return false
}

// uses reads the attribute and call sites of one file (rows SA-C1,
// SA-C2, SA-C3 and D) and emits an action_call node per linked use.
func (l *linker) uses(file string) {
	f := l.in.TypeScript[file]
	var linked []use
	add := func(owner, ownerName, name string, acts []*action, conditional bool, s contract.Span, hash string, attrs map[string]any) {
		if conditional {
			attrs["conditional"] = true
			ids := make([]any, 0, len(acts))
			for _, a := range acts {
				ids = append(ids, a.identity)
			}
			attrs["alternatives"] = ids
			name = "(conditional)"
		}
		linked = append(linked, use{owner: owner, ownerName: ownerName, name: name, acts: acts, span: s, hash: hash, attrs: attrs})
	}
	unread := func(s contract.Span, acts []*action, message string) {
		for _, a := range acts {
			a.unread++
		}
		l.diagnose(file, "server_action_use_unread", s, message)
	}
	if f.Actions != nil {
		for _, a := range f.Actions.Attributes {
			form := l.formAttribute(file, a)
			acts, conditional, unresolvedAlt, known := l.alternatives(file, a.Scope, a.Value)
			switch {
			case !form:
				if len(acts) > 0 {
					element := a.Element
					if element == "" {
						element = "an element"
					}
					unread(a.Span, acts, fmt.Sprintf("action %s passed as prop %s of %s (row D)", a.Value.Text, a.Name, element))
				}
			case len(acts) > 0:
				tag("SA-C1")
				name := a.Value.Name
				if a.Value.Kind != "identifier" {
					name = "(anonymous)"
				}
				add(a.Owner, a.OwnerName, name, acts, conditional, a.Span, a.Hash, map[string]any{"form": "form_action", "attribute": a.Name})
			case !known || unresolvedAlt:
				unread(a.Span, nil, fmt.Sprintf("%s={%s} is not read as an action (row SA-C1)", a.Name, a.Value.Text))
			}
		}
	}
	if f.Routes != nil {
		for _, c := range f.Routes.Clients {
			e := c.Expression
			if e.Kind != "call_expression" || e.Object == nil {
				continue
			}
			callee := *e.Object
			args := e.Args
			switch callee.Kind {
			case "identifier":
				if act, r := l.resolve(file, c.Scope, callee.Name); r == isAction {
					tag("SA-C3")
					add(c.Owner, c.OwnerName, callee.Name, []*action{act}, false, c.Span, c.Hash, l.boundary(file))
				}
			case "member_expression":
				if callee.Object != nil && callee.Object.Kind == "identifier" {
					if act, r := l.resolve(file, c.Scope, callee.Object.Name); r == isAction {
						unread(c.Span, []*action{act}, fmt.Sprintf("%s.%s: an action read through a member (row D)", callee.Object.Name, callee.Name))
					} else if act := l.namespaceAction(file, c.Scope, callee); act != nil {
						unread(c.Span, []*action{act}, fmt.Sprintf("%s.%s: an action read through a namespace object (row D)", callee.Object.Name, callee.Name))
					}
				}
			}
			if h := l.hook(file, c.Scope, callee); h != "" && len(args) > 0 {
				first := args[0]
				args = args[1:]
				acts, conditional, unresolvedAlt, known := l.alternatives(file, c.Scope, first)
				switch {
				case len(acts) > 0:
					tag("SA-C2")
					name := first.Name
					if first.Kind != "identifier" {
						name = "(anonymous)"
					}
					add(c.Owner, c.OwnerName, name, acts, conditional, c.Span, c.Hash, map[string]any{"form": "action_state", "hook": h})
				case !known || unresolvedAlt:
					unread(c.Span, nil, fmt.Sprintf("%s(%s): the first argument is not read as an action (row SA-C2)", h, first.Text))
				}
			}
			for _, a := range args {
				if a.Kind != "identifier" {
					continue
				}
				if act, r := l.resolve(file, c.Scope, a.Name); r == isAction {
					unread(c.Span, []*action{act}, fmt.Sprintf("action %s passed as an argument (row D)", a.Name))
				}
			}
		}
	}
	sort.SliceStable(linked, func(i, j int) bool {
		a, b := linked[i].span, linked[j].span
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartColumn < b.StartColumn
	})
	repeats := map[string]int{}
	for _, u := range linked {
		qualified := facts.Join(u.ownerName, "ACTION "+u.name)
		repeats[qualified]++
		if n := repeats[qualified]; n > 1 {
			qualified += fmt.Sprintf("@%d", n)
		}
		identity := facts.ID("action_call", file, qualified)
		attrs := map[string]any{"framework": "nextjs", "transport": Transport, "status": "linked", "action": nil}
		if len(u.acts) == 1 {
			attrs["action"] = u.acts[0].identity
		}
		for k, v := range u.attrs {
			attrs[k] = v
		}
		if l.test(file) {
			attrs["scope"] = "test"
		}
		owner := u.owner
		if owner == "" {
			owner = facts.ID("file", file, file)
		}
		s := u.span
		l.nodes = append(l.nodes, contract.Node{Type: "node", Kind: "action_call", Level: 4, Identity: identity, Name: "ACTION " + u.name, QualifiedName: qualified, File: file, Span: &s, ContentHash: u.hash, Attributes: attrs})
		l.edges = append(l.edges, facts.Edge("contains", owner, identity, file, s))
		for _, a := range u.acts {
			l.edges = append(l.edges, facts.Edge("calls_action", identity, a.identity, file, s))
		}
	}
}

// boundary labels a direct call (row SA-C3, revised in R1): client when
// the file opens with 'use client', or is in the client module graph and
// no server entry point reaches it through value imports without a
// 'use client' boundary; otherwise unknown (a module in both bundles may
// run the call on the server). A server graph with a dynamic import or
// require the resolver cannot follow leaves reached modules unknown too
// (server_graph_incomplete names the file).
func (l *linker) boundary(file string) map[string]any {
	attrs := map[string]any{"form": "direct", "boundary": "unknown"}
	entry, client := l.client[file]
	switch {
	case client && entry == file:
		attrs["boundary"] = "client"
	case client && !l.server[file] && l.serverOpen != "":
		attrs["server_graph_incomplete"] = l.serverOpen
	case client && !l.server[file]:
		attrs["boundary"] = "client"
		attrs["client_entry"] = entry
	case client:
		attrs["shared_module"] = true
	}
	return attrs
}

// countUnread records on each action how many unread uses may reach it,
// so no view calls an action "not called" that such a use names.
func (l *linker) countUnread() {
	seen := map[*action]bool{}
	var all []*action
	for _, m := range []map[string]*action{l.byIdentity} {
		for _, a := range m {
			all = append(all, a)
		}
	}
	for _, m := range l.byScope {
		for _, a := range m {
			all = append(all, a)
		}
	}
	for _, maps := range []map[string]map[string]*action{l.byLocal, l.reexported} {
		for _, m := range maps {
			for _, a := range m {
				all = append(all, a)
			}
		}
	}
	for _, m := range l.byOffset {
		for _, a := range m {
			all = append(all, a)
		}
	}
	for _, a := range all {
		if seen[a] || a.unread == 0 {
			continue
		}
		seen[a] = true
		l.nodes[a.index].Attributes["unread_uses"] = a.unread
	}
}

// clientFunction reports an inline function value that is not an action
// (a form handler running in the browser): it names no action, so it is
// not counted as an unread use. An inline action is counted (row SA-I1
// claims its node only).
func (l *linker) clientFunction(file string, v facts.RouteExpr) bool {
	if v.Kind != "arrow_function" && v.Kind != "function_expression" && v.Kind != "function" {
		return false
	}
	if a := l.in.TypeScript[file].Actions; a != nil {
		for _, f := range a.Inline {
			if f.Offset == v.Offset {
				return false
			}
		}
	}
	return true
}

// namespaceAction returns the action `ns.a` names, where ns is a
// namespace import of a module and a one of its actions; nil otherwise.
func (l *linker) namespaceAction(file string, scope int, member facts.RouteExpr) *action {
	b, _, ok := l.binding(file, scope, member.Object.Name)
	if !ok || b.Module == "" || b.Export != "*" || b.Value.Invalid {
		return nil
	}
	if target := l.in.Modules.Resolve(file, b.Module); target.File != "" && l.reexported[target.File][member.Name] != nil {
		return l.reexported[target.File][member.Name]
	}
	eb := l.in.Modules.ImportBinding(file, b.Module, "*", member.Name)
	if a := l.byIdentity[eb.Identity]; eb.Identity != "" && a != nil {
		return a
	}
	if eb.File != "" && !eb.External && !eb.Namespace {
		if a, r := l.local(eb.File, 0, eb.Name); r == isAction {
			return a
		}
	}
	return nil
}

// clientGraph computes the client module graph (row SA-C3): every file
// whose prologue holds 'use client', and every file reached from one
// through imports that load a module at runtime, resolved by the module
// resolver. A 'use server' module is never entered: its code runs on the
// server, and a client import of it is a reference to its actions.
func (l *linker) clientGraph(paths []string) {
	l.client = map[string]string{}
	var queue []string
	for _, file := range paths {
		a := l.in.TypeScript[file].Actions
		if a != nil && directive(a, "use client") && !directive(a, "use server") {
			l.client[file] = file
			queue = append(queue, file)
		}
	}
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		for _, target := range l.valueImports(file, false) {
			if _, seen := l.client[target]; seen {
				continue
			}
			if a := l.in.TypeScript[target].Actions; a != nil && directive(a, "use server") {
				continue
			}
			l.client[target] = l.client[file]
			queue = append(queue, target)
		}
	}
}

// valueImports lists, in source order, the scanned files file loads at
// runtime through its imports and its re-export clauses with a module.
//
// With may false (the client graph) only proven loads count: a
// side-effect import, an import with at least one binding that is not
// type-only and names a value declaration (function, method, class or
// variable) or a namespace, and re-export clauses of the same kinds
// (`export * from`, `export * as ns from`, `export { a } from` naming a
// value). The client graph may then miss a module (its calls stay
// boundary unknown) but never gains one.
//
// With may true (the server graph) every import or re-export that is not
// type-only counts, also one whose bindings the resolver cannot prove to
// be values (the compiler may elide it, or may not): a module the server
// may load is in the server graph, so a call is labelled client only when
// no server entry point can reach it (row SA-C3; T024 review: a
// barrel's `export { f } from` passes server reach on to f's module).
func (l *linker) valueImports(file string, may bool) []string {
	f := l.in.TypeScript[file]
	var out []string
	for _, imp := range f.Imports {
		r := l.in.Modules.Resolve(file, imp.Module)
		if r.Package() || r.File == "" {
			continue
		}
		if _, script := l.in.TypeScript[r.File]; !script {
			continue
		}
		loads := len(imp.Bindings) == 0
		for local, exported := range imp.Bindings {
			if loads {
				break
			}
			if b, _, ok := l.binding(file, 0, local); !ok || b.Value.Invalid || b.Module != imp.Module {
				continue
			}
			if exported == "*" || may {
				loads = true
				continue
			}
			eb := l.in.Modules.Export(r.File, exported)
			if kind, _, _, err := contract.ParseIdentity(eb.Identity); err == nil && (kind == "function" || kind == "method" || kind == "class" || kind == "variable") {
				loads = true
			}
		}
		if loads {
			out = append(out, r.File)
		}
	}
	// A re-export clause with a module loads that module whenever this
	// file loads (ECMAScript module linking).
	for _, c := range f.ExportClauses {
		if c.Module == "" || c.TypeOnly {
			continue
		}
		r := l.in.Modules.Resolve(file, c.Module)
		if r.Package() || r.File == "" {
			continue
		}
		if _, script := l.in.TypeScript[r.File]; !script {
			continue
		}
		if !c.Star && !c.Namespace && !may {
			eb := l.in.Modules.Export(r.File, c.Name)
			kind, _, _, err := contract.ParseIdentity(eb.Identity)
			if err != nil || kind != "function" && kind != "method" && kind != "class" && kind != "variable" {
				continue
			}
		}
		out = append(out, r.File)
	}
	return out
}

// serverGraph computes the files server code reaches (row SA-C3, R1):
// from every script under a project's app or pages directory and its
// middleware and instrumentation files that does not open with
// 'use client', through value imports, never entering a 'use client'
// file (a client boundary: the server renders a reference to it). A
// module of the client graph that is also in this set is in both bundles.
func (l *linker) serverGraph(paths []string, projects []string) {
	l.server = map[string]bool{}
	isClient := func(file string) bool {
		a := l.in.TypeScript[file].Actions
		return a != nil && directive(a, "use client") && !directive(a, "use server")
	}
	var queue []string
	for _, file := range paths {
		if !scriptFile(file) || isClient(file) || !serverRoot(projects, file) {
			continue
		}
		l.server[file] = true
		queue = append(queue, file)
	}
	opens := map[string]bool{}
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		targets := l.valueImports(file, true)
		// A dynamic import() or require() of a literal specifier loads
		// that module on the server too; one the resolver cannot follow
		// (a non-literal specifier, an unresolved path) may load any
		// module, so the graph is then open.
		if a := l.in.TypeScript[file].Actions; a != nil {
			opens[file] = a.LoadsUnread
			for _, spec := range a.Loads {
				r := l.in.Modules.Resolve(file, spec)
				switch {
				case r.Package():
				case r.File == "":
					opens[file] = true
				default:
					if _, script := l.in.TypeScript[r.File]; script {
						targets = append(targets, r.File)
					}
				}
			}
		}
		for _, target := range targets {
			if l.server[target] || isClient(target) {
				continue
			}
			l.server[target] = true
			queue = append(queue, target)
		}
	}
	for _, file := range paths {
		if l.server[file] && opens[file] {
			l.serverOpen = file
			break
		}
	}
}

// serverRoot reports a server entry point of a Next.js project: a file
// under app/, src/app/, pages/ or src/pages/, or a middleware or
// instrumentation file at the project root or in src/.
func serverRoot(projects []string, file string) bool {
	for _, p := range projects {
		rel := file
		if p != "." {
			if !strings.HasPrefix(file, p+"/") {
				continue
			}
			rel = strings.TrimPrefix(file, p+"/")
		}
		rel = strings.TrimPrefix(rel, "src/")
		if strings.HasPrefix(rel, "app/") || strings.HasPrefix(rel, "pages/") {
			return true
		}
		if !strings.Contains(rel, "/") {
			base := strings.TrimSuffix(rel, path.Ext(rel))
			if base == "middleware" || base == "instrumentation" || base == "proxy" {
				return true
			}
		}
	}
	return false
}

// scriptFile reports files the TypeScript extractor reads.
func scriptFile(file string) bool {
	switch path.Ext(file) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts":
		return true
	}
	return false
}

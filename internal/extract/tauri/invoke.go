package tauri

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/clients"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// site is one recognized invoke call: a direct or namespace call of
// @tauri-apps/api/core's invoke (I1), a wrapper invocation (I2) or a global
// form (I6). argument is the position of the command name.
type site struct {
	file     string
	call     facts.ClientCall
	form     string
	wrapper  string
	argument int
}

// wrapper is a callable forwarding one of its own parameters, unchanged, as
// the command of a recognized invoke; parameter is that parameter's position.
type wrapper struct {
	parameter, level int
}

// status precedence when alternatives differ; linked only when all link.
var precedence = []string{"dynamic", "registry_unproven", "acl_possible", "ambiguous", "shadow_possible", "unknown_command", "test_only", "unmatched", "plugin", "global_unlinked", "linked"}

func siteKey(file string, s contract.Span) string {
	return fmt.Sprintf("%s:%d:%d:%d:%d", file, s.StartLine, s.StartColumn, s.EndLine, s.EndColumn)
}

// linkInvokes recognizes invoke sites, follows wrappers for three levels and
// emits ipc_call nodes with their matches.
func (l *linker) linkInvokes() {
	files := facts.Paths(l.in.TypeScript)
	imported := false
	for _, file := range files {
		f := l.in.TypeScript[file]
		for _, imp := range f.Imports {
			imported = imported || strings.HasPrefix(imp.Module, "@tauri-apps/api")
		}
		for _, c := range f.ExportClauses {
			imported = imported || strings.HasPrefix(c.Module, "@tauri-apps/api")
		}
	}
	sites := map[string]*site{}
	var frontier []*site
	for _, file := range files {
		d := l.in.TypeScript[file].Routes
		if d == nil {
			continue
		}
		for _, c := range d.Clients {
			form := l.direct(file, c, imported)
			if form == "" {
				continue
			}
			s := &site{file: file, call: c, form: form}
			sites[siteKey(file, c.Span)] = s
			if form != "global" {
				frontier = append(frontier, s)
			}
		}
	}
	wrappers := map[string]wrapper{}
	for level := 1; level <= 3 && len(frontier) > 0; level++ {
		found := map[string]wrapper{}
		invalid := map[string]bool{}
		for _, s := range frontier {
			parameter, ok := l.forwards(s)
			if !ok {
				continue
			}
			owner := s.call.Owner
			if _, known := wrappers[owner]; known {
				continue
			}
			if w, seen := found[owner]; seen && w.parameter != parameter {
				invalid[owner] = true
			}
			found[owner] = wrapper{parameter: parameter, level: level}
		}
		added := map[string]bool{}
		for owner, w := range found {
			if !invalid[owner] {
				wrappers[owner] = w
				added[owner] = true
			}
		}
		frontier = nil
		if len(added) == 0 {
			break
		}
		for _, file := range files {
			d := l.in.TypeScript[file].Routes
			if d == nil {
				continue
			}
			for _, c := range d.Clients {
				key := siteKey(file, c.Span)
				if _, done := sites[key]; done {
					continue
				}
				target := l.wrapperCallee(file, c)
				if !added[target] {
					continue
				}
				s := &site{file: file, call: c, form: "wrapper", wrapper: target, argument: wrappers[target].parameter}
				sites[key] = s
				frontier = append(frontier, s)
			}
		}
	}
	byFile := map[string][]*site{}
	for _, s := range sites {
		byFile[s.file] = append(byFile[s.file], s)
	}
	for _, file := range files {
		list := byFile[file]
		sort.Slice(list, func(i, j int) bool {
			a, b := list[i].call.Span, list[j].call.Span
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
		duplicates := map[string]int{}
		for _, s := range list {
			inner := false
			if w, ok := wrappers[s.call.Owner]; ok && s.form != "global" {
				parameter, forwards := l.forwards(s)
				inner = forwards && parameter == w.parameter
			}
			l.emitCall(s, inner, duplicates)
		}
	}
}

// direct recognizes I1 and I6 forms. Callees bind through the TypeScript
// module resolver, so a scanned package shadowing @tauri-apps/api and
// type-only imports never qualify.
func (l *linker) direct(file string, c facts.ClientCall, imported bool) string {
	if c.Expression.Object == nil {
		return ""
	}
	callee := *c.Expression.Object
	root, members, ok := memberChain(callee)
	if !ok {
		return ""
	}
	path := strings.Join(members, ".")
	global := root == "window" && (path == "__TAURI__.core.invoke" || path == "__TAURI_INTERNALS__.invoke")
	if !global && !imported {
		// No file imports @tauri-apps/api, so no callee can bind to it.
		return ""
	}
	b, _, bound := clients.Binding(l.in.TypeScript, file, c.Scope, root)
	if !bound {
		if global && !l.fileNode(file, "window") {
			return "global"
		}
		return ""
	}
	if !imported || b.Module == "" || b.Value.Invalid {
		return ""
	}
	e := l.in.Modules.ImportBinding(file, b.Module, b.Export, path)
	if !e.External || e.Ambiguous {
		return ""
	}
	if e.File == "@tauri-apps/api/core" && e.Name == "invoke" || e.File == "@tauri-apps/api" && e.Name == "core.invoke" {
		if len(members) == 0 {
			return "direct"
		}
		return "namespace"
	}
	return ""
}

// memberChain flattens `a.b.c` into its root identifier and member names.
func memberChain(e facts.RouteExpr) (string, []string, bool) {
	var members []string
	for e.Kind == "member_expression" && e.Object != nil {
		members = append([]string{e.Name}, members...)
		e = *e.Object
	}
	if e.Kind != "identifier" || e.Name == "" {
		return "", nil, false
	}
	return e.Name, members, true
}

func (l *linker) fileNode(file, name string) bool {
	for _, n := range l.in.TypeScript[file].Nodes {
		if n.QualifiedName == name {
			return true
		}
	}
	return false
}

// forwards reports whether a site passes one of its owner's own parameters,
// unchanged, as the command, and that parameter's position.
func (l *linker) forwards(s *site) (int, bool) {
	d := l.in.TypeScript[s.file].Routes
	if d == nil {
		return 0, false
	}
	params, ok := d.Parameters[s.call.Owner]
	if !ok {
		return 0, false
	}
	arg := argument(s.call.Expression.Args, s.argument)
	if arg.Kind != "identifier" {
		return 0, false
	}
	for i, p := range params {
		if p.Name == arg.Name && p.Name != "" {
			b, at, bound := clients.Binding(l.in.TypeScript, s.file, s.call.Scope, arg.Name)
			if !bound || b.Value.Kind != "parameter" || at != d.ParameterScopes[s.call.Owner] {
				return i, false
			}
			source, ok := l.ownerSource(s.file, s.call.Owner)
			return i, ok && !writesParameter(source, arg.Name, mayHoldJSX(s.file))
		}
	}
	return 0, false
}

// ownerSource returns the source text of a callable node, for the m1 write
// check. A callable without a node span is not a wrapper.
func (l *linker) ownerSource(file, owner string) ([]byte, bool) {
	for _, n := range l.in.TypeScript[file].Nodes {
		if n.Identity != owner || n.Span == nil {
			continue
		}
		start, ok := l.offset(file, n.Span.StartLine, n.Span.StartColumn)
		end, ok2 := l.offset(file, n.Span.EndLine, n.Span.EndColumn)
		if !ok || !ok2 || end < start {
			return nil, false
		}
		return l.in.Sources[file][start:end], true
	}
	return nil, false
}

// mayHoldJSX reports whether a file may contain JSX: every TypeScript or
// JavaScript extension except .ts, .mts and .cts (a .js file may hold JSX
// too, so the write check treats it like .jsx).
func mayHoldJSX(file string) bool {
	switch path.Ext(file) {
	case ".ts", ".mts", ".cts":
		return false
	}
	return true
}

func argument(args []facts.RouteExpr, i int) facts.RouteExpr {
	if i >= 0 && i < len(args) {
		return args[i]
	}
	return facts.RouteExpr{}
}

// wrapperCallee resolves a call's callee to a callable identity through the
// module resolver (imports and namespace members) or, for a local callable,
// through the same-file scope-0 binding plus the file's node with that name.
// A nearer shadowing declaration leaves the callee unresolved.
func (l *linker) wrapperCallee(file string, c facts.ClientCall) string {
	if c.Expression.Object == nil {
		return ""
	}
	root, members, ok := memberChain(*c.Expression.Object)
	if !ok {
		return ""
	}
	b, at, bound := clients.Binding(l.in.TypeScript, file, c.Scope, root)
	if !bound || b.Value.Invalid {
		return ""
	}
	if b.Module != "" {
		e := l.in.Modules.ImportBinding(file, b.Module, b.Export, strings.Join(members, "."))
		if e.External || e.Ambiguous {
			return ""
		}
		return e.Identity
	}
	if at != 0 || len(members) > 0 || b.Value.Kind != "function" {
		return ""
	}
	identity := ""
	for _, n := range l.in.TypeScript[file].Nodes {
		if n.QualifiedName == root {
			if identity != "" {
				return ""
			}
			identity = n.Identity
		}
	}
	return identity
}

// alternative is one evaluated command name with its match. A name that
// several match arms may take (cfg-gated duplicates, M1) gives one linked
// alternative per arm, each with its target and that arm's cfg_gated.
type alternative struct {
	command               *string
	status                string
	plugin, pluginCommand *string
	target                string
	cfgGated              bool
	candidates            []string
}

// emitCall writes the ipc_call node of one site with its contains edge and
// one invokes_command edge per linked target.
func (l *linker) emitCall(s *site, inner bool, duplicates map[string]int) {
	c := s.call
	arg := argument(c.Expression.Args, s.argument)
	text := clients.Compact(arg.Text)
	if text == "" {
		text = "(none)"
	}
	name := "INVOKE " + text
	qualified := facts.Join(c.OwnerName, name)
	duplicates[qualified]++
	if n := duplicates[qualified]; n > 1 {
		qualified += fmt.Sprintf("@%d", n)
	}
	identity := facts.ID("ipc_call", s.file, qualified)
	test := l.test(s.file)
	attrs := map[string]any{"framework": "tauri", "transport": "tauri_ipc", "form": s.form, "command": nil, "conditional": false}
	if s.wrapper != "" {
		attrs["wrapper_identity"] = s.wrapper
	}
	if test {
		attrs["scope"] = "test"
	}
	var alternatives []alternative
	if inner {
		attrs["wrapper"] = true
		attrs["status"] = "wrapper"
	} else {
		alternatives = l.alternatives(s, arg, test)
		statuses := map[string]bool{}
		for _, a := range alternatives {
			statuses[a.status] = true
		}
		for _, status := range precedence {
			if statuses[status] {
				attrs["status"] = status
				break
			}
		}
		if len(alternatives) == 1 {
			a := alternatives[0]
			attrs["command"] = nullable(a.command)
			if a.plugin != nil {
				attrs["plugin"], attrs["plugin_command"] = *a.plugin, *a.pluginCommand
			}
		} else {
			attrs["conditional"] = true
			names := map[string]bool{}
			dynamic := false
			var list []any
			for _, a := range alternatives {
				entry := map[string]any{"command": nullable(a.command), "status": a.status, "conditional": true}
				if a.plugin != nil {
					entry["plugin"], entry["plugin_command"] = *a.plugin, *a.pluginCommand
				}
				if a.status == "linked" {
					entry["target"], entry["cfg_gated"] = a.target, a.cfgGated
				}
				if a.command == nil {
					dynamic = true
				} else {
					names[*a.command] = true
				}
				list = append(list, entry)
			}
			attrs["alternatives"] = list
			// One known name taken by several arms keeps its command.
			if len(names) == 1 && !dynamic {
				attrs["command"] = nullable(alternatives[0].command)
			}
		}
		if status := attrs["status"]; status == "ambiguous" || status == "test_only" {
			seen := map[string]bool{}
			candidates := []string{}
			for _, a := range alternatives {
				if a.status != status {
					continue
				}
				for _, candidate := range a.candidates {
					if !seen[candidate] {
						seen[candidate] = true
						candidates = append(candidates, candidate)
					}
				}
			}
			sort.Strings(candidates)
			if len(candidates) > 10 {
				candidates = candidates[:10]
			}
			attrs["candidates"] = candidates
		}
	}
	span := c.Span
	l.nodes = append(l.nodes, contract.Node{Type: "node", Kind: "ipc_call", Level: 4, Identity: identity, Name: name, QualifiedName: qualified, File: s.file, Span: &span, ContentHash: c.Hash, Attributes: attrs})
	l.edges = append(l.edges, facts.Edge("contains", c.Owner, identity, s.file, span))
	linked := map[string]bool{}
	for _, a := range alternatives {
		if a.status == "linked" && !linked[a.target] {
			linked[a.target] = true
			l.edges = append(l.edges, facts.Edge("invokes_command", identity, a.target, s.file, span))
		}
	}
}

// alternatives evaluates the command argument (I4) and matches each distinct
// alternative on its own, sorted by name with an unknown name last.
func (l *linker) alternatives(s *site, arg facts.RouteExpr, test bool) []alternative {
	values := clients.Evaluate(l.in.TypeScript, l.in.Modules, s.file, s.call.URLScope, arg)
	known := map[string]bool{}
	var names []string
	unknown := len(values) == 0
	for _, v := range values {
		if !v.Known || v.Kind != "string" || hasControl(v.Text) {
			unknown = true
			continue
		}
		if !known[v.Text] {
			known[v.Text] = true
			names = append(names, v.Text)
		}
	}
	sort.Strings(names)
	var out []alternative
	for _, n := range names {
		name := n
		if s.form == "global" {
			out = append(out, alternative{command: &name, status: "global_unlinked"})
			continue
		}
		out = append(out, l.match(&name, test)...)
	}
	if unknown {
		out = append(out, alternative{status: "dynamic"})
	}
	return out
}

// match applies the matching order: plugin prefix, then exact comparison
// with the one proven app registry (A1, A2 first), then test scope. Arms are
// walked in entry order (T3) and collected until the first arm that is not
// cfg-gated (M1) or that completes a P / not(P) pair (Revision 3, mi1): each collected arm is the target in some build, so the
// name links to all of them. A production call does not see test-only arms
// (m2); when only such arms carry the name it is test_only.
func (l *linker) match(name *string, test bool) []alternative {
	one := func(a alternative) []alternative {
		a.command = name
		return []alternative{a}
	}
	if rest, ok := strings.CutPrefix(*name, "plugin:"); ok {
		parts := strings.Split(rest, "|")
		plugin, command := parts[0], ""
		if len(parts) > 1 {
			command = parts[1]
		}
		return one(alternative{status: "plugin", plugin: &plugin, pluginCommand: &command})
	}
	switch {
	case l.app.unproven:
		return one(alternative{status: "registry_unproven"})
	case l.app.ambiguous:
		a := alternative{status: "ambiguous"}
		for _, r := range l.app.apps {
			for _, e := range r.entries {
				if e.name != nil && *e.name == *name {
					a.candidates = append(a.candidates, e.identity)
					break
				}
			}
		}
		return one(a)
	case l.app.app == nil:
		return one(alternative{status: "unmatched"})
	case l.app.app.acl:
		return one(alternative{status: "acl_possible"})
	}
	r := l.app.app
	unknownBefore := false
	var arms, hidden []entry
	// predicates holds the cfg predicates of the arms seen so far, hidden
	// ones included: a P / not(P) pair covers every build (mi1).
	var predicates []string
	for _, e := range r.entries {
		visible := test || !e.testOnly
		if e.name == nil {
			unknownBefore = unknownBefore || visible
			continue
		}
		if *e.name != *name {
			continue
		}
		pair := complements(predicates, e.cfg)
		if e.cfg != "" {
			predicates = append(predicates, e.cfg)
		}
		if !visible {
			hidden = append(hidden, e)
			if pair {
				break
			}
			continue
		}
		if unknownBefore {
			// An unknown arm before this one could match first (T3).
			return one(alternative{status: "shadow_possible"})
		}
		arms = append(arms, e)
		if !e.gated || pair {
			break
		}
	}
	if len(arms) == 0 {
		switch {
		case len(hidden) > 0:
			a := alternative{status: "test_only"}
			for _, e := range hidden {
				a.candidates = append(a.candidates, e.identity)
			}
			return one(a)
		case unknownBefore:
			return one(alternative{status: "unknown_command"})
		}
		return one(alternative{status: "unmatched"})
	}
	if r.test && !test {
		a := alternative{status: "test_only"}
		for _, e := range arms {
			a.candidates = append(a.candidates, e.identity)
		}
		return one(a)
	}
	var out []alternative
	for _, e := range arms {
		out = append(out, alternative{command: name, status: "linked", target: e.identity, cfgGated: e.cfgGated})
	}
	return out
}

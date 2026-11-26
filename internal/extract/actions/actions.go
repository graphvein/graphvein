// Package actions links React server actions in Next.js projects (T024b,
// ADR 0077): one server_action node per action of a 'use server' module or
// inline 'use server' function, an exposes_action edge from the function
// that implements it, and one action_call node per proven use with its
// calls_action edge. The rows are those of docs/extract/server-actions.md;
// a form no row covers goes to the single default branch D and is counted,
// never guessed.
//
// It reads the cached action syntax (facts.ActionFacts), the route scopes
// (facts.RouteFacts) and the TypeScript module resolver at link time, so
// nothing it decides is cached.
package actions

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Transport is the API transport name of server actions (O1 (a)).
const Transport = "server_action"

// Input carries the link-time inventories the linker reads: the
// TypeScript facts and module resolver the index builds for its other
// linkers, and every scanned source (package.json files decide which
// directories are Next.js projects).
type Input struct {
	TypeScript map[string]facts.Facts
	Modules    *typescript.ModuleResolver
	Sources    map[string][]byte
	Tests      testscope.Rules
}

// action is one proven server action.
type action struct {
	identity string
	file     string
	// handler is the node of the implementing binding; "" when the
	// extractor emitted none (an anonymous inline action).
	handler string
	// index is the node's position in linker.nodes, for the unread use
	// count added after the uses are read.
	index  int
	unread int
}

type linker struct {
	in          Input
	nodes       []contract.Node
	edges       []contract.Edge
	diagnostics []contract.Diagnostic
	// byIdentity maps an implementing node to its action.
	byIdentity map[string]*action
	// byLocal maps file and top-level local name to a module action.
	byLocal map[string]map[string]*action
	// byScope maps file and function scope to an inline action.
	byScope map[string]map[int]*action
	// byOffset maps file and function start to an inline action, for an
	// inline action written as an attribute value.
	byOffset map[string]map[int]*action
	// reexported maps a 'use server' file and export name to an action
	// whose implementation is imported (form imported_clause); only an
	// import of that file names it.
	reexported map[string]map[string]*action
	// server holds the files reached from a server entry point without a
	// 'use client' boundary (row SA-C3, R1).
	server map[string]bool
	// serverOpen is the first server-graph file (path order) holding a
	// dynamic import() or require() the resolver cannot follow: the module
	// it loads is unknown, so the server graph may miss modules and no
	// reached module is proven client-only (row SA-C3).
	serverOpen string
	// client maps a file of the client module graph to the 'use client'
	// file it is reached from (itself for a 'use client' file).
	client map[string]string
}

// Link emits server_action and action_call nodes, their contains edges,
// exposes_action and calls_action edges, and the counted refusals
// (server_action_unread, server_action_use_unread). Each record depends
// only on its own file's syntax and the current resolver, so a problem in
// one file never stops the others.
func Link(in Input) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	l := &linker{in: in, byIdentity: map[string]*action{}, byLocal: map[string]map[string]*action{}, byScope: map[string]map[int]*action{},
		byOffset: map[string]map[int]*action{}, reexported: map[string]map[string]*action{}}
	projects := nextProjects(in.Sources)
	if len(projects) == 0 {
		return nil, nil, nil
	}
	paths := facts.Paths(in.TypeScript)
	for _, file := range paths {
		if a := in.TypeScript[file].Actions; a != nil && inProject(projects, file) {
			l.declarations(file, a)
		}
	}
	if len(l.nodes) == 0 {
		return nil, nil, l.diagnostics
	}
	l.clientGraph(paths)
	l.serverGraph(paths, projects)
	for _, file := range paths {
		l.uses(file)
	}
	l.countUnread()
	sort.Slice(l.nodes, func(i, j int) bool { return l.nodes[i].Identity < l.nodes[j].Identity })
	return l.nodes, l.edges, l.diagnostics
}

// diagnose counts a refused form (row D).
func (l *linker) diagnose(file, code string, s contract.Span, message string) {
	tag("D")
	l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "info", Code: code, Message: fmt.Sprintf("%s:%d:%d: %s", file, s.StartLine, s.StartColumn, message)})
}

func (l *linker) test(file string) bool {
	return testscope.Classify(file, l.in.Tests) == testscope.Test
}

func directive(a *facts.ActionFacts, name string) bool {
	for _, d := range a.Directives {
		if d == name {
			return true
		}
	}
	return false
}

// moduleForms are the SA-M1 export forms that make an action.
var moduleForms = map[string]bool{"async_function": true, "const_async_arrow": true, "const_async_function": true, "const_call": true, "clause": true, "imported_clause": true, "default_async_function": true,
	"default_async_arrow": true, "default_call": true, "default_clause": true}

// inlineForms are the SA-I1 forms that make an action.
var inlineForms = map[string]bool{"named_function": true, "const_arrow": true, "const_function": true, "let_binding": true, "anonymous": true}

// declarations reads the actions one file declares (rows SA-M1, SA-I1).
func (l *linker) declarations(file string, a *facts.ActionFacts) {
	server, client := directive(a, "use server"), directive(a, "use client")
	fileID := facts.ID("file", file, file)
	repeats := map[string]int{}
	qualify := func(q string) string {
		repeats[q]++
		if n := repeats[q]; n > 1 {
			q += fmt.Sprintf("@%d", n)
		}
		return q
	}
	module := map[string]*action{}
	switch {
	case server && client:
		if len(a.Exports) > 0 {
			l.diagnose(file, "server_action_unread", a.Exports[0].Span, "both 'use server' and 'use client' open the file (a Next.js build error); its exports are not actions")
		}
	case server:
		for _, e := range a.Exports {
			if !moduleForms[e.Form] {
				l.diagnose(file, "server_action_unread", e.Span, fmt.Sprintf("export %s of a 'use server' module is not read as an action (form %s)", e.Name, e.Form))
				continue
			}
			tag("SA-M1")
			local := e.Local
			if local == "" && e.Name != "default" {
				local = e.Name
			}
			if local == "" && e.Identity != "" {
				// A named default export binds its own name locally.
				if _, _, q, err := contract.ParseIdentity(e.Identity); err == nil && !strings.Contains(q, ".") && q != "default" {
					local = q
				}
			}
			handler := e.Identity
			if e.Form == "imported_clause" {
				// The implementation is the imported binding; uses name
				// the action only through an import of this file.
				handler = ""
				if b := l.in.Modules.ImportBinding(file, e.Module, e.Imported, ""); !b.Ambiguous && !b.External {
					if kind, _, _, err := contract.ParseIdentity(b.Identity); err == nil && (kind == "function" || kind == "variable") {
						handler = b.Identity
					}
				}
				act := l.emit(file, fileID, "", qualify("ACTION "+e.Name), e.Name, "", e.Span, e.Hash, map[string]any{"action_scope": "module", "form": e.Form})
				if handler != "" {
					l.nodes[act.index].Attributes["handler"] = handler
					l.edges = append(l.edges, facts.Edge("exposes_action", handler, act.identity, file, e.Span))
				}
				if l.reexported[file] == nil {
					l.reexported[file] = map[string]*action{}
				}
				l.reexported[file][e.Name] = act
				continue
			}
			act := l.emit(file, fileID, "", qualify("ACTION "+e.Name), e.Name, handler, e.Span, e.Hash, map[string]any{"action_scope": "module", "form": e.Form})
			if local != "" {
				if l.byLocal[file] == nil {
					l.byLocal[file] = map[string]*action{}
				}
				l.byLocal[file][local] = act
			}
			if e.Identity != "" {
				module[e.Identity] = act
			}
			if e.Name == "default" {
				// An import of the file's default export names the
				// action whatever binding (or none) the export has.
				if l.reexported[file] == nil {
					l.reexported[file] = map[string]*action{}
				}
				l.reexported[file]["default"] = act
			}
		}
	}
	for _, f := range a.Inline {
		if existing := module[f.Identity]; f.Identity != "" && existing != nil {
			// An exported function of a 'use server' module whose body
			// repeats the directive is the same action.
			l.scope(file, f.Scope, existing)
			continue
		}
		if client {
			l.diagnose(file, "server_action_unread", f.Span, "an inline 'use server' function in a 'use client' file (a Next.js build error)")
			continue
		}
		if !inlineForms[f.Form] || !f.Async {
			l.diagnose(file, "server_action_unread", f.Span, fmt.Sprintf("an inline 'use server' function is not read as an action (form %s)", f.Form))
			continue
		}
		tag("SA-I1")
		name := f.Name
		label := name
		if name == "" {
			label = "(anonymous)"
		}
		owner := f.Owner
		if owner == "" {
			owner = fileID
		}
		var nameAttr any = name
		if name == "" {
			nameAttr = nil
		}
		extra := map[string]any{"action_scope": "inline", "form": f.Form}
		if f.Reassigned {
			extra["reassigned"] = true
		}
		act := l.emit(file, owner, f.OwnerName, qualify(facts.Join(f.OwnerName, "ACTION "+label)), nameAttr, f.Identity, f.Span, f.Hash, extra)
		if l.byOffset[file] == nil {
			l.byOffset[file] = map[int]*action{}
		}
		l.byOffset[file][f.Offset] = act
		if !f.Reassigned {
			// A reassigned let or var binding may name another value
			// at a use, so only the function itself is the action.
			l.scope(file, f.Scope, act)
		}
	}
}

func (l *linker) scope(file string, scope int, act *action) {
	if l.byScope[file] == nil {
		l.byScope[file] = map[int]*action{}
	}
	l.byScope[file][scope] = act
}

// emit appends one server_action node with its contains edge and, when the
// implementing node is known, its exposes_action edge.
func (l *linker) emit(file, owner, ownerName, qualified string, name any, handler string, s contract.Span, hash string, extra map[string]any) *action {
	identity := facts.ID("server_action", file, qualified)
	attrs := map[string]any{"framework": "nextjs", "transport": Transport, "action": name, "handler": nil}
	for k, v := range extra {
		attrs[k] = v
	}
	if handler != "" {
		attrs["handler"] = handler
	}
	if l.test(file) {
		attrs["scope"] = "test"
	}
	display := qualified
	if i := strings.LastIndex(qualified, "ACTION "); i >= 0 {
		display = qualified[i:]
	}
	span := s
	l.nodes = append(l.nodes, contract.Node{Type: "node", Kind: "server_action", Level: 4, Identity: identity, Name: display, QualifiedName: qualified, File: file, Span: &span, ContentHash: hash, Attributes: attrs})
	l.edges = append(l.edges, facts.Edge("contains", owner, identity, file, span))
	act := &action{identity: identity, file: file, handler: handler, index: len(l.nodes) - 1}
	if handler != "" {
		l.edges = append(l.edges, facts.Edge("exposes_action", handler, identity, file, span))
		l.byIdentity[handler] = act
	}
	return act
}

// nextProjects lists the directories whose package.json declares `next`
// (as row NX-P1 finds Next.js projects).
func nextProjects(sources map[string][]byte) []string {
	var out []string
	for file, data := range sources {
		if path.Base(file) != "package.json" {
			continue
		}
		var manifest struct {
			Dependencies    map[string]any `json:"dependencies"`
			DevDependencies map[string]any `json:"devDependencies"`
		}
		if json.Unmarshal(data, &manifest) != nil {
			continue
		}
		_, dep := manifest.Dependencies["next"]
		_, dev := manifest.DevDependencies["next"]
		if dep || dev {
			out = append(out, path.Dir(file))
		}
	}
	sort.Strings(out)
	return out
}

func inProject(projects []string, file string) bool {
	for _, p := range projects {
		if p == "." || strings.HasPrefix(file, p+"/") {
			return true
		}
	}
	return false
}

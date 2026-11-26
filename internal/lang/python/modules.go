package python

import (
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Module is a module the structural resolution (ModuleResolver.structural,
// rows PS-I1..PS-I7 of docs/lang/python-structure.md) found: a source
// module or regular package (File, and Dirs for a package's __path__), a
// namespace package (Dirs only), an external module (External, with the
// full dotted name so framework allowlists stay exact) or several sibling
// roots' candidates (Ambiguous). The zero Module is an import the contract
// refuses or cannot find: nothing is known about it.
type Module struct {
	File      string
	Dirs      []string
	Name      string
	External  bool
	Ambiguous []string
}

// Binding identifies a terminal scanned declaration or external export.
type Binding struct {
	File, Name, Identity string
	Scope                int
	Syntax               facts.PythonBinding
	Module               *Module
	External             bool
	ViaAssignment        bool
}

// ModuleResolver shares current-inventory resolution across every Python consumer.
type ModuleResolver struct {
	files    map[string]facts.Facts
	roots    []string
	implicit []string
	// rootSet and implicitSet hold roots and implicit for lookups.
	rootSet, implicitSet map[string]bool
	// directories holds every directory the scan saw (a namespace
	// portion to Python's import system); pyDirs only those holding
	// scanned Python files (the src probe of PS-M1).
	directories map[string]bool
	pyDirs      map[string]bool
	resolved    map[string]Module
	scopes      map[string]map[string]int
	callables   map[callableKey]Binding
	// unread maps a directory to the names of its entries Graphvein does
	// not read as Python source (scanned files of other kinds and the
	// scanner's skipped entries); unreadDirs holds skipped directories.
	// Only the structural import rows read them (shadowed_module).
	unread     map[string][]string
	unreadDirs map[string]bool
	// pyprojects holds the text of every scanned pyproject.toml by
	// directory; ws holds row PS-M2's declared environments and refusals,
	// read from it on first use (after WithSkipped and WithDirectories
	// have added the scanner's entries).
	pyprojects map[string]string
	ws         *workspaceState
}

type callableKey struct {
	file  string
	scope int
	name  string
}

// NewModuleResolver rebuilds explicit and implicit roots from the current scan
// so cached importers respond to metadata and package-initializer changes.
func NewModuleResolver(files map[string]facts.Facts, sources map[string][]byte) *ModuleResolver {
	r := &ModuleResolver{files: files, directories: map[string]bool{}, pyDirs: map[string]bool{}, rootSet: map[string]bool{}, implicitSet: map[string]bool{}, resolved: map[string]Module{}, scopes: map[string]map[string]int{}, callables: map[callableKey]Binding{}, unread: map[string][]string{}, unreadDirs: map[string]bool{}, pyprojects: map[string]string{}}
	for file := range sources {
		if _, ok := files[file]; !ok || !strings.HasSuffix(file, ".py") {
			r.unread[path.Dir(file)] = append(r.unread[path.Dir(file)], path.Base(file))
		}
		r.addDirectories(file)
	}
	roots := map[string]bool{".": true}
	for file := range files {
		r.scopes[file] = map[string]int{}
		if p := files[file].Python; p != nil {
			for i, s := range p.Scopes {
				r.scopes[file][s.Name] = i
			}
		}
		for dir := path.Dir(file); ; dir = path.Dir(dir) {
			r.pyDirs[dir] = true
			if dir == "." {
				break
			}
		}
		r.addDirectories(file)
	}
	for file := range sources {
		switch path.Base(file) {
		case "pyproject.toml", "setup.py", "setup.cfg":
			dir := path.Dir(file)
			roots[dir] = true
			if path.Base(file) == "pyproject.toml" {
				r.pyprojects[dir] = string(sources[file])
			}
		}
	}
	// Every explicit root, including the repository root, has its documented
	// src probe when that directory contains scanned Python sources.
	srcRoots := map[string]bool{}
	for root := range roots {
		src := path.Join(root, "src")
		if r.pyDirs[src] {
			srcRoots[src] = true
		}
	}
	for root := range srcRoots {
		roots[root] = true
	}
	for root := range roots {
		r.roots = append(r.roots, root)
		r.rootSet[root] = true
	}
	sort.Strings(r.roots)
	implicit := map[string]bool{}
	for file := range files {
		if path.Base(file) != "__init__.py" {
			continue
		}
		parent := path.Dir(path.Dir(file))
		if _, regular := files[path.Join(parent, "__init__.py")]; !regular && !roots[parent] {
			implicit[parent] = true
		}
	}
	for root := range implicit {
		r.implicit = append(r.implicit, root)
		r.implicitSet[root] = true
	}
	sort.Slice(r.implicit, func(i, j int) bool {
		if len(r.implicit[i]) != len(r.implicit[j]) {
			return len(r.implicit[i]) > len(r.implicit[j])
		}
		return r.implicit[i] < r.implicit[j]
	})
	return r
}

// addDirectories records the directories above p.
func (r *ModuleResolver) addDirectories(p string) {
	for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
		if r.directories[dir] {
			return
		}
		r.directories[dir] = true
	}
}

// WithSkipped adds the scanner's skipped entries (scan.Result.Skipped:
// files it did not read, directories with a trailing "/") to the entries
// the structural import rows treat as unread, and returns r.
func (r *ModuleResolver) WithSkipped(skipped []string) *ModuleResolver {
	for _, p := range skipped {
		if strings.HasSuffix(p, "/") {
			p = strings.TrimSuffix(p, "/")
			r.unreadDirs[p] = true
			r.addDirectories(p)
			continue
		}
		r.unread[path.Dir(p)] = append(r.unread[path.Dir(p)], path.Base(p))
		r.addDirectories(p)
	}
	return r
}

// WithDirectories adds every directory the scanner walked
// (scan.Result.Dirs), including those holding no scanned file: Python's
// import system reads any directory as a namespace portion. It returns r.
func (r *ModuleResolver) WithDirectories(dirs []string) *ModuleResolver {
	for _, dir := range dirs {
		r.directories[dir] = true
	}
	return r
}

func ancestor(dir, file string) bool {
	return dir == "." || file == dir || strings.HasPrefix(file, dir+"/")
}

// Resolve resolves an import's module for calls, routes and models with
// the structural rows' resolution (structural): the same search path,
// meta-path, script-directory and shadowing refusals as the import edges.
// A refused or unavailable import is the zero Module.
func (r *ModuleResolver) Resolve(file, name string) Module {
	key := file + "\x00" + name
	if m, ok := r.resolved[key]; ok {
		return m
	}
	res := r.structural(file, name)
	var m Module
	switch {
	case res.refusal != "" || res.missing:
	case len(res.ambiguous) > 0:
		m = Module{Name: name, Ambiguous: res.ambiguous}
	case res.external:
		m = Module{Name: name, External: true}
	default:
		m = Module{File: res.target.file, Dirs: res.target.dirs, Name: name}
	}
	r.resolved[key] = m
	return m
}

func (m Module) found() bool { return m.File != "" || len(m.Dirs) > 0 }

// member resolves the attribute name of module m as `from m import name`
// binds it (fromList, shared with the import edges): m's own binding, a
// submodule, or nothing when the answer is undecided.
func (r *ModuleResolver) member(m Module, name string, seen map[string]bool) Binding {
	if m.External {
		return Binding{File: m.Name, Name: name, External: true}
	}
	if !m.found() || len(m.Ambiguous) > 0 {
		return Binding{}
	}
	sub, own, form := r.fromList(target{file: m.File, dirs: m.Dirs}, name)
	if form == unreadSelfBoundSubmodule {
		// `from . import name` in the package binds the submodule.
		own = false
		form = ""
	}
	if own {
		return r.lookup(m.File, 0, name, seen)
	}
	if form != "" || !sub.ok() {
		return Binding{}
	}
	module := Module{File: sub.file, Dirs: sub.dirs, Name: m.Name + "." + name}
	return Binding{Module: &module}
}

func (r *ModuleResolver) imported(file string, imp facts.Import, name string, seen map[string]bool) Binding {
	m := r.Resolve(file, imp.Module)
	if imp.From {
		if name == "*" {
			return Binding{}
		}
		return r.member(m, name, seen)
	}
	if !imp.ExplicitAlias && (m.found() || m.External) {
		// import a.b.c loads the full path but binds a. Walking a.b later
		// performs package-member/submodule resolution through this same inventory.
		m = r.Resolve(file, strings.Split(imp.Module, ".")[0])
	}
	if m.External {
		return Binding{External: true, File: m.Name, Name: ""}
	}
	if m.found() && len(m.Ambiguous) == 0 {
		return Binding{Module: &m}
	}
	return Binding{}
}

func (r *ModuleResolver) lookup(file string, scope int, name string, seen map[string]bool) Binding {
	p := r.files[file].Python
	if p == nil {
		return Binding{}
	}
	initial := scope
	for scope >= 0 && scope < len(p.Scopes) {
		// A function's bare names skip enclosing class namespaces. Explicit
		// Class.member lookups start in that class scope and still see methods.
		if scope != initial && p.Scopes[scope].Class {
			scope = p.Scopes[scope].Parent
			continue
		}
		if b, ok := p.Scopes[scope].Bindings[name]; ok {
			key := file + ":" + p.Scopes[scope].Name + ":" + name
			if seen[key] {
				return Binding{}
			}
			seen[key] = true
			defer delete(seen, key)
			if b.Import != nil {
				return r.imported(file, *b.Import, b.Imported, seen)
			}
			if b.Value.Kind == "identifier" || b.Value.Kind == "attribute" {
				v := r.expr(file, scope, b.Value, seen)
				v.ViaAssignment = true
				return v
			}
			return Binding{File: file, Scope: scope, Name: name, Identity: b.Identity, Syntax: b}
		}
		scope = p.Scopes[scope].Parent
	}
	return Binding{}
}

func (r *ModuleResolver) expr(file string, scope int, e facts.RouteExpr, seen map[string]bool) Binding {
	switch e.Kind {
	case "identifier":
		return r.lookup(file, scope, e.Name, seen)
	case "attribute":
		if e.Object == nil {
			return Binding{}
		}
		b := r.expr(file, scope, *e.Object, seen)
		if b.External {
			if b.Name == "" {
				b.Name = e.Name
			} else {
				b.Name += "." + e.Name
			}
			return b
		}
		if b.Module != nil {
			return r.member(*b.Module, e.Name, seen)
		}
		if b.Syntax.Value.Kind == "class_definition" {
			p := r.files[b.File].Python
			if p == nil || b.Syntax.ClassScope >= len(p.Scopes) {
				return Binding{}
			}
			if _, exists := p.Scopes[b.Syntax.ClassScope].Bindings[e.Name]; !exists {
				return Binding{}
			}
			return r.lookup(b.File, b.Syntax.ClassScope, e.Name, seen)
		}
	}
	return Binding{}
}

// Expression follows lexical bindings and per-name re-exports with cycle guards.
func (r *ModuleResolver) Expression(file string, scope int, e facts.RouteExpr) Binding {
	return r.expr(file, scope, e, map[string]bool{})
}

// ImportBinding follows from-import attributes before trying package submodules.
func (r *ModuleResolver) ImportBinding(file string, imp facts.Import, name string) Binding {
	return r.imported(file, imp, name, map[string]bool{})
}

// Scope locates a cached lexical scope for the legacy call candidates.
func (r *ModuleResolver) Scope(file, name string) int {
	return r.scopes[file][name]
}

// Callable resolves declarations without treating object instances as functions.
func (r *ModuleResolver) Callable(file string, scope int, name string) Binding {
	key := callableKey{file, scope, name}
	if b, ok := r.callables[key]; ok {
		return b
	}
	parts := strings.Split(name, ".")
	e := facts.RouteExpr{Kind: "identifier", Name: parts[0]}
	for _, part := range parts[1:] {
		previous := e
		e = facts.RouteExpr{Kind: "attribute", Name: part, Object: &previous}
	}
	b := r.Expression(file, scope, e)
	// Cache only the completed top-level traversal, never an in-progress cycle.
	r.callables[key] = b
	return b
}

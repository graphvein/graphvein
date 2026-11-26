package routes

import (
	"fmt"
	"sort"

	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
)

// Path-preserving wrappers (contract row GR-O7, task T023): a
// call of a module function or method whose syntax keeps a served
// request's path (facts.GoFacts.Wrappers, row RW-wrapper of
// docs/lang/go-routes.md) holds the router passed as its http.Handler
// argument, as http.Handler(x) does (GR-O6). The local proof is the
// capture's; the link pass resolves the wrapper and proves every call that
// receives the request before it is served (facts.GoFacts.Requests, row
// RQ-request), recursively through the module's own functions and
// methods. A wrapper that serves r.WithContext(x) keeps the URL but not
// the context, which chi reads its routing state from: it passes no chi
// router through.

// goRequestKey names one request helper record: its file and index.
type goRequestKey struct {
	file  string
	index int
}

// wrapperCall resolves a call to a proven wrapper: the index of its
// http.Handler argument and whether it serves r.WithContext. form names
// the contract form of the callee (GR-O7).
func (l *goLinker) wrapperCall(at goAt, e *facts.GoExpr) (param int, context bool, form string, ok bool) {
	param, context, form, _, ok = l.wrapperDecl(at, e)
	return param, context, form, ok
}

// wrapperDecl is wrapperCall with the methods the wrapper answers itself.
// A wrapper whose deciding conditions call a request
// helper that reads the request's URL or method (B1) is no proven
// wrapper.
func (l *goLinker) wrapperDecl(at goAt, e *facts.GoExpr) (param int, context bool, form string, methods []string, ok bool) {
	c := e.Callee
	if c == nil {
		return 0, false, "", nil, false
	}
	var files []string
	receiver, name := "", c.Name
	switch c.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, c.Name).kind != "func" {
			return 0, false, "", nil, false
		}
		files, form = l.ownFiles(at.rec.file), "`f(x)`"
	case facts.GoSelector:
		if c.Object == nil {
			return 0, false, "", nil, false
		}
		if imp, isImport := l.importOf(at, c.Object); isImport {
			if imp.files == nil {
				return 0, false, "", nil, false
			}
			files, form = imp.files, "`pkg.F(x)`"
			break
		}
		t, file := l.typeOf(at, c.Object)
		if t == nil {
			return 0, false, "", nil, false
		}
		files, receiver = l.typeFiles(file, t)
		if receiver == "" {
			return 0, false, "", nil, false
		}
		form = "`v.M(x)`"
	default:
		return 0, false, "", nil, false
	}
	found := false
	for _, p := range files {
		g := l.files[p].Go
		if g == nil {
			continue
		}
		for _, w := range g.Wrappers {
			if w.Name != name || w.Receiver != receiver {
				continue
			}
			if found || w.Param >= len(e.Args) || !l.passesKeepPath(p, w.Receiver, w.Passes) || l.guardsDecide(p, w.Receiver, w.Guards) {
				// Two declarations (build variants) or an unproven one.
				return 0, false, "", nil, false
			}
			found, param, context, methods = true, w.Param, w.Context, w.Methods
		}
	}
	return param, context, form, methods, found
}

// passesKeepPath reports that every call receiving the request resolves to
// request helpers that keep its path.
func (l *goLinker) passesKeepPath(file, receiver string, passes []facts.GoRequestPass) bool {
	l.requestSafety()
	for _, p := range passes {
		if !l.passSafe(file, receiver, p) {
			return false
		}
	}
	return true
}

// passSafe reports a pass whose every target is a safe request helper
// whose request parameter is the argument passed.
func (l *goLinker) passSafe(file, receiver string, p facts.GoRequestPass) bool {
	targets := l.passTargets(file, receiver, p)
	if len(targets) == 0 {
		return false
	}
	for _, t := range targets {
		if !l.safeRequests[t] || l.files[t.file].Go.Requests[t.index].Param != p.Arg {
			return false
		}
	}
	return true
}

// passTargets resolves a pass's callee: a function of the file's package
// (f), of an imported module package (pkg.F), or a method of the declaring
// method's receiver type (recv.M). Every declaration of that name is a
// target (build variants each must keep the path).
func (l *goLinker) passTargets(file, receiver string, p facts.GoRequestPass) []goRequestKey {
	c := p.Callee
	var files []string
	want := ""
	switch {
	case p.Method:
		if receiver == "" {
			return nil
		}
		files, want = l.ownFiles(file), receiver
	case c.Kind == facts.GoIdentifier:
		files = l.ownFiles(file)
	case c.Kind == facts.GoSelector && c.Object != nil:
		imp, ok := l.moduleImports(file)[c.Object.Name]
		if !ok || imp.files == nil {
			return nil
		}
		files = imp.files
	default:
		return nil
	}
	var out []goRequestKey
	for _, f := range files {
		g := l.files[f].Go
		if g == nil {
			continue
		}
		for i, r := range g.Requests {
			if r.Name == c.Name && r.Receiver == want {
				out = append(out, goRequestKey{f, i})
			}
		}
	}
	return out
}

// requestSafety decides, once per link, which request helpers keep the
// request's path: the greatest set in which every pass of every member
// resolves to members (a cycle of helpers that only read keeps the path;
// any write is local syntax the capture refused).
func (l *goLinker) requestSafety() {
	if l.safeRequests != nil {
		return
	}
	l.safeRequests = map[goRequestKey]bool{}
	var keys []goRequestKey
	for _, file := range facts.Paths(l.files) {
		if g := l.files[file].Go; g != nil && !g.Rejected && !g.LoneCR {
			for i := range g.Requests {
				k := goRequestKey{file, i}
				l.safeRequests[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].file != keys[j].file {
			return keys[i].file < keys[j].file
		}
		return keys[i].index < keys[j].index
	})
	for changed := true; changed; {
		changed = false
		for _, k := range keys {
			if !l.safeRequests[k] {
				continue
			}
			r := l.files[k.file].Go.Requests[k.index]
			for _, p := range r.Passes {
				if !l.passSafe(k.file, r.Receiver, p) {
					l.safeRequests[k] = false
					changed = true
					break
				}
			}
		}
	}
}

// moduleImports are the module-own package imports of any Go file (the
// recognised files' imports also hold the router packages): name to
// package files.
func (l *goLinker) moduleImports(file string) map[string]goImport {
	if imps, ok := l.imports[file]; ok {
		return imps
	}
	if imps, ok := l.plainImports[file]; ok {
		return imps
	}
	out := map[string]goImport{}
	base, module := golang.ModuleAt(file, l.modules)
	if module != "" && golang.CheckImportPath(module) == nil {
		for _, imp := range l.files[file].Imports {
			if golang.CheckImportPath(imp.Module) != nil {
				continue
			}
			dir, ok := golang.ModuleImportDir(base, module, imp.Module)
			if !ok {
				continue
			}
			name, targets, refusal := golang.ImportPackage(file, dir, l.files)
			if refusal != "" || name == "" {
				continue
			}
			for alias := range imp.Bindings {
				if !imp.ExplicitAlias {
					alias = name
				}
				if alias != "_" && alias != "." {
					out[alias] = goImport{path: imp.Module, files: targets}
				}
			}
		}
	}
	l.plainImports[file] = out
	return out
}

// wrapperValue is the value of a call of a proven wrapper (GR-O7): the
// value of its http.Handler argument, when that holds router objects and
// none is a chi router behind a wrapper that serves r.WithContext. ok is
// false otherwise (the call is read by the other rules).
func (l *goLinker) wrapperValue(at goAt, e *facts.GoExpr, site string) (goValue, bool) {
	param, context, form, methods, ok := l.wrapperDecl(at, e)
	if !ok {
		return goValue{}, false
	}
	v := copied(l.eval(at, &e.Args[param], fmt.Sprintf("%s/a%d", site, param)))
	if len(v.objects) == 0 {
		return goValue{}, false
	}
	for _, id := range v.objects {
		if context && l.objects[id].framework == goChi {
			return goValue{}, false
		}
	}
	if v.proven() {
		l.tag("GR-O7")
		l.tag("GR-O7/" + form)
	}
	v.methods = goUnion(v.methods, methods)
	return v, true
}

// guardsDecide reports that a deciding condition of a wrapper or a
// forwarding closure calls a request helper that reads the request's URL
// or method, directly or through its passes, or one no rule resolves:
// the literal may then answer some paths or methods
// itself, which the payload cannot see.
func (l *goLinker) guardsDecide(file, receiver string, guards []facts.GoRequestPass) bool {
	l.requestSafety()
	for _, g := range guards {
		targets := l.passTargets(file, receiver, g)
		if len(targets) == 0 {
			return true
		}
		for _, t := range targets {
			if l.requestReads(t) {
				return true
			}
		}
	}
	return false
}

// requestReads reports a request helper that reads r.URL, r.RequestURI or
// r.Method, itself or through a helper it passes the request to (an
// unresolved pass counts as a read).
func (l *goLinker) requestReads(k goRequestKey) bool {
	if l.reads == nil {
		l.reads = map[goRequestKey]bool{}
		var keys []goRequestKey
		for _, file := range facts.Paths(l.files) {
			if g := l.files[file].Go; g != nil && !g.Rejected && !g.LoneCR {
				for i, r := range g.Requests {
					key := goRequestKey{file, i}
					keys = append(keys, key)
					l.reads[key] = r.URL || r.Method
				}
			}
		}
		for changed := true; changed; {
			changed = false
			for _, key := range keys {
				if l.reads[key] {
					continue
				}
				r := l.files[key.file].Go.Requests[key.index]
				for _, p := range r.Passes {
					targets := l.passTargets(key.file, r.Receiver, p)
					hit := len(targets) == 0
					for _, t := range targets {
						hit = hit || l.reads[t]
					}
					if hit {
						l.reads[key] = true
						changed = true
						break
					}
				}
			}
		}
	}
	return l.reads[k]
}

// passThrough reports a call that GR-O7 reads as its handler argument, and
// that argument's index.
func (l *goLinker) passThrough(at goAt, e *facts.GoExpr, site string) (int, bool) {
	param, _, _, ok := l.wrapperCall(at, e)
	if !ok {
		return 0, false
	}
	if _, ok := l.wrapperValue(at, e, l.canonical(at, e, site)); !ok {
		return 0, false
	}
	return param, true
}

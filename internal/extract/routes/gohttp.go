package routes

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
	"github.com/graphvein/graphvein/internal/testscope"
)

// Go route extraction (task T023, ADR 0076; docs/extract/routes.md,
// section "Go"). The linker reads the cached router payload of gated Go
// files (facts.GoRoutes, docs/lang/go-routes.md) and the retained go.mod
// sources, never re-parsing a file. Step GR2 recognises router objects and
// registrations and emits route nodes, handler edges and diagnostics;
// paths are composed by step GR3, so every route's paths are empty here.

// Go router frameworks, as the route's framework attribute names them.
const (
	goNetHTTP = "net/http"
	goChi     = "chi"
	goGin     = "gin"
)

// goRouterImports are the import paths whose package a GR-K1 file reads,
// with the package name an unaliased import binds.
var goRouterImports = map[string]struct{ framework, name string }{
	"net/http":                 {goNetHTTP, "http"},
	"github.com/go-chi/chi/v5": {goChi, "chi"},
	"github.com/gin-gonic/gin": {goGin, "gin"},
}

// goImport is one name a file's imports bind.
type goImport struct {
	path string
	// framework is set for an external router package (GR-K1).
	framework string
	// files are the read files of a module-own package (empty for an
	// external package).
	files []string
}

// goRecord is one captured function declaration or literal.
type goRecord struct {
	file   string
	index  int
	key    string
	fn     *facts.GoRouteFunc
	parent *goRecord
	// decl is the enclosing declaration (the record itself for one), nil
	// for a literal in a package-level var.
	decl *goRecord
	// callback is set on a literal that is the callback of a GR-O1 call.
	callback bool
}

// goModuleInfo is what a module's go.mod (and its main packages'
// //go:debug lines) tell the route rules: GR-V2 and the framework
// versions of GR-V1.
type goModuleInfo struct {
	// legacy reports that GR-V2 does not hold: net/http patterns are read
	// with Go 1.21 semantics (legacy_mux_patterns).
	legacy bool
	// versions are the required versions of chi and gin.
	versions map[string]string
}

// goVerified are the framework versions the chi and gin rules are
// oracle-verified at (GR-V1).
var goVerified = map[string][]string{
	goChi: {"v5.2.4", "v5.5.0"},
	goGin: {"v1.10.0", "v1.12.0"},
}

// goModulePaths are the module paths of GR-V1's frameworks in go.mod.
var goModulePaths = map[string]string{goChi: "github.com/go-chi/chi/v5", goGin: "github.com/gin-gonic/gin"}

// goObject is one router object (rows GR-K2, GR-K4, GR-O1-GR-O6): a
// constructor site, the DefaultServeMux, a router-typed parameter or a
// child of another object. Objects are identified by their syntax site,
// so an object made in a factory exists once however often the factory
// is called.
type goObject struct {
	id        string
	framework string
	// kind is ctor, zero, default, param, route, group, with or ginGroup.
	kind string
	file string
	span contract.Span
	// parent is the object a child (route, group, with, ginGroup) was
	// made from; prefix is a route or gin group's prefix expression.
	parent string
	prefix *facts.GoExpr
	// middleware are a with object's arguments (GR-O2), its parent's
	// first when the parent is a with object too.
	middleware []string
	// escapes are the sites where the object is stored into a composite
	// literal (escaped_router): no path is claimed through it.
	escapes []string
	// at is where a child was made (its prefix is read there) and
	// conditional reports conditional context at that call or on the
	// bindings that carried the parent to it (GR-C5).
	at          goAt
	expr        *facts.GoExpr
	conditional bool
}

// goBinding records that a parameter holds an object in one call's
// context (GR-O1 callbacks and GR-O4 calls); step GR3 composes through
// these.
type goBinding struct {
	param, object string
	file          string
	span          contract.Span
	conditional   bool
	// at is the call that binds the parameter (middleware
	// registered before it applies to the routes the callee registers).
	at goAt
}

// goRegistration is one route registration (rows GR-R1-GR-R4).
type goRegistration struct {
	file, site string
	record     *goRecord
	call       *facts.GoRouteCall
	framework  string
	objects    []string
	method     string
	// pattern is the pattern as written (with a net/http method and
	// host); name is the qualified name's pattern part.
	pattern, name string
	host          string
	resolution    string
	handler       *facts.GoExpr
	static        string
	middleware    []string
	conditional   bool
	noPath        string
	order         int
	// oapi is the generated server of a GR-G1 registration.
	oapi *goOapiServer
	// identity is the route node's identity (set when emitted).
	identity string
	// sharedPath is set on a chi route whose method and full path another
	// route of the same served router has (row SH-S24): chi keeps the last
	// registration and prefers a static route to a mounted one, so this
	// registration may not serve the path.
	sharedPath bool
}

// goLinker links the Go route payload of one index run.
type goLinker struct {
	files   map[string]facts.Facts
	sources map[string][]byte
	rules   testscope.Rules
	tag     func(row string)

	modules   map[string]string
	moduleOf  map[string]goModuleInfo
	packages  map[string]golang.GoPackage
	imports   map[string]map[string]goImport
	records   map[string][]*goRecord
	byDecl    map[string]map[string]*goRecord
	objects   map[string]*goObject
	bindings  []goBinding
	memo      map[string]goValue
	active    map[string]bool
	factories map[string]goValue
	building  map[string]bool
	// routerMethods are the names of the module's captured methods that
	// declare a router result (factory_method_chain evidence).
	routerMethods map[string]bool
	// own and names cache ownFiles and packageName.
	own   map[string][]string
	names map[string]goName

	registrations []goRegistration
	// groupMW holds the router-level middleware read for task
	// T029 (gohttp_middleware.go): Use calls, group and With arguments
	// and constructors, and each emitted route's result.
	groupMW goGroupMW
	// nodes and edges are the emitted route nodes and edges.
	nodes []contract.Node
	edges []contract.Edge
	// compositions are the chi Mount and net/http GR-C3 compositions;
	// wrapped are the objects that reach a composition position only
	// inside another call or a closure (GR-C4), with the site; handlers
	// are the objects used directly as a registration's handler (no
	// composition reads them). Step GR3 composes paths through these.
	compositions []goComposition
	// graph is the composition graph of the last emit (tests read it).
	graph     *goGraph
	wrappedAt map[string]string
	handlers  map[string]bool
	// mountRefused are the routers of another framework chi Mount reads
	// (mount_unproven_handler).
	mountRefused map[string]bool
	// composed are the calls a GR-C3 composition or a wrapped-router
	// check has read (stripPrefixes skips them); refused are the
	// compositions whose outer pattern conflicts on its mux.
	composed map[*facts.GoExpr]bool
	// served are the serving positions that reach each object (row
	// GR-C6), servedKeys their keys; rootStarts are the chain starts of
	// each root.
	served       map[string][]goServe
	servedKeys   map[string]bool
	serveBlocked map[string]bool
	refused      map[int]bool
	unread       map[string]map[string]int
	counted      map[string]bool
	diagnostics  []contract.Diagnostic
	// safeRequests are the request helpers that keep a request's path
	// (GR-O7, decided once per link); plainImports cache the module
	// imports of files that are not recognised router files.
	safeRequests map[goRequestKey]bool
	// reads are the request helpers that read the request's URL or
	// method.
	reads map[goRequestKey]bool
	// refusedWhy names why a registration's paths were refused on every
	// chain (a method a wrapper answers, a sibling pattern: step GR8).
	refusedWhy map[string]string
	// hops are the unresolved mount hops by site (T029 FU-2): blocked
	// edges, wrapped and escaped routers.
	hops map[string]*mountHop
	// relChains are the chains composed from each hop's object;
	// handleHops the net/http Handle a wrapped router is passed to;
	// hopStarts the chains composed through each hop when the routers
	// above it are proven.
	relChains  map[string]map[string][]goChain
	handleHops map[*mountHop]goHandleHop
	hopStarts  map[*mountHop][]goChain
	// trees memoizes the chi objects sharing a routing tree;
	// direct the http.Handler parameters that are only served directly.
	trees  map[string]map[string]bool
	direct map[string]bool
	// ifaceMethods, funcValues and methodValues are the module's
	// interface method names and the functions and methods its router
	// syntax uses as values.
	ifaceMethods, funcValues, methodValues map[string]bool
	// callCounts memoizes the visible calls of each factory.
	callCounts   map[string]int
	plainImports map[string]map[string]goImport
	// oapi are the generated servers by file (row GR-G1).
	oapi map[string]*goOapiServer
	// muxOpaque are the ServeMux objects a Handle or HandleFunc call
	// registers on with no recorded registration or composition (a
	// dynamic, invalid or legacy pattern, a wrapped handler, a receiver
	// that may also hold another value): a ServeMux -> chi hop on them
	// (GR-C7) claims no path, since that call's pattern is unknown.
	muxOpaque map[string]bool
	// muxExtra are the constant patterns of the Handle and HandleFunc
	// calls on a ServeMux that record neither a registration nor a
	// composition (a wrapped or refused handler): known
	// siblings.
	muxExtra map[string][]goMuxExtra
	// muxOpaqueAt are where each muxOpaque call is (a sibling
	// in the other branch of an if never coexists with a hop).
	muxOpaqueAt map[string][]goAt
	// gates memoizes the GR-C7 sibling patterns of each net/http root;
	// gateRefused are the registrations a GR-C7 hop refused a path of,
	// gatePassed those it gave one (assumption GR-V2).
	gates       map[string]*goGate
	gateRefused map[string]bool
	gatePassed  map[string]bool
	// shapeDepth is the run's route shape depth limit (0: no shapes);
	// shapeIndex and nodeIndex index the captured shape functions and the
	// function nodes (T029 SH-G, goshape.go).
	shapeDepth int
	shapeIndex map[string]*goShapeRef
	nodeIndex  map[string]goNodeRef
}

// LinkGo emits the route nodes of Go's net/http ServeMux, chi v5 and gin
// v1 registrations (ADR 0076) from the cached Go router payload and the
// retained go.mod sources: node shape, handler edges and diagnostics as
// the Express, FastAPI and Axum routes (ADR 0013). Paths are composed by
// task T023's step GR3; until then every Go route has empty paths.
func LinkGo(files map[string]facts.Facts, sources map[string][]byte, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	return LinkGoShapes(files, sources, routeshape.DefaultDepth, scopeRules...)
}

// LinkGoShapes is LinkGo with the run's route shape depth limit
// (index.shape_depth, routeshape row SM-21): every route whose handler is
// read gets the shape attribute of task T029 (goshape.go).
func LinkGoShapes(files map[string]facts.Facts, sources map[string][]byte, shapeDepth int, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	nodes, edges, diagnostics, _ := LinkGoCounted(files, sources, shapeDepth, scopeRules...)
	return nodes, edges, diagnostics
}

// LinkGoCounted is LinkGoShapes plus the structured counts behind each
// go_routes_unread diagnostic: per file, the D forms by form, exactly the
// counts the diagnostic's message writes. The indexer stores them on the
// file node (attribute go_route_forms_outside_contract) so a consumer
// never parses the message (row CR-38 of
// docs/contract/claims/couldnt-read.md).
func LinkGoCounted(files map[string]facts.Facts, sources map[string][]byte, shapeDepth int, scopeRules ...testscope.Rules) ([]contract.Node, []contract.Edge, []contract.Diagnostic, map[string]map[string]int) {
	l := runGoDepth(files, sources, selectedRules(scopeRules), nil, shapeDepth)
	unread := map[string]map[string]int{}
	for file, forms := range l.unread {
		counts := map[string]int{}
		for form, n := range forms {
			if n > 0 {
				counts[form] = n
			}
		}
		if len(counts) > 0 {
			unread[file] = counts
		}
	}
	return l.nodes, l.edges, l.diagnostics, unread
}

func linkGo(files map[string]facts.Facts, sources map[string][]byte, rules testscope.Rules, tag func(string)) ([]contract.Node, []contract.Edge, []contract.Diagnostic) {
	l := runGo(files, sources, rules, tag)
	return l.nodes, l.edges, l.diagnostics
}

// runGo links the Go route payload and keeps the linker, so tests can read
// the composition graph (goRouteGraph).
func runGo(files map[string]facts.Facts, sources map[string][]byte, rules testscope.Rules, tag func(string)) *goLinker {
	return runGoDepth(files, sources, rules, tag, routeshape.DefaultDepth)
}

func runGoDepth(files map[string]facts.Facts, sources map[string][]byte, rules testscope.Rules, tag func(string), shapeDepth int) *goLinker {
	if tag == nil {
		tag = func(string) {}
	}
	l := &goLinker{
		files: files, sources: sources, rules: rules, tag: tag,
		modules: golang.ModulePaths(sources), moduleOf: map[string]goModuleInfo{},
		packages: map[string]golang.GoPackage{}, imports: map[string]map[string]goImport{},
		records: map[string][]*goRecord{}, byDecl: map[string]map[string]*goRecord{},
		objects: map[string]*goObject{}, memo: map[string]goValue{}, active: map[string]bool{},
		factories: map[string]goValue{}, building: map[string]bool{}, routerMethods: map[string]bool{},
		own: map[string][]string{}, names: map[string]goName{},
		unread: map[string]map[string]int{}, counted: map[string]bool{},
		wrappedAt: map[string]string{}, handlers: map[string]bool{}, mountRefused: map[string]bool{}, composed: map[*facts.GoExpr]bool{}, refused: map[int]bool{},
		served: map[string][]goServe{}, servedKeys: map[string]bool{}, serveBlocked: map[string]bool{}, muxOpaque: map[string]bool{}, muxExtra: map[string][]goMuxExtra{}, muxOpaqueAt: map[string][]goAt{}, gates: map[string]*goGate{}, gateRefused: map[string]bool{}, gatePassed: map[string]bool{}, refusedWhy: map[string]string{}, hops: map[string]*mountHop{}, handleHops: map[*mountHop]goHandleHop{}, trees: map[string]map[string]bool{}, direct: map[string]bool{},
		plainImports: map[string]map[string]goImport{}, shapeDepth: shapeDepth,
	}
	var recognized []string
	for _, file := range facts.Paths(files) {
		g := files[file].Go
		if g == nil || g.Routes == nil || g.Rejected || g.LoneCR {
			continue
		}
		if l.safeIndex(file) {
			recognized = append(recognized, file)
		}
	}
	l.safeOapi(recognized)
	for _, file := range recognized {
		l.safeFile(file)
	}
	// Serving positions (GR-C6) read every file's objects and bindings;
	// the StripPrefix calls no composition or serving position read are
	// then wrapped routers (GR-C4).
	for _, file := range recognized {
		l.safeStep("serving", file, func(rec *goRecord) { l.serving(rec) })
	}
	for _, file := range recognized {
		l.safeStep("strip", file, func(rec *goRecord) { l.stripPrefixes(rec) })
	}
	// Escape positions (GR-C4): an object some use of which no
	// row reads is no root.
	for _, file := range recognized {
		l.safeStep("escape", file, func(rec *goRecord) { l.escapeWalk(rec) })
	}
	l.nodes, l.edges = l.emit()
	for _, file := range facts.Paths(files) {
		l.diagnostics = append(l.diagnostics, goUnreadDiagnostic(file, l.unread[file])...)
	}
	return l
}

// safeIndex recognises and indexes one file (GR-K1); a failure leaves the
// file unread with a warning, never stopping the other files.
func (l *goLinker) safeIndex(file string) (ok bool) {
	defer func() {
		if v := recover(); v != nil {
			delete(l.records, file)
			delete(l.byDecl, file)
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go route recognition failed: %v", file, v)})
			ok = false
		}
	}()
	if goTestFail != nil {
		goTestFail("index", file)
	}
	if !l.recognize(file) {
		return false
	}
	l.index(file)
	return true
}

// safeOapi finds the generated servers and their instances (GR-G1); a
// failure leaves every generated file to the other rows with a warning.
func (l *goLinker) safeOapi(files []string) {
	defer func() {
		if v := recover(); v != nil {
			l.oapi = map[string]*goOapiServer{}
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("Go generated-server recognition failed: %v", v)})
		}
	}()
	l.oapiRecognize(files)
}

// safeFile reads one file; a failure drops that file's registrations and
// becomes a warning, never stopping the other files.
func (l *goLinker) safeFile(file string) {
	n, c, b := len(l.registrations), len(l.compositions), len(l.bindings)
	defer func() {
		if v := recover(); v != nil {
			l.registrations = l.registrations[:n]
			l.compositions = l.compositions[:c]
			l.bindings = l.bindings[:b]
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go route linking failed: %v", file, v)})
		}
	}()
	if goTestFail != nil {
		goTestFail("file", file)
	}
	l.file(file)
}

// safeStep runs one pass over a file's records; a failure becomes a
// warning, never stopping the other files.
func (l *goLinker) safeStep(step, file string, f func(rec *goRecord)) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Severity: "warning", Code: "go_routes_failed", Message: fmt.Sprintf("%s: Go route linking (%s) failed: %v", file, step, v)})
		}
	}()
	if goTestFail != nil {
		goTestFail(step, file)
	}
	for _, rec := range l.records[file] {
		f(rec)
	}
}

// count records one D form at a site, once.
func (l *goLinker) count(file, site, form string) {
	if l.counted[site+"\x00"+form] {
		return
	}
	l.counted[site+"\x00"+form] = true
	l.tag("D")
	l.tag("D/" + form)
	if l.unread[file] == nil {
		l.unread[file] = map[string]int{}
	}
	l.unread[file][form]++
}

func (l *goLinker) diagnostic(file, code, expression string, span contract.Span) {
	severity := "info"
	switch code {
	case "route_dynamic_pattern", "route_dynamic_methods", "route_invalid_pattern":
		severity = "warning"
	}
	l.diagnostics = append(l.diagnostics, contract.Diagnostic{Type: "diagnostic", File: file, Code: code, Severity: severity, Message: fmt.Sprintf("%s at %d:%d-%d:%d", expression, span.StartLine, span.StartColumn, span.EndLine, span.EndColumn)})
}

// goUnreadDiagnostic reports a file's D forms: one info diagnostic with
// the total and the count per form, sorted by form.
func goUnreadDiagnostic(file string, unread map[string]int) []contract.Diagnostic {
	total := 0
	var forms []string
	for form, n := range unread {
		total += n
		forms = append(forms, form)
	}
	if total == 0 {
		return nil
	}
	sort.Strings(forms)
	parts := make([]string, len(forms))
	for i, form := range forms {
		parts[i] = fmt.Sprintf("%s=%d", form, unread[form])
	}
	return []contract.Diagnostic{{Type: "diagnostic", File: file, Severity: "info", Code: "go_routes_unread", Message: fmt.Sprintf("%d Go router forms are outside the route claim contract and claim nothing (%s)", total, strings.Join(parts, ", "))}}
}

// goPackage is golang.Package, cached per directory.
func (l *goLinker) goPackage(dir string) golang.GoPackage {
	if p, ok := l.packages[dir]; ok {
		return p
	}
	p := golang.Package(dir, l.files)
	l.packages[dir] = p
	return p
}

// ownFiles are the files whose package-level declarations file sees
// without an import (golang.OwnPackageFiles).
func (l *goLinker) ownFiles(file string) []string {
	if files, ok := l.own[file]; ok {
		return files
	}
	files := golang.OwnPackageFiles(file, l.goPackage(path.Dir(file)), l.files)
	l.own[file] = files
	return files
}

// recognize applies GR-K1 to a gated file: it must belong to a scanned
// module. It binds the file's imports and counts dot imports and older chi
// modules.
func (l *goLinker) recognize(file string) bool {
	base, module := golang.ModuleAt(file, l.modules)
	if module == "" || golang.CheckImportPath(module) != nil {
		return false
	}
	out := map[string]goImport{}
	for _, imp := range l.files[file].Imports {
		if golang.CheckImportPath(imp.Module) != nil {
			continue
		}
		site := fmt.Sprintf("%s#import:%d:%d", file, imp.Span.StartLine, imp.Span.StartColumn)
		if dir, ok := golang.ModuleImportDir(base, module, imp.Module); ok {
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
			continue
		}
		known, router := goRouterImports[imp.Module]
		if router && l.shadowed(imp.Module) {
			router = false
		}
		older := imp.Module == "github.com/go-chi/chi" || strings.HasPrefix(imp.Module, "github.com/go-chi/chi/") && goMajor(strings.TrimPrefix(imp.Module, "github.com/go-chi/chi/")) && imp.Module != "github.com/go-chi/chi/v5"
		for alias := range imp.Bindings {
			if alias == "." {
				if router {
					l.count(file, site, goDotImport)
				}
				continue
			}
			if older {
				l.count(file, site, goUnsupportedModule)
			}
			if alias == "_" {
				continue
			}
			b := goImport{path: imp.Module}
			if router {
				b.framework = known.framework
				if !imp.ExplicitAlias {
					alias = known.name
				}
			}
			out[alias] = b
			if !imp.ExplicitAlias && !router {
				// The package clause of an external package is unknown;
				// a /vN path usually names the element before.
				parts := strings.Split(imp.Module, "/")
				if n := len(parts); n > 1 && goMajor(parts[n-1]) {
					out[parts[n-2]] = b
				}
			}
		}
	}
	l.imports[file] = out
	l.tag("GR-K1")
	return true
}

// goMajor reports a module major version suffix element (v2, v5, ...).
func goMajor(elem string) bool {
	if len(elem) < 2 || elem[0] != 'v' {
		return false
	}
	for _, r := range elem[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// shadowed reports that a scanned module's path is the import path or a
// prefix of it: the module's own files then are the package.
func (l *goLinker) shadowed(importPath string) bool {
	for _, m := range l.modules {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			return true
		}
	}
	return false
}

// index lists a recognised file's records and the declarations by name.
func (l *goLinker) index(file string) {
	routes := l.files[file].Go.Routes
	var list []*goRecord
	for i := range routes.Funcs {
		fn := &routes.Funcs[i]
		r := &goRecord{file: file, index: i, key: fmt.Sprintf("%s#%d", file, i), fn: fn}
		if fn.Parent > 0 && fn.Parent <= len(list) {
			r.parent = list[fn.Parent-1]
			r.decl = r.parent.decl
		} else if fn.Name != "" {
			r.decl = r
		}
		list = append(list, r)
		if fn.Name != "" && fn.Parent == 0 {
			name := fn.Name
			if fn.Receiver != nil {
				name = goBaseType(fn.Receiver.Type) + "." + name
			}
			if fn.Receiver != nil {
				for _, res := range fn.Results {
					if fw, _ := l.routerType(file, &res.Type); fw != "" {
						l.routerMethods[fn.Name] = true
					}
				}
			}
			if l.byDecl[file] == nil {
				l.byDecl[file] = map[string]*goRecord{}
			}
			l.byDecl[file][name] = r
		}
	}
	l.records[file] = list
}

// goBaseType is the type name of a receiver type T, *T or T[P].
func goBaseType(t facts.GoExpr) string {
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = *t.Object
	}
	if t.Kind == facts.GoIndex && t.Object != nil {
		t = *t.Object
	}
	if t.Kind == facts.GoIdentifier {
		return t.Name
	}
	return ""
}

// module reads the go.mod governing file (GR-V1, GR-V2), cached by module
// directory.
func (l *goLinker) module(file string) goModuleInfo {
	base, _ := golang.ModuleAt(file, l.modules)
	if info, ok := l.moduleOf[base]; ok {
		return info
	}
	gomod := "go.mod"
	if base != "." {
		gomod = base + "/go.mod"
	}
	info := readGoMod(string(l.sources[gomod]))
	// A //go:debug line of a main package of the module applies to the
	// program it builds (GR-V2).
	for p, f := range l.files {
		if f.Go == nil || f.Package != "main" {
			continue
		}
		if b, _ := golang.ModuleAt(p, l.modules); b != base {
			continue
		}
		for _, line := range f.Go.Debug {
			if goLegacySetting(strings.TrimSpace(strings.TrimPrefix(line, "//go:debug"))) {
				info.legacy = true
			}
		}
	}
	l.moduleOf[base] = info
	return info
}

// readGoMod reads a go.mod's go directive, godebug settings and the
// required chi and gin versions.
func readGoMod(text string) goModuleInfo {
	info := goModuleInfo{versions: map[string]string{}}
	goVersion := ""
	block := ""
	for _, raw := range strings.Split(text, "\n") {
		line := raw
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		words := strings.Fields(line)
		if len(words) == 0 {
			continue
		}
		if block != "" {
			if words[0] == ")" {
				block = ""
				continue
			}
			words = append([]string{block}, words...)
		} else if len(words) == 2 && words[1] == "(" {
			block = words[0]
			continue
		}
		switch words[0] {
		case "go":
			if len(words) >= 2 {
				goVersion = words[1]
			}
		case "godebug":
			for _, w := range words[1:] {
				if goLegacySetting(w) {
					info.legacy = true
				}
			}
		case "require":
			if len(words) >= 3 {
				for fw, p := range goModulePaths {
					if words[1] == p {
						info.versions[fw] = words[2]
					}
				}
			}
		case "replace":
			for fw, p := range goModulePaths {
				if len(words) >= 2 && words[1] == p {
					info.versions[fw] = "replaced"
				}
			}
		}
	}
	if !goAtLeast122(goVersion) {
		info.legacy = true
	}
	return info
}

// goLegacySetting reports a GODEBUG setting list that selects the Go
// 1.21 mux: httpmuxgo121=1, or default=go1.N with N < 22.
func goLegacySetting(list string) bool {
	for _, s := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		k, v, _ := strings.Cut(s, "=")
		switch k {
		case "httpmuxgo121":
			if v == "1" {
				return true
			}
		case "default":
			if strings.HasPrefix(v, "go") && !goAtLeast122(strings.TrimPrefix(v, "go")) {
				return true
			}
		}
	}
	return false
}

// goAtLeast122 reports a Go version 1.22 or newer ("" is the go command's
// default for a go.mod without a go line, 1.16).
func goAtLeast122(v string) bool {
	major, rest, _ := strings.Cut(v, ".")
	if !goDigits(major) {
		return false
	}
	if major != "1" {
		return major != "0"
	}
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	minor := rest[:end]
	return goDigits(minor) && (len(minor) > 2 || len(minor) == 2 && minor >= "22")
}

func goDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// assumptions are the GR-V rows a route of framework states.
func (l *goLinker) assumptions(file, framework string) []string {
	if framework == goNetHTTP {
		return []string{"GR-V2"}
	}
	version := l.module(file).versions[framework]
	for _, v := range goVerified[framework] {
		if v == version {
			return nil
		}
	}
	return []string{"GR-V1"}
}

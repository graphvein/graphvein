package routes

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/golang"
)

// Lexical reading of the Go router payload (row GR-K3): which declaration
// a name means at a position, which bindings of it may reach the use, and
// what router objects a value may hold.

// goPos is a position in a file (1-based line, 0-based byte column).
type goPos struct{ line, col int }

func goStart(s contract.Span) goPos { return goPos{s.StartLine, s.StartColumn} }

// before reports that span ends at or before p (a declared name's scope
// starts at the end of its binding's span).
func (p goPos) after(s contract.Span) bool {
	return s.EndLine < p.line || s.EndLine == p.line && s.EndColumn <= p.col
}

// goAt is where an expression is read: a record, its block and the
// position of the enclosing statement.
type goAt struct {
	rec   *goRecord
	block int
	pos   goPos
}

// goName is what a name means at a position.
type goName struct {
	// kind is local, param, receiver, result, package (a package-level var
	// or const of the module), func (a package-level function), import, or
	// "" (unknown: a predeclared or unread name).
	kind string
	rec  *goRecord
	// decl is the declaring binding's index (local) or the parameter's
	// index (param).
	decl  int
	block int
	// file and pkgVar/pkgConst locate a package-level declaration.
	file     string
	pkgVar   *facts.GoVar
	pkgConst *facts.GoConst
	imp      goImport
}

var goDeclKinds = map[string]bool{facts.GoBindVar: true, facts.GoBindDefine: true, facts.GoBindConst: true, facts.GoBindRange: true, facts.GoBindTypeSwitch: true}

// ancestor reports that block a encloses block b (or is b) in fn.
func ancestor(fn *facts.GoRouteFunc, a, b int) bool {
	for {
		if a == b {
			return true
		}
		if b == 0 || b >= len(fn.Blocks) {
			return false
		}
		b = fn.Blocks[b].Parent
	}
}

func hasName(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// lookup resolves name at at: the innermost block declaring it before the
// position, the record's parameters, the enclosing records of a literal,
// then the package scope and the file's imports.
func (l *goLinker) lookup(at goAt, name string) goName {
	if name == "" || name == "_" {
		return goName{}
	}
	for rec, block, pos := at.rec, at.block, at.pos; rec != nil; {
		fn := rec.fn
		for b := block; ; {
			for j, bd := range fn.Bindings {
				if bd.Block == b && goDeclKinds[bd.Kind] && hasName(bd.Names, name) >= 0 && pos.after(bd.Span) {
					return goName{kind: "local", rec: rec, decl: j, block: b}
				}
			}
			if b == 0 || b >= len(fn.Blocks) {
				break
			}
			b = fn.Blocks[b].Parent
		}
		if fn.Receiver != nil && fn.Receiver.Name == name {
			return goName{kind: "receiver", rec: rec}
		}
		for i, p := range fn.Params {
			if p.Name == name {
				return goName{kind: "param", rec: rec, decl: i}
			}
		}
		for i, p := range fn.Results {
			if p.Name == name {
				return goName{kind: "result", rec: rec, decl: i}
			}
		}
		if rec.parent == nil {
			break
		}
		block, pos = fn.Block, goStart(fn.Span)
		rec = rec.parent
	}
	return l.packageName(at.rec.file, name)
}

// packageName resolves a name in the package scope of file: a
// package-level const, var or function of the package, else an import.
func (l *goLinker) packageName(file, name string) goName {
	key := file + "\x00" + name
	if n, ok := l.names[key]; ok {
		return n
	}
	n := goName{}
	for _, p := range l.ownFiles(file) {
		if n = l.declared(p, name); n.kind != "" {
			break
		}
	}
	if n.kind == "" {
		if imp, ok := l.imports[file][name]; ok {
			n = goName{kind: "import", imp: imp, file: file}
		}
	}
	l.names[key] = n
	return n
}

// declared finds a package-level declaration name in file p.
func (l *goLinker) declared(p, name string) goName {
	g := l.files[p].Go
	if g == nil {
		return goName{}
	}
	consts := g.Consts
	if g.Routes != nil {
		consts = append(append([]facts.GoConst(nil), consts...), g.Routes.Consts...)
	}
	for i := range consts {
		if consts[i].Name == name {
			return goName{kind: "package", file: p, pkgConst: &consts[i]}
		}
	}
	var vars []facts.GoVar
	vars = append(vars, g.Vars...)
	if g.Routes != nil {
		vars = append(vars, g.Routes.Vars...)
	}
	for i := range vars {
		if hasName(vars[i].Names, name) >= 0 {
			return goName{kind: "package", file: p, pkgVar: &vars[i]}
		}
	}
	for _, n := range l.files[p].Nodes {
		if n.Kind == "function" && n.QualifiedName == name {
			return goName{kind: "func", file: p}
		}
	}
	return goName{}
}

// writes lists the bindings of the variable n declares (n.kind local or
// param): its declaration, a := in the declaring block that redeclares it,
// and every = or range = that assigns it. Assignments in function literals
// are not read (a known gap).
func (l *goLinker) writes(n goName, name string) []int {
	fn := n.rec.fn
	var out []int
	if n.kind == "local" {
		out = append(out, n.decl)
	}
	for j, bd := range fn.Bindings {
		if n.kind == "local" && j == n.decl || hasName(bd.Names, name) < 0 {
			continue
		}
		switch bd.Kind {
		case facts.GoBindAssign, facts.GoBindRangeAssign:
		case facts.GoBindDefine:
			if n.kind != "local" || bd.Block != n.block {
				continue
			}
		default:
			continue
		}
		m := l.lookup(goAt{n.rec, bd.Block, goStart(bd.Span)}, name)
		if m.kind == n.kind && m.rec == n.rec && m.decl == n.decl {
			out = append(out, j)
		}
	}
	return out
}

// reaching are the bindings among writes that may hold the variable's
// value at at: the last one before the use that encloses it (it runs on
// every path to the use), the ones after it before the use in other
// blocks, and any inside a for block enclosing the use (the next
// iteration). dominated reports that one write runs on every path.
func reaching(fn *facts.GoRouteFunc, writes []int, at goAt) (out []int, dominated bool) {
	sorted := append([]int(nil), writes...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := fn.Bindings[sorted[i]].Span, fn.Bindings[sorted[j]].Span
		if a.StartLine != b.StartLine {
			return a.StartLine < b.StartLine
		}
		return a.StartColumn < b.StartColumn
	})
	dom := -1
	for i, j := range sorted {
		bd := fn.Bindings[j]
		if at.pos.after(bd.Span) && ancestor(fn, bd.Block, at.block) {
			dom = i
		}
	}
	seen := map[int]bool{}
	for i, j := range sorted {
		bd := fn.Bindings[j]
		loop := false
		for b := at.block; b > 0 && b < len(fn.Blocks); b = fn.Blocks[b].Parent {
			if fn.Blocks[b].Kind == facts.GoBlockFor && ancestor(fn, b, bd.Block) {
				loop = true
				break
			}
		}
		if (i >= dom && at.pos.after(bd.Span)) || loop {
			if !seen[j] {
				seen[j] = true
				out = append(out, j)
			}
		}
	}
	return out, dom >= 0
}

// goValue is what a value may hold.
type goValue struct {
	// objects are the router objects it may hold, sorted.
	objects []string
	// unproven reports that it may hold something no row proves a router.
	unproven bool
	// evidence reports a sign that the value is a router (a router-typed
	// declaration or a reaching proven object): only then does a refusal
	// count as a D form.
	evidence bool
	// form is the D form a refusal counts (receiver_unproven by default).
	form string
	// zero reports an addressable zero-value ServeMux variable (GR-K4):
	// readable as a receiver, through & and ServeHTTP, never copied.
	zero bool
	// plain reports a handler proven to hold no router: an
	// http.HandlerFunc conversion of a function literal that uses no
	// router object.
	plain bool
	// alternative reports objects that a plain handler may replace (a
	// binding of one reaches the use too): a composition through them is
	// conditional (GR-C5).
	alternative bool
	// methods are the request methods a path-preserving wrapper on the
	// way answers itself: routes of those methods (and
	// ANY routes) get no path through it.
	methods []string
}

func (v goValue) proven() bool { return len(v.objects) > 0 && !v.unproven }

func goUnproven() goValue { return goValue{unproven: true} }

func goObjects(ids ...string) goValue { return goValue{objects: ids} }

// merge is the value of either of a and b.
func merge(a, b goValue) goValue {
	out := goValue{unproven: a.unproven || b.unproven, evidence: a.evidence || b.evidence || len(a.objects)+len(b.objects) > 0, zero: a.zero && b.zero}
	out.plain = a.plain && b.plain
	out.methods = goUnion(a.methods, b.methods)
	out.alternative = a.alternative || b.alternative || a.plain && len(b.objects) > 0 || b.plain && len(a.objects) > 0
	if a.form != "" {
		out.form = a.form
	} else {
		out.form = b.form
	}
	seen := map[string]bool{}
	for _, id := range append(append([]string(nil), a.objects...), b.objects...) {
		if !seen[id] {
			seen[id] = true
			out.objects = append(out.objects, id)
		}
	}
	sort.Strings(out.objects)
	return out
}

// copied is a value read as a plain value: a zero-value ServeMux variable
// read so is a copy, no proven object.
func copied(v goValue) goValue {
	if v.zero {
		return goValue{unproven: true, evidence: true}
	}
	return v
}

// framework is the single framework of a proven value's objects ("" for
// none or several).
func (l *goLinker) framework(v goValue) string {
	fw := ""
	for _, id := range v.objects {
		o := l.objects[id]
		if fw != "" && o.framework != fw {
			return ""
		}
		fw = o.framework
	}
	return fw
}

// object registers an object once.
func (l *goLinker) object(o goObject) string {
	if _, ok := l.objects[o.id]; !ok {
		l.objects[o.id] = &o
	}
	return o.id
}

// routerType is the framework of a router type expression read in file
// (the parameter types of GR-O4); value reports a ServeMux value type.
func (l *goLinker) routerType(file string, t *facts.GoExpr) (framework string, value bool) {
	if t == nil {
		return "", false
	}
	pointer := false
	if t.Kind == facts.GoPointer && t.Object != nil {
		pointer, t = true, t.Object
	}
	if t.Kind != facts.GoSelector || t.Object == nil || t.Object.Kind != facts.GoIdentifier {
		return "", false
	}
	imp, ok := l.imports[file][t.Object.Name]
	if !ok {
		return "", false
	}
	switch {
	case imp.framework == goNetHTTP && t.Name == "ServeMux":
		return goNetHTTP, !pointer
	case imp.framework == goChi && (t.Name == "Router" && !pointer || t.Name == "Mux" && pointer):
		return goChi, false
	case imp.framework == goGin && (pointer && (t.Name == "Engine" || t.Name == "RouterGroup") || !pointer && (t.Name == "IRouter" || t.Name == "IRoutes")):
		return goGin, false
	}
	return "", false
}

// isImport reports that e is a name bound to an import of framework (or
// to any import for framework "*") at at.
func (l *goLinker) importOf(at goAt, e *facts.GoExpr) (goImport, bool) {
	if e == nil || e.Kind != facts.GoIdentifier {
		return goImport{}, false
	}
	n := l.lookup(at, e.Name)
	if n.kind != "import" {
		return goImport{}, false
	}
	return n.imp, true
}

// name is the value a name holds at at.
func (l *goLinker) name(at goAt, name string) goValue {
	n := l.lookup(at, name)
	switch n.kind {
	case "local":
		if v, ok := l.oapiRouterValue(n, name); ok {
			return v
		}
		bd := n.rec.fn.Bindings[n.decl]
		if bd.Kind == facts.GoBindConst {
			return goValue{unproven: true}
		}
		writes := l.writes(n, name)
		reach, _ := reaching(n.rec.fn, writes, at)
		if len(reach) == 0 {
			return goUnproven()
		}
		var out goValue
		for i, j := range reach {
			v := l.bindingValue(n.rec, j, name)
			if i == 0 {
				out = v
			} else {
				out = merge(out, v)
			}
		}
		return out
	case "param":
		p := n.rec.fn.Params[n.decl]
		fw, value := l.routerType(n.rec.file, &p.Type)
		if fw == "" || value || p.Variadic {
			return goUnproven()
		}
		if len(l.writes(n, name)) > 0 {
			return goValue{unproven: true, evidence: true, form: goParamRebound}
		}
		if n.rec.parent != nil && !n.rec.callback {
			// A function literal's parameter holds an object only as
			// the callback of a GR-O1 call on proven objects (GR-O4
			// binds parameters of module functions and methods).
			return goValue{unproven: true, evidence: true}
		}
		return goObjects(l.object(goObject{id: fmt.Sprintf("param:%s#%d", n.rec.key, n.decl), framework: fw, kind: "param", file: n.rec.file, span: n.rec.fn.Span}))
	case "package":
		if n.pkgVar != nil {
			fw, _ := l.routerType(n.file, n.pkgVar.Type)
			ctor := false
			for _, v := range n.pkgVar.Values {
				if c := l.constructor(goAt{rec: &goRecord{file: n.file}}, &v); c != "" {
					ctor = true
				}
			}
			return goValue{unproven: true, evidence: fw != "" || ctor}
		}
	}
	return goUnproven()
}

// bindingValue is what binding j of rec gives name.
func (l *goLinker) bindingValue(rec *goRecord, j int, name string) goValue {
	bd := rec.fn.Bindings[j]
	key := fmt.Sprintf("%s/b%d/%s", rec.key, j, name)
	if v, ok := l.memo[key]; ok {
		return v
	}
	if l.active[key] {
		return goUnproven()
	}
	l.active[key] = true
	defer delete(l.active, key)
	at := goAt{rec, bd.Block, goStart(bd.Span)}
	i := hasName(bd.Names, name)
	var v goValue
	switch {
	case bd.Kind == facts.GoBindRange || bd.Kind == facts.GoBindRangeAssign || bd.Kind == facts.GoBindTypeSwitch:
		v = goUnproven()
	case len(bd.Values) == 0:
		// var x T: the zero value. A ServeMux value is a usable mux
		// (GR-K4); every other router type's zero value is nil.
		fw, value := l.routerType(rec.file, bd.Type)
		if fw == goNetHTTP && value && len(bd.Names) >= 1 {
			l.tag("GR-K4")
			l.tag("GR-K4/`var m http.ServeMux`")
			v = goObjects(l.object(goObject{id: key, framework: goNetHTTP, kind: "zero", file: rec.file, span: bd.Span}))
			v.zero = true
		} else {
			v = goValue{unproven: true, evidence: fw != ""}
		}
	case len(bd.Values) == len(bd.Names):
		e := bd.Values[i]
		if l.zeroComposite(at, &e) {
			l.tag("GR-K4")
			l.tag("GR-K4/`m := http.ServeMux{}`")
			v = goObjects(l.object(goObject{id: key, framework: goNetHTTP, kind: "zero", file: rec.file, span: bd.Span}))
			v.zero = true
		} else {
			v = copied(l.eval(at, &e, key))
		}
	case len(bd.Values) == 1 && bd.Values[0].Kind == facts.GoCall:
		v = l.factoryCall(at, &bd.Values[0], i, len(bd.Names), key)
	default:
		v = goUnproven()
	}
	if v.proven() && len(bd.Values) > 0 {
		l.tag("GR-K3")
		switch {
		case bd.Kind == facts.GoBindDefine:
			l.tag("GR-K3/`:=`")
		case bd.Kind == facts.GoBindAssign:
			l.tag("GR-K3/`=`")
		case bd.Kind == facts.GoBindVar && bd.Type != nil:
			l.tag("GR-K3/`var x T = e`")
		case bd.Kind == facts.GoBindVar:
			l.tag("GR-K3/`var x = e`")
		}
	}
	l.memo[key] = v
	return v
}

// zeroComposite reports http.ServeMux{} (no elements).
func (l *goLinker) zeroComposite(at goAt, e *facts.GoExpr) bool {
	if e.Kind != facts.GoComposite || len(e.Elements) > 0 || e.Type == nil {
		return false
	}
	fw, value := l.routerType(at.rec.file, e.Type)
	return fw == goNetHTTP && value && l.typeImported(at, e.Type)
}

// typeImported reports that a qualified type's package name is the import
// at at (not shadowed by a local).
func (l *goLinker) typeImported(at goAt, t *facts.GoExpr) bool {
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = t.Object
	}
	_, ok := l.importOf(at, t.Object)
	return ok
}

// constructor returns the framework of a constructor call (GR-K2) or
// new(http.ServeMux) (GR-K4), "" for anything else.
func (l *goLinker) constructor(at goAt, e *facts.GoExpr) string {
	if e.Kind != facts.GoCall || e.Callee == nil {
		return ""
	}
	c := e.Callee
	if c.Kind == facts.GoIdentifier && c.Name == "new" && len(e.Args) == 1 && at.rec.fn != nil && l.lookup(at, "new").kind == "" {
		if fw, value := l.routerType(at.rec.file, &e.Args[0]); fw == goNetHTTP && value && l.typeImported(at, &e.Args[0]) {
			return "new"
		}
		return ""
	}
	if c.Kind != facts.GoSelector || len(e.Args) != 0 {
		return ""
	}
	var imp goImport
	var ok bool
	if at.rec.fn != nil {
		imp, ok = l.importOf(at, c.Object)
	} else if c.Object != nil && c.Object.Kind == facts.GoIdentifier {
		imp, ok = l.imports[at.rec.file][c.Object.Name]
	}
	if !ok {
		return ""
	}
	switch {
	case imp.framework == goNetHTTP && c.Name == "NewServeMux",
		imp.framework == goChi && (c.Name == "NewRouter" || c.Name == "NewMux"),
		imp.framework == goGin && (c.Name == "New" || c.Name == "Default"):
		return imp.framework
	}
	return ""
}

// eval is the value of e read at at; site names the expression's syntax
// position (object identities).
func (l *goLinker) eval(at goAt, e *facts.GoExpr, site string) goValue {
	switch e.Kind {
	case facts.GoIdentifier:
		return l.name(at, e.Name)
	case facts.GoUnary:
		if e.Value != "&" || e.Object == nil {
			return goUnproven()
		}
		if e.Object.Kind == facts.GoComposite && l.zeroComposite(at, e.Object) {
			l.tag("GR-K4")
			l.tag("GR-K4/`&http.ServeMux{}`")
			return goObjects(l.object(goObject{id: site, framework: goNetHTTP, kind: "zero", file: at.rec.file}))
		}
		v := l.eval(at, e.Object, site+"/&")
		v.zero = false
		return v
	case facts.GoSelector:
		if e.Object == nil {
			return goUnproven()
		}
		if imp, ok := l.importOf(at, e.Object); ok {
			if imp.framework == goNetHTTP && e.Name == "DefaultServeMux" {
				l.tag("GR-K2")
				l.tag("GR-K2/`http.DefaultServeMux`")
				return goObjects(l.defaultMux(at.rec.file))
			}
			return goUnproven()
		}
		if e.Name == "ServeHTTP" {
			v := l.eval(at, e.Object, site+"/o")
			if v.proven() {
				l.tag("GR-O6")
				l.tag("GR-O6/`x.ServeHTTP`")
				v.zero = false
			}
			return v
		}
		return l.field(at, e)
	case facts.GoCall:
		return l.call(at, e, site)
	}
	return goUnproven()
}

// defaultMux is the DefaultServeMux object of file's module (one per
// program; the module is the closest reading of a program).
func (l *goLinker) defaultMux(file string) string {
	base, _ := golang.ModuleAt(file, l.modules)
	return l.object(goObject{id: "default:" + base, framework: goNetHTTP, kind: "default", file: file})
}

// field is x.f with x not an import: a struct field or a method value,
// no proven object (GR-K3). It is evidence when the field's declared type
// is a router type.
func (l *goLinker) field(at goAt, e *facts.GoExpr) goValue {
	t, file := l.typeOf(at, e.Object)
	if t == nil {
		return goUnproven()
	}
	files, name := l.typeFiles(file, t)
	for _, p := range files {
		g := l.files[p].Go
		if g == nil {
			continue
		}
		for _, s := range g.Structs {
			if s.Name != name {
				continue
			}
			for _, f := range s.Fields {
				if hasName(f.Names, e.Name) >= 0 {
					fw, _ := l.routerType(p, &f.Type)
					return goValue{unproven: true, evidence: fw != ""}
				}
			}
		}
	}
	return goUnproven()
}

// call is the value of a call expression: a constructor, a conversion,
// a child of a router object, or a factory.
func (l *goLinker) call(at goAt, e *facts.GoExpr, site string) goValue {
	site = l.canonical(at, e, site)
	if v, ok := l.memo[site]; ok {
		return v
	}
	v := l.callValue(at, e, site)
	l.memo[site] = v
	return v
}

// canonical is the site of a call expression that is also listed as a
// call of its record (a binding value, a return value or an argument
// that is itself a listed router or plain call): the
// listed call's site, so one call is one syntax site however it is
// reached. The listed call is the nearest one at or after the reading
// position whose tree equals e.
func (l *goLinker) canonical(at goAt, e *facts.GoExpr, site string) string {
	if at.rec.fn == nil {
		return site
	}
	best := -1
	for k := range at.rec.fn.Calls {
		c := &at.rec.fn.Calls[k]
		if goBefore(c.Span, at.pos) {
			continue
		}
		if best >= 0 && !goBefore(c.Span, goStart(at.rec.fn.Calls[best].Span)) {
			continue
		}
		if reflect.DeepEqual(&c.Call, e) {
			best = k
		}
	}
	if best < 0 {
		return site
	}
	return fmt.Sprintf("%s/c%d", at.rec.key, best)
}

// goBefore reports that span starts before p.
func goBefore(s contract.Span, p goPos) bool {
	return s.StartLine < p.line || s.StartLine == p.line && s.StartColumn < p.col
}

func (l *goLinker) callValue(at goAt, e *facts.GoExpr, site string) goValue {
	if fw := l.constructor(at, e); fw != "" {
		if fw == "new" {
			l.tag("GR-K4")
			l.tag("GR-K4/`new(http.ServeMux)`")
			return goObjects(l.object(goObject{id: site, framework: goNetHTTP, kind: "zero", file: at.rec.file}))
		}
		l.tag("GR-K2")
		l.tag("GR-K2/`" + goCtorText(fw, e.Callee.Name) + "`")
		l.recordCtor(site, fw, at, e)
		return goObjects(l.object(goObject{id: site, framework: fw, kind: "ctor", file: at.rec.file}))
	}
	c := e.Callee
	if c == nil {
		return goUnproven()
	}
	if v, ok := l.oapiCall(at, e); ok {
		return v
	}
	if v, ok := l.wrapperValue(at, e, site); ok {
		return v
	}
	if c.Kind == facts.GoSelector && c.Object != nil {
		if imp, ok := l.importOf(at, c.Object); ok {
			if imp.framework == goNetHTTP && c.Name == "HandlerFunc" && len(e.Args) == 1 && e.Args[0].Kind == facts.GoFuncLit {
				// A handler function literal that uses no router object
				// is no router (a binding of one does not block
				// the compositions of the other bindings, GR-C3).
				for _, name := range l.literalNames(at, &e.Args[0]) {
					id := facts.GoExpr{Kind: facts.GoIdentifier, Name: name}
					if len(l.eval(at, &id, site+"/u"+name).objects) > 0 {
						return goUnproven()
					}
				}
				return goValue{plain: true}
			}
			if imp.framework == goNetHTTP && c.Name == "Handler" && len(e.Args) == 1 {
				v := copied(l.eval(at, &e.Args[0], site+"/a0"))
				if v.proven() {
					l.tag("GR-O6")
					l.tag("GR-O6/`http.Handler(x)`")
				}
				return v
			}
			if imp.files != nil {
				return l.factory(at, e, l.funcRecord(imp.files, c.Name), 0, 1, site)
			}
			return goUnproven()
		}
		recv := l.eval(at, c.Object, site+"/o")
		if recv.proven() {
			return l.child(at, e, recv, site)
		}
		if c.Object.Kind == facts.GoCall {
			return l.chainCall(at, e, 0, 1, site)
		}
		if rec := l.methodRecord(at, c.Object, c.Name); rec != nil {
			return l.factory(at, e, rec, 0, 1, site)
		}
		return goUnproven()
	}
	return l.factoryCall(at, e, 0, 1, site)
}

// chainCall is result i of n of a method called on a call result,
// f(...).M() or pkg.F(...).M() (GR-O9, task T023): when f declares
// exactly one result whose type T (or *T) is a module type, M is a
// captured method of T and every return of M yields proven objects of one
// framework built inside M (GR-O5's factory rule: no parameter, and the
// receiver is no router object), the call's value is those objects. The
// receiver value f(...) only selects the method; M's body does not read a
// router from it (a field of the receiver is no proven object). Otherwise
// the refusal factory_method_chain stands.
func (l *goLinker) chainCall(at goAt, e *facts.GoExpr, i, n int, site string) goValue {
	if rec := l.chainRecord(at, e); rec != nil {
		if v := l.factory(at, e, rec, i, n, site); v.proven() {
			l.tag("GR-O9")
			return v
		}
	}
	return l.methodChain(e.Callee.Name)
}

// methodChain is the value of a method called on a call result (GR-O5's
// refusal factory_method_chain). It is evidence of a router only when a
// method of that name in the module declares a router result: Header().Get
// is no router call.
func (l *goLinker) methodChain(name string) goValue {
	return goValue{unproven: true, evidence: l.routerMethods[name], form: goFactoryMethodChain}
}

// goCtorText is the constructor form's text in the contract.
func goCtorText(fw, name string) string {
	switch fw {
	case goNetHTTP:
		return "http." + name + "()"
	case goChi:
		return "chi." + name + "()"
	}
	return "gin." + name + "()"
}

// factoryCall is result i of n of a call of a module function f() or
// pkg.F() or of a method of a typed receiver (GR-O5).
func (l *goLinker) factoryCall(at goAt, e *facts.GoExpr, i, n int, site string) goValue {
	c := e.Callee
	if c == nil {
		return goUnproven()
	}
	switch c.Kind {
	case facts.GoIdentifier:
		nm := l.lookup(at, c.Name)
		if nm.kind != "func" {
			return goUnproven()
		}
		return l.factory(at, e, l.funcRecord(l.ownFiles(at.rec.file), c.Name), i, n, site)
	case facts.GoSelector:
		if c.Object == nil {
			return goUnproven()
		}
		if imp, ok := l.importOf(at, c.Object); ok {
			if imp.files == nil {
				return goUnproven()
			}
			return l.factory(at, e, l.funcRecord(imp.files, c.Name), i, n, site)
		}
		if c.Object.Kind == facts.GoCall {
			return l.chainCall(at, e, i, n, site)
		}
		return l.factory(at, e, l.methodRecord(at, c.Object, c.Name), i, n, site)
	}
	return goUnproven()
}

// funcRecord is the captured declaration of function name among files
// (nil when there is none or several).
func (l *goLinker) funcRecord(files []string, name string) *goRecord {
	var out *goRecord
	for _, p := range files {
		if r := l.byDecl[p][name]; r != nil {
			if out != nil {
				return nil
			}
			out = r
		}
	}
	return out
}

// methodRecord is the captured declaration of method name of x's stated
// type (the GR-H2 typing).
func (l *goLinker) methodRecord(at goAt, x *facts.GoExpr, name string) *goRecord {
	t, file := l.typeOf(at, x)
	if t == nil {
		return nil
	}
	files, typ := l.typeFiles(file, t)
	if typ == "" {
		return nil
	}
	return l.funcRecord(files, typ+"."+name)
}

// factory is result i of n of a call of rec (GR-O5): the objects every
// return statement of rec returns, when each is a proven object of one
// framework and none is a parameter of rec (the factory's own objects;
// a parameter would depend on the call). The arguments bind rec's
// router-typed parameters (GR-O4).
func (l *goLinker) factory(at goAt, e *facts.GoExpr, rec *goRecord, i, n int, site string) goValue {
	if rec == nil {
		return goUnproven()
	}
	l.bindArgs(at, e, rec, site)
	key := fmt.Sprintf("%s/%d/%d", rec.key, i, n)
	if v, ok := l.factories[key]; ok {
		return v
	}
	if l.building[key] {
		return goUnproven()
	}
	l.building[key] = true
	defer delete(l.building, key)
	var out goValue
	fn := rec.fn
	if len(fn.Results) != n || len(fn.Returns) == 0 {
		out = goUnproven()
	}
	for k, ret := range fn.Returns {
		if out.unproven {
			break
		}
		if len(ret.Values) != n {
			out = goUnproven()
			break
		}
		v := copied(l.eval(goAt{rec, ret.Block, goStart(ret.Span)}, &ret.Values[i], fmt.Sprintf("%s/r%d/%d", rec.key, k, i)))
		if k == 0 {
			out = v
		} else {
			out = merge(out, v)
		}
	}
	if out.proven() {
		for _, id := range out.objects {
			if strings.HasPrefix(id, "param:") {
				out = goUnproven()
				break
			}
		}
	}
	if out.proven() && l.framework(out) == "" {
		out = goUnproven()
	}
	if out.proven() {
		l.tag("GR-O5")
	}
	l.factories[key] = out
	return out
}

// bindArgs records the GR-O4 bindings of a resolved call of rec: each
// argument holding proven objects binds a router-typed parameter that is
// never reassigned.
func (l *goLinker) bindArgs(at goAt, e *facts.GoExpr, rec *goRecord, site string) {
	for k := range e.Args {
		if k >= len(rec.fn.Params) {
			break
		}
		p := rec.fn.Params[k]
		fw, value := l.routerType(rec.file, &p.Type)
		if fw == "" || value || p.Variadic || p.Name == "" || p.Name == "_" {
			continue
		}
		if len(l.writes(goName{kind: "param", rec: rec, decl: k}, p.Name)) > 0 {
			continue
		}
		v := copied(l.eval(at, &e.Args[k], fmt.Sprintf("%s/a%d", site, k)))
		if !v.proven() || l.framework(v) != fw {
			continue
		}
		param := l.object(goObject{id: fmt.Sprintf("param:%s#%d", rec.key, k), framework: fw, kind: "param", file: rec.file, span: rec.fn.Span})
		for _, id := range v.objects {
			l.bind(param, id, at, l.reachCond(at, &e.Args[k], id, map[string]bool{}))
		}
		l.tag("GR-O4")
	}
}

// bind records one parameter binding, once; carried reports that the
// argument reaches the call only through conditional bindings (GR-C5).
func (l *goLinker) bind(param, object string, at goAt, carried bool) {
	for _, b := range l.bindings {
		if b.param == param && b.object == object && b.file == at.rec.file && b.span.StartLine == at.pos.line && b.span.StartColumn == at.pos.col {
			return
		}
	}
	l.bindings = append(l.bindings, goBinding{param: param, object: object, file: at.rec.file, span: contract.Span{StartLine: at.pos.line, StartColumn: at.pos.col, EndLine: at.pos.line, EndColumn: at.pos.col}, conditional: conditional(at) || carried, at: at})
}

// conditional reports conditional context at at (GR-C5): a block of kind
// then, else, for, case, go or defer, or a function literal other than a
// GR-O1 callback, between the position and its declaration.
func conditional(at goAt) bool {
	for rec, block := at.rec, at.block; rec != nil && rec.fn != nil; {
		fn := rec.fn
		for b := block; b > 0 && b < len(fn.Blocks); b = fn.Blocks[b].Parent {
			switch fn.Blocks[b].Kind {
			case facts.GoBlockThen, facts.GoBlockElse, facts.GoBlockFor, facts.GoBlockCase, facts.GoBlockGo, facts.GoBlockDefer:
				return true
			}
		}
		if rec.parent == nil {
			return false
		}
		if !rec.callback {
			return true
		}
		block = fn.Block
		rec = rec.parent
	}
	return false
}

// child is the value of a method call on proven router objects: a chi
// Route, Group or With child (GR-O1, GR-O2), a gin Group child or the
// object itself for gin Use (GR-O3).
func (l *goLinker) child(at goAt, e *facts.GoExpr, recv goValue, site string) goValue {
	fw := l.framework(recv)
	name := e.Callee.Name
	var kind string
	var prefix *facts.GoExpr
	callback := -1
	switch {
	case fw == goChi && name == "Route" && len(e.Args) == 2:
		kind, prefix, callback = "route", &e.Args[0], 1
	case fw == goChi && name == "Group" && len(e.Args) == 1:
		kind, callback = "group", 0
	case fw == goChi && name == "With":
		kind = "with"
	case fw == goGin && name == "Group" && len(e.Args) >= 1:
		kind, prefix = "ginGroup", &e.Args[0]
	case fw == goGin && name == "Use":
		l.tag("GR-O3")
		l.tag("GR-O3/`g.Use(mw...)`")
		l.recordUse(at, e, recv)
		v := recv
		v.zero = false
		return v
	case fw == goChi && name == "Use":
		// chi's Use returns nothing; its middleware is read
		// separately (gohttp_middleware.go).
		l.recordUse(at, e, recv)
		return goUnproven()
	default:
		return goUnproven()
	}
	var out []string
	for _, parent := range recv.objects {
		o := goObject{id: site + "<" + parent, framework: fw, kind: kind, file: at.rec.file, parent: parent, prefix: prefix, at: at, expr: e}
		o.conditional = conditional(at) || l.reachCond(at, e.Callee.Object, parent, map[string]bool{})
		if kind == "with" {
			if p := l.objects[parent]; p.kind == "with" {
				o.middleware = append(o.middleware, p.middleware...)
			}
			for _, a := range e.Args {
				o.middleware = append(o.middleware, goArgText(a))
			}
		}
		id := l.object(o)
		out = append(out, id)
		l.recordGroupArgs(id, kind, at, e)
	}
	switch kind {
	case "route":
		l.tag("GR-O1")
		l.tag("GR-O1/`r.Route(p, f)`")
	case "group":
		l.tag("GR-O1")
		l.tag("GR-O1/`r.Group(f)`")
	case "with":
		l.tag("GR-O2")
	case "ginGroup":
		l.tag("GR-O3")
		l.tag("GR-O3/`g.Group(p, mw...)`")
	}
	if callback >= 0 {
		l.callback(at, &e.Args[callback], out, site)
	}
	sort.Strings(out)
	return goObjects(out...)
}

// callback binds a GR-O1 callback's chi.Router parameter to the children:
// a function literal or a module function (identifier or pkg.F) whose
// first parameter is chi.Router; any other callback is callback_unread.
func (l *goLinker) callback(at goAt, f *facts.GoExpr, children []string, site string) {
	var rec *goRecord
	switch f.Kind {
	case facts.GoFuncLit:
		if f.Func == 0 {
			// A literal the payload did not capture holds no router
			// syntax: nothing in it registers on the child.
			return
		}
		if f.Func <= len(l.records[at.rec.file]) {
			rec = l.records[at.rec.file][f.Func-1]
			rec.callback = true
		}
	case facts.GoIdentifier:
		if l.lookup(at, f.Name).kind == "func" {
			rec = l.funcRecord(l.ownFiles(at.rec.file), f.Name)
		}
	case facts.GoSelector:
		if imp, ok := l.importOf(at, f.Object); ok && imp.files != nil {
			rec = l.funcRecord(imp.files, f.Name)
		}
	}
	if rec == nil || len(rec.fn.Params) == 0 {
		l.count(at.rec.file, site, goCallbackUnread)
		return
	}
	p := rec.fn.Params[0]
	if fw, _ := l.routerType(rec.file, &p.Type); fw != goChi || p.Type.Kind == facts.GoPointer {
		l.count(at.rec.file, site, goCallbackUnread)
		return
	}
	param := l.object(goObject{id: fmt.Sprintf("param:%s#0", rec.key), framework: goChi, kind: "param", file: rec.file, span: rec.fn.Span})
	for _, c := range children {
		l.bind(param, c, at, false)
	}
}

// goArgText is an argument's whitespace-free text (a function literal has
// none in the payload).
func goArgText(a facts.GoExpr) string {
	if a.Text != "" {
		return a.Text
	}
	if a.Kind == facts.GoFuncLit {
		return "func(...){...}"
	}
	return ""
}

// typeOf is the type a name's declaration states (GR-H2): a receiver or
// parameter declared T or *T, a var with a type, a := or var of a
// composite literal T{...} or &T{...}, or of a call of a module function
// with one declared result type. file is where the type is written.
func (l *goLinker) typeOf(at goAt, x *facts.GoExpr) (*facts.GoExpr, string) {
	if x == nil || x.Kind != facts.GoIdentifier {
		return nil, ""
	}
	n := l.lookup(at, x.Name)
	switch n.kind {
	case "receiver":
		l.tag("GR-H2/receiver or parameter")
		return &n.rec.fn.Receiver.Type, n.rec.file
	case "param":
		l.tag("GR-H2/receiver or parameter")
		return &n.rec.fn.Params[n.decl].Type, n.rec.file
	case "local":
		bd := n.rec.fn.Bindings[n.decl]
		if bd.Type != nil {
			l.tag("GR-H2/`var` with a type")
			return bd.Type, n.rec.file
		}
		if len(bd.Values) != len(bd.Names) || bd.Kind != facts.GoBindDefine && bd.Kind != facts.GoBindVar {
			return nil, ""
		}
		v := bd.Values[hasName(bd.Names, x.Name)]
		if v.Kind == facts.GoUnary && v.Value == "&" && v.Object != nil && v.Object.Kind == facts.GoComposite {
			v = *v.Object
		}
		if v.Kind == facts.GoComposite && v.Type != nil {
			l.tag("GR-H2/composite literal")
			return v.Type, n.rec.file
		}
		if v.Kind == facts.GoCall && v.Callee != nil {
			if t, file := l.signature(goAt{n.rec, bd.Block, goStart(bd.Span)}, v.Callee); t != nil {
				l.tag("GR-H2/call of a module function")
				return t, file
			}
		}
	}
	return nil, ""
}

// signature is the single declared result type of a module function f or
// pkg.F (facts.GoFacts.Signatures).
func (l *goLinker) signature(at goAt, callee *facts.GoExpr) (*facts.GoExpr, string) {
	var files []string
	name := callee.Name
	switch callee.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, name).kind != "func" {
			return nil, ""
		}
		files = l.ownFiles(at.rec.file)
	case facts.GoSelector:
		imp, ok := l.importOf(at, callee.Object)
		if !ok || imp.files == nil {
			return nil, ""
		}
		files = imp.files
	default:
		return nil, ""
	}
	var out *facts.GoExpr
	file := ""
	for _, p := range files {
		g := l.files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Signatures {
			if g.Signatures[i].Name == name {
				if out != nil {
					return nil, ""
				}
				out, file = &g.Signatures[i].Result, p
			}
		}
	}
	return out, file
}

// typeFiles resolves a type T, *T or pkg.T written in file to the files of
// its module package and its name ("" when it is not a module type).
func (l *goLinker) typeFiles(file string, t *facts.GoExpr) ([]string, string) {
	if t.Kind == facts.GoPointer && t.Object != nil {
		t = t.Object
	}
	switch t.Kind {
	case facts.GoIdentifier:
		return l.ownFiles(file), t.Name
	case facts.GoSelector:
		if t.Object == nil || t.Object.Kind != facts.GoIdentifier {
			return nil, ""
		}
		imp, ok := l.imports[file][t.Object.Name]
		if !ok || imp.files == nil {
			return nil, ""
		}
		return imp.files, t.Name
	}
	return nil, ""
}

// callable is the identity of the function or method node named name
// (qualified name) among files; "" when there is none or several.
func (l *goLinker) callable(files []string, name string, kinds ...string) string {
	identity := ""
	for _, p := range files {
		for _, n := range l.files[p].Nodes {
			ok := false
			for _, k := range kinds {
				ok = ok || n.Kind == k
			}
			if ok && n.QualifiedName == name {
				if identity != "" {
					return ""
				}
				identity = n.Identity
			}
		}
	}
	return identity
}

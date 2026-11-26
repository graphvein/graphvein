package routes

import (
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// This file states FastAPI security dependencies (T026, ADR
// 0079 option 3A, docs/extract/auth.md): for each route, the Depends and
// Security chains through handler parameters, Annotated aliases and route,
// router, include_router and app `dependencies=` lists that reach an
// instance of a fastapi.security scheme class. The route attribute says
// what the code declares ("declares <scheme>"), never that a route is
// protected: a scheme with auto_error=False lets requests without
// credentials through (langflow).

// AuthRow is one row of the FastAPI auth claim contract (rows DA-F of
// docs/contract/claims/architecture-graph.md).
type AuthRow struct {
	ID string
	// Form names the source form the row reads.
	Form string
}

// FastAPIAuthContract lists the rows in task-file order. Every form the
// code reads maps to exactly one row; everything else is DAD, the single
// unknown branch (an entry in security_dependencies_unknown, or nothing
// for a form that declares no dependency).
var FastAPIAuthContract = []AuthRow{
	{"DA-F1", "Depends/Security in a handler parameter, an Annotated alias or a dependencies= list"},
	{"DA-F2", "a dependency function's own parameters"},
	{"DA-F3", "a module-level fastapi.security scheme instance"},
	{"DAD", "every other form"},
}

// authTag is a test-only hook: every branch reports its row ("<row>" or
// "<row>/<form>").
var authTag func(row string)

func tagAuth(row string) {
	if authTag != nil {
		authTag(row)
	}
}

// maxDependencyDepth is row DA-F2's cap: steps beyond it are truncated.
const maxDependencyDepth = 8

// securityClasses are DA-F3's classes by defining module (fastapi
// 0.161.1); each class is exported from fastapi.security too. OAuth2 is
// the base the documented OAuth2 subclasses extend.
var securityClasses = map[string]string{
	"OAuth2": "oauth2", "OAuth2PasswordBearer": "oauth2", "OAuth2AuthorizationCodeBearer": "oauth2",
	"HTTPBearer": "http", "HTTPBasic": "http", "HTTPDigest": "http",
	"APIKeyHeader": "api_key", "APIKeyQuery": "api_key", "APIKeyCookie": "api_key",
	"OpenIdConnect": "open_id_connect_url",
}

// positional holds the positional index of scheme_name and auto_error for
// the classes whose __init__ accepts them positionally (fastapi 0.161.1);
// the others take them as keywords only.
var positional = map[string][2]int{
	"OAuth2PasswordBearer":          {1, 4},
	"OAuth2AuthorizationCodeBearer": {3, 6},
}

// securityStep is one step of a dependency chain: where the dependency is
// declared (via, file, span of the Depends call or parameter) and what it
// reaches (a function identity, or the scheme instance as file#name).
type securityStep struct {
	Via    string         `json:"via"`
	File   string         `json:"file"`
	Span   *contract.Span `json:"span"`
	Target string         `json:"target"`
}

// securityScheme is one entry of the route attribute security_dependencies.
type securityScheme struct {
	Class      string         `json:"class"`
	Subclass   string         `json:"subclass,omitempty"`
	SchemeName any            `json:"scheme_name"`
	AutoError  any            `json:"auto_error"`
	Instance   string         `json:"instance"`
	Chain      []securityStep `json:"chain"`
}

// securityUnknown is one entry of security_dependencies_unknown: a step
// the linker could not read (DA-F1 unresolved, DA-F2 truncated, DAD).
type securityUnknown struct {
	Reason     string         `json:"reason"`
	Expression string         `json:"expression"`
	Chain      []securityStep `json:"chain"`
}

// dependencyRef is a declared dependency: a list item or parameter in a
// file and lexical scope.
type dependencyRef struct {
	file  string
	scope int
	expr  facts.RouteExpr
	via   string
	// site is the file the declaration is written in (a parameter's file
	// when the Depends call is in an alias of another module); file when
	// empty.
	site string
	span *contract.Span
	// typ is the annotation type Depends() without a callable falls back
	// to (nil when there is none).
	typ *facts.RouteExpr
}

// dependencyList is a dependencies= keyword: its items, or the reason it
// cannot be read.
type dependencyList struct {
	refs    []dependencyRef
	unknown []securityUnknown
}

// readDependencyList reads a dependencies= value (DA-F1) in file/scope.
func readDependencyList(file string, scope int, e facts.RouteExpr, via string) dependencyList {
	var out dependencyList
	for _, kw := range e.Keywords {
		if kw.Name == "" && kw.Value.Kind == "dictionary_splat" {
			tagAuth("DAD/splat")
			out.unknown = append(out.unknown, securityUnknown{Reason: "dependencies may come from keyword unpacking", Expression: kw.Value.Text, Chain: []securityStep{}})
		}
		if kw.Name != "dependencies" {
			continue
		}
		v := kw.Value
		if v.Kind != "list" && v.Kind != "tuple" {
			tagAuth("DAD/list")
			out.unknown = append(out.unknown, securityUnknown{Reason: "dependencies is not a literal list", Expression: v.Text, Chain: []securityStep{}})
			continue
		}
		for _, item := range v.Args {
			out.refs = append(out.refs, dependencyRef{file: file, scope: scope, expr: item, via: via, span: item.Span})
		}
	}
	return out
}

// fastapiMountDeps is the dependency side of one FastAPI mount: an
// include_router's dependencies, or a Starlette mount (which starts a new
// app: the parent's dependencies do not apply).
type fastapiMountDeps struct {
	starlette bool
	list      dependencyList
}

// securityLinker resolves dependency chains through the shared Python
// resolver.
type securityLinker struct {
	files   map[string]facts.Facts
	modules *python.ModuleResolver
	// functions maps a function or method identity to its file and scope.
	functions map[string]functionScope
}

type functionScope struct {
	file  string
	scope int
}

func newSecurityLinker(files map[string]facts.Facts, modules *python.ModuleResolver) *securityLinker {
	l := &securityLinker{files: files, modules: modules, functions: map[string]functionScope{}}
	for _, file := range facts.Paths(files) {
		p := files[file].Python
		if p == nil {
			continue
		}
		for _, s := range p.Scopes {
			for _, b := range s.Bindings {
				if b.Identity != "" && b.Value.Kind == "function_definition" {
					l.functions[b.Identity] = functionScope{file, b.ClassScope}
				}
			}
		}
	}
	return l
}

// fullName is an external binding's dotted name.
func fullName(b python.Binding) string {
	if b.Name == "" {
		return b.File
	}
	return b.File + "." + b.Name
}

// marker classifies a call in a parameter default or Annotated metadata:
// "depends" (fastapi Depends/Security), "param" (another FastAPI parameter
// marker), "other" (an external call that declares nothing) or "unknown".
func (l *securityLinker) marker(file string, scope int, e facts.RouteExpr) string {
	if e.Kind != "call" || e.Object == nil {
		return "other"
	}
	b := l.modules.Expression(file, scope, *e.Object)
	if !b.External {
		return "unknown"
	}
	name := fullName(b)
	module, short := name, ""
	if at := strings.LastIndex(name, "."); at >= 0 {
		module, short = name[:at], name[at+1:]
	}
	if module != "fastapi" && module != "fastapi.param_functions" && module != "fastapi.params" {
		return "other"
	}
	switch short {
	case "Depends", "Security":
		return "depends"
	case "Query", "Path", "Body", "Header", "Cookie", "Form", "File", "Param":
		return "param"
	}
	return "unknown"
}

// annotated reports whether e is Annotated[...] (typing or
// typing_extensions).
func (l *securityLinker) annotated(file string, scope int, e facts.RouteExpr) bool {
	if e.Kind != "subscript" || e.Object == nil || len(e.Args) == 0 {
		return false
	}
	b := l.modules.Expression(file, scope, *e.Object)
	name := fullName(b)
	return b.External && (name == "typing.Annotated" || name == "typing_extensions.Annotated")
}

// metadataItem is one Annotated metadata entry with the scope it is
// written in (an alias may live in another module).
type metadataItem struct {
	file  string
	scope int
	expr  facts.RouteExpr
}

// flatten returns an annotation's type argument and Annotated metadata,
// following module-level aliases (X = Annotated[T, ...]) and nested
// Annotated as Python flattens them.
func (l *securityLinker) flatten(file string, scope int, e facts.RouteExpr, seen map[string]bool) (*metadataItem, []metadataItem) {
	if e.Kind == "identifier" || e.Kind == "attribute" {
		b := l.modules.Expression(file, scope, e)
		if b.External || b.File == "" || b.Scope != 0 || b.Syntax.Value.Kind != "subscript" || !b.Syntax.Stable {
			return &metadataItem{file, scope, e}, nil
		}
		key := b.File + "#" + b.Name
		if seen[key] {
			return &metadataItem{file, scope, e}, nil
		}
		seen[key] = true
		defer delete(seen, key)
		return l.flatten(b.File, 0, b.Syntax.Value, seen)
	}
	if !l.annotated(file, scope, e) {
		return &metadataItem{file, scope, e}, nil
	}
	typ, inner := l.flatten(file, scope, e.Args[0], seen)
	items := append([]metadataItem(nil), inner...)
	for _, m := range e.Args[1:] {
		items = append(items, metadataItem{file, scope, m})
	}
	return typ, items
}

// parameterDependency reads one parameter: the Depends it declares (FastAPI
// takes the last FastAPI-specific Annotated item, then a Depends default),
// or an unknown entry, or nothing.
func (l *securityLinker) parameterDependency(file string, scope int, p facts.PythonDependencyParameter, via string) (*dependencyRef, *securityUnknown) {
	var typ *metadataItem
	var items []metadataItem
	if p.Annotation != nil {
		typ, items = l.flatten(file, scope, *p.Annotation, map[string]bool{})
	}
	chosen, unknownAfter := -1, false
	for i, m := range items {
		switch l.marker(m.file, m.scope, m.expr) {
		case "depends":
			chosen, unknownAfter = i, false
		case "param":
			chosen, unknownAfter = -1, false
		case "unknown":
			unknownAfter = true
		}
	}
	span := p.Span
	if chosen >= 0 && unknownAfter {
		tagAuth("DAD/metadata")
		return nil, &securityUnknown{Reason: "a later Annotated item is not read", Expression: items[len(items)-1].expr.Text}
	}
	if chosen >= 0 {
		m := items[chosen]
		ref := &dependencyRef{file: m.file, scope: m.scope, expr: m.expr, via: via, site: file, span: &span}
		if typ != nil {
			t := typ.expr
			ref.typ = &t
			if typ.file != m.file {
				// Depends() without a callable uses the type, which an alias
				// in another module wrote; the fallback is not read.
				ref.typ = nil
			}
		}
		return ref, nil
	}
	if p.Default != nil && l.marker(file, scope, *p.Default) == "depends" {
		ref := &dependencyRef{file: file, scope: scope, expr: *p.Default, via: via, span: &span}
		if p.Annotation != nil && typ != nil && typ.file == file {
			t := typ.expr
			ref.typ = &t
		}
		return ref, nil
	}
	if unknownAfter {
		tagAuth("DAD/metadata")
		return nil, &securityUnknown{Reason: "an Annotated item is not read", Expression: items[len(items)-1].expr.Text}
	}
	return nil, nil
}

// result collects one route's schemes and unknown entries.
type securityResult struct {
	schemes []securityScheme
	unknown []securityUnknown
	seen    map[string]bool
}

func (r *securityResult) addUnknown(u securityUnknown, chain []securityStep) {
	u.Chain = append([]securityStep{}, chain...)
	r.unknown = append(r.unknown, u)
}

// follow resolves one declared dependency (a Depends/Security call) at
// depth (1 for the route's own declarations).
func (l *securityLinker) follow(ref dependencyRef, chain []securityStep, depth int, out *securityResult, active map[string]bool) {
	if ref.expr.Kind != "call" || l.marker(ref.file, ref.scope, ref.expr) != "depends" {
		// A dependencies= item that is not a Depends/Security call.
		tagAuth("DAD/item")
		out.addUnknown(securityUnknown{Reason: "dependency item is not a Depends or Security call", Expression: ref.expr.Text}, chain)
		return
	}
	target, ok := argumentOf(ref.expr, "dependency", 0)
	file, scope := ref.file, ref.scope
	if !ok || target.Kind == "none" {
		if ref.typ == nil {
			tagAuth("DAD/no-callable")
			out.addUnknown(securityUnknown{Reason: "Depends() without a callable: the annotation type is not read", Expression: ref.expr.Text}, chain)
			return
		}
		target = *ref.typ
	}
	step := securityStep{Via: ref.via, File: ref.file, Span: ref.span}
	if ref.site != "" {
		step.File = ref.site
	}
	if step.Span == nil {
		step.Span = ref.expr.Span
	}
	row := "DA-F1"
	if depth > 1 {
		row = "DA-F2"
	}
	if depth > maxDependencyDepth {
		tagAuth("DA-F2/truncated")
		step.Target = target.Text
		out.addUnknown(securityUnknown{Reason: "truncated: dependency depth above 8", Expression: target.Text}, append(chain, step))
		return
	}
	if target.Kind == "call" {
		tagAuth("DAD/inline")
		out.addUnknown(securityUnknown{Reason: "inline dependency value is not read (DA-F3 reads module-level instances)", Expression: target.Text}, chain)
		return
	}
	if target.Kind != "identifier" && target.Kind != "attribute" {
		tagAuth("DAD/expression")
		out.addUnknown(securityUnknown{Reason: "dependency expression is not read", Expression: target.Text}, chain)
		return
	}
	b := l.modules.Expression(file, scope, target)
	switch {
	case b.External:
		tagAuth("DAD/external")
		out.addUnknown(securityUnknown{Reason: "external callable " + fullName(b) + " is not read", Expression: target.Text}, chain)
	case b.File == "" || b.Module != nil:
		tagAuth(row + "/unresolved")
		out.addUnknown(securityUnknown{Reason: "unresolved dependency", Expression: target.Text}, chain)
	case b.Syntax.Value.Kind == "function_definition":
		tagAuth(row)
		step.Target = b.Identity
		next := append(append([]securityStep{}, chain...), step)
		if active[b.Identity] {
			tagAuth("DAD/cycle")
			out.addUnknown(securityUnknown{Reason: "dependency cycle", Expression: target.Text}, next)
			return
		}
		active[b.Identity] = true
		defer delete(active, b.Identity)
		l.parameters(b.Identity, next, depth+1, out, active)
	case b.Syntax.Value.Kind == "class_definition":
		tagAuth("DAD/class")
		out.addUnknown(securityUnknown{Reason: "class dependency: its __init__ parameters are not read", Expression: target.Text}, chain)
	case b.Scope == 0 && b.Syntax.Value.Kind == "call":
		l.instance(b, target, step, chain, out)
	default:
		tagAuth("DAD/value")
		out.addUnknown(securityUnknown{Reason: "dependency value is not read", Expression: target.Text}, chain)
	}
}

// parameters follows a dependency function's own parameters (DA-F2).
func (l *securityLinker) parameters(identity string, chain []securityStep, depth int, out *securityResult, active map[string]bool) {
	fs, ok := l.functions[identity]
	if !ok {
		tagAuth("DAD/scope")
		out.addUnknown(securityUnknown{Reason: "function parameters are not read", Expression: identity}, chain)
		return
	}
	p := l.files[fs.file].Python
	if p == nil || fs.scope <= 0 || fs.scope >= len(p.Scopes) {
		tagAuth("DAD/scope")
		out.addUnknown(securityUnknown{Reason: "function parameters are not read", Expression: identity}, chain)
		return
	}
	s := p.Scopes[fs.scope]
	if !p.FastAPIDependencies {
		if len(s.Parameters) > 0 {
			tagAuth("DAD/ungated")
			out.addUnknown(securityUnknown{Reason: "parameters not read: the file does not import fastapi", Expression: identity}, chain)
		}
		return
	}
	for _, param := range s.Dependencies {
		ref, unknown := l.parameterDependency(fs.file, fs.scope, param, "parameter "+param.Name)
		if unknown != nil {
			span := param.Span
			out.addUnknown(*unknown, append(append([]securityStep{}, chain...), securityStep{Via: "parameter " + param.Name, File: fs.file, Span: &span}))
		}
		if ref != nil {
			l.follow(*ref, chain, depth, out, active)
		}
	}
}

// instance applies DA-F3 to a module-level instance binding.
func (l *securityLinker) instance(b python.Binding, target facts.RouteExpr, step securityStep, chain []securityStep, out *securityResult) {
	call := b.Syntax.Value
	if !b.Syntax.Stable || call.Object == nil {
		tagAuth("DAD/instance")
		out.addUnknown(securityUnknown{Reason: "instance binding is not a single unconditional assignment", Expression: target.Text}, chain)
		return
	}
	class, subclass, ownInit := l.securityClass(b.File, 0, *call.Object, map[string]bool{}, 0)
	if class == "" {
		tagAuth("DAD/instance")
		out.addUnknown(securityUnknown{Reason: "instance of a class that does not reach a fastapi.security scheme; its __call__ is not read", Expression: target.Text}, chain)
		return
	}
	tagAuth("DA-F3")
	if subclass != "" {
		tagAuth("DA-F3/subclass")
	}
	step.Target = b.File + "#" + b.Name
	s := securityScheme{Class: class, Subclass: subclass, Instance: step.Target, Chain: append(append([]securityStep{}, chain...), step)}
	s.SchemeName, s.AutoError = l.schemeSettings(call, class, subclass, ownInit)
	if s.AutoError == "unknown" {
		tagAuth("DA-F3/auto_error-unknown")
	}
	key := s.Instance
	if out.seen[key] {
		return
	}
	out.seen[key] = true
	out.schemes = append(out.schemes, s)
}

// securityClass resolves a class expression to the fastapi.security class
// it is or extends: the class, the repository subclass name (empty for
// the class itself) and whether a repository class on the way defines
// __init__ (then the instance's settings are not read).
func (l *securityLinker) securityClass(file string, scope int, e facts.RouteExpr, seen map[string]bool, depth int) (string, string, bool) {
	if depth > maxDependencyDepth {
		return "", "", false
	}
	b := l.modules.Expression(file, scope, e)
	if b.External {
		name := fullName(b)
		at := strings.LastIndex(name, ".")
		if at < 0 {
			return "", "", false
		}
		module, short := name[:at], name[at+1:]
		sub, ok := securityClasses[short]
		if ok && (module == "fastapi.security" || module == "fastapi.security."+sub) {
			return short, "", false
		}
		return "", "", false
	}
	if b.File == "" || b.Syntax.Value.Kind != "class_definition" {
		return "", "", false
	}
	key := b.File + "#" + b.Identity
	if seen[key] {
		return "", "", false
	}
	seen[key] = true
	p := l.files[b.File].Python
	ownInit := false
	if p != nil && b.Syntax.ClassScope > 0 && b.Syntax.ClassScope < len(p.Scopes) {
		_, ownInit = p.Scopes[b.Syntax.ClassScope].Bindings["__init__"]
	}
	for _, base := range b.Syntax.Bases {
		if base.Kind != "identifier" && base.Kind != "attribute" {
			continue
		}
		class, _, init := l.securityClass(b.File, b.Scope, base, seen, depth+1)
		if class != "" {
			return class, b.Name, ownInit || init
		}
	}
	return "", "", false
}

// schemeSettings reads scheme_name and auto_error from the instance call
// (DA-F3): a literal as written, the class default when omitted, "unknown"
// otherwise.
func (l *securityLinker) schemeSettings(call facts.RouteExpr, class, subclass string, ownInit bool) (any, any) {
	var name any = class
	if subclass != "" {
		name = subclass
	}
	var autoError any = true
	if ownInit {
		// A repository __init__ decides what reaches the fastapi class.
		return "unknown", "unknown"
	}
	index, hasPositional := positional[class]
	for _, kw := range call.Keywords {
		if kw.Name == "" {
			// **kwargs may set either.
			return "unknown", "unknown"
		}
	}
	if hasPositional && len(call.Args) > index[0] {
		name = "unknown"
	}
	if hasPositional && len(call.Args) > index[1] {
		autoError = "unknown"
	}
	for _, kw := range call.Keywords {
		switch kw.Name {
		case "scheme_name":
			switch kw.Value.Kind {
			case "literal":
				if kw.Value.Name != "" {
					name = kw.Value.Name
				}
			case "none":
			default:
				name = "unknown"
			}
		case "auto_error":
			switch kw.Value.Kind {
			case "true":
				autoError = true
			case "false":
				autoError = false
			default:
				autoError = "unknown"
			}
		}
	}
	return name, autoError
}

// argumentOf returns a call's argument by keyword or position.
func argumentOf(e facts.RouteExpr, name string, index int) (facts.RouteExpr, bool) {
	for _, kw := range e.Keywords {
		if kw.Name == name {
			return kw.Value, true
		}
	}
	if len(e.Args) > index {
		return e.Args[index], true
	}
	return facts.RouteExpr{}, false
}

// routeSecurity computes one registration's attributes: the declared
// chains (object chains, then the route's own list, then the handler's
// parameters, FastAPI's order).
func (l *securityLinker) routeSecurity(chains []dependencyList, own dependencyList, handler string) map[string]any {
	out := &securityResult{seen: map[string]bool{}}
	for _, list := range chains {
		for _, u := range list.unknown {
			out.addUnknown(u, nil)
		}
		for _, ref := range list.refs {
			l.follow(ref, nil, 1, out, map[string]bool{})
		}
	}
	for _, u := range own.unknown {
		out.addUnknown(u, nil)
	}
	for _, ref := range own.refs {
		l.follow(ref, nil, 1, out, map[string]bool{})
	}
	if handler != "" {
		if fs, ok := l.functions[handler]; ok {
			p := l.files[fs.file].Python
			if p != nil && !p.FastAPIDependencies && fs.scope > 0 && fs.scope < len(p.Scopes) && len(p.Scopes[fs.scope].Parameters) > 0 {
				tagAuth("DAD/ungated")
				out.addUnknown(securityUnknown{Reason: "parameters not read: the file does not import fastapi", Expression: handler}, nil)
			} else if p != nil && fs.scope > 0 && fs.scope < len(p.Scopes) {
				for _, param := range p.Scopes[fs.scope].Dependencies {
					ref, unknown := l.parameterDependency(fs.file, fs.scope, param, "parameter "+param.Name)
					if unknown != nil {
						span := param.Span
						out.addUnknown(*unknown, []securityStep{{Via: "parameter " + param.Name, File: fs.file, Span: &span}})
					}
					if ref != nil {
						l.follow(*ref, nil, 1, out, map[string]bool{handler: true})
					}
				}
			}
		}
	}
	attrs := map[string]any{}
	if len(out.schemes) > 0 {
		attrs["security_dependencies"] = out.schemes
	}
	if len(out.unknown) > 0 {
		attrs["security_dependencies_unknown"] = out.unknown
	}
	return attrs
}

// objectChains returns, per object key, the dependency lists of every
// chain from an app root to it (root first; the object's own list last).
// An object no chain reaches has no entry.
func objectChains(objects map[string]pythonObject, mounts []mount, extra []fastapiMountDeps) map[string][][]dependencyList {
	type edge struct {
		to   string
		deps fastapiMountDeps
	}
	adjacent := map[string][]edge{}
	for i, m := range mounts {
		adjacent[m.from] = append(adjacent[m.from], edge{m.to, extra[i]})
	}
	var keys []string
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reached := map[string][][]dependencyList{}
	var visit func(key string, prefix []dependencyList, seen map[string]bool)
	visit = func(key string, prefix []dependencyList, seen map[string]bool) {
		if seen[key] {
			return
		}
		seen[key] = true
		defer delete(seen, key)
		chain := append(append([]dependencyList{}, prefix...), objects[key].deps)
		reached[key] = append(reached[key], chain)
		for _, e := range adjacent[key] {
			if e.deps.starlette {
				// A mounted app starts its own dependency chain.
				visit(e.to, nil, seen)
				continue
			}
			visit(e.to, append(append([]dependencyList{}, chain...), e.deps.list), seen)
		}
	}
	for _, key := range keys {
		if objects[key].kind == "app" {
			visit(key, nil, map[string]bool{})
		}
	}
	return reached
}

// agreeChains keeps the chains' dependencies when every chain declares
// the same ones; otherwise the ones every chain declares, and an unknown
// entry for each of the others.
func agreeChains(chains [][]dependencyList) []dependencyList {
	if len(chains) == 0 {
		return nil
	}
	refKey := func(r dependencyRef) string { return r.file + ":" + strconv.Itoa(r.expr.Offset) }
	signature := func(c []dependencyList) []string {
		var s []string
		for _, list := range c {
			for _, r := range list.refs {
				s = append(s, refKey(r))
			}
			for _, u := range list.unknown {
				s = append(s, "?"+u.Reason+u.Expression)
			}
		}
		return s
	}
	first := strings.Join(signature(chains[0]), "\x00")
	same := true
	for _, c := range chains[1:] {
		if strings.Join(signature(c), "\x00") != first {
			same = false
		}
	}
	if same {
		return chains[0]
	}
	tagAuth("DAD/chains")
	counts := map[string]int{}
	for _, c := range chains {
		for _, s := range distinctStrings(signature(c)) {
			counts[s]++
		}
	}
	shared := dependencyList{}
	listed := map[string]bool{}
	for i, c := range chains {
		for _, list := range c {
			for _, r := range list.refs {
				k := refKey(r)
				switch {
				case counts[k] == len(chains) && i == 0:
					shared.refs = append(shared.refs, r)
				case counts[k] < len(chains) && !listed[k]:
					listed[k] = true
					shared.unknown = append(shared.unknown, securityUnknown{Reason: "dependency declared on some mount chains only", Expression: r.expr.Text})
				}
			}
			for _, u := range list.unknown {
				k := "?" + u.Reason + u.Expression
				if !listed[k] {
					listed[k] = true
					shared.unknown = append(shared.unknown, u)
				}
			}
		}
	}
	return []dependencyList{shared}
}

func distinctStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

package routes

import (
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Generated servers (contract row GR-G1, task T023, developer
// decision GO2). A file whose payload carries Routes.Generated passed the
// capture's token-for-token check against oapi-codegen v2.8.x's server
// templates (docs/lang/go-routes.md, row RO-server). Its entry functions
// are read as the templates define them:
//
//   - chi and net/http: Handler(si) registers on a new router;
//     HandlerFromMux(si, r) and HandlerFromMuxWithBaseURL(si, r, u) on r;
//     HandlerWithOptions(si, o) on o's BaseRouter, or a new router when o
//     is a composite literal without one. Each returns the router.
//   - gin: RegisterHandlers(router, si) and
//     RegisterHandlersWithOptions(router, si, o) register on router (the
//     parameter binding of GR-O4).
//
// The BaseURL that prefixes every registration is "" for Handler,
// HandlerFromMux and RegisterHandlers, the constant u, or o's BaseURL
// element (a constant, "" when absent); every call of the server's entry
// functions (its instances) must give the same value, else no route of
// the server has a path (route_prefix_unresolved). A registration's
// handler wrapper.N calls ServerInterface method N of the instances' si
// (the capture checked the wrapper method): the handler is method N of
// si's type when every instance states the same module struct type,
// otherwise it is unresolved.

// goOapiServer is one generated server file.
type goOapiServer struct {
	file, framework string
	// entry is HandlerWithOptions (chi, net/http) or
	// RegisterHandlersWithOptions (gin); router is its router local (r,
	// m; "" for gin) and ctor the binding that makes the new router.
	entry  *goRecord
	router string
	ctor   int
	// instances are the calls of the entry functions outside the
	// template's own functions.
	instances []goOapiInstance

	valueDone bool
	value     goValue
	baseDone  bool
	base      string
	baseLit   bool
	baseOK    bool
	typeDone  bool
	typeFiles []string
	typeName  string
}

// goOapiInstance is one call of a generated server's entry function.
type goOapiInstance struct {
	at    goAt
	call  *facts.GoExpr
	entry string
	span  contract.Span
}

// goOapiArity are the parameter counts of the entry functions.
var goOapiArity = map[string]int{"Handler": 1, "HandlerFromMux": 2, "HandlerFromMuxWithBaseURL": 3, "HandlerWithOptions": 2, "RegisterHandlers": 2, "RegisterHandlersWithOptions": 3}

// oapiRecognize finds the generated server files among the recognised
// files and their instances.
func (l *goLinker) oapiRecognize(files []string) {
	l.oapi = map[string]*goOapiServer{}
	for _, file := range files {
		gen := l.files[file].Go.Routes.Generated
		if gen == nil || gen.Generator != "oapi-codegen" {
			continue
		}
		s := &goOapiServer{file: file, framework: gen.Framework, ctor: -1}
		switch gen.Framework {
		case goChi, goNetHTTP:
			s.entry = l.byDecl[file]["HandlerWithOptions"]
			s.router = "r"
			if gen.Framework == goNetHTTP {
				s.router = "m"
			}
		case goGin:
			s.entry = l.byDecl[file]["RegisterHandlersWithOptions"]
		}
		if s.entry == nil {
			continue
		}
		if s.router != "" {
			for j, bd := range s.entry.fn.Bindings {
				if bd.Kind == facts.GoBindAssign && len(bd.Names) == 1 && bd.Names[0] == s.router && len(bd.Values) == 1 && bd.Values[0].Kind == facts.GoCall {
					s.ctor = j
				}
			}
			if s.ctor < 0 {
				continue
			}
		}
		l.oapi[file] = s
	}
	if len(l.oapi) == 0 {
		return
	}
	for _, file := range files {
		for _, rec := range l.records[file] {
			if l.oapiTemplate(rec) {
				continue
			}
			fn := rec.fn
			for k := range fn.Calls {
				c := &fn.Calls[k]
				l.oapiFind(goAt{rec, c.Block, goStart(c.Span)}, &c.Call, c.Span)
			}
			for j := range fn.Bindings {
				bd := &fn.Bindings[j]
				for i := range bd.Values {
					l.oapiFind(goAt{rec, bd.Block, goStart(bd.Span)}, &bd.Values[i], bd.Span)
				}
			}
			for k := range fn.Returns {
				ret := &fn.Returns[k]
				for i := range ret.Values {
					l.oapiFind(goAt{rec, ret.Block, goStart(ret.Span)}, &ret.Values[i], ret.Span)
				}
			}
		}
	}
}

// oapiTemplate reports a record inside a generated server's own entry
// functions: their calls of each other are the template, not instances.
func (l *goLinker) oapiTemplate(rec *goRecord) bool {
	d := rec.decl
	if d == nil || l.oapi[d.file] == nil || d.fn.Receiver != nil {
		return false
	}
	return goOapiArity[d.fn.Name] > 0
}

// oapiFind records the entry calls in an expression tree (function
// literals are records of their own).
func (l *goLinker) oapiFind(at goAt, e *facts.GoExpr, span contract.Span) {
	if e == nil || e.Kind == facts.GoFuncLit {
		return
	}
	if e.Kind == facts.GoCall {
		if s, entry := l.oapiEntry(at, e); s != nil {
			s.instances = append(s.instances, goOapiInstance{at: at, call: e, entry: entry, span: span})
		}
	}
	l.oapiFind(at, e.Callee, span)
	l.oapiFind(at, e.Object, span)
	l.oapiFind(at, e.Left, span)
	l.oapiFind(at, e.Right, span)
	for i := range e.Args {
		l.oapiFind(at, &e.Args[i], span)
	}
	for i := range e.Elements {
		l.oapiFind(at, &e.Elements[i].Value, span)
	}
}

// oapiEntry resolves a call to a generated server's entry function: F(...)
// in the server's package or pkg.F(...) with pkg importing it.
func (l *goLinker) oapiEntry(at goAt, e *facts.GoExpr) (*goOapiServer, string) {
	c := e.Callee
	if c == nil || goOapiArity[c.Name] == 0 {
		return nil, ""
	}
	var rec *goRecord
	switch c.Kind {
	case facts.GoIdentifier:
		if l.lookup(at, c.Name).kind != "func" {
			return nil, ""
		}
		rec = l.funcRecord(l.ownFiles(at.rec.file), c.Name)
	case facts.GoSelector:
		imp, ok := l.importOf(at, c.Object)
		if !ok || imp.files == nil {
			return nil, ""
		}
		rec = l.funcRecord(imp.files, c.Name)
	}
	if rec == nil || rec.parent != nil || rec.fn.Receiver != nil {
		return nil, ""
	}
	s := l.oapi[rec.file]
	if s == nil {
		return nil, ""
	}
	gin := c.Name == "RegisterHandlers" || c.Name == "RegisterHandlersWithOptions"
	if gin != (s.framework == goGin) {
		return nil, ""
	}
	return s, c.Name
}

// oapiRouterValue is the router a chi or net/http server's router local
// holds in HandlerWithOptions: the routers of every instance; a server no
// visible call instantiates registers on an object no root reaches
// (route_unmounted).
func (l *goLinker) oapiRouterValue(n goName, name string) (goValue, bool) {
	if n.rec == nil || n.rec.parent != nil {
		return goValue{}, false
	}
	s := l.oapi[n.rec.file]
	if s == nil || s.entry != n.rec || s.router != name {
		return goValue{}, false
	}
	if s.valueDone {
		return s.value, true
	}
	s.valueDone = true
	s.value = goUnproven()
	l.tag("GR-G1")
	if len(s.instances) == 0 {
		fw := s.framework
		s.value = goObjects(l.object(goObject{id: "oapi:" + s.file, framework: fw, kind: "param", file: s.file, span: s.entry.fn.Span}))
		return s.value, true
	}
	for i, inst := range s.instances {
		v := l.oapiInstanceRouter(s, inst)
		if i == 0 {
			s.value = v
		} else {
			s.value = merge(s.value, v)
		}
	}
	return s.value, true
}

// oapiCall is the value of a call of a chi or net/http server's entry
// function: the router it registers on and returns.
func (l *goLinker) oapiCall(at goAt, e *facts.GoExpr) (goValue, bool) {
	if len(l.oapi) == 0 {
		return goValue{}, false
	}
	s, entry := l.oapiEntry(at, e)
	if s == nil || s.framework == goGin {
		return goValue{}, false
	}
	return l.oapiInstanceRouter(s, goOapiInstance{at: at, call: e, entry: entry}), true
}

// oapiInstanceRouter is the router one instance registers on: a new
// router (the ctor binding's object) or the BaseRouter it passes, which
// must be a name holding proven routers of the server's framework.
func (l *goLinker) oapiInstanceRouter(s *goOapiServer, inst goOapiInstance) goValue {
	refused := goValue{unproven: true, evidence: true}
	base, ok := l.oapiBaseRouter(inst)
	if !ok {
		return refused
	}
	if base == nil {
		return l.bindingValue(s.entry, s.ctor, s.router)
	}
	if base.Kind != facts.GoIdentifier {
		return refused
	}
	v := copied(l.eval(inst.at, base, "oapi"))
	if !v.proven() || l.framework(v) != s.framework {
		return refused
	}
	return v
}

// oapiBaseRouter is the BaseRouter expression of a chi or net/http
// instance: nil when the instance makes a new router; ok is false for
// options that are no composite literal or a wrong argument count.
func (l *goLinker) oapiBaseRouter(inst goOapiInstance) (*facts.GoExpr, bool) {
	args := inst.call.Args
	if len(args) != goOapiArity[inst.entry] {
		return nil, false
	}
	switch inst.entry {
	case "Handler":
		l.tag("GR-G1/`Handler(si)`")
	case "HandlerFromMux", "HandlerFromMuxWithBaseURL":
		l.tag("GR-G1/`HandlerFromMux(si, r)`")
		return &args[1], true
	case "HandlerWithOptions":
		o := &args[1]
		if o.Kind != facts.GoComposite {
			return nil, false
		}
		l.tag("GR-G1/`HandlerWithOptions(si, o)`")
		return goElement(o, "BaseRouter"), true
	}
	return nil, true
}

// oapiReachCond is reachCond for the router local of HandlerWithOptions:
// the template's `if r == nil` is decided per instance, so obj reaches
// the registrations conditionally only when every instance that carries
// it is in conditional context or carries it through conditional
// bindings (GR-C5).
func (l *goLinker) oapiReachCond(n goName, name, obj string) (cond, ok bool) {
	if n.rec == nil || n.rec.parent != nil {
		return false, false
	}
	s := l.oapi[n.rec.file]
	if s == nil || s.entry != n.rec || s.router != name {
		return false, false
	}
	found, all := false, true
	for _, inst := range s.instances {
		if !goHas(l.oapiInstanceRouter(s, inst).objects, obj) {
			continue
		}
		found = true
		c := conditional(inst.at)
		if base, _ := l.oapiBaseRouter(inst); !c && base != nil {
			c = l.reachCond(inst.at, base, obj, map[string]bool{})
		}
		all = all && c
	}
	return found && all, true
}

// goElement is the value of a composite literal's keyed element name, nil
// when there is none.
func goElement(o *facts.GoExpr, name string) *facts.GoExpr {
	for i := range o.Elements {
		k := o.Elements[i].Key
		if k != nil && k.Kind == facts.GoIdentifier && k.Name == name {
			return &o.Elements[i].Value
		}
	}
	return nil
}

// oapiServerAt is the generated server whose entry function encloses at
// (the registering function or a chi Group callback in it).
func (l *goLinker) oapiServerAt(at goAt) *goOapiServer {
	if at.rec == nil || at.rec.decl == nil {
		return nil
	}
	s := l.oapi[at.rec.file]
	if s == nil || s.entry != at.rec.decl {
		return nil
	}
	return s
}

// oapiBase decides a server's BaseURL from its instances: the one constant
// every instance gives. An instance that gives another value or a value
// that is not constant leaves it unresolved, named in
// route_prefix_unresolved at that instance.
func (l *goLinker) oapiBase(s *goOapiServer) (string, bool, bool) {
	if s.baseDone {
		return s.base, s.baseLit, s.baseOK
	}
	s.baseDone = true
	s.baseOK, s.baseLit = true, true
	texts := make([]string, len(s.instances))
	for i, inst := range s.instances {
		value, literal, ok := "", true, true
		args := inst.call.Args
		var e *facts.GoExpr
		switch {
		case len(args) != goOapiArity[inst.entry]:
			ok = false
		case inst.entry == "HandlerFromMuxWithBaseURL":
			e = &args[2]
		case inst.entry == "HandlerWithOptions" || inst.entry == "RegisterHandlersWithOptions":
			o := &args[len(args)-1]
			if o.Kind != facts.GoComposite {
				ok = false
			} else {
				e = goElement(o, "BaseURL")
			}
		}
		if ok && e != nil {
			value, literal, ok = l.constant(inst.at, e)
		}
		texts[i] = "BaseURL \"\""
		if e != nil {
			texts[i] = "BaseURL " + goExprText(e)
		}
		if !ok || i > 0 && value != s.base {
			s.baseOK = false
		}
		if i == 0 {
			s.base = value
		}
		s.baseLit = s.baseLit && literal
	}
	if !s.baseOK {
		s.base = ""
		for i, inst := range s.instances {
			l.diagnostic(inst.at.rec.file, "route_prefix_unresolved", texts[i]+" (the generated server "+s.file+" has no one constant BaseURL for every call; no path is claimed through it)", inst.span)
		}
	}
	return s.base, s.baseLit, s.baseOK
}

// oapiPattern evaluates a generated registration's pattern: string
// literals, the server's BaseURL (options.BaseURL) and, for net/http, the
// http.Method* constant; ok is false for any other form. based reports
// that the BaseURL is resolved.
func (l *goLinker) oapiPattern(s *goOapiServer, at goAt, e *facts.GoExpr) (pattern string, literal, based, ok bool) {
	base, baseLit, baseOK := l.oapiBase(s)
	literal, based = true, baseOK
	var eval func(e *facts.GoExpr) (string, bool)
	eval = func(e *facts.GoExpr) (string, bool) {
		switch e.Kind {
		case facts.GoString:
			return e.Value, true
		case facts.GoBinary:
			if e.Value != "+" || e.Left == nil || e.Right == nil {
				return "", false
			}
			a, ok := eval(e.Left)
			if !ok {
				return "", false
			}
			b, ok := eval(e.Right)
			return a + b, ok
		case facts.GoSelector:
			if e.Object == nil || e.Object.Kind != facts.GoIdentifier {
				return "", false
			}
			if e.Name == "BaseURL" && e.Object.Name == "options" {
				if n := l.lookup(at, "options"); n.kind == "param" && n.rec == s.entry {
					literal = literal && baseLit
					return base, true
				}
				return "", false
			}
			if imp, ok := l.importOf(at, e.Object); ok && imp.framework == goNetHTTP && s.framework == goNetHTTP {
				m, ok := goMethods[e.Name]
				return m, ok
			}
		}
		return "", false
	}
	pattern, ok = eval(e)
	return pattern, literal, based, ok
}

// oapiHandler resolves a generated registration's handler wrapper.N: method
// N of the struct type every instance passes as si.
func (l *goLinker) oapiHandler(s *goOapiServer, h *facts.GoExpr) string {
	if h.Kind != facts.GoSelector || h.Object == nil || h.Object.Kind != facts.GoIdentifier || h.Object.Name != "wrapper" {
		return ""
	}
	if !s.typeDone {
		s.typeDone = true
		for i, inst := range s.instances {
			files, name := l.oapiSIType(s, inst)
			if name == "" || i > 0 && (name != s.typeName || strings.Join(files, "\x00") != strings.Join(s.typeFiles, "\x00")) {
				s.typeFiles, s.typeName = nil, ""
				break
			}
			s.typeFiles, s.typeName = files, name
		}
	}
	if s.typeName == "" {
		return ""
	}
	return l.callable(s.typeFiles, s.typeName+"."+h.Name, "method")
}

// oapiSIType is the module struct type an instance's si argument states:
// a name typed as GR-H2 types a receiver, a composite literal T{...} or
// &T{...}, or a call of a module function with one declared result.
func (l *goLinker) oapiSIType(s *goOapiServer, inst goOapiInstance) ([]string, string) {
	args := inst.call.Args
	if len(args) != goOapiArity[inst.entry] {
		return nil, ""
	}
	si := &args[0]
	if s.framework == goGin {
		si = &args[1]
	}
	var t *facts.GoExpr
	file := inst.at.rec.file
	switch {
	case si.Kind == facts.GoIdentifier:
		t, file = l.typeOf(inst.at, si)
	case si.Kind == facts.GoUnary && si.Value == "&" && si.Object != nil && si.Object.Kind == facts.GoComposite:
		t = si.Object.Type
	case si.Kind == facts.GoComposite:
		t = si.Type
	case si.Kind == facts.GoCall && si.Callee != nil:
		t, file = l.signature(inst.at, si.Callee)
	}
	if t == nil {
		return nil, ""
	}
	files, name := l.typeFiles(file, t)
	if name == "" {
		return nil, ""
	}
	for _, p := range files {
		if g := l.files[p].Go; g != nil {
			for _, st := range g.Structs {
				if st.Name == name {
					return files, name
				}
			}
		}
	}
	return nil, ""
}

// oapiRegistration reads the pattern of a registration made in a generated
// server's registering function; handled reports that the registration is
// one (the caller then skips GR-P1).
func (l *goLinker) oapiRegistration(at goAt, reg *goRegistration, arg *facts.GoExpr) bool {
	s := l.oapiServerAt(at)
	if s == nil {
		return false
	}
	pattern, literal, based, ok := l.oapiPattern(s, at, arg)
	if !ok {
		return false
	}
	l.tag("GR-G1")
	l.tag("GR-G1/" + map[string]string{goChi: "chi server", goGin: "gin server", goNetHTTP: "net/http server"}[s.framework])
	reg.oapi = s
	reg.resolution = "constant"
	if literal {
		reg.resolution = "literal"
	}
	reg.pattern, reg.name = pattern, pattern
	if !based {
		reg.noPath = goOapiUnresolved
	}
	return true
}

// goOapiUnresolved marks a generated registration whose BaseURL is not
// resolved: the route exists without a path (route_prefix_unresolved names
// the instance).
const goOapiUnresolved = "oapi_base_unresolved"

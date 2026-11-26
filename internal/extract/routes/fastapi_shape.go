package routes

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/python"
)

// This file builds the route shape of FastAPI routes (T029,
// rows SH-F1..SH-F35 of docs/contract/claims/route-shape-fastapi.md, model
// rows SM-* of docs/contract/claims/route-shape.md). It reads the cached
// shape syntax (facts.PythonShape) at link time and follows the handler's
// dependencies, request object and returned values through proven call
// targets with routeshape.Follow, so a new depth limit needs no re-parse.

// shapeFunc is one function whose shape syntax is cached.
type shapeFunc struct {
	file  string
	scope int
	fn    *facts.PythonShapeFunction
	step  routeshape.Step
	// method is true for a function defined in a class body.
	method bool
}

// fastapiShapes is the route-independent state of shape building.
type fastapiShapes struct {
	files   map[string]facts.Facts
	modules *python.ModuleResolver
	sec     *securityLinker
	types   *typeContext
	depth   int
	funcs   map[string]*shapeFunc // by step key
	byScope map[string]*shapeFunc // by file:scope
	// lines maps a function identity to its definition line, for proven
	// targets in files without shape syntax.
	lines map[string]int
	// Project-level registrations (rows SH-F37, SH-F38), as reasons:
	// exception handlers that can catch HTTPException (all statuses, or
	// by status code) or RequestValidationError, and route_class
	// assignments.
	handlerHTTP       []string
	handlerStatus     map[string][]string
	handlerValidation []string
	routeClassSet     []string
	// responseClassSet are `x.default_response_class = ...` assignments
	// (row SH-F24): the object and the order are not modelled, so every
	// route's default success status and content type are not read.
	responseClassSet []string
}

func stepKey(s routeshape.Step) string {
	return s.File + "\x00" + strconv.Itoa(s.Line) + "\x00" + s.Function
}

func newFastAPIShapes(files map[string]facts.Facts, modules *python.ModuleResolver, sec *securityLinker, depth int) *fastapiShapes {
	l := &fastapiShapes{files: files, modules: modules, sec: sec, types: newTypeContext(files, modules, sec), depth: depth,
		funcs: map[string]*shapeFunc{}, byScope: map[string]*shapeFunc{}, lines: map[string]int{}}
	for _, file := range facts.Paths(files) {
		f := files[file]
		for _, n := range f.Nodes {
			if (n.Kind == "function" || n.Kind == "method") && n.Span != nil {
				l.lines[n.Identity] = n.Span.StartLine
			}
		}
		p := f.Python
		if p == nil || p.Shape == nil || !p.ShapeValid() {
			continue
		}
		for _, h := range p.Shape.Handlers {
			l.handler(file, h)
		}
		for i := range p.Shape.Functions {
			fn := &p.Shape.Functions[i]
			s := p.Scopes[fn.Scope]
			sf := &shapeFunc{file: file, scope: fn.Scope, fn: fn, step: routeshape.Step{File: file, Line: fn.Line, Function: s.Name}}
			if sf.step.Function == "" {
				sf.step.Function = "<function>"
			}
			if s.Parent >= 0 && s.Parent < len(p.Scopes) && p.Scopes[s.Parent].Class {
				sf.method = true
			}
			l.funcs[stepKey(sf.step)] = sf
			l.byScope[fmt.Sprintf("%s:%d", file, fn.Scope)] = sf
		}
	}
	return l
}

// validationErrors are the exception classes whose handler replaces
// FastAPI's 422 response (RequestValidationError and its base; pydantic's
// ValidationError is counted too: precision-safe).
var validationErrors = map[string]bool{
	"fastapi.exceptions.RequestValidationError": true, "fastapi.RequestValidationError": true,
	"fastapi.exceptions.ValidationException": true, "pydantic.ValidationError": true, "pydantic_core.ValidationError": true,
	"pydantic.error_wrappers.ValidationError": true,
}

// builtinExceptions are builtin exception names: their handlers never
// catch HTTPException or RequestValidationError (Starlette looks the
// handler up along the raised class's MRO, which meets HTTPException's own
// handler first).
var builtinExceptions = map[string]bool{
	"Exception": true, "BaseException": true, "ValueError": true, "KeyError": true, "TypeError": true,
	"RuntimeError": true, "LookupError": true, "PermissionError": true, "NotImplementedError": true,
	"AssertionError": true, "OSError": true, "TimeoutError": true, "ConnectionError": true,
	"FileNotFoundError": true, "IndexError": true, "AttributeError": true,
}

// unpackedApp records a FastAPI constructor with keyword unpacking (row
// SH-F37): it may pass exception_handlers, which replace the HTTPException
// and RequestValidationError handlers of every route that app serves.
// Like the other registrations it applies project-wide: an app serves
// routes also through mounted routers and copied route lists, which the
// include graph does not show.
func (l *fastapiShapes) unpackedApp(where string) {
	tagShape("SH-F37/**options")
	text := "keyword unpacking on FastAPI may pass exception_handlers (" + where + ")"
	l.handlerHTTP = append(l.handlerHTTP, text)
	l.handlerValidation = append(l.handlerValidation, text)
}

// handler reads one project-level registration (rows SH-F37, SH-F38).
func (l *fastapiShapes) handler(file string, h facts.PythonShapeHandler) {
	where := fmt.Sprintf("%s:%d", file, h.Line)
	if h.Kind == "route_class" {
		l.routeClassSet = append(l.routeClassSet, "route_class is assigned ("+where+")")
		return
	}
	if h.Kind == "default_response_class" {
		l.responseClassSet = append(l.responseClassSet, "default_response_class is assigned ("+where+")")
		return
	}
	both := func(why string) {
		text := "an exception handler for " + why + " (" + where + ")"
		l.handlerHTTP = append(l.handlerHTTP, text)
		l.handlerValidation = append(l.handlerValidation, text)
	}
	if h.Key == nil {
		both("a class not read")
		return
	}
	switch k := *h.Key; k.Kind {
	case "integer":
		if n, err := strconv.Atoi(k.Name); err == nil && n >= 100 && n <= 599 {
			if l.handlerStatus == nil {
				l.handlerStatus = map[string][]string{}
			}
			l.handlerStatus[k.Name] = append(l.handlerStatus[k.Name], "an exception handler for status "+k.Name+" ("+where+")")
			return
		}
		both(k.Name)
	case "identifier", "attribute":
		b := l.modules.Expression(file, h.Scope, k)
		switch {
		case b.External:
			name := fullName(b)
			switch {
			case httpExceptions[name]:
				l.handlerHTTP = append(l.handlerHTTP, "an exception handler for "+name+" ("+where+")")
			case validationErrors[name]:
				l.handlerValidation = append(l.handlerValidation, "an exception handler for "+name+" ("+where+")")
			}
		case b.File == "" && b.Module == nil && k.Kind == "identifier" && builtinExceptions[k.Name]:
		case b.File != "" && b.Module == nil && b.Syntax.Value.Kind == "class_definition":
			// A repository class: its handler catches its own instances,
			// never a raise of FastAPI's classes.
		default:
			both(describe(k))
		}
	default:
		both(describe(k))
	}
}

// lowerAll lowers every fact of the route's request and response sections
// to intended and leaves them partial (row SH-F38: a custom route class
// can rewrite the request and the response).
func (r *routeBuild) lowerAll(reason string) {
	sh := r.shape
	lower := func(sec *routeshape.Section) {
		if sec.State == routeshape.NotRead {
			return
		}
		if sec.State != routeshape.Partial {
			sec.State, sec.Reason, sec.Evidence = routeshape.Partial, reason, nil
		} else {
			sec.Reason += "; " + reason
		}
	}
	for _, ps := range []*routeshape.ParamSection{&sh.Path, &sh.Query, &sh.Header, &sh.Cookie} {
		for i := range ps.Items {
			if p := &ps.Items[i]; p.Provenance.Kind == routeshape.Fact {
				p.Provenance.Kind = routeshape.Intended
				if p.Presence == routeshape.Required {
					p.Presence = routeshape.PresenceUnknown
				}
				unrequire(p.Schema)
			}
		}
		lower(&ps.Section)
	}
	for i := range sh.Body.Items {
		if c := &sh.Body.Items[i]; c.Provenance.Kind == routeshape.Fact {
			c.Provenance.Kind = routeshape.Intended
			unrequire(c.Schema)
		}
	}
	lower(&sh.Body.Section)
	for i := range sh.Responses.Items {
		if c := &sh.Responses.Items[i]; c.Provenance.Kind == routeshape.Fact {
			c.Provenance.Kind = routeshape.Intended
			unrequire(c.Schema)
		}
	}
	lower(&sh.Responses.Section)
}

// unrequire turns the required fields of an inline schema lowered to
// intended into presence unknown (row SM-14: required only from fact).
func unrequire(s *routeshape.Schema) {
	if s == nil {
		return
	}
	for i := range s.Fields {
		if s.Fields[i].Presence == routeshape.Required {
			s.Fields[i].Presence = routeshape.PresenceUnknown
		}
		unrequire(&s.Fields[i].Schema)
	}
	unrequire(s.Items)
}

// shapeInput is what LinkFastAPI knows about one registration.
type shapeInput struct {
	file      string
	syntax    facts.PythonRoute
	handler   string
	templates []string // raw templates (prefix + pattern) before convertors are stripped
	chains    []dependencyList
	own       dependencyList
	security  map[string]any
	// pattern is the registration's own path pattern.
	pattern string
	// routeClass is the registration object's route_class reason.
	routeClass string
	// responseClass and responseStatus say why the success content type
	// and the default success status are not read: the default response
	// classes the route can inherit (row SH-F24, LinkFastAPI's
	// inheritedDefaults).
	responseClass, responseStatus []string
}

// routeBuild is the state of one route's shape.
type routeBuild struct {
	l       *fastapiShapes
	in      shapeInput
	handler *shapeFunc
	shape   *routeshape.Shape
	params  map[string][]routeshape.Param // by section
	reasons map[string][]string           // partial reasons by section
	body    []bodyParam
	rawBody []routeshape.Content
	// declared is true when a declared parameter or body exists (422);
	// canFail when one of them can fail validation (row SH-F30).
	declared   bool
	canFail    bool
	responses  []routeshape.Response
	statusSeen map[string]bool
	returned   []routeshape.Response
	refs       []string
	pathNames  map[string]bool
	scopes     bool // a Security() with scopes= was seen
	// override says why the declared success response may be bypassed
	// (a Response returned directly, a Response parameter changed).
	override []string
	values   map[string]string
}

// bodyParam is one parameter FastAPI reads from the body.
type bodyParam struct {
	name      string
	presence  routeshape.Presence
	schema    routeshape.Schema
	marker    string
	embed     bool
	mediaType string
	file      bool
	// formModel: a Form() parameter typed with a model (row SH-F20).
	formModel bool
	prov      routeshape.Provenance
	row       string
}

func (r *routeBuild) reason(section, text string) {
	for _, s := range r.reasons[section] {
		if s == text {
			return
		}
	}
	r.reasons[section] = append(r.reasons[section], text)
}

// paramSections are the sections a parameter can land in.
var paramSections = []string{routeshape.SectionPath, routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody}

var templateParam = regexp.MustCompile(`\{([^}:]+)(?::([^}]*))?\}`)

// build returns the route's shape attribute value, or a diagnostic.
func (l *fastapiShapes) build(in shapeInput) (map[string]any, *routeshape.Shape, *contract.Diagnostic) {
	r := &routeBuild{l: l, in: in, shape: routeshape.New(l.depth), params: map[string][]routeshape.Param{}, reasons: map[string][]string{},
		statusSeen: map[string]bool{}, pathNames: map[string]bool{}, values: map[string]string{}}
	if in.handler != "" {
		if fs, ok := l.sec.functions[in.handler]; ok {
			r.handler = l.byScope[fmt.Sprintf("%s:%d", fs.file, fs.scope)]
		}
	}
	for _, t := range append(append([]string{}, in.templates...), in.pattern) {
		for _, m := range templateParam.FindAllStringSubmatch(t, -1) {
			r.pathNames[m[1]] = true
		}
	}
	if _, _, ok := r.templateParams(); !ok && len(in.templates) > 0 {
		for _, s := range paramSections {
			r.reason(s, "the route is mounted under prefixes with different path parameters")
		}
	}
	if v, ok := keyword(in.syntax.Expression, "include_in_schema"); ok && v.Kind == "false" {
		tagShape("SH-F34")
	}
	r.security()
	if r.handler != nil {
		r.follow()
		r.assemble()
		var custom []string
		if in.routeClass != "" {
			custom = append(custom, "a custom "+in.routeClass+" can rewrite the request and the response")
		}
		for _, why := range l.routeClassSet {
			custom = append(custom, "a custom "+why+" can rewrite the request and the response")
		}
		if len(custom) > 0 {
			tagShape("SH-F38")
			r.lowerAll(strings.Join(custom, "; "))
		}
	}
	value, err := routeshape.ToAttribute(r.shape)
	if err != nil {
		return nil, nil, &contract.Diagnostic{Type: "diagnostic", File: in.file, Severity: "warning", Code: "route_shape_invalid", Message: err.Error()}
	}
	return value, r.shape, nil
}

// templates returns the template parameters (name -> convertor) when every
// path of the route has the same ones.
func (r *routeBuild) templateParams() (map[string]string, []string, bool) {
	if len(r.in.templates) == 0 {
		return nil, nil, false
	}
	var first map[string]string
	var order []string
	for i, t := range r.in.templates {
		params := map[string]string{}
		var names []string
		for _, m := range templateParam.FindAllStringSubmatch(t, -1) {
			params[m[1]] = m[2]
			names = append(names, m[1])
		}
		if i == 0 {
			first, order = params, names
			continue
		}
		if len(params) != len(first) {
			return nil, nil, false
		}
		for k, v := range params {
			if w, ok := first[k]; !ok || w != v {
				return nil, nil, false
			}
		}
	}
	return first, order, true
}

// value encoding of the followed values: the kind, then for request
// values the parameter and whether a dependency owns it; a leading "!"
// marks a chain that passed a call inside a try body.
type followValue struct {
	kind    string // dep, ret, req (request object), or a mapping attribute
	param   string
	dep     bool
	tainted bool
}

func (v followValue) String() string {
	s := v.kind
	if v.param != "" {
		s += ":" + v.param
		if v.dep {
			s += ":d"
		} else {
			s += ":h"
		}
	}
	if v.tainted {
		s = "!" + s
	}
	return s
}

func parseValue(s string) followValue {
	var v followValue
	if strings.HasPrefix(s, "!") {
		v.tainted = true
		s = s[1:]
	}
	parts := strings.Split(s, ":")
	v.kind = parts[0]
	if len(parts) == 3 {
		v.param = parts[1]
		v.dep = parts[2] == "d"
	}
	return v
}

// follow runs the route's two Follows: the parameters (the dependency
// tree from the handler, value dep, and the request object read in it)
// and the responses (the handler's returned values, value ret).
func (r *routeBuild) follow() {
	r.followOne(followValue{kind: "dep"})
	r.followOne(followValue{kind: "ret"})
}

func (r *routeBuild) followOne(startValue followValue) {
	start := routeshape.Frame{Function: r.handler.step, Value: startValue.String()}
	res, err := routeshape.Follow(context.Background(), start, r.l.depth, routeshape.ResolverFunc(r.visit))
	if err != nil {
		for _, s := range append(append([]string{}, paramSections...), routeshape.SectionResponses) {
			r.reason(s, err.Error())
		}
		return
	}
	values := map[string]string{}
	for _, f := range res.Frames {
		values[stepsKey(f.Chain)] = f.Value
	}
	for _, c := range res.Chains {
		// The chain ends in the frame whose chain is its steps; a stop at
		// a call takes the kind of the value the call carried.
		v := parseValue(values[stepsKey(c.Steps)])
		if c.At != nil {
			if kind, ok := r.values[siteKey(c.At.File, c.At.Line)]; ok {
				v = parseValue(kind)
			}
		}
		section := routeshape.SectionQuery
		switch v.kind {
		case "ret":
			section = routeshape.SectionResponses
		case "req", "headers":
			section = routeshape.SectionHeader
		case "cookies":
			section = routeshape.SectionCookie
		case "query_params":
			section = routeshape.SectionQuery
		case "path_params":
			section = routeshape.SectionPath
		}
		if c.Stop != routeshape.StopEnded {
			tagShape("SH-F35/" + string(c.Stop))
		}
		rec := routeshape.Records(section, routeshape.Result{DepthLimit: res.DepthLimit, Chains: []routeshape.Chain{c}})
		r.shape.Follow.Chains = append(r.shape.Follow.Chains, rec...)
		switch c.Stop {
		case routeshape.StopEnded, routeshape.StopJoined, routeshape.StopCycle:
			continue
		}
		detail := string(c.Stop)
		if c.Detail != "" {
			detail += ": " + c.Detail
		}
		at := ""
		if c.At != nil {
			at = fmt.Sprintf(" at %s:%d", c.At.File, c.At.Line)
		}
		text := fmt.Sprintf("a call was not followed (%s)%s", detail, at)
		switch v.kind {
		case "dep":
			for _, s := range paramSections {
				r.reason(s, text)
			}
		case "ret":
			r.reason(routeshape.SectionResponses, text)
		default:
			r.escape(v, text)
		}
	}
}

func siteKey(file string, line int) string { return file + ":" + strconv.Itoa(line) }

// mark remembers the value kind a call carries, for its chain record.
func (r *routeBuild) mark(c routeshape.Call, v followValue) routeshape.Call {
	r.values[siteKey(c.Site.File, c.Site.Line)] = v.String()
	return c
}

func stepsKey(steps []routeshape.Step) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(stepKey(s))
		b.WriteString("\x01")
	}
	return b.String()
}

// visit is the Resolver of the route's Follow.
func (r *routeBuild) visit(ctx context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	sf := r.l.funcs[stepKey(frame.Function)]
	if sf == nil {
		return nil, fmt.Errorf("the body of %s is not read: its file imports none of fastapi, starlette, pydantic and sqlmodel", frame.Function.Function)
	}
	v := parseValue(frame.Value)
	switch v.kind {
	case "dep":
		calls := r.dependency(sf, frame, v)
		for i := range calls {
			if calls[i].Value == "" {
				r.mark(calls[i], v)
			}
		}
		return calls, nil
	case "ret":
		if frame.Depth == 0 {
			return r.handlerReturns(sf, frame), nil
		}
		return r.returns(sf, frame, v), nil
	default:
		return r.reads(sf, frame, v), nil
	}
}

// enclosing is the scope annotations of a function are evaluated in.
func (r *routeBuild) enclosing(sf *shapeFunc) int {
	p := r.l.files[sf.file].Python
	return p.Scopes[sf.scope].Parent
}

// prov is a provenance in a frame.
func prov(kind routeshape.Kind, rule, file string, line int, frame routeshape.Frame) routeshape.Provenance {
	chain := append([]routeshape.Step{}, frame.Chain...)
	return routeshape.Provenance{Kind: kind, Rule: rule, File: file, Line: line, Chain: chain, Confidence: 1}
}

// dependency reads one function of the dependency tree (the handler at
// depth 0): its parameters, raises and, for the handler, the route's
// dependencies= lists and returned values.
func (r *routeBuild) dependency(sf *shapeFunc, frame routeshape.Frame, v followValue) []routeshape.Call {
	var calls []routeshape.Call
	if frame.Depth == 0 {
		for _, list := range r.in.chains {
			calls = append(calls, r.dependencyList(list, frame)...)
		}
		calls = append(calls, r.dependencyList(r.in.own, frame)...)
	}
	params := sf.fn.Params
	if frame.Depth > 0 && sf.method && len(params) > 0 {
		// A dependency class's __init__: self is not a parameter.
		params = params[1:]
	}
	for _, p := range params {
		calls = append(calls, r.parameter(sf, p, frame)...)
	}
	r.raises(sf, frame, v)
	return calls
}

// dependencyList follows the Depends items of a dependencies= list (row
// SH-F12): their parameters join the route's.
func (r *routeBuild) dependencyList(list dependencyList, frame routeshape.Frame) []routeshape.Call {
	var calls []routeshape.Call
	for _, u := range list.unknown {
		for _, s := range paramSections {
			r.reason(s, "a dependencies= list is not read: "+u.Reason)
		}
	}
	for _, ref := range list.refs {
		tagShape("SH-F12")
		line := 1
		if ref.span != nil {
			line = ref.span.StartLine
		}
		site := routeshape.Step{File: ref.file, Line: line, Function: compactCall(ref.expr)}
		if ref.expr.Kind != "call" || r.l.sec.marker(ref.file, ref.scope, ref.expr) != "depends" {
			for _, s := range paramSections {
				r.reason(s, "a dependencies= item is not a Depends call")
			}
			continue
		}
		target, ok := argumentOf(ref.expr, "dependency", 0)
		if !ok || target.Kind == "none" {
			for _, s := range paramSections {
				r.reason(s, "Depends() without a callable in a dependencies= list")
			}
			continue
		}
		r.checkScopes(ref.expr)
		if c, ok := r.dependencyTarget(ref.file, ref.scope, target, site, frame); ok {
			calls = append(calls, c)
		}
	}
	return calls
}

func compactCall(e facts.RouteExpr) string {
	t := e.Text
	if t == "" {
		t = describe(e)
	}
	if len(t) > 80 {
		t = t[:77] + "..."
	}
	return t
}

// checkScopes records a Security(..., scopes=...) declaration: the
// requirement's scopes are not read (row SH-F33's refusal).
func (r *routeBuild) checkScopes(call facts.RouteExpr) {
	for _, k := range call.Keywords {
		if k.Name == "scopes" {
			r.scopes = true
		}
	}
}

// dependencyTarget resolves a dependency callable: a repository function
// (followed), a repository class with __init__ (row SH-F10), a security
// scheme instance (row SH-F13), the OAuth2 password form (row SH-F11), or
// a stop.
func (r *routeBuild) dependencyTarget(file string, scope int, target facts.RouteExpr, site routeshape.Step, frame routeshape.Frame) (routeshape.Call, bool) {
	stop := func(s routeshape.Stop, detail string) (routeshape.Call, bool) {
		return routeshape.Call{Site: site, Stop: s, Detail: detail}, true
	}
	if target.Kind != "identifier" && target.Kind != "attribute" {
		return stop(routeshape.StopUnresolved, "dependency expression "+describe(target)+" is not read")
	}
	b := r.l.modules.Expression(file, scope, target)
	switch {
	case b.External:
		name := fullName(b)
		switch name {
		case "fastapi.security.OAuth2PasswordRequestForm", "fastapi.security.oauth2.OAuth2PasswordRequestForm":
			r.passwordForm(false, site, frame)
			return routeshape.Call{}, false
		case "fastapi.security.OAuth2PasswordRequestFormStrict", "fastapi.security.oauth2.OAuth2PasswordRequestFormStrict":
			r.passwordForm(true, site, frame)
			return routeshape.Call{}, false
		}
		return stop(routeshape.StopExternal, name)
	case b.File == "" || b.Module != nil:
		return stop(routeshape.StopUnresolved, describe(target))
	case b.Syntax.Count != 1:
		return stop(routeshape.StopAmbiguous, describe(target)+" is bound more than once")
	case b.Syntax.Value.Kind == "function_definition":
		tagShape("SH-F9")
		return r.proven(b, b.Syntax.ClassScope, site, followValue{kind: "dep"})
	case b.Syntax.Value.Kind == "class_definition":
		p := r.l.files[b.File].Python
		if p == nil || b.Syntax.ClassScope >= len(p.Scopes) {
			return stop(routeshape.StopUnresolved, describe(target))
		}
		init, ok := p.Scopes[b.Syntax.ClassScope].Bindings["__init__"]
		if !ok || init.Count != 1 || init.Value.Kind != "function_definition" {
			return stop(routeshape.StopUnresolved, "class "+b.Name+" without its own __init__ is not read")
		}
		tagShape("SH-F10")
		ib := python.Binding{File: b.File, Name: "__init__", Identity: init.Identity, Syntax: init}
		return r.proven(ib, init.ClassScope, site, followValue{kind: "dep"})
	case b.Scope == 0 && b.Syntax.Value.Kind == "call":
		if b.Syntax.Value.Object != nil {
			if class, sub, _ := r.l.sec.securityClass(b.File, 0, *b.Syntax.Value.Object, map[string]bool{}, 0); class != "" {
				if sub == "" {
					tagShape("SH-F13")
					return routeshape.Call{}, false
				}
				// A repository subclass: FastAPI calls its own __call__,
				// which reads what its code reads (SH-F13's refusal).
				tagShape("SH-F13/subclass")
				cb := r.l.modules.Expression(b.File, 0, *b.Syntax.Value.Object)
				if call, ok := r.l.method(cb, "__call__", map[string]bool{}); ok {
					return r.proven(call, call.Syntax.ClassScope, site, followValue{kind: "dep"})
				}
				return stop(routeshape.StopUnresolved, "repository subclass "+sub+" of "+class+" is not read")
			}
		}
	}
	return stop(routeshape.StopUnresolved, "dependency value "+describe(target)+" is not read")
}

// method finds a method defined once in a repository class or its
// repository bases (depth first, in base order); ok is false when a class
// on the way is not read.
func (l *fastapiShapes) method(b python.Binding, name string, seen map[string]bool) (python.Binding, bool) {
	if b.External || b.File == "" || b.Module != nil || b.Syntax.Value.Kind != "class_definition" || seen[b.File+"#"+b.Identity] {
		return python.Binding{}, false
	}
	seen[b.File+"#"+b.Identity] = true
	p := l.files[b.File].Python
	if p == nil || b.Syntax.ClassScope >= len(p.Scopes) {
		return python.Binding{}, false
	}
	if m, ok := p.Scopes[b.Syntax.ClassScope].Bindings[name]; ok {
		if m.Count != 1 || m.Value.Kind != "function_definition" {
			return python.Binding{}, false
		}
		return python.Binding{File: b.File, Name: name, Identity: m.Identity, Syntax: m}, true
	}
	for _, base := range b.Syntax.Bases {
		if base.Kind != "identifier" && base.Kind != "attribute" {
			continue
		}
		if m, ok := l.method(l.modules.Expression(b.File, b.Scope, base), name, seen); ok {
			return m, true
		}
	}
	return python.Binding{}, false
}

// proven returns the call to a proven repository function.
func (r *routeBuild) proven(b python.Binding, scope int, site routeshape.Step, v followValue) (routeshape.Call, bool) {
	sf := r.l.byScope[fmt.Sprintf("%s:%d", b.File, scope)]
	target := routeshape.Step{File: b.File, Line: r.l.lines[b.Identity], Function: b.Name}
	if sf != nil {
		target = sf.step
	} else if target.Line == 0 {
		return routeshape.Call{Site: site, Stop: routeshape.StopUnresolved, Detail: "the definition of " + b.Name + " is not found"}, true
	}
	return routeshape.Call{Site: site, Target: target, Value: v.String()}, true
}

// passwordForm adds the fields of FastAPI's OAuth2PasswordRequestForm
// (fastapi 0.161.1, row SH-F11): form fields of the body.
func (r *routeBuild) passwordForm(strict bool, site routeshape.Step, frame routeshape.Frame) {
	tagShape("SH-F11")
	str := routeshape.Schema{Type: routeshape.TypeString}
	null := routeshape.Schema{Type: routeshape.TypeString, Nullable: true}
	fields := []struct {
		name     string
		presence routeshape.Presence
		schema   routeshape.Schema
	}{
		{"grant_type", routeshape.Optional, null},
		{"username", routeshape.Required, str},
		{"password", routeshape.Required, str},
		{"scope", routeshape.Optional, str},
		{"client_id", routeshape.Optional, null},
		{"client_secret", routeshape.Optional, null},
	}
	if strict {
		fields[0].presence, fields[0].schema = routeshape.Required, str
	}
	for _, f := range fields {
		r.body = append(r.body, bodyParam{name: f.name, presence: f.presence, schema: f.schema, marker: "Form",
			prov: prov(routeshape.Fact, "SH-F11", site.File, site.Line, frame), row: "SH-F11"})
	}
	r.declared, r.canFail = true, true
}

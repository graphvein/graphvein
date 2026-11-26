package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Route shape of gin, chi and net/http routes (task T029;
// docs/contract/claims/route-shape-go.md, rows SH-G1..SH-G38 and D). The
// shape is built at link time from the cached request and response syntax
// of the handler (facts.GoFacts.Shapes) and of the functions it passes the
// request to, followed through proven call targets only
// (routeshape.Follow), up to the run's shape depth.

// Content types the rows claim.
const (
	goJSONType = "application/json"
	goTextType = "text/plain"
)

// goShapeTag, when set by a test, receives the row of every claim the
// shape rules make (rows of docs/contract/claims/route-shape-go.md); it
// is nil in production.
var goShapeTag func(row string)

func goShapeTagRow(row string) {
	if goShapeTag != nil {
		goShapeTag(row)
	}
}

// goShapeRef is one captured function: its file, record and the name its
// chain steps show.
type goShapeRef struct {
	file string
	fn   *facts.GoShapeFunc
	name string
}

// goShapeName is the name a chain step shows for a captured function.
func goShapeName(fn *facts.GoShapeFunc) string {
	switch {
	case fn.Name == "":
		return "func literal"
	case fn.Receiver != "":
		return fn.Receiver + "." + fn.Name
	}
	return fn.Name
}

func goShapeKey(file string, line int, name string) string {
	return file + "\x00" + strconv.Itoa(line) + "\x00" + name
}

// shapeRefs indexes every captured function of the run: declarations by
// file, line and qualified name, literals by file, line and column.
func (l *goLinker) shapeRefs() map[string]*goShapeRef {
	if l.shapeIndex != nil {
		return l.shapeIndex
	}
	l.shapeIndex = map[string]*goShapeRef{}
	for _, file := range facts.Paths(l.files) {
		g := l.files[file].Go
		if g == nil || g.Rejected || g.LoneCR {
			continue
		}
		for i := range g.Shapes {
			fn := &g.Shapes[i]
			ref := &goShapeRef{file: file, fn: fn, name: goShapeName(fn)}
			key := goShapeKey(file, fn.Line, ref.name)
			if fn.Name == "" {
				key = goShapeKey(file, fn.Line, "@"+strconv.Itoa(fn.Column))
			}
			if _, dup := l.shapeIndex[key]; dup {
				l.shapeIndex[key] = nil // two records, one key: none is read
				continue
			}
			l.shapeIndex[key] = ref
		}
	}
	return l.shapeIndex
}

// nodeOf finds a function or method node by identity.
func (l *goLinker) nodeOf(identity string) (string, *contract.Node) {
	if l.nodeIndex == nil {
		l.nodeIndex = map[string]goNodeRef{}
		for _, file := range facts.Paths(l.files) {
			nodes := l.files[file].Nodes
			for i := range nodes {
				if nodes[i].Kind == "function" || nodes[i].Kind == "method" {
					l.nodeIndex[nodes[i].Identity] = goNodeRef{file: file, node: &nodes[i]}
				}
			}
		}
	}
	r := l.nodeIndex[identity]
	return r.file, r.node
}

// goNodeRef is a node and its file.
type goNodeRef struct {
	file string
	node *contract.Node
}

// refOfNode is the captured function of a function or method node.
func (l *goLinker) refOfNode(identity string) *goShapeRef {
	file, n := l.nodeOf(identity)
	if n == nil || n.Span == nil {
		return nil
	}
	return l.shapeRefs()[goShapeKey(file, n.Span.StartLine, n.QualifiedName)]
}

// literalIn is the one captured function literal that starts inside a
// registration call (its handler literal); nil when there is none or
// several (a middleware literal beside it).
func (l *goLinker) literalIn(file string, sp contract.Span) *goShapeRef {
	g := l.files[file].Go
	if g == nil {
		return nil
	}
	var found *goShapeRef
	for i := range g.Shapes {
		fn := &g.Shapes[i]
		if fn.Name != "" {
			continue
		}
		line, col := fn.Line, fn.Column-1
		after := line > sp.StartLine || line == sp.StartLine && col >= sp.StartColumn
		before := line < sp.EndLine || line == sp.EndLine && col < sp.EndColumn
		if !after || !before {
			continue
		}
		if found != nil {
			return nil
		}
		found = l.shapeRefs()[goShapeKey(file, fn.Line, "@"+strconv.Itoa(fn.Column))]
		if found == nil {
			return nil
		}
	}
	return found
}

// goEnv is what a followed function receives: the parameters that hold
// the request values (by role), the literal arguments its caller passed
// ("s:" a string, "n:" a status code) and the types of values passed to
// parameters of an interface type. It is the frame's Value, encoded.
type goEnv struct {
	Carriers map[string]string    `json:"c,omitempty"`
	Lits     map[string]string    `json:"l,omitempty"`
	Types    map[string]goTypeRef `json:"t,omitempty"`
	// Unchecked: some caller up the chain ignores this function's error
	// result, so an error check here does not stop the request (B6).
	Unchecked bool `json:"u,omitempty"`
}

func (e goEnv) encode() string {
	data, err := json.Marshal(e)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func decodeEnv(v string) goEnv {
	var e goEnv
	_ = json.Unmarshal([]byte(v), &e)
	return e
}

// safeShape builds a registration's shape attribute; a failure records a
// warning and stores no shape, never stopping the route.
func (l *goLinker) safeShape(reg *goRegistration, handler string) (out map[string]any, shape *routeshape.Shape) {
	defer func() {
		if v := recover(); v != nil {
			l.diagnostic(reg.file, "route_shape_failed", fmt.Sprintf("%s %q (route shape: %v)", reg.method, reg.pattern, v), reg.call.Span)
			out, shape = nil, nil
		}
	}()
	if l.shapeDepth == 0 {
		return nil, nil
	}
	s := l.routeShape(reg, handler)
	if s == nil {
		return nil, nil
	}
	l.goSecurity(reg, s)
	attr, err := routeshape.ToAttribute(s)
	if err != nil {
		l.diagnostic(reg.file, "route_shape_failed", fmt.Sprintf("%s %q (%v)", reg.method, reg.pattern, err), reg.call.Span)
		return nil, nil
	}
	return attr, s
}

// routeShape builds one route's shape, nil when its handler is not a
// captured function.
func (l *goLinker) routeShape(reg *goRegistration, handler string) *routeshape.Shape {
	if reg.oapi != nil {
		// SH-G36 (H21): the committed spec is the shape source.
		goShapeTagRow("SH-G36")
		s := routeshape.New(l.shapeDepth)
		for _, sec := range []*routeshape.Section{&s.Path.Section, &s.Query.Section, &s.Header.Section, &s.Cookie.Section, &s.Body.Section, &s.Responses.Section} {
			sec.Reason = "generated from spec"
		}
		return s
	}
	var ref *goShapeRef
	switch {
	case reg.handler != nil && reg.handler.Kind == facts.GoFuncLit:
		ref = l.literalIn(reg.file, reg.call.Span)
	case handler != "":
		ref = l.refOfNode(handler)
	}
	if ref == nil {
		return nil
	}
	env := goEnv{Carriers: map[string]string{}}
	for _, p := range ref.fn.Params {
		if p.Carrier != "" && p.Name != "" && p.Name != "_" {
			env.Carriers[p.Name] = p.Carrier
		}
	}
	if len(env.Carriers) == 0 {
		return nil
	}
	col := newGoShapeCollector(l, reg)
	r := &goShapeResolver{l: l, col: col, refs: map[string]*goShapeRef{}}
	start := routeshape.Step{File: ref.file, Line: ref.fn.Line, Function: ref.name}
	r.refs[goStepKey(start)] = ref
	res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: start, Value: env.encode()}, l.shapeDepth, r)
	if err != nil {
		return nil
	}
	return col.build(res)
}

func goStepKey(s routeshape.Step) string { return goShapeKey(s.File, s.Line, s.Function) }

// goShapeResolver is the routeshape.Resolver of Go: it reads a captured
// function's calls into the collector and returns the proven calls that
// pass the request values on.
type goShapeResolver struct {
	l    *goLinker
	col  *goShapeCollector
	refs map[string]*goShapeRef
}

// Visit reads one function.
func (r *goShapeResolver) Visit(_ context.Context, fr routeshape.Frame) ([]routeshape.Call, error) {
	ref := r.refs[goStepKey(fr.Function)]
	if ref == nil {
		return nil, fmt.Errorf("function %s at %s:%d is not captured", fr.Function.Function, fr.Function.File, fr.Function.Line)
	}
	v := &goShapeVisit{r: r, l: r.l, col: r.col, ref: ref, fn: ref.fn, env: decodeEnv(fr.Value), frame: fr}
	return v.run(), nil
}

// goWriteEvent is one write to the http.ResponseWriter of a function, in
// source order: a status (code, "" when not read), the writer passed to a
// call, or a Content-Type set.
type goPendingStatus struct {
	event int
	block int
	resp  routeshape.Response
}

type goWriteEvent struct {
	block  int
	kind   string // set (gin c.Status), commit (status written), body (status and body written), pass, ct
	status string
	ct     string
}

// goShapeVisit reads one function for one frame.
type goShapeVisit struct {
	r     *goShapeResolver
	l     *goLinker
	col   *goShapeCollector
	ref   *goShapeRef
	fn    *facts.GoShapeFunc
	env   goEnv
	frame routeshape.Frame

	carriers  map[string]string
	locals    map[string]*facts.GoShapeLocal
	params    map[string]*facts.GoShapeParam
	converted map[*facts.GoExpr]bool
	convLocal map[string]bool
	// handled are the call nodes read as part of an outer call
	// (json.NewEncoder(w) of an Encode call).
	handled map[*facts.GoExpr]bool
	events  []goWriteEvent
	calls   []routeshape.Call
	// reassigned are the parameters the function assigns: a literal or
	// type its caller passed no longer holds.
	reassigned map[string]bool
	// pending are gin c.Status claims, kept only when no later write can
	// replace the status (row SH-G20).
	pending []goPendingStatus
	// sections the function contributed items to, in first-seen order.
	sections []string
}

func (v *goShapeVisit) run() []routeshape.Call {
	v.carriers = map[string]string{}
	for name, role := range v.env.Carriers {
		v.carriers[name] = role
	}
	v.params = map[string]*facts.GoShapeParam{}
	for i := range v.fn.Params {
		if n := v.fn.Params[i].Name; n != "" {
			v.params[n] = &v.fn.Params[i]
		}
	}
	v.locals = map[string]*facts.GoShapeLocal{}
	for i := range v.fn.Locals {
		v.locals[v.fn.Locals[i].Name] = &v.fn.Locals[i]
	}
	v.reassigned = map[string]bool{}
	for _, name := range v.fn.Reassigned {
		v.reassigned[name] = true
		if _, ok := v.carriers[name]; ok {
			// SH-G33: a reassigned request value is not read further.
			goShapeTagRow("SH-G33")
			delete(v.carriers, name)
			v.calls = append(v.calls, routeshape.Call{Site: routeshape.Step{File: v.ref.file, Line: v.fn.Line, Function: name}, Stop: routeshape.StopTransformed, Detail: fmt.Sprintf("%s is reassigned in %s", name, v.ref.name)})
		}
	}
	v.converted = map[*facts.GoExpr]bool{}
	v.handled = map[*facts.GoExpr]bool{}
	v.convLocal = map[string]bool{}
	for i := range v.fn.Calls {
		goWalkCalls(&v.fn.Calls[i].Call, func(n *facts.GoExpr) { v.conversion(n) })
	}
	for i := range v.fn.Calls {
		rec := &v.fn.Calls[i]
		goWalkCalls(&rec.Call, func(n *facts.GoExpr) { v.call(n, rec, n == &rec.Call) })
	}
	for _, p := range v.pending {
		if len(v.frame.Chain) > 0 {
			// The caller may write another status after the call.
			v.col.note(routeshape.SectionResponses, "a status set in a followed function may be replaced by its caller")
			continue
		}
		replaced := false
		for _, e := range v.events[p.event+1:] {
			if e.kind != "ct" && !v.returnsBefore(p.block, e.block) {
				replaced = true
			}
		}
		if !replaced {
			v.col.response(p.resp.Status, nil, nil, p.resp.Provenance)
			v.contributed(routeshape.SectionResponses)
		}
	}
	v.col.frameSections[goStepsKey(v.frame.Chain)] = v.sections
	return v.calls
}

// goWalkCalls calls f for every call node of e, outermost first.
func goWalkCalls(e *facts.GoExpr, f func(*facts.GoExpr)) {
	goWalkCallsDepth(e, f, 0)
}

// goWalkCallsDepth bounds the walk as the payload bounds its trees.
func goWalkCallsDepth(e *facts.GoExpr, f func(*facts.GoExpr), depth int) {
	if e == nil || depth > facts.GoMaxDepth {
		return
	}
	if e.Kind == facts.GoCall {
		f(e)
	}
	goWalkCallsDepth(e.Callee, f, depth+1)
	goWalkCallsDepth(e.Object, f, depth+1)
	goWalkCallsDepth(e.Left, f, depth+1)
	goWalkCallsDepth(e.Right, f, depth+1)
	for i := range e.Args {
		goWalkCallsDepth(&e.Args[i], f, depth+1)
	}
	for i := range e.Elements {
		goWalkCallsDepth(e.Elements[i].Key, f, depth+1)
		goWalkCallsDepth(&e.Elements[i].Value, f, depth+1)
	}
}

// goConversions are the strconv functions that parse a read value (row
// SH-G4).
var goConversions = map[string]bool{"Atoi": true, "ParseInt": true, "ParseUint": true, "ParseFloat": true, "ParseBool": true}

// conversion marks the read a strconv conversion parses.
func (v *goShapeVisit) conversion(n *facts.GoExpr) {
	c := n.Callee
	if c == nil || c.Kind != facts.GoSelector || !goConversions[c.Name] || len(n.Args) == 0 || c.Object == nil || c.Object.Kind != facts.GoIdentifier || v.pkg(c.Object.Name) != "strconv" {
		return
	}
	a := &n.Args[0]
	switch a.Kind {
	case facts.GoCall:
		v.converted[a] = true
	case facts.GoIdentifier:
		v.convLocal[a.Name] = true
	}
}

// local is a name the function declares once (its value is the one
// declared).
func (v *goShapeVisit) local(name string) *facts.GoShapeLocal {
	if _, ok := v.params[name]; ok {
		return nil
	}
	l := v.locals[name]
	if l == nil || l.Count != 1 {
		return nil
	}
	return l
}

// pkg is the import path a name binds in the function's file, "" when it
// is a parameter, a local or no import.
func (v *goShapeVisit) pkg(name string) string {
	if _, ok := v.params[name]; ok || name == v.fn.ReceiverName {
		return ""
	}
	if _, ok := v.locals[name]; ok {
		return ""
	}
	return goImportPath(v.l, v.ref.file, name)
}

// carrier is the role of a value expression that holds a request value:
// a carrier parameter, gin's c.Request (a *http.Request) or c.Writer (an
// http.ResponseWriter), or a local declared once from one (row SH-G35).
func (v *goShapeVisit) carrier(e *facts.GoExpr) string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case facts.GoIdentifier:
		if role, ok := v.carriers[e.Name]; ok {
			return role
		}
		if l := v.local(e.Name); l != nil && l.Value != nil && l.Value.Kind == facts.GoSelector {
			return v.carrier(l.Value)
		}
	case facts.GoSelector:
		if v.carrier(e.Object) == facts.GoCarrierGin {
			switch e.Name {
			case "Request":
				goShapeTagRow("SH-G35")
				return facts.GoCarrierRequest
			case "Writer":
				goShapeTagRow("SH-G35")
				return facts.GoCarrierWriter
			}
		}
	}
	return ""
}

// site is a provenance for the current frame.
func (v *goShapeVisit) prov(kind routeshape.Kind, rule string, rec *facts.GoShapeCall) routeshape.Provenance {
	goShapeTagRow(rule)
	return routeshape.Provenance{Kind: kind, Rule: rule, File: v.ref.file, Line: rec.Line, Chain: append([]routeshape.Step{}, v.frame.Chain...), Confidence: 1}
}

func (v *goShapeVisit) contributed(section string) {
	for _, s := range v.sections {
		if s == section {
			return
		}
	}
	v.sections = append(v.sections, section)
}

// name reads a name argument: a string literal, a package-level string
// constant of the file's package, or a parameter the caller passed a
// literal to (row SH-G34).
func (v *goShapeVisit) name(e *facts.GoExpr) (string, bool) {
	if e == nil {
		return "", false
	}
	switch e.Kind {
	case facts.GoString:
		return e.Value, true
	case facts.GoIdentifier:
		if _, ok := v.params[e.Name]; ok {
			if lit, ok := v.env.Lits[e.Name]; ok && !v.reassigned[e.Name] && strings.HasPrefix(lit, "s:") {
				goShapeTagRow("SH-G34")
				return lit[2:], true
			}
			return "", false
		}
		if _, ok := v.locals[e.Name]; ok {
			return "", false
		}
		if n := v.l.packageName(v.ref.file, e.Name); n.kind == "package" && n.pkgConst != nil && n.pkgConst.Value.Kind == facts.GoString {
			goShapeTagRow("SH-G34")
			return n.pkgConst.Value.Value, true
		}
	}
	return "", false
}

// status reads a status argument (row SH-G25): an integer literal, a
// net/http Status constant, or a parameter the caller passed one to.
func (v *goShapeVisit) status(e *facts.GoExpr) (string, bool) {
	if e == nil {
		return "", false
	}
	code := ""
	switch e.Kind {
	case facts.GoNumber:
		goShapeTagRow("SH-G25")
		code = e.Value
	case facts.GoSelector:
		if e.Object != nil && e.Object.Kind == facts.GoIdentifier && v.pkg(e.Object.Name) == "net/http" {
			goShapeTagRow("SH-G25")
			code = goHTTPStatus[e.Name]
		}
	case facts.GoIdentifier:
		if _, ok := v.params[e.Name]; ok {
			if lit, ok := v.env.Lits[e.Name]; ok && !v.reassigned[e.Name] && strings.HasPrefix(lit, "n:") {
				goShapeTagRow("SH-G25")
				code = lit[2:]
			}
		}
	}
	n, err := strconv.Atoi(code)
	if err != nil || n < 100 || n > 599 {
		return "", false
	}
	return strconv.Itoa(n), true
}

// ginMethods are the gin.Context methods the rows read, by row (the
// forms "gin:<method>" of docs/contract/claims/route-shape-go.md).
var ginMethods = map[string]string{
	"Param": "SH-G1",
	"Query": "SH-G5", "DefaultQuery": "SH-G5", "GetQuery": "SH-G5", "QueryArray": "SH-G5", "GetQueryArray": "SH-G5",
	"PostForm": "SH-G8", "DefaultPostForm": "SH-G8", "GetPostForm": "SH-G8", "PostFormArray": "SH-G8", "GetPostFormArray": "SH-G8", "FormFile": "SH-G8",
	"GetHeader":      "SH-G9",
	"Cookie":         "SH-G10",
	"ShouldBindJSON": "SH-G11", "BindJSON": "SH-G11",
	"ShouldBind": "SH-G12", "Bind": "SH-G12",
	"ShouldBindQuery": "SH-G13", "BindQuery": "SH-G13",
	"ShouldBindUri": "SH-G14", "BindUri": "SH-G14",
	"JSON": "SH-G18", "IndentedJSON": "SH-G18", "PureJSON": "SH-G18", "AbortWithStatusJSON": "SH-G18",
	"String": "SH-G19",
	"Status": "SH-G20", "AbortWithStatus": "SH-G20", "AbortWithError": "SH-G20", "Redirect": "SH-G20",
}

// ginMust are the gin binders that answer 400 themselves (row SH-G17).
var ginMust = map[string]bool{"BindJSON": true, "Bind": true, "BindQuery": true, "BindUri": true}

// httpRequestMethods and httpWriterMethods are the *http.Request and
// http.ResponseWriter methods the rows read (forms "http:<method>").
var (
	httpRequestMethods = map[string]string{"PathValue": "SH-G3", "FormValue": "SH-G7", "PostFormValue": "SH-G8", "FormFile": "SH-G8", "Cookie": "SH-G10"}
	httpWriterMethods  = map[string]string{"WriteHeader": "SH-G21"}
)

// goReadSections are the sections the name-read rows fill.
var goReadSections = map[string]string{
	"SH-G1": routeshape.SectionPath, "SH-G3": routeshape.SectionPath, "SH-G5": routeshape.SectionQuery,
	"SH-G8": routeshape.SectionBody, "SH-G9": routeshape.SectionHeader, "SH-G10": routeshape.SectionCookie,
}

// ginUnread are gin.Context methods that read or write the request in ways
// no row models (row D): their section is noted as not fully read.
var ginUnread = map[string]string{
	"ShouldBindWith": routeshape.SectionBody, "ShouldBindBodyWith": routeshape.SectionBody, "BindWith": routeshape.SectionBody, "MustBindWith": routeshape.SectionBody,
	"ShouldBindBodyWithJSON": routeshape.SectionBody, "ShouldBindXML": routeshape.SectionBody, "BindXML": routeshape.SectionBody, "ShouldBindYAML": routeshape.SectionBody, "BindYAML": routeshape.SectionBody,
	"ShouldBindTOML": routeshape.SectionBody, "BindTOML": routeshape.SectionBody, "ShouldBindPlain": routeshape.SectionBody, "BindPlain": routeshape.SectionBody,
	"GetRawData": routeshape.SectionBody, "MultipartForm": routeshape.SectionBody, "QueryMap": routeshape.SectionQuery, "GetQueryMap": routeshape.SectionQuery,
	"PostFormMap": routeshape.SectionBody, "GetPostFormMap": routeshape.SectionBody, "ShouldBindHeader": routeshape.SectionHeader, "BindHeader": routeshape.SectionHeader,
	"XML": routeshape.SectionResponses, "YAML": routeshape.SectionResponses, "TOML": routeshape.SectionResponses, "ProtoBuf": routeshape.SectionResponses,
	"Data": routeshape.SectionResponses, "DataFromReader": routeshape.SectionResponses, "File": routeshape.SectionResponses, "FileAttachment": routeshape.SectionResponses,
	"FileFromFS": routeshape.SectionResponses, "HTML": routeshape.SectionResponses, "SSEvent": routeshape.SectionResponses, "Stream": routeshape.SectionResponses,
	"SecureJSON": routeshape.SectionResponses, "JSONP": routeshape.SectionResponses, "AsciiJSON": routeshape.SectionResponses, "Render": routeshape.SectionResponses,
	"Negotiate": routeshape.SectionResponses, "ProtoBufJSON": routeshape.SectionResponses,
}

// call reads one call node of the function.
func (v *goShapeVisit) call(n *facts.GoExpr, rec *facts.GoShapeCall, top bool) {
	callee := n.Callee
	if callee == nil || v.handled[n] {
		return
	}
	if callee.Kind == facts.GoSelector && callee.Object != nil {
		obj, m := callee.Object, callee.Name
		if role := v.carrier(obj); role != "" {
			v.carrierMethod(role, m, n, rec, top)
			return
		}
		switch {
		case obj.Kind == facts.GoSelector && obj.Name == "Header" && v.carrier(obj.Object) == facts.GoCarrierRequest && (m == "Get" || m == "Values"):
			// r.Header.Get("X") (row SH-G9).
			if len(n.Args) == 1 {
				v.read(routeshape.SectionHeader, "SH-G9", &n.Args[0], n, rec, top)
			}
			return
		case (m == "Get" || m == "Has") && v.query(obj):
			// r.URL.Query().Get("x"), q.Get("x") (row SH-G6).
			if len(n.Args) == 1 {
				v.read(routeshape.SectionQuery, "SH-G6", &n.Args[0], n, rec, top)
			}
			return
		case m == "Set" && obj.Kind == facts.GoCall && obj.Callee != nil && obj.Callee.Kind == facts.GoSelector && obj.Callee.Name == "Header" && v.carrier(obj.Callee.Object) == facts.GoCarrierWriter:
			// w.Header().Set("Content-Type", "...") (row SH-G24).
			if len(n.Args) == 2 && n.Args[0].Kind == facts.GoString && strings.EqualFold(n.Args[0].Value, "Content-Type") {
				ct := ""
				if n.Args[1].Kind == facts.GoString {
					ct = goMediaType(n.Args[1].Value)
				}
				v.events = append(v.events, goWriteEvent{block: rec.Block, kind: "ct", ct: ct})
			}
			return
		case m == "Encode" && v.jsonCall(obj, "NewEncoder") && len(obj.Args) == 1 && v.carrier(&obj.Args[0]) == facts.GoCarrierWriter:
			v.handled[obj] = true
			v.encode(n, rec)
			return
		case m == "Decode" && v.jsonCall(obj, "NewDecoder") && len(obj.Args) == 1 && obj.Args[0].Kind == facts.GoSelector && obj.Args[0].Name == "Body" && v.carrier(obj.Args[0].Object) == facts.GoCarrierRequest:
			v.handled[obj] = true
			if len(n.Args) == 1 {
				v.bindBody("SH-G16", &n.Args[0], rec, top, false, false)
			}
			return
		case obj.Kind == facts.GoIdentifier:
			if p := v.pkg(obj.Name); p != "" {
				if v.pkgCall(p, m, n, rec) {
					return
				}
			}
		}
	}
	v.follow(n, rec)
}

// jsonCall reports e is json.<name>(...) with json encoding/json.
func (v *goShapeVisit) jsonCall(e *facts.GoExpr, name string) bool {
	return e.Kind == facts.GoCall && e.Callee != nil && e.Callee.Kind == facts.GoSelector && e.Callee.Name == name &&
		e.Callee.Object != nil && e.Callee.Object.Kind == facts.GoIdentifier && v.pkg(e.Callee.Object.Name) == "encoding/json"
}

// query reports e is r.URL.Query() on a request value, or a local
// declared once from one.
func (v *goShapeVisit) query(e *facts.GoExpr) bool {
	if e.Kind == facts.GoIdentifier {
		if l := v.local(e.Name); l != nil && l.Value != nil {
			return v.query(l.Value)
		}
		return false
	}
	return e.Kind == facts.GoCall && len(e.Args) == 0 && e.Callee != nil && e.Callee.Kind == facts.GoSelector && e.Callee.Name == "Query" &&
		e.Callee.Object != nil && e.Callee.Object.Kind == facts.GoSelector && e.Callee.Object.Name == "URL" && v.carrier(e.Callee.Object.Object) == facts.GoCarrierRequest
}

// carrierMethod reads a method call on a request value.
func (v *goShapeVisit) carrierMethod(role, m string, n *facts.GoExpr, rec *facts.GoShapeCall, top bool) {
	arg := func(i int) *facts.GoExpr {
		if i < len(n.Args) {
			return &n.Args[i]
		}
		return nil
	}
	status := func() (string, bool) {
		code, ok := v.status(arg(0))
		if !ok {
			v.col.note(routeshape.SectionResponses, "a status Graphvein does not read")
		}
		return code, ok
	}
	switch role {
	case facts.GoCarrierGin:
		row, ok := ginMethods[m]
		if !ok {
			if sec, ok := ginUnread[m]; ok {
				goShapeTagRow("D")
				v.col.note(sec, "a gin call no row reads (c."+m+")")
			}
			return
		}
		must := ginMust[m]
		switch row {
		case "SH-G1", "SH-G5", "SH-G8", "SH-G9", "SH-G10":
			v.read(goReadSections[row], row, arg(0), n, rec, top)
		case "SH-G11":
			v.bindBody(row, arg(0), rec, top, true, must)
		case "SH-G12":
			if v.col.reg.method == "GET" {
				v.bindParams(routeshape.SectionQuery, "form", "SH-G13", arg(0), rec, top, must)
			} else {
				v.bindForm(arg(0), rec, top, must)
			}
		case "SH-G13":
			v.bindParams(routeshape.SectionQuery, "form", row, arg(0), rec, top, must)
		case "SH-G14":
			v.bindParams(routeshape.SectionPath, "uri", row, arg(0), rec, top, must)
		case "SH-G18":
			code, _ := status()
			ct := goJSONType
			v.respond("body", code, &ct, v.valueSchema(arg(1), 0), v.prov(routeshape.Returned, row, rec), rec)
		case "SH-G19":
			code, _ := status()
			ct := goTextType
			v.respond("body", code, &ct, &routeshape.Schema{Type: routeshape.TypeString}, v.prov(routeshape.Returned, row, rec), rec)
		case "SH-G20":
			code, _ := status()
			event := "commit"
			switch m {
			case "Status":
				// c.Status only sets the status: a later write replaces it.
				event = "set"
			case "Redirect":
				event = "body"
			}
			v.respond(event, code, nil, nil, v.prov(routeshape.Returned, row, rec), rec)
		}
	case facts.GoCarrierRequest:
		switch row := httpRequestMethods[m]; row {
		case "SH-G3":
			v.read(routeshape.SectionPath, row, arg(0), n, rec, top)
		case "SH-G7":
			v.read(routeshape.SectionQuery, row, arg(0), n, rec, top)
			v.read(routeshape.SectionBody, row, arg(0), n, rec, top)
			v.col.note(routeshape.SectionQuery, "r.FormValue reads the query and form bodies")
		case "SH-G8":
			v.read(routeshape.SectionBody, row, arg(0), n, rec, top)
		case "SH-G10":
			v.read(routeshape.SectionCookie, row, arg(0), n, rec, top)
		}
	case facts.GoCarrierWriter:
		switch {
		case httpWriterMethods[m] == "SH-G21":
			code, _ := status()
			v.respond("commit", code, nil, nil, v.prov(routeshape.Returned, "SH-G21", rec), rec)
		case m == "Write":
			goShapeTagRow("D")
			v.col.note(routeshape.SectionResponses, "a body written with w.Write is not read")
			v.events = append(v.events, goWriteEvent{block: rec.Block, kind: "body"})
		}
	}
}

// read records a name read (rows SH-G1..SH-G10).
func (v *goShapeVisit) read(section, rule string, arg, n *facts.GoExpr, rec *facts.GoShapeCall, top bool) {
	name, ok := v.name(arg)
	if !ok {
		goShapeTagRow("D")
		v.col.note(section, "a name passed as a value Graphvein does not read")
		return
	}
	converted := v.converted[n] || top && len(rec.Results) > 0 && rec.Results[0] != "" && v.convLocal[rec.Results[0]]
	prov := v.prov(routeshape.ReadInHandler, rule, rec)
	if converted && section != routeshape.SectionBody {
		prov = v.prov(routeshape.ReadInHandler, "SH-G4", rec)
	}
	if section == routeshape.SectionBody {
		v.col.formField(name, prov)
	} else {
		v.col.param(section, routeshape.Param{Name: name, Access: routeshape.ReadByHandler, Presence: routeshape.PresenceUnknown, Provenance: prov})
	}
	v.contributed(section)
}

// bindKind is the provenance kind of a bind or decode: fact when gin's
// Bind* aborts with 400 itself or the error is checked with a return
// (row SH-G15), intended otherwise.
func (v *goShapeVisit) bindKind(rec *facts.GoShapeCall, top, must bool) routeshape.Kind {
	if must || top && rec.Checked && !v.env.Unchecked {
		return routeshape.Fact
	}
	return routeshape.Intended
}

// target is the type a bind or decode writes: the pointed-to type of &x
// or of a pointer value.
func (v *goShapeVisit) bindTarget(arg *facts.GoExpr) (goTypeRef, bool) {
	if arg == nil {
		return goTypeRef{}, false
	}
	t, ok := v.typeOf(arg, 0)
	if !ok || t.Type.Kind != facts.GoPointer || t.Type.Object == nil {
		return goTypeRef{}, false
	}
	return goTypeRef{File: t.File, Type: *t.Type.Object, Shadow: t.Shadow}, true
}

// bindBody reads a JSON body bind or decode (rows SH-G11, SH-G16, SH-G17).
func (v *goShapeVisit) bindBody(rule string, arg *facts.GoExpr, rec *facts.GoShapeCall, top, gin, must bool) {
	t, ok := v.bindTarget(arg)
	if !ok {
		v.col.note(routeshape.SectionBody, "a body decoded into a value whose type is not read")
		return
	}
	kind := v.bindKind(rec, top, must)
	if kind == routeshape.Intended {
		goShapeTagRow("SH-G15")
	}
	b := &goSchemaBuilder{l: v.l, binding: gin && kind == routeshape.Fact, gin: gin, dive: true, mode: goJSONRequest, stack: map[string]bool{}, shadow: goShadow(t.Shadow)}
	s := b.schema(t.File, t.Type, 0)
	if s.Type == routeshape.TypeObject {
		goShapeTagRow("SH-G26")
		if b.binding {
			goShapeTagRow("SH-G27")
		}
	}
	ct := goJSONType
	v.col.body(routeshape.Content{ContentType: &ct, Schema: &s, Provenance: v.prov(kind, rule, rec)})
	v.contributed(routeshape.SectionBody)
	if must && !v.committed(rec) {
		v.col.response("400", nil, nil, v.prov(routeshape.Fact, "SH-G17", rec))
	}
}

// bindForm reads gin's Bind and ShouldBind on a route that is not GET
// (row SH-G12, H18): the binder follows the Content-Type, so the content
// type is not read, and the schema is claimed only when the json and form
// names of every field agree.
func (v *goShapeVisit) bindForm(arg *facts.GoExpr, rec *facts.GoShapeCall, top, must bool) {
	v.col.note(routeshape.SectionBody, "gin Bind and ShouldBind choose the binder from the method and Content-Type: the names are the JSON and form names, the form binder also reads the query, and no field is required")
	t, ok := v.bindTarget(arg)
	if !ok {
		v.col.note(routeshape.SectionBody, "a body decoded into a value whose type is not read")
		return
	}
	kind := v.bindKind(rec, top, must)
	if kind == routeshape.Intended {
		goShapeTagRow("SH-G15")
	}
	// With the binder not read, no field is required: the form binder
	// also takes values from the query, and XML, YAML and TOML read their
	// own names (B5). Types are still enforced by every binder.
	b := &goSchemaBuilder{l: v.l, gin: true, mode: goJSONRequest, stack: map[string]bool{}, shadow: goShadow(t.Shadow)}
	s := b.schema(t.File, t.Type, 0)
	if s.Type == routeshape.TypeObject {
		form, why := b.formParams(t.File, t.Type, "form", 0)
		if why != "" || !goSameNames(s, form) {
			s = unknownSchema("the JSON and form names of the type differ or are not read")
		}
	}
	v.col.body(routeshape.Content{Schema: &s, Provenance: v.prov(kind, "SH-G12", rec)})
	v.contributed(routeshape.SectionBody)
	if must && !v.committed(rec) {
		v.col.response("400", nil, nil, v.prov(routeshape.Fact, "SH-G17", rec))
	}
}

// goSameNames reports that an object's fields and the form parameters
// have the same names in the same order.
func goSameNames(s routeshape.Schema, form []goFormParam) bool {
	if len(s.Fields) != len(form) {
		return false
	}
	for i, f := range s.Fields {
		if f.Name != form[i].name {
			return false
		}
	}
	return true
}

// bindParams reads gin's query and uri binders (rows SH-G13, SH-G14).
func (v *goShapeVisit) bindParams(section, tag, rule string, arg *facts.GoExpr, rec *facts.GoShapeCall, top, must bool) {
	t, ok := v.bindTarget(arg)
	if !ok {
		v.col.note(section, "parameters bound into a value whose type is not read")
		return
	}
	kind := v.bindKind(rec, top, must)
	if kind == routeshape.Intended {
		goShapeTagRow("SH-G15")
	}
	b := &goSchemaBuilder{l: v.l, binding: true, mode: goJSONRequest, stack: map[string]bool{}, shadow: goShadow(t.Shadow)}
	params, why := b.formParams(t.File, t.Type, tag, 0)
	if why != "" {
		v.col.note(section, why)
		return
	}
	goShapeTagRow("SH-G28")
	pattern := v.col.patternParams()
	for _, p := range params {
		presence := p.presence
		if kind != routeshape.Fact && presence == routeshape.Required {
			presence = routeshape.PresenceUnknown
		}
		if section == routeshape.SectionPath {
			if !pattern[p.name] {
				v.col.note(section, fmt.Sprintf("uri field %q is not a parameter of the route's pattern", p.name))
				continue
			}
			if kind == routeshape.Fact {
				presence = routeshape.Required
			}
		}
		if presence == routeshape.Required {
			goShapeTagRow("SH-G27")
		}
		v.col.param(section, routeshape.Param{Name: p.name, Access: routeshape.Declared, Presence: presence, Schema: p.schema, Provenance: v.prov(kind, rule, rec)})
	}
	v.contributed(section)
	if must && !v.committed(rec) {
		v.col.response("400", nil, nil, v.prov(routeshape.Fact, "SH-G17", rec))
	}
}

// encode reads json.NewEncoder(w).Encode(v) (row SH-G24): the status is
// the WriteHeader that precedes it on every path, or 200 in the handler
// itself when nothing wrote to the writer before; the content type is a
// preceding Content-Type header set, else not read.
func (v *goShapeVisit) encode(n *facts.GoExpr, rec *facts.GoShapeCall) {
	code, ct := v.writeState(rec)
	var schema *routeshape.Schema
	if len(n.Args) == 1 {
		schema = v.valueSchema(&n.Args[0], 0)
	}
	if code == "" {
		v.col.note(routeshape.SectionResponses, "a status Graphvein does not read")
	}
	var ctp *string
	if ct != "" {
		ctp = &ct
	}
	// Encode has no status of its own: writeState read the one in force.
	v.respond("encode", code, ctp, schema, v.prov(routeshape.Returned, "SH-G24", rec), rec)
}

// goNoBody are the statuses net/http and gin send without a body.
func goNoBody(code string) bool {
	return strings.HasPrefix(code, "1") || code == "204" || code == "304"
}

// committed reports a write before rec that certainly sent the status
// (a WriteHeader, an abort or a body in rec's block or one enclosing it).
func (v *goShapeVisit) committed(rec *facts.GoShapeCall) bool {
	for _, e := range v.events {
		if (e.kind == "commit" || e.kind == "body") && v.ancestor(e.block, rec.Block) {
			return true
		}
	}
	return false
}

// respond records a write (row SH-G37's write state): a
// status after the status was already sent is never sent and is dropped;
// 1xx, 204 and 304 carry no body; gin's c.Status waits for the end of the
// function (a later write replaces it). An empty code records the event
// only.
func (v *goShapeVisit) respond(event, code string, ct *string, schema *routeshape.Schema, prov routeshape.Provenance, rec *facts.GoShapeCall) {
	written := event != "encode" && v.committed(rec)
	if event == "encode" {
		event = "body"
	}
	v.events = append(v.events, goWriteEvent{block: rec.Block, kind: event, status: code})
	if code == "" {
		return
	}
	if written {
		v.col.note(routeshape.SectionResponses, "a status written after the response was sent is never sent")
		return
	}
	if goNoBody(code) {
		ct, schema = nil, nil
	}
	if event == "set" {
		v.pending = append(v.pending, goPendingStatus{event: len(v.events) - 1, block: rec.Block, resp: routeshape.Response{Status: code, Provenance: prov}})
		return
	}
	v.col.response(code, ct, schema, prov)
	v.contributed(routeshape.SectionResponses)
}

// ancestor reports a is b or a block enclosing b.
func (v *goShapeVisit) ancestor(a, b int) bool {
	for {
		if a == b {
			return true
		}
		if b == 0 || b > len(v.fn.Blocks) {
			return false
		}
		b = v.fn.Blocks[b-1]
	}
}

// returnsBefore reports that a write in block from never reaches block to:
// a block between from and their common ancestor ends in a return.
func (v *goShapeVisit) returnsBefore(from, to int) bool {
	terminal := map[int]bool{}
	for _, t := range v.fn.Terminal {
		terminal[t] = true
	}
	for b := from; !v.ancestor(b, to); {
		if terminal[b] {
			return true
		}
		if b == 0 || b > len(v.fn.Blocks) {
			return false
		}
		b = v.fn.Blocks[b-1]
	}
	return false
}

// writeState is the status and content type in force at rec: the last
// status set or written before it on every path, else 200 in the handler
// itself with nothing written before; the Content-Type set before any
// status was written (a header set after WriteHeader is not sent).
func (v *goShapeVisit) writeState(rec *facts.GoShapeCall) (code, ct string) {
	code = "?"
	for i := len(v.events) - 1; i >= 0; i-- {
		e := v.events[i]
		if e.kind == "ct" {
			continue
		}
		if !v.ancestor(e.block, rec.Block) {
			if v.returnsBefore(e.block, rec.Block) {
				continue
			}
			code = ""
			break
		}
		if e.kind == "set" || e.kind == "commit" {
			code = e.status
		} else {
			code = ""
		}
		break
	}
	if code == "?" {
		code = ""
		if len(v.frame.Chain) == 0 && !rec.Closure {
			goShapeTagRow("SH-G24")
			code = "200"
		}
	}
	for i := len(v.events) - 1; i >= 0; i-- {
		e := v.events[i]
		if e.kind != "ct" {
			continue
		}
		if !v.ancestor(e.block, rec.Block) {
			if v.returnsBefore(e.block, rec.Block) {
				continue
			}
			return code, ""
		}
		for _, prior := range v.events[:i] {
			if (prior.kind == "commit" || prior.kind == "body" || prior.kind == "pass") && (v.ancestor(prior.block, e.block) || !v.returnsBefore(prior.block, e.block)) {
				return code, ""
			}
		}
		return code, e.ct
	}
	return code, ""
}

// pkgCall reads a call of a package function: chi.URLParam (row SH-G2),
// http.Error and http.NotFound (row SH-G22), http.Redirect (row SH-G23).
// It reports whether the call was read.
func (v *goShapeVisit) pkgCall(path, m string, n *facts.GoExpr, rec *facts.GoShapeCall) bool {
	args := n.Args
	switch {
	case (path == "github.com/go-chi/chi/v5" || path == "github.com/go-chi/chi") && m == "URLParam" && len(args) == 2:
		if v.carrier(&args[0]) == facts.GoCarrierRequest {
			v.read(routeshape.SectionPath, "SH-G2", &args[1], n, rec, false)
			return true
		}
	case path == "net/http" && m == "Error" && len(args) == 3 && v.carrier(&args[0]) == facts.GoCarrierWriter:
		code, ok := v.status(&args[2])
		if !ok {
			v.col.note(routeshape.SectionResponses, "a status Graphvein does not read")
		}
		ct := goTextType
		v.respond("body", code, &ct, &routeshape.Schema{Type: routeshape.TypeString}, v.prov(routeshape.Returned, "SH-G22", rec), rec)
		return true
	case path == "net/http" && m == "NotFound" && len(args) == 2 && v.carrier(&args[0]) == facts.GoCarrierWriter:
		ct := goTextType
		v.respond("body", "404", &ct, &routeshape.Schema{Type: routeshape.TypeString}, v.prov(routeshape.Returned, "SH-G22", rec), rec)
		return true
	case path == "net/http" && m == "Redirect" && len(args) == 4 && v.carrier(&args[0]) == facts.GoCarrierWriter:
		code, ok := v.status(&args[3])
		if !ok {
			v.col.note(routeshape.SectionResponses, "a status Graphvein does not read")
		}
		v.respond("body", code, nil, nil, v.prov(routeshape.Returned, "SH-G23", rec), rec)
		return true
	}
	return false
}

// typeOf is the declared type of a value expression (row SH-G30): a
// composite literal T{...} or &T{...}, new(T), &x, or a name declared
// once with a type, a composite literal or new(T), a parameter's declared
// type, or the type the caller passed to an interface parameter.
func (v *goShapeVisit) typeOf(e *facts.GoExpr, depth int) (goTypeRef, bool) {
	if e == nil || depth > 4 {
		return goTypeRef{}, false
	}
	file := v.ref.file
	switch e.Kind {
	case facts.GoComposite:
		if e.Type != nil {
			return goTypeRef{File: file, Type: *e.Type, Shadow: v.fn.LocalTypes}, true
		}
	case facts.GoUnary:
		if e.Value == "&" && e.Object != nil {
			if t, ok := v.typeOf(e.Object, depth+1); ok {
				return goTypeRef{File: t.File, Type: facts.GoExpr{Kind: facts.GoPointer, Object: &t.Type}, Shadow: t.Shadow}, true
			}
		}
	case facts.GoCall:
		if e.Callee != nil && e.Callee.Kind == facts.GoIdentifier && e.Callee.Name == "new" && len(e.Args) == 1 && v.local("new") == nil {
			t := e.Args[0]
			return goTypeRef{File: file, Type: facts.GoExpr{Kind: facts.GoPointer, Object: &t}, Shadow: v.fn.LocalTypes}, true
		}
	case facts.GoIdentifier:
		if p, ok := v.params[e.Name]; ok {
			if p.Carrier != "" {
				return goTypeRef{}, false
			}
			if goOpaqueType(p.Type) {
				if t, ok := v.env.Types[e.Name]; ok && !v.reassigned[e.Name] {
					return t, true
				}
				return goTypeRef{}, false
			}
			return goTypeRef{File: file, Type: p.Type, Shadow: v.fn.LocalTypes}, true
		}
		if l := v.local(e.Name); l != nil {
			if l.Type != nil {
				return goTypeRef{File: file, Type: *l.Type, Shadow: v.fn.LocalTypes}, true
			}
			if l.Value != nil && (l.Value.Kind == facts.GoComposite || l.Value.Kind == facts.GoUnary || l.Value.Kind == facts.GoCall) {
				return v.typeOf(l.Value, depth+1)
			}
		}
	}
	return goTypeRef{}, false
}

// goOpaqueType reports a parameter type whose value's type is the
// caller's: any, interface{} or a type parameter-like name not resolved.
func goOpaqueType(t facts.GoExpr) bool {
	return t.Kind == facts.GoInterface || t.Kind == facts.GoIdentifier && t.Name == "any"
}

// valueSchema is the schema of a response value (rows SH-G26, SH-G29,
// SH-G30): an inline map literal, a literal, nil, or a value of a
// declared type; nil when not read.
func (v *goShapeVisit) valueSchema(e *facts.GoExpr, depth int) *routeshape.Schema {
	if e == nil {
		return nil
	}
	switch e.Kind {
	case facts.GoNil:
		return &routeshape.Schema{Type: routeshape.TypeNull}
	case facts.GoString:
		return &routeshape.Schema{Type: routeshape.TypeString}
	case facts.GoBoolean:
		return &routeshape.Schema{Type: routeshape.TypeBoolean}
	case facts.GoNumber:
		if strings.ContainsAny(e.Value, ".eEpP") && !strings.HasPrefix(e.Value, "0x") {
			return &routeshape.Schema{Type: routeshape.TypeNumber}
		}
		return &routeshape.Schema{Type: routeshape.TypeInteger}
	case facts.GoComposite:
		if v.mapLiteral(e) {
			goShapeTagRow("SH-G29")
			s := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalForbidden, Fields: []routeshape.Field{}}
			seen := map[string]bool{}
			for _, el := range e.Elements {
				if el.Key == nil || el.Key.Kind != facts.GoString || seen[el.Key.Value] {
					return &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "map literal with a key that is not a distinct string literal"}
				}
				seen[el.Key.Value] = true
				val := el.Value
				fs := v.valueSchema(&val, depth+1)
				if fs == nil {
					fs = &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "value type not read"}
				}
				s.Fields = append(s.Fields, routeshape.Field{Name: el.Key.Value, Presence: routeshape.PresenceUnknown, Schema: *fs})
			}
			return &s
		}
	}
	t, ok := v.typeOf(e, 0)
	if !ok {
		return nil
	}
	goShapeTagRow("SH-G30")
	b := &goSchemaBuilder{l: v.l, mode: goJSONResponse, stack: map[string]bool{}, shadow: goShadow(t.Shadow)}
	s := b.schema(t.File, t.Type, depth)
	if s.Type == routeshape.TypeObject {
		goShapeTagRow("SH-G26")
	}
	if s.Type == routeshape.TypeUnknown {
		goShapeTagRow("SH-G31")
	}
	return &s
}

// mapLiteral reports a composite literal of gin.H or map[string]T.
func (v *goShapeVisit) mapLiteral(e *facts.GoExpr) bool {
	t := e.Type
	if t == nil {
		return false
	}
	switch t.Kind {
	case facts.GoSelector:
		return t.Name == "H" && t.Object != nil && t.Object.Kind == facts.GoIdentifier && v.pkg(t.Object.Name) == ginImportPath
	case facts.GoMap:
		return t.Left != nil && t.Left.Kind == facts.GoIdentifier && t.Left.Name == "string"
	}
	return false
}

const ginImportPath = "github.com/gin-gonic/gin"

// follow reads a call that passes a request value on (rows SH-G32,
// SH-G33): it returns the call with a proven target and the callee's
// environment, or the reason it is not followed.
func (v *goShapeVisit) follow(n *facts.GoExpr, rec *facts.GoShapeCall) {
	passes := false
	for i := range n.Args {
		if v.carrier(&n.Args[i]) != "" {
			passes = true
			if role := v.carrier(&n.Args[i]); role == facts.GoCarrierWriter || role == facts.GoCarrierGin {
				v.events = append(v.events, goWriteEvent{block: rec.Block, kind: "pass"})
			}
		}
	}
	if !passes {
		return
	}
	site := routeshape.Step{File: v.ref.file, Line: rec.Line, Function: goExprText(n.Callee)}
	stop := func(s routeshape.Stop, detail string) {
		goShapeTagRow("SH-G33")
		v.calls = append(v.calls, routeshape.Call{Site: site, Stop: s, Detail: detail})
	}
	identity, why, s := v.target(n.Callee)
	if identity == "" {
		stop(s, why)
		return
	}
	ref := v.l.refOfNode(identity)
	if ref == nil {
		stop(routeshape.StopOpaqueType, "the callee declares no request parameter")
		return
	}
	env := goEnv{Carriers: map[string]string{}, Lits: map[string]string{}, Types: map[string]goTypeRef{}}
	// The callee's error checks stop the request only when this call's
	// error is checked with a return, all the way up (B6).
	env.Unchecked = v.env.Unchecked || n != &rec.Call || !rec.Checked
	for i := range n.Args {
		if i >= len(ref.fn.Params) {
			break
		}
		p := ref.fn.Params[i]
		if p.Name == "" || p.Name == "_" {
			continue
		}
		a := &n.Args[i]
		if role := v.carrier(a); role != "" {
			if p.Carrier == role {
				env.Carriers[p.Name] = role
			}
			continue
		}
		if name, ok := v.name(a); ok {
			env.Lits[p.Name] = "s:" + name
			continue
		}
		if code, ok := v.status(a); ok {
			env.Lits[p.Name] = "n:" + code
			continue
		}
		if goOpaqueType(p.Type) {
			if t, ok := v.typeOf(a, 0); ok {
				env.Types[p.Name] = t
			}
		}
	}
	if len(env.Carriers) == 0 {
		stop(routeshape.StopOpaqueType, "the request value is passed to a parameter of another type")
		return
	}
	goShapeTagRow("SH-G32")
	target := routeshape.Step{File: ref.file, Line: ref.fn.Line, Function: ref.name}
	v.r.refs[goStepKey(target)] = ref
	v.calls = append(v.calls, routeshape.Call{Site: site, Target: target, Value: env.encode()})
}

// target resolves a callee to a unique function or method node: a
// package function f or pkg.F of the module, a method of the function's
// own receiver type, or a method of a parameter's or local's declared
// concrete module type. Otherwise it returns the stop reason.
func (v *goShapeVisit) target(callee *facts.GoExpr) (identity, why string, stop routeshape.Stop) {
	unique := func(ids []string, name string) (string, string, routeshape.Stop) {
		switch len(ids) {
		case 0:
			return "", fmt.Sprintf("no definition of %s in the module", name), routeshape.StopUnresolved
		case 1:
			return ids[0], "", ""
		}
		return "", fmt.Sprintf("%d definitions of %s", len(ids), name), routeshape.StopAmbiguous
	}
	switch callee.Kind {
	case facts.GoIdentifier:
		if _, ok := v.params[callee.Name]; ok {
			return "", "a function value", routeshape.StopUnresolved
		}
		if _, ok := v.locals[callee.Name]; ok {
			return "", "a function value", routeshape.StopUnresolved
		}
		return unique(v.l.callables(v.l.ownFiles(v.ref.file), callee.Name, "function"), callee.Name)
	case facts.GoSelector:
		x := callee.Object
		if x == nil || x.Kind != facts.GoIdentifier {
			return "", "the receiver's type is not read", routeshape.StopUnresolved
		}
		if p := v.pkg(x.Name); p != "" {
			if imp, ok := v.l.moduleImports(v.ref.file)[x.Name]; ok && imp.files != nil {
				return unique(v.l.callables(imp.files, callee.Name, "function"), x.Name+"."+callee.Name)
			}
			return "", "a function of " + p, routeshape.StopExternal
		}
		var typ *facts.GoExpr
		if x.Name == v.fn.ReceiverName && v.fn.Receiver != "" {
			return unique(v.l.callables(v.l.ownFiles(v.ref.file), v.fn.Receiver+"."+callee.Name, "method"), v.fn.Receiver+"."+callee.Name)
		}
		typFile := v.ref.file
		if p, ok := v.params[x.Name]; ok {
			typ = &p.Type
		} else if l := v.local(x.Name); l != nil && l.Type != nil {
			typ = l.Type
		} else if l != nil && l.Value != nil {
			// x := NewX(): the single declared result of a module function
			// (cached Signatures).
			typ, typFile = v.resultType(l.Value)
		}
		if typ == nil {
			return "", "the receiver's type is not read", routeshape.StopUnresolved
		}
		t := *typ
		if t.Kind == facts.GoPointer && t.Object != nil {
			t = *t.Object
		}
		b := &goSchemaBuilder{l: v.l, stack: map[string]bool{}, shadow: goShadow(v.fn.LocalTypes)}
		d, why := b.lookupType(typFile, t)
		if why != "" {
			if t.Kind == facts.GoSelector {
				return "", why, routeshape.StopExternal
			}
			return "", why, routeshape.StopUnresolved
		}
		if d.named != nil && d.named.Type.Kind == facts.GoInterface {
			return "", fmt.Sprintf("method %s of interface %s: the implementation is not proven", callee.Name, d.name), routeshape.StopAmbiguous
		}
		return unique(v.l.callables(v.l.ownFiles(d.file), d.name+"."+callee.Name, "method"), d.name+"."+callee.Name)
	}
	return "", "a call of a value", routeshape.StopUnresolved
}

// resultType is the single declared result type of a call of a module
// function f(...) or pkg.F(...) (facts.GoFacts.Signatures), with the file
// it is written in; nil when the callee is not one unique such function.
func (v *goShapeVisit) resultType(e *facts.GoExpr) (*facts.GoExpr, string) {
	if e.Kind != facts.GoCall || e.Callee == nil {
		return nil, ""
	}
	var files []string
	name := ""
	switch c := e.Callee; c.Kind {
	case facts.GoIdentifier:
		if _, ok := v.params[c.Name]; ok {
			return nil, ""
		}
		if _, ok := v.locals[c.Name]; ok {
			return nil, ""
		}
		files, name = v.l.ownFiles(v.ref.file), c.Name
	case facts.GoSelector:
		if c.Object == nil || c.Object.Kind != facts.GoIdentifier || v.pkg(c.Object.Name) == "" {
			return nil, ""
		}
		imp, ok := v.l.moduleImports(v.ref.file)[c.Object.Name]
		if !ok || imp.files == nil {
			return nil, ""
		}
		files, name = imp.files, c.Name
	default:
		return nil, ""
	}
	var found *facts.GoExpr
	file := ""
	for _, p := range files {
		g := v.l.files[p].Go
		if g == nil {
			continue
		}
		for i := range g.Signatures {
			if g.Signatures[i].Name == name {
				if found != nil {
					return nil, ""
				}
				found, file = &g.Signatures[i].Result, p
			}
		}
	}
	if found != nil {
		goShapeTagRow("SH-G32")
	}
	return found, file
}

// callables lists the function or method nodes named name among files.
func (l *goLinker) callables(files []string, name string, kinds ...string) []string {
	var out []string
	for _, p := range files {
		for _, n := range l.files[p].Nodes {
			for _, k := range kinds {
				if n.Kind == k && n.QualifiedName == name {
					out = append(out, n.Identity)
				}
			}
		}
	}
	return out
}

// goMediaType is a Content-Type value without parameters.
func goMediaType(v string) string {
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = v[:i]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

func goStepsKey(steps []routeshape.Step) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(goStepKey(s))
		b.WriteByte('\x01')
	}
	return b.String()
}

// goShapeCollector gathers one route's items across the followed
// functions and builds the shape.
type goShapeCollector struct {
	l   *goLinker
	reg *goRegistration

	params    map[string][]routeshape.Param
	paramAt   map[string]int
	bodies    []routeshape.Content
	bodyAt    map[string]int
	form      []string
	formSeen  map[string]bool
	formProv  *routeshape.Provenance
	responses []routeshape.Response
	respAt    map[string]int
	notes     map[string][]string
	// frameSections are the sections each read function contributed to,
	// by its chain (chain records are stored under the first).
	frameSections map[string][]string
}

func newGoShapeCollector(l *goLinker, reg *goRegistration) *goShapeCollector {
	return &goShapeCollector{l: l, reg: reg, params: map[string][]routeshape.Param{}, paramAt: map[string]int{}, bodyAt: map[string]int{},
		formSeen: map[string]bool{}, respAt: map[string]int{}, notes: map[string][]string{}, frameSections: map[string][]string{}}
}

func (c *goShapeCollector) note(section, text string) {
	for _, n := range c.notes[section] {
		if n == text {
			return
		}
	}
	c.notes[section] = append(c.notes[section], text)
}

// param adds a parameter once per (name, kind); a converted read replaces
// a plain one (row SH-G4 is the more specific).
func (c *goShapeCollector) param(section string, p routeshape.Param) {
	name := p.Name
	if section == routeshape.SectionHeader {
		name = strings.ToLower(name)
	}
	key := section + "\x00" + name + "\x00" + string(p.Provenance.Kind)
	if i, ok := c.paramAt[key]; ok {
		if p.Provenance.Rule == "SH-G4" && c.params[section][i].Provenance.Rule != "SH-G4" {
			c.params[section][i] = p
		}
		return
	}
	c.paramAt[key] = len(c.params[section])
	c.params[section] = append(c.params[section], p)
}

// formField adds a body form field read by name.
func (c *goShapeCollector) formField(name string, prov routeshape.Provenance) {
	if c.formProv == nil {
		p := prov
		c.formProv = &p
	}
	if !c.formSeen[name] {
		c.formSeen[name] = true
		c.form = append(c.form, name)
	}
}

// body adds a body variant once per (content type, kind); a second,
// different one makes the schema unknown.
func (c *goShapeCollector) body(b routeshape.Content) {
	ct := "\x00"
	if b.ContentType != nil {
		ct = *b.ContentType
	}
	key := ct + "\x00" + string(b.Provenance.Kind)
	if i, ok := c.bodyAt[key]; ok {
		if !goSchemaEqual(c.bodies[i].Schema, b.Schema) {
			c.bodies[i].Schema = &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "several body types"}
		}
		return
	}
	c.bodyAt[key] = len(c.bodies)
	c.bodies = append(c.bodies, b)
}

// response adds a response once per (status, content type, kind).
func (c *goShapeCollector) response(status string, ct *string, schema *routeshape.Schema, prov routeshape.Provenance) {
	k := "\x00"
	if ct != nil {
		k = *ct
	}
	key := status + "\x00" + k + "\x00" + string(prov.Kind)
	if i, ok := c.respAt[key]; ok {
		cur := c.responses[i].Schema
		switch {
		case schema == nil:
		case cur == nil:
			c.responses[i].Schema = schema
		case !goSchemaEqual(cur, schema):
			// Either body may be sent: both declarations stay used (row
			// MT-3).
			u := &routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "several bodies with this status"}
			routeshape.AddDecls(u, cur)
			routeshape.AddDecls(u, schema)
			c.responses[i].Schema = u
		default:
			routeshape.AddDecls(cur, schema)
		}
		return
	}
	c.respAt[key] = len(c.responses)
	c.responses = append(c.responses, routeshape.Response{Status: status, ContentType: ct, Schema: schema, Provenance: prov})
}

func goSchemaEqual(a, b *routeshape.Schema) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// patternParams are the parameter names of the registration's own
// pattern (gin :name and *name segments).
func (c *goShapeCollector) patternParams() map[string]bool {
	out := map[string]bool{}
	if c.reg.framework != goGin {
		return out
	}
	for _, seg := range strings.Split(c.reg.pattern, "/") {
		if len(seg) > 1 && (seg[0] == ':' || seg[0] == '*') {
			out[seg[1:]] = true
		}
	}
	return out
}

// goBaseReason is the reason of every read section (row SH-G37).
const goBaseReason = "read from the handler and the calls followed from it; middleware is not read"

// build assembles the shape (rows SH-G37, SH-G38).
func (c *goShapeCollector) build(res routeshape.Result) *routeshape.Shape {
	s := routeshape.New(res.DepthLimit)
	var follow []string
	stops := map[routeshape.Stop]int{}
	for _, ch := range res.Chains {
		switch ch.Stop {
		case routeshape.StopEnded, routeshape.StopJoined, routeshape.StopCycle:
		default:
			stops[ch.Stop]++
		}
	}
	for _, st := range routeshape.Stops {
		n := stops[st]
		switch {
		case n == 0:
		case st == routeshape.StopDepthCap:
			follow = append(follow, fmt.Sprintf("depth cap reached (%d)", res.DepthLimit))
		default:
			follow = append(follow, fmt.Sprintf("calls not followed: %d %s", n, st))
		}
	}
	reason := func(section string, extra ...string) string {
		parts := []string{goBaseReason}
		parts = append(parts, extra...)
		parts = append(parts, c.notes[section]...)
		parts = append(parts, follow...)
		return strings.Join(parts, "; ")
	}
	goShapeTagRow("SH-G37")
	sections := []struct {
		name string
		sec  *routeshape.ParamSection
	}{{routeshape.SectionPath, &s.Path}, {routeshape.SectionQuery, &s.Query}, {routeshape.SectionHeader, &s.Header}, {routeshape.SectionCookie, &s.Cookie}}
	for _, x := range sections {
		x.sec.Items = c.params[x.name]
		extra := []string(nil)
		if len(x.sec.Items) == 0 {
			extra = append(extra, "no "+x.name+" parameter read")
		}
		x.sec.Section = routeshape.Section{State: routeshape.Partial, Reason: reason(x.name, extra...)}
	}
	bodies := c.bodies
	if len(c.form) > 0 {
		fields := []routeshape.Field{}
		for _, f := range c.form {
			fields = append(fields, routeshape.Field{Name: f, Presence: routeshape.PresenceUnknown, Schema: routeshape.Schema{Type: routeshape.TypeUnknown, Reason: "type not read"}})
		}
		bodies = append(bodies, routeshape.Content{Schema: &routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown, Fields: fields}, Provenance: *c.formProv})
	}
	s.Body.Items = bodies
	extra := []string(nil)
	if len(bodies) == 0 {
		extra = append(extra, "no body read")
	}
	s.Body.Section = routeshape.Section{State: routeshape.Partial, Reason: reason(routeshape.SectionBody, extra...)}
	// A status-only item (WriteHeader, Status) adds nothing beside an item
	// of the same status that also reads the body.
	withBody := map[string]bool{}
	for _, r := range c.responses {
		if r.ContentType != nil || r.Schema != nil {
			withBody[r.Status+"\x00"+string(r.Provenance.Kind)] = true
		}
	}
	responses := []routeshape.Response{}
	for _, r := range c.responses {
		if r.ContentType == nil && r.Schema == nil && withBody[r.Status+"\x00"+string(r.Provenance.Kind)] {
			continue
		}
		responses = append(responses, r)
	}
	sort.SliceStable(responses, func(i, j int) bool { return responses[i].Status < responses[j].Status })
	s.Responses.Items = responses
	s.Responses.Section = routeshape.Section{State: routeshape.Partial, Reason: reason(routeshape.SectionResponses, "middleware and error handlers may answer other statuses")}
	// Chains: one follow per route; each chain is stored once, under the
	// first section its last function contributed to (row SH-G38).
	goShapeTagRow("SH-G38")
	for _, rec := range routeshape.Records(routeshape.SectionResponses, res) {
		if secs := c.frameSections[goStepsKey(rec.Steps)]; len(secs) > 0 {
			rec.Section = secs[0]
		}
		s.Follow.Chains = append(s.Follow.Chains, rec)
	}
	return s
}

// goHTTPStatus are net/http's Status constants.
var goHTTPStatus = map[string]string{
	"StatusContinue": "100", "StatusSwitchingProtocols": "101", "StatusProcessing": "102", "StatusEarlyHints": "103",
	"StatusOK": "200", "StatusCreated": "201", "StatusAccepted": "202", "StatusNonAuthoritativeInfo": "203", "StatusNoContent": "204",
	"StatusResetContent": "205", "StatusPartialContent": "206", "StatusMultiStatus": "207", "StatusAlreadyReported": "208", "StatusIMUsed": "226",
	"StatusMultipleChoices": "300", "StatusMovedPermanently": "301", "StatusFound": "302", "StatusSeeOther": "303", "StatusNotModified": "304",
	"StatusUseProxy": "305", "StatusTemporaryRedirect": "307", "StatusPermanentRedirect": "308",
	"StatusBadRequest": "400", "StatusUnauthorized": "401", "StatusPaymentRequired": "402", "StatusForbidden": "403", "StatusNotFound": "404",
	"StatusMethodNotAllowed": "405", "StatusNotAcceptable": "406", "StatusProxyAuthRequired": "407", "StatusRequestTimeout": "408", "StatusConflict": "409",
	"StatusGone": "410", "StatusLengthRequired": "411", "StatusPreconditionFailed": "412", "StatusRequestEntityTooLarge": "413", "StatusRequestURITooLong": "414",
	"StatusUnsupportedMediaType": "415", "StatusRequestedRangeNotSatisfiable": "416", "StatusExpectationFailed": "417", "StatusTeapot": "418",
	"StatusMisdirectedRequest": "421", "StatusUnprocessableEntity": "422", "StatusLocked": "423", "StatusFailedDependency": "424", "StatusTooEarly": "425",
	"StatusUpgradeRequired": "426", "StatusPreconditionRequired": "428", "StatusTooManyRequests": "429", "StatusRequestHeaderFieldsTooLarge": "431",
	"StatusUnavailableForLegalReasons": "451",
	"StatusInternalServerError":        "500", "StatusNotImplemented": "501", "StatusBadGateway": "502", "StatusServiceUnavailable": "503", "StatusGatewayTimeout": "504",
	"StatusHTTPVersionNotSupported": "505", "StatusVariantAlsoNegotiates": "506", "StatusInsufficientStorage": "507", "StatusLoopDetected": "508",
	"StatusNotExtended": "510", "StatusNetworkAuthenticationRequired": "511",
}

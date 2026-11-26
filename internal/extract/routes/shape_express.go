package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// Express and Next.js Pages Router API request and response shape (T029
// SH-E, docs/contract/claims/route-shape-express.md). LinkShapes reads the
// cached parameter-use syntax of each route's handler (facts.ShapeFunction)
// and the functions it passes the request or response to, and stores a
// routeshape.Shape on the route node. Every claim is one row of the
// table; a form no row reads stays not read.

// tagShape (fastapi_shape_contract.go) reports the SH-E rows too: the
// test hook collects the rows of every shape extractor.

// LinkShapes stores the shape attribute on every Express, Next.js Pages
// Router API and App Router route and every Next.js server action of
// nodes, following calls depth calls deep. A shape the model refuses is
// not stored; a diagnostic says why.
func LinkShapes(nodes []contract.Node, files map[string]facts.Facts, modules *typescript.ModuleResolver, depth int) []contract.Diagnostic {
	if !routeshape.ValidDepth(depth) {
		depth = routeshape.DefaultDepth
	}
	l := &shapeLinker{r: &resolver{files: files, objects: map[string]string{}, objectFiles: map[string]string{}, modules: modules}, depth: depth}
	var diagnostics []contract.Diagnostic
	l.nextGET = map[string]bool{}
	for _, n := range nodes {
		if n.Kind == "route" && n.Attributes["framework"] == "nextjs" && n.Attributes["method"] == "GET" {
			l.nextGET[n.File] = true
		}
	}
	for i := range nodes {
		n := &nodes[i]
		if (n.Kind != "route" && n.Kind != "server_action") || n.Attributes == nil {
			continue
		}
		framework, _ := n.Attributes["framework"].(string)
		var shape *routeshape.Shape
		switch {
		case n.Kind == "server_action" && framework == "nextjs":
			// Server actions (T029 SH-N27..SH-N29).
			shape = l.actionShape(n)
		case n.Kind != "route":
			continue
		case framework == "express" || framework == "nextjs_pages":
			shape = l.route(n, framework)
		case framework == "nextjs":
			// App Router route handlers (T029 SH-N).
			shape = l.nextRoute(n)
		default:
			continue
		}
		value, err := routeshape.ToAttribute(shape)
		if err != nil {
			tagShape("D")
			diagnostics = append(diagnostics, contract.Diagnostic{Type: "diagnostic", File: n.File, Severity: "warning", Code: "route_shape_refused", Message: fmt.Sprintf("%s: %v", n.QualifiedName, err)})
			continue
		}
		n.Attributes[routeshape.AttributeKey] = value
		if n.Kind == "route" {
			// Model-type uses (row MT-5).
			routeshape.SetModelUses(n.Attributes, shape)
		}
	}
	return diagnostics
}

type shapeLinker struct {
	r     *resolver
	depth int
	// nextGET names the App Router route files that export GET (SH-N22:
	// a HEAD request there may be answered from GET's prerendered output).
	nextGET map[string]bool
	// byOffset and byEnd index each file's shape functions.
	byOffset map[string]map[int]*facts.ShapeFunction
	byEnd    map[string]map[[2]int]*facts.ShapeFunction
	// passports caches the passport.use registrations (T029 SH-S6).
	passports *passportRegs
}

func (l *shapeLinker) index(file string) {
	if l.byOffset == nil {
		l.byOffset = map[string]map[int]*facts.ShapeFunction{}
		l.byEnd = map[string]map[[2]int]*facts.ShapeFunction{}
	}
	if _, done := l.byOffset[file]; done {
		return
	}
	off := map[int]*facts.ShapeFunction{}
	end := map[[2]int]*facts.ShapeFunction{}
	if data := l.r.files[file].Shape; data != nil {
		for i := range data.Functions {
			f := &data.Functions[i]
			off[f.Offset] = f
			key := [2]int{f.EndLine, f.EndColumn}
			if _, dup := end[key]; dup {
				end[key] = nil
			} else {
				end[key] = f
			}
		}
	}
	l.byOffset[file], l.byEnd[file] = off, end
}

// fnRef is a function the follower reads: its file, its syntax (nil when
// nothing was captured: it uses no parameter in a read form) and its
// function scope.
type fnRef struct {
	file  string
	fn    *facts.ShapeFunction
	scope int
	step  routeshape.Step
}

// scopeOf is the route scope of the function starting at offset (0 when
// none is found, which resolves names at module level only).
func (l *shapeLinker) scopeOf(file string, offset int) int {
	data := l.r.files[file].Routes
	if data == nil {
		return 0
	}
	for i, s := range data.Scopes {
		if s.Function && s.Start == offset {
			return i
		}
	}
	return 0
}

// byIdentity finds a function node and its shape syntax.
func (l *shapeLinker) byIdentity(identity string) (fnRef, bool) {
	kind, file, _, err := contract.ParseIdentity(identity)
	if err != nil || (kind != "function" && kind != "method") {
		return fnRef{}, false
	}
	for _, n := range l.r.files[file].Nodes {
		if n.Identity != identity || n.Span == nil {
			continue
		}
		l.index(file)
		ref := fnRef{file: file, step: routeshape.Step{File: file, Line: n.Span.StartLine, Function: n.QualifiedName}}
		if f, ok := l.byEnd[file][[2]int{n.Span.EndLine, n.Span.EndColumn}]; ok {
			if f == nil {
				return fnRef{}, false
			}
			ref.fn = f
			ref.scope = l.scopeOf(file, f.Offset)
		}
		return ref, true
	}
	return fnRef{}, false
}

// byOffsetRef finds an inline function by its start byte.
func (l *shapeLinker) byOffsetRef(file string, offset int, name string, line int) fnRef {
	l.index(file)
	ref := fnRef{file: file, step: routeshape.Step{File: file, Line: line, Function: name}}
	if f := l.byOffset[file][offset]; f != nil {
		ref.fn = f
		ref.step.Line = f.Line
		ref.scope = l.scopeOf(file, offset)
	}
	return ref
}

// routeInfo is what one route's shape is read from.
type routeInfo struct {
	node      *contract.Node
	framework string
	handler   *fnRef
	// handlerNote says why the handler was not read.
	handlerNote string
	call        *facts.RouteCall
	own         []facts.RouteExpr
	libs        map[string]middlewareUse
	// unread names middleware before the handler that no row reads (a
	// function that is not a known library, or middleware that is not
	// proven): it may set a status (SH-E25).
	unread string
	// static says why a Next.js route's request is emptied by its route
	// segment config (SH-N24): no name read and no response is claimed.
	static string
	// staticBody marks dynamic = 'error', which also refuses body reads.
	staticBody bool
	// answered names a next.config rule that answers every request to the
	// route before its handler (SH-N24): no response is claimed.
	answered string
}

// middlewareUse is a proven middleware of a known library before the
// route, with where it is registered.
type middlewareUse struct {
	file string
	line int
	name string
}

func (l *shapeLinker) route(n *contract.Node, framework string) *routeshape.Shape {
	shape := routeshape.New(l.depth)
	info := routeInfo{node: n, framework: framework, libs: map[string]middlewareUse{}}
	if framework == "express" {
		l.expressHandler(&info)
	} else {
		l.pagesHandler(&info)
	}
	b := &shapeBuilder{l: l, info: &info, shape: shape, sr: &schemaReader{r: l.r, models: newModelSet()}, notes: map[string][]string{}, replaced: map[string]bool{}, assigned: map[string]map[string]bool{}}
	b.path()
	if info.handler == nil {
		b.tag("SH-E36")
		reason := "handler not read: " + info.handlerNote
		for _, sec := range []*routeshape.Section{&shape.Query.Section, &shape.Header.Section, &shape.Cookie.Section, &shape.Body.Section, &shape.Responses.Section} {
			sec.Reason = reason
		}
		return shape
	}
	b.middleware()
	b.request()
	b.responses()
	b.finish()
	b.security()
	return shape
}

// expressHandler finds the registration call of an Express route, its
// handler and its own middleware.
func (l *shapeLinker) expressHandler(info *routeInfo) {
	n := info.node
	data := l.r.files[n.File].Routes
	if data == nil || n.Span == nil {
		info.handlerNote = "registration not found"
		return
	}
	method, _ := n.Attributes["method"].(string)
	for i := range data.Calls {
		c := &data.Calls[i]
		m := strings.ToUpper(c.Method)
		if c.Method == "all" {
			m = "ANY"
		}
		if c.Span != *n.Span || m != method {
			continue
		}
		args := c.Args
		if c.Pattern == nil && len(args) > 0 {
			args = args[1:]
		}
		if len(args) == 0 {
			break
		}
		info.call = c
		h := args[len(args)-1]
		info.own = args[:len(args)-1]
		if h.Kind == "arrow_function" || h.Kind == "function_expression" {
			ref := l.byOffsetRef(n.File, h.Offset, "<inline handler>", n.Span.StartLine)
			info.handler = &ref
			return
		}
		if identity, _ := n.Attributes["handler"].(string); identity != "" {
			if ref, ok := l.byIdentity(identity); ok {
				info.handler = &ref
				return
			}
			info.handlerNote = "handler syntax not found"
			return
		}
		info.handlerNote = "handler " + whitespace(h.Text) + " is not a proven function"
		return
	}
	if info.handlerNote == "" {
		info.handlerNote = "registration not found"
	}
}

// pagesHandler finds the default-exported handler of a Pages Router API
// file.
func (l *shapeLinker) pagesHandler(info *routeInfo) {
	n := info.node
	if identity, _ := n.Attributes["handler"].(string); identity != "" {
		if ref, ok := l.byIdentity(identity); ok {
			info.handler = &ref
			return
		}
		info.handlerNote = "handler syntax not found"
		return
	}
	data := l.r.files[n.File].Routes
	if data != nil && len(data.Scopes) > 0 {
		if b, ok := data.Scopes[0].Bindings["@default"]; ok && (b.Value.Kind == "arrow_function" || b.Value.Kind == "function_expression") {
			line := 1
			if n.Span != nil {
				line = n.Span.StartLine
			}
			ref := l.byOffsetRef(n.File, b.Value.Offset, "default", line)
			info.handler = &ref
			return
		}
	}
	expression, _ := n.Attributes["handler_expression"].(string)
	info.handlerNote = "default export " + expression + " is not a proven function"
}

// shapeBuilder accumulates one route's sections.
type shapeBuilder struct {
	l     *shapeLinker
	info  *routeInfo
	shape *routeshape.Shape
	sr    *schemaReader
	// notes are the reasons per section.
	notes map[string][]string
	// pathNames are the path parameter names of the template (Pages: they
	// arrive in req.query).
	pathNames map[string]bool
	// refs are the functions read, by follower step.
	refs map[routeshape.Step]fnRef
	// cookieUnproven marks a cookie read without cookie-parser (SH-E9).
	cookieUnproven bool
	// replaced and assigned name the sections whose request object, or
	// fields of it, are written in code: those reads are not claimed.
	replaced map[string]bool
	assigned map[string]map[string]bool
	// rawNoted names the upper-case req.headers keys already noted.
	rawNoted map[string]bool
	// sessions are the next-auth session reads of a Next.js route (T029
	// SH-S23).
	sessions []secSource
}

func (b *shapeBuilder) note(section, reason string) {
	for _, r := range b.notes[section] {
		if r == reason {
			return
		}
	}
	b.notes[section] = append(b.notes[section], reason)
}

func (b *shapeBuilder) prov(rule string, kind routeshape.Kind, file string, line int, chain []routeshape.Step) routeshape.Provenance {
	rule = b.row(rule)
	tagShape(rule)
	if line < 1 {
		line = 1
	}
	c := append([]routeshape.Step{}, chain...)
	return routeshape.Provenance{Kind: kind, Rule: rule, File: file, Line: line, Chain: c, Confidence: 1}
}

var exTemplateParam = regexp.MustCompile(`\{(\*?)([^}]*)\}`)

// path reads the path parameters from the composed templates (SH-E1,
// SH-E2, SH-E3).
func (b *shapeBuilder) path() {
	n := b.info.node
	b.pathNames = map[string]bool{}
	var entries []struct {
		Template string `json:"template"`
		Optional bool   `json:"optional"`
	}
	if data, err := json.Marshal(n.Attributes["paths"]); err == nil {
		_ = json.Unmarshal(data, &entries)
	}
	sec := &b.shape.Path
	if len(entries) == 0 {
		sec.Reason = "the route has no composed path"
		return
	}
	line := 1
	if n.Span != nil {
		line = n.Span.StartLine
	}
	count := map[string]int{}
	star := map[string]bool{}
	var order []string
	partial := false
	for _, e := range entries {
		if e.Optional {
			partial = true
		}
		seen := map[string]bool{}
		for _, m := range exTemplateParam.FindAllStringSubmatch(e.Template, -1) {
			name := m[2]
			if name == "" {
				partial = true
				b.note(routeshape.SectionPath, "an unnamed wildcard (req.params[0]) is not an item")
				continue
			}
			if !seen[name] {
				seen[name] = true
				if count[name] == 0 {
					order = append(order, name)
				}
				count[name]++
				star[name] = star[name] || m[1] == "*"
			}
		}
	}
	for _, name := range order {
		b.pathNames[name] = true
		if count[name] != len(entries) {
			partial = true
			b.note(routeshape.SectionPath, "parameter "+name+" is in some composed paths only")
			continue
		}
		p := routeshape.Param{Name: name, Access: routeshape.Declared, Presence: routeshape.Required}
		rule := "SH-E1"
		if b.info.framework == "nextjs_pages" {
			rule = "SH-E3"
		}
		switch {
		case !star[name]:
			p.Schema = &routeshape.Schema{Type: routeshape.TypeString}
		case b.info.framework == "nextjs_pages" || b.info.framework == "nextjs" || b.l.r.expressMajor(n.File) == 5:
			items := routeshape.Schema{Type: routeshape.TypeString}
			p.Schema = &routeshape.Schema{Type: routeshape.TypeArray, Items: &items}
		}
		p.Provenance = b.prov(rule, routeshape.Fact, n.File, line, nil)
		sec.Items = append(sec.Items, p)
	}
	for _, e := range entries {
		if e.Optional {
			b.note(routeshape.SectionPath, "an optional segment: some parameters are absent on some paths")
			break
		}
	}
	evidence := b.prov("SH-E2", routeshape.Fact, n.File, line, nil)
	switch {
	case partial && len(sec.Items) > 0:
		sec.State = routeshape.Partial
	case partial:
		sec.State = routeshape.NotRead
	case len(sec.Items) == 0:
		sec.State = routeshape.ProvablyNone
		sec.Evidence = &evidence
	default:
		ev := b.prov("SH-E1", routeshape.Fact, n.File, line, nil)
		if b.info.framework == "nextjs_pages" {
			ev = b.prov("SH-E3", routeshape.Fact, n.File, line, nil)
		}
		sec.State = routeshape.Complete
		sec.Evidence = &ev
	}
	if sec.State == routeshape.Partial || sec.State == routeshape.NotRead {
		sec.Reason = strings.Join(b.notes[routeshape.SectionPath], "; ")
	}
}

// middleware classifies the middleware before the route: mounted
// (attribute mounted_middleware), the route's own arguments, and the
// mounted entries that are only conditionally registered. A middleware
// of a known library, or a proven function whose response parameter is
// used only by sends that end the request (row SH-E25), leaves the
// handler's response as written; any other middleware is unread.
func (b *shapeBuilder) middleware() {
	n := b.info.node
	if b.info.framework != "express" {
		return
	}
	unread := func(reason string) {
		if b.info.unread == "" {
			b.info.unread = reason
		}
	}
	// at finds the argument of the use call at span written as name.
	at := func(file string, span contract.Span, name string) (facts.RouteCall, facts.RouteExpr, bool) {
		if data := b.l.r.files[file].Routes; data != nil {
			for _, c := range data.Calls {
				if c.Span != span || c.Method != "use" {
					continue
				}
				for _, a := range c.Args {
					if whitespace(a.Text) == name {
						return c, a, true
					}
				}
			}
		}
		return facts.RouteCall{}, facts.RouteExpr{}, false
	}
	if unknown, ok := n.Attributes["mounted_middleware_unknown"].([]map[string]any); ok {
		for _, m := range unknown {
			reason, _ := m["reason"].(string)
			file, _ := m["file"].(string)
			span, _ := m["span"].(contract.Span)
			name, _ := m["expression"].(string)
			// A middleware that may or may not run before the route is
			// harmless when it would be harmless running (decision of the
			// SH-E review): known libraries and proven harmless functions.
			maybe := strings.HasPrefix(reason, "conditional registration") || reason == "declared on some mount chains only" || strings.HasPrefix(reason, "order relative to the route not proven")
			if maybe {
				if c, a, found := at(file, span, name); found {
					if b.classify(file, c.Scope, a, span.StartLine, false) {
						continue
					}
					if why := b.harmless(file, c.Scope, a, span.StartLine); why == "" {
						continue
					}
				}
			}
			unread("middleware mounted before the route is not proven (" + reason + ")")
		}
	}
	if mounted, ok := n.Attributes["mounted_middleware"].([]map[string]any); ok {
		for _, m := range mounted {
			file, _ := m["file"].(string)
			span, _ := m["span"].(contract.Span)
			name, _ := m["name"].(string)
			c, a, found := at(file, span, name)
			switch {
			case !found:
				unread(fmt.Sprintf("mounted middleware %s (%s:%d) is not found", name, file, span.StartLine))
			case b.classify(file, c.Scope, a, span.StartLine, true):
			default:
				if why := b.harmless(file, c.Scope, a, span.StartLine); why != "" {
					unread(fmt.Sprintf("mounted middleware %s (%s:%d) %s", name, file, span.StartLine, why))
				}
			}
		}
	}
	if b.info.call != nil {
		for _, a := range b.info.own {
			if b.classify(n.File, b.info.call.Scope, a, b.info.call.Span.StartLine, true) {
				continue
			}
			if why := b.harmless(n.File, b.info.call.Scope, a, b.info.call.Span.StartLine); why != "" {
				unread(fmt.Sprintf("route middleware %s (%s:%d) %s", whitespace(a.Text), n.File, b.info.call.Span.StartLine, why))
			}
		}
	}
	if listed, ok := n.Attributes["middleware"].([]string); ok && len(listed) != len(b.info.own) {
		unread("route middleware chained with route(p).all is not read")
	}
}

// harmless reads a middleware expression that is no known library: an
// inline function, a proven function, or the one function a proven
// factory returns (`isAuthenticated()`). It returns "" when the function
// leaves the handler's response as written: it does not use its response
// parameter, or uses it only for sends that end the request (no next()
// can follow on the same path); else why not.
func (b *shapeBuilder) harmless(file string, scope int, e facts.RouteExpr, line int) string {
	f, s, d := b.l.r.deref(file, scope, e, 0)
	var ref fnRef
	switch d.Kind {
	case "arrow_function", "function_expression":
		ref = b.l.byOffsetRef(f, d.Offset, whitespace(e.Text), line)
	case "identifier", "member_expression":
		t, stop, _ := b.target(fnRef{file: f, scope: s}, facts.ShapeEvent{Callee: shapeCallee(d)})
		if stop != "" {
			return "is not a proven function"
		}
		ref = t
	case "call_expression":
		if d.Object == nil {
			return "is not a proven function"
		}
		t, stop, _ := b.target(fnRef{file: f, scope: s}, facts.ShapeEvent{Callee: shapeCallee(*d.Object)})
		if stop != "" || t.fn == nil || t.fn.ReturnFn == 0 {
			return "is a call whose returned function is not proven"
		}
		ref = b.l.byOffsetRef(t.file, t.fn.ReturnFn, whitespace(e.Text), line)
	default:
		return "is not a proven function"
	}
	fn := ref.fn
	if fn == nil || len(fn.Params) < 2 {
		return ""
	}
	if fn.Params[1].Name == "" {
		return "destructures its response parameter"
	}
	next := -1
	if len(fn.Params) > 2 && fn.Params[2].Name != "" {
		next = 2
	}
	for i, ev := range fn.Events {
		if ev.Param != 1 {
			continue
		}
		switch {
		case (ev.Kind == "read" || ev.Kind == "assign") && len(ev.Path) > 0 && ev.Path[0] == "locals":
		case ev.Kind == "call" && len(ev.Path) == 0 && sends(ev.Methods) && ending(fn, i, next):
		default:
			return fmt.Sprintf("uses the response other than by a send that ends the request (%s:%d)", ref.file, ev.Line)
		}
	}
	return ""
}

// shapeCallee converts a name or a member of a name to the callee form
// the call target resolver reads.
func shapeCallee(e facts.RouteExpr) *facts.ShapeExpr {
	switch {
	case e.Kind == "identifier":
		return &facts.ShapeExpr{Kind: "identifier", Name: e.Name}
	case e.Kind == "member_expression" && e.Object != nil && e.Object.Kind == "identifier":
		return &facts.ShapeExpr{Kind: "member_expression", Name: e.Name, Object: &facts.ShapeExpr{Kind: "identifier", Name: e.Object.Name}}
	}
	return &facts.ShapeExpr{Kind: "other"}
}

// sends reports a response method chain that ends in a sending method,
// with only status and chainable calls before it.
func sends(methods []facts.ShapeMethod) bool {
	for _, m := range methods {
		switch {
		case m.Name == "status" || chainable[m.Name]:
		case sendingMethod[m.Name]:
			return true
		default:
			return false
		}
	}
	return false
}

// sendingMethod are the response methods that send the response.
var sendingMethod = map[string]bool{"json": true, "send": true, "end": true, "jsonp": true, "render": true, "sendStatus": true, "redirect": true}

// ending reports that no call or pass of the next parameter can follow
// the event at i on its path: its if branch always exits, or no later
// event in the same top-level statement and branch, or in a later
// statement, uses next.
func ending(fn *facts.ShapeFunction, i, next int) bool {
	e := fn.Events[i]
	if e.ArmExits || next < 0 {
		return true
	}
	for _, ev := range fn.Events[i+1:] {
		reachable := e.Stmt == 0 || ev.Stmt > e.Stmt || ev.Stmt == e.Stmt && (e.Arm == 0 || ev.Arm == e.Arm)
		if reachable && ev.Param == next && (ev.Kind == "call" || ev.Kind == "pass") {
			return false
		}
	}
	return true
}

// errorExit reports an if branch that leaves the function only by a send
// of a literal 4xx or 5xx status, next(err) or a throw: every use there
// of a parameter other than param is one of those (or a read). A branch
// that may answer 1xx..3xx lets a request succeed without what follows.
func errorExit(fn *facts.ShapeFunction, stmt, arm, param int) bool {
	for _, e := range fn.Events {
		if e.Stmt != stmt || e.Arm != arm || e.Param < 0 || e.Param == param {
			continue
		}
		switch {
		case e.Kind == "read" || e.Kind == "keys":
		case e.Kind == "call" && errorSend(e):
		default:
			return false
		}
	}
	return true
}

// errorSend reports next(err) or a response chain that sends a literal
// 4xx or 5xx status.
func errorSend(e facts.ShapeEvent) bool {
	if len(e.Path) != 0 {
		return false
	}
	if len(e.Methods) == 1 && e.Methods[0].Name == "" {
		return len(e.Methods[0].Args) > 0
	}
	status := ""
	literal := func(m facts.ShapeMethod) string {
		if len(m.Args) >= 1 && m.Args[0].Kind == "number" {
			return m.Args[0].Name
		}
		return ""
	}
	for _, m := range e.Methods {
		switch {
		case m.Name == "status":
			status = literal(m)
		case m.Name == "sendStatus":
			status = literal(m)
			return errorStatus(status)
		case chainable[m.Name]:
		case sendingMethod[m.Name] && m.Name != "redirect":
			return errorStatus(status)
		default:
			return false
		}
	}
	return false
}

func errorStatus(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 400 && n <= 599
}

// classify records a middleware expression of a known library and
// reports whether it is one (a body parser, cookie-parser, the OpenAPI
// validator, celebrate or an express-validator export: none of them sets
// a status on a request it passes on).
func (b *shapeBuilder) classify(file string, scope int, e facts.RouteExpr, line int, record bool) bool {
	f, s, d := b.l.r.deref(file, scope, e, 0)
	call := d
	for call.Kind == "call_expression" && call.Object != nil && call.Object.Kind == "member_expression" && call.Object.Object != nil && call.Object.Object.Kind == "call_expression" {
		call = *call.Object.Object
	}
	if call.Kind != "call_expression" || call.Object == nil {
		return false
	}
	v := b.l.r.resolve(f, s, *call.Object, 0, nil)
	if v.kind != "import" {
		return false
	}
	name := strings.TrimPrefix(v.name, "default.")
	use := middlewareUse{file: f, line: line, name: whitespace(e.Text)}
	if !record {
		// Known without recording: a middleware that may not run proves
		// no parser and declares no schema.
		b = &shapeBuilder{l: b.l, info: &routeInfo{libs: map[string]middlewareUse{}}, shape: routeshape.New(b.l.depth), sr: b.sr, notes: map[string][]string{}, replaced: map[string]bool{}, assigned: map[string]map[string]bool{}}
	}
	switch v.file {
	case "morgan", "compression", "cors", "helmet":
		// Logging, compression and headers: none sets a status on a
		// request it passes on (cors ends only OPTIONS preflights).
		return true
	case "express":
		if name == "json" || name == "urlencoded" || name == "text" || name == "raw" {
			b.info.libs["parser"] = use
			if name == "json" {
				b.info.libs["json"] = use
			}
			return true
		}
	case "body-parser":
		if name == "json" || name == "urlencoded" || name == "text" || name == "raw" || name == "default" {
			b.info.libs["parser"] = use
			if name == "json" {
				b.info.libs["json"] = use
			}
			return true
		}
	case "cookie-parser":
		b.info.libs["cookie-parser"] = use
		return true
	case "express-openapi-validator":
		b.info.libs["openapi"] = use
		return true
	case "celebrate":
		if name == "celebrate" && len(call.Args) >= 1 {
			b.celebrate(f, s, call.Args[0], len(call.Args) > 1, line)
			return true
		}
	case "express-validator":
		b.expressValidator(name, call, f, line)
		return true
	}
	return false
}

// celebrate reads a celebrate() schema map (SH-E15). options marks a
// second argument (Joi options, not read): presence and extra keys become
// unknown as for a Joi validate with options (SH-E14).
func (b *shapeBuilder) celebrate(file string, scope int, e facts.RouteExpr, options bool, line int) {
	f, s, d := b.l.r.deref(file, scope, e, 0)
	if d.Kind != "object" {
		return
	}
	for key, value := range d.Properties {
		section := ""
		switch key {
		case "body", "[Segments.BODY]":
			section = routeshape.SectionBody
		case "query", "[Segments.QUERY]":
			section = routeshape.SectionQuery
		default:
			continue
		}
		prefs := joiPrefs{relaxed: options}
		fd, lib, ok := b.sr.readValidator(f, s, value, prefs, 0)
		if !ok {
			// A plain object of Joi schemas: celebrate compiles it.
			obj := b.sr.objectFields(f, s, value, libJoi, prefs, 0)
			if obj.Type != routeshape.TypeObject {
				continue
			}
			fd, lib, ok = field{schema: obj, presence: routeshape.Optional}, libJoi, true
		}
		if lib != libJoi {
			continue
		}
		if options {
			relax(&fd.schema)
		}
		if fd.schema.Type == routeshape.TypeObject {
			// celebrate's Joi options are not read: unknown keys may pass.
			fd.schema.Additional = routeshape.AdditionalUnknown
		}
		if section == routeshape.SectionBody && b.joiRoot(&fd, file, line) {
			b.tag("SH-E19")
			s := fd.schema
			exUnrequire(&s)
			b.validatedAs(section, s, b.prov("SH-E15", routeshape.Intended, file, line, nil), routeshape.Intended)
			continue
		}
		b.validated(section, fd.schema, b.prov("SH-E15", routeshape.Fact, file, line, nil))
	}
}

// expressValidator reads an express-validator chain on the route
// (SH-E16): the field it names is read, never typed or required.
func (b *shapeBuilder) expressValidator(name string, call facts.RouteExpr, file string, line int) {
	section := map[string]string{"body": routeshape.SectionBody, "query": routeshape.SectionQuery, "header": routeshape.SectionHeader}[name]
	if section == "" {
		return
	}
	prov := b.prov("SH-E16", routeshape.ReadInHandler, file, line, nil)
	if len(call.Args) == 0 || call.Args[0].Kind != "literal" {
		b.note(section, "an express-validator chain with a field name not read")
		return
	}
	field := call.Args[0].Name
	if i := strings.IndexAny(field, ".["); i >= 0 {
		field = field[:i]
	}
	if field == "" || strings.Contains(field, "*") {
		b.note(section, "an express-validator chain with a wildcard field")
		return
	}
	if section == routeshape.SectionBody {
		b.bodyField(field, prov)
	} else {
		b.param(section, field, prov)
	}
}

// Request sections and the request object key each one reads.
var requestKeys = map[string][]string{
	routeshape.SectionQuery:  {"query"},
	routeshape.SectionHeader: {"headers"},
	routeshape.SectionCookie: {"cookies", "signedCookies"},
	routeshape.SectionBody:   {"body"},
}

// request follows the request parameter for each request section.
func (b *shapeBuilder) request() {
	h := b.info.handler
	if h.fn == nil || len(h.fn.Params) == 0 || h.fn.Params[0].Name == "" && h.fn.Params[0].Keys == nil {
		for _, s := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody} {
			b.note(s, "the handler reads no request property")
		}
		return
	}
	for _, section := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody} {
		v := &requestVisitor{b: b, section: section}
		res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: valueKey(0, "req")}, b.l.depth, v)
		if err != nil {
			continue
		}
		b.addChains(section, res)
	}
	b.handlerTypes()
}

func (b *shapeBuilder) addChains(section string, res routeshape.Result) {
	for _, c := range res.Chains {
		if c.Stop != routeshape.StopEnded && c.Stop != routeshape.StopJoined && c.Stop != routeshape.StopCycle {
			at := ""
			if c.At != nil {
				at = fmt.Sprintf(" at %s:%d", c.At.File, c.At.Line)
			}
			if c.Stop == routeshape.StopDepthCap {
				b.note(section, c.Detail+at+": raise index.shape_depth to read further")
			} else {
				b.note(section, "a call was not followed ("+string(c.Stop)+at+")")
			}
		}
	}
	b.shape.Follow.Chains = append(b.shape.Follow.Chains, routeshape.Records(section, res)...)
}

func valueKey(param int, role string) string { return strconv.Itoa(param) + "|" + role }

func exParseValue(v string) (int, string) {
	i, role, _ := strings.Cut(v, "|")
	n, _ := strconv.Atoi(i)
	role, _, _ = strings.Cut(role, "|")
	return n, role
}

// unenforced reports a followed value reached through a call that does
// not stop the request when it throws (inside a try block, not a
// top-level statement, an async callee not awaited, or after a possible
// send): an error thrown below it may not end the request.
func unenforced(v string) bool { return strings.HasSuffix(v, "|u") }

// mayHaveSent reports an event of fn in [from, to) that uses a parameter
// other than param in a form that may send the response or pass the
// request on (a method call, a pass or an assignment). An event inside a
// branch of a top-level if that always returns or throws does not count
// when that branch leaves only by an error (errorExit): code after the if
// runs only when the branch did not, and the branch lets no request
// succeed.
func mayHaveSent(fn *facts.ShapeFunction, from, to, param int) bool {
	for j := from; j < to && j < len(fn.Events); j++ {
		e := fn.Events[j]
		if e.Param >= 0 && e.Param != param && (e.Kind == "call" || e.Kind == "pass" || e.Kind == "assign") {
			if e.ArmExits && e.Stmt > 0 && errorExit(fn, e.Stmt, e.Arm, param) {
				continue
			}
			return true
		}
	}
	return false
}

// frames maps follower steps back to functions.
func (b *shapeBuilder) lookup(step routeshape.Step) (fnRef, bool) {
	h := b.info.handler
	if step == h.step {
		return *h, true
	}
	if b.refs == nil {
		return fnRef{}, false
	}
	r, ok := b.refs[step]
	return r, ok && r.file != ""
}

// exRolePath is the request path a role stands for.
func exRolePath(role string) []string {
	if role == "req" || role == "res" || role == "next" {
		return nil
	}
	return []string{role}
}

type requestVisitor struct {
	b       *shapeBuilder
	section string
	// raw marks names read from req.headers itself (not req.get).
	raw bool
}

// Visit reads the request uses of one function for one section.
func (v *requestVisitor) Visit(_ context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	b := v.b
	ref, ok := b.lookup(frame.Function)
	if !ok {
		return nil, fmt.Errorf("function %s not indexed", frame.Function.Function)
	}
	if ref.fn == nil {
		return nil, nil
	}
	param, role := exParseValue(frame.Value)
	keys := requestKeys[v.section]
	pages := b.info.framework == "nextjs_pages"
	var calls []routeshape.Call
	for idx, e := range ref.fn.Events {
		if e.Param != param {
			continue
		}
		full := append(append([]string{}, exRolePath(role)...), e.Path...)
		prov := func(rule string) routeshape.Provenance {
			return b.prov(rule, routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
		}
		key := ""
		if len(full) > 0 && exContains(keys, full[0]) {
			key = full[0]
		}
		// Node lowercases the keys of req.headers (SH-E7).
		v.raw = key == "headers"
		switch e.Kind {
		case "read":
			switch {
			case len(full) == 0 && e.Whole:
				b.note(v.section, fmt.Sprintf("the request is read with a computed key (%s:%d)", ref.file, e.Line))
			case key == "":
			case len(full) == 1 && e.Bound:
			case len(full) == 1:
				b.whole(v.section, ref.file, e.Line)
			default:
				v.read(full[1], prov)
			}
		case "keys":
			switch {
			case key == "":
			case len(full) == 1:
				for _, k := range e.Keys {
					v.read(k, prov)
				}
				if e.Open {
					b.whole(v.section, ref.file, e.Line)
				}
			default:
				v.read(full[1], prov)
			}
		case "call":
			name := ""
			if len(e.Methods) > 0 {
				name = e.Methods[0].Name
			}
			switch {
			case len(full) == 0 && v.section == routeshape.SectionHeader && !pages && (name == "get" || name == "header"):
				if args := e.Methods[0].Args; len(args) == 1 && args[0].Kind == "literal" {
					v.raw = false
					v.read(args[0].Name, func(string) routeshape.Provenance {
						return b.prov("SH-E7", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
					})
				} else {
					b.note(v.section, fmt.Sprintf("req.%s with a name not read (%s:%d)", name, ref.file, e.Line))
				}
			case key == "":
			case len(full) == 1 && v.section == routeshape.SectionHeader && name == "get":
				if args := e.Methods[0].Args; len(args) == 1 && args[0].Kind == "literal" {
					v.raw = false
					v.read(args[0].Name, prov)
				} else {
					b.whole(v.section, ref.file, e.Line)
				}
			case len(full) == 1:
				b.whole(v.section, ref.file, e.Line)
			default:
				v.read(full[1], prov)
			}
		case "pass":
			switch {
			case len(full) == 0 && e.Transformed:
				calls = append(calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "the request is spread into another value"})
			case len(full) == 0:
				calls = append(calls, b.callFor(ref, idx, frame, "req", v.section))
			case key == "":
			case len(full) == 1 && e.Transformed:
				b.tag("SH-E11")
				calls = append(calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "req." + key + " is spread into another value"})
			case len(full) == 1:
				if b.validator(v.section, ref, idx, frame) {
					continue
				}
				calls = append(calls, b.callFor(ref, idx, frame, key, v.section))
			default:
				v.read(full[1], prov)
				if v.section == routeshape.SectionBody && b.provenTarget(ref, e) {
					b.tag("SH-E11")
					calls = append(calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "a member of the body (" + strings.Join(full, ".") + ") is passed on"})
				}
			}
		case "cast":
			if key != "" && len(full) == 1 && e.Type != nil {
				b.intended(v.section, *e.Type, ref.file, b.prov("SH-E18", routeshape.Intended, ref.file, e.Line, frame.Chain))
			}
		case "assign":
			switch {
			case len(full) == 0 || key != "" && len(full) == 1:
				// The request object is replaced: later reads are not
				// what the client sent (names read are dropped).
				b.tag("SH-E6")
				b.replaced[v.section] = true
				b.note(v.section, fmt.Sprintf("the request value is replaced in code (%s:%d): names read are not claimed", ref.file, e.Line))
			case key != "":
				if b.assigned[v.section] == nil {
					b.assigned[v.section] = map[string]bool{}
				}
				b.assigned[v.section][full[1]] = true
			}
		}
	}
	return calls, nil
}

func exContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func site(ref fnRef, e facts.ShapeEvent) routeshape.Step {
	name := "call"
	if e.Callee != nil {
		name = exprText(*e.Callee)
	}
	return routeshape.Step{File: ref.file, Line: e.Line, Function: name}
}

// exprText renders a compact expression as written (callees).
func exprText(e facts.ShapeExpr) string {
	switch e.Kind {
	case "identifier", "this":
		return e.Name
	case "member_expression":
		if e.Object != nil {
			return exprText(*e.Object) + "." + e.Name
		}
	case "call_expression":
		if e.Object != nil {
			return exprText(*e.Object) + "(...)"
		}
	case "new":
		return "new"
	}
	return e.Kind
}

// read records one name read of the visitor's section.
func (v *requestVisitor) read(name string, prov func(string) routeshape.Provenance) {
	b := v.b
	switch v.section {
	case routeshape.SectionQuery:
		if b.info.framework == "nextjs_pages" && b.pathNames[name] {
			b.tag("SH-E5")
			return
		}
		b.param(v.section, name, prov("SH-E4"))
	case routeshape.SectionHeader:
		p := prov("SH-E7")
		if v.raw && name != strings.ToLower(name) {
			// Node lowercases header names in req.headers: this read
			// never matches a header. Noted once, at the first read.
			if b.rawNoted[name] {
				return
			}
			if b.rawNoted == nil {
				b.rawNoted = map[string]bool{}
			}
			b.rawNoted[name] = true
			b.note(v.section, fmt.Sprintf("req.headers read with %q never matches: Node lowercases header names (%s:%d)", name, p.File, p.Line))
			return
		}
		b.param(v.section, name, p)
	case routeshape.SectionCookie:
		if b.info.framework == "express" && b.info.libs["cookie-parser"].name == "" {
			b.tag("SH-E9")
			b.cookieUnproven = true
			return
		}
		b.param(v.section, name, prov("SH-E8"))
	case routeshape.SectionBody:
		b.bodyField(name, prov("SH-E10"))
	}
}

// whole notes a request object read as a whole (SH-E6).
func (b *shapeBuilder) whole(section, file string, line int) {
	b.tag("SH-E6")
	b.note(section, fmt.Sprintf("read as a whole or with a computed key (%s:%d)", file, line))
}

// param adds a parameter read in code (or declared by a validator chain).
func (b *shapeBuilder) param(section, name string, prov routeshape.Provenance) {
	sec := b.paramSection(section)
	for _, p := range sec.Items {
		same := p.Name == name
		if section == routeshape.SectionHeader {
			same = strings.EqualFold(p.Name, name)
		}
		if same && p.Provenance.Kind == prov.Kind {
			return
		}
	}
	sec.Items = append(sec.Items, routeshape.Param{Name: name, Access: routeshape.ReadByHandler, Presence: routeshape.PresenceUnknown, Provenance: prov})
}

func (b *shapeBuilder) paramSection(section string) *routeshape.ParamSection {
	switch section {
	case routeshape.SectionQuery:
		return &b.shape.Query
	case routeshape.SectionHeader:
		return &b.shape.Header
	case routeshape.SectionCookie:
		return &b.shape.Cookie
	}
	return &b.shape.Path
}

// bodyField adds a body field read in code to the read_in_handler body.
func (b *shapeBuilder) bodyField(name string, prov routeshape.Provenance) {
	items := b.shape.Body.Items
	for i := range items {
		if items[i].Provenance.Kind != routeshape.ReadInHandler || items[i].ContentType != nil {
			continue
		}
		for _, f := range items[i].Schema.Fields {
			if f.Name == name {
				return
			}
		}
		items[i].Schema.Fields = append(items[i].Schema.Fields, routeshape.Field{Name: name, Presence: routeshape.PresenceUnknown, Schema: exUnknownSchema("type not read")})
		return
	}
	b.shape.Body.Items = append(b.shape.Body.Items, routeshape.Content{Schema: &routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown, Fields: []routeshape.Field{{Name: name, Presence: routeshape.PresenceUnknown, Schema: exUnknownSchema("type not read")}}}, Provenance: prov})
}

// validator reads a zod or Joi validation of a request object (SH-E12,
// SH-E13, SH-E14). It returns false when the callee is not one.
func (b *shapeBuilder) validator(section string, ref fnRef, idx int, frame routeshape.Frame) bool {
	e := ref.fn.Events[idx]
	if e.Callee == nil || e.Callee.Kind != "member_expression" || e.Callee.Object == nil || e.Arg != 0 {
		return false
	}
	method := e.Callee.Name
	switch method {
	case "parse", "parseAsync", "safeParse", "safeParseAsync", "validate", "validateAsync":
	default:
		return false
	}
	prefs := joiPrefs{relaxed: e.Argc > 1}
	fd, lib, ok := b.sr.readValidator(ref.file, ref.scope, e.Callee.Object.RouteExpr(), prefs, 0)
	if !ok {
		return false
	}
	if b.next() && lib != libZod {
		// Next.js rows read zod only (row D of the SH-N table).
		b.tag("D")
		b.note(section, fmt.Sprintf("a %s %s (%s:%d) is not read for Next.js", lib, method, ref.file, e.Line))
		return true
	}
	rule, throws := "", false
	switch {
	case lib == libZod && (method == "parse" || method == "parseAsync"):
		rule, throws = "SH-E12", true
	case lib == libZod && (method == "safeParse" || method == "safeParseAsync") && e.Test != "":
		rule = "SH-E13"
	case lib == libJoi && method == "validateAsync":
		rule, throws = "SH-E14", true
	case lib == libJoi && method == "validate" && e.Test == "error":
		rule = "SH-E14"
	default:
		// A validation whose outcome is not tested enforces nothing.
		b.tag("SH-E19")
		b.note(section, fmt.Sprintf("a %s %s whose result is not tested (%s:%d)", lib, method, ref.file, e.Line))
		return true
	}
	if lib == libJoi && e.Argc > 1 {
		relax(&fd.schema)
	}
	if section != routeshape.SectionBody && section != routeshape.SectionQuery {
		return true
	}
	// Enforcement holds only when a failure stops the request before
	// anything is sent: the call is a top-level statement of its function
	// (awaited when it returns a promise), nothing that may send the
	// response runs before it, and every call of the chain that reached it
	// is the same (a thrown error); or the result is tested at the top
	// level of the handler itself, before anything may send (a tested
	// result).
	async := method == "parseAsync" || method == "safeParseAsync" || method == "validateAsync"
	enforced := e.Top && !e.InTry && (!async || e.Awaited) && !b.mayHaveSent(ref, 0, idx, e.Param)
	if throws {
		enforced = enforced && !unenforced(frame.Value)
	} else {
		enforced = enforced && len(frame.Chain) == 0 && e.TestAt > idx && !b.mayHaveSent(ref, idx+1, e.TestAt, e.Param)
	}
	if section == routeshape.SectionBody && lib == libJoi && b.joiRoot(&fd, ref.file, e.Line) {
		enforced = false
	}
	if !enforced {
		b.tag("SH-E19")
		b.note(section, fmt.Sprintf("a %s %s at %s:%d may not stop the request (not a top-level statement before every send, not awaited, inside a try block, reached through such a call, or tested in a called function): its schema is intended", lib, method, ref.file, e.Line))
		s := fd.schema
		exUnrequire(&s)
		b.validatedAs(section, s, b.prov(rule, routeshape.Intended, ref.file, e.Line, frame.Chain), routeshape.Intended)
		return true
	}
	b.validated(section, fd.schema, b.prov(rule, routeshape.Fact, ref.file, e.Line, frame.Chain))
	return true
}

// joiRoot handles a Joi body schema whose root is not required(): Joi
// accepts undefined, and req.body is undefined without a body parser,
// under Express 5 when no parser matched, and on a Pages Router API
// route without a body; only Express 4 with a proven JSON parser sets
// req.body to {} first. Elsewhere the top-level presence is unknown, and
// on an Express route with no proven body parser at all the schema never
// sees a body (it reports true: the schema is not enforced).
func (b *shapeBuilder) joiRoot(fd *field, file string, line int) bool {
	if fd.presence == routeshape.Required || fd.schema.Type != routeshape.TypeObject {
		return false
	}
	express := b.info.framework == "express"
	if express && b.l.r.expressMajor(b.info.node.File) == 4 && b.info.libs["json"].name != "" {
		return false
	}
	for i := range fd.schema.Fields {
		fd.schema.Fields[i].Presence = routeshape.PresenceUnknown
	}
	if express && b.info.libs["parser"].name == "" {
		b.note(routeshape.SectionBody, fmt.Sprintf("the Joi schema at %s:%d is not required() at its root and no body parser is proven before the route: req.body may be undefined, which it accepts", file, line))
		return true
	}
	b.note(routeshape.SectionBody, fmt.Sprintf("the Joi schema at %s:%d is not required() at its root: a request without a JSON body is accepted, so presence is not claimed", file, line))
	return false
}

// exUnrequire makes required presence unknown (intended schemas are never
// required, SM-14).
func exUnrequire(s *routeshape.Schema) {
	for i := range s.Fields {
		if s.Fields[i].Presence == routeshape.Required {
			s.Fields[i].Presence = routeshape.PresenceUnknown
		}
		exUnrequire(&s.Fields[i].Schema)
	}
	if s.Items != nil {
		exUnrequire(s.Items)
	}
}

// relax makes presence and extra keys unknown: Joi options were passed.
func relax(s *routeshape.Schema) {
	if s.Type == routeshape.TypeObject {
		s.Additional = routeshape.AdditionalUnknown
		for i := range s.Fields {
			s.Fields[i].Presence = routeshape.PresenceUnknown
			relax(&s.Fields[i].Schema)
		}
	}
	if s.Items != nil {
		relax(s.Items)
	}
}

// validated stores a schema a validator enforces: a body variant, or one
// query parameter per field.
func (b *shapeBuilder) validated(section string, schema routeshape.Schema, prov routeshape.Provenance) {
	b.validatedAs(section, schema, prov, routeshape.Fact)
}

func (b *shapeBuilder) validatedAs(section string, schema routeshape.Schema, prov routeshape.Provenance, kind routeshape.Kind) {
	if b.info.framework == frameworkAction && section == routeshape.SectionBody {
		// A server action's arguments are sent as a list; the schema
		// describes argument 1 only (SH-N12, SH-N27).
		b.note(section, actionArgumentNote)
	}
	switch section {
	case routeshape.SectionBody:
		for _, it := range b.shape.Body.Items {
			if it.Provenance.Kind == kind {
				b.note(section, "more than one validated schema: only the first is shown")
				return
			}
		}
		s := schema
		b.shape.Body.Items = append(b.shape.Body.Items, routeshape.Content{Schema: &s, Provenance: prov})
	case routeshape.SectionQuery:
		if schema.Type != routeshape.TypeObject {
			b.note(section, "a validated query schema that is not an object")
			return
		}
		for _, f := range schema.Fields {
			s := f.Schema
			exists := false
			for _, p := range b.shape.Query.Items {
				exists = exists || p.Name == f.Name && p.Provenance.Kind == kind
			}
			if !exists {
				b.shape.Query.Items = append(b.shape.Query.Items, routeshape.Param{Name: f.Name, Access: routeshape.Declared, Presence: f.Presence, Schema: &s, Provenance: prov})
			}
		}
	}
}

// intended stores a TypeScript type of a request object (SH-E17, SH-E18):
// a body variant or query parameters of kind intended.
func (b *shapeBuilder) intended(section string, t facts.ShapeType, file string, prov routeshape.Provenance) {
	if opaque(t) {
		return
	}
	s := b.sr.tsSchema(file, t, routeshape.Intended, 0)
	switch section {
	case routeshape.SectionBody:
		for _, it := range b.shape.Body.Items {
			if it.Provenance.Kind == routeshape.Intended {
				return
			}
		}
		b.shape.Body.Items = append(b.shape.Body.Items, routeshape.Content{Schema: &s, Provenance: prov})
	case routeshape.SectionQuery:
		obj := s
		if obj.Type == routeshape.TypeRef {
			for _, m := range b.sr.models.models {
				if m.Name == obj.Ref {
					obj = m.Schema
				}
			}
		}
		if obj.Type != routeshape.TypeObject {
			return
		}
		for _, f := range obj.Fields {
			fs := f.Schema
			exists := false
			for _, p := range b.shape.Query.Items {
				exists = exists || p.Name == f.Name && p.Provenance.Kind == routeshape.Intended
			}
			if !exists {
				presence := f.Presence
				if presence == routeshape.Required {
					presence = routeshape.PresenceUnknown
				}
				b.shape.Query.Items = append(b.shape.Query.Items, routeshape.Param{Name: f.Name, Access: routeshape.Declared, Presence: presence, Schema: &fs, Provenance: prov})
			}
		}
	}
}

// opaque reports any, unknown, never, object, {} and type parameters
// that give no shape.
func opaque(t facts.ShapeType) bool {
	switch t.Kind {
	case "any", "unknown", "never", "object", "undefined", "other":
		return true
	case "object_type":
		return true
	}
	if t.Kind == "object" && len(t.Props) == 0 && !t.Open {
		return true
	}
	return false
}

// handlerTypes reads Request<P, ResBody, ReqBody, Query> on the handler's
// request parameter and RequestHandler<P, ResBody, ReqBody, Query> on the
// const that binds it (SH-E17).
func (b *shapeBuilder) handlerTypes() {
	h := b.info.handler
	if h.fn == nil || b.info.framework != "express" {
		return
	}
	read := func(t *facts.ShapeType, want string) {
		if t == nil || t.Kind != "name" || !b.expressType(h.file, t.Name, want) {
			return
		}
		prov := b.prov("SH-E17", routeshape.Intended, h.file, h.fn.Line, nil)
		if len(t.Args) >= 3 {
			b.intended(routeshape.SectionBody, t.Args[2], h.file, prov)
		}
		if len(t.Args) >= 4 {
			b.intended(routeshape.SectionQuery, t.Args[3], h.file, prov)
		}
	}
	if len(h.fn.Params) > 0 {
		read(h.fn.Params[0].Type, "Request")
	}
	read(h.fn.Decl, "RequestHandler")
}

// expressType reports whether a type name refers to want exported by
// express (or express-serve-static-core).
func (b *shapeBuilder) expressType(file, name, want string) bool {
	base, member, qualified := strings.Cut(name, ".")
	data := b.l.r.files[file].Routes
	if data == nil || len(data.Scopes) == 0 {
		return false
	}
	bind, ok := data.Scopes[0].Bindings[base]
	if !ok || bind.Module == "" || (bind.Module != "express" && bind.Module != "express-serve-static-core") {
		return false
	}
	if qualified {
		return member == want && (bind.Export == "default" || bind.Export == "*")
	}
	return bind.Export == want
}

// provenTarget reports whether a pass event's callee is a proven function.
func (b *shapeBuilder) provenTarget(ref fnRef, e facts.ShapeEvent) bool {
	_, stop, _ := b.target(ref, e)
	return stop == ""
}

// target resolves a pass event's callee to a proven function (SH-E35).
func (b *shapeBuilder) target(ref fnRef, e facts.ShapeEvent) (fnRef, routeshape.Stop, string) {
	if e.Callee == nil {
		return fnRef{}, routeshape.StopUnresolved, "no callee"
	}
	c := *e.Callee
	file := ref.file
	data := b.l.r.files[file].Routes
	if data == nil {
		return fnRef{}, routeshape.StopUnresolved, "no syntax"
	}
	rootName := ""
	switch {
	case c.Kind == "identifier":
		rootName = c.Name
	case c.Kind == "member_expression" && c.Object != nil && c.Object.Kind == "identifier":
		rootName = c.Object.Name
	default:
		return fnRef{}, routeshape.StopUnresolved, "callee " + exprText(c) + " is not a name or a member of a name"
	}
	if ref.fn != nil {
		for _, local := range ref.fn.Locals {
			if local == rootName {
				return fnRef{}, routeshape.StopUnresolved, rootName + " is a local binding"
			}
		}
		for _, p := range ref.fn.Params {
			if p.Name == rootName {
				return fnRef{}, routeshape.StopUnresolved, rootName + " is a parameter"
			}
		}
	}
	bind, at, ok := b.l.r.binding(file, ref.scope, rootName)
	if !ok {
		return fnRef{}, routeshape.StopUnresolved, rootName + " has no definition in the repository"
	}
	if bind.Module != "" {
		member := ""
		if c.Kind == "member_expression" {
			member = c.Name
		}
		x := b.l.modules().ImportBinding(file, bind.Module, bind.Export, member)
		if x.External {
			return fnRef{}, routeshape.StopExternal, "imported from " + bind.Module
		}
		if x.Ambiguous {
			return fnRef{}, routeshape.StopAmbiguous, "several definitions"
		}
		if x.Identity == "" {
			return fnRef{}, routeshape.StopUnresolved, exprText(c) + " not resolved"
		}
		return b.identityTarget(x.Identity)
	}
	if c.Kind == "member_expression" {
		// A member of a local object literal const: { create(x) {...} }.
		if bind.Value.Kind == "object" && !bind.Mutable && !bind.Value.Spread {
			if v, ok := bind.Value.Properties[c.Name]; ok {
				if v.Kind == "arrow_function" || v.Kind == "function_expression" {
					return b.offsetTarget(file, v.Offset, rootName+"."+c.Name), "", ""
				}
			}
			q := facts.Join(rootName, c.Name)
			for _, n := range b.l.r.files[file].Nodes {
				if n.Kind == "method" && n.QualifiedName == q {
					return b.identityTarget(n.Identity)
				}
			}
		}
		return fnRef{}, routeshape.StopUnresolved, "receiver " + rootName + " is not proven (an instance or another value)"
	}
	switch bind.Value.Kind {
	case "function":
		if bind.ReturnScope > 0 && bind.ReturnScope < len(data.Scopes) {
			start := data.Scopes[bind.ReturnScope].Start
			return b.offsetTarget(file, start, rootName), "", ""
		}
	case "ambiguous":
		return fnRef{}, routeshape.StopAmbiguous, rootName + " is declared more than once"
	case "arrow_function", "function_expression":
		if !bind.Mutable {
			return b.offsetTarget(file, bind.Value.Offset, rootName), "", ""
		}
	}
	_ = at
	return fnRef{}, routeshape.StopUnresolved, rootName + " is not a proven function"
}

func (l *shapeLinker) modules() *typescript.ModuleResolver { return l.r.modules }

// identityTarget is a proven function node by identity, never one of a
// group of repeated declarations.
func (b *shapeBuilder) identityTarget(identity string) (fnRef, routeshape.Stop, string) {
	for _, f := range b.l.r.files {
		for _, group := range f.Repeated {
			for _, id := range group {
				if id == identity {
					return fnRef{}, routeshape.StopAmbiguous, "several declarations of " + identity
				}
			}
		}
	}
	ref, ok := b.l.byIdentity(identity)
	if !ok {
		return fnRef{}, routeshape.StopUnresolved, identity + " is not a function with read syntax"
	}
	return b.register(ref), "", ""
}

func (b *shapeBuilder) offsetTarget(file string, offset int, name string) fnRef {
	ref := b.l.byOffsetRef(file, offset, name, 1)
	if ref.fn == nil {
		// Find the line from the scope's function node is not cached: a
		// function with no parameter use has no syntax; its line is 1.
		ref.step.Line = 1
	}
	return b.register(ref)
}

// register remembers a function for lookup by step. Two different
// functions with the same step are ambiguous and never read.
func (b *shapeBuilder) register(ref fnRef) fnRef {
	if b.refs == nil {
		b.refs = map[routeshape.Step]fnRef{}
	}
	if old, ok := b.refs[ref.step]; ok && (old.file != ref.file || old.fn != ref.fn) {
		b.refs[ref.step] = fnRef{}
		return ref
	}
	b.refs[ref.step] = ref
	return ref
}

// callFor builds the follower call for a request object passed on (the
// event at idx of ref's function).
func (b *shapeBuilder) callFor(ref fnRef, idx int, frame routeshape.Frame, role, section string) routeshape.Call {
	e := ref.fn.Events[idx]
	call := routeshape.Call{Site: site(ref, e)}
	t, stop, detail := b.target(ref, e)
	if stop != "" {
		b.tag("SH-E35")
		call.Stop, call.Detail = stop, detail
		return call
	}
	if r, ok := b.refs[t.step]; ok && r.file == "" {
		call.Stop, call.Detail = routeshape.StopAmbiguous, "two functions at one place"
		return call
	}
	b.tag("SH-E35")
	call.Target = t.step
	// A throw below this call stops the request only when this call is a
	// top-level statement (awaited when the callee is async or returns a
	// promise), outside a try block, after nothing that may have sent, and
	// the same held on the way here.
	async := t.fn != nil && (t.fn.Async || t.fn.Returns != nil && t.fn.Returns.Kind == "name" && t.fn.Returns.Name == "Promise")
	stops := e.Top && !e.InTry && (!async || e.Awaited) && !unenforced(frame.Value) && !b.mayHaveSent(ref, 0, idx, e.Param)
	if t.fn == nil || e.Arg >= len(t.fn.Params) {
		// The callee does not use the argument in a read form (or has no
		// such parameter): following ends there.
		call.Value = valueKey(e.Arg, role)
		if !stops && section != routeshape.SectionResponses {
			call.Value += "|u"
		}
		return call
	}
	p := t.fn.Params[e.Arg]
	if p.Rest {
		call.Stop, call.Detail, call.Target = routeshape.StopUnresolved, "passed to a rest parameter", routeshape.Step{}
		return call
	}
	call.Value = valueKey(e.Arg, role)
	if !stops && section != routeshape.SectionResponses {
		call.Value += "|u"
	}
	if role != "req" && role != "res" && role != "next" && p.Type != nil && (section == routeshape.SectionBody || section == routeshape.SectionQuery) && role == requestKeys[section][0] {
		chain := append(append([]routeshape.Step{}, frame.Chain...), routeshape.Step{File: call.Site.File, Line: call.Site.Line, Function: t.step.Function})
		b.intended(section, *p.Type, t.file, b.prov("SH-E18", routeshape.Intended, t.file, t.fn.Line, chain))
	}
	return call
}

// responses follows the response parameter (and next) of the handler.
func (b *shapeBuilder) responses() {
	h := b.info.handler
	if h.fn == nil || len(h.fn.Params) < 2 || h.fn.Params[1].Name == "" {
		b.note(routeshape.SectionResponses, "the handler does not use a response parameter in a read form")
		return
	}
	v := &responseVisitor{b: b}
	res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: valueKey(1, "res")}, b.l.depth, v)
	if err == nil {
		b.addChains(routeshape.SectionResponses, res)
		for _, c := range res.Chains {
			if c.Stop != routeshape.StopEnded && c.Stop != routeshape.StopJoined && c.Stop != routeshape.StopCycle {
				v.unproven = true
			}
		}
	}
	if b.info.framework == "express" && len(h.fn.Params) >= 3 && h.fn.Params[2].Name != "" {
		res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: valueKey(2, "next")}, b.l.depth, v)
		if err == nil {
			b.addChains(routeshape.SectionResponses, res)
		}
	}
	// SH-E25: a send without a status is 200 only when nothing may have
	// set another status first. SH-E23: Express's res.json sets
	// application/json only when no Content-Type is set yet; Next.js's
	// always sets it; neither sends one for 204 or 304.
	express := b.info.framework == "express"
	typeUnproven := ""
	if express {
		typeUnproven = v.typeSet
	}
	if b.info.unread != "" {
		// Unread middleware may set the Content-Type or replace res.json
		// (row SH-E25): neither the type nor the body is the handler's.
		typeUnproven = b.info.unread
		if len(v.sends) > 0 {
			b.note(routeshape.SectionResponses, "response bodies not claimed: "+b.info.unread+" and may change the response")
		}
	}
	for _, d := range v.sends {
		if d.status == "" {
			switch {
			case v.statusSet || v.unproven:
				b.note(routeshape.SectionResponses, fmt.Sprintf("status of res.%s at %s:%d not proven (a status may be set elsewhere)", d.method, d.prov.File, d.prov.Line))
				continue
			case b.info.unread != "":
				b.note(routeshape.SectionResponses, fmt.Sprintf("status of res.%s at %s:%d not proven: %s and may set a status", d.method, d.prov.File, d.prov.Line, b.info.unread))
				continue
			}
			d.status = "200"
		}
		json := d.method == "json" && d.status != "204" && d.status != "304"
		if json && typeUnproven != "" {
			json = false
			b.note(routeshape.SectionResponses, fmt.Sprintf("content type of res.json at %s:%d not claimed: %s", d.prov.File, d.prov.Line, typeUnproven))
		}
		if b.info.unread != "" {
			d.body, d.returned = nil, nil
		}
		b.response(d.status, json, d.body, d.prov, d.returned)
	}
}

// pendingResponse is a send read in code; its status is empty for a send
// without a status (SH-E25).
type pendingResponse struct {
	status, method string
	body           *routeshape.Schema
	returned       *routeshape.Schema
	prov           routeshape.Provenance
}

type responseVisitor struct {
	b         *shapeBuilder
	statusSet bool
	unproven  bool
	// typeSet says where a Content-Type may be set on the response.
	typeSet string
	sends   []pendingResponse
}

// chainable are Express response methods that return the response and set
// no status.
var chainable = map[string]bool{"set": true, "header": true, "type": true, "contentType": true, "cookie": true, "clearCookie": true, "append": true, "location": true, "vary": true, "links": true, "attachment": true, "setHeader": true}

func (v *responseVisitor) Visit(_ context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	b := v.b
	ref, ok := b.lookup(frame.Function)
	if !ok {
		return nil, fmt.Errorf("function %s not indexed", frame.Function.Function)
	}
	if ref.fn == nil {
		return nil, nil
	}
	param, role := exParseValue(frame.Value)
	pages := b.info.framework == "nextjs_pages"
	var calls []routeshape.Call
	for idx, e := range ref.fn.Events {
		if e.Kind == "throw" {
			b.tag("SH-E29")
			b.note(routeshape.SectionResponses, fmt.Sprintf("an error is thrown (%s:%d): its status is set by error handling, not read", ref.file, e.Line))
			continue
		}
		if e.Param != param {
			continue
		}
		switch e.Kind {
		case "call":
			if role == "next" {
				if len(e.Path) == 0 && len(e.Methods) == 1 && e.Methods[0].Name == "" {
					b.tag("SH-E29")
					if len(e.Methods[0].Args) == 0 {
						b.note(routeshape.SectionResponses, fmt.Sprintf("next() passes the request on (%s:%d)", ref.file, e.Line))
					} else {
						b.note(routeshape.SectionResponses, fmt.Sprintf("an error is passed to next (%s:%d): its status is set by an error handler, not read", ref.file, e.Line))
					}
				}
				continue
			}
			if len(e.Path) == 0 {
				v.chain(ref, e, frame, pages)
			}
		case "pass":
			if len(e.Path) != 0 {
				continue
			}
			if e.Transformed {
				calls = append(calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "the response is spread into another value"})
				continue
			}
			b.tag("SH-E30")
			calls = append(calls, b.callFor(ref, idx, frame, role, routeshape.SectionResponses))
		case "assign":
			if role == "res" && len(e.Path) == 1 && e.Path[0] == "statusCode" {
				v.statusSet = true
				b.note(routeshape.SectionResponses, fmt.Sprintf("res.statusCode is assigned (%s:%d)", ref.file, e.Line))
			}
		}
	}
	return calls, nil
}

// chain reads one method chain on the response (SH-E23..SH-E28, SH-E31).
func (v *responseVisitor) chain(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame, pages bool) {
	b := v.b
	status, statusRead := "", true
	for i, m := range e.Methods {
		last := i == len(e.Methods)-1
		switch {
		case m.Name == "status":
			if len(m.Args) == 1 && m.Args[0].Kind == "number" && statusPattern(m.Args[0].Name) {
				status = m.Args[0].Name
			} else {
				statusRead = false
			}
			if last {
				v.statusSet = true
				b.tag("SH-E25")
			}
		case chainable[m.Name]:
			if setsType(m) && v.typeSet == "" {
				v.typeSet = fmt.Sprintf("res.%s at %s:%d may set a Content-Type", m.Name, ref.file, e.Line)
			}
		case m.Name == "json" || m.Name == "send" || m.Name == "end" || m.Name == "jsonp" || m.Name == "render":
			v.terminal(ref, e, frame, m, status, statusRead)
			return
		case m.Name == "sendStatus":
			if len(m.Args) == 1 && m.Args[0].Kind == "number" && statusPattern(m.Args[0].Name) {
				prov := b.prov("SH-E24", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
				var body *routeshape.Schema
				if m.Args[0].Name == "204" || m.Args[0].Name == "304" {
					body = &routeshape.Schema{Type: routeshape.TypeNull}
				}
				v.sends = append(v.sends, pendingResponse{status: m.Args[0].Name, method: m.Name, body: body, prov: prov})
			} else {
				b.tag("SH-E31")
				b.note(routeshape.SectionResponses, fmt.Sprintf("a status not read (%s:%d)", ref.file, e.Line))
			}
			return
		case m.Name == "redirect":
			prov := b.prov("SH-E26", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
			switch {
			case len(m.Args) == 2 && m.Args[0].Kind == "number" && statusPattern(m.Args[0].Name):
				v.sends = append(v.sends, pendingResponse{status: m.Args[0].Name, method: m.Name, prov: prov})
			case len(m.Args) == 1 && status == "" && statusRead:
				code := "302"
				if pages {
					code = "307"
				}
				v.sends = append(v.sends, pendingResponse{status: code, method: m.Name, prov: prov})
			default:
				b.tag("SH-E31")
				b.note(routeshape.SectionResponses, fmt.Sprintf("a redirect status not read (%s:%d)", ref.file, e.Line))
			}
			return
		default:
			b.tag("D")
			b.note(routeshape.SectionResponses, fmt.Sprintf("res.%s is not read (%s:%d)", m.Name, ref.file, e.Line))
			return
		}
	}
}

func statusPattern(s string) bool {
	// 1xx statuses are informational: Express does not end the response
	// with them; they are not read.
	n, err := strconv.Atoi(s)
	return err == nil && n >= 200 && n <= 599
}

// terminal records a sending method.
func (v *responseVisitor) terminal(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame, m facts.ShapeMethod, status string, statusRead bool) {
	b := v.b
	if !statusRead {
		b.tag("SH-E31")
		b.note(routeshape.SectionResponses, fmt.Sprintf("a status not read (%s:%d)", ref.file, e.Line))
		return
	}
	var body, returned *routeshape.Schema
	switch {
	case status == "204" || status == "304" || (m.Name == "end" || m.Name == "send") && len(m.Args) == 0:
		b.tag("SH-E27")
		body = &routeshape.Schema{Type: routeshape.TypeNull}
	case (m.Name == "json" || m.Name == "send") && len(m.Args) >= 1:
		body, returned = b.responseBody(ref, m, 0)
	}
	rule := "SH-E23"
	if status == "" {
		rule = "SH-E25"
	}
	prov := b.prov(rule, routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
	v.sends = append(v.sends, pendingResponse{status: status, method: m.Name, body: body, returned: returned, prov: prov})
}

// setsType reports a chainable call that may set the Content-Type:
// type, contentType, and set/header/setHeader/append of Content-Type or
// of a name or value not read.
func setsType(m facts.ShapeMethod) bool {
	switch m.Name {
	case "type", "contentType":
		return true
	case "set", "header", "setHeader", "append":
		if len(m.Args) == 0 {
			return false
		}
		a := m.Args[0]
		if a.Kind == "literal" {
			return strings.EqualFold(a.Name, "content-type")
		}
		if a.Kind == "object" {
			if a.Open {
				return true
			}
			for k := range a.Props {
				if strings.EqualFold(k, "content-type") {
					return true
				}
			}
			return false
		}
		return true
	}
	return false
}

// responseBody reads a sent value: an object literal (fields read,
// SH-E27) or a const whose initializer calls a proven function with a
// declared return type (SH-E28).
func (b *shapeBuilder) responseBody(ref fnRef, m facts.ShapeMethod, i int) (*routeshape.Schema, *routeshape.Schema) {
	arg := m.Args[i]
	if arg.Kind == "identifier" && i < len(m.Inits) && m.Inits[i] != nil {
		arg = *m.Inits[i]
		if arg.Kind == "call_expression" && arg.Object != nil {
			if s := b.returnedType(ref, *arg.Object); s != nil {
				return nil, s
			}
		}
	}
	if arg.Kind == "object" {
		b.tag("SH-E27")
		if _, custom := arg.Props["toJSON"]; custom {
			// JSON.stringify sends what toJSON returns.
			u := exUnknownSchema("the object defines toJSON")
			return &u, nil
		}
		s := objectLiteral(arg, 0)
		return &s, nil
	}
	return nil, nil
}

// objectLiteral is the fields-read schema of an object literal.
func objectLiteral(e facts.ShapeExpr, depth int) routeshape.Schema {
	out := routeshape.Schema{Type: routeshape.TypeObject, Additional: routeshape.AdditionalUnknown}
	names := make([]string, 0, len(e.Props))
	for k := range e.Props {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		s := exUnknownSchema("value not read")
		if v := e.Props[k]; v.Kind == "object" && depth < 8 {
			if _, custom := v.Props["toJSON"]; custom {
				s = exUnknownSchema("the object defines toJSON")
			} else {
				s = objectLiteral(v, depth+1)
			}
		}
		out.Fields = append(out.Fields, routeshape.Field{Name: k, Presence: routeshape.PresenceUnknown, Schema: s})
	}
	return out
}

// returnedType is the declared return type of a proven callee (Promise<T>
// unwrapped), as a schema of kind returned; nil when not proven.
func (b *shapeBuilder) returnedType(ref fnRef, callee facts.ShapeExpr) *routeshape.Schema {
	t, stop, _ := b.target(ref, facts.ShapeEvent{Callee: &callee})
	if stop != "" || t.fn == nil || t.fn.Returns == nil {
		return nil
	}
	rt := *t.fn.Returns
	if rt.Kind == "name" && rt.Name == "Promise" && len(rt.Args) == 1 {
		rt = rt.Args[0]
	}
	if opaque(rt) {
		return nil
	}
	s := b.sr.tsSchema(t.file, rt, routeshape.Returned, 0)
	if s.Type == routeshape.TypeUnknown {
		return nil
	}
	b.tag("SH-E28")
	return &s
}

// response adds or merges a response item; json claims application/json.
func (b *shapeBuilder) response(status string, json bool, body *routeshape.Schema, prov routeshape.Provenance, returned *routeshape.Schema) {
	var ct *string
	if json {
		j := "application/json"
		ct = &j
	}
	add := func(kind routeshape.Kind, schema *routeshape.Schema, p routeshape.Provenance) {
		items := b.shape.Responses.Items
		for i := range items {
			it := &items[i]
			if it.Status != status || it.Provenance.Kind != kind || !sameString(it.ContentType, ct) {
				continue
			}
			if !exSameSchema(it.Schema, schema) {
				u := exUnknownSchema("several different bodies at this status")
				it.Schema = &u
			}
			return
		}
		b.shape.Responses.Items = append(b.shape.Responses.Items, routeshape.Response{Status: status, ContentType: ct, Schema: schema, Provenance: p})
	}
	add(routeshape.ReadInHandler, body, prov)
	if returned != nil {
		p := prov
		p.Kind = routeshape.Returned
		p.Rule = b.row("SH-E28")
		add(routeshape.Returned, returned, p)
	}
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func exSameSchema(a, b *routeshape.Schema) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// finish sets section states and reasons, models and the hazard notes.
func (b *shapeBuilder) finish() {
	s := b.shape
	if use := b.info.libs["openapi"]; use.name != "" {
		b.tag("SH-E32")
		note := fmt.Sprintf("an OpenAPI validator (%s, %s:%d) is mounted before this route: the committed spec decides the request shape", use.name, use.file, use.line)
		for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody} {
			b.note(sec, note)
		}
	}
	if b.info.framework == "express" {
		b.tag("SH-E33")
		if use := b.info.libs["parser"]; use.name != "" {
			b.note(routeshape.SectionBody, fmt.Sprintf("body parser %s (%s:%d) is mounted; the content type is not read", use.name, use.file, use.line))
		} else {
			b.note(routeshape.SectionBody, "no body parser is proven mounted before this route: req.body may be unparsed; the content type is not read")
		}
	} else {
		b.tag("SH-E34")
		b.note(routeshape.SectionBody, "Next.js parses the body by content type; the content type is not read")
		if b.pagesBodyParserChanged() {
			b.note(routeshape.SectionBody, "the file's config changes api.bodyParser: parsing not read")
		}
		for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody, routeshape.SectionResponses} {
			b.note(sec, "the route serves every method: reads of all req.method branches are merged")
		}
	}
	if b.cookieUnproven && len(s.Cookie.Items) == 0 {
		b.note(routeshape.SectionCookie, "req.cookies is read but cookie-parser is not proven mounted")
	}
	b.dropWritten()
	s.Models = b.sr.models.models
	params := map[string]*routeshape.ParamSection{routeshape.SectionQuery: &s.Query, routeshape.SectionHeader: &s.Header, routeshape.SectionCookie: &s.Cookie}
	for name, sec := range params {
		sortParams(sec.Items)
		b.state(name, &sec.Section, len(sec.Items), "names read in code; others may exist")
	}
	for i := range s.Body.Items {
		if s.Body.Items[i].Provenance.Kind == routeshape.ReadInHandler {
			fields := s.Body.Items[i].Schema.Fields
			sort.Slice(fields, func(x, y int) bool { return fields[x].Name < fields[y].Name })
		}
	}
	sort.SliceStable(s.Body.Items, func(i, j int) bool {
		return kindOrder(s.Body.Items[i].Provenance.Kind) < kindOrder(s.Body.Items[j].Provenance.Kind)
	})
	b.state(routeshape.SectionBody, &s.Body.Section, len(s.Body.Items), "fields read in code or declared by a validator or a type; others may exist")
	sort.SliceStable(s.Responses.Items, func(i, j int) bool {
		a, c := s.Responses.Items[i], s.Responses.Items[j]
		if a.Status != c.Status {
			return a.Status < c.Status
		}
		return kindOrder(a.Provenance.Kind) < kindOrder(c.Provenance.Kind)
	})
	b.state(routeshape.SectionResponses, &s.Responses.Section, len(s.Responses.Items), "statuses read from code; errors thrown by called functions, error middleware and unread calls may add others")
}

func kindOrder(k routeshape.Kind) int {
	switch k {
	case routeshape.Fact:
		return 0
	case routeshape.ReadInHandler:
		return 1
	case routeshape.Intended:
		return 2
	case routeshape.Returned:
		return 3
	}
	return 4
}

func sortParams(items []routeshape.Param) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return kindOrder(items[i].Provenance.Kind) < kindOrder(items[j].Provenance.Kind)
	})
}

// state sets a section partial (items) or not_read (none), with reasons.
func (b *shapeBuilder) state(name string, sec *routeshape.Section, items int, base string) {
	notes := b.notes[name]
	if items > 0 {
		sec.State = routeshape.Partial
		sec.Reason = strings.Join(append([]string{base}, notes...), "; ")
		return
	}
	sec.State = routeshape.NotRead
	if len(notes) > 0 {
		sec.Reason = strings.Join(notes, "; ")
	} else {
		sec.Reason = "nothing read in the handler or the functions it passes the value to"
	}
}

// pagesBodyParserChanged reports `export const config = { api: {
// bodyParser: ... } }` with a value other than true.
func (b *shapeBuilder) pagesBodyParserChanged() bool {
	data := b.l.r.files[b.info.node.File].Routes
	if data == nil || len(data.Scopes) == 0 {
		return false
	}
	c, ok := data.Scopes[0].Bindings["config"]
	if !ok || c.Value.Kind != "object" {
		return false
	}
	api, ok := c.Value.Properties["api"]
	if !ok {
		return false
	}
	if api.Kind != "object" {
		return true
	}
	v, ok := api.Properties["bodyParser"]
	return ok && v.Kind != "true"
}

// dropWritten removes the names read of sections whose request value (or
// field) the code writes: what is read afterwards is not what the client
// sent.
func (b *shapeBuilder) dropWritten() {
	s := b.shape
	params := map[string]*routeshape.ParamSection{routeshape.SectionQuery: &s.Query, routeshape.SectionHeader: &s.Header, routeshape.SectionCookie: &s.Cookie}
	for name, sec := range params {
		kept := sec.Items[:0]
		for _, p := range sec.Items {
			if p.Provenance.Kind == routeshape.ReadInHandler && (b.replaced[name] || b.assigned[name][p.Name]) {
				continue
			}
			kept = append(kept, p)
		}
		sec.Items = kept
	}
	kept := s.Body.Items[:0]
	for _, it := range s.Body.Items {
		if it.Provenance.Kind == routeshape.ReadInHandler && it.ContentType == nil && it.Schema != nil && it.Schema.Type == routeshape.TypeObject {
			if b.replaced[routeshape.SectionBody] {
				continue
			}
			fields := it.Schema.Fields[:0]
			for _, f := range it.Schema.Fields {
				if !b.assigned[routeshape.SectionBody][f.Name] {
					fields = append(fields, f)
				}
			}
			it.Schema.Fields = fields
			if len(fields) == 0 {
				continue
			}
		}
		kept = append(kept, it)
	}
	s.Body.Items = kept
}

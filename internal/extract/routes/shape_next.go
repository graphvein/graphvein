package routes

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Next.js App Router route handler and server action request and response
// shape (T029 SH-N, docs/contract/claims/route-shape-nextjs.md). The same
// cached parameter-use syntax as the Express rows (facts.ShapeFunction)
// is read: the request's Fetch API members (nextUrl.searchParams,
// headers, cookies, json(), formData()), the next/headers and
// next/navigation functions, and the handler's return statements, which
// build the response. Every claim is one SH-N row; the forms shared with
// the Express rows (validators, TypeScript types, literal bodies) report
// their SH-N row through nextRow.

// frameworkAction is the routeInfo framework of a server action.
const frameworkAction = "nextjs_action"

// nextRows maps the Express rows whose code the Next.js rows share to the
// SH-N row that states the same claim for Next.js. A shared row without an
// entry here is not a Next.js form: it is reported as D.
var nextRows = map[string]string{
	"SH-E1": "SH-N1", "SH-E2": "SH-N2", "SH-E6": "SH-N4", "SH-E10": "SH-N8",
	"SH-E11": "SH-N11", "SH-E12": "SH-N12", "SH-E13": "SH-N13", "SH-E19": "SH-N14",
	"SH-E20": "SH-N15", "SH-E17": "SH-N16", "SH-E18": "SH-N16", "SH-E22": "SH-N16",
	"SH-E27": "SH-N17", "SH-E28": "SH-N21", "SH-E35": "SH-N25",
}

// nextRow is the SH-N row of a shared row (D when Next.js has none).
func nextRow(row string) string {
	if !strings.HasPrefix(row, "SH-E") {
		return row
	}
	if r, ok := nextRows[row]; ok {
		return r
	}
	return "D"
}

// next reports a Next.js App Router route or server action.
func (b *shapeBuilder) next() bool {
	return b.info.framework == "nextjs" || b.info.framework == frameworkAction
}

// row is the row a shared form reports on this route.
func (b *shapeBuilder) row(r string) string {
	if b.info != nil && b.next() {
		return nextRow(r)
	}
	return r
}

// tag reports a row reached on this route.
func (b *shapeBuilder) tag(r string) { tagShape(b.row(r)) }

// mayHaveSent reports, for the events [from, to) of ref's function, a
// use that may have answered the request before (Express: a send on the
// response; Next.js: a return or a redirect, other than an error answer
// in an exiting top-level if branch).
func (b *shapeBuilder) mayHaveSent(ref fnRef, from, to, param int) bool {
	if !b.next() {
		return mayHaveSent(ref.fn, from, to, param)
	}
	fn := ref.fn
	for j := from; j < to && j < len(fn.Events); j++ {
		e := fn.Events[j]
		switch {
		case e.Kind == "return":
			if e.ArmExits && e.Stmt > 0 && e.Arm > 0 && b.errorReturn(ref, e) {
				continue
			}
			return true
		case e.Kind == "call" && e.Param == -2 && len(e.Path) == 1 && (e.Path[0] == "redirect" || e.Path[0] == "permanentRedirect"):
			return true
		}
	}
	return false
}

// errorReturn reports a return of a response built in place with a
// literal 4xx or 5xx status (Response.json, NextResponse.json, new
// Response, new NextResponse).
func (b *shapeBuilder) errorReturn(ref fnRef, e facts.ShapeEvent) bool {
	x, _ := returnedExpr(e)
	if x == nil {
		return false
	}
	form, ok := b.responseCall(ref, *x)
	if !ok || (form != "json" && form != "new") {
		return false
	}
	init := 1
	if len(x.Args) <= init {
		return false
	}
	status, explicit, ok := initStatus(x.Args[init])
	return ok && explicit && errorStatus(status)
}

// returnedExpr is the expression a return event returns (the const's
// initializer for a returned const) and whether it came through a const.
func returnedExpr(e facts.ShapeEvent) (*facts.ShapeExpr, bool) {
	if len(e.Methods) == 0 || len(e.Methods[0].Args) == 0 {
		return nil, false
	}
	x := e.Methods[0].Args[0]
	if x.Kind == "identifier" && len(e.Methods[0].Inits) > 0 && e.Methods[0].Inits[0] != nil {
		return e.Methods[0].Inits[0], true
	}
	return &x, false
}

// responseCall classifies an expression that builds a response with the
// Fetch API or next/server: "json" (Response.json, NextResponse.json),
// "redirect" (Response.redirect, NextResponse.redirect), "new" (new
// Response, new NextResponse) or "other" (another static method, such
// as NextResponse.next). Response must be the global (no binding of the
// name in scope) and NextResponse the export of next/server.
func (b *shapeBuilder) responseCall(ref fnRef, x facts.ShapeExpr) (string, bool) {
	var class, method string
	switch {
	case x.Kind == "new_expression" && x.Object != nil && x.Object.Kind == "identifier":
		class, method = x.Object.Name, "new"
	case x.Kind == "call_expression" && x.Object != nil && x.Object.Kind == "member_expression" && x.Object.Object != nil && x.Object.Object.Kind == "identifier":
		class, method = x.Object.Object.Name, x.Object.Name
	default:
		return "", false
	}
	if !b.responseClass(ref, class) {
		return "", false
	}
	switch method {
	case "new", "json", "redirect":
		return method, true
	}
	return "other", true
}

// responseClass reports that name is the global Response or next/server's
// NextResponse where ref's function uses it.
func (b *shapeBuilder) responseClass(ref fnRef, name string) bool {
	if name != "Response" && name != "NextResponse" {
		return false
	}
	if ref.fn != nil {
		for _, l := range ref.fn.Locals {
			if l == name {
				return false
			}
		}
		for _, p := range ref.fn.Params {
			if p.Name == name {
				return false
			}
		}
	}
	bind, _, ok := b.l.r.binding(ref.file, ref.scope, name)
	if name == "Response" {
		return !ok
	}
	return ok && (bind.Module == "next/server" || bind.Module == "next/dist/server/web/spec-extension/response") && bind.Export == "NextResponse"
}

// importedFrom reports that name, used in ref's function, is the named
// export of module (a next/headers or next/navigation function).
func (b *shapeBuilder) importedFrom(ref fnRef, name, module string) bool {
	if ref.fn != nil {
		for _, l := range ref.fn.Locals {
			if l == name {
				return false
			}
		}
		for _, p := range ref.fn.Params {
			if p.Name == name {
				return false
			}
		}
	}
	bind, _, ok := b.l.r.binding(ref.file, ref.scope, name)
	return ok && bind.Module == module && bind.Export == name
}

// initStatus reads the status of a ResponseInit argument: an object
// literal with a literal status (explicit) or without one (the default
// 200), or undefined. ok is false for any other form.
func initStatus(init facts.ShapeExpr) (status string, explicit, ok bool) {
	switch init.Kind {
	case "undefined":
		return "200", false, true
	case "object":
		if init.Open {
			return "", false, false
		}
		st, has := init.Props["status"]
		if !has {
			return "200", false, true
		}
		if st.Kind == "number" {
			if _, err := strconv.Atoi(st.Name); err == nil {
				return st.Name, true, true
			}
		}
	}
	return "", false, false
}

// initContentType reads the Content-Type a ResponseInit's literal headers
// set: set is true when the headers name one (value nil when not a
// literal), known is false when the headers are not a literal object.
func initContentType(init *facts.ShapeExpr) (value *string, set, known bool) {
	if init == nil || init.Kind != "object" {
		return nil, false, init == nil || init.Kind == "undefined"
	}
	h, ok := init.Props["headers"]
	if !ok {
		return nil, false, !init.Open
	}
	if h.Kind != "object" || h.Open || init.Open {
		return nil, false, false
	}
	for k, v := range h.Props {
		if strings.EqualFold(k, "content-type") {
			if v.Kind == "literal" {
				s := v.Name
				return &s, true, true
			}
			return nil, true, true
		}
	}
	return nil, false, true
}

// nullBodyStatus are the statuses a Response refuses a body for.
func nullBodyStatus(s string) bool {
	return s == "101" || s == "103" || s == "204" || s == "205" || s == "304"
}

// responseStatus reports a status the Response constructor accepts.
func responseStatus(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 200 && n <= 599
}

// redirectStatus reports a status Response.redirect accepts.
func redirectStatus(s string) bool {
	return s == "301" || s == "302" || s == "303" || s == "307" || s == "308"
}

// nextRoute builds the shape of an App Router route handler.
func (l *shapeLinker) nextRoute(n *contract.Node) *routeshape.Shape {
	shape := routeshape.New(l.depth)
	info := routeInfo{node: n, framework: "nextjs", libs: map[string]middlewareUse{}}
	if identity, _ := n.Attributes["handler"].(string); identity != "" {
		if ref, ok := l.byIdentity(identity); ok {
			info.handler = &ref
		} else {
			info.handlerNote = "handler syntax not found"
		}
	} else {
		expression, _ := n.Attributes["handler_expression"].(string)
		info.handlerNote = "export " + whitespace(expression) + " is not a proven function"
	}
	b := newShapeBuilder(l, &info, shape)
	b.path()
	if info.handler == nil {
		tagShape("SH-N26")
		reason := "handler not read: " + info.handlerNote
		for _, sec := range []*routeshape.Section{&shape.Query.Section, &shape.Header.Section, &shape.Cookie.Section, &shape.Body.Section, &shape.Responses.Section} {
			sec.Reason = reason
		}
		return shape
	}
	b.nextMiddleware()
	b.nextStatic()
	b.nextRequest(0, nil)
	b.nextResponses()
	b.nextFinish()
	return shape
}

// nextStatic reads the route segment config `dynamic` of the route's file
// (SH-N24): with 'force-static' Next.js hands every handler of the file a
// request whose headers, cookies and query are empty (and next/headers
// values empty too), and may answer GET from a build-time render, so names
// read there are not parameters and no response is claimed. A value that
// is not a string literal may be 'force-static'.
func (b *shapeBuilder) nextStatic() {
	file := b.info.node.File
	data := b.l.r.files[file].Routes
	if data == nil || len(data.Scopes) == 0 {
		return
	}
	d, ok := data.Scopes[0].Bindings["dynamic"]
	if !ok {
		return
	}
	value, literal := "", false
	if !d.Mutable && d.Module == "" && (d.Value.Kind == "string" || d.Value.Kind == "literal") {
		value = strings.Trim(d.Value.Name, "'\"`")
		if value == "" {
			value = strings.Trim(d.Value.Text, "'\"`")
		}
		literal = value != ""
	}
	var why string
	switch {
	case !literal:
		why = fmt.Sprintf("the route segment config dynamic of %s is not a string literal: it may be 'force-static', which empties the request's query, headers and cookies", file)
	case value == "force-static":
		why = fmt.Sprintf("%s exports dynamic = 'force-static': Next.js empties the request's query, headers and cookies and may answer from a build-time render", file)
	case value == "error":
		why = fmt.Sprintf("%s exports dynamic = 'error': on Next.js 14.1.0 reading the request's URL, query, headers, cookies or body throws (500), while 15.5.4 and 16.2.10 pass the request as sent", file)
		b.info.staticBody = true
	default:
		// 'auto' and 'force-dynamic' hand the handler the request as
		// sent (oracle): D.
		return
	}
	b.tag("SH-N24")
	b.info.static = why
	sections := []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie}
	if b.info.staticBody {
		sections = append(sections, routeshape.SectionBody)
	}
	for _, sec := range sections {
		b.replaced[sec] = true
		b.note(sec, why+": names read are not parameters")
	}
}

func newShapeBuilder(l *shapeLinker, info *routeInfo, shape *routeshape.Shape) *shapeBuilder {
	return &shapeBuilder{l: l, info: info, shape: shape, sr: &schemaReader{r: l.r, models: newModelSet(), next: info.framework == "nextjs" || info.framework == frameworkAction}, notes: map[string][]string{}, replaced: map[string]bool{}, assigned: map[string]map[string]bool{}}
}

// nextMiddleware notes a middleware (or Next.js 16 proxy) file of the
// route's project whose matcher may cover the route (SH-N24): it runs
// before the handler and may answer, redirect, rewrite or set response
// headers.
func (b *shapeBuilder) nextMiddleware() {
	n := b.info.node
	dir, _ := n.Attributes["project"].(string)
	pattern, _ := n.Attributes["pattern"].(string)
	join := func(parts ...string) string {
		out := path.Join(parts...)
		return strings.TrimPrefix(out, "./")
	}
	why, answered := b.configRulesMatch(dir, pattern)
	if answered != "" {
		// Config redirects run before middleware, and a beforeFiles or
		// afterFiles rewrite after it: either way the handler never answers.
		b.info.answered = answered
	}
	var found []string
	for _, base := range []string{dir, join(dir, "src")} {
		for _, name := range []string{"middleware", "proxy"} {
			for _, ext := range []string{".ts", ".js", ".mjs", ".mts", ".tsx", ".jsx", ".cjs", ".cts"} {
				f := join(base, name+ext)
				if _, ok := b.l.r.files[f]; ok {
					found = append(found, f)
				}
			}
		}
	}
	for _, f := range found {
		if why := b.middlewareMatches(f, pattern); why != "" {
			b.tag("SH-N24")
			b.info.unread = fmt.Sprintf("middleware %s %s", f, why)
			for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie} {
				b.note(sec, b.info.unread+": it may rewrite the request or change the headers the handler reads")
			}
			return
		}
	}
	if why != "" {
		b.tag("SH-N24")
		b.info.unread = why
		for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie} {
			b.note(sec, b.info.unread+": it may answer or rewrite the request before the handler")
		}
	}
}

// configRulesMatch reads the project's next.config redirects, rewrites
// and headers rules for the route template (SH-N24). why says why a rule
// may apply (a rule whose source may match, a key or the whole config
// not read); answered names a redirect or applicable rewrite without has
// or missing whose source covers the whole template: the handler never
// answers the route's URL. Next.js serves a route before afterFiles
// rewrites when its path is static, and before fallback rewrites always
// (NX-L3), so only beforeFiles rules, and afterFiles rules for a dynamic
// route, can take a request away from the handler.
func (b *shapeBuilder) configRulesMatch(dir, pattern string) (why, answered string) {
	for _, name := range nextConfigNames {
		file := name
		if dir != "" && dir != "." {
			file = dir + "/" + name
		}
		f, ok := b.l.r.files[file]
		if !ok {
			continue
		}
		var c *facts.NextConfig
		if f.Next != nil {
			c = f.Next.Config
		}
		if c == nil || c.Shape != "object" {
			return fmt.Sprintf("%s is not read: its redirects, rewrites or headers may apply to the route", file), ""
		}
		if len(c.RulesUnread) > 0 {
			why = fmt.Sprintf("the %s of %s are not read: they may apply to the route", strings.Join(c.RulesUnread, " and "), file)
		}
		dynamic := strings.Contains(pattern, "{")
		// A trailing slash setting other than the default changes the path
		// a source is matched against: no rule is taken to cover the route.
		plain := c.TrailingSlashState == "absent" || c.TrailingSlashState == "literal" && !c.TrailingSlash
		for _, r := range c.Rules {
			if r.Key == "rewrites" && !(r.Phase == "beforeFiles" || r.Phase == "afterFiles" && dynamic) {
				continue
			}
			if r.Key == "headers" && !setsContentHeader(r) {
				// Only adds response headers that leave the content alone:
				// it changes neither the request nor the status, content
				// type or body of the route's responses (SH-N30).
				if r.Source == "" || matcherMayMatch(r.Source, pattern) {
					b.tag("SH-N30")
				}
				continue
			}
			if r.Source != "" && !matcherMayMatch(r.Source, pattern) {
				continue
			}
			rule := "a " + strings.TrimSuffix(r.Key, "s") + " rule with a source not read"
			if r.Source != "" {
				rule = fmt.Sprintf("the %s rule %s", strings.TrimSuffix(r.Key, "s"), r.Source)
			}
			if r.Key != "headers" && !r.Conditional && r.Source != "" && plain && sourceCovers(r.Source, pattern) && answered == "" {
				answered = fmt.Sprintf("%s of %s has no has or missing condition and matches every request to the route: Next.js answers before the handler", rule, file)
			}
			if why == "" {
				why = fmt.Sprintf("%s of %s may apply to the route", rule, file)
			}
		}
		return why, answered
	}
	return "", ""
}

// contentHeaders are the response headers a next.config headers rule
// may set over the handler's own: Content-Type replaces (Next.js 15 and
// 16) or joins (14.1.0) the handler's, Content-Length cuts the body short
// and Content-Encoding or Transfer-Encoding change how it is read
// (probed on Next.js 14.1.0 and 16.2.10, T029 NXH).
var contentHeaders = []string{"Content-Type", "Content-Length", "Content-Encoding", "Transfer-Encoding"}

// setsContentHeader reports that a next.config headers rule may set one
// of contentHeaders: a name list not read, or a name equal to one of them
// ignoring case.
func setsContentHeader(r facts.NextConfigRule) bool {
	if !r.HeaderKeysRead {
		return true
	}
	for _, k := range r.HeaderKeys {
		for _, h := range contentHeaders {
			if strings.EqualFold(k, h) {
				return true
			}
		}
	}
	return false
}

// sourceCovers reports that a next.config source of literal segments and
// :name parameters (a final :name* or :name+ for the rest) matches every
// path of the route template: each literal segment of the source equals
// the template's, a :name covers one segment (a literal or a parameter),
// a final :name* covers the rest and a final :name+ one or more segments
// (not an optional catch-all).
func sourceCovers(source, template string) bool {
	if strings.ContainsAny(source, "()[]{}\\|^$") || !strings.HasPrefix(source, "/") {
		return false
	}
	ss, ts := splitPath(source), splitPath(template)
	for i, seg := range ss {
		last := i == len(ss)-1
		if strings.HasPrefix(seg, ":") {
			switch {
			case strings.HasSuffix(seg, "*") && last:
				return true
			case strings.HasSuffix(seg, "+") && last:
				// A catch-all of the template may be optional (no segment).
				return i < len(ts) && !strings.HasPrefix(ts[i], "{*")
			case strings.ContainsAny(seg[1:], "*+?"):
				return false
			}
			if i >= len(ts) || strings.HasPrefix(ts[i], "{*") {
				return false
			}
			continue
		}
		if strings.ContainsAny(seg, "*+?:") || i >= len(ts) || ts[i] != seg {
			return false
		}
	}
	return len(ss) == len(ts)
}

// middlewareMatches returns why the middleware file may run for the
// route template, or "" when its literal matcher proves it does not.
func (b *shapeBuilder) middlewareMatches(file, pattern string) string {
	data := b.l.r.files[file].Routes
	if data == nil || len(data.Scopes) == 0 {
		return "may run before the route (matcher not read)"
	}
	c, ok := data.Scopes[0].Bindings["config"]
	if !ok {
		return "runs before every route (no config.matcher)"
	}
	if c.Value.Kind != "object" || c.Value.Spread {
		return "may run before the route (config not read)"
	}
	m, ok := c.Value.Properties["matcher"]
	if !ok {
		return "runs before every route (no config.matcher)"
	}
	// Only a single string literal is read: the cached route syntax keeps
	// no array elements, so an array matcher may match.
	var entries []facts.RouteExpr
	switch m.Kind {
	case "string", "literal":
		entries = []facts.RouteExpr{m}
	default:
		return "may run before the route (matcher not read)"
	}
	for _, e := range entries {
		if e.Kind != "string" && e.Kind != "literal" {
			return "may run before the route (matcher " + whitespace(e.Text) + " not read)"
		}
		value := strings.Trim(e.Name, "'\"`")
		if value == "" {
			value = strings.Trim(e.Text, "'\"`")
		}
		if matcherMayMatch(value, pattern) {
			return "may run before the route (matcher " + value + ")"
		}
	}
	return ""
}

// matcherMayMatch reports that a middleware matcher may match a route
// template. Only matchers of literal segments and :name, :name*, :name+
// and :name? parameters are read; any other form may match.
func matcherMayMatch(matcher, template string) bool {
	if strings.ContainsAny(matcher, "()[]{}\\|^$") || !strings.HasPrefix(matcher, "/") {
		return true
	}
	ms := splitPath(matcher)
	ts := splitPath(template)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		if i == len(ms) {
			return j == len(ts)
		}
		seg := ms[i]
		if strings.HasPrefix(seg, ":") {
			switch {
			case strings.HasSuffix(seg, "*"):
				return true
			case strings.HasSuffix(seg, "+"):
				return j < len(ts)
			case strings.HasSuffix(seg, "?"):
				return match(i+1, j) || j < len(ts) && match(i+1, j+1)
			}
			return j < len(ts) && match(i+1, j+1)
		}
		if strings.ContainsAny(seg, "*+?:") {
			return true
		}
		if j >= len(ts) {
			return false
		}
		t := ts[j]
		if strings.HasPrefix(t, "{*") {
			return true
		}
		if strings.HasPrefix(t, "{") {
			return match(i+1, j+1)
		}
		return strings.EqualFold(seg, t) && match(i+1, j+1)
	}
	return match(0, 0)
}

func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Request values the Next.js visitor follows, by role: the path each
// stands for from the request.
var nextRolePaths = map[string][]string{
	"req":     nil,
	"query":   {"nextUrl", "searchParams"},
	"url":     {"url", "URL()"},
	"headers": {"headers"},
	"cookies": {"cookies"},
	"json":    {"json()"},
	"form":    {"formData()"},
	"typed":   {"typed()"},
}

// nextPart is what a request path reads: the section, the role of the
// object at the path (a map read by get/has, a JSON value, ...), how many
// path elements name the object, and the row of a name read in it.
type nextPart struct {
	section string
	role    string
	n       int
	row     string
}

// nextClassify reads a request path (free marks a next/headers root).
func nextClassify(full []string, free bool) (nextPart, bool) {
	has := func(prefix ...string) bool {
		if len(full) < len(prefix) {
			return false
		}
		for i, p := range prefix {
			if full[i] != p {
				return false
			}
		}
		return true
	}
	switch {
	case free && has("headers()"):
		return nextPart{routeshape.SectionHeader, "headers", 1, "SH-N6"}, true
	case free && has("cookies()"):
		return nextPart{routeshape.SectionCookie, "cookies", 1, "SH-N6"}, true
	case free:
		return nextPart{}, false
	case has("nextUrl", "searchParams"):
		return nextPart{routeshape.SectionQuery, "query", 2, "SH-N3"}, true
	case has("url", "URL()", "searchParams"):
		return nextPart{routeshape.SectionQuery, "query", 3, "SH-N3"}, true
	case has("url", "URL()"):
		return nextPart{routeshape.SectionQuery, "url", 2, "SH-N3"}, true
	case has("nextUrl"), has("url"):
		return nextPart{routeshape.SectionQuery, "urlwhole", 1, "SH-N4"}, true
	case has("headers"):
		return nextPart{routeshape.SectionHeader, "headers", 1, "SH-N5"}, true
	case has("cookies"):
		return nextPart{routeshape.SectionCookie, "cookies", 1, "SH-N7"}, true
	case has("json()"):
		return nextPart{routeshape.SectionBody, "json", 1, "SH-N8"}, true
	case has("formData()"):
		return nextPart{routeshape.SectionBody, "form", 1, "SH-N9"}, true
	case has("text()"), has("arrayBuffer()"), has("blob()"), has("body"):
		return nextPart{routeshape.SectionBody, "raw", 1, "SH-N10"}, true
	case has("typed()"):
		return nextPart{routeshape.SectionBody, "typed", 1, "SH-N27"}, true
	}
	return nextPart{}, false
}

type nextRequestVisitor struct {
	b       *shapeBuilder
	section string
}

// nextRequest follows the request (parameter param of the handler, with
// the role req) for every request section, or, for a server action, the
// given starting values.
func (b *shapeBuilder) nextRequest(param int, starts []string) {
	h := b.info.handler
	if starts == nil {
		starts = []string{valueKey(param, "req")}
	}
	sections := []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody}
	if b.info.framework == frameworkAction {
		sections = sections[1:]
	}
	for _, section := range sections {
		for _, start := range starts {
			v := &nextRequestVisitor{b: b, section: section}
			res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: start}, b.l.depth, v)
			if err != nil {
				continue
			}
			b.addChains(section, res)
		}
	}
}

// Visit reads one function's request uses for one section.
func (v *nextRequestVisitor) Visit(_ context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	b := v.b
	ref, ok := b.lookup(frame.Function)
	if !ok {
		return nil, fmt.Errorf("function %s not indexed", frame.Function.Function)
	}
	if ref.fn == nil {
		return nil, nil
	}
	param, role := exParseValue(frame.Value)
	var calls []routeshape.Call
	for idx, e := range ref.fn.Events {
		if line, name, ok := b.syncNextHeaders(ref, e); ok {
			if (name == "headers" && v.section == routeshape.SectionHeader) || (name == "cookies" && v.section == routeshape.SectionCookie) {
				b.tag("SH-N6")
				b.note(v.section, fmt.Sprintf("%s() is used without await (%s:%d): Next.js 16 throws there, so no name read through it is claimed", name, ref.file, line))
			}
			continue
		}
		free := e.Param == -2 && len(e.Path) > 0 && (e.Path[0] == "headers()" || e.Path[0] == "cookies()")
		if free {
			if !b.importedFrom(ref, strings.TrimSuffix(e.Path[0], "()"), "next/headers") {
				continue
			}
		} else if e.Param != param || e.Param < 0 {
			continue
		}
		full := e.Path
		if !free {
			full = append(append([]string{}, nextRolePaths[role]...), e.Path...)
		}
		prov := func(rule string) routeshape.Provenance {
			return b.prov(rule, routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
		}
		if len(full) == 0 {
			v.request(ref, idx, e, frame, &calls)
			continue
		}
		part, ok := nextClassify(full, free)
		if !ok || part.section != v.section {
			continue
		}
		rest := full[part.n:]
		v.use(ref, idx, e, frame, part, rest, prov, &calls)
	}
	return calls, nil
}

// request handles a use of the request object itself.
func (v *nextRequestVisitor) request(ref fnRef, idx int, e facts.ShapeEvent, frame routeshape.Frame, calls *[]routeshape.Call) {
	b := v.b
	switch e.Kind {
	case "read":
		if e.Whole {
			b.tag("SH-N4")
			b.note(v.section, fmt.Sprintf("the request is read with a computed key or as a whole (%s:%d)", ref.file, e.Line))
		}
	case "keys":
		if e.Open {
			b.tag("SH-N4")
			b.note(v.section, fmt.Sprintf("the request is destructured with a rest element (%s:%d)", ref.file, e.Line))
		}
	case "call":
		if v.section != routeshape.SectionBody || len(e.Methods) == 0 {
			return
		}
		switch e.Methods[0].Name {
		case "json":
			b.tag("SH-N8")
			b.note(v.section, "request.json() parses the body as JSON whatever its Content-Type; the content type is not read")
		case "formData":
			b.tag("SH-N9")
			b.note(v.section, "request.formData() reads multipart/form-data or application/x-www-form-urlencoded bodies; the content type is not read")
		case "text", "arrayBuffer", "blob":
			b.tag("SH-N10")
			b.note(v.section, fmt.Sprintf("the body is read with request.%s() (%s:%d): its fields are not read", e.Methods[0].Name, ref.file, e.Line))
		}
	case "pass":
		if e.Transformed {
			*calls = append(*calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "the request is spread into another value"})
			return
		}
		*calls = append(*calls, b.callFor(ref, idx, frame, "req", v.section))
	case "assign":
		b.tag("SH-N4")
		b.replaced[v.section] = true
		b.note(v.section, fmt.Sprintf("the request is written in code (%s:%d): names read are not claimed", ref.file, e.Line))
	}
}

// use handles a use of a request part: a map (searchParams, headers,
// cookies, form data), the JSON body or the request URL.
func (v *nextRequestVisitor) use(ref fnRef, idx int, e facts.ShapeEvent, frame routeshape.Frame, part nextPart, rest []string, prov func(string) routeshape.Provenance, calls *[]routeshape.Call) {
	b := v.b
	whole := func(what string) {
		b.tag("SH-N4")
		b.note(v.section, fmt.Sprintf("%s (%s:%d)", what, ref.file, e.Line))
	}
	switch part.role {
	case "urlwhole":
		// request.url (a string) or request.nextUrl used other than for
		// its searchParams: the query may be read from it.
		switch {
		case e.Kind == "read" && e.Bound && len(rest) == 0:
		case full0(e, part) == "nextUrl" && len(rest) > 0 && rest[0] != "search" && rest[0] != "href" && rest[0] != "toString":
		case e.Kind == "read" || e.Kind == "call" || e.Kind == "pass" || e.Kind == "keys":
			whole("the request URL is read as a whole")
		}
		return
	case "url":
		switch {
		case e.Kind == "read" && e.Bound && len(rest) == 0:
		case e.Kind == "keys" && len(rest) == 0 && !e.Open && !exContains(e.Keys, "search") && !exContains(e.Keys, "href"):
		case len(rest) > 0 && rest[0] != "search" && rest[0] != "href":
		case e.Kind == "call" && len(rest) == 0 && len(e.Methods) > 0 && e.Methods[0].Name != "toString":
		case e.Kind == "read" || e.Kind == "call" || e.Kind == "pass" || e.Kind == "keys":
			whole("the request URL object is read as a whole")
		}
		return
	case "typed":
		if e.Kind == "pass" && len(rest) == 0 && !e.Transformed {
			b.validator(v.section, ref, idx, frame)
		}
		return
	case "raw":
		b.tag("SH-N10")
		b.note(v.section, fmt.Sprintf("the body is read as text, bytes or a stream (%s:%d): its fields are not read", ref.file, e.Line))
		return
	case "json":
		v.json(ref, idx, e, frame, rest, prov, calls)
		return
	}
	// A map read by name: searchParams, headers, cookies, form data.
	switch e.Kind {
	case "call":
		if len(rest) != 0 || len(e.Methods) == 0 {
			return
		}
		m := e.Methods[0]
		switch m.Name {
		case "get", "getAll", "has":
			if len(m.Args) >= 1 && m.Args[0].Kind == "literal" {
				v.read(part, m.Args[0].Name, prov)
			} else {
				whole(fmt.Sprintf("%s with a name not read", m.Name))
			}
		case "set", "append", "delete":
			b.tag("SH-N4")
			b.replaced[v.section] = true
			b.note(v.section, fmt.Sprintf("the request %s are written in code (%s:%d): names read are not claimed", part.role, ref.file, e.Line))
		default:
			whole(fmt.Sprintf("the request %s are read as a whole (%s)", part.role, m.Name))
		}
	case "read":
		if len(rest) == 0 && e.Bound {
			return
		}
		if len(rest) > 0 && part.role != "form" {
			whole(fmt.Sprintf("the request %s are read through %s", part.role, strings.Join(rest, ".")))
			return
		}
		whole(fmt.Sprintf("the request %s are read as a whole", part.role))
	case "keys":
		whole(fmt.Sprintf("the request %s are destructured", part.role))
	case "pass":
		if len(rest) != 0 {
			return
		}
		if e.Transformed {
			*calls = append(*calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "the request " + part.role + " are spread into another value"})
			return
		}
		*calls = append(*calls, b.callFor(ref, idx, frame, part.role, v.section))
	case "assign":
		b.tag("SH-N4")
		b.replaced[v.section] = true
		b.note(v.section, fmt.Sprintf("the request %s are written in code (%s:%d): names read are not claimed", part.role, ref.file, e.Line))
	}
}

// syncNextHeaders reports a use of an un-awaited next/headers headers()
// or cookies() call other than binding it to a const (SH-N6): Next.js 16
// removed synchronous access (the call returns a promise), so a member
// read throws there.
func (b *shapeBuilder) syncNextHeaders(ref fnRef, e facts.ShapeEvent) (int, string, bool) {
	if e.Param != -2 || len(e.Path) == 0 || (e.Path[0] != "headers()sync" && e.Path[0] != "cookies()sync") {
		return 0, "", false
	}
	name := strings.TrimSuffix(e.Path[0], "()sync")
	if !b.importedFrom(ref, name, "next/headers") {
		return 0, "", false
	}
	if e.Kind == "read" && e.Bound && len(e.Path) == 1 {
		return 0, "", false
	}
	return e.Line, name, true
}

// full0 is the first request path element of a urlwhole use.
func full0(e facts.ShapeEvent, part nextPart) string {
	_ = part
	if len(e.Path) > 0 {
		return e.Path[0]
	}
	return ""
}

// json handles a use of the parsed JSON body.
func (v *nextRequestVisitor) json(ref fnRef, idx int, e facts.ShapeEvent, frame routeshape.Frame, rest []string, prov func(string) routeshape.Provenance, calls *[]routeshape.Call) {
	b := v.b
	switch e.Kind {
	case "read":
		switch {
		case len(rest) == 0 && e.Bound:
		case len(rest) == 0:
			b.tag("SH-N4")
			b.note(v.section, fmt.Sprintf("the JSON body is read as a whole (%s:%d)", ref.file, e.Line))
		default:
			b.bodyField(rest[0], prov("SH-N8"))
		}
	case "keys":
		if len(rest) == 0 {
			for _, k := range e.Keys {
				b.bodyField(k, prov("SH-N8"))
			}
			if e.Open {
				b.tag("SH-N4")
				b.note(v.section, fmt.Sprintf("the JSON body is destructured with a rest element (%s:%d)", ref.file, e.Line))
			}
		} else {
			b.bodyField(rest[0], prov("SH-N8"))
		}
	case "call":
		if len(rest) > 0 {
			b.bodyField(rest[0], prov("SH-N8"))
		} else {
			b.tag("SH-N4")
			b.note(v.section, fmt.Sprintf("a method is called on the JSON body (%s:%d)", ref.file, e.Line))
		}
	case "pass":
		switch {
		case len(rest) > 0:
			b.bodyField(rest[0], prov("SH-N8"))
			if b.provenTarget(ref, e) {
				b.tag("SH-N11")
				*calls = append(*calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "a member of the body (" + strings.Join(rest, ".") + ") is passed on"})
			}
		case e.Transformed:
			b.tag("SH-N11")
			*calls = append(*calls, routeshape.Call{Site: site(ref, e), Stop: routeshape.StopTransformed, Detail: "the JSON body is spread into another value"})
		default:
			if b.validator(v.section, ref, idx, frame) {
				return
			}
			call := b.callFor(ref, idx, frame, "json", v.section)
			*calls = append(*calls, call)
			if call.Stop == "" {
				b.typedParam(ref, e, call, frame)
			}
		}
	case "cast":
		if len(rest) == 0 && e.Type != nil {
			b.intended(routeshape.SectionBody, *e.Type, ref.file, b.prov("SH-N16", routeshape.Intended, ref.file, e.Line, frame.Chain))
		}
	case "assign":
		b.tag("SH-N4")
		b.replaced[v.section] = true
		b.note(v.section, fmt.Sprintf("the JSON body is written in code (%s:%d): names read are not claimed", ref.file, e.Line))
	}
}

// typedParam reads the declared type of the callee parameter the JSON
// body is passed to (SH-N16, kind intended).
func (b *shapeBuilder) typedParam(ref fnRef, e facts.ShapeEvent, call routeshape.Call, frame routeshape.Frame) {
	t, ok := b.lookup(call.Target)
	if !ok || t.fn == nil || e.Arg >= len(t.fn.Params) || t.fn.Params[e.Arg].Type == nil {
		return
	}
	chain := append(append([]routeshape.Step{}, frame.Chain...), routeshape.Step{File: call.Site.File, Line: call.Site.Line, Function: t.step.Function})
	b.intended(routeshape.SectionBody, *t.fn.Params[e.Arg].Type, t.file, b.prov("SH-N16", routeshape.Intended, t.file, t.fn.Line, chain))
}

// read records one name read of a map part.
func (v *nextRequestVisitor) read(part nextPart, name string, prov func(string) routeshape.Provenance) {
	b := v.b
	switch part.section {
	case routeshape.SectionBody:
		b.bodyField(name, prov("SH-N9"))
	default:
		b.param(part.section, name, prov(part.row))
	}
}

// pendingNext is a response read in code: explicit is false for the
// status a response takes when none is given (200, 307, 302).
type pendingNext struct {
	status   string
	explicit bool
	ct       *string
	body     *routeshape.Schema
	returned *routeshape.Schema
	prov     routeshape.Provenance
}

type nextResponseVisitor struct {
	b        *shapeBuilder
	unproven bool
	sends    []pendingNext
	// sync names an un-awaited next/headers use on the response chain.
	sync string
}

// roleCaught is the value of a returned call followed from a call site
// inside a try block or a promise chain (SH-N20): an error thrown below
// it, such as notFound(), may be caught on the way up.
const roleCaught = "retcaught"

// nextResponses follows the handler's return values (SH-N17..SH-N23).
func (b *shapeBuilder) nextResponses() {
	h := b.info.handler
	v := &nextResponseVisitor{b: b}
	res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: valueKey(0, "ret")}, b.l.depth, v)
	if err == nil {
		b.addChains(routeshape.SectionResponses, res)
	}
	if b.info.answered != "" && len(v.sends) > 0 {
		b.tag("SH-N24")
		b.note(routeshape.SectionResponses, "no response is claimed: "+b.info.answered)
		v.sends = nil
	}
	if b.info.static != "" && len(v.sends) > 0 && b.staticReads() {
		b.note(routeshape.SectionResponses, "no response is claimed: the handler reads the query, headers or cookies, and "+b.info.static)
		v.sends = nil
	}
	if v.sync != "" && len(v.sends) > 0 {
		b.tag("SH-N6")
		b.note(routeshape.SectionResponses, "no response is claimed: "+v.sync)
		v.sends = nil
	}
	if b.info.node.Attributes["method"] == "HEAD" && b.l.nextGET[b.info.node.File] && len(v.sends) > 0 {
		// Next.js may prerender the file's GET at build time and answer HEAD
		// from that output without running this handler (seen with 14.1.0).
		b.tag("SH-N22")
		b.note(routeshape.SectionResponses, "the file also exports GET: a HEAD request may be answered from GET's prerendered output, so no response is claimed")
		v.sends = nil
	}
	for _, d := range v.sends {
		if b.info.unread != "" {
			if !d.explicit {
				b.note(routeshape.SectionResponses, fmt.Sprintf("status of the response at %s:%d not claimed: %s", d.prov.File, d.prov.Line, b.info.unread))
				continue
			}
			d.ct, d.body, d.returned = nil, nil, nil
		}
		b.responseCT(d.status, d.ct, d.body, d.prov, d.returned)
	}
	if b.info.unread != "" && len(v.sends) > 0 {
		b.note(routeshape.SectionResponses, "content types and bodies not claimed: "+b.info.unread+" and may set response headers")
	}
}

// staticReads reports, for a route with a static route config, that the
// handler or a function the request flows to uses the query, headers or
// cookies (a name read or any other use noted): its statuses may depend on
// the values Next.js empties. A handler that uses none answers the same.
func (b *shapeBuilder) staticReads() bool {
	s := b.shape
	for _, sec := range []struct {
		name  string
		items int
	}{{routeshape.SectionQuery, len(s.Query.Items)}, {routeshape.SectionHeader, len(s.Header.Items)}, {routeshape.SectionCookie, len(s.Cookie.Items)}, {routeshape.SectionBody, len(s.Body.Items)}} {
		if sec.name == routeshape.SectionBody && !b.info.staticBody {
			continue
		}
		if sec.items > 0 || len(b.notes[sec.name]) > 1 {
			return true
		}
	}
	return false
}

// Visit reads one function's returns (and next/navigation calls).
func (v *nextResponseVisitor) Visit(_ context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	b := v.b
	ref, ok := b.lookup(frame.Function)
	if !ok {
		return nil, fmt.Errorf("function %s not indexed", frame.Function.Function)
	}
	if ref.fn == nil {
		b.tag("SH-N23")
		b.note(routeshape.SectionResponses, fmt.Sprintf("the returns of %s (%s:%d) are not read", ref.step.Function, ref.file, ref.step.Line))
		return nil, nil
	}
	var calls []routeshape.Call
	returns := 0
	_, role := exParseValue(frame.Value)
	caught := role == roleCaught
	for _, e := range ref.fn.Events {
		if line, name, ok := b.syncNextHeaders(ref, e); ok && v.sync == "" && b.info.framework != frameworkAction {
			v.sync = fmt.Sprintf("%s() is used without await at %s:%d, which throws on Next.js 16", name, ref.file, line)
		}
		switch {
		case e.Kind == "call" && e.Param == -2 && len(e.Path) == 1 && shapeNavName(e.Path[0]) && e.Nested:
			if b.importedFrom(ref, e.Path[0], "next/navigation") {
				b.tag("SH-N23")
				b.note(routeshape.SectionResponses, fmt.Sprintf("%s() in a nested function or callback (%s:%d) is not read", e.Path[0], ref.file, e.Line))
			}
		case e.Kind == "throw":
			b.tag("SH-N23")
			b.note(routeshape.SectionResponses, fmt.Sprintf("an error is thrown (%s:%d): Next.js answers 500 unless it is a framework error; not read", ref.file, e.Line))
		case e.Kind == "call" && e.Param == -2 && len(e.Path) == 1 && shapeNavName(e.Path[0]):
			v.navigation(ref, e, frame, caught)
		case e.Kind == "session" && len(e.Path) == 1:
			b.nextSession(ref, e)
		case e.Kind == "return":
			returns++
			if c := v.ret(ref, e, frame); c != nil {
				calls = append(calls, *c)
			}
		}
	}
	if returns == 0 && b.info.framework != frameworkAction {
		b.tag("SH-N23")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s returns no response read (%s:%d)", ref.step.Function, ref.file, ref.step.Line))
	}
	return calls, nil
}

func shapeNavName(s string) bool {
	switch s {
	case "notFound", "redirect", "permanentRedirect", "forbidden", "unauthorized":
		return true
	}
	return false
}

// navigation reads a next/navigation call in a route handler (SH-N20).
func (v *nextResponseVisitor) navigation(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame, caught bool) {
	b := v.b
	name := e.Path[0]
	if !b.importedFrom(ref, name, "next/navigation") {
		return
	}
	if b.info.framework == frameworkAction {
		b.tag("SH-N28")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s() ends the action (%s:%d): not modelled", name, ref.file, e.Line))
		return
	}
	if e.InTry {
		b.tag("SH-N22")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s() inside a try block may be caught (%s:%d): not claimed", name, ref.file, e.Line))
		return
	}
	if caught {
		b.tag("SH-N22")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s() (%s:%d) is reached through a call inside a try block or a promise chain, which may catch it: not claimed", name, ref.file, e.Line))
		return
	}
	prov := b.prov("SH-N20", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)
	empty := &routeshape.Schema{Type: routeshape.TypeNull}
	switch name {
	case "notFound":
		v.sends = append(v.sends, pendingNext{status: "404", explicit: true, body: empty, prov: prov})
	case "redirect":
		if len(e.Methods) > 0 && len(e.Methods[0].Args) > 1 {
			b.tag("SH-N22")
			b.note(routeshape.SectionResponses, fmt.Sprintf("redirect() with a type argument (%s:%d): status not read", ref.file, e.Line))
			return
		}
		v.sends = append(v.sends, pendingNext{status: "307", explicit: true, body: empty, prov: prov})
	case "permanentRedirect":
		if len(e.Methods) > 0 && len(e.Methods[0].Args) > 1 {
			b.tag("SH-N22")
			b.note(routeshape.SectionResponses, fmt.Sprintf("permanentRedirect() with a type argument (%s:%d): status not read", ref.file, e.Line))
			return
		}
		v.sends = append(v.sends, pendingNext{status: "308", explicit: true, body: empty, prov: prov})
	default:
		b.tag("D")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s() (%s:%d) needs the experimental authInterrupts option: not read", name, ref.file, e.Line))
	}
}

// ret reads one return of a route handler or a followed function.
func (v *nextResponseVisitor) ret(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame) *routeshape.Call {
	b := v.b
	if b.info.framework == frameworkAction {
		v.actionReturn(ref, e, frame)
		return nil
	}
	x, viaConst := returnedExpr(e)
	notRead := func(what string) {
		b.tag("SH-N23")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s (%s:%d)", what, ref.file, e.Line))
	}
	if x == nil {
		notRead("a return without a value: Next.js answers 500")
		return nil
	}
	if form, ok := b.responseCall(ref, *x); ok {
		var args []facts.ShapeExpr
		var inits []*facts.ShapeExpr
		if len(e.Methods) > 1 {
			args, inits = e.Methods[1].Args, e.Methods[1].Inits
		}
		v.build(ref, e, frame, form, *x, args, inits, viaConst)
		return nil
	}
	// A returned call of a function: followed when the target is proven.
	if x.Kind != "call_expression" || x.Object == nil || viaConst {
		notRead("a returned value not read")
		return nil
	}
	if x.Object.Kind == "identifier" && shapeNavName(x.Object.Name) && b.importedFrom(ref, x.Object.Name, "next/navigation") {
		return nil
	}
	callee := *x.Object
	t, stop, detail := b.target(ref, facts.ShapeEvent{Callee: &callee})
	call := routeshape.Call{Site: routeshape.Step{File: ref.file, Line: e.Line, Function: exprText(callee)}}
	b.tag("SH-N25")
	if stop != "" {
		call.Stop, call.Detail = stop, detail
		return &call
	}
	if r, ok := b.refs[t.step]; ok && r.file == "" {
		call.Stop, call.Detail = routeshape.StopAmbiguous, "two functions at one place"
		return &call
	}
	role := "ret"
	if _, r := exParseValue(frame.Value); r == roleCaught || e.InTry || promiseChain(callee) {
		role = roleCaught
	}
	call.Target, call.Value = t.step, valueKey(0, role)
	return &call
}

// promiseChain reports a callee that is a then, catch or finally method:
// a rejection may be handled there.
func promiseChain(callee facts.ShapeExpr) bool {
	return callee.Kind == "member_expression" && (callee.Name == "then" || callee.Name == "catch" || callee.Name == "finally")
}

// build reads a response built in place (SH-N17, SH-N18, SH-N19).
func (v *nextResponseVisitor) build(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame, form string, x facts.ShapeExpr, args []facts.ShapeExpr, inits []*facts.ShapeExpr, viaConst bool) {
	b := v.b
	arg := func(i int) *facts.ShapeExpr {
		if i < len(x.Args) {
			return &x.Args[i]
		}
		return nil
	}
	notClaimed := func(what string) {
		b.tag("SH-N22")
		b.note(routeshape.SectionResponses, fmt.Sprintf("%s (%s:%d)", what, ref.file, e.Line))
	}
	switch form {
	case "json":
		rule := "SH-N17"
		status, explicit, ok := "200", false, true
		if init := arg(1); init != nil {
			status, explicit, ok = initStatus(*init)
		}
		if !ok || !responseStatus(status) || nullBodyStatus(status) {
			notClaimed("the status of a JSON response is not read or not one a JSON response can have")
			return
		}
		if a := arg(0); a == nil || a.Kind == "undefined" || a.Kind == "arrow_function" || a.Kind == "function_expression" || a.Kind == "function" {
			notClaimed("a JSON response of undefined or a function: Response.json throws (Next.js answers 500)")
			return
		}
		ct, set, known := initContentType(arg(1))
		var ctp *string
		switch {
		case !known:
			notClaimed("the headers of a JSON response are not read: content type not claimed")
		case set:
			notClaimed("a JSON response sets its own Content-Type: content type not claimed")
			_ = ct
		default:
			j := "application/json"
			ctp = &j
		}
		if viaConst {
			ctp = nil
			b.note(routeshape.SectionResponses, fmt.Sprintf("the response returned at %s:%d is a const whose headers may be changed: content type not claimed", ref.file, e.Line))
		}
		var body, returned *routeshape.Schema
		if a := arg(0); a != nil {
			m := facts.ShapeMethod{Args: []facts.ShapeExpr{*a}}
			if len(args) > 0 && len(inits) > 0 {
				m.Inits = inits[:1]
			}
			if a.Kind == "call_expression" && a.Object != nil {
				returned = b.returnedType(ref, *a.Object)
			} else {
				body, returned = b.responseBody(ref, m, 0)
			}
		}
		v.sends = append(v.sends, pendingNext{status: status, explicit: explicit, ct: ctp, body: body, returned: returned, prov: b.prov(rule, routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)})
	case "redirect":
		class := x.Object.Object.Name
		var urlInit *facts.ShapeExpr
		if len(inits) > 0 {
			urlInit = inits[0]
		}
		if a := arg(0); a == nil || !b.absoluteURL(ref, *a, urlInit, frame) {
			notClaimed(class + ".redirect with a URL not proven absolute (a relative URL throws: Next.js answers 500)")
			return
		}
		status, explicit := "307", false
		if class == "Response" {
			status = "302"
		}
		if init := arg(1); init != nil {
			switch {
			case init.Kind == "number":
				status, explicit = init.Name, true
			case class == "NextResponse" && init.Kind == "object":
				s, ex, ok := initStatus(*init)
				if !ok {
					notClaimed("the status of a redirect is not read")
					return
				}
				if ex {
					status, explicit = s, true
				}
			default:
				notClaimed("the status of a redirect is not read")
				return
			}
		}
		if !redirectStatus(status) {
			notClaimed("a redirect status that the Fetch API refuses")
			return
		}
		v.sends = append(v.sends, pendingNext{status: status, explicit: explicit, body: &routeshape.Schema{Type: routeshape.TypeNull}, prov: b.prov("SH-N19", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)})
	case "new":
		status, explicit, ok := "200", false, true
		if init := arg(1); init != nil {
			status, explicit, ok = initStatus(*init)
		}
		if !ok || !responseStatus(status) {
			notClaimed("the status of a constructed response is not read or out of range")
			return
		}
		body := arg(0)
		empty := body == nil || body.Kind == "null" || body.Kind == "undefined"
		if nullBodyStatus(status) && !empty {
			notClaimed("a body with a status that has none (the constructor throws)")
			return
		}
		ct, set, known := initContentType(arg(1))
		var schema *routeshape.Schema
		var ctp *string
		switch {
		case empty:
			schema = &routeshape.Schema{Type: routeshape.TypeNull}
		case body.Kind == "literal" || body.Kind == "template_string":
			schema = &routeshape.Schema{Type: routeshape.TypeString}
			if !set && known {
				t := "text/plain;charset=UTF-8"
				ctp = &t
			}
		}
		switch {
		case !known:
			notClaimed("the headers of a constructed response are not read: content type not claimed")
			ctp = nil
		case set && ct != nil:
			ctp = ct
		case set:
			notClaimed("a Content-Type header value not read")
			ctp = nil
		}
		if viaConst {
			ctp = nil
			b.note(routeshape.SectionResponses, fmt.Sprintf("the response returned at %s:%d is a const whose headers may be changed: content type not claimed", ref.file, e.Line))
		}
		if ctp == nil && !empty && !set && known && schema == nil {
			b.note(routeshape.SectionResponses, fmt.Sprintf("the body of the response at %s:%d is not read: content type not claimed", ref.file, e.Line))
		}
		v.sends = append(v.sends, pendingNext{status: status, explicit: explicit, ct: ctp, body: schema, prov: b.prov("SH-N18", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)})
	default:
		b.tag("D")
		b.note(routeshape.SectionResponses, fmt.Sprintf("a response built with %s is not read (%s:%d)", exprText(*x.Object), ref.file, e.Line))
	}
}

// absoluteURL reports a redirect target proven absolute (SH-N19): a string
// literal starting with http:// or https://, `new URL(x, base)` with base
// such a literal or the handler's request.url or request.nextUrl, `new
// URL(request.url)`, or `request.nextUrl.clone()`; directly or through
// the const it is bound to. The request is the route handler's first
// parameter, read in the handler itself.
func (b *shapeBuilder) absoluteURL(ref fnRef, x facts.ShapeExpr, init *facts.ShapeExpr, frame routeshape.Frame) bool {
	if x.Kind == "identifier" && init != nil {
		x = *init
	}
	httpLiteral := func(e facts.ShapeExpr) bool {
		return e.Kind == "literal" && (strings.HasPrefix(e.Name, "http://") || strings.HasPrefix(e.Name, "https://"))
	}
	request := ""
	if len(frame.Chain) == 0 && ref.fn != nil && len(ref.fn.Params) > 0 && b.info.handler != nil && ref.step == b.info.handler.step {
		request = ref.fn.Params[0].Name
	}
	requestURL := func(e facts.ShapeExpr) bool {
		return request != "" && e.Kind == "member_expression" && (e.Name == "url" || e.Name == "nextUrl") && e.Object != nil && e.Object.Kind == "identifier" && e.Object.Name == request
	}
	switch {
	case httpLiteral(x):
		return true
	case x.Kind == "new_expression" && x.Object != nil && x.Object.Kind == "identifier" && x.Object.Name == "URL" && b.globalName(ref, "URL"):
		switch len(x.Args) {
		case 1:
			return requestURL(x.Args[0]) || httpLiteral(x.Args[0])
		case 2:
			return requestURL(x.Args[1]) || httpLiteral(x.Args[1])
		}
	case x.Kind == "call_expression" && len(x.Args) == 0 && x.Object != nil && x.Object.Kind == "member_expression" && x.Object.Name == "clone" && x.Object.Object != nil:
		o := *x.Object.Object
		return requestURL(o) && o.Name == "nextUrl"
	}
	return false
}

// globalName reports that name is not bound in ref's function or file.
func (b *shapeBuilder) globalName(ref fnRef, name string) bool {
	if ref.fn != nil {
		for _, l := range ref.fn.Locals {
			if l == name {
				return false
			}
		}
		for _, p := range ref.fn.Params {
			if p.Name == name {
				return false
			}
		}
	}
	_, _, ok := b.l.r.binding(ref.file, ref.scope, name)
	return !ok
}

// responseCT adds or merges a response item with a content type.
func (b *shapeBuilder) responseCT(status string, ct *string, body *routeshape.Schema, prov routeshape.Provenance, returned *routeshape.Schema) {
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
	add(prov.Kind, body, prov)
	if returned != nil {
		p := prov
		p.Kind = routeshape.Returned
		p.Rule = b.row("SH-E28")
		add(routeshape.Returned, returned, p)
	}
}

// nextFinish sets the section states and reasons of a route or action.
func (b *shapeBuilder) nextFinish() {
	s := b.shape
	if b.info.staticBody {
		// dynamic = 'error': a body read or validated may throw first.
		s.Body.Items = nil
	}
	b.dropWritten()
	s.Models = b.sr.models.models
	params := map[string]*routeshape.ParamSection{routeshape.SectionQuery: &s.Query, routeshape.SectionHeader: &s.Header, routeshape.SectionCookie: &s.Cookie}
	for name, sec := range params {
		if b.info.framework == frameworkAction && name == routeshape.SectionQuery {
			continue
		}
		sortParams(sec.Items)
		b.state(name, &sec.Section, len(sec.Items), "names read in code; others may exist")
	}
	for i := range s.Body.Items {
		if s.Body.Items[i].Provenance.Kind == routeshape.ReadInHandler && s.Body.Items[i].Schema != nil {
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
	base := "statuses read from returned responses; thrown errors, middleware and unread calls may add others"
	if b.info.framework == frameworkAction {
		base = "results read from returned values; thrown errors and redirects end the action otherwise"
	}
	b.state(routeshape.SectionResponses, &s.Responses.Section, len(s.Responses.Items), base)
	s.Security.Reason = "security is not read for Next.js route handlers and server actions"
	if b.info.framework != frameworkAction && len(b.sessions) > 0 {
		s.Security.Reason = ""
		col := &secCollector{sources: b.sessions}
		col.reason("middleware and proxy files are not read for auth")
		col.apply(s)
	}
}

// actionShape builds the shape of a server action (SH-N27..SH-N29).
func (l *shapeLinker) actionShape(n *contract.Node) *routeshape.Shape {
	shape := routeshape.New(l.depth)
	info := routeInfo{node: n, framework: frameworkAction, libs: map[string]middlewareUse{}}
	b := newShapeBuilder(l, &info, shape)
	own := "a server action is a POST to the page that renders it: it has no path or query parameters of its own"
	shape.Path.Reason, shape.Query.Reason = own, own
	if identity, _ := n.Attributes["handler"].(string); identity != "" {
		if ref, ok := l.byIdentity(identity); ok {
			info.handler = &ref
		} else {
			info.handlerNote = "action syntax not found"
		}
	} else {
		info.handlerNote = "the action is not a named function (" + fmt.Sprint(n.Attributes["form"]) + ")"
	}
	if info.handler == nil {
		tagShape("SH-N29")
		reason := "action not read: " + info.handlerNote
		for _, sec := range []*routeshape.Section{&shape.Header.Section, &shape.Cookie.Section, &shape.Body.Section, &shape.Responses.Section} {
			sec.Reason = reason
		}
		return shape
	}
	h := info.handler
	var starts []string
	if h.fn != nil {
		typed := 0
		for i, p := range h.fn.Params {
			switch {
			case p.Rest || p.Name == "":
				b.note(routeshape.SectionBody, fmt.Sprintf("argument %d is a rest or destructured parameter: not read", i+1))
			case p.Type == nil || p.Type.Kind == "name" && p.Type.Name == "FormData":
				starts = append(starts, valueKey(i, "form"))
				if p.Type == nil {
					b.note(routeshape.SectionBody, fmt.Sprintf("argument %s has no type: names read with get() are listed", p.Name))
				}
			default:
				typed++
			}
		}
		if len(h.fn.Params) == 1 && typed == 1 {
			p := h.fn.Params[0]
			if !opaque(*p.Type) {
				s := b.sr.tsSchema(h.file, *p.Type, routeshape.Intended, 0)
				shape.Body.Items = append(shape.Body.Items, routeshape.Content{Schema: &s, Provenance: b.prov("SH-N27", routeshape.Intended, h.file, h.fn.Line, nil)})
				b.note(routeshape.SectionBody, actionArgumentNote)
			}
			starts = append(starts, valueKey(0, "typed"))
		} else if typed > 0 {
			b.tag("SH-N27")
			b.note(routeshape.SectionBody, "several typed arguments: their types are not listed")
		}
	} else {
		b.note(routeshape.SectionBody, "the action uses no parameter in a read form")
	}
	if starts == nil {
		starts = []string{valueKey(-1, "none")}
	}
	b.nextRequest(0, starts)
	v := &nextResponseVisitor{b: b}
	res, err := routeshape.Follow(context.Background(), routeshape.Frame{Function: h.step, Value: valueKey(0, "ret")}, l.depth, v)
	if err == nil {
		b.addChains(routeshape.SectionResponses, res)
	}
	if h.fn != nil && h.fn.Returns != nil {
		rt := *h.fn.Returns
		if rt.Kind == "name" && rt.Name == "Promise" && len(rt.Args) == 1 {
			rt = rt.Args[0]
		}
		if !opaque(rt) {
			s := b.sr.tsSchema(h.file, rt, routeshape.Returned, 0)
			if s.Type != routeshape.TypeUnknown {
				b.responseCT("default", nil, &s, b.prov("SH-N28", routeshape.Returned, h.file, h.fn.Line, nil), nil)
			}
		}
	}
	for _, d := range v.sends {
		b.responseCT(d.status, d.ct, d.body, d.prov, d.returned)
	}
	b.nextFinish()
	return shape
}

// actionArgumentNote says what a server action's body schema describes
// (SH-N12, SH-N27): its arguments are sent as a list, and a schema or a
// type describes the first one only; further arguments are not checked.
const actionArgumentNote = "the action's arguments are sent as a list: a schema here describes argument 1 only; further arguments are not checked and may be anything"

// actionReturn reads one return of a server action (SH-N28): an object
// literal's fields, or the declared return type of a proven callee.
func (v *nextResponseVisitor) actionReturn(ref fnRef, e facts.ShapeEvent, frame routeshape.Frame) {
	b := v.b
	x, _ := returnedExpr(e)
	if x == nil {
		return
	}
	switch {
	case x.Kind == "object":
		m := facts.ShapeMethod{Args: []facts.ShapeExpr{*x}}
		body, _ := b.responseBody(ref, m, 0)
		v.sends = append(v.sends, pendingNext{status: "default", body: body, prov: b.prov("SH-N28", routeshape.ReadInHandler, ref.file, e.Line, frame.Chain)})
	case x.Kind == "call_expression" && x.Object != nil:
		if s := b.returnedType(ref, *x.Object); s != nil {
			p := b.prov("SH-N28", routeshape.Returned, ref.file, e.Line, frame.Chain)
			v.sends = append(v.sends, pendingNext{status: "default", body: s, prov: p})
			return
		}
		b.tag("SH-N28")
		b.note(routeshape.SectionResponses, fmt.Sprintf("a returned call is not read (%s:%d)", ref.file, e.Line))
	default:
		b.tag("SH-N28")
		b.note(routeshape.SectionResponses, fmt.Sprintf("a returned value is not read (%s:%d)", ref.file, e.Line))
	}
}

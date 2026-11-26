package routes

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
	"github.com/graphvein/graphvein/internal/lang/rust"
)

// This file builds the route shape of Axum routes (task T029,
// docs/contract/claims/route-shape-axum.md) at link time from the cached
// Rust shape payload (facts.RustShape): the handler's extractor
// parameters, the type of its result, and the request headers its
// extractors and helpers read.

type axumFn struct {
	file string
	fn   *facts.RustFnShape
}

// axumShaper holds the indexes one link run reads shapes from.
type axumShaper struct {
	modules  *rust.ModuleResolver
	depth    int
	types    map[string]*axumTypeDecl
	fns      map[string]*axumFn
	steps    map[string]string
	traitFns []string
	versions *axumVersions
	// macroRules caches macroRulesNamed per name.
	macroRules map[string]bool
}

func newAxumShaper(files map[string]facts.Facts, modules *rust.ModuleResolver, depth int) *axumShaper {
	s := &axumShaper{modules: modules, depth: depth, types: map[string]*axumTypeDecl{}, fns: map[string]*axumFn{}, steps: map[string]string{}}
	s.index(files)
	return s
}

// paramRole is what a handler parameter or a followed value is.
type paramRole int

const (
	roleNone    paramRole = iota // reads no request part (State, Extension, ...)
	rolePath                     // Path<T>
	roleQuery                    // Query<T>
	roleJSON                     // Json<T>
	roleForm                     // Form<T>
	roleText                     // String body
	roleBytes                    // Bytes body
	roleTyped                    // TypedHeader<T>
	roleHeaders                  // HeaderMap
	roleRequest                  // Request, Parts, RequestParts
	roleURI                      // Uri, OriginalUri, RawQuery
	roleCustom                   // a repository extractor
	roleWrapped                  // Option<X>, Result<X, _>
	roleUnknown                  // anything else
)

type handlerParam struct {
	p      facts.RustParam
	role   paramRole
	t      *tref // the extractor type
	inner  *tref // its type argument
	opt    bool  // Option<TypedHeader<T>>
	method string
	// custom extractors: the impl method read, and whether it is
	// FromRequestParts (cannot read the body).
	impl  string
	parts bool
	why   string
}

// extractorCrates are the crates whose extractor types the reader knows.
const extractorCrates = "axum axum_core axum_extra"

func (s *axumShaper) classify(file string, scope int, p facts.RustParam) handlerParam {
	h := handlerParam{p: p, role: roleUnknown}
	t := s.resolveType(p.Type, file, scope, nil, 0)
	h.t = t
	h.inner = t.arg(0)
	switch {
	case t.std("Option") || t.std("Result"):
		inner := t.arg(0)
		if t.std("Option") && inner != nil && inner.kind == "ext" && inner.name == "TypedHeader" && facts.Builtin(inner.crate, extractorCrates) {
			h.role, h.opt, h.inner, h.t = roleTyped, true, inner.arg(0), inner
			return h
		}
		h.role, h.why = roleWrapped, "extractor "+p.Type.Text+" (a wrapped extractor) is not read"
		return h
	case t.kind == "ext" && facts.Builtin(t.crate, extractorCrates):
		switch t.name {
		case "Path":
			h.role = rolePath
		case "Query":
			h.role = roleQuery
		case "Json":
			h.role = roleJSON
		case "Form":
			h.role = roleForm
		case "TypedHeader":
			h.role = roleTyped
		case "State", "Extension", "ConnectInfo", "MatchedPath", "NestedPath":
			h.role = roleNone
		case "Request", "RequestParts", "Parts":
			h.role = roleRequest
		case "OriginalUri", "RawQuery", "Uri":
			h.role = roleURI
		case "HeaderMap":
			h.role = roleHeaders
		case "Method":
			h.role = roleNone
		case "Bytes":
			h.role = roleBytes
		}
	case t.kind == "ext" && facts.Builtin(t.crate, "http reqwest hyper"):
		switch t.name {
		case "HeaderMap":
			h.role = roleHeaders
		case "Request", "Parts":
			h.role = roleRequest
		case "Uri":
			h.role = roleURI
		case "Method":
			h.role = roleNone
		}
	case t.is("bytes", "Bytes"):
		h.role = roleBytes
	case t.std("String"):
		h.role = roleText
	case t.kind == "local":
		if impl, parts, ok := s.extractorImpl(t.decl.decl.Identity); ok {
			h.role, h.impl, h.parts = roleCustom, impl, parts
			return h
		}
		h.why = "extractor " + p.Type.Text + " has no FromRequestParts or FromRequest impl that was read"
		return h
	}
	if h.role == roleUnknown {
		h.why = "extractor " + p.Type.Text + " is not read"
	}
	return h
}

// extractorImpl finds the from_request_parts or from_request method of a
// FromRequestParts or FromRequest impl for a repository type.
func (s *axumShaper) extractorImpl(typeID string) (string, bool, bool) {
	var found []string
	parts := false
	for _, id := range s.traitFns {
		f := s.fns[id]
		trait := f.fn.Trait
		if i := strings.Index(trait, "<"); i >= 0 {
			trait = trait[:i]
		}
		trait = lastSegment(trait)
		_, _, name, _ := contract.ParseIdentity(id)
		method := name
		if i := strings.LastIndex(method, "."); i >= 0 {
			method = method[i+1:]
		}
		if i := strings.Index(method, "@"); i >= 0 {
			method = method[:i]
		}
		if !(trait == "FromRequestParts" && method == "from_request_parts" || trait == "FromRequest" && method == "from_request") {
			continue
		}
		self := f.fn.SelfType
		if i := strings.Index(self, "<"); i >= 0 {
			self = self[:i]
		}
		b := s.modules.Resolve(f.file, f.fn.Scope, self)
		if b.Identity == typeID && !b.Ambiguous {
			found = append(found, id)
			parts = trait == "FromRequestParts"
		}
	}
	if len(found) != 1 {
		return "", false, false
	}
	return found[0], parts, true
}

// templateParams returns the parameter names of a normalized template.
func templateParams(template string) []string {
	var out []string
	for _, seg := range strings.Split(template, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.TrimPrefix(strings.TrimSuffix(seg[1:], "}"), "*"))
		}
	}
	return out
}

// formattingMacro proves that a macro named like a formatting macro is
// one (row SH-X14): a path or import resolving to log's or tracing's
// level macros or std's formatting macros, or an unqualified std prelude
// macro (format, print, println, eprint, eprintln, write, writeln) when
// no `macro_rules!` of that name appears in any Rust source of the
// repository. A repository macro of the same name may do anything.
func (s *axumShaper) formattingMacro(file string, scope int, path string) bool {
	last := lastSegment(path)
	b := s.modules.Resolve(file, scope, path)
	if b.Ambiguous || b.Identity != "" {
		return false
	}
	switch {
	case facts.Builtin(b.External, "log tracing"):
		return facts.Builtin(lastSegment(b.Name), "trace debug info warn error")
	case facts.Builtin(b.External, stdCrates):
		return facts.Builtin(lastSegment(b.Name), stdFormatMacros)
	case (b.External == "" || b.External == last) && !strings.Contains(path, "::") && facts.Builtin(last, stdFormatMacros):
		// Unresolved: the resolver names an unknown single segment as a
		// crate of that name.
		return !s.macroRulesNamed(last)
	}
	return false
}

// stdFormatMacros are std's formatting macros of the prelude.
const stdFormatMacros = "format print println eprint eprintln write writeln"

// macroRulesNamed reports whether any retained Rust source defines a
// `macro_rules!` of the name (true when the sources are not read).
func (s *axumShaper) macroRulesNamed(name string) bool {
	if s.versions == nil || s.versions.sources == nil {
		return true
	}
	if v, ok := s.macroRules[name]; ok {
		return v
	}
	re := regexp.MustCompile(`macro_rules!\s*` + regexp.QuoteMeta(name) + `\b`)
	found := false
	for file, src := range s.versions.sources {
		if strings.HasSuffix(file, ".rs") && re.Match(src) {
			found = true
			break
		}
	}
	if s.macroRules == nil {
		s.macroRules = map[string]bool{}
	}
	s.macroRules[name] = found
	return found
}

// bound reports whether name is bound inside the function: a parameter,
// or any pattern or nested item of the body.
func (f *axumFn) bound(name string) bool {
	for _, p := range f.fn.Params {
		if p.Name == name || p.Inner == name {
			return true
		}
	}
	for _, b := range f.fn.Bindings {
		if b == name {
			return true
		}
	}
	return false
}

// shapeBuild collects one route's shape.
type shapeBuild struct {
	s       *axumShaper
	sh      *routeshape.Shape
	file    string
	reasons map[string][]string
	seen    map[string]bool
	// headersTaken says why a later extractor always fails (row SH-X11):
	// a HeaderMap took the headers before it.
	headersTaken string
}

func (b *shapeBuild) reason(section, text string) {
	key := section + "\x00" + text
	if b.seen[key] {
		return
	}
	b.seen[key] = true
	b.reasons[section] = append(b.reasons[section], text)
}

func fact(rule, file string, line int) routeshape.Provenance {
	return routeshape.Provenance{Kind: routeshape.Fact, Rule: rule, File: file, Line: line, Chain: []routeshape.Step{}, Confidence: 1}
}

func axumProv(kind routeshape.Kind, rule, file string, line int, chain []routeshape.Step) routeshape.Provenance {
	c := make([]routeshape.Step, len(chain))
	copy(c, chain)
	return routeshape.Provenance{Kind: kind, Rule: rule, File: file, Line: line, Chain: c, Confidence: 1}
}

func strp(s string) *string { return &s }

// shapeOf builds the shape attribute of one route.
func (s *axumShaper) shapeOf(reg axumRegistration, handler string, entries []pathEntry, attrs map[string]any) (map[string]any, *routeshape.Shape, error) {
	b := &shapeBuild{s: s, sh: routeshape.New(s.depth), file: reg.file, reasons: map[string][]string{}, seen: map[string]bool{}}
	f := s.fns[handler]
	var params []handlerParam
	if f != nil {
		for _, p := range f.fn.Params {
			if !p.Self {
				params = append(params, s.classify(f.file, f.fn.Scope, p))
			}
		}
	}
	b.path(reg, entries, f, params)
	if f == nil {
		why := "handler not read"
		if reg.handler.Kind == "closure_expression" {
			why = "inline closure handler not read"
		}
		for _, sec := range []*routeshape.Section{&b.sh.Query.Section, &b.sh.Header.Section, &b.sh.Cookie.Section, &b.sh.Body.Section, &b.sh.Responses.Section} {
			sec.Reason = why
		}
		v, err := routeshape.ToAttribute(b.sh)
		return v, b.sh, err
	}
	b.request(reg, f, params)
	if b.headersTaken != "" {
		// The handler may never run: none of its responses is claimed.
		b.reason(routeshape.SectionResponses, b.headersTaken)
	} else {
		b.responses(f)
	}
	b.finish(f, params)
	b.security(f, params, attrs)
	v, err := routeshape.ToAttribute(b.sh)
	return v, b.sh, err
}

// path builds the path section from the route's templates (SH-X1) and
// types it from a Path extractor (SH-X2, SH-X3).
func (b *shapeBuild) path(reg axumRegistration, entries []pathEntry, f *axumFn, params []handlerParam) {
	sec := &b.sh.Path
	if len(entries) == 0 {
		sec.Reason = "the route is not mounted"
		return
	}
	names := templateParams(entries[0].Template)
	for _, e := range entries[1:] {
		if strings.Join(templateParams(e.Template), "/") != strings.Join(names, "/") {
			sec.State, sec.Reason = routeshape.Partial, "the route is mounted under templates with different parameters"
			return
		}
	}
	ev := fact(axumRowTemplate, reg.file, reg.verb.Span.StartLine)
	if len(names) == 0 {
		sec.State, sec.Evidence = routeshape.ProvablyNone, &ev
		for _, p := range params {
			if p.role == rolePath {
				// Path<T> with no parameter always fails: claim nothing.
				sec.State, sec.Evidence, sec.Reason = routeshape.Partial, nil, "Path extractor on a template without parameters"
			}
		}
		return
	}
	seenName := map[string]bool{}
	for _, n := range names {
		if seenName[n] {
			sec.State, sec.Reason = routeshape.Partial, "template repeats parameter "+n
			return
		}
		seenName[n] = true
	}
	schemas := map[string]*routeshape.Schema{}
	rules := map[string]string{}
	lines := map[string]int{}
	var notes []string
	pathParams := 0
	for _, p := range params {
		if p.role != rolePath {
			continue
		}
		pathParams++
		if pathParams > 1 {
			// Each Path extractor reads the same parameters; the first one
			// types them.
			continue
		}
		for name, sch := range b.pathTypes(f, p, names, &notes) {
			sc := sch
			schemas[name] = &sc
			rules[name] = axumRowPathTyped
			if p.inner != nil && p.inner.kind == "local" {
				rules[name] = axumRowPathStruct
			}
			lines[name] = p.p.Line
		}
	}
	for _, n := range names {
		pr := ev
		if r, ok := rules[n]; ok {
			pr = fact(r, f.file, lines[n])
		}
		sec.Items = append(sec.Items, routeshape.Param{Name: n, Access: routeshape.Declared, Presence: routeshape.Required, Schema: schemas[n], Provenance: pr})
	}
	sec.State, sec.Evidence = routeshape.Complete, &ev
}

// pathTypes types template parameters from one Path<T>.
func (b *shapeBuild) pathTypes(f *axumFn, p handlerParam, names []string, notes *[]string) map[string]routeshape.Schema {
	out := map[string]routeshape.Schema{}
	t := p.inner
	if t == nil {
		return out
	}
	c := &schemaCtx{view: viewPath, visit: map[string]bool{}}
	scalar := func(t *tref) (routeshape.Schema, bool) {
		sch := b.s.schema(t, c)
		switch sch.Type {
		case routeshape.TypeString, routeshape.TypeInteger, routeshape.TypeNumber, routeshape.TypeBoolean, routeshape.TypeEnum:
			return sch, !sch.Nullable
		}
		return sch, false
	}
	switch {
	case t.kind == "tuple":
		if len(t.args) != len(names) {
			return out
		}
		for i, a := range t.args {
			if sch, ok := scalar(a); ok {
				out[names[i]] = sch
			}
		}
	case t.std("HashMap BTreeMap") || t.std("Vec"):
		return out
	case t.kind == "local" && t.decl.decl.Kind == "struct":
		sch := b.s.schema(t, c)
		if sch.Type != routeshape.TypeObject {
			return out
		}
		for _, fld := range sch.Fields {
			switch fld.Schema.Type {
			case routeshape.TypeString, routeshape.TypeInteger, routeshape.TypeNumber, routeshape.TypeBoolean, routeshape.TypeEnum:
				if !fld.Schema.Nullable {
					out[fld.Name] = fld.Schema
				}
			}
		}
	default:
		if len(names) != 1 {
			return out
		}
		if sch, ok := scalar(t); ok {
			out[names[0]] = sch
		}
	}
	for n := range out {
		found := false
		for _, m := range names {
			found = found || m == n
		}
		if !found {
			delete(out, n)
		}
	}
	return out
}

// formMode is where Form<T> reads its data on a route's method (row
// SH-X7): the query on GET only (axum's RawForm tests for GET in every
// version from 0.3 to 0.8.8, although the 0.6.7+ documentation names HEAD
// too); the body on any other single method, HEAD included; either on a
// route answering several methods (`any`).
func formMode(method string) string {
	switch method {
	case "GET":
		return "query"
	case "ANY":
		return "either"
	}
	return "body"
}

// request builds the query, header, cookie and body sections from the
// handler's extractors (SH-X4..SH-X15) and follows the request values
// into helpers and extractor impls (SH-X28).
func (b *shapeBuild) request(reg axumRegistration, f *axumFn, params []handlerParam) {
	sh := b.sh
	version := b.s.versions.of(f.file)
	form := formMode(reg.method)
	bodies := 0
	for _, p := range params {
		switch p.role {
		case roleJSON, roleText, roleBytes:
			bodies++
		case roleForm:
			if form != "query" {
				bodies++
			}
		case roleRequest:
			bodies++
		}
	}
	// Before axum 0.5 the HeaderMap extractor takes the headers: a later
	// extractor that needs them always fails (rows SH-X7, SH-X11, SH-X25).
	taken := ""
	for _, p := range params {
		line := p.p.Line
		if taken != "" {
			switch {
			case p.role == roleJSON || p.role == roleForm && form != "query":
				b.headersTaken = taken
				b.reason(routeshape.SectionBody, taken)
				continue
			case p.role == roleTyped:
				b.headersTaken = taken
				b.reason(routeshape.SectionHeader, taken)
				continue
			}
		}
		switch p.role {
		case roleHeaders:
			if !version.atLeast(axum05) && taken == "" {
				taken = fmt.Sprintf("HeaderMap at %s:%d takes the headers before a later extractor in axum before 0.5, and the axum version is not proven to be later (%s)", f.file, line, version.why)
			}
		case roleQuery:
			b.queryParams(f, p, axumRowQuery)
		case roleForm:
			switch form {
			case "query":
				b.queryParams(f, p, axumRowForm)
			case "body":
				b.bodyItem(f, p, "application/x-www-form-urlencoded", viewForm, axumRowForm, bodies)
			default:
				why := fmt.Sprintf("Form on %s reads the query on GET and the body on other methods", reg.method)
				b.reason(routeshape.SectionQuery, why)
				b.reason(routeshape.SectionBody, why)
			}
		case roleJSON:
			b.bodyItem(f, p, "application/json", viewDe, axumRowJSON, bodies)
		case roleText:
			b.rawBody(p, routeshape.Schema{Type: routeshape.TypeString}, bodies, f.file)
		case roleBytes:
			b.rawBody(p, unknownSchema("raw bytes"), bodies, f.file)
		case roleTyped:
			name, ok := typedHeaderName(p.inner)
			if !ok {
				b.reason(routeshape.SectionHeader, "typed header "+p.p.Type.Text+" not read")
				continue
			}
			presence := routeshape.Required
			if p.opt {
				presence = routeshape.Optional
			}
			b.addParam(&sh.Header, routeshape.Param{Name: name, Access: routeshape.Declared, Presence: presence, Schema: &routeshape.Schema{Type: routeshape.TypeString}, Provenance: fact(axumRowTypedHeader, f.file, line)})
		case roleURI:
			b.reason(routeshape.SectionQuery, "the handler reads the URI ("+p.p.Type.Text+")")
		case roleRequest:
			for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionCookie, routeshape.SectionBody} {
				b.reason(sec, "the handler takes the raw request ("+p.p.Type.Text+")")
			}
		case roleWrapped, roleUnknown:
			for _, sec := range []string{routeshape.SectionQuery, routeshape.SectionHeader, routeshape.SectionCookie, routeshape.SectionBody} {
				b.reason(sec, p.why)
			}
		case roleCustom:
			if !p.parts {
				b.reason(routeshape.SectionBody, "extractor "+p.p.Type.Text+" implements FromRequest and may read the body")
			}
		}
	}
	if bodies > 1 {
		sh.Body.Items = nil
		b.reason(routeshape.SectionBody, "several extractors read the body")
	}
	b.followRequest(f, params)
}

func (b *shapeBuild) addParam(sec *routeshape.ParamSection, p routeshape.Param) {
	for _, q := range sec.Items {
		if strings.EqualFold(q.Name, p.Name) && q.Provenance.Kind == p.Provenance.Kind {
			return
		}
	}
	sec.Items = append(sec.Items, p)
}

// queryParams reads Query<T> or Form<T> on GET (SH-X4, SH-X7) and
// Query<HashMap<..>> (SH-X5).
func (b *shapeBuild) queryParams(f *axumFn, p handlerParam, rule string) {
	sec := &b.sh.Query
	t := p.inner
	if t != nil && (t.std("HashMap BTreeMap") || t.std("Vec")) {
		b.reason(routeshape.SectionQuery, "the handler accepts any query parameter ("+p.p.Type.Text+")")
		if p.p.Inner != "" {
			for _, u := range f.fn.Uses {
				if u.Value != p.p.Inner || u.Via != "" || u.Callee != nil {
					continue
				}
				if (u.Method == "get" || u.Method == "contains_key") && len(u.Args) == 1 && u.Args[0].Kind == "literal" {
					b.addParam(sec, routeshape.Param{Name: u.Args[0].Name, Access: routeshape.ReadByHandler, Presence: routeshape.PresenceUnknown, Provenance: axumProv(routeshape.ReadInHandler, axumRowQueryRead, f.file, u.Line, nil)})
				}
			}
		}
		return
	}
	if t == nil || t.kind != "local" || t.decl.decl.Kind != "struct" {
		b.reason(routeshape.SectionQuery, "query type "+p.p.Type.Text+" not read")
		return
	}
	c := &schemaCtx{view: viewForm, visit: map[string]bool{}}
	sch := b.s.schema(t, c)
	if sch.Type != routeshape.TypeObject {
		b.reason(routeshape.SectionQuery, "query type "+p.p.Type.Text+": "+sch.Reason)
		return
	}
	if c.partial || sch.Additional == routeshape.AdditionalUnknown {
		b.reason(routeshape.SectionQuery, "query type "+p.p.Type.Text+" has flattened fields")
	}
	for _, fld := range sch.Fields {
		fs := fld.Schema
		b.addParam(sec, routeshape.Param{Name: fld.Name, Access: routeshape.Declared, Presence: fld.Presence, Schema: &fs, Provenance: fact(rule, f.file, p.p.Line)})
	}
}

// bodyItem adds the Json<T> or Form<T> body (SH-X6, SH-X7).
func (b *shapeBuild) bodyItem(f *axumFn, p handlerParam, contentType string, v view, rule string, bodies int) {
	if p.inner == nil {
		b.reason(routeshape.SectionBody, "body type not read")
		return
	}
	c := &schemaCtx{view: v, visit: map[string]bool{}}
	sch := b.s.schema(p.inner, c)
	if v == viewForm && sch.Type != routeshape.TypeObject {
		sch = unknownSchema("form type " + p.p.Type.Text + " is not a struct or map")
	}
	if c.partial {
		b.reason(routeshape.SectionBody, "the body type has flattened fields")
	}
	b.sh.Body.Items = append(b.sh.Body.Items, routeshape.Content{ContentType: strp(contentType), Schema: &sch, Provenance: fact(rule, f.file, p.p.Line)})
}

// rawBody adds a String or Bytes body (SH-X8): any content type.
func (b *shapeBuild) rawBody(p handlerParam, sch routeshape.Schema, bodies int, file string) {
	b.sh.Body.Items = append(b.sh.Body.Items, routeshape.Content{Schema: &sch, Provenance: fact(axumRowRawBody, file, p.p.Line)})
}

// typedHeaderNames are the headers crate's typed headers the reader
// knows, by type name (SH-X10).
var typedHeaderNames = map[string]string{
	"Authorization": "authorization", "UserAgent": "user-agent", "ContentType": "content-type", "ContentLength": "content-length",
	"Host": "host", "Origin": "origin", "Referer": "referer", "Cookie": "cookie", "IfNoneMatch": "if-none-match", "IfMatch": "if-match",
	"IfModifiedSince": "if-modified-since", "IfUnmodifiedSince": "if-unmodified-since", "Range": "range", "Connection": "connection",
}

func typedHeaderName(t *tref) (string, bool) {
	if t == nil || t.kind != "ext" || !facts.Builtin(t.crate, "headers axum axum_extra headers_core") {
		return "", false
	}
	n, ok := typedHeaderNames[t.name]
	return n, ok
}

// headerName reads a header name argument: a string literal, an http
// header constant, or a string constant of the repository (SH-X11).
func (s *axumShaper) headerName(file string, scope int, v facts.RustValue) (string, bool) {
	switch v.Kind {
	case "literal":
		return v.Name, v.Name != ""
	case "path":
		b := s.modules.Resolve(file, scope, v.Name)
		if b.Ambiguous {
			return "", false
		}
		if b.External != "" && facts.Builtin(b.External, "axum http reqwest hyper") {
			parts := strings.Split(b.Name, "::")
			if len(parts) >= 2 && parts[len(parts)-2] == "header" {
				c := parts[len(parts)-1]
				if c == strings.ToUpper(c) {
					return strings.ToLower(strings.ReplaceAll(c, "_", "-")), true
				}
			}
			return "", false
		}
		if value, _, ok := s.modules.Prefix(file, facts.RustExpr{Kind: "path", Name: v.Name, Scope: scope}); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

// followRequest follows the request values (HeaderMap, Request, Parts,
// custom extractors) into the functions they are passed to (SH-X11,
// SH-X13, SH-X28) and records the headers read. A chain of a request or
// parts value that stops anywhere but at its end (depth cap, a callee not
// read, a parameter of another kind) leaves its reads unknown: the query
// and cookies (and the body, for a request that holds it) are partial.
func (b *shapeBuild) followRequest(f *axumFn, params []handlerParam) {
	r := &requestResolver{b: b, handler: f, params: params, extractors: map[string]bool{}, sites: map[string]string{}}
	start := routeshape.Frame{Function: b.s.step(f.fn.Identity), Value: "*"}
	any := false
	for _, p := range params {
		switch p.role {
		case roleHeaders, roleRequest, roleCustom:
			any = true
		}
	}
	if !any {
		return
	}
	res, err := routeshape.Follow(context.Background(), start, b.s.depth, r)
	if err != nil {
		return
	}
	for _, c := range res.Chains {
		if c.Stop == routeshape.StopEnded {
			continue
		}
		why := chainReason(c)
		b.reason(routeshape.SectionHeader, why)
		if c.Stop == routeshape.StopJoined || c.Stop == routeshape.StopCycle {
			// The function was read for the same value on this path.
			continue
		}
		kind := ""
		if c.At != nil {
			kind = r.sites[axumStepKey(*c.At)]
		}
		if kind != valueHeaders {
			b.reason(routeshape.SectionQuery, why)
			b.reason(routeshape.SectionCookie, why)
			if kind != valueParts {
				b.reason(routeshape.SectionBody, why)
			}
		}
	}
	b.sh.Follow.Chains = append(b.sh.Follow.Chains, routeshape.Records(routeshape.SectionHeader, res)...)
}

func chainReason(c routeshape.Chain) string {
	at := ""
	if c.At != nil {
		at = fmt.Sprintf(" at %s:%d (%s)", c.At.File, c.At.Line, c.At.Function)
	}
	if c.Stop == routeshape.StopDepthCap {
		return c.Detail + at
	}
	return "call not followed (" + string(c.Stop) + ")" + at
}

// Kinds of followed request values.
const (
	valueHeaders = "headers" // a HeaderMap
	valueParts   = "parts"   // http::request::Parts: no body
	valueRequest = "request" // a request that holds the body
)

type requestResolver struct {
	b          *shapeBuild
	handler    *axumFn
	params     []handlerParam
	extractors map[string]bool
	// sites maps each call site a value leaves through (by its site and
	// by its target, as chain ends name them) to the kind of value.
	sites map[string]string
}

func (r *requestResolver) site(c routeshape.Call, kind string) routeshape.Call {
	r.sites[axumStepKey(c.Site)] = kind
	if c.Stop == "" {
		r.sites[axumStepKey(routeshape.Step{File: c.Site.File, Line: c.Site.Line, Function: c.Target.Function})] = kind
	}
	return c
}

// valueParam classifies a function's parameter as a followed value.
func (r *requestResolver) valueParam(f *axumFn, name string) (handlerParam, bool) {
	for _, p := range f.fn.Params {
		if p.Name == name && !p.Self {
			h := r.b.s.classify(f.file, f.fn.Scope, p)
			return h, h.role == roleHeaders || h.role == roleRequest
		}
	}
	return handlerParam{role: roleUnknown}, false
}

// valueKind is the kind of a request value parameter.
func valueKind(h handlerParam) string {
	switch {
	case h.role == roleHeaders:
		return valueHeaders
	case h.t != nil && h.t.name == "Parts":
		return valueParts
	}
	return valueRequest
}

func (r *requestResolver) Visit(ctx context.Context, frame routeshape.Frame) ([]routeshape.Call, error) {
	id, ok := r.b.s.steps[axumStepKey(frame.Function)]
	if !ok {
		return nil, fmt.Errorf("function %s not read", frame.Function.Function)
	}
	f := r.b.s.fns[id]
	dependency := len(frame.Chain) > 0 && r.extractors[axumStepKey(frame.Chain[0])]
	var calls []routeshape.Call
	if frame.Value == "*" {
		for _, p := range r.params {
			switch p.role {
			case roleHeaders, roleRequest:
				calls = append(calls, r.read(f, p.p.Name, p, frame, dependency)...)
			case roleCustom:
				target := r.b.s.step(p.impl)
				r.extractors[axumStepKey(routeshape.Step{File: f.file, Line: p.p.Line, Function: target.Function})] = true
				ext := r.b.s.fns[p.impl]
				value, kind := "", valueRequest
				for _, ep := range ext.fn.Params {
					if h, ok := r.valueParam(ext, ep.Name); ok && h.role == roleRequest {
						value, kind = ep.Name, valueKind(h)
					}
				}
				if p.parts {
					kind = valueParts
				}
				site := routeshape.Step{File: f.file, Line: p.p.Line, Function: p.p.Type.Text}
				if value == "" {
					calls = append(calls, r.site(routeshape.Call{Site: site, Stop: routeshape.StopOpaqueType, Detail: "extractor " + p.p.Type.Text + " takes no request parts parameter"}, kind))
					continue
				}
				calls = append(calls, r.site(routeshape.Call{Site: site, Target: target, Value: value}, kind))
			}
		}
		return calls, nil
	}
	h, ok := r.valueParam(f, frame.Value)
	if !ok {
		return nil, fmt.Errorf("parameter %s of %s is not a request value", frame.Value, frame.Function.Function)
	}
	return r.read(f, frame.Value, h, frame, dependency), nil
}

// whole records a use of a request or parts value that may read any part
// of it (row SH-X14): every section it holds is partial.
func (r *requestResolver) whole(kind, why string) {
	r.b.reason(routeshape.SectionHeader, why)
	if kind == valueHeaders {
		return
	}
	r.b.reason(routeshape.SectionQuery, why)
	r.b.reason(routeshape.SectionCookie, why)
	if kind != valueParts {
		r.b.reason(routeshape.SectionBody, why)
	}
}

// read records the header reads of one value in a function and returns
// the calls it is passed to. Only a header read (`get`, `get_all`,
// `contains_key` on the value's headers) is claimed; any other use of a
// request or parts value may read the query, cookies or body.
func (r *requestResolver) read(f *axumFn, value string, h handlerParam, frame routeshape.Frame, dependency bool) []routeshape.Call {
	b := r.b
	kind := valueKind(h)
	var calls []routeshape.Call
	for _, u := range f.fn.Uses {
		if u.Value != value {
			continue
		}
		headers := h.role == roleHeaders && u.Via == "" || h.role == roleRequest && u.Via != ""
		if u.Callee != nil {
			passed := kind
			if headers {
				passed = valueHeaders
			}
			calls = append(calls, r.site(r.call(f, u, headers, h.role), passed))
			continue
		}
		if name, ok := strings.CutPrefix(u.Method, "macro:"); ok && b.s.formattingMacro(f.file, u.Scope, name) {
			// Only formatted (Debug or Display): no part is read.
			continue
		}
		if u.Method == "=" {
			r.whole(kind, fmt.Sprintf("request value %s rebound at %s:%d", value, f.file, u.Line))
			continue
		}
		if !headers {
			r.whole(kind, fmt.Sprintf("the request value %s is used by %s at %s:%d", value, u.Method, f.file, u.Line))
			continue
		}
		switch u.Method {
		case "get", "get_all", "contains_key":
			if len(u.Args) != 1 {
				b.reason(routeshape.SectionHeader, fmt.Sprintf("headers read by %s at %s:%d", u.Method, f.file, u.Line))
				continue
			}
			name, ok := b.s.headerName(f.file, u.Scope, u.Args[0])
			if !ok {
				b.reason(routeshape.SectionHeader, fmt.Sprintf("header name not read at %s:%d", f.file, u.Line))
				continue
			}
			access, rule := routeshape.ReadByHandler, axumRowHeaderRead
			if dependency {
				access, rule = routeshape.ReadByDependency, axumRowCustom
			}
			b.addParam(&b.sh.Header, routeshape.Param{Name: name, Access: access, Presence: routeshape.PresenceUnknown, Provenance: axumProv(routeshape.ReadInHandler, rule, f.file, u.Line, frame.Chain)})
		default:
			b.reason(routeshape.SectionHeader, fmt.Sprintf("headers read by %s at %s:%d", u.Method, f.file, u.Line))
		}
	}
	return calls
}

// call resolves a call a request value is passed to (SH-X28). A stop is
// read by followRequest from the chain end.
func (r *requestResolver) call(f *axumFn, u facts.RustUse, headers bool, role paramRole) routeshape.Call {
	site := routeshape.Step{File: f.file, Line: u.Line, Function: calleeText(*u.Callee)}
	id, stop, detail := r.b.s.target(f, *u.Callee)
	if stop != "" {
		return routeshape.Call{Site: site, Stop: stop, Detail: detail}
	}
	callee := r.b.s.fns[id]
	args := 0
	for _, p := range callee.fn.Params {
		if p.Self {
			continue
		}
		if args == u.Arg {
			want := role
			if headers {
				want = roleHeaders
			}
			got, ok := r.valueParam(callee, p.Name)
			if !ok || got.role != want || p.Name == "" {
				return routeshape.Call{Site: site, Stop: routeshape.StopOpaqueType, Detail: "parameter " + p.Pattern + " of " + site.Function + " is not the same request value"}
			}
			return routeshape.Call{Site: site, Target: r.b.s.step(id), Value: p.Name}
		}
		args++
	}
	return routeshape.Call{Site: site, Stop: routeshape.StopUnresolved, Detail: "argument has no parameter"}
}

func calleeText(v facts.RustValue) string {
	if v.Kind == "method" {
		recv := "?"
		if v.Recv != nil && v.Recv.Kind == "path" {
			recv = v.Recv.Name
		}
		return recv + "." + v.Name
	}
	return v.Name
}

// target proves the callee of a call: a function path, `Self::f` /
// `Type::f`, or `self.f(..)` resolved to a unique repository function or
// method whose shape was read (SH-X28).
func (s *axumShaper) target(f *axumFn, callee facts.RustValue) (string, routeshape.Stop, string) {
	path := ""
	switch callee.Kind {
	case "call":
		path = callee.Name
		if !strings.Contains(path, "::") && f.bound(path) {
			// A local closure or function value shadows any item of
			// that name (row SH-X28).
			return "", routeshape.StopUnresolved, path + " is a local binding"
		}
	case "method":
		if callee.Recv == nil || callee.Recv.Kind != "path" || callee.Recv.Name != "self" || f.fn.SelfType == "" {
			return "", routeshape.StopUnresolved, "method " + callee.Name + " of a value whose type is not read"
		}
		path = "Self::" + callee.Name
	default:
		return "", routeshape.StopUnresolved, "callee not read"
	}
	if strings.HasPrefix(path, "Self::") {
		if f.fn.SelfType == "" {
			return "", routeshape.StopUnresolved, "Self outside an impl"
		}
		self := f.fn.SelfType
		if i := strings.Index(self, "<"); i >= 0 {
			self = self[:i]
		}
		path = self + "::" + strings.TrimPrefix(path, "Self::")
	}
	b := s.modules.Resolve(f.file, f.fn.Scope, path)
	switch {
	case b.Ambiguous:
		return "", routeshape.StopAmbiguous, "several definitions of " + path
	case b.External != "":
		return "", routeshape.StopExternal, path + " is in crate " + b.External
	case b.Identity == "":
		return "", routeshape.StopUnresolved, "no definition of " + path
	}
	if s.fns[b.Identity] == nil {
		return "", routeshape.StopUnresolved, "the shape syntax of " + path + " was not read"
	}
	return b.Identity, "", ""
}

// finish sets the section states (SH-X29).
func (b *shapeBuild) finish(f *axumFn, params []handlerParam) {
	sh := b.sh
	sig := fact(axumRowStates, f.file, f.fn.Line)
	// Query: complete from a Query<T> struct when nothing else may read
	// it; provably none when no extractor can.
	setParams := func(sec *routeshape.ParamSection, name string, canNone bool) {
		reasons := b.reasons[name]
		allFact := len(sec.Items) > 0
		for _, it := range sec.Items {
			allFact = allFact && it.Provenance.Kind == routeshape.Fact
		}
		switch {
		case len(reasons) == 0 && allFact && canNone:
			ev := sig
			sec.State, sec.Evidence = routeshape.Complete, &ev
		case len(reasons) == 0 && len(sec.Items) == 0 && canNone:
			ev := sig
			sec.State, sec.Evidence = routeshape.ProvablyNone, &ev
		case len(sec.Items) > 0 || len(reasons) > 0:
			if len(reasons) == 0 {
				reasons = []string{"middleware (layers) is not read"}
			}
			sec.State, sec.Reason = routeshape.Partial, strings.Join(reasons, "; ")
		default:
			sec.State = routeshape.NotRead
		}
	}
	setParams(&sh.Query, routeshape.SectionQuery, true)
	// Headers are read by middleware (layers), which is not read: never
	// complete or provably none.
	setParams(&sh.Header, routeshape.SectionHeader, false)
	if r := b.reasons[routeshape.SectionCookie]; len(r) > 0 {
		sh.Cookie.State, sh.Cookie.Reason = routeshape.NotRead, strings.Join(r, "; ")
	}
	body := b.reasons[routeshape.SectionBody]
	switch {
	case len(body) == 0 && len(sh.Body.Items) == 1:
		ev := sig
		sh.Body.State, sh.Body.Evidence = routeshape.Complete, &ev
	case len(body) == 0 && len(sh.Body.Items) == 0:
		ev := sig
		sh.Body.State, sh.Body.Evidence = routeshape.ProvablyNone, &ev
	default:
		if len(body) == 0 {
			body = []string{"body not read"}
		}
		sh.Body.State, sh.Body.Reason = routeshape.Partial, strings.Join(body, "; ")
	}
	resp := b.reasons[routeshape.SectionResponses]
	resp = append(resp, "panics, extractor rejections and middleware (layers) are not read")
	sh.Responses.State, sh.Responses.Reason = routeshape.Partial, strings.Join(resp, "; ")
	sort.SliceStable(sh.Responses.Items, func(i, j int) bool { return sh.Responses.Items[i].Status < sh.Responses.Items[j].Status })
}

// statusCodes are http::StatusCode's associated constants.
var statusCodes = map[string]int{
	"CONTINUE": 100, "SWITCHING_PROTOCOLS": 101, "PROCESSING": 102, "OK": 200, "CREATED": 201, "ACCEPTED": 202,
	"NON_AUTHORITATIVE_INFORMATION": 203, "NO_CONTENT": 204, "RESET_CONTENT": 205, "PARTIAL_CONTENT": 206, "MULTI_STATUS": 207,
	"ALREADY_REPORTED": 208, "IM_USED": 226, "MULTIPLE_CHOICES": 300, "MOVED_PERMANENTLY": 301, "FOUND": 302, "SEE_OTHER": 303,
	"NOT_MODIFIED": 304, "USE_PROXY": 305, "TEMPORARY_REDIRECT": 307, "PERMANENT_REDIRECT": 308, "BAD_REQUEST": 400,
	"UNAUTHORIZED": 401, "PAYMENT_REQUIRED": 402, "FORBIDDEN": 403, "NOT_FOUND": 404, "METHOD_NOT_ALLOWED": 405,
	"NOT_ACCEPTABLE": 406, "PROXY_AUTHENTICATION_REQUIRED": 407, "REQUEST_TIMEOUT": 408, "CONFLICT": 409, "GONE": 410,
	"LENGTH_REQUIRED": 411, "PRECONDITION_FAILED": 412, "PAYLOAD_TOO_LARGE": 413, "URI_TOO_LONG": 414,
	"UNSUPPORTED_MEDIA_TYPE": 415, "RANGE_NOT_SATISFIABLE": 416, "EXPECTATION_FAILED": 417, "IM_A_TEAPOT": 418,
	"MISDIRECTED_REQUEST": 421, "UNPROCESSABLE_ENTITY": 422, "LOCKED": 423, "FAILED_DEPENDENCY": 424, "TOO_EARLY": 425, "UPGRADE_REQUIRED": 426,
	"PRECONDITION_REQUIRED": 428, "TOO_MANY_REQUESTS": 429, "REQUEST_HEADER_FIELDS_TOO_LARGE": 431,
	"UNAVAILABLE_FOR_LEGAL_REASONS": 451, "INTERNAL_SERVER_ERROR": 500, "NOT_IMPLEMENTED": 501, "BAD_GATEWAY": 502,
	"SERVICE_UNAVAILABLE": 503, "GATEWAY_TIMEOUT": 504, "HTTP_VERSION_NOT_SUPPORTED": 505, "VARIANT_ALSO_NEGOTIATES": 506,
	"INSUFFICIENT_STORAGE": 507, "LOOP_DETECTED": 508, "NOT_EXTENDED": 510, "NETWORK_AUTHENTICATION_REQUIRED": 511,
}

// status reads a StatusCode constant (SH-X23).
func (s *axumShaper) status(file string, scope int, v facts.RustValue) (string, bool) {
	if v.Kind != "path" || v.Try {
		return "", false
	}
	b := s.modules.Resolve(file, scope, v.Name)
	if b.Ambiguous || b.External == "" || !facts.Builtin(b.External, "axum http reqwest hyper") {
		return "", false
	}
	parts := strings.Split(b.Name, "::")
	if len(parts) < 2 || parts[len(parts)-2] != "StatusCode" {
		return "", false
	}
	code, ok := statusCodes[parts[len(parts)-1]]
	if !ok {
		return "", false
	}
	return strconv.Itoa(code), true
}

// emptyBody reports statuses that never carry a body (1xx, 204, 304).
func emptyBody(status string) bool {
	return strings.HasPrefix(status, "1") || status == "204" || status == "304"
}

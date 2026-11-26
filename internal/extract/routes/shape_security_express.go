package routes

import (
	"fmt"
	"strings"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Express security (rows SH-S3..SH-S12). The middleware that runs before
// a route is the route's mounted_middleware (DA-E1, every chain) and its
// own middleware arguments, in that order; each is read for a library of
// the closed list. Library semantics were read in express-jwt 8.5.1,
// express-oauth2-jwt-bearer 1.6.1, express-basic-auth 1.2.1, passport
// 0.7.0, passport-jwt 4.0.1, passport-http-bearer 1.0.1 and passport-http
// 0.3.0, and checked by scripts/oracle/route_security.

// security reads the route's auth middleware into the security section.
func (b *shapeBuilder) security() {
	n := b.info.node
	if b.info.framework != "express" {
		return
	}
	col := &secCollector{complete: true}
	if unknown, ok := n.Attributes["mounted_middleware_unknown"].([]map[string]any); ok && len(unknown) > 0 {
		tagShape("SH-S11/unknown-entry")
		col.complete = false
		for _, m := range unknown {
			reason, _ := m["reason"].(string)
			name, _ := m["expression"].(string)
			col.reason(fmt.Sprintf("middleware %s may run before the route (%s): not read", name, reason))
		}
	}
	read := func(file string, scope int, e facts.RouteExpr, line int, where string) {
		if src, ok := b.authSource(file, scope, e, line); ok {
			col.sources = append(col.sources, src)
			return
		}
		if b.nonAuth(file, scope, e) {
			tagShape("SH-S12/known")
			return
		}
		col.complete = false
		col.reason(fmt.Sprintf("%s middleware %s (%s:%d) is not read for auth", where, whitespace(e.Text), file, line))
	}
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
	if mounted, ok := n.Attributes["mounted_middleware"].([]map[string]any); ok {
		for _, m := range mounted {
			file, _ := m["file"].(string)
			span, _ := m["span"].(contract.Span)
			name, _ := m["name"].(string)
			c, a, found := at(file, span, name)
			if !found {
				col.complete = false
				col.reason(fmt.Sprintf("mounted middleware %s (%s:%d) is not found", name, file, span.StartLine))
				continue
			}
			tagShape("SH-S11/mounted")
			read(file, c.Scope, a, span.StartLine, "mounted")
		}
	}
	if b.info.call != nil {
		for _, a := range b.info.own {
			tagShape("SH-S11/route")
			read(n.File, b.info.call.Scope, a, b.info.call.Span.StartLine, "route")
		}
	}
	if listed, ok := n.Attributes["middleware"].([]string); ok && len(listed) != len(b.info.own) {
		col.complete = false
		col.reason("route middleware chained with route(p).all is not read")
	}
	if len(col.sources) > 0 && col.complete {
		tagShape("SH-S12/complete")
	}
	col.apply(b.shape)
}

// nonAuth reports a middleware of the closed list of libraries that
// never refuse a request for missing credentials (row SH-S12): logging,
// compression, CORS, security headers, body and cookie parsers, the
// celebrate and express-validator validators and passport's initialize
// and session. express-openapi-validator
// may check the spec's security and is not on the list.
func (b *shapeBuilder) nonAuth(file string, scope int, e facts.RouteExpr) bool {
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
	switch v.file {
	case "morgan", "compression", "cors", "helmet", "cookie-parser", "celebrate", "express-validator":
		return true
	case "passport":
		// initialize() and session() restore a session user; they never
		// refuse a request. authenticate('session') is read by
		// passportPassThrough.
		return name == "initialize" || name == "session" || name == "authenticate" && b.passportPassThrough(f, s, call)
	case "express", "body-parser":
		return name == "json" || name == "urlencoded" || name == "text" || name == "raw" || v.file == "body-parser" && name == "default"
	}
	return false
}

// expressAuthLibraries are the factories rows SH-S3..SH-S6 and SH-S10
// read, keyed as the route resolver names an import.
var expressAuthLibraries = map[string]string{
	"express-jwt#expressjwt":                      "express-jwt",
	"express-jwt#default.expressjwt":              "express-jwt",
	"express-jwt#default":                         "express-jwt",
	"express-oauth2-jwt-bearer#auth":              "express-oauth2-jwt-bearer",
	"express-oauth2-jwt-bearer#default.auth":      "express-oauth2-jwt-bearer",
	"express-basic-auth#default":                  "express-basic-auth",
	"passport#default.authenticate":               "passport",
	"passport#authenticate":                       "passport",
	"express-openid-connect#requiresAuth":         "express-openid-connect",
	"express-openid-connect#default.requiresAuth": "express-openid-connect",
}

// authSource reads one middleware expression as an auth library call.
func (b *shapeBuilder) authSource(file string, scope int, e facts.RouteExpr, line int) (secSource, bool) {
	f, s, call := b.l.r.deref(file, scope, e, 0)
	if call.Kind != "call_expression" || call.Object == nil {
		return secSource{}, false
	}
	callee := b.l.r.resolve(f, s, *call.Object, 0, nil)
	lib := expressAuthLibraries[callee.file+"#"+callee.name]
	if callee.kind != "import" || lib == "" {
		return secSource{}, false
	}
	src := secSource{label: lib, kind: routeshape.Fact, file: file, line: line}
	options := func(i int) (facts.RouteExpr, bool) {
		if len(call.Args) <= i {
			return facts.RouteExpr{}, true
		}
		_, _, o := b.l.r.deref(f, s, call.Args[i], 0)
		return o, o.Kind == "object" && !o.Spread
	}
	optional := func(key string) {
		o, ok := options(0)
		v, present := o.Properties[key]
		switch {
		case !ok || present && v.Kind != "true" && v.Kind != "false":
			src.kind = routeshape.Intended
			src.reasons = append(src.reasons, fmt.Sprintf("%s: %s is not read, so requests without credentials may reach the handler", lib, key))
		case present && v.Kind == "false":
			tagShape(src.rule + "/optional")
			src.kind = routeshape.Intended
			src.reasons = append(src.reasons, fmt.Sprintf("%s: %s is false, so requests without credentials reach the handler", lib, key))
		}
	}
	switch lib {
	case "express-jwt":
		src.rule = "SH-S3"
		o, ok := options(0)
		if _, custom := o.Properties["getToken"]; custom || !ok {
			tagShape("SH-S3/getToken")
			src.alts = one(unknownScheme("express-jwt", src.rule, file, line))
			if custom {
				src.reasons = append(src.reasons, "express-jwt: a custom getToken decides where the token is read")
			} else {
				src.reasons = append(src.reasons, "express-jwt: the options are not a literal object (getToken may be set)")
			}
		} else {
			tagShape("SH-S3/default")
			src.alts = one(bearerScheme(src.rule, file, line))
		}
		optional("credentialsRequired")
	case "express-oauth2-jwt-bearer":
		src.rule = "SH-S4"
		tagShape("SH-S4/auth")
		src.alts = [][]routeshape.Scheme{{bearerScheme(src.rule, file, line)}, {apiKeyScheme("query", strPtr("access_token"), src.rule, file, line)}}
		src.reasons = append(src.reasons, "express-oauth2-jwt-bearer also accepts an access_token field of a urlencoded body")
		if len(call.Args) > 0 {
			optional("authRequired")
		}
	case "express-basic-auth":
		src.rule = "SH-S5"
		tagShape("SH-S5/default")
		src.alts = one(basicScheme(src.rule, file, line))
	case "express-openid-connect":
		src.rule = "SH-S10"
		src.alts = one(unknownScheme("express-openid-connect", src.rule, file, line))
		src.reasons = append(src.reasons, "express-openid-connect: a session cookie whose name the auth() configuration sets")
		if len(call.Args) > 0 {
			// requiresAuth(requiresLoginCheck): the check decides whether
			// a login is required, so requests may pass without one.
			tagShape("SH-S10/requiresLoginCheck")
			src.kind = routeshape.Intended
			src.reasons = append(src.reasons, "express-openid-connect: requiresAuth has a requiresLoginCheck argument, which decides whether a login is required")
		} else {
			tagShape("SH-S10/requiresAuth")
		}
	case "passport":
		if b.passportPassThrough(f, s, call) {
			return secSource{}, false
		}
		src.rule = "SH-S6"
		b.passport(&src, f, s, call)
	}
	return src, true
}

// passportPassThrough reports passport.authenticate('session'): no
// passport.use registration names session (and none has an unread
// name), so it is passport's built-in SessionStrategy, which restores a
// login session when there is one and otherwise passes the request on
// (passport 0.7.0 lib/strategies/session.js): it never refuses for
// missing credentials (row SH-S6, form session).
func (b *shapeBuilder) passportPassThrough(f string, s int, call facts.RouteExpr) bool {
	if call.Kind != "call_expression" || call.Object == nil || len(call.Args) == 0 {
		return false
	}
	callee := b.l.r.resolve(f, s, *call.Object, 0, nil)
	if callee.kind != "import" || expressAuthLibraries[callee.file+"#"+callee.name] != "passport" {
		return false
	}
	_, _, st := b.l.r.deref(f, s, call.Args[0], 0)
	if st.Kind != "literal" || st.Name != "session" {
		return false
	}
	for _, r := range b.l.passportStrategies().list {
		if r.name == "" || r.name == "session" {
			return false
		}
	}
	tagShape("SH-S6/session")
	return true
}

// passport reads passport.authenticate(strategy, options?, callback?)
// (rows SH-S6..SH-S9): the strategies named, looked up among the
// passport.use registrations of the repository.
func (b *shapeBuilder) passport(src *secSource, f string, s int, call facts.RouteExpr) {
	name := ""
	if len(call.Args) > 0 {
		// A list of strategies (an array) tries each in turn and passes
		// when any one does; the route syntax does not keep its elements,
		// so it is a name not read below.
		if _, _, st := b.l.r.deref(f, s, call.Args[0], 0); st.Kind == "literal" {
			name = st.Name
		}
	}
	for _, a := range call.Args[min(1, len(call.Args)):] {
		_, _, d := b.l.r.deref(f, s, a, 0)
		if d.Kind != "object" {
			// A custom callback decides what a failed authentication does.
			tagShape("SH-S6/callback")
			src.kind = routeshape.Intended
			src.reasons = append(src.reasons, "passport.authenticate has a custom callback, which decides what happens without credentials")
			break
		}
	}
	// A strategy whose behaviour is not read may let every request through
	// (passport-anonymous, a custom strategy): intended.
	unread := func(label, why string) {
		tagShape("SH-S6/unregistered")
		src.kind = routeshape.Intended
		src.alts = one(unknownScheme(label, src.rule, src.file, src.line))
		src.reasons = append(src.reasons, why+", so it may let requests through")
	}
	if name == "" {
		unread("passport", "passport: the strategy name is not a literal")
		return
	}
	reg, ok := b.l.passportStrategies().lookup(name)
	if !ok {
		unread("passport-"+name, fmt.Sprintf("passport strategy %q: its registration is not read", name))
		return
	}
	tagShape("SH-S6/strategy")
	alts, reasons := b.strategyForms(reg, name, src)
	src.alts = append(src.alts, alts...)
	src.reasons = append(src.reasons, reasons...)
}

// passportReg is one passport.use(name?, new Strategy(...)) registration.
type passportReg struct {
	file    string
	scope   int
	line    int
	class   string // the strategy's package#export, "" when not resolved
	name    string // "" when not read
	options []facts.RouteExpr
}

type passportRegs struct{ list []passportReg }

// lookup returns the only registration that may define name: exactly
// one registration names it, and no registration's name is unread.
func (p passportRegs) lookup(name string) (passportReg, bool) {
	var found []passportReg
	for _, r := range p.list {
		if r.name == "" {
			return passportReg{}, false
		}
		if r.name == name {
			found = append(found, r)
		}
	}
	if len(found) != 1 || found[0].class == "" {
		return passportReg{}, false
	}
	return found[0], true
}

// passportDefaultNames are the strategy names the classes set
// (this.name in each package's strategy constructor).
var passportDefaultNames = map[string]string{
	"passport-jwt#Strategy":                 "jwt",
	"passport-jwt#default.Strategy":         "jwt",
	"passport-http-bearer#Strategy":         "bearer",
	"passport-http-bearer#default.Strategy": "bearer",
	"passport-http-bearer#default":          "bearer",
	"passport-http#BasicStrategy":           "basic",
	"passport-http#default.BasicStrategy":   "basic",
}

// passportStrategies collects every passport.use registration on the
// passport default instance, once per link.
func (l *shapeLinker) passportStrategies() passportRegs {
	if l.passports != nil {
		return *l.passports
	}
	regs := passportRegs{}
	for _, file := range facts.Paths(l.r.files) {
		data := l.r.files[file].Routes
		if data == nil {
			continue
		}
		for _, c := range data.Calls {
			if c.Method != "use" || len(c.Args) == 0 {
				continue
			}
			recv := l.r.resolve(file, c.Scope, c.Receiver, 0, nil)
			if recv.kind != "import" || recv.file != "passport" || recv.name != "default" {
				continue
			}
			reg := passportReg{file: file, scope: c.Scope, line: c.Span.StartLine}
			strategy := c.Args[len(c.Args)-1]
			if len(c.Args) == 2 {
				if c.Args[0].Kind != "literal" {
					regs.list = append(regs.list, reg)
					continue
				}
				reg.name = c.Args[0].Name
			}
			_, _, ctor := l.r.deref(file, c.Scope, strategy, 0)
			if ctor.Kind == "new_expression" && ctor.Object != nil {
				v := l.r.resolve(file, c.Scope, *ctor.Object, 0, nil)
				if v.kind == "import" {
					key := v.file + "#" + v.name
					if def, known := passportDefaultNames[key]; known {
						reg.class = key
						reg.options = ctor.Args
						if reg.name == "" {
							reg.name = def
						}
					}
				}
			}
			regs.list = append(regs.list, reg)
		}
	}
	l.passports = &regs
	return regs
}

// strategyForms are the credential forms of one registered strategy
// (rows SH-S7..SH-S9).
func (b *shapeBuilder) strategyForms(reg passportReg, name string, src *secSource) ([][]routeshape.Scheme, []string) {
	pkg, _, _ := strings.Cut(reg.class, "#")
	file, line := src.file, src.line
	switch pkg {
	case "passport-http-bearer":
		tagShape("SH-S8/Strategy")
		return [][]routeshape.Scheme{{bearerScheme("SH-S8", file, line)}, {apiKeyScheme("query", strPtr("access_token"), "SH-S8", file, line)}},
			[]string{"passport-http-bearer also accepts an access_token field of the request body"}
	case "passport-http":
		tagShape("SH-S9/BasicStrategy")
		return one(basicScheme("SH-S9", file, line)), nil
	case "passport-jwt":
		unknown := func(why string) ([][]routeshape.Scheme, []string) {
			tagShape("SH-S7/other")
			return one(unknownScheme("passport-"+name, "SH-S7", file, line)), []string{fmt.Sprintf("passport strategy %q: %s", name, why)}
		}
		if len(reg.options) == 0 {
			return unknown("options not read")
		}
		_, _, o := b.l.r.deref(reg.file, reg.scope, reg.options[0], 0)
		if o.Kind != "object" || o.Spread {
			return unknown("options are not a literal object")
		}
		from, ok := o.Properties["jwtFromRequest"]
		if !ok {
			return unknown("jwtFromRequest is not read")
		}
		alts, why := b.jwtExtractor(reg.file, reg.scope, from, file, line, 0)
		if why != "" {
			return unknown(why)
		}
		return alts, nil
	}
	tagShape("SH-S7/other")
	return one(unknownScheme("passport-"+name, "SH-S6", file, line)), []string{fmt.Sprintf("passport strategy %q is not read", name)}
}

// jwtExtractor reads a passport-jwt ExtractJwt call (row SH-S7).
func (b *shapeBuilder) jwtExtractor(f string, s int, e facts.RouteExpr, file string, line, depth int) ([][]routeshape.Scheme, string) {
	_, _, call := b.l.r.deref(f, s, e, 0)
	if depth > 2 || call.Kind != "call_expression" || call.Object == nil {
		return nil, "jwtFromRequest is not an ExtractJwt call"
	}
	v := b.l.r.resolve(f, s, *call.Object, 0, nil)
	if v.kind != "import" || v.file != "passport-jwt" {
		return nil, "jwtFromRequest is not an ExtractJwt call"
	}
	method := strings.TrimPrefix(strings.TrimPrefix(v.name, "default."), "ExtractJwt.")
	literal := func() (string, bool) {
		if len(call.Args) != 1 {
			return "", false
		}
		_, _, a := b.l.r.deref(f, s, call.Args[0], 0)
		return a.Name, a.Kind == "literal" && a.Name != ""
	}
	switch method {
	case "fromAuthHeaderAsBearerToken":
		tagShape("SH-S7/fromAuthHeaderAsBearerToken")
		return one(bearerScheme("SH-S7", file, line)), ""
	case "fromHeader":
		// Node lower-cases header names; passport-jwt looks the name up as
		// given, so only a lower-case name can ever match.
		name, ok := literal()
		if !ok || name != strings.ToLower(name) {
			return nil, "ExtractJwt.fromHeader with a name that is not a lower-case literal"
		}
		tagShape("SH-S7/fromHeader")
		return one(apiKeyScheme("header", &name, "SH-S7", file, line)), ""
	case "fromUrlQueryParameter":
		name, ok := literal()
		if !ok {
			return nil, "ExtractJwt.fromUrlQueryParameter with a name that is not a literal"
		}
		tagShape("SH-S7/fromUrlQueryParameter")
		return one(apiKeyScheme("query", &name, "SH-S7", file, line)), ""
	}
	return nil, "ExtractJwt." + method + " is not read"
}

// nextSession reads a next-auth getServerSession call in a Next.js route
// handler (row SH-S23): next-auth reads the session from a cookie whose
// name depends on its configuration (the cookies option, and the
// __Secure- prefix chosen from the site URL), and the call returns null
// without one, so the handler decides: an intended requirement whose key
// name is not read.
func (b *shapeBuilder) nextSession(ref fnRef, e facts.ShapeEvent) {
	name := e.Path[0]
	if !b.importedFrom(ref, name, "next-auth") && !b.importedFrom(ref, name, "next-auth/next") {
		return
	}
	for _, s := range b.sessions {
		if s.file == ref.file && s.line == e.Line {
			return
		}
	}
	tagShape("SH-S23/getServerSession")
	b.sessions = append(b.sessions, secSource{rule: "SH-S23", label: "next-auth", kind: routeshape.Intended, alts: one(apiKeyScheme("cookie", nil, "SH-S23", ref.file, e.Line)), file: ref.file, line: e.Line,
		reasons: []string{"next-auth: the session cookie's name depends on the configuration", "getServerSession returns null without a session: the handler decides what happens"}})
}

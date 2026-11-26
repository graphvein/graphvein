package routes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/graphvein/graphvein/internal/extract/routeshape"
	"github.com/graphvein/graphvein/internal/lang/facts"
)

// gin and chi security (rows SH-S19..SH-S22): the route's router-level
// middleware (group_middleware, row SH-S18) and its own middleware
// arguments, in the order they run, read for the closed list. Semantics
// read in gin v1.12.0 (auth.go), chi v5.5.0 (middleware/basic_auth.go)
// and jwtauth v5.4.0 (jwtauth.go), checked by
// scripts/oracle/route_security.

// goSecurity writes the security section of a gin or chi route.
func (l *goLinker) goSecurity(reg *goRegistration, s *routeshape.Shape) {
	rm, ok := l.groupMW.byRoute[reg.identity]
	if !ok {
		return
	}
	col := &secCollector{complete: true}
	for _, u := range rm.unknown {
		col.complete = false
		col.reason(fmt.Sprintf("middleware %s may run before the route (%s): not read", u.expression, u.reason))
	}
	var verifier *secSource
	entries := append(append([]goMW(nil), rm.group...), rm.route...)
	for _, m := range entries {
		line := m.span.StartLine
		if m.implicit == "gin.Default" {
			// gin.Default's Logger and Recovery (row SH-S22).
			tagShape("SH-S22/known")
			continue
		}
		pkg, name, call, ok := l.goMWCallee(m)
		if !ok {
			col.complete = false
			col.reason(fmt.Sprintf("middleware %s (%s:%d) is not read for auth", m.text, m.file, line))
			continue
		}
		switch {
		case pkg == "github.com/gin-gonic/gin" && (name == "BasicAuth" || name == "BasicAuthForRealm") && call:
			tagShape("SH-S19/" + name)
			col.sources = append(col.sources, secSource{rule: "SH-S19", label: "gin." + name, kind: routeshape.Fact, alts: one(basicScheme("SH-S19", m.file, line)), file: m.file, line: line})
		case pkg == "github.com/go-chi/chi/v5/middleware" && name == "BasicAuth" && call:
			tagShape("SH-S20/BasicAuth")
			col.sources = append(col.sources, secSource{rule: "SH-S20", label: "middleware.BasicAuth", kind: routeshape.Fact, alts: one(basicScheme("SH-S20", m.file, line)), file: m.file, line: line})
		case goJWTAuth(pkg) && (name == "Verifier" || name == "Verify") && call:
			src := l.jwtVerifier(m, name, line)
			if verifier != nil {
				col.complete = false
				col.reason("jwtauth: a second Verifier replaces the first token in the context")
			}
			verifier = &src
		case goJWTAuth(pkg) && name == "Authenticator":
			if verifier == nil {
				col.complete = false
				col.reason(fmt.Sprintf("jwtauth.Authenticator (%s:%d) runs without a Verifier before it", m.file, line))
				continue
			}
			tagShape("SH-S21/Authenticator")
			verifier.kind = routeshape.Fact
			col.sources = append(col.sources, *verifier)
			verifier = nil
		case goKnownNonAuth(pkg, name, call):
			tagShape("SH-S22/known")
		default:
			col.complete = false
			col.reason(fmt.Sprintf("middleware %s (%s:%d) is not read for auth", m.text, m.file, line))
		}
	}
	if reg.sharedPath && len(col.sources) > 0 {
		// Row SH-S24: another registration may serve this path, so none
		// of its middleware is proven to run for a request to it.
		tagShape("SH-S24/shared-path")
		col.complete = false
		for i := range col.sources {
			col.sources[i].kind = routeshape.Intended
		}
		col.reason("another chi route of the same router has this method and path: chi keeps the last registration and prefers a static route to a mounted router's, so this registration may not serve it")
	}
	if verifier != nil {
		tagShape("SH-S21/no-Authenticator")
		verifier.kind = routeshape.Intended
		verifier.reasons = append(verifier.reasons, "jwtauth.Verifier without a later Authenticator: requests without a valid token reach the handler")
		col.sources = append(col.sources, *verifier)
	}
	if len(col.sources) > 0 && col.complete {
		tagShape("SH-S22/complete")
	}
	col.apply(s)
}

func goJWTAuth(pkg string) bool {
	return pkg == "github.com/go-chi/jwtauth" || strings.HasPrefix(pkg, "github.com/go-chi/jwtauth/")
}

// goKnownNonAuth is the closed list of middleware that never refuses a
// request for missing credentials (row SH-S22).
func goKnownNonAuth(pkg, name string, call bool) bool {
	switch pkg {
	case "github.com/gin-gonic/gin":
		return call && (name == "Logger" || name == "Recovery")
	case "github.com/go-chi/chi/v5/middleware":
		return !call && (name == "Logger" || name == "Recoverer" || name == "RequestID" || name == "RealIP")
	}
	return false
}

// goMWCallee is the import path and name a middleware argument names
// (pkg.F, or a call pkg.F(...)).
func (l *goLinker) goMWCallee(m goMW) (pkg, name string, call, ok bool) {
	e := m.expr
	if e == nil || m.at.rec == nil {
		return "", "", false, false
	}
	if e.Kind == facts.GoCall {
		call, e = true, e.Callee
	}
	if e == nil || e.Kind != facts.GoSelector || e.Object == nil {
		return "", "", false, false
	}
	imp, isImport := l.importOf(m.at, e.Object)
	if !isImport || imp.path == "" {
		return "", "", false, false
	}
	return imp.path, e.Name, call, true
}

// jwtVerifier reads jwtauth.Verifier(ja) (the header and the jwt cookie)
// or jwtauth.Verify(ja, finders...) with jwtauth's own finders (row
// SH-S21).
func (l *goLinker) jwtVerifier(m goMW, name string, line int) secSource {
	src := secSource{rule: "SH-S21", label: "jwtauth." + name, kind: routeshape.Fact, file: m.file, line: line}
	header := []routeshape.Scheme{bearerScheme("SH-S21", m.file, line)}
	cookie := []routeshape.Scheme{apiKeyScheme("cookie", strPtr("jwt"), "SH-S21", m.file, line)}
	query := []routeshape.Scheme{apiKeyScheme("query", strPtr("jwt"), "SH-S21", m.file, line)}
	if name == "Verifier" {
		tagShape("SH-S21/Verifier")
		src.alts = [][]routeshape.Scheme{header, cookie}
		return src
	}
	tagShape("SH-S21/Verify")
	args := m.expr.Args
	for i := 1; i < len(args); i++ {
		a := &args[i]
		var finder string
		if a.Kind == facts.GoSelector && a.Object != nil {
			if imp, ok := l.importOf(m.at, a.Object); ok && goJWTAuth(imp.path) {
				finder = a.Name
			}
		}
		switch finder {
		case "TokenFromHeader":
			src.alts = append(src.alts, header)
		case "TokenFromCookie":
			src.alts = append(src.alts, cookie)
		case "TokenFromQuery":
			src.alts = append(src.alts, query)
		default:
			src.alts = [][]routeshape.Scheme{{unknownScheme("jwtauth", "SH-S21", m.file, line)}}
			src.reasons = []string{"jwtauth.Verify with a token finder that is not jwtauth's own (" + goArgText(*a) + ")"}
			return src
		}
	}
	if len(src.alts) == 0 {
		src.alts = [][]routeshape.Scheme{{unknownScheme("jwtauth", "SH-S21", m.file, line)}}
		src.reasons = []string{"jwtauth.Verify without a token finder"}
	}
	return src
}

// sharedChiPaths marks every chi registration whose method and full path
// another chi registration served from the same root has, or whose path a
// route of the router it is mounted on takes (row SH-S24).
// chi v5.5.0 (tree.go) replaces the handler of an equal pattern and
// method on a later registration, and a static route of a parent router
// is matched before the routes of a router mounted below it, so only one
// of such registrations serves the path; which one is not proven here.
// Parameter names are ignored (chi matches by position) and a route for
// every method (Handle) meets any method.
func (l *goLinker) sharedChiPaths(g *goGraph, allPaths [][]goPathEntry) {
	if g == nil {
		return
	}
	type key struct{ root, path string }
	by := map[key][]int{}
	for i := range l.registrations {
		reg := &l.registrations[i]
		if reg.framework != goChi {
			continue
		}
		if l.refusedWhy[reg.identity] == goMountTaken {
			// A route of the router it is mounted on takes the path.
			reg.sharedPath = true
		}
		roots := map[string]bool{}
		for _, id := range reg.objects {
			for _, c := range g.chains[id] {
				roots[c.root] = true
			}
		}
		for root := range roots {
			for _, p := range allPaths[i] {
				path := goParamNames.ReplaceAllString(p.Template, "{}")
				if p.TrailingSlash {
					path += "/"
				}
				k := key{root, path}
				by[k] = append(by[k], i)
			}
		}
	}
	for _, list := range by {
		for x, i := range list {
			for _, j := range list[x+1:] {
				a, b := &l.registrations[i], &l.registrations[j]
				if i != j && (a.method == b.method || a.method == "ANY" || b.method == "ANY") {
					a.sharedPath, b.sharedPath = true, true
				}
			}
		}
	}
}

// goParamNames matches a path template's parameters ({id}, {*}).
var goParamNames = regexp.MustCompile(`\{[^}]*\}`)

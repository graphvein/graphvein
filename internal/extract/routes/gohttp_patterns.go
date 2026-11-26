package routes

import (
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	"github.com/graphvein/graphvein/internal/lang/facts"
)

// Patterns, methods and templates of Go routes (rows GR-P1, GR-P2 and the
// pattern refusals of GR-R1-GR-R4).

// constant evaluates e as a Go string constant (GR-P1): a string literal,
// a local const, a package-level const of the module (the file's own
// package or an imported one) or a + of those. literal reports that every
// part is a string literal.
func (l *goLinker) constant(at goAt, e *facts.GoExpr) (value string, literal, ok bool) {
	return l.constantIn(at, e, map[string]bool{})
}

func (l *goLinker) constantIn(at goAt, e *facts.GoExpr, seen map[string]bool) (string, bool, bool) {
	switch e.Kind {
	case facts.GoString:
		return e.Value, true, true
	case facts.GoBinary:
		if e.Value != "+" || e.Left == nil || e.Right == nil {
			return "", false, false
		}
		a, la, ok := l.constantIn(at, e.Left, seen)
		if !ok {
			return "", false, false
		}
		b, lb, ok := l.constantIn(at, e.Right, seen)
		return a + b, la && lb, ok
	case facts.GoIdentifier:
		n := l.lookup(at, e.Name)
		switch {
		case n.kind == "local":
			bd := n.rec.fn.Bindings[n.decl]
			i := hasName(bd.Names, e.Name)
			if bd.Kind != facts.GoBindConst || len(bd.Values) != len(bd.Names) {
				return "", false, false
			}
			key := fmt.Sprintf("%s/b%d/%d", n.rec.key, n.decl, i)
			if seen[key] {
				return "", false, false
			}
			seen[key] = true
			v, _, ok := l.constantIn(goAt{n.rec, bd.Block, goStart(bd.Span)}, &bd.Values[i], seen)
			return v, false, ok
		case n.kind == "package" && n.pkgConst != nil:
			return l.packageConst(n.file, n.pkgConst, seen)
		}
	case facts.GoSelector:
		if e.Object == nil {
			return "", false, false
		}
		imp, ok := l.importOf(at, e.Object)
		if !ok || imp.files == nil {
			return "", false, false
		}
		var found *facts.GoConst
		file := ""
		for _, p := range imp.files {
			if n := l.declared(p, e.Name); n.pkgConst != nil {
				found, file = n.pkgConst, p
			}
		}
		if found != nil {
			return l.packageConst(file, found, seen)
		}
	}
	return "", false, false
}

// packageConst evaluates a package-level constant declared in file.
func (l *goLinker) packageConst(file string, c *facts.GoConst, seen map[string]bool) (string, bool, bool) {
	key := file + "#const:" + c.Name
	if seen[key] {
		return "", false, false
	}
	seen[key] = true
	// A package-level constant's value is read in its own file's scope:
	// no local declaration encloses it.
	at := goAt{rec: &goRecord{file: file, fn: &facts.GoRouteFunc{Blocks: []facts.GoBlock{{Kind: facts.GoBlockBody}}}}}
	v, _, ok := l.constantIn(at, &c.Value, seen)
	return v, false, ok
}

// goMethods are the http.Method* constants.
var goMethods = map[string]string{
	"MethodGet": "GET", "MethodHead": "HEAD", "MethodPost": "POST", "MethodPut": "PUT", "MethodPatch": "PATCH",
	"MethodDelete": "DELETE", "MethodConnect": "CONNECT", "MethodOptions": "OPTIONS", "MethodTrace": "TRACE",
}

// method evaluates a method argument: a string constant (GR-P1) or an
// http.Method* constant.
func (l *goLinker) method(at goAt, e *facts.GoExpr) (string, bool) {
	if e.Kind == facts.GoSelector && e.Object != nil {
		if imp, ok := l.importOf(at, e.Object); ok && imp.framework == goNetHTTP {
			m, ok := goMethods[e.Name]
			return m, ok
		}
	}
	v, _, ok := l.constant(at, e)
	return v, ok
}

// goChiMethods are the methods chi v5.2.4 accepts (chi tree.go methodMap;
// v5.5.0 adds QUERY, which is version-dependent and refused).
var goChiMethods = map[string]bool{"CONNECT": true, "DELETE": true, "GET": true, "HEAD": true, "OPTIONS": true, "PATCH": true, "POST": true, "PUT": true, "TRACE": true}

// goGinMethod is gin's check of Handle's method (routergroup.go,
// regEnLetter).
var goGinMethod = regexp.MustCompile(`^[A-Z]+$`)

// goMuxPattern checks a net/http pattern with Go 1.27.1's pattern parser
// (gohttp_mux.go, GR-R1).
func goMuxPattern(pattern string) error {
	_, err := goParseMuxPattern(pattern)
	if err != nil {
		return fmt.Errorf("parsing %q: %w", pattern, err)
	}
	return nil
}

// goSplitPattern splits a net/http pattern "[METHOD ][HOST]/PATH" as
// net/http's parsePattern does (pattern.go): method, host, path, and the
// pattern without its method prefix.
func goSplitPattern(p string) (method, host, path, rest string) {
	rest = p
	if i := strings.IndexAny(p, " \t"); i >= 0 {
		method, rest = p[:i], strings.TrimLeft(p[i+1:], " \t")
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return method, "", rest, rest
	}
	return method, rest[:i], rest[i:], rest
}

// goRegexpSlash reports a chi parameter regexp that can match a string
// holding '/': any part of it that can consume '/' (a literal, a class or
// any character). The test is conservative: a regexp it flags may still
// never match '/', and is refused all the same.
func goRegexpSlash(re string) bool {
	tree, err := syntax.Parse(re, syntax.Perl)
	if err != nil {
		return true
	}
	var walk func(*syntax.Regexp) bool
	walk = func(r *syntax.Regexp) bool {
		switch r.Op {
		case syntax.OpLiteral:
			for _, c := range r.Rune {
				if c == '/' {
					return true
				}
			}
		case syntax.OpCharClass:
			for i := 0; i+1 < len(r.Rune); i += 2 {
				if r.Rune[i] <= '/' && '/' <= r.Rune[i+1] {
					return true
				}
			}
		case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
			return true
		}
		for _, s := range r.Sub {
			if walk(s) {
				return true
			}
		}
		return false
	}
	return walk(tree)
}

// goChiRegexpSlash reports a chi pattern with a {name:regexp} parameter
// whose regexp can match '/' (GR-P2's refusal regexp_param).
func goChiRegexpSlash(pattern string) bool {
	for _, p := range goChiParams(pattern) {
		if _, re, ok := strings.Cut(p, ":"); ok && goRegexpSlash(re) {
			return true
		}
	}
	return false
}

// goChiParams lists the contents of a chi pattern's {...} parameters,
// counting nested braces as chi's patNextSegment does.
func goChiParams(pattern string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					out = append(out, pattern[start:i])
				}
			}
		}
	}
	return out
}

// template is goTemplate with the GR-P2 tag (step GR3 composes through
// it).
func (l *goLinker) template(framework, raw string) (string, bool, bool) {
	template, trailing, ok := goTemplate(framework, raw)
	if ok {
		l.tag("GR-P2")
	}
	return template, trailing, ok
}

// goTemplate normalises a composed raw path of framework into a path
// template (GR-P2): gin :x and *x to {x} and {*x}; chi {x} kept, {x:re} to
// {x}, * to {*}; net/http {x} kept, {x...} to {*x}, a trailing {$} to the
// exact path with trailing slash, a subtree pattern /a/ to /a/{*}; then
// the shared slash rules (routes.md), a trailing slash elsewhere giving
// trailingSlash. ok is false for a chi regexp that can match '/'.
func goTemplate(framework, raw string) (template string, trailingSlash, ok bool) {
	if framework == goChi {
		// chi's parameters are brace-delimited and a regexp may hold
		// '/': rewrite them before splitting into segments.
		var b strings.Builder
		rest := raw
		for {
			i := strings.IndexByte(rest, '{')
			if i < 0 {
				b.WriteString(rest)
				break
			}
			b.WriteString(rest[:i])
			params := goChiParams(rest[i:])
			if len(params) == 0 {
				b.WriteString(rest[i:])
				break
			}
			name, re, hasRe := strings.Cut(params[0], ":")
			if hasRe && goRegexpSlash(re) {
				return "", false, false
			}
			b.WriteString("{" + name + "}")
			rest = rest[i+len(params[0])+2:]
		}
		raw = b.String()
	}
	var segments []string
	parts := strings.Split(raw, "/")
	for i, p := range parts {
		last := i == len(parts)-1
		switch framework {
		case goGin:
			if strings.HasPrefix(p, ":") && len(p) > 1 {
				p = "{" + p[1:] + "}"
			} else if strings.HasPrefix(p, "*") {
				p = "{*" + p[1:] + "}"
			}
		case goChi:
			if p == "*" {
				p = "{*}"
			} else if strings.HasSuffix(p, "*") && last {
				p = strings.TrimSuffix(p, "*") + "{*}"
			}
		case goNetHTTP:
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "...}") {
				p = "{*" + strings.TrimSuffix(p[1:], "...}") + "}"
			} else if p == "{$}" && last {
				segments = append(segments, "")
				continue
			}
		}
		segments = append(segments, p)
	}
	joined := strings.Join(segments, "/")
	subtree := framework == goNetHTTP && strings.HasSuffix(raw, "/") && !strings.HasSuffix(raw, "{$}")
	if subtree {
		joined += "{*}"
	}
	trailing := strings.HasSuffix(joined, "/")
	var kept []string
	for _, s := range strings.Split(joined, "/") {
		if s != "" {
			kept = append(kept, s)
		}
	}
	template = "/" + strings.Join(kept, "/")
	return template, trailing && template != "/", true
}

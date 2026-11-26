package routes

import (
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode"
)

// net/http ServeMux patterns (row GR-R1): the pattern parser and the
// conflict check of Go 1.27.1's ServeMux (src/net/http/pattern.go:
// parsePattern, isValidWildcardName, pathUnescape, conflictsWith and the
// relationship functions; server.go: cleanPath and registerErr's checks;
// request.go: validMethod with httpguts.IsTokenRune). They are
// transcribed here rather than run on a real ServeMux: importing net/http
// into the indexer widens its reviewed network dependency surface
// (internal/egress TestDependencySurface). TestGoMuxPatternOracle checks
// the transcription against the real ServeMux. Go source: BSD-style
// licence, Copyright The Go Authors.

// goMuxSegment is one path segment of a parsed pattern: a literal, a
// wildcard name, or "/" for a final {$}.
type goMuxSegment struct {
	s     string
	wild  bool
	multi bool
}

// goMuxPat is a parsed pattern.
type goMuxPat struct {
	str, method, host string
	segments          []goMuxSegment
}

func (p *goMuxPat) last() goMuxSegment { return p.segments[len(p.segments)-1] }

// goMuxTokens are the token characters of RFC 7230 besides letters and
// digits (httpguts isTokenTable).
const goMuxTokens = "!#$%&'*+-.^_`|~"

// goValidMethod is net/http's validMethod: a non-empty token.
func goValidMethod(method string) bool {
	if method == "" {
		return false
	}
	for _, r := range method {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(goMuxTokens, r)) {
			return false
		}
	}
	return true
}

// goCleanPath is net/http's cleanPath.
func goCleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	np := path.Clean(p)
	if p[len(p)-1] == '/' && np != "/" {
		if len(p) == len(np)+1 && strings.HasPrefix(p, np) {
			np = p
		} else {
			np += "/"
		}
	}
	return np
}

// goParseMuxPattern is net/http's parsePattern (with registerErr's empty
// pattern check).
func goParseMuxPattern(s string) (_ *goMuxPat, err error) {
	if len(s) == 0 {
		return nil, errors.New("empty pattern")
	}
	off := 0
	defer func() {
		if err != nil {
			err = fmt.Errorf("at offset %d: %w", off, err)
		}
	}()
	method, rest, found := s, "", false
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		method, rest, found = s[:i], strings.TrimLeft(s[i+1:], " \t"), true
	}
	if !found {
		rest = method
		method = ""
	}
	if method != "" && !goValidMethod(method) {
		return nil, fmt.Errorf("invalid method %q", method)
	}
	p := &goMuxPat{str: s, method: method}
	if found {
		off = len(method) + 1
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return nil, errors.New("host/path missing /")
	}
	p.host = rest[:i]
	rest = rest[i:]
	if j := strings.IndexByte(p.host, '{'); j >= 0 {
		off += j
		return nil, errors.New("host contains '{' (missing initial '/'?)")
	}
	off += i
	if method != "" && method != "CONNECT" && rest != goCleanPath(rest) {
		return nil, errors.New("non-CONNECT pattern with unclean path can never match")
	}
	seenNames := map[string]bool{}
	for len(rest) > 0 {
		rest = rest[1:]
		off = len(s) - len(rest)
		if len(rest) == 0 {
			p.segments = append(p.segments, goMuxSegment{wild: true, multi: true})
			break
		}
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			i = len(rest)
		}
		var seg string
		seg, rest = rest[:i], rest[i:]
		if i := strings.IndexByte(seg, '{'); i < 0 {
			if u, err := url.PathUnescape(seg); err == nil {
				seg = u
			}
			p.segments = append(p.segments, goMuxSegment{s: seg})
		} else {
			if i != 0 {
				return nil, errors.New("bad wildcard segment (must start with '{')")
			}
			if seg[len(seg)-1] != '}' {
				return nil, errors.New("bad wildcard segment (must end with '}')")
			}
			name := seg[1 : len(seg)-1]
			if name == "$" {
				if len(rest) != 0 {
					return nil, errors.New("{$} not at end")
				}
				p.segments = append(p.segments, goMuxSegment{s: "/"})
				break
			}
			name, multi := strings.CutSuffix(name, "...")
			if multi && len(rest) != 0 {
				return nil, errors.New("{...} wildcard not at end")
			}
			if name == "" {
				return nil, errors.New("empty wildcard")
			}
			if !goWildcardName(name) {
				return nil, fmt.Errorf("bad wildcard name %q", name)
			}
			if seenNames[name] {
				return nil, fmt.Errorf("duplicate wildcard name %q", name)
			}
			seenNames[name] = true
			p.segments = append(p.segments, goMuxSegment{s: name, wild: true, multi: multi})
		}
	}
	return p, nil
}

// goWildcardName is net/http's isValidWildcardName: a Go identifier.
func goWildcardName(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !unicode.IsLetter(c) && c != '_' && (i == 0 || !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

// Pattern relationships (net/http pattern.go).
const (
	goEquivalent   = "equivalent"
	goMoreGeneral  = "moreGeneral"
	goMoreSpecific = "moreSpecific"
	goDisjoint     = "disjoint"
	goOverlaps     = "overlaps"
)

// conflicts is net/http's conflictsWith.
func (p1 *goMuxPat) conflicts(p2 *goMuxPat) bool {
	if p1.host != p2.host {
		return false
	}
	rel := p1.comparePathsAndMethods(p2)
	return rel == goEquivalent || rel == goOverlaps
}

func (p1 *goMuxPat) comparePathsAndMethods(p2 *goMuxPat) string {
	mrel := p1.compareMethods(p2)
	if mrel == goDisjoint {
		return goDisjoint
	}
	return goCombine(mrel, p1.comparePaths(p2))
}

func (p1 *goMuxPat) compareMethods(p2 *goMuxPat) string {
	switch {
	case p1.method == p2.method:
		return goEquivalent
	case p1.method == "":
		return goMoreGeneral
	case p2.method == "":
		return goMoreSpecific
	case p1.method == "GET" && p2.method == "HEAD":
		return goMoreGeneral
	case p2.method == "GET" && p1.method == "HEAD":
		return goMoreSpecific
	}
	return goDisjoint
}

func (p1 *goMuxPat) comparePaths(p2 *goMuxPat) string {
	if len(p1.segments) != len(p2.segments) && !p1.last().multi && !p2.last().multi {
		return goDisjoint
	}
	var segs1, segs2 []goMuxSegment
	rel := goEquivalent
	for segs1, segs2 = p1.segments, p2.segments; len(segs1) > 0 && len(segs2) > 0; segs1, segs2 = segs1[1:], segs2[1:] {
		rel = goCombine(rel, goCompareSegments(segs1[0], segs2[0]))
		if rel == goDisjoint {
			return rel
		}
	}
	if len(segs1) == 0 && len(segs2) == 0 {
		return rel
	}
	if len(segs1) < len(segs2) && p1.last().multi {
		return goCombine(rel, goMoreGeneral)
	}
	if len(segs2) < len(segs1) && p2.last().multi {
		return goCombine(rel, goMoreSpecific)
	}
	return goDisjoint
}

func goCompareSegments(s1, s2 goMuxSegment) string {
	switch {
	case s1.multi && s2.multi:
		return goEquivalent
	case s1.multi:
		return goMoreGeneral
	case s2.multi:
		return goMoreSpecific
	case s1.wild && s2.wild:
		return goEquivalent
	case s1.wild:
		if s2.s == "/" {
			return goDisjoint
		}
		return goMoreGeneral
	case s2.wild:
		if s1.s == "/" {
			return goDisjoint
		}
		return goMoreSpecific
	case s1.s == s2.s:
		return goEquivalent
	}
	return goDisjoint
}

func goCombine(r1, r2 string) string {
	switch r1 {
	case goEquivalent:
		return r2
	case goDisjoint:
		return goDisjoint
	case goOverlaps:
		if r2 == goDisjoint {
			return goDisjoint
		}
		return goOverlaps
	}
	// moreGeneral or moreSpecific
	switch r2 {
	case goEquivalent:
		return r1
	case goInverse(r1):
		return goOverlaps
	}
	return r2
}

func goInverse(r string) string {
	switch r {
	case goMoreSpecific:
		return goMoreGeneral
	case goMoreGeneral:
		return goMoreSpecific
	}
	return r
}

// goMux is the conflict state of one ServeMux object: its registered
// patterns, in order.
type goMux struct{ patterns []*goMuxPat }

// register adds pattern as ServeMux.Handle would, reporting the earlier
// pattern it conflicts with (the registration panics then).
func (m *goMux) register(pattern string) (conflict string, err error) {
	p, err := goParseMuxPattern(pattern)
	if err != nil {
		return "", fmt.Errorf("parsing %q: %w", pattern, err)
	}
	for _, q := range m.patterns {
		if p.conflicts(q) {
			return q.str, nil
		}
	}
	m.patterns = append(m.patterns, p)
	return "", nil
}

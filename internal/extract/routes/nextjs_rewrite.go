package routes

import (
	"regexp"
	"strings"
)

// Literal rewrite rules applied to a call path (row NX-L3, T024a2). The
// rules follow Next.js's own reading (next 14.1.0 and 16.2.10,
// dist/lib/load-custom-routes.js and
// dist/server/lib/router-utils/resolve-routes.js): a source is a
// path-to-regexp path matched case-insensitively and strictly; a
// destination that does not start with "/" is external; a destination's
// parameters are filled from the source's.

// Outcomes of NextRewrite.
const (
	// RewriteNoMatch: the rule's source cannot match the call path.
	RewriteNoMatch = "no_match"
	// RewriteUnsure: the source may match, but not provably (a call
	// parameter where the source has a literal, a case difference, a
	// trailing slash, or splits that give different destinations).
	RewriteUnsure = "unsure"
	// RewriteExternal: the source matches and the destination is another
	// origin.
	RewriteExternal = "external"
	// RewriteUnmapped: the source matches but the destination is not a
	// path of literal segments and source parameters.
	RewriteUnmapped = "unmapped"
	// RewriteApplied: the source matches and the destination path is known.
	RewriteApplied = "rewritten"
)

var (
	rewriteParam   = regexp.MustCompile(`^:([A-Za-z_][A-Za-z0-9_]*)([*+?]?)$`)
	rewriteLiteral = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)
)

// rewriteSplit is one way a source matches the call segments: the
// segments each parameter takes, and whether every literal source segment
// met an equal literal call segment.
type rewriteSplit struct {
	captures map[string][]string
	repeat   map[string]bool
	sure     bool
}

// maxRewriteSplits bounds the enumeration; more splits read as unsure.
const maxRewriteSplits = 64

// NextRewrite applies one literal rewrite rule (source and destination
// already prefixed with the project's base path) to call segments. A call
// segment written `{name}` is a call parameter: it may hold any value, so
// it proves no match against a literal source segment. path is the
// rewritten path's segments when outcome is RewriteApplied.
func NextRewrite(source, destination string, call []string, callSlash bool) (path []string, outcome string) {
	src := pathSegments(source)
	var splits []rewriteSplit
	overflow := false
	var walk func(i, j int, s rewriteSplit)
	walk = func(i, j int, s rewriteSplit) {
		if overflow {
			return
		}
		if i == len(src) {
			if j == len(call) {
				if len(splits) == maxRewriteSplits {
					overflow = true
					return
				}
				splits = append(splits, s)
			}
			return
		}
		seg := src[i]
		m := rewriteParam.FindStringSubmatch(seg)
		if m == nil {
			if j == len(call) {
				return
			}
			c := call[j]
			switch {
			case callParam(c):
				s.sure = false
			case c == seg:
			case strings.EqualFold(c, seg):
				// Next.js matches case-insensitively by default; whether
				// experimental.caseSensitiveRoutes is set is not read.
				s.sure = false
			default:
				return
			}
			walk(i+1, j+1, s)
			return
		}
		name, modifier := m[1], m[2]
		least, most := 1, 1
		switch modifier {
		case "?":
			least = 0
		case "*":
			least, most = 0, len(call)-j
		case "+":
			most = len(call) - j
		}
		for n := least; n <= most && j+n <= len(call); n++ {
			next := rewriteSplit{captures: map[string][]string{}, repeat: map[string]bool{}, sure: s.sure}
			for k, v := range s.captures {
				next.captures[k] = v
			}
			for k, v := range s.repeat {
				next.repeat[k] = v
			}
			next.captures[name] = call[j : j+n]
			next.repeat[name] = modifier == "*" || modifier == "+"
			walk(i+1, j+n, next)
		}
	}
	walk(0, 0, rewriteSplit{captures: map[string][]string{}, repeat: map[string]bool{}, sure: true})
	if overflow {
		return nil, RewriteUnsure
	}
	if len(splits) == 0 {
		return nil, RewriteNoMatch
	}
	// Matching is strict: a trailing slash on either side is a different
	// path, which Graphvein does not model.
	if callSlash || len(src) > 0 && strings.HasSuffix(source, "/") {
		return nil, RewriteUnsure
	}
	for _, s := range splits {
		if !s.sure {
			return nil, RewriteUnsure
		}
	}
	if !strings.HasPrefix(destination, "/") {
		return nil, RewriteExternal
	}
	if strings.HasPrefix(destination, "//") {
		// A protocol-relative destination: next 14.1.0 treats it as
		// external, 16.2.10 as a path; Graphvein reads neither.
		return nil, RewriteUnmapped
	}
	destination = destinationPath(destination)
	var result []string
	for n, s := range splits {
		var out []string
		for _, seg := range pathSegments(destination) {
			m := rewriteParam.FindStringSubmatch(seg)
			if m == nil {
				if !rewriteLiteral.MatchString(seg) {
					return nil, RewriteUnmapped
				}
				out = append(out, seg)
				continue
			}
			value, ok := s.captures[m[1]]
			if !ok {
				return nil, RewriteUnmapped
			}
			switch m[2] {
			case "":
				if len(value) != 1 || s.repeat[m[1]] {
					return nil, RewriteUnmapped
				}
			case "?":
				// Next.js's prepareDestination throws for an absent
				// optional parameter (next 14.1.0, 16.2.10).
				if len(value) != 1 || s.repeat[m[1]] {
					return nil, RewriteUnmapped
				}
			case "+":
				if len(value) == 0 {
					return nil, RewriteUnmapped
				}
			}
			out = append(out, value...)
		}
		if n > 0 && strings.Join(out, "/") != strings.Join(result, "/") {
			return nil, RewriteUnsure
		}
		result = out
	}
	return result, RewriteApplied
}

// NextRewriteMayMatch reports whether a rewrite source may match the call
// segments (any outcome but RewriteNoMatch).
func NextRewriteMayMatch(source string, call []string) bool {
	_, outcome := NextRewrite(source, "/", call, false)
	return outcome != RewriteNoMatch
}

// destinationPath drops a destination's query and fragment; a "?" right
// after a parameter name is the parameter's modifier, not a query.
func destinationPath(destination string) string {
	for i, c := range destination {
		if c == '#' {
			return destination[:i]
		}
		if c == '?' {
			segment := destination[strings.LastIndex(destination[:i], "/")+1 : i]
			if !rewriteName.MatchString(segment) {
				return destination[:i]
			}
		}
	}
	return destination
}

var rewriteName = regexp.MustCompile(`^:[A-Za-z_][A-Za-z0-9_]*$`)

func pathSegments(s string) []string {
	return strings.FieldsFunc(s, func(c rune) bool { return c == '/' })
}

func callParam(s string) bool { return strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") }

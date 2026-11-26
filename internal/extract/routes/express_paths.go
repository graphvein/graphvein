package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// This file reads Express route path strings (a registration pattern or a
// `use` mount prefix) into path templates (T028, claim contract
// docs/contract/claims/express5-paths.md). Express 4 parses paths with
// path-to-regexp 0.1.x and Express 5 with path-to-regexp 8, which give the
// same text different meanings: `/*splat` is a named catch-all in Express
// 5 but `.*` followed by the literal `splat` in Express 4, and `:id?` is
// optional in Express 4 but refused in Express 5. The major comes from the
// package.json that declares `express` (row EX5-W12); a path is claimed
// only by a row of its major, and under an unknown major only when both
// readings agree (EX5-W11). Every other form goes to EX5-W14: no path
// through the piece, never a guessed literal.

// ExpressPathRow is one row of the Express path claim contract.
type ExpressPathRow struct {
	ID string
	// Major is "4", "5", "unknown", "major" (how the major is learned) or
	// "default".
	Major string
	// Form names the path form as the contract table does.
	Form string
}

// ExpressPathContract lists the rows of docs/contract/claims/express5-paths.md
// in table order. Every form the readers model maps to exactly one row;
// every other form is EX5-W14.
var ExpressPathContract = []ExpressPathRow{
	{"EX5-W1", "4", "literal text"},
	{"EX5-W2", "4", "`:name` filling a segment"},
	{"EX5-W3", "4", "`:name?` filling a segment"},
	{"EX5-W4", "4", "`*` ending a registration pattern"},
	{"EX5-W5", "5", "literal text"},
	{"EX5-W6", "5", "`:name` filling a segment"},
	{"EX5-W7", "5", "`*name` ending a registration pattern after `/`"},
	{"EX5-W8", "5", "`*name` as the whole registration pattern"},
	{"EX5-W9", "5", "`{...}` optional groups"},
	{"EX5-W10", "5", "a form path-to-regexp 8 refuses"},
	{"EX5-W11", "unknown", "both readings agree"},
	{"EX5-W12", "major", "package.json declaring express 4 or 5"},
	{"EX5-W13", "major", "any other declaration, or none"},
	{"EX5-W14", "default", "every other form"},
}

// expressPathTag is a test-only hook: every branch reports its row.
var expressPathTag func(row string)

func pathTag(row string) {
	if expressPathTag != nil {
		expressPathTag(row)
	}
}

// Diagnostic codes of rows EX5-W10 (warning) and EX5-W14 (info).
const (
	routePatternInvalid    = "route_pattern_invalid"
	routePatternUnmodelled = "route_pattern_unmodelled"
)

// maxExpressCombinations bounds the group expansion of row EX5-W9 (path-to-
// regexp 8 itself refuses more than 256).
const maxExpressCombinations = 16

// Express majors; 0 is unknown (EX5-W13).
const (
	expressMajorUnknown = 0
	expressMajor4       = 4
	expressMajor5       = 5
)

// pathReading is what one path piece reads to: the alternative templates
// (more than one only for Express 5 groups, EX5-W9) and whether an
// Express 4 optional parameter occurs (EX5-W3). A refusal has no
// alternatives and names its row and reason.
type pathReading struct {
	alternatives []string
	optional     bool
	refusal      string // "" when read; otherwise routePatternInvalid or routePatternUnmodelled
	reason       string
}

func refused(code, reason string) pathReading {
	return pathReading{refusal: code, reason: reason}
}

// readExpressPath reads one written piece under a major. pattern is true
// for a registration pattern and false for a mount prefix: a catch-all
// claims only at the end of a registration pattern.
func readExpressPath(piece string, major int, pattern bool) pathReading {
	if r := expressPieceShape(piece, pattern); r.refusal != "" {
		return r
	}
	switch major {
	case expressMajor4:
		return readExpress4(piece, pattern)
	case expressMajor5:
		return readExpress5(piece, pattern)
	}
	four := readExpress4(piece, pattern)
	five := readExpress5(piece, pattern)
	if four.refusal == "" && five.refusal == "" && four.optional == five.optional && strings.Join(four.alternatives, "\x00") == strings.Join(five.alternatives, "\x00") {
		pathTag("EX5-W11")
		return four
	}
	pathTag("EX5-W14")
	return refused(routePatternUnmodelled, fmt.Sprintf("Express major unknown and Express 4 and 5 read %q differently", piece))
}

// expressNeverMatched holds the bytes a request path never carries as
// written (Node's HTTP parser and every client send them percent-encoded or
// not at all; the WHATWG path percent-encode set plus `#`): a literal with
// one of them never matches.
func expressNeverMatched(c byte) bool {
	return c <= ' ' || c >= 0x7f || strings.IndexByte("\"#<>^`", c) >= 0
}

// expressPieceShape refuses (EX5-W14) the pieces neither major can match as
// the slash-normalised template would claim, whatever the major (T028
// review): a non-empty piece that does not start with `/` (request
// paths do; a whole-pattern `*` or `*name` is read by EX5-W4 and EX5-W8),
// and a literal byte a request path never carries. Empty segments are
// checked per major (emptySegment).
func expressPieceShape(piece string, pattern bool) pathReading {
	whole := pattern && (piece == "*" || (strings.HasPrefix(piece, "*") && express5Name.FindString(piece[1:]) == piece[1:]))
	if piece != "" && piece[0] != '/' && !whole {
		pathTag("EX5-W14")
		return refused(routePatternUnmodelled, fmt.Sprintf("Express path %q does not start with \"/\": no request path matches it", piece))
	}
	for i := 0; i < len(piece); i++ {
		if expressNeverMatched(piece[i]) {
			what := fmt.Sprintf("%q", string(piece[i]))
			if piece[i] >= 0x80 {
				what = "non-ASCII text"
			}
			pathTag("EX5-W14")
			return refused(routePatternUnmodelled, fmt.Sprintf("Express path %q holds %s, which no request path carries as written", piece, what))
		}
	}
	return pathReading{}
}

// emptySegment refuses (EX5-W14) a piece with an empty segment (`//`),
// which Express matches only as written while the template would collapse
// it (T028 review). Express 4 makes only one trailing slash optional,
// so `/api//` is checked as written; Express 5 drops trailing slashes
// first (router 2.x `loosen`), so its caller passes the loosened piece.
func emptySegment(piece string) pathReading {
	if strings.Trim(piece, "/") != "" && strings.Contains(piece, "//") {
		pathTag("EX5-W14")
		return refused(routePatternUnmodelled, fmt.Sprintf("Express path %q has an empty segment, which Express does not collapse", piece))
	}
	return pathReading{}
}

// express4Name is path-to-regexp 0.1.x's parameter name (`\w+`).
var express4Name = regexp.MustCompile(`^[A-Za-z0-9_]+`)

// readExpress4 reads a piece as path-to-regexp 0.1.10 (Express 4.21.1)
// does, for the forms of rows EX5-W1..EX5-W4.
func readExpress4(piece string, pattern bool) pathReading {
	if r := emptySegment(piece); r.refusal != "" {
		return r
	}
	var out strings.Builder
	optional := false
	segmentStart := true
	for i := 0; i < len(piece); {
		c := piece[i]
		switch {
		case c == ':':
			name := express4Name.FindString(piece[i+1:])
			if name == "" || !segmentStart {
				pathTag("EX5-W14")
				return refused(routePatternUnmodelled, fmt.Sprintf("Express 4 parameter inside a segment in %q", piece))
			}
			j := i + 1 + len(name)
			opt := j < len(piece) && piece[j] == '?'
			if opt {
				j++
			}
			if j < len(piece) && piece[j] != '/' {
				pathTag("EX5-W14")
				return refused(routePatternUnmodelled, fmt.Sprintf("Express 4 parameter not filling its segment in %q", piece))
			}
			if opt {
				// T028 review: only a final `:name?` of a registration
				// pattern may be absent as the matcher reads `optional`
				// (dropping the final parameter); elsewhere the present
				// form alone is claimed.
				pathTag("EX5-W3")
				optional = optional || (pattern && j == len(strings.TrimSuffix(piece, "/")))
			} else {
				pathTag("EX5-W2")
			}
			out.WriteString("{" + name + "}")
			i = j
			segmentStart = false
		case c == '*':
			if !pattern || !segmentStart || i != len(piece)-1 {
				pathTag("EX5-W14")
				return refused(routePatternUnmodelled, fmt.Sprintf("Express 4 wildcard `*` (any text, slashes included) not ending a registration pattern as a whole segment in %q", piece))
			}
			pathTag("EX5-W4")
			out.WriteString("{*}")
			i++
		case strings.IndexByte(`\?+()[]{}|^$`, c) >= 0:
			pathTag("EX5-W14")
			return refused(routePatternUnmodelled, fmt.Sprintf("Express 4 regular expression syntax %q in %q", string(c), piece))
		default:
			pathTag("EX5-W1")
			out.WriteByte(c)
			segmentStart = c == '/'
			i++
		}
	}
	return pathReading{alternatives: []string{out.String()}, optional: optional}
}

// express5 tokens: path-to-regexp 8's text, param, wildcard and group.
type express5Token struct {
	kind   string // text, param, wildcard, group
	value  string
	tokens []express5Token
}

// express5Name is the ASCII part of path-to-regexp 8's identifier
// (ID_Start and ID_Continue plus `$` and `_`).
var express5Name = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*`)

// parseExpress5 tokenizes as path-to-regexp 8.x `parse` does. It returns
// the row and reason of a refusal: EX5-W10 where path-to-regexp throws,
// EX5-W14 for forms it accepts that are not modelled (escapes, quoted and
// non-ASCII names).
func parseExpress5(s string, i int, end byte) ([]express5Token, int, string, string) {
	var out []express5Token
	text := ""
	flush := func() {
		if text != "" {
			out = append(out, express5Token{kind: "text", value: text})
			text = ""
		}
	}
	for i < len(s) {
		c := s[i]
		i++
		if end != 0 && c == end {
			flush()
			return out, i, "", ""
		}
		switch {
		case c == '\\':
			return nil, i, "EX5-W14", "escape"
		case c == ':' || c == '*':
			name := express5Name.FindString(s[i:])
			if name == "" {
				if i < len(s) && (s[i] == '"' || s[i] >= 0x80) {
					return nil, i, "EX5-W14", "quoted or non-ASCII name"
				}
				return nil, i, "EX5-W10", fmt.Sprintf("missing parameter name after %q", string(c))
			}
			i += len(name)
			if i < len(s) && s[i] >= 0x80 {
				return nil, i, "EX5-W14", "non-ASCII name"
			}
			flush()
			kind := "param"
			if c == '*' {
				kind = "wildcard"
			}
			out = append(out, express5Token{kind: kind, value: name})
		case c == '{':
			flush()
			inner, next, row, reason := parseExpress5(s, i, '}')
			if row != "" {
				return nil, next, row, reason
			}
			i = next
			out = append(out, express5Token{kind: "group", tokens: inner})
		case strings.IndexByte("}()[]+?!", c) >= 0:
			return nil, i, "EX5-W10", fmt.Sprintf("unexpected %q", string(c))
		default:
			text += string(c)
		}
	}
	if end != 0 {
		return nil, i, "EX5-W10", "unterminated group"
	}
	flush()
	return out, i, "", ""
}

// flattenExpress5 expands groups into every sequence path-to-regexp 8
// matches (its `flatten`): each group present, then absent. emit returns
// false to stop the expansion.
func flattenExpress5(tokens, prefix []express5Token, emit func([]express5Token) bool) bool {
	for i, t := range tokens {
		if t.kind == "group" {
			rest := tokens[i+1:]
			with := append(append([]express5Token(nil), t.tokens...), rest...)
			return flattenExpress5(with, append([]express5Token(nil), prefix...), emit) &&
				flattenExpress5(rest, append([]express5Token(nil), prefix...), emit)
		}
		prefix = append(prefix, t)
	}
	return emit(prefix)
}

// readExpress5 reads a piece as path-to-regexp 8 (Express 5.1.0, router
// 2.2.0) does, for rows EX5-W5..EX5-W10.
func readExpress5(piece string, pattern bool) pathReading {
	// router 2.x `loosen`: trailing slashes go before the path is parsed.
	if piece != "/" {
		piece = strings.TrimRight(piece, "/")
	}
	if r := emptySegment(piece); r.refusal != "" {
		return r
	}
	tokens, _, row, reason := parseExpress5(piece, 0, 0)
	if row == "EX5-W10" {
		pathTag(row)
		return refused(routePatternInvalid, fmt.Sprintf("Express 5 refuses %q: %s", piece, reason))
	}
	if row != "" {
		pathTag(row)
		return refused(routePatternUnmodelled, fmt.Sprintf("Express 5 %s in %q", reason, piece))
	}
	grouped := false
	for _, t := range tokens {
		grouped = grouped || t.kind == "group"
	}
	var sequences [][]express5Token
	complete := flattenExpress5(tokens, nil, func(seq []express5Token) bool {
		sequences = append(sequences, append([]express5Token(nil), seq...))
		return len(sequences) <= maxExpressCombinations
	})
	if !complete {
		pathTag("EX5-W14")
		return refused(routePatternUnmodelled, fmt.Sprintf("Express 5 path %q has more than %d group combinations", piece, maxExpressCombinations))
	}
	if grouped && !pattern {
		// A mount matches with `end: false`: the regular expression takes
		// the first alternative that reaches a segment boundary, so the
		// alternatives of a prefix are not a union.
		pathTag("EX5-W14")
		return refused(routePatternUnmodelled, fmt.Sprintf("Express 5 group in the mount prefix %q", piece))
	}
	if grouped {
		pathTag("EX5-W9")
	}
	seen := map[string]bool{}
	var alternatives []string
	var failure pathReading
	for _, seq := range sequences {
		text, r := renderExpress5(piece, seq, pattern)
		if r.refusal != "" {
			// path-to-regexp throws for the whole path when any sequence
			// fails; an unmodelled sequence leaves the path unknown.
			if r.refusal == routePatternInvalid || failure.refusal == "" {
				failure = r
			}
			continue
		}
		// T028 review: Express 5 does not collapse slashes; an
		// alternative with an empty segment or a trailing slash the written
		// path does not have matches only that exact form.
		if strings.Contains(text, "//") || (len(text) > 1 && strings.HasSuffix(text, "/")) {
			pathTag("EX5-W14")
			if failure.refusal == "" {
				failure = refused(routePatternUnmodelled, fmt.Sprintf("Express 5 group alternative %q of %q keeps an empty segment or a trailing slash", text, piece))
			}
			continue
		}
		if !seen[text] {
			seen[text] = true
			alternatives = append(alternatives, text)
		}
	}
	if failure.refusal != "" {
		return failure
	}
	sort.Strings(alternatives)
	return pathReading{alternatives: alternatives}
}

// renderExpress5 renders one flattened sequence as a template.
func renderExpress5(piece string, seq []express5Token, pattern bool) (string, pathReading) {
	// path-to-regexp 8 toRegExpSource: a capture after an earlier capture
	// needs text between them ("Missing text before").
	captured, between := false, false
	for _, t := range seq {
		if t.kind == "text" {
			between = true
			continue
		}
		if captured && !between {
			pathTag("EX5-W10")
			return "", refused(routePatternInvalid, fmt.Sprintf("Express 5 refuses %q: missing text before %q", piece, t.value))
		}
		captured, between = true, false
	}
	var out strings.Builder
	for i, t := range seq {
		before := ""
		if i > 0 && seq[i-1].kind == "text" {
			before = seq[i-1].value
		}
		after := ""
		if i+1 < len(seq) && seq[i+1].kind == "text" {
			after = seq[i+1].value
		}
		switch t.kind {
		case "text":
			pathTag("EX5-W5")
			out.WriteString(t.value)
		case "param":
			startsSegment := (i == 0) || strings.HasSuffix(before, "/")
			endsSegment := i+1 == len(seq) || strings.HasPrefix(after, "/")
			if !startsSegment || !endsSegment {
				pathTag("EX5-W14")
				return "", refused(routePatternUnmodelled, fmt.Sprintf("Express 5 parameter inside a segment in %q", piece))
			}
			pathTag("EX5-W6")
			out.WriteString("{" + t.value + "}")
		case "wildcard":
			last := i+1 == len(seq)
			switch {
			case pattern && last && i > 0 && strings.HasSuffix(before, "/"):
				pathTag("EX5-W7")
			case pattern && last && i == 0:
				pathTag("EX5-W8")
			default:
				pathTag("EX5-W14")
				return "", refused(routePatternUnmodelled, fmt.Sprintf("Express 5 wildcard `*%s` not ending a registration pattern as a whole segment in %q", t.value, piece))
			}
			out.WriteString("{*" + t.value + "}")
		}
	}
	return out.String(), pathReading{}
}

// slashes applies the shared template normalization: join, collapse
// repeated slashes, a leading `/`, no trailing `/` except for the root.
func slashes(template string) string {
	parts := strings.FieldsFunc(template, func(r rune) bool { return r == '/' })
	return "/" + strings.Join(parts, "/")
}

// expressMajorRange is a version range every version of which has one
// major: an optional `^`, `~`, `=` or `v`, the major, and up to two
// numeric or `x`/`*` parts, with no prerelease (Express 5 betas used
// path-to-regexp 3).
var expressMajorRange = regexp.MustCompile(`^[\^~=v]?\s*([0-9]+)(\.([0-9]+|[xX*])){0,2}$`)

// expressMajor learns which Express major parses the paths written in
// file (EX5-W12): the nearest package.json at or above the file's
// directory that declares `express` (dependencies, devDependencies,
// peerDependencies or optionalDependencies) decides, as Node's module
// resolution finds the nearest installed copy. Every declared range must
// name the same major, 4 or 5; anything else is unknown (EX5-W13). A
// manifest on the way that exists but does not parse stops the walk with
// an unknown major (T028 review): it may declare the copy Node finds.
// Known gap: Express parses a path with the copy that created
// the app or router, which may sit under another package.json than the
// file of the call.
func (r *resolver) expressMajor(file string) int {
	if r.majors == nil {
		r.majors = map[string]int{}
	}
	dir := path.Dir(file)
	if m, ok := r.majors[dir]; ok {
		return m
	}
	major := expressMajorUnknown
	for d := dir; ; d = path.Dir(d) {
		if ranges, stop := expressDeclared(r, joinDir(d, "package.json")); stop {
			major = majorOfRanges(ranges)
			break
		}
		if d == "." || d == "/" {
			break
		}
	}
	if major == expressMajorUnknown {
		pathTag("EX5-W13")
	} else {
		pathTag("EX5-W12")
	}
	r.majors[dir] = major
	return major
}

// joinDir names a file in a repository-relative directory.
func joinDir(dir, name string) string {
	if dir == "." || dir == "/" || dir == "" {
		return name
	}
	return dir + "/" + name
}

// expressDeclared reads the express ranges a manifest declares. stop is
// true when the manifest decides the major: it declares express, or it
// exists and does not parse (its ranges are then empty: unknown). A UTF-8
// byte order mark is ignored, as Node ignores it. A `catalog:` range is
// replaced by the pnpm catalog entry it names.
func expressDeclared(r *resolver, manifest string) (ranges []string, stop bool) {
	data, ok := r.modules.Source(manifest)
	if !ok {
		return nil, false
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return nil, true
	}
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		raw, present := fields[key]
		if !present {
			continue
		}
		var deps map[string]json.RawMessage
		if json.Unmarshal(raw, &deps) != nil {
			return nil, true
		}
		value, found := deps["express"]
		if !found {
			continue
		}
		stop = true
		var text string
		if json.Unmarshal(value, &text) != nil {
			return nil, true
		}
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "catalog:") {
			text = pnpmCatalog(r, path.Dir(manifest), strings.TrimPrefix(text, "catalog:"))
		}
		ranges = append(ranges, text)
	}
	if stop && len(ranges) == 0 {
		return nil, true
	}
	return ranges, stop
}

// pnpmCatalog reads the express entry of a pnpm catalog from the nearest
// pnpm-workspace.yaml at or above dir: the default catalog (`catalog:` or
// `catalog:default`, the top-level `catalog` map or `catalogs.default`) or
// a named one (`catalogs.<name>`). It returns "" (an unknown range) when
// no file, catalog or entry is found or the file does not parse.
func pnpmCatalog(r *resolver, dir, name string) string {
	for d := dir; ; d = path.Dir(d) {
		if data, ok := r.modules.Source(joinDir(d, "pnpm-workspace.yaml")); ok {
			var workspace struct {
				Catalog  map[string]string            `yaml:"catalog"`
				Catalogs map[string]map[string]string `yaml:"catalogs"`
			}
			if yaml.Unmarshal(data, &workspace) != nil {
				return ""
			}
			if name == "" || name == "default" {
				if v, ok := workspace.Catalog["express"]; ok {
					return strings.TrimSpace(v)
				}
				name = "default"
			}
			return strings.TrimSpace(workspace.Catalogs[name]["express"])
		}
		if d == "." || d == "/" {
			return ""
		}
	}
}

func majorOfRanges(ranges []string) int {
	major := expressMajorUnknown
	for _, v := range ranges {
		m := expressMajorRange.FindStringSubmatch(v)
		if m == nil {
			return expressMajorUnknown
		}
		n := expressMajorUnknown
		switch m[1] {
		case "4":
			n = expressMajor4
		case "5":
			n = expressMajor5
		default:
			return expressMajorUnknown
		}
		if major != expressMajorUnknown && major != n {
			return expressMajorUnknown
		}
		major = n
	}
	return major
}

// refusalSeverity is a refusal's diagnostic severity: a pattern Express 5
// throws on is a warning (the application fails at startup), an unmodelled
// one is info, as the other route_* path diagnostics.
func refusalSeverity(code string) string {
	if code == routePatternInvalid {
		return "warning"
	}
	return "info"
}

// uniqueSorted returns the distinct values in order.
func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

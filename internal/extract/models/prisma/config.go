package prisma

import (
	"path"
	"strings"
)

// Config file candidates in a package directory, in the order the Prisma
// CLI tries them (prisma/prisma packages/config/src/loadConfigFromFile.ts
// at 7.10.0, lines 16-32; 7.9.1 tries the legacy names in the same order
// through c12). prisma7Configs exist from 7.10.0 on and come first.
var (
	configExtensions = []string{".js", ".ts", ".mjs", ".cjs", ".mts", ".cts"}
	prisma7Configs   []string
	legacyConfigs    []string
)

func init() {
	for _, ext := range configExtensions {
		prisma7Configs = append(prisma7Configs, "prisma7.config"+ext)
	}
	for _, ext := range configExtensions {
		prisma7Configs = append(prisma7Configs, ".config/prisma7"+ext)
	}
	for _, base := range []string{"prisma.config", ".config/prisma", ".config/prisma.config"} {
		for _, ext := range configExtensions {
			legacyConfigs = append(legacyConfigs, base+ext)
		}
		for _, ext := range configExtensions {
			legacyConfigs = append(legacyConfigs, base+"/index"+ext)
		}
	}
}

// firstConfig returns the first candidate (relative to dir) that is a
// file of sources, or "".
func firstConfig(sources map[string][]byte, dir string, candidates []string) string {
	for _, c := range candidates {
		p := path.Join(dir, c)
		if _, ok := sources[p]; ok {
			return p
		}
	}
	return ""
}

// Results of reading a config's schema or migrations.path setting.
const (
	// configAbsent: the exported object is read and has no such key.
	configAbsent = iota
	// configLiteral: the setting is one string literal.
	configLiteral
	// configNotLiteral: the setting (or a spread or computed key that may
	// set it) is not a literal.
	configNotLiteral
	// configUnreadable: no `export default defineConfig({...})` or
	// `export default {...}` the reader can read.
	configUnreadable
)

// readConfigSchema reads the schema setting of a Prisma config file: the
// object literal of `export default defineConfig({...})` or `export
// default {...}` (optionally followed by `satisfies ...` or `as ...`). It
// never runs the file; any other shape is unreadable, and a schema value
// that is not one plain string literal (quotes, or a template without
// substitutions) is not literal.
func readConfigSchema(src string) (int, string) {
	toks, open, ok := configObject(src)
	if !ok {
		return configUnreadable, ""
	}
	state, at, _ := objectMember(toks, open, "schema")
	return literalMember(toks, state, at)
}

// readConfigMigrations reads `migrations: { path: "..." }` of a Prisma
// config file the way readConfigSchema reads schema: the migrations value
// must be an object literal and its path one plain string literal; a
// migrations value of any other shape, or a spread or computed key in
// either object, is not literal.
func readConfigMigrations(src string) (int, string) {
	toks, open, ok := configObject(src)
	if !ok {
		return configUnreadable, ""
	}
	state, at, _ := objectMember(toks, open, "migrations")
	if state != memberValue {
		return literalMember(toks, state, at)
	}
	if token1(toks, at).text != "{" {
		return configNotLiteral, ""
	}
	inner, pathAt, end := objectMember(toks, at, "path")
	if inner == memberUnbalanced {
		return configUnreadable, ""
	}
	if after := token1(toks, end+1).text; after != "," && after != "}" {
		return configNotLiteral, ""
	}
	return literalMember(toks, inner, pathAt)
}

// configObject tokenizes a config and finds the `{` of its exported object
// literal; ok is false when the file has no single `export default` of
// `defineConfig({...})` or `{...}`.
func configObject(src string) (toks []jsToken, open int, ok bool) {
	toks, ok = jsTokens(src)
	if !ok {
		return nil, 0, false
	}
	start := -1
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].text == "export" && toks[i+1].text == "default" {
			if start >= 0 {
				return nil, 0, false
			}
			start = i + 2
		}
	}
	if start < 0 || start >= len(toks) {
		return nil, 0, false
	}
	i := start
	call := false
	if toks[i].text == "defineConfig" && i+1 < len(toks) && toks[i+1].text == "(" {
		call = true
		i += 2
	}
	if i >= len(toks) || toks[i].text != "{" {
		return nil, 0, false
	}
	state, _, end := objectMember(toks, i, "")
	if state == memberUnbalanced || call && token1(toks, end+1).text != ")" {
		return nil, 0, false
	}
	return toks, i, true
}

// Results of objectMember.
const (
	memberAbsent = iota
	// memberValue: the key's value starts at the returned index.
	memberValue
	// memberNotLiteral: a spread, a computed key, a shorthand, method or
	// getter may set the key.
	memberNotLiteral
	// memberUnbalanced: the object is not closed.
	memberUnbalanced
)

// objectMember walks the top-level members of the object literal whose `{`
// is toks[open] and reports the last member that may set key (as in
// JavaScript, a later member wins) and the index of the object's closing
// `}`. An empty key matches no member (the walk only finds the end).
func objectMember(toks []jsToken, open int, key string) (state, at, end int) {
	state, at = memberAbsent, -1
	depth := 0
	member := true // at the start of a member
	for i := open; i < len(toks); i++ {
		t := toks[i]
		switch t.text {
		case "{", "(", "[":
			depth++
			if t.text == "[" && depth == 2 && member {
				// A computed key may be key.
				state, at = memberNotLiteral, -1
			}
			// The object's own brace starts its first member.
			member = depth == 1
			continue
		case "}", ")", "]":
			depth--
			if depth == 0 {
				return state, at, i
			}
			member = false
			continue
		case ",":
			if depth == 1 {
				member = true
				continue
			}
		}
		if depth != 1 || !member {
			member = false
			continue
		}
		member = false
		if t.text == "..." {
			state, at = memberNotLiteral, -1
			continue
		}
		name := t.text
		if t.str {
			name = t.value
		}
		if key == "" || name != key {
			continue
		}
		if token1(toks, i+1).text != ":" {
			// Shorthand, a method `key() {}`, a getter.
			state, at = memberNotLiteral, -1
			continue
		}
		state, at = memberValue, i+2
	}
	return memberUnbalanced, -1, -1
}

// literalMember maps an objectMember result to a config result: a value
// that is one plain string literal followed by the member's end is
// literal.
func literalMember(toks []jsToken, state, at int) (int, string) {
	switch state {
	case memberAbsent:
		return configAbsent, ""
	case memberUnbalanced:
		return configUnreadable, ""
	case memberNotLiteral:
		return configNotLiteral, ""
	}
	val, after := token1(toks, at), token1(toks, at+1)
	if val.str && !val.bad && (after.text == "," || after.text == "}") {
		return configLiteral, val.value
	}
	return configNotLiteral, ""
}

type jsToken struct {
	text  string
	str   bool
	value string
	bad   bool
}

func token1(toks []jsToken, i int) jsToken {
	if i < len(toks) {
		return toks[i]
	}
	return jsToken{}
}

// jsTokens splits JavaScript or TypeScript source into the tokens the
// config reader needs: identifiers and words, strings (with their value),
// `...` and single punctuation characters; comments are dropped. A regular
// expression literal is not recognised (configs do not use them where the
// reader looks). It reports false for an unterminated string or comment.
func jsTokens(src string) ([]jsToken, bool) {
	var out []jsToken
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
		case c == '\'' || c == '"' || c == '`':
			t, n := jsString(src[i:])
			if n == 0 {
				return nil, false
			}
			out = append(out, t)
			i += n
		case strings.HasPrefix(src[i:], "..."):
			out = append(out, jsToken{text: "..."})
			i += 3
		case c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80:
			j := i
			for j < len(src) && (src[j] == '_' || src[j] == '$' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9' || src[j] >= 0x80) {
				j++
			}
			out = append(out, jsToken{text: src[i:j]})
			i = j
		default:
			out = append(out, jsToken{text: string(c)})
			i++
		}
	}
	return out, true
}

// jsString reads a string at the start of s and returns it with its
// length (0 when unterminated). Escapes other than \\, \', \", \/ and a
// template substitution make the token bad (not a plain literal).
func jsString(s string) (jsToken, int) {
	q := s[0]
	t := jsToken{str: true}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == q:
			t.value, t.text = b.String(), s[:i+1]
			return t, i + 1
		case c == '\\' && i+1 < len(s):
			i++
			switch s[i] {
			case '\\', '\'', '"', '/', '`':
				b.WriteByte(s[i])
			default:
				t.bad = true
			}
		case c == '\n' && q != '`':
			return t, 0
		case q == '`' && c == '$' && i+1 < len(s) && s[i+1] == '{':
			t.bad = true
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return t, 0
}

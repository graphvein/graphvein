package drizzle

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// The text helpers of the claim pass: drizzle-orm's casing rule (row
// DR-K2), and the texts drizzle-kit renders for defaults (row DR-C2,
// bin.cjs:17021-17045 for postgresql, :18611-18617 for sqlite) as far as
// they can be computed from the syntax without running it. A helper that
// cannot reproduce JavaScript's output exactly returns ok false, and the
// caller then claims at most the default's presence.

// casingWords splits a name into words as drizzle-orm's toSnakeCase and
// toCamelCase do (drizzle-orm/casing.js:3-13): quotes ' and U+2019 are
// removed, then the matches of /[\da-z]+|[A-Z]+(?![a-z])|[A-Z][\da-z]+/g
// are the words. RE2 has no lookahead, so the alternation is scanned by
// hand: at each position the first alternative that matches wins, and a
// position where none matches is skipped.
func casingWords(input string) []string {
	s := strings.NewReplacer("'", "", "’", "").Replace(input)
	lowerDigit := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }
	upper := func(c byte) bool { return c >= 'A' && c <= 'Z' }
	var words []string
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case lowerDigit(c):
			j := i
			for j < len(s) && lowerDigit(s[j]) {
				j++
			}
			words = append(words, s[i:j])
			i = j
			continue
		case upper(c):
			// [A-Z]+(?![a-z]): the longest uppercase run that a lowercase
			// letter does not follow, backtracking one letter at a time.
			n := 0
			for i+n < len(s) && upper(s[i+n]) {
				n++
			}
			matched := false
			for k := n; k >= 1; k-- {
				if i+k >= len(s) || !(s[i+k] >= 'a' && s[i+k] <= 'z') {
					words = append(words, s[i:i+k])
					i += k
					matched = true
					break
				}
			}
			if matched {
				continue
			}
			// [A-Z][\da-z]+
			if i+1 < len(s) && lowerDigit(s[i+1]) {
				j := i + 1
				for j < len(s) && lowerDigit(s[j]) {
					j++
				}
				words = append(words, s[i:j])
				i = j
				continue
			}
		}
		i++
	}
	return words
}

// convertCasing applies a config's casing to an omitted column name (the
// property key): "snake_case" joins the lowercased words with "_",
// "camelCase" lowercases the first word and uppercases the first letter
// of the others (drizzle-orm/casing.js:3-13).
func convertCasing(casing, key string) string {
	words := casingWords(key)
	switch casing {
	case "snake_case":
		for i, w := range words {
			words[i] = strings.ToLower(w)
		}
		return strings.Join(words, "_")
	case "camelCase":
		var b strings.Builder
		for i, w := range words {
			if i == 0 {
				b.WriteString(strings.ToLower(w))
				continue
			}
			b.WriteString(strings.ToUpper(w[:1]) + w[1:])
		}
		return b.String()
	}
	return key
}

// plainNumber is a number literal whose JavaScript string form is its
// source text (no exponent, no leading or trailing zeros, no hex), small
// enough to be exact in a double.
var plainNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,14})(\.[0-9]{0,5}[1-9])?$`)

// jsNumber returns the text JavaScript prints for a number literal of the
// payload (the source text with its sign), ok false when it could differ.
func jsNumber(v string) (string, bool) {
	v = strings.TrimPrefix(v, "+")
	if !plainNumber.MatchString(v) || v == "-0" || strings.HasPrefix(v, "-0.") && strings.Trim(v[3:], "0") == "" {
		return "", false
	}
	return v, true
}

// jsonStringify returns JSON.stringify of a literal value, ok false when
// the value is not a literal the payload states exactly (an identifier, a
// spread, a method, a key JavaScript would reorder or that is written
// twice).
func jsonStringify(e typescript.ModelExpr) (string, bool) {
	var b strings.Builder
	if !writeJSON(&b, e, 0) {
		return "", false
	}
	return b.String(), true
}

// arrayIndexKey matches keys JavaScript orders before the others (array
// indexes).
var arrayIndexKey = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func writeJSON(b *strings.Builder, e typescript.ModelExpr, depth int) bool {
	if depth > 32 {
		return false
	}
	switch e.Kind {
	case typescript.ModelString, typescript.ModelTemplate:
		s, ok := literalString(e)
		if !ok {
			return false
		}
		writeJSONString(b, s)
	case typescript.ModelNumber:
		n, ok := jsNumber(e.Value)
		if !ok {
			return false
		}
		b.WriteString(n)
	case typescript.ModelBoolean:
		b.WriteString(e.Value)
	case typescript.ModelNull:
		b.WriteString("null")
	case typescript.ModelArray:
		b.WriteByte('[')
		for i, el := range e.Elements {
			if i > 0 {
				b.WriteByte(',')
			}
			if el.Kind == typescript.ModelSpread || !writeJSON(b, el, depth+1) {
				return false
			}
		}
		b.WriteByte(']')
	case typescript.ModelObject:
		seen := map[string]bool{}
		b.WriteByte('{')
		for i, en := range e.Entries {
			if en.Kind != typescript.ModelProperty || seen[en.Key] || arrayIndexKey.MatchString(en.Key) {
				return false
			}
			seen[en.Key] = true
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, en.Key)
			b.WriteByte(':')
			if !writeJSON(b, en.Value, depth+1) {
				return false
			}
		}
		b.WriteByte('}')
	default:
		return false
	}
	return true
}

// writeJSONString writes s as JSON.stringify does: quote, backslash and
// the control characters escaped (\b \f \n \r \t, else \u00xx in lowercase
// hex); everything else as is.
func writeJSONString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// pgArrayString is drizzle-kit's buildArrayString (bin.cjs:16849-16872)
// for the literal elements whose output is safe inside the SQL string it
// is written into: numbers, booleans, nested arrays, and strings without
// a quote, double quote or backslash (drizzle-kit writes strings between
// double quotes without escaping).
func pgArrayString(e typescript.ModelExpr, depth int) (string, bool) {
	if e.Kind != typescript.ModelArray || depth > 8 {
		return "", false
	}
	parts := make([]string, 0, len(e.Elements))
	for _, el := range e.Elements {
		switch el.Kind {
		case typescript.ModelNumber:
			n, ok := jsNumber(el.Value)
			if !ok {
				return "", false
			}
			parts = append(parts, n)
		case typescript.ModelBoolean:
			parts = append(parts, el.Value)
		case typescript.ModelArray:
			s, ok := pgArrayString(el, depth+1)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		case typescript.ModelString, typescript.ModelTemplate:
			s, ok := literalString(el)
			if !ok || strings.ContainsAny(s, `"\'`) {
				return "", false
			}
			parts = append(parts, `"`+s+`"`)
		default:
			return "", false
		}
	}
	return "{" + strings.Join(parts, ",") + "}", true
}

// sqlQuote is drizzle-kit's `'${escapeSingleQuotes(s)}'` (bin.cjs:8078).
func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// truncateIdentifier is the name Postgres stores for an identifier longer
// than 63 bytes (NAMEDATALEN - 1): cut at a character
// boundary.
func truncateIdentifier(name string) string {
	const max = 63
	if len(name) <= max {
		return name
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut]
}

// intLiteral reads a non-negative integer literal.
var smallInt = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`)

func intLiteral(e typescript.ModelExpr) (int, bool) {
	if e.Kind != typescript.ModelNumber || !smallInt.MatchString(e.Value) {
		return 0, false
	}
	n, err := strconv.Atoi(e.Value)
	return n, err == nil
}

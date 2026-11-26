package python

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// This file holds a small TOML reader for pyproject.toml (row PS-M2 of
// docs/lang/python-structure.md: uv workspace members, uv path sources and
// the hatchling wheel options). It reads TOML 1.0 documents into nested
// maps: tables, arrays of tables, dotted and quoted keys, basic and
// literal strings (multi-line included), arrays, inline tables and
// booleans. Numbers and dates are kept as tomlScalar text, since no field
// the resolution reads holds one. Anything else is an error, and a
// pyproject.toml the reader cannot read declares nothing (precision-safe:
// its workspace or member is not modelled).

// tomlScalar is a TOML number or date, kept as written.
type tomlScalar string

// tomlParser reads one document.
type tomlParser struct {
	src string
	pos int
	// kinds records how each table or array path was created (paths join
	// keys with NUL; an element of an array of tables adds its index), so
	// the reader refuses what TOML forbids: a table defined twice, a key
	// defined twice, a header extending an inline table or a static array.
	kinds map[string]tomlKind
	// err is the first error found while skipping a comment.
	err error
}

type tomlKind int

const (
	tomlImplicit      tomlKind = iota + 1 // created as the parent of a header
	tomlHeader                            // [a.b]
	tomlDotted                            // created by a dotted key a.b = 1
	tomlInline                            // { ... } or a static array
	tomlArrayOfTables                     // [[a.b]]
)

// parseTOML reads a TOML document into a map. Values are string, bool,
// tomlScalar, []any and map[string]any.
func parseTOML(src string) (map[string]any, error) {
	p := &tomlParser{src: strings.TrimPrefix(src, "\ufeff"), kinds: map[string]tomlKind{}}
	root := map[string]any{}
	current, currentPath := root, ""
	for {
		p.skipArraySpace()
		if p.err != nil {
			return nil, p.err
		}
		if p.pos >= len(p.src) {
			return root, nil
		}
		line := p.line()
		if p.src[p.pos] == '[' {
			array := strings.HasPrefix(p.src[p.pos:], "[[")
			closing := "]"
			p.pos++
			if array {
				p.pos++
				closing = "]]"
			}
			keys, err := p.key()
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			if !strings.HasPrefix(p.src[p.pos:], closing) {
				return nil, fmt.Errorf("line %d: unclosed table header", line)
			}
			p.pos += len(closing)
			if err := p.endOfLine(); err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			current, currentPath, err = p.header(root, keys, array)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", line, err)
			}
			continue
		}
		if err := p.keyValue(current, currentPath); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := p.endOfLine(); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
	}
}

func (p *tomlParser) line() int {
	return strings.Count(p.src[:p.pos], "\n") + 1
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "\x00" + key
}

// header opens the table a [a.b] or [[a.b]] header names and returns it
// with its path.
func (p *tomlParser) header(root map[string]any, keys []string, array bool) (map[string]any, string, error) {
	table, at := root, ""
	name := strings.Join(keys, ".")
	for i, k := range keys {
		last := i == len(keys)-1
		at = joinPath(at, k)
		v, exists := table[k]
		if last && array {
			if exists && p.kinds[at] != tomlArrayOfTables {
				return nil, "", fmt.Errorf("%s is not an array of tables", name)
			}
			list, _ := v.([]any)
			next := map[string]any{}
			table[k] = append(list, next)
			p.kinds[at] = tomlArrayOfTables
			element := fmt.Sprintf("%s\x00[%d]", at, len(list))
			p.kinds[element] = tomlHeader
			return next, element, nil
		}
		if !exists {
			next := map[string]any{}
			table[k] = next
			table = next
			if last {
				p.kinds[at] = tomlHeader
			} else {
				p.kinds[at] = tomlImplicit
			}
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			kind := p.kinds[at]
			if kind == tomlInline || last && (kind == tomlHeader || kind == tomlDotted) {
				return nil, "", fmt.Errorf("table %s defined twice", name)
			}
			if last {
				p.kinds[at] = tomlHeader
			}
			table = t
		case []any:
			if last || p.kinds[at] != tomlArrayOfTables {
				return nil, "", fmt.Errorf("%s is not a table", name)
			}
			at = fmt.Sprintf("%s\x00[%d]", at, len(t)-1)
			table = t[len(t)-1].(map[string]any)
		default:
			return nil, "", fmt.Errorf("%s is not a table", name)
		}
	}
	return table, at, nil
}

// keyValue reads `key = value` into table (at path at), creating the
// tables of a dotted key.
func (p *tomlParser) keyValue(table map[string]any, at string) error {
	keys, err := p.key()
	if err != nil {
		return err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '=' {
		return fmt.Errorf("expected = after key %s", strings.Join(keys, "."))
	}
	p.pos++
	p.skipSpace()
	value, err := p.value(joinPath(at, strings.Join(keys, "\x00")))
	if err != nil {
		return err
	}
	for _, k := range keys[:len(keys)-1] {
		at = joinPath(at, k)
		v, exists := table[k]
		if !exists {
			next := map[string]any{}
			table[k] = next
			p.kinds[at] = tomlDotted
			table = next
			continue
		}
		t, ok := v.(map[string]any)
		if !ok || p.kinds[at] != tomlDotted {
			return fmt.Errorf("key %s is not a table a dotted key may extend", k)
		}
		table = t
	}
	last := keys[len(keys)-1]
	if _, exists := table[last]; exists {
		return fmt.Errorf("key %s defined twice", strings.Join(keys, "."))
	}
	table[last] = value
	return nil
}

// key reads a dotted key of bare and quoted parts.
func (p *tomlParser) key() ([]string, error) {
	var keys []string
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("expected a key")
		}
		switch c := p.src[p.pos]; {
		case c == '"':
			s, err := p.basicString()
			if err != nil {
				return nil, err
			}
			keys = append(keys, s)
		case c == '\'':
			s, err := p.literalString()
			if err != nil {
				return nil, err
			}
			keys = append(keys, s)
		default:
			start := p.pos
			for p.pos < len(p.src) && bareKeyByte(p.src[p.pos]) {
				p.pos++
			}
			if p.pos == start {
				return nil, fmt.Errorf("expected a key")
			}
			keys = append(keys, p.src[start:p.pos])
		}
		p.skipSpace()
		if p.pos < len(p.src) && p.src[p.pos] == '.' {
			p.pos++
			continue
		}
		return keys, nil
	}
}

func bareKeyByte(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// value reads one value.
func (p *tomlParser) value(at string) (any, error) {
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("expected a value")
	}
	rest := p.src[p.pos:]
	switch {
	case strings.HasPrefix(rest, `"""`):
		return p.multilineString(`"""`, true)
	case strings.HasPrefix(rest, `'''`):
		return p.multilineString(`'''`, false)
	case rest[0] == '"':
		return p.basicString()
	case rest[0] == '\'':
		return p.literalString()
	case rest[0] == '[':
		p.kinds[at] = tomlInline
		return p.array(at)
	case rest[0] == '{':
		p.kinds[at] = tomlInline
		return p.inlineTable(at)
	case strings.HasPrefix(rest, "true") && !scalarByte(rest, 4):
		p.pos += 4
		return true, nil
	case strings.HasPrefix(rest, "false") && !scalarByte(rest, 5):
		p.pos += 5
		return false, nil
	}
	// A number or a date: digits, signs, letters (inf, nan, e, T, Z),
	// `:`, `.`, `_` and one space inside a date-time.
	start := p.pos
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.IndexByte("+-:._", c) >= 0 {
			p.pos++
			continue
		}
		if c == ' ' && p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9' && strings.Contains(p.src[start:p.pos], "-") {
			p.pos++
			continue
		}
		break
	}
	lexeme := p.src[start:p.pos]
	if p.pos == start || !validScalar(lexeme) {
		return nil, fmt.Errorf("unsupported value %q", lexeme)
	}
	return tomlScalar(lexeme), nil
}

// TOML 1.0 integer, float and date-time lexemes.
var (
	tomlInteger = regexp.MustCompile(`^(?:[+-]?(?:0|[1-9](?:_?[0-9])*)|0x[0-9A-Fa-f](?:_?[0-9A-Fa-f])*|0o[0-7](?:_?[0-7])*|0b[01](?:_?[01])*)$`)
	tomlFloat   = regexp.MustCompile(`^[+-]?(?:(?:0|[1-9](?:_?[0-9])*)(?:\.[0-9](?:_?[0-9])*)?(?:[eE][+-]?[0-9](?:_?[0-9])*)?|inf|nan)$`)
	tomlDate    = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})(?:[Tt ]([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.[0-9]+)?(?:[Zz]|[+-]([0-9]{2}):([0-9]{2}))?)?$`)
	tomlTime    = regexp.MustCompile(`^([0-9]{2}):([0-9]{2}):([0-9]{2})(?:\.[0-9]+)?$`)
)

// validScalar reports whether s is a TOML 1.0 number or date-time with
// valid field ranges (what tomllib accepts).
func validScalar(s string) bool {
	if tomlInteger.MatchString(s) {
		return true
	}
	if tomlFloat.MatchString(s) {
		return strings.ContainsAny(s, ".eE") || strings.HasSuffix(s, "inf") || strings.HasSuffix(s, "nan")
	}
	num := func(x string) int { n, _ := strconv.Atoi(x); return n }
	clock := func(h, m, sec string) bool { return num(h) < 24 && num(m) < 60 && num(sec) < 60 }
	if m := tomlTime.FindStringSubmatch(s); m != nil {
		return clock(m[1], m[2], m[3])
	}
	m := tomlDate.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	y, mo, d := num(m[1]), num(m[2]), num(m[3])
	if t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC); mo < 1 || mo > 12 || d < 1 || t.Day() != d {
		return false
	}
	if m[4] != "" && !clock(m[4], m[5], m[6]) {
		return false
	}
	if m[7] != "" && (num(m[7]) > 23 || num(m[8]) > 59) {
		return false
	}
	return true
}

func scalarByte(s string, i int) bool {
	return i < len(s) && bareKeyByte(s[i])
}

// array reads [v, v, ...] over several lines, with comments.
func (p *tomlParser) array(at string) ([]any, error) {
	p.pos++
	out := []any{}
	for {
		p.skipArraySpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unclosed array")
		}
		if p.src[p.pos] == ']' {
			p.pos++
			return out, nil
		}
		v, err := p.value(fmt.Sprintf("%s\x00[%d]", at, len(out)))
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipArraySpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unclosed array")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return out, nil
		default:
			return nil, fmt.Errorf("expected , or ] in an array")
		}
	}
}

// inlineTable reads { k = v, ... } on one line.
func (p *tomlParser) inlineTable(at string) (map[string]any, error) {
	p.pos++
	out := map[string]any{}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return out, nil
	}
	for {
		if err := p.keyValue(out, at); err != nil {
			return nil, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unclosed inline table")
		}
		switch p.src[p.pos] {
		case ',':
			p.pos++
			p.skipSpace()
		case '}':
			p.pos++
			return out, nil
		default:
			return nil, fmt.Errorf("expected , or } in an inline table")
		}
	}
}

// basicString reads "..." with TOML escapes.
func (p *tomlParser) basicString() (string, error) {
	p.pos++
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '"':
			p.pos++
			return b.String(), nil
		case c == '\\':
			if err := p.escape(&b); err != nil {
				return "", err
			}
		case c == '\n' || c == '\r':
			return "", fmt.Errorf("line end in a string")
		case c < 0x20 && c != '\t' || c == 0x7f:
			return "", fmt.Errorf("control character in a string")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return "", fmt.Errorf("unclosed string")
}

// literalString reads '...' without escapes.
func (p *tomlParser) literalString() (string, error) {
	p.pos++
	end := strings.IndexAny(p.src[p.pos:], "'\n")
	if end < 0 || p.src[p.pos+end] != '\'' {
		return "", fmt.Errorf("unclosed literal string")
	}
	s := p.src[p.pos : p.pos+end]
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 && s[i] != '\t' || s[i] == 0x7f {
			return "", fmt.Errorf("control character in a string")
		}
	}
	p.pos += end + 1
	return s, nil
}

// multilineString reads """...""" (escapes, line-ending backslash) or
// ”'...”' (as written). A line end right after the opening is dropped;
// up to two quotes before the closing belong to the string.
func (p *tomlParser) multilineString(quote string, escapes bool) (string, error) {
	p.pos += 3
	if strings.HasPrefix(p.src[p.pos:], "\r\n") {
		p.pos += 2
	} else if strings.HasPrefix(p.src[p.pos:], "\n") {
		p.pos++
	}
	var b strings.Builder
	for p.pos < len(p.src) {
		if strings.HasPrefix(p.src[p.pos:], quote) {
			extra := 0
			for extra < 2 && p.pos+3+extra < len(p.src) && p.src[p.pos+3+extra] == quote[0] {
				extra++
			}
			b.WriteString(p.src[p.pos : p.pos+extra])
			p.pos += 3 + extra
			return b.String(), nil
		}
		c := p.src[p.pos]
		if escapes && c == '\\' {
			// A backslash before white space and a line end trims all
			// white space up to the next other character.
			j := p.pos + 1
			for j < len(p.src) && (p.src[j] == ' ' || p.src[j] == '\t') {
				j++
			}
			if j < len(p.src) && (p.src[j] == '\n' || strings.HasPrefix(p.src[j:], "\r\n")) {
				for j < len(p.src) && (strings.IndexByte(" \t\n", p.src[j]) >= 0 || strings.HasPrefix(p.src[j:], "\r\n")) {
					j++
				}
				p.pos = j
				continue
			}
			if err := p.escape(&b); err != nil {
				return "", err
			}
			continue
		}
		if c == '\r' {
			// CRLF reads as LF; a CR not followed by LF is an error, as
			// everywhere in TOML (tomllib).
			if !strings.HasPrefix(p.src[p.pos:], "\r\n") {
				return "", fmt.Errorf("line %d: a CR without LF in a string", p.line())
			}
			p.pos++
			continue
		}
		if c < 0x20 && c != '\t' && c != '\n' || c == 0x7f {
			return "", fmt.Errorf("control character in a string")
		}
		b.WriteByte(c)
		p.pos++
	}
	return "", fmt.Errorf("unclosed multi-line string")
}

// escape reads one escape sequence at p.pos (a backslash).
func (p *tomlParser) escape(b *strings.Builder) error {
	if p.pos+1 >= len(p.src) {
		return fmt.Errorf("unclosed string")
	}
	c := p.src[p.pos+1]
	simple := map[byte]byte{'b': '\b', 't': '\t', 'n': '\n', 'f': '\f', 'r': '\r', '"': '"', '\\': '\\'}
	if r, ok := simple[c]; ok {
		b.WriteByte(r)
		p.pos += 2
		return nil
	}
	n := 0
	switch c {
	case 'u':
		n = 4
	case 'U':
		n = 8
	default:
		return fmt.Errorf("unknown escape \\%c", c)
	}
	if p.pos+2+n > len(p.src) {
		return fmt.Errorf("short unicode escape")
	}
	v, err := strconv.ParseUint(p.src[p.pos+2:p.pos+2+n], 16, 32)
	if err != nil || !utf8.ValidRune(rune(v)) {
		return fmt.Errorf("invalid unicode escape")
	}
	b.WriteRune(rune(v))
	p.pos += 2 + n
	return nil
}

func (p *tomlParser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

// skipArraySpace skips white space, line ends and comments inside an
// array.
func (p *tomlParser) skipArraySpace() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n':
			p.pos++
		case '\r':
			// Only CRLF ends a line; a lone CR is an error (tomllib).
			if !strings.HasPrefix(p.src[p.pos:], "\r\n") {
				return
			}
			p.pos++
		case '#':
			p.skipComment()
		default:
			return
		}
	}
}

// skipComment skips a comment up to its line end; a control character
// other than tab in it (a lone CR included) is an error, as in TOML 1.0.
func (p *tomlParser) skipComment() {
	for p.pos < len(p.src) && p.src[p.pos] != '\n' {
		c := p.src[p.pos]
		if (c < 0x20 && c != '\t' || c == 0x7f) && !strings.HasPrefix(p.src[p.pos:], "\r\n") && p.err == nil {
			p.err = fmt.Errorf("line %d: control character in a comment", p.line())
		}
		if strings.HasPrefix(p.src[p.pos:], "\r\n") {
			p.pos++
			continue
		}
		p.pos++
	}
}

// endOfLine accepts white space and a comment up to the line end.
func (p *tomlParser) endOfLine() error {
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '#' {
		p.skipComment()
	}
	if p.pos >= len(p.src) {
		return nil
	}
	if strings.HasPrefix(p.src[p.pos:], "\r\n") {
		p.pos += 2
		return nil
	}
	if p.src[p.pos] == '\n' {
		p.pos++
		return nil
	}
	return fmt.Errorf("unexpected text after a value")
}

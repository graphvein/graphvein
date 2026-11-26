package schema

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// tokenKind classifies the lexemes the recognizer needs. Comments and
// whitespace produce no token; their bytes only separate tokens.
type tokenKind uint8

const (
	// tokIdent is an unquoted identifier or keyword; value is folded (P2).
	tokIdent tokenKind = iota
	// tokQuoted is a "..." or U&"..." identifier; value is unescaped and
	// truncated (P2).
	tokQuoted
	// tokString is any string constant: '...', E'...', B'...', X'...',
	// N'...', U&'...' or a dollar-quoted string; value is its content.
	tokString
	tokNumber
	// tokParam is a positional parameter such as $1.
	tokParam
	// tokOp is a run of operator characters, including "::".
	tokOp
	// tokPunct is one of ( ) [ ] , ; . :
	tokPunct
)

// token is one lexeme with its half-open byte range in the normalized source.
type token struct {
	kind       tokenKind
	start, end int
	value      string
}

// statement is the tokens between two top-level semicolons. start and end
// are the byte span from the first token to just after the terminating
// semicolon (or the last token when the file ends without one).
type statement struct {
	tokens     []token
	start, end int
}

// lexFailure marks the first byte of a file region the lexer could not
// read: an unterminated string, quoted identifier, comment or dollar quote.
// Everything from start to the end of the file is one opaque region.
type lexFailure struct {
	start  int
	reason string
}

// nameDataLen is PostgreSQL's NAMEDATALEN; identifiers keep at most
// nameDataLen-1 bytes (scansup.c truncate_identifier).
const nameDataLen = 64

// foldIdentifier applies scansup.c downcase_identifier for a UTF-8
// database: ASCII A-Z are lowered, other bytes kept, then the result is
// truncated to 63 bytes at a character boundary.
func foldIdentifier(raw string) string {
	b := []byte(raw)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return truncateIdentifier(string(b))
}

// truncateIdentifier is scansup.c truncate_identifier with pg_mbcliplen:
// at most 63 bytes, never splitting a UTF-8 character.
func truncateIdentifier(s string) string {
	if len(s) < nameDataLen {
		return s
	}
	cut := 0
	for cut < len(s) {
		_, size := utf8.DecodeRuneInString(s[cut:])
		if cut+size > nameDataLen-1 {
			break
		}
		cut += size
	}
	return s[:cut]
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

// identStart and identCont follow scan.l ident_start and ident_cont; any
// byte with the high bit set belongs to an identifier.
func identStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80
}

func identCont(c byte) bool {
	return identStart(c) || c >= '0' && c <= '9' || c == '$'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// opChars is scan.l self plus op_chars that can form operators.
const opChars = "~!@#^&|`?+-*/%<>="

// lexStatements splits a migration into statements per P1 (scan.l):
// "--" line comments, nested block comments, standard strings with ”
// (standard_conforming_strings on), E” escapes, dollar quotes, quoted
// identifiers with "", U& forms. A semicolon outside all of these ends a
// statement, except inside the BEGIN ATOMIC ... END body of a SQL-standard
// function, where the grammar (not the lexer) consumes semicolons.
func lexStatements(src []byte) ([]statement, *lexFailure) {
	var out []statement
	var current []token
	stmtStart := -1
	atomic := 0 // nesting depth inside BEGIN ATOMIC bodies
	flush := func(end int) {
		if len(current) > 0 {
			out = append(out, statement{tokens: current, start: stmtStart, end: end})
		}
		current, stmtStart, atomic = nil, -1, 0
	}
	add := func(t token) {
		if stmtStart < 0 {
			stmtStart = t.start
		}
		current = append(current, t)
	}
	fail := func(at int, reason string) *lexFailure {
		start := at
		if stmtStart >= 0 {
			start = stmtStart
		}
		return &lexFailure{start: start, reason: reason}
	}
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case isSpace(c):
			i++
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end, ok := skipBlockComment(src, i)
			if !ok {
				return out, fail(i, "unterminated block comment")
			}
			i = end
		case c == '\'':
			end, value, ok := scanString(src, i, false)
			if !ok {
				return out, fail(i, "unterminated string")
			}
			add(token{kind: tokString, start: i, end: end, value: value})
			i = end
		case (c == 'e' || c == 'E') && i+1 < len(src) && src[i+1] == '\'':
			end, value, ok := scanString(src, i+1, true)
			if !ok {
				return out, fail(i, "unterminated escape string")
			}
			if !escapesValid(src[i+2 : end-1]) {
				return out, fail(i, "an escape string with an escape the scanner refuses or the pass does not verify (scan.l xe: Unicode, octal and hexadecimal escapes)")
			}
			add(token{kind: tokString, start: i, end: end, value: value})
			i = end
		case (c == 'b' || c == 'B' || c == 'x' || c == 'X' || c == 'n' || c == 'N') && i+1 < len(src) && src[i+1] == '\'':
			end, value, ok := scanString(src, i+1, false)
			if !ok {
				return out, fail(i, "unterminated string")
			}
			add(token{kind: tokString, start: i, end: end, value: value})
			i = end
		case (c == 'u' || c == 'U') && i+2 < len(src) && src[i+1] == '&' && (src[i+2] == '\'' || src[i+2] == '"'):
			if src[i+2] == '\'' {
				end, value, ok := scanString(src, i+2, false)
				if !ok {
					return out, fail(i, "unterminated Unicode string")
				}
				if !unicodeEscapesValid(src[i+3:end-1]) || uescapeFollows(src, end) {
					return out, fail(i, "a Unicode string with an escape the scanner refuses or the pass does not verify (scan.l xus, UESCAPE)")
				}
				add(token{kind: tokString, start: i, end: end, value: value})
				i = end
				continue
			}
			end, value, ok := scanQuoted(src, i+2)
			if !ok {
				return out, fail(i, "unterminated quoted identifier")
			}
			if !unicodeEscapesValid(src[i+3:end-1]) || uescapeFollows(src, end) {
				return out, fail(i, "a Unicode identifier with an escape the scanner refuses or the pass does not verify (scan.l xui, UESCAPE)")
			}
			add(token{kind: tokQuoted, start: i, end: end, value: truncateIdentifier(unicodeEscapes(value))})
			i = end
		case c == '"':
			end, value, ok := scanQuoted(src, i)
			if !ok {
				return out, fail(i, "unterminated quoted identifier")
			}
			if value == "" {
				return out, fail(i, "zero-length quoted identifier")
			}
			add(token{kind: tokQuoted, start: i, end: end, value: truncateIdentifier(value)})
			i = end
		case c == '$' && i+1 < len(src) && isDigit(src[i+1]):
			j := i + 1
			for j < len(src) && isDigit(src[j]) {
				j++
			}
			add(token{kind: tokParam, start: i, end: j, value: string(src[i:j])})
			i = j
		case c == '$':
			delim, ok := dollarDelimiter(src, i)
			if !ok {
				add(token{kind: tokOp, start: i, end: i + 1, value: "$"})
				i++
				continue
			}
			body := i + len(delim)
			close := strings.Index(string(src[body:]), delim)
			if close < 0 {
				return out, fail(i, "unterminated dollar-quoted string")
			}
			end := body + close + len(delim)
			add(token{kind: tokString, start: i, end: end, value: string(src[body : body+close])})
			i = end
		case identStart(c):
			j := i + 1
			for j < len(src) && identCont(src[j]) {
				j++
			}
			t := token{kind: tokIdent, start: i, end: j, value: foldIdentifier(string(src[i:j]))}
			add(t)
			atomic = trackAtomic(current, atomic)
			i = j
		case isDigit(c) || c == '.' && i+1 < len(src) && isDigit(src[i+1]):
			j := i + 1
			for j < len(src) && (isDigit(src[j]) || identStart(src[j]) && src[j] < 0x80 || src[j] == '.' && !(j+1 < len(src) && src[j+1] == '.')) {
				if (src[j] == 'e' || src[j] == 'E') && j+1 < len(src) && (src[j+1] == '+' || src[j+1] == '-') {
					j++
				}
				j++
			}
			add(token{kind: tokNumber, start: i, end: j, value: string(src[i:j])})
			i = j
		case c == ';':
			if atomic > 0 {
				add(token{kind: tokPunct, start: i, end: i + 1, value: ";"})
				i++
				continue
			}
			flush(i + 1)
			i++
		case c == ':' && i+1 < len(src) && src[i+1] == ':':
			add(token{kind: tokOp, start: i, end: i + 2, value: "::"})
			i += 2
		case strings.IndexByte("()[],.:", c) >= 0:
			add(token{kind: tokPunct, start: i, end: i + 1, value: string(c)})
			i++
		case strings.IndexByte(opChars, c) >= 0:
			j := i + 1
			for j < len(src) && strings.IndexByte(opChars, src[j]) >= 0 {
				// An embedded comment start ends the operator (scan.l operator rule).
				if src[j] == '-' && j+1 < len(src) && src[j+1] == '-' || src[j] == '/' && j+1 < len(src) && src[j+1] == '*' {
					break
				}
				j++
			}
			add(token{kind: tokOp, start: i, end: j, value: string(src[i:j])})
			i = j
		default:
			add(token{kind: tokOp, start: i, end: i + 1, value: string(c)})
			i++
		}
	}
	if len(current) > 0 {
		flush(current[len(current)-1].end)
	}
	return out, nil
}

// trackAtomic follows BEGIN ATOMIC bodies of CREATE FUNCTION/PROCEDURE: the
// body ends at the END that closes it, with CASE ... END nested inside.
func trackAtomic(tokens []token, depth int) int {
	n := len(tokens)
	last := tokens[n-1]
	if depth == 0 {
		if n >= 2 && last.value == "atomic" && tokens[n-2].kind == tokIdent && tokens[n-2].value == "begin" && routineStatement(tokens) {
			return 1
		}
		return 0
	}
	switch last.value {
	case "case":
		return depth + 1
	case "end":
		return depth - 1
	}
	return depth
}

// routineStatement reports whether tokens start CREATE [OR REPLACE]
// FUNCTION or PROCEDURE.
func routineStatement(tokens []token) bool {
	words := []string{}
	for _, t := range tokens {
		if t.kind != tokIdent || len(words) == 4 {
			break
		}
		words = append(words, t.value)
	}
	if len(words) < 2 || words[0] != "create" {
		return false
	}
	k := 1
	if len(words) >= 3 && words[1] == "or" && words[2] == "replace" {
		k = 3
	}
	return k < len(words) && (words[k] == "function" || words[k] == "procedure")
}

func skipBlockComment(src []byte, i int) (int, bool) {
	depth := 0
	for i < len(src) {
		if src[i] == '/' && i+1 < len(src) && src[i+1] == '*' {
			depth++
			i += 2
			continue
		}
		if src[i] == '*' && i+1 < len(src) && src[i+1] == '/' {
			depth--
			i += 2
			if depth == 0 {
				return i, true
			}
			continue
		}
		i++
	}
	return i, false
}

// scanString reads a quoted string starting at the quote at i. With escape
// set (E”), a backslash escapes the next byte.
func scanString(src []byte, i int, escape bool) (int, string, bool) {
	var b strings.Builder
	j := i + 1
	for j < len(src) {
		c := src[j]
		if escape && c == '\\' && j+1 < len(src) {
			b.WriteByte(src[j+1])
			j += 2
			continue
		}
		if c == '\'' {
			if j+1 < len(src) && src[j+1] == '\'' {
				b.WriteByte('\'')
				j += 2
				continue
			}
			return j + 1, b.String(), true
		}
		b.WriteByte(c)
		j++
	}
	return j, "", false
}

// scanQuoted reads a "..." identifier starting at the quote at i.
func scanQuoted(src []byte, i int) (int, string, bool) {
	var b strings.Builder
	j := i + 1
	for j < len(src) {
		if src[j] == '"' {
			if j+1 < len(src) && src[j+1] == '"' {
				b.WriteByte('"')
				j += 2
				continue
			}
			return j + 1, b.String(), true
		}
		b.WriteByte(src[j])
		j++
	}
	return j, "", false
}

// dollarDelimiter returns the $tag$ starting at i (scan.l dolqdelim).
func dollarDelimiter(src []byte, i int) (string, bool) {
	j := i + 1
	if j < len(src) && src[j] == '$' {
		return "$$", true
	}
	if j >= len(src) || !(identStart(src[j])) {
		return "", false
	}
	j++
	for j < len(src) && (identStart(src[j]) || isDigit(src[j])) {
		j++
	}
	if j < len(src) && src[j] == '$' {
		return string(src[i : j+1]), true
	}
	return "", false
}

// unicodeEscapes decodes the default \XXXX and \+XXXXXX escapes of a U&""
// identifier. A custom UESCAPE character is not applied; such names keep
// their written escapes, which only affects names no corpus uses.
func unicodeEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '\\' {
			b.WriteByte('\\')
			i++
			continue
		}
		width := 4
		start := i + 1
		if start < len(s) && s[start] == '+' {
			width, start = 6, start+1
		}
		if start+width <= len(s) {
			if r, err := strconv.ParseUint(s[start:start+width], 16, 32); err == nil {
				b.WriteRune(rune(r))
				i = start + width - 1
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// describe names a byte range for diagnostics.
func describe(start, end int) string {
	return fmt.Sprintf("bytes %d-%d", start, end)
}

// codePointValid reports a Unicode escape value the scanner accepts on its
// own: not zero, not a surrogate (pairs are not verified here) and at most
// U+10FFFF (scan.l check_unicode_value, addunicode).
func codePointValid(v uint64) bool {
	return v != 0 && v <= 0x10FFFF && !(v >= 0xD800 && v <= 0xDFFF)
}

// escapesValid checks the body of an E'...' string (doubled quotes
// included) the way scan.l's xe rules read it (Revision 1.6 D2, P7): \u
// needs four and \U eight hexadecimal digits of a valid code point
// (xeunicode; xeunicodefail is an error, 22025), and an octal or
// hexadecimal escape must give a byte the UTF8 check accepts alone (not 0,
// below 0x80; multi-byte sequences built from escapes are not verified).
func escapesValid(body []byte) bool {
	isHex := func(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' || i+1 >= len(body) {
			continue
		}
		i++
		switch c := body[i]; {
		case c == 'u' || c == 'U':
			n := 4
			if c == 'U' {
				n = 8
			}
			for k := 1; k <= n; k++ {
				if i+k >= len(body) || !isHex(body[i+k]) {
					return false
				}
			}
			v, err := strconv.ParseUint(string(body[i+1:i+1+n]), 16, 32)
			if err != nil || !codePointValid(v) {
				return false
			}
			i += n
		case c >= '0' && c <= '7':
			v := 0
			k := i
			for k < len(body) && k < i+3 && body[k] >= '0' && body[k] <= '7' {
				v = v*8 + int(body[k]-'0')
				k++
			}
			if v&0xFF == 0 || v&0xFF >= 0x80 {
				return false
			}
			i = k - 1
		case c == 'x' && i+1 < len(body) && isHex(body[i+1]):
			k := i + 1
			v := 0
			for k < len(body) && k < i+3 && isHex(body[k]) {
				d, _ := strconv.ParseUint(string(body[k:k+1]), 16, 8)
				v = v*16 + int(d)
				k++
			}
			if v == 0 || v >= 0x80 {
				return false
			}
			i = k - 1
		}
	}
	return true
}

// unicodeEscapesValid checks the body of a U&'...' string or U&"..."
// identifier with the default escape character (scan.l, parser.c
// str_udeescape): \\, \XXXX or \+XXXXXX of a valid code point; any other
// backslash is an error (42601).
func unicodeEscapesValid(body []byte) bool {
	isHex := func(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		if i+1 < len(body) && body[i+1] == '\\' {
			i++
			continue
		}
		n, start := 4, i+1
		if start < len(body) && body[start] == '+' {
			n, start = 6, start+1
		}
		if start+n > len(body) {
			return false
		}
		for k := start; k < start+n; k++ {
			if !isHex(body[k]) {
				return false
			}
		}
		v, _ := strconv.ParseUint(string(body[start:start+n]), 16, 32)
		if !codePointValid(v) {
			return false
		}
		i = start + n - 1
	}
	return true
}

// uescapeFollows reports a UESCAPE clause after a Unicode string or
// identifier ending at end (another escape character, not verified).
func uescapeFollows(src []byte, end int) bool {
	j := end
	for j < len(src) {
		switch {
		case isSpace(src[j]):
			j++
		case src[j] == '-' && j+1 < len(src) && src[j+1] == '-':
			for j < len(src) && src[j] != '\n' {
				j++
			}
		case src[j] == '/' && j+1 < len(src) && src[j+1] == '*':
			e, ok := skipBlockComment(src, j)
			if !ok {
				return false
			}
			j = e
		default:
			return len(src)-j >= 7 && strings.EqualFold(string(src[j:j+7]), "uescape") && (len(src) == j+7 || !identCont(src[j+7]))
		}
	}
	return false
}

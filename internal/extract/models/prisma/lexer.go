package prisma

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Token kinds of the lexer.
const (
	tokEOF = iota
	tokNewline
	// tokWord is a run of identifier characters, dots and a leading minus
	// before a digit: an identifier, a path (db.VarChar) or a number. The
	// parser decides which by position, as Prisma's grammar does.
	tokWord
	tokString
	// tokComment is a `//`, `///` or `/* */` comment; a block comment may
	// span lines.
	tokComment
	// tokPunct is one of { } ( ) [ ] , : = ? ! @ @@ and a lone dot.
	tokPunct
	// tokBad is a character the language does not use, or an unterminated
	// string.
	tokBad
)

// token is one lexed token. Off and End are byte offsets into the source;
// Line and Col (1-based, Col in bytes) locate its start.
type token struct {
	kind      int
	text      string
	off, end  int
	line, col int
	// value is a string token's decoded text; bad is set when the string
	// has an escape Prisma rejects or no closing quote.
	value string
	bad   string
}

// lexer turns a .prisma source into tokens on demand. Whitespace (Unicode
// space separators and tabs, as Prisma's WHITESPACE rule) is skipped;
// newlines (\n, \r\n, \r) are tokens, because the language is line-based.
type lexer struct {
	src       string
	pos       int
	line, col int
}

func newLexer(src string) *lexer { return &lexer{src: src, line: 1, col: 1} }

func isSpace(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) }

// isIdentRune reports whether r may appear in an identifier after its
// first character: a Unicode letter, an ASCII digit, `_` or `-`.
func isIdentRune(r rune) bool {
	return unicode.IsLetter(r) || (r >= '0' && r <= '9') || r == '_' || r == '-'
}

func (lx *lexer) peekRune(at int) (rune, int) {
	if at >= len(lx.src) {
		return utf8.RuneError, 0
	}
	return utf8.DecodeRuneInString(lx.src[at:])
}

// advance moves past n bytes that hold no newline.
func (lx *lexer) advance(n int) {
	lx.pos += n
	lx.col += n
}

// newlineLen returns the length of the newline at pos (0 if none).
func (lx *lexer) newlineLen(at int) int {
	switch {
	case strings.HasPrefix(lx.src[at:], "\r\n"):
		return 2
	case at < len(lx.src) && (lx.src[at] == '\n' || lx.src[at] == '\r'):
		return 1
	}
	return 0
}

func (lx *lexer) skipSpace() {
	for lx.pos < len(lx.src) {
		r, n := lx.peekRune(lx.pos)
		if !isSpace(r) {
			return
		}
		lx.advance(n)
	}
}

// skipLine skips the rest of the current line, newline included, and
// returns the skipped text without the newline (Prisma's BLOCK_OPEN takes
// everything after `{` up to the newline).
func (lx *lexer) skipLine() string {
	start := lx.pos
	for lx.pos < len(lx.src) {
		if n := lx.newlineLen(lx.pos); n > 0 {
			text := lx.src[start:lx.pos]
			lx.pos += n
			lx.line++
			lx.col = 1
			return text
		}
		lx.pos++
		lx.col++
	}
	return lx.src[start:]
}

// skipUntil skips up to and including the next byte c (newlines counted)
// and reports whether it was found.
func (lx *lexer) skipUntil(c byte) bool {
	for lx.pos < len(lx.src) {
		if n := lx.newlineLen(lx.pos); n > 0 {
			lx.pos += n
			lx.line++
			lx.col = 1
			continue
		}
		b := lx.src[lx.pos]
		lx.pos++
		lx.col++
		if b == c {
			return true
		}
	}
	return false
}

// next returns the next token.
func (lx *lexer) next() token {
	lx.skipSpace()
	t := token{off: lx.pos, line: lx.line, col: lx.col}
	if lx.pos >= len(lx.src) {
		t.kind, t.end = tokEOF, lx.pos
		return t
	}
	if n := lx.newlineLen(lx.pos); n > 0 {
		lx.pos += n
		lx.line++
		lx.col = 1
		t.kind, t.end, t.text = tokNewline, lx.pos, "\n"
		return t
	}
	rest := lx.src[lx.pos:]
	switch {
	case strings.HasPrefix(rest, "//"):
		n := 0
		for n < len(rest) && rest[n] != '\n' && rest[n] != '\r' {
			n++
		}
		lx.advance(n)
		t.kind = tokComment
	case strings.HasPrefix(rest, "/*"):
		t.kind = tokComment
		lx.advance(2)
		closed := false
		for lx.pos < len(lx.src) {
			if strings.HasPrefix(lx.src[lx.pos:], "*/") {
				lx.advance(2)
				closed = true
				break
			}
			if n := lx.newlineLen(lx.pos); n > 0 {
				lx.pos += n
				lx.line++
				lx.col = 1
				continue
			}
			lx.advance(1)
		}
		if !closed {
			t.kind, t.bad = tokBad, "unterminated block comment"
		}
	case rest[0] == '"':
		lx.lexString(&t)
	case strings.HasPrefix(rest, "@@"):
		lx.advance(2)
		t.kind = tokPunct
	case strings.ContainsRune("{}()[],:=?!@.", rune(rest[0])):
		lx.advance(1)
		t.kind = tokPunct
	default:
		r, n := lx.peekRune(lx.pos)
		next, _ := lx.peekRune(lx.pos + n)
		// A word starts with an identifier character; a minus only before a
		// digit (a negative number).
		if !isIdentRune(r) || r == '-' && !(next >= '0' && next <= '9') {
			lx.advance(n)
			t.kind = tokBad
			break
		}
		lx.advance(n)
		for lx.pos < len(lx.src) {
			r, n := lx.peekRune(lx.pos)
			if !isIdentRune(r) && r != '.' {
				break
			}
			lx.advance(n)
		}
		t.kind = tokWord
	}
	t.end = lx.pos
	t.text = lx.src[t.off:t.end]
	return t
}

// lexString reads a string literal: Prisma's strings are JSON strings
// (raw control characters end it; escapes \" \\ \/ \b \f \n \r \t and
// \uXXXX, with UTF-16 surrogate pairs). Any other escape or a bad \u
// sequence is a parse error, as in Prisma (parse_expression.rs).
func (lx *lexer) lexString(t *token) {
	t.kind = tokString
	lx.advance(1)
	var b strings.Builder
	for {
		if lx.pos >= len(lx.src) || lx.src[lx.pos] < 0x20 {
			if t.bad == "" {
				t.bad = "unterminated string"
			}
			return
		}
		c := lx.src[lx.pos]
		if c == '"' {
			lx.advance(1)
			t.value = b.String()
			return
		}
		if c != '\\' {
			r, n := lx.peekRune(lx.pos)
			b.WriteRune(r)
			lx.advance(n)
			continue
		}
		if lx.pos+1 >= len(lx.src) {
			lx.advance(1)
			continue
		}
		e, n := lx.peekRune(lx.pos + 1)
		if e == 'u' {
			r, used := decodeUnicodeEscape(lx.src[lx.pos:])
			if r < 0 {
				if t.bad == "" {
					t.bad = "invalid unicode escape sequence"
				}
				if used < 2 {
					used = 2
				}
				lx.advanceInLine(used)
				continue
			}
			b.WriteRune(r)
			lx.advance(used)
			continue
		}
		if repl, ok := simpleEscapes[e]; ok {
			b.WriteRune(repl)
		} else if t.bad == "" {
			t.bad = "unknown escape sequence"
		}
		// The escaped character can be a newline (Prisma's string_escape
		// takes any character), which then is a bad escape.
		lx.advanceInLine(1 + n)
	}
}

// advanceInLine advances n bytes, counting a newline among them.
func (lx *lexer) advanceInLine(n int) {
	for i := 0; i < n && lx.pos < len(lx.src); i++ {
		if lx.src[lx.pos] == '\n' || lx.src[lx.pos] == '\r' && !strings.HasPrefix(lx.src[lx.pos:], "\r\n") {
			lx.pos++
			lx.line++
			lx.col = 1
			continue
		}
		lx.pos++
		lx.col++
	}
}

var simpleEscapes = map[rune]rune{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}

// decodeUnicodeEscape decodes s, which starts with `\u`, as Prisma's
// try_parse_unicode_codepoint does: four hex digits, and when they are a
// UTF-16 surrogate a second `\uXXXX` that completes the pair. It returns
// the rune and the bytes used, or -1 and the bytes to skip.
func decodeUnicodeEscape(s string) (rune, int) {
	first, ok := hex4(s)
	if !ok {
		return -1, 2
	}
	if !utf16.IsSurrogate(first) {
		return first, 6
	}
	second, ok := hex4(s[6:])
	if !ok {
		return -1, 6
	}
	r := utf16.DecodeRune(first, second)
	if r == unicode.ReplacementChar {
		return -1, 12
	}
	return r, 12
}

func hex4(s string) (rune, bool) {
	if len(s) < 6 || !strings.HasPrefix(s, `\u`) {
		return 0, false
	}
	var v rune
	for _, c := range s[2:6] {
		var d rune
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			return 0, false
		}
		v = v<<4 | d
	}
	return v, true
}

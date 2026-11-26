package diesel

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The Diesel front end reads schema files with a small token lexer, not a
// Rust parser (ADR 0062, "Payload decision" of T018g4): the forms it
// models are token trees of identifiers, paths, `->`, generics, groups,
// attributes, literals and comments. Every Rust token is lexed so that a
// form outside the claim contract is seen and refused (D) instead of
// breaking the file; only an unterminated literal or comment, an invalid
// escape or an unbalanced delimiter refuses the whole file.

// TokenKind is the kind of a lexed token.
type TokenKind int

// Token kinds. Group is used by Tree for a delimited group, never by the
// lexer itself.
const (
	// Ident is an identifier or keyword; Raw marks the r# form, and Text
	// holds the name without the prefix.
	Ident TokenKind = iota
	// Punct is one punctuation token: "->", "::" and "=>" are joined, every
	// other punctuation character is its own token ("<", ">", ",", ...).
	Punct
	// String is a string literal; Text holds its value (escapes decoded)
	// and Prefix the literal's prefix ("", "r", "b", "br", "c" or "cr").
	String
	// Number is an integer or float literal as written, suffix included.
	Number
	// Char is a character or byte literal ('x', b'x') as written.
	Char
	// Lifetime is a lifetime or label ('a) as written.
	Lifetime
	// OuterDoc is a `///` or `/** */` doc comment (an outer #[doc]
	// attribute); Text holds the comment text after the marker.
	OuterDoc
	// InnerDoc is a `//!` or `/*! */` doc comment (an inner #![doc]).
	InnerDoc
	// Other is a character no Rust token starts with outside literals and
	// comments (for example a backtick); it makes the item that holds it
	// unread.
	Other
	// Group is a delimited token tree (Tree.Delim).
	Group
)

// Token is one lexed token with its 1-based line and byte column.
type Token struct {
	Kind   TokenKind
	Text   string
	Raw    bool
	Prefix string
	Line   int
	Col    int
}

// LexError is a schema file the lexer refuses as a whole (an unterminated
// literal or comment, an invalid escape, an unbalanced delimiter, text
// that is not UTF-8): every definition in it is unread (row D).
type LexError struct {
	Line    int
	Message string
}

func (e *LexError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// Lex splits Rust source into tokens. Plain comments and white space are
// dropped; doc comments are kept as OuterDoc and InnerDoc tokens because
// they are attributes.
func Lex(src string) ([]Token, error) {
	if !utf8.ValidString(src) {
		return nil, &LexError{Line: 1, Message: "the file is not UTF-8 text"}
	}
	l := &lexer{s: strings.TrimPrefix(src, "\ufeff"), line: 1, lineStart: 0}
	var out []Token
	for {
		l.skipSpace()
		if l.i >= len(l.s) {
			return out, nil
		}
		tok, keep, err := l.next()
		if err != nil {
			return nil, err
		}
		if keep {
			out = append(out, tok)
		}
	}
}

type lexer struct {
	s         string
	i         int
	line      int
	lineStart int
}

func (l *lexer) errorf(line int, format string, args ...any) error {
	return &LexError{Line: line, Message: fmt.Sprintf(format, args...)}
}

// advance moves past n bytes, counting line breaks.
func (l *lexer) advance(n int) {
	end := min(l.i+n, len(l.s))
	for j := l.i; j < end; j++ {
		if l.s[j] == '\n' {
			l.line++
			l.lineStart = j + 1
		}
	}
	l.i = end
}

func (l *lexer) at(prefix string) bool { return strings.HasPrefix(l.s[l.i:], prefix) }

func (l *lexer) skipSpace() {
	for l.i < len(l.s) {
		r, size := utf8.DecodeRuneInString(l.s[l.i:])
		if !unicode.IsSpace(r) {
			return
		}
		l.advance(size)
	}
}

// next lexes one token at l.i; keep is false for a plain comment.
func (l *lexer) next() (Token, bool, error) {
	tok := Token{Line: l.line, Col: l.i - l.lineStart + 1}
	c := l.s[l.i]
	switch {
	case l.at("//"):
		end := strings.IndexByte(l.s[l.i:], '\n')
		if end < 0 {
			end = len(l.s) - l.i
		}
		text := strings.TrimSuffix(l.s[l.i:l.i+end], "\r")
		l.advance(end)
		switch {
		case strings.HasPrefix(text, "///") && !strings.HasPrefix(text, "////"):
			tok.Kind, tok.Text = OuterDoc, text[3:]
			return tok, true, nil
		case strings.HasPrefix(text, "//!"):
			tok.Kind, tok.Text = InnerDoc, text[3:]
			return tok, true, nil
		}
		return tok, false, nil
	case l.at("/*"):
		return l.blockComment(tok)
	case c == '"':
		return l.str(tok, "", 0)
	case c == '\'':
		return l.quote(tok)
	case c >= '0' && c <= '9':
		return l.number(tok), true, nil
	case isIdentStart(l.s[l.i:]):
		return l.ident(tok)
	}
	for _, p := range []string{"->", "::", "=>"} {
		if l.at(p) {
			tok.Kind, tok.Text = Punct, p
			l.advance(len(p))
			return tok, true, nil
		}
	}
	if strings.IndexByte("{}()[]<>,;:#!=.*&+-/@$?|^%~", c) >= 0 {
		tok.Kind, tok.Text = Punct, string(c)
		l.advance(1)
		return tok, true, nil
	}
	r, size := utf8.DecodeRuneInString(l.s[l.i:])
	tok.Kind, tok.Text = Other, string(r)
	l.advance(size)
	return tok, true, nil
}

// blockComment reads a nested block comment; `/** */` and `/*! */` are
// doc comments (but `/**/` and `/***` are not).
func (l *lexer) blockComment(tok Token) (Token, bool, error) {
	start := l.i
	depth := 0
	j := l.i
	for j < len(l.s) {
		switch {
		case strings.HasPrefix(l.s[j:], "/*"):
			depth++
			j += 2
		case strings.HasPrefix(l.s[j:], "*/"):
			depth--
			j += 2
			if depth == 0 {
				text := l.s[start:j]
				l.advance(j - start)
				switch {
				case strings.HasPrefix(text, "/**") && !strings.HasPrefix(text, "/***") && text != "/**/":
					tok.Kind, tok.Text = OuterDoc, text[3:len(text)-2]
					return tok, true, nil
				case strings.HasPrefix(text, "/*!"):
					tok.Kind, tok.Text = InnerDoc, text[3:len(text)-2]
					return tok, true, nil
				}
				return tok, false, nil
			}
		default:
			j++
		}
	}
	return tok, false, l.errorf(tok.Line, "unterminated block comment")
}

func isIdentStart(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return r == '_' || unicode.IsLetter(r)
}

func isIdentContinue(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// identEnd returns the byte length of the identifier at the start of s.
func identEnd(s string) int {
	n := 0
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		if !isIdentContinue(r) {
			break
		}
		n += size
	}
	return n
}

// ident reads an identifier, a raw identifier (r#name) or a prefixed
// literal (r"..", r#".."#, b"..", br"..", c"..", cr"..", b'x').
func (l *lexer) ident(tok Token) (Token, bool, error) {
	rest := l.s[l.i:]
	n := identEnd(rest)
	word := rest[:n]
	after := rest[n:]
	switch word {
	case "r", "br", "cr":
		if strings.HasPrefix(after, "\"") || strings.HasPrefix(after, "#") && strings.HasPrefix(strings.TrimLeft(after, "#"), "\"") {
			hashes := len(after) - len(strings.TrimLeft(after, "#"))
			l.advance(n)
			return l.str(tok, word, hashes)
		}
	case "b", "c":
		if strings.HasPrefix(after, "\"") {
			l.advance(n)
			return l.str(tok, word, 0)
		}
		if word == "b" && strings.HasPrefix(after, "'") {
			l.advance(n)
			t, keep, err := l.quote(tok)
			if t.Kind == Lifetime {
				return t, keep, l.errorf(tok.Line, "invalid byte literal")
			}
			t.Text = "b" + t.Text
			return t, keep, err
		}
	}
	if word == "r" && strings.HasPrefix(after, "#") && isIdentStart(after[1:]) {
		m := identEnd(after[1:])
		tok.Kind, tok.Text, tok.Raw = Ident, after[1:1+m], true
		l.advance(n + 1 + m)
		return tok, true, nil
	}
	tok.Kind, tok.Text = Ident, word
	l.advance(n)
	return tok, true, nil
}

// str reads a string literal starting at the opening quote (after the
// prefix and, for raw strings, the hashes still unread).
func (l *lexer) str(tok Token, prefix string, hashes int) (Token, bool, error) {
	tok.Kind, tok.Prefix = String, prefix
	raw := strings.HasPrefix(prefix, "r") || strings.HasSuffix(prefix, "r")
	if raw {
		l.advance(hashes + 1) // hashes and the opening quote
		closing := "\"" + strings.Repeat("#", hashes)
		end := strings.Index(l.s[l.i:], closing)
		if end < 0 {
			return tok, false, l.errorf(tok.Line, "unterminated raw string")
		}
		tok.Text = l.s[l.i : l.i+end]
		l.advance(end + len(closing))
		return tok, true, nil
	}
	l.advance(1)
	var b strings.Builder
	for l.i < len(l.s) {
		c := l.s[l.i]
		switch c {
		case '"':
			l.advance(1)
			tok.Text = b.String()
			return tok, true, nil
		case '\\':
			if err := l.escape(&b, tok.Line, true); err != nil {
				return tok, false, err
			}
		default:
			r, size := utf8.DecodeRuneInString(l.s[l.i:])
			if r == '\r' && strings.HasPrefix(l.s[l.i:], "\r\n") {
				// Rust normalizes CRLF inside string literals to LF.
				l.advance(1)
				continue
			}
			b.WriteRune(r)
			l.advance(size)
		}
	}
	return tok, false, l.errorf(tok.Line, "unterminated string literal")
}

// escape decodes one escape at l.i (the backslash) into b. inString
// allows the line-continuation escape.
func (l *lexer) escape(b *strings.Builder, line int, inString bool) error {
	if l.i+1 >= len(l.s) {
		return l.errorf(line, "unterminated escape")
	}
	c := l.s[l.i+1]
	switch c {
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case '\\', '"', '\'':
		b.WriteByte(c)
	case '0':
		b.WriteByte(0)
	case 'x':
		if l.i+4 > len(l.s) {
			return l.errorf(line, "invalid \\x escape")
		}
		v, err := strconv.ParseUint(l.s[l.i+2:l.i+4], 16, 8)
		if err != nil {
			return l.errorf(line, "invalid \\x escape")
		}
		b.WriteByte(byte(v))
		l.advance(4)
		return nil
	case 'u':
		rest := l.s[l.i+2:]
		end := strings.IndexByte(rest, '}')
		if !strings.HasPrefix(rest, "{") || end < 0 {
			return l.errorf(line, "invalid \\u escape")
		}
		v, err := strconv.ParseUint(strings.ReplaceAll(rest[1:end], "_", ""), 16, 32)
		if err != nil || !utf8.ValidRune(rune(v)) {
			return l.errorf(line, "invalid \\u escape")
		}
		b.WriteRune(rune(v))
		l.advance(2 + end + 1)
		return nil
	case '\n', '\r':
		if !inString {
			return l.errorf(line, "invalid escape")
		}
		// A line continuation: the break and the white space after it
		// are not part of the value.
		l.advance(1)
		for l.i < len(l.s) && (l.s[l.i] == ' ' || l.s[l.i] == '\t' || l.s[l.i] == '\n' || l.s[l.i] == '\r') {
			l.advance(1)
		}
		return nil
	default:
		return l.errorf(line, "invalid escape \\%c", c)
	}
	l.advance(2)
	return nil
}

// quote reads a character literal ('x', '\n') or a lifetime ('a).
func (l *lexer) quote(tok Token) (Token, bool, error) {
	start := l.i
	rest := l.s[l.i+1:]
	if rest == "" {
		return tok, false, l.errorf(tok.Line, "unterminated character literal")
	}
	if rest[0] == '\\' {
		l.advance(1)
		var b strings.Builder
		if err := l.escape(&b, tok.Line, false); err != nil {
			return tok, false, err
		}
		if l.i >= len(l.s) || l.s[l.i] != '\'' {
			return tok, false, l.errorf(tok.Line, "unterminated character literal")
		}
		l.advance(1)
		tok.Kind, tok.Text = Char, l.s[start:l.i]
		return tok, true, nil
	}
	r, size := utf8.DecodeRuneInString(rest)
	if strings.HasPrefix(rest[size:], "'") {
		l.advance(1 + size + 1)
		tok.Kind, tok.Text = Char, l.s[start:l.i]
		return tok, true, nil
	}
	if isIdentStart(rest) || r == '_' {
		n := identEnd(rest)
		l.advance(1 + n)
		tok.Kind, tok.Text = Lifetime, l.s[start:l.i]
		return tok, true, nil
	}
	return tok, false, l.errorf(tok.Line, "unterminated character literal")
}

// number reads an integer or float literal with its suffix.
func (l *lexer) number(tok Token) Token {
	start := l.i
	n := identEnd(l.s[l.i:])
	l.advance(n)
	// A float: digits '.' digits (not a range "1..2" or a method "1.max").
	if l.i+1 < len(l.s) && l.s[l.i] == '.' && l.s[l.i+1] >= '0' && l.s[l.i+1] <= '9' && !strings.ContainsAny(l.s[start:l.i], "xob") {
		l.advance(1)
		l.advance(identEnd(l.s[l.i:]))
	}
	tok.Kind, tok.Text = Number, l.s[start:l.i]
	return tok
}

// Tree is a token or a delimited group of trees, as Rust macros see their
// input.
type Tree struct {
	Token
	// Delim is '(', '[' or '{' for a group.
	Delim byte
	// Kids are a group's trees.
	Kids []Tree
	// EndLine is the line of a group's closing delimiter.
	EndLine int
}

// Trees groups tokens into token trees; an unbalanced or mismatched
// delimiter is a LexError.
func Trees(tokens []Token) ([]Tree, error) {
	closing := map[string]byte{")": '(', "]": '[', "}": '{'}
	type frame struct {
		open Token
		kids []Tree
	}
	stack := []frame{{}}
	for _, t := range tokens {
		if t.Kind == Punct && (t.Text == "(" || t.Text == "[" || t.Text == "{") {
			stack = append(stack, frame{open: t})
			continue
		}
		if t.Kind == Punct {
			if open, ok := closing[t.Text]; ok {
				if len(stack) == 1 {
					return nil, &LexError{Line: t.Line, Message: "unbalanced " + t.Text}
				}
				top := stack[len(stack)-1]
				if top.open.Text[0] != open {
					return nil, &LexError{Line: t.Line, Message: fmt.Sprintf("%s closes %s of line %d", t.Text, top.open.Text, top.open.Line)}
				}
				stack = stack[:len(stack)-1]
				g := Tree{Token: top.open, Delim: open, Kids: top.kids, EndLine: t.Line}
				g.Kind, g.Text = Group, ""
				stack[len(stack)-1].kids = append(stack[len(stack)-1].kids, g)
				continue
			}
		}
		stack[len(stack)-1].kids = append(stack[len(stack)-1].kids, Tree{Token: t})
	}
	if len(stack) > 1 {
		top := stack[len(stack)-1]
		return nil, &LexError{Line: top.open.Line, Message: "unclosed " + top.open.Text}
	}
	return stack[0].kids, nil
}

// LexTrees lexes src and groups the tokens into trees.
func LexTrees(src string) ([]Tree, error) {
	tokens, err := Lex(src)
	if err != nil {
		return nil, err
	}
	return Trees(tokens)
}

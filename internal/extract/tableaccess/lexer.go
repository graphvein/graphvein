package tableaccess

import (
	"fmt"
	"strings"
)

// This file is the SQL subset reader's lexer: Postgres lexical rules
// (scan.l) for the tokens the reader needs. Comments and whitespace give
// no token. The lexer never guesses: an unterminated string, quoted
// identifier, comment or dollar quote is an error, and the reader refuses
// the site.

type tokenKind uint8

const (
	// tokIdent is an unquoted identifier or keyword, folded to lower case.
	tokIdent tokenKind = iota
	// tokQuoted is a "..." or U&"..." identifier, unescaped, case kept.
	tokQuoted
	// tokString is any string constant ('...', E'...', B'...', X'...',
	// N'...', U&'...', or a dollar-quoted string).
	tokString
	tokNumber
	// tokParam is a positional parameter such as $1.
	tokParam
	// tokOp is a run of operator characters, including "::".
	tokOp
	// tokPunct is one of ( ) [ ] , ; . :
	tokPunct
)

type token struct {
	kind  tokenKind
	value string
	start int
}

// maxIdentifier is NAMEDATALEN-1: Postgres truncates longer identifiers.
const maxIdentifier = 63

func truncateIdentifier(s string) string {
	if len(s) <= maxIdentifier {
		return s
	}
	cut := maxIdentifier
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
}

func identStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func identCont(c byte) bool { return identStart(c) || c == '$' || c >= '0' && c <= '9' }

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

const opChars = "+-*/<>=~!@#%^&|`?"

// lex splits SQL text into tokens.
func lex(src string) ([]token, error) {
	var out []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < len(src) && src[i+1] == '-':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			depth, j := 1, i+2
			for j < len(src) && depth > 0 {
				switch {
				case strings.HasPrefix(src[j:], "/*"):
					depth++
					j += 2
				case strings.HasPrefix(src[j:], "*/"):
					depth--
					j += 2
				default:
					j++
				}
			}
			if depth > 0 {
				return nil, fmt.Errorf("unterminated comment at byte %d", i)
			}
			i = j
		case c == '\'':
			j, ok := scanString(src, i+1, false)
			if !ok {
				return nil, fmt.Errorf("unterminated string at byte %d", i)
			}
			out = append(out, token{tokString, "", i})
			i = j
		case (c == 'e' || c == 'E') && i+1 < len(src) && src[i+1] == '\'':
			j, ok := scanString(src, i+2, true)
			if !ok {
				return nil, fmt.Errorf("unterminated string at byte %d", i)
			}
			out = append(out, token{tokString, "", i})
			i = j
		case (c == 'b' || c == 'B' || c == 'x' || c == 'X' || c == 'n' || c == 'N') && i+1 < len(src) && src[i+1] == '\'':
			j, ok := scanString(src, i+2, false)
			if !ok {
				return nil, fmt.Errorf("unterminated string at byte %d", i)
			}
			out = append(out, token{tokString, "", i})
			i = j
		case (c == 'u' || c == 'U') && i+2 < len(src) && src[i+1] == '&' && (src[i+2] == '\'' || src[i+2] == '"'):
			if src[i+2] == '\'' {
				j, ok := scanString(src, i+3, false)
				if !ok {
					return nil, fmt.Errorf("unterminated string at byte %d", i)
				}
				out = append(out, token{tokString, "", i})
				i = j
				break
			}
			// A Unicode-escaped identifier: its value is not decoded, so the
			// reader cannot name it and refuses the site.
			return nil, fmt.Errorf("U& identifier at byte %d", i)
		case c == '"':
			j, value, ok := scanQuoted(src, i+1)
			if !ok {
				return nil, fmt.Errorf("unterminated quoted identifier at byte %d", i)
			}
			if value == "" {
				return nil, fmt.Errorf("zero-length quoted identifier at byte %d", i)
			}
			out = append(out, token{tokQuoted, truncateIdentifier(value), i})
			i = j
		case c == '$' && i+1 < len(src) && isDigit(src[i+1]):
			j := i + 1
			for j < len(src) && isDigit(src[j]) {
				j++
			}
			out = append(out, token{tokParam, src[i:j], i})
			i = j
		case c == '$':
			tag, ok := dollarTag(src, i)
			if !ok {
				return nil, fmt.Errorf("unexpected $ at byte %d", i)
			}
			end := strings.Index(src[i+len(tag):], tag)
			if end < 0 {
				return nil, fmt.Errorf("unterminated dollar quote at byte %d", i)
			}
			out = append(out, token{tokString, "", i})
			i += len(tag) + end + len(tag)
		case identStart(c):
			j := i + 1
			for j < len(src) && identCont(src[j]) {
				j++
			}
			out = append(out, token{tokIdent, truncateIdentifier(strings.ToLower(src[i:j])), i})
			i = j
		case isDigit(c) || c == '.' && i+1 < len(src) && isDigit(src[i+1]):
			j := i
			for j < len(src) && (isDigit(src[j]) || src[j] == '.' || src[j] == '_' || src[j] == 'e' || src[j] == 'E' ||
				(src[j] == '+' || src[j] == '-') && j > i && (src[j-1] == 'e' || src[j-1] == 'E')) {
				j++
			}
			out = append(out, token{tokNumber, src[i:j], i})
			i = j
		case c == ':' && i+1 < len(src) && src[i+1] == ':':
			out = append(out, token{tokOp, "::", i})
			i += 2
		case strings.IndexByte("()[],;.:", c) >= 0:
			out = append(out, token{tokPunct, string(c), i})
			i++
		case strings.IndexByte(opChars, c) >= 0:
			j := i
			for j < len(src) && strings.IndexByte(opChars, src[j]) >= 0 && !strings.HasPrefix(src[j:], "--") && !strings.HasPrefix(src[j:], "/*") {
				j++
			}
			out = append(out, token{tokOp, src[i:j], i})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q at byte %d", c, i)
		}
	}
	return out, nil
}

// scanString reads a quoted string body from i (after the opening quote)
// and returns the index after the closing quote. ” is a quote; with
// escape (E'...') a backslash escapes the next byte.
func scanString(src string, i int, escape bool) (int, bool) {
	for i < len(src) {
		switch {
		case escape && src[i] == '\\':
			i += 2
		case src[i] == '\'':
			if i+1 < len(src) && src[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1, true
		default:
			i++
		}
	}
	return i, false
}

// scanQuoted reads a quoted identifier body from i; "" is a quote.
func scanQuoted(src string, i int) (int, string, bool) {
	var b strings.Builder
	for i < len(src) {
		if src[i] == '"' {
			if i+1 < len(src) && src[i+1] == '"' {
				b.WriteByte('"')
				i += 2
				continue
			}
			return i + 1, b.String(), true
		}
		b.WriteByte(src[i])
		i++
	}
	return i, "", false
}

// dollarTag reads a dollar-quote delimiter ($$ or $tag$) at i.
func dollarTag(src string, i int) (string, bool) {
	j := i + 1
	if j < len(src) && src[j] == '$' {
		return "$$", true
	}
	if j >= len(src) || !identStart(src[j]) {
		return "", false
	}
	for j < len(src) && src[j] != '$' {
		if !identCont(src[j]) || src[j] == '$' {
			return "", false
		}
		j++
	}
	if j >= len(src) {
		return "", false
	}
	return src[i : j+1], true
}

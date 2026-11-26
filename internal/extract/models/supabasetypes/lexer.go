package supabasetypes

import (
	"fmt"
	"strings"
)

// tokenKind is the kind of one lexical token of a generated types file.
type tokenKind int

const (
	tokIdent tokenKind = iota
	tokString
	tokNumber
	tokPunct
	tokEOF
)

// token is one token with its 1-based line. For a string, text is the
// decoded value; for every other kind it is the text as written.
type token struct {
	kind tokenKind
	text string
	line int
}

// lex splits a generated types file into tokens. Whitespace, newlines and
// comments separate tokens and are dropped: the generator's output is
// formatted by prettier and projects often reformat it (semicolons,
// single quotes), which changes no declaration. A character the generator
// never writes (a template literal, a regular expression, a character
// outside the token set) is an error, so such a file is never read.
func lex(src []byte) ([]token, error) {
	var out []token
	line := 1
	i := 0
	if strings.HasPrefix(string(src[:min(3, len(src))]), "\xef\xbb\xbf") {
		i = 3
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(string(src[i+2:]), "*/")
			if end < 0 {
				return nil, fmt.Errorf("line %d: unterminated comment", line)
			}
			line += strings.Count(string(src[i:i+2+end+2]), "\n")
			i += 2 + end + 2
		case c == '"' || c == '\'':
			start := line
			var b strings.Builder
			j := i + 1
			for ; j < len(src) && src[j] != c; j++ {
				if src[j] == '\n' {
					return nil, fmt.Errorf("line %d: newline in a string", line)
				}
				if src[j] == '\\' {
					// The generator writes names through JSON.stringify:
					// an escape is kept as its escaped character for the
					// common cases and refused otherwise.
					if j+1 >= len(src) {
						break
					}
					switch src[j+1] {
					case '"', '\'', '\\', '/':
						b.WriteByte(src[j+1])
						j++
						continue
					default:
						return nil, fmt.Errorf("line %d: string escape \\%c not read", line, src[j+1])
					}
				}
				b.WriteByte(src[j])
			}
			if j >= len(src) {
				return nil, fmt.Errorf("line %d: unterminated string", line)
			}
			out = append(out, token{kind: tokString, text: b.String(), line: start})
			i = j + 1
		case isIdentStart(c):
			j := i
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			out = append(out, token{kind: tokIdent, text: string(src[i:j]), line: line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.') {
				j++
			}
			out = append(out, token{kind: tokNumber, text: string(src[i:j]), line: line})
			i = j
		case strings.IndexByte("{}[]()<>:;,|&?=.-", c) >= 0:
			out = append(out, token{kind: tokPunct, text: string(c), line: line})
			i++
		default:
			return nil, fmt.Errorf("line %d: character %q is not part of the generated form", line, c)
		}
	}
	out = append(out, token{kind: tokEOF, line: line})
	return out, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

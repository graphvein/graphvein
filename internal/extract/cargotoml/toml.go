// Package cargotoml reads the TOML subset that Cargo manifests, Cargo.lock
// files and diesel.toml use, and the Cargo dependency shapes several
// extractors share. It moved here from internal/extract/schema (T018g4,
// ADR 0062) so the sqlx migration discovery and the Diesel model
// front end read Cargo files with one reader; its behaviour is unchanged.
package cargotoml

import (
	"fmt"
	"strings"
)

// Table is a parsed TOML table. Values are string, bool-or-number text
// (Scalar), []any or Table. An [[array]] of tables is a []any of Table.
type Table map[string]any

// Scalar is any TOML value that is not a string, array or table, kept as
// written (booleans, numbers, dates).
type Scalar string

// ParseError is a TOML text the reader refuses: the 1-based line and what
// was wrong there. Callers put the file name in front ("Cargo.toml line
// 9: expected value").
type ParseError struct {
	Line    int
	Message string
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Message) }

// Parse reads the subset of TOML that Cargo manifests use: comments,
// [table] and [[array]] headers, bare, quoted and dotted keys, basic and
// literal strings (single- and multi-line), arrays and inline tables, both
// of which may span lines (Cargo accepts multi-line inline tables, kftray
// uses one). Anything else is an error (a *ParseError), so a caller
// reports the file as unreadable instead of guessing.
func Parse(src string) (Table, error) {
	p := &parser{s: strings.TrimPrefix(src, "\ufeff")}
	root := Table{}
	current := root
	for {
		p.skipBlank(true)
		if p.eof() {
			return root, nil
		}
		if p.peek() == '[' {
			array := strings.HasPrefix(p.s[p.i:], "[[")
			if array {
				p.i += 2
			} else {
				p.i++
			}
			keys, err := p.keyPath()
			if err != nil {
				return nil, err
			}
			p.skipBlank(false)
			closing := "]"
			if array {
				closing = "]]"
			}
			if !strings.HasPrefix(p.s[p.i:], closing) {
				return nil, p.errorf("unclosed table header")
			}
			p.i += len(closing)
			t, err := descend(root, keys[:len(keys)-1])
			if err != nil {
				return nil, p.errorf("%v", err)
			}
			last := keys[len(keys)-1]
			if array {
				list, _ := t[last].([]any)
				if _, exists := t[last]; exists && list == nil {
					return nil, p.errorf("key %s redefined", last)
				}
				next := Table{}
				t[last] = append(list, next)
				current = next
			} else {
				next, err := descend(t, []string{last})
				if err != nil {
					return nil, p.errorf("%v", err)
				}
				current = next
			}
		} else if err := p.assignment(current); err != nil {
			return nil, err
		}
		p.skipBlank(false)
		if !p.eof() && p.peek() != '\n' {
			return nil, p.errorf("unexpected text after value")
		}
	}
}

// descend walks into nested tables, creating them as needed. An array of
// tables resolves to its last element, as in TOML.
func descend(t Table, keys []string) (Table, error) {
	for _, k := range keys {
		switch v := t[k].(type) {
		case nil:
			next := Table{}
			t[k] = next
			t = next
		case Table:
			t = v
		case []any:
			if len(v) == 0 {
				return nil, fmt.Errorf("key %s is not a table", k)
			}
			last, ok := v[len(v)-1].(Table)
			if !ok {
				return nil, fmt.Errorf("key %s is not a table", k)
			}
			t = last
		default:
			return nil, fmt.Errorf("key %s is not a table", k)
		}
	}
	return t, nil
}

type parser struct {
	s string
	i int
}

func (p *parser) eof() bool  { return p.i >= len(p.s) }
func (p *parser) peek() byte { return p.s[p.i] }

func (p *parser) errorf(format string, args ...any) error {
	line := strings.Count(p.s[:min(p.i, len(p.s))], "\n") + 1
	return &ParseError{Line: line, Message: fmt.Sprintf(format, args...)}
}

// skipBlank skips spaces, tabs, carriage returns and comments; with
// newlines set it also skips line breaks.
func (p *parser) skipBlank(newlines bool) {
	for !p.eof() {
		c := p.peek()
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			p.i++
		case c == '\n' && newlines:
			p.i++
		case c == '#':
			for !p.eof() && p.peek() != '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

func (p *parser) assignment(t Table) error {
	keys, err := p.keyPath()
	if err != nil {
		return err
	}
	p.skipBlank(false)
	if p.eof() || p.peek() != '=' {
		return p.errorf("expected =")
	}
	p.i++
	p.skipBlank(false)
	value, err := p.value()
	if err != nil {
		return err
	}
	target, err := descend(t, keys[:len(keys)-1])
	if err != nil {
		return p.errorf("%v", err)
	}
	last := keys[len(keys)-1]
	if _, exists := target[last]; exists {
		return p.errorf("key %s defined twice", last)
	}
	target[last] = value
	return nil
}

func (p *parser) keyPath() ([]string, error) {
	var keys []string
	for {
		p.skipBlank(false)
		if p.eof() {
			return nil, p.errorf("expected key")
		}
		var key string
		switch c := p.peek(); {
		case c == '"' || c == '\'':
			s, err := p.str()
			if err != nil {
				return nil, err
			}
			key = s
		default:
			start := p.i
			for !p.eof() {
				c := p.peek()
				if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' {
					p.i++
					continue
				}
				break
			}
			if p.i == start {
				return nil, p.errorf("expected key")
			}
			key = p.s[start:p.i]
		}
		keys = append(keys, key)
		p.skipBlank(false)
		if p.eof() || p.peek() != '.' {
			return keys, nil
		}
		p.i++
	}
}

func (p *parser) value() (any, error) {
	if p.eof() {
		return nil, p.errorf("expected value")
	}
	switch p.peek() {
	case '"', '\'':
		return p.str()
	case '[':
		p.i++
		var list []any
		for {
			p.skipBlank(true)
			if p.eof() {
				return nil, p.errorf("unclosed array")
			}
			if p.peek() == ']' {
				p.i++
				if list == nil {
					list = []any{}
				}
				return list, nil
			}
			v, err := p.value()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
			p.skipBlank(true)
			if !p.eof() && p.peek() == ',' {
				p.i++
			} else if p.eof() || p.peek() != ']' {
				return nil, p.errorf("expected , or ] in array")
			}
		}
	case '{':
		p.i++
		t := Table{}
		for {
			p.skipBlank(true)
			if p.eof() {
				return nil, p.errorf("unclosed inline table")
			}
			if p.peek() == '}' {
				p.i++
				return t, nil
			}
			if err := p.assignment(t); err != nil {
				return nil, err
			}
			p.skipBlank(true)
			if !p.eof() && p.peek() == ',' {
				p.i++
			} else if p.eof() || p.peek() != '}' {
				return nil, p.errorf("expected , or } in inline table")
			}
		}
	}
	start := p.i
	for !p.eof() {
		c := p.peek()
		if c == ',' || c == ']' || c == '}' || c == '\n' || c == '#' || c == '\r' {
			break
		}
		p.i++
	}
	text := strings.TrimSpace(p.s[start:p.i])
	if text == "" {
		return nil, p.errorf("expected value")
	}
	return Scalar(text), nil
}

// str reads a basic or literal string, single- or multi-line.
func (p *parser) str() (string, error) {
	quote := p.peek()
	triple := strings.Repeat(string(quote), 3)
	if strings.HasPrefix(p.s[p.i:], triple) {
		p.i += 3
		if strings.HasPrefix(p.s[p.i:], "\n") {
			p.i++
		}
		end := strings.Index(p.s[p.i:], triple)
		if end < 0 {
			return "", p.errorf("unclosed multi-line string")
		}
		raw := p.s[p.i : p.i+end]
		p.i += end + 3
		if quote == '\'' {
			return raw, nil
		}
		return unescape(raw, p)
	}
	p.i++
	var b strings.Builder
	for !p.eof() {
		c := p.peek()
		if c == '\n' {
			break
		}
		if c == quote {
			p.i++
			if quote == '\'' {
				return b.String(), nil
			}
			return unescape(b.String(), p)
		}
		if c == '\\' && quote == '"' && p.i+1 < len(p.s) {
			b.WriteByte(c)
			b.WriteByte(p.s[p.i+1])
			p.i += 2
			continue
		}
		b.WriteByte(c)
		p.i++
	}
	return "", p.errorf("unclosed string")
}

func unescape(raw string, p *parser) (string, error) {
	if !strings.Contains(raw, "\\") {
		return raw, nil
	}
	var b strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			b.WriteByte(raw[i])
			continue
		}
		i++
		switch raw[i] {
		case '"', '\\':
			b.WriteByte(raw[i])
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		default:
			// \u, \U, line-ending backslashes and others never occur in the
			// keys and values this reader looks at; refuse rather than guess.
			return "", p.errorf("unsupported string escape")
		}
	}
	return b.String(), nil
}

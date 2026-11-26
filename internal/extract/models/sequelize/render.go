package sequelize

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/graphvein/graphvein/internal/lang/typescript"
)

// This file holds the JavaScript literal helpers of the claim pass: the
// text JavaScript prints for a number literal, JSON.stringify of a literal
// value, and the quoting Sequelize's query generators apply. They are
// copies of the Drizzle front end's helpers (internal/extract/models/
// drizzle/render.go), which are unexported there.

// literalString returns a string literal's value (a quoted string, or a
// template without substitutions).
func literalString(e *typescript.ModelExpr) (string, bool) {
	switch {
	case e == nil:
	case e.Kind == typescript.ModelString:
		return e.Value, true
	case e.Kind == typescript.ModelTemplate && len(e.Substitutions) == 0 && len(e.Strings) == 1:
		return e.Strings[0], true
	}
	return "", false
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

// smallInt is a non-negative integer literal small enough for an int.
var smallInt = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})$`)

// intArg reads a non-negative integer literal.
func intArg(e *typescript.ModelExpr) (int, bool) {
	if e == nil || e.Kind != typescript.ModelNumber || !smallInt.MatchString(e.Value) {
		return 0, false
	}
	n := 0
	for _, c := range e.Value {
		n = n*10 + int(c-'0')
	}
	return n, true
}

// jsonStringify returns JSON.stringify of a literal value, ok false when
// the value is not a literal the payload states exactly (an identifier, a
// spread, a method, a key JavaScript would reorder or that is written
// twice).
func jsonStringify(e *typescript.ModelExpr) (string, bool) {
	var b strings.Builder
	if !writeJSON(&b, e, 0) {
		return "", false
	}
	return b.String(), true
}

// arrayIndexKey matches keys JavaScript orders before the others (array
// indexes).
var arrayIndexKey = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

func writeJSON(b *strings.Builder, e *typescript.ModelExpr, depth int) bool {
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
		for i := range e.Elements {
			if i > 0 {
				b.WriteByte(',')
			}
			if e.Elements[i].Kind == typescript.ModelSpread || !writeJSON(b, &e.Elements[i], depth+1) {
				return false
			}
		}
		b.WriteByte(']')
	case typescript.ModelObject:
		seen := map[string]bool{}
		b.WriteByte('{')
		for i := range e.Entries {
			en := &e.Entries[i]
			if en.Kind != typescript.ModelProperty || seen[en.Key] || arrayIndexKey.MatchString(en.Key) {
				return false
			}
			seen[en.Key] = true
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, en.Key)
			b.WriteByte(':')
			if !writeJSON(b, &en.Value, depth+1) {
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

// sqlQuote is Sequelize's string escape for postgres and sqlite
// (lib/sql-string.js:57-61): quotes doubled. Strings with a NUL character
// (which postgres escapes differently) are refused by the callers.
func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// pgIdentifierOK says whether postgres keeps an identifier as written:
// at most 63 bytes (NAMEDATALEN - 1; a longer one is truncated with a
// notice) and valid UTF-8 without a NUL.
func pgIdentifierOK(name string) bool {
	return name != "" && len(name) <= 63 && utf8.ValidString(name) && !strings.ContainsRune(name, 0)
}

// pgTypeName is the name format_type prints for a type in a schema on the
// search path: quoted (inner quotes doubled) unless it is a lower-case
// identifier (quote_identifier's rule; enum type names start with "enum_",
// never a keyword).
var pgPlainIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func pgTypeName(name string) string {
	if pgPlainIdentifier.MatchString(name) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

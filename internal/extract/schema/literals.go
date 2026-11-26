package schema

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Literal grammars (T018a2 B2, class rule P5 "literals"): Postgres converts
// an untyped string constant with the type's input function when the
// statement is analysed, so input the function refuses is a refusal of
// the statement itself. A literal is proven valid only when it matches a
// written grammar that is a subset of what the REL_17_6 input function
// accepts; everything else is not proven (may-fail). Each grammar below
// cites the function it follows. The grammars are narrower than the input
// functions on purpose (no non-decimal integers, underscores, NaN for
// numeric, time zone abbreviations, ...): what they accept, Postgres
// accepts, under the environment assumptions E1-E4 (default settings).

// pgSpace reports the bytes the input functions skip as white space:
// isspace() in the C locale and scansup.c scanner_isspace() agree on these
// six; no other byte (and no Unicode space) is skipped.
func pgSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f'
}

// trimPG removes the leading and trailing white space the input functions
// skip (pgSpace), and nothing else.
func trimPG(s string) string {
	i, j := 0, len(s)
	for i < j && pgSpace(s[i]) {
		i++
	}
	for j > i && pgSpace(s[j-1]) {
		j--
	}
	return s[i:j]
}

// literalValue returns the value of a string constant token that the
// literal grammars may read: a plain '...' or dollar-quoted string, or an
// E'...' string without escapes (the lexer does not decode them). B”,
// X”, U&” and N” constants are not read.
func literalValue(t token, src []byte) (string, bool) {
	if t.kind != tokString || t.start < 0 || t.start >= len(src) {
		return "", false
	}
	switch src[t.start] {
	case 'b', 'B', 'x', 'X', 'u', 'U', 'n', 'N':
		return "", false
	case 'e', 'E':
		if t.end > len(src) || t.end < t.start+3 {
			return "", false
		}
		return escapeValue(string(src[t.start+2 : t.end-1]))
	}
	return t.value, true
}

// escapeValue decodes the body of an E'...' string (scan.l xe rules): the
// escapes \b \f \n \r \t, a doubled quote, and a backslash before any
// other character that stands for that character. Octal, hexadecimal and
// Unicode escapes are not decoded (they can produce bytes the server
// encoding refuses), so such a string is not read.
func escapeValue(body string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case c == '\\' && i+1 < len(body):
			i++
			switch d := body[i]; d {
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'x', 'u', 'U', '0', '1', '2', '3', '4', '5', '6', '7':
				return "", false
			default:
				b.WriteByte(d)
			}
		case c == '\\':
			return "", false
		case c == '\'' && i+1 < len(body) && body[i+1] == '\'':
			b.WriteByte('\'')
			i++
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}

var (
	// intLiteral follows numutils.c pg_strtoint16/32/64_safe for decimal
	// input: an optional sign and decimal digits (white space trimmed).
	intLiteral = regexp.MustCompile(`^[+-]?[0-9]+$`)
	// numericLiteral follows numeric.c numeric_in / set_var_from_str for
	// decimal input: sign, digits with at most one point and at least one
	// digit, optional exponent with digits.
	numericLiteral = regexp.MustCompile(`^[+-]?(?:([0-9]+)(?:\.([0-9]*))?|\.([0-9]+))([eE][+-]?[0-9]+)?$`)
	// floatLiteral is the decimal subset of strtod/strtof that float.c
	// float4in_internal / float8in_internal read.
	floatLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	// uuidLiteral follows uuid.c string_to_uuid: 32 hex digits, a hyphen
	// allowed after each group of four (we accept the 8-4-4-4-12 shape or
	// none), no braces, no white space.
	uuidLiteral = regexp.MustCompile(`^(?i)[0-9a-f]{8}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{4}-?[0-9a-f]{12}$`)
	// dateTimeLit is the ISO subset of datetime.c ParseDateTime /
	// DecodeDateTime: YYYY-MM-DD, optionally [ T]hh:mm[:ss[.ffffff]] and a
	// numeric UTC offset (DecodeTimezone: +hh, +hhmm, +hh:mm).
	dateTimeLit = regexp.MustCompile(`^([0-9]{4})-([0-9]{2})-([0-9]{2})(?:[ T]([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.[0-9]{1,6})?)?(?:([+-])([0-9]{2})(?::?([0-9]{2}))?)?)?$`)
	// intervalPart is one "number unit" pair of datetime.c DecodeInterval.
	intervalPart = regexp.MustCompile(`^([+-]?[0-9]{1,6}(?:\.[0-9]{1,6})?)[ \t]+([a-z]+)$`)
	// specialDate are the datetime.c datetktbl special words that date_in,
	// timestamp_in and timestamptz_in accept on their own.
	specialDate = map[string]bool{"epoch": true, "infinity": true, "-infinity": true, "now": true, "today": true, "tomorrow": true, "yesterday": true}
	// intervalUnits maps the deltatktbl unit words the grammar accepts to
	// the field DecodeInterval sets (a field given twice is refused there:
	// "tmask & fmask").
	intervalUnits = map[string]string{
		"year": "year", "years": "year", "y": "year", "yr": "year", "yrs": "year",
		"mon": "month", "mons": "month", "month": "month", "months": "month",
		"week": "week", "weeks": "week", "w": "week",
		"day": "day", "days": "day", "d": "day",
		"hour": "hour", "hours": "hour", "h": "hour", "hr": "hour", "hrs": "hour",
		"min": "minute", "mins": "minute", "minute": "minute", "minutes": "minute", "m": "minute",
		"sec": "second", "secs": "second", "second": "second", "seconds": "second", "s": "second",
	}
	// literalBounds are the magnitudes the float grammars accept: inside
	// the normal range of float4 and float8, with a margin (strtof/strtod
	// report ERANGE beyond it, and float.c refuses an overflow or a result
	// that rounds to zero).
	literalBounds = map[string][2]float64{"float4": {1.2e-38, 3.4e38}, "float8": {2.3e-308, 1.7e308}}
)

// validLiteral reports whether s is valid input for the written type
// under the literal grammars above. Types without a grammar are not
// proven.
func (e *exprEnv) validLiteral(s, written string) bool {
	if !utf8.ValidString(s) || strings.IndexByte(s, 0) >= 0 {
		return false
	}
	canon, ok := canonicalType(written)
	if !ok {
		return false
	}
	canon = strings.TrimPrefix(canon, "pg_catalog.")
	base, mods := canon, ""
	if i := strings.Index(canon, "("); i >= 0 && !strings.HasSuffix(canon, "[]") {
		base, mods = canon[:i], strings.Trim(canon[i:], "()")
	}
	trimmed := trimPG(s)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasSuffix(canon, "[]"):
		// arrayfuncs.c array_in: an empty array, scanner_isspace around it.
		return trimmed == "{}"
	case base == "text" || base == "name":
		return true
	case base == "varchar" || base == "bpchar":
		// varchar.c varchar_input / bpchar_input: longer input is refused
		// unless the excess is spaces; we accept only input that fits.
		if mods == "" {
			return true
		}
		n, err := strconv.Atoi(mods)
		return err == nil && utf8.RuneCountInString(s) <= n
	case base == "int2" || base == "int4" || base == "int8":
		if !intLiteral.MatchString(trimmed) {
			return false
		}
		bits := map[string]int{"int2": 16, "int4": 32, "int8": 64}[base]
		_, err := strconv.ParseInt(trimmed, 10, bits)
		return err == nil
	case base == "numeric":
		return validNumeric(trimmed, mods)
	case base == "float4" || base == "float8":
		return validFloat(trimmed, lower, base)
	case base == "bool":
		// bool.c boolin / parse_bool_with_len (we accept the whole words
		// and the one-letter forms, not other prefixes).
		switch lower {
		case "t", "true", "f", "false", "y", "yes", "n", "no", "on", "off", "1", "0":
			return true
		}
		return false
	case base == "uuid":
		// string_to_uuid skips no white space.
		return uuidLiteral.MatchString(s)
	case base == "date":
		return specialDate[lower] || validDateTime(trimmed, false)
	case base == "timestamp" || base == "timestamptz":
		return specialDate[lower] || validDateTime(trimmed, true)
	case base == "interval":
		return validInterval(lower)
	case base == "json" || base == "jsonb":
		return validJSON(s, base == "jsonb")
	case base == "bytea":
		// varlena.c byteain: \x and hex digit pairs, or escape format
		// without any backslash.
		if strings.HasPrefix(s, `\x`) {
			_, err := hex.DecodeString(s[2:])
			return err == nil
		}
		return !strings.Contains(s, `\`)
	}
	if !isCatalogType(base) && e.s != nil {
		// An enum of the set whose labels are known (R2; enum.c enum_in
		// compares the whole string).
		parts := strings.Split(base, ".")
		q := qname{name: parts[len(parts)-1]}
		if len(parts) == 2 {
			q.schema = &parts[0]
		}
		if r := e.s.enumLabelsKnown(q); r != nil && r.kind == "type" {
			return slices.Contains(r.labels, s)
		}
	}
	return false
}

// validNumeric checks numeric input (white space trimmed). Without a
// typmod the digits and the exponent are bounded far inside the numeric
// format (make_result's weight and dscale limits); with numeric(p,s) the
// value must fit without rounding (apply_typmod refuses a value whose
// rounded integer part is too wide; we accept no rounding at all).
func validNumeric(s, mods string) bool {
	m := numericLiteral.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	intPart, frac := m[1], m[2]
	if m[3] != "" {
		frac = m[3]
	}
	if len(intPart)+len(frac) > 1000 {
		return false
	}
	if m[4] != "" {
		exp, err := strconv.Atoi(strings.TrimLeft(m[4][1:], "+"))
		if err != nil || exp > 1000 || exp < -1000 {
			return false
		}
	}
	if mods == "" {
		return true
	}
	parts := strings.Split(mods, ",")
	p, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return false
	}
	sc := 0
	if len(parts) == 2 {
		if sc, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return false
		}
	}
	if m[4] != "" || sc < 0 || sc > p {
		return false
	}
	return len(strings.TrimLeft(intPart, "0")) <= p-sc && len(frac) <= sc
}

// validFloat checks float4 and float8 input (float.c float4in_internal,
// float8in_internal): the decimal strtod subset inside the normal range
// (or zero), or the special words the functions test.
func validFloat(s, lower, base string) bool {
	switch lower {
	case "nan", "infinity", "+infinity", "-infinity", "inf", "+inf", "-inf":
		return true
	}
	if !floatLiteral.MatchString(s) {
		return false
	}
	mant := strings.TrimLeft(s, "+-")
	if i := strings.IndexAny(mant, "eE"); i >= 0 {
		mant = mant[:i]
	}
	if strings.Trim(mant, "0.") == "" {
		return true
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(v, 0) {
		return false
	}
	b := literalBounds[base]
	v = math.Abs(v)
	return v >= b[0] && v <= b[1]
}

// validDateTime checks an ISO date, optionally with a time of day and a
// UTC offset (time allowed only for timestamps): year 0001-9999 (year 0
// does not exist in AD notation), a real calendar day, hour 0-23, minute
// and second 0-59, offset hour 0-15 and minute 0-59 (DecodeTimezone,
// MAX_TZDISP_HOUR; beyond it 22009).
func validDateTime(s string, withTime bool) bool {
	m := dateTimeLit.FindStringSubmatch(s)
	if m == nil || !withTime && m[4] != "" {
		return false
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	if y < 1 || mo < 1 || mo > 12 || d < 1 || d > time.Date(y, time.Month(mo)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return false
	}
	if m[4] != "" {
		h, _ := strconv.Atoi(m[4])
		mi, _ := strconv.Atoi(m[5])
		sec := 0
		if m[6] != "" {
			sec, _ = strconv.Atoi(m[6])
		}
		if h > 23 || mi > 59 || sec > 59 {
			return false
		}
	}
	if m[7] != "" {
		zh, _ := strconv.Atoi(m[8])
		zm := 0
		if m[9] != "" {
			zm, _ = strconv.Atoi(m[9])
		}
		if zh > 15 || zm > 59 {
			return false
		}
	}
	return true
}

// validInterval checks interval input (lowercase, white space trimmed):
// one or more "number unit" pairs separated by spaces or tabs, each field
// at most once (DecodeInterval refuses a repeated field), at most six
// integer and six fraction digits per number (far inside the interval
// range).
func validInterval(s string) bool {
	words := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '\t' })
	if len(words) == 0 || len(words)%2 != 0 {
		return false
	}
	seen := map[string]bool{}
	for i := 0; i < len(words); i += 2 {
		m := intervalPart.FindStringSubmatch(words[i] + " " + words[i+1])
		if m == nil {
			return false
		}
		field, ok := intervalUnits[m[2]]
		if !ok || seen[field] {
			return false
		}
		seen[field] = true
	}
	return trimPG(s) == s && !strings.ContainsAny(s, "\n\r\v\f")
}

// validJSON checks json and jsonb input (jsonapi.c pg_parse_json): valid
// JSON (the same grammar and white space as RFC 8259), nesting at most 64,
// no \u escape at all (jsonapi.c refuses lone surrogates, and jsonb
// \u0000), and for jsonb numbers that numeric_in accepts within the
// bounds of validNumeric.
func validJSON(s string, binary bool) bool {
	if !json.Valid([]byte(s)) || strings.Contains(s, `\u`) {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return true
		}
		if err != nil {
			return false
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' || v == '[' {
				depth++
				if depth > 64 {
					return false
				}
			} else {
				depth--
			}
		case json.Number:
			if binary && !validNumeric(string(v), "") {
				return false
			}
		}
	}
}

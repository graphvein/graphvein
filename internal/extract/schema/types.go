package schema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Column types (Revision 3 B1). A column type is read with the Typename
// grammar of gram.y (REL_17_6 14294-14760): the SQL-standard multi-word
// types, a generic [schema.]name with optional modifiers, then array
// bounds. Anything after a complete type that is not a column constraint
// is a parse error, so that "text FROBNICATE" is never recorded as a type.

// columnType reads a Typename at the cursor.
func columnType(c *cursor) error {
	c.acceptKw("setof")
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted || t.kind == tokIdent && columnKeywords[t.value] {
		return c.fail("expected a column type")
	}
	// Only unquoted words are the grammar's type keywords.
	kw := ""
	if t.kind == tokIdent {
		kw = t.value
	}
	modifiers := func() error {
		if c.isPunct("(") {
			end, err := c.group()
			if err != nil {
				return err
			}
			c.i = end
		}
		return nil
	}
	switch kw {
	case "int", "integer", "smallint", "bigint", "real", "boolean", "json":
		c.i++
	case "float", "decimal", "dec", "numeric", "varchar":
		c.i++
		if err := modifiers(); err != nil {
			return err
		}
	case "double":
		c.i++
		if !c.acceptKw("precision") {
			return c.fail("expected PRECISION")
		}
	case "bit", "character", "char", "nchar":
		c.i++
		c.acceptKw("varying")
		if err := modifiers(); err != nil {
			return err
		}
	case "national":
		c.i++
		if !c.acceptKw("character") && !c.acceptKw("char") {
			return c.fail("expected CHARACTER or CHAR")
		}
		c.acceptKw("varying")
		if err := modifiers(); err != nil {
			return err
		}
	case "timestamp", "time":
		c.i++
		if err := modifiers(); err != nil {
			return err
		}
		if c.acceptKw("with") || c.acceptKw("without") {
			if !c.acceptKw("time", "zone") {
				return c.fail("expected TIME ZONE")
			}
		}
	case "interval":
		c.i++
		if c.isPunct("(") {
			if err := modifiers(); err != nil {
				return err
			}
			break
		}
		if err := intervalFields(c); err != nil {
			return err
		}
	default:
		// U8: an unqualified GenericType is a type_function_name (no
		// col_name or reserved keyword; the SQL types gram.y spells out
		// are the cases above); a qualified one starts with a ColId, and
		// every part after a dot is a ColLabel.
		if t.kind == tokIdent {
			cat := pgKeywordCategories[t.value]
			qualified := c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "."
			if !qualified && (cat == 'R' || cat == 'C') || qualified && (cat == 'R' || cat == 'T') {
				return c.fail("keyword %s %s (gram.y GenericType)", t.value, keywordNameError)
			}
		}
		c.i++
		for c.isPunct(".") {
			c.i++
			if _, err := c.label(); err != nil {
				return err
			}
		}
		if err := modifiers(); err != nil {
			return err
		}
	}
	// opt_array_bounds, or ARRAY [n].
	for c.isPunct("[") {
		end, err := c.group()
		if err != nil {
			return err
		}
		c.i = end
	}
	if c.acceptKw("array") && c.isPunct("[") {
		end, err := c.group()
		if err != nil {
			return err
		}
		c.i = end
	}
	return nil
}

// intervalFields reads opt_interval (gram.y 14673): YEAR [TO MONTH], MONTH,
// DAY [TO HOUR|MINUTE|SECOND], HOUR [TO MINUTE|SECOND], MINUTE [TO
// SECOND], SECOND, where SECOND may take a precision.
func intervalFields(c *cursor) error {
	second := func() error {
		if c.isPunct("(") {
			end, err := c.group()
			if err != nil {
				return err
			}
			c.i = end
		}
		return nil
	}
	switch {
	case c.acceptKw("year"):
		if c.acceptKw("to") && !c.acceptKw("month") {
			return c.fail("expected MONTH")
		}
	case c.acceptKw("month"), c.acceptKw("minute"), c.acceptKw("hour"), c.acceptKw("day"):
		from := c.toks[c.i-1].value
		if !c.acceptKw("to") {
			return nil
		}
		allowed := map[string][]string{"day": {"hour", "minute", "second"}, "hour": {"minute", "second"}, "minute": {"second"}}[from]
		t := c.tok()
		if c.done() || t.kind != tokIdent || !slices.Contains(allowed, t.value) {
			return c.fail("unexpected interval field")
		}
		c.i++
		if t.value == "second" {
			return second()
		}
	case c.acceptKw("second"):
		return second()
	}
	return nil
}

// builtinTypeWords are the words of the types that pg_catalog defines and
// the grammar's type keywords. A column whose written type uses another
// word (or another schema) has a type that the set or an extension
// defines: a domain, enum, range, base type or the row type of a table,
// view, materialized view, foreign table or composite type (Revision 3
// class 1).
var builtinTypeWords = map[string]bool{
	"pg_catalog": true, "setof": true, "array": true,
	"bool": true, "boolean": true, "bytea": true, "char": true, "character": true, "varchar": true,
	"varying": true, "national": true, "nchar": true, "bpchar": true, "name": true, "text": true,
	"int": true, "integer": true, "int2": true, "int4": true, "int8": true, "smallint": true, "bigint": true,
	"serial": true, "serial2": true, "serial4": true, "serial8": true, "smallserial": true, "bigserial": true,
	"real": true, "float": true, "float4": true, "float8": true, "double": true, "precision": true,
	"numeric": true, "decimal": true, "dec": true, "money": true,
	"json": true, "jsonb": true, "jsonpath": true, "xml": true, "uuid": true,
	"date": true, "time": true, "timetz": true, "timestamp": true, "timestamptz": true, "interval": true,
	"with": true, "without": true, "zone": true, "year": true, "month": true, "day": true, "hour": true,
	"minute": true, "second": true, "to": true,
	"bit": true, "varbit": true, "inet": true, "cidr": true, "macaddr": true, "macaddr8": true,
	"point": true, "line": true, "lseg": true, "box": true, "path": true, "polygon": true, "circle": true,
	"tsvector": true, "tsquery": true, "oid": true, "xid": true, "xid8": true, "cid": true, "tid": true,
	"regclass": true, "regtype": true, "regproc": true, "regprocedure": true, "regoper": true,
	"regoperator": true, "regconfig": true, "regdictionary": true, "regnamespace": true, "regrole": true,
	"regcollation": true, "pg_lsn": true, "pg_snapshot": true, "txid_snapshot": true,
	"int4range": true, "int8range": true, "numrange": true, "tsrange": true, "tstzrange": true, "daterange": true,
	"int4multirange": true, "int8multirange": true, "nummultirange": true, "tsmultirange": true,
	"tstzmultirange": true, "datemultirange": true,
}

// builtinType reports whether every word of a written type is a built-in
// type word.
func builtinType(typeNames []string) bool {
	for _, n := range typeNames {
		if !builtinTypeWords[n] {
			return false
		}
	}
	return true
}

// canonicalType names a written column type the way Postgres resolves it
// (Revision 9 C1), so that foreign-key type checks equate aliases: int,
// integer and int4 are all int4. It follows gram.y REL_17_6 Typename
// 14294-14336, Numeric 14407-14491, Bit 14497-14550, Character
// 14555-14620 and ConstDatetime 14622-14674: the SQL-standard keyword
// spellings become the pg_catalog type they stand for (SystemTypeName),
// with the type modifiers they carry; any other name, quoted or not, is a
// GenericType that stands for itself. A quoted keyword spelling is a
// GenericType too, so "integer" (quoted) is a type named integer, not
// int4, and compares unequal to it, while "int4" (quoted) is int4. Array
// bounds are dropped (Postgres ignores them) but the number of
// dimensions is kept. ok is false when the text cannot be read; the
// caller then treats the type as unequal to every other.
func canonicalType(typ string) (canon string, ok bool) {
	stmts, fail := lexStatements([]byte(typ))
	if fail != nil || len(stmts) != 1 {
		return "", false
	}
	toks := stmts[0].tokens
	i := 0
	kw := func(words ...string) bool {
		for k, w := range words {
			if i+k >= len(toks) || toks[i+k].kind != tokIdent || toks[i+k].value != w {
				return false
			}
		}
		i += len(words)
		return true
	}
	punct := func(v string) bool {
		if i < len(toks) && toks[i].kind == tokPunct && toks[i].value == v {
			i++
			return true
		}
		return false
	}
	// modifiers reads an optional parenthesised modifier list as written
	// without spaces, such as "(10,2)".
	modifiers := func() (string, bool) {
		if i >= len(toks) || toks[i].kind != tokPunct || toks[i].value != "(" {
			return "", true
		}
		var b strings.Builder
		for depth := 0; i < len(toks); i++ {
			t := toks[i]
			if t.kind == tokString || t.kind == tokQuoted {
				return "", false
			}
			b.WriteString(t.value)
			if t.kind == tokPunct && t.value == "(" {
				depth++
			}
			if t.kind == tokPunct && t.value == ")" {
				depth--
				if depth == 0 {
					i++
					return b.String(), true
				}
			}
		}
		return "", false
	}
	setof := ""
	if kw("setof") {
		setof = "setof "
	}
	if i >= len(toks) {
		return "", false
	}
	var base string
	mods, modsOK := "", true
	switch {
	case kw("int"), kw("integer"):
		base = "int4"
	case kw("smallint"):
		base = "int2"
	case kw("bigint"):
		base = "int8"
	case kw("real"):
		base = "float4"
	case kw("double", "precision"):
		base = "float8"
	case kw("boolean"):
		base = "bool"
	case kw("json"):
		base = "json"
	case kw("float"):
		base = "float8"
		if punct("(") {
			if i+1 >= len(toks) || toks[i].kind != tokNumber || !punct2(toks, i+1, ")") {
				return "", false
			}
			p, err := strconv.Atoi(toks[i].value)
			switch {
			case err != nil || p < 1 || p > 53:
				return "", false
			case p <= 24:
				base = "float4"
			}
			i += 2
		}
	case kw("decimal"), kw("dec"), kw("numeric"):
		base = "numeric"
		mods, modsOK = modifiers()
	case kw("bit"):
		base = "bit"
		if kw("varying") {
			base = "varbit"
		}
		mods, modsOK = modifiers()
		if mods == "" && base == "bit" {
			mods = "(1)"
		}
	case kw("character"), kw("char"), kw("nchar"), kw("national", "character"), kw("national", "char"), kw("varchar"):
		base = "bpchar"
		if toks[i-1].value == "varchar" || kw("varying") {
			base = "varchar"
		}
		mods, modsOK = modifiers()
		if mods == "" && base == "bpchar" {
			mods = "(1)"
		}
	case kw("timestamp"), kw("time"):
		base = toks[i-1].value
		mods, modsOK = modifiers()
		switch {
		case kw("with", "time", "zone"):
			base += "tz"
		case kw("without", "time", "zone"):
		}
	case kw("interval"):
		// Interval fields are kept as written, lower case.
		var words []string
		for i < len(toks) && !(toks[i].kind == tokPunct && toks[i].value == "[") && !(toks[i].kind == tokIdent && toks[i].value == "array") {
			words = append(words, toks[i].value)
			i++
		}
		base = strings.Join(append([]string{"interval"}, words...), " ")
	default:
		// GenericType: [schema.]name [modifiers].
		var parts []string
		for {
			t := toks[i]
			if t.kind != tokIdent && t.kind != tokQuoted {
				return "", false
			}
			parts = append(parts, t.value)
			i++
			if !punct(".") {
				break
			}
			if i >= len(toks) {
				return "", false
			}
		}
		base = strings.Join(parts, ".")
		mods, modsOK = modifiers()
	}
	if !modsOK {
		return "", false
	}
	dims := ""
	for punct("[") {
		if i < len(toks) && toks[i].kind == tokNumber {
			i++
		}
		if !punct("]") {
			return "", false
		}
		dims += "[]"
	}
	if dims == "" && kw("array") {
		dims = "[]"
		if punct("[") {
			if i >= len(toks) || toks[i].kind != tokNumber {
				return "", false
			}
			i++
			if !punct("]") {
				return "", false
			}
		}
	}
	if i != len(toks) {
		return "", false
	}
	return setof + base + mods + dims, true
}

// punct2 reports whether toks[j] is the punctuation v.
func punct2(toks []token, j int, v string) bool {
	return j < len(toks) && toks[j].kind == tokPunct && toks[j].value == v
}

// sameType reports whether two written types are surely the same type
// after canonicalType. A type that cannot be read is never the same.
func sameType(a, b string) bool {
	ca, okA := canonicalType(a)
	cb, okB := canonicalType(b)
	return okA && okB && ca == cb
}

// Type modifiers (T018a2 Revision 1.4 R4, contract row M2). Postgres runs
// a type's typmodin function on the modifiers when it reads a Typename
// (parse_type.c typenameTypeMod), and refuses the statement when they do
// not fit (22023), or when the type has no typmodin (42601). The checks
// below are REL_17_6's: varchar.c anychar_typmodin (one length, 1 to
// MaxAttrSize = 10485760), varbit.c anybit_typmodin (one length, 1 to
// MaxAttrSize * 8), numeric.c numerictypmodin (precision 1 to 1000, scale
// -1000 to 1000), date.c anytime_typmod_check and timestamp.c
// anytimestamp_typmod_check (one precision, not negative; above 6 a
// WARNING, and the column is stored with precision 6).

// typmodVerdict is the outcome of typmodCheck.
type typmodVerdict int

const (
	typmodOK typmodVerdict = iota
	typmodRefused
	typmodUnknown
)

// typmodTypes are the pg_catalog types with a typmodin function
// (pg_type.dat REL_17_6).
var typmodTypes = map[string]bool{
	"bpchar": true, "varchar": true, "time": true, "timetz": true, "timestamp": true,
	"timestamptz": true, "interval": true, "bit": true, "varbit": true, "numeric": true,
}

// typmodCheck checks the modifiers of a written type. It returns the
// written type as Postgres stores it (the time types' precision above 6
// is reduced to 6), the verdict and, for a refusal or an unknown, the
// reason. A type the pass cannot read, the modifiers of a type outside
// pg_catalog and an interval with a precision above 6 are unknown.
func typmodCheck(written string) (stored string, v typmodVerdict, why string) {
	canon, ok := canonicalType(written)
	if !ok {
		if toks, ok := exprTokens(written); ok && len(toks) > 0 && toks[0].kind == tokIdent && toks[0].value == "float" {
			// canonicalType reads float(p) for p 1 to 53 only (gram.y
			// opt_float: "precision for type float must be at least 1
			// bit", "must be less than 54 bits", 22023).
			if len(toks) == 4 && toks[2].kind == tokNumber {
				return written, typmodRefused, "the precision of float must be 1 to 53"
			}
		}
		return written, typmodUnknown, "the type is not read"
	}
	canon = strings.TrimPrefix(strings.TrimPrefix(canon, "setof "), "pg_catalog.")
	if strings.HasPrefix(canon, "interval") {
		open := strings.Index(canon, "(")
		if open < 0 {
			return written, typmodOK, ""
		}
		inner := strings.TrimSpace(strings.Trim(canon[open:strings.LastIndex(canon, ")")+1], "() "))
		p, err := strconv.Atoi(inner)
		switch {
		case err != nil:
			return written, typmodUnknown, "the interval precision is not read"
		case p < 0:
			return written, typmodRefused, "an interval precision must not be negative"
		case p > 6:
			return written, typmodUnknown, "an interval precision above 6 is reduced to 6"
		}
		return written, typmodOK, ""
	}
	open := strings.Index(canon, "(")
	if open < 0 {
		return written, typmodOK, ""
	}
	base := canon[:open]
	closeAt := strings.Index(canon, ")")
	if closeAt < open {
		return written, typmodUnknown, "the modifiers are not read"
	}
	if !typmodTypes[base] {
		if isCatalogType(base) {
			return written, typmodRefused, fmt.Sprintf("type %s takes no modifiers", base)
		}
		return written, typmodUnknown, fmt.Sprintf("the modifiers of %s are not proven", base)
	}
	var mods []int
	for _, f := range strings.Split(canon[open+1:closeAt], ",") {
		n, err := strconv.Atoi(f)
		if err != nil {
			return written, typmodUnknown, "a modifier is not an integer constant"
		}
		mods = append(mods, n)
	}
	switch base {
	case "varchar", "bpchar", "bit", "varbit":
		limit := 10485760
		if base == "bit" || base == "varbit" {
			limit *= 8
		}
		if len(mods) != 1 || mods[0] < 1 || mods[0] > limit {
			return written, typmodRefused, fmt.Sprintf("the length of %s must be one value from 1 to %d", base, limit)
		}
	case "numeric":
		if len(mods) > 2 || mods[0] < 1 || mods[0] > 1000 || len(mods) == 2 && (mods[1] < -1000 || mods[1] > 1000) {
			return written, typmodRefused, "numeric needs a precision from 1 to 1000 and a scale from -1000 to 1000"
		}
	case "time", "timetz", "timestamp", "timestamptz":
		if len(mods) != 1 || mods[0] < 0 {
			return written, typmodRefused, fmt.Sprintf("the precision of %s must be one value of at least 0", base)
		}
		if mods[0] > 6 {
			// The written text keeps its spelling; only the precision
			// changes, as format_type then prints it.
			i := strings.Index(written, "(")
			j := strings.Index(written, ")")
			if i < 0 || j < i {
				return written, typmodUnknown, "the precision is not read"
			}
			return written[:i] + "(6" + written[j:], typmodOK, ""
		}
	}
	return written, typmodOK, ""
}

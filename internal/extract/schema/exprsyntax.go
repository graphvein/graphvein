package schema

import "strings"

// The syntax-only expression reader (T018a2 Revision 1.6, V1's closed
// subset of gram.y a_expr, used by V7's whole-statement readers): it
// checks that an expression is written in gram.y's grammar without
// resolving names, types or functions. The grammar is closed: a construct
// outside it is not accepted (the caller makes the statement D or may
// fail), never guessed. Column references follow U8 and V3: the first part
// is a ColId, later parts ColLabels; a bare SQL value function keyword is
// that function.

// exprSyntax reads one a_expr at the cursor and reports whether the subset
// grammar accepts it; on success the cursor is just after it.
func exprSyntax(c *cursor) bool {
	r := &exprReader{c: c}
	start := c.i
	if !r.or() {
		c.i = start
		return false
	}
	return true
}

// querySyntax reads one query of the subset (SELECT ...) at the cursor.
func querySyntax(c *cursor) bool {
	r := &exprReader{c: c}
	start := c.i
	if !r.query() {
		c.i = start
		return false
	}
	return true
}

type exprReader struct {
	c *cursor
	// noCase is set inside an IF condition, where read_sql_construct stops
	// at the first THEN (V1).
	noCase bool
}

func (r *exprReader) kw(w ...string) bool { return r.c.acceptKw(w...) }

func (r *exprReader) punct(v string) bool { return r.c.acceptPunct(v) }

func (r *exprReader) op(v string) bool {
	if !r.c.done() && r.c.tok().kind == tokOp && r.c.tok().value == v {
		r.c.i++
		return true
	}
	return false
}

// Operator token classes (scan.l, gram.y): the comparison tokens are one
// nonassociative precedence level, the arithmetic self tokens and Op
// (any other operator) are left associative, and => is EQUALS_GREATER,
// no operator.
const (
	opOther = iota // not an operator the subset reads
	opCompare
	opArith
	opGeneric
)

// opClass classifies an operator token as scan.l returns it. A token
// scan.l would split (an operator ending in + or - without one of
// ~!@#^&|`?%, which scan.l trims and rescans) is not read.
func opClass(t token) int {
	v := t.value
	if t.kind != tokOp || v == "" || v == "::" || v == "=>" {
		return opOther
	}
	for i := 0; i < len(v); i++ {
		if !strings.ContainsRune(opChars, rune(v[i])) {
			return opOther
		}
	}
	if len(v) > 1 && (v[len(v)-1] == '+' || v[len(v)-1] == '-') && !strings.ContainsAny(v, "~!@#^&|`?%") {
		return opOther
	}
	switch v {
	case "<", ">", "=", "<=", ">=", "<>", "!=":
		return opCompare
	case "+", "-", "*", "/", "%", "^":
		return opArith
	}
	return opGeneric
}

func (r *exprReader) or() bool {
	if !r.and() {
		return false
	}
	for r.kw("or") {
		if !r.and() {
			return false
		}
	}
	return true
}

func (r *exprReader) and() bool {
	if !r.not() {
		return false
	}
	for r.kw("and") {
		if !r.not() {
			return false
		}
	}
	return true
}

func (r *exprReader) not() bool {
	if r.kw("not") {
		return r.not()
	}
	return r.is()
}

// is reads a comparison followed by at most one IS [NOT] NULL | TRUE |
// FALSE | DISTINCT FROM comparison (IS is nonassociative in gram.y).
func (r *exprReader) is() bool {
	if !r.compare() {
		return false
	}
	if r.kw("is") {
		r.kw("not")
		switch {
		case r.kw("null"), r.kw("true"), r.kw("false"):
		case r.kw("distinct", "from"):
			return r.compare()
		default:
			return false
		}
	}
	return true
}

// compare reads a predicate with at most one comparison operator (the
// comparison tokens are one nonassociative level of gram.y).
func (r *exprReader) compare() bool {
	if !r.predicate() {
		return false
	}
	if !r.c.done() && opClass(r.c.tok()) == opCompare {
		r.c.i++
		return r.predicate()
	}
	return true
}

// predicate reads an operator expression with at most one [NOT] IN, LIKE,
// ILIKE or BETWEEN (one nonassociative level of gram.y).
func (r *exprReader) predicate() bool {
	if !r.operators() {
		return false
	}
	save := r.c.i
	r.kw("not")
	switch {
	case r.kw("in"):
		if !r.punct("(") {
			return false
		}
		if r.c.peekKw("select") {
			if !r.query() {
				return false
			}
		} else if !r.list() {
			return false
		}
		return r.punct(")")
	case r.kw("like"), r.kw("ilike"):
		return r.operators()
	case r.kw("between"):
		return r.operators() && r.kw("and") && r.operators()
	}
	r.c.i = save
	return true
}

// operators reads prefix and binary operators of the left-associative
// levels (Op, + -, * / %, ^) over primaries.
func (r *exprReader) operators() bool {
	if !r.unary() {
		return false
	}
	for !r.c.done() {
		switch opClass(r.c.tok()) {
		case opArith, opGeneric:
		default:
			return true
		}
		r.c.i++
		if !r.unary() {
			return false
		}
	}
	return true
}

// unary reads prefix operators: + and - (UMINUS) or an Op (qual_Op).
func (r *exprReader) unary() bool {
	for !r.c.done() {
		t := r.c.tok()
		if cl := opClass(t); !(cl == opGeneric || cl == opArith && (t.value == "+" || t.value == "-")) {
			break
		}
		r.c.i++
	}
	return r.postfix()
}

// postfix reads a primary followed by casts (:: Typename).
func (r *exprReader) postfix() bool {
	if !r.primary() {
		return false
	}
	for r.op("::") {
		if !strictType(r.c) {
			return false
		}
	}
	return true
}

// strictType reads a Typename (columnType) whose type modifiers are
// integer constants and whose array bounds are empty or one integer
// (gram.y opt_type_modifiers is an expr_list; the subset reads only
// constants).
func strictType(c *cursor) bool {
	start := c.i
	if columnType(c) != nil {
		c.i = start
		return false
	}
	depth := 0
	want := "" // what the next token inside a group must be
	for j := start; j < c.i; j++ {
		t := c.toks[j]
		switch {
		case t.kind == tokPunct && (t.value == "(" || t.value == "["):
			if depth > 0 {
				c.i = start
				return false
			}
			depth, want = 1, "number"
			if t.value == "[" {
				want = "number or ]"
			}
		case depth == 0:
		case t.kind == tokPunct && (t.value == ")" || t.value == "]"):
			if want == "number" && t.value == ")" || want == "number" && c.toks[j-1].value == "," {
				c.i = start
				return false
			}
			depth = 0
		case t.kind == tokNumber && strings.HasPrefix(want, "number") && integerLiteral(t, c.src):
			want = ","
			if c.toks[j-1].value == "[" {
				want = "]"
			}
		case t.kind == tokPunct && t.value == "," && want == ",":
			want = "number"
		default:
			c.i = start
			return false
		}
	}
	return true
}

// integerLiteral reports an unsigned decimal integer constant.
func integerLiteral(t token, src []byte) bool {
	text := string(src[t.start:t.end])
	if text == "" {
		return false
	}
	for i := 0; i < len(text); i++ {
		if !isDigit(text[i]) {
			return false
		}
	}
	return true
}

// numericToken reports a decimal numeric constant as scan.l reads it
// (decinteger, numeric, real); a number with trailing junk (PostgreSQL 15
// and later refuse it), underscores, or a non-decimal prefix is not read.
func numericToken(t token, src []byte) bool {
	text := string(src[t.start:t.end])
	i, n := 0, len(text)
	digits := func() int {
		k := i
		for i < n && isDigit(text[i]) {
			i++
		}
		return i - k
	}
	before := digits()
	after := 0
	if i < n && text[i] == '.' {
		i++
		after = digits()
	}
	if before == 0 && after == 0 {
		return false
	}
	if i < n && (text[i] == 'e' || text[i] == 'E') {
		i++
		if i < n && (text[i] == '+' || text[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == n
}

// stringLiteral reports a string constant the raw parser reads without
// lexing errors: '...', a dollar-quoted string, N'...', E'...' with the
// escapes escapeValue decodes, B'...' of binary digits and X'...' of hex
// digits. U&'...' (Unicode escapes the scanner checks) is not read.
func stringLiteral(t token, src []byte) bool {
	if t.kind != tokString || t.start >= len(src) {
		return false
	}
	switch src[t.start] {
	case '\'', '$', 'n', 'N':
		return true
	case 'e', 'E':
		_, ok := literalValue(t, src)
		return ok
	case 'b', 'B':
		return strings.Trim(t.value, "01") == ""
	case 'x', 'X':
		return strings.Trim(t.value, "0123456789abcdefABCDEF") == ""
	}
	return false
}

// list reads a_expr [, a_expr]...
func (r *exprReader) list() bool {
	if !r.or() {
		return false
	}
	for r.punct(",") {
		if !r.or() {
			return false
		}
	}
	return true
}

func (r *exprReader) primary() bool {
	c := r.c
	if c.done() {
		return false
	}
	t := c.tok()
	switch t.kind {
	case tokNumber:
		if !numericToken(t, c.src) {
			return false
		}
		c.i++
		return true
	case tokString:
		if !stringLiteral(t, c.src) {
			return false
		}
		c.i++
		return true
	case tokPunct:
		if !r.punct("(") {
			return false
		}
		if c.peekKw("select") {
			if !r.query() {
				return false
			}
			return r.punct(")")
		}
		if !r.list() {
			return false
		}
		return r.punct(")")
	case tokQuoted:
		return r.nameOrCall()
	case tokIdent:
	default:
		return false
	}
	switch {
	case r.kw("true"), r.kw("false"), r.kw("null"):
		return true
	case r.kw("array"):
		if !r.punct("[") {
			return false
		}
		if r.punct("]") {
			return true
		}
		return r.list() && r.punct("]")
	case r.kw("exists"):
		return r.punct("(") && r.query() && r.punct(")")
	case r.kw("cast"):
		return r.punct("(") && r.or() && r.kw("as") && strictType(c) && r.punct(")")
	case !r.noCase && r.kw("case"):
		if !c.peekKw("when") && !r.or() {
			return false
		}
		n := 0
		for r.kw("when") {
			if !r.or() || !r.kw("then") || !r.or() {
				return false
			}
			n++
		}
		if n == 0 {
			return false
		}
		if r.kw("else") && !r.or() {
			return false
		}
		return r.kw("end")
	}
	if _, ok := sqlValueFunctions[t.value]; ok {
		c.i++
		if (t.value == "current_time" || t.value == "current_timestamp" || t.value == "localtime" || t.value == "localtimestamp") && r.punct("(") {
			return !c.done() && c.tok().kind == tokNumber && integerLiteral(c.tok(), c.src) && c.next() && r.punct(")")
		}
		return true
	}
	switch t.value {
	case "coalesce", "greatest", "least", "nullif":
		// func_expr_common_subexpr forms written like calls.
		if c.isKw(0, t.value) && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(" {
			c.i += 2
			return r.list() && r.punct(")")
		}
		return false
	}
	return r.nameOrCall()
}

// nameOrCall reads a column reference (ColId [. ColLabel]...) or a function
// call func_name ( [DISTINCT] [args] | * ).
func (r *exprReader) nameOrCall() bool {
	c := r.c
	start := c.i
	first := c.tok()
	parts := 1
	if first.kind == tokIdent && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(" {
		// An unqualified function name is a type_function_name: no
		// col_name or reserved keyword.
		if cat := pgKeywordCategories[first.value]; cat == 'R' || cat == 'C' {
			return false
		}
		c.i++
	} else {
		if _, err := c.ident(); err != nil {
			return false
		}
		for c.isPunct(".") {
			c.i++
			if _, err := c.label(); err != nil {
				return false
			}
			parts++
		}
	}
	if !c.isPunct("(") {
		return true
	}
	_ = start
	c.i++
	if r.punct(")") {
		return true
	}
	if r.op("*") {
		return r.punct(")")
	}
	r.kw("distinct")
	return r.list() && r.punct(")")
}

// query reads SELECT [DISTINCT] [targets] [FROM items] [WHERE e] [GROUP BY
// list] [ORDER BY e [ASC | DESC], ...] [LIMIT e]. A FROM item is a
// relation name or a function call (func_table, gram.y), each with an
// optional alias.
func (r *exprReader) query() bool {
	c := r.c
	if !r.kw("select") {
		return false
	}
	r.kw("distinct")
	stop := func() bool {
		return c.done() || c.isPunct(")") || c.peekKw("from") || c.peekKw("where") || c.peekKw("group") || c.peekKw("order") || c.peekKw("limit")
	}
	if !stop() {
		target := func() bool {
			if r.op("*") {
				return true
			}
			if (c.tok().kind == tokIdent || c.tok().kind == tokQuoted) && c.i+2 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "." && c.toks[c.i+2].kind == tokOp && c.toks[c.i+2].value == "*" {
				c.i += 3
				return true
			}
			if !r.or() {
				return false
			}
			if r.kw("as") {
				_, err := c.label()
				return err == nil
			}
			// A bare alias: only a plain identifier (no keyword).
			if !c.done() && (c.tok().kind == tokQuoted || c.tok().kind == tokIdent && !sqlKeywords[c.tok().value]) {
				c.i++
			}
			return true
		}
		if !target() {
			return false
		}
		for r.punct(",") {
			if !target() {
				return false
			}
		}
	}
	if r.kw("from") {
		item := func() bool {
			start := c.i
			if _, err := c.name(); err != nil {
				return false
			}
			if c.isPunct("(") {
				// func_table without ORDINALITY: func_application
				// (func_name ( args )), then alias_clause as for a
				// relation.
				c.i = start
				if !r.nameOrCall() {
					return false
				}
			}
			if r.kw("as") {
				_, err := c.ident()
				return err == nil
			}
			if !c.done() && (c.tok().kind == tokQuoted || c.tok().kind == tokIdent && !sqlKeywords[c.tok().value]) {
				c.i++
			}
			return true
		}
		if !item() {
			return false
		}
		for r.punct(",") {
			if !item() {
				return false
			}
		}
	}
	if r.kw("where") && !r.or() {
		return false
	}
	if r.kw("group", "by") && !r.list() {
		return false
	}
	if r.kw("order", "by") {
		item := func() bool {
			if !r.or() {
				return false
			}
			_ = r.kw("asc") || r.kw("desc")
			return true
		}
		if !item() {
			return false
		}
		for r.punct(",") {
			if !item() {
				return false
			}
		}
	}
	if r.kw("limit") && !r.or() {
		return false
	}
	return true
}

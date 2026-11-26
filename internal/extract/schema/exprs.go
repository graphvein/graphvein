package schema

import (
	"strconv"
	"strings"
)

// pgProc is one pg_catalog function overload (pg_proc.dat): argument
// types in canonicalType's form (space-separated), result type,
// provolatile (i, s, v), prokind (f, p, a, w), proretset and whether it is
// variadic.
type pgProc struct {
	args     string
	ret      string
	vol      byte
	kind     byte
	set      bool
	variadic bool
}

// pgType is a pg_catalog base type: typcategory and the provolatile of
// its typinput and typoutput functions ('?' when overloads differ).
type pgType struct {
	category byte
	in, out  byte
}

// pgCast is a pg_cast entry: castcontext (e, a, i), castmethod (f, i, b)
// and the provolatile of its function.
type pgCast struct {
	context byte
	method  byte
	vol     byte
}

// pgOperator is a pg_operator entry: operand types, result type and the
// provolatile of oprcode.
type pgOperator struct {
	left, right, result string
	vol                 byte
}

// Expression proofs (T018a2 Revision 1 and 1.1, class P5): a typed
// expression's result type, and for the expressions Postgres requires to
// be IMMUTABLE (index elements and predicates, generated columns) its
// volatility, proven from closed tables only: literals, casts, column
// references of a table whose facts are known, pg_catalog functions and
// operators (pgcatalog.go, generated from REL_17_6), the SQL value
// functions, and functions outside the set and pg_catalog (E1, whose
// signature fits their use but whose volatility is not known). Anything
// else is not proven, and the statement that needs the proof may fail.

// typed is the proven result of an expression.
type typed struct {
	// typ is the canonical base type (canonicalType without modifiers);
	// empty for an untyped string literal (lit), NULL or an E1 result.
	typ  string
	lit  *string
	null bool
	// any is the result of a function outside the set and pg_catalog (E1):
	// it fits any type, and its volatility is not known.
	any bool
	// emptyArray is ARRAY[] before its cast: it has no type yet.
	emptyArray bool
	vol        byte
}

// exprEnv is what an expression may refer to: the columns of t (nil when
// column references are not allowed, as in DEFAULT) and the migration
// source (to tell string-constant prefixes apart).
type exprEnv struct {
	s   *state
	t   *table
	src []byte
	// fn is set only while proving a LANGUAGE sql body (sqlbody.go,
	// R1b.P5): it adds parameter references, EXISTS, array subscripts
	// and the SB-M8 platform functions to the grammar.
	fn *sqlFnScope
}

// maxVol combines volatilities: v is worse than s is worse than i.
func maxVol(a, b byte) byte {
	rank := func(v byte) int {
		switch v {
		case 'i':
			return 0
		case 's':
			return 1
		}
		return 2
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

// isCatalogType reports a pg_catalog type the closed tables know.
func isCatalogType(typ string) bool {
	_, ok := pgTypes[typ]
	return ok
}

// normalType is canonicalType's base with a pg_catalog qualification
// dropped.
func normalType(written string) (string, bool) {
	base, ok := typeBase(written)
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(base, "pg_catalog."), true
}

// castContext is the context in which Postgres casts from to to: 'i'
// (implicit), 'a' (assignment), 'e' (explicit) or 0 (no cast), from
// pg_cast and the automatic I/O conversions (to a string type in
// assignment context, from one explicitly; parse_coerce.c
// find_coercion_pathway).
func castContext(from, to string) byte {
	if from == to {
		return 'i'
	}
	if c, ok := pgCasts[[2]string{from, to}]; ok {
		return c.context
	}
	ft, okF := pgTypes[from]
	tt, okT := pgTypes[to]
	switch {
	case !okF || !okT:
		return 0
	case tt.category == 'S':
		return 'a'
	case ft.category == 'S':
		return 'e'
	}
	return 0
}

// castVolatility is the volatility of the cast from to to (0 when there is
// none): its function's, 'i' for a binary cast, and the source's output
// and the target's input function for an I/O cast (contain_mutable_functions
// checks CoerceViaIO that way).
func castVolatility(from, to string) byte {
	if from == to {
		return 'i'
	}
	io := func() byte {
		ft, okF := pgTypes[from]
		tt, okT := pgTypes[to]
		if !okF || !okT || ft.out == '?' || tt.in == '?' {
			return 'v'
		}
		return maxVol(ft.out, tt.in)
	}
	if c, ok := pgCasts[[2]string{from, to}]; ok {
		switch c.method {
		case 'f':
			return c.vol
		case 'b':
			return 'i'
		}
		return io()
	}
	if castContext(from, to) == 0 {
		return 0
	}
	return io()
}

// assignable reports whether a value of the proven result r coerces to
// the column type written (canonical, with modifiers) in assignment
// context: NULL always; an untyped string literal when it is valid input
// for the type; a typed value through an implicit or assignment cast; an
// E1 result always.
func (e *exprEnv) assignable(r typed, written string) bool {
	if r.null || r.any {
		return true
	}
	if r.emptyArray {
		return false
	}
	base, ok := normalType(written)
	if !ok {
		return false
	}
	if r.lit != nil {
		return e.validLiteral(*r.lit, written)
	}
	if !isCatalogType(base) && base != r.typ {
		return false
	}
	c := castContext(r.typ, base)
	return c == 'i' || c == 'a'
}

// sqlValueFunctions are gram.y's SQLValueFunction keywords and their
// result types (parse_expr.c transformSQLValueFunction); all are stable.
var sqlValueFunctions = map[string]string{
	"current_date": "date", "current_time": "timetz", "current_timestamp": "timestamptz",
	"localtime": "time", "localtimestamp": "timestamp", "current_role": "name", "current_user": "name",
	"user": "name", "session_user": "name", "current_catalog": "name", "current_schema": "name",
}

// polymorphic reports a pseudo-type an overload resolves by its argument.
func polymorphic(typ string) bool {
	return strings.HasPrefix(typ, "any") || typ == "record" || typ == "internal" || typ == "cstring"
}

// polymorphicMayMatch reports whether a polymorphic parameter may accept
// an argument of the builtin type arg.
func polymorphicMayMatch(param, arg string) bool {
	switch {
	case strings.Contains(param, "range"):
		return strings.HasSuffix(arg, "range")
	case strings.Contains(param, "array"):
		return strings.HasSuffix(arg, "[]")
	case param == "anyenum":
		return false
	case param == "record":
		// A record parameter takes composite types only (Revision 1.4:
		// a pg_catalog base type never reaches it by an implicit cast).
		return arg == "record" || !isCatalogType(arg)
	}
	return true
}

// resolve picks the overload Postgres chooses for argument types args
// (func_select_candidate, conservatively): an exact non-polymorphic match,
// else the only one reachable by implicit casts when no polymorphic
// overload may also accept the arguments. params returns each overload's
// parameter types. It returns -1 when the choice is not proven.
func resolve(n int, params func(int) []string, args []string) int {
	exact, implicit := -1, -1
	exactCount, implicitCount := 0, 0
	polyMay := false
	// Among the candidates reachable by implicit casts, Postgres keeps
	// those with the most exact argument matches (func_select_candidate's
	// first rule); best is that number.
	best := -1
	for i := 0; i < n; i++ {
		ps := params(i)
		if ps == nil || len(ps) != len(args) {
			continue
		}
		isExact, isImplicit, poly := true, true, false
		for k, p := range ps {
			if polymorphic(p) {
				poly = true
				if !polymorphicMayMatch(p, args[k]) {
					isImplicit = false
				}
				isExact = false
				continue
			}
			if p != args[k] {
				isExact = false
				if castContext(args[k], p) != 'i' {
					isImplicit = false
				}
			}
		}
		switch {
		case poly && isImplicit:
			polyMay = true
		case isExact:
			exact, exactCount = i, exactCount+1
		case isImplicit && !poly:
			matches := 0
			for k, p := range ps {
				if p == args[k] {
					matches++
				}
			}
			switch {
			case matches > best:
				best, implicit, implicitCount = matches, i, 1
			case matches == best:
				implicitCount++
			}
		}
	}
	switch {
	case exactCount == 1:
		return exact
	case exactCount == 0 && implicitCount == 1 && !polyMay:
		return implicit
	}
	return -1
}

// prove returns the proven result of the expression toks, or false.
func (e *exprEnv) prove(toks []token) (typed, bool) {
	p := &exprParser{e: e, toks: toks}
	r, ok := p.or()
	if !ok || p.i != len(toks) {
		return typed{}, false
	}
	return r, true
}

// exprParser reads the closed expression grammar of the proofs.
type exprParser struct {
	e    *exprEnv
	toks []token
	i    int
}

func (p *exprParser) kw(words ...string) bool {
	for k, w := range words {
		if p.i+k >= len(p.toks) || p.toks[p.i+k].kind != tokIdent || p.toks[p.i+k].value != w {
			return false
		}
	}
	p.i += len(words)
	return true
}

func (p *exprParser) punct(v string) bool {
	if p.i < len(p.toks) && p.toks[p.i].kind == tokPunct && p.toks[p.i].value == v {
		p.i++
		return true
	}
	return false
}

func (p *exprParser) boolean(r typed) bool {
	return r.null || r.typ == "bool" || r.lit != nil && p.e.validLiteral(*r.lit, "boolean")
}

func (p *exprParser) or() (typed, bool) {
	r, ok := p.and()
	for ok && p.kw("or") {
		var b typed
		b, ok = p.and()
		ok = ok && p.boolean(r) && p.boolean(b)
		r = typed{typ: "bool", vol: maxVol(r.vol, b.vol)}
	}
	return r, ok
}

func (p *exprParser) and() (typed, bool) {
	r, ok := p.not()
	for ok && p.kw("and") {
		var b typed
		b, ok = p.not()
		ok = ok && p.boolean(r) && p.boolean(b)
		r = typed{typ: "bool", vol: maxVol(r.vol, b.vol)}
	}
	return r, ok
}

func (p *exprParser) not() (typed, bool) {
	if p.kw("not") {
		r, ok := p.not()
		return typed{typ: "bool", vol: r.vol}, ok && p.boolean(r)
	}
	return p.is()
}

func (p *exprParser) is() (typed, bool) {
	r, ok := p.compare()
	if !ok {
		return r, false
	}
	if p.kw("is") {
		p.kw("not")
		if !p.kw("null") {
			return r, false
		}
		return typed{typ: "bool", vol: r.vol}, true
	}
	return r, true
}

var comparisonOps = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *exprParser) compare() (typed, bool) {
	l, ok := p.predicate()
	if !ok || p.i >= len(p.toks) || p.toks[p.i].kind != tokOp || !comparisonOps[p.toks[p.i].value] {
		return l, ok
	}
	op := p.toks[p.i].value
	if op == "!=" {
		op = "<>"
	}
	p.i++
	r, ok := p.predicate()
	if !ok {
		return r, false
	}
	return p.operator(op, &l, r)
}

// predicate reads [NOT] IN (list), [NOT] LIKE / ILIKE and [NOT] BETWEEN
// after an operand (Revision 1.4 R1; gram.y gives them a precedence
// between the comparison operators and the other operators). IN over a
// list of constants and values of the operand's own type is proven when
// the type's = operator is (parse_expr.c transformAExprIn: a common type
// then = ANY); LIKE and ILIKE are the ~~ and ~~* operators (gram.y
// a_expr); BETWEEN is >= AND <=. ESCAPE, SIMILAR TO, SYMMETRIC and a
// subquery are not proven.
func (p *exprParser) predicate() (typed, bool) {
	l, ok := p.operators()
	if !ok {
		return l, ok
	}
	save := p.i
	not := p.kw("not")
	switch {
	case p.kw("in"):
		if l.lit != nil || l.null || l.any || l.emptyArray || l.typ == "" || !p.punct("(") {
			return typed{}, false
		}
		vol := l.vol
		for {
			r, ok := p.or()
			if !ok || r.any || r.emptyArray || r.lit == nil && !r.null && r.typ != l.typ {
				return typed{}, false
			}
			eq, ok := p.operator("=", &l, r)
			if !ok || eq.typ != "bool" {
				return typed{}, false
			}
			vol = maxVol(vol, eq.vol)
			if p.punct(")") {
				break
			}
			if !p.punct(",") {
				return typed{}, false
			}
		}
		return typed{typ: "bool", vol: vol}, true
	case p.kw("like"), p.kw("ilike"):
		op := "~~"
		if p.toks[p.i-1].value == "ilike" {
			op = "~~*"
		}
		if not {
			op = "!" + op
		}
		r, ok := p.operators()
		if !ok {
			return typed{}, false
		}
		res, ok := p.operator(op, &l, r)
		return res, ok && res.typ == "bool"
	case p.kw("between"):
		lo, ok := p.operators()
		if !ok || !p.kw("and") {
			return typed{}, false
		}
		hi, ok := p.operators()
		if !ok {
			return typed{}, false
		}
		a, okA := p.operator(">=", &l, lo)
		b, okB := p.operator("<=", &l, hi)
		if !okA || !okB || a.typ != "bool" || b.typ != "bool" {
			return typed{}, false
		}
		return typed{typ: "bool", vol: maxVol(a.vol, b.vol)}, true
	}
	if not {
		p.i = save
	}
	return l, true
}

// operators reads a run of one non-comparison binary operator (a mix of
// operators is not proven: their precedence is not modelled).
func (p *exprParser) operators() (typed, bool) {
	l, ok := p.unary()
	op := ""
	for ok && p.i < len(p.toks) && p.toks[p.i].kind == tokOp && p.toks[p.i].value != "::" && !comparisonOps[p.toks[p.i].value] {
		next := p.toks[p.i].value
		if op != "" && next != op {
			return l, false
		}
		op = next
		p.i++
		var r typed
		r, ok = p.unary()
		if ok {
			l, ok = p.operator(op, &l, r)
		}
	}
	return l, ok
}

// operator resolves a binary operator: an untyped literal takes the other
// side's type (and must be valid input for it).
func (p *exprParser) operator(op string, l *typed, r typed) (typed, bool) {
	if l.any || r.any || l.null && r.null {
		return typed{}, false
	}
	lt, rt := l.typ, r.typ
	switch {
	case l.lit != nil && r.lit != nil:
		return typed{}, false
	case l.lit != nil || l.null:
		lt = rt
		if l.lit != nil && !p.e.validLiteral(*l.lit, rt) {
			return typed{}, false
		}
	case r.lit != nil || r.null:
		rt = lt
		if r.lit != nil && !p.e.validLiteral(*r.lit, lt) {
			return typed{}, false
		}
	}
	cands := pgOperators[op]
	i := resolve(len(cands), func(k int) []string {
		if cands[k].left == "" {
			return nil
		}
		return []string{cands[k].left, cands[k].right}
	}, []string{lt, rt})
	if i < 0 || polymorphic(cands[i].result) {
		return typed{}, false
	}
	vol := maxVol(maxVol(l.vol, r.vol), cands[i].vol)
	// An implicit coercion of an operand runs its cast, whose volatility
	// counts too (contain_mutable_functions; Revision 1.4).
	vol = maxVol(vol, coercionVolatility(lt, cands[i].left))
	vol = maxVol(vol, coercionVolatility(rt, cands[i].right))
	return typed{typ: cands[i].result, vol: vol}, true
}

// coercionVolatility is the volatility of the implicit coercion of a
// typed operand from to a parameter type to: 'i' when none is needed (the
// same type, a polymorphic parameter, or an untyped constant whose input
// function runs at parse time), the cast's otherwise.
func coercionVolatility(from, to string) byte {
	if from == "" || from == to || polymorphic(to) {
		return 'i'
	}
	if v := castVolatility(from, to); v != 0 {
		return v
	}
	return 'v'
}

func (p *exprParser) unary() (typed, bool) {
	if p.i < len(p.toks) && p.toks[p.i].kind == tokOp && (p.toks[p.i].value == "-" || p.toks[p.i].value == "+") && p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokNumber {
		// A signed number constant (gram.y folds the minus into it).
		p.i++
		return p.postfix()
	}
	return p.postfix()
}

func (p *exprParser) postfix() (typed, bool) {
	r, ok := p.primary()
	for ok && p.i < len(p.toks) && p.toks[p.i].kind == tokOp && p.toks[p.i].value == "::" {
		p.i++
		var to string
		to, ok = p.typeName()
		if ok {
			r, ok = p.cast(r, to)
		}
	}
	return r, ok
}

// typeName reads a Typename and returns it as written.
func (p *exprParser) typeName() (string, bool) {
	c := &cursor{toks: p.toks, i: p.i, src: p.e.src}
	start := c.i
	if err := columnType(c); err != nil || c.i == start {
		return "", false
	}
	p.i = c.i
	var b strings.Builder
	for _, t := range p.toks[start:c.i] {
		if b.Len() > 0 && t.kind != tokPunct && !strings.HasSuffix(b.String(), "(") && !strings.HasSuffix(b.String(), ",") {
			b.WriteString(" ")
		}
		if t.kind == tokQuoted {
			b.WriteString(`"` + strings.ReplaceAll(t.value, `"`, `""`) + `"`)
		} else {
			b.WriteString(t.value)
		}
	}
	return b.String(), true
}

// cast applies an explicit cast to the written type.
func (p *exprParser) cast(r typed, written string) (typed, bool) {
	base, ok := normalType(written)
	if !ok {
		return typed{}, false
	}
	switch {
	case r.any:
		return typed{}, false
	case r.emptyArray:
		return typed{typ: base, vol: 'i'}, strings.HasSuffix(base, "[]")
	case r.null:
		return typed{typ: base, vol: 'i'}, isCatalogType(base) || strings.HasSuffix(base, "[]")
	case r.lit != nil:
		// A typed constant: the input function runs now.
		return typed{typ: base, vol: 'i'}, p.e.validLiteral(*r.lit, written)
	}
	v := castVolatility(r.typ, base)
	if v == 0 {
		return typed{}, false
	}
	return typed{typ: base, vol: maxVol(r.vol, v)}, true
}

func (p *exprParser) primary() (typed, bool) {
	if p.i >= len(p.toks) {
		return typed{}, false
	}
	t := p.toks[p.i]
	switch t.kind {
	case tokNumber:
		p.i++
		v := t.value
		if strings.ContainsAny(v, ".eE") {
			return typed{typ: "numeric", vol: 'i'}, true
		}
		if _, err := strconv.ParseInt(v, 10, 32); err == nil {
			return typed{typ: "int4", vol: 'i'}, true
		}
		if _, err := strconv.ParseInt(v, 10, 64); err == nil {
			return typed{typ: "int8", vol: 'i'}, true
		}
		return typed{typ: "numeric", vol: 'i'}, true
	case tokString:
		// B'', X'', U&'' and N'' constants, and E'' strings with escapes,
		// are not proven (literalValue).
		v, ok := literalValue(t, p.e.src)
		if !ok {
			return typed{}, false
		}
		p.i++
		return typed{lit: &v, vol: 'i'}, true
	case tokPunct:
		if !p.punct("(") {
			return typed{}, false
		}
		r, ok := p.or()
		if !ok || !p.punct(")") {
			return typed{}, false
		}
		if p.e.fn != nil && p.i < len(p.toks) && p.toks[p.i].kind == tokPunct && p.toks[p.i].value == "[" {
			return p.fnSubscript(r)
		}
		return r, true
	case tokIdent, tokQuoted:
	default:
		return typed{}, false
	}
	if t.kind == tokIdent {
		switch {
		case p.kw("null"):
			return typed{null: true, vol: 'i'}, true
		case p.kw("true"), p.kw("false"):
			return typed{typ: "bool", vol: 'i'}, true
		case p.i+1 < len(p.toks) && p.toks[p.i].value == "array" && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].value == "[":
			p.i += 2
			if p.punct("]") {
				return typed{emptyArray: true, vol: 'i'}, true
			}
			var elems []typed
			for {
				r, ok := p.or()
				if !ok {
					return typed{}, false
				}
				elems = append(elems, r)
				if p.punct("]") {
					break
				}
				if !p.punct(",") {
					return typed{}, false
				}
			}
			typ := ""
			for _, el := range elems {
				switch {
				case el.any || el.emptyArray || strings.HasSuffix(el.typ, "[]"):
					return typed{}, false
				case el.null || el.lit != nil:
				case typ == "":
					typ = el.typ
				case typ != el.typ:
					return typed{}, false
				}
			}
			if typ == "" {
				typ = "text"
			}
			vol := byte('i')
			for _, el := range elems {
				if el.lit != nil && !p.e.validLiteral(*el.lit, typ) {
					return typed{}, false
				}
				vol = maxVol(vol, el.vol)
			}
			return typed{typ: typ + "[]", vol: vol}, isCatalogType(typ)
		case p.kw("cast"):
			if !p.punct("(") {
				return typed{}, false
			}
			r, ok := p.or()
			if !ok || !p.kw("as") {
				return typed{}, false
			}
			to, ok := p.typeName()
			if !ok || !p.punct(")") {
				return typed{}, false
			}
			return p.cast(r, to)
		}
		if typ, ok := sqlValueFunctions[t.value]; ok {
			p.i++
			if p.punct("(") {
				if p.i >= len(p.toks) || p.toks[p.i].kind != tokNumber {
					return typed{}, false
				}
				p.i++
				if !p.punct(")") {
					return typed{}, false
				}
			}
			return typed{typ: typ, vol: 's'}, true
		}
	}
	if p.e.fn != nil && p.i+3 < len(p.toks) && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].value == "." &&
		p.toks[p.i+3].kind == tokPunct && p.toks[p.i+3].value == "(" {
		return p.fnQualifiedCall()
	}
	if p.e.fn != nil && t.kind == tokIdent && t.value == "exists" && p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].value == "(" {
		return p.fnExists()
	}
	// A function call, a typed literal (type 'string'), or a column.
	if p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokPunct && p.toks[p.i+1].value == "(" {
		return p.call()
	}
	if p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokString && t.kind == tokIdent {
		start := p.i
		if to, ok := p.typeName(); ok && p.i < len(p.toks) && p.toks[p.i].kind == tokString {
			v, ok := literalValue(p.toks[p.i], p.e.src)
			if !ok {
				return typed{}, false
			}
			p.i++
			return p.cast(typed{lit: &v, vol: 'i'}, to)
		}
		p.i = start
		return typed{}, false
	}
	return p.column()
}

// column reads [table.]column of the env's table.
func (p *exprParser) column() (typed, bool) {
	if p.e.fn != nil {
		return p.fnColumn()
	}
	t := p.e.t
	if t == nil || !factsKnown(t) {
		return typed{}, false
	}
	name := p.toks[p.i].value
	p.i++
	if p.punct(".") {
		if p.i >= len(p.toks) || name != t.q.name {
			return typed{}, false
		}
		name = p.toks[p.i].value
		p.i++
	}
	c := t.column(name)
	if c == nil || c.generated != nil {
		return typed{}, false
	}
	base, ok := normalType(writtenType(c))
	if !ok || !isCatalogType(base) {
		return typed{}, false
	}
	return typed{typ: base, vol: 'i'}, true
}

// call proves f(args): COALESCE, NULLIF, GREATEST and LEAST (grammar
// forms over one type), a pg_catalog function whose overload is proven,
// or a function outside the set and pg_catalog (E1).
func (p *exprParser) call() (typed, bool) {
	name := p.toks[p.i].value
	p.i += 2
	var args []typed
	if !p.punct(")") {
		for {
			r, ok := p.or()
			if !ok {
				return typed{}, false
			}
			args = append(args, r)
			if p.punct(")") {
				break
			}
			if !p.punct(",") {
				return typed{}, false
			}
		}
	}
	vol := byte('i')
	for _, a := range args {
		vol = maxVol(vol, a.vol)
	}
	switch name {
	case "coalesce", "greatest", "least", "nullif":
		typ := ""
		for _, a := range args {
			switch {
			case a.any:
				return typed{}, false
			case a.null || a.lit != nil:
			case typ == "":
				typ = a.typ
			case typ != a.typ:
				return typed{}, false
			}
		}
		if typ == "" || len(args) == 0 || name == "nullif" && len(args) != 2 {
			return typed{}, false
		}
		for _, a := range args {
			if a.lit != nil && !p.e.validLiteral(*a.lit, typ) {
				return typed{}, false
			}
		}
		if name != "coalesce" {
			// GREATEST, LEAST and NULLIF compare with the type's operators.
			op := "<"
			if name == "nullif" {
				op = "="
			}
			l := typed{typ: typ, vol: 'i'}
			if _, ok := p.operator(op, &l, typed{typ: typ, vol: 'i'}); !ok {
				return typed{}, false
			}
			cands := pgOperators[op]
			i := resolve(len(cands), func(k int) []string { return []string{cands[k].left, cands[k].right} }, []string{typ, typ})
			vol = maxVol(vol, cands[i].vol)
		}
		return typed{typ: typ, vol: vol}, true
	}
	procs, inCatalog := pgProcs[name]
	if p.e.fn != nil && (!inCatalog || p.e.s.names.created[name]&kindFunction != 0 || p.e.s.ledger[name]&kindFunction != 0) {
		// A LANGUAGE sql body (R1b.P5): only pg_catalog functions the set
		// does not overload; E1 and the set's own functions are not proven.
		return typed{}, false
	}
	if catalogOutsideDat[name] {
		return typed{}, false
	}
	if !inCatalog {
		if p.e.s != nil && (p.e.s.names.creates(name, kindFunction) || p.e.s.ledger[name]&kindFunction != 0) {
			// A function of the set: proven only when it is held, defined
			// once without arguments, and called without arguments; its
			// volatility is not read.
			if ret := p.e.s.functionReturns[name]; ret != "" && ret != "?" && len(args) == 0 && p.e.s.ledger[name]&kindFunction != 0 {
				return typed{typ: ret, vol: 'v'}, true
			}
			return typed{}, false
		}
		// E1: a function outside the set fits its use.
		return typed{any: true, vol: 'v'}, true
	}
	argTypes := make([]string, len(args))
	untyped := false
	for k, a := range args {
		switch {
		case a.any || a.emptyArray:
			return typed{}, false
		case a.null || a.lit != nil:
			untyped = true
		}
		argTypes[k] = a.typ
	}
	if untyped {
		// An untyped constant or NULL argument: proven only when one
		// overload has this many arguments, without polymorphic or
		// variadic parameters, and every argument fits it (the constant is
		// valid input for its parameter).
		found := -1
		for k, pr := range procs {
			ps := strings.Fields(pr.args)
			if len(ps) != len(args) {
				continue
			}
			if found >= 0 || pr.kind != 'f' || pr.set || pr.variadic {
				return typed{}, false
			}
			found = k
			for j, pt := range ps {
				a := args[j]
				switch {
				case polymorphic(pt):
					return typed{}, false
				case a.null:
				case a.lit != nil:
					if !p.e.validLiteral(*a.lit, pt) {
						return typed{}, false
					}
				case a.typ != pt && castContext(a.typ, pt) != 'i':
					return typed{}, false
				default:
					vol = maxVol(vol, coercionVolatility(a.typ, pt))
				}
			}
		}
		if found < 0 || polymorphic(procs[found].ret) {
			return typed{}, false
		}
		return typed{typ: procs[found].ret, vol: maxVol(vol, procs[found].vol)}, true
	}
	i := resolve(len(procs), func(k int) []string {
		pr := procs[k]
		if pr.kind != 'f' || pr.set || pr.variadic {
			return nil
		}
		if pr.args == "" {
			return []string{}
		}
		return strings.Fields(pr.args)
	}, argTypes)
	if i < 0 || polymorphic(procs[i].ret) {
		return typed{}, false
	}
	vol = maxVol(vol, procs[i].vol)
	if procs[i].args != "" {
		for k, pt := range strings.Fields(procs[i].args) {
			vol = maxVol(vol, coercionVolatility(argTypes[k], pt))
		}
	}
	return typed{typ: procs[i].ret, vol: vol}, true
}

// btreeType reports a type with a btree operator class Postgres finds for
// it: a < operator over the type itself, or over a type it is
// binary-coercible to implicitly (varchar uses text's; GetDefaultOpClass).
func btreeType(typ string) bool {
	has := func(t string) bool {
		for _, o := range pgOperators["<"] {
			if o.left == t && o.right == t {
				return true
			}
		}
		return false
	}
	if has(typ) {
		return true
	}
	for k, c := range pgCasts {
		if k[0] == typ && c.method == 'b' && c.context == 'i' && has(k[1]) {
			return true
		}
	}
	return false
}

// catalogOutsideDat are the pg_catalog functions a default cluster has
// that pg_proc.dat does not list: initdb creates the snowball dictionary
// functions (snowball_create.sql) and the plpgsql extension's handlers.
// They are never E1 (H3); their signatures are not proven.
var catalogOutsideDat = map[string]bool{
	"dsnowball_init": true, "dsnowball_lexize": true,
	"plpgsql_call_handler": true, "plpgsql_inline_handler": true, "plpgsql_validator": true,
}

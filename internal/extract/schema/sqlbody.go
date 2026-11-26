package schema

import (
	"strconv"
	"strings"
)

// LANGUAGE sql bodies analysed at creation (T024c1c, precondition R1b.P5).
//
// With check_function_bodies on (E4's default), CREATE FUNCTION runs the
// language's validator, fmgr_sql_validator (pg_proc.c REL_17_6): unless an
// argument is polymorphic, it parses the AS string (pg_parse_query), then
// parse-analyses and rewrites every statement against the catalog as it is
// at that point (pg_analyze_and_rewrite_withcb with sql_fn_parser_setup,
// which makes the parameters visible by name), checks the statements
// (check_sql_fn_statements) and the result (check_sql_fn_retval). The body
// is not executed. Creation therefore succeeds when every relation,
// column, function and operator the body names exists with types that fit
// at that point. Parse analysis prefers a column of a relation in scope
// over a parameter of the same name (functions.c sql_fn_post_column_ref
// runs only when no column matched, at any query level).
//
// sqlQueryBody proves the one form the reader models; any other form keeps
// R1b.P5's "may be refused". The form, kept 1:1 with R1b.P5's T024c1c
// clause in contract.go and docs/extract/schema.md:
//
//   - CREATE [OR REPLACE] FUNCTION f([[IN] p type, ...]) RETURNS type with
//     named parameters and a result of pg_catalog base types (no pseudo,
//     polymorphic or array types, no defaults, no SETOF or TABLE), options
//     LANGUAGE, volatility, STRICT, LEAKPROOF, SECURITY and PARALLEL only
//     (a SET clause changes what the validator resolves), one AS string;
//   - the body is one SELECT of exactly one output expression, with at
//     most one FROM relation (a table the pass holds with known columns
//     under a certain name, not named pg_*, with an optional alias) and an
//     optional WHERE condition, and no other clause;
//   - expressions are the closed expression reader's (exprs.go) plus
//     parameter references by name, EXISTS (subquery of the same form,
//     its output expressions proven but of any type), one integer
//     subscript of a parenthesised array value, and, in a Supabase set,
//     the platform functions of SB-M8 (supabase.go platformFunction);
//     a function of E1 (outside the set and pg_catalog) is never part of
//     a proof here;
//   - the output's type is the RETURNS type (an untyped string constant:
//     text);
//   - the set has applied no opaque statement and creates no operator,
//     cast, operator class or family, access method or aggregate, and no
//     function named like one the body calls, and no statement so far
//     names row_security (with it off, a table under forced row level
//     security makes the rewrite fail).
//
// A BEGIN ATOMIC body is not covered (neither chatbot-ui nor the fixture
// uses one); it keeps R1b.P5.

// sqlFnScope is what a name in one query level of a LANGUAGE sql body may
// mean: a column of the level's relation (t, referenced as ref), a column
// of an enclosing level, or a parameter.
type sqlFnScope struct {
	params map[string]string
	t      *table
	ref    string
	outer  *sqlFnScope
	// platform counts the SB-M8 platform function calls the proof uses.
	platform *int
}

// sqlQueryBody reports whether the CREATE FUNCTION statement toks is of
// the analysed form above and its body is proven.
func (s *state) sqlQueryBody(toks []token) bool {
	if s.opaqueSeen > 0 || s.operatorObjects || s.rowSecurityWord {
		return false
	}
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	c.acceptKw("or", "replace")
	if !c.acceptKw("function") {
		return false
	}
	if _, err := c.name(); err != nil || !c.isPunct("(") {
		return false
	}
	end, err := c.group()
	if err != nil {
		return false
	}
	params := map[string]string{}
	if end > c.i+2 {
		for _, it := range c.splitList(c.i+1, end-1) {
			name, typ, ok := sqlFnParam(&cursor{toks: toks[:it[1]], i: it[0], src: s.m.source})
			if !ok || params[name] != "" {
				return false
			}
			params[name] = typ
		}
	}
	c.i = end
	returns := ""
	var body *string
	for !c.done() {
		switch {
		case c.acceptKw("returns"):
			if returns != "" || c.isKw(0, "setof") || c.isKw(0, "table") {
				return false
			}
			start := c.i
			if err := columnType(c); err != nil {
				return false
			}
			typ, ok := sqlFnType(c.text(start, c.i))
			if !ok {
				return false
			}
			returns = typ
		case c.acceptKw("language"):
			c.i++
		case c.acceptKw("immutable"), c.acceptKw("stable"), c.acceptKw("volatile"), c.acceptKw("strict"), c.acceptKw("leakproof"),
			c.acceptKw("security", "definer"), c.acceptKw("security", "invoker"):
		case c.acceptKw("parallel"):
			c.i++
		case c.acceptKw("as"):
			if body != nil || c.done() {
				return false
			}
			v, ok := literalValue(c.tok(), s.m.source)
			if !ok {
				return false
			}
			body = &v
			c.i++
		default:
			return false
		}
	}
	if body == nil || returns == "" {
		return false
	}
	stmts, fail := lexStatements([]byte(*body))
	if fail != nil || len(stmts) != 1 {
		return false
	}
	platform := 0
	scope := &sqlFnScope{params: params, platform: &platform}
	outs, ok := s.sqlFnQuery(stmts[0].tokens, []byte(*body), scope)
	if !ok || len(outs) != 1 {
		return false
	}
	r := outs[0]
	switch {
	case r.null || r.emptyArray:
		return false
	case r.lit != nil:
		// An untyped constant in the output list resolves to text.
		if returns != "text" {
			return false
		}
	case r.typ != returns:
		return false
	}
	if platform > 0 {
		s.set.supabaseRows["SB-M8"] += platform
	}
	return true
}

// sqlFnParam reads one parameter [IN] name type of the analysed form.
func sqlFnParam(c *cursor) (name, typ string, ok bool) {
	c.acceptKw("in")
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return "", "", false
	}
	if t.kind == tokIdent && (t.value == "out" || t.value == "inout" || t.value == "variadic") {
		return "", "", false
	}
	name = t.value
	c.i++
	start := c.i
	if err := columnType(c); err != nil || !c.done() {
		return "", "", false
	}
	typ, ok = sqlFnType(c.text(start, c.i))
	return name, typ, ok
}

// sqlFnType is the canonical type of a written parameter or result type
// when it is a pg_catalog base type (not a pseudo-type, not an array).
func sqlFnType(written string) (string, bool) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(written)), "setof") {
		return "", false
	}
	base, ok := normalType(written)
	if !ok {
		return "", false
	}
	pt, ok := pgTypes[base]
	if !ok || pt.category == 'P' {
		return "", false
	}
	return base, true
}

// sqlFnClauses are the words that start a clause the analysed SELECT does
// not have.
var sqlFnClauses = map[string]bool{
	"distinct": true, "all": true, "into": true, "group": true, "having": true, "window": true, "order": true,
	"limit": true, "offset": true, "fetch": true, "for": true, "union": true, "intersect": true, "except": true,
	"with": true, "values": true, "returning": true, "join": true, "on": true, "using": true, "natural": true,
	"cross": true, "inner": true, "left": true, "right": true, "full": true, "lateral": true, "only": true,
	"tablesample": true,
}

// sqlFnQuery proves one query level SELECT expr, ... [FROM rel [[AS] a]]
// [WHERE cond] and returns the types of its output expressions.
func (s *state) sqlFnQuery(toks []token, src []byte, scope *sqlFnScope) ([]typed, bool) {
	if len(toks) < 2 || toks[0].kind != tokIdent || toks[0].value != "select" {
		return nil, false
	}
	from, where := -1, -1
	depth := 0
	for j := 1; j < len(toks); j++ {
		t := toks[j]
		if t.kind == tokPunct {
			switch t.value {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
			continue
		}
		if depth != 0 || t.kind != tokIdent {
			continue
		}
		switch {
		case t.value == "from" && from < 0 && where < 0:
			from = j
		case t.value == "where" && where < 0:
			where = j
		case t.value == "from" || t.value == "where" || sqlFnClauses[t.value]:
			return nil, false
		}
	}
	targetEnd := len(toks)
	switch {
	case from >= 0:
		targetEnd = from
	case where >= 0:
		// WHERE without FROM: not the analysed form.
		return nil, false
	}
	level := &sqlFnScope{params: scope.params, outer: scope.outer, platform: scope.platform}
	if scope.t != nil || scope.ref != "" {
		// A nested level: scope is the enclosing level.
		level.outer = scope
	}
	if from >= 0 {
		fromEnd := len(toks)
		if where >= 0 {
			fromEnd = where
		}
		c := &cursor{toks: toks[:fromEnd], i: from + 1, src: src}
		q, err := c.name()
		if err != nil || q.schema == nil && strings.HasPrefix(q.name, "pg_") {
			return nil, false
		}
		ref := q.name
		c.acceptKw("as")
		if !c.done() {
			a, err := c.ident()
			if err != nil {
				return nil, false
			}
			ref = a
		}
		if !c.done() {
			return nil, false
		}
		sc := s.scope(q, ref)
		if sc.t == nil {
			return nil, false
		}
		level.t, level.ref = sc.t, ref
	}
	e := &exprEnv{s: s, t: level.t, src: src, fn: level}
	c := &cursor{toks: toks, src: src}
	var outs []typed
	for _, it := range c.splitList(1, targetEnd) {
		r, ok := e.prove(toks[it[0]:it[1]])
		if !ok || r.any {
			return nil, false
		}
		outs = append(outs, r)
	}
	if where >= 0 {
		r, ok := e.prove(toks[where+1:])
		if !ok || !(&exprParser{e: e}).boolean(r) {
			return nil, false
		}
	}
	return outs, true
}

// fnColumn resolves a name of a LANGUAGE sql body as parse analysis does:
// [ref.]column of the level's relation first; an unqualified name that is
// a column of an enclosing level is not proven (an outer reference); then
// a parameter.
func (p *exprParser) fnColumn() (typed, bool) {
	fn := p.e.fn
	name := p.toks[p.i].value
	p.i++
	col := func(t *table, name string) (typed, bool) {
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
	if p.punct(".") {
		if fn.t == nil || name != fn.ref || p.i >= len(p.toks) || p.toks[p.i].kind != tokIdent && p.toks[p.i].kind != tokQuoted {
			return typed{}, false
		}
		name = p.toks[p.i].value
		p.i++
		return col(fn.t, name)
	}
	if fn.t != nil && fn.t.column(name) != nil {
		return col(fn.t, name)
	}
	for o := fn.outer; o != nil; o = o.outer {
		if o.t != nil && o.t.column(name) != nil {
			return typed{}, false
		}
	}
	if typ, ok := fn.params[name]; ok {
		return typed{typ: typ, vol: 'i'}, true
	}
	return typed{}, false
}

// fnExists proves EXISTS (subquery) in a LANGUAGE sql body: the subquery
// is a query level of the analysed form; EXISTS is boolean.
func (p *exprParser) fnExists() (typed, bool) {
	c := &cursor{toks: p.toks, i: p.i + 1, src: p.e.src}
	end, err := c.group()
	if err != nil {
		return typed{}, false
	}
	if _, ok := p.e.s.sqlFnQuery(p.toks[p.i+2:end-1], p.e.src, p.e.fn); !ok {
		return typed{}, false
	}
	p.i = end
	return typed{typ: "bool", vol: 's'}, true
}

// fnSubscript proves one integer subscript of a parenthesised array value
// (gram.y c_expr: '(' a_expr ')' opt_indirection): the element type
// (array_subscript_handler).
func (p *exprParser) fnSubscript(r typed) (typed, bool) {
	if p.i+2 >= len(p.toks) || p.toks[p.i+1].kind != tokNumber || p.toks[p.i+2].kind != tokPunct || p.toks[p.i+2].value != "]" {
		return typed{}, false
	}
	if _, err := strconv.ParseInt(p.toks[p.i+1].value, 10, 32); err != nil {
		return typed{}, false
	}
	if r.any || r.null || r.lit != nil || !strings.HasSuffix(r.typ, "[]") {
		return typed{}, false
	}
	elem := strings.TrimSuffix(r.typ, "[]")
	if strings.HasSuffix(elem, "[]") || !isCatalogType(elem) {
		return typed{}, false
	}
	p.i += 3
	return typed{typ: elem, vol: r.vol}, true
}

// fnQualifiedCall proves schema.f(arg) in a LANGUAGE sql body: only a
// platform function of SB-M8 with one text argument.
func (p *exprParser) fnQualifiedCall() (typed, bool) {
	schema, name := p.toks[p.i].value, p.toks[p.i+2].value
	arg, ret, ok := p.e.s.platformFunction(schema, name)
	if !ok {
		return typed{}, false
	}
	p.i += 4
	a, ok := p.or()
	if !ok || !p.punct(")") || a.any || a.null || a.emptyArray {
		return typed{}, false
	}
	if a.lit == nil && a.typ != arg {
		return typed{}, false
	}
	*p.e.fn.platform++
	return typed{typ: ret, vol: 'v'}, true
}

// noteRowSecurity records a statement that names row_security, as a word
// or inside a string (a DO body, set_config): from then on, the analysed
// form of R1b.P5 is not proven.
func (s *state) noteRowSecurity(toks []token) {
	for _, t := range toks {
		if t.value == "row_security" || t.kind == tokString && strings.Contains(strings.ToLower(t.value), "row_security") {
			s.rowSecurityWord = true
			return
		}
	}
}

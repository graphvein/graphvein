package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Column references of DML and queries (T018a2 Revision 1.2, class P1
// extended to columns). A statement of row N3 (INSERT, UPDATE, DELETE,
// MERGE, COPY with a column list, ANALYZE with a column list, a SELECT or
// VALUES query) succeeds only when every column reference it contains is
// proven to exist on a relation whose columns the pass knows; otherwise
// Postgres may refuse it (42703, parse_relation.c colNameToVar and
// errorMissingColumn; analyze.c checkInsertTargets) and the statement is
// one that may fail. R1b's queries are proven by simpleQueryProven, which
// already checks every column it reads.
//
// The reader is closed: it proves the statement forms below and sends
// every other form to "not proven". Inside an expression it treats every
// identifier that may be a column reference as one (gram.y columnref:
// ColId, so any word that is not a reserved or type_func_name keyword),
// and it skips only words that cannot be one: reserved keywords of the
// expression syntax it knows, function names before "(", type names after
// "::" or before a string constant (typed literals), and labels after AS.
// Anything else in an expression (subqueries, other reserved words,
// parameters, field selection) is not proven.

// columnScope is one relation an unqualified or qualified column name may
// resolve to: its reference name (the alias, or the relation's own name)
// and its table, nil when the pass does not know its columns (why says
// why).
type columnScope struct {
	ref string
	t   *table
	why string
}

// columnReader checks the column references of one statement.
type columnReader struct {
	s      *state
	src    []byte
	scopes []columnScope
	// why is the first reason a reference is not proven; valueClass is
	// set when it is about a value or a constraint (Revision 1.3, N3.P5)
	// rather than a column reference (N3.P1).
	why        string
	valueClass bool
}

func (r *columnReader) fail(format string, args ...any) bool {
	if r.why == "" {
		r.why = fmt.Sprintf(format, args...)
	}
	return false
}

// expressionKeywords are the reserved and type_func_name keywords
// (kwlist.h REL_17_6) the reader accepts inside an expression: none of
// them can be a column reference, and none of them hides one.
var expressionKeywords = map[string]bool{
	"and": true, "or": true, "not": true, "is": true, "isnull": true, "notnull": true,
	"in": true, "like": true, "ilike": true, "similar": true, "to": true,
	"case": true, "when": true, "then": true, "else": true, "end": true,
	"true": true, "false": true, "null": true, "any": true, "all": true, "some": true,
	"array": true, "cast": true, "system_user": true,
}

// operandEnd reports whether tok ends an operand, so that a following "*"
// is a multiplication.
func operandEnd(tok token) bool {
	switch tok.kind {
	case tokNumber, tokString, tokIdent, tokQuoted:
		return true
	case tokPunct:
		return tok.value == ")" || tok.value == "]"
	}
	return false
}

// expression reports whether every column reference in toks is proven
// against the reader's scopes.
func (r *columnReader) expression(toks []token) bool {
	if len(toks) == 0 {
		return r.fail("an expression is empty")
	}
	for i := 0; i < len(toks); {
		t := toks[i]
		switch t.kind {
		case tokNumber, tokString:
			i++
			continue
		case tokParam:
			return r.fail("it uses a parameter")
		case tokOp:
			if t.value == "::" {
				c := &cursor{toks: toks, i: i + 1, src: r.src}
				if err := columnType(c); err != nil {
					return r.fail("a cast's type could not be read")
				}
				i = c.i
				continue
			}
			if t.value == "*" {
				countStar := i >= 2 && i+1 < len(toks) && toks[i-1].kind == tokPunct && toks[i-1].value == "(" &&
					(toks[i-2].kind == tokIdent || toks[i-2].kind == tokQuoted) && toks[i+1].kind == tokPunct && toks[i+1].value == ")"
				if !countStar && (i == 0 || !operandEnd(toks[i-1])) {
					return r.fail("it uses * where the reader cannot tell what it expands to")
				}
			}
			i++
			continue
		case tokPunct:
			switch t.value {
			case "(", ")", ",", "[", "]", ":":
				i++
				continue
			}
			return r.fail("it selects a field or uses %q", t.value)
		}
		// An identifier, a keyword, or a quoted identifier.
		if t.kind == tokIdent && nonColumnKeywords[t.value] {
			_, value := sqlValueFunctions[t.value]
			switch {
			case t.value == "is":
				i++
				if i < len(toks) && toks[i].kind == tokIdent && toks[i].value == "not" {
					i++
				}
				if i+1 < len(toks) && toks[i].kind == tokIdent && toks[i].value == "distinct" && toks[i+1].kind == tokIdent && toks[i+1].value == "from" {
					i += 2
				}
			case t.value == "as":
				// A label or CAST's type: not a column.
				if i+1 >= len(toks) || toks[i+1].kind != tokIdent && toks[i+1].kind != tokQuoted {
					return r.fail("AS is not followed by a name")
				}
				i += 2
			case value || expressionKeywords[t.value]:
				i++
			default:
				return r.fail("it uses %s, which the reader does not prove", strings.ToUpper(t.value))
			}
			continue
		}
		// A name chain: name [. name]... [. *].
		parts := []string{t.value}
		j := i + 1
		star := false
		for j+1 < len(toks) && toks[j].kind == tokPunct && toks[j].value == "." {
			n := toks[j+1]
			if n.kind == tokOp && n.value == "*" {
				star = true
				j += 2
				break
			}
			if n.kind != tokIdent && n.kind != tokQuoted {
				break
			}
			parts = append(parts, n.value)
			j += 2
		}
		next := token{kind: tokPunct, value: ";"}
		if j < len(toks) {
			next = toks[j]
		}
		switch {
		case !star && next.kind == tokPunct && next.value == "(":
			// A function call: its name is not a column (P1's token rule
			// and E1 cover the function); its arguments are read on.
		case !star && next.kind == tokString:
			// A typed literal (gram.y AexprConst: func_name Sconst).
		default:
			if !r.column(parts, star) {
				return false
			}
		}
		i = j
	}
	return true
}

// column resolves one column reference (parse_relation.c colNameToVar,
// refnameNamespaceItem): an unqualified name must be a column of exactly
// one scope, and every scope's columns must be known; a qualified name
// rel.col (or rel.*) must name a scope by its reference name. A whole-row
// reference or a three-part name is not proven.
func (r *columnReader) column(parts []string, star bool) bool {
	ref := strings.Join(parts, ".")
	if star {
		ref += ".*"
	}
	if len(r.scopes) == 0 {
		return r.fail("it names %s, but no relation is in scope", ref)
	}
	switch {
	case len(parts) == 1 && !star:
		found := 0
		for _, sc := range r.scopes {
			if sc.t == nil {
				return r.fail("it names %s, and the columns of %s are not known (%s)", ref, sc.ref, sc.why)
			}
			if c := sc.t.column(parts[0]); c != nil {
				found++
			}
		}
		switch found {
		case 0:
			return r.fail("column %s does not exist", ref)
		case 1:
			return true
		}
		return r.fail("column reference %s is ambiguous", ref)
	case len(parts) == 1 && star, len(parts) == 2 && !star:
		var match *columnScope
		for k := range r.scopes {
			if r.scopes[k].ref == parts[0] {
				if match != nil {
					return r.fail("%s names more than one relation", parts[0])
				}
				match = &r.scopes[k]
			}
		}
		if match == nil {
			return r.fail("%s does not name a relation of the statement", parts[0])
		}
		if match.t == nil {
			return r.fail("it names %s, and the columns of %s are not known (%s)", ref, match.ref, match.why)
		}
		if star {
			return true
		}
		if match.t.column(parts[1]) == nil {
			return r.fail("column %s does not exist", ref)
		}
		return true
	}
	return r.fail("the reader does not resolve %s", ref)
}

// scope returns the scope of the relation q under the reference name ref:
// its table when the pass holds it with known columns under a certain
// name (as simpleQueryProven), nil with the reason otherwise.
func (s *state) scope(q qname, ref string) columnScope {
	sc := columnScope{ref: ref}
	exact, others := s.lookup(q)
	switch {
	case exact == nil:
		sc.why = "the relation is not a table the pass holds"
	case len(others) > 0:
		sc.why = "the name may mean another table (search_path)"
	case q.schema == nil && (s.pathChanged || s.temps[q.name]):
		sc.why = "the schema of the name is not certain"
	case exact.kindUnknown || !factsKnown(exact):
		sc.why = "the table is partial"
	default:
		sc.t = exact
	}
	return sc
}

// columnReferences applies Revision 1.2 to one N3 statement: a column
// reference that is not proven makes the migration one that may fail.
func (s *state) columnReferences(toks []token, a analysis) {
	if s.failure != nil || len(toks) == 0 || a.query != "" || a.class == classError || a.path != nil {
		// U3 reads a search_path change (set_config's whole-statement form
		// has constant arguments only).
		return
	}
	first := toks[0]
	if first.kind != tokIdent && !(first.kind == tokPunct && first.value == "(") {
		return
	}
	r := &columnReader{s: s, src: s.m.source}
	what := strings.ToUpper(first.value)
	ok := true
	switch first.value {
	case "insert":
		if s.platformInsert(toks) {
			// SB-M7: an insert into a Supabase platform table.
			return
		}
		ok = r.insert(toks)
	case "update":
		ok = r.update(toks)
	case "delete":
		ok = r.delete(toks)
	case "copy":
		ok = r.copy(toks)
	case "analyze", "analyse":
		ok = r.analyze(toks)
	case "merge":
		ok = r.fail("MERGE's column references are not proven")
	case "with":
		ok = r.fail("a WITH query's column references are not proven")
	case "select", "(":
		what = "the query"
		// Revision 1.3: the items of a query without FROM must be
		// constants, except in a call of a set-defined function (O1,
		// whose body E3 covers).
		ok = s.simpleQueryProven(toks) || r.selectNoFrom(toks, a.opaqueKind != "function_call")
		if !ok && r.why == "" {
			r.why = "the query's column references are not proven"
		}
	case "values":
		ok = r.values(toks[1:])
	default:
		return
	}
	if !ok {
		id := "N3.P1"
		if r.valueClass {
			id = "N3.P5"
		}
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, r.why, precondition(id)))
	}
}

// clauseEnd returns the index of the first depth-0 token at or after from
// that is one of the keywords, or len(toks).
func clauseEnd(toks []token, from int, words ...string) int {
	depth := 0
	for j := from; j < len(toks); j++ {
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
		if depth == 0 && t.kind == tokIdent {
			for _, w := range words {
				if t.value == w {
					return j
				}
			}
		}
	}
	return len(toks)
}

// target reads [ONLY] name [*] [[AS] alias] at the cursor; stop lists the
// words that end the target instead of being an alias.
func (r *columnReader) target(c *cursor, stop ...string) (qname, string, bool) {
	c.acceptKw("only")
	q, err := c.name()
	if err != nil {
		return qname{}, "", r.fail("its target could not be read")
	}
	if !c.done() && c.tok().kind == tokOp && c.tok().value == "*" {
		c.i++
	}
	ref := q.name
	if c.acceptKw("as") {
		n, err := c.ident()
		if err != nil {
			return qname{}, "", r.fail("its alias could not be read")
		}
		return q, n, true
	}
	if !c.done() && (c.tok().kind == tokQuoted || c.tok().kind == tokIdent && !nonColumnKeywords[c.tok().value]) {
		for _, w := range stop {
			if c.isKw(0, w) {
				return q, ref, true
			}
		}
		ref = c.tok().value
		c.i++
	}
	return q, ref, true
}

// writable reports whether a statement may write column col of t: a
// generated column takes only DEFAULT and an identity column GENERATED
// ALWAYS needs OVERRIDING (rewriteHandler.c 428C9); neither is proven.
func (r *columnReader) writable(t *table, name string) bool {
	c := t.column(name)
	if c == nil {
		return r.fail("column %s of %s does not exist", name, t.q)
	}
	if c.generated != nil || c.identity != nil && *c.identity == "always" {
		return r.fail("column %s of %s is generated", name, t.q)
	}
	return true
}

// targetColumns checks a written column list ( a, b, ... ): every name a
// plain column of t, none twice (42701).
func (r *columnReader) targetColumns(t *table, c *cursor, write bool) ([]string, bool) {
	end, err := c.group()
	if err != nil {
		return nil, r.fail("its column list could not be read")
	}
	cols, err := c.nameList()
	if err != nil || c.i != end {
		return nil, r.fail("its column list could not be read")
	}
	if t == nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, n := range cols {
		if seen[n] {
			return nil, r.fail("column %s is named twice", n)
		}
		seen[n] = true
		if write {
			if !r.writable(t, n) {
				return nil, false
			}
		} else if t.column(n) == nil {
			return nil, r.fail("column %s of %s does not exist", n, t.q)
		}
	}
	return cols, true
}

// known returns the scope's table, or false with the reason.
func (r *columnReader) known(sc columnScope) (*table, bool) {
	if sc.t == nil {
		return nil, r.fail("the columns of %s are not known (%s)", sc.ref, sc.why)
	}
	return sc.t, true
}

// insert proves INSERT INTO t [AS a] [(cols)] {DEFAULT VALUES | VALUES
// (...), ...} [ON CONFLICT DO NOTHING] [RETURNING items]: the column
// references (N3.P1), then every value written, explicitly or by an
// omitted column's default, and the constraints a new row meets (N3.P5).
func (r *columnReader) insert(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: r.src}
	if !c.acceptKw("into") {
		return r.fail("its target could not be read")
	}
	q, err := c.name()
	if err != nil {
		return r.fail("its target could not be read")
	}
	ref := q.name
	if c.acceptKw("as") {
		n, err := c.ident()
		if err != nil {
			return r.fail("its alias could not be read")
		}
		ref = n
	}
	sc := r.s.scope(q, ref)
	var cols []string
	listed := false
	if c.isPunct("(") {
		if c.i+1 < len(toks) && toks[c.i+1].kind == tokIdent && (toks[c.i+1].value == "select" || toks[c.i+1].value == "values" || toks[c.i+1].value == "with") {
			return r.fail("INSERT ... SELECT is not proven")
		}
		t, ok := r.known(sc)
		if !ok {
			return false
		}
		if cols, ok = r.targetColumns(t, c, true); !ok {
			return false
		}
		listed = true
	}
	var rowItems [][][]token
	switch {
	case c.acceptKw("default", "values"):
		if listed {
			return r.fail("DEFAULT VALUES after a column list is a syntax error")
		}
	case c.acceptKw("values"):
		end := clauseEnd(toks, c.i, "on", "returning")
		n := len(cols)
		if !listed {
			t, ok := r.known(sc)
			if !ok {
				return false
			}
			n = len(t.columns)
		}
		values := &columnReader{s: r.s, src: r.src}
		width, items, ok := values.rows(toks[c.i:end], true)
		if !ok {
			return r.fail("%s", values.why)
		}
		if listed && width != n || !listed && width > n {
			return r.fail("its VALUES rows do not match the target columns")
		}
		if !listed {
			for _, col := range sc.t.columns[:width] {
				if !r.writable(sc.t, col.name) {
					return false
				}
				cols = append(cols, col.name)
			}
		}
		rowItems = items
		c.i = end
	default:
		return r.fail("only DEFAULT VALUES and VALUES are proven")
	}
	doNothing := false
	if c.acceptKw("on", "conflict") {
		if !c.acceptKw("do", "nothing") {
			return r.fail("ON CONFLICT with a conflict target or DO UPDATE is not proven")
		}
		doNothing = true
	}
	if !r.returning(c, sc) {
		return false
	}
	// Revision 1.3: the values of every row and the constraints they meet.
	t := sc.t
	if t == nil {
		return r.failValue("the columns of %s are not known (%s), so the values of a new row are not proven", sc.ref, sc.why)
	}
	ins := &insertRows{rows: max(len(rowItems), 1), nulls: map[*column]bool{}, doNothing: doNothing}
	// V6 (B2): a sequence default of a table proven empty takes the
	// sequence's first value, when nothing in the set may advance or set a
	// sequence otherwise. One row only.
	freshSequence := t.empty && ins.rows == 1 && !r.s.sequenceUse
	for k := range cols {
		col := t.column(cols[k])
		null := true
		for _, row := range rowItems {
			if kc, ok := readConstant(row[k], r.src); !ok || kc.kind != 'z' {
				null = false
			}
		}
		ins.nulls[col] = null && len(rowItems) > 0
	}
	for _, row := range rowItems {
		for k, item := range row {
			col := t.column(cols[k])
			if freshSequence && sequenceDefault(col) && len(item) == 1 && item[0].kind == tokIdent && item[0].value == "default" {
				continue
			}
			if !r.assigned(t, col, item, false) {
				return false
			}
		}
	}
	for _, col := range t.columns {
		if slices.Contains(cols, col.name) {
			continue
		}
		if freshSequence && sequenceDefault(col) {
			continue
		}
		if why, ok := defaultFits(col); !ok {
			return r.failValue("%s", why)
		}
		ins.nulls[col] = col.def == nil
	}
	if why := r.s.writeHazard(t, "insert", nil, ins); why != "" {
		return r.failValue("%s", why)
	}
	return true
}

// rows reads ( e, ... ), ( e, ... ), ... with no relation in scope and
// returns the common width and each row's items; DEFAULT is an item only
// where allowed.
func (r *columnReader) rows(toks []token, defaults bool) (int, [][][]token, bool) {
	c := &cursor{toks: toks, src: r.src}
	width := -1
	var out [][][]token
	for {
		if !c.isPunct("(") {
			return 0, nil, r.fail("a VALUES row could not be read")
		}
		end, err := c.group()
		if err != nil {
			return 0, nil, r.fail("a VALUES row could not be read")
		}
		items := c.splitList(c.i+1, end-1)
		var row [][]token
		for _, it := range items {
			item := toks[it[0]:it[1]]
			row = append(row, item)
			if defaults && len(item) == 1 && item[0].kind == tokIdent && item[0].value == "default" {
				continue
			}
			if !r.expression(item) {
				return 0, nil, false
			}
		}
		if width >= 0 && len(items) != width {
			return 0, nil, r.fail("its VALUES rows differ in length")
		}
		width = len(items)
		out = append(out, row)
		c.i = end
		if c.done() {
			return width, out, true
		}
		if !c.acceptPunct(",") {
			return 0, nil, r.fail("unexpected text after a VALUES row")
		}
	}
}

// values proves a top-level VALUES query: rows of equal length whose items
// are constants (Revision 1.3).
func (r *columnReader) values(toks []token) bool {
	_, rows, ok := r.rows(toks, false)
	if !ok {
		return false
	}
	for _, row := range rows {
		for _, item := range row {
			if !r.output(item, false) {
				return false
			}
		}
	}
	return true
}

// returning reads an optional RETURNING list against the target's scope
// and requires the end of the statement; each item must be *, a column or
// a constant (Revision 1.3).
func (r *columnReader) returning(c *cursor, sc columnScope) bool {
	if c.acceptKw("returning") {
		r.scopes = []columnScope{sc}
		for _, it := range c.splitList(c.i, len(c.toks)) {
			item := c.toks[it[0]:it[1]]
			if len(item) == 1 && item[0].kind == tokOp && item[0].value == "*" {
				if _, ok := r.known(sc); !ok {
					return false
				}
				continue
			}
			if !r.expression(item) || !r.output(item, false) {
				return false
			}
		}
		c.i = len(c.toks)
	}
	if !c.done() {
		return r.fail("unexpected text the reader does not prove")
	}
	return true
}

// update proves UPDATE [ONLY] t [*] [[AS] a] SET col = expr|DEFAULT, ...
// [WHERE expr] [RETURNING items]: the column references (N3.P1), then
// each assigned value, the condition and the constraints the new values
// meet (N3.P5).
func (r *columnReader) update(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: r.src}
	q, ref, ok := r.target(c, "set")
	if !ok {
		return false
	}
	if !c.acceptKw("set") {
		return r.fail("its SET list could not be read")
	}
	sc := r.s.scope(q, ref)
	t, ok := r.known(sc)
	if !ok {
		return false
	}
	r.scopes = []columnScope{sc}
	end := clauseEnd(toks, c.i, "from", "where", "returning")
	seen := map[string]bool{}
	type assignment struct {
		col   *column
		value []token
	}
	var assignments []assignment
	for _, it := range c.splitList(c.i, end) {
		a := toks[it[0]:it[1]]
		if len(a) < 3 || a[0].kind != tokIdent && a[0].kind != tokQuoted || a[0].kind == tokIdent && nonColumnKeywords[a[0].value] ||
			a[1].kind != tokOp || a[1].value != "=" {
			return r.fail("a SET item is not col = expression")
		}
		name := a[0].value
		if seen[name] {
			return r.fail("column %s is assigned twice", name)
		}
		seen[name] = true
		if !r.writable(t, name) {
			return false
		}
		value := a[2:]
		assignments = append(assignments, assignment{t.column(name), value})
		if len(value) == 1 && value[0].kind == tokIdent && value[0].value == "default" {
			continue
		}
		if !r.expression(value) {
			return false
		}
	}
	c.i = end
	if c.isKw(0, "from") {
		return r.fail("UPDATE ... FROM is not proven")
	}
	var where []token
	if c.acceptKw("where") {
		if c.peekKw("current", "of") {
			return r.fail("WHERE CURRENT OF is not proven")
		}
		w := clauseEnd(toks, c.i, "returning")
		where = toks[c.i:w]
		if !r.expression(where) {
			return false
		}
		c.i = w
	}
	if !r.returning(c, sc) {
		return false
	}
	// Revision 1.3.
	var cols []*column
	for _, a := range assignments {
		if !r.assigned(t, a.col, a.value, true) {
			return false
		}
		cols = append(cols, a.col)
	}
	if where != nil && !r.predicate(where) {
		return false
	}
	if why := r.s.writeHazard(t, "update", cols, nil); why != "" {
		return r.failValue("%s", why)
	}
	return true
}

// delete proves DELETE FROM [ONLY] t [*] [[AS] a] [WHERE expr] [RETURNING
// items]: the column references (N3.P1), then the condition and the
// references a deleted row may have (N3.P5).
func (r *columnReader) delete(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: r.src}
	if !c.acceptKw("from") {
		return r.fail("its target could not be read")
	}
	q, ref, ok := r.target(c)
	if !ok {
		return false
	}
	sc := r.s.scope(q, ref)
	if c.isKw(0, "using") {
		return r.fail("DELETE ... USING is not proven")
	}
	var where []token
	if c.acceptKw("where") {
		if c.peekKw("current", "of") {
			return r.fail("WHERE CURRENT OF is not proven")
		}
		r.scopes = []columnScope{sc}
		w := clauseEnd(toks, c.i, "returning")
		where = toks[c.i:w]
		if !r.expression(where) {
			return false
		}
		c.i = w
	}
	if !r.returning(c, sc) {
		return false
	}
	// Revision 1.3.
	if sc.t == nil {
		return r.failValue("the facts of %s are not known (%s), so the references to its rows are not proven", sc.ref, sc.why)
	}
	r.scopes = []columnScope{sc}
	if where != nil && !r.predicate(where) {
		return false
	}
	if why := r.s.writeHazard(sc.t, "delete", nil, nil); why != "" {
		return r.failValue("%s", why)
	}
	return true
}

// copy proves COPY t [(cols)] FROM|TO ...: a column list names distinct
// columns of t (copy.c CopyGetAttnums: 42703, 42701); a WHERE clause or a
// FORCE option names columns too and is not proven. COPY (query) is not
// proven.
func (r *columnReader) copy(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: r.src}
	if c.isPunct("(") {
		return r.fail("COPY of a query is not proven")
	}
	q, err := c.name()
	if err != nil {
		return r.fail("its target could not be read")
	}
	if c.isPunct("(") {
		t, ok := r.known(r.s.scope(q, q.name))
		if !ok {
			return false
		}
		write := c.i+1 < len(toks) && clauseEnd(toks, c.i, "from") < clauseEnd(toks, c.i, "to")
		if _, ok := r.targetColumns(t, c, write); !ok {
			return false
		}
	}
	for _, t := range toks[c.i:] {
		if t.kind == tokIdent && (t.value == "where" || t.value == "force" || t.value == "force_quote" || t.value == "force_not_null" || t.value == "force_null") {
			return r.fail("COPY's %s names columns that are not proven", strings.ToUpper(t.value))
		}
	}
	return true
}

// analyze proves ANALYZE [VERBOSE] [(options)] [t [(cols)], ...]: a column
// list names distinct columns of t (vacuum.c: 42703, 42701).
func (r *columnReader) analyze(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: r.src}
	c.acceptKw("verbose")
	if c.isPunct("(") {
		end, err := c.group()
		if err != nil {
			return r.fail("its options could not be read")
		}
		c.i = end
	}
	for !c.done() {
		q, err := c.name()
		if err != nil {
			return r.fail("its table list could not be read")
		}
		if c.isPunct("(") {
			t, ok := r.known(r.s.scope(q, q.name))
			if !ok {
				return false
			}
			if _, ok := r.targetColumns(t, c, false); !ok {
				return false
			}
		}
		if !c.done() && !c.acceptPunct(",") {
			return r.fail("its table list could not be read")
		}
	}
	return true
}

// selectNoFrom proves SELECT items with no FROM and nothing after the
// target list: no relation is in scope, so no item may name a column.
// With constants set (Revision 1.3) every item must be a constant,
// optionally AS name: a function or operator may raise.
func (r *columnReader) selectNoFrom(toks []token, constants bool) bool {
	if len(toks) < 2 || toks[0].kind != tokIdent || toks[0].value != "select" || clauseEnd(toks, 1, "from") < len(toks) {
		return false
	}
	c := &cursor{toks: toks, src: r.src}
	for _, it := range c.splitList(1, len(toks)) {
		item := toks[it[0]:it[1]]
		if !r.expression(item) || constants && !r.output(item, false) {
			return false
		}
	}
	return true
}

// Column references and expressions of N1 statements (Revision 1.3: they
// follow P1 extended to columns, cited as N1.P5 with COMMENT ON COLUMN).

// n1Columns checks the columns cols that an N1 statement (what) names on
// the relation q: held on a table whose facts are known, or a relation
// the set never creates (E1). A missing column on a complete table is a
// refusal; anything else not proven may fail.
func (s *state) n1Columns(what string, q qname, cols []string) {
	exact, others := s.lookup(q)
	if exact == nil {
		if _, _, found := s.relationAt(q); found || len(others) > 0 {
			s.unknownFact(what + " may be refused: the columns of the relation it names are not known" + precondition("N1.P5"))
		}
		return
	}
	certain := len(others) == 0 && (q.schema != nil || !s.pathChanged && !s.temps[q.name])
	seen := map[string]bool{}
	for _, col := range cols {
		switch {
		case seen[col]:
			s.unknownFact(fmt.Sprintf("%s is refused: column %s is named twice%s", what, col, precondition("N1.P5")))
			return
		case exact.column(col) != nil && factsKnown(exact) && certain:
		case exact.column(col) == nil && complete(exact) && certain:
			s.refuse(exact, fmt.Sprintf("%s: column %s does not exist (Postgres refuses)%s", what, col, precondition("N1.P5")))
			return
		default:
			s.unproven(exact, fmt.Sprintf("%s may be refused: whether %s has column %s is not proven%s", what, q, col, precondition("N1.P5")))
			return
		}
		seen[col] = true
	}
}

// depth0 returns the index of the first depth-0 token at or after from
// that is the keyword w, or -1.
func depth0(toks []token, from int, w string) int {
	if j := clauseEnd(toks, from, w); j < len(toks) {
		return j
	}
	return -1
}

// createTrigger applies Revision 1.3 to CREATE [OR REPLACE] [CONSTRAINT]
// TRIGGER (gram.y CreateTrigStmt): every table the trigger may be on is
// marked (a row written to it may run code), UPDATE OF columns must be
// held (trigger.c CreateTrigger: 42703, 42701), and a WHEN condition,
// which names NEW and OLD columns and must be boolean, is not proven.
func (s *state) createTrigger(toks []token) {
	markAll := func() {
		for _, t := range s.tables {
			t.triggers = true
			t.triggerColumnsUnknown = true
		}
	}
	on := depth0(toks, 0, "on")
	if on < 0 {
		markAll()
		return
	}
	c := &cursor{toks: toks, i: on + 1, src: s.m.source}
	q, err := c.name()
	if err != nil {
		markAll()
		return
	}
	var targets []*table
	for _, t := range s.tables {
		if t.q.name == q.name {
			t.triggers = true
			targets = append(targets, t)
		}
	}
	// P8 (Revision 1.4): the columns the trigger depends on.
	columnsUnknown := func() {
		for _, t := range targets {
			t.triggerColumnsUnknown = true
		}
	}
	what := fmt.Sprintf("CREATE TRIGGER on %s", q)
	s.triggerName(toks, q, what)
	if w := depth0(toks, c.i, "when"); w >= 0 && !rowChangeWhen(toks, w, on) {
		columnsUnknown()
		s.unknownFact(what + " may be refused: its WHEN condition is not proven" + precondition("N1.P5"))
		return
	}
	var cols []string
	for j := 0; j+1 < on; j++ {
		if toks[j].kind != tokIdent || toks[j].value != "update" || toks[j+1].kind != tokIdent || toks[j+1].value != "of" {
			continue
		}
		for k := j + 2; k < on; k++ {
			t := toks[k]
			switch {
			case t.kind == tokPunct && t.value == ",":
				continue
			case t.kind == tokIdent && t.value == "or":
				k = on
			case t.kind == tokIdent || t.kind == tokQuoted:
				cols = append(cols, t.value)
				continue
			default:
				columnsUnknown()
				s.unknownFact(what + " may be refused: its UPDATE OF list could not be read" + precondition("N1.P5"))
				return
			}
			break
		}
	}
	if len(cols) > 0 {
		s.n1Columns(what+" ... UPDATE OF", q, cols)
	}
	for _, t := range targets {
		for _, col := range cols {
			if t.triggerColumns == nil {
				t.triggerColumns = map[string]bool{}
			}
			t.triggerColumns[col] = true
		}
	}
}

// triggerUses reports whether a trigger the set created on t may depend
// on column c (P8, Revision 1.4: trigger.c records a dependency on every
// UPDATE OF column and every column a WHEN condition names).
func triggerUses(t *table, c *column) bool {
	return t.triggerColumnsUnknown || t.triggerColumns[c.name]
}

// policyExpressions applies Revision 1.3 to CREATE POLICY and ALTER
// POLICY: a USING or WITH CHECK expression names columns of the table and
// must be boolean (policy.c); only TRUE or FALSE is proven.
func (s *state) policyExpressions(toks []token) {
	for _, w := range []string{"using", "check"} {
		j := depth0(toks, 0, w)
		if j < 0 {
			continue
		}
		e := toks[j+1:]
		if len(e) >= 3 && e[0].kind == tokPunct && e[0].value == "(" && e[2].kind == tokPunct && e[2].value == ")" &&
			e[1].kind == tokIdent && (e[1].value == "true" || e[1].value == "false") {
			continue
		}
		s.unknownFact("the policy may be refused: its USING or WITH CHECK expression is not proven" + precondition("N1.P5"))
		return
	}
}

// privilegeColumns applies Revision 1.3 to GRANT and REVOKE with column
// lists (gram.y privilege opt_column_list; aclchk.c: 42703 for a column
// the relation does not have): each column must be held on each table
// named after ON [TABLE].
func (s *state) privilegeColumns(toks []token) {
	on := depth0(toks, 0, "on")
	if on < 0 {
		return
	}
	var cols []string
	lists := false
	for j := 1; j < on; j++ {
		if toks[j].kind != tokPunct || toks[j].value != "(" {
			continue
		}
		lists = true
		c := &cursor{toks: toks, i: j, src: s.m.source}
		names, err := c.nameList()
		if err != nil {
			s.unknownFact("the privilege statement may be refused: its column list could not be read" + precondition("N1.P5"))
			return
		}
		cols = append(cols, names...)
		j = c.i - 1
	}
	if !lists {
		return
	}
	c := &cursor{toks: toks, i: on + 1, src: s.m.source}
	c.acceptKw("table")
	for {
		q, err := c.name()
		if err != nil {
			s.unknownFact("the privilege statement may be refused: the relations of its column privileges could not be read" + precondition("N1.P5"))
			return
		}
		// The same column may appear in several privileges' lists.
		var distinct []string
		for _, col := range cols {
			if !slices.Contains(distinct, col) {
				distinct = append(distinct, col)
			}
		}
		s.n1Columns(fmt.Sprintf("the column privilege on %s", q), q, distinct)
		if !c.acceptPunct(",") {
			break
		}
	}
	if !c.isKw(0, "to") && !c.isKw(0, "from") {
		s.unknownFact("the privilege statement may be refused: the text after its relations could not be read" + precondition("N1.P5"))
	}
}

// rowChangeWhen reports the one WHEN condition the pass proves (B2, for
// O1t): WHEN (OLD IS DISTINCT FROM NEW) of a FOR EACH ROW trigger whose
// only event is UPDATE (OLD and NEW are both available, trigger.c
// CreateTrigger 42P17 otherwise; whole-row comparison resolves to the
// record operators at creation).
func rowChangeWhen(toks []token, when, on int) bool {
	words := func(from, to int) string {
		var b []string
		for _, t := range toks[from:to] {
			b = append(b, t.value)
		}
		return strings.Join(b, " ")
	}
	if words(when, len(toks)) != "when ( old is distinct from new ) execute function" && !strings.HasPrefix(words(when, len(toks)), "when ( old is distinct from new ) execute ") {
		return false
	}
	name := 0
	for name < on && !(toks[name].kind == tokIdent && toks[name].value == "trigger") {
		name++
	}
	events, update := 0, false
	for _, t := range toks[min(name+2, on):on] {
		if t.kind == tokIdent && (t.value == "insert" || t.value == "delete" || t.value == "truncate" || t.value == "update") {
			events++
			update = update || t.value == "update"
		}
	}
	return events == 1 && update && strings.Contains(words(on, when), "for each row")
}

// triggerName applies the trigger-name namespace of a table (B2, N1.P2:
// trigger.c CreateTrigger refuses a name the table holds, 42710, unless
// OR REPLACE): on a table whose facts are known the names the set wrote
// are exact; on any other table the set holds, the name is not proven
// free.
func (s *state) triggerName(toks []token, q qname, what string) {
	c := &cursor{toks: toks, src: s.m.source}
	c.acceptKw("create")
	replace := c.acceptKw("or", "replace")
	c.acceptKw("constraint")
	if !c.acceptKw("trigger") {
		return
	}
	name, err := c.ident()
	if err != nil {
		return
	}
	// A table the set does not hold (E1, or P1 by the token rule) keeps
	// the set's own record of trigger names on it (n1names.go).
	s.tableObjectName("trigger", q, name, replace, what)
}

// dropOrAlterTrigger keeps the trigger names of a table exact through
// DROP TRIGGER [IF EXISTS] n ON t and ALTER TRIGGER n ON t RENAME TO m;
// any other form makes the names of every table of that name unknown.
func (s *state) dropOrAlterTrigger(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	drop := c.acceptKw("drop", "trigger")
	if !drop && !c.acceptKw("alter", "trigger") {
		return
	}
	if drop {
		c.acceptKw("if", "exists")
	}
	name, err1 := c.ident()
	var q qname
	var err2 error
	if c.acceptKw("on") {
		q, err2 = c.name()
	} else {
		err2 = fmt.Errorf("no ON")
	}
	if err1 != nil || err2 != nil {
		for _, t := range s.tables {
			t.triggerNamesUnknown = true
		}
		if !drop {
			s.n1.markRenamed("trigger")
		}
		return
	}
	var t *table
	if exact, _ := s.lookup(q); exact != nil {
		t = s.scope(q, q.name).t
		if t == nil {
			exact.triggerNamesUnknown = true
			return
		}
	} else {
		// A table the pass does not hold: the set's record of names.
		c.acceptKw("cascade")
		c.acceptKw("restrict")
		switch {
		case drop && c.done():
			s.n1.remove("trigger", q, name)
		case !drop && c.acceptKw("rename", "to"):
			if to, err := c.ident(); err == nil && c.done() {
				s.n1.remove("trigger", q, name)
				s.n1.add("trigger", q, to)
			} else {
				s.n1.markRenamed("trigger")
			}
		case !drop:
			s.n1.markRenamed("trigger")
		}
		return
	}
	if drop {
		c.acceptKw("cascade")
		c.acceptKw("restrict")
		if c.done() {
			delete(t.triggerNames, name)
			return
		}
	} else if c.acceptKw("rename", "to") {
		if to, err := c.ident(); err == nil && c.done() {
			delete(t.triggerNames, name)
			if t.triggerNames == nil {
				t.triggerNames = map[string]bool{}
			}
			t.triggerNames[to] = true
			return
		}
	}
	t.triggerNamesUnknown = true
}

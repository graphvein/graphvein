package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Values of DML (T018a2 Revision 1.3, class P5 extended to N3 values). A
// data-changing statement succeeds only when, besides N3.P1 (its column
// references), every value it writes or evaluates is one whose success
// the pass proves from known facts, and every constraint the statement can
// trigger is proven satisfied without reading rows. The pass models no
// rows: a table may hold any rows its constraints allow (E2), so a
// statement whose outcome depends on them may fail. Everything outside the
// allowlist below is not proven, citing N3.P5.
//
// Values (V1-V4), checked against the target column's known type:
//
//   - V1 NULL, into a column proven nullable (not NOT NULL, not a primary
//     key, identity or serial column) whose type is a pg_catalog type (a
//     domain's NOT NULL or CHECK is not modelled);
//   - V2 a constant: an integer (optionally negative) into int2, int4,
//     int8 or numeric within the type's range, precision and scale; a
//     decimal into numeric within precision and scale; TRUE or FALSE into
//     boolean; a plain '...' string that is valid input for text,
//     varchar(n) and char(n) (length in characters), int2/int4/int8,
//     numeric, boolean, uuid, date, timestamp or timestamptz in the narrow
//     syntax validConstant checks; optionally cast (::) to the column's own
//     type. Any other type, prefix or syntax is not proven (Postgres
//     converts the constant with the type's input function when the
//     statement is analysed: 22P02, 22003, 22001, 22007, 22008);
//   - V3 DEFAULT, written or implied by an omitted column: no default is
//     V1; a default that is a V1 or V2 constant, or now() or
//     CURRENT_TIMESTAMP into timestamp/timestamptz, or CURRENT_DATE into
//     date. A sequence (serial, identity) or any other default is not
//     proven (nextval can fail, a function's result is not known);
//   - V4 (UPDATE SET only) a column of the target of the same canonical
//     type, NOT NULL when the assigned column is.
//
// Constraints the statement can trigger (write hazards, writeHazard):
//
//   - INSERT: any primary key, unique key or unique index (the rows it
//     may meet are not modelled), exclusion or CHECK constraint, foreign
//     key of the table, generated column, index expression or predicate
//     (evaluated per row), partitioning or partition bound, or a trigger
//     the set created on the table;
//   - UPDATE: as INSERT, but keys and foreign keys only when the
//     statement assigns one of their columns, and any foreign key of
//     another table whose referenced columns it assigns; any inheritance
//     child (its constraints are checked too);
//   - DELETE: any foreign key that references the table (NO ACTION,
//     RESTRICT, CASCADE, SET NULL and SET DEFAULT can all fail), any
//     inheritance child or partitioning, or a trigger.
//
// WHERE clauses and RETURNING lists are evaluated per row: a predicate is
// proven only when it is built from TRUE, FALSE, col IS [NOT] NULL and col
// op constant (op one of = <> != < <= > >=, the constant V2-valid for the
// column's type) or col op col of the same type, with AND, OR, NOT and
// parentheses; a RETURNING item only when it is *, a column or a
// constant, each optionally AS name. A top-level SELECT without FROM or
// VALUES query is proven only when every item is a constant. Anything else
// (functions, operators, casts, subqueries, aggregates and window
// functions, which Postgres refuses in UPDATE and WHERE: 42803, 42P20) is
// not proven.

// failValue records the first reason a value is not proven.
func (r *columnReader) failValue(format string, args ...any) bool {
	if r.why == "" {
		r.why = fmt.Sprintf(format, args...)
		r.valueClass = true
	}
	return false
}

// constant is a V2 constant as written: its kind ('n' integer, 'd'
// decimal, 's' plain string, 'b' boolean, 'z' NULL), its text (with a
// leading "-" for a negative number) and the type of a :: cast, if any.
type constant struct {
	kind byte
	text string
	cast string
}

var (
	integerText = regexp.MustCompile(`^[0-9]+$`)
	decimalText = regexp.MustCompile(`^([0-9]+\.[0-9]*|\.[0-9]+)$`)
)

// readConstant reads a whole expression that is one constant: a number
// (optionally after a unary minus, which gram.y folds into the constant),
// a plain '...' string, TRUE, FALSE or NULL, optionally followed by ::type.
func readConstant(toks []token, src []byte) (constant, bool) {
	var k constant
	i := 0
	neg := false
	if len(toks) > 1 && toks[0].kind == tokOp && toks[0].value == "-" {
		neg = true
		i = 1
	}
	if i >= len(toks) {
		return k, false
	}
	t := toks[i]
	switch {
	case t.kind == tokNumber && integerText.MatchString(t.value):
		k = constant{kind: 'n', text: t.value}
	case t.kind == tokNumber && decimalText.MatchString(t.value):
		k = constant{kind: 'd', text: t.value}
	case neg:
		return k, false
	case t.kind == tokString && t.start < len(src) && src[t.start] == '\'':
		k = constant{kind: 's', text: t.value}
	case t.kind == tokIdent && (t.value == "true" || t.value == "false"):
		k = constant{kind: 'b', text: t.value}
	case t.kind == tokIdent && t.value == "null":
		k = constant{kind: 'z'}
	default:
		return k, false
	}
	if neg {
		k.text = "-" + k.text
	}
	i++
	if i < len(toks) {
		if toks[i].kind != tokOp || toks[i].value != "::" || k.kind == 'z' {
			return k, false
		}
		c := &cursor{toks: toks, i: i + 1, src: src}
		if err := columnType(c); err != nil || c.i != len(toks) {
			return k, false
		}
		var b strings.Builder
		for j := i + 1; j < len(toks); j++ {
			if j > i+1 {
				b.WriteByte(' ')
			}
			b.Write(src[toks[j].start:toks[j].end])
		}
		k.cast = b.String()
	}
	return k, true
}

// valueTypes are the column types V2 proves constants for (canonical
// bases).
var valueTypes = map[string]bool{
	"int2": true, "int4": true, "int8": true, "numeric": true, "text": true, "varchar": true,
	"bpchar": true, "bool": true, "uuid": true, "date": true, "timestamp": true, "timestamptz": true,
}

// plainDateTime is the date and timestamp syntax V2 accepts: an ISO date
// with an optional hh:mm[:ss], no fraction, zone or other form.
var plainDateTime = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}( [0-9]{2}:[0-9]{2}(:[0-9]{2})?)?$`)

// validConstant reports whether the constant k is proven valid for the
// column type written (V2). It is narrower than Postgres's input functions
// on purpose: what it accepts, they accept.
func validConstant(k constant, written string) bool {
	canon, ok := canonicalType(written)
	if !ok {
		return false
	}
	base, ok := normalType(written)
	if !ok || !valueTypes[base] {
		return false
	}
	if k.cast != "" {
		if c, ok := canonicalType(k.cast); !ok || c != canon {
			return false
		}
	}
	e := &exprEnv{}
	switch k.kind {
	case 'n':
		return (base == "int2" || base == "int4" || base == "int8" || base == "numeric") && e.validLiteral(k.text, written)
	case 'd':
		return base == "numeric" && e.validLiteral(k.text, written)
	case 'b':
		return base == "bool"
	case 's':
	default:
		return false
	}
	s := k.text
	switch base {
	case "text", "varchar", "bpchar":
		return e.validLiteral(s, written)
	}
	// Other types: printable ASCII only, no surrounding blanks (Go's and
	// Postgres's notions of white space differ).
	if s == "" || strings.TrimSpace(s) != s {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	switch base {
	case "int2", "int4", "int8":
		return integerText.MatchString(strings.TrimPrefix(s, "-")) && e.validLiteral(s, written)
	case "numeric":
		u := strings.TrimPrefix(s, "-")
		return (integerText.MatchString(u) || decimalText.MatchString(u)) && e.validLiteral(s, written)
	case "bool":
		switch strings.ToLower(s) {
		case "true", "false", "t", "f", "yes", "no", "on", "off":
			return true
		}
		return false
	case "uuid":
		return e.validLiteral(s, written)
	case "date", "timestamp", "timestamptz":
		if !plainDateTime.MatchString(s) || strings.HasPrefix(s, "0000") || base == "date" && len(s) != 10 {
			return false
		}
		return validDateTime(s, base != "date")
	}
	return false
}

// nullable reports V1: NULL into c is proven to succeed.
func nullable(c *column) bool {
	if !c.nullable || c.implied || c.identity != nil || c.generated != nil {
		return false
	}
	base, ok := normalType(c.typ)
	if !ok {
		return false
	}
	return isCatalogType(strings.TrimSuffix(base, "[]"))
}

// constantFits reports whether the constant k is proven to succeed as the
// value of column c (V1 or V2).
func constantFits(k constant, c *column) bool {
	if k.kind == 'z' {
		return nullable(c)
	}
	return validConstant(k, c.typ)
}

// sequenceDefault reports a column whose default takes a value from its
// own sequence: serial or identity (V6, B2).
func sequenceDefault(c *column) bool {
	return c.identity != nil || c.implied
}

// defaultFits reports V3: the default of column c (written DEFAULT, or an
// omitted column) is proven to succeed.
func defaultFits(c *column) (string, bool) {
	switch {
	case c.generated != nil:
		return fmt.Sprintf("column %s is generated", c.name), false
	case c.identity != nil || c.implied:
		return fmt.Sprintf("the default of column %s takes a sequence value, which may fail", c.name), false
	case c.def == nil:
		if nullable(c) {
			return "", true
		}
		return fmt.Sprintf("column %s gets NULL, and it is not proven nullable", c.name), false
	}
	text := *c.def
	toks, ok := exprTokens(text)
	if ok {
		if k, ok := readConstant(toks, []byte(text)); ok && constantFits(k, c) {
			return "", true
		}
		base, _ := normalType(c.typ)
		switch {
		case (base == "timestamptz" || base == "timestamp") && (len(toks) == 1 && toks[0].kind == tokIdent && toks[0].value == "current_timestamp" ||
			len(toks) == 3 && toks[0].kind == tokIdent && toks[0].value == "now" && toks[1].value == "(" && toks[2].value == ")"):
			return "", true
		case base == "date" && len(toks) == 1 && toks[0].kind == tokIdent && toks[0].value == "current_date":
			return "", true
		}
	}
	return fmt.Sprintf("the default of column %s (%s) is not proven to succeed", c.name, text), false
}

// assigned checks one written value for column c of the target t: DEFAULT
// (V3), a constant (V1, V2) or, when column copies are allowed (UPDATE
// SET), a column of t (V4).
func (r *columnReader) assigned(t *table, c *column, item []token, copies bool) bool {
	if len(item) == 1 && item[0].kind == tokIdent && item[0].value == "default" {
		if why, ok := defaultFits(c); !ok {
			return r.failValue("%s", why)
		}
		return true
	}
	if k, ok := readConstant(item, r.src); ok {
		if !constantFits(k, c) {
			return r.failValue("the value %s for column %s (%s) is not proven valid", tokensText(item, r.src), c.name, c.typ)
		}
		return true
	}
	if copies {
		if src := r.columnOf(t, item); src != nil {
			ca, okA := canonicalType(c.typ)
			cb, okB := canonicalType(src.typ)
			base, _ := normalType(c.typ)
			if okA && okB && ca == cb && isCatalogType(strings.TrimSuffix(base, "[]")) && (c.nullable || !src.nullable) {
				return true
			}
			return r.failValue("column %s takes column %s, whose type or nullability is not proven to fit", c.name, src.name)
		}
	}
	return r.failValue("the value %s for column %s is not a value the pass proves", tokensText(item, r.src), c.name)
}

// columnOf returns the column of t that item names (col, or ref.col with
// the reader's single scope), or nil.
func (r *columnReader) columnOf(t *table, item []token) *column {
	isName := func(tk token) bool {
		return tk.kind == tokQuoted || tk.kind == tokIdent && !nonColumnKeywords[tk.value]
	}
	switch {
	case len(item) == 1 && isName(item[0]):
		return t.column(item[0].value)
	case len(item) == 3 && isName(item[0]) && item[1].kind == tokPunct && item[1].value == "." && isName(item[2]):
		if len(r.scopes) == 1 && r.scopes[0].ref == item[0].value {
			return t.column(item[2].value)
		}
	}
	return nil
}

func tokensText(toks []token, src []byte) string {
	if len(toks) == 0 {
		return ""
	}
	return string(src[toks[0].start:toks[len(toks)-1].end])
}

// writeHazard returns why a statement of kind op ("insert", "update",
// "delete") that assigns the columns assigned may trigger a constraint,
// trigger or expression whose outcome depends on rows or values the pass
// does not prove; "" when none.
//
// For an INSERT, ins describes the new rows (B2): a key or plain unique
// index is met when the table is proven empty and one row is written (V5),
// or when ON CONFLICT DO NOTHING makes the key an arbiter (every
// non-deferrable unique index is one without a conflict target,
// plancat.c infer_arbiter_indexes; a conflict skips the row); a foreign
// key with MATCH SIMPLE is met when one of its columns is NULL in every
// row (ri_triggers.c RI_FKey_check: no check when any column is NULL).
func (s *state) writeHazard(t *table, op string, assigned []*column, ins *insertRows) string {
	keyMet := func(deferrable bool) bool {
		return ins != nil && (ins.rows == 1 && t.empty || ins.doNothing && !deferrable)
	}
	touches := func(cols []*column) bool {
		if op == "insert" {
			return true
		}
		for _, c := range cols {
			if slices.Contains(assigned, c) {
				return true
			}
		}
		return false
	}
	if t.triggers {
		return fmt.Sprintf("the set created a trigger on %s", t.q)
	}
	if op == "delete" {
		for _, fk := range s.referencing(t, nil) {
			if o := s.owner(fk); o != nil {
				return fmt.Sprintf("%s references %s, so deleting rows may be refused", o.q, t.q)
			}
			return fmt.Sprintf("a foreign key may reference %s, so deleting rows may be refused", t.q)
		}
		if len(t.children) > 0 || len(t.partitionKey) > 0 {
			return fmt.Sprintf("%s has inheritance children or partitions, whose references are not checked", t.q)
		}
		return ""
	}
	if t.hasChecks || t.unnamedChecks || len(t.checks) > 0 || t.checksUnknown {
		return fmt.Sprintf("%s has a CHECK or EXCLUDE constraint, which the pass does not evaluate", t.q)
	}
	if ins != nil && ins.doNothing {
		// plancat.c infer_arbiter_indexes: ON CONFLICT refuses a table
		// with a deferrable unique constraint (55000), even without a
		// conflict target.
		deferrable := t.primaryKey != nil && t.primaryKey.deferrable
		for _, k := range t.uniques {
			deferrable = deferrable || k.deferrable
		}
		if deferrable {
			return fmt.Sprintf("%s has a deferrable key, which ON CONFLICT refuses", t.q)
		}
	}
	if t.keysUnknown || t.pkMaybe {
		return fmt.Sprintf("the keys of %s are not known", t.q)
	}
	if t.primaryKey != nil && touches(t.primaryKey.cols) && !keyMet(t.primaryKey.deferrable) {
		return fmt.Sprintf("%s has a primary key over the written columns, and its rows are not modelled", t.q)
	}
	for _, k := range t.uniques {
		if touches(k.cols) && !keyMet(k.deferrable) {
			return fmt.Sprintf("%s has a unique key over the written columns, and its rows are not modelled", t.q)
		}
	}
	for _, ix := range t.indexes {
		expr := ix.where != nil || slices.Contains(ix.columns, nil)
		if expr {
			return fmt.Sprintf("an index of %s has an expression or a predicate, which is evaluated per row", t.q)
		}
		if ix.unique && touches(ix.columns) && !keyMet(false) {
			return fmt.Sprintf("%s has a unique index over the written columns, and its rows are not modelled", t.q)
		}
	}
	for _, fk := range t.foreignKeys {
		if ins != nil && !fk.written.matchNotSimple && slices.ContainsFunc(fk.local, func(c *column) bool { return ins.nulls[c] }) {
			continue
		}
		if fk.table == nil || touches(fk.local) {
			return fmt.Sprintf("%s has a foreign key over the written columns, whose referenced rows are not modelled", t.q)
		}
	}
	if op == "update" {
		for _, fk := range s.referencing(t, nil) {
			if fk.table == nil || touches(fk.columns) {
				return fmt.Sprintf("a foreign key references the assigned columns of %s", t.q)
			}
		}
		if len(t.children) > 0 {
			return fmt.Sprintf("%s has inheritance children, whose constraints are not checked", t.q)
		}
	}
	for _, c := range t.columns {
		if c.generated != nil {
			return fmt.Sprintf("%s has a generated column, which is computed per row", t.q)
		}
	}
	if len(t.partitionKey) > 0 || t.partitionOf != nil {
		return fmt.Sprintf("%s is partitioned or a partition, so a row may fit no partition", t.q)
	}
	return ""
}

// insertRows describes the rows an INSERT writes, for writeHazard: how
// many, the columns proven NULL in every row, and ON CONFLICT DO NOTHING.
type insertRows struct {
	rows      int
	nulls     map[*column]bool
	doNothing bool
}

// predicate proves a WHERE condition (see the comment at the top).
func (r *columnReader) predicate(toks []token) bool {
	p := &predicateParser{r: r, toks: toks}
	if !p.or() || p.i != len(toks) {
		return r.failValue("the condition %s is not one the pass proves", tokensText(toks, r.src))
	}
	return true
}

type predicateParser struct {
	r    *columnReader
	toks []token
	i    int
}

func (p *predicateParser) kw(w string) bool {
	if p.i < len(p.toks) && p.toks[p.i].kind == tokIdent && p.toks[p.i].value == w {
		p.i++
		return true
	}
	return false
}

func (p *predicateParser) or() bool {
	if !p.and() {
		return false
	}
	for p.kw("or") {
		if !p.and() {
			return false
		}
	}
	return true
}

func (p *predicateParser) and() bool {
	if !p.not() {
		return false
	}
	for p.kw("and") {
		if !p.not() {
			return false
		}
	}
	return true
}

func (p *predicateParser) not() bool {
	if p.kw("not") {
		return p.not()
	}
	if p.i < len(p.toks) && p.toks[p.i].kind == tokPunct && p.toks[p.i].value == "(" {
		p.i++
		if !p.or() || p.i >= len(p.toks) || p.toks[p.i].kind != tokPunct || p.toks[p.i].value != ")" {
			return false
		}
		p.i++
		return true
	}
	return p.atom()
}

// operand reads a column of the single scope or a constant: [lo, hi).
func (p *predicateParser) operand() (*column, *constant, bool) {
	if len(p.r.scopes) != 1 || p.r.scopes[0].t == nil {
		return nil, nil, false
	}
	t := p.r.scopes[0].t
	for _, n := range []int{3, 1} {
		if p.i+n <= len(p.toks) {
			if c := p.r.columnOf(t, p.toks[p.i:p.i+n]); c != nil {
				p.i += n
				return c, nil, true
			}
		}
	}
	// A constant: one token, or a minus and a number.
	for _, n := range []int{2, 1} {
		if p.i+n <= len(p.toks) {
			if k, ok := readConstant(p.toks[p.i:p.i+n], p.r.src); ok && k.kind != 'z' && k.cast == "" {
				p.i += n
				return nil, &k, true
			}
		}
	}
	return nil, nil, false
}

var comparisons = map[string]bool{"=": true, "<>": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true}

func (p *predicateParser) atom() bool {
	if p.kw("true") || p.kw("false") {
		return true
	}
	left, lk, ok := p.operand()
	if !ok || left == nil {
		_ = lk
		return false
	}
	base, okB := normalType(left.typ)
	if !okB || !valueTypes[base] {
		return false
	}
	if p.kw("is") {
		p.kw("not")
		return p.kw("null")
	}
	if p.i >= len(p.toks) || p.toks[p.i].kind != tokOp || !comparisons[p.toks[p.i].value] {
		return false
	}
	p.i++
	right, rk, ok := p.operand()
	switch {
	case !ok:
		return false
	case right != nil:
		a, okA := canonicalType(left.typ)
		b, okC := canonicalType(right.typ)
		return okA && okC && a == b
	default:
		return validConstant(*rk, left.typ)
	}
}

// output proves one RETURNING item or one item of a SELECT without FROM: *
// (with a scope), a column of the scope or a constant, optionally AS name.
func (r *columnReader) output(item []token, star bool) bool {
	if len(item) >= 2 && item[len(item)-2].kind == tokIdent && item[len(item)-2].value == "as" &&
		(item[len(item)-1].kind == tokIdent || item[len(item)-1].kind == tokQuoted) {
		item = item[:len(item)-2]
	}
	if len(item) == 1 && item[0].kind == tokOp && item[0].value == "*" && star {
		return true
	}
	if k, ok := readConstant(item, r.src); ok && k.cast == "" {
		return true
	}
	if len(r.scopes) == 1 && r.scopes[0].t != nil && r.columnOf(r.scopes[0].t, item) != nil {
		return true
	}
	return r.failValue("the output item %s is not one the pass proves", tokensText(item, r.src))
}

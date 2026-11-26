package tableaccess

import (
	"fmt"
	"sort"
)

// This file is the SQL subset reader (row CT-Q1): it reads one SELECT,
// INSERT, UPDATE, DELETE or WITH statement and lists its table references
// with their direction. It resolves no name: CT-Q2 (resolve.go) binds the
// references to a schema source. Anything outside the subset is refused as
// a whole (Refusal set, no references), never read in part.

// Ref is one table reference of a statement.
type Ref struct {
	// Schema is the qualifying schema ("" when unqualified); Name the
	// relation name. Both are folded to lower case unless quoted.
	Schema, Name string
	// Write is true for an INSERT, UPDATE or DELETE target; every other
	// reference reads (a target is not also read).
	Write bool
	// Operation is select, insert, update, delete or upsert.
	Operation string
	// CTE is true when an unqualified name names a CTE visible at the
	// reference: it shadows any table (CT-Q2) and claims nothing.
	CTE bool
}

// Reading is the reader's result for one SQL text.
type Reading struct {
	Refs []Ref
	// Forms are the CT-Q1 forms the statement used, sorted.
	Forms []string
	// Refusal is the reason detail when the text is outside the subset.
	Refusal string
}

// readSQL is row CT-Q1's branch: the subset reader over one SQL text.
func readSQL(sql string) Reading {
	tokens, err := lex(sql)
	if err != nil {
		return Reading{Refusal: err.Error()}
	}
	end := len(tokens)
	for i, t := range tokens {
		if t.kind == tokPunct && t.value == ";" {
			if i != len(tokens)-1 {
				return Reading{Refusal: "several statements"}
			}
			end = i
		}
	}
	if end == 0 {
		return Reading{Refusal: "no statement"}
	}
	p := &parser{t: tokens[:end], forms: map[string]bool{}}
	if !p.matchParens() {
		return Reading{Refusal: "unbalanced parentheses"}
	}
	if !p.kw(0, "select", "insert", "update", "delete", "with") {
		return Reading{Refusal: fmt.Sprintf("statement kind %s", describeToken(tokens[0]))}
	}
	p.statement(0, end, nil)
	if p.err != "" {
		return Reading{Refusal: p.err}
	}
	r := Reading{Refs: p.refs}
	for f := range p.forms {
		r.Forms = append(r.Forms, f)
	}
	sort.Strings(r.Forms)
	return r
}

func describeToken(t token) string {
	if t.kind == tokIdent {
		return t.value
	}
	return fmt.Sprintf("starting at byte %d", t.start)
}

type parser struct {
	t     []token
	match []int
	refs  []Ref
	forms map[string]bool
	err   string
}

func (p *parser) fail(format string, args ...any) {
	if p.err == "" {
		p.err = fmt.Sprintf(format, args...)
	}
}

func (p *parser) form(f string) { p.forms[f] = true }

// matchParens records the closing index of every "(".
func (p *parser) matchParens() bool {
	p.match = make([]int, len(p.t))
	var stack []int
	for i, t := range p.t {
		p.match[i] = -1
		if t.kind != tokPunct {
			continue
		}
		switch t.value {
		case "(":
			stack = append(stack, i)
		case ")":
			if len(stack) == 0 {
				return false
			}
			p.match[stack[len(stack)-1]] = i
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack) == 0
}

// kw reports whether token i is an unquoted identifier equal to one of
// words (keywords are never quoted).
func (p *parser) kw(i int, words ...string) bool {
	if i < 0 || i >= len(p.t) || p.t[i].kind != tokIdent {
		return false
	}
	for _, w := range words {
		if p.t[i].value == w {
			return true
		}
	}
	return false
}

func (p *parser) punct(i int, v string) bool {
	return i >= 0 && i < len(p.t) && p.t[i].kind == tokPunct && p.t[i].value == v
}

func (p *parser) isName(i int) bool {
	return i < len(p.t) && (p.t[i].kind == tokQuoted || p.t[i].kind == tokIdent && !reserved[p.t[i].value])
}

// reserved are the words that end or structure a clause and are never read
// as a table or alias name unquoted.
var reserved = map[string]bool{}

func init() {
	for _, w := range []string{"select", "insert", "update", "delete", "with", "from", "where", "group", "having", "window",
		"order", "limit", "offset", "fetch", "for", "union", "intersect", "except", "returning", "on", "join", "inner",
		"left", "right", "full", "cross", "natural", "using", "set", "values", "into", "lateral", "only", "as", "and",
		"or", "not", "default", "table", "all", "distinct", "case", "when", "then", "else", "end", "outer"} {
		reserved[w] = true
	}
}

// keywordFunctions are the reserved words Postgres reads as a function in
// FROM position (SQL-standard functions with no parentheses): FROM user is
// FROM current_user, a one-row result, never a table (R5-B1). Quoted
// ("user") they are names like any other.
var keywordFunctions = map[string]bool{
	"user": true, "current_user": true, "session_user": true, "current_role": true, "current_date": true,
	"current_time": true, "current_timestamp": true, "localtime": true, "localtimestamp": true,
	"current_catalog": true, "current_schema": true, "system_user": true,
}

// keywordFunction reports an unquoted keyword function at i.
func (p *parser) keywordFunction(i int) bool {
	return i < len(p.t) && p.t[i].kind == tokIdent && keywordFunctions[p.t[i].value]
}

// statement reads the statement in tokens [i, end) with the CTE names of
// scope visible.
func (p *parser) statement(i, end int, scope []string) {
	if p.kw(i, "with") {
		p.form("WITH")
		i, scope = p.with(i, end, scope)
		if p.err != "" {
			return
		}
	}
	switch {
	case p.kw(i, "select"):
		p.form("SELECT")
		p.body(i+1, end, scope, &clause{kind: "select"})
	case p.kw(i, "values"):
		p.body(i+1, end, scope, &clause{kind: "select"})
	case p.kw(i, "insert"):
		p.insert(i, end, scope)
	case p.kw(i, "update"):
		p.update(i, end, scope)
	case p.kw(i, "delete"):
		p.delete(i, end, scope)
	case p.punct(i, "("):
		// A parenthesised query, possibly with set operations after it.
		p.body(i, end, scope, &clause{kind: "select"})
	default:
		if i < end {
			p.fail("statement kind %s", describeToken(p.t[i]))
		} else {
			p.fail("empty statement")
		}
	}
}

// clause is what a statement body's scan needs to know about its
// statement.
type clause struct {
	kind string // select, insert, update, delete
	// target is the index in p.refs of the INSERT target, for upsert.
	target int
}

// with reads a WITH list at i and returns the index after it and the scope
// the main statement sees.
func (p *parser) with(i, end int, scope []string) (int, []string) {
	i++
	recursive := p.kw(i, "recursive")
	if recursive {
		i++
	}
	type cte struct {
		name       string
		start, end int
	}
	var ctes []cte
	for {
		if !p.isName(i) {
			p.fail("CTE name expected")
			return end, scope
		}
		name := p.t[i].value
		i++
		if p.punct(i, "(") {
			i = p.match[i] + 1
		}
		if !p.kw(i, "as") {
			p.fail("CTE AS expected")
			return end, scope
		}
		i++
		if p.kw(i, "not") {
			i++
		}
		if p.kw(i, "materialized") {
			i++
		}
		if !p.punct(i, "(") || p.match[i] >= end {
			p.fail("CTE body expected")
			return end, scope
		}
		ctes = append(ctes, cte{name, i + 1, p.match[i]})
		i = p.match[i] + 1
		if p.kw(i, "search", "cycle") {
			p.fail("CTE %s clause", p.t[i].value)
			return end, scope
		}
		if !p.punct(i, ",") {
			break
		}
		i++
	}
	all := append([]string{}, scope...)
	for _, c := range ctes {
		all = append(all, c.name)
	}
	for k, c := range ctes {
		visible := append([]string{}, scope...)
		if recursive {
			visible = all
		} else {
			for _, before := range ctes[:k] {
				visible = append(visible, before.name)
			}
		}
		if p.kw(c.start, "insert", "update", "delete") {
			p.form("writable CTEs")
		}
		p.statement(c.start, c.end, visible)
	}
	return i, all
}

// body scans a statement's tokens at its own depth: FROM (and USING in a
// DELETE) start a FROM list, parentheses may hold subqueries, and the
// clauses with a direction rule are tagged.
func (p *parser) body(i, end int, scope []string, c *clause) {
	for i < end && p.err == "" {
		switch {
		case p.punct(i, "("):
			p.paren(i, scope)
			i = p.match[i] + 1
		case p.kw(i, "from") && !p.distinctFrom(i):
			if c.kind == "update" {
				p.form("UPDATE t ... [FROM ...]")
			} else {
				p.form("FROM")
			}
			i = p.fromList(i+1, end, scope)
		case p.kw(i, "using") && c.kind == "delete":
			p.form("DELETE FROM t [USING ...]")
			i = p.fromList(i+1, end, scope)
		case p.kw(i, "for") && c.kind == "select" && p.lock(i+1):
			i++
		case p.kw(i, "into") && c.kind == "select":
			p.fail("SELECT INTO")
		case p.kw(i, "on") && p.kw(i+1, "conflict") && c.kind == "insert":
			p.form("ON CONFLICT ... DO NOTHING / DO UPDATE")
			for j := i + 2; j < end; j++ {
				if p.kw(j, "do") && p.kw(j+1, "update") {
					p.refs[c.target].Operation = "upsert"
				}
				if p.kw(j, "excluded") {
					p.form("EXCLUDED")
				}
			}
			i += 2
		case p.kw(i, "returning"):
			p.form("RETURNING")
			i++
		case p.kw(i, "with") && p.cteShape(i):
			p.fail("WITH inside a statement")
		case p.kw(i, "join", "tablesample", "lateral"):
			p.fail("%s outside a FROM list", p.t[i].value)
		default:
			i++
		}
	}
}

// cteShape reports whether WITH at i starts a CTE list rather than a
// "with time zone" type or "with ordinality".
func (p *parser) cteShape(i int) bool {
	j := i + 1
	if p.kw(j, "recursive") {
		return true
	}
	if !p.isName(j) {
		return false
	}
	j++
	if p.punct(j, "(") {
		j = p.match[j] + 1
	}
	return p.kw(j, "as")
}

// distinctFrom reports whether FROM at i belongs to IS [NOT] DISTINCT FROM.
func (p *parser) distinctFrom(i int) bool {
	return p.kw(i-1, "distinct") && p.kw(i-2, "is", "not")
}

// lock tags a locking clause starting at i (after FOR); lock clauses
// read. It reports whether i starts one.
func (p *parser) lock(i int) bool {
	switch {
	case p.kw(i, "update"):
		p.form("FOR UPDATE")
	case p.kw(i, "no") && p.kw(i+1, "key") && p.kw(i+2, "update"):
		p.form("NO KEY UPDATE")
	case p.kw(i, "share"):
		p.form("SHARE")
	case p.kw(i, "key") && p.kw(i+1, "share"):
		p.form("KEY SHARE")
	default:
		return false
	}
	return true
}

// paren reads the parenthesised group at i: a subquery when it starts a
// query, else an expression whose own parentheses may hold subqueries.
// FROM inside an expression (EXTRACT(x FROM y)) is not a clause.
func (p *parser) paren(i int, scope []string) {
	end := p.match[i]
	if p.kw(i+1, "select", "with", "values") {
		p.form("subqueries")
		p.statement(i+1, end, scope)
		return
	}
	for j := i + 1; j < end && p.err == ""; j++ {
		if p.punct(j, "(") {
			p.paren(j, scope)
			j = p.match[j]
		}
	}
}

// fromEnd reports whether token i ends a FROM list.
func (p *parser) fromEnd(i int) bool {
	if p.kw(i, "where", "group", "having", "window", "order", "limit", "offset", "fetch", "for", "union",
		"intersect", "except", "returning", "into") {
		return true
	}
	return p.kw(i, "on") && p.kw(i+1, "conflict")
}

// fromList reads a FROM (or UPDATE FROM, DELETE USING) list from i and
// returns the index of the token that ends it.
func (p *parser) fromList(i, end int, scope []string) int {
	expect := true
	for i < end && p.err == "" {
		if p.fromEnd(i) {
			return i
		}
		if expect {
			i = p.item(i, end, scope)
			expect = false
			if p.kw(i, "as") || p.isName(i) && !p.kw(i, "ordinality") {
				p.form("alias")
			}
			continue
		}
		switch {
		case p.punct(i, ","):
			p.form("comma joins")
			expect = true
			i++
		case p.kw(i, "join"):
			p.form("JOIN")
			expect = true
			i++
		case p.punct(i, "("):
			p.paren(i, scope)
			i = p.match[i] + 1
		case p.kw(i, "tablesample", "lateral"):
			p.fail("%s in a FROM list", p.t[i].value)
		default:
			i++
		}
	}
	return i
}

// item reads one FROM item at i and returns the index after it.
func (p *parser) item(i, end int, scope []string) int {
	lateral := p.kw(i, "lateral")
	if lateral {
		i++
	}
	switch {
	case p.kw(i, "only"):
		p.form("ONLY")
		i++
		if p.punct(i, "(") {
			close := p.match[i]
			if j := p.name(i+1, scope); j != close {
				p.fail("ONLY (...) form")
			}
			return close + 1
		}
		return p.name(i, scope)
	case p.punct(i, "("):
		close := p.match[i]
		if p.kw(i+1, "select", "with", "values") {
			p.form("subqueries")
			p.statement(i+1, close, scope)
			return close + 1
		}
		if lateral {
			p.fail("LATERAL form")
			return end
		}
		// A parenthesised join.
		if j := p.fromList(i+1, close, scope); j != close {
			p.fail("parenthesised FROM item")
		}
		return close + 1
	case p.kw(i, "rows") && p.kw(i+1, "from") && p.punct(i+2, "("):
		p.form("unnest(...)")
		p.paren(i+2, scope)
		return p.match[i+2] + 1
	case p.isName(i):
		j := i + 1
		for p.punct(j, ".") && p.isName(j+1) {
			j += 2
		}
		if p.punct(j, "(") {
			// A function call in FROM (unnest, set-returning functions): no
			// table.
			p.form("unnest(...)")
			p.paren(j, scope)
			return p.match[j] + 1
		}
		if p.keywordFunction(i) && j == i+1 {
			// FROM user, FROM current_date: a function without parentheses,
			// no table (R5-B1).
			p.form("unnest(...)")
			return j
		}
		if lateral {
			p.fail("LATERAL form")
			return end
		}
		return p.name(i, scope)
	}
	if i < end {
		p.fail("FROM item %s", describeToken(p.t[i]))
	} else {
		p.fail("FROM item expected")
	}
	return end
}

// name reads a [schema.]name table reference at i as a read and returns
// the index after it.
func (p *parser) name(i int, scope []string) int {
	ref, j, ok := p.relation(i)
	if !ok {
		return len(p.t)
	}
	ref.Operation = "select"
	if ref.Schema == "" {
		for _, c := range scope {
			if c == ref.Name {
				ref.CTE = true
			}
		}
	}
	p.refs = append(p.refs, ref)
	return j
}

// relation reads a one- or two-part name at i.
func (p *parser) relation(i int) (Ref, int, bool) {
	if !p.isName(i) {
		if i < len(p.t) {
			p.fail("table name expected, got %s", describeToken(p.t[i]))
		} else {
			p.fail("table name expected")
		}
		return Ref{}, i, false
	}
	if p.keywordFunction(i) {
		// A reserved word Postgres reads as a function is no relation name
		// (INSERT INTO user, ONLY user, user.t are syntax errors there).
		p.fail("%s is a function, not a table", p.t[i].value)
		return Ref{}, i, false
	}
	parts := []string{p.t[i].value}
	j := i + 1
	for p.punct(j, ".") && p.isName(j+1) {
		parts = append(parts, p.t[j+1].value)
		j += 2
	}
	if p.punct(j, ".") {
		p.fail("qualified name form")
		return Ref{}, j, false
	}
	switch len(parts) {
	case 1:
		return Ref{Name: parts[0]}, j, true
	case 2:
		return Ref{Schema: parts[0], Name: parts[1]}, j, true
	}
	p.fail("a %d-part name", len(parts))
	return Ref{}, j, false
}

// target reads a write target at i ([ONLY] name [*]) and returns the
// index after it.
func (p *parser) target(i int, op string) int {
	if p.kw(i, "only") {
		p.form("ONLY")
		i++
	}
	ref, j, ok := p.relation(i)
	if !ok {
		return len(p.t)
	}
	ref.Write = true
	ref.Operation = op
	p.refs = append(p.refs, ref)
	if j < len(p.t) && p.t[j].kind == tokOp && p.t[j].value == "*" {
		j++
	}
	return j
}

func (p *parser) insert(i, end int, scope []string) {
	p.form("INSERT")
	if !p.kw(i+1, "into") {
		p.fail("INSERT without INTO")
		return
	}
	at := len(p.refs)
	j := p.target(i+2, "insert")
	if p.err != "" {
		return
	}
	p.form("INSERT INTO t [(cols)]")
	if p.kw(j, "as") && p.isName(j+1) {
		p.form("alias")
		j += 2
	}
	if p.punct(j, "(") && !p.kw(j+1, "select", "with", "values") {
		j = p.match[j] + 1
	}
	p.body(j, end, scope, &clause{kind: "insert", target: at})
}

func (p *parser) update(i, end int, scope []string) {
	p.form("UPDATE")
	j := p.target(i+1, "update")
	if p.err != "" {
		return
	}
	for j < end && !p.kw(j, "set") {
		if p.isName(j) || p.kw(j, "as") {
			p.form("alias")
			j++
			continue
		}
		p.fail("UPDATE target form")
		return
	}
	p.body(j, end, scope, &clause{kind: "update"})
}

func (p *parser) delete(i, end int, scope []string) {
	p.form("DELETE")
	if !p.kw(i+1, "from") {
		p.fail("DELETE without FROM")
		return
	}
	j := p.target(i+2, "delete")
	if p.err != "" {
		return
	}
	p.body(j, end, scope, &clause{kind: "delete"})
}

package schema

import (
	"fmt"
	"slices"
	"strings"
)

// PL/pgSQL compilation (T018a2 Revision 1.6 V1, class P14). With
// check_function_bodies on (E4), CREATE FUNCTION / PROCEDURE ... LANGUAGE
// plpgsql runs plpgsql_validator (pl_handler.c 442-551), which refuses
// pseudo-type results and arguments and then test-compiles the body
// (plpgsql_compile, pl_comp.c; the grammar pl_gram.y with every embedded
// SQL statement and expression checked by the core raw parser,
// check_sql_expr). A DO block is compiled the same way before it runs
// (plpgsql_compile_inline). E3 covers what a body does when it runs, never
// its compilation, so a statement that compiles a body is applied only
// when the pass proves the compilation succeeds: the body must be in the
// provable subset below, read by plBody. Every body outside it makes the
// statement may fail (R6.P14 at CREATE, O1.P14 where it is compiled
// later); a certain violation inside it is a refusal.
//
// The subset (pl_gram.y REL_17_6): one block [<<label>>] [DECLARE decl...]
// BEGIN stmt... [EXCEPTION handler...] END [label] [;] and nothing after
// it; declarations name type [{:= | = | DEFAULT} expr]; statements are
// assignments to a declared variable, a parameter or NEW.col / OLD.col of
// a DML trigger function, RETURN as make_return_stmt allows it for the
// routine's result, IF / ELSIF / ELSE, NULL, RAISE [level] 'format' [,
// expr]..., EXECUTE expr, PERFORM query, INSERT / UPDATE / DELETE of the
// expression reader's subset, SELECT [INTO targets] (T024c1b,
// selectStatement) and the statements V7's whole-statement readers read
// completely (syntax only, as Postgres checks them at compile time);
// handlers WHEN condition [OR condition]... THEN stmt.... Every expression
// is read by the syntax-only expression reader (exprsyntax.go). Anything
// else is not proven.

// plRoutine is what a body is compiled against.
type plRoutine struct {
	// what names the statement in diagnostics.
	what string
	// kind is "function", "procedure" or "do".
	kind string
	// returnNone is set when RETURN takes no expression: a function
	// returning a set or void or with OUT parameters, a procedure, an
	// event trigger function and a DO block (make_return_stmt).
	returnNone bool
	// dmlTrigger and eventTrigger mark trigger functions (RETURNS trigger,
	// RETURNS event_trigger).
	dmlTrigger, eventTrigger bool
	// params are the parameter names, IN and OUT (pl_comp.c adds each named
	// parameter as a variable).
	params map[string]bool
	// scalarParams marks the parameters whose type is a pg_catalog type
	// that is not composite: pl_comp.c builds them as scalar variables
	// (PLPGSQL_DTYPE_VAR), which a multiple-item INTO list takes.
	scalarParams map[string]bool
	// nparams counts the declared parameters.
	nparams int
}

// plProof is the outcome of reading a body.
type plProof struct {
	// why is "" when the body is proven to compile.
	why string
	// refused marks a certain compile error.
	refused bool
	// types are the declared variable types as written.
	types []string
}

// plDML are the special variables of a DML trigger function (pl_comp.c
// 627-716); plEvent those of an event trigger function (737-754).
var (
	plDML   = []string{"new", "old", "tg_name", "tg_when", "tg_level", "tg_op", "tg_relid", "tg_relname", "tg_table_name", "tg_table_schema", "tg_nargs", "tg_argv"}
	plEvent = []string{"tg_event", "tg_tag"}
)

// plEmbeddedHeads are the statement heads of embedded SQL the subset reads
// with V7's whole-statement readers (DML has its own reader).
var plEmbeddedHeads = map[string]bool{
	"create": true, "alter": true, "drop": true, "comment": true, "grant": true, "revoke": true,
	"lock": true, "notify": true, "listen": true, "unlisten": true, "security": true, "reassign": true,
}

// lexBody lexes a body with the core lexer and keeps its semicolons as
// tokens (lexStatements drops them). ok is false when the text cannot be
// lexed.
func lexBody(text string) ([]token, bool) {
	src := []byte(text)
	statements, failure := lexStatements(src)
	if failure != nil {
		return nil, false
	}
	var toks []token
	pos := 0
	gap := func(from, to int) bool {
		// Between statements there are only white space, comments and
		// semicolons.
		i := from
		for i < to {
			c := src[i]
			switch {
			case isSpace(c):
				i++
			case c == '-' && i+1 < to && src[i+1] == '-':
				for i < to && src[i] != '\n' {
					i++
				}
			case c == '/' && i+1 < to && src[i+1] == '*':
				end, ok := skipBlockComment(src, i)
				if !ok {
					return false
				}
				i = end
			case c == ';':
				toks = append(toks, token{kind: tokPunct, start: i, end: i + 1, value: ";"})
				i++
			default:
				return false
			}
		}
		return true
	}
	for _, st := range statements {
		if len(st.tokens) == 0 {
			continue
		}
		if !gap(pos, st.tokens[0].start) {
			return nil, false
		}
		toks = append(toks, st.tokens...)
		pos = st.tokens[len(st.tokens)-1].end
	}
	if !gap(pos, len(src)) {
		return nil, false
	}
	return toks, true
}

// plReader reads one body.
type plReader struct {
	s    *state
	r    *plRoutine
	c    *cursor
	out  plProof
	decl map[string]bool
	// scalarDecl marks the declared variables whose type is a pg_catalog
	// type that is not composite (pl_comp.c builds them as scalar
	// variables, PLPGSQL_DTYPE_VAR).
	scalarDecl map[string]bool
	// handler is set inside an exception handler (sqlstate, sqlerrm).
	handler bool
	// builtinOnly restricts declared types to pg_catalog types (a body
	// compiled later, when set types may have changed).
	builtinOnly bool
}

// plFail ends the reading: why the body is not proven, refused when
// Postgres certainly refuses it.
type plFail struct {
	why     string
	refused bool
}

func (p *plReader) fail(format string, args ...any) {
	panic(plFail{why: fmt.Sprintf(format, args...)})
}

func (p *plReader) refuse(format string, args ...any) {
	panic(plFail{why: fmt.Sprintf(format, args...), refused: true})
}

// proveBody reads a PL/pgSQL body against its routine.
func (s *state) proveBody(text string, r *plRoutine, builtinOnly bool) (out plProof) {
	toks, ok := lexBody(text)
	if !ok {
		return plProof{why: "the body could not be lexed"}
	}
	p := &plReader{s: s, r: r, c: &cursor{toks: toks, src: []byte(text)}, decl: map[string]bool{}, scalarDecl: map[string]bool{}, builtinOnly: builtinOnly}
	defer func() {
		if v := recover(); v != nil {
			f, isFail := v.(plFail)
			if !isFail {
				panic(v)
			}
			out = plProof{why: f.why, refused: f.refused, types: p.out.types}
		}
	}()
	for n := range r.params {
		if plpgsqlKeywords[n] {
			// A variable shadows an unreserved PL/pgSQL keyword
			// (pl_scanner.c plpgsql_yylex): the statements read differently.
			return plProof{why: fmt.Sprintf("parameter %s is named like a PL/pgSQL keyword", n)}
		}
	}
	if (r.dmlTrigger || r.eventTrigger) && r.nparams > 0 {
		// pl_comp.c 599-600, 733-734 (42P13).
		p.refuse("trigger functions cannot have declared arguments (42P13)")
	}
	p.block()
	return p.out
}

// at returns a short display of the current token.
func (p *plReader) at() string {
	if p.c.done() {
		return "the end of the body"
	}
	t := p.c.tok()
	return fmt.Sprintf("%q", p.c.src[t.start:t.end])
}

func (p *plReader) isSemi() bool { return p.c.isPunct(";") }

func (p *plReader) semi() {
	if !p.c.acceptPunct(";") {
		p.fail("expected ; at %s", p.at())
	}
}

// identAt reports an identifier token (unquoted or quoted) at the cursor.
func (p *plReader) identAt() bool {
	return !p.c.done() && (p.c.tok().kind == tokIdent || p.c.tok().kind == tokQuoted)
}

// label reads <<name>>.
func (p *plReader) label() string {
	c := p.c
	if !(!c.done() && c.tok().kind == tokOp && c.tok().value == "<<") {
		return ""
	}
	c.i++
	if !p.identAt() || c.tok().kind == tokIdent && plReservedWord(c.tok().value) {
		p.fail("a block label the subset does not read at %s", p.at())
	}
	name := c.tok().value
	c.i++
	if !(!c.done() && c.tok().kind == tokOp && c.tok().value == ">>") {
		p.fail("a block label the subset does not read at %s", p.at())
	}
	c.i++
	return name
}

// plReservedWord reports a word of pl_reserved_kwlist.h.
func plReservedWord(w string) bool {
	switch w {
	case "all", "begin", "by", "case", "declare", "else", "end", "execute", "for", "foreach", "from",
		"if", "in", "into", "loop", "not", "null", "or", "strict", "then", "to", "using", "when", "while":
		return true
	}
	return false
}

// block reads the routine's one block and the end of the body.
func (p *plReader) block() {
	c := p.c
	start := p.label()
	if c.acceptKw("declare") {
		p.declarations()
	}
	if !c.acceptKw("begin") {
		p.fail("expected BEGIN at %s", p.at())
	}
	p.statements("exception", "end")
	if c.acceptKw("exception") {
		p.handlers()
	}
	if !c.acceptKw("end") {
		p.fail("expected END at %s", p.at())
	}
	end := ""
	if p.identAt() {
		end = c.tok().value
		c.i++
	}
	switch {
	case end != "" && start == "":
		// check_labels, pl_gram.y 3815 (42601).
		p.refuse("end label %q specified for an unlabeled block (42601)", end)
	case end != "" && end != start:
		p.refuse("end label %q differs from block's label %q (42601)", end, start)
	}
	c.acceptPunct(";")
	if !c.done() {
		// pl_function: comp_options pl_block opt_semi, then the end.
		p.refuse("text after the block's END (42601) at %s", p.at())
	}
}

// declarations reads decl_stmts up to BEGIN.
func (p *plReader) declarations() {
	c := p.c
	for !c.peekKw("begin") {
		if c.done() {
			p.fail("the DECLARE section has no end")
		}
		if c.acceptKw("declare") {
			// pl_gram.y decl_stmt: useless extra DECLAREs are allowed.
			continue
		}
		p.declaration()
	}
}

// declaration reads name type [{:= | = | DEFAULT} expr] ;.
func (p *plReader) declaration() {
	c := p.c
	t := c.tok()
	switch {
	case t.kind == tokQuoted:
	case t.kind == tokIdent && !plpgsqlKeywords[t.value] && (pgKeywordCategories[t.value] == 'U' || pgKeywordCategories[t.value] == 0 && !sqlKeywords[t.value]):
		// decl_varname is T_WORD: the PL/pgSQL scanner knows only its own
		// keywords, so a core keyword is a word there. The subset takes
		// the unreserved ones (a ColId wherever the variable is used
		// later, as a parameter of that name already is).
	default:
		p.fail("a declaration the subset does not read at %s", p.at())
	}
	name := t.value
	if p.decl[name] {
		// decl_varname: "duplicate declaration" (42601).
		p.refuse("duplicate declaration of %s (42601)", name)
	}
	p.decl[name] = true
	c.i++
	if c.peekKw("constant") || c.peekKw("alias") || c.peekKw("cursor") || c.peekKw("scroll") || c.peekKw("no") {
		p.fail("CONSTANT, ALIAS and cursor declarations are not in the subset")
	}
	// read_datatype: the type ends at ;, :=, =, DEFAULT, NOT or COLLATE.
	from := c.i
	for !c.done() && !p.isSemi() && !p.assignOp(false) && !c.peekKw("default") && !c.peekKw("not") && !c.peekKw("collate") {
		c.i++
	}
	if c.done() || from == c.i {
		p.fail("a declaration the subset does not read")
	}
	typ := p.declaredType(from, c.i)
	p.out.types = append(p.out.types, typ)
	p.scalarDecl[name] = plScalarType(typ)
	switch {
	case p.isSemi():
		c.i++
	case p.assignOp(true) || c.acceptKw("default"):
		p.exprUntil("a default value")
		c.i++
	default:
		p.fail("NOT NULL and COLLATE in declarations are not in the subset")
	}
}

// declaredType proves the type written between from and to (M2.P1, V13).
func (p *plReader) declaredType(from, to int) string {
	c := p.c
	sub := &cursor{toks: c.toks[:to], i: from, src: c.src}
	if !strictType(sub) || !sub.done() {
		p.fail("a declared type the subset does not read (%s)", c.text(from, to))
	}
	typ := c.text(from, to)
	if serialType(typ) {
		// V13: serial is no type outside a column definition (42704).
		p.refuse("type %s does not exist outside a column definition (42704)", typ)
	}
	if _, v, _ := typmodCheck(typ); v != typmodOK {
		p.fail("the modifiers of %s are not proven", typ)
	}
	if why := p.s.typeProven(typ); why != "" {
		p.fail("declared %s", why)
	}
	if p.builtinOnly && !builtinDeclared(typ) {
		p.fail("declared type %s is not a pg_catalog type, and a set type may have been dropped or replaced before the body is compiled", typ)
	}
	return typ
}

// builtinDeclared reports a declared type of pg_catalog (or an array of
// one), which no statement of the set can drop or replace.
func builtinDeclared(typ string) bool {
	schema, name, _, ok := splitType(typ)
	if !ok || schema != "" && schema != "pg_catalog" {
		return false
	}
	ty, found := pgTypes[name]
	return found && ty.category != 'P'
}

// assignOp reports (and with take, consumes) := or = (pl_gram.y
// assign_operator; COLON_EQUALS is the two characters together).
func (p *plReader) assignOp(take bool) bool {
	c := p.c
	if c.done() {
		return false
	}
	t := c.tok()
	if t.kind == tokOp && t.value == "=" {
		if take {
			c.i++
		}
		return true
	}
	if t.kind == tokPunct && t.value == ":" && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokOp && c.toks[c.i+1].value == "=" && c.toks[c.i+1].start == t.end {
		if take {
			c.i += 2
		}
		return true
	}
	return false
}

// names in scope: parameters, declarations, found, the trigger variables
// and in a handler sqlstate and sqlerrm.
func (p *plReader) inScope(name string) bool {
	switch {
	case p.r.params[name], p.decl[name], name == "found":
		return true
	case p.r.dmlTrigger && slices.Contains(plDML, name), p.r.eventTrigger && slices.Contains(plEvent, name):
		return true
	case p.handler && (name == "sqlstate" || name == "sqlerrm"):
		return true
	}
	return false
}

// statements reads proc_sect up to one of the stop words.
func (p *plReader) statements(stop ...string) {
	c := p.c
	for {
		if c.done() {
			p.fail("the body ends inside a block")
		}
		for _, w := range stop {
			if c.peekKw(w) {
				return
			}
		}
		p.statement()
	}
}

// statement reads one statement of the subset.
func (p *plReader) statement() {
	c := p.c
	t := c.tok()
	if t.kind == tokIdent && !p.inScope(t.value) {
		switch t.value {
		case "if":
			p.ifStatement()
			return
		case "null":
			c.i++
			p.semi()
			return
		case "return":
			p.returnStatement()
			return
		case "raise":
			p.raise()
			return
		case "execute":
			c.i++
			for j := c.i; j < len(c.toks) && !(c.toks[j].kind == tokPunct && c.toks[j].value == ";"); j++ {
				if c.toks[j].kind == tokIdent && (c.toks[j].value == "into" || c.toks[j].value == "using") {
					p.fail("EXECUTE ... INTO / USING is not in the subset")
				}
			}
			p.exprUntil("an EXECUTE expression")
			c.i++
			return
		case "perform":
			c.i++
			p.perform()
			return
		}
	}
	if p.identAt() && !p.inScope(t.value) {
		// A head that is no variable: stmt_execsql for T_WORD.
		if t.kind == tokIdent && (t.value == "insert" || t.value == "update" || t.value == "delete") {
			p.dml()
			return
		}
		if t.kind == tokIdent && t.value == "select" {
			p.selectStatement()
			return
		}
		if t.kind == tokIdent && plEmbeddedHeads[t.value] && !plpgsqlKeywords[t.value] {
			p.embedded()
			return
		}
	}
	if p.identAt() {
		p.assignment()
		return
	}
	p.fail("a statement the subset does not read at %s", p.at())
}

// stmtEnd is the index of the semicolon that ends the statement at the
// cursor: the first one outside parentheses (make_execsql_stmt,
// read_sql_construct).
func (p *plReader) stmtEnd() int {
	c := p.c
	depth := 0
	for j := c.i; j < len(c.toks); j++ {
		t := c.toks[j]
		if t.kind != tokPunct {
			continue
		}
		switch t.value {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
			if depth < 0 {
				p.fail("mismatched parentheses")
			}
		case ";":
			if depth == 0 {
				return j
			}
		}
	}
	p.fail("a statement without its ;")
	return 0
}

// exprUntil reads one expression of the subset that ends exactly at the
// semicolon that ends the statement; the cursor stops on it.
func (p *plReader) exprUntil(what string) {
	c := p.c
	end := p.stmtEnd()
	p.exprSpan(c.i, end, what, false)
	c.i = end
}

// exprSpan reads the tokens [from, to) as one expression.
func (p *plReader) exprSpan(from, to int, what string, noCase bool) {
	if from >= to {
		// read_sql_construct: "missing expression" (42601).
		p.refuse("missing expression for %s (42601)", what)
	}
	sub := &cursor{toks: p.c.toks[:to], i: from, src: p.c.src}
	r := &exprReader{c: sub, noCase: noCase}
	if !r.or() || !sub.done() {
		p.fail("%s the expression reader does not read (%s)", what, p.c.text(from, to))
	}
}

// ifStatement reads IF expr THEN stmts {ELSIF | ELSEIF} ... [ELSE stmts]
// END IF ;.
func (p *plReader) ifStatement() {
	c := p.c
	for {
		c.i++ // IF, ELSIF or ELSEIF
		then := -1
		depth := 0
		for j := c.i; j < len(c.toks); j++ {
			t := c.toks[j]
			if t.kind == tokPunct && (t.value == "(" || t.value == "[") {
				depth++
			}
			if t.kind == tokPunct && (t.value == ")" || t.value == "]") {
				depth--
			}
			if t.kind == tokPunct && t.value == ";" && depth == 0 {
				break
			}
			if depth == 0 && t.kind == tokIdent && t.value == "then" {
				then = j
				break
			}
		}
		if then < 0 {
			p.fail("an IF condition without THEN")
		}
		p.exprSpan(c.i, then, "an IF condition", true)
		c.i = then + 1
		p.statements("elsif", "elseif", "else", "end")
		if c.peekKw("elsif") || c.peekKw("elseif") {
			continue
		}
		break
	}
	if c.acceptKw("else") {
		p.statements("end")
	}
	if !c.acceptKw("end", "if") {
		p.fail("expected END IF at %s", p.at())
	}
	p.semi()
}

// returnStatement applies make_return_stmt (pl_gram.y 3285-3330).
func (p *plReader) returnStatement() {
	c := p.c
	c.i++
	if c.peekKw("next") || c.peekKw("query") {
		p.fail("RETURN NEXT and RETURN QUERY are not in the subset")
	}
	if p.r.returnNone {
		if !p.isSemi() {
			if p.r.kind == "procedure" {
				p.refuse("RETURN cannot have a parameter in a procedure (42601)")
			}
			p.refuse("RETURN cannot have a parameter in %s (42804)", map[bool]string{true: "a DO block", false: "a function returning void, a set or OUT parameters"}[p.r.kind == "do"])
		}
		c.i++
		return
	}
	if p.isSemi() {
		p.refuse("RETURN needs an expression here: missing expression (42601)")
	}
	p.exprUntil("a RETURN expression")
	c.i++
}

// raise reads RAISE [level] 'format' [, expr]... ; and applies
// check_raise_parameters (pl_gram.y 4063-4091).
func (p *plReader) raise() {
	c := p.c
	c.i++
	for _, level := range []string{"exception", "warning", "notice", "info", "log", "debug"} {
		if c.acceptKw(level) {
			break
		}
	}
	t := c.tok()
	if c.done() || t.kind != tokString || !(p.c.src[t.start] == '\'' || p.c.src[t.start] == '$' || p.c.src[t.start] == 'e' || p.c.src[t.start] == 'E') {
		p.fail("a RAISE without a format string is not in the subset")
	}
	format, ok := literalValue(t, c.src)
	if !ok {
		p.fail("a RAISE format the pass does not read")
	}
	c.i++
	want := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' {
			if i+1 < len(format) && format[i+1] == '%' {
				i++
			} else {
				want++
			}
		}
	}
	got := 0
	end := p.stmtEnd()
	for !p.isSemi() {
		if !c.acceptPunct(",") {
			p.fail("RAISE ... USING and other RAISE forms are not in the subset")
		}
		// The parameter ends at the next , or ; outside parentheses.
		depth, stop := 0, end
		for j := c.i; j < end; j++ {
			tk := c.toks[j]
			if tk.kind == tokPunct && (tk.value == "(" || tk.value == "[") {
				depth++
			}
			if tk.kind == tokPunct && (tk.value == ")" || tk.value == "]") {
				depth--
			}
			if depth == 0 && (tk.kind == tokPunct && tk.value == "," || tk.kind == tokIdent && tk.value == "using") {
				stop = j
				break
			}
		}
		p.exprSpan(c.i, stop, "a RAISE parameter", false)
		c.i = stop
		got++
	}
	if got != want {
		p.refuse("RAISE has %d parameters for %d placeholders (42601)", got, want)
	}
	c.i++
}

// perform reads PERFORM select_rest ; as the query SELECT select_rest.
func (p *plReader) perform() {
	c := p.c
	end := p.stmtEnd()
	toks := append([]token{{kind: tokIdent, value: "select", start: c.tok().start, end: c.tok().start}}, c.toks[c.i:end]...)
	sub := &cursor{toks: toks, src: c.src}
	if !querySyntax(sub) || !sub.done() {
		p.fail("a PERFORM query the expression reader does not read")
	}
	c.i = end + 1
}

// assignment reads target {:= | =} expr ;.
func (p *plReader) assignment() {
	c := p.c
	t := c.tok()
	name := t.value
	if cat := pgKeywordCategories[name]; t.kind == tokIdent && (cat == 'R' || cat == 'T') {
		// PLAssignStmt's target is a ColId (gram.y plassign_target).
		p.fail("an assignment target that is a keyword the raw parser does not take as a name (%s)", name)
	}
	c.i++
	dotted := c.isPunct(".")
	if dotted {
		// NEW.col / OLD.col of a DML trigger function.
		if !(t.kind == tokIdent && (name == "new" || name == "old")) || c.i+1 >= len(c.toks) || c.toks[c.i+1].kind != tokIdent && c.toks[c.i+1].kind != tokQuoted {
			p.fail("an assignment target the subset does not read")
		}
		if !p.r.dmlTrigger {
			if p.inScope(name) {
				p.fail("an assignment target the subset does not read")
			}
			// cword_is_not_variable: "new.a" is not a known variable (42601).
			p.refuse("%s.%s is not a known variable outside a trigger function (42601)", name, c.toks[c.i+1].value)
		}
		c.i += 2
	}
	if !p.assignOp(false) {
		if !dotted && !p.inScope(name) {
			p.fail("a statement the subset does not read at %q", name)
		}
		p.fail("an assignment target the subset does not read")
	}
	if !dotted {
		switch {
		case !p.inScope(name):
			// word_is_not_variable (42601).
			p.refuse("%s is not a known variable (42601)", name)
		case !p.r.params[name] && !p.decl[name]:
			// found, the trigger variables, sqlstate, sqlerrm.
			p.fail("an assignment to %s is not in the subset", name)
		}
	}
	p.assignOp(true)
	p.exprUntil("an assigned value")
	c.i++
}

// plScalarType reports a type pl_comp.c builds a scalar variable for and
// the pass can prove: a pg_catalog type (or an array of one, always
// scalar) that is not a composite row type (plpgsql_build_variable:
// PLPGSQL_TTYPE_SCALAR unless the type is composite or record).
func plScalarType(typ string) bool {
	if !builtinDeclared(typ) {
		return false
	}
	_, name, dims, _ := splitType(typ)
	return dims > 0 || pgTypes[name].category != 'C'
}

// selectStatement reads SELECT ... [INTO [STRICT] target [, target]...]
// ... ;, stmt_execsql for the word SELECT (pl_gram.y make_execsql_stmt,
// read_into_target, read_into_scalar_list): the statement ends at the
// first semicolon outside parentheses; the one INTO it holds, here at
// parenthesis depth 0, starts the INTO clause, whose targets are
// variables (T_DATUM); a target followed by a comma must be a scalar
// variable, as must every later one ("record variable cannot be part of
// multiple-item INTO list", "is not a scalar variable", 42601), and none
// may be a constant (check_assignable). The INTO clause is cut out and the
// rest is checked by the core raw parser only (check_sql_expr), so it must
// be a query the syntax-only expression reader reads completely. The
// subset takes as targets the routine's parameters and declared variables
// (never CONSTANT, which the subset does not declare), and in a list of
// several only those of a pg_catalog type that is not composite. Anything
// else is not proven: an INTO inside parentheses or twice, a target that
// is another variable (FOUND, NEW, a record field), a target list of
// variables whose kind is not proven.
func (p *plReader) selectStatement() {
	c := p.c
	end := p.stmtEnd()
	into, depth := -1, 0
	for j := c.i; j < end; j++ {
		t := c.toks[j]
		switch {
		case t.kind == tokPunct && (t.value == "(" || t.value == "["):
			depth++
		case t.kind == tokPunct && (t.value == ")" || t.value == "]"):
			depth--
		case t.kind == tokIdent && t.value == "into":
			if into >= 0 || depth > 0 {
				p.fail("an INTO the subset does not read in an embedded SELECT")
			}
			into = j
		}
	}
	toks := c.toks[c.i:end]
	if into >= 0 {
		k := into + 1
		if k < end && c.toks[k].kind == tokIdent && c.toks[k].value == "strict" {
			k++
		}
		var targets []string
		for {
			if k >= end || c.toks[k].kind != tokIdent && c.toks[k].kind != tokQuoted {
				p.fail("an INTO target the subset does not read")
			}
			name := c.toks[k].value
			if !p.r.params[name] && !p.decl[name] || k+1 < end && c.toks[k+1].kind == tokPunct && c.toks[k+1].value == "." {
				p.fail("an INTO target the subset does not read (%s)", name)
			}
			targets = append(targets, name)
			k++
			if !(k < end && c.toks[k].kind == tokPunct && c.toks[k].value == ",") {
				break
			}
			k++
		}
		if len(targets) > 1 {
			for _, name := range targets {
				if !(p.decl[name] && p.scalarDecl[name] || !p.decl[name] && p.r.scalarParams[name]) {
					p.fail("an INTO list of several targets whose variable %s is not proven scalar", name)
				}
			}
		}
		toks = append(append([]token{}, c.toks[c.i:into]...), c.toks[k:end]...)
	}
	sub := &cursor{toks: toks, src: c.src}
	if !querySyntax(sub) || !sub.done() {
		p.fail("an embedded SELECT the expression reader does not read")
	}
	c.i = end + 1
}

// dml reads INSERT, UPDATE and DELETE of the subset, ending at ;.
func (p *plReader) dml() {
	c := p.c
	end := p.stmtEnd()
	for j := c.i; j < end; j++ {
		tk := c.toks[j]
		if tk.kind == tokIdent && (tk.value == "returning" || tk.value == "into" && !(j > c.i && c.toks[j-1].kind == tokIdent && c.toks[j-1].value == "insert")) {
			p.fail("INTO and RETURNING in embedded SQL are not in the subset")
		}
	}
	sub := &cursor{toks: c.toks[:end], i: c.i, src: c.src}
	r := &exprReader{c: sub}
	alias := func() {
		if sub.acceptKw("as") {
			if _, err := sub.ident(); err != nil {
				p.fail("an alias the subset does not read")
			}
			return
		}
		if !sub.done() && (sub.tok().kind == tokQuoted || sub.tok().kind == tokIdent && !sqlKeywords[sub.tok().value]) {
			sub.i++
		}
	}
	ok := false
	switch {
	case sub.acceptKw("insert", "into"):
		if _, err := sub.name(); err != nil {
			break
		}
		if sub.isPunct("(") {
			sub.i++
			for {
				if _, err := sub.ident(); err != nil {
					p.fail("an INSERT column list the subset does not read")
				}
				if !sub.acceptPunct(",") {
					break
				}
			}
			if !sub.acceptPunct(")") {
				break
			}
		}
		if sub.acceptKw("values") {
			ok = true
			for ok {
				ok = sub.acceptPunct("(") && r.list() && sub.acceptPunct(")")
				if !sub.acceptPunct(",") {
					break
				}
			}
		} else {
			ok = r.query()
		}
	case sub.acceptKw("update"):
		if _, err := sub.name(); err != nil {
			break
		}
		alias()
		if !sub.acceptKw("set") {
			break
		}
		ok = true
		for ok {
			_, err := sub.ident()
			ok = err == nil && !sub.done() && sub.tok().kind == tokOp && sub.tok().value == "=" && sub.next() && r.or()
			if !sub.acceptPunct(",") {
				break
			}
		}
		if ok && sub.acceptKw("where") {
			ok = r.or()
		}
	case sub.acceptKw("delete", "from"):
		if _, err := sub.name(); err != nil {
			break
		}
		alias()
		ok = true
		if sub.acceptKw("where") {
			ok = r.or()
		}
	}
	if !ok || !sub.done() {
		p.fail("embedded %s the subset does not read", strings.ToUpper(c.tok().value))
	}
	c.i = end + 1
}

// embedded reads an embedded statement with V7's whole-statement readers
// (syntax only).
func (p *plReader) embedded() {
	c := p.c
	end := p.stmtEnd()
	for j := c.i; j < end; j++ {
		if c.toks[j].kind == tokIdent && c.toks[j].value == "into" {
			p.fail("INTO in embedded SQL is not in the subset")
		}
	}
	toks := c.toks[c.i:end]
	if !embeddedSyntax(toks, c.src) {
		p.fail("embedded statement %s is not read completely by a whole-statement reader", statementText(toks, c.src))
	}
	c.i = end + 1
}

// embeddedSyntax reports whether a statement of a body is read completely
// by a reader that checks syntax only: V7's whole-statement readers in
// their strict form (no group whose content is left to another
// precondition), and ALTER TABLE ... RENAME (M5h), whose reader reads only
// names.
func embeddedSyntax(toks []token, src []byte) bool {
	p := &wp{cursor: &cursor{toks: toks, src: src}, strict: true}
	if p.done() || p.tok().kind != tokIdent {
		return false
	}
	switch {
	case p.peekKw("alter", "table"):
		return renameSyntax(toks, src)
	case p.peekKw("create", "schema"), p.peekKw("create", "trigger"), p.peekKw("create", "or"), p.peekKw("create", "constraint"),
		p.peekKw("create", "policy"), p.peekKw("alter", "policy"), p.peekKw("create", "event"):
		// Elements, WHEN and USING expressions: not syntax-only readers.
		return false
	}
	governed, ok := p.statement()
	return governed && ok && p.done()
}

// renameSyntax reads ALTER TABLE [IF EXISTS] [ONLY] name [*] RENAME
// [COLUMN] a TO b and ALTER TABLE [IF EXISTS] name RENAME TO b (gram.y
// RenameStmt).
func renameSyntax(toks []token, src []byte) bool {
	c := &cursor{toks: toks, src: src, i: 2}
	c.acceptKw("if", "exists")
	only := c.acceptKw("only")
	if _, err := c.name(); err != nil {
		return false
	}
	if !c.done() && c.tok().kind == tokOp && c.tok().value == "*" {
		if only {
			return false
		}
		c.i++
	}
	if !c.acceptKw("rename") {
		return false
	}
	if c.acceptKw("to") {
		_, err := c.ident()
		return err == nil && c.done()
	}
	c.acceptKw("column")
	if _, err := c.ident(); err != nil {
		return false
	}
	if !c.acceptKw("to") {
		return false
	}
	_, err := c.ident()
	return err == nil && c.done()
}

// handlers reads WHEN cond [OR cond]... THEN stmt... up to END.
func (p *plReader) handlers() {
	c := p.c
	p.handler = true
	defer func() { p.handler = false }()
	if !c.peekKw("when") {
		p.fail("EXCEPTION without WHEN")
	}
	for c.acceptKw("when") {
		for {
			p.condition()
			if !c.acceptKw("or") {
				break
			}
		}
		if !c.acceptKw("then") {
			p.fail("expected THEN at %s", p.at())
		}
		p.statements("when", "end")
	}
}

// condition reads one proc_condition: OTHERS, a condition name of
// plerrcodes.h or SQLSTATE '<5 characters>'.
func (p *plReader) condition() {
	c := p.c
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted {
		p.fail("a handler condition the subset does not read at %s", p.at())
	}
	c.i++
	switch {
	case t.value == "sqlstate" && t.kind == tokIdent:
		s := c.tok()
		if c.done() || s.kind != tokString || p.c.src[s.start] != '\'' {
			p.fail("a SQLSTATE condition the subset does not read")
		}
		c.i++
		v := s.value
		if len(v) != 5 || strings.Trim(v, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
			// pl_gram.y 1894-1901: "invalid SQLSTATE code" (42601).
			p.refuse("invalid SQLSTATE code %q (42601)", v)
		}
	case t.value == "others" || plpgsqlConditions[t.value]:
	case t.kind == tokIdent && plReservedWord(t.value):
		p.fail("a handler condition the subset does not read")
	default:
		// plpgsql_parse_err_condition, pl_comp.c 2245 (42704).
		p.refuse("unrecognized exception condition %q (42704)", t.value)
	}
}

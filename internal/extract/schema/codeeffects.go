package schema

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Code effects (T018a2 Revision 1.5 U2, class P12). DO blocks, CALL,
// EXECUTE, calls of set-defined routines and data statements that fire
// set-defined triggers run code that may create, alter or drop objects a
// later statement creates, uses or drops. E3 grants only that the code
// finishes without raising; what it does to objects is this class.
//
// Every O1 statement gets a DDL inventory, read from the bodies it may
// run:
//
//   - the bodies: the DO body; every definition so far of each set-defined
//     routine whose name appears as an identifier in the statement; for a
//     data statement, EXECUTE of a prepared statement and the statements
//     that run index expressions or view queries, also every trigger
//     function and every routine a statement of the set names outside its
//     own definition (a default, check, index expression, policy, rule or
//     operator may run it); closed over the routine names in the bodies,
//     and over that second set when a body changes data;
//   - each body is lexed and split into its statements (PL/pgSQL at
//     statement level, with its block, IF, CASE, loop and exception
//     headers removed; LANGUAGE sql as a statement list); a statement whose
//     head is CREATE, ALTER, DROP, COMMENT, GRANT, REVOKE, SECURITY LABEL,
//     TRUNCATE, REINDEX or CLUSTER is read by the statement analyser; the
//     other heads that cannot change objects (queries, data statements,
//     assignments, RAISE, RETURN, control statements) only contribute the
//     routine names they call;
//   - the inventory is unbounded when a body contains EXECUTE (dynamic SQL),
//     a SET, RESET or set_config (search_path, a role or any other setting),
//     transaction control, a nested DO, a statement the reader does not
//     know or the analyser cannot read, DROP ... CASCADE, a body in another
//     language or one not read, or a call of a routine created by code
//     (whose body the pass did not read).
//
// Effects, from the statement on: a name the inventory may create is
// possibly held (a later CREATE of it without IF NOT EXISTS or OR REPLACE
// may fail, with them its facts are unknown; a later use is not proven,
// because the set creates it and the pass does not hold it); an object the
// inventory may drop, rename or move is no longer held; every table the
// inventory names is partial, and every name it writes may have dependents
// the pass does not see (a later DROP, DROP COLUMN, ALTER COLUMN TYPE, DROP
// CONSTRAINT or RENAME on it without CASCADE may fail). An unbounded
// inventory may have done anything, including changing search_path, so the
// migration may fail from the statement on (O1.P12, Q1.P13).

// routineBody is one definition of a set-defined routine (or a DO body).
type routineBody struct {
	lang string
	// text is the body; toks is set instead for a SQL-standard body
	// (BEGIN ATOMIC ... END or RETURN expr), with src its source.
	text string
	toks []token
	src  []byte
	// unread says why the body is not read ("" when it is).
	unread string
	// trigger marks a trigger function (RETURNS trigger): it may run on
	// any data change.
	trigger bool
	scan    *bodyScan
	// all keeps reading past a statement that makes the scan unbounded
	// (the set-wide name collection).
	all bool
	// callWhy says why compiling the PL/pgSQL body at a later call is not
	// proven (V1, O1.P14); "" when it is.
	callWhy string
	// born is the statement count when the body was created (Y50).
	born int
}

// bodyScan is what one body may run.
type bodyScan struct {
	ddl []srcStatement
	// static are the PL/pgSQL body's query and data statements (SELECT,
	// PERFORM, INSERT, UPDATE, DELETE, VALUES, WITH, RETURN), checked where
	// the body runs (Y49, Revision 1.9).
	static []srcStatement
	src    []byte
	// calls are the identifiers of the body (routine names among them).
	calls []string
	// data is set when the body changes rows (triggers and defaults run).
	data      bool
	unbounded string
	// writes are the target tables of the body's data statements that
	// add or change rows (V5); writesAll is set when a target is not read.
	writes    []qname
	writesAll bool
	// deletes and deletesAll are the targets of the body's DELETE
	// statements (T024 review: a data statement on a table the set
	// does not hold may change row level security, SB-M9).
	deletes    []qname
	deletesAll bool
}

// codeState is the P12 part of the state.
type codeState struct {
	// bodies are the definitions of the set's routines read so far, by
	// bare name (every overload and replacement: a superset).
	bodies map[string][]*routineBody
	// referenced are the routine names a statement of the set names
	// outside the routine's own definition.
	referenced map[string]bool
	// may are the names an inventory may have created, by namespace.
	may map[string]nameKind
	// mayN1 are the N1 kinds (trigger, policy, role, ...) of which an
	// inventory may have created a name.
	mayN1 map[string]bool
	// gone are the names an inventory may have dropped, renamed or moved:
	// the pass no longer holds them.
	gone map[string]bool
	// named are the words of the inventories' statements: an object of
	// such a name may have dependents the pass does not see.
	named map[string]bool
	// routines are the routines an inventory may have created or replaced.
	routines map[string]bool
	// unqualified is set once an inventory may have created an object in
	// the default schema.
	unqualified bool
	// tables are the relations an inventory may have created: every name
	// Postgres may generate with them is possibly held (V8).
	tables map[string]bool
}

// codeHeads are the statement heads the analyser reads in a body.
var codeHeads = map[string]bool{
	"create": true, "alter": true, "drop": true, "comment": true, "grant": true, "revoke": true,
	"security": true, "truncate": true, "reindex": true, "cluster": true,
}

// srcStatement is a statement with the source its tokens point into (a
// body, or the constant string of an EXECUTE).
type srcStatement struct {
	st  statement
	src []byte
	// protected is set when the statement's body is one block whose
	// EXCEPTION WHEN OTHERS handler only raises notices: an error there
	// is caught, so it cannot fail the code (Y48, Y49).
	protected bool
}

// codeSafeHeads are the PL/pgSQL statement heads that change no object
// themselves (gram.y PL/pgSQL statements and the SQL queries and data
// statements; a routine they call joins the inventory).
var codeSafeHeads = map[string]bool{
	"select": true, "perform": true, "insert": true, "update": true, "delete": true, "merge": true,
	"with": true, "values": true, "table": true, "raise": true, "return": true, "null": true,
	"end": true, "exit": true, "continue": true, "get": true, "assert": true, "open": true,
	"fetch": true, "move": true, "close": true, "call": true, "notify": true, "listen": true,
	"unlisten": true, "lock": true, "refresh": true, "copy": true, "analyze": true, "checkpoint": true,
}

// codeDataWords mark a body statement that changes rows.
var codeDataWords = map[string]bool{"insert": true, "update": true, "delete": true, "merge": true, "copy": true, "truncate": true, "refresh": true}

// doBody reads DO [LANGUAGE l] 'body' [LANGUAGE l] (gram.y DoStmt; the
// default language is plpgsql).
func doBody(toks []token, src []byte) *routineBody {
	b := &routineBody{lang: "plpgsql"}
	var body *token
	for i := 1; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.kind == tokIdent && t.value == "language" && i+1 < len(toks) && (toks[i+1].kind == tokIdent || toks[i+1].kind == tokString || toks[i+1].kind == tokQuoted):
			b.lang = strings.ToLower(toks[i+1].value)
			i++
		case t.kind == tokString && body == nil:
			body = &toks[i]
		default:
			b.unread = "the DO statement could not be read"
		}
	}
	if body == nil {
		b.unread = "the DO statement has no body"
		return b
	}
	if b.lang != "plpgsql" {
		// Only languages with an inline handler run DO (do.c ExecuteDoStmt
		// refuses sql, 0A000); the pass reads PL/pgSQL only.
		b.unread = fmt.Sprintf("a DO block in language %s is not read", b.lang)
	}
	text, ok := literalValue(*body, src)
	if !ok {
		b.unread = "the DO body is not a string the pass reads"
	}
	b.text = text
	return b
}

// routineBodyOf reads the body, language and result of CREATE [OR
// REPLACE] FUNCTION|PROCEDURE; name is "" when the statement is not one.
func routineBodyOf(toks []token, src []byte) (name string, b *routineBody) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create") {
		return "", nil
	}
	c.acceptKw("or", "replace")
	if !c.acceptKw("function") && !c.acceptKw("procedure") {
		return "", nil
	}
	q, err := c.name()
	if err != nil {
		return "", nil
	}
	b = &routineBody{src: src}
	if !c.isPunct("(") {
		b.unread = "the routine's parameters could not be read"
		return q.name, b
	}
	end, err := c.group()
	if err != nil {
		b.unread = "the routine's parameters could not be read"
		return q.name, b
	}
	var body *token
	for i := end; i < len(toks); i++ {
		t := toks[i]
		if t.kind == tokPunct && t.value == "(" {
			// RETURNS TABLE (...), a typmod: skip the group.
			c.i = i
			if e, err := c.group(); err == nil {
				i = e - 1
			}
			continue
		}
		if t.kind != tokIdent {
			continue
		}
		switch t.value {
		case "returns":
			if i+1 < len(toks) && toks[i+1].kind == tokIdent && (toks[i+1].value == "trigger" || toks[i+1].value == "event_trigger") {
				b.trigger = true
			}
		case "language":
			if i+1 < len(toks) {
				b.lang = strings.ToLower(toks[i+1].value)
				i++
			}
		case "as":
			if i+1 < len(toks) && toks[i+1].kind == tokString && body == nil {
				body = &toks[i+1]
				i++
				if i+1 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "," {
					b.unread = "the routine is an object file symbol"
				}
			}
		case "begin", "return":
			if body == nil && b.toks == nil && (t.value == "return" || i+1 < len(toks) && toks[i+1].kind == tokIdent && toks[i+1].value == "atomic") {
				// SQL-standard body (gram.y opt_routine_body): LANGUAGE sql.
				b.toks, b.lang = toks[i:], "sql"
				i = len(toks)
			}
		}
	}
	switch {
	case body != nil:
		text, ok := literalValue(*body, src)
		if !ok {
			b.unread = "the routine body is not a string the pass reads"
		}
		b.text = text
	case b.toks == nil:
		b.unread = "the routine has no body the pass reads"
	}
	if b.unread == "" && b.lang != "plpgsql" && b.lang != "sql" {
		b.unread = fmt.Sprintf("the routine is in language %s", b.lang)
	}
	return q.name, b
}

// scanBody splits a body into the statements it may run.
func scanBody(b *routineBody) *bodyScan {
	if b.scan != nil {
		return b.scan
	}
	sc := &bodyScan{}
	b.scan = sc
	if b.unread != "" {
		sc.unbounded = b.unread
		return sc
	}
	if b.lang != "plpgsql" && b.lang != "sql" {
		sc.unbounded = fmt.Sprintf("a body in language %s is not read", b.lang)
		return sc
	}
	var chunks [][]token
	if b.toks != nil {
		sc.src = b.src
		var cur []token
		for _, t := range b.toks {
			if t.kind == tokPunct && t.value == ";" {
				chunks, cur = append(chunks, cur), nil
				continue
			}
			cur = append(cur, t)
		}
		chunks = append(chunks, cur)
	} else {
		sc.src = []byte(b.text)
		statements, failure := lexStatements(sc.src)
		if failure != nil {
			sc.unbounded = "the body could not be lexed: " + failure.reason
			return sc
		}
		for _, st := range statements {
			chunks = append(chunks, st.tokens)
		}
	}
	inDecl := false
	protected := b.lang == "plpgsql" && b.toks == nil && plpgsqlProtected(b.text)
	stop := func(why string) bool {
		if sc.unbounded == "" {
			sc.unbounded = why
		}
		return !b.all
	}
	for _, toks := range chunks {
		why, execute := sc.raw(toks)
		if why != "" && stop(why) {
			return sc
		}
		if b.lang == "plpgsql" {
			var ok bool
			toks, inDecl, ok = stripPlpgsql(toks, inDecl)
			if !ok {
				if stop("a PL/pgSQL control statement could not be read") {
					return sc
				}
				continue
			}
			if inDecl {
				continue
			}
		} else {
			for len(toks) > 0 && toks[0].kind == tokIdent && (toks[0].value == "begin" || toks[0].value == "atomic") {
				toks = toks[1:]
			}
		}
		if len(toks) == 0 {
			continue
		}
		src := sc.src
		if execute {
			// EXECUTE of one string constant runs that statement (pl_exec.c
			// exec_stmt_dynexecute); any other EXECUTE is dynamic SQL.
			inner, innerSrc, ok := constantExecute(toks, src)
			if !ok {
				if stop("EXECUTE runs SQL the pass does not read") {
					return sc
				}
				continue
			}
			if why, again := sc.raw(inner); (why != "" || again) && stop("EXECUTE of a statement the pass does not read") {
				return sc
			}
			toks, src = inner, innerSrc
		}
		head := toks[0]
		sc.dataTarget(toks, src)
		switch {
		case head.kind == tokIdent && codeHeads[head.value]:
			sc.ddl = append(sc.ddl, srcStatement{st: statement{tokens: toks, start: toks[0].start, end: toks[len(toks)-1].end}, src: src, protected: protected})
		case head.kind == tokIdent && codeSafeHeads[head.value]:
			if b.lang == "plpgsql" && y49Heads[head.value] {
				sc.static = append(sc.static, srcStatement{st: statement{tokens: toks, start: toks[0].start, end: toks[len(toks)-1].end}, src: src, protected: protected})
			}
			if b.lang == "sql" && head.value == "select" && slices.ContainsFunc(toks, func(t token) bool { return t.kind == tokIdent && t.value == "into" }) && stop("SELECT INTO in a SQL body creates a table the pass does not read") {
				return sc
			}
		case b.lang == "plpgsql" && (head.kind == tokIdent || head.kind == tokQuoted) && len(toks) > 1 &&
			(toks[1].kind == tokPunct && (toks[1].value == ":" || toks[1].value == "." || toks[1].value == "[") || toks[1].kind == tokOp && (toks[1].value == "=" || toks[1].value == ":=")):
			// An assignment (gram.y stmt_assign).
		default:
			if stop(fmt.Sprintf("a statement the pass does not read (%s)", strings.ToUpper(head.value))) {
				return sc
			}
		}
	}
	return sc
}

// constantExecute reads EXECUTE '<one statement>' (no INTO, USING or
// expression).
func constantExecute(toks []token, src []byte) ([]token, []byte, bool) {
	if len(toks) != 2 || toks[0].kind != tokIdent || toks[0].value != "execute" || toks[1].kind != tokString {
		return nil, nil, false
	}
	text, ok := literalValue(toks[1], src)
	if !ok {
		return nil, nil, false
	}
	inner := []byte(text)
	statements, failure := lexStatements(inner)
	if failure != nil || len(statements) != 1 || len(statements[0].tokens) == 0 {
		return nil, nil, false
	}
	return statements[0].tokens, inner, true
}

// raw reads the words of one body statement: routine names, data changes
// and the words that make the inventory unbounded. execute reports an
// EXECUTE that is not a trigger's EXECUTE FUNCTION.
func (sc *bodyScan) raw(toks []token) (why string, execute bool) {
	for j, t := range toks {
		if t.kind != tokIdent && t.kind != tokQuoted {
			continue
		}
		sc.calls = append(sc.calls, t.value)
		if t.kind != tokIdent {
			continue
		}
		switch {
		case t.value == "execute":
			// CREATE [EVENT] TRIGGER ... EXECUTE FUNCTION|PROCEDURE f()
			// names a routine; every other EXECUTE runs dynamic SQL or a
			// prepared statement.
			if j+1 < len(toks) && toks[j+1].kind == tokIdent && (toks[j+1].value == "function" || toks[j+1].value == "procedure") &&
				slices.ContainsFunc(toks[:j], func(x token) bool { return x.kind == tokIdent && x.value == "trigger" }) {
				continue
			}
			execute = true
		case t.value == "set_config":
			return "set_config may change search_path or another setting", execute
		case codeDataWords[t.value]:
			sc.data = true
		}
	}
	// Heads after the PL/pgSQL headers (stripPlpgsql) are checked by the
	// caller; these words change settings, transactions or run other code
	// wherever they start a statement.
	for _, t := range toks {
		if t.kind != tokIdent {
			continue
		}
		switch t.value {
		case "set", "reset", "discard", "load", "do", "commit", "rollback", "abort", "savepoint", "release",
			"import", "prepare", "deallocate", "vacuum":
			if startsStatement(toks, t) {
				return fmt.Sprintf("%s in code is not read", strings.ToUpper(t.value)), execute
			}
		}
	}
	return "", execute
}

// startsStatement reports whether t starts the statement once the
// PL/pgSQL headers are removed.
func startsStatement(toks []token, t token) bool {
	rest, _, ok := stripPlpgsql(toks, false)
	return !ok || len(rest) > 0 && rest[0].start == t.start
}

// stripPlpgsql removes the block, label, IF, CASE, loop and exception
// headers in front of a PL/pgSQL statement (pl_gram.y), and tracks the
// DECLARE section. ok is false when a header's end is not found.
func stripPlpgsql(toks []token, inDecl bool) ([]token, bool, bool) {
	for len(toks) > 0 {
		t := toks[0]
		if t.kind == tokOp && t.value == "<<" {
			k := slices.IndexFunc(toks, func(x token) bool { return x.kind == tokOp && x.value == ">>" })
			if k < 0 {
				return nil, inDecl, false
			}
			toks = toks[k+1:]
			continue
		}
		if t.kind != tokIdent {
			return toks, inDecl, true
		}
		switch t.value {
		case "declare":
			inDecl, toks = true, toks[1:]
		case "begin":
			inDecl, toks = false, toks[1:]
		case "else", "loop", "exception":
			toks = toks[1:]
		case "if", "elsif", "elseif", "when":
			k := headerEnd(toks, "then")
			if k < 0 {
				return nil, inDecl, false
			}
			toks = toks[k+1:]
		case "case":
			k := headerEnd(toks, "when")
			if k < 0 {
				return nil, inDecl, false
			}
			toks = toks[k:]
		case "while", "for", "foreach":
			k := headerEnd(toks, "loop")
			if k < 0 {
				return nil, inDecl, false
			}
			toks = toks[k+1:]
		default:
			return toks, inDecl, true
		}
	}
	return toks, inDecl, true
}

// headerEnd finds the first word at parenthesis depth 0, outside CASE
// expressions, after the header's first token.
func headerEnd(toks []token, word string) int {
	depth, cases := 0, 0
	for i := 1; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.kind == tokPunct && (t.value == "(" || t.value == "["):
			depth++
		case t.kind == tokPunct && (t.value == ")" || t.value == "]"):
			depth--
		case t.kind == tokIdent && depth == 0 && t.value == "case" && word != "when":
			cases++
		case t.kind == tokIdent && depth == 0 && t.value == "end" && cases > 0:
			cases--
		case t.kind == tokIdent && depth == 0 && cases == 0 && t.value == word:
			return i
		}
	}
	return -1
}

// codeStatement is one analysed statement of an inventory.
type codeStatement struct {
	st  statement
	src []byte
	a   analysis
	// protected as in srcStatement.
	protected bool
}

// inventory is the DDL an O1 statement may run.
type inventory struct {
	unbounded  string
	statements []codeStatement
	// static are the static statements of the PL/pgSQL bodies the
	// statement runs (Y49); stale says why a PL/pgSQL body it may call
	// names an object changed since the body was created (Y50).
	static []srcStatement
	stale  string
	// writes and writesAll are the data statements' targets (V5).
	writes    []qname
	writesAll bool
	// deletes and deletesAll are the DELETE targets (SB-M9, T024
	// review).
	deletes    []qname
	deletesAll bool
	// compile says why a PL/pgSQL body the statement may call is not
	// proven to compile at that call (V1, O1.P14).
	compile string
}

// o1Kinds are the opaque kinds of row O1.
var o1Kinds = map[string]bool{"do": true, "call": true, "execute": true, "function_call": true, "dml": true, "runs_code": true}

// recordRoutine adds a top-level CREATE FUNCTION/PROCEDURE's body and the
// routine names a statement names outside a routine's own definition.
func (s *state) recordRoutine(toks []token, src []byte) {
	if s.code.bodies == nil {
		s.code.bodies, s.code.referenced = map[string][]*routineBody{}, map[string]bool{}
	}
	if name, b := routineBodyOf(toks, src); b != nil {
		if b.lang == "plpgsql" {
			b.callWhy = s.callCompile(toks, src, b)
		}
		if b.lang == "sql" && b.toks == nil && b.unread == "" && s.functionBodies() != cfbOn && !s.sqlConstantBody(toks) {
			// W3 (Revision 1.7): an AS body created while
			// check_function_bodies was off or unknown was not parsed;
			// its first call parses and analyses it (functions.c
			// init_sql_fcache, clauses.c inline_function).
			b.callWhy = "its LANGUAGE sql body was created with check_function_bodies off or unknown, so the call parses it first, and the pass does not prove it"
		}
		// Y50 (Revision 1.9): changes after this statement make the body's
		// plans stale.
		b.born = s.rev19c.seq
		s.code.bodies[name] = append(s.code.bodies[name], b)
		return
	}
	c := &cursor{toks: toks, src: src}
	if c.peekKw("alter", "function") || c.peekKw("alter", "procedure") || c.peekKw("alter", "routine") ||
		c.peekKw("drop", "function") || c.peekKw("drop", "procedure") || c.peekKw("drop", "routine") ||
		c.peekKw("comment") || c.peekKw("grant") || c.peekKw("revoke") {
		return
	}
	s.code.referenceWords(toks)
}

func (cs *codeState) referenceWords(toks []token) {
	if cs.referenced == nil {
		cs.referenced = map[string]bool{}
	}
	for _, t := range toks {
		if t.kind == tokIdent || t.kind == tokQuoted {
			cs.referenced[t.value] = true
		}
	}
}

// inventory computes the DDL inventory of an O1 statement.
func (s *state) inventory(st statement, src []byte, a analysis, functions map[string]bool) inventory {
	var inv inventory
	var queue []*routineBody
	queued := map[*routineBody]bool{}
	visitedName := map[string]bool{}
	push := func(b *routineBody) {
		if !queued[b] {
			queued[b] = true
			queue = append(queue, b)
		}
	}
	name := func(n string) {
		if visitedName[n] {
			return
		}
		visitedName[n] = true
		if s.code.routines[n] && inv.unbounded == "" {
			inv.unbounded = fmt.Sprintf("it may call %s, a routine created by code whose body the pass did not read", n)
		}
		for _, b := range s.code.bodies[n] {
			push(b)
		}
	}
	allData := false
	data := func() {
		if allData {
			return
		}
		allData = true
		var names []string
		for n, bs := range s.code.bodies {
			if s.code.referenced[n] {
				names = append(names, n)
				continue
			}
			for _, b := range bs {
				if b.trigger {
					names = append(names, n)
					break
				}
			}
		}
		for n := range s.code.routines {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			name(n)
		}
	}
	if a.opaqueKind == "do" {
		push(doBody(st.tokens, src))
	} else {
		for _, t := range st.tokens {
			if t.kind == tokIdent || t.kind == tokQuoted {
				name(t.value)
			}
		}
	}
	if a.opaqueKind == "dml" || a.opaqueKind == "execute" || a.opaqueKind == "runs_code" {
		data()
	}
	for len(queue) > 0 && inv.unbounded == "" {
		b := queue[0]
		queue = queue[1:]
		if b.callWhy != "" && inv.compile == "" {
			inv.compile = b.callWhy
		}
		sc := scanBody(b)
		if sc.unbounded != "" {
			inv.unbounded = sc.unbounded
			break
		}
		inv.static = append(inv.static, sc.static...)
		if why := s.y50Stale(b); why != "" && inv.stale == "" {
			inv.stale = why
		}
		for _, n := range sc.calls {
			name(n)
		}
		inv.writes = append(inv.writes, sc.writes...)
		inv.writesAll = inv.writesAll || sc.writesAll
		inv.deletes = append(inv.deletes, sc.deletes...)
		inv.deletesAll = inv.deletesAll || sc.deletesAll
		if sc.data {
			data()
		}
		for _, d := range sc.ddl {
			ca := analyze(d.st, d.src, functions, true)
			if why := codeUnreadable(d.st.tokens, d.src, ca); why != "" {
				inv.unbounded = why
				break
			}
			inv.statements = append(inv.statements, codeStatement{st: d.st, src: d.src, a: ca, protected: d.protected})
		}
	}
	return inv
}

// codeUnreadable says why an analysed body statement makes the inventory
// unbounded.
func codeUnreadable(toks []token, src []byte, a analysis) string {
	text := statementText(toks, src)
	switch {
	case a.class == classError:
		return fmt.Sprintf("a statement in the code could not be read (%s): %s", text, a.reason)
	case a.class == classOpaque && !o1Kinds[a.opaqueKind]:
		return fmt.Sprintf("a statement in the code has effects the pass does not follow (%s): %s", text, a.reason)
	case a.searchPath:
		return fmt.Sprintf("a statement in the code may change search_path (%s)", text)
	}
	for j, t := range toks {
		if t.kind == tokIdent && t.value == "cascade" && !(j > 0 && toks[j-1].kind == tokIdent && (toks[j-1].value == "delete" || toks[j-1].value == "update")) {
			return fmt.Sprintf("a statement in the code drops dependents with CASCADE (%s)", text)
		}
	}
	return ""
}

// statementText is a short display of a body statement: its written
// text, cut after 80 bytes at a character boundary.
func statementText(toks []token, src []byte) string {
	return statementTextUpTo(toks, src, 80)
}

// statementTextUpTo is statementText with another limit (SB-V5's text
// names the whole statement up to 400 bytes).
func statementTextUpTo(toks []token, src []byte, limit int) string {
	t := (&cursor{toks: toks, src: src}).text(0, len(toks))
	if len(t) <= limit {
		return t
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(t[cut]) {
		cut--
	}
	return t[:cut] + " ..."
}

// codeEffects applies an O1 statement's inventory to the state.
func (s *state) codeEffects(inv inventory) {
	if s.failure != nil {
		return
	}
	cs := &s.code
	if inv.compile != "" {
		// V1: a set routine's PL/pgSQL body is compiled at its first call
		// in the session.
		s.unknownFact(fmt.Sprintf("the statement may call a set routine whose body is compiled or parsed at the call, and that is not proven (%s)%s", inv.compile, precondition("O1.P14")))
	}
	if cs.may == nil {
		cs.may, cs.mayN1, cs.gone, cs.named, cs.routines = map[string]nameKind{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	}
	// U-c (Revision 1.9): Y48 statements code runs from constants, Y49
	// static statements, Y50 calls of routines whose objects changed;
	// after the code's effects are applied (the tables it changes are
	// marked whatever its verdict).
	defer s.rev19cCode(inv)
	s.codeWrites(inv)
	if inv.unbounded != "" {
		why := "the code's effects are unbounded: " + inv.unbounded
		for _, t := range s.tables {
			s.partial(t, why+precondition("O1.P12"))
		}
		s.unknownFact(why + "; it may create, change or drop any object and change search_path, so the statements after it, and sqlx's bookkeeping INSERT, may fail" + precondition("O1.P12"))
		return
	}
	if len(inv.statements) == 0 {
		return
	}
	var texts []string
	held := map[*table]bool{}
	for _, c := range inv.statements {
		texts = append(texts, statementText(c.st.tokens, c.src))
		s.codeStatementEffects(c, held)
	}
	s.set.diagnostic(s.m, s.at, "info", "schema_opaque_statement", fmt.Sprintf("the code may run: %s%s", strings.Join(texts, "; "), precondition("O1.P12")))
	for _, t := range s.tables {
		if held[t] || cs.named[t.q.name] {
			s.partialOnce(t, "code run by an earlier statement names this table: its facts and dependents are unknown"+precondition("O1.P12"))
		}
	}
}

// codeStatementEffects applies one inventory statement.
func (s *state) codeStatementEffects(c codeStatement, held map[*table]bool) {
	cs := &s.code
	a, toks := c.a, c.st.tokens
	// U-b (Revision 1.9): roles and constraint names code may change.
	s.rev19bCode(toks)
	// U-c (Revision 1.9 Y51): a superuser-only statement code runs.
	s.y51(toks, c.src)
	sn := namesOf(a, toks)
	for _, e := range sn.creates {
		cs.may[e.name] |= e.kind
		if e.kind&kindRelation != 0 {
			// V8: the names Postgres generates with a relation.
			if cs.tables == nil {
				cs.tables = map[string]bool{}
			}
			cs.tables[e.name] = true
		}
	}
	if ruleWrites(toks) {
		s.rev16.rulesWrite = true
	}
	s.userSchemaCode(sn)
	if a.create != nil && a.create.name.schema == nil || a.index != nil || slices.ContainsFunc(a.relations, func(o namedObject) bool { return o.q.schema == nil }) {
		cs.unqualified = true
	}
	for _, t := range toks {
		if t.kind == tokIdent || t.kind == tokQuoted {
			cs.named[t.value] = true
		}
	}
	cs.referenceWords(toks)
	if a.function != nil {
		cs.routines[*a.function] = true
		s.forgetRoutine(*a.function)
	}
	for _, n := range codeRemoved(a, sn, toks) {
		s.codeRemove(n)
	}
	cur := &cursor{toks: toks, src: c.src}
	head := ""
	if len(toks) > 0 {
		head = toks[0].value
	}
	if head == "create" || head == "alter" || head == "drop" {
		i := 1
		for i < len(toks) && toks[i].kind == tokIdent && (toks[i].value == "or" || toks[i].value == "replace" || toks[i].value == "constraint" || toks[i].value == "unique") {
			i++
		}
		if i < len(toks) && toks[i].kind == tokIdent {
			kind := toks[i].value
			if n1Kinds[kind] != "" {
				kind = n1Kinds[kind]
			}
			if n1Kinds[toks[i].value] != "" || kind == "trigger" {
				if head == "create" {
					cs.mayN1[kind] = true
				}
				s.n1.markRenamed(kind)
			}
		}
		switch {
		case cur.peekKw("alter", "function"), cur.peekKw("alter", "procedure"), cur.peekKw("alter", "routine"),
			cur.peekKw("drop", "function"), cur.peekKw("drop", "procedure"), cur.peekKw("drop", "routine"):
			cur.i = 2
			for _, q := range dropNames(cur) {
				cs.routines[q.name] = true
				s.forgetRoutine(q.name)
			}
		}
	}
	if a.enum != nil && !a.enum.rename {
		// K2: a label the code adds in this transaction is unsafe to use
		// until the commit.
		s.unsafe = append(s.unsafe, unsafeLabel{label: a.enum.label, names: []string{a.enum.q.name}})
	}
	columnsSafe := true
	if a.alter != nil {
		for _, act := range a.alter.actions {
			if act.kind != "add_column" && act.kind != "add_constraint" {
				columnsSafe = false
			}
		}
	}
	indexSafe := a.class != classUnsupported && head == "alter" && a.alter != nil
	if indexSafe {
		for _, act := range a.alter.actions {
			switch act.kind {
			case "add_column", "set_default", "drop_default", "set_not_null", "drop_not_null", "rename_column", "add_constraint", "alter_type":
			default:
				indexSafe = false
			}
		}
	}
	for _, t := range slices.Clone(s.tables) {
		if !slices.ContainsFunc(toks, func(x token) bool { return (x.kind == tokIdent || x.kind == tokQuoted) && x.value == t.q.name }) {
			continue
		}
		if (head == "alter" || head == "drop") && !indexSafe {
			// The statement may rename, take or drop any index or key of
			// a table it names (row D's rule, U1).
			var gone []string
			for _, ix := range t.indexes {
				ix.uncertain, ix.mayBeGone = true, true
				if ix.name != nil {
					gone = append(gone, *ix.name)
				}
			}
			for _, k := range append([]*key{t.primaryKey}, t.uniques...) {
				if k != nil && k.name != nil {
					gone = append(gone, *k.name)
				}
			}
			t.keysUnknown = true
			for _, n := range append(gone, generatedNames(t)...) {
				cs.gone[n] = true
			}
		}
	}
	if a.alter != nil && !columnsSafe {
		// The code may have dropped, renamed or retyped columns: the
		// table's columns and keys are no longer facts (as P3's skipped
		// CREATE TABLE).
		if t, _ := s.lookup(a.alter.name); t != nil {
			forgetCreated(t)
		} else {
			for _, t := range s.tables {
				if t.q.name == a.alter.name.name {
					forgetCreated(t)
				}
			}
		}
	}
	for _, t := range s.tables {
		if cs.named[t.q.name] {
			held[t] = true
			if head == "create" && slices.ContainsFunc(toks, func(x token) bool { return x.kind == tokIdent && x.value == "trigger" }) {
				t.triggers = true
				t.triggerNamesUnknown, t.triggerColumnsUnknown = true, true
			}
			// W1 (Revision 1.7): a trigger or policy code creates, drops or
			// renames is only possibly held.
			if (head == "alter" || head == "drop") && slices.ContainsFunc(toks, func(x token) bool { return x.kind == tokIdent && x.value == "trigger" }) {
				t.triggerNamesUnknown = true
			}
			if (head == "create" || head == "alter" || head == "drop") && slices.ContainsFunc(toks, func(x token) bool { return x.kind == tokIdent && x.value == "policy" }) {
				t.policyNamesUnknown = true
			}
			// SB-M3: code that alters the table may change its row level
			// security.
			if head == "alter" {
				t.rlsUnknown = true
			}
		}
	}
	for k := range s.sequences {
		if cs.named[bareName(k)] {
			delete(s.sequences, k)
		}
	}
}

// forgetRoutine makes a routine name's definitions unknown (U6) after code
// may have changed it.
func (s *state) forgetRoutine(name string) {
	if s.routineUnknown == nil {
		s.routineDefs, s.routineUnknown = map[string][]*routineDef{}, map[string]bool{}
	}
	s.routineUnknown[name] = true
	if s.functionReturns != nil {
		s.functionReturns[name] = "?"
	}
}

// codeRemoved lists the names an inventory statement may drop, rename
// away or move to another schema.
func codeRemoved(a analysis, sn statementNames, toks []token) []string {
	var out []string
	if a.drop != nil {
		for _, q := range a.drop.names {
			out = append(out, q.name)
		}
	}
	for _, o := range a.dropped {
		out = append(out, o.q.name)
	}
	if a.dropOthers != nil {
		for _, q := range a.dropOthers.names {
			out = append(out, q.name)
		}
	}
	for _, e := range sn.drops {
		out = append(out, e.name)
	}
	if a.renamed != nil {
		out = append(out, a.renamed.q.name)
	}
	if a.alter != nil {
		for _, act := range a.alter.actions {
			if act.kind == "rename_table" || act.kind == "set_schema" {
				out = append(out, a.alter.name.name)
			}
		}
	}
	if a.alterIndex != nil {
		if a.alterIndex.newName != nil || slices.ContainsFunc(toks, func(t token) bool { return t.kind == tokIdent && t.value == "schema" }) {
			out = append(out, a.alterIndex.name.name)
		}
	}
	if a.schemaRename != nil {
		out = append(out, a.schemaRename[0])
	}
	// Any other ALTER ... RENAME TO or SET SCHEMA (views, sequences,
	// types, domains, functions): the object after the kind word.
	if len(toks) > 3 && toks[0].kind == tokIdent && toks[0].value == "alter" {
		for j := 2; j+1 < len(toks); j++ {
			if toks[j].kind == tokIdent && (toks[j].value == "rename" && toks[j+1].kind == tokIdent && toks[j+1].value == "to" || toks[j].value == "set" && toks[j+1].kind == tokIdent && toks[j+1].value == "schema") {
				c := &cursor{toks: toks, i: 2}
				c.acceptKw("view")
				c.acceptKw("if", "exists")
				if n, err := c.name(); err == nil {
					out = append(out, n.name)
				}
				break
			}
		}
	}
	return out
}

// codeRemove applies an object the code may have dropped, renamed or
// moved: the pass no longer holds it, and the name may still be taken.
func (s *state) codeRemove(name string) {
	s.code.gone[name] = true
	for _, t := range slices.Clone(s.tables) {
		if t.q.name == name {
			s.partialOnce(t, "code may have dropped, renamed or moved this table"+precondition("O1.P12"))
			s.uncertainTable(t)
		}
		for _, ix := range t.indexes {
			if ix.name != nil && *ix.name == name {
				ix.mayBeGone = true
			}
		}
	}
	for k, r := range s.relations {
		if k == name || strings.HasSuffix(k, "."+name) {
			r.uncertain = true
		}
	}
	s.forgetRoutine(name)
	for k := range s.sequences {
		if bareName(k) == name {
			delete(s.sequences, k)
		}
	}
	s.temps[name] = false
}

// p12IDs are the P12 preconditions of the rows that consume an
// inventory (every M5 row cites M5a.P12, R2 cites R1.P12).
var p12IDs = map[string]string{
	"M1": "M1.P12", "M1b": "M1b.P12", "M4": "M4.P12", "M6": "M6.P12", "M7": "M7.P12", "M8": "M8.P12",
	"R1": "R1.P12", "R2": "R1.P12", "R3": "R3.P12", "R5": "R5.P12", "R6": "R6.P12", "N1": "N1.P12",
}

// p12ID is the P12 precondition of a statement's row.
func p12ID(row string) string {
	if strings.HasPrefix(row, "M5") {
		return "M5a.P12"
	}
	if id, ok := p12IDs[row]; ok {
		return id
	}
	return "O1.P12"
}

// conditionalCreate reports CREATE ... IF NOT EXISTS and CREATE OR
// REPLACE (and ADD COLUMN IF NOT EXISTS) forms.
func conditionalCreate(toks []token) bool {
	for j := 0; j+1 < len(toks) && j < 12; j++ {
		if toks[j].kind != tokIdent {
			continue
		}
		if toks[j].value == "or" && toks[j+1].value == "replace" || toks[j].value == "if" && toks[j+1].value == "not" {
			return true
		}
	}
	return false
}

// conditionalExists reports an IF [NOT] EXISTS or OR REPLACE form.
func conditionalExists(toks []token) bool {
	for j := 0; j+1 < len(toks); j++ {
		if toks[j].kind == tokIdent && toks[j].value == "if" && toks[j+1].kind == tokIdent && (toks[j+1].value == "exists" || toks[j+1].value == "not") {
			return true
		}
	}
	return conditionalCreate(toks)
}

// codeConsumers applies P12 to a statement before it runs: names code may
// have created (P2) and objects that may have dependents code created
// (P8, P1).
func (s *state) codeConsumers(st statement, a analysis) {
	cs := &s.code
	if s.failure != nil || cs.may == nil {
		return
	}
	toks := st.tokens
	id := p12ID(a.row)
	sn := namesOf(a, toks)
	conditional := conditionalCreate(toks)
	for _, e := range sn.creates {
		// pg_class is one namespace; a relation that is not an index also
		// checks pg_type (heap.c 1164-1182), and a type meets the row
		// types of relations.
		mask := e.kind
		if e.kind&kindClass != 0 {
			mask |= kindClass
		}
		if e.kind&kindRelation != 0 {
			mask |= kindType
		}
		if e.kind&kindType != 0 {
			mask |= kindRelation
		}
		if cs.may[e.name]&mask == 0 && !cs.gone[e.name] && !cs.generatedMay(e.name, mask) {
			continue
		}
		if conditional {
			continue
		}
		s.unknownFact(fmt.Sprintf("the statement creates %s, a name that code run by an earlier statement may have created or left behind; Postgres refuses a name that is taken%s", e.name, precondition(id)))
		return
	}
	if !conditionalExists(toks) {
		for _, t := range toks {
			if (t.kind == tokIdent || t.kind == tokQuoted) && cs.gone[t.value] {
				s.unknownFact(fmt.Sprintf("the statement names %s, which code run by an earlier statement may have dropped, renamed or moved; Postgres refuses a missing object%s", t.value, precondition(id)))
				return
			}
		}
	}
	c := &cursor{toks: toks, src: s.m.source}
	if c.acceptKw("create") {
		c.acceptKw("or", "replace")
		c.acceptKw("constraint")
		if !c.done() && c.tok().kind == tokIdent {
			kind := c.tok().value
			if k := n1Kinds[kind]; k != "" {
				kind = k
			}
			if cs.mayN1[kind] && !conditional {
				s.unknownFact(fmt.Sprintf("the statement creates a %s, and code run by an earlier statement may have created one of the same name; Postgres refuses a name that is taken%s", kind, precondition("N1.P12")))
				return
			}
		}
	}
	cascade := slices.ContainsFunc(toks, func(t token) bool { return t.kind == tokIdent && t.value == "cascade" })
	if !cascade && len(toks) > 1 && toks[0].kind == tokIdent && toks[0].value == "drop" {
		// V8: a name code may have created or generated may have
		// dependents (a serial column's default depends on its sequence).
		for _, t := range toks[1:] {
			if (t.kind == tokIdent || t.kind == tokQuoted) && cs.generatedMay(t.value, kindClass|kindType) {
				s.unknownFact(fmt.Sprintf("the statement drops %s, which code run by an earlier statement may have created with objects that depend on it%s", t.value, precondition(id)))
				return
			}
		}
	}
	dependents := func(n, what string) bool {
		if !cs.named[n] {
			return false
		}
		s.unknownFact(fmt.Sprintf("%s may be refused: code run by an earlier statement names %s, so objects the pass does not see may depend on it%s", what, n, precondition(id)))
		return true
	}
	if a.drop != nil && !a.drop.cascade {
		for _, q := range a.drop.names {
			what := fmt.Sprintf("DROP %s %s", strings.ToUpper(a.drop.kind), q)
			if dependents(q.name, what) {
				return
			}
			if a.drop.kind == "index" {
				if t, _ := s.findIndex(q); t != nil && dependents(t.q.name, what) {
					return
				}
				if t, _ := s.findKey(q); t != nil && dependents(t.q.name, what) {
					return
				}
			}
			if a.drop.kind == "schema" && cs.unqualified {
				s.unknownFact(fmt.Sprintf("%s may be refused: code run by an earlier statement may have created objects in it%s", what, precondition(id)))
				return
			}
		}
	}
	if !cascade {
		var others []qname
		for _, o := range a.dropped {
			others = append(others, o.q)
		}
		if a.dropOthers != nil {
			others = append(others, a.dropOthers.names...)
		}
		for _, e := range sn.drops {
			others = append(others, qname{name: e.name})
		}
		for _, q := range others {
			if dependents(q.name, "DROP "+q.String()) {
				return
			}
		}
	}
	if a.alter != nil && cs.named[a.alter.name.name] {
		for _, act := range a.alter.actions {
			switch {
			case act.kind == "drop_column" && !act.cascade, act.kind == "drop_constraint" && !act.cascade,
				act.kind == "alter_type", act.kind == "rename_column", act.kind == "rename_table":
				dependents(a.alter.name.name, "ALTER TABLE "+a.alter.name.String())
				return
			}
		}
	}
	if a.enum != nil && cs.named[a.enum.q.name] {
		s.unknownFact(fmt.Sprintf("ALTER TYPE %s may be refused: code run by an earlier statement may have changed its labels%s", a.enum.q, precondition("R3.P12")))
		return
	}
	if a.create != nil {
		var parents []qname
		parents = append(parents, a.inherits...)
		for _, p := range []*qname{a.create.partitionOf, a.create.likeOf, a.create.ofType} {
			if p != nil {
				parents = append(parents, *p)
			}
		}
		for _, p := range parents {
			if cs.named[p.name] {
				s.unknownFact(fmt.Sprintf("CREATE TABLE %s may be refused: code run by an earlier statement names %s, so partitions, columns or objects the pass does not see may meet it%s", a.create.name, p, precondition("M1b.P12")))
				return
			}
		}
	}
}

// codeAfter applies P3 to a conditional creation of a name code may have
// created: what the statement leaves is not a fact.
func (s *state) codeAfter(sn statementNames) {
	cs := &s.code
	if cs.may == nil {
		return
	}
	for _, e := range sn.creates {
		if cs.may[e.name] == 0 && !cs.gone[e.name] {
			continue
		}
		// An object of the name exists now (whose, and of which kind, is
		// P3's question).
		delete(cs.gone, e.name)
		for k, r := range s.relations {
			if k == e.name || strings.HasSuffix(k, "."+e.name) {
				r.uncertain = true
			}
		}
		for k := range s.sequences {
			if bareName(k) == e.name {
				delete(s.sequences, k)
			}
		}
		for _, t := range s.tables {
			if t.q.name == e.name {
				s.partialOnce(t, "a conditional CREATE over a name code may have created: the object may be the code's"+precondition("M1.P12"))
			}
		}
	}
}

// codeStatements visits the statements the analyser reads in the DO body
// or routine body of st, and in the bodies those statements define, to a
// fixed depth.
func codeStatements(st statement, src []byte, depth int, visit func(statement, []byte)) {
	if depth > 8 || len(st.tokens) == 0 || st.tokens[0].kind != tokIdent {
		return
	}
	var b *routineBody
	switch st.tokens[0].value {
	case "do":
		b = doBody(st.tokens, src)
	case "create":
		_, b = routineBodyOf(st.tokens, src)
	}
	if b == nil || b.unread != "" {
		return
	}
	sc := scanBody(&routineBody{lang: b.lang, text: b.text, toks: b.toks, src: b.src, all: true})
	for _, d := range sc.ddl {
		visit(d.st, d.src)
		codeStatements(d.st, d.src, depth+1, visit)
	}
}

package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Rules of T018a2 Revision 1.6 step D2: V1 (class P14) applied to CREATE
// FUNCTION / PROCEDURE and DO, with check_function_bodies tracked like
// search_path (U3), and the routine signature rules the validators apply
// whatever the setting (M2.P1, R6.P14).

// cfbValue is a value of check_function_bodies the pass follows.
type cfbValue uint8

const (
	cfbUnset cfbValue = iota // not set: E4's value (on)
	cfbOn
	cfbOff
	cfbUnknown
)

// functionBodies is the check_function_bodies value in force.
func (s *state) functionBodies() cfbValue {
	switch {
	case s.rev16.cfbLocal != cfbUnset:
		return s.rev16.cfbLocal
	case s.rev16.cfbSession != cfbUnset:
		return s.rev16.cfbSession
	}
	return cfbOn
}

// cfbBool reads a GUC boolean value (guc.c parse_bool).
func cfbBool(v string) cfbValue {
	b, ok := parseBool(v)
	switch {
	case !ok:
		return cfbUnknown
	case b:
		return cfbOn
	}
	return cfbOff
}

// functionBodiesStatement follows SET / RESET / set_config of
// check_function_bodies with U3's scope rules: a constant SET [SESSION |
// LOCAL] check_function_bodies {TO | =} value | DEFAULT, RESET
// check_function_bodies, RESET ALL, DISCARD ALL and SELECT
// [pg_catalog.]set_config('check_function_bodies', '<constant>', bool).
// Any other statement of the migration string that names the setting
// (other than one that sets it for later sessions or for a routine) makes
// it unknown.
func (s *state) functionBodiesStatement(toks []token) {
	if len(toks) == 0 || toks[0].kind != tokIdent {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	local, value, matched := false, cfbUnset, true
	switch {
	case c.acceptKw("set"):
		if c.acceptKw("local") {
			local = true
		} else {
			c.acceptKw("session")
		}
		if !c.acceptKw("check_function_bodies") {
			matched = false
			break
		}
		if !c.acceptKw("to") && !(!c.done() && c.tok().kind == tokOp && c.tok().value == "=" && c.next()) {
			value = cfbUnknown
			break
		}
		t := c.tok()
		switch {
		case c.done():
			value = cfbUnknown
		case t.kind == tokIdent && t.value == "default":
			value = cfbOn
		case t.kind == tokIdent || t.kind == tokQuoted || t.kind == tokNumber:
			value = cfbBool(t.value)
		case t.kind == tokString:
			v, ok := literalValue(t, c.src)
			value = cfbUnknown
			if ok {
				value = cfbBool(v)
			}
		default:
			value = cfbUnknown
		}
		c.i++
		if !c.done() {
			value = cfbUnknown
		}
	case c.peekKw("reset", "check_function_bodies"), c.peekKw("reset", "all"), c.peekKw("discard", "all"):
		value = cfbOn
	default:
		if v, l, ok := setConfigFunctionBodies(toks, c.src); ok {
			value, local = v, l
			break
		}
		matched = false
	}
	if !matched {
		if !mentionsFunctionBodies(toks, c.src) || c.peekKw("alter", "role") || c.peekKw("alter", "user") || c.peekKw("alter", "database") ||
			c.peekKw("alter", "system") || c.peekKw("alter", "function") || c.peekKw("alter", "procedure") || c.peekKw("alter", "routine") ||
			c.peekKw("create", "function") || c.peekKw("create", "procedure") || c.peekKw("create", "or", "replace") {
			return
		}
		value, local = cfbUnknown, false
	}
	switch {
	case s.pathMode == pathUnclear:
		s.rev16.cfbSession, s.rev16.cfbLocal = cfbUnknown, cfbUnknown
	case local && s.pathMode == pathSingle:
		// SET LOCAL outside a block has no effect (25P01, warning).
	case local:
		s.rev16.cfbLocal = value
	default:
		s.rev16.cfbSession, s.rev16.cfbLocal = value, cfbUnset
	}
}

// mentionsFunctionBodies reports a statement that names
// check_function_bodies as a word or in a string constant.
func mentionsFunctionBodies(toks []token, src []byte) bool {
	for _, t := range toks {
		switch t.kind {
		case tokIdent, tokQuoted:
			if t.value == "check_function_bodies" {
				return true
			}
		case tokString:
			if strings.Contains(strings.ToLower(t.value), "check_function_bodies") {
				return true
			}
		}
	}
	return false
}

// setConfigFunctionBodies reads SELECT [pg_catalog.]set_config(
// 'check_function_bodies', '<constant>', true | false).
func setConfigFunctionBodies(toks []token, src []byte) (cfbValue, bool, bool) {
	if len(toks) == 0 || toks[0].kind != tokIdent || toks[0].value != "select" {
		return 0, false, false
	}
	i := 1
	if len(toks) > 3 && toks[1].kind == tokIdent && toks[1].value == "pg_catalog" && toks[2].kind == tokPunct && toks[2].value == "." {
		i = 3
	}
	if len(toks) != i+8 || toks[i].kind != tokIdent || toks[i].value != "set_config" {
		return 0, false, false
	}
	punct := func(k int, v string) bool { return toks[k].kind == tokPunct && toks[k].value == v }
	if !punct(i+1, "(") || !punct(i+3, ",") || !punct(i+5, ",") || !punct(i+7, ")") ||
		toks[i+2].kind != tokString || toks[i+4].kind != tokString || toks[i+6].kind != tokIdent {
		return 0, false, false
	}
	name, ok := literalValue(toks[i+2], src)
	if !ok || !strings.EqualFold(name, "check_function_bodies") {
		return 0, false, false
	}
	v, ok := literalValue(toks[i+4], src)
	value := cfbUnknown
	if ok {
		value = cfbBool(v)
	}
	switch toks[i+6].value {
	case "true":
		return value, true, true
	case "false":
		return value, false, true
	}
	return cfbUnknown, false, true
}

// routineHeader is a CREATE FUNCTION / PROCEDURE signature as written.
type routineHeader struct {
	kind   string
	params []routineParam
	// table are the columns of RETURNS TABLE.
	table  []routineParam
	ret    string
	retSet bool
	// unread says why the signature is not read ("" when it is).
	unread string
	// options is the index of the first token after the signature.
	options int
}

// readRoutineHeader reads the signature of CREATE [OR REPLACE] FUNCTION |
// PROCEDURE; nil when the statement is not one.
func readRoutineHeader(toks []token, src []byte) *routineHeader {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create") {
		return nil
	}
	c.acceptKw("or", "replace")
	h := &routineHeader{}
	switch {
	case c.acceptKw("function"):
		h.kind = "function"
	case c.acceptKw("procedure"):
		h.kind = "procedure"
	default:
		return nil
	}
	if _, err := c.funcName(); err != nil || !c.isPunct("(") {
		h.unread = "the routine's name or parameters are not read"
		return h
	}
	end, err := c.group()
	if err != nil {
		h.unread = "the routine's parameters are not read"
		return h
	}
	if end > c.i+2 {
		for _, r := range c.splitList(c.i+1, end-1) {
			p, ok := routineParameter(c, r[0], r[1])
			if !ok || r[0] >= r[1] {
				h.unread = "a parameter is not read"
				return h
			}
			h.params = append(h.params, p)
		}
	}
	c.i = end
	defer func() { h.options = c.i }()
	if h.kind == "procedure" {
		return h
	}
	switch {
	case c.acceptKw("returns", "table"):
		h.retSet = true
		if !c.isPunct("(") {
			h.unread = "RETURNS TABLE is not read"
			return h
		}
		e, err := c.group()
		if err != nil {
			h.unread = "RETURNS TABLE is not read"
			return h
		}
		for _, r := range c.splitList(c.i+1, e-1) {
			sub := &cursor{toks: c.toks[:r[1]], i: r[0], src: c.src}
			name, err := sub.ident()
			start := sub.i
			if err != nil || columnType(sub) != nil || !sub.done() {
				h.unread = "a RETURNS TABLE column is not read"
				return h
			}
			h.table = append(h.table, routineParam{mode: "out", name: name, typ: sub.text(start, sub.i)})
		}
		c.i = e
	case c.acceptKw("returns"):
		h.retSet = c.acceptKw("setof")
		start := c.i
		if columnType(c) != nil {
			h.unread = "the result type is not read"
			return h
		}
		h.ret = c.text(start, c.i)
	}
	return h
}

// plAllowedPseudo are the pseudo-types plpgsql_validator accepts as a
// result (pl_handler.c 469-482); arguments take record and the
// polymorphic types only.
func plPolymorphic(name string) bool {
	switch name {
	case "anyelement", "anyarray", "anynonarray", "anyenum", "anyrange", "anymultirange",
		"anycompatible", "anycompatiblearray", "anycompatiblenonarray", "anycompatiblerange", "anycompatiblemultirange":
		return true
	}
	return false
}

// typeName is the bare name of a written type ("" when not read).
func typeName(typ string) string {
	_, name, dims, ok := splitType(typ)
	if !ok || dims > 0 {
		return ""
	}
	return name
}

// sigVerdict is why a routine signature is not proven (why "" when it is).
type sigVerdict struct {
	why, id string
	refused bool
}

// routineSignature applies M2.P1 to the types of a routine signature and
// the validators' pseudo-type rules (pl_handler.c plpgsql_validator,
// pg_proc.c fmgr_sql_validator), which run whatever check_function_bodies
// is. It returns the routine as PL/pgSQL compiles it, or why the
// signature is not proven.
func (s *state) routineSignature(h *routineHeader, lang string) (*plRoutine, sigVerdict) {
	pl := lang == "plpgsql"
	notProven := func(why, id string) (*plRoutine, sigVerdict) { return nil, sigVerdict{why: why, id: id} }
	refused := func(why, id string) (*plRoutine, sigVerdict) {
		return nil, sigVerdict{why: why, id: id, refused: true}
	}
	if h.unread != "" {
		return notProven(h.unread, "M2.P1")
	}
	if h.kind == "function" && h.ret == "" && len(h.table) == 0 {
		out := false
		for _, p := range h.params {
			out = out || p.mode == "out" || p.mode == "inout"
		}
		if !out {
			// compute_return_type: "function result type must be specified"
			// (42P13).
			return refused("a function without RETURNS and without OUT parameters (42P13)", "R6.P14")
		}
	}
	types := []string{}
	for _, p := range h.params {
		types = append(types, p.typ)
	}
	for _, p := range h.table {
		types = append(types, p.typ)
	}
	if h.ret != "" {
		types = append(types, h.ret)
	}
	for _, t := range types {
		if why := s.typeProven(t); why != "" && !pseudoType(t) {
			return notProven(why, "M2.P1")
		}
	}
	r := &plRoutine{kind: h.kind, params: map[string]bool{}, scalarParams: map[string]bool{}}
	ret := typeName(h.ret)
	if h.ret != "" && pseudoType(h.ret) {
		switch {
		case pl && ret == "trigger" && !h.retSet:
			r.dmlTrigger = true
		case pl && ret == "event_trigger" && !h.retSet:
			r.eventTrigger = true
		case ret == "record", ret == "void", plPolymorphic(ret):
		case pl && (ret == "trigger" || ret == "event_trigger"):
			return notProven("a set of a trigger type is not proven", "R6.P14")
		case pl:
			return refused(fmt.Sprintf("PL/pgSQL functions cannot return type %s (0A000)", h.ret), "R6.P14")
		case lang == "sql":
			return refused(fmt.Sprintf("SQL functions cannot return type %s (42P13)", h.ret), "R6.P14")
		}
	}
	all := append(append([]routineParam{}, h.params...), h.table...)
	for _, p := range all {
		if !pseudoType(p.typ) {
			continue
		}
		name := typeName(p.typ)
		switch {
		case plPolymorphic(name):
		case pl && name == "record":
		case pl:
			return refused(fmt.Sprintf("PL/pgSQL functions cannot accept type %s (0A000)", p.typ), "R6.P14")
		case lang == "sql" && p.mode != "out":
			return refused(fmt.Sprintf("SQL functions cannot have arguments of type %s (42P13)", p.typ), "R6.P14")
		}
	}
	for _, p := range all {
		if p.name != "" {
			r.params[p.name] = true
			r.scalarParams[p.name] = plScalarType(p.typ)
		}
		if p.mode == "out" || p.mode == "inout" {
			r.returnNone = true
		}
	}
	r.nparams = len(h.params)
	r.returnNone = r.returnNone || h.retSet || h.kind == "procedure" || ret == "void" || r.eventTrigger || len(h.table) > 0
	return r, sigVerdict{}
}

// routineCompile applies V1 to CREATE FUNCTION / PROCEDURE and DO.
func (s *state) routineCompile(st statement) {
	toks := st.tokens
	if len(toks) == 0 || toks[0].kind != tokIdent {
		return
	}
	src := s.m.source
	if toks[0].value == "do" {
		b := doBody(toks, src)
		if b.lang != "plpgsql" {
			// V2 and do.c decide (O1.P14, O1.P12).
			return
		}
		what := "DO"
		if b.unread != "" {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s, so its compilation is not proven%s", what, b.unread, precondition("O1.P14")))
			return
		}
		r := &plRoutine{what: what, kind: "do", returnNone: true, params: map[string]bool{}}
		s.compileVerdict(what, s.proveBody(b.text, r, false), "O1.P14")
		return
	}
	h := readRoutineHeader(toks, src)
	if h == nil {
		return
	}
	_, b := routineBodyOf(toks, src)
	what := "CREATE " + strings.ToUpper(h.kind)
	lang := ""
	if b != nil {
		lang = b.lang
	}
	if b != nil && b.toks == nil && lang == "" {
		// interpret_AS_clause: "no language specified" (42P13).
		s.mayFail(fmt.Sprintf("%s: no language specified (Postgres refuses)%s", what, precondition("R6.P1")))
		return
	}
	if why, refused := routineOptions(toks, src, h); refused {
		s.mayFail(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, why, precondition("R6.P4")))
		return
	} else if why != "" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, why, precondition("R6.P4")))
		return
	}
	r, v := s.routineSignature(h, lang)
	switch {
	case v.refused:
		s.mayFail(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, v.why, precondition(v.id)))
		return
	case v.why != "":
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, v.why, precondition(v.id)))
		return
	}
	r.what = what
	if lang != "plpgsql" || s.failure != nil {
		return
	}
	if s.functionBodies() == cfbOff {
		// The validator skips the body (pl_handler.c 502); it is compiled
		// at the first call (O1.P14).
		return
	}
	if b.unread != "" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s, so its compilation is not proven%s", what, b.unread, precondition("R6.P14")))
		return
	}
	s.compileVerdict(what, s.proveBody(b.text, r, false), "R6.P14")
}

// compileVerdict applies a body's proof to the statement that compiles it.
func (s *state) compileVerdict(what string, proof plProof, id string) {
	switch {
	case proof.why == "":
	case proof.refused:
		s.mayFail(fmt.Sprintf("%s: the PL/pgSQL body does not compile: %s (Postgres refuses)%s", what, proof.why, precondition(id)))
	default:
		s.unknownFact(fmt.Sprintf("%s may be refused: the PL/pgSQL body is not in the provable subset (%s)%s", what, proof.why, precondition(id)))
	}
}

// callCompile says why a later compilation of a set routine's PL/pgSQL
// body (at its first call in the session, O1.P14) is not proven: "" when
// the body is in the subset with pg_catalog types only and its signature
// was read.
func (s *state) callCompile(toks []token, src []byte, b *routineBody) string {
	h := readRoutineHeader(toks, src)
	if h == nil || h.unread != "" {
		return "its signature is not read"
	}
	if b.unread != "" {
		return b.unread
	}
	r, v := s.routineSignature(h, "plpgsql")
	if v.why != "" {
		return "its signature is not proven"
	}
	if proof := s.proveBody(b.text, r, true); proof.why != "" {
		return proof.why
	}
	return ""
}

// stringBody reports CREATE FUNCTION / PROCEDURE whose body is an AS
// string (not a SQL-standard body, which is parsed at creation whatever
// check_function_bodies is).
func stringBody(toks []token, src []byte) bool {
	_, b := routineBodyOf(toks, src)
	return b != nil && b.toks == nil && b.unread == ""
}

// dataTarget records the target of a body statement that adds or changes
// rows (V5, P11): INSERT INTO, UPDATE, MERGE INTO and COPY ... FROM; WITH
// and a target that is not read write every table. DELETE's target is
// recorded apart (deletes), for SB-M9.
func (sc *bodyScan) dataTarget(toks []token, src []byte) {
	if len(toks) == 0 || toks[0].kind != tokIdent {
		return
	}
	c := &cursor{toks: toks, src: src}
	target := func() {
		if q, err := c.name(); err == nil {
			sc.writes = append(sc.writes, q)
			return
		}
		sc.writesAll = true
	}
	switch toks[0].value {
	case "insert", "merge":
		c.i = 1
		if !c.acceptKw("into") {
			sc.writesAll = true
			return
		}
		target()
	case "update":
		c.i = 1
		c.acceptKw("only")
		target()
	case "delete":
		// DELETE adds no rows (V5); its target is kept for SB-M9 (T024
		// review).
		c.i = 1
		if !c.acceptKw("from") {
			sc.deletesAll = true
			return
		}
		c.acceptKw("only")
		if q, err := c.name(); err == nil {
			sc.deletes = append(sc.deletes, q)
			return
		}
		sc.deletesAll = true
	case "copy":
		c.i = 1
		if c.isPunct("(") {
			// COPY (query) TO writes no table.
			return
		}
		if slices.ContainsFunc(toks, func(t token) bool { return t.kind == tokIdent && t.value == "from" }) {
			target()
		}
	case "with":
		sc.writesAll = true
	}
}

// codeWrites applies V5: from the O1 statement on, the targets of the
// data statements its inventory lists may hold rows the set wrote (P11);
// a target the pass does not resolve, one with triggers, a set rule that
// writes, and an unbounded inventory make every table one that may.
func (s *state) codeWrites(inv inventory) {
	if inv.unbounded != "" || inv.writesAll || len(inv.writes) > 0 && s.rev16.rulesWrite {
		s.allRowsWritten()
		return
	}
	for _, q := range inv.writes {
		t := s.writeTarget(q)
		if t == nil || t.triggers {
			s.allRowsWritten()
			return
		}
		t.setRows = true
		t.empty = false
	}
}

// ruleWrites reports CREATE [OR REPLACE] RULE whose action changes rows.
func ruleWrites(toks []token) bool {
	c := &cursor{toks: toks}
	if !c.peekKw("create", "rule") && !c.peekKw("create", "or", "replace", "rule") {
		return false
	}
	return slices.ContainsFunc(toks, func(t token) bool {
		return t.kind == tokIdent && (t.value == "insert" || t.value == "update" || t.value == "merge" || t.value == "copy")
	})
}

// generatedMay reports a name Postgres may have generated with a relation
// an inventory may have created (V8, U2's "names it may create"): in
// pg_class every <table>_<suffix> (serial and identity sequences, _pkey,
// _key, _excl, _idx and the numbered variants; ChooseRelationName may cut
// the table part when the name reaches 63 bytes), in pg_type the array
// type _<table> (makeArrayTypeName may add underscores or cut).
func (cs *codeState) generatedMay(name string, mask nameKind) bool {
	for t := range cs.tables {
		if mask&kindClass != 0 {
			if strings.HasPrefix(name, t+"_") {
				return true
			}
			if len(name) >= nameDataLen-8 {
				for k := 1; k < len(t); k++ {
					if strings.HasPrefix(name, t[:k]+"_") {
						return true
					}
				}
			}
		}
		if mask&kindType != 0 && strings.HasPrefix(name, "_") {
			base := strings.TrimLeft(name, "_")
			if base == t || len(name) >= nameDataLen-1 && strings.HasPrefix(t, base) {
				return true
			}
		}
	}
	return false
}

// userSchemaStatement applies V6 after a statement: from the first
// statement that may create a schema, the "$user" entry of E4's path may
// name a set schema (the current user's name is unknown, E4): CREATE
// SCHEMA s, CREATE SCHEMA AUTHORIZATION r (named r; CURRENT_USER,
// SESSION_USER and CURRENT_ROLE name the "$user" schema itself), ALTER
// SCHEMA ... RENAME TO s, and a schema code may create. DROP SCHEMA of
// every such schema returns the entry to "no schema".
func (s *state) userSchemaStatement(st statement, a analysis) {
	toks := st.tokens
	u := &s.rev16
	if u.userCandidates == nil {
		u.userCandidates = map[string]bool{}
	}
	sn := namesOf(a, toks)
	for _, e := range sn.drops {
		if e.kind&kindSchema != 0 {
			delete(u.userCandidates, e.name)
		}
	}
	for _, e := range sn.creates {
		if e.kind&kindSchema != 0 {
			u.userCandidates[e.name] = true
		}
	}
	c := &cursor{toks: toks, src: s.m.source}
	if c.acceptKw("create", "schema") {
		c.acceptKw("if", "not", "exists")
		if c.acceptKw("authorization") && !c.done() {
			switch t := c.tok(); {
			case t.kind == tokIdent && (t.value == "current_user" || t.value == "session_user" || t.value == "current_role"):
				u.userKnownUnknown = true
			default:
				u.userCandidates[t.value] = true
			}
		}
	}
	s.userSchemaPath()
}

// userSchemaCode applies V6 to the schemas an inventory may create.
func (s *state) userSchemaCode(sn statementNames) {
	for _, e := range sn.creates {
		if e.kind&kindSchema != 0 {
			s.rev16.userKnownUnknown = true
		}
	}
	s.userSchemaPath()
}

// userSchemaUnknown reports whether E4's "$user" entry may name a schema
// of the set.
func (s *state) userSchemaUnknown() bool {
	return len(s.rev16.userCandidates) > 0 || s.rev16.userKnownUnknown
}

// userSchemaPath keeps pathChanged (R2) in step: while the "$user" entry
// may name a set schema, an unqualified name's schema is not certain.
func (s *state) userSchemaPath() {
	s.pathChanged = s.rev16.pathSet || s.userSchemaUnknown()
}

// writeTarget resolves a data statement's target as U3 resolves names:
// the table the pass holds under exactly this name, when the name means no
// other table and its schema is certain; nil otherwise (a view or another
// relation kind included).
func (s *state) writeTarget(q qname) *table {
	exact, others := s.lookup(q)
	if exact == nil || len(others) > 0 || exact.kindUnknown || q.schema == nil && (s.pathChanged || s.temps[q.name]) {
		return nil
	}
	return exact
}

// routineOptions applies P4 to the option list of CREATE FUNCTION /
// PROCEDURE (functioncmds.c compute_function_attributes and
// compute_common_attribute, REL_17_6): every option at most once except
// SET (errorConflictingDefElem, 42601); volatility, STRICT, LEAKPROOF,
// COST, ROWS, SUPPORT and PARALLEL refused for a procedure (42P13); COST
// and ROWS positive, ROWS only for a set-returning function (22023);
// PARALLEL SAFE, RESTRICTED or UNSAFE; a SET clause proven as N2 proves a
// SET. TRANSFORM, WINDOW, SUPPORT and every form not listed are not
// proven. It returns why the statement may fail ("" when the options are
// proven), refused when Postgres refuses it.
func routineOptions(toks []token, src []byte, h *routineHeader) (string, bool) {
	if h.unread != "" {
		return "", false
	}
	p := &wp{cursor: &cursor{toks: toks, i: h.options, src: src}}
	seen := map[string]bool{}
	for !p.done() {
		if p.peekKw("return") || p.peekKw("begin", "atomic") {
			// The SQL-standard body (opt_routine_body) ends the list.
			return "", false
		}
		start := p.i
		key := ""
		switch {
		case p.kw("as"):
			key = "as"
			if !p.sconst() || p.punct(",") && !p.sconst() {
				return "AS is not read", false
			}
		case p.kw("language"):
			key = "language"
			if !p.nonReservedOrSconst() {
				return "LANGUAGE is not read", false
			}
		case p.peekKw("transform"), p.peekKw("window"), p.peekKw("support"):
			return fmt.Sprintf("%s is not proven", strings.ToUpper(p.tok().value)), false
		case p.peekKw("called"), p.peekKw("returns", "null"), p.peekKw("strict"):
			key = "strict"
		case p.peekKw("immutable"), p.peekKw("stable"), p.peekKw("volatile"):
			key = "volatility"
		case p.peekKw("external"), p.peekKw("security"):
			key = "security"
		case p.peekKw("leakproof"), p.peekKw("not", "leakproof"):
			key = "leakproof"
		case p.peekKw("cost"), p.peekKw("rows"), p.peekKw("parallel"):
			key = p.tok().value
		case p.peekKw("set"), p.peekKw("reset"):
			key = "set"
		default:
			return fmt.Sprintf("the routine option at %q is not read", p.text(p.i, p.i+1)), false
		}
		if key != "as" && key != "language" && !p.funcOption() {
			return fmt.Sprintf("the routine option %s is not read", p.text(start, start+1)), false
		}
		if key != "set" && seen[key] {
			return fmt.Sprintf("conflicting or redundant options: %s twice (42601)", strings.ToUpper(key)), true
		}
		seen[key] = true
		switch key {
		case "volatility", "strict", "leakproof", "cost", "rows", "parallel":
			if h.kind == "procedure" {
				return fmt.Sprintf("invalid attribute in procedure definition: %s (42P13)", strings.ToUpper(key)), true
			}
		}
		switch key {
		case "cost", "rows":
			v := p.text(start+1, p.i)
			if !numericToken(p.toks[p.i-1], src) || p.i != start+2 || strings.Trim(v, "0.") == "" {
				return fmt.Sprintf("%s %s is not proven positive", strings.ToUpper(key), v), false
			}
			if key == "rows" && !h.retSet {
				return "ROWS is not applicable when the function does not return a set (22023)", true
			}
		case "parallel":
			switch toks[p.i-1].value {
			case "safe", "restricted", "unsafe":
			default:
				return "PARALLEL takes SAFE, RESTRICTED or UNSAFE", false
			}
		case "set":
			sub := &cursor{toks: toks[:p.i], i: start + 1, src: src}
			if toks[start].value == "reset" {
				break
			}
			if _, _, ok := gucName(&cursor{toks: toks[:p.i], i: start + 1, src: src}); !ok || sub.peekKw("time", "zone") {
				return "a SET clause that is no setting is not proven", false
			}
			if why := placeholderSetting(&cursor{toks: toks[:p.i], i: start + 1, src: src}); why != "" {
				return why, false
			}
			if why := setChecks(sub); why != "" {
				return why, false
			}
		}
	}
	return "", false
}

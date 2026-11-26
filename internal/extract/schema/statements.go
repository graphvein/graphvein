package schema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Statement preconditions of T018a2 (Revision 1, classes P4, P5
// and P8) that belong to statements the state does not model one by one:
// routine kinds, security labels, column comments, extended statistics,
// the queries and SQL bodies a statement validates when it runs (R1b),
// TRUNCATE and CLUSTER. statementChecks runs after the statement's own
// handler; every possible failure it reports names its precondition.

// statementChecks applies the B1b preconditions to one analysed statement.
func (s *state) statementChecks(st statement, a analysis) {
	toks := st.tokens
	if len(toks) == 0 {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	s.noteRowSecurity(toks)
	s.routineKinds(toks)
	s.forgetRoutines(toks)
	s.n1Create(toks)
	s.n1DropOrRename(toks)
	// Revision 1.6: V2 languages, V13 type positions,
	// V11 system column names.
	s.routineLanguage(toks)
	// Revision 1.6 D2: V1 compilation of PL/pgSQL bodies (P14) and the
	// signature rules, then check_function_bodies (U3's scopes).
	s.routineCompile(st)
	s.functionBodiesStatement(toks)
	// V6: the "$user" entry once a set schema may be named like the user.
	s.userSchemaStatement(st, a)
	s.typePositions(toks)
	s.systemColumnUse(toks)
	switch {
	case c.peekKw("security", "label"):
		// P4: a security label needs a loaded label provider, which E1-E4
		// do not grant (seclabel.c: 22023 "no security label providers
		// have been loaded").
		s.unknownFact("SECURITY LABEL may be refused: it needs a loaded security label provider" + precondition("N1.P4"))
	case c.peekKw("comment", "on", "column"):
		s.commentColumn(toks)
	case c.peekKw("create", "statistics"):
		// P5: statistics columns must be held, distinct, at least two and
		// of types with a default btree operator class (statscmds.c); the
		// pass does not prove them.
		s.unknownFact("CREATE STATISTICS may be refused: its columns and their types are not proven" + precondition("N1.P5"))
	case c.peekKw("create", "trigger"), c.peekKw("create", "constraint", "trigger"), c.peekKw("create", "or", "replace", "trigger"), c.peekKw("create", "or", "replace", "constraint", "trigger"):
		s.createTrigger(toks)
	case c.peekKw("drop", "trigger"), c.peekKw("alter", "trigger"):
		s.dropOrAlterTrigger(toks)
	case c.peekKw("create", "policy"), c.peekKw("alter", "policy"):
		if s.set.tool == toolSupabase {
			// SB-M3: a policy is schema-neutral in a Supabase set; its
			// USING and WITH CHECK are not claimed and not checked.
			break
		}
		s.policyExpressions(toks)
	case c.peekKw("grant"), c.peekKw("revoke"):
		s.privilegeColumns(toks)
	case c.peekKw("truncate"):
		s.truncate(toks)
	case c.peekKw("cluster"):
		s.cluster(toks)
	case c.peekKw("create", "domain"):
		s.createDomain(toks)
	case c.peekKw("create", "type"):
		s.createCompositeType(toks)
	}
	if a.query == "" && sqlBody(toks) && s.functionBodies() == cfbOff && stringBody(toks, s.m.source) {
		// V1: with check_function_bodies off, fmgr_sql_validator does not
		// parse an AS body (pg_proc.c 864); R1b.P5 does not apply.
	} else if a.query != "" && s.simpleQueryProven(toks) || a.query == "" && sqlBody(toks) && s.sqlConstantBody(toks) {
		// The query is one the pass reads.
	} else if a.query == "" && sqlBody(toks) && stringBody(toks, s.m.source) && s.sqlQueryBody(toks) {
		// T024c1c: the AS body is parse-analysed at creation against what
		// the pass holds, and the analysed form proves it (sqlbody.go).
		// With check_function_bodies off or unknown it is not parsed now,
		// so the proof holds for this statement either way.
	} else if a.query != "" || sqlBody(toks) || sqlStandardBody(toks, s.m.source) {
		// P5 (R1b): Postgres analyses the query of a view, materialized
		// view, CREATE TABLE AS or SELECT INTO, and a LANGUAGE sql body
		// (check_function_bodies, E4), when the statement runs: every column
		// it names must exist and every expression must type. The pass
		// does not read queries, so the statement may fail.
		what := a.query
		if what == "" {
			what = "a LANGUAGE sql function body"
		}
		s.unknownFact(fmt.Sprintf("%s may be refused: the columns and types of its query are not proven%s", what, precondition("R1b.P5")))
	}
	if a.tempReferences {
		// P4: a temporary table may reference only temporary tables of
		// this session (tablecmds.c 9696-9705); the pass does not model
		// temporary tables.
		s.unknownFact("a foreign key of a temporary table may be refused: the persistence of the referenced table is not proven" + precondition("M1.P4"))
	}
}

// typeBase returns the canonical type of a written type without its
// modifiers (varchar(20) is varchar; arrays keep their brackets).
func typeBase(written string) (string, bool) {
	canon, ok := canonicalType(written)
	if !ok {
		return "", false
	}
	if strings.Contains(canon, "(") {
		// Revision 1.4 R4: modifiers Postgres refuses make the type
		// unreadable, so no proof uses it.
		if _, v, _ := typmodCheck(written); v == typmodRefused {
			return "", false
		}
	}
	if i := strings.Index(canon, "("); i >= 0 {
		if j := strings.Index(canon[i:], ")"); j >= 0 {
			canon = canon[:i] + canon[i+j+1:]
		}
	}
	return canon, true
}

// changePersistence applies ALTER TABLE SET LOGGED / SET UNLOGGED (M5k-a,
// P4; tablecmds.c ATPrepChangePersistence 17088-17200): SET LOGGED is
// refused while the table references an unlogged table, SET UNLOGGED while
// a permanent table references it (self-references are ignored). known is
// false when the table's foreign keys are not all known.
func (s *state) changePersistence(t *table, toLogged, known bool) {
	if t.unlogged != toLogged {
		// Already of that persistence: nothing to do.
		return
	}
	what := "SET UNLOGGED"
	if toLogged {
		what = "SET LOGGED"
	}
	defer func() { t.unlogged = !toLogged }()
	if !known {
		s.unproven(t, fmt.Sprintf("%s on %s may be refused: the table is partial, so the foreign keys that tie its persistence are unknown%s", what, t.q, precondition("M5k-a.P4")))
		return
	}
	if toLogged {
		for _, fk := range t.foreignKeys {
			switch {
			case fk.table == t:
			case fk.table == nil && s.names.creates(fk.written.table.name, kindAny):
				s.unproven(t, fmt.Sprintf("%s on %s may be refused: the persistence of %s, which it references, is unknown%s", what, t.q, fk.written.table, precondition("M5k-a.P4")))
				return
			case fk.table != nil && fk.table.unlogged:
				s.refuse(t, fmt.Sprintf("%s on %s is refused: it references the unlogged table %s%s", what, t.q, fk.table.q, precondition("M5k-a.P4")))
				return
			}
		}
		return
	}
	for _, fk := range s.referencing(t, nil) {
		o := s.owner(fk)
		switch {
		case o == nil || o == t:
		case fk.table == nil:
			s.unproven(t, fmt.Sprintf("%s on %s may be refused: an unresolved foreign key of %s may reference it%s", what, t.q, o.q, precondition("M5k-a.P4")))
			return
		case !o.unlogged:
			s.refuse(t, fmt.Sprintf("%s on %s is refused: the permanent table %s references it%s", what, t.q, o.q, precondition("M5k-a.P4")))
			return
		}
	}
}

// routineKinds applies P4's kind rule to the set's functions and
// procedures: a procedure called as a function (SELECT p()), a function
// CALLed, or a routine named by the other kind's ALTER, DROP, COMMENT ON,
// GRANT or REVOKE is refused (42809, 42883); CREATE [OR REPLACE] of a name
// held as the other kind may be refused (same arguments). A name held as
// both kinds (overloads) may fail. It also keeps s.routines up to date.
func (s *state) routineKinds(toks []token) {
	if s.routines == nil {
		s.routines, s.functionReturns = map[string]string{}, map[string]string{}
	}
	word := func(i int, w ...string) bool {
		for k, v := range w {
			if i+k >= len(toks) || toks[i+k].kind != tokIdent || toks[i+k].value != v {
				return false
			}
		}
		return true
	}
	kindAt := func(i int) string {
		switch {
		case word(i, "function"):
			return "function"
		case word(i, "procedure"):
			return "procedure"
		case word(i, "routine"):
			return "routine"
		}
		return ""
	}
	nameAt := func(i int) (string, int) {
		if i >= len(toks) || toks[i].kind != tokIdent && toks[i].kind != tokQuoted {
			return "", i
		}
		n := toks[i].value
		for i+2 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "." && (toks[i+2].kind == tokIdent || toks[i+2].kind == tokQuoted) {
			i += 2
			n = toks[i].value
		}
		return n, i + 1
	}
	use := func(name, as, what string) {
		held := s.routines[name]
		switch {
		case held == "" || as == "routine" || held == as:
		case held == "both":
			s.unknownFact(fmt.Sprintf("%s may be refused: %s is both a function and a procedure of the set, and which one the arguments mean is not proven%s", what, name, precondition("R6.P4")))
		default:
			s.unknownFact(fmt.Sprintf("%s is refused: %s is a %s, not a %s%s", what, name, held, as, precondition("R6.P4")))
		}
	}
	define := func(name, kind string) {
		switch held := s.routines[name]; {
		case held == "" || held == kind:
			s.routines[name] = kind
		default:
			s.routines[name] = "both"
		}
	}
	switch {
	case word(0, "create"):
		i := 1
		if word(i, "or", "replace") {
			i += 2
		}
		kind := kindAt(i)
		if kind == "" || kind == "routine" {
			return
		}
		name, _ := nameAt(i + 1)
		if name == "" {
			return
		}
		s.replaceRoutine(toks, i+1, kind, word(1, "or", "replace"))
		if held := s.routines[name]; held != "" && held != kind {
			s.unknownFact(fmt.Sprintf("CREATE %s %s may be refused: the set holds a %s of this name, and Postgres refuses the same arguments for another routine kind%s", strings.ToUpper(kind), name, held, precondition("R6.P4")))
		}
		define(name, kind)
		// P5: a function without arguments has the result type its RETURNS
		// names; any other definition of the name makes it unproven.
		ret := "?"
		if kind == "function" {
			c := &cursor{toks: toks, i: i + 1, src: s.m.source}
			if _, err := c.name(); err == nil && c.acceptPunct("(") && c.acceptPunct(")") && c.acceptKw("returns") {
				start := c.i
				if columnType(c) == nil {
					if base, ok := normalType(c.text(start, c.i)); ok && !strings.HasPrefix(base, "setof") && base != "trigger" && base != "void" {
						ret = base
					}
				}
			}
		}
		if old, ok := s.functionReturns[name]; ok && old != ret {
			ret = "?"
		}
		s.functionReturns[name] = ret
	case word(0, "alter") && kindAt(1) != "":
		kind := kindAt(1)
		name, next := nameAt(2)
		if name == "" {
			return
		}
		use(name, kind, "ALTER "+strings.ToUpper(kind)+" "+name)
		for j := next; j+2 < len(toks); j++ {
			if word(j, "rename", "to") {
				if to, _ := nameAt(j + 2); to != "" {
					held := s.routines[name]
					delete(s.routines, name)
					delete(s.functionReturns, name)
					s.functionReturns[to] = "?"
					if held != "" {
						define(to, held)
					}
				}
				break
			}
		}
	case word(0, "drop") && kindAt(1) != "":
		kind := kindAt(1)
		for _, q := range dropNames(&cursor{toks: toks, i: 2, src: s.m.source}) {
			use(q.name, kind, "DROP "+strings.ToUpper(kind)+" "+q.name)
			delete(s.routines, q.name)
			s.functionReturns[q.name] = "?"
		}
	case word(0, "comment", "on") && kindAt(2) != "":
		if name, _ := nameAt(3); name != "" {
			use(name, kindAt(2), "COMMENT ON "+strings.ToUpper(kindAt(2))+" "+name)
		}
	case word(0, "grant"), word(0, "revoke"):
		for j := 1; j+1 < len(toks); j++ {
			if word(j, "on") && kindAt(j+1) != "" {
				if name, _ := nameAt(j + 2); name != "" {
					use(name, kindAt(j+1), strings.ToUpper(toks[0].value)+" ON "+strings.ToUpper(kindAt(j+1))+" "+name)
				}
				break
			}
		}
	case word(0, "call"):
		if name, _ := nameAt(1); name != "" {
			use(name, "procedure", "CALL "+name)
		}
	default:
		for j, t := range toks {
			if (t.kind == tokIdent || t.kind == tokQuoted) && j+1 < len(toks) && toks[j+1].kind == tokPunct && toks[j+1].value == "(" {
				if j > 0 && toks[j-1].kind == tokPunct && toks[j-1].value == "." {
					continue
				}
				if held := s.routines[t.value]; held == "procedure" || held == "both" {
					use(t.value, "function", "the call of "+t.value+"()")
					return
				}
			}
		}
	}
}

// commentColumn applies P5 to COMMENT ON COLUMN [schema.]table.column: the
// column must be held on a table whose facts are known; a missing column
// of a complete table is a refusal (42703).
func (s *state) commentColumn(toks []token) {
	c := &cursor{toks: toks, i: 3, src: s.m.source}
	var parts []string
	for {
		n, err := c.ident()
		if err != nil {
			return
		}
		parts = append(parts, n)
		if !c.acceptPunct(".") {
			break
		}
	}
	if len(parts) < 2 {
		return
	}
	col := parts[len(parts)-1]
	q := qname{name: parts[len(parts)-2]}
	if len(parts) > 2 {
		schema := parts[len(parts)-3]
		q.schema = &schema
	}
	what := fmt.Sprintf("COMMENT ON COLUMN %s.%s", q, col)
	exact, others := s.lookup(q)
	if exact == nil {
		if _, _, found := s.relationAt(q); found || len(others) > 0 {
			// A view's or another relation's columns, or a table under
			// another qualification: not proven.
			s.unknownFact(what + " may be refused: the columns of the relation it names are not known" + precondition("N1.P5"))
		}
		return
	}
	certain := len(others) == 0 && (q.schema != nil || !s.pathChanged && !s.temps[q.name])
	switch {
	case exact.column(col) != nil && factsKnown(exact) && certain:
	case exact.column(col) == nil && complete(exact) && certain:
		s.refuse(exact, what+": the column does not exist (Postgres refuses)"+precondition("N1.P5"))
	default:
		s.unproven(exact, what+" may be refused: whether the table has this column is not proven"+precondition("N1.P5"))
	}
}

// truncate applies P8 to TRUNCATE [TABLE] [ONLY] name [*] [, ...]
// [RESTART | CONTINUE IDENTITY] [CASCADE | RESTRICT] (gram.y TruncateStmt;
// tablecmds.c ExecuteTruncateGuts and heap_truncate_check_FKs): without
// CASCADE, a foreign key from a table the statement does not truncate to
// one it does is a refusal (0A000); references not known (a partial table,
// an unresolved foreign key that may name it) may fail. A view or sequence
// cannot be truncated (42809).
func (s *state) truncate(toks []token) {
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	c.acceptKw("table")
	var names []qname
	for {
		c.acceptKw("only")
		n, err := c.name()
		if err != nil {
			s.unknownFact("TRUNCATE may be refused: its table list could not be read" + precondition("N3.P8"))
			return
		}
		names = append(names, n)
		if !c.done() && c.tok().kind == tokOp && c.tok().value == "*" {
			c.i++
		}
		if !c.acceptPunct(",") {
			break
		}
	}
	if !c.acceptKw("restart", "identity") {
		c.acceptKw("continue", "identity")
	}
	cascade := c.acceptKw("cascade")
	if !cascade {
		c.acceptKw("restrict")
	}
	if !c.done() {
		s.unknownFact("TRUNCATE may be refused: unexpected text after its table list" + precondition("N3.P8"))
		return
	}
	var listed []*table
	for _, q := range names {
		exact, others := s.lookup(q)
		if exact == nil {
			if kind, certain, found := s.relationAt(q); found && kind != "table" {
				if certain {
					s.unknownFact(fmt.Sprintf("TRUNCATE %s is refused: it names %s%s", q, article(kind), precondition("N3.P8")))
				} else {
					s.unknownFact(fmt.Sprintf("TRUNCATE %s may be refused: the name may be %s%s", q, article(kind), precondition("N3.P8")))
				}
			} else if len(others) > 0 {
				s.unknownFact(fmt.Sprintf("TRUNCATE %s may be refused: the name may mean a table whose references are unknown%s", q, precondition("N3.P8")))
			}
			continue
		}
		if len(others) > 0 || q.schema == nil && (s.pathChanged || s.temps[q.name]) {
			if !cascade {
				s.unknownFact(fmt.Sprintf("TRUNCATE %s may be refused: the name may mean another table, whose references are unknown%s", q, precondition("N3.P8")))
			}
			continue
		}
		listed = append(listed, exact)
	}
	if cascade {
		return
	}
	for _, t := range listed {
		if !factsKnown(t) {
			s.unknownFact(fmt.Sprintf("TRUNCATE %s may be refused: the table is partial, so the foreign keys that reference it are unknown%s", t.q, precondition("N3.P8")))
			return
		}
		for _, fk := range s.referencing(t, nil) {
			o := s.owner(fk)
			if o == nil || slices.Contains(listed, o) {
				continue
			}
			if fk.table == nil {
				s.unknownFact(fmt.Sprintf("TRUNCATE %s may be refused: an unresolved foreign key of %s may reference it%s", t.q, o.q, precondition("N3.P8")))
				return
			}
			s.refuse(t, fmt.Sprintf("TRUNCATE %s without CASCADE is refused: %s references it%s", t.q, o.q, precondition("N3.P8")))
			return
		}
	}
}

// cluster applies P8 to CLUSTER (gram.y ClusterStmt; cluster.c): CLUSTER t
// USING i needs i to be a held, valid, non-partial index of t with a
// clusterable access method (btree or gist); CLUSTER t without USING needs
// the index an earlier CLUSTER t USING i proved (42704 "there is no
// previously clustered index"); CLUSTER i ON t is the old spelling. A
// partitioned table is not proven. CLUSTER without a table is P6.
func (s *state) cluster(toks []token) {
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	if c.isPunct("(") {
		end, err := c.group()
		if err != nil {
			return
		}
		c.i = end
	}
	c.acceptKw("verbose")
	if c.done() {
		return
	}
	first, err := c.name()
	if err != nil {
		s.unknownFact("CLUSTER may be refused: the statement could not be read" + precondition("N3.P8"))
		return
	}
	tq, idx := first, ""
	switch {
	case c.acceptKw("on"):
		n, err := c.name()
		if err != nil {
			s.unknownFact("CLUSTER may be refused: the statement could not be read" + precondition("N3.P8"))
			return
		}
		tq, idx = n, first.name
	case c.acceptKw("using"):
		n, err := c.ident()
		if err != nil {
			s.unknownFact("CLUSTER may be refused: the statement could not be read" + precondition("N3.P8"))
			return
		}
		idx = n
	}
	what := "CLUSTER " + tq.String()
	exact, others := s.lookup(tq)
	if exact == nil {
		if _, _, found := s.relationAt(tq); found || len(others) > 0 {
			s.unknownFact(what + " may be refused: the indexes of the relation it names are not known" + precondition("N3.P8"))
		}
		return
	}
	t := exact
	certain := len(others) == 0 && (tq.schema != nil || !s.pathChanged && !s.temps[tq.name])
	if !certain || t.partitionKey != nil || !factsKnown(t) {
		s.unproven(t, what+" may be refused: the table's indexes are not known exactly"+precondition("N3.P8"))
		return
	}
	if idx == "" {
		if t.clustered == nil || !slices.Contains(t.indexes, t.clustered) {
			s.unproven(t, what+" may be refused: no clustered index of the table is proven (Postgres needs one without USING)"+precondition("N3.P8"))
		}
		return
	}
	for _, ix := range t.indexes {
		if ix.name != nil && *ix.name == idx {
			if ix.where != nil || ix.mayBeGone || ix.uncertain || ix.using != nil && *ix.using != "btree" && *ix.using != "gist" {
				s.unproven(t, fmt.Sprintf("%s USING %s may be refused: the index is partial, uncertain or of an access method that cannot cluster%s", what, idx, precondition("N3.P8")))
				return
			}
			t.clustered = ix
			return
		}
	}
	s.unproven(t, fmt.Sprintf("%s USING %s may be refused: %s is not proven to be an index of the table%s", what, idx, idx, precondition("N3.P8")))
}

// alterPasses is the P9 order table (T018a2 Revision 1): the pass in
// which Postgres executes each modelled ALTER TABLE subcommand
// (tablecmds.c AlterTablePass 146-162 and ATPrepCmd 4779-5010 at
// REL_17_6). ATPrepAlterColumnType checks its column when the statement
// is prepared, before any pass runs; executing ALTER TYPE in its pass,
// after the DROP pass and before ADD COLUMN, gives the same verdicts.
var alterPasses = map[string]int{
	"drop_column":    0, // AT_PASS_DROP
	"drop_default":   0, // AT_ColumnDefault without a default
	"drop_not_null":  0,
	"alter_type":     1, // AT_PASS_ALTER_TYPE
	"add_column":     2, // AT_PASS_ADD_COL
	"add_constraint": 6, // AT_PASS_ADD_CONSTR and the index passes after it
	"set_not_null":   7, // AT_PASS_COL_ATTRS
	"set_default":    10,
}

// passOrder returns the subcommands of an ALTER TABLE statement in the
// order the state applies them (P9). When two subcommands name the same
// column, Postgres's pass order decides the outcome: the subcommands are
// applied in pass order (stable within a pass) when every subcommand
// naming that column is in the order table; when one of them adds, drops
// or retypes the column and another is not in the table (or ALTER TYPE
// names it twice, which Postgres refuses) the statement may fail.
// Otherwise the written order is kept.
func (s *state) passOrder(t *table, at *alterTable) []alterAction {
	type use struct {
		action  int
		changes bool
	}
	uses := map[string][]use{}
	for i, a := range at.actions {
		var cols []string
		changes := false
		switch a.kind {
		case "add_column":
			cols, changes = []string{a.column.name}, true
		case "drop_column", "alter_type":
			cols, changes = []string{a.name}, true
		case "add_constraint":
			tc := a.constraints
			for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
				cols = append(cols, k.columns...)
			}
			for _, fk := range tc.foreignKeys {
				cols = append(cols, fk.local...)
			}
			for _, ch := range tc.checks {
				cols = append(cols, ch.names...)
			}
		default:
			if a.name != "" && a.kind != "drop_constraint" {
				cols = []string{a.name}
			}
		}
		for _, c := range cols {
			uses[c] = append(uses[c], use{i, changes})
		}
	}
	reorder := false
	for _, col := range slices.Sorted(maps.Keys(uses)) {
		us := uses[col]
		if len(us) < 2 {
			continue
		}
		changes := slices.ContainsFunc(us, func(u use) bool { return u.changes })
		types, listed := 0, true
		for _, u := range us {
			a := at.actions[u.action]
			if a.kind == "alter_type" {
				types++
			}
			if _, ok := alterPasses[a.kind]; !ok || types > 1 {
				listed = false
			}
		}
		switch {
		case listed:
			// Two modelled subcommands on one column: their effects
			// depend on the pass order (SET NOT NULL runs after DROP NOT
			// NULL whatever the written order).
			reorder = true
		case changes:
			s.unproven(t, fmt.Sprintf("ALTER TABLE %s may be refused: two of its subcommands name column %s, and the pass order of this pair is not in the P9 table%s", at.name, col, precondition("M5a.P9")))
			return at.actions
		}
	}
	if !reorder {
		return at.actions
	}
	ordered := slices.Clone(at.actions)
	slices.SortStableFunc(ordered, func(a, b alterAction) int { return alterPasses[a.kind] - alterPasses[b.kind] })
	return ordered
}

// exprTokens lexes an expression's text as one statement.
func exprTokens(text string) ([]token, bool) {
	stmts, fail := lexStatements([]byte(text))
	if fail != nil || len(stmts) != 1 {
		return nil, false
	}
	return stmts[0].tokens, true
}

// defaultProven applies P5 to a DEFAULT expression of column c: its
// result must be proven and assignable to the column's type (42804
// otherwise; parse_utilcmd.c / heap.c cookDefault coerce it in assignment
// context, and an untyped constant goes through the type's input
// function then). A default cannot name columns.
func (s *state) defaultProven(t *table, c *column, text, what, id string) {
	toks, ok := exprTokens(text)
	e := &exprEnv{s: s, src: []byte(text)}
	if ok {
		var r typed
		if r, ok = e.prove(toks); ok {
			ok = e.assignable(r, writtenType(c))
		}
	}
	if !ok {
		s.unproven(t, fmt.Sprintf("%s %s may be refused: the result type of the expression is not proven assignable to %s%s", what, text, c.typ, precondition(id)))
	}
}

// columnExpressions applies P5 to a new column's DEFAULT and generation
// expression. A generated column's expression must be proven IMMUTABLE
// (Revision 1.1; heap.c cookDefault: "generation expression is not
// immutable", 42P17) and assignable to the column's type; it may name the
// table's other non-generated columns.
func (s *state) columnExpressions(t *table, c *column, what, id string) {
	if c.def != nil && c.identity == nil {
		s.defaultProven(t, c, *c.def, what+" DEFAULT", id)
	}
	if c.generated == nil {
		return
	}
	toks, ok := exprTokens(*c.generated)
	e := &exprEnv{s: s, t: t, src: []byte(*c.generated)}
	if ok {
		var r typed
		if r, ok = e.prove(toks); ok {
			ok = r.vol == 'i' && e.assignable(r, writtenType(c))
		}
	}
	if !ok {
		s.unproven(t, fmt.Sprintf("%s: the generation expression of %s is not proven IMMUTABLE and of the column's type%s", what, c.name, precondition(id)))
	}
}

// retypeProven applies P5 and P8 to ALTER COLUMN c TYPE (M5c;
// tablecmds.c ATPrepAlterColumnType and ATExecAlterColumnType): without
// USING the old type must cast to the new one in assignment context
// (42804), with USING the expression's result must be proven and
// assignable (42846); the column's default is re-coerced the same way;
// an index or key on the column is rebuilt and needs a btree operator
// class for the new type (P8); a CHECK constraint, expression index or
// partial-index predicate that names the column is re-analysed, which the
// pass does not prove.
func (s *state) retypeProven(t *table, c *column, a alterAction) {
	what := fmt.Sprintf("ALTER COLUMN %s TYPE %s", c.name, a.typ)
	fail := func(why, id string) {
		s.unproven(t, fmt.Sprintf("%s may be refused: %s%s", what, why, precondition(id)))
	}
	newBase, okN := normalType(a.typ)
	oldBase, okO := normalType(writtenType(c))
	if !okN || !okO {
		fail("the old or new type is not read", "M5c.P5")
		return
	}
	e := &exprEnv{s: s, t: t}
	if a.using != nil {
		toks, ok := exprTokens(*a.using)
		e.src = []byte(*a.using)
		var r typed
		if ok {
			r, ok = e.prove(toks)
		}
		if !ok || !e.assignable(r, a.typ) {
			fail("the result type of the USING expression is not proven assignable to the new type", "M5c.P5")
			return
		}
	} else if ctx := castContext(oldBase, newBase); oldBase != newBase && ctx != 'i' && ctx != 'a' {
		fail(fmt.Sprintf("%s is not proven to cast to %s in assignment context", c.typ, a.typ), "M5c.P5")
		return
	}
	if c.def != nil {
		toks, ok := exprTokens(*c.def)
		de := &exprEnv{s: s, src: []byte(*c.def)}
		var r typed
		if ok {
			r, ok = de.prove(toks)
		}
		if ok && r.lit != nil {
			r = typed{typ: oldBase}
		}
		if !ok || !r.null && !r.any && castContext(r.typ, newBase) != 'i' && castContext(r.typ, newBase) != 'a' && r.typ != newBase {
			fail("the column's default is not proven to cast to the new type", "M5c.P5")
			return
		}
	}
	names := append([]string{c.name}, c.previousNames...)
	named := func(ids []string) bool {
		return slices.ContainsFunc(ids, func(n string) bool { return slices.Contains(names, n) })
	}
	for _, fk := range t.foreignKeys {
		if fk.table == nil && slices.Contains(fk.local, c) {
			// P8: Postgres rebuilds the foreign key and checks the new
			// type against the referenced key, whose type is not known.
			fail("an unresolved foreign key uses the column", "M5c.P8")
			return
		}
	}
	keyed := t.primaryKey != nil && slices.Contains(t.primaryKey.cols, c)
	for _, k := range t.uniques {
		keyed = keyed || slices.Contains(k.cols, c)
	}
	for _, ix := range t.indexes {
		for k, el := range ix.elements {
			switch {
			case ix.columns[k] == c:
				if ix.using != nil && *ix.using != "btree" {
					fail("an index of another access method uses the column", "M5c.P8")
					return
				}
				keyed = true
			case ix.columns[k] == nil && named(el.names):
				fail("an expression index uses the column", "M5c.P8")
				return
			}
		}
		if named(ix.whereNames) {
			fail("a partial index predicate names the column", "M5c.P8")
			return
		}
	}
	if keyed {
		if !btreeType(newBase) {
			fail("an index or key on the column is rebuilt, and the new type is not proven to have a btree operator class", "M5c.P8")
			return
		}
	}
	for _, ch := range t.checks {
		if named(ch.names) {
			fail("a CHECK constraint names the column", "M5c.P8")
			return
		}
	}
	for _, ids := range t.unnamedCheckNames {
		if named(ids) {
			fail("a CHECK constraint names the column", "M5c.P8")
			return
		}
	}
	if t.checksUnknown {
		fail("a CHECK constraint of the table may name the column", "M5c.P8")
	}
}

// indexExpressions applies P5 with Revision 1.1 to CREATE INDEX: every
// expression element and the predicate must be proven, and IMMUTABLE
// (indexcmds.c CheckMutability: "functions in index expression must be
// marked IMMUTABLE", 42P17); the predicate must be boolean. An element
// with a collation or operator class is not proven.
func (s *state) indexExpressions(t *table, ci *createIndex) {
	what := "CREATE INDEX ON " + ci.table.String()
	method := "btree"
	if ci.using != nil {
		method = *ci.using
	}
	if ci.unique && strings.ToLower(method) != "btree" {
		// P4 (U7, found by g8 in Revision 1.6 D1): of the built-in access
		// methods only btree has amcanunique (indexcmds.c DefineIndex,
		// 0A000 "access method does not support unique indexes"); an
		// extension's method is not proven either.
		s.refuse(t, fmt.Sprintf("%s: access method %s does not support unique indexes (Postgres refuses, 0A000)%s", what, method, precondition("M4.P4")))
		return
	}
	for _, el := range ci.elements {
		if el.column != nil {
			// U7 (P4): the column's type needs a default operator class
			// of the access method.
			if c := t.column(*el.column); c != nil {
				if el.opclass != nil {
					// V9 (Revision 1.6): a written operator class must
					// accept the column's type.
					if why, refused := s.writtenOpclass(method, *el.opclass, writtenType(c)); why != "" {
						if refused {
							s.refuse(t, fmt.Sprintf("%s: column %s: %s%s", what, c.name, why, precondition("M4.P4")))
						} else {
							s.unproven(t, fmt.Sprintf("%s: column %s: %s%s", what, c.name, why, precondition("M4.P4")))
						}
						return
					}
					continue
				}
				if why := s.opclassProven(method, writtenType(c)); why != "" {
					s.unproven(t, fmt.Sprintf("%s: column %s: %s%s", what, c.name, why, precondition("M4.P4")))
					return
				}
			}
			continue
		}
		toks, ok := exprTokens(el.text)
		if ok {
			for len(toks) > 0 {
				last := toks[len(toks)-1]
				if last.kind == tokIdent && (last.value == "asc" || last.value == "desc" || last.value == "first" || last.value == "last" || last.value == "nulls") {
					toks = toks[:len(toks)-1]
					continue
				}
				break
			}
		}
		e := &exprEnv{s: s, t: t, src: []byte(el.text)}
		var r typed
		if ok {
			r, ok = e.prove(toks)
		}
		if !ok || r.vol != 'i' || r.typ == "" {
			s.unproven(t, fmt.Sprintf("%s: the index expression %s is not proven IMMUTABLE and typed%s", what, el.text, precondition("M4.P5")))
			return
		}
		if why := s.opclassProven(method, r.typ); why != "" {
			s.unproven(t, fmt.Sprintf("%s: the index expression %s: %s%s", what, el.text, why, precondition("M4.P4")))
			return
		}
	}
	if ci.where != nil {
		toks, ok := exprTokens(*ci.where)
		e := &exprEnv{s: s, t: t, src: []byte(*ci.where)}
		var r typed
		if ok {
			r, ok = e.prove(toks)
		}
		if !ok || r.vol != 'i' || r.typ != "bool" {
			s.unproven(t, fmt.Sprintf("%s: the index predicate %s is not proven IMMUTABLE and boolean%s", what, *ci.where, precondition("M4.P5")))
		}
	}
}

// simpleQueryProven proves the query forms R1b's reader covers (Revision
// 1, B2 "a reader for single-relation queries"): SELECT of a target list
// whose items are *, bare columns or constants (number, string, TRUE,
// FALSE, NULL), each optionally AS name, optionally INTO a target, and
// either no FROM (constants only) or FROM one table the pass holds with
// known facts under a certain name; nothing may follow but CREATE TABLE
// AS's or a materialized view's WITH [NO] DATA. Every column must exist
// (42703 otherwise) and the output names must be distinct (42701 for a
// view or a table; a constant without a name is ?column?).
func (s *state) simpleQueryProven(toks []token) bool {
	at := -1
	for j, t := range toks {
		if t.kind == tokIdent && t.value == "select" {
			at = j
			break
		}
		if t.kind == tokPunct && t.value == "(" && j > 0 && toks[j-1].kind == tokIdent && toks[j-1].value == "as" {
			return false
		}
	}
	if at < 0 {
		return false
	}
	// A parenthesised SELECT INTO statement: as many closing parentheses
	// at the end as opening ones before SELECT.
	open := 0
	for open < at && toks[open].kind == tokPunct && toks[open].value == "(" {
		open++
	}
	if open > 0 && open == at {
		for k := 1; k <= open; k++ {
			if len(toks)-k <= at || toks[len(toks)-k].kind != tokPunct || toks[len(toks)-k].value != ")" {
				return false
			}
		}
		toks = toks[:len(toks)-open]
	}
	c := &cursor{toks: toks, i: at + 1, src: s.m.source}
	star, aggregate := false, false
	var cols, outputs []string
	for {
		t := c.tok()
		switch {
		case c.done():
			return false
		case t.kind == tokOp && t.value == "*":
			star = true
			c.i++
		case t.kind == tokNumber || t.kind == tokString && t.start < len(c.src) && c.src[t.start] == '\'' ||
			t.kind == tokIdent && (t.value == "true" || t.value == "false" || t.value == "null"):
			c.i++
			// FigureColname names an unnamed number or string constant
			// ?column?; TRUE, FALSE and NULL need a name here.
			name := "?column?"
			if t.kind == tokIdent && !c.isKw(0, "as") {
				return false
			}
			if c.acceptKw("as") {
				n, err := c.ident()
				if err != nil {
					return false
				}
				name = n
			}
			outputs = append(outputs, name)
		case t.kind == tokIdent && t.value == "count" && c.i+3 < len(toks) && toks[c.i+1].value == "(" && toks[c.i+2].value == "*" && toks[c.i+3].value == ")":
			// count(*): an aggregate over the rows, named count.
			c.i += 4
			name := "count"
			if c.acceptKw("as") {
				n, err := c.ident()
				if err != nil {
					return false
				}
				name = n
			}
			outputs = append(outputs, name)
			aggregate = true
		case t.kind == tokIdent && !sqlKeywords[t.value] || t.kind == tokQuoted:
			c.i++
			name := t.value
			cols = append(cols, name)
			if c.acceptKw("as") {
				n, err := c.ident()
				if err != nil {
					return false
				}
				name = n
			}
			outputs = append(outputs, name)
		default:
			return false
		}
		if !c.acceptPunct(",") {
			break
		}
	}
	if c.acceptKw("into") {
		c.acceptKw("table")
		if _, err := c.name(); err != nil {
			return false
		}
	}
	var t *table
	if c.acceptKw("from") {
		q, err := c.name()
		if err != nil {
			return false
		}
		exact, others := s.lookup(q)
		if exact == nil || len(others) > 0 || q.schema == nil && (s.pathChanged || s.temps[q.name]) || !factsKnown(exact) {
			return false
		}
		t = exact
	} else if star || len(cols) > 0 {
		return false
	}
	c.acceptKw("with", "no", "data")
	c.acceptKw("with", "data")
	if !c.done() {
		return false
	}
	if aggregate && (star || len(cols) > 0) {
		// A column next to an aggregate needs GROUP BY.
		return false
	}
	for _, n := range cols {
		if t.column(n) == nil {
			return false
		}
	}
	if star {
		return t != nil && len(outputs) == 0
	}
	seen := map[string]bool{}
	for _, n := range outputs {
		if seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// inheritProven proves ALTER TABLE t INHERIT p (M5k-b with P4; tablecmds.c
// ATExecAddInherit, MergeAttributesIntoExisting and
// MergeConstraintsIntoExisting): p is a table the pass holds with known
// facts under a certain name, neither table is partitioned, a partition or
// typed, t does not already inherit from p, p has no CHECK constraint (the
// child would need a matching one), and every column of p is in t with the
// same type and collation, NOT NULL when p's is, and neither generated.
func (s *state) inheritProven(t *table, pq qname) bool {
	p, others := s.lookup(pq)
	if p == nil || p == t || len(others) > 0 || pq.schema == nil && (s.pathChanged || s.temps[pq.name]) || !factsKnown(p) {
		return false
	}
	if p.partitionKey != nil || t.partitionKey != nil || p.partitionOf != nil || t.partitionOf != nil || p.ofType != nil || t.ofType != nil || p.hasChecks {
		return false
	}
	if slices.ContainsFunc(p.children, func(q qname) bool { return q.String() == t.q.String() }) {
		return false
	}
	for _, pc := range p.columns {
		cc := t.column(pc.name)
		if cc == nil || !sameType(writtenType(pc), writtenType(cc)) || pc.generated != nil || cc.generated != nil {
			return false
		}
		if (pc.collation == nil) != (cc.collation == nil) || pc.collation != nil && *pc.collation != *cc.collation {
			return false
		}
		if !pc.nullable && cc.nullable {
			return false
		}
	}
	return true
}

// noInheritProven proves ALTER TABLE t NO INHERIT p: the pass holds p
// with known facts and records t as its child (42P01 "is not a parent"
// otherwise). The child is forgotten.
func (s *state) noInheritProven(t *table, pq qname) bool {
	p, others := s.lookup(pq)
	if p == nil || len(others) > 0 || pq.schema == nil && (s.pathChanged || s.temps[pq.name]) || !factsKnown(p) {
		return false
	}
	i := slices.IndexFunc(p.children, func(q qname) bool { return q.String() == t.q.String() })
	if i < 0 {
		return false
	}
	p.children = slices.Delete(p.children, i, i+1)
	return true
}

// sqlConstantBody proves the one LANGUAGE sql body form R1b covers: a
// routine without arguments whose body is one SELECT of a constant
// expression the closed tables prove, for a procedure, or for a function
// whose RETURNS type is the expression's result type (text for an untyped
// string constant; check_sql_fn_retval).
func (s *state) sqlConstantBody(toks []token) bool {
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	c.acceptKw("or", "replace")
	procedure := c.acceptKw("procedure")
	if !procedure && !c.acceptKw("function") {
		return false
	}
	if _, err := c.name(); err != nil {
		return false
	}
	if !c.acceptPunct("(") || !c.acceptPunct(")") {
		return false
	}
	returns := ""
	var body *string
	for !c.done() {
		switch {
		case c.acceptKw("returns"):
			start := c.i
			if err := columnType(c); err != nil {
				return false
			}
			returns = c.text(start, c.i)
		case c.acceptKw("language"):
			c.i++
		case c.acceptKw("immutable"), c.acceptKw("stable"), c.acceptKw("volatile"), c.acceptKw("strict"), c.acceptKw("leakproof"),
			c.acceptKw("security", "definer"), c.acceptKw("security", "invoker"):
		case c.acceptKw("parallel"):
			c.i++
		case c.acceptKw("as"):
			if c.done() || c.tok().kind != tokString || body != nil {
				return false
			}
			v := c.tok().value
			body = &v
			c.i++
		default:
			return false
		}
	}
	if body == nil || !procedure && returns == "" {
		return false
	}
	stmts, fail := lexStatements([]byte(*body))
	if fail != nil || len(stmts) != 1 {
		return false
	}
	b := stmts[0].tokens
	if len(b) < 2 || b[0].kind != tokIdent || b[0].value != "select" {
		return false
	}
	e := &exprEnv{s: s, src: []byte(*body)}
	r, ok := e.prove(b[1:])
	if !ok || r.any || r.null {
		return false
	}
	if procedure {
		return true
	}
	base, ok := normalType(returns)
	if !ok {
		return false
	}
	if r.lit != nil {
		// An untyped constant in the output list resolves to text.
		return base == "text"
	}
	return r.typ == base
}

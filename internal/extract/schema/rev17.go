package schema

import (
	"fmt"
	"strings"
)

// T018a2 Revision 1.7 (2026-11-07, after the third independent re-check):
// class rules W1-W7. W1 lives in objectkinds.go (per-table names), W3 in
// recordRoutine, W6 in rowsChecked and W7 in the enum reader; this file
// holds W2 (string arguments of reg* parameters), W4 (databases) and W5
// (drops of N1 and R5 kinds not held).

// regTypes are the reg* types of REL_17_6 pg_type.dat: their input
// functions look a name up when the value is read (regproc.c), so a
// string literal bound to one is resolved when the call is analysed.
var regTypes = map[string]bool{
	"regclass": true, "regcollation": true, "regconfig": true, "regdictionary": true, "regnamespace": true,
	"regoper": true, "regoperator": true, "regproc": true, "regprocedure": true, "regrole": true, "regtype": true,
}

// regArguments applies W2 (O1.P1) to one statement before it is analysed:
// a string literal bound to a reg* parameter of a set-defined routine is
// proven only when it is a regclass argument naming a relation the pass
// holds under a certain name. Postgres coerces the literal when the call
// is analysed (regclassin, regproc.c 874-940), under the session's
// search_path; E1 does not cover a name looked up from a string.
func (s *state) regArguments(toks []token, src []byte) {
	if s.failure != nil || len(s.routineDefs) == 0 && len(s.code.routines) == 0 {
		return
	}
	for i := 0; i+1 < len(toks); i++ {
		t := toks[i]
		if t.kind != tokIdent && t.kind != tokQuoted || toks[i+1].kind != tokPunct || toks[i+1].value != "(" {
			continue
		}
		if !s.setRoutine(t.value) || callIsHeader(toks, i) {
			continue
		}
		c := &cursor{toks: toks, i: i + 1, src: src}
		end, err := c.group()
		if err != nil {
			return
		}
		for pos, r := range c.splitList(i+2, end-1) {
			arg := toks[r[0]:r[1]]
			named := ""
			if len(arg) >= 3 && (arg[0].kind == tokIdent || arg[0].kind == tokQuoted) && arg[1].kind == tokOp && (arg[1].value == "=>" || arg[1].value == ":=") {
				named, arg = arg[0].value, arg[2:]
			}
			if len(arg) != 1 || arg[0].kind != tokString {
				continue
			}
			typ, bound := s.regBound(t.value, pos, named)
			if !bound {
				continue
			}
			v, _ := literalValue(arg[0], src)
			if typ == "regclass" {
				if q, ok := regclassName(arg[0], src); ok && s.regclassHeld(q) {
					continue
				}
				s.unknownFact(fmt.Sprintf("the call of %s passes '%s' to a regclass parameter: it is looked up when the call is analysed, and it is not a relation the pass holds under a certain name (E1 does not cover a name read from a string)%s", t.value, v, precondition("O1.P1")))
				return
			}
			what := "a " + typ + " parameter"
			if typ == "" {
				what = "a parameter that may be of a reg* type"
			}
			s.unknownFact(fmt.Sprintf("the call of %s passes '%s' to %s: it is looked up when the call is analysed, which the pass does not prove%s", t.value, v, what, precondition("O1.P1")))
			return
		}
	}
}

// setRoutine reports whether name is a routine the set defined (top level
// or by code).
func (s *state) setRoutine(name string) bool {
	return len(s.routineDefs[name]) > 0 || s.routineUnknown[name] || s.code.routines[name]
}

// callIsHeader reports whether the name at i is the name of a routine
// header or a trigger's function, not a call: after FUNCTION, PROCEDURE,
// ROUTINE or AGGREGATE (CREATE, ALTER, DROP, COMMENT ON, GRANT ON, and
// CREATE TRIGGER ... EXECUTE FUNCTION, whose string arguments are
// TG_ARGV, not parameters).
func callIsHeader(toks []token, i int) bool {
	j := i - 1
	if j >= 1 && toks[j].kind == tokPunct && toks[j].value == "." {
		j -= 2
	}
	if j < 0 || toks[j].kind != tokIdent {
		return false
	}
	switch toks[j].value {
	case "function", "procedure", "routine", "aggregate":
		return true
	}
	return false
}

// regBound reports whether the argument at pos (or named) of a call of the
// set routine name may be bound to a reg* parameter, and the reg* type
// when it is known ("" when the parameter's type is not known: a header
// the pass did not read, a routine code defined, or a type outside
// pg_catalog that may be a domain over a reg* type).
func (s *state) regBound(name string, pos int, named string) (string, bool) {
	if s.routineUnknown[name] || s.code.routines[name] || len(s.routineDefs[name]) == 0 {
		return "", true
	}
	found, typ := false, ""
	for _, d := range s.routineDefs[name] {
		if !d.paramsRead {
			return "", true
		}
		at := pos
		if named != "" {
			at = -1
			for k, n := range d.inNames {
				if n == named {
					at = k
				}
			}
		}
		var p string
		switch {
		case at >= 0 && at < len(d.inputs):
			p = d.inputs[at]
		case at >= len(d.inputs) && len(d.inputs) > 0 && strings.HasSuffix(d.inputs[len(d.inputs)-1], "[]"):
			// A VARIADIC array parameter may take the extra arguments.
			p = d.inputs[len(d.inputs)-1]
		default:
			continue
		}
		base := strings.TrimSuffix(p, "[]")
		switch {
		case regTypes[base] && base == p:
			if found && typ != base {
				return "", true
			}
			found, typ = true, base
		case regTypes[base], !isCatalogType(base):
			return "", true
		}
	}
	return typ, found
}

// regclassHeld reports whether q names a relation the pass holds under a
// certain name: a known table, view, materialized view or sequence, with
// the search_path known for an unqualified name (P13, V6).
func (s *state) regclassHeld(q qname) bool {
	if s.code.gone[q.name] || q.schema == nil && s.pathChanged {
		return false
	}
	exact, others := s.lookup(q)
	if exact != nil && len(others) == 0 {
		return true
	}
	if exact != nil || len(others) > 0 {
		return false
	}
	for k, r := range s.relations {
		if r.uncertain || r.kind != "view" && r.kind != "materialized view" && r.kind != "sequence" {
			continue
		}
		if q.schema != nil && k == *q.schema+"."+q.name || q.schema == nil && k == q.name {
			return true
		}
	}
	return false
}

// rev17Objects applies W4 and W5 to one statement, before the statement
// changes what the pass holds.
func (s *state) rev17Objects(toks []token) {
	if s.failure != nil || len(toks) == 0 {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	databaseMay := func(name string) {
		s.unknownFact(fmt.Sprintf("the statement needs database %s, which may not exist: E1 does not cover databases, and the name of the database the set runs in is unknown%s", name, precondition("N1.P1")))
	}
	switch {
	case c.acceptKw("alter", "database"):
		// W4: every ALTER DATABASE form names the database (gram.y
		// AlterDatabaseStmt, AlterDatabaseSetStmt: a name, no
		// CURRENT_DATABASE form in REL_17_6).
		if n, err := c.ident(); err == nil {
			databaseMay(n)
		}
	case c.peekKw("alter", "role"), c.peekKw("alter", "user"):
		if in := depth0(toks, 2, "in"); in >= 0 && (&cursor{toks: toks, i: in + 1}).peekKw("database") {
			c.i = in + 2
			if n, err := c.ident(); err == nil {
				databaseMay(n)
			}
		}
	case c.peekKw("grant"), c.peekKw("revoke"):
		if on := depth0(toks, 1, "on"); on >= 0 && (&cursor{toks: toks, i: on + 1}).peekKw("database") {
			c.i = on + 2
			if n, err := c.ident(); err == nil {
				databaseMay(n)
			}
		}
	case c.acceptKw("drop"):
		s.dropNotHeld(c)
	case c.acceptKw("create", "collation"), c.acceptKw("create", "publication"):
		// A created name is held again (N1.P2 records it).
		c.acceptKw("if", "not", "exists")
		if q, err := c.name(); err == nil && s.rev17.dropped != nil {
			delete(s.rev17.dropped, n1Key(toks[1].value, "", q.String()))
		}
	}
}

// rev17State is the state of the Revision 1.7 rules.
type rev17State struct {
	// dropped are the publication and collation names (n1Key) a DROP
	// named since their last CREATE (W5).
	dropped map[string]bool
}

// w5Kinds are the kinds W5 governs, with their precondition (the row of
// their DROP) and whether the N1.P2 record can prove a name held.
var w5Kinds = []struct {
	words    []string
	kind, id string
	recorded bool
}{
	{[]string{"statistics"}, "statistics object", "N1.P1", false},
	{[]string{"publication"}, "publication", "N1.P1", true},
	{[]string{"operator"}, "operator", "N1.P1", false},
	{[]string{"cast"}, "cast", "N1.P1", false},
	{[]string{"collation"}, "collation", "R5.P1", true},
	{[]string{"aggregate"}, "aggregate", "R5.P1", false},
}

// dropNotHeld applies W5 (V4's "name not held -> may fail") to DROP
// STATISTICS, PUBLICATION, OPERATOR, CAST, COLLATION and AGGREGATE without
// IF EXISTS: a publication or collation is held when the set's record
// (N1.P2) holds the written name and no unread statement may have renamed
// one of its kind; statistics objects are not recorded, and operators,
// aggregates and casts are identified by argument types the record does
// not keep, so their drop may fail.
func (s *state) dropNotHeld(c *cursor) {
	for _, k := range w5Kinds {
		if !c.acceptKw(k.words...) {
			continue
		}
		if c.peekKw("if", "exists") || k.words[0] == "operator" && (c.peekKw("class") || c.peekKw("family")) {
			return
		}
		what := "DROP " + strings.ToUpper(k.words[0])
		if !k.recorded {
			s.unknownFact(fmt.Sprintf("%s may be refused: the pass does not hold the %s it names, and E1 does not cover objects of that kind%s", what, k.kind, precondition(k.id)))
			return
		}
		names := dropNames(c)
		defer func() {
			// A dropped name is no longer held, whether the drop was read
			// by N1.P2's record or not (the record keeps unread drops).
			if s.rev17.dropped == nil {
				s.rev17.dropped = map[string]bool{}
			}
			for _, q := range names {
				s.rev17.dropped[n1Key(k.words[0], "", q.String())] = true
			}
		}()
		for _, q := range names {
			key := n1Key(k.words[0], "", q.String())
			// A collation is schema-qualified: after a search_path change
			// an unqualified name may resolve elsewhere (P13).
			pathOK := k.words[0] != "collation" || q.schema != nil || !s.pathChanged
			if s.n1.names[key] && !s.n1.renamed[k.words[0]] && !s.rev17.dropped[key] && pathOK {
				continue
			}
			s.unknownFact(fmt.Sprintf("%s %s may be refused: %s is not a %s the pass holds, and E1 does not cover objects of that kind%s", what, q, q, k.kind, precondition(k.id)))
			return
		}
		return
	}
}

// familySetRows reports whether t or one of its known inheritance children
// or partitions, recursively, may hold rows the set wrote (W6: a statement
// that checks t's rows checks its descendants' too, tablecmds.c recursion).
func (s *state) familySetRows(t *table) bool {
	seen := map[*table]bool{}
	var walk func(x *table) bool
	walk = func(x *table) bool {
		if x == nil || seen[x] {
			return false
		}
		seen[x] = true
		if x.setRows && !x.empty {
			return true
		}
		for _, y := range s.tables {
			if y.partitionOf == x && walk(y) {
				return true
			}
		}
		for _, ch := range x.children {
			for _, y := range s.tables {
				if y.q.name == ch.name && walk(y) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
}

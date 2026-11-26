package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Names the set creates (T018a2 Revision 1, class P1). Environment
// assumption E1 lets a name the set never creates stand for an object
// outside the set. A name the set creates anywhere (later: K1; earlier and
// then dropped or renamed away; or a generated-name shape of a set table)
// is not outside: a statement that needs such an object has it only when
// the pass holds it at that point, otherwise Postgres refuses the
// statement. The pass reports a refusal it cannot rule out as a possible
// failure of the migration (applied: null), the same outcome it gives a
// certain refusal (WF8).

// nameKind is the namespace a name lives in, as a bit set.
type nameKind uint8

const (
	// kindRelation is pg_class without indexes: tables, sequences, views,
	// materialized views, composite types, foreign tables.
	kindRelation nameKind = 1 << iota
	// kindIndex is an index of pg_class (including constraint indexes):
	// it shares the namespace with kindRelation (P2) but is another kind
	// for the statements that need one or the other (P1).
	kindIndex
	// kindType is pg_type: types, domains, and row types of tables, views
	// and composite types.
	kindType
	kindFunction
	kindSchema
	// kindConstraint is a CHECK, FOREIGN KEY or NOT NULL constraint name
	// (CONSTRAINT n): not a relation, but a name the set creates, so E1
	// does not cover a relation of that name either (P1, Revision 1.4,
	// g6-s69809). It is outside kindAny: only statements that need a
	// relation or index consult it.
	kindConstraint
	kindAny = kindRelation | kindIndex | kindType | kindFunction | kindSchema
	// kindClass is the whole pg_class namespace.
	kindClass = kindRelation | kindIndex
)

// kindOf maps a recorded relation kind to its namespaces.
func kindOf(kind string) nameKind {
	var k nameKind
	switch {
	case kind == "index":
		k |= kindIndex
	case relationKinds[kind]:
		k |= kindRelation
	}
	if rowTypeKinds[kind] {
		k |= kindType
	}
	if k == 0 {
		k = kindType
	}
	return k
}

// nameSet is the set's names: every name some statement of the set creates
// (by bare name; schemas are compared separately), and the names of the
// set's tables for generated-name shapes.
type nameSet struct {
	created map[string]nameKind
	tables  map[string]bool
}

func newNameSet() *nameSet {
	return &nameSet{created: map[string]nameKind{}, tables: map[string]bool{}}
}

// nameEvent is one name a statement creates or drops.
type nameEvent struct {
	name string
	kind nameKind
	// table marks a table name (its generated names have its shape).
	table bool
	// ledger marks a name the model does not record (functions, schemas,
	// temporary views and sequences): the ledger holds it.
	ledger bool
}

// statementNames is what one statement does to names: the analysis's
// creations, renames and drops, plus the token patterns the analysis does
// not record (schemas, temporary views and sequences, function drops,
// index-backed constraint names).
type statementNames struct {
	creates []nameEvent
	drops   []nameEvent
}

// namesOf reads the name effects of one analysed statement.
func namesOf(a analysis, toks []token) statementNames {
	var out statementNames
	add := func(name string, kind nameKind, table bool) {
		out.creates = append(out.creates, nameEvent{name: name, kind: kind, table: table})
	}
	addLedger := func(name string, kind nameKind) {
		out.creates = append(out.creates, nameEvent{name: name, kind: kind, ledger: true})
	}
	if a.create != nil {
		add(a.create.name.name, kindRelation|kindType, true)
	}
	if a.temp != nil {
		add(a.temp.name, kindRelation|kindType, true)
	}
	if a.index != nil && a.index.name != nil {
		add(*a.index.name, kindIndex, false)
	}
	for _, o := range a.relations {
		add(o.q.name, kindOf(o.kind), o.kind == "table")
	}
	if a.function != nil {
		addLedger(*a.function, kindFunction)
	}
	if a.renamed != nil && a.renamed.newName != "" {
		add(a.renamed.newName, kindOf(a.renamed.kind), false)
	}
	if a.alterIndex != nil && a.alterIndex.newName != nil {
		// ALTER INDEX accepts any relation: the new name may be a table's.
		add(*a.alterIndex.newName, kindClass|kindType, true)
	}
	if a.alter != nil {
		for _, act := range a.alter.actions {
			if act.kind == "rename_table" {
				add(act.newName, kindRelation|kindType, true)
			}
		}
	}
	if a.schemaRename != nil {
		addLedger(a.schemaRename[1], kindSchema)
		out.drops = append(out.drops, nameEvent{name: a.schemaRename[0], kind: kindSchema})
	}
	if a.drop != nil && a.drop.kind == "schema" {
		for _, q := range a.drop.names {
			out.drops = append(out.drops, nameEvent{name: q.name, kind: kindSchema})
		}
	}
	word := func(i int, w string) bool {
		return i < len(toks) && toks[i].kind == tokIdent && toks[i].value == w
	}
	nameAt := func(i int) (string, bool) {
		// The last part of a possibly qualified name starting at i.
		if i >= len(toks) || toks[i].kind != tokIdent && toks[i].kind != tokQuoted {
			return "", false
		}
		n := toks[i].value
		for i+2 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "." && (toks[i+2].kind == tokIdent || toks[i+2].kind == tokQuoted) {
			i += 2
			n = toks[i].value
		}
		return n, true
	}
	switch {
	case word(0, "create"):
		i := 1
		if word(i, "or") && word(i+1, "replace") {
			i += 2
		}
		temp := false
		for word(i, "temp") || word(i, "temporary") || word(i, "local") || word(i, "global") || word(i, "recursive") {
			temp = temp || word(i, "temp") || word(i, "temporary")
			i++
		}
		switch {
		case word(i, "schema"):
			i++
			if word(i, "if") && word(i+1, "not") && word(i+2, "exists") {
				i += 3
			}
			switch {
			case !word(i, "authorization"):
				if n, ok := nameAt(i); ok {
					addLedger(n, kindSchema)
				}
			case !word(i+1, "current_user") && !word(i+1, "session_user") && !word(i+1, "current_role"):
				// X4 (Revision 1.8): CREATE SCHEMA AUTHORIZATION r without
				// a schema name creates the schema r (schemacmds.c
				// CreateSchemaCommand takes the role's name).
				if n, ok := nameAt(i + 1); ok {
					addLedger(n, kindSchema)
				}
			}
		case temp && (word(i, "view") || word(i, "sequence")):
			i++
			if word(i, "if") && word(i+1, "not") && word(i+2, "exists") {
				i += 3
			}
			if n, ok := nameAt(i); ok {
				addLedger(n, kindRelation|kindType)
			}
		}
	case word(0, "alter") && (word(1, "function") || word(1, "procedure") || word(1, "routine")) && a.function != nil:
		// RENAME TO: the old name is no longer held (another overload of
		// it may be; the ledger does not count overloads, which only makes
		// a later use of the old name one that may fail).
		if n, ok := nameAt(2); ok {
			out.drops = append(out.drops, nameEvent{name: n, kind: kindFunction})
		}
	case word(0, "drop") && (word(1, "function") || word(1, "procedure") || word(1, "routine")):
		for _, q := range dropNames(&cursor{toks: toks, i: 2}) {
			out.drops = append(out.drops, nameEvent{name: q.name, kind: kindFunction})
		}
	}
	// CONSTRAINT n PRIMARY KEY / UNIQUE / EXCLUDE creates an index of that
	// name (CREATE TABLE, ALTER TABLE ADD); RENAME CONSTRAINT a TO b renames
	// it when a is such a constraint.
	if a.create != nil || a.alter != nil {
		for i := range toks {
			if word(i, "constraint") && (word(i+2, "primary") || word(i+2, "unique") || word(i+2, "exclude")) {
				if n, ok := nameAt(i + 1); ok {
					add(n, kindIndex, false)
				}
			} else if word(i, "constraint") && (word(i+2, "check") || word(i+2, "foreign") || word(i+2, "not")) {
				if n, ok := nameAt(i + 1); ok {
					add(n, kindConstraint, false)
				}
			}
			if word(i, "rename") && word(i+1, "constraint") && word(i+3, "to") {
				if n, ok := nameAt(i + 4); ok {
					add(n, kindIndex, false)
				}
			}
		}
	}
	return out
}

// collect adds a statement's creations to the set's names.
func (ns *nameSet) collect(sn statementNames) {
	for _, e := range sn.creates {
		ns.created[e.name] |= e.kind
		if e.table {
			ns.tables[e.name] = true
		}
	}
}

// creates reports whether the set creates name in one of kinds.
func (ns *nameSet) creates(name string, kinds nameKind) bool {
	return ns != nil && ns.created[name]&kinds != 0
}

// shapedBy returns the set table whose generated-name shape name has
// (<t>_pkey, or <t>_..._<suffix> with optional collision digits), or "".
func (ns *nameSet) shapedBy(name string) string {
	if ns == nil {
		return ""
	}
	base := strings.TrimRight(name, "0123456789")
	var tables []string
	for t := range ns.tables {
		tables = append(tables, t)
	}
	slices.Sort(tables)
	for _, t := range tables {
		for _, suf := range append(slices.Clone(relationSuffixes), constraintSuffixes...) {
			head, ok := strings.CutSuffix(base, "_"+suf)
			if !ok {
				continue
			}
			if suf == "pkey" && head == t || strings.HasPrefix(head, t+"_") && len(head) > len(t)+1 {
				return t
			}
		}
	}
	return ""
}

// ledger holds the names the pass cannot read from its model: functions,
// schemas and temporary views and sequences, as the statements so far
// create and drop them.
type ledger map[string]nameKind

func (l ledger) apply(sn statementNames) {
	for _, e := range sn.drops {
		l[e.name] &^= e.kind
	}
	for _, e := range sn.creates {
		if e.ledger {
			l[e.name] |= e.kind
		}
	}
}

// holds reports whether the pass holds an object named name in one of
// kinds at this point: a known table, a recorded relation or type, a
// written or generated index or key name, a temporary table, or a ledger
// entry.
func (s *state) holds(name string, kinds nameKind) bool {
	if s.code.gone[name] || s.rev18.gone[name] != "" {
		// P12: code may have dropped, renamed or moved it; X1, X2: a
		// CASCADE may have removed it, or a collation was renamed away.
		return false
	}
	if s.ledger[name]&kinds != 0 {
		return true
	}
	if kinds&(kindRelation|kindType) != 0 {
		for _, t := range s.tables {
			if t.q.name == name && !t.kindUnknown {
				return true
			}
		}
		if s.temps[name] {
			return true
		}
	}
	for k, r := range s.relations {
		if (k == name || strings.HasSuffix(k, "."+name)) && kindOf(r.kind)&kinds != 0 {
			return true
		}
	}
	for _, t := range s.tables {
		if kinds&kindIndex != 0 {
			for _, ix := range t.indexes {
				if ix.name != nil && *ix.name == name {
					return true
				}
			}
			for _, k := range append([]*key{t.primaryKey}, t.uniques...) {
				if k != nil && k.name != nil && *k.name == name {
					return true
				}
			}
			for _, c := range t.checks {
				// An EXCLUDE constraint has an index of its name; a CHECK
				// constraint has none.
				if c.exclude && c.name == name {
					return true
				}
			}
			if slices.Contains(generatedKeyNames(t), name) {
				return true
			}
			for _, ix := range t.indexes {
				if ix.name == nil && !slices.Contains(ix.columns, nil) && orGenerated(ix.gen, t.q.name+"_"+strings.Join(names(ix.columns), "_")+"_idx") == name {
					return true
				}
			}
		}
		if kinds&kindRelation != 0 {
			for _, c := range t.columns {
				if (c.implied || c.identity != nil) && seqName(t, c) == name {
					return true
				}
			}
		}
	}
	return false
}

// needs applies P1 to a name a statement needs in one of kinds: when the
// set creates the name (or, with shapes, the name has the generated-name
// shape of a set table) and the pass does not hold it, Postgres refuses
// the statement unless an object the pass does not see holds the name, so
// the migration may fail. It reports whether it did so. id is the
// precondition the statement's row carries.
func (s *state) needs(q qname, kinds nameKind, shapes bool, what, id string) bool {
	if s.failure != nil {
		return false
	}
	if q.schema != nil && s.names.creates(*q.schema, kindSchema) && !s.holds(*q.schema, kindSchema) {
		s.unknownFact(fmt.Sprintf("%s: schema %s is created by the set but does not exist at this point (created later, or dropped or renamed earlier); Postgres refuses a missing schema%s", what, *q.schema, precondition(id)))
		return true
	}
	if s.holds(q.name, kinds) {
		return false
	}
	for _, t := range s.tables {
		if t.q.name == q.name && t.kindUnknown && kinds&kindRelation != 0 {
			// P3 left the kind of this name unknown.
			s.unknownFact(fmt.Sprintf("%s: %s may be a relation of another kind (an earlier CREATE TABLE IF NOT EXISTS may have been skipped); Postgres refuses a relation of the wrong kind%s", what, q, precondition(id)))
			return true
		}
	}
	if other := kindClass &^ kinds; kinds&kindClass != 0 && other != 0 && kinds&other == 0 && s.holds(q.name, other) {
		// The name is held in pg_class, but as the other kind (an index
		// where a table is needed, or the reverse): Postgres refuses it.
		what2 := "an index"
		if other == kindRelation {
			what2 = "a table, view or sequence"
		}
		s.unknownFact(fmt.Sprintf("%s: %s is %s here, not the kind of relation the statement needs; Postgres refuses it%s", what, q, what2, precondition(id)))
		return true
	}
	// E1 covers only names the set never creates, whatever the kind; a
	// constraint name counts for a relation or index (Revision 1.4).
	created := kindAny
	if kinds&kindClass != 0 {
		created |= kindConstraint
	}
	if s.names.creates(q.name, created) {
		s.unknownFact(fmt.Sprintf("%s: %s is a name the set creates, but no such object exists at this point (it is created later, or was dropped or renamed earlier); Postgres refuses a missing object%s", what, q, precondition(id)))
		return true
	}
	if shapes && kinds&kindClass != 0 {
		if t := s.names.shapedBy(q.name); t != "" {
			s.unknownFact(fmt.Sprintf("%s: %s has the shape of a generated name of the set's table %s, but no such object is known at this point; Postgres refuses a missing object%s", what, q, t, precondition(id)))
			return true
		}
	}
	return false
}

// needsNames applies P1 to identifiers of an expression or type that may
// name any object (functions, types, sequences; columns never match a name
// the set creates unless they share it, which only makes the check
// conservative).
func (s *state) needsNames(names []string, what, id string) {
	for _, n := range names {
		if s.needs(qname{name: n}, kindAny, false, what, id) {
			return
		}
	}
}

// tokenRuleSkips are statements whose identifiers do not need objects to
// exist: settings, transaction control, notifications and savepoints.
var tokenRuleSkips = map[string]bool{
	"set": true, "reset": true, "show": true, "discard": true, "begin": true, "start": true, "commit": true,
	"end": true, "rollback": true, "abort": true, "savepoint": true, "release": true, "prepare": true,
	"notify": true, "listen": true, "unlisten": true, "checkpoint": true, "deallocate": true,
}

// tokenRule applies P1's token rule to a statement the pass does not parse
// name by name (N1, N3, R1, R1b, R4, R5, R6, O1): an identifier equal to a
// name the set creates but does not hold at this point makes the
// statement one that may fail. Names the statement itself creates, and the
// names of DROP ... IF EXISTS, are not needed. A LANGUAGE sql body is
// validated at creation (check_function_bodies, E4), so its identifiers
// count too.
func (s *state) tokenRule(st statement, sn statementNames) {
	toks := st.tokens
	if s.failure != nil || len(toks) == 0 || tokenRuleSkips[toks[0].value] && toks[0].kind == tokIdent {
		return
	}
	if toks[0].kind == tokIdent && toks[0].value == "drop" && containsKw(&cursor{toks: toks}, "exists") {
		return
	}
	own := map[string]bool{}
	for _, e := range sn.creates {
		own[e.name] = true
	}
	if c := (&cursor{toks: toks}); c.acceptKw("create", "role") || c.acceptKw("create", "user") || c.acceptKw("create", "group") {
		// X4 (Revision 1.8): the role a statement creates is its own,
		// whatever a schema of the set is named (CREATE SCHEMA
		// AUTHORIZATION r).
		if !c.done() {
			own[c.tok().value] = true
		}
	}
	check := func(toks []token) bool {
		for i, t := range toks {
			if t.kind == tokString && i > 0 && toks[i-1].kind == tokPunct && (toks[i-1].value == "(" || toks[i-1].value == ",") {
				// B2 (g5-s80): a string argument may be coerced to a
				// relation by regclassin (a reg* parameter of a set-defined
				// function), so a relation name it spells follows P1.
				if q, ok := regclassName(t, s.m.source); ok && !own[q.name] && s.needs(q, kindRelation, false, "the statement passes the relation name "+q.String(), "O1.P1") {
					return true
				}
				continue
			}
			if t.kind != tokIdent && t.kind != tokQuoted || own[t.value] {
				continue
			}
			if t.kind == tokIdent && sqlKeywords[t.value] && !namePosition(toks, i) {
				// An unquoted keyword is syntax unless it stands where a
				// relation, type or function name does.
				continue
			}
			// A name after INDEX (COMMENT ON INDEX, REINDEX INDEX) must be an
			// index, one after FROM, TABLE, ON and the like a table, view or
			// sequence; both may be generated names of a set table.
			kinds, shapes := kindAny, false
			qualifier := i+1 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "."
			if !qualifier && i > 0 && toks[i-1].kind == tokIdent && relationIntroducers[toks[i-1].value] {
				kinds, shapes = kindRelation|kindType, true
				if toks[i-1].value == "index" {
					kinds = kindIndex
				}
			}
			if s.needs(qname{name: t.value}, kinds, shapes, "the statement names "+t.value, "N1.P1") {
				return true
			}
		}
		return false
	}
	if check(toks) {
		return
	}
	if sqlBody(toks) {
		for _, t := range toks {
			if t.kind != tokString {
				continue
			}
			body, _ := lexStatements([]byte(t.value))
			for _, b := range body {
				if check(b.tokens) {
					return
				}
			}
		}
	}
}

// sqlBody reports CREATE FUNCTION/PROCEDURE ... LANGUAGE sql.
func sqlBody(toks []token) bool {
	c := &cursor{toks: toks}
	if !c.peekKw("create") {
		return false
	}
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].value == "language" && (toks[i+1].kind == tokIdent || toks[i+1].kind == tokQuoted || toks[i+1].kind == tokString) && strings.EqualFold(toks[i+1].value, "sql") {
			return true
		}
	}
	return false
}

// exactNames reports the statements whose names the state handlers check
// one by one (M rows); the others get the token rule.
func exactNames(a analysis) bool {
	return a.create != nil || a.index != nil || a.alter != nil || a.drop != nil || a.alterIndex != nil || a.schemaRename != nil
}

// needsColumn applies P1 to a column definition: its type and the objects
// its DEFAULT, CHECK and generation expressions name.
func (s *state) needsColumn(def columnDef, what, id string) {
	for _, n := range def.typeNames {
		if s.needs(qname{name: n}, kindType, false, what, id) {
			return
		}
	}
	s.needsNames(def.exprNames, what, id)
	for _, ch := range def.checks {
		s.needsNames(ch.names, what, id)
	}
}

// relationIntroducers are the words after which a name is a relation the
// statement needs (P1's token rule checks its kind and generated shapes).
var relationIntroducers = map[string]bool{
	"from": true, "join": true, "into": true, "update": true, "table": true, "only": true, "on": true,
	"truncate": true, "lock": true, "view": true, "index": true, "like": true,
}

// nameIntroducers are the words after which an unquoted keyword is a name.
var nameIntroducers = map[string]bool{
	"from": true, "join": true, "into": true, "update": true, "table": true, "only": true, "on": true,
	"truncate": true, "lock": true, "view": true, "sequence": true, "index": true, "type": true, "domain": true,
	"function": true, "procedure": true, "routine": true, "schema": true, "setof": true, "references": true,
}

// namePosition reports whether the token at i stands where a name does:
// after a name-introducing word or a comma, or before a qualifying dot.
func namePosition(toks []token, i int) bool {
	if i+1 < len(toks) && toks[i+1].kind == tokPunct && toks[i+1].value == "." {
		return true
	}
	if i == 0 {
		return false
	}
	p := toks[i-1]
	return p.kind == tokIdent && nameIntroducers[p.value] || p.kind == tokPunct && (p.value == "," || p.value == ".")
}

// needsSchema applies P1 to the schema of a qualified name a statement
// creates an object in.
func (s *state) needsSchema(q qname, what, id string) {
	if q.schema != nil {
		s.needs(qname{name: *q.schema}, kindSchema, false, what, id)
	}
}

// regclassName reads a string constant as regclassin would read a
// relation name: one or two dot-separated plain or quoted identifiers.
func regclassName(t token, src []byte) (qname, bool) {
	v, ok := literalValue(t, src)
	if !ok {
		return qname{}, false
	}
	parts := strings.Split(v, ".")
	if len(parts) > 2 {
		return qname{}, false
	}
	var names []string
	for _, p := range parts {
		n, ok := plainIdentifier(p)
		if !ok {
			return qname{}, false
		}
		names = append(names, n)
	}
	if len(names) == 2 {
		return qname{schema: &names[0], name: names[1]}, true
	}
	return qname{name: names[0]}, true
}

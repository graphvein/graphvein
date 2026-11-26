package schema

import (
	"fmt"
	"slices"
	"strings"
)

// T018a2 Revision 1.8 (2026-11-07, after the fourth independent re-check):
// X1 (dependents of a dropped object, closed by the pass's own dependency
// record), X2 (collation renames) and X4 (CREATE SCHEMA AUTHORIZATION of an
// unknown role name). X3 lives in createIndex and the foreign key target
// check, X4's named form in namesOf.
//
// Postgres records a dependency where the dependent is created, by the
// names the creating statement resolves (pg_depend: trigger.c 1049, a
// trigger on its function; event_trigger.c 330; pg_proc.c 640-650, a
// routine on its argument, result and OUT types, its BEGIN ATOMIC body and
// parameter defaults; heap.c 847-855, a column on its type and collation;
// pg_attrdef.c 193, a default on its expression; pg_type.c 711, a domain
// default; find_expr_references_walker 1783-1799, reg* constants), and
// refuses DROP without CASCADE while a NORMAL dependent exists
// (dependency.c findDependentObjects, reportDependentObjects 1150, with or
// without IF EXISTS). Every such name is written in a statement of the
// set, or in a body the set's code runs, or is produced by dynamic SQL
// (whose inventory is unbounded, O1.P12). The record keeps those words.

// rev18State is the state of the Revision 1.8 rules.
type rev18State struct {
	// entries is the dependency record (X1).
	entries []*depEntry
	// links joins the names an object has had (ALTER ... RENAME TO):
	// dependencies follow identity.
	links map[string]map[string]bool
	// gone are names the pass no longer holds: objects a DROP ... CASCADE
	// may have removed ("cascade") and collations renamed away
	// ("renamed").
	gone map[string]string
	// schemaUnknown is set once CREATE SCHEMA AUTHORIZATION CURRENT_USER
	// (or SESSION_USER, CURRENT_ROLE) created a schema whose name is the
	// role's, which is unknown (X4).
	schemaUnknown bool
	// dropOwner is the table of an index a DROP INDEX names, read before
	// the statement applies.
	dropOwner map[string]*table
}

// depEntry is one statement of the dependency record.
type depEntry struct {
	// words are the names the statement writes outside the positions Y39
	// (Revision 1.9) proves are no dependency; self are those its own
	// object depends on (a routine's header and SQL-standard body; for
	// other statements, all words). No word is removed: a word that also
	// occurs elsewhere in the statement is in words.
	words, self map[string]bool
	// nameAt, onAt and refAt are the words of the three positions of Y39:
	// the created (or changed) object's own name, the table an object of a
	// table is ON, and a REFERENCES target. A name is never a reference;
	// an ON-table is an AUTO or INTERNAL dependency, so it counts except
	// for a drop of a relation of that name; a REFERENCES target counts
	// unless the foreign-key record is exact for the key (refExact), which
	// then decides DROP TABLE (M6.P8).
	nameAt, onAt, refAt map[string]bool
	refExact            bool
	// sqlBody marks a routine with a SQL-standard body (BEGIN ATOMIC or
	// RETURN expr): it depends on the columns it uses (Y41).
	sqlBody bool
	// anyType marks a statement that copies columns whose types the pass
	// does not hold: it may depend on any type (Y44).
	anyType bool
	// qualified are the words written qualified, quoted or inside a
	// string: an unqualified built-in type name means pg_catalog's type.
	qualified map[string]bool
	// any is set when the statement may name an object by OID.
	any bool
	// owner is the table the statement's object belongs to (dropped with
	// it); nil for objects that are not a table's.
	owner *table
	// key identifies the object the statement created, for removal by a
	// later DROP of it ("" when it cannot be removed); sig is a routine's
	// argument text.
	key, sig string
	// kind and name are what the statement created (for CASCADE).
	kind, name string
	dead       bool
	text       string
}

// x1Kinds are the DROP kinds X1 governs, with their row's precondition.
var x1Kinds = [][]string{
	{"materialized", "view"}, {"foreign", "table"}, {"function"}, {"procedure"}, {"routine"}, {"aggregate"},
	{"type"}, {"domain"}, {"table"}, {"view"}, {"sequence"}, {"collation"}, {"schema"},
}

// depFreeHeads are statement heads that create no object with a
// dependency: data, queries, privileges, comments, maintenance, session
// and transaction control, and DROP. A routine they call runs code whose
// DDL is in the routine's own entry; dynamic SQL is unbounded (O1.P12).
// SET and RESET are kept: a search_path change decides the schema of later
// objects.
var depFreeHeads = map[string]bool{
	"insert": true, "update": true, "delete": true, "merge": true, "select": true, "with": true, "values": true,
	"table": true, "copy": true, "truncate": true, "comment": true, "grant": true, "revoke": true, "analyze": true,
	"vacuum": true, "cluster": true, "reindex": true, "refresh": true, "lock": true, "show": true, "begin": true,
	"commit": true, "rollback": true, "start": true, "end": true, "abort": true, "savepoint": true, "release": true,
	"notify": true, "listen": true, "unlisten": true, "discard": true, "explain": true, "drop": true, "security": true,
	"checkpoint": true, "deallocate": true, "prepare": true, "execute": true, "fetch": true, "close": true,
	"declare": true, "move": true, "call": true, "load": true,
}

// wordsOf adds the names a token list writes: identifiers, quoted
// identifiers (also folded), and the identifier-like words inside string
// constants, lower-cased.
func wordsOf(toks []token, src []byte, into map[string]bool, skip func(i int) bool) {
	for i, t := range toks {
		if skip != nil && skip(i) {
			continue
		}
		switch t.kind {
		case tokIdent:
			into[t.value] = true
		case tokQuoted:
			into[t.value] = true
			into[strings.ToLower(t.value)] = true
		case tokString:
			text, ok := literalValue(t, src)
			if !ok && t.start >= 0 && t.end <= len(src) && t.start < t.end {
				text = string(src[t.start:t.end])
			}
			textWords(text, into)
		}
	}
}

// textWords adds the identifier-like words of a text, lower-cased.
func textWords(text string, into map[string]bool) {
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r == '_' || r == '$' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x80)
	}) {
		into[w] = true
	}
}

// rev18Before applies the gone names (X1 after CASCADE, X2) and X4's
// unknown schema to a statement before it runs.
func (s *state) rev18Before(st statement, a analysis) {
	r := &s.rev18
	r.dropOwner = nil
	toks := st.tokens
	if s.failure != nil || len(toks) == 0 {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	if c.acceptKw("drop", "index") {
		c.acceptKw("concurrently")
		c.acceptKw("if", "exists")
		for {
			q, err := c.name()
			if err != nil {
				break
			}
			if t, _ := s.findIndex(q); t != nil {
				if r.dropOwner == nil {
					r.dropOwner = map[string]*table{}
				}
				r.dropOwner[q.String()] = t
			}
			if !c.acceptPunct(",") {
				break
			}
		}
	}
	creates := map[string]bool{}
	for _, e := range namesOf(a, toks).creates {
		creates[e.name] = true
	}
	if n, ok := createdCollation(toks, s.m.source); ok {
		creates[n] = true
	}
	conditionalDrop := toks[0].kind == tokIdent && toks[0].value == "drop" && containsKw(&cursor{toks: toks}, "exists")
	for _, t := range toks {
		if t.kind != tokIdent && t.kind != tokQuoted {
			continue
		}
		why := r.gone[t.value]
		if why == "" || conditionalDrop {
			continue
		}
		if creates[t.value] {
			if why == "cascade" {
				s.unknownFact(fmt.Sprintf("the statement creates %s, which an earlier DROP ... CASCADE may or may not have removed; Postgres refuses a name that is taken%s", t.value, precondition("R5.P8")))
				return
			}
			continue
		}
		what := "an earlier DROP ... CASCADE may have removed it with the object it depends on"
		if why == "renamed" {
			what = "an earlier ALTER COLLATION renamed it"
		}
		s.unknownFact(fmt.Sprintf("the statement names %s, a name the set created: %s, so Postgres may refuse a missing object%s", t.value, what, precondition("N1.P1")))
		return
	}
	if r.schemaUnknown {
		if c := (&cursor{toks: toks, src: s.m.source}); c.acceptKw("create", "schema") && !c.peekKw("if") {
			s.unknownFact("CREATE SCHEMA may be refused: an earlier CREATE SCHEMA AUTHORIZATION created a schema named after a role whose name is unknown" + precondition("N1.P1"))
			return
		}
		for i := 0; i+2 < len(toks); i++ {
			t := toks[i]
			if t.kind != tokIdent && t.kind != tokQuoted || toks[i+1].kind != tokPunct || toks[i+1].value != "." || i > 0 && toks[i-1].kind == tokPunct && toks[i-1].value == "." {
				continue
			}
			if catalogSchema(t.value) || s.holds(t.value, kindSchema) || creates[t.value] {
				continue
			}
			s.unknownFact(fmt.Sprintf("the statement names schema %s: an earlier CREATE SCHEMA AUTHORIZATION created a schema whose name (the role's) is unknown, which may be %s and may have been dropped or changed since; E1 does not cover it%s", t.value, t.value, precondition("N1.P1")))
			return
		}
	}
}

// catalogSchema reports the schema names no role can take (public is a
// reserved role name, pg_ names are reserved) and the information schema.
func catalogSchema(n string) bool {
	return n == "public" || n == "information_schema" || strings.HasPrefix(n, "pg_")
}

// createdCollation reads CREATE COLLATION [IF NOT EXISTS] name.
func createdCollation(toks []token, src []byte) (string, bool) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create", "collation") {
		return "", false
	}
	c.acceptKw("if", "not", "exists")
	q, err := c.name()
	if err != nil {
		return "", false
	}
	return q.name, true
}

// rev18After applies X1, X2 and X4 after a statement: the dependency check
// of a DROP without CASCADE (against the record before the statement), the
// objects a CASCADE removed, the entries the statement ends, the renames
// it makes and its own entry.
func (s *state) rev18After(st statement, a analysis) {
	r := &s.rev18
	toks := st.tokens
	if len(toks) == 0 || toks[0].kind != tokIdent {
		return
	}
	src := s.m.source
	if d, ok := readX1Drop(toks, src); ok {
		if !d.cascade && s.failure == nil {
			s.x1Check(d)
		}
		if d.cascade {
			s.x1Cascade(d)
		} else if s.failure == nil {
			s.x1Remove(d)
		}
	}
	if s.failure != nil {
		return
	}
	s.x1RemoveTableObject(toks, src)
	if s.x1Rename(toks, src) {
		// A rename creates no dependent.
		return
	}
	c := &cursor{toks: toks, src: src}
	if c.acceptKw("create", "schema") {
		c.acceptKw("if", "not", "exists")
		if c.acceptKw("authorization") && !c.done() {
			if t := c.tok(); t.kind == tokIdent && (t.value == "current_user" || t.value == "session_user" || t.value == "current_role") {
				r.schemaUnknown = true
			}
		}
	}
	if n, ok := createdCollation(toks, src); ok && r.gone[n] == "renamed" {
		delete(r.gone, n)
	}
	if s.collationUses(toks, src); s.failure != nil {
		return
	}
	if !depFreeHeads[toks[0].value] || a.query == "SELECT INTO" {
		// Y44 (Revision 1.9): SELECT INTO creates a table whose columns
		// depend on their types.
		s.recordDependencies(st, a)
	}
}

// collationUses applies X2 with U7 (M2.P1) to the collations a statement
// names outside the column definitions M2.P1 already checks: COLLATE in
// index elements and expressions, COMMENT ON COLLATION and ALTER
// COLLATION. A collation is proven when it is built in or held by the
// N1.P2 record (not dropped or renamed away since); E1 does not cover
// collations (V4).
func (s *state) collationUses(toks []token, src []byte) {
	if s.failure != nil {
		return
	}
	c := &cursor{toks: toks, src: src}
	var names []qname
	switch {
	case c.acceptKw("comment", "on", "collation"), c.acceptKw("alter", "collation"):
		c.acceptKw("if", "exists")
		if q, err := c.name(); err == nil {
			names = append(names, q)
		}
	case c.peekKw("create", "collation"), c.peekKw("drop"):
		// CREATE COLLATION ... FROM copies an existing collation (a
		// creation, its own rules); DROP is W5's.
	default:
		for i := 0; i+1 < len(toks); i++ {
			if toks[i].kind != tokIdent || toks[i].value != "collate" {
				continue
			}
			nc := &cursor{toks: toks, i: i + 1, src: src}
			if q, err := nc.name(); err == nil {
				names = append(names, q)
			}
		}
	}
	for _, q := range names {
		n := q.String()
		if why := s.collationProven(&n); why != "" {
			s.unknownFact(fmt.Sprintf("the statement names collation %s, which may not exist: %s%s", q, why, precondition("M2.P1")))
			return
		}
	}
}

// x1Drop is one DROP of an X1 kind.
type x1Drop struct {
	kind     string
	names    []qname
	sigs     []string
	cascade  bool
	ifExists bool
}

// readX1Drop reads DROP <X1 kind> [IF EXISTS] name [(args)] [, ...]
// [CASCADE | RESTRICT].
func readX1Drop(toks []token, src []byte) (x1Drop, bool) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("drop") {
		return x1Drop{}, false
	}
	var d x1Drop
	for _, words := range x1Kinds {
		if c.acceptKw(words...) {
			d.kind = strings.Join(words, " ")
			break
		}
	}
	if d.kind == "" {
		return x1Drop{}, false
	}
	d.ifExists = c.acceptKw("if", "exists")
	for {
		q, err := c.name()
		if err != nil {
			return x1Drop{}, false
		}
		sig := ""
		if c.isPunct("(") {
			start := c.i
			end, err := c.group()
			if err != nil {
				return x1Drop{}, false
			}
			sig = argText(toks[start:end])
		}
		d.names, d.sigs = append(d.names, q), append(d.sigs, sig)
		if !c.acceptPunct(",") {
			break
		}
	}
	d.cascade = containsKw(c, "cascade")
	return d, true
}

// argText is the normalised text of an argument list.
func argText(toks []token) string {
	var b strings.Builder
	for _, t := range toks {
		b.WriteString(t.value)
		b.WriteByte(' ')
	}
	return b.String()
}

// aliases are the names an object of name n has had, and the names of its
// array type and multirange.
func (r *rev18State) aliases(n string) map[string]bool {
	out := map[string]bool{n: true}
	todo := []string{n}
	for len(todo) > 0 {
		x := todo[0]
		todo = todo[1:]
		for y := range r.links[x] {
			if !out[y] {
				out[y] = true
				todo = append(todo, y)
			}
		}
	}
	for x := range copySet(out) {
		out["_"+x] = true
		out[x+"_multirange"] = true
		if strings.Contains(x, "range") {
			out[strings.Replace(x, "range", "multirange", 1)] = true
		}
	}
	return out
}

// copySet returns a copy of a set to range over while adding to it.
func copySet(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// link joins two names of one object.
func (r *rev18State) link(a, b string) {
	if r.links == nil {
		r.links = map[string]map[string]bool{}
	}
	for _, p := range [][2]string{{a, b}, {b, a}} {
		if r.links[p[0]] == nil {
			r.links[p[0]] = map[string]bool{}
		}
		r.links[p[0]][p[1]] = true
	}
}

// live reports whether an entry still stands for an object that exists.
func (s *state) live(e *depEntry) bool {
	return !e.dead && (e.owner == nil || slices.Contains(s.tables, e.owner))
}

// naming returns the first live entry that names one of the names, using
// its self words only when self is set. With typeNames, an unqualified
// built-in type name (date, text, ...) means pg_catalog's type while the
// search_path is the default one (pg_catalog is searched first), so only
// its qualified, quoted or string spellings count.
func (s *state) naming(names map[string]bool, self, typeNames bool, kind string) *depEntry {
	for _, e := range s.rev18.entries {
		if !s.live(e) {
			continue
		}
		if e.any {
			return e
		}
		for n := range names {
			if !e.names(n, self, kind) {
				continue
			}
			if typeNames && builtinTypeWords[n] && !s.rev16.pathSet && !e.qualified[n] {
				continue
			}
			return e
		}
	}
	return nil
}

// onTableKinds are the drop kinds whose objects other objects can be ON
// (triggers, rules, policies, indexes, statistics: Y39 position 2).
var onTableKinds = map[string]bool{"table": true, "foreign table": true, "view": true, "materialized view": true}

// names reports whether the entry has an occurrence of n that counts for a
// drop of kind (Y39): any occurrence outside the three proven positions,
// an ON-table occurrence unless a relation of that name is dropped, and a
// REFERENCES target unless the foreign-key record is exact for it. With
// self, only the words its own object depends on.
func (e *depEntry) names(n string, self bool, kind string) bool {
	words := e.words
	if self {
		words = e.self
	}
	return words[n] || e.onAt[n] && !onTableKinds[kind] || e.refAt[n] && !e.refExact
}

// typeNamespace are the DROP kinds whose objects have a pg_type name.
var typeNamespace = map[string]bool{"type": true, "domain": true, "table": true, "view": true, "materialized view": true, "foreign table": true}

// x1Check applies X1 to a DROP without CASCADE: applied only when no live
// entry of the record names the object (or, for a schema, may have created
// an object in it).
func (s *state) x1Check(d x1Drop) {
	id := "R5.P8"
	if d.kind == "table" {
		id = "M6.P8"
	}
	for _, q := range d.names {
		e := s.naming(s.dropAliases(d.kind, q), false, typeNamespace[d.kind], d.kind)
		if e == nil && (d.kind == "type" || d.kind == "domain") {
			// Y44: a statement that copied columns whose types the pass
			// does not hold may depend on any type.
			e = s.anyTypeEntry()
		}
		if e == nil && d.kind == "schema" {
			// An unqualified object goes to the first schema of the
			// search_path, which may be this one ("$user", or a changed
			// path): any entry made while the path was not the default.
			if s.userSchemaUnknown() || s.rev16.pathSet {
				for _, x := range s.rev18.entries {
					if s.live(x) {
						e = x
						break
					}
				}
			}
		}
		if e == nil {
			continue
		}
		s.unknownFact(fmt.Sprintf("DROP %s %s may be refused: the set's statement %q names it (or may have created an object in it), and Postgres refuses to drop an object another object depends on without CASCADE%s", strings.ToUpper(d.kind), q, e.text, precondition(id)))
		return
	}
}

// x1Cascade applies X1 after DROP ... CASCADE: every object a live entry
// naming the dropped objects created may be gone.
func (s *state) x1Cascade(d x1Drop) {
	r := &s.rev18
	if r.gone == nil {
		r.gone = map[string]string{}
	}
	for _, q := range d.names {
		names := s.dropAliases(d.kind, q)
		for _, e := range r.entries {
			if !s.live(e) || e.name == "" {
				continue
			}
			hit := e.any || e.anyType && (d.kind == "type" || d.kind == "domain")
			for n := range names {
				if e.names(n, true, d.kind) {
					hit = true
					break
				}
			}
			if d.kind == "schema" && e.owner == nil {
				// Objects of the schema: those it may hold are named by
				// their entries' words.
				hit = hit || e.words[q.name]
			}
			if !hit {
				continue
			}
			r.gone[e.name] = "cascade"
			if e.owner != nil && e.kind != "table" {
				if names, unknown := e.owner.objectNames(e.kind); names != nil {
					*unknown = true
				}
			}
			// Y39 (Revision 1.9): the record is a superset, so a word
			// occurrence does not prove that the CASCADE removed the
			// entry's object; the entry stays live (its object may still
			// depend on what it names) and its name is no longer held.
		}
	}
}

// x1Remove ends the entries of the objects a DROP without CASCADE the pass
// applied removed: those of the same written name (and argument text, for
// a routine whose overloads the record cannot tell apart) under a certain
// search_path.
func (s *state) x1Remove(d x1Drop) {
	r := &s.rev18
	for i, q := range d.names {
		if q.schema == nil && s.pathChanged {
			continue
		}
		key := depKey(d.kind, q)
		var hits []*depEntry
		sigs := map[string]bool{}
		for _, e := range r.entries {
			if !e.dead && e.key == key {
				hits = append(hits, e)
				sigs[e.sig] = true
			}
		}
		if routineKinds[d.kind] {
			if len(sigs) > 1 || d.sigs[i] != "" && len(sigs) == 1 && !sigs[d.sigs[i]] {
				continue
			}
		}
		for _, e := range hits {
			e.dead = true
		}
	}
}

// routineKinds are the kinds whose objects are identified by name and
// argument types.
var routineKinds = map[string]bool{"function": true, "procedure": true, "routine": true, "aggregate": true}

// depKey is the removal key of an object of a kind and written name.
func depKey(kind string, q qname) string {
	switch {
	case routineKinds[kind]:
		kind = "routine"
	case kind == "collation", kind == "schema":
	default:
		kind = "relation"
	}
	return kind + "\x00" + q.String()
}

// x1Rename links the names of ALTER <kind> name [(args)] RENAME TO new
// (identity: the object keeps its dependents) and applies X2 to a
// collation.
func (s *state) x1Rename(toks []token, src []byte) bool {
	n := len(toks)
	if n < 5 || toks[n-3].kind != tokIdent || toks[n-3].value != "rename" || toks[n-2].kind != tokIdent || toks[n-2].value != "to" || toks[n-1].kind != tokIdent && toks[n-1].kind != tokQuoted {
		return false
	}
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("alter") {
		return false
	}
	kind := ""
	for _, words := range append(slices.Clone(x1Kinds), []string{"index"}, []string{"conversion"}, []string{"statistics"}) {
		if c.acceptKw(words...) {
			kind = strings.Join(words, " ")
			break
		}
	}
	if kind == "" {
		return false
	}
	c.acceptKw("if", "exists")
	q, err := c.name()
	if err != nil {
		return false
	}
	sig := ""
	if c.isPunct("(") {
		start := c.i
		end, err := c.group()
		if err != nil {
			return false
		}
		sig = argText(toks[start:end])
		// Y45 (Revision 1.9): group does not move the cursor; before, a
		// rename with an argument list was never read (its names were not
		// linked).
		c.i = end
	}
	if c.i != n-3 {
		return false
	}
	to := toks[n-1].value
	s.rev18.link(q.name, to)
	s.rekey(kind, q, sig, to)
	if kind == "collation" {
		// X2: the old name is no longer held; the new one is.
		if s.rev18.gone == nil {
			s.rev18.gone = map[string]string{}
		}
		if q.name != to {
			s.rev18.gone[q.name] = "renamed"
		}
		delete(s.rev18.gone, to)
	}
	return true
}

// x1RemoveTableObject ends the entry of a trigger, policy or index a DROP
// the pass applied removed (keyed by its name and its table).
func (s *state) x1RemoveTableObject(toks []token, src []byte) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("drop") {
		return
	}
	kill := func(key string) {
		for _, e := range s.rev18.entries {
			if e.key == key {
				e.dead = true
			}
		}
	}
	switch {
	case c.acceptKw("trigger"), c.acceptKw("policy"):
		kind := toks[1].value
		c.acceptKw("if", "exists")
		name, err := c.ident()
		if err != nil || !c.acceptKw("on") {
			return
		}
		q, err := c.name()
		if err != nil {
			return
		}
		if t, others := s.lookup(q); t != nil && len(others) == 0 && (q.schema != nil || !s.pathChanged) {
			kill(fmt.Sprintf("%s\x00%s\x00%p", kind, name, t))
		}
	case c.peekKw("index"):
		for k, t := range s.rev18.dropOwner {
			kill(fmt.Sprintf("index\x00%s\x00%p", bareName(k), t))
		}
	}
}

// recordDependencies adds the statement's entry to the dependency record.
func (s *state) recordDependencies(st statement, a analysis) {
	r := &s.rev18
	toks := st.tokens
	src := s.m.source
	e := &depEntry{words: map[string]bool{}, text: shortText(statementText(toks, src))}
	c := &cursor{toks: toks, src: src}
	// Y39 (Revision 1.9): the token positions proven not to be a
	// dependency: the last part of the entry's own name (nameIdx) and of
	// the table its object is ON (onIdx). Schema qualifiers stay words (an
	// object depends on its schema).
	nameIdx, onIdx := map[int]bool{}, map[int]bool{}
	var owner *qname
	replace := false
	switch {
	case c.acceptKw("create"):
		replace = c.acceptKw("or", "replace")
		for _, w := range []string{"global", "local", "temp", "temporary", "unlogged", "unique", "constraint", "recursive", "trusted", "procedural", "default"} {
			c.acceptKw(w)
		}
		kind := ""
		for _, words := range append(slices.Clone(x1Kinds), []string{"index"}, []string{"trigger"}, []string{"policy"},
			[]string{"rule"}, []string{"statistics"}, []string{"event", "trigger"}, []string{"operator"}, []string{"cast"},
			[]string{"extension"}, []string{"server"}, []string{"publication"}, []string{"language"}) {
			if c.acceptKw(words...) {
				kind = strings.Join(words, " ")
				break
			}
		}
		e.kind = kind
		c.acceptKw("concurrently")
		c.acceptKw("if", "not", "exists")
		var name *qname
		if kind != "cast" && !(kind == "index" && c.peekKw("on")) && !(kind == "schema" && c.peekKw("authorization")) {
			if q, err := c.name(); err == nil {
				name = &q
				if kind != "schema" || c.done() {
					// Schema elements depend on the schema (an
					// AUTHORIZATION clause alone keeps the name too: a
					// superset).
					nameIdx[c.i-1] = true
				}
			}
		}
		if name != nil {
			e.name = name.name
		}
		switch kind {
		case "index", "trigger", "policy", "statistics":
			at := "on"
			if kind == "statistics" {
				at = "from"
			}
			if i := depth0(toks, c.i, at); i >= 0 {
				oc := &cursor{toks: toks, i: i + 1, src: src}
				oc.acceptKw("only")
				if q, err := oc.name(); err == nil {
					owner = &q
					onIdx[oc.i-1] = true
				}
			}
		case "rule":
			if i := depth0(toks, c.i, "to"); i >= 0 {
				oc := &cursor{toks: toks, i: i + 1, src: src}
				if q, err := oc.name(); err == nil {
					owner = &q
					onIdx[oc.i-1] = true
				}
			}
		case "table", "foreign table":
			owner = name
		}
		if name != nil {
			switch {
			case routineKinds[kind]:
				e.key = depKey(kind, *name)
				if c.isPunct("(") {
					start := c.i
					if end, err := c.group(); err == nil {
						e.sig = argText(toks[start:end])
					}
				}
			case kind == "trigger" || kind == "policy" || kind == "index":
				// Keyed with the owner below.
			case kind != "" && kind != "table" && kind != "foreign table" && kind != "schema":
				e.key = depKey(kind, *name)
			}
		}
		if kind == "type" {
			// A range's multirange may be named in the statement.
			for i := 0; i+2 < len(toks); i++ {
				if toks[i].kind == tokIdent && toks[i].value == "multirange_type_name" && toks[i+1].kind == tokOp && toks[i+1].value == "=" && name != nil {
					r.link(name.name, toks[i+2].value)
				}
			}
		}
	case c.acceptKw("alter"):
		if c.acceptKw("table") || c.acceptKw("foreign", "table") {
			c.acceptKw("if", "exists")
			c.acceptKw("only")
			if q, err := c.name(); err == nil {
				owner = &q
				nameIdx[c.i-1] = true
			}
		} else {
			for _, words := range append(slices.Clone(x1Kinds), []string{"index"}, []string{"trigger"}, []string{"policy"}, []string{"rule"}, []string{"statistics"}) {
				if c.acceptKw(words...) {
					break
				}
			}
			c.acceptKw("if", "exists")
			if _, err := c.name(); err == nil {
				nameIdx[c.i-1] = true
			}
			if i := depth0(toks, c.i, "on"); i >= 0 && c.i < len(toks) {
				oc := &cursor{toks: toks, i: i + 1, src: src}
				if q, err := oc.name(); err == nil {
					owner = &q
					onIdx[oc.i-1] = true
				}
			}
		}
	}
	if owner != nil {
		if t, _ := s.lookup(*owner); t != nil {
			e.owner = t
		}
		if e.owner != nil && e.name != "" && (e.kind == "trigger" || e.kind == "policy" || e.kind == "index") {
			e.key = fmt.Sprintf("%s\x00%s\x00%p", e.kind, e.name, e.owner)
		}
		if e.kind == "table" || e.kind == "foreign table" {
			e.kind = "table"
		}
	}
	// A routine's AS body records no dependency itself; the DDL it may
	// run creates dependents when it is called. A DO body runs now.
	var body *routineBody
	bodyAt := -1
	if _, b := routineBodyOf(toks, src); b != nil {
		body = b
		e.sqlBody = b.toks != nil
		for i := 0; i+1 < len(toks); i++ {
			if toks[i].kind == tokIdent && toks[i].value == "as" && toks[i+1].kind == tokString {
				bodyAt = i + 1
				break
			}
		}
	} else if toks[0].value == "do" {
		body = doBody(toks, src)
		for i, t := range toks {
			if t.kind == tokString {
				bodyAt = i
				break
			}
		}
	}
	// The referenced table of a foreign key is M6's: its exact record of
	// foreign keys decides DROP TABLE (Revision 3 class 3, Revision 8).
	refs := map[int]bool{}
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].kind == tokIdent && toks[i].value == "references" {
			refs[i+1] = true
			if i+3 < len(toks) && toks[i+2].kind == tokPunct && toks[i+2].value == "." {
				refs[i+3] = true
			}
		}
	}
	skip := func(i int) bool {
		return refs[i] || nameIdx[i] || onIdx[i] || i == bodyAt || i == bodyAt+2 && bodyAt >= 0 && toks[bodyAt+1].value == ","
	}
	wordsOf(toks, src, e.words, skip)
	e.nameAt, e.onAt, e.refAt = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i := range toks {
		switch {
		case nameIdx[i]:
			wordsOf(toks[i:i+1], src, e.nameAt, nil)
		case onIdx[i]:
			wordsOf(toks[i:i+1], src, e.onAt, nil)
		case refs[i]:
			wordsOf(toks[i:i+1], src, e.refAt, nil)
		}
	}
	// A REFERENCES target is decided by M6's foreign-key record when the
	// table that holds the key is held with its facts known.
	e.refExact = e.owner != nil && factsKnown(e.owner)
	e.qualified = map[string]bool{}
	for i, t := range toks {
		if skip(i) && !nameIdx[i] && !onIdx[i] && !refs[i] {
			continue
		}
		if t.kind == tokQuoted || t.kind == tokString || t.kind == tokIdent && i > 0 && toks[i-1].kind == tokPunct && toks[i-1].value == "." {
			wordsOf(toks[i:i+1], src, e.qualified, nil)
		}
	}
	e.self = copySet(e.words)
	if body != nil && body.toks == nil && bodyAt >= 0 {
		bw := map[string]bool{}
		sc := scanBody(&routineBody{lang: body.lang, text: body.text, toks: body.toks, src: body.src, all: true})
		if body.unread != "" || sc.unbounded != "" {
			wordsOf(toks[bodyAt:bodyAt+1], src, bw, nil)
		} else {
			for _, d := range sc.ddl {
				wordsOf(d.st.tokens, d.src, bw, nil)
			}
		}
		for w := range bw {
			e.words[w] = true
			e.qualified[w] = true
		}
		if toks[0].value == "do" {
			e.self = map[string]bool{}
		}
	}
	// A reg* constant written as a number names an object by OID.
	hasReg, hasNumber := false, false
	for _, t := range toks {
		hasReg = hasReg || t.kind == tokIdent && regTypes[t.value]
		hasNumber = hasNumber || t.kind == tokNumber
	}
	e.any = hasReg && hasNumber
	// LIKE, INHERITS and PARTITION OF copy defaults, constraints and
	// indexes of the parent: its entries' words are the child's too.
	if a.create != nil {
		parents := slices.Clone(a.inherits)
		for _, p := range []*qname{a.create.partitionOf, a.create.likeOf} {
			if p != nil {
				parents = append(parents, *p)
			}
		}
		for _, p := range parents {
			pt, _ := s.lookup(p)
			if pt == nil {
				if s.names.creates(p.name, kindRelation) {
					e.any = true
				}
				continue
			}
			for _, x := range r.entries {
				if x.owner == pt && s.live(x) {
					for w := range x.words {
						e.words[w] = true
						e.self[w] = true
					}
					for w := range x.qualified {
						e.qualified[w] = true
					}
				}
			}
			// The parent's generated sequences (serial, identity): a
			// copied default uses them by a name no statement writes.
			for _, g := range generatedNames(pt) {
				e.words[g] = true
				e.self[g] = true
				e.qualified[g] = true
			}
		}
	}
	// Y39: CREATE OR REPLACE of the same identity replaces the object; the
	// new statement's entry follows.
	if replace && e.key != "" {
		for _, x := range r.entries {
			if !x.dead && x.key == e.key && x.sig == e.sig {
				x.dead = true
			}
		}
	}
	s.copiedTypes(e, a)
	r.entries = append(r.entries, e)
}

// shortText is a statement's text cut for a message.
func shortText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 80 {
		text = text[:77] + "..."
	}
	return text
}

// notATable applies X3 (V4 kinds) to a CREATE INDEX or REFERENCES target
// the pass does not hold as a table: one it holds as another relation
// kind is refused when the kind is certain (42809: indexcmds.c DefineIndex
// "cannot create index on relation", tablecmds.c "referenced relation is
// not a table") and may fail otherwise; an index on a materialized view
// may fail (its columns are not proven, R1b.P5). It reports whether it
// recorded a failure.
func (s *state) notATable(q qname, what, id string, index bool) bool {
	if s.failure != nil {
		return false
	}
	kind, certain, found := s.relationAt(q)
	if !found || kind == "table" {
		return false
	}
	if index && kind == "materialized view" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s is a materialized view, whose columns the pass does not prove%s", what, q, precondition(id)))
		return true
	}
	if certain && kind != "generated index or sequence" {
		s.mayFail(fmt.Sprintf("%s: %s is %s, not a table (Postgres refuses, 42809)%s", what, q, article(kind), precondition(id)))
		return true
	}
	s.unknownFact(fmt.Sprintf("%s may be refused: %s may be %s, not a table%s", what, q, article(kind), precondition(id)))
	return true
}

// mayBeOtherTarget applies X3 to a table the pass may hold over another
// relation kind (CREATE TABLE IF NOT EXISTS over a name it may not have
// created): as a CREATE INDEX or REFERENCES target it is not proven.
func (s *state) mayBeOtherTarget(t *table, what, id string) bool {
	if s.failure != nil || t == nil || !t.mayBeOther {
		return false
	}
	s.unknownFact(fmt.Sprintf("%s may be refused: %s may be a relation of another kind (CREATE TABLE IF NOT EXISTS may have been skipped)%s", what, t.q, precondition(id)))
	return true
}

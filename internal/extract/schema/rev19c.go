package schema

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// T018a2 Revision 1.9, Step U-c: the dependency rule without exceptions
// (Y39 with Y40, Y41, Y44, Y45 and Y52), statements code runs from
// constants and static statements of code (Y48-Y50), and the
// superuser-only statements (Y51, D-superuser, E6).

// superuserForm is one superuser-only check of REL_17_6 (Y51), generated
// into superuserForms by TestGenerateSuperuserForms.
type superuserForm struct {
	form, source, message, detail string
}

// matches reports whether a refusal's message and detail are this check's
// (the C format's conversions match any text). An untrusted-language
// refusal shares its message with a missing USAGE privilege on a trusted
// language, so plpgsql and sql are not matched (E6 grants USAGE).
func (f superuserForm) matches(message, detail string) bool {
	if !cFormat(f.message).MatchString(message) {
		return false
	}
	if f.detail != "" && !cFormat(f.detail).MatchString(detail) {
		return false
	}
	if strings.HasPrefix(f.message, "permission denied for language ") {
		lang := strings.TrimPrefix(message, "permission denied for language ")
		return lang != "plpgsql" && lang != "sql"
	}
	return true
}

// cFormat turns a C message format into an anchored pattern.
func cFormat(f string) *regexp.Regexp {
	parts := regexp.MustCompile(`%[-0-9.]*[a-z]`).Split(f, -1)
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$")
}

// y51SuperuserOnly reports whether a statement is one that only a
// superuser may run (Y51, D-superuser; E6 grants no superuser), with the
// precondition id of its row, from the forms of superuserForms. Forms
// that are already refused or may fail elsewhere (ALTER ROLE ALL ... SET,
// LEAKPROOF, SUPPORT, untrusted languages, PGC_SUSET parameters) are
// listed too, so that each form has exactly one branch.
func y51SuperuserOnly(toks []token, src []byte) (form, id string) {
	c := &cursor{toks: toks, src: src}
	switch {
	case c.peekKw("create", "event", "trigger"):
		return "CREATE EVENT TRIGGER", "O5.P4"
	case c.peekKw("alter", "event", "trigger"):
		return "ALTER EVENT TRIGGER", "O5.P4"
	case c.peekKw("create", "role"), c.peekKw("create", "user"), c.peekKw("create", "group"):
		if depth0Word(toks, 3, "superuser") {
			return "CREATE ROLE ... SUPERUSER", "N1.P4"
		}
	case c.peekKw("alter", "role"), c.peekKw("alter", "user"), c.peekKw("alter", "group"):
		c.i = 2
		if c.acceptKw("all") && (c.peekKw("set") || c.peekKw("reset") || c.peekKw("in")) {
			return "ALTER ROLE ALL ... SET", "N1.P4"
		}
		if !depth0Word(toks, 3, "set") && !depth0Word(toks, 3, "reset") && !depth0Word(toks, 3, "rename") &&
			(depth0Word(toks, 3, "superuser") || depth0Word(toks, 3, "nosuperuser")) {
			return "ALTER ROLE ... [NO]SUPERUSER", "N1.P4"
		}
	case c.peekKw("create", "cast"):
		if depth0Words(toks, 2, "without", "function") {
			return "CREATE CAST ... WITHOUT FUNCTION", "N1.P4"
		}
	case c.peekKw("create", "language"), c.peekKw("create", "or", "replace", "language"),
		c.peekKw("create", "trusted"), c.peekKw("create", "procedural"),
		c.peekKw("create", "or", "replace", "trusted"), c.peekKw("create", "or", "replace", "procedural"):
		return "CREATE LANGUAGE", "N1.P4"
	case c.peekKw("create", "type"):
		c.i = 2
		if _, err := c.name(); err == nil && (c.done() || c.isPunct("(")) {
			return "CREATE TYPE of a base or shell type", "R1.P4"
		}
	case c.peekKw("alter", "type"):
		c.i = 2
		if _, err := c.name(); err == nil && c.acceptKw("set") && c.isPunct("(") {
			return "ALTER TYPE ... SET", "R4.P4"
		}
	case c.peekKw("create", "operator", "class"), c.peekKw("create", "operator", "family"), c.peekKw("alter", "operator", "family"):
		return "CREATE OPERATOR CLASS / FAMILY, ALTER OPERATOR FAMILY", "N1.P4"
	case c.peekKw("create", "text", "search", "parser"), c.peekKw("create", "text", "search", "template"):
		return "CREATE TEXT SEARCH PARSER / TEMPLATE", "N1.P4"
	case c.peekKw("create", "access", "method"):
		return "CREATE ACCESS METHOD", "N1.P4"
	case c.peekKw("create", "foreign", "data", "wrapper"), c.peekKw("alter", "foreign", "data", "wrapper"):
		return "CREATE / ALTER FOREIGN DATA WRAPPER", "N1.P4"
	case c.peekKw("create", "publication"):
		if depth0Words(toks, 2, "all", "tables") || depth0Words(toks, 2, "tables", "in", "schema") {
			return "CREATE PUBLICATION FOR ALL TABLES / TABLES IN SCHEMA", "N1.P4"
		}
	case c.peekKw("alter", "publication"):
		if (depth0Word(toks, 2, "add") || depth0Word(toks, 2, "set")) && depth0Words(toks, 2, "tables", "in", "schema") {
			return "ALTER PUBLICATION ADD / SET TABLES IN SCHEMA", "N1.P4"
		}
	case c.peekKw("alter", "table"), c.peekKw("alter", "foreign", "table"):
		// ENABLE / DISABLE [REPLICA | ALWAYS] TRIGGER ALL includes the
		// table's internal triggers (foreign keys, deferrable keys):
		// trigger.c 1771 refuses them unless superuser (internalTriggers
		// decides the held tables that certainly have none).
		for i := 2; i+1 < len(toks); i++ {
			if toks[i].kind == tokIdent && (toks[i].value == "enable" || toks[i].value == "disable") {
				j := i + 1
				if j < len(toks) && toks[j].kind == tokIdent && (toks[j].value == "replica" || toks[j].value == "always") {
					j++
				}
				if j+1 < len(toks) && toks[j].kind == tokIdent && toks[j].value == "trigger" &&
					(toks[j+1].kind == tokIdent && toks[j+1].value == "all" || strings.HasPrefix(strings.ToLower(toks[j+1].value), "ri_constrainttrigger")) {
					return "ENABLE / DISABLE TRIGGER ALL or of a system trigger", "M5k-b.P4"
				}
			}
		}
	case c.peekKw("set", "session", "authorization"), c.peekKw("set", "session", "session", "authorization"), c.peekKw("set", "local", "session", "authorization"):
		// variable.c 881: only a superuser may become another role; the
		// migrating role's own name is not known (E4), so any name may be
		// another role. DEFAULT and the role itself are the same session.
		i := depth0Index(toks, 1, "authorization")
		if i+1 < len(toks) && !(toks[i+1].kind == tokIdent && (toks[i+1].value == "default" || toks[i+1].value == "session_user" || toks[i+1].value == "current_user")) {
			return "SET SESSION AUTHORIZATION of another role", "N2.P4"
		}
	case c.peekKw("create", "extension"):
		c.i = 2
		ine := c.acceptKw("if", "not", "exists")
		n, ok := extensionName(c)
		if !ok {
			return "", ""
		}
		if ine && n == "plpgsql" {
			// initdb installs plpgsql: IF NOT EXISTS skips before the
			// superuser check.
			return "", ""
		}
		if !extensionTrusted(n, depth0Word(toks, 3, "cascade")) {
			return "CREATE EXTENSION of an extension that is not trusted", "O2.P4"
		}
	case c.peekKw("alter", "extension"):
		c.i = 2
		if n, ok := extensionName(c); ok && c.acceptKw("update") && !extensionTrusted(n, false) {
			return "ALTER EXTENSION ... UPDATE of an extension that is not trusted", "O2.P4"
		}
	}
	if fn, ok := routineSuperuserOnly(toks, src); ok {
		return fn, "R6.P4"
	}
	return "", ""
}

// extensionTrusted reports whether a role that is not a superuser may
// install an extension (Y51): its REL_17_6 control file says trusted =
// true, and with CASCADE so do the extensions it requires.
func extensionTrusted(n string, cascade bool) bool {
	e, ok := e1ExtensionVersions[n]
	if !ok || !e.trusted {
		return false
	}
	if cascade {
		for _, r := range e.requires {
			if r != "plpgsql" && !extensionTrusted(r, true) {
				return false
			}
		}
	}
	return true
}

// routineSuperuserOnly reads CREATE [OR REPLACE] FUNCTION / PROCEDURE and
// ALTER FUNCTION / PROCEDURE / ROUTINE for LEAKPROOF, SUPPORT and a
// language other than plpgsql and sql (functioncmds.c 703, 1134, 1419;
// an untrusted language, aclchk.c 2759: c, internal, and the untrusted
// procedural languages; a trusted language other than these two is not
// installed by initdb, E1 lists none).
func routineSuperuserOnly(toks []token, src []byte) (string, bool) {
	c := &cursor{toks: toks, src: src}
	create := c.acceptKw("create")
	if create {
		c.acceptKw("or", "replace")
		if !c.acceptKw("function") && !c.acceptKw("procedure") {
			return "", false
		}
	} else if !c.acceptKw("alter") || !c.acceptKw("function") && !c.acceptKw("procedure") && !c.acceptKw("routine") {
		return "", false
	}
	for i := c.i; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tokIdent {
			continue
		}
		switch {
		case t.value == "leakproof" && (i == 0 || toks[i-1].kind != tokIdent || toks[i-1].value != "not"):
			return "a function with LEAKPROOF", true
		case t.value == "support" && i+1 < len(toks) && (toks[i+1].kind == tokIdent || toks[i+1].kind == tokQuoted):
			return "a function with SUPPORT", true
		case create && t.value == "language" && i+1 < len(toks):
			lang := strings.ToLower(toks[i+1].value)
			if toks[i+1].kind == tokString {
				lang, _ = literalValue(toks[i+1], src)
				lang = strings.ToLower(lang)
			}
			if lang != "plpgsql" && lang != "sql" {
				return "a function in a language that is not trusted", true
			}
		}
	}
	return "", false
}

// depth0Word reports whether a word appears at parenthesis depth 0 from
// token from on.
func depth0Word(toks []token, from int, w string) bool {
	return depth0(toks, from, w) >= 0
}

// depth0Words reports whether the words appear in sequence at depth 0.
func depth0Words(toks []token, from int, words ...string) bool {
	for i := from; i < len(toks); i++ {
		if i = depth0(toks, i, words[0]); i < 0 {
			return false
		}
		ok := i+len(words) <= len(toks)
		for k := 1; ok && k < len(words); k++ {
			ok = toks[i+k].kind == tokIdent && toks[i+k].value == words[k]
		}
		if ok {
			return true
		}
	}
	return false
}

// y51 applies Y51 to a statement (top level or one code runs): only a
// superuser may run it, so it may fail.
func (s *state) y51(toks []token, src []byte) {
	if s.failure != nil {
		return
	}
	if form, id := y51SuperuserOnly(toks, src); form != "" && !(id == "M5k-b.P4" && !s.internalTriggers(toks, src)) && !(id == "O2.P4" && s.platformExtension(toks, src)) {
		s.unknownFact(fmt.Sprintf("%s may be refused: only a superuser may run it, and the migrating role is not assumed to be one (E6)%s", form, precondition(id)))
	}
}

// rev19cBefore applies the U-c statement rules before a statement runs.
func (s *state) rev19cBefore(st statement, a analysis) {
	s.rev19c.seq++
	if s.failure != nil || len(st.tokens) == 0 {
		return
	}
	s.y51(st.tokens, s.m.source)
	s.e6Roles(st.tokens, s.m.source, a)
	if s.failure == nil {
		s.y40y41Alter(st.tokens, a)
	}
	s.rev19c.y52 = nil
	if s.failure == nil {
		s.rev19c.y52 = s.y52Before(st.tokens, s.m.source)
		s.tableSeqAliases(st.tokens, s.m.source)
	}
}

// rev19cAfter applies the U-c rules after a statement: CASCADE column
// removal (Y52), owned sequences (Y40) and the type records Y52 reads.
func (s *state) rev19cAfter(st statement, a analysis) {
	toks := st.tokens
	if s.failure != nil || len(toks) == 0 {
		return
	}
	s.y52After(s.rev19c.y52)
	s.rev19c.y52 = nil
	if s.failure != nil {
		return
	}
	s.y40OwnedBy(toks, s.m.source)
	s.y52Types(toks, a)
	s.y50Changes(toks, s.m.source, a)
}

// y52Types records the base of CREATE DOMAIN and the subtype of CREATE
// TYPE ... AS RANGE, and counts the creations of names in the pg_type
// namespace (tables, views, types, domains, renames to the name).
func (s *state) y52Types(toks []token, a analysis) {
	r := &s.rev19c
	if r.creations == nil {
		r.creations, r.bases = map[string]int{}, map[string][]string{}
	}
	for _, e := range namesOf(a, toks).creates {
		if e.kind&(kindRelation|kindType) != 0 {
			r.creations[e.name]++
		}
	}
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.acceptKw("create", "domain"):
		q, err := c.name()
		if err != nil {
			return
		}
		c.acceptKw("as")
		var base []string
		for ; !c.done(); c.i++ {
			t := c.tok()
			if t.kind == tokIdent && (t.value == "constraint" || t.value == "not" || t.value == "null" || t.value == "check" || t.value == "default" || t.value == "collate") {
				break
			}
			if t.kind == tokIdent || t.kind == tokQuoted {
				base = append(base, t.value)
			}
		}
		r.bases[q.name] = base
	case c.acceptKw("create", "type"):
		q, err := c.name()
		if err != nil || !c.acceptKw("as", "range") {
			return
		}
		for i := c.i; i+2 < len(toks); i++ {
			if toks[i].kind == tokIdent && toks[i].value == "subtype" && toks[i+1].kind == tokOp && toks[i+1].value == "=" {
				sc := &cursor{toks: toks, i: i + 2, src: s.m.source}
				if st, err := sc.name(); err == nil {
					r.bases[q.name] = []string{st.name}
				}
				break
			}
		}
	}
}

// placeholderSetting applies Y51 (N2.P4) to the setting a role or
// routine stores (ALTER ROLE / USER ... SET or RESET, a routine's SET
// clause): a two-part placeholder parameter is checked as PGC_SUSET
// there (guc.c validate_option_array_item: an unknown custom parameter
// needs a superuser or a SET privilege on it), and E6 grants neither.
// It reads the cursor's copy; "" when the name is not a placeholder.
func placeholderSetting(c *cursor) string {
	sub := *c
	name, parts, ok := gucName(&sub)
	if !ok || parts != 2 {
		return ""
	}
	return fmt.Sprintf("parameter %s is a placeholder, which a stored setting checks as a superuser-only parameter (Postgres refuses a role that is not a superuser, 42501; E6)%s", name, precondition("N2.P4"))
}

// rekey applies Y45 to ALTER <kind> q [(args)] RENAME TO to: the entries
// of the renamed object keep their identity under the new name, so a
// later DROP [IF EXISTS] of the old name removes nothing and ends none of
// them. A routine rename without an argument list moves the entries only
// when the name has one held identity, as x1Remove does.
func (s *state) rekey(kind string, q qname, sig, to string) {
	if s.failure != nil {
		return
	}
	from := depKey(kind, q)
	var hits []*depEntry
	sigs := map[string]bool{}
	for _, e := range s.rev18.entries {
		if !e.dead && e.key == from {
			hits = append(hits, e)
			sigs[e.sig] = true
		}
	}
	if routineKinds[kind] && (len(sigs) > 1 && sig == "" || sig != "" && len(sigs) == 1 && !sigs[sig]) {
		return
	}
	nq := qname{schema: q.schema, name: to}
	for _, e := range hits {
		if routineKinds[kind] && sig != "" && e.sig != sig && len(sigs) > 1 {
			continue
		}
		e.key = depKey(kind, nq)
	}
}

// anyTypeEntry returns a live entry that may depend on any type (Y44).
func (s *state) anyTypeEntry() *depEntry {
	for _, e := range s.rev18.entries {
		if e.anyType && s.live(e) {
			return e
		}
	}
	return nil
}

// dropAliases are the names a dropped object of kind and name q is named
// by (Y39, Y40): every name it has had, its array and multirange names
// (X1's aliases) and, for a table the pass holds, the sequences its
// columns own; a sequence name whose numeric suffix the pass cannot prove
// is matched by every recorded word that starts with the clipped
// <table>_<column>_seq.
func (s *state) dropAliases(kind string, q qname) map[string]bool {
	out := s.rev18.aliases(q.name)
	if kind != "table" {
		return out
	}
	for a := range s.rev19c.seqAliases[q.String()] {
		out[a] = true
	}
	return out
}

// tableSeqAliases reads, before a DROP TABLE runs, the sequences the
// columns of each held table it names own (Y40): the X1 check after the
// statement no longer holds the table.
func (s *state) tableSeqAliases(toks []token, src []byte) {
	s.rev19c.seqAliases = nil
	d, ok := readX1Drop(toks, src)
	if !ok || d.kind != "table" {
		return
	}
	s.rev19c.seqAliases = map[string]map[string]bool{}
	for _, q := range d.names {
		t, _ := s.lookup(q)
		if t == nil {
			continue
		}
		m := map[string]bool{}
		for _, c := range t.columns {
			s.addSeqAliases(m, t, c)
		}
		s.rev19c.seqAliases[q.String()] = m
	}
}

// addSeqAliases adds the names of the sequences column c of t owns (Y40):
// the generated name of a serial or identity column (ChooseRelationName
// from the table and column names at its creation: renames of the table
// or column do not rename it), clipped as makeObjectName does, with every
// recorded word that is that name with a numeric suffix the pass does not
// prove; and the sequences ALTER or CREATE SEQUENCE ... OWNED BY gave it,
// under every name they have had.
func (s *state) addSeqAliases(out map[string]bool, t *table, c *column) {
	for _, n := range s.rev19c.ownedBy[c] {
		for a := range s.rev18.aliases(n) {
			out[a] = true
		}
	}
	if !(c.implied || c.identity != nil) {
		return
	}
	tn, cn := c.seqParts[0], c.seqParts[1]
	if tn == "" {
		tn, cn = t.q.name, c.name
	}
	base, ok := makeObjectName(tn, &cn, "seq")
	if !ok {
		base = seqName(t, c)
	}
	for a := range s.rev18.aliases(base) {
		out[a] = true
	}
	for _, e := range s.rev18.entries {
		for _, m := range []map[string]bool{e.words, e.self, e.onAt, e.refAt} {
			for w := range m {
				d := strings.TrimLeft(w[len(strings.TrimRight(w, "0123456789")):], "")
				if d == "" || !ok {
					continue
				}
				if x, _ := makeObjectName(tn, &cn, "seq"+d); x == w {
					out[w] = true
				}
			}
		}
	}
}

// rev19cState is what the U-c rules keep.
type rev19cState struct {
	// ownedBy are the sequences CREATE or ALTER SEQUENCE ... OWNED BY
	// gave a column (Y40).
	ownedBy map[*column][]string
	// bases are the base type of each set domain and the subtype of each
	// set range, by name (Y52: they go with it).
	bases map[string][]string
	// creations count the statements that create a name in the pg_type
	// namespace (Y52: a name created once is exact).
	creations map[string]int
	// y52 is the DROP ... CASCADE read before it runs.
	y52 *y52Plan
	// seqAliases are the owned sequences of the tables a DROP TABLE
	// names, read before it runs (Y40).
	seqAliases map[string]map[string]bool
	// seq counts the statements the pass applied; changed is the last
	// statement that dropped, renamed or changed the type of an object or
	// column of each name (Y50).
	seq     int
	changed map[string]int
}

// copiedTypes applies Y44 to a statement's entry: a statement that creates
// columns by copying them (CREATE TABLE AS, SELECT INTO, LIKE, INHERITS,
// PARTITION OF, OF t, ALTER TABLE ... INHERIT) depends on the copied
// columns' types, which it does not write. The entry gets the types of
// the copied columns as the pass holds them; when it does not hold them
// (a source not held or partial, a query's columns, which the pass does
// not read; views and materialized views too, whose columns depend on
// their types the same way, heap.c 844-847), the entry may depend on any
// type.
func (s *state) copiedTypes(e *depEntry, a analysis) {
	switch {
	case a.query != "" || a.view:
		e.anyType = true
		return
	}
	var srcs []qname
	if a.create != nil {
		srcs = append(srcs, a.inherits...)
		for _, p := range []*qname{a.create.partitionOf, a.create.likeOf} {
			if p != nil {
				srcs = append(srcs, *p)
			}
		}
		if o := a.create.ofType; o != nil {
			words, ok := s.rev19.typeWords[o.name]
			if !ok {
				e.anyType = true
				return
			}
			for _, w := range words {
				e.words[w], e.self[w] = true, true
			}
		}
	}
	if a.alter != nil {
		for _, act := range a.alter.actions {
			if act.inherit != nil {
				srcs = append(srcs, *act.inherit)
			}
		}
	}
	for _, q := range srcs {
		t, _ := s.lookup(q)
		if t == nil || !factsKnown(t) {
			e.anyType = true
			return
		}
		for _, c := range t.columns {
			words := c.typeNames
			if len(words) == 0 {
				words = textWordList(c.typ)
			}
			for _, w := range words {
				e.words[w], e.self[w] = true, true
			}
		}
	}
}

// y40OwnedBy records CREATE / ALTER SEQUENCE s ... OWNED BY t.c (Y40): the
// sequence becomes a name of column c of the held table t.
func (s *state) y40OwnedBy(toks []token, src []byte) {
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create", "sequence") && !c.acceptKw("alter", "sequence") {
		if c = (&cursor{toks: toks, src: src}); !c.acceptKw("create") {
			return
		}
		for _, w := range []string{"temp", "temporary", "unlogged"} {
			c.acceptKw(w)
		}
		if !c.acceptKw("sequence") {
			return
		}
	}
	c.acceptKw("if", "not", "exists")
	c.acceptKw("if", "exists")
	q, err := c.name()
	if err != nil {
		return
	}
	i := depth0Index(toks, c.i, "owned", "by")
	if i < 0 {
		return
	}
	oc := &cursor{toks: toks, i: i + 2, src: src}
	var parts []string
	for {
		n, err := oc.ident()
		if err != nil {
			return
		}
		parts = append(parts, n)
		if !oc.acceptPunct(".") {
			break
		}
	}
	if len(parts) < 2 {
		return
	}
	tq := qname{name: parts[len(parts)-2]}
	if len(parts) > 2 {
		sch := parts[len(parts)-3]
		tq.schema = &sch
	}
	t, _ := s.lookup(tq)
	if t == nil {
		return
	}
	for _, col := range t.columns {
		if col.name == parts[len(parts)-1] {
			if s.rev19c.ownedBy == nil {
				s.rev19c.ownedBy = map[*column][]string{}
			}
			s.rev19c.ownedBy[col] = append(s.rev19c.ownedBy[col], q.name)
		}
	}
}

// depth0Index returns the index of a word sequence at depth 0, or -1.
func depth0Index(toks []token, from int, words ...string) int {
	for i := from; i < len(toks); i++ {
		if i = depth0(toks, i, words[0]); i < 0 {
			return -1
		}
		ok := i+len(words) <= len(toks)
		for k := 1; ok && k < len(words); k++ {
			ok = toks[i+k].kind == tokIdent && toks[i+k].value == words[k]
		}
		if ok {
			return i
		}
	}
	return -1
}

// y40y41Alter applies Y40 and Y41 to ALTER TABLE t ... DROP COLUMN c,
// ALTER COLUMN c DROP IDENTITY and ALTER COLUMN c TYPE of a held table,
// against the record before the statement: a live entry naming a
// sequence c owns (Y40), or a live SQL-standard body naming t (Y41: it
// depends on every column it uses, also without naming them), makes the
// statement may fail without CASCADE (never refused on that ground: the
// record is a superset). ALTER COLUMN TYPE has no CASCADE.
func (s *state) y40y41Alter(toks []token, a analysis) {
	if a.alter == nil {
		return
	}
	t, _ := s.lookup(a.alter.name)
	if t == nil {
		return
	}
	tnames := s.rev18.aliases(t.q.name)
	sqlBody := func() *depEntry {
		for _, e := range s.rev18.entries {
			if !e.sqlBody || !s.live(e) {
				continue
			}
			for n := range tnames {
				if e.names(n, false, "table") || e.onAt[n] {
					return e
				}
			}
		}
		return nil
	}
	column := func(n string) *column {
		for _, c := range t.columns {
			if c.name == n {
				return c
			}
		}
		return nil
	}
	what := "ALTER TABLE " + t.q.String()
	for _, act := range a.alter.actions {
		col := column(act.name)
		switch {
		case act.kind == "drop_column" && !act.cascade, act.kind == "alter_type":
			if e := sqlBody(); e != nil {
				verb := "DROP COLUMN"
				if act.kind == "alter_type" {
					verb = "ALTER COLUMN TYPE"
				}
				s.unknownFact(fmt.Sprintf("%s %s %s may be refused: the SQL-standard body %q uses the table and may use the column%s", what, verb, act.name, e.text, precondition("M5c.P8")))
				return
			}
		}
		if act.kind == "drop_column" && act.cascade {
			// With CASCADE the routines whose SQL-standard body uses the
			// column, and the objects that use the column's sequence, may
			// be gone (X1's cascade): their names are no longer held.
			s.y41Cascade(tnames, col, t)
			continue
		}
		if col == nil || act.kind != "drop_column" {
			continue
		}
		seqs := map[string]bool{}
		s.addSeqAliases(seqs, t, col)
		if e := s.naming(seqs, false, false, "sequence"); e != nil {
			s.unknownFact(fmt.Sprintf("%s DROP COLUMN %s may be refused: the set's statement %q names the sequence the column owns%s", what, act.name, e.text, precondition("M5c.P8")))
			return
		}
	}
	// ALTER [COLUMN] c DROP IDENTITY [IF EXISTS] drops c's sequence.
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].kind != tokIdent || toks[i].value != "drop" || toks[i+1].kind != tokIdent || toks[i+1].value != "identity" || i < 2 {
			continue
		}
		col := column(toks[i-1].value)
		if col == nil {
			continue
		}
		seqs := map[string]bool{}
		s.addSeqAliases(seqs, t, col)
		if e := s.naming(seqs, false, false, "sequence"); e != nil {
			s.unknownFact(fmt.Sprintf("%s ALTER COLUMN %s DROP IDENTITY may be refused: the set's statement %q names the column's sequence%s", what, col.name, e.text, precondition("M5c.P8")))
			return
		}
	}
}

// y52Plan is a DROP ... CASCADE of types read before it runs (Y52).
type y52Plan struct {
	// names are every name of the dropped types and of the types that go
	// with them (domains and ranges over them), with their arrays and
	// multiranges; exact the subset under which a column's type is
	// certainly a dropped type.
	names, exact map[string]bool
}

// y52Kinds are the DROP kinds whose objects have a type a column can use:
// a relation's row type, a composite type, an enum, a domain or a range.
var y52Kinds = map[string]bool{"table": true, "foreign table": true, "view": true, "materialized view": true, "type": true, "domain": true}

// y52Before reads a DROP ... CASCADE of an object with a type (Y52) before
// it runs: the names of the types that go (the dropped ones, every name
// they have had, their arrays and multiranges, and the domains and ranges
// over them, recursively) and which of them a column names exactly (the
// current name, created once by the set, never renamed, resolved
// certainly).
func (s *state) y52Before(toks []token, src []byte) *y52Plan {
	d, ok := readX1Drop(toks, src)
	if !ok || !d.cascade || !y52Kinds[d.kind] {
		return nil
	}
	p := &y52Plan{names: map[string]bool{}, exact: map[string]bool{}}
	var todo []string
	for _, q := range d.names {
		certain := q.schema != nil || !s.pathChanged
		for a := range s.rev18.aliases(q.name) {
			p.names[a] = true
		}
		if certain && len(s.rev18.links[q.name]) == 0 && s.rev19c.creations[q.name] <= 1 && s.code.may[q.name] == 0 {
			p.exact[q.name] = true
		}
		todo = append(todo, q.name)
	}
	// Domains and ranges over a dropped type go too (their base or subtype
	// is it); composite types lose the attribute and survive.
	for len(todo) > 0 {
		x := todo[0]
		todo = todo[1:]
		for t, base := range s.rev19c.bases {
			if p.names[t] {
				continue
			}
			for _, w := range base {
				if !p.names[w] {
					continue
				}
				for a := range s.rev18.aliases(t) {
					p.names[a] = true
				}
				if p.exact[w] && len(s.rev18.links[t]) == 0 && s.rev19c.creations[t] <= 1 {
					p.exact[t] = true
				}
				todo = append(todo, t)
				break
			}
		}
		_ = x
	}
	return p
}

// y52After applies Y52 after a DROP ... CASCADE the pass applied: every
// held table's column whose type is a type that went is dropped with M5b's
// CASCADE effects; a partition-key column takes its table (M6 CASCADE).
// When the column's type or the dropped type is not exact, the column is
// dropped from the record all the same and the table is partial (its
// facts may differ); a partition key that may use the type makes the
// statement may fail (the table's existence is unknown).
func (s *state) y52After(p *y52Plan) {
	if p == nil || s.failure != nil {
		return
	}
	var whole []qname
	for _, t := range slices.Clone(s.tables) {
		for _, c := range slices.Clone(t.columns) {
			words := c.typeNames
			exactType := len(words) > 0
			if !exactType {
				words = textWordList(c.typ)
			}
			hit, exact := false, false
			for _, w := range words {
				if p.names[w] || p.names[strings.ToLower(w)] {
					hit = true
					exact = exact || p.exact[w]
				}
			}
			if !hit {
				continue
			}
			exact = exact && exactType
			s.touch(t)
			if slices.Contains(t.partitionKey, c.name) {
				if !exact {
					s.unknownFact(fmt.Sprintf("the CASCADE may drop table %s: its partition key column %s may have a dropped type%s", t.q, c.name, precondition("M6.P8")))
					return
				}
				whole = append(whole, t.q)
				break
			}
			if !exact {
				s.partialOnce(t, fmt.Sprintf("the CASCADE may have dropped column %s, whose type may be a dropped type%s", c.name, precondition("R5.P8")))
			}
			s.dropColumnStatement(t, c, true)
		}
	}
	if len(whole) > 0 {
		s.dropTables(&dropObjects{kind: "table", names: whole, cascade: true})
	}
}

// y41Cascade marks the names of what DROP COLUMN ... CASCADE may remove
// (Y40, Y41): routines with a SQL-standard body naming the table, and the
// objects whose entries name the column's sequence.
func (s *state) y41Cascade(tnames map[string]bool, col *column, t *table) {
	r := &s.rev18
	if r.gone == nil {
		r.gone = map[string]string{}
	}
	seqs := map[string]bool{}
	if col != nil {
		s.addSeqAliases(seqs, t, col)
	}
	for _, e := range r.entries {
		if !s.live(e) || e.name == "" {
			continue
		}
		hit := false
		for n := range tnames {
			hit = hit || e.sqlBody && (e.names(n, true, "table") || e.onAt[n])
		}
		for n := range seqs {
			hit = hit || e.names(n, true, "sequence")
		}
		if hit {
			r.gone[e.name] = "cascade"
		}
	}
}

// y49Heads are the PL/pgSQL statements Y49 checks where the body runs.
var y49Heads = map[string]bool{"select": true, "perform": true, "insert": true, "update": true, "delete": true, "values": true, "with": true, "return": true}

// rev19cCode applies Y48, Y49 and Y50 to the code an O1 statement runs:
// a CREATE FUNCTION or PROCEDURE the code runs (from a constant EXECUTE or
// statically) is analysed with the checks it gets at top level (R6.P14's
// compilation, R1b.P5's SQL body); the static statements of the PL/pgSQL
// bodies get the top-level rules for names (N1.P1), columns (N3.P1) and
// reg* arguments (W2); a PL/pgSQL body created before an object or column
// it names was dropped, renamed or retyped is planned again at the call.
// Any of them makes the running statement may fail (a refusal there is
// reported the same way).
func (s *state) rev19cCode(inv inventory) {
	if s.failure != nil {
		return
	}
	if inv.stale != "" {
		s.unknownFact(fmt.Sprintf("the statement may call a set PL/pgSQL routine whose statements are planned at the call: %s%s", inv.stale, precondition("O1.P1")))
		return
	}
	saved := s.m.source
	defer func() { s.m.source = saved }()
	for _, c := range inv.statements {
		if c.protected {
			continue
		}
		if _, b := routineBodyOf(c.st.tokens, c.src); b == nil {
			// Y48: any other statement code runs is judged where it runs
			// (H21), and the pass does not analyse it as a top-level
			// statement there (its preconditions against the state at
			// that point): it is row D, so the running statement may fail.
			s.m.source = saved
			s.unknownFact(fmt.Sprintf("the code may run %q, which the pass does not analyse where it runs (row D)%s", statementText(c.st.tokens, c.src), precondition("O1.P12")))
			return
		}
		s.m.source = c.src
		s.routineCompile(c.st)
		if s.failure == nil && sqlBody(c.st.tokens) && !(s.functionBodies() == cfbOff && stringBody(c.st.tokens, c.src)) && !s.sqlConstantBody(c.st.tokens) {
			s.unknownFact("the code creates a LANGUAGE sql function whose body is not proven" + precondition("O1.P12"))
		}
		if s.failure != nil {
			s.m.source = saved
			s.unknownFact("the code runs a CREATE FUNCTION or PROCEDURE that may be refused (see above)" + precondition("O1.P12"))
			return
		}
	}
	for _, st := range inv.static {
		if st.protected {
			continue
		}
		s.m.source = st.src
		toks := st.st.tokens
		s.regArguments(toks, st.src)
		if s.failure != nil {
			break
		}
		query := toks
		ret := toks[0].value == "return"
		if ret || toks[0].value == "perform" {
			query = append([]token{{kind: tokIdent, value: "select", start: toks[0].start, end: toks[0].end}}, toks[1:]...)
		}
		qa := analyze(statement{tokens: query, start: st.st.start, end: st.st.end}, st.src, nil, true)
		s.tokenRule(statement{tokens: query, start: st.st.start, end: st.st.end}, namesOf(qa, query))
		if s.failure != nil {
			break
		}
		if ret {
			// RETURN expr: the names rule only (its expression reads
			// variables, NEW and OLD, which the column reader does not).
			continue
		}
		s.columnReferences(query, qa)
		if s.failure != nil {
			break
		}
	}
	if s.failure != nil {
		s.m.source = saved
		s.unknownFact("a static statement of the code may be refused (see above)" + precondition("O1.P1"))
	}
}

// y50Changes records the names a statement dropped, renamed or changed
// the type of (Y50): DROP of any object, ALTER ... RENAME, and ALTER
// TABLE's RENAME, DROP COLUMN and ALTER COLUMN TYPE, with the table.
func (s *state) y50Changes(toks []token, src []byte, a analysis) {
	r := &s.rev19c
	if r.changed == nil {
		r.changed = map[string]int{}
	}
	mark := func(n string) {
		if n != "" {
			r.changed[n] = r.seq
			r.changed[strings.ToLower(n)] = r.seq
		}
	}
	if d, ok := readX1Drop(toks, src); ok {
		for _, q := range d.names {
			mark(q.name)
		}
	}
	if a.drop != nil {
		for _, q := range a.drop.names {
			mark(q.name)
		}
	}
	c := &cursor{toks: toks, src: src}
	if c.acceptKw("drop") {
		// Any other DROP: every name it writes.
		for _, t := range toks[1:] {
			if t.kind == tokIdent || t.kind == tokQuoted {
				mark(t.value)
			}
		}
	}
	if a.alter != nil {
		for _, act := range a.alter.actions {
			switch act.kind {
			case "rename_column", "drop_column", "alter_type", "rename_table", "set_schema":
				mark(a.alter.name.name)
				mark(act.name)
			}
		}
	}
	c = &cursor{toks: toks, src: src}
	if !c.acceptKw("alter") || a.alter != nil {
		return
	}
	for _, words := range append(slices.Clone(x1Kinds), []string{"index"}, []string{"trigger"}, []string{"policy"}, []string{"statistics"}) {
		if c.acceptKw(words...) {
			break
		}
	}
	c.acceptKw("if", "exists")
	q, err := c.name()
	if err != nil {
		return
	}
	changes := false
	for i := c.i; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tokIdent {
			continue
		}
		switch t.value {
		case "rename", "drop", "type", "schema":
			changes = true
		case "column", "attribute", "value":
			if i+1 < len(toks) {
				mark(toks[i+1].value)
			}
		}
	}
	if changes {
		mark(q.name)
	}
}

// y50Stale says why a PL/pgSQL body is planned against changed objects
// (Y50): since its creation, a statement the pass applied dropped,
// renamed or retyped an object or column a word of the body names.
func (s *state) y50Stale(b *routineBody) string {
	if b.lang != "plpgsql" || b.text == "" || len(s.rev19c.changed) == 0 {
		return ""
	}
	words := map[string]bool{}
	textWords(b.text, words)
	for w := range words {
		if at, ok := s.rev19c.changed[w]; ok && at > b.born {
			return fmt.Sprintf("%s was dropped, renamed or changed after the routine was created", w)
		}
	}
	return ""
}

// e6Roles applies E6 to the privileges of a role the set created (H23): it
// holds only what the set granted it, which the pass does not record. So
// ALTER ... OWNER TO such a role may fail (the new owner needs CREATE on
// the object's schema, tablecmds.c ATExecChangeOwner and
// AlterObjectOwner_internal; a role the migrating role created has none
// unless the set grants it), and so does every statement but SET, RESET,
// SHOW and transaction control while SET ROLE puts such a role in force
// (its privileges replace the migrating role's).
func (s *state) e6Roles(toks []token, src []byte, a analysis) {
	if s.failure != nil {
		return
	}
	r := &s.rev19b.roles
	if x := r.set[r.current]; r.current != "" && x != nil {
		switch head := toks[0].value; {
		case head == "set", head == "reset", head == "show", head == "discard":
		case txStatementKind(toks):
		default:
			s.unknownFact(fmt.Sprintf("the statement runs as role %s, which the set created: its privileges are not the migrating role's (E6)%s", r.current, precondition("N1.P2")))
			return
		}
	}
	c := &cursor{toks: toks, src: src}
	if c.acceptKw("create", "cast") && c.isPunct("(") {
		// functioncmds.c CreateCast: the migrating role must own the source
		// or the target type. It owns the types the set creates and, under
		// E6, the E1 types; a built-in type is the bootstrap superuser's.
		end, err := c.group()
		if err != nil {
			return
		}
		owned := false
		for _, t := range toks[c.i+1 : end-1] {
			if t.kind != tokIdent && t.kind != tokQuoted {
				continue
			}
			if _, ok := s.rev19.typeWords[t.value]; ok || s.names.creates(t.value, kindType|kindRelation) || e1Types[t.value] {
				owned = true
			}
		}
		if !owned {
			s.unknownFact("CREATE CAST may be refused: the migrating role must own the source or the target type, and the set created neither (E6)" + precondition("N1.P4"))
		}
		return
	}
	if !c.peekKw("alter") {
		return
	}
	i := depth0Index(toks, 1, "owner", "to")
	if i < 0 {
		return
	}
	oc := &cursor{toks: toks, i: i + 2, src: src}
	n, ok := r.roleName(oc)
	if !ok || r.set[n] == nil {
		return
	}
	id := "N1.P4"
	switch {
	case c.peekKw("alter", "table"), c.peekKw("alter", "foreign", "table"):
		id = "M5k-b.P4"
	case c.peekKw("alter", "function"), c.peekKw("alter", "procedure"), c.peekKw("alter", "routine"), c.peekKw("alter", "aggregate"):
		id = "R6.P4"
	case c.peekKw("alter", "type"), c.peekKw("alter", "domain"):
		id = "R4.P4"
	}
	s.unknownFact(fmt.Sprintf("OWNER TO %s may be refused: the set created the role, and the new owner needs CREATE on the object's schema, which E6 does not grant it%s", n, precondition(id)))
}

// txStatementKind reports transaction control (BEGIN, COMMIT, ...).
func txStatementKind(toks []token) bool {
	kind, _, _ := txStatement(toks, nil)
	return kind != txOther && kind != txPrevent
}

// protectedBody is a PL/pgSQL body of one block whose only handler is
// EXCEPTION WHEN OTHERS THEN with RAISE NOTICE (or NULL) statements: any
// error its statements raise is caught (pl_exec.c exec_stmt_block), so
// they cannot fail the code that runs it.
var protectedBody = regexp.MustCompile(`(?is)^\s*(?:declare\b[^$]*?)?begin\b(.*)\bexception\s+when\s+others\s+then\s+((?:raise\s+notice\s+'(?:[^']|'')*'(?:\s*,\s*[a-z_]+)*\s*;\s*|null\s*;\s*)+)end\s*;?\s*$`)

// plpgsqlProtected reports a protected body (protectedBody) whose block
// holds no nested BEGIN or EXCEPTION.
func plpgsqlProtected(body string) bool {
	m := protectedBody.FindStringSubmatch(body)
	if m == nil {
		return false
	}
	inner := strings.ToLower(m[1])
	return !regexp.MustCompile(`\b(?:begin|exception)\b`).MatchString(inner)
}

// internalTriggers reports whether ALTER TABLE t's table may have internal
// triggers (Y51): it may unless the pass holds it under a certain name
// with its facts known, and it has no foreign key, no deferrable key and
// no foreign key of a held table references it.
func (s *state) internalTriggers(toks []token, src []byte) bool {
	c := &cursor{toks: toks, i: 2, src: src}
	c.acceptKw("if", "exists")
	c.acceptKw("only")
	q, err := c.name()
	if err != nil {
		return true
	}
	t, certain := s.heldTable(q)
	if t == nil || !certain || !factsKnown(t) || t.keysUnknown || len(t.foreignKeys) > 0 {
		return true
	}
	keys := append([]*key{}, t.uniques...)
	if t.primaryKey != nil {
		keys = append(keys, t.primaryKey)
	}
	for _, k := range keys {
		if k.deferrable {
			return true
		}
	}
	for _, o := range s.tables {
		for _, fk := range o.foreignKeys {
			if fk.table == t || fk.table == nil && fk.written.table.name == t.q.name {
				return true
			}
		}
	}
	return false
}

// sqlStandardBody reports a CREATE FUNCTION or PROCEDURE with a
// SQL-standard body (BEGIN ATOMIC ... END or RETURN expr), which is a
// LANGUAGE sql body whatever the LANGUAGE clause says: Postgres parses and
// analyses it when the routine is created, whatever check_function_bodies
// says (functioncmds.c interpret_AS_clause, pg_proc.c), so R1b.P5 applies
// to it (found with Y40's g9 production: a body's nextval of a missing
// sequence, and columns a body names, were claimed).
func sqlStandardBody(toks []token, src []byte) bool {
	_, b := routineBodyOf(toks, src)
	return b != nil && b.toks != nil
}

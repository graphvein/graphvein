package schema

import (
	"fmt"
	"strings"
)

// Object names that N1 statements create (T018a2 Revision 1.4, class P2,
// precondition N1.P2). Postgres refuses a CREATE whose name is already
// taken in its namespace: a policy per table (policy.c CreatePolicy,
// 42710), a trigger per table (trigger.c CreateTrigger, 42710), a role
// per cluster for ROLE, USER and GROUP alike (user.c CreateRole, 42710), a
// publication per database (publicationcmds.c CreatePublication, 42710),
// and a collation, aggregate, operator or cast in their own catalogs
// (42710, 42723). E1 covers only names the set never creates, so a name is
// free until the set itself creates it; after that it stays taken until a
// statement the pass reads drops or renames it away. A drop the pass does
// not read leaves the name recorded (the CREATE stays not proven); a
// rename it does not read may take any name of that kind, so every later
// CREATE of the kind is not proven.

// n1Kinds maps the keyword after CREATE, ALTER or DROP to the namespace
// kind it creates names in. ROLE, USER and GROUP share pg_authid.
var n1Kinds = map[string]string{
	"policy": "policy", "role": "role", "user": "role", "group": "role",
	"publication": "publication", "collation": "collation",
	"aggregate": "aggregate", "operator": "operator", "cast": "cast",
}

// n1Exact are the kinds whose recorded names identify the object exactly,
// so a name met again is a refusal. Collations, aggregates, operators and
// casts are identified by schema or signature as well, which the record
// does not compare: a name met again is only not proven.
var n1Exact = map[string]bool{"policy": true, "role": true, "publication": true, "trigger": true}

// n1Names records the names N1 statements created on objects the pass does
// not hold as tables (outside tables, roles, publications, ...). Names of
// policies and triggers on tables the set holds live on the table.
type n1Names struct {
	// names maps kind, target and name (n1Key) to true while taken.
	names map[string]bool
	// renamed holds the kinds a statement the pass could not read may
	// have renamed some object of to any name.
	renamed map[string]bool
}

func n1Key(kind, target, name string) string { return kind + "\x00" + target + "\x00" + name }

// taken reports whether the set's record holds the name exactly (same
// target text) or possibly (the same name on a target of the same bare
// name, or a kind renamed by an unread statement).
func (n *n1Names) taken(kind string, target qname, name string) (exact, maybe bool) {
	if n.names[n1Key(kind, target.String(), name)] {
		return true, true
	}
	if n.renamed[kind] {
		return false, true
	}
	suffix := "\x00" + name
	for k := range n.names {
		if !strings.HasPrefix(k, kind+"\x00") || !strings.HasSuffix(k, suffix) {
			continue
		}
		t := strings.TrimSuffix(strings.TrimPrefix(k, kind+"\x00"), suffix)
		if bareName(t) == target.name {
			return false, true
		}
	}
	return false, false
}

func (n *n1Names) add(kind string, target qname, name string) {
	if n.names == nil {
		n.names = map[string]bool{}
	}
	n.names[n1Key(kind, target.String(), name)] = true
}

// markRenamed records that an unread statement may have renamed an
// object of the kind to any name.
func (n *n1Names) markRenamed(kind string) {
	if n.renamed == nil {
		n.renamed = map[string]bool{}
	}
	n.renamed[kind] = true
}

func (n *n1Names) remove(kind string, target qname, name string) {
	delete(n.names, n1Key(kind, target.String(), name))
}

// bareName is the last part of a written target name.
func bareName(t string) string {
	if i := strings.LastIndexByte(t, '.'); i >= 0 {
		return t[i+1:]
	}
	return t
}

// n1Create applies N1.P2 to CREATE POLICY, ROLE, USER, GROUP, PUBLICATION,
// COLLATION, AGGREGATE, OPERATOR and CAST. A form it cannot read reports
// nothing new (the statement's other preconditions still apply).
func (s *state) n1Create(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	if !c.acceptKw("create") {
		return
	}
	replace := c.acceptKw("or", "replace")
	if c.done() {
		return
	}
	word := c.tok().value
	if word == "trigger" || word == "policy" || c.peekKw("constraint", "trigger") {
		// U8: trigger and policy names are a ColId (gram.y name).
		at := c.i + 1
		if word == "constraint" {
			at++
		}
		if at < len(c.toks) && c.toks[at].kind == tokIdent {
			if v := c.toks[at].value; pgKeywordCategories[v] == 'R' || pgKeywordCategories[v] == 'T' {
				s.unknownFact(fmt.Sprintf("CREATE %s %s: keyword %s %s (gram.y ColId)%s", strings.ToUpper(word), v, v, keywordNameError, precondition("N1.P7")))
				return
			}
		}
	}
	kind, ok := n1Kinds[word]
	if !ok || c.tok().kind != tokIdent || n1Other(c) {
		return
	}
	c.i++
	what := "CREATE " + strings.ToUpper(word)
	switch kind {
	case "policy":
		name, err := c.ident()
		if err != nil || !c.acceptKw("on") {
			return
		}
		q, err := c.name()
		if err != nil {
			return
		}
		s.tableObjectName("policy", q, name, false, fmt.Sprintf("CREATE POLICY %s on %s", name, q))
	case "cast":
		// The cast is named by its two types: the text inside the
		// parentheses stands for it.
		if !c.acceptPunct("(") {
			return
		}
		start := c.i
		for !c.done() && !c.isPunct(")") {
			c.i++
		}
		if c.done() {
			return
		}
		s.globalObjectName(kind, words(toks[start:c.i]), false, what)
	case "operator":
		// An operator name is punctuation, optionally schema-qualified;
		// the text up to its parenthesised definition stands for it.
		start := c.i
		for !c.done() && !c.isPunct("(") {
			c.i++
		}
		if c.done() || c.i == start {
			return
		}
		s.globalObjectName(kind, strings.ReplaceAll(words(toks[start:c.i]), " ", ""), false, what)
	default:
		if kind == "collation" {
			c.acceptKw("if", "not", "exists")
		}
		q, err := c.name()
		if err != nil {
			return
		}
		s.globalObjectName(kind, q.String(), replace, what)
	}
}

// n1Other reports the forms whose keyword is a kind of n1Kinds but which
// name other objects: OPERATOR CLASS and FAMILY, USER MAPPING.
func n1Other(c *cursor) bool {
	return c.isKw(0, "operator") && (c.isKw(1, "class") || c.isKw(1, "family")) || c.isKw(0, "user") && c.isKw(1, "mapping")
}

// words joins the token values of toks with spaces.
func words(toks []token) string {
	b := make([]string, len(toks))
	for i, t := range toks {
		b[i] = t.value
	}
	return strings.Join(b, " ")
}

// globalObjectName checks and records a name in a namespace that is not
// per table: the same written name is refused for roles and publications;
// a name of the same last part (another schema or signature) is not
// proven, as is any name of a kind an unread statement may have renamed.
func (s *state) globalObjectName(kind, name string, replace bool, what string) {
	exact := s.n1.names[n1Key(kind, "", name)]
	maybe := exact || s.n1.renamed[kind]
	for k := range s.n1.names {
		if strings.HasPrefix(k, kind+"\x00\x00") && bareName(strings.TrimPrefix(k, kind+"\x00\x00")) == bareName(name) {
			maybe = true
		}
	}
	switch {
	case replace:
	case exact && n1Exact[kind]:
		s.unknownFact(fmt.Sprintf("%s is refused: the set already created %s %s%s", what, kind, name, precondition("N1.P2")))
	case maybe:
		s.unknownFact(fmt.Sprintf("%s may be refused: a %s %s created by the set may exist%s", what, kind, name, precondition("N1.P2")))
	}
	s.n1.add(kind, qname{}, name)
}

// tableObjectName applies the per-table namespace of policies and
// triggers: on a table whose facts are known the names the set wrote are
// exact, on any other table the set holds the name is not proven free,
// and on a table the pass does not hold the set's own record of names on
// that target applies.
func (s *state) tableObjectName(kind string, q qname, name string, replace bool, what string) {
	if exact, _ := s.lookup(q); exact == nil {
		e, maybe := s.n1.taken(kind, q, name)
		switch {
		case replace:
		case e:
			s.unknownFact(fmt.Sprintf("%s is refused: the set already created %s %s on %s%s", what, kind, name, q, precondition("N1.P2")))
		case maybe:
			s.unknownFact(fmt.Sprintf("%s may be refused: a %s %s may exist on the table%s", what, kind, name, precondition("N1.P2")))
		}
		s.n1.add(kind, q, name)
		return
	}
	t := s.scope(q, q.name).t
	names, unknown := t.objectNames(kind)
	if t == nil || *unknown {
		if !replace {
			s.unknownFact(fmt.Sprintf("%s may be refused: a %s %s may exist on the table%s", what, kind, name, precondition("N1.P2")))
		}
		return
	}
	if *names == nil {
		*names = map[string]bool{}
	}
	if (*names)[name] && !replace {
		s.unknownFact(fmt.Sprintf("%s is refused: the table has a %s %s%s", what, kind, name, precondition("N1.P2")))
	}
	(*names)[name] = true
}

// objectNames returns the name record of a per-table kind; on a nil table
// it returns a record that reads as unknown.
func (t *table) objectNames(kind string) (*map[string]bool, *bool) {
	if t == nil {
		unknown := true
		return new(map[string]bool), &unknown
	}
	if kind == "trigger" {
		return &t.triggerNames, &t.triggerNamesUnknown
	}
	return &t.policyNames, &t.policyNamesUnknown
}

// n1DropOrRename keeps the record exact through the drops and renames the
// pass reads: DROP POLICY [IF EXISTS] n ON t, ALTER POLICY n ON t RENAME
// TO m, DROP ROLE | USER | GROUP | PUBLICATION [IF EXISTS] names, and
// ALTER ROLE | USER | GROUP | PUBLICATION | COLLATION | AGGREGATE ...
// RENAME TO m. Any other ALTER that renames makes the kind's names
// unknown; any other DROP leaves the record as it is.
func (s *state) n1DropOrRename(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	drop := c.acceptKw("drop")
	if !drop && !c.acceptKw("alter") {
		return
	}
	if c.done() || c.tok().kind != tokIdent {
		return
	}
	word := c.tok().value
	if word == "trigger" || word == "policy" || c.peekKw("constraint", "trigger") {
		// U8: trigger and policy names are a ColId (gram.y name).
		at := c.i + 1
		if word == "constraint" {
			at++
		}
		if at < len(c.toks) && c.toks[at].kind == tokIdent {
			if v := c.toks[at].value; pgKeywordCategories[v] == 'R' || pgKeywordCategories[v] == 'T' {
				s.unknownFact(fmt.Sprintf("CREATE %s %s: keyword %s %s (gram.y ColId)%s", strings.ToUpper(word), v, v, keywordNameError, precondition("N1.P7")))
				return
			}
		}
	}
	kind, ok := n1Kinds[word]
	if !ok || n1Other(c) {
		return
	}
	c.i++
	renames := false
	for _, t := range toks {
		if t.kind == tokIdent && t.value == "rename" {
			renames = true
		}
	}
	if !drop && !renames {
		return
	}
	switch {
	case kind == "policy":
		if drop {
			c.acceptKw("if", "exists")
		}
		name, err1 := c.ident()
		var q qname
		var err2 error = fmt.Errorf("no ON")
		if c.acceptKw("on") {
			q, err2 = c.name()
		}
		var to string
		if drop {
			c.acceptKw("cascade")
			c.acceptKw("restrict")
		} else if c.acceptKw("rename", "to") {
			to, _ = c.ident()
		}
		read := err1 == nil && err2 == nil && c.done() && (drop || to != "")
		s.movePolicyName(q, name, to, read, drop)
	case drop && (kind == "role" || kind == "publication"):
		c.acceptKw("if", "exists")
		var names []string
		for {
			n, err := c.ident()
			if err != nil {
				return
			}
			names = append(names, n)
			if !c.acceptPunct(",") {
				break
			}
		}
		if kind == "publication" {
			c.acceptKw("cascade")
			c.acceptKw("restrict")
		}
		if !c.done() {
			return
		}
		for _, n := range names {
			s.n1.remove(kind, qname{}, n)
		}
	case !drop:
		// ALTER <kind> <name> [(args)] RENAME TO m: the new name is taken.
		n := len(toks)
		if n >= 3 && toks[n-3].kind == tokIdent && toks[n-3].value == "rename" && toks[n-2].kind == tokIdent && toks[n-2].value == "to" &&
			(toks[n-1].kind == tokIdent || toks[n-1].kind == tokQuoted) {
			old, err := c.name()
			if err == nil && n1Exact[kind] && c.i == n-3 {
				s.n1.remove(kind, qname{}, old.String())
			}
			to := toks[n-1].value
			if err == nil && old.schema != nil {
				to = *old.schema + "." + to
			}
			s.n1.add(kind, qname{}, to)
			return
		}
		s.n1.markRenamed(kind)
	}
}

// movePolicyName applies a read (or unread) DROP POLICY or ALTER POLICY
// RENAME to the names of the table it names.
func (s *state) movePolicyName(q qname, name, to string, read, drop bool) {
	if !read {
		if drop {
			return
		}
		for _, t := range s.tables {
			t.policyNamesUnknown = true
		}
		s.n1.markRenamed("policy")
		return
	}
	if exact, _ := s.lookup(q); exact == nil {
		s.n1.remove("policy", q, name)
		if !drop {
			s.n1.add("policy", q, to)
		}
		return
	}
	t := s.scope(q, q.name).t
	if t == nil {
		if exact, _ := s.lookup(q); exact != nil && !drop {
			exact.policyNamesUnknown = true
		}
		return
	}
	delete(t.policyNames, name)
	if !drop {
		if t.policyNames == nil {
			t.policyNames = map[string]bool{}
		}
		t.policyNames[to] = true
	}
}

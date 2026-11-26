package schema

import (
	"fmt"
	"strings"
)

// Object kinds E1 does not cover (T018a2 Revision 1.6 V4, class P1). E1
// lets the set use roles, contrib extensions and their objects, schemas,
// tables, server files, functions and operators it does not create. It
// does not cover indexes, sequences, views, materialized views, triggers,
// rules, policies, constraints, statistics objects, publications,
// collations other than the built-in ones, or types other than E1's. A
// statement whose success needs such an object is applied only when the
// pass holds it under that name and of that kind, or the statement says IF
// EXISTS; otherwise it may fail (never a refusal: an outside table may
// carry an index, trigger or constraint of any name).

// tableObjectHeld reports whether the pass holds a trigger or policy of
// that name on the table q names (W1): the table resolves certainly with
// its facts known, and its record of names of the kind is exact. The pass
// holds no rule (CREATE RULE is row D).
func (s *state) tableObjectHeld(kind string, q qname, name string) bool {
	if kind != "trigger" && kind != "policy" {
		return false
	}
	t := s.scope(q, q.name).t
	if t == nil {
		return false
	}
	names, unknown := t.objectNames(kind)
	return !*unknown && (*names)[name]
}

// heldObject reports whether the pass holds q as an object of kind
// (index, sequence, view, materialized view, type).
func (s *state) heldObject(q qname, kind string) bool {
	if s.code.gone[q.name] {
		return false
	}
	switch kind {
	case "index":
		return s.holds(q.name, kindIndex)
	case "type":
		return s.holds(q.name, kindType) || e1Types[q.name]
	}
	for k, r := range s.relations {
		if r.uncertain || r.kind != kind {
			continue
		}
		if q.schema != nil && k == *q.schema+"."+q.name || q.schema == nil && (k == q.name || strings.HasSuffix(k, "."+q.name)) {
			return true
		}
	}
	if kind == "sequence" {
		for _, t := range s.tables {
			for _, c := range t.columns {
				if (c.implied || c.identity != nil) && seqName(t, c) == q.name {
					return true
				}
			}
		}
	}
	return false
}

// objectKinds applies V4 to one statement.
func (s *state) objectKinds(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	need := func(q qname, kind, what, id string) bool {
		if s.heldObject(q, kind) {
			return false
		}
		s.unknownFact(fmt.Sprintf("%s may be refused: %s is not a %s the pass holds, and E1 does not cover objects of that kind%s", what, q, kind, precondition(id)))
		return true
	}
	onTable := func(name string, q qname, kind, what, id string) {
		if t, _ := s.target(q); t != nil {
			// W1 (Revision 1.7): on a held table the name must be one the
			// pass holds there (trigger.c get_trigger_oid, policy.c
			// get_relation_policy_oid, rewriteSupport.c get_rewrite_oid:
			// 42704 otherwise).
			if !s.tableObjectHeld(kind, q, name) {
				s.unknownFact(fmt.Sprintf("%s may be refused: the pass does not hold %s %s on %s (a name never created there, dropped or renamed away, created by code, or on a table whose names are not known)%s", what, kind, name, q, precondition(id)))
			}
			return
		}
		s.unknownFact(fmt.Sprintf("%s may be refused: %s %s is on %s, a table outside the set, and E1 does not cover its %ss%s", what, kind, name, q, kind, precondition(id)))
	}
	relKinds := []struct {
		words []string
		kind  string
	}{
		{[]string{"materialized", "view"}, "materialized view"}, {[]string{"view"}, "view"},
		{[]string{"sequence"}, "sequence"}, {[]string{"index"}, "index"},
	}
	kindAt := func() string {
		for _, k := range relKinds {
			if c.acceptKw(k.words...) {
				return k.kind
			}
		}
		return ""
	}
	switch {
	case c.peekKw("alter", "type"), c.peekKw("alter", "domain"):
		// V4 (2026-11-07): types other than E1's are not covered, and the
		// pass does not hold a domain's constraints.
		kind := c.toks[1].value
		c.i += 2
		q, err := c.name()
		if err != nil {
			return
		}
		if kind == "type" && (c.peekKw("add", "value") || c.peekKw("rename", "value")) {
			// The enum reader decides (R3.P1).
			return
		}
		held := s.heldObject(q, "type")
		if kind == "domain" {
			held = !s.code.gone[q.name] && s.holds(q.name, kindType)
		}
		if !held {
			s.unknownFact(fmt.Sprintf("ALTER %s %s may be refused: %s is not a type the pass holds, and E1 does not cover types of that kind%s", strings.ToUpper(kind), q, q, precondition("R4.P1")))
			return
		}
		if kind == "type" {
			return
		}
		switch {
		case c.acceptKw("drop", "constraint"):
			if c.peekKw("if", "exists") {
				return
			}
		case c.acceptKw("validate", "constraint"), c.acceptKw("rename", "constraint"):
		default:
			return
		}
		if n, err := c.ident(); err == nil {
			s.unknownFact(fmt.Sprintf("ALTER DOMAIN %s may be refused: constraint %s is not one the pass holds, and E1 does not cover constraints%s", q, n, precondition("R4.P1")))
		}
	case c.peekKw("alter", "policy"):
		// W1: ALTER POLICY needs the policy (policy.c AlterPolicy 887,
		// rename_policy 1166).
		c.i += 2
		n, err := c.ident()
		if err != nil || !c.acceptKw("on") {
			return
		}
		if q, err := c.name(); err == nil {
			onTable(n, q, "policy", "ALTER POLICY "+n, "N1.P1")
		}
	case c.acceptKw("alter"):
		kind := kindAt()
		if kind == "" || kind == "index" {
			// ALTER INDEX is M8.P1's (alterIndex).
			return
		}
		if c.acceptKw("if", "exists") {
			return
		}
		q, err := c.name()
		if err != nil {
			return
		}
		id := "R4.P1"
		if kind == "index" {
			id = "M8.P1"
		}
		need(q, kind, "ALTER "+strings.ToUpper(kind)+" "+q.String(), id)
	case c.acceptKw("drop"):
		switch {
		case c.peekKw("trigger"), c.peekKw("policy"), c.peekKw("rule"):
			kind := c.tok().value
			c.i++
			if c.acceptKw("if", "exists") {
				return
			}
			n, err := c.ident()
			if err != nil || !c.acceptKw("on") {
				return
			}
			q, err := c.name()
			if err != nil {
				return
			}
			onTable(n, q, kind, "DROP "+strings.ToUpper(kind)+" "+n, "N1.P1")
			return
		}
		kind := kindAt()
		if kind == "" {
			switch {
			case c.acceptKw("type"), c.acceptKw("domain"):
				kind = "type"
			default:
				return
			}
		}
		if kind == "index" || c.acceptKw("if", "exists") {
			// DROP INDEX is M7.P1's.
			return
		}
		for _, q := range dropNames(c) {
			if kind != "type" && s.holds(q.name, kindRelation) && !s.heldObject(q, kind) {
				// Held as another relation kind: R5.P4's kind rule decides.
				continue
			}
			if need(q, kind, "DROP "+strings.ToUpper(kind)+" "+q.String(), "R5.P1") {
				return
			}
		}
	case c.acceptKw("comment", "on"):
		switch {
		case c.peekKw("trigger"), c.peekKw("policy"), c.peekKw("rule"), c.peekKw("constraint"):
			kind := c.tok().value
			c.i++
			n, err := c.ident()
			if err != nil || !c.acceptKw("on") || c.peekKw("domain") {
				return
			}
			q, err := c.name()
			if err != nil {
				return
			}
			if kind == "constraint" {
				if t, _ := s.target(q); t != nil && s.constraintNameHeld(t, n) {
					return
				} else if t != nil {
					s.unknownFact(fmt.Sprintf("COMMENT ON CONSTRAINT %s may be refused: no constraint of that name is held on %s%s", n, q, precondition("N1.P1")))
					return
				}
			}
			onTable(n, q, kind, "COMMENT ON "+strings.ToUpper(kind)+" "+n, "N1.P1")
			return
		}
		kind := kindAt()
		if kind == "" {
			return
		}
		if q, err := c.name(); err == nil {
			need(q, kind, "COMMENT ON "+strings.ToUpper(kind)+" "+q.String(), "N1.P1")
		}
	case c.peekKw("grant"), c.peekKw("revoke"):
		on := depth0(toks, 1, "on")
		if on < 0 || !(&cursor{toks: toks, i: on + 1}).peekKw("sequence") {
			return
		}
		c.i = on + 2
		for {
			q, err := c.name()
			if err != nil {
				return
			}
			if need(q, "sequence", strings.ToUpper(toks[0].value)+" ON SEQUENCE "+q.String(), "N1.P1") || !c.acceptPunct(",") {
				return
			}
		}
	case c.acceptKw("refresh", "materialized", "view"):
		c.acceptKw("concurrently")
		if q, err := c.name(); err == nil {
			need(q, "materialized view", "REFRESH MATERIALIZED VIEW "+q.String(), "N3.P1")
		}
	case c.acceptKw("reindex", "index"):
		c.acceptKw("concurrently")
		if q, err := c.name(); err == nil {
			need(q, "index", "REINDEX INDEX "+q.String(), "N3.P1")
		}
	case c.acceptKw("cluster"):
		c.acceptKw("verbose")
		first, err := c.name()
		if err != nil {
			return
		}
		switch {
		case c.acceptKw("using"):
			if n, err := c.ident(); err == nil {
				need(qname{schema: first.schema, name: n}, "index", "CLUSTER ... USING "+n, "N3.P1")
			}
		case c.acceptKw("on"):
			need(first, "index", "CLUSTER "+first.String()+" ON", "N3.P1")
		}
	}
}

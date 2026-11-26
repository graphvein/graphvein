package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Dependency drops (Revision 3 class 1). Postgres records dependencies of
// columns on their types (and through them on domains, row types of
// tables, views, materialized views, foreign tables and composite types),
// on collations, and of defaults, generation expressions, CHECK and
// EXCLUDE constraints and indexes on the functions, operators, types,
// sequences and collations they use. DROP ... CASCADE removes the
// dependent columns, defaults, constraints and indexes; without CASCADE
// Postgres refuses the drop while any dependent exists. Graphvein does not
// track these dependencies exactly; it uses one broad rule for CASCADE
// (any table that could depend is partial) and refuses only the
// dependencies it can name.

// mayDependOn reports whether anything in t could depend on an object a
// DROP ... CASCADE removes: a column whose written type is not built in
// (an enum, range, domain, base type or row type of any relation), a
// column with a default, generation expression, serial or identity
// sequence or explicit collation, a CHECK or EXCLUDE constraint, an index
// that is not a plain btree over columns, or any identifier that names a
// dropped object. Unsure is yes.
func (t *table) mayDependOn(names []qname) bool {
	mentions := func(list []string) bool {
		for _, n := range names {
			if slices.Contains(list, n.name) {
				return true
			}
		}
		return false
	}
	if t.hasChecks || t.ofType != nil {
		return true
	}
	for _, c := range t.columns {
		if !builtinType(c.typeNames) || c.def != nil || c.generated != nil || c.identity != nil || c.implied || c.collation != nil {
			return true
		}
		if mentions(c.typeNames) || mentions(c.exprNames) {
			return true
		}
	}
	for _, ix := range t.indexes {
		if ix.using != nil || ix.where != nil || slices.Contains(ix.columns, nil) {
			return true
		}
		for _, el := range ix.elements {
			// "a" is a bare column; "a text_pattern_ops", "a COLLATE x"
			// or "a DESC" use an operator class, collation or ordering.
			if len(el.names) > 1 || mentions(el.names) {
				return true
			}
		}
	}
	for _, fk := range t.foreignKeys {
		for _, n := range names {
			if fk.written.table.name == n.name {
				return true
			}
		}
	}
	return false
}

// typeNamed reports whether the written type of c names q: its last word,
// with a schema that does not contradict q's.
func typeNamed(c *column, q qname) bool {
	// A built-in name ("date") written unqualified means pg_catalog's type
	// (pg_catalog is searched first), not a relation of that name.
	if len(c.typeNames) == 0 || !slices.Contains(c.typeNames, q.name) || builtinTypeWords[q.name] {
		return false
	}
	if q.schema != nil && len(c.typeNames) > 1 {
		i := slices.Index(c.typeNames, q.name)
		if i > 0 && c.typeNames[i-1] != *q.schema && !builtinTypeWords[c.typeNames[i-1]] {
			return false
		}
	}
	return true
}

// usesObject names the column of t that surely uses the object, or "".
// It covers what source shows exactly: a column type that names a type,
// domain or the row type of a relation; a collation; a function in a
// default, generation expression, index or named CHECK; a sequence named
// in a default or generated for a serial or identity column.
func (t *table) usesObject(kind string, q qname) string {
	for _, c := range t.columns {
		switch kind {
		case "type", "domain", "view", "materialized view", "foreign table", "table":
			if typeNamed(c, q) {
				return c.name
			}
		case "collation":
			if c.collation != nil && (*c.collation == q.name || *c.collation == q.String()) {
				return c.name
			}
		case "function", "procedure", "routine", "aggregate":
			if slices.Contains(c.exprNames, q.name) {
				return c.name
			}
		case "sequence":
			if c.def != nil && (strings.Contains(*c.def, "'"+q.name+"'") || strings.Contains(*c.def, "."+q.name+"'")) {
				return c.name
			}
			if (c.implied || c.identity != nil) && q.name == t.q.name+"_"+c.name+"_seq" {
				return c.name
			}
		}
	}
	return ""
}

// dropDependencies applies the dependency rules of a DROP of named
// objects of kind; skip are tables the statement drops itself. With
// CASCADE every table that may depend on them is partial; without it a
// known use makes Postgres refuse the drop (the migration may fail).
func (s *state) dropDependencies(kind string, names []qname, cascade bool, skip []*table) {
	what := strings.ToUpper(kind)
	var written []string
	for _, q := range names {
		written = append(written, q.String())
	}
	for _, t := range slices.Clone(s.tables) {
		if slices.Contains(skip, t) {
			continue
		}
		if t.ofType != nil && kind == "type" && slices.ContainsFunc(names, func(q qname) bool { return q.name == t.ofType.name && sameSchema(q.schema, t.ofType.schema) }) {
			// Revision 4 G-d: a typed table depends on its type; CASCADE
			// drops the table.
			if !cascade {
				s.refuse(t, fmt.Sprintf("DROP TYPE %s without CASCADE is refused while this table is OF it (Postgres refuses)", t.ofType))
				continue
			}
			s.touch(t)
			var owners []*table
			for _, fk := range s.referencing(t, func(fk *foreignKey) bool { return fk.table == t }) {
				if o := s.owner(fk); o != nil && o != t {
					owners = append(owners, o)
					s.partialOnce(o, fmt.Sprintf("DROP TYPE %s CASCADE dropped table %s and the foreign keys to it", t.ofType, t.q))
				}
				s.dropForeignKey(fk)
			}
			s.remove(t, owners)
			continue
		}
		if cascade {
			if t.mayDependOn(names) {
				s.partialOnce(t, fmt.Sprintf("DROP %s %s CASCADE may have removed a column, default, constraint or index of this table that depends on it", what, strings.Join(written, ", ")))
			}
			continue
		}
		for _, q := range names {
			if col := t.usesObject(kind, q); col != "" {
				s.refuse(t, fmt.Sprintf("DROP %s %s without CASCADE is refused while column %s uses it (Postgres refuses)", what, q, col))
				break
			}
		}
	}
}

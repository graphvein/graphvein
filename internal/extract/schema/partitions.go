package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Unique indexes on partitioned tables (T018a2 Revision 1.5 U4, class
// P4). Postgres refuses a unique index, primary key or UNIQUE constraint
// on a partitioned table unless it can match every partition key column to
// a key column of the index with the same collation and equality operator
// (indexcmds.c DefineIndex REL_17_6 960-1090, 0A000): the index must use
// btree, the partition key must have no expressions, and every partition
// key column must be a key column (INCLUDE columns do not count). Postgres
// checks the same for every partition that is itself partitioned, because
// it creates the index there too, and for a sub-partitioned PARTITION OF
// child, which receives its parent's unique indexes.
//
// The pass proves the match only for a bare column written without an
// operator class or COLLATE, whose default btree class and collation are
// those of a plain partition key column; anything else is not proven.

// uniqueKey is one unique index Postgres would create: its key columns in
// order (empty for an element that is not a bare column), whether each is
// written bare, and its access method.
type uniqueKey struct {
	what    string
	columns []string
	bare    []bool
	method  string
	exclude bool
}

// partitioningUnknown reports whether the pass cannot tell whether t is
// partitioned or what its key is: the relation of the name may not be the
// one its CREATE TABLE wrote (P3), or its kind is unknown.
func partitioningUnknown(t *table) bool {
	return t.mayBeOther || t.kindUnknown
}

// partitionedTree returns t and the tables the pass holds below it whose
// partition key a unique index on t must include: t when it is
// partitioned, and its partitions that are themselves partitioned, at any
// depth.
func (s *state) partitionedTree(t *table) []*table {
	var out []*table
	seen := map[*table]bool{}
	var walk func(p *table)
	walk = func(p *table) {
		if seen[p] {
			return
		}
		seen[p] = true
		if p.partitionKey != nil {
			out = append(out, p)
		}
		for _, c := range s.tables {
			if c.partitionOf == p {
				walk(c)
			}
		}
	}
	walk(t)
	return out
}

// partitionColumn returns the current name of partition key column n of
// p: a column renamed since PARTITION BY keeps its place in the key
// (inPartitionKey). ok is false when the pass does not know it.
func partitionColumn(p *table, n string) (string, bool) {
	if p.partitionOf != nil && p.columns == nil {
		// A PARTITION OF child: its columns are its parent's, whose
		// renames the pass does not follow onto the child.
		for _, c := range p.partitionOf.columns {
			if len(c.previousNames) > 0 {
				return "", false
			}
		}
		return n, true
	}
	for _, c := range p.columns {
		if c.name == n || slices.Contains(c.previousNames, n) {
			return c.name, true
		}
	}
	return "", false
}

// partitionedUnique applies U4 to one unique index on t. certain is
// false when the name may mean another table; a violation is then only
// possible, not certain.
func (s *state) partitionedUnique(t *table, k uniqueKey, certain bool, id string) {
	if t == nil {
		// E1: a table outside the set fits its use.
		return
	}
	unproven := func(why string) {
		s.unproven(t, fmt.Sprintf("%s on %s may be refused: %s%s", k.what, t.q, why, precondition(id)))
	}
	refuse := func(why string) {
		text := fmt.Sprintf("%s on %s: %s (Postgres refuses, 0A000)%s", k.what, t.q, why, precondition(id))
		if certain {
			s.refuse(t, text)
			return
		}
		s.unproven(t, text)
	}
	if partitioningUnknown(t) {
		unproven("whether the table is partitioned is not known, and a unique index on a partitioned table must include the partition key")
		return
	}
	for _, p := range s.partitionedTree(t) {
		where := "the partition key"
		if p != t {
			where = "the partition key of partition " + p.q.String()
		}
		if partitioningUnknown(p) || !p.partitionProven {
			unproven(where + " is not proven to be plain RANGE or LIST columns")
			return
		}
		if k.exclude {
			unproven("an EXCLUDE constraint on a partitioned table is not proven")
			return
		}
		if k.method != "btree" {
			refuse(fmt.Sprintf("a unique index on a partitioned table must use btree, not %s", k.method))
			return
		}
		for _, n := range p.partitionKey {
			name, ok := partitionColumn(p, n)
			if !ok {
				unproven(fmt.Sprintf("%s names column %s, which the pass does not follow", where, n))
				return
			}
			i := slices.Index(k.columns, name)
			switch {
			case i < 0:
				refuse(fmt.Sprintf("unique constraint on partitioned table must include all partitioning columns: (%s) lacks %s of %s", strings.Join(k.columns, ", "), name, where))
				return
			case !k.bare[i]:
				unproven(fmt.Sprintf("column %s is written with an operator class, collation or ordering whose match with %s is not proven", name, where))
				return
			}
		}
	}
}

// constraintKeys applies U4 to the primary keys, UNIQUE and EXCLUDE
// constraints an ALTER TABLE adds to t (rows M5a and M5f).
func (s *state) constraintKeys(t *table, tc tableConstraints, certain bool, row string) {
	id := rowIDs[row].p4
	for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
		bare := make([]bool, len(k.columns))
		for i := range bare {
			bare[i] = true
		}
		s.partitionedUnique(t, uniqueKey{what: fmt.Sprintf("a key over (%s)", strings.Join(k.columns, ", ")), columns: k.columns, bare: bare, method: "btree"}, certain, id)
	}
	for _, ch := range tc.checks {
		if ch.exclude {
			s.partitionedUnique(t, uniqueKey{what: "an EXCLUDE constraint", exclude: true}, certain, id)
		}
	}
}

// indexKey is the uniqueKey of CREATE UNIQUE INDEX (row M4).
func indexKey(ci *createIndex) uniqueKey {
	k := uniqueKey{what: "CREATE UNIQUE INDEX", method: "btree"}
	if ci.using != nil {
		k.method = *ci.using
	}
	for _, el := range ci.elements {
		name, bare := "", false
		if el.column != nil {
			name, bare = *el.column, true
			for _, w := range el.names[1:] {
				if w != "asc" && w != "desc" && w != "nulls" && w != "first" && w != "last" {
					bare = false
				}
			}
		}
		k.columns = append(k.columns, name)
		k.bare = append(k.bare, bare)
	}
	return k
}

// partitionChild applies U4 to CREATE TABLE c PARTITION OF p ...
// PARTITION BY (row M1b): c receives p's unique indexes, each of which
// must include c's own partition key.
func (s *state) partitionChild(t *table, ct *createTable) {
	if ct.partitionBy == "" {
		return
	}
	t.partitionKey = ct.partitionKey
	t.partitionProven = (ct.partitionBy == "range" || ct.partitionBy == "list") && ct.partitionPlain
	what := "CREATE TABLE " + ct.name.String() + " PARTITION OF " + ct.partitionOf.String()
	if listKeyRefused(ct) {
		s.refuse(t, fmt.Sprintf("%s ... PARTITION BY LIST: cannot use \"list\" partition strategy with more than one column (Postgres refuses, 42P17)%s", what, precondition("M1b.P4")))
		return
	}
	p := t.partitionOf
	if p == nil || partitioningUnknown(p) || !factsKnown(p) || p.keysUnknown {
		s.unproven(t, fmt.Sprintf("%s ... PARTITION BY may be refused: the parent's unique indexes are not known, and each must include the new partition key%s", what, precondition("M1b.P4")))
		return
	}
	var keys []uniqueKey
	add := func(label string, cols []*column) {
		k := uniqueKey{what: what + " (" + label + ")", method: "btree"}
		for _, c := range cols {
			k.columns = append(k.columns, c.name)
			k.bare = append(k.bare, true)
		}
		keys = append(keys, k)
	}
	if p.primaryKey != nil {
		add("the parent's primary key", p.primaryKey.cols)
	}
	for _, u := range p.uniques {
		add("a unique key of the parent", u.cols)
	}
	for _, ix := range p.indexes {
		if !ix.unique {
			continue
		}
		if ix.uncertain {
			s.unproven(t, fmt.Sprintf("%s ... PARTITION BY may be refused: a unique index of the parent may exist, and it must include the new partition key%s", what, precondition("M1b.P4")))
			return
		}
		k := indexKey(&createIndex{using: ix.using, elements: ix.elements})
		k.what = what + " (a unique index of the parent)"
		keys = append(keys, k)
	}
	if p.hasChecks {
		for _, ch := range p.checks {
			if ch.exclude {
				keys = append(keys, uniqueKey{what: what + " (an EXCLUDE constraint of the parent)", exclude: true})
			}
		}
		if p.unnamedChecks {
			// An unnamed CHECK or EXCLUDE: the pass does not tell them apart.
			keys = append(keys, uniqueKey{what: what + " (an unnamed constraint of the parent)", exclude: true})
		}
	}
	for _, k := range keys {
		s.partitionedUnique(t, k, true, "M1b.P4")
		if s.failure != nil {
			return
		}
	}
}

// partitionByOf reads the PARTITION BY clause of CREATE TABLE ...
// PARTITION OF p (FOR VALUES ... | DEFAULT) [PARTITION BY ...]: the
// strategy and key as for a plain CREATE TABLE.
func partitionByOf(c *cursor, ct *createTable) {
	for c.i < len(c.toks) {
		if c.isPunct("(") {
			e, err := c.group()
			if err != nil {
				return
			}
			c.i = e
			continue
		}
		if !c.acceptKw("partition", "by") {
			c.i++
			continue
		}
		strategy := "?"
		for _, w := range []string{"range", "list", "hash"} {
			if c.acceptKw(w) {
				strategy = w
			}
		}
		if !c.isPunct("(") {
			ct.partitionBy = strategy
			return
		}
		e, err := c.group()
		if err != nil {
			ct.partitionBy = strategy
			return
		}
		ct.partitionKey = c.identifiers(c.i+1, e-1)
		ct.partitionBy = strategy
		ct.partitionPlain = true
		ct.partitionCount = len(c.splitList(c.i+1, e-1))
		for _, r := range c.splitList(c.i+1, e-1) {
			if r[1]-r[0] != 1 || c.toks[r[0]].kind != tokIdent && c.toks[r[0]].kind != tokQuoted {
				ct.partitionPlain = false
			}
		}
		return
	}
}

package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Key dependencies (Revision 4). A foreign key depends on the unique index
// of the primary key, UNIQUE constraint or unique index it uses
// (pg_constraint.c 312-324), and that index depends internally on its
// constraint. So DROP CONSTRAINT of a key and DROP INDEX of a unique index
// that a foreign key uses are refused without CASCADE, and CASCADE drops
// the foreign key; DROP INDEX of a constraint's index is refused even with
// CASCADE (dependency.c 770-789: "cannot drop index because constraint
// requires it"). Revision 7: DROP CONSTRAINT never names a known key, since
// constraint names are not tracked; it may remove any key of its table,
// so the keys become unknown, never stay proven.

// namesTable reports whether a written reference may name t.
func namesTable(ref qname, t *table) bool {
	return ref.name == t.q.name && (ref.schema == nil || t.q.schema == nil || *ref.schema == *t.q.schema)
}

// sameNames reports whether the written column names are, as a set, the
// names of cols.
func sameNames(written []string, cols []*column) bool {
	if len(written) != len(cols) {
		return false
	}
	for _, c := range cols {
		if !slices.Contains(written, c.name) {
			return false
		}
	}
	return true
}

// referencing lists every foreign key of the set that may reference t, in
// table order: those linked to t and those unresolved whose written
// table may be t. match, when set, keeps only the ones it accepts.
func (s *state) referencing(t *table, match func(*foreignKey) bool) []*foreignKey {
	var out []*foreignKey
	for _, o := range s.tables {
		for _, fk := range o.foreignKeys {
			if fk.table != t && (fk.table != nil || !namesTable(fk.written.table, t)) {
				continue
			}
			if match == nil || match(fk) {
				out = append(out, fk)
			}
		}
	}
	return out
}

// keyUsers lists the foreign keys that may use the key or unique index
// over cols of t: linked ones over exactly cols, unresolved ones whose
// written columns are those (or none, for the primary key).
func (s *state) keyUsers(t *table, cols []*column, primary bool) []*foreignKey {
	return s.referencing(t, func(fk *foreignKey) bool {
		if fk.table != nil {
			return sameColumns(fk.columns, cols)
		}
		if fk.written.columns == nil {
			return primary
		}
		return sameNames(fk.written.columns, cols)
	})
}

// unresolve makes a linked foreign key unresolved: whether it still
// exists, or what it references, is unknown.
func (s *state) unresolve(fk *foreignKey, why string) {
	if fk.table == nil {
		return
	}
	from := "a table"
	if o := s.owner(fk); o != nil {
		from = o.q.String()
	}
	fk.table, fk.columns, fk.why = nil, nil, why
	s.unresolved(fmt.Sprintf("reference from %s to %s is unresolved: %s", from, fk.written.table, why))
}

// keyDependents applies the dependency of foreign keys on a key or index
// that a statement (what) drops from t. Without CASCADE the drop is
// refused if a user uses it: when certain the statement is refused,
// otherwise it may be refused; keyDependents then reports false. With
// CASCADE each user goes: dropped when sole reports that it surely used
// the dropped key, unresolved otherwise; the users' tables are partial.
func (s *state) keyDependents(t *table, what string, users []*foreignKey, cascade, certain bool, sole func(*foreignKey) bool) bool {
	if len(users) == 0 {
		return true
	}
	if !cascade {
		if certain {
			s.refuse(t, fmt.Sprintf("%s without CASCADE is refused while foreign keys use it (Postgres refuses)", what))
		} else {
			text := fmt.Sprintf("%s without CASCADE may be refused: a foreign key may use the key it drops", what)
			s.partial(t, text)
			s.mayFail(text)
		}
		for _, fk := range users {
			if o := s.owner(fk); o != nil && o != t {
				s.partialOnce(o, fmt.Sprintf("a foreign key of this table may make %s fail", what))
			}
		}
		return !certain
	}
	for _, fk := range users {
		o := s.owner(fk)
		if o == nil {
			continue
		}
		if o != t {
			s.touch(o)
		}
		if sole != nil && fk.table != nil && sole(fk) {
			s.dropForeignKey(fk)
			s.partialOnce(o, fmt.Sprintf("%s CASCADE dropped a foreign key of this table", what))
			continue
		}
		s.unresolve(fk, fmt.Sprintf("%s CASCADE may have dropped this foreign key", what))
		s.partialOnce(o, fmt.Sprintf("%s CASCADE may have dropped a foreign key of this table", what))
	}
	return true
}

// dropConstraint applies ALTER TABLE ... DROP CONSTRAINT (Revision 7) to
// every table the statement may name. The pass does not track constraint
// names: six reviews showed that Postgres's generated names
// (truncation, collision suffixes, expression columns, renames through
// ALTER INDEX and ALTER TABLE) cannot be computed exactly from source.
// So any DROP CONSTRAINT, written or generated name, makes every key fact
// of each possible target unknown. Without IF EXISTS the name may be
// missing, which Postgres refuses (tablecmds.c 12644-12651), so the
// migration may fail (WF8).
func (s *state) dropConstraint(targets []*table, a alterAction) {
	what := "DROP CONSTRAINT " + a.name
	if !a.ifExists {
		s.mayFail(fmt.Sprintf("%s may name a constraint the table does not hold (constraint names are not tracked); Postgres refuses a missing constraint without IF EXISTS", what))
	}
	for _, t := range targets {
		s.forgetKeys(t, what, a.cascade)
	}
}

// forgetKeys applies a DROP CONSTRAINT to t: the constraint may have been
// any primary key, UNIQUE constraint or foreign key of t, so t is partial,
// its keys become unknown (primary_key, unique_keys and the columns'
// primary_key and unique facts are null) and its own references are
// unresolved. Unique indexes are no longer proven either: an unmodeled
// ADD CONSTRAINT ... USING INDEX may have made one a constraint's index.
// Foreign keys that may use a key of t depend on it (pg_constraint.c
// 312-324): without CASCADE the statement may be refused, with CASCADE
// they may have been dropped; either way they are unresolved and their
// tables partial. NOT NULL marks stay: dropping a primary key keeps them
// (Postgres 17 clears attnotnull only in ATExecDropNotNull, tablecmds.c
// 7666-7668). CHECK constraints are kept, so that later drops that depend
// on them stay conservative.
func (s *state) forgetKeys(t *table, what string, cascade bool) {
	s.partial(t, fmt.Sprintf("%s may have dropped any primary key, unique constraint or foreign key of this table; its keys are unknown", what))
	users := s.referencing(t, nil)
	s.keyDependents(t, what, users, cascade, false, nil)
	if !cascade {
		// keyDependents made their tables partial; whether the statement
		// was refused is unknown, so the keys they use are too.
		for _, fk := range users {
			s.unresolve(fk, what+" may have dropped the key this foreign key uses")
		}
	}
	for _, fk := range t.foreignKeys {
		s.unresolve(fk, what+" may have dropped this foreign key")
	}
	if t.primaryKey != nil {
		t.primaryKey, t.pkMaybe = nil, true
	}
	t.uniques, t.keysUnknown = nil, true
	for _, ix := range t.indexes {
		if ix.unique {
			ix.uncertain = true
		}
	}
}

// generatedKeyNames are the names Postgres gives the indexes of t's
// unnamed primary key and UNIQUE constraints (without truncation or
// collision suffixes).
func generatedKeyNames(t *table) []string {
	var out []string
	if t.primaryKey != nil && t.primaryKey.name == nil {
		out = append(out, orGenerated(t.primaryKey.gen, t.q.name+"_pkey"))
	}
	for _, u := range t.uniques {
		if u.name == nil {
			out = append(out, orGenerated(u.gen, t.q.name+"_"+strings.Join(names(u.cols), "_")+"_key"))
		}
	}
	return out
}

// dropIndex applies DROP INDEX of the written index ix of t.
func (s *state) dropIndex(t *table, ix *index, q qname, d *dropObjects) {
	what := "DROP INDEX " + q.String()
	if q.schema == nil && s.pathChanged || s.failure != nil {
		s.partial(t, fmt.Sprintf("%s may name another schema's index or may not apply; the index may have been dropped", what))
		if !d.ifExists {
			// Revision 8: the name may mean no index.
			s.unknownFact(fmt.Sprintf("%s may be refused: the name may mean no index (search_path)", what))
		}
		if backsKey(ix) && d.cascade {
			s.keyDependents(t, what, s.keyUsers(t, ix.columns, false), true, false, nil)
		}
		ix.uncertain = true
		if s.failure == nil {
			// P3: the statement applies, so the index may be gone.
			ix.mayBeGone = true
		}
		return
	}
	if backsKey(ix) {
		users := s.keyUsers(t, ix.columns, false)
		sole := func(fk *foreignKey) bool { return keyCandidates(t, fk.columns) == 1 }
		if !s.keyDependents(t, what, users, d.cascade, true, sole) {
			return
		}
	}
	t.indexes = slices.DeleteFunc(t.indexes, func(x *index) bool { return x == ix })
	if d.concurrently {
		s.concurrently(t, "DROP INDEX CONCURRENTLY", "M7.P6")
	}
}

// dropConstraintIndex applies DROP INDEX of the index of a primary key or
// UNIQUE constraint of t: Postgres refuses it, with or without CASCADE
// (dependency.c 770-789).
func (s *state) dropConstraintIndex(t *table, q qname) {
	s.touch(t)
	if q.schema != nil || !s.pathChanged {
		s.refuse(t, fmt.Sprintf("DROP INDEX %s names the index of a primary key or unique constraint (Postgres refuses, even with CASCADE)", q))
		return
	}
	text := fmt.Sprintf("DROP INDEX %s may name the index of a primary key or unique constraint of this table, which Postgres refuses", q)
	s.partial(t, text)
	s.mayFail(text)
}

// dropUnwrittenIndex applies DROP INDEX of a name the set never wrote on
// a table it may belong to: any unnamed index may be the one (generated
// name), so those become uncertain, and foreign keys that may use an
// unnamed unique index make the drop a possible refusal without CASCADE
// (unresolved with it). Keys stay: DROP INDEX cannot drop a constraint's
// index.
func (s *state) dropUnwrittenIndex(t *table, q qname, cascade bool) {
	what := "DROP INDEX " + q.String()
	var users []*foreignKey
	for _, ix := range t.indexes {
		if ix.name != nil {
			continue
		}
		if backsKey(ix) {
			for _, fk := range s.keyUsers(t, ix.columns, false) {
				if !slices.Contains(users, fk) {
					users = append(users, fk)
				}
			}
		}
	}
	s.keyDependents(t, what, users, cascade, false, nil)
	for _, ix := range t.indexes {
		if ix.name == nil {
			ix.uncertain = true
			// P3: any unnamed index may be the one dropped.
			ix.mayBeGone = true
		}
	}
}

// C3 (T018a2 B4, contract row M5g-1): DROP CONSTRAINT of a name the set
// wrote on the table. Postgres keeps constraint names unique per table
// (pg_constraint.c ConstraintNameIsUsed, ChooseConstraintName never reuses
// a held name) and ATExecDropConstraint (tablecmds.c REL_17_6) looks the
// name up among the constraints of that one table: a name it does not
// hold is refused without IF EXISTS (42704) and a notice with it. So when
// the pass holds every constraint name of the table exactly, the
// statement drops exactly the constraint of that name and nothing else.
//
// The names are exact while nothing unseen can have added, renamed or
// dropped a constraint of the table (constraintNamesExact): its facts are
// known (no opaque, D or M5k statement since it was created), no DROP
// CONSTRAINT made its keys unknown, and ALTER INDEX/TABLE RENAME of a
// key's index renamed the key with it (alterIndex; tablecmds.c
// RenameRelationInternal renames an index-backed constraint together with
// its index). Postgres merges equal index constraints of one
// transformation and gives the survivor the written name
// (mergeIndexConstraints), so a merged-away name is not held. Generated
// names of unnamed constraints are not tracked (backlog): a name of a
// generated shape that the table may hold keeps the Revision 7 rule.

// constraintNamesExact reports whether every constraint name of t is held
// by the pass (C3).
func constraintNamesExact(t *table) bool {
	return factsKnown(t) && !t.keysUnknown && !t.pkMaybe && !t.checksUnknown
}

// pkDependency reports a statement that may create an object that
// depends on a primary key by functional dependency (a query that groups
// by the key and uses other columns: a view, materialized view or rule,
// or a function with a SQL-standard body, BEGIN ATOMIC or RETURN;
// dependency recorded by check_functional_grouping, parse_agg.c).
// Dropping such a key is refused without CASCADE (2BP01), so C3 does not
// claim a primary-key drop in a set that has one.
func pkDependency(toks []token) bool {
	if len(toks) == 0 || toks[0].kind != tokIdent || toks[0].value != "create" {
		return false
	}
	routine := false
	for _, t := range toks {
		if t.kind != tokIdent {
			continue
		}
		switch t.value {
		case "view", "rule":
			return true
		case "function", "procedure":
			routine = true
		case "atomic", "return":
			if routine {
				return true
			}
		}
	}
	return false
}

// dropWrittenConstraint applies DROP CONSTRAINT under C3. It reports
// false when the rule does not apply (the caller then applies Revision
// 7): the name may mean another table, t's constraint names are not
// exact, the statement adds constraints before Postgres's drop pass, or
// the name may be a generated name of an unnamed constraint of t.
func (s *state) dropWrittenConstraint(t *table, a alterAction, certain bool) bool {
	if !certain || a.afterOther || !constraintNamesExact(t) {
		return false
	}
	what := "DROP CONSTRAINT " + a.name
	var (
		pk    *key
		uq    *key
		fk    *foreignKey
		chk   = -1
		count int
	)
	if t.primaryKey != nil && t.primaryKey.name != nil && *t.primaryKey.name == a.name {
		pk = t.primaryKey
		count++
	}
	for _, u := range t.uniques {
		if u.name != nil && *u.name == a.name {
			uq = u
			count++
		}
	}
	for _, f := range t.foreignKeys {
		if f.name != nil && *f.name == a.name {
			fk = f
			count++
		}
	}
	for i, ch := range t.checks {
		if ch.name == a.name {
			chk = i
			count++
		}
	}
	switch {
	case count > 1:
		return false
	case count == 0:
		// A constraint trigger (CREATE CONSTRAINT TRIGGER) also has a
		// pg_constraint row under the trigger's name.
		if generatedShape(t, a.name, constraintSuffixes) != "" || t.triggers || t.triggerNamesUnknown {
			return false
		}
		if !a.ifExists {
			s.refuse(t, fmt.Sprintf("%s: the table holds no constraint of this name (Postgres refuses)%s", what, precondition("M5g-1.P1")))
		}
		return true
	case pk != nil && s.pkDependents:
		// A view, rule or SQL-standard body may depend on the key.
		return false
	}
	// sole reports that a foreign key surely uses the dropped key: it is
	// the only candidate over its columns, and no unique index that may
	// still exist could be the one it uses.
	sole := func(f *foreignKey) bool {
		if keyCandidates(t, f.columns) != 1 {
			return false
		}
		for _, ix := range t.indexes {
			if ix.unique && ix.uncertain && sameColumns(ix.columns, f.columns) {
				return false
			}
		}
		return true
	}
	switch {
	case pk != nil:
		var users []*foreignKey
		if !pk.deferrable {
			// No foreign key can use a deferrable key (tablecmds.c
			// transformFkeyCheckAttrs).
			users = s.keyUsers(t, pk.cols, true)
		}
		if !s.keyDependents(t, what+precondition("M5g-1.P8"), users, a.cascade, true, sole) {
			return true
		}
		t.primaryKey = nil
	case uq != nil:
		var users []*foreignKey
		if !uq.deferrable {
			users = s.keyUsers(t, uq.cols, false)
		}
		if !s.keyDependents(t, what+precondition("M5g-1.P8"), users, a.cascade, true, sole) {
			return true
		}
		t.uniques = slices.DeleteFunc(t.uniques, func(k *key) bool { return k == uq })
	case fk != nil:
		s.dropForeignKey(fk)
	default:
		t.checks = slices.Delete(t.checks, chk, chk+1)
	}
	return true
}

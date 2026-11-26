package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Enum labels (T018a2 K2, B2 as amended by Revision 1; contract rows R2
// and R3). CREATE TYPE ... AS ENUM records its labels on the type's
// relation record; ALTER TYPE ... ADD VALUE and RENAME VALUE change them
// when they are known (pg_enum.c AddEnumLabel, RenameEnumLabel, REL_17_6):
//
//   - ADD VALUE of an existing label is refused (42710) unless IF NOT EXISTS
//     is written, which makes it a notice and skips the statement before the
//     neighbour is looked up;
//   - BEFORE or AFTER a label the type does not have is refused (22023);
//   - RENAME VALUE of a missing label, or to an existing one, is refused;
//   - a label added by ADD VALUE in the current transaction to a type that
//     existed before that transaction may not be used until the commit
//     (55P04, enum.c check_safe_enum_use and pg_enum.c 575-598); a type
//     created in the same transaction uses its new labels freely.
//
// Labels are known only while the record is certain (no opaque statement,
// search_path change or possible failure since it was recorded) and every
// change since was one of these. A type the set never creates is E1; one
// it creates but does not hold is P1.

// unsafeLabel is a label added in the current migration's transaction to a
// type that existed before it.
type unsafeLabel struct {
	// label is the decoded label; any is set when the label was written in
	// a form the pass does not decode (W7): every string may use it.
	label string
	any   bool
	// names are the type's names since the label was added (a rename in
	// the same transaction keeps the label unsafe).
	names []string
}

// enumLabelsKnown returns the type's record when its labels are known.
func (s *state) enumLabelsKnown(q qname) *relation {
	r := s.relations[q.String()]
	if r == nil || r.labels == nil {
		return nil
	}
	if _, certain, _ := s.relationAt(q); !certain {
		return nil
	}
	return r
}

// enumOperation applies ALTER TYPE ... ADD VALUE or RENAME VALUE.
func (s *state) enumOperation(op *enumOp) {
	what := "ALTER TYPE " + op.q.String()
	r := s.relations[op.q.String()]
	if t, _ := s.lookup(op.q); r == nil && t != nil {
		// V4 kinds (2026-11-07): a table's row type is not an enum
		// (typecmds.c checkEnumOwner, 42809).
		if certain := s.unqualified(t, op.q) && s.failure == nil; certain {
			s.refuse(t, fmt.Sprintf("%s names this table's row type, which is not an enum (Postgres refuses)%s", what, precondition("R3.P1")))
		} else {
			s.unknownFact(fmt.Sprintf("%s may be refused: the name may mean the row type of table %s, which is not an enum%s", what, t.q, precondition("R3.P1")))
		}
		return
	}
	if r == nil {
		if s.needs(op.q, kindType, false, what, "R3.P1") {
			return
		}
		for _, k := range s.relationKeys() {
			if k == op.q.name || strings.HasSuffix(k, "."+op.q.name) {
				// The name may mean a recorded type under another
				// qualification, whose labels are not known here.
				s.relations[k].labels = nil
				if op.rename || !op.ifNotExists || op.neighbor != nil {
					s.unknownFact(fmt.Sprintf("%s may be refused: the type may be %s, whose labels are not known here%s", what, k, precondition("R3.K2")))
				}
				return
			}
		}
		// V4 (2026-11-07, g8-s8547): E1 does not cover types other than
		// its own, so an enum the pass does not hold may not exist.
		s.unknownFact(fmt.Sprintf("%s may be refused: %s is not a type the pass holds, and E1 does not cover enums%s", what, op.q, precondition("R3.P1")))
		return
	}
	known := s.enumLabelsKnown(op.q)
	if op.undecoded {
		// W7 (Revision 1.7): a label the pass does not decode may equal
		// any label; the type's labels are unknown from here on.
		if op.rename || !op.ifNotExists || op.neighbor != nil {
			s.unknownFact(fmt.Sprintf("%s may be refused: a label is written in a form the pass does not decode, so it is not compared with the type's labels%s", what, precondition("R3.K2")))
		}
		r.labels = nil
		if !op.rename {
			s.addUnsafe(r, op)
		}
		return
	}
	if op.rename {
		switch {
		case known == nil:
			s.unknownFact(fmt.Sprintf("%s RENAME VALUE may be refused: the labels of the type are not known%s", what, precondition("R3.K2")))
			r.labels = nil
		case !slices.Contains(r.labels, op.label):
			s.mayFail(fmt.Sprintf("%s RENAME VALUE %q: the type has no such label (Postgres refuses)%s", what, op.label, precondition("R3.K2")))
		case slices.Contains(r.labels, op.newLabel):
			s.mayFail(fmt.Sprintf("%s RENAME VALUE to %q: the type already has that label (Postgres refuses)%s", what, op.newLabel, precondition("R3.K2")))
		default:
			r.labels[slices.Index(r.labels, op.label)] = op.newLabel
		}
		return
	}
	if known == nil {
		// IF NOT EXISTS without BEFORE/AFTER succeeds whatever the labels.
		if !op.ifNotExists || op.neighbor != nil {
			s.unknownFact(fmt.Sprintf("%s ADD VALUE %q may be refused: the labels of the type are not known%s", what, op.label, precondition("R3.K2")))
		}
		r.labels = nil
		s.addUnsafe(r, op)
		return
	}
	if slices.Contains(r.labels, op.label) {
		if !op.ifNotExists {
			s.mayFail(fmt.Sprintf("%s ADD VALUE %q: the type already has this label (Postgres refuses)%s", what, op.label, precondition("R3.K2")))
		}
		return
	}
	at := len(r.labels)
	if op.neighbor != nil {
		i := slices.Index(r.labels, *op.neighbor)
		if i < 0 {
			s.mayFail(fmt.Sprintf("%s ADD VALUE %q %s %q: the type has no such label (Postgres refuses)%s", what, op.label, map[bool]string{true: "BEFORE", false: "AFTER"}[op.before], *op.neighbor, precondition("R3.K2")))
			return
		}
		at = i
		if !op.before {
			at = i + 1
		}
	}
	r.labels = slices.Insert(r.labels, at, op.label)
	s.addUnsafe(r, op)
}

// addUnsafe records a label added to a type that existed before the
// current migration's transaction (a type created by this migration uses
// its labels freely). The pass runs every migration in sqlx's transaction
// (it does not read -- no-transaction, C4), so the label stays unsafe for
// the rest of the migration.
func (s *state) addUnsafe(r *relation, op *enumOp) {
	if r.m == s.m {
		return
	}
	s.unsafe = append(s.unsafe, unsafeLabel{label: op.label, any: op.undecoded, names: []string{op.q.name}})
}

// mayBeLabel reports whether a string constant of the statement may be
// the unsafe label u: its decoded value equals it, or either is written
// in a form the pass does not decode (W7).
func (s *state) mayBeLabel(t token, u unsafeLabel) bool {
	if u.any {
		return true
	}
	v, ok := literalValue(t, s.m.source)
	return !ok || v == u.label
}

// unsafeUse makes a statement that may use a label added in this
// transaction one that may fail (55P04): it contains the label as a string
// constant, names the type, or is opaque.
func (s *state) unsafeUse(st statement, a analysis) {
	if len(s.unsafe) == 0 || s.failure != nil {
		return
	}
	opaque := a.class == classOpaque || a.runsFunction != ""
	for _, u := range s.unsafe {
		uses := opaque
		for _, t := range st.tokens {
			if t.kind == tokString && s.mayBeLabel(t, u) || (t.kind == tokIdent || t.kind == tokQuoted) && slices.Contains(u.names, t.value) {
				uses = true
				break
			}
		}
		if uses {
			s.unknownFact(fmt.Sprintf("the statement may use enum label %q, which was added in this transaction to a type that existed before it; Postgres refuses such a use before the commit%s", u.label, precondition("R3.K2")))
			return
		}
	}
}

// renameUnsafe follows a renamed type in the unsafe list.
func (s *state) renameUnsafe(old, newName string) {
	for i := range s.unsafe {
		if slices.Contains(s.unsafe[i].names, old) {
			s.unsafe[i].names = append(s.unsafe[i].names, newName)
		}
	}
}

package schema

import (
	"fmt"
	"strings"
)

// Relation kinds of a held name (T018a2 Revision 1.6 V4, contract-violation
// fix of 2026-11-07, g8-s4299). Tables, indexes, sequences, views,
// materialized views, composite types and foreign tables share pg_class's
// namespace in a schema. A statement addressed to one relation kind finds
// the name in pg_class whatever its kind, so IF EXISTS skips only a name
// pg_class does not hold; a name held by a relation of another kind reaches
// the kind check, which refuses it (42809):
//
//   - ALTER INDEX, VIEW, MATERIALIZED VIEW, SEQUENCE, FOREIGN TABLE and TYPE
//     (tablecmds.c RangeVarCallbackForAlterRelation 17847-17950: each names
//     its own kind; ALTER INDEX ... RENAME accepts any relation but a
//     composite type, ALTER TABLE accepts every kind but a composite type);
//   - DROP of every relation kind (RangeVarCallbackForDropRelation 1632,
//     DropErrorMsgWrongType 1441);
//   - each ALTER TABLE (and ALTER INDEX) subcommand on the relation it
//     reaches (ATPrepCmd 4819-5135, ATSimplePermissions 6543: the ATT_ mask
//     of each subcommand);
//   - ALTER TYPE and ALTER DOMAIN of a relation's row type or of a type of
//     another kind (typecmds.c 3772-3784, 4073; the attribute forms are
//     ALTER TABLE forms on a composite type).
//
// Each case was checked with psql against Postgres 17 (rev16Cases, "V4
// kinds"). A refusal is certain only when relationAt is; otherwise the
// statement may fail.

// pgClassKind reports whether a kind relationAt returns is a pg_class
// relation, not a type or domain only.
func pgClassKind(kind string) bool {
	return relationKinds[kind] || kind == "generated index or sequence"
}

// attTarget is a set of relation kinds, as tablecmds.c's ATT_ masks.
type attTarget uint8

const (
	attTable attTarget = 1 << iota
	attView
	attMatview
	attIndex
	attComposite
	attForeign
	attSequence
	attAll = attTable | attView | attMatview | attIndex | attComposite | attForeign | attSequence
)

// attOf maps a relationAt kind to its ATT_ bits; an unwritten generated
// name may be an index or a sequence.
func attOf(kind string) attTarget {
	switch kind {
	case "table":
		return attTable
	case "view":
		return attView
	case "materialized view":
		return attMatview
	case "index":
		return attIndex
	case "composite type":
		return attComposite
	case "foreign table":
		return attForeign
	case "sequence":
		return attSequence
	case "generated index or sequence":
		return attIndex | attSequence
	}
	return 0
}

// heldAtt is attOf for the name q held as kind; an unwritten generated
// name is an index unless it is the generated name of a serial or
// identity sequence of a table in the same schema.
func (s *state) heldAtt(q qname, kind string) attTarget {
	if kind != "generated index or sequence" {
		return attOf(kind)
	}
	for _, t := range s.tables {
		if !sameSchema(t.q.schema, q.schema) {
			continue
		}
		for _, c := range t.columns {
			if (c.implied || c.identity != nil) && seqName(t, c) == q.name {
				return attIndex | attSequence
			}
		}
	}
	return attIndex
}

// actionTargets returns the relation kinds Postgres accepts an ALTER TABLE
// action on (ATSimplePermissions' mask, or the RenameStmt and
// AlterObjectSchemaStmt checks), and whether the kind alone proves the
// action on a relation that is not a modeled table. An action that also
// needs a column, constraint, index, option or object the pass does not
// track on such a relation is not proven (proven false); an action the
// table does not list has targets 0: neither refused nor proven.
func actionTargets(a alterAction) (targets attTarget, proven bool) {
	word := a.kind
	if word == "unsupported" {
		word = a.word
	}
	switch word {
	case "rename_table":
		return attAll &^ attComposite, true
	case "set_schema":
		return attAll &^ (attComposite | attIndex), true
	case "owner":
		return attAll &^ attComposite, true
	case "set_logged", "set_unlogged":
		return attTable | attSequence, true
	case "rename_column":
		// renameatt_check 3679-3691.
		return attAll &^ attSequence, false
	case "add_column", "drop_column", "alter_type":
		return attTable | attComposite | attForeign, false
	case "set_default", "drop_default", "add_identity":
		return attTable | attView | attForeign, false
	case "set_not_null", "drop_not_null", "add_constraint", "drop_constraint", "validate", "inherit", "no_inherit",
		"trigger_all", "enable_trigger", "set_without_oids":
		return attTable | attForeign, false
	case "add_using_index", "alter_constraint", "row_security", "enable_rule", "of", "detach":
		return attTable, false
	case "attach":
		return attTable | attIndex, false
	case "set_without_cluster", "cluster_on", "replica_identity", "replica_identity_index":
		return attTable | attMatview, false
	case "storage_parameters":
		// The options a view, materialized view or index takes differ
		// from a table's (U9).
		return attTable | attView | attMatview | attIndex, false
	case "alter_column_other":
		// SET STATISTICS, SET/RESET (...), SET STORAGE, SET COMPRESSION,
		// SET/DROP EXPRESSION, SET/DROP IDENTITY: the union of their masks.
		return attAll &^ (attSequence | attComposite), false
	}
	return 0, false
}

// kindRefusal applies the rule to a statement what on q, held as kind
// (certain as relationAt says): accepts is the set of kinds the statement
// takes. It reports whether the statement is refused or may be, and says
// so (id is the precondition of the statement's row).
func (s *state) kindRefusal(what string, q qname, kind string, certain bool, accepts attTarget, id string) bool {
	held := s.heldAtt(q, kind)
	if held == 0 || held&accepts == held {
		return false
	}
	if certain && held&accepts == 0 {
		s.mayFail(fmt.Sprintf("%s: %s is %s, which this statement does not accept (Postgres refuses, 42809, also with IF EXISTS)%s", what, q, article(kind), precondition(id)))
		return true
	}
	s.unknownFact(fmt.Sprintf("%s may be refused: %s may be %s, which this statement does not accept (Postgres refuses it, also with IF EXISTS)%s", what, q, article(kind), precondition(id)))
	return true
}

// alterTableOnRelation applies the rule to ALTER TABLE of a name held as a
// relation that is not a modeled table: each action must be one its kind
// accepts (refused otherwise), and one the kind alone proves (may fail
// otherwise). It reports whether every action is proven.
func (s *state) alterTableOnRelation(at *alterTable, kind string, certain bool) bool {
	held := s.heldAtt(at.name, kind)
	for _, a := range at.actions {
		what := fmt.Sprintf("ALTER TABLE %s (%s)", at.name, actionName(a))
		targets, proven := actionTargets(a)
		if targets != 0 && s.kindRefusal(what, at.name, kind, certain, targets, "M5a.P1") {
			return false
		}
		if !proven || held&targets != held {
			s.unknownFact(fmt.Sprintf("%s may be refused: the action on %s needs more than the relation's kind, which the pass does not prove%s", what, article(kind), precondition("M5a.P1")))
			return false
		}
	}
	return true
}

// actionName is the action's kind or M5k word, for messages.
func actionName(a alterAction) string {
	if a.kind == "unsupported" && a.word != "" {
		return a.word
	}
	return a.kind
}

// typeKindRefusal applies the rule to ALTER TYPE and ALTER DOMAIN of a name
// held as kind: ALTER DOMAIN takes only a domain (typecmds.c 3772-3784,
// 4073), the attribute forms of ALTER TYPE only a composite type (they are
// ALTER TABLE forms, RangeVarCallbackForAlterRelation 17931-17937), and the
// other ALTER TYPE forms any type but a relation's row type (typecmds.c
// 3869, 4223). A sequence or an index has no row type: the statement then
// needs a type of that name the pass does not hold. It reports whether it
// said the statement is refused or may be.
func (s *state) typeKindRefusal(r renameObject, kind string, certain bool) bool {
	what := fmt.Sprintf("ALTER %s %s", map[bool]string{true: "DOMAIN", false: "TYPE"}[r.kind == "domain"], r.q)
	var ok bool
	switch {
	case r.kind == "domain":
		ok = kind == "domain"
	case r.attributes != "":
		ok = kind == "composite type"
	default:
		ok = kind == "type" || kind == "domain" || kind == "composite type"
	}
	if ok {
		return false
	}
	rowType := rowTypeKinds[kind]
	if r.attributes != "" && !relationKinds[kind] {
		// A type or domain, not a relation: ALTER TABLE's lookup finds
		// no relation of the name (42P01), or another kind (42809).
		rowType = true
	}
	if certain && rowType {
		s.mayFail(fmt.Sprintf("%s: %s is %s, which this statement does not accept (Postgres refuses)%s", what, r.q, article(kind), precondition("R4.P1")))
		return true
	}
	s.unknownFact(fmt.Sprintf("%s may be refused: %s is %s here, which this statement does not accept, and the pass holds no other type of the name%s", what, r.q, article(kind), precondition("R4.P1")))
	return true
}

// renameColumnOf applies the rule to ALTER VIEW / MATERIALIZED VIEW ...
// RENAME [COLUMN] and ALTER TYPE ... RENAME ATTRIBUTE. Both are renameatt,
// which takes any relation but a sequence whatever kind the statement
// names (renameatt_check 3679-3691, checked with psql), and a column or
// attribute the pass does not track outside modeled tables. It reports
// whether it applied (the statement then changes no record).
func (s *state) renameColumnOf(r renameObject) bool {
	if r.attributes != "rename" {
		return false
	}
	what := fmt.Sprintf("ALTER %s %s RENAME", strings.ToUpper(r.kind), r.q)
	if t, _ := s.lookup(r.q); t != nil {
		s.touch(t)
		s.partial(t, what+" may rename a column of this table, which the pass does not model")
		s.unknownFact(fmt.Sprintf("%s may be refused: whether %s has the column is not proven on this path%s", what, r.q, precondition("R4.P1")))
		return true
	}
	kind, certain, found := s.relationAt(r.q)
	switch {
	case !found:
		// Not held: V4's rule for a name the pass does not hold decides.
		return false
	case kind == "sequence" && certain:
		s.mayFail(fmt.Sprintf("%s: %s is a sequence, whose columns cannot be renamed (Postgres refuses)%s", what, r.q, precondition("R4.P1")))
	case !pgClassKind(kind):
		// A type or domain is no relation: IF EXISTS skips unless an
		// index the pass does not see holds the name.
		if !r.ifExists {
			s.unknownFact(fmt.Sprintf("%s may be refused: the pass holds %s as %s, not as a relation%s", what, r.q, article(kind), precondition("R4.P1")))
		}
	default:
		s.unknownFact(fmt.Sprintf("%s may be refused: the pass does not track the columns of %s%s", what, r.q, precondition("R4.P1")))
	}
	return true
}

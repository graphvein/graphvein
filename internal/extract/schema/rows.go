package schema

import (
	"regexp"
)

// Tables proven empty (T018a2, contract row N3 amendment V5/V6).
//
// The pass models no rows, so in general a table may hold any rows its
// constraints allow (Revision 1.3). One case is proven without modelling
// rows: a table the pass created, with no statement since its creation that
// may have written a row into it. A single-row INSERT into such a table
// cannot meet another row, so its primary key, unique keys and plain
// unique indexes are satisfied, and its serial or identity defaults take
// the first value of a sequence nobody else advanced.
//
// The proof is closed: the empty mark is set by a CREATE TABLE the pass
// models (a new table object), and every statement clears it unless the
// statement is in the short list below of forms that write no rows into
// another table. Anything else, including every opaque, unsupported or
// unreadable statement, clears the mark of every table.

// sequenceUse matches source text that may advance or set a sequence
// other than through an INSERT's own defaults: a call of nextval or
// setval anywhere (also inside function bodies and other tables'
// defaults) or ALTER SEQUENCE. A set whose text matches never proves a
// sequence default (V6).
// sequenceCall matches a call of nextval or setval (U5).
var sequenceCall = regexp.MustCompile(`(?i)\b(nextval|setval)\b`)

var sequenceUse = regexp.MustCompile(`(?i)\b(nextval|setval)\b|\balter\s+sequence\b`)

// rowFreeHeads are statement heads that never write rows into a table
// the set created: schema changes, comments, privileges and transaction
// and session control. CREATE and ALTER are refined in rowWrites.
var rowFreeHeads = map[string]bool{
	"create": true, "alter": true, "drop": true, "comment": true, "grant": true, "revoke": true,
	"begin": true, "start": true, "commit": true, "end": true, "rollback": true, "abort": true,
	"savepoint": true, "release": true, "set": true, "reset": true, "lock": true,
	"notify": true, "listen": true, "unlisten": true, "security": true,
}

// dataHeads are the heads of statements that write or change rows (R5).
var dataHeads = map[string]bool{"insert": true, "update": true, "merge": true, "copy": true, "with": true}

// rowWrites updates the empty marks after one analysed statement.
func (s *state) rowWrites(st statement, a analysis) {
	toks := st.tokens
	if len(toks) == 0 {
		return
	}
	// A query (CREATE TABLE AS, a view's query run by WITH DATA, SELECT
	// INTO) or a function the set defines may run code that writes rows.
	if a.class == classOpaque || a.class == classUnsupported || a.class == classError || a.runsFunction != "" || a.query != "" || toks[0].kind != tokIdent {
		s.clearEmpty()
		if toks[0].kind == tokIdent && dataHeads[toks[0].value] {
			s.allRowsWritten()
		}
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	head := toks[0].value
	if ruleWrites(toks) {
		// V5: a rule's action writes rows when a data statement runs.
		s.rev16.rulesWrite = true
	}
	if s.rev16.rulesWrite && dataHeads[head] {
		s.clearEmpty()
		s.allRowsWritten()
		return
	}
	switch {
	case head == "insert":
		// INSERT writes rows into its target only, when the target is a
		// table the pass holds under a certain name and no trigger of the
		// set may run.
		c.i = 1
		if c.acceptKw("into") {
			if q, err := c.name(); err == nil {
				if t := s.scope(q, q.name).t; t != nil && !t.triggers {
					t.empty = false
					t.setRows = true
					return
				}
			}
		}
		s.clearEmpty()
		s.allRowsWritten()
	case head == "update" || head == "delete" || head == "truncate":
		// No row is added; a trigger may add rows anywhere. UPDATE changes
		// the values of rows (R5).
		if head == "update" {
			c.i = 1
			c.acceptKw("only")
			if q, err := c.name(); err == nil && s.scope(q, q.name).t != nil {
				s.scope(q, q.name).t.setRows = true
			} else {
				s.allRowsWritten()
			}
		}
		for _, t := range s.tables {
			if t.triggers {
				s.clearEmpty()
				s.allRowsWritten()
				return
			}
		}
	case c.peekKw("create", "extension"), c.peekKw("alter", "extension"), c.peekKw("create", "rule"), c.peekKw("create", "or", "replace", "rule"):
		s.clearEmpty()
	case rowFreeHeads[head]:
	default:
		s.clearEmpty()
		if head == "merge" || head == "copy" || head == "with" {
			s.allRowsWritten()
		}
	}
}

// allRowsWritten marks every table as holding rows the set may have
// written (Revision 1.4 R5): a data-changing statement whose target the
// pass did not resolve.
func (s *state) allRowsWritten() {
	for _, t := range s.tables {
		t.setRows = true
	}
}

// clearEmpty forgets every empty mark.
func (s *state) clearEmpty() {
	for _, t := range s.tables {
		t.empty = false
	}
}

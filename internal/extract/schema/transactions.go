package schema

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

// Transaction structure (T018a2 Revision 1, class P6, row T1). sqlx runs a
// migration's whole file as one simple query: inside the transaction sqlx
// opened (migrate.rs 206-225), or, for a migration that starts with
// `-- no-transaction` (source.rs 127, sqlx 0.8.0 and later), outside any
// block, where a query string of two or more statements runs in an
// implicit block (postgres.c 1095, 1150-1157). The pass reads the Cargo
// requirement of the set's sqlx (C4, sqlxversion.go): a migration with the
// marker is followed outside sqlx's transaction when every allowed
// version reads it, inside when none does, and otherwise in both block
// states, where a statement whose outcome differs between them may fail.
//
// The outcome of each transaction-control statement and each statement
// Postgres refuses inside a block (PreventInTransactionBlock) in each block
// state is the P6 table below, written from xact.c REL_17_6:
//
//	statement            sqlx/own block            implicit block            outside a block
//	BEGIN, START         warning (no effect)       becomes an own block      opens an own block
//	COMMIT, END          commits; implicit after   commits (warning)         warning (no effect)
//	ROLLBACK, ABORT      undo; implicit after      undo (warning)            warning (no effect)
//	SAVEPOINT n          defines n                 refused (25P01)           refused (25P01)
//	RELEASE n            n held: releases it;      refused (25P01)           refused (25P01)
//	                     else refused (3B001)
//	ROLLBACK TO n        as RELEASE, with undo     refused (25P01)           refused (25P01)
//	LOCK                 no effect                 no effect                 refused (25P01)
//	PreventInTransaction refused (25001)           refused (25001)           runs
//	PREPARE TRANSACTION, not listed: may fail      not listed: may fail      not listed: may fail
//	COMMIT/ROLLBACK PREPARED
//
// BeginTransactionBlock 3873-3935, EndTransactionBlock 3993-4100,
// UserAbortTransactionBlock 4153-4260, DefineSavepoint 4322-4400,
// ReleaseSavepoint 4407-4510, RollbackToSavepoint 4516-4600,
// PreventInTransactionBlock 3584-3631, CheckTransactionBlock 3671-3700,
// IsTransactionBlock 4915-4923 (an implicit block is a block). An undo is
// modelled as a possible failure of the migration (the pass does not
// revert its state), and so is a string that ends inside a block it opened
// in a no-transaction migration: sqlx never commits it (g1-s8415).

// blockState is where a statement of a migration's query string runs.
type blockState uint8

const (
	// blockSqlx is the transaction sqlx opened for the migration.
	blockSqlx blockState = iota
	// blockOwn is an explicit block the string opened with BEGIN.
	blockOwn
	// blockImplicit is the implicit block of a multi-statement string.
	blockImplicit
	// blockNone is outside any block (a one-statement no-transaction
	// migration).
	blockNone
)

// txPath is one possible execution of a migration's string.
type txPath struct {
	state blockState
	// noTx is the path on which sqlx runs the string without its
	// transaction (and does not commit an own block left open).
	noTx bool
	// savepoints are the savepoints defined in the current block.
	savepoints []string
	// changed is set once a statement ran in the current block, so that a
	// ROLLBACK undoes something.
	changed bool
}

// txKind classifies a statement for P6.
type txKind uint8

const (
	txOther txKind = iota
	txBegin
	txCommit
	txRollback
	txSavepoint
	txRelease
	txRollbackTo
	txPrepare
	txLock
	// txPrevent is a statement PreventInTransactionBlock refuses inside a
	// block.
	txPrevent
)

// txStatement classifies one statement: its P6 kind, the savepoint it
// names and, for txPrevent, the statement's name as Postgres prints it.
func txStatement(toks []token, src []byte) (kind txKind, name, what string) {
	word := func(i int, w ...string) bool {
		for k, v := range w {
			if i+k >= len(toks) || toks[i+k].kind != tokIdent || toks[i+k].value != v {
				return false
			}
		}
		return true
	}
	ident := func(i int) string {
		if i < len(toks) && (toks[i].kind == tokIdent || toks[i].kind == tokQuoted) {
			return toks[i].value
		}
		return ""
	}
	has := func(w string) bool { return containsKw(&cursor{toks: toks}, w) }
	switch {
	case len(toks) == 0:
		return txOther, "", ""
	case word(0, "begin"), word(0, "start", "transaction"):
		return txBegin, "", ""
	case word(0, "commit", "prepared"), word(0, "rollback", "prepared"):
		// Refused in a block, and outside one it needs a prepared
		// transaction, which the defaults disable: not in the table.
		return txPrepare, "", ""
	case word(0, "commit"), word(0, "end"):
		return txCommit, "", ""
	case word(0, "rollback", "to", "savepoint"):
		return txRollbackTo, ident(3), ""
	case word(0, "rollback", "to"):
		return txRollbackTo, ident(2), ""
	case word(0, "rollback"), word(0, "abort"):
		return txRollback, "", ""
	case word(0, "savepoint"):
		return txSavepoint, ident(1), ""
	case word(0, "release", "savepoint"):
		return txRelease, ident(2), ""
	case word(0, "release"):
		return txRelease, ident(1), ""
	case word(0, "prepare", "transaction"):
		return txPrepare, "", ""
	case word(0, "lock"):
		return txLock, "", ""
	case word(0, "create", "index", "concurrently"), word(0, "create", "unique", "index", "concurrently"):
		return txPrevent, "", "CREATE INDEX CONCURRENTLY"
	case word(0, "drop", "index", "concurrently"):
		return txPrevent, "", "DROP INDEX CONCURRENTLY"
	case word(0, "reindex") && has("concurrently"):
		return txPrevent, "", "REINDEX CONCURRENTLY"
	case word(0, "reindex", "schema"), word(0, "reindex", "database"), word(0, "reindex", "system"):
		return txPrevent, "", "REINDEX " + strings.ToUpper(toks[1].value)
	case word(0, "vacuum"):
		return txPrevent, "", "VACUUM"
	case word(0, "cluster") && clusterWithoutTable(toks, src):
		return txPrevent, "", "CLUSTER"
	case word(0, "create", "database"), word(0, "drop", "database"):
		return txPrevent, "", strings.ToUpper(toks[0].value) + " DATABASE"
	case word(0, "create", "tablespace"), word(0, "drop", "tablespace"):
		return txPrevent, "", strings.ToUpper(toks[0].value) + " TABLESPACE"
	case word(0, "alter", "system"):
		return txPrevent, "", "ALTER SYSTEM"
	case word(0, "discard", "all"):
		return txPrevent, "", "DISCARD ALL"
	case word(0, "create", "subscription"), word(0, "drop", "subscription"), word(0, "alter", "subscription"):
		return txPrevent, "", strings.ToUpper(toks[0].value) + " SUBSCRIPTION"
	}
	return txOther, "", ""
}

// clusterWithoutTable reports CLUSTER [VERBOSE] [(options)] without a
// table (cluster.c: it clusters every previously clustered table and
// cannot run in a block).
func clusterWithoutTable(toks []token, src []byte) bool {
	i := 1
	if i < len(toks) && toks[i].kind == tokPunct && toks[i].value == "(" {
		c := &cursor{toks: toks, i: i, src: src}
		end, err := c.group()
		if err != nil {
			return false
		}
		i = end
	}
	if i < len(toks) && toks[i].kind == tokIdent && toks[i].value == "verbose" {
		i++
	}
	return i >= len(toks)
}

// hasNoTransactionMarker reports sqlx's marker: the file's very first bytes
// (source.rs 127 at 0.8.6: sql.starts_with("-- no-transaction")).
func hasNoTransactionMarker(source []byte) bool {
	return bytes.HasPrefix(source, []byte("-- no-transaction"))
}

// txTracker follows the block state of one migration's statements on each
// possible path.
type txTracker struct {
	paths []*txPath
}

// newTxTracker starts a migration of n statements. A marked migration is
// followed without sqlx's transaction when the set's sqlx reads the
// marker, in it when it does not, and both ways when that is unknown (C4,
// sqlxversion.go).
func newTxTracker(m *migration, n int, marker markerReading) *txTracker {
	sqlx := &txPath{state: blockSqlx}
	if !hasNoTransactionMarker(m.source) || marker == markerIgnored {
		return &txTracker{paths: []*txPath{sqlx}}
	}
	state := blockImplicit
	if n == 1 {
		state = blockNone
	}
	noTx := &txPath{state: state, noTx: true}
	if marker == markerRead {
		return &txTracker{paths: []*txPath{noTx}}
	}
	return &txTracker{paths: []*txPath{sqlx, noTx}}
}

// inBlock reports whether the next statement may run inside a
// transaction block on some path (C4: CREATE and DROP INDEX CONCURRENTLY
// are accepted only outside one).
func (tr *txTracker) inBlock() bool {
	for _, p := range tr.paths {
		if p.state != blockNone {
			return true
		}
	}
	return false
}

// txOutcome is the P6 table's verdict for one statement on one path.
type txOutcome struct {
	// refused is a refusal text; undo a rollback of earlier changes.
	refused string
	undo    string
}

// step applies one statement to every path and returns the verdict: ""
// when every path runs it with the same effect, otherwise the reason the
// migration may fail (a refusal, an undo the pass does not model, or
// paths that disagree), with the precondition id it cites.
func (tr *txTracker) step(toks []token, src []byte) (why string, id string) {
	kind, name, what := txStatement(toks, src)
	id = "T1.P6"
	if kind == txPrevent {
		switch {
		case strings.HasPrefix(what, "CREATE INDEX"):
			id = "M4.P6"
		case strings.HasPrefix(what, "DROP INDEX"):
			id = "M7.P6"
		default:
			id = "F1.P6"
		}
	}
	var outcomes []txOutcome
	for _, p := range tr.paths {
		outcomes = append(outcomes, p.step(kind, name, what))
	}
	first := outcomes[0]
	for _, o := range outcomes {
		if o.undo != "" {
			return o.undo, id
		}
		if o != first {
			return fmt.Sprintf("the statement's outcome depends on whether sqlx runs this migration in its transaction (-- no-transaction is read from sqlx 0.8.0): %s", firstNonEmpty(o.refused, first.refused)), id
		}
	}
	return first.refused, id
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (p *txPath) step(kind txKind, name, what string) txOutcome {
	inBlock := p.state != blockNone
	explicit := p.state == blockSqlx || p.state == blockOwn
	undo := func() txOutcome {
		if p.changed {
			return txOutcome{undo: "ROLLBACK undoes the statements before it in this block; the pass does not revert them"}
		}
		return txOutcome{}
	}
	switch kind {
	case txBegin:
		if !explicit {
			p.state = blockOwn
		}
		return txOutcome{}
	case txCommit:
		if inBlock {
			p.state, p.savepoints, p.changed = blockImplicit, nil, false
		}
		return txOutcome{}
	case txRollback:
		if !inBlock {
			return txOutcome{}
		}
		o := undo()
		p.state, p.savepoints, p.changed = blockImplicit, nil, false
		return o
	case txSavepoint:
		if !explicit {
			return txOutcome{refused: "SAVEPOINT can only be used in transaction blocks (25P01)"}
		}
		p.savepoints = append(p.savepoints, name)
		p.changed = true
		return txOutcome{}
	case txRelease, txRollbackTo:
		if !explicit {
			return txOutcome{refused: "RELEASE and ROLLBACK TO SAVEPOINT can only be used in transaction blocks (25P01)"}
		}
		i := slices.Index(p.savepoints, name)
		if name == "" || i < 0 {
			return txOutcome{refused: fmt.Sprintf("savepoint %q does not exist (3B001)", name)}
		}
		if kind == txRollbackTo {
			return undo()
		}
		p.savepoints = p.savepoints[:i]
		return txOutcome{}
	case txPrepare:
		return txOutcome{refused: "PREPARE TRANSACTION and COMMIT/ROLLBACK PREPARED are not in the P6 table (prepared transactions are disabled under the default settings)"}
	case txLock:
		if !inBlock {
			return txOutcome{refused: "LOCK TABLE can only be used in transaction blocks (25P01)"}
		}
		p.changed = true
		return txOutcome{}
	case txPrevent:
		if inBlock {
			return txOutcome{refused: what + " cannot run inside a transaction block (25001)"}
		}
		return txOutcome{}
	}
	p.changed = true
	return txOutcome{}
}

// finish reports a path that ends inside a block it opened in a
// no-transaction migration: sqlx does not commit it, so the statements'
// effects are unknown.
func (tr *txTracker) finish() string {
	for _, p := range tr.paths {
		if p.noTx && p.state == blockOwn {
			return "the query string ends inside a transaction block it opened, which sqlx does not commit in a no-transaction migration"
		}
	}
	return ""
}

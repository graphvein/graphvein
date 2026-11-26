package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// T018a2 Revision 1.9, step U-b: state records. Each rule (Y03, Y04, Y13,
// Y23, Y24, Y27-Y29, Y33, Y34, Y38) keeps a record of what the set did
// before the statement (transaction modes, constraint deferrability, role
// membership edges, materialized views, inheritance children's columns,
// partition bounds, installed extension versions, roles' dependents) and
// judges the statement against it. As in U-a, a refusal and a possible
// failure both make the migration's applied state unknown (unknownFact);
// the text says which it is and ends with the rule's precondition id.

// rev19bState is the state of the Revision 1.9 U-b rules.
type rev19bState struct {
	// tx are the modes of the transaction in force and the session's
	// transaction defaults (Y03).
	tx txModes
	// modelled is set while the current migration runs in sqlx's
	// transaction on its only path (Y03; a no-transaction migration runs
	// in implicit transactions the pass does not model).
	modelled bool
	// deferrable is each set table's constraints' deferrability, by name,
	// as written (Y04); otherConstraints the constraint names of domains
	// and constraint triggers.
	deferrable       map[*table]map[string]tri
	otherConstraints map[string]bool
	// roles is the record of the set's roles (Y13, Y38).
	roles roleRecord
	// matviews are the materialized views the set created, by written
	// name (Y24).
	matviews map[string]*matview
	// inhCols are the columns of the INHERITS children the pass holds as
	// name-only nodes (Y27); nil when not known.
	inhCols map[*table]map[string]inhColumn
	// parts are the partitioned tables the set created (Y29).
	parts map[*table]*partInfo
	// extensions are the extensions the set installed, with the version
	// ("" when not known; Y33, Y34).
	extensions map[string]string
}

// tri is a fact the pass may not know.
type tri int8

const (
	triNo tri = iota
	triYes
	triUnknown
)

// txModes are the transaction modes Y03 follows (variable.c
// check_transaction_read_only, check_transaction_isolation,
// check_transaction_deferrable; xact.c StartTransaction takes the
// defaults).
type txModes struct {
	readOnly tri
	// iso is the isolation level ("read committed", ...); "" unknown.
	iso string
	// snapshot is FirstSnapshotSet: a statement that takes a snapshot ran
	// in the transaction (pquery.c PlannedStmtRequiresSnapshot).
	snapshot bool
	// defReadOnly, defIso and defDeferrable are the session defaults
	// (default_transaction_read_only, default_transaction_isolation,
	// default_transaction_deferrable; E4: off, read committed, off).
	defReadOnly   tri
	defIso        string
	defDeferrable tri
	// set is false until the first migration starts the record.
	set bool
}

// rev19bBegin starts a migration (Y03): sqlx's transaction (or the
// implicit ones of a no-transaction migration) starts with the session
// defaults.
func (s *state) rev19bBegin(tx *txTracker) {
	r := &s.rev19b
	if !r.tx.set {
		r.tx = txModes{defReadOnly: triNo, defIso: "read committed", defDeferrable: triNo, set: true}
	}
	if r.extensions == nil {
		// initdb installs plpgsql in every database (Y33).
		r.extensions = map[string]string{"plpgsql": "1.0"}
	}
	r.modelled = len(tx.paths) == 1 && !tx.paths[0].noTx
	r.tx.newTransaction()
}

// newTransaction starts a transaction with the session's defaults.
func (m *txModes) newTransaction() {
	m.readOnly, m.iso, m.snapshot = m.defReadOnly, m.defIso, false
}

// rev19bBefore applies the U-b rules to one statement, before it changes
// the state.
func (s *state) rev19bBefore(st statement, a analysis) {
	toks := st.tokens
	if s.failure != nil || len(toks) == 0 {
		return
	}
	s.y03Statement(toks)
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case s.failure != nil:
	case c.peekKw("set", "constraints"):
		s.y04SetConstraints(toks)
	case c.peekKw("grant"):
		s.y13Grant(toks)
	case c.peekKw("alter", "group"):
		s.y13AlterGroup(toks)
	case c.peekKw("drop", "role"), c.peekKw("drop", "user"), c.peekKw("drop", "group"):
		s.y38DropRole(toks)
	case c.peekKw("refresh", "materialized", "view"):
		s.y24Refresh(toks)
	case c.peekKw("create", "extension"):
		s.y33CreateExtension(toks)
	case c.peekKw("alter", "extension"):
		s.y33AlterExtension(toks)
	case c.peekKw("drop", "extension"):
		s.y34DropExtension(toks)
	case c.peekKw("create"):
		s.y28CreateTable(toks)
	}
	if s.failure == nil && a.alter != nil {
		s.y27AlterTable(a.alter)
	}
	if s.failure == nil {
		s.y29Bound(toks, a)
	}
	if s.failure == nil {
		s.y23Values(toks)
	}
}

// rev19bAfter records what a statement the pass applied changed.
func (s *state) rev19bAfter(st statement, a analysis) {
	toks := st.tokens
	if len(toks) == 0 {
		return
	}
	s.y04Record(toks)
	s.y24Record(toks, a)
	s.y27Record(toks)
	s.y29Record(toks, a)
	s.y33Record(toks)
	if a.alter != nil {
		s.y27After(a.alter)
	}
	s.y38Record(toks)
	s.y13After(toks)
	kind, _, _ := txStatement(toks, s.m.source)
	if kind == txCommit || kind == txRollback {
		// Y03: the rest of the string runs in a new transaction.
		s.rev19b.tx.newTransaction()
	} else if takesSnapshot(toks) {
		s.rev19b.tx.snapshot = true
	}
}

// rev19bEnd applies Y03 to sqlx's bookkeeping at the end of a migration:
// the INSERT INTO _sqlx_migrations runs in the transaction in force (or,
// after the string committed sqlx's, in a new one), and the UPDATE of
// execution_time after the commit in a new transaction with the session
// defaults; a read-only transaction refuses both (25006). sqlx stops the
// run when the UPDATE fails (migrate.rs apply, `?` on its result), so
// every later migration is not applied (Q1.P13).
func (s *state) rev19bEnd(tx *txTracker) {
	r := &s.rev19b
	if !r.modelled {
		return
	}
	m := r.tx
	ro := m.defReadOnly
	if st := tx.paths[0].state; st == blockSqlx || st == blockOwn {
		ro = m.readOnly
	}
	switch ro {
	case triYes:
		s.unknownFact("sqlx records the migration with an INSERT INTO _sqlx_migrations in a read-only transaction (Postgres refuses, 25006)" + precondition("Q1.P13"))
	case triUnknown:
		s.unknownFact("sqlx's INSERT INTO _sqlx_migrations may run in a read-only transaction" + precondition("Q1.P13"))
	}
	if s.failure != nil || s.laterMayFail != "" {
		return
	}
	switch m.defReadOnly {
	case triYes:
		s.laterMayFail = "sqlx's UPDATE of _sqlx_migrations after an earlier migration committed runs in a read-only transaction (default_transaction_read_only is on; Postgres refuses, 25006), which stops the run" + precondition("Q1.P13")
	case triUnknown:
		s.laterMayFail = "sqlx's UPDATE of _sqlx_migrations after an earlier migration committed may run in a read-only transaction, which stops the run" + precondition("Q1.P13")
	}
}

// snapshotFree are the statement heads that take no snapshot
// (PlannedStmtRequiresSnapshot: TransactionStmt, LockStmt,
// VariableSetStmt, VariableShowStmt, ConstraintsSetStmt, FetchStmt,
// ListenStmt, NotifyStmt, UnlistenStmt, CheckPointStmt).
var snapshotFree = map[string]bool{
	"begin": true, "start": true, "commit": true, "end": true, "abort": true, "rollback": true,
	"savepoint": true, "release": true, "lock": true, "set": true, "reset": true, "show": true,
	"fetch": true, "move": true, "listen": true, "notify": true, "unlisten": true, "checkpoint": true,
}

// takesSnapshot reports whether a statement sets FirstSnapshotSet.
func takesSnapshot(toks []token) bool {
	if len(toks) == 0 || toks[0].kind != tokIdent {
		return len(toks) > 0
	}
	h := toks[0].value
	if h == "prepare" {
		// PREPARE TRANSACTION is a TransactionStmt; PREPARE name AS ... a
		// PrepareStmt, which takes one.
		return !(len(toks) > 1 && toks[1].kind == tokIdent && toks[1].value == "transaction")
	}
	if h == "start" {
		return !(len(toks) > 1 && toks[1].kind == tokIdent && toks[1].value == "transaction")
	}
	return !snapshotFree[h]
}

// readOnlyFree are the statement heads utility.c
// ClassifyUtilityCommandAsReadOnly lets run in a read-only transaction
// whatever they name (transaction control, LOCK, SET, SHOW, DISCARD,
// DEALLOCATE, PREPARE, FETCH, MOVE, CLOSE, LISTEN, NOTIFY, UNLISTEN,
// LOAD, CLUSTER, REINDEX, VACUUM, ANALYZE, CHECKPOINT). Queries, calls,
// EXECUTE, EXPLAIN, DECLARE and COPY may run a write; every other
// statement is refused (PreventCommandIfReadOnly or a DML write).
var readOnlyFree = map[string]bool{
	"begin": true, "start": true, "commit": true, "end": true, "abort": true, "rollback": true,
	"savepoint": true, "release": true, "lock": true, "set": true, "reset": true, "show": true,
	"discard": true, "deallocate": true, "prepare": true, "fetch": true, "move": true, "close": true,
	"listen": true, "notify": true, "unlisten": true, "load": true, "cluster": true, "reindex": true,
	"vacuum": true, "analyze": true, "analyse": true, "checkpoint": true,
}

// readOnlyMay are the heads that are refused in a read-only transaction
// only when what they run writes.
var readOnlyMay = map[string]bool{
	"select": true, "values": true, "table": true, "with": true, "call": true, "do": true,
	"execute": true, "explain": true, "declare": true, "copy": true,
}

// isolationLevels are transaction_isolation's values (guc_tables.c
// isolation_level_options).
var isolationLevels = map[string]bool{"serializable": true, "repeatable read": true, "read committed": true, "read uncommitted": true}

// y03Statement applies Y03 (N2.P6, N2.P13) to one statement: the
// read-only guard, SET TRANSACTION and its GUC forms against the
// transaction's snapshot and savepoints, SET TRANSACTION SNAPSHOT, and the
// session defaults.
func (s *state) y03Statement(toks []token) {
	r := &s.rev19b
	m := &r.tx
	head := ""
	if toks[0].kind == tokIdent {
		head = toks[0].value
	}
	if r.modelled && m.readOnly != triNo && !readOnlyFree[head] {
		switch {
		case m.readOnly == triUnknown:
			s.unknownFact("the transaction may be read-only, and Postgres refuses a write in a read-only transaction (25006)" + precondition("N2.P6"))
		case readOnlyMay[head]:
			s.unknownFact("the transaction is read-only, and the statement may write (Postgres refuses a write in a read-only transaction, 25006)" + precondition("N2.P6"))
		default:
			s.unknownFact("the transaction is read-only (Postgres refuses the statement, 25006)" + precondition("N2.P6"))
		}
		return
	}
	if head != "set" && head != "reset" {
		return
	}
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	local := false
	if c.acceptKw("local") {
		local = true
	} else if !c.peekKw("session", "characteristics") {
		c.acceptKw("session")
	}
	if head == "reset" {
		s.y03Reset(c)
		return
	}
	switch {
	case c.acceptKw("transaction", "snapshot"):
		// The exported snapshot is outside the pass's state; after the
		// first snapshot, or under READ COMMITTED, Postgres refuses it.
		if r.modelled && m.snapshot {
			s.unknownFact("SET TRANSACTION SNAPSHOT after a statement that took a snapshot (Postgres refuses, 25001)" + precondition("N2.P6"))
		} else if r.modelled && (m.iso == "read committed" || m.iso == "read uncommitted") {
			s.unknownFact("SET TRANSACTION SNAPSHOT under READ COMMITTED (Postgres refuses, 0A000)" + precondition("N2.P6"))
		} else {
			s.unknownFact("SET TRANSACTION SNAPSHOT imports a snapshot the pass does not know" + precondition("N2.P6"))
		}
	case c.acceptKw("transaction"):
		modes, ok := transactionModes(c)
		if !ok {
			s.unknownFact("SET TRANSACTION: the modes are not read" + precondition("N2.P6"))
			return
		}
		for _, md := range modes {
			if !s.y03Mode(md[0], md[1]) {
				return
			}
		}
	case c.acceptKw("session", "characteristics", "as", "transaction"):
		modes, ok := transactionModes(c)
		if !ok || !r.modelled {
			s.unknownFact("SET SESSION CHARACTERISTICS: the transaction defaults are not followed here" + precondition("N2.P13"))
			return
		}
		if local {
			return
		}
		for _, md := range modes {
			s.y03Default(md[0], md[1])
		}
	default:
		name, parts, ok := gucName(c)
		if !ok || parts != 1 {
			return
		}
		var kind string
		switch name {
		case "transaction_isolation":
			kind = "isolation"
		case "transaction_read_only":
			kind = "read_only"
		case "transaction_deferrable":
			kind = "deferrable"
		case "default_transaction_isolation", "default_transaction_read_only", "default_transaction_deferrable":
		default:
			return
		}
		if !c.acceptKw("to") && !(!c.done() && c.tok().kind == tokOp && c.tok().value == "=" && c.next()) {
			return
		}
		vals := setValues(c)
		v := ""
		if len(vals) == 1 {
			v = strings.ToLower(vals[0])
		}
		if kind != "" {
			value, ok := modeValue(kind, v)
			if !ok {
				s.unknownFact(fmt.Sprintf("SET %s: the value is not read%s", name, precondition("N2.P6")))
				return
			}
			s.y03Mode(kind, value)
			return
		}
		if !r.modelled {
			s.unknownFact(fmt.Sprintf("SET %s: the transaction defaults are not followed in a no-transaction migration%s", name, precondition("N2.P13")))
			return
		}
		if local {
			return
		}
		kind = strings.TrimPrefix(name, "default_transaction_")
		if v == "default" {
			s.y03Default(kind, "default")
			return
		}
		value, ok := modeValue(kind, v)
		if !ok {
			value = "unknown"
		}
		s.y03Default(kind, value)
	}
}

// modeValue reads a value of a transaction mode GUC: a boolean word or an
// isolation level.
func modeValue(kind, v string) (string, bool) {
	if kind == "isolation" {
		return v, isolationLevels[v]
	}
	switch v {
	case "on", "true", "yes", "1", "t", "y":
		return "on", true
	case "off", "false", "no", "0", "f", "n":
		return "off", true
	}
	return "", false
}

// transactionModes reads gram.y transaction_mode_list: each mode as
// (kind, value) with kind isolation, read_only or deferrable.
func transactionModes(c *cursor) ([][2]string, bool) {
	var out [][2]string
	for !c.done() {
		switch {
		case c.acceptKw("isolation", "level"):
			switch {
			case c.acceptKw("serializable"):
				out = append(out, [2]string{"isolation", "serializable"})
			case c.acceptKw("repeatable", "read"):
				out = append(out, [2]string{"isolation", "repeatable read"})
			case c.acceptKw("read", "committed"):
				out = append(out, [2]string{"isolation", "read committed"})
			case c.acceptKw("read", "uncommitted"):
				out = append(out, [2]string{"isolation", "read uncommitted"})
			default:
				return nil, false
			}
		case c.acceptKw("read", "only"):
			out = append(out, [2]string{"read_only", "on"})
		case c.acceptKw("read", "write"):
			out = append(out, [2]string{"read_only", "off"})
		case c.acceptKw("not", "deferrable"):
			out = append(out, [2]string{"deferrable", "off"})
		case c.acceptKw("deferrable"):
			out = append(out, [2]string{"deferrable", "on"})
		default:
			return nil, false
		}
		c.acceptPunct(",")
	}
	return out, len(out) > 0
}

// y03Mode applies one transaction mode to the transaction in force; it
// reports false when it recorded a failure.
func (s *state) y03Mode(kind, value string) bool {
	r := &s.rev19b
	m := &r.tx
	if !r.modelled {
		s.unknownFact("SET TRANSACTION in a no-transaction migration runs in an implicit transaction the pass does not model" + precondition("N2.P6"))
		return false
	}
	sub := s.inSavepoint()
	switch kind {
	case "isolation":
		if m.iso == value {
			return true
		}
		switch {
		case m.snapshot:
			s.unknownFact("SET TRANSACTION ISOLATION LEVEL to another level after a statement that took a snapshot (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		case sub:
			s.unknownFact("SET TRANSACTION ISOLATION LEVEL to another level inside a savepoint (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		case m.iso == "":
			s.unknownFact("SET TRANSACTION ISOLATION LEVEL: the transaction's level is not known" + precondition("N2.P6"))
			return false
		}
		m.iso = value
	case "read_only":
		if value == "on" {
			m.readOnly = triYes
			return true
		}
		if m.readOnly == triNo {
			return true
		}
		switch {
		case m.readOnly == triUnknown:
			s.unknownFact("SET TRANSACTION READ WRITE: whether the transaction is read-only is not known" + precondition("N2.P6"))
			return false
		case sub:
			s.unknownFact("SET TRANSACTION READ WRITE inside a savepoint of a read-only transaction (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		case m.snapshot:
			s.unknownFact("SET TRANSACTION READ WRITE after a statement that took a snapshot in a read-only transaction (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		}
		m.readOnly = triNo
	case "deferrable":
		switch {
		case sub:
			s.unknownFact("SET TRANSACTION [NOT] DEFERRABLE inside a savepoint (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		case m.snapshot:
			s.unknownFact("SET TRANSACTION [NOT] DEFERRABLE after a statement that took a snapshot (Postgres refuses, 25001)" + precondition("N2.P6"))
			return false
		}
	}
	return true
}

// y03Default records a session default a SET (not SET LOCAL) or SET
// SESSION CHARACTERISTICS set; it lasts once the migration commits, and
// takes effect at the next transaction.
func (s *state) y03Default(kind, value string) {
	m := &s.rev19b.tx
	switch kind {
	case "read_only":
		switch value {
		case "on":
			m.defReadOnly = triYes
		case "off", "default":
			m.defReadOnly = triNo
		default:
			m.defReadOnly = triUnknown
		}
	case "isolation":
		switch {
		case value == "default":
			m.defIso = "read committed"
		case isolationLevels[value]:
			m.defIso = value
		default:
			m.defIso = ""
		}
	case "deferrable":
		switch value {
		case "on":
			m.defDeferrable = triYes
		case "off", "default":
			m.defDeferrable = triNo
		default:
			m.defDeferrable = triUnknown
		}
	}
}

// y03Reset applies RESET of the transaction defaults (back to E4's) and
// RESET ALL.
func (s *state) y03Reset(c *cursor) {
	if c.acceptKw("all") {
		if !s.rev19b.modelled {
			s.unknownFact("RESET ALL: the transaction defaults are not followed in a no-transaction migration" + precondition("N2.P13"))
			return
		}
		for _, k := range []string{"read_only", "isolation", "deferrable"} {
			s.y03Default(k, "default")
		}
		return
	}
	name, parts, ok := gucName(c)
	if !ok || parts != 1 || !strings.HasPrefix(name, "default_transaction_") {
		return
	}
	if !s.rev19b.modelled {
		s.unknownFact(fmt.Sprintf("RESET %s: the transaction defaults are not followed in a no-transaction migration%s", name, precondition("N2.P13")))
		return
	}
	s.y03Default(strings.TrimPrefix(name, "default_transaction_"), "default")
}

// inSavepoint reports whether a savepoint is open on the migration's
// path (a subtransaction).
func (s *state) inSavepoint() bool {
	return s.tx != nil && len(s.tx.paths) == 1 && len(s.tx.paths[0].savepoints) > 0
}

// constraintStarters are the words that start a column constraint
// (gram.y ColConstraintElem) or a table constraint: a deferrability
// clause after one belongs to it, not to an earlier named constraint.
var constraintStarters = map[string]bool{
	"check": true, "default": true, "unique": true, "primary": true, "references": true,
	"generated": true, "collate": true, "null": true, "exclude": true, "foreign": true,
}

// constraintDeferrability reads the deferrability of every constraint a
// statement names with CONSTRAINT n (Y04): yes when its clauses include
// DEFERRABLE without NOT, no otherwise; unknown when the clauses cannot be
// delimited. ALTER CONSTRAINT n [NOT] DEFERRABLE is read the same way.
func constraintDeferrability(toks []token) map[string]tri {
	out := map[string]tri{}
	for i := 0; i+1 < len(toks); i++ {
		if toks[i].kind != tokIdent || toks[i].value != "constraint" {
			continue
		}
		if i > 0 && toks[i-1].kind == tokIdent {
			switch toks[i-1].value {
			case "drop", "rename", "validate", "create":
				continue
			}
		}
		n := toks[i+1]
		if n.kind != tokIdent && n.kind != tokQuoted || n.value == "trigger" {
			continue
		}
		v, depth, body := triNo, 0, ""
	scan:
		for j := i + 2; j < len(toks); j++ {
			t := toks[j]
			if t.kind == tokPunct {
				switch t.value {
				case "(", "[":
					depth++
				case ")", "]":
					depth--
					if depth < 0 {
						break scan
					}
				case ",", ";":
					if depth == 0 {
						break scan
					}
				}
				continue
			}
			if depth != 0 || t.kind != tokIdent {
				continue
			}
			prev := toks[j-1]
			after := func(w string) bool { return prev.kind == tokIdent && prev.value == w }
			switch {
			case t.value == "constraint":
				break scan
			case t.value == "deferrable":
				if after("not") {
					v = triNo
				} else {
					v = triYes
				}
			case t.value == "not" && j+1 < len(toks) && toks[j+1].kind == tokIdent && toks[j+1].value == "null" && body != "":
				break scan
			case constraintStarters[t.value]:
				if after("set") || after("nulls") || t.value == "null" && after("not") {
					continue
				}
				switch {
				case body == "":
					body = t.value
				case body == "foreign" && t.value == "references":
				case body == "exclude":
				default:
					break scan
				}
			}
		}
		if prev, ok := out[n.value]; ok && prev != v {
			v = triUnknown
		}
		out[n.value] = v
	}
	return out
}

// y04Record records the deferrability of the constraints a CREATE TABLE or
// ALTER TABLE names, and the constraint names other statements create
// in a schema (domains, constraint triggers), after the statement applied.
func (s *state) y04Record(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	r := &s.rev19b
	switch {
	case c.peekKw("create", "domain"), c.peekKw("alter", "domain"), c.peekKw("create", "constraint", "trigger"), c.peekKw("create", "or", "replace", "constraint", "trigger"):
		if r.otherConstraints == nil {
			r.otherConstraints = map[string]bool{}
		}
		for i, t := range toks {
			if t.kind == tokIdent && t.value == "constraint" && i+1 < len(toks) {
				n := toks[i+1]
				if n.kind == tokIdent && n.value == "trigger" && i+2 < len(toks) {
					n = toks[i+2]
				}
				r.otherConstraints[n.value] = true
			}
		}
		return
	}
	var q qname
	var err error
	switch {
	case c.acceptKw("create"):
		for _, w := range []string{"global", "local", "temp", "temporary", "unlogged"} {
			c.acceptKw(w)
		}
		if !c.acceptKw("table") {
			return
		}
		c.acceptKw("if", "not", "exists")
		q, err = c.name()
	case c.acceptKw("alter", "table"):
		c.acceptKw("if", "exists")
		c.acceptKw("only")
		q, err = c.name()
	default:
		return
	}
	if err != nil {
		return
	}
	defs := constraintDeferrability(toks)
	if len(defs) == 0 {
		return
	}
	t, _ := s.lookup(q)
	if t == nil {
		return
	}
	if r.deferrable == nil {
		r.deferrable = map[*table]map[string]tri{}
	}
	if r.deferrable[t] == nil {
		r.deferrable[t] = map[string]tri{}
	}
	for n, v := range defs {
		r.deferrable[t][n] = v
	}
}

// constraintNamed reports whether t holds a constraint named n (C3's
// names: a key, a foreign key, a CHECK or an EXCLUDE constraint).
func constraintNamed(t *table, n string) bool {
	if t.primaryKey != nil && t.primaryKey.name != nil && *t.primaryKey.name == n {
		return true
	}
	for _, u := range t.uniques {
		if u.name != nil && *u.name == n {
			return true
		}
	}
	for _, f := range t.foreignKeys {
		if f.name != nil && *f.name == n {
			return true
		}
	}
	for _, ch := range t.checks {
		if ch.name == n {
			return true
		}
	}
	return false
}

// y04SetConstraints applies Y04 (N2.P4) to SET CONSTRAINTS names
// {DEFERRED | IMMEDIATE} (trigger.c AfterTriggerSetState 5800-5870: an
// unqualified name is looked up in the first schema of the search path
// that holds a constraint of that name, every match there is taken; a
// match that is not deferrable is refused only for DEFERRED, 42809; no
// match is refused, 42704). SET CONSTRAINTS ALL is accepted. The pass
// claims the statement when every set table it holds has exact
// constraint names (C3), the path is E4's, no temporary table or code may
// add a constraint of the name, and the name is held with its
// deferrability read.
func (s *state) y04SetConstraints(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	if c.acceptKw("all") {
		return
	}
	var names []qname
	for {
		q, err := c.name()
		if err != nil {
			s.unknownFact("SET CONSTRAINTS: a name is not a qualified_name (gram.y ConstraintsSetStmt; Postgres refuses, 42601)" + precondition("N2.P4"))
			return
		}
		names = append(names, q)
		if !c.acceptPunct(",") {
			break
		}
	}
	deferred := c.acceptKw("deferred")
	if !deferred && !c.acceptKw("immediate") || !c.done() {
		s.unknownFact("SET CONSTRAINTS: the statement is not ALL or a list of names followed by DEFERRED or IMMEDIATE (gram.y ConstraintsSetStmt; Postgres refuses, 42601)" + precondition("N2.P4"))
		return
	}
	for _, q := range names {
		if cat := pgKeywordCategories[q.name]; q.schema == nil && (cat == 'R' || cat == 'T') && !writtenQuoted(toks, q.name) {
			s.unknownFact(fmt.Sprintf("SET CONSTRAINTS: keyword %s is not a ColId (Postgres refuses, 42601)%s", q.name, precondition("N2.P4")))
			return
		}
	}
	r := &s.rev19b
	for _, q := range names {
		what := "SET CONSTRAINTS " + q.String()
		may := ""
		switch {
		case q.schema != nil:
			may = "a qualified constraint name is not followed"
		case s.pathChanged:
			may = "search_path may have changed, so the schema searched first is not known"
		case len(s.temps) > 0:
			may = "a temporary table's constraints are searched first"
		case r.otherConstraints[q.name]:
			may = "a domain or constraint trigger of the set may hold a constraint of this name"
		case len(s.code.tables) > 0:
			may = "code may have created a table with a constraint of this name"
		}
		found, nonDeferrable := false, false
		for _, t := range s.tables {
			if may != "" {
				break
			}
			if !constraintNamesExact(t) {
				may = fmt.Sprintf("the constraint names of %s are not known exactly", t.q)
				break
			}
			if !constraintNamed(t, q.name) {
				continue
			}
			if t.q.schema != nil {
				may = fmt.Sprintf("%s holds it in a schema the pass does not place on the path", t.q)
				break
			}
			found = true
			switch r.deferrable[t][q.name] {
			case triNo:
				nonDeferrable = true
			case triUnknown:
				may = "its deferrability is not read"
			}
		}
		if may == "" && !found {
			may = "the set holds no constraint of this name exactly"
		}
		if deferred && nonDeferrable && may == "" {
			s.unknownFact(fmt.Sprintf("%s DEFERRED: constraint %s is not deferrable (Postgres refuses, 42809)%s", what, q.name, precondition("N2.P4")))
			return
		}
		if may != "" {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, may, precondition("N2.P4")))
			return
		}
	}
}

// writtenQuoted reports whether the statement writes n as a quoted
// identifier.
func writtenQuoted(toks []token, n string) bool {
	for _, t := range toks {
		if t.kind == tokQuoted && t.value == n {
			return true
		}
	}
	return false
}

// migratorRole stands for the role the migrations run as (E6): the
// creator of every role the set creates is a member of it
// (user.c CreateRole grants a new role to a creator that is not a
// superuser, with createrole_self_grant).
const migratorRole = "\x00migrator"

// roleRecord is the U-b record of roles (Y13, Y38).
type roleRecord struct {
	// set are the roles the set created (a top-level CREATE ROLE, USER or
	// GROUP the pass read), by name; dependents their shared dependents.
	set map[string]*setRole
	// memberOf are the membership edges: member -> roles it is a member
	// of (every edge into or out of a set role, and none between two
	// outside roles).
	memberOf map[string]map[string]bool
	// unknown says why the edges are not all recorded (code or GRANTED BY
	// changed memberships); "" when they are.
	unknown string
	// current is the role SET ROLE put in force ("" the migrating role);
	// local is set when SET LOCAL ROLE did (it ends with the migration's
	// transaction); authorization is set after SET SESSION AUTHORIZATION.
	current       string
	local         bool
	authorization bool
}

// setRole is one role the set created.
type setRole struct {
	deps []*roleDep
	// unknown says why the role may have dependents the record does not
	// hold ("" when it holds them all).
	unknown string
}

// roleDep is one shared dependent of a set role (Y38): an object it owns
// or a privilege granted to it. t is the set table the dependent belongs
// to (it ends when the table is dropped); nil for one that lasts until
// DROP OWNED. privs are a table grant's privileges, policy a policy's
// name.
type roleDep struct {
	t      *table
	owner  bool
	privs  map[string]bool
	policy string
	text   string
}

// roleName reads a RoleSpec: a name, CURRENT_USER, CURRENT_ROLE,
// SESSION_USER or PUBLIC (the last returned as "public").
func (r *roleRecord) roleName(c *cursor) (string, bool) {
	if c.done() {
		return "", false
	}
	t := c.tok()
	if t.kind == tokIdent {
		switch t.value {
		case "current_user", "current_role":
			c.i++
			if r.current != "" {
				return r.current, true
			}
			return migratorRole, true
		case "session_user":
			c.i++
			return migratorRole, true
		case "public":
			c.i++
			return "public", true
		}
	}
	n, err := c.ident()
	return n, err == nil
}

// roleList reads RoleSpec [, ...].
func (r *roleRecord) roleList(c *cursor) ([]string, bool) {
	var out []string
	for {
		n, ok := r.roleName(c)
		if !ok {
			return nil, false
		}
		out = append(out, n)
		if !c.acceptPunct(",") {
			return out, true
		}
	}
}

func (r *roleRecord) addEdge(member, role string) {
	if r.memberOf == nil {
		r.memberOf = map[string]map[string]bool{}
	}
	if r.memberOf[member] == nil {
		r.memberOf[member] = map[string]bool{}
	}
	r.memberOf[member][role] = true
}

// up is a and every role a is transitively a member of.
func (r *roleRecord) up(a string) map[string]bool {
	out := map[string]bool{a: true}
	work := []string{a}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		for y := range r.memberOf[x] {
			if !out[y] {
				out[y] = true
				work = append(work, y)
			}
		}
	}
	return out
}

// down is b and every role transitively a member of b.
func (r *roleRecord) down(b string) map[string]bool {
	out := map[string]bool{b: true}
	for changed := true; changed; {
		changed = false
		for m, roles := range r.memberOf {
			if out[m] {
				continue
			}
			for y := range roles {
				if out[y] {
					out[m], changed = true, true
					break
				}
			}
		}
	}
	return out
}

// outside reports a role the set did not create (E1).
func (r *roleRecord) outside(n string) bool {
	return r.set[n] == nil
}

// grantMembership applies Y13 (N1.P2) to making member a member of role
// (GRANT role TO member, CREATE ROLE ... IN ROLE / ROLE / ADMIN, ALTER
// GROUP ... ADD USER; user.c AddRoleMems 1743-1746: refused, 0LP01, when
// role is member itself or already a member of member), and records the
// edge. It reports false when it recorded a failure.
func (s *state) grantMembership(role, member, what string) bool {
	r := &s.rev19b.roles
	show := func(n string) string {
		if n == migratorRole {
			return "the migrating role"
		}
		return n
	}
	if role == "public" || member == "public" {
		return true
	}
	if role == member {
		s.unknownFact(fmt.Sprintf("%s: role %s cannot be a member of itself (Postgres refuses, 0LP01)%s", what, show(role), precondition("N1.P2")))
		return false
	}
	up := r.up(role)
	if up[member] && r.unknown == "" {
		s.unknownFact(fmt.Sprintf("%s: role %s is a member of role %s (Postgres refuses, 0LP01)%s", what, show(role), show(member), precondition("N1.P2")))
		return false
	}
	why := r.unknown
	if why == "" {
		upOut, downOut := false, false
		for n := range up {
			upOut = upOut || r.outside(n)
		}
		for n := range r.down(member) {
			downOut = downOut || r.outside(n)
		}
		if upOut && downOut {
			why = "memberships between roles outside the set are not known"
		}
	}
	if why != "" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s may already be a member of %s (%s)%s", what, show(role), show(member), why, precondition("N1.P2")))
		return false
	}
	r.addEdge(member, role)
	return true
}

// y13CreateRole records a role CREATE ROLE, USER or GROUP created and
// applies Y13 to its IN ROLE, ROLE, USER and ADMIN options (user.c
// CreateRole: addroleto, then the members).
func (s *state) y13CreateRole(toks []token) {
	r := &s.rev19b.roles
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	n, err := c.ident()
	if err != nil || reservedName(n) {
		return
	}
	what := "CREATE " + strings.ToUpper(toks[1].value) + " " + n
	var in, members []string
	c.acceptKw("with")
	for !c.done() {
		var list *[]string
		switch {
		case c.acceptKw("in", "role"), c.acceptKw("in", "group"):
			list = &in
		case c.acceptKw("role"), c.acceptKw("user"), c.acceptKw("admin"):
			list = &members
		default:
			c.i++
			continue
		}
		names, ok := r.roleList(c)
		if !ok {
			return
		}
		*list = append(*list, names...)
	}
	if r.set == nil {
		r.set = map[string]*setRole{}
	}
	delete(r.memberOf, n)
	r.set[n] = &setRole{}
	r.addEdge(migratorRole, n)
	for _, x := range in {
		if !s.grantMembership(x, n, what+" IN ROLE "+x) {
			return
		}
	}
	for _, x := range members {
		if !s.grantMembership(n, x, what+" ROLE "+x) {
			return
		}
	}
}

// y13Grant applies Y13 to GRANT roles TO members (a role grant: no ON
// clause) and records the edges; GRANTED BY makes the record unknown.
func (s *state) y13Grant(toks []token) {
	r := &s.rev19b.roles
	if depth0(toks, 1, "on") >= 0 {
		return
	}
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	roles, ok := r.roleList(c)
	if !ok || !c.acceptKw("to") {
		return
	}
	members, ok := r.roleList(c)
	if !ok {
		return
	}
	if depth0(toks, c.i, "granted") >= 0 {
		r.unknown = "a GRANTED BY grant is not followed"
	}
	for _, a := range roles {
		for _, b := range members {
			if !s.grantMembership(a, b, "GRANT "+a+" TO "+b) {
				return
			}
		}
	}
}

// y13Revoke removes the edges REVOKE roles FROM members removes (not
// REVOKE ... OPTION FOR, which keeps the membership).
func (s *state) y13Revoke(toks []token) {
	r := &s.rev19b.roles
	if depth0(toks, 1, "on") >= 0 {
		return
	}
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	if c.acceptKw("admin", "option", "for") || c.acceptKw("inherit", "option", "for") || c.acceptKw("set", "option", "for") {
		return
	}
	roles, ok := r.roleList(c)
	if !ok || !c.acceptKw("from") {
		return
	}
	members, ok := r.roleList(c)
	if !ok {
		return
	}
	if depth0(toks, c.i, "granted") >= 0 {
		r.unknown = "a GRANTED BY revoke is not followed"
	}
	for _, a := range roles {
		for _, b := range members {
			delete(r.memberOf[b], a)
		}
	}
}

// y13AlterGroup applies Y13 to ALTER GROUP g ADD USER u [, ...] and
// removes the edges of DROP USER.
func (s *state) y13AlterGroup(toks []token) {
	r := &s.rev19b.roles
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	g, ok := r.roleName(c)
	if !ok {
		return
	}
	add := c.acceptKw("add", "user")
	if !add && !c.acceptKw("drop", "user") {
		return
	}
	users, ok := r.roleList(c)
	if !ok {
		return
	}
	for _, u := range users {
		if !add {
			delete(r.memberOf[u], g)
			continue
		}
		if !s.grantMembership(g, u, "ALTER GROUP "+g+" ADD USER "+u) {
			return
		}
	}
}

// dropRoleNames reads DROP ROLE|USER|GROUP [IF EXISTS] names.
func (r *roleRecord) dropRoleNames(toks []token, src []byte) ([]string, bool) {
	c := &cursor{toks: toks, i: 2, src: src}
	c.acceptKw("if", "exists")
	return r.roleList(c)
}

// forget removes a dropped role and its edges.
func (r *roleRecord) forget(n string) {
	delete(r.set, n)
	delete(r.memberOf, n)
	for _, roles := range r.memberOf {
		delete(roles, n)
	}
}

// rename moves a renamed role's record and edges.
func (r *roleRecord) rename(old, n string) {
	if x := r.set[old]; x != nil {
		delete(r.set, old)
		r.set[n] = x
	}
	if e := r.memberOf[old]; e != nil {
		delete(r.memberOf, old)
		r.memberOf[n] = e
	}
	for _, roles := range r.memberOf {
		if roles[old] {
			delete(roles, old)
			roles[n] = true
		}
	}
	if r.current == old {
		r.current = n
	}
}

// tablePrivileges are the privilege words GRANT and REVOKE take on a
// table (gram.y privilege; MAINTAIN since Postgres 17).
var tablePrivileges = map[string]bool{
	"select": true, "insert": true, "update": true, "delete": true, "truncate": true,
	"references": true, "trigger": true, "maintain": true,
}

// grantTarget reads GRANT|REVOKE [GRANT OPTION FOR] privileges ON
// [TABLE] names TO|FROM roles: the privileges (nil for ALL; "\x00cols"
// for a column list), the tables (nil when the object is not a plain
// table list) and the roles.
func (r *roleRecord) grantTarget(toks []token, src []byte, revoke bool) (privs map[string]bool, tables []qname, roles []string, optionFor, ok bool) {
	c := &cursor{toks: toks, i: 1, src: src}
	if revoke && c.acceptKw("grant", "option", "for") {
		optionFor = true
	}
	on := depth0(toks, c.i, "on")
	if on < 0 {
		return nil, nil, nil, false, false
	}
	all := c.acceptKw("all")
	if all {
		c.acceptKw("privileges")
	} else {
		privs = map[string]bool{}
		for c.i < on {
			t := c.tok()
			switch {
			case t.kind == tokPunct && t.value == "(":
				privs["\x00cols"] = true
				end, err := c.group()
				if err != nil {
					return nil, nil, nil, false, false
				}
				c.i = end
				continue
			case t.kind == tokIdent && tablePrivileges[t.value]:
				privs[t.value] = true
			case t.kind == tokPunct && t.value == ",":
			default:
				privs["\x00other"] = true
			}
			c.i++
		}
	}
	c.i = on + 1
	plain := !c.peekKw("all") && !c.peekKw("schema") && !c.peekKw("sequence") && !c.peekKw("function") &&
		!c.peekKw("procedure") && !c.peekKw("routine") && !c.peekKw("database") && !c.peekKw("domain") &&
		!c.peekKw("type") && !c.peekKw("language") && !c.peekKw("large") && !c.peekKw("foreign") &&
		!c.peekKw("tablespace") && !c.peekKw("parameter")
	c.acceptKw("table")
	to := "to"
	if revoke {
		to = "from"
	}
	at := depth0(toks, c.i, to)
	if at < 0 {
		return nil, nil, nil, false, false
	}
	if plain {
		for c.i < at {
			q, err := c.name()
			if err != nil {
				return nil, nil, nil, false, false
			}
			tables = append(tables, q)
			c.acceptPunct(",")
		}
	}
	c.i = at + 1
	c.acceptKw("group")
	roles, ok = r.roleList(c)
	return privs, tables, roles, optionFor, ok
}

// y38Record records the shared dependents a statement gives the set's
// roles, after it applied (Y38): table grants (ended by REVOKE of the
// same privileges, or the table's drop), ownership (OWNER TO,
// AUTHORIZATION, objects created under SET ROLE), policies TO a role,
// default privileges, and any other statement naming a set role, which
// makes the role's dependents unknown. DROP OWNED BY ends them, REASSIGN
// OWNED BY the owned ones.
func (s *state) y38Record(toks []token) {
	r := &s.rev19b.roles
	if len(r.set) == 0 || len(toks) == 0 || toks[0].kind != tokIdent {
		return
	}
	src := s.m.source
	c := &cursor{toks: toks, src: src}
	text := shortText(statementText(toks, src))
	add := func(n string, d *roleDep) {
		if x := r.set[n]; x != nil {
			d.text = text
			x.deps = append(x.deps, d)
		}
	}
	head := toks[0].value
	named := map[string]bool{}
	for _, t := range toks {
		if (t.kind == tokIdent || t.kind == tokQuoted) && r.set[t.value] != nil {
			named[t.value] = true
		}
	}
	if r.current != "" && r.set[r.current] != nil && head != "set" && head != "reset" && head != "show" {
		// Objects created, and privileges granted, while SET ROLE r is
		// in force belong to r.
		add(r.current, &roleDep{owner: true})
	}
	handled := false
	switch {
	case head == "create" && (c.peekKw("create", "role") || c.peekKw("create", "user") || c.peekKw("create", "group")),
		head == "alter" && (c.peekKw("alter", "role") || c.peekKw("alter", "user") || c.peekKw("alter", "group")),
		head == "drop" && (c.peekKw("drop", "role") || c.peekKw("drop", "user") || c.peekKw("drop", "group")),
		c.peekKw("comment", "on", "role"), head == "set" || head == "reset":
		// The role's own attributes, settings, memberships and comment:
		// DROP ROLE removes them.
		handled = true
	case head == "grant" || head == "revoke":
		if depth0(toks, 1, "on") < 0 {
			handled = true // role membership (Y13)
			break
		}
		if c.peekKw("revoke") || c.peekKw("grant") {
			privs, tables, roles, optionFor, ok := r.grantTarget(toks, src, head == "revoke")
			if !ok {
				break
			}
			handled = true
			for _, n := range roles {
				if r.set[n] == nil {
					continue
				}
				if head == "grant" {
					if tables == nil {
						add(n, &roleDep{})
						continue
					}
					for _, q := range tables {
						t, certain := s.heldTable(q)
						if t == nil || !certain {
							add(n, &roleDep{})
							continue
						}
						p := privs
						if p == nil {
							p = map[string]bool{"\x00all": true}
						}
						add(n, &roleDep{t: t, privs: p})
					}
					continue
				}
				if optionFor || tables == nil {
					continue
				}
				for _, q := range tables {
					t, _ := s.lookup(q)
					for _, d := range r.set[n].deps {
						if d.t == nil || d.t != t || d.owner || d.policy != "" {
							continue
						}
						if privs == nil {
							d.privs = map[string]bool{}
							continue
						}
						for p := range privs {
							if p != "\x00cols" && p != "\x00other" {
								delete(d.privs, p)
							}
						}
					}
				}
			}
		}
	case c.peekKw("drop", "owned", "by"), c.peekKw("reassign", "owned", "by"):
		c.i = 3
		roles, ok := r.roleList(c)
		if !ok {
			break
		}
		handled = true
		for _, n := range roles {
			x := r.set[n]
			if x == nil {
				continue
			}
			if head == "drop" {
				x.deps = nil
				continue
			}
			var kept []*roleDep
			for _, d := range x.deps {
				if !d.owner {
					kept = append(kept, d)
				}
			}
			x.deps = kept
		}
	case c.peekKw("create", "policy"), c.peekKw("alter", "policy"):
		p, ok := readPolicy(toks, src)
		to := depth0(toks, 2, "to")
		if !ok || to < 0 {
			break
		}
		handled = true
		t, certain := s.heldTable(p.table)
		rc := &cursor{toks: toks, i: to + 1, src: src}
		roles, _ := r.roleList(rc)
		for _, n := range roles {
			if t == nil || !certain {
				add(n, &roleDep{})
			} else {
				add(n, &roleDep{t: t, policy: p.name})
			}
		}
	case c.peekKw("drop", "policy"):
		c.i = 2
		c.acceptKw("if", "exists")
		name, err := c.ident()
		if err != nil || !c.acceptKw("on") {
			break
		}
		q, err := c.name()
		if err != nil {
			break
		}
		t, _ := s.lookup(q)
		for _, x := range r.set {
			for _, d := range x.deps {
				if t != nil && d.t == t && d.policy == name {
					d.policy, d.privs = "", map[string]bool{}
				}
			}
		}
		handled = len(named) == 0
	}
	if handled {
		return
	}
	if i := depth0(toks, 0, "owner"); i >= 0 && i+1 < len(toks) && toks[i+1].kind == tokIdent && toks[i+1].value == "to" {
		oc := &cursor{toks: toks, i: i + 2, src: src}
		if n, ok := r.roleName(oc); ok && oc.done() {
			var t *table
			if c.peekKw("alter", "table") {
				ac := &cursor{toks: toks, i: 2, src: src}
				ac.acceptKw("if", "exists")
				ac.acceptKw("only")
				if q, err := ac.name(); err == nil {
					if x, certain := s.heldTable(q); x != nil && certain {
						t = x
					}
				}
			}
			if t != nil {
				for _, x := range r.set {
					x.deps = slicesDeleteFunc(x.deps, func(d *roleDep) bool { return d.t == t && d.owner })
				}
			}
			add(n, &roleDep{t: t, owner: true})
			delete(named, n)
		}
	}
	for n := range named {
		x := r.set[n]
		if x.unknown == "" {
			x.unknown = fmt.Sprintf("the set's statement %q names the role, and the pass does not read what it gives the role", text)
		}
	}
}

func slicesDeleteFunc(in []*roleDep, del func(*roleDep) bool) []*roleDep {
	var out []*roleDep
	for _, d := range in {
		if !del(d) {
			out = append(out, d)
		}
	}
	return out
}

// live reports a dependent that still stands.
func (s *state) liveDep(d *roleDep) bool {
	if d.t == nil {
		return true
	}
	if !slicesContainsTable(s.tables, d.t) {
		return false
	}
	return d.owner || d.policy != "" || len(d.privs) > 0
}

func slicesContainsTable(ts []*table, t *table) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

// y38DropRole applies Y38 (N1.P8) to DROP ROLE|USER|GROUP [IF EXISTS]
// names (user.c DropRole 1295-1299, checkSharedDependencies: refused,
// 2BP01, while a shared dependent exists in any database; the current
// and the session user cannot be dropped, 55006).
func (s *state) y38DropRole(toks []token) {
	r := &s.rev19b.roles
	names, ok := r.dropRoleNames(toks, s.m.source)
	if !ok {
		return
	}
	for _, n := range names {
		x := r.set[n]
		if x == nil {
			continue
		}
		what := "DROP ROLE " + n
		switch {
		case r.current == n:
			s.unknownFact(fmt.Sprintf("%s: the role is the current user (Postgres refuses, 55006)%s", what, precondition("N1.P8")))
			return
		case r.authorization:
			s.unknownFact(fmt.Sprintf("%s may be refused: SET SESSION AUTHORIZATION ran, and the role may own objects created since%s", what, precondition("N1.P8")))
			return
		case len(s.code.tables) > 0 || s.code.unqualified:
			s.unknownFact(fmt.Sprintf("%s may be refused: code may have created objects owned by or granted to it%s", what, precondition("N1.P8")))
			return
		}
		for _, d := range x.deps {
			if s.liveDep(d) {
				s.unknownFact(fmt.Sprintf("%s: the set's statement %q gave it a dependent object or privilege (Postgres refuses, 2BP01)%s", what, d.text, precondition("N1.P8")))
				return
			}
		}
		if x.unknown != "" {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, x.unknown, precondition("N1.P8")))
			return
		}
	}
}

// y38SetRole follows SET [SESSION | LOCAL] ROLE and RESET ROLE, and SET
// SESSION AUTHORIZATION (after which the record is not followed).
func (s *state) y38SetRole(toks []token) {
	r := &s.rev19b.roles
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	if toks[0].value == "reset" {
		switch {
		case c.peekKw("role"):
			r.current, r.local = "", false
		case c.peekKw("session", "authorization"):
			r.current, r.local = "", false
		}
		return
	}
	local := c.acceptKw("local")
	if !local {
		c.acceptKw("session")
	}
	switch {
	case c.acceptKw("session", "authorization"):
		r.authorization = true
	case c.acceptKw("role"):
		if c.acceptKw("none") {
			r.current, r.local = "", local
			return
		}
		n := ""
		if c.tok().kind == tokString {
			n, _ = tokenText(c.tok(), c.src)
		} else if x, ok := r.roleName(c); ok {
			n = x
		}
		if n == migratorRole {
			n = ""
		}
		r.current, r.local = n, local
	}
}

// y13After records the roles and edges a statement the pass applied
// created or removed (Y13, Y38).
func (s *state) y13After(toks []token) {
	r := &s.rev19b.roles
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.peekKw("create", "role"), c.peekKw("create", "user"), c.peekKw("create", "group"):
		if s.failure == nil {
			s.y13CreateRole(toks)
		}
	case c.peekKw("revoke"):
		s.y13Revoke(toks)
	case c.peekKw("drop", "role"), c.peekKw("drop", "user"), c.peekKw("drop", "group"):
		if names, ok := r.dropRoleNames(toks, s.m.source); ok {
			for _, n := range names {
				r.forget(n)
			}
		}
	case c.peekKw("alter", "role"), c.peekKw("alter", "user"), c.peekKw("alter", "group"):
		c.i = 2
		old, err := c.ident()
		if err == nil && c.acceptKw("rename", "to") {
			if n, err := c.ident(); err == nil {
				r.rename(old, n)
			}
		}
	case c.peekKw("set"), c.peekKw("reset"):
		s.y38SetRole(toks)
	}
}

// rev19bCode applies a statement code may run (codeStatementEffects) to
// the role record: a role, membership or grant code may change is not
// followed (Y13, Y38).
func (s *state) rev19bCode(toks []token) {
	r := &s.rev19b.roles
	c := &cursor{toks: toks}
	switch {
	case c.peekKw("grant"), c.peekKw("revoke"), c.peekKw("create", "role"), c.peekKw("create", "user"),
		c.peekKw("create", "group"), c.peekKw("alter", "group"), c.peekKw("alter", "role"), c.peekKw("alter", "user"),
		c.peekKw("drop", "role"), c.peekKw("drop", "user"), c.peekKw("drop", "group"), c.peekKw("set"), c.peekKw("reset"):
		if r.unknown == "" {
			r.unknown = "code may change role memberships"
		}
	}
	for _, t := range toks {
		if x := r.set[t.value]; x != nil && (t.kind == tokIdent || t.kind == tokQuoted) && x.unknown == "" {
			x.unknown = "code may give the role a dependent object or privilege"
		}
	}
	if s.rev19b.otherConstraints == nil {
		s.rev19b.otherConstraints = map[string]bool{}
	}
	for i, t := range toks {
		if t.kind == tokIdent && t.value == "constraint" && i+1 < len(toks) {
			s.rev19b.otherConstraints[toks[i+1].value] = true
		}
	}
}

// heldTable returns the table the pass holds under exactly this name and
// whether the name surely means it (no table of the name under another
// qualification, no search_path change for an unqualified name, no
// temporary table of the name).
func (s *state) heldTable(q qname) (*table, bool) {
	t, others := s.lookup(q)
	if t == nil {
		return nil, false
	}
	return t, len(others) == 0 && (q.schema != nil || !s.pathChanged && !s.temps[q.name])
}

// valuesCategory are the type categories Y23 resolves (pg_type
// typcategory: N numeric, S string, B boolean) with each numeric type's
// rank (select_common_type moves to a type the current one casts to
// implicitly and that does not cast back: int2 < int4 < int8 < numeric).
var valuesCategory = map[string]struct {
	cat  byte
	rank int
}{
	"int2": {'N', 1}, "int4": {'N', 2}, "int8": {'N', 3}, "numeric": {'N', 4}, "text": {'S', 0}, "bool": {'B', 0},
}

// valueEntryType is the type of one VALUES entry that is a constant: ""
// for an untyped string literal or NULL (unknown), the type otherwise; ok
// is false for an entry the rule does not read.
func valueEntryType(k constant) (string, bool) {
	if k.cast != "" {
		canon, ok := canonicalType(k.cast)
		canon = strings.TrimPrefix(canon, "pg_catalog.")
		if _, known := valuesCategory[canon]; !ok || !known {
			return "", false
		}
		if k.kind == 's' && !(&exprEnv{}).validLiteral(k.text, canon) {
			return "", false
		}
		if k.kind != 's' && k.kind != 'z' && valuesCategory[canon].cat != map[byte]byte{'n': 'N', 'd': 'N', 'b': 'B'}[k.kind] && canon != "text" {
			return "", false
		}
		return canon, true
	}
	switch k.kind {
	case 's', 'z':
		return "", true
	case 'b':
		return "bool", true
	case 'd':
		return "numeric", true
	case 'n':
		if _, err := strconv.ParseInt(k.text, 10, 32); err == nil {
			return "int4", true
		}
		if _, err := strconv.ParseInt(k.text, 10, 64); err == nil {
			return "int8", true
		}
		return "numeric", true
	}
	return "", false
}

// y23Values applies Y23 (N3.P5) to every VALUES list of two or more rows
// that is a query (a VALUES statement, a parenthesised VALUES, CREATE
// TABLE / VIEW ... AS VALUES): not the VALUES of INSERT, whose entries
// are coerced to the target columns row by row. Each column's common
// type is resolved as parse_coerce.c select_common_type does
// (transformValuesClause): categories that differ are refused (42804),
// untyped string literals are then read by the common type's input
// function (refused when it rejects them, 22P02); rows of different
// lengths are refused (42601). An entry that is not a constant makes the
// column not proven.
func (s *state) y23Values(toks []token) {
	src := s.m.source
	for i, t := range toks {
		if t.kind != tokIdent || t.value != "values" {
			continue
		}
		query := i == 0
		if i > 0 {
			p := toks[i-1]
			query = query || p.kind == tokPunct && p.value == "(" || p.kind == tokIdent && p.value == "as" && toks[0].kind == tokIdent && toks[0].value == "create"
		}
		if !query {
			continue
		}
		c := &cursor{toks: toks, i: i + 1, src: src}
		var rows [][][]token
		for c.isPunct("(") {
			start := c.i
			end, err := c.group()
			if err != nil {
				return
			}
			var row [][]token
			for _, r := range c.splitList(start+1, end-1) {
				row = append(row, toks[r[0]:r[1]])
			}
			rows = append(rows, row)
			c.i = end
			if !c.acceptPunct(",") {
				break
			}
		}
		if len(rows) < 2 {
			continue
		}
		for _, row := range rows[1:] {
			if len(row) != len(rows[0]) {
				if i > 0 {
					// A VALUES statement's rows are N3.P1's.
					s.unknownFact("VALUES lists must all be the same length (Postgres refuses, 42601)" + precondition("N3.P5"))
				}
				return
			}
		}
		for col := range rows[0] {
			if !s.y23Column(rows, col, src) {
				return
			}
		}
	}
}

// y23Column resolves one VALUES column; it reports false when it recorded
// a failure.
func (s *state) y23Column(rows [][][]token, col int, src []byte) bool {
	common := ""
	var strs []constant
	for _, row := range rows {
		k, ok := readConstant(row[col], src)
		var typ string
		if ok {
			typ, ok = valueEntryType(k)
		}
		if !ok {
			s.unknownFact(fmt.Sprintf("VALUES column %d: the entry %s is not a constant the pass types, so the column's common type is not proven%s", col+1, tokensText(row[col], src), precondition("N3.P5")))
			return false
		}
		if typ == "" {
			if k.kind == 's' {
				strs = append(strs, k)
			}
			continue
		}
		if common == "" {
			common = typ
			continue
		}
		a, b := valuesCategory[common], valuesCategory[typ]
		if a.cat != b.cat {
			s.unknownFact(fmt.Sprintf("VALUES column %d: types %s and %s cannot be matched (Postgres refuses, 42804)%s", col+1, common, typ, precondition("N3.P5")))
			return false
		}
		if b.rank > a.rank {
			common = typ
		}
	}
	if common == "" {
		return true
	}
	for _, k := range strs {
		if !(&exprEnv{}).validLiteral(k.text, common) {
			s.unknownFact(fmt.Sprintf("VALUES column %d: %q is not proven valid input for its common type %s (Postgres refuses invalid input, 22P02)%s", col+1, k.text, common, precondition("N3.P5")))
			return false
		}
	}
	return true
}

// matview is the U-b record of one materialized view (Y24).
type matview struct {
	populated tri
	// indexes are the index names on it (written; "" for an unnamed one)
	// with whether each is usable by REFRESH CONCURRENTLY (matview.c
	// is_usable_unique_index: unique, btree, no predicate, plain columns).
	indexes map[string]bool
	unnamed int
	// inexact says why its index record may miss an index or hold one
	// that is gone.
	inexact string
}

// y24Record follows CREATE MATERIALIZED VIEW, REFRESH, indexes on a held
// materialized view, its renames and drops (Y24), after the statement
// applied.
func (s *state) y24Record(toks []token, a analysis) {
	r := &s.rev19b
	c := &cursor{toks: toks, src: s.m.source}
	key := func(q qname) string { return q.String() }
	switch {
	case c.acceptKw("create", "materialized", "view"):
		ine := c.acceptKw("if", "not", "exists")
		q, err := c.name()
		if err != nil {
			return
		}
		if r.matviews == nil {
			r.matviews = map[string]*matview{}
		}
		if ine && r.matviews[key(q)] != nil {
			return
		}
		mv := &matview{populated: triYes, indexes: map[string]bool{}}
		if n := len(toks); n >= 3 && toks[n-3].kind == tokIdent && toks[n-3].value == "with" && toks[n-2].kind == tokIdent && toks[n-2].value == "no" && toks[n-1].kind == tokIdent && toks[n-1].value == "data" {
			mv.populated = triNo
		}
		if ine {
			mv.inexact = "CREATE MATERIALIZED VIEW IF NOT EXISTS may have been skipped"
			mv.populated = triUnknown
		}
		r.matviews[key(q)] = mv
	case c.acceptKw("refresh", "materialized", "view"):
		c.acceptKw("concurrently")
		q, err := c.name()
		if err != nil {
			return
		}
		if mv := r.matviews[key(q)]; mv != nil {
			mv.populated = triYes
			if c.acceptKw("with", "no", "data") {
				mv.populated = triNo
			}
		}
	case a.index != nil:
		mv := r.matviews[key(a.index.table)]
		if mv == nil {
			return
		}
		ci := a.index
		usable := ci.unique && ci.where == nil && len(ci.elements) > 0 && (ci.using == nil || *ci.using == "btree")
		for _, e := range ci.elements {
			usable = usable && e.column != nil
		}
		if ci.name == nil {
			mv.unnamed++
			if usable {
				mv.indexes["\x00"+fmt.Sprint(mv.unnamed)] = true
			}
			return
		}
		if ci.ifNotExists && mv.indexes[*ci.name] != usable {
			mv.inexact = "CREATE INDEX IF NOT EXISTS may have been skipped"
		}
		mv.indexes[*ci.name] = usable
	case a.alterIndex != nil && a.alterIndex.newName != nil:
		for _, mv := range r.matviews {
			if u, ok := mv.indexes[a.alterIndex.name.name]; ok {
				delete(mv.indexes, a.alterIndex.name.name)
				mv.indexes[*a.alterIndex.newName] = u
			}
		}
	case a.drop != nil && a.drop.kind == "index":
		for _, q := range a.drop.names {
			found := false
			for _, mv := range r.matviews {
				if _, ok := mv.indexes[q.name]; ok {
					delete(mv.indexes, q.name)
					found = true
				}
			}
			if !found {
				for _, mv := range r.matviews {
					if mv.unnamed > 0 {
						mv.inexact = "DROP INDEX " + q.name + " may have dropped an index of a generated name"
					}
				}
			}
		}
	case c.acceptKw("alter", "materialized", "view"):
		c.acceptKw("if", "exists")
		q, err := c.name()
		if err != nil {
			return
		}
		mv := r.matviews[key(q)]
		if mv == nil {
			return
		}
		if c.acceptKw("rename", "to") {
			if n, err := c.ident(); err == nil {
				nq := qname{name: n, schema: q.schema}
				delete(r.matviews, key(q))
				r.matviews[key(nq)] = mv
			}
			return
		}
		if c.peekKw("set", "schema") || c.peekKw("rename") {
			delete(r.matviews, key(q))
		}
	case c.acceptKw("drop", "materialized", "view"):
		c.acceptKw("if", "exists")
		for {
			q, err := c.name()
			if err != nil {
				return
			}
			delete(r.matviews, key(q))
			if !c.acceptPunct(",") {
				return
			}
		}
	}
}

// y24Refresh applies Y24 (N3.P4) to REFRESH MATERIALIZED VIEW
// CONCURRENTLY (matview.c RefreshMatViewByOid 202-213, 240-272): refused
// with WITH NO DATA (42601) or on a view the record holds unpopulated
// (0A000), or when its index record is exact and holds no usable unique
// index (55000); claimed when the view is populated and holds one;
// may fail otherwise.
func (s *state) y24Refresh(toks []token) {
	c := &cursor{toks: toks, i: 3, src: s.m.source}
	if !c.acceptKw("concurrently") {
		return
	}
	q, err := c.name()
	if err != nil {
		return
	}
	what := "REFRESH MATERIALIZED VIEW CONCURRENTLY " + q.String()
	if c.acceptKw("with", "no", "data") {
		s.unknownFact(what + ": CONCURRENTLY and WITH NO DATA cannot be used together (Postgres refuses, 42601)" + precondition("N3.P4"))
		return
	}
	mv := s.rev19b.matviews[q.String()]
	switch {
	case mv == nil:
		s.unknownFact(what + " may be refused: the pass does not hold the materialized view and its indexes" + precondition("N3.P4"))
		return
	case q.schema == nil && s.pathChanged:
		s.unknownFact(what + " may be refused: search_path may have changed, so the name may mean another view" + precondition("N3.P4"))
		return
	case mv.populated == triNo:
		s.unknownFact(what + ": the materialized view is not populated (Postgres refuses, 0A000)" + precondition("N3.P4"))
		return
	}
	usable := false
	for _, u := range mv.indexes {
		usable = usable || u
	}
	inexact := mv.inexact
	if inexact == "" && (len(s.code.may) > 0 || len(s.code.gone) > 0) {
		inexact = "code may have created or dropped an index"
	}
	switch {
	case !usable && inexact == "":
		s.unknownFact(what + ": the materialized view has no unique index without a WHERE clause on plain columns (Postgres refuses, 55000)" + precondition("N3.P4"))
	case inexact != "" || mv.populated != triYes:
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, firstNonEmpty(inexact, "whether it is populated is not known"), precondition("N3.P4")))
	}
}

// inhColumn is a column of an inheritance child the pass records (Y27):
// its written type and collation, and whether it is inherited.
type inhColumn struct {
	typ       string
	collation string
	inherited bool
	// local is set for a column the child defines itself (merged with an
	// inherited one or not).
	local bool
}

// tableColumns returns the columns of a table the pass holds with known
// facts, or of an INHERITS child it recorded; ok is false when they are
// not known.
func (s *state) tableColumns(t *table) (map[string]inhColumn, bool) {
	if cols, ok := s.rev19b.inhCols[t]; ok {
		return cols, cols != nil
	}
	if !factsKnown(t) {
		return nil, false
	}
	for _, r := range t.reasons {
		if strings.Contains(r.text, "its columns and constraints are unknown") || strings.Contains(r.text, "could not be read") {
			return nil, false
		}
	}
	out := map[string]inhColumn{}
	for _, c := range t.columns {
		col := ""
		if c.collation != nil {
			col = *c.collation
		}
		out[c.name] = inhColumn{typ: writtenType(c), collation: col}
	}
	return out, true
}

// createTableParts reads the parts of CREATE TABLE ... (elements)
// [INHERITS (parents)] Y27 and Y28 need: the explicit columns, the LIKE
// sources, the parents, whether the table is temporary, and OF and
// PARTITION OF.
type createTableParts struct {
	name     qname
	temp     bool
	columns  []columnDef
	likes    []qname
	parents  []qname
	of       *qname
	partOf   *qname
	readable bool
}

func readCreateTableParts(toks []token, src []byte) (createTableParts, bool) {
	var p createTableParts
	c := &cursor{toks: toks, src: src}
	if !c.acceptKw("create") {
		return p, false
	}
	for {
		switch {
		case c.acceptKw("temp"), c.acceptKw("temporary"):
			p.temp = true
			continue
		case c.acceptKw("global"), c.acceptKw("local"), c.acceptKw("unlogged"):
			continue
		}
		break
	}
	if !c.acceptKw("table") {
		return p, false
	}
	c.acceptKw("if", "not", "exists")
	q, err := c.name()
	if err != nil {
		return p, false
	}
	p.name = q
	if q.schema != nil && isTempSchema(q.schema) {
		p.temp = true
	}
	switch {
	case c.acceptKw("of"):
		if t, err := c.name(); err == nil {
			p.of = &t
		}
		return p, true
	case c.acceptKw("partition", "of"):
		if t, err := c.name(); err == nil {
			p.partOf = &t
		}
		return p, true
	case !c.isPunct("("):
		return p, false
	}
	end, err := c.group()
	if err != nil {
		return p, false
	}
	p.readable = true
	if end-c.i > 2 {
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] >= r[1] {
				return p, false
			}
			if c.toks[r[0]].kind == tokIdent && c.toks[r[0]].value == "like" {
				sub := &cursor{toks: c.toks[:r[1]], i: r[0] + 1, src: src}
				if l, err := sub.name(); err == nil {
					p.likes = append(p.likes, l)
				} else {
					p.readable = false
				}
				continue
			}
			if isConstraintStart(c, r[0]) {
				continue
			}
			col, err := columnDefinition(c, r[0], r[1])
			if err != nil {
				p.readable = false
				continue
			}
			p.columns = append(p.columns, col)
		}
	}
	c.i = end
	if c.acceptKw("inherits") && c.acceptPunct("(") {
		for {
			pq, err := c.name()
			if err != nil {
				p.readable = false
				break
			}
			p.parents = append(p.parents, pq)
			if !c.acceptPunct(",") {
				break
			}
		}
	}
	return p, true
}

// y28CreateTable applies Y28 (M1b.P2, M1b.P4) and Y27's merge check to
// CREATE TABLE with LIKE, OF, INHERITS or PARTITION OF (tablecmds.c
// MergeAttributes 2552, 2640, check_of_type 6968, DefineRelation 1067):
// a LIKE column whose name another LIKE source or an explicit column has
// (42701), OF a type that is not a composite type (42809), PARTITION OF
// a table that is not partitioned (42809), INHERITS of a temporary
// table from a permanent one, of a partitioned table, a partition or a
// relation that is not a table (42809), the same parent twice (42P07),
// and an inherited column whose type differs from another parent's or
// the explicit column's (42804).
func (s *state) y28CreateTable(toks []token) {
	p, ok := readCreateTableParts(toks, s.m.source)
	if !ok {
		return
	}
	what := "CREATE TABLE " + p.name.String()
	switch {
	case p.of != nil:
		s.y19KindCheck(*p.of, func(k string) bool { return k == "composite type" }, what+" OF "+p.of.String(), "42809", "M1b.P2")
		return
	case p.partOf != nil:
		t, certain := s.heldTable(*p.partOf)
		if t == nil {
			return
		}
		if t.partitionKey == nil {
			if certain && !partitioningUnknown(t) && !t.kindUnknown {
				s.unknownFact(fmt.Sprintf("%s PARTITION OF %s: the table is not partitioned (Postgres refuses, 42P17)%s", what, p.partOf, precondition("M1b.P4")))
			} else {
				s.unknownFact(fmt.Sprintf("%s PARTITION OF %s may be refused: the table may not be partitioned%s", what, p.partOf, precondition("M1b.P4")))
			}
		}
		return
	}
	if !p.readable {
		return
	}
	// The column names LIKE and the explicit columns write (42701).
	names := map[string]string{}
	for _, l := range p.likes {
		t, certain := s.heldTable(l)
		cols, known := map[string]inhColumn(nil), false
		if t != nil && certain {
			cols, known = s.tableColumns(t)
		}
		if !known {
			if len(p.likes) > 1 || len(p.columns) > 0 || len(p.parents) > 0 {
				s.unknownFact(fmt.Sprintf("%s (LIKE %s) may be refused: the pass does not hold the columns of %s, which may clash with the table's other columns%s", what, l, l, precondition("M1b.P2")))
				return
			}
			continue
		}
		for n := range cols {
			if src, dup := names[n]; dup {
				s.unknownFact(fmt.Sprintf("%s: column %s is specified more than once (LIKE %s and %s; Postgres refuses, 42701)%s", what, n, l, src, precondition("M1b.P2")))
				return
			}
			names[n] = "LIKE " + l.String()
		}
	}
	if len(p.likes) > 0 && len(p.parents) > 0 {
		s.unknownFact(fmt.Sprintf("%s may be refused: LIKE together with INHERITS is not followed%s", what, precondition("M1b.P2")))
		return
	}
	for _, col := range p.columns {
		if src, dup := names[col.name]; dup {
			s.unknownFact(fmt.Sprintf("%s: column %s is specified more than once (%s and the column definition; Postgres refuses, 42701)%s", what, col.name, src, precondition("M1b.P2")))
			return
		}
		names[col.name] = "a column definition"
	}
	if len(p.parents) == 0 {
		return
	}
	seen := map[string]bool{}
	merged := map[string]inhColumn{}
	known := true
	for _, pq := range p.parents {
		if seen[pq.String()] {
			s.unknownFact(fmt.Sprintf("%s: relation %s would be inherited from more than once (Postgres refuses, 42P07)%s", what, pq, precondition("M1b.P2")))
			return
		}
		seen[pq.String()] = true
		if (pq.schema == nil && s.temps[pq.name] || pq.schema != nil && isTempSchema(pq.schema)) && !p.temp {
			s.unknownFact(fmt.Sprintf("%s: cannot inherit from temporary relation %s (Postgres refuses, 42809)%s", what, pq, precondition("M1b.P2")))
			return
		}
		if s.y19KindCheck(pq, func(k string) bool { return k == "table" || k == "foreign table" }, what+" INHERITS "+pq.String(), "42809", "M1b.P2") {
			return
		}
		t, certain := s.heldTable(pq)
		if t != nil && t.partitionOf != nil {
			s.unknownFact(fmt.Sprintf("%s: cannot inherit from partition %s (Postgres refuses, 42809)%s", what, pq, precondition("M1b.P2")))
			return
		}
		if t == nil || !certain {
			known = false
			continue
		}
		cols, ok := s.tableColumns(t)
		if !ok {
			known = false
			continue
		}
		for n, c := range cols {
			if prev, dup := merged[n]; dup && !sameInhColumn(prev, c) {
				s.unknownFact(fmt.Sprintf("%s: inherited column %s has a type or collation conflict (Postgres refuses, 42804)%s", what, n, precondition("M1b.P2")))
				return
			}
			merged[n] = inhColumn{typ: c.typ, collation: c.collation, inherited: true}
		}
	}
	if !known {
		return
	}
	for _, col := range p.columns {
		c := inhColumn{typ: col.typ, collation: derefOr(col.collation)}
		if prev, dup := merged[col.name]; dup {
			if !sameInhColumn(prev, c) {
				s.unknownFact(fmt.Sprintf("%s: column %s has a type or collation conflict with the inherited column (Postgres refuses, 42804)%s", what, col.name, precondition("M1b.P2")))
				return
			}
			continue
		}
		merged[col.name] = c
	}
}

func derefOr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// sameInhColumn reports two column definitions Postgres merges: the same
// type and typmod, and the same written collation.
func sameInhColumn(a, b inhColumn) bool {
	return sameType(a.typ, b.typ) && a.collation == b.collation
}

// y27Record records the columns of a CREATE TABLE ... INHERITS child the
// pass holds as a name-only node (its parents' columns merged with its
// own), after the statement applied; nil when any part is not known.
func (s *state) y27Record(toks []token) {
	p, ok := readCreateTableParts(toks, s.m.source)
	if !ok || len(p.parents) == 0 {
		return
	}
	t, _ := s.lookup(p.name)
	if t == nil {
		return
	}
	r := &s.rev19b
	if r.inhCols == nil {
		r.inhCols = map[*table]map[string]inhColumn{}
	}
	r.inhCols[t] = nil
	if !p.readable || len(p.likes) > 0 {
		return
	}
	cols := map[string]inhColumn{}
	for _, pq := range p.parents {
		pt, certain := s.heldTable(pq)
		if pt == nil || !certain {
			return
		}
		pc, ok := s.tableColumns(pt)
		if !ok {
			return
		}
		for n, c := range pc {
			cols[n] = inhColumn{typ: c.typ, collation: c.collation, inherited: true}
		}
	}
	for _, col := range p.columns {
		if prev, ok := cols[col.name]; ok {
			prev.typ, prev.local = col.typ, true
			cols[col.name] = prev
			continue
		}
		cols[col.name] = inhColumn{typ: col.typ, collation: derefOr(col.collation), local: true}
	}
	r.inhCols[t] = cols
}

// descendants are the inheritance children of t the pass holds, and below
// them theirs; unknown is set when a child's name does not resolve to one
// table.
func (s *state) descendants(t *table) (out []*table, unknown bool) {
	seen := map[*table]bool{t: true}
	queue := []*table{t}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		for _, q := range x.children {
			c, others := s.lookup(q)
			if c == nil || len(others) > 0 {
				unknown = true
				continue
			}
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
				queue = append(queue, c)
			}
		}
	}
	return out, unknown
}

// y27AlterTable applies Y27 (M5a.P8, M5h.P2) to ALTER TABLE p (without
// ONLY) on a table with inheritance children: ADD COLUMN c t is refused
// (42804) when a child holds c with another type or collation (the same
// merges), and may fail when a child's columns are not held or the column
// has constraints the children must meet; RENAME COLUMN a TO b is refused
// (42701) when a child holds b; ALTER COLUMN TYPE reaches the children,
// whose indexes and constraints the pass does not hold: may fail. On a
// child, RENAME, DROP or ALTER COLUMN TYPE of an inherited column is
// refused (42P16).
func (s *state) y27AlterTable(at *alterTable) {
	t, certain := s.heldTable(at.name)
	if t == nil {
		return
	}
	what := "ALTER TABLE " + at.name.String()
	if cols, ok := s.rev19b.inhCols[t]; ok && cols != nil {
		for _, a := range at.actions {
			var col string
			switch a.kind {
			case "rename_column", "drop_column", "alter_type":
				col = a.name
			}
			if c, held := cols[col]; held && c.inherited && col != "" && certain {
				s.unknownFact(fmt.Sprintf("%s: column %s is inherited (Postgres refuses to rename, drop or retype an inherited column, 42P16)%s", what, col, precondition("M5a.P8")))
				return
			}
		}
	}
	if at.only || len(t.children) == 0 {
		return
	}
	kids, unknown := s.descendants(t)
	for _, a := range at.actions {
		switch a.kind {
		case "add_column":
			if a.column == nil {
				continue
			}
			if a.ifNotExists && t.column(a.column.name) != nil {
				continue
			}
			def := *a.column
			if !noConstraints(columnConstraintSet(def)) || def.identity != nil || def.generated != nil || def.notNull {
				s.unknownFact(fmt.Sprintf("%s ADD COLUMN %s may be refused: the column's constraints are added to the inheritance children too, whose rows and columns the pass does not hold%s", what, def.name, precondition("M5a.P8")))
				return
			}
			add := inhColumn{typ: def.typ, collation: derefOr(def.collation), inherited: true}
			for _, k := range kids {
				cols, ok := s.tableColumns(k)
				if !ok || unknown {
					s.unknownFact(fmt.Sprintf("%s ADD COLUMN %s may be refused: an inheritance child's columns are not held%s", what, def.name, precondition("M5a.P8")))
					return
				}
				if c, held := cols[def.name]; held && !sameInhColumn(c, add) {
					s.unknownFact(fmt.Sprintf("%s ADD COLUMN %s: child table %s has a different type or collation for the column (Postgres refuses, 42804)%s", what, def.name, k.q, precondition("M5a.P8")))
					return
				}
			}
		case "rename_column":
			for _, k := range kids {
				cols, ok := s.tableColumns(k)
				if !ok || unknown {
					s.unknownFact(fmt.Sprintf("%s RENAME COLUMN %s may be refused: an inheritance child's columns are not held%s", what, a.name, precondition("M5h.P2")))
					return
				}
				if c, held := cols[a.newName]; held {
					why := "already exists"
					if c.inherited {
						why = "is inherited from another parent"
					}
					s.unknownFact(fmt.Sprintf("%s RENAME COLUMN %s TO %s: column %s of child table %s %s (Postgres refuses, 42701)%s", what, a.name, a.newName, a.newName, k.q, why, precondition("M5h.P2")))
					return
				}
			}
		case "alter_type":
			s.unknownFact(fmt.Sprintf("%s ALTER COLUMN %s TYPE may be refused: the change reaches the inheritance children, whose indexes and constraints the pass does not hold%s", what, a.name, precondition("M5a.P8")))
			return
		}
	}
}

// y27After updates the children's column records after an ALTER TABLE
// the pass applied: ADD COLUMN and RENAME COLUMN on the parent reach
// them; any other change to a recorded child makes its record unknown.
func (s *state) y27After(at *alterTable) {
	r := &s.rev19b
	t, _ := s.lookup(at.name)
	if t == nil {
		return
	}
	if _, ok := r.inhCols[t]; ok {
		r.inhCols[t] = nil
	}
	if at.only {
		return
	}
	kids, _ := s.descendants(t)
	for _, k := range kids {
		cols := r.inhCols[k]
		if cols == nil {
			continue
		}
		for _, a := range at.actions {
			switch a.kind {
			case "add_column":
				if a.column != nil {
					if _, held := cols[a.column.name]; !held {
						cols[a.column.name] = inhColumn{typ: a.column.typ, collation: derefOr(a.column.collation), inherited: true}
					}
				}
			case "rename_column":
				if c, held := cols[a.name]; held {
					delete(cols, a.name)
					cols[a.newName] = c
				}
			case "drop_column":
				// An inherited column the child does not define itself is
				// dropped with the parent's (ATExecDropColumn).
				if c, held := cols[a.name]; held && c.inherited {
					if c.local {
						c.inherited = false
						cols[a.name] = c
					} else {
						delete(cols, a.name)
					}
				}
			default:
				r.inhCols[k] = nil
			}
		}
	}
}

// partInfo is the U-b record of one partitioned table (Y29): its strategy,
// its key column when the key is one bare column, and the bounds of its
// partitions.
type partInfo struct {
	strategy string
	keyCol   string
	parts    []*partBound
}

// partBound is one partition's bound: DEFAULT, a list of values, a range
// or a hash modulus and remainder. Values are normalised for comparison
// (partValue); a range's open ends are "\x00min" and "\x00max".
type partBound struct {
	child     *table
	isDefault bool
	list      []string
	from, to  string
	modulus   int64
	remainder int64
}

// partKeyKind is the comparison kind of a key type Y29 models: "int"
// (int2, int4, int8), "text" (text, varchar; equality only), "date" or
// "bool"; "" for any other type.
func partKeyKind(typ string) string {
	canon, ok := canonicalType(typ)
	if !ok {
		return ""
	}
	canon = strings.TrimPrefix(canon, "pg_catalog.")
	if i := strings.Index(canon, "("); i >= 0 {
		canon = canon[:i]
	}
	switch canon {
	case "int2", "int4", "int8":
		return "int"
	case "text", "varchar":
		return "text"
	case "date":
		return "date"
	case "bool":
		return "bool"
	}
	return ""
}

// partValue reads one bound value for a key of kind k: its normalised
// form ("\x00null" for NULL, "\x00min"/"\x00max" for MINVALUE/MAXVALUE,
// a zero-padded integer, the string, a date, "t"/"f"), whether the pass
// proves it valid (ok) and whether the key type's input surely rejects it
// (bad).
func partValue(toks []token, src []byte, k string, typ string) (v string, ok, bad bool) {
	if len(toks) == 1 && toks[0].kind == tokIdent {
		switch toks[0].value {
		case "minvalue":
			return "\x00min", true, false
		case "maxvalue":
			return "\x00max", true, false
		}
	}
	c, read := readConstant(toks, src)
	if !read || c.cast != "" {
		return "", false, false
	}
	text := c.text
	switch c.kind {
	case 'z':
		return "\x00null", true, false
	case 'b':
		if k != "bool" {
			return "", false, false
		}
		return text[:1], true, false
	case 'd':
		return "", false, false
	case 'n':
		if k != "int" && k != "text" {
			return "", false, false
		}
	}
	if !(&exprEnv{}).validLiteral(text, typ) {
		// int4in and int8in reject a string without any digit; other
		// rejected input is only not proven.
		return "", false, k == "int" && c.kind == 's' && !strings.ContainsAny(text, "0123456789")
	}
	switch k {
	case "int":
		n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return "", false, false
		}
		return fmt.Sprintf("%+021d", n), true, false
	case "bool":
		switch strings.ToLower(strings.TrimSpace(text)) {
		case "t", "true", "y", "yes", "on", "1":
			return "t", true, false
		}
		return "f", true, false
	case "date":
		t := strings.TrimSpace(text)
		if len(t) != 10 {
			return "", false, false
		}
		return t, true, false
	}
	return text, true, false
}

// y29Record records the partitioning a CREATE TABLE ... PARTITION BY
// wrote and the bound a PARTITION OF or ATTACH PARTITION gave, and drops
// the bound of DETACH PARTITION, after the statement applied.
func (s *state) y29Record(toks []token, a analysis) {
	r := &s.rev19b
	if r.parts == nil {
		r.parts = map[*table]*partInfo{}
	}
	if a.create != nil {
		t, _ := s.lookup(a.create.name)
		if t == nil {
			return
		}
		if a.create.partitionBy != "" {
			info := &partInfo{strategy: a.create.partitionBy}
			if a.create.partitionPlain && a.create.partitionCount == 1 && len(a.create.partitionKey) == 1 {
				info.keyCol = a.create.partitionKey[0]
			}
			r.parts[t] = info
		}
		if a.create.partitionOf != nil {
			if p, _ := s.lookup(*a.create.partitionOf); p != nil && r.parts[p] != nil {
				if b, ok := s.readBound(toks, r.parts[p], p); ok {
					b.child = t
					r.parts[p].parts = append(r.parts[p].parts, b)
				}
			}
		}
		return
	}
	if a.alter == nil {
		return
	}
	p, _ := s.lookup(a.alter.name)
	if p == nil || r.parts[p] == nil {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	i := depth0(toks, 0, "partition")
	if i < 1 {
		return
	}
	c.i = i + 1
	child, err := c.name()
	if err != nil {
		return
	}
	ct, _ := s.lookup(child)
	switch toks[i-1].value {
	case "attach":
		if b, ok := s.readBound(toks, r.parts[p], p); ok && ct != nil {
			b.child = ct
			r.parts[p].parts = append(r.parts[p].parts, b)
		}
	case "detach":
		info := r.parts[p]
		var kept []*partBound
		for _, b := range info.parts {
			if b.child != ct {
				kept = append(kept, b)
			}
		}
		info.parts = kept
	}
}

// readBound reads FOR VALUES ... or DEFAULT of a statement for the
// partitioned table p; ok is false when the bound is not one the record
// holds (it is then refused or not proven by y29Bound).
func (s *state) readBound(toks []token, info *partInfo, p *table) (*partBound, bool) {
	b, why, _ := s.parseBound(toks, info, p)
	return b, why == ""
}

// parseBound reads a partition bound: why is "" when it is read and
// proven, otherwise why not, with refused set when Postgres surely
// refuses it (and sqlstate).
func (s *state) parseBound(toks []token, info *partInfo, p *table) (b *partBound, why string, refused string) {
	src := s.m.source
	i := depth0(toks, 0, "default")
	j := depth0(toks, 0, "for")
	if i >= 0 && (j < 0 || i < j) && (i+1 == len(toks) || toks[i+1].kind == tokPunct && toks[i+1].value == ";") {
		if info.strategy == "hash" {
			return nil, "a hash-partitioned table may not have a default partition", "42P16"
		}
		return &partBound{isDefault: true}, "", ""
	}
	if j < 0 || j+1 >= len(toks) || toks[j+1].value != "values" {
		return nil, "the bound is not read", ""
	}
	c := &cursor{toks: toks, i: j + 2, src: src}
	kind := ""
	switch {
	case c.acceptKw("in"):
		kind = "list"
	case c.acceptKw("from"):
		kind = "range"
	case c.acceptKw("with"):
		kind = "hash"
	default:
		return nil, "the bound is not read", ""
	}
	if kind != info.strategy {
		return nil, fmt.Sprintf("invalid bound specification for a %s partition", info.strategy), "42P16"
	}
	values := func() ([][]token, bool) {
		if !c.isPunct("(") {
			return nil, false
		}
		start := c.i
		end, err := c.group()
		if err != nil {
			return nil, false
		}
		var out [][]token
		for _, r := range c.splitList(start+1, end-1) {
			out = append(out, toks[r[0]:r[1]])
		}
		c.i = end
		return out, true
	}
	if kind == "hash" {
		vals, ok := values()
		if !ok || len(vals) != 2 {
			return nil, "the hash bound is not read", ""
		}
		b = &partBound{modulus: -1, remainder: -1}
		for _, v := range vals {
			if len(v) != 2 || v[0].kind != tokIdent || v[1].kind != tokNumber {
				return nil, "the hash bound is not read", ""
			}
			n, err := strconv.ParseInt(v[1].value, 10, 32)
			if err != nil {
				return nil, "the hash bound is not read", ""
			}
			switch v[0].value {
			case "modulus":
				b.modulus = n
			case "remainder":
				b.remainder = n
			}
		}
		switch {
		case b.modulus < 0 || b.remainder < 0:
			return nil, "the hash bound is not read", ""
		case b.modulus == 0:
			return nil, "modulus for hash partition must be an integer value greater than zero", "42P16"
		case b.remainder >= b.modulus:
			return nil, "remainder for hash partition must be less than modulus", "42P16"
		}
		return b, "", ""
	}
	if info.keyCol == "" {
		return nil, "the partition key is not one plain column", ""
	}
	col := p.column(info.keyCol)
	if col == nil || !factsKnown(p) {
		return nil, "the partition key column's type is not held", ""
	}
	typ := writtenType(col)
	k := partKeyKind(typ)
	if k == "" {
		return nil, "bounds of a key of type " + typ + " are not modelled", ""
	}
	read := func(v []token) (string, string, string) {
		x, ok, bad := partValue(v, src, k, typ)
		if bad {
			return "", fmt.Sprintf("%s is not valid input for the key's type %s", tokensText(v, src), typ), "22P02"
		}
		if !ok {
			return "", fmt.Sprintf("the bound value %s is not proven", tokensText(v, src)), ""
		}
		return x, "", ""
	}
	if kind == "list" {
		vals, ok := values()
		if !ok {
			return nil, "the bound is not read", ""
		}
		b = &partBound{}
		for _, v := range vals {
			x, why, st := read(v)
			if why != "" {
				return nil, why, st
			}
			if x == "\x00min" || x == "\x00max" {
				return nil, "MINVALUE and MAXVALUE are not list values", ""
			}
			b.list = append(b.list, x)
		}
		return b, "", ""
	}
	from, ok := values()
	if !ok || !c.acceptKw("to") {
		return nil, "the bound is not read", ""
	}
	to, ok := values()
	if !ok {
		return nil, "the bound is not read", ""
	}
	if len(from) != 1 || len(to) != 1 {
		return nil, "FROM and TO must specify exactly one value per partitioning column", "42P16"
	}
	if k == "text" {
		return nil, "range bounds of a text key depend on its collation", ""
	}
	b = &partBound{}
	var why2, st string
	if b.from, why2, st = read(from[0]); why2 != "" {
		return nil, why2, st
	}
	if b.to, why2, st = read(to[0]); why2 != "" {
		return nil, why2, st
	}
	if b.from == "\x00null" || b.to == "\x00null" {
		return nil, "cannot specify NULL in range bound", "42601"
	}
	return b, "", ""
}

// rangeLess orders range bound values (MINVALUE first, MAXVALUE last).
func rangeLess(a, b string) bool {
	switch {
	case a == b:
		return false
	case a == "\x00min" || b == "\x00max":
		return true
	case a == "\x00max" || b == "\x00min":
		return false
	}
	return a < b
}

// y29Bound applies Y29 (M1b.P5) to CREATE TABLE ... PARTITION OF p and
// ALTER TABLE p ATTACH PARTITION (parse_utilcmd.c transformPartitionBound
// 4004-4114; partbounds.c check_new_partition_bound 2920-3237): a bound
// of the wrong kind, DEFAULT on a hash-partitioned table, a hash
// remainder not below its modulus (42P16), a second DEFAULT, an empty
// range, a list value or range another partition holds, a hash modulus
// that is not a factor or multiple of the others, or a hash remainder
// another partition holds (42P17), and a bound value the key type's input
// rejects (22P02) are refused; keys of other types, multi-column or
// expression keys, values the pass does not prove, partitions code may
// have created, and a new partition while a default partition may hold
// rows may fail.
func (s *state) y29Bound(toks []token, a analysis) {
	r := &s.rev19b
	var pq qname
	what := ""
	switch {
	case a.create != nil && a.create.partitionOf != nil:
		pq, what = *a.create.partitionOf, "CREATE TABLE "+a.create.name.String()+" PARTITION OF "+a.create.partitionOf.String()
	case a.alter != nil && len(a.alter.actions) == 1 && depth0(toks, 0, "attach") >= 0:
		pq, what = a.alter.name, "ALTER TABLE "+a.alter.name.String()+" ATTACH PARTITION"
	default:
		return
	}
	p, certain := s.heldTable(pq)
	if p == nil {
		return
	}
	info := r.parts[p]
	if info == nil {
		if p.partitionKey != nil {
			s.unknownFact(fmt.Sprintf("%s may be refused: the partitioning of %s is not held%s", what, pq, precondition("M1b.P5")))
		}
		return
	}
	b, why, refused := s.parseBound(toks, info, p)
	if refused != "" && certain {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, refused, precondition("M1b.P5")))
		return
	}
	if why != "" {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, why, precondition("M1b.P5")))
		return
	}
	may := ""
	switch {
	case !certain:
		may = "the name may mean another table"
	case len(s.code.tables) > 0:
		may = "code may have created partitions"
	}
	var live []*partBound
	for _, x := range info.parts {
		if slicesContainsTable(s.tables, x.child) {
			live = append(live, x)
		}
	}
	conflict := ""
	for _, x := range live {
		switch {
		case b.isDefault && x.isDefault:
			conflict = "a default partition already exists"
		case b.isDefault || x.isDefault:
			if x.isDefault && (p.setRows || x.child.setRows) && may == "" {
				may = "the default partition may hold rows that belong to the new partition"
			}
		case info.strategy == "list":
			for _, v := range b.list {
				if slicesContainsString(x.list, v) {
					conflict = "a list value is held by partition " + x.child.q.String()
				}
			}
		case info.strategy == "range":
			if rangeLess(b.from, x.to) && rangeLess(x.from, b.to) {
				conflict = "it would overlap partition " + x.child.q.String()
			}
		case info.strategy == "hash":
			lo, hi := x, b
			if b.modulus < x.modulus {
				lo, hi = b, x
			}
			if hi.modulus%lo.modulus != 0 {
				conflict = "every hash partition modulus must be a factor of the next larger modulus"
			} else if hi.remainder%lo.modulus == lo.remainder {
				conflict = "it would overlap partition " + x.child.q.String()
			}
		}
		if conflict != "" {
			break
		}
	}
	if info.strategy == "range" && !b.isDefault && conflict == "" && !rangeLess(b.from, b.to) {
		conflict = "empty range bound specified"
	}
	switch {
	case conflict != "" && may == "":
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, 42P17)%s", what, conflict, precondition("M1b.P5")))
	case conflict != "" || may != "":
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, firstNonEmpty(may, conflict), precondition("M1b.P5")))
	}
}

func slicesContainsString(in []string, v string) bool {
	for _, x := range in {
		if x == v {
			return true
		}
	}
	return false
}

// extVersions is one contrib extension of the generated table (Y33,
// extensions.go): its default version, the extensions it requires, the
// versions that have an install script, its update scripts (from, to)
// and whether its scripts create operators.
type extVersions struct {
	def       string
	requires  []string
	installs  []string
	updates   [][2]string
	operators bool
	// trusted is the control file's trusted = true (Y51): a role that is
	// not a superuser may install it.
	trusted bool
}

// extensionName reads an extension name (an identifier or a quoted one;
// "uuid-ossp" is written quoted).
func extensionName(c *cursor) (string, bool) {
	n, err := c.ident()
	return n, err == nil
}

// installable reports whether CREATE EXTENSION ... VERSION v finds an
// install script for v, directly or through update scripts from a
// version that has one (extension.c find_install_path).
func (e extVersions) installable(v string) bool {
	for _, i := range e.installs {
		if e.reaches(i, v) {
			return true
		}
	}
	return false
}

// reaches reports whether update scripts lead from version a to b (a = b
// included).
func (e extVersions) reaches(a, b string) bool {
	seen := map[string]bool{a: true}
	work := []string{a}
	for len(work) > 0 {
		x := work[len(work)-1]
		work = work[:len(work)-1]
		if x == b {
			return true
		}
		for _, u := range e.updates {
			if u[0] == x && !seen[u[1]] {
				seen[u[1]] = true
				work = append(work, u[1])
			}
		}
	}
	return false
}

// y33CreateExtension applies Y33 (O2.P7, O2.P4) to CREATE EXTENSION
// (extension.c CreateExtension 1821-1835: SCHEMA, VERSION or CASCADE
// twice, 42601; ExecCreateExtension 1533 and find_install_path 1288: a
// version without an install script or update path, 22023): a VERSION is
// claimed only when it is the default version of the REL_17_6 control
// file; one the generated table proves has no install path is refused;
// any other version, and a VERSION of an extension outside the table,
// may fail. An extension the record holds installed is skipped with IF
// NOT EXISTS and refused without it (42710).
func (s *state) y33CreateExtension(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	ine := c.acceptKw("if", "not", "exists")
	n, ok := extensionName(c)
	if !ok {
		return
	}
	what := "CREATE EXTENSION " + n
	r := &s.rev19b
	if _, held := r.extensions[n]; held {
		if ine {
			return
		}
		s.unknownFact(fmt.Sprintf("%s: the set installed the extension earlier (Postgres refuses, 42710)%s", what, precondition("O2.P4")))
		return
	}
	c.acceptKw("with")
	seen := map[string]bool{}
	version := ""
	for !c.done() {
		var key string
		switch {
		case c.acceptKw("schema"):
			key = "schema"
			if _, err := c.ident(); err != nil {
				return
			}
		case c.acceptKw("version"):
			key = "version"
			if c.done() {
				return
			}
			v, ok := tokenText(c.tok(), c.src)
			if !ok {
				return
			}
			version = v
			c.i++
		case c.acceptKw("cascade"):
			key = "cascade"
		case c.acceptKw("from"):
			// FROM old_version: gram.y still reads it; Postgres refuses it.
			return
		default:
			return
		}
		if seen[key] {
			s.unknownFact(fmt.Sprintf("%s: conflicting or redundant options (%s given twice; Postgres refuses, 42601)%s", what, strings.ToUpper(key), precondition("O2.P7")))
			return
		}
		seen[key] = true
	}
	if version == "" {
		return
	}
	e, known := e1ExtensionVersions[n]
	switch {
	case !known:
		s.unknownFact(fmt.Sprintf("%s VERSION %s may be refused: the extension's versions are outside the REL_17_6 contrib table%s", what, version, precondition("O2.P4")))
	case !e.installable(version):
		s.unknownFact(fmt.Sprintf("%s VERSION %s: the extension has no installation script nor update path for this version (Postgres refuses, 22023)%s", what, version, precondition("O2.P4")))
	case version != e.def:
		s.unknownFact(fmt.Sprintf("%s VERSION %s may be refused: only the default version %s is proven%s", what, version, e.def, precondition("O2.P4")))
	}
}

// y33AlterExtension applies Y33 (O2.P4) to ALTER EXTENSION x UPDATE [TO
// v] (extension.c ExecAlterExtensionStmt: the installed version again is
// a notice; no update path, 22023): claimed when v is the version the
// record holds installed, or the default version reached by update
// scripts from it; refused when no update path leads there; may fail
// otherwise.
func (s *state) y33AlterExtension(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	n, ok := extensionName(c)
	if !ok || !c.acceptKw("update") {
		return
	}
	e, known := e1ExtensionVersions[n]
	v := ""
	if c.acceptKw("to") {
		if c.done() {
			return
		}
		x, ok := tokenText(c.tok(), c.src)
		if !ok {
			return
		}
		v = x
	} else if known {
		v = e.def
	}
	what := "ALTER EXTENSION " + n + " UPDATE"
	held, installed := s.rev19b.extensions[n]
	switch {
	case !known || !installed || held == "":
		if !(installed && held != "" && held == v) {
			s.unknownFact(fmt.Sprintf("%s may be refused: the installed version or its update path is not held%s", what, precondition("O2.P4")))
		}
	case v == held:
	case !e.reaches(held, v):
		s.unknownFact(fmt.Sprintf("%s TO %s: the extension has no update path from version %s (Postgres refuses, 22023)%s", what, v, held, precondition("O2.P4")))
	case v != e.def:
		s.unknownFact(fmt.Sprintf("%s TO %s may be refused: only the default version %s is proven%s", what, v, e.def, precondition("O2.P4")))
	}
}

// y33Record follows the extensions the set installs, updates and drops
// (Y33, Y34), after the statement applied: the installed version ("" when
// not known).
func (s *state) y33Record(toks []token) {
	r := &s.rev19b
	if r.extensions == nil {
		r.extensions = map[string]string{}
	}
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.acceptKw("create", "extension"):
		ine := c.acceptKw("if", "not", "exists")
		n, ok := extensionName(c)
		if !ok {
			return
		}
		if _, held := r.extensions[n]; held && ine {
			return
		}
		v := ""
		if i := depth0(toks, c.i, "version"); i >= 0 && i+1 < len(toks) {
			v, _ = tokenText(toks[i+1], s.m.source)
		} else if e, known := e1ExtensionVersions[n]; known {
			v = e.def
		}
		r.extensions[n] = v
		if depth0(toks, c.i, "cascade") >= 0 {
			for _, req := range e1ExtensionVersions[n].requires {
				if _, held := r.extensions[req]; !held {
					r.extensions[req] = e1ExtensionVersions[req].def
				}
			}
		}
	case c.acceptKw("alter", "extension"):
		n, ok := extensionName(c)
		if !ok || !c.acceptKw("update") {
			return
		}
		if _, held := r.extensions[n]; !held {
			return
		}
		v := ""
		if c.acceptKw("to") && !c.done() {
			v, _ = tokenText(c.tok(), c.src)
		} else if e, known := e1ExtensionVersions[n]; known {
			v = e.def
		}
		r.extensions[n] = v
	case c.acceptKw("drop", "extension"):
		c.acceptKw("if", "exists")
		for {
			n, ok := extensionName(c)
			if !ok {
				return
			}
			delete(r.extensions, n)
			if !c.acceptPunct(",") {
				return
			}
		}
	}
}

// y34DropExtension applies Y34 (O2.P8) to DROP EXTENSION [IF EXISTS]
// names without CASCADE (dependency.c reportDependentObjects 1150,
// 2BP01): refused or may fail while a live entry of the dependency record
// (X1) names a member of the extension (the generated table of its
// scripts), while another extension the set installed requires it, or,
// for an extension whose scripts create operators or one outside the
// table, while any live entry exists; code that may have created objects
// makes it may fail.
func (s *state) y34DropExtension(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	c.acceptKw("if", "exists")
	var names []string
	for {
		n, ok := extensionName(c)
		if !ok {
			return
		}
		names = append(names, n)
		if !c.acceptPunct(",") {
			break
		}
	}
	if c.acceptKw("cascade") {
		return
	}
	dropped := map[string]bool{}
	for _, n := range names {
		dropped[n] = true
	}
	for _, n := range names {
		what := "DROP EXTENSION " + n
		for other := range s.rev19b.extensions {
			if dropped[other] {
				continue
			}
			for _, req := range e1ExtensionVersions[other].requires {
				if req == n {
					s.unknownFact(fmt.Sprintf("%s: extension %s requires it (Postgres refuses, 2BP01)%s", what, other, precondition("O2.P8")))
					return
				}
			}
		}
		if len(s.code.may) > 0 {
			s.unknownFact(fmt.Sprintf("%s may be refused: code may have created objects that use it%s", what, precondition("O2.P8")))
			return
		}
		e, known := e1ExtensionVersions[n]
		members := map[string]bool{}
		for _, m := range e1ExtensionMembers[n] {
			members[m] = true
		}
		for _, x := range s.rev18.entries {
			if !s.live(x) || x.kind == "extension" {
				// Extensions depend on each other only through their
				// control files' requires (above).
				continue
			}
			if !known || e.operators || x.any {
				s.unknownFact(fmt.Sprintf("%s may be refused: the set's statement %q may use an object of the extension%s", what, x.text, precondition("O2.P8")))
				return
			}
			for w := range x.words {
				if members[w] {
					s.unknownFact(fmt.Sprintf("%s: the set's statement %q uses %s, a member of the extension (Postgres refuses, 2BP01)%s", what, x.text, w, precondition("O2.P8")))
					return
				}
			}
		}
	}
}

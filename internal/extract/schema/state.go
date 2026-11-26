package schema

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
)

// The schema state applies recognized operations in migration order. It
// only ever keeps what source proves: an operation it cannot apply exactly
// marks the tables involved partial with a reason and leaves their known
// columns in place (unknown is not absent).
//
// Revision 2 adds three rules. A destructive change (DROP TABLE, DROP
// COLUMN, DROP CONSTRAINT, DROP INDEX) whose target is not certain (search
// path, a temporary table of the same name, another qualification, or a
// migration that may fail) keeps the object and marks the table partial
// (Revision 7: DROP CONSTRAINT instead forgets every key of each table it
// may name, since constraint names are not tracked).
// A statement Postgres refuses or may refuse means the whole migration may
// fail (Postgres runs it as one query in one transaction, and sqlx stops the
// set at a failed migration): from then on every table the migration or a
// later one touches is partial, and those migrations' applied state is
// unknown. Relations and types other than modeled tables are remembered by
// name, so a later CREATE TABLE of such a name is skipped or refused.
//
// Revision 8: a statement whose success depends on a fact the pass does
// not know (a column, key, dependent or name of a table whose facts are not
// known, or a name that may mean another table or none) makes the
// migration one that may fail (unproven); forms that succeed whatever the
// fact (IF EXISTS, IF NOT EXISTS) do not. Objects the set never creates
// stay outside the model.

type reason struct {
	migration *migration
	span      byteSpan
	text      string
	// nameOnly is set when the reason is about the table's qualified name
	// only (a schema rename or SET SCHEMA moved it): its columns and keys
	// are still known (Revision 8, factsKnown).
	nameOnly bool
	// indexOnly is set when the reason is that one non-unique index may or
	// may not exist (P3): columns, keys and dependents are still known.
	indexOnly bool
	// row is the contract row or precondition id whose branch added the
	// reason (T018f); empty for set-level reasons no row produced.
	row string
	// noUnnamed is set when the statement behind the reason cannot have
	// created an unnamed object on the table (C5, unnamedFreeWords): the
	// table's generated names are still those the pass holds.
	noUnnamed bool
}

type key struct {
	name *string
	cols []*column
	// deferrable keys cannot be referenced by a foreign key.
	deferrable bool
	// gen is the name Postgres generated for an unnamed key's index when it
	// was created; a later table or column rename does not change it
	// (P2: names derived from an old name do not move).
	gen string
}

type column struct {
	name           string
	file           string
	span           byteSpan
	typ            string
	typeNames      []string
	typeResolved   string
	nullable       bool
	def            *string
	implied        bool
	collation      *string
	identity       *string
	generated      *string
	exprNames      []string
	generatedNames []string
	previousNames  []string
	// seq is the generated name of a serial or identity column's
	// sequence, fixed when the sequence was created (P2).
	seq string
	// seqParts are the table and column names the sequence name was
	// chosen from (Y40, Revision 1.9).
	seqParts [2]string
	// claims are the statements behind the column's facts (T018f,
	// claims.go), by record attribute name.
	claims map[string]claim
}

type foreignKey struct {
	name     *string
	local    []*column
	table    *table // nil when unresolved
	columns  []*column
	written  referenceDef
	onDelete *string
	onUpdate *string
	file     string
	span     byteSpan
	why      string // unresolved reason
	// external is the Supabase platform table (SB-M2) the key references.
	external string
}

type index struct {
	name     *string
	unique   bool
	using    *string
	elements []indexElement
	columns  []*column // per element; nil for an expression element
	include  []string
	where    *string
	// whereNames are the identifiers of the partial-index predicate.
	whereNames []string
	file       string
	span       byteSpan
	// uncertain is set when a statement may have dropped the index
	// (Revision 4): it no longer proves uniqueness or backs a foreign key.
	uncertain bool
	// gen is the generated name of an unnamed bare-column index, fixed at
	// creation (P2).
	gen string
	// mayBeGone is set when a statement that applies may have dropped the
	// index (DROP INDEX under an uncertain name, P3): whether it exists is
	// unknown, so it is not emitted as a fact, but its name may still be
	// taken.
	mayBeGone bool
	// nullsNotDistinct, concurrently and options are CREATE INDEX's NULLS
	// NOT DISTINCT, CONCURRENTLY and WITH (...) (U1 reads them).
	nullsNotDistinct bool
	concurrently     bool
	options          *[]storageOption
	// claims are the statements behind the index's facts (T018f).
	claims map[string]claim
}

// check is a named CHECK or EXCLUDE constraint and the identifiers it uses.
type check struct {
	name  string
	names []string
	// exclude marks an EXCLUDE constraint, which has an index of its name
	// (a CHECK has none; P1, Revision 1.4, g6-s69809).
	exclude bool
}

type alteration struct {
	migration *migration
	spans     []byteSpan
}

type table struct {
	q             qname
	file          string
	span          byteSpan
	creator       *migration
	columns       []*column
	reasons       []reason
	opaqueAfter   int
	primaryKey    *key
	uniques       []*key
	foreignKeys   []*foreignKey
	indexes       []*index
	checks        []check
	previousNames []string
	alterations   []*alteration
	// hasChecks is set by any CHECK or EXCLUDE constraint, named or not:
	// its expression may use objects a DROP ... CASCADE removes.
	hasChecks bool
	// unnamedCheckNames are the identifiers of each unnamed CHECK
	// constraint (P8: ALTER COLUMN TYPE re-analyses those that name the
	// column); checksUnknown is set when they are not all known.
	unnamedCheckNames [][]string
	checksUnknown     bool
	// unnamedChecks is set by an unnamed CHECK or EXCLUDE constraint,
	// whose generated name (_check, _excl) a later name may meet.
	unnamedChecks bool
	// children are the tables that inherit from this one (INHERITS,
	// INHERIT): DROP TABLE without CASCADE is refused while they exist.
	children []qname
	// keysUnknown is set when a DROP CONSTRAINT may have dropped a key
	// (Revision 4 WF3, Revision 7: any DROP CONSTRAINT), or when the pass
	// cannot tell whether Postgres merged two equal key constraints:
	// unique_keys is unknown from then on.
	keysUnknown bool
	// pkMaybe is set when a DROP CONSTRAINT forgot the primary key: it may
	// still exist, so adding another may be refused.
	pkMaybe bool
	// partitionKey are the identifiers of PARTITION BY (Revision 4 WF4).
	partitionKey []string
	// partitionProven is set when PARTITION BY is RANGE or LIST over
	// plain columns (U4: only then can the pass match a unique index to
	// the key).
	partitionProven bool
	// partitionOf is the partitioned table this one is a partition of
	// (PARTITION OF, ATTACH PARTITION): dropping that table drops this one.
	partitionOf *table
	// ofType is the type of CREATE TABLE ... OF: the table depends on it.
	ofType *qname
	// likeOf is the table CREATE TABLE ... LIKE copied: copied defaults may
	// use its sequences, so dropping it may be refused (P8).
	likeOf *table
	// unlogged is the table's persistence (CREATE UNLOGGED TABLE, SET
	// LOGGED/UNLOGGED): foreign keys between permanent and unlogged tables
	// are refused one way (P4, tablecmds.c 9681-9705).
	unlogged bool
	// clustered is the index CLUSTER t USING i last clustered the table on
	// (P8: CLUSTER t without USING needs one).
	clustered *index
	// kindUnknown is set when CREATE TABLE IF NOT EXISTS may have been
	// skipped over a relation of another kind (P3): the name is held, but
	// not proven to be a table's (P1).
	kindUnknown bool
	// triggers is set when the set created a trigger that may be on this
	// table (Revision 1.3): a row it writes may run code that changes or
	// refuses it.
	triggers bool
	// empty is set while the table is proven to hold no rows: the pass
	// created it, and no statement since may have written a row into it
	// (B2, rows.go).
	empty bool
	// triggerNames are the triggers the set created on this table (B2,
	// N1.P2); triggerNamesUnknown is set when a DROP or ALTER TRIGGER on
	// it could not be read.
	triggerNames        map[string]bool
	triggerNamesUnknown bool
	// policyNames are the policies the set created on this table
	// (Revision 1.4, N1.P2); policyNamesUnknown is set when an ALTER
	// POLICY ... RENAME on it could not be read.
	policyNames        map[string]bool
	policyNamesUnknown bool
	// rls and policies are SB-M3's record (Supabase sets): the row level
	// security flags and the policies the set created on this table, in
	// written order. rlsUnknown and policiesUnknown are set when an
	// unread statement may have changed them.
	rls        rlsState
	rlsUnknown bool
	// rlsOpaque counts the opaque statements since the table's creation
	// that may change its row level security: opaqueAfter less the SB-M9
	// data statements.
	rlsOpaque       int
	policies        []tablePolicy
	policiesUnknown bool
	// policyCommands are the commands (all, select, insert, update,
	// delete) of the policies the set created on this table, by name
	// (Revision 1.9 Y12: ALTER POLICY's USING and WITH CHECK checks).
	policyCommands map[string]string
	// triggerColumns are the columns a trigger the set created on this
	// table depends on (UPDATE OF; P8, Revision 1.4): ALTER COLUMN TYPE
	// and DROP COLUMN without CASCADE of one are refused.
	// triggerColumnsUnknown is set when a trigger's columns could not be
	// read (a WHEN condition other than OLD IS DISTINCT FROM NEW, an
	// unread UPDATE OF list or target).
	triggerColumns        map[string]bool
	triggerColumnsUnknown bool
	// mayBeOther is set when the relation the name holds may not be the
	// one this CREATE TABLE wrote (a relation of the name may have existed,
	// or IF NOT EXISTS may have skipped it; C4: whether it is partitioned
	// is then unknown).
	mayBeOther bool
	// setRows is set once a statement of the set may have written or
	// changed rows of this table (Revision 1.4 R5): those rows are not
	// E2's, and a statement that checks existing rows is not proven.
	setRows bool
}

func (t *table) column(name string) *column {
	for _, c := range t.columns {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (t *table) alteredBy(m *migration) bool {
	for _, a := range t.alterations {
		if a.migration == m {
			return true
		}
	}
	return false
}

// relation is a relation or type other than a modeled table (WF3).
type relation struct {
	kind string
	m    *migration
	// opaqueAt is the opaque count when it was recorded: an opaque
	// statement since then may have dropped or renamed it.
	opaqueAt  int
	uncertain bool
	// labels are an enum type's labels in order (R2, R3); nil when not
	// known.
	labels []string
}

// failure is the first statement that Postgres refuses or may refuse.
type failure struct {
	m      *migration
	reason string
}

// droppedTable is a table the current migration dropped; it is restored
// as partial when the migration may fail.
type droppedTable struct {
	t      *table
	owners []*table
}

type state struct {
	// rowSecurityWord is set once a statement names row_security (as a
	// word or in a string): R1b.P5's analysed SQL bodies are then not
	// proven (sqlbody.go).
	rowSecurityWord bool
	// rlsBlocked is set once the set ran an opaque statement other than an
	// SB-M9 data statement, or a statement the pass could not read: code
	// the pass did not read (a trigger among it) may exist from then on,
	// so no later data statement is SB-M9's.
	rlsBlocked bool
	// rlsRuled is set once the set ran CREATE [OR REPLACE] RULE: every
	// later data statement may run rule actions SB-M9 does not read.
	rlsRuled bool
	// platformTypes are the types SB-M6 extensions created, by name
	// (SB-M10, supabase.go).
	platformTypes map[string]*heldPlatformType
	// operatorObjects is set when the set creates an operator, cast,
	// operator class or family, access method or aggregate (SB-M6).
	operatorObjects bool
	// bucketCode is set when a statement of the set or its code may make a
	// trigger or rule fire on storage.buckets (SB-M7).
	bucketCode bool
	// o1t are the set's functions of the O1t form (o1t.go).
	o1t o1tFunctions
	// sequenceUse is set when the set's text may advance or set a
	// sequence outside an INSERT's own defaults (rows.go): sequence
	// defaults are then never proven.
	sequenceUse bool
	// sequenceCalls is set when the set's text calls nextval or setval
	// (U5: a sequence's last value is then not known).
	sequenceCalls bool
	// sequences are the parameters of the sequences whose options the
	// pass read (U5), by qualified name.
	sequences map[string]*seqParams
	// keyDependents is set when the set may create an object that can
	// depend on a primary key by functional dependency (a view, rule or
	// SQL-standard function body; C3, keyConstraintDependency).
	pkDependents bool
	// inBlock is set while the current statement may run inside a
	// transaction block (C4, txTracker.inBlock).
	inBlock bool
	set     *migrationSet
	tables  []*table
	// n1 records the names N1 statements created outside the tables the
	// pass holds (n1names.go, N1.P2).
	n1 n1Names
	// outsideChanged holds the bare names of tables the pass does not
	// hold on which the set created an index or ran ALTER TABLE (P2,
	// Revision 1.4): their columns and names are then partly the set's.
	outsideChanged map[string]bool
	// pathChanged is R2: a statement may have changed search_path.
	pathChanged bool
	// sessionPath and localPath are the session's and the current
	// transaction's search_path (U3; nil: E4's value, no local value),
	// pathMode how sqlx runs the current migration, and laterMayFail the
	// reason every later migration may fail (sqlx's UPDATE of
	// _sqlx_migrations after a commit; Q1.P13).
	sessionPath, localPath *pathValue
	pathMode               pathMode
	laterMayFail           string
	opaqueSeen             int
	temps                  map[string]bool
	views                  bool
	relations              map[string]*relation
	// rev16 is what Revision 1.6 V11 keeps (composite attributes, typed
	// tables).
	rev16 rev16State
	// rev17 is what Revision 1.7 W5 keeps (names dropped since created).
	rev17 rev17State
	// rev18 is what Revision 1.8 keeps (the dependency record, X1).
	rev18 rev18State
	// rev19 is what Revision 1.9 U-a keeps (E5 schemas dropped,
	// multirange names).
	rev19 rev19State
	// rev19b is what Revision 1.9 U-b keeps (state records), and tx the
	// current migration's block tracker (Y03 reads its savepoints).
	rev19b rev19bState
	// rev19c is what Revision 1.9 U-c keeps (owned sequences, Y40).
	rev19c  rev19cState
	tx      *txTracker
	failure *failure
	dropped []droppedTable
	// lexing is set once a setting that changes lexing was changed
	// (Revision 3 G8); every later migration may fail.
	lexing string
	// deferring collects the foreign keys of an ALTER TABLE statement,
	// which Postgres adds after the statement's other actions.
	deferring bool
	pending   []pendingKey
	// m and at locate the statement being applied, and row is its
	// contract row (T018f: the row of the partial reasons it adds).
	m   *migration
	at  byteSpan
	row string
	// names are the names the set creates anywhere, and ledger the
	// functions, schemas and temporary relations held so far (P1).
	names  *nameSet
	ledger ledger
	// unsafe are enum labels added in the current migration's transaction
	// to types that existed before it (K2).
	unsafe []unsafeLabel
	// routines are the kinds of the set's functions and procedures held so
	// far: "function", "procedure" or "both" (P4).
	routines map[string]string
	// functionReturns is the result type of each set function defined
	// without arguments ("?" when not proven: arguments, several
	// definitions, a rename or a drop; P5).
	functionReturns map[string]string
	// routineDefs are the routine definitions the pass read, by bare name,
	// and routineUnknown the names a statement it did not read may have
	// changed (U6).
	routineDefs    map[string][]*routineDef
	routineUnknown map[string]bool
	// code is what code run by O1 statements may have done (U2, P12,
	// codeeffects.go).
	code codeState
}

func newState(set *migrationSet) *state {
	s := &state{set: set, temps: map[string]bool{}, relations: map[string]*relation{}, ledger: ledger{}}
	for _, m := range set.migrations {
		if sequenceUse.Match(m.source) {
			s.sequenceUse = true
		}
		if sequenceCall.Match(m.source) {
			s.sequenceCalls = true
		}
	}
	return s
}

// pendingKey is a foreign key whose resolution waits for the end of its
// ALTER TABLE statement.
type pendingKey struct {
	t       *table
	ref     referenceDef
	at      byteSpan
	certain bool
}

// begin starts applying a migration. After a lexing setting changed in an
// earlier migration, this one may lex differently and may fail (G8).
func (s *state) begin(m *migration) {
	s.m, s.dropped, s.unsafe = m, nil, nil
	if s.lexing != "" {
		s.at = byteSpan{}
		s.mayFail(s.lexing)
	}
	if s.laterMayFail != "" {
		s.at = byteSpan{}
		s.unknownFact(s.laterMayFail)
	}
}

// complete reports whether t has no partial reason and no opaque statement
// ran after its creation: only then does a missing column or key prove
// that Postgres refuses a statement (Revision 3 class 3).
func complete(t *table) bool {
	return len(t.reasons) == 0 && t.opaqueAfter == 0
}

// factsKnown reports whether the pass knows t's columns, keys and
// dependents exactly: no opaque statement ran since its creation and every
// partial reason is about its name only. Revision 8 uses it where a
// statement's success depends on those facts; certain refusals keep
// requiring complete.
func factsKnown(t *table) bool {
	if t.opaqueAfter > 0 {
		return false
	}
	for _, r := range t.reasons {
		if !r.nameOnly && !r.indexOnly {
			return false
		}
	}
	return true
}

// outsideReferences applies P8 to DROP TABLE of a name the pass does not
// hold (g1-s48011): foreign keys of the set written to that name depend on
// the outside table. With CASCADE they are dropped with it (the tables are
// partial); without it Postgres refuses the drop while one exists (2BP01).
func (s *state) outsideReferences(q qname, d *dropObjects) {
	for _, t := range slices.Clone(s.tables) {
		for _, fk := range slices.Clone(t.foreignKeys) {
			ref := fk.written.table
			if fk.table != nil || ref.name != q.name || ref.schema != nil && q.schema != nil && *ref.schema != *q.schema {
				continue
			}
			s.touch(t)
			if d.cascade {
				if sameSchema(ref.schema, q.schema) && (q.schema != nil || !s.pathChanged) {
					s.dropForeignKey(fk)
				}
				s.partialOnce(t, fmt.Sprintf("DROP TABLE %s CASCADE may have dropped a foreign key of this table%s", q, precondition("M6.P8")))
				continue
			}
			s.partialOnce(t, fmt.Sprintf("DROP TABLE %s may be refused: a foreign key of this table may reference it%s", q, precondition("M6.P8")))
			s.unknownFact(fmt.Sprintf("DROP TABLE %s without CASCADE may be refused: a foreign key of the set may reference it%s", q, precondition("M6.P8")))
		}
	}
}

// viewsMayBeGone applies P1 and P8 to DROP TABLE and DROP COLUMN with
// CASCADE (g1-s31160): a view or materialized view the set created may
// depend on what is dropped and go with it, so its name is no longer
// proven held.
func (s *state) viewsMayBeGone() {
	for _, r := range s.relations {
		if r.kind == "view" || r.kind == "materialized view" {
			r.uncertain = true
		}
	}
}

// onlyChildren applies P8 to ALTER TABLE ONLY t ADD COLUMN and ALTER
// COLUMN TYPE (g1-s27299): Postgres refuses them while t has inheritance
// children (tablecmds.c ATPrepAddColumn, ATPrepAlterColumnType: "column
// must be added to child tables too", "type of inherited column must be
// changed in child tables too", 42P16). Children the pass knows make it a
// refusal; after an opaque statement on t, children it does not know may
// exist. It reports whether the subcommand applies.
func (s *state) onlyChildren(t *table, a alterAction, what string) bool {
	if !a.only {
		return true
	}
	if len(t.children) > 0 {
		s.refuse(t, fmt.Sprintf("ALTER TABLE ONLY %s %s: the table has inheritance children (Postgres refuses)%s", t.q, what, precondition("M5a.P8")))
		return false
	}
	if t.opaqueAfter > 0 {
		s.unproven(t, fmt.Sprintf("ALTER TABLE ONLY %s %s may be refused: inheritance children the pass does not know may exist%s", t.q, what, precondition("M5a.P8")))
	}
	return true
}

// unnamedUnknown reports C5 (T018a2 B6 as amended): whether a statement the
// pass did not model may have created unnamed objects (keys, indexes,
// constraints, sequences, whose names Postgres generates) on t. It is
// false while t's facts are known, and on a partial table whose every
// reason is on the closed "no" list: name moves (nameOnly), one index that
// may be gone (indexOnly), and the ALTER TABLE words of unnamedFreeWords
// (contract.go). An opaque statement since t was created, and every other
// reason, may have created any.
func unnamedUnknown(t *table) bool {
	if t.opaqueAfter > 0 {
		return true
	}
	for _, r := range t.reasons {
		if !r.nameOnly && !r.indexOnly && !r.noUnnamed {
			return true
		}
	}
	return false
}

// partialName adds a reason about t's name only (see factsKnown).
func (s *state) partialName(t *table, text string) {
	s.partial(t, text)
	t.reasons[len(t.reasons)-1].nameOnly = true
}

// missing reports a column a statement names that t does not have: on a
// complete table whose name surely means t, Postgres refuses the statement;
// otherwise the column may exist and t is partial, and since Postgres
// refuses a missing column the migration may fail (Revision 8).
func (s *state) missing(t *table, certain bool, text string) {
	if certain && complete(t) {
		s.refuse(t, text+" (Postgres refuses)")
		return
	}
	s.partial(t, text)
	s.unknownFact(text + "; Postgres refuses a missing column")
}

// unproven records a statement whose success depends on a fact the pass
// does not know (Revision 8): t (when known) is partial, and the migration
// may fail (WF8). Postgres may refuse the statement, and an applied: true
// would claim it did not.
func (s *state) unproven(t *table, text string) {
	if t != nil {
		s.partialOnce(t, text)
	}
	s.unknownFact(text)
}

// unknownFact makes the current migration one that may fail because a
// statement depends on an unknown fact. Once a possible failure is
// recorded this migration and every later one are already of unknown
// applied state, so nothing more is reported.
func (s *state) unknownFact(text string) {
	if s.failure == nil {
		s.mayFail(text)
	}
}

// uncertainName reports whether target's certain == false comes from the
// name (search_path, another qualification, a temporary table) rather than
// from a possible failure, which already makes the migration's applied
// state unknown.
func (s *state) uncertainName(certain bool) bool {
	return !certain && s.failure == nil
}

// nameTaken checks the relation namespace before a statement gives q to a
// table or index. It returns a refusal text when a known table or a
// relation that surely exists holds the name, and a "may" text when one
// may (an uncertain relation, or a table of that name whose written
// qualification may be the same schema). skip, when set, is the table that
// takes the name (SET SCHEMA), which does not count.
func (s *state) nameTaken(q qname, skip *table) (refused, maybe string) {
	exact, others := s.lookup(q)
	if exact != nil {
		return fmt.Sprintf("table %s already exists", q.name), ""
	}
	if kind, certain, found := s.relationAt(q); found {
		if certain {
			return fmt.Sprintf("%s named %s exists", article(kind), q.name), ""
		}
		return "", fmt.Sprintf("%s named %s may exist", article(kind), q.name)
	}
	for _, o := range others {
		if o != skip && namesTable(q, o) {
			return "", fmt.Sprintf("table %s may be in the same schema", o.q)
		}
	}
	return "", ""
}

func sameSchema(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// lookup returns the table with exactly this qualification and the known
// tables of the same name under another qualification (P3).
func (s *state) lookup(q qname) (exact *table, others []*table) {
	for _, t := range s.tables {
		if t.q.name != q.name {
			continue
		}
		if sameSchema(t.q.schema, q.schema) {
			exact = t
		} else {
			others = append(others, t)
		}
	}
	return exact, others
}

func (s *state) partial(t *table, text string) {
	t.reasons = append(t.reasons, reason{migration: s.m, span: s.at, text: text, row: s.reasonRow(text)})
	s.set.diagnostic(s.m, s.at, "info", "schema_partial_table", fmt.Sprintf("table %s is partial: %s", t.q, text))
}

// partialOnce adds a reason unless the table already has it.
func (s *state) partialOnce(t *table, text string) {
	for _, r := range t.reasons {
		if r.text == text {
			return
		}
	}
	s.partial(t, text)
}

func (s *state) unresolved(text string) {
	s.set.diagnostic(s.m, s.at, "info", "schema_unresolved_reference", text)
}

// target resolves the table a modeled statement changes. It returns nil
// when no single known table is proven; the tables it may be are partial.
// certain is false when the name may resolve elsewhere (P3, R2, temporary
// tables) or the statement may not apply (WF8): a destructive change then
// keeps what it would remove.
func (s *state) target(q qname) (*table, bool) {
	exact, others := s.lookup(q)
	if len(others) > 0 {
		for _, t := range append(others, exact) {
			if t != nil {
				s.partial(t, fmt.Sprintf("%s may name this table under another qualification (search_path)", q))
			}
		}
		if exact == nil {
			s.unresolved(fmt.Sprintf("table %s is not known under this qualification", q))
		}
		return exact, false
	}
	if exact == nil {
		return nil, false
	}
	certain := s.unqualified(exact, q)
	return exact, certain && s.failure == nil
}

// unqualified applies R2 and temporary-table shadowing to an unqualified
// name used after them; it reports whether the name surely means t.
func (s *state) unqualified(t *table, q qname) bool {
	if q.schema != nil {
		return true
	}
	if s.pathChanged {
		s.partial(t, "search_path may have changed earlier in the set; the schema of this name is unknown")
		return false
	}
	if s.temps[q.name] {
		s.partial(t, "a temporary table of this name may shadow it")
		return false
	}
	return true
}

// unknown reports a statement on a table no known record matches. When a
// table of that name exists under another qualification, target has
// already reported it.
func (s *state) unknown(q qname, ifExists bool, what string) {
	if _, others := s.lookup(q); len(others) > 0 || ifExists {
		return
	}
	s.unresolved(fmt.Sprintf("%s %s: table is not known", what, q))
}

func (s *state) alter(t *table) {
	if t.creator == s.m {
		return
	}
	for _, a := range t.alterations {
		if a.migration == s.m {
			if a.spans[len(a.spans)-1] != s.at {
				a.spans = append(a.spans, s.at)
			}
			return
		}
	}
	t.alterations = append(t.alterations, &alteration{migration: s.m, spans: []byteSpan{s.at}})
}

// touch records that the current statement changes t: an alters edge and,
// once a migration may have failed, the reason the change may not apply.
func (s *state) touch(t *table) {
	s.alter(t)
	s.failureReason(t)
}

func (s *state) failureReason(t *table) {
	if s.failure == nil {
		return
	}
	if s.failure.m == s.m {
		s.partialOnce(t, s.failureText(s.failure.reason))
		return
	}
	s.partialOnce(t, fmt.Sprintf("%s is applied only if %s did not fail (%s)", path.Base(s.m.file), path.Base(s.failure.m.file), s.failure.reason))
}

func (s *state) failureText(reason string) string {
	return fmt.Sprintf("%s may fail as a whole (%s), so its changes to this table may not apply", path.Base(s.m.file), reason)
}

// mayFail records that Postgres refuses or may refuse the current
// statement (WF8). Postgres parses and runs a migration as one query in one
// transaction, and sqlx stops the set at the first failed migration
// (migrator.rs 168-182 at 0.8.6), so the changes of this migration and of
// every later one are unknown: the tables they touch become partial, tables
// this migration dropped are restored as partial, and the applied state of
// these migrations becomes unknown.
func (s *state) mayFail(reason string) {
	if s.failure == nil {
		s.failure = &failure{m: s.m, reason: reason}
	}
	s.set.diagnostic(s.m, s.at, "info", "schema_migration_may_fail", reason+"; the migration may fail as a whole, and sqlx stops the set at a failed migration, so its changes and those of later migrations are unknown")
	text := s.failureText(reason)
	for _, t := range s.tables {
		if t.creator == s.m || t.alteredBy(s.m) {
			s.partialOnce(t, text)
		}
	}
	for _, d := range s.dropped {
		if exact, _ := s.lookup(d.t.q); exact != nil {
			s.partialOnce(exact, fmt.Sprintf("%s dropped a table of this name earlier and may fail as a whole (%s)", path.Base(s.m.file), reason))
			continue
		}
		s.tables = append(s.tables, d.t)
		s.partialOnce(d.t, fmt.Sprintf("dropped by %s, which may fail as a whole (%s); the table may still exist", path.Base(s.m.file), reason))
		for _, o := range d.owners {
			if slices.Contains(s.tables, o) {
				s.partialOnce(o, fmt.Sprintf("a foreign key to %s was dropped by %s, which may fail as a whole", d.t.q, path.Base(s.m.file)))
			}
		}
	}
	s.dropped = nil
	for _, r := range s.relations {
		if r.m == s.m {
			r.uncertain = true
		}
	}
}

// refuse marks t partial for a statement Postgres refuses; the migration
// may fail as a whole.
func (s *state) refuse(t *table, text string) {
	s.partial(t, text)
	s.mayFail(text)
}

// remove drops t; owners are the tables whose foreign keys to t were
// dropped with it.
func (s *state) remove(t *table, owners []*table) {
	s.tables = slices.DeleteFunc(s.tables, func(x *table) bool { return x == t })
	s.dropped = append(s.dropped, droppedTable{t: t, owners: owners})
}

// opaque applies an opaque statement: every existing table counts it.
func (s *state) opaque(kind string) {
	s.opaqueData(kind, false)
}

// opaqueData applies an opaque statement; rlsInert marks an SB-M9 data
// statement, which cannot change row level security, so the tables'
// rlsOpaque counts do not count it and later data statements may still
// be SB-M9's.
func (s *state) opaqueData(kind string, rlsInert bool) {
	s.opaqueSeen++
	s.set.opaque[kind]++
	if !rlsInert {
		s.rlsBlocked = true
	}
	for _, t := range s.tables {
		t.opaqueAfter++
		if !rlsInert {
			t.rlsOpaque++
		}
	}
}

// apply runs one analysed statement against the state.
// statementHook is nil in production. TestContractFacts sets it to see the
// facts each statement writes: it is called before (after false) and after
// (after true) the state applies one statement (B7, Revision 1.4 R6).
var statementHook func(s *state, after bool)

func (s *state) apply(a analysis) {
	switch {
	case a.create != nil:
		s.createTable(a.create)
	case a.index != nil:
		s.createIndex(a.index)
	case a.alter != nil:
		s.alterTable(a.alter)
	case a.drop != nil && a.drop.kind == "table":
		s.dropTables(a.drop)
	case a.drop != nil && a.drop.kind == "index":
		s.dropIndexes(a.drop)
	case a.drop != nil && a.drop.kind == "schema":
		if !a.drop.ifExists {
			for _, q := range a.drop.names {
				s.needs(qname{name: q.name}, kindSchema, false, "DROP SCHEMA "+q.name, "R5.P1")
			}
		}
		s.dropSchemas(a.drop)
	case a.alterIndex != nil:
		s.alterIndex(a.alterIndex)
	case a.enum != nil:
		s.enumOperation(a.enum)
	case a.schemaRename != nil:
		s.needs(qname{name: a.schemaRename[0]}, kindSchema, false, "ALTER SCHEMA "+a.schemaRename[0], "M9.P1")
		s.renameSchema(a.schemaRename[0], a.schemaRename[1])
	}
	for _, o := range a.relations {
		s.createRelation(o)
	}
	if a.dropOthers != nil {
		s.dropDependencies(a.dropOthers.kind, a.dropOthers.names, a.dropOthers.cascade, nil)
	}
	for _, o := range a.dropped {
		s.dropRelation(o)
	}
	for _, p := range a.inherits {
		if a.alter != nil && s.circular(p, *a.inheritor) {
			continue
		}
		if t, _ := s.lookup(p); t != nil && !slices.ContainsFunc(t.children, func(q qname) bool { return q.String() == a.inheritor.String() }) {
			t.children = append(t.children, *a.inheritor)
		}
	}
	if a.lexing != "" && s.lexing == "" {
		s.lexing = a.lexing
	}
	if a.renamed != nil {
		s.renameRelation(*a.renamed)
	}
	for _, d := range a.dependents {
		for _, t := range s.tables {
			if t.uses(d) {
				s.partial(t, d.why)
			}
		}
	}
	if a.allTables != "" {
		for _, t := range s.tables {
			s.partial(t, a.allTables)
		}
	}
	if a.temp != nil {
		s.temps[a.temp.name] = true
	}
	if a.view {
		s.views = true
	}
	if a.searchPath {
		s.pathChanged, s.rev16.pathSet = true, true
		s.changePath(a.path)
	}
	if a.mayFail != "" {
		s.mayFail(a.mayFail)
	}
	if a.notProven != "" {
		s.unknownFact(a.notProven)
	}
	if a.runsFunction != "" {
		s.opaque("function_call")
	}
	if a.class == classError {
		if a.target != nil {
			if t, _ := s.target(*a.target); t != nil {
				s.touch(t)
				// SB-M3: an unread statement on the table may change its
				// row level security (T024 review).
				t.rlsUnknown = true
				s.partial(t, "statement could not be read: "+a.reason)
				s.mayFail("a statement could not be read: " + a.reason)
				return
			}
		}
		s.mayFail("a statement could not be read: " + a.reason)
	}
	if a.opaqueKind != "" && (a.class == classOpaque || a.class == classError) {
		s.opaqueData(a.opaqueKind, a.rlsInert)
	}
}

// circular applies ALTER TABLE child INHERIT parent when parent is child or
// inherits from it (Revision 4 G-a): Postgres refuses circular inheritance
// (tablecmds.c 15732). It reports whether the statement is refused.
func (s *state) circular(parent, child qname) bool {
	c, others := s.lookup(child)
	if c == nil || len(others) > 0 {
		return false
	}
	seen := map[string]bool{}
	queue := []qname{child}
	for len(queue) > 0 {
		q := queue[0]
		queue = queue[1:]
		if q.String() == parent.String() {
			s.refuse(c, fmt.Sprintf("INHERIT %s makes inheritance circular: %s already inherits from %s (Postgres refuses)", parent, parent, child))
			return true
		}
		if seen[q.String()] {
			continue
		}
		seen[q.String()] = true
		if t, _ := s.lookup(q); t != nil {
			queue = append(queue, t.children...)
		}
	}
	return false
}

// uses reports whether a column, default, index or constraint of t uses the
// object (G1).
func (t *table) uses(d dependentObject) bool {
	for _, c := range t.columns {
		switch d.kind {
		case "type":
			if slices.Contains(c.typeNames, d.name) {
				return true
			}
		case "collation":
			if c.collation != nil && (*c.collation == d.name || strings.HasSuffix(*c.collation, "."+d.name)) {
				return true
			}
		case "function":
			if slices.Contains(c.exprNames, d.name) {
				return true
			}
		case "sequence":
			if c.def != nil && strings.Contains(strings.ToLower(*c.def), d.name) {
				return true
			}
			if (c.implied || c.identity != nil) && d.name == seqName(t, c) {
				return true
			}
		}
	}
	if d.kind == "function" {
		for _, ix := range t.indexes {
			if slices.Contains(ix.whereNames, d.name) {
				return true
			}
			for _, el := range ix.elements {
				if slices.Contains(el.names, d.name) {
					return true
				}
			}
		}
		for _, ch := range t.checks {
			if slices.Contains(ch.names, d.name) {
				return true
			}
		}
	}
	return false
}

// relationAt finds a relation or type other than a modeled table under
// this name. certain is false when an opaque statement, a search_path
// change or a possible failure since it was recorded may have removed it,
// or when it only matches under another qualification.
func (s *state) relationAt(q qname) (kind string, certain, found bool) {
	pathOK := q.schema != nil || !s.pathChanged
	if r := s.relations[q.String()]; r != nil {
		return r.kind, !r.uncertain && r.opaqueAt == s.opaqueSeen && pathOK, true
	}
	if _, ix := s.findIndex(q); ix != nil {
		return "index", pathOK && s.failure == nil && !ix.mayBeGone, true
	}
	if _, k := s.findKey(q); k != nil {
		// P2 (g1-s34388): the index of a named primary key or UNIQUE
		// constraint holds its name in pg_class.
		return "index", pathOK && s.failure == nil, true
	}
	for _, k := range s.relationKeys() {
		if k == q.name || strings.HasSuffix(k, "."+q.name) {
			return s.relations[k].kind, false, true
		}
	}
	if hits := s.indexesNamed(q); len(hits) > 0 {
		return "index", false, true
	}
	for _, t := range s.tables {
		if sameSchema(t.q.schema, q.schema) && slices.Contains(generatedNames(t), q.name) {
			return "generated index or sequence", false, true
		}
	}
	return "", false, false
}

// article prefixes a kind with "a" or "an".
func article(kind string) string {
	if strings.ContainsAny(kind[:1], "aeiou") {
		return "an " + kind
	}
	return "a " + kind
}

// relationKeys returns the recorded relation names in order, so that
// messages do not depend on map order.
func (s *state) relationKeys() []string {
	keys := make([]string, 0, len(s.relations))
	for k := range s.relations {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// generatedNames are the names Postgres generates for t's unnamed primary
// key, unique constraints, bare-column indexes and serial or identity
// sequences when nothing else holds them (ChooseRelationName; truncation
// and collision suffixes are not computed).
func generatedNames(t *table) []string {
	out := generatedKeyNames(t)
	for _, ix := range t.indexes {
		if ix.name == nil && !slices.Contains(ix.columns, nil) {
			out = append(out, orGenerated(ix.gen, t.q.name+"_"+strings.Join(names(ix.columns), "_")+"_idx"))
		}
	}
	for _, c := range t.columns {
		if c.implied || c.identity != nil {
			out = append(out, seqName(t, c))
		}
	}
	return out
}

// indexGenBase is the name Postgres chooses for an unnamed index before
// collision digits (ChooseIndexName, ChooseIndexColumnNames, indexcmds.c
// REL_17_6): <table>_<element names>_idx, where a bare column is named by
// itself, a plain function call by its function, a repeated name gets a
// digit; "" when an element's name is not derived.
func indexGenBase(t *table, ix *index, elements []indexElement) string {
	var parts []string
	seen := map[string]bool{}
	for i, el := range elements {
		var n string
		switch {
		case ix.columns[i] != nil:
			n = ix.columns[i].name
		case el.colname != nil:
			n = *el.colname
		default:
			return ""
		}
		base := n
		for d := 1; seen[n]; d++ {
			n = fmt.Sprintf("%s%d", base, d)
		}
		seen[n] = true
		parts = append(parts, n)
	}
	return t.q.name + "_" + strings.Join(parts, "_") + "_idx"
}

// generatedExcluded reports, for a table whose facts are known, that name
// cannot be the generated name of one of its unnamed objects with this
// suffix: every such object's generated name is known and none has name's
// base (name without collision digits). P3 uses it to tell a name that may
// be held from one that is not.
func generatedExcluded(t *table, name, suffix string) bool {
	if !factsKnown(t) || len(name) >= maxIdentifier {
		return false
	}
	base := strings.TrimRight(name, "0123456789")
	var gens []string
	switch suffix {
	case "pkey":
		if t.primaryKey != nil && t.primaryKey.name == nil {
			gens = append(gens, t.primaryKey.gen)
		}
	case "key":
		for _, u := range t.uniques {
			if u.name == nil {
				gens = append(gens, u.gen)
			}
		}
	case "idx":
		for _, ix := range t.indexes {
			if ix.name == nil {
				gens = append(gens, ix.gen)
			}
		}
	case "seq":
		for _, c := range t.columns {
			if c.implied || c.identity != nil {
				gens = append(gens, c.seq)
			}
		}
	default:
		return false
	}
	for _, g := range gens {
		if g == "" || g == base {
			return false
		}
	}
	return true
}

// generatedMayHold is generatedRelationName without the tables whose
// generated names are known to differ (generatedExcluded).
func (s *state) generatedMayHold(q qname) string {
	for _, t := range s.tables {
		if !namesTable(qname{schema: q.schema, name: t.q.name}, t) {
			continue
		}
		if suf := generatedShape(t, q.name, relationSuffixes); suf != "" && !generatedExcluded(t, q.name, suf) {
			return fmt.Sprintf("%s may be the generated name of an unnamed object (%s) of %s", q.name, suf, t.q)
		}
	}
	return ""
}

// orGenerated returns the name recorded at creation, or the name computed
// from the current names when none was recorded.
func orGenerated(recorded, computed string) string {
	if recorded != "" {
		return recorded
	}
	return computed
}

// seqName is the generated name of c's sequence.
func seqName(t *table, c *column) string {
	return orGenerated(c.seq, t.q.name+"_"+c.name+"_seq")
}

// createRelation records a relation or type other than a modeled table.
func (s *state) createRelation(o namedObject) {
	if t, _ := s.lookup(o.q); t != nil {
		certain := s.unqualified(t, o.q)
		if o.ifNotExists && relationKinds[o.kind] {
			return
		}
		if certain {
			s.refuse(t, fmt.Sprintf("%s named %s is created while this table exists (Postgres refuses)", article(o.kind), o.q))
		} else {
			s.partial(t, fmt.Sprintf("%s named %s may have been created instead", article(o.kind), o.q))
			s.unknownFact(fmt.Sprintf("CREATE %s %s may be refused: a table of this name may exist", strings.ToUpper(o.kind), o.q))
		}
		return
	}
	if kind, certain, found := s.relationMeeting(o.q); found && namespacesMeet(o.kind, kind) {
		// Revision 8: a relation or type of this name exists or may.
		why := s.existingRelation(o, kind)
		switch {
		case why == "":
			return
		case certain:
			s.mayFail(why + " (Postgres refuses)")
			return
		default:
			s.unknownFact(why + " may exist; Postgres refuses the name if it does")
		}
	} else if o.ifNotExists && found && relationKinds[o.kind] && relationKinds[kind] {
		return
	}
	if relationKinds[o.kind] && !o.ifNotExists {
		if gen := s.generatedRelationName(o.q); gen != "" {
			s.unknownFact(fmt.Sprintf("CREATE %s %s may be refused: %s", strings.ToUpper(o.kind), o.q, gen))
		}
	}
	key := o.q.String()
	s.relations[key] = &relation{kind: o.kind, m: s.m, opaqueAt: s.opaqueSeen, uncertain: o.uncertain || s.failure != nil || o.q.schema == nil && s.pathChanged, labels: slices.Clone(o.labels)}
}

// relationMeeting is relationAt without the matches that surely live in
// another schema: a qualified name never meets a recorded relation written
// under a different schema.
func (s *state) relationMeeting(q qname) (kind string, certain, found bool) {
	kind, certain, found = s.relationAt(q)
	if !found || certain || q.schema == nil || s.relations[q.String()] != nil {
		return kind, certain, found
	}
	if _, ix := s.findIndex(q); ix != nil || len(s.indexesNamed(q)) > 0 {
		return kind, certain, found
	}
	for _, t := range s.tables {
		if sameSchema(t.q.schema, q.schema) && slices.Contains(generatedNames(t), q.name) {
			return kind, certain, found
		}
	}
	if r := s.relations[q.name]; r != nil {
		return r.kind, false, true
	}
	return "", false, false
}

// maxIdentifier is NAMEDATALEN - 1: Postgres truncates generated names to
// this length (ChooseRelationName, makeObjectName).
const maxIdentifier = 63

// relationSuffixes are the generated-name suffixes of objects in pg_class:
// key indexes, unnamed indexes, exclusion indexes and serial or identity
// sequences.
var relationSuffixes = []string{"pkey", "key", "idx", "excl", "seq"}

// constraintSuffixes are the generated-name suffixes of a table's
// constraints.
var constraintSuffixes = []string{"pkey", "key", "fkey", "check", "excl"}

// hasUnnamed reports whether t holds, or may hold, an unnamed object whose
// generated name ends in suffix. When something the pass did not model may
// have created unnamed objects on t (unnamedUnknown) it may hold any.
func hasUnnamed(t *table, suffix string) bool {
	if unnamedUnknown(t) {
		return true
	}
	switch suffix {
	case "pkey":
		return t.primaryKey != nil && t.primaryKey.name == nil
	case "key":
		return slices.ContainsFunc(t.uniques, func(k *key) bool { return k.name == nil })
	case "fkey":
		return slices.ContainsFunc(t.foreignKeys, func(fk *foreignKey) bool { return fk.name == nil })
	case "idx":
		return slices.ContainsFunc(t.indexes, func(ix *index) bool { return ix.name == nil })
	case "check", "excl":
		return t.unnamedChecks
	case "seq":
		return slices.ContainsFunc(t.columns, func(c *column) bool { return c.implied || c.identity != nil })
	}
	return false
}

// generatedShape reports the suffix when name has the shape of a name
// Postgres may have generated for an unnamed object of t (Revision 8):
// <t>_pkey, <t>_check, or <t>_<...>_<suffix>, each optionally followed by collision
// digits; a name of maximum length may be a truncated one. Generated names
// are never exact (Revision 7), so the shape is enough.
func generatedShape(t *table, name string, suffixes []string) string {
	base := strings.TrimRight(name, "0123456789")
	for _, suf := range suffixes {
		head, ok := strings.CutSuffix(base, "_"+suf)
		if !ok || head == "" {
			continue
		}
		// P2: a generated name keeps the table name it was made from, so a
		// former name of the table shapes it too.
		shaped := len(name) >= maxIdentifier
		for _, n := range append([]string{t.q.name}, t.previousNames...) {
			// <t>_pkey, and <t>_check for a CHECK over no or several
			// columns (heap.c AddRelationNewConstraints, Revision 1.4 R2).
			shaped = shaped || (suf == "pkey" || suf == "check") && head == n || strings.HasPrefix(head, n+"_") && len(head) > len(n)+1
		}
		if shaped && hasUnnamed(t, suf) {
			return suf
		}
	}
	return ""
}

// generatedRelationName describes a known table whose unnamed index or
// sequence may have q's name ("" when none): a new relation of that name
// may be refused.
func (s *state) generatedRelationName(q qname) string {
	for _, rel := range slices.Sorted(maps.Keys(s.outsideChanged)) {
		for _, suf := range relationSuffixes {
			if generatedShapeOf(rel, q.name, suf) {
				return fmt.Sprintf("%s may be the generated name of an unnamed object (%s) the set created on %s, which the pass does not hold", q.name, suf, rel)
			}
		}
	}
	for _, t := range s.tables {
		if !namesTable(qname{schema: q.schema, name: t.q.name}, t) {
			continue
		}
		if suf := generatedShape(t, q.name, relationSuffixes); suf != "" {
			return fmt.Sprintf("%s may be the generated name of an unnamed object (%s) of %s", q.name, suf, t.q)
		}
	}
	return ""
}

// indexRefusedWords are the M5k words of actions Postgres refuses on an
// index (tablecmds.c ATSimplePermissions, 42809, checked with psql).
var indexRefusedWords = map[string]bool{
	"row_security": true, "trigger_all": true, "enable_trigger": true, "enable_rule": true,
	"set_logged": true, "set_unlogged": true, "set_without_cluster": true, "set_without_oids": true,
	"replica_identity": true, "replica_identity_index": true, "cluster_on": true, "no_inherit": true,
	"of": true, "detach": true, "validate": true, "alter_constraint": true,
}

// indexAction applies P4 (kinds, Revision 1.4) to an ALTER TABLE action
// on a name that is an index: ADD/DROP COLUMN, constraints, ALTER COLUMN
// TYPE, NOT NULL and DEFAULT and the words of indexRefusedWords need a
// table (tablecmds.c ATSimplePermissions: 42809 "ALTER action ... cannot
// be performed on relation"). A refusal when the records prove the name
// is that index, not proven when it may name one under another
// qualification.
func (s *state) indexAction(q qname, action string) {
	what := fmt.Sprintf("ALTER TABLE %s (%s)", q, action)
	_, ix := s.findIndex(q)
	_, k := s.findKey(q)
	if (ix != nil || k != nil) && (q.schema != nil || !s.pathChanged) {
		s.mayFail(fmt.Sprintf("%s names an index, on which Postgres refuses the action (42809)%s", what, precondition("M5a.P1")))
		return
	}
	s.unknownFact(fmt.Sprintf("%s may be refused: the name may be an index, on which Postgres refuses the action (42809)%s", what, precondition("M5a.P1")))
}

// outsideChange applies P2 (Revision 1.4) to a CREATE INDEX or ALTER
// TABLE on a table the pass does not hold: E1 grants the table as the set
// needs it only until the set changes it, so once an earlier statement
// created an index on it or altered it, the names and columns a later one
// meets may be the set's own (a generated index name, a constraint name,
// an added or dropped column): not proven.
func (s *state) outsideChange(q qname, what, id string) {
	if s.outsideChanged[q.name] {
		s.unknownFact(fmt.Sprintf("%s may be refused: the set changed this table, which the pass does not hold, earlier, so its names and columns are not proven%s", what, precondition(id)))
	}
	if s.outsideChanged == nil {
		s.outsideChanged = map[string]bool{}
	}
	s.outsideChanged[q.name] = true
}

// generatedConstraintName describes why ADD CONSTRAINT name on t may meet
// a generated name: one of t's unnamed constraints, or, for a constraint
// with an index, an unnamed index or sequence in the schema.
func (s *state) generatedConstraintName(t *table, name string, indexed bool) string {
	if suf := generatedShape(t, name, constraintSuffixes); suf != "" {
		return fmt.Sprintf("%s may be the generated name of an unnamed constraint (%s) of %s", name, suf, t.q)
	}
	if indexed {
		return s.generatedRelationName(qname{schema: t.q.schema, name: name})
	}
	return ""
}

// rowTypeKinds are the kinds with an entry in pg_type: types and domains,
// and the relations that get a row type (heap.c 1316-1324: not sequences
// or indexes).
var rowTypeKinds = map[string]bool{
	"table": true, "foreign table": true, "view": true, "materialized view": true, "composite type": true,
	"type": true, "domain": true,
}

// namespacesMeet reports whether objects of the two kinds share a
// namespace in a schema: pg_class for relations (an unwritten generated
// name counts as an index or sequence), pg_type for types and row types
// (heap.c 1164-1182: a relation's row type conflicts with a type).
func namespacesMeet(a, b string) bool {
	class := func(k string) bool { return relationKinds[k] || k == "generated index or sequence" }
	// P2: every relation heap_create_with_catalog makes (not an index)
	// also checks pg_type for its name (heap.c 1164-1182, 42710), so it
	// meets any type, row type or domain of the name.
	return class(a) && class(b) || rowTypeKinds[a] && rowTypeKinds[b] || relationKinds[a] && a != "index" && rowTypeKinds[b]
}

// existingRelation describes why CREATE o meets an existing object of
// kind; "" when Postgres skips the statement instead (IF NOT EXISTS over a
// relation; it still fails on a type only, heap.c 1164-1182). CREATE OR
// REPLACE VIEW over a view replaces it only when the old column list is a
// prefix of the new one, which the pass does not know; over another
// relation it is refused (view.c 110-116).
func (s *state) existingRelation(o namedObject, kind string) string {
	what := fmt.Sprintf("CREATE %s %s: %s of this name", strings.ToUpper(o.kind), o.q, article(kind))
	switch {
	case o.ifNotExists && relationKinds[kind]:
		return ""
	case o.orReplace && kind == "view":
		s.unknownFact(fmt.Sprintf("CREATE OR REPLACE VIEW %s may be refused: whether the new column list extends the old one is unknown", o.q))
		return ""
	}
	return what
}

// dropRelation forgets a dropped relation or type. Naming a table with
// another object kind is refused (tablecmds.c 17907-17937; a table's row
// type cannot be dropped alone).
func (s *state) dropRelation(o namedObject) {
	if t, _ := s.lookup(o.q); t != nil {
		s.refuse(t, fmt.Sprintf("DROP %s %s names this table (Postgres refuses)", strings.ToUpper(o.kind), o.q))
		return
	}
	key := o.q.String()
	if kind, certain, found := s.relationAt(o.q); found && !dropKindFits(o.kind, kind) {
		// P4: DROP VIEW of a table, DROP DOMAIN of a type, ... is refused
		// even with IF EXISTS (tablecmds.c DropErrorMsgWrongType, 42809).
		if certain {
			s.unknownFact(fmt.Sprintf("DROP %s %s is refused: it names %s%s", strings.ToUpper(o.kind), o.q, article(kind), precondition("R5.P4")))
		} else {
			s.unknownFact(fmt.Sprintf("DROP %s %s may be refused: the name may be %s%s", strings.ToUpper(o.kind), o.q, article(kind), precondition("R5.P4")))
		}
	}
	if r := s.relations[key]; r != nil {
		if o.q.schema == nil && s.pathChanged || s.failure != nil {
			r.uncertain = true
		} else {
			delete(s.relations, key)
		}
	}
	for k, r := range s.relations {
		if k != key && (k == o.q.name || strings.HasSuffix(k, "."+o.q.name)) {
			r.uncertain = true
		}
	}
}

// dropKindFits reports whether DROP <drop> accepts an object recorded as
// held (P4): each relation kind only its own DROP, DROP TYPE any type
// except a domain's own spelling (a domain is a type too), DROP DOMAIN
// only a domain.
func dropKindFits(drop, held string) bool {
	switch drop {
	case "type":
		return held == "type" || held == "composite type" || held == "domain"
	case "domain":
		return held == "domain"
	}
	return drop == held
}

// renameRelation applies ALTER VIEW/SEQUENCE/TYPE/DOMAIN to the recorded
// relations; any form other than RENAME TO makes the name uncertain (SET
// SCHEMA moves it).
func (s *state) renameRelation(r renameObject) {
	if s.renameColumnOf(r) {
		return
	}
	if t, _ := s.lookup(r.q); t != nil {
		s.refuse(t, fmt.Sprintf("ALTER %s %s names this table (Postgres refuses)", strings.ToUpper(r.kind), r.q))
		return
	}
	if r.kind == "type" || r.kind == "domain" {
		// V4 kinds (2026-11-07): a type statement on a name held as
		// another kind.
		if kind, certain, found := s.relationAt(r.q); found && s.typeKindRefusal(r, kind, certain) {
			return
		}
	}
	// Revision 3: ALTER VIEW/SEQUENCE/MATERIALIZED VIEW/TYPE on an index or
	// on a relation of another kind is refused (tablecmds.c 17907-17930).
	if t, ix := s.findIndex(r.q); ix != nil {
		if r.q.schema != nil || !s.pathChanged {
			s.refuse(t, fmt.Sprintf("ALTER %s %s names an index of this table (Postgres refuses)", strings.ToUpper(r.kind), r.q))
		} else {
			s.partial(t, fmt.Sprintf("ALTER %s %s may name an index of this table", strings.ToUpper(r.kind), r.q))
		}
		return
	}
	if rel := s.relations[r.q.String()]; rel != nil && relationKinds[r.kind] && relationKinds[rel.kind] {
		kind, certain, _ := s.relationAt(r.q)
		switch {
		case certain && rel.kind != r.kind:
			s.mayFail(fmt.Sprintf("ALTER %s %s names %s (Postgres refuses)", strings.ToUpper(r.kind), r.q, article(kind)))
			return
		case !certain:
			// P1/P4 (g1-s20371, g1-s31160): the relation the set created
			// may since have been dropped or replaced (an opaque
			// statement, a CASCADE), so its existence and kind are not
			// proven.
			s.unknownFact(fmt.Sprintf("ALTER %s %s may be refused: %s the set created may no longer exist or may be of another kind%s", strings.ToUpper(r.kind), r.q, article(rel.kind), precondition("R4.P1")))
		}
	}
	s.moveRelation(r.q, r.newName)
}

// moveRelation renames a recorded relation; newName "" makes it uncertain.
func (s *state) moveRelation(q qname, newName string) {
	key := q.String()
	rel := s.relations[key]
	if rel == nil {
		for k, r := range s.relations {
			if k == q.name || strings.HasSuffix(k, "."+q.name) {
				r.uncertain = true
			}
		}
		return
	}
	if newName == "" {
		rel.uncertain = true
		return
	}
	nq := qname{schema: q.schema, name: newName}
	if t, _ := s.lookup(nq); t != nil {
		s.refuse(t, fmt.Sprintf("%s is renamed to the name of this table (Postgres refuses)", article(rel.kind)))
		return
	}
	delete(s.relations, key)
	if q.schema == nil && s.pathChanged || s.failure != nil {
		rel.uncertain = true
	}
	s.relations[nq.String()] = rel
	s.renameUnsafe(q.name, newName)
}

func (s *state) createTable(ct *createTable) {
	what := "CREATE TABLE " + ct.name.String()
	for _, def := range ct.columns {
		s.needsColumn(def, what, "M1.P1")
		if systemColumns[def.name] {
			s.mayFail(fmt.Sprintf("%s: column %s has a system column name (Postgres refuses)%s", what, def.name, precondition("M1.P2")))
		}
		if why := typePosition(def.typ, "column"); why != "" {
			// V13 (Revision 1.6): a pseudo-type column.
			s.mayFail(fmt.Sprintf("%s: %s%s", what, why, precondition("M2.P4")))
		} else if why := s.columnTypeProven(def.typ) + s.collationProven(def.collation); why != "" {
			s.unknownFact(fmt.Sprintf("%s: column %s may be refused: %s%s", what, def.name, why, precondition("M2.P1")))
		}
	}
	for _, ch := range ct.constraints.checks {
		s.needsNames(ch.names, what, "M1.P1")
	}
	if ct.partitionOf != nil {
		s.needs(*ct.partitionOf, kindRelation, true, what, "M1b.P1")
	}
	if ct.ofType != nil {
		s.needs(*ct.ofType, kindType, false, what, "M1b.P1")
	}
	s.needsSchema(ct.name, what, "M1.P1")
	exact, others := s.lookup(ct.name)
	if exact != nil {
		certain := s.unqualified(exact, ct.name)
		if ct.ifNotExists {
			return
		}
		if certain {
			s.refuse(exact, "created again while a table of this name exists (Postgres refuses)")
		} else {
			s.partial(exact, "created again while a table of this name may exist")
			s.unknownFact(fmt.Sprintf("CREATE TABLE %s may be refused: a table of this name may exist", ct.name))
		}
		return
	}
	kind, certain, found := s.relationAt(ct.name)
	if found && certain {
		if ct.ifNotExists && relationKinds[kind] {
			s.set.diagnostic(s.m, s.at, "info", "schema_relation_exists", fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s is skipped: %s of this name exists", ct.name, article(kind)))
			return
		}
		s.mayFail(fmt.Sprintf("CREATE TABLE %s: %s of this name exists (Postgres refuses)", ct.name, article(kind)))
		return
	}
	// Revision 8: Postgres refuses a name that is taken, so a name that may
	// be taken makes the migration one that may fail.
	_, maybe := s.nameTaken(ct.name, nil)
	held := maybe
	if maybe == "" {
		maybe = s.generatedRelationName(ct.name)
		held = s.generatedMayHold(ct.name)
	}
	t := &table{q: ct.name, file: s.m.file, span: s.at, creator: s.m, unlogged: ct.unlogged, empty: true}
	// SB-M3: a new table has row level security disabled and not forced.
	off, notForced := false, false
	t.rls = rlsState{enabled: &off, forced: &notForced}
	s.tables = append(s.tables, t)
	s.failureReason(t)
	if maybe != "" && !ct.ifNotExists {
		s.unknownFact(fmt.Sprintf("CREATE TABLE %s may be refused: %s", ct.name, maybe))
	}
	for _, o := range others {
		s.partial(o, fmt.Sprintf("%s may name this table under another qualification (search_path)", ct.name))
	}
	if len(others) > 0 {
		s.partial(t, "a table of the same name exists under another qualification (search_path)")
	}
	s.unqualified(t, ct.name)
	if found {
		s.partial(t, fmt.Sprintf("%s of this name may exist; the table may not have been created", article(kind)))
		t.mayBeOther = true
	}
	if ct.ifNotExists && s.opaqueSeen > 0 {
		s.partial(t, "IF NOT EXISTS after an opaque statement: the table may already exist with other columns")
	}
	if ct.ifNotExists && (s.opaqueSeen > 0 || found || held != "" || len(others) > 0 || ct.name.schema == nil && (s.pathChanged || s.temps[ct.name.name])) {
		// P3: whether Postgres skipped the statement is unknown, so its
		// columns and constraints are not facts. When it is not skipped it
		// must still succeed, so the statement is applied as written (its
		// refusal conditions are checked) and its facts are removed after.
		s.partial(t, "CREATE TABLE IF NOT EXISTS may have been skipped over an existing relation; the columns and constraints it writes are unknown"+precondition("M1.P3"))
		// SB-M3: an existing relation's row level security is unknown.
		t.rls = rlsState{}
		t.kindUnknown = found || held != ""
		t.mayBeOther = true
		defer forgetCreated(t)
		if found && kind != "table" || s.generatedMayHold(ct.name) != "" {
			// P3 (T018a2 B1b, g2-s1123): the name may be held by a relation
			// of another kind (a generated index name, a recorded relation),
			// so whether a table exists is itself unknown: no table is
			// recorded, only an uncertain relation of this name.
			defer s.uncertainTable(t)
		}
	}
	if listKeyRefused(ct) && ct.partitionOf == nil {
		// R3 (Revision 1.4, P4): LIST takes one key element, whatever the
		// columns are (tablecmds.c transformPartitionSpec, 42P17).
		s.refuse(t, "cannot use \"list\" partition strategy with more than one column (Postgres refuses, 42P17)"+precondition("M1.P4"))
	}
	if ct.unreadable != "" {
		// Revision 3 G4: the header names the table; its body is unknown.
		s.partial(t, fmt.Sprintf("the CREATE TABLE statement could not be read (%s); its columns and constraints are unknown", ct.unreadable))
		return
	}
	if ct.unmodeled != "" {
		// Revision 4 G-d: LIKE, OF, PARTITION OF and INHERITS take columns
		// from elsewhere; the table exists with columns that are unknown.
		s.partial(t, ct.unmodeled+"; its columns and constraints are unknown")
		t.ofType = ct.ofType
		if ct.likeOf != nil {
			if src, _ := s.lookup(*ct.likeOf); src != nil {
				t.likeOf = src
			}
		}
		if ct.partitionOf != nil {
			if p, _ := s.lookup(*ct.partitionOf); p != nil {
				t.partitionOf = p
			}
			s.partitionChild(t, ct)
		}
		return
	}
	t.partitionKey = ct.partitionKey
	t.partitionProven = (ct.partitionBy == "range" || ct.partitionBy == "list") && ct.partitionPlain
	switch {
	case ct.onCommit:
		s.refuse(t, "ON COMMIT is only allowed on temporary tables (Postgres refuses)"+precondition("M1.P4"))
	case ct.optionsUnproven != "":
		s.unproven(t, fmt.Sprintf("CREATE TABLE %s may be refused: %s are not proven%s", ct.name, ct.optionsUnproven, precondition("M1.P4")))
	}
	for _, def := range ct.columns {
		if t.column(def.name) != nil {
			s.refuse(t, fmt.Sprintf("column %s is defined twice (Postgres refuses)", def.name))
			continue
		}
		def.typ = s.columnDefinitionChecks(t, def, "M1")
		s.addColumn(t, def)
	}
	s.partitionedChecks(t, ct)
	// Postgres adds the keys of the whole statement before its foreign keys
	// (transformCreateStmt), so a column's REFERENCES may use a key defined
	// by a later element.
	tc := ct.constraints
	for _, def := range ct.columns {
		tc = mergeConstraints(columnConstraintSet(def), tc)
	}
	s.constraintIndexNames(t, tc, "M1.P2")
	s.tableConstraints(t, tc, true, false, "M1")
	for _, def := range ct.columns {
		if c := t.column(def.name); c != nil {
			s.columnExpressions(t, c, what, "M1.P5")
		}
	}
}

// uncertainTable replaces a table whose existence is unknown by an
// uncertain relation of its name (P3).
func (s *state) uncertainTable(t *table) {
	s.tables = slices.DeleteFunc(s.tables, func(x *table) bool { return x == t })
	key := t.q.String()
	if r := s.relations[key]; r != nil {
		r.uncertain = true
		return
	}
	s.relations[key] = &relation{kind: "table", m: s.m, opaqueAt: s.opaqueSeen, uncertain: true}
}

// forgetCreated removes the column and key facts a CREATE TABLE IF NOT
// EXISTS wrote when Postgres may have skipped it (P3). Its foreign keys
// stay in the model as possible dependents of the tables they reference
// (they are not emitted: their columns are gone); a primary key may exist.
func forgetCreated(t *table) {
	t.columns, t.primaryKey, t.uniques, t.checks = nil, nil, nil, nil
	t.keysUnknown, t.pkMaybe, t.hasChecks, t.unnamedChecks, t.checksUnknown = true, true, true, true, true
}

// constraintIndexNames applies P2 to the written names of a statement's
// primary key and UNIQUE constraints: each creates an index of that name
// in the table's schema, so the name must be free in pg_class (tables,
// indexes including other constraints' indexes and generated names,
// sequences, views). A name held is a refusal, one that may be held a
// possible failure.
func (s *state) constraintIndexNames(t *table, tc tableConstraints, id string) {
	for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
		if k.name == nil {
			continue
		}
		q := qname{schema: t.q.schema, name: *k.name}
		refused, maybe := s.nameTaken(q, nil)
		if refused == "" && maybe == "" {
			if owner, _ := s.findKey(q); owner != nil {
				if owner.keysUnknown || !factsKnown(owner) {
					maybe = fmt.Sprintf("constraint index %s of table %s may exist", *k.name, owner.q)
				} else {
					refused = fmt.Sprintf("constraint index %s of table %s exists", *k.name, owner.q)
				}
			}
		}
		switch {
		case refused != "":
			s.mayFail(fmt.Sprintf("CONSTRAINT %s needs a free relation name: %s (Postgres refuses)%s", *k.name, refused, precondition(id)))
		case maybe != "":
			s.unknownFact(fmt.Sprintf("CONSTRAINT %s may be refused: %s; Postgres refuses a taken relation name%s", *k.name, maybe, precondition(id)))
		}
	}
}

// mergeConstraints appends b's constraints to a's.
func mergeConstraints(a, b tableConstraints) tableConstraints {
	return tableConstraints{
		primaryKeys: append(slices.Clone(a.primaryKeys), b.primaryKeys...),
		uniques:     append(slices.Clone(a.uniques), b.uniques...),
		foreignKeys: append(slices.Clone(a.foreignKeys), b.foreignKeys...),
		checks:      append(slices.Clone(a.checks), b.checks...),
	}
}

func (s *state) addColumn(t *table, def columnDef) *column {
	c := &column{name: def.name, file: s.m.file, span: def.span, typ: def.typ, typeNames: def.typeNames, typeResolved: def.serial, nullable: !def.notNull,
		def: def.def, implied: def.serial != "", collation: def.collation, identity: def.identity, generated: def.generated,
		exprNames: def.exprNames, generatedNames: def.generatedNames}
	// Y46 (Revision 1.9): a NULL constant default is not stored.
	c.def = s.storedDefault(c.def, c.typ)
	if c.implied || c.identity != nil {
		c.seq = t.q.name + "_" + c.name + "_seq"
		// Y40 (Revision 1.9): the names ChooseRelationName starts from.
		c.seqParts = [2]string{t.q.name, c.name}
	}
	t.columns = append(t.columns, c)
	return c
}

// columnConstraintSet is the inline constraints of a column definition.
func columnConstraintSet(def columnDef) tableConstraints {
	var tc tableConstraints
	if def.primaryKey != nil {
		tc.primaryKeys = append(tc.primaryKeys, *def.primaryKey)
	}
	tc.uniques = def.unique
	tc.foreignKeys = def.references
	tc.checks = def.checks
	return tc
}

func (s *state) columns(t *table, names []string, certain bool) ([]*column, bool) {
	var out []*column
	for _, n := range names {
		c := t.column(n)
		if c == nil {
			s.missing(t, certain, fmt.Sprintf("constraint names column %s, which is not known", n))
			return nil, false
		}
		out = append(out, c)
	}
	return out, true
}

// tableConstraints adds the constraints of one CREATE TABLE or ALTER TABLE
// subcommand to t. altering is set for ALTER TABLE, where t may already
// hold a primary key.
func (s *state) tableConstraints(t *table, tc tableConstraints, certain, altering bool, row string) {
	if altering {
		s.constraintNames(t, tc)
		s.constraintIndexNames(t, tc, "M5f.P2")
		// U4: a key on a partitioned table includes the partition key.
		s.constraintKeys(t, tc, certain, row)
	}
	tc = s.mergeIndexConstraints(t, tc)
	// Revision 1.4 R2: one namespace of constraint names per table.
	s.constraintNamespace(t, tc, altering, row)
	if altering {
		// R5: a validated constraint checks the rows the table holds.
		for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
			s.rowsChecked(t, fmt.Sprintf("a key over (%s)", strings.Join(k.columns, ", ")), rowIDs[row].p11)
		}
		for _, ch := range tc.checks {
			if !ch.notValid {
				s.rowsChecked(t, "a CHECK or EXCLUDE constraint", rowIDs[row].p11)
			}
		}
		for _, f := range tc.foreignKeys {
			if !f.notValid {
				s.rowsChecked(t, fmt.Sprintf("a foreign key over (%s)", strings.Join(f.local, ", ")), rowIDs[row].p11)
			}
		}
	}
	for _, ch := range tc.checks {
		s.checkProven(t, ch, row)
	}
	for _, pk := range tc.primaryKeys {
		if !s.keyColumnChecks(t, pk, row) {
			continue
		}
		cols, ok := s.columns(t, pk.columns, certain)
		if !ok {
			continue
		}
		s.keyOpclasses(t, cols, row)
		if t.primaryKey != nil {
			s.refuse(t, "a second primary key (Postgres refuses)")
			continue
		}
		if t.pkMaybe {
			// Revision 4 WF3: the earlier key may still exist.
			s.partial(t, "a primary key may still exist (an earlier DROP CONSTRAINT may not have dropped it); Postgres refuses a second one")
			s.mayFail("ADD PRIMARY KEY may be refused: a primary key may still exist")
			t.pkMaybe = false
		} else if altering && !factsKnown(t) {
			// Revision 8: on a partial table a primary key the pass does not
			// know may exist.
			s.unproven(t, "ADD PRIMARY KEY may be refused: the table is partial, so whether it already has a primary key is unknown")
		}
		t.primaryKey = &key{name: pk.name, cols: cols, deferrable: pk.deferrable}
		if pk.name == nil {
			t.primaryKey.gen = t.q.name + "_pkey"
		}
		for _, c := range cols {
			c.nullable = false
		}
	}
	for _, u := range tc.uniques {
		if !s.keyColumnChecks(t, u, row) {
			continue
		}
		cols, ok := s.columns(t, u.columns, certain)
		if ok {
			s.keyOpclasses(t, cols, row)
			k := &key{name: u.name, cols: cols, deferrable: u.deferrable}
			if u.name == nil {
				k.gen = t.q.name + "_" + strings.Join(names(cols), "_") + "_key"
			}
			t.uniques = append(t.uniques, k)
		}
	}
	for _, ch := range tc.checks {
		t.hasChecks = true
		if ch.name == nil {
			t.unnamedChecks = true
			t.unnamedCheckNames = append(t.unnamedCheckNames, ch.names)
		}
		if ch.name != nil {
			t.checks = append(t.checks, check{name: *ch.name, names: ch.names, exclude: ch.exclude})
		}
	}
	for _, ref := range tc.foreignKeys {
		if s.deferring {
			s.pending = append(s.pending, pendingKey{t: t, ref: ref, at: s.at, certain: certain})
			continue
		}
		s.addForeignKey(t, ref, certain)
	}
}

// constraintNames applies Revision 8 to the written names of ALTER TABLE
// constraints: a name that may be the generated name of an unnamed
// constraint of t, or (for a key, which has an index) of an unnamed index
// or sequence, may be refused. CHECK and EXCLUDE are not told apart here,
// so both are checked like EXCLUDE (with an index).
func (s *state) constraintNames(t *table, tc tableConstraints) {
	check := func(name *string, indexed bool) {
		if name == nil {
			return
		}
		if why := s.generatedConstraintName(t, *name, indexed); why != "" {
			s.unproven(t, fmt.Sprintf("ADD CONSTRAINT %s may be refused: %s", *name, why))
		}
	}
	for _, k := range append(append(slices.Clone(tc.primaryKeys), tc.uniques...), tc.checks...) {
		check(k.name, true)
	}
	for _, ref := range tc.foreignKeys {
		check(ref.name, false)
	}
}

// mergeIndexConstraints applies Postgres's merging of equal index
// constraints within one transformation (Revision 7; transformIndexConstraints,
// parse_utilcmd.c 2084-2140, REL_17_6): one CREATE TABLE, or one ALTER
// TABLE subcommand, since ALTER TABLE transforms each subcommand alone
// (tablecmds.c ATParseTransformCmd 5567-5592). The primary key is kept
// first; then, in source order, a UNIQUE constraint equal to a kept one in
// ordered columns, INCLUDE columns, NULLS NOT DISTINCT, DEFERRABLE and
// INITIALLY DEFERRED is dropped, and its written name goes to the kept one
// when that one is unnamed. WITH and USING INDEX TABLESPACE are not
// compared; the access method is btree and there is no predicate for
// both kinds. EXCLUDE constraints are not keys here and never equal a
// primary key or UNIQUE constraint (their operators differ). When an
// INCLUDE list cannot be read the pass cannot tell whether the two merge:
// both are kept, the table is partial and unique_keys is unknown.
func (s *state) mergeIndexConstraints(t *table, tc tableConstraints) tableConstraints {
	if len(tc.uniques) == 0 {
		return tc
	}
	pks := slices.Clone(tc.primaryKeys)
	uniques := slices.Clone(tc.uniques)
	order := make([]int, len(uniques))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return uniques[a].span.start - uniques[b].span.start })
	var kept []*constraintDef
	if len(pks) > 0 {
		kept = append(kept, &pks[0])
	}
	dropped := make([]bool, len(uniques))
	for _, i := range order {
		u := &uniques[i]
		for _, k := range kept {
			equal, known := sameIndexConstraint(*k, *u)
			if !known {
				s.partialOnce(t, fmt.Sprintf("the pass cannot tell whether Postgres merges the equal key constraints over (%s): an INCLUDE list could not be read", strings.Join(u.columns, ", ")))
				t.keysUnknown = true
				continue
			}
			if equal {
				if k.name == nil {
					k.name = u.name
				}
				dropped[i] = true
				break
			}
		}
		if !dropped[i] {
			kept = append(kept, u)
		}
	}
	tc.primaryKeys = pks
	tc.uniques = nil
	for i, u := range uniques {
		if !dropped[i] {
			tc.uniques = append(tc.uniques, u)
		}
	}
	return tc
}

// sameIndexConstraint compares the attributes Postgres compares when it
// merges index constraints; known is false when an INCLUDE list could not
// be read and the columns are equal.
func sameIndexConstraint(a, b constraintDef) (equal, known bool) {
	if !slices.Equal(a.columns, b.columns) {
		return false, true
	}
	if a.includeUnknown || b.includeUnknown {
		return false, false
	}
	return slices.Equal(a.include, b.include) && a.nullsNotDistinct == b.nullsNotDistinct &&
		a.deferrable == b.deferrable && a.initDeferred == b.initDeferred, true
}

// sameColumns reports whether a and b hold the same columns in any order.
func sameColumns(a, b []*column) bool {
	if len(a) != len(b) {
		return false
	}
	for _, c := range a {
		if !slices.Contains(b, c) {
			return false
		}
	}
	return true
}

// referencedKey reports whether cols are, as a set, the columns of a
// non-deferrable primary key, UNIQUE constraint or unique index without
// predicate or expression of t: the only targets Postgres accepts for a
// foreign key (tablecmds.c transformFkeyCheckAttrs).
func referencedKey(t *table, cols []*column) bool {
	return keyCandidates(t, cols) > 0
}

// keyCandidates counts the non-deferrable primary key, UNIQUE constraints
// and certain unique indexes without predicate or expression of t over
// exactly cols: the keys a foreign key to cols may use. A foreign key
// depends on the index of the one it uses (pg_constraint.c 312-324).
func keyCandidates(t *table, cols []*column) int {
	n := 0
	if t.primaryKey != nil && !t.primaryKey.deferrable && sameColumns(t.primaryKey.cols, cols) {
		n++
	}
	for _, u := range t.uniques {
		if !u.deferrable && sameColumns(u.cols, cols) {
			n++
		}
	}
	for _, ix := range t.indexes {
		if backsKey(ix) && sameColumns(ix.columns, cols) {
			n++
		}
	}
	return n
}

// backsKey reports whether ix can be the unique index of a foreign key.
func backsKey(ix *index) bool {
	return ix.unique && !ix.uncertain && ix.where == nil && !slices.Contains(ix.columns, nil)
}

func (s *state) addForeignKey(t *table, ref referenceDef, certain bool) {
	local, ok := s.columns(t, ref.local, certain)
	if !ok {
		return
	}
	s.foreignKeyOptions(t, ref, local)
	fk := &foreignKey{name: ref.name, local: local, written: ref, onDelete: ref.onDelete, onUpdate: ref.onUpdate, file: s.m.file, span: ref.span}
	t.foreignKeys = append(t.foreignKeys, fk)
	exact, others := s.lookup(ref.table)
	switch {
	case exact == nil && !s.pathChanged && s.platformReference(fk, ref):
		// SB-M2: a key of a Supabase platform table (SB-V2). With the
		// search_path unchanged an unqualified table of the set is in the
		// default schema, never a platform schema.
	case len(others) > 0:
		for _, o := range append(others, exact) {
			if o != nil {
				s.partial(o, fmt.Sprintf("reference %s may name this table under another qualification (search_path)", ref.table))
			}
		}
		fk.why = "the referenced table is ambiguous under search_path"
		s.unproven(t, fmt.Sprintf("foreign key to %s may be refused: the name may mean another table, whose keys are unknown", ref.table))
	case exact == nil:
		// A table the set never creates is outside the model (Revision 8
		// leaves unknown objects as they were); one the set creates but does
		// not hold here is missing (P1); X3: one held as another relation
		// kind is not a table.
		fk.why = "the referenced table is not known"
		if s.notATable(ref.table, fmt.Sprintf("foreign key to %s", ref.table), "M3.P1", false) {
			break
		}
		s.needs(ref.table, kindRelation, true, fmt.Sprintf("foreign key to %s", ref.table), "M3.P1")
	case ref.table.schema == nil && (s.pathChanged || s.temps[ref.table.name]):
		fk.why = "search_path may have changed earlier in the set"
		s.unproven(t, fmt.Sprintf("foreign key to %s may be refused: the name may mean another table, whose keys are unknown", ref.table))
	case exact.mayBeOther:
		// X3 (Revision 1.8): the name may hold another relation kind.
		fk.why = "the referenced relation may not be a table"
		s.mayBeOtherTarget(exact, fmt.Sprintf("foreign key to %s", ref.table), "M3.P1")
	default:
		// Revision 3 class 3: a foreign key needs a primary key, UNIQUE
		// constraint or unique index over exactly the referenced columns;
		// on a complete referenced table its absence is a refusal, on a
		// partial one the reference stays unresolved (never linked).
		var cols []*column
		switch {
		case ref.columns == nil && (exact.primaryKey == nil || exact.primaryKey.deferrable):
			fk.why = "the referenced table has no known non-deferrable primary key"
		case ref.columns == nil:
			cols = exact.primaryKey.cols
		default:
			for _, n := range ref.columns {
				c := exact.column(n)
				if c == nil {
					fk.why = fmt.Sprintf("column %s of %s is not known", n, exact.q)
					cols = nil
					break
				}
				cols = append(cols, c)
			}
			if fk.why == "" && !referencedKey(exact, cols) {
				fk.why = fmt.Sprintf("no known primary key, unique constraint or unique index of %s has exactly the columns (%s)", exact.q, strings.Join(ref.columns, ", "))
			}
		}
		if fk.why == "" && len(cols) != len(local) {
			fk.why = "the referenced key has a different number of columns"
		}
		mismatch := ""
		if fk.why == "" {
			for i, c := range local {
				if !fkTypePair(c, writtenType(c), cols[i], writtenType(cols[i])) {
					mismatch = fmt.Sprintf("%s (%s) and %s.%s (%s)", c.name, c.typ, exact.q, cols[i].name, cols[i].typ)
					break
				}
			}
		}
		switch {
		case fk.why == "" && mismatch == "" && !t.unlogged && exact.unlogged:
			// P4: a permanent table may reference only permanent tables
			// (tablecmds.c 9681-9688, 42P16); an unlogged one may reference
			// either.
			fk.why = "a permanent table cannot reference an unlogged table"
			if certain {
				s.refuse(t, fmt.Sprintf("foreign key to %s is refused: %s is unlogged and %s is permanent%s", ref.table, exact.q, t.q, precondition("M3.P4")))
			} else {
				s.unproven(t, fmt.Sprintf("foreign key to %s may be refused: %s is unlogged%s", ref.table, exact.q, precondition("M3.P4")))
			}
		case mismatch != "":
			// Revision 8: whether Postgres finds an equality operator for
			// two different types is not decided (Revision 9: aliases of
			// one type are the same type, canonicalType), as
			// for ALTER COLUMN TYPE (retypeForeignKeys).
			fk.why = "whether the column types are compatible is unknown: " + mismatch
			s.unproven(t, fmt.Sprintf("foreign key to %s may be refused: the column types differ as written, %s", ref.table, mismatch))
		case fk.why == "":
			fk.table, fk.columns = exact, cols
		case complete(exact):
			s.refuse(t, fmt.Sprintf("foreign key to %s: %s (Postgres refuses)", ref.table, fk.why))
		default:
			// Revision 8: on a partial referenced table a matching key may
			// exist or not; Postgres refuses the statement without one.
			s.unproven(t, fmt.Sprintf("foreign key to %s may be refused: %s, and %s is partial, so whether it has a matching key is unknown", ref.table, fk.why, exact.q))
		}
	}
	if fk.why != "" {
		s.unresolved(fmt.Sprintf("reference from %s to %s is unresolved: %s", t.q, ref.table, fk.why))
	}
}

func (s *state) createIndex(ci *createIndex) {
	t, certain := s.target(ci.table)
	if t == nil && s.notATable(ci.table, "CREATE INDEX ON "+ci.table.String(), "M4.P1", true) {
		return
	}
	if s.mayBeOtherTarget(t, "CREATE INDEX ON "+ci.table.String(), "M4.P1") {
		return
	}
	if t == nil {
		if _, others := s.lookup(ci.table); len(others) > 0 {
			// Revision 8: a known table may be meant, or none.
			s.unknownFact(fmt.Sprintf("CREATE INDEX ON %s may be refused: the name may mean no table", ci.table))
		}
		s.needs(ci.table, kindRelation, true, "CREATE INDEX ON "+ci.table.String(), "M4.P1")
		s.outsideChange(ci.table, "CREATE INDEX ON "+ci.table.String(), "M4.P2")
		if ci.name != nil {
			// P2 (g1-s16467): the index takes its name in the table's
			// schema whether or not the pass holds the table.
			s.createRelation(namedObject{q: qname{schema: ci.table.schema, name: *ci.name}, kind: "index", ifNotExists: ci.ifNotExists, uncertain: ci.table.schema == nil && s.pathChanged})
		}
		if ci.concurrently && !s.inBlock {
			// C4 (P4): a table the pass does not hold may be partitioned.
			s.unknownFact(fmt.Sprintf("CREATE INDEX CONCURRENTLY ON %s may be refused: the pass does not hold the table, so whether it is partitioned is not proven%s", ci.table, precondition("M4.P6")))
		}
		s.unknown(ci.table, false, "CREATE INDEX")
		return
	}
	for _, el := range ci.elements {
		if el.column == nil {
			s.needsNames(el.names, "CREATE INDEX "+ci.table.String(), "M4.P1")
		}
	}
	s.needsNames(ci.whereNames, "CREATE INDEX "+ci.table.String(), "M4.P1")
	s.touch(t)
	if s.uncertainName(certain) {
		// Revision 8: the name may mean another table, or none.
		s.unproven(t, fmt.Sprintf("CREATE INDEX ON %s may be refused: the name may mean another table or none, whose columns are unknown", ci.table))
	}
	skipped := false
	if ci.name != nil && ci.ifNotExists {
		// P1 columns (g1-s37115): Postgres resolves the index columns
		// (DefineIndex, ComputeIndexAttrs) before IF NOT EXISTS looks at
		// the name, so a missing column is refused even when the name
		// exists.
		for _, el := range ci.elements {
			if el.column != nil && t.column(*el.column) == nil {
				s.missing(t, certain, fmt.Sprintf("index names column %s, which is not known", *el.column))
				return
			}
		}
	}
	if ci.name != nil {
		// Index names share the relation namespace of the table's schema.
		q := qname{schema: t.q.schema, name: *ci.name}
		if owner, existing := s.findIndex(q); existing != nil {
			if !ci.ifNotExists {
				s.refuse(owner, fmt.Sprintf("index %s is created twice (Postgres refuses)", *ci.name))
			}
			return
		}
		if other, _ := s.lookup(q); other != nil {
			if !ci.ifNotExists {
				s.refuse(t, fmt.Sprintf("index %s has the name of table %s (Postgres refuses)", *ci.name, other.q))
			}
			return
		}
		kind, certainKind, found := s.relationAt(q)
		if found && certainKind {
			if !ci.ifNotExists {
				s.refuse(t, fmt.Sprintf("index %s has the name of %s (Postgres refuses)", *ci.name, article(kind)))
			}
			return
		}
		_, taken := s.nameTaken(q, nil)
		maybe := taken
		if maybe == "" {
			maybe = s.generatedRelationName(q)
		}
		if maybe != "" && !ci.ifNotExists {
			s.unproven(t, fmt.Sprintf("CREATE INDEX %s may be refused: %s", *ci.name, maybe))
		}
		held := taken
		if held == "" {
			held = s.generatedMayHold(q)
		}
		if held == "" && found {
			held = fmt.Sprintf("%s named %s may exist", article(kind), q.name)
		}
		if ci.ifNotExists && (held != "" || s.opaqueSeen > 0) {
			// P3: Postgres may have skipped the statement; the index is not
			// a fact. Its columns are still checked (when not skipped the
			// statement must succeed).
			s.partial(t, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s may have been skipped over an existing relation; the index it writes is unknown%s", *ci.name, precondition("M4.P3")))
			// A non-unique index carries no key or column fact.
			t.reasons[len(t.reasons)-1].indexOnly = !ci.unique
			skipped = true
		}
	}
	if ci.options != nil {
		// U9: the index's storage parameters for its access method.
		if why := storageCheck(indexReloptKind(ci.using), *ci.options, false, s.m.source); why != "" {
			s.unproven(t, fmt.Sprintf("CREATE INDEX on %s may be refused: WITH (...): %s%s", t.q, why, precondition("M4.P4")))
		}
	}
	ix := &index{name: ci.name, unique: ci.unique, using: ci.using, elements: ci.elements, include: ci.include, where: ci.where, whereNames: ci.whereNames, file: s.m.file, span: s.at, mayBeGone: skipped, uncertain: skipped, nullsNotDistinct: ci.nullsNotDistinct, concurrently: ci.concurrently, options: ci.options}
	for _, el := range ci.elements {
		var c *column
		if el.column != nil {
			if c = t.column(*el.column); c == nil {
				s.missing(t, certain, fmt.Sprintf("index names column %s, which is not known", *el.column))
				return
			}
		}
		ix.columns = append(ix.columns, c)
	}
	for _, n := range ci.include {
		if t.column(n) == nil {
			s.missing(t, certain, fmt.Sprintf("index includes column %s, which is not known", n))
			return
		}
	}
	if ix.name == nil {
		ix.gen = indexGenBase(t, ix, ci.elements)
	}
	s.indexExpressions(t, ci)
	expression := ci.where != nil
	for _, el := range ci.elements {
		expression = expression || el.column == nil
	}
	if ci.unique {
		// U4: a unique index on a partitioned table includes the
		// partition key.
		s.partitionedUnique(t, indexKey(ci), certain && s.failure == nil, "M4.P4")
	}
	if ci.unique || expression {
		// R5: building the index checks or evaluates every row.
		s.rowsChecked(t, "a unique or expression index", "M4.P11")
	}
	t.indexes = append(t.indexes, ix)
	if ci.concurrently {
		s.concurrently(t, "CREATE INDEX CONCURRENTLY", "M4.P6")
	}
}

// concurrently applies P6 and C4 to CREATE or DROP INDEX CONCURRENTLY on
// the set table t: refused inside any block (PreventInTransactionBlock),
// and outside one refused on a partitioned table (indexcmds.c
// DefineIndex, REL_17_6: "cannot create index on partitioned table
// concurrently"; RemoveRelations: "cannot drop partitioned index
// concurrently"; 0A000) and not proven when whether t is partitioned is
// unknown (P4: a CREATE TABLE IF NOT EXISTS skipped over a relation the
// pass does not hold).
func (s *state) concurrently(t *table, what, id string) {
	switch {
	case s.inBlock:
		s.refuse(t, what+" cannot run inside the migration's transaction")
	case t.partitionKey != nil:
		s.refuse(t, fmt.Sprintf("%s on partitioned table %s (Postgres 17 refuses, 0A000)%s", what, t.q, precondition(id)))
	case t.kindUnknown || t.mayBeOther:
		s.unproven(t, fmt.Sprintf("%s on %s may be refused: whether the table is partitioned is not proven%s", what, t.q, precondition(id)))
	}
}

// findIndex finds a written index name among tables of the schema.
func (s *state) findIndex(q qname) (*table, *index) {
	for _, t := range s.tables {
		if !sameSchema(t.q.schema, q.schema) {
			continue
		}
		for _, ix := range t.indexes {
			if ix.name != nil && *ix.name == q.name {
				return t, ix
			}
		}
	}
	return nil, nil
}

// findKey finds a primary key or UNIQUE constraint whose written name is
// this name: its index has the constraint's name.
func (s *state) findKey(q qname) (*table, *key) {
	for _, t := range s.tables {
		if !sameSchema(t.q.schema, q.schema) {
			continue
		}
		for _, k := range append([]*key{t.primaryKey}, t.uniques...) {
			if k != nil && k.name != nil && *k.name == q.name {
				return t, k
			}
		}
	}
	return nil, nil
}

// indexLike reports whether a name may be an index the pass knows: a
// written index or constraint name, under this or another qualification.
func (s *state) indexLike(q qname) bool {
	if _, ix := s.findIndex(q); ix != nil {
		return true
	}
	if _, k := s.findKey(q); k != nil {
		return true
	}
	return len(s.indexesNamed(q)) > 0
}

// indexesNamed finds the tables with an index of this name under another
// qualification (WF5): public.i may be the index i of an unqualified table.
// The index of a named primary key or UNIQUE constraint counts too (C3: an
// ALTER INDEX public.u RENAME of the key u's index renames the key).
func (s *state) indexesNamed(q qname) []*table {
	var out []*table
	for _, t := range s.tables {
		if sameSchema(t.q.schema, q.schema) {
			continue
		}
		if keyNamed(t, q.name) {
			out = append(out, t)
			continue
		}
		for _, ix := range t.indexes {
			if ix.name != nil && *ix.name == q.name {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// keyNamed reports whether t has a primary key or UNIQUE constraint whose
// written name (the name of its index) is name.
func keyNamed(t *table, name string) bool {
	for _, k := range append([]*key{t.primaryKey}, t.uniques...) {
		if k != nil && k.name != nil && *k.name == name {
			return true
		}
	}
	return false
}

// generatedIndexMayMatch handles an index name that was never written: it
// may be the generated name of an unnamed index (P5).
func (s *state) generatedIndexMayMatch(q qname, what string) []*table {
	var hit []*table
	for _, t := range s.tables {
		named := sameSchema(t.q.schema, q.schema) && slices.Contains(generatedNames(t), q.name)
		for _, ix := range t.indexes {
			if ix.name == nil {
				named = true
				break
			}
		}
		if named {
			s.partial(t, fmt.Sprintf("%s %s may name an unnamed index of this table (generated name)", what, q))
			hit = append(hit, t)
		}
	}
	return hit
}

// alterIndex applies ALTER INDEX. RENAME TO renames a written index, and,
// because ALTER INDEX ... RENAME accepts any relation (tablecmds.c
// 17901-17906 and 17931-17937), a table of that name (WF5). Other forms
// make the index's table partial; naming a table with them is refused.
func (s *state) alterIndex(ai *alterIndex) {
	what := "ALTER INDEX " + ai.name.String()
	pathOK := ai.name.schema != nil || !s.pathChanged
	if t, ix := s.findIndex(ai.name); ix != nil {
		s.touch(t)
		if ai.newName == nil {
			s.partial(t, "ALTER INDEX is not modeled")
			if ai.tableForm != "" {
				// V4 kinds (2026-11-07): a table form on an index.
				s.kindRefusal(what+" "+ai.tableForm, ai.name, "index", pathOK && s.failure == nil && !ix.mayBeGone, attAll&^attIndex, "M8.P1")
				return
			}
			if ai.options != nil {
				kind := ""
				if !ix.uncertain {
					kind = indexReloptKind(ix.using)
				}
				if why := storageCheck(kind, *ai.options, ai.reset, s.m.source); why != "" {
					s.unproven(t, fmt.Sprintf("ALTER INDEX %s SET/RESET (...) may be refused: %s%s", ai.name, why, precondition("M4.P4")))
				}
			}
			return
		}
		if ai.name.schema == nil && s.pathChanged {
			s.partial(t, fmt.Sprintf("search_path may have changed earlier in the set; ALTER INDEX %s may rename another schema's index", ai.name))
			if !ai.ifExists {
				s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: the name may mean no index (search_path)", ai.name))
			}
		}
		if !s.renameIndexTo(t, *ai.newName) {
			return
		}
		ix.name = ai.newName
		return
	}
	if t, k := s.findKey(ai.name); k != nil {
		// The index of a named primary key or UNIQUE constraint: renaming
		// it renames the constraint (tablecmds.c RenameRelationInternal).
		s.touch(t)
		if ai.newName == nil {
			s.partial(t, "ALTER INDEX is not modeled")
			if ai.tableForm != "" {
				s.kindRefusal(what+" "+ai.tableForm, ai.name, "index", pathOK && s.failure == nil, attAll&^attIndex, "M8.P1")
				return
			}
			if ai.options != nil {
				// U9: a key's index is a btree index.
				if why := storageCheck("btree", *ai.options, ai.reset, s.m.source); why != "" {
					s.unproven(t, fmt.Sprintf("ALTER INDEX %s SET/RESET (...) may be refused: %s%s", ai.name, why, precondition("M4.P4")))
				}
			}
			return
		}
		if ai.name.schema == nil && s.pathChanged || s.failure != nil {
			s.partial(t, fmt.Sprintf("ALTER INDEX %s may not rename this table's constraint index", ai.name))
			if !ai.ifExists {
				s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: the name may mean no index (search_path)", ai.name))
			}
		}
		// R2: renaming a constraint's index renames the constraint, whose
		// name must be free on the table (pg_constraint.c
		// RenameConstraintById, 42710).
		if *ai.newName != *k.name && s.constraintNameHeld(t, *ai.newName) {
			s.refuse(t, fmt.Sprintf("ALTER INDEX %s RENAME TO %s: constraint %s exists on table %s (Postgres refuses)%s", ai.name, *ai.newName, *ai.newName, t.q, precondition("M8.P2")))
			return
		}
		if !constraintNamesExact(t) || generatedShape(t, *ai.newName, constraintSuffixes) != "" {
			s.unproven(t, fmt.Sprintf("ALTER INDEX %s RENAME TO %s may be refused: a constraint of that name may exist on table %s%s", ai.name, *ai.newName, t.q, precondition("M8.P2")))
		}
		if !s.renameIndexTo(t, *ai.newName) {
			return
		}
		n := *ai.newName
		k.name = &n
		return
	}
	if kind, certain, found := s.relationAt(ai.name); found && (kind != "index" || ai.tableForm != "") {
		// V4 kinds (2026-11-07, g8-s4299): the name is held by a relation
		// of another kind, so IF EXISTS does not skip. ALTER INDEX ...
		// RENAME accepts every relation but a composite type; every other
		// form only an index (RangeVarCallbackForAlterRelation
		// 17911-17950). A type or domain is no relation.
		if !pgClassKind(kind) {
			if !ai.ifExists {
				s.unknownFact(fmt.Sprintf("%s may be refused: the pass holds %s as %s, not as a relation%s", what, ai.name, article(kind), precondition("M8.P1")))
			}
			return
		}
		accepts := attIndex
		if ai.newName != nil {
			accepts = attAll &^ attComposite
		}
		if ai.tableForm != "" {
			accepts = 0
		}
		if s.kindRefusal(what, ai.name, kind, certain, accepts, "M8.P1") && kind != "generated index or sequence" {
			return
		}
	}
	if ai.newName != nil {
		if t, certain := s.target(ai.name); t != nil {
			s.touch(t)
			if s.uncertainName(certain) && !ai.ifExists {
				s.unproven(t, fmt.Sprintf("ALTER INDEX %s may be refused: the name may mean no relation", ai.name))
			}
			s.renameTable(t, *ai.newName)
			return
		}
		if _, certain, found := s.relationAt(ai.name); found {
			if !certain && !ai.ifExists {
				// Revision 8: the relation may not exist (a generated name,
				// or one an opaque statement may have dropped).
				s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: the relation may not exist", ai.name))
			}
			// P2: the new name must be free in pg_class.
			q := qname{schema: ai.name.schema, name: *ai.newName}
			refused, maybe := s.nameTaken(q, nil)
			if maybe == "" {
				maybe = s.generatedRelationName(q)
			}
			switch {
			case refused != "":
				s.mayFail(fmt.Sprintf("ALTER INDEX %s RENAME TO %s: %s (Postgres refuses the rename)%s", ai.name, q.name, refused, precondition("M8.P2")))
			case maybe != "":
				s.unknownFact(fmt.Sprintf("ALTER INDEX %s RENAME TO %s may be refused: %s%s", ai.name, q.name, maybe, precondition("M8.P2")))
			}
			s.moveRelation(ai.name, *ai.newName)
			return
		}
	} else if exact, _ := s.lookup(ai.name); exact != nil {
		s.refuse(exact, fmt.Sprintf("ALTER INDEX %s names a table (Postgres refuses)", ai.name))
		return
	}
	if hits := s.indexesNamed(ai.name); len(hits) > 0 {
		for _, t := range hits {
			s.touch(t)
			s.partial(t, fmt.Sprintf("ALTER INDEX %s may name this table's index %s under another qualification (search_path)", ai.name, ai.name.name))
		}
		if !ai.ifExists {
			s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: the name may mean no index under this qualification", ai.name))
		}
		return
	}
	hits := s.generatedIndexMayMatch(ai.name, "ALTER INDEX")
	if len(hits) > 0 && !ai.ifExists {
		// Revision 8: the name may also be no index at all.
		s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: the name was never written and may not be a generated index name", ai.name))
	}
	if len(hits) == 0 && !ai.ifExists && !s.needs(ai.name, kindClass, true, "ALTER INDEX "+ai.name.String(), "M8.P1") && !s.holds(ai.name.name, kindIndex) {
		// V4 (Revision 1.6): E1 does not cover indexes; a name the pass
		// does not hold may mean no index.
		s.unknownFact(fmt.Sprintf("ALTER INDEX %s may be refused: no index of that name is held, and E1 does not cover indexes%s", ai.name, precondition("M8.P1")))
	}
	if ai.tableForm != "" && ai.ifExists {
		// V4 kinds (2026-11-07): the form is refused on every relation, so
		// it applies only when no relation of the name exists, which E1
		// does not prove for indexes and sequences.
		s.unknownFact(fmt.Sprintf("%s %s may be refused: Postgres refuses it on any relation of that name, and E1 does not cover indexes or sequences%s", what, ai.tableForm, precondition("M8.P1")))
	}
}

// renameIndexTo checks the new name of an index of t (ALTER INDEX ...
// RENAME TO): a taken name is refused, one that may be taken may fail
// (Revision 8). It reports whether the rename applies.
func (s *state) renameIndexTo(t *table, newName string) bool {
	q := qname{schema: t.q.schema, name: newName}
	if _, other := s.findIndex(q); other != nil {
		s.refuse(t, fmt.Sprintf("index %s already exists (Postgres refuses the rename)", newName))
		return false
	}
	refused, maybe := s.nameTaken(q, nil)
	if refused != "" {
		s.refuse(t, refused+" (Postgres refuses the rename)")
		return false
	}
	if maybe == "" {
		maybe = s.generatedRelationName(q)
	}
	if maybe != "" {
		s.unproven(t, fmt.Sprintf("ALTER INDEX ... RENAME TO %s may be refused: %s", newName, maybe))
	}
	return true
}

// renameTable renames t unless the name is taken in its schema.
// A name that may be taken makes the rename one that may fail (Revision 8).
func (s *state) renameTable(t *table, newName string) {
	q := qname{schema: t.q.schema, name: newName}
	refused, maybe := s.nameTaken(q, nil)
	if refused != "" {
		s.refuse(t, refused+" (Postgres refuses the rename)")
		return
	}
	if maybe == "" {
		maybe = s.generatedRelationName(q)
	}
	if maybe != "" {
		s.unproven(t, fmt.Sprintf("RENAME TO %s may be refused: %s", newName, maybe))
	}
	t.previousNames = append(t.previousNames, t.q.name)
	t.q.name = newName
}

// renameSchema applies ALTER SCHEMA from RENAME TO to (WF6): tables written
// in the schema move to the new name (partial, since previous_names holds
// table names only); unqualified tables may live in it and become partial;
// later unqualified names may resolve elsewhere (R2).
func (s *state) renameSchema(from, to string) {
	for _, t := range s.tables {
		switch {
		case t.q.schema != nil && *t.q.schema == from:
			s.touch(t)
			old := t.q.String()
			n := to
			t.q.schema = &n
			s.partialName(t, fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s moved this table from %s", from, to, old))
		case t.q.schema == nil:
			s.partialName(t, fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s may rename the schema of this unqualified table", from, to))
		}
	}
	for _, k := range s.relationKeys() {
		if strings.HasPrefix(k, from+".") {
			r := s.relations[k]
			delete(s.relations, k)
			s.relations[to+"."+strings.TrimPrefix(k, from+".")] = r
		}
	}
	s.pathChanged = true

}

func (s *state) alterTable(at *alterTable) {
	t, certain := s.target(at.name)
	if t == nil {
		for _, a := range at.actions {
			if a.kind == "set_default" && a.exprUnread {
				// V7 (P7): on a table the pass does not hold, no typer
				// reads the expression; the syntax-only reader did not.
				s.unknownFact(fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT may be refused: the expression is not read completely by the syntax reader%s", at.name, a.name, precondition("M5e.P7")))
			}
		}
	}
	if t == nil && s.indexLike(at.name) {
		// Revision 3 WF-c: ALTER TABLE accepts an index (tablecmds.c
		// 17901-17906); RENAME renames it, SET SCHEMA is refused (17950),
		// other actions are ALTER INDEX forms that are not modeled.
		for _, a := range at.actions {
			switch a.kind {
			case "rename_table":
				newName := a.newName
				s.alterIndex(&alterIndex{name: at.name, newName: &newName, ifExists: at.ifExists})
			case "set_schema":
				s.mayFail(fmt.Sprintf("ALTER TABLE %s SET SCHEMA names an index (Postgres refuses)", at.name))
			case "rename_column":
				s.alterIndex(&alterIndex{name: at.name, ifExists: at.ifExists})
			case "unsupported":
				switch {
				case a.word == "owner":
					s.alterIndex(&alterIndex{name: at.name, ifExists: at.ifExists})
				case a.word == "storage_parameters":
					// U9: an index's parameters depend on its access
					// method, which this path does not prove.
					opts := a.options
					if opts == nil {
						opts = &[]storageOption{{}}
					}
					s.alterIndex(&alterIndex{name: at.name, ifExists: at.ifExists, options: opts, reset: a.reset})
					if _, ix := s.findIndex(at.name); ix == nil {
						if _, k := s.findKey(at.name); k == nil {
							s.unknownFact(fmt.Sprintf("ALTER TABLE %s SET/RESET (...) on an index may be refused: its access method is not known, so its storage parameters are not proven%s", at.name, precondition("M5k-a.P5")))
						}
					}
				case indexRefusedWords[a.word]:
					s.indexAction(at.name, a.word)
				default:
					// SET TABLESPACE and SET STATISTICS (of an expression
					// column) are accepted, other forms refused: not proven.
					s.alterIndex(&alterIndex{name: at.name, ifExists: at.ifExists})
					s.unknownFact(fmt.Sprintf("ALTER TABLE %s may be refused: the name may be an index, on which this action is not proven%s", at.name, precondition("M5a.P1")))
				}
			default:
				s.indexAction(at.name, a.kind)
			}
		}
		return
	}
	if t == nil {
		// The name may be the generated name of a key or bare-column index
		// (ALTER TABLE renames indexes too).
		hit := false
		for _, o := range s.tables {
			if sameSchema(o.q.schema, at.name.schema) && slices.Contains(generatedNames(o), at.name.name) {
				s.touch(o)
				s.partial(o, fmt.Sprintf("ALTER TABLE %s may name a generated index or sequence of this table", at.name))
				hit = true
			}
		}
		if hit {
			if !at.ifExists {
				// Revision 8: the name may also be no relation at all.
				s.unknownFact(fmt.Sprintf("ALTER TABLE %s may be refused: the name may not be a generated index or sequence name", at.name))
			}
			return
		}
		if kind, certain, found := s.relationAt(at.name); found {
			if !pgClassKind(kind) {
				// V4 kinds (2026-11-07): a type or domain is no relation;
				// IF EXISTS skips, without it Postgres refuses (42P01)
				// unless a relation the pass does not see holds the name.
				if !at.ifExists {
					s.unknownFact(fmt.Sprintf("ALTER TABLE %s may be refused: the pass holds %s as %s, not as a relation%s", at.name, at.name, article(kind), precondition("M5a.P1")))
				}
				return
			}
			// ALTER TABLE accepts other relations (tablecmds.c 17901-17906)
			// except composite types (17939-17946).
			if kind == "composite type" {
				if slices.ContainsFunc(at.actions, func(a alterAction) bool { return a.kind == "rename_column" }) {
					// RENAME COLUMN is a RenameStmt that accepts a composite
					// type (renameatt_check 3679-3691, checked with psql).
					s.unknownFact(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN may be refused: %s is a composite type whose attributes the pass does not track%s", at.name, at.name, precondition("M5a.P1")))
					return
				}
				s.mayFail(fmt.Sprintf("ALTER TABLE %s names a composite type (Postgres refuses)", at.name))
				return
			}
			// V4 kinds (2026-11-07): each action must be one the relation's
			// kind accepts, also with IF EXISTS (ATSimplePermissions).
			s.alterTableOnRelation(at, kind, certain)
			for _, a := range at.actions {
				switch a.kind {
				case "rename_table":
					s.moveRelation(at.name, a.newName)
				case "set_schema", "unsupported":
					s.moveRelation(at.name, "")
				}
			}
			return
		}
		// Revision 7: DROP CONSTRAINT applies to every known table the name
		// may mean (none when no table is known), and may fail without IF
		// EXISTS whatever the table.
		_, others := s.lookup(at.name)
		if len(others) > 0 && !at.ifExists {
			// Revision 8: a known table may be meant, or none.
			s.unknownFact(fmt.Sprintf("ALTER TABLE %s may be refused: the name may mean no table", at.name))
		}
		for _, a := range at.actions {
			if a.kind == "drop_constraint" {
				s.dropConstraint(others, a)
			}
		}
		if !at.ifExists {
			s.needs(at.name, kindClass, true, "ALTER TABLE "+at.name.String(), "M5a.P1")
		}
		s.outsideChange(at.name, "ALTER TABLE "+at.name.String(), "M5a.P1")
		s.unknown(at.name, at.ifExists, "ALTER TABLE")
		return
	}
	what := "ALTER TABLE " + at.name.String()
	other := false
	for i, a := range at.actions {
		at.actions[i].only = at.only
		if a.kind == "drop_constraint" {
			at.actions[i].afterOther = other
		} else if a.kind != "drop_column" {
			other = true
		}
	}
	for _, a := range at.actions {
		if a.column != nil {
			s.needsColumn(*a.column, what, "M5a.P1")
		}
		s.needsNames(a.typeNames, what, "M5a.P1")
		s.needsNames(a.exprNames, what, "M5a.P1")
		for _, ch := range a.constraints.checks {
			s.needsNames(ch.names, what, "M5a.P1")
		}
		for _, q := range []*qname{a.inherit, a.attach} {
			if q != nil {
				s.needs(*q, kindRelation, false, what, "M5a.P1")
			}
		}
	}
	s.touch(t)
	if s.uncertainName(certain) && !succeedsAnywhere(at, at.name.schema == nil && s.pathChanged) {
		// Revision 8: the name may mean another table or none, whose
		// columns and constraints are unknown.
		s.unproven(t, fmt.Sprintf("ALTER TABLE %s may be refused: the name may mean another table or none, whose columns and constraints are unknown", at.name))
	}
	// Postgres adds an ALTER TABLE statement's foreign keys after its other
	// actions (tablecmds.c AT_PASS_ADD_OTHERCONSTR).
	s.deferring = true
	for _, a := range s.passOrder(t, at) {
		s.alterAction(t, a, certain)
	}
	s.deferring = false
	pending := s.pending
	s.pending = nil
	at0 := s.at
	for _, p := range pending {
		if slices.Contains(s.tables, p.t) {
			s.at = p.at
			if p.ref.name != nil && s.constraintNameHeld(p.t, *p.ref.name) {
				// R2: Postgres adds the statement's foreign keys after its
				// other constraints, which may hold the name.
				s.refuse(p.t, fmt.Sprintf("constraint %s already exists on table %s (Postgres refuses, 42710)%s", *p.ref.name, p.t.q, precondition("M5f.P2")))
			}
			s.addForeignKey(p.t, p.ref, p.certain)
		}
	}
	s.at = at0
}

// succeedsAnywhere reports whether an ALTER TABLE statement succeeds
// whatever existing table its name means: its actions are all ADD COLUMN
// IF NOT EXISTS without constraints or DROP CONSTRAINT IF EXISTS (whose
// foreign-key users forgetKeys checks on every possible target). When the
// name may mean no table (mayMeanNone, after a search_path change) it also
// needs ALTER TABLE IF EXISTS. DROP COLUMN IF EXISTS does not count: a
// partition key or inherited column is refused even with CASCADE.
func succeedsAnywhere(at *alterTable, mayMeanNone bool) bool {
	if mayMeanNone && !at.ifExists {
		return false
	}
	for _, a := range at.actions {
		switch {
		case a.kind == "drop_constraint" && a.ifExists:
		case a.kind == "add_column" && a.ifNotExists && noConstraints(columnConstraintSet(*a.column)):
		default:
			return false
		}
	}
	return true
}

func noConstraints(tc tableConstraints) bool {
	return len(tc.primaryKeys)+len(tc.uniques)+len(tc.foreignKeys)+len(tc.checks) == 0
}

// dependents lists foreign keys of other constraints that reference any
// of the columns, excluding those in skip.
func (s *state) dependents(cols []*column, skip func(*foreignKey) bool) []*foreignKey {
	var out []*foreignKey
	for _, t := range s.tables {
		for _, fk := range t.foreignKeys {
			if skip != nil && skip(fk) {
				continue
			}
			for _, c := range fk.columns {
				if slices.Contains(cols, c) {
					out = append(out, fk)
					break
				}
			}
		}
	}
	return out
}

func (s *state) owner(fk *foreignKey) *table {
	for _, t := range s.tables {
		if slices.Contains(t.foreignKeys, fk) {
			return t
		}
	}
	return nil
}

func (s *state) dropForeignKey(fk *foreignKey) {
	if t := s.owner(fk); t != nil {
		t.foreignKeys = slices.DeleteFunc(t.foreignKeys, func(x *foreignKey) bool { return x == fk })
	}
}

// generatedUsing lists the generated columns of t whose expression names c
// (pg_attrdef.c 187-195: the generation expression depends on the columns
// it uses, so DROP COLUMN needs CASCADE and then drops them too).
func generatedUsing(t *table, c *column) []*column {
	names := append([]string{c.name}, c.previousNames...)
	var out []*column
	for _, g := range t.columns {
		if g == c || g.generated == nil {
			continue
		}
		for _, n := range g.generatedNames {
			if slices.Contains(names, n) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func (s *state) alterAction(t *table, a alterAction, certain bool) {
	at := s.at
	s.at = a.span
	defer func() { s.at = at }()
	col := func() *column {
		c := t.column(a.name)
		if c == nil && !(a.ifExists && a.kind == "drop_column") {
			s.missing(t, certain, fmt.Sprintf("column %s is not known", a.name))
		}
		return c
	}
	switch a.kind {
	case "unsupported":
		if a.word == "row_security" && s.set.tool == toolSupabase {
			// SB-M3: schema-neutral. The flag is set as written unless the
			// name may mean another table; after a possible failure the
			// migration's applied state carries the condition, as for the
			// other facts it writes (WF8).
			if !s.uncertainName(certain) {
				s.rowSecurity(t, a.rls)
			} else {
				t.rlsUnknown = true
			}
			return
		}
		known := certain && factsKnown(t)
		if unnamedFreeWords[a.word] {
			// C5: reasons this action adds to t create no unnamed object.
			from := len(t.reasons)
			defer func() {
				for i := from; i < len(t.reasons); i++ {
					t.reasons[i].noUnnamed = true
				}
			}()
		}
		s.partial(t, a.reason)
		if a.word == "" {
			// U1 (Revision 1.5): a subcommand the parser does not model and
			// no M5k word names is row D, not M5k: it may fail, and it may
			// rename, take or drop any index of the table.
			for _, ix := range t.indexes {
				ix.uncertain, ix.mayBeGone = true, true
			}
			t.keysUnknown = true
			s.mayFail(fmt.Sprintf("ALTER TABLE %s: %s (row D: the subcommand is not modeled, so it may be refused, and every index of the table may be renamed or taken)", t.q, a.reason))
			return
		}
		switch {
		case a.addIdentity && known:
			s.addIdentity(t, a)
		case a.addIdentity:
			// Revision 8: NOT NULL, identity and default are not known.
			s.unproven(t, fmt.Sprintf("ADD GENERATED AS IDENTITY on %s may be refused: the table is partial, so whether the column is NOT NULL without identity or default is unknown", a.name))
		case a.word == "set_logged" || a.word == "set_unlogged":
			s.changePersistence(t, a.word == "set_logged", known)
		case a.word == "set_without_cluster":
			t.clustered = nil
		case a.word == "storage_parameters":
			// U9 (P5): the list against the table's kind.
			kind := ""
			if known && !t.kindUnknown && !t.mayBeOther {
				kind = "heap"
				if t.partitionKey != nil {
					kind = "partitioned"
				}
			}
			why := "the parameter list could not be read"
			if a.options != nil {
				why = storageCheck(kind, *a.options, a.reset, s.m.source)
			}
			if why != "" {
				s.unproven(t, fmt.Sprintf("ALTER TABLE %s SET/RESET (...) may be refused: %s%s", t.q, why, precondition("M5k-a.P5")))
			}
		case a.word == "inherit" && a.inherit != nil && known && s.inheritProven(t, *a.inherit):
		case a.word == "no_inherit" && a.noInherit != nil && certain && t.opaqueAfter == 0 && s.noInheritProven(t, *a.noInherit):
		case a.dependsOn != "":
			// P4: an M5k-b word succeeds only when the object or option it
			// names exists and fits, which the pass does not track.
			s.unproven(t, fmt.Sprintf("ALTER TABLE %s (%s) may be refused: it depends on %s, which the pass does not track%s", t.q, strings.ReplaceAll(a.word, "_", " "), a.dependsOn, precondition("M5k-b.P4")))
		}
		if a.attach != nil {
			if p, _ := s.lookup(*a.attach); p != nil && p != t {
				p.partitionOf = t
			}
		}
	case "add_using_index":
		s.addUsingIndex(t, a, certain)
	case "add_column":
		if why := typePosition(a.column.typ, "column"); why != "" {
			// V13 (Revision 1.6): a pseudo-type column.
			s.refuse(t, fmt.Sprintf("ADD COLUMN %s: %s%s", a.column.name, why, precondition("M2.P4")))
			return
		}
		if why := s.columnTypeProven(a.column.typ) + s.collationProven(a.column.collation); why != "" {
			s.unproven(t, fmt.Sprintf("ADD COLUMN %s may be refused: %s%s", a.column.name, why, precondition("M2.P1")))
		}
		if systemColumns[a.column.name] {
			// U10: also under IF NOT EXISTS (tablecmds.c
			// check_for_column_name_collision).
			s.refuse(t, fmt.Sprintf("ADD COLUMN %s: a system column name (Postgres refuses)%s", a.column.name, precondition("M5a.P2")))
			return
		}
		if !s.onlyChildren(t, a, "ADD COLUMN "+a.column.name) {
			return
		}
		if t.column(a.column.name) != nil {
			if !a.ifNotExists {
				s.refuse(t, fmt.Sprintf("column %s is added twice (Postgres refuses)", a.column.name))
			}
			return
		}
		if !a.ifNotExists && !factsKnown(t) {
			// Revision 8: a column the pass does not know may exist.
			s.unproven(t, fmt.Sprintf("ADD COLUMN %s may be refused: the table is partial, so a column of this name may exist", a.column.name))
		}
		if a.ifNotExists && !factsKnown(t) {
			// P3 (Revision 1.4, g1-s56780): a column the pass does not
			// know may exist (an inherited one, for example), and IF NOT
			// EXISTS then skips the definition, so it writes no facts. When
			// the column is new, its definition is checked as usual (R3,
			// R4), and its constraints and expressions, which the partial
			// table may conflict with, are not proven.
			def := *a.column
			s.columnDefinitionChecks(t, def, "M5a")
			tc := columnConstraintSet(def)
			if len(tc.primaryKeys)+len(tc.uniques)+len(tc.foreignKeys)+len(tc.checks) > 0 || def.def != nil || def.generated != nil || def.identity != nil {
				s.unproven(t, fmt.Sprintf("ADD COLUMN IF NOT EXISTS %s may be refused: the table is partial, so its constraints and expressions are not proven%s", def.name, precondition("M5a.P3")))
			}
			s.partial(t, fmt.Sprintf("ADD COLUMN IF NOT EXISTS %s on a partial table: a column of this name may exist, so the definition may be skipped and its facts are not known%s", def.name, precondition("M5a.P3")))
			return
		}
		def := *a.column
		def.typ = s.columnDefinitionChecks(t, def, "M5a")
		if columnChecksRows(def) {
			s.rowsChecked(t, "ADD COLUMN "+def.name, "M5a.P11")
		}
		c := s.addColumn(t, def)
		s.tableConstraints(t, columnConstraintSet(def), certain, true, "M5a")
		s.columnExpressions(t, c, "ALTER TABLE "+t.q.String()+" ADD COLUMN "+c.name, "M5a.P5")
	case "drop_column":
		c := col()
		if c == nil {
			return
		}
		if !certain {
			s.partial(t, fmt.Sprintf("DROP COLUMN %s may not apply to this table; the column may have been dropped", c.name))
			return
		}
		if inPartitionKey(t, c) {
			// Revision 4 WF4 (tablecmds.c 9047-9057).
			s.refuse(t, fmt.Sprintf("cannot drop column %s, which is part of the partition key (Postgres refuses)", c.name))
			return
		}
		s.dropColumnStatement(t, c, a.cascade)
	case "alter_type":
		if why := s.typeProven(a.typ) + s.collationProven(a.collation); why != "" {
			s.unproven(t, fmt.Sprintf("ALTER COLUMN %s TYPE may be refused: %s%s", a.name, why, precondition("M2.P1")))
		}
		c := col()
		if c == nil {
			return
		}
		if !s.onlyChildren(t, a, "ALTER COLUMN "+c.name+" TYPE") {
			return
		}
		if a.reason != "" {
			s.refuse(t, a.reason)
			return
		}
		if len(generatedUsing(t, c)) > 0 {
			// tablecmds.c 13627-13634.
			s.refuse(t, fmt.Sprintf("cannot alter type of column %s, which a generated column uses (Postgres refuses)", c.name))
			return
		}
		if !factsKnown(t) {
			// Revision 8: generated columns, partition keys and foreign
			// keys that use the column may be unknown.
			s.unproven(t, fmt.Sprintf("ALTER COLUMN %s TYPE may be refused: the table is partial, so objects that depend on the column are unknown", c.name))
		} else if fks := s.mayReference(t, c); len(fks) > 0 {
			s.unproven(t, fmt.Sprintf("ALTER COLUMN %s TYPE may be refused: an unresolved foreign key may reference the column", c.name))
		}
		if triggerUses(t, c) {
			s.unproven(t, fmt.Sprintf("ALTER COLUMN %s TYPE may be refused: a trigger the set created may depend on the column (Postgres refuses, 0A000)%s", c.name, precondition("M5c.P8")))
		}
		if inPartitionKey(t, c) {
			// Revision 4 WF4 (tablecmds.c 12871-12877).
			if certain {
				s.refuse(t, fmt.Sprintf("cannot alter type of column %s, which is part of the partition key (Postgres refuses)", c.name))
			} else {
				s.partial(t, fmt.Sprintf("ALTER COLUMN TYPE of %s may be refused (partition key)", c.name))
			}
			return
		}
		s.rowsChecked(t, "ALTER COLUMN "+c.name+" TYPE", "M5c.P11")
		stored, v, why := typmodCheck(a.typ)
		switch {
		case v == typmodRefused:
			s.refuse(t, fmt.Sprintf("ALTER COLUMN %s TYPE %s: %s (Postgres refuses)%s", c.name, a.typ, why, precondition("M2.P5")))
		case v == typmodUnknown && strings.Contains(a.typ, "("):
			s.unproven(t, fmt.Sprintf("ALTER COLUMN %s TYPE %s may be refused: %s%s", c.name, a.typ, why, precondition("M2.P5")))
		}
		a.typ = stored
		s.retypeProven(t, c, a)
		s.retypeForeignKeys(t, c, a.typ)
		if s.views {
			s.partial(t, "ALTER COLUMN TYPE may be refused by a dependent view (views are not modeled)")
			s.mayFail("ALTER COLUMN TYPE may be refused by a dependent view")
			return
		}
		c.typ, c.typeNames, c.typeResolved, c.collation = a.typ, a.typeNames, "", a.collation
	case "set_not_null":
		if c := col(); c != nil {
			if c.nullable {
				s.rowsChecked(t, "SET NOT NULL on "+c.name, "M5d.P11")
			}
			c.nullable = false
		}
	case "drop_not_null":
		c := col()
		if c == nil {
			return
		}
		if c.identity != nil || t.primaryKey != nil && slices.Contains(t.primaryKey.cols, c) {
			s.refuse(t, fmt.Sprintf("DROP NOT NULL on %s is refused (primary key or identity column)", c.name))
			return
		}
		if !factsKnown(t) {
			// Revision 8.
			s.unproven(t, fmt.Sprintf("DROP NOT NULL on %s may be refused: the table is partial, so whether the column is in the primary key or an identity column is unknown", c.name))
		}
		c.nullable = true
	case "set_default", "drop_default":
		c := col()
		if c == nil {
			return
		}
		if c.identity != nil || c.generated != nil {
			s.refuse(t, fmt.Sprintf("%s on identity or generated column %s is refused", a.kind, c.name))
			return
		}
		if !factsKnown(t) {
			// Revision 8.
			s.unproven(t, fmt.Sprintf("%s on %s may be refused: the table is partial, so whether the column is an identity or generated column is unknown", a.kind, c.name))
		}
		if a.expr != nil {
			s.defaultProven(t, c, *a.expr, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT", t.q, c.name), "M5e.P5")
		}
		c.def, c.implied, c.exprNames = s.storedDefault(a.expr, c.typ), false, a.exprNames
	case "add_constraint":
		// P8: an inherited CHECK added with ONLY must reach the children
		// (tablecmds.c ATAddCheckNNConstraint, 42P16).
		for _, ch := range a.constraints.checks {
			if !ch.exclude && !ch.noInherit && !s.onlyChildren(t, a, "ADD CHECK") {
				return
			}
		}
		s.tableConstraints(t, a.constraints, certain, true, "M5f")
	case "drop_constraint":
		_, others := s.lookup(t.q)
		if len(others) == 0 && s.dropWrittenConstraint(t, a, certain) {
			// C3 (M5g-1): a name the set wrote on t, tracked exactly.
			s.set.rows["M5g-1"]++
			return
		}
		// Revision 7: every table the name may mean.
		s.dropConstraint(append([]*table{t}, others...), a)
	case "rename_column":
		c := col()
		if c == nil {
			return
		}
		// P8: an inherited column renamed with ONLY must be renamed in the
		// children too (tablecmds.c renameatt_internal, 42P16).
		if !s.onlyChildren(t, a, "RENAME COLUMN "+c.name) {
			return
		}
		if t.column(a.newName) != nil {
			s.refuse(t, fmt.Sprintf("column %s already exists (Postgres refuses the rename)", a.newName))
			return
		}
		if systemColumns[a.newName] {
			s.refuse(t, fmt.Sprintf("RENAME COLUMN %s TO %s: %s is a system column name (Postgres refuses)%s", c.name, a.newName, a.newName, precondition("M5h.P2")))
			return
		}
		if !factsKnown(t) {
			// Revision 8: a column the pass does not know may hold the name.
			s.unproven(t, fmt.Sprintf("RENAME COLUMN %s TO %s may be refused: the table is partial, so a column named %s may exist", c.name, a.newName, a.newName))
		}
		if t.triggerColumns[c.name] {
			// A trigger's UPDATE OF column follows the rename.
			delete(t.triggerColumns, c.name)
			t.triggerColumns[a.newName] = true
		}
		c.previousNames = append(c.previousNames, c.name)
		c.name = a.newName
	case "rename_table":
		s.renameTable(t, a.newName)
	case "set_schema":
		q := qname{schema: &a.newName, name: t.q.name}
		if exact, _ := s.lookup(q); exact != nil {
			s.refuse(t, fmt.Sprintf("table %s already exists (Postgres refuses SET SCHEMA)", q))
			return
		}
		refused, maybe := s.nameTaken(q, t)
		if maybe == "" {
			maybe = s.generatedRelationName(q)
		}
		if refused != "" {
			s.refuse(t, refused+" (Postgres refuses SET SCHEMA)")
			return
		} else if maybe != "" {
			s.unproven(t, fmt.Sprintf("SET SCHEMA %s may be refused: %s", a.newName, maybe))
		}
		old := t.q.String()
		t.q = q
		s.partialName(t, fmt.Sprintf("SET SCHEMA moved this table from %s", old))
	}
}

// inPartitionKey reports whether PARTITION BY of t names c (by its current
// or a previous name).
func inPartitionKey(t *table, c *column) bool {
	if slices.Contains(t.partitionKey, c.name) {
		return true
	}
	for _, n := range c.previousNames {
		if slices.Contains(t.partitionKey, n) {
			return true
		}
	}
	return false
}

// addIdentity applies the refusals of ALTER COLUMN ... ADD GENERATED AS
// IDENTITY (Revision 4 G-b, tablecmds.c 8064-8085): the column must be
// NOT NULL and have no identity and no default. The caller checks that
// t's facts are known (factsKnown) and its name surely means t; the action
// itself is not modeled.
func (s *state) addIdentity(t *table, a alterAction) {
	c := t.column(a.name)
	if c == nil {
		return
	}
	base, _ := typeBase(writtenType(c))
	switch {
	case base != "int2" && base != "int4" && base != "int8":
		// P4: identity columns are smallint, integer or bigint only
		// (tablecmds.c ATExecAddIdentity, 22023); a type the pass cannot
		// read may be a domain over one of them.
		s.unproven(t, fmt.Sprintf("ADD GENERATED AS IDENTITY on %s may be refused: its type %s is not proven to be smallint, integer or bigint%s", c.name, c.typ, precondition("M5k-b.P4")))
	case c.nullable:
		s.refuse(t, fmt.Sprintf("ADD GENERATED AS IDENTITY on %s, which is not NOT NULL (Postgres refuses)", c.name))
	case c.identity != nil:
		s.refuse(t, fmt.Sprintf("ADD GENERATED AS IDENTITY on %s, which is already an identity column (Postgres refuses)", c.name))
	case c.def != nil || c.implied || c.generated != nil:
		s.refuse(t, fmt.Sprintf("ADD GENERATED AS IDENTITY on %s, which has a default (Postgres refuses)", c.name))
	}
}

// writtenType is c's type as Postgres resolves it from the column
// definition: a serial alias stands for the integer type it creates
// (parse_utilcmd.c 575-601). The type is compared with canonicalType.
func writtenType(c *column) string {
	if c.typeResolved != "" {
		return c.typeResolved
	}
	return c.typ
}

// retypeForeignKeys applies ALTER COLUMN TYPE of c to the known foreign
// keys that use c on either side (Revision 4 G-c): Postgres rebuilds them
// and refuses the statement when the types are not compatible, which the
// pass does not decide. Unless the new type is the other side's type
// (canonicalType equates aliases such as int and int4), the foreign key
// becomes unresolved, both tables partial, and the migration may fail.
func (s *state) retypeForeignKeys(t *table, c *column, typ string) {
	for _, o := range slices.Clone(s.tables) {
		for _, fk := range o.foreignKeys {
			if fk.table == nil {
				continue
			}
			var other *column
			if o == t {
				if i := slices.Index(fk.local, c); i >= 0 {
					other = fk.columns[i]
				}
			}
			if fk.table == t && other == nil {
				if i := slices.Index(fk.columns, c); i >= 0 {
					other = fk.local[i]
				}
			}
			if other == nil || other == c {
				continue
			}
			if local := o == t && slices.Contains(fk.local, c); local && fkTypePair(c, typ, other, writtenType(other)) ||
				!local && fkTypePair(other, writtenType(other), c, typ) {
				continue
			}
			text := fmt.Sprintf("ALTER COLUMN %s TYPE %s: whether Postgres accepts the foreign key with %s (type %s) is unknown", c.name, typ, other.name, other.typ)
			ref := fk.table
			s.unresolve(fk, "a column of this foreign key changed type; whether the types are compatible is unknown")
			s.partialOnce(o, text)
			s.partialOnce(ref, text)
			s.mayFail(text)
		}
	}
}

// mayReference lists the unresolved foreign keys that may reference
// column c of t (Revision 8): their written table may be t and their
// written columns name c, or they name none and the primary key may hold
// c. A foreign key whose own column is c goes with it and is not listed.
func (s *state) mayReference(t *table, c *column) []*foreignKey {
	names := append([]string{c.name}, c.previousNames...)
	var out []*foreignKey
	for _, o := range s.tables {
		for _, fk := range o.foreignKeys {
			if fk.table != nil || !namesTable(fk.written.table, t) || slices.Contains(fk.local, c) {
				continue
			}
			uses := false
			if fk.written.columns == nil {
				uses = t.primaryKey == nil || slices.Contains(t.primaryKey.cols, c)
			}
			for _, n := range fk.written.columns {
				uses = uses || slices.Contains(names, n)
			}
			if uses {
				out = append(out, fk)
			}
		}
	}
	return out
}

// dropColumnStatement applies ALTER TABLE DROP COLUMN c: foreign keys of
// other tables and generated columns that use c need CASCADE (without it
// the drop is refused), and CASCADE drops them too (WF7). Revision 8:
// without CASCADE, a partial table, or an unresolved foreign key that may
// reference c, makes the drop one that may be refused.
func (s *state) dropColumnStatement(t *table, c *column, cascade bool) {
	if cascade {
		s.viewsMayBeGone()
		if triggerUses(t, c) {
			// CASCADE drops a trigger that uses the column.
			t.triggerNamesUnknown = true
		}
	}
	if !cascade && triggerUses(t, c) {
		s.unproven(t, fmt.Sprintf("DROP COLUMN %s without CASCADE may be refused: a trigger the set created may depend on the column (Postgres refuses, 2BP01)%s", c.name, precondition("M5c.P8")))
	}
	if !cascade {
		switch {
		case !factsKnown(t):
			s.unproven(t, fmt.Sprintf("DROP COLUMN %s without CASCADE may be refused: the table is partial, so objects that depend on the column are unknown", c.name))
		case len(s.mayReference(t, c)) > 0:
			s.unproven(t, fmt.Sprintf("DROP COLUMN %s without CASCADE may be refused: an unresolved foreign key may reference the column", c.name))
		}
	}
	gen := generatedUsing(t, c)
	cols := append([]*column{c}, gen...)
	deps := s.dependents(cols, func(fk *foreignKey) bool { return slices.Contains(t.foreignKeys, fk) && slices.Contains(fk.local, c) })
	// Revision 4: a unique index that INCLUDEs c goes with it, and so do
	// the foreign keys that use it.
	var included []*foreignKey
	for _, ix := range t.indexes {
		if backsKey(ix) && slices.Contains(ix.include, c.name) {
			for _, fk := range s.keyUsers(t, ix.columns, false) {
				if fk.table != nil && !slices.Contains(deps, fk) && !slices.Contains(included, fk) {
					included = append(included, fk)
				}
			}
		}
	}
	if len(included) > 0 && !cascade {
		s.refuse(t, fmt.Sprintf("DROP COLUMN %s without CASCADE is refused while foreign keys use a unique index that includes it", c.name))
		return
	}
	for _, fk := range included {
		if o := s.owner(fk); o != nil {
			s.touch(o)
			s.unresolve(fk, fmt.Sprintf("DROP COLUMN %s CASCADE may have dropped this foreign key with a unique index that includes the column", c.name))
			s.partialOnce(o, fmt.Sprintf("DROP COLUMN %s.%s CASCADE may have dropped a foreign key of this table", t.q, c.name))
		}
	}
	if (len(deps) > 0 || len(gen) > 0) && !cascade {
		why := fmt.Sprintf("DROP COLUMN %s without CASCADE is refused while foreign keys reference it", c.name)
		if len(deps) == 0 {
			why = fmt.Sprintf("DROP COLUMN %s without CASCADE is refused while generated column %s uses it", c.name, gen[0].name)
		}
		s.refuse(t, why)
		for _, fk := range deps {
			if o := s.owner(fk); o != nil && o != t {
				s.partial(o, fmt.Sprintf("a refused DROP COLUMN of %s.%s involves this table", t.q, c.name))
			}
		}
		return
	}
	if s.views && !cascade {
		s.partial(t, "DROP COLUMN may be refused by a dependent view (views are not modeled)")
		s.mayFail("DROP COLUMN may be refused by a dependent view")
		return
	}
	for _, fk := range deps {
		s.dropForeignKey(fk)
	}
	for _, g := range gen {
		s.dropColumn(t, g)
	}
	s.dropColumn(t, c)
}

// dropColumn removes a column and the indexes and table constraints that
// involve it (ALTER TABLE DROP COLUMN).
func (s *state) dropColumn(t *table, c *column) {
	names := append([]string{c.name}, c.previousNames...)
	mentions := func(list []string) bool {
		for _, n := range list {
			if slices.Contains(names, n) {
				return true
			}
		}
		return false
	}
	t.indexes = slices.DeleteFunc(t.indexes, func(ix *index) bool {
		if slices.Contains(ix.columns, c) || slices.Contains(ix.include, c.name) {
			return true
		}
		if mentions(ix.whereNames) {
			s.partial(t, fmt.Sprintf("partial index predicate may depend on dropped column %s", c.name))
			return true
		}
		for i, el := range ix.elements {
			if ix.columns[i] == nil && mentions(el.names) {
				s.partial(t, fmt.Sprintf("expression index element %q may depend on dropped column %s", el.text, c.name))
				return true
			}
		}
		return false
	})
	if t.primaryKey != nil && slices.Contains(t.primaryKey.cols, c) {
		t.primaryKey = nil
	}
	t.uniques = slices.DeleteFunc(t.uniques, func(k *key) bool { return slices.Contains(k.cols, c) })
	// CHECK and EXCLUDE constraints that use the column go with it.
	t.checks = slices.DeleteFunc(t.checks, func(ch check) bool { return mentions(ch.names) })
	t.unnamedCheckNames = slices.DeleteFunc(t.unnamedCheckNames, mentions)
	t.foreignKeys = slices.DeleteFunc(t.foreignKeys, func(fk *foreignKey) bool {
		return slices.Contains(fk.local, c) || slices.Contains(fk.columns, c)
	})
	t.columns = slices.DeleteFunc(t.columns, func(x *column) bool { return x == c })
}

func (s *state) dropTables(d *dropObjects) {
	var drop []*table
	for _, q := range d.names {
		t, certain := s.target(q)
		if t == nil {
			if kind, certain, found := s.relationAt(q); found {
				switch {
				case kind == "table":
					if !certain && !d.ifExists {
						s.unknownFact(fmt.Sprintf("DROP TABLE %s may be refused: the table may no longer exist", q))
					}
					s.dropRelation(namedObject{q: q, kind: "table"})
				case certain:
					// RemoveRelations refuses another relation kind.
					s.mayFail(fmt.Sprintf("DROP TABLE %s names %s (Postgres refuses)", q, article(kind)))
				default:
					// P1: the name may be held by a relation of another kind,
					// which Postgres refuses even with IF EXISTS.
					s.unknownFact(fmt.Sprintf("DROP TABLE %s may be refused: the name may be %s%s", q, article(kind), precondition("M6.P1")))
				}
				continue
			}
			if _, others := s.lookup(q); len(others) > 0 && !d.ifExists {
				s.unknownFact(fmt.Sprintf("DROP TABLE %s may be refused: the name may mean no table", q))
			}
			if !d.ifExists {
				s.needs(q, kindRelation, true, "DROP TABLE "+q.String(), "M6.P1")
			}
			s.outsideReferences(q, d)
			s.unknown(q, d.ifExists, "DROP TABLE")
			continue
		}
		s.touch(t)
		if !certain {
			s.partial(t, fmt.Sprintf("DROP TABLE %s may not have dropped this table (the name may resolve elsewhere, or the migration may fail)", q))
			// Revision 8: after a search_path change the name may mean no
			// table; under another qualification it may mean a table whose
			// dependents are unknown. A temporary table of the name exists
			// and has no permanent dependents.
			_, others := s.lookup(q)
			pathOnly := q.schema == nil && s.pathChanged
			if s.uncertainName(certain) && (pathOnly && !d.ifExists || len(others) > 0 && !d.cascade) {
				s.unproven(t, fmt.Sprintf("DROP TABLE %s may be refused: the name may mean no table or another one", q))
			}
			continue
		}
		if !d.cascade {
			// Revision 8: dependents of a partial table are unknown, and an
			// unresolved foreign key may reference the table.
			unresolved := s.referencing(t, func(fk *foreignKey) bool { return fk.table == nil && !slices.Contains(t.foreignKeys, fk) })
			like := slices.ContainsFunc(s.tables, func(o *table) bool {
				return o != t && o.likeOf == t && !slices.ContainsFunc(d.names, func(n qname) bool { return n.String() == o.q.String() })
			})
			why := ""
			switch {
			case !factsKnown(t):
				why = fmt.Sprintf("DROP TABLE %s without CASCADE may be refused: the table is partial, so objects that depend on it are unknown", q)
			case len(unresolved) > 0:
				why = fmt.Sprintf("DROP TABLE %s without CASCADE may be refused: an unresolved foreign key may reference it", q)
			case like:
				// P8: a table created LIKE this one may have copied defaults
				// that use its sequences (2BP01).
				why = fmt.Sprintf("DROP TABLE %s without CASCADE may be refused: a table created LIKE it may use its sequences%s", q, precondition("M6.P8"))
			}
			if why != "" {
				// The table may still exist: it stays, partial.
				s.unproven(t, why)
				continue
			}
		}
		drop = append(drop, t)
	}
	// Revision 4: dropping a partitioned table drops its partitions, and
	// CASCADE drops the tables that inherit from a dropped one.
	for i := 0; i < len(drop); i++ {
		for _, o := range slices.Clone(s.tables) {
			if slices.Contains(drop, o) {
				continue
			}
			switch {
			case o.partitionOf == drop[i] && complete(drop[i]):
				s.touch(o)
				drop = append(drop, o)
			case o.partitionOf == drop[i]:
				s.touch(o)
				s.partial(o, fmt.Sprintf("DROP TABLE %s may have dropped this partition with it", drop[i].q))
			case d.cascade && slices.ContainsFunc(drop[i].children, func(q qname) bool { return q.String() == o.q.String() }):
				s.touch(o)
				drop = append(drop, o)
			}
		}
	}
	var deps []*foreignKey
	for _, t := range s.tables {
		if slices.Contains(drop, t) {
			continue
		}
		for _, fk := range t.foreignKeys {
			if slices.Contains(drop, fk.table) {
				deps = append(deps, fk)
			}
		}
	}
	// Revision 3 class 3: a column of another table whose type is the row
	// type of a dropped table, or a table that inherits from it, also makes
	// Postgres refuse the drop without CASCADE.
	var rowUsers []*table
	why := ""
	for _, o := range s.tables {
		if slices.Contains(drop, o) {
			continue
		}
		for _, t := range drop {
			if col := o.usesObject("table", t.q); col != "" {
				rowUsers = append(rowUsers, o)
				if why == "" {
					why = fmt.Sprintf("DROP TABLE %s without CASCADE is refused while column %s of %s has its row type", t.q, col, o.q)
				}
				break
			}
		}
	}
	dropped := func(q qname) bool {
		return slices.ContainsFunc(d.names, func(n qname) bool { return n.String() == q.String() }) ||
			slices.ContainsFunc(drop, func(t *table) bool { return t.q.String() == q.String() })
	}
	for _, t := range drop {
		for _, child := range t.children {
			if !dropped(child) && why == "" {
				why = fmt.Sprintf("DROP TABLE %s without CASCADE is refused while table %s inherits from it", t.q, child)
			}
		}
	}
	switch {
	case d.cascade:
		why = ""
		if len(drop) > 0 {
			s.viewsMayBeGone()
		}
	case len(deps) > 0:
		why = "DROP TABLE without CASCADE is refused while foreign keys reference it"
	case why == "" && s.views && len(drop) > 0:
		why = "DROP TABLE may be refused by a dependent view (views are not modeled)"
	}
	if why != "" {
		for _, t := range drop {
			s.partial(t, why)
		}
		for _, fk := range deps {
			if o := s.owner(fk); o != nil {
				s.partial(o, "a refused DROP TABLE involves this table")
			}
		}
		for _, o := range rowUsers {
			s.partial(o, "a refused DROP TABLE involves this table")
		}
		s.mayFail(why)
		return
	}
	for _, t := range drop {
		var owners []*table
		for _, fk := range deps {
			if fk.table == t {
				if o := s.owner(fk); o != nil && !slices.Contains(owners, o) {
					owners = append(owners, o)
				}
			}
		}
		for _, fk := range deps {
			if fk.table == t {
				s.dropForeignKey(fk)
			}
		}
		s.remove(t, owners)
		for _, child := range t.children {
			if exact, _ := s.lookup(child); exact == nil {
				s.dropRelation(namedObject{q: child, kind: "table"})
			}
		}
	}
	if d.cascade && len(drop) > 0 {
		// Class 1: columns of the dropped tables' row types, and defaults
		// or constraints that use them, go too.
		var names []qname
		for _, t := range drop {
			names = append(names, t.q)
		}
		s.dropDependencies("table", names, true, nil)
	}
}

func (s *state) dropIndexes(d *dropObjects) {
	if d.concurrently && !s.inBlock && (len(d.names) > 1 || d.cascade) {
		// C4: outside a block DROP INDEX CONCURRENTLY drops one index and
		// has no CASCADE (tablecmds.c RemoveRelations, 0A000).
		s.mayFail("DROP INDEX CONCURRENTLY names several indexes or CASCADE (Postgres refuses)" + precondition("M7.P6"))
	}
	for _, q := range d.names {
		if t, ix := s.findIndex(q); ix != nil {
			s.touch(t)
			s.dropIndex(t, ix, q, d)
			continue
		}
		if t, k := s.findKey(q); k != nil {
			// Revision 4 WF2: the index of a written constraint name.
			s.dropConstraintIndex(t, q)
			continue
		}
		if exact, _ := s.lookup(q); exact != nil {
			s.refuse(exact, fmt.Sprintf("DROP INDEX %s names a table (Postgres refuses)", q))
			continue
		}
		if kind, certain, found := s.relationAt(q); found && kind != "index" && pgClassKind(kind) {
			// V4 kinds (2026-11-07): a relation of another kind holds the
			// name, so IF EXISTS does not skip (DropErrorMsgWrongType).
			if s.kindRefusal("DROP INDEX "+q.String(), q, kind, certain, attIndex, "R5.P4") && kind != "generated index or sequence" {
				continue
			}
		}
		if hits := s.indexesNamed(q); len(hits) > 0 {
			for _, t := range hits {
				s.touch(t)
				s.partial(t, fmt.Sprintf("DROP INDEX %s may name this table's index %s under another qualification (search_path); the index may have been dropped", q, q.name))
				for _, ix := range t.indexes {
					if ix.name != nil && *ix.name == q.name {
						ix.uncertain = true
						// P3: the index this name may mean is no longer a fact.
						ix.mayBeGone = true
						s.partialOnce(t, fmt.Sprintf("DROP INDEX %s may have dropped index %s, which is no longer stated%s", q, q.name, precondition("M7.P3")))
					}
				}
			}
			keyIndex := slices.ContainsFunc(hits, func(t *table) bool { return keyNamed(t, q.name) })
			switch {
			case keyIndex:
				// Revision 4 WF2: the name may mean a constraint's index,
				// which Postgres refuses to drop even with IF EXISTS.
				s.unknownFact(fmt.Sprintf("DROP INDEX %s may be refused: the name may mean the index of a primary key or unique constraint under this qualification", q))
			case !d.ifExists:
				// Revision 8: the name may mean no index.
				s.unknownFact(fmt.Sprintf("DROP INDEX %s may be refused: the name may mean no index under this qualification", q))
			}
			continue
		}
		keyIndex := false
		for _, t := range slices.Clone(s.tables) {
			if sameSchema(t.q.schema, q.schema) && slices.Contains(generatedKeyNames(t), q.name) {
				// Revision 4 WF2: the generated name of a constraint's index.
				s.dropConstraintIndex(t, q)
				keyIndex = true
			}
		}
		if keyIndex {
			continue
		}
		hits := s.generatedIndexMayMatch(q, "DROP INDEX")
		if len(hits) > 0 && !d.ifExists {
			// Revision 8: the name may also be no index at all.
			s.unknownFact(fmt.Sprintf("DROP INDEX %s may be refused: the name was never written and may not be a generated index name", q))
		}
		for _, t := range hits {
			s.dropUnwrittenIndex(t, q, d.cascade)
		}
		if d.concurrently && !s.inBlock {
			// C4 (P4): an index the pass does not hold may be a
			// partitioned index, which Postgres refuses to drop
			// concurrently (0A000).
			s.unknownFact(fmt.Sprintf("DROP INDEX CONCURRENTLY %s may be refused: the pass does not hold the index, so whether it is partitioned is not proven%s", q, precondition("M7.P6")))
		}
		if len(hits) == 0 && !d.ifExists {
			if !s.needs(q, kindIndex, true, "DROP INDEX "+q.String(), "M7.P1") {
				// H12 (Revision 1.5): E1 covers tables, schemas, roles and
				// functions outside the set, not indexes, so an index the
				// pass does not hold is not proven to exist.
				s.unknownFact(fmt.Sprintf("DROP INDEX %s may be refused: the pass does not hold an index of this name, and E1 does not cover indexes outside the set%s", q, precondition("M7.P1")))
			}
			s.unresolved(fmt.Sprintf("DROP INDEX %s: index is not known", q))
		}
	}
}

// dropSchemas applies DROP SCHEMA to the tables it can name: written
// qualifications are dropped with CASCADE (or partial when refused);
// unqualified tables may live in the schema and become partial. Later
// unqualified names may resolve elsewhere (R2).
func (s *state) dropSchemas(d *dropObjects) {
	for _, q := range d.names {
		schema := q.name
		for _, t := range slices.Clone(s.tables) {
			inSchema := t.q.schema != nil && *t.q.schema == schema
			switch {
			case inSchema && d.cascade && s.failure == nil:
				var owners []*table
				for _, fk := range s.dependents(t.columns, nil) {
					if o := s.owner(fk); o != nil && o != t {
						owners = append(owners, o)
					}
					s.dropForeignKey(fk)
				}
				s.remove(t, owners)
			case inSchema && d.cascade:
				s.touch(t)
				s.partial(t, fmt.Sprintf("DROP SCHEMA %s CASCADE may have dropped this table", schema))
			case inSchema:
				s.refuse(t, "DROP SCHEMA without CASCADE is refused while it holds this table")
			case t.q.schema == nil:
				s.partial(t, fmt.Sprintf("DROP SCHEMA %s may remove this unqualified table", schema))
			}
		}
		if d.cascade {
			// Class 1: objects of the schema (types, functions, tables'
			// row types) may be used by tables elsewhere.
			s.dropDependencies("schema", []qname{{name: schema}}, true, nil)
		}
		for k, r := range s.relations {
			if strings.HasPrefix(k, schema+".") {
				if d.cascade {
					delete(s.relations, k)
				} else {
					r.uncertain = true
				}
			}
		}
	}
	s.pathChanged = true
}

// fkTypePair reports C2 (T018a2 B3 as amended by Revision 1): a foreign
// key column of type local may reference a key column of type ref. The
// verified pairs are those tablecmds.c ATAddForeignKeyConstraint accepts
// through an equality operator and implicit coercion (REL_17_6 9850-9899,
// each confirmed by the oracle, generator g5): the same type after alias
// normalisation; int2/int4/int8 in any combination; float4/float8; text,
// varchar, varchar(n) and char(n) in any combination; an integer column
// referencing a numeric key (not the reverse). A column with an explicit
// collation is proven only against the same type (collations of text keys
// must be compatible). Every other pair is not proven.
func fkTypePair(lc *column, local string, rc *column, ref string) bool {
	if sameType(local, ref) {
		return true
	}
	if lc.collation != nil || rc.collation != nil {
		return false
	}
	a, okA := typeBase(local)
	b, okB := typeBase(ref)
	if !okA || !okB {
		return false
	}
	a, b = strings.TrimPrefix(a, "pg_catalog."), strings.TrimPrefix(b, "pg_catalog.")
	family := func(t string) string {
		switch t {
		case "int2", "int4", "int8":
			return "int"
		case "float4", "float8":
			return "float"
		case "text", "varchar", "bpchar":
			return "text"
		case "numeric":
			return "numeric"
		}
		return ""
	}
	fa, fb := family(a), family(b)
	switch {
	case fa == "" || fb == "":
		return false
	case fa == fb && fa != "numeric":
		return true
	default:
		return fa == "int" && fb == "numeric"
	}
}

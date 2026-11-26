package schema

import (
	"fmt"
	"strings"
)

// The recognizer turns one lexed statement into a dialect-neutral
// operation the schema state applies. It recognizes the Postgres subset in
// docs/extract/schema.md; everything else is classified without effect,
// unsupported, opaque or as a parse error.

// class is a statement's classification in the migration counts.
type class string

const (
	classModeled     class = "modeled"
	classNoEffect    class = "no_effect"
	classUnsupported class = "unsupported"
	classOpaque      class = "opaque"
	classError       class = "error"
)

// qname is a possibly schema-qualified relation name, folded per P2.
type qname struct {
	schema *string
	name   string
}

func (q qname) String() string {
	if q.schema != nil {
		return *q.schema + "." + q.name
	}
	return q.name
}

// byteSpan is a half-open byte range in the normalized migration source.
type byteSpan struct{ start, end int }

// columnDef is a column definition of CREATE TABLE or ADD COLUMN.
type columnDef struct {
	name string
	span byteSpan
	typ  string
	// typeNames are the identifiers of the written type (G1).
	typeNames    []string
	serial       string // smallint, integer or bigint for serial aliases
	notNull      bool
	explicitNull bool
	def          *string
	collation    *string
	unique       []constraintDef
	primaryKey   *constraintDef
	identity     *string
	generated    *string
	// exprNames are the identifiers of the DEFAULT and generation
	// expressions; generatedNames those of the generation expression only.
	exprNames      []string
	generatedNames []string
	references     []referenceDef
	checks         []constraintDef
	// Revision 1.4 R3 (column definitions): how often each clause was
	// written, and the clauses whose validity the pass does not prove.
	// Postgres refuses a second DEFAULT, identity, generation expression,
	// PRIMARY KEY or COLLATE (parse_utilcmd.c transformColumnDefinition,
	// gram.y SplitColQualList); identityOptions are the sequence options
	// of an identity column; storage and compression are STORAGE and
	// COMPRESSION, which depend on the type.
	defaults, identities, generations, primaryKeys, collations int
	identityOptions                                            bool
	storage, compression                                       bool
}

// constraintDef carries the written name of a constraint (nil when unnamed).
type constraintDef struct {
	// optionsUnproven says why a key's WITH (...) list is not proven (U9).
	optionsUnproven string
	name            *string
	columns         []string
	span            byteSpan
	// names are the identifiers of a CHECK or EXCLUDE constraint, so that
	// DROP COLUMN can drop the constraints that use the column.
	names []string
	// deferrable is a DEFERRABLE (or INITIALLY DEFERRED) primary key or
	// unique constraint: a foreign key cannot reference it (Revision 3).
	deferrable bool
	// The other attributes Postgres compares when it merges equal index
	// constraints of one statement (Revision 7, parse_utilcmd.c
	// 2084-2140): INITIALLY DEFERRED, NULLS NOT DISTINCT and the INCLUDE
	// columns. includeUnknown is set when INCLUDE is not a plain column
	// list, so the comparison cannot be made.
	initDeferred     bool
	nullsNotDistinct bool
	include          []string
	includeUnknown   bool
	// expr is the text of a CHECK constraint's expression (Revision 1.4
	// R1); exclude marks an EXCLUDE constraint, whose body is excludeBody;
	// notValid is NOT VALID (existing rows are not checked, R5).
	expr        *string
	exclude     bool
	excludeBody string
	notValid    bool
	// noInherit is NO INHERIT on a CHECK: ALTER TABLE ONLY may add it to
	// a table with inheritance children (tablecmds.c, Revision 1.4 P8).
	noInherit bool
}

// referenceDef is a REFERENCES clause or a table-level FOREIGN KEY.
type referenceDef struct {
	name     *string
	local    []string
	table    qname
	columns  []string // nil: the referenced table's primary key
	onDelete *string
	onUpdate *string
	span     byteSpan
	// matchNotSimple is set by MATCH FULL or MATCH PARTIAL: a NULL in one
	// column then does not exempt a row from the check (B2).
	matchNotSimple bool
	// matchPartial is MATCH PARTIAL, which Postgres refuses (0A000,
	// tablecmds.c "MATCH PARTIAL not yet implemented"; Revision 1.4 R3).
	matchPartial bool
	// notValid is NOT VALID: existing rows are not checked (R5).
	notValid bool
	// setColumns are the columns of ON DELETE SET NULL / SET DEFAULT (...);
	// setColumnsOnUpdate is set when ON UPDATE has a column list, which
	// gram.y refuses; setColumnsUnread when a list could not be read.
	setColumns         []string
	setColumnsOnUpdate bool
	setColumnsUnread   bool
}

// tableConstraints groups the table-level constraints of one statement.
type tableConstraints struct {
	primaryKeys []constraintDef
	uniques     []constraintDef
	foreignKeys []referenceDef
	checks      []constraintDef
}

type createTable struct {
	name        qname
	ifNotExists bool
	columns     []columnDef
	constraints tableConstraints
	// unreadable is set when the header was read but the body was not
	// (Revision 3 G4): the table is emitted with its name only, partial.
	unreadable string
	// unmodeled is set for the forms whose columns come from elsewhere
	// (LIKE, OF type, PARTITION OF, INHERITS; Revision 4 G-d): the table is
	// emitted with its name only, partial, and the statement is not a
	// possible failure.
	unmodeled string
	// partitionKey holds the identifiers of PARTITION BY (Revision 4 WF4);
	// partitionBy is its strategy (range, list, hash; "" without PARTITION
	// BY) and partitionPlain is set when every element is a bare column
	// (Revision 1.4 R3).
	partitionKey   []string
	partitionBy    string
	partitionPlain bool
	// partitionCount is the number of PARTITION BY elements.
	partitionCount int
	// optionsUnproven names a table option whose success the pass does not
	// prove (Revision 1.4 R3); onCommit is ON COMMIT, refused on a
	// permanent table.
	optionsUnproven string
	onCommit        bool
	// partitionOf is the parent of PARTITION OF; ofType the type of OF.
	partitionOf *qname
	ofType      *qname
	// unlogged is CREATE UNLOGGED TABLE (persistence, P4).
	unlogged bool
	// likeOf is the source of LIKE: copied defaults may use its sequences
	// (P8).
	likeOf *qname
}

// isTempSchema reports the names of the session's temporary schema: the
// pg_temp alias (namespace.c 673-678) and the pg_temp_N schemas
// (isAnyTempNamespace, namespace.c 3692-3697).
func isTempSchema(schema *string) bool {
	return schema != nil && (*schema == "pg_temp" || strings.HasPrefix(*schema, "pg_temp_"))
}

type createIndex struct {
	name         *string
	table        qname
	ifNotExists  bool
	unique       bool
	concurrently bool
	using        *string
	elements     []indexElement
	include      []string
	where        *string
	// whereNames are the identifiers of the WHERE predicate.
	whereNames []string
	// nullsNotDistinct is NULLS NOT DISTINCT (U1); options are the WITH
	// (...) storage parameters (U9).
	nullsNotDistinct bool
	options          *[]storageOption
}

// indexElement is one written index element; column is set when the
// element is a bare column reference, names holds every identifier the
// element mentions (for expression elements).
type indexElement struct {
	text   string
	column *string
	names  []string
	// colname is the name Postgres derives for an expression element
	// that is a plain function call f(...) (FigureIndexColname,
	// parse_target.c 1723-1800 at REL_17_6: a FuncCall is named by its
	// function); nil when the pass does not derive it.
	colname *string
	// opclass is the operator class written after a bare column (V9).
	opclass *string
}

// specialCall are call-like forms that are not FuncCall nodes and take
// another name (CAST and TREAT name their argument, ROW and ARRAY none).
var specialCall = map[string]bool{"cast": true, "treat": true, "row": true, "array": true}

// alterAction is one comma-separated action of ALTER TABLE.
type alterAction struct {
	kind string
	// exprUnread is set on SET DEFAULT when the syntax-only expression
	// reader does not read the expression whole (V7); the typer decides on
	// a held table, and an outside table's statement may fail.
	exprUnread  bool
	span        byteSpan
	column      *columnDef
	name        string // column or constraint name the action targets
	newName     string
	ifExists    bool
	ifNotExists bool
	cascade     bool
	typ         string
	typeNames   []string
	collation   *string
	expr        *string
	exprNames   []string
	constraints tableConstraints
	reason      string // unsupported actions
	// usingIndex is ADD [CONSTRAINT n] PRIMARY KEY | UNIQUE USING INDEX i
	// (row M5f-u, Revision 1.5 U1).
	usingIndex *usingIndex
	// options and reset are the list of SET (...) / RESET (...) (U9); nil
	// when it could not be read.
	options *[]storageOption
	reset   bool
	// inherit is the parent of INHERIT p (an unsupported action).
	inherit *qname
	// addIdentity is ALTER COLUMN ... ADD GENERATED ... AS IDENTITY (an
	// unsupported action; Revision 4 G-b).
	addIdentity bool
	// attach is the partition of ATTACH PARTITION p (an unsupported
	// action): dropping the table drops it too.
	attach *qname
	// noInherit is the parent of NO INHERIT p (an unsupported action).
	noInherit *qname
	// using is the USING expression of ALTER COLUMN TYPE (P5).
	using *string
	// sole names an action gram.y accepts only as the whole statement
	// (RENAME, SET SCHEMA, ATTACH/DETACH PARTITION; P7).
	sole string
	// word is the M5k subform of an unsupported action, and dependsOn is
	// set for the M5k-b words, whose success depends on an object or
	// option the pass does not track (P4).
	word      string
	dependsOn string
	// rls is the ROW LEVEL SECURITY word of a row_security action
	// (enable, disable, force, no_force; SB-M3).
	rls string
	// afterOther is set on a DROP CONSTRAINT written after a subcommand
	// other than DROP CONSTRAINT or DROP COLUMN in the same statement
	// (C3): Postgres runs every DROP CONSTRAINT in AT_PASS_DROP, before
	// the subcommands that add constraints, so the written order is not
	// the order the name is looked up in.
	afterOther bool
	// only is copied from the statement's ONLY.
	only bool
}

type alterTable struct {
	name     qname
	ifExists bool
	// only is ALTER TABLE ONLY (no recursion to inheritance children).
	only    bool
	actions []alterAction
}

type dropObjects struct {
	kind     string // table, index, schema
	names    []qname
	ifExists bool
	cascade  bool
	// concurrently is DROP INDEX CONCURRENTLY.
	concurrently bool
}

// analysis is the recognizer's result for one statement.
type analysis struct {
	// path is a search_path change the pass reads (U3); nil with searchPath
	// set is a change it does not read.
	path *pathChange
	// sequence is a CREATE or ALTER SEQUENCE option list (U5).
	sequence *sequenceStmt
	// row is the claim-contract row the statement was produced under
	// (contract.go rowOf; D for the default branch).
	row   string
	class class
	// opaqueKind is set for statements that count as opaque (R4 kinds).
	opaqueKind string
	// rlsInert marks an SB-M9 data statement (supabase.go): opaque,
	// but it cannot change any table's row level security.
	rlsInert bool
	// reason explains unsupported, opaque and error classes.
	reason string
	// target is the table a failed modeled statement was about, when known.
	target *qname
	// searchPath marks R2 statements.
	searchPath bool
	// extension is the CREATE EXTENSION name.
	extension *string
	// defines reports CREATE FUNCTION/PROCEDURE/TRIGGER (R4).
	function *string
	trigger  bool
	// views reports CREATE [MATERIALIZED] VIEW (dependents for DROP).
	view bool
	// temp is the name of a TEMP table (it shadows later unqualified names).
	temp *qname
	// mayFail is set when Postgres refuses or may refuse the statement:
	// the whole migration may then fail (Revision 2 WF8).
	mayFail string
	// notProven is set when the statement may be refused but keeps its
	// row (Revision 1.5 U7, N2.P1/P4/P5): the migration may fail.
	notProven string
	// runsFunction is set on a modeled statement that also runs a function
	// the set defines on existing rows (DEFAULT, CHECK, index expression;
	// Revision 2 G5): it counts as opaque as well.
	runsFunction string
	// relations are relations or types other than modeled tables that the
	// statement creates (views, sequences, types, CREATE TABLE AS, SELECT
	// INTO targets, CREATE SCHEMA elements). A later CREATE TABLE of such a
	// name is skipped or refused (Revision 2 WF3).
	relations []namedObject
	// dropped are relations or types other than tables dropped by name.
	dropped []namedObject
	// renamed is ALTER VIEW/SEQUENCE/TYPE ... RENAME TO, or any other ALTER
	// of such an object (newName empty).
	renamed *renameObject
	// dependents are objects whose removal or change affects the columns,
	// defaults or indexes that use them (Revision 2 G1).
	dependents []dependentObject
	// allTables makes every known table partial (DROP OWNED, DROP
	// EXTENSION CASCADE, other DROP ... CASCADE; Revision 2 WF2).
	allTables string
	// schemaRename is ALTER SCHEMA from RENAME TO to (Revision 2 WF6).
	schemaRename *[2]string
	// dropObjects is DROP of a kind other than TABLE, INDEX, SCHEMA and
	// OWNED with readable names (Revision 3 class 1).
	dropOthers *dropOthers
	// inherits are the parents of CREATE TABLE ... INHERITS or ALTER TABLE
	// ... INHERIT: DROP TABLE of a parent without CASCADE is refused.
	inherits []qname
	// inheritor is the child of inherits.
	inheritor *qname
	// lexing is set by a setting that changes how later query strings are
	// lexed or accepted (standard_conforming_strings, backslash_quote;
	// Revision 3 G8).
	lexing string

	// query names a statement whose query Postgres analyses when it runs
	// (a view, materialized view, CREATE TABLE AS or SELECT INTO; R1b).
	query string
	// tempReferences is a CREATE TEMP TABLE with a foreign key (P4).
	tempReferences bool

	create *createTable
	index  *createIndex
	alter  *alterTable
	drop   *dropObjects
	// alterIndex is ALTER INDEX.
	alterIndex *alterIndex
	// enum is ALTER TYPE ... ADD VALUE or RENAME VALUE (R3).
	enum *enumOp
}

// enumOp is ALTER TYPE n ADD VALUE [IF NOT EXISTS] 'v' [BEFORE | AFTER 'w']
// or ALTER TYPE n RENAME VALUE 'v' TO 'w' (gram.y AlterEnumStmt).
type enumOp struct {
	q           qname
	rename      bool
	label       string
	newLabel    string
	ifNotExists bool
	// neighbor is the BEFORE or AFTER label; before tells which.
	neighbor *string
	before   bool
	// undecoded is set when a label is written in a form the pass does
	// not decode (W7): the labels are compared as unknown.
	undecoded bool
}

// enumLabels reads ( 'a', 'b', ... ) after CREATE TYPE n AS ENUM; it
// returns nil when the list is not plain string constants. An empty
// list is an enum without labels. Labels are the decoded string values
// (W7, Revision 1.7); a label the pass does not decode (literalValue:
// octal, hexadecimal or Unicode escapes, U&'...') makes the labels
// unknown, reported by undecoded.
func enumLabels(c *cursor) (labels []string, undecoded bool) {
	if !c.acceptPunct("(") {
		return nil, false
	}
	labels = []string{}
	if c.acceptPunct(")") {
		return labels, false
	}
	for {
		t := c.tok()
		if c.done() || t.kind != tokString {
			return nil, false
		}
		c.i++
		v, ok := literalValue(t, c.src)
		if !ok {
			undecoded = true
		}
		labels = append(labels, v)
		if c.acceptPunct(")") {
			if !c.done() {
				return nil, false
			}
			if undecoded {
				return nil, true
			}
			return labels, false
		}
		if !c.acceptPunct(",") {
			return nil, false
		}
	}
}

// enumOperation reads ADD VALUE and RENAME VALUE after ALTER TYPE; ok is
// false for any other form (the cursor is restored).
func enumOperation(c *cursor) (*enumOp, bool) {
	start := c.i
	fail := func() (*enumOp, bool) {
		c.i = start
		return nil, false
	}
	n, err := c.name()
	if err != nil {
		return fail()
	}
	op := &enumOp{q: n}
	str := func() (string, bool) {
		t := c.tok()
		if c.done() || t.kind != tokString {
			return "", false
		}
		c.i++
		// W7: the decoded value; an undecoded label is unknown.
		v, ok := literalValue(t, c.src)
		if !ok {
			op.undecoded = true
		}
		return v, true
	}
	switch {
	case c.acceptKw("add", "value"):
		op.ifNotExists = c.acceptKw("if", "not", "exists")
		v, ok := str()
		if !ok {
			return fail()
		}
		op.label = v
		if c.acceptKw("before") || c.acceptKw("after") {
			op.before = c.toks[c.i-1].value == "before"
			w, ok := str()
			if !ok {
				return fail()
			}
			op.neighbor = &w
		}
	case c.acceptKw("rename", "value"):
		v, ok := str()
		if !ok || !c.acceptKw("to") {
			return fail()
		}
		w, ok := str()
		if !ok {
			return fail()
		}
		op.rename, op.label, op.newLabel = true, v, w
	default:
		return fail()
	}
	if !c.done() {
		return fail()
	}
	return op, true
}

// namedObject is a relation or type that is not a modeled table.
type namedObject struct {
	q           qname
	kind        string
	ifNotExists bool
	// uncertain is set when the schema of the name is not known (a CREATE
	// SCHEMA AUTHORIZATION CURRENT_USER element).
	uncertain bool
	// orReplace is CREATE OR REPLACE VIEW.
	orReplace bool
	// labels are the labels of CREATE TYPE ... AS ENUM, in order (R2); nil
	// for other kinds or a list that is not plain string constants.
	labels []string
	// labelsUndecoded is set when a label is written in a form the pass
	// does not decode (W7): the enum's labels are unknown.
	labelsUndecoded bool
}

// relationKinds are the kinds stored in pg_class: CREATE TABLE IF NOT
// EXISTS of such a name is skipped (parse_utilcmd.c 195-211). The other
// kinds are types only, and CREATE TABLE fails on them with or without IF
// NOT EXISTS (heap.c 1170-1180: the table's row type would conflict).
var relationKinds = map[string]bool{
	"view": true, "materialized view": true, "sequence": true, "composite type": true,
	"table": true, "foreign table": true, "index": true,
}

// renameObject is an ALTER of a relation or type other than a table.
type renameObject struct {
	q       qname
	kind    string
	newName string
	// attributes is the first word of ALTER TYPE ... ADD | DROP | ALTER |
	// RENAME ATTRIBUTE, the ALTER TABLE and RenameStmt forms on a composite
	// type, or "rename" for ALTER VIEW / MATERIALIZED VIEW ... RENAME
	// [COLUMN] (V4 kinds, 2026-11-07).
	attributes string
	ifExists   bool
}

// dependentObject names an object that columns, defaults or indexes may
// use: kind is type, collation, function or sequence.
type dependentObject struct {
	kind string
	name string
	why  string
}

// dropOthers is DROP of other objects by name. harmless kinds are those
// nothing in a table can use.
type dropOthers struct {
	kind    string
	names   []qname
	cascade bool
}

// alterIndex is ALTER INDEX name; newName is set for RENAME TO.
type alterIndex struct {
	name    qname
	newName *string
	// ifExists is ALTER INDEX IF EXISTS (or ALTER TABLE IF EXISTS on an
	// index): Postgres skips a missing name.
	ifExists bool
	// options and reset are ALTER INDEX ... SET (...) / RESET (...) (U9).
	options *[]storageOption
	reset   bool
	// tableForm is the text of a SET form other than SET (...), RESET
	// (...) and SET TABLESPACE: a table action Postgres refuses on an
	// index (V4 kinds, 2026-11-07).
	tableForm string
}

// parseError is a recognizer failure at a token.
type parseError struct{ reason string }

func (e parseError) Error() string { return e.reason }

type cursor struct {
	toks []token
	i    int
	src  []byte
}

func (c *cursor) done() bool { return c.i >= len(c.toks) }

// next moves past the current token; it returns true so that it can end a
// condition that has matched it.
func (c *cursor) next() bool {
	c.i++
	return true
}

func (c *cursor) tok() token {
	if c.done() {
		return token{kind: tokPunct, start: len(c.src), end: len(c.src)}
	}
	return c.toks[c.i]
}

// isKw reports whether the token at offset k from the cursor is the
// unquoted keyword w.
func (c *cursor) isKw(k int, w string) bool {
	j := c.i + k
	return j < len(c.toks) && c.toks[j].kind == tokIdent && c.toks[j].value == w
}

// peekKw reports whether the next tokens are the given keywords.
func (c *cursor) peekKw(words ...string) bool {
	for k, w := range words {
		if !c.isKw(k, w) {
			return false
		}
	}
	return true
}

func (c *cursor) acceptKw(words ...string) bool {
	if c.peekKw(words...) {
		c.i += len(words)
		return true
	}
	return false
}

func (c *cursor) isPunct(v string) bool {
	return !c.done() && c.toks[c.i].kind == tokPunct && c.toks[c.i].value == v
}

func (c *cursor) acceptPunct(v string) bool {
	if c.isPunct(v) {
		c.i++
		return true
	}
	return false
}

func (c *cursor) fail(format string, args ...any) parseError {
	at := "end of statement"
	if !c.done() {
		at = fmt.Sprintf("%q", c.src[c.toks[c.i].start:c.toks[c.i].end])
	}
	return parseError{reason: fmt.Sprintf(format, args...) + " at " + at}
}

// ident reads an identifier (unquoted or quoted) where gram.y takes a
// ColId (U8, Revision 1.5): an unquoted reserved or type_func_name keyword
// is a syntax error there.
func (c *cursor) ident() (string, error) {
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return "", c.fail("expected a name")
	}
	if t.kind == tokIdent {
		if cat := pgKeywordCategories[t.value]; cat == 'R' || cat == 'T' {
			return "", c.fail("keyword %s %s (gram.y ColId)", t.value, keywordNameError)
		}
	}
	c.i++
	return t.value, nil
}

// label reads an identifier after a dot (gram.y attr_name, ColLabel):
// every keyword is accepted there (U8).
func (c *cursor) label() (string, error) {
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return "", c.fail("expected a name")
	}
	c.i++
	return t.value, nil
}

// funcName reads a routine name (gram.y func_name 17185, U8): an
// unqualified name is a type_function_name, which takes type_func_name
// keywords but no col_name or reserved one; a qualified name starts with
// a ColId.
func (c *cursor) funcName() (qname, error) {
	t := c.tok()
	if !c.done() && t.kind == tokIdent && !(c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == ".") {
		switch pgKeywordCategories[t.value] {
		case 'R', 'C':
			return qname{}, c.fail("keyword %s %s (gram.y type_function_name)", t.value, keywordNameError)
		case 'T':
			c.i++
			return qname{name: t.value}, nil
		}
	}
	return c.name()
}

// name reads [schema.]name; a three-part name keeps its last two parts
// (the catalog must be the current database).
func (c *cursor) name() (qname, error) {
	first, err := c.ident()
	if err != nil {
		return qname{}, err
	}
	parts := []string{first}
	for c.isPunct(".") {
		c.i++
		next, err := c.label()
		if err != nil {
			return qname{}, err
		}
		parts = append(parts, next)
	}
	switch len(parts) {
	case 1:
		return qname{name: parts[0]}, nil
	case 2, 3:
		s := parts[len(parts)-2]
		return qname{schema: &s, name: parts[len(parts)-1]}, nil
	}
	return qname{}, c.fail("name with too many parts")
}

// group returns the token index just after the bracket group that starts
// at the cursor; the cursor is not moved.
func (c *cursor) group() (int, error) {
	depth := 0
	for j := c.i; j < len(c.toks); j++ {
		t := c.toks[j]
		if t.kind != tokPunct {
			continue
		}
		switch t.value {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
			if depth == 0 {
				return j + 1, nil
			}
		}
	}
	return 0, c.fail("unbalanced parentheses")
}

// text is the written text of tokens [from, to) with every gap between
// tokens (whitespace, comments) compacted to one space.
func (c *cursor) text(from, to int) string {
	var b strings.Builder
	for j := from; j < to; j++ {
		if j > from && c.toks[j].start > c.toks[j-1].end {
			b.WriteByte(' ')
		}
		b.Write(c.src[c.toks[j].start:c.toks[j].end])
	}
	return b.String()
}

func (c *cursor) span(from, to int) byteSpan {
	if from >= to {
		at := len(c.src)
		if from < len(c.toks) {
			at = c.toks[from].start
		}
		return byteSpan{at, at}
	}
	return byteSpan{c.toks[from].start, c.toks[to-1].end}
}

// splitList splits tokens [from, to) at top-level commas into ranges.
func (c *cursor) splitList(from, to int) [][2]int {
	var out [][2]int
	depth, start := 0, from
	for j := from; j < to; j++ {
		t := c.toks[j]
		if t.kind != tokPunct {
			continue
		}
		switch t.value {
		case "(", "[":
			depth++
		case ")", "]":
			depth--
		case ",":
			if depth == 0 {
				out = append(out, [2]int{start, j})
				start = j + 1
			}
		}
	}
	return append(out, [2]int{start, to})
}

// identifiers returns the folded values of the identifier tokens in
// [from, to).
func (c *cursor) identifiers(from, to int) []string {
	var out []string
	for j := from; j < to && j < len(c.toks); j++ {
		if c.toks[j].kind == tokIdent || c.toks[j].kind == tokQuoted {
			out = append(out, c.toks[j].value)
		}
	}
	return out
}

// nameList reads ( a, b, ... ) of plain column names.
func (c *cursor) nameList() ([]string, error) {
	if !c.acceptPunct("(") {
		return nil, c.fail("expected (")
	}
	var out []string
	for {
		n, err := c.ident()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		if c.acceptPunct(",") {
			continue
		}
		if c.acceptPunct(")") {
			return out, nil
		}
		return nil, c.fail("expected , or )")
	}
}

// serialTypes is parse_utilcmd.c 575-601: the single unqualified type name,
// after lexer folding, compared with strcmp.
var serialTypes = map[string]string{
	"smallserial": "smallint", "serial2": "smallint",
	"serial": "integer", "serial4": "integer",
	"bigserial": "bigint", "serial8": "bigint",
}

// columnKeywords end a column's type or a DEFAULT expression.
var columnKeywords = map[string]bool{
	"constraint": true, "not": true, "null": true, "default": true, "primary": true,
	"unique": true, "references": true, "check": true, "collate": true, "generated": true,
	"deferrable": true, "initially": true, "storage": true, "compression": true,
}

func (c *cursor) atColumnKeyword() bool {
	t := c.tok()
	return !c.done() && t.kind == tokIdent && columnKeywords[t.value]
}

// columnDefinition reads name type [constraints] over tokens [from, to).
func columnDefinition(c *cursor, from, to int) (columnDef, error) {
	sub := &cursor{toks: c.toks[:to], i: from, src: c.src}
	col := columnDef{span: c.span(from, to)}
	name, err := sub.ident()
	if err != nil {
		return col, err
	}
	col.name = name
	typeStart := sub.i
	if err := columnType(sub); err != nil {
		return col, err
	}
	col.typ = sub.text(typeStart, sub.i)
	col.typeNames = sub.identifiers(typeStart, sub.i)
	if sub.i == typeStart+1 {
		t := sub.toks[typeStart]
		if t.kind == tokIdent || t.kind == tokQuoted {
			col.serial = serialTypes[t.value]
		}
	}
	if col.serial != "" {
		col.notNull = true
	}
	var pending *string
	// last is the key constraint just read, for a following DEFERRABLE;
	// attrs is set after a constraint that takes DEFERRABLE / INITIALLY
	// (PRIMARY KEY, UNIQUE, REFERENCES and their attribute clauses):
	// elsewhere they are misplaced (parse_utilcmd.c
	// transformConstraintAttrs, 42601; Revision 1.4, P7).
	var last *constraintDef
	attrsNext := false
	for !sub.done() {
		start := sub.i
		key := last
		last = nil
		attrs := attrsNext
		attrsNext = false
		if !attrs && (sub.peekKw("deferrable") || sub.peekKw("not", "deferrable") || sub.peekKw("initially")) {
			return col, sub.fail("misplaced DEFERRABLE or INITIALLY clause")
		}
		switch {
		case sub.acceptKw("constraint"):
			n, err := sub.ident()
			if err != nil {
				return col, err
			}
			pending = &n
			continue
		case sub.acceptKw("not", "null"):
			col.notNull = true
		case sub.acceptKw("null"):
			col.explicitNull = true
		case sub.acceptKw("default"):
			exprStart := sub.i
			if sub.done() {
				return col, sub.fail("expected a default expression")
			}
			if err := skipExpression(sub); err != nil {
				return col, err
			}
			text := sub.text(exprStart, sub.i)
			col.def = &text
			col.defaults++
			col.exprNames = append(col.exprNames, sub.identifiers(exprStart, sub.i)...)
		case sub.acceptKw("primary", "key"):
			include, known, optionsWhy := skipIndexParameters(sub)
			col.primaryKey = &constraintDef{name: pending, columns: []string{col.name}, span: sub.span(start, sub.i), include: include, includeUnknown: !known, optionsUnproven: optionsWhy}
			col.primaryKeys++
			col.notNull = true
			last = col.primaryKey
			attrsNext = true
		case sub.acceptKw("unique"):
			notDistinct := false
			if sub.acceptKw("nulls") {
				notDistinct = sub.acceptKw("not")
				if !sub.acceptKw("distinct") {
					return col, sub.fail("expected DISTINCT")
				}
			}
			include, known, optionsWhy := skipIndexParameters(sub)
			col.unique = append(col.unique, constraintDef{name: pending, columns: []string{col.name}, span: sub.span(start, sub.i), include: include, includeUnknown: !known, optionsUnproven: optionsWhy, nullsNotDistinct: notDistinct})
			last = &col.unique[len(col.unique)-1]
			attrsNext = true
		case sub.peekKw("references"):
			ref, err := references(sub)
			if err != nil {
				return col, err
			}
			ref.name, ref.local = pending, []string{col.name}
			ref.span = sub.span(start, sub.i)
			col.references = append(col.references, ref)
			attrsNext = true
		case sub.acceptKw("check"):
			if !sub.isPunct("(") {
				return col, sub.fail("expected (")
			}
			end, err := sub.group()
			if err != nil {
				return col, err
			}
			names := sub.identifiers(sub.i, end)
			expr := sub.text(sub.i+1, end-1)
			sub.i = end
			sub.acceptKw("no", "inherit")
			col.checks = append(col.checks, constraintDef{name: pending, span: sub.span(start, sub.i), names: names, expr: &expr})
		case sub.acceptKw("collate"):
			n, err := sub.name()
			if err != nil {
				return col, err
			}
			text := n.String()
			col.collation = &text
			col.collations++
		case sub.acceptKw("generated"):
			kind := ""
			switch {
			case sub.acceptKw("always"):
				kind = "always"
			case sub.acceptKw("by", "default"):
				kind = "by default"
			default:
				return col, sub.fail("expected ALWAYS or BY DEFAULT")
			}
			if !sub.acceptKw("as") {
				return col, sub.fail("expected AS")
			}
			if sub.acceptKw("identity") {
				if sub.isPunct("(") {
					end, err := sub.group()
					if err != nil {
						return col, err
					}
					col.identityOptions = true
					sub.i = end
				}
				col.identities++
				col.identity = &kind
				col.notNull = true
			} else if kind == "always" && sub.isPunct("(") {
				end, err := sub.group()
				if err != nil {
					return col, err
				}
				text := sub.text(sub.i+1, end-1)
				col.generatedNames = sub.identifiers(sub.i+1, end-1)
				col.exprNames = append(col.exprNames, col.generatedNames...)
				sub.i = end
				if !sub.acceptKw("stored") {
					return col, sub.fail("expected STORED")
				}
				col.generations++
				col.generated = &text
			} else {
				return col, sub.fail("expected IDENTITY or a generation expression")
			}
		case sub.acceptKw("deferrable"):
			if key != nil {
				key.deferrable = true
			}
			last, attrsNext = key, true
		case sub.acceptKw("not", "deferrable"):
			last, attrsNext = key, true
		case sub.acceptKw("initially"):
			deferred := sub.acceptKw("deferred")
			if !deferred && !sub.acceptKw("immediate") {
				return col, sub.fail("expected DEFERRED or IMMEDIATE")
			}
			if key != nil && deferred {
				key.deferrable, key.initDeferred = true, true
			}
			last, attrsNext = key, true
		case sub.acceptKw("storage"), sub.acceptKw("compression"):
			if sub.toks[sub.i-1].value == "storage" {
				col.storage = true
			} else {
				col.compression = true
			}
			if _, err := sub.ident(); err != nil {
				return col, err
			}
		default:
			return col, sub.fail("unrecognized column constraint")
		}
		pending = nil
	}
	if col.notNull && col.explicitNull {
		return col, sub.fail("conflicting NULL/NOT NULL declarations")
	}
	if col.serial != "" && col.def != nil {
		return col, sub.fail("multiple default values for a serial column")
	}
	return col, nil
}

// skipExpression consumes a b_expr: at least one token, then up to the
// next column keyword at bracket depth 0.
func skipExpression(c *cursor) error {
	first := true
	for !c.done() {
		if !first && c.atColumnKeyword() {
			return nil
		}
		first = false
		if c.isPunct("(") || c.isPunct("[") {
			end, err := c.group()
			if err != nil {
				return err
			}
			c.i = end
			continue
		}
		c.i++
	}
	return nil
}

// skipIndexParameters consumes INCLUDE (...), WITH (...) and USING INDEX
// TABLESPACE x after PRIMARY KEY or UNIQUE. It returns the INCLUDE
// columns (nil when none were written); known is false when an INCLUDE
// list is not a plain list of column names.
func skipIndexParameters(c *cursor) (include []string, known bool, optionsWhy string) {
	known = true
	for {
		switch {
		case c.acceptKw("include"):
			save := c.i
			names, err := c.nameList()
			if err == nil {
				include = append(include, names...)
				continue
			}
			c.i, known = save, false
			if c.isPunct("(") {
				if end, err := c.group(); err == nil {
					c.i = end
				}
			}
		case c.acceptKw("with"):
			// U9: the key's index is a btree index.
			optionsWhy = "WITH (...) without a parenthesised list"
			if c.isPunct("(") {
				if end, err := c.group(); err == nil {
					optionsWhy = storageCheck("btree", storageOptions(c, c.i, end), false, c.src)
					c.i = end
				}
			}
		case c.acceptKw("using", "index", "tablespace"):
			c.ident()
		default:
			return include, known, optionsWhy
		}
	}
}

// references reads REFERENCES t [(cols)] [MATCH x] [ON DELETE a] [ON
// UPDATE a] in any order of the trailing clauses.
func references(c *cursor) (referenceDef, error) {
	var ref referenceDef
	if !c.acceptKw("references") {
		return ref, c.fail("expected REFERENCES")
	}
	t, err := c.name()
	if err != nil {
		return ref, err
	}
	ref.table = t
	if c.isPunct("(") {
		cols, err := c.nameList()
		if err != nil {
			return ref, err
		}
		ref.columns = cols
	}
	for {
		switch {
		case c.acceptKw("match"):
			if !c.acceptKw("simple") {
				if !c.acceptKw("full") && !c.acceptKw("partial") {
					return ref, c.fail("expected FULL, PARTIAL or SIMPLE")
				}
				ref.matchNotSimple = true
				ref.matchPartial = c.toks[c.i-1].value == "partial"
			}
		case c.peekKw("on", "delete") || c.peekKw("on", "update"):
			update := c.isKw(1, "update")
			c.i += 2
			listAt := -1
			if c.peekKw("set", "null") || c.peekKw("set", "default") {
				listAt = c.i + 2
			}
			action, err := referentialAction(c)
			if err != nil {
				return ref, err
			}
			if listAt >= 0 && listAt < len(c.toks) && c.toks[listAt].kind == tokPunct && c.toks[listAt].value == "(" {
				if update {
					ref.setColumnsOnUpdate = true
				} else {
					sub := &cursor{toks: c.toks, i: listAt, src: c.src}
					cols, err := sub.nameList()
					if err != nil {
						ref.setColumnsUnread = true
					}
					ref.setColumns = append(ref.setColumns, cols...)
				}
			}
			if update {
				ref.onUpdate = &action
			} else {
				ref.onDelete = &action
			}
		default:
			return ref, nil
		}
	}
}

func referentialAction(c *cursor) (string, error) {
	switch {
	case c.acceptKw("cascade"):
		return "cascade", nil
	case c.acceptKw("restrict"):
		return "restrict", nil
	case c.acceptKw("no", "action"):
		return "no action", nil
	case c.acceptKw("set", "null"):
		return "set null" + optionalColumns(c), nil
	case c.acceptKw("set", "default"):
		return "set default" + optionalColumns(c), nil
	}
	return "", c.fail("expected a referential action")
}

func optionalColumns(c *cursor) string {
	if !c.isPunct("(") {
		return ""
	}
	start := c.i
	end, err := c.group()
	if err != nil {
		return ""
	}
	c.i = end
	return " " + c.text(start, end)
}

// tableConstraint reads a table-level constraint over tokens [from, to).
func tableConstraint(c *cursor, from, to int, out *tableConstraints) error {
	sub := &cursor{toks: c.toks[:to], i: from, src: c.src}
	var name *string
	if sub.acceptKw("constraint") {
		n, err := sub.ident()
		if err != nil {
			return err
		}
		name = &n
	}
	span := c.span(from, to)
	switch {
	case sub.acceptKw("primary", "key"):
		cols, err := sub.nameList()
		if err != nil {
			return err
		}
		include, known, optionsWhy := skipIndexParameters(sub)
		out.primaryKeys = append(out.primaryKeys, constraintDef{name: name, columns: cols, span: span, include: include, includeUnknown: !known, optionsUnproven: optionsWhy})
	case sub.acceptKw("unique"):
		notDistinct := false
		if sub.acceptKw("nulls") {
			notDistinct = sub.acceptKw("not")
			sub.acceptKw("distinct")
		}
		cols, err := sub.nameList()
		if err != nil {
			return err
		}
		include, known, optionsWhy := skipIndexParameters(sub)
		out.uniques = append(out.uniques, constraintDef{name: name, columns: cols, span: span, include: include, includeUnknown: !known, optionsUnproven: optionsWhy, nullsNotDistinct: notDistinct})
	case sub.acceptKw("foreign", "key"):
		cols, err := sub.nameList()
		if err != nil {
			return err
		}
		ref, err := references(sub)
		if err != nil {
			return err
		}
		ref.name, ref.local, ref.span = name, cols, span
		if ref.columns != nil && len(ref.columns) != len(cols) {
			return sub.fail("foreign key column counts differ")
		}
		out.foreignKeys = append(out.foreignKeys, ref)
	case sub.acceptKw("check"), sub.acceptKw("exclude"):
		// No column effect; the written name is kept for DROP CONSTRAINT,
		// the identifiers for DROP COLUMN; the expression (CHECK) or body
		// (EXCLUDE) for Revision 1.4 R1.
		def := constraintDef{name: name, span: span, names: sub.identifiers(sub.i, to)}
		if sub.toks[sub.i-1].value == "exclude" {
			def.exclude = true
			def.excludeBody = sub.text(sub.i, to)
			// gram.y ConstraintElem: EXCLUDE access_method_clause '('
			// ExclusionConstraintList ')' (P7).
			ec := &cursor{toks: sub.toks[:to], i: sub.i, src: sub.src}
			if ec.acceptKw("using") {
				if _, err := ec.ident(); err != nil {
					return err
				}
			}
			if !ec.isPunct("(") {
				return ec.fail("expected ( after EXCLUDE")
			}
		}
		from := sub.i
		if !def.exclude {
			// gram.y ConstraintElem: CHECK '(' a_expr ')' ConstraintAttributeSpec
			// (NO INHERIT, NOT VALID; DEFERRABLE is refused for CHECK).
			if !sub.isPunct("(") {
				return sub.fail("expected ( after CHECK")
			}
			end, err := sub.group()
			if err != nil || end > to {
				return sub.fail("CHECK expression not read")
			}
			expr := sub.text(sub.i+1, end-1)
			def.expr = &expr
			from = end
			rest := &cursor{toks: sub.toks[:to], i: end, src: sub.src}
			for !rest.done() {
				switch {
				case rest.acceptKw("no", "inherit"):
					def.noInherit = true
				case rest.acceptKw("not", "valid"):
				default:
					return rest.fail("unexpected text after CHECK")
				}
			}
		}
		for k := from; k+1 < to; k++ {
			if sub.toks[k].kind == tokIdent && sub.toks[k].value == "not" && sub.toks[k+1].kind == tokIdent && sub.toks[k+1].value == "valid" {
				def.notValid = true
			}
		}
		out.checks = append(out.checks, def)
		sub.i = to
	default:
		return sub.fail("unrecognized table constraint")
	}
	deferrable, deferred := false, false
	for !sub.done() {
		switch {
		case sub.acceptKw("deferrable"):
			deferrable = true
		case sub.acceptKw("not", "valid"):
			if n := len(out.foreignKeys); n > 0 && out.foreignKeys[n-1].span == span {
				out.foreignKeys[n-1].notValid = true
			}
		case sub.acceptKw("not", "deferrable"):
		case sub.acceptKw("initially"):
			if sub.acceptKw("deferred") {
				deferrable, deferred = true, true
			} else {
				sub.i++
			}
		default:
			return sub.fail("unexpected text after constraint")
		}
	}
	if deferrable {
		if n := len(out.primaryKeys); n > 0 && out.primaryKeys[n-1].span == span {
			out.primaryKeys[n-1].deferrable, out.primaryKeys[n-1].initDeferred = true, deferred
		}
		if n := len(out.uniques); n > 0 && out.uniques[n-1].span == span {
			out.uniques[n-1].deferrable, out.uniques[n-1].initDeferred = true, deferred
		}
	}
	return nil
}

func isConstraintStart(c *cursor, at int) bool {
	save := c.i
	c.i = at
	defer func() { c.i = save }()
	if c.peekKw("constraint") || c.peekKw("primary", "key") || c.peekKw("unique") || c.peekKw("foreign", "key") || c.peekKw("check") {
		// CONSTRAINT, PRIMARY, UNIQUE, FOREIGN and CHECK are reserved
		// keywords (gram.y 17992, 18033, 18047, 18011, 17989): an unquoted
		// column cannot have these names.
		return true
	}
	// EXCLUDE is unreserved (gram.y 17616), so a column may be named
	// exclude; the constraint is EXCLUDE [USING m] ( ... ) (gram.y 4211,
	// access_method_clause 8164).
	return c.peekKw("exclude") && (c.isKw(1, "using") || c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(")
}

// analyze classifies one statement. functions holds the names of functions
// and procedures the set defines (R4).
//
// Revision 2: a statement form that is not modeled is opaque, unsupported
// (with its effect on known tables applied conservatively) or a parse
// error; it is no-effect only when it cannot drop, rename or alter a
// table, column, index, default or nullability.
func analyze(st statement, src []byte, functions map[string]bool, dmlOpaque bool) analysis {
	a := analyzeForm(st, src, functions, dmlOpaque)
	if a.class == classError && strings.Contains(a.reason, keywordNameError) {
		a.reason += precondition(keywordRow(st.tokens))
	}
	if a.class != classError {
		// V7 (Revision 1.6, P7): a governed row claims success only when
		// one of its gram.y productions reads the whole statement.
		why := wholeStatement(st.tokens, src)
		if why == "" && a.create != nil && a.create.unmodeled != "" && !createTableM1bWhole(st.tokens, src) {
			why = "CREATE TABLE ... LIKE / OF / PARTITION OF / INHERITS is not read completely by a gram.y production" + precondition("M1b.P7")
		}
		if why != "" {
			if id := wholeStatementID(rowOf(st.tokens, a), st.tokens); id != "" {
				why += precondition(id)
			}
			a = parseErrorAnalysis(parseError{why})
		} else if why, id := keywordPositions(st.tokens); why != "" {
			// V3 (Revision 1.6): keyword categories in element and
			// expression positions.
			a = parseErrorAnalysis(parseError{why + precondition(id)})
		}
	}
	a.row = rowOf(st.tokens, a)
	return a
}

// analyzeForm dispatches one statement to the reader of its form.
func analyzeForm(st statement, src []byte, functions map[string]bool, dmlOpaque bool) analysis {
	c := &cursor{toks: st.tokens, src: src}
	first := c.tok()
	if first.kind != tokIdent {
		if first.kind == tokPunct && first.value == "(" {
			return selectLike(c, functions, dmlOpaque)
		}
		return analysis{class: classError, opaqueKind: "parse_error", reason: "statement does not start with a keyword"}
	}
	switch first.value {
	case "create":
		return analyzeCreate(c, functions)
	case "alter":
		return analyzeAlter(c, functions)
	case "drop":
		return analyzeDrop(c)
	case "comment", "grant", "revoke", "notify", "listen", "unlisten", "lock", "checkpoint":
		// Comments, privileges, locks and notifications only.
		return analysis{class: classNoEffect}
	case "reassign":
		// REASSIGN OWNED changes owners only (gram.y ReassignOwnedStmt).
		if c.peekKw("reassign", "owned") {
			return analysis{class: classNoEffect}
		}
	case "vacuum":
		return mayFailAnalysis("VACUUM cannot run inside a transaction block")
	case "analyze", "cluster", "reindex", "refresh":
		if c.peekKw("reindex") && containsKw(c, "concurrently") {
			return mayFailAnalysis("REINDEX CONCURRENTLY cannot run inside a transaction block")
		}
		// These evaluate index expressions or a materialized view's query,
		// which may call a function the set defines (G5).
		if dmlOpaque {
			return analysis{class: classOpaque, opaqueKind: "runs_code", reason: "runs index expressions or a view query in a set that defines functions or triggers"}
		}
		return analysis{class: classNoEffect}
	case "security":
		if c.peekKw("security", "label") {
			return analysis{class: classNoEffect}
		}
	case "begin", "start", "commit", "end":
		return analysis{class: classNoEffect}
	case "rollback", "abort", "savepoint", "release":
		return analysis{class: classOpaque, opaqueKind: "transaction", reason: "transaction control may undo earlier statements"}
	case "prepare":
		if c.peekKw("prepare", "transaction") {
			return analysis{class: classOpaque, opaqueKind: "transaction", reason: "transaction control may undo earlier statements"}
		}
	case "insert", "update", "delete", "copy", "truncate", "merge":
		if strayInto(c.toks, 1) {
			return parseErrorAnalysis(parseError{"SELECT ... INTO inside a data-changing statement (gram.y, analyze.c)" + precondition("N3.P7")})
		}
		return dml(dmlOpaque)
	case "select", "values", "with", "table":
		if p, ok := setConfigPath(c.toks, c.src); ok {
			// U3: pg_dump's header form, the whole statement.
			a := searchPath("set_config changes search_path")
			a.path = p
			return a
		}
		return selectLike(c, functions, dmlOpaque)
	case "set":
		return analyzeSet(c)
	case "reset":
		c.i = 1
		if c.peekKw("all") || c.peekKw("session", "authorization") || pathVariable(c.tok()) {
			// U3: RESET search_path and RESET ALL restore E4's value (role
			// and session_authorization are GUC_NO_RESET_ALL); RESET of
			// role or session authorization changes what "$user" names.
			a := searchPath("RESET may change where unqualified names resolve")
			if c.peekKw("all") || c.tok().value == "search_path" {
				a.path = &pathChange{entries: []string{defaultEntry}}
			} else {
				a.path = &pathChange{role: true}
			}
			return a
		}
		if why := resetChecks(&cursor{toks: c.toks, i: c.i, src: c.src}); why != "" {
			return analysis{class: classNoEffect, notProven: why}
		}
		return analysis{class: classNoEffect}
	case "discard":
		if c.peekKw("discard", "all") {
			return searchPath("DISCARD ALL resets search_path")
		}
		return analysis{class: classNoEffect}
	case "do":
		return analysis{class: classOpaque, opaqueKind: "do", reason: "DO block runs code whose effect is not visible in source"}
	case "call":
		return analysis{class: classOpaque, opaqueKind: "call", reason: "CALL runs a procedure whose effect is not visible in source"}
	case "execute":
		return analysis{class: classOpaque, opaqueKind: "execute", reason: "EXECUTE runs a prepared statement"}
	}
	return analysis{class: classError, opaqueKind: "parse_error", reason: fmt.Sprintf("unrecognized statement %q", strings.ToUpper(first.value))}
}

func searchPath(reason string) analysis {
	return analysis{class: classOpaque, opaqueKind: "search_path", searchPath: true, reason: reason}
}

// mayFailAnalysis is a statement Postgres refuses inside the migration's
// transaction (sqlx runs each migration in one, unless sqlx 0.8's
// `-- no-transaction` is written, which T018a does not read).
func mayFailAnalysis(reason string) analysis {
	return analysis{class: classUnsupported, mayFail: reason, reason: reason}
}

func dml(opaque bool) analysis {
	if opaque {
		return analysis{class: classOpaque, opaqueKind: "dml", reason: "data change in a set that defines functions or triggers (a trigger may run DDL)"}
	}
	return analysis{class: classNoEffect}
}

// containsKw reports whether any token of the statement is the unquoted
// keyword w.
func containsKw(c *cursor, w string) bool {
	for _, t := range c.toks {
		if t.kind == tokIdent && t.value == w {
			return true
		}
	}
	return false
}

// pathVariable reports the settings that decide where unqualified names
// resolve: search_path, and role and session_authorization, which change
// what "$user" names.
func pathVariable(t token) bool {
	return (t.kind == tokIdent || t.kind == tokQuoted) && (t.value == "search_path" || t.value == "role" || t.value == "session_authorization")
}

// callsFunction returns the first function the set defines that the tokens
// call (a name followed by "("), or "".
func callsFunction(toks []token, functions map[string]bool) string {
	for j, t := range toks {
		if (t.kind == tokIdent || t.kind == tokQuoted) && functions[t.value] && j+1 < len(toks) && toks[j+1].kind == tokPunct && toks[j+1].value == "(" {
			return t.value
		}
	}
	return ""
}

func analyzeSet(c *cursor) analysis {
	c.i = 1
	if c.peekKw("session", "authorization") {
		a := searchPath("SET may change where unqualified names resolve")
		a.path = &pathChange{role: true}
		return a
	}
	if !c.peekKw("session", "characteristics") {
		// Revision 1.9 Y03: SET SESSION CHARACTERISTICS is read whole.
		c.acceptKw("session")
	}
	local := c.acceptKw("local")
	if a, ok := clientEncoding(c); ok {
		// V10 (Revision 1.6): SET NAMES is SET client_encoding.
		return a
	}
	if c.peekKw("schema") || pathVariable(c.tok()) {
		// U3: the new value of search_path, or a role change.
		a, ok := searchPathSet(c, local)
		if !ok {
			return parseErrorAnalysis(c.fail("a SET of search_path that gram.y does not accept"))
		}
		return a
	}
	if why := setChecks(&cursor{toks: c.toks, i: c.i, src: c.src}); why != "" {
		return analysis{class: classNoEffect, notProven: why}
	}
	if lexingVariable(c.tok()) {
		name := c.tok().value
		c.i++
		if !c.acceptKw("to") && !c.done() && c.tok().kind == tokOp && c.tok().value == "=" {
			c.i++
		}
		if c.i+1 == len(c.toks) && lexingSafe(name, c.tok()) {
			return analysis{class: classNoEffect}
		}
		return lexingChange(name)
	}
	return analysis{class: classNoEffect}
}

// lexingVariable reports the settings that change how a later query string
// is lexed (standard_conforming_strings: whether backslashes escape in
// '...' strings, scan.l 563-582) or which escapes it accepts
// (backslash_quote, scan.l 723-724). Graphvein lexes with the defaults.
func lexingVariable(t token) bool {
	return (t.kind == tokIdent || t.kind == tokQuoted) && (t.value == "standard_conforming_strings" || t.value == "backslash_quote")
}

// lexingSafe reports a value that keeps the lexing Graphvein assumes: on
// for standard_conforming_strings (the default since 9.1), on or
// safe_encoding for backslash_quote, or DEFAULT.
func lexingSafe(name string, v token) bool {
	if v.kind != tokIdent && v.kind != tokString && v.kind != tokNumber {
		return false
	}
	value := strings.ToLower(v.value)
	switch value {
	case "default", "on", "true", "yes", "1":
		return true
	case "safe_encoding":
		return name == "backslash_quote"
	}
	return false
}

// lexingChange is a SET of a lexingVariable to a value other than the
// default: Postgres parses each migration's whole query string before it
// runs any of it, so later migrations, not the rest of this one, may lex
// differently (Revision 3 G8).
func lexingChange(name string) analysis {
	return analysis{class: classOpaque, opaqueKind: "lexing_setting", lexing: name + " was changed by an earlier migration, so this file may lex differently", reason: name + " may change how later migrations are lexed"}
}

// selectLike classifies SELECT, VALUES, WITH and TABLE statements: a call
// of a set-defined function is opaque (R4), set_config('search_path', ...)
// or a set_config whose name is not a literal is a search path change
// (R2), SELECT INTO creates a table that is not modeled, a data-changing
// WITH follows the DML rule.
//
// INTO (T018b1 review): every INTO keyword at any paren depth that
// is not INSERT INTO, MERGE INTO or a column label (AS into, x.into) makes
// the statement unsupported; strings, quoted identifiers and comments are
// not keyword tokens. Postgres keeps the INTO of the leftmost SELECT, also
// inside parentheses or after a WITH clause (analyze.c
// transformOptionalSelectInto, transformSetOperationStmt), and refuses
// every other one ("INTO is only allowed on first SELECT of
// UNION/INTERSECT/EXCEPT", "SELECT ... INTO is not allowed here", a syntax
// error after FROM or ORDER BY, or with VALUES and TABLE). An INTO counts
// as the leftmost SELECT's only when it is at the depth of the leading
// parentheses, after the first SELECT there and before any FROM, WHERE,
// GROUP, HAVING, WINDOW, ORDER, LIMIT, OFFSET, FETCH, FOR, set operator or
// closing parenthesis at that depth; any other INTO may make Postgres
// refuse the statement, so the migration may fail (WF8). Its target is
// still recorded as a relation, conservatively.
func selectLike(c *cursor, functions map[string]bool, dmlOpaque bool) analysis {
	changes := false
	var into, other *qname
	intoTemp, otherTemp, refusable := false, false, false
	called := ""
	depth := 0
	lead := 0
	for lead < len(c.toks) && c.toks[lead].kind == tokPunct && c.toks[lead].value == "(" {
		lead++
	}
	selected, open := false, false
	// clause is the last clause keyword at each depth, for P7: INTO is
	// reserved, so it cannot be a table alias (gram.y alias_clause takes a
	// ColId).
	clause := map[int]string{}
	syntax := ""
	for j, t := range c.toks {
		if t.kind == tokPunct && (t.value == "(" || t.value == "[") {
			depth++
		}
		if t.kind == tokPunct && (t.value == ")" || t.value == "]") {
			depth--
			if depth < lead {
				open = false
			}
		}
		label := j > 0 && (c.toks[j-1].kind == tokIdent && c.toks[j-1].value == "as" || c.toks[j-1].kind == tokPunct && c.toks[j-1].value == ".")
		if t.kind == tokIdent && !label {
			switch t.value {
			case "select", "from", "where", "group", "having", "window", "order", "limit", "offset", "fetch", "for", "union", "intersect", "except":
				clause[depth] = t.value
			}
		}
		if t.kind == tokIdent && t.value == "into" && label && c.toks[j-1].value == "as" && clause[depth] == "from" && syntax == "" {
			syntax = "INTO is a reserved word and cannot be a table alias (gram.y alias_clause)"
		}
		if t.kind == tokIdent && depth == lead && !label {
			switch t.value {
			case "select":
				open = !selected
				selected = true
			case "from", "where", "group", "having", "window", "order", "limit", "offset", "fetch", "for", "union", "intersect", "except":
				open = false
			}
		}
		if t.kind == tokIdent && t.value == "into" && !label && !(j > 0 && c.toks[j-1].kind == tokIdent && (c.toks[j-1].value == "insert" || c.toks[j-1].value == "merge")) {
			switch {
			case depth == lead && open && into == nil:
				var after int
				into, intoTemp, after = selectIntoTarget(c, j+1)
				if into == nil {
					refusable = true
				}
				if j > 0 && c.toks[j-1].kind == tokIdent && c.toks[j-1].value == "distinct" && syntax == "" {
					syntax = "SELECT DISTINCT needs a target list before INTO (gram.y simple_select)"
				}
				if into != nil && !intoFollower(c.toks, after) && syntax == "" {
					// gram.y simple_select: into_clause is followed by
					// from_clause and the later clauses, not by DISTINCT or
					// a target list.
					syntax = "unexpected text after SELECT ... INTO's target (gram.y simple_select)"
				}
				open = false
			default:
				refusable = true
				if other == nil {
					other, otherTemp, _ = selectIntoTarget(c, j+1)
				}
			}
		}
		if t.kind == tokIdent && (t.value == "insert" || t.value == "update" || t.value == "delete" || t.value == "merge") && j > 0 {
			changes = true
		}
		if (t.kind == tokIdent || t.kind == tokQuoted) && j+1 < len(c.toks) && c.toks[j+1].kind == tokPunct && c.toks[j+1].value == "(" {
			if t.value == "set_config" {
				if j+2 >= len(c.toks) || c.toks[j+2].kind != tokString || strings.EqualFold(c.toks[j+2].value, "search_path") {
					return searchPath("set_config may change search_path")
				}
				if v := strings.ToLower(c.toks[j+2].value); v == "standard_conforming_strings" || v == "backslash_quote" {
					return lexingChange(v)
				}
			}
			if functions[t.value] && called == "" {
				called = t.value
			}
		}
	}
	if syntax != "" {
		return parseErrorAnalysis(parseError{syntax + precondition("N3.P7")})
	}
	if into != nil || refusable {
		a := analysis{class: classUnsupported, reason: "SELECT INTO creates a table that is not modeled", runsFunction: called, query: "SELECT INTO"}
		if refusable {
			a.reason = "INTO outside the first SELECT's target list: Postgres refuses the statement or creates a table that is not modeled"
			a.mayFail = "Postgres may refuse INTO outside the first SELECT"
		}
		for _, target := range []struct {
			q    *qname
			temp bool
		}{{into, intoTemp}, {other, otherTemp}} {
			switch {
			case target.q == nil:
			case target.temp:
				a.temp = target.q
			default:
				a.relations = append(a.relations, namedObject{q: *target.q, kind: "table"})
			}
		}
		return a
	}
	if called != "" {
		return analysis{class: classOpaque, opaqueKind: "function_call", reason: fmt.Sprintf("calls %s, a function defined in the migration set", called)}
	}
	if changes {
		return dml(dmlOpaque)
	}
	return analysis{class: classNoEffect}
}

// intoFollower reports whether the token at i may follow SELECT ... INTO's
// target: the end of the statement, a closing parenthesis, or a clause
// keyword after the target list (gram.y simple_select).
func intoFollower(toks []token, i int) bool {
	if i >= len(toks) {
		return true
	}
	t := toks[i]
	if t.kind == tokPunct {
		return t.value == ")" || t.value == ";"
	}
	if t.kind != tokIdent {
		return false
	}
	switch t.value {
	case "from", "where", "group", "having", "window", "order", "limit", "offset", "fetch", "for", "union", "intersect", "except":
		return true
	}
	return false
}

// strayInto reports an INTO keyword at or after from that is not INSERT
// INTO or MERGE INTO and not a column label: in a data-changing statement
// or a view's or CREATE TABLE AS's query Postgres refuses it (P7, T018b1
// review: gram.y has no into_clause there, and analyze.c refuses
// "SELECT ... INTO is not allowed here").
func strayInto(toks []token, from int) bool {
	for j := from; j < len(toks); j++ {
		t := toks[j]
		if t.kind != tokIdent || t.value != "into" || j == 0 {
			continue
		}
		p := toks[j-1]
		if p.kind == tokIdent && (p.value == "insert" || p.value == "merge" || p.value == "as") || p.kind == tokPunct && p.value == "." {
			continue
		}
		return true
	}
	return false
}

// selectIntoTarget reads [TEMPORARY | TEMP | UNLOGGED] [TABLE] name after
// SELECT ... INTO (gram.y into_clause, OptTempTableName); it also returns
// the index after the target.
func selectIntoTarget(c *cursor, at int) (*qname, bool, int) {
	sub := &cursor{toks: c.toks, i: at, src: c.src}
	temp := false
	for {
		switch {
		case sub.acceptKw("temporary"), sub.acceptKw("temp"):
			temp = true
			continue
		case sub.acceptKw("local"), sub.acceptKw("global"), sub.acceptKw("unlogged"):
			continue
		}
		break
	}
	sub.acceptKw("table")
	n, err := sub.name()
	if err != nil {
		return nil, false, sub.i
	}
	return &n, temp || isTempSchema(n.schema), sub.i
}

func analyzeCreate(c *cursor, functions map[string]bool) analysis {
	c.i = 1
	orReplace := c.acceptKw("or", "replace")
	temp := false
	for {
		switch {
		case c.acceptKw("global"), c.acceptKw("local"):
			continue
		case c.acceptKw("temp"), c.acceptKw("temporary"):
			temp = true
			continue
		}
		break
	}
	// UNLOGGED changes durability, not columns; foreign keys check it (P4).
	unlogged := c.acceptKw("unlogged")
	switch {
	case c.peekKw("table"):
		if orReplace {
			return parseErrorAnalysis(c.fail("OR REPLACE is not valid for CREATE TABLE"))
		}
		a := analyzeCreateTable(c, temp, functions)
		if a.create != nil {
			a.create.unlogged = unlogged
		}
		return a
	case c.peekKw("unique") || c.peekKw("index"):
		return analyzeCreateIndex(c, functions)
	case c.acceptKw("extension"):
		c.acceptKw("if", "not", "exists")
		n, err := c.ident()
		a := analysis{class: classOpaque, opaqueKind: "extension", reason: "an extension may create tables"}
		if err == nil {
			a.extension = &n
		}
		return a
	case c.acceptKw("function"), c.acceptKw("procedure"):
		n, err := c.funcName()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		return analysis{class: classNoEffect, function: &n.name}
	case c.peekKw("trigger"), c.peekKw("constraint", "trigger"):
		return analysis{class: classNoEffect, trigger: true}
	case c.peekKw("event", "trigger"):
		return analysis{class: classOpaque, opaqueKind: "event_trigger", trigger: true, reason: "an event trigger may run code on later DDL"}
	case c.acceptKw("sequence"):
		a := created(c, analysis{class: classNoEffect}, "sequence", temp)
		if a.class == classError {
			return a
		}
		// U5: the option list.
		st, err := seqOptionList(c, true)
		if err != nil {
			return parseErrorAnalysis(err)
		}
		if len(a.relations) == 1 {
			st.q = a.relations[0].q
			a.sequence = st
		}
		return a
	case c.peekKw("collation"), c.peekKw("policy"), c.peekKw("role"), c.peekKw("user"), c.peekKw("group"), c.peekKw("operator"), c.peekKw("cast"), c.peekKw("aggregate"), c.peekKw("statistics"), c.peekKw("publication"):
		return analysis{class: classNoEffect}
	case c.acceptKw("type"), c.acceptKw("domain"):
		kind := c.toks[c.i-1].value
		n, err := c.name()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		if kind == "type" && c.isKw(0, "as") && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(" {
			kind = "composite type"
		}
		o := namedObject{q: n, kind: kind}
		if kind == "type" && c.acceptKw("as", "enum") {
			o.labels, o.labelsUndecoded = enumLabels(c)
		}
		return analysis{class: classUnsupported, reason: "CREATE TYPE/DOMAIN is not modeled", relations: []namedObject{o}}
	case c.acceptKw("view"), c.acceptKw("recursive", "view"):
		a := created(c, analysis{class: classUnsupported, view: true, reason: "views are not modeled", query: "CREATE VIEW"}, "view", temp)
		if orReplace && len(a.relations) > 0 {
			a.relations[len(a.relations)-1].orReplace = true
		}
		return a
	case c.acceptKw("materialized", "view"):
		a := created(c, analysis{class: classUnsupported, view: true, reason: "views are not modeled", query: "CREATE MATERIALIZED VIEW"}, "materialized view", temp)
		if a.class != classError && !containsKw(c, "no") {
			// WITH DATA (the default) runs the query now.
			a.runsFunction = callsFunction(c.toks, functions)
		}
		return a
	case c.acceptKw("schema"):
		return createSchema(c)
	}
	return parseErrorAnalysis(c.fail("unrecognized CREATE statement"))
}

// created records the relation a CREATE statement names after an optional
// IF NOT EXISTS; temporary relations are not recorded.
func created(c *cursor, a analysis, kind string, temp bool) analysis {
	ifNotExists := c.acceptKw("if", "not", "exists")
	n, err := c.name()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	if a.query != "" && strayInto(c.toks, c.i) {
		// P7: views must not contain SELECT INTO (analyze.c).
		return parseErrorAnalysis(parseError{"SELECT ... INTO in the query of " + a.query + " (analyze.c)" + precondition("R1.P7")})
	}
	if !temp && !isTempSchema(n.schema) {
		a.relations = append(a.relations, namedObject{q: n, kind: kind, ifNotExists: ifNotExists})
	}
	return a
}

// createSchema records the relations of CREATE SCHEMA's elements (gram.y
// schema_stmt 1598-1606: tables, indexes, sequences, triggers, grants and
// views); the tables are not modeled. Element names take the new schema
// (gram.y 1526-1585: the schema name, else the AUTHORIZATION role name).
func createSchema(c *cursor) analysis {
	a := analysis{class: classUnsupported, reason: "CREATE SCHEMA is not modeled"}
	c.acceptKw("if", "not", "exists")
	schema, known := "", true
	if !c.peekKw("authorization") {
		n, err := c.ident()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		schema = n
	}
	if c.acceptKw("authorization") {
		role := c.tok()
		if _, err := c.ident(); err != nil {
			return parseErrorAnalysis(err)
		}
		if schema == "" {
			// CURRENT_USER, CURRENT_ROLE and SESSION_USER name a role that
			// source does not show.
			schema = role.value
			known = role.kind == tokQuoted || role.value != "current_user" && role.value != "current_role" && role.value != "session_user"
		}
	}
	depth := 0
	for j := c.i; j < len(c.toks); j++ {
		t := c.toks[j]
		if t.kind == tokPunct && (t.value == "(" || t.value == "[") {
			depth++
		}
		if t.kind == tokPunct && (t.value == ")" || t.value == "]") {
			depth--
		}
		if depth != 0 || t.kind != tokIdent || t.value != "create" {
			continue
		}
		sub := &cursor{toks: c.toks, i: j + 1, src: c.src}
		sub.acceptKw("or", "replace")
		for sub.acceptKw("temp") || sub.acceptKw("temporary") || sub.acceptKw("local") || sub.acceptKw("global") || sub.acceptKw("unlogged") || sub.acceptKw("recursive") {
		}
		kind := ""
		switch {
		case sub.acceptKw("table"):
			kind = "table"
		case sub.acceptKw("view"):
			kind = "view"
			a.query = "CREATE SCHEMA with a view element"
		case sub.acceptKw("sequence"):
			kind = "sequence"
		case sub.acceptKw("unique", "index"), sub.acceptKw("index"):
			kind = "index"
			sub.acceptKw("concurrently")
		default:
			continue
		}
		ifNotExists := sub.acceptKw("if", "not", "exists")
		if kind == "index" && sub.peekKw("on") {
			continue
		}
		n, err := sub.name()
		if err != nil {
			continue
		}
		if n.schema == nil && known {
			s := schema
			n.schema = &s
		}
		a.relations = append(a.relations, namedObject{q: n, kind: kind, ifNotExists: ifNotExists, uncertain: !known})
	}
	return a
}

func parseErrorAnalysis(err error) analysis {
	return analysis{class: classError, opaqueKind: "parse_error", reason: err.Error()}
}

func analyzeCreateTable(c *cursor, temp bool, functions map[string]bool) analysis {
	c.i++ // table
	ct := &createTable{ifNotExists: c.acceptKw("if", "not", "exists")}
	n, err := c.name()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	ct.name = n
	if temp && n.schema != nil && !isTempSchema(n.schema) {
		return mayFailAnalysis("cannot create a temporary table in a schema that is not temporary")
	}
	// A pg_temp qualification makes the table temporary (namespace.c
	// 673-678), like TEMP (Revision 2 WF9).
	if temp || isTempSchema(n.schema) {
		a := analysis{class: classUnsupported, temp: &n, reason: "temporary tables exist only for the session", tempReferences: containsKw(c, "references")}
		if c.peekKw("as") {
			a.query = "CREATE TEMP TABLE AS"
		}
		return a
	}
	// Revision 3 G4: once the header is read, a body Graphvein cannot read
	// still leaves a table of this name (partial) if Postgres accepts it.
	bodyFail := func(reason string) analysis {
		return analysis{class: classError, opaqueKind: "parse_error", reason: reason,
			create: &createTable{name: n, ifNotExists: ct.ifNotExists, unreadable: reason}}
	}
	unmodeled := func(reason string) analysis {
		return analysis{class: classUnsupported, relations: []namedObject{{q: n, kind: "table", ifNotExists: ct.ifNotExists}}, reason: reason}
	}
	// Revision 4 G-d: a table whose columns come from another relation or
	// type is still a table of this name; it is emitted partial.
	shell := func(reason string) analysis {
		return analysis{class: classUnsupported, reason: reason, create: &createTable{name: n, ifNotExists: ct.ifNotExists, unmodeled: reason}}
	}
	if c.peekKw("as") {
		if strayInto(c.toks, c.i) {
			return parseErrorAnalysis(parseError{"SELECT ... INTO in the query of CREATE TABLE AS (analyze.c)" + precondition("R1.P7")})
		}
		a := unmodeled("CREATE TABLE AS / OF / PARTITION OF is not modeled")
		a.query = "CREATE TABLE AS"
		if !containsKw(c, "no") {
			// WITH DATA (the default) runs the query now.
			a.runsFunction = callsFunction(c.toks, functions)
		}
		return a
	}
	if c.acceptKw("partition", "of") {
		a := shell("CREATE TABLE ... PARTITION OF is not modeled")
		if p, err := c.name(); err == nil {
			a.create.partitionOf = &p
			partitionByOf(c, a.create)
		}
		return a
	}
	if c.acceptKw("of") {
		a := shell("CREATE TABLE ... OF is not modeled")
		if ty, err := c.name(); err == nil {
			a.create.ofType = &ty
		}
		return a
	}
	if !c.isPunct("(") {
		return bodyFail(c.fail("expected (").Error())
	}
	end, err := c.group()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	if end-c.i > 2 {
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] >= r[1] {
				return bodyFail("empty table element")
			}
			if c.toks[r[0]].kind == tokIdent && c.toks[r[0]].value == "like" {
				a := shell("CREATE TABLE ... LIKE is not modeled")
				sub := &cursor{toks: c.toks[:r[1]], i: r[0] + 1, src: c.src}
				if src, err := sub.name(); err == nil {
					a.create.likeOf = &src
				}
				return a
			}
			if isConstraintStart(c, r[0]) {
				if err := tableConstraint(c, r[0], r[1], &ct.constraints); err != nil {
					return bodyFail(err.Error())
				}
				continue
			}
			col, err := columnDefinition(c, r[0], r[1])
			if err != nil {
				return bodyFail(err.Error())
			}
			ct.columns = append(ct.columns, col)
		}
	}
	c.i = end
	if c.acceptKw("inherits") {
		a := shell("CREATE TABLE ... INHERITS is not modeled")
		if c.acceptPunct("(") {
			for {
				p, err := c.name()
				if err != nil {
					break
				}
				a.inherits = append(a.inherits, p)
				if !c.acceptPunct(",") {
					break
				}
			}
		}
		a.inheritor = &n
		return a
	}
	// PARTITION BY, USING, WITH (...), ON COMMIT and TABLESPACE do not
	// change columns.
	withSeen := false
	for !c.done() {
		switch {
		case c.acceptKw("partition", "by"), c.acceptKw("with"), c.acceptKw("without", "oids"):
			partition := c.toks[c.i-1].value == "by"
			if c.toks[c.i-1].value == "with" {
				if c.isPunct("(") && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == ")" {
					// gram.y reloptions: at least one parameter (P7).
					return bodyFail("WITH () needs a storage parameter")
				}
				// U9 (Revision 1.5, replacing Revision 1.4 R3's "not
				// proven"): storage parameters are checked against
				// reloptions.c (pgRelopts) for the table's kind; PARTITION
				// BY precedes WITH in gram.y (OptPartitionSpec, OptWith).
				kind := "heap"
				if ct.partitionKey != nil {
					kind = "partitioned"
				}
				why := "WITH (...) without a parenthesised list"
				if c.isPunct("(") {
					if e, err := c.group(); err == nil {
						why = storageCheck(kind, storageOptions(c, c.i, e), false, c.src)
					}
				}
				if why != "" {
					ct.optionsUnproven = "WITH storage parameters (" + why + ")"
				}
				withSeen = true
			}
			if partition && withSeen {
				ct.optionsUnproven = "PARTITION BY after WITH (gram.y)"
			}
			strategy := "?"
			for _, w := range []string{"range", "list", "hash"} {
				if c.acceptKw(w) {
					strategy = w
				}
			}
			if c.isPunct("(") {
				e, err := c.group()
				if err != nil {
					return parseErrorAnalysis(err)
				}
				if partition {
					ct.partitionKey = c.identifiers(c.i+1, e-1)
					ct.partitionBy = strategy
					ct.partitionPlain = true
					ct.partitionCount = len(c.splitList(c.i+1, e-1))
					for _, r := range c.splitList(c.i+1, e-1) {
						if r[1]-r[0] != 1 || c.toks[r[0]].kind != tokIdent && c.toks[r[0]].kind != tokQuoted {
							ct.partitionPlain = false
						}
					}
				}
				c.i = e
			}
		case c.acceptKw("using"), c.acceptKw("tablespace"):
			using := c.toks[c.i-1].value == "using"
			am, err := c.ident()
			if err != nil {
				return bodyFail(err.Error())
			}
			if using && am != "heap" {
				ct.optionsUnproven = "a table access method other than heap"
			}
		case c.acceptKw("on", "commit"):
			c.i++
			c.acceptKw("rows")
			// tablecmds.c DefineRelation: "ON COMMIT can only be used on
			// temporary tables" (42P16); temporary tables are not read here.
			ct.onCommit = true
		default:
			return bodyFail(c.fail("unexpected text after table definition").Error())
		}
	}
	return analysis{class: classModeled, create: ct}
}

func analyzeCreateIndex(c *cursor, functions map[string]bool) analysis {
	ci := &createIndex{unique: c.acceptKw("unique")}
	if !c.acceptKw("index") {
		return parseErrorAnalysis(c.fail("expected INDEX"))
	}
	ci.concurrently = c.acceptKw("concurrently")
	ci.ifNotExists = c.acceptKw("if", "not", "exists")
	if !c.peekKw("on") {
		n, err := c.ident()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		ci.name = &n
	}
	if !c.acceptKw("on") {
		return parseErrorAnalysis(c.fail("expected ON"))
	}
	c.acceptKw("only")
	t, err := c.name()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	ci.table = t
	target := &t
	fail := func(err error) analysis {
		return analysis{class: classError, opaqueKind: "parse_error", target: target, reason: err.Error()}
	}
	if ci.ifNotExists && ci.name == nil {
		// P7: gram.y IndexStmt accepts IF NOT EXISTS only before a name.
		return fail(parseError{"CREATE INDEX IF NOT EXISTS requires an index name (gram.y IndexStmt)" + precondition("M4.P7")})
	}
	if c.acceptKw("using") {
		m, err := c.ident()
		if err != nil {
			return fail(err)
		}
		ci.using = &m
	}
	if !c.isPunct("(") {
		return fail(c.fail("expected ("))
	}
	elementsAt := c.i
	end, err := c.group()
	if err != nil {
		return fail(err)
	}
	for _, r := range c.splitList(c.i+1, end-1) {
		if r[0] >= r[1] {
			return fail(c.fail("empty index element"))
		}
		el := indexElement{text: c.text(r[0], r[1])}
		first := c.toks[r[0]]
		bare := first.kind == tokIdent || first.kind == tokQuoted
		if bare && r[0]+1 < r[1] {
			next := c.toks[r[0]+1]
			bare = !(next.kind == tokPunct && (next.value == "(" || next.value == ".")) && next.kind != tokOp
		}
		if bare {
			v := first.value
			el.column = &v
			j := r[0] + 1
			if j < r[1] && c.toks[j].kind == tokIdent && c.toks[j].value == "collate" {
				j++
				for j < r[1] && (c.toks[j].kind == tokIdent || c.toks[j].kind == tokQuoted || c.toks[j].kind == tokPunct && c.toks[j].value == ".") {
					j++
				}
			}
			if j < r[1] && (c.toks[j].kind == tokIdent || c.toks[j].kind == tokQuoted) && !(c.toks[j].kind == tokIdent && (c.toks[j].value == "asc" || c.toks[j].value == "desc" || c.toks[j].value == "nulls")) {
				o := c.toks[j].value
				if j+2 < r[1] && c.toks[j+1].kind == tokPunct && c.toks[j+1].value == "." {
					o += "." + c.toks[j+2].value
				}
				el.opclass = &o
			}
		} else if first.kind == tokIdent && r[0]+1 < r[1] && c.toks[r[0]+1].kind == tokPunct && c.toks[r[0]+1].value == "(" && !specialCall[first.value] {
			sub := &cursor{toks: c.toks, i: r[0] + 1, src: c.src}
			if e, err := sub.group(); err == nil && e == r[1] {
				v := first.value
				el.colname = &v
			}
		}
		for j := r[0]; j < r[1]; j++ {
			if c.toks[j].kind == tokIdent || c.toks[j].kind == tokQuoted {
				el.names = append(el.names, c.toks[j].value)
			}
		}
		ci.elements = append(ci.elements, el)
	}
	c.i = end
	for !c.done() {
		switch {
		case c.acceptKw("include"):
			cols, err := c.nameList()
			if err != nil {
				return fail(err)
			}
			ci.include = cols
		case c.acceptKw("nulls"):
			ci.nullsNotDistinct = c.acceptKw("not")
			if !c.acceptKw("distinct") {
				return fail(c.fail("expected DISTINCT"))
			}
		case c.acceptKw("with"):
			if !c.isPunct("(") {
				return fail(c.fail("expected ("))
			}
			e, err := c.group()
			if err != nil {
				return fail(err)
			}
			opts := storageOptions(c, c.i, e)
			ci.options = &opts
			c.i = e
		case c.acceptKw("tablespace"):
			if _, err := c.ident(); err != nil {
				return fail(err)
			}
		case c.acceptKw("where"):
			text := c.text(c.i, len(c.toks))
			ci.where = &text
			for _, t := range c.toks[c.i:] {
				if t.kind == tokIdent || t.kind == tokQuoted {
					ci.whereNames = append(ci.whereNames, t.value)
				}
			}
			c.i = len(c.toks)
		default:
			return fail(c.fail("unexpected text after index elements"))
		}
	}
	if ci.include == nil {
		ci.include = []string{}
	}
	// Building the index evaluates its expressions and predicate on the
	// existing rows (G5).
	return analysis{class: classModeled, index: ci, runsFunction: callsFunction(c.toks[elementsAt:], functions)}
}

func analyzeAlter(c *cursor, functions map[string]bool) analysis {
	c.i = 1
	unsupported := analysis{class: classUnsupported, reason: "ALTER of a type, domain, view or schema is not modeled"}
	switch {
	case c.acceptKw("table"):
	case c.acceptKw("sequence"):
		a := alterRelation(c, "sequence", analysis{class: classNoEffect})
		if a.class == classError || a.renamed == nil || a.renamed.newName != "" || c.done() || !seqOptionWords[c.tok().value] || c.tok().kind != tokIdent {
			return a
		}
		// U5: ALTER SEQUENCE n SeqOptList.
		st, err := seqOptionList(c, false)
		if err != nil {
			return parseErrorAnalysis(err)
		}
		st.q = a.renamed.q
		a.sequence = st
		return a
	case c.acceptKw("view"):
		return alterRelation(c, "view", unsupported)
	case c.acceptKw("materialized", "view"):
		return alterRelation(c, "materialized view", unsupported)
	case c.acceptKw("type"), c.acceptKw("domain"):
		kind := c.toks[c.i-1].value
		if kind == "type" {
			if op, ok := enumOperation(c); ok {
				return analysis{class: classModeled, enum: op}
			}
		}
		a := alterRelation(c, kind, unsupported)
		if a.class == classError {
			return a
		}
		// Columns of the type keep its written name; a rename, a new
		// schema, a changed default, NOT NULL or constraint of a domain, or
		// changed attributes change what the column is (G1). Enum values and
		// owners do not.
		if !c.peekKw("add", "value") && !c.peekKw("rename", "value") && !c.peekKw("owner", "to") {
			a.dependents = []dependentObject{{kind: "type", name: a.renamed.q.name, why: fmt.Sprintf("ALTER %s %s changes a type this column uses", strings.ToUpper(kind), a.renamed.q)}}
		}
		return a
	case c.acceptKw("collation"):
		c.acceptKw("if", "exists")
		n, err := c.name()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		a := analysis{class: classNoEffect}
		if c.peekKw("rename") || c.peekKw("set", "schema") {
			a.dependents = []dependentObject{{kind: "collation", name: n.name, why: fmt.Sprintf("ALTER COLLATION %s renames a collation this column uses", n)}}
		}
		return a
	case c.acceptKw("function"), c.acceptKw("procedure"), c.acceptKw("routine"):
		// Defaults and indexes keep using a renamed function; no column
		// changes. The new name is a function the set defines (Revision 3
		// WF-d): calls of it count as opaque, and the old name stays in the
		// set (it may be overloaded).
		if _, err := c.funcName(); err != nil {
			return parseErrorAnalysis(err)
		}
		if c.isPunct("(") {
			end, err := c.group()
			if err != nil {
				return parseErrorAnalysis(err)
			}
			c.i = end
		}
		if c.acceptKw("rename", "to") {
			m, err := c.ident()
			if err != nil {
				return parseErrorAnalysis(err)
			}
			return analysis{class: classNoEffect, function: &m}
		}
		return analysis{class: classNoEffect}
	case c.acceptKw("policy"), c.acceptKw("group"), c.acceptKw("default", "privileges"):
		return analysis{class: classNoEffect}
	case c.acceptKw("role"), c.acceptKw("user"), c.acceptKw("database"):
		return roleSetting(c)
	case c.acceptKw("extension"):
		return analysis{class: classOpaque, opaqueKind: "extension", reason: "an extension update may change tables"}
	case c.acceptKw("index"):
		ifExists := c.acceptKw("if", "exists")
		n, err := c.name()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		ai := &alterIndex{name: n, ifExists: ifExists}
		if c.acceptKw("rename", "to") {
			m, err := c.ident()
			if err != nil {
				return parseErrorAnalysis(err)
			}
			ai.newName = &m
			if !c.done() {
				return parseErrorAnalysis(c.fail("unexpected text after ALTER INDEX RENAME"))
			}
			return analysis{class: classModeled, alterIndex: ai}
		}
		if !c.peekKw("set") && !c.peekKw("reset") {
			// V7 (Revision 1.6): row M8 reads RENAME TO, and U9 SET /
			// RESET (...); any other ALTER INDEX form is row D.
			return parseErrorAnalysis(c.fail("an ALTER INDEX form row M8 does not read%s", precondition("M8.P7")))
		}
		if c.done() || c.tok().kind != tokIdent || !alterIndexWords[c.tok().value] {
			// gram.y: ALTER INDEX name takes alter_table_cmds or
			// partition_cmd (P7).
			return parseErrorAnalysis(c.fail("expected an ALTER INDEX action%s", precondition("M8.P7")))
		}
		if c.peekKw("set", "schema") {
			// gram.y has no ALTER INDEX ... SET SCHEMA (checked with psql:
			// 42601).
			return parseErrorAnalysis(c.fail("ALTER INDEX has no SET SCHEMA form%s", precondition("M8.P7")))
		}
		paren := c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "("
		if !paren && !c.peekKw("set", "tablespace") {
			// V4 kinds (2026-11-07): SET LOGGED, UNLOGGED, WITHOUT
			// CLUSTER, WITHOUT OIDS and ACCESS METHOD are table forms
			// whose ATSimplePermissions masks exclude indexes
			// (tablecmds.c 4998-5053).
			ai.tableForm = c.text(c.i, len(c.toks))
		}
		if (c.peekKw("set") || c.peekKw("reset")) && paren {
			// U9: the storage parameter list, checked by the state
			// against the index's access method.
			ai.reset = c.peekKw("reset")
			pc := &cursor{toks: c.toks, i: c.i + 1, src: c.src}
			if e, err := pc.group(); err == nil && e == len(c.toks) {
				opts := storageOptions(pc, pc.i, e)
				ai.options = &opts
			} else {
				empty := []storageOption{{}}
				ai.options = &empty
			}
		}
		return analysis{class: classUnsupported, alterIndex: ai, reason: "ALTER INDEX is not modeled"}
	case c.acceptKw("schema"):
		n, err := c.ident()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		if c.acceptKw("rename", "to") {
			m, err := c.ident()
			if err != nil {
				return parseErrorAnalysis(err)
			}
			return analysis{class: classModeled, schemaRename: &[2]string{n, m}}
		}
		if c.acceptKw("owner", "to") {
			return analysis{class: classNoEffect}
		}
		return parseErrorAnalysis(c.fail("unrecognized ALTER SCHEMA action"))
	default:
		return parseErrorAnalysis(c.fail("unrecognized ALTER statement"))
	}
	at := &alterTable{ifExists: c.acceptKw("if", "exists")}
	at.only = c.acceptKw("only")
	n, err := c.name()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	at.name = n
	// "t *" names the table and its inheritance children.
	if !c.done() && c.tok().kind == tokOp && c.tok().value == "*" {
		c.i++
	}
	target := &n
	if c.done() {
		return analysis{class: classError, opaqueKind: "parse_error", target: target, reason: "ALTER TABLE without an action"}
	}
	actionsAt := c.i
	for _, r := range c.splitList(c.i, len(c.toks)) {
		action, err := alterTableAction(c, r[0], r[1])
		if err != nil {
			return analysis{class: classError, opaqueKind: "parse_error", target: target, reason: err.Error()}
		}
		at.actions = append(at.actions, action)
	}
	if len(at.actions) > 1 {
		for i, action := range at.actions {
			if action.kind == "add_using_index" {
				// U1: USING INDEX beside another subcommand is outside
				// row M5f-u (row D).
				at.actions[i].kind, at.actions[i].reason, at.actions[i].usingIndex = "unsupported", "USING INDEX beside another subcommand is not modeled", nil
			}
			if action.sole != "" {
				// P7: RENAME, SET SCHEMA (RenameStmt, AlterObjectSchemaStmt)
				// and ATTACH/DETACH PARTITION (partition_cmd) are whole
				// statements in gram.y, not alter_table_cmd list items.
				return analysis{class: classError, opaqueKind: "parse_error", target: target, reason: action.sole + " cannot be combined with other ALTER TABLE actions (gram.y)" + precondition("M5k-b.P7")}
			}
		}
	}
	// ADD COLUMN ... DEFAULT, ADD CHECK and ALTER TYPE ... USING evaluate
	// their expressions on the existing rows (G5).
	a := analysis{class: classModeled, alter: at, runsFunction: callsFunction(c.toks[actionsAt:], functions)}
	for _, action := range at.actions {
		if action.inherit != nil {
			a.inherits, a.inheritor = append(a.inherits, *action.inherit), &n
		}
	}
	return a
}

// alterRelation reads [IF EXISTS] name [RENAME TO new] of ALTER on a
// relation or type that is not a modeled table.
func alterRelation(c *cursor, kind string, a analysis) analysis {
	ifExists := c.acceptKw("if", "exists")
	n, err := c.name()
	if err != nil {
		return parseErrorAnalysis(err)
	}
	r := &renameObject{q: n, kind: kind, ifExists: ifExists}
	if kind == "type" {
		for _, w := range []string{"add", "drop", "alter", "rename"} {
			if c.peekKw(w, "attribute") {
				r.attributes = w
			}
		}
	}
	if (kind == "view" || kind == "materialized view") && c.peekKw("rename") && !c.peekKw("rename", "to") {
		// RENAME [COLUMN] a TO b (renameatt).
		r.attributes = "rename"
	}
	if c.peekKw("rename", "to") {
		c.i += 2
		m, err := c.ident()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		r.newName = m
	}
	a.renamed = r
	return a
}

// roleSetting classifies ALTER ROLE/USER/DATABASE: a SET or RESET of
// search_path, role or session_authorization (or RESET ALL) changes where
// unqualified names resolve for later sessions, and a role rename changes
// what "$user" names (G2: R2). Other forms change no table.
func roleSetting(c *cursor) analysis {
	for j := c.i; j < len(c.toks); j++ {
		t := c.toks[j]
		if t.kind != tokIdent {
			continue
		}
		next := token{}
		if j+1 < len(c.toks) {
			next = c.toks[j+1]
		}
		switch {
		case t.value == "rename":
			a := searchPath("a renamed role changes what \"$user\" names in search_path")
			a.path = &pathChange{role: true}
			return a
		case t.value == "set" && pathVariable(next), t.value == "reset" && (pathVariable(next) || next.kind == tokIdent && next.value == "all"):
			a := searchPath("a role or database setting may change search_path for later sessions")
			a.path = &pathChange{later: true}
			return a
		case t.value == "set" && lexingVariable(next):
			return lexingChange(next.value)
		}
	}
	return analysis{class: classNoEffect}
}

// unmodeledActions are the first words of ALTER TABLE actions that
// gram.y accepts but T018a does not model (alter_table_cmd); the table
// becomes partial. Any other word is a parse error.
var unmodeledActions = map[string]bool{
	"owner": true, "enable": true, "disable": true, "set": true, "reset": true, "cluster": true,
	"inherit": true, "no": true, "of": true, "not": true, "replica": true, "force": true,
	"attach": true, "detach": true, "validate": true,
}

// m5kWord classifies an ALTER TABLE word the pass does not model (T018a2
// Revision 1, M5k-a and M5k-b): it returns the subform and, for an M5k-b
// word, what its success depends on besides the table (P4: an object or
// option the pass does not track). M5k-a words depend only on the table
// and E1: OWNER TO, SET/RESET (storage parameters), SET WITHOUT CLUSTER,
// SET WITHOUT OIDS, REPLICA IDENTITY FULL/DEFAULT/NOTHING, ENABLE/DISABLE/
// FORCE/NO FORCE ROW LEVEL SECURITY, ENABLE/DISABLE TRIGGER ALL/USER, and
// SET LOGGED/UNLOGGED (whose foreign-key pairs the state checks).
func m5kWord(c *cursor) (word, dependsOn string) {
	switch {
	case c.peekKw("owner", "to"):
		return "owner", ""
	case c.peekKw("enable", "row", "level", "security"), c.peekKw("disable", "row", "level", "security"),
		c.peekKw("force", "row", "level", "security"), c.peekKw("no", "force", "row", "level", "security"):
		return "row_security", ""
	case c.peekKw("enable", "trigger", "all"), c.peekKw("enable", "trigger", "user"),
		c.peekKw("disable", "trigger", "all"), c.peekKw("disable", "trigger", "user"):
		return "trigger_all", ""
	case c.peekKw("enable"), c.peekKw("disable"):
		if containsKw(&cursor{toks: c.toks[c.i:]}, "rule") {
			return "enable_rule", "the rule it names"
		}
		return "enable_trigger", "the trigger it names"
	case c.peekKw("set", "logged"):
		return "set_logged", ""
	case c.peekKw("set", "unlogged"):
		return "set_unlogged", ""
	case c.peekKw("set", "without", "cluster"):
		return "set_without_cluster", ""
	case c.peekKw("set", "without", "oids"):
		return "set_without_oids", ""
	case c.peekKw("set") && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(",
		c.peekKw("reset") && c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokPunct && c.toks[c.i+1].value == "(":
		return "storage_parameters", ""
	case c.peekKw("replica", "identity", "using"):
		return "replica_identity_index", "the index it names"
	case c.peekKw("replica", "identity"):
		return "replica_identity", ""
	case c.peekKw("cluster"):
		return "cluster_on", "the index it names"
	case c.peekKw("no", "inherit"):
		return "no_inherit", "the parent it names"
	case c.peekKw("of"), c.peekKw("not", "of"):
		return "of", "the typed-table type"
	case c.peekKw("detach"):
		return "detach", "the partitioned table and the partition"
	case c.peekKw("validate"):
		return "validate", "the constraint it names and its kind"
	}
	return "other", "an object or option the pass does not track"
}

// alterTableAction reads one ALTER TABLE action over tokens [from, to).
func alterTableAction(c *cursor, from, to int) (alterAction, error) {
	sub := &cursor{toks: c.toks[:to], i: from, src: c.src}
	a := alterAction{span: c.span(from, to)}
	unsupported := func(reason string) (alterAction, error) {
		a.kind, a.reason = "unsupported", reason
		return a, nil
	}
	if sub.done() {
		return a, sub.fail("empty ALTER TABLE action")
	}
	// wholeCmd applies V7 (P7) to an M5k subcommand: a form no listed
	// production reads completely is row D.
	wholeCmd := func(a alterAction, err error) (alterAction, error) {
		if !alterCmdWhole(c.toks[:to], from, c.src) {
			id := "M5k-b.P7"
			if a.dependsOn == "" {
				id = "M5k-a.P7"
			}
			return a, (&cursor{toks: c.toks[:to], i: from, src: c.src}).fail("ALTER TABLE subcommand not read completely by a gram.y production%s", precondition(id))
		}
		return a, err
	}
	cascade := func() {
		if sub.acceptKw("cascade") {
			a.cascade = true
		} else {
			sub.acceptKw("restrict")
		}
	}
	switch {
	case sub.peekKw("add"):
		sub.i++
		if u, ok := usingIndexForm(sub); ok {
			a.kind, a.usingIndex = "add_using_index", u
			return a, nil
		}
		if isConstraintStart(sub, sub.i) {
			a.kind = "add_constraint"
			if err := tableConstraint(sub, sub.i, to, &a.constraints); err != nil {
				if sub.peekKw("primary", "key", "using") || sub.peekKw("unique", "using") || sub.peekKw("constraint") {
					return unsupported("constraint form not modeled")
				}
				return a, err
			}
			return a, nil
		}
		sub.acceptKw("column")
		a.ifNotExists = sub.acceptKw("if", "not", "exists")
		col, err := columnDefinition(sub, sub.i, to)
		if err != nil {
			return a, err
		}
		a.kind, a.column = "add_column", &col
		return a, nil
	case sub.acceptKw("drop", "constraint"):
		a.ifExists = sub.acceptKw("if", "exists")
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.kind, a.name = "drop_constraint", n
		cascade()
	case sub.acceptKw("drop"):
		sub.acceptKw("column")
		a.ifExists = sub.acceptKw("if", "exists")
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.kind, a.name = "drop_column", n
		cascade()
	case sub.acceptKw("rename", "constraint"):
		a.sole = "RENAME CONSTRAINT"
		a.word, a.dependsOn = "rename_constraint", "the constraint it renames"
		return wholeCmd(unsupported("RENAME CONSTRAINT is not modeled"))
	case sub.acceptKw("rename", "to"):
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.kind, a.newName, a.sole = "rename_table", n, "RENAME TO"
	case sub.peekKw("rename"):
		a.sole = "RENAME COLUMN"
		sub.i++
		sub.acceptKw("column")
		old, err := sub.ident()
		if err != nil {
			return a, err
		}
		if !sub.acceptKw("to") {
			return a, sub.fail("expected TO")
		}
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.kind, a.name, a.newName = "rename_column", old, n
	case sub.acceptKw("alter"):
		sub.acceptKw("column")
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.name = n
		switch {
		case sub.acceptKw("set", "data", "type"), sub.acceptKw("type"):
			start := sub.i
			if err := columnType(sub); err != nil {
				return a, err
			}
			if !sub.done() && !sub.peekKw("collate") && !sub.peekKw("using") {
				return a, sub.fail("unexpected text after the column type")
			}
			a.kind, a.typ, a.typeNames = "alter_type", sub.text(start, sub.i), sub.identifiers(start, sub.i)
			if sub.acceptKw("collate") {
				cn, err := sub.name()
				if err != nil {
					return a, err
				}
				text := cn.String()
				a.collation = &text
			}
			if sub.acceptKw("using") {
				text := sub.text(sub.i, to)
				a.using = &text
				sub.i = to
			}
			if t := sub.toks[start]; (t.kind == tokIdent || t.kind == tokQuoted) && serialTypes[t.value] != "" && !strings.Contains(a.typ, " ") && !strings.Contains(a.typ, "[") {
				// parse_utilcmd.c handles serial only in column definitions;
				// ALTER COLUMN TYPE serial fails ("type does not exist").
				a.reason = "serial is not a type in ALTER COLUMN TYPE"
			}
		case sub.acceptKw("set", "not", "null"):
			a.kind = "set_not_null"
		case sub.acceptKw("drop", "not", "null"):
			a.kind = "drop_not_null"
		case sub.acceptKw("set", "default"):
			start := sub.i
			if sub.done() {
				return a, sub.fail("expected a default expression")
			}
			text := sub.text(start, to)
			a.exprNames = sub.identifiers(start, to)
			es := &cursor{toks: sub.toks, i: start, src: sub.src}
			a.exprUnread = !exprSyntax(es) || es.i != to
			sub.i = to
			a.kind, a.expr = "set_default", &text

		case sub.acceptKw("drop", "default"):
			a.kind = "drop_default"
		case sub.peekKw("add", "generated"):
			a.addIdentity = true
			a.word, a.dependsOn = "add_identity", "the column's type, nullability and default"
			return wholeCmd(unsupported("ALTER COLUMN ... ADD GENERATED AS IDENTITY is not modeled"))
		default:
			a.word, a.dependsOn = "alter_column_other", "the column's type and options"
			return wholeCmd(unsupported("ALTER COLUMN form not modeled"))
		}
	case sub.peekKw("alter", "constraint"):
		a.word, a.dependsOn = "alter_constraint", "the constraint it names and its kind"
		return wholeCmd(unsupported("ALTER CONSTRAINT is not modeled"))
	case sub.acceptKw("set", "schema"):
		n, err := sub.ident()
		if err != nil {
			return a, err
		}
		a.kind, a.newName, a.sole = "set_schema", n, "SET SCHEMA"

	case sub.peekKw("inherit"):
		sub.i++
		if p, err := sub.name(); err == nil {
			a.inherit = &p
		}
		a.word, a.dependsOn = "inherit", "the parent's columns and constraints"
		return wholeCmd(unsupported("ALTER TABLE INHERIT is not modeled"))
	case sub.peekKw("attach", "partition"):
		sub.i += 2
		if p, err := sub.name(); err == nil {
			a.attach = &p
		}
		a.word, a.dependsOn, a.sole = "attach", "the partitioned table and the partition bound", "ATTACH PARTITION"
		return wholeCmd(unsupported("ALTER TABLE ATTACH is not modeled"))
	case sub.tok().kind == tokIdent && unmodeledActions[sub.tok().value]:
		a.word, a.dependsOn = m5kWord(sub)
		if a.word == "row_security" {
			a.rls = rlsWord(sub)
		}
		if a.word == "storage_parameters" {
			// U9: the SET (...) / RESET (...) list, checked by the state
			// against the table's kind; an item that is not name [=
			// value] is a syntax error (gram.y reloption_elem, P7).
			a.reset = sub.peekKw("reset")
			pc := &cursor{toks: sub.toks, i: sub.i + 1, src: sub.src}
			e, err := pc.group()
			if err != nil || e != len(sub.toks) {
				return a, pc.fail("storage parameter list not read")
			}
			opts := storageOptions(pc, pc.i, e)
			for _, o := range opts {
				if !o.ok {
					return a, pc.fail("storage parameter not read%s", precondition("M5a.P7"))
				}
			}
			a.options = &opts
		}
		if a.word == "no_inherit" {
			pc := &cursor{toks: sub.toks, i: sub.i + 2, src: sub.src}
			if p, err := pc.name(); err == nil && pc.done() {
				a.noInherit = &p
			}
		}
		if a.word == "detach" {
			a.sole = "DETACH PARTITION"
		}
		return wholeCmd(unsupported(fmt.Sprintf("ALTER TABLE %s is not modeled", strings.ToUpper(sub.tok().value))))
	default:
		return a, sub.fail("unrecognized ALTER TABLE action")
	}
	if !sub.done() {
		return a, sub.fail("unexpected text after action")
	}
	return a, nil
}

func analyzeDrop(c *cursor) analysis {
	c.i = 1
	d := &dropObjects{}
	switch {
	case c.acceptKw("table"):
		d.kind = "table"
	case c.acceptKw("index"):
		d.kind = "index"
		d.concurrently = c.acceptKw("concurrently")
	case c.acceptKw("schema"):
		d.kind = "schema"
	case c.acceptKw("owned"):
		// DROP OWNED drops every object the roles own, with or without
		// CASCADE (gram.y DropOwnedStmt 6784-6793): any table may go.
		return analysis{class: classOpaque, opaqueKind: "drop_owned", reason: "DROP OWNED drops every object the roles own", allTables: "DROP OWNED may have dropped this table or its objects"}
	default:
		return dropOther(c)
	}
	d.ifExists = c.acceptKw("if", "exists")
	for {
		n, err := c.name()
		if err != nil {
			return parseErrorAnalysis(err)
		}
		d.names = append(d.names, n)
		if !c.acceptPunct(",") {
			break
		}
	}
	if c.acceptKw("cascade") {
		d.cascade = true
	} else {
		c.acceptKw("restrict")
	}
	if !c.done() {
		a := parseErrorAnalysis(c.fail("unexpected text after DROP"))
		if d.kind == "table" {
			a.target = &d.names[0]
		}
		return a
	}
	if d.kind == "schema" {
		return analysis{class: classUnsupported, drop: d, reason: "DROP SCHEMA is not modeled beyond its tables"}
	}
	return analysis{class: classModeled, drop: d}
}

// dropKinds are the object kinds of gram.y's DROP statements other than
// TABLE, INDEX, SCHEMA and OWNED, longest first.
var dropKinds = [][]string{
	{"materialized", "view"}, {"foreign", "data", "wrapper"}, {"foreign", "table"}, {"event", "trigger"},
	{"user", "mapping"}, {"access", "method"}, {"text", "search"}, {"procedural", "language"},
	{"operator"}, {"view"}, {"sequence"}, {"type"}, {"domain"}, {"collation"}, {"function"},
	{"procedure"}, {"routine"}, {"aggregate"}, {"extension"}, {"database"}, {"tablespace"},
	{"trigger"}, {"policy"}, {"rule"}, {"publication"}, {"subscription"}, {"statistics"},
	{"server"}, {"role"}, {"user"}, {"group"}, {"cast"}, {"conversion"}, {"language"}, {"transform"},
}

// harmlessCascade are kinds on which no table column, default, constraint
// or index can depend, so CASCADE removes only other objects. Views,
// materialized views, foreign tables (and the servers and foreign data
// wrappers that hold them) are not harmless: their row types can be column
// types (Revision 3 D1).
var harmlessCascade = map[string]bool{
	"trigger": true, "policy": true, "publication": true, "subscription": true, "statistics": true,
	"event trigger": true, "user mapping": true, "role": true, "user": true, "group": true,
}

// dropOther classifies DROP of objects other than tables, indexes, schemas
// and owned objects (Revision 3 class 1). Without CASCADE, Postgres refuses
// to drop an object that a known column, default or index uses: the state
// treats that as a refusal. With CASCADE, every table that could depend on
// a dropped object becomes partial (the state's mayDependOn rule), or every
// table when the names cannot be read or the kind is used in ways source
// does not show (operators, casts, extensions, access methods, ...).
func dropOther(c *cursor) analysis {
	kind := ""
	for _, words := range dropKinds {
		if c.acceptKw(words...) {
			kind = strings.Join(words, " ")
			break
		}
	}
	if kind == "" {
		return parseErrorAnalysis(c.fail("unrecognized DROP statement"))
	}
	cascade := containsKw(c, "cascade")
	switch kind {
	case "database", "tablespace":
		return mayFailAnalysis(fmt.Sprintf("DROP %s cannot run inside a transaction block", strings.ToUpper(kind)))
	case "extension":
		// The extension's member objects go with it, with or without
		// CASCADE; tables the extension created are not modeled.
		a := analysis{class: classOpaque, opaqueKind: "extension", reason: "DROP EXTENSION drops the objects the extension owns"}
		if cascade {
			a.allTables = "DROP EXTENSION ... CASCADE may drop columns, defaults or indexes that use the extension's objects"
		}
		return a
	}
	names := dropNames(c)
	a := analysis{class: classNoEffect}
	switch {
	case cascade:
		a = analysis{class: classOpaque, opaqueKind: "drop_cascade", reason: "DROP ... CASCADE may remove dependent columns or defaults"}
	case kind == "view" || kind == "materialized view" || kind == "type" || kind == "domain" || kind == "foreign table":
		a = analysis{class: classUnsupported, reason: "DROP of a type, domain, view or foreign table is not modeled"}
	}
	switch kind {
	case "view", "materialized view", "sequence", "type", "domain", "foreign table":
		for _, n := range names {
			a.dropped = append(a.dropped, namedObject{q: n, kind: kind})
		}
	}
	named := map[string]bool{"type": true, "domain": true, "collation": true, "function": true, "procedure": true,
		"routine": true, "aggregate": true, "sequence": true, "view": true, "materialized view": true,
		"foreign table": true, "server": true, "foreign data wrapper": true}[kind]
	switch {
	case harmlessCascade[kind]:
	case names == nil && cascade, !named && cascade:
		a.allTables = fmt.Sprintf("DROP %s ... CASCADE may remove columns, defaults or indexes that use the dropped objects", strings.ToUpper(kind))
	case names != nil:
		a.dropOthers = &dropOthers{kind: kind, names: names, cascade: cascade}
	}
	return a
}

// dropNames reads [IF EXISTS] name [(args)] [, ...]; it returns nil when the
// names cannot be read.
func dropNames(c *cursor) []qname {
	c.acceptKw("if", "exists")
	var out []qname
	for {
		n, err := c.name()
		if err != nil {
			return nil
		}
		out = append(out, n)
		if c.isPunct("(") {
			end, err := c.group()
			if err != nil {
				return nil
			}
			c.i = end
		}
		if !c.acceptPunct(",") {
			return out
		}
	}
}

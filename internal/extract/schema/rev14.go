package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Revision 1.4 (T018a2, 2026-11-07, after the independent review): class
// rules R1-R5 for what a column definition, a constraint and the rows the
// set wrote must satisfy before the pass claims a CREATE TABLE or ALTER
// TABLE succeeds. Each rule follows the governing principle: success that
// depends on something the pass does not prove is may-fail, and a
// condition the records prove false is a refusal.
//
//   - R1 (expressions, class P5): a CHECK expression is applied only when
//     the closed typer (exprs.go) proves it: its columns are held on the
//     table, its result is boolean, and it calls only proven pg_catalog
//     functions and operators (no aggregate, window function, subquery or
//     set-returning function: the typer has no production for them). An
//     EXCLUDE constraint is proven only in its btree "column WITH =" form.
//   - R2 (namespaces, class P2): constraint names are unique per table
//     across every kind (pg_constraint.c ConstraintNameIsUsed, index.c
//     index_create, heap.c AddRelationNewConstraints, tablecmds.c
//     ATAddForeignKeyConstraint); a name written twice in one statement or
//     held on the table is refused, and one that may be a generated name or
//     may be held is may-fail.
//   - R3 (column definitions, class P4): the allowlist of clause
//     combinations of parse_utilcmd.c transformColumnDefinition and
//     transformColumnType, key column lists (transformIndexConstraint) and
//     the foreign-key options of tablecmds.c ATAddForeignKeyConstraint and
//     gram.y key_actions.
//   - R4 (type modifiers, row M2, class P5): typmodCheck (types.go).
//   - R5 (known rows, class P11): rows the set's own DML wrote are known
//     rows; a statement that checks or rewrites existing rows (SET NOT
//     NULL, a validated constraint, ALTER COLUMN TYPE, a unique or
//     expression index, a column whose value each row must satisfy) is
//     claimed only when they provably satisfy it, which the pass does not
//     evaluate: may-fail.

// rowIDs are the precondition ids of R2-R5 for the rows whose column
// definitions and constraints the helpers below check: CREATE TABLE (M1),
// ADD COLUMN (M5a) and ADD CONSTRAINT (M5f).
var rowIDs = map[string]struct{ p2, p4, p5, p11 string }{
	"M1":  {"M1.P2", "M1.P4", "M1.P5", ""},
	"M5a": {"M5a.P2", "M5a.P4", "M5a.P5", "M5a.P11"},
	"M5f": {"M5f.P2", "M5f.P4", "M5f.P5", "M5f.P11"},
}

// collatableTypes are the pg_catalog types with a typcollation (pg_type.dat
// REL_17_6), and their arrays (transformColumnType: "collations are not
// supported by type", 42804).
var collatableTypes = map[string]bool{"name": true, "text": true, "bpchar": true, "varchar": true}

// columnDefinitionChecks applies R3 and R4 to one column definition of
// CREATE TABLE (row M1) or ADD COLUMN (row M5a). It returns the column type
// as Postgres stores it (R4: a time precision above 6 is stored as 6).
func (s *state) columnDefinitionChecks(t *table, def columnDef, row string) string {
	id := rowIDs[row].p4
	refuse := func(why string) {
		s.refuse(t, fmt.Sprintf("column %s: %s (Postgres refuses)%s", def.name, why, precondition(id)))
	}
	unproven := func(why string) {
		s.unproven(t, fmt.Sprintf("column %s may be refused: %s%s", def.name, why, precondition(id)))
	}
	// A serial type adds a DEFAULT (parse_utilcmd.c 625-655).
	defaults := def.defaults
	if def.serial != "" {
		defaults++
	}
	switch {
	case defaults > 1:
		refuse("more than one default (a serial type writes one)")
	case def.identities > 1:
		refuse("more than one identity specification")
	case def.generations > 1:
		refuse("more than one generation expression")
	case defaults > 0 && def.identities > 0:
		refuse("both a default (or a serial type) and an identity")
	case defaults > 0 && def.generations > 0:
		refuse("both a default (or a serial type) and a generation expression")
	case def.identities > 0 && def.generations > 0:
		refuse("both an identity and a generation expression")
	case def.primaryKeys > 1:
		refuse("PRIMARY KEY written twice (multiple primary keys)")
	case def.collations > 1:
		refuse("more than one COLLATE clause")
	}
	canon, canonOK := canonicalType(def.typ)
	base := strings.TrimPrefix(strings.TrimSuffix(canon, "[]"), "pg_catalog.")
	if i := strings.Index(base, "("); i >= 0 {
		base = base[:i]
	}
	if canonOK && strings.HasSuffix(canon, "[]") && serialTypes[strings.TrimSuffix(canon, "[]")] != "" {
		refuse("an array of serial is not implemented")
	}
	if def.identities > 0 {
		switch {
		case !canonOK:
			unproven("the type of the identity column is not read")
		case canon != "int2" && canon != "int4" && canon != "int8":
			// sequence.c init_params: the identity sequence's AS type is the
			// column type, which must be smallint, integer or bigint (a
			// domain over one is refused too).
			refuse("an identity column must be smallint, integer or bigint")
		case def.identityOptions:
			unproven("the identity's sequence options are not proven")
		}
	}
	if def.collation != nil {
		switch {
		case !canonOK || !isCatalogType(base):
			unproven("COLLATE on a type the pass does not prove collatable")
		case !collatableTypes[base]:
			refuse(fmt.Sprintf("collations are not supported by type %s", base))
		}
	}
	if def.storage || def.compression {
		unproven("STORAGE and COMPRESSION depend on the type and method, which the pass does not prove")
	}
	stored, v, why := typmodCheck(def.typ)
	if pt := s.platformColumnType(def.typ); pt != nil {
		// SB-M10: a platform extension's type and its typmod_in.
		stored = def.typ
		v, why = pt.typmod(def.typ)
		s.set.supabaseRows["SB-M10"]++
	}
	switch v {
	case typmodRefused:
		s.refuse(t, fmt.Sprintf("column %s: type %s: %s (Postgres refuses)%s", def.name, def.typ, why, precondition("M2.P5")))
	case typmodUnknown:
		if strings.Contains(def.typ, "(") || !canonOK {
			s.unproven(t, fmt.Sprintf("column %s may be refused: type %s: %s%s", def.name, def.typ, why, precondition("M2.P5")))
		}
	}
	return stored
}

// keyColumnChecks applies R3 to the column list of a primary key or UNIQUE
// constraint: a column written twice is refused (parse_utilcmd.c
// transformIndexConstraint, 42701); an INCLUDE column that repeats a key
// or INCLUDE column is not proven.
func (s *state) keyColumnChecks(t *table, k constraintDef, row string) bool {
	if k.optionsUnproven != "" {
		// U9: the key's storage parameters.
		s.unproven(t, fmt.Sprintf("key over (%s) may be refused: %s%s", strings.Join(k.columns, ", "), k.optionsUnproven, precondition(rowIDs[row].p4)))
	}
	seen := map[string]bool{}
	for _, c := range k.columns {
		if seen[c] {
			s.refuse(t, fmt.Sprintf("column %s appears twice in a key (Postgres refuses)%s", c, precondition(rowIDs[row].p4)))
			return false
		}
		seen[c] = true
	}
	for _, c := range k.include {
		if seen[c] {
			s.unproven(t, fmt.Sprintf("key over (%s) may be refused: INCLUDE repeats column %s%s", strings.Join(k.columns, ", "), c, precondition(rowIDs[row].p4)))
			return true
		}
		seen[c] = true
	}
	return true
}

// foreignKeyOptions applies R3 to the options of a foreign key whose local
// columns are resolved: MATCH PARTIAL is refused (gram.y key_match,
// 0A000); a SET NULL or SET DEFAULT column list on ON UPDATE is refused
// (gram.y key_actions, 0A000), and on ON DELETE it may name only local
// columns of the key (tablecmds.c validateFkOnDeleteSetColumns, 42P10, or
// 42703 for a column that does not exist); a generated local column with
// ON UPDATE SET NULL, SET DEFAULT or CASCADE, or ON DELETE SET NULL or SET
// DEFAULT, is refused (tablecmds.c 9755-9777, 42601).
func (s *state) foreignKeyOptions(t *table, ref referenceDef, local []*column) {
	id := "M3.P4"
	refuse := func(why string) {
		s.refuse(t, fmt.Sprintf("foreign key (%s): %s (Postgres refuses)%s", strings.Join(ref.local, ", "), why, precondition(id)))
	}
	switch {
	case ref.matchPartial:
		refuse("MATCH PARTIAL is not implemented")
		return
	case ref.setColumnsOnUpdate:
		refuse("a SET NULL or SET DEFAULT column list is supported for ON DELETE only")
		return
	case ref.setColumnsUnread:
		s.unproven(t, fmt.Sprintf("foreign key (%s) may be refused: its ON DELETE column list is not read%s", strings.Join(ref.local, ", "), precondition(id)))
		return
	}
	for _, c := range ref.setColumns {
		if !slices.Contains(ref.local, c) {
			refuse(fmt.Sprintf("column %s of the ON DELETE SET column list is not a column of the key", c))
			return
		}
	}
	action := func(p *string, words ...string) bool {
		if p == nil {
			return false
		}
		for _, w := range words {
			if strings.HasPrefix(*p, w) {
				return true
			}
		}
		return false
	}
	for _, c := range local {
		if c.generated == nil {
			continue
		}
		if action(ref.onUpdate, "set null", "set default", "cascade") || action(ref.onDelete, "set null", "set default") {
			refuse(fmt.Sprintf("generated column %s with an ON UPDATE or ON DELETE action that writes it", c.name))
			return
		}
	}
}

// checkProven applies R1 to a CHECK or EXCLUDE constraint of t (rows M1,
// M5a, M5f).
func (s *state) checkProven(t *table, ch constraintDef, row string) {
	id := rowIDs[row].p5
	if ch.exclude {
		if !s.excludeProven(t, ch.excludeBody) {
			s.unproven(t, fmt.Sprintf("EXCLUDE %s may be refused: only a btree exclusion of held columns WITH = is proven%s", ch.excludeBody, precondition(id)))
		}
		return
	}
	if ch.expr == nil {
		s.unproven(t, "a CHECK constraint whose expression is not read may be refused"+precondition(id))
		return
	}
	toks, ok := exprTokens(*ch.expr)
	e := &exprEnv{s: s, t: t, src: []byte(*ch.expr)}
	var r typed
	if ok {
		r, ok = e.prove(toks)
	}
	if ok {
		// parse_expr.c / heap.c cookConstraint: coerce_to_boolean
		// ("argument of CHECK must be type boolean", 42804); an untyped
		// constant is read as boolean.
		ok = r.null || r.any || r.typ == "bool" || r.lit != nil && e.validLiteral(*r.lit, "boolean")
	}
	if !ok {
		s.unproven(t, fmt.Sprintf("CHECK (%s) may be refused: its columns, functions and operators and its boolean result are not proven%s", *ch.expr, precondition(id)))
	}
}

// rangeTypes are pg_catalog's range types.
var rangeTypes = map[string]bool{"int4range": true, "int8range": true, "numrange": true, "tsrange": true, "tstzrange": true, "daterange": true}

// excludeProven proves EXCLUDE [USING btree] (col WITH = [, ...]), or
// USING gist over range columns WITH && or =, with optional DEFERRABLE /
// INITIALLY clauses: each column held on t, not
// generated, of a pg_catalog type with an = operator over itself (the
// default btree operator class's equality; indexcmds.c
// ComputeIndexAttrs checks that the operator is in the opfamily).
func (s *state) excludeProven(t *table, body string) bool {
	toks, ok := exprTokens(body)
	if !ok || !factsKnown(t) {
		return false
	}
	c := &cursor{toks: toks, src: []byte(body)}
	method := "btree"
	if c.acceptKw("using") {
		method = "?"
		if c.acceptKw("btree") || c.acceptKw("gist") {
			method = c.toks[c.i-1].value
		}
	}
	if method == "?" {
		return false
	}
	if !c.isPunct("(") {
		return false
	}
	end, err := c.group()
	if err != nil {
		return false
	}
	for _, r := range c.splitList(c.i+1, end-1) {
		if r[1]-r[0] != 3 {
			return false
		}
		name, with, op := toks[r[0]], toks[r[0]+1], toks[r[0]+2]
		if name.kind != tokIdent && name.kind != tokQuoted || with.kind != tokIdent || with.value != "with" || op.kind != tokOp {
			return false
		}
		col := t.column(name.value)
		if col == nil || col.generated != nil {
			return false
		}
		base, ok := normalType(writtenType(col))
		if !ok || !isCatalogType(base) {
			return false
		}
		switch method {
		case "btree":
			// U7: the column's type also needs a default btree operator
			// class (box has = but none).
			if op.value != "=" || !catalogDefault("btree", base) || !slices.ContainsFunc(pgOperators["="], func(o pgOperator) bool { return o.left == base && o.right == base }) {
				return false
			}
		case "gist":
			// The range types' default gist operator class range_ops has
			// && and = (pg_amop.dat REL_17_6).
			if !rangeTypes[base] || op.value != "&&" && op.value != "=" {
				return false
			}
		}
	}
	c.i = end
	for !c.done() {
		switch {
		case c.acceptKw("deferrable"), c.acceptKw("not", "deferrable"), c.acceptKw("initially", "deferred"), c.acceptKw("initially", "immediate"):
		default:
			return false
		}
	}
	return true
}

// constraintNameHeld reports whether t holds a constraint of this written
// name: a primary key, UNIQUE, CHECK, EXCLUDE or foreign key, including
// the foreign keys of the current ALTER TABLE that Postgres adds last.
func (s *state) constraintNameHeld(t *table, name string) bool {
	if keyNamed(t, name) {
		return true
	}
	for _, ch := range t.checks {
		if ch.name == name {
			return true
		}
	}
	for _, fk := range t.foreignKeys {
		if fk.name != nil && *fk.name == name {
			return true
		}
	}
	for _, p := range s.pending {
		if p.t == t && p.ref.name != nil && *p.ref.name == name {
			return true
		}
	}
	return false
}

// makeObjectName is indexcmds.c makeObjectName (REL_17_6): name1, an
// underscore and name2 (when set), an underscore and the label, the
// longer of the two names cut first so that the result fits NAMEDATALEN -
// 1 bytes. ok is false for a name that is not ASCII (pg_mbcliplen may cut
// it elsewhere).
func makeObjectName(name1 string, name2 *string, label string) (string, bool) {
	ascii := func(x string) bool {
		for i := 0; i < len(x); i++ {
			if x[i] >= 0x80 {
				return false
			}
		}
		return true
	}
	n1, n2, overhead := len(name1), 0, len(label)+1
	if name2 != nil {
		n2 = len(*name2)
		overhead++
		if !ascii(*name2) {
			return "", false
		}
	}
	if !ascii(name1) {
		return "", false
	}
	avail := maxIdentifier - overhead
	for n1+n2 > avail {
		if n1 > n2 {
			n1--
		} else {
			n2--
		}
	}
	out := name1[:n1]
	if name2 != nil {
		out += "_" + (*name2)[:n2]
	}
	return out + "_" + label, true
}

// nameAddition is indexcmds.c ChooseIndexNameAddition and tablecmds.c
// ChooseForeignKeyConstraintNameAddition: the column names joined by
// underscores, stopped once NAMEDATALEN bytes are reached.
func nameAddition(cols []string) string {
	var b strings.Builder
	for _, c := range cols {
		if b.Len() > 0 {
			b.WriteByte('_')
		}
		if len(c) > maxIdentifier {
			c = c[:maxIdentifier]
		}
		b.WriteString(c)
		if b.Len() >= maxIdentifier+1 {
			break
		}
	}
	return b.String()
}

// mayBeChosen reports whether ChooseConstraintName or ChooseRelationName
// (label, then label1, label2, ...) may give name to an unnamed object
// whose name parts are name1 and name2: the name is one of the
// candidates. A candidate is taken only when the earlier ones are in use,
// which the pass does not track, so every candidate counts.
func mayBeChosen(name, name1 string, name2 *string, label string) bool {
	for n := 0; n <= 1000; n++ {
		l := label
		if n > 0 {
			l = fmt.Sprintf("%s%d", label, n)
		}
		c, ok := makeObjectName(name1, name2, l)
		if !ok || c == name {
			return true
		}
		if len(name) < len(c) && n > 0 {
			break
		}
	}
	return false
}

// statementGenerated reports the suffix when name may be the generated
// name of an unnamed constraint of tc on t (the generated names of one
// statement are chosen while it runs, so an explicit name may meet one).
// The names are computed as Postgres computes them: <table>_pkey;
// <table>_<columns>_key, _fkey and _excl; <table>_<column>_check for a
// CHECK over one column and <table>_check otherwise (heap.c
// AddRelationNewConstraints counts the distinct columns of the expression;
// every identifier of the expression that names a column is tried).
func statementGenerated(t *table, name string, tc tableConstraints, columnNames []string) string {
	rel := t.q.name
	for _, k := range tc.primaryKeys {
		if k.name == nil && mayBeChosen(name, rel, nil, "pkey") {
			return "pkey"
		}
	}
	for _, k := range tc.uniques {
		if a := nameAddition(k.columns); k.name == nil && mayBeChosen(name, rel, &a, "key") {
			return "key"
		}
	}
	for _, f := range tc.foreignKeys {
		if a := nameAddition(f.local); f.name == nil && mayBeChosen(name, rel, &a, "fkey") {
			return "fkey"
		}
	}
	for _, ch := range tc.checks {
		if ch.name != nil {
			continue
		}
		if ch.exclude {
			if generatedShapeOf(rel, name, "excl") {
				return "excl"
			}
			continue
		}
		if mayBeChosen(name, rel, nil, "check") {
			return "check"
		}
		for _, id := range ch.names {
			if slices.Contains(columnNames, id) && mayBeChosen(name, rel, &id, "check") {
				return "check"
			}
		}
	}
	return ""
}

// generatedShapeOf is the coarse shape test of generatedShape for one
// suffix: <rel>_..._<suffix>[digits], or any name of maximum length.
func generatedShapeOf(rel, name, suffix string) bool {
	head, ok := strings.CutSuffix(strings.TrimRight(name, "0123456789"), "_"+suffix)
	return ok && head != "" && (len(name) >= maxIdentifier || strings.HasPrefix(head, rel+"_"))
}

// constraintNamespace applies R2 to the constraints one CREATE TABLE (row
// M1) or ALTER TABLE subcommand (rows M5a, M5f) writes, after Postgres's
// merging of equal index constraints.
func (s *state) constraintNamespace(t *table, tc tableConstraints, altering bool, row string) {
	id := rowIDs[row].p2
	type named struct {
		name    string
		indexed bool
	}
	var written []named
	for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
		if k.name != nil {
			written = append(written, named{*k.name, true})
		}
	}
	for _, ch := range tc.checks {
		if ch.name != nil {
			written = append(written, named{*ch.name, ch.exclude})
		}
	}
	for _, f := range tc.foreignKeys {
		if f.name != nil {
			written = append(written, named{*f.name, false})
		}
	}
	seen := map[string]bool{}
	for _, n := range written {
		if seen[n.name] {
			s.refuse(t, fmt.Sprintf("constraint name %s is written twice (Postgres refuses: 42710, or 42P07 for two indexes)%s", n.name, precondition(id)))
			return
		}
		seen[n.name] = true
	}
	for _, n := range written {
		if altering && s.constraintNameHeld(t, n.name) {
			s.refuse(t, fmt.Sprintf("constraint %s already exists on table %s (Postgres refuses, 42710)%s", n.name, t.q, precondition(id)))
			return
		}
		if altering && !constraintNamesExact(t) {
			s.unproven(t, fmt.Sprintf("CONSTRAINT %s may be refused: the constraint names of table %s are not all known%s", n.name, t.q, precondition(id)))
			continue
		}
		var columnNames []string
		for _, c := range t.columns {
			columnNames = append(columnNames, c.name)
		}
		if suf := statementGenerated(t, n.name, tc, columnNames); suf != "" {
			s.unproven(t, fmt.Sprintf("CONSTRAINT %s may be refused: it may be the generated name of an unnamed constraint (%s) of the same statement%s", n.name, suf, precondition(id)))
			continue
		}
		if !altering {
			if why := s.generatedConstraintName(t, n.name, n.indexed); why != "" {
				s.unproven(t, fmt.Sprintf("CONSTRAINT %s may be refused: %s%s", n.name, why, precondition(id)))
			}
		}
	}
}

// partitionedChecks applies R3 to CREATE TABLE ... PARTITION BY (row M1):
// the partition key is proven only as a list of held, non-generated
// columns with a btree operator class for RANGE or LIST (HASH and
// expressions are not proven), the table is not UNLOGGED (0A000), and
// every primary key and UNIQUE constraint includes every key column
// (0A000, indexcmds.c DefineIndex); EXCLUDE is not proven.
func (s *state) partitionedChecks(t *table, ct *createTable) {
	if ct.partitionBy == "" {
		return
	}
	fail := func(why string) {
		s.unproven(t, fmt.Sprintf("CREATE TABLE %s PARTITION BY may be refused: %s%s", ct.name, why, precondition("M1.P4")))
	}
	if ct.unlogged {
		s.refuse(t, "a partitioned table cannot be unlogged (Postgres refuses)"+precondition("M1.P4"))
		return
	}
	if listKeyRefused(ct) {
		// Refused by createTable.
		return
	}
	if ct.partitionBy != "range" && ct.partitionBy != "list" || !ct.partitionPlain {
		fail("only RANGE or LIST over plain columns is proven")
		return
	}
	for _, n := range ct.partitionKey {
		c := t.column(n)
		if c == nil || c.generated != nil {
			fail(fmt.Sprintf("partition column %s is not a held, stored column", n))
			return
		}
		if base, ok := normalType(writtenType(c)); !ok || !isCatalogType(base) || !btreeType(base) {
			fail(fmt.Sprintf("partition column %s has no proven btree operator class", n))
			return
		}
	}
	tc := ct.constraints
	for _, def := range ct.columns {
		tc = mergeConstraints(columnConstraintSet(def), tc)
	}
	for _, k := range append(slices.Clone(tc.primaryKeys), tc.uniques...) {
		for _, n := range ct.partitionKey {
			if !slices.Contains(k.columns, n) {
				fail(fmt.Sprintf("a key over (%s) does not include partition column %s", strings.Join(k.columns, ", "), n))
				return
			}
		}
	}
	for _, ch := range tc.checks {
		if ch.exclude {
			fail("an EXCLUDE constraint on a partitioned table is not proven")
			return
		}
	}
}

// listKeyRefused reports PARTITION BY LIST over more than one element,
// which Postgres refuses (tablecmds.c transformPartitionSpec, 42P17).
func listKeyRefused(ct *createTable) bool {
	return ct.partitionBy == "list" && ct.partitionCount > 1
}

// rowsChecked applies R5: a statement that checks or rewrites the rows of
// t is claimed only when t holds no rows the set's own DML wrote (rows from
// outside the set are E2). The pass does not evaluate those rows.
func (s *state) rowsChecked(t *table, what, id string) {
	if t != nil && s.familySetRows(t) {
		s.unproven(t, fmt.Sprintf("%s may be refused: the table holds rows the set's own statements wrote, which the pass does not prove satisfy it%s", what, precondition(id)))
	}
}

// columnChecksRows reports whether an ADD COLUMN definition makes every
// existing row satisfy something (R5): NOT NULL, a key, CHECK, REFERENCES,
// a generation expression, or an identity.
func columnChecksRows(def columnDef) bool {
	nullDefault := def.def == nil || strings.EqualFold(strings.TrimSpace(*def.def), "null")
	notNull := def.notNull && def.identity == nil && def.serial == "" && nullDefault
	return notNull || def.primaryKey != nil || len(def.unique) > 0 || len(def.checks) > 0 || len(def.references) > 0 || def.generated != nil
}

// createDomain applies R1, R3 and R4 to CREATE DOMAIN n [AS] type
// [constraints] (row R1, typecmds.c DefineDomain REL_17_6): the base
// type's modifiers (typmodCheck); at most one DEFAULT (42601 "multiple
// default expressions"), assignable to the base type; NULL and NOT NULL
// not both (42601); COLLATE only on a collatable base type (42804); each
// CHECK proven boolean by the typer with VALUE of the base type. Anything
// else (another clause, a base type the pass cannot read) is not proven.
func (s *state) createDomain(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	c.i = 2
	n, err := c.name()
	if err != nil {
		return
	}
	what := "CREATE DOMAIN " + n.String()
	fail := func(why string) {
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, why, precondition("R1.P5")))
	}
	refuse := func(why string) {
		s.mayFail(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, why, precondition("R1.P5")))
	}
	c.acceptKw("as")
	start := c.i
	if err := columnType(c); err != nil {
		fail("its base type is not read")
		return
	}
	typ := c.text(start, c.i)
	switch _, v, why := typmodCheck(typ); v {
	case typmodRefused:
		refuse(why)
		return
	case typmodUnknown:
		fail(why)
		return
	}
	base, ok := normalType(typ)
	if !ok || !isCatalogType(base) {
		fail("its base type is not a pg_catalog type")
		return
	}
	fake := &table{q: qname{name: n.name}, columns: []*column{{name: "value", typ: typ}}}
	defaults, null, notNull := 0, false, false
	for !c.done() {
		if c.isPunct(";") {
			break
		}
		if c.acceptKw("constraint") {
			if _, err := c.ident(); err != nil {
				fail("a constraint is not read")
				return
			}
			continue
		}
		switch {
		case c.acceptKw("not", "null"):
			notNull = true
		case c.acceptKw("null"):
			null = true
		case c.acceptKw("collate"):
			if _, err := c.name(); err != nil {
				fail("COLLATE is not read")
				return
			}
			if !collatableTypes[strings.TrimSuffix(base, "[]")] {
				refuse(fmt.Sprintf("collations are not supported by type %s", base))
				return
			}
		case c.acceptKw("default"):
			from := c.i
			if err := skipExpression(c); err != nil {
				fail("DEFAULT is not read")
				return
			}
			defaults++
			text := c.text(from, c.i)
			etoks, ok := exprTokens(text)
			e := &exprEnv{s: s, src: []byte(text)}
			var r typed
			if ok {
				r, ok = e.prove(etoks)
			}
			if !ok || !e.assignable(r, typ) {
				fail(fmt.Sprintf("DEFAULT %s is not proven assignable to %s", text, typ))
				return
			}
		case c.acceptKw("check"):
			if !c.isPunct("(") {
				fail("CHECK is not read")
				return
			}
			end, err := c.group()
			if err != nil {
				fail("CHECK is not read")
				return
			}
			text := c.text(c.i+1, end-1)
			c.i = end
			etoks, ok := exprTokens(text)
			e := &exprEnv{s: s, t: fake, src: []byte(text)}
			var r typed
			if ok {
				r, ok = e.prove(etoks)
			}
			if !ok || !(r.null || r.any || r.typ == "bool" || r.lit != nil && e.validLiteral(*r.lit, "boolean")) {
				fail(fmt.Sprintf("CHECK (%s) is not proven boolean over VALUE", text))
				return
			}
		default:
			fail("a clause is not proven")
			return
		}
	}
	switch {
	case defaults > 1:
		refuse("more than one DEFAULT")
	case null && notNull:
		refuse("both NULL and NOT NULL")
	}
}

// createCompositeType applies R3 and R4 to the attributes of CREATE TYPE n
// AS (attr type [COLLATE c], ...) (row R1, typecmds.c DefineCompositeType
// through DefineRelation): each type's modifiers and collation as for a
// column. Other CREATE TYPE forms are left to their rows.
func (s *state) createCompositeType(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	c.i = 2
	n, err := c.name()
	if err != nil || !c.acceptKw("as") || !c.isPunct("(") {
		return
	}
	end, err := c.group()
	if err != nil {
		return
	}
	what := "CREATE TYPE " + n.String()
	if end-c.i <= 2 {
		return
	}
	for _, r := range c.splitList(c.i+1, end-1) {
		sub := &cursor{toks: c.toks[:r[1]], i: r[0], src: c.src}
		if _, err := sub.ident(); err != nil {
			continue
		}
		start := sub.i
		if err := columnType(sub); err != nil {
			continue
		}
		typ := sub.text(start, sub.i)
		switch _, v, why := typmodCheck(typ); v {
		case typmodRefused:
			s.mayFail(fmt.Sprintf("%s: attribute type %s: %s (Postgres refuses)%s", what, typ, why, precondition("R1.P5")))
			return
		case typmodUnknown:
			if strings.Contains(typ, "(") {
				s.unknownFact(fmt.Sprintf("%s may be refused: attribute type %s: %s%s", what, typ, why, precondition("R1.P5")))
				return
			}
		}
		if sub.acceptKw("collate") {
			base, ok := normalType(typ)
			base = strings.TrimSuffix(base, "[]")
			switch {
			case !ok || !isCatalogType(base):
				s.unknownFact(fmt.Sprintf("%s may be refused: COLLATE on attribute type %s is not proven%s", what, typ, precondition("R1.P5")))
				return
			case !collatableTypes[base]:
				s.mayFail(fmt.Sprintf("%s: collations are not supported by type %s (Postgres refuses)%s", what, base, precondition("R1.P5")))
				return
			}
		}
	}
}

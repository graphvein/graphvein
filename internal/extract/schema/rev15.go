package schema

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// T018a2 Revision 1.5 class rules U7-U10 (types, operator classes,
// collations, settings, keywords in name positions, storage parameters and
// system column names). The tables they read are generated into
// pgtables.go by TestGeneratePgTables from REL_17_6 sources.

// pgOpclass is one operator class: its access method, name, input type and
// whether it is the method's default for that type (pg_opclass.dat, or a
// contrib script's CREATE OPERATOR CLASS ... DEFAULT).
type pgOpclass struct {
	method, name, intype string
	isDefault            bool
}

// pgGUC is one run-time parameter of guc_tables.c (U7): its context
// (user, superuser, postmaster, ...), type (b bool, i integer, r real,
// s string, e enum), integer or real bounds ("" when the source bound is
// not a constant), whether it takes units, its enum values, whether a
// check hook validates values, whether it is compiled only under a
// preprocessor condition, and whether RESET is refused (GUC_NO_RESET).
type pgGUC struct {
	context     string
	typ         byte
	min, max    string
	unit        bool
	enum        []string
	check       bool
	conditional bool
	noReset     bool
}

// pgRelopt is one storage parameter definition of reloptions.c (U9): the
// relation kinds it applies to (heap, toast, btree, hash, gin, gist,
// spgist, brin, view, partitioned, ...), its type (b, i, r, e, s), its
// bounds and its enum values.
type pgRelopt struct {
	kinds    []string
	typ      byte
	min, max string
	enum     []string
}

// systemColumns are the system column names of heap.c REL_17_6
// (SysAtt, SystemAttributeByName 253): every table, partitioned table,
// materialized view and foreign table holds them in its column namespace,
// so a column definition or rename target with one is refused (42701,
// CheckAttributeNamesTypes 457, check_for_column_name_collision 450; U10,
// P2). oid is no longer a system column.
var systemColumns = map[string]bool{"ctid": true, "xmin": true, "cmin": true, "xmax": true, "cmax": true, "tableoid": true}

// usingIndex is the subcommand of row M5f-u (U1): ALTER TABLE [ONLY] t ADD
// [CONSTRAINT n] PRIMARY KEY | UNIQUE USING INDEX i [constraint
// attributes].
type usingIndex struct {
	// constraint is the written constraint name n, nil when unnamed.
	constraint *string
	index      string
	primary    bool
	// attributes is set when DEFERRABLE, INITIALLY ... or NOT DEFERRABLE
	// is written (not proven: U1's P4 checks them against the index).
	attributes bool
}

// usingIndexForm reads the M5f-u subcommand after ADD (the cursor stands
// after ADD and is moved only when the whole action is the form). Any
// other text (a column list, EXCLUDE, an unknown attribute) is not the
// form, and the caller reads it as before (row D).
func usingIndexForm(c *cursor) (*usingIndex, bool) {
	sub := &cursor{toks: c.toks, i: c.i, src: c.src}
	u := &usingIndex{}
	if sub.acceptKw("constraint") {
		n, err := sub.ident()
		if err != nil {
			return nil, false
		}
		u.constraint = &n
	}
	switch {
	case sub.acceptKw("primary", "key"):
		u.primary = true
	case sub.acceptKw("unique"):
	default:
		return nil, false
	}
	if !sub.acceptKw("using", "index") {
		return nil, false
	}
	n, err := sub.ident()
	if err != nil {
		return nil, false
	}
	u.index = n
	for !sub.done() {
		switch {
		case sub.acceptKw("deferrable"), sub.acceptKw("not", "deferrable"),
			sub.acceptKw("initially", "deferred"), sub.acceptKw("initially", "immediate"):
			u.attributes = true
		default:
			return nil, false
		}
	}
	c.i = sub.i
	return u, true
}

// storageOption is one written storage parameter of a WITH (...) or SET
// (...) / RESET (...) list (U9): its name (with a "toast." or other
// namespace prefix kept), its value token when one is written, and ok
// false when the item could not be read as name [= value].
type storageOption struct {
	name     string
	value    *token
	negative bool
	ok       bool
}

// storageOptions reads the parenthesised list at toks[from] (the opening
// parenthesis) up to toks[to-1] (the closing one).
func storageOptions(c *cursor, from, to int) []storageOption {
	var out []storageOption
	inner := &cursor{toks: c.toks[:to-1], i: from + 1, src: c.src}
	for _, r := range inner.splitList(from+1, to-1) {
		toks := c.toks[r[0]:r[1]]
		o := storageOption{}
		i := 0
		if i < len(toks) && (toks[i].kind == tokIdent || toks[i].kind == tokQuoted) {
			o.name = toks[i].value
			i++
			if i+1 < len(toks) && toks[i].kind == tokPunct && toks[i].value == "." && (toks[i+1].kind == tokIdent || toks[i+1].kind == tokQuoted) {
				o.name += "." + toks[i+1].value
				i += 2
			}
			o.ok = true
		}
		if o.ok && i < len(toks) {
			o.ok = toks[i].kind == tokOp && toks[i].value == "="
			i++
			if o.ok && i < len(toks) && toks[i].kind == tokOp && (toks[i].value == "-" || toks[i].value == "+") {
				o.negative = toks[i].value == "-"
				i++
			}
			if o.ok && i == len(toks)-1 {
				v := toks[i]
				o.value = &v
			} else {
				o.ok = false
			}
		}
		out = append(out, o)
	}
	return out
}

// addUsingIndex applies row M5f-u (U1): ADD [CONSTRAINT n] PRIMARY KEY |
// UNIQUE USING INDEX i as the statement's only subcommand. Postgres turns
// the index into the constraint's index and renames it to n when n is
// written and differs (tablecmds.c ATExecAddIndexConstraint 9298-9312);
// it refuses an index that is not a plain unique btree index over columns
// with default ordering (parse_utilcmd.c transformIndexConstraint
// 2255-2390), a second primary key and a NULLS NOT DISTINCT index under a
// primary key (index.c index_check_primary_key), and every form on a
// partitioned table (tablecmds.c 9283-9286). Every property the pass does
// not prove makes the migration one that may fail.
func (s *state) addUsingIndex(t *table, a alterAction, certain bool) {
	u := a.usingIndex
	name := u.index
	if u.constraint != nil {
		name = *u.constraint
	}
	what := fmt.Sprintf("ADD CONSTRAINT %s USING INDEX %s", name, u.index)
	notProven := func(why, id string) {
		s.partial(t, what+" is not proven")
		s.unproven(t, fmt.Sprintf("%s may be refused: %s%s", what, why, precondition(id)))
	}
	refused := func(why, id string) {
		s.refuse(t, fmt.Sprintf("%s: %s (Postgres refuses)%s", what, why, precondition(id)))
	}
	if t.partitionKey != nil {
		refused("the table is partitioned", "M5f-u.P4")
		return
	}
	owner, ix := s.findIndex(qname{schema: t.q.schema, name: u.index})
	switch {
	case owner != nil && owner != t && certain && !ix.uncertain:
		refused(fmt.Sprintf("index %s belongs to table %s", u.index, owner.q), "M5f-u.P4")
		return
	case owner != t || ix.uncertain || ix.mayBeGone:
		notProven(fmt.Sprintf("index %s is not held under a certain name on %s", u.index, t.q), "M5f-u.P1")
		return
	case !certain || !factsKnown(t) || t.kindUnknown || t.mayBeOther:
		notProven(fmt.Sprintf("table %s is partial or not proven a plain table", t.q), "M5f-u.P4")
		return
	}
	cols := make([]*column, len(ix.elements))
	plain := ix.where == nil && len(ix.include) == 0 && (ix.using == nil || strings.EqualFold(*ix.using, "btree"))
	for i, el := range ix.elements {
		cols[i] = ix.columns[i]
		if el.column == nil || cols[i] == nil || el.text != *el.column && el.text != quoteIdent(*el.column) {
			plain = false
		}
	}
	switch {
	case !ix.unique:
		refused(fmt.Sprintf("index %s is not unique", u.index), "M5f-u.P4")
		return
	case ix.where != nil:
		refused(fmt.Sprintf("index %s is partial", u.index), "M5f-u.P4")
		return
	case !plain:
		notProven(fmt.Sprintf("index %s is not proven a btree index over plain columns with default ordering", u.index), "M5f-u.P4")
		return
	case ix.concurrently:
		notProven(fmt.Sprintf("index %s was built CONCURRENTLY, so its validity is not proven", u.index), "M5f-u.P4")
		return
	case u.attributes:
		notProven("constraint attributes against the index are not modeled", "M5f-u.P4")
		return
	case u.primary && ix.nullsNotDistinct:
		refused(fmt.Sprintf("index %s is NULLS NOT DISTINCT", u.index), "M5f-u.P4")
		return
	case u.primary && t.primaryKey != nil:
		refused("a second primary key", "M5f-u.P4")
		return
	case u.primary && t.pkMaybe:
		notProven("a primary key may still exist", "M5f-u.P4")
		return
	}
	if s.constraintNameHeld(t, name) {
		refused(fmt.Sprintf("constraint %s already exists on table %s", name, t.q), "M5f-u.P2")
		return
	}
	if !constraintNamesExact(t) {
		notProven(fmt.Sprintf("the constraint names of table %s are not all known", t.q), "M5f-u.P2")
		return
	}
	if name != u.index {
		q := qname{schema: t.q.schema, name: name}
		refusedName, maybe := s.nameTaken(q, nil)
		if refusedName == "" && maybe == "" {
			maybe = s.generatedRelationName(q)
		}
		switch {
		case refusedName != "":
			refused(fmt.Sprintf("the index cannot take the name %s: %s", name, refusedName), "M5f-u.P2")
			return
		case maybe != "":
			notProven(fmt.Sprintf("the index may not take the name %s: %s", name, maybe), "M5f-u.P2")
			return
		}
	}
	if u.primary {
		for _, c := range cols {
			if c.nullable && s.familySetRows(t) {
				// The NOT NULL the key adds checks the rows the set wrote.
				notProven(fmt.Sprintf("column %s is nullable and the table may hold rows the set wrote", c.name), "M5f-u.P11")
				return
			}
		}
	}
	s.touch(t)
	t.indexes = slices.DeleteFunc(t.indexes, func(x *index) bool { return x == ix })
	k := &key{name: &name, cols: cols}
	if u.primary {
		t.primaryKey = k
		for _, c := range cols {
			c.nullable = false
		}
		return
	}
	t.uniques = append(t.uniques, k)
}

// storageCheck applies U9 (P5, values) to a storage parameter list of a
// relation of the given reloptions kind (heap, partitioned, btree, hash,
// gist, gin, spgist, brin; "" when the kind is not known): it returns ""
// when every parameter is proven (pgRelopts, reloptions.c REL_17_6) and
// otherwise why the list is refused or not proven. reset is a RESET list,
// which Postgres accepts for any name without a value.
func storageCheck(kind string, opts []storageOption, reset bool, src []byte) string {
	if len(opts) == 0 {
		return "an empty storage parameter list (gram.y)"
	}
	if kind == "" {
		return "the relation's kind is not known"
	}
	seen := map[string]bool{}
	for _, o := range opts {
		switch {
		case !o.ok:
			return "a storage parameter that is not name [= value]"
		case reset && o.value != nil:
			return fmt.Sprintf("RESET %s with a value (Postgres refuses)", o.name)
		case reset:
			continue
		case kind == "partitioned":
			return "storage parameters on a partitioned table (Postgres refuses, 42809)"
		case strings.Contains(o.name, "."):
			return fmt.Sprintf("%s is validated only when the table has a TOAST table", o.name)
		case seen[o.name]:
			return fmt.Sprintf("parameter %s is written twice (Postgres refuses, 22023)", o.name)
		}
		seen[o.name] = true
		var def *pgRelopt
		for i, d := range pgRelopts[o.name] {
			if slices.Contains(d.kinds, kind) {
				def = &pgRelopts[o.name][i]
			}
		}
		if def == nil {
			return fmt.Sprintf("%s is not a storage parameter of a %s relation (Postgres refuses, 22023)", o.name, kind)
		}
		if why := reloptValue(*def, o, src); why != "" {
			return fmt.Sprintf("%s: %s", o.name, why)
		}
	}
	return ""
}

// reloptValue checks one value against its definition: a boolean word
// (or no value) for a bool option, an integer constant within bounds for
// an int option, a numeric constant within bounds for a real option, an
// enum value; a fraction for an integer option is not proven (Postgres
// accepts some), and every other form is refused or not proven.
func reloptValue(d pgRelopt, o storageOption, src []byte) string {
	if o.value == nil {
		if d.typ == 'b' {
			return ""
		}
		return "a value is required (Postgres refuses)"
	}
	text, ok := tokenText(*o.value, src)
	if !ok {
		return "the value is not a constant"
	}
	if o.negative {
		text = "-" + text
	}
	switch d.typ {
	case 'b':
		if _, ok := parseBool(text); ok && !o.negative {
			return ""
		}
		return "not a boolean (Postgres refuses)"
	case 'i', 'r':
		return numberInBounds(text, d.typ == 'i', d.min, d.max)
	case 'e':
		if !o.negative && slices.Contains(d.enum, strings.ToLower(text)) {
			return ""
		}
		return "not one of the option's values (Postgres refuses)"
	}
	return "string options are not proven"
}

// tokenText is the text of an identifier, number or string constant.
func tokenText(t token, src []byte) (string, bool) {
	switch t.kind {
	case tokIdent, tokNumber:
		return t.value, true
	case tokString:
		return literalValue(t, src)
	}
	return "", false
}

// numberInBounds checks a numeric value against bounds ("" unknown): ""
// when proven, otherwise why it is refused or not proven.
func numberInBounds(text string, integer bool, min, max string) string {
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return "not a number (Postgres refuses)"
	}
	if integer {
		if _, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64); err != nil {
			return "not an integer constant (not proven)"
		}
	}
	if min == "" || max == "" {
		return "the bounds are not known"
	}
	lo, _ := strconv.ParseFloat(min, 64)
	hi, _ := strconv.ParseFloat(max, 64)
	if v < lo || v > hi {
		return fmt.Sprintf("outside the bounds %s-%s (Postgres refuses)", min, max)
	}
	return ""
}

// parseBool is parse_bool of REL_17_6 (bool.c parse_bool_with_len): true,
// false, yes, no and unique prefixes of them, on, off (at least "of"), 1
// and 0, case-insensitive.
func parseBool(s string) (value, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return false, false
	}
	switch {
	case strings.HasPrefix("true", s):
		return true, true
	case strings.HasPrefix("false", s):
		return false, true
	case strings.HasPrefix("yes", s):
		return true, true
	case strings.HasPrefix("no", s):
		return false, true
	case s == "on":
		return true, true
	case len(s) >= 2 && strings.HasPrefix("off", s):
		return false, true
	case s == "1":
		return true, true
	case s == "0":
		return false, true
	}
	return false, false
}

// indexReloptKind is the reloptions kind of an index access method, ""
// for a method whose options the pass does not know (contrib's).
func indexReloptKind(using *string) string {
	if using == nil {
		return "btree"
	}
	switch m := strings.ToLower(*using); m {
	case "btree", "hash", "gist", "gin", "spgist", "brin":
		return m
	}
	return ""
}

// keywordNameError marks a parse error U8 raises: an unquoted keyword in a
// name position whose gram.y nonterminal does not accept its category
// (kwlist.h, pgKeywordCategories).
const keywordNameError = "is not accepted in this name position"

// keywordRow is the precondition id of U8 for a statement (P7, syntax):
// the row its head belongs to.
func keywordRow(toks []token) string {
	c := &cursor{toks: toks}
	switch {
	case c.peekKw("create", "table"), c.peekKw("create", "unlogged", "table"), c.peekKw("create", "temp"), c.peekKw("create", "temporary"):
		return "M1.P7"
	case c.peekKw("alter", "table"):
		return "M5a.P7"
	case c.peekKw("alter", "index"):
		return "M8.P7"
	case c.peekKw("create", "index"), c.peekKw("create", "unique", "index"):
		return "M4.P7"
	case c.peekKw("create", "type"):
		return "R2.P7"
	case c.peekKw("create", "function"), c.peekKw("create", "procedure"), c.peekKw("create", "or", "replace"), c.peekKw("alter", "function"), c.peekKw("alter", "procedure"), c.peekKw("alter", "routine"):
		return "R6.P7"
	case c.peekKw("create", "view"), c.peekKw("create", "materialized"), c.peekKw("create", "sequence"), c.peekKw("create", "domain"), c.peekKw("create", "schema"):
		return "R1.P7"
	case c.peekKw("insert"), c.peekKw("update"), c.peekKw("delete"), c.peekKw("copy"), c.peekKw("select"), c.peekKw("with"), c.peekKw("merge"), c.peekKw("truncate"):
		return "N3.P7"
	}
	return "N1.P7"
}

// gucName reads a parameter name at the cursor (gram.y var_name: ColId
// with dotted ColLabel parts) and returns it lower-cased with its parts.
func gucName(c *cursor) (string, int, bool) {
	t := c.tok()
	if c.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return "", 0, false
	}
	name, parts := t.value, 1
	c.i++
	for c.isPunct(".") {
		c.i++
		n, err := c.label()
		if err != nil {
			return "", 0, false
		}
		name += "." + n
		parts++
	}
	return strings.ToLower(name), parts, true
}

// gucKnown applies N2.P1 and N2.P4 (U7) to a parameter name: "" when SET
// or RESET of it is proven, otherwise why it is refused or not proven.
func gucKnown(name string, parts int) (pgGUC, string) {
	if parts == 2 {
		prefix, _, _ := strings.Cut(name, ".")
		if pgReservedGUCPrefixes[prefix] {
			return pgGUC{}, fmt.Sprintf("parameter %s uses the prefix %s, which a module reserves once loaded%s", name, prefix, precondition("N2.P1"))
		}
		// A placeholder: any string value is accepted.
		return pgGUC{context: "user", typ: 's'}, ""
	}
	if parts > 2 {
		return pgGUC{}, fmt.Sprintf("parameter %s is not a known placeholder form%s", name, precondition("N2.P1"))
	}
	g, ok := pgGUCs[name]
	switch {
	case !ok:
		return g, fmt.Sprintf("unrecognized configuration parameter %s (Postgres refuses, 42704)%s", name, precondition("N2.P1"))
	case g.conditional:
		return g, fmt.Sprintf("parameter %s exists only in some builds%s", name, precondition("N2.P1"))
	case g.context == "suset":
		return g, fmt.Sprintf("parameter %s can be set only by a superuser or a role granted it%s", name, precondition("N2.P4"))
	case g.context != "userset":
		return g, fmt.Sprintf("parameter %s cannot be changed now (context %s; Postgres refuses, 55P02)%s", name, g.context, precondition("N2.P4"))
	}
	return g, ""
}

// setChecks applies U7 to SET [SESSION | LOCAL] at the cursor (after those
// words): "" when the statement is proven, otherwise why it may fail. The
// special forms other than TIME ZONE (ROLE, TRANSACTION, CONSTRAINTS,
// NAMES, XML OPTION, SESSION CHARACTERISTICS) keep their earlier rows.
func setChecks(c *cursor) string {
	switch {
	case c.peekKw("time", "zone"):
		c.i += 2
		vals := setValues(c)
		if len(vals) == 1 && (vals[0] == "default" || vals[0] == "local" || timezoneValue(vals[0])) {
			return ""
		}
		return "SET TIME ZONE: the value is not proven" + precondition("N2.P5")
	case c.peekKw("role"), c.peekKw("transaction"), c.peekKw("constraints"), c.peekKw("names"),
		c.peekKw("xml", "option"), c.peekKw("session", "characteristics"), c.peekKw("session", "authorization"):
		return ""
	}
	name, parts, ok := gucName(c)
	if !ok {
		return ""
	}
	g, why := gucKnown(name, parts)
	if why != "" {
		return "SET " + why
	}
	switch {
	case c.acceptKw("to"):
	case !c.done() && c.tok().kind == tokOp && c.tok().value == "=":
		c.i++
	default:
		return fmt.Sprintf("SET %s: expected TO or =%s", name, precondition("N2.P5"))
	}
	vals := setValues(c)
	if len(vals) == 1 && vals[0] == "default" {
		return ""
	}
	if len(vals) != 1 {
		return fmt.Sprintf("SET %s: a list value is not proven%s", name, precondition("N2.P5"))
	}
	if why := gucValue(name, g, vals[0]); why != "" {
		return fmt.Sprintf("SET %s: %s%s", name, why, precondition("N2.P5"))
	}
	return ""
}

// resetChecks applies U7 to RESET name.
func resetChecks(c *cursor) string {
	name, parts, ok := gucName(c)
	if !ok || !c.done() {
		return ""
	}
	g, why := gucKnown(name, parts)
	if why != "" {
		return "RESET " + why
	}
	if g.noReset {
		return fmt.Sprintf("RESET %s: the parameter cannot be reset (Postgres refuses)%s", name, precondition("N2.P4"))
	}
	return ""
}

// setValues reads gram.y var_list at the cursor: each value's text
// (string constants unquoted, a leading sign kept, keywords lower-cased;
// "default" for DEFAULT), or nil when an item is not one constant.
func setValues(c *cursor) []string {
	var out []string
	for !c.done() {
		sign := ""
		if c.tok().kind == tokOp && (c.tok().value == "-" || c.tok().value == "+") {
			sign = c.tok().value
			c.i++
		}
		if c.done() {
			return nil
		}
		text, ok := tokenText(c.tok(), c.src)
		if !ok {
			return nil
		}
		if c.tok().kind == tokIdent {
			text = strings.ToLower(text)
		}
		c.i++
		out = append(out, sign+text)
		if c.done() {
			break
		}
		if !c.acceptPunct(",") {
			return nil
		}
	}
	return out
}

// gucValue applies N2.P5 to one value: "" when proven for the parameter's
// type, otherwise why it is refused or not proven.
func gucValue(name string, g pgGUC, v string) string {
	if g.check {
		switch name {
		case "timezone":
			if timezoneValue(v) {
				return ""
			}
		case "application_name":
			return ""
		case "search_path":
			// check_search_path (namespace.c) checks only that the value
			// splits as an identifier list (SplitIdentifierString), and
			// flatten_set_variable_args quotes each item of a
			// GUC_LIST_QUOTE parameter as an identifier, so every value
			// the grammar takes splits; whether the schemas exist is not
			// checked.
			return ""
		}
		return "the parameter has a check hook, and the value is not in its closed table"
	}
	switch g.typ {
	case 'b':
		if _, ok := parseBool(v); ok {
			return ""
		}
		return "not a boolean (Postgres refuses, 22023)"
	case 'i', 'r':
		if g.unit && strings.IndexFunc(v, func(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }) >= 0 {
			return "a value with a unit is not proven"
		}
		return numberInBounds(v, g.typ == 'i', g.min, g.max)
	case 'e':
		if slices.Contains(g.enum, strings.ToLower(v)) {
			return ""
		}
		return "not one of the parameter's values (Postgres refuses, 22023)"
	}
	return ""
}

// timezoneValue is U7's closed table for timezone: UTC and GMT.
func timezoneValue(v string) bool {
	return strings.EqualFold(v, "utc") || strings.EqualFold(v, "gmt")
}

// serialTypeNames are the serial pseudo-types of column definitions
// (parse_utilcmd.c transformColumnDefinition).
var serialTypeNames = map[string]bool{"serial": true, "serial2": true, "serial4": true, "serial8": true, "smallserial": true, "bigserial": true}

// typeProven applies M2.P1 (U7) to a written type: "" when it is proven to
// exist (a pg_catalog type of pg_type.dat or a gram.y SQL type name, an
// array of one, a type the set holds, an E1 type of contrib), otherwise
// why it may not.
func (s *state) typeProven(typ string) string {
	canon, ok := canonicalType(typ)
	if !ok {
		return fmt.Sprintf("type %s could not be read", typ)
	}
	base := canon
	for strings.HasSuffix(base, "[]") {
		base = strings.TrimSuffix(base, "[]")
	}
	if i := strings.Index(base, "("); i >= 0 {
		base = base[:i]
	}
	if strings.HasPrefix(base, "interval ") {
		// interval's field qualifiers (gram.y opt_interval).
		base = "interval"
	}
	schema, name, qualified := strings.Cut(base, ".")
	if !qualified {
		name, schema = schema, ""
	}
	if schema == "" || schema == "pg_catalog" {
		if ty, ok := pgTypes[name]; ok && ty.category != 'P' {
			return ""
		}
		if schema == "" && serialTypeNames[name] && !strings.HasSuffix(canon, "[]") {
			return ""
		}
	}
	if schema != "pg_catalog" && (s.holds(name, kindType) || e1Types[name]) {
		return ""
	}
	return fmt.Sprintf("type %s is not a pg_catalog type, a type the set holds or an E1 type", typ)
}

// builtinCollations are the collations of pg_collation.dat REL_17_6 that
// exist under every provider and build (U7); libc and ICU locale names
// depend on the server.
var builtinCollations = map[string]bool{"default": true, "C": true, "POSIX": true, "ucs_basic": true, "pg_c_utf8": true}

// collationProven applies M2.P1 (U7) to a written COLLATE name (schema
// and name joined by a dot, as qname.String writes them): "" when it is a
// built-in collation or one the set holds, otherwise why it may not
// exist.
func (s *state) collationProven(name *string) string {
	if name == nil {
		return ""
	}
	n := strings.TrimPrefix(*name, "pg_catalog.")
	if builtinCollations[n] {
		return ""
	}
	key := n1Key("collation", "", *name)
	held := s.n1.names[key] && !s.n1.renamed["collation"] && !s.rev17.dropped[key]
	if held && s.rev18.gone[bareName(*name)] == "" && (strings.Contains(*name, ".") || !s.pathChanged) {
		// A collation the set created under this written name, not
		// dropped or renamed away since (W5, X2), and no unread statement
		// may have renamed (N1's record of names); unqualified, under a
		// search_path the pass knows.
		return ""
	}
	return fmt.Sprintf("collation %s is not a built-in collation (libc and ICU locales depend on the server)", *name)
}

// splitType returns a written type's base name, schema ("" when
// unqualified) and array dimensions, as typeProven reads it.
func splitType(typ string) (schema, name string, dims int, ok bool) {
	canon, ok := canonicalType(typ)
	if !ok {
		return "", "", 0, false
	}
	for strings.HasSuffix(canon, "[]") {
		canon = strings.TrimSuffix(canon, "[]")
		dims++
	}
	if i := strings.Index(canon, "("); i >= 0 {
		canon = canon[:i]
	}
	if strings.HasPrefix(canon, "interval ") {
		canon = "interval"
	}
	schema, name, qualified := strings.Cut(canon, ".")
	if !qualified {
		name, schema = schema, ""
	}
	return schema, name, dims, true
}

// catalogDefault reports whether pg_opclass.dat has a default class of
// method for the pg_catalog type typ, directly, through a binary-coercible
// cast (pg_cast.dat method b, GetDefaultOpClass), or by the polymorphic
// classes of its kind (ranges, multiranges).
func catalogDefault(method, typ string) bool {
	accepts := func(intype string) bool {
		if intype == typ {
			return true
		}
		if c, ok := pgCasts[[2]string{typ, intype}]; ok && c.method == 'b' && c.context == 'i' {
			return true
		}
		if ty, ok := pgTypes[typ]; ok && ty.category == 'R' {
			if strings.HasSuffix(typ, "multirange") {
				return intype == "anymultirange"
			}
			return intype == "anyrange"
		}
		return false
	}
	for _, o := range pgOpclasses {
		if o.method == method && o.isDefault && accepts(o.intype) {
			return true
		}
	}
	return false
}

// opclassProven applies U7 (P4) to an index key column without a written
// operator class: "" when its type has a default operator class of the
// access method (catalogDefault for pg_catalog types and arrays of them,
// enum_ops and record_ops for enums, composite types and row types the set
// holds, an E1 type's declared default), otherwise why the key may be
// refused.
func (s *state) opclassProven(method, typ string) string {
	schema, name, dims, ok := splitType(typ)
	if !ok {
		return fmt.Sprintf("type %s could not be read", typ)
	}
	method = strings.ToLower(method)
	if schema == "" && dims == 0 {
		switch name {
		case "serial", "serial4":
			name = "int4"
		case "bigserial", "serial8":
			name = "int8"
		case "smallserial", "serial2":
			name = "int2"
		}
	}
	if schema == "" || schema == "pg_catalog" {
		if ty, ok := pgTypes[name]; ok {
			switch {
			case dims > 0 && (method == "btree" || method == "hash" || method == "gin"):
				// array_ops: the element type needs its own class (btree
				// for GIN's element comparisons).
				elem := method
				if method == "gin" {
					elem = "btree"
				}
				if catalogDefault(elem, name) {
					return ""
				}
			case dims == 0 && ty.category != 'P' && catalogDefault(method, name):
				return ""
			}
			return fmt.Sprintf("data type %s has no default operator class for access method %s (Postgres refuses, 42704)", typ, method)
		}
	}
	if dims > 0 {
		return fmt.Sprintf("the default %s operator class of %s is not proven", method, typ)
	}
	if schema != "pg_catalog" {
		var hits []*relation
		for k, r := range s.relations {
			if k == name || strings.HasSuffix(k, "."+name) {
				hits = append(hits, r)
			}
		}
		if len(hits) == 1 && !hits[0].uncertain {
			r := hits[0]
			switch {
			case r.kind == "type" && r.labels != nil && (method == "btree" || method == "hash"):
				return "" // enum_ops
			case r.kind == "composite type" && method == "btree":
				return "" // record_ops
			}
		}
		if e1Types[name] {
			for _, o := range e1Opclasses {
				if o.method == method && o.isDefault && o.intype == name {
					return ""
				}
			}
		}
	}
	return fmt.Sprintf("the default %s operator class of %s is not proven", method, typ)
}

// keyOpclasses applies U7 (P4) to the columns of a primary key or UNIQUE
// constraint: their types need a default btree operator class.
func (s *state) keyOpclasses(t *table, cols []*column, row string) {
	for _, c := range cols {
		if why := s.opclassProven("btree", writtenType(c)); why != "" {
			s.unproven(t, fmt.Sprintf("key column %s: %s%s", c.name, why, precondition(rowIDs[row].p4)))
			return
		}
	}
}

// alterIndexWords are the first words of gram.y's alter_table_cmd and
// partition_cmd (ALTER INDEX name accepts any of them syntactically; the
// executor refuses those that do not apply to an index). Any other word
// after ALTER INDEX name is a syntax error (P7, g7-s1811).
var alterIndexWords = map[string]bool{
	"add": true, "alter": true, "drop": true, "rename": true, "set": true, "reset": true, "owner": true,
	"cluster": true, "inherit": true, "no": true, "of": true, "not": true, "replica": true, "enable": true,
	"disable": true, "force": true, "validate": true, "attach": true, "detach": true, "depends": true,
}

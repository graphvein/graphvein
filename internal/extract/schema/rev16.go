package schema

import (
	"fmt"
	"strings"
	"unicode"
)

// Rules of T018a2 Revision 1.6 step D1 that complete Revision 1.5 rules
// (same-rule completions): V2 languages that must exist (U7), V9 written
// operator classes (U7), V10 SET NAMES (U7), V11 system column names in
// other forms (U10), V12 storage parameters of views and materialized
// views (U9) and V13 types valid only in some positions (U7 M2.P1).

// languageProven applies V2 to a LANGUAGE clause's value (its token): sql
// and plpgsql exist in every database initdb makes (template0 and
// template1 hold plpgsql); a string constant is taken as written, not
// folded. Every other language may not exist or needs superuser: "" when
// proven, otherwise why the statement may fail.
func languageProven(t token, src []byte) string {
	name := t.value
	if t.kind == tokString {
		text, _ := tokenText(t, src)
		name = strings.Trim(text, "'")
	} else if t.kind != tokIdent && t.kind != tokQuoted {
		return "its language is not read"
	}
	switch name {
	case "sql", "plpgsql":
		return ""
	}
	return fmt.Sprintf("language %s is not proven to exist (only sql and plpgsql are)", name)
}

// routineLanguage applies V2 to CREATE FUNCTION / PROCEDURE and DO.
func (s *state) routineLanguage(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	routine := c.peekKw("create", "function") || c.peekKw("create", "procedure") || c.peekKw("create", "or", "replace", "function") || c.peekKw("create", "or", "replace", "procedure")
	do := c.peekKw("do")
	if !routine && !do {
		return
	}
	for j := 1; j+1 < len(toks); j++ {
		if toks[j].kind != tokIdent || toks[j].value != "language" {
			continue
		}
		if why := languageProven(toks[j+1], s.m.source); why != "" {
			id := "R6.P1"
			if do {
				id = "O1.P14"
			}
			s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", strings.ToUpper(words(toks[:2])), why, precondition(id)))
		}
		return
	}
}

// cleanEncodingName is encnames.c clean_encoding_name: alphanumerics,
// lower case.
func cleanEncodingName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// clientEncoding applies V10 to SET NAMES and SET client_encoding: the
// analysis of the statement, or ok false when it is neither.
func clientEncoding(c *cursor) (analysis, bool) {
	at := c.i
	names := false
	switch {
	case c.acceptKw("names"):
		// gram.y: NAMES opt_encoding (Sconst, DEFAULT or nothing).
		names = true
		if !c.done() && c.tok().kind != tokString && !c.isKw(0, "default") {
			return parseErrorAnalysis(c.fail("SET NAMES takes a string constant or DEFAULT (gram.y opt_encoding)%s", precondition("N2.P7"))), true
		}
	case c.isKw(0, "client_encoding") && (c.isKw(1, "to") || c.i+1 < len(c.toks) && c.toks[c.i+1].kind == tokOp && c.toks[c.i+1].value == "="):
		c.i += 2
	default:
		return analysis{}, false
	}
	if c.done() || c.acceptKw("default") {
		if c.done() {
			return analysis{class: classNoEffect}, true
		}
		if names {
			return parseErrorAnalysis(c.fail("unexpected text after SET NAMES%s", precondition("N2.P7"))), true
		}
		c.i = at
		return analysis{}, false
	}
	t := c.tok()
	if c.i+1 != len(c.toks) || t.kind != tokString && t.kind != tokIdent && t.kind != tokQuoted {
		return analysis{class: classNoEffect, notProven: "SET NAMES / client_encoding with a value the pass does not read may be refused" + precondition("N2.P5")}, true
	}
	name := t.value
	if t.kind == tokString {
		text, _ := tokenText(t, c.src)
		name = strings.Trim(text, "'")
	}
	enc, known := pgEncodingNames[cleanEncodingName(name)]
	switch {
	case !known:
		return analysis{class: classNoEffect, notProven: fmt.Sprintf("SET client_encoding %s: no such encoding (Postgres refuses, 22023)%s", name, precondition("N2.P5"))}, true
	case enc == "UTF8":
		return analysis{class: classNoEffect}, true
	}
	a := lexingChange("client_encoding")
	a.notProven = fmt.Sprintf("SET client_encoding %s: another encoding may not be accepted, and later migrations are read in it%s", name, precondition("N2.P5"))
	return a, true
}

// viewOptions applies V12: the WITH (...) list of CREATE VIEW and CREATE
// MATERIALIZED VIEW, SET / RESET (...) of ALTER VIEW and ALTER
// MATERIALIZED VIEW, and of ALTER TABLE on a relation held as a view or
// materialized view, against the view kind (security_barrier,
// security_invoker, check_option) and the heap kind. A check_option (and
// WITH CHECK OPTION) is not proven: the pass does not prove a view
// automatically updatable (view.c 440).
func (s *state) viewOptions(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	kind, id, what := "", "", ""
	isGroupAt := func(j int) bool { return j < len(toks) && toks[j].kind == tokPunct && toks[j].value == "(" }
	check := func(at int, reset bool) {
		g := &cursor{toks: toks, i: at, src: s.m.source}
		end, err := g.group()
		if err != nil {
			return
		}
		opts := storageOptions(g, at, end)
		if why := storageCheck(kind, opts, reset, s.m.source); why != "" {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, why, precondition(id)))
			return
		}
		for _, o := range opts {
			if o.name == "check_option" && !reset {
				s.unknownFact(fmt.Sprintf("%s may be refused: check_option needs an automatically updatable view, which the pass does not prove%s", what, precondition(id)))
				return
			}
		}
	}
	switch {
	case c.acceptKw("create"):
		c.acceptKw("or", "replace")
		for c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("recursive") {
		}
		switch {
		case c.acceptKw("view"):
			kind, what = "view", "CREATE VIEW"
		case c.acceptKw("materialized", "view"):
			kind, what = "heap", "CREATE MATERIALIZED VIEW"
			c.acceptKw("if", "not", "exists")
		default:
			return
		}
		id = "R1.P5"
		if _, err := c.name(); err != nil {
			return
		}
		if c.isPunct("(") {
			if e, err := c.group(); err == nil {
				c.i = e
			}
		}
		if kind == "heap" && c.acceptKw("using") {
			c.i++
		}
		if c.acceptKw("with") && isGroupAt(c.i) {
			check(c.i, false)
		}
		if kind == "view" {
			n := len(toks)
			if n >= 3 && toks[n-1].value == "option" && toks[n-2].value == "check" {
				s.unknownFact(fmt.Sprintf("%s may be refused: WITH CHECK OPTION needs an automatically updatable view, which the pass does not prove%s", what, precondition(id)))
			}
		}
		return
	case c.acceptKw("alter", "view"):
		kind, id, what = "view", "R4.P5", "ALTER VIEW"
	case c.acceptKw("alter", "materialized", "view"):
		kind, id, what = "heap", "R4.P5", "ALTER MATERIALIZED VIEW"
	case c.acceptKw("alter", "table"):
		id, what = "M5k-a.P5", "ALTER TABLE"
	default:
		return
	}
	c.acceptKw("if", "exists")
	q, err := c.name()
	if err != nil {
		return
	}
	if kind == "" {
		switch {
		case s.heldObject(q, "view"):
			kind = "view"
		case s.heldObject(q, "materialized view"):
			kind = "heap"
		default:
			return
		}
		what += " on view " + q.String()
	}
	for _, r := range c.splitList(c.i, len(toks)) {
		if r[0]+1 < r[1] && (toks[r[0]].value == "set" || toks[r[0]].value == "reset") && toks[r[0]].kind == tokIdent && isGroupAt(r[0]+1) {
			check(r[0]+1, toks[r[0]].value == "reset")
		}
	}
}

// pseudoType reports a pg_catalog pseudo-type (typtype p) written as typ.
func pseudoType(typ string) bool {
	schema, name, dims, ok := splitType(typ)
	return ok && dims == 0 && (schema == "" || schema == "pg_catalog") && pgPseudoTypes[name]
}

// serialType reports serial and its aliases written as a type name.
func serialType(typ string) bool {
	schema, name, dims, ok := splitType(typ)
	return ok && schema == "" && dims == 0 && serialTypes[name] != ""
}

// typePosition applies V13 to a type written outside a column definition
// of CREATE TABLE or ADD COLUMN (column is false) or in one (column is
// true): serial is a type only in a column definition (42704); a
// pseudo-type is no column or attribute type (42P16) and no domain base
// (42804). It returns why Postgres refuses, or "".
func typePosition(typ, position string) string {
	switch {
	case serialType(typ) && position != "column":
		return fmt.Sprintf("%s is not a type outside a column definition (Postgres refuses, 42704)", typ)
	case pseudoType(typ) && (position == "column" || position == "attribute"):
		return fmt.Sprintf("%s has pseudo-type %s (Postgres refuses, 42P16)", position, typ)
	case pseudoType(typ) && position == "domain":
		return fmt.Sprintf("%s is a pseudo-type, no domain base type (Postgres refuses, 42804)", typ)
	}
	return ""
}

// typePositions applies V13 to the type positions the other readers do
// not check: composite type attributes, domain bases, ALTER TYPE ...
// ADD / ALTER ATTRIBUTE and routine signatures.
func (s *state) typePositions(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	refuse := func(what, why, id string) {
		s.mayFail(fmt.Sprintf("%s: %s%s", what, why, precondition(id)))
	}
	idFor := func(typ string) string {
		if pseudoType(typ) {
			return "M2.P4"
		}
		return "M2.P1"
	}
	readType := func(sub *cursor) (string, bool) {
		start := sub.i
		if columnType(sub) != nil {
			return "", false
		}
		return sub.text(start, sub.i), true
	}
	switch {
	case c.peekKw("create", "domain"):
		c.i = 2
		if _, err := c.name(); err != nil {
			return
		}
		c.acceptKw("as")
		if typ, ok := readType(c); ok {
			if why := typePosition(typ, "domain"); why != "" {
				refuse("CREATE DOMAIN", why, idFor(typ))
			}
		}
	case c.peekKw("create", "type"):
		c.i = 2
		if _, err := c.name(); err != nil || !c.acceptKw("as") || !c.isPunct("(") {
			return
		}
		end, err := c.group()
		if err != nil || end-c.i <= 2 {
			return
		}
		for _, r := range c.splitList(c.i+1, end-1) {
			sub := &cursor{toks: toks[:r[1]], i: r[0], src: s.m.source}
			if _, err := sub.ident(); err != nil {
				continue
			}
			if typ, ok := readType(sub); ok {
				if why := typePosition(typ, "attribute"); why != "" {
					refuse("CREATE TYPE", why, idFor(typ))
					return
				}
			}
		}
	case c.peekKw("alter", "type"):
		c.i = 2
		if _, err := c.name(); err != nil {
			return
		}
		for _, r := range c.splitList(c.i, len(toks)) {
			sub := &cursor{toks: toks[:r[1]], i: r[0], src: s.m.source}
			switch {
			case sub.acceptKw("add", "attribute"):
				if _, err := sub.ident(); err != nil {
					continue
				}
			case sub.acceptKw("alter", "attribute"):
				if _, err := sub.ident(); err != nil {
					continue
				}
				sub.acceptKw("set", "data")
				if !sub.acceptKw("type") {
					continue
				}
			default:
				continue
			}
			if typ, ok := readType(sub); ok {
				if why := typePosition(typ, "attribute"); why != "" {
					refuse("ALTER TYPE", why, idFor(typ))
					return
				}
			}
		}
	case c.peekKw("create", "function"), c.peekKw("create", "procedure"), c.peekKw("create", "or", "replace", "function"), c.peekKw("create", "or", "replace", "procedure"):
		c.i = 1
		c.acceptKw("or", "replace")
		c.i++
		if _, err := c.funcName(); err != nil || !c.isPunct("(") {
			return
		}
		end, err := c.group()
		if err != nil {
			return
		}
		if end-c.i > 2 {
			for _, r := range c.splitList(c.i+1, end-1) {
				if p, ok := routineParameter(c, r[0], r[1]); ok && serialType(p.typ) {
					refuse("CREATE FUNCTION/PROCEDURE", typePosition(p.typ, "signature"), "M2.P1")
					return
				}
			}
		}
		c.i = end
		if c.acceptKw("returns") {
			c.acceptKw("setof")
			if typ, ok := readType(c); ok && serialType(typ) {
				refuse("CREATE FUNCTION", typePosition(typ, "signature"), "M2.P1")
			}
		}
	}
}

// writtenOpclass applies V9 to an index element's written operator class
// over a column of type typ (ResolveOpClass, indexcmds.c 2263): "" when
// proven, otherwise why the index may be or is refused (refused true).
func (s *state) writtenOpclass(method, class, typ string) (why string, refused bool) {
	method = strings.ToLower(method)
	schema, cname, _ := strings.Cut(class, ".")
	if !strings.Contains(class, ".") {
		cname, schema = schema, ""
	}
	tschema, tname, dims, ok := splitType(typ)
	if !ok {
		return fmt.Sprintf("type %s could not be read", typ), false
	}
	if base, ok := serialTypes[tname]; ok && tschema == "" && dims == 0 {
		tname = map[string]string{"smallint": "int2", "integer": "int4", "bigint": "int8"}[base]
	}
	if schema == "" || schema == "pg_catalog" {
		var classes []pgOpclass
		for _, o := range pgOpclasses {
			if o.method == method && o.name == cname {
				classes = append(classes, o)
			}
		}
		if len(classes) > 0 {
			catalogType := false
			if ty, ok := pgTypes[tname]; ok && (tschema == "" || tschema == "pg_catalog") {
				catalogType = ty.category != 'P'
			}
			for _, o := range classes {
				switch {
				case dims > 0 && o.intype == "anyarray":
					return "", false
				case dims > 0:
				case o.intype == tname:
					return "", false
				case catalogType:
					if cast, ok := pgCasts[[2]string{tname, o.intype}]; ok && cast.method == 'b' && cast.context == 'i' {
						return "", false
					}
					if ty := pgTypes[tname]; ty.category == 'R' && (o.intype == "anyrange" && !strings.HasSuffix(tname, "multirange") || o.intype == "anymultirange" && strings.HasSuffix(tname, "multirange")) {
						return "", false
					}
				}
			}
			if catalogType && dims == 0 {
				return fmt.Sprintf("operator class %s does not accept data type %s (Postgres refuses, 42804)", class, typ), true
			}
			if tschema != "pg_catalog" {
				for k, r := range s.relations {
					if (k == tname || strings.HasSuffix(k, "."+tname)) && !r.uncertain && dims == 0 {
						for _, o := range classes {
							if r.kind == "type" && r.labels != nil && o.intype == "anyenum" || r.kind == "composite type" && o.intype == "record" {
								return "", false
							}
						}
					}
				}
			}
			return fmt.Sprintf("operator class %s is not proven to accept type %s", class, typ), false
		}
	}
	if schema == "" {
		for _, o := range e1Opclasses {
			if o.method == method && o.name == cname {
				if o.intype == tname && dims == 0 {
					return "", false
				}
			}
		}
	}
	return fmt.Sprintf("operator class %s of %s is not proven for type %s", class, method, typ), false
}

// systemColumnUse applies V11 to the forms U10 did not cover: explicit
// column definitions of CREATE TABLE ... LIKE / INHERITS (M1b.P2), the
// attributes of the composite type of a typed table (M1b.P2), ADD
// ATTRIBUTE of a type a typed table uses (R4.P2), and the output column
// names of CREATE TABLE AS, SELECT INTO and materialized views (R1.P2).
func (s *state) systemColumnUse(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	refuse := func(what, name, id string) {
		s.mayFail(fmt.Sprintf("%s: column name %s conflicts with a system column name (Postgres refuses, 42701)%s", what, name, precondition(id)))
	}
	switch {
	case c.peekKw("create", "type"):
		c.i = 2
		q, err := c.name()
		if err != nil || !c.acceptKw("as") || !c.isPunct("(") {
			return
		}
		end, err := c.group()
		if err != nil {
			return
		}
		var attrs []string
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] < r[1] && (toks[r[0]].kind == tokIdent || toks[r[0]].kind == tokQuoted) {
				attrs = append(attrs, toks[r[0]].value)
			}
		}
		s.compositeAttrs()[q.name] = attrs
		return
	case c.peekKw("alter", "type"):
		c.i = 2
		q, err := c.name()
		if err != nil || !s.typedTables()[q.name] {
			return
		}
		for _, r := range c.splitList(c.i, len(toks)) {
			sub := &cursor{toks: toks[:r[1]], i: r[0], src: s.m.source}
			attr := sub.peekKw("add", "attribute") || sub.peekKw("drop", "attribute") || sub.peekKw("alter", "attribute")
			if !attr {
				continue
			}
			cascade := containsKw(&cursor{toks: toks[r[0]:r[1]]}, "cascade")
			if !cascade {
				// tablecmds.c find_typed_table_dependencies: a typed table
				// uses the type, so RESTRICT is refused (2BP01).
				s.mayFail(fmt.Sprintf("ALTER TYPE %s: a typed table uses the type, and the attribute change is not CASCADE (Postgres refuses, 2BP01)%s", q, precondition("R4.P2")))
				return
			}
			if sub.acceptKw("add", "attribute") && !sub.done() && systemColumns[sub.tok().value] {
				refuse("ALTER TYPE "+q.String()+" ADD ATTRIBUTE", sub.tok().value, "R4.P2")
				return
			}
		}
		return
	case c.peekKw("select"):
		if into := depth0(toks, 1, "into"); into > 0 {
			for _, n := range outputNames(toks, 1, into) {
				if systemColumns[n] {
					refuse("SELECT ... INTO", n, "R1.P2")
					return
				}
			}
		}
		return
	case !c.acceptKw("create"):
		return
	}
	for c.acceptKw("global") || c.acceptKw("local") || c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("unlogged") {
	}
	switch {
	case c.acceptKw("table"):
	case c.acceptKw("materialized", "view"):
		s.queryNames(c, "CREATE MATERIALIZED VIEW", refuse)
		return
	default:
		return
	}
	c.acceptKw("if", "not", "exists")
	q, err := c.name()
	if err != nil {
		return
	}
	switch {
	case c.acceptKw("of"):
		if ty, err := c.name(); err == nil {
			s.typedTables()[ty.name] = true
			for _, a := range s.compositeAttrs()[ty.name] {
				if systemColumns[a] {
					refuse("CREATE TABLE "+q.String()+" OF "+ty.String(), a, "M1b.P2")
					return
				}
			}
		}
		return
	case c.isPunct("("):
		if depth0(toks, c.i, "as") > 0 {
			s.queryNames(c, "CREATE TABLE AS", refuse)
			return
		}
		end, err := c.group()
		if err != nil {
			return
		}
		m1b := c.isKw(end-c.i, "inherits") || depth0(toks[:end], c.i+1, "like") > 0
		if !m1b && !(end < len(toks) && toks[end].kind == tokIdent && toks[end].value == "inherits") {
			return
		}
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] >= r[1] || isConstraintStart(c, r[0]) || toks[r[0]].kind == tokIdent && toks[r[0]].value == "like" {
				continue
			}
			if systemColumns[toks[r[0]].value] {
				refuse("CREATE TABLE "+q.String(), toks[r[0]].value, "M1b.P2")
				return
			}
		}
	case c.peekKw("as"):
		s.queryNames(c, "CREATE TABLE AS", refuse)
	}
}

// queryNames checks the output column names of CREATE TABLE AS or a
// materialized view at the cursor (an optional column list, then AS
// query).
func (s *state) queryNames(c *cursor, what string, refuse func(what, name, id string)) {
	if !c.isPunct("(") && !c.peekKw("as") {
		if _, err := c.name(); err != nil {
			return
		}
	}
	if c.isPunct("(") {
		end, err := c.group()
		if err != nil {
			return
		}
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] < r[1] && systemColumns[c.toks[r[0]].value] {
				refuse(what, c.toks[r[0]].value, "R1.P2")
				return
			}
		}
		return
	}
	at := depth0(c.toks, c.i, "select")
	if at < 0 {
		return
	}
	end := len(c.toks)
	for _, w := range []string{"from", "into", "where", "with", "group", "order", "limit", "union", "except", "intersect"} {
		if j := depth0(c.toks, at+1, w); j > 0 && j < end {
			end = j
		}
	}
	for _, n := range outputNames(c.toks, at+1, end) {
		if systemColumns[n] {
			refuse(what, n, "R1.P2")
			return
		}
	}
}

// outputNames returns the output column names a target list toks[from:to]
// gives (an alias, or FigureColname's name of a column reference, function
// call or cast, parse_target.c 1708); "" where the pass does not compute
// one.
func outputNames(toks []token, from, to int) []string {
	c := &cursor{toks: toks[:to], i: from}
	var out []string
	for _, r := range c.splitList(from, to) {
		el := toks[r[0]:r[1]]
		n := len(el)
		name := ""
		switch {
		case n == 0:
		case n >= 2 && el[n-2].kind == tokIdent && el[n-2].value == "as" && (el[n-1].kind == tokIdent || el[n-1].kind == tokQuoted):
			name = el[n-1].value
		case n >= 2 && (el[n-1].kind == tokIdent || el[n-1].kind == tokQuoted) && el[n-2].kind == tokOp && el[n-2].value == "::":
			// A cast is named by its type.
			name = el[n-1].value
		case dottedName(el):
			// A column reference names its last part.
			name = el[n-1].value
		case n >= 2 && (el[n-1].kind == tokIdent || el[n-1].kind == tokQuoted) && el[n-2].kind != tokOp && !(el[n-2].kind == tokPunct && el[n-2].value == "."):
			// A bare alias.
			name = el[n-1].value
		case el[0].kind == tokIdent && n > 1 && el[1].kind == tokPunct && el[1].value == "(":
			name = el[0].value
		}
		out = append(out, name)
	}
	return out
}

// dottedName reports a name [. name]... and nothing else.
func dottedName(el []token) bool {
	for i, t := range el {
		if i%2 == 0 && t.kind != tokIdent && t.kind != tokQuoted || i%2 == 1 && !(t.kind == tokPunct && t.value == ".") {
			return false
		}
	}
	return len(el)%2 == 1
}

// compositeAttrs and typedTables are the composite types' attribute names
// and the types typed tables use (V11), kept on the state.
func (s *state) compositeAttrs() map[string][]string {
	if s.rev16.attrs == nil {
		s.rev16.attrs = map[string][]string{}
	}
	return s.rev16.attrs
}

func (s *state) typedTables() map[string]bool {
	if s.rev16.typed == nil {
		s.rev16.typed = map[string]bool{}
	}
	return s.rev16.typed
}

// rev16State is the state V11 keeps, and V1's check_function_bodies
// values (session and transaction-local, U3's scopes).
type rev16State struct {
	attrs                map[string][]string
	typed                map[string]bool
	cfbSession, cfbLocal cfbValue
	// rulesWrite is set once the set may hold a rule whose action
	// writes rows (V5): a data statement may write other tables.
	rulesWrite bool
	// userCandidates are the set schemas that may be named like the
	// current user (V6); userKnownUnknown is set once one may exist that
	// the pass cannot name (code, AUTHORIZATION CURRENT_USER). pathSet is
	// set once a statement may have changed search_path (U3).
	userCandidates   map[string]bool
	userKnownUnknown bool
	pathSet          bool
	bookkeeping      bool
}

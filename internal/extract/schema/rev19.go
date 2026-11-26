package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// T018a2 Revision 1.9, step U-a: statement validators. Each rule (Y01,
// Y02, Y05-Y12, Y14-Y22, Y25, Y26, Y30-Y32, Y35-Y37, Y42, Y43, Y46) is a
// check Postgres 17 makes on the statement's text and on kinds the pass
// already records. A refusal and a possible failure both make the
// migration's applied state unknown (unknownFact); the text says which it
// is and ends with the rule's precondition id. The rules run before the
// statement changes the state (rev19Before), except those that record a
// name the statement creates (rev19After).

// rev19State is the state of the Revision 1.9 U-a rules.
type rev19State struct {
	// droppedBuiltin are the E5 schemas (public, information_schema) a
	// DROP SCHEMA the pass applied removed (Y08).
	droppedBuiltin map[string]bool
	// multiranges are the multirange type names CREATE TYPE ... AS RANGE
	// recorded, by written key, with the range's (Y21).
	multiranges map[string]string
	// typeWords are the identifiers of each set type's and domain's
	// definition, by name (Y02: a composite attribute, a domain's base or
	// a range's subtype may be a table's row type).
	typeWords map[string][]string
}

// reservedName is catalog.c IsReservedName: the pg_ prefix, as written
// after case folding.
func reservedName(n string) bool {
	return strings.HasPrefix(n, "pg_")
}

// builtinSchema reports the schemas E5 grants in every database (public,
// information_schema) and the system schemas.
func builtinSchema(n string) bool {
	return n == "public" || n == "information_schema" || n == "pg_catalog" || n == "pg_toast"
}

// rev19Before applies the U-a rules to one statement, before it changes
// the state.
func (s *state) rev19Before(st statement, a analysis) {
	toks := st.tokens
	if s.failure != nil || len(toks) == 0 {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.peekKw("create", "schema"):
		s.y19CreateSchema(toks)
	case c.peekKw("alter", "schema"):
		s.y19RenameSchema(toks)
	case c.peekKw("create", "role"), c.peekKw("create", "user"), c.peekKw("create", "group"):
		s.y19CreateRole(toks)
	case c.peekKw("alter", "role"), c.peekKw("alter", "user"), c.peekKw("alter", "group"):
		s.y19AlterRole(toks)
	case c.peekKw("create", "trigger"), c.peekKw("create", "constraint", "trigger"), c.peekKw("create", "or", "replace", "trigger"), c.peekKw("create", "or", "replace", "constraint", "trigger"):
		s.y19Trigger(toks)
	case c.peekKw("create", "policy"), c.peekKw("alter", "policy"):
		s.y19Policy(toks)
	case c.peekKw("lock"):
		s.y19Lock(toks)
	case c.peekKw("comment", "on"):
		s.y19Comment(toks)
	case c.peekKw("create", "publication"), c.peekKw("alter", "publication"):
		s.y19Publication(toks)
	case c.peekKw("alter", "index"):
		s.y19AlterPartitionedIndex(toks)
	case c.peekKw("reindex"):
		s.y19Reindex(toks)
	case c.peekKw("alter", "function"), c.peekKw("alter", "procedure"), c.peekKw("alter", "routine"):
		s.y19AlterRoutine(toks)
	case c.peekKw("create", "function"), c.peekKw("create", "procedure"), c.peekKw("create", "or", "replace", "function"), c.peekKw("create", "or", "replace", "procedure"):
		s.y19RoutineTypes(toks)
	case c.peekKw("create", "collation"):
		s.y19Collation(toks)
	case c.peekKw("alter", "default", "privileges"):
		s.y19DefaultPrivileges(toks)
	case c.peekKw("create", "type"), c.peekKw("create", "domain"), c.peekKw("alter", "type"):
		s.y19Types(toks, a)
	}
	if s.failure == nil && a.alter != nil {
		s.y19AlterColumnType(a.alter)
	}
	if s.failure == nil && a.index != nil {
		s.y19IndexMethod(a.index)
	}
	if s.failure == nil && (c.peekKw("create") || c.peekKw("alter", "table")) {
		s.y19ConstraintAttributes(toks)
		s.y19Exclude(toks)
	}
	if s.failure == nil {
		s.y19Multirange(toks, a)
	}
	if s.failure == nil && (c.peekKw("grant") || c.peekKw("alter", "default", "privileges")) {
		s.y19GrantPublic(toks)
	}
	if s.failure == nil {
		s.y19Catalog(toks)
	}
}

// rev19After records what a statement the pass applied created or
// removed (Y08, Y21).
func (s *state) rev19After(st statement, a analysis) {
	toks := st.tokens
	if len(toks) == 0 {
		return
	}
	c := &cursor{toks: toks, src: s.m.source}
	s.policyRecord(toks)
	if c.acceptKw("drop", "schema") {
		c.acceptKw("if", "exists")
		for {
			q, err := c.name()
			if err != nil {
				break
			}
			if q.name == "public" || q.name == "information_schema" {
				if s.rev19.droppedBuiltin == nil {
					s.rev19.droppedBuiltin = map[string]bool{}
				}
				s.rev19.droppedBuiltin[q.name] = true
			}
			if !c.acceptPunct(",") {
				break
			}
		}
	}
}

// schemaTaken reports whether Postgres holds schema n when a statement
// creates or renames onto it (Y08, Y43): a schema the set created and
// still holds, an E5 schema no DROP SCHEMA of the set removed, or a
// system schema; may is set when code may have created it.
func (s *state) schemaTaken(n string) (taken, may bool) {
	switch {
	case n == "pg_catalog" || n == "pg_toast":
		return true, false
	case (n == "public" || n == "information_schema") && !s.rev19.droppedBuiltin[n]:
		return true, false
	case s.holds(n, kindSchema):
		return true, false
	}
	return false, s.code.may[n]&kindSchema != 0
}

// y19CreateSchema applies Y06, Y08, Y32 and Y43 to CREATE SCHEMA [IF NOT
// EXISTS] [n] [AUTHORIZATION r] [elements] (schemacmds.c
// CreateSchemaCommand 104-133: the reserved-name check first, then IF NOT
// EXISTS, then NamespaceCreate's 42P06, pg_namespace.c 64; the schema
// takes the role's name when it has none).
func (s *state) y19CreateSchema(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	ifNotExists := c.acceptKw("if", "not", "exists")
	name, known := "", true
	if c.acceptKw("authorization") {
		switch {
		case c.peekKw("current_user"), c.peekKw("session_user"), c.peekKw("current_role"):
			known = false
			c.i++
		default:
			n, err := c.ident()
			if err != nil {
				return
			}
			name = n
		}
	} else {
		n, err := c.ident()
		if err != nil {
			return
		}
		name = n
		if c.acceptKw("authorization") {
			c.i++
		}
	}
	what := "CREATE SCHEMA " + name
	if known && reservedName(name) {
		s.unknownFact(fmt.Sprintf("%s: unacceptable schema name, the prefix pg_ is reserved (Postgres refuses, 42939)%s", what, precondition("R1.P2")))
		return
	}
	if known && !ifNotExists {
		taken, may := s.schemaTaken(name)
		switch {
		case taken:
			s.unknownFact(fmt.Sprintf("%s: schema %s already exists (Postgres refuses, 42P06)%s", what, name, precondition("R1.P2")))
			return
		case may:
			s.unknownFact(fmt.Sprintf("%s may be refused: code the set runs may have created schema %s%s", what, name, precondition("R1.P2")))
			return
		}
	}
	// Y32: an element qualified by another schema (parse_utilcmd.c
	// transformCreateSchemaStmtElements, setSchemaName 3920).
	for j := c.i; j < len(toks); j++ {
		if toks[j].kind != tokIdent || toks[j].value != "create" && toks[j].value != "grant" {
			continue
		}
		e := &cursor{toks: toks, i: j + 1, src: s.m.source}
		var q qname
		var err error
		switch {
		case e.acceptKw("table"), e.acceptKw("unlogged", "table"), e.acceptKw("sequence"), e.acceptKw("view"),
			e.acceptKw("or", "replace", "view"), e.acceptKw("recursive", "view"), e.acceptKw("or", "replace", "recursive", "view"):
			e.acceptKw("if", "not", "exists")
			q, err = e.name()
		case e.acceptKw("index"), e.acceptKw("unique", "index"):
			on := depth0(toks, e.i, "on")
			if on < 0 {
				continue
			}
			e.i = on + 1
			e.acceptKw("only")
			q, err = e.name()
		case e.acceptKw("trigger"), e.acceptKw("constraint", "trigger"), e.acceptKw("or", "replace", "trigger"):
			on := depth0(toks, e.i, "on")
			if on < 0 {
				continue
			}
			e.i = on + 1
			q, err = e.name()
		default:
			continue
		}
		if err != nil || q.schema == nil {
			continue
		}
		if !known {
			s.unknownFact(fmt.Sprintf("CREATE SCHEMA may be refused: element %s is qualified, and the schema's name (the role's) is not known%s", q, precondition("R1.P2")))
			return
		}
		if *q.schema != name {
			s.unknownFact(fmt.Sprintf("%s: element %s specifies a different schema (Postgres refuses, 42P15)%s", what, q, precondition("R1.P2")))
			return
		}
	}
}

// y19RenameSchema applies Y06 and Y08 to ALTER SCHEMA s RENAME TO n
// (schemacmds.c RenameSchema 273-293: 42P06 when n exists, then the
// reserved-name check).
func (s *state) y19RenameSchema(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	old, err := c.ident()
	if err != nil || !c.acceptKw("rename", "to") {
		return
	}
	n, err := c.ident()
	if err != nil {
		return
	}
	what := fmt.Sprintf("ALTER SCHEMA %s RENAME TO %s", old, n)
	taken, may := s.schemaTaken(n)
	switch {
	case taken:
		s.unknownFact(fmt.Sprintf("%s: schema %s already exists (Postgres refuses, 42P06)%s", what, n, precondition("M9.P2")))
	case reservedName(n):
		s.unknownFact(fmt.Sprintf("%s: unacceptable schema name, the prefix pg_ is reserved (Postgres refuses, 42939)%s", what, precondition("M9.P2")))
	case may || s.rev18.schemaUnknown:
		s.unknownFact(fmt.Sprintf("%s may be refused: a schema of that name may exist (created by code, or by CREATE SCHEMA AUTHORIZATION of a role whose name is unknown)%s", what, precondition("M9.P2")))
	}
}

// y19CreateRole applies Y06 to CREATE ROLE / USER / GROUP pg_x
// (user.c CreateRole 351: 42939).
func (s *state) y19CreateRole(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	n, err := c.ident()
	if err != nil {
		return
	}
	if reservedName(n) {
		s.unknownFact(fmt.Sprintf("CREATE %s %s: role name %s is reserved (Postgres refuses, 42939)%s", strings.ToUpper(toks[1].value), n, n, precondition("N1.P2")))
		return
	}
	s.roleOptions(c, "CREATE "+strings.ToUpper(toks[1].value)+" "+n, true)
}

// y19AlterRole applies Y06 to ALTER ROLE r RENAME TO n (user.c
// RenameRole 1383, 1390: a reserved old or new name, 42939).
func (s *state) y19AlterRole(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	var old string
	switch {
	case c.acceptKw("current_user"), c.acceptKw("session_user"), c.acceptKw("current_role"), c.acceptKw("all"):
		old = toks[c.i-1].value
	default:
		n, err := c.ident()
		if err != nil {
			return
		}
		old = n
	}
	what := fmt.Sprintf("ALTER %s %s", strings.ToUpper(toks[1].value), old)
	if c.acceptKw("in", "database") {
		if _, err := c.ident(); err != nil {
			return
		}
	}
	switch {
	case c.acceptKw("set"):
		if searchPathList(c.toks, c.i) && !wordAfterList(c.toks, c.i) {
			return
		}
		if why := placeholderSetting(c); why != "" {
			s.unknownFact(fmt.Sprintf("%s: %s", what, why))
			return
		}
		if why := setChecks(c); why != "" {
			s.unknownFact(fmt.Sprintf("%s: %s%s", what, why, precondition("N1.P5")))
		}
		return
	case c.acceptKw("reset"):
		if c.peekKw("all") {
			return
		}
		if why := placeholderSetting(c); why != "" {
			s.unknownFact(fmt.Sprintf("%s: %s", what, why))
			return
		}
		if why := resetChecks(c); why != "" {
			s.unknownFact(fmt.Sprintf("%s: %s%s", what, why, precondition("N1.P5")))
		}
		return
	case !c.acceptKw("rename", "to"):
		s.roleOptions(c, what, false)
		return
	}
	n, err := c.ident()
	if err != nil {
		return
	}
	if reservedName(old) || reservedName(n) {
		s.unknownFact(fmt.Sprintf("ALTER ROLE %s RENAME TO %s: role names starting with pg_ are reserved (Postgres refuses, 42939)%s", old, n, precondition("N1.P2")))
	}
}

// y19Catalog applies Y07: a pg_class relation created in pg_catalog or
// pg_toast is refused (heap.c heap_create 326, 42501, while
// allow_system_table_mods is off, even for a superuser); any other object
// created in pg_catalog needs CREATE on it, which E6 does not grant.
func (s *state) y19Catalog(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	var q qname
	relation, id := true, "M1.P2"
	switch {
	case c.acceptKw("create"):
		c.acceptKw("or", "replace")
		for c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("unlogged") || c.acceptKw("local") || c.acceptKw("global") || c.acceptKw("recursive") {
		}
		switch {
		case c.acceptKw("table"), c.acceptKw("foreign", "table"):
		case c.acceptKw("view"), c.acceptKw("materialized", "view"), c.acceptKw("sequence"):
			id = "R1.P2"
		case c.acceptKw("index"), c.acceptKw("unique", "index"):
			id = "M4.P2"
			on := depth0(toks, c.i, "on")
			if on < 0 {
				return
			}
			c.i = on + 1
			c.acceptKw("only")
		case c.acceptKw("type"):
			id = "R1.P2"
			// A composite type is a pg_class relation; other types are
			// not.
			relation = depth0(toks, c.i, "as") >= 0 && depth0(toks, c.i, "enum") < 0 && depth0(toks, c.i, "range") < 0
		case c.acceptKw("function"), c.acceptKw("procedure"), c.acceptKw("domain"), c.acceptKw("aggregate"),
			c.acceptKw("operator"), c.acceptKw("collation"), c.acceptKw("text", "search"), c.acceptKw("conversion"),
			c.acceptKw("default", "conversion"):
			relation, id = false, "R1.P2"
			if c.peekKw("configuration") || c.peekKw("dictionary") || c.peekKw("parser") || c.peekKw("template") {
				c.i++
			}
		default:
			return
		}
		c.acceptKw("if", "not", "exists")
		n, err := c.name()
		if err != nil {
			return
		}
		q = n
	case c.peekKw("select"):
		into := depth0(toks, 0, "into")
		if into < 0 {
			return
		}
		c.i = into + 1
		for c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("unlogged") || c.acceptKw("local") || c.acceptKw("global") || c.acceptKw("table") {
		}
		n, err := c.name()
		if err != nil {
			return
		}
		q = n
	default:
		return
	}
	if q.schema == nil {
		return
	}
	switch {
	case relation && (*q.schema == "pg_catalog" || *q.schema == "pg_toast"):
		s.unknownFact(fmt.Sprintf("the statement creates a relation in %s: permission denied to create %s, system catalog modifications are disallowed (Postgres refuses, 42501)%s", *q.schema, q, precondition(id)))
	case *q.schema == "pg_catalog" || *q.schema == "pg_toast":
		s.unknownFact(fmt.Sprintf("the statement creates %s in %s, which needs CREATE on that schema; E6 does not grant it%s", q, *q.schema, precondition("R1.P2")))
	}
}

// numericJunk applies Y22 to a numeric literal starting at p (a digit, or
// a dot before a digit): it returns why scan.l REL_17_6 refuses it, or ""
// when the lexer accepts it. The rules are scan.l's (395-438, 1020-1077),
// matched longest first and, among matches of one length, in scan.l's
// order: decinteger, hexinteger, octinteger, bininteger, hexfail,
// octfail, binfail, numeric, numericfail, real, realfail, integer_junk,
// numeric_junk, real_junk. A numeric literal followed directly by another
// one (1.2.3) is two constants in a row, which gram.y refuses.
func numericJunk(src []byte, p int) string {
	at := func(j int) byte {
		if j < len(src) {
			return src[j]
		}
		return 0
	}
	digits := func(j int, ok func(byte) bool) int {
		// (_?d)+ after a prefix; -1 when none.
		end := -1
		for {
			k := j
			if at(k) == '_' {
				k++
			}
			if !ok(at(k)) {
				return end
			}
			j, end = k+1, k+1
		}
	}
	dec := func(j int) int {
		// {decinteger}: d(_?d)*.
		if !isDigit(at(j)) {
			return -1
		}
		if e := digits(j+1, isDigit); e > 0 {
			return e
		}
		return j + 1
	}
	identEnd := func(j int) int {
		c := at(j)
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80) {
			return -1
		}
		j++
		for c := at(j); c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c >= 0x80 || isDigit(c) || c == '$'; c = at(j) {
			j++
		}
		return j
	}
	numeric := func(j int) int {
		if e := dec(j); e > 0 {
			if at(e) == '.' && at(e+1) != '.' {
				if f := dec(e + 1); f > 0 {
					return f
				}
				return e + 1
			}
			return -1
		}
		if at(j) == '.' {
			return dec(j + 1)
		}
		return -1
	}
	type match struct {
		end int
		why string
	}
	var ms []match
	add := func(end int, why string) {
		if end > p {
			ms = append(ms, match{end, why})
		}
	}
	add(dec(p), "")
	isHex := func(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	isOct := func(c byte) bool { return c >= '0' && c <= '7' }
	isBin := func(c byte) bool { return c == '0' || c == '1' }
	prefixed := []struct {
		x   byte
		ok  func(byte) bool
		why string
	}{{'x', isHex, "invalid hexadecimal integer"}, {'o', isOct, "invalid octal integer"}, {'b', isBin, "invalid binary integer"}}
	if at(p) == '0' {
		for _, f := range prefixed {
			if at(p+1)|0x20 == f.x {
				add(digits(p+2, f.ok), "")
			}
		}
		for _, f := range prefixed {
			if at(p+1)|0x20 == f.x {
				e := p + 2
				if at(e) == '_' {
					e++
				}
				add(e, f.why)
			}
		}
	}
	num := numeric(p)
	add(num, "")
	if e := dec(p); e > 0 && at(e) == '.' && at(e+1) == '.' {
		add(e, "") // numericfail: the integer, then ".."
	}
	mantissa := []int{dec(p), num}
	for _, m := range mantissa {
		if m > 0 && at(m)|0x20 == 'e' {
			e := m + 1
			if at(e) == '-' || at(e) == '+' {
				e++
			}
			add(dec(e), "")
		}
	}
	for _, m := range mantissa {
		if m > 0 && at(m)|0x20 == 'e' && (at(m+1) == '-' || at(m+1) == '+') {
			add(m+2, "trailing junk after numeric literal")
		}
	}
	if d := dec(p); d > 0 {
		add(identEnd(d), "trailing junk after numeric literal")
	}
	if num > 0 {
		add(identEnd(num), "trailing junk after numeric literal")
	}
	for _, m := range mantissa {
		if m > 0 && at(m)|0x20 == 'e' {
			e := m + 1
			if at(e) == '-' || at(e) == '+' {
				e++
			}
			if r := dec(e); r > 0 {
				add(identEnd(r), "trailing junk after numeric literal")
			}
		}
	}
	best := match{end: -1}
	for _, m := range ms {
		if m.end > best.end {
			best = m
		}
	}
	if best.end < 0 {
		return ""
	}
	if best.why != "" {
		return best.why
	}
	if numeric(best.end) > 0 && at(best.end) == '.' {
		return "syntax error: two numeric constants in a row"
	}
	return ""
}

// rev19Junk applies Y22 to one migration: Postgres lexes and parses the
// whole migration string before it runs any statement (postgres.c
// exec_simple_query), so a lexer error anywhere refuses the whole
// migration.
func (s *state) rev19Junk(statements []statement) {
	for _, st := range statements {
		for _, t := range st.tokens {
			if t.kind != tokNumber {
				continue
			}
			if why := numericJunk(s.m.source, t.start); why != "" {
				s.at = byteSpan{st.start, st.end}
				s.unknownFact(fmt.Sprintf("the migration does not parse: %q: %s (Postgres refuses the whole migration, 42601)%s", s.m.source[t.start:t.end], why, precondition("Q1.P7")))
				return
			}
		}
	}
}

// y19Kind returns the relation kind the pass holds under q (Y05, Y10,
// Y12, Y30, Y31, Y42): "table", "partitioned table", or a relationAt
// kind, with every other kind q may name (candidates) and whether the
// held kind is certain. A name the pass does not hold has kind "".
func (s *state) y19Kind(q qname) (kind string, candidates []string, certain bool) {
	pathOK := q.schema != nil || !s.pathChanged
	exact, others := s.lookup(q)
	tableKind := func(t *table) string {
		if t.partitionKey != nil {
			return "partitioned table"
		}
		return "table"
	}
	for _, t := range others {
		candidates = append(candidates, tableKind(t))
	}
	for _, k := range s.relationKeys() {
		if k == q.String() || q.schema == nil && strings.HasSuffix(k, "."+q.name) || q.schema != nil && k == q.name {
			candidates = append(candidates, s.relations[k].kind)
		}
	}
	if exact != nil {
		kind = tableKind(exact)
		certain = pathOK && len(candidates) == 0 && !exact.kindUnknown && !exact.mayBeOther
		if exact.kindUnknown || exact.mayBeOther {
			candidates = append(candidates, "relation of another kind")
		}
		return kind, candidates, certain
	}
	kind, certain, found := s.relationAt(q)
	if !found {
		return "", candidates, false
	}
	if kind == "generated index or sequence" {
		// An unwritten generated name is an index unless it is also a
		// serial or identity sequence's (heldAtt).
		if s.heldAtt(q, kind) == attIndex {
			return "index", candidates, false
		}
		return kind, append(candidates, "index", "sequence"), false
	}
	if len(candidates) > 1 {
		certain = false
	}
	return kind, candidates, certain
}

// y19KindCheck refuses a statement whose target q may be a relation kind
// it does not accept: refused when the held kind is certain and not
// accepted, may fail when q may be of a kind not accepted. It reports
// whether it recorded a failure.
func (s *state) y19KindCheck(q qname, accepts func(kind string) bool, what, sqlstate, id string) bool {
	kind, candidates, certain := s.y19Kind(q)
	if kind == "" && len(candidates) == 0 {
		return false
	}
	if kind != "" && !accepts(kind) {
		if certain {
			s.unknownFact(fmt.Sprintf("%s: %s is %s (Postgres refuses, %s)%s", what, q, article(kind), sqlstate, precondition(id)))
		} else {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s may be %s%s", what, q, article(kind), precondition(id)))
		}
		return true
	}
	for _, k := range candidates {
		if !accepts(k) {
			s.unknownFact(fmt.Sprintf("%s may be refused: %s may name %s%s", what, q, article(k), precondition(id)))
			return true
		}
	}
	return false
}

// triggerStmt is CREATE [OR REPLACE] [CONSTRAINT] TRIGGER as Y09-Y11 read
// it (gram.y CreateTrigStmt).
type triggerStmt struct {
	constraint                 bool
	timing                     string // before, after, instead
	insert, update, del, trunc bool
	columns                    bool
	table                      qname
	row, when                  bool
	newTables, oldTables       []string
	rowVariable                bool
	function                   qname
	functionArgs               bool
}

// readTrigger reads a CREATE TRIGGER; ok is false when it cannot.
func readTrigger(toks []token, src []byte) (triggerStmt, bool) {
	var t triggerStmt
	c := &cursor{toks: toks, i: 1, src: src}
	c.acceptKw("or", "replace")
	t.constraint = c.acceptKw("constraint")
	if !c.acceptKw("trigger") {
		return t, false
	}
	if _, err := c.ident(); err != nil {
		return t, false
	}
	switch {
	case c.acceptKw("before"):
		t.timing = "before"
	case c.acceptKw("after"):
		t.timing = "after"
	case c.acceptKw("instead", "of"):
		t.timing = "instead"
	default:
		return t, false
	}
	for {
		switch {
		case c.acceptKw("insert"):
			t.insert = true
		case c.acceptKw("delete"):
			t.del = true
		case c.acceptKw("truncate"):
			t.trunc = true
		case c.acceptKw("update"):
			t.update = true
			if c.acceptKw("of") {
				t.columns = true
				for !c.done() && !c.peekKw("on") && !c.peekKw("or") {
					c.i++
				}
			}
		default:
			return t, false
		}
		if !c.acceptKw("or") {
			break
		}
	}
	if !c.acceptKw("on") {
		return t, false
	}
	q, err := c.name()
	if err != nil {
		return t, false
	}
	t.table = q
	t.row = t.constraint
	for !c.done() {
		switch {
		case c.acceptKw("referencing"):
			for {
				isNew := c.peekKw("new")
				if !c.acceptKw("new") && !c.acceptKw("old") {
					break
				}
				switch {
				case c.acceptKw("table"):
				case c.acceptKw("row"):
					t.rowVariable = true
				default:
					return t, false
				}
				c.acceptKw("as")
				n, err := c.ident()
				if err != nil {
					return t, false
				}
				if isNew {
					t.newTables = append(t.newTables, n)
				} else {
					t.oldTables = append(t.oldTables, n)
				}
			}
		case c.acceptKw("for"):
			c.acceptKw("each")
			switch {
			case c.acceptKw("row"):
				t.row = true
			case c.acceptKw("statement"):
				t.row = false
			default:
				return t, false
			}
		case c.peekKw("when"):
			t.when = true
			c.i++
			e, err := c.group()
			if err != nil {
				return t, false
			}
			c.i = e
		case c.acceptKw("execute"):
			if !c.acceptKw("function") && !c.acceptKw("procedure") {
				return t, false
			}
			f, err := c.funcName()
			if err != nil || !c.isPunct("(") {
				return t, false
			}
			t.function = f
			e, err := c.group()
			if err != nil {
				return t, false
			}
			t.functionArgs = e > c.i+2
			c.i = e
			return t, c.done()
		default:
			c.i++
		}
	}
	return t, false
}

// y19Trigger applies Y09, Y10 and Y11 to CREATE TRIGGER (and to the
// statement an O1t call runs): trigger.c CreateTriggerFiringOn 217-330
// (relation kinds), 381-397 (TRUNCATE FOR EACH ROW, INSTEAD OF), 420-560
// (transition tables), 691-703 (the function: LookupFuncName with no
// arguments, then a trigger result).
func (s *state) y19Trigger(toks []token) {
	t, ok := readTrigger(toks, s.m.source)
	if !ok {
		return
	}
	what := fmt.Sprintf("CREATE TRIGGER on %s", t.table)
	refuse := func(why, sqlstate string) {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("N1.P4")))
	}
	transition := len(t.newTables)+len(t.oldTables) > 0
	events := 0
	for _, e := range []bool{t.insert, t.update, t.del} {
		if e {
			events++
		}
	}
	switch {
	// Y11: clauses decided by the text.
	case t.row && t.trunc:
		refuse("TRUNCATE FOR EACH ROW triggers are not supported", "0A000")
	case t.timing == "instead" && !t.row:
		refuse("INSTEAD OF triggers must be FOR EACH ROW", "0A000")
	case t.timing == "instead" && t.when:
		refuse("INSTEAD OF triggers cannot have WHEN conditions", "0A000")
	case t.timing == "instead" && t.columns:
		refuse("INSTEAD OF triggers cannot have column lists", "0A000")
	case t.rowVariable:
		refuse("ROW variable naming in the REFERENCING clause is not supported", "0A000")
	case transition && t.constraint:
		refuse("a constraint trigger cannot have transition tables", "42601")
	case transition && t.timing != "after":
		refuse("transition table name can only be specified for an AFTER trigger", "42P17")
	case transition && t.trunc:
		refuse("TRUNCATE triggers with transition tables are not supported", "0A000")
	case transition && events != 1:
		refuse("transition tables cannot be specified for triggers with more than one event", "0A000")
	case transition && t.columns:
		refuse("transition tables cannot be specified for triggers with column lists", "0A000")
	case len(t.newTables) > 0 && !t.insert && !t.update:
		refuse("NEW TABLE can only be specified for an INSERT or UPDATE trigger", "42P17")
	case len(t.oldTables) > 0 && !t.del && !t.update:
		refuse("OLD TABLE can only be specified for a DELETE or UPDATE trigger", "42P17")
	case len(t.newTables) > 1:
		refuse("NEW TABLE cannot be specified multiple times", "42P17")
	case len(t.oldTables) > 1:
		refuse("OLD TABLE cannot be specified multiple times", "42P17")
	case len(t.newTables) == 1 && len(t.oldTables) == 1 && t.newTables[0] == t.oldTables[0]:
		refuse("OLD TABLE name and NEW TABLE name cannot be the same", "42P17")
	}
	if s.failure != nil {
		return
	}
	// Y10: the relation kind.
	accepts := func(kind string) bool {
		switch kind {
		case "table":
			return t.timing != "instead"
		case "partitioned table":
			return t.timing != "instead"
		case "view":
			return (t.timing == "instead" || !t.row) && !t.trunc && !transition
		case "foreign table":
			return t.timing != "instead" && !t.constraint && !transition
		}
		return false
	}
	if s.y19KindCheck(t.table, accepts, what, "42809", "N1.P4") {
		return
	}
	if kind, _, _ := s.y19Kind(t.table); transition && t.row && (kind == "partitioned table" || s.inheritsOrPartition(t.table)) {
		refuse("ROW triggers with transition tables are not supported on partitioned tables, partitions or inheritance children", "0A000")
		return
	}
	// Y09: the trigger function.
	s.triggerFunction(t.function, what)
}

// inheritsOrPartition reports whether the table q names is a partition or
// an inheritance child the pass holds.
func (s *state) inheritsOrPartition(q qname) bool {
	exact, _ := s.lookup(q)
	if exact == nil {
		return false
	}
	if exact.partitionOf != nil {
		return true
	}
	for _, p := range s.tables {
		for _, c := range p.children {
			if c.name == exact.q.name && sameSchema(c.schema, exact.q.schema) {
				return true
			}
		}
	}
	return false
}

// triggerFunction applies Y09: a trigger function named f is looked up
// with no arguments and must return trigger (trigger.c 691, 703). It is
// decided only for a set routine whose one definition the pass read.
func (s *state) triggerFunction(f qname, what string) {
	if s.routineDefs == nil {
		return
	}
	defs := s.routineDefs[f.name]
	if len(defs) == 0 && !s.routineUnknown[f.name] {
		return
	}
	if s.routineUnknown[f.name] || len(defs) > 1 {
		s.unknownFact(fmt.Sprintf("%s may be refused: the set holds more than one routine %s, or changed it in a way the pass did not read, so whether %s() is a trigger function is not proven%s", what, f.name, f.name, precondition("N1.P4")))
		return
	}
	d := defs[0]
	if f.schema != nil && d.schema != nil && *f.schema != *d.schema {
		return
	}
	// public.f names the set's unqualified f when that was created with
	// E4's search_path ("$user", public), where "$user" names no schema
	// (V6, pathUnknown): an unqualified CREATE puts it in public.
	publicName := f.schema != nil && *f.schema == "public" && d.schema == nil && !d.pathUnknown
	if !d.paramsRead || d.pathUnknown || f.schema != nil && d.schema == nil && !publicName || f.schema == nil && d.schema != nil {
		s.unknownFact(fmt.Sprintf("%s may be refused: whether %s() is the set's routine, and a trigger function, is not proven%s", what, f.name, precondition("N1.P4")))
		return
	}
	switch {
	case d.kind == "procedure" || len(d.inputs) > 0:
		s.unknownFact(fmt.Sprintf("%s: function %s() does not exist (the set's %s takes arguments or is a procedure; Postgres refuses, 42883)%s", what, f.name, f.name, precondition("N1.P4")))
	case !d.defKnown && d.ret == "":
		s.unknownFact(fmt.Sprintf("%s may be refused: the result type of %s() is not proven%s", what, f.name, precondition("N1.P4")))
	case d.ret != "trigger":
		s.unknownFact(fmt.Sprintf("%s: function %s must return type trigger (it returns %s; Postgres refuses, 42P17)%s", what, f.name, d.ret, precondition("N1.P4")))
	}
}

// policyStmt is CREATE POLICY n ON t [AS ...] [FOR cmd] [TO ...] [USING
// (...)] [WITH CHECK (...)], or ALTER POLICY n ON t ..., as Y12 reads it.
type policyStmt struct {
	create          bool
	name            string
	table           qname
	command         string
	using, check    bool
	rename, renamed string
}

func readPolicy(toks []token, src []byte) (policyStmt, bool) {
	var p policyStmt
	c := &cursor{toks: toks, src: src}
	p.create = c.acceptKw("create", "policy")
	if !p.create && !c.acceptKw("alter", "policy") {
		return p, false
	}
	n, err := c.ident()
	if err != nil || !c.acceptKw("on") {
		return p, false
	}
	p.name = n
	q, err := c.name()
	if err != nil {
		return p, false
	}
	p.table = q
	if c.acceptKw("rename", "to") {
		m, err := c.ident()
		if err != nil {
			return p, false
		}
		p.renamed = m
		return p, c.done()
	}
	if p.create {
		p.command = "all"
		if c.acceptKw("as") {
			c.i++
		}
		if c.acceptKw("for") {
			if c.done() {
				return p, false
			}
			p.command = c.tok().value
			c.i++
		}
	}
	p.using = depth0(toks, c.i, "using") >= 0
	if j := depth0(toks, c.i, "check"); j > 0 && toks[j-1].kind == tokIdent && toks[j-1].value == "with" {
		p.check = true
	}
	return p, true
}

// y19Policy applies Y12 (policy.c CreatePolicy 598-613, AlterPolicy;
// RangeVarCallbackForPolicy 89-93): FOR INSERT takes no USING, FOR SELECT
// and DELETE no WITH CHECK (42601), and a policy is only on a table or a
// partitioned table (42809). ALTER POLICY takes the command of a policy
// the pass holds.
func (s *state) y19Policy(toks []token) {
	p, ok := readPolicy(toks, s.m.source)
	if !ok {
		return
	}
	what := fmt.Sprintf("%s POLICY %s ON %s", map[bool]string{true: "CREATE", false: "ALTER"}[p.create], p.name, p.table)
	command := p.command
	if !p.create {
		if exact, _ := s.lookup(p.table); exact != nil {
			command = exact.policyCommands[p.name]
		}
	}
	switch {
	case command == "insert" && p.using:
		s.unknownFact(fmt.Sprintf("%s: only WITH CHECK expression allowed for INSERT (Postgres refuses, 42601)%s", what, precondition("N1.P4")))
		return
	case (command == "select" || command == "delete") && p.check:
		s.unknownFact(fmt.Sprintf("%s: WITH CHECK cannot be applied to SELECT or DELETE (Postgres refuses, 42601)%s", what, precondition("N1.P4")))
		return
	}
	s.y19KindCheck(p.table, func(kind string) bool { return kind == "table" || kind == "partitioned table" }, what, "42809", "N1.P4")
}

// policyRecord keeps policyCommands through CREATE POLICY, ALTER POLICY
// ... RENAME TO and DROP POLICY on a table the pass holds.
//
// It also keeps the SB-M3 record of policies (table.policies) of a
// Supabase set: CREATE POLICY adds the policy as written (command ALL
// when FOR is absent), DROP POLICY removes it, ALTER POLICY ... RENAME TO
// renames it, and any other ALTER POLICY keeps it. A policy statement the
// pass cannot read, or whose table name may mean several held tables,
// makes the record of the tables it may name unknown.
func (s *state) policyRecord(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	isPolicy := c.peekKw("create", "policy") || c.peekKw("alter", "policy") || c.peekKw("drop", "policy")
	if isPolicy && s.set.tool == toolSupabase {
		s.set.supabaseRows["SB-M3"]++
	}
	if c.acceptKw("drop", "policy") {
		c.acceptKw("if", "exists")
		n, err := c.ident()
		if err != nil || !c.acceptKw("on") {
			s.policiesUnread()
			return
		}
		q, err := c.name()
		if err != nil {
			s.policiesUnread()
			return
		}
		exact, others := s.lookup(q)
		if exact != nil {
			delete(exact.policyCommands, n)
			exact.dropPolicy(n)
		}
		if exact != nil && len(others) > 0 {
			exact.policiesUnknown = true
		}
		for _, o := range others {
			o.policiesUnknown = true
		}
		return
	}
	p, ok := readPolicy(toks, s.m.source)
	if !ok {
		if isPolicy {
			s.policiesUnread()
		}
		return
	}
	exact, others := s.lookup(p.table)
	if exact == nil {
		// The name may still mean a held table under another
		// qualification (public.t and t): its policies are unknown.
		for _, o := range others {
			o.policiesUnknown = true
		}
		return
	}
	if len(others) > 0 {
		exact.policiesUnknown = true
		for _, o := range others {
			o.policiesUnknown = true
		}
	}
	switch {
	case p.create:
		if exact.policyCommands == nil {
			exact.policyCommands = map[string]string{}
		}
		exact.policyCommands[p.name] = p.command
		exact.addPolicy(tablePolicy{name: p.name, command: strings.ToUpper(p.command)})
	case p.renamed != "":
		if cmd, ok := exact.policyCommands[p.name]; ok {
			delete(exact.policyCommands, p.name)
			exact.policyCommands[p.renamed] = cmd
		}
		exact.renamePolicy(p.name, p.renamed)
	}
}

// policiesUnread makes the policy record of every held table unknown
// after a policy statement the pass could not read (SB-M3).
func (s *state) policiesUnread() {
	for _, t := range s.tables {
		t.policiesUnknown = true
	}
}

// y19Lock applies Y05: LOCK [TABLE] [ONLY] n [*], ... takes a table, a
// partitioned table or a view (lockcmds.c RangeVarCallbackForLockTable
// 87-93, 42809 "cannot lock relation"); a view's base relations are
// locked whatever their kinds.
func (s *state) y19Lock(toks []token) {
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	c.acceptKw("table")
	for {
		c.acceptKw("only")
		q, err := c.name()
		if err != nil {
			return
		}
		c.acceptPunct("*")
		accepts := func(kind string) bool { return kind == "table" || kind == "partitioned table" || kind == "view" }
		if s.y19KindCheck(q, accepts, "LOCK "+q.String(), "42809", "T1.P4") || !c.acceptPunct(",") {
			return
		}
	}
}

// commentKinds maps the object word of COMMENT ON to the kinds the pass
// records that it accepts (objectaddress.c get_relation_by_qualified_name
// 1340-1392, get_object_address_type 1610-1629: TYPE takes every type,
// which every relation but a sequence and an index has as its row type;
// DOMAIN only a domain).
var commentKinds = map[string]func(kind string) bool{
	"table":             func(k string) bool { return k == "table" || k == "partitioned table" },
	"view":              func(k string) bool { return k == "view" },
	"materialized view": func(k string) bool { return k == "materialized view" },
	"sequence":          func(k string) bool { return k == "sequence" },
	"foreign table":     func(k string) bool { return k == "foreign table" },
	"index":             func(k string) bool { return k == "index" },
	"type":              func(k string) bool { return k != "sequence" && k != "index" && k != "generated index or sequence" },
	"domain":            func(k string) bool { return k == "domain" },
}

// y19Comment applies Y42 to COMMENT ON TABLE, VIEW, MATERIALIZED VIEW,
// SEQUENCE, FOREIGN TABLE, INDEX, TYPE and DOMAIN of a name the pass
// holds as another kind (42809 "is not a table", "is not a domain", ...;
// 42704 for TYPE or DOMAIN of a relation without a row type).
func (s *state) y19Comment(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	var word string
	switch {
	case c.acceptKw("materialized", "view"):
		word = "materialized view"
	case c.acceptKw("foreign", "table"):
		word = "foreign table"
	case c.acceptKw("table"), c.acceptKw("view"), c.acceptKw("sequence"), c.acceptKw("index"), c.acceptKw("type"), c.acceptKw("domain"):
		word = toks[c.i-1].value
	default:
		return
	}
	q, err := c.name()
	if err != nil || !c.peekKw("is") {
		return
	}
	s.y19KindCheck(q, commentKinds[word], fmt.Sprintf("COMMENT ON %s %s", strings.ToUpper(word), q), "42809", "N1.P4")
}

// publishActions are the values of the publish option
// (publicationcmds.c parse_publication_options 143).
var publishActions = map[string]bool{"insert": true, "update": true, "delete": true, "truncate": true}

// y19Publication applies Y31 to CREATE PUBLICATION and ALTER
// PUBLICATION: the WITH options (publish, publish_via_partition_root,
// each once; publish a list of insert, update, delete, truncate) and the
// relations of FOR TABLE, ADD TABLE and SET TABLE, which must be tables
// or partitioned tables, neither unlogged nor temporary
// (pg_publication.c check_publication_add_relation 66, 22023).
func (s *state) y19Publication(toks []token) {
	what := "CREATE PUBLICATION"
	if toks[0].value == "alter" {
		what = "ALTER PUBLICATION"
	}
	refuse := func(why, sqlstate string) {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("N1.P4")))
	}
	if w := depth0(toks, 2, "with"); w >= 0 && w+1 < len(toks) && toks[w+1].kind == tokPunct && toks[w+1].value == "(" {
		c := &cursor{toks: toks, i: w + 1, src: s.m.source}
		end, err := c.group()
		if err != nil {
			return
		}
		seen := map[string]bool{}
		for _, r := range c.splitList(w+2, end-1) {
			if r[0] >= r[1] {
				continue
			}
			o := toks[r[0]]
			name := o.value
			if seen[name] {
				refuse(fmt.Sprintf("conflicting or redundant options: %s", name), "42601")
				return
			}
			seen[name] = true
			val := toks[r[0]+1 : r[1]]
			if len(val) > 0 && val[0].kind == tokOp && val[0].value == "=" {
				val = val[1:]
			}
			switch name {
			case "publish":
				if len(val) != 1 || val[0].kind != tokString {
					s.unknownFact(fmt.Sprintf("%s may be refused: its publish value is not a string the pass reads%s", what, precondition("N1.P4")))
					return
				}
				text, ok := tokenText(val[0], s.m.source)
				if !ok {
					return
				}
				for _, a := range strings.Split(text, ",") {
					a = strings.ToLower(strings.TrimSpace(a))
					if a != "" && !publishActions[a] {
						refuse(fmt.Sprintf("unrecognized value for publication option \"publish\": %q", a), "42601")
						return
					}
				}
			case "publish_via_partition_root":
				if len(val) > 1 || len(val) == 1 && !booleanWord(val[0]) {
					s.unknownFact(fmt.Sprintf("%s may be refused: its publish_via_partition_root value is not a boolean the pass reads%s", what, precondition("N1.P4")))
					return
				}
			default:
				refuse(fmt.Sprintf("unrecognized publication parameter: %q", name), "42601")
				return
			}
		}
	}
	for j := 2; j+1 < len(toks); j++ {
		if toks[j].kind != tokIdent || toks[j].value != "table" || toks[j-1].kind != tokIdent || !(toks[j-1].value == "for" || toks[j-1].value == "add" || toks[j-1].value == "set" || toks[j-1].value == "drop") {
			continue
		}
		if toks[j-1].value == "drop" {
			return
		}
		c := &cursor{toks: toks, i: j + 1, src: s.m.source}
		for !c.done() {
			c.acceptKw("table")
			c.acceptKw("only")
			q, err := c.name()
			if err != nil {
				return
			}
			c.acceptPunct("*")
			if s.y19KindCheck(q, func(k string) bool { return k == "table" || k == "partitioned table" }, what, "22023", "N1.P4") {
				return
			}
			if exact, _ := s.lookup(q); exact != nil && exact.unlogged {
				refuse(fmt.Sprintf("cannot add relation %s to publication: unlogged relations cannot be replicated", q), "22023")
				return
			}
			if q.schema == nil && s.temps[q.name] {
				s.unknownFact(fmt.Sprintf("%s may be refused: %s may be a temporary table%s", what, q, precondition("N1.P4")))
				return
			}
			for !c.done() && !c.isPunct(",") && !c.peekKw("with") {
				if c.isPunct("(") {
					e, err := c.group()
					if err != nil {
						return
					}
					c.i = e
					continue
				}
				c.i++
			}
			if !c.acceptPunct(",") || c.peekKw("tables") {
				break
			}
		}
		return
	}
}

// booleanWord reports a token defGetBoolean reads as a boolean.
func booleanWord(t token) bool {
	v := strings.ToLower(t.value)
	switch t.kind {
	case tokIdent, tokString:
		switch v {
		case "true", "false", "on", "off", "yes", "no", "t", "f", "y", "n", "1", "0":
			return true
		}
	case tokNumber:
		return v == "0" || v == "1"
	}
	return false
}

// y19AlterPartitionedIndex applies Y30 to ALTER INDEX [IF EXISTS] i SET
// (...) / RESET (...) of a partitioned index (tablecmds.c
// ATSimplePermissions 6588: 42809).
func (s *state) y19AlterPartitionedIndex(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	c.acceptKw("if", "exists")
	q, err := c.name()
	if err != nil || !(c.peekKw("set") || c.peekKw("reset")) || c.i+1 >= len(toks) || toks[c.i+1].kind != tokPunct || toks[c.i+1].value != "(" {
		return
	}
	t, _ := s.findIndex(q)
	if t == nil {
		return
	}
	switch {
	case t.partitionKey != nil:
		s.unknownFact(fmt.Sprintf("ALTER INDEX %s %s (...): %s is a partitioned index (Postgres refuses, 42809)%s", q, strings.ToUpper(toks[c.i].value), q, precondition("M5k-a.P4")))
	case t.kindUnknown || t.mayBeOther:
		s.unknownFact(fmt.Sprintf("ALTER INDEX %s %s (...) may be refused: whether %s is partitioned is not proven%s", q, strings.ToUpper(toks[c.i].value), t.q, precondition("M5k-a.P4")))
	}
}

// y19Reindex applies Y30 to REINDEX INDEX of a partitioned index and
// REINDEX TABLE of a partitioned table inside a transaction block
// (indexcmds.c ReindexPartitions 3243: PreventInTransactionBlock, 25001).
func (s *state) y19Reindex(toks []token) {
	if !s.inBlock {
		return
	}
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	if c.isPunct("(") {
		e, err := c.group()
		if err != nil {
			return
		}
		c.i = e
	}
	index := c.acceptKw("index")
	if !index && !c.acceptKw("table") {
		return
	}
	if c.peekKw("concurrently") {
		return
	}
	q, err := c.name()
	if err != nil {
		return
	}
	var t *table
	if index {
		t, _ = s.findIndex(q)
	} else {
		t, _ = s.lookup(q)
	}
	if t == nil {
		return
	}
	what := fmt.Sprintf("REINDEX TABLE %s inside a transaction block", q)
	if index {
		what = fmt.Sprintf("REINDEX INDEX %s inside a transaction block", q)
	}
	switch {
	case t.partitionKey != nil:
		s.unknownFact(fmt.Sprintf("%s: %s is partitioned, and REINDEX of a partitioned relation cannot run inside a transaction block (Postgres refuses, 25001)%s", what, t.q, precondition("N3.P6")))
	case t.kindUnknown || t.mayBeOther:
		s.unknownFact(fmt.Sprintf("%s may be refused: whether %s is partitioned is not proven%s", what, t.q, precondition("N3.P6")))
	}
}

// roleOptionKeys maps each role option word to the DefElem name gram.y
// gives it (CreateOptRoleElem, AlterOptRoleElem): two options of one
// name conflict (user.c errorConflictingDefElem, 42601).
var roleOptionKeys = map[string]string{
	"superuser": "superuser", "nosuperuser": "superuser", "createdb": "createdb", "nocreatedb": "createdb",
	"createrole": "createrole", "nocreaterole": "createrole", "inherit": "inherit", "noinherit": "inherit",
	"login": "canlogin", "nologin": "canlogin", "replication": "isreplication", "noreplication": "isreplication",
	"bypassrls": "bypassrls", "nobypassrls": "bypassrls",
}

// roleOptions applies Y14 to the option list of CREATE ROLE / USER /
// GROUP and ALTER ROLE (user.c CreateRole 194-265 and AlterRole: an
// option twice, 42601; CONNECTION LIMIT below -1, 22023 at 299, 739;
// VALID UNTIL is timestamptz input, proven only for 'infinity',
// '-infinity' and the ISO forms validUntil reads).
func (s *state) roleOptions(c *cursor, what string, create bool) {
	seen := map[string]bool{}
	refuse := func(why, sqlstate string) {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("N1.P7")))
	}
	c.acceptKw("with")
	for !c.done() {
		key := ""
		start := c.i
		switch {
		case c.acceptKw("password"), c.acceptKw("encrypted", "password"):
			key = "password"
			if !c.acceptKw("null") {
				if c.tok().kind != tokString {
					return
				}
				c.i++
			}
		case c.acceptKw("connection", "limit"):
			key = "connectionlimit"
			neg := c.tok().kind == tokOp && c.tok().value == "-"
			if neg || c.tok().kind == tokOp && c.tok().value == "+" {
				c.i++
			}
			if c.tok().kind != tokNumber {
				return
			}
			n, err := strconv.ParseInt(c.tok().value, 10, 64)
			c.i++
			if err != nil {
				s.unknownFact(fmt.Sprintf("%s may be refused: CONNECTION LIMIT %s is not proven%s", what, c.text(start+2, c.i), precondition("N1.P7")))
				return
			}
			if neg && n > 1 {
				refuse(fmt.Sprintf("invalid connection limit: -%d", n), "22023")
				return
			}
		case c.acceptKw("valid", "until"):
			key = "validuntil"
			if c.tok().kind != tokString {
				return
			}
			v, ok := tokenText(c.tok(), c.src)
			c.i++
			if !ok || !validUntilProven(v) {
				s.unknownFact(fmt.Sprintf("%s may be refused: VALID UNTIL %q is timestamptz input the pass does not prove%s", what, v, precondition("N1.P5")))
				return
			}
		case create && (c.acceptKw("in", "role") || c.acceptKw("in", "group")):
			key = "addroleto"
		case create && (c.acceptKw("role") || c.acceptKw("user")):
			key = "rolemembers"
		case create && c.acceptKw("admin"):
			key = "adminmembers"
		case create && c.acceptKw("sysid"):
			key = "sysid"
			c.i++
		case c.tok().kind == tokIdent && roleOptionKeys[c.tok().value] != "":
			key = roleOptionKeys[c.tok().value]
			c.i++
		default:
			return
		}
		if key == "addroleto" || key == "rolemembers" || key == "adminmembers" {
			for {
				if _, err := c.ident(); err != nil {
					return
				}
				if !c.acceptPunct(",") {
					break
				}
			}
		}
		if seen[key] {
			refuse(fmt.Sprintf("conflicting or redundant options: %s", c.text(start, start+1)), "42601")
			return
		}
		seen[key] = true
	}
}

// validUntilProven reports a VALID UNTIL value whose timestamptz input
// the pass proves accepted: 'infinity', '-infinity', or
// YYYY-MM-DD[ HH:MM[:SS[.frac]]][zone] with each field in range, zone
// Z or +HH, -HH, +HH:MM, -HH:MM (datetime.c DecodeDateTime under the ISO
// forms every DateStyle reads alike).
func validUntilProven(v string) bool {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "infinity") || strings.EqualFold(v, "-infinity") {
		return true
	}
	num := func(s string, n int) (int, bool) {
		if len(s) != n {
			return 0, false
		}
		x, err := strconv.Atoi(s)
		return x, err == nil && !strings.ContainsAny(s, "+-")
	}
	if len(v) < 10 {
		return false
	}
	y, ok1 := num(v[0:4], 4)
	mo, ok2 := num(v[5:7], 2)
	d, ok3 := num(v[8:10], 2)
	if !ok1 || !ok2 || !ok3 || v[4] != '-' || v[7] != '-' || y < 1 || mo < 1 || mo > 12 || d < 1 {
		return false
	}
	days := []int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[mo-1]
	if mo == 2 && (y%4 == 0 && y%100 != 0 || y%400 == 0) {
		days = 29
	}
	if d > days {
		return false
	}
	rest := v[10:]
	if rest == "" {
		return true
	}
	if rest[0] != ' ' || len(rest) < 6 {
		return false
	}
	rest = rest[1:]
	h, ok1 := num(rest[0:2], 2)
	mi, ok2 := num(rest[3:5], 2)
	if !ok1 || !ok2 || rest[2] != ':' || h > 23 || mi > 59 {
		return false
	}
	rest = rest[5:]
	if len(rest) >= 3 && rest[0] == ':' {
		sec, ok := num(rest[1:3], 2)
		if !ok || sec > 59 {
			return false
		}
		rest = rest[3:]
		if len(rest) > 0 && rest[0] == '.' {
			k := 1
			for k < len(rest) && isDigit(rest[k]) {
				k++
			}
			if k == 1 || k > 7 {
				return false
			}
			rest = rest[k:]
		}
	}
	switch {
	case rest == "" || rest == "Z" || rest == "z":
		return true
	case len(rest) == 3 && (rest[0] == '+' || rest[0] == '-'):
		zh, ok := num(rest[1:3], 2)
		return ok && zh <= 15
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		zh, ok1 := num(rest[1:3], 2)
		zm, ok2 := num(rest[4:6], 2)
		return ok1 && ok2 && zh <= 15 && zm <= 59
	}
	return false
}

// y19DefaultPrivileges applies Y36: ALTER DEFAULT PRIVILEGES with IN
// SCHEMA or FOR ROLE (USER) given twice (aclchk.c
// ExecAlterDefaultPrivilegesStmt 996, 1002: 42601).
func (s *state) y19DefaultPrivileges(toks []token) {
	c := &cursor{toks: toks, i: 3, src: s.m.source}
	seen := map[string]bool{}
	for !c.done() && !c.peekKw("grant") && !c.peekKw("revoke") {
		key := ""
		switch {
		case c.acceptKw("in", "schema"):
			key = "IN SCHEMA"
		case c.acceptKw("for", "role"), c.acceptKw("for", "user"):
			key = "FOR ROLE"
		default:
			return
		}
		if seen[key] {
			s.unknownFact(fmt.Sprintf("ALTER DEFAULT PRIVILEGES: conflicting or redundant options: %s given twice (Postgres refuses, 42601)%s", key, precondition("N1.P7")))
			return
		}
		seen[key] = true
		for !c.done() && !c.peekKw("in") && !c.peekKw("for") && !c.peekKw("grant") && !c.peekKw("revoke") {
			c.i++
		}
	}
}

// y19GrantPublic applies Y37: GRANT privileges ... TO PUBLIC WITH GRANT
// OPTION (aclchk.c merge_acl_with_grant 211: 0LP01), also in ALTER
// DEFAULT PRIVILEGES.
func (s *state) y19GrantPublic(toks []token) {
	g := 0
	if toks[0].value == "alter" {
		g = depth0(toks, 0, "grant")
		if g < 0 {
			return
		}
	}
	n := len(toks)
	if n < 4 || toks[n-1].value != "option" || toks[n-2].value != "grant" || toks[n-3].value != "with" || depth0(toks, g, "on") < 0 {
		return
	}
	to := depth0(toks, g, "to")
	if to < 0 {
		return
	}
	for j := to + 1; j < n-3; j++ {
		if (toks[j].kind == tokIdent || toks[j].kind == tokQuoted) && toks[j].value == "public" {
			s.unknownFact("GRANT ... TO PUBLIC WITH GRANT OPTION: grant options can only be granted to roles (Postgres refuses, 0LP01)" + precondition("N1.P4"))
			return
		}
	}
}

// heldRoutine returns the one definition of a set routine name the pass
// read, or nil (none held, several, or changed unread).
func (s *state) heldRoutine(name string) *routineDef {
	if s.routineDefs == nil || s.routineUnknown[name] || len(s.routineDefs[name]) != 1 {
		return nil
	}
	return s.routineDefs[name][0]
}

// y19AlterRoutine applies Y15 and Y16 to ALTER FUNCTION / PROCEDURE /
// ROUTINE: the action list as CREATE's options (routineOptions:
// functioncmds.c AlterFunction 1427-1439, compute_common_attribute
// 518-565, 634, 775; SET values as N2.P5), and RENAME TO g of a routine
// whose argument types a held routine g of the same schema has
// (IsThereFunctionInNamespace 2053: 42723).
func (s *state) y19AlterRoutine(toks []token) {
	c := &cursor{toks: toks, i: 1, src: s.m.source}
	word := toks[1].value
	c.i++
	f, err := c.funcName()
	if err != nil {
		return
	}
	args := c.isPunct("(")
	var probe *routineDef
	if args {
		e, err := c.group()
		if err != nil {
			return
		}
		pt := append([]token{{kind: tokIdent, value: "x"}}, toks[c.i:e]...)
		probe = parseRoutine(&cursor{toks: pt, src: s.m.source}, "function")
		c.i = e
	}
	held := s.heldRoutine(f.name)
	if held != nil && (f.schema != nil) != (held.schema != nil) || held != nil && f.schema != nil && *f.schema != *held.schema || held != nil && held.pathUnknown {
		held = nil
	}
	if held != nil && probe != nil {
		if equal, known := sameIdentity(&routineDef{schema: held.schema, name: held.name, inputs: probe.inputs, identityKnown: probe.identityKnown}, held); !equal || !known {
			held = nil
		}
	}
	what := fmt.Sprintf("ALTER %s %s", strings.ToUpper(word), f.name)
	switch {
	case c.peekKw("rename", "to"):
		c.i += 2
		g, err := c.ident()
		if err != nil || held == nil || !held.identityKnown || s.routineDefs == nil {
			return
		}
		for _, d := range s.routineDefs[g] {
			equal, known := sameIdentity(&routineDef{schema: held.schema, name: g, inputs: held.inputs, identityKnown: true}, d)
			switch {
			case equal:
				s.unknownFact(fmt.Sprintf("%s RENAME TO %s: function %s(%s) already exists in the schema (Postgres refuses, 42723)%s", what, g, g, strings.Join(held.inputs, ", "), precondition("R6.P2")))
				return
			case !known:
				s.unknownFact(fmt.Sprintf("%s RENAME TO %s may be refused: whether %s has the same argument types is not proven%s", what, g, g, precondition("R6.P2")))
				return
			}
		}
		if s.routineUnknown[g] {
			s.unknownFact(fmt.Sprintf("%s RENAME TO %s may be refused: a routine %s was changed by a statement the pass did not read%s", what, g, g, precondition("R6.P2")))
		}
		return
	case c.peekKw("owner", "to"), c.peekKw("set", "schema"), c.peekKw("depends"), c.peekKw("no", "depends"):
		return
	}
	h := &routineHeader{kind: word, options: c.i, retSet: true}
	known := held != nil && held.defKnown
	if known {
		h.retSet = held.retSet
		h.kind = held.kind
	}
	if word == "routine" && !known {
		h.kind = "function"
	}
	opts := toks
	if n := len(toks); n > 0 && toks[n-1].kind == tokIdent && toks[n-1].value == "restrict" {
		opts = toks[:n-1]
	}
	why, refused := routineOptions(opts, s.m.source, h)
	if strings.HasPrefix(why, "SET search_path: a list value") {
		// A search_path list is read as O3 reads SET search_path.
		why = ""
		for j := h.options; j+1 < len(opts); j++ {
			if opts[j].kind == tokIdent && opts[j].value == "set" && opts[j+1].kind == tokIdent && opts[j+1].value == "search_path" && !searchPathList(opts, j+1) {
				why = "SET search_path: the value is not a list of names"
			}
		}
	}
	if why == "" && !known {
		// ROWS needs a set-returning function, and a ROUTINE's kind
		// decides which options it takes: neither is proven.
		for j := h.options; j < len(opts); j++ {
			v := opts[j].value
			if opts[j].kind == tokIdent && (v == "rows" || word == "routine" && (v == "strict" || v == "immutable" || v == "stable" || v == "volatile" || v == "leakproof" || v == "cost" || v == "parallel" || v == "called")) {
				why = "the routine's kind and result are not proven"
				break
			}
		}
	}
	id := "R6.P4"
	if strings.HasPrefix(why, "SET ") || strings.HasPrefix(why, "RESET ") {
		// Y15: a SET clause's value, as N2.P5.
		id = "R6.P5"
	}
	switch {
	case refused:
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses)%s", what, why, precondition(id)))
	case why != "":
		s.unknownFact(fmt.Sprintf("%s may be refused: %s%s", what, why, precondition(id)))
	}
}

// polymorphic families of check_valid_polymorphic_signature
// (parse_coerce.c 2877-2937).
var (
	polyFamily1 = map[string]bool{"anyelement": true, "anyarray": true, "anynonarray": true, "anyenum": true, "anyrange": true, "anymultirange": true}
	polyFamily2 = map[string]bool{"anycompatible": true, "anycompatiblearray": true, "anycompatiblenonarray": true, "anycompatiblerange": true, "anycompatiblemultirange": true}
)

// polymorphicResult returns why a result (or OUT) type t cannot be
// determined from the input types (ProcedureCreate, pg_proc.c 189-235),
// or "".
func polymorphicResult(t string, inputs []string) string {
	has := func(ok func(string) bool) bool {
		for _, in := range inputs {
			if ok(in) {
				return true
			}
		}
		return false
	}
	switch {
	case t == "anyrange" || t == "anymultirange":
		if !has(func(in string) bool { return in == "anyrange" || in == "anymultirange" }) {
			return "a result of type " + t + " requires at least one input of type anyrange or anymultirange (42P13)"
		}
	case t == "anycompatiblerange" || t == "anycompatiblemultirange":
		if !has(func(in string) bool { return in == "anycompatiblerange" || in == "anycompatiblemultirange" }) {
			return "a result of type " + t + " requires at least one input of type anycompatiblerange or anycompatiblemultirange (42P13)"
		}
	case polyFamily1[t]:
		if !has(func(in string) bool { return polyFamily1[in] }) {
			return "a result of type " + t + " requires at least one input of type anyelement, anyarray, anynonarray, anyenum, anyrange, or anymultirange (42P13)"
		}
	case polyFamily2[t]:
		if !has(func(in string) bool { return polyFamily2[in] }) {
			return "a result of type " + t + " requires at least one polymorphic input of the anycompatible family (42P13)"
		}
	case t == "internal":
		if !has(func(in string) bool { return in == "internal" }) {
			return "unsafe use of pseudo-type internal: a result of type internal requires at least one input of type internal (42P13)"
		}
	}
	return ""
}

// y19RoutineTypes applies Y17 and Y18 to CREATE FUNCTION / PROCEDURE:
// a polymorphic result or OUT parameter no input determines, and a type
// modifier on a routine type (parse_type.c typenameTypeMod 363, as
// M2.P5 for columns).
func (s *state) y19RoutineTypes(toks []token) {
	h := readRoutineHeader(toks, s.m.source)
	if h == nil || h.unread != "" {
		return
	}
	what := "CREATE " + strings.ToUpper(h.kind)
	norm := func(t string) string {
		n, _ := catalogArgType(t)
		return strings.TrimPrefix(n, "pg_catalog.")
	}
	var inputs []string
	var written []string
	for _, p := range h.params {
		if p.mode != "out" {
			inputs = append(inputs, norm(p.typ))
		}
		written = append(written, p.typ)
	}
	for _, p := range h.table {
		written = append(written, p.typ)
	}
	if h.ret != "" {
		written = append(written, h.ret)
	}
	for _, w := range written {
		if !strings.Contains(w, "(") {
			continue
		}
		switch _, v, why := typmodCheck(w); v {
		case typmodRefused:
			s.unknownFact(fmt.Sprintf("%s: type %s: %s (Postgres refuses)%s", what, w, why, precondition("R6.P5")))
			return
		case typmodUnknown:
			s.unknownFact(fmt.Sprintf("%s may be refused: type %s: %s%s", what, w, why, precondition("R6.P5")))
			return
		}
	}
	results := []string{}
	if h.ret != "" {
		results = append(results, norm(strings.TrimPrefix(strings.TrimSpace(h.ret), "setof ")))
	}
	for _, p := range h.params {
		if p.mode == "out" || p.mode == "inout" {
			results = append(results, norm(p.typ))
		}
	}
	for _, p := range h.table {
		results = append(results, norm(p.typ))
	}
	for _, r := range results {
		if why := polymorphicResult(r, inputs); why != "" {
			s.unknownFact(fmt.Sprintf("%s: cannot determine result data type: %s (Postgres refuses)%s", what, why, precondition("R6.P5")))
			return
		}
	}
}

// y19Collation applies Y35 to CREATE COLLATION [IF NOT EXISTS] n (...)
// (collationcmds.c DefineCollation 80-312, define.c defGetBoolean 153):
// an unknown or repeated attribute, LOCALE with LC_COLLATE or LC_CTYPE,
// FROM with another option (42601); an unknown provider, a deterministic
// value that is no boolean; a missing locale (builtin, icu) or
// lc_collate / lc_ctype (libc); a nondeterministic collation or rules
// with a provider other than icu. Whether a locale exists stays U7's.
func (s *state) y19Collation(toks []token) {
	c := &cursor{toks: toks, i: 2, src: s.m.source}
	c.acceptKw("if", "not", "exists")
	q, err := c.name()
	if err != nil || !c.isPunct("(") {
		return
	}
	end, err := c.group()
	if err != nil {
		return
	}
	what := "CREATE COLLATION " + q.String()
	refuse := func(why, sqlstate string) {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("N1.P4")))
	}
	vals := map[string][]token{}
	for _, r := range c.splitList(c.i+1, end-1) {
		if r[0] >= r[1] {
			continue
		}
		name := toks[r[0]].value
		if toks[r[0]].kind != tokIdent && toks[r[0]].kind != tokQuoted {
			return
		}
		if !collationAttributes[name] {
			refuse(fmt.Sprintf("collation attribute %q not recognized", name), "42601")
			return
		}
		if _, ok := vals[name]; ok {
			refuse(fmt.Sprintf("conflicting or redundant options: %s", name), "42601")
			return
		}
		v := toks[r[0]+1 : r[1]]
		if len(v) > 0 && v[0].kind == tokOp && v[0].value == "=" {
			v = v[1:]
		}
		vals[name] = v
	}
	_, locale := vals["locale"]
	_, lcCollate := vals["lc_collate"]
	_, lcCtype := vals["lc_ctype"]
	if locale && (lcCollate || lcCtype) {
		refuse("LOCALE cannot be specified together with LC_COLLATE or LC_CTYPE", "42601")
		return
	}
	if _, from := vals["from"]; from {
		if len(vals) != 1 {
			refuse("FROM cannot be specified together with any other options", "42601")
		}
		return
	}
	provider := "libc"
	if v, ok := vals["provider"]; ok {
		if len(v) != 1 {
			return
		}
		text, ok := tokenText(v[0], s.m.source)
		if !ok {
			return
		}
		provider = strings.ToLower(text)
		if provider != "libc" && provider != "icu" && provider != "builtin" {
			refuse(fmt.Sprintf("unrecognized collation provider: %s", text), "42P17")
			return
		}
	}
	deterministic := true
	if v, ok := vals["deterministic"]; ok {
		if len(v) > 1 || len(v) == 1 && !booleanWord(v[0]) {
			refuse("deterministic requires a Boolean value", "22023")
			return
		}
		if len(v) == 1 {
			w := strings.ToLower(v[0].value)
			deterministic = !(w == "false" || w == "off" || w == "no" || w == "f" || w == "n" || w == "0")
		}
	}
	_, rules := vals["rules"]
	switch {
	case provider == "libc" && !locale && !lcCollate:
		refuse("parameter lc_collate must be specified", "42P17")
	case provider == "libc" && !locale && !lcCtype:
		refuse("parameter lc_ctype must be specified", "42P17")
	case provider != "libc" && !locale:
		refuse("parameter locale must be specified", "42P17")
	case !deterministic && provider != "icu":
		refuse("nondeterministic collations not supported with this provider", "0A000")
	case rules && provider != "icu":
		refuse("ICU rules cannot be specified unless locale provider is ICU", "42P17")
	}
}

// typeKey normalises a written type for comparisons: aliases resolved,
// pg_catalog's qualification dropped, _t written as t[].
func typeKey(written string) string {
	c, ok := canonicalType(written)
	if !ok {
		return strings.ToLower(strings.TrimSpace(written))
	}
	c = strings.TrimPrefix(c, "pg_catalog.")
	if strings.HasPrefix(c, "_") && isCatalogType(c[1:]) {
		c = c[1:] + "[]"
	}
	return c
}

// y19AlterColumnType applies Y01 and Y02 to ALTER TABLE ... ALTER COLUMN
// c TYPE t: an identity column takes only smallint, integer or bigint
// (sequence.c 1383: 22023; a domain over them too, the check compares
// the type), and a table whose row type a column of a table or
// materialized view uses cannot change a column's type
// (tablecmds.c 12975, find_composite_type_dependencies 6738: 0A000).
func (s *state) y19AlterColumnType(at *alterTable) {
	t, others := s.lookup(at.name)
	if t == nil || len(others) > 0 {
		return
	}
	for _, act := range at.actions {
		if act.kind != "alter_type" {
			continue
		}
		what := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", t.q, act.name, act.typ)
		if c := t.column(act.name); c != nil && c.identity != nil {
			if k := typeKey(act.typ); k != "int2" && k != "int4" && k != "int8" {
				s.unknownFact(fmt.Sprintf("%s: identity column type must be smallint, integer, or bigint (Postgres refuses, 22023)%s", what, precondition("M5c.P5")))
				return
			}
		}
		if s.rowTypeInUse(t, what) {
			return
		}
	}
}

// rowTypeInUse applies Y02 to table t: it reports whether it recorded a
// failure because a column the pass holds, or may not hold, uses t's row
// type under any name t has had.
func (s *state) rowTypeInUse(t *table, what string) bool {
	targets := s.rev18.aliases(t.q.name)
	reaches := func(words []string) bool {
		seen := map[string]bool{}
		todo := slicesClone(words)
		for len(todo) > 0 {
			w := todo[0]
			todo = todo[1:]
			if seen[w] {
				continue
			}
			seen[w] = true
			if targets[w] || targets["_"+w] || strings.HasPrefix(w, "_") && targets[w[1:]] {
				return true
			}
			todo = append(todo, s.rev19.typeWords[w]...)
		}
		return false
	}
	for _, u := range s.tables {
		if u == t {
			continue
		}
		for _, c := range u.columns {
			words := c.typeNames
			if len(words) == 0 {
				words = textWordList(c.typ)
			}
			if reaches(words) {
				s.unknownFact(fmt.Sprintf("%s: cannot alter table %s because column %s.%s uses its row type (Postgres refuses, 0A000)%s", what, t.q, u.q.name, c.name, precondition("M5c.P8")))
				return true
			}
		}
	}
	for _, e := range s.rev18.entries {
		if !s.live(e) || !unknownColumns(e.text) {
			continue
		}
		var words []string
		for w := range e.words {
			words = append(words, w)
		}
		if reaches(words) {
			s.unknownFact(fmt.Sprintf("%s may be refused: %q creates columns whose types the pass does not hold, and it names %s, whose row type a column may use%s", what, shortText(e.text), t.q, precondition("M5c.P8")))
			return true
		}
	}
	return false
}

// unknownColumns reports a statement that creates relation columns whose
// types the pass does not hold (Y02's list): CREATE TABLE AS, SELECT
// INTO, CREATE MATERIALIZED VIEW, and CREATE TABLE with LIKE, OF or
// INHERITS.
func unknownColumns(text string) bool {
	toks, ok := exprTokens(text)
	if !ok || len(toks) == 0 {
		return true
	}
	c := &cursor{toks: toks}
	switch {
	case c.peekKw("select"):
		return depth0(toks, 0, "into") >= 0
	case c.acceptKw("create"):
		for c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("unlogged") || c.acceptKw("local") || c.acceptKw("global") || c.acceptKw("or", "replace") {
		}
		if c.peekKw("materialized") {
			return true
		}
		if !c.peekKw("table") {
			return false
		}
		// PARTITION OF copies the parent's columns, which cannot use the
		// parent's row type; OF t copies t's attributes.
		of := depth0(toks, 0, "of")
		typedOf := of > 0 && !(toks[of-1].kind == tokIdent && toks[of-1].value == "partition")
		return depth0(toks, 0, "as") >= 0 || typedOf || depth0(toks, 0, "inherits") >= 0 || containsKw(c, "like")
	}
	return false
}

// textWordList returns the identifiers of a written text.
func textWordList(text string) []string {
	m := map[string]bool{}
	textWords(text, m)
	out := make([]string, 0, len(m))
	for w := range m {
		out = append(out, w)
	}
	return out
}

func slicesClone(in []string) []string {
	return append([]string(nil), in...)
}

// y19Types applies Y19 and Y20 to CREATE TYPE / DOMAIN and ALTER TYPE
// ... ADD VALUE / RENAME VALUE, and records the words of each type's
// definition for Y02.
func (s *state) y19Types(toks []token, a analysis) {
	c := &cursor{toks: toks, src: s.m.source}
	switch {
	case c.acceptKw("create", "type"), c.acceptKw("create", "domain"):
		q, err := c.name()
		if err != nil {
			return
		}
		if s.rev19.typeWords == nil {
			s.rev19.typeWords = map[string][]string{}
		}
		var words []string
		for j := c.i; j < len(toks); j++ {
			if toks[j].kind == tokIdent || toks[j].kind == tokQuoted {
				words = append(words, toks[j].value)
			}
		}
		s.rev19.typeWords[q.name] = words
		what := fmt.Sprintf("CREATE %s %s", strings.ToUpper(toks[1].value), q)
		switch {
		case toks[1].value == "type" && c.peekKw("as", "enum"):
			if len(a.relations) != 1 {
				return
			}
			o := a.relations[0]
			strs, long := 0, false
			for j := c.i; j < len(toks); j++ {
				if toks[j].kind == tokString {
					strs++
					// A decoded label is never longer than its literal.
					long = long || toks[j].end-toks[j].start > 63
				}
			}
			if o.labelsUndecoded && (strs > 1 || long) {
				s.unknownFact(fmt.Sprintf("%s may be refused: a label is written in a form the pass does not decode, so repeated or long labels are not ruled out%s", what, precondition("R2.P5")))
				return
			}
			seen := map[string]bool{}
			for _, l := range o.labels {
				if len(l) > 63 {
					s.unknownFact(fmt.Sprintf("%s: invalid enum label %q: labels must be 63 bytes or less (Postgres refuses, 42602)%s", what, l, precondition("R2.P5")))
					return
				}
				if seen[l] {
					s.unknownFact(fmt.Sprintf("%s: the label %q is written twice: duplicate key value violates unique constraint pg_enum_typid_label_index (Postgres refuses, 23505)%s", what, l, precondition("R2.P5")))
					return
				}
				seen[l] = true
			}
		case toks[1].value == "type" && c.peekKw("as") && c.i+1 < len(toks) && toks[c.i+1].kind == tokPunct && toks[c.i+1].value == "(":
			c.i++
			end, err := c.group()
			if err != nil {
				return
			}
			seen := map[string]bool{}
			for _, r := range c.splitList(c.i+1, end-1) {
				if r[0] >= r[1] || toks[r[0]].kind != tokIdent && toks[r[0]].kind != tokQuoted {
					continue
				}
				n := toks[r[0]].value
				if seen[n] {
					s.unknownFact(fmt.Sprintf("%s: column %q specified more than once (Postgres refuses, 42701)%s", what, n, precondition("R1.P5")))
					return
				}
				seen[n] = true
			}
		case toks[1].value == "domain":
			seen := map[string]bool{}
			depth := 0
			for j := c.i; j+1 < len(toks); j++ {
				switch {
				case toks[j].kind == tokPunct && (toks[j].value == "(" || toks[j].value == "["):
					depth++
				case toks[j].kind == tokPunct && (toks[j].value == ")" || toks[j].value == "]"):
					depth--
				case depth == 0 && toks[j].kind == tokIdent && toks[j].value == "constraint" && (toks[j+1].kind == tokIdent || toks[j+1].kind == tokQuoted):
					n := toks[j+1].value
					if seen[n] {
						s.unknownFact(fmt.Sprintf("%s: constraint %q for domain %s already exists (Postgres refuses, 42710)%s", what, n, q.name, precondition("R1.P5")))
						return
					}
					seen[n] = true
				}
			}
		}
	case c.acceptKw("alter", "type"):
		if a.enum == nil || a.enum.undecoded {
			return
		}
		l := a.enum.label
		if a.enum.rename {
			l = a.enum.newLabel
		}
		if len(l) > 63 {
			s.unknownFact(fmt.Sprintf("ALTER TYPE %s: invalid enum label %q: labels must be 63 bytes or less (Postgres refuses, 42602)%s", a.enum.q, l, precondition("R3.K2")))
		}
	}
}

// amCapabilities are an index access method's IndexAmRoutine flags (Y25).
type amCapabilities struct {
	unique, multicol, order, include, gettuple bool
}

// y19IndexMethod applies Y25 to CREATE INDEX (indexcmds.c DefineIndex
// 660, 868, 873; ComputeIndexAttrs 2161, 2166): UNIQUE needs amcanunique,
// more than one key column amcanmulticol, INCLUDE amcaninclude, ASC, DESC
// and NULLS FIRST / LAST amcanorder (0A000); more than INDEX_MAX_KEYS key
// and INCLUDE columns are refused (54011). An access method outside the
// core six (contrib, E1) may fail.
func (s *state) y19IndexMethod(ci *createIndex) {
	am := "btree"
	if ci.using != nil {
		am = strings.ToLower(*ci.using)
	}
	what := fmt.Sprintf("CREATE INDEX on %s USING %s", ci.table, am)
	caps, ok := amCaps[am]
	if !ok {
		s.unknownFact(fmt.Sprintf("%s may be refused: the capabilities of access method %s are not proven%s", what, am, precondition("M4.P4")))
		return
	}
	ordered := false
	for _, e := range ci.elements {
		if elementOrdered(e.text) {
			ordered = true
		}
	}
	refuse := func(why, sqlstate string) {
		s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("M4.P4")))
	}
	switch {
	case len(ci.elements)+len(ci.include) > indexMaxKeys:
		refuse(fmt.Sprintf("cannot use more than %d columns in an index", indexMaxKeys), "54011")
	case ci.unique && !caps.unique:
		refuse(fmt.Sprintf("access method %s does not support unique indexes", am), "0A000")
	case len(ci.include) > 0 && !caps.include:
		refuse(fmt.Sprintf("access method %s does not support included columns", am), "0A000")
	case len(ci.elements) > 1 && !caps.multicol:
		refuse(fmt.Sprintf("access method %s does not support multicolumn indexes", am), "0A000")
	case ordered && !caps.order:
		refuse(fmt.Sprintf("access method %s does not support ASC/DESC or NULLS FIRST/LAST options", am), "0A000")
	}
}

// elementOrdered reports an index element written with ASC, DESC or
// NULLS FIRST / LAST (gram.y index_elem_options).
func elementOrdered(text string) bool {
	toks, ok := exprTokens(text)
	if !ok {
		return false
	}
	depth := 0
	for i, t := range toks {
		switch {
		case t.kind == tokPunct && (t.value == "(" || t.value == "["):
			depth++
		case t.kind == tokPunct && (t.value == ")" || t.value == "]"):
			depth--
		case depth == 0 && i > 0 && t.kind == tokIdent && (t.value == "asc" || t.value == "desc" || t.value == "nulls"):
			return true
		}
	}
	return false
}

// casAccepts are the constraint attribute bits each table constraint
// type takes (gram.y TableConstraint 4138-4245 with processCASbits
// 19280): deferrable (DEFERRABLE, INITIALLY DEFERRED), NOT VALID, NO
// INHERIT.
var casAccepts = map[string][3]bool{
	"CHECK":       {false, true, true},
	"UNIQUE":      {true, false, false},
	"PRIMARY KEY": {true, false, false},
	"EXCLUDE":     {true, false, false},
	"FOREIGN KEY": {true, true, false},
}

// y19ConstraintAttributes applies Y26 to CREATE TABLE and ALTER TABLE:
// a table constraint's attributes (ConstraintAttributeSpec 6098: NOT
// DEFERRABLE with INITIALLY DEFERRED, and two conflicting words, 42601;
// processCASbits: a bit the constraint type does not take, 0A000) and a
// column constraint's (parse_utilcmd.c transformConstraintAttrs
// 3660-3730: DEFERRABLE, NOT DEFERRABLE and INITIALLY words only after a
// PRIMARY KEY, UNIQUE, EXCLUDE or REFERENCES constraint, once each, and
// INITIALLY DEFERRED only when deferrable; 42601).
func (s *state) y19ConstraintAttributes(toks []token) {
	c := &cursor{toks: toks, src: s.m.source}
	var elements [][2]int
	columnLevel := map[int]bool{}
	id := "M5f.P7"
	switch {
	case c.acceptKw("alter", "table"):
		c.acceptKw("if", "exists")
		c.acceptKw("only")
		if _, err := c.name(); err != nil {
			return
		}
		for _, r := range c.splitList(c.i, len(toks)) {
			e := &cursor{toks: toks[:r[1]], i: r[0], src: s.m.source}
			if !e.acceptKw("add") {
				continue
			}
			if e.peekKw("constraint") || e.peekKw("check") || e.peekKw("unique") || e.peekKw("primary") || e.peekKw("exclude") || e.peekKw("foreign") {
				elements = append(elements, [2]int{e.i, r[1]})
				continue
			}
			e.acceptKw("column")
			e.acceptKw("if", "not", "exists")
			columnLevel[len(elements)] = true
			elements = append(elements, [2]int{e.i, r[1]})
		}
	case c.acceptKw("create"):
		id = "M1.P7"
		for c.acceptKw("temp") || c.acceptKw("temporary") || c.acceptKw("unlogged") || c.acceptKw("local") || c.acceptKw("global") {
		}
		if !c.acceptKw("table") {
			return
		}
		c.acceptKw("if", "not", "exists")
		if _, err := c.name(); err != nil {
			return
		}
		if c.acceptKw("partition", "of") || c.acceptKw("of") {
			if _, err := c.name(); err != nil {
				return
			}
		}
		if !c.isPunct("(") {
			return
		}
		end, err := c.group()
		if err != nil {
			return
		}
		for _, r := range c.splitList(c.i+1, end-1) {
			if r[0] >= r[1] {
				continue
			}
			e := &cursor{toks: toks[:r[1]], i: r[0], src: s.m.source}
			if e.peekKw("like") {
				continue
			}
			if !(e.peekKw("constraint") || e.peekKw("check") || e.peekKw("unique") || e.peekKw("primary") || e.peekKw("exclude") || e.peekKw("foreign")) {
				columnLevel[len(elements)] = true
			}
			elements = append(elements, r)
		}
	default:
		return
	}
	for k, r := range elements {
		if why, sqlstate := constraintAttrs(toks[r[0]:r[1]], columnLevel[k]); why != "" {
			s.unknownFact(fmt.Sprintf("%s (Postgres refuses, %s)%s", why, sqlstate, precondition(id)))
			return
		}
	}
}

// constraintAttrs checks the attribute words of one table element (a
// column definition when column is set, else a table constraint); it
// returns why Postgres refuses it, or "".
func constraintAttrs(toks []token, column bool) (string, string) {
	kind := ""
	bits := map[string]bool{}
	// Column level: the attributes of the last constraint.
	deferrable, initially := "", ""
	attrs := false
	word := func(j int, w string) bool {
		return j < len(toks) && toks[j].kind == tokIdent && toks[j].value == w
	}
	depth := 0
	for j := 0; j < len(toks); j++ {
		t := toks[j]
		if t.kind == tokPunct {
			switch t.value {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			}
			continue
		}
		if depth > 0 || t.kind != tokIdent || column && j == 0 {
			continue
		}
		attr := ""
		switch {
		case word(j, "not") && word(j+1, "deferrable"):
			attr = "not deferrable"
		case word(j, "deferrable"):
			attr = "deferrable"
		case word(j, "initially") && word(j+1, "deferred"):
			attr = "initially deferred"
		case word(j, "initially") && word(j+1, "immediate"):
			attr = "initially immediate"
		case word(j, "not") && word(j+1, "valid"):
			attr = "not valid"
		case word(j, "no") && word(j+1, "inherit"):
			attr = "no inherit"
		case word(j, "check"):
			kind = "CHECK"
		case word(j, "unique"):
			kind = "UNIQUE"
		case word(j, "primary") && word(j+1, "key"):
			kind = "PRIMARY KEY"
		case word(j, "exclude"):
			kind = "EXCLUDE"
		case word(j, "foreign") && word(j+1, "key"), word(j, "references") && !column:
			if kind != "FOREIGN KEY" {
				kind = "FOREIGN KEY"
			}
		case word(j, "references"):
			kind = "FOREIGN KEY"
		case word(j, "not") && word(j+1, "null"), word(j, "null"), word(j, "default"), word(j, "generated"), word(j, "collate"), word(j, "compression"), word(j, "storage"):
			kind = "OTHER"
		}
		if kind != "" && attr == "" && column && (word(j, "check") || word(j, "unique") || word(j, "primary") || word(j, "references") || word(j, "not") && word(j+1, "null") || word(j, "null") || word(j, "default") || word(j, "generated")) {
			// A new column constraint: its attributes start afresh.
			deferrable, initially, attrs = "", "", false
		}
		if attr == "" {
			continue
		}
		if strings.Contains(attr, " ") {
			j++
		}
		if !column {
			bits[attr] = true
			continue
		}
		switch attr {
		case "not valid":
			return "a column constraint cannot be marked NOT VALID: syntax error", "42601"
		case "no inherit":
			if kind != "CHECK" {
				return "NO INHERIT is only written after a CHECK column constraint: syntax error", "42601"
			}
			continue
		}
		if kind != "UNIQUE" && kind != "PRIMARY KEY" && kind != "FOREIGN KEY" && kind != "EXCLUDE" {
			return fmt.Sprintf("misplaced %s clause", strings.ToUpper(attr)), "42601"
		}
		attrs = true
		switch attr {
		case "deferrable", "not deferrable":
			if deferrable != "" {
				return "multiple DEFERRABLE/NOT DEFERRABLE clauses not allowed", "42601"
			}
			deferrable = attr
			if attr == "not deferrable" && initially == "initially deferred" {
				return "constraint declared INITIALLY DEFERRED must be DEFERRABLE", "42601"
			}
		default:
			if initially != "" {
				return "multiple INITIALLY IMMEDIATE/DEFERRED clauses not allowed", "42601"
			}
			initially = attr
			if attr == "initially deferred" && deferrable == "not deferrable" {
				return "constraint declared INITIALLY DEFERRED must be DEFERRABLE", "42601"
			}
		}
	}
	_ = attrs
	if column || kind == "" || kind == "OTHER" {
		return "", ""
	}
	switch {
	case bits["not deferrable"] && bits["initially deferred"]:
		return "constraint declared INITIALLY DEFERRED must be DEFERRABLE", "42601"
	case bits["not deferrable"] && bits["deferrable"], bits["initially deferred"] && bits["initially immediate"]:
		return "conflicting constraint properties", "42601"
	}
	ok := casAccepts[kind]
	switch {
	case (bits["deferrable"] || bits["initially deferred"]) && !ok[0]:
		return fmt.Sprintf("%s constraints cannot be marked DEFERRABLE", kind), "0A000"
	case bits["not valid"] && !ok[1]:
		return fmt.Sprintf("%s constraints cannot be marked NOT VALID", kind), "0A000"
	case bits["no inherit"] && !ok[2]:
		return fmt.Sprintf("%s constraints cannot be marked NO INHERIT", kind), "0A000"
	}
	return "", ""
}

// y19Multirange applies Y21 (pg_type.c makeMultirangeTypeName 950-967;
// typecmds.c DefineRange 1449, 1592; heap.c 1178): CREATE TYPE r AS
// RANGE also creates the multirange type named by the
// multirange_type_name option, or by r with its first "range" replaced
// by "multirange", or r_multirange (r clipped to 52 bytes), clipped to
// 63 bytes. A later statement that creates a pg_type entry of that name
// is refused (42710), and so is the range when the name is held.
func (s *state) y19Multirange(toks []token, a analysis) {
	c := &cursor{toks: toks, src: s.m.source}
	if c.acceptKw("create", "type") {
		q, err := c.name()
		if err == nil && c.acceptKw("as", "range") {
			mr := multirangeName(toks, c.i, q.name, s.m.source)
			if mr == "" {
				s.unknownFact(fmt.Sprintf("CREATE TYPE %s AS RANGE may be refused: the name of its multirange type is not read%s", q, precondition("R1.P2")))
				return
			}
			mq := qname{schema: q.schema, name: mr}
			if s.multirangeHeld(mq) || s.typeNameHeld(mq) {
				s.unknownFact(fmt.Sprintf("CREATE TYPE %s AS RANGE: type %s already exists (Postgres refuses, 42710)%s", q, mr, precondition("R1.P2")))
				return
			}
			if s.rev19.multiranges == nil {
				s.rev19.multiranges = map[string]string{}
			}
			s.rev19.multiranges[mq.String()] = q.String()
			return
		}
	}
	if len(s.rev19.multiranges) == 0 {
		return
	}
	for _, e := range namesOf(a, toks).creates {
		// heap_create_with_catalog checks pg_type for every relation it
		// creates, a sequence included (heap.c 1165); an index is not
		// created there.
		if e.kind&(kindType|kindRelation) == 0 {
			continue
		}
		if s.multirangeHeld(qname{name: e.name}) {
			s.unknownFact(fmt.Sprintf("the statement creates %s, the name of a multirange type the set holds: type %s already exists (Postgres refuses, 42710)%s", e.name, e.name, precondition("M1.P2")))
			return
		}
	}
}

// multirangeName returns the multirange type name of CREATE TYPE r AS
// RANGE (...) whose option list starts at i, or "" when it is not read.
func multirangeName(toks []token, i int, r string, src []byte) string {
	c := &cursor{toks: toks, i: i, src: src}
	if !c.isPunct("(") {
		return ""
	}
	end, err := c.group()
	if err != nil {
		return ""
	}
	for _, rg := range c.splitList(i+1, end-1) {
		if rg[0] < rg[1] && toks[rg[0]].kind == tokIdent && toks[rg[0]].value == "multirange_type_name" {
			v := &cursor{toks: toks[:rg[1]], i: rg[0] + 1, src: src}
			if !v.done() && v.tok().kind == tokOp && v.tok().value == "=" {
				v.i++
			}
			q, err := v.name()
			if err != nil || !v.done() {
				return ""
			}
			return q.name
		}
	}
	var name string
	if k := strings.Index(r, "range"); k >= 0 {
		name = r[:k] + "multi" + r[k:]
	} else {
		name = clipBytes(r, 52) + "_multirange"
	}
	return clipBytes(name, 63)
}

// clipBytes clips s to at most n bytes at a UTF-8 character boundary
// (pg_mbcliplen).
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
}

// multirangeHeld reports whether the pass recorded a multirange type of
// q's name in q's schema (or may have: another qualification).
func (s *state) multirangeHeld(q qname) bool {
	for k := range s.rev19.multiranges {
		schema, name, qualified := strings.Cut(k, ".")
		if !qualified {
			schema, name = "", k
		}
		if name != q.name {
			continue
		}
		if q.schema == nil || schema == "" || schema == *q.schema {
			return true
		}
	}
	return false
}

// typeNameHeld reports whether the pass holds a pg_type entry named q: a
// table, view, materialized view, composite type, foreign table, type or
// domain (a sequence and an index have no row type).
func (s *state) typeNameHeld(q qname) bool {
	kind, candidates, _ := s.y19Kind(q)
	for _, k := range append(candidates, kind) {
		if k != "" && k != "sequence" && k != "index" && k != "generated index or sequence" {
			return true
		}
	}
	return false
}

// y19Exclude applies Y25 to EXCLUDE [USING am] (element WITH op, ...)
// [INCLUDE (...)] in CREATE TABLE and ALTER TABLE: the access method
// must have amgettuple (indexcmds.c DefineIndex: "access method does
// not support exclusion constraints", 0A000) and the capabilities a
// CREATE INDEX of those elements needs.
func (s *state) y19Exclude(toks []token) {
	for j := 0; j < len(toks); j++ {
		if toks[j].kind != tokIdent || toks[j].value != "exclude" {
			continue
		}
		c := &cursor{toks: toks, i: j + 1, src: s.m.source}
		am := "btree"
		if c.acceptKw("using") {
			if c.done() {
				return
			}
			am = strings.ToLower(c.tok().value)
			c.i++
		}
		if !c.isPunct("(") {
			continue
		}
		end, err := c.group()
		if err != nil {
			return
		}
		what := "EXCLUDE USING " + am
		caps, ok := amCaps[am]
		if !ok {
			s.unknownFact(fmt.Sprintf("%s may be refused: the capabilities of access method %s are not proven%s", what, am, precondition("M4.P4")))
			return
		}
		elements := c.splitList(c.i+1, end-1)
		ordered := false
		for _, r := range elements {
			if w := depth0(toks[:r[1]], r[0], "with"); w > r[0] && elementOrdered(c.text(r[0], w)) {
				ordered = true
			}
		}
		include := 0
		c.i = end
		if c.acceptKw("include") && c.isPunct("(") {
			e, err := c.group()
			if err != nil {
				return
			}
			include = len(c.splitList(c.i+1, e-1))
		}
		refuse := func(why, sqlstate string) {
			s.unknownFact(fmt.Sprintf("%s: %s (Postgres refuses, %s)%s", what, why, sqlstate, precondition("M4.P4")))
		}
		switch {
		case !caps.gettuple:
			refuse(fmt.Sprintf("access method %s does not support exclusion constraints", am), "0A000")
		case len(elements)+include > indexMaxKeys:
			refuse(fmt.Sprintf("cannot use more than %d columns in an index", indexMaxKeys), "54011")
		case include > 0 && !caps.include:
			refuse(fmt.Sprintf("access method %s does not support included columns", am), "0A000")
		case len(elements) > 1 && !caps.multicol:
			refuse(fmt.Sprintf("access method %s does not support multicolumn indexes", am), "0A000")
		case ordered && !caps.order:
			refuse(fmt.Sprintf("access method %s does not support ASC/DESC or NULLS FIRST/LAST options", am), "0A000")
		default:
			continue
		}
		return
	}
}

// storedDefault applies Y46 to a written DEFAULT of a column of type typ:
// Postgres stores no default (atthasdef false) exactly when the cooked
// expression is a bare NULL constant (heap.c AddRelationNewConstraints
// 2370-2386), which happens when the column's type is not a domain and
// has no modifier and the default is NULL, (NULL), or NULL cast (:: or
// CAST) any number of times to unknown or to the column's type, with or
// without COLLATE. It returns the default the column has.
func (s *state) storedDefault(def *string, typ string) *string {
	if def == nil || !nullConstant(*def, typeKey(typ)) {
		return def
	}
	k := typeKey(typ)
	if strings.Contains(k, "(") {
		return def
	}
	base := strings.TrimSuffix(k, "[]")
	if !isCatalogType(base) && !e1Types[base] && !s.heldNonDomainType(base) {
		return def
	}
	return nil
}

// heldNonDomainType reports a type the set holds as an enum, range,
// composite type or relation row type, and not also as a domain.
func (s *state) heldNonDomainType(name string) bool {
	found := false
	for k, r := range s.relations {
		if k != name && !strings.HasSuffix(k, "."+name) {
			continue
		}
		if r.kind == "domain" || r.uncertain {
			return false
		}
		found = true
	}
	return found
}

// nullConstant reports whether text is NULL, parenthesised, cast to
// unknown or to the type whose key is col, and collated, in any nesting.
func nullConstant(text, col string) bool {
	toks, ok := exprTokens(text)
	if !ok {
		return false
	}
	end, ok := nullExpr(toks, 0, col)
	return ok && end == len(toks)
}

func nullExpr(toks []token, i int, col string) (int, bool) {
	isKw := func(j int, w string) bool { return j < len(toks) && toks[j].kind == tokIdent && toks[j].value == w }
	isP := func(j int, v string) bool {
		return j < len(toks) && (toks[j].kind == tokPunct || toks[j].kind == tokOp) && toks[j].value == v
	}
	typeOK := func(from, to int) bool {
		if from >= to {
			return false
		}
		var b strings.Builder
		for j := from; j < to; j++ {
			if j > from {
				b.WriteByte(' ')
			}
			if toks[j].kind == tokQuoted {
				b.WriteString(`"` + toks[j].value + `"`)
			} else {
				b.WriteString(toks[j].value)
			}
		}
		k := typeKey(b.String())
		return k == "unknown" || k == col
	}
	// typeEnd is the end of a cast's type: the next depth-0 ::, COLLATE
	// or closing bracket.
	typeEnd := func(j int) int {
		depth := 0
		for ; j < len(toks); j++ {
			switch {
			case isP(j, "(") || isP(j, "["):
				depth++
			case isP(j, ")") || isP(j, "]"):
				if depth == 0 {
					return j
				}
				depth--
			case depth == 0 && (isP(j, "::") || isKw(j, "collate") || isKw(j, "as")):
				return j
			}
		}
		return j
	}
	var j int
	switch {
	case isKw(i, "null"):
		j = i + 1
	case isP(i, "("):
		e, ok := nullExpr(toks, i+1, col)
		if !ok || !isP(e, ")") {
			return 0, false
		}
		j = e + 1
	case isKw(i, "cast") && isP(i+1, "("):
		e, ok := nullExpr(toks, i+2, col)
		if !ok || !isKw(e, "as") {
			return 0, false
		}
		te := typeEnd(e + 1)
		if !typeOK(e+1, te) || !isP(te, ")") {
			return 0, false
		}
		j = te + 1
	default:
		return 0, false
	}
	for j < len(toks) {
		switch {
		case isP(j, "::"):
			te := typeEnd(j + 1)
			if !typeOK(j+1, te) {
				return 0, false
			}
			j = te
		case isKw(j, "collate"):
			c := &cursor{toks: toks, i: j + 1}
			if _, err := c.name(); err != nil {
				return 0, false
			}
			j = c.i
		default:
			return j, true
		}
	}
	return j, true
}

// searchPathList reports search_path {TO | =} followed by a list of names
// (identifiers, quoted identifiers or string constants without a comma
// or a double quote) up to the end or the next routine option: values
// check_search_path accepts (SplitIdentifierString), as O3 reads SET
// search_path (Y15).
func searchPathList(toks []token, i int) bool {
	if i >= len(toks) || toks[i].kind != tokIdent || toks[i].value != "search_path" {
		return false
	}
	i++
	switch {
	case i < len(toks) && toks[i].kind == tokIdent && toks[i].value == "to":
	case i < len(toks) && toks[i].kind == tokOp && toks[i].value == "=":
	default:
		return false
	}
	i++
	for n := 0; ; n++ {
		if i >= len(toks) {
			return false
		}
		t := toks[i]
		switch t.kind {
		case tokIdent, tokQuoted:
		case tokString:
			if strings.ContainsAny(t.value, ",\"") || t.value == "" {
				return false
			}
		default:
			return false
		}
		i++
		if i >= len(toks) || toks[i].kind == tokIdent {
			return true
		}
		if toks[i].kind != tokPunct || toks[i].value != "," {
			return false
		}
		i++
	}
}

// wordAfterList reports a word after the value list of SET at i (a
// statement that does not end there).
func wordAfterList(toks []token, i int) bool {
	for j := i + 2; j < len(toks); j++ {
		if toks[j].kind == tokIdent && j > i+2 && toks[j-1].kind != tokPunct {
			return true
		}
	}
	return false
}

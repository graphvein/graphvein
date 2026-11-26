package schema

import "strings"

// Whole-statement syntax (T018a2 Revision 1.6 V7, class P7). Several rows
// read only the names they need; Postgres parses the whole statement with
// gram.y REL_17_6 and refuses any token its production does not take
// (42601). wholeStatement holds every such row to a closed list of gram.y
// productions: a statement of a governed family that no listed production
// reads completely is row D (may fail). A family a reader does not govern
// returns "" (its own reader decides). The readers are also the
// embedded-statement readers of PL/pgSQL bodies (V1).

// wholeStatement returns why the statement is not read completely by one of
// its family's productions, or "" when it is (or no family governs it).
func wholeStatement(toks []token, src []byte) string {
	p := &wp{cursor: &cursor{toks: toks, src: src}}
	if p.done() || p.tok().kind != tokIdent {
		return ""
	}
	governed, ok := p.statement()
	if !governed {
		return ""
	}
	if !ok || !p.done() {
		return "the statement is not read completely by a gram.y production of its row (" + p.fail("V7").reason + ")"
	}
	return ""
}

// wp is the whole-statement reader: a cursor whose methods accept one
// grammar symbol each and report whether it was there.
type wp struct {
	*cursor
	// strict is set for the embedded statements of PL/pgSQL bodies (V1):
	// a group whose content another precondition proves at the top level
	// (a policy's USING, a trigger's WHEN) is not read.
	strict bool
}

func (p *wp) kw(words ...string) bool { return p.acceptKw(words...) }

func (p *wp) punct(v string) bool { return p.acceptPunct(v) }

// colID accepts a ColId (U8: no reserved or type_func_name keyword).
func (p *wp) colID() bool {
	_, err := p.ident()
	return err == nil
}

// colLabel accepts any identifier or keyword (gram.y ColLabel).
func (p *wp) colLabel() bool {
	t := p.tok()
	if p.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return false
	}
	p.i++
	return true
}

// nonReservedWord accepts IDENT or an unreserved, col_name or
// type_func_name keyword (gram.y NonReservedWord).
func (p *wp) nonReservedWord() bool {
	t := p.tok()
	if p.done() || t.kind != tokIdent && t.kind != tokQuoted {
		return false
	}
	if t.kind == tokIdent && pgKeywordCategories[t.value] == 'R' {
		return false
	}
	p.i++
	return true
}

// anyName accepts ColId [. ColLabel ...] (any_name, qualified_name).
func (p *wp) anyName() bool {
	_, err := p.name()
	return err == nil
}

// list accepts item [, item]...
func (p *wp) list(item func() bool) bool {
	if !item() {
		return false
	}
	for p.punct(",") {
		if !item() {
			return false
		}
	}
	return true
}

// sconst accepts a string constant gram.y reads as Sconst: '...', E'...',
// U&'...' or a dollar-quoted string (not B'...', X'...' or N'...').
func (p *wp) sconst() bool {
	t := p.tok()
	if p.done() || t.kind != tokString {
		return false
	}
	switch b := p.src[t.start]; b {
	case '\'', '$', 'e', 'E', 'u', 'U':
	default:
		return false
	}
	p.i++
	return true
}

// iconst accepts an unsigned integer constant.
func (p *wp) iconst() bool {
	t := p.tok()
	if p.done() || t.kind != tokNumber || strings.ContainsAny(string(p.src[t.start:t.end]), ".eE") {
		return false
	}
	p.i++
	return true
}

// sign accepts an optional + or -.
func (p *wp) sign() {
	if !p.done() && p.tok().kind == tokOp && (p.tok().value == "-" || p.tok().value == "+") {
		p.i++
	}
}

// signedIconst accepts [+|-] Iconst.
func (p *wp) signedIconst() bool {
	p.sign()
	return p.iconst()
}

// numericOnly accepts [+|-] a numeric constant.
func (p *wp) numericOnly() bool {
	p.sign()
	if p.done() || p.tok().kind != tokNumber {
		return false
	}
	p.i++
	return true
}

// nonReservedOrSconst accepts NonReservedWord_or_Sconst.
func (p *wp) nonReservedOrSconst() bool {
	return p.sconst() || p.nonReservedWord()
}

// roleSpec accepts gram.y RoleSpec: a NonReservedWord (PUBLIC only where
// public is true; NONE is refused, 42939), CURRENT_ROLE, CURRENT_USER or
// SESSION_USER.
func (p *wp) roleSpec(public bool) bool {
	t := p.tok()
	if p.done() {
		return false
	}
	if t.kind == tokIdent {
		switch t.value {
		case "current_role", "current_user", "session_user":
			p.i++
			return true
		case "none":
			return false
		case "public":
			if !public {
				return false
			}
		}
	}
	return p.nonReservedWord()
}

// roleList accepts role_list (RoleSpec, ...).
func (p *wp) roleList() bool { return p.list(func() bool { return p.roleSpec(false) }) }

// roleID accepts RoleId: a RoleSpec that names a role to create (no
// PUBLIC, NONE or CURRENT_* forms: 42939).
func (p *wp) roleID() bool {
	t := p.tok()
	if t.kind == tokIdent && (t.value == "current_role" || t.value == "current_user" || t.value == "session_user" || t.value == "public" || t.value == "none") {
		return false
	}
	return p.nonReservedWord()
}

// typename accepts a gram.y Typename.
func (p *wp) typename() bool { return columnType(p.cursor) == nil }

// funcArgs accepts func_args: ( [func_arg [, ...]] ), each argument read
// as routineParameter reads it, without a default.
func (p *wp) funcArgs() bool {
	if !p.isPunct("(") {
		return false
	}
	end, err := p.group()
	if err != nil {
		return false
	}
	if end == p.i+2 {
		p.i = end
		return true
	}
	for _, r := range p.splitList(p.i+1, end-1) {
		if r[0] >= r[1] {
			return false
		}
		if prm, ok := routineParameter(p.cursor, r[0], r[1]); !ok || prm.hasDefault || prm.typ == "" {
			return false
		}
	}
	p.i = end
	return true
}

// funcWithArgtypes accepts function_with_argtypes: func_name [func_args].
func (p *wp) funcWithArgtypes() bool {
	if _, err := p.funcName(); err != nil {
		return false
	}
	if p.isPunct("(") {
		return p.funcArgs()
	}
	return true
}

// aggrWithArgtypes accepts aggregate_with_argtypes: func_name ( * ) or
// func_name func_args.
func (p *wp) aggrWithArgtypes() bool {
	if _, err := p.funcName(); err != nil {
		return false
	}
	if p.isPunct("(") && p.i+2 < len(p.toks) && p.toks[p.i+1].kind == tokOp && p.toks[p.i+1].value == "*" && p.toks[p.i+2].kind == tokPunct && p.toks[p.i+2].value == ")" {
		p.i += 3
		return true
	}
	return p.funcArgs()
}

// parenGroup accepts a balanced ( ... ) whose content another
// precondition proves or makes may fail (a policy's USING, a trigger's
// WHEN: N1.P5).
func (p *wp) parenGroup() bool {
	if p.strict || !p.isPunct("(") {
		return false
	}
	end, err := p.group()
	if err != nil {
		return false
	}
	p.i = end
	return true
}

// parenExpr accepts ( a_expr ) with the syntax-only expression reader.
func (p *wp) parenExpr() bool {
	if !p.punct("(") {
		return false
	}
	if !exprSyntax(p.cursor) {
		return false
	}
	return p.punct(")")
}

// dropBehavior accepts [CASCADE | RESTRICT].
func (p *wp) dropBehavior() {
	_ = p.kw("cascade") || p.kw("restrict")
}

// columnList accepts ( ColId [, ...] ).
func (p *wp) columnList() bool {
	if !p.punct("(") {
		return false
	}
	if !p.list(p.colID) {
		return false
	}
	return p.punct(")")
}

// statement dispatches on the statement head: governed reports a family
// V7 governs, ok that its production read it (the caller checks the end).
func (p *wp) statement() (governed, ok bool) {
	switch {
	case p.kw("comment", "on"):
		return true, p.commentStmt()
	case p.peekKw("grant"), p.peekKw("revoke"):
		return true, p.grantStmt()
	case p.kw("alter", "default", "privileges"):
		return true, p.defaultPrivileges()
	case p.kw("create", "role"), p.kw("create", "user"), p.kw("create", "group"):
		if p.peekKw("mapping") {
			return true, false
		}
		return true, p.createRole()
	case p.kw("alter", "role"), p.kw("alter", "user"):
		return true, p.alterRole()
	case p.kw("alter", "group"):
		return true, p.alterGroup()
	case p.kw("alter", "database"):
		return true, p.alterDatabase()
	case p.peekKw("create", "trigger"), p.peekKw("create", "constraint", "trigger"), p.peekKw("create", "or", "replace", "trigger"), p.peekKw("create", "or", "replace", "constraint", "trigger"):
		return true, p.createTrigger()
	case p.kw("create", "policy"):
		return true, p.createPolicy()
	case p.kw("alter", "policy"):
		return true, p.alterPolicy()
	case p.kw("alter", "trigger"):
		return true, p.alterTrigger()
	case p.peekKw("create", "collation"), p.peekKw("create", "statistics"), p.peekKw("create", "publication"),
		p.peekKw("create", "operator"), p.peekKw("create", "cast"), p.peekKw("create", "aggregate"),
		p.peekKw("create", "or", "replace", "aggregate"):
		return true, p.createDefinition()
	case p.kw("lock"):
		return true, p.lockStmt()
	case p.kw("notify"):
		return true, p.colID() && (!p.punct(",") || p.sconst())
	case p.kw("listen"):
		return true, p.colID()
	case p.kw("unlisten"):
		if !p.done() && p.tok().kind == tokOp && p.tok().value == "*" {
			p.i++
			return true, true
		}
		return true, p.colID()
	case p.kw("checkpoint"):
		return true, true
	case p.kw("security", "label"):
		return true, p.securityLabel()
	case p.kw("reassign", "owned", "by"):
		return true, p.roleList() && p.kw("to") && p.roleSpec(false)
	case p.kw("reset"):
		return true, p.resetRest()
	case p.kw("discard"):
		return true, p.kw("all") || p.kw("plans") || p.kw("sequences") || p.kw("temp") || p.kw("temporary")
	case p.peekKw("begin"), p.peekKw("start"), p.peekKw("commit"), p.peekKw("end"), p.peekKw("rollback"), p.peekKw("abort"),
		p.peekKw("savepoint"), p.peekKw("release"), p.peekKw("prepare", "transaction"):
		return true, p.transactionStmt()
	case p.kw("create", "extension"):
		return true, p.createExtension()
	case p.kw("alter", "extension"):
		// Only UPDATE is listed (row O2); other ALTER EXTENSION forms: D.
		return true, p.colID() && p.kw("update") && (!p.kw("to") || p.nonReservedOrSconst())
	case p.kw("create", "schema"):
		return true, p.createSchema()
	case p.kw("create", "event", "trigger"):
		return true, p.createEventTrigger()
	case p.peekKw("create", "type") && p.typeIsEnum():
		p.i += 2
		return true, p.createEnum()
	case p.kw("create", "type"):
		return true, p.createType()
	case p.kw("alter", "type"):
		return true, p.alterType()
	case p.kw("alter", "domain"):
		return true, p.alterDomain()
	case p.kw("alter", "view"):
		return true, p.alterView(false)
	case p.kw("alter", "materialized", "view"):
		return true, p.alterView(true)
	case p.kw("alter", "collation"):
		return true, p.anyName() && (p.kw("rename", "to") && p.colID() || p.kw("owner", "to") && p.roleSpec(false) || p.kw("set", "schema") && p.colID() || p.kw("refresh", "version"))
	case p.peekKw("alter", "sequence"):
		return p.alterSequence()
	case p.kw("alter", "function"), p.kw("alter", "procedure"), p.kw("alter", "routine"):
		return true, p.alterFunction()
	case p.peekKw("alter", "schema"):
		p.i += 2
		return true, p.colID() && (p.kw("rename", "to") && p.colID() || p.kw("owner", "to") && p.roleSpec(false))
	case p.peekKw("drop"):
		return p.dropStmt()
	case p.kw("do"):
		return true, p.doStmt()
	case p.kw("call"):
		return true, p.funcApplication()
	case p.kw("execute"):
		return true, p.colID() && (!p.isPunct("(") || p.exprListParen())
	case p.kw("cluster"):
		return true, p.clusterStmt()
	case p.kw("reindex"):
		return true, p.reindexStmt()
	case p.kw("refresh", "materialized", "view"):
		p.kw("concurrently")
		return true, p.anyName() && (!p.kw("with") || (p.kw("no") || true) && p.kw("data"))
	case p.peekKw("copy"):
		return true, p.copyStmt()
	case p.peekKw("table"):
		p.i++
		p.kw("only")
		return true, p.anyName() && (p.done() || p.tok().kind == tokOp && p.tok().value == "*" && p.next())
	}
	return false, false
}

// typeIsEnum reports CREATE TYPE name AS ENUM.
func (p *wp) typeIsEnum() bool {
	q := &wp{cursor: &cursor{toks: p.toks, i: p.i + 2, src: p.src}}
	return q.anyName() && q.peekKw("as", "enum")
}

// commentStmt reads CommentStmt after COMMENT ON (gram.y 7049-7218).
func (p *wp) commentStmt() bool {
	if !p.commentObject() {
		return false
	}
	return p.kw("is") && (p.sconst() || p.kw("null"))
}

// commentObject reads the object of COMMENT ON and SECURITY LABEL ON.
func (p *wp) commentObject() bool {
	switch {
	case p.kw("table"), p.kw("sequence"), p.kw("view"), p.kw("materialized", "view"), p.kw("index"),
		p.kw("foreign", "table"), p.kw("collation"), p.kw("conversion"), p.kw("statistics"),
		p.kw("text", "search", "parser"), p.kw("text", "search", "dictionary"),
		p.kw("text", "search", "template"), p.kw("text", "search", "configuration"):
		return p.anyName()
	case p.kw("column"):
		start := p.i
		if !p.anyName() {
			return false
		}
		return p.i > start+1
	case p.kw("access", "method"), p.kw("event", "trigger"), p.kw("extension"), p.kw("foreign", "data", "wrapper"),
		p.kw("procedural", "language"), p.kw("language"), p.kw("publication"), p.kw("schema"), p.kw("server"),
		p.kw("database"), p.kw("role"), p.kw("subscription"), p.kw("tablespace"):
		return p.colID()
	case p.kw("type"), p.kw("domain"):
		return p.typename()
	case p.kw("aggregate"):
		return p.aggrWithArgtypes()
	case p.kw("function"), p.kw("procedure"), p.kw("routine"):
		return p.funcWithArgtypes()
	case p.kw("constraint"):
		if !p.colID() || !p.kw("on") {
			return false
		}
		p.kw("domain")
		return p.anyName()
	case p.kw("policy"), p.kw("rule"), p.kw("trigger"):
		return p.colID() && p.kw("on") && p.anyName()
	case p.kw("cast"):
		return p.castTypes()
	case p.peekKw("operator") && !p.isKw(1, "class") && !p.isKw(1, "family"):
		p.i++
		return p.operatorWithArgtypes()
	}
	return false
}

// operatorWithArgtypes reads operator_with_argtypes: any_operator
// ( {Typename | NONE} , {Typename | NONE} ) (not both NONE).
func (p *wp) operatorWithArgtypes() bool {
	if (p.tok().kind == tokIdent || p.tok().kind == tokQuoted) && p.isPunctAt(1, ".") {
		p.i += 2
	}
	if p.done() || p.tok().kind != tokOp {
		return false
	}
	p.i++
	if !p.punct("(") {
		return false
	}
	left := p.kw("none") || p.typename()
	if !left || !p.punct(",") {
		return false
	}
	return (p.kw("none") || p.typename()) && p.punct(")")
}

// castTypes reads ( Typename AS Typename ).
func (p *wp) castTypes() bool {
	return p.punct("(") && p.typename() && p.kw("as") && p.typename() && p.punct(")")
}

// privilege words by the kind of object a GRANT or REVOKE names (aclchk.c
// string_to_privilege and the per-kind ACL_ALL_RIGHTS_* sets): another word
// is refused (42601 unrecognized, 0LP01 invalid for the kind).
var privilegeWords = map[string]map[string]bool{
	"table":    {"select": true, "insert": true, "update": true, "delete": true, "truncate": true, "references": true, "trigger": true, "maintain": true},
	"sequence": {"usage": true, "select": true, "update": true},
	"function": {"execute": true},
	"schema":   {"usage": true, "create": true},
	"database": {"create": true, "connect": true, "temporary": true, "temp": true},
	"type":     {"usage": true},
}

// columnPrivileges are the privileges that take a column list.
var columnPrivileges = map[string]bool{"select": true, "insert": true, "update": true, "references": true}

// privileges reads `privileges` for a target kind: ALL [PRIVILEGES] [(cols)]
// or privilege [(cols)] [, ...]; cols only on tables.
func (p *wp) privileges(kind string) bool {
	if p.kw("all") {
		p.kw("privileges")
		if p.isPunct("(") {
			return kind == "table" && p.columnList()
		}
		return true
	}
	return p.list(func() bool {
		t := p.tok()
		if p.done() || t.kind != tokIdent || !privilegeWords[kind][t.value] {
			return false
		}
		p.i++
		if p.isPunct("(") {
			return kind == "table" && columnPrivileges[t.value] && p.columnList()
		}
		return true
	})
}

// privilegeTarget reads privilege_target and returns its kind.
func (p *wp) privilegeTarget() (string, bool) {
	names := func() bool { return p.list(p.anyName) }
	switch {
	case p.kw("table"):
		return "table", names()
	case p.kw("sequence"):
		return "sequence", names()
	case p.kw("function"), p.kw("procedure"), p.kw("routine"):
		return "function", p.list(p.funcWithArgtypes)
	case p.kw("database"):
		return "database", p.list(p.colID)
	case p.kw("domain"), p.kw("type"):
		return "type", names()
	case p.kw("schema"):
		return "schema", p.list(p.colID)
	case p.kw("all", "tables", "in", "schema"):
		return "table", p.list(p.colID)
	case p.kw("all", "sequences", "in", "schema"):
		return "sequence", p.list(p.colID)
	case p.kw("all", "functions", "in", "schema"), p.kw("all", "procedures", "in", "schema"), p.kw("all", "routines", "in", "schema"):
		return "function", p.list(p.colID)
	}
	if p.peekKw("foreign") || p.peekKw("language") || p.peekKw("large") || p.peekKw("parameter") || p.peekKw("tablespace") {
		return "", false
	}
	return "table", names()
}

// grantees reads grantee_list: [GROUP] RoleSpec (PUBLIC allowed).
func (p *wp) grantees() bool {
	return p.list(func() bool {
		p.kw("group")
		return p.roleSpec(true)
	})
}

// grantStmt reads GrantStmt, RevokeStmt, GrantRoleStmt and RevokeRoleStmt.
func (p *wp) grantStmt() bool {
	grant := p.kw("grant")
	if !grant {
		p.kw("revoke")
	}
	// The privilege form has ON before TO/FROM; the role form has none.
	on := depth0(p.toks, p.i, "on")
	if on < 0 {
		return p.roleGrant(grant)
	}
	if !grant {
		p.kw("grant", "option", "for")
	}
	// The target decides which privilege words are valid: read it first.
	q := &wp{cursor: &cursor{toks: p.toks, i: on + 1, src: p.src}}
	kind, ok := q.privilegeTarget()
	if !ok {
		return false
	}
	privEnd := &wp{cursor: &cursor{toks: p.toks[:on], i: p.i, src: p.src}}
	if !privEnd.privileges(kind) || !privEnd.done() {
		return false
	}
	p.i = q.i
	if grant {
		if !p.kw("to") || !p.grantees() {
			return false
		}
		p.kw("with", "grant", "option")
		if p.kw("granted", "by") && !p.roleSpec(false) {
			return false
		}
		return true
	}
	if !p.kw("from") || !p.grantees() {
		return false
	}
	if p.kw("granted", "by") && !p.roleSpec(false) {
		return false
	}
	p.dropBehavior()
	return true
}

// roleGrant reads GrantRoleStmt (after GRANT) or RevokeRoleStmt (after
// REVOKE).
func (p *wp) roleGrant(grant bool) bool {
	optionWord := func() bool {
		t := p.tok()
		if p.done() || t.kind != tokIdent || t.value != "admin" && t.value != "inherit" && t.value != "set" {
			return false
		}
		p.i++
		return true
	}
	if !grant && p.isKw(1, "option") && p.isKw(2, "for") {
		if !optionWord() {
			return false
		}
		p.i += 2
	}
	if !p.roleList() {
		return false
	}
	if grant {
		if !p.kw("to") || !p.roleList() {
			return false
		}
		if p.kw("with") && !p.list(func() bool { return optionWord() && (p.kw("option") || p.kw("true") || p.kw("false")) }) {
			return false
		}
		return !p.kw("granted", "by") || p.roleSpec(false)
	}
	if !p.kw("from") || !p.roleList() {
		return false
	}
	if p.kw("granted", "by") && !p.roleSpec(false) {
		return false
	}
	p.dropBehavior()
	return true
}

// defaultPrivileges reads AlterDefaultPrivilegesStmt after ALTER DEFAULT
// PRIVILEGES.
func (p *wp) defaultPrivileges() bool {
	for {
		switch {
		case p.kw("in", "schema"):
			if !p.list(p.colID) {
				return false
			}
			continue
		case p.kw("for", "role"), p.kw("for", "user"):
			if !p.roleList() {
				return false
			}
			continue
		}
		break
	}
	grant := p.kw("grant")
	if !grant {
		if !p.kw("revoke") {
			return false
		}
		p.kw("grant", "option", "for")
	}
	on := depth0(p.toks, p.i, "on")
	if on < 0 {
		return false
	}
	kinds := map[string]string{"tables": "table", "sequences": "sequence", "functions": "function", "routines": "function", "types": "type", "schemas": "schema"}
	if on+1 >= len(p.toks) || p.toks[on+1].kind != tokIdent || kinds[p.toks[on+1].value] == "" {
		return false
	}
	kind := kinds[p.toks[on+1].value]
	privEnd := &wp{cursor: &cursor{toks: p.toks[:on], i: p.i, src: p.src}}
	if !privEnd.privileges(kind) || !privEnd.done() {
		return false
	}
	p.i = on + 2
	if grant {
		if !p.kw("to") || !p.grantees() {
			return false
		}
		p.kw("with", "grant", "option")
		return true
	}
	if !p.kw("from") || !p.grantees() {
		return false
	}
	p.dropBehavior()
	return true
}

// roleOptionWords are the IDENT options of AlterOptRoleElem (gram.y
// 1207-1297); another IDENT is refused (42601 unrecognized role option).
var roleOptionWords = map[string]bool{
	"superuser": true, "nosuperuser": true, "createrole": true, "nocreaterole": true,
	"replication": true, "noreplication": true, "createdb": true, "nocreatedb": true,
	"login": true, "nologin": true, "bypassrls": true, "nobypassrls": true, "noinherit": true,
}

// roleOption reads one AlterOptRoleElem, or with create one
// CreateOptRoleElem.
func (p *wp) roleOption(create bool) bool {
	switch {
	case p.kw("password"):
		return p.sconst() || p.kw("null")
	case p.kw("encrypted", "password"):
		return p.sconst()
	case p.kw("inherit"):
		return true
	case p.kw("connection", "limit"):
		return p.signedIconst()
	case p.kw("valid", "until"):
		return p.sconst()
	case p.kw("user"):
		return p.roleList()
	case create && p.kw("sysid"):
		return p.iconst()
	case create && (p.kw("admin") || p.kw("role") || p.kw("in", "role") || p.kw("in", "group")):
		return p.roleList()
	}
	t := p.tok()
	if !p.done() && t.kind == tokIdent && roleOptionWords[t.value] && pgKeywordCategories[t.value] == 0 {
		p.i++
		return true
	}
	return false
}

// createRole reads CREATE {ROLE | USER | GROUP} RoleId [WITH] options.
func (p *wp) createRole() bool {
	if !p.roleID() {
		return false
	}
	p.kw("with")
	for !p.done() {
		if !p.roleOption(true) {
			return false
		}
	}
	return true
}

// alterRole reads AlterRoleStmt, AlterRoleSetStmt and ALTER ROLE ...
// RENAME TO after ALTER {ROLE | USER}.
func (p *wp) alterRole() bool {
	if !p.kw("all") && !p.roleSpec(false) {
		return false
	}
	if p.kw("rename", "to") {
		return p.roleID()
	}
	inDB := p.kw("in", "database")
	if inDB && !p.colID() {
		return false
	}
	if p.peekKw("set") || p.peekKw("reset") {
		return p.setResetClause()
	}
	if inDB {
		return false
	}
	p.kw("with")
	for !p.done() {
		if !p.roleOption(false) {
			return false
		}
	}
	return true
}

// alterGroup reads ALTER GROUP RoleSpec {ADD | DROP} USER role_list and
// ALTER GROUP RoleId RENAME TO RoleId.
func (p *wp) alterGroup() bool {
	if !p.roleSpec(false) {
		return false
	}
	if p.kw("rename", "to") {
		return p.roleID()
	}
	return (p.kw("add", "user") || p.kw("drop", "user")) && p.roleList()
}

// alterDatabase reads ALTER DATABASE name {SetResetClause | OWNER TO
// RoleSpec}; other forms: D.
func (p *wp) alterDatabase() bool {
	if !p.colID() {
		return false
	}
	if p.kw("owner", "to") {
		return p.roleSpec(false)
	}
	return p.setResetClause()
}

// setResetClause reads SET set_rest | RESET reset_rest as ALTER ROLE and
// ALTER DATABASE take them.
func (p *wp) setResetClause() bool {
	if p.kw("reset") {
		return p.resetRest()
	}
	if !p.kw("set") {
		return false
	}
	if p.kw("time", "zone") {
		return p.kw("local") || p.kw("default") || p.varValue()
	}
	if !p.varName() {
		return false
	}
	if p.kw("from", "current") {
		return true
	}
	if !p.kw("to") && !(p.tok().kind == tokOp && p.tok().value == "=" && p.next()) {
		return false
	}
	return p.kw("default") || p.list(p.varValue)
}

// varName reads var_name: ColId [. ColId]...
func (p *wp) varName() bool {
	if !p.colID() {
		return false
	}
	for p.punct(".") {
		if !p.colID() {
			return false
		}
	}
	return true
}

// varValue reads var_value: TRUE, FALSE, ON, NonReservedWord_or_Sconst, or
// NumericOnly.
func (p *wp) varValue() bool {
	return p.kw("true") || p.kw("false") || p.kw("on") || p.numericOnly() || p.nonReservedOrSconst()
}

// resetRest reads reset_rest after RESET (VariableResetStmt).
func (p *wp) resetRest() bool {
	return p.kw("time", "zone") || p.kw("transaction", "isolation", "level") || p.kw("session", "authorization") || p.kw("all") || p.varName()
}

// constraintAttributes reads ConstraintAttributeSpec as CREATE CONSTRAINT
// TRIGGER takes it (processCASbits: no NOT VALID or NO INHERIT, no
// conflicting or INITIALLY DEFERRED NOT DEFERRABLE pairs).
func (p *wp) constraintAttributes() bool {
	deferrable, initially := "", ""
	for {
		switch {
		case p.kw("not", "deferrable"):
			if deferrable != "" {
				return false
			}
			deferrable = "not"
		case p.kw("deferrable"):
			if deferrable != "" {
				return false
			}
			deferrable = "yes"
		case p.kw("initially", "immediate"), p.kw("initially", "deferred"):
			if initially != "" {
				return false
			}
			initially = p.toks[p.i-1].value
		default:
			return !(deferrable == "not" && initially == "deferred")
		}
	}
}

// createTrigger reads CreateTrigStmt.
func (p *wp) createTrigger() bool {
	p.kw("create")
	replace := p.kw("or", "replace")
	constraint := p.kw("constraint")
	if !p.kw("trigger") || !p.colID() {
		return false
	}
	if constraint {
		if replace || !p.kw("after") {
			// CREATE OR REPLACE CONSTRAINT TRIGGER is refused (0A000).
			return false
		}
	} else if !p.kw("before") && !p.kw("after") && !p.kw("instead", "of") {
		return false
	}
	seen := map[string]bool{}
	event := func() bool {
		t := p.tok()
		if p.done() || t.kind != tokIdent || seen[t.value] {
			// gram.y: duplicate trigger events specified (42601).
			return false
		}
		switch t.value {
		case "insert", "delete", "truncate":
			p.i++
		case "update":
			p.i++
			if p.kw("of") && !p.list(p.colID) {
				return false
			}
		default:
			return false
		}
		seen[t.value] = true
		return true
	}
	if !event() {
		return false
	}
	for p.kw("or") {
		if !event() {
			return false
		}
	}
	if !p.kw("on") || !p.anyName() {
		return false
	}
	if constraint {
		if p.kw("from") && !p.anyName() {
			return false
		}
		if !p.constraintAttributes() || !p.kw("for", "each", "row") {
			return false
		}
	} else {
		if p.kw("referencing") {
			n := 0
			for p.peekKw("new") || p.peekKw("old") {
				p.i++
				if !p.kw("table") && !p.kw("row") {
					return false
				}
				p.kw("as")
				if !p.colID() {
					return false
				}
				n++
			}
			if n == 0 {
				return false
			}
		}
		if p.kw("for") {
			p.kw("each")
			if !p.kw("row") && !p.kw("statement") {
				return false
			}
		}
	}
	if p.kw("when") && !p.parenGroup() {
		return false
	}
	if !p.kw("execute") || !p.kw("function") && !p.kw("procedure") {
		return false
	}
	if _, err := p.funcName(); err != nil || !p.punct("(") {
		return false
	}
	if p.punct(")") {
		return true
	}
	arg := func() bool {
		return p.iconst() || p.sconst() || !p.done() && p.tok().kind == tokNumber && p.next() || p.colLabel()
	}
	return p.list(arg) && p.punct(")")
}

// createPolicy reads CreatePolicyStmt after CREATE POLICY.
func (p *wp) createPolicy() bool {
	if !p.colID() || !p.kw("on") || !p.anyName() {
		return false
	}
	if p.kw("as") {
		t := p.tok()
		if p.done() || t.kind != tokIdent || t.value != "permissive" && t.value != "restrictive" {
			return false
		}
		p.i++
	}
	if p.kw("for") && !(p.kw("all") || p.kw("select") || p.kw("insert") || p.kw("update") || p.kw("delete")) {
		return false
	}
	return p.policyTail()
}

// policyTail reads [TO role_list] [USING (a_expr)] [WITH CHECK (a_expr)].
func (p *wp) policyTail() bool {
	if p.kw("to") && !p.list(func() bool { return p.roleSpec(true) }) {
		return false
	}
	if p.kw("using") && !p.parenGroup() {
		return false
	}
	if p.kw("with", "check") && !p.parenGroup() {
		return false
	}
	return true
}

// alterPolicy reads AlterPolicyStmt and ALTER POLICY ... RENAME TO.
func (p *wp) alterPolicy() bool {
	if !p.colID() || !p.kw("on") || !p.anyName() {
		return false
	}
	if p.kw("rename", "to") {
		return p.colID()
	}
	return !p.done() && p.policyTail()
}

// alterTrigger reads ALTER TRIGGER name ON t {RENAME TO name | [NO]
// DEPENDS ON EXTENSION name}.
func (p *wp) alterTrigger() bool {
	if !p.colID() || !p.kw("on") || !p.anyName() {
		return false
	}
	if p.kw("rename", "to") {
		return p.colID()
	}
	p.kw("no")
	return p.kw("depends", "on", "extension") && p.colID()
}

// collationAttributes are the attributes DefineCollation reads
// (collationcmds.c 92-129); another is refused (42601).
var collationAttributes = map[string]bool{"from": true, "locale": true, "lc_collate": true, "lc_ctype": true, "provider": true, "deterministic": true, "rules": true, "version": true}

// operatorAttributes are DefineOperator's (operatorcmds.c 100-160);
// another is only a warning there, but the reader stays closed.
var operatorAttributes = map[string]bool{"leftarg": true, "rightarg": true, "function": true, "procedure": true, "commutator": true, "negator": true, "restrict": true, "join": true, "hashes": true, "merges": true}

// aggregateAttributes are the DefineAggregate attributes the reader takes
// (aggregatecmds.c 100-190; a closed subset).
var aggregateAttributes = map[string]bool{"sfunc": true, "stype": true, "finalfunc": true, "initcond": true, "combinefunc": true, "parallel": true}

// definition reads ( def_elem [, ...] ) with def_elem = name [= def_arg]
// and the attribute names of attrs.
func (p *wp) definition(attrs map[string]bool) bool {
	if !p.punct("(") {
		return false
	}
	elem := func() bool {
		t := p.tok()
		if p.done() || t.kind != tokIdent && t.kind != tokQuoted || !attrs[t.value] {
			return false
		}
		p.i++
		if !(p.tok().kind == tokOp && p.tok().value == "=" && p.next()) {
			return true
		}
		// def_arg: func_type, a reserved keyword, an operator, NumericOnly,
		// Sconst or NONE.
		if p.sconst() || p.numericOnly() || p.kw("none") {
			return true
		}
		if !p.done() && p.tok().kind == tokOp {
			p.i++
			return true
		}
		if !p.done() && p.tok().kind == tokIdent && pgKeywordCategories[p.tok().value] == 'R' {
			p.i++
			return true
		}
		return p.typename()
	}
	return p.list(elem) && p.punct(")")
}

// createDefinition reads CREATE COLLATION, STATISTICS, PUBLICATION,
// OPERATOR, CAST and AGGREGATE.
func (p *wp) createDefinition() bool {
	p.kw("create")
	switch {
	case p.kw("collation"):
		p.kw("if", "not", "exists")
		if !p.anyName() {
			return false
		}
		if p.kw("from") {
			return p.anyName()
		}
		return p.definition(collationAttributes)
	case p.kw("statistics"):
		p.kw("if", "not", "exists")
		if !p.peekKw("on") && !p.anyName() {
			return false
		}
		if p.isPunct("(") && !p.columnList() {
			return false
		}
		if !p.kw("on") {
			return false
		}
		if !p.list(func() bool { return p.parenGroup() || p.colID() }) {
			return false
		}
		return p.kw("from") && p.anyName()
	case p.kw("publication"):
		if !p.colID() {
			return false
		}
		if p.kw("for") {
			switch {
			case p.kw("all", "tables"):
			case p.kw("tables", "in", "schema"):
				if !p.list(p.colID) {
					return false
				}
			case p.kw("table"):
				if !p.list(func() bool {
					p.kw("table")
					return p.anyName()
				}) {
					return false
				}
			default:
				return false
			}
		}
		if p.kw("with") {
			return p.parenGroup()
		}
		return true
	case p.kw("operator"):
		// any_operator: [ColId .] operator characters.
		if (p.tok().kind == tokIdent || p.tok().kind == tokQuoted) && p.isPunctAt(1, ".") {
			p.i += 2
		}
		if p.done() || p.tok().kind != tokOp {
			return false
		}
		p.i++
		return p.definition(operatorAttributes)
	case p.kw("cast"):
		if !p.castTypes() {
			return false
		}
		switch {
		case p.kw("with", "function"):
			if !p.funcWithArgtypes() {
				return false
			}
		case p.kw("without", "function"), p.kw("with", "inout"):
		default:
			return false
		}
		_ = p.kw("as", "assignment") || p.kw("as", "implicit")
		return true
	case p.kw("aggregate"), p.kw("or", "replace", "aggregate"):
		return p.aggrWithArgtypes() && p.definition(aggregateAttributes)
	}
	return false
}

// lockStmt reads LockStmt after LOCK.
func (p *wp) lockStmt() bool {
	p.kw("table")
	if !p.list(func() bool {
		if p.kw("only") {
			if p.punct("(") {
				return p.anyName() && p.punct(")")
			}
			return p.anyName()
		}
		if !p.anyName() {
			return false
		}
		if !p.done() && p.tok().kind == tokOp && p.tok().value == "*" {
			p.i++
		}
		return true
	}) {
		return false
	}
	if p.kw("in") {
		if !(p.kw("access", "share") || p.kw("row", "share") || p.kw("row", "exclusive") || p.kw("share", "update", "exclusive") ||
			p.kw("share", "row", "exclusive") || p.kw("share") || p.kw("exclusive") || p.kw("access", "exclusive")) {
			return false
		}
		if !p.kw("mode") {
			return false
		}
	}
	p.kw("nowait")
	return true
}

// securityLabel reads SecLabelStmt after SECURITY LABEL.
func (p *wp) securityLabel() bool {
	if p.kw("for") && !p.nonReservedOrSconst() {
		return false
	}
	return p.kw("on") && p.commentObject() && p.kw("is") && (p.sconst() || p.kw("null"))
}

// transactionStmt reads TransactionStmt and TransactionStmtLegacy without
// transaction modes and without AND CHAIN, whose effects the pass does not
// model (T1's P6 table): those forms are D.
func (p *wp) transactionStmt() bool {
	opt := func() { _ = p.kw("work") || p.kw("transaction") }
	chain := func() { p.kw("and", "no", "chain") }
	switch {
	case p.kw("begin"):
		opt()
		return true
	case p.kw("start", "transaction"):
		return true
	case p.kw("commit", "prepared"), p.kw("rollback", "prepared"), p.kw("prepare", "transaction"):
		return p.sconst()
	case p.kw("commit"), p.kw("end"), p.kw("abort"):
		opt()
		chain()
		return true
	case p.kw("rollback"):
		opt()
		if p.kw("to") {
			p.kw("savepoint")
			return p.colID()
		}
		chain()
		return true
	case p.kw("savepoint"):
		return p.colID()
	case p.kw("release"):
		p.kw("savepoint")
		return p.colID()
	}
	return false
}

// createExtension reads CreateExtensionStmt after CREATE EXTENSION.
func (p *wp) createExtension() bool {
	p.kw("if", "not", "exists")
	if !p.colID() {
		return false
	}
	p.kw("with")
	for !p.done() {
		switch {
		case p.kw("schema"):
			if !p.colID() {
				return false
			}
		case p.kw("version"):
			if !p.nonReservedOrSconst() {
				return false
			}
		case p.kw("cascade"):
		default:
			// FROM is refused (0A000); anything else is a syntax error.
			return false
		}
	}
	return true
}

// createSchema reads CreateSchemaStmt: the name or AUTHORIZATION, then the
// schema elements (CREATE TABLE, INDEX, SEQUENCE, TRIGGER, VIEW and GRANT),
// each read by its own row's reader (IF NOT EXISTS takes no elements:
// 0A000).
func (p *wp) createSchema() bool {
	ifNotExists := p.kw("if", "not", "exists")
	if p.kw("authorization") {
		if !p.roleSpec(false) {
			return false
		}
	} else {
		if !p.colID() {
			return false
		}
		if p.kw("authorization") && !p.roleSpec(false) {
			return false
		}
	}
	if p.done() {
		return true
	}
	if ifNotExists {
		return false
	}
	for !p.done() {
		if !p.peekKw("create") && !p.peekKw("grant") {
			return false
		}
		start := p.i
		end := len(p.toks)
		depth := 0
		for j := start + 1; j < len(p.toks); j++ {
			t := p.toks[j]
			if t.kind == tokPunct && (t.value == "(" || t.value == "[") {
				depth++
			}
			if t.kind == tokPunct && (t.value == ")" || t.value == "]") {
				depth--
			}
			if depth == 0 && t.kind == tokIdent && (t.value == "create" || t.value == "grant") {
				end = j
				break
			}
		}
		elem := p.toks[start:end]
		e := &cursor{toks: elem, src: p.src}
		ok := e.peekKw("create", "table") || e.peekKw("create", "unique", "index") || e.peekKw("create", "index") ||
			e.peekKw("create", "sequence") || e.peekKw("create", "view") || e.peekKw("create", "or", "replace", "view") ||
			e.peekKw("create", "trigger") || e.peekKw("create", "or", "replace", "trigger") || e.peekKw("create", "constraint", "trigger") || e.peekKw("grant")
		if !ok {
			return false
		}
		a := analyzeForm(statement{tokens: elem}, p.src, nil, false)
		if a.class == classError || wholeStatement(elem, p.src) != "" || a.create != nil && a.create.unmodeled != "" && !createTableM1bWhole(elem, p.src) {
			return false
		}
		p.i = end
	}
	return true
}

// createEventTrigger reads CreateEventTrigStmt after CREATE EVENT TRIGGER.
func (p *wp) createEventTrigger() bool {
	if !p.colID() || !p.kw("on") || !p.colLabel() {
		return false
	}
	if p.kw("when") {
		item := func() bool {
			return p.colID() && p.kw("in") && p.punct("(") && p.list(p.sconst) && p.punct(")")
		}
		if !item() {
			return false
		}
		for p.kw("and") {
			if !item() {
				return false
			}
		}
	}
	if !p.kw("execute") || !p.kw("function") && !p.kw("procedure") {
		return false
	}
	if _, err := p.funcName(); err != nil {
		return false
	}
	return p.punct("(") && p.punct(")")
}

// createEnum reads CREATE TYPE any_name AS ENUM ( [Sconst [, ...]] ).
func (p *wp) createEnum() bool {
	if !p.anyName() || !p.kw("as", "enum") || !p.punct("(") {
		return false
	}
	if p.punct(")") {
		return true
	}
	return p.list(p.sconst) && p.punct(")")
}

// alterType reads AlterEnumStmt, AlterCompositeTypeStmt and the RENAME,
// OWNER and SET SCHEMA forms of ALTER TYPE (rows R3 and R4).
func (p *wp) alterType() bool {
	if !p.anyName() {
		return false
	}
	switch {
	case p.kw("add", "value"):
		p.kw("if", "not", "exists")
		if !p.sconst() {
			return false
		}
		if p.kw("before") || p.kw("after") {
			return p.sconst()
		}
		return true
	case p.kw("rename", "value"):
		return p.sconst() && p.kw("to") && p.sconst()
	case p.kw("rename", "to"):
		return p.colID()
	case p.kw("rename", "attribute"):
		if !p.colID() || !p.kw("to") || !p.colID() {
			return false
		}
		p.dropBehavior()
		return true
	case p.kw("owner", "to"):
		return p.roleSpec(false)
	case p.kw("set", "schema"):
		return p.colID()
	}
	return p.list(func() bool {
		switch {
		case p.kw("add", "attribute"):
			if !p.colID() || !p.typename() {
				return false
			}
			if p.kw("collate") && !p.anyName() {
				return false
			}
		case p.kw("drop", "attribute"):
			p.kw("if", "exists")
			if !p.colID() {
				return false
			}
		case p.kw("alter", "attribute"):
			if !p.colID() {
				return false
			}
			p.kw("set", "data")
			if !p.kw("type") || !p.typename() {
				return false
			}
			if p.kw("collate") && !p.anyName() {
				return false
			}
		default:
			return false
		}
		p.dropBehavior()
		return true
	})
}

// alterDomain reads AlterDomainStmt and the RENAME, OWNER and SET SCHEMA
// forms of ALTER DOMAIN.
func (p *wp) alterDomain() bool {
	if !p.anyName() {
		return false
	}
	switch {
	case p.kw("rename", "to"), p.kw("set", "schema"):
		return p.colID()
	case p.kw("rename", "constraint"):
		return p.colID() && p.kw("to") && p.colID()
	case p.kw("owner", "to"):
		return p.roleSpec(false)
	case p.kw("set", "default"):
		return exprSyntax(p.cursor)
	case p.kw("drop", "default"), p.kw("set", "not", "null"), p.kw("drop", "not", "null"):
		return true
	case p.kw("validate", "constraint"):
		return p.colID()
	case p.kw("drop", "constraint"):
		p.kw("if", "exists")
		if !p.colID() {
			return false
		}
		p.dropBehavior()
		return true
	case p.kw("add"):
		if p.kw("constraint") && !p.colID() {
			return false
		}
		if p.kw("not", "null") {
			return true
		}
		if !p.kw("check") || !p.parenExpr() {
			return false
		}
		p.kw("not", "valid")
		return true
	}
	return false
}

// alterView reads the ALTER VIEW and ALTER MATERIALIZED VIEW forms of
// RenameStmt, AlterObjectSchemaStmt, AlterObjectDependsStmt and the
// alter_table_cmds a view takes (OWNER TO, ALTER COLUMN SET/DROP DEFAULT,
// SET/RESET (...)).
func (p *wp) alterView(materialized bool) bool {
	p.kw("if", "exists")
	if !p.anyName() {
		return false
	}
	switch {
	case p.kw("rename", "to"), p.kw("set", "schema"):
		return p.colID()
	case p.kw("rename"):
		p.kw("column")
		return p.colID() && p.kw("to") && p.colID()
	case materialized && (p.kw("depends", "on", "extension") || p.kw("no", "depends", "on", "extension")):
		return p.colID()
	}
	return p.list(func() bool {
		switch {
		case p.kw("owner", "to"):
			return p.roleSpec(false)
		case (p.peekKw("set") || p.peekKw("reset")) && p.isPunctAt(1, "("):
			p.i++
			end, err := p.group()
			if err != nil {
				return false
			}
			for _, o := range storageOptions(p.cursor, p.i, end) {
				if !o.ok {
					return false
				}
			}
			p.i = end
			return true
		case !materialized && p.kw("alter"):
			p.kw("column")
			if !p.colID() {
				return false
			}
			if p.kw("drop", "default") {
				return true
			}
			return p.kw("set", "default") && exprSyntax(p.cursor)
		}
		return false
	})
}

// isPunctAt reports punctuation v at offset k from the cursor.
func (p *wp) isPunctAt(k int, v string) bool {
	j := p.i + k
	return j < len(p.toks) && p.toks[j].kind == tokPunct && p.toks[j].value == v
}

// alterSequence governs ALTER SEQUENCE except its option list (U5's
// reader): RENAME TO, SET SCHEMA, OWNER TO, SET LOGGED/UNLOGGED.
func (p *wp) alterSequence() (governed, ok bool) {
	q := &wp{cursor: &cursor{toks: p.toks, i: p.i + 2, src: p.src}}
	q.kw("if", "exists")
	if !q.anyName() {
		p.i = q.i
		return true, false
	}
	if !q.done() && q.tok().kind == tokIdent && (seqOptionWords[q.tok().value] || q.peekKw("owned", "by")) {
		// U5 reads the option list.
		return false, false
	}
	p.i = q.i
	switch {
	case p.kw("rename", "to"), p.kw("set", "schema"):
		return true, p.colID()
	case p.kw("owner", "to"):
		return true, p.roleSpec(false)
	case p.kw("set", "logged"), p.kw("set", "unlogged"):
		return true, true
	}
	return true, false
}

// funcOption reads one common_func_opt_item (gram.y 8441-8493).
func (p *wp) funcOption() bool {
	switch {
	case p.kw("called", "on", "null", "input"), p.kw("returns", "null", "on", "null", "input"), p.kw("strict"),
		p.kw("immutable"), p.kw("stable"), p.kw("volatile"), p.kw("external", "security", "definer"),
		p.kw("external", "security", "invoker"), p.kw("security", "definer"), p.kw("security", "invoker"),
		p.kw("leakproof"), p.kw("not", "leakproof"):
		return true
	case p.kw("cost"), p.kw("rows"):
		return p.numericOnly()
	case p.kw("support"):
		return p.anyName()
	case p.kw("parallel"):
		return p.colID()
	case p.kw("reset"):
		return p.kw("all") || p.varName()
	case p.peekKw("set"):
		q := &wp{cursor: &cursor{toks: p.toks, i: p.i + 1, src: p.src}}
		if !q.varName() {
			return false
		}
		p.i = q.i
		if p.kw("from", "current") {
			return true
		}
		if !p.kw("to") && !(p.tok().kind == tokOp && p.tok().value == "=" && p.next()) {
			return false
		}
		if p.kw("default") {
			return true
		}
		// A var_list ends where the next option starts.
		for {
			if !p.varValue() {
				return false
			}
			if !p.punct(",") {
				return true
			}
		}
	}
	return false
}

// alterFunction reads AlterFunctionStmt and the RENAME, OWNER, SET SCHEMA
// and DEPENDS forms after ALTER {FUNCTION | PROCEDURE | ROUTINE}.
func (p *wp) alterFunction() bool {
	if !p.funcWithArgtypes() {
		return false
	}
	switch {
	case p.kw("rename", "to"), p.kw("set", "schema"):
		return p.colID()
	case p.kw("owner", "to"):
		return p.roleSpec(false)
	case p.kw("depends", "on", "extension"), p.kw("no", "depends", "on", "extension"):
		return p.colID()
	}
	if !p.funcOption() {
		return false
	}
	for !p.done() && !p.peekKw("restrict") {
		if !p.funcOption() {
			return false
		}
	}
	p.kw("restrict")
	return true
}

// dropStmt governs DropStmt, RemoveFuncStmt, RemoveAggrStmt, RemoveOperStmt,
// DropCastStmt, DropRoleStmt and DropOwnedStmt; DROP TABLE, INDEX and
// SCHEMA keep their own reader (rows M6, M7, R5 already read them whole),
// and DROP DATABASE / TABLESPACE are F1.
func (p *wp) dropStmt() (governed, ok bool) {
	q := &wp{cursor: &cursor{toks: p.toks, i: p.i + 1, src: p.src}}
	if q.peekKw("table") || q.peekKw("index") || q.peekKw("schema") || q.peekKw("database") || q.peekKw("tablespace") {
		return false, false
	}
	p.i++
	names := func(item func() bool) bool {
		p.kw("if", "exists")
		if !p.list(item) {
			return false
		}
		p.dropBehavior()
		return true
	}
	switch {
	case p.kw("sequence"), p.kw("view"), p.kw("materialized", "view"), p.kw("foreign", "table"), p.kw("collation"),
		p.kw("conversion"), p.kw("statistics"), p.kw("text", "search", "parser"), p.kw("text", "search", "dictionary"),
		p.kw("text", "search", "template"), p.kw("text", "search", "configuration"), p.kw("type"), p.kw("domain"):
		return true, names(p.anyName)
	case p.kw("access", "method"), p.kw("event", "trigger"), p.kw("extension"), p.kw("foreign", "data", "wrapper"),
		p.kw("procedural", "language"), p.kw("language"), p.kw("publication"), p.kw("server"), p.kw("subscription"):
		return true, names(p.colID)
	case p.kw("function"), p.kw("procedure"), p.kw("routine"):
		return true, names(p.funcWithArgtypes)
	case p.kw("aggregate"):
		return true, names(p.aggrWithArgtypes)
	case p.kw("operator"):
		if p.peekKw("class") || p.peekKw("family") {
			return true, false
		}
		return true, names(p.operatorWithArgtypes)
	case p.kw("cast"):
		p.kw("if", "exists")
		if !p.castTypes() {
			return true, false
		}
		p.dropBehavior()
		return true, true
	case p.kw("trigger"), p.kw("policy"), p.kw("rule"):
		p.kw("if", "exists")
		if !p.colID() || !p.kw("on") || !p.anyName() {
			return true, false
		}
		p.dropBehavior()
		return true, true
	case p.kw("role"), p.kw("user"), p.kw("group"):
		if p.peekKw("mapping") {
			return true, false
		}
		p.kw("if", "exists")
		return true, p.roleList()
	case p.kw("owned", "by"):
		if !p.roleList() {
			return true, false
		}
		p.dropBehavior()
		return true, true
	}
	return true, false
}

// doStmt reads DoStmt after DO: one Sconst body and at most one LANGUAGE.
func (p *wp) doStmt() bool {
	body, lang := false, false
	for !p.done() {
		switch {
		case !body && p.sconst():
			body = true
		case !lang && p.kw("language"):
			if !p.nonReservedOrSconst() {
				return false
			}
			lang = true
		default:
			return false
		}
	}
	return body
}

// funcApplication reads CALL's func_application: func_name ( [args] ),
// each argument an a_expr (optionally name => a_expr).
func (p *wp) funcApplication() bool {
	if _, err := p.funcName(); err != nil || !p.punct("(") {
		return false
	}
	if p.punct(")") {
		return true
	}
	if !p.list(func() bool {
		if (p.tok().kind == tokIdent || p.tok().kind == tokQuoted) && p.i+1 < len(p.toks) && p.toks[p.i+1].kind == tokOp && p.toks[p.i+1].value == "=>" {
			p.i += 2
		}
		return exprSyntax(p.cursor)
	}) {
		return false
	}
	return p.punct(")")
}

// exprListParen reads ( a_expr [, ...] ).
func (p *wp) exprListParen() bool {
	return p.punct("(") && p.list(func() bool { return exprSyntax(p.cursor) }) && p.punct(")")
}

// clusterStmt reads ClusterStmt after CLUSTER.
func (p *wp) clusterStmt() bool {
	p.kw("verbose")
	if p.done() {
		return true
	}
	if !p.anyName() {
		return false
	}
	if p.kw("using") {
		return p.colID()
	}
	if p.kw("on") {
		return p.anyName()
	}
	return true
}

// reindexStmt reads ReindexStmt after REINDEX (without an option list).
func (p *wp) reindexStmt() bool {
	switch {
	case p.kw("index"), p.kw("table"):
		p.kw("concurrently")
		return p.anyName()
	case p.kw("schema"), p.kw("database"), p.kw("system"):
		p.kw("concurrently")
		return p.done() || p.colID()
	}
	return false
}

// copyStmt reads COPY [BINARY] qualified_name [(cols)] {FROM | TO}
// {Sconst | STDIN | STDOUT | PROGRAM Sconst} [[WITH] (options)]; the
// query form, old-style options and WHERE are D.
func (p *wp) copyStmt() bool {
	p.kw("copy")
	p.kw("binary")
	if !p.anyName() {
		return false
	}
	if p.isPunct("(") && !p.columnList() {
		return false
	}
	from := p.kw("from")
	if !from && !p.kw("to") {
		return false
	}
	switch {
	case from && p.kw("stdin"), !from && p.kw("stdout"):
	case p.kw("program"):
		if !p.sconst() {
			return false
		}
	default:
		if !p.sconst() {
			return false
		}
	}
	// The parenthesised option list (copy_generic_opt_list); the old
	// option forms and WHERE are not read: D.
	p.kw("with")
	if !p.isPunct("(") {
		return true
	}
	p.i++
	if !p.list(p.copyOption) {
		return false
	}
	return p.punct(")")
}

// copyOption reads one COPY option the reader takes (copy.c
// ProcessCopyOptions 489-680), with the values it accepts.
func (p *wp) copyOption() bool {
	boolean := func() bool {
		return p.kw("true") || p.kw("false") || p.kw("on") || p.kw("off") || !p.done() && p.tok().kind == tokNumber && (p.tok().value == "0" || p.tok().value == "1") && p.next() || p.sconst()
	}
	switch {
	case p.kw("format"):
		return p.kw("text") || p.kw("csv") || p.kw("binary")
	case p.kw("freeze"):
		return p.done() || p.isPunct(",") || p.isPunct(")") || boolean()
	case p.kw("header"):
		return p.done() || p.isPunct(",") || p.isPunct(")") || p.kw("match") || boolean()
	case p.kw("delimiter"), p.kw("null"), p.kw("default"), p.kw("quote"), p.kw("escape"), p.kw("encoding"):
		return p.sconst()
	case p.kw("force_quote"), p.kw("force_not_null"), p.kw("force_null"):
		if !p.done() && p.tok().kind == tokOp && p.tok().value == "*" {
			p.i++
			return true
		}
		return p.columnList()
	}
	return false
}

// rangeAttributes are DefineRange's attributes (typecmds.c 1388-1450).
var rangeAttributes = map[string]bool{"subtype": true, "subtype_opclass": true, "collation": true, "canonical": true, "subtype_diff": true, "multirange_type_name": true}

// createType reads the shell, composite and range forms of CREATE TYPE
// (DefineStmt, CompositeTypeStmt); the base-type form (superuser) is D.
func (p *wp) createType() bool {
	if !p.anyName() {
		return false
	}
	switch {
	case p.done():
		return true
	case p.kw("as", "range"):
		return p.definition(rangeAttributes)
	case p.kw("as"):
		if !p.punct("(") {
			return false
		}
		if p.punct(")") {
			return true
		}
		if !p.list(func() bool {
			if !p.colID() || !p.typename() {
				return false
			}
			return !p.kw("collate") || p.anyName()
		}) {
			return false
		}
		return p.punct(")")
	}
	return false
}

// wholeStatementID is the precondition a V7 failure cites: the P7 id of
// the row the statement would have had ("" for D, which needs none).
func wholeStatementID(row string, toks []token) string {
	c := &cursor{toks: toks}
	if c.peekKw("alter", "type") && (depth0(toks, 2, "value") > 0) {
		// ADD VALUE / RENAME VALUE that the enum reader did not take.
		return "R3.P7"
	}
	switch row {
	case "N1":
		return "N1.P7"
	case "N2":
		return "N2.P7"
	case "N3":
		return "N3.P7"
	case "T1":
		return "T1.P7"
	case "O1":
		return "O1.P7"
	case "O2":
		return "O2.P7"
	case "O3":
		return "O3.P7"
	case "O5":
		return "O5.P7"
	case "R1":
		return "R1.P7"
	case "R2":
		return "R2.P7"
	case "R3":
		return "R3.P7"
	case "R4":
		return "R4.P7"
	case "R5":
		return "R5.P7"
	case "R6":
		return "R6.P7"
	case "M9":
		return "M9.P7"
	}
	return ""
}

// alterCmdWhole reads one alter_table_cmd (or partition_cmd) that the pass
// models as an M5k word, completely (V7): the productions of gram.y
// alter_table_cmd 2390-2905 for the M5k words. A form outside them is D.
func alterCmdWhole(toks []token, from int, src []byte) bool {
	p := &wp{cursor: &cursor{toks: toks, i: from, src: src}}
	ok := p.alterCmd()
	return ok && p.done()
}

func (p *wp) alterCmd() bool {
	switch {
	case p.kw("owner", "to"):
		return p.roleSpec(false)
	case p.kw("enable", "row", "level", "security"), p.kw("disable", "row", "level", "security"),
		p.kw("force", "row", "level", "security"), p.kw("no", "force", "row", "level", "security"):
		return true
	case p.kw("enable", "trigger"), p.kw("enable", "always", "trigger"), p.kw("enable", "replica", "trigger"), p.kw("disable", "trigger"):
		return p.kw("all") || p.kw("user") || p.colID()
	case p.kw("enable", "rule"), p.kw("enable", "always", "rule"), p.kw("enable", "replica", "rule"), p.kw("disable", "rule"):
		return p.colID()
	case p.kw("set", "logged"), p.kw("set", "unlogged"), p.kw("set", "without", "cluster"), p.kw("set", "without", "oids"):
		return true
	case p.kw("set", "tablespace"):
		return p.colID()
	case p.kw("set", "access", "method"):
		return p.kw("default") || p.colID()
	case (p.peekKw("set") || p.peekKw("reset")) && p.isPunctAt(1, "("):
		p.i++
		end, err := p.group()
		if err != nil {
			return false
		}
		p.i = end
		return true
	case p.kw("replica", "identity"):
		if p.kw("using", "index") {
			return p.colID()
		}
		return p.kw("default") || p.kw("full") || p.kw("nothing")
	case p.kw("cluster", "on"):
		return p.colID()
	case p.kw("no", "inherit"), p.kw("inherit"):
		return p.anyName()
	case p.kw("of"):
		return p.anyName()
	case p.kw("not", "of"):
		return true
	case p.kw("detach", "partition"):
		if !p.anyName() {
			return false
		}
		_ = p.kw("concurrently") || p.kw("finalize")
		return true
	case p.kw("attach", "partition"):
		return p.anyName() && p.partitionBound()
	case p.kw("validate", "constraint"):
		return p.colID()
	case p.kw("rename", "constraint"):
		return p.colID() && p.kw("to") && p.colID()
	case p.kw("alter", "constraint"):
		if !p.colID() || p.done() {
			return false
		}
		return p.constraintAttributes()
	case p.kw("alter"):
		p.kw("column")
		if !p.colID() {
			return false
		}
		switch {
		case p.kw("set", "statistics"):
			return p.signedIconst()
		case p.kw("set", "storage"):
			return p.kw("plain") || p.kw("external") || p.kw("extended") || p.kw("main") || p.kw("default")
		case p.kw("set", "compression"):
			return p.kw("default") || p.colID()
		case p.kw("drop", "expression"), p.kw("drop", "identity"):
			p.kw("if", "exists")
			return true
		case p.kw("add", "generated", "always", "as", "identity"), p.kw("add", "generated", "by", "default", "as", "identity"):
			// The sequence options in parentheses are not read: D.
			return true
		}
		return false
	}
	return false
}

// partitionBound reads PartitionBoundSpec: FOR VALUES IN (list), FROM
// (list) TO (list), WITH (MODULUS n, REMAINDER n), or DEFAULT.
func (p *wp) partitionBound() bool {
	if p.kw("default") {
		return true
	}
	if !p.kw("for", "values") {
		return false
	}
	switch {
	case p.kw("in"):
		return p.exprListParen()
	case p.kw("from"):
		return p.exprListParen() && p.kw("to") && p.exprListParen()
	case p.kw("with"):
		return p.punct("(") && p.kw("modulus") && p.iconst() && p.punct(",") && p.kw("remainder") && p.iconst() && p.punct(")")
	}
	return false
}

// createTableM1bWhole reads a row M1b statement completely (V7): CREATE
// TABLE n (elements with LIKE) [INHERITS (...)], OF t, or PARTITION OF p
// [(table constraints)] bound, each followed by OptPartitionSpec,
// table_access_method_clause, OptWith, OnCommitOption and OptTableSpace in
// gram.y's order (CreateStmt 3739-3840).
func createTableM1bWhole(toks []token, src []byte) bool {
	p := &wp{cursor: &cursor{toks: toks, src: src}}
	if !p.kw("create") {
		return false
	}
	for p.kw("global") || p.kw("local") || p.kw("temp") || p.kw("temporary") || p.kw("unlogged") {
	}
	if !p.kw("table") {
		return false
	}
	p.kw("if", "not", "exists")
	if !p.anyName() {
		return false
	}
	switch {
	case p.kw("of"):
		if !p.anyName() {
			return false
		}
	case p.kw("partition", "of"):
		if !p.anyName() {
			return false
		}
		if p.isPunct("(") {
			end, err := p.group()
			if err != nil {
				return false
			}
			for _, r := range p.splitList(p.i+1, end-1) {
				var tc tableConstraints
				if r[0] >= r[1] || !isConstraintStart(p.cursor, r[0]) || tableConstraint(p.cursor, r[0], r[1], &tc) != nil {
					return false
				}
			}
			p.i = end
		}
		if !p.partitionBound() {
			return false
		}
	default:
		if !p.isPunct("(") {
			return false
		}
		end, err := p.group()
		if err != nil {
			return false
		}
		if end > p.i+2 {
			for _, r := range p.splitList(p.i+1, end-1) {
				if r[0] >= r[1] {
					return false
				}
				switch {
				case p.toks[r[0]].kind == tokIdent && p.toks[r[0]].value == "like":
					q := &wp{cursor: &cursor{toks: p.toks[:r[1]], i: r[0] + 1, src: p.src}}
					if !q.anyName() {
						return false
					}
					for q.kw("including") || q.kw("excluding") {
						if !(q.kw("comments") || q.kw("compression") || q.kw("constraints") || q.kw("defaults") || q.kw("identity") ||
							q.kw("generated") || q.kw("indexes") || q.kw("statistics") || q.kw("storage") || q.kw("all")) {
							return false
						}
					}
					if !q.done() {
						return false
					}
				case isConstraintStart(p.cursor, r[0]):
					var tc tableConstraints
					if tableConstraint(p.cursor, r[0], r[1], &tc) != nil {
						return false
					}
				default:
					if _, err := columnDefinition(p.cursor, r[0], r[1]); err != nil {
						return false
					}
				}
			}
		}
		p.i = end
		if p.kw("inherits") {
			if !p.punct("(") || !p.list(p.anyName) || !p.punct(")") {
				return false
			}
		}
	}
	if p.kw("partition", "by") {
		if !(p.kw("range") || p.kw("list") || p.kw("hash")) || !p.parenGroup() {
			return false
		}
	}
	if p.kw("using") && !p.colID() {
		return false
	}
	if p.kw("with") {
		if !p.parenGroup() {
			return false
		}
	} else {
		p.kw("without", "oids")
	}
	if p.kw("on", "commit") && !(p.kw("drop") || p.kw("delete", "rows") || p.kw("preserve", "rows")) {
		return false
	}
	if p.kw("tablespace") && !p.colID() {
		return false
	}
	return p.done()
}

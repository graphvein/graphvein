package schema

import (
	"fmt"
	"strings"
)

// Session search_path (T018a2 Revision 1.5 U3, class P13, rows O3 and
// Q1). The pass follows the session's search_path through the run: sqlx
// runs every migration of one run on one connection (sqlx-core 0.8.6
// migrate/migrator.rs run 131-136), and records each migration with an
// unqualified INSERT INTO _sqlx_migrations at the end of the migration's
// string, inside its transaction (sqlx-postgres 0.8.6 migrate.rs
// execute_migration 275-297), then an unqualified UPDATE after the
// commit (apply 206-247). An unqualified CREATE lands in the first
// existing schema of the path (namespace.c RecomputeNamespacePath and
// namespace_search_path; an empty path refuses creation, 3F000;
// pg_catalog as the creation schema is refused, 42501).
//
// Entries are known when they are pg_catalog, the default schema (E4's
// value, written here as defaultEntry; its "$user" is part of E4) or a
// schema the set creates (held: it exists; not held: it does not). Any
// other entry, "$user" written explicitly and pg_temp are unknown: E1
// makes outside schemas exist when a statement needs them, not when a
// path names them.

// defaultEntry stands for E4's default value of search_path.
const defaultEntry = "\x00default"

// pathChange is one statement's effect on search_path.
type pathChange struct {
	// local is SET LOCAL or set_config(..., true): the value lasts to the
	// end of the current transaction.
	local bool
	// entries is the new value (defaultEntry alone for DEFAULT and RESET).
	entries []string
	// role is SET/RESET ROLE or SESSION AUTHORIZATION: what "$user" names
	// changes.
	role bool
	// unknown says why the new value is not read.
	unknown string
	// later marks a setting for later sessions (ALTER ROLE/DATABASE ...
	// SET): sqlx's run keeps its one session, whose path does not change.
	later bool
}

// pathValue is a value of search_path the pass follows; nil entries with
// known false is an unknown value.
type pathValue struct {
	known   bool
	entries []string
}

var defaultPath = &pathValue{known: true, entries: []string{defaultEntry}}

// pathMode is how sqlx runs the current migration's string, as far as
// search_path is concerned.
type pathMode uint8

const (
	// pathTx: in sqlx's transaction (a SET LOCAL lasts to its end, and
	// the bookkeeping INSERT sees it).
	pathTx pathMode = iota
	// pathImplicit: a no-transaction string of several statements (a SET
	// LOCAL lasts to the end of the string; the INSERT runs after it).
	pathImplicit
	// pathSingle: a no-transaction string of one statement (SET LOCAL has
	// no effect, 25P01 warning).
	pathSingle
	// pathUnclear: the block structure is not one the model follows
	// (both block states, or transaction control in the string): any
	// change makes the path unknown.
	pathUnclear
)

// searchPathSet reads SET [SESSION | LOCAL] search_path {TO | =} list |
// DEFAULT, SET SCHEMA 'x', and SET/RESET of role and session_authorization
// (c after SET [SESSION | LOCAL]). ok is false for text gram.y rejects.
func searchPathSet(c *cursor, local bool) (analysis, bool) {
	a := searchPath("SET may change where unqualified names resolve")
	if c.acceptKw("schema") {
		if c.done() || c.tok().kind != tokString {
			return a, false
		}
		v, ok := literalValue(c.tok(), c.src)
		c.i++
		if !ok || !c.done() {
			return a, false
		}
		a.path = &pathChange{local: local, entries: []string{v}}
		return a, true
	}
	name := c.tok().value
	c.i++
	if name != "search_path" {
		a.path = &pathChange{local: local, role: true}
		return a, true
	}
	if c.acceptKw("from", "current") {
		a.path = &pathChange{local: local, unknown: "SET search_path FROM CURRENT"}
		return a, c.done()
	}
	if !c.acceptKw("to") && !(!c.done() && c.tok().kind == tokOp && c.tok().value == "=" && c.next()) {
		return a, false
	}
	if c.acceptKw("default") {
		a.path = &pathChange{local: local, entries: []string{defaultEntry}}
		return a, c.done()
	}
	p := &pathChange{local: local}
	for {
		if c.done() {
			return a, false
		}
		t := c.tok()
		switch t.kind {
		case tokIdent, tokQuoted:
			p.entries = append(p.entries, t.value)
		case tokString:
			v, ok := literalValue(t, c.src)
			if !ok {
				p.unknown = "a string the pass does not read"
			}
			p.entries = append(p.entries, v)
		case tokNumber:
			p.unknown = "a numeric list item"
		default:
			return a, false
		}
		c.i++
		if c.done() {
			break
		}
		if !c.acceptPunct(",") {
			return a, false
		}
	}
	a.path = p
	return a, true
}

// setConfigPath reads a whole statement SELECT [pg_catalog.]set_config(
// 'search_path', '<constant>', true | false), pg_dump's header form.
func setConfigPath(toks []token, src []byte) (*pathChange, bool) {
	i := 1
	if len(toks) > 3 && toks[1].kind == tokIdent && toks[1].value == "pg_catalog" && toks[2].kind == tokPunct && toks[2].value == "." {
		i = 3
	}
	if len(toks) != i+8 || toks[i].kind != tokIdent || toks[i].value != "set_config" {
		return nil, false
	}
	punct := func(k int, v string) bool { return toks[k].kind == tokPunct && toks[k].value == v }
	if !punct(i+1, "(") || !punct(i+3, ",") || !punct(i+5, ",") || !punct(i+7, ")") {
		return nil, false
	}
	if toks[i+2].kind != tokString || toks[i+4].kind != tokString || toks[i+6].kind != tokIdent {
		return nil, false
	}
	name, ok := literalValue(toks[i+2], src)
	if !ok || !strings.EqualFold(name, "search_path") {
		return nil, false
	}
	value, ok := literalValue(toks[i+4], src)
	if !ok {
		return nil, false
	}
	local := toks[i+6].value
	if local != "true" && local != "false" {
		return nil, false
	}
	entries, ok := splitIdentifierString(value)
	if !ok {
		// check_search_path refuses it (22023); the query reader says
		// the statement may fail.
		return nil, false
	}
	return &pathChange{local: local == "true", entries: entries}, true
}

// splitIdentifierString is varlena.c SplitIdentifierString with ','
// separators: items are trimmed, unquoted ones down-cased, double-quoted
// ones taken as written with "" read as ".
func splitIdentifierString(s string) ([]string, bool) {
	var out []string
	rest := strings.TrimLeft(s, " \t\n\r\f\v")
	if rest == "" {
		return nil, true
	}
	for {
		var item string
		if strings.HasPrefix(rest, `"`) {
			var b strings.Builder
			i := 1
			for {
				j := strings.IndexByte(rest[i:], '"')
				if j < 0 {
					return nil, false
				}
				b.WriteString(rest[i : i+j])
				i += j + 1
				if i < len(rest) && rest[i] == '"' {
					b.WriteByte('"')
					i++
					continue
				}
				break
			}
			item, rest = b.String(), rest[i:]
			if item == "" {
				return nil, false
			}
		} else {
			end := strings.IndexAny(rest, ", \t\n\r\f\v")
			if end < 0 {
				end = len(rest)
			}
			item, rest = strings.ToLower(rest[:end]), rest[end:]
			if item == "" {
				return nil, false
			}
		}
		out = append(out, item)
		rest = strings.TrimLeft(rest, " \t\n\r\f\v")
		if rest == "" {
			return out, true
		}
		if rest[0] != ',' {
			return nil, false
		}
		rest = strings.TrimLeft(rest[1:], " \t\n\r\f\v")
	}
}

// migrationPathMode is pathMode for a migration of n statements whose
// block structure tx follows.
func migrationPathMode(tx *txTracker, statements []statement) pathMode {
	for _, st := range statements {
		switch kind, _, _ := txStatement(st.tokens, nil); kind {
		case txBegin, txCommit, txRollback, txSavepoint, txRelease, txRollbackTo, txPrepare:
			return pathUnclear
		}
	}
	switch {
	case len(tx.paths) != 1:
		return pathUnclear
	case !tx.paths[0].noTx:
		return pathTx
	case len(statements) > 1:
		return pathImplicit
	}
	return pathSingle
}

// effectivePath is the value in force: the transaction-local one, else
// the session's.
func (s *state) effectivePath() *pathValue {
	if s.localPath != nil {
		return s.localPath
	}
	if s.sessionPath != nil {
		return s.sessionPath
	}
	return defaultPath
}

// changePath applies one statement's path change (nil: not read, and
// its scope unknown too).
func (s *state) changePath(p *pathChange) {
	unknown := &pathValue{}
	switch {
	case p != nil && p.later:
	case p == nil || s.pathMode == pathUnclear:
		s.sessionPath, s.localPath = unknown, unknown
	case p.role:
		// "$user", explicit or in E4's value, now names another role.
		if cur := s.effectivePath(); !cur.known || containsEntry(cur, "$user") || containsEntry(cur, defaultEntry) {
			s.sessionPath, s.localPath = unknown, unknown
		}
		if s.sessionPath == nil {
			// E4's value holds "$user".
			s.sessionPath = unknown
		}
	case p.local && s.pathMode == pathSingle:
		// SET LOCAL outside a block has no effect (25P01, warning).
	case p.local && p.unknown != "":
		s.localPath = unknown
	case p.local:
		s.localPath = &pathValue{known: true, entries: p.entries}
	case p.unknown != "":
		s.sessionPath, s.localPath = unknown, nil
	default:
		s.sessionPath, s.localPath = &pathValue{known: true, entries: p.entries}, nil
	}
}

func containsEntry(p *pathValue, e string) bool {
	for _, x := range p.entries {
		if x == e {
			return true
		}
	}
	return false
}

// entryState classifies one path entry at this point: "default", "catalog",
// "held" (a schema the set holds), "absent" (known not to exist) or
// "unknown".
func (s *state) entryState(e string) string {
	switch {
	case e == defaultEntry && s.userSchemaUnknown() && !s.rev16.bookkeeping:
		// V6: "$user" may name a set schema.
		return "unknown"
	case e == defaultEntry:
		return "default"
	case e == "pg_catalog":
		return "catalog"
	case e == "":
		return "absent"
	case e == "$user" || e == "pg_temp" || strings.HasPrefix(e, "pg_temp_") || e == "public":
		// public may be the default schema even when the set creates it
		// (CREATE SCHEMA IF NOT EXISTS public over E4's schema).
		return "unknown"
	case s.code.may[e]&kindSchema != 0 || s.code.gone[e]:
		// P12: code may have created, dropped or renamed it.
		return "unknown"
	case s.holds(e, kindSchema):
		return "held"
	case s.names.creates(e, kindSchema):
		return "absent"
	}
	return "unknown"
}

// creationSchema decides where an unqualified CREATE lands under the
// path in force (O3.P13): schema is "" for the default schema; refused
// and unknown say why Postgres refuses or may refuse it.
func (s *state) creationSchema() (schema string, refused, unknown string) {
	p := s.effectivePath()
	if !p.known {
		return "", "", "search_path is not known here"
	}
	for _, e := range p.entries {
		switch s.entryState(e) {
		case "default":
			return "", "", ""
		case "catalog":
			return "", "permission denied to create in pg_catalog (42501)", ""
		case "held":
			return e, "", ""
		case "unknown":
			if e == defaultEntry {
				return "", "", "the \"$user\" entry of the default search_path may name a schema the set created (the current user's name is not known, E4)"
			}
			return "", "", fmt.Sprintf("search_path entry %q may or may not name an existing schema", e)
		}
	}
	return "", "no schema has been selected to create in (3F000)", ""
}

// placeCreation applies O3.P13 to the unqualified names of the
// relations, tables and functions an analysis creates, before the state
// applies it: under a known path they take the creation schema; under an
// unknown one the statement may fail and the names stay unqualified (the
// tables are partial: the schema of the name is unknown).
func (s *state) placeCreation(a *analysis) {
	if !s.pathChanged {
		return
	}
	var names []*qname
	if a.create != nil {
		names = append(names, &a.create.name)
	}
	for i := range a.relations {
		names = append(names, &a.relations[i].q)
	}
	if a.sequence != nil && a.sequence.create {
		names = append(names, &a.sequence.q)
	}
	unqualified := false
	for _, q := range names {
		unqualified = unqualified || q.schema == nil
	}
	if !unqualified && a.function == nil {
		return
	}
	schema, refused, unknown := s.creationSchema()
	switch {
	case refused != "":
		s.mayFail(fmt.Sprintf("an unqualified CREATE under this search_path: %s (Postgres refuses)%s", refused, precondition("O3.P13")))
		return
	case unknown != "":
		s.unknownFact(fmt.Sprintf("an unqualified CREATE may be refused or land in another schema: %s%s", unknown, precondition("O3.P13")))
		return
	}
	if schema == "" {
		return
	}
	for _, q := range names {
		if q.schema == nil {
			v := schema
			q.schema = &v
		}
	}
}

// bookkeepingResolves decides whether p resolves the unqualified name
// _sqlx_migrations to the bookkeeping table in the default schema: proven
// (""), proven not (refused) or not proven (unknown).
func (s *state) bookkeepingResolves(p *pathValue) (refused, unknown string) {
	// V6: for sqlx's _sqlx_migrations, "$user" is any set schema, and no
	// set schema holds a relation of that name (checked below).
	s.rev16.bookkeeping = true
	defer func() { s.rev16.bookkeeping = false }()
	if !p.known {
		return "", "search_path is not known"
	}
	if s.names.creates("_sqlx_migrations", kindRelation) {
		return "", "the set creates a relation named _sqlx_migrations"
	}
	why := ""
	for _, e := range p.entries {
		switch s.entryState(e) {
		case "default":
			if why != "" {
				return "", why
			}
			return "", ""
		case "unknown":
			if why == "" {
				why = fmt.Sprintf("search_path entry %q may name a schema that holds a relation _sqlx_migrations", e)
			}
		}
	}
	if why != "" {
		return "", why
	}
	return "relation \"_sqlx_migrations\" does not exist under this search_path (42P01)", ""
}

// endPath applies Q1.P13 at the end of a migration's string: sqlx's
// INSERT runs under the path in force, and after a migration with a
// transaction its UPDATE runs under the session's path (a failure there
// stops the run after the migration committed). The transaction-local
// value ends here.
func (s *state) endPath() {
	insertPath := s.effectivePath()
	if s.pathMode == pathImplicit || s.pathMode == pathSingle {
		insertPath = s.sessionPath
		if insertPath == nil {
			insertPath = defaultPath
		}
	}
	if refused, unknown := s.bookkeepingResolves(insertPath); refused != "" {
		s.mayFail(fmt.Sprintf("sqlx records the migration with an unqualified INSERT: %s (Postgres refuses)%s", refused, precondition("Q1.P13")))
	} else if unknown != "" {
		s.unknownFact(fmt.Sprintf("sqlx's unqualified INSERT INTO _sqlx_migrations may be refused: %s%s", unknown, precondition("Q1.P13")))
	}
	s.localPath = nil
	s.rev16.cfbLocal = cfbUnset
	session := s.sessionPath
	if session == nil {
		session = defaultPath
	}
	if refused, unknown := s.bookkeepingResolves(session); refused != "" || unknown != "" {
		s.laterMayFail = fmt.Sprintf("sqlx's unqualified UPDATE of _sqlx_migrations after an earlier migration may have been refused (%s%s), which stops the run%s", refused, unknown, precondition("Q1.P13"))
	}
}

// bookkeepingStatement applies Q1.P13 to a statement that may drop,
// rename or move the default schema or a relation named _sqlx_migrations.
func (s *state) bookkeepingStatement(toks []token) {
	if s.set.tool == toolSupabase {
		s.supabaseBookkeeping(toks)
		return
	}
	c := &cursor{toks: toks}
	why := ""
	switch {
	case c.peekKw("drop", "schema"), c.peekKw("alter", "schema") && containsKw(c, "rename"):
		c.i = 2
		c.acceptKw("if", "exists")
		for !c.done() {
			t := c.tok()
			if (t.kind == tokIdent || t.kind == tokQuoted) && t.value == "public" && !reservedAfterSchema[t.value] {
				// Under E4 the default schema is public or a schema the
				// set never names.
				why = fmt.Sprintf("schema %s may be the default schema, which holds _sqlx_migrations", t.value)
				break
			}
			c.i++
			if !c.acceptPunct(",") {
				break
			}
		}
	case c.peekKw("drop"), c.peekKw("alter"):
		for _, t := range toks {
			if (t.kind == tokIdent || t.kind == tokQuoted) && t.value == "_sqlx_migrations" {
				why = "the statement names _sqlx_migrations"
				break
			}
		}
	}
	if why != "" {
		s.unknownFact(fmt.Sprintf("sqlx's unqualified INSERT INTO _sqlx_migrations may be refused: %s%s", why, precondition("Q1.P13")))
	}
}

// reservedAfterSchema are words that follow DROP/ALTER SCHEMA's names.
var reservedAfterSchema = map[string]bool{"cascade": true, "restrict": true, "rename": true, "owner": true}

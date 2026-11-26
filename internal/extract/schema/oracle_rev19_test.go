package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Harness rules of T018a2 Revision 1.9 (Step U-c): H21 (E3 is code logic,
// not names, kinds or syntax the set's text fixes), H22 (assumption hits
// on claimed migrations are listed) and H23 (the replay runs as a role
// that is not a superuser, E6). H24 is in applyMigration (Step U-b).

// h21CodeLogic reports whether a refusal raised inside code is code logic
// whatever statement raised it (H21 (a)): RAISE (P0001), INTO STRICT
// (P0002, P0003), ASSERT (P0004), or a data exception or integrity
// constraint violation (classes 22 and 23), which depend on the rows the
// code reads.
func h21CodeLogic(code string) bool {
	switch code {
	case "P0001", "P0002", "P0003", "P0004":
		return true
	}
	return strings.HasPrefix(code, "22") || strings.HasPrefix(code, "23")
}

var (
	// plpgsqlFrame is a PL/pgSQL error context frame (pl_exec.c
	// plpgsql_exec_error_callback): the function, the body line and the
	// statement kind.
	plpgsqlFrame = regexp.MustCompile(`^PL/pgSQL function (.+?) line (\d+) at (.+)$`)
	// h21ConstantExecute is an EXECUTE of a string constant, or of format()
	// of a constant with constant arguments, on one body line.
	h21ConstantExecute = regexp.MustCompile(`(?i)\bexecute\s+(?:e?'|\$[a-z0-9_]*\$|format\s*\(\s*(?:e?'(?:[^']|'')*'|\$[a-z0-9_]*\$)(?:\s*,\s*(?:e?'(?:[^']|'')*'|\d+))*\s*\))`)
	// formatTemplate finds the templates of format() calls in the set.
	formatTemplate = regexp.MustCompile(`(?i)\bformat\s*\(\s*'((?:[^']|'')*)'`)
	// formatSpec is one format() conversion (%s, %I, %L, with an optional
	// argument position and width), matched in the lower-cased template.
	formatSpec = regexp.MustCompile(`(?i)%(?:\d+\$)?-?\d*[sil]`)
)

// h21Statements are the frame kinds of H21 (b) whose statement is a static
// statement of the body when the frame has no `SQL statement` text.
var h21Statements = map[string]bool{"SQL statement": true, "PERFORM": true, "RETURN": true, "assignment": true, "EXECUTE": true}

// normalizeSQL collapses whitespace and case, for comparing a statement
// text with the set's text.
func normalizeSQL(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// h21Fixed reports whether the statement that raised a refusal inside code
// is fixed by the set's text (H21 (b)): the innermost `SQL statement "..."`
// text (or `SQL expression`, `PL/pgSQL assignment` text) equals, after
// normalising whitespace, a string constant or static statement of the
// set's text, or a format() of a template of the set whose substituted
// values the set's text writes; without such a text, the innermost
// PL/pgSQL frame is a static statement (SQL statement, PERFORM, RETURN,
// assignment) of a body the set wrote, or an EXECUTE of a constant on that
// body's line. Anything else is not fixed (E3, as before H21).
func h21Fixed(where, stmtText, setText string) bool {
	norm := normalizeSQL(setText)
	unescaped := strings.ReplaceAll(norm, "''", "'")
	for _, line := range strings.Split(where, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{`SQL statement "`, `SQL expression "`, `PL/pgSQL assignment "`} {
			text, ok := strings.CutPrefix(line, prefix)
			if !ok {
				continue
			}
			text = normalizeSQL(strings.TrimSuffix(text, `"`))
			if text == "" {
				return false
			}
			if strings.Contains(norm, text) || strings.Contains(unescaped, text) {
				return true
			}
			return formatted(text, setText, unescaped)
		}
		m := plpgsqlFrame.FindStringSubmatch(line)
		if m == nil || !h21Statements[m[3]] {
			return false
		}
		fn, _, _ := strings.Cut(m[1], "(")
		bodies := setBodies(setText, stmtText, fn)
		if m[3] != "EXECUTE" {
			return len(bodies) > 0
		}
		n := 0
		fmt.Sscan(m[2], &n)
		for _, b := range bodies {
			lines := strings.Split(b, "\n")
			if n >= 1 && n <= len(lines) && h21ConstantExecute.MatchString(lines[n-1]) {
				return true
			}
		}
		return false
	}
	return false
}

// formatted reports whether text is a format() of a template the set
// writes, every substituted value written by the set too (a constant
// argument of the call).
func formatted(text, setText, unescaped string) bool {
	for _, m := range formatTemplate.FindAllStringSubmatch(setText, -1) {
		tmpl := normalizeSQL(strings.ReplaceAll(m[1], "''", "'"))
		parts := formatSpec.Split(tmpl, -1)
		if len(parts) < 2 {
			continue
		}
		var re strings.Builder
		re.WriteString("^")
		for i, p := range parts {
			if i > 0 {
				re.WriteString(`(.+?)`)
			}
			re.WriteString(regexp.QuoteMeta(p))
		}
		re.WriteString("$")
		sub := regexp.MustCompile(re.String()).FindStringSubmatch(text)
		if sub == nil {
			continue
		}
		ok := true
		for _, v := range sub[1:] {
			v = strings.Trim(v, `"'`)
			if v == "" || !strings.Contains(unescaped, v) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// setBodies are the bodies the set wrote for a PL/pgSQL frame's function:
// the DO bodies of the attributed statement and of the set for
// inline_code_block, the bodies of CREATE FUNCTION or PROCEDURE of that
// name (folded, quoted or qualified) otherwise.
func setBodies(setText, stmtText, fn string) []string {
	var out []string
	name := fn
	if i := strings.LastIndex(fn, "."); i >= 0 {
		name = fn[i+1:]
	}
	name = strings.Trim(name, `"`)
	for _, text := range []string{stmtText, setText} {
		statements, _ := lexStatements([]byte(text))
		for _, st := range statements {
			toks := st.tokens
			if len(toks) == 0 {
				continue
			}
			var b *routineBody
			if fn == "inline_code_block" {
				if toks[0].kind == tokIdent && toks[0].value == "do" {
					b = doBody(toks, []byte(text))
				}
			} else if n, rb := routineBodyOf(toks, []byte(text)); rb != nil && (n == name || strings.EqualFold(n, name)) {
				b = rb
			}
			if b != nil && b.text != "" {
				out = append(out, b.text)
			}
		}
	}
	return out
}

// setText is the text of the set's migrations (not the prelude: E1's
// objects are not the set's), for H21.
func setText(h *oracleHistory) string {
	var b strings.Builder
	for _, f := range h.all() {
		b.WriteString(f.SQL)
		b.WriteString("\n")
	}
	return b.String()
}

// claimedHits lists the assumption hits on migrations the pass claims
// applied: true (H22): `claimed-hit <class> <sqlstate> <case> <statement>`.
// Each is reviewed against its assumption's text; one the assumption does
// not grant is a root.
func claimedHits(j *judged) []string {
	var out []string
	for _, o := range j.v.Outcomes {
		if o.Status != "refused" || o.Assumption == "" {
			continue
		}
		m := j.x.migrations[o.File]
		if m == nil || m.applied == nil || !*m.applied {
			continue
		}
		out = append(out, fmt.Sprintf("claimed-hit %s %s %s %s", o.Assumption, o.SQLState, j.h.ID, shortText(o.StmtText)))
	}
	return out
}

// logClaimedHits writes the H22 lines of a judged history.
func logClaimedHits(t *testing.T, j *judged) {
	t.Helper()
	for _, l := range claimedHits(j) {
		t.Log(l)
	}
}

// oracleMigrator is the role the replay runs as (H23, E6).
const oracleMigrator = "gv_migrator"

// migratorMembership are the roles E6 makes the migrating role a member
// of (E1's server files).
var migratorMembership = []string{"pg_read_server_files", "pg_write_server_files", "pg_execute_server_program"}

// migratorPassword derives the migrating role's password from the
// cluster's system identifier: every test process on a cluster computes
// the same one, and no password is stored. It only lets the replay log in
// as that role on a test server.
func migratorPassword(ctx context.Context, admin *sql.DB) (string, error) {
	var id string
	if err := admin.QueryRowContext(ctx, "SELECT system_identifier::text FROM pg_catalog.pg_control_system()").Scan(&id); err != nil {
		return "", fmt.Errorf("reading the system identifier: %w", err)
	}
	sum := sha256.Sum256([]byte("graphvein-oracle-" + oracleMigrator + "-" + id))
	return hex.EncodeToString(sum[:12]), nil
}

// resetMigrator (re)creates the migrating role of E6 as H23 states it:
// LOGIN, CREATEROLE, CREATEDB, REPLICATION, BYPASSRLS, not SUPERUSER, a
// member of E1's server-file roles and of nothing else (a history's
// GRANT ... TO it is dropped), createrole_self_grant 'set, inherit' and no
// other role setting. It runs before every history, so what one history
// did to the role never reaches the next (H18).
func resetMigrator(ctx context.Context, admin sqlRunner, password string) error {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	stmts := []string{
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = '` + oracleMigrator + `') THEN CREATE ROLE ` + oracleMigrator + `; END IF; END $$`,
		"ALTER ROLE " + oracleMigrator + " LOGIN NOSUPERUSER CREATEROLE CREATEDB REPLICATION BYPASSRLS INHERIT CONNECTION LIMIT -1 VALID UNTIL 'infinity' PASSWORD " + q(password),
		"ALTER ROLE " + oracleMigrator + " RESET ALL",
		"ALTER ROLE " + oracleMigrator + " SET createrole_self_grant = 'set, inherit'",
	}
	for _, s := range stmts {
		if _, err := admin.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("setting up %s: %w", oracleMigrator, err)
		}
	}
	rows, err := admin.QueryContext(ctx, `SELECT r.rolname FROM pg_catalog.pg_auth_members m JOIN pg_catalog.pg_roles r ON r.oid = m.roleid WHERE m.member = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1)`, oracleMigrator)
	if err != nil {
		return fmt.Errorf("reading %s's memberships: %w", oracleMigrator, err)
	}
	var held []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		held = append(held, n)
	}
	rows.Close()
	want := map[string]bool{}
	for _, r := range migratorMembership {
		want[r] = true
	}
	for _, n := range held {
		if !want[n] && n != "gv_oracle_role" {
			if _, err := admin.ExecContext(ctx, "REVOKE "+quoteIdent(n)+" FROM "+oracleMigrator+" CASCADE"); err != nil {
				return fmt.Errorf("revoking %s from %s: %w", n, oracleMigrator, err)
			}
		}
	}
	for _, r := range migratorMembership {
		if _, err := admin.ExecContext(ctx, "GRANT "+r+" TO "+oracleMigrator); err != nil {
			return fmt.Errorf("granting %s to %s: %w", r, oracleMigrator, err)
		}
	}
	return nil
}

// preludeAsSuperuser reports whether a prelude statement runs as the
// superuser (H23): CREATE EXTENSION of E1's extensions. Every other
// prelude statement runs as the migrating role, so the set may alter what
// it creates (E6) and the roles it creates are ordinary, with ADMIN OPTION
// for the migrating role.
func preludeAsSuperuser(stmt string) bool {
	return strings.HasPrefix(normalizeSQL(stripComments(stmt)), "create extension")
}

// superuserRefusal reports whether a 42501 refusal is one of the
// superuser-only checks of Y51 (superuserForms) or the system catalog
// check no role passes (Y07): such a refusal is judged; any other 42501 is
// an E6 assumption hit (H23).
func superuserRefusal(message, detail string) bool {
	for _, f := range superuserForms {
		if f.matches(message, detail) {
			return true
		}
	}
	return detail == "System catalog modifications are currently disallowed."
}

// migratorURL is the URL of a database on the test server as the
// migrating role (H23).
func (o *oracleDB) migratorURL(database string) (string, error) {
	raw, err := withDatabase(o.baseURL, database)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid GRAPHVEIN_TEST_POSTGRES_URL")
	}
	u.User = url.UserPassword(oracleMigrator, o.password)
	return u.String(), nil
}

// preludeRole is a role a prelude statement creates.
var preludeRole = regexp.MustCompile(`(?i)\bcreate\s+(?:role|user|group)\s+("(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)`)

// outsideRoles are the outside roles of E1 a history may give objects to:
// the oracle's role and the roles its prelude creates (H23).
func outsideRoles(prelude []string) []string {
	out := []string{"gv_oracle_role"}
	for _, p := range prelude {
		for _, m := range preludeRole.FindAllStringSubmatch(p, -1) {
			n := m[1]
			if strings.HasPrefix(n, `"`) {
				n = strings.ReplaceAll(n[1:len(n)-1], `""`, `"`)
			} else {
				n = strings.ToLower(n)
			}
			out = append(out, n)
		}
	}
	return out
}

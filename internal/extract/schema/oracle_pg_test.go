package schema

// Oracle harness, Postgres side (T018a2 A2): runs a history on a real
// PostgreSQL 17 with sqlx 0.8.6's execution semantics, reads the catalog
// and classifies refusals that break an environment assumption (E1-E4).
// It connects only through egress.OpenPostgres and runs only where
// GRAPHVEIN_TEST_POSTGRES_URL is set; it never starts a database.

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
)

// oracleDB is the oracle's own database on the test server, created from
// template0 so extensions and settings of other tests never leak in.
type oracleDB struct {
	baseURL string
	admin   *sql.DB // connected to the server's default database
	name    string  // the oracle database
	db      *sql.DB // connected to the oracle database
	// password is the migrating role's (H23, Revision 1.9).
	password string
	// notes are harness diagnostics of role cleanup (H25, Revision 2.1),
	// which never abort a run; the tests log them (takeNotes).
	notes []string
}

func withDatabase(raw, name string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid GRAPHVEIN_TEST_POSTGRES_URL")
	}
	u.Path = "/" + name
	return u.String(), nil
}

func randomSuffix() string { return strings.ToLower(rand.Text())[:12] }

func openOracle(ctx context.Context, raw string) (*oracleDB, error) {
	admin, err := egress.OpenPostgres(raw, nil)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := admin.PingContext(pingCtx); err != nil {
		admin.Close()
		return nil, fmt.Errorf("connecting to the test server: %w", err)
	}
	o := &oracleDB{baseURL: raw, admin: admin, name: "graphvein_oracle_" + randomSuffix()}
	// H23 (Revision 1.9): the histories run as the migrating role of E6,
	// which owns the oracle database (and so its public schema) and every
	// fresh history database.
	if o.password, err = migratorPassword(ctx, admin); err != nil {
		admin.Close()
		return nil, err
	}
	// H28 (Revision 2.1): the role setup is cluster-wide, so concurrent
	// test processes on one cluster take the setup lock around it.
	if err := withSetupLock(ctx, admin, func(db sqlRunner) error {
		if err := resetMigrator(ctx, db, o.password); err != nil {
			return err
		}
		// Roles are cluster-wide; the generator's role is an E1 object.
		if _, err := db.ExecContext(ctx, `DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gv_oracle_role') THEN CREATE ROLE gv_oracle_role; END IF; END $$`); err != nil {
			return fmt.Errorf("creating the oracle role: %w", err)
		}
		if _, err := db.ExecContext(ctx, "GRANT gv_oracle_role TO "+oracleMigrator+" WITH ADMIN OPTION"); err != nil {
			return fmt.Errorf("granting the oracle role: %w", err)
		}
		return nil
	}); err != nil {
		admin.Close()
		return nil, err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+o.name+" OWNER "+oracleMigrator+" TEMPLATE template0"); err != nil {
		admin.Close()
		return nil, fmt.Errorf("creating the oracle database: %w", err)
	}
	dbURL, err := withDatabase(raw, o.name)
	if err != nil {
		return nil, err
	}
	if o.db, err = egress.OpenPostgres(dbURL, nil); err != nil {
		return nil, err
	}
	return o, nil
}

func (o *oracleDB) close(ctx context.Context) error {
	o.db.Close()
	_, err := o.admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+o.name+" WITH (FORCE)")
	o.admin.Close()
	return err
}

// pgError is the part of a server error the harness reads. The driver's
// error type is not imported (only internal/egress may use pgx), so the
// fields are read by name.
type pgError struct {
	code, message, where, detail string
	position                     int
}

func readPgError(err error) (pgError, bool) {
	var coded interface{ SQLState() string }
	if !errors.As(err, &coded) {
		return pgError{}, false
	}
	v := reflect.ValueOf(coded)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	field := func(name string) string {
		if f := v.FieldByName(name); f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
		return ""
	}
	pos := 0
	if f := v.FieldByName("Position"); f.IsValid() && f.CanInt() {
		pos = int(f.Int())
	}
	return pgError{code: coded.SQLState(), message: field("Message"), where: field("Where"), detail: field("Detail"), position: pos}, true
}

// run applies the history and returns the oracle's verdict. H18
// (Revision 1.7): roles are cluster-wide, so every role the history (its
// prelude included) created is dropped after its database or schema is,
// with what it owns and the privileges granted to it; a role that cannot
// be dropped is an error of the history, never ignored.
func (o *oracleDB) run(ctx context.Context, h *oracleHistory) (*verdict, error) {
	if err := withSetupLock(ctx, o.admin, func(db sqlRunner) error { return resetMigrator(ctx, db, o.password) }); err != nil {
		return nil, err
	}
	before, err := o.roleNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the roles before %s: %w", h.ID, err)
	}
	v, err := o.runHistory(ctx, h)
	if derr := o.dropNewRoles(ctx, before, historyText(h)); derr != nil && err == nil {
		return nil, fmt.Errorf("dropping the roles %s created: %w", h.ID, derr)
	}
	return v, err
}

// historyText is the prelude and every migration of the history.
func historyText(h *oracleHistory) string {
	var b strings.Builder
	for _, p := range h.Prelude {
		b.WriteString(p)
		b.WriteString("\n")
	}
	for _, f := range h.all() {
		b.WriteString(f.SQL)
		b.WriteString("\n")
	}
	return b.String()
}

// roleNames lists the cluster's roles.
func (o *oracleDB) roleNames(ctx context.Context) (map[string]bool, error) {
	rows, err := o.admin.QueryContext(ctx, "SELECT rolname FROM pg_catalog.pg_roles")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// dropNewRoles drops every role not in before whose name the history's
// text contains (folded to lower case; a role another process created on
// the same cluster meanwhile is left alone). H25 (Revision 2.1, S6-W10):
// in the shared oracle database (a fresh history database is already
// dropped), REASSIGN OWNED BY those roles TO the admin, then DROP OWNED BY
// them (objects, privileges and default privileges; shared objects too),
// then DROP ROLE. A failing step is a harness note (o.notes), never an
// error that aborts the run.
func (o *oracleDB) dropNewRoles(ctx context.Context, before map[string]bool, text string) error {
	now, err := o.roleNames(ctx)
	if err != nil {
		return err
	}
	lower := strings.ToLower(text)
	var names []string
	for n := range now {
		if !before[n] && (strings.Contains(text, n) || strings.Contains(lower, n)) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	var quoted []string
	for _, n := range names {
		quoted = append(quoted, `"`+strings.ReplaceAll(n, `"`, `""`)+`"`)
	}
	list := strings.Join(quoted, ", ")
	for _, stmt := range []string{"REASSIGN OWNED BY " + list + " TO CURRENT_USER", "DROP OWNED BY " + list} {
		if _, err := o.db.ExecContext(ctx, stmt); err != nil {
			o.note(fmt.Sprintf("role cleanup (H25): %s: %v", stmt, err))
		}
	}
	for _, q := range quoted {
		if _, err := o.admin.ExecContext(ctx, "DROP ROLE "+q); err != nil {
			o.note(fmt.Sprintf("role cleanup (H25): DROP ROLE %s: %v", q, err))
		}
	}
	// REASSIGN OWNED also moves the oracle database when a history gave
	// it to a role it created; E6's migrating role owns it again.
	if _, err := o.admin.ExecContext(ctx, "ALTER DATABASE "+o.name+" OWNER TO "+oracleMigrator); err != nil {
		o.note(fmt.Sprintf("role cleanup (H25): restoring the oracle database's owner: %v", err))
	}
	return nil
}

// catalogHook, when set, reads more of the catalog into the verdict after
// the history ran (the Supabase oracle's row level security).
var catalogHook func(ctx context.Context, conn *sql.Conn, v *verdict) error

// runHistory applies the history and returns the oracle's verdict.
func (o *oracleDB) runHistory(ctx context.Context, h *oracleHistory) (*verdict, error) {
	// db is the migrating role's connection (H23); su the superuser's, for
	// the prelude's CREATE EXTENSION.
	var db, su *sql.DB
	schema := "public"
	var cleanup func()
	if h.FreshDB {
		name := "gv_h_" + randomSuffix()
		if _, err := o.admin.ExecContext(ctx, "CREATE DATABASE "+name+" OWNER "+oracleMigrator+" TEMPLATE template0"); err != nil {
			return nil, fmt.Errorf("creating database for %s: %w", h.ID, err)
		}
		dbURL, err := o.migratorURL(name)
		if err != nil {
			return nil, err
		}
		if db, err = egress.OpenPostgres(dbURL, nil); err != nil {
			return nil, err
		}
		suURL, err := withDatabase(o.baseURL, name)
		if err != nil {
			return nil, err
		}
		if su, err = egress.OpenPostgres(suURL, nil); err != nil {
			return nil, err
		}
		cleanup = func() {
			db.Close()
			su.Close()
			o.admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		}
	} else {
		schema = "h_" + randomSuffix()
		if _, err := o.db.ExecContext(ctx, "CREATE SCHEMA "+schema+" AUTHORIZATION "+oracleMigrator); err != nil {
			return nil, fmt.Errorf("creating schema for %s: %w", h.ID, err)
		}
		dbURL, err := o.migratorURL(o.name)
		if err != nil {
			return nil, err
		}
		if db, err = egress.OpenPostgres(dbURL, map[string]string{"search_path": schema}); err != nil {
			return nil, err
		}
		suURL, err := withDatabase(o.baseURL, o.name)
		if err != nil {
			return nil, err
		}
		if su, err = egress.OpenPostgres(suURL, map[string]string{"search_path": schema}); err != nil {
			return nil, err
		}
		cleanup = func() {
			db.Close()
			su.Close()
			o.db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
			// H29 (Revision 2.1): a history whose search_path names no
			// schema it can create in (set_config, SET) creates in the
			// oracle database's public schema, outside its own schema; the
			// object would reach the next history of the process (the H27
			// prefix replay of g7-s1695 was refused with 42P07). Everything
			// the migrating role still owns in the oracle database is the
			// history's.
			if _, err := o.db.ExecContext(ctx, "DROP OWNED BY "+oracleMigrator); err != nil {
				o.note(fmt.Sprintf("cleanup (H29): DROP OWNED BY %s: %v", oracleMigrator, err))
			}
		}
	}
	defer cleanup()
	// E4: every history starts with the default settings. A history may set
	// role-level defaults (ALTER ROLE CURRENT_USER SET ...), which are
	// cluster-wide and would reach later histories' sessions (Revision 1.6
	// D2: the fixture pg-rev2-roles sets search_path for the role).
	// H23: the histories run as the migrating role, whose settings
	// resetMigrator restores before each history.
	defer o.admin.ExecContext(ctx, "ALTER ROLE CURRENT_USER RESET ALL")
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("connecting for %s: %w", h.ID, err)
	}
	defer conn.Close()
	v := &verdict{Schema: schema}
	for _, p := range h.Prelude {
		// H23: CREATE EXTENSION as the superuser, the rest as the
		// migrating role (E6).
		if preludeAsSuperuser(p) {
			if _, err := su.ExecContext(ctx, p); err != nil {
				return nil, fmt.Errorf("prelude of %s (%s): %w", h.ID, p, err)
			}
			continue
		}
		if _, err := conn.ExecContext(ctx, p); err != nil {
			return nil, fmt.Errorf("prelude of %s (%s): %w", h.ID, p, err)
		}
	}
	// H23: the outside roles of E1 own what the set gives them (E1: they
	// fit their use), so they may create in the history's schema.
	for _, r := range outsideRoles(h.Prelude) {
		if _, err := conn.ExecContext(ctx, "GRANT CREATE, USAGE ON SCHEMA "+schema+" TO "+quoteIdent(r)); err != nil {
			return nil, fmt.Errorf("granting the history schema to %s: %w", r, err)
		}
		if h.FreshDB {
			// And the schemas the set creates in its own database.
			if _, err := conn.ExecContext(ctx, "ALTER DEFAULT PRIVILEGES GRANT CREATE, USAGE ON SCHEMAS TO "+quoteIdent(r)); err != nil {
				return nil, fmt.Errorf("granting the set's schemas to %s: %w", r, err)
			}
		}
	}
	before, err := readCatalog(ctx, conn, schema, h.FreshDB)
	if err != nil {
		return nil, err
	}
	// outside are the prelude's tables (E1) by name: column -> type.
	outside := map[string]map[string]string{}
	for _, t := range before.Tables {
		v.Prelude = append(v.Prelude, t.Key)
		_, name, _ := strings.Cut(t.Key, ".")
		outside[name] = map[string]string{}
		for _, c := range t.Columns {
			outside[name][c.Name] = c.Type
		}
	}
	// sqlx-postgres migrate.rs 114-131 (ensure_migrations_table).
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _sqlx_migrations (
    version BIGINT PRIMARY KEY,
    description TEXT NOT NULL,
    installed_on TIMESTAMPTZ NOT NULL DEFAULT now(),
    success BOOLEAN NOT NULL,
    checksum BYTEA NOT NULL,
    execution_time BIGINT NOT NULL
)`); err != nil {
		return nil, fmt.Errorf("creating _sqlx_migrations: %w", err)
	}
	v.Prelude = append(v.Prelude, schema+"._sqlx_migrations")
	all := h.all()
	order, ok := sqlxOrder(all, sqlxReadsNoTx(h.cargo()))
	if !ok {
		for _, f := range all {
			if _, _, found := strings.Cut(f.Name, "_"); found && strings.HasSuffix(f.Name, ".sql") {
				v.Outcomes = append(v.Outcomes, outcome{File: f.Name, Status: "set_refused"})
			}
		}
	}
	created := createdNames(h)
	unvalidated := unvalidatedRoutines(h)
	text := setText(h)
	stopped := false
	// H9 (Revision 1.4 R5): text seen so far, for ownRows.
	var seen strings.Builder
	for _, m := range order {
		seen.WriteString(m.file.SQL)
		seen.WriteString("\n")
		oc := outcome{File: m.file.Name, NoTx: m.noTx}
		switch {
		case m.down:
			oc.Status = "down"
		case stopped:
			oc.Status = "not_run"
		default:
			oc.Status = "accepted"
			failed, bookkeeping, post := applyMigration(ctx, conn, m)
			if post != nil {
				// H24 (Revision 1.9): sqlx's post-commit UPDATE failed. The
				// migration committed and its row is recorded; sqlx stops
				// the set there.
				stopped = true
				oc.PostCommit = post.Error()
				if pe, ok := readPgError(post); ok {
					oc.PostCommit = pe.code + " " + pe.message
				}
			}
			if failed != nil {
				stopped = true
				oc.Status = "refused"
				pe, ok := readPgError(failed)
				if ok {
					oc.SQLState, oc.Message, oc.Where, oc.Detail = pe.code, pe.message, pe.where, pe.detail
				} else {
					oc.Message = failed.Error()
				}
				if !bookkeeping {
					oc.Statement, oc.StmtText, oc.RowFree = attribute(ctx, conn, m, pe, rowScope(schema, h.FreshDB))
					catalogFunction := func(name string) bool {
						var found bool
						err := conn.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_proc WHERE proname = $1 AND pronamespace = 'pg_catalog'::pg_catalog.regnamespace)", name).Scan(&found)
						return err != nil || found
					}
					// H16 (Revision 1.6): rows are the set's own unless a
					// checked table may hold rows of a COPY or the prelude.
					tables, read := checkedTables(oc.StmtText)
					own := ownRows(seen.String(), h.Prelude)
					if read {
						own = !foreignRowsFor(seen.String(), h.Prelude, tables)
					}
					oc.Assumption = assumption(oc, created, outside, catalogFunction, own, unvalidated, text)
				} else {
					oc.Message = "sqlx bookkeeping: " + oc.Message
				}
			}
		}
		v.Outcomes = append(v.Outcomes, oc)
	}
	// A migration may leave a transaction block open (BEGIN in a
	// no-transaction migration, or after its own COMMIT); sqlx then
	// releases the connection and the block is rolled back.
	conn.ExecContext(ctx, "ROLLBACK")
	after, err := readCatalog(ctx, conn, schema, h.FreshDB)
	if err != nil {
		return nil, err
	}
	if catalogHook != nil {
		if err := catalogHook(ctx, conn, v); err != nil {
			return nil, err
		}
	}
	pre := map[string]bool{}
	for _, p := range v.Prelude {
		pre[p] = true
	}
	for _, t := range after.Tables {
		if !pre[t.Key] {
			v.Tables = append(v.Tables, t)
		}
	}
	v.Enums = after.Enums
	if !h.FreshDB {
		v.rename(schema, "history")
	}
	return v, nil
}

// rename replaces the random per-history schema with a fixed name, so a
// recorded verdict can be re-checked on another run.
func (v *verdict) rename(from, to string) {
	re := func(s string) string {
		if rest, ok := strings.CutPrefix(s, from+"."); ok {
			return to + "." + rest
		}
		return s
	}
	v.Schema = to
	for i := range v.Tables {
		v.Tables[i].Key = re(v.Tables[i].Key)
		for k := range v.Tables[i].FKs {
			v.Tables[i].FKs[k].RefTable = re(v.Tables[i].FKs[k].RefTable)
		}
	}
	for i := range v.Enums {
		v.Enums[i].Key = re(v.Enums[i].Key)
	}
	for i := range v.Prelude {
		v.Prelude[i] = re(v.Prelude[i])
	}
	for i := range v.Outcomes {
		v.Outcomes[i].Message = strings.ReplaceAll(v.Outcomes[i].Message, from, to)
		v.Outcomes[i].Where = strings.ReplaceAll(v.Outcomes[i].Where, from, to)
	}
}

// applyMigration runs one migration as sqlx-postgres 0.8.6 does
// (migrate.rs 206-225 apply, 275-300 execute_migration): BEGIN, the whole
// file as one simple query, the _sqlx_migrations row, COMMIT; without the
// transaction for a no-transaction migration. bookkeeping reports a failure
// of the _sqlx_migrations insert (a duplicate version). post is a failure
// of sqlx's UPDATE of execution_time, which runs after the migration
// committed, outside its transaction (H24, Revision 1.9: migrate.rs apply
// at 0.8.6, `?` on its result stops the set).
func applyMigration(ctx context.Context, conn *sql.Conn, m sqlxMigration) (failed error, bookkeeping bool, post error) {
	sum := sha512.Sum384([]byte(m.file.SQL))
	_, desc, _ := strings.Cut(m.file.Name, "_")
	for _, suffix := range []string{".up.sql", ".down.sql", ".sql"} {
		if strings.HasSuffix(desc, suffix) {
			for strings.HasSuffix(desc, suffix) {
				desc = strings.TrimSuffix(desc, suffix)
			}
			break
		}
	}
	desc = strings.ReplaceAll(desc, "_", " ")
	insert := func() error {
		_, err := conn.ExecContext(ctx, "INSERT INTO _sqlx_migrations ( version, description, success, checksum, execution_time ) VALUES ( $1, $2, TRUE, $3, -1 )", m.version, desc, sum[:])
		return err
	}
	update := func() error {
		_, err := conn.ExecContext(ctx, "UPDATE _sqlx_migrations SET execution_time = $1 WHERE version = $2", int64(1), m.version)
		return err
	}
	if m.noTx {
		if _, err := conn.ExecContext(ctx, m.file.SQL); err != nil {
			return err, false, nil
		}
		if err := insert(); err != nil {
			return err, true, nil
		}
		return nil, false, update()
	}
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return err, true, nil
	}
	if _, err := conn.ExecContext(ctx, m.file.SQL); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return err, false, nil
	}
	if err := insert(); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return err, true, nil
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return err, false, nil
	}
	return nil, false, update()
}

// attribute finds the statement a refused migration failed on, by
// replaying its statements one by one in a transaction that is rolled
// back. It uses the pass's scan.l lexer only to split the file; the
// verdict itself is the whole migration's.
// rowScope is the filter of the tables a history's statements can reach
// (pg_class c, pg_namespace n): its schema, or every non-system schema of
// its fresh database, and the session's temporary tables.
func rowScope(schema string, fresh bool) string {
	if fresh {
		return "(n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\\_toast%' AND (n.nspname NOT LIKE 'pg\\_temp\\_%' OR c.relnamespace = pg_my_temp_schema()))"
	}
	return fmt.Sprintf("(n.nspname = '%s' OR c.relnamespace = pg_my_temp_schema())", schema)
}

// refusedWithoutRows re-runs a refused statement after emptying every
// table in scope (H7): true when Postgres refuses it again with the same
// SQLSTATE, so the refusal does not depend on rows. The caller holds the
// savepoint gv_rowfree taken just before the statement. A scope with a
// materialized view, or a TRUNCATE that fails, answers false (rows may
// matter).
func refusedWithoutRows(ctx context.Context, conn *sql.Conn, text, code, scope string) bool {
	if _, err := conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT gv_rowfree"); err != nil {
		return false
	}
	rows, err := conn.QueryContext(ctx, "SELECT c.relkind, pg_catalog.format('%I.%I', n.nspname, c.relname) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r', 'p', 'm') AND c.relname <> '_sqlx_migrations' AND "+scope)
	if err != nil {
		return false
	}
	var tables []string
	matview := false
	for rows.Next() {
		var kind, name string
		if rows.Scan(&kind, &name) != nil {
			rows.Close()
			return false
		}
		if kind == "m" {
			matview = true
		}
		tables = append(tables, name)
	}
	rows.Close()
	if matview || rows.Err() != nil {
		return false
	}
	if len(tables) > 0 {
		if _, err := conn.ExecContext(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" CASCADE"); err != nil {
			return false
		}
	}
	_, err = conn.ExecContext(ctx, text)
	pe, ok := readPgError(err)
	return ok && pe.code == code
}

func attribute(ctx context.Context, conn *sql.Conn, m sqlxMigration, real pgError, scope string) (int, string, bool) {
	statements, _ := lexStatements([]byte(m.file.SQL))
	if real.position > 0 {
		// A parse error carries its 1-based character position in the
		// query string: Postgres parsed the whole string first.
		runes := []rune(m.file.SQL)
		if real.position <= len(runes) {
			offset := len(string(runes[:real.position-1]))
			for i, st := range statements {
				if offset < st.end {
					return i + 1, m.file.SQL[st.start:st.end], false
				}
			}
		}
	}
	if m.noTx && len(statements) == 1 {
		text := m.file.SQL[statements[0].start:statements[0].end]
		rowFree := false
		if strings.HasPrefix(real.code, "22") || strings.HasPrefix(real.code, "23") {
			if _, err := conn.ExecContext(ctx, "BEGIN"); err == nil {
				if _, err := conn.ExecContext(ctx, "SAVEPOINT gv_rowfree"); err == nil {
					rowFree = refusedWithoutRows(ctx, conn, text, real.code, scope)
				}
				conn.ExecContext(ctx, "ROLLBACK")
			}
		}
		return 1, text, rowFree
	}
	// A multi-statement no-transaction migration runs in an implicit
	// transaction block (postgres.c 1095, 1156-1157), so a replay in a
	// transaction block refuses the same statement.
	//
	// H1 (Revision 1): transaction control in the string is emulated
	// instead of run, so that the replay stays in its own block and is
	// rolled back. The real string's block state is tracked: "explicit"
	// (sqlx's transaction, or a BEGIN the string wrote) or "implicit" (after
	// COMMIT/END/ROLLBACK in the string, or a multi-statement
	// no-transaction migration). COMMIT and END become a savepoint that a
	// later ROLLBACK returns to; ROLLBACK and ABORT return to the last such
	// savepoint (the start of the replay when none); SAVEPOINT, RELEASE and
	// ROLLBACK TO outside an explicit block are refused with 25P01 (xact.c
	// DefineSavepoint, ReleaseSavepoint, RollbackToSavepoint), as the real
	// string is. LOCK is not: RequireTransactionBlock accepts the implicit
	// block of a multi-statement string (IsTransactionBlock; harness defect
	// H8, g1-s45067, found in B3's 100,000-seed run). The real run
	// keeps what a COMMIT or END committed before the refusal, so a segment
	// up to a COMMIT whose replay is refused with another SQLSTATE was
	// committed by the real run: the replay returns to the segment's start
	// and continues after that COMMIT.
	//
	// H10 (Revision 1.4, g1-s69180 and g3-s43863 of the 100,000-seed
	// run): a statement run inside a subtransaction, even a released one,
	// loses the exemption of an enum type created in the same transaction,
	// so a label its ALTER TYPE ... ADD VALUE added is refused there (55P04
	// unsafe use of new value) where the real run accepts it. A migration
	// without transaction control is therefore replayed without any
	// savepoint: no opening savepoint, and for a class 22 or 23 refusal
	// the statements before the refused one are replayed again in a fresh
	// block before the row-free check, instead of a savepoint before every
	// statement.
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return 0, "", false
	}
	defer conn.ExecContext(ctx, "ROLLBACK")
	plain := !hasTransactionControl(statements)
	explicit := !m.noTx
	marks := 0
	commitAfter := func(i int) int {
		for j := i + 1; j < len(statements); j++ {
			if t := statements[j].tokens; len(t) > 0 && t[0].kind == tokIdent && (t[0].value == "commit" || t[0].value == "end") {
				return j
			}
		}
		return -1
	}
	if !plain {
		if _, err := conn.ExecContext(ctx, "SAVEPOINT gv_replay_0"); err != nil {
			return 0, "", false
		}
	}
	mark := func() error {
		marks++
		_, err := conn.ExecContext(ctx, fmt.Sprintf("SAVEPOINT gv_replay_%d", marks))
		return err
	}
	undo := func() error {
		_, err := conn.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT gv_replay_%d", marks))
		return err
	}
	skipTo := -1
	for i, st := range statements {
		if i < skipTo {
			continue
		}
		text := m.file.SQL[st.start:st.end]
		head, second := "", ""
		if len(st.tokens) > 0 && st.tokens[0].kind == tokIdent {
			head = st.tokens[0].value
		}
		if len(st.tokens) > 1 && st.tokens[1].kind == tokIdent {
			second = st.tokens[1].value
		}
		switch {
		case head == "begin" || head == "start" && second == "transaction":
			explicit = true
			continue
		case head == "commit" && second != "prepared" || head == "end":
			explicit = false
			if mark() != nil {
				return 0, "", false
			}
			continue
		case (head == "rollback" || head == "abort") && second != "to" && second != "prepared":
			explicit = false
			if undo() != nil {
				return 0, "", false
			}
			continue
		case !explicit && (head == "savepoint" || head == "release" || head == "rollback" && second == "to"):
			if real.code == "25P01" {
				return i + 1, text, false
			}
			return 0, "", false
		}
		rowCheck := strings.HasPrefix(real.code, "22") || strings.HasPrefix(real.code, "23")
		if rowCheck && !plain {
			if _, err := conn.ExecContext(ctx, "SAVEPOINT gv_rowfree"); err != nil {
				return 0, "", false
			}
		}
		if _, err := conn.ExecContext(ctx, text); err != nil {
			if pe, ok := readPgError(err); ok && pe.code != real.code {
				if j := commitAfter(i); j >= 0 {
					// The real run committed this segment; skip to its COMMIT.
					if _, err := conn.ExecContext(ctx, fmt.Sprintf("ROLLBACK TO SAVEPOINT gv_replay_%d", marks)); err != nil {
						return 0, "", false
					}
					skipTo = j
					continue
				}
				// Another refusal than the real one (the replay runs the
				// statements alone): not attributed.
				return 0, "", false
			}
			if rowCheck && plain {
				return i + 1, text, replayPrefix(ctx, conn, m.file.SQL, statements[:i]) && refusedWithoutRows(ctx, conn, text, real.code, scope)
			}
			return i + 1, text, rowCheck && refusedWithoutRows(ctx, conn, text, real.code, scope)
		}
	}
	return 0, "", false
}

// hasTransactionControl reports whether a migration's statements include
// transaction control, which the replay emulates with savepoints (H1).
func hasTransactionControl(statements []statement) bool {
	for _, st := range statements {
		if len(st.tokens) == 0 || st.tokens[0].kind != tokIdent {
			continue
		}
		switch st.tokens[0].value {
		case "begin", "start", "commit", "end", "rollback", "abort", "savepoint", "release", "prepare":
			return true
		}
	}
	return false
}

// replayPrefix starts the replay block again and runs the statements
// before a refused one without savepoints (H10), then sets the savepoint
// gv_rowfree that refusedWithoutRows returns to. It reports false when a
// statement of the prefix is refused.
func replayPrefix(ctx context.Context, conn *sql.Conn, src string, prefix []statement) bool {
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		return false
	}
	if _, err := conn.ExecContext(ctx, "BEGIN"); err != nil {
		return false
	}
	for _, st := range prefix {
		if _, err := conn.ExecContext(ctx, src[st.start:st.end]); err != nil {
			return false
		}
	}
	_, err := conn.ExecContext(ctx, "SAVEPOINT gv_rowfree")
	return err == nil
}

var (
	// H20 (Revision 1.8): every creating form of the set's text. CREATE
	// SCHEMA AUTHORIZATION r (the schema takes the role's name) comes
	// before the plain CREATE SCHEMA form (leftmost-first alternation);
	// SELECT INTO targets are read by selectIntoPattern.
	createdPattern = regexp.MustCompile(`(?is)\b(?:create\s+schema\s+(?:if\s+not\s+exists\s+)?authorization\s+|create\s+(?:or\s+replace\s+)?(?:(?:global|local|temp|temporary|unlogged|materialized|recursive|unique|constraint|event|trusted|procedural|foreign|default)\s+)*(?:operator\s+(?:class|family)|text\s+search\s+(?:configuration|dictionary|parser|template)|access\s+method|table|index|view|sequence|type|domain|function|procedure|schema|trigger|extension|role|user|group|aggregate|collation|policy|statistics|operator|language|rule|server|publication|subscription|conversion)\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?|rename\s+(?:(?:column|constraint|attribute)\s+)?(?:\S+\s+)?to\s+|add\s+(?:column\s+|attribute\s+)?(?:if\s+not\s+exists\s+)?|add\s+value\s+(?:if\s+not\s+exists\s+)?|constraint\s+)((?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)(?:\s*\.\s*(?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*))*)`)
	// selectIntoPattern finds INTO targets; oracleIntoCreates keeps those
	// that may create a table (H20).
	selectIntoPattern = regexp.MustCompile(`(?is)\binto\s+(?:(?:global|local|temp|temporary|unlogged|table)\s+)*((?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)(?:\s*\.\s*(?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*))*)`)
	quotedName        = regexp.MustCompile(`"((?:[^"]|"")+)"`)
	keyColumns        = regexp.MustCompile(`Key columns "([^"]+)" and "([^"]+)" are of incompatible types: (.+) and (.+)\.`)
	referencesName    = regexp.MustCompile(`references\s+((?:"[^"]+"|[a-z_][a-z0-9_$]*)(?:\.(?:"[^"]+"|[a-z_][a-z0-9_$]*))?)`)
	functionName      = regexp.MustCompile(`function ([A-Za-z_][A-Za-z0-9_$.]*)\(`)
	// creationPattern finds statements that create a relation, type,
	// schema or function, for K1 (a refusal naming an object created
	// later). H20 (Revision 1.8): CREATE SCHEMA AUTHORIZATION r and, in
	// firstCreation, SELECT INTO targets.
	creationPattern = regexp.MustCompile(`(?is)\b(?:create\s+schema\s+(?:if\s+not\s+exists\s+)?authorization\s+|create\s+(?:or\s+replace\s+)?(?:(?:global|local|temp|temporary|unlogged|materialized|recursive|unique)\s+)*(?:table|index|view|sequence|type|domain|function|procedure|schema)\s+(?:concurrently\s+)?(?:if\s+not\s+exists\s+)?|rename\s+to\s+)((?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*)(?:\s*\.\s*(?:"(?:[^"]|"")+"|[a-z_][a-z0-9_$]*))*)`)
)

// createdNames lists every name the set may create anywhere (relations,
// types, schemas, roles, functions, columns, constraints, labels): E1 does
// not cover them (a forward reference to such a name is K1, not E1).
// Over-listing is safe: it only classifies fewer refusals as E1.
// splitQualified splits a qualified name at the dots outside quotes.
func splitQualified(name string) []string {
	var parts []string
	start, quoted := 0, false
	for i := 0; i < len(name); i++ {
		switch {
		case name[i] == '"':
			quoted = !quoted
		case name[i] == '.' && !quoted:
			parts = append(parts, name[start:i])
			start = i + 1
		}
	}
	return append(parts, name[start:])
}

func createdNames(h *oracleHistory) map[string]bool {
	out := map[string]bool{}
	for _, f := range h.Files {
		matches := createdPattern.FindAllStringSubmatch(f.SQL, -1)
		for _, ix := range selectIntoPattern.FindAllStringSubmatchIndex(f.SQL, -1) {
			if oracleIntoCreates(f.SQL[:ix[0]]) {
				matches = append(matches, []string{f.SQL[ix[0]:ix[1]], f.SQL[ix[2]:ix[3]]})
			}
		}
		for _, m := range matches {
			// Only the last part of a qualified name is created; the
			// schema is created only by CREATE SCHEMA.
			// A quoted part may hold a dot (T024c1: Supabase policy
			// names such as "Can view own user data.").
			parts := splitQualified(m[1])
			for _, part := range parts[len(parts)-1:] {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, `"`) {
					out[strings.ReplaceAll(part[1:len(part)-1], `""`, `"`)] = true
				} else {
					out[strings.ToLower(part)] = true
				}
			}
		}
	}
	return out
}

// oracleIntoCreates reports whether an INTO whose preceding text is before
// may create a table: not INSERT INTO or MERGE INTO (H20). PL/pgSQL
// SELECT ... INTO variables are over-listed, which only judges more
// refusals.
func oracleIntoCreates(before string) bool {
	fields := strings.Fields(strings.ToLower(before))
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	return last != "insert" && last != "merge"
}

// assumption classifies a refusal (A0's table, recorded in the inventory):
//   - E1 (also): 42804 for a foreign key whose referenced key belongs to
//     a prelude table (its key type is whatever the set needs);
//   - E1: 42P01 undefined_table, 42704 undefined_object, 3F000
//     invalid_schema_name, 42883 undefined_function naming only objects
//     the set never creates; 58P01/0A000 from CREATE EXTENSION of an
//     extension that is not available; 58P01 from COPY of a server file;
//   - E2: class 23 (integrity constraint violation), and class 22 (data
//     exception) raised by a statement that reads existing rows (COPY of a
//     server file, ALTER TABLE, CREATE INDEX); since Revision 1.3 not by
//     DML or a query, whose own values and constraints are predictions;
//   - E3: P0001 raise_exception, or an error whose innermost context frame
//     is code running (H14, Revision 1.6: executionFrame; never the
//     compilation or validation of a body, never 2D000);
//   - E4: none expected (the session starts with the defaults);
//   - E6 (Revision 1.9 H23): 42501 outside Y51's superuser-only checks.
//
// H3 (Revision 1): a 42883 naming a function is E1 only when pg_proc has no
// pg_catalog function of that name; a pg_catalog function's signature is
// Postgres's own (P5), never an outside object (catalogFunction reports
// one, and an error reading pg_proc counts as one).
func assumption(oc outcome, created map[string]bool, outside map[string]map[string]string, catalogFunction func(string) bool, own bool, unvalidated map[string]bool, setText string) string {
	code := oc.SQLState
	// H14 (Revision 1.6): E3 covers the execution of code, never its
	// compilation or validation; 2D000 (transaction control in code run
	// inside a transaction block) is predicted from the SQL. H17
	// (Revision 1.7): the startup and inlining frames of a SQL function
	// that may have been created with check_function_bodies off are the
	// first parse of its body, not its execution.
	if f, ok := sqlStartupFrame(oc.Where); ok && unvalidated[f] {
		return ""
	}
	if code != "2D000" && (code == "P0001" || executionFrame(oc.Where)) {
		// H21 (Revision 1.9): E3 is code logic, or a statement the set's
		// text does not fix; every other refusal raised inside code is
		// judged like a top-level one (E1 below may still cover it).
		if h21CodeLogic(code) || !h21Fixed(oc.Where, oc.StmtText, setText) {
			return "E3"
		}
	}
	if code == "42501" {
		// H23 (Revision 1.9): a superuser-only check of Y51 (or the system
		// catalog check) is judged; any other permission refusal is a
		// privilege E6 grants.
		if superuserRefusal(oc.Message, oc.Detail) {
			return ""
		}
		return "E6"
	}
	stmt := strings.ToLower(strings.TrimSpace(stripComments(oc.StmtText)))
	switch {
	case code == "42P01" || code == "42704" || code == "3F000" || code == "42883" || code == "42703":
		if !e1Template(code, oc.Message) {
			// H12 (Revision 1.5): any other refusal with these codes is
			// judged, never E1.
			return ""
		}
		names := quotedName.FindAllStringSubmatch(oc.Message, -1)
		if code == "42703" {
			// column "c" of relation "t": E1 when t is outside the set.
			if len(names) < 2 {
				return ""
			}
			names = names[1:]
		}
		// function f(integer) does not exist prints the name unquoted.
		functions := functionName.FindAllStringSubmatch(oc.Message, -1)
		for _, f := range functions {
			parts := strings.Split(f[1], ".")
			if catalogFunction(parts[len(parts)-1]) {
				return ""
			}
		}
		names = append(names, functions...)
		if len(names) == 0 {
			return ""
		}
		if code == "42P01" || code == "42703" {
			// H15 (Revision 1.6): a relation template is E1 only when the
			// statement names the relation in a table position; E1 covers
			// tables, not indexes, sequences, views or materialized views,
			// nor names only inside string constants.
			parts := strings.Split(names[0][1], ".")
			if !tablePosition(oc.StmtText, parts[len(parts)-1]) {
				return ""
			}
		}
		for _, n := range names {
			for _, part := range strings.Split(n[1], ".") {
				// Function signatures print as name(args).
				part, _, _ = strings.Cut(part, "(")
				if setName(created, part) {
					return ""
				}
			}
		}
		return "E1"
	case (code == "58P01" || code == "0A000") && strings.HasPrefix(stmt, "create extension"):
		return "E1"
	case code == "58P01" && strings.HasPrefix(stmt, "copy"):
		// A server file COPY reads is an object outside the set (A0
		// reading of E1; listed for review in the inventory).
		return "E1"
	case code == "42804" && strings.Contains(oc.Message, "foreign key constraint"):
		// Key types of a referenced table outside the set are whatever the
		// set needs (E1): the refused pair is an outside table's key when
		// the statement references a prelude table whose key column has
		// the referenced type the detail prints.
		m := keyColumns.FindStringSubmatch(oc.Detail)
		if m == nil {
			return ""
		}
		for _, r := range referencesName.FindAllStringSubmatch(stmt, -1) {
			parts := strings.Split(r[1], ".")
			name := strings.Trim(parts[len(parts)-1], `"`)
			if setName(created, name) {
				continue
			}
			if outside[name][m[2]] == m[4] {
				return "E1"
			}
		}
		return ""
	case strings.HasPrefix(code, "23") || strings.HasPrefix(code, "22"):
		// Revision 1.3: a data-changing statement's own values, and the
		// constraints its rows meet, are predictions (P5 on N3), not E2.
		// E2 keeps the rows of a server file COPY reads and existing rows
		// that a later ALTER TABLE or CREATE INDEX checks or casts.
		for _, p := range []string{"insert", "update", "delete", "merge", "with", "select", "values"} {
			if strings.HasPrefix(stmt, p) {
				return ""
			}
		}
		//
		// H7 (B2): a refusal of ALTER TABLE or CREATE INDEX is E2 only when
		// it depends on rows: the replay refuses the statement again with
		// every reachable table emptied (oc.RowFree) means it does not
		// (for example SET DEFAULT 'abc' on an int column, 22P02 at
		// analysis), and the refusal is a prediction like any other.
		if strings.HasPrefix(stmt, "copy") {
			return "E2"
		}
		if oc.RowFree {
			return ""
		}
		// H9 (Revision 1.4 R5): when every row the history holds was
		// written by its own data statements (ownRows: no COPY, no code
		// that may write rows), a refusal those rows cause is a
		// prediction, not E2.
		if own {
			return ""
		}
		if strings.HasPrefix(code, "23") {
			return "E2"
		}
		rowCodes := map[string]bool{"22P02": true, "22003": true, "22001": true, "22007": true, "22008": true, "22012": true, "22004": true}
		if rowCodes[code] && (strings.HasPrefix(stmt, "alter table") || strings.Contains(stmt, "index")) {
			return "E2"
		}
	}
	return ""
}

// e1Templates are the messages a refusal with one of the E1 codes must
// match to be an E1 assumption hit (H12, T018a2 Revision 1.5): an object
// E1 lets the set use without creating it (a relation, schema, role or
// function outside the set, a column of an outside table, an operator), and
// a type or operator class only when it is in the E1 tables generated from
// contrib (e1Types, e1Opclasses). Anything else with these codes (a type
// with no default operator class, an unknown setting, a collation, an
// index, ...) is judged.
var e1Templates = []struct {
	code string
	re   *regexp.Regexp
	// table, when set, must hold the first quoted name (unqualified).
	table func(string) bool
}{
	{"42P01", regexp.MustCompile(`^relation "[^"]+" does not exist$`), nil},
	// DROP TABLE words the same undefined_table refusal for a table
	// (tablecmds.c DropErrorMsgNonExistent); E1 covers tables outside the
	// set, so it is the relation template's DROP TABLE form.
	{"42P01", regexp.MustCompile(`^table "[^"]+" does not exist$`), nil},
	{"3F000", regexp.MustCompile(`^schema "[^"]+" does not exist$`), nil},
	{"42704", regexp.MustCompile(`^role "[^"]+" does not exist$`), nil},
	{"42704", regexp.MustCompile(`^type "[^"]+" does not exist$`), func(n string) bool { return e1Types[strings.TrimSuffix(n, "[]")] }},
	{"42704", regexp.MustCompile(`^operator class "[^"]+" does not exist for access method "[^"]+"$`), func(n string) bool {
		for _, o := range e1Opclasses {
			if o.name == n {
				return true
			}
		}
		return false
	}},
	{"42883", regexp.MustCompile(`^function \S+\(.*\) does not exist$`), nil},
	{"42883", regexp.MustCompile(`^operator does not exist: `), nil},
	{"42703", regexp.MustCompile(`^column "[^"]+" of relation "[^"]+" does not exist$`), nil},
}

// e1Template reports whether a refusal's message is one of e1Templates.
func e1Template(code, message string) bool {
	for _, t := range e1Templates {
		if t.code != code || !t.re.MatchString(message) {
			continue
		}
		if t.table == nil {
			return true
		}
		m := quotedName.FindStringSubmatch(message)
		parts := strings.Split(m[1], ".")
		return t.table(strings.ToLower(parts[len(parts)-1]))
	}
	return false
}

// ownRows reports whether the migrations text seen so far (comments
// stripped) holds no COPY and the prelude writes no rows: the fallback of
// H16 (Revision 1.6, amending H9) when the harness cannot read which
// tables a refused statement checks. Rows the set's code writes are the
// set's own.
func ownRows(text string, prelude []string) bool {
	return !foreignRowsFor(text, prelude, nil) && !copyWord.MatchString(stripComments(text))
}

// setName reports a name the set creates, or one Postgres may generate
// for an object of a set table (<table>_pkey, <table>_<column>_idx, ...).
func setName(created map[string]bool, name string) bool {
	if created[name] {
		return true
	}
	for n := range created {
		if strings.HasPrefix(name, n+"_") {
			return true
		}
	}
	return false
}

var lineComment = regexp.MustCompile(`(?m)--[^\n]*$`)

func stripComments(s string) string { return lineComment.ReplaceAllString(s, "") }

// readCatalog reads the comparable catalog facts of the history's tables:
// the history schema, or every non-system schema in a fresh database.
// H19 (Revision 1.7): every catalog relation, type and function is
// qualified with pg_catalog, so a history that puts pg_catalog after its
// own schema in search_path (SET search_path TO public, pg_catalog) and
// creates a table named like a catalog type is still read.
func readCatalog(ctx context.Context, conn *sql.Conn, schema string, all bool) (*verdict, error) {
	filter := "n.nspname = $1"
	if all {
		filter = "($1::pg_catalog.text = $1::pg_catalog.text AND n.nspname NOT IN ('pg_catalog', 'information_schema') AND n.nspname NOT LIKE 'pg\\_toast%' AND n.nspname NOT LIKE 'pg\\_temp\\_%')"
	}
	out := &verdict{Schema: schema}
	type rel struct {
		oid  int64
		key  string
		cols map[int]string
	}
	rels := map[int64]*rel{}
	var order []int64
	rows, err := conn.QueryContext(ctx, `SELECT c.oid::pg_catalog.int8, n.nspname, c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relkind IN ('r', 'p') AND `+filter+` ORDER BY n.nspname, c.relname`, schema)
	if err != nil {
		return nil, fmt.Errorf("reading pg_class: %w", err)
	}
	for rows.Next() {
		var r rel
		var ns, name string
		if err := rows.Scan(&r.oid, &ns, &name); err != nil {
			rows.Close()
			return nil, err
		}
		r.key, r.cols = ns+"."+name, map[int]string{}
		rels[r.oid] = &r
		order = append(order, r.oid)
	}
	rows.Close()
	tables := map[int64]*catTable{}
	for _, oid := range order {
		tables[oid] = &catTable{Key: rels[oid].key, Columns: []catColumn{}, Uniques: []catKey{}, FKs: []catFK{}, Indexes: []catIndex{}}
	}
	if len(order) > 0 {
		rows, err = conn.QueryContext(ctx, `SELECT a.attrelid::pg_catalog.int8, a.attnum, a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod), a.attnotnull, a.atthasdef, a.attidentity <> '', a.attgenerated <> ''
			FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped AND `+filter+` ORDER BY a.attrelid, a.attnum`, schema)
		if err != nil {
			return nil, fmt.Errorf("reading pg_attribute: %w", err)
		}
		for rows.Next() {
			var oid int64
			var num int
			var c catColumn
			if err := rows.Scan(&oid, &num, &c.Name, &c.Type, &c.NotNull, &c.HasDef, &c.Identity, &c.Generated); err != nil {
				rows.Close()
				return nil, err
			}
			if t := tables[oid]; t != nil {
				t.Columns = append(t.Columns, c)
				rels[oid].cols[num] = c.Name
			}
		}
		rows.Close()
		names := func(oid int64, list string) []string {
			out := []string{}
			if list == "" {
				return out
			}
			for _, s := range strings.Split(list, ",") {
				var n int
				fmt.Sscan(s, &n)
				if r := rels[oid]; r != nil {
					out = append(out, r.cols[n])
				} else {
					out = append(out, fmt.Sprintf("#%d", n))
				}
			}
			return out
		}
		backing := map[int64]bool{}
		rows, err = conn.QueryContext(ctx, `SELECT con.conrelid::pg_catalog.int8, con.conname, con.contype, pg_catalog.array_to_string(con.conkey, ','), con.confrelid::pg_catalog.int8, coalesce(pg_catalog.array_to_string(con.confkey, ','), ''), con.conindid::pg_catalog.int8,
			coalesce((SELECT fn.nspname || '.' || fc.relname FROM pg_catalog.pg_class fc JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace WHERE fc.oid = con.confrelid), '')
			FROM pg_catalog.pg_constraint con JOIN pg_catalog.pg_class c ON c.oid = con.conrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE con.contype IN ('p', 'u', 'f', 'x') AND `+filter+` ORDER BY con.conrelid, con.conname`, schema)
		if err != nil {
			return nil, fmt.Errorf("reading pg_constraint: %w", err)
		}
		type fkRow struct {
			oid, ref int64
			fk       catFK
			refCols  string
		}
		var fks []fkRow
		for rows.Next() {
			var oid, ref, ind int64
			var name, typ, cols, refCols, refName string
			if err := rows.Scan(&oid, &name, &typ, &cols, &ref, &refCols, &ind, &refName); err != nil {
				rows.Close()
				return nil, err
			}
			t := tables[oid]
			if t == nil {
				continue
			}
			switch typ {
			case "p":
				t.PK = names(oid, cols)
				backing[ind] = true
			case "u":
				t.Uniques = append(t.Uniques, catKey{Name: name, Columns: names(oid, cols)})
				backing[ind] = true
			case "x":
				backing[ind] = true
			case "f":
				fks = append(fks, fkRow{oid: oid, ref: ref, fk: catFK{Name: name, Columns: names(oid, cols), RefTable: refName}, refCols: refCols})
			}
		}
		rows.Close()
		for _, f := range fks {
			f.fk.RefColumns = names(f.ref, f.refCols)
			if rels[f.ref] == nil {
				// A table outside the read schemas: read its columns directly.
				f.fk.RefColumns = nil
				for _, s := range strings.Split(f.refCols, ",") {
					var name string
					if err := conn.QueryRowContext(ctx, "SELECT attname FROM pg_catalog.pg_attribute WHERE attrelid = $1 AND attnum = $2", f.ref, s).Scan(&name); err == nil {
						f.fk.RefColumns = append(f.fk.RefColumns, name)
					}
				}
			}
			tables[f.oid].FKs = append(tables[f.oid].FKs, f.fk)
		}
		rows, err = conn.QueryContext(ctx, `SELECT i.indrelid::pg_catalog.int8, i.indexrelid::pg_catalog.int8, ic.relname, i.indisunique, pg_catalog.array_to_string(i.indkey::pg_catalog.int2[], ','), i.indnkeyatts, i.indpred IS NOT NULL
			FROM pg_catalog.pg_index i JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid JOIN pg_catalog.pg_class c ON c.oid = i.indrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE `+filter+` ORDER BY i.indrelid, ic.relname`, schema)
		if err != nil {
			return nil, fmt.Errorf("reading pg_index: %w", err)
		}
		for rows.Next() {
			var oid, ind int64
			var ix catIndex
			var keys string
			var nkey int
			if err := rows.Scan(&oid, &ind, &ix.Name, &ix.Unique, &keys, &nkey, &ix.Partial); err != nil {
				rows.Close()
				return nil, err
			}
			t := tables[oid]
			if t == nil || backing[ind] {
				continue
			}
			for k, s := range strings.Split(keys, ",") {
				if k >= nkey {
					break
				}
				if s == "0" {
					ix.Columns = append(ix.Columns, "")
				} else {
					ix.Columns = append(ix.Columns, names(oid, s)...)
				}
			}
			t.Indexes = append(t.Indexes, ix)
		}
		rows.Close()
	}
	for _, oid := range order {
		out.Tables = append(out.Tables, *tables[oid])
	}
	rows, err = conn.QueryContext(ctx, `SELECT n.nspname || '.' || t.typname, e.enumlabel FROM pg_catalog.pg_enum e JOIN pg_catalog.pg_type t ON t.oid = e.enumtypid JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		WHERE `+filter+` ORDER BY 1, e.enumsortorder`, schema)
	if err != nil {
		return nil, fmt.Errorf("reading pg_enum: %w", err)
	}
	enums := map[string]*catEnum{}
	var keys []string
	for rows.Next() {
		var key, label string
		if err := rows.Scan(&key, &label); err != nil {
			rows.Close()
			return nil, err
		}
		if enums[key] == nil {
			enums[key] = &catEnum{Key: key}
			keys = append(keys, key)
		}
		enums[key].Labels = append(enums[key].Labels, label)
	}
	rows.Close()
	sort.Strings(keys)
	for _, k := range keys {
		out.Enums = append(out.Enums, *enums[k])
	}
	return out, nil
}

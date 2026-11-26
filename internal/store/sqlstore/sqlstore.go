// Package sqlstore shares graph semantics across the two supported SQL drivers.
package sqlstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
)

// DB implements one graph model rather than letting backend behavior drift.
type DB struct {
	db      *sql.DB
	dialect string
}

// Open takes ownership of the connection pool, including on migration failure.
func Open(ctx context.Context, db *sql.DB, dialect string) (store.Store, error) {
	s := &DB{db, dialect}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s store: %w", dialect, err)
	}
	return s, nil
}

func (s *DB) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning migration: %w", err)
	}
	defer tx.Rollback()
	if s.dialect == "postgres" {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(714839210)"); err != nil {
			return fmt.Errorf("locking migrations: %w", err)
		}
	}
	for _, q := range []string{
		"CREATE TABLE IF NOT EXISTS schema_version (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), version INTEGER NOT NULL)",
		"INSERT INTO schema_version(singleton,version) VALUES(1,0) ON CONFLICT(singleton) DO NOTHING",
		"UPDATE schema_version SET version=version WHERE singleton=1",
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("initializing schema version: %w", err)
		}
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT version FROM schema_version WHERE singleton=1").Scan(&version); err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}
	if version > migrations.Version {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, migrations.Version)
	}
	if version < 0 {
		return fmt.Errorf("invalid database schema version %d", version)
	}
	for next := version + 1; next <= migrations.Version; next++ {
		data, err := migrations.Files.ReadFile(fmt.Sprintf("%s/%04d_graph.sql", s.dialect, next))
		if err != nil {
			return fmt.Errorf("reading migration %d: %w", next, err)
		}
		for _, q := range strings.Split(string(data), ";") {
			if strings.TrimSpace(q) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("applying migration %d: %w", next, err)
			}
		}
		if next == 3 {
			if err := backfillNames(ctx, tx); err != nil {
				return fmt.Errorf("applying migration %d: %w", next, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE schema_version SET version=$1 WHERE singleton=1", next); err != nil {
			return fmt.Errorf("recording migration %d: %w", next, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migrations: %w", err)
	}
	return nil
}

// Close releases the backend resources owned by this store.
func (s *DB) Close() error { return s.db.Close() }

// OpenRead checks an existing schema without running migration writes.
func OpenRead(ctx context.Context, db *sql.DB, dialect string) (store.Store, error) {
	var version int
	if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version WHERE singleton=1").Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening existing %s store: %w", dialect, err)
	}
	if version != migrations.Version {
		db.Close()
		return nil, fmt.Errorf("store schema version %d requires indexing with schema %d", version, migrations.Version)
	}
	return &DB{db: db, dialect: dialect}, nil
}

type transaction struct {
	tx                *sql.Tx
	run               store.Run
	ordinal           int
	old               map[string]map[string]string
	seen              map[string]map[string]bool
	writes            store.Writes
	preserveFactCache bool
	// options are the run's options; journal is false for ref runs.
	options store.RunOptions
	journal journalState
}

// BeginRun serializes writes to one repository and stages a complete replacement.
func (s *DB) BeginRun(ctx context.Context, h contract.Header, options ...store.RunOptions) (store.Transaction, error) {
	finish := profilePhase("comparison")
	defer func() {
		if finish != nil {
			finish()
		}
	}()
	if err := contract.Validate(h); err != nil {
		return nil, fmt.Errorf("beginning run: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning run: %w", err)
	}
	t := &transaction{tx: tx, run: store.Run{ID: rand.Text(), Header: h, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, old: map[string]map[string]string{}, seen: map[string]map[string]bool{}}
	if len(options) > 0 {
		t.options = options[0]
		t.preserveFactCache = options[0].PreserveFactCache
		t.run.Settings = options[0].Settings
	}
	fail := func(err error) (store.Transaction, error) { tx.Rollback(); return nil, err }
	if err := checkNamespace(h.Repository, t.options.Namespace); err != nil {
		return fail(fmt.Errorf("beginning run: %w", err))
	}
	t.journal = newJournalState(t.options.Namespace == nil)
	if err := t.exec(ctx, "INSERT INTO repositories(repository) VALUES($1) ON CONFLICT(repository) DO NOTHING", h.Repository); err != nil {
		return fail(err)
	}
	if err := t.exec(ctx, "UPDATE repositories SET generation=generation+1 WHERE repository=$1", h.Repository); err != nil {
		return fail(err)
	}
	for _, table := range []string{"files", "nodes", "edges"} {
		t.old[table] = map[string]string{}
		t.seen[table] = map[string]bool{}
		rows, err := tx.QueryContext(ctx, "SELECT payload FROM "+table+" WHERE repository=$1", h.Repository)
		if err != nil {
			return fail(err)
		}
		for rows.Next() {
			var data string
			if err := rows.Scan(&data); err != nil {
				rows.Close()
				return fail(err)
			}
			key, err := recordKey(table, data)
			if err != nil {
				rows.Close()
				return fail(err)
			}
			t.old[table][key] = data
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fail(err)
		}
	}
	if err := t.exec(ctx, "DELETE FROM diagnostics WHERE repository=$1", h.Repository); err != nil {
		return fail(err)
	}
	if finish != nil {
		finish()
		finish = nil
	}
	profileMaps("old-loaded", t)
	return t, nil
}

func edgeKey(e contract.Edge) string { return e.Kind + "\x00" + e.Source + "\x00" + e.Target }
func recordKey(table, data string) (string, error) {
	switch table {
	case "files":
		var f contract.File
		err := json.Unmarshal([]byte(data), &f)
		return f.Path, err
	case "nodes":
		var n contract.Node
		err := json.Unmarshal([]byte(data), &n)
		return n.Identity, err
	default:
		var e contract.Edge
		err := json.Unmarshal([]byte(data), &e)
		return edgeKey(e), err
	}
}
func (t *transaction) unchanged(table, key, data string) bool {
	t.seen[table][key] = true
	if old, ok := t.old[table][key]; ok && old == data {
		return true
	}
	t.remember(table, key)
	t.old[table][key] = data
	return false
}

// Writes exposes actual graph changes for incremental verification.
func (t *transaction) Writes() store.Writes { return t.writes }

func (t *transaction) exec(ctx context.Context, q string, args ...any) error {
	finish := profilePhase("writes")
	if finish != nil {
		defer finish()
	}
	if _, err := t.tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("writing graph store: %w", err)
	}
	return nil
}

func payload(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encoding store payload: %w", err)
	}
	return string(data), nil
}

func invalid(err error) error { return fmt.Errorf("%w: %v", store.ErrInvalid, err) }

func (t *transaction) exists(ctx context.Context, table, column, key string) (bool, error) {
	return t.seen[table][key], nil
}

func (t *transaction) UpsertFile(ctx context.Context, f contract.File) error {
	finish := profilePhase("comparison")
	defer func() {
		if finish != nil {
			finish()
		}
	}()
	if err := contract.Validate(f); err != nil {
		return invalid(err)
	}
	data, err := payload(f)
	if err != nil {
		return err
	}
	// Replacing a changed file removes its symbols and every incident edge first.
	var old string
	err = t.tx.QueryRowContext(ctx, "SELECT content_hash FROM files WHERE repository=$1 AND path=$2", t.run.Header.Repository, f.Path).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("reading file hash: %w", err)
	}
	if err == nil && old != f.ContentHash && t.seen["files"][f.Path] {
		if err := t.DeleteFile(ctx, f.Path); err != nil {
			return err
		}
	}
	unchanged := t.unchanged("files", f.Path, data)
	if finish != nil {
		finish()
		finish = nil
	}
	if unchanged {
		return nil
	}
	t.writes.Files++
	return t.exec(ctx, "INSERT INTO files(repository,path,content_hash,payload) VALUES($1,$2,$3,$4) ON CONFLICT(repository,path) DO UPDATE SET content_hash=excluded.content_hash,payload=excluded.payload", t.run.Header.Repository, f.Path, f.ContentHash, data)
}

func (t *transaction) UpsertNode(ctx context.Context, n contract.Node) error {
	finish := profilePhase("comparison")
	defer func() {
		if finish != nil {
			finish()
		}
	}()
	if err := contract.Validate(n); err != nil {
		return invalid(err)
	}
	var file any
	if n.File != "." {
		ok, err := t.exists(ctx, "files", "path", n.File)
		if err != nil {
			return err
		}
		if !ok {
			return invalid(fmt.Errorf("node file %q has not been emitted", n.File))
		}
		file = n.File
	}
	data, err := payload(n)
	if err != nil {
		return err
	}
	unchanged := t.unchanged("nodes", n.Identity, data)
	if finish != nil {
		finish()
		finish = nil
	}
	if unchanged {
		return nil
	}
	t.writes.Nodes++
	// Name columns come from the payload being written, never from n directly.
	name, qualified := nameColumns(t.run.Header.Repository, n.Identity, data)
	return t.exec(ctx, "INSERT INTO nodes(repository,identity,file,kind,level,content_hash,payload,name,qualified_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(repository,identity) DO UPDATE SET file=excluded.file,kind=excluded.kind,level=excluded.level,content_hash=excluded.content_hash,payload=excluded.payload,name=excluded.name,qualified_name=excluded.qualified_name", t.run.Header.Repository, n.Identity, file, n.Kind, n.Level, n.ContentHash, data, name, qualified)
}

func (t *transaction) UpsertEdge(ctx context.Context, e contract.Edge) error {
	finish := profilePhase("comparison")
	defer func() {
		if finish != nil {
			finish()
		}
	}()
	if err := contract.Validate(e); err != nil {
		return invalid(err)
	}
	for _, id := range []string{e.Source, e.Target} {
		ok, err := t.exists(ctx, "nodes", "identity", id)
		if err != nil {
			return err
		}
		if !ok {
			return invalid(fmt.Errorf("unresolved edge endpoint %q", id))
		}
	}
	for _, evidence := range e.Evidence {
		ok, err := t.exists(ctx, "files", "path", evidence.File)
		if err != nil {
			return err
		}
		if !ok {
			return invalid(fmt.Errorf("unknown evidence file %q", evidence.File))
		}
	}
	data, err := payload(e)
	if err != nil {
		return err
	}
	unchanged := t.unchanged("edges", edgeKey(e), data)
	if finish != nil {
		finish()
		finish = nil
	}
	if unchanged {
		return nil
	}
	t.writes.Edges++
	return t.exec(ctx, "INSERT INTO edges(repository,kind,source,target,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(repository,kind,source,target) DO UPDATE SET payload=excluded.payload", t.run.Header.Repository, e.Kind, e.Source, e.Target, data)
}

func (t *transaction) DeleteFile(ctx context.Context, path string) error {
	if !contract.ValidPath(path, false) {
		return invalid(fmt.Errorf("invalid file path"))
	}
	t.remember("files", path)
	delete(t.old["files"], path)
	delete(t.seen["files"], path)
	t.journal.dropGaps(path)
	// The database cascades the file's nodes and, through them, every
	// incident edge; the maps (and the journal) follow the same sets.
	deleted := map[string]bool{}
	for id, data := range t.old["nodes"] {
		var n contract.Node
		_ = json.Unmarshal([]byte(data), &n)
		if n.File == path {
			t.remember("nodes", id)
			delete(t.old["nodes"], id)
			delete(t.seen["nodes"], id)
			deleted[id] = true
		}
	}
	for key, data := range t.old["edges"] {
		var e contract.Edge
		_ = json.Unmarshal([]byte(data), &e)
		remove := deleted[e.Source] || deleted[e.Target]
		for _, v := range e.Evidence {
			remove = remove || v.File == path
		}
		if remove {
			t.remember("edges", key)
			delete(t.old["edges"], key)
			delete(t.seen["edges"], key)
		}
	}
	// Evidence can belong to a third file unrelated to either endpoint.
	rows, err := t.tx.QueryContext(ctx, "SELECT payload FROM edges WHERE repository=$1", t.run.Header.Repository)
	if err != nil {
		return fmt.Errorf("reading edge evidence: %w", err)
	}
	var remove []contract.Edge
	for rows.Next() {
		var data string
		var e contract.Edge
		if err := rows.Scan(&data); err != nil {
			rows.Close()
			return fmt.Errorf("reading edge evidence payload: %w", err)
		}
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			rows.Close()
			return fmt.Errorf("decoding edge evidence payload: %w", err)
		}
		for _, v := range e.Evidence {
			if v.File == path {
				remove = append(remove, e)
				break
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("reading edge evidence rows: %w", err)
	}
	for _, e := range remove {
		if err := t.exec(ctx, "DELETE FROM edges WHERE repository=$1 AND kind=$2 AND source=$3 AND target=$4", t.run.Header.Repository, e.Kind, e.Source, e.Target); err != nil {
			return err
		}
	}
	if err := t.exec(ctx, "DELETE FROM diagnostics WHERE repository=$1 AND file=$2", t.run.Header.Repository, path); err != nil {
		return err
	}
	return t.exec(ctx, "DELETE FROM files WHERE repository=$1 AND path=$2", t.run.Header.Repository, path)
}

func (t *transaction) Diagnostic(ctx context.Context, d contract.Diagnostic) error {
	finish := profilePhase("comparison")
	defer func() {
		if finish != nil {
			finish()
		}
	}()
	if err := contract.Validate(d); err != nil {
		return invalid(err)
	}
	data, err := payload(d)
	if err != nil {
		return err
	}
	t.ordinal++
	t.journal.noteDiagnostic(d)
	if finish != nil {
		finish()
		finish = nil
	}
	return t.exec(ctx, "INSERT INTO diagnostics(repository,ordinal,file,payload) VALUES($1,$2,$3,$4)", t.run.Header.Repository, t.ordinal, d.File, data)
}

func (t *transaction) Commit(ctx context.Context) error {
	profileMaps("before-prune", t)
	pruned := profilePhase("pruning")
	defer func() {
		if pruned != nil {
			pruned()
		}
	}()
	// Prune only absent keys, after framing and all references have validated.
	for _, table := range []string{"edges", "nodes", "files"} {
		for key, data := range t.old[table] {
			if t.seen[table][key] {
				continue
			}
			t.remember(table, key)
			var err error
			switch table {
			case "edges":
				var e contract.Edge
				_ = json.Unmarshal([]byte(data), &e)
				err = t.exec(ctx, "DELETE FROM edges WHERE repository=$1 AND kind=$2 AND source=$3 AND target=$4", t.run.Header.Repository, e.Kind, e.Source, e.Target)
			case "nodes":
				err = t.exec(ctx, "DELETE FROM nodes WHERE repository=$1 AND identity=$2", t.run.Header.Repository, key)
			case "files":
				err = t.exec(ctx, "DELETE FROM files WHERE repository=$1 AND path=$2", t.run.Header.Repository, key)
			}
			if err != nil {
				return err
			}
			t.writes.Deleted++
		}
	}
	if !t.preserveFactCache {
		if err := t.exec(ctx, "DELETE FROM fact_cache WHERE repository=$1 AND path NOT IN (SELECT path FROM files WHERE repository=$1)", t.run.Header.Repository); err != nil {
			return err
		}
	}
	if pruned != nil {
		pruned()
		pruned = nil
	}
	committed := profilePhase("commit")
	if committed != nil {
		defer committed()
	}
	t.run.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	data, err := payload(t.run)
	if err != nil {
		return err
	}
	// A run without settings stores NULL: unknown, never an empty record.
	var settings sql.NullString
	if t.run.Settings != "" {
		settings = sql.NullString{String: t.run.Settings, Valid: true}
	}
	if err := t.exec(ctx, "INSERT INTO index_runs(id,repository,payload,settings) VALUES($1,$2,$3,$4)", t.run.ID, t.run.Header.Repository, data, settings); err != nil {
		return err
	}
	if t.options.Namespace != nil {
		if err := t.registerRefSnapshot(ctx); err != nil {
			return err
		}
	} else if err := t.writeJournal(ctx); err != nil {
		return err
	}
	if err := t.tx.Commit(); err != nil {
		return fmt.Errorf("committing index run: %w", err)
	}
	return nil
}
func (t *transaction) Rollback() error { return t.tx.Rollback() }

// Run returns the run this transaction records; FinishedAt is set by Commit.
func (t *transaction) Run() store.Run { return t.run }

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func read[T any](ctx context.Context, db queryer, q string, args ...any) ([]T, error) {
	result := []T{}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying graph: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var v T
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("reading graph payload: %w", err)
		}
		if err := decodeStored(data, &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading graph rows: %w", err)
	}
	return result, nil
}

// decodeStored decodes one stored JSON payload, keeping numbers as
// json.Number so attribute precision survives the round trip.
func decodeStored(data string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("decoding stored graph: %w", err)
	}
	return nil
}

// Nodes returns stable identity ordering for projections and deterministic dumps.
func (s *DB) Nodes(ctx context.Context, repo string, filter store.Query) ([]contract.Node, error) {
	if filter.Limit < 0 {
		return nil, fmt.Errorf("node limit must be nonnegative")
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q, args := nodeQuery("payload", "nodes", repo, filter)
	nodes, err := read[contract.Node](ctx, tx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []contract.Node{}
	for _, n := range nodes {
		if !termMatches(n, filter.Term) || !attributeMatches(n, filter.Attribute) || !containsMatches(n, filter.Contains) {
			continue
		}
		out = append(out, n)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

// attributePattern is the SQL prefilter of Query.Attribute: a LIKE pattern
// matching every payload whose text holds the key as a JSON string. It
// selects a superset (LIKE's "_" matches any character, and SQLite's LIKE
// folds ASCII case); attributeMatches is the exact test. A key with
// characters JSON escapes or LIKE treats specially gets no prefilter.
func attributePattern(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return "", false
		}
	}
	return `%"` + key + `"%`, true
}

// attributeMatches applies a Query's exact attribute predicate.
func attributeMatches(n contract.Node, key string) bool {
	if key == "" {
		return true
	}
	_, ok := n.Attributes[key]
	return ok
}

// containsMatches applies a Query's exact Contains predicate.
func containsMatches(n contract.Node, text string) bool {
	return text == "" || store.ContainsFolded(n.Name, text) || store.ContainsFolded(n.QualifiedName, text)
}

// termMatches applies a Query's exact name predicate. Names live in the v1
// payload. Go matching is byte-sensitive on both drivers, avoiding SQLite
// LIKE's ASCII case folding and SQL wildcards.
func termMatches(n contract.Node, term string) bool {
	return term == "" || n.Name == term || n.QualifiedName == term || strings.HasSuffix(n.QualifiedName, term)
}

// NodesWithoutModelClaims reads the nodes filter selects like Nodes, but a
// model record's claims attribute is removed in SQL, so Go never decodes it
// (ADR 0057): SQLite with json_remove; Postgres by rebuilding the payload
// with the json type's json_each and json_object_agg, which keep every
// other value's text (numbers included) as stored, where jsonb's
// "attributes - 'claims'" would rewrite number text.
func (s *DB) NodesWithoutModelClaims(ctx context.Context, repo string, filter store.Query) ([]contract.Node, error) {
	if filter.Limit < 0 {
		return nil, fmt.Errorf("node limit must be nonnegative")
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	columns, from := sqliteWithoutModelClaims, "nodes"
	if s.dialect == "postgres" {
		columns, from = postgresWithoutModelClaims, postgresParsedPayload
	}
	q, args := nodeQuery(columns, from, repo, filter)
	nodes, err := read[contract.Node](ctx, tx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("reading nodes without model claims: %w", err)
	}
	out := []contract.Node{}
	for _, n := range nodes {
		if _, has := n.Attributes["claims"]; has && n.Attributes["source"] == "model" {
			// The SQL left this model record whole (postgresParsedPayload).
			attrs := make(map[string]any, len(n.Attributes)-1)
			for k, v := range n.Attributes {
				if k != "claims" {
					attrs[k] = v
				}
			}
			n.Attributes = attrs
		}
		if !termMatches(n, filter.Term) || !attributeMatches(n, filter.Attribute) || !containsMatches(n, filter.Contains) {
			continue
		}
		out = append(out, n)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

// postgresParsedPayload parses each payload as json once. Postgres's JSON
// functions refuse a document with a \u0000 escape anywhere in it, so a
// payload whose text holds one is not parsed (p is NULL): the projection
// returns it whole and NodesWithoutModelClaims removes its claims in Go. Go
// encodes only a NUL character that way; an escaped backslash before
// "u0000" takes the same, still exact, path.
const postgresParsedPayload = `nodes CROSS JOIN LATERAL (SELECT CASE WHEN strpos(nodes.payload, '\u0000')=0 THEN nodes.payload::json END AS p) AS j`

// sqliteWithoutModelClaims and postgresWithoutModelClaims select a node's
// payload, without the claims attribute when it is a model record.
const (
	sqliteWithoutModelClaims   = `CASE WHEN json_extract(payload,'$.attributes.source')='model' THEN json_remove(payload,'$.attributes.claims') ELSE payload END`
	postgresWithoutModelClaims = `CASE WHEN p->'attributes'->>'source'='model' THEN (SELECT json_object_agg(k, CASE WHEN k='attributes' THEN (SELECT coalesce(json_object_agg(ak,av),'{}'::json) FROM json_each(v) AS a(ak,av) WHERE ak<>'claims') ELSE v END) FROM json_each(p) AS t(k,v))::text ELSE payload END`
)

// nodeQuery builds the identity-ordered read for Nodes and
// NodesWithoutModelClaims: columns are the selected expressions, from the
// FROM clause around the nodes table. A name term adds a SQL prefilter that
// selects a superset of the matches; the caller then applies the exact Go
// predicate.
func nodeQuery(columns, from, repo string, filter store.Query) (string, []any) {
	conditions := ""
	args := []any{repo}
	for _, pair := range []struct{ column, value string }{{"identity", filter.Identity}, {"file", filter.File}, {"kind", filter.Kind}} {
		if pair.value != "" {
			args = append(args, pair.value)
			if pair.column == "file" && pair.value == "." {
				conditions += " AND file IS NULL"
				args = args[:len(args)-1]
			} else {
				conditions += fmt.Sprintf(" AND %s=$%d", pair.column, len(args))
			}
		}
	}
	if filter.Level != nil {
		args = append(args, *filter.Level)
		conditions += fmt.Sprintf(" AND level=$%d", len(args))
	}
	if indexableTerm(filter.Term) {
		conditions, args = termPrefilter(filter.Term, conditions, args)
	}
	if indexableTerm(filter.Contains) {
		conditions, args = containsPrefilter(filter.Contains, conditions, args)
	}
	if pattern, ok := attributePattern(filter.Attribute); ok {
		args = append(args, pattern)
		conditions += fmt.Sprintf(" AND payload LIKE $%d", len(args))
	}
	q := "SELECT " + columns + " FROM " + from + " WHERE nodes.repository=$1" + conditions
	if conditions != "" && filter.Identity == "" {
		// With ORDER BY identity on the outer query alone, both planners
		// would walk the primary key and test every row. Selecting
		// identities first lets them use nodes_names or nodes_kind_level.
		q = "SELECT " + columns + " FROM " + from + " WHERE nodes.repository=$1 AND nodes.identity IN (SELECT identity FROM nodes WHERE repository=$1" + conditions + ")"
	}
	return q + " ORDER BY nodes.identity", args
}

func (s *DB) readTransaction(ctx context.Context) (*sql.Tx, error) {
	options := &sql.TxOptions{ReadOnly: true}
	if s.dialect == "postgres" {
		options.Isolation = sql.LevelRepeatableRead
	}
	tx, err := s.db.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("beginning graph read: %w", err)
	}
	return tx, nil
}

// Edges returns every matching edge once, ordered identically on both drivers
// by (kind, source, target) in Go byte order. Sets are chunked inside one read transaction to bound SQL parameter counts.
func (s *DB) Edges(ctx context.Context, repo string, identities, kinds []string, direction store.Direction) ([]contract.Edge, error) {
	column := "source"
	if direction == store.Incoming {
		column = "target"
	} else if direction != store.Outgoing {
		return nil, fmt.Errorf("invalid edge direction %q", direction)
	}
	out := []contract.Edge{}
	if len(identities) == 0 {
		return out, nil
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids := map[string]bool{}
	for _, id := range identities {
		ids[id] = true
	}
	ordered := []string{}
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	kindSet := map[string]bool{}
	for _, kind := range kinds {
		kindSet[kind] = true
	}
	for start := 0; start < len(ordered); start += 128 {
		end := min(start+128, len(ordered))
		args := []any{repo}
		placeholders := []string{}
		for _, id := range ordered[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		// No ORDER BY: the sort below defines the order, and (kind, source,
		// target) is the primary key, so it is total. Ordering in SQL made
		// SQLite walk the primary key for the whole repository instead of
		// using edges_incoming/edges_outgoing (ADR 0039).
		q := "SELECT payload FROM edges WHERE repository=$1 AND " + column + " IN (" + strings.Join(placeholders, ",") + ")"
		edges, err := read[contract.Edge](ctx, tx, q, args...)
		if err != nil {
			return nil, err
		}
		for _, e := range edges {
			if len(kindSet) == 0 || kindSet[e.Kind] {
				out = append(out, e)
			}
		}
	}
	sortEdges(out)
	return out, nil
}

// sortEdges orders edges by (kind, source, target) in Go byte order, the
// same on both drivers whatever the database collation.
func sortEdges(edges []contract.Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Target < b.Target
	})
}

// EdgesOfKind reads the edges of one kind through the primary key
// (repository, kind, source, target); no ORDER BY, for the reason Edges
// gives, so the sort defines the order.
func (s *DB) EdgesOfKind(ctx context.Context, repo, kind string) ([]contract.Edge, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	edges, err := read[contract.Edge](ctx, tx, "SELECT payload FROM edges WHERE repository=$1 AND kind=$2", repo, kind)
	if err != nil {
		return nil, err
	}
	sortEdges(edges)
	return edges, nil
}

// FactFiles reads, in one read transaction, the files of the nodes that
// are not file nodes and the evidence files of every edge. Evidence lives
// only in the edge payload, so each edge payload is decoded into its
// evidence list alone.
func (s *DB) FactFiles(ctx context.Context, repo string) (map[string]bool, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := map[string]bool{}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT file FROM nodes WHERE repository=$1 AND kind<>'file' AND file IS NOT NULL", repo)
	if err != nil {
		return nil, fmt.Errorf("reading node files: %w", err)
	}
	for rows.Next() {
		var file string
		if err := rows.Scan(&file); err != nil {
			rows.Close()
			return nil, fmt.Errorf("reading node files: %w", err)
		}
		out[file] = true
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("reading node files: %w", err)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading node files: %w", err)
	}
	type evidence struct {
		Evidence []struct {
			File string `json:"file"`
		} `json:"evidence"`
	}
	edges, err := read[evidence](ctx, tx, "SELECT payload FROM edges WHERE repository=$1", repo)
	if err != nil {
		return nil, fmt.Errorf("reading edge evidence: %w", err)
	}
	for _, e := range edges {
		for _, ev := range e.Evidence {
			out[ev.File] = true
		}
	}
	return out, nil
}

// Neighbours deduplicates nodes even if multiple edge kinds connect the endpoints.
func (s *DB) Neighbours(ctx context.Context, repo, id, kind string, direction store.Direction) ([]contract.Node, error) {
	match, other := "source", "target"
	if direction == store.Incoming {
		match, other = other, match
	} else if direction != store.Outgoing {
		return nil, fmt.Errorf("invalid neighbour direction %q", direction)
	}
	q := "SELECT DISTINCT n.identity,n.payload FROM nodes n JOIN edges e ON n.repository=e.repository AND n.identity=e." + other + " WHERE e.repository=$1 AND e." + match + "=$2"
	args := []any{repo, id}
	if kind != "" {
		q += " AND e.kind=$3"
		args = append(args, kind)
	}
	// The subquery retains identity for sorting, while the decoder reads one column.
	return read[contract.Node](ctx, s.db, "SELECT payload FROM ("+q+") neighbours ORDER BY identity", args...)
}

// Dump exposes facts without volatile run timestamps so both stores share goldens.
func (s *DB) Dump(ctx context.Context, repo string) (store.Snapshot, error) {
	var out store.Snapshot
	// SQLite pins a snapshot at its first SELECT. Postgres needs repeatable read
	// so each table is read from the same committed repository generation.
	var options *sql.TxOptions
	if s.dialect == "postgres" {
		options = &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead}
	}
	tx, err := s.db.BeginTx(ctx, options)
	if err != nil {
		return out, fmt.Errorf("beginning snapshot query: %w", err)
	}
	defer tx.Rollback()
	if out.Files, err = read[contract.File](ctx, tx, "SELECT payload FROM files WHERE repository=$1 ORDER BY path", repo); err != nil {
		return out, err
	}
	if out.Nodes, err = read[contract.Node](ctx, tx, "SELECT payload FROM nodes WHERE repository=$1 ORDER BY identity", repo); err != nil {
		return out, err
	}
	if out.Edges, err = read[contract.Edge](ctx, tx, "SELECT payload FROM edges WHERE repository=$1 ORDER BY kind,source,target", repo); err != nil {
		return out, err
	}
	out.Diagnostics, err = read[contract.Diagnostic](ctx, tx, "SELECT payload FROM diagnostics WHERE repository=$1 ORDER BY ordinal", repo)
	return out, err
}

// FileHashes reads the files table's hash column in one read transaction.
// The column is written together with the payload by UpsertFile.
func (s *DB) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT path,content_hash FROM files WHERE repository=$1", repo)
	if err != nil {
		return nil, fmt.Errorf("querying graph: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var path, hash string
		if err := rows.Scan(&path, &hash); err != nil {
			return nil, fmt.Errorf("reading file hash: %w", err)
		}
		out[path] = hash
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading graph rows: %w", err)
	}
	return out, nil
}

// Files reads every file record payload inside one read transaction.
func (s *DB) Files(ctx context.Context, repo string) ([]contract.File, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	files, err := read[contract.File](ctx, tx, "SELECT payload FROM files WHERE repository=$1", repo)
	if err != nil {
		return nil, err
	}
	sortFiles(files)
	return files, nil
}

// sortFiles orders file records by path in byte order on both drivers,
// whatever the database collation.
func sortFiles(files []contract.File) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

// FilesByPath chunks the deduplicated path list inside one read
// transaction, like NodesByIdentity, so parameter counts stay bounded.
func (s *DB) FilesByPath(ctx context.Context, repo string, paths []string) ([]contract.File, error) {
	out := []contract.File{}
	if len(paths) == 0 {
		return out, nil
	}
	ordered := distinctSorted(paths)
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for start := 0; start < len(ordered); start += 128 {
		end := min(start+128, len(ordered))
		args := []any{repo}
		placeholders := []string{}
		for _, p := range ordered[start:end] {
			args = append(args, p)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		files, err := read[contract.File](ctx, tx, "SELECT payload FROM files WHERE repository=$1 AND path IN ("+strings.Join(placeholders, ",")+")", args...)
		if err != nil {
			return nil, err
		}
		out = append(out, files...)
	}
	sortFiles(out)
	return out, nil
}

// distinctSorted returns the distinct values in byte order.
func distinctSorted(values []string) []string {
	set := map[string]bool{}
	out := []string{}
	for _, v := range values {
		if !set[v] {
			set[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// NodesByIdentity chunks the identity set inside one read transaction, like
// Edges, so parameter counts stay bounded on both drivers.
func (s *DB) NodesByIdentity(ctx context.Context, repo string, identities []string) ([]contract.Node, error) {
	out := []contract.Node{}
	if len(identities) == 0 {
		return out, nil
	}
	set := map[string]bool{}
	ordered := []string{}
	for _, id := range identities {
		if !set[id] {
			set[id] = true
			ordered = append(ordered, id)
		}
	}
	sort.Strings(ordered)
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for start := 0; start < len(ordered); start += 128 {
		end := min(start+128, len(ordered))
		args := []any{repo}
		placeholders := []string{}
		for _, id := range ordered[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		nodes, err := read[contract.Node](ctx, tx, "SELECT payload FROM nodes WHERE repository=$1 AND identity IN ("+strings.Join(placeholders, ",")+") ORDER BY identity", args...)
		if err != nil {
			return nil, err
		}
		// Chunks follow the sorted identity list, so appending keeps order.
		out = append(out, nodes...)
	}
	return out, nil
}

// Diagnostics reads the diagnostics of the given files inside one read
// transaction, chunking the deduplicated file list like Edges so parameter
// counts stay bounded. The ordinal is read beside the payload and the merged
// result sorted by it in Go, so the chunks need no ORDER BY. The query uses
// the diagnostics_file index of both migration sets.
func (s *DB) Diagnostics(ctx context.Context, repo string, files []string) ([]contract.Diagnostic, error) {
	out := []contract.Diagnostic{}
	if len(files) == 0 {
		return out, nil
	}
	set := map[string]bool{}
	ordered := []string{}
	for _, f := range files {
		if !set[f] {
			set[f] = true
			ordered = append(ordered, f)
		}
	}
	sort.Strings(ordered)
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics of %s: %w", repo, err)
	}
	defer tx.Rollback()
	rows := []ordinalDiagnostic{}
	for start := 0; start < len(ordered); start += 128 {
		end := min(start+128, len(ordered))
		args := []any{repo}
		placeholders := []string{}
		for _, f := range ordered[start:end] {
			args = append(args, f)
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		chunk, err := diagnosticRows(ctx, tx, "SELECT ordinal,payload FROM diagnostics WHERE repository=$1 AND file IN ("+strings.Join(placeholders, ",")+")", args...)
		if err != nil {
			return nil, fmt.Errorf("reading diagnostics of %s (file chunk %d): %w", repo, start/128, err)
		}
		rows = append(rows, chunk...)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ordinal < rows[j].ordinal })
	for _, r := range rows {
		out = append(out, r.d)
	}
	return out, nil
}

// DiagnosticsWithoutFile reads, in one read transaction and by ordinal,
// the diagnostics whose file has no row in files. Diagnostics on stored
// files are the large majority and are not decoded.
func (s *DB) DiagnosticsWithoutFile(ctx context.Context, repo string) ([]contract.Diagnostic, error) {
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics without a file of %s: %w", repo, err)
	}
	defer tx.Rollback()
	rows, err := diagnosticRows(ctx, tx, "SELECT d.ordinal,d.payload FROM diagnostics d WHERE d.repository=$1 AND NOT EXISTS (SELECT 1 FROM files f WHERE f.repository=d.repository AND f.path=d.file) ORDER BY d.ordinal", repo)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics without a file of %s: %w", repo, err)
	}
	out := make([]contract.Diagnostic, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.d)
	}
	return out, nil
}

// DiagnosticsWithCodes reads, in one read transaction and by ordinal, the
// diagnostics whose code is one of codes or starts with one of prefixes,
// whatever their file. The code is read from the stored payload in SQL
// (json_extract on SQLite, ->> on Postgres), so rows with other codes are
// not decoded; the decoded code is checked again in Go, so both drivers
// return exactly the matching diagnostics. (A plain substring search with
// instr was slower than json_extract on the pure-Go SQLite driver.)
func (s *DB) DiagnosticsWithCodes(ctx context.Context, repo string, codes, prefixes []string) ([]contract.Diagnostic, error) {
	out := []contract.Diagnostic{}
	if len(codes) == 0 && len(prefixes) == 0 {
		return out, nil
	}
	code := "json_extract(payload,'$.code')"
	if s.dialect == "postgres" {
		code = "(payload::json->>'code')"
	}
	args := []any{repo}
	param := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions := []string{}
	if len(codes) > 0 {
		placeholders := []string{}
		for _, c := range codes {
			placeholders = append(placeholders, param(c))
		}
		conditions = append(conditions, code+" IN ("+strings.Join(placeholders, ",")+")")
	}
	for _, p := range prefixes {
		conditions = append(conditions, fmt.Sprintf("substr(%s,1,%d)=%s", code, len([]rune(p)), param(p)))
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics by code of %s: %w", repo, err)
	}
	defer tx.Rollback()
	rows, err := diagnosticRows(ctx, tx, "SELECT ordinal,payload FROM diagnostics WHERE repository=$1 AND ("+strings.Join(conditions, " OR ")+") ORDER BY ordinal", args...)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics by code of %s: %w", repo, err)
	}
	want := map[string]bool{}
	for _, c := range codes {
		want[c] = true
	}
	for _, r := range rows {
		match := want[r.d.Code]
		for _, p := range prefixes {
			match = match || strings.HasPrefix(r.d.Code, p)
		}
		if match {
			out = append(out, r.d)
		}
	}
	return out, nil
}

// DiagnosticsWithMessage implements store.Store.
func (s *DB) DiagnosticsWithMessage(ctx context.Context, repo, code, text string) ([]contract.Diagnostic, error) {
	out := []contract.Diagnostic{}
	codeExpr, contains := "json_extract(payload,'$.code')", "instr(json_extract(payload,'$.message'),$3)>0"
	if s.dialect == "postgres" {
		codeExpr, contains = "(payload::json->>'code')", "strpos(payload::json->>'message',$3)>0"
	}
	tx, err := s.readTransaction(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics by message of %s: %w", repo, err)
	}
	defer tx.Rollback()
	rows, err := diagnosticRows(ctx, tx, "SELECT ordinal,payload FROM diagnostics WHERE repository=$1 AND "+codeExpr+"=$2 AND "+contains+" ORDER BY ordinal", repo, code, text)
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics by message of %s: %w", repo, err)
	}
	for _, r := range rows {
		// The SQL filter only narrows the read; the decoded record decides.
		if r.d.Code == code && strings.Contains(r.d.Message, text) {
			out = append(out, r.d)
		}
	}
	return out, nil
}

// ordinalDiagnostic is one diagnostics row with its stored ordinal.
type ordinalDiagnostic struct {
	ordinal int64
	d       contract.Diagnostic
}

// diagnosticRows scans (ordinal, payload) rows; the generic read helper
// scans the payload alone.
func diagnosticRows(ctx context.Context, db queryer, q string, args ...any) ([]ordinalDiagnostic, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("querying diagnostics: %w", err)
	}
	defer rows.Close()
	out := []ordinalDiagnostic{}
	for rows.Next() {
		var r ordinalDiagnostic
		var data string
		if err := rows.Scan(&r.ordinal, &data); err != nil {
			return nil, fmt.Errorf("reading diagnostic row: %w", err)
		}
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&r.d); err != nil {
			return nil, fmt.Errorf("decoding stored diagnostic: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading diagnostic rows: %w", err)
	}
	return out, nil
}

// Runs retains import provenance across replacement snapshots, with each
// run's settings record (empty when it has none).
func (s *DB) Runs(ctx context.Context, repo string) ([]store.Run, error) {
	result := []store.Run{}
	rows, err := s.db.QueryContext(ctx, "SELECT payload, settings FROM index_runs WHERE repository=$1 ORDER BY id", repo)
	if err != nil {
		return nil, fmt.Errorf("querying runs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		var settings sql.NullString
		if err := rows.Scan(&data, &settings); err != nil {
			return nil, fmt.Errorf("reading run rows: %w", err)
		}
		var run store.Run
		if err := decodeStored(data, &run); err != nil {
			return nil, err
		}
		run.Settings = settings.String
		result = append(result, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading run rows: %w", err)
	}
	return result, nil
}

var _ store.Store = (*DB)(nil)

// FactCache returns extraction bytes only when both source and extractor match.
func (s *DB) FactCache(ctx context.Context, repo, path, hash, version string) ([]byte, error) {
	var data string
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM fact_cache WHERE repository=$1 AND path=$2 AND content_hash=$3 AND version=$4", repo, path, hash, version).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading fact cache: %w", err)
	}
	return []byte(data), nil
}

// SaveFactCache bounds retained revisions to the most recent extraction per path.
func (s *DB) SaveFactCache(ctx context.Context, repo, path, hash, version string, data []byte) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO fact_cache(repository,path,content_hash,version,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(repository,path) DO UPDATE SET content_hash=excluded.content_hash,version=excluded.version,payload=excluded.payload", repo, path, hash, version, string(data))
	if err != nil {
		return fmt.Errorf("saving fact cache: %w", err)
	}
	return nil
}

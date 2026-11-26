package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
)

const schemaFixture = "../../testdata/fixtures/sql-migrations"

func schemaRecordKinds(snapshot store.Snapshot) map[string]int {
	counts := map[string]int{}
	for _, n := range snapshot.Nodes {
		switch n.Kind {
		case "migration", "table", "column", "db_index":
			counts[n.Kind]++
		}
	}
	for _, e := range snapshot.Edges {
		if e.Kind == "alters" || e.Kind == "foreign_key" {
			counts[e.Kind]++
		}
	}
	return counts
}

// TestSchemaSnapshotParity round-trips the schema fixture through both
// backends (Postgres only where GRAPHVEIN_TEST_POSTGRES_URL is set, in CI).
func TestSchemaSnapshotParity(t *testing.T) {
	data, err := os.ReadFile("../../testdata/fixtures/sql-migrations.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected store.Snapshot
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	want := schemaRecordKinds(expected)
	if want["table"] == 0 || want["column"] == 0 || want["foreign_key"] == 0 {
		t.Fatal("schema golden must contain tables, columns and foreign keys")
	}
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(schemaFixture)); err != nil {
			t.Fatal(err)
		}
		// The non-UTF-8 member of the golden run (Revision 1 R5).
		if err := os.WriteFile(filepath.Join(root, "pg-unreadable/migrations/2_unreadable.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Every member of pg-all-unreadable is unreadable (T018b1 review).
		if err := os.WriteFile(filepath.Join(root, "pg-all-unreadable/migrations/1_only.sql"), []byte("CREATE TABLE \xff (id integer);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		emit := func() store.Writes {
			t.Helper()
			var stream bytes.Buffer
			if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture"}); err != nil {
				t.Fatal(err)
			}
			writes, err := ingest.StreamWithWrites(ctx, &stream, s)
			if err != nil {
				t.Fatal(err)
			}
			return writes
		}
		emit()
		out, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if got := schemaRecordKinds(out); !reflect.DeepEqual(got, want) {
			t.Fatalf("schema records %v, want %v", got, want)
		}
		if w := emit(); w.Files+w.Nodes+w.Edges+w.Deleted != 0 {
			t.Fatal("warm writes", w)
		}
		file := filepath.Join(root, "pg-serial/migrations/1_serials.sql")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(strings.Replace(string(data), "h integer DEFAULT 0", "h integer DEFAULT 7", 1)), 0o644); err != nil {
			t.Fatal(err)
		}
		emit()
		out, err = s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, n := range out.Nodes {
			if n.Kind == "column" && n.QualifiedName == "counters.h" {
				found = n.Attributes["default"] == "7"
			}
		}
		if !found {
			t.Fatal("migration edit did not propagate through persistence")
		}
	})
}

// postgresSkipped lists the pg-app migrations the Postgres check does not
// apply: down migrations are never applied by sqlx, 8 holds statements
// Postgres rejects on purpose (an unparseable ALTER and a public.-qualified
// name that does not exist in the test schema) and 9 ends in an
// unterminated dollar quote. Everything else, including the opaque (6) and
// unsupported (7) statements, is valid Postgres and is applied.
var postgresSkipped = map[string]bool{
	"8_unreadable_alter.sql": true,
	"9_unterminated.sql":     true,
	"10_reversible.down.sql": true,
	"README.md":              true,
	"nested":                 true,
}

// TestSchemaPostgresSemantics (B6) applies the fixture's pg-app migrations
// in sqlx order to a fresh Postgres schema and compares the catalog with
// the extracted records: tables, column order and nullability, primary
// keys, foreign-key column pairs and indexes. It runs only where
// GRAPHVEIN_TEST_POSTGRES_URL is set (CI's postgres job); it never starts a
// database.
func TestSchemaPostgresSemantics(t *testing.T) {
	url := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
	if url == "" {
		postgresUnavailable(t, url, false)
	}
	ctx := context.Background()
	admin, err := egress.OpenPostgres(url, nil)
	if err != nil {
		t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
	}
	// Close through t.Cleanup, not defer: cleanups run after deferred calls,
	// and the DROP SCHEMA cleanup registered below still needs admin.
	t.Cleanup(func() { admin.Close() })
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	pingErr := admin.PingContext(pingCtx)
	cancel()
	if pingErr != nil {
		postgresUnavailable(t, url, false)
	}

	extracted := extractPgApp(t)

	schema := "graphvein_schema_" + strings.ToLower(rand.Text())
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	db, err := egress.OpenPostgres(url, map[string]string{"search_path": schema})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	dir := filepath.Join(schemaFixture, "pg-app", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	type file struct {
		version int64
		name    string
	}
	var files []file
	for _, e := range entries {
		if postgresSkipped[e.Name()] {
			continue
		}
		var v int64
		if _, err := fmt.Sscanf(e.Name(), "%d_", &v); err != nil {
			t.Fatalf("unexpected fixture entry %s", e.Name())
		}
		files = append(files, file{v, e.Name()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			t.Fatal(err)
		}
		// One transaction per migration and one multi-statement simple
		// query, as sqlx-postgres runs it (migrate.rs apply/execute_migration).
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, string(data)); err != nil {
			tx.Rollback()
			t.Fatalf("applying %s: %v", f.name, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	actual := catalogSchema(t, ctx, db, schema)
	if !reflect.DeepEqual(actual.tables, extracted.tables) {
		t.Fatalf("tables: postgres %v, extracted %v", actual.tables, extracted.tables)
	}
	for _, table := range actual.tables {
		if !columnsMatch(actual.columns[table], extracted.columns[table]) {
			t.Errorf("%s columns: postgres %v, extracted %v", table, actual.columns[table], extracted.columns[table])
		}
		if !reflect.DeepEqual(actual.primaryKeys[table], extracted.primaryKeys[table]) {
			t.Errorf("%s primary key: postgres %v, extracted %v", table, actual.primaryKeys[table], extracted.primaryKeys[table])
		}
		if len(actual.indexes[table]) != len(extracted.indexes[table]) {
			t.Errorf("%s indexes: postgres %v, extracted %v", table, actual.indexes[table], extracted.indexes[table])
		}
		for _, name := range extracted.indexes[table] {
			if name != "" && !slices.Contains(actual.indexes[table], name) {
				t.Errorf("%s index %s is not in Postgres (%v)", table, name, actual.indexes[table])
			}
		}
	}
	if !reflect.DeepEqual(actual.foreignKeys, extracted.foreignKeys) {
		t.Errorf("foreign keys: postgres %v, extracted %v", actual.foreignKeys, extracted.foreignKeys)
	}
}

// columnsMatch compares "name:nullable" lists; an extracted "name:?"
// (nullability unknown) matches either catalog value.
func columnsMatch(actual, extracted []string) bool {
	if len(actual) != len(extracted) {
		return false
	}
	for i, e := range extracted {
		if name, ok := strings.CutSuffix(e, ":?"); ok {
			if a, _, _ := strings.Cut(actual[i], ":"); a == name {
				continue
			}
		}
		if actual[i] != e {
			return false
		}
	}
	return true
}

// schemaFacts is the comparable part of a schema: tables sorted, columns
// as "name:nullable" in position order, primary key columns in key order,
// index names per table ("" for unnamed in the extracted records) and
// foreign-key pairs "table.column->table.column", sorted.
type schemaFacts struct {
	tables      []string
	columns     map[string][]string
	primaryKeys map[string][]string
	indexes     map[string][]string
	foreignKeys []string
}

func newFacts() schemaFacts {
	return schemaFacts{columns: map[string][]string{}, primaryKeys: map[string][]string{}, indexes: map[string][]string{}, foreignKeys: []string{}}
}

// extractPgApp indexes the fixture and returns the pg-app records whose
// creating migration the Postgres check applies.
func extractPgApp(t *testing.T) schemaFacts {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(schemaFixture)); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stream bytes.Buffer
	if _, err := index.Emit(ctx, root, s, &stream, index.Options{Repository: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.StreamWithWrites(ctx, &stream, s); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.Dump(ctx, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	facts := newFacts()
	type col struct {
		table, name string
		position    int
		nullable    *bool
		pk          bool
	}
	tableNames := map[string]string{}
	columns := map[string]col{}
	var cols []col
	applied := func(file string) bool {
		return strings.HasPrefix(file, "pg-app/migrations/") && !postgresSkipped[strings.TrimPrefix(file, "pg-app/migrations/")]
	}
	for _, n := range snapshot.Nodes {
		if n.Kind == "table" && applied(n.File) {
			name := n.Attributes["name"].(string)
			tableNames[n.QualifiedName] = name
			facts.tables = append(facts.tables, name)
			if pk, ok := n.Attributes["primary_key"].([]any); ok {
				for _, c := range pk {
					facts.primaryKeys[name] = append(facts.primaryKeys[name], c.(string))
				}
			}
		}
	}
	sort.Strings(facts.tables)
	for _, n := range snapshot.Nodes {
		if n.Kind != "column" && n.Kind != "db_index" || !strings.HasPrefix(n.File, "pg-app/") {
			continue
		}
		owner := ""
		for qn, name := range tableNames {
			if strings.HasPrefix(n.QualifiedName, qn+".") {
				owner = name
			}
		}
		if owner == "" {
			continue
		}
		if n.Kind == "db_index" {
			name, _ := n.Attributes["name"].(string)
			facts.indexes[owner] = append(facts.indexes[owner], name)
			continue
		}
		// nullable is null (unknown) on partial tables (T018a Revision 3
		// N2); such a column matches either catalog value.
		c := col{table: owner, name: n.Attributes["name"].(string), position: position(n.Attributes["position"])}
		if v, ok := n.Attributes["nullable"].(bool); ok {
			c.nullable = &v
		}
		columns[n.Identity] = c
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		if cols[i].table != cols[j].table {
			return cols[i].table < cols[j].table
		}
		return cols[i].position < cols[j].position
	})
	for _, c := range cols {
		nullable := "?"
		if c.nullable != nil {
			nullable = fmt.Sprint(*c.nullable)
		}
		facts.columns[c.table] = append(facts.columns[c.table], c.name+":"+nullable)
	}
	for _, e := range snapshot.Edges {
		if e.Kind != "foreign_key" {
			continue
		}
		from, okFrom := columns[e.Source]
		to, okTo := columns[e.Target]
		if okFrom && okTo {
			facts.foreignKeys = append(facts.foreignKeys, from.table+"."+from.name+"->"+to.table+"."+to.name)
		} else if _, file, _, _ := contract.ParseIdentity(e.Source); applied(file) {
			t.Fatalf("foreign key %s -> %s outside the compared tables", e.Source, e.Target)
		}
	}
	sort.Strings(facts.foreignKeys)
	return facts
}

// position reads a decoded integer attribute (json.Number from the store).
func position(v any) int {
	var p int
	fmt.Sscan(fmt.Sprint(v), &p)
	return p
}

func catalogSchema(t *testing.T, ctx context.Context, db *sql.DB, schema string) schemaFacts {
	t.Helper()
	facts := newFacts()
	query := func(q string, scan func(*sql.Rows) error) {
		t.Helper()
		rows, err := db.QueryContext(ctx, q, schema)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				t.Fatal(err)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
	}
	query(`SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') ORDER BY c.relname`, func(r *sql.Rows) error {
		var name string
		err := r.Scan(&name)
		facts.tables = append(facts.tables, name)
		return err
	})
	sort.Strings(facts.tables)
	// attnum order without dropped columns is the extracted position order.
	query(`SELECT c.relname, a.attname, NOT a.attnotnull FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY c.relname, a.attnum`, func(r *sql.Rows) error {
		var table, name string
		var nullable bool
		err := r.Scan(&table, &name, &nullable)
		facts.columns[table] = append(facts.columns[table], fmt.Sprintf("%s:%v", name, nullable))
		return err
	})
	query(`SELECT c.relname, a.attname FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		WHERE n.nspname = $1 AND con.contype = 'p' ORDER BY c.relname, k.ord`, func(r *sql.Rows) error {
		var table, name string
		err := r.Scan(&table, &name)
		facts.primaryKeys[table] = append(facts.primaryKeys[table], name)
		return err
	})
	query(`SELECT t.relname, i.relname FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid JOIN pg_class t ON t.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1 AND NOT EXISTS (
			SELECT 1 FROM pg_constraint c WHERE c.conindid = x.indexrelid AND c.conrelid = x.indrelid AND c.contype IN ('p', 'u', 'x'))
		ORDER BY t.relname, i.relname`, func(r *sql.Rows) error {
		var table, name string
		err := r.Scan(&table, &name)
		facts.indexes[table] = append(facts.indexes[table], name)
		return err
	})
	query(`SELECT c.relname, la.attname, rc.relname, ra.attname FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class rc ON rc.oid = con.confrelid
		CROSS JOIN LATERAL unnest(con.conkey, con.confkey) AS k(l, r)
		JOIN pg_attribute la ON la.attrelid = con.conrelid AND la.attnum = k.l
		JOIN pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = k.r
		WHERE n.nspname = $1 AND con.contype = 'f'`, func(r *sql.Rows) error {
		var table, column, refTable, refColumn string
		err := r.Scan(&table, &column, &refTable, &refColumn)
		facts.foreignKeys = append(facts.foreignKeys, table+"."+column+"->"+refTable+"."+refColumn)
		return err
	})
	sort.Strings(facts.foreignKeys)
	return facts
}

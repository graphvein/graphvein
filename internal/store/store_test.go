package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/migrations"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

func TestProductionFixtureAndDiff(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/repos/typescript")); err != nil {
			t.Fatal(err)
		}
		for _, names := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
			if err := os.Rename(filepath.Join(root, names[0]), filepath.Join(root, names[1])); err != nil {
				t.Fatal(err)
			}
		}
		emit := func() []byte {
			t.Helper()
			var stream bytes.Buffer
			if _, err := index.Emit(ctx, root, s, &stream, opt); err != nil {
				t.Fatal(err)
			}
			return stream.Bytes()
		}
		data := emit()
		if err := ingest.Stream(ctx, bytes.NewReader(data), s); err != nil {
			t.Fatal(err)
		}
		snapshot, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "../../testdata/repos/typescript.store.golden.json", snapshot)
		// Database triggers detect accidental UPDATEs, even if the final dump is identical.
		if strings.HasSuffix(t.Name(), "/sqlite") {
			for _, table := range []string{"files", "nodes", "edges"} {
				for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
					if _, err := db.Exec("CREATE TRIGGER unchanged_" + strings.ToLower(operation) + "_" + table + " BEFORE " + operation + " ON " + table + " BEGIN SELECT RAISE(ABORT, 'unchanged row rewritten'); END"); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		writes, err := ingest.StreamWithWrites(ctx, bytes.NewReader(emit()), s)
		if err != nil || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
			t.Fatal(writes, err)
		}
		cached, err := s.FactCache(ctx, "fixture", "missing.ts", "hash", "version")
		if err != nil || cached != nil {
			t.Fatal(cached, err)
		}
		if err := s.SaveFactCache(ctx, "other", "file.ts", "hash", "version", []byte("facts")); err != nil {
			t.Fatal(err)
		}
		cached, err = s.FactCache(ctx, "other", "file.ts", "hash", "version")
		if err != nil || string(cached) != "facts" {
			t.Fatal(cached, err)
		}
		cached, err = s.FactCache(ctx, "other", "file.ts", "hash", "changed-version")
		if err != nil || cached != nil {
			t.Fatal(cached, err)
		}
	})
}

func TestIncrementalAcrossBackends(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		a := filepath.Join(root, "a.ts")
		b := filepath.Join(root, "b.ts")
		write := func(p, source string) {
			t.Helper()
			if err := os.WriteFile(p, []byte(source), 0644); err != nil {
				t.Fatal(err)
			}
		}
		write(a, "export function f() { return 1; }\n")
		write(b, "import { f } from './a'; export function caller() { return f(); }\n")
		opt := index.Options{Repository: "incremental", GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}
		run := func() (index.Summary, store.Writes) {
			t.Helper()
			var stream bytes.Buffer
			summary, err := index.Emit(ctx, root, s, &stream, opt)
			if err != nil {
				t.Fatal(err)
			}
			writes, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s)
			if err != nil {
				t.Fatal(err)
			}
			return summary, writes
		}
		run()
		write(a, "export function f() { return 2; }\n")
		summary, writes := run()
		if summary.Parsed != 1 || summary.Reused != 1 || writes.Files != 1 || writes.Nodes != 2 || writes.Edges != 0 || writes.Deleted != 0 {
			t.Fatal(summary, writes)
		}
		incremental, err := s.Dump(ctx, "incremental")
		if err != nil {
			t.Fatal(err)
		}
		opt.Repository = "full"
		run()
		full, err := s.Dump(ctx, "full")
		if err != nil || !reflect.DeepEqual(incremental, full) {
			t.Fatal("full/incremental mismatch", err)
		}
		opt.Repository = "incremental"
		write(a, "export function renamed() { return 2; }\n")
		summary, writes = run()
		if summary.Parsed != 1 || summary.Reused != 1 || writes.Deleted == 0 {
			t.Fatal(summary, writes)
		}
		snapshot, err := s.Dump(ctx, "incremental")
		if err != nil {
			t.Fatal(err)
		}
		for _, edge := range snapshot.Edges {
			if edge.Kind == "calls" {
				t.Fatal("stale cross-file edge survived")
			}
		}
	})
}

func backends(t *testing.T, test func(*testing.T, store.Store, *sql.DB)) {
	t.Helper()
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var db *sql.DB
			if backend == "sqlite" {
				path := filepath.Join(t.TempDir(), "index.db")
				s, err := sqlite.Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				db, err = sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				db.SetMaxOpenConns(1)
				if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
					t.Fatal(err)
				}
			} else {
				url := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
				if url == "" {
					postgresUnavailable(t, url, false)
				}
				admin, err := egress.OpenPostgres(url, nil)
				if err != nil {
					t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
				}
				pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				pingErr := admin.PingContext(pingCtx)
				cancel()
				if pingErr != nil {
					admin.Close()
					postgresUnavailable(t, url, false)
				}
				schema := "graphvein_test_" + strings.ToLower(rand.Text())
				if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
					admin.Close()
					t.Fatal(err)
				}
				t.Cleanup(func() {
					defer admin.Close()
					if _, err := admin.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
						t.Error(err)
					}
				})
				db, err = egress.OpenPostgres(url, map[string]string{"search_path": schema})
				if err != nil {
					t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
				}
				db.SetMaxOpenConns(1)
			}
			s, err := sqlstore.Open(ctx, db, backend)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			test(t, s, db)
		})
	}
}

// TestBackendEgressRouting proves storage connections go through egress:
// SQLite opens no network connection at all, and Postgres dials only the
// configured database destination. The observer is started before the
// shared helper opens each backend, so its connections are included.
func TestBackendEgressRouting(t *testing.T) {
	var mu sync.Mutex
	var events []egress.Event
	stop := egress.Observe(func(e egress.Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	defer stop()
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		if err := db.PingContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		got := events
		events = nil
		mu.Unlock()
		if path.Base(t.Name()) == "sqlite" {
			if len(got) != 0 {
				t.Fatalf("SQLite produced egress events: %+v", got)
			}
			return
		}
		dials := 0
		for _, e := range got {
			if !e.Allowed || e.Purpose != egress.Database {
				t.Fatalf("unexpected egress event: %+v", e)
			}
			if e.Op == "dial" {
				dials++
			}
		}
		if dials == 0 {
			t.Fatalf("Postgres connection did not dial through egress: %+v", got)
		}
	})
}

// The decision is independently testable without a database or network call.
func postgresDecision(required, url string, reachable bool) string {
	if url != "" && reachable {
		return "run"
	}
	if required == "1" {
		return "fail"
	}
	return "skip"
}

func postgresUnavailable(t *testing.T, url string, reachable bool) {
	t.Helper()
	message := "Postgres tests unavailable: GRAPHVEIN_TEST_POSTGRES_URL is unset or unreachable"
	if postgresDecision(os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES"), url, reachable) == "fail" {
		t.Fatal(message + "; GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
	}
	t.Skip(message)
}

func TestPostgresRequirementDecision(t *testing.T) {
	for _, tc := range []struct {
		required, url string
		reachable     bool
		want          string
	}{
		{"1", "", false, "fail"}, {"1", "configured", false, "fail"}, {"1", "configured", true, "run"},
		{"", "", false, "skip"}, {"", "configured", false, "skip"}, {"", "configured", true, "run"},
		{"0", "", false, "skip"},
	} {
		if got := postgresDecision(tc.required, tc.url, tc.reachable); got != tc.want {
			t.Fatalf("decision(%q, %q, %v) = %q, want %q", tc.required, tc.url, tc.reachable, got, tc.want)
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../testdata/contract/" + name + ".ndjson")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func importFixture(t *testing.T, s store.Store, name string) {
	t.Helper()
	if err := ingest.Stream(context.Background(), bytes.NewReader(fixture(t, name)), s); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenRoundTrips(t *testing.T) {
	for _, name := range []string{"valid", "diagnostic", "version-b", "invalid-identity", "malformed-body", "unknown-fields"} {
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
				importFixture(t, s, name)
				dump, err := s.Dump(context.Background(), "fixture")
				if err != nil {
					t.Fatal(err)
				}
				golden.Check(t, "../../testdata/store/"+name+".golden.json", dump)
			})
		})
	}
}

func TestReingestAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		importFixture(t, s, "valid")
		importFixture(t, s, "valid")
		importFixture(t, s, "version-b")
		dump, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "../../testdata/store/version-b.golden.json", dump)
		for _, name := range []string{"truncated", "unknown-version"} {
			if err := ingest.Stream(ctx, bytes.NewReader(fixture(t, name)), s); err == nil {
				t.Fatal("invalid framing was accepted")
			}
			after, err := s.Dump(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, dump) {
				t.Fatal("failed stream changed stored facts")
			}
		}
		runs, err := s.Runs(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 3 {
			t.Fatalf("runs = %d, want 3", len(runs))
		}
		for _, run := range runs {
			if run.ID == "" || run.Header.ContractVersion != 1 || run.Header.Indexer.Name != "fixture" || run.Header.Commit != "abc123" {
				t.Fatalf("missing metadata: %#v", run)
			}
			start, err := time.Parse(time.RFC3339Nano, run.StartedAt)
			if err != nil {
				t.Fatal(err)
			}
			end, err := time.Parse(time.RFC3339Nano, run.FinishedAt)
			if err != nil || end.Before(start) {
				t.Fatal("invalid run timestamps")
			}
		}
		// A second repository with the same node identities must survive replacement.
		other := strings.Replace(string(fixture(t, "valid")), `"repository":"fixture"`, `"repository":"other"`, 1)
		if err := ingest.Stream(ctx, strings.NewReader(other), s); err != nil {
			t.Fatal(err)
		}
		before, err := s.Dump(ctx, "other")
		if err != nil {
			t.Fatal(err)
		}
		importFixture(t, s, "version-b")
		after, err := s.Dump(ctx, "other")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("repository isolation failed")
		}
	})
}

func TestQueriesAndDeletion(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		importFixture(t, s, "valid")
		level := 4
		cases := []struct {
			q     store.Query
			count int
		}{
			{store.Query{Identity: "function:src/a.ts#a"}, 1},
			{store.Query{File: "src/b.py"}, 1},
			{store.Query{Kind: "function", Level: &level}, 2},
			{store.Query{File: "."}, 1},
			{store.Query{Identity: "missing"}, 0},
		}
		for _, tc := range cases {
			nodes, err := s.Nodes(ctx, "fixture", tc.q)
			if err != nil || len(nodes) != tc.count {
				t.Fatalf("Nodes(%#v) = %d, %v", tc.q, len(nodes), err)
			}
		}
		for _, tc := range []struct {
			id, kind string
			dir      store.Direction
			want     []string
		}{
			{"function:src/a.ts#a", "calls", store.Outgoing, []string{"function:src/b.py#b"}},
			{"function:src/b.py#b", "calls", store.Incoming, []string{"function:src/a.ts#a"}},
			{"function:src/a.ts#a", "", store.Outgoing, []string{"external:.#npm%3Areact", "function:src/b.py#b"}},
		} {
			nodes, err := s.Neighbours(ctx, "fixture", tc.id, tc.kind, tc.dir)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, n := range nodes {
				ids = append(ids, n.Identity)
			}
			if !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("neighbours = %v, want %v", ids, tc.want)
			}
		}
		if _, err := s.Neighbours(ctx, "fixture", "x", "", store.Direction("sideways")); err == nil {
			t.Fatal("invalid direction accepted")
		}
		// Rebuild via the transaction API to exercise explicit deletion and upserts.
		header := contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"}
		tx, err := s.BeginRun(ctx, header)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		reader := contract.NewReader(bytes.NewReader(fixture(t, "valid")))
		for {
			event, err := reader.Next()
			if err != nil {
				break
			}
			switch r := event.Record.(type) {
			case *contract.File:
				err = tx.UpsertFile(ctx, *r)
			case *contract.Node:
				err = tx.UpsertNode(ctx, *r)
			case *contract.Edge:
				err = tx.UpsertEdge(ctx, *r)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		// Edges backed by a third file must also disappear when that file is deleted.
		third := contract.File{Type: "file", Path: "evidence.ts", Language: "typescript", Precision: "precise", ContentHash: contract.ContentHash(nil)}
		if err := tx.UpsertFile(ctx, third); err != nil {
			t.Fatal(err)
		}
		e := contract.Edge{Type: "edge", Kind: "references", Source: "function:src/a.ts#a", Target: "external:.#npm%3Areact", Provenance: "inferred", Confidence: .8, Evidence: []contract.Evidence{{File: third.Path, Span: contract.Span{StartLine: 1, EndLine: 1}}}}
		if err := tx.UpsertEdge(ctx, e); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteFile(ctx, third.Path); err != nil {
			t.Fatal(err)
		}
		if err := tx.Diagnostic(ctx, contract.Diagnostic{Type: "diagnostic", File: "src/b.py", Severity: "warning", Code: "test", Message: "removed with file"}); err != nil {
			t.Fatal(err)
		}
		if err := tx.DeleteFile(ctx, "src/b.py"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if len(dump.Nodes) != 2 || len(dump.Edges) != 1 || dump.Edges[0].Kind != "imports" || len(dump.Diagnostics) != 0 {
			t.Fatalf("orphaned facts: %#v", dump)
		}
	})
}

func TestChangedFileReplacement(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		reader := contract.NewReader(bytes.NewReader(fixture(t, "valid")))
		var file contract.File
		for {
			event, err := reader.Next()
			if err != nil {
				break
			}
			switch r := event.Record.(type) {
			case *contract.File:
				err = tx.UpsertFile(ctx, *r)
				if r.Path == "src/a.ts" {
					file = *r
				}
			case *contract.Node:
				err = tx.UpsertNode(ctx, *r)
			case *contract.Edge:
				err = tx.UpsertEdge(ctx, *r)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		file.ContentHash = contract.ContentHash([]byte("changed"))
		file.Size = 7
		if err := tx.UpsertFile(ctx, file); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if len(dump.Files) != 2 || len(dump.Nodes) != 2 || len(dump.Edges) != 0 {
			t.Fatalf("changed file retained stale graph: %#v", dump)
		}
		nodes, err := s.Nodes(ctx, "fixture", store.Query{File: file.Path})
		if err != nil || len(nodes) != 0 {
			t.Fatal("changed file retained old symbols", err)
		}
	})
}

func TestPortableOrdering(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		tx, err := s.BeginRun(ctx, contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "test", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, path := range []string{"é.go", "Z.go", "a.go", "A.go"} {
			if err := tx.UpsertFile(ctx, contract.File{Type: "file", Path: path, Language: "go", Precision: "best_effort", ContentHash: contract.ContentHash(nil)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		paths := []string{}
		for _, f := range dump.Files {
			paths = append(paths, f.Path)
		}
		if !reflect.DeepEqual(paths, []string{"A.go", "Z.go", "a.go", "é.go"}) {
			t.Fatalf("locale-dependent ordering: %v", paths)
		}
	})
}

func TestRecoverableRecords(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		for _, name := range []string{"invalid-identity", "malformed-body", "unknown-fields"} {
			importFixture(t, s, name)
			dump, err := s.Dump(context.Background(), "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if len(dump.Nodes) != 1 || len(dump.Diagnostics) == 0 {
				t.Fatalf("%s did not isolate bad records: %#v", name, dump)
			}
		}
		data := strings.Replace(string(fixture(t, "valid")), `"target":"function:src/b.py#b"`, `"target":"function:missing.py#missing"`, 1)
		if err := ingest.Stream(context.Background(), strings.NewReader(data), s); err != nil {
			t.Fatal(err)
		}
		dump, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		if len(dump.Edges) != 1 || len(dump.Diagnostics) != 1 || dump.Diagnostics[0].Code != "invalid_reference" {
			t.Fatal("unresolved edge was not skipped")
		}
	})
}

func TestBatchesAndCancellation(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		importFixture(t, s, "valid")
		var stream bytes.Buffer
		writer := contract.NewWriter(&stream)
		header := contract.Header{Type: "header", ContractVersion: 1, Indexer: contract.Indexer{Name: "batch", Version: "1"}, Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z"}
		if err := writer.Write(header); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < ingest.BatchSize*2+1; i++ {
			if err := writer.Write(contract.File{Type: "file", Path: fmt.Sprintf("f%03d.go", i), Language: "go", Precision: "best_effort", ContentHash: contract.ContentHash(nil)}); err != nil {
				t.Fatal(err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		data := stream.Bytes()
		cut := bytes.LastIndex(data, []byte(`{"type":"footer"`))
		if err := ingest.Stream(ctx, bytes.NewReader(data[:cut]), s); err == nil {
			t.Fatal("accepted truncated batches")
		}
		dump, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "../../testdata/store/valid.golden.json", dump)
		if err := ingest.Stream(ctx, bytes.NewReader(data), s); err != nil {
			t.Fatal(err)
		}
		dump, err = s.Dump(ctx, "fixture")
		if err != nil || len(dump.Files) != ingest.BatchSize*2+1 {
			t.Fatal("batch import failed", err)
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := ingest.Stream(canceled, bytes.NewReader(data), s); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
	})
}

func TestMigrationVersions(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, db *sql.DB) {
		ctx := context.Background()
		var version int
		if err := db.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version); err != nil || version != migrations.Version {
			t.Fatal("migration version", version, err)
		}
		// Reopening an already migrated database must be idempotent.
		dialect := "sqlite"
		if strings.HasSuffix(t.Name(), "/postgres") {
			dialect = "postgres"
		}
		reopened, err := sqlstore.Open(ctx, db, dialect)
		if err != nil {
			t.Fatal(err)
		}
		_ = reopened
		// Reconstruct the T004 version-1 schema and verify the cache migration
		// preserves its graph, rather than testing only empty-database creation.
		importFixture(t, s, "valid")
		dropChangeTables(t, db)
		for _, q := range []string{"DROP TABLE fact_cache", "DROP INDEX nodes_names", "ALTER TABLE nodes DROP COLUMN name", "ALTER TABLE nodes DROP COLUMN qualified_name"} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.ExecContext(ctx, "UPDATE schema_version SET version=1"); err != nil {
			t.Fatal(err)
		}
		upgraded, err := sqlstore.Open(ctx, db, dialect)
		if err != nil {
			t.Fatal(err)
		}
		dump, err := upgraded.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "../../testdata/store/valid.golden.json", dump)
		if _, err := upgraded.FactCache(ctx, "fixture", "a.ts", "hash", "version"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("UPDATE schema_version SET version=%d", migrations.Version+1)); err != nil {
			t.Fatal(err)
		}
		if _, err := sqlstore.Open(ctx, db, dialect); err == nil || !strings.Contains(err.Error(), "newer than supported") {
			t.Fatalf("future schema = %v", err)
		}
	})
}

func TestAttributePrecision(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		data := strings.Replace(string(fixture(t, "version-b")), `"attributes":{}`, `"attributes":{"large":9007199254740993}`, 1)
		if err := ingest.Stream(context.Background(), strings.NewReader(data), s); err != nil {
			t.Fatal(err)
		}
		nodes, err := s.Nodes(context.Background(), "fixture", store.Query{Identity: "function:src/a.ts#a"})
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 1 || nodes[0].Attributes["large"] != json.Number("9007199254740993") {
			t.Fatal("source attributes lost numeric precision", nodes)
		}
	})
}

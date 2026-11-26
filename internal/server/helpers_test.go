package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// testHost is the Host header the handler tests send; the handlers they
// build allow exactly the hosts for port 7420.
const testHost = "127.0.0.1:7420"

var testHosts = []string{"127.0.0.1:7420", "localhost:7420", "[::1]:7420"}

// fixedNow makes checked_at deterministic in goldens.
func fixedNow() time.Time { return time.Date(2026, 11, 3, 14, 40, 0, 0, time.UTC) }

// backends runs check against a fresh SQLite store and, when
// GRAPHVEIN_TEST_POSTGRES_URL is set, a fresh Postgres schema. It follows
// internal/externalindex's helper; CI's postgres job requires the second.
// check receives two stores on the same data: write is migrated and
// read-write, for indexing; read is opened read-only the way graphvein serve
// opens it (sqlite.OpenRead, and postgres.OpenRead's settings with a test
// search_path), so the status checks cover the read-only path.
func backends(t *testing.T, check func(t *testing.T, write, read store.Store)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "index.db")
		s, err := sqlite.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		ro, err := sqlite.OpenRead(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		check(t, s, ro)
	})
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
		unavailable := func() {
			if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
				t.Fatal("GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
			}
			t.Skip("Postgres unavailable: GRAPHVEIN_TEST_POSTGRES_URL unset or unreachable")
		}
		if url == "" {
			unavailable()
		}
		admin, err := egress.OpenPostgres(url, nil)
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		defer admin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = admin.PingContext(ctx)
		cancel()
		if err != nil {
			unavailable()
		}
		schema := "graphvein_server_" + strings.ToLower(rand.Text())
		if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		}()
		db, err := egress.OpenPostgres(url, map[string]string{"search_path": schema})
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		s, err := sqlstore.Open(context.Background(), db, "postgres")
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		// postgres.OpenRead with the test schema: same runtime setting,
		// same connection limit, same schema check.
		rodb, err := egress.OpenPostgres(url, map[string]string{"search_path": schema, "default_transaction_read_only": "on"})
		if err != nil {
			t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
		}
		rodb.SetMaxOpenConns(1)
		ro, err := sqlstore.OpenRead(context.Background(), rodb, "postgres")
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		check(t, s, ro)
	})
}

// fixtureRoot copies the small Go fixture repository to a temporary
// directory, so tests can edit it.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/go")); err != nil {
		t.Fatal(err)
	}
	return root
}

// indexRoot indexes root into s as repository "fixture", the way
// graphvein index does.
func indexRoot(t *testing.T, root string, s store.Store) {
	t.Helper()
	indexRepository(t, root, s, "fixture")
}

// indexRepository indexes root into s as repository repo.
func indexRepository(t *testing.T, root string, s store.Store, repo string) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repo, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	summary, err := index.Emit(ctx, root, s, &stream, opt)
	if err != nil {
		t.Fatal(err)
	}
	// The run records the settings it read, as indexrun's runs do.
	if err := ingest.Stream(ctx, &stream, store.WithRunOptions(s, store.RunOptions{Settings: summary.Settings.Record()})); err != nil {
		t.Fatal(err)
	}
}

// testOptions builds handler options for root and s with a fixed clock and
// a one-file asset tree.
func testOptions(root string, s store.Store) Options {
	return Options{
		Root:       root,
		Repository: "fixture",
		Version:    "test",
		Store:      s,
		Assets:     fstest.MapFS{"index.html": {Data: []byte("<!doctype html><p>test</p>\n")}},
		Now:        fixedNow,
	}
}

// get sends one request through a handler built from opt.
func get(t *testing.T, opt Options, method, path string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Host = testHost
	for k, v := range header {
		if k == "Host" {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	NewHandler(opt, testHosts).ServeHTTP(rec, req)
	return rec
}

// decodeStatus decodes a status body into the documented struct, failing on
// unknown fields so the documented schema and the code cannot drift apart.
func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) statusResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var out statusResponse
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err, rec.Body.String())
	}
	return out
}

// failingStore wraps a store and fails the chosen reads.
type failingStore struct {
	store.Store
	runs, hashes, diagnostics bool
}

var errInjected = errors.New("injected failure")

func (f failingStore) Runs(ctx context.Context, repo string) ([]store.Run, error) {
	if f.runs {
		return nil, errInjected
	}
	return f.Store.Runs(ctx, repo)
}

func (f failingStore) FileHashes(ctx context.Context, repo string) (map[string]string, error) {
	if f.hashes {
		return nil, errInjected
	}
	return f.Store.FileHashes(ctx, repo)
}

func (f failingStore) Diagnostics(ctx context.Context, repo string, files []string) ([]contract.Diagnostic, error) {
	if f.diagnostics {
		return nil, errInjected
	}
	return f.Store.Diagnostics(ctx, repo, files)
}

func (f failingStore) DiagnosticsWithMessage(ctx context.Context, repo, code, text string) ([]contract.Diagnostic, error) {
	if f.diagnostics {
		return nil, errInjected
	}
	return f.Store.DiagnosticsWithMessage(ctx, repo, code, text)
}

func (f failingStore) DiagnosticsWithCodes(ctx context.Context, repo string, codes, prefixes []string) ([]contract.Diagnostic, error) {
	if f.diagnostics {
		return nil, errInjected
	}
	return f.Store.DiagnosticsWithCodes(ctx, repo, codes, prefixes)
}

func (f failingStore) DiagnosticsWithoutFile(ctx context.Context, repo string) ([]contract.Diagnostic, error) {
	if f.diagnostics {
		return nil, errInjected
	}
	return f.Store.DiagnosticsWithoutFile(ctx, repo)
}

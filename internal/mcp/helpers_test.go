package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
	"github.com/graphvein/graphvein/internal/store/sqlite"
	"github.com/graphvein/graphvein/internal/store/sqlstore"
)

// fixedNow makes checked_at deterministic in goldens.
func fixedNow() time.Time { return time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC) }

// backends runs check against a fresh SQLite store and, when
// GRAPHVEIN_TEST_POSTGRES_URL is set, a fresh Postgres schema (CI's
// postgres job requires the second), as internal/server's helper does:
// write is read-write for indexing, read is opened read-only the way
// graphvein mcp opens it.
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
		schema := "graphvein_mcp_" + strings.ToLower(rand.Text())
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

// indexRepository indexes root into s as repository repo, the way
// graphvein index does.
func indexRepository(t *testing.T, root string, s store.Store, repo string) {
	t.Helper()
	ctx := context.Background()
	var stream bytes.Buffer
	opt := index.Options{Repository: repo, Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
	summary, err := index.Emit(ctx, root, s, &stream, opt)
	if err != nil {
		t.Fatal(err)
	}
	// The run records the settings it read, as graphvein index's runs do
	// (the settings claim contract, ST-41).
	if err := ingest.Stream(ctx, &stream, store.WithRunOptions(s, store.RunOptions{Settings: summary.Settings.Record()})); err != nil {
		t.Fatal(err)
	}
}

// apiFixtureRoot copies testdata/fixtures/api-section and restores the
// inert names its README lists.
func apiFixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/api-section")); err != nil {
		t.Fatal(err)
	}
	renames := map[string]string{"_graphveinignore": ".graphveinignore"}
	for _, name := range []string{"app/Cargo.toml", "app/build.rs", "app/tauri.conf.json", "app/src/main.rs", "app/src/commands.rs"} {
		renames[name+".inert"] = name
	}
	for from, to := range renames {
		if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// session drives one MCP session over in-process pipes with raw JSON-RPC
// lines, as a client on stdio would.
type session struct {
	t      *testing.T
	in     io.WriteCloser
	out    *bufio.Reader
	done   chan error
	nextID int
}

func startSession(t *testing.T, opt Options) *session {
	t.Helper()
	s := New(opt)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := s.Serve(context.Background(), inR, outW)
		outW.Close()
		done <- err
	}()
	t.Cleanup(s.Close)
	return &session{t: t, in: inW, out: bufio.NewReaderSize(outR, 1<<20), done: done}
}

// call sends a request and returns its result (or error) object.
func (c *session) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	for {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			c.t.Fatalf("reading response to %s: %v", method, err)
		}
		var msg map[string]any
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.UseNumber()
		if err := dec.Decode(&msg); err != nil {
			c.t.Fatalf("decoding %s: %v", line, err)
		}
		if fmt.Sprint(msg["id"]) != fmt.Sprint(id) {
			continue // a notification
		}
		if e, ok := msg["error"]; ok {
			return map[string]any{"error": e}
		}
		res, _ := msg["result"].(map[string]any)
		return res
	}
}

func (c *session) notify(method string) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *session) send(v any) {
	c.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := c.in.Write(append(raw, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

// close ends the session as a client does, by closing stdin.
func (c *session) close() {
	c.t.Helper()
	c.in.Close()
	if err := <-c.done; err != nil {
		c.t.Fatalf("session ended with %v", err)
	}
}

func (c *session) initialize() map[string]any {
	c.t.Helper()
	res := c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "golden", "version": "1"}})
	c.notify("notifications/initialized")
	return res
}

// copyTreeTest replaces dst's contents (all but .git) with src's, as the
// change-impact fixture's own golden test moves a checkout between
// revisions.
func copyTreeTest(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(dst)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dst, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
}

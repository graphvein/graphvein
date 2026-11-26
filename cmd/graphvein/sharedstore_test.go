package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/egress"
	"github.com/graphvein/graphvein/internal/workspace"
)

// TestServeSharedStore checks WS-57 end to end on two copies of one
// fixture folder, both named "app". With SQLite each copy has its own
// store, so both are served. With Postgres (in a schema of its own) both
// copies' facts would live under one repository ID: serve of the second
// copy exits 1 before it registers anything, a copy registered by another
// process makes the running server refuse its own index with 409
// shared_store_conflict and list the conflict, and renaming one copy's
// repository in graphvein.yaml ends it.
func TestServeSharedStore(t *testing.T) {
	copies := func(t *testing.T) (string, string) {
		t.Helper()
		sc := t.TempDir()
		a, b := filepath.Join(sc, "a", "app"), filepath.Join(sc, "b", "app")
		for _, dir := range []string{a, b} {
			if err := os.CopyFS(dir, os.DirFS("../../testdata/repos/go")); err != nil {
				t.Fatal(err)
			}
		}
		return a, b
	}
	t.Run("sqlite", func(t *testing.T) {
		freshWorkspace(t)
		t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
		t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
		a, b := copies(t)
		for _, dir := range []string{a, b} {
			if code, out, errOut := runCLI("index", dir); code != 0 {
				t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
			}
		}
		startServe(t, a)
		if line := startServe(t, b); !strings.HasPrefix(line, "Serving app (+1 in the workspace) at ") {
			t.Fatalf("second copy with its own store: %q", line)
		}
	})
	t.Run("postgres", func(t *testing.T) {
		registry := freshWorkspace(t)
		t.Setenv("GRAPHVEIN_POSTGRES_URL", postgresSchemaURL(t, "graphvein_cli_shared_"))
		t.Setenv("GRAPHVEIN_SQLITE_PATH", "")
		a, b := copies(t)
		if code, out, errOut := runCLI("index", a); code != 0 {
			t.Fatalf("index = %d\n%s\n%s", code, out, errOut)
		}
		line := startServe(t, a)
		base := line[strings.LastIndex(line, " ")+1:]

		// The second copy is refused before it serves or registers.
		var out, errOut strings.Builder
		if code := serveCommand(context.Background(), []string{b, "--addr", "127.0.0.1:0"}, &out, &errOut); code != 1 {
			t.Fatalf("serve of the second copy = %d\n%s\n%s", code, out.String(), errOut.String())
		}
		msg := errOut.String()
		if !strings.Contains(msg, "shared_store_conflict") || !strings.Contains(msg, a) || !strings.Contains(msg, "graphvein.yaml") {
			t.Fatalf("refusal %q", msg)
		}
		entries, err := workspace.Open(registry).Entries()
		if err != nil || len(entries) != 1 {
			t.Fatalf("registry after the refusal: %+v %v", entries, err)
		}

		get := func(path string) (int, []byte) {
			t.Helper()
			resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(base + path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, data
		}
		if code, data := get("api/v0/status"); code != http.StatusOK {
			t.Fatalf("one copy registered: %d %s", code, data)
		}
		// Another process registers the second copy.
		resolved, err := workspace.Resolve(b)
		if err != nil {
			t.Fatal(err)
		}
		other, _, err := workspace.Open(registry).Add(workspace.Entry{Kind: workspace.KindRepository, Name: "app", Path: resolved})
		if err != nil {
			t.Fatal(err)
		}
		code, data := get("api/v0/status")
		if code != http.StatusConflict || !strings.Contains(string(data), `"shared_store_conflict"`) {
			t.Fatalf("open index after a second copy was registered: %d %s", code, data)
		}
		code, data = get("api/v0/workspace")
		var l struct {
			Entries []struct {
				ID        string `json:"id"`
				Freshness any    `json:"freshness"`
				Conflict  *struct {
					Code  string `json:"code"`
					Entry string `json:"entry"`
				} `json:"conflict"`
			} `json:"entries"`
		}
		if err := json.Unmarshal(data, &l); err != nil || code != http.StatusOK || len(l.Entries) != 2 {
			t.Fatalf("list %d %s %v", code, data, err)
		}
		for _, e := range l.Entries {
			if e.Conflict == nil || e.Conflict.Code != "shared_store_conflict" || e.Freshness != nil {
				t.Fatalf("entry %s: %+v", e.ID, e)
			}
			if e.ID != other.ID && e.Conflict.Entry != other.ID {
				t.Fatalf("entry %s names %s, want %s", e.ID, e.Conflict.Entry, other.ID)
			}
		}
		// Renaming the second copy's repository ends the conflict.
		if err := os.WriteFile(filepath.Join(b, "graphvein.yaml"), []byte("version: 1\nrepository: app-b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code, data := get("api/v0/status"); code != http.StatusOK {
			t.Fatalf("after the rename: %d %s", code, data)
		}
	})
}

// postgresSchemaURL returns GRAPHVEIN_TEST_POSTGRES_URL with a fresh schema
// of its own as the search path, dropped when the test ends; the test is
// skipped without a reachable database (failed with
// GRAPHVEIN_REQUIRE_POSTGRES=1).
func postgresSchemaURL(t *testing.T, prefix string) string {
	t.Helper()
	base := os.Getenv("GRAPHVEIN_TEST_POSTGRES_URL")
	unavailable := func() {
		if os.Getenv("GRAPHVEIN_REQUIRE_POSTGRES") == "1" {
			t.Fatal("GRAPHVEIN_REQUIRE_POSTGRES=1 requires a live test database")
		}
		t.Skip("Postgres unavailable: GRAPHVEIN_TEST_POSTGRES_URL unset or unreachable")
	}
	if base == "" {
		unavailable()
	}
	admin, err := egress.OpenPostgres(base, nil)
	if err != nil {
		t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = admin.PingContext(ctx)
	cancel()
	if err != nil {
		admin.Close()
		unavailable()
	}
	schema := prefix + strings.ToLower(rand.Text())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal("invalid GRAPHVEIN_TEST_POSTGRES_URL")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

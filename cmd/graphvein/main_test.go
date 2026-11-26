package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store/sqlite"
)

func TestRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"version", []string{"version"}, 0, "graphvein dev\ncommit: unknown\nbuild date: unknown\n"},
		{"help", []string{"help"}, 0, "Commands:\n  version"},
		{"default help", nil, 0, "Commands:"},
		{"help flag", []string{"--help"}, 0, "Commands:"},
		{"short help", []string{"-h"}, 0, "Commands:"},
		{"unknown", []string{"nonexistent"}, 2, "unknown command \"nonexistent\""},
		{"extra argument", []string{"version", "extra"}, 2, "usage:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := run(tc.args, &out, &errOut); code != tc.code {
				t.Fatalf("exit code = %d, want %d", code, tc.code)
			}
			got, empty := out.String(), errOut.String()
			if tc.code != 0 {
				got, empty = empty, got
			}
			if !strings.Contains(got, tc.want) || empty != "" {
				t.Fatalf("stdout = %q, stderr = %q", out.String(), errOut.String())
			}
		})
	}
}

func TestIndexAndStatusCommands(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, ".graphvein", "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", db)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	file := filepath.Join(root, "a.js")
	if err := os.WriteFile(file, []byte("export function f() { return 1; }"), 0644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"index", root, "--emit", filepath.Join(root, ".graphvein", "facts.ndjson")}, {"status", root}} {
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
	}
	if !strings.Contains(out.String(), "0 files changed") {
		t.Fatal(out.String())
	}
	if err := os.WriteFile(file, []byte("export function f() { return 2; }"), 0644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"status", root}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "1 files changed") {
		t.Fatal(code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run([]string{"index", root}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "Parsed: 1; reused: 0") {
		t.Fatal(code, out.String(), errOut.String())
	}
}

// TestStatusSettingsLine checks graphvein status's settings line (row
// ST-46): the --shape-depth flag's value is what the run used, a re-index
// without it records the file's, a comment edit changes nothing, and a
// run that recorded nothing (an ingest) is unknown.
func TestStatusSettingsLine(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte("export function f() { return 1; }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("version: 1\nrepository: fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status := func(want string) {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := run([]string{"status", root}, &out, &errOut); code != 0 || !strings.Contains(out.String(), want+"\n") {
			t.Fatalf("status = %d %q %s, want %q", code, out.String(), errOut.String(), want)
		}
	}
	index := func(args ...string) {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := run(append([]string{"index", root}, args...), &out, &errOut); code != 0 {
			t.Fatal(code, errOut.String())
		}
	}
	index("--shape-depth", "3")
	status("Settings changed since last index: index.shape_depth (re-index to apply)")
	index()
	status("Settings unchanged since last index")
	if err := os.WriteFile(filepath.Join(root, "graphvein.yaml"), []byte("# team file\nversion: 1\nrepository: fixture   # shared\nui:\n  labels: technical\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status("Settings unchanged since last index")
	snapshot := filepath.Join(t.TempDir(), "facts.ndjson")
	index("--emit", snapshot)
	var out, errOut bytes.Buffer
	if code := run([]string{"ingest", snapshot}, &out, &errOut); code != 0 {
		t.Fatal(code, errOut.String())
	}
	status("Settings since last index: unknown: the index run did not record its settings (an index from before settings were recorded, or from an external indexer)")
}

func TestIngestCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	t.Setenv("GRAPHVEIN_SQLITE_PATH", path)
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"ingest", "../../testdata/contract/valid.ndjson"}, &out, &errOut); code != 0 || errOut.Len() != 0 {
		t.Fatalf("ingest = %d, %s", code, errOut.String())
	}
	s, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	dump, err := s.Dump(context.Background(), "fixture")
	s.Close()
	if err != nil || len(dump.Nodes) != 3 {
		t.Fatalf("CLI did not persist fixture: %v", err)
	}
	for _, args := range [][]string{{"ingest"}, {"ingest", "a", "b"}} {
		out.Reset()
		errOut.Reset()
		if code := run(args, &out, &errOut); code != 2 {
			t.Fatalf("usage exit = %d", code)
		}
	}
	for _, file := range []string{"missing.ndjson", "../../testdata/contract/truncated.ndjson"} {
		out.Reset()
		errOut.Reset()
		if code := run([]string{"ingest", file}, &out, &errOut); code != 1 || errOut.Len() == 0 {
			t.Fatalf("failure exit = %d", code)
		}
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("SQLite override was ignored", err)
	}
}

func TestInjectedVersion(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = oldVersion, oldCommit, oldDate })
	version, commit, buildDate = "v0.1.0", "abc123", "2026-10-05T00:00:00Z"
	var out bytes.Buffer
	run([]string{"version"}, &out, &out)
	if got := out.String(); got != "graphvein v0.1.0\ncommit: abc123\nbuild date: 2026-10-05T00:00:00Z\n" {
		t.Fatalf("version output = %q", got)
	}
}

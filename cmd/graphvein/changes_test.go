package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/changes"
)

// TestChangesCommand runs graphvein changes on the change-impact fixture:
// the first run has no previous run, then rev2 compares with rev1, the JSON
// is the changes.Report shape, pin and unpin take the lock, impact prints
// the traversal, and the usage errors exit 2.
func TestChangesCommand(t *testing.T) {
	root := t.TempDir()
	revision := func(name string) {
		t.Helper()
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.Name() != ".graphvein" {
				os.RemoveAll(filepath.Join(root, e.Name()))
			}
		}
		if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "..", "testdata", "fixtures", "change-impact", name))); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	var stdout, stderr bytes.Buffer
	exec := func(want int, args ...string) string {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if code := run(args, &stdout, &stderr); code != want {
			t.Fatalf("graphvein %v = %d, want %d\n%s%s", args, code, want, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	revision("rev1")
	exec(0, "index", root, "--repository", "fixture")
	exec(1, "changes", root, "--repository", "fixture")
	if !strings.Contains(stderr.String(), "no previous run") {
		t.Errorf("first run: %s", stderr.String())
	}
	exec(0, "changes", "pin", "rev1", root, "--repository", "fixture")
	revision("rev2")
	exec(0, "index", root, "--repository", "fixture")
	var report changes.Report
	if err := json.Unmarshal([]byte(exec(0, "changes", root, "--repository", "fixture", "--json", "--tests")), &report); err != nil {
		t.Fatal(err)
	}
	if report.Base != "previous" || len(report.Items) != report.Summary.Total || report.Summary.Total == 0 {
		t.Errorf("report: base %s, %d items, total %d", report.Base, len(report.Items), report.Summary.Total)
	}
	text := exec(0, "changes", root, "--repository", "fixture", "--base", "pin:rev1", "--group", "routes")
	if !strings.Contains(text, "C3 removed route route:server/app.ts#DELETE%20%2Fapi%2Flegacy") {
		t.Errorf("routes against the pin:\n%s", text)
	}
	text = exec(0, "changes", "impact", "function:server/handlers.ts#listItems", root, "--repository", "fixture")
	if !strings.Contains(text, "I4 route:server/app.ts#GET%20%2Fapi%2Fitems depends on") {
		t.Errorf("impact:\n%s", text)
	}
	exec(1, "changes", "pin", "rev1", root, "--repository", "fixture")
	exec(0, "changes", "unpin", "rev1", root, "--repository", "fixture")
	exec(1, "changes", root, "--repository", "fixture", "--base", "pin:rev1")
	exec(2, "changes", root, "--repository", "fixture", "--base", "bogus")
	exec(2, "changes", "impact")
	exec(2, "changes", root, "extra", "--repository", "fixture")
}

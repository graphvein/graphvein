package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatusWithSkippedEntries is the regression test for graphvein
// status over a repository holding a binary, a non-UTF-8 file and (on
// Unix) a FIFO: the scanner's skip records do not make the scan
// incomplete, so status reports 0 changed files after an index and 1
// after an edit.
func TestStatusWithSkippedEntries(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	file := filepath.Join(root, "main.go")
	for name, data := range map[string]string{"main.go": "package main\n\nfunc main() {}\n", "logo.png": "\x89PNG\r\n\x1a\n\x00\x00", "latin1.txt": "caf\xe9\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	makeFIFO(filepath.Join(root, "pipe"))
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"index", root}, {"status", root}} {
		out.Reset()
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatal(args, code, errOut.String())
		}
	}
	if !strings.Contains(out.String(), "0 files changed") {
		t.Fatal(out.String(), errOut.String())
	}
	if err := os.WriteFile(file, []byte("package main\n\nfunc main() { main() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"status", root}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "1 files changed") {
		t.Fatal(code, out.String(), errOut.String())
	}
	// A genuine walk failure still makes status fail.
	if err := os.Mkdir(filepath.Join(root, ".graphveinignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	errOut.Reset()
	if code := run([]string{"status", root}, &out, &errOut); code == 0 || !strings.Contains(errOut.String(), "freshness scan incomplete") {
		t.Fatal(code, errOut.String())
	}
}

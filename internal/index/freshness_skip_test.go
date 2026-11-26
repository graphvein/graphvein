package index_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
)

// writeSkippedEntries adds the entries the scanner deliberately does not
// read: a binary file, a non-UTF-8 file and, where the OS has them, a
// FIFO. It reports whether the FIFO was made.
func writeSkippedEntries(t *testing.T, root string) bool {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "latin1.txt"), []byte("caf\xe9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return makeFIFO(filepath.Join(root, "pipe"))
}

// TestFreshnessIgnoresDeliberateSkips is the regression test for the
// scanner's skip records (couldn't-read rows CR-34/CR-35): a repository
// holding a binary, a non-UTF-8 file or a FIFO reads fresh after an index
// and stale after an edit, never "freshness scan incomplete".
func TestFreshnessIgnoresDeliberateSkips(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := writeSkippedEntries(t, root)
	s := open(t)
	emit(t, root, s, options(t))
	changed, err := index.Freshness(ctx, root, "fixture", s)
	if err != nil || len(changed) != 0 {
		t.Fatalf("after index (fifo %v): changed %v, err %v", fifo, changed, err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { main() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err = index.Freshness(ctx, root, "fixture", s)
	if err != nil || !reflect.DeepEqual(changed, []string{"main.go"}) {
		t.Fatalf("after edit: changed %v, err %v", changed, err)
	}
}

// TestFreshnessWalkFailureUnknown checks that a genuine walk failure still
// makes freshness unknown when skip records are present as well: an
// unreadable .graphveinignore (a directory, which fails on every OS).
func TestFreshnessWalkFailureUnknown(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkippedEntries(t, root)
	s := open(t)
	emit(t, root, s, options(t))
	if err := os.Mkdir(filepath.Join(root, ".graphveinignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	changed, err := index.Freshness(context.Background(), root, "fixture", s)
	if err == nil || !strings.HasPrefix(err.Error(), "freshness scan incomplete:") {
		t.Fatalf("changed %v, err %v; want an incomplete scan", changed, err)
	}
}

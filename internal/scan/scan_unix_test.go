//go:build unix

package scan

import (
	"context"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

// TestSkippedSpecialEntries checks that a non-regular file (a FIFO) and a
// path the contract cannot carry are in Skipped: Python's
// import system may still see them, so an import they satisfy is refused,
// never claimed external.
func TestSkippedSpecialEntries(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ok.py", "x = 1\n")
	write(t, root, "bad\\name.py", "x = 1\n")
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.py"), 0o644); err != nil {
		t.Skip("FIFOs unavailable:", err)
	}
	result, err := Walk(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"bad\\name.py", "pipe.py"}; !reflect.DeepEqual(result.Skipped, want) {
		t.Errorf("skipped %q, want %q", result.Skipped, want)
	}
	// The FIFO gets a record (couldn't-read row CR-34).
	var codes []string
	for _, d := range result.Diagnostics {
		codes = append(codes, d.File+":"+d.Code+":"+d.Severity)
	}
	if want := []string{":invalid_path:warning", "pipe.py:file_not_regular:info"}; !reflect.DeepEqual(codes, want) {
		t.Errorf("diagnostics %q, want %q", codes, want)
	}
}

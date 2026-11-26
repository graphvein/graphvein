package sqlite

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteDSNWindows proves a Windows path becomes file:///C:/... and
// decodes back to the same absolute path.
func TestWriteDSNWindows(t *testing.T) {
	abs, err := filepath.Abs(filepath.Join(t.TempDir(), "Jane Doe", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := writeDSN(abs)
	if err != nil {
		t.Fatal(err)
	}
	if prefix := "file:///" + filepath.VolumeName(abs); !strings.HasPrefix(dsn, prefix) {
		t.Fatalf("DSN %q does not start with %q", dsn, prefix)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.FromSlash(strings.TrimPrefix(u.Path, "/")); got != abs {
		t.Fatalf("DSN path %q, want %q", got, abs)
	}
}

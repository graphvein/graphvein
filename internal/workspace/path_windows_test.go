//go:build windows

package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveWindowsCase checks WS-23 on Windows: a lower-case drive
// letter and components typed in another case resolve to the upper-case
// drive letter and the stored casing (filepath.EvalSymlinks does both).
func TestResolveWindowsCase(t *testing.T) {
	dir, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "MixedCase")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	vol := filepath.VolumeName(folder)
	if len(vol) != 2 {
		t.Skip("not on a drive letter:", vol)
	}
	typed := strings.ToLower(folder)
	got, err := Resolve(typed)
	if err != nil || got != folder {
		t.Fatalf("Resolve(%q) = %q %v, want %q", typed, got, err, folder)
	}
	if !SamePath(strings.ToLower(vol)+folder[2:], folder) {
		t.Fatal("drive letter case makes two folders")
	}
}

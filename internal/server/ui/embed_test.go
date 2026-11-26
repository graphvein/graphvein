package ui

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLegalCopies keeps the embedded notice files equal to the
// repository's NOTICE and THIRD_PARTY_NOTICES, and checks that Assets
// serves them and still serves the UI.
func TestLegalCopies(t *testing.T) {
	for copyName, original := range map[string]string{
		"NOTICE.txt":              "NOTICE",
		"THIRD_PARTY_NOTICES.txt": "THIRD_PARTY_NOTICES",
	} {
		want, err := os.ReadFile(filepath.Join("..", "..", "..", original))
		if err != nil {
			t.Fatal(err)
		}
		got, err := fs.ReadFile(Assets, copyName)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
			t.Errorf("internal/server/ui/legal/%s differs from %s; run go run ./scripts/notices or copy it", copyName, original)
		}
	}
	if _, err := fs.Stat(Assets, "index.html"); err != nil {
		t.Errorf("index.html: %v", err)
	}
	if _, err := fs.Stat(Assets, "third-party-licenses.txt"); err != nil {
		t.Errorf("third-party-licenses.txt: %v", err)
	}
}

// TestGoNoticeVersion keeps the Go standard library version named in the
// served third-party-licenses.txt equal to the toolchain go.mod selects.
// The generator (web/scripts/third-party-licenses.mjs) reads go.mod, so a
// toolchain bump without a rebuild of the web UI fails here.
func TestGoNoticeVersion(t *testing.T) {
	goMod, err := os.ReadFile(filepath.Join("..", "..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(goMod), "\r\n", "\n"), "\n") {
		if v, ok := strings.CutPrefix(line, "toolchain go"); ok {
			want = strings.TrimSpace(v)
			break
		}
		if v, ok := strings.CutPrefix(line, "go "); ok && want == "" {
			want = strings.TrimSpace(v)
		}
	}
	if want == "" {
		t.Fatal("go.mod has neither a toolchain nor a go directive")
	}
	notices, err := fs.ReadFile(Assets, "third-party-licenses.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := ""
	for _, line := range strings.Split(string(notices), "\n") {
		if v, ok := strings.CutPrefix(line, "Go standard library "); ok {
			got = v
			break
		}
	}
	if got != want {
		t.Errorf("third-party-licenses.txt names Go standard library %q, go.mod selects %q; rebuild the web UI (bash scripts/check-web.sh)", got, want)
	}
}

package server

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/store"
)

// TestStatusFreshWithSkippedEntries is the regression test for
// GET /api/v0/status over a repository holding a binary, a non-UTF-8 file
// and (on Unix) a FIFO: fresh after an index, stale after an edit, and
// unknown only for a genuine walk failure.
func TestStatusFreshWithSkippedEntries(t *testing.T) {
	backends(t, func(t *testing.T, w, s store.Store) {
		root := fixtureRoot(t)
		for name, data := range map[string]string{"logo.png": "\x89PNG\r\n\x1a\n\x00\x00", "latin1.txt": "caf\xe9\n"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		makeFIFO(filepath.Join(root, "pipe"))
		indexRoot(t, root, w)
		opt := testOptions(root, s)
		got := decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if got.Freshness.State != stateFresh || *got.Freshness.ChangedCount != 0 {
			t.Fatalf("after index: %+v", got.Freshness)
		}
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got = decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if got.Freshness.State != stateStale || !reflect.DeepEqual(got.Freshness.ChangedFiles, []string{"main.go"}) {
			t.Fatalf("after edit: %+v", got.Freshness)
		}
		if err := os.Mkdir(filepath.Join(root, ".graphveinignore"), 0o755); err != nil {
			t.Fatal(err)
		}
		got = decodeStatus(t, get(t, opt, http.MethodGet, "/api/v0/status", nil))
		if got.Freshness.State != stateUnknown || !strings.HasPrefix(got.Freshness.Error, "freshness scan incomplete:") {
			t.Fatalf("walk failure: %+v", got.Freshness)
		}
	})
}

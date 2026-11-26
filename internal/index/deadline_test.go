package index_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/parse"
)

// TestSizeScaledParseDeadline pins ADR 0090 through the pipeline: a large
// file whose parse needs more than the base timeout, but less than its
// size-scaled deadline, is indexed; a small file needing the same work
// still fails with parse_failed, as every file did under the old fixed
// deadline. The work is simulated by a parse hook that compares it with the
// deadline each file got, so the test does not race the clock.
func TestSizeScaledParseDeadline(t *testing.T) {
	const base, work = 200 * time.Millisecond, 500 * time.Millisecond
	// Five deadline units: a scaled deadline of 1 s, twice the work.
	large := "/*" + strings.Repeat("x", 5*parse.DeadlineUnit-4) + "*/"
	if parse.FileTimeout(base, len(large)) < 2*work || parse.FileTimeout(base, 20) >= work {
		t.Fatal("fixture sizes do not separate the deadlines")
	}
	root := t.TempDir()
	for name, body := range map[string]string{"bundle.js": large, "small.js": "export function f() {}\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opt := options(t)
	opt.Parser.Timeout = base
	restore := parse.SimulateParseWork(work)
	t.Cleanup(restore)
	s := open(t)
	_, summary, _ := emit(t, root, s, opt)
	restore()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	failed := map[string]string{}
	for _, d := range snapshot.Diagnostics {
		failed[d.File] = d.Code
	}
	if failed["small.js"] != "parse_failed" || failed["bundle.js"] != "" || summary.Diagnostics != 1 {
		t.Fatal(summary, snapshot.Diagnostics)
	}
}

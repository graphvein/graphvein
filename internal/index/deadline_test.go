package index_test

import (
	"bytes"
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
	// The large file fits its scaled deadline in the parallel pass; the
	// small one does not, and is read by the serial retry with the capped
	// deadline (T074). Before T074 it failed with parse_failed.
	if summary.Diagnostics != 0 || summary.Retried != 1 || summary.Recovered != 1 || summary.ParseFailed != 0 {
		t.Fatal(summary, snapshot.Diagnostics)
	}
}

// copyTypeScriptRepo copies testdata/repos/typescript with its inert names
// restored, as TestRepositoryGolden does.
func copyTypeScriptRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/repos/typescript")); err != nil {
		t.Fatal(err)
	}
	for _, names := range [][2]string{{"_graphveinignore", ".graphveinignore"}, {"_claude", ".claude"}, {"nested/_gitignore", "nested/.gitignore"}, {"nested/_skip.ts", "nested/skip.ts"}} {
		if err := os.Rename(filepath.Join(root, names[0]), filepath.Join(root, names[1])); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestParseRetryMatchesUnloadedRun pins T074: when every file's parse runs
// past its deadline in the parallel pass (as under heavy CPU load), the
// serial retry with the capped deadline reads them all, and the snapshot is
// byte for byte the snapshot of a run without any timeout. The parse work
// is simulated: it does not fit the base deadline but fits the retry's.
func TestParseRetryMatchesUnloadedRun(t *testing.T) {
	root := copyTypeScriptRepo(t)
	opt := options(t)
	want, normal, _ := emit(t, root, open(t), opt)
	if normal.Retried != 0 || normal.Parsed == 0 {
		t.Fatal("the unloaded run retried or parsed nothing", normal)
	}
	const base, work = 50 * time.Millisecond, 200 * time.Millisecond
	if parse.FileTimeout(base, 64<<10) >= work || parse.RetryTimeout(base) < 2*work {
		t.Fatal("deadlines do not separate the passes")
	}
	loaded := options(t)
	loaded.Parser.Timeout = base
	restore := parse.SimulateParseWork(work)
	t.Cleanup(restore)
	got, summary, _ := emit(t, root, open(t), loaded)
	restore()
	if summary.Retried != normal.Parsed || summary.Recovered != summary.Retried || summary.ParseFailed != 0 {
		t.Fatalf("retried %d, recovered %d, failed %d; want all %d files read", summary.Retried, summary.Recovered, summary.ParseFailed, normal.Parsed)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("the retried run's snapshot differs from the unloaded run's")
	}
}

// TestParseRetryStillTimesOut pins that a file whose retry also runs past
// its deadline keeps its parse_failed diagnostic, which says so, and is
// counted as not read and timed out.
func TestParseRetryStillTimesOut(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := options(t)
	opt.Parser.Timeout = 20 * time.Millisecond
	restore := parse.HoldParseUntilDeadline()
	t.Cleanup(restore)
	s := open(t)
	_, summary, _ := emit(t, root, s, opt)
	restore()
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ParseFailed != 1 || summary.ParseTimedOut != 1 || summary.Retried != 1 || summary.Recovered != 0 || len(snapshot.Diagnostics) != 1 {
		t.Fatal(summary, snapshot.Diagnostics)
	}
	d := snapshot.Diagnostics[0]
	if d.Code != "parse_failed" || d.Message != "parsing a.ts: context deadline exceeded (also when parsed again alone with a 240ms deadline)" {
		t.Fatalf("%+v", d)
	}
}

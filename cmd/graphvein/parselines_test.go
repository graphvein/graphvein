package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/parse"
)

// TestParseLines pins graphvein index's lines on files it could not parse
// (T074): none for a run that read every file, the retry when one ran, and
// the reason of the files still not read.
func TestParseLines(t *testing.T) {
	for _, c := range []struct {
		summary index.Summary
		want    []string
	}{
		{index.Summary{}, nil},
		{index.Summary{Retried: 39, Recovered: 39}, []string{"Parse retry: 39 files timed out and parsed again alone; 39 read"}},
		{index.Summary{Retried: 4, Recovered: 1, ParseFailed: 3, ParseTimedOut: 3}, []string{
			"Parse retry: 4 files timed out and parsed again alone; 1 read",
			"3 files not read: parse timed out (parse_failed); counts are incomplete, see graphvein serve's Couldn't read page",
		}},
		{index.Summary{ParseFailed: 1}, []string{"1 file not read: parse failed (parse_failed); counts are incomplete, see graphvein serve's Couldn't read page"}},
		{index.Summary{Retried: 1, ParseFailed: 2, ParseTimedOut: 1}, []string{
			"Parse retry: 1 file timed out and parsed again alone; 0 read",
			"2 files not read: parse failed, 1 of them timed out (parse_failed); counts are incomplete, see graphvein serve's Couldn't read page",
		}},
	} {
		if got := parseLines(c.summary); !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseLines(%+v) = %q, want %q", c.summary, got, c.want)
		}
	}
}

// TestIndexPrintsParseTimeouts runs graphvein index with a parse that
// outlasts every deadline, the retry's included: the run succeeds and says
// which files it could not read.
func TestIndexPrintsParseTimeouts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GRAPHVEIN_SQLITE_PATH", filepath.Join(root, ".graphvein", "index.db"))
	t.Setenv("GRAPHVEIN_POSTGRES_URL", "")
	if err := os.WriteFile(filepath.Join(root, "a.js"), []byte("export function f() { return 1; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := parse.HoldParseUntilDeadline()
	t.Cleanup(restore)
	var out, errOut bytes.Buffer
	code := run([]string{"index", root, "--timeout", "20ms"}, &out, &errOut)
	restore()
	if code != 0 {
		t.Fatal(code, errOut.String())
	}
	for _, want := range []string{"Parse retry: 1 file timed out and parsed again alone; 0 read\n", "1 file not read: parse timed out (parse_failed); counts are incomplete"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q lacks %q", out.String(), want)
		}
	}
}

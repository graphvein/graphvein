package index_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/parse"
)

func TestAngularGrammarFixture(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-grammar")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, summary, _ := emit(t, root, s, opt)
	if summary.Files != 2 || summary.Nodes != 2 || summary.Edges != 0 || summary.Diagnostics != 0 || summary.Parsed != 2 {
		t.Fatal(summary)
	}
	snapshot, err := s.Dump(context.Background(), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	// Existing file nodes remain unchanged; no template structure is emitted.
	for _, n := range snapshot.Nodes {
		if n.Kind != "file" {
			t.Fatal("unexpected template node", n)
		}
	}
	golden.Check(t, "../../testdata/fixtures/angular-grammar.golden.json", snapshot.Files)
	want := []contract.Span{{StartLine: 2, StartColumn: 18, EndLine: 2, EndColumn: 28}, {StartLine: 2, StartColumn: 24, EndLine: 2, EndColumn: 26}, {StartLine: 3, StartColumn: 16, EndLine: 3, EndColumn: 16}}
	for _, f := range snapshot.Files {
		if f.Language != "html" || f.Precision != "best_effort" {
			t.Fatal(f)
		}
		cached, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
		if err != nil {
			t.Fatal(err)
		}
		var facts template.Facts
		var shape map[string]json.RawMessage
		if json.Unmarshal(cached, &facts) != nil || json.Unmarshal(cached, &shape) != nil || len(shape) != 5 || !facts.Valid() {
			t.Fatalf("cache must contain complete template syntax: %s", cached)
		}
		if f.Path == "broken.htm" && !reflect.DeepEqual(facts.ErrorSpans, want) {
			t.Fatal(facts)
		}
		if f.Path == "clean.html" && len(facts.ErrorSpans) != 0 {
			t.Fatal(facts)
		}
	}
	again, reused, writes := emit(t, root, s, opt)
	if !bytes.Equal(data, again) || reused.Parsed != 0 || reused.Reused != 2 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
		t.Fatal("template replay changed snapshot", reused, writes)
	}
	// Reformatting line endings leaves the normalized spans and hashes reusable.
	p := filepath.Join(root, "broken.htm")
	source, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	if err := os.WriteFile(p, bytes.ReplaceAll(source, []byte("\n"), []byte("\r\n")), 0644); err != nil {
		t.Fatal(err)
	}
	_, reused, _ = emit(t, root, s, opt)
	if reused.Parsed != 0 || reused.Reused != 2 {
		t.Fatal("line endings invalidated normalized cache", reused)
	}
}

func TestAngularGrammarFailures(t *testing.T) {
	for _, failure := range []string{"size", "deadline"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<p>hello</p>"), 0644); err != nil {
				t.Fatal(err)
			}
			opt := options(t)
			code := "file_too_large"
			restore := func() {}
			if failure == "size" {
				opt.MaxFileSize = 1
			} else {
				// The hold keeps the parse running until its deadline has
				// passed; a 1 ns timeout alone lost the race on a Windows
				// runner and the page parsed.
				opt.Parser.Timeout = time.Nanosecond
				restore = parse.HoldParseUntilDeadline()
				code = "parse_failed"
			}
			t.Cleanup(restore)
			s := open(t)
			_, summary, _ := emit(t, root, s, opt)
			restore()
			snapshot, err := s.Dump(context.Background(), "fixture")
			if err != nil || summary.Diagnostics != 1 || snapshot.Diagnostics[0].Code != code {
				t.Fatal(summary, snapshot.Diagnostics, err)
			}
			opt.MaxFileSize, opt.Parser.Timeout = 0, 0
			_, summary, _ = emit(t, root, s, opt)
			if summary.Parsed != 1 || summary.Reused != 0 || summary.Diagnostics != 0 {
				t.Fatal("transient failure cached", summary)
			}
		})
	}
}

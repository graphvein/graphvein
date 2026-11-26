//go:build !native

package benchmark

import (
	"encoding/json"
	"graphvein/spike/parsing/internal/parse"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanExtensionsAndIgnores(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.js", "b.jsx", "c.mjs", "d.cjs", "e.ts", "f.mts", "g.cts", "h.tsx", "i.rs", "j.go", "k.py", "l.java", "template.html", "item.cs", "item.dart", "package.json", "node_modules/ignored.ts"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("source"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	files, skipped, err := Scan(root, "fixture", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 15 || len(skipped) != 0 {
		t.Fatalf("files=%d, skipped=%+v", len(files), skipped)
	}
	for _, file := range files {
		if strings.Contains(file.Label, "ignored") {
			t.Fatal("scanned ignored file")
		}
	}
	angular, _, err := Scan(root, "angular", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range angular {
		if filepath.Ext(file.Path) == ".html" && file.Language != "angular" {
			t.Fatal("Angular corpus template was not routed to Angular grammar")
		}
	}
}

func TestAggregateMediansErrorsAndPrivacy(t *testing.T) {
	results := []Observation{}
	for i := 1; i <= 7; i++ {
		results = append(results, Observation{File: File{Corpus: "private", Language: "go", Path: "secret/path.go", Label: "[private]", Private: true}, Bytes: 10, ParseNS: int64(i), SyntaxError: i%2 == 0, ErrorNodes: i % 2})
	}
	results = append(results, Observation{File: File{Corpus: "private", Language: "go", Label: "[private]", Private: true}, Error: "withheld", Panic: true})
	stats := summarize(results)
	s := stats[0]
	if s.Files != 8 || s.Parsed != 7 || s.Bytes != 70 || s.SumNS != 28 || s.MedianNS != 4 || s.SyntaxFiles != 3 || s.Failures != 1 || s.Panics != 1 || len(s.Slowest) != 5 || len(s.Worst) != 3 {
		t.Fatalf("wrong stats: %+v", s)
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatal("private filename leaked")
	}
}

type panickingParser struct{ closed bool }

type panickingCloser struct{ panickingParser }

func (p *panickingCloser) Close() { panic("seeded cleanup failure") }

func TestClosePanicIsReported(t *testing.T) {
	if err := closeParser(&panickingCloser{}); err == nil || !strings.Contains(err.Error(), "seeded cleanup failure") {
		t.Fatalf("cleanup failure was hidden: %v", err)
	}
}

func (p *panickingParser) Parse([]byte) (parse.Tree, error) { panic("seeded failure") }
func (p *panickingParser) Close()                           { p.closed = true }

func TestPanicRetiresParserAndNextFileSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "valid.go")
	if err := os.WriteFile(path, []byte("package sample\nfunc Fine() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	file := File{Corpus: "fixture", Language: "go", Path: path, Label: "valid.go"}
	bad := &panickingParser{}
	parsers := map[string]parse.Parser{"go": bad}
	create := func(lang string) (parse.Parser, error) { return parse.New("wasm", lang) }
	first := attempt(file, parsers, create)
	if !first.Panic || first.Error == "" || !bad.closed || parsers["go"] != nil {
		t.Fatalf("panic not isolated: %+v", first)
	}
	second := attempt(file, parsers, create)
	if p := parsers["go"]; p != nil {
		defer p.Close()
	}
	if second.Error != "" || second.SyntaxError || second.Bytes == 0 {
		t.Fatalf("next file failed: %+v", second)
	}
}

func TestMissingFileAndSyntaxErrorDoNotStopWorkers(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.go")
	valid := filepath.Join(dir, "valid.go")
	if err := os.WriteFile(broken, []byte("package sample\nfunc Broken() {"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(valid, []byte("package sample\nfunc Fine() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	files := []File{}
	for _, path := range []string{filepath.Join(dir, "missing.go"), broken, valid} {
		files = append(files, File{Corpus: "fixture", Language: "go", Path: path, Label: filepath.Base(path)})
	}
	run, err := Measure(files, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	s := run.Stats[0]
	if s.Files != 3 || s.Parsed != 2 || s.Failures != 1 || s.SyntaxFiles != 1 || s.Panics != 0 || len(s.Worst) != 1 || s.Worst[0].ErrorNodes < 1 {
		t.Fatalf("failure isolation/counting: %+v", s)
	}
	if _, err := Measure(nil, nil, 0); err == nil {
		t.Fatal("accepted zero workers")
	}
}

func TestPrivateAttemptHidesReadErrorPath(t *testing.T) {
	file := File{Corpus: "private", Language: "go", Path: "secret/missing.go", Label: "[private]", Private: true}
	result := attempt(file, map[string]parse.Parser{}, func(lang string) (parse.Parser, error) { return parse.New("wasm", lang) })
	if result.Error == "" || strings.Contains(result.Error, "secret") {
		t.Fatalf("private diagnostic: %q", result.Error)
	}
}

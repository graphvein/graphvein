package experiment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graphvein/spike/parsing/internal/parse"
)

func TestGoldenSamples(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	m := Run(testBackend, "")
	for _, sample := range m.Samples {
		if !sample.Correct {
			t.Errorf("%s: %s", sample.Path, sample.Error)
		}
	}
	if !m.OK() {
		t.Fatal("fixture suite failed")
	}
	// Prove that comparison fails on a wrong golden without modifying it.
	wrong := append([]parse.Entry(nil), m.Samples[0].Entries...)
	wrong[0].Line++
	if err := CheckGolden(wrong, "samples/items.tsx.golden.json"); err == nil || !strings.Contains(err.Error(), "actual:") {
		t.Fatalf("wrong golden should produce a readable mismatch, got %v", err)
	}
}

func TestLargeSourceAndMeasurements(t *testing.T) {
	t.Chdir(filepath.Join("..", ".."))
	source := LargeSource()
	if lines := strings.Count(string(source), "\n"); lines != 6000 {
		t.Fatalf("got %d lines", lines)
	}
	path := filepath.Join(t.TempDir(), "large.tsx")
	if err := os.WriteFile(path, source, 0644); err != nil {
		t.Fatal(err)
	}
	m := Run(testBackend, path)
	if !m.OK() || len(m.ParseNS) != 5 || m.MedianNS <= 0 {
		t.Fatalf("invalid measurement: %+v", m)
	}
	p, err := parse.New(testBackend, "tsx")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	tree, err := p.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	entries, err := parse.Extract(tree, source)
	if err != nil {
		t.Fatal(err)
	}
	// T002 also lists interfaces and methods: every block has an interface,
	// a function, an arrow function, a class and its method.
	if len(entries) != 5000 {
		t.Fatalf("got %d declarations, want 5000", len(entries))
	}
}

func TestMalformedFileDoesNotPreventNextParse(t *testing.T) {
	for _, language := range []string{"tsx", "rust"} {
		t.Run(language, func(t *testing.T) {
			p, err := parse.New(testBackend, language)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			malformed := []byte("export function broken( {")
			valid := []byte("const Fine = () => <div />;")
			if language == "rust" {
				malformed = []byte("pub fn broken( {")
				valid = []byte("pub fn fine() {}")
			}
			tree, err := p.Parse(malformed)
			if err != nil {
				t.Fatal(err)
			}
			_, extractErr := parse.Extract(tree, malformed)
			tree.Close()
			if extractErr == nil {
				t.Fatal("malformed source accepted")
			}
			tree, err = p.Parse(valid)
			if err != nil {
				t.Fatal(err)
			}
			defer tree.Close()
			entries, err := parse.Extract(tree, valid)
			if err != nil || len(entries) != 1 {
				t.Fatalf("subsequent valid parse: %v, %+v", err, entries)
			}
		})
	}
}

func TestRunContinuesAfterMissingFile(t *testing.T) {
	// Supply only the second sample in an isolated directory.
	rust, err := os.ReadFile(filepath.Join("..", "..", "samples", "items.rs"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join("..", "..", "samples", "items.rs.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "samples"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "samples", "items.rs"), rust, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "samples", "items.rs.golden.json"), golden, 0644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	m := Run(testBackend, "")
	if m.OK() || len(m.Samples) != 2 || m.Samples[0].Error == "" || !m.Samples[1].Correct {
		t.Fatalf("failure was not isolated: %+v", m)
	}
}

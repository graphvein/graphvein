package parse

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
)

func TestAngularGrammarGolden(t *testing.T) {
	source, err := os.ReadFile("../../testdata/fixtures/angular-grammar/clean.html")
	if err != nil {
		t.Fatal(err)
	}
	source = bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))
	results, err := Batch(context.Background(), []Job{{Language: "angular", Source: source}}, Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil || results[0].SyntaxError || len(results[0].Root.ErrorSpans) != 0 {
		t.Fatalf("Angular sample: %+v, %v", results, err)
	}
	type entry struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Line int    `json:"line"`
	}
	var entries []entry
	var walk func(*Node)
	walk = func(n *Node) {
		kind, target := "", (*Node)(nil)
		switch n.Kind {
		case "start_tag", "self_closing_tag":
			for _, child := range n.Children {
				if child.Kind == "tag_name" {
					kind, target = "element", child
				}
			}
		case "structural_directive", "property_binding", "event_binding", "two_way_binding", "interpolation", "pipe_call":
			kind, target = n.Kind, n
		case "control_keyword":
			kind, target = "control", n
		}
		if target != nil {
			entries = append(entries, entry{kind, target.Text, int(target.Row) + 1})
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(results[0].Root)
	golden.Check(t, "angular.golden.json", entries)
}

func TestAngularMissingAndErrorSpans(t *testing.T) {
	// The missing ']' is unnamed and would be lost by named-child detachment.
	source := []byte("<p>é</p>\n<div *rxLet=\"tags$; let tags\"></div>\n<div [title]=\"[x\"></div>\n")
	want := []contract.Span{{StartLine: 2, StartColumn: 18, EndLine: 2, EndColumn: 28}, {StartLine: 2, StartColumn: 24, EndLine: 2, EndColumn: 26}, {StartLine: 3, StartColumn: 16, EndLine: 3, EndColumn: 16}}
	for _, workers := range []int{1, 4} {
		jobs := []Job{{Language: "angular", Source: source}, {Language: "angular", Source: []byte("<div title=\"é\" [title]=\"[x\"></div>")}}
		results, err := Batch(context.Background(), jobs, Options{Workers: workers, CacheDir: filepath.Join(t.TempDir(), "cache")})
		if err != nil {
			t.Fatal(err)
		}
		if results[0].Err != nil || !results[0].SyntaxError || !reflect.DeepEqual(results[0].Root.ErrorSpans, want) {
			t.Fatalf("workers=%d: %+v", workers, results[0])
		}
		byteSpan := []contract.Span{{StartLine: 1, StartColumn: 27, EndLine: 1, EndColumn: 27}}
		if results[1].Err != nil || !reflect.DeepEqual(results[1].Root.ErrorSpans, byteSpan) {
			t.Fatalf("UTF-8 columns: %+v", results[1])
		}
	}
}

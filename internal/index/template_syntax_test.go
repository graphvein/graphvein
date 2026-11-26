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

	"github.com/graphvein/graphvein/internal/contract"
	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/lang/typescript"
)

func TestAngularTemplateSyntax(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-templates")); err != nil {
		t.Fatal(err)
	}
	s, opt := open(t), options(t)
	data, first, _ := emit(t, root, s, opt)
	load := func() map[string]any {
		t.Helper()
		snapshot, err := s.Dump(context.Background(), "fixture")
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]any{}
		for _, f := range snapshot.Files {
			if f.Language != "html" && f.Language != "typescript" {
				continue
			}
			cached, err := s.FactCache(context.Background(), "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
			if err != nil {
				t.Fatal(err)
			}
			if f.Language == "html" {
				var v template.Facts
				if err = json.Unmarshal(cached, &v); err != nil || !v.Valid() {
					t.Fatal(err, string(cached))
				}
				out[f.Path] = v
			} else {
				var v typescript.CachedFacts
				if err = json.Unmarshal(cached, &v); err != nil || !v.TemplateSyntax.Valid() {
					t.Fatal(err, string(cached))
				}
				out[f.Path] = *v.TemplateSyntax
			}
		}
		return out
	}
	projection := load()
	golden.Check(t, "../../testdata/fixtures/angular-templates.golden.json", projection)
	html := projection["page.html"].(template.Facts)
	kinds := map[string]int{}
	extra, invalid, i18n := 0, 0, 0
	for _, a := range html.Attributes {
		kinds[a.Kind]++
		if a.ExtraStructural {
			extra++
		}
		if a.InvalidVariable {
			invalid++
		}
		if a.I18n {
			i18n++
		}
		if a.RawName == "DATA-bind-title" && (a.Kind != "property" || a.Name != "title" || a.NormalizedName != "bind-title") {
			t.Fatal(a)
		}
	}
	for _, kind := range []string{"static", "property", "event", "two_way", "variable", "reference", "animation", "interpolated", "structural"} {
		if kinds[kind] == 0 {
			t.Error("missing kind", kind)
		}
	}
	if extra != 1 || invalid != 1 || i18n != 2 {
		t.Fatal(extra, invalid, i18n)
	}
	blocks := map[string]bool{}
	for _, b := range html.Blocks {
		blocks[b.Name] = true
		if b.Parameters == nil {
			t.Fatal(b)
		}
	}
	for _, name := range []string{"if", "else if", "else", "for", "empty", "switch", "case", "default", "defer", "placeholder", "loading", "error", "let"} {
		if !blocks[name] {
			t.Error("missing block", name)
		}
	}
	pipes := map[string]template.Pipe{}
	for _, p := range html.Pipes {
		pipes[p.Name] = p
	}
	for _, name := range []string{"first", "second", "interpolated", "condition", "assigned", "covered"} {
		if _, ok := pipes[name]; !ok {
			t.Error("missing pipe", name)
		}
	}
	if _, ok := pipes["ignored"]; ok {
		t.Error("non-bindable or dropped content emitted a pipe")
	}
	if !pipes["covered"].Flagged {
		t.Error("pipe in a recovered expression must be flagged", pipes["covered"], html.ErrorSpans)
	}
	dropped, nonbind, slot, svg := 0, 0, false, false
	for _, e := range html.Elements {
		if e.Dropped {
			dropped++
		}
		if e.NonBindable {
			nonbind++
		}
		slot = slot || e.Name == "ng-content" && e.Select == ".slot"
		svg = svg || e.Name == "svg:rect"
	}
	if dropped != 3 || nonbind != 2 || !slot || !svg {
		t.Fatal(dropped, nonbind, slot, svg)
	}
	ts := projection["decorators.ts"].(typescript.TemplateSyntax)
	if len(ts.Decorators) != 13 || len(ts.Inline) != 8 {
		t.Fatal("decorators/inline", len(ts.Decorators), len(ts.Inline))
	}
	reasons := map[string]int{}
	for _, in := range ts.Inline {
		reasons[in.Reason]++
	}
	for _, reason := range []string{"substitution", "escape", "nonliteral", "metadata_spread_element", "metadata_computed"} {
		if reasons[reason] != 1 {
			t.Error("missing inadmissible reason", reason, reasons)
		}
	}
	for _, d := range ts.Decorators {
		if d.Class == "" || d.Callee.Span == nil {
			t.Fatal("metadata lost class or callee location", d)
		}
	}
	if ts.Decorators[0].Callee.Text != "C" || !ts.Decorators[0].ObjectMetadata || ts.Decorators[1].Callee.Text != "core.Component" || !ts.Decorators[1].ObjectMetadata || ts.Decorators[6].Arguments != 0 || ts.Decorators[6].ObjectMetadata {
		t.Fatal(ts.Decorators)
	}
	for _, entry := range ts.Decorators[7].Entries {
		if entry.Key == "factory" && (entry.Kind != "method_definition" || entry.KeySpan == nil) {
			t.Fatal("method key lost", entry)
		}
		if entry.Key == "standalone" && entry.Value.Kind != "identifier" {
			t.Fatal("shorthand syntax lost", entry)
		}
	}
	// Compare the actual host source at every captured location, including first
	// line columns and later lines; use independent expected names as evidence.
	host, err := os.ReadFile(filepath.Join(root, "decorators.ts"))
	if err != nil {
		t.Fatal(err)
	}
	host = bytes.ReplaceAll(host, []byte("\r\n"), []byte("\n"))
	for _, in := range ts.Inline {
		if in.Facts == nil {
			continue
		}
		for _, p := range in.Facts.Pipes {
			if string(atSpan(host, p.Span)) != p.Name {
				t.Fatal("mapped pipe", p)
			}
		}
		for _, a := range in.Facts.Attributes {
			if string(atSpan(host, a.NameSpan)) != a.RawName || a.ValueSpan != nil && string(atSpan(host, *a.ValueSpan)) != a.Value {
				t.Fatal("mapped attribute", a)
			}
		}
	}
	again, second, writes := emit(t, root, s, opt)
	if !bytes.Equal(data, again) || second.Parsed != 0 || second.Reused != first.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 || !reflect.DeepEqual(projection, load()) {
		t.Fatal("cache round trip or replay", second, writes)
	}
	// A fresh store on CRLF input must produce identical facts, not just reuse.
	for _, name := range []string{"page.html", "decorators.ts"} {
		p := filepath.Join(root, name)
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
		if err = os.WriteFile(p, bytes.ReplaceAll(src, []byte("\n"), []byte("\r\n")), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s = open(t)
	emit(t, root, s, opt)
	if !reflect.DeepEqual(projection, load()) {
		t.Fatal("CRLF changed facts")
	}
}

func atSpan(src []byte, s contract.Span) []byte {
	point := func(line, col int) int {
		parts := bytes.SplitAfter(src, []byte("\n"))
		i := col
		for _, p := range parts[:line-1] {
			i += len(p)
		}
		return i
	}
	return src[point(s.StartLine, s.StartColumn):point(s.EndLine, s.EndColumn)]
}

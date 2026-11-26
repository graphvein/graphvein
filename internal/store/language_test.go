package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/graphvein/graphvein/internal/golden"
	"github.com/graphvein/graphvein/internal/index"
	"github.com/graphvein/graphvein/internal/ingest"
	"github.com/graphvein/graphvein/internal/lang/template"
	"github.com/graphvein/graphvein/internal/parse"
	"github.com/graphvein/graphvein/internal/store"
)

func TestAngularTemplateCache(t *testing.T) {
	backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
		ctx := context.Background()
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS("../../testdata/fixtures/angular-grammar")); err != nil {
			t.Fatal(err)
		}
		opt := index.Options{Repository: "fixture", Parser: parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")}}
		for run := 0; run < 2; run++ {
			var stream bytes.Buffer
			summary, err := index.Emit(ctx, root, s, &stream, opt)
			if err != nil {
				t.Fatal(err)
			}
			writes, err := ingest.StreamWithWrites(ctx, &stream, s)
			if err != nil {
				t.Fatal(err)
			}
			if summary.Files != 2 || summary.Nodes != 2 || summary.Edges != 0 || summary.Diagnostics != 0 {
				t.Fatal(summary)
			}
			if run == 0 && summary.Parsed != 2 || run == 1 && (summary.Parsed != 0 || summary.Reused != 2 || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0) {
				t.Fatal("template cache replay", summary, writes)
			}
		}
		snapshot, err := s.Dump(ctx, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, "../../testdata/fixtures/angular-grammar.golden.json", snapshot.Files)
		for _, f := range snapshot.Files {
			data, err := s.FactCache(ctx, "fixture", f.Path, f.ContentHash, fmt.Sprintf("%s/max=%d", index.Version, 2<<20))
			if err != nil {
				t.Fatal(err)
			}
			var facts template.Facts
			if json.Unmarshal(data, &facts) != nil || facts.ErrorSpans == nil {
				t.Fatalf("template cache: %s", data)
			}
			want := 0
			if f.Path == "broken.htm" {
				want = 3
			}
			if len(facts.ErrorSpans) != want {
				t.Fatal(f.Path, facts)
			}
		}
	})
}

func TestLanguageFixtures(t *testing.T) {
	for _, language := range []string{"go", "python", "rust"} {
		t.Run(language, func(t *testing.T) {
			backends(t, func(t *testing.T, s store.Store, _ *sql.DB) {
				ctx := context.Background()
				root := t.TempDir()
				if err := os.CopyFS(root, os.DirFS("../../testdata/repos/"+language)); err != nil {
					t.Fatal(err)
				}
				opt := index.Options{Repository: "fixture", GeneratedAt: "2026-10-05T00:00:00Z", Parser: parse.Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")}}
				run := func() ([]byte, index.Summary, store.Writes) {
					t.Helper()
					var stream bytes.Buffer
					summary, err := index.Emit(ctx, root, s, &stream, opt)
					if err != nil {
						t.Fatal(err)
					}
					writes, err := ingest.StreamWithWrites(ctx, bytes.NewReader(stream.Bytes()), s)
					if err != nil {
						t.Fatal(err)
					}
					return stream.Bytes(), summary, writes
				}
				data, summary, _ := run()
				snapshot, err := s.Dump(ctx, "fixture")
				if err != nil {
					t.Fatal(err)
				}
				verifyLanguage(t, language, snapshot)
				var records []any
				for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
					var value any
					decoder := json.NewDecoder(bytes.NewReader(line))
					decoder.UseNumber()
					if err := decoder.Decode(&value); err != nil {
						t.Fatal(err)
					}
					records = append(records, value)
				}
				golden.Check(t, "../../testdata/repos/"+language+".golden.json", records)
				golden.Check(t, "../../testdata/repos/"+language+".store.golden.json", snapshot)
				t.Logf("%s: files=%d nodes=%d edges=%d diagnostics=%d parsed=%d", language, summary.Files, summary.Nodes, summary.Edges, summary.Diagnostics, summary.Parsed)
				again, reused, writes := run()
				if !bytes.Equal(data, again) || reused.Parsed != 0 || reused.Reused != summary.Parsed || writes.Files+writes.Nodes+writes.Edges+writes.Deleted != 0 {
					t.Fatal("replay changed facts", reused, writes)
				}
				if language == "go" {
					p := filepath.Join(root, "main.go")
					source, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(p, bytes.Replace(source, []byte("return len(l.Items)"), []byte("return len(l.Items) + 1"), 1), 0644); err != nil {
						t.Fatal(err)
					}
					_, changed, _ := run()
					if changed.Parsed != 1 || changed.Reused != summary.Parsed-1 {
						t.Fatal("body edit work", changed)
					}
					t.Logf("one-line Go edit: parsed=%d reused=%d", changed.Parsed, changed.Reused)
					incremental, err := s.Dump(ctx, "fixture")
					if err != nil {
						t.Fatal(err)
					}
					opt.Repository = "full"
					run()
					full, err := s.Dump(ctx, "full")
					if err != nil || !reflect.DeepEqual(incremental, full) {
						t.Fatal("incremental/full mismatch", err)
					}
				}
			})
		})
	}
}

func verifyLanguage(t *testing.T, language string, snapshot store.Snapshot) {
	t.Helper()
	want := []int{9, 29, 31, 3}
	if language == "go" {
		// go.mod is a confirmed manifest: one package node (T026).
		want = []int{9, 30, 31, 3}
	}
	if language == "python" {
		// pkg/broken.py claims nothing beyond its file (row PS-F2 of
		// docs/lang/python-structure.md): no broken node or contains edge,
		// one python_structure_unread diagnostic.
		want = []int{6, 24, 32, 5}
	}
	if language == "rust" {
		// Six diagnostics: one is structure_unrooted for src/blind.rs (T020 S4L).
		// Cargo.toml is a confirmed manifest: one package node (T026).
		want = []int{8, 37, 53, 6}
	}
	got := []int{len(snapshot.Files), len(snapshot.Nodes), len(snapshot.Edges), len(snapshot.Diagnostics)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s counts %v, want %v", language, got, want)
	}
	names := map[string]bool{}
	for _, n := range snapshot.Nodes {
		names[n.Identity] = true
		// Go files are precise (row PL-L1) except broken.go, which go/parser
		// rejects, so its structure claims nothing (ADR 0068); Python files
		// are precise (row PL-L2) except pkg/broken.py, which CPython 3.14
		// rejects (ADR 0069). Rust files are precise (row PL-L4) except
		// src/broken.rs, with a syntax error (RS-F2), and src/blind.rs, which
		// no crate root reaches (ADR 0071).
		if n.Kind == "file" {
			want := "best_effort"
			if language == "go" && n.File != "broken.go" && strings.HasSuffix(n.File, ".go") {
				want = "precise"
			}
			if language == "python" && n.File != "pkg/broken.py" && strings.HasSuffix(n.File, ".py") {
				want = "precise"
			}
			if language == "rust" && n.File != "src/broken.rs" && n.File != "src/blind.rs" && strings.HasSuffix(n.File, ".rs") {
				want = "precise"
			}
			if n.Attributes["precision"] != want {
				t.Fatal("precision", want, n)
			}
		}
		if n.Kind == "file" {
			for _, key := range []string{"unresolved_calls_external", "unresolved_calls_unknown"} {
				if _, ok := n.Attributes[key]; !ok {
					t.Fatal("missing unresolved split", n.Identity, key)
				}
			}
		}
	}
	var required []string
	if language == "go" {
		required = []string{"method:main.go#User.Save", "method:main.go#List.Len", "function:main.go#init", "function:main.go#init%402", "function:extra.go#Extra", "function:main_test.go#TestOnly", "type_alias:main.go#Alias", "type:main.go#Count", "struct:main.go#List", "interface:main.go#Saver", "variable:main.go#Exported", "constant:main.go#Answer"}
	} else if language == "python" {
		required = []string{"class:pkg/main.py#User", "method:pkg/main.py#User.save", "function:pkg/main.py#run.validate", "class:pkg/main.py#run.Local", "function:pkg/main.py#conditional%402", "function:pkg/main.py#convert", "function:pkg/main.py#convert%40overload%28value%3Aint%29", "function:pkg/main.py#convert%40overload%28value%3Astr%3D%22a%20b%22%29"}
	} else {
		required = []string{"method:src/worker.rs#User.save", "method:src/worker.rs#User.save%40Local", "method:src/lib.rs#Local.save", "method:src/worker.rs#List.len", "function:src/lib.rs#inline.go", "function:src/lib.rs#tests.check", "function:src/lib.rs#alternative%402", "macro:src/lib.rs#greeting", "union:src/lib.rs#Value"}
	}
	for _, identity := range required {
		if !names[identity] {
			t.Errorf("missing %s", identity)
		}
	}
	calls := map[string]bool{}
	for _, e := range snapshot.Edges {
		if !names[e.Source] || !names[e.Target] {
			t.Fatal("dangling edge", e)
		}
		if e.Kind == "calls" {
			calls[e.Source+" -> "+e.Target] = true
		}
		if language == "go" && e.Kind == "imports" && e.Source == "file:main.go#main.go" && strings.HasSuffix(e.Target, "_test.go") {
			t.Fatal("non-test import included test file", e)
		}
		if language == "go" && e.Kind == "calls" && e.Source == "function:main.go#Run" && e.Target == "function:main_test.go#TestOnly" {
			t.Fatal("production call resolved to test", e)
		}
	}
	var wantedCalls []string
	if language == "go" {
		wantedCalls = []string{"function:main.go#Run -> method:main.go#User.Save", "function:main.go#local -> function:helper/help.go#Help", "function:main_test.go#TestRun -> function:main_test.go#TestOnly"}
	} else if language == "python" {
		wantedCalls = []string{"method:pkg/main.py#User.save -> method:pkg/main.py#User.validate", "function:pkg/main.py#run -> function:pkg/main.py#run.validate", "function:pkg/main.py#run.validate -> function:pkg/util.py#help", "function:pkg/main.py#run -> class:pkg/main.py#User"}
		wantedCalls = append(wantedCalls, "function:pkg/main.py#run -> function:pkg/util.py#help", "function:pkg/main.py#run -> method:pkg/main.py#User.validate", "function:pkg/main.py#run -> method:pkg/util.py#Helper.method", "method:pkg/util.py#Helper.method -> function:pkg/util.py#help")
	} else {
		wantedCalls = []string{"function:src/lib.rs#run -> function:src/worker.rs#work", "method:src/worker.rs#User.new -> method:src/worker.rs#User.save", "method:src/worker.rs#User.save%40Local -> method:src/worker.rs#User.validate", "method:src/worker.rs#List.len -> method:src/worker.rs#List.empty", "function:src/nested/mod.rs#deep -> function:src/nested/leaf.rs#end", "function:src/lib.rs#inline.go -> function:src/lib.rs#helper"}
	}
	for _, e := range wantedCalls {
		if !calls[e] {
			t.Errorf("missing call %s", e)
		}
	}
	syntax := false
	for _, d := range snapshot.Diagnostics {
		if d.Code == "syntax_error" {
			syntax = true
		}
		if d.Code == "parse_failed" || d.Code == "extract_failed" || d.Code == "invalid_reference" || d.Code == "invalid_record" {
			t.Error(d)
		}
	}
	if !syntax {
		t.Fatal("broken file missing syntax diagnostic")
	}
	counts := map[string][2]int{}
	for _, n := range snapshot.Nodes {
		if n.Kind == "file" {
			e, _ := strconv.Atoi(fmt.Sprint(n.Attributes["unresolved_calls_external"]))
			u, _ := strconv.Atoi(fmt.Sprint(n.Attributes["unresolved_calls_unknown"]))
			counts[n.File] = [2]int{e, u}
		}
	}
	if language == "go" && counts["main.go"] != [2]int{2, 1} {
		t.Fatal("Go unresolved split", counts)
	}
	if language == "python" && counts["pkg/main.py"] != [2]int{1, 1} {
		t.Fatal("Python unresolved split", counts)
	}
	if language == "rust" && counts["src/lib.rs"] != [2]int{1, 1} {
		t.Fatal("Rust unresolved split", counts)
	}
	// Span prefixes include decorators and therefore participate in hashing.
	if language == "python" {
		for _, n := range snapshot.Nodes {
			if n.QualifiedName == "User" {
				if n.Span.StartLine != 14 || !reflect.DeepEqual(n.Attributes["decorators"], []any{"typing.final"}) {
					t.Fatal("class decorator prefix", n)
				}
			}
		}
	}
}
